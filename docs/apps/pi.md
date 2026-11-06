# Pi

Pi (`@earendil-works/pi-coding-agent`) is a terminal AI-coding harness. Devgeta
installs it as a terminal tool and registers it in `global_config.yaml`.

## Install

`dg install` (terminal category) or `dg install --only pi`.

Devgeta runs Pi's official installer (`https://pi.dev/install.sh`) under `sh`
— its shebang is `#!/bin/sh`. Like opencode, the script is staged through a
download-then-execute guard so a broken download cannot be reported as a
successful install (ADR-0059).

Idempotency:

- Skips if the script's marker (`~/.pi/agent/install/managed-install.json`)
  exists.
- Skips if `pi` already resolves on PATH (installed by other means — npm, your
  own setup): devgeta never lays a second copy over it.

Pi needs Node.js ≥ 22.19 and npm. The script checks this itself and fails with
instructions if they are missing. Terminal tools install before languages, so on
a clean machine the first `dg install` may report that; run
`dg install --only languages`, then `dg install --only pi`.

## Configuration — a curated extension set and one theme

`dg configure pi` (and the configure step of `dg install`) writes the devgeta
theme (see [Theme](#theme)), then runs Pi's own package manager once per
extension, `pi install npm:<name>`:

| Extension                                | What it adds                                                                               |
| ---------------------------------------- | ------------------------------------------------------------------------------------------ |
| `npm:pi-subagents`                       | Subagent delegation and scripted multi-agent workflows                                     |
| `npm:pi-web-access`                      | Web search, page/PDF reading, GitHub repo cloning, video input                             |
| `npm:pi-mcp-adapter`                     | MCP servers that start on first use and stop when idle, in place of Pi's built-in MCP      |
| `npm:@juicesharp/rpiv-ask-user-question` | A tool for the model to ask you up to four multiple-choice questions in one dialog         |
| `npm:@juicesharp/rpiv-todo`              | A todo tool with a live task panel above the editor that survives `/reload` and compaction |
| `npm:pi-powerline-footer`                | A powerline status bar in place of Pi's footer, colored by the active Pi theme             |
| `npm:pi-stalled-agent-recovery`          | Retries empty or thinking-only replies and nudges the model out of failing tool loops      |
| `npm:pi-openrouter-advanced-provider`    | An OpenRouter provider with the account's live model list and per-model routing            |
| `npm:pi-openrouter-request-details`      | Shows which provider and model OpenRouter actually served, with per-turn cost              |
| `npm:pi-thinking-reformatter`            | Repairs broken spacing and line breaks in thinking blocks (DeepSeek-style models)          |
| `npm:pi-lens`                            | Language-server diagnostics after each edit; its formatting, autofix and downloads are off |

The list lives in `internal/apps/pi/pi.go` (`piExtensions`) and is mirrored
here by a test.

### pi-subagents

It works with no configuration: its built-in agents (`scout`, `worker`,
`reviewer`, `oracle`, `delegate`, …) use your current Pi model, so nothing
depends on a provider you have not set up. Run `/subagents-doctor` if
delegation misbehaves. Devgeta writes no configuration for it.

One setting is worth adding yourself if you have more than one provider signed
in. The parent model may pick a model for a child on its own, and Pi matches the
name against any provider that has a key — one user's local model delegated to
a paid model this way
([pi-subagents#3203](https://github.com/nicobailon/pi-subagents/issues/3203)).
This limits children to the parent's model, or to your `/scoped-models` set
when you have one, in `~/.pi/agent/settings.json`:

```json
{
  "subagents": {
    "modelScope": { "enforce": true, "strict": true, "allow": ["scoped"] }
  }
}
```

With this on, the bundled `claude-code` and `claude-code-writer` agents must
name a model, or they refuse to start. To route roles to different models on purpose, set
`subagents.agentOverrides.<agent>.model` and add those models to `allow`. The
upstream docs (`/subagents-guide models`) describe a tiering that works well:
a cheap fast model for `scout`, a mid-tier one for `worker` and `reviewer`, and
a top reasoning model only for `oracle`.

### pi-web-access

It works with no configuration and no API key. Search goes to Exa's free hosted
endpoint by default, so your search queries are sent to Exa. If Pi is signed in
with a ChatGPT/Codex subscription, OpenAI search is tried first. Devgeta writes
no configuration for it.

To use another search service (Brave, Tavily, Perplexity, Gemini, …) or raise
Exa's limits, export its key in your shell, for example `BRAVE_API_KEY` or
`EXA_API_KEY`. If you prefer a config file, use
`~/.pi/agent/web-search.json` and point at a variable rather than pasting the
key itself:

```json
{
  "braveApiKey": "$BRAVE_API_KEY"
}
```

A value starting with `!` runs a command instead (for example a password
manager's CLI, given by absolute path) each time the key is needed.

The extension edits that file itself (`/curator` and the curator page save
their choices there), which is one more reason devgeta leaves it alone.

The defaults are conservative: search returns plain results without opening a
browser page or making an extra model call, browser cookies are never read
unless you allow them (`allowBrowserCookies`), page fetches never fall back to
third-party scraping services unless you allow them, and private network
addresses are blocked.

Optional tools it uses when present:

- `gh` (installed by devgeta) — private GitHub repos and readable PR/issue
  views.
- `ffmpeg` and `yt-dlp` — only for pulling still frames out of videos. Video
  understanding itself needs a Gemini key.
- `xdg-utils` on Linux — only to open the curator page, which is off by
  default.

**With pi-subagents:** the built-in `researcher` and `evidence-auditor` agents
need pi-web-access loaded inside the child agent, and foreground children never
load the parent's extensions. Run them in the background (`async: true`), where
extensions load normally, or list the extension for those agents in
`~/.pi/agent/settings.json` under
`subagents.agentOverrides.<agent>.subagentOnlyExtensions`.

### pi-mcp-adapter

It replaces Pi's built-in MCP support. The built-in starts every enabled server
when a session starts and keeps all of them running; the adapter starts a
server the first time the model calls one of its tools and stops it after 10
idle minutes. The model sees one small `mcp` tool and searches for the server
tools it needs. Sign-in tokens go to the OS keychain instead of a JSON file.
Devgeta writes no configuration for it.

**It turns Pi's built-in MCP off for you.** On the first start after an install
or update it adds `"-builtin:mcp"` to `extensions` in
`~/.pi/agent/settings.json`, the same entry `pi config` writes, and says so
once. If you remove the adapter later, the built-in stays off: turn it back on
under Built-in extensions in `pi config`.

**Where servers come from.** With no setup it reads `~/.config/mcp/mcp.json`
(all projects), a project's `.mcp.json`, and servers you added with
`pi mcp add`. A project's servers ask once before they first run. Servers
configured in Claude Code, OpenCode, Cursor, Codex or VS Code are not read
until you import them: run `/mcp-adapter setup` in Pi (or
`pi-mcp-adapter init` in a shell), which shows what it found and the exact file
change before writing. The Claude Code import reads only user-scope servers
(`claude mcp add --scope user`), not ones added for a single project. Devgeta
does not add imports: it ships no MCP servers to any agent, and the adapter
rewrites its own `~/.pi/agent/mcp-adapter.json`.

`pi-mcp-adapter doctor` checks every configured server from a shell and never
starts a sign-in.

Things worth knowing:

- **The model can add a remote server by itself.**
  `mcp({ action: "install", url })` saves an HTTPS server to the global
  `mcp-adapter.json` and connects to it without asking; only a project install
  asks. To block it, add `"settings": { "allowInstall": false }` to that file.
- **Sign-in on Linux needs an unlocked keyring** (GNOME Keyring or another
  Secret Service). Without one, OAuth sign-in fails rather than saving tokens in
  plain text. Desktop Ubuntu has one; a headless server usually does not.
  Local servers and servers that use a header or token are not affected.
- **Bulk MCP work costs more tokens than the built-in** — up to 5.5× by the
  adapter's own measurement, when many records pass through the model one call
  at a time. Single lookups cost about the same. Turning on Pi's `codemode`
  lets the model script many calls in one step, which roughly halves that gap.
  Add it in `~/.pi/agent/settings.json`:

  ```json
  {
    "defaultTools": ["+codemode"]
  }
  ```

**With pi-subagents:** foreground children never load the parent's extensions,
so they get no MCP tools. Run an agent that needs them in the background
(`async: true`).

### rpiv-ask-user-question

It adds one tool, `ask_user_question`, the counterpart of Claude Code's
AskUserQuestion: the model asks up to four questions in one tabbed dialog, each
with two to four described options plus a free-text answer, and gets your
choices back as structured data. It makes no model calls of its own. Devgeta
writes no configuration for it.

- **Only where someone can answer.** In a non-interactive run (`pi -p`, JSON
  mode) the tool is removed from the model's tool list rather than failing each
  call. Editor hosts such as Zed get a plain one-question-at-a-time version.
- **Keys:** inside the dialog, `n` adds a note and Ctrl+] shrinks the dialog to
  one line. Ctrl+] is only taken while a dialog is open; otherwise it keeps
  Pi's own meaning in the editor (jump to a character).
- **How eagerly the model asks** is set by the tool's built-in guidance, which
  tells it to ask whenever a request is ambiguous. To change that, or the keys,
  create `~/.config/rpiv-ask-user-question/config.json` (`guidance.*`,
  `collapseKey`, `notesKey`). The extension only reads that file; it never
  writes it.
- Interface text is in English. The optional `@juicesharp/rpiv-i18n`
  package translates it; devgeta does not install it.

### rpiv-todo

It adds a `todo` tool, a `/todos` command, and a panel above the editor showing
what the agent is doing, what it finished and what is queued. The list is
rebuilt from the session itself, so it survives `/reload` and compaction and
writes nothing to disk. Tasks can depend on each other (`blockedBy`). Devgeta
writes no configuration for it.

Pi has no task-list tool of its own, and the Superpowers skills (`executing-plans`,
`subagent-driven-development`) ask for one on Pi: without it they fall back to a
`TODO.md` file.

- **Keys:** Ctrl+Shift+T collapses the panel to one line. Pi binds nothing to
  it by default. Whether your terminal and tmux pass that combination through
  is untested; `/todos` prints the full list either way.
- **Settings** go in `~/.config/rpiv-todo/config.json`: `maxWidgetLines`
  (default 12), `collapseKey`, `guidance`. The extension only reads that file.
- With no screen (`pi -p`) the tool still works; nothing is drawn.

**With pi-subagents:** each session keeps its own list, and foreground children
never load the parent's extensions, so a foreground child agent has neither
tool: it cannot ask you questions or show a task panel.

### pi-powerline-footer

It replaces Pi's footer with a powerline bar (model, path, git, thinking level,
context use). Its colors come from the active Pi theme, so under the `devgeta`
theme `dg theme set` restyles the bar within about five seconds, with no
restart (ADR-0062).

Devgeta writes one file for it,
`~/.pi/agent/extensions/powerline-footer/theme.json`. The extension hard-codes
three colors (model, path and a rainbow for the high thinking levels); this
file points them at Pi theme roles instead: `model` → `syntaxNumber`, `path` →
`syntaxType`, and the `high`/`xhigh`/`max` thinking levels → Pi's own
`thinkingHigh`/`thinkingXhigh`/`thinkingMax`. The file names roles, never
colors, so it also works under a theme you picked yourself. `dg configure pi`
writes it only when it is absent; `--force` restores it. `dg theme set` never
touches it. To get the rainbow back, set a key to `rainbow` there and
configure without `--force`.

What still ignores the palette, because the extension offers no way to change
it: the startup welcome overlay, and the gray used for separators in the editor
frame and for ghost text in bash mode.

Nerd Font glyphs: the extension guesses from the terminal's name. Ghostty is
recognized. Devgeta's Alacritty config sets `POWERLINE_NERD_FONTS=1` when it
installs its own Nerd Font, so the bar shows glyphs there too (run
`dg configure alacritty --force`). Its network features (`autoFollowUp`,
non-USD `cost.currency`, `/vibe`) are all off by default; devgeta turns none on.

### pi-stalled-agent-recovery

When a turn ends with an empty or thinking-only reply, it sends a hidden "keep
going" message and retries, up to 5 times. When the same tool call fails 5
times in a row, it nudges the model to change approach, up to 3 times, then
stops intervening. Each retry or nudge is a new model request, so it costs
tokens. The limits are in `~/.pi/agent/stalled-agent-recovery.json`
(`maxRetries`, `loopThreshold`, `maxLoopNudges`; `"maxRetries": 0` turns
retries off). Devgeta writes no configuration for it.

### pi-openrouter-advanced-provider

It replaces Pi's OpenRouter provider with one that loads your account's model
list (including bring-your-own-key models), picks the provider behind each
model by price, speed and tool-call error rate, and uses that provider's real
context window. It reads your key from `OPENROUTER_API_KEY` or from Pi's
`/login`. Commands: `/openrouter-sync`, `/openrouter-balance`,
`/openrouter-status`, `/openrouter-tier`. Settings live in
`~/.pi/agent/openrouter-advanced-provider.json`; devgeta writes none.

**It contacts openrouter.ai even if you never use OpenRouter.** Each session
start fetches the public model list, and the first prompt fetches OpenRouter's
public catalog (cached for a day). Without a key, those requests carry no
key and no prompt text, only the model name. Setting
`"selection": { "enabled": false }` in its settings file stops the catalog
lookup; `pi remove npm:pi-openrouter-advanced-provider` stops both.

**With pi-subagents:** OpenRouter puts almost every model behind one key, which
is how a child agent can quietly end up on an expensive model. If you sign in
to OpenRouter, add the `subagents.modelScope` setting shown under
[pi-subagents](#pi-subagents).

### pi-openrouter-request-details

For OpenRouter replies only, it shows which provider and model actually served
each request, with token counts and cost. `/openrouter-details` hides or shows
the cards. It fetches prices from openrouter.ai only for OpenRouter model names
(`author/model`), and uses `OPENROUTER_API_KEY` when set. Devgeta writes no
configuration for it.

### pi-thinking-reformatter

Some models (DeepSeek-style) stream thinking with stray spaces and broken
lines. It repairs that both on screen and in the saved message, so the clean
text is what the model reads back later. It checks every model's thinking, but
only rewrites text that matches the broken patterns, so clean output from
Claude or OpenAI models is left as is. Devgeta writes no configuration for it.

### pi-lens

Adds language-server diagnostics (for example a Go type error from gopls) to
Pi after edits. On its own it also formats, autofixes, and downloads any tool it
is missing, with tools that differ from the Mason ones Claude Code and OpenCode
use. Devgeta keeps only the diagnostics: see [Formatting](#formatting).

**Pi owns the install.** Devgeta never touches Pi's npm tree, and writes one key
in Pi's settings file, `theme` (see [Theme](#theme)). Versions are whatever npm
serves at install time, and `pi update` advances them (ADR-0060).

Re-running:

- `dg configure pi` installs only extensions `~/.pi/agent/settings.json` does
  not already declare — a declaration with a version or tag counts. `--force`
  does the same: devgeta's own pieces are the theme, and refreshing installed
  extensions is `pi update`.
- If an install fails, configure stops and names the extension. Nothing is
  rolled back and pi is not registered as configured; fix the cause and re-run
  to install what is still missing.
- A settings file devgeta cannot parse stops configure with the file named; it
  is never treated as empty.

Configure runs `pi` from your PATH, then falls back to the launcher the script
writes at `~/.pi/agent/bin/pi`, so a configure right after an install works
without a shell reload.

To drop one extension, use Pi directly: `pi remove <source>`. `pi list` shows
the current set; `dg configure pi` will add it back.

Devgeta no longer installs `pi-schedule-prompt` (it crashes on current Pi and
can corrupt a session); v1.44.0 was the last release that did. Configure only
adds extensions, so if you have it, remove it yourself: find its entry in
`pi list` and pass that source to `pi remove`.

## Formatting

After every `edit` or `write`, Pi formats and lints the file with the same
Mason binaries and settings as Claude Code (ADR-0063). Three pieces:

- `~/.config/devgeta/format-file.sh` is the one table of formatters and
  linters, shared with Claude's hook. Mason (`$XDG_DATA_HOME/nvim/mason/bin`,
  default `~/.local/share/nvim/mason/bin`) is the only source of tools.
- `~/.pi/agent/extensions/devgeta-format.js` runs it after each edit. If the
  file changed, the tool result says `Formatted by devgeta: lines N–M changed`
  and shows those lines (capped; over the cap it tells the model to re-read the
  file). Linter findings are appended as `Linter findings for <path>`. Any
  failure is silent and never fails the edit. Devgeta rewrites this file on
  every `dg configure pi`; put your own changes in a separate extension file.
- `~/.pi-lens/config.json` gets `format.enabled` and `autofix.enabled` set to
  `false` (other keys are kept; without `--force` a value you already set
  stays). `PI_LENS_CONFIG_PATH` moves that file; `PI_CODING_AGENT_DIR` is not
  followed.

Side effects to know about:

- The extension prepends the Mason directory to Pi's `PATH` so pi-lens finds
  those tools.
- It sets `PI_LENS_DISABLE_TOOL_INSTALL` and `PI_LENS_DISABLE_LSP_INSTALL` to
  `1` unless you already set them. To let pi-lens download tools again, export
  both as `0`.
- Tools pi-lens downloaded earlier can still win over Mason. Delete
  `~/.pi-lens/tools` and `~/.pi-lens/bin` to stop that; devgeta never deletes
  them.

## Theme

Pi is the eighth surface `dg theme` restyles (ADR-0061). Devgeta owns exactly
one file in Pi's themes directory, `~/.pi/agent/themes/devgeta.json`, rendered
from `configs/pi/themes/devgeta.json.tmpl` and the `colors:` group of the
current theme. The name `devgeta` never changes; its colors follow
`dg theme set`. Any other theme file you put in that directory is left alone.

- `dg theme set <name>` rewrites that file. A running Pi picks the new colors up
  without a restart. It never touches Pi's settings, so a theme you chose in
  Pi's `/settings` stays chosen.
- `dg configure pi` sets `"theme": "devgeta"` in `~/.pi/agent/settings.json`
  only when that file has no `theme` key. `dg configure pi --force` sets it
  unconditionally. Every other key in the file, `packages` above all, is kept
  as is. A file that does not parse stops configure with the file named.
- Pi shows block backgrounds behind user messages and tool output; those use the
  palette's element and diff background colors. Text itself uses your terminal's
  foreground color.

### Terminal keys

Pi tells Shift+Enter (newline) from Enter (submit) and Option+Enter (queue a
follow-up). Devgeta's own configs forward those keys:

- **tmux:** `extended-keys on`, plus `extended-keys-format csi-u` on tmux 3.5 or
  newer (older tmux does not have that option). Run `dg configure tmux --force`
  and start a new tmux server for it to apply.
- **Alacritty, macOS only:** Option+Enter sends `\u001b[13;3u`. A program that
  does not read that sequence, such as a bare zsh prompt, may print it instead of
  acting like Enter.
- **Ghostty:** nothing to change.

Everything else stays out of scope: no shared skills, no permission or
formatter parity with claude/opencode, no `dg worktree`/`dg task` wiring
(ADR-0059).

## Uninstall

`dg uninstall pi` removes **only** `~/.pi/agent/install`. `~/.pi/agent` itself
holds Pi's config, sessions, installed extensions and agent state and is never
touched. The `pi` binary is a symlink into the install root, so it disappears
with it; PATH entries the script added to your shell profile are left alone.
