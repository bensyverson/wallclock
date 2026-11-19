// Command gaps lists the longest intervals of one bucket (harness by
// default) across recent transcripts, with the entries around each, to
// explain or fix attribution rules.
//
//	go run ./scripts/gaps -days 14 -bucket harness -n 10
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bensyverson/wallclock/classify"
	"github.com/bensyverson/wallclock/timeline"
	tr "github.com/bensyverson/wallclock/transcript"
)

type hit struct {
	path string
	iv   timeline.Interval
	es   []tr.Entry
}

func main() {
	days := flag.Int("days", 14, "look back this many days")
	bucket := flag.String("bucket", "harness", "bucket to list")
	n := flag.Int("n", 10, "how many intervals")
	ctx := flag.Int("context", 3, "entries to show on each side")
	flag.Parse()
	home, _ := os.UserHomeDir()
	sessions, err := tr.Discover(filepath.Join(home, ".claude", "projects"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(5)
	}
	cl, _ := classify.New()
	opt := timeline.Options{Away: 10 * time.Minute, Suspend: time.Hour, Classify: func(c string) string { return string(cl.Classify(c)) }}
	since := time.Now().Add(-time.Duration(*days) * 24 * time.Hour)
	var hits []hit
	for _, s := range sessions {
		paths := []string{s.Path}
		for _, a := range s.Subagents {
			paths = append(paths, a.Path)
		}
		for _, p := range paths {
			if fi, err := os.Stat(p); err != nil || fi.ModTime().Before(since) {
				continue
			}
			f, err := tr.ParseFile(p)
			if err != nil {
				continue
			}
			es := f.Entries
			sort.SliceStable(es, func(i, j int) bool { return es[i].Timestamp.Before(es[j].Timestamp) })
			for _, iv := range timeline.Build(es, opt).Intervals {
				if string(iv.Bucket) == *bucket {
					hits = append(hits, hit{p, iv, es})
				}
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].iv.Duration() > hits[j].iv.Duration() })
	var total time.Duration
	for _, h := range hits {
		total += h.iv.Duration()
	}
	fmt.Printf("%d %s intervals, %v total\n", len(hits), *bucket, total.Round(time.Second))
	for _, h := range hits[:min(*n, len(hits))] {
		fmt.Printf("\n== %v  %s  %s..%s\n", h.iv.Duration().Round(time.Second), shortPath(h.path), h.iv.Start.Format(time.DateTime), h.iv.End.Format(time.DateTime))
		for i, e := range h.es {
			if e.Timestamp.Before(h.iv.Start) || e.Timestamp.After(h.iv.End) {
				// show context only near the edges
				if !near(h.es, i, h.iv, *ctx) {
					continue
				}
			}
			fmt.Printf("   %s %s\n", e.Timestamp.Format("01-02 15:04:05.000"), describe(e))
		}
	}
}

func near(es []tr.Entry, i int, iv timeline.Interval, k int) bool {
	for d := 1; d <= k; d++ {
		if i+d < len(es) && !es[i+d].Timestamp.Before(iv.Start) && !es[i+d].Timestamp.After(iv.End) {
			return true
		}
		if i-d >= 0 && !es[i-d].Timestamp.Before(iv.Start) && !es[i-d].Timestamp.After(iv.End) {
			return true
		}
	}
	return false
}

func shortPath(p string) string {
	parts := strings.Split(p, string(filepath.Separator))
	if len(parts) > 3 {
		return strings.Join(parts[len(parts)-3:], "/")
	}
	return p
}

func describe(e tr.Entry) string {
	var b strings.Builder
	b.WriteString(string(e.Type))
	if e.Subtype != "" {
		b.WriteString("/" + string(e.Subtype))
	}
	if e.Origin != "" {
		b.WriteString(" origin=" + string(e.Origin))
	}
	if e.Operation != "" {
		b.WriteString(" op=" + e.Operation)
	}
	for _, bl := range e.Blocks {
		b.WriteString(" " + string(bl.Type))
		if bl.Name != "" {
			b.WriteString(":" + bl.Name)
		}
	}
	if e.RequestID != "" {
		b.WriteString(" id=" + e.RequestID)
	}
	if e.RequestRef != "" {
		b.WriteString(" ref=" + e.RequestRef)
	}
	if e.Permission != nil {
		b.WriteString(" perm=" + e.Permission.Source)
	}
	if t := strings.TrimSpace(e.Text); t != "" {
		if len(t) > 60 {
			t = t[:60]
		}
		b.WriteString(" text=" + strings.ReplaceAll(t, "\n", " "))
	}
	return b.String()
}
