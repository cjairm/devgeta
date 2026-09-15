# Theming & Visual Consistency

Devgeta installs a _coordinated_ terminal environment. The terminal emulator
(Alacritty or Ghostty — the user picks one, see ADR-0037), tmux (the
multiplexer), Neovim (the editor), i3 (Linux desktop), and the AI-coder
configs (OpenCode, Claude) are meant to look like **one cohesive setup**, not
several tools that happen to be installed together.

This guide is the source of truth for how the visual layer is wired, what the
shared conventions are, and **the rule you must follow when changing any color
or theme behavior.**

---

## The rule

> **When you add or change a color role, check every themed surface, not just
> the one you're testing against.**
>
> Alacritty and Ghostty are two configs for the same rule, not one: since the
> user only ever has one of them installed (the terminal chooser in
> `internal/tooling/desktop/candidates.go`), it is easy to change one template
> and forget the other exists. Update both on every visual change regardless
> of which one you're testing against.

Every themed surface renders from one shared palette (see below) and a
transparency convention. A change to one that isn't mirrored in the others
creates visible drift — mismatched accent colors between the terminal border
and the editor, a solid pane where the rest of the UI is translucent, etc.
Treat the visual layer as a single surface.

---

## What's wired to what

| Config        | File                                                               | Theming mechanism                                                                              |
| ------------- | ------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------- |
| **Alacritty** | `configs/alacritty/alacritty.toml.tmpl`                            | Templated from `theme.Palette` (`terminal:` group)                                             |
| **Ghostty**   | `configs/ghostty/ghostty.conf.tmpl`                                | Templated from `theme.Palette` (`terminal:` group)                                             |
| **OpenCode**  | `configs/opencode/opencode.json.tmpl` + `themes/default.json.tmpl` | `"theme": "{{ .Theme }}"`; the theme file's `defs` templated from `theme.Palette`              |
| **Neovim**    | `configs/neovim/init.lua` → generated `lua/devgeta/theme.lua`      | A fixed-name shim requires `devgeta.themes.<neovim_module>`, named by the current theme's YAML |
| **tmux**      | `configs/tmux/tmux.conf.tmpl`                                      | Templated from `theme.Palette` (alongside the unrelated `worktree.notify_sound` setting)       |
| **Claude**    | `configs/claude/settings.json.tmpl` + `themes/default.json.tmpl`   | `"theme": "custom:{{ .Theme }}"`; the theme file's `overrides` templated from `theme.Palette`  |
| **i3**        | `configs/i3/config.tmpl`                                           | Templated from `theme.Palette` (`colors:` group)                                               |

Every surface above renders from the same `theme.Palette` type
(`internal/theme`), resolved from a single `configs/themes/<name>.yaml` file
per theme. See
[ADR-0043](../decisions/ADR-0043-a-theme-is-a-palette-file-not-a-branch-in-every-template.md)
for why a theme is one palette file rather than a branch repeated in every
template.

### `current_theme` and `dg theme`

- `internal/config/fromFile.go` defines `CurrentTheme string \`yaml:"current_theme"\``in the global config: the name of the theme every themed app's config was
last rendered with. Every themed app's plain`ForceConfigure()`reads it via`theme.CurrentDefinition()`, falling back to `default` when it is empty.
- **`current_theme` is not a `dg config` key.** It is deploy state, not a
  preference — a bare `dg config set current_theme x` would persist a
  selection with no palette validation, no app reconfigure, and no rollback.
  `dg theme set <name>` is its only write path (see `cmd/theme.go`), because
  writing it has to be coupled to actually reconfiguring every themed app,
  transactionally. `dg config get/set/unset current_theme` is refused the
  same way `integrations.rtk_claude_hook` already is; `dg theme` is the read
  path.
- `dg theme` shows the current theme and what else is available. `dg theme
list` lists available themes. `dg theme set <name>` switches every
  **installed** themed app to `<name>` together: it validates the theme
  before touching anything, backs up every manifest path it is about to
  overwrite, and rolls the whole machine back on the first failure — see the
  cycle doc below for the full transaction design.

Full design history: `docs/plans/cycles/2026-09-14-dg-theme.md`.

---

## The shared palette

Each theme file (`configs/themes/<name>.yaml`) declares colors by semantic
role, not by app — `foreground`, `red`, `background_element`, and so on. The
full role list, and the exact YAML-key/template-field spelling of each one,
is
[ADR-0043 decision point 4](../decisions/ADR-0043-a-theme-is-a-palette-file-not-a-branch-in-every-template.md#decision) —
that table is the **one place** those role names are listed; nothing else,
including this guide, restates it.

`configs/themes/default.yaml` — today's colors, recorded so wiring
`current_theme` through was a visually-null change when it landed — carries
**two** color groups instead of one: classic Gruvbox in `colors:` (tmux,
Neovim, OpenCode, Claude, i3) and Gruvbox Material in `terminal:` (Alacritty,
Ghostty). Which group a surface reads is decided in exactly one place,
`internal/theme`'s `Definition.PaletteFor` — see
[ADR-0043's group mapping](../decisions/ADR-0043-a-theme-is-a-palette-file-not-a-branch-in-every-template.md#which-group-each-surface-gets-and-where-that-is-decided).

**This two-group split is `default`'s own compatibility artifact, not a
design to imitate.** Every theme added after `default` should define only
`colors:`, so one palette applies everywhere — `configs/themes/tokyonight.yaml`
already does this. Collapsing `default`'s own split into one palette is
tracked separately (gap #1 below) since it is a real, user-visible color
change and not a side effect of plumbing.

When adding new colored UI (a tmux status segment, a border, an nvim
highlight), add or reuse a role in the palette rather than inventing a new
hardcoded hex.

---

## Adding a theme

1. Create `configs/themes/<name>.yaml` with a `colors:` group covering every
   role in ADR-0043's table, and a `neovim_module:` key naming a Neovim
   colorscheme module.
2. That module has to already exist, either shipped
   (`configs/neovim/lua/devgeta/themes/<module>.lua`) or in the user's own
   `~/.config/nvim/lua/devgeta/themes/<module>.lua` — `internal/theme.Load`
   validates this before the theme is otherwise usable, and rejects a theme
   naming a module that exists in neither tree.
3. `dg theme set <name>` — that's it. No Go code or template changes for a
   theme whose Neovim module already exists (`tokyonight` is the existing
   example: it only needed the YAML file, since `tokyonight.lua` already
   shipped, unused, before this feature existed).

---

## Transparency is part of the theme

Alacritty ships with `opacity = 0.8` and `blur = true`
(`configs/alacritty/alacritty.toml.tmpl`). Ghostty ships the same values under
its own key names, `background-opacity = 0.8` and `background-blur = true`
(`configs/ghostty/ghostty.conf.tmpl`) — see the parity rule at the top of this
guide. To preserve that translucency, the configs layered on top must **not
paint solid backgrounds**:

- **Neovim** forces a transparent background regardless of colorscheme via
  `configs/neovim/lua/devgeta/transparent.lua` (re-applied on every
  `ColorScheme` event).
- **tmux** must not set a background color on panes/windows. A solid `bg=` in
  `window-style` / `window-active-style` punches an opaque rectangle through the
  blur. To distinguish the active pane, use **foreground** dimming plus border
  emphasis instead:

  ```tmux
  set-window-option -g window-style fg={{.Palette.ForegroundDim}}
  set-window-option -g window-active-style fg={{.Palette.Foreground}}
  set-window-option -g pane-border-lines heavy          # heavy active border
  set-window-option -g pane-border-indicators arrows    # arrows at active pane
  ```

**Rule:** any background styling that defeats the terminal's opacity is a
regression. If you need to emphasize a region, do it with foreground color,
borders, or bold — never an opaque fill.

---

## Fonts

The default font is **MesloLGLDZ Nerd Font** — `alacritty.toml.tmpl`'s
`font.normal`/`bold`/`italic` and `ghostty.conf.tmpl`'s
`font-family`/`font-family-bold`/`font-family-italic` all name it, gated on
`{{if eq .Font "default"}}`. Nerd Font glyphs are assumed by the prompt
(powerlevel10k), tmux status, and editor UI. If you change the font, keep it a
Nerd Font or the icons break across the board.

Font switching (`current_font`, `.Font`) has the identical dead-config shape
`current_theme` had before this feature — it is deliberately out of scope for
now (see the cycle doc's "Explicitly Out of Scope"), a separate piece of work
to keep this one's blast radius reviewable.

---

## Wallpaper

Both shipped themes come with a wallpaper, and `dg theme set-wallpaper <path>`
overrides it per machine (ADR-0044, as amended 2026-09-14).

### Shipped wallpapers

`configs/themes/wallpapers/default.jpg` and `tokyonight.jpg` are embedded
like every other config, and each theme names its own with a **relative**
`wallpaper:` key:

```yaml
neovim_module: gruvbox
wallpaper: wallpapers/default.jpg
```

`internal/theme.Load` resolves a relative value against
`paths.Paths.App.Configs.Themes` — the tree `ExtractEmbeddedConfigs` writes
on install and after a binary upgrade — so the path is correct on whatever
machine it landed on, which is the whole point of shipping them. An
**absolute** value means an image on the user's own machine and is left
exactly as written.

**Adding another shipped wallpaper is a deliberate act, not a drop-in.**
`theme_guards_test.go` holds an allowlist (`shippedWallpapers`): an image
under `configs/` that is not on it fails the build, an allowlist entry must
exist and must sit under `configs/themes/wallpapers/`, and a theme naming a
relative wallpaper that is not shipped fails the build too. Remember what an
entry costs — the file goes into every release binary and is redistributed
to everyone who installs devgeta, so the project needs the right to do that
for that image.

### Per-machine overrides

`dg theme set-wallpaper <path>` records an image for the current theme, and
deliberately never stores the path you gave it. The image is copied into
`~/.config/devgeta/wallpapers/` under a name derived from its own bytes (16
hex characters of its sha256 plus its extension), and that copy's path is
what `global_config.yaml`'s `wallpapers` map records, keyed by theme name.
Content addressing is what makes two different images sharing a basename
(set for two different themes) not collide, and what makes re-setting the
same image a no-op.

`wallpapers` lives in `global_config.yaml`, not in a theme's own YAML file —
`configs/themes/*.yaml` is embedded and `ExtractEmbeddedConfigs` overwrites
it unconditionally on every install and binary upgrade, so a path recorded
there would be silently erased on the next upgrade.

**The map wins over the theme file's declared default.** `dg theme set X`
uses `wallpapers["X"]` when it exists, falls back to theme `X`'s own
`wallpaper:` key when it does not, and leaves the desktop alone when neither
is set. So a shipped wallpaper is a default a user can replace, not
something devgeta re-imposes on every switch.

`dg theme set <name>` applies `wallpapers[<name>]` after everything else has
already committed, through a small platform `WallpaperSetter`
(`internal/theme/wallpaper.go`): AppleScript's `System Events` on macOS
(`tell every desktop to set picture to ...`, so it covers every Space, not
just the current one), `feh --bg-fill` on Linux/i3. Both are best-effort —
a failure warns and never fails the switch (ADR-0044 point 4), and a theme
with neither a recorded nor a declared wallpaper leaves the desktop exactly
as it was. `feh` is
not in devgeta's install set, so its absence on a fresh Linux machine is the
normal case, not an edge one; the warning names the install command.

**The macOS route is implemented from documented AppleScript behavior, not
independently verified against a real machine as part of this cycle.**
Before relying on it, check that it survives a Space switch and a logout
(the measurement ADR-0044 originally called for) and update this note once
it has been.

---

## Known gaps (converge these over time)

These are documented honestly so contributors don't mistake them for intent:

1. **Palette mismatch inside `default`.** Classic Gruvbox (`colors:`) vs.
   Gruvbox Material (`terminal:`) — `default.yaml`'s two groups exist only to
   keep the palette rewrite visually null; converging them into one is a
   real, user-visible color change and belongs to its own decision, not this
   plumbing.
2. **Font switching is unwired**, same shape as `current_theme` was — see
   "Fonts" above.
3. **The macOS wallpaper route is unverified against a real machine** — see
   "Wallpaper" above.
4. **Devgeta's own TUI needs no theme wiring at all.**
   `internal/tui/components/styles.go` builds its palette from **ANSI
   indices**, not hex, so it already inherits whatever the terminal is
   themed to. This is the model other themed surfaces cannot follow (a
   config file, not a live terminal, is what most of them write), not a gap.

When you touch theming, prefer changes that move toward convergence (one
palette, one theme source) rather than adding another hardcoded color in
isolation.

---

## Checklist when changing the visual layer

- [ ] Did you add or change a color role? Add it to
      [ADR-0043's role table](../decisions/ADR-0043-a-theme-is-a-palette-file-not-a-branch-in-every-template.md#decision)
      and every theme file (`configs/themes/*.yaml`) that needs it — never a
      literal hex in a template.
- [ ] Did you add a background fill? Confirm it doesn't defeat Alacritty
      opacity (prefer fg/border emphasis).
- [ ] Did you change the font? Confirm it's still a Nerd Font.
- [ ] Did you touch theme resolution, `current_theme`, or the `dg theme`
      command? Update this guide and, if the design itself changed, the ADR.
