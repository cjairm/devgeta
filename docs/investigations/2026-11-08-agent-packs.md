# Investigation — Agent packs: installing Superpowers, Ponytail and devgeta's own context into every coder

**Date:** 2026-11-08
**Status:** Concluded, except the session-level probes. Everything the CLIs
can show was probed in a sandbox (see [Probe results](#probe-results-2026-11-08))
and the three ADRs were accepted. Whether each coder's session actually
receives the Superpowers bootstrap, and whether Pi's `before_agent_start` text
reaches the model, still need a model login; they are Manual Verification 2, 4
and 5 in [the cycle doc](../plans/cycles/2026-11-08-agent-packs.md). A "no"
there reopens the ADRs.
**Feeds:** the ADRs in [Decisions this will need](#decisions-this-will-need), then a cycle for `dg agent-pack`
**Follows:** [2026-11-06-skills-and-agents-reset.md](2026-11-06-skills-and-agents-reset.md)
(concluded). Its item 5 and its open items moved here. Work happens on `agentsv2`.

## Question

After the reset, devgeta installs no agent content. The goal is to install
[obra/superpowers](https://github.com/obra/superpowers),
[DietrichGebert/ponytail](https://github.com/DietrichGebert/ponytail) and
devgeta's own `DEVGETA.md` into Claude Code, OpenCode and Pi so that they:

- **work fully,** with every hook, bootstrap and command upstream relies on;
- **stay controlled:** skills can be removed or customized without editing
  upstream files;
- **are pinned:** each one runs a known version and is checked before a newer
  one replaces it;
- **are installed by default** by `dg install`.

Questions:

1. What is the command and its flags?
2. Should each coder load a pack through its own plugin or package system, or
   should devgeta copy files into folders?
3. How do skill removal and customization work in each coder?
4. How do pinning and upgrades work?
5. How does `DEVGETA.md` reach all three coders?

## Scope

- **In:**
  - The command.
  - Delivery to the three coders.
  - Removing and customizing skills.
  - Pinning and upgrade checks.
  - Superpowers, Ponytail and a `devgeta` pack.
  - Carried from the reset investigation:
    - `DEVGETA.md` v2 and how Pi gets it.
    - The OpenCode folder check.
    - Widening parity to every coder.
- **Out:**
  - Packs for apps other than the coders, such as Neovim. The design below
    leaves room for them (finding 1).
  - Hooks and permissions that devgeta itself ships (ADR-0067).
  - Other skill sets, such as `humanizer` (moved to Verification).

## Method

- **Sources, read on 2026-11-08:**
  - Pi docs: [packages](https://pi.dev/docs/latest/packages),
    [skills](https://pi.dev/docs/latest/skills),
    [prompt templates](https://pi.dev/docs/latest/prompt-templates), and the
    settings, CLI, configuration and extensions pages.
  - OpenCode docs: [skills](https://opencode.ai/docs/skills/),
    [commands](https://opencode.ai/docs/commands/), and the agents, plugins,
    config and [rules](https://docs.opencode.ai/docs/rules/) pages.
  - Claude Code docs: [skills](https://code.claude.com/docs/en/skills),
    [commands](https://code.claude.com/docs/en/commands), plugins, plugin
    marketplaces, the plugin reference, the plugin CLI reference, plugin
    loading, and the marketplace reference.
  - Superpowers at tag `v7.0.2` and Ponytail at tag `v5.3.3`: manifests,
    hooks and per-coder glue files, read with `gh api`.
- **Installed coders on the maintainer's machine,** updated after the
  2026-11-08 work:
  - Claude Code 2.1.319, pinned.
  - OpenCode 2.2.6, pinned, with devgeta's 2.x config deployed and checked
    live.
  - Pi is not installed: no binary and no managed install, only
    `~/.pi/agent/` config. `dg configure pi` fails until
    `dg install --only pi` runs.
  - Node 26.10.0.
- **Newest releases, checked 2026-11-08:**
  - Claude Code 2.1.319 (`gh api repos/anthropics/claude-code/releases/latest`).
  - OpenCode 2.2.6 (`gh api repos/anomalyco/opencode/releases`). The repo's
    `releases/latest` endpoint returns v1.19.5, so read the release list, not
    that endpoint.
  - Pi 1.5.0 (`npm view @earendil-works/pi-coding-agent version`).
  - Superpowers v7.0.2 and Ponytail v5.3.3.
- **Criteria, set before looking:**
  1. Each pack works as upstream intends in each coder, hooks and bootstraps
     included.
  2. devgeta's fetched copy of upstream stays byte-for-byte identical.
     Removals and customizations live in a separate layer applied on top, so
     moving to a new version means fetching it and re-applying that layer.
  3. Every coder gets the same capabilities. If something can't work in one
     coder, devgeta says so where the user can see it. It never hides the gap
     and never forces a workaround.
  4. Pinned to a tag and commit. Nothing updates behind devgeta's back.
  5. Removal deletes only what devgeta installed.
- **Not done:**
  - Nothing about packs was installed or run. All per-coder pack commands
    below come from the docs and still need a sandbox probe (see
    [Verification](#verification-still-needed)).
  - What _was_ probed, during the OpenCode 2 move, is recorded under
    [Since this was written](#since-this-was-written-2026-11-08) and
    finding 3.
  - Parts of the Claude skills page and settings reference were summarized by
    the fetch tool, not quoted. Those claims are marked _(summary)_.

## Summary

| Item                                         | Action         | Why, in a few words                                                                              |
| -------------------------------------------- | -------------- | ------------------------------------------------------------------------------------------------ |
| `dg agent-pack`                              | Adopt          | Room for packs aimed at other apps later; "plugin", "package", "extension" are taken             |
| Native install of a devgeta-built tree       | Adopt          | Only way to keep upstream hooks working _and_ remove or customize skills                         |
| Upstream's own install commands              | Reject         | Claude can't remove one plugin skill; official marketplace auto-updates; no customizing          |
| Flat copy into skill folders (the old way)   | Reject         | Drops Superpowers' bootstrap and all of Ponytail's hooks; loses `superpowers:` names in Claude   |
| Customization (override layer) when building | Adopt          | One place, same result in all three coders, fetched upstream untouched                           |
| Removing skills                              | Only if needed | None removed by default; `--exclude` exists for the case that needs it                           |
| Pin tag + commit; `dg agent-pack check`      | Adopt          | Local sources never auto-update, so devgeta's pin is the only one                                |
| Pin the coders too, at newest                | Done           | Claude Code 2.1.319 and OpenCode 2.2.6 pinned; Pi is a reference only (no version option)        |
| `DEVGETA.md` to every coder                  | Adopt          | Claude: `@DEVGETA.md` added if missing, left if present. OpenCode: `instructions`. Pi: extension |
| Default in `dg install`                      | Adopt          | Maintainer's decision; skippable                                                                 |
| Ponytail statusline nudge                    | No action      | devgeta already sets `statusLine`; Ponytail only nudges when none is set                         |

---

## Since this was written (2026-11-08)

The coder pins and the OpenCode 2 move landed first (`9309762`; user-facing
details in `docs/apps/opencode.md` and `docs/migrations/opencode-2.md`). What
they established, measured in a sandbox `HOME` with OpenCode 2.2.6 and then on
the maintainer's machine, that bears on packs:

- **Pins exist.** `constants.PinnedVersion` holds Claude Code 2.1.319 and
  OpenCode 2.2.6; both coders' self-updates are off (`DISABLE_AUTOUPDATER`,
  `"update": "disable"`). Pi has no version option; its entry is a reference.
- **OpenCode 2 discovers plugin folders by itself** in
  `~/.config/opencode/plugins/` (and a project's `.opencode/plugins/`). A
  folder's `server.js` (or `index.js`) runs in the server, its `tui.js` in
  each terminal client. Extra named exports are tolerated. A plugin listed in
  `opencode.json`'s `plugins` must be a folder, given as an absolute path.
- **One shared background service.** Every OpenCode terminal connects to one
  `opencode serve --service`, and server plugins run inside it with the
  environment of whichever terminal started it. Per-pane behaviour belongs in
  a TUI plugin; anything a server plugin reads from its environment is set on
  the service (`opencode service set env …`).
- **No dependency install for plugins that import nothing.** OpenCode 2
  created no `package.json` or `node_modules` for them, and started in 0.41s
  with or without a leftover 1.x `package.json`.
- **Permission rules are an ordered, last-match-wins list,** and a server
  plugin can deny through the `permission` `evaluate` hook. The `edit`,
  `write` and `patch` tools all ask for the `edit` action with every target
  path, so one hook covers a multi-file patch.
- **The tools' inputs changed:** `edit` and `write` take `path`, not
  `filePath`.

---

## Findings

### 1. Command

- **Decision input from the maintainer (2026-11-08):**
  - Use `dg agent-pack`, so that packs for other apps (Neovim, for example)
    can get their own command later.
  - Support `--coder`.
  - Let `dg install` install the default packs.
- **Proposal:**

  ```
  dg agent-pack list                                    # packs, versions, and what each coder got or skipped
  dg agent-pack install superpowers [--coder pi,claude] [--exclude <skill>,...]
  dg agent-pack uninstall superpowers [--coder ...]
  dg agent-pack update [name]                           # to the version this devgeta release pins
  dg agent-pack check <name> [--ref <tag>]              # build and validate a version without installing it
  ```

  - `--coder` narrows where a pack goes. The default is every coder devgeta
    has configured.
  - `--exclude` removes named skills, and is saved (finding 3). It is for the
    case that needs it; no skill is excluded by default. It replaces
    the `--thing=skill,command` idea: filtering by content type breaks packs,
    because Superpowers' bootstrap is a hook and Ponytail's whole ruleset is
    hooks. Removing a type removes the thing that makes the pack work.

- **`dg install`** installs the default packs (`devgeta`, `superpowers`,
  `ponytail`) into the coders it installed. It skips them with
  `dg install --skip agent-packs`. That is one category token; pack names
  stay out of the app namespace.
- **Reuse later:** the build-and-install engine (fetch pinned source → apply
  removals and wrappers → install natively) is not coder-specific. A future
  `dg nvim-pack` would reuse it with a different installer.

### 2. What the two upstreams need in order to work

Both repos are MIT licensed. Superpowers is at `v7.0.2` (2026-11-03) and
Ponytail at `v5.3.3` (2026-10-29), per `gh api repos/.../releases`. Neither is
just a folder of skills.

- **Superpowers:**
  - **Contents:** 15 skills. Since 5.1.0 there are no commands or agents
    (RELEASE-NOTES).
  - **Bootstrap:** it depends on the `using-superpowers` bootstrap being
    injected at session start and after compaction. Each coder does this
    differently:
    - Claude Code: a `SessionStart` hook, matcher `startup|clear|compact`,
      running `hooks/session-start` with bash (`hooks/hooks.json`).
    - OpenCode: a plugin, `.opencode/plugins/superpowers.js`. It registers
      `../../skills` relative to its own file: through the `config` hook on
      1.x and through `ctx.skill.transform()` on 2.x. It then injects the
      bootstrap.
    - Pi: a package. Its `package.json` has a `pi` key listing the extension
      `./.pi/extensions/superpowers.ts` and the skills folder `./skills`. The
      extension re-injects the bootstrap on `session_start`,
      `session_compact` and `context`.
  - **Naming:** skills call each other as `superpowers:<name>`. In Claude
    Code that is exactly the plugin namespace. The OpenCode and Pi glue tells
    the model to drop the prefix.
- **Ponytail:**
  - **Contents:** 6 skills (`ponytail`, `-review`, `-audit`, `-debt`,
    `-gain`, `-help`). The old devgeta copy had only 2.
  - **Ruleset:** the ruleset reaches the model through hooks, not just
    skills:
    - Claude Code: `SessionStart`, `SubagentStart` and `UserPromptSubmit`
      hooks that run `node`.
    - OpenCode: a plugin plus `.opencode/command/*.md`.
    - Pi: an extension that registers `/ponytail`, `/ponytail-review` and
      the others with `pi.registerCommand`.
  - **The "band":** it writes a CSV row per task to
    `~/.config/ponytail/band/<project>.csv` by default (`ledger: true` in
    `.claude-plugin/plugin.json`). A webhook is off unless configured.
  - **Statusline:** it reads Claude's `settings.json`. It asks the model to
    add Ponytail's statusline only when no `statusLine` is set, or when the
    one that is set points at a stale copy of Ponytail's own script
    (`hooks/ponytail-activate.js:73-152`). It never writes `settings.json`
    itself.
    - devgeta already sets one: `configs/claude/settings.json.tmpl:58` points
      `statusLine` at `~/.claude/statusline.sh`, deployed from
      `configs/claude/statusline.sh`. The maintainer's real settings have it
      too.
    - So Ponytail leaves it alone, which is the behaviour we want: add one
      when there is none, never touch an existing one. No action needed.
- **Verdict:** a copy of just the `skills/` folders, which is what devgeta
  did before the reset, loses the Superpowers bootstrap in every coder and
  all of Ponytail's behaviour. Criterion 1 needs each coder's own plugin or
  package system.

### 3. Delivery: build a devgeta-owned tree, install it natively

Each coder can load a bundle from a **local folder** and leave it in place:

| Coder       | Local install                                                                                                                                                                                                   | Can drop one skill?                                                                                             | Source                                         |
| ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| Claude Code | `claude plugin marketplace add <dir>`, then `claude plugin install <plugin>@<marketplace>`. Scriptable, `-s user`. A `directory` marketplace's relative-path plugins "load in place"                            | **No.** "Plugin skills are not affected by `skillOverrides`"; only the whole plugin can be disabled _(summary)_ | plugins/cli-reference, plugins/loading, skills |
| OpenCode    | An absolute folder path in `opencode.json`'s `plugins` list (2.x rejects file paths), or a folder under `~/.config/opencode/plugins/`, which 2.x discovers by itself. Skills: extra paths in the `skills` array | Yes: a `skill` permission rule with `effect: deny` hides and blocks it                                          | plugins, skills, config; probed 2.2.6          |
| Pi          | `pi install ./dir`: "Loaded from the resolved path without copying". Recorded in `~/.pi/agent/settings.json` `packages`                                                                                         | Yes: package filter `"skills": ["!pattern"]` (7.0.2 fixed a bug where `[]` loaded all of them)                  | packages, Superpowers RELEASE-NOTES            |

Claude Code can't drop one skill from a plugin, and no coder can customize a
skill without editing it. So removal and customization have to happen
**before** the coder sees the files:

1. **Fetch:** `git` clone at the pinned tag. Check that the commit equals the
   pinned SHA, and keep the clone read-only in devgeta's data directory.
2. **Build:** make a copy of the clone, then:
   - delete the excluded skills, if any;
   - lay the override layer on top. An override is a file at the same path as
     the upstream file it replaces, usually `skills/<name>/SKILL.md`, or a new
     file the skill needs.

   An override can take either of two forms, chosen per skill:
   - **Full replacement:** a `SKILL.md` written from scratch.
   - **Wrapper:** it says "follow `UPSTREAM.md`, with these changes", and the
     build keeps upstream's file beside it as `UPSTREAM.md` (reset finding 3).

   The wrapper is optional. Either way the override keeps upstream's skill
   name, so other skills' calls still reach it. A wrapper also survives
   upstream edits better. Every coder treats only `SKILL.md` as a skill:
   OpenCode doesn't search inside a folder that holds one, and Pi doesn't load
   loose `.md` files there either. So an `UPSTREAM.md` never loads as a second
   skill.

3. **Install:** point all three coders at the same stable path, for example
   `<data>/agent-packs/superpowers/`. The upstream glue resolves skills
   relative to its own file (`superpowers.js:26`; the Pi extension's
   `packageRoot`), so it finds the built tree, not upstream's.
   - Claude Code: a devgeta marketplace whose `.claude-plugin/marketplace.json`
     sits at `<data>/agent-packs/` and lists each pack with a relative
     source.
   - OpenCode: an absolute folder path in the `plugins` list of the
     `opencode.json` devgeta already generates. Each upstream already ships
     the folder form 2.x wants, and both import only Node's standard library
     and their own files (checked at `v7.0.2` / `v5.3.3`), so no dependency
     install is triggered:
     - Superpowers: the tree root. Its root `index.js` exists for exactly this
       ("OpenCode V2 hosts … require config plugin entries to be directories
       with an index entrypoint"; `~` is not expanded, so the path must be
       absolute).
     - Ponytail: the tree's `.opencode/plugins/` folder, whose `index.js`
       re-exports `ponytail.mjs`. That plugin parses the
       `.opencode/command/*.md` files itself (`parseCommandFile`), so the
       commands most likely need no copying; still to probe.
     - Copying a pack into `~/.config/opencode/plugins/<pack>/` would also be
       discovered, but `plugins/` is shared with the user's own plugins and a
       config entry keeps the built tree in one place for all three coders.
   - Pi: `pi install <path>`.
4. **Update:** build the new tree in a temp folder, check it, then swap it in
   with a rename. That keeps the swap atomic (CLAUDE.md §7).

Why this beats the other options:

- **Upstream's own install commands:**
  - No way to remove one skill in Claude Code.
  - Pins differ per coder, and the official Claude marketplace auto-updates by
    default ("on for official marketplaces such as `claude-plugins-official`",
    plugins/loading).
  - No customizing.
- **A flat copy:** fails criterion 1 (finding 2).
- **A wrapper inside a Claude plugin keeps the `superpowers:<name>`
  namespace,** so the "wrapper keeps the same name" rule from reset finding 3
  holds with no extra work.

Where the removals and customizations live:

- **Removals a user chose:** stored in `global_config.yaml` under the pack,
  and only when needed. They are the user's choice, not something devgeta
  ships.
- **Overrides a user wrote:** stored in
  `~/.config/devgeta/agent-packs/<pack>/`, laid out like the pack
  (`skills/<name>/SKILL.md`). The user owns them, and they win over
  devgeta's.
- **Overrides devgeta ships:** stored in `configs/agent-packs/<pack>/`.
  - These are the "devgeta needs" changes: making a skill use `dg worktree`,
    for example, or fitting devgeta's guards.
  - They ship to every user, so each must make sense to someone who has never
    seen this repo (principle 8).
  - The `using-git-worktrees` → `dg worktree` change qualifies, because
    `dg worktree` ships to everyone. A change that only suits this repo's
    workflow belongs in the user layer or in `CLAUDE.md`.
- **Build checks.** Any failure stops the build:
  - Every excluded or overridden skill still exists upstream.
  - No kept skill names an excluded skill, as `superpowers:<name>` or as a
    bare name. Removing `using-git-worktrees`, for example, breaks
    `executing-plans`.
  - An override's `name` matches upstream's. For a wrapper, its
    `description` also matches upstream's.
  - For a full replacement, the build records upstream's hash for that file.
    When the hash changes on a version bump, `check` lists the skill so
    someone re-reads the upstream change. A full replacement doesn't follow
    upstream on its own.
  - The files the installers need exist: `.claude-plugin/plugin.json`,
    `package.json` with its `pi` key and `main`, and the hook files `plugin.json`
    declares (`hooks/hooks.json` when it declares none; Ponytail has no
    `hooks/hooks.json`).

### 4. Pinning and upgrades

- **Evidence:**
  - Local-folder sources aren't auto-updated by any of the three coders:
    - Claude Code: a `directory` marketplace isn't cloned, and auto-update is
      off for third-party marketplaces (plugins/loading).
    - Pi: a local package isn't fetched.
    - OpenCode: a path entry isn't an npm or git spec.
  - So devgeta's pin is the only one, which meets criterion 4. The old pin
    procedure (`d7adb89:docs/guides/vendored-skills.md`) pinned
    Superpowers by commit `2ce8aa15` (v7.0.1) and patched it by hand. The
    wrapper layer replaces that patching.
- **Proposal:**
  - devgeta embeds a manifest per pack: repo, tag, full SHA, and devgeta's
    wrappers.
  - Moving to a new version is a devgeta change and needs a release. It
    involves:
    1. reading upstream's release notes since the pinned tag;
    2. running `dg agent-pack check <pack> --ref <tag>`, which builds into a
       temp folder and runs the finding 3 checks plus
       `claude plugin validate` when Claude Code is present;
    3. a smoke test in a sandbox for each coder (Verification 1–3);
    4. bumping the manifest.
  - `check` is a general command. Any user can use it to try a newer tag
    before devgeta ships it, so it doesn't break principle 8.
  - The coder pins went into `constants.PinnedVersion` (below). Whether pack
    pins join it or get their own manifest is for the cycle to decide; Pi's
    extensions are still unpinned.
- **The coders are pinned** (done 2026-11-08, `9309762`):

  | What        | Pin / reference | On the maintainer's machine |
  | ----------- | --------------- | --------------------------- |
  | Claude Code | 2.1.319         | 2.1.319                     |
  | OpenCode    | 2.2.6           | 2.2.6, 2.x config deployed  |
  | Pi          | 1.5.0 reference | not installed               |
  | Superpowers | v7.0.2          | not installed               |
  | Ponytail    | v5.3.3          | not installed               |
  - The pins live in `constants.PinnedVersion`, one field per coder, rather
    than in a manifest file. Pack pins can join the same place or a pack
    manifest; the cycle decides.
  - Claude Code's `DISABLE_AUTOUPDATER=1` also turns off automatic plugin
    updates, so a marketplace devgeta adds cannot move on its own either.
  - Pi always installs its latest release, so a pack's Pi side is checked
    against whatever Pi a user has. `dg agent-pack check` should report the
    Pi version it ran against.

- **Claude `version` caveat:** "a manifest that pins `"version": "1.0.0"`
  keeps every user on the cached copy until its author changes the string"
  (plugins/loading). That matters only if Claude copies a directory
  marketplace's plugins into its cache, and the docs say it loads them in
  place. Verify. If it does copy, the built tree's `plugin.json` version must
  change with every build, for example `7.0.2+dg.<hash>`, which would edit an
  upstream file.

### 5. The `devgeta` pack and `DEVGETA.md`

- **Evidence:**
  - Claude Code: "A `CLAUDE.md` at the plugin root isn't loaded as context"
    (plugin reference), so a plugin can't carry it.
  - Superpowers' `using-superpowers` treats `CLAUDE.md` and `AGENTS.md` as
    user instructions that override skills. Text loaded through `CLAUDE.md`
    therefore outranks the packs, which is the right order for devgeta's
    defaults.
  - Pi: an extension can add to the system prompt in `before_agent_start`.
    Superpowers' extension re-injects on `session_compact`.
  - OpenCode: `instructions` entries "are combined with your `AGENTS.md`
    files" (rules).
- **Decision input from the maintainer (2026-11-08):** in Claude Code, use the
  import. Read `~/.claude/CLAUDE.md`, add `@DEVGETA.md` if it isn't there,
  and leave the file alone if it is. The maintainer's `CLAUDE.md` is empty
  now; the old line is gone.
- **Proposal:**
  - The `devgeta` pack is a tree embedded in the binary. Its parts:
    - **`DEVGETA.md` v2**, from Appendix A of the reset investigation.
    - **Claude Code:** the file copied to `~/.claude/DEVGETA.md`, plus the
      `@DEVGETA.md` line added when missing.
    - **OpenCode:** an `instructions` entry pointing at its copy.
    - **Pi:** a package whose extension appends the file. This is the
      `devgeta-instructions.js` the reset investigation planned.
    - **Later, devgeta's own skills,** delivered as a plugin or package like
      the other packs.
  - On `dg agent-pack uninstall devgeta`, remove the line only if it is
    exactly the one devgeta adds, and say so in the output.
  - One difference from ADR-0058:
    - ADR-0058 added the line once and never again, so a user who deleted it
      stayed opted out.
    - "Add if missing" puts it back on every install or update. That is fine
      while installing the pack is the opt-in. A user who wants it gone should
      uninstall the `devgeta` pack, and the docs need to say so.

### 6. Parity: same capabilities, visible gaps

- **What the coders can load today:** all three load skills. Commands exist
  in all three:
  - Claude Code: merged into skills; "Custom commands have been merged into
    skills".
  - OpenCode: `commands/`.
  - Pi: prompt templates.

  Subagents exist in Claude Code and OpenCode. Pi has none built in; there is
  only an example extension. Neither upstream ships agents at these versions,
  so nothing is lost today.

- **OpenCode's shared service matters for packs too.** Superpowers' and
  Ponytail's OpenCode plugins are server plugins, so they run once per
  service, not per terminal. Superpowers injects its bootstrap per session
  (`ctx.session.hook`), which fits. Ponytail reads `PONYTAIL_DEFAULT_MODE`
  and its subagent matcher from the environment, which under the service
  means the service's environment; the pack docs need to say so, the same
  way devgeta's guard bypass does.

- **Rule:**
  - Install every part of a pack each coder can load, and never fake the
    rest.
  - `dg agent-pack list`, and the output of `install`, name anything a coder
    skipped and why.
  - CLAUDE.md §12, `agent-sync.md` and the parity tests widen from "the two
    agents" to "every coder", in the same cycle.

---

## Considered, not adopted

| Item                                                                                                      | Why not                                                                                                                |
| --------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `dg pack`                                                                                                 | Too general once packs for other apps exist (maintainer, 2026-11-08)                                                   |
| `--thing=skill,command` filter                                                                            | Hooks and extensions are what make these packs work; filtering by type breaks them. `--exclude` per skill instead      |
| Upstream's install commands (`/plugin install superpowers@claude-plugins-official`, `pi install git:...`) | No skill removal in Claude, auto-updates, no customizing (finding 3)                                                   |
| Flat copy into `skills/` (pre-reset)                                                                      | Loses bootstraps and hooks (finding 2)                                                                                 |
| Pi skill filters and OpenCode `deny` rules for removal                                                    | Works in two coders only; removing at build time gives one mechanism for all three                                     |
| Embedding upstream packs in the binary                                                                    | Every upstream release would need a devgeta build _and_ a re-copy; fetching at a pinned SHA keeps upstream bytes exact |

## Shared work

- **State:** `global_config.yaml` gains an `agent_packs:` section. For each
  pack it holds the version, SHA, coders, exclusions, and every item it
  installed: the marketplace name, Pi package path, and `opencode.json`
  entries. This is a config format change, so it needs a migration note
  (CLAUDE.md §10).
- **Conflicts with what the user installed:** if the user already has
  Superpowers from `claude-plugins-official`, through `pi install git:...`, or
  from an OpenCode plugin spec, both copies load. `install` should detect that
  (`claude plugin list --json`, `pi list`, the plugin list in `opencode.json`)
  and stop with the exact command to remove the other copy. It must not
  remove it on its own.
- **Runtime needs, to report rather than hide:**
  - Ponytail's Claude Code hooks need `node`.
  - Superpowers' Claude Code hook needs `bash`.
  - Superpowers' brainstorming visual companion needs `node`.
  - `install` checks for these and names anything missing.
- **Ponytail's ledger:** `install` mentions that Ponytail writes task rows to
  `~/.config/ponytail/band/` by default, and how to turn that off. Upstream's
  default stays as it is.

## Probe results (2026-11-08)

Run by the agent in
`~/.cache/devgeta/scratch/agent-packs-probe/`. `HOME`, `XDG_*`,
`CLAUDE_CONFIG_DIR` and `PI_CODING_AGENT_DIR` all pointed inside it, and Pi
1.5.0 was installed there with npm. Nothing touched the real `~/.claude`,
`~/.config` or `~/.pi`. The tree was built by hand from `v7.0.2` and `v5.3.3`:
`dispatching-parallel-agents` deleted, a wrapper for `using-git-worktrees`
(with `UPSTREAM.md`), and a full replacement of `verification-before-completion`.

**Pinned commits.** Both tags are annotated, so `refs/tags/<tag>` is a tag
object, not the commit. `git rev-parse HEAD` in a clone of the tag gives the
commit, and that is what `pack.yaml` must hold:

| Pack        | Tag      | Commit (`HEAD`)                            |
| ----------- | -------- | ------------------------------------------ |
| Superpowers | `v7.0.2` | `22f0b463e7a5e579904dd6211a520321035526f7` |
| Superpowers | `v7.0.1` | `2ce8aa15d05bd57213e6f746f953aafb7ded7a8b` |
| Ponytail    | `v5.3.3` | `7801d061243d7b753bad0e0b3c057cd250b11755` |

**Correction to finding 2.** Superpowers' Claude hook runs
`hooks/run-hook.cmd session-start` with `"shell": "bash"`, not
`hooks/session-start` directly. It has 15 skills including
`diagnosing-superpowers`.

### Claude Code 2.1.319

| Question                                                | Answer                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `marketplace add <dir>` and `install …@devgeta -s user` | **Yes, no prompts**, exit 0 (stdin was `/dev/null`).                                                                                                                                                                                                                                                                                                                                                                              |
| Files written                                           | `settings.json` gets `enabledPlugins` and `extraKnownMarketplaces` (`source: directory`, absolute path). `plugins/known_marketplaces.json`, `plugins/installed_plugins.json`, `.claude.json` and a backup under `backups/`. A copy of each plugin under `plugins/cache/devgeta/<plugin>/<version>/`.                                                                                                                              |
| Is `settings.json` alone enough?                        | **Yes for loading.** With only those two keys and no CLI install, the debug log says the marketplace "load[s] in place from the declared path" and loads 14 + 6 skills. `claude plugin list --json` stays empty until the CLI install records it.                                                                                                                                                                                 |
| Directory marketplace: in place or copied?              | **Both exist, but a session uses the source folder.** The CLI copies to `plugins/cache/`, and `plugin update` leaves that copy stale ("read from its folder … Edits there take effect at the next session start"). But the session debug log reads `hooks.json` and skills from `data/devgeta-packs/<plugin>/…`, not the cache. 14 Superpowers skills loaded, so the deleted skill is gone. A reinstall refreshes the cache copy. |
| So is the `+dg.<hash>` version needed?                  | **No.** The version string only affects the unused cache copy. Step 6 drops the version caveat.                                                                                                                                                                                                                                                                                                                                   |
| `claude plugin validate <tree>`                         | Passes on the hand-built tree.                                                                                                                                                                                                                                                                                                                                                                                                    |
| Bootstrap hook                                          | `run-hook.cmd session-start` runs under macOS bash 3.2 and prints `hookSpecificOutput.additionalContext` holding the `using-superpowers` skill. This shows what the hook emits. **It does not show a session received it.**                                                                                                                                                                                                       |
| Wrapper beats `UPSTREAM.md`; `superpowers:*` resolves   | **Not probed:** needs a model login (`claude -p` returns "Not logged in").                                                                                                                                                                                                                                                                                                                                                        |

### OpenCode 2.2.6

- **Sandbox trap:** `opencode debug config` and other commands try the shared
  service on the default port. My first run collided with the maintainer's
  real service (`opencode serve --service`, PID 53896, port 49374). It failed
  to start and the real service was untouched (same PID, same uptime
  afterwards). Probes must use `--standalone` or a private
  `opencode serve --port <n>`. The cycle's tests and docs must say so.
- Both plugins **load from absolute folder paths** in `plugins`: Superpowers
  from the tree root (`index.js`), Ponytail from `.opencode/plugins/`
  (`index.js`). The server log shows `loading plugin … entrypoint=…/index.js`
  for each, with no warnings or errors.
- **Skills come from the built tree.** `GET /api/skill` on a private server
  listed 14 Superpowers skills (the deleted one absent), plus Ponytail's 6.
  The wrapper and full-replacement markers were present, so it is not
  upstream's copy.
- **`/ponytail*` commands appear without copying.** `GET /api/command` lists
  `ponytail`, `-audit`, `-debt`, `-gain`, `-help` and `-review`. The plugin
  parses `.opencode/command/*.md` itself, so Step 10's copy fallback is not
  needed.
- `opencode debug config` only lists config sources. It shows no warnings but
  does not prove plugins loaded. Use `plugin list --standalone` or the
  skill/command API instead.
- Plugins load lazily: a fresh server showed none until something asked for
  them.
- **Not probed:** the bootstrap reaching a session (needs a login).

### Pi 1.5.0

- `pi install <abs path>` and `pi remove <abs path>` run **without prompts**,
  exit 0.
- **Pi stores the path relative to the agent dir** in `settings.json`
  `packages` (`../data/devgeta-packs/superpowers`), not the absolute path
  that was passed. The shared package reader (Step 11) must resolve entries
  against the agent dir before comparing, and `pi remove` accepts the
  absolute path.
- Over RPC (`pi --mode rpc --no-session`, `get_commands`): all six
  `/ponytail*` commands register from `ponytail/pi-extension/index.js`, and
  14 Superpowers skills load from `…/superpowers/skills/…` of the built tree,
  with the deleted one absent. The Ponytail extension started and reported
  "Ponytail loaded: full".
- Superpowers' extension computes `packageRoot` from its own file
  (`resolve(extensionDir, "../..")`), so it reads skills from the built tree.
  It registers no commands, so the bootstrap itself is not visible over RPC.
- **Not probed:** whether text appended in `before_agent_start` reaches the
  model and survives compaction (needs a login).

### Build checks, `v7.0.1` → `v7.0.2`

- Skill set unchanged (15 each). No `name` or `description` change. Three
  skill bodies changed: `brainstorming`, `executing-plans` and
  `subagent-driven-development`. A full replacement of any of them would show
  up in `check` as a changed upstream hash.
- Cross-references exist. `using-git-worktrees` is named by
  `executing-plans`, `writing-plans` and `subagent-driven-development`, so
  excluding it would trip the "no kept skill names an excluded one" check.
- The 7.0.2 glue changes (the OpenCode plugin adds `compaction` and
  `generate` hooks and a bootstrap marker; the skill-id wording) are in
  files the build never edits. The checks neither need nor catch them. A
  smoke test per coder is what covers glue changes.

### Still open

Needs a model login the sandbox doesn't have. Stopped per the cycle's rule;
the maintainer decides how to give a probe a login.

- Claude: the bootstrap reaches a session; a wrapper `SKILL.md` wins over
  `UPSTREAM.md`; `superpowers:*` resolves.
- OpenCode: a new session gets the bootstrap on the shared service.
- Pi: `before_agent_start` text reaches the model and survives compaction.

## Verification still needed

The original checklist, kept as written. Answers are under
[Probe results](#probe-results-2026-11-08); items 1, 2, 3 and 5 still have
session-level parts open.

All probes run with a throwaway `HOME`, plus `PI_CODING_AGENT_DIR` and
`XDG_CONFIG_HOME` under the scratch directory. Never on the real machine.

1. **Claude Code 2.1.319:**
   - Does `claude plugin marketplace add <dir>`, followed by
     `claude plugin install superpowers@devgeta -s user`, run without prompts?
   - Does a `directory` marketplace load in place, with no cache copy, so the
     `version` caveat goes away?
   - Does the `SessionStart` bootstrap fire?
   - Does a wrapper `SKILL.md` win over `UPSTREAM.md`?
   - Do `superpowers:*` calls resolve?
2. **OpenCode 2.2.6.** Answered already: devgeta's own config, permissions
   and plugins load on 2.x (done). Still open:
   - Does an absolute folder path in `plugins` load Superpowers (tree root)
     and Ponytail (`.opencode/plugins/`) from the built tree, with the
     built tree's skills (overrides and exclusions applied), not upstream's?
   - Do Ponytail's `/ponytail` commands appear without copying
     `.opencode/command/`?
   - With both loaded on the shared service, does each session get the
     Superpowers bootstrap, and does `opencode debug config` stay
     warning-free?
3. **Pi:**
   - Install it in the sandbox. Do `pi install <path>` and `pi remove <path>`
     run without prompts?
   - Does the Superpowers extension see the built tree?
   - Do Ponytail's commands register?
   - Pi is not installed on the maintainer's machine. Probe in a sandbox
     install, or run `dg install --only pi` first (the maintainer's call).
4. **Build checks:** run them against `v7.0.1` → `v7.0.2` to see whether they
   catch real changes. That release changed the OpenCode skill IDs.
5. **`DEVGETA.md` in Pi** (carried from reset item 2). Does text appended in
   `before_agent_start` reach the model, and does it survive compaction?
6. **Later candidates** (carried from reset item 4): `humanizer`, and
   Anthropic's `internal-comms` and `doc-coauthoring`. Check license,
   maintenance and steps that only work in Claude.

## Decisions this will need

- **ADR:** `dg agent-pack`. Packs are built from a pinned upstream plus a
  removal and wrapper layer, then installed through each coder's own plugin
  or package system. Includes the `--coder` and `--exclude` flags, the
  default install in `dg install`, and `--skip agent-packs`.
- **ADR:** customization happens only through an override layer applied at
  build time, on top of a fetched upstream copy that stays byte-for-byte
  identical. Each override is a full replacement or a wrapper with
  `UPSTREAM.md`. devgeta's overrides must pass principle 8; the user's own
  overrides win. This replaces the reset investigation's planned
  wrapper-only ADR and the "Known differences" practice.
- **ADR:** removal deletes only what devgeta installed, tracked in
  `agent_packs:`.
- **ADR-0058 amendment, and ADR-0059 §7:**
  - `DEVGETA.md` ships as the `devgeta` pack.
  - Claude Code: the `@DEVGETA.md` line is added when missing and left alone
    when present.
  - Pi: a devgeta extension.
- **Pinning (done, no ADR at the maintainer's call):** Claude Code 2.1.319
  and OpenCode 2.2.6 pinned, Pi a 1.5.0 reference, the OpenCode 1 → 2
  migration shipped on `agentsv2` (`9309762`).
- **Parity:** CLAUDE.md §12, `agent-sync.md` and the parity tests widened to
  every coder.
- **The maintainer's calls:** which upstream skills need a devgeta override
  first.
