// Package transcript reads Claude Code session transcripts (JSONL) into
// typed entries. The format is undocumented and changes between Claude Code
// versions, so parsing keeps only the fields wallclock needs and tolerates
// unknown entry types, unknown fields and malformed lines.
package transcript

import "time"

// Type is a transcript entry's top-level "type".
type Type string

const (
	TypeAPIRequest     Type = "api-request"
	TypeAssistant      Type = "assistant"
	TypeUser           Type = "user"
	TypeSystem         Type = "system"
	TypeQueueOperation Type = "queue-operation"
	TypeCostState      Type = "cost-state"
	// TypeSessionOwner is written when a Claude Code process attaches to a
	// session, so repeats mark a resume.
	TypeSessionOwner Type = "session-owner"
)

// Subtype is a system entry's "subtype".
type Subtype string

const (
	SubtypeTurnDuration    Subtype = "turn_duration"
	SubtypeStopHookSummary Subtype = "stop_hook_summary"
	SubtypeAwaySummary     Subtype = "away_summary"
	SubtypeCompactBoundary Subtype = "compact_boundary"
)

// Origin says what started a user turn.
type Origin string

const (
	OriginNone             Origin = ""
	OriginHuman            Origin = "human"
	OriginTaskNotification Origin = "task-notification"
	OriginPeer             Origin = "peer"
	OriginCoordinator      Origin = "coordinator"
)

// BlockType is a message content block's type.
type BlockType string

const (
	BlockText       BlockType = "text"
	BlockThinking   BlockType = "thinking"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
)

// Entry is one transcript line, reduced to the fields wallclock uses.
// Fields that don't apply to an entry's Type are zero.
type Entry struct {
	Type         Type
	Timestamp    time.Time
	HasTimestamp bool
	SessionID    string
	AgentID      string
	IsSidechain  bool
	Cwd          string
	Version      string

	// api-request
	RequestID   string
	QuerySource string

	// assistant
	RequestRef        string
	Model             string
	StopReason        string
	IsAPIError        bool
	ThinkingMs        int64
	ToolInputStreamMs int64

	// assistant and user
	Blocks []Block
	// Text is the entry's plain text (string content or text blocks),
	// truncated to MaxText bytes.
	Text string

	// user
	Origin            Origin
	Permission        *Permission
	ToolResult        *ToolResult
	NotifiedToolUseID string
	Interrupted       bool

	// system
	Subtype    Subtype
	DurationMs int64

	// queue-operation
	Operation string

	// cost-state
	Cost *CostState
}

// MaxText bounds Entry.Text; wallclock only inspects the start of a message.
const MaxText = 4096

// Block is one content block of a message.
type Block struct {
	Type BlockType
	// ToolUseID is the tool_use's id, or the id a tool_result answers.
	ToolUseID string
	// Name is the tool's name, on tool_use blocks.
	Name string
	// Command is a Bash tool_use's command line.
	Command         string
	RunInBackground bool
	IsError         bool
}

// Permission records who allowed or refused a tool call.
type Permission struct {
	Decision string
	// Source is "config" when a rule, classifier or mode decided, and
	// "user_*" when a person did.
	Source string
}

// ByPerson reports whether a person, not configuration, made the decision.
func (p *Permission) ByPerson() bool {
	return p != nil && len(p.Source) >= 5 && p.Source[:5] == "user_"
}

// ToolResult carries the parts of toolUseResult that bear on timing.
type ToolResult struct {
	Interrupted      bool
	BackgroundTaskID string
	TimedOutAfterMs  int64
}

// CostState is Claude Code's own running totals for a session, used to
// cross-check wallclock's numbers.
type CostState struct {
	APIMs               int64
	APIWithoutRetriesMs int64
	ToolMs              int64
	TotalMs             int64
}
