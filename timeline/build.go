package timeline

import (
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
	b := &builder{opt: opt, tools: map[string]toolUse{}}
	b.collect(es)
	b.resolve(es[0].Timestamp, es[len(es)-1].Timestamp, es)
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
			for _, bl := range e.Blocks {
				if bl.Type == tr.BlockToolUse {
					b.tools[bl.ToolUseID] = toolUse{start: e.Timestamp, block: bl}
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
			b.retry(es, reqStart[id])
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
func (b *builder) retry(es []tr.Entry, start time.Time) {
	for _, e := range es {
		if !e.Timestamp.After(start) {
			continue
		}
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
// parallel tools), and classifies the uncovered gaps.
func (b *builder) resolve(first, last time.Time, es []tr.Entry) {
	cuts := []time.Time{first, last}
	for _, s := range b.spans {
		if s.End.After(s.Start) {
			cuts = append(cuts, s.Start, s.End)
		}
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].Before(cuts[j]) })
	var gapStart time.Time
	inGap := false
	for i := 0; i+1 < len(cuts); i++ {
		a, z := cuts[i], cuts[i+1]
		if !z.After(a) || a.Before(first) || z.After(last) {
			continue
		}
		owner, ok := b.owner(a, z)
		if !ok {
			if !inGap {
				gapStart, inGap = a, true
			}
			continue
		}
		if inGap {
			b.gap(gapStart, a, es)
			inGap = false
		}
		b.emit(Interval{Start: a, End: z, Bucket: owner.Bucket, Kind: owner.Kind})
	}
	if inGap {
		b.gap(gapStart, last, es)
	}
}

func (b *builder) owner(a, z time.Time) (Interval, bool) {
	var best Interval
	found := false
	for _, s := range b.spans {
		if s.Start.After(a) || s.End.Before(z) || !s.End.After(s.Start) {
			continue
		}
		if !found || priority[s.Bucket] > priority[best.Bucket] ||
			(priority[s.Bucket] == priority[best.Bucket] && s.End.After(best.End)) {
			best, found = s.Interval, true
		}
	}
	return best, found
}

// gap classifies an uncovered stretch by the entries inside it: a stop
// hook ends hook time, a prompt ends a user wait, a task notification ends
// a background or subagent wait. Once a turn has ended, the thread is
// waiting on the user until something starts the next turn. What remains
// is harness.
func (b *builder) gap(a, z time.Time, es []tr.Entry) {
	cur := a
	turnEnded := false
	localKind := ""
	for _, e := range es {
		if !e.Timestamp.After(cur) || e.Timestamp.After(z) {
			continue
		}
		switch {
		case e.Type == tr.TypeSystem && e.Subtype == tr.SubtypeStopHookSummary:
			b.emit(Interval{Start: cur, End: e.Timestamp, Bucket: BucketHook})
		case e.Type == tr.TypeSystem && e.Subtype == tr.SubtypeTurnDuration:
			b.emit(Interval{Start: cur, End: e.Timestamp, Bucket: BucketHarness})
			turnEnded = true
		case e.Interrupted || isOrphanError(e, b.reqs):
			b.emit(Interval{Start: cur, End: e.Timestamp, Bucket: BucketHarness})
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
	b.emit(Interval{Start: cur, End: z, Bucket: BucketHarness})
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
