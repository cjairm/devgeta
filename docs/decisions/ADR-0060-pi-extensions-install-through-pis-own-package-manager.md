# ADR-0060 — Pi extensions install through Pi's own package manager, at configure time

**Date:** 2026-11-05
**Status:** ACCEPTED

## Context

ADR-0059 added Pi with no configuration. The extensions that make Pi a
practical harness (subagent delegation, web access, scheduled prompts, a status
line, OpenRouter provider controls, stall recovery, thinking cleanup) had to be
installed by hand, one `pi install npm:…` at a time, on every machine.

Pi already has a package manager: `pi install <source>` records the source in
`~/.pi/agent/settings.json` under `packages` and fetches it into
`~/.pi/agent/npm`. An entry is a bare source string or an object carrying one
under `source`. The question was who owns the install mechanism — devgeta
writing settings and running npm itself, or Pi doing it.

## Decision

**1. Devgeta states the set; Pi performs the installs.** `dg configure pi` (and
`dg install`) runs `pi install <source>` once per entry in `piExtensions`
(`internal/apps/pi/pi.go`). Devgeta never writes Pi's settings, never resolves
versions, never touches Pi's npm tree. _(Amended by
[ADR-0061](ADR-0061-pi-is-a-themed-surface-with-one-devgeta-owned-theme-file.md):
devgeta now writes exactly one key, `theme`, in Pi's settings.)_

**2. The curated set is an opinionated default.** Eight general-purpose
extensions from the public npm registry (listed in `docs/apps/pi.md`, which a
test keeps in sync with the code). Two are OpenRouter-specific and harmless
otherwise.

**3. Idempotency reads Pi's own state.** Configure parses
`~/.pi/agent/settings.json` and installs only the sources it does not declare.
A declaration counts by package name, so a pinned `npm:<pkg>@<version>` is
respected and never replaced. Devgeta keeps no parallel record of extensions.
`--force` behaves the same: there is nothing of devgeta's to overwrite, and
refreshing installed extensions is `pi update`.

**4. A failed install stops and names the extension; there is no rollback.**
Extensions are Pi's own state, a re-run installs whatever is missing, and no
other app rolls back installs made by another tool. Pi is registered in
`global_config.yaml` only after every extension lands, so a failed configure
does not mark Pi as configured.

**5. A settings file devgeta cannot parse is an error, never a reset.** Treating
it as empty would re-install over a user's file and hide the corruption.

**6. Versions are not pinned.** Sources are plain `npm:<package>`, the same
trade ADR-0059 decision 6 accepted for Pi itself. Pinning would stop `pi update`
from advancing a source and go stale between devgeta releases. If the threat
model changes, pinning becomes a code change here plus an amendment.

**7. Uninstall still removes only the managed install root.** Extensions and
settings survive `dg uninstall pi`. _(Amended by
[ADR-0068](ADR-0068-agent-packs-are-a-pinned-upstream-plus-a-build-layer.md):
agent packs install through the same package manager, `pi install <path>`.
Unlike the curated extensions, `dg agent-pack uninstall` removes them with
`pi remove`, because devgeta tracks what it installed
([ADR-0070](ADR-0070-pack-removal-deletes-only-what-devgeta-installed.md)).
`dg uninstall pi` still keeps them, so a pack's Pi record stays and `list`
shows "pi not installed".)_

**8. Everything else stays out of scope:** no themes, no shared skills, no
two-agent parity, no `dg worktree`/`dg task` wiring. _(Amended by
[ADR-0061](ADR-0061-pi-is-a-themed-surface-with-one-devgeta-owned-theme-file.md):
themes are now in scope. Amended by
[ADR-0063](ADR-0063-pi-formats-through-devgetas-shared-mason-script-and-pi-lens-only-diagnoses.md):
Pi formats through devgeta's shared Mason script. Amended by
[ADR-0068](ADR-0068-agent-packs-are-a-pinned-upstream-plus-a-build-layer.md):
shared skills and parity with the other coders are now in scope, through agent
packs.)_

## Consequences

- A fresh `dg install` leaves Pi with the same capabilities on every machine.
- Configure can fail on a network or npm problem. It fails loudly, names the
  extension, and leaves any already-installed ones in place.
- Devgeta depends on Pi's settings shape (both entry forms are read) and on
  `pi install`; a change there is a change devgeta follows, the same exposure
  ADR-0047 accepted for OpenCode.
- `dg update pi` stays unsupported; refreshing an extension is `pi update`.
