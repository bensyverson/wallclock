package repo

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// newTestResolver treats nothing as scratch so t.TempDir() fixtures, which
// live under the OS temp dir, resolve like ordinary repos.
func newTestResolver() *Resolver {
	return NewResolverWithScratch(nil)
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func write(t *testing.T, path, body string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlainRepo(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myapp")
	mkdir(t, root, ".git")
	write(t, filepath.Join(root, "go.mod"), "module x\n")

	info := newTestResolver().Resolve(root)
	if info.Root != root || info.Name != "myapp" {
		t.Errorf("root/name = %q/%q", info.Root, info.Name)
	}
	if !info.Verified || !info.InRepo || info.Scratch {
		t.Errorf("flags wrong: %+v", info)
	}
	if info.Language != LangGo {
		t.Errorf("language = %q", info.Language)
	}
}

func TestSubdirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	mkdir(t, root, ".git")
	sub := mkdir(t, root, "internal", "deep")

	info := newTestResolver().Resolve(sub)
	if info.Root != root || info.Name != "proj" || !info.Verified {
		t.Errorf("got %+v", info)
	}
}

func TestWorktreeWithCommondirFile(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "myapp")
	mkdir(t, main, ".git", "worktrees", "agent-a1b2")
	write(t, filepath.Join(main, ".git", "worktrees", "agent-a1b2", "commondir"), "../..\n")
	wt := filepath.Join(main, ".claude", "worktrees", "agent-a1b2")
	write(t, filepath.Join(wt, ".git"), "gitdir: "+filepath.Join(main, ".git", "worktrees", "agent-a1b2")+"\n")
	sub := mkdir(t, wt, "pkg")

	for _, cwd := range []string{wt, sub} {
		info := newTestResolver().Resolve(cwd)
		if info.Root != main || info.Name != "myapp" || !info.Verified {
			t.Errorf("cwd %s: got %+v", cwd, info)
		}
	}
}

func TestWorktreeWithoutCommondirFile(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "main")
	mkdir(t, main, ".git", "worktrees", "feat")
	wt := filepath.Join(base, "elsewhere", "feat")
	write(t, filepath.Join(wt, ".git"), "gitdir: "+filepath.Join(main, ".git", "worktrees", "feat")+"\n")

	info := newTestResolver().Resolve(wt)
	if info.Root != main || info.Name != "main" {
		t.Errorf("got %+v", info)
	}
}

func TestWorktreeRelativeGitdir(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "main")
	mkdir(t, main, ".git", "worktrees", "feat")
	wt := filepath.Join(base, "feat")
	write(t, filepath.Join(wt, ".git"), "gitdir: ../main/.git/worktrees/feat\n")

	info := newTestResolver().Resolve(wt)
	if info.Root != main {
		t.Errorf("got %+v", info)
	}
}

func TestSubmoduleGitFileIsItsOwnRoot(t *testing.T) {
	base := t.TempDir()
	super := filepath.Join(base, "super")
	mkdir(t, super, ".git", "modules", "lib")
	lib := filepath.Join(super, "lib")
	write(t, filepath.Join(lib, ".git"), "gitdir: ../.git/modules/lib\n")

	info := newTestResolver().Resolve(lib)
	if info.Root != lib || info.Name != "lib" {
		t.Errorf("got %+v", info)
	}
}

func TestExistingClaudeWorktreeDirWithoutGitFolds(t *testing.T) {
	main := filepath.Join(t.TempDir(), "myapp")
	mkdir(t, main, ".git")
	wt := mkdir(t, main, ".claude", "worktrees", "agent-zz")

	info := newTestResolver().Resolve(wt)
	if info.Root != main || !info.Verified {
		t.Errorf("got %+v", info)
	}
}

func TestMissingDirectoryFallsBackToHeuristics(t *testing.T) {
	base := t.TempDir()
	r := newTestResolver()

	gone := filepath.Join(base, "gone", "project")
	info := r.Resolve(gone)
	if info.Root != gone || info.Name != "project" || info.Verified || info.InRepo {
		t.Errorf("plain missing: %+v", info)
	}
	if info.Language != LangUnknown {
		t.Errorf("language = %q", info.Language)
	}

	wt := filepath.Join(base, "gone", "myapp", ".claude", "worktrees", "agent-a1b2", "sub")
	info = r.Resolve(wt)
	want := filepath.Join(base, "gone", "myapp")
	if info.Root != want || info.Name != "myapp" || info.Verified {
		t.Errorf("missing worktree: %+v", info)
	}
}

func TestMissingWorktreeOfExistingRepoStillDetectsLanguage(t *testing.T) {
	main := filepath.Join(t.TempDir(), "myapp")
	mkdir(t, main, ".git")
	write(t, filepath.Join(main, "Cargo.toml"), "")
	info := newTestResolver().Resolve(filepath.Join(main, ".claude", "worktrees", "agent-gone"))
	if info.Root != main || info.Verified || info.Language != LangRust {
		t.Errorf("got %+v", info)
	}
}

func TestNoGitDirectory(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "loose")
	info := newTestResolver().Resolve(dir)
	if info.Root != dir || info.InRepo || !info.Verified {
		t.Errorf("got %+v", info)
	}
}

func TestScratchPaths(t *testing.T) {
	r := NewResolverWithScratch([]string{"/tmp", "/private/tmp", "/var/folders", "/custom/tmpdir"})
	cases := map[string]bool{
		"/tmp/foo":                   true,
		"/tmp":                       true,
		"/private/tmp/claude-501/x":  true,
		"/var/folders/ab/cd/T/thing": true,
		"/custom/tmpdir/run1":        true,
		"/tmpfoo/bar":                false,
		"/home/dev/src/myapp":        false,
		"/Users/ben/tmp/foo":         false,
		"/home/dev/src/x/.claude/worktrees/a/tmp": false,
	}
	for cwd, want := range cases {
		if got := r.Resolve(cwd).Scratch; got != want {
			t.Errorf("Scratch(%q) = %v, want %v", cwd, got, want)
		}
	}
}

func TestDefaultResolverFlagsEnvTmpdir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	r := NewResolver()
	if !r.Resolve(filepath.Join(dir, "x")).Scratch {
		t.Error("paths under $TMPDIR should be scratch")
	}
	if !r.Resolve("/private/tmp/claude-501/foo").Scratch {
		t.Error("/private/tmp should be scratch")
	}
	if r.Resolve("/home/dev/src/myapp").Scratch {
		t.Error("ordinary path should not be scratch")
	}
}

func TestLanguageDetection(t *testing.T) {
	cases := []struct {
		name  string
		files []string // relative to root; trailing "/" makes a directory
		want  Lang
		all   []Lang
	}{
		{"go", []string{"go.mod"}, LangGo, []Lang{LangGo}},
		{"swift package", []string{"Package.swift"}, LangSwift, []Lang{LangSwift}},
		{"xcodeproj", []string{"App.xcodeproj/"}, LangSwift, []Lang{LangSwift}},
		{"xcworkspace", []string{"App.xcworkspace/"}, LangSwift, []Lang{LangSwift}},
		{"rust", []string{"Cargo.toml"}, LangRust, []Lang{LangRust}},
		{"pyproject", []string{"pyproject.toml"}, LangPython, []Lang{LangPython}},
		{"setup.py", []string{"setup.py"}, LangPython, []Lang{LangPython}},
		{"requirements", []string{"requirements.txt"}, LangPython, []Lang{LangPython}},
		{"js", []string{"package.json"}, LangJS, []Lang{LangJS}},
		{"several", []string{"package.json", "Cargo.toml", "go.mod", "pyproject.toml", "setup.py"}, LangGo, []Lang{LangGo, LangRust, LangPython, LangJS}},
		{"swift before js", []string{"package.json", "Package.swift"}, LangSwift, []Lang{LangSwift, LangJS}},
		{"none", []string{"README.md"}, LangUnknown, nil},
	}
	for _, tc := range cases {
		root := filepath.Join(t.TempDir(), "r")
		mkdir(t, root, ".git")
		for _, f := range tc.files {
			p := filepath.Join(root, f)
			if f[len(f)-1] == '/' {
				mkdir(t, p)
			} else {
				write(t, p, "")
			}
		}
		info := newTestResolver().Resolve(root)
		if info.Language != tc.want || !reflect.DeepEqual(info.Languages, tc.all) {
			t.Errorf("%s: got %q %v, want %q %v", tc.name, info.Language, info.Languages, tc.want, tc.all)
		}
	}
}

func TestLanguageComesFromRootNotSubdirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "r")
	mkdir(t, root, ".git")
	write(t, filepath.Join(root, "go.mod"), "")
	sub := mkdir(t, root, "web")
	write(t, filepath.Join(sub, "package.json"), "{}")

	info := newTestResolver().Resolve(sub)
	if info.Language != LangGo || len(info.Languages) != 1 {
		t.Errorf("got %q %v", info.Language, info.Languages)
	}
}

func TestResolverMemoizes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "r")
	mkdir(t, root, ".git")
	r := newTestResolver()
	first := r.Resolve(root)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	second := r.Resolve(root)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("cache miss: %+v vs %+v", first, second)
	}
	if !second.Verified {
		t.Error("memoized result should be returned unchanged")
	}
}
