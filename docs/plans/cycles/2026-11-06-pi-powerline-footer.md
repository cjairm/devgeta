# Cycle: pi-powerline-footer, on the devgeta palette

**Date:** 2026-11-06
**Estimated Duration:** ~2 hours
**Status:** Implemented 2026-11-06; maintainer's manual verification (§6) and the upstream post pending

---

## 1. Domain Context

Step two of two. Step one
([2026-11-06-pi-theme](2026-11-06-pi-theme.md), ADR-0061) put Pi on a
`devgeta` theme that follows `dg theme`. This step installs
`pi-powerline-footer` and makes its bar use that theme. Design and the source
reading behind it: [ADR-0062](../../decisions/ADR-0062-pi-powerline-footer-colors-come-from-the-active-pi-theme.md).
Maintenance check: [investigation](../../investigations/2026-11-05-pi-extension-set.md#pi-powerline-footer).

## 2. Engineer Context

- `internal/apps/pi/pi.go` — `piExtensions`, `configure(force)`,
  `ForceConfigureTheme`, `themeFile()`.
- `internal/apps/pi/pi_test.go` — docs-mirror test over `piExtensions`;
  install-order tests that slice `piExtensions`.
- `docs/apps/pi.md` — extension list (mirrored by the test) and Theme section.
- `configs/alacritty/alacritty.toml.tmpl` — `[env]`, `{{if eq .Font "default"}}`.
- `pkg/files` — `CopyFile` / `WriteFileAtomic`.

Targeted tests (importers of `internal/apps/pi` and `internal/apps/alacritty`
per CLAUDE.md §6, plus the root package because `configs/` changes):

```bash
go test ./internal/apps/pi/ ./internal/apps/alacritty/ ./internal/apps/registry/ \
        ./internal/tooling/terminal/ ./internal/tooling/desktop/
go test .
make lint
```

## 3. Objective

`dg configure pi` installs `pi-powerline-footer` and leaves every color in its
bar coming from the active Pi theme, so `dg theme set <name>` restyles the bar
live along with the rest of Pi.

## 4. Scope Boundary

### In Scope

- [x] `npm:pi-powerline-footer` appended to `piExtensions` and `docs/apps/pi.md`
- [x] `configs/pi/powerline-footer/theme.json` (static; five keys, ADR-0062 §2)
- [x] Configure writes it to `~/.pi/agent/extensions/powerline-footer/theme.json`:
      soft only when absent, force always
- [x] Alacritty `[env]` `POWERLINE_NERD_FONTS = "1"` when `.Font == "default"`
      (separable, ADR-0062 §5)
- [x] Upstream issue/PR: route `colors.ts` through `theme.ts` (ADR-0062 §4) — text drafted, posting is the maintainer's call
- [x] Tests; docs (`docs/apps/pi.md`, `docs/guides/theming.md`, ADR index,
      investigation entry marked done)

### Explicitly Out of Scope

- Any other `settings.json` key (`powerline.*` presets, `welcome`, layout).
- Hiding or patching the welcome overlay.
- The rest of the investigation's extension list.
- `PI_CODING_AGENT_DIR` support.

## 5. Implementation Plan

### Steps

#### Step 1: Override file

`configs/pi/powerline-footer/theme.json`:

```json
{
  "colors": {
    "model": "syntaxNumber",
    "path": "syntaxType",
    "thinkingHigh": "thinkingHigh",
    "thinkingXhigh": "thinkingXhigh",
    "thinkingMax": "thinkingMax"
  }
}
```

Verify: root-package test — parses, and its `colors` map equals the ADR-0062
§2 table exactly (key and value), so dropping or remapping a key fails a test
that names the ADR; no value is a hex color or `rainbow` (the file must never
pin a color).

#### Step 2: Configure writes it

`footerThemeFile()` beside `themeFile()`. In `configure(force)`, after
`selectTheme`, before extensions: `force || !exists` → copy the embedded file
with `files.WriteFileAtomic` (parent dir created). Not called from
`ForceConfigureTheme` — `dg theme set` must not touch it.
Verify: tests for absent → written; present + soft → byte-identical; present +
force → replaced; `ForceConfigureTheme` does not create it; no real commands.

#### Step 3: Install the extension

Append `"npm:pi-powerline-footer"` to `piExtensions`; add its section to
`docs/apps/pi.md` (what it adds, the override file, what still ignores the
palette, opt-in network features). Fix any install-order test that slices
`piExtensions` by index.
Verify: `go test ./internal/apps/pi/`.

#### Step 4: Alacritty Nerd Font signal

Inside `[env]`: `{{if eq .Font "default"}}POWERLINE_NERD_FONTS = "1"{{end}}`
with a one-line comment. Update the golden.
Verify: alacritty tests — present with the default font, absent with another.

#### Step 5: Docs and upstream

- `docs/guides/theming.md` — note the footer follows Pi's theme; no new surface.
- `docs/decisions/README.md` — ADR-0062 entry.
- Investigation: mark `pi-powerline-footer` done, and close its two open
  questions: OSC 4/10/11 passthrough under tmux no longer matters (the
  `devgeta` Pi theme uses explicit colors, not the terminal palette), and the
  footer paints no backgrounds while Pi's own block backgrounds were settled in
  step one (`docs/apps/pi.md`, Theme).
- Upstream issue/PR text covering both parts that ignore the palette (ADR-0062
  §4): the welcome overlay, and the gray (`244`) for editor-chrome separators
  and bash-mode ghost text — drafted for the maintainer to post (posting is
  outward-facing).

## 6. Verification Plan

### Automated

The targeted commands in §2.

### Manual (maintainer runs — each writes to the real machine; ask first)

1. `make build`, install the binary.
2. `dg configure pi --force` → extension in `pi list`;
   `~/.pi/agent/extensions/powerline-footer/theme.json` matches the embedded file.
3. Open `pi` in Alacritty+tmux and in Ghostty: bar uses palette colors, Nerd
   Font glyphs (Alacritty needs `dg configure alacritty --force` and a new
   tmux server first).
4. With Pi open, `dg theme set tokyonight` → bar restyles within ~5s;
   `dg theme set default` → back.
5. Set thinking to high, then xhigh, then max: each level uses the palette, not
   the rainbow. Max matters most — the `devgeta` theme does not define
   `thinkingMax`, so it relies on Pi's built-in fallback to `thinkingXhigh`
   (read in Pi 1.5.0's `theme-tokens.d.ts`), which no automated test can check.

## 7. Risks & Trade-offs

| Risk                                                     | Likelihood | Mitigation                                                            |
| -------------------------------------------------------- | ---------- | --------------------------------------------------------------------- |
| Upstream adds another hard-coded color key               | Med        | Shows upstream's hex until we add the key; check on extension changes |
| Upstream renames the override path                       | Low        | File is ignored, footer falls back to defaults; no breakage           |
| Pi starts warning about entry-less dirs in `extensions/` | Low        | ADR-0062 names this file as the cause                                 |
| Welcome overlay keeps its own colors                     | Certain    | Upstream fix (ADR-0062 §4); startup splash only                       |
| Separator and bash-mode ghost text stay gray `244`       | Certain    | Upstream fix (ADR-0062 §4); visible during use, but a neutral gray    |

### Trade-offs Made

- Static role-name file over a palette template: nothing to re-render on
  `dg theme set`, and it works under a user's own Pi theme (ADR-0062 §2).
- Rainbow dropped for high thinking levels; one key restores it.

## 8. Cross-Model Review Notes

**Reviewer notes:**

- 2026-11-06 document review, round 1: REQUEST CHANGES. n1 (Step 5 and the
  risk table covered only the welcome overlay, not the `244` gray) fixed; the
  `thinkingMax` manual check, the investigation's open questions, and the
  exact-table test suggestion applied.
- 2026-11-06 document review, round 2: APPROVE. Recommends keeping
  `model` → `syntaxNumber`, `path` → `syntaxType` over `accent` for both: two
  distinct hues like upstream, and `accent` is Pi's chrome color.
- 2026-11-06 maintainer: plan approved with `model` → `syntaxNumber`,
  `path` → `syntaxType`. ADR-0062 marked ACCEPTED. Implementation happens in a
  later session.
