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
