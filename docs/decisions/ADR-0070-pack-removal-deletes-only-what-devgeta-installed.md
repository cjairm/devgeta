# ADR-0070 — Removing a pack deletes only what devgeta installed

**Date:** 2026-11-08
**Status:** ACCEPTED

## Context

A pack changes files the user also edits: `~/.claude/settings.json`,
`~/.claude/CLAUDE.md`, `opencode.json`, and Pi's `settings.json`. Principle 4
says user edits are never overwritten. A user may also already have the same
upstream from another source (for example Superpowers from
`claude-plugins-official`, or `pi install git:…`), and removing that would
destroy something devgeta didn't create.

## Decision

1. **`agent_packs:` in `global_config.yaml` records every item devgeta
   installed,** per pack and per coder: version and SHA, the saved `exclude`
   list, `replaced_hashes`, the Claude marketplace and plugin id, the Pi package
   path, the `opencode.json` entries, and whether the `CLAUDE.md` line was
   added. The section is additive; old configs load and round-trip.
2. **Uninstall removes only what the record lists,** and only for the coders
   asked for. Nothing else in those configs changes.
3. **The `@DEVGETA.md` line is removed only when devgeta added it and it is
   still exactly devgeta's line,** and the output says so. A line the user
   added before devgeta did, or later changed, stays. `~/.claude/DEVGETA.md`
   itself belongs to devgeta and is deleted.
4. **When no coder has a pack any more, its built tree and state entry are
   deleted.** The read-only clone cache stays, so a reinstall at the same SHA
   doesn't need the network. When no pack is left, devgeta also removes its
   Claude marketplace.
5. **A pack installed another way is never removed.** `install` detects it,
   stops, and prints the exact removal command for the user to run.
6. **State follows the coder.** `dg uninstall claude` and
   `dg uninstall opencode` delete their config directories, so they also drop
   that coder from every pack's record. `dg uninstall pi` keeps Pi's packages
   ([ADR-0060](ADR-0060-pi-extensions-install-through-pis-own-package-manager.md)),
   so the record stays and `list` shows "pi not installed".
7. **State is written once, at the end** of a successful install or
   uninstall, so a failed run leaves it unchanged.

## Consequences

- Uninstall is safe next to the user's own packs and edits.
- State can go stale if someone edits a coder's files by hand. For Claude
  Code and OpenCode, the next `dg configure` re-renders the pack entries from
  state. Pi's `settings.json` isn't rendered by devgeta, so `list` checks it
  against state and reports any difference. Pi stores the package path
  relative to its agent dir, so `list` resolves it before comparing.
- Unlike Pi's curated extensions, devgeta removes its packs from Pi on
  uninstall, because it installed them for a specific purpose and tracked them.
- A config format change: it needs a migration note
  (`docs/migrations/agent-packs.md`), per CLAUDE.md §10.
