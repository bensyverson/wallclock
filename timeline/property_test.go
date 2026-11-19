package timeline

import (
	"fmt"
	"math/rand/v2"
	"testing"

	tr "github.com/bensyverson/wallclock/transcript"
)

// TestRandomThreadsStayContiguous generates plausible-but-messy threads
// (parallel tools, missing results, long gaps, out-of-order logging) and
// checks the contiguity invariant on each.
func TestRandomThreadsStayContiguous(t *testing.T) {
	for seed := range uint64(500) {
		r := rand.New(rand.NewPCG(seed, 7))
		var es []tr.Entry
		now := 0.0
		step := func() float64 {
			if r.IntN(20) == 0 {
				now += float64(r.IntN(7200))
			} else {
				now += r.Float64() * 5
			}
			return now
		}
		names := []string{"Bash", "Read", "Agent", "AskUserQuestion", "WebFetch"}
		for turn := 0; turn < 1+r.IntN(5); turn++ {
			es = append(es, human(step()))
			for call := 0; call < 1+r.IntN(4); call++ {
				id := fmt.Sprintf("r%d-%d", turn, call)
				es = append(es, req(step(), id))
				var tools []string
				for k := 0; k < r.IntN(3); k++ {
					tid := fmt.Sprintf("t%d-%d-%d", turn, call, k)
					tools = append(tools, tid)
					es = append(es, asst(step(), id, tool(tid, names[r.IntN(len(names))])))
				}
				if r.IntN(10) == 0 {
					es = append(es, apiErr(step(), id, "API Error: 529 Overloaded"))
				} else {
					es = append(es, asst(step(), id, text()))
				}
				for _, tid := range tools {
					if r.IntN(8) == 0 {
						continue
					}
					if r.IntN(4) == 0 {
						es = append(es, resultBy(step(), tid, "accept", "user_temporary"))
					} else {
						es = append(es, result(step(), tid))
					}
				}
			}
			if r.IntN(3) == 0 {
				es = append(es, stopHook(step()))
			}
		}
		r.Shuffle(len(es), func(i, j int) {
			if r.IntN(10) == 0 {
				es[i], es[j] = es[j], es[i]
			}
		})
		t.Run(fmt.Sprint(seed), func(t *testing.T) { build(t, es...) })
	}
}
