# Task Design — AI-first, token-wise `dg task` subcommands

`dg task` subcommands are consumed by two audiences: **an LLM agent first** (Claude
Code / OpenCode invoking `devgeta task …` from the shared configs), **a human second**
(via the `dge` shell wrapper). When those audiences conflict, design for the agent —
humans tolerate terse output far better than agents tolerate noisy output.

Origin: the output principles were decided in the
[2026-07-14 token-aware git tasks cycle](../plans/cycles/2026-07-14-token-aware-git-tasks.md)
(§5) and generalized here. This guide is their canonical home going forward.

---

## When to build a task (and when not to)

Build a `dg task` subcommand only when it adds **semantic value** an agent can't get
from raw commands. The three justifications, strongest first:

1. **Collapse round-trips.** Each agent tool call costs ~100–200 tokens of framing
   plus a reasoning turn. If a flow needs N commands that always run together
   (fetch → detect default branch → merge-base → stat), one task that runs them
   in-process wins even if the output were identical. Example: `review-scope`
   replaced a 6-call orientation dance.
2. **Enforce policy and contracts.** Things an agent _should_ always do but might
   skip: dedup against resolved threads, keep the merge-base stable across a review
   session, never silently hide excluded files. Encoding the policy in the task makes
   it non-optional. Example: `submit-review` posts body + inline comments atomically.
3. **Render for the reader.** Reshape verbose structured output into compact text
   (see principles below). Example: `review-threads` turns a GraphQL payload into
   one markdown block per thread.

**Don't build a task when:**

- The saving is negligible — measure first (see below). `git status` → `--porcelain`
  saves ~350 bytes; that's prompt guidance, not a subcommand.
- Only generic compression is needed, with no orchestration or policy. That is a
  general-purpose tool's job, not bespoke Go code — see [rtk](#future-rtk) below.
- The output feeds a human only. Streaming raw output (`Stream: true`) is fine there.

**Keep every task (and any redirect that steers agents to it) generic.** This is the
task-specific case of a repo-wide rule — CLAUDE.md §3 principle 8 (_Everything general,
never bespoke_): a `dg task` subcommand runs in whatever repo the agent is in, so its
behavior must make sense in any of them and must not encode devgeta's own internal
process. When a task genuinely does encode devgeta-specific policy (e.g. `release`
implements the CLAUDE.md §9 flow), any hook/plugin that redirects raw commands to it
must be gated so it never fires outside this repo — see the `go.mod` gate in
`configs/claude/task-redirect.sh`.

## Output principles

1. **Labeled plain text, not markdown scaffolding.** Line-oriented `key: value`
   labels, `- ` lists, aligned stat lines. No headers, tables, bold, or emoji — that
   is rendering decoration an LLM pays for without needing. Markdown syntax only
   where structure earns its tokens (e.g. ` ```diff ` fences), per the
   `reviewThreadsFilter` precedent in `internal/tooling/terminal/dev_tools/jq/jq.go`.
2. **Payload only.** Never wrap output in prose ("Here is the scope:", "Done! ✓").
   The first byte of output is data.
3. **Mutations confirm with one line: verb + target** — e.g. `Resolved thread
PRRT_abc`. Never bare `ok` (the echoed target lets an agent verify it acted on the
   right object and reuse the id without re-fetching) and never more than one line.
   Success/failure for scripts lives in the exit code, not the text.
4. **Stable sentinels.** Fixed phrases agents match verbatim (`No unresolved review
threads.`, `On <default> — no branch to compare…`) are contracts with the shared
   configs in `configs/shared/` — changing one is a breaking change and needs the
   consuming config updated in the same commit. Empty results always get a sentinel;
   empty stdout is ambiguous to an agent (success? nothing found? crash?).
5. **Lossy is allowed only with a receipt.** Filtering noise (lockfiles, generated
   files) is encouraged, but every omission must be announced with a one-line note
   and an escape hatch (`branch-diff` prints `excluded: go.sum (+40/-12)` and offers
   `--file`). A reviewer agent's verification bar requires reading actual code —
   silently compressed payloads produce confidently wrong reviews.
6. **Progress goes to stderr; stdout stays the contract.** A task that runs long
   enough to leave a caller watching silence (a multi-minute headless review, a
   multi-step install) may emit human-readable progress — start/finish, elapsed
   time — but it belongs on stderr, never stdout: stdout stays exactly the
   parseable payload principles 1-4 describe. `review-run` is the precedent: it
   streams a `[n/total] <label>: running` / `<label>: <outcome> (<elapsed>)` line
   per reviewer to stderr while its stdout return stays the `<label> → <outcome>`
   contract, unaffected.

   **Progress is only progress if it arrives during the work.** Lines emitted
   around a long call (before it, then after it) leave the same silence in the
   middle that they were added to fix. When the tool itself reports what it is
   doing, render that as it arrives: `CommandParams.OnStdoutLine` hands a caller
   each stdout line while the child still runs, which is how `review-run` reports
   what a reviewer is doing instead of nothing for four minutes. Keep each line
   short and summarized — a tool name and what it was pointed at — never the
   tool's raw output.

   **Sample the stream; don't relay it.** A tool that reports its own activity
   reports far more of it than a reader needs, and every line is tokens for an
   agent caller. Print at most one line per interval — carrying the running
   counters and the most recent event, so the sample says where the work is and
   not merely that it lives — and keep counting everything in between so the
   closing line still totals the whole run. `review-run` samples at 30s, which
   turned ~200 lines a round into a handful. Put the full stream behind the root
   `--verbose` flag rather than a per-command flag of its own: "how much progress
   detail" is one decision a user makes for the whole CLI, not per task.

7. **A payload states an outcome; it does not carry the record.** When a task's
   result already lives somewhere a caller can read (the review journal, a config
   file, a PR), the payload names the outcome and stops. `review-run` prints one
   verdict line per reviewer and nothing else: the findings are journal entries,
   and `review-notes` is the command that reads them. Reprinting the record in
   every round costs tokens for output the caller must re-read anyway, and gives
   the same fact two spellings that can disagree.

## Architecture: orchestrate, then format

Mirror `PRManager` (`internal/tooling/task/pr.go`): the manager method
**orchestrates** raw fetches, then hands raw output to a **pure formatter** that
renders the final text.

- Input is JSON (a `gh` payload) → a `jq` filter is the formatter.
- Input is line-oriented text (git plumbing) → a pure Go function is the formatter;
  a jq subprocess would add a hop just to split strings.

The split is what makes testing cheap: formatters get golden-fixture unit tests with
zero mocking; orchestration tests only assert which commands ran and the error paths
(`testutil.MockApp` + `VerifyNoRealCommands`, per
[testing-patterns.md](testing-patterns.md)).

## Aggregate tasks compose, never re-derive (ADR-0034)

A task that answers a question several existing tasks already partly answer
(e.g. `pr-state`, composing what `pr-view`/`pr-checks`/`review-threads`/
`pr-review-state` each already fetch) must not re-query the same facts from
scratch. Re-querying gives one fact two independent spellings that can
silently disagree the next time either path changes — principle 7's "does
not carry the record" concern, one level up: an aggregate is not a place to
re-derive a fact that already has an owner.

The mechanism: split each existing command's raw-fetch-plus-reduce core into
a **structured gatherer** — a function returning a small struct, not
rendered text — and have both the original command and the new aggregate
render from the same struct. The gatherer is the seam; the aggregate never
parses a sibling command's markdown or one-line output to recover data it
could have gotten as a struct. `internal/tooling/task/prstate.go` composing
`gatherPRChecks`/`gatherPRReviewState` (extracted from `pr.go` and
`prreviewstate.go`) is the precedent.

Two rules follow directly:

- **No incidental heavy work.** A gatherer built for the aggregate must stop
  at what the aggregate needs, even when the sibling command that also uses
  it does more. `pr-state`'s checks gatherer stops at a pass/fail/pending
  tally; it never calls the per-failure log-digest fetch `pr-checks` makes
  for its own richer answer.
- **Resolve identifying context once.** When every gatherer needs the same
  resolved identifiers (owner/repo/PR, in `pr-state`'s case), resolve them
  once in the aggregate and pass the resolved values into each gatherer —
  never let each gatherer re-run its own resolution, which multiplies calls
  that were already paid for once.

Partial failure is a per-field concern, not a whole-command one: one
gatherer erroring reports that field as unavailable and the rest of the
payload still renders, per principle 4's stable-sentinel spirit extended to
failure.

## Measure before and after

Token estimates: `bytes / 4` is close enough for prose; code runs ~3.5 chars/token.
Before building, measure the raw output the agent ingests today (`… | wc -c`); after,
measure the task's output on the same input. If the delta plus saved round-trips
isn't clearly worth a Go file and its tests, don't build it. Reference baselines from
the 2026-07-14 cycle: a 25-file branch diff was 192 KB (~50k tokens) raw vs 3.3 KB
(~950 tokens) as a stat table; npm-repo lockfile diffs alone run 10–50k tokens.

---

## rtk

[rtk](https://github.com/rtk-ai/rtk) ("Rust Token Killer", Apache-2.0) is a CLI proxy
that compresses the output of 100+ common commands (`git`, test runners, `docker`,
`cat`, `grep`, …) before it reaches the LLM — claiming 60–90% reduction — and hooks
into Claude Code by auto-rewriting Bash calls (`rtk init -g`). Verified 2026-07-14:
~71k stars, single Rust binary, official Homebrew formula, very active; young
(created 2026-01) with a large open-issue count.

**Relationship to `dg task`: complementary, not competing.** rtk is _generic lossy
compression_ of whatever command runs; `dg task` is _semantic orchestration + policy_
(one-call flows, dedup contracts, stable sentinels, atomic reviews) that no generic
proxy can provide. rtk would cover the long tail this guide says **not** to build
tasks for: test-runner output, `docker ps`, `cat`/`grep` noise.

Adoption stance (decided in
[ADR-0004](../decisions/ADR-0004-ai-tools-install-category.md), shipped
2026-07-23):

- Shipped as the first app of the `ai-tools` category — `dg install --only rtk`;
  brew on macOS, GitHub-release binary on Debian. See
  [docs/apps/rtk.md](../apps/rtk.md).
- **The binary is installed; the hook stays opt-in.** `rtk init -g` rewrites
  _every_ agent Bash call, including inside our carefully designed flows, and
  lossy compression must never silently apply to a reviewer's diff payload
  (principle 5). Users run `rtk init -g` themselves if they want it.
- Revisit automating the hook once the project stabilizes (created 2026-01 and
  moving fast).
