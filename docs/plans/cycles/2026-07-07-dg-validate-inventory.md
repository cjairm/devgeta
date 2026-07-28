# Cycle: `dg validate` + inventory dashboard for `dg list`

**Date:** 2026-07-07
**Estimated Duration:** ~6-9 hours
**Status:** Done

---

## 1. Domain Context

[ROADMAP.md](../../../ROADMAP.md) lists `dg validate` as planned: "Verify current configuration
is valid" + "Check if all dependencies are met." `dg list` (shipped v0.28.0,
[2026-07-06-dg-list.md](2026-07-06-dg-list.md)) already reads `global_config.yaml` and prints
what devgeta _thinks_ is installed, grouped by category — but never checks whether those items are
_actually still present_ on the system. That gap is what `dg validate` closes: drift detection
between tracked state and system reality.

**Decision (confirmed with user 2026-07-07):** MVP scope is drift detection only — for every item
devgeta tracked (either as something it installed, or as something it found pre-existing), confirm
it's still actually present on the system (package manager / binary / version-command check).
Neovim-style explicit dependency chains (make, gcc, ripgrep, etc.) are **not** special-cased —
those prerequisites are already tracked as ordinary `"package"` items (see
`internal/apps/neovim/deps.go`), so generic drift detection covers them for free.

**Decision (confirmed with user 2026-07-07):** rather than building `dg validate` as a second
plain-text report next to `dg list`, both commands share one data model and one interactive
dashboard. `dg list` shows everything; `dg validate` opens the same screen with a "problems only"
filter pre-applied. Non-interactive contexts (piped output, CI, `--plain`) still get plain-text
output — `dg list`'s existing table is unchanged, and `dg validate` gets a new plain table with a
STATUS column and a non-zero exit code on any missing item, so it stays usable in scripts/CI even
though the default is now an interactive dashboard.

**Visual authority (clarified with user 2026-07-07):** the design target is
[docs/designs/dg_worktree_wireframes.pdf](../../designs/dg_worktree_wireframes.pdf) — **not** the
current `internal/tui/worktree` implementation. The existing worktree TUI does not yet fully match
those wireframes, so "make it look like the worktree TUI" would copy the wrong thing. Use
`internal/tui/worktree/model.go` only as a _code-structure_ reference (Bubbletea program wiring,
Init/Update/View, testing pattern) and build the dashboard's _look_ directly from the wireframes'
visual grammar (see Engineer Context for the concrete element list). Where a wireframe element
needs a new reusable component, add it to `internal/tui/components` so the worktree TUI can adopt
it later instead of diverging further.

**Decision (confirmed with user 2026-07-07):** read-only for this cycle. No in-TUI repair/reinstall
actions — that's flagged as an explicit follow-up below, not built now.

---

## 2. Engineer Context

**Relevant files and their purposes:**

- `internal/config/fromFile.go:18-37` — `InstalledConfig` / `AlreadyInstalledConfig`, 7 fields each
  (`Packages, DesktopApps, Fonts, Themes, TerminalTools, DevLanguages, Databases`). Confirmed via
  Explore-agent verification: the `Themes` and `TerminalTools` buckets are **never populated by any
  current code path** — grep of every `AddToInstalled`/`AddToAlreadyInstalled` call site in
  `internal/` shows only `"package"`, `"desktop_app"`, `"dev_language"`, `"database"` are ever
  passed as `itemType`. The collector must handle these two buckets as always-empty without special
  logic (just don't error on zero items).
- `cmd/list.go:26-69` — `categoryDefs`: the existing category→field accessor table. The new
  collector should mirror this same 7-category vocabulary (`packages`, `desktop_apps`, `fonts`,
  `themes`, `terminal_tools`, `dev_languages`, `databases`) so `dg list` and `dg validate` agree on
  grouping.
- `internal/commands/factory.go:5-25` — `Command` interface: `IsPackageInstalled(name) (bool,
error)` and `IsDesktopAppInstalled(name) (bool, error)`, already platform-dispatched (macOS:
  `brew list` / `brew list --cask` in `internal/commands/macos.go:177-187`; Debian: `dpkg -l` /
  `.desktop` file scan in `internal/commands/debian.go:217-232` — different mechanism per platform,
  already abstracted behind the interface). Use `commands.NewCommand()` (platform factory,
  `internal/commands/factory.go:27-41`) for real runs, `MockCommand` in tests.
- **Fonts are NOT checked via the `Command` interface** — it has no font method. The right
  mechanism is `BaseCommandExecutor.IsFontPresent(fontName) (bool, error)`
  (`internal/commands/base.go:161-192`): uses `fc-list : family` when fontconfig is available,
  falling back to scanning the user/system font directories for `.ttf`/`.otf`/`.woff` files. This
  is a _real_ font check on both platforms, is what Debian's font install path already uses
  (`internal/commands/debian.go:45`), and is already mockable via
  `MockBaseCommand.IsFontPresentResult` (`internal/commands/mock.go:262-265`). The collector
  therefore carries both a `Command` and a `BaseCommandExecutor` — the codebase's standard
  `{Cmd, Base}` pair. Do **not** add an `IsFontInstalled` method to `Command`; it would duplicate
  `IsFontPresent`.
- **`MockCommand` gap (prerequisite fix, in scope):** `MockCommand.IsPackageInstalled` /
  `IsDesktopAppInstalled` (`internal/commands/mock.go:103-109`) ignore the name argument — they
  return a single global boolean (`m.PackageInstalled` / `m.DesktopAppInstalled`) for every
  package, and can never return an error. Collector tests need to simulate "git present, tmux
  missing" and "check errored" per item. Extend the mock with per-name overrides (pattern already
  exists in the same file: `MaybeInstallErrors map[string]error`):
  `PackageInstalledMap map[string]bool`, `DesktopAppInstalledMap map[string]bool`, plus matching
  `map[string]error` fields for error injection; map hit wins, otherwise fall back to the existing
  global boolean (keeps all current tests passing unchanged).
- `internal/tooling/languages/languages.go:90-96` — private `isLanguageInstalledOnSystem(langCfg
LanguageConfig) bool`, runs a version command via `dl.Base.ExecCommand`. `GetLanguageConfigs()`
  (in `internal/tooling/languages/util.go:58`, not `languages.go`) returns `[]LanguageConfig` with
  a `Name` field for lookup-by-name.
- `internal/tooling/databases/databases.go:80-86` — same pattern: private
  `isDatabaseInstalledOnSystem(dbCfg DatabaseConfig) bool`, `GetDatabaseConfigs()` in
  `internal/tooling/databases/util.go:51`.
- **Constructor side-effect warning** (found during design verification): `languages.New()` /
  `databases.New()` are **not** side-effect-free — they call `detectPreInstalledLanguages()` /
  `detectPreInstalledDatabases()`, which shell out to version-check _every_ configured
  language/database on the host (not just the one being checked) and can silently `gc.Save()`
  (write `global_config.yaml`) if they detect something newly pre-existing. The inventory collector
  must **not** call `New()`. Instead construct directly as a struct literal — `&languages.DevLanguages{Cmd:
cmd.NewCommand(), Base: cmd.NewBaseCommand()}` (same for `Databases`) — bypassing the detection
  side effect entirely. This mirrors the existing test-mocking convention already documented in
  [CLAUDE.md](../../../CLAUDE.md) §6 ("always use `&foo.Foo{Cmd: mockApp.Cmd, Base: mockApp.Base}`
  instead of `foo.New()`").
- `internal/tui/worktree/run.go`, `internal/tui/worktree/model.go` (980 lines) — existing Bubbletea
  v2 dashboard pattern to follow **for code structure only**: `tea.NewProgram(model).Run()`,
  `Init`/`Update`/`View`. Do **not** treat its rendered output as the visual reference — it does not
  yet match the wireframes (see "Visual authority" in §1).
- `internal/tui/components/styles.go` — `Palette` (lipgloss styles: colors, section headers, hint
  bar, etc.) — reusable directly. `internal/tui/components/statusdot.go` — **worktree-specific**
  (`SessionState`: running/needs-review/dirty/no-session); do not extend or repurpose this file for
  inventory status. Define a separate, local status-render helper in the new `internal/tui/inventory`
  package that reuses `Palette`'s raw colors but has its own OK/MISSING/UNKNOWN vocabulary.
- No existing TTY-detection helper in the codebase — add one using stdlib only (`os.Stdout.Stat()` +
  `os.ModeCharDevice`), no new dependency.

**Visual grammar to implement (from the wireframes PDF — this is the authoritative look):**

- **Bordered pane with embedded title** — rounded box border with the pane label rendered into the
  top border line (wireframes: `╭─ 1 worktrees ─╮`-style; here: e.g. `inventory` / category name).
  No such component exists in `internal/tui/components` yet — build it there, reusable, so the
  worktree TUI can adopt it later.
- **Tree/grouping grammar (T1 page)** — category headers styled like `RepoHeader` (bold cyan) with a
  right-aligned item count; child rows indented beneath, `▾`/`▸` collapse-expand glyphs on group
  headers.
- **Status glyph + color, not text badges (legend page)** — colored dot prefix per row: `●` green =
  OK, `●` red = MISSING, `○` gray = UNKNOWN (mirrors the legend's filled-vs-hollow convention;
  color differentiates filled states, exactly like the worktree legend reuses `●` for
  running/dirty). Source shown as a dim suffix tag on the row (e.g. `pre-existing`) styled with
  `Palette.Inactive`, not a boxed badge.
- **Counts summary line (layout-A page)** — dim caps line under the list, wireframe-style
  `4 REPOS · 9 WORKTREES · 2 RUNNING` → here `7 CATEGORIES · 142 ITEMS · 2 MISSING`, styled with
  `Palette.SectionHead`.
- **Persistent hint bar (K1 page)** — always-on footer via the existing
  `internal/tui/components/hintbar.go`: `j/k move · h/l collapse/expand · / filter · p problems ·
g group · q quit` with `HintKey`/`HintSep`/`HintDesc` styles.

**Key functions/types involved (new):**

- `internal/inventory.Item`, `internal/inventory.ItemState`, and a `Collector` struct carrying the
  standard `{Cmd cmd.Command, Base cmd.BaseCommandExecutor}` pair with a
  `Collect(gc *config.GlobalConfig) []Item` method (struct rather than free function so tests can
  inject `MockCommand`/`MockBaseCommand`)
- **`ItemState` semantics (normative):** `StateOK` — the presence check ran and returned
  `(true, nil)`. `StateMissing` — the check ran and definitively returned `(false, nil)`.
  `StateUnknown` — the check itself failed (`err != nil`, e.g. `brew` not on PATH, `dpkg` errored);
  record the error in `Item.Detail`. Never conflate a failed check with a missing item: only
  `StateMissing` affects `dg validate`'s exit code.
- `Collect` is read-only by contract: it never calls `gc.Save()` (or anything else that writes
  `global_config.yaml`)
- `languages.DevLanguages.IsInstalledOnSystem(name string) bool` (new exported wrapper)
- `databases.Databases.IsInstalledOnSystem(name string) bool` (new exported wrapper)
- `internal/tui/inventory` package: `Run(gc *config.GlobalConfig, opts Options) error` where
  `Options` carries `ProblemsOnly bool` and `Category string` (pre-filter), model following the
  `internal/tui/worktree` pattern. The list view must scroll when content exceeds the viewport
  (follow the worktree model's height-handling approach) — do not assume the inventory fits one
  screen

**Testing patterns used in this area:**

- `internal/inventory`: mock via the extended `commands.MockCommand` (per-name maps, see the
  MockCommand-gap bullet above) + `MockBaseCommand.IsFontPresentResult` for fonts, and the
  `&languages.DevLanguages{Cmd: mock.Cmd, Base: mock.Base}` / `&databases.Databases{...}` literal
  pattern — never `New()`. No real commands executed (`testutil.VerifyNoRealCommands`).
- Per-state coverage: at least one test each for OK (`true, nil`), MISSING (`false, nil`), and
  UNKNOWN (`false, err`) per check mechanism.
- **No-write assertion:** collector tests call `testutil.IsolateXDGDirs(t)` and assert
  `global_config.yaml` was not created/modified by `Collect` (guards the read-only contract and the
  `New()` side-effect regression).
- `internal/tui/inventory`: follow `internal/tui/worktree/model_test.go`'s existing Bubbletea test
  pattern.
- `cmd/validate_test.go`, updated `cmd/list_test.go`: exercise the plain-path output and exit codes
  — the scriptable/CI-relevant surface.

**Commands to run tests:**

```bash
go test ./internal/inventory/
go test ./internal/tui/inventory/
go test ./cmd/
go test ./...
make lint
```

---

## 3. Objective

Ship `dg validate` (new command) and upgrade `dg list`, both backed by one shared drift-detection
data model (`internal/inventory`) and one shared interactive dashboard
(`internal/tui/inventory`): in a terminal, both commands open the dashboard (grouped-by-category
list with a live OK/MISSING/UNKNOWN status per item; `dg validate` pre-filters to problems only);
piped/CI/`--plain` contexts get plain-text output, with `dg validate` exiting non-zero if anything
tracked is missing.

---

## 4. Scope Boundary

### In Scope

- [x] `internal/inventory` package: `Item`, `ItemState`, `Collector` (with `{Cmd, Base}` pair),
      dispatching presence checks per category (packages/desktop_apps via `commands.Command`; fonts
      via `BaseCommandExecutor.IsFontPresent`; dev_languages/databases via new exported
      `IsInstalledOnSystem` wrappers; themes/terminal_tools always-empty). Categories with zero
      tracked items are omitted from both the dashboard and plain output (matches `dg list`'s
      current skip-empty behavior)
- [x] Extend `commands.MockCommand` with per-name presence/error maps (prerequisite for collector
      tests — see §2; small isolated change, existing tests unaffected)
- [x] Exported `IsInstalledOnSystem` on `languages.DevLanguages` and `databases.Databases`
- [x] TTY-detection helper (stdlib only)
- [x] `internal/tui/inventory` package: shared Bubbletea dashboard built to the wireframes' visual
      grammar (see §2 "Visual grammar to implement": bordered pane with embedded title, T1-style
      category groups with counts and `▾`/`▸` collapse, legend-style status glyphs, counts summary
      line, K1 persistent hint bar) — read-only, no mutating actions
- [x] New reusable bordered-pane-with-title component in `internal/tui/components` (needed by the
      wireframe look; doesn't exist yet)
- [x] `cmd/validate.go`: new `dg validate` command — TTY → dashboard with problems-only filter;
      non-TTY/`--plain` → plain table with STATUS column, exit 1 on any MISSING, 0 otherwise
      (UNKNOWN does not fail the exit code)
- [x] `cmd/list.go`: TTY → dashboard (no filter); non-TTY/`--plain` → existing plain table,
      unchanged
- [x] **`--category` interaction (both commands):** in dashboard mode, `--category=<name>` opens
      the dashboard pre-filtered to that category only; in `--plain`/non-TTY mode it filters the
      table exactly as `dg list --category` does today. `dg validate` gains the same `--category`
      flag for parity
- [x] `--plain` flag on both commands to force non-interactive output even in a TTY (defined
      per-command, not as a root persistent flag — it's meaningless for commands without a TUI
      mode)
- [x] Tests for `internal/inventory`, `internal/tui/inventory`, and both commands' plain-path
      behavior (including exit codes)
- [x] Update `docs/spec.md` and `ROADMAP.md` (move `dg validate` to Implemented; note the `dg list`
      TUI upgrade)

### Explicitly Out of Scope

- In-TUI repair/reinstall actions (pressing a key to fix a MISSING item) — flagged as a follow-up;
  pulls in install/configure error-handling and confirmation flows, a separate cycle
- Explicit neovim-style prerequisite-chain modeling for other apps — generic drift detection over
  tracked `"package"` items already covers this
- Real presence checks for `themes`/`terminal_tools` — those buckets are dead code today; wiring
  them up is out of scope until something actually populates them (e.g. the planned `dg change
--theme` command)
- Any change to `global_config.yaml`'s schema (no version/timestamp tracking — separate, already
  flagged in ROADMAP.md)
- Config _content_ validation (e.g. malformed YAML structure) beyond `gc.Load()`'s existing error
  return — out of scope for this cycle, which is about drift, not schema validation
- Retrofitting the existing `internal/tui/worktree` dashboard to full wireframe fidelity — known
  gap (the current worktree TUI does not match the wireframes PDF), but fixing it belongs to the
  worktree UX cycle, not this one. This cycle only ensures new shared components land in
  `internal/tui/components` so that retrofit gets cheaper, and must not copy the worktree TUI's
  current (off-design) rendering

**Scope is locked.** If something out-of-scope turns out to be needed, document it here and defer
to a follow-up cycle rather than expanding this one.

---

## 5. Implementation Plan

This plan was produced by the writing-plans skill from the spec above and executed task-by-task
via subagent-driven-development. All 10 tasks shipped, each reviewed for spec compliance and code
quality before moving to the next (see commit history on branch `dg-validate-inventory` for the
full task-by-task trail, including two review round-trips: a `BorderedPane` off-by-one/title-overflow
fix in Task 6, and added cursor-navigation test coverage in Task 7). The step-by-step instructions
below are preserved as shipped — code blocks reflect what was actually implemented, not a draft.

### Task 1: Extend command mocks for per-item presence/error simulation

**Why first:** every later test (Collector, languages/databases wrappers) needs to simulate
"item A present, item B missing, item C errored" in a single test — the current mocks only support
one global boolean per mock instance.

**Files:**

- Modify: `internal/commands/mock.go`
- Test: `internal/commands/mock_test.go` (new file)

- [x] **Step 1: Write failing tests for the new per-name mock behavior**

```go
// internal/commands/mock_test.go
package commands

import (
	"errors"
	"testing"
)

func TestMockCommand_IsPackageInstalled_PerNameMap(t *testing.T) {
	m := NewMockCommand()
	m.PackageInstalledMap = map[string]bool{"git": true, "tmux": false}

	ok, err := m.IsPackageInstalled("git")
	if err != nil || !ok {
		t.Errorf("git: got (%v, %v), want (true, nil)", ok, err)
	}

	ok, err = m.IsPackageInstalled("tmux")
	if err != nil || ok {
		t.Errorf("tmux: got (%v, %v), want (false, nil)", ok, err)
	}
}

func TestMockCommand_IsPackageInstalled_FallsBackToGlobalBool(t *testing.T) {
	m := NewMockCommand()
	m.PackageInstalled = true // legacy global flag, no map entry for "unmapped"

	ok, err := m.IsPackageInstalled("unmapped")
	if err != nil || !ok {
		t.Errorf("got (%v, %v), want (true, nil) via fallback", ok, err)
	}
}

func TestMockCommand_IsPackageInstalled_PerNameError(t *testing.T) {
	m := NewMockCommand()
	wantErr := errors.New("brew: command not found")
	m.PackageInstalledErrors = map[string]error{"broken": wantErr}

	ok, err := m.IsPackageInstalled("broken")
	if ok || err != wantErr {
		t.Errorf("got (%v, %v), want (false, %v)", ok, err, wantErr)
	}
}

func TestMockCommand_IsDesktopAppInstalled_PerNameMapAndError(t *testing.T) {
	m := NewMockCommand()
	m.DesktopAppInstalledMap = map[string]bool{"docker": true}
	wantErr := errors.New("dpkg: not found")
	m.DesktopAppInstalledErrors = map[string]error{"broken-app": wantErr}

	if ok, err := m.IsDesktopAppInstalled("docker"); err != nil || !ok {
		t.Errorf("docker: got (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := m.IsDesktopAppInstalled("broken-app"); ok || err != wantErr {
		t.Errorf("broken-app: got (%v, %v), want (false, %v)", ok, err, wantErr)
	}
}

func TestMockCommand_Reset_ClearsPerNameMaps(t *testing.T) {
	m := NewMockCommand()
	m.PackageInstalledMap = map[string]bool{"git": true}
	m.DesktopAppInstalledMap = map[string]bool{"docker": true}
	m.PackageInstalledErrors = map[string]error{"x": errors.New("boom")}
	m.DesktopAppInstalledErrors = map[string]error{"y": errors.New("boom")}

	m.Reset()

	if len(m.PackageInstalledMap) != 0 || len(m.DesktopAppInstalledMap) != 0 ||
		len(m.PackageInstalledErrors) != 0 || len(m.DesktopAppInstalledErrors) != 0 {
		t.Error("Reset should clear all per-name maps")
	}
}

func TestMockBaseCommand_IsFontPresent_Error(t *testing.T) {
	m := NewMockBaseCommand()
	wantErr := errors.New("fc-list: not found")
	m.IsFontPresentResult = false
	m.IsFontPresentError = wantErr

	ok, err := m.IsFontPresent("JetBrainsMono")
	if ok || err != wantErr {
		t.Errorf("got (%v, %v), want (false, %v)", ok, err, wantErr)
	}
}
```

- [x] **Step 2: Run the tests, confirm they fail to compile**

Run: `go test ./internal/commands/ -run 'TestMockCommand|TestMockBaseCommand_IsFontPresent_Error' -v`
Expected: compile errors — `PackageInstalledMap`, `DesktopAppInstalledMap`,
`PackageInstalledErrors`, `DesktopAppInstalledErrors`, `IsFontPresentError` undefined.

- [x] **Step 3: Add the per-name fields and update the mock methods**

In `internal/commands/mock.go`, add fields to `MockCommand` (after the existing `State tracking`
block, i.e. after `DesktopAppInstalled bool`):

```go
	// Per-name presence/error overrides — map hit wins over the global bool/error above.
	PackageInstalledMap       map[string]bool
	DesktopAppInstalledMap    map[string]bool
	PackageInstalledErrors    map[string]error
	DesktopAppInstalledErrors map[string]error
```

Update `NewMockCommand`:

```go
func NewMockCommand() *MockCommand {
	return &MockCommand{
		PackageManagerInstalled:   true,
		PackageInstalled:          false,
		DesktopAppInstalled:       false,
		MaybeInstalledPkgs:        []string{},
		MaybeInstallErrors:        map[string]error{},
		PackageInstalledMap:       map[string]bool{},
		DesktopAppInstalledMap:    map[string]bool{},
		PackageInstalledErrors:    map[string]error{},
		DesktopAppInstalledErrors: map[string]error{},
	}
}
```

Replace `IsPackageInstalled` and `IsDesktopAppInstalled`:

```go
func (m *MockCommand) IsPackageInstalled(packageName string) (bool, error) {
	if m.PackageInstalledErrors != nil {
		if err, ok := m.PackageInstalledErrors[packageName]; ok {
			return false, err
		}
	}
	if m.PackageInstalledMap != nil {
		if v, ok := m.PackageInstalledMap[packageName]; ok {
			return v, nil
		}
	}
	return m.PackageInstalled, nil
}

func (m *MockCommand) IsDesktopAppInstalled(desktopAppName string) (bool, error) {
	if m.DesktopAppInstalledErrors != nil {
		if err, ok := m.DesktopAppInstalledErrors[desktopAppName]; ok {
			return false, err
		}
	}
	if m.DesktopAppInstalledMap != nil {
		if v, ok := m.DesktopAppInstalledMap[desktopAppName]; ok {
			return v, nil
		}
	}
	return m.DesktopAppInstalled, nil
}
```

Update `Reset` to clear the new maps (add at the end of the method body):

```go
	m.PackageInstalledMap = map[string]bool{}
	m.DesktopAppInstalledMap = map[string]bool{}
	m.PackageInstalledErrors = map[string]error{}
	m.DesktopAppInstalledErrors = map[string]error{}
```

In `MockBaseCommand`, add a field next to `IsFontPresentResult`:

```go
	IsFontPresentResult bool
	IsFontPresentError  error
```

Update `IsFontPresent`:

```go
func (m *MockBaseCommand) IsFontPresent(fontName string) (bool, error) {
	return m.IsFontPresentResult, m.IsFontPresentError
}
```

- [x] **Step 4: Run the tests again, confirm they pass, and run the full existing suite**

Run: `go test ./internal/commands/ -v`
Expected: all tests PASS, including the 6 new ones and every pre-existing `MockCommand`/
`MockBaseCommand` test (unchanged behavior when maps are nil/empty — nil maps are handled by the
`if m.X != nil` guards, and zero-value `IsFontPresentError` is `nil`).

Run: `go build ./...`
Expected: no errors (confirms no other package broke from the field additions).

- [x] **Step 5: Commit**

```bash
git add internal/commands/mock.go internal/commands/mock_test.go
git commit -m "test(commands): add per-name presence/error mocks for package, desktop app, font checks"
```

---

### Task 2: `languages.DevLanguages.IsInstalledOnSystem`

**Files:**

- Modify: `internal/tooling/languages/languages.go`
- Test: `internal/tooling/languages/languages_test.go`

- [x] **Step 1: Write the failing tests**

Add to `internal/tooling/languages/languages_test.go` (same file, same package — it already
imports `testutil` and `fmt`):

```go
func TestIsInstalledOnSystem_MiseLanguageOK(t *testing.T) {
	mockApp := testutil.NewMockApp()
	dl := &DevLanguages{Cmd: mockApp.Cmd, Base: mockApp.Base}
	mockApp.Base.SetExecCommandResult("v20.0.0", "", nil)

	// "node@lts" is exactly what formatSpec produces for the mise-managed Node config.
	if !dl.IsInstalledOnSystem("node@lts") {
		t.Error("expected node@lts to be detected as installed")
	}
}

func TestIsInstalledOnSystem_NativeLanguageMissing(t *testing.T) {
	mockApp := testutil.NewMockApp()
	dl := &DevLanguages{Cmd: mockApp.Cmd, Base: mockApp.Base}
	mockApp.Base.SetExecCommandResult("", "command not found", fmt.Errorf("command not found"))

	// "php" (no version suffix — PHP is UseMise: false) is what formatSpec produces for PHP.
	if dl.IsInstalledOnSystem("php") {
		t.Error("expected php to be detected as missing")
	}
}

func TestIsInstalledOnSystem_UnknownSpecReturnsFalse(t *testing.T) {
	mockApp := testutil.NewMockApp()
	dl := &DevLanguages{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if dl.IsInstalledOnSystem("cobol@1985") {
		t.Error("expected an unrecognized language spec to return false")
	}
	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
```

- [x] **Step 2: Run, confirm failure**

Run: `go test ./internal/tooling/languages/ -run TestIsInstalledOnSystem -v`
Expected: FAIL — `dl.IsInstalledOnSystem undefined (type *DevLanguages has no field or method IsInstalledOnSystem)`.

- [x] **Step 3: Implement**

In `internal/tooling/languages/languages.go`, add after `isLanguageInstalledOnSystem`:

```go
// IsInstalledOnSystem reports whether the tracked language spec (as produced by
// formatSpec — e.g. "node@lts" for mise-managed languages, "php" for native ones)
// matches a known language config and is present on the system via its version
// command. Returns false for a spec that matches no current config.
func (dl *DevLanguages) IsInstalledOnSystem(name string) bool {
	for _, langCfg := range GetLanguageConfigs() {
		if formatSpec(langCfg.Name, langCfg.Version, langCfg.UseMise) == name {
			return dl.isLanguageInstalledOnSystem(langCfg)
		}
	}
	return false
}
```

- [x] **Step 4: Run, confirm pass, run full package**

Run: `go test ./internal/tooling/languages/ -v`
Expected: all PASS.

- [x] **Step 5: Commit**

```bash
git add internal/tooling/languages/languages.go internal/tooling/languages/languages_test.go
git commit -m "feat(languages): add IsInstalledOnSystem drift-check wrapper"
```

---

### Task 3: `databases.Databases.IsInstalledOnSystem`

**Files:**

- Modify: `internal/tooling/databases/databases.go`
- Test: `internal/tooling/databases/databases_test.go`

- [x] **Step 1: Write the failing tests**

Add to `internal/tooling/databases/databases_test.go`:

```go
func TestIsInstalledOnSystem_KnownDatabaseOK(t *testing.T) {
	mockApp := testutil.NewMockApp()
	d := &Databases{Cmd: mockApp.Cmd, Base: mockApp.Base}
	mockApp.Base.SetExecCommandResult("redis-server 7.2.0", "", nil)

	if !d.IsInstalledOnSystem("redis") {
		t.Error("expected redis to be detected as installed")
	}
}

func TestIsInstalledOnSystem_KnownDatabaseMissing(t *testing.T) {
	mockApp := testutil.NewMockApp()
	d := &Databases{Cmd: mockApp.Cmd, Base: mockApp.Base}
	mockApp.Base.SetExecCommandResult("", "command not found", fmt.Errorf("command not found"))

	if d.IsInstalledOnSystem("postgresql") {
		t.Error("expected postgresql to be detected as missing")
	}
}

func TestIsInstalledOnSystem_UnknownNameReturnsFalse(t *testing.T) {
	mockApp := testutil.NewMockApp()
	d := &Databases{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if d.IsInstalledOnSystem("cassandra") {
		t.Error("expected an unrecognized database name to return false")
	}
	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
```

Confirm `fmt` and `testutil` are already imported in `databases_test.go` (they are, per the
existing `TestGetVersionCommand`-style tests) — if not, add them.

- [x] **Step 2: Run, confirm failure**

Run: `go test ./internal/tooling/databases/ -run TestIsInstalledOnSystem -v`
Expected: FAIL to compile — `IsInstalledOnSystem` undefined.

- [x] **Step 3: Implement**

In `internal/tooling/databases/databases.go`, add after `isDatabaseInstalledOnSystem`:

```go
// IsInstalledOnSystem reports whether the tracked database name matches a known
// database config and is present on the system via its version command. Returns
// false for a name that matches no current config.
func (d *Databases) IsInstalledOnSystem(name string) bool {
	for _, dbCfg := range GetDatabaseConfigs() {
		if dbCfg.Name == name {
			return d.isDatabaseInstalledOnSystem(dbCfg)
		}
	}
	return false
}
```

- [x] **Step 4: Run, confirm pass**

Run: `go test ./internal/tooling/databases/ -v`
Expected: all PASS.

- [x] **Step 5: Commit**

```bash
git add internal/tooling/databases/databases.go internal/tooling/databases/databases_test.go
git commit -m "feat(databases): add IsInstalledOnSystem drift-check wrapper"
```

---

### Task 4: TTY-detection helper

**Files:**

- Create: `cmd/tty.go`
- Test: `cmd/tty_test.go`

- [x] **Step 1: Write the failing test**

```go
// cmd/tty_test.go
package cmd

import "testing"

func TestIsInteractiveTerminal_FalseUnderTestRunner(t *testing.T) {
	// `go test` never attaches a real TTY to stdout, so this must be false —
	// this is also exactly the condition dg list/dg validate rely on to decide
	// whether to fall back to plain output in CI.
	if isInteractiveTerminal() {
		t.Error("expected isInteractiveTerminal() to be false under the test runner")
	}
}
```

- [x] **Step 2: Run, confirm failure**

Run: `go test ./cmd/ -run TestIsInteractiveTerminal -v`
Expected: FAIL to compile — `isInteractiveTerminal` undefined.

- [x] **Step 3: Implement**

```go
// cmd/tty.go
/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import "os"

// isInteractiveTerminal reports whether stdout is attached to a real terminal.
// Used by dg list / dg validate to decide between the interactive dashboard and
// plain-text output (piped, redirected, or CI contexts always get plain text).
func isInteractiveTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
```

- [x] **Step 4: Run, confirm pass**

Run: `go test ./cmd/ -run TestIsInteractiveTerminal -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add cmd/tty.go cmd/tty_test.go
git commit -m "feat(cmd): add stdout TTY-detection helper"
```

---

### Task 5: `internal/inventory` package — Item, ItemState, Collector

**Files:**

- Create: `internal/inventory/inventory.go`
- Test: `internal/inventory/inventory_test.go`

This is the shared data model both `dg list` and `dg validate` read from. `Collect` must **never**
call `gc.Save()`, and must construct `languages.DevLanguages` / `databases.Databases` as struct
literals (never `languages.New()` / `databases.New()`), because those constructors run
`detectPreInstalled*`, which shells out for _every_ configured language/database (not just tracked
ones) and can silently write `global_config.yaml`.

- [x] **Step 1: Write the failing tests**

```go
// internal/inventory/inventory_test.go
package inventory

import (
	"errors"
	"os"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

func TestCollect_PackageStates_OK_Missing_Unknown(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Cmd.PackageInstalledMap = map[string]bool{"git": true, "tmux": false}
	mockApp.Cmd.PackageInstalledErrors = map[string]error{"broken-pkg": errors.New("brew: not found")}

	gc := &config.GlobalConfig{}
	gc.Installed.Packages = []string{"git", "tmux", "broken-pkg"}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	byName := map[string]Item{}
	for _, it := range items {
		byName[it.Name] = it
	}

	if byName["git"].State != StateOK {
		t.Errorf("git: got state %v, want StateOK", byName["git"].State)
	}
	if byName["tmux"].State != StateMissing {
		t.Errorf("tmux: got state %v, want StateMissing", byName["tmux"].State)
	}
	if byName["broken-pkg"].State != StateUnknown {
		t.Errorf("broken-pkg: got state %v, want StateUnknown", byName["broken-pkg"].State)
	}
	if byName["broken-pkg"].Detail == "" {
		t.Error("broken-pkg: expected Detail to carry the check error")
	}
	for _, name := range []string{"git", "tmux", "broken-pkg"} {
		if byName[name].Category != "packages" {
			t.Errorf("%s: got category %q, want %q", name, byName[name].Category, "packages")
		}
		if byName[name].Source != "installed" {
			t.Errorf("%s: got source %q, want %q", name, byName[name].Source, "installed")
		}
	}
}

func TestCollect_AlreadyInstalledSource(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Cmd.PackageInstalledMap = map[string]bool{"curl": true}

	gc := &config.GlobalConfig{}
	gc.AlreadyInstalled.Packages = []string{"curl"}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	if len(items) != 1 || items[0].Source != "pre-existing" {
		t.Fatalf("got %+v, want a single pre-existing curl item", items)
	}
}

func TestCollect_DesktopAppAndFontStates(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Cmd.DesktopAppInstalledMap = map[string]bool{"docker": true}
	mockApp.Base.IsFontPresentResult = false
	mockApp.Base.IsFontPresentError = errors.New("fc-list: not found")

	gc := &config.GlobalConfig{}
	gc.Installed.DesktopApps = []string{"docker"}
	gc.Installed.Fonts = []string{"JetBrainsMono"}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	var dockerState, fontState ItemState
	for _, it := range items {
		if it.Name == "docker" {
			dockerState = it.State
		}
		if it.Name == "JetBrainsMono" {
			fontState = it.State
		}
	}
	if dockerState != StateOK {
		t.Errorf("docker: got %v, want StateOK", dockerState)
	}
	if fontState != StateUnknown {
		t.Errorf("JetBrainsMono: got %v, want StateUnknown", fontState)
	}
}

func TestCollect_DevLanguageAndDatabaseStates(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("v20.0.0", "", nil) // every version check succeeds

	gc := &config.GlobalConfig{}
	gc.Installed.DevLanguages = []string{"node@lts"} // matches mise-managed Node config
	gc.Installed.Databases = []string{"redis"}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	for _, it := range items {
		if it.Name == "node@lts" && it.State != StateOK {
			t.Errorf("node@lts: got %v, want StateOK", it.State)
		}
		if it.Name == "redis" && it.State != StateOK {
			t.Errorf("redis: got %v, want StateOK", it.State)
		}
	}
}

func TestCollect_ThemesAndTerminalToolsAlwaysEmptyIsHarmless(t *testing.T) {
	mockApp := testutil.NewMockApp()
	gc := &config.GlobalConfig{} // Themes and TerminalTools are never populated by real code paths

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	if len(items) != 0 {
		t.Errorf("expected zero items for an empty config, got %d", len(items))
	}
}

func TestCollect_EmptyCategoriesProduceNoItems(t *testing.T) {
	mockApp := testutil.NewMockApp()
	gc := &config.GlobalConfig{}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	if items != nil {
		t.Errorf("expected nil/empty items for a fully empty config, got %+v", items)
	}
}

func TestCollect_DoesNotWriteGlobalConfig(t *testing.T) {
	// config.GlobalConfig.Load/Save resolve their file path from the package-level
	// paths.Paths.Config.Root var, not from XDG_CONFIG_HOME directly — that var is
	// set once when the paths package loads, so t.Setenv (testutil.IsolateXDGDirs)
	// does not affect it. testutil.SetupCompleteTest is the pattern that actually
	// redirects paths.Paths.Config.Root, and is required for any test that reads or
	// writes global_config.yaml through GlobalConfig.Load/Save/Create.
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("gc.Load() failed: %v", err)
	}
	gc.Installed.Packages = []string{"git"}
	if err := gc.Save(); err != nil {
		t.Fatalf("gc.Save() failed: %v", err)
	}

	before, err := os.ReadFile(tc.ConfigPath)
	if err != nil {
		t.Fatalf("reading config before Collect: %v", err)
	}

	mockApp := testutil.NewMockApp()
	mockApp.Cmd.PackageInstalledMap = map[string]bool{"git": true}
	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	c.Collect(gc)

	after, err := os.ReadFile(tc.ConfigPath)
	if err != nil {
		t.Fatalf("reading config after Collect: %v", err)
	}
	if string(before) != string(after) {
		t.Error("Collect must not modify global_config.yaml on disk")
	}
}
```

- [x] **Step 2: Run, confirm failure**

Run: `go test ./internal/inventory/ -v`
Expected: FAIL to compile — package `internal/inventory` doesn't exist yet.

- [x] **Step 3: Implement**

```go
// internal/inventory/inventory.go
package inventory

import (
	cmdpkg "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/databases"
	"github.com/cjairm/devgeta/internal/tooling/languages"
)

// ItemState is the result of a live drift check for one tracked item.
type ItemState int

const (
	// StateOK: the presence check ran and definitively found the item.
	StateOK ItemState = iota
	// StateMissing: the check ran and definitively did not find the item.
	StateMissing
	// StateUnknown: the check itself failed to run (e.g. brew/dpkg unavailable).
	// Never conflated with StateMissing — only StateMissing affects `dg validate`'s exit code.
	StateUnknown
)

func (s ItemState) String() string {
	switch s {
	case StateOK:
		return "OK"
	case StateMissing:
		return "MISSING"
	default:
		return "UNKNOWN"
	}
}

// Item is one tracked piece of devgeta state plus its live drift-check result.
type Item struct {
	Name     string
	Category string // "packages", "desktop_apps", "fonts", "themes", "terminal_tools", "dev_languages", "databases"
	Source   string // "installed" (devgeta installed it) or "pre-existing" (found already on the system)
	State    ItemState
	Detail   string // populated when State == StateUnknown (the check error's message)
}

// CategoryInfo pairs a category key with its display label, in the fixed display
// order shared by `dg list` and `dg validate`.
type CategoryInfo struct {
	Key   string
	Label string
}

// Categories is the canonical 7-category vocabulary and display order.
var Categories = []CategoryInfo{
	{Key: "packages", Label: "Packages"},
	{Key: "desktop_apps", Label: "Desktop Apps"},
	{Key: "fonts", Label: "Fonts"},
	{Key: "themes", Label: "Themes"},
	{Key: "terminal_tools", Label: "Terminal Tools"},
	{Key: "dev_languages", Label: "Dev Languages"},
	{Key: "databases", Label: "Databases"},
}

// Collector runs presence checks for every item devgeta has tracked, for both
// the "installed" and "already_installed" buckets of global_config.yaml.
type Collector struct {
	Cmd  cmdpkg.Command
	Base cmdpkg.BaseCommandExecutor
}

// Collect is read-only by contract: it never calls gc.Save() or otherwise
// writes global_config.yaml, and never calls languages.New() / databases.New()
// (which would shell out for every configured — not just tracked — language and
// database, and can silently persist newly-detected pre-existing installs).
func (c *Collector) Collect(gc *config.GlobalConfig) []Item {
	dl := &languages.DevLanguages{Cmd: c.Cmd, Base: c.Base}
	db := &databases.Databases{Cmd: c.Cmd, Base: c.Base}

	var items []Item
	items = append(items, c.collectCategory("packages", gc.Installed.Packages, gc.AlreadyInstalled.Packages, c.checkPackage)...)
	items = append(items, c.collectCategory("desktop_apps", gc.Installed.DesktopApps, gc.AlreadyInstalled.DesktopApps, c.checkDesktopApp)...)
	items = append(items, c.collectCategory("fonts", gc.Installed.Fonts, gc.AlreadyInstalled.Fonts, c.checkFont)...)
	items = append(items, c.collectCategory("themes", gc.Installed.Themes, gc.AlreadyInstalled.Themes, checkNotImplemented)...)
	items = append(items, c.collectCategory("terminal_tools", gc.Installed.TerminalTools, gc.AlreadyInstalled.TerminalTools, checkNotImplemented)...)
	items = append(items, c.collectCategory("dev_languages", gc.Installed.DevLanguages, gc.AlreadyInstalled.DevLanguages, checkLanguageFn(dl))...)
	items = append(items, c.collectCategory("databases", gc.Installed.Databases, gc.AlreadyInstalled.Databases, checkDatabaseFn(db))...)
	return items
}

type checkFn func(name string) (ItemState, string)

func (c *Collector) collectCategory(category string, installed, alreadyInstalled []string, check checkFn) []Item {
	var items []Item
	for _, name := range installed {
		state, detail := check(name)
		items = append(items, Item{Name: name, Category: category, Source: "installed", State: state, Detail: detail})
	}
	for _, name := range alreadyInstalled {
		state, detail := check(name)
		items = append(items, Item{Name: name, Category: category, Source: "pre-existing", State: state, Detail: detail})
	}
	return items
}

func (c *Collector) checkPackage(name string) (ItemState, string) {
	ok, err := c.Cmd.IsPackageInstalled(name)
	return stateFromCheck(ok, err)
}

func (c *Collector) checkDesktopApp(name string) (ItemState, string) {
	ok, err := c.Cmd.IsDesktopAppInstalled(name)
	return stateFromCheck(ok, err)
}

func (c *Collector) checkFont(name string) (ItemState, string) {
	ok, err := c.Base.IsFontPresent(name)
	return stateFromCheck(ok, err)
}

func checkLanguageFn(dl *languages.DevLanguages) checkFn {
	return func(name string) (ItemState, string) {
		if dl.IsInstalledOnSystem(name) {
			return StateOK, ""
		}
		return StateMissing, ""
	}
}

func checkDatabaseFn(db *databases.Databases) checkFn {
	return func(name string) (ItemState, string) {
		if db.IsInstalledOnSystem(name) {
			return StateOK, ""
		}
		return StateMissing, ""
	}
}

// checkNotImplemented backs the themes/terminal_tools categories, which no
// current code path ever populates. If a future feature (e.g. `dg change
// --theme`) starts populating them, tracked items surface as UNKNOWN here
// until a real presence check is added — never silently reported OK or MISSING.
func checkNotImplemented(name string) (ItemState, string) {
	return StateUnknown, "presence check not implemented for this category"
}

func stateFromCheck(ok bool, err error) (ItemState, string) {
	if err != nil {
		return StateUnknown, err.Error()
	}
	if ok {
		return StateOK, ""
	}
	return StateMissing, ""
}
```

- [x] **Step 4: Run, confirm pass**

Run: `go test ./internal/inventory/ -v`
Expected: all PASS.

Run: `go vet ./internal/inventory/`
Expected: no issues.

- [x] **Step 5: Commit**

```bash
git add internal/inventory/
git commit -m "feat(inventory): add Collector for drift detection across all tracked items"
```

---

### Task 6: `BorderedPane` component

**Files:**

- Create: `internal/tui/components/borderedpane.go`
- Test: `internal/tui/components/borderedpane_test.go`

Builds the "rounded box with an embedded title in the top border" grammar from the wireframes' T1
page (`╭─ title ────╮`). No such component exists yet — the current worktree TUI's help overlay
(`renderHelpOverlay` in `internal/tui/worktree/model.go`) uses square corners and a _centered_
title on its own border row, which is a different, worktree-specific pattern; do not copy it.

- [x] **Step 1: Write the failing tests**

```go
// internal/tui/components/borderedpane_test.go
package tuicomponents_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func TestBorderedPane_TitleEmbeddedInTopBorder(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.BorderedPane("inventory", 40, []string{"row one", "row two"})
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "inventory") {
		t.Errorf("top border %q should contain the title", lines[0])
	}
	if !strings.HasPrefix(lines[0], "╭") {
		t.Errorf("top border %q should start with ╭", lines[0])
	}
}

func TestBorderedPane_BottomBorder(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.BorderedPane("x", 20, []string{"a"})
	lines := strings.Split(got, "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "╰") || !strings.HasSuffix(last, "╯") {
		t.Errorf("bottom border %q should be ╰...╯", last)
	}
}

func TestBorderedPane_BodyLinesWrappedInSideBorders(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.BorderedPane("t", 20, []string{"hello"})
	lines := strings.Split(got, "\n")
	body := lines[1]
	if !strings.Contains(body, "hello") {
		t.Errorf("body line %q should contain the content", body)
	}
	if !strings.HasPrefix(body, "│") || !strings.HasSuffix(body, "│") {
		t.Errorf("body line %q should be wrapped in │...│", body)
	}
}

func TestBorderedPane_EveryLineMatchesRequestedWidth(t *testing.T) {
	p := tuicomponents.NewPalette()
	width := 30
	got := p.BorderedPane("inventory", width, []string{"short", "a much longer line of content here"})
	for i, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w != width {
			t.Errorf("line %d: display width %d, want %d (%q)", i, w, width, line)
		}
	}
}

func TestBorderedPane_NoBodyLinesStillRendersBorders(t *testing.T) {
	p := tuicomponents.NewPalette()
	got := p.BorderedPane("empty", 20, nil)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected top+bottom border only (2 lines), got %d: %q", len(lines), got)
	}
}
```

- [x] **Step 2: Run, confirm failure**

Run: `go test ./internal/tui/components/ -run TestBorderedPane -v`
Expected: FAIL to compile — `p.BorderedPane` undefined.

- [x] **Step 3: Implement**

```go
// internal/tui/components/borderedpane.go
package tuicomponents

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// BorderedPane renders a rounded-corner box with the title embedded in the top
// border line (e.g. "╭─ inventory ─────────╮"), matching the T1 wireframe page.
// lines are body rows (may already carry ANSI styling); each is padded or
// truncated to fit the interior width so every returned line has exactly
// `width` display columns.
func (p *Palette) BorderedPane(title string, width int, lines []string) string {
	if width < 6 {
		width = 6
	}
	inner := width - 2
	border := p.PaletteBorder.Render

	label := " " + title + " "
	dashCount := inner - ansi.StringWidth(label) - 1 // 1 for the leading "─" before the label
	if dashCount < 0 {
		dashCount = 0
	}
	top := border("╭─") + p.RepoHeader.Render(title) + border(" "+strings.Repeat("─", dashCount)+"╮")

	var sb strings.Builder
	sb.WriteString(top)
	for _, line := range lines {
		trimmed := ansi.Truncate(line, inner, "")
		pad := inner - ansi.StringWidth(trimmed)
		if pad < 0 {
			pad = 0
		}
		sb.WriteString("\n")
		sb.WriteString(border("│") + trimmed + strings.Repeat(" ", pad) + border("│"))
	}
	sb.WriteString("\n")
	sb.WriteString(border("╰" + strings.Repeat("─", inner) + "╯"))
	return sb.String()
}
```

- [x] **Step 4: Run, confirm pass**

Run: `go test ./internal/tui/components/ -v`
Expected: all PASS, including every pre-existing component test.

- [x] **Step 5: Commit**

```bash
git add internal/tui/components/borderedpane.go internal/tui/components/borderedpane_test.go
git commit -m "feat(tui/components): add BorderedPane, a rounded box with an embedded title"
```

---

### Task 7: `internal/tui/inventory` package — rows, status glyphs, model, run

**Files:**

- Create: `internal/tui/inventory/rows.go`
- Create: `internal/tui/inventory/status.go`
- Create: `internal/tui/inventory/model.go`
- Create: `internal/tui/inventory/run.go`
- Test: `internal/tui/inventory/rows_test.go`
- Test: `internal/tui/inventory/status_test.go`
- Test: `internal/tui/inventory/model_test.go`

This is the shared dashboard both `dg list` and `dg validate` open. Read-only: no attach/delete/
repair actions (those are explicitly out of scope — see the cycle doc §4).

#### 7a. Row building and grouping

- [x] **Step 1: Write the failing tests**

```go
// internal/tui/inventory/rows_test.go
package tuiinventory

import (
	"testing"

	"github.com/cjairm/devgeta/internal/inventory"
)

func sampleItems() []inventory.Item {
	return []inventory.Item{
		{Name: "git", Category: "packages", Source: "installed", State: inventory.StateOK},
		{Name: "tmux", Category: "packages", Source: "installed", State: inventory.StateMissing},
		{Name: "docker", Category: "desktop_apps", Source: "installed", State: inventory.StateOK},
		{Name: "JetBrainsMono", Category: "fonts", Source: "installed", State: inventory.StateUnknown},
	}
}

func TestBuildRows_GroupedByCategory(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{}, "", false)
	// Packages(2) header+2 items, Desktop Apps(1) header+1, Fonts(1) header+1 = 3 headers + 4 items = 7
	if len(rows) != 7 {
		t.Fatalf("expected 7 rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].kind != rowGroup || rows[0].group != "Packages" || rows[0].count != 2 {
		t.Errorf("expected first row to be Packages header with count 2, got %+v", rows[0])
	}
}

func TestBuildRows_ProblemsOnlyHidesOK(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{}, "", true)
	for _, r := range rows {
		if r.kind == rowItem && r.item.State == inventory.StateOK {
			t.Errorf("problems-only filter should hide OK items, found %+v", r.item)
		}
	}
}

func TestBuildRows_TextFilterMatchesItemName(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{}, "git", false)
	itemCount := 0
	for _, r := range rows {
		if r.kind == rowItem {
			itemCount++
			if r.item.Name != "git" {
				t.Errorf("filter 'git' should only match item 'git', got %q", r.item.Name)
			}
		}
	}
	if itemCount != 1 {
		t.Errorf("expected exactly 1 matching item, got %d", itemCount)
	}
}

func TestBuildRows_CollapsedGroupHidesItemsButKeepsCount(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{"Packages": true}, "", false)
	for _, r := range rows {
		if r.kind == rowGroup && r.group == "Packages" {
			if r.count != 2 {
				t.Errorf("collapsed Packages header should still report count 2, got %d", r.count)
			}
		}
		if r.kind == rowItem && r.item.Category == "packages" {
			t.Errorf("collapsed group should hide its item rows, found %+v", r.item)
		}
	}
}

func TestBuildRows_GroupedByStatus(t *testing.T) {
	rows := buildRows(sampleItems(), groupByStatus, map[string]bool{}, "", false)
	if rows[0].kind != rowGroup || rows[0].group != "MISSING" {
		t.Errorf("status grouping should show MISSING first, got %+v", rows[0])
	}
}

func TestItemIndices_OnlySkipsGroupRows(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{}, "", false)
	indices := itemIndices(rows)
	for _, i := range indices {
		if rows[i].kind != rowItem {
			t.Errorf("index %d should point to a rowItem, got kind %v", i, rows[i].kind)
		}
	}
	if len(indices) != 4 {
		t.Errorf("expected 4 item rows, got %d", len(indices))
	}
}
```

- [x] **Step 2: Run, confirm failure**

Run: `go test ./internal/tui/inventory/ -v`
Expected: FAIL to compile — package doesn't exist yet.

- [x] **Step 3: Implement**

```go
// internal/tui/inventory/rows.go
package tuiinventory

import (
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/inventory"
)

type rowKind int

const (
	rowGroup rowKind = iota
	rowItem
)

type groupMode int

const (
	groupByCategory groupMode = iota
	groupByStatus
)

type row struct {
	kind  rowKind
	group string // display label of the group (T1/T3 style)
	count int    // set for rowGroup only — total items in the group, even when collapsed
	item  inventory.Item
}

var categoryLabels = func() map[string]string {
	m := map[string]string{}
	for _, c := range inventory.Categories {
		m[c.Key] = c.Label
	}
	return m
}()

var categoryOrder = func() []string {
	order := make([]string, len(inventory.Categories))
	for i, c := range inventory.Categories {
		order[i] = c.Label
	}
	return order
}()

var statusOrder = []string{"MISSING", "UNKNOWN", "OK"}

func groupLabel(item inventory.Item, mode groupMode) string {
	if mode == groupByStatus {
		return item.State.String()
	}
	return categoryLabels[item.Category]
}

// buildRows filters items (problems-only, text filter), groups them per mode,
// sorts items alphabetically within each group, and returns header+item rows
// in display order. Groups with zero visible items are omitted entirely.
func buildRows(items []inventory.Item, mode groupMode, collapsed map[string]bool, filter string, problemsOnly bool) []row {
	filter = strings.ToLower(filter)

	groups := map[string][]inventory.Item{}
	for _, it := range items {
		if problemsOnly && it.State == inventory.StateOK {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(it.Name), filter) {
			continue
		}
		label := groupLabel(it, mode)
		groups[label] = append(groups[label], it)
	}

	order := categoryOrder
	if mode == groupByStatus {
		order = statusOrder
	}

	var rows []row
	for _, label := range order {
		visible := groups[label]
		if len(visible) == 0 {
			continue
		}
		sort.Slice(visible, func(i, j int) bool { return visible[i].Name < visible[j].Name })
		rows = append(rows, row{kind: rowGroup, group: label, count: len(visible)})
		if !collapsed[label] {
			for _, it := range visible {
				rows = append(rows, row{kind: rowItem, group: label, item: it})
			}
		}
	}
	return rows
}

// itemIndices returns row indices that are rowItem kind.
func itemIndices(rows []row) []int {
	var out []int
	for i, r := range rows {
		if r.kind == rowItem {
			out = append(out, i)
		}
	}
	return out
}

// navigableIndices returns indices that j/k visit: all item rows, plus
// collapsed group headers (so the user can reach a collapsed header and
// press l to expand it).
func navigableIndices(rows []row, collapsed map[string]bool) []int {
	var out []int
	for i, r := range rows {
		if r.kind == rowItem || (r.kind == rowGroup && collapsed[r.group]) {
			out = append(out, i)
		}
	}
	return out
}
```

- [x] **Step 4: Run, confirm pass**

Run: `go test ./internal/tui/inventory/ -run 'TestBuildRows|TestItemIndices' -v`
Expected: all PASS.

- [x] **Step 5: Commit**

```bash
git add internal/tui/inventory/rows.go internal/tui/inventory/rows_test.go
git commit -m "feat(tui/inventory): add row grouping (by category / by status), filtering"
```

#### 7b. Status glyphs

Per the cycle doc: reuse `Palette`'s raw colors, but do **not** extend `statusdot.go` (that file's
`SessionState` vocabulary is worktree-specific). This gives inventory its own OK/MISSING/UNKNOWN
vocabulary in its own package.

- [x] **Step 1: Write the failing tests**

```go
// internal/tui/inventory/status_test.go
package tuiinventory

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/inventory"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func TestStatusGlyph_NoANSI(t *testing.T) {
	cases := map[inventory.ItemState]string{
		inventory.StateOK:      "●",
		inventory.StateMissing: "●",
		inventory.StateUnknown: "○",
	}
	for state, want := range cases {
		got := statusGlyph(state)
		if got != want {
			t.Errorf("state %v: got %q, want %q", state, got, want)
		}
		if strings.ContainsRune(got, '\x1b') {
			t.Errorf("state %v: statusGlyph must not contain ANSI escape bytes", state)
		}
	}
}

func TestStatusDot_ContainsGlyph(t *testing.T) {
	p := tuicomponents.NewPalette()
	cases := map[inventory.ItemState]string{
		inventory.StateOK:      "●",
		inventory.StateMissing: "●",
		inventory.StateUnknown: "○",
	}
	for state, glyph := range cases {
		got := statusDot(p, state)
		if !strings.Contains(got, glyph) {
			t.Errorf("state %v: statusDot %q does not contain glyph %q", state, got, glyph)
		}
	}
}

func TestSourceTag_PreExistingIsTagged(t *testing.T) {
	p := tuicomponents.NewPalette()
	if got := sourceTag(p, "pre-existing"); !strings.Contains(got, "pre-existing") {
		t.Errorf("expected pre-existing tag, got %q", got)
	}
	if got := sourceTag(p, "installed"); got != "" {
		t.Errorf("installed items should have no tag, got %q", got)
	}
}
```

- [x] **Step 2: Run, confirm failure**

Run: `go test ./internal/tui/inventory/ -run 'TestStatusGlyph|TestStatusDot|TestSourceTag' -v`
Expected: FAIL to compile.

- [x] **Step 3: Implement**

```go
// internal/tui/inventory/status.go
package tuiinventory

import (
	"github.com/cjairm/devgeta/internal/inventory"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

// statusGlyph returns the raw glyph for an item state (no ANSI styling).
// StateOK and StateMissing intentionally share "●" — color is the differentiator,
// mirroring the legend page's filled-vs-hollow convention.
func statusGlyph(state inventory.ItemState) string {
	if state == inventory.StateUnknown {
		return "○"
	}
	return "●"
}

// statusDot returns the colored glyph string, reusing Palette's raw colors:
// green for OK, red for MISSING, gray for UNKNOWN.
func statusDot(p *tuicomponents.Palette, state inventory.ItemState) string {
	g := statusGlyph(state)
	switch state {
	case inventory.StateOK:
		return p.Running.Render(g)
	case inventory.StateMissing:
		return p.DiffRemoved.Render(g)
	default:
		return p.NoSession.Render(g)
	}
}

// sourceTag renders a dim suffix tag for pre-existing items (e.g. "(pre-existing)").
// Installed items get no tag.
func sourceTag(p *tuicomponents.Palette, source string) string {
	if source != "pre-existing" {
		return ""
	}
	return p.Inactive.Render(" (pre-existing)")
}
```

- [x] **Step 4: Run, confirm pass**

Run: `go test ./internal/tui/inventory/ -run 'TestStatusGlyph|TestStatusDot|TestSourceTag' -v`
Expected: all PASS.

- [x] **Step 5: Commit**

```bash
git add internal/tui/inventory/status.go internal/tui/inventory/status_test.go
git commit -m "feat(tui/inventory): add OK/MISSING/UNKNOWN status glyphs and source tag"
```

#### 7c. Model (Init/Update/View) and Run

- [x] **Step 1: Write the failing tests**

```go
// internal/tui/inventory/model_test.go
package tuiinventory

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/inventory"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

func testItems() []inventory.Item {
	return []inventory.Item{
		{Name: "git", Category: "packages", Source: "installed", State: inventory.StateOK},
		{Name: "tmux", Category: "packages", Source: "installed", State: inventory.StateMissing},
		{Name: "docker", Category: "desktop_apps", Source: "installed", State: inventory.StateOK},
	}
}

func TestNewModel_InitialCursorOnItemRow(t *testing.T) {
	m := newModel(testItems(), Options{})
	if m.rows[m.cursor].kind != rowItem {
		t.Error("initial cursor should be on an item row")
	}
}

func TestNewModel_CategoryPreFilter(t *testing.T) {
	m := newModel(testItems(), Options{Category: "packages"})
	for _, it := range m.items {
		if it.Category != "packages" {
			t.Errorf("expected only packages items after pre-filter, found %+v", it)
		}
	}
	if m.title != "Packages" {
		t.Errorf("expected title %q, got %q", "Packages", m.title)
	}
}

func TestNewModel_ProblemsOnlyOption(t *testing.T) {
	m := newModel(testItems(), Options{ProblemsOnly: true})
	for _, r := range m.rows {
		if r.kind == rowItem && r.item.State == inventory.StateOK {
			t.Error("ProblemsOnly should hide OK items from the initial rows")
		}
	}
}

func TestUpdate_QuitOnQ(t *testing.T) {
	m := newModel(testItems(), Options{})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
}

func TestUpdate_ToggleProblemsOnly(t *testing.T) {
	m := newModel(testItems(), Options{})
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'p'})
	m3 := m2.(model)
	if !m3.problemsOnly {
		t.Error("p should toggle problemsOnly on")
	}
	for _, r := range m3.rows {
		if r.kind == rowItem && r.item.State == inventory.StateOK {
			t.Error("after toggling problems-only, OK items should be hidden")
		}
	}
}

func TestUpdate_ToggleGroupMode(t *testing.T) {
	m := newModel(testItems(), Options{})
	if m.groupMode != groupByCategory {
		t.Fatal("expected initial groupMode to be groupByCategory")
	}
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'g'})
	m3 := m2.(model)
	if m3.groupMode != groupByStatus {
		t.Error("g should toggle groupMode to groupByStatus")
	}
}

func TestUpdate_CollapseExpandGroup(t *testing.T) {
	m := newModel(testItems(), Options{})
	// h collapses the selected item's group and lands the cursor on its header.
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m3 := m2.(model)
	if m3.rows[m3.cursor].kind != rowGroup {
		t.Fatalf("after h, cursor should be on a group header, got kind %v", m3.rows[m3.cursor].kind)
	}
	collapsedGroup := m3.rows[m3.cursor].group
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'l'})
	m5 := m4.(model)
	if m5.rows[m5.cursor].kind != rowItem {
		t.Fatal("after l, cursor should return to an item row")
	}
	if m5.collapsed[collapsedGroup] {
		t.Error("l should have expanded the group")
	}
}

func TestUpdate_FilterMode(t *testing.T) {
	m := newModel(testItems(), Options{})
	m2, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	m3 := m2.(model)
	if !m3.filtering {
		t.Fatal("/ should enter filtering mode")
	}
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'g'})
	m5 := m4.(model)
	if m5.filter != "g" {
		t.Errorf("expected filter %q, got %q", "g", m5.filter)
	}
	m6, _ := m5.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m7 := m6.(model)
	if m7.filtering || m7.filter != "" {
		t.Error("esc should clear filter and exit filtering mode")
	}
}

func TestView_NoPanicAtVariousSizes(t *testing.T) {
	m := newModel(testItems(), Options{})
	sizes := []tea.WindowSizeMsg{{Width: 0, Height: 0}, {Width: 20, Height: 10}, {Width: 120, Height: 40}}
	for _, sz := range sizes {
		m2, _ := m.Update(sz)
		mm := m2.(model)
		v := mm.View()
		_ = v
	}
}
```

- [x] **Step 2: Run, confirm failure**

Run: `go test ./internal/tui/inventory/ -v`
Expected: FAIL to compile — `Options`, `newModel`, `model` undefined.

- [x] **Step 3: Implement the model**

```go
// internal/tui/inventory/model.go
package tuiinventory

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cjairm/devgeta/internal/inventory"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

// Options configures the dashboard's initial filter state.
type Options struct {
	ProblemsOnly bool   // pre-applied "problems only" filter (dg validate sets this)
	Category     string // pre-filter to a single category key (e.g. "fonts"); "" = all
}

type model struct {
	items []inventory.Item
	title string

	rows      []row
	cursor    int
	collapsed map[string]bool
	groupMode groupMode

	problemsOnly bool
	filtering    bool
	filter       string

	width, height int

	palette *tuicomponents.Palette
}

func newModel(items []inventory.Item, opts Options) model {
	title := "inventory"
	if opts.Category != "" {
		if label, ok := categoryLabels[opts.Category]; ok {
			title = label
		}
		var filtered []inventory.Item
		for _, it := range items {
			if it.Category == opts.Category {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	m := model{
		items:        items,
		title:        title,
		collapsed:    map[string]bool{},
		groupMode:    groupByCategory,
		problemsOnly: opts.ProblemsOnly,
		palette:      tuicomponents.NewPalette(),
	}
	m.rebuildRows()
	return m
}

func (m *model) rebuildRows() {
	m.rows = buildRows(m.items, m.groupMode, m.collapsed, m.filter, m.problemsOnly)
	indices := itemIndices(m.rows)
	if len(indices) == 0 {
		m.cursor = 0
		return
	}
	for _, i := range indices {
		if i >= m.cursor {
			m.cursor = i
			return
		}
	}
	m.cursor = indices[len(indices)-1]
}

func (m *model) moveCursor(delta int) {
	indices := navigableIndices(m.rows, m.collapsed)
	if len(indices) == 0 {
		return
	}
	cur := -1
	for i, idx := range indices {
		if idx == m.cursor {
			cur = i
			break
		}
	}
	if cur == -1 {
		if delta > 0 {
			for _, idx := range indices {
				if idx > m.cursor {
					m.cursor = idx
					return
				}
			}
		} else {
			for i := len(indices) - 1; i >= 0; i-- {
				if indices[i] < m.cursor {
					m.cursor = indices[i]
					return
				}
			}
		}
		m.cursor = indices[0]
		return
	}
	cur = ((cur + delta) % len(indices) + len(indices)) % len(indices)
	m.cursor = indices[cur]
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			m.rebuildRows()
		case "enter":
			m.filtering = false
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.rebuildRows()
			}
		default:
			if len(key) == 1 && key >= " " {
				m.filter += key
				m.rebuildRows()
			}
		}
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j":
		m.moveCursor(1)
	case "k":
		m.moveCursor(-1)
	case "h":
		if g := m.cursorGroup(); g != "" {
			m.collapsed[g] = true
			m.rebuildRows()
			m.landCursorOnGroup(g)
		}
	case "l":
		if g := m.cursorGroup(); g != "" {
			m.collapsed[g] = false
			m.rebuildRows()
		}
	case "/":
		m.filtering = true
	case "p":
		m.problemsOnly = !m.problemsOnly
		m.rebuildRows()
	case "g":
		if m.groupMode == groupByCategory {
			m.groupMode = groupByStatus
		} else {
			m.groupMode = groupByCategory
		}
		m.collapsed = map[string]bool{}
		m.rebuildRows()
	}
	return m, nil
}

func (m model) cursorGroup() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	return m.rows[m.cursor].group
}

func (m *model) landCursorOnGroup(group string) {
	for i, r := range m.rows {
		if r.kind == rowGroup && r.group == group {
			m.cursor = i
			return
		}
	}
}

func (m model) View() tea.View {
	v := tea.NewView(m.renderContent())
	v.AltScreen = true
	return v
}

func (m model) renderContent() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	hint := m.renderHint(m.width)
	summary := m.renderSummary(m.width)

	// Reserve 1 line for hint, 1 for summary, 2 for the pane's own top/bottom border.
	viewportHeight := m.height - 4
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	start, end := visibleWindow(len(m.rows), m.cursor, viewportHeight)

	var lines []string
	for i := start; i < end; i++ {
		lines = append(lines, m.renderRow(i))
	}

	return m.palette.BorderedPane(m.title, m.width, lines) + "\n" + summary + "\n" + hint
}

// visibleWindow returns [start, end) into a rowsLen-length list such that the
// window has at most viewportHeight rows and always contains cursor.
func visibleWindow(rowsLen, cursor, viewportHeight int) (start, end int) {
	if rowsLen <= viewportHeight {
		return 0, rowsLen
	}
	start = cursor - viewportHeight/2
	if start < 0 {
		start = 0
	}
	end = start + viewportHeight
	if end > rowsLen {
		end = rowsLen
		start = end - viewportHeight
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func (m model) renderRow(i int) string {
	r := m.rows[i]
	innerWidth := m.width - 2
	if r.kind == rowGroup {
		collapse := "▾"
		if m.collapsed[r.group] {
			collapse = "▸"
		}
		text := collapse + " " + r.group
		count := fmt.Sprintf("%d", r.count)
		pad := innerWidth - ansi.StringWidth(text) - ansi.StringWidth(count)
		if pad < 1 {
			pad = 1
		}
		plain := text + strings.Repeat(" ", pad) + count
		if i == m.cursor {
			return m.palette.Selected.Render(plain)
		}
		return m.palette.RepoHeader.Render(text) + strings.Repeat(" ", pad) + m.palette.SectionHead.Render(count)
	}

	glyph := statusGlyph(r.item.State)
	name := "  " + glyph + " " + r.item.Name
	if i == m.cursor {
		plain := name
		if r.item.Source == "pre-existing" {
			plain += " (pre-existing)"
		}
		return m.palette.Selected.Render(plain)
	}
	line := "  " + statusDot(m.palette, r.item.State) + " " + r.item.Name + sourceTag(m.palette, r.item.Source)
	return line
}

func (m model) renderSummary(width int) string {
	categories := map[string]bool{}
	missing := 0
	for _, it := range m.items {
		categories[it.Category] = true
		if it.State == inventory.StateMissing {
			missing++
		}
	}
	text := fmt.Sprintf("%d CATEGORIES · %d ITEMS · %d MISSING", len(categories), len(m.items), missing)
	return m.palette.SectionHead.Render(ansi.Truncate(text, width, ""))
}

func (m model) renderHint(width int) string {
	if m.filtering {
		hint := "filter: " + m.filter + "█  · esc: clear · enter: keep"
		return m.palette.HintDesc.Render(ansi.Truncate(hint, width, ""))
	}
	hints := []tuicomponents.KeyHint{
		{Key: "j/k", Desc: "move"},
		{Key: "h/l", Desc: "collapse/expand"},
		{Key: "/", Desc: "filter"},
		{Key: "p", Desc: "problems"},
		{Key: "g", Desc: "group"},
		{Key: "q", Desc: "quit"},
	}
	return m.palette.HintBar(hints, width)
}
```

- [x] **Step 4: Implement Run**

```go
// internal/tui/inventory/run.go
package tuiinventory

import (
	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/inventory"
)

// Run starts the shared inventory dashboard. dg list calls this with
// Options{} (unfiltered); dg validate calls it with Options{ProblemsOnly: true}.
func Run(gc *config.GlobalConfig, opts Options) error {
	c := &inventory.Collector{Cmd: commands.NewCommand(), Base: commands.NewBaseCommand()}
	items := c.Collect(gc)
	m := newModel(items, opts)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
```

- [x] **Step 5: Run, confirm pass**

Run: `go test ./internal/tui/inventory/ -v`
Expected: all PASS.

Run: `go build ./...`
Expected: no errors.

- [x] **Step 6: Commit**

```bash
git add internal/tui/inventory/model.go internal/tui/inventory/run.go internal/tui/inventory/model_test.go
git commit -m "feat(tui/inventory): add read-only dashboard model, view, and Run entrypoint"
```

---

### Task 8: `dg validate` command

**Files:**

- Create: `cmd/validate.go`
- Test: `cmd/validate_test.go`

- [x] **Step 1: Write the failing tests**

```go
// cmd/validate_test.go
package cmd

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/inventory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatValidate_EmptyItems(t *testing.T) {
	out, anyMissing, err := formatValidate(nil, "")
	require.NoError(t, err)
	assert.False(t, anyMissing)
	assert.Contains(t, out, "Nothing tracked yet")
}

func TestFormatValidate_TableHasStatusColumn(t *testing.T) {
	items := []inventory.Item{
		{Name: "git", Category: "packages", Source: "installed", State: inventory.StateOK},
		{Name: "tmux", Category: "packages", Source: "installed", State: inventory.StateMissing},
	}
	out, anyMissing, err := formatValidate(items, "")
	require.NoError(t, err)
	assert.True(t, anyMissing)
	assert.Contains(t, out, "STATUS")
	assert.Contains(t, out, "OK")
	assert.Contains(t, out, "MISSING")
	assert.Contains(t, out, "git")
	assert.Contains(t, out, "tmux")
}

func TestFormatValidate_UnknownDoesNotSetAnyMissing(t *testing.T) {
	items := []inventory.Item{
		{Name: "JetBrainsMono", Category: "fonts", Source: "installed", State: inventory.StateUnknown},
	}
	_, anyMissing, err := formatValidate(items, "")
	require.NoError(t, err)
	assert.False(t, anyMissing, "UNKNOWN must never fail dg validate's exit code")
}

func TestFormatValidate_CategoryFilter(t *testing.T) {
	items := []inventory.Item{
		{Name: "git", Category: "packages", Source: "installed", State: inventory.StateOK},
		{Name: "JetBrainsMono", Category: "fonts", Source: "installed", State: inventory.StateOK},
	}
	out, _, err := formatValidate(items, "fonts")
	require.NoError(t, err)
	assert.Contains(t, out, "JetBrainsMono")
	assert.NotContains(t, out, "git")
}

func TestFormatValidate_InvalidCategory(t *testing.T) {
	_, _, err := formatValidate(nil, "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid category")
}

func TestFormatValidate_GroupedByCategoryLabel(t *testing.T) {
	items := []inventory.Item{
		{Name: "git", Category: "packages", Source: "installed", State: inventory.StateOK},
	}
	out, _, err := formatValidate(items, "")
	require.NoError(t, err)
	assert.Contains(t, out, "Packages:")
	packagesIdx := strings.Index(out, "Packages:")
	require.NotEqual(t, -1, packagesIdx)
}
```

- [x] **Step 2: Run, confirm failure**

Run: `go test ./cmd/ -run TestFormatValidate -v`
Expected: FAIL to compile — `formatValidate` undefined.

- [x] **Step 3: Implement**

```go
// cmd/validate.go
/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/inventory"
	tuiinventory "github.com/cjairm/devgeta/internal/tui/inventory"
	"github.com/spf13/cobra"
)

var (
	validateCategoryFlag string
	validatePlainFlag    bool
)

// formatValidate renders items as one STATUS table per non-empty category (or
// just the matching one, if category is set). Returns the rendered text,
// whether any item is StateMissing (drives dg validate's exit code — UNKNOWN
// never does), and a category-validation error if any.
func formatValidate(items []inventory.Item, category string) (string, bool, error) {
	if category != "" && !isValidCategory(category) {
		return "", false, fmt.Errorf(
			"invalid category %q: valid categories are %s",
			category, strings.Join(validCategoryKeys(), ", "),
		)
	}

	byCategory := map[string][]inventory.Item{}
	for _, it := range items {
		if category != "" && it.Category != category {
			continue
		}
		byCategory[it.Category] = append(byCategory[it.Category], it)
	}

	var buf bytes.Buffer
	anyMissing := false
	wrote := false
	for _, cat := range inventory.Categories {
		rows := byCategory[cat.Key]
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

		fmt.Fprintf(&buf, "%s:\n", cat.Label)
		w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSTATUS\tSOURCE")
		for _, it := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\n", it.Name, it.State.String(), it.Source)
			if it.State == inventory.StateMissing {
				anyMissing = true
			}
		}
		_ = w.Flush()
		fmt.Fprintln(&buf)
		wrote = true
	}

	if !wrote {
		return "Nothing tracked yet. Run `dg install` to get started.\n", false, nil
	}
	return buf.String(), anyMissing, nil
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Verify tracked installations are still present on the system",
	Long: `Verify tracked installations are still present on the system.

For every item devgeta tracked (installed by devgeta, or found pre-existing),
checks whether it's still actually present — catching drift between
global_config.yaml and system reality.

In a terminal, opens the shared inventory dashboard (same as 'dg list') pre-
filtered to problems only. Piped output, CI, or --plain get a plain STATUS
table and a non-zero exit code if anything tracked is missing.

Examples:
  dg validate                    # Interactive dashboard, problems only
  dg validate --plain            # Plain STATUS table, exits 1 if anything missing
  dg validate --category=fonts   # Limit to one category`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if validateCategoryFlag != "" && !isValidCategory(validateCategoryFlag) {
			return fmt.Errorf(
				"invalid category %q: valid categories are %s",
				validateCategoryFlag, strings.Join(validCategoryKeys(), ", "),
			)
		}

		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			return fmt.Errorf("failed to load global config: %w", err)
		}

		if !validatePlainFlag && isInteractiveTerminal() {
			return tuiinventory.Run(gc, tuiinventory.Options{
				ProblemsOnly: true,
				Category:     validateCategoryFlag,
			})
		}

		c := &inventory.Collector{Cmd: commands.NewCommand(), Base: commands.NewBaseCommand()}
		items := c.Collect(gc)

		out, anyMissing, err := formatValidate(items, validateCategoryFlag)
		if err != nil {
			return err
		}
		fmt.Print(out)
		if anyMissing {
			return fmt.Errorf("drift detected: one or more tracked items are missing")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().StringVar(
		&validateCategoryFlag,
		"category",
		"",
		fmt.Sprintf("Filter to a single category (%s)", strings.Join(validCategoryKeys(), ", ")),
	)
	validateCmd.Flags().BoolVar(
		&validatePlainFlag,
		"plain",
		false,
		"Force plain-text output even in a terminal",
	)
}
```

- [x] **Step 4: Run, confirm pass**

Run: `go test ./cmd/ -run TestFormatValidate -v`
Expected: all PASS.

Run: `go build ./...`
Expected: no errors — confirms `dg validate` is now wired into `rootCmd` (it was already mentioned
in `cmd/root.go`'s help text as a planned command; this makes it real).

- [x] **Step 5: Manually verify the golden path**

```bash
go build -o /tmp/dg .
/tmp/dg validate --plain | cat   # non-TTY path even without piping, but | cat guarantees it
```

Expected: either "Nothing tracked yet..." (fresh machine) or a STATUS table, exit code 0 unless
something tracked is genuinely missing on your machine.

- [x] **Step 6: Commit**

```bash
git add cmd/validate.go cmd/validate_test.go
git commit -m "feat(cmd): add dg validate — drift detection with dashboard and plain-table modes"
```

---

### Task 9: Wire `dg list` into the shared dashboard

**Files:**

- Modify: `cmd/list.go`
- Test: `cmd/list_test.go` (add one test; existing tests are unaffected)

- [x] **Step 1: Write the failing test**

Add to `cmd/list_test.go`:

```go
func TestListCmd_PlainFlagRegistered(t *testing.T) {
	flag := listCmd.Flags().Lookup("plain")
	if flag == nil {
		t.Fatal("expected --plain flag to be registered on dg list")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected --plain to default to false, got %q", flag.DefValue)
	}
}
```

- [x] **Step 2: Run, confirm failure**

Run: `go test ./cmd/ -run TestListCmd_PlainFlagRegistered -v`
Expected: FAIL — `flag` is nil (`--plain` not yet registered).

- [x] **Step 3: Implement**

In `cmd/list.go`, add the import and flag variable, update `RunE`, and register the flag.

Add to the import block:

```go
	tuiinventory "github.com/cjairm/devgeta/internal/tui/inventory"
```

Add next to `listCategoryFlag`:

```go
var listPlainFlag bool
```

Replace the `RunE` body:

```go
	RunE: func(cmd *cobra.Command, args []string) error {
		if listCategoryFlag != "" && !isValidCategory(listCategoryFlag) {
			return fmt.Errorf(
				"invalid category %q: valid categories are %s",
				listCategoryFlag, strings.Join(validCategoryKeys(), ", "),
			)
		}

		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			return fmt.Errorf("failed to load global config: %w", err)
		}

		if !listPlainFlag && isInteractiveTerminal() {
			return tuiinventory.Run(gc, tuiinventory.Options{Category: listCategoryFlag})
		}

		out, err := formatInstalled(gc, listCategoryFlag)
		if err != nil {
			return err
		}

		fmt.Print(out)
		return nil
	},
```

Add flag registration in `init()`, alongside the existing `--category` registration:

```go
	listCmd.Flags().BoolVar(
		&listPlainFlag,
		"plain",
		false,
		"Force plain-text output even in a terminal",
	)
```

Also update the `Long` help text to mention `--plain` and the new dashboard behavior:

```go
	Long: `View all items installed via Devgeta (alias: installed).

In a terminal, opens the interactive inventory dashboard grouped by category
with a live OK/MISSING/UNKNOWN status per item. Piped output, CI, or --plain
fall back to the plain-text table (reads ~/.config/devgeta/global_config.yaml
directly, with no live status check).

Examples:
  dg list                          # Interactive dashboard in a terminal
  dg list --plain                  # Force the plain-text table
  dg list --category=terminal_tools  # Show only one category
  dg installed                     # Same as 'dg list'`,
```

- [x] **Step 4: Run, confirm pass and no regressions**

Run: `go test ./cmd/ -v`
Expected: all PASS — the new test, plus every pre-existing `formatInstalled`/`dg list` test
unchanged (they call `formatInstalled` directly, which this change doesn't touch).

Run: `go build ./...`
Expected: no errors.

- [x] **Step 5: Manually verify both paths**

```bash
go build -o /tmp/dg .
/tmp/dg list | cat          # plain path (piped) — should look exactly as before
/tmp/dg list --plain        # plain path (forced) — should match the piped output
/tmp/dg list                # in an actual terminal — should open the dashboard
```

Confirm the dashboard opens, shows categories with counts, status dots render with the right
colors for real installed/missing items on your machine, `j/k` moves, `h/l` collapses/expands,
`/` filters, `p` toggles problems-only, `g` toggles category/status grouping, `q` quits.

- [x] **Step 6: Commit**

```bash
git add cmd/list.go cmd/list_test.go
git commit -m "feat(cmd): open the shared inventory dashboard from dg list in a terminal"
```

---

### Task 10: Docs — `docs/spec.md` and `ROADMAP.md`

**Files:**

- Modify: `docs/spec.md`
- Modify: `ROADMAP.md`

- [x] **Step 1: Update `ROADMAP.md`**

Find the `dg validate` entry (currently under a "planned" section, per the grep below) and move it
out of the planned list. Read the surrounding structure first:

Run: `grep -n "dg validate\|dg list\|## Implemented\|### " /Users/jair.mendez/Documents/projects/devgeta/ROADMAP.md`

Move the `dg validate` bullet to the same "Implemented" section that `dg list` was moved to (see
commit `290b6f9` for the pattern that cycle used), with equivalent phrasing, e.g.:

```markdown
- **`dg validate`** — Drift detection dashboard (shipped vX.Y.Z)
  - Checks every tracked item (installed by devgeta, or found pre-existing) against system reality
  - Interactive dashboard in a terminal (shared with `dg list`), pre-filtered to problems
  - `--plain` / non-TTY: STATUS table, exits 1 if anything is missing
```

Also add a note next to `dg list`'s existing "Implemented" entry that it now opens the same
dashboard in a terminal (plain table unchanged for piped/CI/`--plain` use).

- [x] **Step 2: Update `docs/spec.md`**

Read the file's existing structure first to match its section conventions and heading level:

Run: `grep -n "^#\|dg list\|dg validate" /Users/jair.mendez/Documents/projects/devgeta/docs/spec.md`

Add a section documenting:

- `dg list` / `dg validate` share one data model (`internal/inventory`) and one dashboard
  (`internal/tui/inventory`).
- The three-state model: `OK` / `MISSING` / `UNKNOWN`, and that only `MISSING` affects
  `dg validate`'s exit code.
- `--category` and `--plain` flags on both commands.
- The dashboard's keybindings: `j/k` move, `h/l` collapse/expand, `/` filter, `p` toggle
  problems-only, `g` toggle category/status grouping, `q` quit.
- That `themes`/`terminal_tools` are tracked categories with no live presence check yet (always
  `UNKNOWN` if ever populated) — matches the "always-empty today" reality described for `dg list`.

Follow the existing doc's tone and structure (don't invent a new section style) — mirror how the
`dg list` section was written when it shipped (see the cycle doc referenced by commit `84706d8`).

- [x] **Step 3: Cross-check against the cycle doc**

Re-open `docs/plans/cycles/2026-07-07-dg-validate-inventory.md` and mark every checkbox in §4
"Scope Boundary → In Scope" as done, and update the document's header `Status:` field from `Draft`
to `Done`.

- [x] **Step 4: Commit**

```bash
git add docs/spec.md ROADMAP.md docs/plans/cycles/2026-07-07-dg-validate-inventory.md
git commit -m "docs: document dg validate and the shared inventory dashboard, close out the cycle"
```

---

## 6. Verification Plan

(The plan body above ended with its own "Final verification" step pointing back at this section —
merged here since both said the same thing once the plan and cycle doc became one document.)

### Automated Verification

```bash
go test ./internal/inventory/
go test ./internal/tui/inventory/
go test ./cmd/
go test ./... -cover
make lint
```

### Manual Verification

1. `dg list` in a terminal → dashboard opens, shows all categories, status dots correct for known
   installed/missing items
2. `dg list | cat` (piped) → falls back to existing plain table, exit 0
3. `dg validate` in a terminal → dashboard opens with problems-only filter active; toggle with `p`
4. `dg validate --plain` with nothing missing → plain table, exit 0
5. `dg validate --plain` with a manually-removed tracked package → STATUS shows MISSING, exit 1
6. Confirm `internal/inventory.Collect` does not write to `global_config.yaml` (no `New()` calls to
   `languages`/`databases`)
7. Side-by-side check against the wireframes PDF (legend, layout-A, T1 tree, K1 hint-bar pages):
   bordered pane title, category groups with counts + collapse glyphs, status dot colors, counts
   summary line, and hint bar all match the wireframe grammar — not the current worktree TUI's
   rendering
8. `dg list --category=fonts` in a terminal → dashboard opens showing only the fonts category;
   same flag with `--plain` filters the table as before
9. Simulate a failing check (e.g. temporarily rename `brew`/`dpkg` in PATH) → affected items show
   UNKNOWN (gray `○`), and `dg validate --plain` still exits 0 if nothing is MISSING

### Regression Check

- `dg list --category=<name>` (existing flag) still works in plain mode
- `dg install`, `dg configure`, `dg uninstall` unaffected

---

## 7. Risks & Trade-offs

| Risk                                                                                      | Likelihood | Mitigation                                                                                                                                                                 |
| ----------------------------------------------------------------------------------------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `languages`/`databases` presence checks are slow (shell out per tracked item)             | Med        | Only check tracked items (not every configured language/database like `detectPreInstalled*` does); consider a spinner/progress indicator in the dashboard while collecting |
| Font check (`IsFontPresent`) matches by substring against family names / filenames        | Low        | Tracked font names come from devgeta's own install path, which uses the same `IsFontPresent` matching at install time — the two sides agree by construction                |
| TUI dashboard becomes the _only_ discoverable path and users miss `--plain` for scripting | Low        | Auto-detect non-TTY by default so scripts/CI never need to know about `--plain` in the first place                                                                         |
| Bubbletea dashboard scope creeps into interactivity beyond read-only                      | Med        | Explicitly out of scope above; flag any temptation to add actions as a follow-up cycle                                                                                     |
| Implementer imitates the current worktree TUI's rendering instead of the wireframes       | Med        | §1 "Visual authority" + §2 visual-grammar list are the spec; manual verification step 7 checks against the PDF page by page                                                |

### Trade-offs Made

- **Shared dashboard vs. two separate screens:** chose one shared `internal/inventory` +
  `internal/tui/inventory` model for both `dg list` and `dg validate` over two independent
  implementations, to avoid duplicating the category/status data model.
- **Read-only vs. in-TUI actions:** chose read-only for this cycle; repair actions deferred to keep
  this cycle's blast radius (and review surface) small.
- **Font presence check:** originally specced as `IsDesktopAppInstalled` (an approximation);
  corrected during review to `BaseCommandExecutor.IsFontPresent` — a real font check (`fc-list` +
  font-directory scan) that already exists, is what the Debian install path uses, and is already
  mockable. No new `Command` interface method needed.
- **`StateUnknown` vs `StateMissing`:** a failed check (`err != nil`) reports UNKNOWN and never
  fails `dg validate`'s exit code; only a definitive `(false, nil)` reports MISSING. This keeps CI
  runs from going red because a package manager was unavailable, at the cost of potentially
  under-reporting drift when checks error.
- **`dev_languages`/`databases` don't distinguish UNKNOWN from MISSING (accepted asymmetry,
  found in final review):** `languages.IsInstalledOnSystem`/`databases.IsInstalledOnSystem`
  return a bare `bool`, not `(bool, error)` like the package/desktop-app/font checks — so a
  failed version-command run (`node --version`, `psql --version`, ...) always reports MISSING,
  never UNKNOWN, for these two categories. This was raised as an "Important" finding in the
  final whole-implementation review and deliberately not fixed here, because for these two
  categories the check target and the check mechanism are the same command — unlike
  `IsPackageInstalled`, where `brew`/`dpkg` failing is a distinct, meaningful "checker itself is
  broken" signal separate from the package's own presence, a `node --version` failure IS the
  install-detection signal (there's no separate meta-checker whose failure would mean something
  else). In the overwhelming majority of real failures this is "exec: not found," which
  genuinely means not-installed — so MISSING is usually the _correct_ classification here, not
  an under-classification the way it would be for packages/desktop_apps/fonts. Revisit only if a
  concrete false-positive case surfaces in practice (e.g. `dg validate` failing CI for a reason
  unrelated to actual drift); the fix would be threading `(bool, error)` through
  `isLanguageInstalledOnSystem`/`isDatabaseInstalledOnSystem` and `IsInstalledOnSystem`,
  updating `internal/inventory`'s `checkLanguageFn`/`checkDatabaseFn` to route through
  `stateFromCheck` like the other three check mechanisms already do.
- **Two independent 7-category vocabularies (accepted duplication, found in final review):**
  `cmd/list.go`'s pre-existing `categoryDefs` and the new `internal/inventory.Categories` both
  hardcode the same 7 `{Key, Label}` pairs in the same order, with nothing enforcing they stay
  in sync. Already flagged as a non-blocking follow-up during Task 8's code review (2 call sites
  doesn't yet justify collapsing them); the final review raised it again from the
  whole-implementation view. Deferred rather than fixed here to avoid touching `cmd/list.go`'s
  pre-existing, unrelated `categoryDefs` structure (which also carries `Installed`/
  `AlreadyInstalled` field-accessor closures that `inventory.Categories` has no equivalent for)
  in a docs-only closing task. Revisit if a category is ever added, renamed, or removed — that
  change must touch both lists, and a `TestCategoryVocabulariesMatch`-style test (asserting the
  two lists agree) would be a cheap guardrail to add at that point, or when a third consumer of
  the category vocabulary appears.
- **`Item.Detail` surfaced in `dg validate`'s plain table, not the interactive dashboard:** the
  final review found `Detail` (the check-error message for `StateUnknown` items) was captured
  but never displayed anywhere — a diagnostic dead end for the one state that most needs
  explanation. Fixed by adding a `DETAIL` column to `formatValidate`'s plain STATUS table
  (`cmd/validate.go`). Deliberately NOT added to the interactive dashboard's row rendering
  (`internal/tui/inventory/model.go`) in this pass — the dashboard's row layout is a fixed-width
  wireframe-driven design (`BorderedPane` guarantees every line matches the pane width exactly),
  and a raw error string doesn't fit that grammar without a real design decision (e.g. a
  detail popover, a status-bar line on selection) that's out of scope for a docs-closing fix.
  `dg validate --plain` is the immediate, low-risk way to unblock diagnosing an UNKNOWN result;
  a dashboard treatment is a reasonable follow-up.

---

## 8. Cross-Model Review Notes

- [x] Domain context clear?
- [x] Engineer context sufficient?
- [x] Objective unambiguous?
- [x] Scope is actually locked?
- [x] Steps are actionable?
- [x] Verification is executable?
- [x] Risks are realistic?

**Reviewer notes:**
Retroactively confirmed by execution, not a pre-implementation review: all 10 tasks in §5 shipped
via subagent-driven-development, each passing an independent spec-compliance review and a
code-quality review before the next task started. Two review round-trips surfaced real issues the
spec/steps didn't anticipate — a `BorderedPane` off-by-one and title-overflow bug in Task 6 (the
plan's own given code had the off-by-one; fixed and documented in follow-up commits), and a
cursor-navigation test-coverage gap in Task 7 (closed with 4 additional tests, verified via a
deliberate mutation test). Both were resolved within the task they were found in, without requiring
scope changes — confirms the steps were actionable and the scope boundary held.
