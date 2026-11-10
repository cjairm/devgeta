# Cycle: Worktree wrapper skill, multi-repo `dg wt create`, Pi coder

**Date:** 2026-11-10
**Estimated Duration:** ~1 day
**Status:** Approved (2026-11-10)

---

## 1. Domain Context

`dg worktree` (`dg wt`) creates a git worktree and branch under
`~/.local/share/devgeta/worktrees/<repo>/<name>`, opens a tmux window for it and
launches an AI coder there. `--repo <path>` targets another repository.

devgeta 1.x shipped a `dg-worktree` skill so agents would use it. The 2.0 reset
removed it because it competed with Superpowers' `using-git-worktrees`, which
`executing-plans`, `subagent-driven-development` and `writing-plans` call by
name ([reset investigation](../../investigations/2026-11-06-skills-and-agents-reset.md),
findings 2 and 3). The plan recorded there: rebuild it as a wrapper with the
same name, so every Superpowers flow that sets up a worktree goes through
`dg worktree`. v2.2.0 shipped that wrapper pattern for `receiving-code-review`.

Three gaps today:

- No skill tells an agent to use `dg worktree`, so agents run raw
  `git worktree add` (or a harness tool like Claude Code's `EnterWorktree`) and
  the worktrees land where `dg wt list/remove/repair` can't see them.
- An issue often spans several repositories. Today that takes one
  `dg wt create` per repo, and an agent repeating the command can drift the
  branch name between repos. Every call also opens a window and a coder, which
  is wrong when one agent should do the whole issue across repos.
- `--ai` accepts only `opencode` and `claude`; Pi is a supported coder since
  the Pi app cycle but can't be launched in a worktree.

## 2. Intent, goals, non-goals

**Intent.** Starting work in a worktree — in one repo or several — goes through
`dg worktree`, so it is always resumable later with `dg wt list` / `repair`,
and the person picks per issue whether one agent works across all repos or each
repo gets its own window and coder.

**Goals**

- One `dg wt create` call creates the same branch in several repositories.
- A way to create worktrees without a tmux window or coder, for the
  "one agent, one issue, several repos" case.
- `--ai pi` works wherever `--ai claude` does.
- Superpowers' worktree step uses `dg worktree` in Claude Code, OpenCode and Pi.

**Non-goals**

- Orchestrating several windows toward one goal. When repos are separate
  tasks, each window's coder works alone.
- Changing where worktrees live, the default coder (stays `opencode`), or
  teardown (`remove`/`prune`).
- A `dg-worktree` skill under its own name.

**Anti-goals**

- A partial multi-repo create: some repos with the branch, others without,
  and nothing saying so.
- Anything specific to one company's repos. The multi-repo case is general:
  frontend + backend + shared library is a common shape.

## 3. Constraints

- CLAUDE.md principle 8 / §12: the wrapper ships to strangers. It must not name
  devgeta's own policies; it names only `dg worktree`, which ships with devgeta.
- The wrapper keeps upstream's `name` and exact `description`
  (`review_skills_test.go` enforces this for the existing wrapper).
- `dg` may not be on PATH in an agent's shell (CLAUDE.md §7, "Shell
  Integration"). The wrapper must fall back to upstream's steps then.
- Atomicity (CLAUDE.md §4, "Data Integrity"): a multi-repo create either
  creates every worktree or removes the ones it made.

## 4. Approaches considered

1. **Wrapper skill + `--no-window` + repeatable `--repo` + Pi.** (picked)
   One `dg wt create` call makes the same branch in every repo, so names can't
   drift. The skill picks windowed or windowless from what the person says.
2. **Skill only, no code.** The windowless case would use raw
   `git worktree add`; those worktrees fall outside `dg wt list/remove/repair`,
   which defeats resuming. Rejected.
3. **Wrapper + `--no-window` only, one call per repo.** Smaller, but the agent
   repeats the branch name per call and a failure mid-way leaves a partial
   set. The maintainer chose to fold the multi-repo call in (approach 1).

## 5. Design

### 5.1 `dg wt create`: repeatable `--repo`

- `--repo` becomes repeatable: `dg wt create fix-auth --repo ~/code/api --repo ~/code/web`.
  With one `--repo`, behavior is unchanged.
- With two or more, the current repo is not implied; the caller lists every
  repo (`--repo .` for the current one). Duplicate paths (after resolving to
  the repo root) are an error.
- Same `<name>`, `--base`, layout/AI and `--prompt` apply to every repo. The
  existing per-repo rules still hold: an existing branch is adopted; `--base`
  with an existing branch is an error.
- **Validate all, then create.** Resolve every path to a repo root and run the
  existing pre-create checks for each before creating anything. Record, per
  repo, whether `<name>` already exists as a local branch, so rollback knows
  which branches it created and which it adopted.
- **Phase 1: create every worktree, launch nothing.** If any repo fails, roll
  back everything this call created, **including the failing repo's own
  partial state**: `CreateWorktreeIn` can fail at `NormalizeWorktreeGitfile`
  after the worktree already exists (`internal/apps/git/git.go:735`). Remove
  each worktree path that exists, and delete each branch this call created by
  passing its name, not `""` as the window-failure cleanup in
  `internal/tooling/worktree/worktree.go:516` does today. Adopted branches are
  never deleted. Report the repo that failed, plus any cleanup step that failed
  and the paths or branches still on disk. Nothing is lost here: no coder has
  started yet.
- **Phase 2: launch windows** (skipped with `--no-window`). From here on a coder
  may already be editing or committing (`--prompt`), so a later launch failure
  **never removes a worktree or branch**. Keep going through the remaining
  repos, then report which windows failed, with `dg wt repair <name>` run from
  that repo as the fix. Repair looks up by name only, so running it from inside
  the intended repo is what picks the right one when several repos share
  `<name>`. Single-repo create retains its existing window-failure cleanup
  behavior.
- On success, print one line per repo: repo, worktree path, branch, window
  (or "no window").

### 5.2 `dg wt create --no-window`

- Creates the worktree and branch only: no tmux window, no coder. Works outside
  tmux.
- Mutually exclusive with `--ai`, `--layout`, `--prompt` and `--pane` (error
  naming the conflict).
- `dg wt list` already shows a worktree with no window; `dg wt repair <name>`
  opens one later. That is the resume path, no new code.

### 5.3 Pi as a worktree coder

- `ResolveAICoder` accepts `pi`; a `PiCoder` mirrors `ClaudeCoder` (launch via
  the `pi` binary, prompt as a launch argument, `EnsureInstalled` checks the
  binary). Add a `PiLaunch` recipe in `pkg/constants/coder_launch.go` next to
  `ClaudeLaunch`.
- Built-in layout `pi` (single pane), added to `builtinLayoutNames`.
- `DEVGETA_WORKTREE_AI=pi` and `worktree.default_ai: pi` work through the
  existing resolution; nothing new there.
- Help text and the alias error list include `pi`.
- **Builder's call:** whether the `dg ws` dashboard (`internal/tui/worktree/agents.go`)
  needs a Pi entry to show Pi sessions. Add it if it keys on coder kind;
  otherwise leave it.

### 5.4 Wrapper skill `using-git-worktrees`

File: `configs/agent-packs/superpowers/skills/using-git-worktrees/SKILL.md`, in
the same override layer as `receiving-code-review`; the build supplies
`UPSTREAM.md`. Same `name` and exact upstream `description`. Content, short:

- Follow `UPSTREAM.md`, with these changes; where they conflict, this file wins.
- If `dg` is on PATH (`command -v dg`), `dg worktree` is the native worktree
  tool for Step 1a. Use it instead of harness tools (`EnterWorktree`) and
  instead of `git worktree add`. If not, follow `UPSTREAM.md` unchanged.
- Step 0 (already in a worktree? consent) applies per repo.
- **Several repos for one issue, one agent:**
  `dg wt create <branch> --no-window --repo <X> --repo <Y> …` (include the
  current repo). Then work in the printed paths.
- **Separate tasks per repo, one coder each:**
  `dg wt create <branch> --repo <X> --repo <Y> [--ai <coder>] [--prompt '<task>']`.
  If the tasks need different prompts, one call per repo.
- Pick the mode from what the person said; if unclear, ask which one. Pass
  `--ai` only when they named a coder.
- Upstream Steps 2–3 (setup, baseline tests) run in each new worktree.

### 5.5 Docs and tests

- Tests (mocked, `testutil.MockApp`, no real git): multi-repo create success;
  phase-1 failure in the second repo rolls back the first; a failure after the
  second repo's worktree exists (gitfile normalization) removes that worktree
  and deletes its created branch but not an adopted one; a cleanup failure is
  reported with the surviving path; a phase-2 launch failure keeps every
  worktree and branch and names the failed window; duplicate repo error; `--no-window`
  creates no window and conflicts with `--ai`/`--layout`/`--prompt`/`--pane`;
  `ResolveAICoder("pi")`; `pi` layout.
- `review_skills_test.go`: add the new wrapper to the shipped-and-general
  check and assert its description equals upstream's.
- Update `docs/spec.md` (worktree section), `docs/guides/agent-sync.md` and
  `docs/guides/agent-packs.md` (new wrapper), `cmd/worktree.go` help.

## 6. Left to the builder

- Flag plumbing for a repeatable `--repo` (`StringArrayVarP`) and how
  `CreateAt` is reused per repo.
- Exact wording of the wrapper and of error messages.
- Whether rollback reuses `Remove` or a narrower helper.
- How phase 1 and phase 2 split `create()`; `--no-window` is phase 1 alone, so
  it should fall out of the split rather than need its own path.

## 7. Checklist

- [ ] `--no-window`
- [ ] Repeatable `--repo`: validate, create all (rollback incl. partial), then launch (no rollback)
- [ ] Pi coder and `pi` layout
- [ ] `using-git-worktrees` wrapper skill
- [ ] Tests (5.5)
- [ ] Docs (5.5)
- [ ] Maintainer: rebuild and `dg agent-pack update superpowers` to try it in all three coders
