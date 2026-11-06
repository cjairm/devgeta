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

| Library                                          | Action                                 | Maintenance            |
| ------------------------------------------------ | -------------------------------------- | ---------------------- |
| ~~`pi-subagents`~~ ✅ done                       | Keep                                   | Well maintained        |
| ~~`pi-web-access`~~ ✅ done                      | Keep                                   | Well maintained        |
| ~~`pi-powerline-footer`~~ ✅ done                | Keep, plus a color override            | Well maintained        |
| ~~`pi-stalled-agent-recovery`~~ ✅ done          | Keep (maintainer's choice)             | Too new to judge       |
| ~~`pi-openrouter-advanced-provider`~~ ✅ done    | Keep (maintainer's choice)             | Too new to judge       |
| ~~`pi-openrouter-request-details`~~ ✅ done      | Keep (maintainer's choice)             | Too new to judge       |
| ~~`pi-thinking-reformatter`~~ ✅ done            | Keep (maintainer's choice)             | Too new to judge       |
| ~~`pi-schedule-prompt`~~ ✅ done                 | **Drop**                               | Not maintained         |
| ~~`pi-mcp-adapter`~~ ✅ done                     | **Add**                                | Well maintained        |
| `@plannotator/pi-extension`                      | **Not added** — overlaps Superpowers   | Well maintained        |
| ~~`@juicesharp/rpiv-ask-user-question`~~ ✅ done | **Add**                                | Well maintained        |
| ~~`@juicesharp/rpiv-todo`~~ ✅ done              | **Add**                                | Well maintained        |
| ~~`pi-lens`~~ ✅ done                            | **Add**, last in order, formatting off | Well maintained        |
| `pi-schedule`                                    | **Add only if the sandbox run passes** | Active, one maintainer |

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

The four packages below share one author and were read together on 2026-11-06
(1.0.0 tarballs, `npm pack --ignore-scripts`). All four have no dependencies
and no install scripts, use no `child_process`, `eval` or dynamic imports, and
write no files; they read only their own `~/.pi/agent/<name>.json` settings
file (the OpenRouter provider also reads Pi's `auth.json` for an OpenRouter
key). They hard-code `~/.pi/agent`, so they ignore `PI_CODING_AGENT_DIR`.

### ~~`pi-stalled-agent-recovery`~~ — done 2026-11-06

Retries empty or thinking-only responses and breaks loops of the same failing
tool call. Works with any provider.

- 1.0.0 · 0 stars · 0 open issues · 2 commits 90d · 1 contributor · MIT · repo
  created 2026-09-26
- Does not meet the "well maintained" bar: one author, new, unused. Kept by the
  maintainer's choice.
- **Source read:** 365 lines, no network access. On `agent_settled` it sends a
  hidden follow-up message that starts a new turn: up to 5 retries for an empty
  or thinking-only reply, and up to 3 nudges once the same tool call has failed
  5 times in a row, then it stops intervening. Budgets reset on a reply with
  text or an abort. Each retry is a paid request. Limits are configurable in
  `~/.pi/agent/stalled-agent-recovery.json`.
- **Action:** done — added to `piExtensions` after `pi-powerline-footer`, with
  a `docs/apps/pi.md` section. No devgeta config.

### ~~`pi-openrouter-advanced-provider`~~ — done 2026-11-06

OpenRouter provider: live model catalog for the account, per-model provider
routing, real context windows.

- 1.0.0 · 1 star · 1 open issue · 17 commits 90d · 1 contributor · MIT · repo
  created 2026-09-26
- Same author and same caveat as `pi-stalled-agent-recovery`.
- **Correction — it is not inert without OpenRouter.** On every
  `session_start` it fetches the model list from `openrouter.ai/api/v1/models`
  (the public list when there is no key) and registers the provider. With
  selection on (the default), `before_agent_start` fetches OpenRouter's
  frontend catalog (`/api/frontend/v1/catalog/models`, cached 24h) for the
  current model, whatever its provider. Without a key these requests carry no
  credentials and no prompt text. `"selection": { "enabled": false }` in
  `~/.pi/agent/openrouter-advanced-provider.json` stops the catalog lookup;
  only removing the extension stops the session-start fetch.
- **Source read:** 2,800 lines of TypeScript. Network access goes only to
  `openrouter.ai`; the key comes from `OPENROUTER_API_KEY`, Pi's model
  registry, or an `sk-or-` entry in `~/.pi/agent/auth.json`.
- **Subagent model guard — kept as documentation.** Shipping
  `subagents.modelScope` by default means writing Pi's `settings.json`, which
  needs an ADR-0060 §1 amendment, so it is not done here. `docs/apps/pi.md`
  now tells OpenRouter users to add the opt-in snippet. Still open for the
  maintainer: whether to make it a default.
- **Action:** done — added to `piExtensions` after
  `pi-stalled-agent-recovery`, with a `docs/apps/pi.md` section covering the
  network calls. No devgeta config.

### ~~`pi-openrouter-request-details`~~ — done 2026-11-06

Shows which provider and model OpenRouter actually served, with per-turn cost
cards. Does nothing unless you use OpenRouter.

- 1.0.0 · 0 stars · 0 open issues · 2 commits 90d · 1 contributor · MIT · repo
  created 2026-09-26
- Same author and same caveat.
- **Source read:** 1,200 lines. Its `message_end` handler returns early unless
  `provider === "openrouter"`. It reads response streams only from the
  `openrouter.ai` host. On session start it fetches prices from
  `openrouter.ai/api/v1/models/<id>/endpoints` only when the current model id
  has the `author/model` form, so a non-OpenRouter model normally triggers no
  request.
- **Action:** done — added to `piExtensions` after
  `pi-openrouter-advanced-provider`, with a `docs/apps/pi.md` section. No
  devgeta config.

### ~~`pi-thinking-reformatter`~~ — done 2026-11-06

Fixes the spurious whitespace and line breaks in thinking blocks from
DeepSeek-style models. Not replaced by `pi-lens`: that one checks files the
agent writes, this one fixes how model output is displayed.

- 1.0.0 · 0 stars · 0 open issues · 2 commits 90d · 1 contributor · MIT · repo
  created 2026-09-26
- Same author and same caveat.
- **Source read:** 210 lines, no network or file access. It is not limited to
  any model: it checks every thinking block and rewrites only text that
  matches its broken-spacing patterns. The rewrite applies to the stored
  message too (`message_end`), not just the display, so the repaired text is
  what goes back into the model's context.
- **Action:** done — added to `piExtensions` after
  `pi-openrouter-request-details`, with a `docs/apps/pi.md` section. No
  devgeta config.

---

## Drop

### ~~`pi-schedule-prompt`~~ — done 2026-11-06

Cron-like recurring and one-shot prompts.

- 0.4.1 · 122 stars · 11 open issues · last commit 2026-06-23 · MIT
- **Not maintained.** Two open bugs matter:
  - #23 (2026-09-20, no reply): crash on Pi 0.86 —
    `Cannot read properties of undefined (reading 'runtime')`.
  - #12 (2026-05-05): scheduler messages with empty content corrupt the parent
    session, so every later request fails with a 400. A user posted a verified
    three-line fix in May; it was never merged.
- **Action:** done — it left `piExtensions` and the `docs/apps/pi.md` table
  when the set was restarted (4dc0252), and v1.44.0 was the last release to
  install it. Configure only adds, so `docs/apps/pi.md` now tells existing
  users to run `pi remove npm:pi-schedule-prompt`. Replacement: `pi-schedule`
  (conditional, below).

---

## Add

### ~~`pi-mcp-adapter`~~ — done 2026-11-06

MCP support that starts a server on first use and stops it after 10 idle
minutes. Pi 1.5's built-in MCP starts every enabled server at session start and
keeps all of them running.

- 7.0.1 (2026-11-04) · 1,642 stars · 0 open issues, 0 open PRs · 100+ commits
  90d · 90 contributors · MIT · repo created 2026-01-19. In the 30 days to
  2026-11-06: 84 commits, 70 by the author (who also wrote `pi-subagents` and
  `pi-web-access`) and 14 by 10 others. The 15 most recent issues were all
  closed, most within a day.
- Pi 1.5 support is covered but not tested: peer `@earendil-works/pi-ai`
  `^1.0.0`, built against Pi 1.0.0, docs written against Pi 1.3.0. It imports
  `SettingsManager` from `@earendil-works/pi-coding-agent`, which it does not
  declare as a peer, so it relies on Pi's loader to provide it. Sandbox step 2
  now also checks that the built-in-off write below works on Pi 1.5.
- **Install is low risk** (read from the registry): its native dependencies
  (`@napi-rs/keyring`, `recheck`) ship as prebuilt optional packages for darwin
  and linux on x64 and arm64, and nothing in its dependency tree has an install
  script. Unlike `pi-lens`, there is no build step to fail.
- **Open security report, fixed but not yet released:** #936 (2026-11-05)
  found that 7.0.1's `app-bridge.bundle.js` bundles a copy of the MCP SDK 1.25.2
  (CVE-2026-25536, CVE-2026-111767), which `npm audit` cannot see. The reporter
  found no way to exploit it in the adapter. The fix (#937) is on `main` since
  2026-11-05 and reaches users through `pi update` once released, since
  versions are not pinned (ADR-0060 §6).
- **Turns Pi's built-in MCP off itself** (read from `pi-builtin-mcp.ts`): once
  per adapter version it adds `"-builtin:mcp"` to `extensions` in Pi's user
  `settings.json`, unless a `builtin:mcp` entry is already there, so a user who
  turns the built-in back on in `pi config` keeps that choice. It never changes
  project settings. ADR-0060 §1 still holds — the adapter writes this key, not
  devgeta — and devgeta's own `theme` write (`selectTheme`) keeps every other
  key in that file. Removing the adapter leaves the built-in off.
- **Files it writes itself**, all under the Pi agent dir (it honors
  `PI_CODING_AGENT_DIR`, `agent-dir.ts`): `mcp-cache.json`,
  `mcp-onboarding.json`, a project-approvals file and `mcp.log`.
  `mcp-adapter.json` is written by URL installs, `init` and `/mcp-adapter
setup`.
- **Servers it reads with no setup:** `~/.config/mcp/mcp.json`,
  `~/.agents/mcp.json`, `~/.agents/mcp/mcp.json`, Pi's `mcp.json` files
  (`pi mcp add`), `mcp-adapter.json`, and project `.mcp.json` / `.pi/mcp.json`
  behind a per-project approval prompt.
- **Reusing Claude Code's setup:**
  - MCP servers: only after an import is added. Its `imports` setting accepts
    `claude-code`, plus `opencode`, `claude-desktop`, `cursor`, `codex`,
    `vscode`, `windsurf`; once listed, the host file is read at every start,
    not copied. Host configs are not loaded by default
    (`hostConfigDiscovery: "off"`). **Correction:** the `claude-code` import
    reads only the top-level `mcpServers` of `~/.claude.json` (plus
    `~/.claude/mcp.json` and `~/.claude/claude_desktop_config.json`), i.e.
    user-scope servers. Servers added for one project
    (`projects[<path>].mcpServers`) are not imported.
  - Claude Code plugins: partly. Its `claudePlugins` setting loads a plugin's
    MCP servers (root `.mcp.json`) and skills (`skills/**/SKILL.md`) from a
    listed local plugin directory. Hooks, commands and agents do not carry over.
  - Nothing is removed from Claude Code either way.
- **Devgeta adds no imports.** Devgeta ships no MCP servers to any agent
  (`configs/claude` and `configs/opencode` only deny edits to `.mcp.json`), so
  an import would bring in nothing devgeta set up. On the maintainer's machine
  it would import nothing at all: no user-scope servers in `~/.claude.json`, no
  `mcp` key in `opencode.json`. The adapter also rewrites `mcp-adapter.json`
  itself, the same reason devgeta stays out of `web-search.json`. And
  `/mcp-adapter setup` / `pi-mcp-adapter init` already find the host configs
  and show the file diff before writing.
- **The model can add a remote server with no prompt** (read from
  `executeInstall` in `index.ts`): `mcp({ action: "install", url })` saves an
  HTTPS (or loopback HTTP) server to the global `mcp-adapter.json` and connects
  it; only `target: "project"` asks. Devgeta does not set
  `allowInstall: false`: Pi has no permission layer by default, so the model
  can already write that file with its own write or bash tools. Documented as
  an opt-out.
- **OAuth on Linux needs Secret Service** (`docs/auth.md`): tokens go to the
  macOS Keychain or a Secret Service keyring, and with no unlocked keyring
  sign-in fails rather than falling back to plaintext. Local servers and
  header or bearer-token servers are unaffected. Devgeta installs no keyring;
  documented.
- **Trade-off:** bulk MCP work can cost up to 5.5× more tokens than the built-in
  (2.9× with `"+codemode"` in Pi's `defaultTools`); single lookups cost about
  the same (the adapter's own measurement, Pi 1.3.0). `defaultTools` lives in
  Pi's `settings.json`, so `docs/apps/pi.md` shows it as an opt-in snippet.
- **pi-subagents interaction:** foreground children never load the parent's
  extensions, so they get no MCP tools; agents that need them must run with
  `async: true`. Documented.
- **Action:** done — added to `piExtensions` after `pi-web-access`, with a
  `docs/apps/pi.md` section covering the above. No devgeta config.

### ~~`@juicesharp/rpiv-ask-user-question`~~ — done 2026-11-06

A structured question tool with typed options, the closest match to Claude
Code's AskUserQuestion.

- 2.15.0 · 945 stars (monorepo `juicesharp/rpiv-mono`) · 83 open issues · 100+
  commits 90d · MIT. Rechecked 2026-11-06: last push and release 2026-10-24,
  after five releases on 2026-10-23. The 44-contributor figure was not
  re-confirmed.
- Its peer `@juicesharp/rpiv-i18n` only translates the interface and is
  optional. That matters because Pi does not install peer dependencies for
  managed npm packages (`docs/packages.md` in the Pi package). Its other peers
  (`typebox`, `pi-tui`, `pi-coding-agent`) are the ones `pi-subagents` also
  declares, which Pi provides.
- **No duplication** (read from the published packages): Pi core registers only
  `read`, `bash`, `edit`, `write`, `grep`, `find`, `ls`; none of the other
  curated extensions registers a question tool; Superpowers names no question
  tool for Pi (`using-superpowers/references/pi-tools.md`).
- **One tension:** its built-in guidance tells the model to ask "whenever the
  request is ambiguous", while Superpowers' `brainstorming` wants open
  questions first and a menu only when the user is stuck. Claude Code has the
  same tension with AskUserQuestion and the skill steers it there. The
  `guidance.promptGuidelines` setting can tune it per user; devgeta ships none.
- In non-interactive runs it removes itself from the tool list. Its collapse
  key (Ctrl+]) is registered per dialog, so it does not take Pi's editor
  binding for that key (`tui.editor.jumpForward`) the rest of the time.
- **Action:** done — added to `piExtensions` after `pi-mcp-adapter`, with a
  `docs/apps/pi.md` section. No devgeta config: its config file is optional and
  only read.

### ~~`@juicesharp/rpiv-todo`~~ — done 2026-11-06

A todo tool shown as a live overlay that survives `/reload` and compaction, the
closest match to Claude Code's todo list.

- 2.15.0 · same monorepo and numbers as above · 140 releases
- **Fills a gap Superpowers names:** `using-superpowers/references/pi-tools.md`
  says Pi has no task-list tool and to use an installed one, otherwise
  `TODO.md`; `executing-plans` and `subagent-driven-development` mark todos per
  task. Pi ships a todo tool only as an unloaded example
  (`examples/extensions/todo.ts`). With `@plannotator/pi-extension` not added,
  no other checklist widget overlaps it.
- Its collapse key, Ctrl+Shift+T, has no Pi default (`app.session.tree` uses it
  only in a docs example). Whether Alacritty and tmux pass it through is
  untested; `/todos` works regardless.
- **Action:** done — added to `piExtensions` after
  `rpiv-ask-user-question`, with a `docs/apps/pi.md` section. No devgeta
  config.

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
    `additionalContext` does. ~~Pi loads a local-path package without copying
    it (`pi install <dir>`)~~ — see "Where the extension lives" below.
  - `format.sh` moves to an agent-neutral location (`~/.config/devgeta/`, as
    `output-budget-run.sh` already did), so three agents call one script.
- **Open question, closed (2026-11-06, 4.4.9 source):** pi-lens reads the
  install switches at call time — `isToolInstallDisabled()` checks
  `process.env.PI_LENS_DISABLE_TOOL_INSTALL` on every install attempt. Pi runs
  every extension in one process, so a devgeta extension that sets them (and
  prepends the Mason bin to `process.env.PATH`) when it loads is enough. Side
  effect: the `bash` tool's children inherit that `PATH` too.
- **Where the extension lives (Pi 1.5 source, `package-manager.js`
  `resourcePrecedenceRank`):** `tool_result` handlers chain in extension load
  order, and Pi sorts extensions as project files → user files
  (`~/.pi/agent/extensions/*.ts` or `*.js`; `FILE_PATTERNS.extensions` is
  `/\.(ts|js)$/`) → npm packages → built-ins. A file in
  `~/.pi/agent/extensions/` therefore always runs before pi-lens, so pi-lens
  diagnoses the formatted bytes. A `pi install <dir>` local package is ranked
  with the npm packages instead, and its order against pi-lens would depend on
  the `packages` list. Use the auto-discovered file. It also hot-reloads with
  `/reload`.
- **pi-lens read-before-edit guard vs an outside formatter:** the guard blocks
  an edit when the file's bytes differ from what the conversation last read or
  wrote. pi-lens records "what was written" by hashing the file on disk in its
  own `tool_result` handler (`observeAuthoredBytes`), and that runs after ours,
  so formatted bytes should count as the agent's. Not run yet — the sandbox run
  must check an edit → format → second edit on the same file is not blocked.
- **The agent must be told the file changed.** Pi's `edit` needs an exact
  `oldText` match, so a reformat silently breaks the next edit. pi-lens
  defers its own formatting to `agent_end` for exactly this reason. The
  existing `@ian-pascoe/pi-formatter` (0.6.9, 11 stars, 4 contributors, 35
  commits 90d) solves it by appending `Formatted by <tool>: lines N–M changed`
  plus a short diff to the tool result. The devgeta extension should do the
  same, alongside the lint findings. `@ian-pascoe/pi-formatter` itself was not
  chosen: it would need the per-language formatter table copied into Pi's
  `settings.json` (a third copy beside `format.sh` and `opencode.json.tmpl`),
  and it carries no lint feedback.
- **`hire2` exception:** `format.sh` skips HTML formatting for one private
  repo. The maintainer chose to keep it (grandfathered in
  `TestEmbeddedConfigsNameNoPersonalCheckoutPaths`), so it stays in Claude's
  wrapper and is not carried into the shared script, where it would spread
  to Pi.
- **Sources for the facts above** (Pi 1.5.0 and pi-lens 4.4.9, unpacked with
  `npm pack --ignore-scripts`):
  - Pi `docs/extensions.md`: "Handler Composition" and `tool_result`
    (middleware chaining, partial patches, `structuredContent` dropped when
    `content` is replaced without it), `pi.exec(command, args, {signal,
timeout})`, "Extension Locations".
  - Pi `dist/core/package-manager.js`: `resourcePrecedenceRank`,
    `FILE_PATTERNS`, `toResolvedPaths` (sort by rank).
  - Pi `dist/core/tools/edit.d.ts` / `write.d.ts`: inputs carry `path`.
    `isEditToolResult` / `isWriteToolResult` are exported from
    `dist/core/extensions/index.d.ts`.
  - pi-lens `dist/chunk-VCRVNLE7.js` `isToolInstallDisabled()` (reads
    `process.env` per call).
  - pi-lens `dist/chunk-OUYAUXAT.js`: `recordWritten`, `creditAuthorship`,
    `observeAuthoredBytes` (hashes the file on disk when no hash is passed).
  - pi-lens `docs/features.md` ("Formatters", "Read-Before-Edit Guard", bus
    events: pi-lens publishes, does not consume) and `docs/globalconfig.md`
    (`format.enabled`, `autofix.enabled`).
  - pi-lens `docs/configuration.md`: global config path table
    (`PI_LENS_CONFIG_PATH`, `~/.pi-lens/config.json`,
    `$PI_CODING_AGENT_DIR/extensions/pi-lens.json`).
- **Plan:** [ADR-0063](../decisions/ADR-0063-pi-formats-through-devgetas-shared-mason-script-and-pi-lens-only-diagnoses.md),
  cycle [2026-11-06-pi-lens-and-mason-formatting](../plans/cycles/2026-11-06-pi-lens-and-mason-formatting.md).
  Decision taken: no pi-lens downloads; Mason is the only tool source.
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

| Library                     | Why not                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `context-mode`              | 33,564 stars, very active, but Elastic-2.0 (not open source) and overlaps rtk and devgeta's output-budget hook. Revisit when the token-efficiency tooling is reworked.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `pi-cc-loop`                | Copies Claude Code's `/loop` and `CronCreate` tool names, the closest match to Claude. Three weeks old, 0 stars, one author, docs in Chinese.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `@henryqw/pi-cron`          | Active (6 contributors) but requires its own `@henryqw/pi-subagent` and `@henryqw/pi-task-models` — a second subagent system beside `pi-subagents`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `@bacnh85/pi-cron`          | Declares `@earendil-works/pi-coding-agent >=0.84.3 <1.2.0`; no Pi 1.5 support.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `@tintinweb/pi-tasks`       | Claude Code-style task tracking, but same author as the unmaintained `pi-schedule-prompt`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `@plannotator/pi-extension` | Well maintained (0.28.51, 100+ contributors), but its plan mode is a second planning workflow beside the Superpowers skills (`brainstorming` → `writing-plans` → `executing-plans`), with its own instructions for how to write and submit a plan. Running both gives the model two sets of rules. What is given up: the browser UI for commenting on plans and diffs (`/plannotator-review`, `/plannotator-annotate`), and a write lock while planning. The lock is partial: only edits are limited to markdown files, bash stays unrestricted. Also on by default, with no config key to turn it off: a daily anonymous usage ping to `ping.plannotator.ai`, which only `DO_NOT_TRACK=1` or `NO_ANON_PING=1` stops. |

---

## Shared work

### Install order

1. ~~`npm:pi-subagents`~~ ✅ done
2. ~~`npm:pi-web-access`~~ ✅ done
3. ~~`npm:pi-mcp-adapter`~~ ✅ done
4. ~~`npm:@juicesharp/rpiv-ask-user-question`~~ ✅ done
5. ~~`npm:@juicesharp/rpiv-todo`~~ ✅ done
6. ~~`npm:pi-powerline-footer`~~ ✅ done
7. ~~`npm:pi-stalled-agent-recovery`~~ ✅ done
8. ~~`npm:pi-openrouter-advanced-provider`~~ ✅ done
9. ~~`npm:pi-openrouter-request-details`~~ ✅ done
10. ~~`npm:pi-thinking-reformatter`~~ ✅ done
11. `npm:pi-schedule` — only if the sandbox run passes
12. `npm:pi-lens` — always last

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
   foreground `researcher` gets the web tools; for `pi-mcp-adapter`, that the
   first start adds `"-builtin:mcp"` to `settings.json` and Pi then loads no
   built-in MCP;
3. check whether pi-lens honors the install switches and `PATH` set from an
   extension at load time;
4. look at Pi's `system` theme and the footer inside tmux.

Node on the maintainer's machine is v26.10.0; Pi needs 22.19 or later.

## Decisions this will need

- Amend ADR-0059 §7 and ADR-0060 §8 ("no themes, no formatter parity"): devgeta
  would now write two things of its own for Pi — the footer's `theme.json` and
  the formatter extension. Taken up by ADR-0063, which uses an auto-discovered
  extension file instead of a local-path package (load order, see `pi-lens`).
- ADR-0060 §1 ("devgeta never writes Pi's settings") stays true: the extension
  file is not a settings entry, and pi-lens's config is not Pi's settings.
- Whether a one-maintainer package (`pi-schedule`) may be a default.
