# Investigation — Resetting the shared skills, commands and agents

**Date:** 2026-11-06
**Status:** Open
**Feeds:** a cycle doc for the reset; ADRs listed in [Decisions this will need](#decisions-this-will-need)

## Question

`configs/shared/` ships 18 skills, 14 commands and 3 agents to every user's
Claude Code and OpenCode. The set has grown by addition, several pieces do the
same job, and a few are very large. The plan is to remove everything devgeta
wrote itself, keep Superpowers and Ponytail, and then rebuild the custom
pieces on top of those upstream skills without editing them.

That raises five questions:

1. Do Anthropic's skills work, and get delivered, the same way in every coder?
2. What exactly should be removed, and what else breaks when it goes?
3. How can a vendored skill be customized without changing the upstream copy?
4. Where should the rebuild happen: a `configs/sharedv2/` folder or a branch?
5. What other skills (development, communication, natural-sounding writing)
   are worth adding, and what belongs in the global context file instead,
   so that Pi gets it too?

## Scope

- **In:** `configs/shared/{skills,commands,agents}`, `configs/shared/DEVGETA.md`,
  how devgeta deploys them, and what Claude Code, OpenCode and Pi each load.
- **Out:** hooks, permissions, the Pi extension set
  ([2026-11-05-pi-extension-set.md](2026-11-05-pi-extension-set.md)), and
  removing `dg task` subcommands that only existed to support a removed command
  (listed in Shared work, but deciding on them is separate).

## Method

- Sources: files in `configs/shared/`, `embedded.go`,
  `internal/apps/baseapp/configure.go`, `docs/guides/vendored-skills.md`,
  ADR-0059. For Pi: the docs and `dist/core/skills.js` from the
  `@earendil-works/pi-coding-agent` 1.5.0 tarball, unpacked in devgeta's
  scratch directory. Web searches for community skills and override patterns,
  run on 2026-11-06.
- Criteria, set before looking:
  - The pattern leaves upstream files byte-for-byte unchanged, so a sync is a
    plain copy.
  - It works unchanged in Claude Code, OpenCode and Pi.
  - It adds no cost to skills nobody customized.
  - A recommended skill is portable (a `SKILL.md` with no vendor-only tools),
    has a clear license, and does not compete with Superpowers or Ponytail for
    the same requests.
- Not done: nothing was installed or run. OpenCode's current docs were not
  re-read. The community repos below were found by search and have **not been
  read**: their licenses, maintenance and content are unverified. Usage of each
  command was not measured.

## Summary

| Item                                         | Action                     | Why, in a few words                                                            |
| -------------------------------------------- | -------------------------- | ------------------------------------------------------------------------------ |
| Superpowers (15 skills)                      | Keep, unchanged            | Portable, maintained upstream, ships notes for Pi                              |
| Ponytail (`ponytail`, `ponytail-debt`)       | Keep, unchanged            | Portable, small                                                                |
| All 14 commands, 3 agents, `post-to-tracker` | Remove                     | In-house; to be rebuilt                                                        |
| `dg-worktree`                                | Remove, then rebuild       | Overlaps Superpowers' `using-git-worktrees`; becomes a wrapper around it       |
| `skill-creator` (Anthropic)                  | Remove                     | Its tooling calls `claude -p`; `writing-skills` covers it portably             |
| Wrapper + `UPSTREAM.md` pattern              | Adopt, only where needed   | Upstream file stays unchanged; works in all three coders                       |
| `configs/sharedv2/`                          | Reject; use a branch       | Anything under `configs/` is embedded and tested; a branch can deploy and test |
| `humanizer`                                  | Needs a read before adding | Matches the removed `human` command; portable `SKILL.md`                       |
| `DEVGETA.md` v2                              | Drafted (Appendix A)       | Absorbs the general rules from the removed commands; ships with the reset      |
| `DEVGETA.md` for Pi                          | Adopt                      | A devgeta-owned Pi extension appends it; user files untouched                  |

---

## Findings

### 1. Anthropic skills: same format, not the same content

- **Evidence:**
  - Skills are folders with a `SKILL.md`, following the Agent Skills
    specification (agentskills.io). Pi 1.5.0 `docs/skills.md`: "Pi implements
    the Agent Skills specification". It points to `anthropics/skills` for
    examples.
  - Devgeta downloads nothing at install time. A vendored skill is a pinned
    copy, embedded in the binary (`embedded.go:15`, `//go:embed all:configs`)
    and copied out by `baseapp.SyncSharedParts`
    (`internal/apps/baseapp/configure.go:30`). An Anthropic skill and a
    Superpowers skill are delivered in exactly the same way.
  - `skill-creator`'s tooling is tied to Claude: `scripts/run_eval.py:46,71`
    runs `claude -p`, and `SKILL.md:432` says that step is "only available in
    Claude Code". It also has sections for Claude.ai and Cowork
    (`SKILL.md:420,445`).
- **Verdict:** Any skill in this format loads in all three coders. Whether it is
  useful everywhere depends on its content. Before vendoring a skill, grep it
  for `claude -p`, `claude.ai` and Claude-only tool names.

### 2. What to remove

- **Remove:**
  - Every file in `configs/shared/commands/` (14) and `configs/shared/agents/` (3).
  - The in-house skills `post-to-tracker` and `dg-worktree`.
  - The vendored `skill-creator`.
- **Keep:** the 15 Superpowers skills and the two Ponytail skills, unchanged.
  Keep the whole Superpowers set, including `diagnosing-superpowers`, which
  is rarely needed. Vendoring only part of a set adds a list of exceptions to
  every sync, and that list is exactly the complexity this reset is meant to
  remove.
- **Also remove `DEVGETA.md`?** No. It is the global context file, not a skill.
  It becomes v2 and also reaches Pi (finding 6).
- **Why `dg-worktree` goes even though it's legitimate:** Superpowers'
  `executing-plans` and `subagent-driven-development` call
  `superpowers:using-git-worktrees` by name
  (`executing-plans/SKILL.md:111`, `subagent-driven-development/SKILL.md:104`,
  `writing-plans/SKILL.md:14`). A
  separate `dg-worktree` competes with that skill for the same requests
  instead of plugging into it. Rebuilt as a wrapper around
  `using-git-worktrees` (finding 3), every Superpowers flow that sets up a
  worktree would use `dg worktree` automatically.

### 3. Customizing a vendored skill: wrapper `SKILL.md` + `UPSTREAM.md`

Your proposal: the upstream `SKILL.md` is renamed to an "original" file, and a
new `SKILL.md` points to it with the changes.

- **Evidence:**
  - Each coder only treats a file named `SKILL.md` as a skill. Pi's loader
    stops at the first `SKILL.md` in a folder and does not look further down
    (`dist/core/skills.js:117,137`). It only picks up loose `.md` files at the
    top of a skills folder (`:195`), so an extra `.md` beside a `SKILL.md`
    can't be loaded as a second skill.
  - The same separate-layer idea shows up in other projects, found by search
    and not read in depth: an `overlay.md` read beside a skill (the
    `manage-skills` skill on skillsmp.com), an `ADAPTATION.md` next to a
    byte-identical vendored `SKILL.md` (Quidge/firstmate PR #18), and a
    per-project config file that a skill reads (freeCodeCamp article). None of
    these is a standard.
  - Relative paths inside the upstream text (`references/pi-tools.md`,
    `scripts/task-brief`) still resolve, because the file stays in the same
    folder.
- **Verdict:** The pattern meets the criteria, with four details that make it
  work:
  1. **Call the copy `UPSTREAM.md`, not `ORIGINAL.md`.** The name says where
     it came from, and a sync rule can say "replace `UPSTREAM.md`".
  2. **The wrapper keeps the same `name`.** Superpowers skills call each other
     by name, so a wrapper named `using-git-worktrees` takes over every call.
     A different name would leave the other skills calling a skill that no
     longer exists.
  3. **The wrapper says which file wins.** For example: "Read `UPSTREAM.md` in
     this folder and follow it, with these changes. Where they conflict, this
     file wins." Its `description` is a copy of upstream's, and a sync has to
     check that it still matches.
  4. **Only wrap skills you actually change.** An untouched skill stays a plain
     `SKILL.md`. Wrapping all of them would add an extra file read to every
     use and double the sync work, for nothing.
- **Sync then becomes a copy:** replace `UPSTREAM.md` and the upstream support
  files, leave the wrapper alone. That retires the "Known differences" table in
  `vendored-skills.md`, including the `task-brief` patch, which moves into a
  wrapper or a local script.
- **Make it enforced, not remembered:** a test hashes each `UPSTREAM.md`
  against the commit pinned in the manifest. An accidental edit then fails the
  build (CLAUDE.md §4, "prefer making it structurally impossible").
- **Cheaper option for preferences:** a change that applies everywhere ("keep
  replies short") goes in the global context file, not a wrapper.
  `using-superpowers/SKILL.md` says "User instructions (CLAUDE.md, AGENTS.md
  ...) take precedence over skills". Use a wrapper only for a change to one
  skill's steps, such as "use `dg worktree`".

### 4. `configs/sharedv2/` versus a branch

- **Evidence:** `embedded.go:15` embeds all of `configs/`, so a `sharedv2/`
  folder would ship inside every binary built from `main` while it is
  unfinished. Tests walk the embedded tree (`reviewer_agents_test.go`,
  `embedded_test.go`). `SyncSharedParts` reads only `configs/shared`, so you
  can't deploy `sharedv2/` to try it out without writing deploy code that gets
  thrown away later.
- **Verdict:** A branch (or a `dg worktree`) gives the same isolation without
  those problems. You can build and `dg configure` from the branch to try the
  new set for real, and merging the branch is the "move into place". The
  "remove everything" step is then one commit on that branch, and the
  rebuild follows on top of it.

### 5. Skill recommendations

All found by search on 2026-11-06. **None of the repos were read**, so treat
every row as a lead to check, not a recommendation to install.

| Area               | Candidate                                                            | Fit                                                                                                                                                                               |
| ------------------ | -------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Natural writing    | `humanizer` (originally blader/humanizer; forks by chi-feng, jooray) | Based on Wikipedia's "Signs of AI writing". Plain `SKILL.md`, described as working in any coder. Closest match to the removed `human` command. Read its license and content first |
| Communication      | Anthropic `internal-comms`, `doc-coauthoring` (anthropics/skills)    | Status updates and document writing. From memory; not re-checked that they still exist or contain no Claude-only steps                                                            |
| Development        | nothing new                                                          | Superpowers already covers planning, TDD, debugging, review and worktrees. Listicles mostly repeat it                                                                             |
| Development, niche | Anthropic `mcp-builder`, `webapp-testing`, `frontend-design`         | Only if that kind of work comes up                                                                                                                                                |

- **Rejected:** `planning-with-files` (OthmanAdi, listed as about 13.4k stars)
  competes with Superpowers' `writing-plans` for the same requests.
  Listicle-style "Code Review Assistant" and similar skills appear to be role
  descriptions, not published repos.

### 6. Global context file: `DEVGETA.md` v2, read by all three coders

- **Evidence:**
  - Devgeta owns `DEVGETA.md` and copies it into each coder's config folder
    (`internal/apps/baseapp/instructions.go`, ADR-0058). Claude Code loads it
    through an `@DEVGETA.md` line added once to `~/.claude/CLAUDE.md`.
    OpenCode loads it through the `instructions` field of `opencode.json`. The
    user's own `CLAUDE.md` and `AGENTS.md` stay theirs.
  - Pi loads `AGENTS.override.md`, `AGENTS.md` or `CLAUDE.md` from
    `~/.pi/agent/` for every folder (Pi 1.5.0 `docs/configuration.md`,
    "Context files"). Those are the user's files, the same way `CLAUDE.md` is.
  - Pi also reads `~/.pi/agent/APPEND_SYSTEM.md`, but a project's own
    `.pi/APPEND_SYSTEM.md` replaces it and the two "are not combined" (same
    doc). Any repo that ships one would silently drop devgeta's text.
  - A Pi extension can add to the system prompt on every turn through
    `before_agent_start` (Pi 1.5.0 `docs/extensions.md:615-650`). Devgeta
    already ships and owns one Pi extension, `devgeta-format.js`
    (`internal/apps/pi/pi.go`, ADR-0063).
  - No test checks the wording of `DEVGETA.md`. The tests in `baseapp`,
    `claude`, `opencode` and `cmd` only check that every coder gets the same
    bytes. They passed with the v2 draft in place (687 tests, run
    2026-11-06), so adopting it needs no test changes.
- **Verdict on content:** v2 is drafted in [Appendix A](#appendix-a--devgetamd-v2-draft).
  `configs/shared/DEVGETA.md` stays as it is until the reset branch lands,
  because it ships to every user on their next `dg configure`. The draft keeps
  v1's "Response style" and "Writing code" sections and adds what the removed
  commands contributed that applies in any repo, in a few lines each:
  - **Honesty:** facts come from what was checked, and certainty is never
    raised. This came from `human` and `explain-simply`.
  - **Explaining:** teach when the reader is new, stay on topic, and replace
    references with what they say. This came from `explain-simply` and
    `teach`.
  - **Writing for other people:** messages, PR descriptions, commits and
    ticket comments. This came from `human`.
  - **Comments explain why.**

  Two things were left out on purpose. The fixed output shapes (`human`'s
  single code block, `explain-simply`'s 200-word target) are request formats,
  not defaults, so they belong in a skill if they come back. Vendor-specific
  wording is also left out, so the same file reads correctly in all three
  coders. The file grew from 1.6 KB to 2.7 KB and is loaded in every
  session, so anything added later must be just as general.
- **Verdict on Pi delivery:** use a devgeta-owned extension,
  `devgeta-instructions.js`, that reads `~/.pi/agent/DEVGETA.md` and appends
  it in `before_agent_start`. It plays the same role as OpenCode's
  `instructions` entry: a file devgeta owns, loaded alongside the user's own
  `AGENTS.md`, never in place of it.
  - Writing into the user's `AGENTS.md` breaks ADR-0058.
  - `APPEND_SYSTEM.md` loses to any project copy.
  - Whether Pi expands `@DEVGETA.md` imports stays unverified (step 2 below),
    and the extension doesn't depend on it.
- **What belongs where:**
  - **`DEVGETA.md`:** preferences that apply in every repo (tone, honesty,
    code style).
  - **Wrapper:** a change to one skill's steps.
  - **New skill:** a new workflow, or a fixed output format someone asks for.
  - **Never in `DEVGETA.md`:** anything only one project needs (CLAUDE.md
    principle 8).

---

## Considered, not adopted

| Item                                            | Why not                                                                                               |
| ----------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Editing upstream `SKILL.md` in place            | Every sync becomes a merge; that is the "Known differences" table we already carry                    |
| Patch files applied at build time               | Fails whenever upstream moves a line; harder to read than a wrapper                                   |
| Anthropic's plugins (`claude-plugins-official`) | A plugin installs into Claude Code only; devgeta must ship the same set to OpenCode (`agent-sync.md`) |
| `configs/sharedv2/`                             | See finding 4                                                                                         |

## Shared work

- **Removal already reaches users.** `SyncSharedParts` empties each destination
  folder before copying (`configure.go:34`). What you delete from
  `configs/shared/` disappears from users' machines on their next
  `dg configure claude --force` or `dg configure opencode --force`.
- **That same emptying deletes skills users wrote themselves.** The
  destination is the coder's whole `skills/`, `commands/` or `agents/` folder
  (`claude.go:362`, `opencode.go:342`). Fix this first, by tracking which
  entries devgeta installed and removing only those, so the rebuild doesn't
  delete a user's own skills on the next deploy.
- **Tests and code tied to the removed pieces:**
  - `reviewer_agents_test.go` (root package)
  - `internal/apps/opencode/permissions_test.go`
  - `configs/claude/task-redirect.sh` and its tests
  - `docs/guides/vendored-skills.md`, `docs/guides/agent-sync.md`
  - `dg task` subcommands that only the removed commands call, such as
    `review-run`. Decide on these separately.

## Verification still needed

1. **OpenCode folders.** Re-read OpenCode's current docs for its global
   `AGENTS.md` location, and check whether it reads `~/.agents/skills/`.
2. **Pi extension.** In a sandboxed `PI_CODING_AGENT_DIR`, confirm that text
   appended in `before_agent_start` reaches the model and survives compaction.
   Whether Pi expands `@` imports is no longer needed for the plan.
3. **Wrapper in practice.** Build one wrapper (`using-git-worktrees` → `dg
worktree`) on the branch and confirm all three coders follow the wrapper
   over `UPSTREAM.md` where they conflict.
4. **Read `humanizer`** and the Anthropic communication skills for license,
   maintenance and Claude-only steps before vendoring.

## Decisions this will need

- **ADR:** vendored skills are customized only through a wrapper `SKILL.md`
  plus a byte-identical `UPSTREAM.md`, enforced by a hash test. This replaces
  the "Known differences" practice in `vendored-skills.md`.
- **ADR:** deploys remove only entries devgeta installed (Shared work).
- **ADR-0059 §7 amendment, and an ADR-0058 amendment:** Pi receives
  `DEVGETA.md` through a devgeta-owned extension, and possibly the shared
  skills.
- **The maintainer's call:** whether to keep the `dg task` subcommands that
  only removed commands used.

## Appendix A — `DEVGETA.md` v2 draft

Proposed replacement for `configs/shared/DEVGETA.md`, applied on the reset
branch, not before.

````markdown
# Response style

- **Lead with the answer.** Put the answer or the outcome first, then only what the reader needs to act on it.
- **Size the reply to the question,** not to the effort it took. Drop any sentence that tells the reader nothing new.
- **No filler.** No preambles, restating the question, praise, apologies, hedging, recaps of what you just did, or offers of more help.
- **Plain words.** Use words any engineer understands on first read. Spell out terms, codenames and abbreviations the first time they appear.
- **No decoration.** Write prose. Use headers, bold labels, tables or lists only when the content has separate parts the reader will scan.
- **One recommendation.** Mention an alternative only when it is a real contender.
- **Never cut substance to look short.** Keep the reasoning behind non-obvious choices, risks and edge cases the reader would miss, assumptions, failures and uncertainty.

# Honesty

- **Facts come from the code, docs, command output or the conversation,** never from guesswork. If you could not check something, say so.
- **Keep certainty as it is.** "Should be fixed" stays tentative, a failure stays a failure, a skipped step is named. Never make work sound more done or more certain than it is.

# Explaining

- **When the reader is new to something,** explain what it is and how it works, with a short example. Assume no background, but stay on the topic: explain a term only when it blocks understanding.
- **Replace references with their content.** Instead of "see the ticket" or "per the design doc", say in a sentence what that source says.

# Writing for other people

Messages, PR descriptions, commit messages, comments on tickets:

- **Sound like an engineer writing to teammates,** not like an AI. Simple wording, no corporate language or buzzwords.
- **Say it once.** No closing line that restates the point, no greeting or sign-off unless asked.
- **Keep the details the reader needs** and drop the rest. A chat message is one or two short paragraphs.
- **Questions are direct, suggestions are actionable.**

# Writing code

- **Reuse before writing.** Prefer what the codebase already has, then the standard library, then a dependency already in use, and only then new code. When two places need the same logic, extract it once instead of copying it.
- **Keep functions focused.** One job per function, readable top to bottom, without splitting it into fragments the reader has to jump between.
- **Use names that last.** Say what the thing is or does in plain words someone new would understand. Avoid abbreviations, task-specific jargon, and names like `data2` or `newFix`.
- **Comments explain why,** not what the code already says.
````
