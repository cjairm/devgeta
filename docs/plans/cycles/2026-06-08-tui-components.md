# Cycle: TUI Component Library

**Status:** Done
**Date:** 2026-06-08
**Scope:** ~3 hours

---

## Goal

Extract reusable UI primitives from the existing layout A (`internal/tui/worktree/`) into a shared component package (`internal/tui/components/`), then migrate layout A to consume them. The components will serve as the building blocks for layouts B–E described in the wt v2 wireframes.

---

## Decisions Made

| Decision               | Choice                                                                                         | Rationale                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| Package location       | `internal/tui/components/` (`package tuicomponents`)                                           | Importable by any future TUI, not coupled to worktree                                                        |
| Session state coupling | `SessionState` enum + `SessionStateFromWorktree()` helper                                      | Components stable as data model evolves; zero API change when `NeedsReview`/`Dirty` land on `WorktreeStatus` |
| Component style        | `Palette` method receivers for logic-bearing components; raw `lip.Style` fields for direct use | Matches existing devgeta style, idiomatic Go                                                                 |
| Layout A migration     | Migrate in this cycle                                                                          | Validates the API against real code immediately                                                              |
| Dead code              | Remove all deprecated code                                                                     | No `glyphFor`, old `Styles`, `newStyles`, unused render paths left behind                                    |

---

## New Package: `internal/tui/components/`

### `styles.go` — `Palette` + `NewPalette()`

Single struct holding all lipgloss styles. Fields are either raw `lip.Style` (used directly by callers) or the zero-value anchor for method receivers.

```go
type Palette struct {
    // Session state colors (raw, used by StatusDot / StatusGlyph)
    Running     lip.Style // ANSI 2 — green
    NeedsReview lip.Style // ANSI 5 — magenta/purple
    Dirty       lip.Style // ANSI 3 — yellow
    NoSession   lip.Style // ANSI 8 — dark gray

    // Branch glyph
    BranchGlyph lip.Style // ANSI 8 — dim

    // Diff colors (raw)
    DiffAdded   lip.Style // ANSI 2
    DiffRemoved lip.Style // ANSI 1
    DiffFiles   lip.Style // ANSI 15 — white

    // Tree structure (raw)
    RepoHeader  lip.Style // ANSI 6, bold
    SectionHead lip.Style // ANSI 8 — dim caps

    // Row selection (raw)
    Selected lip.Style // bg ANSI 4, fg ANSI 15, bold
    Armed    lip.Style // bg ANSI 1, fg ANSI 15

    // Tabs (raw)
    TabActive   lip.Style // underline + ANSI 15
    TabInactive lip.Style // ANSI 8

    // Hint bar (raw)
    HintKey  lip.Style // bold ANSI 6
    HintSep  lip.Style // ANSI 8
    HintDesc lip.Style // ANSI 8

    // Status bar mode badges (raw)
    ModeNormal  lip.Style // bg ANSI 4, fg ANSI 15, bold, pad 0 1
    ModeCommand lip.Style // bg ANSI 3, fg ANSI 0, bold, pad 0 1
    ModeInsert  lip.Style // bg ANSI 2, fg ANSI 0, bold, pad 0 1

    // Notification toast (raw)
    ToastBorder lip.Style // ANSI 5
    ToastTitle  lip.Style // bold ANSI 5
    ToastBody   lip.Style // ANSI 8
    ToastAction lip.Style // ANSI 6

    // Palette / which-key (raw)
    PaletteInput    lip.Style // ANSI 15
    PaletteSelected lip.Style // bg ANSI 4, fg ANSI 15
    PaletteHint     lip.Style // ANSI 8
    PaletteBorder   lip.Style // ANSI 8

    // Misc (raw)
    Divider   lip.Style // ANSI 8
    Inactive  lip.Style // ANSI 8, italic — "⟂ offline", placeholder text
    StatusMsg lip.Style // ANSI 3 — bottom bar feedback ("repaired: branch-1")
    Timestamp lip.Style // ANSI 8
}
```

Raw fields are used directly: `p.RepoHeader.Render(text)`. Method receivers (below) encapsulate multi-step logic.

---

### `statusdot.go`

```go
type SessionState int
const (
    StateRunning     SessionState = iota
    StateNeedsReview              // finished — fired by Stop hook
    StateDirty                    // uncommitted changes, no active session
    StateNoSession                // worktree exists, no tmux window
)

// SessionStateFromWorktree derives state from WorktreeStatus.
// needsReview and dirtyCount are zero-valued until WorktreeStatus gains those fields.
// When those fields land, only this function's body changes — callers and components are unaffected.
func SessionStateFromWorktree(s worktree.WorktreeStatus, needsReview bool, dirtyCount int) SessionState

// StatusDot returns a styled glyph string (with ANSI color codes).
// Use in standalone contexts (non-selected rows, status bars, kanban cards).
// Do NOT nest inside a parent style.Render() — use StatusGlyph instead.
func (p *Palette) StatusDot(state SessionState) string  // e.g. "\x1b[32m●\x1b[0m"

// StatusGlyph returns the raw glyph character with no ANSI styling.
// Use when the caller wraps the result in a parent style (e.g. Selected.Render(...)).
func (p *Palette) StatusGlyph(state SessionState) string  // e.g. "●"

// BranchLabel returns the styled ∕ glyph.
func (p *Palette) BranchLabel() string
```

**Glyph mapping** (intentional collision — color is the differentiator):

| State       | Glyph | Color           |
| ----------- | ----- | --------------- |
| Running     | `●`   | green (ANSI 2)  |
| NeedsReview | `◆`   | purple (ANSI 5) |
| Dirty       | `●`   | yellow (ANSI 3) |
| NoSession   | `○`   | gray (ANSI 8)   |

`StateRunning` and `StateDirty` share `●` by design — color alone differentiates them. Implementations must not "fix" this by changing the glyphs.

---

### `diffstat.go`

```go
func (p *Palette) DiffStat(added, removed int) string          // "+84 -12"
func (p *Palette) DirtyCount(count int) string                 // "±3"
func (p *Palette) DiffStatLine(files, added, removed int) string // "±3 +84 -12" (omits "±0" prefix)
```

---

### `tabbar.go`

```go
type Tab struct {
    Label string
    Extra string // optional suffix, e.g. diff stat
}
// TabBar renders underline-style active indicator (R1 from wireframes).
// If activeIdx < 0 or >= len(tabs), no tab is marked active.
func (p *Palette) TabBar(tabs []Tab, activeIdx int) string
```

---

### `hintbar.go`

```go
type KeyHint struct{ Key, Desc string }

// HintBar renders the K1 persistent hint bar.
// Empty hints slice returns "". Width <= 0 returns "".
// String width measured with ansi.StringWidth (display width, not byte length).
func (p *Palette) HintBar(hints []KeyHint, width int) string
// "↵ attach · j/k move · ..." truncated to width
```

---

### `section.go`

```go
// SectionHeader renders a T2/T3 tree section header: "RUNNING · 2"
// Width <= 0 returns "".
func (p *Palette) SectionHeader(label string, count int, width int) string
```

---

### `cmdpalette.go`

```go
type PaletteItem struct{ Command, Hint string }

// CommandPalette renders the K3 command palette overlay.
// ": query█" input line + item list with right-aligned hints.
// selectedIdx out of range → no item highlighted.
// width < 6 → returns "".
func (p *Palette) CommandPalette(query string, items []PaletteItem, selectedIdx, width int) string
```

---

### `notification.go`

```go
type ToastKind int
const ( ToastNeedsReview, ToastInfo, ToastError )

type Toast struct {
    Kind   ToastKind
    Title  string // bold first line
    Body   string // secondary text (omitted if empty)
    Action string // e.g. "⏎ to attach" (omitted if empty)
}

// Notification renders a bordered toast box for top-right placement (layout E).
// maxWidth < 6 → returns "".
func (p *Palette) Notification(t Toast, maxWidth int) string
```

---

### `statusbar.go`

```go
type StatusBarMode int
const ( ModeNormal, ModeCommand, ModeInsert )

type StatusBarModel struct {
    Mode       StatusBarMode
    Breadcrumb string       // "repo-a > branch-1"
    State      SessionState
    StateLabel string       // "running" or "2 running · 1 done"
    Added      int
    Removed    int
    Index      int          // 1-based; ignored when Total == 0
    Total      int
}

// StatusBar renders the layout-B style bottom bar across the full width.
// Layout: [mode badge + breadcrumb] ... [dot + label] ... [diff stat + position]
func (p *Palette) StatusBar(m StatusBarModel, width int) string
```

---

### `whichkey.go`

```go
type WhichKeyEntry struct{ Key, Desc string }

// WhichKeyPopup renders the K2 which-key popup in a multi-column bordered box.
// cols < 1 → treated as 1. maxWidth < 6 → returns "".
func (p *Palette) WhichKeyPopup(title string, entries []WhichKeyEntry, cols, maxWidth int) string
```

---

## Layout A Migration (`internal/tui/worktree/`)

### Files changed

| File            | Change                                                                                           |
| --------------- | ------------------------------------------------------------------------------------------------ |
| `styles.go`     | **Deleted** — fully replaced by `tuicomponents.Palette`                                          |
| `model.go`      | `styles Styles` field → `palette *tuicomponents.Palette`; render calls updated (see table below) |
| `model_test.go` | `makeTestModel`: `styles: newStyles()` → `palette: tuicomponents.NewPalette()`                   |
| `tree.go`       | `glyphFor()` **removed**; callers replaced (see below)                                           |

### Render call mapping

| Old (`m.styles.*`)                               | New (`m.palette.*`)                                  | Notes                                          |
| ------------------------------------------------ | ---------------------------------------------------- | ---------------------------------------------- |
| `ActiveGlyph.Render("●")` in non-selected rows   | `StatusDot(SessionStateFromWorktree(s, false, 0))`   | styled glyph                                   |
| `InactiveGlyph.Render("○")` in non-selected rows | `StatusDot(SessionStateFromWorktree(s, false, 0))`   | styled glyph                                   |
| glyph inside `SelectedRow.Render(plainText)`     | `StatusGlyph(SessionStateFromWorktree(s, false, 0))` | raw char, parent styles it                     |
| `ActiveTab.Render(l)` / `InactiveTab.Render(l)`  | `TabBar(tabs, activeIdx)`                            |                                                |
| `HintBar.Render(hint)` (inline string)           | `HintBar(hints, width)`                              |                                                |
| `RepoHeader.Render(text)`                        | `RepoHeader.Render(text)`                            | direct raw field                               |
| `SelectedRow.Render(text)`                       | `Selected.Render(text)`                              | direct raw field                               |
| `ArmedRow.Render(text)`                          | `Armed.Render(text)`                                 | direct raw field                               |
| `Divider.Render("│")`                            | `Divider.Render("│")`                                | direct raw field                               |
| `OfflinePlaceholder.Render(...)`                 | `Inactive.Render(...)`                               | direct raw field                               |
| `HelpKey.Render(...)`                            | `HintKey.Render(...)`                                | direct raw field, used in `renderHelpOverlay`  |
| `HelpBorder.Render(...)`                         | `PaletteBorder.Render(...)`                          | direct raw field, used in `renderHelpOverlay`  |
| `StatusMsg.Render(...)`                          | `StatusMsg.Render(...)`                              | direct raw field (explicit on Palette, ANSI 3) |

### `glyphFor` replacement

`glyphFor()` in `tree.go` is removed. Its two call sites in `renderLeft()` become:

- Non-selected rows: `p.StatusDot(tuicomponents.SessionStateFromWorktree(r.status, false, 0))`
- Selected rows (inside `SelectedRow.Render`): `p.StatusGlyph(tuicomponents.SessionStateFromWorktree(r.status, false, 0))`

### What is NOT migrated

- `renderHelpOverlay()` logic stays inline in `model.go` — layout A-specific, not shared. Uses `p.HintKey`, `p.PaletteBorder`, `p.RepoHeader` as raw fields.
- `renderAgentContent()`, `renderDiffContent()` — layout-specific, stay inline.
- `cmdpalette.go`, `notification.go`, `statusbar.go`, `whichkey.go`, `section.go` — created now for future layouts B–E; not consumed by layout A yet.

---

## Tests

### New: `internal/tui/components/statusdot_test.go`

Required test cases for `SessionStateFromWorktree`:

- `WindowActive: true, needsReview: false, dirty: 0` → `StateRunning`
- `WindowActive: true, needsReview: true, dirty: 0` → `StateNeedsReview`
- `WindowActive: false, needsReview: false, dirty: 3` → `StateDirty`
- `WindowActive: false, needsReview: false, dirty: 0` → `StateNoSession`

Required tests for `StatusDot` / `StatusGlyph`:

- `StatusGlyph` returns raw chars (`●`, `◆`, `●`, `○`) with no ANSI escape bytes
- `StatusDot` output contains the expected glyph character

### New: `internal/tui/components/diffstat_test.go`

- `DiffStat(84, 12)` output contains `"+84"` and `"-12"`
- `DirtyCount(3)` output contains `"±3"`
- `DiffStatLine(0, 84, 12)` does not contain `"±0"` prefix
- `DiffStatLine(3, 84, 12)` contains `"±3"`, `"+84"`, `"-12"`

### New: `internal/tui/components/tabbar_test.go`

- Active tab output contains the label
- Inactive tab output contains the label
- `activeIdx < 0` or `activeIdx >= len(tabs)` does not panic
- Empty tabs slice returns `""`

### New: `internal/tui/components/hintbar_test.go`

- Output contains key and description strings
- Empty hints returns `""`
- Width truncation: output display width ≤ width (measured with `ansi.StringWidth`)
- `width <= 0` returns `""`

### Existing: `internal/tui/worktree/model_test.go`

- `makeTestModel`: update `styles: newStyles()` → `palette: tuicomponents.NewPalette()`
- All existing tests must continue to pass without behavior change

---

## Files to Create

```
internal/tui/components/
├── styles.go
├── statusdot.go
├── statusdot_test.go
├── diffstat.go
├── diffstat_test.go
├── tabbar.go
├── tabbar_test.go
├── hintbar.go
├── hintbar_test.go
├── section.go
├── cmdpalette.go
├── notification.go
├── statusbar.go
└── whichkey.go
```

## Files to Modify

```
internal/tui/worktree/model.go
internal/tui/worktree/model_test.go
internal/tui/worktree/tree.go
```

## Files to Delete

```
internal/tui/worktree/styles.go
```

---

## Verification Steps

- [x] `go build ./...` passes
- [x] `go test ./...` passes (including new component tests)
- [x] `go vet ./...` clean
- [x] `make lint` clean — no `//nolint` comments
- [ ] `dg wt ui` launches and renders correctly (manual smoke test)
- [x] No deprecated symbols remain:

```bash
grep -r "newStyles\|m\.styles\.\|glyphFor\|OfflinePlaceholder\|ActiveGlyph\|InactiveGlyph\|DirtyAdded\|DirtyRemoved\|WorktreeRow\|HelpKey\|HelpBorder\|ArmedRow\|SelectedRow" \
  internal/tui/worktree/
# Should return no results
```
