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

## Configuration — a curated extension set, and nothing else

Configuring Pi means installing devgeta's extension set. `dg configure pi` —
and the configure step of `dg install` — runs Pi's own package manager once per
extension, `pi install npm:<name>`:

| Extension                           | What it adds                                                      |
| ----------------------------------- | ----------------------------------------------------------------- |
| `npm:pi-subagents`                  | Subagent delegation and scripted multi-agent workflows            |
| `npm:pi-web-access`                 | Web search, URL/PDF/YouTube reading, repo cloning                 |
| `npm:pi-schedule-prompt`            | Cron-like recurring and one-shot prompts                          |
| `npm:pi-powerline-footer`           | Powerline-style status bar                                        |
| `npm:pi-openrouter-advanced-provider` | Live model catalog, per-model provider routing                  |
| `npm:pi-openrouter-request-details` | Which provider/model served each request, with cost cards         |
| `npm:pi-stalled-agent-recovery`     | Retries empty or thinking-only runs, breaks failing-tool loops    |
| `npm:pi-thinking-reformatter`       | Cleans up deepseek-style thinking whitespace                      |

The two OpenRouter extensions only matter if you use OpenRouter as your
provider; the rest are provider-agnostic. The list lives in
`internal/apps/pi/pi.go` (`piExtensions`) and is documented here — adding one is
a normal code change.

For a manual setup outside devgeta, run each install directly:

```bash
pi install npm:pi-subagents
pi install npm:pi-web-access
pi install npm:pi-schedule-prompt
pi install npm:pi-powerline-footer
pi install npm:pi-openrouter-advanced-provider
pi install npm:pi-openrouter-request-details
pi install npm:pi-stalled-agent-recovery
pi install npm:pi-thinking-reformatter
```

**Pi owns the install.** Devgeta never writes Pi's settings file and never
touches Pi's `npm` tree: `pi install` records each source in the former and
fetches the package into the latter. Versions are whatever npm serves at
configure time — the set is deliberately unpinned, so `pi update` can advance
it. See ADR-0060 decision 8.

The settings file is `<agent dir>/settings.json`, where the agent dir is
`$PI_CODING_AGENT_DIR` if you set it (a leading `~` is expanded) and
`~/.pi/agent` otherwise — the same directory Pi writes to.

Re-running:

- `dg configure pi` installs only extensions Pi's settings do not already
  declare, including declarations with a version or tag.
- `dg configure pi --force` re-runs every install, preserving existing version
  or tag specifications. Unpinned extensions can pick up newer versions.
- Each install prints its progress and result.
- If an install fails, the run removes the extensions it added during that run
  (newest first) so no half-set is left behind. Extensions it merely refreshed
  over an existing choice are left alone — their prior version cannot be
  recovered, so the run never pretends to restore them. A rollback step that
  fails does not stop the rest, and every failure is named with the manual
  `pi remove <package>` fix.
- A settings file devgeta cannot parse stops configure with the file named. It
is never treated as empty — that would re-install over your file.

To drop one, use Pi directly: `pi remove <source>`, and `pi list` to see the
current set. `dg configure pi` will add it back.

Everything else stays out of scope: no themes, no shared skills, no permission
or formatter parity with claude/opencode, no `dg worktree`/`dg task` wiring
(ADR-0059 decision 6). The only other state is the entry in
`global_config.yaml`'s installed list, written only after the extensions
install successfully.

## Uninstall

`dg uninstall pi` removes **only** `~/.pi/agent/install` — the managed install
root. `~/.pi/agent` itself holds Pi's config, sessions, installed extensions and
agent state and is never touched, so the extension set survives an uninstall.
Note that the `pi` binary is a symlink into the managed root, so it disappears
with it; PATH entries the script added to your shell profile are left alone.
