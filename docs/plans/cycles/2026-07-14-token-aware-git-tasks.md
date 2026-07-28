# Cycle: token-aware git tasks — `dg task review-scope` + `dg task branch-diff`

**Date:** 2026-07-14
**Estimated Duration:** ~4–5 hours
**Status:** Done

---

## 1. Domain Context

The 2026-06-18 `dg task` cycle gave agents compact, LLM-oriented output for **GitHub PR
operations**: `githubcli` fetches raw JSON, `jq` filters render terse markdown
(`FormatReviewThreads`, `FormatPRView`, `FormatPRChecks`), and the AI-coder commands
consume them. The **git side got no equivalent** — agents still compose raw git and
ingest raw output.

**Problem (measured on this repo, 2026-07-14):**

| Output                                                 | Size   | ~Tokens |
| ------------------------------------------------------ | ------ | ------- |
| Full branch diff (commit `131cd30` as proxy, 25 files) | 192 KB | ~50,000 |
| Same change, `--stat` only                             | 3.3 KB | ~950    |
| `git log --format="%s%n%b%n---"` for the range         | 1.8 KB | ~500    |

Three concrete token sinks in the shared AI-coder configs:

1. **The orientation dance.** `code-reviewer.md` runs up to 6 tool calls before reviewing
   anything: `git fetch`, `git symbolic-ref` (default-branch detection), up to three
   `git rev-parse` fallback probes (main/master/develop), then the diff. Each bash
   round-trip costs ~100–200 tokens of framing, plus the prompt prose that teaches the
   fallback logic. `create-pr.md` independently runs its own 3-call variant — and
   **hardcodes `main`** (`git log main..HEAD`, `git diff main...`), which breaks on
   master/develop repos.
2. **Diff noise.** Lock files (`package-lock.json`, `yarn.lock`, `go.sum`, …) and
   generated files are unreviewable but ride along in every full diff. Minor in Go
   repos; in npm repos a lockfile diff alone is routinely 10–50k tokens, often larger
   than the rest of the PR. Git natively supports `':(exclude)…'` pathspecs.
3. **No per-file retrieval.** Agents either ingest the whole branch diff or nothing;
   the token-efficient stat-first-then-file-by-file review pattern requires composing
   raw git today.

Related: `Git.DefaultBranch()` (`internal/apps/git/git.go:256`) blindly falls back to
`"main"` when `origin/HEAD` is unset — the same blind spot, and it also affects
worktree creation, which calls it.

Related docs: [CLAUDE.md](../../../CLAUDE.md) §6/§12,
[2026-06-18-dg-task-command.md](2026-06-18-dg-task-command.md),
[docs/guides/cli-patterns.md](../../guides/cli-patterns.md),
[docs/guides/testing-patterns.md](../../guides/testing-patterns.md).

---

## 2. Engineer Context

**Relevant files and their purposes:**

- `internal/tooling/task/task.go` — `TaskManager{Git, Base, Fzf}`; git-flavored tasks live here
- `internal/tooling/task/pr.go` — `PRManager`; the fetch-raw → render-compact pattern to mirror
- `internal/apps/git/git.go` — primitives: `DefaultBranch()` (256, needs the probe fix),
  `FetchOrigin()` (170), `RemoteBranchExists()` (240), `ExecuteCommand`; `Base.ExecCommand`
  captures stdout when output must be parsed rather than streamed
- `cmd/task.go` / `cmd/task_pr.go` — cobra registration pattern for task subcommands
- `configs/shared/agents/code-reviewer.md` — Scope section = the orientation dance; has **no**
  `devgeta task *` permission today
- `configs/shared/commands/create-pr.md` — step 1 hardcodes `main` (lines 26–31)
- `configs/shared/commands/review-pr.md` — step 2's local-branch fallback repeats the fetch/default-branch prose

**Key git plumbing the new tasks wrap:**

- `git merge-base origin/<default> HEAD` — the comparison base
- `git rev-list --left-right --count origin/<default>...HEAD` — ahead/behind
- `git diff --numstat <base>...HEAD` — machine-readable per-file +/- for the table and exclusion notes
- `git diff <base>...HEAD -- . ':(exclude)<pattern>'` — the filtered payload

**Testing patterns used in this area:** `testutil.MockApp` with queued outputs,
`testutil.VerifyNoRealCommands(t, mockApp.Base)`, `func init() { testutil.InitLogger() }`;
see `internal/tooling/task/pr_test.go` for the closest precedent.

---

## 3. Objective

Give agents one-call, compact, noise-filtered git context — the `ReviewThreads` pattern
applied to git — and rewire the shared AI-coder configs to use it, so every review /
create-pr invocation spends tokens on the change under review instead of on orientation
round-trips and lockfile noise.

---

## 4. Scope Boundary

**In scope:**

- `Git.DefaultBranch()` fix: when `origin/HEAD` is unset, probe `origin/main` →
  `origin/master` → `origin/develop` (via `RemoteBranchExists`) before defaulting to `main`
- `dg task review-scope` — fetch + orient in one call (see step 2 for output)
- `dg task branch-diff [--file <path>]` — merge-base diff with default noise exclusions,
  explicit one-line notes for everything excluded, per-file retrieval
- Config rewires: `code-reviewer.md`, `create-pr.md`, `review-pr.md`
- Docs: `docs/spec.md` task list, README task table

**Out of scope (explicitly deferred):**

- `--pr` mode on `branch-diff` (wrapping `gh pr diff` with the same filtering) — the
  current flows review locally checked-out branches; add when a real need appears
- Staged-diff exclusions for `smart-commit.md` — it reviews `git diff --cached` on a
  haiku model; lockfile noise there is real but rarer; revisit if it shows up in practice
- Configurable exclusion list (global_config / flag) — hardcode sane defaults first;
  making it configurable is a follow-up once the defaults prove out
- `git status` / `git log` wrappers — measured savings are negligible (hundreds of bytes)
- `document-reviewer.md` — reviews documents, not branch diffs; nothing to rewire

---

## 5. Implementation Plan

> Follow CLAUDE.md §6: implement → verify manually → test → commit. Mocks only in tests.

### Output design principles (apply to every task in this cycle)

Task output is read by an LLM first, a human (via `dge`) second. Decided 2026-07-14:

1. **Labeled plain text, not markdown scaffolding.** Line-oriented `key: value` labels,
   `- ` lists, aligned stat lines. No headers, tables, bold, or emoji — that's rendering
   decoration an LLM pays for without needing. Markdown syntax is used only where
   structure earns its tokens (e.g. ` ```diff ` fences), per the `ReviewThreads` precedent.
2. **Payload only.** Never wrap output in prose ("Here is the scope:", "Done! ✓").
   The first byte of output is data.
3. **Mutations confirm with one line: verb + target** — e.g. `Resolved thread PRRT_abc`.
   Never bare `ok` (the echoed target lets an agent verify it acted on the right object
   and reuse the id without re-fetching) and never more than one line. Success/failure
   for scripts lives in the exit code, not the text.
4. **Stable sentinels.** Fixed phrases agents match verbatim (`No unresolved review
threads.`, `On <default> — no branch to compare…`, `(fetch failed — comparing against
local refs)`) are contracts — changing them is a breaking change to the shared configs.

### Architecture — fetch/format separation (mirrors `PRManager` + `Jq`)

Follow the `out, err := p.Jq.FormatReviewThreads(raw, resolved)` shape: the manager
method **orchestrates** raw fetches, then hands raw output to a **pure formatter** that
renders the LLM-oriented text. For git the formatters are pure Go functions — not jq —
because git output (`--numstat`, `rev-list --count`, `log --format`) is line-oriented
text, not JSON; piping it through a jq subprocess would add a hop just to split strings.
jq remains the formatter only where the input is JSON (the `gh` payloads in `pr.go`).

```go
// orchestration (mock-tested)          // pure formatting (fixture-tested, no mocks)
func (tm *TaskManager) ReviewScope()    func formatReviewScope(s scopeData) string
                                        func parseNumstat(raw string) ([]fileStat, error)
```

Same benefits as the jq filters: formatters get golden-fixture unit tests with zero
mocking; orchestration tests only assert the git calls made and error paths.

**No existing signatures change.** `pr.go` is untouched; `Git.DefaultBranch()` keeps its
signature so its existing callers (worktree creation) need no updates — if any step turns
out to require changing a current task's signature or output, update every call site in
the same commit and call it out in the PR.

### Step 1 — `Git.DefaultBranch()` probe fallback

- [x] When `symbolic-ref refs/remotes/origin/HEAD` fails or is empty, probe
      `origin/main`, `origin/master`, `origin/develop` in order with `RemoteBranchExists`;
      return the first hit; keep `"main"` as the last resort — **same signature; no
      call-site changes**
- [x] Unit tests: origin/HEAD set; unset + master repo; unset + develop repo; unset + nothing

### Step 2 — `TaskManager.ReviewScope() (string, error)`

- [x] `FetchOrigin()` first, under a bounded timeout (a hang defeats the "fast single
      call" purpose as surely as a failure does); on failure **or timeout** **do not
      abort** — mark the output `(fetch failed — comparing against local refs)` and
      continue
- [x] **Timeout mechanics** (decided after review round 2): add an optional
      `Timeout time.Duration` field to `commands.CommandParams`; when non-zero the base
      executor runs via `exec.CommandContext` with that deadline. Zero value = today's
      behavior, so **no existing call site changes** (the struct is built by field name
      everywhere). The fetch uses 10s. Test strategy: factor the context construction
      into a small pure helper and unit-test it, plus a mock-level test that a
      deadline-exceeded error takes the same `(fetch failed …)` path — no real
      commands, per testing-patterns.md
- [x] Resolve default branch (step 1), merge-base, ahead/behind, commit subjects
      (`git log --format=%s <base>..HEAD`), and the per-file table with a total row
- [x] **File-row status letters** (decided after review round 2): `--numstat` alone has
      no change type, so run two plumbing calls in-process — `git diff --numstat
--no-renames <base>...HEAD` and `git diff --name-status --no-renames
    <base>...HEAD` — and merge them by path. `--no-renames` on **both** so a rename
      deterministically renders as `D` + `A` regardless of the machine's `diff.renames`
      config. Both calls are inside one `dg task` invocation: zero extra LLM round trips
- [x] Detect the edge: current branch **is** the default → print exactly
      `On <default> — no branch to compare. Review uncommitted changes or name a target.`
      (this feeds the agent's "ask for clarification" path)
- [x] Detect the edge: **detached HEAD** (`git branch --show-current` prints nothing) →
      print exactly `Detached HEAD at <short-sha> — no branch to compare. Check out a
branch or name a target.` and exit 0 — a sentinel, not an error, mirroring the
      on-default case so agent behavior is deterministic in both modes
- [x] Output shape (compact markdown, one screen):

  ```
  branch: feat/x -> main (default)  [ahead 5, behind 0]
  commits:
  - feat(task): add review-scope
  - test(task): cover offline fetch
  files (12):
  M  internal/tooling/task/task.go      +120/-30
  A  internal/tooling/task/scope.go     +200/-0
  ...
  total: +1500/-400
  excluded (see `dg task branch-diff --file <path>` to inspect): go.sum (+40/-12)
  ```

- [x] Split per the architecture note: `ReviewScope()` orchestrates; `parseNumstat` /
      `formatReviewScope` are pure functions with fixture tests (including binary-file
      `-	-	path` numstat lines); orchestration tests mock git and cover
      offline-fetch and on-default-branch cases

### Step 3 — `TaskManager.BranchDiff(file string) (string, error)`

- [x] Default exclusion list (package-level `var`, one place):
      `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `bun.lockb`, `go.sum`,
      `Cargo.lock`, `Gemfile.lock`, `composer.lock`, `poetry.lock`, `uv.lock`,
      `*.min.js`, `*.min.css`. Deliberately **non-exhaustive** — it covers the ecosystems
      most likely in play, not every lockfile (`Pipfile.lock`, `mix.lock`,
      `Podfile.lock`, `packages.lock.json`, … are absent by design). The `--file` bypass
      and raw `git diff` escape hatch cover the gaps; growing the list or making it
      configurable is the deferred follow-up in §4, not a launch requirement
- [x] **Patterns match at any depth** (decided after review round 2): emit each pattern
      as `':(exclude,glob)**/<pattern>'` — git's glob magic matches `**/foo` at the root
      _and_ in subdirectories, so monorepo workspace lockfiles
      (`packages/app/package-lock.json`) are excluded too. Monorepos are where the
      lockfile noise is largest, so root-only matching would miss the main payoff
- [x] **Does not fetch.** `review-scope` is the orient-and-fetch step; `branch-diff` is
      follow-up retrieval within the same review session. Re-fetching per file pull is
      wasteful and actively harmful — a commit landing on `origin` between calls would
      shift the merge-base and desync `branch-diff` from the scope the agent already read.
      Compute the base against the current (possibly stale) remote-tracking refs and
      accept it: base stability across a review session is the intended behavior, and it
      reinforces §7's "compute the merge-base once and reuse it".
- [x] No file argument → a **single** `git diff <base>...HEAD -- . ':(exclude)<p1>'
':(exclude)<p2>' …` invocation (one process, N exclude pathspecs — not one diff per
      pattern); then append **one line per excluded file that actually changed** with its
      `--numstat` counts — nothing is ever silently hidden. When no non-excluded file
      changed, print the sentinel `No reviewable changes in <base>...HEAD (all changes
excluded — see notes below).` above the exclusion notes so the payload is never
      empty
- [x] `--file <path>` → that file's diff **without** exclusions (explicit request wins).
      The path is passed as an argv element to `git diff` (never shell-interpolated), so
      it needs no escaping; if the path has no changes in the range, print the sentinel
      `No changes for <path> in <base>...HEAD.` rather than emitting nothing
- [x] Reuse step 2's base/default-branch resolution and `parseNumstat` (shared unexported
      helpers); exclusion-note rendering is a pure formatter like step 2's
- [x] Unit tests: exclusions applied, exclusion notes emitted, `--file` bypass,
      all-excluded sentinel, `--file`-not-in-range sentinel, no-changes
      case — formatter cases via fixtures, orchestration via mocks

### Step 4 — CLI registration

- [x] `cmd/task.go`: register `review-scope` (no flags) and `branch-diff` (`--file`)
      following the existing task subcommand pattern; print the returned string
- [x] Extend the `taskRunner` interface (`cmd/task.go:29`) with `ReviewScope()` and
      `BranchDiff(file string)` — and update the fake runner in `cmd/task_test.go` in
      the same change, or nothing compiles
- [x] Help text: `Short` one-liner + an `Example` block per subcommand showing the
      review flow (`review-scope` → `branch-diff --file <path>`), matching the existing
      task subcommands' style
- [x] Command tests alongside (`cmd/task_test.go` precedent)

### Step 5 — Rewire the shared configs

- [x] `configs/shared/agents/code-reviewer.md`: Scope step 3 becomes
      `devgeta task review-scope` then `devgeta task branch-diff` (whole or `--file` by
      file for large branches); delete the fetch/never-pull/fallback prose it replaces;
      **add the missing `"devgeta task *": allow` permission**; keep raw `git diff`
      permissions as escape hatch
- [x] `configs/shared/commands/create-pr.md`: step 1 becomes `review-scope`
      (+ one `git log <default>..HEAD --format="%s%n%b%n---"` for commit bodies, using
      the default branch review-scope reported — fixes the hardcoded `main`); the
      "read the full diff" pointer becomes `branch-diff`
- [x] `configs/shared/commands/review-pr.md`: step 2's local fallback becomes
      `review-scope` + `branch-diff`; delete the fetch prose it replaces
- [x] No changes: `approve-pr.md`, `address-feedback.md` (already fully task-based),
      `smart-commit.md` (staged-only, already minimal — see Scope Boundary)

### Step 6 — Docs & close-out

- [x] `docs/spec.md`: add the two subcommands to the task section
- [x] README task table
- [x] Mark this cycle **Done**, all boxes checked

---

## 6. Verification Plan

**Automated:** `go test ./...` green; `make lint` clean; `VerifyNoRealCommands` in every
new test; new tests cover offline fetch, fetch **timeout**, on-default-branch,
**detached HEAD**, the numstat/name-status merge (incl. binary and `--no-renames`
cases), exclusion notes, the **all-excluded** and **`--file`-not-in-range** sentinels,
`--file` bypass, and all three default-branch probe outcomes.

**Manual (golden path, per CLAUDE.md §6 — before writing tests):**

1. On a scratch branch of this repo with a couple of commits:
   `dg task review-scope` → header, commits, stat table; totals match
   `git diff <default>...HEAD --stat`, where `<default>` is the default branch
   review-scope itself reported (don't hardcode `main` — that's the bug this cycle fixes)
2. `dg task branch-diff` → diff excludes `go.sum` (touch it deliberately), exclusion note shows counts
3. `dg task branch-diff --file go.sum` → full go.sum diff comes back
4. `git remote set-url origin <unreachable>` (temporarily) → review-scope still answers,
   marked `(fetch failed …)`; restore the remote afterwards
5. On `main` directly → the "no branch to compare" message, exit 0

**Token sanity check:** rerun the §1 measurement on the scratch branch —
`review-scope` output should be ≤ the `--stat` baseline (~1 KB), and `branch-diff` on a
branch touching `go.sum` should shrink by the lockfile's share.

---

## 7. Risks & Trade-offs

- **Exclusion hides a real change** — e.g. a hand-edited lockfile or a supply-chain
  review that _needs_ lockfile diffs. Mitigated three ways: exclusions are announced
  per-file with counts (never silent), `--file` bypasses them, and raw `git diff`
  permissions remain in the agent configs.
- **`review-scope` fetches (network side effect).** `git fetch` only updates
  remote-tracking refs — never the working tree — and the offline path degrades
  gracefully. Acceptable for a read-only reviewer.
- **Merge-base drift vs. three-dot:** `git diff <merge-base>..HEAD` and
  `git diff origin/<default>...HEAD` are equivalent when base is computed after the
  fetch; compute base once and reuse it for stat + diff so the two can't disagree.
- **Shared-config coupling:** the configs will instruct `devgeta task …`, which assumes
  the target machine has devgeta ≥ this release. Same trade already accepted for the PR
  subcommands in the 2026-06-18 cycle.
- **`bun.lockb` is binary** — git shows it as `Bin` in stat; the exclusion note must
  handle numstat's `-	-	path` form for binary files.

---

## 8. Cross-Model Review Notes

Reviewed 2026-07-14 (document-reviewer lens). The three challenged decisions, resolved:

1. **Hardcoded exclusion list vs. configurable now — keep hardcoded.** Configurability is
   deferred in §4 and the risk is mitigated three ways (per-file announce, `--file`
   bypass, raw `git diff` escape hatch). The list is now explicitly documented as
   non-exhaustive (Step 3) so its gaps read as a deliberate choice, not an oversight.
2. **Fetch inside `review-scope` — keep, but bounded and one-sided.** Only `review-scope`
   fetches (now under a timeout, Step 2); `branch-diff` explicitly does **not** fetch
   (Step 3), so the merge-base can't shift mid-review-session. This resolves the
   reviewer's "separate processes don't share runtime state" gap: base stability across a
   session is intended, not accidental.
3. **Commit subjects in `review-scope`, bodies via `create-pr`'s own `git log` — keep the
   split.** Subjects are cheap and always wanted for orientation; bodies are only needed
   at PR-authoring time, so `create-pr` fetching them itself avoids paying for body text
   on every review. A `review-scope --with-bodies` flag is premature — folded into the
   §4 configurability deferral.

Also addressed from the review: `--file` path handling and empty-result sentinels (Step
3), a fetch timeout (Step 2). The exclusion-list-hides-a-real-change risk remains tracked
in §7.

**Round 2 (2026-07-14, document-reviewer lens — risk rating Medium, feasibility gaps).**
All four author questions answered and folded into the steps:

1. **Timeout plumbing** → additive `Timeout time.Duration` on `commands.CommandParams`
   driving `exec.CommandContext`; zero value preserves current behavior so no call sites
   change (verified: the struct is built by field name everywhere). Fetch uses 10s.
   (Step 2)
2. **Status letters** → kept as a hard requirement; derived from a second in-process
   `git diff --name-status --no-renames` merged with numstat by path. `--no-renames` on
   both calls makes renames render deterministically as `D` + `A` regardless of local
   `diff.renames` config. (Step 2)
3. **Nested lockfiles** → patterns match at any depth via `':(exclude,glob)**/<pattern>'`;
   monorepo workspace lockfiles are the largest noise source. (Step 3)
4. **Detached HEAD** → sentinel, not error: `Detached HEAD at <short-sha> — no branch to
compare. Check out a branch or name a target.`, exit 0. (Step 2)

Also fixed from round 2: `taskRunner` interface + `cmd/task_test.go` fake updates and
help-text examples added to Step 4; §6's automated list synced with the step-level test
cases; §6 manual step 1 no longer hardcodes `main`.

---

## Notes for Implementers

- Mirror `pr.go`'s shape: manager methods return the string to print; `cmd/` stays thin.
- Compute the merge-base **once** per invocation and thread it through — a second
  resolution after a concurrent fetch could disagree.
- Keep the output formats stable once shipped: agents' prompts will reference the
  `excluded (…)` line and the `On <default> —` sentinel verbatim.
- Numstat parsing: fields are tab-separated `added<TAB>removed<TAB>path`; binary files
  use `-` for both counts.
