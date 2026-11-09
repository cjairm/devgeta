# Devgeta

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)
[![macOS](https://img.shields.io/badge/macOS-13+-black?logo=apple)](README.md#supported-platforms)
[![Linux](https://img.shields.io/badge/Linux-Debian%2012%2B-orange?logo=linux)](README.md#supported-platforms)

One command to set up a complete, configured development environment. Automates installation and configuration of terminal tools, language runtimes, database systems, and desktop applications on macOS and Linux.

**Core value proposition:** Instead of manually installing 40+ tools across 10+ installers and editing 100+ configuration files, run `dg install` and get a fully configured dev environment in minutes.

---

## 🚀 Quick Start

**1. Install devgeta:**

```bash
curl -fsSL https://raw.githubusercontent.com/cjairm/devgeta/main/install.sh | bash
```

**2. Restart your shell:**

```bash
source ~/.zshrc  # or source ~/.bashrc for bash
```

**3. Set up your environment:**

```bash
dg install
```

That's it! You'll be prompted to select which programming languages, databases, and desktop apps to install.

---

## ✨ What Gets Installed

**Terminal Tools** (always included)

- Shell: Zsh with Powerlevel10k, syntax highlighting, autosuggestions
- Editors: Neovim with LSP support
- Multiplexer: Tmux with custom configuration
- Modern utilities: fzf, ripgrep, bat, eza, zoxide, btop, fd, tldr, lazydocker, lazygit, fastfetch
- Runtime manager: Mise for managing multiple language versions
- Fonts: JetBrains Mono and developer fonts

**Agent Packs** (into each AI coder this run installs)

- `superpowers` (skills and a session bootstrap), `ponytail` (a "simplest thing that works" ruleset) and `devgeta` (response and writing rules), installed into Claude Code, OpenCode and Pi at pinned versions. A coder you already had is left alone; `dg install` prints the command to add the packs to it. Skip with `dg install --skip agent-packs`. See [docs/apps/agent-packs.md](docs/apps/agent-packs.md)

**Programming Languages** (interactive selection)

- Node.js (LTS)
- Python (latest)
- Go (latest)
- PHP
- Rust (latest)

**Database Systems** (interactive selection)

- PostgreSQL
- Redis
- MySQL
- MongoDB
- SQLite

**Desktop Applications** (platform-specific)

- **macOS:** Docker Desktop, Alacritty, Brave, Aerospace, Raycast, GIMP, Flameshot
- **Linux:** Docker Desktop, Alacritty, Brave, i3, Ulauncher, GIMP, Flameshot

---

## 📋 Requirements

### System Requirements

- **macOS:** 13+ (Ventura or newer)
  - Apple Silicon (M1/M2/M3+) or Intel chips
- **Linux:** Debian 12+ (Bookworm) or Ubuntu 24+
  - x86_64 architecture
- **Free disk space:** ~5GB for all tools and databases
- **Internet connection:** Required for downloading packages

### Prerequisites

- **curl** — For downloading the installer
- **bash** — For running the installation script
- **git** — Automatically installed or required for use

---

## Installation

### Quick Install (Recommended)

Install devgeta with a single command. This will:

1. Download the appropriate binary for your OS and architecture
2. Install to `~/.local/bin/devgeta`
3. Configure your PATH automatically — in your shell config for interactive
   shells, and (zsh only) in `~/.zshenv` so shells that never read a profile
   find `devgeta` too: an AI coding agent running `zsh -c`, a git or editor
   hook, cron, launchd. The `~/.zshenv` half is written by `dg install` /
   `dg configure`, not by the installer; see
   [docs/migrations/devgeta-on-path-in-non-interactive-shells.md](docs/migrations/devgeta-on-path-in-non-interactive-shells.md)
   if you installed before it existed.

```bash
curl -fsSL https://raw.githubusercontent.com/cjairm/devgeta/main/install.sh | bash
source ~/.zshrc  # or source ~/.bashrc
```

### Manual Installation (Verify First)

If you prefer to review the installer script:

```bash
# Download the installer
curl -fsSL -o install.sh https://raw.githubusercontent.com/cjairm/devgeta/main/install.sh

# Review it
cat install.sh

# Run it
bash install.sh
```

## Usage

### Install Development Environment

Run the interactive installer to set up your development environment:

```bash
devgeta install
```

Or use the short alias:

```bash
dg install
```

This will guide you through:

1. Installing terminal tools and shell configuration
2. Selecting programming languages to install
3. Selecting database systems to install
4. Installing desktop applications

### Category-Specific Installation

Install only specific categories:

```bash
# Install only terminal tools
dg install --only terminal

# Install only languages (interactive selection)
dg install --only languages

# Install only databases (interactive selection)
dg install --only databases

# Install only desktop applications
dg install --only desktop

# Combine multiple categories
dg install --only terminal,languages
```

### Skip Categories

Install everything except specific categories:

```bash
# Install everything except desktop apps
dg install --skip desktop

# Skip multiple categories
dg install --skip databases,desktop
```

### Available Commands

- `dg install` - Install and configure development environment
  - `--only <categories>` - Install only specified categories (terminal, agent-packs, languages, databases, desktop)
  - `--skip <categories>` - Install everything except specified categories
  - `--verbose` - Enable verbose logging
- `dg agent-pack` - Install agent packs (skills, hooks, rules) into Claude Code, OpenCode and Pi. See [docs/apps/agent-packs.md](docs/apps/agent-packs.md)
  - `dg agent-pack list` - Each pack's pinned and installed version, and each coder's state
  - `dg agent-pack install <pack>` - Install `superpowers`, `ponytail` or `devgeta` into every installed coder
    - `--coder <claude,opencode,pi>` - Only these coders
    - `--exclude <skills>` - Leave these skills out (saved; `--exclude=` clears it)
  - `dg agent-pack uninstall <pack>` - Remove a pack, deleting only what devgeta installed (`--coder` limits it)
  - `dg agent-pack update [pack]` - Move installed packs to this devgeta's pinned version and rebuild them
  - `dg agent-pack check <pack>` - Build and check a pack without installing it (`--ref <tag>` tries another upstream tag)
- `dg worktree` (alias: `dg wt`) - Manage git worktrees with tmux windows and AI coders
  - `dg wt create <name>` - Create a worktree + tmux window + launch AI
    - `--ai <opencode|claude>` - AI coder to launch (mutually exclusive with `--layout`)
    - `--layout <opencode|claude|claude-nvim|nvim|shell>` - Window layout to build (mutually exclusive with `--ai`)
    - `--prompt <text>` - Launch the AI coder with `<text>` as its opening prompt, so the session starts already working on it. Passed as a launch argument, so it can't be dropped. Needs a layout that has an AI pane (`--prompt` with `--layout nvim` is an error)
    - `--pane <command>` - Add a shell pane beside the layout running `<command>` (e.g. `make finit`). Repeatable; the value is a shell command line used as written, so `'cd api && make dev'` works
  - `dg wt list` - List all managed worktrees
  - `dg wt remove [name]` - Remove a worktree (fzf picker if name omitted)
  - `dg wt repair <name>` - Recreate the tmux window for an existing worktree
  - `dg wt move <name>` - Move a worktree between the shared and in-repo locations (alias: `mv`)
  - `dg wt prune` - Remove all managed worktrees
  - `dg wt prune --stale` - Clear git's leftover entries for already-deleted worktrees (removes nothing of yours)
- `dg ws` (alias: `dg workspace`) - Unified TUI dashboard: repo workspaces (worktrees, circle `●`/`○` marker, expandable, `N trees` badge) and standalone tmux sessions (square `■`/`□` marker) in one flat list (`enter` switch/attach, `d` delete/kill, `s` new session, `n`/`N` new worktree, `/` filter, `q` quit). Also bound to bare `ctrl+t` in tmux.
  - `enter` on a repo's header row switches to the non-worktree window in that repo's tmux session — the plain shell alongside its worktree windows, which has no row of its own. The header is only selectable when there is such a window to reach ([ADR-0048](docs/decisions/ADR-0048-a-repo-header-reaches-its-sessions-plain-window.md)).
- `dg list` (alias: `dg installed`) - Show everything Devgeta has installed, grouped by category
  - `--category <name>` - Filter to one bucket (`packages`, `desktop_apps`, `fonts`, `themes`, `terminal_tools`, `dev_languages`, `databases`)
- `dg config` - View and change user-settable devgeta settings (`worktree.*` in `global_config.yaml`) without hand-editing YAML
  - `dg config` / `dg config list` - List every setting: current value, default, and description
  - `dg config get <key>` - Print one setting's effective value (for scripts)
  - `dg config set <key> <value...>` - Validate and persist a value (`worktree.search_paths` takes multiple values)
  - `dg config unset <key>` - Clear a setting back to its default
- `dg task` (alias: `dg t`) - Developer utilities for git, npm, releases and scratch space (mirrors `dge` shell function)
  - `dg task refresh-branch [target]` - Checkout target (default: `main`), pull, return to previous branch, merge
  - `dg task reset-main-branch` - Checkout `main`, hard-reset to `origin/main`
  - `dg task delete-branch [target]` - Checkout target, fetch, pick a branch via fzf to force-delete
  - `dg task reinstall-libraries` - Clean ignored files, remove `node_modules/`, run `npm install`
  - `dg task reinstall-library <name>` - Remove `node_modules/<name>`, run `npm install`
  - `dg task release <version> --message-file <file> [--push]` - Squash unpushed commits, create an annotated tag, optionally push
  - `dg task scratch [--key <name>] [--clean <path>]` - Allocate or remove a devgeta-owned scratch directory
- `dg archive <source> <destination-dir>` - Pack a folder onto an external drive for a machine move (one-shot, not an incremental backup); skips only what's proven regenerable (`node_modules`, virtualenvs, build caches — never by name alone), then verifies the archive against the source. Shows a live progress bar with the rate and time remaining for both the write and the verify. Reads the source only, and refuses to start if any file it would write is already on the drive — it never overwrites or deletes anything there
  - `--dry-run` - Scan and print the report without writing anything
  - `--gzip` - Write `.tar.gz` instead of the default `.tar.zst`
  - `--no-skip` - Disable every skip rule; archive everything
  - `--no-verify` - Skip the post-write verification pass
  - `--mac-metadata` - Include extended attributes as pax records (macOS only)
  - `--yes` - Skip the confirmation prompt
  - `dg archive verify <archive-file>` - Re-check an archive against its manifest
  - Decompress with a plain `tar`, no devgeta needed — always into a directory of its own, since a bare `tar -xf` unpacks into the current one: `mkdir -p NAME && tar --zstd -xf NAME.tar.zst -C NAME` (use `tar -xzf` for a `--gzip` archive). List without extracting with `tar --zstd -tf NAME.tar.zst`; check it with `shasum -a 256 -c NAME.tar.zst.sha256`. Full command reference in [docs/spec.md](docs/spec.md#dg-archive)
- `dg export <app> <destination-dir>` - Pack the state you accumulated _inside_ an app — bookmarks, open tabs, settings — into one verified bundle for a machine move, then restore it with `dg import`. This is an **allowlist**: only what the app's adapter names ever moves, so passwords, cookies, autofill and anything bound to this machine are never included at any flag. Not a backup of the app's folder — a measured Brave profile is 3.8G of which ~22M is worth moving, so use `dg archive` if you want the folder itself
  - `--dry-run` - Print every group with its paths, its size and whether it's on, and write nothing (no destination directory needed)
  - `--group a,b` - Export only these groups; the default is every group that's on
  - `--profile "Default,Profile 1"` - Export only these profiles; the default is all of them
  - `dg import <app> <bundle-file>` - Restore a bundle on the new machine. Everything it would replace is renamed aside first, so a failure partway leaves the profile exactly as it was and an unwanted restore can be undone by hand; `--force` is required if the profile already has state
  - Both directions refuse while the app is running — its state is SQLite and LevelDB, and a copy taken from under a live process arrives corrupt. Quit it first
  - The bundle is a plain `.tar.zst` with a `.sha256` beside it, readable without devgeta: `tar --zstd -tf NAME.tar.zst`. Brave is the only app with an adapter today — [docs/apps/brave.md](docs/apps/brave.md) says exactly what moves and what does not. Full command reference in [docs/spec.md](docs/spec.md#dg-export--dg-import)
- `dg theme` - Show, list, or switch the active theme across Alacritty, Ghostty, tmux, Neovim, OpenCode, Claude, and i3 together
  - `dg theme list` - List available themes
  - `dg theme set <name>` - Switch every installed themed app to `<name>`, transactionally — validates first, rolls the whole machine back on any failure, and skips apps that aren't installed. See [docs/guides/theming.md](docs/guides/theming.md)
- `dg --version` - Show version information
- `dg --help` - Show help message

## ⚙️ Configuration

Devgeta manages configurations in `~/.config/devgeta/`:

- **Global state**: `global_config.yaml` tracks installed packages
- **Shell integration**: `devgeta.zsh` sourced from your shell config
- **App configs**: Application-specific configuration templates

### Customization

After installation, you can customize:

- Neovim: `~/.config/nvim/init.lua`
- Tmux: `~/.tmux.conf`
- Alacritty: `~/.config/alacritty/alacritty.toml`
- Git: `~/.config/git/.gitconfig`
- i3 (Linux): `~/.config/i3/config`
- Aerospace (macOS): `~/.config/aerospace/aerospace.toml`

## ❓ Troubleshooting

### Common Issues

**Binary not found after installation:**

```bash
# Restart your shell to pick up PATH changes
exec $SHELL

# Or manually source shell config
source ~/.zshrc  # or ~/.bashrc
```

**Permission denied when installing:**

```bash
# Ensure install directory is writable
mkdir -p ~/.local/bin
chmod 755 ~/.local/bin
```

**Unsupported platform:**

- Devgeta requires macOS 13+ or Debian 12+/Ubuntu 24+
- Only amd64 and arm64 (Apple Silicon) architectures are supported

**Mise commands not found after installation:**

```bash
# Mise needs to be activated in your shell
exec $SHELL
# Or manually activate:
eval "$(mise activate zsh)"  # or bash, fish, etc.
```

**Language version not available:**

```bash
# List installed versions
mise list

# Install specific version
mise install node@20
mise use --global node@20
```

For more help, see the [full documentation](docs/) or [open an issue](https://github.com/cjairm/devgeta/issues).

---

## 📚 Documentation

- **[Product Specification](docs/spec.md)** — Detailed feature documentation, edge cases, testing strategy
- **[Architecture](docs/guides/cross-platform-installation.md)** — Cross-platform installation strategy and package mappings
- **[App Guides](docs/apps/)** — Individual configuration guides for each installed app
- **[Roadmap](ROADMAP.md)** — Planned features, future commands, and open questions
- **[Contributing Guide](CONTRIBUTING.md)** — Development setup, testing, release process

---

## 🤝 Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for:

- Development setup and build instructions
- Testing and code quality standards
- Git workflow and PR process
- Release procedures

Quick links:

- [Open an issue](https://github.com/cjairm/devgeta/issues)
- [View the roadmap](ROADMAP.md)
- [See development guide](CONTRIBUTING.md)

---

## 📄 License

[MIT License](LICENSE) — Use devgeta freely in personal and commercial projects.

---

## Advanced Usage

### Building from Source

### Prerequisites

- Go 1.25 or newer
- Git

### Build Commands

```bash
# Build for current platform
make build

# Build all platform binaries
make all

# Build specific platforms
make build-darwin-arm64    # macOS Apple Silicon
make build-darwin-amd64    # macOS Intel
make build-linux-amd64     # Linux/Debian/Ubuntu

# Run the full test suite (day to day, run the changed package plus its direct
# importers instead — see CLAUDE.md §6, "Which tests to run")
make test

# Code quality checks
make lint

# Clean build artifacts
make clean
```

### Testing Local Builds

Test a local build before releasing:

```bash
# Build for your platform
make build

# Install using local binary
bash install.sh --local ./devgeta-darwin-arm64
```
