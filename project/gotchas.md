# Gotchas

Project-specific traps that cost real time and that no general rule predicts. Read at session start; append when you hit one.

- **Fix at the source when you can.** A gotcha is a bug report on our tooling, not a permanent fact — if it can be fixed in code, file it in `job` and fix it instead of recording it here.
- **Delete anything that becomes obvious, gets fixed, or stops recurring.** Keep this list short; a long list is one nobody reads.
- **Feedback about `AGENTS.md` itself** — a rule that was wrong, misread, or cost time — goes here too, prefixed `rule:`. It is harvested when the shared rules are reviewed.

Format: one dated H2 headline, then one paragraph. If an entry needs more than that, it's a finding — write it as a dated doc under `project/` and link it from the paragraph.

---

## 2026-11-19 Go and git fail inside the sandbox

The sandbox denies reads under `~` except listed paths, so `go test` fails with the baffling "package bytes is not in std" (the real cause is the default `GOCACHE` under `~/Library/Caches`), and `git` fails reading `~/.gitconfig`. Run Go with `GOCACHE=$TMPDIR/gocache GOTELEMETRY=off`; run git commits outside the sandbox.

## 2026-11-19 `$TMPDIR` differs inside and outside the sandbox

A sandboxed command's `$TMPDIR` is a per-session directory; an unsandboxed one (needed for `git commit`, see above) gets the shared system temp, where other sessions leave files with the same generic names. `git commit -F $TMPDIR/msg2.txt` committed another project's message. Write message files to the session scratchpad by absolute path, and read the file back in the same command that commits.
