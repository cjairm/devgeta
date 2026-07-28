# ADR-0001 — In-TUI floating overlays instead of external picker processes

**Date:** 2026-07-15
**Status:** ACCEPTED

## Context

The worktree dashboard (`dg wt ui`) needs interactive pickers and prompts (repo
selection, name entry) for the create-from-TUI flow. Two ways to get them: shell out to
an external picker (fzf, as `dg wt rm` does on the plain CLI), or build the picker inside
the Bubble Tea program from the shared components toolkit.

Running an external program from inside a running Bubble Tea TUI suspends the program and
hands the whole terminal to the child process, so the dashboard disappears while the
picker is open. The product requirement is the opposite: popups must float on top of the
still-visible dashboard. A survey of competing multi-session tools (July 2026) showed the
fastest tools keep the interaction inside one screen.

## Decision

Inside any devgeta TUI, pickers, prompts, and modals are built from the
`internal/tui/components` toolkit and rendered as floating overlays composited over the
current view. External picker processes are not launched from inside a running TUI.
Plain CLI commands (no TUI running) may still use fzf.

## Consequences

- Easier: consistent look and keybindings across all TUI modals; background context stays
  visible; no dependency on fzf being installed for TUI features; modal behavior is
  testable with the standard `tea.KeyMsg`-driven tests.
- Harder: we own fuzzy matching and overlay compositing (including ANSI-safe line
  splicing), which fzf would have given us for free.
- Accepted trade-off: a small amount of picker code to maintain in exchange for the
  floating-overlay UX and one fewer runtime dependency.
