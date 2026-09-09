# Ghostty

Devgeta offers [Ghostty](https://ghostty.org), a fast, GPU-accelerated terminal emulator, as the platform-filtered alternative to Alacritty (see [ADR-0037](../decisions/ADR-0037-a-desktop-app-is-chosen-by-the-user-from-what-the-platform-can-install.md)). `dg install` chooses between the two automatically — see the selection rules in [docs/spec.md](../spec.md#1-installation-command-dg-install).

- **Module:** `internal/apps/ghostty/`
- **GitHub:** [ghostty-org/ghostty](https://github.com/ghostty-org/ghostty)
- **License:** MIT
- **Platform:** Cross-platform (macOS, Linux)

## Installation

### macOS

Ghostty is installed via Homebrew cask:

```bash
brew install --cask ghostty
```

### Linux

Ghostty is installed via apt:

```bash
apt install ghostty
```

Whether apt actually carries `ghostty` varies by distro and release; `dg install`'s chooser probes `apt-cache policy ghostty` before offering it, so a release without it silently falls back to Alacritty rather than failing.

## Why Ghostty instead of Alacritty

Homebrew disabled the `alacritty` cask on 2026-09-01 because Alacritty's maintainers have declined to notarize it, so it now fails macOS Gatekeeper. This is permanent, not an outage. Ghostty is the closest terminal in devgeta's existing configuration (padding, opacity, blur, font, Gruvbox Material palette) that still installs cleanly on a fresh macOS machine.

## Configuration

`dg configure ghostty` (or `dg install`) renders `configs/ghostty/ghostty.conf.tmpl` to `~/.config/ghostty/config` — Ghostty reads from `$XDG_CONFIG_HOME/ghostty/config` on both platforms.

Notable choices, mapped from the Alacritty template devgeta already shipped:

| Alacritty                           | Ghostty                          | Why                                                                                                                                                                                |
| ----------------------------------- | -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `[env] TERM = "xterm-256color"`     | `term = xterm-256color`          | Ghostty defaults `TERM` to `xterm-ghostty`, which breaks SSH to hosts without that terminfo entry.                                                                                 |
| `decorations = "Buttonless"`        | `macos-titlebar-style = hidden`  | Closest analogue on macOS; not pixel-identical — `hidden` drops the titlebar and keeps native rounded corners, where Buttonless kept the titlebar and only hid the traffic lights. |
| `[terminal.shell] program`          | `command`                        | Both point at the same shared launcher script, `configs/terminal/starter.sh` (see below).                                                                                          |
| `[colors.normal]`/`[colors.bright]` | `palette = N=#RRGGBB` (N = 0-15) | Same Gruvbox Material values as Alacritty's template, so the two never drift — see [docs/guides/theming.md](../guides/theming.md).                                                 |

The `.Theme` template value stays pinned to `"default"` — Ghostty's built-in `theme` option is deliberately unused so the palette can't drift from tmux/Neovim, which don't have theme-switching wired up either (see the theming guide's "Known gaps").

### The shared launcher script

Ghostty launches from launchd's bare GUI environment on macOS, the same as Alacritty, so both terminals reuse the same `starter.sh` — now at `configs/terminal/starter.sh` rather than nested under `configs/alacritty/`, since it belongs to neither app specifically. It repairs `PATH` via `path_helper` before attaching (or creating) a tmux session named `misc`.

## Uninstall

`dg uninstall ghostty` removes the cask (macOS) or apt package (Linux) and clears `~/.config/ghostty`. Ghostty is tracked in `global_config.yaml` under `desktop_apps` on macOS and under `packages` on Linux, matching how it was installed — see `registry.Meta`'s `AltItemType` for ghostty.
