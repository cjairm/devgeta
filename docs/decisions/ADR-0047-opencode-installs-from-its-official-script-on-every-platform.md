# ADR-0047 — opencode installs from its official script on every platform

**Date:** 2026-09-16
**Status:** ACCEPTED

## Context

Devgeta installed opencode two different ways depending on the platform:

- **macOS:** `brew install opencode`, via the generic
  `MacOSCommand.InstallPackage` path (`internal/apps/opencode/opencode.go:57`).
- **Debian:** the upstream install script at `https://opencode.ai/install`, via
  `InstallScriptStrategy` (`internal/commands/debian.go:113-117`).

Neither channel selects a version; both take whatever upstream shipped last.

The asymmetry surfaced as an outage. opencode 1.18.30 crashes while building its
system prompt, before any request reaches a model:

```
TypeError: undefined is not an object (evaluating 'a.name')
  at resolve (chunk-36bwgd4p.js)
  at SystemPrompt.environment
  at SessionPrompt.run → SessionPrompt.loop
```

Every `dg task review-run` reviewer returned
`ERROR(Unexpected server error. Check server logs for details.)` on every model.
It reproduces in an empty directory with `~/.config/opencode` moved aside, so it
is not config, plugins, skills or the project. The cause was only visible in
`~/.local/share/opencode/log/opencode.log`.

On macOS the Homebrew formula offers only `stable`, with no pin and no
downgrade, so recovery meant leaving devgeta's channel entirely
(`npm i -g opencode-ai@1.18.20`) — a manual step every user would have to
discover alone, silently reverted by the next `brew upgrade` or `dg install`.

Investigating the fix exposed three further defects in the existing Debian
script path, all of which a macOS switch would have inherited:

1. **Wrong interpreter.** `RunInstallScript` executes the script with `sh`
   (`internal/commands/debian_strategies.go:216-219`). The script's shebang is
   `#!/usr/bin/env bash`, and on Debian `sh` is dash.
2. **Never idempotent.** `MaybeInstallPackage("opencode")` asks
   `IsPackageInstalled`, which reads `dpkg -l` (`internal/commands/debian.go:231`).
   A script-installed binary is never in dpkg, so `dg install` re-ran the
   installer on every run — against product principle 2.
3. **Uninstall is a no-op.** `UninstallPackage` runs `apt-get remove -y opencode`
   (`internal/commands/debian.go:63`), which cannot remove `~/.opencode`.

## Decision

**1. Both platforms install opencode by running the official install script with
`bash`.** The install logic moves out of the per-platform package-manager
dispatch and into the app itself, where it needs no platform branch at all. The
`case constants.OpenCode` entry is removed from the Debian strategy switch.

**2. Detection, idempotency and uninstall key on the binary, not on a package
manager.** The script installs to `$HOME/.opencode/bin` — hardcoded, with no
override env var. `SoftInstall` skips when that binary is present, and
`Uninstall` removes `~/.opencode`. This is the shape rtk's Debian branch already
uses (`internal/apps/rtk/rtk.go:76-82`).

**3. Devgeta owns `~/.opencode/bin` on PATH.** The script edits the user's own
rc files, which devgeta does not own and cannot keep consistent. The path is
added to the generated shell config alongside the existing `oc` alias, which
resolves `opencode` through PATH (`pkg/constants/coder_launch.go:76-79`).

**4. No version pin: devgeta tracks whatever the script installs.** Decided by
the maintainer after the trade below was put in writing.

**5. This knowingly amends ADR-0004 decision 3 for opencode.** That ADR chose a
checksum-verified GitHub release binary for rtk _specifically_ to avoid piping an
upstream install script into a shell, citing the §4 security rule against
executing downloaded code without verification. opencode's script is not
verified — it downloads and extracts a release archive with no hash or signature
check — and `https://opencode.ai/install` 307-redirects to
`raw.githubusercontent.com/anomalyco/opencode/refs/heads/dev/install`, a moving
development branch head rather than a tagged release. So what devgeta executes at
install time is whatever is on that branch at that moment.

This is a real and accepted reduction in the §4 guarantee, taken because the
script is opencode's only supported install channel, because devgeta already ran
it on Debian, and because the maintainer confirmed it as the fix. Two properties
limit it, and neither is verification: `RunInstallScript` stages the script to a
private temp file before running it, so a truncated or failed download is a
failed install rather than a half-executed one; and the fetch is HTTPS.

rtk, lazygit and lazydocker keep `InstallGitHubBinary` and its mandatory SHA-256
check. This ADR does not relax the rule for them, and does not make install
scripts the default for future apps — a verified release binary remains the
preferred channel, and any further exception needs its own ADR.

## Consequences

- opencode behaves identically on macOS and Debian (product principle 3), which
  it did not before.
- `dg install` stops reinstalling opencode on every run, and
  `dg uninstall opencode` actually removes it — on Debian these were broken.
- A macOS user with a brew-installed opencode keeps it until they reinstall;
  `dg uninstall opencode` no longer calls `brew uninstall`, so a brew copy is
  left behind. This needs a migration note.
- Because nothing is pinned, an upstream release that crashes on startup — the
  1.18.30 case above — reaches every user on their next `dg install`, and
  devgeta offers no way to hold back or roll back. Recovery remains manual:
  rerun the script with `VERSION=<good>` or `--version <good>`, both of which it
  supports. This is the specific trade accepted in decision 4; revisiting it
  means adding a pin, not changing the channel.
- devgeta now executes a script from a development branch head on every fresh
  opencode install, unverified. That is the §4 cost recorded in decision 5.
