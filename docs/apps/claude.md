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
| `configs/claude/secret-guard.sh`           | `~/.claude/secret-guard.sh`           | `chmod 0755`                                 |
| `configs/claude/suppression-guard.sh`      | `~/.claude/suppression-guard.sh`      | `chmod 0755`                                 |
| `configs/claude/agent-state.sh`            | `~/.claude/agent-state.sh`            | `chmod 0755`                                 |
| `configs/claude/lib/`                      | `~/.claude/lib/`                      | sourced helpers, not executed directly       |
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
| `git worktree add ...`                                                                   | `devgeta task worktree-start <name> [--base <ref>]`             | devgeta repo only |
| `git worktree remove ...`                                                                | `devgeta task worktree-finish [<name>] --merge\|--discard`      | devgeta repo only |
| `gh pr checks ...`                                                                       | `devgeta task pr-checks`                                        | global            |
| `gh api graphql ... reviewThreads ...`                                                   | `devgeta task review-threads`                                   | global            |
| `gh pr review ...`                                                                       | `devgeta task submit-review --event ...`                        | global            |
| `git reset --soft HEAD~N` (N ≥ 1)                                                        | `devgeta task release <version> --message-file <file> [--push]` | devgeta repo only |
| `git tag -a v<semver> ...`                                                               | `devgeta task release <version> --message-file <file> [--push]` | devgeta repo only |

**Scope.** These hooks deploy to the user's global config, so they fire on
every Bash call in every repo. The `review-package`, `pr-checks`,
`review-threads`, and `submit-review` rules are **global**: each is a
better/compressed form of a universal git or `gh` operation that imposes no
devgeta-specific convention, so redirecting it everywhere is correct. Four rules
are **devgeta-repo-only**: `git worktree add` → `worktree-start` and `git
worktree remove` → `worktree-finish` (devgeta's worktree storage location
`~/.local/share/devgeta/worktrees/...` is devgeta's own layout convention, so
redirecting would wrongly impose it in other repos), and `git reset --soft
HEAD~N` and `git tag -a v<semver>` (which encode devgeta's own release policy,
§9 squash-before-tag, strict `vX.Y.Z`). All devgeta-repo-only rules fire only
when the command runs inside the devgeta repo — detected by walking up from the
payload's working directory (`.cwd`, falling back to the shell's `$PWD`) to the
first `go.mod` and confirming its module path is `github.com/cjairm/devgeta`. This check runs only after a devgeta-repo-only pattern has already matched (the
common allow path pays no lookup cost) and it **fails toward not firing**: if the
working directory is indeterminate, no `go.mod` is found, or the module doesn't
match, the raw git command is allowed through — the acceptable failure is "the
devgeta redirect didn't help here", never "a general `git worktree`/`git
reset`/`git tag` got blocked in another repo". The gh rules are narrow: `gh pr checks` and `gh pr review`
match only those exact subcommands (never `gh pr view`/`status`/`list`), and
the review-threads rule requires a `gh` invocation carrying both `api` and
`graphql` plus the literal `reviewThreads` (a bare `gh api graphql` or `gh api`
never matches).

Matching is deliberately narrow but not limited to the start of the whole
command string: the script splits the command into segments on unquoted
`&&`, `||`, `;`, and `|`, and checks every segment — so `cd some/dir && git
worktree add ../wt`, `git status; git worktree remove ../wt`, and `git fetch
&& git diff main..feature` are all caught, not just a bare `git ...` with
nothing else on the line. Each segment's `git`/`gh` anchor also tolerates a
leading run of shell `VAR=value` assignments (e.g. `GIT_PAGER=cat git diff
a..b`) and a run of the binary's own global options between it and the
subcommand — `-C <dir>`/`-c name=value`/`--git-dir=<path>` for `git`,
`-R <owner/repo>`/`--repo <owner/repo>`/`--repo=<owner/repo>` for `gh`, or any
other bare `--long`/`-x` flag — so `git -C ../wt worktree add x` and
`gh --repo owner/repo pr checks` still redirect (a global option defeating
the anchor entirely was a bypass found in review). Splitting respects single-
and double-quoted spans (a separator
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

**Bypass:** export `DEVGETA_SKIP_TASK_REDIRECT=1` in the shell that launches this
agent (e.g. the repo's `.envrc` file or your shell profile), BEFORE invoking the
agent — not inside the denied command, and not fixable mid-session, because the
hook reads its own process environment. Alternatively, edit `~/.claude/settings.json`
to remove the `PreToolUse` entry entirely if you never want this hook active.

The OpenCode plugin equivalent (`~/.config/opencode/plugin/task-redirect.js`,
a `tool.execute.before` hook) mirrors the same pattern table, the same
global-vs-devgeta scoping, and the same `DEVGETA_SKIP_TASK_REDIRECT` bypass. It
reads the working directory for the devgeta-repo-only gate (worktree add,
worktree remove, and the two release rules) from the plugin context's
`directory` (falling back to `worktree`, then `process.cwd()`) and applies the
identical fail-toward-not-firing `go.mod` check — see that file's header
comment.

Upstream-synced skills under `configs/shared/skills/` still hand-roll these raw
git sequences in their prose (they can't be edited without conflicting with
upstream syncs); this hook is the durable, runtime answer for those flows.

## Secret-commit guard (PreToolUse hook)

`settings.json` registers `~/.claude/secret-guard.sh` as a second hook under
the same `Bash` `PreToolUse` matcher as task-redirect.sh. Before a `git
commit` runs (anywhere in a compound command, using the same segment-splitting
logic as task-redirect.sh), it inspects what is actually staged
(`git diff --cached`, read-only) and denies with what to unstage if it finds:

| Signal                                                                                                            | Excluded                                                                        |
| ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Staged filename: `.env`/`.env.*`, `*.pem`, `id_rsa`/`id_ed25519`/`id_ecdsa`, `*.p12`/`*.pfx`/`*.keystore`/`*.jks` | `.env.example`/`.env.sample`/`.env.template` (templates), `*.pub` (public keys) |
| Staged content (added lines only): a private-key header, an AWS access key ID, a GitHub token, or a Slack token   | —                                                                               |

**Scope:** GLOBAL — fires in every repo, since "never commit a secret" is
universal practice, not a devgeta convention (see
[ADR-0006](../decisions/ADR-0006-hook-guardrails-scope-and-sharing.md)). This
is a safety net (a small, high-confidence pattern list), not a replacement for
a real secret scanner or CI pre-commit hooks. Denies via exit 2, same
mechanism as task-redirect.sh; a missing/unparseable command, `jq`/`git` being
unavailable, or the target not being a git repo falls through to exit 0
(allow).

**`git -C <dir> commit` is checked against `<dir>`, not the agent's own
working directory.** The hook resolves the actual git target from the LAST
`-C` in the commit invocation (uppercase only — `-c` is git's unrelated
inline-config flag), falling back to the payload's own working directory
when no `-C` is present. Scanning the wrong repository was a bypass found in
review.

**A commit that could stage NEW content this hook hasn't already seen is
denied outright, not checked.** This is a PreToolUse hook: it runs before
any part of the Bash command executes, so `git diff --cached` at check time
cannot reflect a staging action that's part of the SAME, not-yet-run
command. Two shapes trigger this:

- A compound command that stages before committing in one call
  (`git add -A && git commit ...`, also `git mv`/`git stage` — not `git rm`,
  which only removes content).
- A commit that self-stages via `-a`/`-am`/`--all` (`--amend` alone is
  unaffected).

Either denies with a message asking for two separate Bash calls: run the
staging command to completion first, then commit as its own call — at that
point `git diff --cached` faithfully reflects what will be committed.

**Bypass:** export `DEVGETA_SKIP_SECRET_GUARD=1` in the shell that launches this
agent (e.g. the repo's `.envrc`), BEFORE invoking the agent — not inside the
command — because the hook reads its own environment.

The OpenCode plugin equivalent
(`~/.config/opencode/plugin/secret-guard.js`) mirrors the same pattern lists
and the same bypass variable, importing `splitCommandSegments` from
`task-redirect.js` rather than duplicating it (ADR-0006 explains why it does
not introduce a separate shared helper file).

## Lint-suppression guard (PreToolUse hook)

`settings.json` registers `~/.claude/suppression-guard.sh` under a new
`Edit|Write` `PreToolUse` matcher. It denies an Edit or Write that
**introduces** a lint-suppression comment — `//nolint` (Go), `# noqa` /
`# type: ignore` / `# pylint: disable` (Python), `eslint-disable` /
`@ts-ignore` / `@ts-nocheck` (JS/TS), `@SuppressWarnings` (Java/Kotlin), or
`rubocop:disable` (Ruby) — instead of fixing the underlying issue. This makes
CLAUDE.md's "Lint issues" rule (`//nolint` is "never acceptable") structurally
impossible to violate rather than a convention to remember.

For an Edit, only a needle present in `new_string` but **absent** from
`old_string` counts as introduced — editing code around an existing,
untouched suppression elsewhere in the file is unaffected. For a Write, any
needle in the new content denies, since Write replaces the whole file and
there is no "before" to diff against.

**Scope:** DEVGETA-REPO-ONLY — gated by the same `is_devgeta_repo` go.mod walk
task-redirect.sh's worktree and release rules use. Banning suppression
comments outright is _devgeta's own_ stance, not a universal one (plenty of
codebases use them deliberately), so it must not fire in any other repo — see
[ADR-0006](../decisions/ADR-0006-hook-guardrails-scope-and-sharing.md). Denies
via exit 2; a missing/unparseable payload or `jq` being unavailable falls
through to exit 0 (allow).

**Bypass:** export `DEVGETA_SKIP_SUPPRESSION_GUARD=1` in the shell that launches this
agent (e.g. the repo's `.envrc`), BEFORE invoking the agent — not inside the
command — because the hook reads its own environment.

The OpenCode plugin equivalent
(`~/.config/opencode/plugin/suppression-guard.js`) mirrors the same pattern
list and bypass variable, importing `isDevgetaRepo` from `task-redirect.js`.

**Shared hook code:** `task-redirect.sh`, `secret-guard.sh`, and
`suppression-guard.sh` all source small helper files from `~/.claude/lib/`
(command segmentation, the devgeta-repo gate) instead of duplicating that
logic — safe on the Claude side since nothing auto-executes files there. The
OpenCode side deliberately does **not** mirror this with a standalone helper
file under `plugin/`: see ADR-0006 for why (OpenCode's plugin loader invokes
every export of every file in that directory as if it were a plugin).

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
