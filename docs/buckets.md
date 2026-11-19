# Time buckets

wallclock splits each agent thread's time into non-overlapping intervals and labels each one with a bucket. A thread is the main session or one subagent. The intervals cover the thread from its first logged entry to its last.

## Totals are agent-time

Every thread counts its own time, and the totals add up across threads. If four subagents each wait two minutes on a build at the same time, that is eight minutes of `tool:build`. So a report's total can exceed the wall-clock time it covers. A thread that is only waiting for a subagent to finish is `idle`, which is shown but counted as zero; the subagent's own time is what counts.

## Buckets

| Bucket | What it measures |
|---|---|
| `api` | From sending a model request to receiving the last block of its response: thinking, generation and network. |
| `api_error:<kind>` | Requests that ended in an API error. Kinds: `overloaded`, `rate_limit`, `timeout`, `server`, `network`, `auth`, `other`, and `retry` (a request that got no response for 5 s or more and was re-sent). |
| `tool:<type>` | Waiting for a tool call to return, or for a shell command the user ran with `!`. Bash commands are typed: `test`, `build`, `lint`, `package`, `wait` (sleeping or polling, such as `until …; do sleep`), `vcs`, and `bash` for anything else. Other tools: `file` (Read, Edit, Write, Glob, Grep), `web`, `mcp`, `other`. Parallel calls count once, typed by the slowest. |
| `background:<type>` | A thread with nothing to do until a background command finishes, typed like Bash. |
| `approval` | A tool call a person had to approve or reject. The transcript doesn't record when the person answered, so an approved call's whole interval counts here, including the command's own run time. |
| `user` | Waiting for the person: from the end of a turn until the next one starts, or for an answer to a question Claude asked. Each wait counts at most `--away` (default 10 minutes). |
| `away` | The part of a user or approval wait beyond `--away`. Reported, but not part of the active total, so coming back to a session days later doesn't skew the numbers. |
| `suspended` | A computer or API wait longer than `--suspend` (default 1 hour), or a response cut off because the machine slept. Treated as the laptop being closed; reported, not active. |
| `hook` | Time spent in hooks after Claude finished a turn. |
| `idle` | A thread with nothing to do until a subagent finishes. Counted as zero (see above). |
| `harness` | Gaps between known events that none of the above explains, such as Claude Code's own processing. A large share here means the rules need work; on real data it is about 1%. |

## How turns are read

- A turn ends at `turn_duration`, at an interruption (Esc), or at an API error that had no request (such as an expired login). From then on the thread is waiting on the user.
- The next turn starts at a prompt, a `!` shell command or slash command, or a task notification. A notification from a background command makes the wait `background:<type>`; one from a subagent makes it `idle`.
- When explicit intervals overlap, the API wins over tools (Claude Code starts tools while a response is still streaming), and tools win over waits.

## Events

Alongside time, wallclock counts API errors by kind, interruptions, absences (waits longer than `--away`) and suspensions.

## Custom command rules

`--rules FILE` adds patterns ahead of the built-in ones:

```json
{"rules": [
  {"kind": "test",  "prefix": ["bazel", "test"]},
  {"kind": "build", "regexp": "^bazel build"}
]}
```

A `prefix` matches the command's leading words after env assignments and launchers (`time`, `env`, `uv run`, `xargs`, …) are removed; a `regexp` matches those words joined by single spaces. Kinds are the Bash types listed above.

## Limits

See the known limits in `project/2026-11-19-validation-findings.md`: approval includes run time, suspension is a duration heuristic, and language comes from marker files.
