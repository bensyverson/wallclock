package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureEnv lays out a projects dir with one Go session and a repo it ran in.
func fixtureEnv(t *testing.T) Env {
	t.Helper()
	root := t.TempDir()
	app := filepath.Join(root, "src", "myapp")
	must(t, os.MkdirAll(filepath.Join(app, ".git"), 0o755))
	must(t, os.WriteFile(filepath.Join(app, "go.mod"), []byte("module x\n"), 0o644))
	proj := filepath.Join(root, "projects", "-src-myapp")
	must(t, os.MkdirAll(proj, 0o755))
	lines := []string{
		`{"type":"user","timestamp":"2026-11-13T19:00:00Z","cwd":"` + app + `","origin":{"kind":"human"},"message":{"role":"user","content":"go"}}`,
		`{"type":"api-request","id":"r1","timestamp":"2026-11-13T19:00:01Z","cwd":"` + app + `"}`,
		`{"type":"assistant","requestRef":"r1","timestamp":"2026-11-13T19:00:11Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","timestamp":"2026-11-13T19:00:41Z","permissionDecision":{"decision":"accept","source":"config"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1"}]}}`,
		`{"type":"api-request","id":"r2","timestamp":"2026-11-13T19:00:42Z"}`,
		`{"type":"assistant","requestRef":"r2","timestamp":"2026-11-13T19:01:00Z","message":{"content":[{"type":"text","text":"done"}]}}`,
	}
	must(t, os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	return Env{
		ProjectsDir: filepath.Join(root, "projects"),
		CacheDir:    filepath.Join(root, "cache"),
		Now:         time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
		Scratch:     []string{},
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func runEnv(env Env, args ...string) (string, string, int) {
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb, env)
	return out.String(), errb.String(), code
}

const wantReport = `# wallclock report 2026-11-08..2026-11-15; agent-time: subagent time adds up, so totals can exceed wall-clock
repo	lang	bucket	seconds	share
myapp	go	api	28	0.467
myapp	go	tool:test	30	0.500
myapp	go	harness	2	0.033
myapp	go	active	60	1.000
all	-	api	28	0.467
all	-	tool:test	30	0.500
all	-	harness	2	0.033
all	-	active	60	1.000
`

func TestReportPlain(t *testing.T) {
	out, errOut, code := runEnv(fixtureEnv(t), "report")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if out != wantReport {
		t.Errorf("got:\n%s\nwant:\n%s", out, wantReport)
	}
}

func TestReportUsesCacheOnSecondRun(t *testing.T) {
	env := fixtureEnv(t)
	first, _, _ := runEnv(env, "report")
	files, _ := filepath.Glob(filepath.Join(env.CacheDir, "v*", "*.json"))
	if len(files) != 1 {
		t.Fatalf("cache files = %v", files)
	}
	second, _, _ := runEnv(env, "report")
	if first != second {
		t.Errorf("cached run differs:\n%s\nvs\n%s", first, second)
	}
}

func TestReportJSON(t *testing.T) {
	out, _, code := runEnv(fixtureEnv(t), "report", "--json")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var v struct {
		Repos []struct {
			Repo          string `json:"repo"`
			ActiveSeconds int    `json:"active_seconds"`
		} `json:"repos"`
	}
	must(t, json.Unmarshal([]byte(out), &v))
	if len(v.Repos) != 1 || v.Repos[0].Repo != "myapp" || v.Repos[0].ActiveSeconds != 60 {
		t.Errorf("got %+v", v)
	}
}

func TestReportPretty(t *testing.T) {
	out, _, code := runEnv(fixtureEnv(t), "report", "--pretty")
	if code != ExitOK || !strings.Contains(out, "myapp (go): 1 session, 1 thread, 1m00s active") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestDateRangeFlags(t *testing.T) {
	env := fixtureEnv(t)
	out, _, code := runEnv(env, "report", "--since", "2026-11-14")
	if code != ExitOK || strings.Contains(out, "myapp") {
		t.Errorf("--since after the session should exclude it (exit %d):\n%s", code, out)
	}
	out, _, _ = runEnv(env, "report", "--since", "2026-11-13", "--until", "2026-11-13")
	if !strings.Contains(out, "myapp\tgo\tactive\t60") {
		t.Errorf("--since/--until on the session's day should include it:\n%s", out)
	}
	out, _, _ = runEnv(env, "report", "--days", "1")
	if strings.Contains(out, "myapp") {
		t.Errorf("--days 1 should exclude a session 2 days ago:\n%s", out)
	}
}

func TestRepoFilterFlag(t *testing.T) {
	out, _, _ := runEnv(fixtureEnv(t), "report", "--repo", "other")
	if strings.Contains(out, "myapp") {
		t.Errorf("--repo other still shows myapp:\n%s", out)
	}
}

func TestSessionsVerb(t *testing.T) {
	out, errOut, code := runEnv(fixtureEnv(t), "sessions")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	want := "session\trepo\tstart\tthreads\tactive_s\tapi\tcomputer\tuser\tother\n" +
		"s1\tmyapp\t2026-11-13T19:00\t1\t60\t0.467\t0.500\t0.000\t0.033\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestCacheVerb(t *testing.T) {
	env := fixtureEnv(t)
	runEnv(env, "report")
	out, _, code := runEnv(env, "cache")
	if code != ExitOK || !strings.HasPrefix(out, "cache\t"+env.CacheDir+"\t1 files\t") {
		t.Errorf("cache stats (exit %d): %q", code, out)
	}
	out, _, code = runEnv(env, "cache", "clear")
	if code != ExitOK || !strings.Contains(out, "cleared") {
		t.Errorf("cache clear (exit %d): %q", code, out)
	}
	out, _, _ = runEnv(env, "cache")
	if !strings.Contains(out, "\t0 files\t") {
		t.Errorf("after clear: %q", out)
	}
}

func TestErrorsTeach(t *testing.T) {
	env := fixtureEnv(t)
	cases := []struct {
		name string
		env  Env
		args []string
		code int
		want []string
	}{
		{"bad since", env, []string{"report", "--since", "last week"}, ExitUsage, []string{"--since", "YYYY-MM-DD"}},
		{"bad until", env, []string{"report", "--until", "11/13"}, ExitUsage, []string{"--until", "YYYY-MM-DD"}},
		{"since after until", env, []string{"report", "--since", "2026-11-14", "--until", "2026-11-13"}, ExitUsage, []string{"--since", "before"}},
		{"days and since", env, []string{"report", "--days", "3", "--since", "2026-11-13"}, ExitUsage, []string{"--days", "--since", "one"}},
		{"bad days", env, []string{"report", "--days", "0"}, ExitUsage, []string{"--days", "positive"}},
		{"json and pretty", env, []string{"report", "--json", "--pretty"}, ExitUsage, []string{"--json", "--pretty", "one"}},
		{"unknown flag", env, []string{"report", "--frob"}, ExitUsage, []string{"frob", "wallclock report --help"}},
		{"stray argument", env, []string{"report", "extra"}, ExitUsage, []string{`"extra"`, "--repo"}},
		{"missing rules", env, []string{"report", "--rules", "/nonexistent/rules.json"}, ExitTarget, []string{"rules.json", "--rules"}},
		{"bad cache verb", env, []string{"cache", "nuke"}, ExitUsage, []string{`"nuke"`, "clear"}},
		{"no projects dir", Env{ProjectsDir: "/nonexistent/projects", Now: env.Now}, []string{"report"}, ExitEnvironment, []string{"/nonexistent/projects", "WALLCLOCK_PROJECTS_DIR"}},
	}
	for _, c := range cases {
		_, errOut, code := runEnv(c.env, c.args...)
		if code != c.code {
			t.Errorf("%s: exit %d, want %d (%s)", c.name, code, c.code, errOut)
		}
		for _, w := range c.want {
			if !strings.Contains(errOut, w) {
				t.Errorf("%s: stderr missing %q: %s", c.name, w, errOut)
			}
		}
	}
}
