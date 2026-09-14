# ADR-0043 — A theme is a palette file, not a branch in every template

**Date:** 2026-09-14
**Status:** PROPOSED

## Context

devgeta ships one theme, Gruvbox dark, and no way to change it. The machinery
to change it half-exists and has been dead since it was written:

- `internal/config/fromFile.go:303` defines `current_theme string`. **Nothing
  reads it.**
- `internal/apps/alacritty/alacritty.go:58`,
  `internal/apps/ghostty/ghostty.go:83` and
  `internal/apps/opencode/opencode.go:100` each assign a local
  `theme := "default"` immediately before rendering their template, so the
  `.Theme` value the templates receive is a constant.
- `cmd/root.go:52` and `:59` advertise `dg change --theme=... --font=...` to
  every user who runs `dg --help`. No such command is registered.
  `ROADMAP.md:30` lists it as planned.

[docs/guides/theming.md](../guides/theming.md) already documents this as
"Known gaps" #1 and #2, and states the direction to prefer: "changes that move
toward convergence (one palette, one theme source) rather than adding another
hardcoded color in isolation."

So the question is not "should theme switching work" — it is **what shape the
theme definition takes**, because that shape decides the cost of every theme
devgeta ever adds.

### Seven surfaces, three mechanisms

Theming today is spread across seven config surfaces that do not agree on how
a color gets set:

| Surface       | Where                                   | Mechanism                                               |
| ------------- | --------------------------------------- | ------------------------------------------------------- |
| **Alacritty** | `configs/alacritty/alacritty.toml.tmpl` | `{{if eq .Theme "default"}}` wrapping literal hex       |
| **Ghostty**   | `configs/ghostty/ghostty.conf.tmpl`     | same                                                    |
| **OpenCode**  | `configs/opencode/opencode.json.tmpl:3` | `"theme": "{{ .Theme }}"` + copies `themes/<name>.json` |
| **tmux**      | `configs/tmux/tmux.conf.tmpl:111-161`   | hardcoded hex, thirteen lines                           |
| **Neovim**    | `configs/neovim/init.lua:138`           | hardcoded `require("devgeta.themes.gruvbox")`           |
| **Claude**    | `configs/claude/settings.json.tmpl:3`   | hardcoded `"theme": "custom:default"` + `themes/` copy  |
| **i3**        | `configs/i3/config:153-161`             | hardcoded hex behind i3 `set $name` variables           |

One surface needs no work at all and should be left alone:
`internal/tui/components/styles.go` builds devgeta's own TUI palette out of
**ANSI indices**, not hex. It inherits whatever the terminal is themed to, for
free, forever. That is the property to imitate, not to replace.

### The obvious approach, and why it does not survive

The path of least resistance is to extend the mechanism Alacritty and Ghostty
already use — add a branch per theme:

```
{{if eq .Theme "default"}}
  ...
{{else if eq .Theme "tokyonight"}}
  ...
{{end}}
```

Cost of adding one theme under that design: edit seven files, none of which
can be validated against the others. Cost of adding the tenth theme: the same
seven files, now each carrying ten near-identical blocks. Nothing structurally
prevents theme N from defining a red in tmux that disagrees with its red in
Alacritty, which is precisely the failure theming.md's palette table exists to
catch by hand.

That this drift is real and not hypothetical is already visible in the tree.
theming.md gap #3 records it: tmux, Neovim, OpenCode and Claude use **classic**
Gruvbox (`fg #ebdbb2`, `red #fb4934`), while Alacritty and Ghostty use **Gruvbox
Material** (`fg #d4be98`, `red #ea6962`). Same nominal theme, two palettes,
because two people edited two files. A per-template branch design makes that
outcome the default for every future theme.

## Decision

**A theme is one palette file. Templates consume palette values; they never
branch on a theme name.**

Concretely:

1. **One file per theme**, `configs/themes/<name>.yaml`, declaring colors by
   **semantic role** — not by app. `default.yaml` records exactly what ships
   today.

2. **Every template takes the resolved palette** as its template data
   (`{{.Palette.Background}}`, `{{.Palette.Red}}`, …) in place of literal hex
   and in place of `{{if eq .Theme "..."}}` gates. The four hardcoded surfaces
   (tmux, Neovim, Claude, i3) become generated the same way the templated ones
   already are.

   **A template receives exactly one flat palette** — one hex per role, no
   nesting — and never sees that a theme file can carry more than one group of
   colors. A theme file with two groups (see "One deliberate ugliness" below)
   is resolved to one flat palette per surface **in the loader**, keyed by
   surface name from a single table. No template, and no app, decides which
   group it is looking at.

3. **`current_theme` becomes the single source of which theme the machine is
   on.** The three local `theme := "default"` assignments are deleted.
   `ForceConfigure` resolves the palette from `current_theme` in the loaded
   `GlobalConfig`, falling back to `default` when it is empty — the dead config
   field becomes live config.

   **The palette is a parameter of config generation, not a read inside it.**
   The configure body takes an already-resolved palette; `ForceConfigure` is
   just the caller that resolves it out of the config. A caller that already
   knows which palette it wants passes it in instead, and `dg theme set` has to
   be such a caller for two reasons that meet here: `App.ForceConfigure()` takes
   no arguments (`internal/apps/contract.go:26`), and a theme switch must not
   persist `current_theme` until every app has succeeded — so an app that read
   the field mid-switch would read the theme being replaced and faithfully
   re-render it. The mechanism is a small optional interface beside
   `SelectiveConfigurer`, specified in the cycle doc's Step 5.

4. **Roles are named after what they mean, not after Gruvbox**, and each role
   has exactly one name in two spellings: `snake_case` as the YAML key, the
   same words in `PascalCase` as the template field. `foreground_muted:` in a
   theme file is `{{.Palette.ForegroundMuted}}` in a template, and the loader
   maps one onto the other mechanically. No abbreviation — not `fg_muted`, not
   `bg_hard` — is a role name anywhere, in a file or in a doc. A theme file is
   the only place a hex value appears.

   The role set, derived from the union of what the seven surfaces actually
   use today:

   | YAML key                  | Template field          | `default` today               |
   | ------------------------- | ----------------------- | ----------------------------- |
   | `background_hard`         | `BackgroundHard`        | `#1d2021`                     |
   | `background`              | `Background`            | `#282828`                     |
   | `background_element`      | `BackgroundElement`     | `#3c3836`                     |
   | `background_subtle`       | `BackgroundSubtle`      | `#504945`                     |
   | `border`                  | `Border`                | `#665c54`                     |
   | `foreground`              | `Foreground`            | `#ebdbb2`                     |
   | `foreground_muted`        | `ForegroundMuted`       | `#a89984`                     |
   | `foreground_dim`          | `ForegroundDim`         | `#928374`                     |
   | `foreground_subtle`       | `ForegroundSubtle`      | `#7c6f64`                     |
   | `red` … `orange`          | `Red` … `Orange`        | bright: `#fb4934`, `#b8bb26`… |
   | `red_dim` … `aqua_dim`    | `RedDim` … `AquaDim`    | i3's `#cc241d`, `#98971a`…    |
   | `diff_added_background`   | `DiffAddedBackground`   | `#32361a`                     |
   | `diff_removed_background` | `DiffRemovedBackground` | `#3c1f1e`                     |

   The "`default` today" column records `default`'s **`colors:`** group. Its
   `terminal:` group defines the same role names with the Gruvbox Material hex
   — `foreground: #d4be98`, `red: #ea6962` — which is the split the next
   section is about. The role set is the same in both groups; only the values
   differ.

   Adding a role later is cheap. Renaming one breaks every theme file, which is
   why the set comes from measured usage rather than invention. This table is
   the one place the role names are listed; anything else that needs them —
   the cycle doc, theming.md — points here instead of restating them.

Adding a theme becomes: **add one palette file.** No Go changes, no template
changes, and a palette that cannot disagree with itself across surfaces because
there is only one of it.

**One surface bounds that claim: Neovim.** Six of the seven take flat colors and
are fully generated from the palette. Neovim takes a plugin, not colors, so a
theme file _names_ a Neovim colorscheme module and the palette cannot conjure
one — a theme is one file only when the module it names already exists. The
prerequisite and how it is validated are spelled out under "What a palette
cannot generate" below.

### One deliberate ugliness, recorded on purpose

`default.yaml` will carry **two** color groups, not one:

```yaml
neovim_module: gruvbox
colors: # classic Gruvbox
  foreground: "#ebdbb2"
  red: "#fb4934"
  # … every role in point 4's table
terminal: # Gruvbox Material — the same role names, different hex
  foreground: "#d4be98"
  red: "#ea6962"
  # … every role in point 4's table
```

This is not the design we want. It exists so that wiring `current_theme`
through is a **visually null change**: after it lands, every config renders
byte-equivalent colors to what it renders today, which is the only way to
verify the rewrite of seven files did not quietly change anyone's terminal.

Collapsing the two groups into one is theming.md gap #3, and it is a separate,
deliberate, user-visible change — someone's Alacritty foreground shifts from
`#d4be98` to `#ebdbb2`. It does not get smuggled in as a side effect of
plumbing work. New themes added after this ADR SHOULD define only `colors:` and
omit `terminal:`, inheriting one palette everywhere; the split is a
compatibility artifact of `default`, not a feature.

#### Which group each surface gets, and where that is decided

Two groups mean something has to choose between them, and point 2 says it is
neither the template nor the app. **The loader owns the choice, from one
table:**

| Group       | Surfaces                           |
| ----------- | ---------------------------------- |
| `terminal:` | Alacritty, Ghostty                 |
| `colors:`   | tmux, Neovim, OpenCode, Claude, i3 |

That table is the mapping's only statement in the docs. In code it is one
`map[string]group` in `internal/theme`, keyed by the `pkg/constants` app names
(`constants.Alacritty`, …), and a test asserts the code table and this one
agree. A mapping restated per app or per template is exactly the drift this ADR
exists to remove, so it is not restated anywhere — the cycle doc points here.

The shape that follows from that:

- `theme.Load(name)` returns a **`theme.Definition`**: the theme's name, its
  `neovim_module`, and its two resolved groups. It is not a `Palette` itself,
  because a `Palette` is flat and a theme file is not.
- `Definition.PaletteFor(surface string) (Palette, error)` returns the one flat
  palette that surface's template consumes. A surface with no table entry is an
  **error**, not a silent default, so an eighth themed surface added without a
  mapping fails loudly instead of quietly rendering the wrong group.
- Everything downstream — `GenerateFromTemplate` data, the
  `ForceConfigureTheme(p theme.Palette)` call in the cycle doc's Step 5 — carries
  the flat `Palette` only. An app names itself and gets its group; it never
  names a group.

**A theme that omits `terminal:` uses `colors:` for the terminal surfaces too.**
That is the fallback new themes are expected to rely on (the SHOULD above), and
it is a copy of the resolved `colors:` group, so `PaletteFor` never returns a
partially filled palette.

**`terminal:` is either absent or complete.** When present it is validated as
strictly as `colors:` — every role in point 4's table, every value parseable as
a color. A partial `terminal:` that inherited the missing roles from `colors:`
was considered and rejected: it makes "which group is this role missing from" an
ambiguity in every validation error, and it would make the split ergonomic to
author, when the split is a compatibility artifact we intend to delete.

For `default`, the two groups genuinely differ, so the visually-null byte
goldens in the cycle's §3 already fail if a surface is wired to the wrong group.
But that is the only theme they cover: for a theme with just a `colors:` group
both candidates are the same palette, so a mis-mapping there is invisible. The
mapping is therefore pinned directly as well, against a fixture theme whose
groups differ — the cycle doc's Step 6 names those tests.

### What a palette cannot generate, and how that is handled

Two surfaces do not consume flat colors, and pretending otherwise would break
them:

- **OpenCode** (`configs/opencode/themes/default.json`) is a rich semantic
  theme: a `defs` block of raw colors, then a large `theme` block mapping ~60
  roles (`syntaxKeyword`, `diffAddedLineNumberBg`, …) onto those defs.
- **Claude** (`configs/claude/themes/default.json`) has its own smaller key set
  under `overrides`. Every hex in that file is inside `overrides`, so
  substituting that one block covers all of Claude's color.

Neither role mapping is regenerated from scratch. Each becomes a template in
which **only the raw-color block is substituted** from the palette — OpenCode's
`defs`, Claude's `overrides` — while the role-mapping block stays static and
hand-maintained. The palette supplies pigment; each app keeps its own opinion
about where the pigment goes.

#### OpenCode's `theme` block has to be cleaned up first

"Only the `defs` block is substituted" is not true of the file as it stands.
Six entries in OpenCode's `theme` block hold **raw hex instead of a def name**:
`border` (`#665c54`), `borderSubtle` (`#504945`), `diffAddedBg` and
`diffAddedLineNumberBg` (`#32361a`), `diffRemovedBg` and
`diffRemovedLineNumberBg` (`#3c1f1e`). Substitute only `defs` and those six
stay Gruvbox in every theme devgeta ever adds — which would make the decision
at the top of this ADR false, not merely incomplete.

So, as part of the conversion:

- **Every raw dark-side color in the `theme` block moves into `defs`** — new
  defs `gb_border_dark`, `gb_bg_subtle_dark`, `gb_diff_added_bg_dark`,
  `gb_diff_removed_bg_dark` — and the `theme` entries reference those names.
  OpenCode resolves a def name and a literal to the same color, so the rendered
  colors do not change; the file's shape does. The palette roles this needs are
  `border`, `background_subtle`, `diff_added_background` and
  `diff_removed_background`, all in point 4's set.
- **A test asserts that no `theme`-block value in a shipped OpenCode theme is a
  literal color.** A comment would not survive the next hand-edit; §4 asks for
  the class of mistake to be made impossible instead.
- **The `light` halves are the one recorded exception.** Light themes are out
  of scope (the cycle's scope boundary says so), the palette defines no light
  roles, and the `light` values stay literal until a light theme is actually
  wanted. A new dark theme inherits `default`'s light colors and never displays
  them. This is a known, bounded gap, not an oversight — closing it means
  adding a light half to the palette, which is its own decision.

- **Neovim** is not colors at all; it is a plugin choice
  (`gruvbox.nvim` vs `tokyonight.nvim`). So the theme file names a Neovim module
  in a required `neovim_module:` key, and config generation writes a fixed-name
  shim (`lua/devgeta/theme.lua`) that `init.lua` requires unconditionally.
  `init.lua` stops naming a specific colorscheme.

  **The module is a prerequisite the palette cannot supply.** A module is a
  hand-written Lua file that installs a colorscheme plugin and calls
  `vim.cmd.colorscheme`; `configs/neovim/lua/devgeta/themes/gruvbox.lua` and
  `tokyonight.lua` are the two that exist today, both complete. A module reaches
  Neovim from one of two places, and the shim resolves both identically because
  `files.CopyDir` (`pkg/files/files.go:73`) is additive — it overwrites the files
  it ships and deletes nothing else:

  - **Shipped.** A module under `configs/neovim/lua/devgeta/themes/` is embedded
    in the binary and deployed by `dg configure neovim`. Adding one is a repo
    change plus a rebuild, so **a theme that needs a new module is two files and
    a release, not one file.**
  - **User-supplied.** A module a user drops into
    `~/.config/nvim/lua/devgeta/themes/<name>.lua` survives
    `dg configure neovim --force`, so a user-authored theme can name it without
    recompiling anything.

  **`neovim_module` is validated exactly like a color role, against both places
  above.** The loader looks for `lua/devgeta/themes/<module>.lua` under the
  extracted shipped configs **and** under the user's Neovim config dir, accepts
  the theme if either has it, and otherwise rejects it — before any config is
  written — naming both paths it looked in and the modules it found. Without that
  check, a bad name renders a shim that fails only on the next `nvim` start,
  after every other surface has already been rewritten: exactly the outcome the
  "validate before writing anything" rule below exists to prevent.

  **Both trees, not just the deployed one**, because the deployed tree reliably
  holds only the user-supplied case. A machine with no Neovim has no
  `~/.config/nvim` at all, and a deployed-only check would reject every theme
  there — including `default` — on a machine where the right behavior is to skip
  Neovim and theme the other six surfaces. A shipped module also only arrives in
  the deployed tree via the same `dg configure neovim` call that writes the shim,
  so at validation time it is legitimately absent from it. Checking the shipped
  tree as well keeps a theme file's validity a property of the file rather than
  of the machine reading it, without letting a module that exists nowhere pass.

## Consequences

**Easier**

- Adding a theme is one palette file whenever the Neovim module it names is
  already there — shipped or user-supplied. That is the whole point, and it is
  what makes "keep adding new themes on top of default" a cheap operation
  instead of a seven-file chore. A theme that also needs a new Neovim module
  costs two files, and a rebuild if that module is to ship.
- Cross-surface drift becomes structurally impossible within a theme, rather
  than a convention theming.md asks contributors to remember. §4's "prefer
  making it structurally impossible over documenting a convention people must
  remember" is satisfied for the first time in the visual layer.
- theming.md's palette table stops being hand-maintained truth and becomes
  derived from `configs/themes/default.yaml`.
- Fonts have the identical dead-config shape (`current_font`, `.Font`,
  `{{if eq .Font "default"}}`). This design transfers to them unchanged, whether
  or not that work happens now.

**Harder**

- Seven config files get rewritten at once, which is a wide blast radius for a
  change whose success criterion is "nothing looks different". This is why the
  visually-null constraint above is not optional, and why the cycle verifies
  rendered output mechanically rather than by eye.

  Most surfaces are byte-compared before and after. **Two cannot be, because
  their change is structural by design:** Neovim's `init.lua` stops naming a
  colorscheme and requires a generated shim instead (a new file appears), and
  OpenCode's theme JSON moves four colors out of `theme` into `defs`. For those
  two the check is on **resolved colors**, plus an explicit assertion of the
  structural change that was intended. The cycle's §6 carries both lists; a
  criterion of "byte-identical everywhere" would be unachievable and would get
  quietly dropped, which is worse than naming the exceptions.

- A palette role set is a public-ish contract. Once themes exist, renaming a
  role breaks every theme file. The role set should be derived from the union of
  what the seven surfaces actually use today, and then left alone.
- Anything under `configs/` is read by the embedded-config tests in the root
  package plus `internal/apps/claude` and `internal/apps/opencode`, and
  `internal/config` has dozens of importers. Per CLAUDE.md §6 this is explicitly
  a "blast radius is most of the tree" change: verification is the full
  `go test ./...`, not a targeted run.

**Accepted trade-offs**

- **Two color groups in `default.yaml` instead of one.** Accepted to keep the
  plumbing change invisible; convergence is tracked separately rather than
  bundled.
- **Static role-mapping blocks in the OpenCode and Claude theme templates.**
  A new theme inherits `default`'s opinion about which role paints
  `syntaxKeyword` and can only override the pigment. Accepted: the alternative
  is modeling sixty semantic roles in devgeta's own palette format to serve two
  apps that each already have a good one.
- **YAML, not Go, for theme definitions.** Consistent with
  `global_config.yaml` and `gopkg.in/yaml.v3`, already a dependency. A theme
  authored by a user needs no recompile, provided it names a Neovim module that
  devgeta ships or that the user has already dropped in — which also means a
  malformed theme is a runtime error,
  so palette loading validates every required role is present in every group the
  file defines, every value parses as a color, and `neovim_module` resolves to a
  real file, all before any config is written.
