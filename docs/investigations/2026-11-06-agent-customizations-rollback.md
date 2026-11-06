# Investigation — Rolling back devgeta's custom agent machinery

**Date:** 2026-11-06
**Status:** Open
**Feeds:** a cycle doc on `agentsv2` for the rollback; ADRs listed in [Decisions this will need](#decisions-this-will-need)
**Branch:** `agentsv2`, alongside
[2026-11-06-skills-and-agents-reset.md](2026-11-06-skills-and-agents-reset.md)
and [2026-11-06-rtk-value-and-risk.md](2026-11-06-rtk-value-and-risk.md).

## Question

Since July, devgeta has added a layer of its own machinery around the AI
coders, most of it meant to cut token costs: hooks that rewrite or block
commands, agent-shaped `dg task` subcommands, a session handoff note, a
context report, and the rtk integration. The rtk investigation found that the
savings it could measure were under 2% of the bill, and that hiding output
made the agent redo work. The plan for v2 is to remove that layer and rebuild
something simpler, adding a piece back only when there is a clear need for
it. What exactly is in the layer, what is safe to remove, and what is not
about tokens at all and should stay?

## Scope

- **In:** everything devgeta ships to Claude Code, OpenCode and Pi beyond the
  coder's own install and theme: hooks, plugins, Pi extensions, `dg task`
  subcommands written for agents, `integrations.*` flags, and the guides and
  ADRs behind them.
- **Out:** skills, commands, agents and `DEVGETA.md` (covered by the skills
  reset investigation), and rtk itself (covered by the rtk investigation).
  Both are listed below only where they block or unblock something here.

## Method

- Sources: `configs/claude/settings.json.tmpl`, `configs/opencode/opencode.json.tmpl`,
  the files in `configs/claude/`, `configs/opencode/plugin/`, `configs/devgeta/`
  and `configs/pi/`, `cmd/task.go`, `cmd/task_pr.go`, `cmd/task_issue.go`,
  `internal/tooling/`, `internal/config/fromFile.go`,
  `docs/guides/token-efficiency.md`, `docs/guides/task-design.md`, and the
  list of ADRs.
- Callers of each `dg task` subcommand: a grep for `task <name>` in
  `configs/shared/` and for the name in the hook scripts and runner, run
  2026-11-06.
- Criteria, set before looking:
  - **Remove** what exists to cut tokens or to support a removed command.
  - **Keep** what protects the user (secrets, agent config, lint
    suppressions), what a person uses directly, and what drives a feature
    outside the agent (the tmux dashboard, formatting).
  - When unsure, keep it and list it as the maintainer's call. A safety rule
    removed by accident is worse than a hook kept one release too long.
- Not done: nothing was run or measured. Token cost per item was not
  measured. Go callers were found by file grep only, not traced. Usage of
  each subcommand by real users is unknown.

## Summary

| Item                                                      | Action            | Why, in a few words                                               |
| --------------------------------------------------------- | ----------------- | ----------------------------------------------------------------- |
| rtk (app, shim, flag)                                     | Remove            | rtk investigation                                                 |
| Output-budget hook, runner, flag                          | Remove            | Token-only; off by default; most of its complexity is rtk-related |
| `task-redirect` hook (both coders)                        | Remove            | Blocks plain `git`/`gh` to push agents onto `dg task`             |
| Agent-shaped `dg task` subcommands (review, PR, etc.)     | Remove            | Their callers are the commands being removed                      |
| Review journal and review loop                            | Remove            | Only the removed review commands use them                         |
| `dg task handoff`                                         | Remove            | Used only by `/save-context` and `/load-context`                  |
| `dg task context-report`                                  | Remove            | Token tooling; its only caller is `/context-report`               |
| `secret-guard`, `agent-config-guard`, `suppression-guard` | Keep              | Safety, not tokens                                                |
| Permissions (allow/ask/deny) and the sync test            | Keep              | Safety; the test keeps the two coders matching                    |
| `format.sh`, `format-file.sh`, Pi `devgeta-format.js`     | Keep              | Formatting, a feature people use                                  |
| `agent-state.sh`, `notify.js`, statusline                 | Keep              | Drive the tmux dashboard and notifications                        |
| Human `dg task` subcommands, `release`                    | Keep              | Used by people (`dge`) and by devgeta's own release flow          |
| Scratch directory (`dg task scratch`)                     | Maintainer's call | Wired into permissions; skills may still want it                  |

---

## Findings

### 1. Built to cut tokens: remove

- **Evidence:**
  - **Output-budget.** `configs/claude/output-budget.sh`,
    `configs/opencode/plugin/output-budget.js`, the runner
    `configs/devgeta/output-budget-run.sh` (18.8 KB), the
    `integrations.output_budget` flag (`internal/config/fromFile.go:64-69,243-246`),
    and the `OutputBudgetEnabled` branch in `settings.json.tmpl:181-190`. Off
    by default (`token-efficiency.md` §3). Its contract,
    `docs/guides/output-budget-runner.md`, spends a section (§9) on racing
    rtk's hook.
  - **`task-redirect`.** `configs/claude/task-redirect.sh` (12.5 KB) and
    `configs/opencode/plugin/task-redirect.js` (17.1 KB) deny certain raw
    `git`/`gh` commands and name the `dg task` command to run instead
    (`task-redirect.sh:1-6`; ADR-0007 keeps it a hard deny). Its global rules
    are described as "better/compressed forms" of git and gh
    (`task-redirect.sh:36-40`).
  - **`context-report`** measures what loads each session
    (`internal/tooling/contextreport/`, `cmd/task.go:527`). The only shipped
    caller is the `/context-report` command.
  - **`handoff`** (`internal/tooling/handoff/`, ADR-0032) is a per-branch
    note. Its callers are `/save-context` and `/load-context`
    (`token-efficiency.md` §4).
- **Verdict:** each of these exists to reduce or measure token spend. The rtk
  investigation found the measurable share of the bill was small and that
  shortened output had a hidden cost in reruns. `task-redirect` has the same
  cost in another form: a denied command is a wasted turn, plus the agent
  has to learn a devgeta-only command. Remove all four.
- **This supersedes the rtk investigation's recommendation 2** (decide
  whether output-budget becomes the default). Under this plan it goes
  instead.

### 2. Agent-shaped `dg task` subcommands: remove with their callers

- **Evidence:** callers found by grep (count of files under `configs/shared/`
  / count of hook or runner files):

  | Subcommand                                                                                                               | Shared | Hooks               |
  | ------------------------------------------------------------------------------------------------------------------------ | ------ | ------------------- |
  | `review-scope`, `branch-diff`, `review-notes`, `review-note`                                                             | 5–7    | 0                   |
  | `review-package`, `review-threads`, `submit-review`, `pr-checks`                                                         | 1–6    | 3 (`task-redirect`) |
  | `review-run`, `pr-review-target`, `pr-review-state`, `current-pr`, `pr-view`                                             | 1–4    | 0                   |
  | `resolve-thread`, `reply-thread`, `create-pr`, `approve-pr`, `request-review`, `comment-pr`, `commit-trailers`           | 1–2    | 0                   |
  | `unresolve-thread`, `update-pr-description`, `request-changes-pr`, `merge-pr`, `pr-state`, `current-repo`, `issue-scope` | 0      | 0                   |

  `docs/guides/task-design.md` describes these as "AI-first, token-wise"
  subcommands whose first consumer is an agent.

- **Verdict:** once the commands go (skills reset) and `task-redirect` goes
  (finding 1), nothing shipped calls this group. Each one is a thin layer
  over `git` or `gh`, which agents already know. Remove the group, along with
  the review journal (`internal/tooling/reviewjournal/`, ADR-0012) and the
  review-loop machinery behind `review-run` (ADR-0017 to ADR-0020,
  ADR-0025 to ADR-0027).
- **Open question:** `pr-state`, `current-repo` and `issue-scope` have no
  shipped caller, but `internal/tooling/task/prstate.go`, `issue.go` and
  `internal/tooling/worktree/worktree.go` turned up in the grep. The
  dashboard may use the Go functions underneath. Trace this before deleting:
  remove the subcommand, and keep any Go function the dashboard still calls.

### 3. Not about tokens: keep

- **Evidence:**
  - `secret-guard`, `agent-config-guard` and `suppression-guard` (both
    coders) block writing secrets, editing the agent's own config, and adding
    lint suppressions (ADR-0006, ADR-0014). They share
    `configs/claude/lib/segments.sh` and `lib/devgeta-repo.sh` with
    `task-redirect`.
  - The permission lists in `settings.json.tmpl` and `opencode.json.tmpl`,
    held in sync by `internal/apps/opencode/permissions_test.go`.
  - `format.sh` (PostToolUse on Edit/Write) and `configs/devgeta/format-file.sh`
    format through Neovim's Mason. Pi uses the same script (ADR-0063).
  - `agent-state.sh` (six hook events) and `notify.js` (23.7 KB) feed the tmux
    dashboard and its sounds (ADR-0005, ADR-0008, ADR-0009, ADR-0055).
  - `refresh-branch`, `reset-main-branch`, `delete-branch` and
    `reinstall-libraries` back the `dge` shell wrapper. `release` is
    devgeta's own release flow (CLAUDE.md §9).
- **Verdict:** none of these exist to save tokens. Removing them would take
  away safety rules or features people use, and the v2 aim (simpler) doesn't
  require it. Keep them.
- **Watch for:** deleting `task-redirect` must keep `lib/segments.sh` and
  `lib/devgeta-repo.sh`, because the guards still source them. The hook also
  holds the `release` redirect, gated on `go.mod` (CLAUDE.md §12), and that
  goes with it. After that, only CLAUDE.md §9 tells devgeta's own sessions to
  release through `dg task release`, and CLAUDE.md §12 needs a different
  example of a gated process.

### 4. Scratch directory: the maintainer's call

- **Evidence:** `dg task scratch` and the devgeta-owned scratch folder
  (ADR-0015, ADR-0033) are referenced in 5 shared files and one hook, and are
  added to Claude Code's `additionalDirectories` (`settings.json.tmpl:129`)
  and OpenCode's allow list (`opencode.json.tmpl:167`).
- **Verdict:** it isn't token tooling. It gives agents a place for temporary
  files that isn't `/tmp` or the repo. Whether v2 still wants that depends on
  which skills come back. Keep it until the rebuild shows whether anything
  uses it.

---

## Shared work

- **Order on `agentsv2`:**
  1. Skills reset removals (commands, agents, in-house skills).
  2. rtk.
  3. `task-redirect` and output-budget, in both coders in the same commit
     (`agent-sync.md`).
  4. The `dg task` group and the review journal.
  5. Guides and ADR status updates.

  Doing the commands first means no shipped file points at a subcommand
  that's already gone.

- **Docs to remove or rewrite:** `docs/guides/token-efficiency.md`,
  `docs/guides/output-budget-runner.md`, `docs/guides/task-design.md`, the
  `dg task` sections of `docs/spec.md` and README, the hook sections of
  `docs/apps/claude.md` and `docs/apps/opencode.md`, and the CLAUDE.md
  documentation index rows for task design and output budget.
- **Existing users:** a `dg configure --force` rewrites `settings.json` and
  the OpenCode plugin folder, so removed hooks and plugins disappear on
  deploy. Users who set `integrations.output_budget` or
  `integrations.rtk_claude_hook` keep a flag that no longer does anything.
  Decide whether config loading ignores unknown flags or removes them, and
  write a `docs/migrations/` note.
- **Versioning:** removing `dg task` subcommands changes the CLI, so this is
  at least a MINOR bump and arguably MAJOR (CLAUDE.md §9). The maintainer's
  call.

## Verification still needed

1. Trace the Go callers of the `dg task` group (`go list` plus a grep for
   each `TaskManager` method) so the dashboard keeps what it uses.
2. After removal, run the full `go test ./...` once on the branch. It touches
   `configs/`, `cmd/` and `internal/tooling`, which is most of the tree.
3. Deploy from the branch to a sandboxed `HOME` and check that each coder
   starts with no hook errors and the guards still fire.

## Decisions this will need

- **ADR:** devgeta removes its token-reduction layer (output-budget,
  `task-redirect`, `handoff`, `context-report`, the agent `dg task` group),
  and any piece comes back only through its own ADR showing a measured need.
  This supersedes ADR-0007, ADR-0012, ADR-0017 to ADR-0020, ADR-0025 to
  ADR-0027 and ADR-0032. ADR-0031 stays: its rule against rewriting requests
  at send time still holds, and it is the reason rtk's proxy is rejected.
  ADR-0034 (aggregate tasks) was not read; check whether anything it governs
  survives.
- **Maintainer's calls:** whether the scratch directory stays; MINOR or MAJOR
  bump; what happens to the old `integrations.*` flags.
