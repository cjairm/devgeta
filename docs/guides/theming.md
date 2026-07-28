# Theming & Visual Consistency

Devgeta installs a _coordinated_ terminal environment. Alacritty (the terminal),
tmux (the multiplexer), Neovim (the editor), and the AI-coder configs (OpenCode,
Claude) are meant to look like **one cohesive setup**, not four tools that
happen to be installed together.

This guide is the source of truth for how the visual layer is wired, what the
shared conventions are, and **the rule you must follow when changing any color
or theme behavior.**

---

## The rule

> **When you change a color, font, or theme behavior in one of {alacritty, tmux,
> neovim, opencode, claude}, check the others and update them to match.**

These configs share a palette and a transparency convention. A change to one
that isn't mirrored in the others creates visible drift — mismatched accent
colors between the terminal border and the editor, a solid pane where the rest
of the UI is translucent, etc. Treat the visual layer as a single surface.

---

## What's wired to what

| Config        | File                                       | Theming mechanism                        |
| ------------- | ------------------------------------------ | ---------------------------------------- |
| **Alacritty** | `configs/alacritty/alacritty.toml.tmpl`    | Templated — `{{if eq .Theme "default"}}` |
| **OpenCode**  | `configs/opencode/opencode.json.tmpl`      | Templated — `"theme": "{{ .Theme }}"`    |
| **Neovim**    | `configs/neovim/init.lua` → `themes/*.lua` | Hardcoded — loads `themes/gruvbox.lua`   |
| **tmux**      | `configs/tmux/tmux.conf`                   | Hardcoded — static color block           |
| **Claude**    | `configs/claude/themes/default.json`       | Static theme file                        |

The templated configs receive a `.Theme` value at config-generation time (see
`files.GenerateFromTemplate`). The hardcoded ones bake the colors directly into
the shipped file.

### The `.Theme` variable and `current_theme` (current state)

- `internal/config/fromFile.go` defines `CurrentTheme string \`yaml:"current_theme"\``
  in the global config.
- **However, `current_theme` is not read anywhere yet.** Both
  `internal/apps/alacritty/alacritty.go` and `internal/apps/opencode/opencode.go`
  hardcode `theme := "default"` before generating their templates. The
  templating machinery exists, but the theme is effectively **pinned to
  `"default"`** across the board.
- So today there is exactly one theme: **Gruvbox dark**. Multi-theme switching
  is plumbed but not finished — see "Known gaps" below.

---

## The shared palette (Gruvbox dark)

Everything defaults to **Gruvbox dark** on a `#282828` background. Two important
caveats:

1. **There are two Gruvbox variants in play.** tmux and Neovim use _classic_
   Gruvbox; Alacritty's `default` colors are _Gruvbox Material_. They share the
   background but their accent colors differ (see the table). This is a known
   inconsistency, not an intentional design.
2. The background `#282828` is the one value that **is** consistent everywhere —
   keep it that way.

| Role          | Classic Gruvbox (tmux, nvim) | Gruvbox Material (alacritty) |
| ------------- | ---------------------------- | ---------------------------- |
| Background    | `#282828`                    | `#282828`                    |
| Foreground    | `#ebdbb2`                    | `#d4be98`                    |
| Red           | `#fb4934`                    | `#ea6962`                    |
| Green         | `#b8bb26`                    | `#a9b665`                    |
| Yellow        | `#fabd2f`                    | `#d8a657`                    |
| Blue          | `#83a598`                    | `#7daea3`                    |
| Magenta       | `#d3869b`                    | `#d3869b`                    |
| Cyan          | `#8ec07c` / `#89b482`        | `#89b482`                    |
| Orange/accent | `#fe8019`                    | `#e78a4e`                    |

When adding new colored UI (a tmux status segment, a border, an nvim highlight),
pull from the matching column rather than inventing a new hue.

---

## Transparency is part of the theme

Alacritty ships with `opacity = 0.8` and `blur = true`
(`configs/alacritty/alacritty.toml.tmpl`). To preserve that translucency, the
configs layered on top must **not paint solid backgrounds**:

- **Neovim** forces a transparent background regardless of colorscheme via
  `configs/neovim/lua/devgeta/transparent.lua` (re-applied on every
  `ColorScheme` event).
- **tmux** must not set a background color on panes/windows. A solid `bg=` in
  `window-style` / `window-active-style` punches an opaque rectangle through the
  blur. To distinguish the active pane, use **foreground** dimming plus border
  emphasis instead:

  ```tmux
  set-window-option -g window-style fg=#928374          # inactive: muted fg
  set-window-option -g window-active-style fg=#ebdbb2   # active: bright fg
  set-window-option -g pane-border-lines heavy          # heavy active border
  set-window-option -g pane-border-indicators arrows    # arrows at active pane
  ```

**Rule:** any background styling that defeats Alacritty's opacity is a
regression. If you need to emphasize a region, do it with foreground color,
borders, or bold — never an opaque fill.

---

## Fonts

The default font is **MesloLGLDZ Nerd Font** (`alacritty.toml.tmpl`, gated on
`{{if eq .Font "default"}}`). Nerd Font glyphs are assumed by the prompt
(powerlevel10k), tmux status, and editor UI. If you change the font, keep it a
Nerd Font or the icons break across all three.

---

## Known gaps (converge these over time)

These are documented honestly so contributors don't mistake them for intent:

1. **`current_theme` is dead config.** It exists in the global config but no app
   reads it. Wiring it through to the `.Theme` template value (and to the
   hardcoded tmux/nvim configs) would make theme switching real.
2. **tmux and nvim are hardcoded**, while alacritty and opencode are templated.
   A single theme switch can't currently affect all four.
3. **Palette mismatch.** Classic Gruvbox (tmux/nvim) vs. Gruvbox Material
   (alacritty). Picking one variant everywhere would make accents line up.
4. **An unused `tokyonight.lua`** sits in `configs/neovim/lua/devgeta/themes/`.
   It's the seed of a second theme but nothing selects it.

When you touch theming, prefer changes that move toward convergence (one palette,
one theme source) rather than adding another hardcoded color in isolation.

---

## Checklist when changing the visual layer

- [ ] Did you change a color? Update the matching column in the palette table
      above **and** mirror it in the sibling configs.
- [ ] Did you add a background fill? Confirm it doesn't defeat Alacritty opacity
      (prefer fg/border emphasis).
- [ ] Did you change the font? Confirm it's still a Nerd Font.
- [ ] Did you touch the `.Theme` flow or `current_theme`? Update this guide.
