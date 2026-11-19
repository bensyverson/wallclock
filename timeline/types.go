// Package timeline turns one agent thread's transcript entries into
// contiguous, classified intervals: what the thread was waiting on, when.
// Every attribution rule lives here; docs/buckets.md describes them.
package timeline

import "time"

// Bucket is what a thread was waiting on during an interval.
type Bucket string

const (
	BucketAPI        Bucket = "api"
	BucketAPIError   Bucket = "api_error"
	BucketTool       Bucket = "tool"
	BucketApproval   Bucket = "approval"
	BucketUser       Bucket = "user"
	BucketAway       Bucket = "away"
	BucketHook       Bucket = "hook"
	BucketBackground Bucket = "background"
	BucketIdle       Bucket = "idle"
	BucketSuspended  Bucket = "suspended"
	BucketHarness    Bucket = "harness"
)

// Active reports whether time in the bucket counts toward the active total.
// Away and suspended time are reported but excluded; idle time (waiting on a
// subagent) counts as zero because the subagent's own time is counted.
func (b Bucket) Active() bool {
	switch b {
	case BucketAway, BucketSuspended, BucketIdle:
		return false
	}
	return true
}

// Interval is a stretch of a thread's time attributed to one bucket.
type Interval struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Bucket Bucket    `json:"bucket"`
	// Kind refines the bucket: a command or tool type for tool and
	// background, an error kind for api_error.
	Kind string `json:"kind,omitempty"`
}

// Duration is the interval's length.
func (iv Interval) Duration() time.Duration { return iv.End.Sub(iv.Start) }

// EventType names a countable occurrence.
type EventType string

const (
	EventAPIError     EventType = "api_error"
	EventInterruption EventType = "interruption"
	// EventAbsence is a user or approval wait longer than the away threshold.
	EventAbsence EventType = "absence"
	// EventSuspension is an interval treated as the machine sleeping or the
	// session being stopped mid-work.
	EventSuspension EventType = "suspension"
)

// Event is a countable occurrence at a point in time.
type Event struct {
	At   time.Time `json:"at"`
	Type EventType `json:"type"`
	Kind string    `json:"kind,omitempty"`
}

// Timeline is one thread's classified time.
type Timeline struct {
	Intervals []Interval `json:"intervals"`
	Events    []Event    `json:"events"`
}

// Options are the attribution thresholds and the command classifier.
type Options struct {
	// Away caps each user or approval wait; the remainder is away.
	Away time.Duration
	// Suspend is the longest a computer or API wait can plausibly run;
	// longer intervals count as suspended.
	Suspend time.Duration
	// Classify types a Bash command line (test, build, …).
	Classify func(command string) string
}
