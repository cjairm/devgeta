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

| Extension          | What it adds                                           |
| ------------------ | ------------------------------------------------------ |
| `npm:pi-subagents` | Subagent delegation and scripted multi-agent workflows |

The list lives in `internal/apps/pi/pi.go` (`piExtensions`) and is mirrored
here by a test.

### pi-subagents

It works with no configuration: its built-in agents (`scout`, `worker`,
`reviewer`, `oracle`, `delegate`, …) use your current Pi model, so nothing
depends on a provider you have not set up. Run `/subagents-doctor` if
delegation misbehaves. Devgeta writes no configuration for it.

One setting is worth adding yourself if you have more than one provider signed
in. The parent model may pick a model for a child on its own, and Pi matches the
name against any provider that has a key — one user's local model delegated to
a paid model this way
([pi-subagents#3203](https://github.com/nicobailon/pi-subagents/issues/3203)).
This limits children to the parent's model, or to your `/scoped-models` set
when you have one, in `~/.pi/agent/settings.json`:

```json
{
  "subagents": {
    "modelScope": { "enforce": true, "strict": true, "allow": ["scoped"] }
  }
}
```

With this on, the bundled `claude-code` and `claude-code-writer` agents must
name a model, or they refuse to start. To route roles to different models on purpose, set
`subagents.agentOverrides.<agent>.model` and add those models to `allow`. The
upstream docs (`/subagents-guide models`) describe a tiering that works well:
a cheap fast model for `scout`, a mid-tier one for `worker` and `reviewer`, and
a top reasoning model only for `oracle`.

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
