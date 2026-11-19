package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bensyverson/wallclock/cache"
	"github.com/bensyverson/wallclock/classify"
	"github.com/bensyverson/wallclock/repo"
	"github.com/bensyverson/wallclock/report"
	"github.com/bensyverson/wallclock/timeline"
	"github.com/bensyverson/wallclock/transcript"
)

const rangeFlagsHelp = `Range:
  --days N            the past N days, ending now (default 7)
  --since YYYY-MM-DD  start of the range (local time)
  --until YYYY-MM-DD  last day of the range, inclusive (default today)

Attribution:
  --away DURATION     cap on each wait for the user; the rest is away (default 10m)
  --suspend DURATION  longest plausible computer or API wait; longer is suspended (default 1h)
  --rules FILE        extra command-classification rules (JSON; see docs/buckets.md)

Selection and output:
  --repo NAME         only this repo
  --include-scratch   include sessions run from temp directories
  --json              stable machine-readable output
  --pretty            aligned table for people
  --no-cache          parse every transcript instead of using the cache
`

const reportHelp = `wallclock report: per-repo breakdown of where session time went (read verb)

Prints one row per repo and bucket: seconds and share of active time,
then event counts (API errors, interruptions, absences, suspensions).
Totals are agent-time: subagents' time adds up, so they can exceed
wall-clock time. docs/buckets.md defines each bucket.

` + rangeFlagsHelp + `
Example:
  wallclock report --days 14 --pretty
`

const sessionsHelp = `wallclock sessions: per-session breakdown (read verb)

Prints one row per session: start, threads (main plus subagents),
active seconds, and the share spent on the API, the computer (tools and
background commands), the user (prompts and approvals), and other
(API errors, hooks, harness).

` + rangeFlagsHelp + `
Example:
  wallclock sessions --repo myapp --days 30
`

const cacheHelp = `wallclock cache: inspect or clear the analysis cache (management verb)

  wallclock cache         print the cache directory, file count and size
  wallclock cache clear   delete every cached file

The cache lives in ~/.cache/wallclock (WALLCLOCK_CACHE_DIR overrides it).

Example:
  wallclock cache clear
`

type runFlags struct {
	opt     report.Options
	format  report.Format
	noCache bool
}

// usageError is a malformed invocation; its message carries the remedy.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

func parseRunFlags(verb string, args []string, env Env) (runFlags, error) {
	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	days := fs.Int("days", 0, "")
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	away := fs.Duration("away", 10*time.Minute, "")
	suspend := fs.Duration("suspend", time.Hour, "")
	rules := fs.String("rules", "", "")
	repoName := fs.String("repo", "", "")
	scratch := fs.Bool("include-scratch", false, "")
	asJSON := fs.Bool("json", false, "")
	pretty := fs.Bool("pretty", false, "")
	noCache := fs.Bool("no-cache", false, "")
	if err := fs.Parse(args); err != nil {
		return runFlags{}, usagef("wallclock %s: %v\nRun 'wallclock %s --help' for flags.", verb, err, verb)
	}
	if fs.NArg() > 0 {
		return runFlags{}, usagef("wallclock %s: unexpected argument %q; to pick a repo use --repo NAME", verb, fs.Arg(0))
	}
	var rf runFlags
	switch {
	case *asJSON && *pretty:
		return rf, usagef("wallclock %s: --json and --pretty conflict; pass one", verb)
	case *asJSON:
		rf.format = report.FormatJSON
	case *pretty:
		rf.format = report.FormatPretty
	}
	daysSet := false
	fs.Visit(func(f *flag.Flag) { daysSet = daysSet || f.Name == "days" })
	if daysSet && (*since != "" || *until != "") {
		return rf, usagef("wallclock %s: --days conflicts with --since/--until; pass one way of setting the range", verb)
	}
	if daysSet && *days <= 0 {
		return rf, usagef("wallclock %s: --days must be a positive number of days", verb)
	}
	loc := env.Now.Location()
	rf.opt.Until = env.Now
	rf.opt.Since = env.Now.Add(-7 * 24 * time.Hour)
	if daysSet {
		rf.opt.Since = env.Now.Add(-time.Duration(*days) * 24 * time.Hour)
	}
	if *since != "" || *until != "" {
		rf.opt.Since = time.Time{}
		if *since != "" {
			d, err := time.ParseInLocation("2006-01-02", *since, loc)
			if err != nil {
				return rf, usagef("wallclock %s: --since %q is not a date; use YYYY-MM-DD", verb, *since)
			}
			rf.opt.Since = d
		}
		if *until != "" {
			d, err := time.ParseInLocation("2006-01-02", *until, loc)
			if err != nil {
				return rf, usagef("wallclock %s: --until %q is not a date; use YYYY-MM-DD", verb, *until)
			}
			rf.opt.Until = d.AddDate(0, 0, 1)
		}
		if *since == "" {
			rf.opt.Since = rf.opt.Until.AddDate(0, 0, -7)
		}
		if !rf.opt.Since.Before(rf.opt.Until) {
			return rf, usagef("wallclock %s: --since must be on or before --until", verb)
		}
	}
	cl, err := classifier(*rules)
	if err != nil {
		return rf, err
	}
	rf.opt.Timeline = timeline.Options{
		Away:     *away,
		Suspend:  *suspend,
		Classify: func(cmd string) string { return string(cl.Classify(cmd)) },
	}
	rf.opt.Repo = *repoName
	rf.opt.IncludeScratch = *scratch
	rf.noCache = *noCache
	return rf, nil
}

// targetError is a file that could not be loaded.
type targetError struct{ msg string }

func (e targetError) Error() string { return e.msg }

func classifier(rulesPath string) (*classify.Classifier, error) {
	var extra []classify.Rule
	if rulesPath != "" {
		rules, err := classify.LoadRules(rulesPath)
		if err != nil {
			return nil, targetError{fmt.Sprintf("wallclock: cannot load --rules: %v\nCheck the path and the format in docs/buckets.md.", err)}
		}
		extra = rules
	}
	cl, err := classify.New(extra...)
	if err != nil {
		return nil, targetError{fmt.Sprintf("wallclock: --rules: %v", err)}
	}
	return cl, nil
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, err.Error())
	var ue usageError
	var te targetError
	switch {
	case errors.As(err, &ue):
		return ExitUsage
	case errors.As(err, &te):
		return ExitTarget
	}
	return ExitEnvironment
}

func collect(verb string, args []string, stderr io.Writer, env Env) (*report.Report, report.Format, int) {
	rf, err := parseRunFlags(verb, args, env)
	if err != nil {
		return nil, 0, fail(stderr, err)
	}
	sessions, err := transcript.Discover(env.ProjectsDir)
	if err != nil {
		fmt.Fprintf(stderr, "wallclock: cannot read transcripts at %s: %v\nSet WALLCLOCK_PROJECTS_DIR to Claude Code's projects directory.\n", env.ProjectsDir, err)
		return nil, 0, ExitEnvironment
	}
	sessions = modifiedSince(sessions, rf.opt.Since)
	dir := env.CacheDir
	if rf.noCache {
		dir = ""
	}
	scratch := env.Scratch
	if scratch == nil {
		scratch = repo.DefaultScratch()
	}
	rep, err := report.Collect(sessions, cache.Open(dir), repo.NewResolverWithScratch(scratch), rf.opt)
	if err != nil {
		fmt.Fprintf(stderr, "wallclock %s: %v\n", verb, err)
		return nil, 0, ExitEnvironment
	}
	return rep, rf.format, ExitOK
}

// modifiedSince drops sessions none of whose files changed since t: they
// cannot hold entries in range.
func modifiedSince(sessions []transcript.Session, t time.Time) []transcript.Session {
	var out []transcript.Session
	for _, s := range sessions {
		paths := []string{s.Path}
		for _, a := range s.Subagents {
			paths = append(paths, a.Path)
		}
		for _, p := range paths {
			if fi, err := os.Stat(p); err == nil && !fi.ModTime().Before(t) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func runReport(args []string, stdout, stderr io.Writer, env Env) int {
	rep, format, code := collect("report", args, stderr, env)
	if code != ExitOK {
		return code
	}
	if err := report.WriteRepos(stdout, rep, format); err != nil {
		return fail(stderr, err)
	}
	return ExitOK
}

func runSessions(args []string, stdout, stderr io.Writer, env Env) int {
	rep, format, code := collect("sessions", args, stderr, env)
	if code != ExitOK {
		return code
	}
	if err := report.WriteSessions(stdout, rep, format); err != nil {
		return fail(stderr, err)
	}
	return ExitOK
}

func runCache(args []string, stdout, stderr io.Writer, env Env) int {
	c := cache.Open(env.CacheDir)
	switch {
	case len(args) == 0:
		files, size, err := c.Stats()
		if err != nil {
			fmt.Fprintf(stderr, "wallclock cache: %v\n", err)
			return ExitEnvironment
		}
		fmt.Fprintf(stdout, "cache\t%s\t%d files\t%d bytes\n", env.CacheDir, files, size)
		return ExitOK
	case len(args) == 1 && args[0] == "clear":
		if err := c.Clear(); err != nil {
			fmt.Fprintf(stderr, "wallclock cache clear: %v\n", err)
			return ExitEnvironment
		}
		fmt.Fprintf(stdout, "cleared %s\n", env.CacheDir)
		return ExitOK
	}
	fmt.Fprintf(stderr, "wallclock cache: unknown subcommand %q; use 'wallclock cache' or 'wallclock cache clear'\n", strings.Join(args, " "))
	return ExitUsage
}
