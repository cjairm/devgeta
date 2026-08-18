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
- `notify_sound` — whether an agent pane finishing (`idle`), blocking on a permission prompt (`blocked`), or erroring (`error`) plays a sound while its window is unattended, per [ADR-0009](decisions/ADR-0009-audible-agent-notifications.md). Default: **absent/`false`, which means off**. Unlike `attach_after_create` just above, where the zero value would silently flip existing behavior and so needed a nullable bool, off is already the correct default here, so a plain boolean is enough — the zero value already means what it should. The two hooks that actually play the sound (`configs/claude/agent-state.sh`, `configs/opencode/plugin/notify.js`) cannot parse `global_config.yaml`, so this YAML value is only the durable source: `configs/tmux/tmux.conf.tmpl` renders it into the deployed `~/.tmux.conf` as the tmux global option `@dg_notify_sound`, which is what the hooks actually query at runtime. Changing this key therefore has no effect on a running tmux server by itself — it takes effect after `dg configure tmux --force` plus a config reload (or a fresh server), or immediately if you also run `tmux set-option -g @dg_notify_sound on` by hand.
- `location` — where worktrees are created on disk, and where `remove`/`repair`/state checks look for an existing one. Default: `shared` (`~/.local/share/devgeta/worktrees/<repo-slug>/<name>`, today's behavior — no existing install changes). The other value, `in-repo`, creates worktrees at `<repo-root>/.claude/worktrees/<name>` instead, the same path Claude Code's own worktree feature uses. Changing this key only affects where _new_ worktrees are created — it does not relocate an existing one on its own. `remove`/`repair` always check git for a worktree's actual location first (both shapes), so they still find an existing worktree wherever it really lives even if this key changes underneath it; use `dg wt move` (below) to physically relocate one that already exists.

**Review config keys** (`global_config.yaml`, under `review:` — see
[ADR-0017](decisions/ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md)):

- `reviewers` — list of AI reviewer models `dg task review-run` runs headless, each written as `provider/model` (e.g. `anthropic/claude-opus-4-6`). Default: empty, which runs one reviewer on OpenCode's own default model. Validation only checks the shape (at least one `/`, non-empty on both sides of the first one) — a stale or unconfigured provider is not caught here; it surfaces as an `ERROR(<reason>)` outcome from `review-run` at run time instead. `/review-loop` narrows this key mid-run to just the reviewers still blocking, and restores it after every round — see `/review-loop` below for the mechanics and the interruption residual.
- `rounds` — max review rounds `/review-loop` performs **per phase** before moving on. Default: `3`. Valid range: `1`–`5`; anything outside it is rejected by `dg config set`, not clamped. A full run is at most this cap plus 2 rounds — the one opening round and the one confirming round that bracket the phase this caps.

**Flag for `create`**:

- `--repo <path>` / `-r <path>` — Path to the repository (`~` is expanded), so the command works
  from any directory. The window opens in a tmux session named after the repo — created when
  missing, reused otherwise. Without the flag, the repo is the one containing the current
  directory and the window opens in the current session.

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

**Kicking a review from the dashboard (`R`)**:

- `R` on a worktree row opens a floating picker with three choices, `code` first:
  `code — bugs, security`, `document — plans, specs`, `skill — agents/commands`.
- Selecting one launches the matching reviewer agent (`code-reviewer`, `document-reviewer`, or
  `skill-reviewer`) via OpenCode, in that worktree, with the fixed prompt "Review this branch
  against the default branch." — the agent scopes itself from there (each reviewer agent already
  runs `devgeta task review-scope` on its own).
- Where the review lands, in order: no live window → a new window with the review as its only
  pane; a pane sitting at a shell prompt → the review runs **in that pane**, no new pane (this is
  what makes `R` on a `shell`-layout worktree reuse the empty pane you already have, and it prefers
  the lowest-numbered idle pane, i.e. the one devgeta built the window with); otherwise → a new
  pane split beside the running one.
- The rule is "never type into a pane that is running something", not "always split". Idle is
  decided by the same shell allowlist `dg wt move` uses to pick panes it may `cd`
  (`isIdleShellPane`) — anything unrecognized counts as busy, because a running agent cannot be
  identified by process name (a Claude Code pane reports its versioned binary directory, e.g.
  `2.1.222` — see [ADR-0008](decisions/ADR-0008-agent-state-on-every-pane-row.md)). So the
  fallback is always the safe one: at worst you get today's extra pane. A reused pane is never
  killed on failure — it is the user's own shell, not something `R` created.
- Either way you stay in the dashboard: `R` reports `review started: <name>` and leaves you on the
  list, so you can kick a review per worktree without being thrown into each one. (The
  window-was-missing case used to yank you into the new window; window creation no longer moves
  the client at all.)
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

| Subcommand               | Description                                                                                                                                              |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| _(bare, same as `list`)_ | Print every setting: key, current value (or `(default)` if unset), live default, and description                                                         |
| `get <key>`              | Print only the effective value, nothing else (bare, for scripts)                                                                                         |
| `set <key> <value...>`   | Validate and persist a value; a `stringlist`-kind key (`worktree.search_paths`, `review.reviewers`) accepts multiple values, every other key exactly one |
| `unset <key>`            | Clear a setting back to its default                                                                                                                      |

**Flags**:

- `--plain` — Suppress the interactive hint line that follows the table (persistent flag; works on both `dg config` and `dg config list`).

**Settable keys**: `worktree.default_ai`, `worktree.search_paths`, `worktree.scan_depth`,
`worktree.default_layout`, `worktree.attach_after_create`, `worktree.notify_sound`,
`worktree.location`, `review.reviewers`, `review.rounds` — see "Worktree scan and layout
config keys" and "Review config keys" above for what each one does.
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
dg config set review.reviewers anthropic/claude-opus-4-6 openai/gpt-5.2
dg config set review.rounds 5                       # Cap /review-loop at 5 rounds per phase
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
finished reviewer side by side shows exactly which one is which once expanded. `enter` on a
pane row switches the attached tmux client straight to that exact pane, not just its window.
A parent with zero or one stateful pane never gets a chevron — a single pane's state is
already exactly what the parent's own dot says, so a chevron there would be noise. Collapsing
a worktree/session's pane rows is independent of collapsing a repo header, even when a repo
and a standalone session happen to share the same name.

Attaching to a row, or switching to a pane (`enter`), clears its state — attaching is the user acknowledging it.
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
`h`/`l` fold, `z` toggle-all, `n`/`N` create a worktree, `/` filter, `?` help, `q` quit), plus
two keys added this cycle: `e` toggles the left pane between its default width and double
width, both clamped to 60% of the terminal — per-session only, not persisted — and `ctrl+r`
recomputes the branch diff for the currently selected row, in both the list and the
diff-focused view. `ctrl+r` is diff-only: it deliberately does not re-read git worktree state
([ADR-0024](decisions/ADR-0024-the-dashboard-refreshes-fast-and-slow-state-separately.md)).
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

Pane rows add:

- `enter` — switch the tmux client to that exact pane (same tmux guard and dashboard-quit
  behavior as attaching to a session or worktree). Previously a no-op.
- The diff pane: a pane belongs to a worktree, so moving the cursor onto one of a worktree's
  pane rows keeps showing that worktree's branch diff, exactly as if the cursor were still on
  the worktree row itself — drilling into a pane (`l`) never blanks or delays the diff. A pane
  row under a standalone session shows no diff, same as the session row itself.

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
| `review-scope`   | `--bodies`                       | Fetch origin (bounded, best-effort), then print branch, default branch, ahead/behind, commit lines (short SHA, ISO date, subject), and a per-file stat table. The table covers everything the branch would merge — its commits AND its uncommitted work, staged or not ([ADR-0019](decisions/ADR-0019-a-review-covers-the-branch-working-state.md)) — with two notes under it: `uncommitted (in the table above, in no commit yet): …` names the changed files no commit carries, and `untracked (not in the table above, no diff — read them directly): …` names files git does not track (no counts exist for those, so they are named only and are not in the table's file count). `--bodies` appends each commit's body as indented lines beneath its subject. Lockfile-style noise (`package-lock.json`, `go.sum`, `*.min.js`, …) is excluded from the table, and from the uncommitted note, and reported separately with its own counts — never silently dropped.                                                                                                                                                                                                                                                                                                                                                                                                         |
| `branch-diff`    | `--file <path>`                  | Diff the working tree against the merge-base with the default branch (`git diff <base>`, two dots) — so committed AND uncommitted work is included ([ADR-0019](decisions/ADR-0019-a-review-covers-the-branch-working-state.md)) — with the same default exclusions applied in one `git diff` call, and untracked files listed by name at the end under `Untracked files (no diff — read them directly):` since `git diff` cannot see them. This is the same gather `dg ws`'s diff pane uses, so the two always agree. Does **not** fetch (reuses `review-scope`'s comparison base within the same review session). `--file` bypasses exclusions for that one file and, on an untracked file, says so instead of reporting "no changes" — an empty diff there means the whole file is the change.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `review-package` | `<base> <head>`, `--file <path>` | Verify both refs resolve (`rev-parse --verify`, an actionable error names whichever ref failed), then in one call print `range: <base>..<head>`, the commit list (short SHA, date, subject), a noise-filtered per-file stat table with exclusion receipts, and the full `-U10`-context diff of the included files as a fenced ` ```diff ` block. Unlike `review-scope`/`branch-diff`, base and head are not tied to the current branch's default-branch merge-base — this is for reviewing an arbitrary historical range or a PR that isn't checked out. `--file` bypasses exclusions and returns just that file's `-U10` diff. Sentinels: `No commits in range.` when the commit list is empty, `No file changes in range.` when the stat table is empty. Replaces a 6-call raw dance (`rev-parse --verify` x2, `log --oneline`, `diff --stat`, `diff -U10`, `rev-list --count`) that measured 793,426 bytes on a representative 10-commit range (`b0e98fd..main` in this repo); the one-call equivalent on the same range measured 792,704 bytes — the byte savings come from applying the same default lockfile exclusions as `review-scope`/`branch-diff`, not from compressing the diff itself (which review-package still prints in full); the real win is collapsing 6 round-trips into 1, per the "collapse round-trips" justification in `docs/guides/task-design.md`. |

**Review journal subcommands** (per-branch review memory, so a re-review does not re-ask
what was already answered — see
[ADR-0012](decisions/ADR-0012-review-knowledge-in-a-local-journal.md)):

| Subcommand     | Args / Flags                                                                                                                                                           | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `review-notes` | `--branch <b>`, `--rev <sha>`, `--path`, `--prune`                                                                                                                     | Print the branch's journal: settled entries (answered / rejected-with-reason / fixed) and still-open ones, each with its id and a resolved `[fresh]` or `[STALE: …]` marker. Staleness compares the **content** of the cited file (`git hash-object`), not a commit, so an uncommitted edit correctly invalidates an entry — a commit-based check cannot see one. A **rejected** entry is exempt and reads `[STANDING: …]` instead of either: a rejection records a decision about the reason it states, and [ADR-0012](decisions/ADR-0012-review-knowledge-in-a-local-journal.md) §2 has it expire when that reason stops holding, never because the cited file moved. Findings cluster in a handful of files, so without the exemption the first fix of any round flipped every rejection in those files to "re-check", which is the re-litigation the journal exists to stop. An entry citing no file carries no marker, because nothing can date it. `--rev <sha>` resolves staleness against that revision instead of the working tree, for reviewing code that is not checked out (a PR head): pass the revision under review **now**, so `[STALE]` means "that revision changed this file since the finding was written" rather than "your checkout differs from it" — which is true of nearly every file and would mark the whole journal stale ([ADR-0023](decisions/ADR-0023-a-pr-review-targets-immutable-shas.md) §4). It is refused with `--path` and `--prune`, which compute no freshness. A `--rev` this repository does not have is refused up front, naming the revision — never reported as bad cited paths or as a journal of stale findings. `--path` prints the journal file's location for hand correction; `--prune` deletes journals whose branch no longer exists locally or on the remote; a PR-scoped journal (`pr/<owner>/<repo>/<number>`) is left alone, because no branch ever carries that name and the check would report every one of them gone — including a PR still under review. Sentinels: `No review notes for branch <b>.`, `No branch — review notes are keyed by branch.` (detached HEAD), `No review journals to prune.`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `review-note`  | `--open` \| `--settle` \| `--ratify` \| `--reopen`, `--id <n>`, `--as rejected\|answered\|fixed`, `--at <path[:line]>`, `--note <text>`, `--branch <b>`, `--rev <sha>` | Write one entry. `--open` records something awaiting an answer and echoes its new id (`Noted n4`); `--settle --id n4 --as …` closes that entry, carrying its cited path over so an answer can never retarget the question it closes (`--at` is refused there); `--settle` without an id records an exchange that was never open. `--at` is optional — a design-level entry cites no file and never goes stale — but when given, the path must exist, because an entry that claims to cite code with no content stamp could never be checked. `--rev <sha>` stamps the cited path at that revision instead of the working tree, and the path must exist **there** — the same typo guard, relocated — for reviewing code that is not checked out, where the cited file may be missing from the checkout or hold unrelated content ([ADR-0023](decisions/ADR-0023-a-pr-review-targets-immutable-shas.md) §4). The head stamp is then the revision itself, not the checkout's `HEAD` — resolved to its commit sha first, so passing a mutable ref name (`refs/pull/213/head`, a branch, `HEAD`) records the immutable commit it named rather than a name that will point somewhere else after the next fetch. A cited path is looked up literally (`:(literal)` pathspec) — not to defeat globbing, since `git ls-tree` does not glob at all, but so that a filename starting with `:` names that file instead of being read as pathspec magic (without the prefix `ls-tree -- ':weird.go'` exits 0 with no output, which would be reported as "that path does not exist at that revision" for a perfectly correct cite). `--rev` applies to `--open` and `--settle` only; `--ratify` and `--reopen` never restamp, and are refused with it rather than ignoring it. A `--rev` this repository does not have is refused up front, naming the revision; a cite naming a directory is refused at a revision exactly as it is in the working tree. Echoes `Settled n4 (fixed)`. `--ratify --id n7` and `--reopen --id n7` are **human-only** transitions (ADR-0017 §6): when the review loop's coding agent settles a finding `rejected` with a note prefixed to mark that the agent, not a human, made the call, `--ratify` strips that prefix (the entry becomes an ordinary rejection) and `--reopen` returns the entry to open under the **same id** — original finding text kept, settle note dropped — so the next round re-raises it. `--ratify` fails on anything but an agent-prefixed rejection; `--reopen` fails on an already-open entry or an unknown id; both name the actual state so the caller sees why. Echoes `Ratified n7` / `Reopened n7`. |

**Review loop subcommand** (headless, multi-round AI review — see
[ADR-0017](decisions/ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md) and
[ADR-0018](decisions/ADR-0018-review-loop-refuses-the-default-branch.md)):

| Subcommand   | Args / Flags                                                                                                                                                           | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `review-run` | `--reviewer code\|document\|skill` (default `code`), `--note <text>`, `--base <commit-ish> --head <commit-ish> --journal <key> --report-dir <dir>` (required together) | Runs every reviewer model configured in `review.reviewers` sequentially, headless, through OpenCode (`opencode run --agent <a> --format json [-m <provider/model>]`), against the current branch — one invocation is one round; the round cap lives in the agent-side loop, not here. `--reviewer` picks which reviewer agent runs (the same three choices the `dg ws` `R` keybinding offers); every configured model runs that same reviewer. With `review.reviewers` unset, one run uses OpenCode's own default model. A model id repeated in the list runs **once**, in the position of its first entry: a second run of one model pays twice for one opinion, and in explicit-range mode (below) it would also overwrite that model's own report file while both output lines still named it, breaking the one-line-one-report pairing. `--note <text>` appends the caller's own words to the fixed review prompt for every reviewer this round, introduced as emphasis that explicitly does **not** narrow the review, so "focus on file A" cannot be read as "review only file A"; a present-but-blank `--note` is refused rather than dropped, since a silently ignored note is indistinguishable from one that was delivered. Prints **exactly one line per reviewer** (`<model, or "OpenCode default model"> → <outcome>`) and nothing else — no findings, no journal ids: a finding lives in the journal, and `review-notes` is what reads it (it also lists what is still open, which is where the agent-side loop gets the ids). An outcome is `APPROVE`, `REQUEST CHANGES`, or `NEEDS DISCUSSION` (parsed from the reviewer's last `**Status:**` line), `NO VERDICT` (the run finished but stated no verdict), `NO VERDICT(<reason>)` (the run wrote no report at all, and the reason is OpenCode's own words for why — its stderr, which is where a headless run records an auto-rejected permission and nowhere else, or failing that the last step's finish reason), or `ERROR(<reason>)` (the run itself failed — spawn failure, nonzero exit, timeout, or an OpenCode error event; never guessed from matching message text, since an unusable model and a wedged provider both surface the same generic OpenCode error). One reviewer failing never stops the ones after it. A reviewer whose attempt produced **no report at all** is launched **once more** inside the same round before its outcome is reported — never more than that, and never when the attempt wrote anything (a report with no verdict in it is still an opinion that was paid for). If the retry also produces no report, the **first** attempt's outcome is the one reported, so a retry that dies with nothing to say cannot erase a first failure that named a reason. The retry is announced on the progress stream, not in the printed outcome; worst case it doubles one reviewer's wall clock and spend, bounded at two runs of the 30-minute timeout ([ADR-0020](decisions/ADR-0020-a-reviewer-that-reports-nothing-is-retried-once.md)). Refuses to run on the default branch or a detached HEAD (both checked before any reviewer is launched, with an actionable `git switch -c <name>` error — a review diffs against the default branch and a journal is keyed by branch name, so neither state has anything to review or anywhere to record it; see [ADR-0018](decisions/ADR-0018-review-loop-refuses-the-default-branch.md)), and refuses a branch that changes nothing at all — no commits ahead of the default branch **and** a clean working tree (`git status --porcelain`, so untracked files count), checked the same way before any reviewer is launched. Uncommitted work alone is reviewable: the reviewers read the branch's working state, not just its commits ([ADR-0019](decisions/ADR-0019-a-review-covers-the-branch-working-state.md)). The branch is re-checked **after every reviewer**, not only at the start: a reviewer runs shell commands for minutes, and one that moves `HEAD` silently redirects the round's findings into another branch's journal (the journal is keyed by branch name) — including a `main` journal, the thing [ADR-0018](decisions/ADR-0018-review-loop-refuses-the-default-branch.md) exists to prevent. When that happens the round is **abandoned**: no further reviewer is launched, no verdict is printed (there is no way to tell whether the switch preceded the reviewer that just finished, so reporting its verdict would assert something the command cannot know), and the error names both branches, the reviewer that was running, the journal the findings actually went to, and the `git switch` back. `HEAD` is never put back automatically. Both halves of the nothing-to-review guard fail **open** — an unresolvable ahead/behind comparison or status check lets the round run, because the guard saves cost rather than protecting correctness. **Explicit-range mode** (`--base --head --journal --report-dir`, all four required together — a partial group is refused by name rather than silently downgraded to a branch review of unrelated code) reviews an arbitrary pair of commits instead of the checkout ([ADR-0023](decisions/ADR-0023-a-pr-review-targets-immutable-shas.md) §5). Both ends take any commit-ish and are resolved to immutable SHAs before a reviewer is launched — a commit this clone does not have is refused there and then, with `git fetch` named, rather than surfacing minutes later inside a reviewer's own tool calls — and `--base` must be the range's **merge base** when the range has to equal a pull request's diff (`pr-review-target` prints one). The prompt, not Go, carries the scope: it names the resolved range, tells the reviewer to take the diff from `devgeta task review-package <base> <head>` (`review-scope` and `branch-diff` read the checkout, so they are named as not applying), and states the journal key and the reviewed revision — the pair the reviewer agents' scoped-journal clause triggers on, so every `review-notes`/`review-note` call they make carries `--branch <key> --rev <sha>`. `--journal` keys the round's journal reads and its round-start snapshot, so findings are never filed under whatever branch happens to be checked out. In this mode **none of the three HEAD-dependent refusals applies, and neither does the after-every-reviewer branch re-check**: each guards an inference about the checkout that the flags supply outright, so a tick runs from the default branch, a detached HEAD, or an unrelated branch, and a `HEAD` that moves mid-round changes nothing the command asserts (it never reads `HEAD` at all). Unlike a branch review, range mode reviews the **immutable SHAs only** — the working tree is never part of the diff, and the prompt says so, because uncommitted files in the reader's checkout belong to other work ([ADR-0019](decisions/ADR-0019-a-review-covers-the-branch-working-state.md) does not carry over). Each run's **full report** — every severity, strengths, evidence — is written to `<report-dir>/<reviewer-agent>+<percent-encoded-model>.md` (the journal's own encoder, since a model id's `/` is a path separator; the two segments join on `+` because that is a byte the encoder can never emit, so no pair of names can ever spell another pair's filename and overwrite its report), and every output line gains a trailing report field: exactly two spaces, then `report: `, then the path — last on the line, so a parser reads it from the right rather than assuming an `ERROR(...)` outcome contains no colon. A run that produced no report writes no file and its field reads `report: none (the reviewer wrote no report)` verbatim; the field is always present so a caller parses one shape either way, and both the two-space prefix and that sentinel are a parsing contract the agent-side loop depends on (pinned by a literal-string test, not just by the constants). The persisted report is always the same attempt's text as the reported outcome, retry included. The directory is created up front, and one that cannot be created is refused before any reviewer starts; a write that fails mid-round stops the round rather than paying for reports with nowhere to go. Branch mode's output is unchanged — verdict lines with nothing appended. |

While a reviewer runs, progress goes to **stderr** as it happens: a line when the reviewer
starts, a **sampled heartbeat** while it works, and a closing line with the outcome,
elapsed time, tool count, and what the run cost as OpenCode reported it. The heartbeat
prints at most once every 30 seconds, on the next tool call after the interval has passed,
and carries the running counters plus the tool call that triggered it — e.g.
`[1/2]   ... 1m0.3s, 7 tools, $0.42 - read internal/x.go`. Every tool call is counted
whether or not it is printed, so the closing count is the whole run. The root `--verbose`
flag switches to one short line per tool call instead (`[1/2]   read internal/x.go`); that
was the default until a real round was measured at ~200 lines, which `/review-loop`
captures and pays tokens for every round. The lines are rendered from the `--format json`
event stream as it arrives (`CommandParams.OnStdoutLine`), not after the run, so a
multi-minute round reads as working rather than wedged. Sampling is driven by the tool
calls themselves, not a timer, so a reviewer that goes quiet prints nothing until it acts
again — the trade for keeping the reporter single-threaded. The reviewer's own prose is
never printed there — findings belong in the journal — and stdout is unaffected in either
mode, staying exactly the contract above.

Each reviewer this round reads the journal as it stood when the round began, not the live
file, so no reviewer is anchored by what a peer already opened or settled earlier in the
same round — every configured model is an independent sample of the branch rather than a
follow-up on the one before it (ADR-0017 §4). Writes are untouched: `review-note --open`
and `--settle` still hit the live journal immediately and get real, final ids. The
mechanism is a round-start snapshot: `review-run` copies the journal before launching
anyone (even when no journal exists yet), points each reviewer's `review-notes` at it with
a child-only environment variable, and removes the snapshot once the round ends. Every
other caller of `review-notes`/`review-note` — including a human running it by hand — sees
no pointer and reads the live journal exactly as before.

`configs/shared/commands/review-loop.md` (`/review-loop`) is the agent-side driver built on
top of `review-run`: it runs a round, verifies and settles each open finding with the
`receiving-code-review` skill's rigor, and moves through three phases in order — an
**opening round** that runs every reviewer configured in `review.reviewers`, **narrowing
rounds** that run only the reviewers that have not yet approved, and a **confirming round**
that runs every reviewer again. `review.rounds` caps rounds **per phase**, not the run as a
whole: the counter restarts at the start of each phase, so the narrowing phase alone can run
up to `review.rounds` rounds (default 3, max 5), and a full run is at most the cap plus 2
rounds — the one opening round and the one confirming round bracketing it.
Only the confirming round can produce a clean approval (every reviewer APPROVEs, the journal
lists nothing under `open:`, and no agent-authored rejection is still awaiting ratification);
an approval given during the opening round or a narrowing round is provisional, since the
branch can keep changing after it was given. The loop stops at one of exactly two outcomes: a
clean approval, or a report to the human (persistent disagreement, an open finding not yet
settled, every configured reviewer failing, a failure in the confirming round, or an
unratified rejection). Hitting the round cap is not a report trigger on its own: reaching
it ends the current phase, and only the confirming round's ending is the end of the loop. A
single reviewer failure does not by itself reach the report: it is retried and the reviewer
stays in the narrowing set; only two consecutive failures drop it from that set, naming it
failed with its last reason reported verbatim in whatever report the run eventually
produces.
Narrowing works by rewriting `review.reviewers` down to the still-blocking reviewers for one
round, then restoring the original list — after every single round, never left narrowed for a
whole phase. It refuses to narrow at all unless it has first proved it can restore the exact
list it found, and it prints that list and the restore command before the first narrowing
write, not only in the terminal report, so an interruption mid-run (Ctrl-C, a crash) still
finds the restore instruction already on screen. That does not close the gap itself: a
process that is gone prints nothing further, so an interrupted run leaves `review.reviewers`
narrowed until someone runs the printed restore command by hand — and re-running the loop
before that happens records the narrowed list as the new run's baseline, making the
narrowing permanent and silent.
It parses `--reviewer` and `--note` from its own arguments once, before the first round, and
forwards both to every `review-run` call it makes — the note verbatim, never summarized or
answered by the loop itself. It reads the open ids from `review-notes` after each round,
since `review-run` prints only verdicts.
When the coding agent judges a finding wrong, it settles it `rejected` with a note prefixed
`agent:` so the rejection is visibly provisional; the report carries every such rejection
with the exact `--ratify`/`--reopen` command to close it. The loop command itself never
runs either flag — that decision is the human's alone.

The journal lives at `<git common dir>/devgeta/review/<encoded-branch>.md` — the **common**
git directory, so the same branch shares one journal across the main checkout and every
linked worktree. Nothing there is ever committed or appears in a diff (which is also why no
`.gitignore` handling exists — ADR-0010 forbids editing one), and `dg wt remove` deletes the
journal in the same teardown that deletes the branch, so memory does not accumulate for work
that no longer exists.

What gets written is bound to severity, not to agent judgment: each reviewer opens an entry
for every `[CRITICAL]` and `[IMPORTANT]` finding as it writes its report, prints the returned
id inline beside that finding, and ends the report with the `--settle` line that closes them.
`[MINOR]`/`[Nit]` findings never enter the journal, and a finding already listed as open
keeps its id rather than gaining a duplicate. The earlier rule — open only what the reviewer
could not answer itself — recorded nothing in practice; see ADR-0012's amendment.

**Worktree lifecycle subcommands** (start/finish a git worktree in one call each —
same base path `dg wt` uses, `~/.local/share/devgeta/worktrees/<repo-slug>/<flat-name>`,
so `dg wt list` and worktrees created here are the same population, never two parallel
trackers):

| Subcommand        | Args / Flags                                       | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ----------------- | -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `worktree-start`  | `<name>`, `--base <ref>`                           | Refuse on a dirty tree, fetch origin, then create a worktree + branch at `dg wt`'s shared location. Without `--base`, the branch is based on the freshly-fetched default branch (reusing the same local/remote-branch-reuse logic as `dg wt create`); with `--base`, the branch starts fresh from exactly that ref. Prints `Created worktree <path> (branch <name>, base <ref>)`.                                                                                                                                                                                                                                                                                                                                                                      |
| `worktree-finish` | `[name]`, `--merge\|--discard\|--check`, `--force` | Tear down a worktree via merge, discard, or a read-only check — exactly one of `--merge`, `--discard`, or `--check` is required. Target resolution is deterministic: an explicit `name` wins; otherwise the current directory resolves to the linked worktree it's inside; otherwise the command errors and lists the worktrees it found — it never guesses from a main checkout. `--merge` refuses on a dirty worktree, refuses when the main checkout isn't on the default branch, refuses when the main checkout is dirty (any uncommitted changes there, not just paths overlapping the merge), refuses when the branch's review journal has an open, non-stale finding (settle it with `devgeta task review-note --settle --id <id> --as answered | rejected | fixed --note "<text>"`), and refuses when the divergence probe itself can't be answered (an unanswerable `git merge-base --is-ancestor`, e.g. no local branch by the default branch's name) — then rebases onto the default branch if diverged, fast-forward-merges from the main checkout, and removes the worktree and deletes the branch (safe only once the fast-forward landed the branch's commits). `--discard`refuses on a dirty worktree unless`--force`, then removes the worktree and deletes the branch unconditionally. Does not run a build or test suite — verification is the caller's responsibility. `--check`reports the same readiness`--merge`would act on, without acting: no fetch, no ref moved, and no mutation — except that a`git merge-tree`conflict prediction can write unreferenced objects to the object database, advisory-only and does not block. Prints dirty state, ahead/behind and rebase need, predicted merge conflicts, open review-journal findings, and changed docs' status markers, ending in a`ready: yes`or`ready: no — <reason>`line naming the first blocking refusal above (in the same order`--merge` checks them); exits non-zero when not ready. |

**Pull request subcommands** (via `gh`; data-returning ones are formatted by `jq`
into compact, LLM-oriented output — `gh` fetches/acts, `jq` renders):

| Subcommand              | Args / Flags                                                                                                                      | Description                                                                                                                                                                                                                                                                                                                                                                   |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `review-threads`        | `--pr N`, `--state unresolved\|resolved\|all`                                                                                     | Render PR review threads as compact markdown (default: unresolved)                                                                                                                                                                                                                                                                                                            |
| `resolve-thread`        | `<id>`                                                                                                                            | Mark a review thread resolved                                                                                                                                                                                                                                                                                                                                                 |
| `unresolve-thread`      | `<id>`                                                                                                                            | Reopen a resolved review thread                                                                                                                                                                                                                                                                                                                                               |
| `reply-thread`          | `<id> <body>`                                                                                                                     | Reply to a review thread                                                                                                                                                                                                                                                                                                                                                      |
| `create-pr`             | `--title` (req), `--body`, `--base`                                                                                               | Open a PR from the current branch; prints the URL                                                                                                                                                                                                                                                                                                                             |
| `update-pr-description` | `--pr N`, `--body` (req)                                                                                                          | Replace a PR's description                                                                                                                                                                                                                                                                                                                                                    |
| `submit-review`         | `--pr N`, `--event approve\|request-changes\|comment` (req), `--body`, `--body-file <f>`, `--comments-file <f>`, `--commit <sha>` | Post one review — verdict, optional Markdown body, optional inline comments anchored to diff lines — in a single REST submission. `--comments-file` is a JSON array of `{"path","line","body"}` (optionally `"start_line"`, `"side"`). A `request-changes` or `comment` review needs a body or inline comments; `approve` may have neither. `--commit` anchors it (see below) |
| `approve-pr`            | `--pr N`, `--body`, `--body-file <f>`, `--commit <sha>`                                                                           | Approve a PR. `--commit` anchors the approval (see below) and switches the call to the same REST reviews endpoint, because `gh pr review` cannot carry a commit id; without it the plain `gh` route is unchanged                                                                                                                                                              |
| `request-changes-pr`    | `--pr N`, `--body` (req)                                                                                                          | Request changes on a PR                                                                                                                                                                                                                                                                                                                                                       |
| `request-review`        | `--pr N`, `<reviewer>...` (req)                                                                                                   | Re-request review (adds reviewers back to the requested list)                                                                                                                                                                                                                                                                                                                 |
| `comment-pr`            | `--pr N`, `--body` (req)                                                                                                          | Post a top-level PR comment                                                                                                                                                                                                                                                                                                                                                   |
| `merge-pr`              | `--pr N`, `--method squash\|merge\|rebase`                                                                                        | Merge a PR (default: squash)                                                                                                                                                                                                                                                                                                                                                  |
| `pr-view`               | `--pr N`                                                                                                                          | Compact PR summary (number, title, state, mergeable, review, branch)                                                                                                                                                                                                                                                                                                          |
| `pr-checks`             | `--pr N`                                                                                                                          | CI check status, one line per check; failing checks get an indented log digest appended (see below)                                                                                                                                                                                                                                                                           |
| `pr-review-target`      | `--pr N`                                                                                                                          | Immutable review target for a PR: merge-base/head SHAs, journal key, noise-filtered file list (see below)                                                                                                                                                                                                                                                                     |
| `pr-review-state`       | `--pr N`                                                                                                                          | Whether a PR wants a review from you right now: `pr:` / `requested:` / `my-review:` (see below)                                                                                                                                                                                                                                                                               |
| `current-pr`            | —                                                                                                                                 | PR number for the current branch                                                                                                                                                                                                                                                                                                                                              |
| `current-repo`          | —                                                                                                                                 | Current repository as `owner/name`                                                                                                                                                                                                                                                                                                                                            |

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

**`pr-review-target`.** Resolves what a PR review looks at, as fixed commit
SHAs, for a PR that is usually not checked out. It fetches `refs/pull/<n>/head`
and the PR's base branch read-only into non-branch refs under `refs/devgeta/pr/<n>/`
(no local branch moves, nothing is checked out, the working tree is untouched),
then prints:

```
base: 9f2c1ab8bc0d1e2f3a4b5c6d7e8f90a1b2c3d4e5
head: 2f38a274cd0e1f2a3b4c5d6e7f8091a2b3c4d5e6
journal: pr/Employ-Inc/employ-agent/213
files:
- internal/tooling/task/pr.go
- docs/spec.md
```

- `base` is the **merge base** of the base branch and the head, not the base
  branch tip, so `git diff base..head` is exactly the diff GitHub shows. An
  endpoint range against a base branch that advanced after the PR opened would
  render every commit merged into it meanwhile as a reversal.
- `journal` is the PR-scoped review journal key (`pr/<owner>/<repo>/<n>`) passed
  to `review-notes`/`review-note --branch`, so a PR's review memory is never
  mixed with a checkout branch's.
- `files` is the range's changed files with lockfile-style noise filtered out
  (same exclusion list as `review-scope`); an `excluded (...)` receipt naming
  what was dropped follows it whenever anything was. An empty list prints
  `(none)`.
- **A failed fetch ends the command with an error** — it never falls back to
  whatever refs are on disk, because reviewing a stale ref produces a confident
  review of code the PR no longer contains.

The two fetched refs — `refs/devgeta/pr/<n>/head` and `.../base` — **stay in the
repository** after the command exits. That is the one durable trace it leaves,
and it is deliberate: later steps of the same review read them (`git show
<head>:<path>`), and holding the refs pins those objects against a `git gc` that
runs while reviewers are still working. They are keyed by PR number, so a
re-review of the same PR reuses them and the count tracks distinct PRs reviewed,
not reviews run. Remove them by hand with
`git update-ref -d refs/devgeta/pr/<n>/head` (and `.../base`).

See [ADR-0023](decisions/ADR-0023-a-pr-review-targets-immutable-shas.md).

**`pr-review-state`.** Reads one pull request's current review-request state —
the trigger a PR review loop decides from — and prints exactly three lines:

```
pr: open
requested: yes
my-review: none
```

- `pr` is `open`, `draft`, `merged`, or `closed`. `draft` is reported
  separately from `open` because a requested draft is still unfinished work; a
  caller that treats drafts as not-yet-reviewable needs to see it. A **closed
  draft reads `closed`**, not `draft` — it is over, not waiting.
- `requested` is whether the authenticated GitHub user (`gh api user`) is named
  in the PR's `reviewRequests` right now. A request addressed to a **team** that
  does not name the user is not a request from them and reads `no`
  ([ADR-0022](decisions/ADR-0022-pr-review-trigger-is-a-polled-state-read.md) §3).
- `my-review` is the state of that user's latest **submitted** review:
  `approved`, `changes-requested`, `commented`, or `none`. A review still being
  drafted is not submitted, and a dismissed approval reports `none` rather than
  `approved` — an approval GitHub has thrown away must not read as standing.
- An unrecognized PR state ends the command with an error instead of resolving
  to a value, because a caller acts on `pr:` and a guess there is a wrong action.

This is a **state read, not an event log**, and devgeta keeps no record of its
own. GitHub maintains all three facts: submitting any review — approve,
request-changes, or comment alike — removes the user from `reviewRequests`, and
the re-request button puts them back. So one field answers "is a review wanted",
"was it already answered", and "did the author ask again", with nothing local
that could go stale after a session dies or a review is submitted from another
machine. See
[ADR-0022](decisions/ADR-0022-pr-review-trigger-is-a-polled-state-read.md).

**`--commit <sha>` on `submit-review` and `approve-pr`.** Both put the sha in the
REST review payload's `commit_id`, so the posted review names the commit that was
actually read. This matters when the reviewed code is not the checkout — a PR
reviewed from an unrelated branch, where `pr-review-target` fixed the head SHA
minutes before the submit.

It is **attribution, not enforcement.** GitHub does not reject a review whose
`commit_id` is behind the PR's current head; this API has no atomic submit, so
nothing here stops an author pushing between the read and the post. What it buys is
that a review landing late is visibly stamped with the commit it judged instead of
silently claiming the new head: inline comments hang off the reviewed diff (GitHub
marks them outdated once the head moves), the review record carries the sha, and
branch protection's dismiss-stale-approvals has one to key off.

Passing it to `approve-pr` changes the route, not just the payload: `gh pr review
--approve` has no way to carry a commit id, so an anchored approval is delegated to
`submit-review`'s REST call, while an unanchored one keeps the existing `gh` route
byte-for-byte. The common case therefore gains no new failure modes — it needs
neither owner/repo resolution nor a payload file.

**`/pr-review-loop` — the PR review tick.**
`configs/shared/commands/pr-review-loop.md` is the agent-side driver that reviews one
pull request — now, when a human runs it, or on watch when a repeat driver fires it —
assembled from the three pieces above: `pr-review-state` for the trigger,
`pr-review-target` for the immutable review target, and `review-run`'s explicit-range
mode for the reviewers. It is the PR-side counterpart to `/review-loop` — the same
cross-model reviewers, the same journal — with three deliberate differences: it reviews
and posts, never fixes (that is the author's `/address-feedback`, or `/review-loop` on
their side); it has no round cap of its own, because a "round" here is triggered from
outside, so GitHub's re-request button is the round counter and `review.rounds` does not
apply; and it runs every configured reviewer on every tick, never narrowing to just the
reviewers still blocking the way `/review-loop`'s narrowing rounds do.

**A tick is explicit or watch, told apart by a flag and not by inference**
([ADR-0025](decisions/ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md)).
An **explicit tick** is one a human typed: running the command is itself the request —
addressed to this tool, by the person running it, about the PR they named — so it
reviews unless the pull request is over. A **watch tick** is one a repeat driver fired,
marked by `--on-request`, which only the driver's own handoff writes and no human ever
types; it stays gated on GitHub's own state exactly as before
([ADR-0022](decisions/ADR-0022-pr-review-trigger-is-a-polled-state-read.md)): presence
in the PR's `reviewRequests` is its entire trigger, and submitting any review removes
the user from it. Nothing is stored between ticks either way. That makes a watch tick
safe to run twice, by hand or after a crash; an explicit tick is deliberately not
idempotent instead — typing the command again is asking again, so it reviews and posts
again.

```
/pr-review-loop [PR_NUMBER] [code|document|skill ...] [--reviewer <type>] [--note <text>] [--once] [--on-request]
```

`PR_NUMBER` is optional and resolves from the current branch's PR (`current-pr`) when
omitted — pass it for the normal case of reviewing someone else's PR, whose branch is
not the checkout. The PR does not have to be checked out at all: every step reads the
fetched refs, so no branch is created, switched, stashed, or committed and the working
tree is never the source. That matters more here than usual, because a tick can fire
while the human is mid-edit in the same clone.

The bare words are reviewer types, and more than one is allowed — `code document` is
two reviewer runs, one per type, each covering every configured model internally. The
three values are exactly `code`, `document`, and `skill`, the keys
`review-run --reviewer` validates against, forwarded with no translation. **There is
no `doc` shorthand:** an unknown type stops the tick before any state is read rather
than being mapped onto a near-miss, because a friendlier second vocabulary here could
drift from the one the runner validates against. Types omitted is normal, not an
error — they are judged from the target's `files:` list. `--reviewer <type>` and
`--reviewer=<type>` are a second spelling of those same three values, accepted because
it is the flag the sibling `/review-loop` takes and therefore what a human moving
between the two actually types; the spellings mix freely and mean exactly the same
thing (`code --reviewer=document` is two runs), and nothing is translated on the way
through, so `doc` is still an error however it is written. `--note <text>` is the
human's own emphasis, forwarded verbatim to every reviewer run of the tick; it adds
context and never narrows what gets reviewed, the same contract `/review-loop --note`
has.

`--once` reviews and starts no repeat driver — a single look, after which nothing
watches the PR; that used to be what every bare invocation did. `--on-request` marks a
tick a repeat driver fired; no human ever types it. It does exactly two things: it
makes the tick request-gated, so it reviews only when GitHub says a review is asked of
the user, and it stops that tick from starting a driver of its own
([ADR-0025 §5](decisions/ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md)).

**One invocation is one tick.** Repetition belongs to the driver, not to this file, but
starting it is part of the job — and that happens at the **end** of the tick, after the
outcome is known, not at the start. A repeat driver fires at its next scheduled match and
never on creation, so handing off first would answer the human who just asked for a
review with a state read now and the review a whole interval later
([ADR-0025 §4](decisions/ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md)).
An explicit tick whose outcome leaves the PR still worth watching starts the harness's own
repeat driver on itself: on Claude Code,
`/loop <interval> /pr-review-loop <n> [types] [--note <text>] --on-request`, carrying
every argument verbatim plus `--on-request`, which marks every tick that driver fires as
a watch tick; the interval is the one the human named, or the driver's default. Nothing
is started on a terminal exit (approved, closed, or escalated) or when `--once` was
passed — the watch is over, or was never asked for — and a watch tick never hands off at
all, because the driver that fired it is already running and a handoff there would start
a second one on every tick, each starting another. Where the harness has no repeat driver
(OpenCode), the handoff is impossible rather than optional: the tick still reviews, and
the report says plainly that nothing will run another. Either way the review runs at most
once per tick and at most one review is posted per tick. Running the command **is** the
authorization for the whole tick — the state read, the fetch, the reviewer runs, the
posting step, and the handoff itself — so the tick never pauses to ask whether to post or
whether to start the driver; a watch that stops for a go-ahead each interval costs
exactly the attention it exists to save.

The state read plus the tick's mode selects **exactly one** row of this table,
evaluated top to bottom, first match wins — read `requested:` off the matched row
rather than checking it separately, since that is what keeps the two request states
apart: an author re-requesting a review on a PR this user already approved lands on
the `requested: yes` row, never the standing-approval one below it.

| `pr:`             | `requested:` | `my-review:`  | Explicit tick         | Watch tick (`--on-request`)                        |
| ----------------- | ------------ | ------------- | --------------------- | -------------------------------------------------- |
| `merged`/`closed` | any          | any           | **Terminal: closed.** | **Terminal: closed.**                              |
| `draft`           | any          | any           | **Review**            | Wait — a formal review on unfinished work is noise |
| `open`            | `yes`        | any           | **Review**            | **Review**                                         |
| `open`            | `no`         | `approved`    | **Review**            | **Terminal: approved.**                            |
| `open`            | `no`         | anything else | **Review**            | Wait — the ball is with the author                 |

**An explicit tick reviews unless the pull request is over** — only `merged` and
`closed` stop it, because the human typed the command and that is a request already,
addressed to this tool about the PR they named; gating it on the GitHub field would
make this file ask for a permission it was just handed. The two rows it overrides
exist to protect the **author** from a review they did not ask to receive, and
neither reason survives a human asking on purpose: a draft is exactly when an author
wants a private read of unfinished work, and "review it again" is a normal ask after
a rebase or a late doubt
([ADR-0025 §2](decisions/ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md)).

**A watch tick takes the rows as written, unchanged from before**
([ADR-0025 §3](decisions/ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md)):
draft waits, no request waits, a standing approval is terminal. That is what keeps an
unattended watch from reposting — submitting any review removes the user from the
request list, so the tick after a post reads `requested: no` and waits, and the
author's re-request is the next trigger. Draft is still checked before the request
state, so a requested draft waits on a watch tick even though it reviews on an
explicit one. Most watch ticks land on a wait row, which is why they are cheap — no
fetch, no reviewer, nothing posted.

Two consequences of reading current state rather than a local log look like bugs and
are neither. A colleague who answers the request first removes the user from the
request list, so the next **watch** tick simply waits. And a dismissed approval
reports `my-review: none`, so a watch keeps going on an approval GitHub already threw
away instead of stopping on it.

The **review** action, in order — nothing in it reads the working tree. (Numbered 1–7
here, condensed from the command file's own steps 3–9: step 1 below is its step 3, and
from step 4 below on the file's number is two higher — the aggregation is its step 6,
the pre-post re-check its step 7.)

1. `pr-review-target --pr <n>` gives the `base`/`head` SHAs, the `journal:` key, and
   the `files:` list. Its failure ends the tick with that error; there is no fallback
   to the refs already on disk and none to the checkout. Every later step reuses these
   values rather than re-resolving a ref name itself, because a review takes minutes
   and a name resolved twice inside that window can mean two different commits — step 5
   resolves the head again deliberately, to catch exactly that. `pr-view` then supplies
   the PR's purpose and linked ticket, read before any code.
2. Types are fixed — the ones passed, else judged from `files:` (`code` for code,
   `document` for docs and prose, `skill` for agent skills and commands; a mixed PR
   takes the matching set). `files: (none)` — an empty range, or one entirely filtered
   as noise — ends the tick as `nothing to review`: no reviewer, nothing posted, refs
   kept. It is its own status word rather than a wait, because nothing is pending on
   anyone; any review request on the PR stays pending, so the tick report is what tells
   the human to look themselves.
3. One `review-run` per type, in range mode:
   `review-run --reviewer <type> --base <base> --head <head> --journal <key> --report-dir "$PR_REVIEW_SCRATCH"`
   (plus `--note` when one was given), against a `scratch` directory allocated for
   this tick and named distinctly from `/review-pr`'s own scratch variable, so the two
   can never be confused with each other in the same session. The runs happen in the main session, not a subagent — the verdict lines
   are the one thing a tick must never take second-hand — and each line's `report:`
   field is parsed from the **right**, since a reason inside `ERROR(<reason>)` can
   contain that same sequence.
4. **Aggregate every run's verdict once,** across every type times every model. Any
   single `ERROR(<reason>)` or `NO VERDICT` → **terminal: escalated**, naming the
   failing run and its reason verbatim: never approve on a run that did not complete,
   and never re-run it, because `review-run` already relaunched a reviewer that
   produced no report once inside the same run. Every run `APPROVE` → the approval
   path. Anything else → the review path. One blocking outcome from one run is enough;
   the runs are independent opinions, not votes to tally.
5. **Re-check the state and the head immediately before posting** — the reviewer runs
   took minutes, and this check is mode-aware. An **explicit tick** only needs `pr:` to
   still be neither `merged` nor `closed` — `requested: no` cannot cancel the post,
   since on most explicit ticks it was never `yes`, and requiring it would run the
   whole cross-model review and then post nothing. A **watch tick** still needs the
   fresh state to land on the Review row (`open` and `requested: yes`), or the world
   moved (merged, closed, went draft, or someone else answered) and posting now would
   be duplicate or unsolicited; either way, failing takes the row the fresh state
   selects for this tick's mode instead. In both modes, `pr-review-target`'s `head`
   must still equal the sha the reviewers read, or the author pushed mid-review and an
   approval would cover commits no reviewer saw. **Either condition failing means post
   nothing**
   ([ADR-0025 §6](decisions/ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md)).
   On the watch side the request is still pending, so the next tick reviews the new
   head from scratch — an author pushing repeatedly defers the review, which is the
   correct outcome rather than starvation.
6. **Post through the unchanged posting commands,** exactly one of them, exactly once:
   `/approve-pr <n> --target <head>` when every run approved, otherwise
   `/review-pr <n> --base <base> --target <head> --journal <key>` — both SHAs and the
   journal key from step 1, so the posted review's diff is the same merge-base range
   the reviewers read, and any finding `/review-pr` settles lands in this PR's journal
   rather than the checked-out branch's. `/approve-pr` never reads the journal, so it
   gets no key. Before `/review-pr` the tick reads every `report:` file and
   `review-notes --branch <key> --rev <head>`, because the reports carry the full
   cross-model findings (every severity, the strengths, the evidence) while the journal
   carries only the blocking entries as one-liners — a review composed from the
   one-liners alone throws most of what was found away.
7. **The approval path has one outcome to read.** `/approve-pr` prints
   `## PR #<num> — <approved | not approved>`. `approved` → **terminal: approved**, and
   the loop stops there, including on the very first trigger; it never keeps listening
   past an approval it posted. `not approved` → **one re-ask, approve-only** — this
   branch is only reachable when every run said `APPROVE`, which is exactly the basis
   `approve-pr.md` names for approving over live non-blocking comments, so the tick
   invokes it once more stating that verdict and expecting an approval whose body is
   `LGWC; <who/what remains>`, naming the leftover non-blocking comments — not a
   re-review, and not a comment. Approved on the re-ask → terminal: approved; still not approved → **terminal:
   escalated**, since it is standing on a blocker every run missed. **Never a third
   ask** bounds the asking in either mode: nothing in the PR's state stops it, and on a
   watch tick a decline leaves `requested:` at `yes`, so asking forever would post forever.
   The review path has nothing to parse — posting any review removes the user from the
   request list, so the next **watch** tick waits and the author's re-request is the next trigger.

**Cleanup is two things with two different scopes,** and the first runs far more often
than the second. The **scratch directory** goes on every completed exit of the review
action — approved, escalated, the head moved or the state changed at step 5, a review
posted, or a submit that failed (a failed submit prints the review into the tick report
_before_ cleaning, so nothing is lost). The **fetched refs**
(`refs/devgeta/pr/<n>/head` and `.../base`) go only on the three exits that are
terminal for the loop — approved, closed, or escalated — including a terminal row
reached straight from the table, where a previous tick may have left them. They cannot
go earlier: every step from the target read onward reads them, and holding them is what
keeps a concurrent `git gc` from collecting the commits under review. A non-terminal
exit keeps them, because the next tick reviews the same PR. Neither cleanup can cover
the process being killed mid-tick — a dead process runs no cleanup — and the two
leftovers are bounded differently. Every tick allocates its **own** scratch directory
(`os.MkdirTemp` under the scratch root), so ten killed ticks leave ten of them; what
bounds those is age, not the PR — the stale-directory prune drops an allocation older
than 24 hours, and it runs from both agents' `ForceConfigure`, meaning
`dg configure --force` or a first install, never from a `SoftConfigure` that finds its
marker file. The **refs** are keyed by PR number and reused every tick, so a killed
tick leaks one pair (`git update-ref -d`) and the leftovers are bounded by the number
of distinct PRs reviewed, not by ticks.

The tick reports in at most three lines under one status word —
`waiting | nothing to review | reviewed | approved | closed | escalated | head moved`
— and on a terminal exit says the watch is over explicitly, because stopping the driver
is the human's or the harness's action, not the command's. It never edits code, never
resolves threads, never re-requests reviewers, and never settles a finding it did not go
through `/review-pr` to settle.

**One accepted risk, decided rather than overlooked.** The aggregation in step 4 reads
reviewer verdicts only. Neither it nor `/approve-pr` reads the PR's journal, so an
approval can be posted while findings from an earlier round are still open **at the same
reviewed commit**: round 1 posts `REQUEST CHANGES` and opens findings, the author
re-requests with no new commits, and reviewer non-determinism swings round 2 to
all-`APPROVE`. The sibling `/review-loop` guards exactly this shape
(`TestReviewLoopCleanApprovalRequiresNothingOpen`, from a real incident); this loop
deliberately does not. That asymmetry was raised in review and **adjudicated by the
maintainer (2026-08-07) as ship-as-is** — a known, documented risk, not a defect, and
not to be "fixed" without asking again.

**`--target` and `--base` on the posting commands.** `/review-pr` and `/approve-pr`
gained the flags this loop needs, and **without them both files behave word-for-word as
they always have** — the working tree is the source and nothing changes:

```
/review-pr  [PR_NUMBER] [--base <merge-base-sha>] [--target <head-sha>] [--journal <key>]
/approve-pr [PR_NUMBER] [--target <head-sha>]
```

`--target` names the commit the review judges, for code that is not in the working
tree. With it, four things change and nothing else: the sha is resolved first with
`git rev-parse --verify <head-sha>^{commit}` and **a failure stops the command** —
never a silent fall back to the working tree, which holds different code, so a finding
would be checked against something the PR never contained; every read of repo content
resolves at that commit — `git show <head-sha>:<path>` in both, plus
`git log <head-sha> -- <path>` where `/review-pr` needs a path's own history — with the
same checks, dedup rules, and verdict rules as before; the submit passes
`--commit <head-sha>`, which is the attribution described above and not a lock; and,
given `--journal <key>`, `/review-pr` appends `--branch <key> --rev <head-sha>` to
every `review-notes`/`review-note` call it makes, settling included — without a key,
a settle falls back to the checked-out branch's journal, where the same numeric id
means an unrelated finding on an unrelated branch. `/approve-pr` never touches the
journal, so it takes no `--journal` and needs none; a PR number must be in hand
whenever `--target` is given, since an omitted one no longer infers from the checkout
(that branch is not this PR) — it silently posts against whatever PR the checkout
happens to have open, so `/approve-pr` stops and asks for one rather than guessing.

`/review-pr` takes `--base` alongside `--target` because it reads a diff and a diff
needs both ends — a merge base cannot be worked out from a head alone. **Given
`--target` without `--base` it stops and says it needs the base sha**, guessing neither
that nor the checked-out branch's diff. `--base` must be the PR's **merge base**, not
the base branch's tip: once the base branch moves on after the PR opens, a tip-based
diff renders everything merged since as if this PR reverted it, and those read as real
findings against work the author never touched. `pr-review-target`'s `base:` line
already prints a merge base. With `--target`, `review-scope` and `branch-diff` do not
apply at all — both describe the checked-out branch — so the diff comes from
`review-package <base-sha> <head-sha>`, which is also what says which lines can carry
an inline comment. `/approve-pr` never reads a diff, so it takes `--target` alone.

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

**Scratch directory subcommand** (a devgeta-owned working directory instead of
`/tmp` — see [ADR-0015](decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md)):

| Subcommand | Args / Flags     | Description                                                                                                                  |
| ---------- | ---------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `scratch`  | `--clean <path>` | Bare call allocates a fresh directory under the scratch root and prints its path; `--clean` removes one previously allocated |

Bare `dg task scratch` calls `paths.EnsureScratchDir()` — creating
`~/.cache/devgeta/scratch` (honoring `XDG_CACHE_HOME`) at mode `0700` if
absent, rather than assuming a prior `dg configure` did — then
`os.MkdirTemp`s a `task-*`-prefixed subdirectory under it and prints its
absolute path on one line.

`--clean <path>` accepts only a real directory that is a direct child of the
scratch root and carries the `task-` prefix: the root itself, a grandchild, a
directory beside the root, an unprefixed child, a relative path, a `..`
escape, and **any** symlink (resolvable or not) are all refused rather than
silently widening what gets deleted. Bounds are checked lexically first, so
an out-of-bounds path is an error whether or not it exists, then re-checked
after symlink resolution to catch a symlinked ancestor. It is idempotent — an
in-bounds path that is already gone succeeds — so a command's cleanup can run
on failure paths and retries. Pass it the exact path `scratch` printed; a
reconstructed or parent path is refused by design. Not enforced: a session
cleaning a sibling session's directory by guessing its random suffix — the
only parties able to do that are the same user's own agent sessions, so
ownership isolation stops there deliberately.

The same `task-` ownership rule bounds the stale-directory prune that
`dg configure --force` runs: anything a user keeps under the granted scratch
root is left alone regardless of age.

This is what `/review-pr` and `/create-pr` allocate their scratch files
under instead of `/tmp` — see
[docs/apps/claude.md](apps/claude.md#permissions-model) for why `/tmp` would
otherwise prompt on every run.

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
