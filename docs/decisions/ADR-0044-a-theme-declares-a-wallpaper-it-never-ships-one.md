# ADR-0044 — A theme declares a wallpaper; it never ships one

**Date:** 2026-09-14
**Status:** ACCEPTED

## Context

`ROADMAP.md:31` scopes the theme command as "Change terminal theme (**with
background image updates**)". So the desktop wallpaper is in scope for
theming from the start, and the question is what devgeta is allowed to put in
a theme file.

The motivating case is concrete. The maintainer's current wallpaper is:

```
/Users/jair.mendez/Downloads/dragon-ball-super-vegeta-uhd-4k-wallpaper.jpg
```

and the ask was to make what is installed today the `default` theme — wallpaper
included. Two facts make "embed it and ship it" the wrong answer, and neither is
technical.

### The image cannot ship

1. **It is third-party copyrighted artwork.** Dragon Ball is Toei/Shueisha
   property. Embedding it in `configs/` puts it inside every release binary and
   redistributes it to every person who installs devgeta, from a repository
   under a license (`LICENSE`) that has no right to grant that. This is not a
   theoretical exposure; `//go:embed all:configs` in `embedded.go` means
   anything under `configs/` is in the binary by construction.

2. **Principle 8 forbids it anyway.** "Everything general, never bespoke" — a
   feature whose value exists only for one person does not ship. One
   maintainer's anime wallpaper as the global default for all users is the exact
   shape §12 calls out: bending a shipped artifact to a devgeta-specific
   decision. That the project is named after Vegeta makes this tempting and does
   not make it general.

A secondary, smaller problem: the file lives in `~/Downloads`, which is not a
durable location. It will not survive the machine move that prompted this work.

### Setting a wallpaper is platform-specific and, on macOS, unsettled

**macOS 26.6** (verified on the maintainer's machine) stores wallpaper state in
`~/Library/Application Support/com.apple.wallpaper/Store/Index.plist`, read by
`WallpaperAgent`. The path is buried in a **nested binary plist** inside
`AllSpacesAndDisplays.Linked.Content.Choices[0].Configuration`, as
`{'type': 'imageFile', 'url': {'relative': 'file:///...'}}`.

Three routes, none clean:

| Route                                          | Dependency     | Problem                                                     |
| ---------------------------------------------- | -------------- | ----------------------------------------------------------- |
| AppleScript (`System Events`, `set picture`)   | none           | Since Sonoma, reliably affects only the current Space       |
| Write `Index.plist` + `killall WallpaperAgent` | none           | Undocumented nested-plist format; breaks on any OS redesign |
| `brew install wallpaper` / cask `desktoppr`    | one new binary | Reliable, maintained, but a new dependency                  |

**The AppleScript route is unverified.** The read-only probe
(`osascript -e 'tell application "System Events" to get picture of desktop 1'`)
was denied by the permission prompt during investigation, so its behavior on
macOS 26.6 is an expectation, not a measurement. It must be measured before
being relied on.

Calling `NSWorkspace.setDesktopImageURL` — the documented Apple API — requires
cgo, which §5 avoids for cross-compilation reasons. It is not an option.

**Linux** is simpler and depends on the desktop, not the distro. devgeta ships
i3 configs (`configs/i3/config`), where `feh --bg-fill` is the conventional
setter. i3 has no wallpaper setting of its own — the image is painted onto the
X root window by a separate program — so the Linux route needs a helper binary
whichever program is chosen, and there is no stdlib alternative short of
speaking X11 directly.

**devgeta does not install that helper today.** `feh` appears nowhere in the
repository: not in `pkg/constants`, not in
`pkg/constants/package_mappings.go`, not in `internal/apps/i3/i3.go` (which
installs `constants.I3` and nothing else), and not in `configs/i3/config`,
which sets no wallpaper at all. So the Linux setter has a dependency this ADR
has to account for, the same way it accounts for the possible macOS one above —
otherwise the supported Linux path is specified down to a command that is not
there.

## Decision

**A theme file may _declare_ a wallpaper. devgeta ships no image bytes, ever.**

1. **`configs/themes/<name>.yaml` gets an optional `wallpaper:` key holding a
   path.** Absent or empty means _leave the desktop alone_. `default.yaml`
   ships with no wallpaper, so a fresh install never touches a user's desktop.

2. **Nothing under `configs/` is ever an image.** No raster asset enters the
   embedded tree. This is enforced by a test against `ConfigsFS`, not by a
   comment — §4 requires that a class of mistake be made structurally
   impossible rather than documented — and the test checks **bytes, not
   filenames**: `http.DetectContentType` over every embedded file rejects
   anything sniffing as `image/*` (PNG, JPEG, GIF, WebP, BMP, ICO) whatever the
   file is called, with a raster-extension check alongside it to catch a
   truncated or empty file whose name claims to be an image. A name-only check
   would not enforce this at all: an image committed as `wallpaper.dat` would
   sail straight through it. The guard is specified in the cycle doc's Step 6.

   **Bounded limit.** A format Go's sniffer does not know — AVIF, HEIC, TIFF —
   carrying an extension the check does not list would still pass. Deliberately
   not closed: a decoder per format is a new dependency (§6) for a case no
   commit has produced, and the sniffer covers every format a wallpaper or icon
   realistically arrives as. The invariant is enforced to that bound, not
   absolutely, and the legal exposure it exists to prevent — redistributing
   someone's artwork in the release binary — is not something that happens by
   accident through a disguised file extension.

3. **The user supplies the image**, via
   `dg theme set-wallpaper <path>`, which copies the file into
   `~/.config/devgeta/wallpapers/` and records the resulting durable path. The
   copy is the point: it decouples the theme from `~/Downloads` and from
   anywhere else transient.

   **The recorded path goes in `global_config.yaml`, not in the theme file.**
   `configs/themes/default.yaml` is embedded (`//go:embed all:configs`) and
   `ExtractEmbeddedConfigs` rewrites every embedded file unconditionally on
   install and after a binary upgrade, so a path `dg theme set-wallpaper` wrote
   into a **shipped** theme file would be erased by the next upgrade with no
   warning. That is the opposite of durable. A user's own theme file is not in
   the embed and would survive — but then the feature would behave differently
   for shipped and user themes, which is worse than one rule. So one rule:

   - `GlobalConfig` gains `wallpapers: map[string]string`, keyed by theme name.
     `dg theme set-wallpaper <path>` copies the image, then writes
     `wallpapers[<current theme>] = <copied path>` through `config.Update`
     (locked load-mutate-save, `internal/config/lock.go`).
   - **Scoped to the theme, by that key.** `dg theme set X` applies
     `wallpapers["X"]` when the map has an entry for `X`; when it does not, it
     **leaves the desktop alone**. It never clears a wallpaper the user set
     outside devgeta, and never carries theme A's image onto theme B.
   - **The copy is named after its bytes, not after the source file.** Keeping
     the source's basename would break the guarantee above: two different images
     both named `wallpaper.jpg` would land on one path, the copy overwrites
     (`files.CopyFile` → `os.WriteFile`, `pkg/files/files.go:64`), and both map
     entries would point at whichever was set last. A content-addressed
     destination removes the case instead of warning about it; the scheme and
     the cleanup of orphaned copies are specified in the cycle doc's Step 9.
   - `wallpaper:` in a theme file keeps the meaning point 1 gives it — a
     _declared default_ a theme author may ship as a path — and is consulted
     only when `wallpapers[<name>]` has no entry. `default.yaml` ships none, so
     a fresh install still never touches a desktop.
   - `dg theme set-wallpaper` with no theme set writes under the fallback name
     `default`, the same name the palette loader falls back to, so the two
     cannot disagree about which theme is current.

4. **Applying a wallpaper is best-effort and says so.** A failure to set the
   desktop picture warns; it never fails `dg theme set`. A theme's terminal,
   editor and AI-coder colors are the deliverable; the desktop is a bonus that
   depends on OS internals devgeta does not control.

5. **The setter is one interface, two implementations** — macOS and i3 — behind
   the existing platform split. The macOS implementation's route is chosen
   _after_ measuring the AppleScript probe above; if AppleScript proves
   per-Space-only, the fallback is the maintained CLI rather than
   reimplementing the plist format.

6. **A setter's helper binary is an optional prerequisite, not something
   devgeta installs.** On Linux that is `feh`; on macOS it is whichever CLI the
   fallback route needs, if one is needed at all. The setter probes for the
   binary at the moment it would use it and, when it is absent, **warns naming
   the binary and how to install it, and leaves the desktop alone**.
   `dg theme set` still succeeds and the rest of the theme still applies.
   devgeta never installs the binary and never prompts to.

   Two reasons:

   - **§6: a new dependency is "a decision to surface in the PR, not a
     default."** Installing `feh` on every Linux machine that runs
     `dg install` makes that decision for everyone, for a feature that is
     inert unless the user has run `dg theme set-wallpaper` — `default` ships
     no wallpaper (point 1), so most installs never call a setter at all.
   - **Point 4 already makes this failure non-fatal**, so a missing binary
     needs no second mechanism — it lands in the same best-effort branch as a
     failed `osascript`. What point 4 did not pin down is the message, and the
     message is the actual requirement here: a warn-only path that says
     nothing is indistinguishable from the Linux wallpaper support silently not
     existing.

   Concretely on Linux: `feh` absent → a warning reading "wallpaper not
   applied: feh is not installed (`sudo apt install feh`); desktop left
   unchanged", exit 0. `feh` present but failing → its stderr in the warning,
   exit 0.

   **Left open for the maintainer:** promoting the helper to something
   `dg install` puts on the machine. That is a product call rather than a
   documentation one — it needs a `pkg/constants` entry, a package mapping,
   tracked installed state in `global_config.yaml`, and a decision about
   whether a Linux user who wanted a terminal setup gets an X11 image viewer
   they did not ask for. Nothing here is blocked on it: the probe is the same
   code either way, and an installed `feh` simply never reaches the warn
   branch.

This keeps the maintainer's wallpaper working on the maintainer's machine —
declared locally, copied somewhere durable — while the repository ships a theme
definition, not someone else's art.

## Consequences

**Easier**

- The licensing question closes permanently. "Does this theme ship an image?"
  has one answer and a test enforcing it.
- Theme files stay small and diffable. A palette plus a path is text; a 4K JPEG
  is several MB per theme in a binary that is already 21 MB.
- Users can point any theme at any wallpaper they own, which is strictly more
  useful than choosing from images devgeta picked.
- `dg theme set-wallpaper` solves the maintainer's actual problem — the image
  moving off `~/Downloads` onto a path that survives a machine move — as a
  general feature anyone benefits from.

**Harder**

- `default` is visually incomplete out of the box: a new user gets the Gruvbox
  terminal and their own untouched desktop. Accepted; the alternative is
  devgeta choosing wallpapers for people, or shipping art it has no right to.
- The machine-specific half of a theme now lives somewhere else. Because the
  copied path is recorded in `global_config.yaml` (point 3), a theme file stays
  portable — but "the theme" is two pieces on disk, and copying only the YAML
  to a new machine brings the colors and not the wallpaper. Accepted: the
  alternative is a theme file that an upgrade overwrites.
- The macOS implementation carries real risk of breaking on an OS update,
  whichever route is chosen. Mitigated by making failure non-fatal (point 4)
  rather than by pretending the risk is absent.

**Accepted trade-offs**

- **Best-effort instead of atomic.** §4 requires installation state be atomic,
  and a wallpaper that silently fails to apply is a partial outcome. Accepted
  narrowly, and only for the wallpaper: it is cosmetic, external, and the
  warning makes the gap visible. Config generation stays atomic.
- **The wallpaper feature has a prerequisite binary on both platforms** —
  `feh` on Linux, and possibly `wallpaper` or `desktoppr` on macOS. §6 says a
  new dependency is "a decision to surface in the PR, not a default" — this ADR
  is that surfacing, for both. Neither is installed by devgeta (point 6):
  absent, the wallpaper step warns with the install command and the theme still
  switches. On macOS a helper is still preferred over reimplementing an
  undocumented nested-plist format, which would be a workaround of the kind §4
  rejects.
- **A Linux user therefore gets no wallpaper until they install `feh`
  themselves**, and learns that from a warning rather than up front. Accepted
  as the cost of not putting an X11 image viewer on every machine that runs
  `dg install`; the warning names the package, so the fix is one command. If
  this turns out to be the common case rather than the rare one, point 6's
  "left open" paragraph is the decision to revisit.
- **No Windows, no GNOME/KDE.** Out of devgeta's platform scope (§8) and out of
  the desktops it configures. A theme's `wallpaper:` key is simply inert where
  no setter exists.
