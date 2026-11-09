# Cycle: Agent packs (`dg agent-pack`)

**Date:** 2026-11-08
**Estimated Duration:** ~24 hours, plus the maintainer's deploy
**Status:** Done (2026-11-09, shipped in v2.0.0). Full `go test ./...` green. Manual
Verification run on the maintainer's machine: 1 (Claude and OpenCode), 2 (Claude:
skills and Ponytail hook in a new session), 9 (added once, left alone on re-run),
13 (`--only agent-packs`), and 3 in a sandbox HOME against the real
`global_config.yaml` (packs survive `--force` in both coders). **Not run:** 4–8,
the uninstall half of 9, 10–12, 14, and every Pi check (Pi not installed). Upgrade
path: [agent-layer-v1-to-v2.md](../../migrations/agent-layer-v1-to-v2.md).
**Branch:** `agentsv2`

**Investigations:**

- [2026-11-08-agent-packs.md](../../investigations/2026-11-08-agent-packs.md)
  — what this cycle implements (findings 1–6, "Shared work", "Verification
  still needed").
- [2026-11-06-skills-and-agents-reset.md](../../investigations/2026-11-06-skills-and-agents-reset.md)
  — the reset before it (`1906e9e`). Its
  [Appendix A](../../investigations/2026-11-06-skills-and-agents-reset.md#appendix-a--devgetamd-v2-draft)
  is the `DEVGETA.md` v2 draft that ships here.

**ADRs:** written in Phase 0, after the probes and before any code
(CLAUDE.md [§10, "Spec-driven development"](../../../CLAUDE.md#spec-driven-development)).
All three and the amendments were **accepted by the maintainer on
2026-11-08**. Where this doc and an ADR disagree, the ADR wins; fix this doc.

- [ADR-0068](../../decisions/ADR-0068-agent-packs-are-a-pinned-upstream-plus-a-build-layer.md)
  — `dg agent-pack`: a pinned upstream plus a build layer, installed through
  each coder's own plugin or package system.
- [ADR-0069](../../decisions/ADR-0069-pack-customization-only-through-a-build-time-override-layer.md)
  — customization only through a build-time override layer.
- [ADR-0070](../../decisions/ADR-0070-pack-removal-deletes-only-what-devgeta-installed.md)
  — removal deletes only what devgeta installed, tracked in `agent_packs:`.
- Amendments:
  - [ADR-0058](../../decisions/ADR-0058-devgeta-instructions-live-in-their-own-file.md):
    `DEVGETA.md` ships as the `devgeta` pack, to all three coders, and the
    `@DEVGETA.md` line is "added if missing", not "added once".
  - [ADR-0059 §7](../../decisions/ADR-0059-pi-installs-from-its-official-script.md):
    parity with Claude Code and OpenCode is now in scope for Pi.
  - [ADR-0060](../../decisions/ADR-0060-pi-extensions-install-through-pis-own-package-manager.md):
    §7, packs install through `pi install <path>`, the same package manager,
    and unlike the curated extensions devgeta removes them on uninstall. §8,
    shared skills and parity are now in scope.

**Prior cycles on `agentsv2`:**

1. `d7adb89` removed rtk.
2. `1906e9e` was the skills reset.
3. [Agent customizations rollback](2026-11-07-agent-customizations-rollback.md).
4. `9309762` added the coder pins and the OpenCode 2 move. See
   [docs/migrations/opencode-2.md](../../migrations/opencode-2.md).
5. **This cycle.**

---

## 1. Domain Context

After the reset, devgeta installs no agent content into Claude Code, OpenCode
or Pi. This cycle brings content back as **agent packs**:

- [Superpowers](https://github.com/obra/superpowers), pinned at `v7.0.2`;
- [Ponytail](https://github.com/DietrichGebert/ponytail), pinned at `v5.3.3`;
- devgeta's own `devgeta` pack, which carries `DEVGETA.md`.

Neither upstream is just a folder of skills
([investigation finding 2](../../investigations/2026-11-08-agent-packs.md#2-what-the-two-upstreams-need-in-order-to-work)).
Superpowers needs its bootstrap injected at session start. That is a hook in
Claude Code, a plugin in OpenCode and an extension in Pi. Ponytail's ruleset
lives almost entirely in hooks and commands. Copying `skills/` folders, which
is what devgeta did before the reset, breaks both.

So each pack goes through three stages
([finding 3](../../investigations/2026-11-08-agent-packs.md#3-delivery-build-a-devgeta-owned-tree-install-it-natively)):

1. **Fetched** at a pinned tag and commit, and kept byte-for-byte as upstream
   published it.
2. **Built** into a tree devgeta owns: excluded skills removed, then the
   override layer copied on top.
3. **Installed** from that tree through each coder's own system: a Claude
   marketplace and plugin, an OpenCode `plugins` entry, and
   `pi install <path>`.

Claude Code can't drop a single skill from a plugin, and no coder can
customize a skill without editing it. Doing both at build time gives one
mechanism that behaves the same in all three coders.

### Decisions already made

| Decision                                                                                | Who, when                                     |
| --------------------------------------------------------------------------------------- | --------------------------------------------- |
| Command is `dg agent-pack`, with `--coder`                                              | Maintainer, 2026-11-08 (finding 1)            |
| `dg install` installs `devgeta`, `superpowers` and `ponytail` by default                | Maintainer, 2026-11-08                        |
| …only into coders **installed in that run**, not ones already present before it         | Maintainer, 2026-11-08 (this cycle's review)  |
| Claude Code gets `DEVGETA.md` through `@DEVGETA.md`, added if missing, left if present  | Maintainer, 2026-11-08 (finding 5)            |
| Coders pinned (Claude Code 2.1.319, OpenCode 2.2.6); Pi 1.5.0 is a reference, not a pin | Done, `9309762`                               |
| The agent runs the Phase 0 probes itself, in a sandbox `HOME`                           | Maintainer, 2026-11-08 (this cycle's review)  |
| A saved `--exclude` list is replaced by a new `--exclude`, and cleared by `--exclude=`  | Default chosen in this cycle; change if wrong |
| `dg install --only agent-packs` installs into every configured coder, not just new ones | Default chosen in ADR review; change if wrong |
| `update` always rebuilds, instead of tracking whether the user's overrides changed      | Default chosen in ADR review; change if wrong |

---

## 2. Engineer Context

### Read first

- [CLAUDE.md §4, "The maintainer's machine is production"](../../../CLAUDE.md#the-maintainers-machine-is-production):
  no pack install, `dg configure` or coder CLI write on the real machine
  without a yes for that command.
- [CLAUDE.md §12, "Anything we ship is built for strangers"](../../../CLAUDE.md#anything-we-ship-is-built-for-strangers):
  `DEVGETA.md` and every override load into other people's repos.
- [docs/guides/agent-sync.md](../../guides/agent-sync.md): where each coder
  concern lives, and the
  [accepted differences](../../guides/agent-sync.md#accepted-differences).
  Step 15 widens it to Pi.
- [docs/guides/cli-patterns.md](../../guides/cli-patterns.md),
  [error-handling.md](../../guides/error-handling.md),
  [testing-patterns.md](../../guides/testing-patterns.md),
  [app-interface.md](../../guides/app-interface.md).
- How each coder is configured today:
  - [docs/apps/claude.md, "What gets deployed"](../../apps/claude.md#what-gets-deployed);
  - [docs/apps/opencode.md, "Plugins"](../../apps/opencode.md#plugins) and
    ["`--force` and what it leaves alone"](../../apps/opencode.md#--force-and-what-it-leaves-alone);
  - [docs/apps/pi.md, "Configuration"](../../apps/pi.md#configuration--a-curated-extension-set-and-one-theme).
- Upstream docs the design relies on, read 2026-11-08 and listed in the
  investigation's [Method](../../investigations/2026-11-08-agent-packs.md#method):
  - Claude Code [plugins](https://code.claude.com/docs/en/plugins) and
    [skills](https://code.claude.com/docs/en/skills);
  - OpenCode [plugins](https://opencode.ai/docs/plugins/),
    [skills](https://opencode.ai/docs/skills/) and
    [rules](https://docs.opencode.ai/docs/rules/);
  - Pi [packages](https://pi.dev/docs/latest/packages) and
    [skills](https://pi.dev/docs/latest/skills).
- The old hand-patched pin procedure, for history only:
  `git show d7adb89:docs/guides/vendored-skills.md`.

### Existing code this cycle touches

| File                                                 | Why                                                                                                                                                                                                                                                        |
| ---------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/install.go:28-29,206-250`                       | `knownCategories` and `--only`/`--skip` parsing. `agent-packs` joins as a category token.                                                                                                                                                                  |
| `cmd/root.go`                                        | Register the new command.                                                                                                                                                                                                                                  |
| `internal/tooling/terminal/terminal.go:156-181`      | `InstallTerminalApps` calls `SoftInstall` and doesn't say whether the app was missing before. `dg install` needs that to know which coders it installed (Step 14).                                                                                         |
| `pkg/constants/constants.go:52-65`                   | `PinnedVersion` for the coders. Pack pins don't go here (see Trade-offs).                                                                                                                                                                                  |
| `pkg/paths/paths.go:250,303,454`                     | `Paths.App.Data` and `GetDataDir`. Built trees and clones go under the devgeta data root.                                                                                                                                                                  |
| `internal/config/fromFile.go:219-251`                | `GlobalConfig` gains `AgentPacks`. ADR-0058's `integrations.claude_instructions_imported` no longer exists (the reset removed it), so nothing needs migrating from it.                                                                                     |
| `internal/apps/git/git.go:207`                       | `Git.Clone` takes only url and path. Extend the wrapper (clone one tag, `rev-parse HEAD` in a dir) rather than calling `git` directly (CLAUDE.md §6).                                                                                                      |
| `internal/apps/claude/claude.go:141-237`             | Gained `ExecuteCommandOutput` (Step 3; `ExecuteCommand` already existed, now with stdin disconnected). `ForceConfigureTheme` re-renders **all** of `settings.json` from the template, so it would wipe what `claude plugin install` writes there (Step 9). |
| `internal/apps/claude/claude.go:107-125`             | `Uninstall` deletes `~/.claude`, so the Claude side of every pack goes with it. State must follow (Step 8).                                                                                                                                                |
| `configs/claude/settings.json.tmpl`                  | Gains `extraKnownMarketplaces` and `enabledPlugins`, rendered from `agent_packs:` state.                                                                                                                                                                   |
| `internal/apps/opencode/opencode.go:170-264`         | `ForceConfigureTheme` re-renders `opencode.json` from the template, which drops anything added by hand (Step 10).                                                                                                                                          |
| `configs/opencode/opencode.json.tmpl`                | Gains `plugins` and `instructions` entries, rendered from `agent_packs:` state.                                                                                                                                                                            |
| `internal/apps/pi/pi.go:122-190,367-383,401`         | `declaredExtensions` reads `settings.json` `packages` but only matches the curated list. Split out a reader that returns every source, used by both. `Uninstall` keeps Pi's packages (ADR-0060).                                                           |
| `internal/apps/opencode/permissions_test.go:333-434` | Parity tests. Pi joins in Step 15.                                                                                                                                                                                                                         |

### New code

- `internal/tooling/agentpack/` is the engine, and nothing in it is specific
  to a coder: manifest, fetch, build, checks, state, and orchestration across
  coders. A future `nvim-pack` would reuse it with a different installer
  ([finding 1](../../investigations/2026-11-08-agent-packs.md#1-command)).
- `internal/tooling/agentpack/{claude,opencode,pi}.go` hold one installer per
  coder, behind one small interface.
- `cmd/agent_pack.go` holds `list`, `install`, `uninstall`, `update` and
  `check`.
- `configs/agent-packs/<pack>/pack.yaml` holds the repo, tag and full SHA.
- `configs/agent-packs/<pack>/skills/…` holds devgeta's shipped overrides.
  It is empty for Superpowers and Ponytail in this cycle.
- `configs/agent-packs/devgeta/` holds `pack.yaml` (`source: embedded`),
  `DEVGETA.md`, and the Pi package (`package.json` plus
  `extensions/devgeta-instructions.js`).

### Must keep working

- `dg configure claude|opencode|pi`, with and without `--force`.
- devgeta's own OpenCode plugins (`devgeta-agent-config-guard`,
  `devgeta-notify`) and Pi extensions (`devgeta-format.js`, the curated set).
- The statusline and `agent-config-guard`.
- The permission parity tests.

### Testing

**This cycle is test-driven (maintainer's call, 2026-11-08).** It replaces
the "implement → verify → test" order in
[CLAUDE.md §6, "Feature workflow"](../../../CLAUDE.md#feature-workflow-implement--verify--test--commit)
for this cycle only. Every other rule in §6 still applies. For each behaviour
in a step:

1. **Red.** Write the test first. Run it and watch it fail, and check it
   fails for the reason you expect (a missing behaviour), not because it
   won't compile or because of a typo. A test you never saw fail proves
   nothing.
2. **Green.** Write the least code that makes it pass. Nothing for later.
3. **Refactor.** Remove duplication against the existing codebase
   (CLAUDE.md §6, "Reuse before writing") while the tests stay green.
4. Repeat for the next behaviour. When the step's behaviours are done, run
   the step's **Verify** commands and `make lint`, then commit.

- The tests come from each step's **Verify** list and from §6 "Verification
  Plan". Write the failure paths (SHA mismatch, failed build, a coder failing
  mid-install) as tests before the code that handles them.
- Never weaken or delete a test to get to green. If a test turns out to be
  wrong, say so in the commit message and fix it on purpose.
- Manual verification (§6) still runs after the code, in the sandbox,
  because it needs real coders.
- Every coder CLI call and every `git` call goes through a mocked wrapper.
- Upstream trees in tests are fixtures under
  `internal/tooling/agentpack/testdata/`. Tests never fetch them.
- Call `testutil.VerifyNoRealCommands` on the same base the code uses.
- Isolate every root a test writes: config, data, `~/.claude`, the OpenCode
  config dir and the Pi agent dir.

Targeted test commands
([CLAUDE.md §6, "Which tests to run"](../../../CLAUDE.md#which-tests-to-run)):

```bash
go test ./internal/tooling/agentpack/...
go test ./internal/apps/claude/ ./internal/apps/opencode/ ./internal/apps/pi/ ./internal/apps/git/
go test ./internal/config/ ./internal/tooling/terminal/ ./cmd/
go test .     # only when the step changed something under configs/
make lint
```

---

## 3. Objective

When this cycle is done, two things are true:

- `dg agent-pack list|install|uninstall|update|check` exists.
- `dg install` installs the `devgeta`, `superpowers` and `ponytail` packs into
  each coder it installed in that run.

In every coder that gets a pack, the pack's hooks, bootstrap, skills and
commands work from a pinned tree that devgeta built. Anything a coder can't
load is named in the output. Nothing a pack added is lost on
`dg configure --force`.

---

## 4. Scope Boundary

### In Scope

- [x] Phase 0: sandbox probes for investigation Verification 1–5, with the
      results recorded in the investigation. The CLI-level questions are all
      answered. The session-level ones need a model login and moved to
      Manual Verification 2, 4 and 5 (see Step 1).
- [x] Phase 0: the three ADRs and the ADR-0058, ADR-0059 and ADR-0060
      amendments, accepted 2026-11-08.
- [x] The pack manifest, fetching at the pinned tag and SHA, and a read-only
      clone cache.
- [x] The build step: exclusions, the override layer (devgeta's, then the
      user's), the wrapper form with `UPSTREAM.md`, and every build check
      from finding 3.
- [x] Installers for Claude Code, OpenCode and Pi, with rollback across
      coders when one fails. (Engine and rollback done in Step 8; the three
      installers are Steps 9–11.)
- [x] `agent_packs:` state, read back by `dg configure` so that `--force`
      keeps pack entries. (State type done in Step 7; the render is Steps 9–10.)
- [x] State follows a coder's uninstall.
- [x] `dg agent-pack list|install|uninstall|update|check`, with `--coder`,
      `--exclude` and `--ref`.
- [x] Conflict detection for packs the user installed another way: stop,
      print the removal command, never remove it.
- [x] Runtime checks for `node` and `bash`, and the Ponytail ledger notice.
- [x] The `devgeta` pack: `DEVGETA.md` v2 to all three coders.
- [x] `dg install` default (coders installed in that run only), plus
      `--skip agent-packs` and `--only agent-packs`.
- [x] Parity widened to every coder: CLAUDE.md §10 and §12, `agent-sync.md`,
      and the parity tests.
- [x] Docs: `docs/spec.md`, `README.md`, a new `docs/apps/agent-packs.md`,
      the three app docs, a migration note, ROADMAP and recent changes.

### Explicitly Out of Scope

- Any override of an upstream skill that devgeta ships. The mechanism ships
  and is tested with fixtures. Which skill gets one first (for example
  `using-git-worktrees` → `dg worktree`) is the maintainer's call, made as its
  own change
  ([investigation, "Decisions this will need"](../../investigations/2026-11-08-agent-packs.md#decisions-this-will-need)).
- devgeta's own skills inside the `devgeta` pack (finding 5, "Later").
- Packs for apps that aren't coders, such as `nvim-pack`.
- Hooks and permissions devgeta itself ships
  ([ADR-0067](../../decisions/ADR-0067-agent-permission-floor.md)).
- Other skill sets: `humanizer`, `internal-comms`, `doc-coauthoring`
  (Verification 6).
- Pinning Pi itself or Pi's curated extensions.
- Changing Ponytail's ledger default or its statusline behaviour.
- Retrying packs for coders that were already installed when `dg install`
  runs. The user runs `dg agent-pack install` for those, and the output says
  so.

**Scope is locked.** If a Phase 0 probe shows a coder can't do what the
design needs, stop. Amend the investigation and the ADRs before working
around it.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                                        | Description                                                                             |
| ------ | ---------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Modify | `docs/investigations/2026-11-08-agent-packs.md`                  | Probe results; status to Concluded at the end                                           |
| Create | `docs/decisions/ADR-0068…0070-*.md`                              | The three ADRs                                                                          |
| Modify | `docs/decisions/ADR-0058-*.md`, `ADR-0059-*.md`, `ADR-0060-*.md` | Amendments (see header)                                                                 |
| Modify | `docs/decisions/README.md`                                       | Index the new ADRs                                                                      |
| Modify | `internal/apps/git/git.go`                                       | Clone one tag shallowly; `rev-parse HEAD` in a dir                                      |
| Modify | `internal/apps/claude/claude.go`                                 | `ExecuteCommandOutput`, stdin disconnected; render pack entries; uninstall clears state |
| Modify | `configs/claude/settings.json.tmpl`                              | `extraKnownMarketplaces`, `enabledPlugins` from state                                   |
| Modify | `internal/apps/opencode/opencode.go`                             | Render pack `plugins` and `instructions`; uninstall clears state                        |
| Modify | `configs/opencode/opencode.json.tmpl`                            | `plugins`, `instructions` from state                                                    |
| Modify | `internal/apps/pi/pi.go`                                         | Split out a reader for every package source; `declaredExtensions` uses it               |
| Modify | `pkg/paths/paths.go`                                             | `Paths.App.AgentPacks`, `Paths.App.Configs.AgentPacks`, `Paths.Config.AgentPacks`       |
| Modify | `internal/config/fromFile.go`                                    | `AgentPacks map[string]AgentPackState`, `yaml:"agent_packs,omitempty"`                  |
| Create | `internal/tooling/agentpack/manifest.go`                         | Load and validate the embedded `pack.yaml`                                              |
| Create | `internal/tooling/agentpack/fetch.go`                            | Clone at tag, check the SHA, make it read-only                                          |
| Create | `internal/tooling/agentpack/build.go`                            | Copy, exclude, overlay, handle wrappers, swap atomically                                |
| Create | `internal/tooling/agentpack/checks.go`                           | Finding 3 build checks; track upstream hashes for full replacements                     |
| Create | `internal/tooling/agentpack/installer.go`                        | `Installer` interface; orchestration and cross-coder rollback                           |
| Create | `internal/tooling/agentpack/{claude,opencode,pi}.go`             | The three installers                                                                    |
| Create | `internal/tooling/agentpack/devgeta.go`                          | `@DEVGETA.md`: add if missing, remove only if exact                                     |
| Create | `internal/tooling/agentpack/*_test.go`, `testdata/`              | Fixture upstream trees; all mocked                                                      |
| Create | `configs/agent-packs/{superpowers,ponytail}/pack.yaml`           | Pins: `v7.0.2` and `v5.3.3`, with full SHAs                                             |
| Create | `configs/agent-packs/devgeta/…`                                  | `pack.yaml`, `DEVGETA.md`, Pi package                                                   |
| Create | `cmd/agent_pack.go`, `cmd/agent_pack_test.go`                    | Subcommands and flags                                                                   |
| Modify | `cmd/root.go`                                                    | Register `agent-pack`                                                                   |
| Modify | `cmd/install.go`, `internal/tooling/terminal/terminal.go`        | `agent-packs` token; report the coders installed in this run                            |
| Modify | `internal/apps/opencode/permissions_test.go`                     | Parity covers Pi                                                                        |
| Modify | `CLAUDE.md` §10 and §12, `docs/guides/agent-sync.md`             | "the two agents" becomes "every coder"                                                  |
| Create | `docs/apps/agent-packs.md`                                       | User guide                                                                              |
| Modify | `docs/apps/{claude,opencode,pi}.md`, `docs/spec.md`, `README.md` | Feature and command docs                                                                |
| Create | `docs/migrations/agent-packs.md`                                 | Per [migrations/README.md](../../migrations/README.md#writing-a-new-one)                |
| Modify | `ROADMAP.md`, `docs/recent-changes.md`                           | Entries                                                                                 |

### Step-by-Step

#### Phase 0: probe, then decide. No code before this phase is done.

##### Step 1: Sandbox probes (the agent runs these)

**Sandbox.** Everything happens under
`~/.cache/devgeta/scratch/agent-packs-probe/`:

- `HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `PI_CODING_AGENT_DIR` all
  point inside it.
- Pi is installed into the sandbox with npm (`--prefix` inside it).
- No command touches the real `~/.claude`, `~/.config` or `~/.pi`.

**Tree.** Clone `v7.0.2` and `v5.3.3`, then build a tree by hand: delete one
skill, add one wrapper skill, add one full replacement.

**Claude Code 2.1.319:**

- Do `claude plugin marketplace add <dir>` and
  `claude plugin install superpowers@devgeta -s user` run without prompts?
- Which files do they write? (`settings.json` keys, anything under
  `~/.claude/plugins/`.)
- Is declaring `extraKnownMarketplaces` and `enabledPlugins` in
  `settings.json` enough on its own, or is the CLI install also needed?
  This decides Step 9.
- Is a directory marketplace loaded in place, or copied to the cache? This
  decides the `version` caveat in finding 4.
- In a session: does the bootstrap fire, does the wrapper win over
  `UPSTREAM.md`, and does `superpowers:*` resolve?

**OpenCode 2.2.6:**

- Do both plugins load from absolute folder paths: Superpowers from the tree
  root, Ponytail from `.opencode/plugins/`?
- Does OpenCode see the built tree's skills, with the exclusion and override
  applied?
- Do the `/ponytail` commands appear without copying `.opencode/command/`?
- Does `opencode debug config` stay warning-free?

**Pi 1.5.0:**

- Do `pi install <path>` and `pi remove <path>` run without prompts?
- Does the Superpowers extension see the built tree?
- Do Ponytail's commands register?
- Does text appended in `before_agent_start` reach the model, and does it
  survive compaction?

**Build checks:** run them by hand against `v7.0.1` → `v7.0.2`
(Verification 4).

**Login limits.** Checks that only use a coder's CLI (plugin, install and
config commands) need no login. Checks inside a session (bootstrap fired,
skill resolved, text reached the model) need a model login, and the sandbox
`HOME` has none. If a coder can't log in from the sandbox, stop and ask the
maintainer how to give it a login. Never point a probe at real credentials
without a yes.

- Expected outcome: each question answered in the investigation, with the
  command and its output.
- Verify: a "no" on any question stops the cycle until the investigation is
  amended.
- **Result (done 2026-11-08):** no question came back "no". The answers are
  in the investigation's
  [Probe results](../../investigations/2026-11-08-agent-packs.md#probe-results-2026-11-08),
  and Steps 4, 6, 10 and 11 below already include them. The session-level
  questions (bootstrap reaches a session, wrapper wins over `UPSTREAM.md`,
  `superpowers:*` resolves, Pi's text survives compaction) are **unanswered**:
  the sandbox has no model login. They are checked in Manual Verification 2,
  4 and 5, with a login the maintainer provides. A "no" there reopens the
  ADRs, as a "no" here would have.

##### Step 2: ADRs and amendments

- Write ADR-0068, 0069 and 0070, plus the ADR-0058, ADR-0059 and ADR-0060
  amendments.
- Start from the investigation's "Decisions this will need" and adjust for
  what Step 1 found.
- Expected outcome: ADRs in `Proposed`, indexed in
  `docs/decisions/README.md`.
- Verify: the maintainer accepts them.
- **Result (done 2026-11-08):** ADR-0068, 0069 and 0070 are accepted and
  indexed. Amendments are in ADR-0058 (two notes), ADR-0059 §7, and ADR-0060
  §7 and §8. The review added two defaults to the decision table: what
  `--only agent-packs` does, and `update` always rebuilding.

#### Phase 1: the engine

##### Step 3: Wrapper extensions

- `Git`: clone one tag shallowly into a dir; `rev-parse HEAD` in a dir.
- `Claude`: `ExecuteCommand` already existed. It now disconnects stdin, and
  `ExecuteCommandOutput` returns stdout for `--json` queries.
- **Done.** `Git.CloneTag` refuses a tag starting with `-`; `Git.HeadIn`
  returns the full commit SHA.
- Verify: `go test ./internal/apps/git/ ./internal/apps/claude/`.

##### Step 4: Manifest and paths

- `pack.yaml` holds `name`, `repo`, `tag` and `sha` (40 hex characters,
  validated). `sha` is the **commit** (`git rev-parse HEAD` in a clone of the
  tag), because the tags are annotated. Pins: Superpowers `v7.0.2` →
  `22f0b463e7a5e579904dd6211a520321035526f7`; Ponytail `v5.3.3` →
  `7801d061243d7b753bad0e0b3c057cd250b11755`. The `devgeta` pack holds `source: embedded` instead.
- Three paths: `paths.Paths.App.AgentPacks` is `<data>/devgeta/agent-packs/`
  (built trees and clones; `Paths.App` has no `Data` struct, so the first
  draft's name was wrong), `Paths.App.Configs.AgentPacks` is devgeta's shipped
  overrides after `ExtractEmbeddedConfigs`, and `Paths.Config.AgentPacks` is
  the user's layer, `~/.config/devgeta/agent-packs/`.
  Clones go in `<agent-packs>/.src/<pack>@<sha>/`.
- **Done.** `LoadManifests` reads the extracted `configs/agent-packs/`; the
  `devgeta` pack's `pack.yaml` lands with Step 12.
- Verify: manifest tests reject short SHAs, missing tags and unknown keys.

##### Step 5: Fetch

- Clone at the tag into a temp dir, then check that `HEAD == sha`.
- Rename the clone into the cache, then strip its write bits. In that order:
  a directory with no write bit can't be renamed into another parent (found
  by the first test run).
- If a cached clone at the same SHA exists, reuse it.
- Verify: mocked tests for a SHA mismatch (fails, nothing cached), reuse, and
  a network failure.

##### Step 6: Build and checks

- **Copy and overlay.**
  - Copy the cached clone to `<agent-packs>/.build-<pack>-<rand>/`.
  - Delete the excluded skills.
  - Copy `configs/agent-packs/<pack>/` on top, then
    `~/.config/devgeta/agent-packs/<pack>/`. The user's layer wins.
- **Wrappers.** An override `SKILL.md` that references `UPSTREAM.md` keeps
  upstream's `SKILL.md` beside it, renamed to `UPSTREAM.md`
  ([finding 3, step 2](../../investigations/2026-11-08-agent-packs.md#3-delivery-build-a-devgeta-owned-tree-install-it-natively)).
- **Checks.** Any failure stops the build and deletes the temp tree:
  - every excluded or overridden skill exists upstream;
  - no kept skill names an excluded one, with or without the prefix;
  - an override's `name` matches upstream's, and for a wrapper its
    `description` matches too;
  - the files the installers need exist: `.claude-plugin/plugin.json`,
    `package.json` with `pi` and `main`, and the hook files `plugin.json`
    declares (`hooks/hooks.json` when it declares none). Ponytail has no
    `hooks/hooks.json`; see ADR-0069 §6.
- **Hashes.** Record upstream's hash of every fully replaced file.
- **Swap.** Rename the old tree aside, rename the new one into
  `<agent-packs>/<pack>/`, then delete the old one. If the second rename
  fails, rename the old tree back
  ([CLAUDE.md §7](../../../CLAUDE.md#7-critical-surfaces); ADR-0069 §8).
- **Version caveat: not needed.** Step 1 showed a Claude session loads skills
  and hooks from the declared source folder, not from its `plugins/cache/`
  copy. The built tree's `plugin.json` stays as upstream wrote it. `update`
  reinstalls so the unused cache copy doesn't go stale.
- **Done.** Run against the real `v7.0.2`, `v7.0.1` and Ponytail `v5.3.3`
  trees, every check passes with no exclusions. Excluding
  `dispatching-parallel-agents` is refused, because
  `using-superpowers/references/codex-tools.md` names it. That is the check
  working as written: it scans every `.md` in a kept skill's folder, not only
  `SKILL.md`.
- Verify: fixture tests for:
  - each check;
  - both override layers;
  - a wrapper versus a full replacement;
  - a failed build leaving the previous tree untouched.

##### Step 7: State

- `GlobalConfig.AgentPacks[name]` holds, per pack:
  - `version` and `sha`;
  - `exclude` and `replaced_hashes`;
  - per coder, what was installed: the marketplace and plugin id, the Pi
    package path, the `opencode.json` entries, and whether the `CLAUDE.md`
    line was added.
- It is saved through the existing atomic save path, and `clone.go` deep-copies
  it (the cache hands out copies; the clone test caught the missing case).
- **Done.** Types live in `internal/config/agent_packs.go`.
- Verify: `go test ./internal/config/`. Old configs without the key load and
  round-trip.

#### Phase 2: installers

##### Step 8: Installer interface, orchestration, uninstall hooks

- **Interface:**
  - `Name()`
  - `Available() bool`
  - `Conflicts(pack) []string`
  - `Install(tree) (Record, error)`
  - `Uninstall(Record) error`
  - `Skipped(tree) []string`, which names the parts this coder can't load,
    and why.
- **Orchestration:**
  - Build once, then install into each selected coder.
  - If any coder fails, uninstall the coders already done in this run and
    leave state unchanged. A coder that isn't installed is skipped with a
    reason, which is not a failure.
  - When no coder has a pack any more, delete its built tree and state entry,
    and keep the clone cache (ADR-0070 §4).
  - State is written once, at the end.
- **`--coder` default:** every coder devgeta has configured, per
  `global_config.yaml`.
- **Coder uninstall:**
  - `dg uninstall claude` and `dg uninstall opencode` delete their config
    dirs, so they also drop that coder from every pack's state record.
  - `dg uninstall pi` keeps Pi's packages
    ([ADR-0060](../../decisions/ADR-0060-pi-extensions-install-through-pis-own-package-manager.md)),
    so the record stays, and `list` shows "pi not installed".
- **Done.** Notes on how it was built, for the later installer steps:
  - `Engine` (`installer.go`) takes a `TreeSource` and a `StateStore`;
    `PackBuilder` and `ConfigStore` are the real ones (`wiring.go`).
  - **Render before save.** An installer whose coder file is rendered from
    state (OpenCode) implements `Render(packs)`. The engine passes it the
    state as it will be, so the single state write stays last and a failure
    still leaves state untouched. This is how Step 10's "writes state, then
    re-renders" fits ADR-0070 §7. An installer with shared cleanup (Claude's
    marketplace) implements `Finish()`, called once no pack is left for it.
  - **Reinstall rollback.** Only coders the run _added_ are undone when a
    later coder fails. A coder that already had the pack keeps it: every
    coder points at the same tree path, so reinstalling refreshes it in
    place, and undoing it would remove an install that was fine before the
    run. This reads ADR-0068 §11's "coders already done in this run" narrowly;
    flagged for the maintainer.
  - **Conflicts first.** They are checked before anything is fetched.
  - `dg uninstall claude|opencode` calls `baseapp.ForgetAgentPackCoder`, which
    drops the coder from every record and deletes the tree of any pack left
    with no coder.
- Verify: mocked tests for:
  - Pi failing after Claude and OpenCode succeeded: both are undone and
    state is unchanged;
  - a coder's uninstall clearing its records.

##### Step 9: Claude Code installer

- Write `<agent-packs>/.claude-plugin/marketplace.json` (marketplace
  `devgeta`), listing each built pack by relative path.
- Install with the CLI calls Step 1 showed are needed. Uninstall removes the
  plugin, and removes the marketplace once no pack is left.
- `settings.json.tmpl` renders `extraKnownMarketplaces` and `enabledPlugins`
  from `agent_packs:` state, so `dg configure claude --force` keeps them.
  Without this, the next `--force` silently disables every pack.
- Conflicts: `claude plugin list --json` shows the same plugin installed
  from another marketplace.
- **Done.** CLI calls (all with stdin disconnected, all through
  `claude.Claude`): `plugin marketplace list --json` then `add <packs root>`
  (or `remove` + `add` when a `devgeta` marketplace points elsewhere);
  `plugin list --json`; `plugin install <pack>@devgeta -s user`, preceded by
  an `uninstall` when it is already installed, because `plugin update` leaves
  the cache copy stale. A failed install removes the plugin and, only if this
  call added it, the marketplace. `Finish()` runs `marketplace remove devgeta`
  and deletes `.claude-plugin/`. The marketplace file lists every built pack
  that has a `.claude-plugin/plugin.json`, skipping hidden folders (the clone
  cache). `Install` refuses an embedded pack until Step 12.
  The CLI output shapes were read from the Phase 0 sandbox, not guessed.
- Verify:
  - `go test ./internal/apps/claude/ ./internal/apps/opencode/ ./internal/tooling/agentpack/ -run Claude`
    (the OpenCode package renders Claude's template in its parity tests);
  - the golden settings test covers state with no packs and with two packs.

##### Step 10: OpenCode installer

- `opencode.json.tmpl` renders `plugins` from state:
  - Superpowers: the tree root;
  - Ponytail: `<tree>/.opencode/plugins/`.
  - Both as absolute folder paths, because OpenCode 2 rejects file paths and
    doesn't expand `~`
    ([docs/apps/opencode.md, "Plugins"](../../apps/opencode.md#plugins)).
- The installer writes state, then re-renders `opencode.json` through the
  app's own configure path. It never edits the JSON by hand.
- No command copying: Step 1 showed `/ponytail*` appear from the plugin
  folder alone.
- Tests and docs that run OpenCode commands must use `--standalone` or a
  private port, or they collide with the user's shared service.
- Conflicts: a user's own `plugins` spec for the same upstream. The app
  docs say `opencode.json` is devgeta-rendered, so this can only come from a
  project config or another tool; check
  `opencode debug config` output.
- **Done.** Notes:
  - **Which folder.** The plugin folder is the first of `<tree>/.opencode/plugins/`
    and the tree root that holds an `index.js`, which is the directory form
    OpenCode 2 requires (Superpowers' own `index.js` says so). That finds
    Superpowers' root and Ponytail's `.opencode/plugins/` without naming a pack.
    A pack with neither gets an empty record and a `Skipped` line.
  - **Narrow render.** `OpenCode.ConfigureWithPacks(packs)` renders
    `opencode.json` only, through the same `renderConfig` as `--force`. It does
    not run the whole `ForceConfigure` (theme, `cli.json`, plugin folders). It
    does re-render the whole `opencode.json`, so a hand edit to that file is
    lost on the next pack change, as it already is on `--force`.
  - **Conflicts** read `opencode debug config --standalone` (`--standalone`
    keeps it off the shared service) and report a plugin spec for the same
    upstream in any config other than devgeta's own: a repo-slug match, the bare
    name, or a path whose last folder is the pack name. The "removal command" is
    an edit instruction, since OpenCode has no CLI for it.
  - `opencode.json.tmpl` renders `plugins` and `instructions` from state;
    with no packs the output is byte-identical (existing golden unchanged).
  - The OpenCode permission-parity tests rendered Claude's `settings.json.tmpl`
    with a hand-mirrored struct, which Step 9's template change broke. Step 9's
    Verify list did not include this package. They now render from a map, so an
    optional template section can't break them again.
- Verify: tests for add, idempotent re-add, uninstall removing only what was
  recorded, and `--force` keeping the entries.

##### Step 11: Pi installer

- Run `pi install <tree>` and `pi remove <tree>`, then read back
  `settings.json` `packages` through the shared reader from the Engineer
  Context table. Pi stores the path **relative to its agent dir**, so the
  reader resolves entries against that dir before comparing.
- Conflicts: a `git:` or `npm:` package for the same upstream.
- If Pi isn't installed, skip it with a named reason, not an error.
- **Done.** Notes:
  - `Pi.PackageSources()` wraps the new `readPackageSources`, which
    `declaredExtensions` now uses too (one reader, not two). It resolves
    `./`, `../` and `~` entries against the agent dir and cleans absolute ones.
  - `Pi.ExecuteCommand` now disconnects stdin, like Claude's. `Pi.IsInstalled`
    is PATH or the managed launcher.
  - `pi install` is skipped when Pi already lists the tree: a local package is
    referenced in place, so there is nothing to refresh. A failure removes a
    package Pi recorded before failing; success is confirmed by reading the
    settings back, and a `pi install` that recorded nothing is an error.
  - The conflict matcher is now shared (`sourceNamesPack`): repo slug at a
    boundary (so `superpowers-fork` is not `superpowers`), or the last path
    segment once scheme, version and `.opencode/plugins` are stripped. OpenCode
    uses it too. It can't tell two owners' packages of one name apart.
  - Pi's installer does not special-case embedded packs: the `devgeta` pack's
    Pi side is an ordinary Pi package (Step 12).
- Verify: `go test ./internal/apps/pi/ ./internal/tooling/agentpack/ -run Pi`.

#### Phase 3: packs and command

##### Step 12: The `devgeta` pack

- `DEVGETA.md` v2 comes from
  [reset Appendix A](../../investigations/2026-11-06-skills-and-agents-reset.md#appendix-a--devgetamd-v2-draft).
  Re-read it against principle 8 before it ships: nothing specific to the
  devgeta repo.
- **Claude Code:**
  - Copy the file to `~/.claude/DEVGETA.md`.
  - Add `@DEVGETA.md` to `~/.claude/CLAUDE.md` when no line imports it, and
    leave the file alone when one does.
  - Uninstall removes the line only when it is exactly devgeta's, and says so.
- **OpenCode:** an `instructions` entry pointing at the built copy, rendered
  from state like `plugins`.
- **Pi:** a package whose `devgeta-instructions.js` appends the file in
  `before_agent_start`. It re-injects after compaction if Step 1 showed the
  text is lost.
- **Same bytes:** a test checks that all three coders get the same
  `DEVGETA.md` bytes. This carries over ADR-0058's single-source guarantee.
- **Done.** Notes:
  - **Pi extension.** Pi's own types (1.5.0) say a `before_agent_start` handler
    gets `event.systemPrompt` and may return `{ systemPrompt }`. The system
    prompt is rebuilt each run, so the text is not part of the conversation
    that compaction summarizes, and no `session_compact` handler is needed
    (Superpowers' extension needs one because it injects through `context`
    messages). Whether the text reaches the model is still Manual
    Verification 5; the unit tests only prove what the handler returns.
  - **How the pack is delivered.** It is built like any other (the embedded
    folder is the source, the user's layer goes on top, `pack.yaml` is dropped)
    and all three coders read the one built `DEVGETA.md`: Claude gets a byte
    copy at `~/.claude/DEVGETA.md`, OpenCode an `instructions` entry, Pi a
    package whose extension reads it from its package root.
  - **`@DEVGETA.md` matching.** A line "imports" the file when it contains
    `@DEVGETA.md`, `@./DEVGETA.md` or `@~/.claude/DEVGETA.md` standing alone
    (Claude Code reads `@path` inline, so "See @DEVGETA.md" counts). Lookalikes
    (`@DEVGETA.md.bak`, `email@DEVGETA.md`) don't. Matching too much only means
    no duplicate line is added.
  - **Reinstall.** A second install finds the line already there and would
    record "not added", so uninstall would never remove it. The engine now
    calls an installer's optional `MergeRecord(prior, next)` so Claude carries
    "devgeta added it" forward. `AgentPackCoderState` gained
    `instructions_file`.
  - **Uninstall** deletes only `~/.claude/DEVGETA.md` (a path from state that
    points anywhere else is refused) and removes the line only when devgeta
    added it and it is still exactly `@DEVGETA.md`. Each case produces a note
    in the coder's outcome (`Noter.TakeNotes`), which the command prints.
  - Three placeholder tests that asserted "refuse an embedded pack for now"
    (Claude, OpenCode, PackBuilder) were replaced on purpose by the real
    behaviour.
  - `DEVGETA.md` is Appendix A verbatim. A root test checks it names nothing
    specific to this repo (principle 8).
- Verify: tests for add-if-missing, present-and-left, and remove-only-exact;
  `go test .` for the embedded files.

##### Step 13: The `dg agent-pack` command

- **`list`** shows each pack's pinned and installed version, and per coder
  whether it was installed or skipped, with the reason.
- **`install <pack> [--coder …] [--exclude …]`**: `--exclude` is saved to
  state.
  - A later `--exclude` replaces the saved list.
  - `--exclude=` clears it.
  - Leaving the flag out keeps the saved list.
- **`uninstall <pack> [--coder …]`**.
- **`update [pack]`** moves to the pin in this binary and always rebuilds, so
  changed user overrides are picked up.
- **`check <pack> [--ref <tag>]`** installs nothing. It:
  - builds into a temp dir and runs the checks;
  - runs `claude plugin validate` when Claude Code is present;
  - lists skills whose replaced upstream hash changed;
  - prints the Pi version it ran against, when Pi is present.
- **Runtime checks:** `node` for Ponytail's Claude hooks and for Superpowers'
  visual companion, and `bash` for Superpowers' Claude hook. Name whatever
  is missing.
- **Ponytail notices:**
  - where the ledger writes, and how to turn it off;
  - in OpenCode, `PONYTAIL_DEFAULT_MODE` is read from the shared service's
    environment, not the pane's
    ([docs/apps/opencode.md](../../apps/opencode.md#agent-config-guard-devgeta-agent-config-guardserverjs)
    shows the same pattern for the guard).
- Errors follow [error-handling.md](../../guides/error-handling.md). Each one
  says what failed and the command to fix it.
- **Done.** Notes:
  - **Pack facts live in `pack.yaml`.** The runtime checks and the Ponytail
    notices are two optional keys, `requires` (`command` + `reason`) and
    `notices` (`text`, optional `coder`), so the engine names no pack. This
    extends ADR-0068 §3, which listed only repo, tag and SHA; flagged for the
    maintainer. A missing runtime is a warning printed after an install that
    put the pack somewhere, not a failure.
  - **Verified in the sandbox, not assumed:** the ledger notice's command,
    `echo '{"ledger":"false"}' | claude plugin configure ponytail@devgeta
--values-stdin`, saves and reads back as `"ledger": "false"`.
  - **`list`** also shows a coder that has a record but is no longer
    installed, and (for Pi, whose settings devgeta doesn't render) any
    difference between the record and Pi's settings, via an optional
    `Verifier`. A pack behind its pin gets the update command.
  - **`check`** runs each coder's optional `Inspector` on the built tree while
    it still exists: Claude runs `claude plugin validate`, Pi reports its
    version beside the one devgeta last checked. A validation failure fails
    the check. `--ref` fetches an unpinned tag (`Fetcher.FetchRef`), caches it
    by the commit it resolved to, and builds it with the saved exclusions.
  - **`update`** reinstalls into the coders that have the pack, with saved
    exclusions kept; with no pack it does every installed one, stopping at the
    first failure. The `devgeta` pack's version is recorded as `bundled`.
  - New wrapper methods: `Pi.ExecuteCommandOutput`. `Builder.Check` takes an
    `inspect` callback.
  - Runs: `dg agent-pack --help` and `list` were run against a sandbox `HOME`
    only. `refreshEmbeddedConfigs` extracts configs, so the command was never
    run against the real machine.
  - Test-first slip, caught and repaired: the engine's `Status`, `Update` and
    `Check` were written in the same edit as their tests, so no red run was
    seen. Mutation checks (each body replaced by a no-op) failed 3, 4 and 5
    tests, and two tests that panicked instead of failing were fixed.
- Verify: `go test ./cmd/ -run AgentPack`; `./devgeta agent-pack --help`.

##### Step 14: `dg install` integration

- `agent-packs` joins `knownCategories` and runs after the terminal category.
- `InstallTerminalApps` reports which coders were missing before
  `SoftInstall` and present after it. Only those get packs.
  - A coder that was already installed doesn't count, even when
    `SoftInstall` upgraded it to the pin.
- When a coder was skipped because it was already installed, print the
  `dg agent-pack install …` command for it.
- `--only agent-packs` installs no coders, so it installs the default packs
  into every configured coder instead (ADR-0068 §8). Without that, it would
  do nothing.
- A pack failure is reported and doesn't fail the other categories
  (principle 5).
- **Done.** Notes:
  - **Fresh vs. already there.** `InstallTerminalApps` asks an app that can
    say (an optional `IsInstalled()`: Claude, OpenCode and Pi now all do)
    whether it is present before and after `SoftInstall`, and records
    `FreshlyInstalled` and `AlreadyPresent` on the summary. An app that was
    present and got upgraded to the pin is `AlreadyPresent`. Skipped and
    filtered apps appear in neither list. `InstallAndConfigure` now returns
    the summary.
  - **Categories.** `agent-packs` is in `knownCategories`, runs by default
    after terminal, and is off for any `--only` that doesn't name it.
    `--only agent-packs` (also `--only terminal,agent-packs`) is explicit, so it
    targets every configured coder; `--skip agent-packs` installs none and
    wins over `--only`.
  - **The step.** The default packs are `agentpack.DefaultPacks` (`devgeta`,
    `superpowers`, `ponytail`, in that order). The terminal summary also lists
    non-coder apps, so the plan keeps only coders (`--coder git` would be an
    error). A pack that fails is reported with `dg agent-pack install <pack>`
    to retry and the next pack is still tried; an engine that can't be built
    prints one error and `dg install` goes on.
  - Three existing `parseInstallFlags` tests now list `agent-packs` among the
    categories that run by default; that is the intended behaviour change.
  - Not run: `dg install` end to end. It installs real software, so it is a
    Manual Verification step for the maintainer (13). `dg install --help` was
    checked from a scratch build.
- Verify:
  - `go test ./cmd/ ./internal/tooling/terminal/`;
  - `--skip agent-packs` and `--only agent-packs` parse;
  - a run with one coder freshly installed installs packs into that coder
    only.

#### Phase 4: parity and docs

##### Step 15: Parity to every coder

- Widen CLAUDE.md §10 and §12, and `agent-sync.md`, from "the two agents" to
  "every coder". Add Pi columns to the "Where each concern lives" table.
- Parity tests: Pi joins wherever it has an equivalent. Where it has none,
  the test checks that the gap is listed under "Accepted differences" in
  `agent-sync.md`, instead of skipping Pi silently.
- **Done.** Notes:
  - **What counts as parity.** Claude Code and OpenCode keep the permission
    comparison they had. Pi's own docs (1.5.0, `docs/usage.md`) say it does not
    ask before tool calls, so there is no permission list, guard or scratch
    grant to mirror; those are listed as gaps. Formatting is the one place Pi
    does have an equivalent, and it shares `format-file.sh` with Claude Code, so
    a root test pins that instead of comparing language lists.
  - **`coder_parity_test.go`** reads the guide's concern table and checks: a
    column per coder; every file a cell names exists; every gap is explained
    under "Accepted differences" in a bullet that starts
    `- **<Concern>: <Coder> has none`; every such bullet is still a gap; and the
    pack rows (`DEVGETA.md`, agent packs) are never gaps. Four mutations of the
    guide (unexplained gap, missing file, stale gap, no Pi column) each fail it.
  - CLAUDE.md §10 and §12 and the guides index now say "every coder".
  - `docs/apps/pi.md` still says "no permission or formatter parity" and "no
    shared skills"; both are stale (ADR-0059 §7, ADR-0060 §8). That is Step 16.
- Verify: `go test ./internal/apps/opencode/ ./internal/apps/pi/ .`.

##### Step 16: Docs

- **`docs/apps/agent-packs.md`** covers:
  - the command and its flags;
  - where overrides go and the wrapper form;
  - exclusions;
  - opting out of `@DEVGETA.md`, which means uninstalling the `devgeta` pack;
  - the Ponytail ledger and its OpenCode service environment;
  - conflicts with packs installed another way.
- **The three app docs:** what a pack adds, and that `--force` keeps it.
- **`docs/spec.md` and `README.md`:** the command, and the `dg install`
  default.
- **`docs/migrations/agent-packs.md`:**
  - the new `agent_packs:` section (additive; old configs load);
  - `@DEVGETA.md` coming back on `dg install`;
  - how to skip packs.
- **`ROADMAP.md`, `docs/recent-changes.md`.**
- Mark the investigation Concluded and the ADRs Accepted.
- **Done.** New: `docs/apps/agent-packs.md`, `docs/migrations/agent-packs.md`.
  Updated: the three app docs (including two stale `pi.md` lines and a false
  "never writes `~/.claude/CLAUDE.md`" in `claude.md`), `spec.md`, `README.md`,
  ROADMAP, recent changes. Every flag in the new docs was checked against the
  built `--help`.
  - The investigation is marked Concluded _except_ the session-level probes,
    which still need a login; marking it fully concluded would overstate it.
  - `docs/migrations/agent-packs.md` is **not** in the migrations index yet:
    that README lists a guide only once its change has shipped. Add the row in
    the release commit.
- Verify: read everything through; every command in the docs matches
  `--help`.

---

## 6. Verification Plan

### Automated Verification

```bash
go test ./internal/tooling/agentpack/... ./internal/apps/git/ ./internal/apps/claude/ \
        ./internal/apps/opencode/ ./internal/apps/pi/ ./internal/config/ \
        ./internal/tooling/terminal/ ./cmd/
go test .          # configs/ changed (embedded-config tests)
make lint
```

`pkg/paths` and `internal/config` both change, and almost the whole tree
depends on them
([CLAUDE.md §6](../../../CLAUDE.md#which-tests-to-run)). Run the full
`go test ./...` once before the cycle is marked done, and say that it was
run.

### Manual Verification

These run in the Step 1 sandbox. Running any of them on the real machine
needs the maintainer's yes for each command.

1. `dg agent-pack install superpowers`: all three coders report it
   installed, and `list` shows `v7.0.2` and the SHA.
2. Claude: a new session gets the `using-superpowers` bootstrap, a
   `superpowers:*` call resolves, and `/plugin` lists `superpowers@devgeta`.
3. `dg configure claude --force` and then `dg configure opencode --force`:
   both packs are still enabled.
4. OpenCode: a new session gets the bootstrap, and `opencode debug config`
   is warning-free.
5. Pi: the Superpowers extension loads from the built tree.
6. `--exclude <skill>`, where a kept skill names that skill: the build fails
   and names both skills. Nothing changes.
7. A user wrapper at
   `~/.config/devgeta/agent-packs/superpowers/skills/<x>/SKILL.md`: after
   `update`, all three coders use the wrapper.
8. `dg agent-pack install ponytail`: `/ponytail` works in all three, the
   ledger notice prints, and the existing `statusLine` is untouched.
9. `dg agent-pack install devgeta`: `@DEVGETA.md` is added once. Running it
   again leaves the file unchanged. Uninstall removes the line and says so.
10. `dg agent-pack uninstall superpowers`: the Claude plugin, the OpenCode
    entry and the Pi package are gone, and nothing else in those configs
    changed.
11. Install Superpowers from `claude-plugins-official` first, then run
    `dg agent-pack install superpowers`: it stops with the exact removal
    command, and installs nothing.
12. `dg agent-pack check superpowers --ref v7.0.1`: it builds, reports the
    checks and changed hashes, and installs nothing.
13. `dg install` with Claude already present and OpenCode missing: packs go
    to OpenCode only, and the output prints the command for Claude.
    `dg install --skip agent-packs` installs no packs.
14. `dg uninstall opencode`, then `dg agent-pack list`: OpenCode no longer
    appears under any pack.

### Regression Check

- `dg configure claude|opencode|pi` without packs installed renders the same
  output as before (golden tests).
- devgeta's own OpenCode plugins and Pi extensions still load.
- `dg install --help`, `dg list` and `dg uninstall claude` still work.

---

## 7. Risks & Trade-offs

| Risk                                                                          | Likelihood                     | Mitigation                                                                                                          |
| ----------------------------------------------------------------------------- | ------------------------------ | ------------------------------------------------------------------------------------------------------------------- |
| A Phase 0 probe fails (for example, Claude copies directory marketplaces)     | Med                            | Phase 0 comes before any code; the `+dg.<hash>` version on the built tree is the planned fallback                   |
| `--force` re-renders `settings.json` / `opencode.json` and drops pack entries | **Certain without Steps 9–10** | Both templates render pack entries from state; golden tests with and without packs                                  |
| Session-level probes need a login the sandbox doesn't have                    | Med                            | Step 1 stops and asks; CLI-level checks still run                                                                   |
| Code is built before a session proves the bootstrap arrives (Step 1 result)   | Low                            | CLI probes showed every coder loads the built tree; Manual Verification 2, 4, 5 decide, and a "no" reopens the ADRs |
| Pi is unpinned, so its side breaks on a Pi release                            | Med                            | `check` prints the Pi version; skipped coders are named, not hidden                                                 |
| Partial install across coders                                                 | Med                            | Build once, install everywhere, roll back on failure, write state last                                              |
| Upstream renames a skill that an override targets                             | Med                            | The build check fails on the bump; `check` lists changed hashes for full replacements                               |
| Network failure during `dg install`                                           | Med                            | The pack step fails alone; other categories are unaffected; a cached clone is reused on retry                       |
| State goes stale when a coder is uninstalled                                  | Med                            | Step 8: Claude and OpenCode uninstall clear their records; `list` checks Pi                                         |
| `DEVGETA.md` v2 carries devgeta-repo rules into users' repos                  | Low                            | Step 12 re-reads it against principle 8                                                                             |
| Ponytail runs per service, not per pane, in OpenCode                          | Low                            | Documented in `agent-packs.md`, the same way the guard bypass is                                                    |

### Trade-offs Made

- **Pack pins live in `configs/agent-packs/<pack>/pack.yaml`, not in
  `constants.PinnedVersion`.** A pin sits next to the overrides it was
  checked against, so a version bump is one folder's diff. Coder pins stay in
  Go.
- **Fetched at install time instead of embedded in the binary.** Fetching
  keeps upstream bytes exact and decouples upstream releases from devgeta
  builds. The cost is a network dependency
  ([investigation, "Considered, not adopted"](../../investigations/2026-11-08-agent-packs.md#considered-not-adopted)).
- **Skills removed at build time, not through Pi filters or OpenCode deny
  rules.** That gives one mechanism for all three coders, at the cost of a
  rebuild for every change.
- **Pack entries are rendered from state, not edited into the coder's
  files.** `dg configure --force` stays the single writer of `settings.json`
  and `opencode.json`. The state file is the source of truth, and hand edits
  to those files are still lost, as they are today.
- **`@DEVGETA.md` is added if missing.** A user who deletes the line gets it
  back on update. Opting out means uninstalling the `devgeta` pack, and the
  docs say so. This replaces ADR-0058's "add once".
- **`dg install` installs packs only into coders it installed in that run.**
  Re-running `dg install` never changes an existing coder's agent content. A
  pack that failed during `dg install` isn't retried; the output prints the
  command to run.
- **No overrides ship from devgeta in this cycle.** The mechanism lands
  without a first user. Each override is a separate change, reviewed against
  principle 8.

---

## 8. Cross-Model Review Notes

- [x] Domain context clear?
- [x] Engineer context sufficient? It now covers the `--force` re-render and
      uninstall paths found in review.
- [x] Objective unambiguous?
- [x] Scope locked? Overrides, devgeta skills and retry for existing coders
      are out.
- [x] Steps actionable?
- [x] Verification executable? It runs in the Step 1 sandbox.
- [x] Risks realistic?

**Reviewer notes (2026-11-08):**

- The first draft missed that both coders' `--force` re-renders their whole
  config file, which would silently disable every pack. Fixed in Steps 9–10.
- The first draft missed that `dg uninstall claude|opencode` deletes the
  config dir, which leaves pack state stale. Fixed in Step 8.
- ADR-0058's `claude_instructions_imported` key is already gone, so no
  migration is needed for it.
- Phase 0 order was reversed: the probes now come before the ADRs, because
  the ADRs depend on what the probes find.
- Pi's `declaredExtensions` only matches the curated list. Conflict detection
  needs a general reader, which is split out rather than duplicated.

---

## Notes for Implementers

- **Phase 0 is done.** Start at Step 3. Read the three ADRs and the
  investigation's "Probe results" first.
- **Test first, every step.** See §2 "Testing": red, green, refactor. A
  step's code lands only alongside the tests that drove it.
- **Sandbox only.** Never run a pack install, a `dg configure` or a coder CLI
  write on the real machine without the maintainer's yes for that command.
  Sandbox runs go under `~/.cache/devgeta/scratch/agent-packs-probe/`.
- **OpenCode in a sandbox needs `--standalone`** (or a private
  `opencode serve --port <n>`). Otherwise it talks to the maintainer's
  running shared service.
- **No real credentials** in any probe or manual check without the
  maintainer's yes.
- **Commit after each step** once its tests and Verify check pass, one commit
  per step.
- **Escalate** any probe result or check that contradicts the investigation
  or an ADR. Stop; don't work around it.
- **Tick the boxes** in §4 as steps finish. Mark the cycle Complete only
  after the full `go test ./...` and the manual checks.
