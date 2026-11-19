package report

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bensyverson/wallclock/repo"
	"github.com/bensyverson/wallclock/timeline"
	tr "github.com/bensyverson/wallclock/transcript"
)

var base = time.Date(2026, 11, 13, 19, 0, 0, 0, time.UTC)

func at(sec float64) time.Time { return base.Add(time.Duration(sec * float64(time.Second))) }

func e(typ tr.Type, sec float64) tr.Entry {
	return tr.Entry{Type: typ, Timestamp: at(sec), HasTimestamp: true}
}

func req(sec float64, id, cwd string) tr.Entry {
	x := e(tr.TypeAPIRequest, sec)
	x.RequestID, x.Cwd = id, cwd
	return x
}

func asst(sec float64, ref string, blocks ...tr.Block) tr.Entry {
	x := e(tr.TypeAssistant, sec)
	x.RequestRef, x.Blocks = ref, blocks
	return x
}

func bash(id, cmd string) tr.Block {
	return tr.Block{Type: tr.BlockToolUse, ToolUseID: id, Name: "Bash", Command: cmd}
}

func result(sec float64, id string) tr.Entry {
	x := e(tr.TypeUser, sec)
	x.Blocks = []tr.Block{{Type: tr.BlockToolResult, ToolUseID: id}}
	return x
}

func human(sec float64) tr.Entry {
	x := e(tr.TypeUser, sec)
	x.Origin = tr.OriginHuman
	return x
}

func apiErr(sec float64, ref string) tr.Entry {
	x := asst(sec, ref)
	x.IsAPIError, x.Text = true, "API Error: 529 Overloaded"
	return x
}

type fakeLoader map[string]*tr.File

func (f fakeLoader) Load(path string) (*tr.File, error) {
	if file, ok := f[path]; ok {
		return file, nil
	}
	return nil, errors.New("no such file")
}

type fakeResolver map[string]repo.Info

func (f fakeResolver) Resolve(cwd string) repo.Info { return f[cwd] }

func classify(cmd string) string {
	if strings.HasPrefix(cmd, "go test") {
		return "test"
	}
	return "bash"
}

func opts() Options {
	return Options{
		Since:    base.Add(-24 * time.Hour),
		Until:    base.Add(24 * time.Hour),
		Timeline: timeline.Options{Away: 10 * time.Minute, Suspend: time.Hour, Classify: classify},
	}
}

// fixture: one Go session with a subagent, one Swift session, one scratch session.
func fixture() ([]tr.Session, fakeLoader, fakeResolver) {
	loader := fakeLoader{
		// main: api 0-10, tool:test 10-16, harness 16-17? no: user 16-20 then api 20-30
		"/p/a/s1.jsonl": {Entries: []tr.Entry{
			req(0, "r1", "/src/goapp"), asst(10, "r1", bash("t1", "go test ./...")),
			result(16, "t1"),
			human(20), req(20, "r2", "/src/goapp"), asst(30, "r2"),
		}},
		// subagent overlapping in wall time: api 5-15, api_error 15-20
		"/p/a/s1/subagents/agent-x.jsonl": {Entries: []tr.Entry{
			req(5, "q1", "/src/goapp/.claude/worktrees/agent-x"), asst(15, "q1"),
			req(15, "q2", ""), apiErr(20, "q2"),
		}},
		"/p/b/s2.jsonl": {Entries: []tr.Entry{
			req(100, "r1", "/src/swiftapp"), asst(104, "r1", bash("t1", "xcodebuild")),
			result(124, "t1"),
		}},
		"/p/c/s3.jsonl": {Entries: []tr.Entry{
			req(0, "r1", "/private/tmp/x"), asst(50, "r1"),
		}},
	}
	resolver := fakeResolver{
		"/src/goapp":     {Root: "/src/goapp", Name: "goapp", Language: repo.LangGo},
		"/src/swiftapp":  {Root: "/src/swiftapp", Name: "swiftapp", Language: repo.LangSwift},
		"/private/tmp/x": {Root: "/private/tmp/x", Name: "x", Scratch: true},
	}
	sessions := []tr.Session{
		{ID: "s1", Path: "/p/a/s1.jsonl", Subagents: []tr.Subagent{{AgentID: "x", Path: "/p/a/s1/subagents/agent-x.jsonl"}}},
		{ID: "s2", Path: "/p/b/s2.jsonl"},
		{ID: "s3", Path: "/p/c/s3.jsonl"},
	}
	return sessions, loader, resolver
}

func collect(t *testing.T, o Options) *Report {
	t.Helper()
	sessions, loader, resolver := fixture()
	rep, err := Collect(sessions, loader, resolver, o)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func fsecs(d time.Duration) float64 { return d.Seconds() }

func TestSubagentTimeAddsUp(t *testing.T) {
	rep := collect(t, opts())
	if len(rep.Repos) != 2 {
		t.Fatalf("repos = %+v", rep.Repos)
	}
	g := rep.Repos[0]
	if g.Repo != "goapp" || g.Language != "go" || g.Sessions != 1 || g.Threads != 2 {
		t.Errorf("goapp summary = %+v", g)
	}
	// main api 10+10, subagent api 10 → 30; tool:test 6; user 4; api_error 5.
	want := map[Key]float64{"api": 30, "tool:test": 6, "user": 4, "api_error:overloaded": 5}
	for k, w := range want {
		if got := fsecs(g.Buckets[k]); got != w {
			t.Errorf("goapp %s = %v, want %v", k, got, w)
		}
	}
	if got := fsecs(g.Active()); got != 45 {
		t.Errorf("goapp active = %v, want 45", got)
	}
	if g.Events["api_error:overloaded"] != 1 {
		t.Errorf("events = %+v", g.Events)
	}
}

func TestReposSortedByActiveTimeAndAllSums(t *testing.T) {
	rep := collect(t, opts())
	if rep.Repos[0].Repo != "goapp" || rep.Repos[1].Repo != "swiftapp" {
		t.Errorf("order = %s, %s", rep.Repos[0].Repo, rep.Repos[1].Repo)
	}
	if got := fsecs(rep.All.Active()); got != 45+24 {
		t.Errorf("all active = %v, want 69", got)
	}
	if rep.All.Sessions != 2 || rep.All.Threads != 3 {
		t.Errorf("all = %+v", rep.All)
	}
}

func TestScratchExcludedUnlessAsked(t *testing.T) {
	o := opts()
	o.IncludeScratch = true
	rep := collect(t, o)
	if len(rep.Repos) != 3 {
		t.Errorf("repos with scratch = %d, want 3", len(rep.Repos))
	}
}

func TestRepoFilter(t *testing.T) {
	o := opts()
	o.Repo = "swiftapp"
	rep := collect(t, o)
	if len(rep.Repos) != 1 || rep.Repos[0].Repo != "swiftapp" {
		t.Errorf("repos = %+v", rep.Repos)
	}
}

func TestIntervalsClippedToRange(t *testing.T) {
	o := opts()
	o.Since = at(5)
	o.Until = at(13)
	o.Repo = "goapp"
	rep := collect(t, o)
	g := rep.Repos[0]
	// main: api 5-10, tool 10-13; subagent: api 5-13.
	if fsecs(g.Buckets["api"]) != 13 || fsecs(g.Buckets["tool:test"]) != 3 {
		t.Errorf("buckets = %+v", g.Buckets)
	}
	if g.Events["api_error:overloaded"] != 0 {
		t.Errorf("event outside range counted: %+v", g.Events)
	}
}

func TestSessionsOutsideRangeDropped(t *testing.T) {
	o := opts()
	o.Since = base.Add(48 * time.Hour)
	o.Until = base.Add(72 * time.Hour)
	if rep := collect(t, o); len(rep.Repos) != 0 || len(rep.Sessions) != 0 {
		t.Errorf("got %+v", rep)
	}
}

func TestSessionRows(t *testing.T) {
	rep := collect(t, opts())
	if len(rep.Sessions) != 2 {
		t.Fatalf("sessions = %+v", rep.Sessions)
	}
	s := rep.Sessions[0]
	if s.ID != "s1" || s.Repo != "goapp" || !s.Start.Equal(at(0)) || s.Threads != 2 {
		t.Errorf("session row = %+v", s)
	}
}

func TestUnreadableFileRecordedNotFatal(t *testing.T) {
	sessions, loader, resolver := fixture()
	sessions = append(sessions, tr.Session{ID: "gone", Path: "/p/d/gone.jsonl"})
	rep, err := Collect(sessions, loader, resolver, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Unreadable) != 1 || rep.Unreadable[0] != "/p/d/gone.jsonl" {
		t.Errorf("unreadable = %+v", rep.Unreadable)
	}
}

const wantPlain = `# wallclock report 2026-11-12..2026-11-14; agent-time: subagent time adds up, so totals can exceed wall-clock
repo	lang	bucket	seconds	share
goapp	go	api	30	0.667
goapp	go	api_error:overloaded	5	0.111
goapp	go	tool:test	6	0.133
goapp	go	user	4	0.089
goapp	go	active	45	1.000
swiftapp	swift	api	4	0.167
swiftapp	swift	tool:build	20	0.833
swiftapp	swift	active	24	1.000
all	-	api	34	0.493
all	-	api_error:overloaded	5	0.072
all	-	tool:build	20	0.290
all	-	tool:test	6	0.087
all	-	user	4	0.058
all	-	active	69	1.000

repo	lang	event	count
goapp	go	api_error:overloaded	1
all	-	api_error:overloaded	1
`

func xcodeClassify(cmd string) string {
	if cmd == "xcodebuild" {
		return "build"
	}
	return classify(cmd)
}

func TestWritePlain(t *testing.T) {
	o := opts()
	o.Timeline.Classify = xcodeClassify
	rep := collect(t, o)
	var buf bytes.Buffer
	if err := WriteRepos(&buf, rep, FormatPlain); err != nil {
		t.Fatal(err)
	}
	if buf.String() != wantPlain {
		t.Errorf("plain output:\n%s\nwant:\n%s", buf.String(), wantPlain)
	}
}

const wantPretty = `wallclock: 2026-11-12 to 2026-11-14
Agent-time: subagent time adds up, so totals can exceed wall-clock.

goapp (go): 1 session, 2 threads, 45s active
  api                     30s   66.7%  #############
  api_error:overloaded     5s   11.1%  ##
  tool:test                6s   13.3%  ###
  user                     4s    8.9%  ##
  events: api_error:overloaded 1

swiftapp (swift): 1 session, 1 thread, 24s active
  api                      4s   16.7%  ###
  tool:build              20s   83.3%  #################

all: 2 sessions, 3 threads, 1m09s active
  api                     34s   49.3%  ##########
  api_error:overloaded     5s    7.2%  #
  tool:build              20s   29.0%  ######
  tool:test                6s    8.7%  ##
  user                     4s    5.8%  #
  events: api_error:overloaded 1
`

func TestWritePretty(t *testing.T) {
	o := opts()
	o.Timeline.Classify = xcodeClassify
	rep := collect(t, o)
	var buf bytes.Buffer
	if err := WriteRepos(&buf, rep, FormatPretty); err != nil {
		t.Fatal(err)
	}
	if buf.String() != wantPretty {
		t.Errorf("pretty output:\n%s\nwant:\n%s", buf.String(), wantPretty)
	}
}

func TestWriteJSON(t *testing.T) {
	rep := collect(t, opts())
	var buf bytes.Buffer
	if err := WriteRepos(&buf, rep, FormatJSON); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`"since": "2026-11-12T19:00:00Z"`, `"repo": "goapp"`, `"bucket": "tool:test"`, `"seconds": 6`, `"active_seconds": 45`, `"event": "api_error:overloaded"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON missing %s:\n%s", want, out)
		}
	}
}

const wantSessions = `session	repo	start	threads	active_s	api	computer	user	other
s1	goapp	2026-11-13T19:00	2	45	0.667	0.133	0.089	0.111
s2	swiftapp	2026-11-13T19:01	1	24	0.167	0.833	0.000	0.000
`

func TestWriteSessionsPlain(t *testing.T) {
	o := opts()
	o.Timeline.Classify = xcodeClassify
	rep := collect(t, o)
	var buf bytes.Buffer
	if err := WriteSessions(&buf, rep, FormatPlain); err != nil {
		t.Fatal(err)
	}
	if buf.String() != wantSessions {
		t.Errorf("sessions output:\n%s\nwant:\n%s", buf.String(), wantSessions)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0: "0s", 4 * time.Second: "4s", 69 * time.Second: "1m09s",
		time.Hour + 2*time.Minute + 30*time.Second: "1h02m", 50 * time.Hour: "50h00m",
	}
	for d, w := range cases {
		if got := FormatDuration(d); got != w {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, w)
		}
	}
}

// Regression (review 2026-11-19): repos sharing a base name merged.
func TestSameBaseNameReposStaySeparate(t *testing.T) {
	loader := fakeLoader{
		"/p/1.jsonl": {Entries: []tr.Entry{req(0, "r", "/a/myapp"), asst(10, "r")}},
		"/p/2.jsonl": {Entries: []tr.Entry{req(0, "r", "/b/myapp"), asst(20, "r")}},
	}
	resolver := fakeResolver{
		"/a/myapp": {Root: "/a/myapp", Name: "myapp", Language: repo.LangGo},
		"/b/myapp": {Root: "/b/myapp", Name: "myapp", Language: repo.LangSwift},
	}
	rep, err := Collect([]tr.Session{{ID: "1", Path: "/p/1.jsonl"}, {ID: "2", Path: "/p/2.jsonl"}}, loader, resolver, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Repos) != 2 || rep.Repos[0].Repo != "b/myapp" || rep.Repos[1].Repo != "a/myapp" || rep.Repos[0].Language != "swift" {
		t.Errorf("repos = %+v", rep.Repos)
	}
}

// Regression (review 2026-11-19): a session with no cwd resolved against
// wallclock's own working directory.
func TestSessionWithoutCwdIsUnknown(t *testing.T) {
	loader := fakeLoader{"/p/1.jsonl": {Entries: []tr.Entry{req(0, "r", ""), asst(10, "r")}}}
	rep, err := Collect([]tr.Session{{ID: "1", Path: "/p/1.jsonl"}}, loader, fakeResolver{"": {Root: "/somewhere", Name: "somewhere"}}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Repos) != 1 || rep.Repos[0].Repo != UnknownRepo || rep.Repos[0].Language != "unknown" {
		t.Errorf("repos = %+v", rep.Repos)
	}
}

func TestRepoNamesInRangeListedWhenFilterMisses(t *testing.T) {
	o := opts()
	o.Repo = "goap"
	rep := collect(t, o)
	if len(rep.Repos) != 0 || len(rep.InRange) != 2 || rep.InRange[0] != "goapp" || rep.InRange[1] != "swiftapp" {
		t.Errorf("repos = %+v, in range = %v", rep.Repos, rep.InRange)
	}
}
