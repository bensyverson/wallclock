package report

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/bensyverson/wallclock/timeline"
)

// Format selects an output shape.
type Format int

const (
	// FormatPlain is tab-separated rows with a header: terse and easy to parse.
	FormatPlain Format = iota
	// FormatJSON is the stable machine shape.
	FormatJSON
	// FormatPretty is an aligned table for people.
	FormatPretty
)

// AgentTimeNote explains why totals can exceed wall-clock time.
const AgentTimeNote = "agent-time: subagent time adds up, so totals can exceed wall-clock"

// bucketOrder fixes the display order of bucket families.
var bucketOrder = []timeline.Bucket{
	timeline.BucketAPI, timeline.BucketAPIError, timeline.BucketTool, timeline.BucketBackground,
	timeline.BucketApproval, timeline.BucketUser, timeline.BucketHook, timeline.BucketHarness,
	timeline.BucketAway, timeline.BucketSuspended, timeline.BucketIdle,
}

func rank(b timeline.Bucket) int {
	if i := slices.Index(bucketOrder, b); i >= 0 {
		return i
	}
	return len(bucketOrder)
}

// sortedKeys orders a summary's buckets: active families first in display
// order, kinds alphabetically within a family.
func sortedKeys(s Summary) []Key {
	keys := make([]Key, 0, len(s.Buckets))
	for k, v := range s.Buckets {
		if v > 0 {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b Key) int {
		if c := cmp.Compare(rank(bucketOf(a)), rank(bucketOf(b))); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	return keys
}

func sortedEvents(s Summary) []Key {
	keys := make([]Key, 0, len(s.Events))
	for k, v := range s.Events {
		if v > 0 {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func share(d, active time.Duration) float64 {
	if active <= 0 {
		return 0
	}
	return float64(d) / float64(active)
}

func dateRange(rep *Report) (string, string) {
	return rep.Since.Format("2006-01-02"), rep.Until.Add(-time.Nanosecond).Format("2006-01-02")
}

func summaries(rep *Report) []Summary {
	return append(slices.Clone(rep.Repos), rep.All)
}

// WriteRepos renders the per-repo report.
func WriteRepos(w io.Writer, rep *Report, f Format) error {
	switch f {
	case FormatJSON:
		return writeJSON(w, rep)
	case FormatPretty:
		return writePretty(w, rep)
	}
	return writePlain(w, rep)
}

func writePlain(w io.Writer, rep *Report) error {
	from, to := dateRange(rep)
	var b strings.Builder
	fmt.Fprintf(&b, "# wallclock report %s..%s; %s\n", from, to, AgentTimeNote)
	b.WriteString("repo\tlang\tbucket\tseconds\tshare\n")
	hasEvents := false
	for _, s := range summaries(rep) {
		active := s.Active()
		var inactive []Key
		for _, k := range sortedKeys(s) {
			if !bucketOf(k).Active() {
				inactive = append(inactive, k)
				continue
			}
			fmt.Fprintf(&b, "%s\t%s\t%s\t%d\t%.3f\n", s.Repo, s.Language, k, secs(s.Buckets[k]), share(s.Buckets[k], active))
		}
		fmt.Fprintf(&b, "%s\t%s\tactive\t%d\t1.000\n", s.Repo, s.Language, secs(active))
		for _, k := range inactive {
			fmt.Fprintf(&b, "%s\t%s\t%s\t%d\t-\n", s.Repo, s.Language, k, secs(s.Buckets[k]))
		}
		if len(sortedEvents(s)) > 0 {
			hasEvents = true
		}
	}
	if hasEvents {
		b.WriteString("\nrepo\tlang\tevent\tcount\n")
		for _, s := range summaries(rep) {
			for _, k := range sortedEvents(s) {
				fmt.Fprintf(&b, "%s\t%s\t%s\t%d\n", s.Repo, s.Language, k, s.Events[k])
			}
		}
	}
	writeUnreadable(&b, rep, "# ")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeUnreadable(b *strings.Builder, rep *Report, prefix string) {
	if len(rep.Unreadable) > 0 {
		fmt.Fprintf(b, "%s%d transcript(s) could not be read; first: %s\n", prefix, len(rep.Unreadable), rep.Unreadable[0])
	}
}

func secs(d time.Duration) int64 { return int64(math.Round(d.Seconds())) }

func writePretty(w io.Writer, rep *Report) error {
	from, to := dateRange(rep)
	var b strings.Builder
	fmt.Fprintf(&b, "wallclock: %s to %s\nAgent-time: subagent time adds up, so totals can exceed wall-clock.\n", from, to)
	if len(rep.Repos) == 0 {
		b.WriteString("\nNo sessions in range. Widen it with --days or --since.\n")
	}
	width := 0
	for _, s := range summaries(rep) {
		for _, k := range sortedKeys(s) {
			width = max(width, len(k))
		}
	}
	for _, s := range summaries(rep) {
		if s.Repo == "all" && len(rep.Repos) == 0 {
			continue
		}
		active := s.Active()
		b.WriteString("\n")
		if s.Language == "-" {
			fmt.Fprintf(&b, "%s: ", s.Repo)
		} else {
			fmt.Fprintf(&b, "%s (%s): ", s.Repo, s.Language)
		}
		fmt.Fprintf(&b, "%s, %s, %s active\n", plural(s.Sessions, "session"), plural(s.Threads, "thread"), FormatDuration(active))
		for _, k := range sortedKeys(s) {
			d := s.Buckets[k]
			if !bucketOf(k).Active() {
				fmt.Fprintf(&b, "  %-*s %6s  %6s\n", width, k, FormatDuration(d), "-")
				continue
			}
			sh := share(d, active)
			fmt.Fprintf(&b, "  %-*s %6s  %5.1f%%  %s\n", width, k, FormatDuration(d), sh*100, strings.Repeat("#", int(math.Round(sh*20))))
		}
		if evs := sortedEvents(s); len(evs) > 0 {
			parts := make([]string, len(evs))
			for i, k := range evs {
				parts[i] = fmt.Sprintf("%s %d", k, s.Events[k])
			}
			fmt.Fprintf(&b, "  events: %s\n", strings.Join(parts, ", "))
		}
	}
	writeUnreadable(&b, rep, "\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// FormatDuration renders a duration compactly: 4s, 1m09s, 1h02m.
func FormatDuration(d time.Duration) string {
	s := secs(d)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
}

type jsonBucket struct {
	Bucket  Key     `json:"bucket"`
	Seconds int64   `json:"seconds"`
	Active  bool    `json:"active"`
	Share   float64 `json:"share"`
}

type jsonEvent struct {
	Event Key `json:"event"`
	Count int `json:"count"`
}

type jsonSummary struct {
	Repo          string       `json:"repo"`
	Language      string       `json:"language"`
	Sessions      int          `json:"sessions"`
	Threads       int          `json:"threads"`
	ActiveSeconds int64        `json:"active_seconds"`
	Buckets       []jsonBucket `json:"buckets"`
	Events        []jsonEvent  `json:"events"`
}

type jsonSession struct {
	ID    string    `json:"id"`
	Start time.Time `json:"start"`
	jsonSummary
}

type jsonReport struct {
	Since      time.Time     `json:"since"`
	Until      time.Time     `json:"until"`
	Note       string        `json:"note"`
	Repos      []jsonSummary `json:"repos"`
	All        jsonSummary   `json:"all"`
	Sessions   []jsonSession `json:"sessions,omitempty"`
	Unreadable []string      `json:"unreadable"`
}

func toJSON(s Summary) jsonSummary {
	active := s.Active()
	out := jsonSummary{Repo: s.Repo, Language: s.Language, Sessions: s.Sessions, Threads: s.Threads,
		ActiveSeconds: secs(active), Buckets: []jsonBucket{}, Events: []jsonEvent{}}
	for _, k := range sortedKeys(s) {
		jb := jsonBucket{Bucket: k, Seconds: secs(s.Buckets[k]), Active: bucketOf(k).Active()}
		if jb.Active {
			jb.Share = math.Round(share(s.Buckets[k], active)*1000) / 1000
		}
		out.Buckets = append(out.Buckets, jb)
	}
	for _, k := range sortedEvents(s) {
		out.Events = append(out.Events, jsonEvent{Event: k, Count: s.Events[k]})
	}
	return out
}

func writeJSON(w io.Writer, rep *Report) error {
	return encodeJSON(w, rep, false)
}

func encodeJSON(w io.Writer, rep *Report, sessions bool) error {
	out := jsonReport{Since: rep.Since, Until: rep.Until, Note: AgentTimeNote, All: toJSON(rep.All),
		Repos: []jsonSummary{}, Unreadable: rep.Unreadable}
	if out.Unreadable == nil {
		out.Unreadable = []string{}
	}
	for _, s := range rep.Repos {
		out.Repos = append(out.Repos, toJSON(s))
	}
	if sessions {
		out.Sessions = []jsonSession{}
		for _, s := range rep.Sessions {
			out.Sessions = append(out.Sessions, jsonSession{ID: s.ID, Start: s.Start, jsonSummary: toJSON(s.Summary)})
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// WriteSessions renders one row per session.
func WriteSessions(w io.Writer, rep *Report, f Format) error {
	if f == FormatJSON {
		return encodeJSON(w, rep, true)
	}
	var b strings.Builder
	header := []string{"session", "repo", "start", "threads", "active_s", "api", "computer", "user", "other"}
	rows := [][]string{header}
	for _, s := range rep.Sessions {
		active := s.Active()
		var api, computer, user, other time.Duration
		for k, d := range s.Buckets {
			switch bucketOf(k) {
			case timeline.BucketAPI:
				api += d
			case timeline.BucketTool, timeline.BucketBackground:
				computer += d
			case timeline.BucketUser, timeline.BucketApproval:
				user += d
			case timeline.BucketAPIError, timeline.BucketHook, timeline.BucketHarness:
				other += d
			}
		}
		rows = append(rows, []string{
			s.ID, s.Repo, s.Start.Format("2006-01-02T15:04"), fmt.Sprint(s.Threads), fmt.Sprint(secs(active)),
			fmt.Sprintf("%.3f", share(api, active)), fmt.Sprintf("%.3f", share(computer, active)),
			fmt.Sprintf("%.3f", share(user, active)), fmt.Sprintf("%.3f", share(other, active)),
		})
	}
	if f == FormatPretty {
		widths := make([]int, len(header))
		for _, r := range rows {
			for i, c := range r {
				widths[i] = max(widths[i], len(c))
			}
		}
		for _, r := range rows {
			for i, c := range r {
				if i > 0 {
					b.WriteString("  ")
				}
				fmt.Fprintf(&b, "%-*s", widths[i], c)
			}
			b.WriteString("\n")
		}
	} else {
		for _, r := range rows {
			b.WriteString(strings.Join(r, "\t") + "\n")
		}
	}
	writeUnreadable(&b, rep, "# ")
	_, err := io.WriteString(w, b.String())
	return err
}
