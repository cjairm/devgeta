# ADR-0059 — Pi installs from its official script, keyed on the managed-install root

**Date:** 2026-11-04
**Status:** ACCEPTED

## Context

Devgeta adds Pi as a third terminal AI-coding harness alongside opencode and
claude. Its only supported install channel is the official script,
`https://pi.dev/install.sh`. It installs the npm package
`@earendil-works/pi-coding-agent` as a _managed install_ under
`~/.pi/agent/install`, symlinks `pi` into an existing PATH directory, writes a
launcher at `~/.pi/agent/bin/pi`, and records a JSON marker
(`managed-install.json`) inside the root, last.

Three facts shaped the decision:

1. **The shebang is `#!/bin/sh`** — the inverse of opencode's (`bash`). ADR-0047's
   interpreter lesson applies in reverse: match the shebang, run it with `sh`.
2. **It is non-interactive without a tty and checks its own prerequisites.**
   If Node.js ≥ 22.19 + npm is missing it fails cleanly with instructions, so
   devgeta needs no preflight of its own; the script is the check.
3. **The managed root and Pi's own state share a home.** Pi's config, sessions
   and agent state live in `~/.pi/agent`. Only the `install` subtree is
   devgeta's business; deleting all of `~/.pi` would destroy user data.

## Decision

**1. Install runs the official script with `sh`, via `cmd.RunInstallScript`.**
Same staging as opencode — no raw `curl | sh`, so a failed download cannot pipe
an empty script into a shell that then exits 0.

**2. Idempotency keys on the managed install, not a package manager.**
`SoftInstall` skips when `~/.pi/agent/install/managed-install.json` exists or
when `pi` resolves on PATH (a user who installed Pi by other means keeps their
copy). It goes through `baseapp.SoftInstall`, shared with opencode.

**3. Uninstall removes only `~/.pi/agent/install`.** `~/.pi/agent` — sessions,
config, skills, extensions — is untouched.

**4. Devgeta does not manage Pi's PATH.** The script symlinks into an existing
PATH directory or edits the shell profile itself. No alias, no shell feature.
Configure uses the launcher at `~/.pi/agent/bin/pi` as a fallback when `pi` is
not yet on PATH (ADR-0060).

**5. Like opencode, devgeta does not follow the script's environment
overrides.** `PI_MANAGED_INSTALL_ROOT` and `PI_CODING_AGENT_DIR` can move the
root; devgeta runs the script without them and assumes the default layout, the
same way opencode assumes `~/.opencode`. A user who relocates Pi by hand gets a
re-run of the installer on soft install and must uninstall by hand.

**6. This knowingly repeats ADR-0047's accepted security trade.** The script is
not hash-checked and installs whatever npm serves at install time. It is the
official channel, it verifies its own runtime dependencies, and the npm package
it installs is fetched under npm's tarball integrity checks. A checksum-pinned
alternative would fork Pi's install channel for no gain over the parity opencode
already established.

**7. Config parity with claude/opencode is out of scope.** No themes, no shared
skills, no agent-sync entries, no `coder_launch` wiring; the two-agent sync test
is not extended. _(Amended by [ADR-0060](ADR-0060-pi-extensions-install-through-pis-own-package-manager.md):
Pi installs a curated extension set at configure time. Amended by
[ADR-0061](ADR-0061-pi-is-a-themed-surface-with-one-devgeta-owned-theme-file.md):
themes are now in scope. Amended by
[ADR-0063](ADR-0063-pi-formats-through-devgetas-shared-mason-script-and-pi-lens-only-diagnoses.md):
Pi formats through devgeta's shared Mason script. Amended by
[ADR-0068](ADR-0068-agent-packs-are-a-pinned-upstream-plus-a-build-layer.md):
parity with Claude Code and OpenCode is now in scope. Agent packs go to Pi
like the other coders, and the sync guide and parity tests widen from "the two
agents" to every coder.)_

## Consequences

Terminal apps install before languages, so a clean-machine `dg install` can
reach Pi before Node exists. The script's own error says what to do; run
`dg install --only languages`, then `dg install --only pi`. Reordering the
category phases is a separate, larger decision.
