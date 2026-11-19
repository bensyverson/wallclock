// Command commands ranks Bash commands of one kind by total wait time
// across recent transcripts, to find what the classifier misses.
//
//	go run ./scripts/commands -days 14 -kind bash -n 25
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
	tr "github.com/bensyverson/wallclock/transcript"
)

func main() {
	days := flag.Int("days", 14, "look back this many days")
	kind := flag.String("kind", "bash", "command kind to rank")
	n := flag.Int("n", 25, "how many commands")
	words := flag.Int("words", 2, "group commands by their first N words")
	flag.Parse()
	home, _ := os.UserHomeDir()
	sessions, err := tr.Discover(filepath.Join(home, ".claude", "projects"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(5)
	}
	cl, _ := classify.New()
	since := time.Now().Add(-time.Duration(*days) * 24 * time.Hour)
	total := map[string]time.Duration{}
	count := map[string]int{}
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
			starts := map[string]time.Time{}
			cmds := map[string]string{}
			for _, e := range f.Entries {
				for _, b := range e.Blocks {
					switch {
					case b.Type == tr.BlockToolUse && b.Name == "Bash" && !b.RunInBackground:
						starts[b.ToolUseID], cmds[b.ToolUseID] = e.Timestamp, b.Command
					case b.Type == tr.BlockToolResult:
						cmd, ok := cmds[b.ToolUseID]
						if !ok || string(cl.Classify(cmd)) != *kind {
							continue
						}
						key := group(cmd, *words)
						total[key] += e.Timestamp.Sub(starts[b.ToolUseID])
						count[key]++
					}
				}
			}
		}
	}
	keys := make([]string, 0, len(total))
	for k := range total {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return total[keys[i]] > total[keys[j]] })
	for _, k := range keys[:min(*n, len(keys))] {
		fmt.Printf("%8s  %4d  %s\n", total[k].Round(time.Second), count[k], k)
	}
}

// group reduces a command to its first words, skipping a leading cd.
func group(cmd string, n int) string {
	cmd = strings.TrimSpace(strings.SplitN(cmd, "\n", 2)[0])
	if strings.HasPrefix(cmd, "cd ") {
		if i := strings.Index(cmd, "&&"); i > 0 {
			cmd = strings.TrimSpace(cmd[i+2:])
		}
	}
	f := strings.Fields(cmd)
	return strings.Join(f[:min(n, len(f))], " ")
}
