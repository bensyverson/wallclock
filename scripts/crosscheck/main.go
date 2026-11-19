// Command crosscheck compares wallclock's API and tool totals with the
// cost-state totals Claude Code records, for each session that has one.
// cost-state covers one process run: startTime to startTime+totalDuration,
// across the main thread and its subagents.
//
//	go run ./scripts/crosscheck
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bensyverson/wallclock/classify"
	"github.com/bensyverson/wallclock/timeline"
	tr "github.com/bensyverson/wallclock/transcript"
)

type costState struct {
	StartTime int64 `json:"startTime"`
	API       int64 `json:"totalAPIDuration"`
	APINoRetr int64 `json:"totalAPIDurationWithoutRetries"`
	Tool      int64 `json:"totalToolDuration"`
	Total     int64 `json:"totalDuration"`
}

// lastCostState reads the final cost-state line; the parser keeps only
// its totals, and this check also needs startTime.
func lastCostState(path string) (costState, bool) {
	f, err := os.Open(path)
	if err != nil {
		return costState{}, false
	}
	defer f.Close()
	var cs costState
	found := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		if strings.Contains(sc.Text(), `"type":"cost-state"`) {
			if json.Unmarshal(sc.Bytes(), &cs) == nil {
				found = true
			}
		}
	}
	return cs, found
}

func main() {
	home, _ := os.UserHomeDir()
	sessions, _ := tr.Discover(filepath.Join(home, ".claude", "projects"))
	cl, _ := classify.New()
	opt := timeline.Options{Away: 10 * time.Minute, Suspend: time.Hour, Classify: func(c string) string { return string(cl.Classify(c)) }}
	fmt.Println("session   threads  cs_api  wc_api  ratio  cs_tool  wc_tool  ratio")
	for _, s := range sessions {
		cs, ok := lastCostState(s.Path)
		if !ok || cs.StartTime == 0 {
			continue
		}
		start := time.UnixMilli(cs.StartTime)
		end := start.Add(time.Duration(cs.Total) * time.Millisecond)
		paths := []string{s.Path}
		for _, a := range s.Subagents {
			paths = append(paths, a.Path)
		}
		var api, tool time.Duration
		for _, p := range paths {
			f, err := tr.ParseFile(p)
			if err != nil {
				continue
			}
			for _, iv := range timeline.Build(f.Entries, opt).Intervals {
				a, z := iv.Start, iv.End
				if a.Before(start) {
					a = start
				}
				if z.After(end) {
					z = end
				}
				if !z.After(a) {
					continue
				}
				switch iv.Bucket {
				case timeline.BucketAPI, timeline.BucketAPIError:
					api += z.Sub(a)
				case timeline.BucketTool, timeline.BucketApproval:
					tool += z.Sub(a)
				}
			}
		}
		csAPI := time.Duration(cs.API) * time.Millisecond
		csTool := time.Duration(cs.Tool) * time.Millisecond
		fmt.Printf("%s  %7d  %6.0f  %6.0f  %5.2f  %7.0f  %7.0f  %5.2f\n", s.ID[:8], len(paths),
			csAPI.Seconds(), api.Seconds(), ratio(api, csAPI), csTool.Seconds(), tool.Seconds(), ratio(tool, csTool))
	}
}

func ratio(a, b time.Duration) float64 {
	if b == 0 {
		return 0
	}
	return a.Seconds() / b.Seconds()
}
