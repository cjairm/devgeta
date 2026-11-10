# Cycle: Review skills — `review` and the feedback wrapper

**Date:** 2026-11-09
**Estimated Duration:** ~1–2 days
**Status:** Done (released in v2.2.0, 2026-11-09)

---

## 1. Domain Context

devgeta 2.0 installs agent packs (Superpowers, Ponytail, devgeta's own) into
Claude Code, OpenCode and Pi ([ADR-0068](../../decisions/ADR-0068-agent-packs-are-a-pinned-upstream-plus-a-build-layer.md)).
Two review jobs are not covered well today:

- **Reviewing work.** Superpowers' `requesting-code-review` reviews code against
  a plan, from inside its own workflow, by dispatching a subagent. Nothing
  reviews a design doc or plan on its own before code exists, and nothing
  posts a review to a pull request (PR).
- **Addressing feedback.** Superpowers' `receiving-code-review` has the right
  judgment (verify each comment, push back with reasons, reply in the thread),
  but it does not gather every comment on a PR, resolve threads, or ask for
  another review.

The usual way this is used: work is written with one model (say Claude), then
reviewed in a separate session with another model (GPT, GLM) in whichever
coder runs it. "Cross-model" needs no machinery: the person starts the review
in the other coder or model. The skills must therefore work the same in all
three coders.

**Lesson from 1.x.** devgeta 1.x shipped `review-pr` (285 lines),
`address-feedback` (158), a `document-reviewer` agent, a review journal and
about a dozen `dg task` helpers, plus `dg task review-run` to fan out models.
It was removed as noise ([reset investigation](../../investigations/2026-11-06-skills-and-agents-reset.md)).
This cycle builds the smallest version that does the job, on `gh` directly,
and grows only when a real need shows up. The 1.x files remain readable at
`d7adb89` (`git show d7adb89:configs/shared/commands/review-pr.md`).

---

## 2. Intent, goals, non-goals

**Intent.** One way to review any stage of work — a design doc, a plan, code,
or a mix — with fresh eyes, that leaves a clean review on the PR when there is
one; and one way to work through all review feedback and close it out.

**Goals**

- Review a design doc or plan alone, code alone, or both.
- Review in order of cost to fix: problem → design → plan → code. A blocker in
  an earlier stage stops the later stages.
- Weight security, regressions and test quality highest.
- Every finding: severity (blocker / should-fix / nit), where, what's wrong,
  the fix, why it matters — readable by a junior engineer on first read.
- On a PR: inline comments on the exact lines, a final summary note, and a
  verdict (approve or request changes).
- Address feedback: see every comment (people and bots), fix or answer each,
  reply short and plain, resolve what's handled, and ask for a review only if
  the PR isn't approved yet.
- Works in Claude Code, OpenCode and Pi.

**Non-goals**

- Launching other models or coders (1.x `review-run`). The person picks the
  model by where they run the review.
- A review journal or any state outside GitHub and the repo.
- New `dg task` helpers. The skills call `gh` and `git` directly.
- Removing or editing any upstream skill.

**Anti-goals** (it technically works but has failed)

- A review a junior engineer can't act on: vague, jargon-heavy, or long.
- Repeating a point an open PR thread already makes.
- Anything devgeta-specific inside the skills (cycle docs, our test policy,
  our release rules). The skills read each repo's own conventions instead
  (principle 8).

---

## 3. Constraints

- **Superpowers is wrapped, never copied or edited** ([ADR-0069](../../decisions/ADR-0069-pack-customization-only-through-a-build-time-override-layer.md)).
  A wrapper keeps upstream's `name` and `description`; upstream's text stays
  beside it as `UPSTREAM.md`.
- **New skills can only ship in the `devgeta` pack** (ADR-0069 §3). Today that
  pack carries instructions only (`DEVGETA.md` and its Pi loader), so it must learn to carry skills
  ([ADR-0071](../../decisions/ADR-0071-the-devgeta-pack-ships-skills.md)).
- **GitHub does not allow approving or requesting changes on your own PR.**
  The reviewer usually runs under the PR author's `gh` login.
- **Posting is outward-facing.** Command/skill frontmatter does not grant
  authority in Claude Code or OpenCode, so each skill states in its own prose
  what running it authorizes (CLAUDE.md §12).
- **Coder parity.** Every coder gets the same skills; `docs/guides/agent-sync.md`
  must say where they live for each coder (`coder_parity_test.go`).
- `gh` is needed only for PR work. Without it (or without a PR) `review` still
  reports in the session.

---

## 4. Approaches considered

1. **Wrap both Superpowers skills.** No engine work. But a wrapper must keep
   upstream's description ("use when completing tasks … before merging"), so
   "review this plan" in another model may not trigger it; and
   `subagent-driven-development` calls `requesting-code-review` after every
   task, so added PR posting would fire there too.
2. **Two new skills in the `devgeta` pack.** Clear descriptions, but the
   feedback skill would repeat judgment `receiving-code-review` already has.
3. **Mixed — chosen.** A new `review` skill in the `devgeta` pack (nothing
   upstream reviews a doc alone or posts to a PR), and a wrapper over
   `receiving-code-review` that adds only the PR mechanics. No duplication;
   upstream used where it fits. Cost: the one-time engine change so the
   `devgeta` pack can ship skills, which also makes later devgeta skills cheap.

---

## 5. Design

### 5.1 Engine: the `devgeta` pack ships skills

`configs/agent-packs/devgeta/skills/<name>/SKILL.md` becomes part of the
embedded pack, and each coder loads it the way it loads other packs' skills:

| Coder       | How the skills load                                                                                                                                                                                                                                               |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Claude Code | The built `devgeta` tree gets a `.claude-plugin/plugin.json` and is listed in the existing `devgeta` marketplace like the other packs; installed as `devgeta@devgeta`. The `@DEVGETA.md` import stays as it is.                                                   |
| OpenCode    | Builder's call after a probe: a skills path in `opencode.json` if OpenCode 2.2.6 supports one, otherwise a minimal plugin (`index.js`) that registers the skills folder the way Superpowers' plugin does (`ctx.skill.transform`). The `instructions` entry stays. |
| Pi          | Add the skills folder to the existing `package.json` `pi` section; the package is already installed with `pi install <tree>`.                                                                                                                                     |

Uninstall, `--exclude`, overrides and the dangling-reference check apply to
devgeta's skills exactly as to upstream ones. `pack.yaml` gains
`requires: gh` (a warning, not a failure, per `manifest.go`).

### 5.2 Skill: `review` (new, `devgeta` pack)

Description (what triggers it): review a design doc, plan, or code change —
or a PR — before it's built on or merged; for "review this", "review the
plan", "review PR 12".

**What it reviews.** What the person names (files, a branch, a PR number or
link). Nothing named: the current branch against its merge base with the
default branch, including uncommitted and untracked files. On a PR, the PR's
diff.

**Which stages run.** Sort what's in scope into design docs, plans and code.
The repo's own conventions decide what counts as which (its instructions
file, templates and doc folders). Run the stages present, in order. A blocker
in an earlier stage ends the review there with the verdict "fix the design
first" — later stages are not reviewed against an unsettled design.

**Before judging.** Read the repo's instructions and contributing docs and any
template the doc should follow; repo rules beat the skill's defaults. When
code is in scope, read the linked design doc or plan if one exists. If this
session wrote the work, say so at the top and recommend a fresh session or
another model, then continue.

**Checklists** (condensed from the maintainer's review guide; a check runs
only when the stage is present):

- _Design doc:_ problem stated before solution (who has it, what happens if
  we do nothing); goals, non-goals, success measure; at least two real
  alternatives with reasons; assumptions marked verified or not, riskiest
  first; failure modes (partial failure, retries, concurrency, bad input,
  load, tenant isolation where relevant); hard-to-reverse choices (data model,
  public API, security boundary) get the deepest pass; how we'll know it's
  broken, roll back, migrate.
- _Plan:_ every task traces to a requirement and back; riskiest work first;
  each step has a testable definition of done; dependencies and rollback per
  step; anything outside the goals is cut.
- _Code:_ intent first (if the description and tests don't say what it does,
  that's the first finding); matches the approved design, deviations
  explained; correctness (edge cases, error paths, concurrency, idempotency,
  transactions) — run the tests when possible; **security** (authorization on
  every path, input validation, secrets, migration safety); **regressions**
  (grep every caller of a changed function or behavior); **tests** (would they
  fail if the logic were wrong? behavior, not implementation); simplicity
  (deletable code, one-user abstractions); can someone debug it in six months.

**Feedback upstream.** A code finding that shows a design flaw also says which
doc needs the fix.

**Every finding** has: severity, `file:line`, _what's wrong_, _the fix_, _why
it matters_ — each one or two short sentences, plain words, a term explained
the first time. Only blockers stop approval. No finding without a concrete
case.

**Re-review.** When the PR already has review threads, check each earlier
blocker first (resolved or not, with evidence), then review only what changed
since. Never repeat a point an open thread already makes.

**Output.**

- _No PR named (or no `gh`):_ the report in the session — one line on what the
  change does, findings grouped by severity, verdict, a "not checked" line.
- _PR named:_ running the skill on a PR the person named authorizes posting,
  without asking first (see the rule at the end of §5.4).
  One GitHub review: an inline comment on each finding's line when the line
  is in the diff (otherwise the finding goes in the summary); a summary body
  with what the change does, the findings list, the verdict and "not
  checked". Event: `REQUEST_CHANGES` with blockers, `APPROVE` without. When
  the PR author is the current `gh` user, the event is `COMMENT` and the
  summary's first line is the verdict ("Verdict: approve" / "Verdict: request
  changes").
- The skill never edits files.

### 5.3 Skill: `receiving-code-review` wrapper (`devgeta`'s Superpowers override)

`configs/agent-packs/superpowers/skills/receiving-code-review/SKILL.md`, a
wrapper: same name and description; tells the agent to follow `UPSTREAM.md`
for judging each item and adds only the PR steps. Running it on a PR the
person named authorizes committing, pushing and posting replies, without
asking first (see the rule at the end of §5.4).

1. **Find the PR** the person named: a number, a link, or "the PR" (the
   current branch's). No PR named: upstream's process on whatever feedback is
   in the conversation, and stop.
2. **Gather everything** before changing anything: review summaries, every
   unresolved inline thread, and top-level PR comments, from people and bots.
3. **Judge each item** with upstream's process (verify against the code, push
   back with reasons, ask when unclear).
4. **Fix** in logical batches, run the repo's tests for what changed, commit,
   push.
5. **Reply to every item**, short and plain: `Fixed in <sha>: <what changed>`,
   `Not changing: <reason>`, or the question. Inline threads get the reply in
   the thread; top-level comments and review summaries get one PR comment
   listing them.
6. **Resolve** each thread that was fixed or answered with a reason. Leave a
   thread open only when the reply is a question to the reviewer.
7. **Close out.** If the PR has no approval, request a review again from the
   people who reviewed (bots excepted). If it's approved, don't ask again —
   the remaining work is nits — and say it's ready.

### 5.4 Calls made after the builder check (author's calls, open to correction)

- **An excluded skill beats devgeta's shipped override.** Today the build
  refuses a skill that is both excluded and overridden, in either layer
  (`checkSkillTarget`, ADR-0069 §6). That would break the next install for
  anyone who already excludes `receiving-code-review`. devgeta's own layer
  therefore drops an override for an excluded skill silently; the error stays
  for the user's own overrides. This amends ADR-0069 §6 (noted in ADR-0071).
- **Existing installs pick the skills up on their next
  `dg agent-pack install devgeta` or `update`.** Updating devgeta changes
  nothing on disk by itself (2.0 behavior). The Claude installer stops
  returning early for the embedded pack: it registers the plugin _and_ keeps
  the `@DEVGETA.md` import; uninstall removes both. One line in
  `docs/migrations/` says which command to run.
- **The skill is named `review` unless the probe finds a collision** with a
  built-in in OpenCode or Pi; then it becomes `review-work`. In Claude Code
  plugin skills are namespaced (`devgeta:review`), so there's no name clash,
  only trigger competition, which the description handles. The manual check
  confirms which skill "review this plan" picks in each coder.
- **`requires: gh` stays a pack-wide warning.** Without `gh`, `review` still
  reports in the session.

**GitHub actions only when the person points at a PR** (maintainer's
decision, 2026-11-09). A skill posts, commits or pushes without asking only
when the person names a PR: a number, a link, or plainly "the PR". An open PR
on the current branch is not enough. Otherwise `review`
reports in the session and the wrapper behaves as plain upstream. Reason: the
wrapper keeps upstream's broad description, so it also fires inside
`subagent-driven-development`; treating "the branch has an open PR" as
permission could post or push mid-workflow.

### 5.5 Left to the builder

- The exact `gh` calls (REST vs GraphQL; resolving threads needs GraphQL).
- The OpenCode loading mechanism (5.1), after the probe.
- Wording and length of both skills — the target is the size of the
  Superpowers skills they sit next to, not the 1.x commands.

---

## 6. Verification

- Engine: unit tests in `internal/tooling/agentpack/` for the devgeta pack's
  skills in each installer (mocked, no real commands); the existing
  "same bytes to every coder" test extended to skills.
- Skills: an embedded-config test that both skills exist, that the wrapper
  references `UPSTREAM.md`, and that neither names devgeta-specific paths.
- `docs/guides/agent-sync.md` updated; `coder_parity_test.go` passes.
- ADR-0069 §6 gains an amendment note for the excluded-skill rule (§5.4),
  following its existing 2026-11-08 note, so the ADR matches the build.
- Manual (maintainer runs; writes to the real machine):
  `dg agent-pack install devgeta superpowers`, then in each coder run `review`
  on a doc-only change, a code-only change and a mixed one, on a scratch PR;
  then the feedback wrapper on that PR's comments.
- Outcome (2026-11-09): `dg agent-pack check devgeta` passed (including
  `claude plugin validate`); the install loaded the skills in Claude Code and
  OpenCode. The scratch-PR runs and the Pi install were skipped by the
  maintainer's call (Pi is not installed on the test machine). The name probe
  found no `review` name clash, but Ponytail's `ponytail-review` wins plain
  code reviews on its description; `review`'s description was narrowed to
  design docs, plans and posting to a PR, and `/devgeta:review` calls it
  directly.

---

## 7. Risks

| Risk                                                            | Mitigation                                                                                                                   |
| --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| OpenCode 2.2.6 offers no clean way to load a skills folder      | Probe first; fall back to a minimal plugin like Superpowers'                                                                 |
| Wrapper drifts when Superpowers changes `receiving-code-review` | The build re-checks name/description on every pin bump (ADR-0069 §6); the wrapper adds steps and defers judgment to upstream |
| Skills grow back into 1.x size                                  | Non-goals above; additions need a real failed review to justify them                                                         |
| Auto-posting a wrong verdict                                    | Verdict rules are mechanical (blockers → request changes); every finding needs a concrete case                               |

---

## 8. Cross-Model Review Notes

**Reviewer notes:**

Implementation notes (2026-11-09): `plugin.json` carries no `version` (the pack's recorded version is `bundled`, and a fixed string would pin Claude's plugin cache). The self-review below was done by the author, not a fresh reviewer.
