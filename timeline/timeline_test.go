package timeline

import (
	"strings"
	"testing"
	"time"

	tr "github.com/bensyverson/wallclock/transcript"
)

var base = time.Date(2026, 11, 13, 19, 0, 0, 0, time.UTC)

func at(sec float64) time.Time { return base.Add(time.Duration(sec * float64(time.Second))) }

func req(sec float64, id string) tr.Entry {
	return tr.Entry{Type: tr.TypeAPIRequest, Timestamp: at(sec), HasTimestamp: true, RequestID: id}
}

func asst(sec float64, ref string, blocks ...tr.Block) tr.Entry {
	return tr.Entry{Type: tr.TypeAssistant, Timestamp: at(sec), HasTimestamp: true, RequestRef: ref, Blocks: blocks}
}

func apiErr(sec float64, ref, text string) tr.Entry {
	e := asst(sec, ref, tr.Block{Type: tr.BlockText})
	e.IsAPIError, e.Text = true, text
	return e
}

func bash(id, cmd string) tr.Block {
	return tr.Block{Type: tr.BlockToolUse, ToolUseID: id, Name: "Bash", Command: cmd}
}

func bgBash(id, cmd string) tr.Block {
	b := bash(id, cmd)
	b.RunInBackground = true
	return b
}

func tool(id, name string) tr.Block {
	return tr.Block{Type: tr.BlockToolUse, ToolUseID: id, Name: name}
}

func text() tr.Block { return tr.Block{Type: tr.BlockText} }

func result(sec float64, id string) tr.Entry {
	return tr.Entry{Type: tr.TypeUser, Timestamp: at(sec), HasTimestamp: true,
		Blocks:     []tr.Block{{Type: tr.BlockToolResult, ToolUseID: id}},
		Permission: &tr.Permission{Decision: "accept", Source: "config"}}
}

func resultBy(sec float64, id, decision, source string) tr.Entry {
	e := result(sec, id)
	e.Permission = &tr.Permission{Decision: decision, Source: source}
	return e
}

func human(sec float64) tr.Entry {
	return tr.Entry{Type: tr.TypeUser, Timestamp: at(sec), HasTimestamp: true, Origin: tr.OriginHuman, Text: "hi"}
}

func notify(sec float64, toolUseID string) tr.Entry {
	return tr.Entry{Type: tr.TypeUser, Timestamp: at(sec), HasTimestamp: true, Origin: tr.OriginTaskNotification, NotifiedToolUseID: toolUseID}
}

func stopHook(sec float64) tr.Entry {
	return tr.Entry{Type: tr.TypeSystem, Timestamp: at(sec), HasTimestamp: true, Subtype: tr.SubtypeStopHookSummary}
}

func interrupt(sec float64) tr.Entry {
	return tr.Entry{Type: tr.TypeUser, Timestamp: at(sec), HasTimestamp: true, Interrupted: true, Text: "[Request interrupted by user]"}
}

// classifyStub types commands by their first word, standing in for package classify.
func classifyStub(cmd string) string {
	switch {
	case strings.HasPrefix(cmd, "go test"):
		return "test"
	case strings.HasPrefix(cmd, "go build"):
		return "build"
	}
	return "bash"
}

func build(t *testing.T, entries ...tr.Entry) Timeline {
	t.Helper()
	tl := Build(entries, Options{Away: 10 * time.Minute, Suspend: time.Hour, Classify: classifyStub})
	checkContiguous(t, tl, entries)
	return tl
}

// checkContiguous asserts the invariant every timeline must hold: intervals
// are ordered, non-overlapping, non-empty, and exactly cover the span from
// the first to the last timestamp.
func checkContiguous(t *testing.T, tl Timeline, entries []tr.Entry) {
	t.Helper()
	var first, last time.Time
	for _, e := range entries {
		if !e.HasTimestamp {
			continue
		}
		if first.IsZero() || e.Timestamp.Before(first) {
			first = e.Timestamp
		}
		if e.Timestamp.After(last) {
			last = e.Timestamp
		}
	}
	if len(tl.Intervals) == 0 {
		if !first.Equal(last) {
			t.Fatalf("no intervals for span %v..%v", first, last)
		}
		return
	}
	if !tl.Intervals[0].Start.Equal(first) {
		t.Errorf("first interval starts %v, span starts %v", tl.Intervals[0].Start, first)
	}
	if end := tl.Intervals[len(tl.Intervals)-1].End; !end.Equal(last) {
		t.Errorf("last interval ends %v, span ends %v", end, last)
	}
	for i, iv := range tl.Intervals {
		if !iv.End.After(iv.Start) {
			t.Errorf("interval %d empty or reversed: %+v", i, iv)
		}
		if i > 0 && !iv.Start.Equal(tl.Intervals[i-1].End) {
			t.Errorf("interval %d starts %v, previous ends %v", i, iv.Start, tl.Intervals[i-1].End)
		}
		if i > 0 && iv.Bucket == tl.Intervals[i-1].Bucket && iv.Kind == tl.Intervals[i-1].Kind {
			t.Errorf("intervals %d and %d share bucket %s/%s and should be merged", i-1, i, iv.Bucket, iv.Kind)
		}
	}
}

type want struct {
	bucket Bucket
	kind   string
	from   float64
	to     float64
}

func expect(t *testing.T, tl Timeline, ws ...want) {
	t.Helper()
	if len(tl.Intervals) != len(ws) {
		t.Errorf("got %d intervals, want %d:\n%s", len(tl.Intervals), len(ws), dump(tl))
		return
	}
	for i, w := range ws {
		iv := tl.Intervals[i]
		if iv.Bucket != w.bucket || iv.Kind != w.kind || !iv.Start.Equal(at(w.from)) || !iv.End.Equal(at(w.to)) {
			t.Errorf("interval %d = %s/%s %v..%v, want %s/%s %v..%v\n%s", i, iv.Bucket, iv.Kind,
				iv.Start.Sub(base).Seconds(), iv.End.Sub(base).Seconds(), w.bucket, w.kind, w.from, w.to, dump(tl))
		}
	}
}

func dump(tl Timeline) string {
	var b strings.Builder
	for _, iv := range tl.Intervals {
		b.WriteString("  " + string(iv.Bucket) + "/" + iv.Kind + " " +
			iv.Start.Sub(base).String() + ".." + iv.End.Sub(base).String() + "\n")
	}
	return b.String()
}

func TestPromptThenResponse(t *testing.T) {
	tl := build(t, human(0), req(0.5, "r1"), asst(1, "r1", tr.Block{Type: tr.BlockThinking}), asst(3, "r1", text()))
	expect(t, tl,
		want{BucketHarness, "", 0, 0.5},
		want{BucketAPI, "", 0.5, 3},
	)
}

func TestToolCallTypedByCommand(t *testing.T) {
	tl := build(t,
		req(0, "r1"), asst(2, "r1", bash("t1", "go test ./...")),
		result(10, "t1"),
		req(10.5, "r2"), asst(12, "r2", text()),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketTool, "test", 2, 10},
		want{BucketHarness, "", 10, 10.5},
		want{BucketAPI, "", 10.5, 12},
	)
}

func TestNonBashToolsTypedByName(t *testing.T) {
	cases := map[string]string{
		"Read": "file", "Edit": "file", "Write": "file", "Glob": "file", "Grep": "file", "NotebookEdit": "file",
		"WebFetch": "web", "WebSearch": "web",
		"mcp__claude_ai_Gmail__search": "mcp",
		"Skill":                        "other", "ToolSearch": "other",
	}
	for name, kind := range cases {
		tl := build(t, req(0, "r1"), asst(1, "r1", tool("t1", name)), result(4, "t1"))
		expect(t, tl, want{BucketAPI, "", 0, 1}, want{BucketTool, kind, 1, 4})
	}
}

func TestUserGapCappedWithAwayRemainder(t *testing.T) {
	tl := build(t,
		req(0, "r1"), asst(2, "r1", text()),
		stopHook(3),
		human(3+20*60),
		req(3+20*60+0.5, "r2"), asst(3+20*60+2, "r2", text()),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketHook, "", 2, 3},
		want{BucketUser, "", 3, 3 + 10*60},
		want{BucketAway, "", 3 + 10*60, 3 + 20*60},
		want{BucketHarness, "", 3 + 20*60, 3 + 20*60 + 0.5},
		want{BucketAPI, "", 3 + 20*60 + 0.5, 3 + 20*60 + 2},
	)
	if n := countEvents(tl, EventAbsence); n != 1 {
		t.Errorf("absence events = %d, want 1", n)
	}
}

func TestShortUserGapCountsInFull(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(2, "r1", text()), human(62), req(62.5, "r2"), asst(64, "r2", text()))
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketUser, "", 2, 62},
		want{BucketHarness, "", 62, 62.5},
		want{BucketAPI, "", 62.5, 64},
	)
	if n := countEvents(tl, EventAbsence); n != 0 {
		t.Errorf("absence events = %d, want 0", n)
	}
}

func TestPersonApprovedToolIsApproval(t *testing.T) {
	for _, src := range []string{"user_temporary", "user_permanent"} {
		tl := build(t, req(0, "r1"), asst(2, "r1", bash("t1", "rm -rf build")), resultBy(30, "t1", "accept", src))
		expect(t, tl, want{BucketAPI, "", 0, 2}, want{BucketApproval, "", 2, 30})
	}
}

func TestRejectedToolIsApproval(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(2, "r1", bash("t1", "rm -rf /")), resultBy(9, "t1", "reject", "user_reject"))
	expect(t, tl, want{BucketAPI, "", 0, 2}, want{BucketApproval, "", 2, 9})
}

func TestLongApprovalCappedWithAwayRemainder(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(2, "r1", bash("t1", "make")), resultBy(2+15*60, "t1", "accept", "user_temporary"))
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketApproval, "", 2, 2 + 10*60},
		want{BucketAway, "", 2 + 10*60, 2 + 15*60},
	)
}

func TestQuestionToolIsUserWait(t *testing.T) {
	for _, name := range []string{"AskUserQuestion", "ExitPlanMode"} {
		tl := build(t, req(0, "r1"), asst(2, "r1", tool("t1", name)), resultBy(2+12*60, "t1", "accept", "user_temporary"))
		expect(t, tl,
			want{BucketAPI, "", 0, 2},
			want{BucketUser, "", 2, 2 + 10*60},
			want{BucketAway, "", 2 + 10*60, 2 + 12*60},
		)
	}
}

func TestParallelToolsCountTheSlowest(t *testing.T) {
	tl := build(t,
		req(0, "r1"),
		asst(2, "r1", tool("a", "Read")),
		asst(2.5, "r1", bash("b", "go build ./...")),
		result(4, "a"),
		result(9, "b"),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 2.5},
		want{BucketTool, "build", 2.5, 9},
	)
}

func TestToolStartedDuringStreamingYieldsToAPI(t *testing.T) {
	// Claude Code can start a tool before the response finishes streaming.
	tl := build(t, req(0, "r1"), asst(1, "r1", tool("a", "Read")), asst(3, "r1", text()), result(2, "a"))
	expect(t, tl, want{BucketAPI, "", 0, 3})
}

func TestBackgroundCommandWakeup(t *testing.T) {
	tl := build(t,
		req(0, "r1"), asst(2, "r1", bgBash("b1", "go test ./...")),
		result(2.2, "b1"),
		req(2.3, "r2"), asst(4, "r2", text()),
		notify(60, "b1"),
		req(60.2, "r3"), asst(62, "r3", text()),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketTool, "test", 2, 2.2},
		want{BucketHarness, "", 2.2, 2.3},
		want{BucketAPI, "", 2.3, 4},
		want{BucketBackground, "test", 4, 60},
		want{BucketHarness, "", 60, 60.2},
		want{BucketAPI, "", 60.2, 62},
	)
}

func TestSubagentWakeupIsIdle(t *testing.T) {
	tl := build(t,
		req(0, "r1"), asst(2, "r1", tool("ag", "Agent")),
		result(2.5, "ag"),
		req(2.6, "r2"), asst(4, "r2", text()),
		notify(300, "ag"),
		req(300.1, "r3"), asst(302, "r3", text()),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketIdle, "", 2, 2.5},
		want{BucketHarness, "", 2.5, 2.6},
		want{BucketAPI, "", 2.6, 4},
		want{BucketIdle, "", 4, 300},
		want{BucketHarness, "", 300, 300.1},
		want{BucketAPI, "", 300.1, 302},
	)
}

func TestForegroundAgentToolIsIdle(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(2, "r1", tool("ag", "Agent")), result(100, "ag"))
	expect(t, tl, want{BucketAPI, "", 0, 2}, want{BucketIdle, "", 2, 100})
}

func TestAPIErrorBucketAndEvent(t *testing.T) {
	tl := build(t, req(0, "r1"), apiErr(5, "r1", "API Error: 529 Overloaded"))
	expect(t, tl, want{BucketAPIError, "overloaded", 0, 5})
	evs := eventsOf(tl, EventAPIError)
	if len(evs) != 1 || evs[0].Kind != "overloaded" {
		t.Errorf("api error events = %+v", evs)
	}
}

func TestAPIErrorKinds(t *testing.T) {
	cases := map[string]string{
		"API Error: 529 Overloaded":                        "overloaded",
		"API Error: 429 rate limit exceeded":               "rate_limit",
		"API Error: Request timed out":                     "timeout",
		"Login reached its time limit · Please run /login": "auth",
		"API Error: 500 Internal server error":             "server",
		"API Error: Connection error":                      "network",
		"something else entirely":                          "other",
	}
	for text, kind := range cases {
		if got := ErrorKind(text); got != kind {
			t.Errorf("ErrorKind(%q) = %q, want %q", text, got, kind)
		}
	}
}

func TestSleepErrorIsSuspended(t *testing.T) {
	tl := build(t, req(0, "r1"), apiErr(4000, "r1", "API Error: Your computer went to sleep mid-response."))
	expect(t, tl, want{BucketSuspended, "", 0, 4000})
	if n := countEvents(tl, EventSuspension); n != 1 {
		t.Errorf("suspension events = %d, want 1", n)
	}
}

func TestOverlongToolIsSuspended(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(2, "r1", bash("t1", "go test ./...")), result(2+2*3600, "t1"))
	expect(t, tl, want{BucketAPI, "", 0, 2}, want{BucketSuspended, "", 2, 2 + 2*3600})
	if n := countEvents(tl, EventSuspension); n != 1 {
		t.Errorf("suspension events = %d, want 1", n)
	}
}

func TestUnfinishedToolThenLateResume(t *testing.T) {
	// The session died mid-command; the user resumed it a day later.
	tl := build(t,
		req(0, "r1"), asst(2, "r1", bash("t1", "go test ./...")),
		human(2+86400),
		req(2+86400+0.5, "r2"), asst(2+86400+2, "r2", text()),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketUser, "", 2, 2 + 10*60},
		want{BucketAway, "", 2 + 10*60, 2 + 86400},
		want{BucketHarness, "", 2 + 86400, 2 + 86400 + 0.5},
		want{BucketAPI, "", 2 + 86400 + 0.5, 2 + 86400 + 2},
	)
}

func TestInterruptionEvent(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(2, "r1", bash("t1", "go test ./...")), interrupt(5), result(5, "t1"))
	if n := countEvents(tl, EventInterruption); n != 1 {
		t.Errorf("interruption events = %d, want 1", n)
	}
}

func TestOutOfOrderEntriesAreSorted(t *testing.T) {
	tl := build(t, asst(2, "r1", text()), req(0, "r1"))
	expect(t, tl, want{BucketAPI, "", 0, 2})
}

func TestRequestWithoutResponseLeavesGap(t *testing.T) {
	tl := build(t, req(0, "r1"), human(30), req(30.5, "r2"), asst(32, "r2", text()))
	expect(t, tl,
		want{BucketUser, "", 0, 30},
		want{BucketHarness, "", 30, 30.5},
		want{BucketAPI, "", 30.5, 32},
	)
}

func TestEmptyAndSingleEntry(t *testing.T) {
	if tl := build(t); len(tl.Intervals) != 0 {
		t.Errorf("empty: %+v", tl)
	}
	if tl := build(t, human(0)); len(tl.Intervals) != 0 {
		t.Errorf("single: %+v", tl)
	}
}

func TestEntriesWithoutTimestampsIgnored(t *testing.T) {
	tl := build(t, req(0, "r1"), tr.Entry{Type: tr.TypeCostState}, asst(2, "r1", text()))
	expect(t, tl, want{BucketAPI, "", 0, 2})
}

func countEvents(tl Timeline, typ EventType) int { return len(eventsOf(tl, typ)) }

func eventsOf(tl Timeline, typ EventType) []Event {
	var out []Event
	for _, e := range tl.Events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func turnEnd(sec float64) tr.Entry {
	return tr.Entry{Type: tr.TypeSystem, Timestamp: at(sec), HasTimestamp: true, Subtype: tr.SubtypeTurnDuration}
}

func sysEntry(sec float64, sub tr.Subtype) tr.Entry {
	return tr.Entry{Type: tr.TypeSystem, Timestamp: at(sec), HasTimestamp: true, Subtype: sub}
}

func userText(sec float64, text string) tr.Entry {
	return tr.Entry{Type: tr.TypeUser, Timestamp: at(sec), HasTimestamp: true, Text: text, LocalInput: true}
}

// Regression: a session left idle after its last turn ended in harness
// time when no prompt followed (found in real transcripts, 2026-11-19).
func TestIdleAfterTurnEndWithoutPromptIsUserThenAway(t *testing.T) {
	tl := build(t,
		req(0, "r1"), asst(2, "r1", text()),
		stopHook(2.1), turnEnd(2.2),
		sysEntry(180, tr.SubtypeAwaySummary),
		sysEntry(3*86400, "informational"),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketHook, "", 2, 2.1},
		want{BucketHarness, "", 2.1, 2.2},
		want{BucketUser, "", 2.2, 2.2 + 10*60},
		want{BucketAway, "", 2.2 + 10*60, 3 * 86400},
	)
}

// Regression: a user's own shell command or slash command carries no
// human origin but still ends a wait on the user.
func TestShellAndSlashInputEndUserWait(t *testing.T) {
	for _, input := range []string{"<bash-input>scripts/deploy.sh</bash-input>", "<local-command-caveat>The command below…", "<command-name>/clear</command-name>"} {
		tl := build(t,
			req(0, "r1"), asst(2, "r1", text()), stopHook(2.1), turnEnd(2.2),
			userText(122.2, input),
			req(130, "r2"), asst(132, "r2", text()),
		)
		expect(t, tl,
			want{BucketAPI, "", 0, 2},
			want{BucketHook, "", 2, 2.1},
			want{BucketHarness, "", 2.1, 2.2},
			want{BucketUser, "", 2.2, 122.2},
			want{BucketHarness, "", 122.2, 130},
			want{BucketAPI, "", 130, 132},
		)
	}
}

func TestNotificationAfterTurnEndIsStillBackground(t *testing.T) {
	tl := build(t,
		req(0, "r1"), asst(2, "r1", bgBash("b1", "go test ./...")), result(2.2, "b1"),
		req(2.3, "r2"), asst(4, "r2", text()), turnEnd(4.1),
		notify(60, "b1"),
		req(60.2, "r3"), asst(62, "r3", text()),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketTool, "test", 2, 2.2},
		want{BucketHarness, "", 2.2, 2.3},
		want{BucketAPI, "", 2.3, 4},
		want{BucketHarness, "", 4, 4.1},
		want{BucketBackground, "test", 4.1, 60},
		want{BucketHarness, "", 60, 60.2},
		want{BucketAPI, "", 60.2, 62},
	)
}

func localCmd(sec float64, cmd string) tr.Entry {
	e := userText(sec, "<bash-input>"+cmd+"</bash-input>")
	e.LocalCommand = cmd
	return e
}

func localOut(sec float64) tr.Entry {
	return tr.Entry{Type: tr.TypeUser, Timestamp: at(sec), HasTimestamp: true, LocalOutput: true}
}

// Regression: after Esc, the thread waits on the user, not the harness.
func TestInterruptionEndsTurn(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(2, "r1", text()), req(3, "r2"), interrupt(4), sysEntry(700, tr.SubtypeAwaySummary))
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketHarness, "", 2, 4},
		want{BucketUser, "", 4, 604},
		want{BucketAway, "", 604, 700},
	)
}

// Regression: a request that got no response and was re-sent was a wait
// on Anthropic (a 10-minute timeout in real transcripts).
func TestUnansweredRequestRetriedIsAPIError(t *testing.T) {
	tl := build(t, req(0, "r1"), req(600, "r2"), asst(610, "r2", text()))
	expect(t, tl,
		want{BucketAPIError, "retry", 0, 600},
		want{BucketAPI, "", 600, 610},
	)
	if evs := eventsOf(tl, EventAPIError); len(evs) != 1 || evs[0].Kind != "retry" {
		t.Errorf("events = %+v", evs)
	}
}

// Regression: a user's own shell command is a wait on the computer.
func TestUserShellCommandIsTool(t *testing.T) {
	tl := build(t,
		req(0, "r1"), asst(2, "r1", text()), turnEnd(2.5),
		localCmd(32.5, "go test ./..."), localOut(62.5),
		req(63, "r2"), asst(65, "r2", text()),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 2},
		want{BucketHarness, "", 2, 2.5},
		want{BucketUser, "", 2.5, 32.5},
		want{BucketTool, "test", 32.5, 62.5},
		want{BucketHarness, "", 62.5, 63},
		want{BucketAPI, "", 63, 65},
	)
}

// Regression: an API error with no request (an expired login) ends the turn
// and is counted.
func TestUnrequestedAPIErrorEndsTurn(t *testing.T) {
	tl := build(t, human(0), apiErr(0.5, "", "Login reached its time limit · Please run /login"), req(66, "r1"), asst(70, "r1", text()))
	expect(t, tl,
		want{BucketHarness, "", 0, 0.5},
		want{BucketUser, "", 0.5, 66},
		want{BucketAPI, "", 66, 70},
	)
	if evs := eventsOf(tl, EventAPIError); len(evs) != 1 || evs[0].Kind != "auth" {
		t.Errorf("events = %+v", evs)
	}
}

// Regression: Claude Code often supersedes a request within a second; that
// is part of the API wait, not a retry (74 of 76 "retries" in real data).
func TestQuicklySupersededRequestIsAPI(t *testing.T) {
	tl := build(t, req(0, "r1"), req(0.4, "r2"), asst(3, "r2", text()))
	expect(t, tl, want{BucketAPI, "", 0, 3})
	if n := countEvents(tl, EventAPIError); n != 0 {
		t.Errorf("api error events = %d, want 0", n)
	}
}

func legacyAsst(sec float64, requestID string, blocks ...tr.Block) tr.Entry {
	e := asst(sec, "", blocks...)
	e.APIRequestID = requestID
	return e
}

// Regression: Claude Code before ~2.1.3xx logged no api-request entries, so
// older sessions reported zero API time (found by the cost-state cross-check).
func TestLegacyThreadWithoutRequestEntries(t *testing.T) {
	tl := build(t,
		human(0),
		legacyAsst(3, "A", tool("t1", "Read")),
		result(4, "t1"),
		legacyAsst(5, "A", bash("t2", "go test ./...")),
		result(15, "t2"),
		legacyAsst(18, "B", text()),
	)
	expect(t, tl,
		want{BucketAPI, "", 0, 5},
		want{BucketTool, "test", 5, 15},
		want{BucketAPI, "", 15, 18},
	)
}
