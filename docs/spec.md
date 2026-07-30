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
│   ├── apps/               # Individual app implementations (19 apps)
│   ├── commands/           # Platform-specific installers (Darwin, Debian)
│   ├── config/             # State management
│   └── tui/                # Interactive UI components
├── pkg/                    # Shared utilities (logger, paths, constants)
├── configs/                # Configuration templates (embedded at compile time)
└── docs/                   # Documentation
```

**Key patterns:**

- **Interface-based design** for cross-platform compatibility
- **Strategy pattern** for installation (AptStrategy, PPAStrategy, InstallScriptStrategy, etc.)
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

**Individually targetable apps** (registry-managed, 19 apps):

| Coordinator | Apps                                                                         |
| ----------- | ---------------------------------------------------------------------------- |
| terminal    | claude, fastfetch, git, lazydocker, lazygit, mise, neovim, opencode, tmux    |
| desktop     | aerospace, alacritty, brave, docker, flameshot, gimp, i3, raycast, ulauncher |
| ai-tools    | rtk                                                                          |

**Note on alacritty:** `alacritty` has `KindTerminal` in the registry but is installed by the desktop coordinator. Use `--only alacritty` (not `--only terminal`) to target it specifically.

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

_macOS_:

- Docker Desktop
- Alacritty terminal
- Brave browser
- Aerospace window manager
- Raycast launcher
- GIMP
- Flameshot

_Linux (Debian/Ubuntu)_:

- Docker Desktop
- Alacritty terminal
- Brave browser
- i3 window manager
- Ulauncher launcher
- GIMP
- Flameshot

**AI Tools** (no selection, installed with full setup or via `--only ai-tools`)

- rtk — token-compressing CLI proxy for coding agents (binary only; its
  command-rewriting hook is opt-in — see [docs/apps/rtk.md](apps/rtk.md) and
  [ADR-0004](decisions/ADR-0004-ai-tools-install-category.md))

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

#### `dg configure [app]`

Re-applies configuration files for a named app without reinstalling the app itself.

```
dg configure <app> [--force] [--only=<parts>]
```

**Flags**:

- `--force` — Overwrite existing configuration files. Without this flag, configuration is only applied if files do not already exist (soft mode).
- `--only=<parts>` — Refresh only the named app-defined parts; requires `--force` and an app implementing `SelectiveConfigurer`. The AI coders (`claude`, `opencode`) expose their shared config subtrees (`skills`, `commands`, `agents`) plus an `rtk` part that runs the matching `rtk init` to wire rtk's command-rewriting hook into that coder — the explicit opt-in required by ADR-0004. The Claude opt-in is recorded in `global_config.yaml` (`integrations.rtk_claude_hook`) and claude's `settings.json` is rendered from a template honoring it, so the hook survives later `--force` re-renders; `dg uninstall rtk` clears it.

**Behavior**:

- Exact app name required (case-sensitive). Supported apps: `aerospace`, `alacritty`, `brave`, `claude`, `devgeta`, `docker`, `fastfetch`, `flameshot`, `gimp`, `git`, `i3`, `lazydocker`, `lazygit`, `mise`, `neovim`, `opencode`, `raycast`, `rtk`, `tmux`, `ulauncher`.
- Apps that have no configuration to deploy (e.g., `brave`) return `ErrConfigureNotSupported` — the command prints an info message and exits zero.
- Unknown app names print a sorted list of supported apps and exit non-zero.
- Unknown `--only` values list the app's valid parts and exit non-zero; `--only` without `--force` is an error; apps without parts reject `--only`.

**Examples**:

```
dg configure git            # Apply git config if not already present
dg configure neovim --force # Overwrite existing neovim config
dg configure brave          # Info: configure not supported for brave (exit 0)
dg configure foo            # Error: unknown app "foo" + supported list (exit non-zero)
dg configure claude --force --only=skills          # Refresh only the skills folder
dg configure claude --force --only=rtk             # Opt into rtk's hook for Claude Code
dg configure opencode --force --only=rtk           # Install rtk's OpenCode plugin
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

| Subcommand      | Description                                            |
| --------------- | ------------------------------------------------------ |
| `create <name>` | Create a new worktree + tmux window                    |
| `list`          | List all managed worktrees                             |
| `remove [name]` | Remove a worktree (interactive picker if name omitted) |
| `repair <name>` | Recreate the tmux window for an existing worktree      |
| `prune`         | Remove **all** managed worktrees after confirmation    |

**Flags for `create` and `repair`**:

- `--ai <alias>` / `-a <alias>` — AI coder to launch in the window. Accepted aliases: `opencode`, `oc`, `claude`, `cc`, `claudecode`.
- `--layout <name>` / `-l <name>` — Window layout to build. Valid names: `opencode`, `claude`, `claude-nvim`, `nvim` (see "Window layouts" below). Mutually exclusive with `--ai` — passing both is a cobra error before either command runs.

  **Resolution order** (highest wins; each rule below only applies when none of the rules above it fired):

  1. `--layout` flag — explicit layout name, wins over everything.
  2. `--ai` flag — derived into a single-pane layout running that coder.
  3. `DEVGETA_WORKTREE_AI` env var — derived into a single-pane layout.
  4. `worktree.default_layout` in `global_config.yaml`.
  5. `worktree.default_ai` in `global_config.yaml` — derived into a single-pane layout.
  6. Default: `opencode`, single-pane.

  `repair` uses the exact same resolution order as `create` — it does **not** remember the layout a worktree was originally created with. If the window is missing, it is rebuilt from scratch using whatever `--layout`/`--ai`/env/config resolves to at that moment. If the window already exists (e.g. only one pane in it was closed), `repair` only relaunches the AI coder in the existing window — it does not add or recreate missing panes, since there's no way to tell whether the surviving panes already match the requested layout.

**Window layouts**:

A layout is a named, ordered set of tmux panes built when a worktree's window is created or repaired. Built-in layouts (no config required):

| Layout        | Panes                                                |
| ------------- | ---------------------------------------------------- |
| `opencode`    | Single pane running OpenCode                         |
| `claude`      | Single pane running Claude Code                      |
| `claude-nvim` | Claude Code and Neovim side by side (vertical split) |
| `nvim`        | Single pane running Neovim only                      |

Before any tmux window is touched, every pane's underlying tool is checked for installation; a layout referencing a missing tool fails with an actionable error and the worktree is not created. If a multi-pane window fails to build partway through (e.g. a later pane's split fails), the partially built window is killed and the worktree is rolled back — never left half-created.

**Worktree scan and layout config keys** (`global_config.yaml`, under `worktree:`):

Use [`dg config`](#dg-config) to list, read, set, or unset these — hand-editing
`global_config.yaml` directly still works, but is no longer necessary.

- `search_paths` — list of directories to scan for git repositories to offer in the `n`/`N` repo picker (see "Creating from the dashboard" below). Default: empty, which disables the scan entirely — this is the only off-switch. The scan walks each path with `filepath.WalkDir`, stops descending at a repo's `.git` boundary (so nested repos/submodules are not listed as separate entries), and skips `node_modules`, `.cache`, `vendor`, `target`, `dist`, and `.git` directories encountered during the walk (a configured root itself is still scanned even if its name matches one of these, e.g. a root literally named `vendor`).
- `scan_depth` — max directory depth below each search path to descend. Unset, `0`, or negative all mean the default of `4` — there is no separate "unlimited" or "disabled via depth" mode; use an empty `search_paths` to disable scanning.
- `default_layout` — default window layout name (see the resolution order above). Default: empty, which means rule 5 (`default_ai`-derived single-pane layout) or the built-in `opencode` fallback applies instead.
- `attach_after_create` — whether a successful `n`/`N` create in `dg ws` attaches into the new worktree's window (quitting the dashboard) or leaves you on the new row to keep working in the dashboard. Default: **absent, which means attach** — set it to `false` to stay put, which is the useful setting when you create several worktrees in one sitting, since attaching ejects you from the dashboard after the first one. Outside tmux there is no client to move, so the create already stays put regardless of this key. Unlike every other boolean in `global_config.yaml` (all feature flags where "off" is the right default), an absent key here must mean **on**, so the field is stored as a nullable boolean: absent is distinct from an explicit `false`, and configs written before this key existed keep attaching.
- `notify_sound` — whether an agent pane finishing (`idle`), blocking on a permission prompt (`blocked`), or erroring (`error`) plays a sound while its window is unattended, per [ADR-0009](decisions/ADR-0009-audible-agent-notifications.md). Default: **absent/`false`, which means off**. Unlike `attach_after_create` just above, where the zero value would silently flip existing behavior and so needed a nullable bool, off is already the correct default here, so a plain boolean is enough — the zero value already means what it should. The two hooks that actually play the sound (`configs/claude/agent-state.sh`, `configs/opencode/plugin/notify.js`) cannot parse `global_config.yaml`, so this YAML value is only the durable source: `configs/tmux/tmux.conf.tmpl` renders it into the deployed `~/.tmux.conf` as the tmux global option `@dg_notify_sound`, which is what the hooks actually query at runtime. Changing this key therefore has no effect on a running tmux server by itself — it takes effect after `dg configure tmux --force` plus a config reload (or a fresh server), or immediately if you also run `tmux set-option -g @dg_notify_sound on` by hand.

**Flag for `create`**:

- `--repo <path>` / `-r <path>` — Path to the repository (`~` is expanded), so the command works
  from any directory. The window opens in a tmux session named after the repo — created when
  missing, reused otherwise — and the attached client switches to it when run inside tmux.
  Without the flag, the repo is the one containing the current directory and the window opens
  in the current session.

**Flag for `remove`**:

- `--force` / `-f` — Force removal even if the worktree has uncommitted changes.

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
```

**Creating from the dashboard (`n` / `N`)**:

- `n` opens a floating repo picker over the dashboard — the background stays visible, matching
  the `?` help overlay. Candidates are ranked: the repo containing the directory `dg ws` was
  launched from first (when that directory is inside a git repo — otherwise this source is
  skipped), then the repo under the cursor, then repos from the recent-repos store
  (most-recently-used first), then repos found by scanning `worktree.search_paths` (see above —
  contributes nothing until configured), then `zoxide query -l` results when zoxide is
  installed. Typing filters the list; if the query matches nothing, Enter validates it directly
  as a free-typed repo path instead.
- Enter on a repo opens a floating name prompt. For `n`, Enter on the name creates the worktree
  immediately using the resolved default layout (same as `create --repo` with no `--layout`/
  `--ai`) and attaches into the new window — the TUI exits, identical to pressing Enter on an
  existing row.
- `N` follows the same repo-pick → name-prompt flow as `n`, but after the name is entered it
  opens one more floating picker: a layout picker listing the built-in layout names
  (`opencode`, `claude`, `claude-nvim`, `nvim`), cursor pre-positioned on the resolved default so
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

**Kicking a review from the dashboard (`R`)**:

- `R` on a worktree row opens a floating picker with three choices, `code` first:
  `code — bugs, security`, `document — plans, specs`, `skill — agents/commands`.
- Selecting one launches the matching reviewer agent (`code-reviewer`, `document-reviewer`, or
  `skill-reviewer`) via OpenCode, in that worktree, with the fixed prompt "Review this branch
  against the default branch." — the agent scopes itself from there (each reviewer agent already
  runs `devgeta task review-scope` on its own).
- If the worktree's window isn't live, a new window is created with the review as its only pane.
  If the window already has a coder running, a new pane is split beside it and the review
  launches there instead — it never types into the existing coder's pane.
- `R` only applies to worktree rows (see the `D`/`r`/`R` note below for the exact no-ops), and
  it's a no-op on every row while any review launch is already in flight (a single global guard,
  not per-row), so repeated presses can't start a second launch.
- Esc at the picker cancels and returns to the dashboard unchanged.

**In-progress feedback**: any dashboard action that builds or tears down tmux state shows a
status line at the bottom while it runs, so the dashboard is never silently unresponsive during
the (occasionally slow) git/tmux work: `creating worktree: <name> (<layout>)…` on create,
`repairing: <name> (<layout>)…` on `r` (and on attaching to a worktree whose window is missing,
which auto-repairs), `deleting: <name>…` on the confirming `d`/`D` press, and `review: <name>
(<label>)…` on `R`, resolving to `review started: <name>` or `review failed: <error>`. The layout is named
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

| Subcommand               | Description                                                                                        |
| ------------------------ | -------------------------------------------------------------------------------------------------- |
| _(bare, same as `list`)_ | Print every setting: key, current value (or `(default)` if unset), live default, and description   |
| `get <key>`              | Print only the effective value, nothing else (bare, for scripts)                                   |
| `set <key> <value...>`   | Validate and persist a value; `worktree.search_paths` is the only key that accepts multiple values |
| `unset <key>`            | Clear a setting back to its default                                                                |

**Flags**:

- `--plain` — Suppress the interactive hint line that follows the table (persistent flag; works on both `dg config` and `dg config list`).

**Settable keys**: `worktree.default_ai`, `worktree.search_paths`, `worktree.scan_depth`,
`worktree.default_layout`, `worktree.attach_after_create`, `worktree.notify_sound` — see
"Worktree scan and layout config keys" above for what each one does. Setting
`worktree.notify_sound` only persists the YAML value and the tmux-rendered default for the
_next_ server — see that key's entry above for why it does not also flip the option on an
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
dg config set worktree.search_paths ~/code ~/work   # search_paths is the only variadic set
dg config unset worktree.attach_after_create        # Clear back to default (true)
```

#### `dg ws`

```
dg ws
dg workspace   # alias
```

Unified full-screen TUI dashboard — the single entry point to the worktree/session UI (the
old `dg wt ui` subcommand has been removed). Scoped to **workspaces** rather than worktrees
only. Every top-level row in the dashboard is exactly one of two kinds:

- **Repo workspace** (worktree-backed): a repo with git worktrees, sourced from the same
  worktree scan `dg wt list` uses. Expandable to its worktree rows via `h`/`l` (or `z`
  to toggle every repo at once), shown with a `▼`/`▶` chevron and an `N trees` badge. Shown even
  when its repo-slug tmux session isn't live.
- **Session workspace**: a standalone tmux session with no worktree-backed (`wt-`) window,
  sourced from `tmux list-sessions`. A leaf row, labeled `session`, unless it qualifies for
  its own pane-row expansion (see below).

The two kinds carry different marker shapes so they're distinguishable at a glance while no
agent has ever reported on them, not just by their label: worktree rows use a circle (`●`
running / `○` not), session rows use a square (`■` attached / `□` detached) — in both, a
filled glyph means active and the color matches (green active, dim inactive). This shape
distinction is the "quiet" default; once an agent has reported, the row switches to the state
vocabulary described next, which is shared across every row kind.

The dot on a worktree row, a session row, and a repo header all report what the AI coder(s)
underneath are doing, not just whether something is running. On top of running (`●` green) /
not-running (`○`/`□` gray, no window or no agent yet), three "wants you" states layer on top
of a live window: finished and waiting on you (`◆` purple), blocked on a permission prompt
(`!` red), and errored (`✕` bold red). See
[ADR-0005](decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md) for the underlying
signal — each coder writes its activity to a tmux pane option — and
[ADR-0008](decisions/ADR-0008-agent-state-on-every-pane-row.md) for how that signal was
extended to every row kind, all sharing one precedence rule
(`blocked > error > idle > busy > no agent`, most urgent wins):

- **Worktree rows** aggregate every pane of their `wt-…` window, as before: a window holding
  more than one coder pane (e.g. a split-pane review beside a working coder) shows the most
  urgent state, so one finished pane is enough to show `◆` even while its neighbor keeps
  working.
- **Session rows** aggregate every pane in that tmux session and show the same state
  vocabulary (`●`/`◆`/`!`/`✕`) once any agent has reported there, replacing the plain
  attached/detached square with a colored dot. A session nobody has ever run an agent in —
  the common case — keeps the plain `■`/`□` square; the shape distinction still applies to
  that quiet case.
- **Repo headers** aggregate across every worktree in the repo (whether or not the repo is
  currently expanded), so collapsing a repo no longer hides an urgent child's state. A repo
  where no worktree has ever had an agent report falls back to a dim "not running" glyph
  rather than a false all-clear.
- **Individual panes**, revealed by expansion (next paragraph), each show their own dot for
  that one pane's state.

A worktree row or session row with **two or more** panes reporting a non-empty agent state
gains its own `▼`/`▶` chevron (the same convention as a repo header), and `h`/`l` reveal or
hide its **pane rows** — one child row per pane, indented further than the parent, showing
the pane's index, the command currently running in it, and that pane's own dot. This answers
"which pane wants attention," not just "which window": a window with a working coder and a
finished reviewer side by side shows exactly which one is which once expanded. A parent with
zero or one stateful pane never gets a chevron — a single pane's state is already exactly what
the parent's own dot says, so a chevron there would be noise. Collapsing a worktree/session's
pane rows is independent of collapsing a repo header, even when a repo and a standalone
session happen to share the same name.

Attaching to a row (`enter`) clears its state — attaching is the user acknowledging it.
tmux's own status bar (`configs/tmux/tmux.conf.tmpl`) separately flags any other window in the
current session whose coder wants attention while you're looking elsewhere, so `dg ws`
doesn't have to stay open to notice. That status bar only paints windows of the **attached**
session, though, and gives no signal at all when the terminal itself isn't the focused window —
an agent blocked in another session, or in any window while you're looking at a different
application, has nothing visual reaching you.
[ADR-0009](decisions/ADR-0009-audible-agent-notifications.md) adds an audible signal for
exactly that gap: the same two hooks that write `@dg_agent_state` also play a sound for the
same three "wants you" states (`idle`, `blocked`, `error`) at the moment they write them —
never for `busy`, since an agent starting work isn't an event you asked to hear — gated on
`window_active_clients == 0`, the identical predicate the status-bar flag above already uses,
so the audible and visual signals can never disagree about whether you've seen something. It
is off by default and opt-in (see `notify_sound` above), each state has a distinct sound so
the three are told apart without looking, and a missing sound player or audio device is
silence rather than an error or a blocked hook.

Both kinds share the existing worktree-row keys (`j`/`k` nav,
`h`/`l` fold, `z` toggle-all, `n`/`N` create a worktree, `/` filter, `?` help, `q` quit).
Session rows add:

- `enter` — switch the attached tmux client to the session (guarded: only works inside tmux,
  same guard message as attaching to a worktree) and quit the dashboard.
- `d` `d` — kill the session (two-press confirm, same "press again" hint style as worktree
  delete).
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
  no separate pre-check.

- `D`/`r`/`R` are worktree-only actions and are no-ops on a session row. `R` is additionally a
  no-op on a repo-header row, since only a worktree row has a specific worktree to
  review.

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
PR review, and releasing. These commands are callable by both agents (Claude Code,
CI, any non-interactive process) and humans (via the `dge` shell wrapper or directly).

```
dg task <subcommand> [args]
dg t <subcommand> [args]   # alias
```

| Subcommand            | Args       | Description                                                                            |
| --------------------- | ---------- | -------------------------------------------------------------------------------------- |
| `refresh-branch`      | `[target]` | Checkout target (default: `main`), pull, return to previous branch, merge              |
| `reset-main-branch`   | —          | Checkout `main`, hard-reset to `origin/main`                                           |
| `delete-branch`       | `[target]` | Checkout target (default: `main`), fetch, pick a branch via fzf to force-delete        |
| `reinstall-libraries` | —          | `git clean -Xdf`, remove `node_modules/`, `npm install`, remove `tsconfig.tsbuildinfo` |
| `reinstall-library`   | `<name>`   | Remove `node_modules/<name>`, run `npm install`                                        |

**Review scope subcommands** (compact, noise-filtered git context for agents — the
`review-threads` pattern applied to git; `git` plumbing is fetched, Go formatters render):

| Subcommand       | Args / Flags                     | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ---------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `review-scope`   | `--bodies`                       | Fetch origin (bounded, best-effort), then print branch, default branch, ahead/behind, commit lines (short SHA, ISO date, subject), and a per-file stat table. `--bodies` appends each commit's body as indented lines beneath its subject. Lockfile-style noise (`package-lock.json`, `go.sum`, `*.min.js`, …) is excluded from the table and noted separately with its own counts — never silently dropped.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `branch-diff`    | `--file <path>`                  | Diff against the merge-base with the default branch, same default exclusions applied in one `git diff` call. Does **not** fetch (reuses `review-scope`'s comparison base within the same review session). `--file` bypasses exclusions for that one file.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `review-package` | `<base> <head>`, `--file <path>` | Verify both refs resolve (`rev-parse --verify`, an actionable error names whichever ref failed), then in one call print `range: <base>..<head>`, the commit list (short SHA, date, subject), a noise-filtered per-file stat table with exclusion receipts, and the full `-U10`-context diff of the included files as a fenced ` ```diff ` block. Unlike `review-scope`/`branch-diff`, base and head are not tied to the current branch's default-branch merge-base — this is for reviewing an arbitrary historical range or a PR that isn't checked out. `--file` bypasses exclusions and returns just that file's `-U10` diff. Sentinels: `No commits in range.` when the commit list is empty, `No file changes in range.` when the stat table is empty. Replaces a 6-call raw dance (`rev-parse --verify` x2, `log --oneline`, `diff --stat`, `diff -U10`, `rev-list --count`) that measured 793,426 bytes on a representative 10-commit range (`b0e98fd..main` in this repo); the one-call equivalent on the same range measured 792,704 bytes — the byte savings come from applying the same default lockfile exclusions as `review-scope`/`branch-diff`, not from compressing the diff itself (which review-package still prints in full); the real win is collapsing 6 round-trips into 1, per the "collapse round-trips" justification in `docs/guides/task-design.md`. |

**Worktree lifecycle subcommands** (start/finish a git worktree in one call each —
same base path `dg wt` uses, `~/.local/share/devgeta/worktrees/<repo-slug>/<flat-name>`,
so `dg wt list` and worktrees created here are the same population, never two parallel
trackers):

| Subcommand        | Args / Flags                              | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| ----------------- | ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `worktree-start`  | `<name>`, `--base <ref>`                  | Refuse on a dirty tree, fetch origin, then create a worktree + branch at `dg wt`'s shared location. Without `--base`, the branch is based on the freshly-fetched default branch (reusing the same local/remote-branch-reuse logic as `dg wt create`); with `--base`, the branch starts fresh from exactly that ref. Prints `Created worktree <path> (branch <name>, base <ref>)`.                                                                                                                                                                                                                                                                                                                       |
| `worktree-finish` | `[name]`, `--merge\|--discard`, `--force` | Tear down a worktree. Target resolution is deterministic: an explicit `name` wins; otherwise the current directory resolves to the linked worktree it's inside; otherwise the command errors and lists the worktrees it found — it never guesses from a main checkout. `--merge` rebases onto the default branch if diverged, fast-forward-merges from the main checkout, then removes the worktree and deletes the branch (safe only once the fast-forward landed the branch's commits). `--discard` refuses on a dirty worktree unless `--force`, then removes the worktree and deletes the branch unconditionally. Does not run a build or test suite — verification is the caller's responsibility. |

**Pull request subcommands** (via `gh`; data-returning ones are formatted by `jq`
into compact, LLM-oriented output — `gh` fetches/acts, `jq` renders):

| Subcommand              | Args / Flags                                  | Description                                                                                         |
| ----------------------- | --------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `review-threads`        | `--pr N`, `--state unresolved\|resolved\|all` | Render PR review threads as compact markdown (default: unresolved)                                  |
| `resolve-thread`        | `<id>`                                        | Mark a review thread resolved                                                                       |
| `unresolve-thread`      | `<id>`                                        | Reopen a resolved review thread                                                                     |
| `reply-thread`          | `<id> <body>`                                 | Reply to a review thread                                                                            |
| `create-pr`             | `--title` (req), `--body`, `--base`           | Open a PR from the current branch; prints the URL                                                   |
| `update-pr-description` | `--pr N`, `--body` (req)                      | Replace a PR's description                                                                          |
| `approve-pr`            | `--pr N`, `--body`                            | Approve a PR                                                                                        |
| `request-changes-pr`    | `--pr N`, `--body` (req)                      | Request changes on a PR                                                                             |
| `request-review`        | `--pr N`, `<reviewer>...` (req)               | Re-request review (adds reviewers back to the requested list)                                       |
| `comment-pr`            | `--pr N`, `--body` (req)                      | Post a top-level PR comment                                                                         |
| `merge-pr`              | `--pr N`, `--method squash\|merge\|rebase`    | Merge a PR (default: squash)                                                                        |
| `pr-view`               | `--pr N`                                      | Compact PR summary (number, title, state, mergeable, review, branch)                                |
| `pr-checks`             | `--pr N`                                      | CI check status, one line per check; failing checks get an indented log digest appended (see below) |
| `current-pr`            | —                                             | PR number for the current branch                                                                    |
| `current-repo`          | —                                             | Current repository as `owner/name`                                                                  |

For every PR subcommand, `--pr` defaults to the current branch's PR when omitted.
Review-thread output is paginated across all threads (`gh api graphql --paginate`).

**`pr-checks` failure digest.** Passing and pending checks stay exactly one
line each, in `gh pr checks`'s own format (`<STATE>\t<name>  <link>`) —
unchanged from before this digest existed. A failing check (`bucket ==
"fail"` in `gh pr checks --json ...,bucket`) gets extra indented lines
appended under its one-liner:

- If the check's `link` matches a GitHub Actions job URL
  (`.../actions/runs/<run-id>/job/<job-id>`, optionally with a
  `#step:N:M` fragment), devgeta fetches that job's failed-step log
  (`gh run view --job <job-id> --log-failed`) and appends a bounded,
  deduplicated tail: consecutive identical log lines collapse into one
  line with a `(×N)` suffix (CI retry/poll loops routinely repeat a line
  dozens of times), then only the last ~60 lines are kept — the tail,
  since the real failure is almost always at the end. When lines are cut
  this way a receipt is prepended: `… 214 earlier lines omitted`. This
  receipt is never emitted when nothing was cut.
- If the link doesn't match that exact shape (external checks, commit
  statuses), devgeta never guesses a job id — it appends `log unavailable:
external check` instead.
- If the job id parses but the log fetch comes back empty or errors
  (verified in practice: `gh`'s log-download API only serves log content
  to users with write access to the check's repo, even for public repos),
  it appends an honest `log unavailable: ...` note rather than fabricating
  content.

The combined digest size across every failing check in one call is capped
at 240 lines total; once that budget is spent, remaining failing checks get
a one-line `log digest omitted: total digest size bound reached` note
instead of a fetched digest (no further `gh` calls are made once the budget
runs out).

The 60-lines-per-check figure is a documented estimate, not a measurement
against a real failing-run log: fetching a real third-party failing job's
log was attempted (`junegunn/fzf`, `BurntSushi/ripgrep`) and blocked by the
same write-access gating above. 60 sits at the top of this feature's
originally suggested 40-60 line range and matches the order of magnitude
observed for one ordinary CI step's full log on this repo's own successful
runs (30-90 lines/step).

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
Only when `--push` is passed: `git push origin <default> --tags`.

Without `--push`, nothing is pushed — the final line states exactly what remains,
e.g. `Tagged v0.12.0 (squashed 3 commits). Not pushed — run: git push origin main --tags`.
A failure partway through a mutation (reset, commit, tag, or push) reports the exact
state left behind and the raw git command to finish or undo it by hand, since these
steps are hard to reverse once they run.

**Redirect hook** (steers agents from raw git to the task equivalents above):
a Claude Code `PreToolUse` hook (`configs/claude/task-redirect.sh`, deployed to
`~/.claude/task-redirect.sh` and registered on the `Bash` tool in
`settings.json`) and an OpenCode plugin equivalent
(`configs/opencode/plugin/task-redirect.js`, deployed to
`~/.config/opencode/plugin/`, a `tool.execute.before` hook) intercept a narrow
set of raw-git and `gh` patterns and deny with the exact `devgeta task`
replacement. **Global rules** (fire in every repo, since these hooks deploy to
the user's global config): `git diff <ref>..<ref>` / `git log <ref>..<ref>`
(any flags) → `review-package`; `gh pr checks` → `pr-checks`; `gh api
graphql ... reviewThreads` → `review-threads`; `gh pr review` → `submit-review`.
**Devgeta-repo-only rules**: `git worktree add` → `worktree-start`, `git
worktree remove` → `worktree-finish`, `git reset --soft HEAD~N` (N ≥ 1), and
`git tag -a v<semver>` — these encode devgeta's own worktree storage layout
(`~/.local/share/devgeta/worktrees/...`) and release policy, so they fire only
when the command runs inside the devgeta repo (detected by walking up from the
payload's `.cwd`, falling back to `$PWD`, to a `go.mod` with module
`github.com/cjairm/devgeta`); the check runs only after a pattern matches and
fails toward not firing, so a general `git worktree`/`git reset`/`git tag` in
any other repo is never blocked. Matching checks every command segment (split on
unquoted `&&`, `||`, `;`, `|`, and tolerant of a leading `VAR=value` prefix), so
`cd x && git worktree add y` and `git fetch && git diff a..b` are caught too —
while a bare `git diff`, `git log`, `git tag` (list), `git reset --soft HEAD`
(no `~N`), `gh pr view`, or a bare `gh api graphql` is still never intercepted.
Deny is exit-code-based (exit 2 + a one-line stderr reason for
Claude Code; a thrown `Error` for OpenCode), never a silent rewrite, and every
deny message states the bypass: export `DEVGETA_SKIP_TASK_REDIRECT=1` in the shell
that launches the agent (e.g. the repo's `.envrc`), before invoking it. See
[docs/apps/claude.md](apps/claude.md#command-redirect-pretooluse-hook) for the
full contract.

`dge` (the shell function in `devgeta.zsh`) is now a thin wrapper that forwards to `dg task`:

```sh
dge() {
  if [[ $# -eq 0 ]]; then dg task --help; return; fi
  dg task "$@"
}
```

Agents should prefer `dg task` directly; humans can use either `dg task` or `dge`.

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
