package timeline

import (
	"container/heap"
	"sort"
	"strings"
	"time"

	tr "github.com/bensyverson/wallclock/transcript"
)

// Tool kinds for tools other than Bash.
const (
	KindFile  = "file"
	KindWeb   = "web"
	KindMCP   = "mcp"
	KindOther = "other"
)

// priority orders buckets when explicit intervals overlap: the higher one
// owns the overlap. API wins over tools because Claude Code can start a
// tool while the response is still streaming.
var priority = map[Bucket]int{
	BucketSuspended:  9,
	BucketAPI:        8,
	BucketAPIError:   7,
	BucketApproval:   6,
	BucketUser:       5,
	BucketTool:       4,
	BucketIdle:       3,
	BucketBackground: 2,
	BucketHook:       1,
}

// span is an explicit interval before overlap resolution.
type span struct {
	Interval
}

// Build classifies one thread's entries.
func Build(entries []tr.Entry, opt Options) Timeline {
	es := make([]tr.Entry, 0, len(entries))
	for _, e := range entries {
		if e.HasTimestamp {
			es = append(es, e)
		}
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].Timestamp.Before(es[j].Timestamp) })
	var tl Timeline
	if len(es) < 2 {
		return tl
	}
	b := &builder{opt: opt, es: es, tools: map[string]toolUse{}}
	b.collect(es)
	b.resolve(es[0].Timestamp, es[len(es)-1].Timestamp)
	tl.Intervals = b.out
	tl.Events = b.events
	sort.SliceStable(tl.Events, func(i, j int) bool { return tl.Events[i].At.Before(tl.Events[j].At) })
	return tl
}

type toolUse struct {
	start time.Time
	block tr.Block
}

type builder struct {
	opt    Options
	es     []tr.Entry // timestamped entries, sorted
	reqs   map[string]time.Time
	tools  map[string]toolUse
	spans  []span
	out    []Interval
	events []Event
}

func (b *builder) event(at time.Time, typ EventType, kind string) {
	b.events = append(b.events, Event{At: at, Type: typ, Kind: kind})
}

// collect derives explicit intervals from request/response and
// tool_use/tool_result pairs.
func (b *builder) collect(es []tr.Entry) {
	reqStart := map[string]time.Time{}
	type resp struct {
		end     time.Time
		errText string
		isErr   bool
	}
	resps := map[string]*resp{}
	var reqOrder []string
	// Index every tool_use first so a result pairs with its call whatever
	// order the two sort in.
	for _, e := range es {
		if e.Type != tr.TypeAssistant {
			continue
		}
		for _, bl := range e.Blocks {
			if bl.Type == tr.BlockToolUse {
				b.tools[bl.ToolUseID] = toolUse{start: e.Timestamp, block: bl}
			}
		}
	}
	for _, e := range es {
		switch e.Type {
		case tr.TypeAPIRequest:
			if _, ok := reqStart[e.RequestID]; !ok {
				reqOrder = append(reqOrder, e.RequestID)
			}
			reqStart[e.RequestID] = e.Timestamp
		case tr.TypeAssistant:
			if e.RequestRef != "" {
				r := resps[e.RequestRef]
				if r == nil {
					r = &resp{}
					resps[e.RequestRef] = r
				}
				if e.Timestamp.After(r.end) {
					r.end = e.Timestamp
				}
				if e.IsAPIError {
					r.isErr, r.errText = true, e.Text
				}
			}
		case tr.TypeUser:
			if e.Interrupted {
				b.event(e.Timestamp, EventInterruption, "")
			}
			for _, bl := range e.Blocks {
				if bl.Type != tr.BlockToolResult {
					continue
				}
				if tu, ok := b.tools[bl.ToolUseID]; ok {
					b.addTool(tu, e)
				}
			}
		}
	}
	if len(reqStart) == 0 {
		b.legacyRequests(es)
	}
	b.orphanErrors(es, reqStart)
	for _, id := range reqOrder {
		r := resps[id]
		if r == nil {
			b.retry(reqStart[id])
			continue
		}
		iv := Interval{Start: reqStart[id], End: r.end, Bucket: BucketAPI}
		if r.isErr {
			iv.Bucket, iv.Kind = BucketAPIError, ErrorKind(r.errText)
			b.event(r.end, EventAPIError, iv.Kind)
			if iv.Kind == ErrorSleep {
				b.suspend(iv)
				continue
			}
		}
		b.addComputer(iv)
	}
}

// legacyRequests derives API intervals for transcripts that log no
// api-request entries: a response, grouped by Anthropic's request id, runs
// from the last user entry before its first block to its last block.
func (b *builder) legacyRequests(es []tr.Entry) {
	type group struct{ start, end time.Time }
	groups := map[string]*group{}
	var order []string
	var lastUser time.Time
	for _, e := range es {
		switch {
		case e.Type == tr.TypeUser:
			lastUser = e.Timestamp
		case e.Type == tr.TypeAssistant && e.APIRequestID != "" && !e.IsAPIError:
			g := groups[e.APIRequestID]
			if g == nil {
				start := lastUser
				if start.IsZero() {
					start = e.Timestamp
				}
				g = &group{start: start}
				groups[e.APIRequestID] = g
				order = append(order, e.APIRequestID)
			}
			g.end = e.Timestamp
		}
	}
	for _, id := range order {
		g := groups[id]
		b.addComputer(Interval{Start: g.start, End: g.end, Bucket: BucketAPI})
	}
}

// minRetry is the shortest wait on an unanswered request that counts as a
// retry; Claude Code routinely supersedes a request within a second.
const minRetry = 5 * time.Second

// retry records an unanswered request that was followed by another
// request as a wait on Anthropic: the request was abandoned and re-sent.
func (b *builder) retry(start time.Time) {
	first := sort.Search(len(b.es), func(i int) bool { return b.es[i].Timestamp.After(start) })
	for _, e := range b.es[first:] {
		switch e.Type {
		case tr.TypeAPIRequest:
			iv := Interval{Start: start, End: e.Timestamp, Bucket: BucketAPI}
			if iv.Duration() >= minRetry {
				iv.Bucket, iv.Kind = BucketAPIError, ErrorRetry
				b.event(start, EventAPIError, ErrorRetry)
			}
			b.addComputer(iv)
			return
		case tr.TypeAssistant, tr.TypeUser:
			return
		}
	}
}

// orphanErrors counts API errors Claude Code reported without a request,
// such as an expired login.
func (b *builder) orphanErrors(es []tr.Entry, reqs map[string]time.Time) {
	for _, e := range es {
		if isOrphanError(e, reqs) {
			b.event(e.Timestamp, EventAPIError, ErrorKind(e.Text))
		}
	}
	b.reqs = reqs
}

func isOrphanError(e tr.Entry, reqs map[string]time.Time) bool {
	if e.Type != tr.TypeAssistant || !e.IsAPIError {
		return false
	}
	_, ok := reqs[e.RequestRef]
	return e.RequestRef == "" || !ok
}

// addTool records the interval from a tool call to its result.
func (b *builder) addTool(tu toolUse, res tr.Entry) {
	iv := Interval{Start: tu.start, End: res.Timestamp}
	name := tu.block.Name
	switch {
	case name == "AskUserQuestion" || name == "ExitPlanMode":
		b.addPerson(iv, BucketUser)
		return
	case res.Permission.ByPerson():
		b.addPerson(iv, BucketApproval)
		return
	case name == "Agent" || name == "Task":
		iv.Bucket = BucketIdle
		b.spans = append(b.spans, span{iv})
		return
	}
	iv.Bucket, iv.Kind = BucketTool, b.toolKind(tu.block)
	b.addComputer(iv)
}

func (b *builder) toolKind(bl tr.Block) string {
	switch bl.Name {
	case "Bash":
		if b.opt.Classify != nil {
			return b.opt.Classify(bl.Command)
		}
		return "bash"
	case "Read", "Edit", "Write", "Glob", "Grep", "NotebookEdit", "MultiEdit":
		return KindFile
	case "WebFetch", "WebSearch":
		return KindWeb
	}
	if strings.HasPrefix(bl.Name, "mcp__") {
		return KindMCP
	}
	return KindOther
}

// addPerson records a wait on a person, capped at the away threshold.
func (b *builder) addPerson(iv Interval, bucket Bucket) {
	iv.Bucket = bucket
	if b.opt.Away > 0 && iv.Duration() > b.opt.Away {
		cut := iv.Start.Add(b.opt.Away)
		b.spans = append(b.spans, span{Interval{Start: iv.Start, End: cut, Bucket: bucket}})
		b.spans = append(b.spans, span{Interval{Start: cut, End: iv.End, Bucket: BucketAway}})
		b.event(iv.Start, EventAbsence, string(bucket))
		return
	}
	b.spans = append(b.spans, span{iv})
}

// addComputer records a wait on the API or the machine, treating an
// implausibly long one as a suspension.
func (b *builder) addComputer(iv Interval) {
	if b.opt.Suspend > 0 && iv.Duration() > b.opt.Suspend {
		b.suspend(iv)
		return
	}
	b.spans = append(b.spans, span{iv})
}

func (b *builder) suspend(iv Interval) {
	kind := string(iv.Bucket)
	if iv.Kind != "" {
		kind += ":" + iv.Kind
	}
	b.event(iv.Start, EventSuspension, kind)
	b.spans = append(b.spans, span{Interval{Start: iv.Start, End: iv.End, Bucket: BucketSuspended}})
}

// resolve lays the explicit spans over the thread's span, resolving
// overlaps by priority (ties go to the span ending last: the slowest of
// parallel tools, then the earliest recorded), and classifies the uncovered
// gaps. It sweeps the sorted cut points with a heap of the spans open at
// each one.
func (b *builder) resolve(first, last time.Time) {
	cuts := []time.Time{first, last}
	var spans []span
	for _, s := range b.spans {
		if s.End.After(s.Start) {
			cuts = append(cuts, s.Start, s.End)
			spans = append(spans, s)
		}
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].Before(cuts[j]) })
	byStart := make([]int, len(spans))
	for i := range byStart {
		byStart[i] = i
	}
	sort.SliceStable(byStart, func(i, j int) bool { return spans[byStart[i]].Start.Before(spans[byStart[j]].Start) })
	open := &openSpans{}
	next := 0
	var gapStart time.Time
	inGap := false
	for i := 0; i+1 < len(cuts); i++ {
		a, z := cuts[i], cuts[i+1]
		for next < len(byStart) && !spans[byStart[next]].Start.After(a) {
			heap.Push(open, openSpan{span: spans[byStart[next]], order: byStart[next]})
			next++
		}
		if !z.After(a) || a.Before(first) || z.After(last) {
			continue
		}
		// Every cut is a boundary, so a span ending at or before a cannot cover a..z.
		for open.Len() > 0 && !(*open)[0].End.After(a) {
			heap.Pop(open)
		}
		if open.Len() == 0 {
			if !inGap {
				gapStart, inGap = a, true
			}
			continue
		}
		owner := (*open)[0].Interval
		if inGap {
			b.gap(gapStart, a)
			inGap = false
		}
		b.emit(Interval{Start: a, End: z, Bucket: owner.Bucket, Kind: owner.Kind})
	}
	if inGap {
		b.gap(gapStart, last)
	}
}

type openSpan struct {
	span
	order int // index in recording order; the earlier span wins a full tie
}

// openSpans is a max-heap: the top is the span that owns an overlap.
type openSpans []openSpan

func (h openSpans) Len() int { return len(h) }
func (h openSpans) Less(i, j int) bool {
	pi, pj := priority[h[i].Bucket], priority[h[j].Bucket]
	if pi != pj {
		return pi > pj
	}
	if !h[i].End.Equal(h[j].End) {
		return h[i].End.After(h[j].End)
	}
	return h[i].order < h[j].order
}
func (h openSpans) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *openSpans) Push(x any)   { *h = append(*h, x.(openSpan)) }
func (h *openSpans) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// gap classifies an uncovered stretch by the entries inside it: a stop
// hook ends hook time, a prompt ends a user wait, a task notification ends
// a background or subagent wait. Once a turn has ended, the thread is
// waiting on the user until something starts the next turn. What remains
// is harness.
func (b *builder) gap(a, z time.Time) {
	cur := a
	turnEnded := false
	localKind := ""
	start := sort.Search(len(b.es), func(i int) bool { return !b.es[i].Timestamp.Before(a) })
	for _, e := range b.es[start:] {
		if e.Timestamp.After(z) {
			break
		}
		switch {
		case e.Type == tr.TypeSystem && e.Subtype == tr.SubtypeStopHookSummary:
			b.emitHook(Interval{Start: cur, End: e.Timestamp, Bucket: BucketHook})
		case e.Type == tr.TypeSystem && e.Subtype == tr.SubtypeTurnDuration:
			b.emitHarness(Interval{Start: cur, End: e.Timestamp, Bucket: BucketHarness})
			turnEnded = true
		case e.Interrupted || isOrphanError(e, b.reqs):
			b.emitHarness(Interval{Start: cur, End: e.Timestamp, Bucket: BucketHarness})
			turnEnded = true
		case e.LocalOutput && localKind != "":
			b.emit(Interval{Start: cur, End: e.Timestamp, Bucket: BucketTool, Kind: localKind})
			localKind = ""
		case isPrompt(e):
			b.emitPerson(Interval{Start: cur, End: e.Timestamp, Bucket: BucketUser})
			turnEnded = false
			if e.LocalCommand != "" {
				localKind = b.toolKind(tr.Block{Name: "Bash", Command: e.LocalCommand})
			}
		case e.Type == tr.TypeUser && e.Origin == tr.OriginTaskNotification:
			b.emit(b.wakeup(cur, e))
			turnEnded = false
		case e.Type == tr.TypeUser && (e.Origin == tr.OriginPeer || e.Origin == tr.OriginCoordinator):
			b.emit(Interval{Start: cur, End: e.Timestamp, Bucket: BucketIdle})
			turnEnded = false
		default:
			continue
		}
		cur = e.Timestamp
	}
	if !z.After(cur) {
		return
	}
	if turnEnded {
		b.emitPerson(Interval{Start: cur, End: z, Bucket: BucketUser})
		return
	}
	b.emitHarness(Interval{Start: cur, End: z, Bucket: BucketHarness})
}

// emitHarness records unexplained time. A stretch longer than the away
// threshold is a thread that was stopped or killed mid-turn, not work.
func (b *builder) emitHarness(iv Interval) {
	if b.opt.Away > 0 && iv.Duration() > b.opt.Away {
		b.suspendGap(iv)
		return
	}
	b.emit(iv)
}

// emitHook records stop-hook time, treating an implausibly long one as a
// suspension like any other wait on the machine.
func (b *builder) emitHook(iv Interval) {
	if b.opt.Suspend > 0 && iv.Duration() > b.opt.Suspend {
		b.suspendGap(iv)
		return
	}
	b.emit(iv)
}

func (b *builder) suspendGap(iv Interval) {
	b.event(iv.Start, EventSuspension, string(iv.Bucket))
	b.emit(Interval{Start: iv.Start, End: iv.End, Bucket: BucketSuspended})
}

func isPrompt(e tr.Entry) bool {
	return e.Type == tr.TypeUser && (e.Origin == tr.OriginHuman || e.LocalInput)
}

// wakeup classifies an idle stretch ended by a task notification, by the
// tool call that started the task.
func (b *builder) wakeup(start time.Time, e tr.Entry) Interval {
	iv := Interval{Start: start, End: e.Timestamp, Bucket: BucketIdle}
	tu, ok := b.tools[e.NotifiedToolUseID]
	if !ok || tu.block.Name != "Bash" {
		return iv
	}
	iv.Bucket, iv.Kind = BucketBackground, b.toolKind(tu.block)
	if b.opt.Suspend > 0 && iv.Duration() > b.opt.Suspend {
		b.event(iv.Start, EventSuspension, string(BucketBackground)+":"+iv.Kind)
		iv.Bucket, iv.Kind = BucketSuspended, ""
	}
	return iv
}

func (b *builder) emitPerson(iv Interval) {
	if b.opt.Away > 0 && iv.Duration() > b.opt.Away {
		cut := iv.Start.Add(b.opt.Away)
		b.emit(Interval{Start: iv.Start, End: cut, Bucket: iv.Bucket})
		b.emit(Interval{Start: cut, End: iv.End, Bucket: BucketAway})
		b.event(iv.Start, EventAbsence, string(iv.Bucket))
		return
	}
	b.emit(iv)
}

// emit appends an interval, merging it into the previous one when they
// share a bucket and kind.
func (b *builder) emit(iv Interval) {
	if !iv.End.After(iv.Start) {
		return
	}
	if n := len(b.out); n > 0 {
		prev := &b.out[n-1]
		if prev.Bucket == iv.Bucket && prev.Kind == iv.Kind && prev.End.Equal(iv.Start) {
			prev.End = iv.End
			return
		}
	}
	b.out = append(b.out, iv)
}
