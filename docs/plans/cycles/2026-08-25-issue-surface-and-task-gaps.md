# Cycle: Issue Surface and Task Gaps — orienting on an issue, keyed scratch, and six smaller closures

**Date:** 2026-08-25
**Estimated Duration:** ~16 hours (8 slices, independently shippable)
**Status:** Draft

---

## 1. Domain Context

`dg task` subcommands are AI-first composite commands: each collapses a multi-step
git/gh pipeline into one call with compact, token-cheap output, per
[docs/guides/task-design.md](../../guides/task-design.md). The family has grown
along one axis only — the **pull request**. `internal/tooling/task/pr.go` plus
`cmd/task_pr.go` cover view, checks, review target, review state, threads,
replies, resolve, submit, approve, request-changes, merge, and description
updates. The `GithubCli` wrapper
(`internal/tooling/terminal/dev_tools/githubcli/githubcli.go`) has ~30 methods and
**not one of them touches an issue**.

That leaves the front half of a normal development flow unserved. Before an agent
writes a line of code for a tracked piece of work, it needs to know what already
exists for it — an open PR, a local branch, a worktree someone abandoned — and
today it answers that by listing PRs and grepping bodies, which is both expensive
and wrong at the boundaries (`#12` matches inside `#1234`).

This cycle came from an external report by a user running devgeta's agent configs
in a non-Go repository, listing ten friction points. Each was verified against
this codebase before being scoped here; two turned out to rest on incorrect
premises about current behavior and are reshaped or dropped accordingly (§4,
"Explicitly Out of Scope", and Slice E). The report's own token figures are the
reporter's anecdote from their repository and are **not** carried into this doc as
evidence — every slice re-measures locally per task-design.md, "Measure before and
after".

Verified findings that motivate the slices:

- **No issue surface at all.** Confirmed by inspection of `cmd/task.go`,
  `cmd/task_pr.go`, and the `GithubCli` method list. Six of the ten reported
  frictions reduce to this one gap.
- **Scratch directories cannot be re-derived.** `TaskManager.Scratch()` allocates
  with `os.MkdirTemp(root, paths.ScratchAllocPrefix+"*")`
  (`internal/tooling/task/scratch.go:25`), so the path carries a random suffix
  that only the allocating process knows. `validScratchChild`
  (`scratch.go:146-154`) then refuses any path lacking that prefix, so a caller
  who works around the first problem by hardcoding a stable path cannot use
  `--clean` on it. The function's own doc comment (lines 47-50) already names this
  as a deliberate stopping point. It blocks any workflow where one agent session
  produces a working file and a _later, separate_ session consumes it.
- **`refresh-branch` merges, with no alternative.** `cmd/task.go:96-110` —
  "return to the previous branch, and merge target into it". Repositories whose
  merge gate rejects merge commits get a branch that cannot land, and nothing in
  the command warns about it.
- **"Where does this PR stand?" costs four calls.** `pr-view`, `pr-checks`,
  `review-threads --state unresolved`, and `pr-review-state` are four separate
  subcommands that a review round opens with together, every round.
- **`review-scope` reports counts but not sizes.** It prints ahead/behind, the
  commit list, and a per-file stat table (`cmd/task.go:177-199`) — enough to know
  _how many_ commits, not _how large_ they are, which is the input to choosing
  between a per-commit and a whole-range review.
- **`review-threads` renders markdown only** (`cmd/task_pr.go:94-118`). Thread and
  comment ids are embedded in a fixed shape, so they are extractable, but a
  reply/resolve loop is parsing prose to get them.
- **Nothing validates a commit trailer block.** No use of
  `git interpret-trailers` anywhere in the tree. A trailer separated from the body
  by the wrong whitespace is silently not a trailer, and a substring grep cannot
  tell the difference.

Related docs: [task-design.md](../../guides/task-design.md),
[ADR-0015](../../decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md)
(scratch ownership model — Slice A modifies its contract),
[2026-07-22-agent-task-expansion.md](2026-07-22-agent-task-expansion.md) (the
worktree/release/review-package expansion this one continues),
[2026-07-14-token-aware-git-tasks.md](2026-07-14-token-aware-git-tasks.md) (output
principles), CLAUDE.md §3 principle 8 (everything general, never bespoke).

---

## 2. Engineer Context

- **Relevant files:**
  - `internal/tooling/task/pr.go` — `PRManager`; the orchestrate-then-format
    pattern every new task mirrors. `resolveOwnerRepoPR` is the resolution helper
    the issue equivalent parallels.
  - `internal/tooling/task/task.go` — `TaskManager` (git-flavored tasks); home for
    `RefreshBranch`, and for the trailer check.
  - `internal/tooling/task/scratch.go` — `Scratch`, `ScratchClean`,
    `scratchChildName`, `validScratchChild`. Slice A changes the allocation and
    must leave the cleanup bounds checks intact.
  - `internal/tooling/task/scope.go` — reusable helpers: `mergeBase`,
    `aheadBehind`, `commitSubjects`, `fileChanges(rangeSpec)`,
    `partitionExcluded`, `formatReviewScope`. Slice F extends the formatter here.
  - `internal/tooling/task/prreviewstate.go`, `pr_checks_digest.go` — the two
    already-digested state reads Slice D composes rather than re-derives.
  - `internal/tooling/terminal/dev_tools/githubcli/githubcli.go` — the `gh`
    wrapper. Every new GitHub call goes through it (CLAUDE.md §6, "Route external
    tools through their app wrappers"); `GraphQL` and `RunWithOutput` are the two
    entry points.
  - `internal/apps/git/git.go` — `CreateWorktreeIn` (line 704),
    `CreateWorktreeAtBaseIn` (line 711), `createWorktreeIn` (line 779). Slice E
    reuses the existing base-aware call rather than adding one.
  - `internal/tooling/worktree/worktree.go` — `Create`/`CreateAt`/`create`
    (lines 306-454). Slice E threads a base through this path.
  - `cmd/task.go`, `cmd/task_pr.go`, `cmd/worktree.go` — cobra registration.
  - `configs/shared/commands/*.md` — devgeta-owned shipped commands that may be
    updated to call new tasks. **`configs/shared/skills/` is mostly vendored from
    upstream and must not be edited** (CLAUDE.md §12).
- **Architecture rule (task-design.md):** the manager method orchestrates raw
  fetches; a pure formatter renders the text. Git plumbing → pure Go formatter;
  `gh` JSON → a jq filter. The split is what makes formatter tests golden-fixture
  cheap and orchestration tests mock-only.
- **Output rules (task-design.md):** labeled plain text, no markdown scaffolding;
  payload only; mutations confirm with one line (verb + target); stable sentinels
  for empty results; lossy only with a receipt and an escape hatch.
- **Generality rule (CLAUDE.md §3 principle 8, §4):** every task here runs in
  whatever repository the agent is in. Two specific traps this cycle walks into
  and must not fall for, called out again in the slices that own them:
  - Slice C must not assume a branch-naming convention. Matching "the branch whose
    name _is_ the issue number" encodes one team's habit as product behavior.
  - Slice H must not hardcode _which_ trailers a commit needs. Well-formedness is
    universal; the required set is per-repository.
- **Testing:** [testing-patterns.md](../../guides/testing-patterns.md) —
  `testutil.MockApp`, `VerifyNoRealCommands`, golden-fixture tests for formatters,
  no real commands ever. Slice A touches paths and must isolate every root it
  mutates and restore each `paths.Paths.*` override via `t.Cleanup`.
- **Measure first (task-design.md):** before building each slice, capture the raw
  bytes an agent ingests today for a representative case in **this** repository;
  after, capture the task's output on the same input. Record both in the slice's
  checklist entry. If the delta plus the saved round-trips is not clearly worth a
  Go file and its tests, stop and re-scope that slice — the report's figures do
  not substitute for this.
- **Tests (targeted — the touched packages plus their direct importers, from the
  `go list` query in CLAUDE.md §6):**
  ```bash
  go test ./internal/tooling/task/ ./internal/tooling/terminal/dev_tools/githubcli/ \
          ./internal/tooling/worktree/ ./internal/apps/git/ ./internal/apps/registry/ \
          ./internal/tooling/reviewjournal/ ./internal/tooling/terminal/ \
          ./internal/apps/opencode/ ./internal/tui/worktree/ ./internal/tui/components/ ./cmd/
  make lint
  ```
  The root package `github.com/cjairm/devgeta` is also a direct importer and is
  **in scope for this cycle** (~4.8 min on its own) because Slices C, D, and H may
  edit `configs/shared/commands/`, which its embedded-config tests cover. Run it
  only on the slices that actually touch `configs/`.

---

## 3. Objective

An agent can orient on a tracked issue, hand a working file to a later session,
and read a PR's full standing — each in one `dg task` call — and the three
existing commands with sharp edges (`refresh-branch`'s merge-only behavior,
`review-scope`'s missing sizes, `review-threads`' prose-only output) gain the
option they were missing, without any of it encoding a convention that only holds
in one repository.

---

## 4. Scope Boundary

### In Scope

- [ ] **Slice A — `dg task scratch --key <name>`.** A deterministic, re-derivable
      scratch directory so a file written by one session is findable by a later,
      independent one. Keeps `paths.ScratchAllocPrefix` in the directory name so
      `--clean` keeps working unchanged; sanitizes the key (rejects path
      separators, `.`, `..`, and anything that would escape the root); documents
      that two concurrent sessions sharing a key share the directory. **Requires
      ADR-0033 first** — this changes ADR-0015's ownership contract.
- [ ] **Slice B — `dg task refresh-branch --rebase`.** Rebase the current branch
      onto the freshly-pulled target instead of merging the target into it. The
      merge behavior stays the default in this cycle (see Out of Scope).
- [ ] **Slice C — the issue surface, and `dg task issue-scope <n>`.** Add issue
      methods to the `GithubCli` wrapper and a new
      `internal/tooling/task/issue.go` holding an `IssueManager` that mirrors
      `PRManager`. First and only consumer this cycle: `issue-scope <n>`, which
      returns in one call — the issue's state and title; open PRs that reference
      it, boundary-checked so `#12` never matches inside `#1234`; local branches
      that reference it, matched by number with the same boundary rule and **not**
      by assuming any naming scheme; and any worktree holding one of those
      branches. Named `issue-scope` for consistency with `review-scope`'s
      "orient in one call" precedent. **Requires ADR-0035 first** — the
      issue↔branch association rule is the generality decision in this slice.
- [ ] **Slice D — `dg task pr-state`.** One call answering "where does this PR
      stand?": PR state, a CI checks summary, the unresolved-thread count, and the
      authenticated user's own review state. Composes the four existing
      subcommands' manager methods internally; does not re-query or re-derive any
      fact they already own. Returns counts and states only — never thread bodies
      (task-design.md principle 7). The four existing commands stay, unchanged.
      **Requires ADR-0034 first** — this sets the precedent for aggregate tasks.
- [ ] **Slice E — `dg wt create --base <ref>`.** Let `create` start the new branch
      at an explicit ref while still opening the tmux window and launching the
      coder. Routes through the existing `Git.CreateWorktreeAtBaseIn`. Applies
      only when the branch does not already exist; passing `--base` for a branch
      that is being adopted is an error, not a silent no-op. Note the corrected
      premise: `create` already bases new branches on a freshly-fetched
      `origin/<default>` (`internal/apps/git/git.go:820-833`), falling back to
      `HEAD` only when the remote default is unreachable — so this closes the
      _non-default_ base case, not a staleness bug.
- [ ] **Slice F — `dg task review-scope --sizes`.** Add cumulative two-dot diff
      bytes for the range and per-commit diff bytes to the existing report, so the
      per-commit-vs-range review decision is a ratio rather than a feel. A flag on
      the existing command, not a sibling command (CLAUDE.md §6, reuse before
      writing).
- [ ] **Slice G — `dg task review-threads --json`.** Opt-in machine-readable
      output for reply/resolve loops. Markdown stays the default: JSON costs more
      tokens, and the default must stay the cheap one (task-design.md principle 1).
      The existing markdown sentinels are unchanged contracts.
- [ ] **Slice H — `dg task commit-trailers`.** Parse a commit message's trailer
      block via `git interpret-trailers --parse` and print the parsed trailers;
      `--check` exits non-zero when the block is malformed (present-looking
      trailers that git will not parse as trailers, e.g. missing the blank-line
      separator). `--require <key>` (repeatable) additionally fails when a named
      trailer is absent. **The command hardcodes no trailer names** — which
      trailers a commit needs is a per-repository decision, so it is an argument,
      never a default. Wire it into `configs/shared/commands/smart-commit.md` so
      the shipped commit flow calls it consistently.

### Explicitly Out of Scope

- **Flipping `refresh-branch`'s default from merge to rebase.** Changing the
  behavior of a shipped command sits on CLAUDE.md §10's "never silently" list and
  needs its own discussion and deprecation plan. Slice B adds the option; the
  default change is a separate decision.
- **Auto-detecting a "merge-hostile" repository** so `refresh-branch` can refuse
  on its own. There is no portable signal for "this repo's gate rejects merge
  commits" — branch protection settings, required-status configuration, and
  reviewer conventions all express it differently and none is readable without
  privileged API access. Building a guess here would fail open in the cases that
  matter and produce false refusals in the ones that do not.
- **`issue-view`.** `gh issue view <n>` is already compact, so the saving is
  negligible — the case task-design.md explicitly says not to build for. Slice C
  already covers the round-trip-collapsing case that motivated it.
- **Duplicate-issue search.** A titles-only similarity search across open issues
  and merged PRs is a fuzzy heuristic whose correctness is repository-specific;
  it has the weakest justification of the reported ten and no clear success
  criterion. Revisit only with a concrete accuracy target.
- **Issue dependency management (`blocked_by` links).** The underlying GitHub
  feature's availability varies by plan and repository, so a task wrapping it
  would work for some users and 404 for others. Confirm availability across the
  supported surface first; that confirmation is a prerequisite, not part of this
  cycle.
- **Any per-repository or per-person workflow convention.** Issue-number-derived
  branch names, a fixed set of required trailers, a particular multi-session
  hand-off shape — none of these become defaults. Where behavior needs one, it is
  a flag with no default value.
- **Installing a git hook into a user's repository** to make the trailer check
  non-optional. That is invasive in a way devgeta has never been, and Slice H is
  honest about the consequence: called from a shipped command, the check is
  consistent, not unskippable.

**Scope is locked.** If you discover something out of scope is needed, document it
for a future cycle and reference it here.

---

## 5. Implementation Plan

### Decisions to record before implementation starts

Per CLAUDE.md §10, each of these is a choice between approaches with lasting
impact and needs its ADR written and approved **before** the slice that depends on
it begins.

| ADR      | Title                                                          | Gates   |
| -------- | -------------------------------------------------------------- | ------- |
| ADR-0033 | A keyed scratch directory trades isolation for hand-off        | Slice A |
| ADR-0034 | An aggregate task composes its parts, never re-derives them    | Slice D |
| ADR-0035 | An issue is matched by number, never by a branch-naming scheme | Slice C |

ADR-0033 must state plainly what it gives up: ADR-0015 §3 chose a random suffix so
one invocation could never delete a concurrent session's directory, and a
predictable name reopens exactly that. The argument for accepting it is that the
collision is between the same user's own sessions and the hand-off is otherwise
impossible; the argument must be made, not assumed.

ADR-0034 exists because the alternative — an aggregate that re-queries GitHub
itself — is the easier code to write and gives the same fact two spellings that
can disagree (task-design.md principle 7). Deciding it once stops every future
aggregate task from re-litigating it.

ADR-0035 is the generality decision. The natural implementation of "find the
branch for issue 42" is to look for a branch named `42`, which is one team's
convention and product behavior for everyone else. The ADR fixes the rule: match
the number with word-boundary semantics wherever it appears, report what matched
and how, and never require a shape.

### File Changes

| Action | File Path                                                    | Description                                                         |
| ------ | ------------------------------------------------------------ | ------------------------------------------------------------------- |
| Create | `docs/decisions/ADR-0033-*.md`                               | Keyed scratch allocation, and what it gives up                      |
| Create | `docs/decisions/ADR-0034-*.md`                               | Aggregate tasks compose, never re-derive                            |
| Create | `docs/decisions/ADR-0035-*.md`                               | Issue↔branch association is by number, not by naming scheme         |
| Modify | `internal/tooling/task/scratch.go`                           | `Scratch(key string)`; key sanitizer; keep `--clean` bounds intact  |
| Modify | `internal/tooling/task/scratch_test.go`                      | Keyed allocation, re-derivation, sanitizer rejections, clean parity |
| Modify | `internal/tooling/task/task.go`                              | `RefreshBranch` rebase path; `CommitTrailers`                       |
| Create | `internal/tooling/task/issue.go`                             | `IssueManager`, `resolveOwnerRepoIssue`, `IssueScope`, formatter    |
| Create | `internal/tooling/task/issue_test.go`                        | Orchestration (mocked) + golden-fixture formatter tests             |
| Create | `internal/tooling/task/prstate.go`                           | `PRState` composing the four existing manager methods               |
| Create | `internal/tooling/task/prstate_test.go`                      | Composition + formatter tests                                       |
| Modify | `internal/tooling/task/scope.go`                             | `--sizes`: range and per-commit diff bytes in `formatReviewScope`   |
| Modify | `internal/tooling/task/pr.go`                                | `ReviewThreads` JSON rendering path                                 |
| Modify | `internal/tooling/terminal/dev_tools/githubcli/githubcli.go` | `IssueView`, `SearchIssueReferences` (or GraphQL equivalent)        |
| Modify | `internal/tooling/worktree/worktree.go`                      | Thread `base` through `Create`/`CreateAt`/`create`                  |
| Modify | `cmd/task.go`                                                | `--key`, `--rebase`, `--sizes`, `issue-scope`, `commit-trailers`    |
| Modify | `cmd/task_pr.go`                                             | `pr-state`, `review-threads --json`                                 |
| Modify | `cmd/worktree.go`                                            | `--base` flag on `create`, plus its mutual-exclusion error          |
| Modify | `configs/shared/commands/smart-commit.md`                    | Call `commit-trailers --check` in the commit flow                   |
| Modify | `docs/guides/task-design.md`                                 | Note the aggregate-task rule once ADR-0034 lands                    |
| Modify | `docs/spec.md`                                               | Document the new subcommands and flags                              |

### Step-by-Step

Slices are independently shippable and ordered by value-to-risk. Each ends at a
committable state.

#### Slice A — keyed scratch (ADR-0033 first)

**Step A1: Write ADR-0033.** State the hand-off problem, the isolation property
being traded, and why the trade is acceptable between one user's own sessions.
Verify: the ADR answers "what breaks if two sessions pick the same key" explicitly.

**Step A2: Add the key sanitizer.** A pure function rejecting path separators,
`.`, `..`, empty/whitespace-only keys, and anything that would not survive as a
single path element. Table-driven test first — this is the security boundary.
Verify: `go test -run TestScratchKey ./internal/tooling/task/`

**Step A3: Thread the key through allocation.** `Scratch(key)` with an empty key
keeping today's `os.MkdirTemp` behavior byte-for-byte; a non-empty key producing
`<root>/<ScratchAllocPrefix><sanitized-key>`, created idempotently (an existing
directory is returned, not an error). Verify: allocation with the same key twice
returns the same path; `--clean` accepts it unchanged.

**Step A4: Wire `--key` in cobra and update the command's long help.** The help
must say the directory is shared, not private, when a key is given.
Verify: `go test ./internal/tooling/task/ ./cmd/` and `dg task scratch --key demo` twice.

#### Slice B — `refresh-branch --rebase`

**Step B1:** Add the rebase path to `RefreshBranch` behind a parameter, leaving
the merge path untouched. Verify: `go test -run TestRefreshBranch ./internal/tooling/task/`

**Step B2:** Register `--rebase` and document in the long help that merge is still
the default and why a caller might want the other. Verify: `go test ./cmd/`

#### Slice C — issue surface + `issue-scope` (ADR-0035 first)

**Step C1: Write ADR-0035.** Fix the matching rule and the reporting obligation
(say what matched and how, so a caller can see a coincidental match for what it is).

**Step C2: Measure the baseline.** Capture the bytes an agent ingests today
answering "what already exists for issue N" in this repository, by the raw route.
Record it in this doc. If the eventual delta plus round-trips does not justify the
slice, stop here.

**Step C3: Add issue methods to the `GithubCli` wrapper.** Minimum needed for
`issue-scope`; no speculative methods (CLAUDE.md §6, "prefer existing over new").
Verify: `go test ./internal/tooling/terminal/dev_tools/githubcli/`

**Step C4: Add `IssueManager` and the boundary-checked reference matcher.** The
matcher is a pure function with a table-driven test covering `#12` vs `#1234`,
`#12x`, `x#12`, `12` bare, and the same cases in branch names.
Verify: `go test -run TestIssueRef ./internal/tooling/task/`

**Step C5: Implement `IssueScope` orchestration + pure formatter.** Labeled plain
text, stable sentinel when nothing references the issue.
Verify: golden-fixture formatter tests pass; `VerifyNoRealCommands` on the
orchestration tests.

**Step C6: Register `issue-scope`, measure the after-figure, record both.**
Verify: `go test ./internal/tooling/task/ ./cmd/`

#### Slice D — `pr-state` (ADR-0034 first)

**Step D1: Write ADR-0034.**

**Step D2: Implement `PRState` by calling the four existing manager methods** and
reducing their output to states and counts. No new `gh` calls. Verify: the
orchestration test asserts exactly which existing methods ran.

**Step D3: Register `pr-state`; leave the four originals untouched.** Verify:
`go test ./internal/tooling/task/ ./cmd/` and a manual run against a live PR.

#### Slice E — `dg wt create --base <ref>`

**Step E1:** Thread an optional base through `create`, dispatching to
`CreateWorktreeAtBaseIn` when set. Error — not a silent no-op — when the branch
already exists and would be adopted. Verify: `go test ./internal/tooling/worktree/`

**Step E2:** Register `--base` on `worktreeCreateCmd`; document that omitting it
keeps the existing freshly-fetched-default behavior. Verify: `go test ./cmd/`

#### Slice F — `review-scope --sizes`

**Step F1:** Extend the scope computation with range and per-commit diff byte
counts. Verify: `go test -run TestReviewScope ./internal/tooling/task/`

**Step F2:** Render them in `formatReviewScope` only when the flag is set, so the
default output's byte count is unchanged. Verify: existing golden fixtures still
pass untouched.

#### Slice G — `review-threads --json`

**Step G1:** Add the JSON rendering path alongside the markdown formatter; the
markdown path and its sentinels must be byte-identical to today.
Verify: existing `review-threads` fixtures pass unmodified.

**Step G2:** Register `--json`. Verify: `go test ./internal/tooling/task/ ./cmd/`

#### Slice H — `commit-trailers`

**Step H1:** Implement the parse via `git interpret-trailers --parse` through the
git app wrapper (never a raw `exec.Command`), with `--check` and repeatable
`--require`. Verify: table-driven tests including the missing-blank-line case that
motivated the slice.

**Step H2:** Register the command; update
`configs/shared/commands/smart-commit.md` to call it. Verify:
`go test ./internal/tooling/task/ ./cmd/ .` — the root package is required here
because `configs/` changed.

---

## 6. Verification Plan

### Automated Verification

```bash
# Touched packages + their direct importers (from the CLAUDE.md §6 go list query)
go test ./internal/tooling/task/ ./internal/tooling/terminal/dev_tools/githubcli/ \
        ./internal/tooling/worktree/ ./internal/apps/git/ ./internal/apps/registry/ \
        ./internal/tooling/reviewjournal/ ./internal/tooling/terminal/ \
        ./internal/apps/opencode/ ./internal/tui/worktree/ ./internal/tui/components/ ./cmd/

# Only on slices that touch configs/ (Slice H, and C/D if their shipped commands change)
go test .

make lint
```

### Manual Verification

1. `dg task scratch --key demo` twice → same path both times; `dg task scratch`
   with no key → a fresh unique path each time; `dg task scratch --clean` accepts
   both forms and still refuses the root, a grandchild, and a path without the
   allocation prefix.
2. `dg task scratch --key ../escape` and `--key a/b` → refused with an actionable
   message, nothing created.
3. `dg task refresh-branch --rebase` on a branch behind the default → linear
   history, no merge commit. Without the flag → unchanged behavior.
4. `dg task issue-scope <n>` on an issue with a live PR → reports it; on an issue
   whose number is a substring of another's → does not report the other; on an
   issue nothing references → prints the sentinel, exit 0.
5. `dg task pr-state` against a PR with failing checks and unresolved threads →
   one payload whose every field agrees with running the four originals.
6. `dg wt create tmp-base-check --base <a tag or release branch>` → worktree starts
   at that ref **and** the tmux window opens with the coder running.
7. `dg task review-scope` → byte-identical to before the cycle;
   `--sizes` → adds the size lines and nothing else.
8. `dg task review-threads` → byte-identical to before; `--json` → parses as JSON
   and carries every thread and comment id the markdown showed.
9. A commit message with `Co-Authored-By` on the line directly after the body →
   `commit-trailers --check` exits non-zero; with the blank line → exits 0.
   `--require Signed-off-by` on a message lacking it → non-zero.

### Regression Check

- `dg task --help` lists the new subcommands; every existing one still runs.
- `dg wt create <name>` with no `--base` behaves exactly as before (fetch,
  `origin/<default>`, window, coder).
- Existing `review-scope` / `review-threads` golden fixtures pass **unmodified** —
  if a fixture needs editing, a default-path contract was broken.
- `dg install --help`, `dg version`, `dg ws` unaffected.
- Both agents deployed after any `configs/` change: `dg configure claude --force`
  **and** `dg configure opencode --force` (CLAUDE.md §12).

---

## 7. Risks & Trade-offs

| Risk                                                                         | Likelihood | Mitigation                                                                                                                                            |
| ---------------------------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| A keyed scratch path becomes a cross-session footgun (two sessions, one key) | Med        | ADR-0033 states the trade; the key is opt-in; `--clean` bounds checks stay untouched                                                                  |
| Key sanitizer misses an escape and `--clean` deletes outside the root        | Low        | Sanitizer is a pure function with an adversarial table test; the existing lexical + resolved bounds checks in `ScratchClean` remain as a second layer |
| `issue-scope` encodes a branch-naming convention by accident                 | **High**   | ADR-0035 decided before code; boundary matcher is a separately tested pure function; output says what matched and how                                 |
| `pr-state` drifts from the four commands it summarizes                       | Med        | ADR-0034: compose the existing manager methods, never re-query; orchestration test asserts which methods ran                                          |
| A new flag changes default output and silently breaks a shipped config       | Med        | Every default-path golden fixture must pass unmodified; a fixture edit is treated as a broken contract                                                |
| Issue API shape differs across GitHub plans/hosts                            | Med        | Slice C uses only core issue read APIs; the dependency-link feature is explicitly out of scope until availability is confirmed                        |
| Cycle is large enough to stall as one unit                                   | Med        | Eight independently shippable slices; A, B, F, G, E are each small and land alone                                                                     |

### Trade-offs Made

- **Keyed scratch is opt-in, not the new default.** The unique-suffix allocation
  stays the default because it is the safer of the two, and the hand-off case is
  the minority.
- **`pr-state` is added, not substituted.** Keeping the four single-purpose
  commands costs a little surface area and buys the ability to ask one narrow
  question cheaply — and keeps their sentinels, which shipped configs depend on.
- **`refresh-branch` keeps merging by default.** The safer behavior is arguably
  rebase, but changing a shipped command's behavior without a deprecation plan is
  the thing CLAUDE.md §10 forbids. The option lands now; the default is a separate
  conversation.
- **`review-scope --sizes` is a flag, not a `branch-stats` command.** Reuse over a
  second command that would compute most of the same thing.
- **The trailer check is called by a shipped command, not enforced by a hook.**
  A hook installed into a user's repository would make it unskippable, and that is
  more invasive than the problem warrants. Stated plainly rather than papered over:
  a command that must be called can be skipped.
- **Six reported asks became one issue surface.** Building the wrapper methods and
  `IssueManager` once, with a single consumer, is cheaper than six independent
  additions and leaves the second consumer trivial.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear? (Is it obvious why "no issue surface" is one gap and not six?)
- [ ] Engineer context sufficient? (Are the two generality traps in §2 concrete enough to avoid?)
- [ ] Objective unambiguous?
- [ ] Scope is actually locked? (Are the four dropped asks dropped for stated reasons?)
- [ ] Steps are actionable? (Each 5-15 min, with clear success criteria?)
- [ ] Verification is executable?
- [ ] Risks are realistic? (Is the branch-naming risk rated high enough?)

**Reviewer notes:**
(Fill in during review.)

---

## Notes for Implementers

- **Write the three ADRs before the slices they gate.** A cycle doc's trade-offs
  section does not replace an ADR (CLAUDE.md §10).
- **Measure before and after, per slice, in this repository.** The report that
  prompted this cycle carried figures from someone else's repo; they are context,
  not evidence.
- **Every new GitHub or git call goes through the app wrapper.** No raw
  `exec.Command`, no hand-assembled `CommandParams`.
- **A golden fixture that needs editing is a broken contract, not a stale test.**
  Stop and check whether a default output path changed.
- **Commit after each step** once its verify check passes.
- **If a slice's measurement does not justify it, drop that slice and say so here.**
  Dropping one does not block the others.
