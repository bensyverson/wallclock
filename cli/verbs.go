package cli

import (
	"fmt"
	"io"
)

const reportHelp = `wallclock report: per-repo breakdown of where session time went (read verb)

Example:
  wallclock report --days 14 --pretty
`

const sessionsHelp = `wallclock sessions: per-session breakdown (read verb)

Example:
  wallclock sessions --repo wallclock
`

const cacheHelp = `wallclock cache: inspect or clear the analysis cache (management verb)

Example:
  wallclock cache clear
`

func runReport(args []string, stdout, stderr io.Writer, env Env) int {
	fmt.Fprintln(stderr, "wallclock report: not implemented yet")
	return ExitEnvironment
}

func runSessions(args []string, stdout, stderr io.Writer, env Env) int {
	fmt.Fprintln(stderr, "wallclock sessions: not implemented yet")
	return ExitEnvironment
}

func runCache(args []string, stdout, stderr io.Writer, env Env) int {
	fmt.Fprintln(stderr, "wallclock cache: not implemented yet")
	return ExitEnvironment
}
