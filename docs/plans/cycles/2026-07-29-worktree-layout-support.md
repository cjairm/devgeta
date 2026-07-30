# Cycle: make devgeta work with worktrees wherever they live

**Date:** 2026-07-29
**Estimated Duration:** ~5 hours
**Status:** Approved — in progress

---

## 1. Domain Context

`docs/migrations/v1-to-v2.md` was applied for real today against this repo's two
worktrees. Git moved them correctly and no work was lost, but devgeta lost track of
them completely: `dg wt ls` listed nothing, `dg ws` showed an empty dashboard, and
`remove`/`repair` could not find them. One tmux shell fell back to `$HOME`, where an
agent would have run git against the wrong repo. The worktrees were moved back to
restore the workflow; the findings are recorded in `f7b2b89`.

The cause is that five call sites compute a worktree's location from convention
rather than asking git, and `List()` enumerates _directories_ — so a git-ignored
husk left behind still showed up as a worktree row with an empty branch.

[ADR-0010](../../decisions/ADR-0010-worktree-layout-is-a-setting-git-is-the-index.md)
decides the two answers this cycle implements: the layout is a `dg config` setting
defaulting to today's shared root, and git — not a directory scan — is the index.

---

## 2. Engineer Context

### The five sites that assume the layout

| Site                                      | What it assumes                           |
| ----------------------------------------- | ----------------------------------------- |
| `worktree.go:131` `worktreePath()`        | builds the shared-root path from parts    |
| `worktree.go:136` `GetWorktreeBasePath()` | the single hard-coded root                |
| `worktree.go:459` `List()`                | `os.ReadDir` of the shared root           |
| `worktree.go:586` `findRepoForWorktree()` | `os.ReadDir` of the shared root           |
| `repo_candidates.go:111`                  | `filepath.Join(GetWorktreeBasePath(), …)` |

`worktreePath()` is the choke point for mutation — `worktreeState`, `Repair`,
`RepairInRepo`, and `removeByRepo` all route through it, so making it layout-aware
fixes those four without touching them individually.

### Why git-based enumeration is a perf win, not a cost

`List()` already calls `ListWorktreesAt` **inside its per-worktree loop**
(`worktree.go:506`) purely to recover each branch — that is `git -C <dir> worktree
list --porcelain` (`internal/apps/git/git.go:553`), one exec per worktree, on every
3-second `dg ws` refresh. The same command run once per _repo_ returns every
worktree of that repo with path and branch. Exec count goes from per-worktree to
per-repo. Do not add a second git call per item anywhere in this path.

### Where the settings registry lives

`cmd/config_settings.go` — `var Settings []Setting` plus `FindSetting`. Note the
import direction: `internal/config` **cannot** import `internal/tooling/worktree`
(worktree already imports config in four files), which is why the registry is in
`cmd/`. A `worktree.layout` entry's `Default`/`Set` must therefore call into
worktree from `cmd/`, exactly like `default_layout` already does.

### Testing

`internal/tooling/worktree` uses `internal/testutil` mocks; `internal/config` cannot
(import cycle) and uses its local `setupIsolatedConfigPaths`. Never execute real git
in tests — `testutil.VerifyNoRealCommands`. Enumeration tests must mock
`git worktree list --porcelain` output, including a worktree at an in-repo path.

```bash
go test ./internal/tooling/worktree/ ./internal/config/ ./cmd/
go test ./...
make lint
```

---

## 3. Objective

Every `dg wt` command and the `dg ws` dashboard operate on a worktree wherever it
lives on disk; `worktree.layout` selects where new ones are created; `dg wt move`
relocates an existing one and moves its tmux window with it; and the v1→v2 migration
becomes a real, listed guide instead of a draft.

---

## 4. Scope Boundary

### In Scope

- [ ] `Git.MoveWorktree` in `internal/apps/git/git.go` (wrapper, per CLAUDE.md)
- [ ] `worktree.layout` registered in `cmd/config_settings.go` (`shared` | `in-repo`)
- [ ] `worktreePath()` becomes layout-aware; all mutation paths inherit it
- [ ] `List()` reasks git per repo instead of scanning the shared root
- [ ] `findRepoForWorktree` and `repo_candidates.go:111` become layout-agnostic
- [ ] `dg wt move <name> [--to <layout>]` — relocate + retarget the tmux window
- [ ] Tests: both layouts, phantom-husk case, in-repo enumeration, move + window
- [ ] Rewrite `docs/migrations/v1-to-v2.md` as a real guide; list it in the index
- [ ] `docs/spec.md` + `README.md` for the new setting and command

### Explicitly Out of Scope

- **Changing the default layout.** Stays `shared`. Promoting `in-repo` is a separate
  decision and a v2.0.0 bump (ADR-0010, "Not decided here").
- **Editing anyone's `.gitignore`.** `dg wt move --to in-repo` warns if
  `.claude/worktrees/` is not ignored; it does not write the file.
- **A third layout**, per-repo layout overrides, and migrating other tools'
  worktrees into devgeta's naming.

**Scope is locked.**

---

## 5. Implementation Plan

| Action | File                                                                                    | Description                            |
| ------ | --------------------------------------------------------------------------------------- | -------------------------------------- |
| Modify | `internal/apps/git/git.go`                                                              | `MoveWorktree`                         |
| Modify | `internal/tooling/worktree/worktree.go`                                                 | layout-aware paths, git-based `List()` |
| Modify | `internal/tooling/worktree/repo_candidates.go`                                          | drop the base-path join                |
| Modify | `cmd/config_settings.go`                                                                | `worktree.layout` entry                |
| Modify | `cmd/worktree.go`                                                                       | `dg wt move`                           |
| Modify | `docs/migrations/v1-to-v2.md`, `docs/migrations/README.md`, `docs/spec.md`, `README.md` | docs                                   |

### Step 1: `Git.MoveWorktree`

Wrap `git worktree move <from> <to>`, resolving the main worktree first like
`RemoveWorktree` does. Surface git's own refusal (locked worktree) unchanged — it is
the safety net.

- Verify: `go test ./internal/apps/git/ -run TestMoveWorktree`

### Step 2: The `worktree.layout` setting

Register it with `Default` returning the live default (`shared`) and `Set`
validating against the two known values, listing them on error the way
`default_layout` does. Add it to `knownStateFields`' sibling assertions so the
completeness test stays green.

- Verify: `go test ./cmd/ -run TestSettings`

### Step 3: Layout-aware `worktreePath()`

`worktreePath(repoSlug, name)` consults the setting: `shared` keeps today's path,
`in-repo` returns `<repo-root>/.claude/worktrees/<flat-name>`. It needs the repo
root, not just the slug, so resolve it from the recent-repos store or the current
repo. `worktreeState`, `Repair`, `RepairInRepo`, and `removeByRepo` inherit this for
free — confirm each by test rather than by inspection.

- Verify: `go test ./internal/tooling/worktree/ -run TestWorktreePath`

### Step 4: `List()` asks git

For each known repo (recent-repos store ∪ shared-root slugs ∪ current repo), one
`ListWorktreesAt` call; build `WorktreeStatus` from its output, keeping the single
`PaneStates()` scan for tmux. Drop the per-worktree git call and the directory walk.
A husk directory git does not report must not appear.

- Verify: `go test ./internal/tooling/worktree/ -run TestList`

### Step 5: `findRepoForWorktree` and the picker

Both resolve through the same git-backed enumeration from Step 4 instead of reading
the shared root.

- Verify: `go test ./internal/tooling/worktree/`

### Step 6: `dg wt move`

`dg wt move <name> [--to shared|in-repo]` (default: the other layout). Move via
Step 1, then retarget the tmux window at the new path — `tmux respawn-pane` is wrong
(it would kill a running agent); send a `cd` to an idle pane and, when a pane has a
live foreground process, say so and leave it, since a running agent must not be
killed to satisfy a path change. Refuse on a dirty worktree without `--force`, and
warn when moving `--to in-repo` if `.claude/worktrees/` is not gitignored.

- Verify: `go test ./cmd/ -run TestWorktreeMove`

### Step 7: Docs

Rewrite `v1-to-v2.md` around `dg config set worktree.layout in-repo` + `dg wt move`,
delete the hand-rolled `git worktree move` steps and the stale warnings, and add it
to `docs/migrations/README.md`'s table. Update `docs/spec.md` and `README.md`.

---

## 6. Verification Plan

```bash
go test ./... -cover
make lint
```

Beyond the happy paths, tests must cover: a worktree at an **in-repo** path being
listed, a **husk** directory in the shared root **not** being listed, a repo with
**two** worktrees costing **one** git exec, `remove`/`repair` on an in-repo
worktree, and `move` **refusing** rather than killing a pane with a live process.

### Manual

1. `go build -o ~/.local/bin/devgeta .` (there is still no `make install`).
2. `dg config` shows `worktree.layout` as `(default)` → `shared`.
3. `dg wt ls` unchanged for existing worktrees — the regression that matters most.
4. `dg wt move ws-agent-panes --to in-repo` → moves, window follows, `dg wt ls`
   **still lists it**, `dg ws` still shows it.
5. `dg wt remove`/`repair` work on it while in-repo.
6. `dg config set worktree.layout in-repo` → a new `dg wt create` lands in-repo and
   is listed alongside a shared-root one.
7. Leave an empty dir in the shared root → it does **not** appear as a row.
8. `dg wt move` on a pane running an agent → refuses, does not kill it.

### Regression Check

- A config with no `worktree.layout` behaves exactly as v1.6.0.
- `dg ws`'s 3-second refresh does no more git execs than before (fewer, with 2+
  worktrees in a repo).

---

## 7. Risks & Trade-offs

| Risk                                                    | Likelihood | Mitigation                                                                                                 |
| ------------------------------------------------------- | ---------- | ---------------------------------------------------------------------------------------------------------- |
| Enumeration change breaks `dg ws` for everyone          | **High**   | It is the most-used path; Step 4 lands with tests for both layouts and the husk case before any doc change |
| A repo in neither the store nor the root goes unseen    | Med        | Accepted in ADR-0010; `create` records the repo, and the picker already had this reachability              |
| `move` kills a live agent's pane                        | Med        | Step 6 refuses instead, asserted by test                                                                   |
| In-repo worktrees pollute `git status`                  | Med        | Warn on `--to in-repo` when not ignored; never edit the user's `.gitignore`                                |
| Layout setting drifts from where worktrees actually are | Low        | Git is the index — the setting only chooses where _new_ ones go                                            |

### Trade-offs Made

- **Two layouts forever.** The cost of not forcing anyone to migrate.
- **Git becomes required to list.** A repo with broken metadata lists empty rather
  than showing husks. That is the honest answer, and husks were the bug.

---

## 8. Cross-Model Review Notes

- [x] **`--to` is optional, defaulting to the configured `worktree.layout`.**
      Resolved on approval. Neither offered option was right: the common intent is
      "bring this worktree in line with the layout I configured", which is exactly
      the migration case, so a bare `dg wt move <name>` means that. An explicit
      `--to` still overrides, and moving a worktree already at the target layout is a
      no-op that says so rather than an error. Inferring "the other layout" was
      rejected because it silently becomes wrong once a third layout exists.
- [x] **No `search_paths` fallback.** Resolved on approval: `List()` runs on the
      3-second `dg ws` refresh and `search_paths` scanning is a `filepath.WalkDir`
      (`scan.go`), so it cannot go in that path at any depth limit. The three cheap
      sources stand; if a repo genuinely goes missing in practice the fix is to
      record it on use, not to walk the filesystem every 3 seconds.
- [ ] Still open from the last cycle: there is no `make install`, so every manual
      verification silently requires `go build -o ~/.local/bin/devgeta .`. This has
      now bitten twice in one day.
