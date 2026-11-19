# Time buckets

wallclock splits each agent thread's time into non-overlapping intervals and labels each one with a bucket. A thread is the main session or one subagent.

## Totals are agent-time

Every thread counts its own time, and the totals add up across threads. If four subagents each wait two minutes on a build at the same time, that is eight minutes of `tool:build`. So a report's total can exceed the wall-clock time it covers. A thread that is only waiting for a subagent to finish is `idle`, which is shown but counted as zero; the subagent's own time is what counts.

## Buckets

| Bucket | What it measures |
|---|---|
| `api` | From sending a model request to receiving the last block of its response: thinking, generation and network. |
| `api_error` | Requests that ended in an API error (overload, rate limit, a response cut off by sleep, login expiry). |
| `tool:<type>` | Waiting for a tool call to return. Bash commands are typed: `test`, `build`, `lint`, `package`, `vcs`, `bash` (anything else). Other tools: `file` (Read, Edit, Write, Glob, Grep), `web`, `mcp`, `other`. Parallel calls count once for the span they cover. |
| `approval` | A tool call a person had to approve or reject. The transcript doesn't record when the person answered, so an approved call's whole interval counts here, including the command's own run time. |
| `user` | Waiting for the person: from the end of a turn to their next prompt, or for an answer to a question Claude asked. Each gap counts at most `--away` (default 10 minutes). |
| `away` | The part of a user gap beyond `--away`. Reported, but not part of the active total, so coming back to a session days later doesn't skew the numbers. |
| `hook` | Time spent in hooks after Claude finished a turn. |
| `background` | A thread with nothing to do until a background command finishes. Typed like Bash, e.g. `background:test`. |
| `idle` | A thread with nothing to do until a subagent finishes. Counted as zero (see above). |
| `harness` | Gaps between known events that none of the above explains, such as Claude Code's own processing. A large share here means the rules need work. |

## Events

Alongside time, wallclock counts API errors by kind, user interruptions, and session resumes.
