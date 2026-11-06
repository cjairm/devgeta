# Pi

Pi (`@earendil-works/pi-coding-agent`) is a terminal AI-coding harness. Devgeta
installs it as a terminal tool and registers it in `global_config.yaml`.

## Install

`dg install` (terminal category) or `dg install --only pi`.

Devgeta runs Pi's official installer (`https://pi.dev/install.sh`) under `sh`
— its shebang is `#!/bin/sh`. Like opencode, the script is staged through a
download-then-execute guard so a broken download cannot be reported as a
successful install (ADR-0059).

Idempotency:

- Skips if the script's marker (`~/.pi/agent/install/managed-install.json`)
  exists.
- Skips if `pi` already resolves on PATH (installed by other means — npm, your
  own setup): devgeta never lays a second copy over it.

Pi needs Node.js ≥ 22.19 and npm. The script checks this itself and fails with
instructions if they are missing. Terminal tools install before languages, so on
a clean machine the first `dg install` may report that; run
`dg install --only languages`, then `dg install --only pi`.

## Configuration — a curated extension set, and nothing else

`dg configure pi` (and the configure step of `dg install`) runs Pi's own package
manager once per extension, `pi install npm:<name>`:

| Extension                             | What it adds                                                   |
| ------------------------------------- | -------------------------------------------------------------- |
| `npm:pi-subagents`                    | Subagent delegation and scripted multi-agent workflows         |
| `npm:pi-web-access`                   | Web search, URL/PDF/YouTube reading, repo cloning              |
| `npm:pi-schedule-prompt`              | Cron-like recurring and one-shot prompts                       |
| `npm:pi-powerline-footer`             | Powerline-style status bar                                     |
| `npm:pi-openrouter-advanced-provider` | Live model catalog, per-model provider routing                 |
| `npm:pi-openrouter-request-details`   | Which provider/model served each request, with cost cards      |
| `npm:pi-stalled-agent-recovery`       | Retries empty or thinking-only runs, breaks failing-tool loops |
| `npm:pi-thinking-reformatter`         | Cleans up deepseek-style thinking whitespace                   |

The two OpenRouter extensions only matter if you use OpenRouter; the rest are
provider-agnostic. The list lives in `internal/apps/pi/pi.go` (`piExtensions`)
and is mirrored here by a test.

**Pi owns the install.** Devgeta never writes Pi's settings file or touches its
npm tree. Versions are whatever npm serves at install time, and `pi update`
advances them (ADR-0060).

Re-running:

- `dg configure pi` installs only extensions `~/.pi/agent/settings.json` does
  not already declare — a declaration with a version or tag counts. `--force`
  does the same: there is nothing of devgeta's to overwrite, and refreshing
  installed extensions is `pi update`.
- If an install fails, configure stops and names the extension. Nothing is
  rolled back and pi is not registered as configured; fix the cause and re-run
  to install what is still missing.
- A settings file devgeta cannot parse stops configure with the file named; it
  is never treated as empty.

Configure runs `pi` from your PATH, then falls back to the launcher the script
writes at `~/.pi/agent/bin/pi`, so a configure right after an install works
without a shell reload.

To drop one extension, use Pi directly: `pi remove <source>`. `pi list` shows
the current set; `dg configure pi` will add it back.

Everything else stays out of scope: no themes, no shared skills, no permission
or formatter parity with claude/opencode, no `dg worktree`/`dg task` wiring
(ADR-0059).

## Uninstall

`dg uninstall pi` removes **only** `~/.pi/agent/install`. `~/.pi/agent` itself
holds Pi's config, sessions, installed extensions and agent state and is never
touched. The `pi` binary is a symlink into the install root, so it disappears
with it; PATH entries the script added to your shell profile are left alone.
