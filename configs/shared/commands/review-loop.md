---
description: Use when a branch is ready for an unattended review cycle before merge — repeats devgeta task review-run, verifying and settling each finding, until a clean approval or a report to the human
---

Run the review loop: call `devgeta task review-run` round after round, verify and settle
each finding it surfaces, and stop at one of exactly two outcomes — a clean approval, or a
report to the human. A process failure or an unresolved pushback is never presented as
approval.

## Usage

```
/review-loop [--reviewer code|document|skill]
```

`--reviewer` picks the reviewer agent for every round — the same choices `devgeta task
review-run` takes; default is `code`. The branch is whatever is currently checked out.
`--reviewer` is read from `$ARGUMENTS` once, at step 0 below, and forwarded to every
`review-run` call the loop makes — it is not just documentation here.

## What this drives

- `devgeta task review-run [--reviewer <key>]` — one round: every reviewer model
  configured in `review.reviewers` runs sequentially, headless, against the current
  branch. Prints one line per reviewer (`<model label> → <outcome>`), then the ids still
  open in the journal (`open: n4 n7`, or `open: none`). An outcome is `APPROVE`,
  `REQUEST CHANGES`, `NEEDS DISCUSSION`, `NO VERDICT`, or `ERROR(<reason>)`. It refuses to
  run on the default branch, a detached HEAD, or a branch with no commits ahead of the
  default branch (nothing committed to review yet) — if it refuses, surface that error
  as-is and stop; there is nothing to loop on.
- `devgeta task review-notes` — the branch's review journal: every open finding, and
  every settled one with its resolution and note.
- `devgeta task review-note --open --note "<text>" [--at <path[:line]>]` — record a
  finding. Reviewers do this themselves; this loop does not open new findings.
- `devgeta task review-note --settle --id <id> --as answered|rejected|fixed --note
"<text>"` — close an open finding with the outcome and the reasoning behind it.
- `devgeta config get review.rounds` — the round cap (default 3, maximum 5).
- `devgeta config get review.reviewers` — the configured reviewer models; empty means
  one run on OpenCode's own default model.

Two more `review-note` transitions exist for retiring an agent's provisional rejection.
Both are human-only, and this loop never runs either — see the terminal report below,
where their exact form is spelled out.

This file owns the round counter and the cap. `devgeta task review-run` only knows about
the one round it just ran.

## Flow

### 0. Resolve the reviewer selector

Parse `$ARGUMENTS` once, before the first round.

- Empty (no `--reviewer` at all): this is the default, not an error. Every round below
  calls `devgeta task review-run` with no `--reviewer` flag, and `review-run` falls back to
  its own default (`code`).
- `--reviewer <key>` with `<key>` one of `code`, `document`, `skill`: carry that exact value
  into every `devgeta task review-run` call this loop makes, round after round — the same
  key each time, never re-parsed per round.
- `--reviewer <key>` with anything else: stop before running a single round. Report that
  `--reviewer` only accepts `code`, `document`, or `skill`, naming the value that was
  actually passed. Do not guess which one was meant.

### 1. Run a round

Run `devgeta task review-run`, passing `--reviewer <key>` when step 0 resolved one — omit
the flag entirely otherwise. Read its output exactly as printed — one verdict line per
reviewer, then the open ids. Never guess at, soften, or invent a verdict the line does not
state.

### 2. A reviewer failure stops the loop here

If any reviewer's outcome this round is `ERROR(<reason>)` or `NO VERDICT`, stop. Do not
run another round, and do not attempt to fix anything based on a run that did not
complete. Go straight to the terminal report, and name the failing reviewer in it:

- `ERROR(<reason>)`: report the reason verbatim.
- `NO VERDICT`: there is no reason to report — the outcome is the bare string. State
  that the reviewer completed without producing a verdict. Do not invent a reason; the
  outcome carries none, and making one up would misreport what happened.

There is no retry in this version — a flaky run and a broken one look the same from
here, so both get reported rather than silently rerun.

### 3. Check for clean approval

If every reviewer's outcome this round is `APPROVE` **and** the round's `open:` line reads
`open: none`, read `devgeta task review-notes`.

Read the right line. Each settled entry in that output is a head line (id, resolution,
cite, freshness), then the reviewer's finding on an indented line with no label, then — when
the entry was settled with a note — an indented line labelled `answer:` carrying that note.
The `agent:` marker only ever appears on the `answer:` line, because that is the settle
note. It is never on the finding line, which is the reviewer's words, not the settler's.
Looking for it there finds nothing and turns an unratified pushback into a false approval:

```
branch: feat/retry-context
base: origin/main
last-review: 2026-08-06

settled:
- n2 fixed internal/tooling/task/reviewrun.go:73 [fresh]
  the (?m) flag makes statusLinePattern scan the whole report
  answer: dropped the flag; the scanner already feeds it one line at a time
- n3 rejected docs/spec.md:120 [fresh]
  the spec should name the flag too
  answer: agent: the spec documents behavior, not regex flags — nothing to change
```

There, `n2` is an ordinary settled entry and `n3` is an agent rejection: its `answer:` line
begins with `agent:`.

- If no settled entry's `answer:` line begins with `agent:`, this is a clean approval. Stop
  here and report it — do not run another round just because rounds remain.
- If any settled entry's `answer:` line begins with `agent:`, an agent-authored rejection is
  still waiting on a human decision. All-APPROVE with nothing open does **not** make this
  clean; go to the terminal report instead, carrying that entry into it.

Otherwise — any reviewer's outcome is not `APPROVE`, or `open:` names any ids even though
every outcome was `APPROVE` — this round is not a clean approval. Continue to step 4. An
id under `open:` is an unanswered finding regardless of what the verdicts say, and step 4
has not run yet at this point in the flow, so it must never be waved through because every
outcome happened to say `APPROVE`.

### 4. Otherwise, verify and settle each open finding

For every id under `open:`, read its text from `devgeta task review-notes` and verify it
with the rigor of the `receiving-code-review` skill — restate what it claims, check it
against the actual code, don't take it at face value just because a reviewer wrote it.

- **The finding is real:** implement the fix. Do this work in a subagent whenever the
  host agent you're running under supports launching one (see step 6). Then settle it:
  `devgeta task review-note --settle --id <id> --as fixed --note "<what changed and
where>"`.
- **The finding is wrong:** do not implement it, and do not leave it open — an open
  finding just comes back next round. Settle it rejected, with the evidence that
  disproves it, and mark it as an agent call:
  `devgeta task review-note --settle --id <id> --as rejected --note "agent: <the
disproving evidence>"`. The `agent:` prefix is mandatory and belongs at the very
  start of the `--note` value, so it lands at the start of the entry's `answer:` line —
  the one line step 3 reads. It is the only thing that tells a later reader — human or
  reviewer — that this rejection is provisional rather than final. Never settle a finding
  rejected without real evidence, and never omit the evidence to save time: the human's
  decision at the end rests on being able to check your reasoning, not just your
  conclusion.

Never use a rejection to make a disagreement disappear because re-litigating it is
inconvenient. It does not remove the finding from anyone's view — it turns into a
pushback the human sees, and can undo, in the terminal report.

### 5. Enforce the round cap

After this round's findings are handled, read `devgeta config get review.rounds`. If this
was that many rounds, stop — go to the terminal report regardless of what the branch's
state is at that point. Otherwise return to step 1 and run another round.

### 6. Run fix work in a subagent

Wherever the host agent supports launching one, do step 4's implementation and
verification (build, test, confirm the fix) inside a subagent rather than the main
session. The main session should only see the outcome of each round — verdicts, which
findings were settled and how, and the eventual report — not the diffs and test runs a
fix took getting there.

## Terminal report

Every run of this loop ends here — there is no third outcome, and this section is the
only place either human-only journal transition is written out.

**Clean approval** (step 3's clean case):

```
## Review loop — clean approval

Round <n> of <cap>. Every reviewer approved, the round's `open:` line read `open: none`,
and no settled entry's `answer:` line carries an `agent:` rejection awaiting ratification.
```

**Report to the human** — everything else, including a reviewer failure (step 2) and
hitting the round cap (step 5):

```
## Review loop — report

### Rounds
| Round | Reviewer | Verdict |
|-------|----------|---------|
| 1     | <label>  | <verdict> |
| ...   | ...      | ... |

### Agent rejections awaiting your decision
| id  | Finding | Why the agent rejected it | Accept | Refuse |
|-----|---------|----------------------------|--------|--------|
| n7  | <the reviewer's finding>  | <the disproving evidence> | `devgeta task review-note --ratify --id n7` | `devgeta task review-note --reopen --id n7` |

(Omit this table when there are no agent rejections outstanding.)

### Failures
<name the failing reviewer. For ERROR(<reason>), give the reason verbatim. For NO
VERDICT, state that the reviewer completed without producing a verdict — do not invent
a reason, since NO VERDICT carries none. Omit this section if none>

### Journal
<the full, verbatim output of `devgeta task review-notes`>
```

`--ratify --id <id>` accepts the agent's rejection: it strips the `agent:` marker,
leaving an ordinary human rejection that no longer blocks clean approval. `--reopen --id
<id>` refuses it: the finding returns to open under the same id, and the next round
raises it again. Print both commands with the real id filled in — never run either one
yourself. Ratification is the human's decision to make, not this loop's: the permission
model has no way to tell who typed the command, so the only thing standing between "the
loop reports a rejection" and "the loop quietly approves its own rejection" is this
instruction. Follow it exactly.

## Notes

- One call to `devgeta task review-run` is one round; this file is what turns that into a
  loop with a stopping point.
- Never invent a reviewer's verdict, and never present a run that failed, or a run that
  hit the round cap, as one that passed.
- If `devgeta task review-run` itself refuses to run (default branch, detached HEAD, or no
  commits ahead of the default branch), surface that error as-is and stop before running
  anything else.
