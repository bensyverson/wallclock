package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func parse(t *testing.T, lines ...string) *File {
	t.Helper()
	f, err := Parse(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

func only(t *testing.T, f *File) Entry {
	t.Helper()
	if len(f.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(f.Entries))
	}
	return f.Entries[0]
}

func TestAPIRequest(t *testing.T) {
	e := only(t, parse(t, `{"type":"api-request","id":"req-1","querySource":"repl_main_thread","timestamp":"2026-11-13T19:53:11.484Z","sessionId":"s1"}`))
	if e.Type != TypeAPIRequest || e.RequestID != "req-1" || e.QuerySource != "repl_main_thread" {
		t.Errorf("got %+v", e)
	}
	if !e.Timestamp.Equal(ts("2026-11-13T19:53:11.484Z")) {
		t.Errorf("timestamp = %v", e.Timestamp)
	}
}

func TestAssistantToolUse(t *testing.T) {
	e := only(t, parse(t, `{"type":"assistant","requestRef":"req-1","timestamp":"2026-11-13T19:53:14.728Z","cwd":"/r","isSidechain":true,"agentId":"a1","thinkingDurationMs":600,"toolInputStreamMs":941,"message":{"model":"claude-opus-5-5","stop_reason":"tool_use","content":[{"type":"thinking","thinking":"x"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./...","run_in_background":true}}]}}`))
	if e.Type != TypeAssistant || e.RequestRef != "req-1" || e.Cwd != "/r" || !e.IsSidechain || e.AgentID != "a1" {
		t.Errorf("header: %+v", e)
	}
	if e.ThinkingMs != 600 || e.ToolInputStreamMs != 941 || e.StopReason != "tool_use" || e.Model != "claude-opus-5-5" {
		t.Errorf("timing: %+v", e)
	}
	if len(e.Blocks) != 2 {
		t.Fatalf("blocks = %+v", e.Blocks)
	}
	b := e.Blocks[1]
	if b.Type != BlockToolUse || b.ToolUseID != "toolu_1" || b.Name != "Bash" || b.Command != "go test ./..." || !b.RunInBackground {
		t.Errorf("tool_use block = %+v", b)
	}
}

func TestAssistantAPIError(t *testing.T) {
	e := only(t, parse(t, `{"type":"assistant","isApiErrorMessage":true,"timestamp":"2026-11-10T03:26:42.656Z","message":{"model":"<synthetic>","content":[{"type":"text","text":"API Error: Your computer went to sleep mid-response."}]}}`))
	if !e.IsAPIError || !strings.Contains(e.Text, "went to sleep") {
		t.Errorf("got %+v", e)
	}
}

func TestSyntheticModelIsAPIError(t *testing.T) {
	e := only(t, parse(t, `{"type":"assistant","timestamp":"2026-11-10T03:26:42.656Z","message":{"model":"<synthetic>","content":[{"type":"text","text":"Login reached its time limit"}]}}`))
	if !e.IsAPIError {
		t.Errorf("synthetic model entry not flagged as API error: %+v", e)
	}
}

func TestUserToolResult(t *testing.T) {
	e := only(t, parse(t, `{"type":"user","timestamp":"2026-11-13T19:55:29.818Z","permissionDecision":{"decision":"accept","source":"user_temporary"},"toolUseResult":{"interrupted":true,"backgroundTaskId":"bucwz","timedOutAfterMs":120000},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","is_error":true,"content":"x"}]}}`))
	if e.Type != TypeUser || e.Permission == nil || e.Permission.Source != "user_temporary" || e.Permission.Decision != "accept" {
		t.Errorf("permission: %+v", e.Permission)
	}
	if e.ToolResult == nil || !e.ToolResult.Interrupted || e.ToolResult.BackgroundTaskID != "bucwz" || e.ToolResult.TimedOutAfterMs != 120000 {
		t.Errorf("tool result: %+v", e.ToolResult)
	}
	if len(e.Blocks) != 1 || e.Blocks[0].Type != BlockToolResult || e.Blocks[0].ToolUseID != "toolu_1" || !e.Blocks[0].IsError {
		t.Errorf("blocks: %+v", e.Blocks)
	}
}

func TestUserHumanPrompt(t *testing.T) {
	e := only(t, parse(t, `{"type":"user","timestamp":"2026-11-13T19:53:11.130Z","origin":{"kind":"human"},"message":{"role":"user","content":"Can you triage this?"}}`))
	if e.Origin != OriginHuman || e.Text != "Can you triage this?" {
		t.Errorf("got %+v", e)
	}
}

func TestUserTaskNotification(t *testing.T) {
	e := only(t, parse(t, `{"type":"user","timestamp":"2026-11-13T19:56:47.721Z","origin":{"kind":"task-notification","producer":"session-task"},"message":{"role":"user","content":"<task-notification>\n<task-id>ac6fb</task-id>\n<tool-use-id>toolu_9</tool-use-id>\n<status>completed</status>\n</task-notification>"}}`))
	if e.Origin != OriginTaskNotification || e.NotifiedToolUseID != "toolu_9" {
		t.Errorf("got origin=%q notified=%q", e.Origin, e.NotifiedToolUseID)
	}
}

func TestUserInterruptMarker(t *testing.T) {
	e := only(t, parse(t, `{"type":"user","timestamp":"2026-11-13T19:56:47.721Z","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}}`))
	if !e.Interrupted {
		t.Errorf("interrupt marker not detected: %+v", e)
	}
}

func TestSystemEntries(t *testing.T) {
	f := parse(t,
		`{"type":"system","subtype":"turn_duration","durationMs":94783,"timestamp":"2026-11-13T22:58:19.398Z"}`,
		`{"type":"system","subtype":"away_summary","content":"We were...","timestamp":"2026-11-08T15:32:23.376Z"}`,
	)
	if len(f.Entries) != 2 {
		t.Fatalf("entries = %d", len(f.Entries))
	}
	if e := f.Entries[0]; e.Type != TypeSystem || e.Subtype != SubtypeTurnDuration || e.DurationMs != 94783 {
		t.Errorf("turn_duration: %+v", e)
	}
	if e := f.Entries[1]; e.Subtype != SubtypeAwaySummary {
		t.Errorf("away_summary: %+v", e)
	}
}

func TestCostState(t *testing.T) {
	e := only(t, parse(t, `{"type":"cost-state","startTime":1763060000000,"totalAPIDuration":1000,"totalAPIDurationWithoutRetries":900,"totalToolDuration":500,"totalDuration":3000}`))
	if e.Type != TypeCostState || e.Cost == nil || e.Cost.APIMs != 1000 || e.Cost.APIWithoutRetriesMs != 900 || e.Cost.ToolMs != 500 || e.Cost.TotalMs != 3000 {
		t.Errorf("got %+v", e.Cost)
	}
}

func TestSessionHeaderMarksProcessStart(t *testing.T) {
	f := parse(t, `{"type":"session-owner","sessionId":"s1","accountUuid":"x"}`)
	if e := only(t, f); e.Type != TypeSessionOwner {
		t.Errorf("got %+v", e)
	}
}

func TestUnknownTypesAreSkippedAndMalformedLinesCounted(t *testing.T) {
	f := parse(t,
		`{"type":"atis-latch","atis":1}`,
		`{"type":"file-history-snapshot"}`,
		`not json at all`,
		``,
		`{"type":"system","subtype":"turn_duration","durationMs":1,"timestamp":"2026-11-13T22:58:19.398Z"}`,
	)
	if len(f.Entries) != 1 {
		t.Errorf("entries = %d, want 1 (unknown types skipped)", len(f.Entries))
	}
	if f.Malformed != 1 {
		t.Errorf("malformed = %d, want 1", f.Malformed)
	}
	if f.Skipped != 2 {
		t.Errorf("skipped = %d, want 2", f.Skipped)
	}
}

func TestLongLinesParse(t *testing.T) {
	big := strings.Repeat("x", 3<<20)
	e := only(t, parse(t, `{"type":"user","timestamp":"2026-11-13T19:53:11.130Z","origin":{"kind":"human"},"message":{"role":"user","content":"`+big+`"}}`))
	if e.Origin != OriginHuman {
		t.Errorf("got %+v", e.Origin)
	}
	if len(e.Text) > 4096 {
		t.Errorf("text kept %d bytes; want it truncated", len(e.Text))
	}
}

func TestDiscoverSessions(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-Users-ben-git-x")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(proj, "s1", "subagents"), 0o755))
	must(os.MkdirAll(filepath.Join(proj, "memory"), 0o755))
	for _, p := range []string{
		filepath.Join(proj, "s1.jsonl"),
		filepath.Join(proj, "s2.jsonl"),
		filepath.Join(proj, "s1", "subagents", "agent-a1.jsonl"),
		filepath.Join(proj, "s1", "subagents", "agent-a2.jsonl"),
		filepath.Join(proj, "s1", "subagents", "agent-a2.meta.json"),
		filepath.Join(proj, "memory", "MEMORY.md"),
	} {
		must(os.WriteFile(p, []byte("{}\n"), 0o644))
	}
	sessions, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %+v", sessions)
	}
	s1 := sessions[0]
	if s1.ID != "s1" || s1.Path != filepath.Join(proj, "s1.jsonl") || s1.ProjectDir != proj {
		t.Errorf("s1 = %+v", s1)
	}
	if len(s1.Subagents) != 2 || s1.Subagents[0].AgentID != "a1" || s1.Subagents[1].Path != filepath.Join(proj, "s1", "subagents", "agent-a2.jsonl") {
		t.Errorf("subagents = %+v", s1.Subagents)
	}
	if len(sessions[1].Subagents) != 0 {
		t.Errorf("s2 subagents = %+v", sessions[1].Subagents)
	}
}

func TestCompactDropsWhatTimingDoesNotUse(t *testing.T) {
	f := parse(t,
		`{"type":"user","timestamp":"2026-11-13T19:53:11.130Z","origin":{"kind":"human"},"message":{"role":"user","content":"a long prompt"}}`,
		`{"type":"assistant","requestRef":"r","timestamp":"2026-11-13T19:53:12Z","message":{"content":[{"type":"text","text":"hello"},{"type":"thinking"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test"}}]}}`,
		`{"type":"assistant","isApiErrorMessage":true,"timestamp":"2026-11-13T19:53:13Z","message":{"model":"<synthetic>","content":[{"type":"text","text":"API Error: 529 Overloaded"}]}}`,
	)
	f.Compact()
	if f.Entries[0].Text != "" {
		t.Errorf("prompt text kept: %q", f.Entries[0].Text)
	}
	if bs := f.Entries[1].Blocks; len(bs) != 1 || bs[0].Type != BlockToolUse || bs[0].Command != "go test" {
		t.Errorf("blocks = %+v, want only the tool_use", bs)
	}
	if f.Entries[2].Text != "API Error: 529 Overloaded" {
		t.Errorf("api error text dropped: %q", f.Entries[2].Text)
	}
}
