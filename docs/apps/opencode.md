# OpenCode

Devgeta installs and configures [OpenCode](https://opencode.ai/docs), a
terminal-based AI code editor, and deploys a curated config to
`~/.config/opencode/`.

- **Module:** `internal/apps/opencode/`
- **Config source:** `configs/opencode/`
- **Install:** OpenCode 2's official script (`https://opencode.ai/v2/install`),
  pinned to `constants.PinnedVersion.OpenCode` (2.2.6)

## Install channel

devgeta runs OpenCode's official install script on both macOS and Linux; it is
the only channel OpenCode supports. Since OpenCode 2 that script is
`https://opencode.ai/v2/install`: the unversioned `https://opencode.ai/install`
installs only OpenCode 1 and refuses a 2.x version. The script puts everything
under `~/.opencode`, with the binary at `~/.opencode/bin/opencode`; that
location is hardcoded upstream and has no override.

Because nothing else adds that directory to your PATH, devgeta's generated
`devgeta.zsh` prepends it. Everything else follows from the same path:

- `dg install` installs when `~/.opencode/bin/opencode` is missing, or when its
  `--version` is older than the pin. An OpenCode 1 install is upgraded this
  way. A newer version you installed yourself is left alone, and so is an
  `opencode` that resolves on your PATH from somewhere else.
- `dg uninstall opencode` removes `~/.opencode` and `~/.config/opencode`. No
  package manager is involved, so on macOS a copy from an older
  `brew install opencode` is left behind — see
  [the migration note](../migrations/opencode-install-channel.md).

Why this channel, and the security trade it accepts:
[ADR-0047](../decisions/ADR-0047-opencode-installs-from-its-official-script-on-every-platform.md).

### Pinned version

> **Upgrading from OpenCode 1,** or from a devgeta release before the pins:
> follow [docs/migrations/opencode-2.md](../migrations/opencode-2.md).

devgeta installs exactly `constants.PinnedVersion.OpenCode` (the script's
`--version` flag) and writes `"update": "disable"` into `opencode.json`, so
OpenCode does not replace itself with a release devgeta's config was never
checked against. OpenCode 2 is the reason: it silently ignores several 1.x
config keys (the theme, the permission format, plugins), so a self-update
across such a change turns protections off without an error.

Moving the pin is a devgeta change: bump the constant, read the release notes
against what devgeta writes, check it in a sandbox `HOME` (below), and release.
To run a different version yourself, install it with the script and devgeta
leaves a newer one alone:

```bash
curl -fsSL https://opencode.ai/v2/install | bash -s -- --version <version>
```

### Checking a version without touching your setup

Everything devgeta writes follows `HOME` and the `XDG_*` variables, and
`--standalone` keeps OpenCode off the shared background service, so a version
can be tried in a throwaway directory:

```bash
SB=$(mktemp -d)
env HOME=$SB XDG_CONFIG_HOME=$SB/.config XDG_DATA_HOME=$SB/.local/share \
  XDG_STATE_HOME=$SB/.local/state XDG_CACHE_HOME=$SB/.cache \
  sh -c 'dg configure opencode --force && opencode debug config --standalone'
```

## After Installation

**Start OpenCode:**

```bash
opencode
```

**View configuration:**

```bash
ls ~/.config/opencode/
```

---

## Provider

Use **[OpenRouter](https://openrouter.ai)** — one API key, access to all models below.

```bash
export OPENROUTER_API_KEY="your-key-here"
```

---

## Models

OpenCode pins no model of its own — devgeta ships no default here, so these are
suggestions, not configuration. Pick one per role and set it in OpenCode.

| Role                       | Family                    | Fallback, different lineage | When to reach for it                                    |
| -------------------------- | ------------------------- | --------------------------- | ------------------------------------------------------- |
| Daily coding (default)     | `anthropic/claude-opus`   | `openai/gpt-5.6-luna`       | Everyday coding, bug fixes, PR reviews                  |
| Hardest reasoning          | `anthropic/claude-fable`  | OpenAI's reasoning line     | When nothing else solves it                             |
| Agents + large repos       | `moonshotai/kimi`         | `deepseek/deepseek-v4-pro`  | Large codebases, multi-file refactors, long agent runs  |
| Deep review + architecture | `z-ai/glm`                | `openai/gpt-5.6-luna`       | Architecture decisions, hard debugging, critical review |
| Cheap bulk tasks           | `deepseek/deepseek-flash` | `z-ai/glm-5.3-flash`        | Background or non-critical automation                   |

**Fallbacks verified against OpenRouter on 2026-09-19 — re-check before
trusting one.** The left column names families on purpose, because point
releases land every few weeks and a version pinned in prose is wrong long
before anyone notices. The fallback column has to name specific models to be
useful, so it carries a date instead; treat an old date as a reason to look
rather than a recommendation. Resolve every current slug, price, and context
window at [openrouter.ai/models](https://openrouter.ai/models) — all three
move.

Luna appears twice on purpose: it is the only pick here from a lineage that
differs from **both** Anthropic and the Chinese open-weight cluster, which is
exactly what you want in the reviewer slot.

Two caveats on picking from a leaderboard. OpenRouter's programming collection
ranks by **tokens processed over a trailing week**, which measures adoption, not
quality — a cheap model doing bulk work outranks a better one used sparingly.
And third-party benchmark numbers in blog roundups are frequently unsourced;
check the model's own page before believing one.

### Choosing a reviewer

A second reviewer is only worth paying for if it fails differently from your
daily driver, so pick it from a **different lineage** than whatever fills the
daily-coding row. A model that misses what yours misses is a rubber stamp.

Reviewing models default to agreeable. Give a reviewer a rubric that forces a
verdict — "list at least three concrete defects with file/line references; if
you find none, state exactly what you checked" — or you get praise instead of
review.

## Fallbacks

Wire the fallback column above into **OpenRouter's own failover** rather than
switching models by hand: pass a `models` array on the request and it moves on
when your first choice is down, rate-limited, or erroring. Current request
shape is in [OpenRouter's docs](https://openrouter.ai/docs).

The different-lineage rule from the reviewer section applies here too, for a
different reason: a fallback sharing a provider with its primary is taken out
by the same outage, the same price change, and the same bad release. That is
why the column crosses lineages on every row rather than falling back to a
sibling model.

Two tiers are worth having underneath that:

- **Open weights** — Alibaba's Qwen (the Coder line) and Mistral both publish
  weights, so they can run locally through Ollama. That is the only tier that
  still works when OpenRouter itself is what's down, and nobody can deprecate or
  reprice it out from under you.
- **Free tier** — OpenRouter carries a rotating set of no-cost models, some with
  very large context windows. Useful as a zero-cost floor for bulk work, but
  check the context window before committing: some free variants are capped far
  below their paid siblings and are too small for repo-scale tasks.

---

## Configuration

`dg configure opencode` writes, in `~/.config/opencode/`:

| File                                  | What                                                                                                        |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `opencode.json`                       | rendered from `configs/opencode/opencode.json.tmpl`: agents, permissions, formatters, `"update": "disable"` |
| `themes/<theme>.json`                 | the current devgeta theme, rendered from its palette                                                        |
| `cli.json` → `theme.name`             | selects that theme; the only key devgeta writes in this file                                                |
| `plugins/devgeta-agent-config-guard/` | server plugin, below                                                                                        |
| `plugins/devgeta-notify/`             | terminal (TUI) plugin, below                                                                                |

`opencode.json` uses OpenCode 2's native keys (`agents`, an ordered
`permissions` list), not the 1.x keys OpenCode 2 still accepts through a
compatibility layer. A key OpenCode stops reading would otherwise fail
silently.

`cli.json` is shared: OpenCode keeps your TUI preferences (diffs, sidebar,
keybinds) there. devgeta sets `theme.name` and keeps everything else, the same
way it treats Pi's `settings.json`. OpenCode 2 ignores a `theme` key in
`opencode.json`.

### Permissions

The same floor as Claude Code ([ADR-0067](../decisions/ADR-0067-agent-permission-floor.md)),
written as an ordered list where **the last matching rule wins**: each action
starts with `*` → allow, then the deny and ask exceptions follow.
`internal/apps/opencode/permissions_test.go` fails the build if the two coders
drift, or if a catch-all moves anywhere but first.

OpenCode 2 allows reaching paths outside the project by default (1.x asked).
To keep the floor, `external_directory` starts with `*` → ask, followed by the
scratch-directory grant (ADR-0015).

How OpenCode 2 matches the patterns (`*` crosses `/`, `~` and `$HOME` expand
for read, edit and external_directory): see
[agent-permission-matching.md](../guides/agent-permission-matching.md).

---

## Plugins

Each devgeta plugin is a folder under `~/.config/opencode/plugins/`, which
OpenCode 2 discovers by itself. A folder's `server.js` runs inside OpenCode's
server; its `tui.js` runs inside each terminal client. Both import nothing
outside Node's standard library, so OpenCode installs no dependency for them.

**OpenCode 2 runs one shared background server** for every terminal
(`opencode serve --service`) unless you start it with `--standalone`. Server
plugins run inside that service, with the environment of whichever terminal
started it. That decides where each plugin lives.

### Agent-config guard (`devgeta-agent-config-guard/server.js`)

Denies an edit to an agent's own configuration; the same four-clause rule as
Claude Code's `agent-config-guard.sh` (see [claude.md](claude.md) and
[ADR-0014](../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)).
It hooks OpenCode's permission evaluation and sets the effect to `deny` with a
message. The edit, write and patch tools all ask for the `edit` action with
every file they would touch, so a patch that edits several files is denied if
any one of them is protected.

**Bypass:** `DEVGETA_SKIP_AGENT_CONFIG_GUARD=1` in the server's environment.
With the shared service that is not your shell, but the service:

```bash
opencode service set env DEVGETA_SKIP_AGENT_CONFIG_GUARD 1
opencode service restart
# undo: opencode service unset env DEVGETA_SKIP_AGENT_CONFIG_GUARD && opencode service restart
```

With `opencode --standalone`, exporting it in the launching shell works as it
does for Claude Code.

### Activity state (`devgeta-notify/tui.js`)

Reports this coder's activity into the tmux pane it is running in — working /
finished / blocked / errored — so `dg ws`'s status dot, its agents section
(ADR-0056), and tmux's status bar can show it without switching windows.

It is a TUI plugin because the terminal client is the process in your pane:
its `TMUX_PANE` is that pane. A server plugin would see the service's
environment and write every session's state into one pane. The service still
sends every session's events to every client, so the plugin only acts on the
session its own client is showing (and that session's subagents). Tabs do not
count: OpenCode syncs tabs across every client and keeps old sessions in them.

| Event                                                                    | Writes                                                            |
| ------------------------------------------------------------------------ | ----------------------------------------------------------------- |
| plugin load                                                              | `@dg_agent_kind opencode` only                                    |
| `session.execution.started`                                              | `busy`                                                            |
| `session.execution.succeeded` / `.interrupted`                           | `idle`                                                            |
| `session.execution.failed`                                               | `error`                                                           |
| `permission.asked` / `form.created`                                      | `blocked` (subagents count too)                                   |
| `permission.replied`, `form.replied`, `session.tool.success` / `.failed` | `busy`, while the session is still running and not already `busy` |
| client quits (plugin cleanup)                                            | unsets kind and state, recomputes the mirror                      |

Execution events count only for the root session: a subagent finishing is not
the turn finishing. Handlers run one at a time, in arrival order, so a slow
handler cannot overwrite the state of a later event. Without that, a rejected
prompt's `session.tool.failed` used to land its `busy` after the
`session.execution.interrupted` that followed it.

It writes via `tmux set-option -p -t "$TMUX_PANE" @dg_agent_state <value>`,
plus a window-level mirror (`@dg_window_agent_state`) that tmux's status bar
reads to flag a window nobody is looking at, and the opt-in sound
([ADR-0009](../decisions/ADR-0009-audible-agent-notifications.md)). Outside
tmux it loads nothing.

**Identity, separately from state ([ADR-0055](../decisions/ADR-0055-an-agent-says-what-it-is.md)).**
Every state write also stamps `@dg_agent_kind opencode`, folded into the same
exec call via a literal `;` argv element, which is what lets `dg ws` list a
coder that hasn't prompted yet. The plugin's cleanup runs when the client
quits (checked on OpenCode 2.2.6 with its double ctrl-c), so the pane is
cleared reliably, which OpenCode 1 never did. A process-exit fallback stays as
a backstop for a client killed without cleanup, and the dashboard's
shell-name backstop keeps the agents list right either way.

See [ADR-0005](../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md)
for the full design.

### `--force` and what it leaves alone

`--force` clears only what devgeta generates: `opencode.json`, `themes/`,
devgeta's two folders under `plugins/`, and `plugin/` (singular), where
devgeta put its OpenCode 1 plugins. Your `cli.json` settings, your `skills/`,
`commands/` and `agents/`, other tools' folders in `plugins/`, an OpenCode 1
`node_modules`, and anything else in there are left alone.

OpenCode 1 needed a dependency lockfile to start quickly
([ADR-0049](../decisions/ADR-0049-opencode-plugins-ship-with-a-lockfile.md)).
OpenCode 2 does not resolve those dependencies (startup measured at 0.41s with
or without a leftover 1.x `package.json`), and devgeta's plugins have none, so
devgeta no longer generates one.

### Agent packs

[Agent packs](agent-packs.md) reach OpenCode through two keys in `opencode.json`:
`plugins`, an absolute folder path per pack (the folder holding the plugin's
`index.js`: Superpowers' pack root, Ponytail's `.opencode/plugins/`), and
`instructions`, which is how the `devgeta` pack's `DEVGETA.md` is loaded. The
`devgeta` pack's skills load through a third key, `skills`: the absolute path of
its `skills/` folder, listed only while at least one skill is left in it.

`opencode.json` is rendered from a template, and the template renders both keys
from `agent_packs:` in `global_config.yaml`. So `dg configure opencode --force`
keeps your packs. `dg agent-pack install` and `uninstall` re-render
`opencode.json` the same way (only that file: not the theme, `cli.json` or
devgeta's plugin folders), so a hand edit to `opencode.json` is lost on any of
these, as it is on `--force`.

Commands you run to look at OpenCode from a script or a sandbox, such as
`opencode debug config`, need `--standalone`: without it OpenCode 2 tries the
shared background service first, which may be yours. `dg agent-pack` passes it
when it checks for another copy of a pack.

Ponytail's `PONYTAIL_DEFAULT_MODE` is read from the service's environment, like
the guard's bypass above; see [Ponytail in agent-packs.md](agent-packs.md#ponytail).
