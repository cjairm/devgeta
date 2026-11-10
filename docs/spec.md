# Devgeta Product Specification

**Last Updated**: 2026-07-15  
**Owner**: @cjairm

---

## Overview

Devgeta is a cross-platform development environment manager that automates installation and configuration of tools, runtimes, databases, and applications on macOS and Linux (Debian/Ubuntu).

**Core value proposition**: One command to install a complete, configured development environment instead of manual setup across 10+ tools and 100+ configuration files.

**Core features:**

- **Smart installation**: Detects existing packages to avoid conflicts
- **Global state tracking**: Maintains what was installed by devgeta vs pre-existing
- **Interactive selection**: TUI-based multi-select for languages and databases
- **Safe operations**: Only manages devgeta-installed packages
- **Configuration templates**: Consistent, reproducible configs across machines

---

## Architecture

```
devgeta/
├── cmd/                     # Cobra CLI commands
├── internal/
│   ├── tooling/            # Category-based coordinators
│   │   ├── terminal/       # Dev tools, shell, editors
│   │   ├── languages/      # Runtime management via Mise
│   │   ├── databases/      # Database systems
│   │   └── worktree/       # Git worktree management
│   ├── apps/               # Individual app implementations (21 apps)
│   ├── commands/           # Platform-specific installers (Darwin, Debian)
│   ├── config/             # State management
│   └── tui/                # Interactive UI components
├── pkg/                    # Shared utilities (logger, paths, constants)
├── configs/                # Configuration templates (embedded at compile time)
└── docs/                   # Documentation
```

**Key patterns:**

- **Interface-based design** for cross-platform compatibility
- **Strategy pattern** for installation (AptStrategy, PPAStrategy, NerdFontStrategy, etc.)
- **Factory pattern** for platform detection
- **Coordinator pattern** for category orchestration (see `internal/tooling/languages/` as reference)

---

## Features

### 1. Installation Command: `dg install`

Primary entry point for setting up a development environment.

#### Behavior

- Interactive mode: `dg install` launches interactive prompts for category selection
- Category filtering: `--only category1,category2`
- Category exclusion: `--skip category1`
- Per-app targeting: `--only appname` or `--skip appname` (registry apps only)
- Verbose logging: `--verbose`

#### Per-App Targeting

`--only` and `--skip` accept both category names and individual app names from the registry.

**Granularity levels:**

```
dg install --only terminal          # full terminal category (existing behavior)
dg install --only neovim            # single app by name — only neovim installed
dg install --skip git               # skip git; install everything else normally
dg install --only terminal --skip lazygit  # full terminal minus lazygit
dg install --only neovim --only docker    # neovim (terminal) + docker (desktop) only
```

**Behavior when an app filter is active** (`--only <appname>`):

- Only the specified registry apps are installed in that coordinator
- `InstallDevTools` and `InstallCoreLibs` are skipped (user asked for a specific app, not a full setup)
- Fonts installation is also skipped in the desktop coordinator

**Individually targetable apps** (registry-managed, 21 apps):

| Coordinator | Apps                                                                                          |
| ----------- | --------------------------------------------------------------------------------------------- |
| terminal    | claude, fastfetch, git, lazydocker, lazygit, mise, neovim, opencode, pi, tmux                 |
| desktop     | aerospace, alacritty, brave, docker, flameshot, ghostty, gimp, i3, raycast, shottr, ulauncher |

**Note on alacritty and ghostty:** both have `KindTerminal` in the registry
but are installed by the desktop coordinator. Use `--only alacritty` or
`--only ghostty` (not `--only terminal`) to target one specifically.

**Terminal and screenshot tool selection (ADR-0037):** `dg install` no longer
installs a fixed terminal emulator and screenshot tool. Instead, for each of
the two groups — {`alacritty`, `ghostty`} and {`flameshot`, `shottr`} — it
filters the group down to whichever candidates the platform's own package
manager can actually install, then:

- **0 available:** warns and skips the group; nothing is installed for it,
  and this is never reported as an install failure.
- **1 available:** installs it silently — no prompt. This is every platform
  today: macOS resolves to `ghostty` + `shottr` (Homebrew disabled the
  `alacritty` and `flameshot` casks on 2026-09-01 — permanent, not an outage),
  Debian 12 resolves to `alacritty` + `flameshot` (no Linux build of either
  `ghostty` or `shottr` exists in its apt repos yet).
- **2 available:** prompts once per group (e.g. Ubuntu 26.04, where apt
  carries both terminals) and installs the chosen one.

`--only <name>` or `--skip <name>` on a specific app in either group narrows
the group to that one name **before** availability is checked, bypassing the
prompt even when the named app turns out unavailable on this platform — it is
then skipped with the same "not available" warning as the 0-available case
above, never surfaced as a failure. See
`internal/tooling/desktop/candidates.go` (the filter + chooser) and
`internal/tooling/desktop/desktop.go`'s `chooseTerminal`/`chooseScreenshot`.

**Not individually targetable** (no registry entry):

- Core libs: autoconf, bison, ncurses, openssl, etc.
- Dev tools: bat, fzf, ripgrep, zoxide, etc.
- Languages: node, python, go, rust, php (use interactive selection)
- Databases: postgresql, redis, mysql, etc. (use interactive selection)

#### Categories

**Terminal Tools** (no selection, always installed)

- Essential: curl, unzip, git, gh (GitHub CLI)
- Shell: Zsh with Powerlevel10k, syntax highlighting, autosuggestions
- Editors: Neovim with LSP support
- Multiplexer: Tmux with custom configuration
- Modern utilities: fzf, ripgrep, bat, eza, zoxide, btop, fd, tldr, lazydocker, lazygit, fastfetch
- Runtime manager: Mise for language version management
- Fonts: JetBrains Mono and developer fonts

**Agent packs** (after terminal tools; `--skip agent-packs` to turn off)

- `devgeta`, `superpowers` and `ponytail`, installed into each AI coder
  (Claude Code, OpenCode, Pi) **that this run installed**. A coder that was
  already installed is left alone, and the `dg agent-pack install` commands for
  it are printed. `--only agent-packs` is an explicit request: it installs the
  packs into every configured coder. A pack that fails is reported with its
  retry command and does not stop the other categories. See
  [`dg agent-pack`](#dg-agent-pack) and [docs/apps/agent-packs.md](apps/agent-packs.md).

**Languages** (interactive selection)

- Node.js (LTS)
- Python (latest)
- Go (latest)
- PHP (native package)
- Rust (latest)
- [Others to be added]

**Databases** (interactive selection)

- PostgreSQL
- Redis
- MySQL
- MongoDB
- SQLite

**Desktop Applications** (category-level selection)

_macOS_ (resolves to `ghostty` + `shottr` today — see the terminal/screenshot
selection note above):

- Docker Desktop
- Ghostty terminal
- Brave browser
- Aerospace window manager
- Raycast launcher
- GIMP
- Shottr

_Linux (Debian/Ubuntu)_ (resolves to `alacritty` + `flameshot` today):

- Docker Desktop
- Alacritty terminal
- Brave browser
- i3 window manager
- Ulauncher launcher
- GIMP
- Flameshot

---

### 2. Configuration Management

#### Persistent State: `~/.config/devgeta/`

**`global_config.yaml`**

- Tracks installed packages (name, version, category, timestamp)
- Prevents duplicate installations
- Used by other commands to detect what's already installed

**`devgeta.zsh`**

- Shell integration script sourced from `~/.zshrc`
- Sets up Mise activation, aliases, and environment variables
- Platform-specific customizations

**App-specific configs**

- `neovim/init.lua` — Neovim configuration
- `tmux/.tmux.conf` — Tmux configuration
- `alacritty/alacritty.toml` — Terminal emulator config
- `ghostty/config` — Terminal emulator config (Ghostty's alternative to Alacritty)
- `git/.gitconfig` — Git configuration (extends user's existing config)
- Platform-specific: `i3/config` (Linux), `aerospace/aerospace.toml` (macOS)

#### Installation Idempotency

- Check `global_config.yaml` before installing
- Only install if not already present
- Skip system packages that conflict with existing installations (with user prompt)

---

### 3. Command Reference

**Current commands**:

#### `dg install`

See [Installation Command](#1-installation-command-dg-install) above.

#### `dg agent-pack`

Installs, updates and removes **agent packs**: skills, hooks, commands and rules
for Claude Code, OpenCode and Pi. Full guide: [docs/apps/agent-packs.md](apps/agent-packs.md).
Decisions: [ADR-0068](decisions/ADR-0068-agent-packs-are-a-pinned-upstream-plus-a-build-layer.md),
[ADR-0069](decisions/ADR-0069-pack-customization-only-through-a-build-time-override-layer.md),
[ADR-0070](decisions/ADR-0070-pack-removal-deletes-only-what-devgeta-installed.md).

```
dg agent-pack list
dg agent-pack install <pack> [--coder claude,opencode,pi] [--exclude skill,...]
dg agent-pack uninstall <pack> [--coder ...]
dg agent-pack update [pack]
dg agent-pack check <pack> [--ref <tag>]
```

Packs: `superpowers` and `ponytail` (pinned upstream tags and commits, in
`configs/agent-packs/<pack>/pack.yaml`) and `devgeta` (ships in the binary, carries
`DEVGETA.md` and the `review` skill; devgeta's wrapper over Superpowers'
`receiving-code-review` ships in the `superpowers` override layer).

**Behavior**

- `install` fetches the pack at its pinned tag and checks the commit, builds one
  tree in `~/.local/share/devgeta/agent-packs/<pack>/` (excluded skills removed,
  then devgeta's and the user's override layers on top), and installs it into each
  selected coder through that coder's own system: a `devgeta` plugin marketplace
  for Claude Code, a `plugins`, `instructions` or `skills` entry in `opencode.json`, and
  `pi install <tree>` for Pi. Without `--coder` it targets every installed coder.
- A coder that isn't installed is skipped with a reason, which is not a failure.
  What a coder can't load is named. If a coder fails, the coders added in this
  run are undone and `global_config.yaml` is left unchanged.
- `--exclude`: a new list replaces the saved one, `--exclude=` clears it, no flag
  keeps it. Excluding a skill a kept skill names is refused, naming both.
- Another install of the same upstream is a **conflict**: devgeta prints the
  command that removes it and installs nothing. It never removes it.
- `uninstall` removes only what `agent_packs:` in `global_config.yaml` records. The
  `@DEVGETA.md` line in `~/.claude/CLAUDE.md` is removed only if devgeta added it
  and it is still exactly that line.
- `update` moves to the pin in this binary and always rebuilds, so changed
  overrides are applied.
- `check` builds into a temporary folder and runs the build checks; it runs
  `claude plugin validate` when Claude Code is present, prints the Pi version when
  Pi is present, and lists fully replaced files whose upstream copy changed. It
  installs nothing. `--ref` tries a tag devgeta doesn't pin.
- `list` shows each pack's pinned and installed version and each coder's state,
  and flags a recorded Pi package that Pi's settings no longer list.
- `dg uninstall claude|opencode` drops that coder from every pack's record (their
  config folders are deleted); `dg uninstall pi` keeps Pi's packages and records.

**Edge cases**: a moved upstream tag (commit mismatch) installs nothing; a failed
build leaves the previous tree as it was; `node` or `bash` missing is a warning
after the install, not a failure.

#### `dg configure [app]`

Re-applies configuration files for a named app without reinstalling the app itself.

```
dg configure <app> [--force]
```

**Flags**:

- `--force` — Overwrite existing configuration files. Without this flag, configuration is only applied if files do not already exist (soft mode).

**Behavior**:

- Exact app name required (case-sensitive). Supported apps: `aerospace`, `alacritty`, `brave`, `claude`, `devgeta`, `docker`, `fastfetch`, `flameshot`, `ghostty`, `gimp`, `git`, `i3`, `lazydocker`, `lazygit`, `mise`, `neovim`, `opencode`, `pi`, `raycast`, `shottr`, `tmux`, `ulauncher`.
- Apps that have no configuration to deploy (e.g., `brave`) return `ErrConfigureNotSupported` — the command prints an info message and exits zero.
- Unknown app names print a sorted list of supported apps and exit non-zero.

**Examples**:

```
dg configure git            # Apply git config if not already present
dg configure neovim --force # Overwrite existing neovim config
dg configure brave          # Info: configure not supported for brave (exit 0)
dg configure foo            # Error: unknown app "foo" + supported list (exit non-zero)
```

**Planned commands**: See [ROADMAP.md](ROADMAP.md) for planned features and future commands.

#### `dg completion [shell]`

Generates a shell completion script for the given shell.

```
dg completion [bash|zsh|fish|powershell]
```

**Behavior**:

- Prints the completion script to stdout; source it or add to your shell config.
- Example: `dg completion zsh > ~/.zsh/completions/_dg`
- Tab completion is pre-wired for `dg worktree remove <name>`.

#### `dg worktree`

Manages git worktrees with integrated tmux sessions and AI coders.

```
dg worktree <subcommand> [flags]
dg wt <subcommand> [flags]     # alias
```

**Subcommands**:

| Subcommand      | Description                                                                                         |
| --------------- | --------------------------------------------------------------------------------------------------- |
| `create <name>` | Create a new worktree + tmux window                                                                 |
| `list`          | List all managed worktrees                                                                          |
| `remove [name]` | Remove a worktree (interactive picker if name omitted)                                              |
| `repair <name>` | Recreate the tmux window for an existing worktree                                                   |
| `move <name>`   | Move a worktree between the shared and in-repo locations, retargeting its tmux window (alias: `mv`) |
| `prune`         | Remove **all** managed worktrees after confirmation                                                 |

**Flags for `create` and `repair`**:

- `--ai <alias>` / `-a <alias>` — AI coder to launch in the window. Accepted aliases: `opencode`, `oc`, `claude`, `cc`, `claudecode`.
- `--layout <name>` / `-l <name>` — Window layout to build. Valid names: `opencode`, `claude`, `claude-nvim`, `nvim`, `shell` (see "Window layouts" below). Mutually exclusive with `--ai` — passing both is a cobra error before either command runs.

  **Resolution order** (highest wins; each rule below only applies when none of the rules above it fired):

  1. `--layout` flag — explicit layout name, wins over everything.
  2. `--ai` flag — derived into a single-pane layout running that coder.
  3. `DEVGETA_WORKTREE_AI` env var — derived into a single-pane layout.
  4. `worktree.default_layout` in `global_config.yaml`.
  5. `worktree.default_ai` in `global_config.yaml` — derived into a single-pane layout.
  6. Default: `opencode`, single-pane.

  `repair` uses the exact same resolution order as `create` — it does **not** remember the layout a worktree was originally created with. If the window is missing, it is rebuilt from scratch using whatever `--layout`/`--ai`/env/config resolves to at that moment. If the window already exists (e.g. only one pane in it was closed), `repair` only relaunches the AI coder in the existing window — it does not add or recreate missing panes, since there's no way to tell whether the surviving panes already match the requested layout.

**Flags for `create` only**:

- `--prompt <text>` — Launch the layout's AI coder with `<text>` as its opening prompt, so the session is already working on the task when you attach instead of sitting at an empty prompt. No shorthand (`-p` would be ambiguous with `--pane`).

  The prompt is passed to the coder **as a launch argument**, using the coder's own binary rather than the `cc`/`oc` shell alias — `claude '<text>'` for Claude Code (positional), `opencode '--prompt' '<text>'` for OpenCode. Every argument, flags included, is uniformly single-quoted, so apostrophes and shell metacharacters in `<text>` survive intact. The whole command is exec'd as the pane's process at creation time rather than typed into it, so there is neither a wait-for-startup race in which the prompt can be dropped nor a length at which it can be silently truncated — the earlier `tmux send-keys` delivery relied on the pane's pty, whose input queue macOS/BSD caps at 1024 bytes. See [ADR-0011](decisions/ADR-0011-agent-prompt-as-launch-argument.md) (the prompt as a launch argument) and [ADR-0021](decisions/ADR-0021-pane-commands-are-exec-d-not-typed.md) (exec instead of typed, and why).

  It requires a layout that **has** an AI pane. `--prompt` with `--layout nvim` is an **error**, not a silent no-op, and fails before the worktree or window is created — silently dropping the prompt would leave a session that looks correctly created but was never given its task. A layout with two AI panes (none ships today) is rejected as ambiguous.

  `repair` deliberately does **not** accept `--prompt`: re-sending an opening prompt to a repaired window would start a _new_ conversation rather than restore the old one.

- `--pane <command>` — Add a shell pane beside the layout's panes, running `<command>` in the worktree directory. **Repeatable** — pass it once per pane. No shorthand.

  The value is a **shell command line, used exactly as written and not quoted**, so compound commands work: `--pane 'cd api && make dev'`. (This is the deliberate asymmetry with `--prompt`, which _is_ quoted because it is one literal argument to a coder — quoting a `--pane` value would break the compound commands that justify the flag.) The user is handing devgeta a command to run in their own shell, the same trust level as a shell alias.

  An empty or whitespace-only value is an **error**: `--pane "$VAR"` with an unset variable is a far likelier cause than a deliberate request for an idle shell, and a silent empty pane looks like the feature half-worked. There is deliberately no way to ask for a bare idle shell pane.

  Extra panes carry **no install check**, unlike a built-in layout's panes: the command may be a shell builtin, a compound, or a Makefile target, so probing its first token would reject legitimate commands. Any failure shows in the pane itself, and the worktree is still good. Each appended pane splits `vertical` (side by side); with two or more they get progressively narrower, so the clean 50/50 case is coder plus one shell.

  Example — a worktree already running its task and its bootstrap:

  ```bash
  dg wt create fix-1082 --ai claude --prompt 'fix issue 1082' --pane 'make finit'
  ```

  `repair` does not accept `--pane` either; it never rebuilds panes in an existing window.

**Window layouts**:

A layout is a named, ordered set of tmux panes built when a worktree's window is created or repaired. Built-in layouts (no config required):

| Layout        | Panes                                                      |
| ------------- | ---------------------------------------------------------- |
| `opencode`    | Single pane running OpenCode                               |
| `claude`      | Single pane running Claude Code                            |
| `claude-nvim` | Claude Code and Neovim side by side (vertical split)       |
| `nvim`        | Single pane running Neovim only                            |
| `shell`       | Single pane running nothing — just a shell in the worktree |

`shell` is the plain option: a window and a shell already sitting in the worktree directory, with no AI coder and no editor started. Nothing is typed into the pane at all (not even an empty line), it has no tool to check for, and it takes no `--prompt` — a prompt needs an AI pane to launch, so `--prompt` with `--layout shell` is the same error `--layout nvim` gives. Repairing a `shell` worktree whose window still exists is a no-op, since the window already has its shell.

Before any tmux window is touched, every pane's underlying tool is checked for installation; a layout referencing a missing tool fails with an actionable error and the worktree is not created. The check probes the tool's **binary** (`claude`, `opencode`, `nvim`) in your interactive shell, not devgeta's `cc`/`oc` alias, so a coder you installed yourself — outside devgeta, with no devgeta alias — now passes the check and launches fine; the flip side is that this check no longer tells you whether devgeta configured the tool, only whether it resolves. If a multi-pane window fails to build partway through (e.g. a later pane's split fails), the partially built window is killed and the worktree is rolled back — never left half-created.

**Worktree scan and layout config keys** (`global_config.yaml`, under `worktree:`):

Use [`dg config`](#dg-config) to list, read, set, or unset these — hand-editing
`global_config.yaml` directly still works, but is no longer necessary.

- `search_paths` — list of directories to scan for git repositories to offer in the `n`/`N` repo picker (see "Creating from the dashboard" below). Default: empty, which disables the scan entirely — this is the only off-switch. The scan walks each path with `filepath.WalkDir`, stops descending at a repo's `.git` boundary (so nested repos/submodules are not listed as separate entries), and skips `node_modules`, `.cache`, `vendor`, `target`, `dist`, and `.git` directories encountered during the walk (a configured root itself is still scanned even if its name matches one of these, e.g. a root literally named `vendor`).
- `scan_depth` — max directory depth below each search path to descend. Unset, `0`, or negative all mean the default of `4` — there is no separate "unlimited" or "disabled via depth" mode; use an empty `search_paths` to disable scanning.
- `default_layout` — default window layout name (see the resolution order above). Default: empty, which means rule 5 (`default_ai`-derived single-pane layout) or the built-in `opencode` fallback applies instead.
- `attach_after_create` — whether a successful `n`/`N` create in `dg ws` attaches into the new worktree's window (quitting the dashboard) or leaves you on the new row to keep working in the dashboard. Default: **absent, which means attach** — set it to `false` to stay put, which is the useful setting when you create several worktrees in one sitting, since attaching ejects you from the dashboard after the first one. Outside tmux there is no client to move, so the create already stays put regardless of this key. Unlike every other boolean in `global_config.yaml` (all feature flags where "off" is the right default), an absent key here must mean **on**, so the field is stored as a nullable boolean: absent is distinct from an explicit `false`, and configs written before this key existed keep attaching.
- `notify_sound` — whether an agent pane finishing (`idle`), blocking on a permission prompt (`blocked`), or erroring (`error`) plays a sound while its window is unattended, per [ADR-0009](decisions/ADR-0009-audible-agent-notifications.md). Default: **absent/`false`, which means off**. Unlike `attach_after_create` just above, where the zero value would silently flip existing behavior and so needed a nullable bool, off is already the correct default here, so a plain boolean is enough — the zero value already means what it should. The two hooks that actually play the sound (`configs/claude/agent-state.sh`, `configs/opencode/plugins/devgeta-notify/tui.js`) cannot parse `global_config.yaml`, so this YAML value is only the durable source: `configs/tmux/tmux.conf.tmpl` renders it into the deployed `~/.tmux.conf` as the tmux global option `@dg_notify_sound`, which is what the hooks actually query at runtime. Changing this key therefore has no effect on a running tmux server by itself — it takes effect after `dg configure tmux --force` plus a config reload (or a fresh server), or immediately if you also run `tmux set-option -g @dg_notify_sound on` by hand.
- `location` — where worktrees are created on disk, and where `remove`/`repair`/state checks look for an existing one. Default: `shared` (`~/.local/share/devgeta/worktrees/<repo-slug>/<name>`, today's behavior — no existing install changes). The other value, `in-repo`, creates worktrees at `<repo-root>/.claude/worktrees/<name>` instead, the same path Claude Code's own worktree feature uses. Changing this key only affects where _new_ worktrees are created — it does not relocate an existing one on its own. `remove`/`repair` always check git for a worktree's actual location first (both shapes), so they still find an existing worktree wherever it really lives even if this key changes underneath it; use `dg wt move` (below) to physically relocate one that already exists.

**Flags for `create`**:

- `--repo <path>` / `-r <path>` — Path to the repository (`~` is expanded), so the command works
  from any directory. The window opens in a tmux session named after the repo — created when
  missing, reused otherwise. Without the flag, the repo is the one containing the current
  directory and the window opens in the current session.
- `--base <ref>` — Root the new branch at `<ref>` instead of the freshly-fetched default branch.
  Only applies when `<name>` is a genuinely **new** branch: `create` normally adopts a local or
  remote branch of the same name into the worktree rather than failing, and `--base` for a branch
  that would be adopted is an **error**, not a silent no-op — omit `--base` to adopt it instead.
  `CreateWorktreeAtBaseIn` gives this the same pre-flight the no-`--base` path already has: a
  best-effort bounded fetch of `origin`, the same local/remote existence check that decides
  adopt-vs-new, and the base ref resolved to a real commit before any directory is created —
  routing `--base` around that pre-flight would reopen the staleness and divergence bugs the
  normal path avoids.

Either way, `dg wt create` moves you to the new window when run inside tmux, and says so if it
can't (the create still succeeded; it prints the window name to jump to by hand). `dg wt repair`
does the same for the window it rebuilt. **Building a window never moves anyone by itself** — both
tmux window creators pass `-d`, and going to the window is always a separate, explicit step. That
is what makes `attach_after_create: false` possible: before it, `new-window`'s default plus an
unconditional switch inside create had already relocated the client by the time `dg ws` read the
setting, so the dashboard appeared to eject you on every create no matter what you configured.

**Flag for `remove`**:

- `--force` / `-f` — Force removal even if the worktree has uncommitted changes.

**Flag for `move`**:

- `--to <shared|in-repo>` — Target location for this one move, regardless of the configured `worktree.location`. Without it, `move` targets whatever `worktree.location` currently resolves to — the common case right after changing that setting.
- `--force` / `-f` — Move even if the worktree has uncommitted changes.

If the worktree is already at the target location, `move` prints that and exits `0`
without touching git. It refuses on a dirty worktree unless `--force` is given. If the
worktree has a live tmux window, every pane is sent a `cd` to the new path, but only
when every pane in the window is an idle shell — if any pane is running something else
(an editor, a build, an AI agent), nothing is sent to any pane, a warning names the busy
pane, and the move itself still succeeds (a busy window is a follow-up inconvenience,
never a reason to fail the command). Moving `--to in-repo` warns, but does not refuse,
when `.claude/worktrees/` is not gitignored in the target repo — devgeta never edits
another repo's `.gitignore`.

**Adopting an existing branch (`create`)**: if a branch named `<name>` already exists locally,
`create` adopts it into the worktree instead of failing. If that branch is currently checked out
in the main clone, git refuses to check it out again in the new worktree — `create` frees it
first by switching the main clone to the repo's default branch (printing a one-line note so the
switch isn't a surprise), then proceeds. If the main clone's checkout of that branch has
uncommitted changes, `create` refuses up front with an error telling you to commit or stash them
first, rather than risk carrying or losing that work.

**Examples**:

```
dg wt create feature-login                  # Create worktree, use default AI/layout
dg wt create feature-login --ai claude      # Create with Claude Code
dg wt create feature-login --layout nvim    # Create with the nvim-only layout
dg wt new fix-auth --repo ~/code/api        # Create for another repo; window opens in its session
dg wt repair feature-login                  # Recreate missing tmux window (rebuilds current layout resolution, not the original)
dg wt prune                                 # Remove all worktrees (prompts for confirmation)
dg wt prune --stale                         # Clear git's leftover entries for deleted worktrees (no confirmation; removes nothing of yours)
```

**Creating from the dashboard (`n` / `N`)**:

- `n` opens a floating repo picker over the dashboard — the background stays visible, matching
  the `?` help overlay. Candidates are ranked: **the repo the cursor is on first**, then the repo
  containing the directory `dg ws` was launched from (when that directory is inside a git repo —
  otherwise this source is skipped), then repos from the recent-repos store (most-recently-used
  first), then repos found by scanning `worktree.search_paths` (see above — contributes nothing
  until configured), then `zoxide query -l` results when zoxide is installed. Typing filters the
  list; if the query matches nothing, Enter validates it directly as a free-typed repo path
  instead.
- The cursor row is what puts a repo at the top, so the common case is: move to the row you mean,
  press `n`, press Enter — no typing, no arrow keys. Every row kind answers: a worktree row and a
  repo header give their repo, a **session row** gives its session (resolved back to the repo
  whose own `TmuxSessionName` matches it, since that rewrite turns `.`, `:` and whitespace into
  `_` and cannot be reversed), and a **pane row** answers as the worktree or session row above it
  would. A row that matches no known repo simply contributes nothing, and the next source leads.
  Ranking the launch directory ahead of the cursor is what previously made this look broken:
  `dg ws` is normally started inside a repo, so that repo won every time and moving the cursor
  changed nothing.
- Enter on a repo opens a floating name prompt. For `n`, Enter on the name creates the worktree
  immediately using the resolved default layout (same as `create --repo` with no `--layout`/
  `--ai`) and attaches into the new window — the TUI exits, identical to pressing Enter on an
  existing row.
- `N` follows the same repo-pick → name-prompt flow as `n`, but after the name is entered it
  opens one more floating picker: a layout picker listing the built-in layout names
  (`opencode`, `claude`, `claude-nvim`, `nvim`, `shell`), cursor pre-positioned on the resolved default so
  accepting it is a single Enter. Picking a layout (or free-typing an unlisted name — `ResolveLayout`
  validates it and reports an unknown name the same way the CLI does) creates the worktree with
  that layout and attaches, same as `n`.
- If the create's pre-flight hook-compatibility check finds warnings, they're shown as a status
  message and a second Enter confirms; any other key cancels the confirm.
- A failed create (invalid path, duplicate name, unknown/uninstalled layout, etc.) is shown as a
  status message; the dashboard keeps running rather than exiting.
- Esc at the repo picker, the name prompt, or (for `N`) the layout picker returns to the
  dashboard unchanged; no worktree is created.
- Every successful create — from `ui`, `create`, or `new` — records the repo's root path in
  `global_config.yaml`'s `worktree.recent_repos` (MRU-ordered, capped at 20). This is what lets
  the picker offer a repo that currently has zero worktrees.

**In-progress feedback**: any dashboard action that builds or tears down tmux state shows a
status line at the bottom while it runs, so the dashboard is never silently unresponsive during
the (occasionally slow) git/tmux work: `creating worktree: <name> (<layout>)…` on create,
`repairing: <name> (<layout>)…` on `r` (and on attaching to a worktree whose window is missing,
which auto-repairs), and `deleting: <name>…` on the confirming `d`/`D` press. The layout is named
by its tools (`claude`, `opencode`, `neovim`, `claude + neovim`). Each message is replaced by the
result the moment the action finishes (and is moot inside tmux on a create/attach that succeeds,
since the TUI then attaches and exits).

**Planned commands**: See [ROADMAP.md](ROADMAP.md) for planned features and future commands.

#### `dg config`

Discover and change devgeta's user-settable configuration keys
(`~/.config/devgeta/global_config.yaml`) without hand-editing YAML — the supported way to change
the `worktree.*` settings documented above.

```
dg config [list] [--plain]
dg config get <key>
dg config set <key> <value...>
dg config unset <key>
```

**Subcommands**:

| Subcommand               | Description                                                                                                                                              |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| _(bare, same as `list`)_ | Print every setting: key, current value (or `(default)` if unset), live default, and description                                                         |
| `get <key>`              | Print only the effective value, nothing else (bare, for scripts)                                                                                         |
| `set <key> <value...>`   | Validate and persist a value; a `stringlist`-kind key (`worktree.search_paths`) accepts multiple values, every other key exactly one |
| `unset <key>`            | Clear a setting back to its default                                                                                                                      |

**Flags**:

- `--plain` — Suppress the interactive hint line that follows the table (persistent flag; works on both `dg config` and `dg config list`).

**Settable keys**: `worktree.default_ai`, `worktree.search_paths`, `worktree.scan_depth`,
`worktree.default_layout`, `worktree.attach_after_create`, `worktree.notify_sound`,
`worktree.location` — see "Worktree scan and layout config keys" above for
what each one does.
Setting `worktree.notify_sound` only persists the YAML value and the tmux-rendered default for
the _next_ server — see that key's entry above for why it does not also flip the option on an
already-running tmux server.

**Behavior**:

- `set` and `unset` both work on a fresh machine with no config file yet — the file is created
  on first use.
- An unrecognized key errors and lists every valid key.
- `set` validates through the same resolver the value would hit elsewhere (e.g. an unknown AI
  alias produces the identical error `dg wt create --ai <bad>` would); an invalid value writes
  nothing.
- `get` prints the value actually in effect, even for a setting that falls back through another
  — an unset `worktree.default_layout` prints whatever `worktree.default_ai` resolves to, never
  the literal word "unset".
- `set`/`unset` print a before/after confirmation (previous value or default, then the new
  state); `get` prints only the bare value.

**Examples**:

```
dg config                                           # List every setting
dg config get worktree.scan_depth                   # Print only the effective value, e.g. "4"
dg config set worktree.scan_depth 6                 # Validate and persist
dg config set worktree.search_paths ~/code ~/work   # stringlist keys accept several values
dg config unset worktree.attach_after_create        # Clear back to default (true)
```

#### `dg ws`

```
dg ws
dg workspace   # alias
```

Unified full-screen TUI dashboard — the single entry point to the worktree/session UI (the
old `dg wt ui` subcommand has been removed). Scoped to **workspaces** rather than worktrees
only. Every row in the dashboard is one of four kinds:

- **Repo header** (worktree-backed): a repo with git worktrees, sourced from the same
  worktree scan `dg wt list` uses. It is a **label**, not a switch target
  ([ADR-0052](decisions/ADR-0052-a-repo-header-is-a-label-and-its-sessions-are-rows.md),
  supersedes ADR-0048): bold name only, no status dot and no count badge while expanded.
  Expanded, it is never a cursor stop — `h`/`l`/`z` fold and expand its worktree rows, but
  `j`/`k` skip over it entirely. Collapsed, it shows `▸ name  N` (N = the number of worktrees
  hidden under it) and is a stop only so `l` or `enter` can re-expand it; neither switches
  anywhere.
- **Window row**: one row per **plain (non-worktree) tmux window** in a session that holds
  this repo's worktree windows, listed under the repo header before its worktree rows and
  named by the window (`node`, `zsh`). The session is not shown: the header already says
  which repo this is, and a repo's windows can live in more than one session (`hire2` and
  `hire2-tien` is a real case) without the rows caring. Read straight off the scan's own
  panes — never derived from a name, which ADR-0048 already showed can be wrong. A window is
  identified by tmux's window id, not its name, so two windows both called `zsh` stay two
  rows. A session holding **only** worktree windows gets no window rows: each of those
  windows already has its own worktree row. Windows count, not panes — a window split into
  several panes is one row, and a worktree window split into several panes is still just that
  worktree — and the dashboard's own `[workspace]` window (from `ctrl+t`) is never listed.
- **Worktree row**: one git worktree, a leaf under its repo header.
- **Standalone session row**: a tmux session with no window backed by a **live** worktree,
  sourced from `tmux list-sessions`, listed under a dim `sessions` section header after every
  repo group. Liveness is what decides, not the `wt-` name: a worktree removed outside
  `dg wt remove` leaves its window behind with the name intact, and that window no longer
  hides its session (see `LiveWorktreeWindows`).

A standalone session row keys off `sess:<name>` (a session name is unique on the tmux
server) and a window row off `win:<window id>`, for folding, pane-row expansion, and cursor
restore. Both render the same way — the name, no marker, no trailing label.

**Space rows carry no status marker at all** ([ADR-0056](decisions/ADR-0056-the-dashboard-lists-agents-in-their-own-section.md),
supersedes [ADR-0008](decisions/ADR-0008-agent-state-on-every-pane-row.md)'s per-row glyph):
no agent-state color and no "has a window" dot on a worktree, session, window, or pane
row. A worktree, session or window row with **two or more** panes reporting a non-empty
agent state still gains its own `▼`/`▶` chevron, and `h`/`l` still reveal or hide its **pane
rows** — one child row per pane, indented further than the parent, showing the pane's index
and the command currently running in it, just with no dot of its own either. This answers
"which pane is which" for `enter`'s sake; "which pane wants attention" now lives entirely in
the **agents section** below, not on these rows. A repo with no open window starts folded the
first time this launch sees it (default, not reapplied on every refresh — see "View state is
saved" below).

The repo header itself carries no glyph either (ADR-0052 — unchanged by ADR-0056): the header
is a label, not something that competes with its own rows for attention.

### The agents section

Below spaces, a second, independent section lists **every pane running an AI coder**
([ADR-0055](decisions/ADR-0055-an-agent-says-what-it-is.md),
[ADR-0056](decisions/ADR-0056-the-dashboard-lists-agents-in-their-own-section.md)) as one flat
list across every repo and session — "who needs me" answered by the top of one list, instead
of scanning dot colors across the whole tree. A pane counts as an agent when the coder itself
has written `@dg_agent_kind` (`claude`/`opencode`) to it — on session start and every state
write — **not** derived from its process name, which is unreliable (Claude Code can show up as
its own version string) and can't say which coder it is. A coder killed before it can clean up
still drops off the list once its pane reverts to a plain shell (the "is a plain shell"
backstop; the kind option itself may linger harmlessly). That check only applies to a pane
that falls back to a shell when its program exits; a coder wrapped in `zsh -c '<coder>'`,
as worktree layouts start them, reports `zsh` while it runs and is still listed.

Each agent is two lines:

```
! devgeta/fix-stale-notify      :1
  blocked · claude
```

The first line is the state glyph, where the pane lives (`repo/worktree` for a pane in a
worktree window, otherwise the session name), and `:N` (the pane index, for when one window
runs two coders side by side). The second line is the state word and the coder's own name. The
selection bar runs down both lines as one stripe. Sorted by urgency, most urgent first —
`blocked > error > done > working > idle`, ADR-0005's aggregation order with `idle` split into
**done** and **idle** — ties broken by location, then by pane index. State words:

| Glyph | Word      | Means                                          |
| ----- | --------- | ---------------------------------------------- |
| `!`   | `blocked` | Waiting on your answer (permission, question)  |
| `✕`   | `error`   | Turn ended with an error                       |
| `◆`   | `done`    | Finished, and you haven't looked yet           |
| `●`   | `working` | Agent is running a turn                        |
| `○`   | `idle`    | Finished and seen, or hasn't been prompted yet |

`enter` on an agent row switches the tmux client straight to that exact pane — the same
pane-row switch worktree/session pane rows already use, and the same per-pane acknowledgement
(reaching a pane any way — these keys, the mouse, `switch-client`, or the dashboard — clears
its `idle`/`blocked`/`error` state, never `busy`; see the shipped tmux config's
`pane-focus-in`/`pane-focus-out` hooks). The right pane shows what that pane's own worktree row
would show: the branch diff for a worktree-backed agent, nothing for a repo-window or
standalone-session one.

`d d` on an agent row closes **that agent's pane only** (same two-press confirm as every other
`d`, and any other key cancels). A split's other panes, such as the Neovim beside a coder,
stay open; tmux closes the window with its last pane and the session with its last window.
It never deletes a worktree or kills a session by itself; use `d` on the space row for that.

**Folding.** `a` folds or unfolds agents, `w` folds or unfolds spaces (`s` still means "new
session"), from anywhere, in any order. A folded section collapses to a one-line bar on the
**last line of the column**, with its unfold key on the right: the agents bar keeps a per-state
count for every state with at least one agent (`!1  ◆1  ●1`), so a new blocked agent stays
visible while the section is closed; the spaces bar shows how many spaces, repos and sessions
there are. The **last open section can't fold** — its key does nothing — and folding the
section the cursor is in moves the cursor to the other one.

**Headers.** Each open section has a header line, `SPACES` or `AGENTS`, with its count on the
right (`1 of 4` while a filter narrows it), yellow for the section the cursor is in. A rule
separates the two sections. With no agent panes at all, there is nothing to separate: the
column is the plain spaces list, with no headers.

**Split.** With both sections open, a section that needs less than its share gives the rest to
the other, so agents sit right under the spaces rows and the split only matters once both
outgrow the column. Then agents get half the column by default, adjustable with `+`/`-`, or by
dragging the `AGENTS` header line with the mouse (the same drag-then-commit-on-release pattern
as the left/right divider). Each section scrolls on its own once its content outgrows its share. The mouse
wheel scrolls whatever is under the pointer: over a section it moves that section's cursor
(focusing it, and stopping at its ends), over the diff it scrolls the diff three lines a notch.

**Filter scope.** `/` searches only the sections that are currently open. In agents, it matches
the location, the coder's name, and the state word, so `/blocked` and `/opencode` both narrow
the list; a folded section is skipped entirely and its bar keeps its normal, unfiltered count. The
text each row matched on is highlighted (reverse video) in both sections.

`j`/`k` walk across the boundary between spaces and agents, clamping at the true top (spaces)
and bottom (agents) of the combined list rather than wrapping — a folded section counts as
absent, so moving toward one just clamps at the edge instead.

### Moving with your own tmux keys

The dashboard reads its pane-move keys from tmux itself rather than hard-coding a copy
([ADR-0057](decisions/ADR-0057-the-dashboard-takes-its-pane-move-keys-from-tmux.md)): at
startup it asks tmux which keys run `select-pane -L/-D/-U/-R` (`tmux list-keys -T root`),
directly or as the fallback branch of an `if-shell` (the shipped `is_vim`-style bindings), and
uses those — falling back to `ctrl+h/j/k/l` when there's no tmux, no such binding, or nothing
parses. Change the binding in tmux and the dashboard follows the next time it opens.

| Direction | From the list (spaces or agents)      | From the diff pane |
| --------- | ------------------------------------- | ------------------ |
| down      | spaces → agents; from agents: edge    | edge               |
| up        | agents → spaces; from spaces: edge    | edge               |
| right     | → diff pane (what `space` does today) | edge               |
| left      | edge                                  | → list             |

At an edge, the dashboard hands the move back to tmux with `select-pane` in that direction —
the same way vim-tmux-navigator does from Neovim — so the same keys keep working seamlessly
when the dashboard runs alongside another pane. While a text input has focus (the `/` filter, a
rename, the create flow's prompts), these keys are not moves; they go to the input exactly as
today. The shipped tmux config passes them through to the dashboard the same way it already
does for Vim, recognizing it by process (a `ps` match for `devgeta ws` on the pane's tty).

Attaching to a row, or switching to a pane (`enter`), clears its state — attaching is the user acknowledging it.
Reaching the pane any other way counts too: the shipped tmux config clears a pane's `idle` /
`blocked` / `error` state whenever the pane gains or loses focus, whether that came from tmux
keys, the mouse, or `switch-client`. `busy` is never cleared this way. Without the shipped tmux
config, only the dashboard's own `enter` clears state.
tmux's own status bar (`configs/tmux/tmux.conf.tmpl`) separately flags any other window in the
current session whose coder wants attention while you're looking elsewhere, so `dg ws`
doesn't have to stay open to notice. That status bar only paints windows of the **attached**
session, though, and gives no signal at all when the terminal itself isn't the focused window —
an agent blocked in another session, or in any window while you're looking at a different
application, has nothing visual reaching you.
[ADR-0009](decisions/ADR-0009-audible-agent-notifications.md) adds an audible signal for
exactly that gap: the same hooks that write `@dg_agent_state` also play a sound for the
same three "wants you" states (`idle`, `blocked`, `error`) at the moment they write them —
never for `busy`, since an agent starting work isn't an event you asked to hear — gated on
`window_active_clients == 0`, the identical predicate the status-bar flag above already uses,
so the audible and visual signals can never disagree about whether you've seen something. It
is off by default and opt-in (see `notify_sound` above), each state has a distinct sound so
the three are told apart without looking, and a missing sound player or audio device is
silence rather than an error or a blocked hook.

Every row kind shares the base keys (`j`/`k` nav — crossing between spaces and agents —, `h`/`l`
fold a repo/pane-group, `z` toggle-all-repos, `a`/`w` fold agents/spaces, `+`/`-` adjust the
split, `n`/`N` create a worktree, `/` filter, `?` help, `q` quit, and the tmux-read pane-move
keys above), plus `e` (toggle the left pane between its default width, 40 columns, and double
that, both clamped to 60% of the terminal) and `ctrl+r` (recompute the branch diff for the
currently selected row, in both the list and the diff-focused view — diff-only, it
deliberately does not re-read git worktree state,
[ADR-0024](decisions/ADR-0024-the-dashboard-refreshes-fast-and-slow-state-separately.md)).

**Deleting a worktree never loses work unless forced**
([ADR-0053](decisions/ADR-0053-removing-a-worktree-never-loses-work-unless-forced.md)). On a
worktree row, `d` `d` (delete) and `D` `D` (delete and kill its session) refuse when the
worktree has uncommitted changes (untracked files included) or commits that no remote branch
and no local default branch contain, and when either can't be checked. The status line says
what is at risk — `feat not deleted: it has uncommitted changes and 2 unpushed commits · F F
deletes it anyway` — and `F` `F` is the one forced delete, with an armed hint that names what
will be lost. A delete removes the branch git reports for the worktree, never one derived from
the row's flattened name.

**The dashboard opens on its last worktree list.** Each successful git load and diffstat
sweep is saved to `$XDG_CACHE_HOME/devgeta/ws-snapshot.json`, and the next open draws those
rows immediately instead of `(loading...)` while the normal loads run and replace them
([ADR-0054](decisions/ADR-0054-the-dashboard-opens-on-its-last-worktree-list.md)). Only git
fields (repo, name, path, branch) and diffstats are saved; tmux state always comes from the
live scan: one scan runs before the first frame, so the cursor already opens on the row for
the session you're in instead of jumping there. A worktree removed outside the dashboard shows
until the first git load lands. The "no worktrees yet" message still waits for that real load.
Deleting the file only costs one slow first frame.

**View state is saved** across the dashboard closing and reopening — which repo/pane folds are
collapsed, the left-pane width, and (since ADR-0056) which repos were explicitly unfolded out
of their windowless default, whether each section is folded, and the agents split — in one
tmux server-global option, `@dg_ws_state`
([ADR-0050](decisions/ADR-0050-dashboard-view-state-lives-in-a-tmux-server-option.md); its
2026-09-28 revision adds the four new fields, all optional, so the value's version stays `1` —
an older value reads as "both sections open, default split, no windowless repo explicitly
unfolded"). It lasts as long as the tmux server (gone after `tmux kill-server` or a reboot,
same as the sessions it describes) and is written only when one of those actually changes (a
fold, `e`, a drag ending, or `+`/`-`), never on every tick or mouse-motion event. The cursor
itself is still **not** saved: the dashboard always opens on the row for the tmux session
you're actually in (or, when that session holds only worktree windows and so has no row, on
its first worktree row) — or, if spaces loads folded, on the agent in the window you came from
(the first agent when that window runs none) — which is the
more useful default when `ctrl+t` is pressed from inside a session, and a saved cursor would
compete with that.

Every worktree row carries a diffstat, `+A −R` (green and red, like the diff header in the right pane), pinned to its right edge
([ADR-0051](decisions/ADR-0051-worktree-rows-carry-a-diffstat-from-the-slow-refresh.md)) —
nothing is drawn when there are no changes. It is computed for every worktree on the **slow**
refresh only, with the repo's default branch resolved once and reused across that repo's
worktrees, and the selected row's own number stays current between slow refreshes for free
(the full branch-diff computation it already does when selected produces the same counts).

Window rows add:

- `enter` — switch the attached tmux client to that exact window, by its window id (never by
  name, which two windows can share, and never to the bare session, which would land on the
  dashboard's own `[workspace]` window). Guarded like attaching to a worktree: only works
  inside tmux.
- `d` `d` — close **that window only** (two-press confirm, any other key cancels), by its
  window id, so a same-named window is never hit. Its session and the other windows in it stay.
- `h` folds the repo, as on a worktree row. `$` does nothing here: it renames a whole session,
  and a window row is one window of it. Renaming a single window is not built yet; tmux's own
  key works.

Standalone session rows add:

- `enter` — switch the attached tmux client to the session. Guarded: only works inside tmux,
  same guard message as attaching to a worktree.
- `d` `d` — kill the session (two-press confirm, same "press again" hint style as worktree
  delete).
- `$` — rename the session. The prompt is prefilled with the current name; the typed name is
  flattened the way `TmuxSessionName` does (`.`, `:`, and whitespace become `_`). A duplicate is
  checked against the live session list first, only for a clearer message — tmux's own
  `rename-session` already rejects a duplicate (`duplicate session: …`), and that is what shows
  if the check is raced. The renamed row keeps its fold and the cursor: both move from
  `sess:<old>` to `sess:<new>`.
- `s` (works from any row, not just a session row) — opens a two-step create flow:
  1. **Pick a folder** — a fuzzy picker with `root` (the user's home `~`) pinned at the top,
     then the same ranked repo candidates the worktree flow offers, and — like that flow — a
     free-typed path is also accepted. A session isn't tied to a repo, so the chosen folder is
     validated as an existing directory only (not a git repo).
  2. **Name prompt** — on Enter with a name, creates the session in the chosen folder. Enter with
     a **blank** name auto-generates a `devgeta-<character>` name (Dragon Ball characters, e.g.
     `devgeta-goku`), checked against the live tmux sessions so a blank-name create never collides
     with an existing one.

  Inside tmux, the client switches to the new session and the dashboard quits; outside tmux, the
  session is created detached and reported (`session created: <name>`) without switching. A
  duplicate typed name surfaces tmux's own "duplicate session" error on the status line — there's
  no separate pre-check. New worktree windows for a repo go into the session that already holds
  that repo's other windows (read from one `tmux list-panes -a` at create time), not the derived
  `TmuxSessionName(<repo slug>)` — otherwise a rename would make the next worktree create start
  a second, differently-named session for the same repo.

- `D`/`r` are worktree-only actions and are no-ops on a session row.
- The diff pane draws nothing, same as a repo header or pane row: a session has no single
  branch to diff.

The repo header adds nothing (ADR-0052): it is a label, and `enter`/`l` on a **collapsed**
header expand it — the same action either key already performs, since that is the only thing
left for the header to do. An **expanded** header is never a `j`/`k` stop; only a collapsed
one is, so `l` can re-expand it. The diff pane draws nothing on a header row either way.

Pane rows add:

- `enter` — switch the tmux client to that exact pane (same tmux guard and dashboard-quit
  behavior as attaching to a session or worktree). Previously a no-op.
- The diff pane: a pane belongs to a worktree, so moving the cursor onto one of a worktree's
  pane rows keeps showing that worktree's branch diff, exactly as if the cursor were still on
  the worktree row itself — drilling into a pane (`l`) never blanks or delays the diff. A pane
  row under a session or window draws nothing, same as its parent row itself.

Bare `ctrl+t` (no tmux prefix) opens `dg ws` (see `configs/tmux/tmux.conf.tmpl`) — it previously
opened tmux's native `choose-tree -Zs` popup, which this replaces. This is the only key bound
to the dashboard.

#### `dg list`

`dg list` is the single inventory command (there is no separate `dg validate`; drift
checking lives inside the dashboard's problems-only view):

- **Data model** — every item Devgeta has tracked in `~/.config/devgeta/global_config.yaml`
  (both what it installed and what it found pre-existing) is live-checked against the system,
  producing a three-state status per item:
  - `OK` — the presence check ran and found the item.
  - `MISSING` — the check ran and definitively did not find the item.
  - `UNKNOWN` — the check itself failed to run (e.g. `brew`/`dpkg` unavailable). A failed
    check is never conflated with a missing item, so a flaky or unavailable package manager
    can't misreport drift.
  - The `themes` and `terminal_tools` categories are tracked but have no live presence check
    implemented yet (no current code path populates them); if a future feature starts
    tracking items there, they report `UNKNOWN` rather than being silently misreported as
    `OK` or `MISSING`.
- **Dashboard** — renders the collected items as an interactive, grouped list. Opens
  automatically in a terminal; falls back to plain-text output for piped, CI, or `--plain`
  invocations. Keybindings: `j`/`k` move, `h`/`l` collapse/expand a group, `/` enter a text
  filter, `p` toggle problems-only (`MISSING`/`UNKNOWN` items), `g` toggle between grouping
  by category and grouping by status, `?` open the keybinding help overlay, `q` quit.
  While problems-only is active the pane title shows the mode and the `p` hint flips to
  "show all"; if nothing is missing, the pane says so explicitly instead of rendering an
  empty list.

```
dg list [--category <name>] [--plain]
dg installed [--category <name>] [--plain]   # alias
```

**Flags**:

- `--category <name>` — Filter to a single bucket. Valid values: `packages`, `desktop_apps`,
  `fonts`, `themes`, `terminal_tools`, `dev_languages`, `databases`.
- `--plain` — Force plain-text output even when run in a terminal.

**Behavior**:

- In a terminal, opens the dashboard unfiltered (every tracked item, all categories).
- Piped output, CI, or `--plain`: prints one table per non-empty category (name only, no
  live status check); empty categories are omitted. The "Already on this machine (not
  installed by Devgeta)" section only prints if it has entries. An empty config prints a
  clear message instead of a blank screen. An unrecognized `--category` value prints an
  error listing the valid category names.
- This is still the MVP: name + category only in plain mode. Per-item version and
  install-timestamp tracking requires a `global_config.yaml` schema change and is planned as
  a future release (see [ROADMAP.md](ROADMAP.md)).

**Examples**:

```
dg list                             # Interactive dashboard in a terminal
dg list --category=terminal_tools   # Only the terminal tools bucket
dg installed                        # Same as 'dg list'
```

#### `dg task`

Developer utility commands for git branch management, npm dependency management,
releasing, and scratch directories. Run them directly or through the `dge` shell
wrapper.

```
dg task <subcommand> [args]
dg t <subcommand> [args]   # alias
```

| Subcommand            | Args                   | Description                                                                                                                                                                                                                       |
| --------------------- | ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `refresh-branch`      | `[target]`, `--rebase` | Checkout target (default: `main`), pull, return to previous branch, merge target in — or, with `--rebase`, rebase onto it instead (merge stays the default; a repository whose merge gate rejects merge commits wants `--rebase`) |
| `reset-main-branch`   | —                      | Checkout `main`, hard-reset to `origin/main`                                                                                                                                                                                      |
| `delete-branch`       | `[target]`             | Checkout target (default: `main`), fetch, pick a branch via fzf to force-delete                                                                                                                                                   |
| `reinstall-libraries` | —                      | `git clean -Xdf`, remove `node_modules/`, `npm install`, remove `tsconfig.tsbuildinfo`                                                                                                                                            |
| `reinstall-library`   | `<name>`               | Remove `node_modules/<name>`, run `npm install`                                                                                                                                                                                   |

**Release management subcommand** (automates the CLAUDE.md §9 push-and-tag flow):

| Subcommand | Args / Flags                                | Description                                                                   |
| ---------- | ------------------------------------------- | ----------------------------------------------------------------------------- |
| `release`  | `<version>`, `--message-file <f>`, `--push` | Squash 2+ unpushed commits into one, tag, and (with `--push`) push commit+tag |

`release` runs five guards, in order, before any mutation — each refuses with an
actionable one-liner and nothing is changed if any of them fails:

1. `<version>` matches `vMAJOR.MINOR.PATCH` exactly (strict semver, no prerelease
   or build-metadata suffixes — CLAUDE.md §9's tag policy, machine-enforced).
2. The working tree is clean (`git status --porcelain` empty).
3. HEAD is on the repository's default branch.
4. `--message-file` exists and is non-empty.
5. `<version>` is not already an existing tag.

Once all guards pass: count commits ahead of `origin/<default>`
(`git rev-list --count`); if 2 or more, `git reset --soft HEAD~N` followed by
`git commit -F <message-file>`; then `git tag -a <version> -F <message-file>`.
Only when `--push` is passed: `git push origin <default> refs/tags/<version>`, so only
the release tag is published, never other local tags.

Without `--push`, nothing is pushed — the final line states exactly what remains,
e.g. `Tagged v0.12.0 (squashed 3 commits). Not pushed — run: git push origin main refs/tags/v0.12.0`.
A failure partway through a mutation (reset, commit, tag, or push) reports the exact
state left behind and the raw git command to finish or undo it by hand, since these
steps are hard to reverse once they run.

**Scratch directory subcommand** (a devgeta-owned working directory instead of
`/tmp` — see [ADR-0015](decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md)):

| Subcommand | Args / Flags                     | Description                                                                                                                                                                                                     |
| ---------- | -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `scratch`  | `--key <name>`, `--clean <path>` | Bare call allocates a fresh, unique directory under the scratch root and prints its path; `--key` allocates or re-derives a shared, keyed one instead; `--clean` removes one previously allocated (either form) |

Bare `dg task scratch` calls `paths.EnsureScratchDir()` — creating
`~/.cache/devgeta/scratch` (honoring `XDG_CACHE_HOME`) at mode `0700` if
absent, rather than assuming a prior `dg configure` did — then
`os.MkdirTemp`s a `task-*`-prefixed subdirectory under it and prints its
absolute path on one line.

`--key <name>` (ADR-0033) trades the unique path's isolation for a
**re-derivable** one: the same key always maps to the same
`key-<name>`-prefixed path, so a later, independent session that knows the
key can find a file an earlier one left there — a hand-off, not a private
working directory. The directory is **shared**, not private: two sessions
that pick the same key by accident share it, and whichever writes last wins.
It also does not age out on its own: an unkeyed allocation is pruned after
24h by `dg configure --force`'s maintenance pass, but a keyed one persists
until explicitly `--clean`ed, because durability across sessions that may not
run for days is the entire point. Reusing an existing keyed path is refused
outright — never silently written through — when it is a symlink or a
non-directory, or when a symlinked ancestor would move it outside the
scratch root; only a `Lstat`-verified real directory, contained under the
root, is reused. The key must survive as a single path element: empty or
whitespace-only, `.`, `..`, a path separator, or a null byte are all refused
before anything is touched.

`--clean <path>` accepts only a real directory that is a direct child of the
scratch root and carries the `task-` **or** `key-` prefix: the root itself, a
grandchild, a directory beside the root, an unprefixed child, a relative
path, a `..` escape, and **any** symlink (resolvable or not) are all refused
rather than silently widening what gets deleted. Bounds are checked lexically
first, so an out-of-bounds path is an error whether or not it exists, then
re-checked after symlink resolution to catch a symlinked ancestor. It is
idempotent — an in-bounds path that is already gone succeeds — so a command's
cleanup can run on failure paths and retries. Pass it the exact path
`scratch` printed; a reconstructed or parent path is refused by design. Not
enforced: a session cleaning a sibling session's directory by guessing its
random suffix — the only parties able to do that are the same user's own
agent sessions, so ownership isolation stops there deliberately.

The same ownership rule bounds the stale-directory prune that
`dg configure --force` runs, but only for the `task-` prefix: anything a user
keeps under the granted scratch root is left alone regardless of age, and a
`key-` prefixed directory is skipped entirely (never pruned by age at all,
per ADR-0033) rather than reaped out from under a hand-off.

It gives an agent a place for working files outside the project instead of
`/tmp` — see
[docs/apps/claude.md](apps/claude.md#permissions-model) for why `/tmp` would
otherwise prompt on every run.

`dge` (the shell function in `devgeta.zsh`) is now a thin wrapper that forwards to `dg task`:

```sh
dge() {
  if [[ $# -eq 0 ]]; then dg task --help; return; fi
  dg task "$@"
}
```

Use either `dg task` or `dge`.

#### `dg archive`

Packs a folder onto an external drive for a machine move — a one-shot archive,
not an incremental backup (`dg backup` stays reserved for devgeta config
snapshots; restic/borg do incremental backups well). Written entirely in Go
(`archive/tar` + `klauspost/compress/zstd`), not by shelling out to `tar` — see
[ADR-0040](decisions/ADR-0040-an-archive-is-written-in-go-not-by-shelling-out-to-tar.md).

```
dg archive <source> <destination-dir> [--dry-run] [--gzip] [--no-skip] [--no-verify] [--mac-metadata] [--yes]
dg archive verify <archive-file>
```

**Flags**:

- `--dry-run` — Scan and print the report (kept size/count, each skip with its
  rule and size, unreadable/special/iCloud-only files, Windows-incompatible
  names) without writing anything.
- `--gzip` — Write `.tar.gz` instead of the default `.tar.zst`. zstd is faster
  and passes incompressible data (photos, video) through cheaply; gzip exists
  for zero-install restores on Windows' command-line `tar`, which may lack zstd
  support.
- `--no-skip` — Disable every skip rule; archive everything.
- `--no-verify` — Skip the post-write verification pass (on by default).
- `--mac-metadata` — Include each file's extended attributes as
  `SCHILY.xattr.<name>` pax records (read by GNU tar and libarchive/bsdtar),
  instead of leaving them out. macOS only — errors immediately on Linux.
- `--yes` — Skip the confirmation prompt.

**Behavior**:

- Skips only what [ADR-0041](decisions/ADR-0041-an-archive-skips-only-what-is-provably-regenerable.md)
  proves is regenerable: a `CACHEDIR.TAG` with the exact standard signature, a
  `pyvenv.cfg` virtualenv, or a named folder (`node_modules`, `target`,
  `vendor`, `Pods`, `.gradle`, `.dart_tool`, `_build`/`deps`, `.zig-cache`,
  `.tox`, `.terraform`, …) next to the manifest file that proves a tool
  generates it — never by name alone. `.git`, `build`, `dist`, `out`,
  `coverage`, and `bin` are never skipped.
- Refuses before writing anything (nothing created) when: the source isn't a
  directory or the destination isn't an existing directory; the destination is
  nested inside the source; **any** of the four files a run writes already
  exists — the archive, the manifest, the skip report, or the archive checksum
  (a stale `.partial` from a crashed run is overwritten instead); the
  destination filesystem is FAT (4 GiB file-size limit — reformat as exFAT); or
  the scan finds unreadable files or iCloud-only placeholders (fix permissions,
  or download them in Finder, then re-run).
- Never deletes or modifies anything else on the destination. The source is
  only ever opened for reading. The sole removal a run performs is of the
  `.partial` files it created itself, and only when it fails. Why the
  overwrite check refuses but the free-space check only warns:
  [ADR-0042](decisions/ADR-0042-an-archive-refuses-on-proof-and-warns-on-prediction.md).
- Warns and continues, archiving unchanged, for: names that would fail to
  extract on Windows (renaming would alter the user's files); special files
  (sockets, FIFOs, devices — skipped and listed); files whose size or mtime
  changed between the scan and the write, or that vanished — the manifest
  hashes only what was actually archived; and a destination with less free
  space than the source's uncompressed size (a warning, not a refusal —
  compression usually covers the gap, and running out mid-write discards the
  run without touching anything already on the drive).
- Writes three files to `<destination-dir>`: `<source-basename>-<YYYY-MM-DD>.tar.zst`
  (or `.tar.gz`), a `sha256sum`-format checksum manifest
  (`<name>.sha256`, GNU-escaped for any path with a backslash or newline —
  checkable with `shasum -a 256 -c` without devgeta), and a skip report
  (`<name>.skipped.txt`). All three are written to `.partial` files, fsynced,
  and renamed into place together; any failure mid-write deletes every
  `.partial` file and leaves no incomplete final-named file.
- Verifies by default: re-reads the archive from the destination, hashes every
  regular-file entry against the manifest (same set of paths, same hashes),
  and writes `<name>.tar.zst.sha256` holding the hash of the archive file
  itself.
- Confirms interactively (unless `--yes`) after printing the scan summary;
  refuses outright without `--yes` when stdin/stdout isn't a TTY.
- Reports progress for both long phases — the write, measured against the
  scanned byte total, and the verify, measured against the archive's size on
  disk. Each shows a bar, percent, bytes done over total, the current rate,
  and the estimated time left, finishing with a one-line summary. Progress
  draws to **stderr**, so a redirected stdout stays clean: an attached terminal
  gets one line redrawn four times a second; a pipe or log gets a plain line
  every 30 seconds. Measuring adds no extra read of the data — the counter is
  one atomic add per buffer on a stream both phases already read.

**Examples**:

```
dg archive ~/Documents /Volumes/SSD
dg archive ~/Documents /Volumes/SSD --dry-run
dg archive ~/Documents /Volumes/SSD --gzip --yes
dg archive verify /Volumes/SSD/Documents-2026-09-13.tar.zst
```

**Compress**:

```bash
dg archive ~/Documents /Volumes/SSD                 # .tar.zst, verified after writing
dg archive ~/Documents /Volumes/SSD --dry-run       # scan and report, write nothing
dg archive ~/Documents /Volumes/SSD --gzip          # .tar.gz for a zero-install Windows restore
dg archive ~/Documents /Volumes/SSD --yes           # skip the confirmation prompt
dg archive verify /Volumes/SSD/Documents-2026-09-13.tar.zst   # re-check an existing archive
```

**Decompress** — no devgeta involved, a plain `tar` on macOS, Linux, or
Windows. macOS ships bsdtar/libarchive and Debian/Ubuntu ship GNU tar; both
read `--zstd` natively, so nothing needs installing:

```bash
# Extract into a directory of its own — the form dg archive prints, and the
# one to prefer. A bare `tar -xf` unpacks into the CURRENT directory, which
# for a whole home folder means hundreds of entries landing on top of
# whatever you were standing in. `-C` also fails if the directory does not
# already exist, hence the mkdir.
mkdir -p Documents-2026-09-13 && tar --zstd -xf Documents-2026-09-13.tar.zst -C Documents-2026-09-13

tar -xzf Documents-2026-09-13.tar.gz -C somewhere   # a --gzip archive
zstd -d Documents-2026-09-13.tar.zst -c | tar -x -C somewhere  # if tar lacks --zstd
```

**Inspect and check** — also without devgeta. The two `.sha256` files are
plain `sha256sum` format, so `shasum -a 256 -c` reads them directly:

```bash
tar --zstd -tf Documents-2026-09-13.tar.zst                # list entries without extracting
tar --zstd -xf Documents-2026-09-13.tar.zst some/one/file  # pull out a single path
shasum -a 256 -c Documents-2026-09-13.tar.zst.sha256       # the archive file itself is intact
cd ~/restored && shasum -a 256 -c /path/to/Documents-2026-09-13.sha256  # every extracted file is intact
```

The last one is the strongest check available without devgeta: it hashes every
extracted file against the manifest written at archive time. `dg archive verify`
does the same plus confirms the archive and manifest list the same set of paths.

On **Windows**, File Explorer opens `.tar.zst` since Windows 11 23H2. The
bundled `tar.exe` may not, which is what `--gzip` is for — `tar -xzf` works
there with nothing installed.

**Out of scope**: extraction/restore (plain `tar -xf` is the restore path),
incremental or deduplicated backups, encryption (use an encrypted volume, or
`age`/`gpg` the file afterward), splitting output into chunks, user-supplied
`--exclude` patterns, and downloading iCloud placeholder files for the user.
See the [2026-09-13-dg-archive cycle doc](plans/cycles/2026-09-13-dg-archive.md)
for the full design and research behind the format choices.

#### `dg export` / `dg import`

Moves the state a user accumulated _inside_ an app — bookmarks, open tabs,
settings — from one machine to another. `dg install` installs the app
and `dg configure` applies devgeta's own config to it; neither carries this, and
before these commands it was hand-copied from memory on every hardware change.

This is **not** `dg archive` pointed at an app's folder, and the difference is
the whole design. `dg archive` skips only what is proven regenerable and keeps
everything else ([ADR-0041](decisions/ADR-0041-an-archive-skips-only-what-is-provably-regenerable.md)),
which is right for a documents folder where a wrongly skipped file is
unrecoverable. An app's data directory inverts that: a measured Brave profile is
3.8G of which about 22M is worth moving, and a faithful copy also reinstates
files that are meaningless or harmful on the destination — `Secure Preferences`
carries HMACs bound to the machine that wrote it, `Login Data` and `Cookies` are
encrypted against the local keychain, `Local State` holds that machine's
encryption key. So these commands are an **allowlist**: nothing moves unless an
app's adapter names it.
[ADR-0045](decisions/ADR-0045-portable-app-state-is-an-allowlist-not-a-smaller-archive.md)
records the decision; `dg archive` is unchanged and the two commands are allowed
to disagree.

```
dg export <app> [destination-dir] [--dry-run] [--group a,b] [--profile "Default,Profile 1"]
dg import <app> <bundle-file> [--force] [--group a,b] [--profile "Default,Profile 1"]
```

**The unit is a named state group**, not a path list: a group is a name, its
paths, whether it is on by default, and a one-line reason. `dg export brave
--dry-run` prints every group with its paths, its size and whether it is on — the
groups that are OFF included, since "what did not move" is the half of the answer
no other output gives. A dry run needs no destination directory.

**What can never be in a group.** Credential and machine-bound files are on a
denylist: `Login Data*`, `Cookies*`, `Web Data*`, `Secure Preferences`,
`Local State`, `Affiliation Database*` and the `Network/` directory. No adapter
may name one — a test checks every registered adapter, so a future adapter that
does fails the build — and no bundle may restore one. Matching is per path
element and case-insensitive, because the filesystems this runs on are.

**Both directions refuse while the app is running.** `Sessions/`, `History` and
`Local Extension Settings/` are SQLite and LevelDB; copied out from under a live
process they are corrupt on arrival. The refusal names the app and says to quit
it — a running process is proof, not a guess
([ADR-0042](decisions/ADR-0042-an-archive-refuses-on-proof-and-warns-on-prediction.md)).

**The bundle** is `<app>-state-<YYYY-MM-DD>.tar.zst` plus a `.sha256` manifest,
written by the same writer and checked by the same verifier `dg archive` uses,
so it is an ordinary tar a machine with no devgeta can open:

```
tar --zstd -tf brave-state-2026-09-15.tar.zst          # list without extracting
shasum -a 256 -c brave-state-2026-09-15.tar.zst.sha256 # the bundle file is intact
```

Inside, every member sits under its profile's key — `Default/Bookmarks`,
`Profile 1/Bookmarks` — which is what lets one bundle hold several profiles that
all use the same file names. Export refuses before writing if any file it would
produce is already on the drive; it never overwrites.

**Export flags**:

- `--dry-run` — Print the report and write nothing. No destination directory
  needed.
- `--group a,b` — Export only these groups. The default is every group that is on.
- `--profile "Default,Profile 1"` — Export only these profiles. The default is
  all of them; an unknown key is an error that lists the real ones.

**Import flags**:

- `--force` — Replace state that is already there. Without it, an import into a
  profile that has state refuses and names the paths.
- `--group a,b` — Restore only these groups. The default is **every group the
  bundle carries, including ones that are off by default on the way out** — the
  export already decided what was worth carrying, so a bundle written with
  `--group history` restores its history rather than silently dropping it. A real
  group the bundle does not carry is a refusal naming it, not a quieter import.
- `--profile "Default,Profile 1"` — Restore only these profiles. A profile the
  bundle holds that this machine does not have is a refusal, not a directory
  devgeta creates: an app's profile registry is its own (for Brave it is
  `Local State`, which is denied), so a profile directory the app was never told
  about is one it never shows.

**What the import checks, in order.** Each step exists to fail before the one
after it can do damage:

1. **The app is not running.**
2. **The bundle is named for this app.** Every Chromium browser uses the same
   profile file names, so a Chrome bundle passes every path rule Brave applies
   and would restore cleanly into the wrong browser. The check is the bundle's
   base name, and it runs before anything reads its bytes. A name is not proof —
   a hand-built tar under the right name still passes — but it stops the
   realistic accident of two bundles on one drive and the wrong path typed.
3. **The `.sha256` manifest travelled with the bundle**, and the bundle's
   directory is writable (verification writes the bundle's own checksum beside
   it, which fails on a write-protected USB drive for a reason that has nothing
   to do with the bundle). Neither check is skippable.
4. **The bundle matches its manifest**, so a damaged transfer refuses before
   anything good is overwritten.
5. **Every member is valid.** The allowlist bound what devgeta packed, not what
   the tar in front of us contains, so each member must be a regular file or a
   directory (any symlink or hard-link member rejects the bundle — a link written
   early redirects a later, perfectly relative member outside the root), have a
   relative `..`-free path, start with one profile key, fall under one of the
   adapter's group paths, and match nothing on the denylist. One bad member
   rejects the whole bundle: a bundle carrying something it should not is not one
   to trust the rest of.
6. **`--force`**, if any selected path already exists.

**All or nothing.** Every path the import will write is renamed aside to a
`.dg-import-backup` sibling _before the first one is written_, so a failure on
the fourth of seven files restores all of them and the profile is exactly as it
was. On success the backups stay and the command prints where they are, so an
unwanted import can be undone by hand; a path the profile did not have before is
simply left in place, with no marker, since there is nothing to put back.

**Out of scope**: a sync or daemon mode (this is an explicit two-command move;
Brave Sync already occupies that space), credentials and cookies at any opt-in
level, recovering automatically from a crash _between_ the first and last write
(the by-hand undo is the one documented backup suffix), and Windows. Brave is
the only adapter today — see [docs/apps/brave.md](apps/brave.md) for exactly what
moves and what does not. An app with no adapter is not an error the user can
spell their way out of, so `dg export <app>` says so and names the apps that have
one.

#### `dg theme`

Shows, lists, or switches the active theme across every themed surface
(Alacritty, Ghostty, tmux, Neovim, OpenCode, Claude, i3). See
[docs/guides/theming.md](guides/theming.md) and
[docs/decisions/ADR-0043](decisions/ADR-0043-a-theme-is-a-palette-file-not-a-branch-in-every-template.md)
for the palette design this command drives.

```
dg theme                # show the current theme and what's available
dg theme list            # list available themes
dg theme set <name>      # switch every installed themed app to <name>
```

**Behavior**:

- `dg theme set <name>` is transactional: `<name>` is loaded and validated
  (every palette role present and parseable, `neovim_module` resolved against
  the shipped and deployed Neovim trees) before anything is written. Apps not
  installed on this machine are skipped with a printed reason and are left
  untouched.
- Every manifest path a themed app is about to overwrite is backed up first
  (renamed aside, or marked absent if it doesn't exist yet). If any app fails
  to configure, or if the final commit itself fails, every backed-up path is
  restored and `current_theme` is left exactly as it was — a switch never
  half-applies.
- `current_theme` is written last, together with clearing the in-progress
  marker, in one atomic config write — nothing on disk names the new theme
  until every app has already succeeded.
- A crashed switch's leftover backups are swept automatically on the next
  `dg theme set`, `dg configure`, or `dg install`, before any of those do
  their own filesystem work.
- `current_theme` is **not** a `dg config` key — `dg config get/set/unset
current_theme` is refused as an unknown setting, the same treatment
  `integrations.claude_instructions_imported` gets, because a bare `set` there would
  desync the recorded name from what the machine is actually configured with.

**Examples**:

```
dg theme
dg theme list
dg theme set tokyonight
dg theme set default
```

---

## Behavior & Edge Cases

### Installation Failures

- If an app installation fails partway through, document which apps were installed
- Provide clear error messages with steps to fix (e.g., "Permission denied, run: `sudo chown`")
- Do not partially commit state; either succeed or roll back to previous state

### Platform Differences

- Same feature set on macOS and Linux where possible
- Platform-specific apps clearly labeled in help text and documentation
- Same command syntax across platforms

### Version Management

- Languages installed via Mise (automatic version tracking)
- Mise enables multiple versions of same language
- Database versions follow platform package manager defaults
- User can override post-installation (e.g., `dg install node@20`)

### Configuration Files

- Templates provided in `configs/` directory
- User edits after installation are preserved (not overwritten)
- Re-configure command can update files (with user confirmation if file exists)

---

## Installation Flow (Current UX)

```
$ dg install

Welcome to Devgeta! Let's set up your development environment.

[✓] Installing terminal tools...
    ├─ curl
    ├─ git
    ├─ Zsh + Powerlevel10k
    ├─ Neovim
    ├─ Tmux
    ├─ fzf, ripgrep, bat, ...
    └─ Mise

Select programming languages to install:
  ◉ Node.js (LTS)
  ○ Python
  ○ Go
  ○ PHP
  ○ Rust

[✓] Installing languages...

Select databases to install:
  ◉ PostgreSQL
  ○ Redis
  ○ MySQL
  ○ MongoDB
  ○ SQLite

[✓] Installing databases...

Install desktop applications?
  ◉ Docker Desktop
  ○ Alacritty
  ○ Brave Browser
  ○ [others...]

[✓] Installing desktop apps...

✓ Setup complete! Restart your shell to activate.
  source ~/.zshrc
```

---

## Error Handling

### Common Issues & Messages

| Error                | Message                                                         | Resolution                              |
| -------------------- | --------------------------------------------------------------- | --------------------------------------- |
| Missing dependency   | "Git not found. Install from: [link]"                           | Prompt user to install dependency first |
| Permission denied    | "Permission denied: [path]. Run: `sudo mkdir -p [path]`"        | Specific fix instructions               |
| Package conflict     | "Homebrew already has [package] installed. Use system version?" | Allow user to skip or force             |
| Unsupported platform | "Your system (macOS 12) is not supported. Requires macOS 13+"   | Clear version requirement               |

---

## Testing Strategy

### Unit Tests

- Config parsing and validation
- State tracking logic
- Command argument parsing
- Cross-platform path handling

### Integration Tests

- End-to-end installation flows
- Config file creation and updates
- Multi-category installation combinations
- Idempotency (running install twice gives same result)

### Manual Testing Checklist

- [ ] Fresh system install (clean virtual machine)
- [ ] Install + skip category combinations
- [ ] Re-run install (idempotency)
- [ ] Verify shell configuration is sourced correctly
- [ ] Verify all installed tools are in PATH

---

## Platform Scope & Constraints

**Supported platforms:**

- **macOS 13+** (Ventura or newer) with Homebrew; supports both Apple Silicon (M1/M2/M3+) and Intel
- **Debian 12+** (Bookworm) and **Ubuntu 24+** with APT

**Intentional constraints:**

- **CLI-only** — No graphical installation interfaces
- **Official package sources only** — Homebrew (macOS) and APT (Linux); no custom repositories
- **No Windows support** — macOS and Linux only

---

## Related Documents

- `CLAUDE.md` — Development guidelines and constraints
- `docs/decisions/` — Architectural decisions
- `docs/plans/cycles/` — Feature planning and cycles
- `README.md` — User-facing documentation
- `docs/apps/agent-packs.md` — Agent packs: commands, overrides, opting out
