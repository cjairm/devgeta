---
description: Use when a pull request should be reviewed now, or watched and answered unattended — read the PR's review state, run the reviewer agents over its own diff, and post at most one review or approval. Running it reviews the named PR; an unattended tick reviews when a review is requested of you. Use for a single look at one PR or for a standing watch on it ("review PR 213", "watch PR 213", "keep answering reviews on it")
---

Review one pull request and answer review requests on it. **One invocation is one tick:**
read the PR's current state, take exactly one action, exit. This file holds no timer and
keeps nothing between runs.

A tick comes from one of two places, and that is the only thing that changes what it does:

- An **explicit tick** is one a human typed. It reviews, because running the command is the
  request — it was addressed to this tool, by the person running it, about the PR they named.
- A **watch tick** is one a repeat driver fired. It carries `--on-request`, and it reviews
  only when GitHub says a review is requested of the authenticated user.

GitHub's own review-request field is the watch's whole memory, so running a watch tick twice
in a row, or again after a crash, is always safe. An explicit invocation is not idempotent and
is not meant to be: typing the command twice reviews twice, because asking twice is asking
again.

The PR does not have to be checked out. Every step below reads the PR's own commits,
fetched read-only into refs nothing else drives, so the working tree is never the source
and is never modified. That matters here more than usual: a tick can fire while the human
is mid-edit in the same clone.

## Usage

```
/pr-review-loop [PR_NUMBER] [code|document|skill ...] [--reviewer <type>] [--note <text>] [--once] [--on-request]
```

`PR_NUMBER` is optional and resolves from the current branch's PR when omitted — the same
inference every PR command makes. Pass it when reviewing a PR whose branch is not the
checkout, which is the normal case for reviewing someone else's work.

The types name **which reviewer agents run**, and more than one is allowed: `code document`
is two reviewer runs, one per type, each covering every configured model internally. The
three values are `code`, `document`, and `skill` — exactly the keys the `--reviewer` flag of
`devgeta task review-run` takes, passed straight through with no translation. There is no `doc`
shorthand; a friendlier spelling here would be a second vocabulary that can drift from the
one the runner validates against. Types omitted → judge them from what the PR changes.

`--reviewer <type>` and `--reviewer=<type>` are a second **spelling** of those same three
values, accepted because it is the flag the sibling `/review-loop` takes and therefore what
a human moving between the two actually types. The spellings mix freely and mean exactly
the same thing: `code --reviewer=document` is two runs. Nothing is translated on the way
through, so `doc` is still an error however it is written.

`--note <text>` is the human's own emphasis, forwarded verbatim to every reviewer run of
the tick (e.g. `--note "the retry path is the risky part"`). It adds context; it never
narrows what gets reviewed.

`--once` reviews and starts no repeat driver — a single look, after which nothing watches the
PR.

`--on-request` marks a tick a repeat driver fired. **No human ever types it.** It does two
things and nothing else: it makes the tick request-gated, so an unattended watch reviews only
when GitHub says a review is asked of this user, and it stops that tick from starting a driver
of its own.

**Repetition belongs to the driver, not to this file — but starting it is part of the job.**
One tick watches nothing, so an explicit tick whose outcome leaves the pull request still
worth watching starts the harness's own repeat driver on this command and lets it run the
ticks from then on: on
Claude Code, `/loop <interval> /pr-review-loop <n> [types] [--note <text>] --on-request`, with
the interval the human named or the driver's default when they named none. A standing watch
("watch PR 213", "keep answering reviews on it") is what that gives them, and it is the
default; `--once` is how a human asks for the single look instead.

**The handoff happens at the end of the tick, not the start.** A repeat driver fires at its
next scheduled match and never on creation, so handing off first would answer the person who
asked for a review with a state read now and the review a whole interval later. Step 11 starts
the driver, after the review this invocation was asked for and after the outcome is known —
which is also what lets a first-look approval start nothing at all.

**Hand the driver every argument, the note included.** A repeat driver re-runs the command
line it was given and nothing else, so whatever is left out of the handoff is left out of
every tick it ever fires — and the human is not there to notice. Carry the PR number, the
types, and the `--note` text through verbatim and still quoted, so the note reaches the
reviewers on tick fifty the same way it would have on a single look. `--on-request` is the one
argument on the driver's line that the invocation did not have: it is what tells each fired
tick that it is a watch tick.

**A `--on-request` tick starts no driver, whatever it finds.** The driver that fired it is
already running, so a handoff there would start a second driver on every tick, and each of
those would start another — a watch that multiplies instead of repeating. Starting and
stopping the driver both stay outside this file: a watch tick reviews or waits, reports, and
exits.

Where the harness has no repeat driver (OpenCode has none), the handoff is impossible rather
than optional: run the tick — it still reviews — and let step 12 say plainly that nothing will
run another, so the human knows the watch is theirs to press.

In both modes, the review runs at most once per tick and at most one review is posted per
tick — step 9's single re-ask is the only repeat of the posting step.

## Authority to post

Running this command **is** the authorization for the whole tick: the state read, the
read-only fetch of the PR's refs, the reviewer runs, and the posting step at the end —
whether that step is `/review-pr` or `/approve-pr`. **Do not ask before any of it, do not
show the review or the approval for confirmation first, and do not stop at "shall I post
this?".** A watch that pauses for a go-ahead each tick is not unattended, and the human
started the loop precisely so they would not have to be present for it. Step 11's handoff to
the repeat driver is authorized the same way — start it, do not stop to confirm it.

This authorizes _acting unprompted_, nothing else. Every gate below still holds: the
decision table still picks exactly one action, the pre-post re-check at step 7 can still
cancel the post, the verdicts still come from the reviewer runs rather than from your own
read, and an escalation is still reported to the human instead of decided for them.

## What this drives

- `devgeta task pr-review-state --pr <n>` — the state read, and the trigger a watch tick is
  gated on. Prints exactly three lines
  and nothing else: `pr:` (`open | draft | merged | closed`), `requested:` (`yes | no` —
  whether the authenticated user is in the PR's review requests **right now**), and
  `my-review:` (`approved | changes-requested | commented | none` — that user's latest
  submitted review). A request addressed to a team and not to the user reports
  `requested: no`; claiming a team's work is a staffing judgment this loop does not make.
- `devgeta task pr-review-target --pr <n>` — fetches the PR's head and base read-only, then
  prints the immutable review target: a `base:` merge-base sha, a `head:` sha, a `journal:`
  key, and the noise-filtered `files:` list. Each file is a `- <path>` line, and one
  `excluded (see …): …` receipt can follow the list when the range had filtered noise — a
  trailing line that does not start with `- ` is that receipt, not a path. A failed fetch is
  an error, not a warning, and it ends the tick — a confident review of code the PR no longer
  contains is worse than no review.
- `devgeta task pr-view --pr <n>` — the PR's purpose, description, and linked ticket.
- `devgeta task review-run` in range mode — one reviewer type against the PR's range. Takes
  `--reviewer <type>` plus `--base <sha> --head <sha> --journal <key> --report-dir <dir>`,
  and an optional `--note <text>`. Those four range flags are **required together**; three
  of them is an error. Every model
  configured in `review.reviewers` runs the selected agent sequentially and headless, and
  the output is one line per model and nothing else.
- `devgeta task review-notes --branch <key> --rev <sha>` — the PR's review journal, read at
  the reviewed revision. **This is the only place open findings are listed** — `review-run`
  prints verdicts and report paths, never ids — so this is how the tick learns what is
  open.
- `devgeta task scratch` — a fresh directory for this tick's reviewer reports.
  `devgeta task scratch --clean <path>` removes it.
- `devgeta task current-pr` — the current branch's PR number, when none was passed.
- `/review-pr <n> --base <base> --target <head> --journal <key>` and
  `/approve-pr <n> --target <head>` — the posting step. Exactly one of them runs per tick
  (step 8), and step 9's single re-ask is the only repeat of either. `/review-pr` gets the
  journal key because it may settle findings and must settle
  them in this PR's journal; `/approve-pr` never reads or writes the journal, so it takes no
  key.

Invoke the `devgeta` binary only — never a `dg` alias, `go run`, or a local build. Only the
installed binary is available in this environment.

## Flow

### 0. Resolve the PR number, the reviewer types, the note, and the mode

Parse `$ARGUMENTS` once, before anything else. The values resolved here are used unchanged
for the rest of the tick.

- A bare number is `PR_NUMBER`. With none given, run `devgeta task current-pr` and use what
  it prints. If it reports that the branch has no pull request, stop and say so — there is
  nothing to review.
- `--once` and `--on-request` are flags, not values. Recognize both here, before anything
  else is classified, so neither is ever read as a reviewer type. `--on-request` makes this a
  **watch tick** that a repeat driver fired; without it the tick is **explicit**, typed by a
  human. `--once` means start no driver at the end of the tick. A tick may carry neither,
  either, or both.
- Reviewer types arrive in two spellings, and both are read here: a bare word (`document`),
  and `--reviewer <type>` or `--reviewer=<type>`. Take them in any mix and in the order given.
  Each resolved value must be `code`, `document`, or `skill`. Anything
  else: stop before reading any state, and report that the types are `code`, `document`, or
  `skill`, naming the value that was actually passed. Do not guess which one was meant, and
  do not map a near-miss like `doc` or `docs` onto `document` — these values are forwarded
  to `devgeta task review-run --reviewer` verbatim, and this file owns no translation. A
  `--reviewer` with nothing after it is that same error, reported the same way.
- No types at all is normal, not an error: step 3 judges them from the PR's changed files.
- `--note <text>` is carried verbatim into every `review-run` call this tick makes. Pass it
  exactly as written — never summarize it, extend it, or answer it yourself. It is the
  human's message to the reviewers, not an instruction to this loop. Omit the flag entirely
  when no note was given.
- Anything still left over once the above have been taken — an unknown flag, a word that is
  neither a number nor a type — stops the tick before any state is read, naming the value.
  That rule is what makes the list above exhaustive, so it must never be what swallows a flag
  the Usage line advertises: a flag documented up there and missing from here silently does
  nothing at all.

Nothing is handed off for repetition here. That is step 11, after the tick's outcome is known.
Everything from step 1 on is one tick, whether a driver fired it or a human did; only which
rows of step 2's table apply, and whether step 11 starts anything, differ between the two.

### 1. Read the state once

```bash
devgeta task pr-review-state --pr <n>
```

Read the three lines exactly as printed. This one read, plus the mode step 0 resolved, selects
the tick's action; nothing else does. Do not re-read it here to "confirm" anything — the only
other state read in the tick is step 7's, which exists for a different reason.

### 2. Take exactly one row of the table

The row depends on the state; the action depends on the row **and** on the mode step 0
resolved. Rows are evaluated top to bottom, and **the first match wins** — every row states
the full state it matches, so read `requested:` off the row rather than checking it
separately:

| `pr:`             | `requested:` | `my-review:`  | Explicit tick                               | Watch tick (`--on-request`)                   |
| ----------------- | ------------ | ------------- | ------------------------------------------- | --------------------------------------------- |
| `merged`/`closed` | any          | any           | **Terminal: closed.** Report, stop the loop | **Terminal: closed.** Report, stop the loop   |
| `draft`           | any          | any           | **Review** — steps 3 to 9                   | Wait — a formal review on unfinished work     |
| `open`            | `yes`        | any           | **Review** — steps 3 to 9                   | **Review** — steps 3 to 9                     |
| `open`            | `no`         | `approved`    | **Review** — steps 3 to 9                   | **Terminal: approved.** Report, stop the loop |
| `open`            | `no`         | anything else | **Review** — steps 3 to 9                   | Wait — the ball is with the author            |

**An explicit tick reviews unless the pull request is over.** Only `merged` and `closed` stop
it: there is nothing left to review, and a review posted on a closed PR is noise nobody can
act on. Every other row is a review, request button or not — the human typed the command, and
that is a request already, addressed to this tool about the PR they named. Gating it on a
GitHub field would make this file ask for a permission it was just handed.

The two rows an explicit tick overrides exist to protect the **author** from a review they did
not ask to receive, and neither reason survives a human asking for one deliberately. A draft is
exactly when an author wants a private read of their own unfinished work, and "review it again"
is a normal ask after a rebase or a late doubt.

**A watch tick takes the rows as written**, and that is what keeps an unattended watch from
reposting: submitting any review removes the user from the PR's review requests, so the watch
tick after a post reads `requested: no` and waits, and the author's re-request is the next
trigger. Draft is checked **before** the request state on purpose, so a requested draft still
waits on a watch tick: a draft is work the author is still shaping, and a formal review posted
on it unattended is noise they did not ask to receive yet, request button or not.

Reading `requested:` off the row is also what keeps the two request states apart. An author who
re-requests a review on a PR this user already approved matches the `requested: yes` row and is
reviewed, never the standing-approval row below it.

**Wait** means exactly that: report the tick in the shape step 12 gives and take no other
action. No fetch, no reviewer run, nothing posted, and step 11 starts nothing new. Most watch
ticks are a wait row, which is why they are cheap.

**Terminal** means the watch is over: report it, run step 10's ref cleanup, and say plainly
that the loop should be stopped, so a human or a driver reading the report knows not to tick
again. A terminal tick also starts no driver at step 11, so a first-look approval leaves
nothing running. Nothing in this file can stop a driver that is already running.

Two consequences of reading current state rather than a local log are worth knowing, because
both look like bugs and are neither. A colleague who answers the request first removes the
user from the request list, so the next watch tick simply waits. And a dismissed approval
reports `my-review: none` rather than `approved`, so the watch keeps going on an approval
GitHub has already thrown away instead of stopping on it.

### 3. Resolve the review target

```bash
devgeta task pr-review-target --pr <n>
```

This is **the** context for the rest of the tick: the `base:` and `head:` shas, the
`journal:` key, and the `files:` list. Every later step reads these values. Nothing reads
the working tree, and nothing re-resolves a ref name into a sha — a review takes minutes
across several models, and a name resolved twice inside that window can mean two different
commits.

If the command fails, end the tick with its error in the report. Never fall back to whatever
refs are already on disk and never fall back to the checkout: both describe code that may
not be this PR.

Then read the PR's own account of itself:

```bash
devgeta task pr-view --pr <n>
```

Read the purpose and the linked ticket before any code. A diff shows what moved, never why.

Now fix the reviewer types. If step 0 resolved any, use exactly those. Otherwise judge them
from the target's `files:` list: `document` for docs and prose, `skill` for agent, command,
and skill prompt files, and `code` for **everything else**. A mixed PR takes the matching
set — more than one type is normal and each becomes its own run.

`code` is the catch-all on purpose, so that every listed file lands in some bucket. The code
reviewer reads the whole diff of the range it is given rather than a list of source
extensions, so a workflow YAML, a shell script, a Makefile, a JSON config, or an asset is
work it can judge. The alternative is worse than a slightly loose fit: a file matching none
of the three types would resolve to no type at all, and a tick with no type runs no reviewer
and then has nothing to weigh in step 6.

So a non-empty `files:` list always yields at least one type. If you somehow end up with
none anyway, that is a bug in this step and never an approval: end the tick as `escalated`,
naming the files you could not place, and post nothing.

If `files:` prints `(none)`, the whole range is either empty or excluded as noise, so there
is nothing for a reviewer lens to judge. End the tick without running a reviewer and without
posting, and report it as `nothing to review` — its own status word in step 12, because
nothing is pending on anyone and calling it a wait would misdescribe it. Any review request on
the PR stays pending, so the report is what tells the human this PR needs their own eyes; a
review of an empty file list would say nothing and still cost a post.

### 4. Allocate the scratch directory

```bash
PR_REVIEW_SCRATCH=$(devgeta task scratch)
```

This is where the reviewer reports land for this tick. Step 10 removes it. The name is the
tick's own: `/review-pr` allocates a `SCRATCH` of its own in step 8 and cleans it there, so a
variable shared between them would leave step 10 cleaning a directory that is already gone and
this tick's reports behind.

### 5. Run the reviewers, one run per type

For each resolved type, in turn:

```bash
devgeta task review-run --reviewer <type> --base <base> --head <head> --journal <key> --report-dir "$PR_REVIEW_SCRATCH"
```

Add `--note <text>` when step 0 resolved one, with the human's text verbatim. Pass all four
range flags every time, with the values from step 3 — the group is required together, and
review-run rejects a partial one rather than guessing at the missing end of the range.

Run these yourself, in the main session, not in a subagent. The verdict lines are the one
thing this tick must never take second-hand, and each run's stdout is a few lines.

Read stdout exactly as printed. One line per configured model:

```
<label> → APPROVE | REQUEST CHANGES | NEEDS DISCUSSION | NO VERDICT | NO VERDICT(<reason>) | ERROR(<reason>)  report: <path>
```

Never guess at, soften, or invent a verdict a line does not state. Progress goes to stderr
while a run works (a line per reviewer plus a periodic heartbeat) so a long run reads as
working rather than stuck; none of it is a verdict and none of it carries findings. Never
pass `--verbose`: it replaces the heartbeat with a line per tool call, which is hundreds of
lines this tick does not read.

**Parse the `report:` field from the right** — find the **last** occurrence in the line of
the exact sequence `report:` preceded by two spaces and followed by one, and take everything
after that final space as the value. The value therefore starts at the first character of the
path and never carries a leading space. The value itself can contain spaces: a run that
produced nothing prints
`report: none (the reviewer wrote no report)`, and an `ERROR(<reason>)` reason is the
model's own text, which could contain that same sequence too. Splitting the line from the
left, or on the first match, gets both of those wrong.

Keep, for every run: its type, its model label, its outcome, and its report path. Step 6
weighs the outcomes and step 8 reads the reports.

### 6. Aggregate every run's verdict, once

Weigh **all** the runs together — every type times every model — and pick one of four
outcomes here. This is the only place the outcome is decided; steps 7 to 9 act on it and
never recompute it.

**Count the runs before weighing them.** Every rule below is a statement about the runs that
happened, and each one is trivially true of nothing — "every run approved" most of all. So
zero runs is its own outcome, checked first, and it is never an approval:

- **No run happened this tick → terminal: escalated.** Step 3 resolved no reviewer type, or
  step 5 never got as far as running one. Say which, name the PR, and go to step 10. A tick
  that ran no reviewer looked at no code, and posting an approval for it would put your name
  on a review no model performed.
- **Any run's outcome is `ERROR(<reason>)` or `NO VERDICT` → terminal: escalated.** Name the
  failing run — its type and its model label — and go to step 10. Never approve on a run
  that did not complete, and never re-run it: `review-run` already relaunches a reviewer
  that produced no report at all, once, inside the same run, so a failure that reaches you
  has already survived the only retry there is. Report an `ERROR` reason verbatim. A
  `NO VERDICT(<reason>)` carries the runner's own words for why, also verbatim. A bare
  `NO VERDICT` carries no reason at all — state that the reviewer completed without
  producing a verdict, and do not invent one.
- **At least one run happened and every run's outcome is `APPROVE`** → the approval path in
  step 8. Both halves are required: the count first, the verdicts second.
- **Otherwise** — at least one `REQUEST CHANGES` or `NEEDS DISCUSSION`, and no `ERROR` and
  no `NO VERDICT` anywhere — the review path in step 8.

One blocking outcome from any single run is enough. The runs are independent opinions, not
votes to be tallied: a finding one lens or one model sees is not cancelled by the others
missing it.

### 7. Re-check the state and the head before posting

The reviewer runs take minutes, so the world may have moved. Before anything is posted,
read both again:

```bash
devgeta task pr-review-state --pr <n>
devgeta task pr-review-target --pr <n>
```

Two conditions, and **either one failing means post nothing.** The first is mode-aware — it is
step 2's split applied a second time, to the fresh read — and the second is the same in both
modes:

- **The PR must still be somewhere a review belongs**, which is the row this tick's mode takes:
  - **Explicit tick:** `pr:` must still be neither `merged` nor `closed`. Nothing else here can
    cancel the post. In particular `requested: no` cannot: on most explicit ticks it was never
    `yes`, and requiring it would run the whole cross-model review and then post nothing.
  - **Watch tick (`--on-request`):** the state must still land on the Review row (`open` and
    `requested: yes`). Anything else means the PR merged, closed, or went draft, or that
    someone else already answered the request — so posting now would be a duplicate or an
    unsolicited review.

  When the condition fails, take the row the fresh state selects for this tick's mode
  (terminal or wait) instead, then go to step 10.

- **`head` must still equal the sha the reviewers read.** If it moved, the author pushed
  mid-review: the reviews describe code the PR no longer is, and an approval would cover
  commits no reviewer saw. Post nothing, end the tick reporting that the head moved during
  the review, and go to step 10. A moved head is a non-terminal exit, so the refs are kept and
  a watch is still wanted — and on the watch side the request is still pending, so the next
  tick reviews the new head from scratch. An author pushing repeatedly just defers the review,
  which is the correct outcome rather than starvation.

### 8. Post exactly one review

Step 6 chose the path. Run **one** of these two commands, **once**:

- **Every run `APPROVE`** → `/approve-pr <n> --target <head>`. Its own file verifications
  read the reviewed commit rather than the working tree, and the cross-model `APPROVE`
  verdicts sitting in this conversation are the basis it needs for approving over live
  non-blocking comments.
- **Otherwise** → `/review-pr <n> --base <base> --target <head> --journal <key>`. The two shas
  and the key all come from step 3, so the posted review's diff is the same merge-base range
  the reviewers read and any settling lands in this PR's journal. Before invoking it, put the
  findings in context: read every `report:` file from step 5 (skipping any whose value was the
  no-report sentinel) and run `devgeta task review-notes --branch <key> --rev <head>`. The
  reports carry the full cross-model findings — every severity, the strengths, the evidence —
  while the journal carries only the blocking entries as one-liners, and a review composed
  from the one-liners alone throws most of what the reviewers found away. `--journal <key>` is
  what makes `/review-pr`'s own journal calls carry `--branch <key> --rev <head>`, so it reads
  and writes this PR's journal rather than the checkout branch's; without it, a settle would
  close an id in whatever journal the checkout happens to have.

Never run both. Never run either twice — the single exception is step 9's one re-ask, which
is the same `/approve-pr` invocation and is bounded there. Each invocation posts exactly one
thing, so the whole of what reaches the PR is one review or one approval — plus, on that
re-ask path only, the decline the first `/approve-pr` already posted.

Both commands stamp the posted review with the reviewed commit, so what lands on GitHub
names the sha it judged even if a push races the submission itself.

### 9. Read the approval outcome

Only the approval path has an outcome to read. `/approve-pr` prints one line:

```
## PR #<num> — <approved | not approved>
```

- **`approved` → terminal: approved.** The loop stops now, including on the very first
  trigger. It never keeps listening past an approval it posted.
- **`not approved` → one re-ask, approve-only.** This branch is reached only when every
  reviewer run said `APPROVE`, and that verdict is exactly the basis `approve-pr.md` names
  for approving over live comments. So invoke `/approve-pr <n> --target <head>` one more
  time, stating that the cross-model verdict in context is `APPROVE` and that the expected
  outcome is an approval whose body is `LGWC; <who/what remains>`, naming the leftover
  non-blocking comments — not a re-review, and not a comment.
  - Approved on the re-ask → terminal: approved.
  - Still `not approved` → **terminal: escalated.** It is standing on a blocker every
    reviewer run missed. Report what it named and stop.
  - **Never a third ask.** A decline leaves `requested:` at `yes`, so asking forever would
    post forever.

  Step 7's gate is deliberately not re-run before the re-ask. The re-ask is the same
  invocation naming the same `--target <head>`, so anything it posts is stamped with the
  commit the reviewers actually judged — the attribution that bounds a late post is already in
  place, and a second state read here would narrow a window it cannot close while adding a
  second way for one decision to be read twice.

The review path has nothing to parse. `/review-pr` prints its own summary line; record it in
the tick report and keep listening. Posting any review — approve, comment, or request
changes — removes the user from the PR's review requests, so the next **watch** tick reads
`requested: no` and waits. The author's re-request is the next trigger. `/review-pr` can
itself decide to approve — its own no-new-findings branch — and this tick still reports
`reviewed`, which names what it ran rather than what the PR now is; the next watch tick's state
read sees `my-review: approved` with `requested: no` and stops on the terminal-approved row, so
that reading corrects itself without a second check here.

### 10. Clean up

Two cleanups with two different scopes. Get the scopes right: the first runs far more often
than the second.

**The scratch directory, on every exit taken after step 4 allocated it.** The condition is
that one thing — **step 4 ran, so this runs** — and the shape of the exit does not matter:
approved, escalated (including step 6's), the head moved or the state changed at step 7, a
review posted, a submit that failed, or a command refusing to run mid-tick under the last
rule in "Notes". Those are examples, not the list. An instruction anywhere in this file to
stop, to end the tick, or to report an error and go no further means stop _through here_,
never around it:

```bash
devgeta task scratch --clean "$PR_REVIEW_SCRATCH"
```

`--clean` is idempotent, so running it after a partial failure is safe. If a submit failed,
print the review into the tick report **before** cleaning, so nothing is lost and the human
can post it by hand. The one exit this cannot cover is the process being killed mid-tick — a
dead process runs no cleanup — and that directory is swept by the existing
`dg configure --force` scratch sweep, the same recovery every scratch user has.

**The PR's fetched refs, only on the three exits that are terminal for the loop** —
approved, closed, or escalated:

```bash
git update-ref -d refs/devgeta/pr/<n>/head
git update-ref -d refs/devgeta/pr/<n>/base
```

These cannot go earlier. Every step from 3 onward reads them, and holding them is what keeps
a concurrent `git gc` from collecting the commits under review. A non-terminal exit — a
wait row, a head that moved, a review posted, nothing to review — **keeps** them, because
the next tick reviews the same PR. Run these on a terminal exit reached straight from the
table too (closed, or approved with no review this tick): a previous tick may have left
them. They are keyed by PR number and reused per tick, so a killed tick leaks one pair and
the leftovers are bounded by the number of distinct PRs reviewed, not by ticks.

### 11. Start the watch, unless this tick was one

The tick's outcome is known by now, which is the whole reason this step sits here and not at
the top: the review the human asked for has already happened, and whether a watch is still
wanted depends on how the tick ended.

Start a repeat driver only when all four of these hold:

- **This tick is explicit** — `--on-request` was not on its command line.
- **`--once` was not passed.**
- **The exit is not terminal** — not approved, not closed, not escalated.
- **The harness has a repeat driver at all.**

Then start the harness's own repeat driver on this command and say in the report that you did.
On Claude Code:

```
/loop <interval> /pr-review-loop <n> [types] [--note <text>] --on-request
```

Every argument travels verbatim and still quoted — the PR number, the types, the `--note` text
— plus `--on-request`, which is what makes each fired tick a watch tick. A driver re-runs
exactly the line it was handed and nothing else, so an argument dropped here is dropped from
every tick the watch ever fires, and the human is not there to notice. The interval is the one
the human named, or the driver's default when they named none.

The driver's own first firing is harmless whenever it lands, because it is request-gated. If
this tick posted a review, that already cleared the request, so the first fired tick reads
`requested: no` and waits. If it posted nothing, a request still standing on the PR is exactly
what a watch tick should review.

Otherwise start nothing, and let step 12 say so:

- **A `--on-request` tick starts no driver, whatever its outcome.** The driver that fired it is
  already running, so a handoff here would start a second one on every tick and each of those
  would start another — a watch that multiplies instead of repeating.
- **A terminal exit starts nothing.** The watch is over, so a driver started here would be one
  the human has to remember to stop.
- **`--once` starts nothing.** That is the whole of what the flag is for.
- **A harness with no repeat driver starts nothing** (OpenCode has none), because there is
  nothing to start. The tick still reviews; only the repetition is missing.

Never ask before starting the driver — see "Authority to post". And note the asymmetry: this
step can start a driver but can never stop one. A driver already running keeps firing until the
human or the harness stops it, however this tick ended, which is why a terminal exit has to say
so in the report.

### 12. Report the tick

Three lines at most: what the state read said, what ran and what it returned, and what
happens next. A tick is a line in a log a human skims, not a document.

```
## PR #<n> — <waiting | nothing to review | reviewed | approved | closed | escalated | head moved>

<the state read: pr/requested/my-review, or the reason the tick stopped early>
<the runs and their verdicts, one clause — omit on a wait row, where nothing ran>
<what the next tick expects, or "the watch is over — stop the loop" on a terminal exit>
```

On a non-terminal exit, that last line must also say **what will run the next tick** — the
driver, when one is repeating this command, which is the one step 11 just started or the one
that fired this tick, or **nothing**, when step 11 started none. The "nothing"
case is the one that matters: a lone tick leaves the PR unwatched, and a line that only says
what the next tick expects reads as a watch this invocation never started. So whenever step 11
started none — on `--once`, or on a harness with no repeat driver — say plainly that **nothing
will run another tick**, and name what would start one
(`/loop <interval> /pr-review-loop <n> [types] [--note <text>] --on-request` on Claude Code,
carrying this tick's own arguments so the watch reviews what this tick reviewed, or a tick per
invocation by hand where the harness has no driver), so the human is one step from a real
watch.

On an escalation, name the failing run and its reason verbatim in place of the next-tick
line. On a terminal exit, say the watch is over explicitly: step 11 started nothing, and
stopping a driver that may still be running is the human's or the harness's action, not this
file's.

## Notes

- Run the whole tick yourself, without asking — see "Authority to post" above. Running the
  command is the authorization for every part of it, posting included, and a watch that
  stops to confirm each tick costs the human exactly the attention the loop exists to save.
- One invocation is one tick — plus, when step 11 hands this command to a repeat driver, one
  driver started. This file owns no repetition either way: it never sleeps, never retries a
  tick, and never loops back to step 1 within a run.
- At most one review reaches the PR per tick: step 6 picks one path, step 8 runs one command
  once, and step 9's single re-ask is the only repeat of either.
- This tick never edits code, never resolves threads, never re-requests reviewers, and never
  settles a finding it did not go through `/review-pr` to settle.
- It never moves the checkout either: no branch is created, switched, or checked out,
  nothing is stashed, staged, or committed, and it never pulls or merges. The human may be
  working in this clone while a tick runs, and the review target comes from fetched refs
  that touch no branch and no working tree.
- Never invent a verdict, and never present an escalation, a head-moved exit, or a wait as
  an approval.
- If any `devgeta task` command refuses to run — a PR number that is not a PR number, a
  branch with no PR, a fetch that failed, a blank `--note` — surface that error as-is in the
  tick report and end the tick. Do not work around it. **Ending the tick still goes through
  step 10:** when step 4 has already allocated the scratch directory, clean it on the way out,
  the same as any other exit does. Only a refusal that happens before step 4 has nothing to
  clean, and the reviewer runs, both step 7 reads, and step 8's journal read all happen after
  it.
