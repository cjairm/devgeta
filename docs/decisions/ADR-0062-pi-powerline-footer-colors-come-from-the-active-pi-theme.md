# ADR-0062 — pi-powerline-footer's colors come from the active Pi theme

**Date:** 2026-11-06
**Status:** ACCEPTED

## Context

`pi-powerline-footer` (0.22.0, MIT, 24 contributors, investigated in
[2026-11-05-pi-extension-set](../investigations/2026-11-05-pi-extension-set.md))
replaces Pi's footer with a powerline bar. ADR-0061 put Pi on a devgeta theme
so the footer could follow it. Reading the 0.22.0 source (`theme.ts`,
`colors.ts`, `icons.ts`, 2026-11-06) shows how far it already does:

- Every footer segment color is a **Pi theme color name** (`accent`, `success`,
  `thinkingLow`, …) resolved through Pi's `theme.fg`, except three:
  `model` (`#d787af`), `path` (`#00afaf`), and the `rainbow` gradient (eight
  hard-coded hex values) used for thinking levels `high`, `xhigh`, `max`.
- Those can be overridden in
  `<agent dir>/extensions/powerline-footer/theme.json` (`colors` map; any Pi
  foreground color name, a hex value, or `rainbow`). The file is re-read every
  5 seconds, so edits apply without a restart.
- A directory holding only `theme.json` is not loaded as an extension: Pi's
  loader (`core/extensions/loader.js`, Pi 1.5.0) only loads a subdirectory that
  has an `index.ts`/`index.js` or a `package.json` with a `pi` manifest.
- **Not overridable:** the startup welcome overlay (256-color gradient logo and
  `colors.ts` hex values), and the gray (`244`) used for editor-chrome
  separators and bash-mode ghost text.
- Nerd Font glyphs are chosen by a heuristic: `POWERLINE_NERD_FONTS`, then
  `GHOSTTY_RESOURCES_DIR`, then `TERM_PROGRAM`/`TERM` containing a known
  terminal name. Devgeta's Alacritty config sets `TERM=xterm-256color` and
  Alacritty sets no `TERM_PROGRAM`, so under devgeta's Alacritty (with or
  without tmux) the footer always falls back to ASCII, even though devgeta
  installs and selects a Nerd Font there. Ghostty is detected, inside tmux too.
- Its network features (`autoFollowUp`, non-USD `cost.currency`, `/vibe`) are
  all off by default.

## Decision

**1. Install it as a curated extension** (`npm:pi-powerline-footer`, appended
to `piExtensions`, ADR-0060). No pinning, no devgeta-side update path.

**2. Ship one static override file,
`~/.pi/agent/extensions/powerline-footer/theme.json`, that maps the three
hard-coded colors to Pi theme roles.** Every value is a role name, never a hex
value, so the file is not a template: the colors come from whichever Pi theme
is active. Under the `devgeta` theme that is the current `dg theme` palette;
under a theme the user picked, it is that theme. All roles used are required
(or have a built-in fallback) in Pi's schema, so the file is valid under any
Pi theme.

| Footer key      | Upstream default | Pi role         | Why this role                                    |
| --------------- | ---------------- | --------------- | ------------------------------------------------ |
| `model`         | `#d787af`        | `syntaxNumber`  | purple in devgeta's theme, like upstream's mauve |
| `path`          | `#00afaf`        | `syntaxType`    | aqua in devgeta's theme, like upstream's teal    |
| `thinkingHigh`  | `rainbow`        | `thinkingHigh`  | Pi's own role for that level                     |
| `thinkingXhigh` | `rainbow`        | `thinkingXhigh` | same                                             |
| `thinkingMax`   | `rainbow`        | `thinkingMax`   | same; Pi falls back to `thinkingXhigh`           |

Consequences of being static:

- `dg theme set` does not touch it, and it is not in `theme.Manifest()`.
  Pi's hot reload of `devgeta.json` (ADR-0061 §2) restyles the footer too.
- `dg configure pi` writes it only when absent; `--force` rewrites it (the
  same soft/force rule as ADR-0061 §3). This file is also the user's only
  place to customize footer colors, which is why soft never overwrites it.
- Devgeta owns that one file, never the `powerline-footer/` directory. The
  extension's own state (`inbox.jsonl`, `stash-history.json`, …) lives in a
  different directory (`<agent dir>/powerline-footer/`) and is never touched.

**3. No other Pi settings key.** Devgeta still writes only `theme` in
`settings.json` (ADR-0061 §3). In particular it does not set
`powerline.welcome: false` to hide the overlay that ignores the palette —
that is a feature choice for the user, not a theming fix.

**4. The parts that ignore the palette are fixed upstream, not hidden here.**
Open an upstream issue/PR to route `colors.ts` (welcome overlay, separator
gray) through `theme.ts`. Until it lands, those stay as upstream draws them.

**5. Alacritty tells the footer it has a Nerd Font.** When devgeta's Alacritty
template selects its Nerd Font (`.Font == "default"`), it also sets
`POWERLINE_NERD_FONTS = "1"` in `[env]`. A tmux server started from Alacritty
inherits it. With a user-chosen font devgeta cannot know, so nothing is set.
This is separable: dropping it only costs glyphs (ASCII fallback), not colors.

## Consequences

- The whole footer bar follows `dg theme set` live, including under a user's
  own Pi theme.
- Thinking levels high/xhigh/max lose upstream's rainbow. Users who want it
  put `"rainbow"` back in the override file, which soft configure then keeps.
- If upstream adds a new hard-coded color key, it shows upstream's hex until
  the override file gains that key. The test that pins the file's keys against
  the extension's `DEFAULT_COLORS` cannot run offline, so this is a manual
  check when the extension changes.
- Devgeta now writes inside `~/.pi/agent/extensions/`, Pi's extension
  discovery directory. Safe today because a directory without an entry point
  is skipped; if Pi ever starts warning about such directories, this file is
  the cause.
- Devgeta resolves the agent dir as `~/.pi/agent` and does not read
  `PI_CODING_AGENT_DIR`, as before (ADR-0059). A user who sets it gets the
  extension's defaults, not this override.
