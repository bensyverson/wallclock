package timeline

import (
	"fmt"
	"slices"
	"testing"
	"time"

	tr "github.com/bensyverson/wallclock/transcript"
)

func turnDur(sec float64) tr.Entry {
	return tr.Entry{Type: tr.TypeSystem, Timestamp: at(sec), HasTimestamp: true, Subtype: tr.SubtypeTurnDuration}
}

const threeDays = 3 * 24 * 3600.0

// noLongActive fails if any active interval runs longer than an hour.
func noLongActive(t *testing.T, tl Timeline) {
	t.Helper()
	for _, iv := range tl.Intervals {
		if iv.Bucket.Active() && iv.Duration() > time.Hour {
			t.Errorf("%v of %s counted as active", iv.Duration(), iv.Bucket)
		}
	}
}

func suspensionKinds(tl Timeline) []string {
	var kinds []string
	for _, ev := range tl.Events {
		if ev.Type == EventSuspension {
			kinds = append(kinds, ev.Kind)
		}
	}
	return kinds
}

func wantSuspension(t *testing.T, tl Timeline, kind string) {
	t.Helper()
	if slices.Contains(suspensionKinds(tl), kind) {
		return
	}
	t.Errorf("want a %q suspension event, got %v", kind, suspensionKinds(tl))
}

func TestHarnessGapBeforeLateInterruptIsSuspended(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(5, "r1", text()), interrupt(threeDays))
	noLongActive(t, tl)
	wantSuspension(t, tl, "harness")
}

func TestHarnessGapBeforeLateTurnDurationIsSuspended(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(5, "r1", text()), turnDur(threeDays))
	noLongActive(t, tl)
	wantSuspension(t, tl, "harness")
}

func TestPlainMidTurnGapOfDaysIsSuspended(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(5, "r1", text()), req(threeDays, "r2"), asst(threeDays+5, "r2", text()))
	noLongActive(t, tl)
	wantSuspension(t, tl, "harness")
}

func TestHarnessGapUnderAwayStaysHarness(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(5, "r1", text()), turnDur(65))
	if got := sumBucket(tl, BucketHarness); got != 60*time.Second {
		t.Errorf("harness = %v, want 60s", got)
	}
	if k := suspensionKinds(tl); len(k) != 0 {
		t.Errorf("unexpected suspension events %v", k)
	}
}

func TestHookGapOfDaysIsSuspended(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(5, "r1", text()), stopHook(threeDays))
	noLongActive(t, tl)
	wantSuspension(t, tl, "hook")
}

func TestShortHookGapStaysHook(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(5, "r1", text()), stopHook(35))
	if got := sumBucket(tl, BucketHook); got != 30*time.Second {
		t.Errorf("hook = %v, want 30s", got)
	}
}

func sumBucket(tl Timeline, b Bucket) time.Duration {
	var d time.Duration
	for _, iv := range tl.Intervals {
		if iv.Bucket == b {
			d += iv.Duration()
		}
	}
	return d
}

// A turn_duration stamped at the same millisecond as the last assistant
// block must still end the turn, so the wait after it is the user's.
func TestEntryAtGapStartIsConsidered(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(5, "r1", text()), turnDur(5), req(60, "r2"), asst(61, "r2", text()))
	if got := sumBucket(tl, BucketUser); got != 55*time.Second {
		t.Errorf("user = %v, want 55s (intervals %+v)", got, tl.Intervals)
	}
	if got := sumBucket(tl, BucketHarness); got != 0 {
		t.Errorf("harness = %v, want 0", got)
	}
}

func TestInterruptAtToolResultTimestampIsConsidered(t *testing.T) {
	tl := build(t, req(0, "r1"), asst(1, "r1", bash("b1", "ls")), result(10, "b1"), interrupt(10), req(70, "r2"), asst(71, "r2", text()))
	if got := sumBucket(tl, BucketUser); got != 60*time.Second {
		t.Errorf("user = %v, want 60s (intervals %+v)", got, tl.Intervals)
	}
}

// A tool_result that sorts before its tool_use must still pair with it.
func TestResultBeforeToolUsePairs(t *testing.T) {
	es := []tr.Entry{req(0, "r1"), result(31, "b1"), asst(31, "r1", bash("b1", "go test")), human(40)}
	b := &builder{opt: Options{Away: time.Minute, Suspend: time.Hour}, tools: map[string]toolUse{}}
	b.collect(es)
	for _, s := range b.spans {
		if s.Bucket == BucketTool {
			return
		}
	}
	t.Errorf("no tool span recorded; spans %+v", b.spans)
}

func TestLargeThreadBuildsQuickly(t *testing.T) {
	var es []tr.Entry
	for i := range 12500 {
		s := float64(i * 20)
		id, tid := fmt.Sprintf("r%d", i), fmt.Sprintf("t%d", i)
		es = append(es, req(s, id), asst(s+3, id, bash(tid, "ls")), result(s+8, tid), turnDur(s+9))
	}
	start := time.Now()
	tl := Build(es, Options{Away: 10 * time.Minute, Suspend: time.Hour})
	elapsed := time.Since(start)
	t.Logf("%d entries, %d intervals, built in %v", len(es), len(tl.Intervals), elapsed)
	if elapsed > time.Second {
		t.Errorf("Build took %v, want under 1s", elapsed)
	}
}

func TestManyUnansweredRequestsBuildQuickly(t *testing.T) {
	var es []tr.Entry
	for i := range 20000 {
		es = append(es, req(float64(i), fmt.Sprintf("r%d", i)))
	}
	start := time.Now()
	Build(es, Options{Away: 10 * time.Minute, Suspend: time.Hour})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Build took %v, want under 1s", elapsed)
	}
}
