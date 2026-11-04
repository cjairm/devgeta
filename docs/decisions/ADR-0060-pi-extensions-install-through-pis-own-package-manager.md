# ADR-0060 — Pi extensions install through Pi's own package manager, at configure time

**Date:** 2026-11-05
**Status:** ACCEPTED

## Context

ADR-0059 added Pi with no configuration at all (decision 6): no themes, no
skills, no extension management. That left Pi markedly less useful out of the
box than the other two coders devgeta ships — Claude Code and OpenCode both get
devgeta-managed configuration — and the extensions that make Pi a practical
harness (subagent delegation, web access, scheduled prompts, a status line,
OpenRouter provider controls, stall recovery, thinking cleanup) had to be
installed by hand, one `pi install npm:…` at a time, on every machine.

Pi already has a package manager for exactly this:

```
pi install <source> [-l]   # install a source and add it to settings
pi remove <source> [-l]    # remove it again
pi list                    # list what settings declare
```

`pi install` records each source in `~/.pi/agent/settings.json` under
`packages` and fetches the package into `~/.pi/agent/npm`. Entries are either a
bare source string or an object carrying one under `source`; adding a source
that is already declared replaces that entry rather than appending a second.

The question was therefore not *whether* devgeta should configure Pi
extensions, but *who owns the install mechanism* — devgeta writing settings and
running npm itself, or Pi doing it.

## Decision

**1. Devgeta states the set; Pi performs the installs.** `dg configure pi`
(and therefore `dg install`) runs `pi install <source>` once per extension in
`piExtensions` (`internal/apps/pi/pi.go`), through the Pi app's own
`ExecuteCommand` wrapper. Devgeta never writes Pi's settings file, never
resolves extension versions itself, and never touches Pi's `npm` tree. Pi owns
the file format, the npm tree, and updates — the same split ADR-0047 reached for
OpenCode plugins.

**2. The curated set is an opinionated default, not a per-user feature.** Eight
extensions, all general-purpose Pi extensions taken from the public npm
registry: `pi-subagents`, `pi-web-access`, `pi-schedule-prompt`,
`pi-powerline-footer`, `pi-openrouter-advanced-provider`,
`pi-openrouter-request-details`, `pi-stalled-agent-recovery`,
`pi-thinking-reformatter`. Two are OpenRouter-specific and harmless otherwise.
The list lives in one place in code and is mirrored in `docs/apps/pi.md`; adding
one is a normal code change.

**3. Idempotency reads Pi's own state, from the directory Pi actually uses.**
`SoftConfigure` parses `<agent dir>/settings.json` and installs only the sources
it does not already declare, so re-running `dg install` costs no network.
`ForceConfigure` re-runs every install, which is how Pi picks up newer versions
of an already-declared source. Pi's settings file is the source of truth;
devgeta keeps no parallel record of which extensions are installed (no
`global_config.yaml` entry per extension).

The agent dir is `$PI_CODING_AGENT_DIR` when set — with a leading `~` expanded,
as Pi does — and otherwise the managed install root's parent. Pi moves both
`settings.json` and its `npm` tree together, so reading the default location
while Pi writes to a relocated one would make every soft configure reinstall the
whole set. Only this read follows the override: install detection and uninstall
stay keyed on the fixed managed root (ADR-0059 decision 2).

**4. A failed install rolls back the ones this run made.** Installs run
sequentially; if the fifth fails, the four this run added are removed again
(newest first, via `pi remove`), so configure never leaves a half-installed set
behind (§4, atomic installation). Only sources this run added are rolled back —
a `--force` re-install of an extension Pi already declared is not ours to
remove. `pi install` records a package only after it installs successfully, so
the failing source needs no removal. If the rollback itself fails, both failures
are reported together.

**5. A settings file devgeta cannot parse is an error, never a reset.**
Unreadable or malformed JSON fails `configure` with the file named. Treating it
as empty would re-install over a user's file and hide the corruption.

**6. `configuring` still registers Pi, and only after the extensions land.**
The installed-list entry in `global_config.yaml` is written after the installs
succeed, so a failed extension install does not leave Pi marked as configured.
The extensions themselves are not tracked there — Pi's settings already say what
is installed.

**7. Uninstall still removes only the managed install root.** Extensions, their
npm tree and Pi's settings survive `dg uninstall pi`, unchanged from ADR-0059
decision 3. Removing packages from a user's settings is a separate decision the
maintainer can make explicitly with `pi remove`.

**8. Extension versions are not pinned.** Sources are plain
`npm:<package>`, so each configure installs whatever npm serves. This is the
same trade ADR-0059 decision 5 accepted for Pi itself, applied to the
references Pi resolves through the npm registry: the artifact is the one npm's
tarball integrity checks vouch for, and the packages are Pi extensions — code
the user's agent loads and runs anyway — installed by Pi's own resolver rather
than by a devgeta-owned installer.

Pinning was considered and rejected. Pi does support exact specs
(`npm:<pkg>@<version>`; `parseSource` marks them `pinned`), and it would narrow
what configure can pull in, but it changes the update story this ADR documents:
a pinned source cannot advance through `pi update`, and a forced configure would
*downgrade* a user whose Pi already has a newer release. A
reviewed-version list would also go stale between devgeta releases, leaving
users on extensions with fixed bugs. If the threat model changes — extensions
becoming privileged beyond what the agent already is, or a supply-chain incident
in one of these packages — pinning becomes a code change here plus an ADR
amendment, not a silent default.

**9. Everything else stays out of scope.** No themes, no shared skills, no
two-agent permission/formatting parity, no `dg worktree`/`dg task` wiring. This
ADR amends ADR-0059 decision 6 only as far as extension installation; the rest
of that decision stands.

## Consequences

- A fresh `dg install` leaves Pi with the same practical capability set on every
  machine, without a per-machine ritual.
- Configure can fail on a network or npm problem, and it now takes longer than a
  config-file write. It fails loudly, names the extension, and does not mark Pi
  as configured.
- Devgeta depends on Pi's package-manager surface (`install`, source syntax,
  settings shape). Reading both package-entry shapes (string and object) keeps
  that dependency narrow, but a change in Pi's settings format is a change
  devgeta has to follow — the same exposure ADR-0047 accepted for OpenCode.
- Extension versions are whatever npm serves at configure time (decision 8).
  There is no lockfile and no pinning; `dg update pi` remains unsupported, so
  refreshing an extension is `dg configure pi --force` or `pi update <source>`.
- Configure is atomic at the set level: a failed install leaves Pi's settings as
  it found them. It cannot undo a package whose own npm install half-wrote its
  tree — that is Pi's npm transaction, not devgeta's.
