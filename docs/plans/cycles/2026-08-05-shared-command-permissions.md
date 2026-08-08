# Cycle: Shared commands declare restrictions that nothing enforces

**Date:** 2026-08-05
**Estimated Duration:** ~3 hours
**Status:** Done

---

## 1. Domain Context

Files in `configs/shared/commands/` declare restriction blocks that do nothing. Seven
declare `permission:`; the eighth (`smart-commit.md`) declares `tools:` instead. Both
keys are ignored.

The authoritative source is OpenCode's own config schema
(<https://opencode.ai/config.json>), whose command object allows exactly **`template`
(required), `description`, `agent`, `model`, `variant`, `subtask`** and sets
`"additionalProperties": false`. `permission`, `tools`, and `temperature` are all
outside it. A command's permissions are the permissions of **the agent it runs
under**; the only way a command influences them is an `agent:` field naming an agent
whose own (enforced) block applies.

Confirmed against the installed binary on 2026-08-05 — the first three probes are
recorded in [2026-08-05-review-loop.md](2026-08-05-review-loop.md) Step 0, the fourth
was run for this cycle:

| Probe                                             | Result                                                                    |
| ------------------------------------------------- | ------------------------------------------------------------------------- |
| Agent with `permission.bash {"*": deny}`          | **Enforced** — bash absent from the tool list (`unavailable tool 'bash'`) |
| Command with `permission.bash {"*": deny}`        | **Ignored** — an unlisted `echo` ran                                      |
| Command with `agent: <deny-agent>` (no own perms) | **Enforced** — bash unavailable; model fell back to `webfetch`            |
| Command with `tools: {bash: false}`               | **Ignored** — bash ran and echoed its marker                              |

`temperature:` is schema-invalid too and appears in all eight files, but it is not
probed here: unlike `permission`/`tools` it cannot imply a false safety guarantee, so
it is cleaned up for correctness rather than for risk. Being schema-invalid, it is
almost certainly also ignored — the doc does not claim more than that.

### Why this matters

The blocks are not merely inert — they read as guarantees the repo then relies on:

- **`review-pr.md` declares `edit: deny`.** It is not read-only. It runs under the
  host's default agent, where `opencode.json` allows edits. Anyone reasoning about
  "the reviewer commands can't write" from this file is reasoning from fiction.
- **`address-feedback.md` declares a `bash` catch-all deny plus six allowed tool
  families.** In practice it has the default agent's full bash access.
- **`smart-commit.md` declares `tools: {write: false, edit: false, bash: {…}}`.** It
  can write and edit. Its block is also internally malformed: `write`/`edit` use the
  agent `tools:` boolean shape while `bash` uses the `permission:` pattern-map shape,
  so it could not have been valid under either schema — a sign these blocks were
  written from memory and never verified against a running binary.
- **CLAUDE.md's accepted-difference note is half wrong.** It states "Agent and command
  frontmatter is enforced by OpenCode only. The shared `.md` files use OpenCode's
  `permission:` schema; Claude Code ignores it." True for agents. For commands,
  _neither_ agent enforces it — Claude Code uses its own `allowed-tools` key, so these
  commands are unrestricted on both sides while the docs claim one side holds.
- **No guard test covers this at all.** The existing suite checks the global config
  templates and the agent files; nothing reads command frontmatter, so nothing has
  ever noticed these keys are invalid. That absence is the gap this cycle closes.

None of this is a live security hole on its own — the global `opencode.json` policy
still applies its deny/ask list to every session, and that _is_ enforced. The defect
is that per-command restriction is believed to exist and does not, so any future
decision that leans on it ("this command is safe, it's edit-denied") is unsound.

## 2. Engineer Context

- `configs/shared/commands/*.md` — eight files: seven with a dead `permission:` block,
  one (`smart-commit.md`) with a dead `tools:` block; all eight also carry a
  schema-invalid `temperature:`
- `configs/opencode/opencode.json.tmpl` — the global policy that **is** enforced
- `configs/shared/agents/*.md` — agent frontmatter, which **is** enforced (leave alone)
- `internal/apps/opencode/permissions_test.go` — every existing test here reads either
  the rendered global templates (`permissionBlocks`, `claudePermissions`) or the agent
  files. **None reads command frontmatter**, so none needs changing; this cycle only
  adds a new test
- `CLAUDE.md` § "Keeping the two AI agents in sync" — the accepted-differences table
  and its command-frontmatter claim

Test commands:

```bash
go build ./...
go test ./internal/apps/opencode/
go test ./...
make lint
```

## 3. Objective

No shared command file declares a frontmatter key that nothing enforces, and the
repo's docs and a guard test state the real rule: **command permissions come from the
agent a command runs under.**

## 4. Scope Boundary

### In Scope

- [x] Apply the Step 1 disposition table to all eight `configs/shared/commands/*.md`
      (decisions already made — see the table; nothing is deferred to implementation)
- [x] Guard test: no command file declares a key outside OpenCode's command schema
      (`template`, `description`, `agent`, `model`, `variant`, `subtask`) — an
      allowlist, so a future invalid key of any name is caught, not just today's three
- [x] Correct CLAUDE.md's accepted-differences entry: agent frontmatter enforced by
      OpenCode, command frontmatter enforced by neither; `agent:` is the real lever
- [x] Re-verify with the real binary after the change (one command, one probe)

### Explicitly Out of Scope

- **Agent frontmatter.** Enforced and correct; `TestSharedAgentsInheritGlobalBashPolicy`
  stays exactly as it is.
- **The global `opencode.json` policy.** Enforced and correct.
- **Claude Code's `allowed-tools` schema.** Adding real per-command restriction on the
  Claude side is a separate decision; this cycle only stops the repo from claiming a
  restriction it doesn't have.
- **`/review-loop`.** Its new file is written without a permission block from the
  start — see the sibling cycle.

**Scope is locked.**

## 5. Implementation Plan

### Step 1: Disposition table — DECIDED

Every declared restriction below is already inert, so **dropping a block never changes
behavior**. The only choice with consequences is whether to add `agent:`, which would
_newly_ restrict a command that is unrestricted today.

| File                  | Declares                                              | Disposition                     | Reason                                                                                                                                          |
| --------------------- | ----------------------------------------------------- | ------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `explain-simply.md`   | `permission` (write/edit deny, bash `*` deny)         | **Drop**                        | Pure prose rewriting; needs no tools. Nothing to enforce, nothing lost.                                                                         |
| `human.md`            | `permission` (write/edit deny, bash `*` deny)         | **Drop**                        | Same — text rewriting only.                                                                                                                     |
| `teach.md`            | `permission` (+ `gh issue/pr view*` allows)           | **Drop**                        | Read-only intent, but the allows are a convenience list, not a safety boundary; global policy already governs `gh`.                             |
| `approve-pr.md`       | `permission` (write/edit deny, `devgeta task *` only) | **Drop**                        | Restriction is aspirational; approving a PR is a judgment step whose safety comes from the human, not a tool gate.                              |
| `create-pr.md`        | `permission` (write allow, edit deny, git allows)     | **Drop**                        | The allowlist is a hint about what it uses, not a boundary; it already has full default-agent access.                                           |
| `smart-commit.md`     | `tools` (write/edit false, bash pattern map)          | **Drop**                        | Malformed under both schemas (see §1); never enforced under any reading.                                                                        |
| `address-feedback.md` | `permission` (bash `*` deny + six tool families)      | **Drop**                        | The six hardcoded families fail §3.8 generality anyway; the command must run arbitrary repos' build/test commands, so a boundary here is wrong. |
| `review-pr.md`        | `permission` (**`edit: deny`**)                       | **Drop — and do NOT add agent** | See below. The one genuine intent, deliberately not converted.                                                                                  |

Plus: **drop `temperature:` from all eight** (schema-invalid, see §1).

**Note on `teach.md`:** During Step 3, the `description:` value was also changed from unquoted to double-quoted because the original was invalid YAML — an unquoted plain scalar with a mid-value `: ` that YAML's grammar treats as an ambiguous nested mapping. This was a pre-existing bug fix discovered because the new guard test could not parse the file, not a planned disposition change, but necessary for correctness.

**`review-pr.md` — the one real decision.** Its `edit: deny` expresses a genuine and
defensible intent: a review command that cannot rewrite the code it is reviewing.
Converting it to `agent: <read-only-agent>` is the only mechanism that would make that
true. Not doing it, for three reasons:

1. **It changes behavior, and this cycle's premise is that it does not.** Today
   `/review-pr` can edit. Adding an agent would newly break any use where it is asked
   to fix something inline — an unrelated regression smuggled into a cleanup.
2. **The agent would need the command's full instructions to still work.** A command's
   `agent:` swaps the whole system prompt and tool posture, not just permissions;
   `review-pr.md` is 198 lines that assume the default agent's capabilities. Verifying
   that the three existing reviewer agents' instructions compose correctly with it is
   its own piece of work.
3. **Restricting `/review-pr` is a policy decision, not a cleanup.** It deserves its
   own proposal where the behavior change is the headline, not a footnote.

Recorded as follow-up: _should `/review-pr` be genuinely read-only, via a dedicated
agent?_ Out of scope here.

### Step 2: Apply dispositions

Edit the eight files per the table. No behavior changes — every dropped key was
already inert, which is the point.

Verify: `go build ./...`; `go test ./internal/apps/opencode/`; diff review confirms
only frontmatter keys were removed and no command body changed.

### Step 3: Guard test (new — nothing existing changes)

Add one test asserting every `configs/shared/commands/*.md` frontmatter key is in
OpenCode's command schema allowlist (`template`, `description`, `agent`, `model`,
`variant`, `subtask`). An allowlist rather than a `permission`/`tools` denylist, so a
future invalid key of any name fails too — `temperature` is proof that a denylist
aimed at today's known-bad keys would have missed one.

The failure message must state the schema and that OpenCode drops unknown keys, so the
next contributor understands the key is not merely unused but _misleading_.

**No existing test is modified.** `TestClaudeAndOpenCodePermissionParity` compares the
two rendered global config templates and never reads command files; narrowing it would
weaken real global-policy coverage while removing nothing. Same for
`TestSharedAgentsInheritGlobalBashPolicy` (agents only, correct as-is).

Verify: `go test ./internal/apps/opencode/ -v`; confirm the new test fails when a
`permission:` block is reintroduced to any command file.

### Step 4: CLAUDE.md correction

Replace the half-wrong accepted-difference bullet with: agent frontmatter is enforced
by OpenCode (ignored by Claude Code); **command** frontmatter permissions are ignored
by both, and a command's permissions come from the agent it runs under, selectable via
`agent:`.

Verify: read the section back; it should let a newcomer predict all four probe results.

### Step 5: Re-verify against the binary

Deploy (`dg configure opencode --force`) and re-run one probe: a command with an
`agent:` field pointing at a restricted agent is restricted; a command without one
runs under the default agent.

Verify: probe output matches the table in §1.

**Probe result:** Built and installed this worktree's binary (`make build` + `sh install.sh --local ./devgeta`), ran `dg configure opencode --force`, then probed with two temporary command files (later deleted, never committed): one with `agent: code-reviewer` (edit tool reported unavailable, no file written) and one without an `agent:` field (edit tool worked normally, file written). Matches the plan's expected outcome exactly.

## 6. Verification Plan

### Automated

```bash
go build ./...
go test ./internal/apps/opencode/
go test ./...
make lint
```

### Manual

1. `dg configure opencode --force`, then run a shared command → behaves as before
   (dropping dead config changes nothing)
2. Reintroduce a `permission:` block to any command file → new guard test fails
3. Reintroduce `tools:` — and separately an invented key (`foo:`) → guard test fails
   for both, proving it is an allowlist and not a denylist of today's known-bad keys
4. `/review-pr` still edits when asked (unchanged — no `agent:` was added, per Step 1)

### Regression

- Agent-side guards untouched and green
- Global `opencode.json` policy unchanged
- Both agents redeployed: `dg configure claude --force`, `dg configure opencode --force`

## 7. Risks & Trade-offs

| Risk                                                          | Likelihood | Mitigation                                                                                                                                             |
| ------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Dropping blocks reads as "removing security"                  | High       | It removes fiction, not enforcement; §1's four-row probe table is the evidence, kept in the doc                                                        |
| `/review-pr` stays able to edit                               | Med        | Real, and deliberate — Step 1 records why converting it is a behavior change deserving its own proposal, not a cleanup footnote                        |
| OpenCode later adds command-level `permission` support        | Low        | Guard test's failure message names the schema and its source URL, so a future contributor re-checks rather than assuming                               |
| Guard test's allowlist goes stale when the schema gains a key | Med        | Failure message cites <https://opencode.ai/config.json> as the authority; adding a newly-valid key is a one-line test edit with the schema as evidence |

### Trade-offs Made

- **Delete rather than migrate to agents, in every case including `review-pr.md`.**
  Keeps this cycle strictly behavior-preserving; the one genuine intent is recorded as
  a follow-up proposal instead of being smuggled in as cleanup.
- **Allowlist guard, not a denylist.** `temperature` proves a denylist written against
  today's known-bad keys misses things.
- **Docs corrected in the same cycle as the code.** CLAUDE.md's currency rule requires
  it, and the wrong sentence is what made the config look load-bearing.

## 7b. Follow-up: the removal had a side effect nobody predicted (2026-08-07)

Removing the blocks was correct — they enforced nothing — but the risk table above
missed what they were doing anyway. The bash list read `"devgeta task *": allow`, and
while OpenCode never acted on it, **the agent read it as durable authorization** to run
the posting commands. With nothing in its place, the base instinct to confirm
outward-facing actions took over: `/review-pr` and `/approve-pr` started coming back with
a drafted verdict and "want me to post this?" instead of posting it themselves.

Fix: state the authorization in prose, which is the only carrier left. Every shared
command that invokes an outward `devgeta task` verb (`submit-review`, `approve-pr`,
`comment-pr`, `create-pr`, `reply-thread`, `resolve-thread`, `request-review`) now
carries an **"## Authority to post"** section saying that running the command _is_ the
authorization and that the agent must not ask first. `smart-commit` and `review-loop`
got the same statement scaled to what they do locally — with `review-loop`'s human-only
ratification carve-out left intact.

`TestPostingCommandsDeclareStandingAuthorization` guards it, deriving the covered files
from the files themselves, so a new posting command fails the build until it declares
the authorization too. Two siblings cover the commands that act without asking but never
post, which that derivation cannot reach:
`TestCommittingCommandsDeclareStandingAuthorization` (derived the same way, from a `git
commit` or `git push` in the file's prose — `smart-commit`, and `create-pr`'s push) and
`TestReviewLoopRunsUnattendedWithoutAsking` (per-file, since an unattended loop has no
verb to derive from; it also pins the human-only ratification carve-out beside the grant,
because the two are in tension).

All three are substring checks over prose, and each says so in its own "What this does
NOT catch" comment: none of them can tell correct wording from wording that reverses the
meaning, and none can prove an agent obeys. That is a limit of the repo, not of these
tests — a fresh-agent evaluation would have to run a live agent, and CLAUDE.md §6 forbids
tests that execute real commands (the reviewer runs in
`internal/tooling/task/reviewrun_test.go` script `opencode run` through a fixture for
exactly this reason). Behavioral confidence here comes from using the commands, not from
`go test`.

The general lesson for this cycle: dead config is not inert. Before deleting a block
that nothing enforces, ask what the _model_ reads it as, not only what the _runtime_
does with it.

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope locked?
- [ ] Steps actionable?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**

Approved 2026-08-06. Disposition table and guard-test plan verified against the
current repo state before approval: all 8 command files still carry the dead
keys as described, and `permissions_test.go` has no existing test that reads
command frontmatter. Implementation complete: all 4 steps shipped and reviewed clean. The per-task review loop caught and fixed a markdown-formatter body-mangling bug in Step 2, an unverified overclaim added to Step 4's CLAUDE.md fix, and a stale ADR-0015 claim and test-code duplication in the final whole-branch review. Real-binary probe in Step 5 confirmed the `agent:` mechanism works exactly as designed: a command with `agent: code-reviewer` had its edit tool genuinely unavailable; a command without an `agent:` field edited normally.
