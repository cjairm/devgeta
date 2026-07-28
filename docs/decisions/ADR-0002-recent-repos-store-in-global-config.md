# ADR-0002 — Recent-repos store in global config

**Date:** 2026-07-15
**Status:** ACCEPTED

## Context

Creating a worktree for a repo requires the repo's root path, but devgeta stores no repo
paths anywhere: the worktree base directory contains repo _slugs_, and a repo's root is
only recoverable through one of its existing worktrees. Once a repo's last worktree is
removed, devgeta forgets the repo entirely. The create-from-TUI flow needs a ranked list
of repo candidates so the user almost never types a path.

Alternatives considered: derive candidates only from existing worktrees (forgets repos
with zero worktrees), or rely on zoxide (may not be installed, and knows directories, not
"repos devgeta used").

## Decision

devgeta keeps its own store: every successful worktree create upserts the repo root and a
last-used timestamp into `global_config.yaml` (`worktree.recent_repos`), capped at 20
entries, ordered most-recently-used, pruned of nonexistent paths on read. The write is
best-effort — a store failure logs a warning and never fails the create. zoxide, when
installed, only supplements the list with repos devgeta has never seen.

## Consequences

- Easier: repo picker works with zero external dependencies; repos persist across
  worktree removals; "propose the last-used repo first" comes directly from the store.
- Harder: one more piece of state in `global_config.yaml` to keep coherent (additive,
  backward-compatible field — old configs load unchanged).
- Accepted trade-off: paths can go stale between runs; pruning on read plus re-validation
  at selection time contains this without a background cleanup mechanism.
