// Command wallclock reports where Claude Code session time went.
package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/bensyverson/wallclock/cli"
)

func main() {
	home, _ := os.UserHomeDir()
	cache := os.Getenv("WALLCLOCK_CACHE_DIR")
	if cache == "" {
		if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
			cache = filepath.Join(xdg, "wallclock")
		} else {
			cache = filepath.Join(home, ".cache", "wallclock")
		}
	}
	projects := os.Getenv("WALLCLOCK_PROJECTS_DIR")
	if projects == "" {
		projects = filepath.Join(home, ".claude", "projects")
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, cli.Env{
		ProjectsDir: projects,
		CacheDir:    cache,
		Now:         time.Now(),
	}))
}
