---
description: Use when a branch is ready for an unattended review cycle before merge — repeats devgeta task review-run, verifying and settling each finding, until a clean approval or a report to the human
---

Run the review loop: call `devgeta task review-run` round after round, verify and settle
each finding it surfaces, and stop at one of exactly two outcomes — a clean approval, or a
report to the human. A process failure or an unresolved pushback is never presented as
approval.

## Usage

```
/review-loop [--reviewer code|document|skill] [--note <text>]
```

`--reviewer` picks the reviewer agent for every round — the same choices `devgeta task
review-run` takes; default is `code`. The branch is whatever is currently checked out.
`--note` passes the human's own words to every reviewer of every round (e.g. `--note
"focus on docs/spec.md, I only changed the wording there"`). Both are read from
`$ARGUMENTS` once, at step 0 below, and forwarded to every `review-run` call the loop
makes — they are not just documentation here.

## What this drives

- `devgeta task review-run [--reviewer <key>] [--note <text>]` — one round: every
  reviewer model configured in `review.reviewers` runs sequentially, headless, against
  the current branch. Prints exactly one line per reviewer (`<model label> → <outcome>`)
  and nothing else — no findings, no ids. An outcome is `APPROVE`, `REQUEST CHANGES`,
  `NEEDS DISCUSSION`, `NO VERDICT`, or `ERROR(<reason>)`. Progress goes to stderr while it
  runs (a line per reviewer, plus a heartbeat at most every 30s), so a long round reads as
  working rather than stuck; none of that is part of the verdict — read the verdict lines
  only. Never pass `--verbose`: it replaces the heartbeat with one line per tool call,
  which is hundreds of lines a round for a log this loop does not read. The branch under
  review is everything it would
  merge, commits AND uncommitted work, so work in progress does not need committing
  first. It refuses to run on the default branch, a detached HEAD, or a branch that
  changes nothing at all (no commits ahead AND a clean working tree) — if it refuses,
  surface that error as-is and stop; there is nothing to loop on.
- `devgeta task review-notes` — the branch's review journal: every open finding under
  `open:`, and every settled one with its resolution and note. **This is the only place
  open findings are listed**, so the loop reads it after every round to learn what is
  still open — `review-run` does not repeat it.
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

## Phases

This loop runs three phases, always in this order:

1. **Opening round.** Every reviewer configured in `review.reviewers` runs. Nothing has
   been narrowed yet, so this round establishes who is actually blocking.
2. **Narrowing rounds.** Only the reviewers that did not approve in the previous round
   run. A reviewer whose outcome was `APPROVE` drops out of this phase's set; any other
   outcome (`REQUEST CHANGES`, `NEEDS DISCUSSION`, an error, or no verdict at all) keeps
   it in. Findings are triaged and settled exactly as in any other round — only which
   reviewers run changes.
3. **Confirming round.** Every configured reviewer runs again, including any dropped
   during narrowing. **Only this round can produce a clean approval.** An approval from
   the opening round or a narrowing round is provisional: the branch kept changing after
   it was given, so by the time the loop would act on it, the approval is evidence about
   a version of the branch that no longer exists. A reviewer can approve during narrowing
   and then find something real on the very next look, once the branch has moved
   further — that is exactly what the confirming round exists to catch.

**The config-restore invariant.** Narrowing works by rewriting `review.reviewers` down
to the still-blocking reviewers for one round, then putting the original list back —
never left narrowed for a whole phase. Every read and write of that key goes only
through `devgeta config get` and `devgeta config set`; the loop never reads devgeta's
stored config directly. The rule that governs every one of those reads and writes,
stated here in full because later steps refer back to it rather than repeating it:

- The loop narrows `review.reviewers` only while the key still holds the exact list it
  recorded — checked again immediately before every narrowing write, not assumed from
  an earlier check.
- It restores the key after every single round, never held narrowed for a whole phase:
  narrow, run the round, restore, before the key is touched again for the next round.
- It writes the key at all only when the narrowing set is a strict subset of the
  recorded list. A round that would narrow to the same list it already holds makes no
  write and runs on the configured value as it stands.
- It refuses to narrow at all unless it has first established that it can put back
  exactly the list it found. If that cannot be established, nothing is written, for the
  rest of the run.
- It prints the recorded list and the exact command that restores it before the first
  narrowing write — not only in the final report — so the recovery instruction is
  already on screen if anything interrupts the loop mid-run.
- If the key no longer holds what the loop expects to find there — the recorded list
  before a narrowing write, or the narrowed list before a restore — because something
  else changed it in between, the loop leaves the key exactly as found and stops
  narrowing for the rest of the run.

## Flow

### 0. Resolve the reviewer selector and the note

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
- `--note <text>`: carry that exact text into every `review-run` call, unchanged. Pass it
  verbatim — never summarize it, extend it, or answer it yourself. It is the human's
  message to the reviewers, not an instruction to this loop, and it does not narrow what
  gets reviewed. Omit the flag entirely when no note was given.

### 1. Run a round

Run `devgeta task review-run`, passing `--reviewer <key>` and `--note <text>` when step 0
resolved them — omit each flag entirely otherwise. Read its stdout exactly as printed: one
verdict line per reviewer, nothing else. Never guess at, soften, or invent a verdict the
line does not state. Its stderr progress lines (per reviewer, plus periodic heartbeats)
are not verdicts and carry no findings — do not read anything into them.

Run this yourself, in the main session — not in a subagent. The verdict lines are the one
thing this loop must never take second-hand, and stdout is two or three lines.

Then run `devgeta task review-notes` to see what this round left open. `review-run` does
not print ids, so this is how the loop learns them: every id under the journal's `open:`
section is an unanswered finding, and "nothing under `open:`" (or the
`No review notes for branch <b>.` sentinel) means nothing is open.

### 2. A reviewer failure stops the loop here

If any reviewer's outcome this round is `ERROR(<reason>)` or `NO VERDICT`, stop. Do not
run another round, and do not attempt to fix anything based on a run that did not
complete. Go straight to the terminal report, and name the failing reviewer in it:

- `ERROR(<reason>)`: report the reason verbatim.
- `NO VERDICT(<reason>)`: the reviewer wrote no report at all, and the text in parentheses
  is OpenCode's own words for why. Report it verbatim, the same as for `ERROR`.
- `NO VERDICT` with nothing in parentheses: there is no reason to report — the outcome is
  the bare string. State that the reviewer completed without producing a verdict. Do not
  invent a reason; the outcome carries none, and making one up would misreport what
  happened.

This loop never re-runs a round — a flaky round and a broken one look the same from here,
so both get reported rather than silently rerun. One level below, `review-run` does retry:
a reviewer whose attempt produced no report at all is launched once more inside the same
round (devgeta's ADR-0020), and the outcome you are reading already accounts for it. So a
failure that reaches you has already survived the only retry there is — there is nothing
left for this loop to rerun.

### 3. Check for clean approval

If every reviewer's outcome this round is `APPROVE` **and** the journal you read in step 1
lists nothing under `open:`, look at its settled entries.

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

Otherwise — any reviewer's outcome is not `APPROVE`, or the journal's `open:` section names
any ids even though every outcome was `APPROVE` — this round is not a clean approval. An id
under `open:` is an unanswered finding regardless of what the verdicts say, and step 4 has
not run yet at this point in the flow, so it must never be waved through because every
outcome happened to say `APPROVE`.

Where a round that is not a clean approval goes next depends on whether it left the loop
anything to work on:

- **The journal's `open:` section names at least one id:** continue to step 4.
- **Nothing under `open:`, and some outcome was not `APPROVE`:** stop here and go to the
  terminal report, naming that reviewer and its verdict and stating that the round recorded
  no finding. A reviewer can withhold approval without opening one — `NEEDS DISCUSSION` asks
  for a conversation, and only a reviewer's blocking findings ever reach the journal — so
  there is nothing for step 4 to triage and nothing for a subagent to fix.
  Do not run another round: the loop would change nothing in between, so the next round
  re-runs the same reviewers against the same tree and buys the same verdict.

### 4. Otherwise, triage each open finding, then settle it

For every id under the journal's `open:` section, read its text from `devgeta task
review-notes` and sort it into one of two piles. This triage is a cheap filter, not a
verdict — the verifying happens in step 6.

- **It needs a human, not this loop.** The finding is real but resolving it lies outside
  the branch's code, or asks for a call this loop has no standing to make: a failing or
  missing CI job, infrastructure or environment, a credential, a scope or product
  decision, a release or versioning policy. Do not dispatch a subagent for it and do not
  settle it. Leave it open, and carry it into the terminal report — step 5 stops the loop
  once anything is in this pile.

  Escalating is not an escape hatch. "I would have to read more code", "I'm not sure", or
  "this looks like a big change" are reasons to dispatch a subagent, not reasons to hand
  the finding back. The test is whether the work is outside the branch or outside the
  loop's authority — not whether it is hard.

- **Everything else** goes to the round's fix subagent (step 6), which verifies each
  finding it is given with the rigor of the `receiving-code-review` skill — restate what
  the finding claims, check it against the actual code, don't take it at face value just
  because a reviewer wrote it — and then settles it one of two ways:

  - **The finding is real:** implement the fix, then settle it:
    `devgeta task review-note --settle --id <id> --as fixed --note "<what changed and
where, plus the test command you ran and its result>"`.
  - **The finding is wrong:** do not implement it, and do not leave it open — an open
    finding just comes back next round. Settle it rejected, with the evidence that
    disproves it, and mark it as an agent call:
    `devgeta task review-note --settle --id <id> --as rejected --note "agent: <the
disproving evidence>"`. The `agent:` prefix is mandatory and belongs at the very
    start of the `--note` value, so it lands at the start of the entry's `answer:` line —
    the one line step 3 reads. It is the only thing that tells a later reader — human or
    reviewer — that this rejection is provisional rather than final. Never settle a
    finding rejected without real evidence, and never omit the evidence to save time: the
    human's decision at the end rests on being able to check your reasoning, not just
    your conclusion.

  A subagent that discovers, while verifying, that a finding belongs in the first pile
  after all leaves it open and says so in its report instead of forcing it into one of
  these two.

Never use a rejection to make a disagreement disappear because re-litigating it is
inconvenient. It does not remove the finding from anyone's view — it turns into a
pushback the human sees, and can undo, in the terminal report.

**"Already raised" is never a resolution.** A finding is settled `fixed` only when the code
changed, and `rejected` only with evidence that disproves it. That someone else already
raised the same point — an earlier round, another reviewer this round, a comment on the PR —
says nothing about whether the code is still wrong, so it can never be the grounds for
settling anything. When two open ids are the same point and the point is real, fix it once
and settle both `fixed`, citing the same change; never settle one on the grounds that the
other exists. A finding left open keeps the loop out of step 3's clean approval, which is
exactly what should happen while the problem is still in the code.

When a subagent ran, whatever it reports, the journal is what counts: after it returns,
re-read `devgeta task review-notes` yourself and treat that as the state of the round. A
finding the subagent says it settled but the journal still lists under `open:` is not
settled.

### 5. Stop for anything escalated, then enforce the round cap

After this round's findings are handled:

- If anything is still open after this round — step 4's triage left it for a human, or the
  fix subagent escalated it back while verifying — stop; go to the terminal report, even if
  rounds remain. The journal you re-read at the end of step 4 decides this: any id still
  under `open:` counts, whichever way it got there. Another round cannot clear it: the
  finding is still open, so step 3 could never call the result a clean approval, and every
  further round pays the reviewers to raise it again and waits on the same answer.
- Otherwise read `devgeta config get review.rounds`. If this was that many rounds, stop —
  go to the terminal report regardless of what the branch's state is at that point.
- Otherwise return to step 1 and run another round.

### 6. The fix subagent

When step 4's second pile has anything in it, dispatch **one fresh subagent per round**,
carrying all of that round's findings from that pile — never one subagent per finding.
Per-finding subagents each rebuild the same branch context and re-run the same suites, and
they cannot see each other's edits, so two fixes touching one file collide.

When that pile is empty — step 4 sent every open finding of this round to a human —
dispatch nothing and go straight to step 5, which stops the loop. A subagent with no
findings has nothing to fix, and dispatching one anyway invites handing it the escalated
finding step 4 just said not to dispatch a subagent for.

The main session stays out of the fix work entirely: it never reads a diff, runs a test,
or edits a file for a finding. It sees the verdict lines, what the subagent reports, and
what `devgeta task review-notes` says afterwards — not the work in between. That is the
whole point of the split, and it is also why the subagent is fresh: it starts on the
findings with nothing else in its context.

A fresh subagent inherits nothing from this session, so the dispatch must carry
everything it cannot look up — and nothing it can. Include:

1. One line on what this branch is changing and why. A diff shows what moved, never the
   intent behind it.
2. Every id you are handing it, with the finding's text and its `path:line` cite copied
   **verbatim** from `devgeta task review-notes`.
3. The human's `--note` from step 0, verbatim, if there was one. It exists only in this
   session — drop it here and it is gone.
4. The instruction to follow the repo's own contributor guide (`CLAUDE.md`, `AGENTS.md`,
   `CONTRIBUTING.md` — whichever it has) for how code and tests are written here, and to
   read it rather than guessing.
5. The verification standard: the rigor of the `receiving-code-review` skill, as spelled
   out in step 4.
6. The two settle commands from step 4, verbatim, including the `agent:` prefix rule.
7. The never-do list below.
8. The return contract below.

Do **not** paste the diff, file contents, test output, or a recap of earlier rounds. The
subagent runs `git diff` and `devgeta task review-notes` itself and gets the current
truth; a pasted copy is context you pay for twice and is stale the moment a fix lands.

The never-do list, which every dispatch carries:

- **Never move HEAD** — no `git switch`, no `git checkout <branch>`, no rebase or merge.
  `devgeta task review-run` abandons a whole round whose HEAD moved, so this throws away
  the round it is working on.
- **Never commit, stage, or stash.** Uncommitted work is reviewable as it stands.
- **Never open a new finding.** Reviewers do that; a fixer only settles what it was given.
- **Never settle a finding because it is already known.** Duplicate of another id, already
  discussed on the PR, raised in an earlier round — none of that is a fix or a
  disproof. Settle on the code, or leave it open.
- **Never retire another agent's provisional rejection.** Those two journal transitions
  are the human's alone and are named only in the terminal report below.
- **Verify before settling `fixed`.** Run the tests covering the change and name the
  command and its result in the `--note`, so the journal carries the evidence.

The return contract: one line per id — `<id> — fixed | rejected | needs-human — <one
clause>` — and nothing else. No diffs, no test output, no file contents, no summary of
how the work went.

## Terminal report

Every run of this loop ends here — there is no third outcome, and this section is the
only place either human-only journal transition is written out.

**Clean approval** (step 3's clean case):

```
## Review loop — clean approval

Round <n> of <cap>. Every reviewer approved, the journal lists nothing under `open:`, and
no settled entry's `answer:` line carries an `agent:` rejection awaiting ratification.
```

**Report to the human** — everything else, including a reviewer failure (step 2), a round
that withheld approval without recording a finding (step 3), a finding that needs a human
(step 4), and hitting the round cap (step 5):

```
## Review loop — report

### Rounds
| Round | Reviewer | Verdict |
|-------|----------|---------|
| 1     | <label>  | <verdict> |
| ...   | ...      | ... |

(If the loop stopped because a round withheld approval but left nothing under `open:`, say
so in one line under this table, naming the reviewer and its verdict. The table shows what
the verdict was, never why the loop stopped.)

### Findings that need you
| id  | Finding | Why the loop did not settle it | What it is waiting on |
|-----|---------|--------------------------------|------------------------|
| n5  | <the reviewer's finding> | <what puts it outside the branch's code or this loop's authority> | <the decision or action you need to take> |

(These are still open in the journal, deliberately — nothing was settled on your behalf.
Omit this table when nothing was escalated.)

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
- Run the rounds and settle findings yourself, without asking — running this command is the
  authorization for the whole cycle, and an unattended loop that stops to ask before each
  round is not unattended. The single exception is retiring an agent's rejection, which
  stays the human's call (see the report section above).
- Never invent a reviewer's verdict, and never present a run that failed, or a run that
  hit the round cap, as one that passed.
- If `devgeta task review-run` itself refuses to run (default branch, detached HEAD, a
  branch that changes nothing at all, or a blank `--note`), surface that error as-is and
  stop before running anything else.
- Uncommitted work is reviewable. Never commit, stage, or stash anything to get a round to
  run, and never ask the human to — the reviewers see the working tree as it is.
