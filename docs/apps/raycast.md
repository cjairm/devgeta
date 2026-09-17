# Raycast

Devgeta installs [Raycast](https://www.raycast.com/) as the macOS app launcher.
On Linux it installs [Ulauncher](https://ulauncher.io/) instead; `dg install`
picks by platform.

- **Module:** `internal/apps/raycast/`
- **Platform:** macOS only
- **Install:** `brew install --cask raycast`
- **Configuration:** none deployed — everything lives in Raycast's own settings

## Install

```bash
dg install --only desktop      # or: brew install --cask raycast
```

## Replace Spotlight

macOS owns `Cmd+Space`, so take it in two steps:

1. System Settings → Keyboard → Keyboard Shortcuts → Spotlight → clear "Show
   Spotlight search".
2. Raycast → Settings → General → Raycast Hotkey → set `Cmd+Space`.

To try Raycast without giving up Spotlight, give it `Option+Space` instead.

## Uninstall

```bash
dg uninstall raycast
```

Removes the cask and the `global_config.yaml` entry. Restore the Spotlight
shortcut yourself — uninstalling doesn't give it back.

Raycast's settings aren't covered by `dg export` / `dg import` (Brave is the only
app with an adapter — see [brave.md](brave.md)). Use Raycast → Settings →
Advanced → Export Settings & Data.
