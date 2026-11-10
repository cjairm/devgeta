# Claude Code (`claude`)

Devgeta installs [Claude Code](https://claude.com/claude-code), Anthropic's
terminal AI CLI, as a first-class terminal tool and deploys a curated config to
`~/.claude/`.

- **Module:** `internal/apps/claude/`
- **Config source:** `configs/claude/`
- **Install:** official script (`https://claude.ai/install.sh`), pinned to
  `constants.PinnedVersion.Claude` (2.1.319)

## Pinned version

devgeta installs exactly `constants.PinnedVersion.Claude`: the install script
takes the version as its first argument. `settings.json` sets
`DISABLE_AUTOUPDATER=1` in its `env` block, so Claude Code does not update
itself past the version devgeta's config was checked against. That variable
also stops automatic plugin updates.

`dg install` reinstalls when `claude --version` is older than the pin, or
unreadable. A newer version you installed yourself is left alone. Moving the
pin is a devgeta change: bump the constant, read the release notes against
`settings.json` and the hooks, and release.

## Using it

Start it with `cc`. Useful slash commands inside the session:

- `/help` — help with Claude Code features
- `/config` — settings (model, theme, permissions)
- `/fast` — toggle fast mode (Opus with faster output)
- `/loop` — run a prompt or command on a recurring interval
- `/skills` — list available skills
- `/init` — initialize CLAUDE.md for your project
- `/review` — review a pull request

Settings, hooks and themes land in `~/.claude/` — see the deployment table
below for exactly what goes where. `dg configure` ships no skills, commands,
agents or instructions file. Those come only from [agent packs](agent-packs.md),
which you install with `dg agent-pack` (and `dg install` for a coder it
installs). The one pack that touches `~/.claude/CLAUDE.md` is `devgeta`, and only
to add an `@DEVGETA.md` line when no line there already imports it.

**Resetting chat history.** Message/input history and past chats are separate:

```bash
rm ~/.claude/history.jsonl   # message/input history
rm -rf ~/.claude/sessions ~/.claude/projects ~/.claude/tasks   # chats
```

Restart Claude Code afterwards. Note that `~/.claude/projects/<slug>/memory/`
lives under that second path — deleting it discards the agent's memory notes
for those projects along with the chats.

## What gets deployed

`ForceConfigure` copies the following into `~/.claude/`:

| Source                                 | Destination                        | Notes                                            |
| -------------------------------------- | ---------------------------------- | ------------------------------------------------ |
| `configs/claude/settings.json.tmpl`    | `~/.claude/settings.json`          | rendered template: theme, permissions, hooks     |
| `configs/claude/statusline.sh`         | `~/.claude/statusline.sh`          | `chmod 0755`                                     |
| `configs/claude/format.sh`             | `~/.claude/format.sh`              | `chmod 0755`                                     |
| `configs/devgeta/format-file.sh`       | `~/.config/devgeta/format-file.sh` | `chmod 0755`; the formatter table the hook calls |
| `configs/claude/agent-config-guard.sh` | `~/.claude/agent-config-guard.sh`  | `chmod 0755`                                     |
| `configs/claude/agent-state.sh`        | `~/.claude/agent-state.sh`         | `chmod 0755`                                     |
| `configs/claude/themes/`               | `~/.claude/themes/`                |                                                  |

`settings.json` is rendered from a template (theme, scratch directory, and the
agent packs), so a `--force` re-render rewrites the whole file: hand edits to it
are lost.

## Agent packs

[Agent packs](agent-packs.md) are installed as plugins of a local marketplace
named `devgeta`, whose folder is `~/.local/share/devgeta/agent-packs/`. `dg
agent-pack install <pack>` runs `claude plugin marketplace add` and `claude
plugin install <pack>@devgeta -s user`, so `claude plugin list` and `/plugin`
show them.

`settings.json` carries them as `extraKnownMarketplaces` and `enabledPlugins`.
Both keys are rendered from `agent_packs:` in `global_config.yaml`, so
`dg configure claude --force` keeps every pack enabled instead of silently
disabling them. A pack for a coder that isn't Claude Code adds nothing here.

The `devgeta` pack is a plugin (it carries skills) and also carries
instructions, which a plugin can't: a copy at `~/.claude/DEVGETA.md` plus an
`@DEVGETA.md` line in `~/.claude/CLAUDE.md`, added only if missing and removed
on uninstall only if devgeta added it and it is still exactly that line.
Uninstalling the pack removes the plugin and both of those.

`dg configure` does not delete scripts it no longer ships. See
[the rollback migration note](../migrations/agent-customizations-rollback.md)
for the old hook files to remove by hand.

## Permissions model

`settings.json` ships a small floor ([ADR-0067](../decisions/ADR-0067-agent-permission-floor.md))
on top of a broad allow. Rules are evaluated deny → ask → allow (first match
wins):

- **allow** — `Bash(*)`, `Read`, `Edit`: day-to-day work never prompts.
- **ask** — `git push --force*`, `git push -f*`, `sudo *`, `gh repo delete *`,
  `gh release delete *`: irreversible, or they act outside the machine.
- **deny** — reads of secret and credential files (`.env`, `*.pem`, `*.key`,
  SSH keys, `.aws`, `.kube`, gcloud and Azure config, `.npmrc`, `.pypirc`,
  `.netrc`, `.git-credentials`, Docker config, `.pgpass`) and shell history,
  plus edits under `.git/`. `Edit(path)` rules cover all file-editing tools
  (Edit, Write, NotebookEdit); `Write(path)` rules are not matched by file
  permission checks, so they must not be used.

Nothing else is denied: `curl`, `rm -rf`, cloud CLIs and interpreter one-liners
all run. A prefix-matching Bash deny is skipped by `command curl` or
`bash -c '…'`, so it costs turns without stopping anything. Agent config is
protected by the agent-config guard below, not by path denies.

Deny and ask rules apply in **every** permission mode, including
`bypassPermissions` (`claude --dangerously-skip-permissions`).

**`permissions.additionalDirectories` grants one scratch directory** —
`paths.EnsureScratchDir()`'s path (`~/.cache/devgeta/scratch` by default,
honoring `XDG_CACHE_HOME`), rendered into `settings.json` at configure time.
`devgeta task scratch` allocates a unique subdirectory under it for working
files outside the project directory instead of writing to `/tmp`, which this
agent would otherwise prompt on. See [ADR-0015](../decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md).

**Known limits:** nothing checks an agent's commits for secrets, and Bash
rules are prefix matchers (see
[agent-permission-matching.md §5](../guides/agent-permission-matching.md)). The
agent-config guard intercepts only Edit/Write, so `echo … > .claude/settings.json`
reaches the same files through Bash. For a real boundary on high-autonomy work,
enable Claude Code's built-in OS sandbox (`/sandbox`; Seatbelt on macOS), which
enforces filesystem and network limits at the kernel level.

## Formatting & linting (PostToolUse hook)

`settings.json` registers a single `PostToolUse` hook on `Edit|Write` that runs
`~/.claude/format.sh`, a thin wrapper around the agent-neutral
`~/.config/devgeta/format-file.sh` that Pi uses too (ADR-0063). The wrapper
reads the hook payload, skips one grandfathered HTML path, and wraps the
script's findings in hook JSON. After Claude edits a file, the pair:

1. Reads the edited path from the hook payload on stdin (`.tool_input.file_path`).
2. **Formats** the file in place with the tools matching its extension.
3. **Lints** the formatted result and feeds any findings back to Claude as
   `hookSpecificOutput.additionalContext` (non-blocking — Claude self-corrects on
   its next turn).

> **Dependency: neovim/Mason.** The hook deliberately **reuses the formatter and
> linter binaries that neovim installs via Mason** at
> `~/.local/share/nvim/mason/bin`, rather than installing a separate toolchain.
> This keeps one source of truth for tool versions across the editor and Claude.
> The trade-off: if neovim (and its Mason tools) is not installed via devgeta,
> `format.sh` silently no-ops — each tool is guarded by an executable check, so a
> missing binary is skipped, not an error.

| Extension                                 | Formatters                  | Linter (→ Claude) |
| ----------------------------------------- | --------------------------- | ----------------- |
| `.js .jsx .ts .tsx .mjs .cjs`             | eslint_d --fix, prettier    | eslint_d          |
| `.py`                                     | isort, black                | flake8            |
| `.go`                                     | goimports, gofumpt, golines | golangci-lint     |
| `.md .markdown`                           | prettier                    | —                 |
| `.json .css .scss .less .html .yaml .yml` | prettier                    | —                 |
| `.lua`                                    | stylua                      | —                 |
| `.sh .bash`                               | shfmt                       | —                 |

Notes:

- The hook prints **only** JSON to stdout (Claude parses stdout for JSON on exit
  0); all formatter/linter chatter is routed to `/dev/null`.
- `golangci-lint` adds a few seconds of latency per `.go` edit (longer on first
  run while it builds its cache).
- `eslint_d` on a project without an ESLint config will report a config error as
  a "finding"; this is expected outside configured JS/TS repos.
- prettier is the exception to "Mason's binaries": the nearest
  `node_modules/.bin/prettier` above the file wins, and Mason's copy is only the
  fallback. A project's CI lints against its own pinned prettier, and prettier 2
  and 3 lay out the same code differently. OpenCode's `formatter` block does the
  same lookup.

## Agent-config guard (PreToolUse hook)

`settings.json` registers `~/.claude/agent-config-guard.sh` under an
`Edit|Write` `PreToolUse` matcher. It denies an edit that would
modify an agent's own configuration — permissions, hooks, agent/command/skill
definitions, or plugins — rather than the source code it was asked to work
on: an agent that can rewrite its own permissions can grant itself any
permission the settings withhold.

It replaces the blanket `Edit(.claude/**)` / `Edit(.opencode/**)` denies that
used to carry this rule. Those matched a directory of that name **at any
depth**, so every source file inside an in-repo worktree
(`<repo>/.claude/worktrees/<name>/` — `claude --worktree`, or devgeta's own
`worktree.location=in-repo`) was wrongly denied too, and deny always wins over
a same-scope allow with no specificity tiebreak, so a `.claude/worktrees/**`
allow carve-out could not fix it. A hook can express the exception a
permission rule cannot:

1. a `.claude` path segment **not** immediately followed by either
   `worktrees`, or `projects/<slug>/memory/` with a file below it — that
   directory holds the agent's own memory notes, which grant no permission,
   register no hook, and define no agent, so denying it blocked a feature and
   protected nothing. The rest of `projects/<slug>/` stays denied, and a
   `.claude` segment appearing again below `memory/` is judged on its own;
2. **any** `.opencode` path segment — no exception, since nothing creates
   `.opencode/worktrees/`;
3. a path under OpenCode's resolved global config root
   (`${XDG_CONFIG_HOME:-$HOME/.config}/opencode`) or under
   `$OPENCODE_CONFIG_DIR` when set — both verified additive, not relocating,
   against the installed OpenCode binary (`opencode debug paths`), so this
   guard (deployed to the default root) still loads and can enforce them;
4. the file named by `$OPENCODE_CONFIG` when set, resolved against every
   candidate base — OpenCode's own resolution base for a relative value could
   not be established from outside the binary, so the guard checks all of
   them rather than guessing one.

`$CLAUDE_CONFIG_DIR` is **not** handled: devgeta deploys this very hook to a
fixed `~/.claude/`, and `settings.json.tmpl`'s hook commands are themselves
literal `~/.claude/*.sh` paths, so under a relocated Claude root this hook —
along with every other Claude integration devgeta ships — simply does not
run. That is a devgeta-deployment limit, not something a clause here could
fix.

**Canonicalization is the guard's load-bearing step.** The target path is
made absolute against the payload's `cwd`, lexically cleaned of `.`/`..`
(always succeeds), then symlinks are resolved on the deepest **existing**
ancestor with the not-yet-existing tail re-appended — a `Write` target
usually doesn't exist yet. Without this, `.claude/worktrees/../agents/x.md`
would pass a naive segment walk, and a symlink under `worktrees/` pointing at
`.claude/agents/` would put a denied directory behind an allowed-looking
path. Symlink resolution is bounded (16 hops) against a self-referential
loop; on any resolution failure the guard falls back to the lexically
cleaned path rather than skipping the check — the one place this guard
family's fail-open stance does not apply, so a `..` escape is closed
unconditionally regardless of filesystem state.

The guard is the only mechanism for this rule: devgeta ships no `Edit(...)`
path denies for agent config, only `Edit(.git/**)` (see
[ADR-0067](../decisions/ADR-0067-agent-permission-floor.md)). The guard
fails open when `jq` is missing or the payload is malformed, so it is a
guardrail, not a boundary.

**Scope:** GLOBAL — it replaces a rule that was already global, and gating it
to the devgeta repo would silently remove escalation protection from every
other project on the machine.

**Bypass:** export `DEVGETA_SKIP_AGENT_CONFIG_GUARD=1` in the shell that
launches this agent (e.g. the repo's `.envrc`), BEFORE invoking the agent —
not inside the command — because the hook reads its own environment.

The OpenCode plugin equivalent
(`~/.config/opencode/plugins/devgeta-agent-config-guard/server.js`) mirrors the
same four-clause rule and the same bypass variable, though under OpenCode's
shared background service the variable has to be set on the service (see
[opencode.md](opencode.md)). It does not
hand-roll the bounded symlink chase the bash version needs: `fs.realpathSync`
already resolves an existing path's full symlink chain (with the OS's own
loop detection) in one call, so only the "resolve as much of a not-yet-existing
path as exists" step is written by hand, matching the bash version's shape.

See [ADR-0014](../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)
for the full design and the table of canonicalization test cases.

## Agent activity state (SessionStart / SessionEnd / Stop / UserPromptSubmit / PostToolUse / Notification hooks)

`settings.json` registers six hooks that report this coder's activity into
the tmux pane it's running in, so `dg ws`'s status dot, its agents section
(ADR-0056), and tmux's status bar can show working / finished / blocked
without switching to the window:

| Hook               | Matcher                       | Writes                                       |
| ------------------ | ----------------------------- | -------------------------------------------- |
| `SessionStart`     | none — fires once at start    | `@dg_agent_kind claude` only                 |
| `Stop`             | none — fires every turn end   | `idle`                                       |
| `UserPromptSubmit` | none — fires every turn start | `busy`                                       |
| `PostToolUse`      | none — fires after every tool | `busy`, unless already `busy` (`resume`)     |
| `Notification`     | `permission_prompt`           | `blocked`                                    |
| `SessionEnd`       | none — fires once at end      | unsets kind and state, recomputes the mirror |

All six run `~/.claude/agent-state.sh <value>` (`start`/`end`/`resume` for the
newer ones). `resume` exists because Claude Code has no "permission prompt
answered" event: after you answer one, a tool finishing is the first sign the
turn is running again. Without it, a pane whose `blocked` was cleared by
looking at it read as idle until the turn ended. It reads the state first, so
the usual case (already `busy`) costs one tmux call per tool. Unlike `format.sh`, this script reads no data
from the hook's JSON stdin payload — which value to write is fully determined
by which hook fired, passed as `$1`. It no-ops silently when `$TMUX_PANE` is
unset (Claude Code run outside tmux) and swallows any `tmux` failure, the same
no-op contract the other hooks use. `Notification`'s matcher is narrowed
specifically to `permission_prompt` so it excludes `idle_prompt` and
Notification's other types — an unfiltered `Notification` hook would map every
notification onto `blocked`, which is wrong.

`agent-state.sh` also writes/clears a window-level mirror
(`@dg_window_agent_state`) alongside its pane-level write, the same way the
OpenCode plugin does — this is what lets tmux's own status bar flag a window
nobody is looking at, since the status bar can't read one pane's option
directly.

**Identity, separately from state ([ADR-0055](../decisions/ADR-0055-an-agent-says-what-it-is.md)).**
Every state write (`start` included) also stamps `@dg_agent_kind claude`,
folded into the same `tmux` invocation via `\;` rather than a second process
spawn — this is what lets `dg ws`'s agents section list a coder that hasn't
prompted yet, and what survives `@dg_agent_state` being cleared by an
attach/focus/ack. `end` unsets both options and recomputes
`@dg_window_agent_state` with the same "any sibling pane still wants you?"
rule the pane-focus-in/out hooks use. `SessionEnd` fires reliably on `/exit`,
double ctrl-c, and normal completion, always carrying `$TMUX_PANE`; OpenCode's
equivalent is its TUI plugin's cleanup. Either way, the dashboard's agents
list stays correct regardless: a pane whose foreground process has reverted to
a plain shell is never listed, kind option or not (see ADR-0055's shell-name
backstop).

See [ADR-0005](../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md)
for the full design and value table. The OpenCode plugin equivalent
(`~/.config/opencode/plugins/devgeta-notify/tui.js`) writes the same pane
options, keeping both coders behaviourally matched for `busy`/`idle`/`blocked`
and for the kind option — but not for `error`: Claude's registered hooks above
have no counterpart for OpenCode's `session.execution.failed` event. Claude Code does have a
`StopFailure` event ("when the turn ends due to an API error") that could
serve this purpose, but it isn't wired up — it was out of scope for this
cycle. A Claude Code coder that errors out currently surfaces as an ordinary
`Stop`/`idle` transition, not as the red `✕` state. This is a real, current
asymmetry between the two coders, not a hypothetical one.

## Statusline

`statusline.sh` renders model, context bar, git branch/status, rate limits, and
duration. For directories that are **git worktrees** — either a _linked_ worktree
(`git --git-dir` ≠ `--git-common-dir`) **or** any directory under devgeta's
worktree base (`$XDG_DATA_HOME/devgeta/worktrees`, e.g. standalone clones placed
there) — it shows a compact `wt:<repo>` label instead of the full path, since the
branch already conveys the worktree identity.

The repo name in that label comes from git, not from the path. `dg worktree`
has two layouts — `<worktree-base>/<repo-slug>/<name>`, where the parent
directory is the repo, and `<repo-root>/.claude/worktrees/<name>`, where the
parent is the literal directory `worktrees` and names nothing — so no amount of
walking up from the current directory is right for both. For a linked worktree
the script instead resolves `--git-common-dir` (absolutising it first, since
git may answer with a relative path) and takes the repo name from the main
repo it points at, which is correct for both layouts and for a bare main repo.
The parent directory is used only as the fallback for a **standalone clone**
parked under the worktree base, where the common git dir is the checkout's own
`.git` and would name the branch rather than the repo.

### Statusline cache

The git portion of the line is cached for 5 seconds in `$TMPDIR`, keyed by
`<uid>` and a hash of the directory. It is deliberately **not** keyed by
session: everything cached is a pure function of the directory, so sessions in
the same directory share one file instead of each minting their own. Writes go
to a private name and are renamed into place, so a concurrent reader never sees
a partial line.

Files are reclaimed by a TTL sweep — `find … -maxdepth 1 -type f -name
'cc-statusline-*' -mtime +1 -delete` — run on roughly 1 in 32 cache refreshes
rather than on every render, so its cost amortises to near zero. A directory
still in use rewrites its own file every 5 seconds and can never age out from
under an active session. `find` does the deleting so that a hostile or
malformed filename in a shared temp directory is never re-expanded by the
shell, and `-type f` keeps the sweep off directories and symlinks.
