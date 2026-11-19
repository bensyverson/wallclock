# Backlog

Work decided against, parked rather than dropped in silence. Nothing here is scheduled or blocking — active work lives in `job`. Read it before proposing something that sounds novel; it may already have been weighed and parked.

- **One dated H2 per item:** what it is, why it's parked, and *what would un-park it*.
- **Delete an item when it lands, or when it stops being plausible.** A long list is one nobody reads.
- **Something parked that turns out to be needed becomes a task in `job`** — move it, don't work it from here.

Format: one dated H2 per entry, a headline, then what it is, why it's parked, and the trigger that would revive it.

---

## 2026-11-19 Exact sleep detection from the macOS power log

**What:** read `pmset -g log` sleep/wake events and mark any interval spanning a sleep as `suspended`, instead of the `--suspend` duration heuristic. **Why parked:** the log only reaches back a few days and is macOS-only, and the heuristic plus "went to sleep" API errors caught the one real case in 14 days. **Un-park when:** reports show `suspended` time, or long builds misfiled as suspended, often enough to matter.

## 2026-11-19 Split API time into thinking, generation and time-to-first-token

**What:** use `thinkingDurationMs` and `toolInputStreamMs` to break `api` into sub-buckets. **Why parked:** they're present on only some entries, and v1's question is Anthropic-versus-computer, not where inside a call the time goes. **Un-park when:** someone wants to tell slow thinking apart from slow streaming.

## 2026-11-19 Configurable language override per repo

**What:** let a config file set a repo's language when marker files mislead (mixed Rust/Python, docs repos). **Why parked:** marker detection is right for the current comparisons. **Un-park when:** a comparison by language is skewed by a misdetected repo.

## 2026-11-19 Split a long approved command into approval and run time

**What:** when a person-approved command's interval runs past `--away`, count the rest as `tool:<type>` for kinds that plausibly run long (`build`, `test`, `package`, `wait`), and keep it as `away` for the rest (a quick `git status` approved after lunch). **Why parked:** the transcript doesn't record when the person clicked, and in 30 days of real data no command waited on approval at all: all 6 person-decided permissions were answers to `AskUserQuestion` (grep for a `permissionDecision` source starting `user_`, then `go run ./scripts/gaps -days 30 -bucket approval`). **Un-park when:** someone who approves commands by hand uses wallclock and their `approval` or `away` time is large.
