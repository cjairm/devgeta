# Cycle: Issue Surface and Task Gaps — orienting on an issue, keyed scratch, and six smaller closures

**Date:** 2026-08-25
**Estimated Duration:** ~16 hours (8 slices, independently shippable)
**Status:** In Progress (2026-08-26) — four cross-model review rounds settled,
all findings fixed. The three gating ADRs (0033/0034/0035) are written. Slices A
(`dg task scratch --key`), B (`refresh-branch --rebase`), and C (the issue
surface + `issue-scope`) are implemented and tested on this branch; Slices D–H
remain.

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
- **Nothing confirms a required trailer actually parsed.** No use of
  `git interpret-trailers` anywhere in the tree. A trailer separated from the body
  by the wrong whitespace is silently not a trailer, and a substring grep cannot
  tell the difference — so a commit that was supposed to carry, say, a co-author
  trailer can land without one. (Detecting this in the general case is undecidable
  from syntax alone — see Slice H — so the fix is scoped to trailer keys the caller
  names.)

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

- [x] **Slice A — `dg task scratch --key <name>`.** A deterministic, re-derivable
      scratch directory so a file written by one session is findable by a later,
      independent one. Sanitizes the key (rejects path separators, `.`, `..`,
      empty/whitespace, and anything that would escape the root); documents that
      two concurrent sessions sharing a key share the directory. Two lifecycle
      facts that the first draft got wrong and this slice must handle: - **Distinct prefix, exempt from age-pruning.** Keyed dirs use a **separate**
      `paths.ScratchKeyPrefix` (not `ScratchAllocPrefix`), and
      `baseapp.MaintainScratchDir` skips that prefix entirely rather than pruning
      it at 24h (`internal/apps/baseapp/configure.go:84-105`). Reusing
      `ScratchAllocPrefix` would let configure-time maintenance delete a keyed
      dir out from under a hand-off, so a later session re-deriving the key would
      silently find an empty recreated directory. `ScratchClean`'s bounds checks
      (`scratch.go:146-154`) are widened to accept **either** prefix so `--clean`
      works on both forms. Trade recorded in ADR-0033: keyed dirs persist until
      explicitly `--clean`ed — a durable hand-off costs a dir that age-pruning no
      longer reaps. - **Symlink-safe idempotent allocation.** Returning an existing keyed dir is
      only safe after an `Lstat` proving it is a real directory, not a symlink a
      concurrent process substituted, and after resolving containment under the
      root (the same defense `ScratchClean` already applies to cleanup, now also
      applied to the write path). A symlink or non-directory at the keyed path is
      an error, never reused.
      **Requires ADR-0033 first** — this changes ADR-0015's ownership contract.
- [x] **Slice B — `dg task refresh-branch --rebase`.** Rebase the current branch
      onto the freshly-pulled target instead of merging the target into it. The
      merge behavior stays the default in this cycle (see Out of Scope).
- [x] **Slice C — the issue surface, and `dg task issue-scope <n>`.** Add issue
      methods to the `GithubCli` wrapper and a new
      `internal/tooling/task/issue.go` holding an `IssueManager` that mirrors
      `PRManager`. First and only consumer this cycle: `issue-scope <n>`, which
      returns in one call — the issue's state and title; PRs that reference it;
      local branches that reference it; and any worktree holding one of those
      branches. Named `issue-scope` for consistency with `review-scope`'s "orient
      in one call" precedent. The association rules — the generality decision of
      this slice — are fixed by ADR-0035 and summarized here so the slice is
      implementable without opening it: - **PR references come from GitHub's own cross-reference, not text grep.**
      The accepted source is the issue's **timeline cross-referenced events**
      (`gh issue view <n> --json` does not carry these; use the GraphQL
      `timelineItems(itemTypes: [CROSS_REFERENCED_EVENT])` connection through the
      `gh` wrapper), which is what GitHub itself uses to show "linked" PRs and is
      already boundary-correct. Same-repository references only this cycle;
      cross-repository events are filtered out (reported neither as candidates
      nor confirmed). Paginate the connection; do not cap silently. - **Branch references are textual candidates, never confirmed links.** A
      local branch is reported only when the branch name contains the **exact
      digit run** of the issue number, delimited on both sides by a **non-neighbor**
      character — precisely: the character immediately before (if any) and
      immediately after (if any) must not be in `[0-9A-Za-z.]`, with an optional
      single `#` allowed immediately before. This is the grammar, not "word
      boundary" (a regex `\b` sits between `.` and a digit, so `\b` would wrongly
      match `12` inside `v1.12`). Worked from that one rule: `#12`, `12`, `x#12`
      match; `#1234` (run is `1234`), `#12x` (letter after), `v1.12` (`.` before),
      `2012` (digit neighbors), `12.3` (`.` after) do not. Every match is labeled a
      _candidate_ in the output — never a confirmed association, because a bare
      number in a branch name is a coincidence as often as a link. No naming scheme
      is assumed.
      **Requires ADR-0035 first** — see §"Decisions to record" for the full grammar
      ADR-0035 must pin down (accepted sources, same-repo vs cross-repo,
      pagination, candidate-vs-confirmed reporting).
- [ ] **Slice D — `dg task pr-state`.** One call answering "where does this PR
      stand?": PR state, a CI checks summary, the unresolved-thread count, and the
      authenticated user's own review state. Returns counts and states only —
      never thread bodies (task-design.md principle 7). The four existing commands
      stay, unchanged. **Requires ADR-0034 first** — this sets the precedent for
      aggregate tasks. Two constraints the first draft missed, because the existing
      methods return **formatted display text**, not structured data: - **Compose structured gatherers, not formatted output.** `PRView`,
      `PRChecks`, `ReviewThreads`, and `PRReviewState`
      (`pr.go:319-325,386-426,77-121`, `prreviewstate.go:100-141`) each return a
      rendered string — markdown, in the threads case. `pr-state` cannot count
      threads by parsing markdown or re-derive a checks bucket from a formatted
      line. ADR-0034's mechanism: extract the raw-fetch-plus-reduce core of each
      into a **structured gatherer** (returning a small struct — lifecycle,
      check buckets, unresolved count, my-review state), and have **both** the
      existing command and `pr-state` render from the same struct. The existing
      commands' output stays byte-identical; the shared gatherer is the new
      seam, not a second query. - **No incidental heavy work.** `PRChecks` fetches a failing job's log digest
      per failing check (`pr.go:386-426`, `RunFailedJobLog`). `pr-state` needs
      only the pass/fail/pending **counts**, so its checks gatherer must stop at
      the bucket tally and make **zero** `RunFailedJobLog` calls. **Resolve
      owner/repo/PR once, then pass the resolved identifiers to every gatherer** —
      each existing method calls `resolveOwnerRepoPR`, which always calls
      `CurrentRepo` and (when `--pr` is absent) `CurrentPRNumber` (`pr.go:52-74`),
      so letting each gatherer re-resolve would multiply those calls. The gatherers
      must accept an already-resolved `(owner, name, pr)` so resolution happens
      exactly once. `pr-state` also skips `FetchPRDiscussion` entirely: the
      unresolved-thread **count** comes from the review-threads query, and summary
      bodies are not part of this payload. The resulting budget, asserted in the
      orchestration test for **both** input modes:
      | Call | With `--pr` | Inferred (no `--pr`) |
      | --- | --- | --- |
      | `CurrentRepo` | 1 | 1 |
      | `CurrentPRNumber` | 0 | 1 |
      | `PRView` (metadata + review-state fields) | 1 | 1 |
      | `PRChecks` | 1 | 1 |
      | `FetchReviewThreads` (paginated) | 1 | 1 |
      | `AuthenticatedLogin` | 1 | 1 |
      | **Total** | **5** | **6** |
      Partial-failure behavior: if one gatherer errors (e.g. checks unavailable),
      that field reports an explicit "unavailable" sentinel and the rest still
      render — one failed fetch does not fail the whole command.
- [ ] **Slice E — `dg wt create --base <ref>`.** Let `create` start the new branch
      at an explicit ref while still opening the tmux window and launching the
      coder. Applies only when the branch does not already exist; passing `--base`
      for a branch that is being adopted is an error, not a silent no-op. Corrected
      premise: `create` already bases new branches on a freshly-fetched
      `origin/<default>` (`internal/apps/git/git.go:820-833`), falling back to
      `HEAD` only when the remote default is unreachable — so this closes the
      _non-default_ base case, not a staleness bug. But `CreateWorktreeAtBaseIn`
      (`git.go:719-724`) is a bare `git worktree add -b <branch> <path> <base>`: it
      skips the fetch, the local/remote-existence checks, and the collision
      handling that `createWorktreeIn` (`git.go:779-833`) does. Routing `--base`
      straight through it would resurrect exactly the staleness and divergence bugs
      the normal path avoids. So this slice must: - **Give `CreateWorktreeAtBaseIn` the same pre-flight as the normal path** —
      fetch `origin` first (best-effort, bounded, same as `createWorktreeIn`),
      reject when a local **or** remote branch of that name already exists
      (that is the "adopt, don't re-base" error), and resolve/validate the base
      ref before any directory is created. Validate before `mkdir`, so a bad base
      leaves nothing behind. - **Update every caller atomically.** Threading a base through
      `worktree.Create`/`CreateAt`/`create` changes signatures the TUI depends on
      (`internal/tui/worktree/model.go:442` calls `mgr.CreateAt`). The signature
      change and all its callers (cmd, TUI) land in one commit — see the
      committability note in §"Step-by-Step".
- [ ] **Slice F — `dg task review-scope --sizes`.** Add diff sizes to the existing
      report so the per-commit-vs-range review decision is a ratio rather than a
      feel. A flag on the existing command, not a sibling command (CLAUDE.md §6,
      reuse before writing). "Size" is defined precisely so it is testable: - **Unit: bytes of unified-patch text.** The range size is
      `len(git diff <base>)` — the byte length of the same two-dot patch the file
      table already computes (`scope.go:133-139`), so it **includes staged and
      unstaged working-tree edits**, matching the file counts shown right beside
      it. Per-commit size is `len(git diff <commit>^ <commit>)` for each commit
      in the range's `commitLog`. - **Same exclusion filter as the file table.** Excluded paths
      (`partitionExcluded`) are diffed out before counting, so a lockfile nobody
      reviews does not inflate the size — the size and the reviewable-file list
      agree. Binary files: git emits `Binary files a/x and b/x differ` in the
      patch; those bytes count (they are part of the patch), but no blob content
      is counted, because `git diff` does not emit it without `--binary`, which
      this does not pass. Untracked files are not in any diff and are not counted
      (they are already listed separately). Merge commits in the range are shown
      with `git diff <commit>^ <commit>` (first-parent), consistent with a
      review reading the merge as one delta. - Fixtures assert exact byte counts against a fixed diff, so the semantics are
      pinned by test, not prose.
- [ ] **Slice G — `dg task review-threads --json`.** Opt-in machine-readable
      output for reply/resolve loops. Markdown stays the default: JSON costs more
      tokens, and the default must stay the cheap one (task-design.md principle 1).
      The existing markdown sentinels are unchanged contracts. The first draft
      called this "an alternate renderer," which the underlying fetch shape makes
      impossible — so the slice must define a real schema and aggregation: - **One versioned document, all pages merged.** `FetchReviewThreads` runs
      `gh ... --paginate`, which emits **one JSON document per page**
      (`githubcli.go:255-268`), and discussion (review summaries + conversation
      comments) comes from a **separate** `FetchPRDiscussion` query
      (`githubcli.go:233-284`). A pass-through renderer cannot produce one valid
      document. `--json` must parse and **merge** all thread pages plus the
      discussion query into a single object with a top-level
      `"schemaVersion": 1`, so consumers can pin it. - **`--state` preserved.** The `--state unresolved|resolved|all` filter the
      markdown path honors applies identically to the JSON path. - **Truncation is explicit, never silent.** Nested `comments(first: 100)`,
      `reviews(first: 100)`, and conversation `comments(first: 100)` are capped
      and **not** paginated today (documented at `githubcli.go:178-179,229-232`).
      The JSON document carries a `"truncated"` boolean (and which collection)
      wherever a cap was hit, so a machine consumer is told the data is partial
      rather than silently receiving less. Paginating those nested collections is
      out of scope; surfacing the cap is not. - **The queries must gain a truncation signal — node count alone cannot
      report it.** The three capped connections today request only `nodes`
      (`githubcli.go:192-203,236-241`), so a length of exactly 100 is
      indistinguishable from >100. Slice G adds `pageInfo { hasNextPage }` (or
      `totalCount`) to each capped nested connection — per-thread `comments`,
      `reviews`, and conversation `comments` — and derives `"truncated"` from
      `hasNextPage`, never from `len(nodes) == 100`. The test asserts exactly-100
      (not truncated) separately from 101+ (truncated), which a count-based check
      would get wrong.
- [ ] **Slice H — `dg task commit-trailers`.** Parse a commit message's trailer
      block via `git interpret-trailers --parse` and print the parsed trailers;
      `--require <key>` (repeatable) exits non-zero when a named trailer is **not
      among the parsed trailers**. **The command hardcodes no trailer names** —
      which trailers a commit needs is a per-repository decision, so it is an
      argument, never a default. The contract, corrected after a review round
      showed the earlier "detect any malformed block" design was undecidable: - **Enforcement is scoped to declared keys (`--require`), never guessed from
      syntax.** The earlier design scanned the last block for trailer-looking
      lines and failed when `--parse` recognized fewer than it counted. Verified
      against git 2.51.1, that **rejects valid prose**: for
      `Subject\n\nReason: this is needed\nMore explanation.`, git parses **zero**
      trailers (a trailer line followed by a prose line is not a recognized
      block), while the scanner sees `Reason:` as trailer-looking → one detected,
      zero parsed → false-positive rejection of an ordinary commit message. Since
      `smart-commit.md` runs this on every commit, that false positive would be
      shipped behavior. Syntax alone cannot tell "a trailer whose blank separator
      is missing" from "prose containing a label," so the command does not try.
      Instead, the missing-separator case is caught **for the keys the caller
      declares**: a required `Co-authored-by` glued to the body is simply absent
      from the parsed trailers (confirmed: git parses nothing for
      `Subject\nCo-authored-by: A <a@x>`), so `--require Co-authored-by` fails —
      with an error that points at the likely cause (not separated from the body
      by a blank line). A prose `Reason:` the caller did not name is ignored. Key
      comparison is case-insensitive, matching git. - **Input is an explicit `--message-file <path>`** (falling back to stdin),
      never an implicit "current commit." The command reads a candidate message
      file and validates it _before_ it becomes a commit. - **`smart-commit.md` requires only the trailers it generated for that
      commit.** Being a shipped, general command, it hardcodes no key (not even
      `Co-authored-by` — that is devgeta's own convention, CLAUDE.md principle 8).
      When it adds a trailer for the commit at hand (e.g. a co-author the user
      supplied), it passes `--require` for exactly that key, guarding its own
      formatting; when it adds none, it passes none and the check is a successful
      no-op. It writes the message to a temp file, runs
      `commit-trailers --require <generated-keys> --message-file <file>`, then
      commits the validated bytes with `git commit -F <file>` (`git commit -m`
      cannot guarantee the validated bytes are the committed ones).

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
impossible; the argument must be made, not assumed. ADR-0033 also records the two
lifecycle decisions Slice A forces (see Slice A): keyed dirs get a **distinct
prefix that age-pruning skips** (so a hand-off is not silently reaped at 24h, at
the cost of a dir that lives until `--clean`ed), and allocation is
**symlink-safe** (`Lstat` + containment before an existing keyed dir is reused).

ADR-0034 exists because the alternative — an aggregate that re-queries GitHub
itself — is the easier code to write and gives the same fact two spellings that
can disagree (task-design.md principle 7). Deciding it once stops every future
aggregate task from re-litigating it. The ADR must also fix the **mechanism**,
because the existing methods return formatted display text, not data: an aggregate
composes **structured gatherers** (the raw-fetch-plus-reduce core of each command,
returning a struct), which both the original command and the aggregate render
from — it never parses another command's rendered output, and it never triggers a
sibling's incidental heavy work (e.g. `PRChecks`'s per-failure log fetch) to get a
count. Partial-failure behavior (one gatherer failing yields an "unavailable"
field, not a failed command) and the aggregate's bounded gh-call count are part of
the decision.

ADR-0035 is the generality decision, and it must be exact about **what
establishes an association**, not only the branch rule:

- **PR↔issue:** the accepted source is GitHub's own **cross-referenced timeline
  events** (GraphQL `CROSS_REFERENCED_EVENT`), same-repository only this cycle;
  body/title/comment text grep is explicitly **not** an accepted source (it is the
  boundary-wrong route this cycle exists to replace). Pagination is required;
  cross-repo events are filtered out.
- **Branch↔issue:** the branch name contains the **exact digit run** of the issue
  number, and the characters immediately before and after that run (if any) are
  **not** in `[0-9A-Za-z.]` (an optional single `#` may precede). ADR-0035 states
  that rule once and derives every example from it — `#12`/`12`/`x#12` match,
  `#1234`/`#12x`/`v1.12`/`2012`/`12.3` do not — rather than saying "word boundary,"
  which a regex `\b` would satisfy between `.` and a digit (wrongly matching
  `v1.12`). Every match is a **candidate**, reported as such and never as a
  confirmed link; no naming scheme (`42` == "the branch for 42") is assumed.

The through-line: report what matched, from which source, and how strong the link
is, so a caller can see a coincidental match for what it is.

### File Changes

| Action | File Path                                                                 | Description                                                                                                                |
| ------ | ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Create | `docs/decisions/ADR-0033-*.md`                                            | Keyed scratch allocation, and what it gives up                                                                             |
| Create | `docs/decisions/ADR-0034-*.md`                                            | Aggregate tasks compose, never re-derive                                                                                   |
| Create | `docs/decisions/ADR-0035-*.md`                                            | Issue↔branch association is by number, not by naming scheme                                                                |
| Modify | `internal/apps/baseapp/configure.go`                                      | `MaintainScratchDir` skips the new keyed prefix (no age-prune)                                                             |
| Modify | `pkg/paths/*.go`                                                          | Add `ScratchKeyPrefix`, distinct from `ScratchAllocPrefix`                                                                 |
| Modify | `internal/tooling/task/scratch.go`                                        | `Scratch(key string)`; sanitizer; symlink-safe reuse; `--clean` accepts both prefixes                                      |
| Modify | `internal/tooling/task/scratch_test.go`                                   | Keyed allocation, re-derivation, sanitizer + symlink-substitution rejections, clean parity                                 |
| Modify | `internal/tooling/task/task.go`                                           | `RefreshBranch` rebase path; `CommitTrailers` (reads `--message-file`/stdin)                                               |
| Create | `internal/tooling/task/issue.go`                                          | `IssueManager`, `resolveOwnerRepoIssue`, `IssueScope`, boundary matcher, formatter                                         |
| Create | `internal/tooling/task/issue_test.go`                                     | Orchestration (mocked) + golden-fixture formatter tests                                                                    |
| Create | `internal/tooling/task/prstate.go`                                        | `PRState` composing structured gatherers (not formatted output)                                                            |
| Create | `internal/tooling/task/prstate_test.go`                                   | Composition + formatter tests; asserts bounded gh-call count                                                               |
| Modify | `internal/tooling/task/pr.go`, `prreviewstate.go`                         | Extract structured gatherers behind `PRView`/`PRChecks`/`ReviewThreads`/`PRReviewState`; `ReviewThreads` gains a JSON path |
| Modify | `internal/tooling/task/scope.go`                                          | `--sizes`: range and per-commit patch-byte counts in `formatReviewScope`                                                   |
| Modify | `internal/tooling/terminal/dev_tools/githubcli/githubcli.go`              | `IssueView` + issue timeline cross-reference query (GraphQL); merge review-thread pages + discussion for JSON              |
| Modify | `internal/apps/git/git.go`                                                | `CreateWorktreeAtBaseIn` gains fetch + collision + base-validation pre-flight                                              |
| Modify | `internal/tooling/worktree/worktree.go`, `internal/tui/worktree/model.go` | Thread `base` through `Create`/`CreateAt`/`create` and its TUI caller (one commit)                                         |
| Modify | `cmd/task.go`                                                             | `--key`, `--rebase`, `--sizes`, `issue-scope`, `commit-trailers`                                                           |
| Modify | `cmd/task_pr.go`                                                          | `pr-state`, `review-threads --json`                                                                                        |
| Modify | `cmd/worktree.go`                                                         | `--base` flag on `create`, plus its mutual-exclusion error                                                                 |
| Modify | `configs/shared/commands/smart-commit.md`                                 | Validate generated trailers via `commit-trailers --require <keys>`, then `git commit -F`                                   |
| Modify | `docs/guides/task-design.md`                                              | Note the aggregate-task rule once ADR-0034 lands                                                                           |
| Modify | `docs/spec.md`                                                            | Document the new subcommands and flags                                                                                     |

### Step-by-Step

Slices are independently shippable and ordered by value-to-risk. Each ends at a
committable state.

**Committability rule (applies to every step):** a step that changes a public
signature (`Scratch`, `RefreshBranch`, the review-scope compute helper,
`ReviewThreads`, `Create`/`CreateAt`) updates **every caller and its tests in the
same step and the same commit** — the intermediate tree must build. Each step's
verify therefore begins with `go build ./...`; the targeted `go test` follows.
Steps below are grouped so no step leaves a broken build; where an earlier draft
split a signature change from its callers, they are merged here.

**Workflow order (CLAUDE.md §6):** implement → verify manually → then add or update
tests. The one nuance this cycle keeps: the pure security boundaries (the scratch
key sanitizer, the issue boundary matcher, the trailer detector) are exercised by
their table tests as the manual verification, and those adversarial tables are
written alongside the function, not deferred — they _are_ how you confirm those
functions work. That is table-driven verification of a pure function, not
test-first development of a feature.

#### Slice A — keyed scratch (ADR-0033 first)

**Step A1: Write ADR-0033.** State the hand-off problem, the isolation property
being traded, the distinct-prefix/no-prune and symlink-safe decisions, and why the
trade is acceptable between one user's own sessions. Verify: the ADR answers "what
breaks if two sessions pick the same key" and "what happens after 24h" explicitly.

**Step A2: Add the key sanitizer and the `ScratchKeyPrefix` constant.** A pure
function rejecting path separators, `.`, `..`, empty/whitespace-only keys, and
anything that would not survive as a single path element, with an adversarial
table exercising each rejection. Verify: `go build ./...`;
`go test -run TestScratchKey ./internal/tooling/task/`

**Step A3: Thread the key through allocation, symlink-safe, and teach `--clean`
and `MaintainScratchDir` the new prefix — one commit.** `Scratch(key)` with an
empty key keeping today's `os.MkdirTemp` behavior byte-for-byte; a non-empty key
producing `<root>/<ScratchKeyPrefix><sanitized-key>`, created idempotently but only
after an `Lstat` proving an existing entry is a real directory (not a symlink) and
resolving its containment under the root — a symlink or non-directory is an error,
never reused. In the same commit: widen `ScratchClean`'s bounds to accept both
prefixes, and make `MaintainScratchDir` skip `ScratchKeyPrefix` so age-pruning
never reaps a keyed dir. Update the `Scratch()` caller in `cmd/`. Verify:
`go build ./...`; same key twice returns the same path; a symlink substituted at
the keyed path is rejected; `--clean` accepts both forms; a keyed dir survives a
`MaintainScratchDir` pass regardless of age.

**Step A4: Wire `--key` in cobra and update the command's long help.** The help
must say the directory is shared, not private, when a key is given, and that a
keyed dir persists until `--clean`ed. Verify: `go build ./...`; the **full**
`go test ./...` (Slice A touches `pkg/paths` and `baseapp`, both broad-radius — see
§6) and `dg task scratch --key demo` twice.

#### Slice B — `refresh-branch --rebase`

**Step B1:** Add the rebase path to `RefreshBranch` behind a parameter, leaving
the merge path untouched. Verify: `go test -run TestRefreshBranch ./internal/tooling/task/`

**Step B2:** Register `--rebase` and document in the long help that merge is still
the default and why a caller might want the other. Verify: `go test ./cmd/`

#### Slice C — issue surface + `issue-scope` (ADR-0035 first)

**Step C1: Write ADR-0035.** Fix the accepted sources and grammar (PR↔issue via
cross-referenced timeline events, same-repo only, paginated; branch↔issue as the
exact digit run with neighbors outside `[0-9A-Za-z.]`, reported as a candidate,
never confirmed) and the reporting obligation — say what matched, from which
source, and how strong.

**Step C2: Measure the baseline.** Capture the bytes an agent ingests today
answering "what already exists for issue N" in this repository, by the raw route.
Record it in this doc. If the eventual delta plus round-trips does not justify the
slice, stop here.

**Measured 2026-08-26, against `cjairm/devgeta` issue #3 (the repository's only
issue), the raw route the domain-context section names — list-PRs-and-grep:**

| Call                                                          | Bytes ingested |
| ------------------------------------------------------------- | -------------- |
| `gh issue view 3 --json number,title,state,body`              | 4,590          |
| `gh pr list --state all --json number,title,body,headRefName` | 8,527          |
| `git branch -a` (local, then grepped by hand for "3")         | 92             |
| **Total, three round-trips (two network, one local)**         | **~13,209**    |

That total is the cost of confirming the answer is "nothing references issue
#3" — every PR's full body has to be fetched and grepped before a caller can
conclude none of them mention it, and the grep itself is boundary-wrong (would
false-positive on `#13`, `#31`, `#300`, none of which happen to exist here).
The GraphQL cross-reference check that Slice C uses instead
(`timelineItems(itemTypes: [CROSS_REFERENCED_EVENT])`) returns `{"nodes":
[]}` in one call, confirming both of this repo's PRs (#1 `001-binary-dist-audit`,
#2 `002-debian-package-fixes`) are unrelated to issue #3 — no PR body fetch, no
grep, no boundary risk. `issue-scope 3`'s eventual output is one stable
sentinel line. The round-trip count drops from three to one, and the byte cost
from ~13KB to whatever the sentinel line costs (measured after C6, below) —
comfortably justifying the slice on this repository's own numbers, not the
external report's.

**Step C3: Add issue methods to the `GithubCli` wrapper.** `IssueView` for state +
title, and the timeline cross-reference query (GraphQL
`CROSS_REFERENCED_EVENT`, paginated, same-repo filtered). Minimum needed for
`issue-scope`; no speculative methods (CLAUDE.md §6, "prefer existing over new").
Verify: `go build ./...`; `go test ./internal/tooling/terminal/dev_tools/githubcli/`

**Step C4: Add `IssueManager` and the reference matcher.** The matcher implements
the ADR-0035 grammar exactly — the exact digit run of the number, neighbors (if
any) outside `[0-9A-Za-z.]`, optional leading `#` — as a pure function. Its
table derives every row from that rule: match `#12`, `12`, `x#12`; reject `#1234`,
`#12x`, `x12`, `v1.12`, `2012`, `12.3`, and the same set inside branch names.
Verify: `go test -run TestIssueRef ./internal/tooling/task/`

**Step C5: Implement `IssueScope` orchestration + pure formatter.** PRs from the
cross-reference (labeled confirmed), branches from the matcher (labeled candidate),
worktrees holding a matched branch. Labeled plain text, stable sentinel when
nothing references the issue. Verify: golden-fixture formatter tests pass;
`VerifyNoRealCommands` on the orchestration tests.

**Step C6: Register `issue-scope`, measure the after-figure, record both.**
Verify: `go build ./...`; `go test ./internal/tooling/task/ ./cmd/`

**Measured 2026-08-26, `dg task issue-scope 3` against the same repository and
issue as the Step C2 baseline: 186 bytes, one call to `gh issue view`, one
paginated GraphQL cross-reference call, and two local git reads (`git branch`,
`git worktree list`) — no PR bodies fetched, no grep. Verified live: a real
local branch (`issue-3-test-branch`) is correctly reported as a `candidate`,
and an issue number with no cross-referencing PR still resolves in one round
of calls to the stable per-section "none" sentinel, not an error. Against the
~13,209-byte, three-round-trip baseline, this is a ~98% byte reduction and one
fewer round-trip class (no PR-body fetch at all) — comfortably justifying the
slice.**

#### Slice D — `pr-state` (ADR-0034 first)

**Step D1: Write ADR-0034.** Include the structured-gatherer mechanism, the
no-incidental-heavy-work rule, partial-failure behavior, and the bounded gh-call
count.

**Step D2: Extract the structured gatherers — one commit with their callers.**
Pull the raw-fetch-plus-reduce core out of `PRView`, `PRChecks`, `ReviewThreads`,
and `PRReviewState` into gatherers returning small structs; re-point the existing
commands to render from those structs so their output stays byte-identical (the
golden fixtures must pass unmodified). Verify: `go build ./...`; existing pr-* and
review-threads fixtures pass untouched.

**Step D3: Implement `PRState` from the gatherers.** Reduce to lifecycle, check
buckets, unresolved count, and my-review state — the checks gatherer stops at the
tally and makes zero `RunFailedJobLog` calls. Partial failure → an "unavailable"
field, not a failed command. Verify: the orchestration test asserts exactly which
gatherers ran and the total gh-call count.

**Step D4: Register `pr-state`; leave the four originals untouched.** Verify:
`go build ./...`; `go test ./internal/tooling/task/ ./cmd/` and a manual run
against a live PR whose every field agrees with the four originals.

#### Slice E — `dg wt create --base <ref>`

**Step E1: Give `CreateWorktreeAtBaseIn` the normal path's pre-flight.** Fetch
`origin` first (best-effort, bounded), reject when a local or remote branch of that
name already exists, and resolve/validate the base ref before any directory is
created. Verify: `go build ./...`; `go test ./internal/apps/git/`

**Step E2: Thread an optional base through `create`/`Create`/`CreateAt` and every
caller — one commit.** Dispatch to `CreateWorktreeAtBaseIn` when a base is set;
error (not a silent no-op) when the branch would be adopted. Update the TUI caller
(`internal/tui/worktree/model.go:442`) in the same commit so the tree builds.
Verify: `go build ./...`; `go test ./internal/tooling/worktree/ ./internal/tui/worktree/`

**Step E3:** Register `--base` on `worktreeCreateCmd`; document that omitting it
keeps the existing freshly-fetched-default behavior. Verify: `go build ./...`;
`go test ./cmd/`

#### Slice F — `review-scope --sizes`

**Step F1:** Extend the scope computation with range patch-bytes
(`len(git diff <base>)`, same two-dot semantics and exclusion filter as the file
table) and per-commit patch-bytes (`len(git diff <commit>^ <commit>)`). Fixtures
assert exact byte counts against a fixed diff. Verify: `go build ./...`;
`go test -run TestReviewScope ./internal/tooling/task/`

**Step F2:** Render them in `formatReviewScope` only when the flag is set, so the
default output's byte count is unchanged. Verify: existing golden fixtures still
pass untouched.

#### Slice G — `review-threads --json`

**Step G0:** Add `pageInfo { hasNextPage }` to the three capped nested connections
(`reviewThreadsQuery`'s per-thread `comments`, `prDiscussionQuery`'s `reviews` and
`comments`) so truncation is knowable at all — node count cannot distinguish exactly
100 from >100. The markdown path ignores the new field, so its fixtures stay
byte-identical. Verify: `go build ./...`; `review-threads` markdown fixtures pass
unmodified.

**Step G1:** Add the JSON path: parse and merge all `FetchReviewThreads` pages plus
`FetchPRDiscussion` into one object with `"schemaVersion": 1`, honor `--state`, and
set `"truncated"` from each connection's `hasNextPage` — never from
`len(nodes) == 100`. The markdown path and its sentinels stay byte-identical to
today. Verify: `go build ./...`; existing `review-threads` fixtures pass unmodified;
a golden JSON fixture parses and carries every id the markdown showed across more
than one page; a fixture with exactly 100 nested nodes reports `truncated: false`
and one with `hasNextPage: true` reports `truncated: true`.

**Step G2:** Register `--json`. Verify: `go build ./...`;
`go test ./internal/tooling/task/ ./cmd/`

#### Slice H — `commit-trailers`

**Step H1:** Implement `CommitTrailers` reading `--message-file` (fallback stdin),
parsing via `git interpret-trailers --parse --only-trailers` through the git app
wrapper (never a raw `exec.Command`), and printing the parsed trailers.
`--require <key>` (repeatable) exits non-zero when a key is absent from the parsed
trailers, case-insensitive, with an error naming the missing key and the likely
cause (no blank-line separator). No syntax-scanning "malformed" detector — it is
undecidable and produces false positives. Verify: `go build ./...`; table-driven
tests including the **valid prose-label** case (`Subject\n\nReason: needed\nMore
text.` with no `--require` → exit 0; the round-4 regression), a required trailer
glued to the body → non-zero, the same trailer blank-separated → zero, and a
required key genuinely absent → non-zero.

**Step H2:** Register the command; update `configs/shared/commands/smart-commit.md`
to write the message to a temp file, run
`commit-trailers --require <keys-it-generated> --message-file <file>` (no keys when
it generated no trailers), then `git commit -F <file>`. It hardcodes no key. Deploy
both agents afterward (`dg configure claude --force` **and**
`dg configure opencode --force`, CLAUDE.md §12). Verify: `go build ./...`;
`go test ./internal/tooling/task/ ./cmd/ .` — the root package is required here
because `configs/` changed.

---

## 6. Verification Plan

### Automated Verification

Each slice ships alone, so each runs **its own** targeted set — the touched package
plus its **actual** direct importers. These sets were generated by running the
CLAUDE.md §6 `go list` query against this tree, not hand-picked; regenerate before
running a slice if the tree has moved.

One slice trips the broad-blast-radius rule and must run the **full** `go test ./...`
instead of a subset:

- **Slice A touches `pkg/paths` (24 direct importers) and `internal/apps/baseapp`
  (22 direct importers).** Both are exactly the "blast radius is most of the tree"
  case CLAUDE.md §6 names — nearly every app package imports them. Even though
  Slice A only _adds_ a constant to `pkg/paths` and skips one prefix in `baseapp`,
  the honest call by the measured importer count is the full suite. Run
  `go test ./...` for Slice A and say so.

The remaining slices' measured sets (`+ .` marks the root package included because
`configs/` changed):

| Slice | Targeted packages (measured direct importers)                                                                                                                                                                                                              |
| ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B     | `./internal/tooling/task/ ./internal/tui/worktree/ ./cmd/`                                                                                                                                                                                                 |
| C     | `./internal/tooling/task/ ./internal/tooling/terminal/dev_tools/githubcli/ ./internal/tooling/terminal/ ./internal/tui/worktree/ ./cmd/`                                                                                                                   |
| D     | `./internal/tooling/task/ ./internal/tui/worktree/ ./cmd/`                                                                                                                                                                                                 |
| E     | `./internal/apps/git/ ./internal/apps/registry/ ./internal/tooling/reviewjournal/ ./internal/tooling/task/ ./internal/tooling/terminal/ ./internal/tooling/worktree/ ./internal/tui/worktree/ ./internal/tui/components/ ./internal/apps/opencode/ ./cmd/` |
| F     | `./internal/tooling/task/ ./internal/tui/worktree/ ./cmd/`                                                                                                                                                                                                 |
| G     | `./internal/tooling/task/ ./internal/tooling/terminal/dev_tools/githubcli/ ./internal/tooling/terminal/ ./internal/tui/worktree/ ./cmd/`                                                                                                                   |
| H     | `./internal/tooling/task/ ./internal/tui/worktree/ ./cmd/ .`                                                                                                                                                                                               |

Notes on the measured sets:

- `internal/tui/worktree` imports `internal/tooling/task`, so it appears in every
  task-touching slice (B, C, D, F, G, H) — it was missing from the previous
  hand-written table.
- `internal/tooling/terminal` imports `githubcli`, so C and G include it.
- Slice E's `git`/`worktree` fan-out is wide; the root package
  (`github.com/cjairm/devgeta`) is a direct importer of `internal/tooling/worktree`
  but is **excluded** here because Slice E changes no `configs/` or hook script —
  the root package's tests cover only those (CLAUDE.md §6), so they add ~4.8 min of
  pure cost for a worktree-signature change. It is included only on Slice H, which
  does edit `configs/`.

Every slice also runs `go build ./...` (the whole tree must build after any
signature change) and `make lint`. The full `go test ./...` is the release gate
(CLAUDE.md §9), run once when the cycle's slices land together — and, per the note
above, also the per-slice run for Slice A.

### Manual Verification

1. `dg task scratch --key demo` twice → same path both times; `dg task scratch`
   with no key → a fresh unique path each time; `dg task scratch --clean` accepts
   both prefixes and still refuses the root, a grandchild, and a path carrying
   neither allocation prefix. A keyed dir survives a `dg configure claude --force`
   (which runs `MaintainScratchDir`; `configure` takes exactly one app, so name
   one) regardless of age; an unkeyed dir older than 24h is still pruned.
2. `dg task scratch --key ../escape` and `--key a/b` → refused with an actionable
   message, nothing created. A symlink substituted at an existing keyed path →
   `--key` refuses to reuse it rather than writing through it.
3. `dg task refresh-branch --rebase` on a branch behind the default → linear
   history, no merge commit. Without the flag → unchanged behavior.
4. `dg task issue-scope <n>` on an issue with a live cross-referenced PR → reports
   it as confirmed; a branch whose name merely contains the number → reported as a
   candidate, labeled as such; on an issue whose number is a substring of another's
   → does not report the other; on an issue nothing references → prints the
   sentinel, exit 0.
5. `dg task pr-state` against a PR with failing checks and unresolved threads →
   one payload whose every field agrees with running the four originals, and it
   makes no `RunFailedJobLog` call (no failure-log fetch just to count).
6. `dg wt create tmp-base-check --base <a tag or release branch>` → worktree starts
   at that ref **and** the tmux window opens with the coder running.
7. `dg task review-scope` → byte-identical to before the cycle;
   `--sizes` → adds the size lines and nothing else.
8. `dg task review-threads` → byte-identical to before; `--json` → one document
   with `schemaVersion`, parses as JSON, carries every thread and comment id the
   markdown showed across all pages plus the discussion, honors `--state`, and sets
   `truncated` if a `first: 100` cap was hit.
9. `commit-trailers --require Co-authored-by` on a message with `Co-Authored-By`
   glued to the body (no blank line) → non-zero (git parses no trailer, so the key
   is absent); with the blank line → exit 0. `--require Signed-off-by` on a message
   lacking it → non-zero. A message with a prose `Reason:` label followed by more
   prose, **no `--require`** → exit 0 (not falsely rejected).

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

- [x] Domain context clear? (Is it obvious why "no issue surface" is one gap and not six?)
- [x] Engineer context sufficient? (Are the two generality traps in §2 concrete enough to avoid?)
- [x] Objective unambiguous?
- [x] Scope is actually locked? (Are the four dropped asks dropped for stated reasons?)
- [x] Steps are actionable? (Each 5-15 min, with clear success criteria?)
- [x] Verification is executable?
- [x] Risks are realistic? (Is the branch-naming risk rated high enough?)

**Reviewer notes:**

Cross-model review (2026-08-25) raised 11 findings; all verified accurate against
the codebase and all addressed in this revision:

- **Trailer detection + input (blocker):** the command reads an explicit
  `--message-file` and the commit flow commits the validated bytes with
  `git commit -F`. (The original "detect a malformed block" design here was later
  replaced by `--require`-scoped enforcement in round 4 — see below — because
  syntax alone cannot decide it.) Slice H.
- **`pr-state` composition (blocker):** now composes **structured gatherers**, not
  formatted output; bounded gh-call count; no incidental failure-log fetch;
  partial-failure sentinel. Slice D / ADR-0034.
- **Issue association (blocker):** ADR-0035 now fixes accepted sources — PR↔issue
  via cross-referenced timeline events (same-repo, paginated), branch↔issue as a
  candidate (exact grammar set in round 2, see below), text grep excluded. Slice C.
- **`review-threads --json` (blocker):** versioned schema, all pages + discussion
  merged, `--state` preserved, explicit `truncated` metadata. Slice G.
- **`wt create --base` (blocker):** `CreateWorktreeAtBaseIn` gains the normal
  path's fetch/collision/base-validation pre-flight; signature change lands with
  every caller (incl. the TUI) in one commit. Slice E.
- **Keyed scratch symlink-safety + expiry (blockers):** distinct `ScratchKeyPrefix`
  exempt from age-pruning, symlink-safe idempotent reuse. Slice A / ADR-0033.
- **Diff-byte semantics (important):** defined as unified-patch bytes, two-dot,
  same exclusion filter, with binary/untracked/merge handling and fixture-pinned
  counts. Slice F.
- **Independently committable (important):** committability rule added — signature
  changes land with all callers and tests; every step verifies `go build ./...`.
- **Workflow order (minor):** reworded to implement → verify → test, keeping the
  adversarial tables as verification of the pure boundaries.
- **Per-slice verification (minor):** §6 now lists a targeted set per slice; root
  package only on Slice H.

Second cross-model round (2026-08-25) raised three important and one minor; all
verified accurate and addressed:

- **`pr-state` call budget (n10, important):** confirmed `resolveOwnerRepoPR` always
  calls `CurrentRepo` and `CurrentPRNumber` when `--pr` is absent (`pr.go:52-74`).
  Fix: resolve once and pass identifiers into the gatherers; skip
  `FetchPRDiscussion`; the budget is now a table asserting 5 calls with `--pr` and 6
  inferred. Slice D.
- **Branch matcher grammar (n12, important):** confirmed a regex `\b` matches `12`
  in `v1.12`, contradicting the stated example. Fix: replaced "word boundary" with
  an exact rule — the digit run's neighbors must be outside `[0-9A-Za-z.]`, optional
  leading `#` — and derived every example from it, in ADR-0035, Slice C, and Step
  C4. Slice C.
- **Per-slice importer sets (n11, important):** confirmed the hand-written table was
  wrong (missing `internal/tui/worktree` on task slices, `internal/tooling/terminal`
  on githubcli slices, and E's full fan-out). Fix: regenerated every set from the
  §6 `go list` query; Slice A moved to the **full** `go test ./...` because
  `pkg/paths` (24 importers) and `baseapp` (22) hit the broad-blast-radius rule.
- **`dg configure --force` (minor):** confirmed `configure` is `ExactArgs(1)`;
  corrected the manual step to name an app.

Third cross-model round (2026-08-25) raised two important and two minor; all
verified (git behavior tested empirically) and addressed:

- **Trailer detector grammar (n13, important):** ran `git interpret-trailers
--parse` on each form (git 2.51.1) and confirmed git accepts `Key: value`,
  `Key:value`, and `Key : value` — the old `^[A-Za-z0-9-]+: ` regex required a
  trailing space and would pass a malformed glued `Key:value`. Fix: detector is
  `^[A-Za-z0-9-]+[ \t]*<sep>` with `<sep>` from the effective `trailer.separators`
  (default `:`), value optional. (Round 4 then removed syntax-based detection
  entirely — see below — so this grammar no longer ships; the separator research it
  produced still informed the decision.) Slice H.
- **JSON truncation signal (n14, important):** confirmed the three capped
  connections request only `nodes` (`githubcli.go:192-203,236-241`), so
  `len(nodes) == 100` cannot tell 100 from >100. Fix: new Step G0 adds
  `pageInfo { hasNextPage }` to each capped connection; `"truncated"` derives from
  `hasNextPage`, and the test asserts exactly-100 vs 101+. Slice G.
- **Minor (§6 count):** "Two slices" → "One slice" (only Slice A trips the rule).
- **Minor (stale note):** the round-1 summary bullet no longer says
  "word-boundary"; it now points at round 2's exact grammar.

Fourth cross-model round (2026-08-25) raised one important; verified against git and
addressed:

- **Trailer `--check` false-positives on prose (n15, important):** confirmed
  against git 2.51.1 that for `Subject\n\nReason: needed\nMore text.` git parses
  **zero** trailers, so the round-3 "trailer-looking lines vs parsed" detector would
  reject a valid commit message — and it ships in `smart-commit.md`, so the false
  positive would be shipped behavior. Root cause: syntax alone cannot decide whether
  a label was intended as a trailer. Fix: **removed the syntax detector entirely.**
  Enforcement is now `--require <key>`, which fails only when a **declared** key is
  absent from git's parsed trailers — this catches a glued/misformatted required
  trailer (git parses it as absent, confirmed) with **no** false positive on prose
  the caller did not name. `smart-commit.md` requires only the keys it generated for
  that commit and hardcodes none. Slice H.

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
