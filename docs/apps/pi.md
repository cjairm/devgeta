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

| Extension                 | What it adds                                                                   |
| ------------------------- | ------------------------------------------------------------------------------ |
| `npm:pi-subagents`        | Subagent delegation and scripted multi-agent workflows                         |
| `npm:pi-web-access`       | Web search, page/PDF reading, GitHub repo cloning, video input                 |
| `npm:pi-powerline-footer` | A powerline status bar in place of Pi's footer, colored by the active Pi theme |

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
