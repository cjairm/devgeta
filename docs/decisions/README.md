# Architectural Decision Records

This directory contains decisions about significant technical choices, trade-offs, and their consequences. See the "Spec-driven development" section of `CLAUDE.md` for when to write an ADR.

## Status

| Status         | Meaning                 |
| -------------- | ----------------------- |
| **PROPOSED**   | Under discussion        |
| **ACCEPTED**   | Decided and implemented |
| **SUPERSEDED** | Replaced by another ADR |
| **DEPRECATED** | No longer in use        |

## How to Create an ADR

1. Copy `TEMPLATE.md` → `ADR-NNNN-brief-title.md` (use next number)
2. Fill in Context, Decision, Consequences
3. Add to the index below
4. Reference in related code comments: `// See ADR-NNNN`

## Index

- [ADR-0001](ADR-0001-in-tui-overlays-over-external-pickers.md) — In-TUI floating overlays instead of external picker processes
- [ADR-0002](ADR-0002-recent-repos-store-in-global-config.md) — Recent-repos store in global config
- [ADR-0003](ADR-0003-sessions-in-workspace-dashboard.md) — A single `dg ws` dashboard for sessions and worktrees
- [ADR-0004](ADR-0004-ai-tools-install-category.md) — New `ai-tools` install category, rtk as first app
- [ADR-0005](ADR-0005-agent-activity-state-in-tmux-pane-options.md) — Agent activity state lives in a tmux pane option
- [ADR-0006](ADR-0006-hook-guardrails-scope-and-sharing.md) — Scope and code-sharing for the secret-commit and lint-suppression guardrail hooks
- [ADR-0007](ADR-0007-task-redirects-stay-hard-deny.md) — The task redirects stay hard-deny, not `ask`
- [ADR-0008](ADR-0008-agent-state-on-every-pane-row.md) — Agent state belongs to every row in `dg ws`, not just worktree rows
- [ADR-0009](ADR-0009-audible-agent-notifications.md) — An agent that wants you makes a sound, from the hook that already knows
- [ADR-0010](ADR-0010-worktree-layout-is-a-setting-git-is-the-index.md) — Worktree layout is a setting, and git is the worktree index
- [ADR-0011](ADR-0011-agent-prompt-as-launch-argument.md) — An opening prompt is a launch argument, not keystrokes
- [ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md) — Review knowledge lives in a local journal devgeta owns
- [ADR-0013](ADR-0013-normalize-the-worktree-gitfile.md) — Normalize the worktree gitfile instead of warning about it
- [ADR-0014](ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md) — Agent-config protection is a capability guard, not a path deny
- [ADR-0015](ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md) — Agent scratch files get a devgeta-owned directory, not `/tmp`
- [ADR-0016](ADR-0016-inconclusive-tool-probe-fails-open.md) — An inconclusive tool probe must not block worktree creation
- [ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md) — The review loop escalates instead of seeking consensus
- [ADR-0018](ADR-0018-review-loop-refuses-the-default-branch.md) — The review loop refuses the default branch, and the fix is a branch
- [ADR-0019](ADR-0019-a-review-covers-the-branch-working-state.md) — A review covers the branch's working state, not just its committed history
- [ADR-0020](ADR-0020-a-reviewer-that-reports-nothing-is-retried-once.md) — A reviewer that reports nothing is retried once
- [ADR-0021](ADR-0021-pane-commands-are-exec-d-not-typed.md) — A pane's command is exec'd at pane creation, not typed into the pane
