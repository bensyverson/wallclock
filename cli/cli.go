// Package cli is the thin command-line adapter over wallclock's library
// packages: it parses arguments, calls the library, and renders results.
package cli

import (
	"fmt"
	"io"
	"time"
)

// Exit codes shared across the house's CLIs (project/agents/cli-design.md).
const (
	ExitOK          = 0
	ExitNegative    = 1
	ExitUsage       = 2
	ExitConflict    = 3
	ExitTarget      = 4
	ExitEnvironment = 5
)

// Env carries the ambient inputs a run depends on, so tests can pin them.
type Env struct {
	// ProjectsDir is the Claude Code transcripts root (~/.claude/projects).
	ProjectsDir string
	// CacheDir holds per-file analysis; empty disables caching.
	CacheDir string
	// Now is the reference time for relative date ranges.
	Now time.Time
	// Scratch lists path prefixes whose sessions are scratch; nil means
	// the defaults from repo.DefaultScratch.
	Scratch []string
}

type verb struct {
	name    string
	summary string
	help    string
	run     func(args []string, stdout, stderr io.Writer, env Env) int
}

func verbs() []verb {
	return []verb{
		{"report", "per-repo breakdown of where session time went", reportHelp, runReport},
		{"sessions", "per-session breakdown, for drilling into a repo", sessionsHelp, runSessions},
		{"cache", "inspect or clear the analysis cache", cacheHelp, runCache},
	}
}

const primer = `wallclock: where did your Claude Code sessions spend their time?

Reads Claude Code transcripts and splits each session's time into waiting
on Anthropic (model calls), on the computer (tests, builds, other commands),
on the user (prompts, approvals), and everything else, per repo.

Read verbs:
  report     per-repo breakdown of where session time went
  sessions   per-session breakdown, for drilling into a repo

Management verbs:
  cache      inspect or clear the analysis cache

Start here:
  wallclock report            the past 7 days, one row per repo and bucket

Run 'wallclock <verb> --help' for flags and an example.
`

// Run executes one invocation and returns its exit code.
func Run(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, primer)
		return ExitOK
	}
	for _, v := range verbs() {
		if v.name == args[0] {
			rest := args[1:]
			if wantsHelp(rest) {
				fmt.Fprint(stdout, v.help)
				return ExitOK
			}
			return v.run(rest, stdout, stderr, env)
		}
	}
	fmt.Fprintf(stderr, "wallclock: unknown verb %q; valid verbs: report, sessions, cache\nRun 'wallclock' for an overview.\n", args[0])
	return ExitUsage
}

func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}
