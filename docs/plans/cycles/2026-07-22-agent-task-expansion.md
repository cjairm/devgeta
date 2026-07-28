# Cycle: Agent Task Expansion — review-package, worktree lifecycle, release, and the redirect hook

**Date:** 2026-07-22
**Estimated Duration:** ~14 hours (6 slices, independently shippable)
**Status:** Done

---

## 1. Domain Context

`dg task` subcommands are AI-first composite commands: each collapses a multi-step
git/gh pipeline into one call with compact, token-cheap output, per
[docs/guides/task-design.md](../../guides/task-design.md). The existing family
(review-scope, branch-diff, review-threads, submit-review, and the PR suite) covers
the PR-review flow well. A friction audit of `configs/shared/` (2026-07-22) found
three workflows that agents still hand-roll as raw multi-command sequences, plus two
gaps in existing tasks, and no mechanism that steers an agent to a task when it
reaches for raw git instead.

Evidence (from the audit):

- The `BASE..HEAD` review-range dance (`rev-parse` verify ×2 → `log --oneline` →
  `diff --stat` → `diff -U10` → `rev-list --count`) is hand-rolled in **4 skill
  files plus a bash script** (`configs/shared/skills/subagent-driven-development/scripts/review-package`).
  The raw dance also lacks the lockfile-noise filtering `branch-diff` enforces.
- The worktree lifecycle (`rev-parse --git-dir`/`--git-common-dir` isolation probe,
  `worktree add`/`remove`/`prune`, ff-merge + branch cleanup) repeats across
  2 skill files (~5 occurrences of the probe alone) and was executed by hand 3×
  in a single working session.
- The CLAUDE.md §9 squash-before-tag release flow is prescribed prose an agent must
  reassemble every release — pure policy with no wrapper.
- `review-scope` returns commit subjects without dates (blocking the
  `/review-pr` "code changed since that reply" rule) and without bodies (forcing
  `create-pr.md` into a raw `git log --format` follow-up).
- `pr-checks` prints a link for failing checks, forcing a second fetch to see why
  CI failed.

External validation: Anthropic's tool-writing guidance (consolidate multi-step
workflows into one tool; concise-by-default output), GitHub's agentic-workflow token
audit ("the cheapest LLM call is the one you don't make"; pre-digest fetches outside
the reasoning loop), and SWE-agent's ACI research (explicit sentinels; minimal-context
views) all independently confirm the task-design doctrine and these gaps.

**Constraint discovered during planning:** most files under `configs/shared/skills/`
are synced from upstream superpowers (see commit `8ff444e`). They must NOT be edited
to reference devgeta tasks — edits would conflict on the next sync. Direct reference
updates are limited to devgeta-owned files (`configs/shared/commands/*.md`,
`configs/shared/agents/*.md`, `CLAUDE.md`). For synced skills, the redirect hook
(Slice F) is the only steering mechanism.

Related docs: [task-design.md](../../guides/task-design.md),
[2026-07-14-token-aware-git-tasks.md](2026-07-14-token-aware-git-tasks.md) (output
principles), [2026-06-18-dg-task-command.md](2026-06-18-dg-task-command.md) (PR task
suite), CLAUDE.md §9 (release workflow).

---

## 2. Engineer Context

- **Relevant files:**
  - `internal/tooling/task/task.go` — `TaskManager` (git-flavored tasks); home for worktree/release tasks
  - `internal/tooling/task/scope.go` — reusable helpers: `mergeBase`, `aheadBehind`, `commitSubjects`, `fileChanges(rangeSpec)`, `partitionExcluded`, `formatReviewScope`
  - `internal/tooling/task/branchdiff.go` — `BranchDiffAt`, `formatBranchDiff`, `FileStat`
  - `internal/tooling/task/exclusions.go` — noise-filter patterns every diff task must reuse
  - `internal/tooling/task/pr.go` — `PRManager` orchestrate-then-format pattern to mirror; `resolveOwnerRepoPR`
  - `cmd/task.go`, `cmd/task_pr.go` — cobra registration
  - `internal/tooling/worktree/` — existing worktree conventions (dg wt) that Slice C must align with
  - `configs/claude/` (or wherever the existing golangci-lint hook ships from) — deployment path precedent for Slice F
- **Architecture rule (task-design.md):** manager method orchestrates raw fetches;
  a pure formatter renders. Git plumbing → pure Go formatter; gh JSON → jq filter.
- **Output rules (task-design.md):** labeled plain text; payload only; mutations
  confirm with one line (verb + target); stable sentinels for empty results; lossy
  only with a receipt + escape hatch.
- **Testing:** [testing-patterns.md](../../guides/testing-patterns.md) —
  `testutil.MockApp`, `VerifyNoRealCommands`, golden-fixture tests for formatters,
  no real commands ever.
- **Measure first (task-design.md):** before building each slice, capture the raw
  output bytes an agent ingests today for a representative case; after, capture the
  task's output. Record both in this doc's checklist. If the delta plus saved
  round-trips isn't clearly worth the Go file, stop and re-scope.
- **Tests:** `go test ./internal/tooling/task/ ./cmd/` · `go test ./...` · `make lint`

---

## 3. Objective

Agents complete the review-range, worktree-lifecycle, and release workflows in one
`dg task` call each (with `review-scope`/`pr-checks` gaps closed), and a PreToolUse
redirect hook steers agents from known raw-git sequences to the task equivalents —
so the task family is used by default, not by memory.

---

## 4. Scope Boundary

### In Scope

- [x] **Slice A — `dg task review-package <base> <head>`:** one call returning
      range verification, commit list (SHA + date + subject), noise-filtered stat
      table with exclusion receipts, and the full `-U10` diff of included files;
      `--file <path>` escape hatch mirroring `branch-diff`. Update
      `configs/shared/agents/code-reviewer.md` (scope section) to name it for
      arbitrary-range reviews.
      Measured on `b0e98fd..main` (10 commits, this repo): raw 6-call dance
      (`rev-parse --verify` x2, `log --oneline`, `diff --stat`, `diff -U10`,
      `rev-list --count`) = 793,426 bytes; one-call `review-package` on the same
      range = 792,704 bytes. The byte delta is small (same default lockfile
      exclusions `review-scope`/`branch-diff` already apply, and the full diff is
      still printed in full) — the win here is collapsing 6 round-trips into 1,
      per the "collapse round-trips" justification in task-design.md, not raw
      compression.
- [x] **Slice B — `review-scope` gaps:** commit lines gain ISO dates
      (`<sha> <date> <subject>`); new `--bodies` flag appends commit bodies.
      Update `configs/shared/commands/create-pr.md` to drop its raw
      `git log --format` follow-up.
- [x] **Slice C — `dg task worktree-start <name> [--base <ref>]` and
      `dg task worktree-finish [<name>] --merge|--discard`:** start = clean-tree check,
      fetch, create worktree + branch off base, one-line confirmation with path;
      finish = rebase-onto-base if diverged, ff-only merge, worktree remove, branch
      delete (`--merge`), or safe teardown (`--discard`, refuses on dirty tree
      without explicit confirmation). Target selection is deterministic: an explicit
      `<name>` wins; otherwise cwd-inside-a-linked-worktree resolves to that worktree;
      otherwise the command errors and lists the available worktrees — it never
      guesses from a main checkout. Must align with `internal/tooling/worktree/`
      conventions — resolve the path convention (`.worktrees/` vs sibling dir)
      during design, matching what `dg wt` already does.
- [x] **Slice D — `pr-checks` failure digest:** failing checks include the failing
      job name plus a trimmed log tail of the failure region (deduped repeated
      lines, bounded length, truncation receipt); passing checks stay one line.
- [x] **Slice E — `dg task release <vX.Y.Z> --message-file <f> [--push]`:** the
      CLAUDE.md §9 flow — verify clean tree + on default branch, count unpushed
      commits, squash 2+ into one commit using the message file, create the
      annotated tag with the same message, and push commit+tags only when `--push`
      is passed. Update CLAUDE.md §9 to route through the task.
- [x] **Slice F — redirect hook:** a PreToolUse Bash hook (Claude Code) + OpenCode
      plugin equivalent that intercepts narrow raw-git patterns with task
      equivalents and denies with a one-line reason naming the exact replacement
      command. Ships from the **app-specific config roots**, NOT `configs/shared/`:
      the shared sync surface is only `skills/commands/agents`
      (`internal/apps/baseapp/configure.go` — `SharedConfigParts`), so the hook
      follows the `format.sh` precedent — script in `configs/claude/`, added to
      claude.go's script copy loop, registered in the copied `settings.json`; the
      OpenCode plugin needs a new copy step added to opencode.go's configure.
      Ships LAST (depends on Slices A/C/E existing).
- [x] Each slice updates `docs/spec.md` and the consuming devgeta-owned configs in
      the same PR (sentinel-contract rule, task-design.md §4).

### Explicitly Out of Scope

- Editing upstream-synced skills under `configs/shared/skills/` (sync conflicts;
  the hook covers them at runtime). This includes the `scripts/review-package`
  bash script — it is upstream's; ours coexists.
- `test-summary` (test-runner compression is rtk's lane per task-design.md),
  `rebase-check`/range-diff, `stack-status`, a `smart-commit` wrapper, any
  status/orientation micro-task (negligible-savings rule).
- The `receiving-code-review` skill's raw `gh api` reply pattern (upstream-synced;
  explicitly left alone).
- rtk installation (tracked separately in ROADMAP under ai-tools).
- Hook auto-rewrite of commands (deny-with-guidance only; silent rewriting
  conflicts with the security review non-negotiables).

**Scope is locked.**

---

## 5. Implementation Plan

Slices are independent except F (needs A, C, E). Recommended order: A → B → C → D → E → F.
Each slice: measure → implement → verify manually → tests → update consuming configs +
spec.md → commit (feature workflow, CLAUDE.md §6).

**Hard rule for all config updates:** every command reference written into
`configs/shared/commands/*.md` or `configs/shared/agents/*.md` uses
`devgeta task …` — never `dg` — per the binary-invocation contract those files
already state (`review-pr.md` Notes, `code-reviewer.md` Scope): only the installed
`devgeta` binary is guaranteed on PATH where agents run. `dg task` in this doc's
prose is the feature's colloquial name, not the invocation.

### Slice A — `review-package` (~3h)

| Action | File                                          | Description                   |
| ------ | --------------------------------------------- | ----------------------------- |
| Create | `internal/tooling/task/reviewpackage.go`      | Orchestrator + pure formatter |
| Create | `internal/tooling/task/reviewpackage_test.go` | Golden-fixture + mock tests   |
| Modify | `cmd/task.go`                                 | Register `review-package`     |
| Modify | `configs/shared/agents/code-reviewer.md`      | Name it in the Scope section  |
| Modify | `docs/spec.md`                                | Document                      |

Steps:

1. Measure: capture bytes of the raw 5-command dance on a representative 10+-commit range.
2. Orchestrator: verify both refs (`rev-parse --verify`), reject unknown refs with an
   actionable error; gather `log --format=%h %as %s base..head`, `fileChanges(rangeSpec)`
   (reuse), partition exclusions (reuse), then `diff -U10` for included paths only.
3. Formatter (pure Go): `range:`/`commits:`/stat table/`excluded: … (+A/-D)` receipts,
   then ` ```diff ` fenced payload. Sentinels: `No commits in range.` / `No file changes in range.`
4. `--file <path>` flag: full diff for one file (escape hatch, mirrors branch-diff).
5. Tests: formatter golden fixtures; orchestration asserts commands + error paths.
6. Manual verify on this repo's history; record before/after bytes above; update configs + spec.

### Slice B — `review-scope` dates + `--bodies` (~1.5h)

| Action | File                                   | Description                                                 |
| ------ | -------------------------------------- | ----------------------------------------------------------- |
| Modify | `internal/tooling/task/scope.go`       | `commitSubjects` → `%h %as %s`; `--bodies` adds `%b` blocks |
| Modify | `internal/tooling/task/scope_test.go`  | Update fixtures                                             |
| Modify | `cmd/task.go`                          | `--bodies` flag                                             |
| Modify | `configs/shared/commands/create-pr.md` | Use `review-scope --bodies`; drop raw `git log --format`    |
| Modify | `configs/shared/commands/review-pr.md` | Note dates are available for the "since that reply" check   |
| Modify | `docs/spec.md`                         | Document                                                    |

Note: the commit-line format is a sentinel-adjacent contract — audit
`configs/shared/` for consumers that parse it before changing (task-design.md §4).

### Slice C — worktree lifecycle (~3.5h)

| Action | File                                     | Description                                        |
| ------ | ---------------------------------------- | -------------------------------------------------- |
| Create | `internal/tooling/task/worktree.go`      | `WorktreeStart`, `WorktreeFinish` on `TaskManager` |
| Create | `internal/tooling/task/worktree_test.go` | All mocked                                         |
| Modify | `cmd/task.go`                            | Register both                                      |
| Modify | `docs/spec.md`                           | Document                                           |

Steps:

1. Design decision first: path + naming convention — must match
   `internal/tooling/worktree/` (`dg wt`) so both features see the same worktrees.
   Record the decision here before coding.
2. `worktree-start <name> [--base <ref>]`: refuse on dirty tree (actionable error);
   `fetch origin`; `worktree add -b <name> <path> <base|origin/default>`. Output:
   `Created worktree <path> (branch <name>, base <ref>)`.
3. `worktree-finish [<name>] --merge|--discard`: resolve the target worktree —
   explicit `<name>` wins; else cwd inside a linked worktree resolves to that one;
   else error listing available worktrees (never guess from a main checkout);
   `--merge` = rebase onto default branch if diverged → verify
   `go build ./...` is NOT run (task stays generic; verification is the caller's
   job) → `merge --ff-only` from the main checkout → `worktree remove` →
   `branch -D` (safe: only after the ff-merge made it fully merged). `--discard` =
   refuse on dirty tree unless `--force`; remove + delete. One-line confirmations;
   failures leave state intact and say what to do next.
4. Flag interplay: exactly one of `--merge`/`--discard` required; clear usage error otherwise.

**Design decision:** `worktree-start`/`worktree-finish` reuse `dg wt`'s exact base path —
`worktree.GetWorktreeBasePath()` (`~/.local/share/devgeta/worktrees/<repo-slug>/<flat-name>`) —
rather than `.worktrees/` in-repo or a sibling directory. `internal/tooling/task` imports
`internal/tooling/worktree` for the exported `GetWorktreeBasePath()`; `flattenName`
(`strings.ReplaceAll(name, "/", "-")`) is a one-line duplicate in the task package rather
than an export from `worktree` for a single caller — exporting a helper for one external
call site is a bigger footprint change than this slice calls for. This makes `dg wt list`
and `worktree-start`-created worktrees the same population: no parallel tracking, no
naming drift. Verified manually (see report) that a worktree created via `worktree-start`
shows up in `dg wt list` and vice versa.

### Slice D — `pr-checks` failure digest (~2.5h)

| Action | File                                                         | Description                                                          |
| ------ | ------------------------------------------------------------ | -------------------------------------------------------------------- |
| Modify | `internal/tooling/terminal/dev_tools/githubcli/githubcli.go` | Fetch failing job log (`gh run view --log-failed` or API equivalent) |
| Modify | `internal/tooling/task/pr.go`                                | `PRChecks` enrichment: digest failing checks                         |
| Modify | `internal/tooling/terminal/dev_tools/jq/jq.go`               | Formatter changes if JSON-shaped                                     |
| Modify | tests alongside                                              | Fixtures for pass/fail/pending mixes                                 |
| Modify | `docs/spec.md`                                               | Document                                                             |

Rules: passing/pending checks stay one line each (current format preserved —
sentinel contract). Failing checks append job name + last N deduplicated log lines
with a truncation receipt (`… 214 earlier lines omitted`). Bound total digest size.
Measure a real failing-run log before picking N.

Check→log mapping (the current fetch is `gh pr checks --json name,state,link,workflow`
— no run/job IDs): parse the failing check's Actions `link` URL
(`…/actions/runs/<run-id>/job/<job-id>`) and fetch via
`gh run view --job <job-id> --log-failed`. When the link doesn't match the Actions
URL shape (external checks/commit statuses), fall back to today's one-line + link
with a `log unavailable: external check` note — never guess a mapping.

### Slice E — `release` (~2h)

| Action | File                                    | Description                                       |
| ------ | --------------------------------------- | ------------------------------------------------- |
| Create | `internal/tooling/task/release.go`      | Orchestrator                                      |
| Create | `internal/tooling/task/release_test.go` | All mocked; every guard tested                    |
| Modify | `cmd/task.go`                           | Register with `<version> --message-file [--push]` |
| Modify | `CLAUDE.md` §9                          | Route the workflow through the task               |
| Modify | `docs/spec.md`                          | Document                                          |

Guards (each refuses with an actionable one-liner): version must match `^v\d+\.\d+\.\d+$`
— strict semver only, no prerelease forms, deliberately mirroring CLAUDE.md §9's
`vMAJOR.MINOR.PATCH` policy; tree clean; on default branch; message file exists and
non-empty; tag must not already exist. Flow: count `origin/<default>..HEAD`; if ≥2,
`reset --soft` + commit with the message file; `tag -a <version> -F <file>`; only
with `--push`: `push origin <default> --tags`.
Without `--push`, final line states exactly what remains:
`Tagged v0.12.0 (squashed 3 commits). Not pushed — run: git push origin main --tags`.

### Slice F — redirect hook (~2h, after A/C/E)

| Action | File                                       | Description                                                             |
| ------ | ------------------------------------------ | ----------------------------------------------------------------------- |
| Create | `configs/claude/task-redirect.sh`          | PreToolUse Bash-matcher script (sibling of `format.sh`)                 |
| Modify | `internal/apps/claude/claude.go`           | Add script to the ForceConfigure copy+chmod loop (format.sh precedent)  |
| Modify | `configs/claude/settings.json`             | Register the PreToolUse hook (this file is already copied on configure) |
| Create | `configs/opencode/plugin/task-redirect.js` | `tool.execute.before` intercept                                         |
| Modify | `internal/apps/opencode/opencode.go`       | Add a plugin-dir copy step to configure (none exists today)             |
| Modify | `internal/apps/*/..._test.go`              | Assert the new files deploy on ForceConfigure                           |
| Modify | `docs/apps/claude.md`                      | Document behavior + bypass                                              |
| Modify | `docs/spec.md`                             | Document                                                                |

Rules:

1. **Narrow patterns only**, each mapped to its replacement:
   `git diff <ref>..<ref>` (+ the stat/log range combo) → `review-package`;
   `git worktree add` → `worktree-start`; `git worktree remove` → `worktree-finish`;
   `git reset --soft HEAD~N` and `git tag -a v*` → `release`.
   Never match bare `git diff`, `git log`, or other legitimate single commands.
2. Deny with a one-line reason naming the exact replacement and why (one call,
   noise-filtered / policy-enforcing). Never rewrite silently.
3. Escape hatch: the deny message states how to proceed with raw git when genuinely
   needed (documented bypass), so no flow dead-ends.
4. Test the hook script itself (it's shell/jq: table-driven cases of command → allow/deny).
5. Per CLAUDE.md config rule: enforce the hook's pattern table with a test against
   the embedded configs FS if any constraint is imposed on it.

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/tooling/task/ ./internal/tooling/terminal/dev_tools/... ./cmd/
go test ./...
make lint
```

### Manual (per slice, on this repo)

1. A: `devgeta task review-package <old-sha> HEAD` → verify sections, exclusion
   receipts, and that a lockfile-touching range excludes it with a receipt.
2. B: `devgeta task review-scope --bodies` on a feature branch → dates + bodies render.
3. C: full cycle — `worktree-start test-x` → commit inside → `worktree-finish --merge`
   → change on main, worktree gone, branch gone; repeat with `--discard`.
4. D: `devgeta task pr-checks` on a PR with a failing check → digest appears, bounded.
5. E: in a throwaway temp repo (`git init`, default branch, 3 commits, a fake
   `origin` remote): `release v0.0.1 --message-file m.txt` (no `--push`) → squash +
   tag correct, nothing pushed; delete the temp repo. (A scratch _branch_ can't be
   used — the on-default-branch guard would refuse, and `-test` suffixes fail the
   semver guard by design.)
6. F: in a live agent session, attempt `git worktree add …` → denied with the
   replacement named → agent adapts. Verify a plain `git diff` is NOT intercepted.

### Regression

- Existing task sentinels unchanged (`review-scope`, `branch-diff`, `pr-checks`
  one-line format for passing checks, all PR tasks).
- `dg wt` unaffected by Slice C (shared conventions, no interference).
- `dg install` / `dg configure` still work after hook config additions.

---

## 7. Risks & Trade-offs

| Risk                                                         | Likelihood       | Mitigation                                                                          |
| ------------------------------------------------------------ | ---------------- | ----------------------------------------------------------------------------------- |
| Hook false-positives block legitimate git and erode trust    | Med              | Narrow anchored patterns; table-driven hook tests; documented bypass; ship last     |
| Slice C conflicts with `dg wt` worktree conventions          | Med              | Design decision recorded before coding; reuse `internal/tooling/worktree` helpers   |
| `release` is hard to reverse (squash + tag + push)           | Med              | `--push` opt-in; every guard refuses early; tag-exists check; tested guards         |
| Changing `review-scope` commit-line format breaks a consumer | Low              | Audit `configs/shared/` consumers first; same-commit config updates (sentinel rule) |
| `pr-checks` digest balloons on huge CI logs                  | Med              | Hard length bound + dedup + truncation receipt; measure real logs before picking N  |
| Upstream skill sync reintroduces raw patterns                | High (by design) | Expected: the hook is the durable answer for synced files; do not edit them         |

### Trade-offs Made

- **Deny-with-guidance vs auto-rewrite (hook):** deny keeps the agent aware of what
  runs and respects the security non-negotiables; costs one retry turn on each miss.
- **`worktree-finish` does not run build/tests:** verification stays the caller's
  responsibility (tasks are generic; policy is git-state safety, not project builds).
- **`release --push` opt-in:** one more step on the happy path, in exchange for the
  irreversible action never happening implicitly.
- **Synced skills untouched:** upstream raw patterns persist in text; runtime hook
  redirects them. Slower first-turn for those flows, zero sync conflicts.

---

## 8. Cross-Model Review Notes

- [x] Domain context clear?
- [x] Engineer context sufficient?
- [x] Objective unambiguous?
- [x] Scope actually locked?
- [x] Steps actionable (5–15 min each)?
- [x] Verification executable?
- [x] Risks realistic?

**Reviewer notes:**

Reviewed 2026-07-22 (document-reviewer). All findings resolved in this revision:

- Slice F deployment path was underspecified vs the real config-sync surface
  (`SharedConfigParts` = skills/commands/agents only) → respecified on the
  `format.sh` precedent with explicit file-level tasks, including the missing
  OpenCode plugin copy step and deploy-assertion tests.
- Slice E verification used `v0.0.1-test` (fails the semver guard) on a scratch
  branch (fails the default-branch guard) → now a throwaway temp repo with `v0.0.1`;
  strict-semver-only stance recorded (mirrors CLAUDE.md §9).
- Binary-invocation contract (`devgeta`, never `dg`, in shared configs) → hard rule
  added to §5.
- `worktree-finish` selector ambiguity → deterministic contract fixed:
  `[<name>] --merge|--discard`, explicit name > cwd resolution > error with list.
- `pr-checks` check→log mapping unspecified → Actions-link URL parse to
  `gh run view --job <id> --log-failed`, with an explicit external-check fallback.
