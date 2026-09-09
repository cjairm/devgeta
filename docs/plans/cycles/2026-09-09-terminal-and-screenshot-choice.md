# Cycle: Terminal and screenshot apps become a platform-filtered user choice

**Date:** 2026-09-09
**Estimated Duration:** ~6 hours
**Status:** Approved — scope and plan signed off 2026-09-09; implementation
deferred to a separate session. Start at §5 step 1; the two open questions in §8
are still open and must be resolved before the steps they affect (8 and 5).

---

## 1. Domain Context

`dg install` installs desktop apps from a fixed list. Two of them cannot be
installed on macOS any more: Homebrew disabled the `alacritty` and `flameshot`
casks on 2026-09-01 for failing the macOS Gatekeeper check. Two consecutive
fresh-machine runs reported the same four failures, and every future run will.

The disable is permanent, not an outage — Homebrew has stopped carrying unsigned
software in `homebrew/cask`, and Alacritty's maintainers have declined to
notarize. Neither app comes back on its own.

This cycle replaces the fixed list with a per-platform candidate list the user
chooses from. The decision and its trade-offs are recorded in
[ADR-0037](../../decisions/ADR-0037-a-desktop-app-is-chosen-by-the-user-from-what-the-platform-can-install.md);
read it first — this document implements it and does not re-argue it.

Relevant background: [CLAUDE.md](../../../CLAUDE.md) principles 3 (cross-platform
consistency), 7 (visual consistency) and §4 (security); the parity rule in
[docs/guides/theming.md](../../guides/theming.md).

---

## 2. Engineer Context

**Relevant files and their purposes:**

- `internal/tooling/desktop/desktop.go` — the desktop coordinator. `InstallAndConfigure`
  currently calls `InstallAlacritty()` unconditionally, then branches on
  `IsMac()` for the window manager; `getCrossPlatformApps()` returns the fixed
  list that includes `flameshot`.
- `internal/apps/alacritty/alacritty.go` — the existing terminal app module; the
  template for what a Ghostty module must implement.
- `internal/apps/flameshot/flameshot.go` — the existing screenshot module.
- `configs/alacritty/alacritty.toml.tmpl` — the config to mirror, and
  `configs/alacritty/starter.sh`, the tmux-attach launcher. **`starter.sh` is
  reused unchanged** — Ghostty also launches from launchd's bare GUI
  environment on macOS, so its `path_helper` repair stays correct.
- `internal/apps/registry/registry.go` — `Meta` map; every app needs an entry
  with the `ItemType` that `MaybeInstall*` actually stores.
- `cmd/install.go` — `appToCoordinator` (per-app `--only`/`--skip` targeting)
  and `knownCategories`.
- `pkg/constants/constants.go` — app-name constants.
- `pkg/paths/paths.go` — config-dir wiring (`Paths.Config.Alacritty`,
  `Paths.App.Configs.Alacritty`).
- `pkg/promptui/promptui.go` — `MultiSelect(label, options)`. A single-choice
  prompt does not exist yet; see step 3.

**Key facts an implementer will otherwise rediscover the hard way:**

- Ghostty's config is **`key = value`, not TOML**. Comments are `#` on their own
  line. It is a new template, not a port of the TOML file.
- Ghostty's config path is `$XDG_CONFIG_HOME/ghostty/config` on both platforms
  (it also reads a macOS Application Support path, but the XDG one works
  everywhere and matches devgeta's existing convention).
- Ghostty defaults `TERM` to `xterm-ghostty`, which breaks SSH to hosts without
  that terminfo. Pin `term = xterm-256color`, matching what the Alacritty
  config already chose.
- `macos-titlebar-style = hidden` is the closest analogue to Alacritty's
  `decorations = "Buttonless"`, but not identical: Buttonless keeps the titlebar
  and hides the traffic lights; `hidden` removes the titlebar and keeps native
  borders and rounded corners. Needs eyeballing (step 9).
- `MaybeInstallDesktopApp(name, alias)` — the first argument is what the package
  manager gets, the second is what the installed-check and the global config
  use. Getting these backwards is what ADR-less commit `35b85cd` had to fix.

**Option mapping (Alacritty → Ghostty), verified against ghostty.org/docs:**

| Alacritty                             | Ghostty                                                              |
| ------------------------------------- | -------------------------------------------------------------------- |
| `[env] TERM = "xterm-256color"`       | `term = xterm-256color`                                              |
| `[terminal.shell] program`            | `command`                                                            |
| `padding.x` / `padding.y`             | `window-padding-x` / `window-padding-y`                              |
| `decorations = "Buttonless"`          | `macos-titlebar-style = hidden`                                      |
| `opacity = 0.8`                       | `background-opacity = 0.8`                                           |
| `blur = true`                         | `background-blur = true`                                             |
| `[font]` size, normal/bold/italic     | `font-size`, `font-family`, `font-family-bold`, `font-family-italic` |
| `[colors.primary]`                    | `background`, `foreground`                                           |
| `[colors.normal]` / `[colors.bright]` | `palette = N=#RRGGBB`, N = 0–15                                      |

**Testing patterns:** [docs/guides/testing-patterns.md](../../guides/testing-patterns.md).
Always `testutil.MockApp`; never `foo.New()` in a test that calls a
state-changing method; always `testutil.VerifyNoRealCommands(t, mockApp.Base)`
against the same base the app uses.

**Commands to run tests** (targeted — CLAUDE.md §6):

```bash
go test ./internal/apps/ghostty/ ./internal/apps/shottr/ ./internal/apps/alacritty/ \
        ./internal/apps/flameshot/ ./internal/apps/registry/ \
        ./internal/tooling/desktop/ ./cmd/
make lint
```

`configs/` changes are read by the root package's embedded-config tests, so the
config steps additionally need `go test .` (slow — ~4.8 min; run it once at
step 8, not per-step).

---

## 3. Objective

`dg install` offers the user a choice of terminal emulator and screenshot tool
from the candidates the platform's own package manager can install, prompting
only when there is more than one, and never reporting an unobtainable app as an
install failure.

---

## 4. Scope Boundary

### In Scope

- [ ] `internal/apps/ghostty/` — new app module implementing `apps.App`
- [ ] `internal/apps/shottr/` — new app module implementing `apps.App`
- [ ] `configs/ghostty/ghostty.conf.tmpl` — Gruvbox Material, transparency,
      font, `starter.sh` launch, at visual parity with the Alacritty template
- [ ] A candidate-selection helper in `internal/tooling/desktop/` that filters by
      platform availability and prompts only when >1 remains
- [ ] Single-choice prompt in `pkg/promptui` (`Select`), if `MultiSelect` cannot
      be reused cleanly
- [ ] apt availability probe (`apt-cache policy`) for the Linux filter
- [ ] Registry, `appToCoordinator`, constants and paths wiring for both new apps
- [ ] Tests for every new unit, including the filter's three cases (many / one /
      none)
- [ ] `docs/guides/theming.md` — add Ghostty to the wiring table and the parity
      rule
- [ ] `docs/apps/ghostty.md`, `docs/apps/shottr.md`
- [ ] `docs/spec.md` — the new selection behavior

### Explicitly Out of Scope

- Removing Alacritty or Flameshot. Both work on Linux; both stay.
- Any non-first-party install route (unsigned-cask tap, community `.deb`,
  building from source). Settled in ADR-0037.
- Finishing multi-theme support. Ghostty's built-in `theme` option makes it
  tempting; `.Theme` stays pinned to `"default"` and the palette stays explicit
  so it cannot drift from tmux and Neovim. Separate cycle.
- Persisting the user's terminal choice for future runs, or a `dg config
set-terminal`. The existing `--only` / `--skip` targeting covers re-running.
- The non-admin/sudo cask failure seen in the same install log. Environmental,
  explicitly deferred by the maintainer.

**Scope is locked.**

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                     | Description                                     |
| ------ | --------------------------------------------- | ----------------------------------------------- |
| Create | `internal/apps/ghostty/ghostty.go`            | App module: cask on macOS, apt on Linux         |
| Create | `internal/apps/ghostty/ghostty_test.go`       | Unit tests, fully mocked                        |
| Create | `internal/apps/shottr/shottr.go`              | App module: cask on macOS, unavailable on Linux |
| Create | `internal/apps/shottr/shottr_test.go`         | Unit tests, fully mocked                        |
| Create | `configs/ghostty/ghostty.conf.tmpl`           | Templated config at parity with Alacritty's     |
| Create | `internal/tooling/desktop/candidates.go`      | Availability filter + chooser                   |
| Create | `internal/tooling/desktop/candidates_test.go` | Filter cases: many / one / none                 |
| Modify | `pkg/promptui/promptui.go`                    | Add `Select` for a single choice                |
| Modify | `internal/tooling/desktop/desktop.go`         | Replace the hardcoded Alacritty/Flameshot calls |
| Modify | `internal/apps/registry/registry.go`          | `Meta` entries for ghostty, shottr              |
| Modify | `cmd/install.go`                              | `appToCoordinator` entries                      |
| Modify | `pkg/constants/constants.go`                  | `Ghostty`, `Shottr`                             |
| Modify | `pkg/paths/paths.go`                          | Ghostty config dirs                             |
| Modify | `docs/guides/theming.md`                      | Ghostty in the wiring table + parity rule       |
| Create | `docs/apps/ghostty.md`, `docs/apps/shottr.md` | App docs                                        |
| Modify | `docs/spec.md`                                | Selection behavior                              |

### Step-by-Step

#### Step 1: Constants, paths, and the empty Ghostty module

- Add `Ghostty = "ghostty"` and `Shottr = "shottr"` to `pkg/constants`.
- Wire Ghostty's config dirs in `pkg/paths/paths.go` alongside Alacritty's.
- Create `internal/apps/ghostty/ghostty.go` with `var _ apps.App = (*Ghostty)(nil)`,
  `Name()`, `Kind()`, and every other method returning the sentinel errors from
  `internal/apps/contract.go`.
- Verify: `go build ./...`

#### Step 2: Ghostty install/uninstall

- `Install()`: `InstallDesktopApp(constants.Ghostty)` on macOS;
  `InstallPackage(constants.Ghostty)` on Linux.
- `SoftInstall()`: the `MaybeInstall*` equivalents. No alias — the cask token,
  apt package and devgeta constant are all `ghostty`.
- `ForceInstall()`: `baseapp.Reinstall(a.Install, a.Uninstall)`.
- Verify: `go test ./internal/apps/ghostty/`

#### Step 3: `promptui.Select`

- Add a single-choice `Select(label string, options []string) (string, error)`
  next to `MultiSelect`, reusing its styling.
- Verify: `go test ./pkg/promptui/`

#### Step 4: The availability filter

- Create `internal/tooling/desktop/candidates.go`:
  - a `candidate` type pairing a name with its app and an availability predicate;
  - `available()` — on Linux, `apt-cache policy <pkg>` reports a candidate
    version; on macOS, a static answer with the upstream reason in a comment;
  - `chooseOne(label, candidates)` — >1 prompts, ==1 returns it silently, ==0
    returns nothing and warns.
- Route the apt probe through the existing command executor so it is mockable.
- Verify: `go test ./internal/tooling/desktop/`

#### Step 5: Wire the terminal group into the coordinator

- Replace the unconditional `InstallAlacritty()` in `InstallAndConfigure` with
  the chooser over `{alacritty, ghostty}`.
- The chosen app's `SoftInstall()` then `SoftConfigure()` runs, exactly as
  `InstallAlacritty` does today.
- Respect the existing `appFilter` / `skipFilter`: naming a terminal with
  `--only` must bypass the prompt and pick that one.
- Verify: `go test ./internal/tooling/desktop/ ./cmd/`

#### Step 6: Shottr module and the screenshot group

- `internal/apps/shottr/shottr.go` — cask `shottr` on macOS; on Linux, report
  unavailable rather than attempting an install.
- Replace `flameshot` in `getCrossPlatformApps()` with the chooser over
  `{flameshot, shottr}`.
- Verify: `go test ./internal/apps/shottr/ ./internal/tooling/desktop/`

#### Step 7: Registry and CLI wiring

- `Meta` entries: both are `Coordinator: "desktop"`. `ItemType` is
  `"desktop_app"` for shottr and for ghostty on macOS — **check what
  `MaybeInstall*` actually stores for ghostty on Linux, where it installs as an
  apt package**, and make `Meta` match rather than assuming.
- Add both to `appToCoordinator` in `cmd/install.go` so `--only ghostty` works.
- Verify: `go test ./internal/apps/registry/ ./cmd/`

#### Step 8: The Ghostty config template

- Write `configs/ghostty/ghostty.conf.tmpl` from the mapping table in §2, taking
  `.Font`, `.Theme` and `.ConfigPath` like the Alacritty template does.
- Point `command` at the existing `configs/alacritty/starter.sh`, or move that
  script to a shared location if pointing at another app's directory reads
  wrong — decide in review, do not do both.
- Implement `ForceConfigure` / `SoftConfigure` on the Ghostty module following
  `alacritty.go`.
- Verify: `go test ./internal/apps/ghostty/` **and** `go test .` (embedded-config
  tests; slow, run here once).

#### Step 9: Docs and visual check

- Add Ghostty to the wiring table and the parity rule in
  `docs/guides/theming.md`.
- Write `docs/apps/ghostty.md` and `docs/apps/shottr.md`.
- Document the selection behavior in `docs/spec.md`.
- Install Ghostty for real and compare side by side with Alacritty: padding,
  opacity, blur, font rendering, the 16 colors, and the titlebar difference
  called out in §2.
- Verify: read the docs; screenshot the comparison.

---

## 6. Verification Plan

### Automated Verification

```bash
go test ./internal/apps/ghostty/ ./internal/apps/shottr/ ./internal/apps/alacritty/ \
        ./internal/apps/flameshot/ ./internal/apps/registry/ \
        ./internal/tooling/desktop/ ./cmd/ ./pkg/promptui/ ./pkg/paths/
go test .            # embedded-config tests — configs/ changed
make lint
```

`pkg/paths` has 24 direct importers, so if step 1's wiring turns out to touch
shared path construction rather than adding leaves, run the full `go test ./...`
and say so.

### Manual Verification

1. `dg install --only ghostty` on macOS → installs, no prompt (single candidate)
2. `dg install --only alacritty` on macOS → reports unavailable with the
   Gatekeeper reason; **does not** surface as an install failure
3. `dg install` on macOS → no terminal prompt appears; Ghostty installs
4. `dg install` on Ubuntu 26.04 → prompt lists both terminals; the chosen one
   installs and the other does not
5. `dg install` on Debian 12 → no prompt; Alacritty installs
6. Launch Ghostty → tmux attaches via `starter.sh`; `echo $TERM` is
   `xterm-256color`
7. Colors, padding, opacity and blur match Alacritty side by side
8. `dg uninstall ghostty` → removes it and clears the global-config entry

### Regression Check

- `dg install --skip desktop` still skips the whole category
- Alacritty on Linux is unchanged — same config, same install path
- `dg install --only terminal` is unaffected (terminal ≠ desktop coordinator)
- `global_config.yaml` records the chosen app under the name `dg uninstall` looks
  for

---

## 7. Risks & Trade-offs

| Risk                                                                              | Likelihood | Mitigation                                                                                                                    |
| --------------------------------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `apt-cache policy` output parsed too loosely, marking an absent package available | Med        | Require a real candidate version, not just exit 0; test against captured output for both present and absent packages          |
| Ghostty config drifts from the Gruvbox palette in tmux/Neovim                     | Med        | Add Ghostty to the theming guide's table in the same PR; the parity rule is manual and this is one more file to remember      |
| A prompt appears in a non-interactive install and hangs                           | Med        | Prompt only when >1 candidate; on macOS and Debian 12 that is never. Confirm behavior when stdin is not a TTY before shipping |
| `Meta` `ItemType` wrong for ghostty on Linux (package, not desktop_app)           | Med        | Step 7 checks what `MaybeInstall*` stored rather than assuming; a mismatch silently breaks `dg uninstall`                     |
| Titlebar difference is visually worse than Buttonless                             | Low        | Step 9 eyeballs it; `macos-titlebar-style` has four values to try                                                             |
| Shottr's purchase prompt annoys users                                             | Accepted   | ADR-0037 records this; `--skip shottr` opts out                                                                               |

### Trade-offs Made

- **Both apps ship, neither is removed.** Costs two configs at theming parity;
  avoids breaking Linux to fix macOS.
- **Probe apt, declare macOS.** Costs a small asymmetry in how the two platforms
  answer the same question; avoids both a distro-version table that rots and a
  network round-trip on every macOS install.
- **Prompt only when >1.** Costs a chooser that is invisible on most platforms
  today; avoids a one-option question and a hang in non-interactive runs.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope is actually locked?
- [ ] Steps are actionable?
- [ ] Verification is executable?
- [ ] Risks are realistic?

**Reviewer notes:**

Two things genuinely need validating before implementation, both flagged inline:

1. **Step 8, `starter.sh` location.** Pointing Ghostty's `command` at
   `configs/alacritty/starter.sh` works but reads wrong. Moving it to a shared
   directory is cleaner and touches Alacritty's shipped config. Pick one.
2. **Step 5, non-interactive installs.** The prompt is new behavior in a command
   that previously never asked about desktop apps. Confirm what happens with no
   TTY before this ships.
