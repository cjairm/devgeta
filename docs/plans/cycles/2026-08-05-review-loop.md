# Cycle: Review loop — one command that runs reviewers, fixes, and re-reviews until verdict

**Date:** 2026-08-05
**Estimated Duration:** ~8 hours
**Status:** Draft

---

## 1. Domain Context

Today a full review round trip is manual: launch a reviewer agent in OpenCode (the `R`
keybinding in `dg ws`, or by hand), read its report, carry the findings to the main
coding agent, verify each one (the `receiving-code-review` skill), fix or push back,
then re-launch the reviewer — repeated until approval. The review journal
([ADR-0012](../../decisions/ADR-0012-review-knowledge-in-a-local-journal.md)) already
gives that loop memory (findings and answers persist per branch), but nothing drives
the loop itself. The human is the orchestrator.

This cycle builds the orchestration: **one command that runs the configured
reviewer(s), collects verdicts, hands blocking findings to the coding agent for
verified fixes, and repeats — bounded — until approval or escalation.**

Two design constraints come from published multi-agent-review research, discussed and
agreed before this doc:

- **Never loop toward consensus.** Models conform to each other; unanimous agreement is
  not evidence of correctness. Rounds are bounded (default 3, max 5) and persistent
  disagreement is **escalated to the human**, never voted away.
- **Cross-model review is a flag, not architecture.** Different vendors have
  non-overlapping blind spots, so the reviewer list is configurable
  (`review.reviewers`: e.g. one GPT reviewer, or GPT + Gemini + Kimi). Each is the same
  reviewer agent run under a different model; they share the one journal.

And two from this repo's own review machinery:

- **Reviews are keyed by branch and diffed against the default branch**
  (`ADR-0012`, `reviewPrompt` in `internal/tooling/worktree/layout.go`). Running the
  loop _on_ the default branch is doubly broken: the diff is meaningless, and the
  journal for `main` would never be cleaned up (cleanup rides on branch deletion). The
  loop therefore refuses to run on the default branch.
- **Reviewer agents stay on OpenCode.** Their read-only `permission:` frontmatter is
  enforced by OpenCode and ignored by Claude Code (accepted difference, CLAUDE.md §
  "Keeping the two AI agents in sync").

## 2. Engineer Context

- **Relevant files and their purposes:**
  - `internal/tooling/task/reviewnotes.go` — journal read/write task commands (the family this joins)
  - `internal/tooling/reviewjournal/` — journal core (manager, encoding, staleness)
  - `internal/apps/opencode/` — the OpenCode app wrapper; per CLAUDE.md §6 every
    external-binary call goes through its wrapper, so the headless `opencode run`
    invocation must be added here, not hand-rolled in the task package
  - `cmd/task.go` — task subcommand registration (`review-scope`, `review-notes`, …)
  - `cmd/config_settings.go` — the `dg config` settings registry (`Settings` var);
    `worktree.search_paths` is the existing `stringlist` example to copy
  - `internal/config/fromFile.go` — `GlobalConfig`; needs a `ReviewConfig` section
  - `configs/shared/commands/` — shared agent commands; `address-feedback.md` is the
    closest model (permission frontmatter, journal settle flow)
  - `configs/shared/agents/code-reviewer.md` — verdict contract:
    `**Status:** APPROVE | REQUEST CHANGES | NEEDS DISCUSSION` (line 152)
  - `internal/apps/opencode/permissions_test.go` — agent/command sync guard tests

- **Key facts already verified:**
  - `opencode run [message]` exists with `--agent <name>`, `-m provider/model`,
    `--format json` (checked against the installed binary, 2026-08-05)
  - The settings registry supports `stringlist` kinds with validation in `Set`
  - All three reviewers journal every `[CRITICAL]`/`[IMPORTANT]` finding and end with
    the settle command (enforced by `TestReviewerAgentsReadTheJournalAndCanApprove`)

- **Testing:** [docs/guides/testing-patterns.md](../guides/testing-patterns.md) —
  `testutil.MockApp`, `VerifyNoRealCommands`, no real `opencode` in any test.

- **Test commands:**

  ```bash
  go test ./internal/tooling/task/ ./internal/apps/opencode/ ./cmd/
  go test ./...
  make lint
  ```

## 3. Objective

`/review-loop` run from the coding agent executes the whole review cycle unattended —
run configured reviewer(s) headless, verify and address blocking findings, settle the
journal, re-run — and ends in one of exactly two states:

1. **Clean approval** — every reviewer APPROVEs _and_ no agent-authored rejection is
   still awaiting ratification (a rejection the human has ratified is an ordinary
   human rejection, ADR-0012 semantics, and does not block).
2. **Report to the human** — everything else: persistent disagreement, the round cap,
   any reviewer process failure (`ERROR`/`NO VERDICT`), or approval that rests on one
   or more agent-authored rejections awaiting ratification. The report carries the
   verdict table per round, every agent rejection with its evidence and the exact
   ratify/reopen command to close it, and any failure by name.

A process failure or an unratified pushback can therefore never masquerade as state 1
— there is no third, undocumented outcome.

## 4. Scope Boundary

### In Scope

- [ ] ADR: loop contract — bounded rounds, any-single-blocker rule, escalation over
      consensus, sequential reviewers (write before implementation, §11)
- [ ] ADR: isolation — refuse on default branch, auto-branch not auto-worktree,
      worktree as opt-in (write before implementation, §11)
- [ ] `ReviewConfig` in `GlobalConfig` + `review.reviewers` (stringlist) and
      `review.rounds` (int, default 3, max 5) in the `dg config` registry
- [ ] OpenCode wrapper method for headless review runs (`opencode run --agent <a>
[-m <provider/model>] --format json`)
- [ ] `dg task review-run` — run each configured reviewer sequentially, parse each
      verdict, print compact per-reviewer verdicts + open journal ids (task-design
      output contract); refuses on the default branch with an actionable error
- [ ] `configs/shared/commands/review-loop.md` — the agent-side loop: subagent
      execution, per-finding verification (receiving-code-review discipline), journal
      settle, round cap, escalation report
- [ ] Journal ratification transitions — `review-note --ratify --id <id>` (human
      accepts an agent rejection: strips the `agent:` provenance, entry becomes an
      ordinary rejection) and `review-note --reopen --id <id>` (human refuses: the
      settled entry returns to open under the **same id** — no duplication — with its
      original finding text, the agent's rejection note dropped). Today neither move
      exists: `manager.go:186` refuses to settle a settled entry, and nothing reopens
      one — without these, an agent rejection blocks clean approval forever
- [ ] Guard-test updates (`permissions_test.go` family) for the new shared command
- [ ] Docs: `docs/spec.md`, command doc

### Explicitly Out of Scope

- **Parallel reviewers.** Ship sequential. Parallel later needs exactly two additions —
  a file lock on journal writes and a post-fan-out dedup pass — recorded here so the
  future flag has its checklist.
- **Cross-model debate** (reviewers critiquing each other's findings). The journal
  already carries rejections between rounds; explicit debate topologies are unproven
  value at real risk (consensus drift).
- **Auto-worktree.** The default-branch fix is a branch (`git switch -c` carries dirty
  files, no copy, no merge-back). A `--worktree` opt-in for "keep coding while it
  runs" is future work via `dg wt create`.
- **Seeding the journal from PR threads** (ADR-0012 "revisit when").
- **Fixing the existing shared commands' dead frontmatter keys.** Step 0 found seven
  dead `permission:` blocks, one dead `tools:` block, and eight schema-invalid
  `temperature:` keys, all ignored by OpenCode (`review-pr.md` believes it is
  `edit: deny` and is not); CLAUDE.md's "enforced by OpenCode only" note is half
  wrong — agents yes, commands no. Filed as
  [2026-08-05-shared-command-permissions.md](2026-08-05-shared-command-permissions.md).
  This cycle only ensures the file it adds does not extend the defect.
- **Validating model names offline** — `review.reviewers` entries are passed through;
  OpenCode errors at runtime if a provider isn't configured.

**Scope is locked.**

## 5. Implementation Plan

### File Changes

| Action | File Path                                           | Description                                            |
| ------ | --------------------------------------------------- | ------------------------------------------------------ |
| Create | `docs/decisions/ADR-0016-*.md`, `ADR-0017-*.md`     | Loop contract; isolation (names/numbers at write time) |
| Modify | `internal/config/fromFile.go`                       | `ReviewConfig{Reviewers []string; Rounds int}`         |
| Modify | `cmd/config_settings.go`                            | `review.reviewers`, `review.rounds` registry entries   |
| Modify | `internal/apps/opencode/opencode.go` (+test)        | Headless run method on the wrapper                     |
| Create | `internal/tooling/task/reviewrun.go` (+test)        | Fan-out, verdict parse, output contract                |
| Modify | `cmd/task.go`                                       | Register `review-run`; `--ratify`/`--reopen` flags     |
| Modify | `internal/tooling/reviewjournal/manager.go` (+test) | `Ratify` and `Reopen` transitions                      |
| Modify | `internal/tooling/task/reviewnotes.go` (+test)      | Wire the two transitions into `review-note`            |
| Create | `configs/shared/commands/review-loop.md`            | The loop command (both agents, per sync rule)          |
| Modify | `internal/apps/opencode/permissions_test.go`        | Extend guards to the new command                       |
| Modify | `docs/spec.md`, `docs/plans/cycles/` (this file)    | Document; check off steps                              |

### Step-by-Step

#### Step 0: Probes — DONE (2026-08-05, real binary, `opencode/big-pickle`)

- [x] **Headless run completes with no interactive prompt.**
      `opencode run --agent code-reviewer --format json "<prompt>"` returns cleanly.
      No redesign around `opencode serve` needed.
- [x] **`--format json` is parseable; the AVX/Bun warning goes to stderr only.**
      stdout is newline-delimited JSON events; the final assistant text arrives as a
      `{"type":"text",...,"part":{"text":"…"}}` event, so the `**Status:** …` line is
      recoverable. Redirecting stderr is unnecessary but harmless.
- [x] **An unusable model fails fast, but with a generic message.** Exit 1 and a
      parseable `{"type":"error","error":{"name":"UnknownError","data":{"message":
"Unexpected server error…"}}}` event — no "provider not configured" text. So
      `ERROR(<reason>)` will often carry a generic string; the outcome is still
      unambiguous (the event type is), only the reason is vague. Step 4 must key off
      the error **event**, never off message-text matching.
- [x] **Command-file `permission:` blocks are ignored by OpenCode. Agent ones are
      enforced.** This contradicted the original Step 5 design and is why it was
      rewritten — see below. Evidence:

  | Probe                                             | Result                                                                    |
  | ------------------------------------------------- | ------------------------------------------------------------------------- |
  | Agent with `permission.bash {"*": deny}`          | **Enforced** — bash absent from the tool list (`unavailable tool 'bash'`) |
  | Command with `permission.bash {"*": deny}`        | **Ignored** — an unlisted `echo` ran                                      |
  | Command with `agent: <deny-agent>` (no own perms) | **Enforced** — bash unavailable; model fell back to `webfetch`            |

  Cause: OpenCode's command schema (<https://opencode.ai/config.json>) allows only
  `template`, `description`, `agent`, `model`, `variant`, `subtask`, with
  `additionalProperties: false` — `permission` is not in it. **A command's permissions
  are the permissions of the agent it runs under**, and an `agent:` field is the only
  way a command influences them.

  This is a live defect in the existing `configs/shared/commands/*.md`: seven declare
  a dead `permission:` block, `smart-commit.md` declares a dead `tools:` block, and
  all eight carry a schema-invalid `temperature:` (`review-pr.md` believes it is
  `edit: deny` and is not). Out of scope here — filed as its own cycle,
  [2026-08-05-shared-command-permissions.md](2026-08-05-shared-command-permissions.md).

#### Step 1: ADRs

Write both ADRs (scope list above), get approval. Decisions already made in
discussion, to be recorded not re-litigated: bounded rounds (3/5) · any single
REQUEST CHANGES blocks; NEEDS DISCUSSION is treated as blocking and escalates if it
survives a round · disagreement at cap → human, never a vote · sequential reviewers
(journal has no lock; sequence gives reviewer N sight of N−1's entries) · refuse on
default branch, tell the user the `git switch -c` fix · Go does fan-out/parse/journal,
the agent does judgment · **agent rejections are provisional** — provenance-marked
(`agent:` note prefix) and always surfaced in the terminal report for human
ratification, because ADR-0012's settled-means-settled rule leaves the reviewer no
way to sustain a disagreement once rejected. The alternative (reserving `rejected`
for humans, leaving disputed entries open) was considered and dropped: an open entry
carries no channel for the agent's counter-evidence, so every false-positive finding
would re-raise blind and escalate, defeating the automation. Ratification completes
the state machine: `--ratify` (accept → ordinary rejection) and `--reopen` (refuse →
open again, same id) are the only two exits from provisional, both human-only.

Verify: ADRs merged into `docs/decisions/README.md` index.

#### Step 2: Config

- `ReviewConfig` on `GlobalConfig` (omitempty, like `WorktreeConfig`)
- Registry entries: `review.reviewers` (stringlist; entries must look like
  `provider/model` — one `/` minimum, no other offline validation), `review.rounds`
  (int; 1–5, default 3)
- Tests mirror the existing registry tests (`config_settings_registry_test.go`)

Verify: `go test ./cmd/ ./internal/config/`; `dg config set/get/unset review.reviewers`.

#### Step 3: OpenCode wrapper run method

Extend `internal/apps/opencode` with the headless-run capability (agent, optional
model, prompt, working dir, generous timeout). Per §6, this is the only place the
binary is assembled. Mocked tests both ways (with/without model).

Verify: `go test ./internal/apps/opencode/`.

#### Step 4: `dg task review-run`

- Resolve reviewer list: `review.reviewers`, or one entry "OpenCode default model"
  when unset; `--reviewer` picks the agent (default `code`), validated against the
  existing registry (`worktree.BuiltinReviewerChoices` — `internal/tooling/task`
  already imports that package), never a restated list
- Refuse on the default branch (reuse the existing default-branch resolution the
  review-scope family uses) with the `git switch -c` suggestion in the error
- Run reviewers **sequentially** through the wrapper. Each reviewer ends in exactly
  one of five outcomes — the three verdicts parsed from the last `**Status:**` line
  (`APPROVE`, `REQUEST CHANGES`, `NEEDS DISCUSSION`), plus:
  - `NO VERDICT` — the run completed but no status line parsed
  - `ERROR(<reason>)` — the run itself failed: spawn failure, nonzero exit, timeout,
    unparseable JSON, unconfigured provider or unavailable model. Detected from the
    `{"type":"error"}` event and/or nonzero exit — **never** by matching error message
    text, which Step 0 found to be generic (`"Unexpected server error"`) even for a
    bad model. The reason is OpenCode's own text, truncated, never guessed at.

  `NO VERDICT` and `ERROR` never count as approval and are never retried by the
  command; the loop maps them to the report-to-human terminal state. A reviewer
  failure also does not abort the remaining reviewers — each runs and reports.
  Tests enumerate every outcome path.

- Output (task-design contract): one line per reviewer (`model → outcome`), then open
  journal ids. Nothing else. The three shapes:

  ```
  openai/gpt-5.2 → APPROVE
  google/gemini-3-pro → APPROVE
  open: none
  ```

  ```
  openai/gpt-5.2 → REQUEST CHANGES
  google/gemini-3-pro → APPROVE
  open: n4 n7
  ```

  ```
  openai/gpt-5.2 → ERROR(Unexpected server error. Check server logs…)
  google/gemini-3-pro → NO VERDICT
  open: none
  ```

Verify: `go test ./internal/tooling/task/ ./cmd/` — all mocked;
`VerifyNoRealCommands` on every test.

#### Step 4b: Journal ratification transitions

Two additions to `reviewjournal.Manager`, wired into `review-note`:

- `--ratify --id <id>` — valid only on a settled-rejected entry whose note carries
  the `agent:` provenance prefix; strips the prefix in place. Anything else (open,
  fixed/answered, already-ratified) fails with the state echoed back.
- `--reopen --id <id>` — valid on any settled entry; moves it back to open under the
  same id with its original finding text, dropping the resolution note. The next
  round re-raises it like any open entry (ADR-0012: an open entry is re-raised,
  never duplicated).

Both are human decisions. The permission model cannot distinguish who typed a
`devgeta task` command, so this is enforced the way the reviewers' settle step is:
the `/review-loop` command's own instructions never invoke either flag, and the
terminal report prints them for the human — see the risk table.

Verify: `go test ./internal/tooling/reviewjournal/ ./internal/tooling/task/ ./cmd/` —
covering: ratify on agent-rejected → ordinary rejection; ratify on anything else →
error; reopen → same id open, count unchanged; reopen of nonexistent/open id → error.

#### Step 5: `/review-loop` command file

`configs/shared/commands/review-loop.md`, modeled on `address-feedback.md` for shape
— but it **declares no `permission:` block at all**, because Step 0 proved OpenCode
ignores that key in command files (it is not in the command frontmatter schema). Any
permission block here would be decoration that reads as a guarantee, which is exactly
the defect the sibling cycle exists to remove; writing another one would be shipping
the bug knowingly.

The loop therefore runs under the **host's default agent** and its `opencode.json`
policy (`bash "*": allow`, minus the global deny/ask list). That is the correct
posture on the merits, not merely what's left over: verifying a fix means running
whatever build and test commands the target repo uses, which no fixed allowlist can
enumerate (`address-feedback.md`'s six hardcoded families —
`go/npm/npx/pnpm/yarn/make` — fail §3.8 generality for precisely this reason, and
don't work anyway).

A dedicated restricted agent (`agent: review-fixer`) is the one mechanism that
_would_ constrain this command, since Step 0 proved a command's `agent:` field does
carry that agent's enforced permissions. Deliberately not used in v1: the fixer needs
broad bash to verify, so the only honest restricted agent would be one that still
allows arbitrary build/test commands — the same posture with more moving parts.
Recorded here so the option is a decision, not an oversight.

**Guard test** (replacing the one the old design proposed, which would have asserted
dead config): assert `review-loop.md` declares only keys in OpenCode's command schema
(`template`, `description`, `agent`, `model`, `variant`, `subtask`) — which excludes
`permission`, `tools`, and `temperature` alike — with the failure
message stating why — OpenCode ignores it and its presence implies a guarantee that
does not exist. That test belongs to the sibling cycle's repo-wide sweep; this cycle
adds the new file in the shape that sweep will require.

The flow it encodes:

1. `devgeta task review-run` (round 1)
2. All APPROVE and no agent-authored rejections → clean approval, stop
3. Otherwise, per open finding: verify with receiving-code-review rigor — implement
   real ones; for wrong ones, `review-note --settle --as rejected` with the
   disproving evidence, the note prefixed `agent:` so provenance is never ambiguous.
   An agent rejection is **provisional**: ADR-0012 makes the reviewer treat a fresh
   rejected entry as settled, so the reviewer cannot re-dispute it — which is why
   every agent rejection is carried into the terminal report for the human to ratify,
   and why its existence downgrades "all APPROVE" from clean approval to a report.
   The loop never uses `rejected` to make a disagreement disappear; it uses it to
   stop the re-raise circle while keeping the dispute visible at the end. The report
   closes each pushback with the two commands the human picks from, ids filled in —
   `review-note --ratify --id <id>` (accept: becomes an ordinary rejection, stops
   blocking clean approval) or `review-note --reopen --id <id>` (refuse: the finding
   returns to open, same id, and the next run re-raises it)
4. Any reviewer outcome of `ERROR` or `NO VERDICT` → stop after this round and
   report, naming the failure; no auto-retry in v1
5. Re-run; stop at `review.rounds` (cap 5) → report: verdict table per reviewer per
   round, every agent rejection with evidence, + `dg task review-notes` output
6. Run the fix work in a subagent where the host agent supports it, so the main
   session keeps only the outcome

Verify: `go test ./internal/apps/opencode/` green — the new command carries no
`permission:` key, and the Claude/OpenCode parity guards still pass (parity is
unaffected: the block is absent on both sides rather than asymmetric).

#### Step 6: Manual end-to-end

On a real feature branch with a seeded flaw: run `/review-loop` with one default
reviewer; then with two configured models. Confirm: refusal on main, verdict parsing,
journal settle between rounds, escalation path (force it with `review.rounds 1`).

#### Step 7: Docs + close out

`docs/spec.md` feature entry, `dg config` docs mention the new keys, check off this
doc, status → Done. Deploy: `dg configure claude --force` and
`dg configure opencode --force`.

## 6. Verification Plan

### Automated

```bash
go build ./...
go test ./internal/tooling/task/ ./internal/apps/opencode/ ./cmd/ ./internal/config/
go test ./...
make lint
```

### Manual

1. `dg task review-run` on `main` → refuses, names the branch fix
2. `dg config set review.reviewers openai/gpt-5.2` → `review-run` uses it; unset →
   default model
3. `/review-loop` on a branch with a planted bug → finding journaled, fixed, settled,
   round 2 approves
4. `dg config set review.rounds 1` + a disputed finding → escalation report, no
   further rounds
5. Journal after approval: entries settled, file still present (cleanup stays on
   branch teardown — nothing in this cycle deletes journals)
6. `dg config set review.reviewers bogus/model` → `review-run` reports
   `ERROR(<reason>)` for it and the loop ends in the report state, not approval
7. A finding the agent rejects → the run ends in the report state with the
   `agent:`-prefixed rejection listed for ratification, even when all reviewers
   approve the re-run
8. Ratify that rejection (`review-note --ratify --id <id>`) → re-run `/review-loop`
   → clean approval (the ratified rejection no longer blocks)
9. Instead reopen it (`review-note --reopen --id <id>`) → the next round re-raises
   the same id as an open finding; `review-notes` shows one entry, not two

### Regression

- `dg ws` `R` keybinding review flow unchanged
- `review-notes` / `review-note` behavior unchanged
- Agent config sync tests green (`go test ./internal/apps/opencode/`)

## 7. Risks & Trade-offs

| Risk                                                  | Likelihood | Mitigation                                                                                                                                                                                                                                                                                |
| ----------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `opencode run` blocks on permissions headless         | Med        | Step 0 probe is a hard gate; redesign before code if it fails                                                                                                                                                                                                                             |
| Verdict line missing/malformed in a reviewer's output | Med        | Explicit `NO VERDICT` outcome, surfaced not guessed; reviewer templates already carry the line                                                                                                                                                                                            |
| Two models both wrong, both approve                   | Low–Med    | Inherent to AI review; bounded rounds + human owns the merge decision; loop never self-merges                                                                                                                                                                                             |
| Long wall-clock per round (full review × N models)    | High       | Sequential is a deliberate trade; subagent execution keeps the human unblocked; parallel later                                                                                                                                                                                            |
| Loop fixes drift from what the user wanted            | Med        | receiving-code-review verification per finding; escalation report shows every fix and rejection                                                                                                                                                                                           |
| Model/provider strings go stale in config             | Low        | Pass-through by design; surfaces as an `ERROR(<reason>)` outcome in the report, never a silent skip                                                                                                                                                                                       |
| Loop calls `--ratify`/`--reopen` itself               | Low–Med    | Cannot be blocked structurally (permissions can't tell who typed a task command); the command's instructions forbid it and a guard test asserts the command file never invokes either flag outside the report template. Accepted as prose-level, same trust as the reviewers' settle step |

### Trade-offs Made

- **Branch, not worktree, as the on-main fix** — carries dirty files for free, no
  merge-back machinery; loses "keep coding while it runs" (future `--worktree`).
- **Sequential, not parallel** — slower rounds; buys journal-write safety and
  cross-reviewer dedup for free.
- **Any single blocker blocks** — strictest consensus rule; more escalations, never a
  silently outvoted finding.
- **Judgment lives in the agent command, not Go** — the loop's fix step can't be
  unit-tested in Go; in exchange no model-judgment call is hardcoded where it can't
  reason.

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope locked?
- [ ] Steps actionable?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**

(fill in during review)
