# ADR-0059 — Pi installs from its official script, keyed on the managed-install root

**Date:** 2026-11-04
**Status:** ACCEPTED

## Context

Devgeta adds Pi as a third terminal AI-coding harness alongside opencode and
claude. Its only supported install channel is the official script:

```
https://pi.dev/install.sh
```

The script installs the npm package `@earendil-works/pi-coding-agent` as a
*managed install* under `$HOME/.pi/agent/install` (overridable only by
`PI_MANAGED_INSTALL_ROOT`), with the `pi` binary symlinked into an existing
PATH directory (`~/.local/bin`, `~/bin`, …). It records a JSON marker —
`managed-install.json` — inside that root.

Three facts shaped the decision:

1. **The shebang is `#!/bin/sh`** — the inverse of opencode's (`bash`). Running
   it under `bash` would still work, but the contract the script advertises is
   POSIX `sh`, and on Debian that means dash. ADR-0047's interpreter lesson
   applies in reverse: match the shebang, run it with `sh`.
2. **It is non-interactive without a tty.** With stdin/stdout captured it takes
   the default action (install or migrate) and skips the "start pi now?"
   launch. If Node.js ≥ 22.19 + npm is missing it fails cleanly with
   instructions rather than half-installing — so devgeta needs no prerequisite
   check of its own; the script is the check.
3. **The managed root and Pi's own state share a home.** Pi's config, sessions
   and agent state live in `$HOME/.pi/agent`. Only the `install` subtree is
   devgeta's business; deleting all of `~/.pi` would destroy user data.

## Decision

**1. Install runs the official script with `sh`, via `cmd.RunInstallScript`.**
Same staging as opencode's installer — no raw `curl | sh` pipeline, so a failed
download cannot pipe an empty script into a shell that then exits 0.

**2. Detection and idempotency key on the managed install, not a package
manager.** `SoftInstall` skips when the managed marker
(`~/.pi/agent/install/managed-install.json`) exists or when `pi` resolves on
PATH — the second check keeps a user who installed Pi by other means (npm,
their own setup) on their own copy.

**3. Uninstall removes only `~/.pi/agent/install`.** The managed root is
everything the script owns. `~/.pi/agent` — sessions, config, skills — is
untouched. This is the opencode `clearManagedConfig` allowlist thinking
applied to an uninstall.

**4. Devgeta does not manage Pi's PATH.** The script itself symlinks into an
existing PATH directory, or asks to edit the shell profile — that part is the
script's job, and devgeta adds no alias, no shell feature, and no `ShellFeatures`
entry. Nothing in the generated shell config references pi.

One fact about the layout is worth recording for the same reason: the script
also always writes a launcher at `<agent dir>/bin/pi`
(`write_managed_install_launcher` in the install script), so a machine with the
managed marker has a known working entry point even when nothing on PATH points
at it yet — configure uses that fallback (ADR-0060), while PATH management
itself stays the script's.

**5. This knowingly repeats ADR-0047's accepted security trade.** ADR-0004
chose checksum-verified GitHub releases for rtk specifically to avoid piping an
upstream script into a shell. As with opencode, the Pi script is not
hash-checked and installs whatever npm serves at install time. The same
rationale applies: it is the official channel, it self-verifies its runtime
dependencies, and the alternative (npm global installs devgeta owns) reopens
the version-drift problems ADR-0047 closed.

**6. Config parity with claude/opencode is out of scope and not promised.** No
themes, no shared skills, no agent-sync entries, no `coder_launch` wiring. The
two-agent sync contract (`internal/apps/opencode/permissions_test.go`) is
deliberately not extended; how devgeta should configure Pi is undecided until a
later cycle. *(Amended by [ADR-0060](ADR-0060-pi-extensions-install-through-pis-own-package-manager.md):
Pi now installs a curated extension set at configure time. Everything else in
this decision still stands.)*

**7. Devgeta validates Node.js ≥ 22.19 and npm before running the script.**
Terminal apps install before the language phase, so a clean-machine full
install reaches pi before any Node.js exists. The app's own preflight —
matching the script's — fails before anything is downloaded, with an error that
names the fix (`dg install --only languages`, then re-run). Full category
reordering is a separate, larger decision and was not taken here.

## Notes for reviewers

On the §4 security rule: this is the same channel decision ADR-0047 already
made for opencode, and it applies here for the same reasons — the script is the
official installer, it runs under `sh` per its own shebang, and it cannot run
without a compatible Node.js runtime it verifies itself. The npm package it
installs (`@earendil-works/pi-coding-agent`) is fetched through the npm
registry's own tarball integrity checks, so the artifact that actually lands on
the machine is the one npm verifies, not the unverified script. A
checksum-pinned alternative was considered and rejected: it would fork Pi's
install channel for no gain over the parity opencode already established.
