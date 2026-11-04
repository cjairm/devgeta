# Recent Changes

Short prose summaries of changes whose _reasoning_ isn't obvious from the code
or the commit. This is a reading aid, not a record — the permanent record is
`docs/decisions/` (why we chose it), `docs/plans/cycles/` (what the work was),
and git history (what changed).

**Pruning rule:** keep entries from the last two releases. Delete anything older
in the same pass that adds a new entry. If deleting an entry would lose
something a reader still needs, that thing belongs in an ADR or a guide — put it
there first, then delete. This file must not grow without bound; that is exactly
why it no longer lives in `CLAUDE.md`.

**Last updated:** 2026-11-04

---

## Recent changes

- Pi is a devgeta-managed terminal app (2026-11-04). `dg install` now sets up
  Pi (`@earendil-works/pi-coding-agent`) alongside Claude and OpenCode, from
  its official installer run under `sh` — its shebang is `#!/bin/sh`, the
  inverse of opencode's bash contract, so the same `RunInstallScript` staging
  needed a different interpreter. Three things make the app shape different
  from its neighbors. First, the script requires Node.js 22.19+ and installs
  Node itself only interactively, which devgeta never is — so the app runs
  its own preflight matching the script's, before anything is downloaded,
  and a clean-machine full install (terminal phase runs before languages)
  fails with "install Node first (`dg install --only languages`), then
  re-run" instead of a wasted download. Second, the managed install root
  (`~/.pi/agent/install`) shares a home with Pi's own state
  (`~/.pi/agent` — sessions, config, agent state), so uninstall removes
  only the `install` subtree; deleting all of `~/.pi` would take the user's
  sessions with it. Third, unlike opencode and claude, Pi gets no themes, no
  shared skills, no permission sync, and no shell feature — how devgeta
  should configure it is undecided until a later cycle, and the two-agent
  sync contract is deliberately not extended to a third. The channel repeats
  ADR-0047's accepted opencode trade knowingly; ADR-0059 records it. Review
  follow-ups live in `docs/plans/cycles/2026-11-04-pi-app.md`.

## Recent specs completed

- `specs/001-binary-dist-audit/` — Go embed, text/template for config generation
- `specs/002-debian-package-fixes/` — Strategy pattern, package mappings, exponential backoff downloads
