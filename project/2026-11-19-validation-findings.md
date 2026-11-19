# Validation on real transcripts (2026-11-19)

**Outcome.** After six attribution fixes, wallclock explains all but 1.2% of active time across 14 days of local transcripts (30 sessions, 125 threads, 41.5 h of active agent-time). Its API totals agree with Claude Code's own `cost-state` within about 8%. Across everything, agents spent **38% of active time waiting on Anthropic, 35% on the computer, and 24% on the user.**

Reproduce with the binary built from this commit, run against `~/.claude/projects` on 2026-11-19:

```sh
wallclock report --days 14            # per-repo buckets
wallclock report --days 14 --json     # shares below were computed from this
go run ./scripts/gaps -bucket harness # largest unexplained gaps
go run ./scripts/commands -kind bash  # unclassified commands by wait time
go run ./scripts/crosscheck           # comparison with cost-state
```

Repos are described by profile, never by name (AGENTS.md ruling).

## What the first run got wrong

The first real run put **82% of active time in `harness`** (178 h of 216 h). `scripts/gaps` showed the causes. Each was fixed test-first in c231270:

1. **Idle after a turn ended.** Days-long gaps after `turn_duration` that ended in something other than a human prompt (an `away_summary`, a "Remote Control disconnected" notice, end of file) fell through to harness. Rule now: once a turn ends, the thread waits on the user until the next turn starts.
2. **Prompts without `origin: human`.** The user's own `!` shell commands (`<bash-input>`) and slash commands carry no origin. They now end a user wait, and the shell command's run time counts as typed tool time.
3. **Interruptions and request-less API errors** (an expired login) now end the turn too.
4. **Unanswered requests.** One session waited 10 minutes on a request that timed out and was re-sent. That is now `api_error:retry`, but only when the wait is 5 s or more: 74 of the first 76 "retries" were requests superseded within a second, which is routine.
5. **Old transcripts** (Claude Code 2.1.257) log no `api-request` entries, so their API time read as zero. Responses are now grouped by Anthropic's `requestId`.
6. **Polling loops** (`until …; do sleep`, `gh run watch`) were the largest unclassified Bash wait (about 40 min). They are now `tool:wait`.

## Cross-check with cost-state

`scripts/crosscheck`, 28 sessions with a `cost-state` entry. Ratios are wallclock ÷ Claude Code.

- **API:** typically 0.86–0.95. wallclock measures from the logged request to the last response block. Claude Code's figure plausibly also includes side calls (classifier, titles) that this doesn't time. One outlier at 1.86 is the 10-minute timeout above, which wallclock deliberately counts as a wait on Anthropic and Claude Code doesn't.
- **Tool:** typically 1.03–1.21. wallclock counts from the `tool_use` entry to the result, which includes harness overhead around the call. Large outliers (4×, 33×) are sessions where the user ran their own `!` commands, which Claude Code's tool total excludes. The one legacy-format session reads 1.69 and is unexplained.

## Profiles (14 days)

| Profile | Sessions / threads | Active | Anthropic | Computer | User |
|---|---|---|---|---|---|
| Large Go web service, heavy subagent use | 17 / 86 | 31.2 h | 39% | 40% | 19% |
| Rust emulator toolchain, subagents | 2 / 15 | 4.6 h | 49% | 35% | 15% |
| Small Go CLI (this repo) | 1 / 4 | 0.6 h | 70% | 13% | 17% |
| Docs, planning and a Rust/Python repo: short, conversational sessions | 6 / 15 | 4.7 h | 2–47% | 0–12% | 51–96% |

Four smaller repos (under 0.2 h each) are omitted.

In the large Go service, computer time was mostly tests (11% foreground plus 4.5% backgrounded), unclassified Bash (6%), git (4.5%) and lint (2.4%). That repo is where a faster machine or faster test suite would pay off most. Short planning sessions are dominated by waiting on the user, as expected.

No Swift data: the heavy Swift/Xcode repo's transcripts had been cleared, so the Swift-versus-Go comparison waits on new sessions.

## Known limits

- **Approval time includes run time.** The transcript doesn't record when the person answered, so an approved command's whole interval counts as `approval`.
- **Suspension is a heuristic.** Any computer or API wait over 1 h (`--suspend`) is treated as a sleeping machine, as is a "went to sleep" API error. A genuine 90-minute build would be misfiled. macOS's sleep log could make this exact (backlog).
- **Language** comes from root marker files; repos without one read `unknown`, and a mixed Rust/Python repo reads as Rust.
