# Keeping every AI coder in sync

Devgeta configures three AI coding agents ("coders"): Claude Code, OpenCode and
Pi. They must behave the same wherever each one can. A change to a concern goes
into **every coder that can express it**, in each one's own format. A coder that
can't is listed under [Accepted differences](#accepted-differences), never
skipped silently.

The hard rule lives in [CLAUDE.md §12](../../CLAUDE.md) ("Keeping every AI coder
in sync"); this guide is the detail behind it: where each concern lives, what
the enforcing tests do and do not check, and which differences are deliberate.

---

## Where each concern lives

| Concern                                                                               | Claude Code                                                                                                           | OpenCode                                                                                                 | Pi                                                                                 |
| ------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Permissions                                                                           | `configs/claude/settings.json.tmpl`                                                                                   | `configs/opencode/opencode.json.tmpl`                                                                    | None, see below                                                                    |
| Formatting on save                                                                    | `configs/claude/format.sh` (wraps `configs/devgeta/format-file.sh`)                                                   | `formatter` block in `configs/opencode/opencode.json.tmpl`                                               | `configs/pi/extensions/devgeta-format.js` (calls `configs/devgeta/format-file.sh`) |
| Agent activity state                                                                  | `configs/claude/agent-state.sh`                                                                                       | `configs/opencode/plugins/devgeta-notify/tui.js` (TUI plugin)                                            | None, see below                                                                    |
| Agent-config protection                                                               | `configs/claude/agent-config-guard.sh`                                                                                | `configs/opencode/plugins/devgeta-agent-config-guard/server.js` (server plugin)                          | None, see below                                                                    |
| Scratch dir grant                                                                     | `additionalDirectories` in `configs/claude/settings.json.tmpl`                                                        | `external_directory` in `configs/opencode/opencode.json.tmpl`                                            | None, see below                                                                    |
| Shared instructions (`DEVGETA.md`)                                                    | an `@DEVGETA.md` line in `~/.claude/CLAUDE.md`, added by the `devgeta` pack (`internal/tooling/agentpack/devgeta.go`) | an `instructions` entry in `opencode.json`, rendered from state by `configs/opencode/opencode.json.tmpl` | `configs/agent-packs/devgeta/extensions/devgeta-instructions.js`                   |
| Devgeta skills (`review`, `receiving-code-review` and `using-git-worktrees` wrappers) | a plugin in the `devgeta` marketplace: `configs/agent-packs/devgeta/.claude-plugin/plugin.json`                       | a `skills` entry in `opencode.json`, rendered from state by `configs/opencode/opencode.json.tmpl`        | the `pi.skills` entry in `configs/agent-packs/devgeta/package.json`                |
| Agent packs (skills, hooks, commands)                                                 | a plugin in the `devgeta` marketplace, enabled by `configs/claude/settings.json.tmpl` from state                      | a `plugins` entry in `opencode.json`, rendered from state by `configs/opencode/opencode.json.tmpl`       | `pi install <built tree>` (Pi's own package manager)                               |

Nothing in this table is a place to put devgeta policy: it all ships to every
user. A devgeta rule belongs in CLAUDE.md or a guide — see CLAUDE.md §12,
"Anything we ship is built for strangers".

## Rules

- **Never add a deny/ask rule, or a formatter language, to one coder only when
  the others can express it.** Claude Code's and OpenCode's permission sets must
  stay the same rule for rule, and every coder's formatter must cover the same
  file extensions (Claude Code and Pi share one script, so they cannot differ).
- `internal/apps/opencode/permissions_test.go` enforces the Claude Code and
  OpenCode comparison and fails the build on any asymmetry, in either direction.
  It is the reason the lists cannot drift again — do not weaken it to land a
  one-sided change. If a rule genuinely cannot be expressed in one of the two,
  drop it from both.
- `internal/apps/opencode/coder_parity_test.go` enforces the table above: every
  coder has an entry for every concern, every file it names exists, and a
  concern a coder has no equivalent for is listed under Accepted differences
  (and a listed gap is still a gap in the table). Pi joins every comparison where
  it has an equivalent; where it has none, this is the check that says so.
- **Add a coder, a concern or a gap here and in the table together.** The test
  fails if they disagree.
- **That test compares pattern STRINGS, not what they match.** A rule can be
  present in both configs, identical, and still enforce nothing — on OpenCode 1
  the `~/`-anchored rules were inert, because it matched paths project-relative
  (OpenCode 2 documents `~` expansion; not yet re-probed). Read
  [agent-permission-matching.md](agent-permission-matching.md) before adding or
  re-anchoring a rule; symmetry on paper is not symmetry in effect.
- **The order of OpenCode's rules matters; Claude's does not.** OpenCode 2's
  `permissions` list is last-match-wins, so each action's `*` catch-all comes
  first and the exceptions after it. The test enforces that too.
- Deploy every coder you changed: `dg configure claude --force`,
  `dg configure opencode --force`, `dg configure pi --force`. Agent packs reach
  them through `dg agent-pack`, and the config files above keep their entries
  across `--force` (they are rendered from `agent_packs:` state).

## Accepted differences

Deliberate — do not "fix" these by halves.

- **Permissions: Pi has none.** Pi does not ask before tool calls (its own
  documentation says to review changes and use a sandbox for unattended work),
  so there is no permission list to keep in step. Claude Code and OpenCode stay
  rule for rule the same.
- **Agent activity state: Pi has none.** devgeta ships no activity-state
  integration for Pi, so a Pi pane is not reported busy, idle or blocked to the
  tmux dashboard the way a Claude Code or OpenCode pane is.
- **Agent-config protection: Pi has none.** devgeta ships no guard for Pi, so
  nothing stops Pi editing its own configuration.
- **Scratch dir grant: Pi has none.** Pi has no permission system to grant a
  directory in.
- **The lint feedback loop is Claude Code and Pi only.** `format.sh` returns linter findings
  via `hookSpecificOutput.additionalContext` and Pi's `devgeta-format.js` appends
  them to the tool result, both from the shared `format-file.sh`; OpenCode's `formatter` block cannot
  return context, and OpenCode surfaces LSP diagnostics instead.
- **`statusLine` has no OpenCode or Pi equivalent.**
- **OpenCode's activity plugin runs in the terminal client, the guard in the
  server.** OpenCode 2 runs one shared background service per machine, and
  server plugins see that service's environment, not the pane's. So the notify
  plugin is a TUI plugin (its `TMUX_PANE` is its own pane) that acts only on the
  session its client shows, and the guard's bypass variable has to be set on
  the service (`opencode service set env …`). Claude Code's hooks run per
  session, so neither difference exists there.
- **"Agent ends" fires on both, through different mechanisms.** Claude Code's
  `SessionEnd` hook and OpenCode's TUI-plugin cleanup both unset
  `@dg_agent_kind`/`@dg_agent_state` and recompute the window mirror
  ([ADR-0055](../decisions/ADR-0055-an-agent-says-what-it-is.md)). OpenCode 1's
  `server.instance.disposed` never fired on quit; the 2.x cleanup does (checked
  on 2.2.6). ADR-0055's shell-name backstop keeps the dashboard right either
  way.
