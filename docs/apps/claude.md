# Claude Code (`claude`)

Devgeta installs [Claude Code](https://claude.com/claude-code), Anthropic's
terminal AI CLI, as a first-class terminal tool and deploys a curated config to
`~/.claude/`.

- **Module:** `internal/apps/claude/`
- **Config source:** `configs/claude/` (+ shared content in `configs/shared/`)
- **Install:** official script (`curl -fsSL https://claude.ai/install.sh | bash`)

## What gets deployed

`ForceConfigure` copies the following into `~/.claude/`:

| Source                                     | Destination                           | Notes                                        |
| ------------------------------------------ | ------------------------------------- | -------------------------------------------- |
| `configs/claude/settings.json.tmpl`        | `~/.claude/settings.json`             | rendered template: theme, permissions, hooks |
| `configs/claude/statusline.sh`             | `~/.claude/statusline.sh`             | `chmod 0755`                                 |
| `configs/claude/format.sh`                 | `~/.claude/format.sh`                 | `chmod 0755`                                 |
| `configs/claude/task-redirect.sh`          | `~/.claude/task-redirect.sh`          | `chmod 0755`                                 |
| `configs/claude/agent-state.sh`            | `~/.claude/agent-state.sh`            | `chmod 0755`                                 |
| `configs/claude/themes/`                   | `~/.claude/themes/`                   |                                              |
| `configs/shared/{skills,commands,agents}/` | `~/.claude/{skills,commands,agents}/` | shared with OpenCode                         |

`settings.json` is rendered from a template so tracked opt-ins survive a
`--force` re-render: when `integrations.rtk_claude_hook` is set in
`global_config.yaml` (via `dg configure claude --force --only=rtk`, the
explicit opt-in required by [ADR-0004](../decisions/ADR-0004-ai-tools-install-category.md)),
the rendered file includes rtk's `PreToolUse` hook entry alongside devgeta's
own hooks. `dg uninstall rtk` clears the flag. See [rtk.md](rtk.md).

## Permissions model

`settings.json` uses a broad-allow-with-carve-outs model to minimize prompts.
Rules are evaluated deny → ask → allow (first match wins), so the carve-outs
override the broad allow:

- **allow** — `Bash(*)`, `Read`, `Edit`: day-to-day work never prompts.
- **ask** — rare-but-legitimate commands (remote copies, force pushes, infra
  applies) prompt instead of being blocked.
- **deny** — never allowed: network exfiltration tools, credential file reads
  (SSH keys, cloud CLI configs, token files, shell history), privilege
  escalation, persistence mechanisms (crontab/launchctl), destructive disk ops,
  and file edits to `.git/`, `.claude/`, and shell rc files. `Edit(path)` rules
  cover all file-editing tools (Edit, Write, NotebookEdit); `Write(path)` rules
  are not matched by file permission checks, so they must not be used.

Deny and ask rules apply in **every** permission mode, including
`bypassPermissions` (`claude --dangerously-skip-permissions`), so the guardrails
survive YOLO sessions.

**Known limits:** Bash deny rules are prefix matchers — interpreter one-liners
(`python -c`, `node -e`) and shell wrappers (`sh -c "..."`) can evade them, so
the deny list is friction and defense-in-depth, not a security boundary. For a
real boundary on high-autonomy work, enable Claude Code's built-in OS sandbox
(`/sandbox`; Seatbelt on macOS) which enforces filesystem and network limits at
the kernel level, and keep Claude Code up to date (deny-list bypass bugs have
been patched in past releases).

## Formatting & linting (PostToolUse hook)

`settings.json` registers a single `PostToolUse` hook on `Edit|Write` that runs
`~/.claude/format.sh`. After Claude edits a file, the script:

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

## Command redirect (PreToolUse hook)

`settings.json` registers a `PreToolUse` hook on the `Bash` tool that runs
`~/.claude/task-redirect.sh` before every Bash command Claude runs. The script
reads the command from the hook payload (`.tool_input.command`) and denies a
narrow set of raw-git patterns that have a dedicated `devgeta task` equivalent
— it never rewrites or runs anything itself, only denies with the exact
replacement to run instead:

| Raw pattern                                                                              | Replacement                                                     | Scope             |
| ---------------------------------------------------------------------------------------- | --------------------------------------------------------------- | ----------------- |
| `git diff <ref>..<ref>` / `git log <ref>..<ref>` (any flags, e.g. `--stat`, `--oneline`) | `devgeta task review-package <base> <head>`                     | global            |
| `git worktree add ...`                                                                   | `devgeta task worktree-start <name> [--base <ref>]`             | global            |
| `git worktree remove ...`                                                                | `devgeta task worktree-finish [<name>] --merge\|--discard`      | global            |
| `gh pr checks ...`                                                                       | `devgeta task pr-checks`                                        | global            |
| `gh api graphql ... reviewThreads ...`                                                   | `devgeta task review-threads`                                   | global            |
| `gh pr review ...`                                                                       | `devgeta task submit-review --event ...`                        | global            |
| `git reset --soft HEAD~N` (N ≥ 1)                                                        | `devgeta task release <version> --message-file <file> [--push]` | devgeta repo only |
| `git tag -a v<semver> ...`                                                               | `devgeta task release <version> --message-file <file> [--push]` | devgeta repo only |

**Scope.** These hooks deploy to the user's global config, so they fire on
every Bash call in every repo. The `review-package`, `worktree-start`,
`worktree-finish`, `pr-checks`, `review-threads`, and `submit-review` rules are
**global**: each is a better/compressed form of a universal git or `gh`
operation that imposes no devgeta-specific convention, so redirecting it
everywhere is correct. The two **release** rules (`git reset --soft HEAD~N` and
`git tag -a v<semver>`) are **devgeta-repo-only**: they encode devgeta's own
release policy (§9 squash-before-tag, strict `vX.Y.Z`), which would be wrong to
steer in any other project. A release rule fires only when the command is
running inside the devgeta repo — detected by walking up from the payload's
working directory (`.cwd`, falling back to the shell's `$PWD`) to the first
`go.mod` and confirming its module path is `github.com/cjairm/devgeta`. This
check runs only after a release pattern has already matched (the common allow
path pays no lookup cost) and it **fails toward not firing**: if the working
directory is indeterminate, no `go.mod` is found, or the module doesn't match,
the raw git command is allowed through — the acceptable failure is "the release
redirect didn't help here", never "a general `git reset`/`git tag` got blocked
in another repo". The gh rules are narrow: `gh pr checks` and `gh pr review`
match only those exact subcommands (never `gh pr view`/`status`/`list`), and
the review-threads rule requires a `gh` invocation carrying both `api` and
`graphql` plus the literal `reviewThreads` (a bare `gh api graphql` or `gh api`
never matches).

Matching is deliberately narrow but not limited to the start of the whole
command string: the script splits the command into segments on unquoted
`&&`, `||`, `;`, and `|`, and checks every segment — so `cd some/dir && git
worktree add ../wt`, `git status; git worktree remove ../wt`, and `git fetch
&& git diff main..feature` are all caught, not just a bare `git ...` with
nothing else on the line. Each segment's `git` anchor also tolerates a
leading run of shell `VAR=value` assignments (e.g. `GIT_PAGER=cat git diff
a..b`). Splitting respects single- and double-quoted spans (a separator
character inside a commit message is not treated as a boundary), but it is a
best-effort, non-adversarial split — it does not handle backslash-escaped
quotes, command substitution (`$(...)`/`` `...` ``), or heredocs; a command
deliberately crafted to defeat quote tracking is out of scope.

Within all of that, matching is still anchored and narrow: a bare `git diff`,
`git diff HEAD~1` (single ref, no range), `git log`, `git log -5`, `git tag`
(list, no `-a`), `git reset --soft HEAD` (no `~N`), and a commit message that
merely contains a trigger word (e.g. `git commit -m "fix: worktree stuff"`)
are never intercepted — only the exact multi-step dances these tasks
replace.

The hook denies via exit code 2 with a one-line reason on stderr (Claude Code's
simpler PreToolUse deny mechanism, chosen over the structured
`hookSpecificOutput`/`permissionDecision` JSON form to avoid any JSON-escaping
failure mode). A missing/unparseable command, or jq itself being unavailable,
falls through to exit 0 (allow) — this hook must never accidentally block all
Bash calls.

**Bypass:** every deny message ends with `set DEVGETA_SKIP_TASK_REDIRECT=1 to
bypass this session if raw git is genuinely needed`. Set that environment
variable for the session to let raw git through unconditionally, or edit
`~/.claude/settings.json` to remove the `PreToolUse` entry entirely if you
never want this hook active.

The OpenCode plugin equivalent (`~/.config/opencode/plugin/task-redirect.js`,
a `tool.execute.before` hook) mirrors the same pattern table, the same
global-vs-devgeta scoping, and the same `DEVGETA_SKIP_TASK_REDIRECT` bypass. It
reads the working directory for the release gate from the plugin context's
`directory` (falling back to `worktree`, then `process.cwd()`) and applies the
identical fail-toward-not-firing `go.mod` check — see that file's header
comment.

Upstream-synced skills under `configs/shared/skills/` still hand-roll these raw
git sequences in their prose (they can't be edited without conflicting with
upstream syncs); this hook is the durable, runtime answer for those flows.

## Agent activity state (Stop / UserPromptSubmit / Notification hooks)

`settings.json` registers three more hooks that report this coder's activity
into the tmux pane it's running in, so `dg ws`'s status dot and tmux's status
bar can show working / finished / blocked without switching to the window:

| Hook               | Matcher                       | Writes    |
| ------------------ | ----------------------------- | --------- |
| `Stop`             | none — fires every turn end   | `idle`    |
| `UserPromptSubmit` | none — fires every turn start | `busy`    |
| `Notification`     | `permission_prompt`           | `blocked` |

All three run `~/.claude/agent-state.sh <value>`. Unlike `format.sh` and
`task-redirect.sh`, this script reads no data from the hook's JSON stdin
payload — which value to write is fully determined by which hook fired,
passed as `$1`. It no-ops silently when `$TMUX_PANE` is unset (Claude Code run
outside tmux) and swallows any `tmux` failure, the same no-op contract the
other hooks use. `Notification`'s matcher is narrowed specifically to
`permission_prompt` so it excludes `idle_prompt` and Notification's other
types — an unfiltered `Notification` hook would map every notification onto
`blocked`, which is wrong.

`agent-state.sh` also writes/clears a window-level mirror
(`@dg_window_agent_state`) alongside its pane-level write, the same way the
OpenCode plugin does — this is what lets tmux's own status bar flag a window
nobody is looking at, since the status bar can't read one pane's option
directly.

See [ADR-0005](../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md)
for the full design and value table. The OpenCode plugin equivalent
(`~/.config/opencode/plugin/notify.js`) writes the same pane option, keeping
both coders behaviourally matched for `busy`/`idle`/`blocked` — but not for
`error`: Claude's three registered hooks above have no counterpart for
OpenCode's `session.error` event. Claude Code does have a `StopFailure` event
("when the turn ends due to an API error") that could serve this purpose, but
it isn't wired up — it was out of scope for this cycle. A Claude Code coder
that errors out currently surfaces as an ordinary `Stop`/`idle` transition,
not as the red `✕` state. This is a real, current asymmetry between the two
coders, not a hypothetical one.

## Statusline

`statusline.sh` renders model, context bar, git branch/status, rate limits, and
duration. For directories that are **git worktrees** — either a _linked_ worktree
(`git --git-dir` ≠ `--git-common-dir`) **or** any directory under devgeta's
worktree base (`$XDG_DATA_HOME/devgeta/worktrees`, e.g. standalone clones placed
there) — it shows a compact `wt:<repo>` label instead of the full path, since the
branch already conveys the worktree identity.
