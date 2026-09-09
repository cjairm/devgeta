# Shottr

Devgeta offers [Shottr](https://shottr.cc), a macOS screenshot and annotation tool, as the platform-filtered alternative to Flameshot (see [ADR-0037](../decisions/ADR-0037-a-desktop-app-is-chosen-by-the-user-from-what-the-platform-can-install.md)). `dg install` chooses between the two automatically — see the selection rules in [docs/spec.md](../spec.md#1-installation-command-dg-install).

- **Module:** `internal/apps/shottr/`
- **Website:** [shottr.cc](https://shottr.cc)
- **License:** Freemium (free tier is fully functional; a one-time purchase unlocks OCR and a few extras)
- **Platform:** macOS only — Shottr has no Linux build

## Installation

### macOS

Shottr is installed via Homebrew cask:

```bash
brew install --cask shottr
```

### Linux

Shottr is not available. Every mutating method (`Install`, `SoftInstall`, `Uninstall`) returns `apps.ErrUnsupportedPlatform` rather than attempting an install — in practice this path is rarely hit directly, since `dg install`'s screenshot-tool chooser already filters Shottr out of the pool on Linux before ever calling it (see `internal/tooling/desktop/candidates.go`). `dg install` resolves to Flameshot on Linux instead.

## Why Shottr instead of Flameshot

Homebrew disabled the `flameshot` cask on 2026-09-01 for the same reason as Alacritty's: Flameshot's cask stopped passing macOS Gatekeeper, and Homebrew no longer carries unsigned software in `homebrew/cask`. This is permanent, not an outage. Shottr is a native, actively maintained macOS screenshot tool that installs cleanly.

## Configuration

Shottr needs no configuration files — like Flameshot, it's a GUI application configured entirely through its own interface. `ForceConfigure`/`SoftConfigure` only record it in `global_config.yaml`.

## A note on the purchase prompt

Shottr's free tier covers screenshot capture and basic annotation; a one-time purchase unlocks OCR and a few extras. The app may prompt about this on first launch. This is accepted per ADR-0037 — users who'd rather not see it can run `dg install --skip shottr`.

## Uninstall

`dg uninstall shottr` removes the cask and clears the `shottr` entry from `global_config.yaml`'s `desktop_apps` list.
