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
3. Configure your PATH automatically

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

# Install only AI tools (rtk, …)
dg install --only ai-tools

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
  - `--only <categories>` - Install only specified categories (terminal, languages, databases, desktop, ai-tools)
  - `--skip <categories>` - Install everything except specified categories
  - `--verbose` - Enable verbose logging
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
- `dg list` (alias: `dg installed`) - Show everything Devgeta has installed, grouped by category
  - `--category <name>` - Filter to one bucket (`packages`, `desktop_apps`, `fonts`, `themes`, `terminal_tools`, `dev_languages`, `databases`)
- `dg config` - View and change user-settable devgeta settings (`worktree.*` in `global_config.yaml`) without hand-editing YAML
  - `dg config` / `dg config list` - List every setting: current value, default, and description
  - `dg config get <key>` - Print one setting's effective value (for scripts)
  - `dg config set <key> <value...>` - Validate and persist a value (`worktree.search_paths` takes multiple values)
  - `dg config unset <key>` - Clear a setting back to its default
- `dg task` (alias: `dg t`) - Developer utilities callable by agents and humans (mirrors `dge` shell function)
  - `dg task refresh-branch [target]` - Checkout target (default: `main`), pull, return to previous branch, merge
  - `dg task reset-main-branch` - Checkout `main`, hard-reset to `origin/main`
  - `dg task delete-branch [target]` - Checkout target, fetch, pick a branch via fzf to force-delete
  - `dg task reinstall-libraries` - Clean ignored files, remove `node_modules/`, run `npm install`
  - `dg task reinstall-library <name>` - Remove `node_modules/<name>`, run `npm install`
  - **Review scope tasks (noise-filtered git context for agents):**
    - `dg task review-scope` - Fetch + orient in one call: branch, default branch, ahead/behind, commits, per-file stats (lockfile-style noise excluded and noted)
    - `dg task branch-diff [--file <path>]` - Merge-base diff against the default branch, same exclusions applied; `--file` bypasses them for one file
  - **Pull request tasks (via `gh`, formatted for agents):**
    - `dg task review-threads [--pr N] [--state unresolved|resolved|all]` - Show PR review threads as compact markdown
    - `dg task resolve-thread <id>` / `unresolve-thread <id>` - Resolve/reopen a review thread
    - `dg task reply-thread <id> <body>` - Reply to a review thread
    - `dg task create-pr --title T [--body B] [--base main]` - Open a PR from the current branch
    - `dg task update-pr-description [--pr N] --body B` - Replace a PR description
    - `dg task approve-pr [--pr N] [--body B]` - Approve a PR
    - `dg task request-changes-pr [--pr N] --body B` - Request changes on a PR
    - `dg task request-review [--pr N] <reviewer>...` - Re-request review from reviewers (adds them back to the reviewers list)
    - `dg task comment-pr [--pr N] --body B` - Post a top-level PR comment
    - `dg task merge-pr [--pr N] [--method squash|merge|rebase]` - Merge a PR
    - `dg task pr-view [--pr N]` / `pr-checks [--pr N]` - Compact PR summary / CI check status
    - `dg task current-pr` / `current-repo` - Resolve the current branch's PR number / `owner/name`
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
