# Cycle: Create Worktrees from the TUI (`n` in `dg wt ui`)

**Date:** 2026-07-15
**Estimated Duration:** ~8 hours
**Status:** Done

---

## 1. Domain Context

`dg wt ui` is the worktree dashboard: it lists every devgeta-managed worktree grouped by
repo and supports attach, destroy (with session-hop), repair, filter, and a branch-diff
pane. Creating a worktree, however, still requires leaving the TUI and typing
`dg wt new <name> --repo <path>` — the most common action has the most friction.

A July 2026 investigation of competing multi-session tools (Claude Squad, worktrunk, uzi,
sesh, gwq, Crystal) found the winning UX patterns are: single-key in-TUI create,
create+attach collapsed into one action, and "the name is the only thing you type —
everything else is inferred." This cycle brings those patterns to `dg wt ui`.

Target flow: press `n` → a floating repo picker opens **on top of** the dashboard
(pre-selected on the repo under the cursor) → Enter → a floating name prompt → Enter →
worktree + window created, TUI exits into the new window. Common case: `n`, Enter, type
name, Enter. Zero flags, zero copy-paste.

Related: [ROADMAP.md](../../ROADMAP.md) Worktree Enhancements; prior cycles
`2026-06-07-worktree-v2-tui-dashboard.md`, `2026-06-08-tui-components.md`.

---

## 2. Engineer Context

- **Relevant files:**
  - `internal/tui/worktree/model.go` — dashboard model. Injectable action seams
    (`attachFn`, `removeFn`, `repairFn`, …) at `model.go:90-95`; normal-mode key dispatch
    in `handleKey` (`model.go:296-436`); attach logic incl. auto-repair in `handleAttach`
    (`model.go:496-539`); two-press confirm pattern in `confirmThenRemove`
    (`model.go:545-578`); actions refresh the list by re-emitting `statusesMsg`.
  - `internal/tui/components/` — shared toolkit (mandatory building blocks):
    `cmdpalette.go` (`CommandPalette` renders a `: query█` input + selectable list —
    rendering only, no key handling), `filter.go` (`FilterField` single-line text input
    with `HandleKey`), `helpoverlay.go` + `whichkey.go` (popup rendering),
    `notification.go` (toast for errors).
  - `internal/tooling/worktree/worktree.go` — `Create(name, coder, force)` (`:103`, current
    repo), `CreateAt(repoPath, name, coder, force)` (`:118`, any repo; window opens in the
    repo-slug session), `List()` (`:286`), `GetWorktreeBasePath()` (`:95`),
    `GetWindowName(repoSlug, name)` (`:703`).
  - `internal/tooling/worktree/aicoder.go` — `ResolveAICoder` (`:47`),
    `ResolveAIAlias(flag, gc)` (`:63`); `handleAttach`/`handleRepair` already show the
    resolution pattern the create action must reuse.
  - `internal/config/fromFile.go` — `WorktreeConfig{DefaultAI}` (`:68-71`) inside
    `GlobalConfig` (`:83`). **Caution:** `Save()` (`:102-108`) and `Reset()` (`:110-118`)
    currently use plain `os.WriteFile` — NOT atomic, despite CLAUDE.md requiring atomic
    state writes. This cycle fixes that (Step 4) before expanding what gets written.
- **Key fact:** the `?` help overlay currently _replaces_ the whole view
  (`model.go:631-632` returns only the popup), which is why the background disappears.
  This cycle adds real compositing so popups float over a static background.
- **Key gap:** nothing stores source repo paths. Repo slugs in the dashboard are not
  paths; a repo's root is recoverable from any of its worktrees via
  `git rev-parse --git-common-dir`, but a repo with zero worktrees is unknown. The
  recent-repos store closes this.
- **Testing:** [docs/guides/testing-patterns.md](../guides/testing-patterns.md) — always
  `testutil.MockApp`, `testutil.VerifyNoRealCommands`, isolate every `paths.Paths.*`
  mutation with `t.Cleanup`, `func init() { testutil.InitLogger() }`. TUI tests drive
  `Update` with `tea.KeyMsg` and assert on `View()` output and injected seam calls.
- **Test commands:**

  ```bash
  go test ./internal/tui/... ./internal/tooling/worktree/ ./internal/config/
  go test ./...
  make lint
  ```

---

## 3. Objective

Pressing `n` in `dg wt ui` creates a worktree in a repo chosen from a floating fuzzy
picker (defaulting to the repo under the cursor, ranked by recent use), prompts only for
a name, then attaches to the new window — without the dashboard background ever clearing.

---

## 4. Scope Boundary

### In Scope

- [x] Overlay compositor in `internal/tui/components` that renders a popup on top of the
      existing view (background stays visible); `?` help overlay migrated to it
- [x] In-TUI fuzzy picker component (fzf-like: type to filter, move, Enter to select)
      built on `CommandPalette` — no external fzf process
- [x] Atomic global-config writes: `GlobalConfig.Save()` and `Reset()` switch to
      write-temp-then-rename, with tests — prerequisite for expanding config writes,
      and closes an existing violation of the atomic-state rule
- [x] Recent-repos store: repo roots + last-used timestamps recorded in
      `global_config.yaml` on every successful create (CLI and TUI), MRU-ordered,
      capped at 20, missing paths pruned on read
- [x] Repo candidate provider: cursor-row repo first, then stored recent repos (MRU),
      then zoxide results when zoxide is installed, then a free-typed path
- [x] `n` keybinding wiring: repo picker → name prompt → create (`CreateAt`) → attach and
      quit; errors shown as a toast without leaving the TUI
- [x] Hint bar and `?` help updated; docs updated (`docs/spec.md`, ROADMAP)

### Explicitly Out of Scope

- Prompt-first create (passing a task prompt to the AI coder) — separate roadmap item
- Auto-naming when the name is left blank — separate roadmap item
- AI-coder selection inside the flow (uses the existing `ResolveAIAlias` precedence)
- Base-branch selection (branch always created from the repo's current default behavior)
- Migrating other overlays/TUIs beyond the worktree dashboard's `?`

**Scope is locked.** If something out of scope is needed, document it for a future cycle
and reference here.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                     | Description                                                        |
| ------ | --------------------------------------------- | ------------------------------------------------------------------ |
| Create | `internal/tui/components/overlay.go`          | Compositor: splice popup lines over a rendered background          |
| Create | `internal/tui/components/overlay_test.go`     | Width/ANSI/edge-case tests                                         |
| Create | `internal/tui/components/fuzzypicker.go`      | Picker state + `HandleKey` + `View` (uses `CommandPalette`)        |
| Create | `internal/tui/components/fuzzypicker_test.go` | Filtering, navigation, selection tests                             |
| Modify | `internal/config/fromFile.go`                 | Atomic `Save()`/`Reset()`; add `RecentRepos` to `WorktreeConfig`   |
| Modify | `internal/tooling/worktree/worktree.go`       | Record repo on successful create; repo-candidates provider         |
| Modify | `internal/tui/worktree/model.go`              | `n` flow (picker → name prompt → `createFn`), migrate `?` overlay  |
| Modify | `cmd/worktree.go`                             | No behavior change expected; verify create path still records repo |
| Modify | `docs/spec.md`, `ROADMAP.md`                  | Document the flow; move roadmap item to in-progress                |

### Step-by-Step

#### Step 1: Overlay compositor

- `internal/tui/components/overlay.go`: `Overlay(background, popup string, width, height int) string`
  — center the popup box over the background's lines, preserving the visible background
  around it. Use `charmbracelet/x/ansi` for width math and truncation; background lines
  under the popup are cut at the popup's left edge and resume after its right edge (reset
  styles at the seams so background colors don't bleed into the popup).
- Tests: plain and ANSI-styled backgrounds, popup taller/wider than screen, tiny sizes.
- Verify: `go test ./internal/tui/components/`

#### Step 2: Migrate `?` help to the compositor

- `renderHelpOverlay` (`model.go:853`) composites over the normal dashboard view instead
  of returning only the popup.
- Verify manually: `dg wt ui` → `?` → dashboard stays visible behind the popup.

#### Step 3: Fuzzy matching + picker component

- Add a small case-insensitive subsequence matcher (rank: exact prefix > substring >
  subsequence) — plain Go, no new dependency.
- `FuzzyPicker` component: items (label + optional hint), query editing (reuse
  `FilterField` semantics), `j`/`k`/arrows, Enter selects, Esc cancels; `View` renders via
  `CommandPalette` inside a `WhichKeyPopup`-style border.
- Verify: `go test ./internal/tui/components/`

#### Step 4: Atomic config writes + recent-repos store

- First, make `GlobalConfig.Save()` and `Reset()` atomic: marshal, write to a temp file
  in the same directory, then `os.Rename` over the target. Tests prove a failed write
  never leaves a truncated/partial config behind. This fixes an existing violation of
  the atomic-state rule and is a prerequisite for writing to the config more often.
- `WorktreeConfig` gains `RecentRepos []RecentRepo` (`path`, `last_used` RFC3339). New
  optional field — old configs load unchanged; call the schema addition out in the PR per
  change discipline.
- Helpers: upsert (bump timestamp, cap 20), read pruned (drop paths that no longer
  exist). Tests isolate config paths with `t.Cleanup`.
- Paths are stored in one canonical form: expand `~`, make absolute, `filepath.Clean`,
  resolve symlinks best-effort (`filepath.EvalSymlinks`, falling back to the cleaned
  absolute path on error). Upsert and dedupe compare only canonical paths.
- Verify: `go test ./internal/config/`

#### Step 5: Record repos on create + candidates provider

- On successful `create` (both `Create` and `CreateAt` paths) upsert the canonical repo
  root into the store. Best-effort, never fails the create; a store write failure is
  surfaced as a user-visible non-fatal warning — CLI create prints it via the existing
  `utils` warning helper, TUI create shows it as a toast — plus a debug log entry.
- `RepoCandidates(cursorRepoSlug string)` in `internal/tooling/worktree`: resolve the
  cursor repo's root via an existing worktree (`git rev-parse --git-common-dir`, mocked in
  tests), then stored recents (MRU), then `zoxide query -l` when zoxide exists (mocked).
  Every candidate is canonicalized (same contract as Step 4) before deduping, so the same
  repo never appears twice regardless of source.
- Free-typed paths are validated at selection time (exists, is a directory, is a git
  repo) — invalid input shows a toast and stays in the picker; validation never waits
  until create.
- Verify: `go test ./internal/tooling/worktree/` with `VerifyNoRealCommands`

#### Step 6: Wire `n` in the dashboard

- Add a `createFn` seam next to the others (`model.go:90-95`) delegating to
  `mgr.CreateAt`.
- Mode state: normal → repo-pick (picker pre-selected on the cursor row's repo) →
  name-input (single-line prompt, floating) → create. On success: attach via the existing
  `handleAttach` path and quit; on failure: toast, stay in the TUI. Refresh with
  `statusesMsg` if the user cancels attach.
- Add `n` to the hint bar and `?` help entries.
- Verify: `go test ./internal/tui/worktree/` (key-sequence tests against seams)

#### Step 7: Manual verification, docs, roadmap

- Manual golden path (below), then update `docs/spec.md` and flip the ROADMAP item.
- Verify: full `go test ./...` + `make lint`

---

## 6. Verification Plan

### Automated Verification

```bash
go test ./internal/tui/... ./internal/tooling/worktree/ ./internal/config/
go test ./... -cover
make lint
```

### Manual Verification

Executed 2026-07-15 in an isolated sandbox (throwaway `$HOME`/XDG dirs, two throwaway git
repos, driven via a dedicated tmux session) — see cycle closeout notes for evidence.

1. [x] `dg wt ui` → `?` → help floats over the dashboard; any key closes it, background intact
2. [x] Cursor on a repo's row → `n` → picker floats, that repo pre-selected → Enter → name
       prompt → type a name → Enter → TUI exits into the new tmux window with the AI coder
3. [x] Repeat `n` and type to filter: recent repos rank first; a repo used moments ago is top
4. [x] Pick a repo that has no worktrees yet (from zoxide or typed path) → create succeeds and
       the repo appears in `global_config.yaml` under `worktree.recent_repos`
5. [x] Remove that repo's only worktree → press `n` → the repo still appears (from the store)
6. [x] Esc in the picker and in the name prompt returns to the dashboard unchanged
7. [x] Enter an invalid name (existing worktree) → toast error, TUI still running
8. [x] `dg wt new <name>` from a repo directory still works and records the repo

### Regression Check

- [x] `dg wt ui` attach/destroy/repair/filter/diff pane unchanged
- [x] `dg wt new`, `dg wt rm`, `dg wt ls` unchanged
- [x] Old `global_config.yaml` without `recent_repos` loads without error

---

## 7. Risks & Trade-offs

| Risk                                                        | Likelihood | Mitigation                                                                                |
| ----------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------- |
| ANSI compositing corrupts styled background lines           | Med        | Dedicated tests with styled input; reset sequences at popup seams; reuse `x/ansi` helpers |
| Stored repo paths go stale (moved/deleted repos)            | Med        | Prune on read; selection re-validates the path is a git repo before create                |
| zoxide output slow or huge                                  | Low        | Query lazily on picker open, cap results, skip entirely when zoxide is absent             |
| Config schema change breaks old configs                     | Low        | Additive optional field only; explicit round-trip test with a legacy config fixture       |
| TUI state machine complexity (three modes + existing modes) | Med        | Follow the existing confirm/diff mode patterns; key-sequence tests per mode               |

### Trade-offs Made

- **In-TUI picker vs. external fzf:** external fzf suspends the TUI and takes over the
  terminal — exactly the background-clearing effect this cycle removes. We build the
  picker from the components toolkit instead.
- **Own store vs. zoxide-only:** zoxide may be absent and knows directories, not "repos
  devgeta used." The store is authoritative; zoxide is a supplement for first-time repos.
- **Attach immediately after create vs. stay in dashboard:** attach-and-quit matches the
  create+attach pattern every surveyed tool converged on; staying would add a keystroke to
  the common case.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope is actually locked?
- [ ] Steps are actionable?
- [ ] Verification is executable?
- [ ] Risks are realistic?

**Reviewer notes:**
2026-07-15 external review, resolutions:

- CRITICAL — doc claimed global config writes were atomic; `Save()`/`Reset()` actually
  use plain `os.WriteFile`. Resolved: atomic write (temp + rename) for **all** config
  writes added to scope as the first part of Step 4, with no-partial-write tests.
- IMPORTANT — recents-store write-failure warning was unspecified. Resolved: visible
  non-fatal warning (CLI `utils` helper / TUI toast) plus debug log, defined in Step 5.
- IMPORTANT — candidate dedupe lacked a normalization contract. Resolved: canonical path
  form defined in Step 4 (expand `~`, absolute, clean, best-effort symlink resolution)
  and applied to all sources in Step 5; free-typed paths validate at selection time.
- MINOR — `docs/spec.md` said `DEVGETA_AI` instead of `DEVGETA_WORKTREE_AI`. Fixed
  directly (doc correction, independent of this cycle).

---

## Notes for Implementers

- **Cycle document is your spec.** Update it if requirements change, but don't change
  scope without calling it out.
- **Commit after each step.** Run `/smart-commit` once a step's verify check passes.
- **Verification must pass before "done."** Automated tests + manual checks + regression
  check.
- **If you hit a risk, escalate immediately.**
- **Ask questions early.**
