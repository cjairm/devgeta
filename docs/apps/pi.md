# Pi

Pi (`@earendil-works/pi-coding-agent`) is a terminal AI-coding harness. Devgeta
installs it as a terminal tool and registers it in `global_config.yaml`.

## Install

`dg install` (terminal category) or `dg install --only pi`.

Devgeta runs Pi's official installer (`https://pi.dev/install.sh`) under `sh`
— its shebang is `#!/bin/sh`. Like opencode, the script is staged through a
download-then-execute guard so a broken download cannot be reported as a
successful install (ADR-0059).

Idempotency keys on the managed install root, not a package manager:

- Skips if the script's marker (`~/.pi/agent/install/managed-install.json`)
  exists.
- Skips if `pi` already resolves on PATH (installed by other means — npm, your
  own setup): devgeta never lays a second copy over it.

## Requirements

Devgeta checks **Node.js ≥ 22.19 + npm** before downloading anything, so a
missing runtime fails fast with the fix in the message. The official script
makes the same check — it installs Node itself only interactively, which
devgeta never is. If the check fails, install Node first — `dg install --only
languages` — then re-run `dg install --only pi`. On a clean machine a full `dg
install` reaches the terminal phase before languages, so pi reports this once
and is picked up on the re-run.

## Configuration — deliberately minimal

Devgeta currently configures nothing for Pi: no themes, no shared skills, no
permissions sync with claude/opencode, no `dg worktree`/`dg task` wiring. The
only state is the entry in `global_config.yaml`'s installed list. How devgeta
should configure Pi is undecided — see ADR-0059 decision 6.

## Uninstall

`dg uninstall pi` removes **only** `~/.pi/agent/install` — the managed install
root. `~/.pi/agent` itself holds Pi's config, sessions and agent state and is
never touched. Note that the `pi` binary is a symlink into the managed root,
so it disappears with it; PATH entries the script added to your shell profile
are left alone.
