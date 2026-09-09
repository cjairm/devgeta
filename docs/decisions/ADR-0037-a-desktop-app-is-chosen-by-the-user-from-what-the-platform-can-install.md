# ADR-0037 — A desktop app is chosen by the user from what the platform can install

**Date:** 2026-09-09
**Status:** ACCEPTED

## Context

devgeta hardcodes one terminal emulator (Alacritty) and one screenshot tool
(Flameshot), installs them unconditionally, and has no way to express "this app
is not obtainable here." That assumption broke on macOS.

Homebrew disabled both casks on 2026-09-01 for failing the macOS Gatekeeper
check. This is not a temporary outage: Homebrew has stopped carrying unsigned
software in `homebrew/cask`, and Alacritty's maintainers have declined to
notarize. Two consecutive fresh-machine installs reported the same four
failures, and every future run on macOS will too.

The obvious repair — swap in a maintained replacement — runs into the fact that
no single app is installable everywhere:

|               | macOS           | Debian 12 / Ubuntu 24 | Ubuntu 26.04+ |
| ------------- | --------------- | --------------------- | ------------- |
| **Alacritty** | ✗ cask disabled | ✓ apt                 | ✓ apt         |
| **Ghostty**   | ✓ official cask | ✗ not packaged        | ✓ apt         |
| **Flameshot** | ✗ cask disabled | ✓ apt                 | ✓ apt         |
| **Shottr**    | ✓ cask          | ✗ macOS-only          | ✗ macOS-only  |

Three constraints bound the answer:

- **Principle 3 (cross-platform consistency)** wants the same command and the
  same outcome on both platforms. No terminal satisfies that today.
- **Principle 7 (visual consistency)** ties the terminal's palette to tmux,
  Neovim and the AI-coder configs. Whatever ships must carry the Gruvbox
  convention.
- **§4 (security)** forbids executing unverified downloaded code. The routes
  that would fill the empty cells above are a third-party tap that exists
  specifically to re-publish casks Homebrew disabled for failing Gatekeeper,
  and a community-built `.deb` for Ghostty on older Debian/Ubuntu.

A fourth constraint is process, not technical: §10 lists "modifying what a
category includes" as something that must never happen silently. Replacing the
terminal is exactly that.

## Decision

**devgeta ships every candidate it supports, and installs the ones the
platform's own package manager can actually provide — asking the user only when
more than one is available.**

Four parts:

1. **Both candidates ship as app modules.** Alacritty and Ghostty both remain
   in the tree as terminal emulators; Flameshot and Shottr both ship as
   screenshot tools. Nothing is deleted — Alacritty is fully working on Linux,
   and removing it would break a platform to fix another.

2. **The coordinator offers a choice, not a default.** For each such group it
   builds the list of candidates installable on this machine, then:
   - more than one candidate → prompt the user to pick (the same
     `promptui` selection shape already used for languages and databases);
   - exactly one → install it without prompting, because a one-option prompt
     is a question with no answer;
   - none → warn once, naming why, and continue. A category with nothing
     installable must not read as an install failure.

3. **First-party sources only.** Only what Homebrew's own taps and the
   distribution's own apt repositories carry. No unsigned-cask tap, no
   community `.deb`, no building from source. This is what keeps §4 intact,
   and it is the reason the matrix above has empty cells rather than
   footnotes.

4. **Linux availability is probed, macOS availability is declared.** On Linux
   the candidate list is filtered by asking apt (`apt-cache policy <pkg>`) —
   local, no network, and correct on every Debian and Ubuntu release without a
   distro-version table that would need editing each time a distro ships
   Ghostty. On macOS the two exclusions are recorded as static facts with the
   upstream reasoning next to them, because "Homebrew disabled this cask" is a
   deliberate upstream decision rather than a property of the machine, and
   probing it would cost a network round-trip on every install to re-learn
   something already known.

## Consequences

**Easier.**

- A disabled or unpackaged app stops being an install failure. `dg install`
  reports what it cannot obtain and moves on, instead of erroring three times
  per app on every run forever.
- Users on a platform with real alternatives get to pick, rather than
  inheriting the maintainer's preference.
- Adding a fourth terminal later is a list entry plus a config template, not a
  new branch in the coordinator.
- Ghostty ships Gruvbox Material as a built-in theme — the same palette
  `configs/alacritty/alacritty.toml.tmpl` maintains by hand — which is a
  usable path out of the unfinished multi-theme work described in
  `docs/guides/theming.md`.

**Harder.**

- **Two terminal configs to keep at parity.** Principle 7 now spans six
  configs, not five. A palette change must land in Ghostty's template too, and
  nothing enforces that automatically — it is the same manual rule the theming
  guide already carries, applied to one more file.
- **The choice is only real in one cell today.** On macOS the prompt will not
  appear (Ghostty alone); on Debian 12 and Ubuntu 24 it will not appear
  (Alacritty alone). Only Ubuntu 26.04+ sees both. The machinery is built for a
  situation that is currently rare, on the expectation that packaging moves.
- **Screenshot tools never overlap.** Shottr is macOS-only and Flameshot is
  effectively Linux-only now, so that group is always platform-determined. It
  uses the same mechanism for consistency, not because it needs one.
- **Shottr is freemium.** Free indefinitely, but it periodically asks the user
  to buy it, and its author has said pricing will rise. Shipping it as the
  macOS default means shipping that prompt. Accepted deliberately: it is the
  only maintained, notarized screenshot cask found, and `dg install --skip
shottr` opts out.
- **Two empty cells stay empty.** Users who specifically want Alacritty on
  macOS, or Ghostty on Debian 12, are told it is unavailable and pointed at the
  upstream reason. devgeta will not install it for them by an unverified route.
  If Alacritty ever notarizes, or Debian packages Ghostty, the fix is one list
  entry.
