# Handy

Devgeta installs [Handy](https://handy.computer), a fully-offline speech-to-text desktop application that provides system-wide dictation capabilities across any application.

- **Module:** `internal/apps/handy/`
- **GitHub:** [cjpais/Handy](https://github.com/cjpais/Handy)
- **License:** MIT
- **Platform:** Cross-platform (macOS, Linux)

## Installation

### macOS

Handy is installed via Homebrew cask:

```bash
brew install --cask handy
```

The cask is community-maintained and includes auto-update functionality. Requires macOS 13+ (Ventura or newer).

### Linux

**Manual installation required.** Handy is not available via package managers on Linux. Users must download from [GitHub releases](https://github.com/cjpais/Handy/releases):

- `.deb` file for Debian/Ubuntu
- `.rpm` file for Fedora/RHEL
- `AppImage` for universal Linux

**Additional dependency:** Linux also requires a text injection tool:

- **X11:** `sudo apt install xdotool`
- **Wayland:** `sudo apt install wtype`

When `dg install` runs on Linux, it displays installation instructions rather than attempting automatic installation.

## Technology

- **Frontend:** React + TypeScript
- **Backend:** Tauri (Rust)
- **Models:** Whisper and Parakeet for speech recognition
- **Privacy:** Fully offline, no network communication

## Usage

Handy provides system-wide dictation by injecting recognized speech as typed text into any application. Once installed and launched, it runs in the background and can be activated with its configured hotkey.

## Configuration

Handy does not require separate configuration files. All settings are managed through the application's own interface.
