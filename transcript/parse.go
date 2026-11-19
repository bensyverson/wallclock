package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// File is a parsed transcript.
type File struct {
	Entries []Entry
	// Malformed counts lines that were not valid JSON.
	Malformed int
	// Skipped counts entries of types wallclock does not use.
	Skipped int
}

// ParseFile parses the transcript at path.
func ParseFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads JSONL transcript lines from r.
func Parse(r io.Reader) (*File, error) {
	out := &File{}
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			out.add(line)
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

func (f *File) add(line []byte) {
	var raw rawEntry
	if err := json.Unmarshal(line, &raw); err != nil {
		f.Malformed++
		return
	}
	e, ok := raw.entry()
	if !ok {
		f.Skipped++
		return
	}
	f.Entries = append(f.Entries, e)
}

type rawEntry struct {
	Type        Type   `json:"type"`
	Timestamp   string `json:"timestamp"`
	SessionID   string `json:"sessionId"`
	AgentID     string `json:"agentId"`
	IsSidechain bool   `json:"isSidechain"`
	Cwd         string `json:"cwd"`
	Version     string `json:"version"`

	ID          string `json:"id"`
	QuerySource string `json:"querySource"`

	RequestRef        string      `json:"requestRef"`
	APIRequestID      string      `json:"requestId"`
	IsAPIErrorMessage bool        `json:"isApiErrorMessage"`
	ThinkingMs        int64       `json:"thinkingDurationMs"`
	ToolInputStreamMs int64       `json:"toolInputStreamMs"`
	Message           *rawMessage `json:"message"`

	Origin             *rawOrigin      `json:"origin"`
	PermissionDecision *Permission     `json:"permissionDecision"`
	ToolUseResult      json.RawMessage `json:"toolUseResult"`

	Subtype    Subtype `json:"subtype"`
	DurationMs int64   `json:"durationMs"`

	Operation string `json:"operation"`

	TotalAPIDuration               int64 `json:"totalAPIDuration"`
	TotalAPIDurationWithoutRetries int64 `json:"totalAPIDurationWithoutRetries"`
	TotalToolDuration              int64 `json:"totalToolDuration"`
	TotalDuration                  int64 `json:"totalDuration"`
}

type rawMessage struct {
	Model      string          `json:"model"`
	StopReason string          `json:"stop_reason"`
	Content    json.RawMessage `json:"content"`
}

type rawOrigin struct {
	Kind Origin `json:"kind"`
}

type rawBlock struct {
	Type      BlockType `json:"type"`
	ID        string    `json:"id"`
	ToolUseID string    `json:"tool_use_id"`
	Name      string    `json:"name"`
	Text      string    `json:"text"`
	IsError   bool      `json:"is_error"`
	Input     *struct {
		Command         string `json:"command"`
		RunInBackground bool   `json:"run_in_background"`
	} `json:"input"`
}

type rawToolResult struct {
	Interrupted      bool   `json:"interrupted"`
	BackgroundTaskID string `json:"backgroundTaskId"`
	TimedOutAfterMs  int64  `json:"timedOutAfterMs"`
}

const syntheticModel = "<synthetic>"

var (
	toolUseIDTag = regexp.MustCompile(`<tool-use-id>([^<]+)</tool-use-id>`)
	bashInputTag = regexp.MustCompile(`(?s)^<bash-input>(.*?)</bash-input>`)
)

func (r *rawEntry) entry() (Entry, bool) {
	switch r.Type {
	case TypeAPIRequest, TypeAssistant, TypeUser, TypeSystem, TypeQueueOperation, TypeCostState, TypeSessionOwner:
	default:
		return Entry{}, false
	}
	e := Entry{
		Type:        r.Type,
		SessionID:   r.SessionID,
		AgentID:     r.AgentID,
		IsSidechain: r.IsSidechain,
		Cwd:         r.Cwd,
		Version:     r.Version,
	}
	if r.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil {
			e.Timestamp, e.HasTimestamp = t, true
		}
	}
	switch r.Type {
	case TypeAPIRequest:
		e.RequestID, e.QuerySource = r.ID, r.QuerySource
	case TypeAssistant:
		e.RequestRef, e.APIRequestID = r.RequestRef, r.APIRequestID
		e.ThinkingMs, e.ToolInputStreamMs = r.ThinkingMs, r.ToolInputStreamMs
		if r.Message != nil {
			e.Model, e.StopReason = r.Message.Model, r.Message.StopReason
			e.Blocks, e.Text = decodeContent(r.Message.Content)
		}
		e.IsAPIError = r.IsAPIErrorMessage || e.Model == syntheticModel
	case TypeUser:
		if r.Message != nil {
			e.Blocks, e.Text = decodeContent(r.Message.Content)
		}
		if r.Origin != nil {
			e.Origin = r.Origin.Kind
		}
		e.Permission = r.PermissionDecision
		if len(r.ToolUseResult) > 0 && r.ToolUseResult[0] == '{' {
			var tr rawToolResult
			if json.Unmarshal(r.ToolUseResult, &tr) == nil {
				e.ToolResult = &ToolResult{Interrupted: tr.Interrupted, BackgroundTaskID: tr.BackgroundTaskID, TimedOutAfterMs: tr.TimedOutAfterMs}
			}
		}
		if e.Origin == OriginTaskNotification {
			if m := toolUseIDTag.FindStringSubmatch(e.Text); m != nil {
				e.NotifiedToolUseID = m[1]
			}
		}
		e.Interrupted = strings.HasPrefix(e.Text, "[Request interrupted by user")
		e.LocalInput = isLocalInput(e.Text)
		if m := bashInputTag.FindStringSubmatch(e.Text); m != nil {
			e.LocalCommand = m[1]
		}
		e.LocalOutput = strings.HasPrefix(e.Text, "<bash-stdout>") || strings.HasPrefix(e.Text, "<bash-stderr>") ||
			strings.HasPrefix(e.Text, "<local-command-stdout>") || strings.HasPrefix(e.Text, "<local-command-stderr>")
	case TypeSystem:
		e.Subtype, e.DurationMs = r.Subtype, r.DurationMs
	case TypeQueueOperation:
		e.Operation = r.Operation
	case TypeCostState:
		e.Cost = &CostState{
			APIMs:               r.TotalAPIDuration,
			APIWithoutRetriesMs: r.TotalAPIDurationWithoutRetries,
			ToolMs:              r.TotalToolDuration,
			TotalMs:             r.TotalDuration,
		}
	}
	return e, true
}

// localInputPrefixes open the messages Claude Code writes when the user
// runs a shell command (!) or a slash command.
var localInputPrefixes = []string{"<bash-input>", "<local-command-caveat>", "<command-name>", "<command-message>"}

func isLocalInput(text string) bool {
	for _, p := range localInputPrefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

// decodeContent reads a message's content, which is either a string or an
// array of blocks, returning the blocks and the leading plain text.
func decodeContent(raw json.RawMessage) ([]Block, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil, ""
		}
		return nil, truncate(s)
	}
	var rbs []rawBlock
	if json.Unmarshal(raw, &rbs) != nil {
		return nil, ""
	}
	var blocks []Block
	var text strings.Builder
	for _, rb := range rbs {
		b := Block{Type: rb.Type, Name: rb.Name, IsError: rb.IsError}
		switch rb.Type {
		case BlockToolUse:
			b.ToolUseID = rb.ID
			if rb.Input != nil {
				b.Command, b.RunInBackground = rb.Input.Command, rb.Input.RunInBackground
			}
		case BlockToolResult:
			b.ToolUseID = rb.ToolUseID
		case BlockText:
			if text.Len() < MaxText {
				text.WriteString(rb.Text)
			}
		}
		blocks = append(blocks, b)
	}
	return blocks, truncate(text.String())
}

func truncate(s string) string {
	if len(s) > MaxText {
		return s[:MaxText]
	}
	return s
}

// Compact drops what timing analysis does not read, so a cached copy stays
// small: message text (kept only for API errors, whose text names the
// error) and content blocks other than tool calls and results.
func (f *File) Compact() {
	for i := range f.Entries {
		e := &f.Entries[i]
		if !e.IsAPIError {
			e.Text = ""
		}
		kept := e.Blocks[:0]
		for _, b := range e.Blocks {
			if b.Type == BlockToolUse || b.Type == BlockToolResult {
				kept = append(kept, b)
			}
		}
		if len(kept) == 0 {
			kept = nil
		}
		e.Blocks = kept
	}
}
