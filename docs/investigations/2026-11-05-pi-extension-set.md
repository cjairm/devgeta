# Investigation — Pi's curated extension set

**Date:** 2026-11-05
**Status:** Open
**Feeds:** none yet — a cycle doc, and an ADR amending ADR-0059 §7 and ADR-0060
§8 (see [Decisions this will need](#decisions-this-will-need))

## Question

Which extensions should `dg configure pi` install by default, is each one well
maintained, and what devgeta-side work does the new set need? The current set
lives in `piExtensions` (`internal/apps/pi/pi.go`) and is mirrored in
`docs/apps/pi.md` by a test. This is the first of the "rework our tools" passes:
settle Pi's set, implement it, then migrate the other agents' tooling. Each
entry below is meant to be worked on its own.

## Scope

- **In:** the default extension set, each library's maintenance, live install
  output, footer colors, and reusing the Mason formatters.
- **Out:** the other agents' tooling, MCP server definitions themselves, and
  pinning extension versions (ADR-0060 §6 stands).

## Method

- Sources: the npm registry (`npm view`, `npm search`, package tarballs unpacked
  into a scratch directory with `npm pack --ignore-scripts`) and the GitHub API
  (`gh api repos/<owner>/<repo>`, `gh issue list`).
- When: 2026-11-05. "Commits 90d" counts commits since 2026-08-05. The API pages
  at 100, so `100+` means at least 100; the same cap applies to contributors.
- Criteria: "well maintained" means all of: commits in the last month, more than
  one contributor or a long release history, open bugs that get answered, and
  declared compatibility with Pi 1.5 (`@earendil-works/pi-coding-agent` 1.5.0,
  released 2026-11-02).
- Not done: nothing was installed or run. Every runtime behavior below is read
  from docs or source and is unverified until the sandbox run.

## Summary

| Library                              | Action                                 | Maintenance            |
| ------------------------------------ | -------------------------------------- | ---------------------- |
| ~~`pi-subagents`~~ ✅ done           | Keep                                   | Well maintained        |
| ~~`pi-web-access`~~ ✅ done          | Keep                                   | Well maintained        |
| ~~`pi-powerline-footer`~~ ✅ done    | Keep, plus a color override            | Well maintained        |
| `pi-stalled-agent-recovery`          | Keep (maintainer's choice)             | Too new to judge       |
| `pi-openrouter-advanced-provider`    | Keep (maintainer's choice)             | Too new to judge       |
| `pi-openrouter-request-details`      | Keep (maintainer's choice)             | Too new to judge       |
| `pi-thinking-reformatter`            | Keep (maintainer's choice)             | Too new to judge       |
| `pi-schedule-prompt`                 | **Drop**                               | Not maintained         |
| `pi-mcp-adapter`                     | **Add**                                | Well maintained        |
| `@plannotator/pi-extension`          | **Add**                                | Well maintained        |
| `@juicesharp/rpiv-ask-user-question` | **Add**                                | Well maintained        |
| `@juicesharp/rpiv-todo`              | **Add**                                | Well maintained        |
| `pi-lens`                            | **Add**, last in order, formatting off | Well maintained        |
| `pi-schedule`                        | **Add only if the sandbox run passes** | Active, one maintainer |

---

## Keep

### ~~`pi-subagents`~~ — done 2026-11-05

Delegation to focused helper agents and scripted multi-agent workflows. Pi
leaves delegation out on purpose; this is the most useful extension in the set.

- 0.86.0 · 4,137 stars · 3 open issues · 100+ commits 90d · 100+ contributors ·
  MIT
- Declares `@earendil-works/pi-ai >=1.2.0`, so it targets current Pi. Releases
  daily; 0.86.0 (2026-11-05) fixes a Pi 1.5 bug where a session lost its
  subagent tools after a compaction (#3198).
- **Configuration (read from the 0.86.0 tarball's `docs/`):** none is needed.
  Built-in agents inherit the parent's model, `toolActivation: "auto"` avoids a
  prompt-cache miss, nesting stops at depth 2, 20 children run at once at most,
  and a run times out after 30 minutes. `/subagents-doctor` checks the setup.
  Upstream's README and the web search found no recommended `config.json`.
- **One risk with no-config:** the parent model may choose a child's model by
  itself, and Pi fuzzy-matches it against any signed-in provider. #3203 (open,
  2026-11-05): a local Qwen delegated to Opus over OpenRouter, a surprise $20.
  `subagents.modelScope` with `allow: ["scoped"]` blocks this; it lives in Pi's
  `settings.json`, which devgeta does not write (ADR-0060 §1), so
  `docs/apps/pi.md` shows it as an opt-in snippet.
- **Matters for later entries:** foreground children never load the parent's
  extensions. The built-in `researcher` and `evidence-auditor` need
  `pi-web-access` loaded in the child (via the agent's `extensions` or
  `subagentOnlyExtensions`, or `async: true`), and agents using
  `pi-mcp-adapter` tools must run in the background. Check both when those two
  are added.
- **Optional:** `pi-intercom` is no longer needed; child-to-parent messages are
  built in.
- **Action:** done — it was the first entry restored to `piExtensions`; the
  others are re-added one at a time.

### ~~`pi-web-access`~~ — done 2026-11-06

Web search, page and PDF reading, GitHub repo cloning, YouTube and local video
input.

- 0.47.0 · 1,769 stars · 2 open issues · 100+ commits 90d · 91 contributors ·
  MIT. In the 30 days to 2026-11-06: 64 commits by the author (who also wrote
  `pi-subagents`), 32 by 25 others. `main` is three commits past 0.47.0, all
  search-provider additions and fixes; none touch setup.
- Pi 1.5 support is not declared: peer range `*`, built against Pi 1.0.4, and
  the README says "Requires Pi v0.37.3+". Sandbox step 2 still applies.
- **Correction — no key is needed** (read from the 0.47.0 tarball's README and
  `exa.ts`, and the README on `main`). With no configuration, search goes to
  Exa's free hosted endpoint (`https://mcp.exa.ai/mcp`), so queries are sent to
  Exa. If Pi is signed in with a ChatGPT/Codex subscription, OpenAI search is
  tried first. Keys only add other services (Brave, Tavily, Perplexity, …) or
  raise Exa's limits. They come from environment variables (`BRAVE_API_KEY`,
  `EXA_API_KEY`, …) or from `~/.pi/agent/web-search.json`, which accepts a
  variable reference (`"$BRAVE_API_KEY"`) or a command (`"!<cmd>"`) in place
  of the key.
- **Devgeta must not write `web-search.json`.** The extension rewrites the
  whole file itself (`saveConfig` in `index.ts`, called by `/curator` and by
  provider changes in the curator page), so a devgeta template would be
  overwritten, and a key placed there would sit in a config file. The file
  honors `PI_CODING_AGENT_DIR` and `XDG_CONFIG_HOME`.
- **The defaults need no change:** plain results with no browser page and no
  extra model call (`workflow: "none"`), browser cookies off
  (`allowBrowserCookies: false`), no third-party scraping fallbacks
  (`fetchRouting.allowRemoteHostedProviders: false`), private network
  addresses blocked, and `toolActivation: "auto"` (a compact `web_enable`
  tool where the model can take tools mid-session without a prompt-cache
  miss).
- **Optional system tools, none added:** `gh` (already installed by devgeta)
  for private repos and PR/issue views; `ffmpeg` and `yt-dlp` only for video
  frame extraction (video understanding itself needs a Gemini key);
  `xdg-utils` on Linux only to open the curator page, which is off by default.
- **pi-subagents interaction** (read from the 0.86.0 tarball's
  `docs/agents.md`): `researcher` and `evidence-auditor` fail unless the child
  registers all four web tools. Foreground children never load the parent's
  extensions, so either run them with `async: true` or set
  `subagents.agentOverrides.<agent>.subagentOnlyExtensions` in Pi's
  `settings.json`. The docs call those entries "extension paths"; whether an
  `npm:pi-web-access` source works there is unverified — add it to the sandbox
  run. Devgeta does not write that file (ADR-0060 §1), so `docs/apps/pi.md`
  documents both options.
- Not used: the package also ships an MCP server (`npx -y pi-web-access`) for
  Claude Code and others. Claude Code has web search built in.
- **Action:** done — added to `piExtensions` after `pi-subagents`, and
  `docs/apps/pi.md` has a section covering the above. No devgeta config.

### `pi-powerline-footer`

Powerline-style status bar, plus features beyond the footer: holds messages
typed during compaction, a sticky bash mode, `/cd`.

- 0.22.0 · 471 stars · 3 open issues · 93 commits 90d · 24 contributors · MIT
- **Do not build our own.** It is configurable through Pi's settings (presets,
  hidden segments, colors as theme names or hex), and a custom footer would be
  JavaScript against `@earendil-works/pi-tui`, which was just renamed.
- **Color fix:** Pi's default `system` theme builds its colors from the
  terminal's palette (`docs/themes.md` in the Pi package). Alacritty and Ghostty
  are rendered from `theme.Palette`, so Pi already follows `dg theme`. The
  footer uses Pi theme color names except two hard-coded hex values: `model`
  `#d787af` and `path` `#00afaf`.
- **Action:** done (ADR-0062, cycle 2026-11-06-pi-powerline-footer) — added to
  `piExtensions` after `pi-web-access` (the entries ahead of it in the install
  order are not shipped yet), plus a static
  `~/.pi/agent/extensions/powerline-footer/theme.json`, written only when absent
  (soft) or always (force). Reading the 0.22.0 source showed a third hard-coded
  color (the `rainbow` for the high thinking levels), so it maps three colors,
  five keys, to Pi theme roles; and that Alacritty needs
  `POWERLINE_NERD_FONTS=1` to get glyphs.
- **Open questions, closed:** OSC 4/10/11 passthrough under tmux no longer
  matters, because the `devgeta` Pi theme uses explicit colors, not the
  terminal's palette (ADR-0061). The footer paints no backgrounds; Pi's own
  block backgrounds were settled in step one (`docs/apps/pi.md`, Theme).

### `pi-stalled-agent-recovery`

Retries empty or thinking-only responses and breaks loops of the same failing
tool call. Works with any provider.

- 1.0.0 · 0 stars · 0 open issues · 2 commits 90d · 1 contributor · MIT · repo
  created 2026-09-26
- Does not meet the "well maintained" bar: one author, new, unused. Kept by the
  maintainer's choice.
- **Action:** read the source before shipping — extensions run with the agent's
  full permissions.

### `pi-openrouter-advanced-provider`

OpenRouter provider: live model catalog for the account, per-model provider
routing, real context windows. Does nothing unless you use OpenRouter.

- 1.0.0 · 1 star · 1 open issue · 17 commits 90d · 1 contributor · MIT · repo
  created 2026-09-26
- Same author and same caveat as `pi-stalled-agent-recovery`.
- **Action:** read the source before shipping.
- **Revisit the subagent model guard first.** OpenRouter puts almost every model
  behind one key, which is how a local model in pi-subagents#3203 reached Opus
  and cost $20. Shipping this extension by default makes that setup common, so
  decide then whether devgeta ships `subagents.modelScope`
  (`allow: ["scoped"]`) by default instead of only documenting it in
  `docs/apps/pi.md`. That means writing Pi's `settings.json`, so it needs an
  amendment to ADR-0060 §1.

### `pi-openrouter-request-details`

Shows which provider and model OpenRouter actually served, with per-turn cost
cards. Does nothing unless you use OpenRouter.

- 1.0.0 · 0 stars · 0 open issues · 2 commits 90d · 1 contributor · MIT · repo
  created 2026-09-26
- Same author and same caveat.
- **Action:** read the source before shipping.

### `pi-thinking-reformatter`

Fixes the spurious whitespace and line breaks in thinking blocks from
DeepSeek-style models. Does nothing for Claude or OpenAI models. Not replaced by
`pi-lens`: that one checks files the agent writes, this one fixes how model
output is displayed.

- 1.0.0 · 0 stars · 0 open issues · 2 commits 90d · 1 contributor · MIT · repo
  created 2026-09-26
- Same author and same caveat.
- **Action:** read the source before shipping.

---

## Drop

### `pi-schedule-prompt`

Cron-like recurring and one-shot prompts.

- 0.4.1 · 122 stars · 11 open issues · last commit 2026-06-23 · MIT
- **Not maintained.** Two open bugs matter:
  - #23 (2026-09-20, no reply): crash on Pi 0.86 —
    `Cannot read properties of undefined (reading 'runtime')`.
  - #12 (2026-05-05): scheduler messages with empty content corrupt the parent
    session, so every later request fails with a 400. A user posted a verified
    three-line fix in May; it was never merged.
- **Action:** remove from `piExtensions` and the `docs/apps/pi.md` table.
  Configure only adds, so existing installs keep it; the doc should tell users
  to run `pi remove npm:pi-schedule-prompt`. Replacement: `pi-schedule`
  (conditional, below).

---

## Add

### `pi-mcp-adapter`

MCP support that starts a server on first use and stops it after 10 idle
minutes. Pi 1.5's built-in MCP starts every enabled server at session start and
keeps all of them running.

- 7.0.1 · 1,641 stars · 0 open issues · 100+ commits 90d · 90 contributors · MIT
- Turns Pi's built-in MCP off itself when installed (in Pi's own settings).
- Stores sign-in tokens in the OS keychain instead of a JSON file under
  `~/.pi/agent`.
- **Reusing Claude Code's setup:**
  - MCP servers: yes. Its `imports` setting accepts `claude-code` (reads
    `~/.claude.json`), plus `opencode`, `claude-desktop`, `cursor`, `codex`,
    `vscode`, `windsurf`. The host file is read at startup, not copied, so
    Claude stays the single place to edit servers. `/mcp-adapter setup` or
    `pi-mcp-adapter init` adds the import. Project `.mcp.json` files load
    automatically, behind a per-project approval prompt.
  - Claude Code plugins: partly. Its `claudePlugins` setting loads a plugin's
    MCP servers (root `.mcp.json`) and skills (`skills/**/SKILL.md`) from a
    listed local plugin directory. Hooks, commands and agents do not carry over.
  - Nothing is removed from Claude Code either way.
- **Trade-off:** bulk MCP work can cost up to 5.5× more tokens than the built-in
  unless `"+codemode"` is added to Pi's `defaultTools` setting (the adapter's
  own measurement).
- **Action:** add to `piExtensions`. Decide whether devgeta adds the
  `claude-code` and `opencode` imports or leaves that to `/mcp-adapter setup`.

### `@plannotator/pi-extension`

Plan mode: destructive commands blocked, writes limited to the plan file, and a
browser UI for commenting on plans. Pi has no plan mode of its own (it ships one
only as an example).

- 0.28.50 · 9,866 stars · 161 open issues · 100+ commits 90d · 100+
  contributors · MIT OR Apache-2.0
- The backlog is large, but so is the project (it also ships plugins for other
  agents).
- **Action:** add to `piExtensions`.

### `@juicesharp/rpiv-ask-user-question`

A structured question tool with typed options, the closest match to Claude
Code's AskUserQuestion.

- 2.15.0 · 943 stars (monorepo `juicesharp/rpiv-mono`) · 81 open issues · 100+
  commits 90d · 44 contributors · MIT
- Its peer `@juicesharp/rpiv-i18n` only translates the interface and is
  optional. That matters because Pi does not install peer dependencies for
  managed npm packages (`docs/packages.md` in the Pi package).
- **Action:** add to `piExtensions`.

### `@juicesharp/rpiv-todo`

A todo tool shown as a live overlay that survives `/reload` and compaction, the
closest match to Claude Code's todo list.

- 2.15.0 · same monorepo and numbers as above · 140 releases
- **Action:** add to `piExtensions`.

### `pi-lens`

After every edit or write, runs language-server diagnostics, linters, type
checkers and formatters, and feeds the findings back to the agent.

- 4.4.9 · 514 stars · 122 open issues · 100+ commits 90d · 74 contributors · MIT
- **Install risk:** its install builds a native binary (`@ast-grep/cli`
  postinstall), its README documents that step failing, and configure stops at
  the first failed extension. **Put it last** in `piExtensions`.
- **How it picks tools** (read from `dist/` of 4.4.9, `getToolPathResolved`):
  its own installs first (`~/.pi-lens/tools/node_modules/.bin`, then
  `~/.pi-lens/bin`), `PATH` second, otherwise it downloads the tool. Downloads
  are switched off only by environment variables —
  `PI_LENS_DISABLE_TOOL_INSTALL=1` and `PI_LENS_DISABLE_LSP_INSTALL=1`; no
  config key exists.
- **Its tools differ from `format.sh`**, and the choice is built in:

  | Language | `format.sh` / OpenCode (Mason)                    | pi-lens                                         |
  | -------- | ------------------------------------------------- | ----------------------------------------------- |
  | Go       | goimports, gofumpt, golines; golangci-lint always | gofmt; golangci-lint only with a project config |
  | Python   | isort, black; flake8                              | ruff; black only with a project config          |
  | JS/TS    | eslint_d, prettier                                | biome or prettier, only with a project config   |

- **Action — split the job so all three agents use the same Mason binaries:**
  - pi-lens keeps diagnostics and type checks (gopls, pyright,
    typescript-language-server are in Mason), with its own formatting off
    (`format.enabled: false` in `~/.pi-lens/config.json`) and both install
    switches set.
  - A small devgeta Pi extension runs the same formatter script Claude uses on
    Pi's `tool_result` event after `edit`/`write`. That event can rewrite the
    tool result, so lint findings reach the model the way Claude's
    `additionalContext` does. Pi loads a local-path package without copying it
    (`pi install <dir>`), so devgeta keeps the extension in a directory it owns.
  - `format.sh` moves to an agent-neutral location (`~/.config/devgeta/`, as
    `output-budget-run.sh` already did), so three agents call one script.
- **Open question:** whether pi-lens reads the install switches and `PATH` at
  call time (the devgeta extension can set them) or at load time (they must be
  in Pi's launch environment).
- **Unrelated bug found:** `~/.local/share/nvim/mason/bin` on the maintainer's
  machine has no `eslint_d` and no `clang-format`, yet `format.sh` and
  `configs/opencode/opencode.json.tmpl` call both. JS linting and C formatting
  are silently skipped today.

### `pi-schedule` — conditional

Scheduled prompts, shell checks, notifications and bounded polls. The
replacement for `pi-schedule-prompt`.

- 0.5.0 · 2 stars · 1 open issue · 19 commits 90d · 1 contributor · MIT · repo
  created 2026-07-17
- Best design checked: jobs are read-only by default (`tier`), a record of every
  run, no overlapping runs of the same job, a policy for runs missed while Pi
  was closed, fire caps.
- Active — a release every 1–3 weeks since 2026-07-20, 18 closed issues/PRs —
  but one maintainer and almost no users. Pi 1.5 compatibility is unverified
  (peer range `*`, last release 2026-10-19).
- **Action:** add only if the sandbox run shows it loading and firing on Pi 1.5;
  otherwise list it in `docs/apps/pi.md` as optional. Re-check its maintenance
  at each devgeta release.

---

## Considered, not added

| Library               | Why not                                                                                                                                                                |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `context-mode`        | 33,564 stars, very active, but Elastic-2.0 (not open source) and overlaps rtk and devgeta's output-budget hook. Revisit when the token-efficiency tooling is reworked. |
| `pi-cc-loop`          | Copies Claude Code's `/loop` and `CronCreate` tool names, the closest match to Claude. Three weeks old, 0 stars, one author, docs in Chinese.                          |
| `@henryqw/pi-cron`    | Active (6 contributors) but requires its own `@henryqw/pi-subagent` and `@henryqw/pi-task-models` — a second subagent system beside `pi-subagents`.                    |
| `@bacnh85/pi-cron`    | Declares `@earendil-works/pi-coding-agent >=0.84.3 <1.2.0`; no Pi 1.5 support.                                                                                         |
| `@tintinweb/pi-tasks` | Claude Code-style task tracking, but same author as the unmaintained `pi-schedule-prompt`.                                                                             |

---

## Shared work

### Install order

1. ~~`npm:pi-subagents`~~ ✅ done
2. ~~`npm:pi-web-access`~~ ✅ done
3. `npm:@plannotator/pi-extension`
4. `npm:pi-mcp-adapter`
5. `npm:@juicesharp/rpiv-ask-user-question`
6. `npm:@juicesharp/rpiv-todo`
7. ~~`npm:pi-powerline-footer`~~ ✅ done
8. `npm:pi-stalled-agent-recovery`
9. `npm:pi-openrouter-advanced-provider`
10. `npm:pi-openrouter-request-details`
11. `npm:pi-thinking-reformatter`
12. `npm:pi-schedule` — only if the sandbox run passes
13. `npm:pi-lens` — always last

### Live install output

Nothing in devgeta draws a multi-line live region today: `dge` wraps `dg task`,
`review-run` prints a heartbeat line every 30 seconds
(`internal/tooling/task/reviewprogress.go`), and `pkg/progress` redraws one
line.

- A rolling region of about 8 lines in `pkg/progress`, redrawn in place on a
  terminal. On success it collapses to one `✓ npm:<name>` line; on failure the
  last lines stay on screen. Plain lines when output is not a terminal.
- A stderr line callback in `cmd.CommandParams` — only stdout has one
  (`OnStdoutLine`), and npm writes progress to stderr.
- **Open question:** what `pi install` actually prints. If it is close to
  silent, the region shows only an elapsed timer.

### Verification still needed

A sandbox run that never touches the real machine: install Pi with npm into a
scratch prefix, point `HOME` and `PI_CODING_AGENT_DIR` at scratch, then:

1. run `pi install` for each extension and record what it prints (stdout and
   stderr separately) and how long it takes;
2. confirm every extension loads on Pi 1.5 — `pi-schedule` especially — and
   whether `subagentOnlyExtensions` accepts `npm:pi-web-access` so the
   foreground `researcher` gets the web tools;
3. check whether pi-lens honors the install switches and `PATH` set from an
   extension at load time;
4. look at Pi's `system` theme and the footer inside tmux.

Node on the maintainer's machine is v26.10.0; Pi needs 22.19 or later.

## Decisions this will need

- Amend ADR-0059 §7 and ADR-0060 §8 ("no themes, no formatter parity"): devgeta
  would now write two things of its own for Pi — the footer's `theme.json` and a
  local-path Pi package holding the formatter extension.
- ADR-0060 §1 ("devgeta never writes Pi's settings") stays true: `pi install`
  writes the settings, including for the local package.
- Whether a one-maintainer package (`pi-schedule`) may be a default.
