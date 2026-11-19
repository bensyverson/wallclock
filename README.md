# wallclock

See where your Claude Code sessions spend their time: thinking, waiting on your computer, or waiting on you.

wallclock reads the transcripts Claude Code keeps in `~/.claude/projects` and splits each session's time into buckets: waiting on Anthropic (model calls and API errors), waiting on the computer (tests, builds, other commands), waiting on you (prompts and approvals), and the rest. It reports per repo over a date range, so you can compare, say, a Swift project with a Go one, and see how much of Claude's time went to a slow build instead of to thinking.

## Install

```sh
go install github.com/bensyverson/wallclock/cmd/wallclock@latest
```

## Quick start

```sh
wallclock report                 # past 7 days, one row per repo and bucket
wallclock report --days 14 --pretty
wallclock sessions --repo myapp  # drill into one repo's sessions
```

The first run reads every transcript in range; later runs reuse a cache in `~/.cache/wallclock/` (set `WALLCLOCK_CACHE_DIR` to move it).

## More

- [docs/buckets.md](docs/buckets.md): what each bucket measures, and how subagents and long absences are counted.

## Author and license

By Ben Syverson. MIT License; see [LICENSE](LICENSE).
