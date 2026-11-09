# ADR-0066 — Devgeta removes its token-reduction layer

**Date:** 2026-11-07
**Status:** ACCEPTED (2026-11-08, step 0 of the
[rollback cycle](../plans/cycles/2026-11-07-agent-customizations-rollback.md),
which implements it)

## Context

Since July, devgeta has wrapped the AI coders in machinery of its own, most of
it built to cut token costs:

- **output-budget:** a hook, plugin and runner that capped command output
  (off by default).
- **`task-redirect`:** a hook in both coders that denied certain raw `git` and
  `gh` commands and named a `dg task` command to run instead (ADR-0007).
- **`dg task handoff`:** a per-branch session note (ADR-0032).
- **`dg task context-report`:** a measurement of what loads each session.
- **An agent-facing `dg task` group:** about 25 subcommands for reviews, PRs
  and issues, described in `task-design.md` as "AI-first, token-wise".
- **The review machinery behind them:** the review journal (ADR-0012) and the
  review loop (ADR-0017 to ADR-0020, ADR-0022, ADR-0023, ADR-0025 to
  ADR-0027).

The evidence is in
[the rollback investigation](../investigations/2026-11-06-agent-customizations-rollback.md)
and [the rtk investigation](../investigations/2026-11-06-rtk-value-and-risk.md):

- **The savings were small.** The measurable share of the bill that output
  shortening could touch was under 2%.
- **Blocking and shortening cost turns.** Hidden output made agents rerun
  commands. A denied command is a wasted turn, plus a devgeta-only command
  the agent has to learn.
- **The callers are gone.** The shared commands and skills that called the
  agent `dg task` group were removed in `1906e9e`. Once `task-redirect` goes,
  nothing devgeta ships calls the group.

## Decision

Remove, in both coders:

- output-budget: `configs/claude/output-budget.sh`,
  `configs/opencode/plugin/output-budget.js`,
  `configs/devgeta/output-budget-run.sh`, and the
  `integrations.output_budget` setting.
- `task-redirect`: `configs/claude/task-redirect.sh`,
  `configs/opencode/plugin/task-redirect.js`, and the `release` redirect
  inside it.
- `dg task` subcommands: `handoff`, `context-report`, `commit-trailers`, the
  review group (`review-scope`, `branch-diff`, `review-package`,
  `review-notes`, `review-note`), every PR and issue subcommand in
  `cmd/task_pr.go` and `cmd/task_issue.go`, and `worktree-start` /
  `worktree-finish` (people use `dg wt`).
- The Go packages behind them: `internal/tooling/handoff`, `contextreport`
  and `reviewjournal` (imported by `internal/tooling/task`; `reviewjournal`
  also by `internal/tooling/worktree`, whose teardown deletes the branch's
  journal; that call goes with it and `dg wt` otherwise stays as it is), and
  the functions in `internal/tooling/task` that nothing else calls.

Keep:

- `task.BranchDiffAt`, `task.BranchStatsAt` and their result types, which
  the `dg ws` dashboard uses.
- The subcommands people use: `refresh-branch`, `reset-main-branch`,
  `delete-branch`, `reinstall-libraries`, `reinstall-library` (the `dge`
  wrapper), `release` (devgeta's own release flow), and `scratch` until the
  skill-package work shows whether anything needs it.

Any piece comes back only through its own ADR that shows a measured need,
the same bar ADR-0065 set for output-rewriting tools.

## Consequences

- Agents run plain `git` and `gh` with no redirect, which is also what
  upstream skills such as Superpowers expect.
- Devgeta's own sessions lose the hook that pushed them to
  `dg task release`. Only CLAUDE.md §9 tells them to release that way now.
  CLAUDE.md §3 principle 8 and §12 used the redirect's `go.mod` gate as their
  example of a gated process and need a new example or none.
- Removing CLI subcommands is a breaking change (CLAUDE.md §9), so the
  release that ships this is a MAJOR bump.
- An old `output_budget` key in `global_config.yaml` is ignored on load and
  dropped on the next save; the migration note says so.
- Supersedes ADR-0007, ADR-0012, ADR-0017, ADR-0018, ADR-0019, ADR-0020,
  ADR-0022, ADR-0023, ADR-0025, ADR-0026, ADR-0027, ADR-0032 and ADR-0034.
  ADR-0031 stays: its rule against rewriting requests at send time still
  holds.
- Work: cycle
  [2026-11-07-agent-customizations-rollback.md](../plans/cycles/2026-11-07-agent-customizations-rollback.md).
