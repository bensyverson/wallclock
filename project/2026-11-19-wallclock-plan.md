# wallclock: where a Claude Code session's time goes

**Goal.** A Go CLI that reads Claude Code transcripts (`~/.claude/projects/**.jsonl`) and reports, per repo and over a date range, how wall-clock time split between Anthropic (model calls), the computer (commands, tests, builds), the user (prompts and approvals), and everything else. The point is to show how much time Claude spends waiting on a slow computer; ideally most active time goes to Anthropic.

## Rulings (Ben, 2026-11-19)

1. **Subagent time adds up.** Every agent thread (the main session plus each subagent transcript) contributes its own buckets, and they are summed. Four CPU-bound subagents waiting on four builds count four times. A thread that is idle waiting on a subagent's notification counts **zero**. The total is therefore *agent-time*, which can exceed wall-clock time; reports say so.
2. **Approval is its own bucket**, separate from waiting on a prompt.
3. **User wait is capped at 10 minutes per gap.** A gap of 10 minutes or less counts in full as user wait. A longer gap contributes 10 minutes of user wait, and the remainder goes to an **away** bucket that is reported but excluded from the active total. The threshold is a flag (`--away`).
4. **Commands are grouped by type** using built-in patterns covering Go, Swift, Rust and Python (test, build, lint/format, package, VCS, other), overridable by a config file.
5. **Repo identity**: resolve each entry's `cwd` to its git root; fold worktrees and subdirectories into the main repo; exclude `/private/tmp`/`/tmp` scratch sessions by default. Tag each repo with its primary language from marker files.
6. **Output**: plain rows by default (easy for agents to parse), `--json` for a stable machine shape, `--pretty` for an aligned human table. Follows `project/agents/cli-design.md`.
7. **API errors** (overloads, rate limits, sleep-interrupted responses, login expiry) are counted and timed separately from normal generation.
8. **Defaults**: last 7 days (`--days`, `--since`, `--until`); cache in `~/.cache/wallclock/` (override with `WALLCLOCK_CACHE_DIR`); binary named `wallclock`.

## What the transcripts give us (spike, 2026-11-19)

Surveyed 122 non-scratch transcripts modified in the past 14 days (Claude Code 2.1.319 to 2.1.328) with ad-hoc Python; the reproducible version of this survey becomes `wallclock` itself.

- **API calls.** Every request is an `api-request` entry (`id`, `timestamp`, `querySource` such as `repl_main_thread`, `agent:builtin:general-purpose`). Each response content block is an `assistant` entry whose `requestRef` names the request (15,921 of 15,923 linked). API interval = request timestamp to the last block's timestamp. `thinkingDurationMs` and `toolInputStreamMs` give sub-splits where present.
- **API errors** appear as `assistant` entries with `model: "<synthetic>"` and `isApiErrorMessage`, with text such as "API Error: Your computer went to sleep mid-response".
- **Tools.** A `tool_use` block in an assistant entry, then a `user` entry carrying the `tool_result` and `toolUseResult` (Bash: `stdout`, `interrupted`, `backgroundTaskId`, `timedOutAfterMs`). Tool interval = tool_use timestamp to result timestamp; parallel calls are unioned.
- **Approvals.** `user.permissionDecision.source` is `config` (rule, classifier, auto mode) or `user_temporary` / `user_permanent` / `user_reject` (a human decided). There is no separate timestamp for when the human answered, so for a human-approved call the whole interval counts as approval (approval plus execution); for a rejection it is pure approval. `AskUserQuestion` and `ExitPlanMode` also show as `user_*` but are questions, so they count as user wait.
- **Turn boundaries.** Human prompts are `user` entries with `origin.kind == "human"`; wakeups are `origin.kind == "task-notification"` (background Bash or subagent finishing) or `peer`. `system/turn_duration` closes a turn. `system/away_summary` marks a return after absence.
- **Hooks.** `system/stop_hook_summary` has no duration field; hook time shows up as the gap between the last assistant entry and `turn_duration`.
- **Subagents** live at `<project>/<session-id>/subagents/agent-<id>.jsonl`, entries marked `isSidechain: true` with an `agentId`.
- **Interruptions.** A user entry containing "Request interrupted by user"; resumes show as a new process run of the same session (repeated `session-owner` / `bridge-session` headers) with a large gap.
- **Cross-check.** A `cost-state` entry carries `totalAPIDuration`, `totalAPIDurationWithoutRetries` and `totalToolDuration`; validation compares against it.

## Architecture

Library packages under the module, with `cmd/wallclock` as a thin adapter.

- `transcript` reads a JSONL file into typed entries, tolerating unknown types and fields, and discovers a session's subagent files.
- `timeline` turns one thread's entries into classified intervals. This is the core and holds every attribution decision.
- `classify` maps a Bash command line to a command type.
- `repo` resolves a cwd to a repo identity and a language tag.
- `cache` stores each file's intervals keyed by path, size and mtime.
- `report` filters by date range, aggregates per repo, and renders rows, JSON or a pretty table.

Buckets: `api`, `api_error`, `tool:<type>` (test, build, lint, package, vcs, bash, file, web, mcp, other), `approval`, `user`, `away`, `hook`, `harness` (unexplained gaps between known events), and `idle` (waiting on a subagent; reported, counted as zero). Events: API errors by kind, interruptions, resumes.

## Order

Scaffold first (module, primer, README). Then transcript parsing, which everything reads. Timeline, classify and repo can then proceed independently; cache and report follow; validation on real data closes the plan.

Imported into `job` on 2026-11-19 under `vHiCCc`; the tracker is the live plan from then on.

```yaml
tasks:
  - title: wallclock v1
    desc: |
      Build the wallclock CLI described in project/2026-11-19-wallclock-plan.md: a per-repo, date-ranged breakdown of where Claude Code session time goes.
    children:
      - title: Scaffold the Go module, CLI primer and README
        ref: scaffold
        desc: |
          Why: everything else needs a module, a binary and a place to hang verbs.

          What: `git init`, Go module `github.com/bensyverson/wallclock`, `cmd/wallclock` with a bare-invocation primer (what the tool is for, verbs grouped by role, the command to run first) and `--help` per verb, exit codes from project/agents/cli-design.md, a README per the house style, and the AGENTS.md head filled in (overview, doc list). Install the pre-commit hook with `agents hooks install`.
        criteria:
          - "`wallclock` with no arguments prints the primer and exits 0 (binary test)."
          - "An unknown verb exits 2 with a message naming the valid verbs (binary test)."
          - "README and the AGENTS.md head describe the project with no placeholders left."
      - title: Parse transcripts into typed entries
        ref: transcript
        blockedBy: [scaffold]
        desc: |
          Why: every other package reads transcripts; the format is undocumented and changes between Claude Code versions, so parsing must be tolerant and typed.

          What: package `transcript` reads one JSONL file into typed entries covering the fields listed in the plan's spike section (api-request, assistant with requestRef/model/content blocks/thinkingDurationMs, user with tool_result/toolUseResult/permissionDecision/origin, system subtypes, queue-operation, cost-state). Unknown entry types and malformed lines are counted, not fatal. Discovers subagent transcripts for a session. Tests use small hand-written fixtures, never copies of real transcripts (they are private).
        criteria:
          - "Fixture tests cover each entry type the timeline needs, plus an unknown type and a malformed line."
          - "Subagent discovery finds agent files for a session in a fixture directory."
      - title: Build the per-thread timeline classifier
        ref: timeline
        blockedBy: [transcript]
        desc: |
          Why: this is the core: it decides what each stretch of time was spent waiting on.

          What: package `timeline` turns one thread's entries into non-overlapping classified intervals using the buckets and rulings in the plan (API, API error, tool by type, approval, user with the 10-minute cap and away remainder, hook, harness, idle on subagent counted as zero, background command waits). Parallel tool calls are unioned. Out-of-order timestamps are sorted. Also emits events: API errors by kind, interruptions, resumes.

          Seams: uses `classify` for Bash command types through a small interface, so it can be built before classify lands.
        criteria:
          - "Table tests cover each bucket, including a capped user gap, an approval, a rejection, AskUserQuestion, parallel tools, a background command wakeup, a subagent wakeup, an API error and an interruption."
          - "Intervals for a thread never overlap and sum to the thread's span (property test)."
      - title: Classify Bash commands by type for Go, Swift, Rust and Python
        ref: classify
        blockedBy: [scaffold]
        desc: |
          Why: Ben wants waits grouped by type (tests, builds, other bash) to compare stacks.

          What: package `classify` maps a command line to test, build, lint, package, vcs or bash, handling `cd x &&` prefixes, env assignments, pipes and chains (the slowest-looking segment wins: test over build over the rest). Built-in patterns for go, swift/xcodebuild, cargo, python/pytest/uv/pip, plus make and just. A config file can add patterns.
        criteria:
          - "Table tests cover each language's test and build commands, a chained command, an env-prefixed command and an unknown command."
      - title: Resolve cwd to repo identity and language
        ref: repo
        blockedBy: [scaffold]
        desc: |
          Why: reports are per repo, and the encoded project directory names are lossy.

          What: package `repo` resolves a cwd to the main repo root (following worktree `.git` files to the common dir, folding `.claude/worktrees/*`), falls back to path heuristics when the directory no longer exists, flags scratch paths (`/private/tmp`, `/tmp`) for exclusion, and tags language from marker files (go.mod, Package.swift or *.xcodeproj, Cargo.toml, pyproject.toml or setup.py).
        criteria:
          - "Tests with temp-dir fixtures cover a plain repo, a subdirectory, a worktree, a missing directory and a scratch path."
      - title: Cache per-file analysis
        ref: cache
        blockedBy: [timeline]
        desc: |
          Why: the first run reads every transcript; later runs should only read what changed.

          What: package `cache` stores each transcript's intervals and events keyed by path, size and mtime under `~/.cache/wallclock/` (override `WALLCLOCK_CACHE_DIR`), with a schema version so a format change invalidates old entries. Writes are atomic.
        criteria:
          - "Tests show a hit on unchanged files, a miss after a change, and a miss after a schema version bump."
      - title: Aggregate and render the report
        ref: report
        blockedBy: [timeline, classify, repo, cache]
        desc: |
          Why: this is the product: per-repo breakdowns over a date range.

          What: package `report` clips intervals to the date range, aggregates per repo (with language) and overall, and renders plain rows (default), `--json` and `--pretty`. CLI verbs: `report` (per repo), `sessions` (per session, for drilling in), `cache clear`. Flags: `--days` (default 7), `--since`, `--until`, `--repo`, `--away` (default 10m), `--include-scratch`. Output states that totals are agent-time and can exceed wall-clock.
        criteria:
          - "Binary tests against a fixture projects directory assert exact output bytes for rows, JSON and pretty."
          - "Every error path has a binary test that reads the remedy."
      - title: Validate on real transcripts and write the findings
        ref: validate
        blockedBy: [report]
        desc: |
          Why: the attribution rules are only as good as their fit to real sessions.

          What: run wallclock on the past 14 days, compare per-session API and tool totals to the `cost-state` entries where present, inspect the largest `harness` gaps and explain or fix them, and write a dated findings doc in project/ with the numbers and the commands that reproduce them.
        criteria:
          - "A findings doc in project/ reports per-repo results, the cost-state cross-check and the share of time left in harness, each with the command that reproduces it."
```
