# Cycle: reviewer agents robustness — fetch-first, deeper lens, binary invariant

**Date:** 2026-07-16
**Estimated Duration:** ~3 hours (prompt edits ~2h; optional merge-base code check ~1h,
splittable into a follow-up).
**Status:** Edits complete and reviewed (spec + quality, per-file). Pending: deploy
(`dg configure`) and live manual verification 1–3 in §6.

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

1. [ ] Run `code-reviewer` on a sample feature branch → it runs `review-scope` (fetches),
       then `branch-diff`; it never pulls/merges; the branch is unchanged after review.
       (Pending: requires the deployed agent — run after `dg configure`.)
2. [ ] Findings lead with design/correctness; at least one names a concrete language idiom;
       a nit-only run says so and approves rather than blocking. (Pending: post-deploy.)
3. [ ] Run `document-reviewer` on a plan that references code → it fetches via `review-scope`
       and verifies the claim against current code. (Pending: post-deploy.)
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
