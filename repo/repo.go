// Package repo resolves a session's working directory to a repository
// identity and language tag, so reports can group time per repo.
package repo

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Info describes the repository a working directory belongs to.
type Info struct {
	Root      string // main repo root, with worktrees folded in
	Name      string // base name of Root
	Verified  bool   // Root was confirmed on disk, not guessed from the path
	InRepo    bool   // a .git was found at or above the directory
	Scratch   bool   // under a temp directory; reports usually exclude these
	Language  Lang   // first of Languages, or LangUnknown
	Languages []Lang // every marker found at Root, in priority order
}

// Resolver turns working directories into Info, remembering each answer
// because many transcript entries share a cwd. Safe for concurrent use.
type Resolver struct {
	scratch []string
	mu      sync.Mutex
	cache   map[string]Info
}

// defaultScratch are the temp locations flagged as scratch on every machine.
var defaultScratch = []string{"/tmp", "/private/tmp", "/var/folders"}

// NewResolver returns a Resolver that flags the usual temp directories and
// $TMPDIR as scratch.
func NewResolver() *Resolver {
	return NewResolverWithScratch(DefaultScratch())
}

// DefaultScratch returns the usual temp directories plus $TMPDIR.
func DefaultScratch() []string {
	prefixes := append([]string(nil), defaultScratch...)
	if t := os.Getenv("TMPDIR"); t != "" {
		prefixes = append(prefixes, t)
	}
	return prefixes
}

// NewResolverWithScratch returns a Resolver that flags paths under the given
// prefixes as scratch.
func NewResolverWithScratch(prefixes []string) *Resolver {
	clean := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if p = filepath.Clean(p); p != "." && p != "/" {
			clean = append(clean, p)
		}
	}
	return &Resolver{scratch: clean, cache: map[string]Info{}}
}

// Resolve returns the repo identity for cwd.
func (r *Resolver) Resolve(cwd string) Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info, ok := r.cache[cwd]; ok {
		return info
	}
	info := r.resolve(cwd)
	r.cache[cwd] = info
	return info
}

func (r *Resolver) resolve(cwd string) Info {
	cwd = filepath.Clean(cwd)
	var info Info
	if isDir(cwd) {
		info.Root, info.InRepo = findRoot(cwd)
		info.Root = foldWorktree(info.Root)
		info.Verified = isDir(info.Root)
	} else {
		info.Root = foldWorktree(cwd)
	}
	info.Name = filepath.Base(info.Root)
	info.Scratch = r.isScratch(cwd)
	info.Languages = detectLanguages(info.Root)
	info.Language = LangUnknown
	if len(info.Languages) > 0 {
		info.Language = info.Languages[0]
	}
	return info
}

func (r *Resolver) isScratch(p string) bool {
	for _, s := range r.scratch {
		if p == s || strings.HasPrefix(p, s+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// foldWorktree maps <repo>/.claude/worktrees/<name>[/...] to <repo>.
func foldWorktree(p string) string {
	sep := string(filepath.Separator)
	marker := sep + filepath.Join(".claude", "worktrees") + sep
	if i := strings.Index(p+sep, marker); i > 0 {
		return p[:i]
	}
	return p
}

// findRoot walks up from dir to the nearest .git. For a worktree's .git file
// it returns the main repo root. With no .git it returns dir and false.
func findRoot(dir string) (string, bool) {
	for d := dir; ; d = filepath.Dir(d) {
		gitPath := filepath.Join(d, ".git")
		if fi, err := os.Lstat(gitPath); err == nil {
			if fi.IsDir() {
				return d, true
			}
			return rootFromGitFile(d, gitPath), true
		}
		if filepath.Dir(d) == d {
			return dir, false
		}
	}
}

// rootFromGitFile follows "gitdir: <path>" in a .git file. A linked worktree
// points at <common>/worktrees/<name>; a submodule points elsewhere and is
// its own root.
func rootFromGitFile(dir, gitFile string) string {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return dir
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
	target, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return dir
	}
	gitdir := strings.TrimSpace(target)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	gitdir = filepath.Clean(gitdir)

	common := ""
	if b, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		common = strings.TrimSpace(string(b))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitdir, common)
		}
		common = filepath.Clean(common)
	} else if filepath.Base(filepath.Dir(gitdir)) == "worktrees" {
		common = filepath.Dir(filepath.Dir(gitdir))
	}
	if common == "" {
		return dir
	}
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return common // bare repository
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
