// Package report builds per-repo and per-session summaries of where agent
// time went over a date range, and renders them as plain rows, JSON or a
// pretty table.
package report

import (
	"cmp"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/bensyverson/wallclock/repo"
	"github.com/bensyverson/wallclock/timeline"
	tr "github.com/bensyverson/wallclock/transcript"
)

// Loader reads a transcript (in practice, through the cache).
type Loader interface {
	Load(path string) (*tr.File, error)
}

// Resolver maps a cwd to its repo.
type Resolver interface {
	Resolve(cwd string) repo.Info
}

// Options select what to report and how to attribute time.
type Options struct {
	// Since and Until bound the range; intervals are clipped to it.
	Since, Until time.Time
	Timeline     timeline.Options
	// Repo, when set, keeps only the repo with this name.
	Repo string
	// IncludeScratch keeps sessions run from temp directories.
	IncludeScratch bool
}

// Key names a bucket ("api", "tool:test") or an event ("api_error:overloaded").
type Key string

// UnknownRepo names the repo of sessions that recorded no working directory.
const UnknownRepo = "unknown"

// Summary totals one repo, one session, or everything.
type Summary struct {
	// Repo is the display name: the repo's base name, prefixed with its
	// parent directory when two repos in the report share a base name.
	Repo string
	// Root is the repo's root directory; repos are grouped by it.
	Root     string
	Language string
	Sessions int
	Threads  int
	Buckets  map[Key]time.Duration
	Events   map[Key]int
}

func newSummary(name, lang string) Summary {
	return Summary{Repo: name, Language: lang, Buckets: map[Key]time.Duration{}, Events: map[Key]int{}}
}

// Active is the summed time of buckets that count toward the active total.
func (s Summary) Active() time.Duration {
	var d time.Duration
	for k, v := range s.Buckets {
		if bucketOf(k).Active() {
			d += v
		}
	}
	return d
}

func (s *Summary) add(o Summary) {
	s.Sessions += o.Sessions
	s.Threads += o.Threads
	for k, v := range o.Buckets {
		s.Buckets[k] += v
	}
	for k, v := range o.Events {
		s.Events[k] += v
	}
}

// SessionRow is one session's summary.
type SessionRow struct {
	ID    string
	Start time.Time
	Summary
}

// Report is the result of Collect.
type Report struct {
	Since, Until time.Time
	// Repos are sorted by active time, largest first.
	Repos []Summary
	All   Summary
	// Sessions are sorted by start time.
	Sessions []SessionRow
	// Unreadable lists transcripts that could not be read.
	Unreadable []string
	// InRange lists the names of repos with sessions in range, ignoring
	// Options.Repo, so a filter that matches nothing can suggest names.
	InRange []string
}

// Collect analyzes the sessions and aggregates them.
func Collect(sessions []tr.Session, l Loader, r Resolver, opt Options) (*Report, error) {
	rows := make([]*SessionRow, len(sessions))
	unreadable := make([][]string, len(sessions))
	names := make([]string, len(sessions))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, s := range sessions {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rows[i], names[i], unreadable[i] = analyze(s, l, r, opt)
		}()
	}
	wg.Wait()

	rep := &Report{Since: opt.Since, Until: opt.Until, All: newSummary("all", "-")}
	byRepo := map[string]*Summary{}
	seen := map[string]bool{}
	for i, row := range rows {
		rep.Unreadable = append(rep.Unreadable, unreadable[i]...)
		if names[i] != "" && !seen[names[i]] {
			seen[names[i]] = true
			rep.InRange = append(rep.InRange, names[i])
		}
		if row == nil {
			continue
		}
		sum := byRepo[row.Root]
		if sum == nil {
			s := newSummary(row.Repo, row.Language)
			s.Root = row.Root
			sum = &s
			byRepo[row.Root] = sum
		}
		sum.add(row.Summary)
		rep.All.add(row.Summary)
	}
	slices.Sort(rep.InRange)
	display := disambiguate(byRepo)
	for _, s := range byRepo {
		s.Repo = display[s.Root]
		rep.Repos = append(rep.Repos, *s)
	}
	for _, row := range rows {
		if row != nil {
			row.Repo = display[row.Root]
			rep.Sessions = append(rep.Sessions, *row)
		}
	}
	slices.SortFunc(rep.Repos, func(a, b Summary) int {
		if c := cmp.Compare(b.Active(), a.Active()); c != 0 {
			return c
		}
		return cmp.Compare(a.Repo, b.Repo)
	})
	slices.SortFunc(rep.Sessions, func(a, b SessionRow) int {
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return rep, nil
}

// disambiguate maps each repo root to a display name: its base name, or
// parent/base when another repo in the report shares the base name.
func disambiguate(byRepo map[string]*Summary) map[string]string {
	count := map[string]int{}
	for _, s := range byRepo {
		count[s.Repo]++
	}
	out := map[string]string{}
	for root, s := range byRepo {
		name := s.Repo
		if count[name] > 1 && root != "" {
			name = filepath.Base(filepath.Dir(root)) + "/" + name
		}
		out[root] = name
	}
	return out
}

// analyze builds one session's row, or nil when it is filtered out or has
// no time in range. It also returns the repo's name when the session is
// not scratch, whether or not the repo filter kept it.
func analyze(s tr.Session, l Loader, r Resolver, opt Options) (*SessionRow, string, []string) {
	var unreadable []string
	main, err := l.Load(s.Path)
	if err != nil {
		return nil, "", []string{s.Path}
	}
	info := repo.Info{Name: UnknownRepo, Language: repo.LangUnknown}
	if cwd := firstCwd(main); cwd != "" {
		info = r.Resolve(cwd)
	}
	if info.Scratch && !opt.IncludeScratch {
		return nil, "", nil
	}
	if opt.Repo != "" && info.Name != opt.Repo {
		return nil, info.Name, nil
	}
	lang := string(info.Language)
	if lang == "" {
		lang = string(repo.LangUnknown)
	}
	row := &SessionRow{ID: s.ID, Summary: newSummary(info.Name, lang)}
	row.Root = info.Root
	row.Sessions = 1
	threads := []*tr.File{main}
	for _, a := range s.Subagents {
		f, err := l.Load(a.Path)
		if err != nil {
			unreadable = append(unreadable, a.Path)
			continue
		}
		threads = append(threads, f)
	}
	for _, f := range threads {
		tl := timeline.Build(f.Entries, opt.Timeline)
		if row.addTimeline(tl, opt.Since, opt.Until) {
			row.Threads++
		}
	}
	if row.Threads == 0 {
		return nil, info.Name, unreadable
	}
	return row, info.Name, unreadable
}

func firstCwd(f *tr.File) string {
	for _, e := range f.Entries {
		if e.Cwd != "" {
			return e.Cwd
		}
	}
	return ""
}

// addTimeline adds a thread's in-range time and events, reporting whether
// any of its time fell in range.
func (row *SessionRow) addTimeline(tl timeline.Timeline, since, until time.Time) bool {
	any := false
	for _, iv := range tl.Intervals {
		start, end := maxTime(iv.Start, since), minTime(iv.End, until)
		if !end.After(start) {
			continue
		}
		if !any || start.Before(row.Start) {
			if row.Start.IsZero() || start.Before(row.Start) {
				row.Start = start
			}
		}
		any = true
		row.Buckets[keyOf(iv.Bucket, iv.Kind)] += end.Sub(start)
	}
	for _, ev := range tl.Events {
		if !ev.At.Before(since) && ev.At.Before(until) {
			row.Events[Key(joinKind(string(ev.Type), ev.Kind))]++
		}
	}
	return any
}

func keyOf(b timeline.Bucket, kind string) Key { return Key(joinKind(string(b), kind)) }

func joinKind(a, kind string) string {
	if kind == "" {
		return a
	}
	return a + ":" + kind
}

func bucketOf(k Key) timeline.Bucket {
	s := string(k)
	for i := range len(s) {
		if s[i] == ':' {
			return timeline.Bucket(s[:i])
		}
	}
	return timeline.Bucket(s)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
