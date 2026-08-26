# Cycle: reviewer agents robustness — fetch-first, deeper lens, binary invariant

**Date:** 2026-07-16
**Estimated Duration:** ~3 hours (prompt edits ~2h; optional merge-base code check ~1h,
splittable into a follow-up).
**Status:** Done — every §4 deliverable shipped, and every §6 verification now passes.
Deploy has since happened
— both agents' live `code-reviewer.md`/`document-reviewer.md` match this cycle's shipped
content byte-for-byte (checked directly against `~/.claude/agents/` on 2026-08-26: the
fetch-first discipline, `document-reviewer`'s read-only task allowlist, and the `devgeta`-binary
invariant line are all present). **All of §6's manual verification was run on 2026-08-26 via
`devgeta task review-run` and passes** — see the two "Live run" sections at the end. Item 2
needed two purpose-built branches (a defective one and a nit-only one), since neither half of
it can be judged on a clean branch.

Those runs also exposed a real bug and fixed it: all three reviewer agents were reaching
outside the repository they were sandboxed to — for a global `CLAUDE.md`, and for the review
journal, which in a worktree sits under the _main_ repo's `.git` — and a refused read was
ending whole rounds in `NO VERDICT`, retry included. `gpt-5.6-terra` failed that way 4 times
out of 4. Both halves are fixed in the prompts and re-verified; the durable follow-up (the
runner should not retry a permission-denial `NO VERDICT`) is named at the end.

---

## 1. Domain Context

Automated review runs through three shared configs, deployed to every machine via
`configs/shared/` → rebuild → `dg configure`:

- `configs/shared/agents/code-reviewer.md` — reviews code changes.
- `configs/shared/agents/document-reviewer.md` — reviews plans/technical docs (not code).
- `configs/shared/commands/review-pr.md` — takes findings (from either agent or another
  model) and posts one cohesive PR review via `devgeta task submit-review`.

The goal of this cycle is to raise **review signal**: catch things that don't make sense,
push improvements grounded in the language's own idioms, and stop reviews from drowning in
nitpicks — without bloating the prompts or adding machinery we don't need.

Related: this sits next to the `devgeta task` review tooling (`review-scope`, `branch-diff`)
and the PR command family in `cmd/task_pr.go`.

---

## 2. Research findings (the "deep research")

### 2.1 "Pull latest before reviewing" is already handled — and a reviewer must NOT pull

The user asked whether we should pull latest before a review, and whether that needs a new
multi-step task. Both questions resolve against the existing code:

- **`review-scope` already fetches origin**, bounded to 10s best-effort
  (`internal/tooling/task/scope.go:12,49` — `FetchOriginTimeout(reviewScopeFetchTimeout)`).
- **Both diff commands base the merge-base on `origin/<default>`, not a possibly-stale local
  branch** (`internal/tooling/task/branchdiff.go:41` and `scope.go:108`:
  `merge-base origin/<default> HEAD`). So once `review-scope` has fetched, the diff is
  computed against current remote state.
- Therefore the correct primitive for a reviewer is a **read-only fetch**, which the
  existing `review-scope` already performs. **No new task is needed.**

Critically, a reviewer must **not** run `refresh-branch` or otherwise pull/merge:
`refresh-branch` checks out the target, pulls, and **merges target into the working branch**
(`cmd/task.go:67-72`) — that _mutates the branch under review_ and changes what's being
reviewed. Pulling/merging during review is the wrong operation. The rule to encode is:
**run `review-scope` first (it fetches); never pull or merge.**

Open item to verify during implementation (the only code-side question): confirm nothing in
the reviewer flow reads a stale _local_ default branch. The task commands use
`origin/<default>` already; just make sure the agents don't add their own `git diff main`
style calls that would bypass it.

### 2.2 `document-reviewer` can't verify claims against current code

`document-reviewer.md` has no `devgeta task *` entry in its bash allowlist (compare
`code-reviewer.md:16`), so it can't run `review-scope`/`branch-diff` and never fetches. Its
own process says "verify plan claims against the repo" (`document-reviewer.md:46`), but it
can only `grep`/`read` a possibly-stale local tree. Gap: give it the same read-only task
access so it can check a plan against fetched code.

### 2.3 Reviews skew toward "minimal stuff"

Both agents already de-prioritize style (code-reviewer pass 8; review-pr pass 6) and tag
severity. But nothing tells them to _lead_ with substance or to hold back a review that is
all nits. The user's explicit ask — "not minimal stuff, but things that don't make sense" —
means we should make the nit-discipline explicit, not just implied by ordering.

### 2.4 Language-idiom depth is implicit, not required

code-reviewer loads repo standards (`CLAUDE.md`) and mentions "language-specific guidelines"
only in passing. The user wants improvements "based on best patterns of the language." The
fix is to require the reviewer to evaluate changed files against their language's concrete
idioms and **name the idiom**, not say "follow best practices." For this repo's primary
language (Go): error wrapping (`%w`), `context` propagation, zero-value readiness, defining
interfaces at the consumer, table-driven tests, avoiding premature abstraction — grounded in
Effective Go and `CLAUDE.md` §6.

### 2.5 The user's review-area checklist maps almost entirely onto existing passes

Correctness, concurrency/async, performance, maintainability, security, testing, refactoring
are already covered by code-reviewer's passes — with two thin spots: concurrency detail
(atomicity, memory visibility, blocking an event loop) and testing depth (edge/property-based
tests, caching/memoization opportunities). Fold these as sub-bullets into the **existing**
passes; do not add new top-level sections (prompt bloat lowers adherence).

### 2.6 `devgeta` binary invariant — currently compliant, keep it that way

All shared configs already invoke the **`devgeta` binary** (`devgeta task ...`); a repo-wide
search found no `dg` alias, `go run`, `go build`, `./devgeta`, `make build`, or local-build
invocation in `configs/shared/`. Agents run where only the installed binary is on PATH, so
this must stay true. Encode it as a one-line invariant in the agents/commands plus a review
checklist item, so a future edit can't silently reintroduce an alias or a local build.

---

## 3. Objective

Reviewers that (a) always review against current remote state via a **read-only fetch**
(`review-scope`) and never mutate the branch; (b) **lead with design/correctness and named
language idioms**, holding back nit-only reviews; (c) can **verify claims against fetched
code** — both agents, not just code-reviewer; and (d) always invoke the **`devgeta`
binary**.

---

## 4. Scope Boundary

### In Scope

- [x] Fetch-first / never-pull discipline made explicit in `code-reviewer.md`,
      `document-reviewer.md`, and `review-pr.md`.
- [x] `document-reviewer.md` gains read-only `devgeta task` access
      (`review-scope`, `branch-diff`, `pr-view`, `current-pr`, `current-repo`) and a step to
      verify plan claims against fetched code.
- [x] Deeper lens folded into existing passes: named language idioms; concurrency detail
      (atomicity, memory visibility, blocking); testing depth (edge/property-based, caching).
      Note: caching/memoization landed in the **Performance** pass, not Tests — review found
      it is a performance concern, and the ordered-passes design put it there.
- [x] Explicit nit-discipline: lead with substance; if every finding is `[Nit]`/`[MINOR]`,
      say so and approve.
- [x] `devgeta`-binary invariant line in the reviewer agents and commands. Note: the plan
      also called for a "checklist item"; review found an editor-facing "keep this true in
      future edits" parenthetical was noise in a runtime prompt, so it was dropped. The
      invariant line documents the rule; durable enforcement is the manual grep in §6 (a CI
      grep guard would make it structural — a follow-up, out of this prompt-only cycle).
      Also: `code-reviewer.md` keeps its existing `devgeta task *` allow, while
      `document-reviewer.md` enumerates only the five read-only commands (tighter scope for
      an edit-denied doc reviewer) — an intentional divergence, not an inconsistency.

### Explicitly Out of Scope

- New `devgeta task` commands, or any pull/merge step in the review flow (rejected above).
- Rewriting the agent output formats or the `/review-pr` posting flow.
- Web-based research or new external references beyond those already cited.
- Changing `branch-diff`/`review-scope` behavior, except a read-only confirmation that the
  agents don't bypass `origin/<default>` (see 2.1); any actual code change there splits into
  its own follow-up with task tests.

**Scope is locked.** New needs → document for a future cycle.

---

## 5. Implementation Plan

### Step 1 — Fetch-first, never-pull discipline

- `code-reviewer.md` Scope section: state that `review-scope` fetches and must run before
  `branch-diff`; add a one-liner that the reviewer never pulls or merges (that would change
  the branch under review).
- `review-pr.md` step 2: same clarification.
- `document-reviewer.md`: add the same fetch-first note (enabled by Step 2's tooling).

### Step 2 — Give `document-reviewer` read-only repo-verification tooling

- Add to its bash allowlist the read-only task commands only:
  `devgeta task review-scope`, `devgeta task branch-diff*`, `devgeta task pr-view*`,
  `devgeta task current-pr`, `devgeta task current-repo`. Do **not** grant write/PR-mutating
  task commands.
- Add a process step: when a plan claims something about existing code, run `review-scope`
  (fetches) then `branch-diff`/grep to verify against current code, not a stale checkout.

### Step 3 — Deepen the lens (fold into existing passes, no new sections)

- code-reviewer Functionality pass: add concurrency sub-bullets (atomicity, memory
  visibility, blocking a hot/event path).
- code-reviewer Tests pass: add edge/property-based coverage and caching/memoization
  opportunities.
- New requirement across passes: evaluate changed files against the **named** idioms of
  their language (Go examples grounded in Effective Go and `CLAUDE.md` §6); reject "follow
  best practices" with no specific idiom.
- Nit-discipline line: lead with design/correctness; a review whose only findings are `Nit:`
  should say so and approve.

### Step 4 — `devgeta`-binary invariant

- One invariant line in `code-reviewer.md`, `document-reviewer.md`, and the commands:
  "Invoke the `devgeta` binary only — never a `dg` alias, `go run`, or a local build."
- Add a checklist item so future prompt edits keep this true.

### Step 5 (optional, splittable) — confirm no stale-local-default bypass

- Read-only check that the agents rely on `devgeta task` (which uses `origin/<default>`) and
  don't introduce their own `git diff <local-default>` calls. If a code change to the task
  commands turns out to be needed, split into a follow-up with `internal/tooling/task` tests.

### Deploy

Per the shared-configs workflow: edit under `configs/shared/`, rebuild, then `dg configure`
to deploy. PR text written plainly.

---

## 6. Verification Plan

### Manual

1. [x] Run `code-reviewer` on a sample feature branch → it runs `review-scope` (fetches),
       then `branch-diff`; it never pulls/merges; the branch is unchanged after review.
       **Verified 2026-08-26** via `devgeta task review-run` (the production path) on branch
       `finish-cycle-leftovers`: the common git dir's `FETCH_HEAD` was rewritten mid-review,
       so the read-only fetch happened, and `HEAD`, the working tree, and the merge-base with
       `origin/main` were byte-identical before and after — nothing pulled or merged.
2. [x] Findings lead with design/correctness; at least one names a concrete language idiom;
       a nit-only run says so and approves rather than blocking. **Verified 2026-08-26** with
       two purpose-built throwaway branches, since neither half can be judged on a clean one —
       see "Live run" below for what each returned.
3. [x] Run `document-reviewer` on a plan that references code → it fetches via `review-scope`
       and verifies the claim against current code. **Verified 2026-08-26**, and it surfaced a
       real limitation — see "Live run" below.
4. [x] Grep `configs/shared/` → still zero `dg ` alias / `go run` / `./devgeta` / local-build
       invocations; only `devgeta ...`. (Done: only the invariant rule-text and an unrelated
       pre-existing `cargo build` matched — no real forbidden invocation.)

### Automated

- No new automated tests for prompt files. If Step 5 changes task code, add
  `internal/tooling/task` tests with `testutil.MockApp` + `VerifyNoRealCommands`.

---

## 7. Risks & Trade-offs

| Risk                                                         | Likelihood | Mitigation                                                                       |
| ------------------------------------------------------------ | ---------- | -------------------------------------------------------------------------------- |
| Reviewer pulls/merges and mutates the branch under review    | Med        | Explicit never-pull rule; only `review-scope`'s read-only fetch is allowed       |
| `document-reviewer` gains too-broad task access              | Low        | Allowlist only read-only review/pr-view/current-* commands; no write/PR mutation |
| Prompt bloat lowers instruction adherence                    | Med        | Fold new guidance into existing passes; add lines, not sections                  |
| Fetch fails in restricted CI and review proceeds on old base | Low        | Fetch is already best-effort/bounded; `review-scope` reports `FetchFailed`       |

### Trade-offs

- **No new task; rely on `review-scope`'s existing fetch** — the merge-base already uses
  `origin/<default>`, so a read-only fetch is sufficient and a pull would be actively wrong.
- **Fold depth into existing passes** rather than adding sections — keeps the prompts short
  enough that the model actually follows them.

---

## 8. Notes for Implementers

- The single most important behavioral rule: a reviewer **fetches (read-only) and never
  pulls or merges**. Everything else is refinement.
- Deploy through `configs/shared/` → rebuild → `dg configure` (never edit deployed copies
  directly).
- If any step wants a new task command or a branch-mutating step, stop and open a follow-up
  rather than widening scope.

---

## Live run (2026-08-26) — and the limitation it surfaced

`devgeta task review-run --reviewer document` on branch `finish-cycle-leftovers`,
two configured models. This is the production path, not a hand-spawned subagent,
so it exercises the deployed `document-reviewer.md` exactly as a real round does.

| Model                             | Verdict                                                           |
| --------------------------------- | ----------------------------------------------------------------- |
| `github-copilot/gemini-3.6-flash` | `APPROVE`, no findings                                            |
| `github-copilot/gpt-5.6-terra`    | `NO VERDICT` — twice, including ADR-0020's retry, at $0.51 burned |

**What passed.** The fetch-first, never-pull discipline holds under a real run
(see §6 item 1 for the evidence). Both models read repo files to check the
documents' claims rather than taking them at face value, which is the capability
§2.2 added and this cycle never got to observe.

**The limitation.** `gpt-5.6-terra` aborted both attempts on
`permission requested: external_directory (/Users/jair.mendez/.claude/*);
auto-rejecting`. It was trying to verify a claim several cycle docs now make —
that a shipped config under `configs/` matches its **deployed** copy under
`~/.claude/` — and a reviewer is sandboxed to the repo, so it cannot read that
path. Two things follow, and only the second is really about this cycle:

1. **A doc that cites deployed state cites something its reviewer cannot check.**
   That is a property of where the evidence lives, not a defect in the agent
   prompt. Whether such claims should be written differently (e.g. recorded as a
   dated observation rather than a verifiable assertion) is a docs-convention
   question, not a prompt fix.
2. **The failure is not graceful.** A blocked read should degrade to "could not
   verify this claim" inside a normal verdict. Instead the whole run produces no
   verdict, the ADR-0020 retry spends a second full run reaching the same wall,
   and the round ends with nothing usable from that model. A reviewer that cannot
   read one path should still be able to review the other forty files.

Neither is fixed here — this cycle's scope is prompt edits, and (2) is a
behavior change to the review runner's handling of a blocked tool call. Recorded
for a follow-up cycle.

### Item 2, and the reviewer-boundary bug the runs exposed (2026-08-26)

Item 2 cannot be judged on a clean branch — a review with no findings proves
nothing about how findings are ordered or worded. So two throwaway branches were
built and reviewed with `devgeta task review-run --reviewer code`, then discarded.

**A branch with real defects** (a new `probe` package: unclosed file, ignored
`io.ReadAll` error, `%v` instead of `%w`, and an unchecked `parts[1]` index).
The review opened five findings, in this order:

1. runtime panic on a line lacking the `=` delimiter
2. missing `defer f.Close()`, file-descriptor leak
3. ignored `io.ReadAll` error **and unwrapped error formatting**
4. `strings.Split` truncates values containing more than one `=`
5. missing unit tests for the new exported functions

Correctness leads, and finding 3 names Go's error-wrapping idiom explicitly —
the §2.4 requirement. Finding 4 was **not planted**: `strings.Cut` is the right
call there and the reviewer found that on its own. Deliberately planted cosmetic
noise (a pointless `tmp` variable, a `for i := 0; i <= len(x)-1` loop) was not
raised at all, which is the nit-discipline of §2.3 doing its job.

**A branch with only cosmetic issues** (correct logic, full passing tests, but a
redundant comment, a `theSettings` name, an unnecessary `else`). Both models
returned `APPROVE` with no findings opened. Note the first attempt at this branch
was mis-built — it had no tests, so `REQUEST CHANGES` over "missing unit tests"
was the _correct_ verdict, not a failure of nit-discipline; the branch only
became genuinely nit-only once `probe_test.go` was added. One nuance: the journal
records only opened findings, so "approves rather than blocking" is directly
evidenced while "says so" would have to be read out of the report body.

**The bug these runs exposed.** Reviewers are sandboxed to the repository under
review, and were reaching outside it on almost every round:

- `code-reviewer.md`'s step 1 says to read `CLAUDE.md`, and models globbed
  `~/.claude/*` hunting for a global one.
- The review journal is worse, because that reach is _legitimate_: it lives under
  the git common dir, so in a **worktree** it sits under the main repo's `.git`,
  outside the sandbox. A model that globbed `.git/devgeta/review/*` instead of
  calling `devgeta task review-notes` hit the same wall.

Each refusal auto-rejects headlessly, and a model that treats it as fatal returns
`NO VERDICT` — then ADR-0020's retry spends a second full run reaching the same
wall. `gpt-5.6-terra` failed this way **four times out of four**, across three
unrelated branches; `gemini-3.6-flash` failed once, on the journal path. Half to
all of the configured review capacity, dead on every round, at ~$0.12 a time.

Fixed in all three reviewer agents (`code-`, `document-`, `skill-reviewer`), both
halves being general rules any repo wants: search for instruction files **inside
the repo under review only**, reach the journal **only** through
`devgeta task review-note(s)` and never by reading `.git/`, and treat a refused
tool call as **non-fatal** — note what could not be checked and review the rest,
because ending a round with no verdict discards every other real finding over one
unreadable path. Re-run immediately afterwards: **both models `APPROVE`**, with
`gpt-5.6-terra` completing a full 25-tool review and reading `README.md` from
inside the repo rather than globbing `~/.claude/*`.

A prompt is a nudge, not an enforcement, so this narrows the trigger rather than
removing it. The durable follow-up is in the runner: a `NO VERDICT` caused by a
permission denial is deterministic, so retrying it is pure waste and it should be
distinguished from a genuine no-report. That is Go code and belongs to its own
cycle.
