# ADR-0061 — Pi is a themed surface, with one devgeta-owned theme file

**Date:** 2026-11-06
**Status:** ACCEPTED

## Context

Every other coder and terminal devgeta configures follows `dg theme`
(ADR-0043). Pi does not: ADR-0059 §7 and ADR-0060 §8 put themes out of scope,
and ADR-0060 §1 says devgeta never writes Pi's settings. Pi is the odd surface
in a setup that is meant to look like one thing, and the next step
(`pi-powerline-footer`) colors its status line from Pi's active theme, so it
needs a devgeta theme to be active first.

How Pi themes work (from <https://pi.dev/docs/latest/themes> and
`https://pi.dev/schemas/theme.schema.json`, read 2026-11-06):

- A theme is a JSON file. `name` and `colors` are required, and `colors` must
  carry all 51 required roles; extra keys anywhere are rejected. `vars` holds
  reusable values. A value may be hex, OKLCH/OKHSL, a 256-color index, a `vars`
  reference, or `""` (the terminal's default color).
- User themes load from `~/.pi/agent/themes/<name>.json`. The **active** theme
  in that directory is hot-reloaded: rewriting the file restyles a running Pi.
- The active theme is the `theme` key in `~/.pi/agent/settings.json` — the
  same file Pi writes `packages` into (ADR-0060). Built-ins are `system`
  (default), `dark`, `light`; `system` is reserved.

## Decision

**1. Pi becomes the eighth themed surface.** It implements
`apps.ThemedConfigurer`, joins `cmd/theme.go`'s `themedApps`, `theme.Manifest()`
and `surfaceGroup`, and reads the `colors:` group, like OpenCode and Claude.

**2. One theme file with a fixed name: `~/.pi/agent/themes/devgeta.json`,
`"name": "devgeta"`.** The file's content follows `current_theme`; its name
never changes. Rendered from `configs/pi/themes/devgeta.json.tmpl`, which maps
palette roles to Pi's roles the way `configs/opencode/themes/default.json.tmpl`
does.

Why a fixed name rather than one file per devgeta theme (OpenCode's pattern):
`~/.pi/agent/themes/` is a directory the user puts their own themes in, so
devgeta may own exactly one known path there, never the directory. A fixed
name also means `dg theme set` only rewrites that file — Pi's settings are not
touched on a switch, and Pi's hot reload applies the new colors to a running
session with no extra live-apply step.

**3. Devgeta writes exactly one key in Pi's settings: `theme`. This amends
ADR-0060 §1.** _(Amended by [ADR-0064](ADR-0064-pi-hides-thinking-blocks-by-default.md):
devgeta also writes `hideThinkingBlock`, under the same rules.)_ It is a read-modify-write that keeps every other key
(`packages` above all) and is written atomically (`files.WriteFileAtomic`).
A file that does not parse is an error, never a reset (same rule as ADR-0060
§5). A missing file is created with only that key.

- `dg configure pi` (soft) sets `"theme": "devgeta"` only when no `theme` key
  exists. A user who already picked a Pi theme keeps it (principle 4).
- `dg configure pi --force` sets it unconditionally — `--force` is the user
  asking for devgeta's configuration.
- `dg theme set` never writes settings (point 2), so a user who chose another
  Pi theme is not overridden on every switch.

**4. Transparency.** Pi has no page background to clear, but it paints block
backgrounds behind user messages and tool output. Those map to the palette's
element/diff background roles (the same compromise OpenCode's
`backgroundElement` makes); `text` is `""` so Pi uses the terminal's own
foreground. No new palette role is added.

**5. Manifest entry is the theme file only.** `settings.json` is not in
`theme.Manifest()`, because `dg theme set` never writes it (point 2). A crashed
switch therefore restores or discards one file and never Pi's own settings.

**6. Pi's terminal requirements are applied where devgeta owns the config,
and only when they are general.** From
<https://pi.dev/docs/latest/terminal-setup> and
<https://pi.dev/docs/latest/tmux>:

- **tmux**: `set -g extended-keys on`, plus `extended-keys-format csi-u` gated
  on tmux ≥ 3.5 (Debian 12 ships 3.3a, Ubuntu 24.04 ships 3.4, where the
  option does not exist). Without it tmux delivers Shift+Enter as Enter, which
  breaks every modern TUI that distinguishes them, not just Pi.
- **Ghostty**: nothing. Its config has none of the mappings Pi warns about.
- **Alacritty (macOS)**: forward Option+Enter as `\u001b[13;3u` so Pi's
  follow-up key works. See the consequence below.
- **Shell aliases** (`shellCommandPrefix`): not touched now.

## Consequences

- `dg theme set` restyles Pi, live, with no restart.
- Devgeta now writes one key in a file Pi owns. If Pi changes the settings
  shape, that write is the exposure — the same one ADR-0060 already accepted
  for reading `packages`.
- A user who picks another Pi theme in `/settings` keeps it through every
  `dg theme set`; only `dg configure pi --force` puts `devgeta` back.
- The `devgeta` theme name shows up in Pi's `/settings` theme list. It is
  devgeta's name for "follow the system palette", not a palette name.
- The tmux version gate uses `#{>=:#{version},3.5}`, which compares as
  strings: a future tmux `3.10` would compare below `3.5` and silently skip
  the CSI-u line (keys still work through `extended-keys on`'s fallback
  format). Revisit if tmux ever ships a two-digit minor.
- The Alacritty binding changes what Option+Enter sends in every program, not
  only Pi. Programs that do not understand CSI-u (a bare zsh prompt) may
  insert the raw sequence instead of acting like Enter. This is why it is
  macOS-only and listed separately in the cycle doc, so it can be dropped
  without affecting the rest.
- ADR-0059 §7 and ADR-0060 §1/§8 get an amendment note pointing here.
