# ADR-0026 — The review loop narrows to the reviewers still blocking, then confirms with all of them

**Date:** 2026-08-12
**Status:** ACCEPTED
**Related:** [ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md) (**amends its §1 round cap and its §3 no-retry rule**; the rest of it governs unchanged — see [What this amends in ADR-0017](#what-this-amends-in-adr-0017)), [ADR-0020](ADR-0020-a-reviewer-that-reports-nothing-is-retried-once.md) (its within-round retry runs one level below and is untouched), [cycle 2026-08-12-review-loop-narrows-to-blocking-reviewers](../plans/cycles/2026-08-12-review-loop-narrows-to-blocking-reviewers.md)

## Context

`/review-loop` runs every model in `review.reviewers` on every round until a clean
approval or the round cap. Four consecutive runs against one branch exposed what
that costs when reviewers disagree in a stable way:

| Reviewer                          | Rounds run | Verdicts                         |
| --------------------------------- | ---------- | -------------------------------- |
| `github-copilot/gpt-5.6-terra`    | 9          | 8 × REQUEST CHANGES, 1 × APPROVE |
| `github-copilot/gemini-3.6-flash` | 7          | 7 × APPROVE                      |

Gemini approved every round it completed and never opened a finding. It was
re-run six more times, at roughly $0.4 a round, to re-state an answer already
given. Every finding in the run — all 21 — came from terra.

The loop has no way to spend its rounds on the reviewer that is actually
blocking. It also has two rules that made the run worse:

**A failure kills the whole loop.** Step 2 stops on any `ERROR` or `NO VERDICT`,
on the reasoning that "a flaky round and a broken one look the same from here."
Three rounds died this way, and every one of them turned out to be transient:
terra hit `external_directory` permission auto-rejects twice (once for
`~/.claude/*`, once for `~/*`) while verifying claims about state that genuinely
lives outside the repo, and gemini produced one bare `NO VERDICT` that succeeded
on the very next round. Treating a transient failure as terminal threw away two
otherwise-complete rounds.

Across the whole run there were **five** such failures — four
`external_directory` auto-rejects and that one bare `NO VERDICT` — and all five
succeeded on a later round. None was a misconfiguration. That count is the
evidence the failure rule below rests on, and the evidence
[ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md) did
not have when it rejected retrying.

**An approval is trusted regardless of what happened after it.** Gemini approved
at a point when the document was 200 lines shorter than when the loop ended. Any
scheme that stops running a reviewer has to answer what its old approval is
still worth, and today's loop has no concept of an approval going stale.

## Decision

**Run three phases, and spend the middle one only on reviewers that are still
blocking.**

1. **Opening round — every configured reviewer.** Establishes who blocks. No
   narrowing has happened yet, so this round is the same as today's.

2. **Narrowing rounds — only the reviewers that did not approve.** A reviewer
   whose outcome was `APPROVE` is dropped for this phase. Everything else —
   `REQUEST CHANGES`, `NEEDS DISCUSSION`, `ERROR`, `NO VERDICT` — keeps the
   reviewer in the set. Findings are settled exactly as they are today; only the
   reviewer set changes. This phase ends when every remaining reviewer approves,
   when nothing is left for it to work on, or at the round cap.

3. **Confirming round — every configured reviewer again.** **Only this round can
   produce a clean approval.** An approval from phase 1 or 2 is provisional: the
   branch changed underneath it, so it is evidence about a document that no
   longer exists. This is not a formality — in the run that motivated this ADR,
   terra approved in a narrowing round and then requested changes on the very
   next look, having found a real contradiction between the cycle doc and its own
   ADR. A loop that had stopped at the narrowing approval would have reported a
   clean approval over a live defect.

**The round cap applies per phase**, not across the loop. `review.rounds`
(default 3, max 5) was written for a single-phase loop; spending one budget
across three phases would leave the narrowing phase at most one round at the
default, which is the phase that needs them.

**A failure is a non-approval, not a stop — except in the confirming round.**
It keeps the reviewer in the narrowing set and is retried on the next round. A
reviewer that fails **two consecutive rounds** is dropped from the set and named
in the terminal report as failed, so a permanently broken reviewer (a revoked
credential, a model id that no longer exists) cannot consume every remaining
round. The count is per reviewer, counts only rounds that reviewer ran, and any
other outcome resets it to zero — a reviewer that fails, then completes, then
fails again is flaky, not broken, and dropping it would throw away the opinion the
narrowing phase exists to collect. In the confirming round a failure still stops
the loop: that round's whole purpose is a complete verdict from everyone, and an
incomplete one cannot supply it.

Dropping reviewers can empty the narrowing set, and that state has to be routed
explicitly rather than left to an empty set vacuously satisfying "everyone
approved". If **any** reviewer approved during the run, the loop goes on to the
confirming round: that round runs the full configured list, so it is not empty, and
those approvals are provisional ones that phase 3 exists to re-check. If **no**
reviewer approved — every configured reviewer dropped as failed, which is exactly
what one reviewer with a dead model id produces — the loop reports instead. There
is nothing to confirm, and a round made up only of reviewers that failed twice each
cannot end anywhere but that same report, since one failure in the confirming round
stops the loop.

**Narrowing is done by editing `review.reviewers` and restoring it, and the
restore happens after every single round** — set narrowed, run, restore
immediately — rather than holding the narrowed list for a whole phase. The window
in which a user's global config differs from what they set is then one command
wide instead of the length of the loop. **The restore is conditional on the key
still holding the narrowed list.** A round takes minutes and the human is
unattended, not absent, so before writing the record back the loop confirms the key
still holds exactly what it wrote — the same two checks as the record's proof, run
against the narrowed list this time, which is exact because the loop composed that
list and wrote it verbatim itself. If it does not match, something outside the loop
wrote the key while the round ran: the value is left exactly as found and narrowing
stops for the rest of the run. Restoring blindly would revert a user's deliberate
change to their own config, which is the same objection that made narrowing fail
closed in the first place, applied to the window after the write rather than before
it.

**And the narrowing write is conditional too, on the key still holding the recorded
list.** The record is proved once, from the opening round, but the write that uses it
comes after the journal read and after every fix the previous round dispatched — the
longest stretch of wall time in the loop, and a wider window than the round the
restore's check guards. Checking only after the round is worse than insufficient there:
the loop's own write makes the key match the narrowed list, so that check passes and the
restore then writes the stale record over whatever the user had set — two silent
overwrites, reported as a success. So the key is re-checked against the record
immediately before every narrowing write, and a mismatch means no write and no further
narrowing, exactly as a mismatch after the round does.

**The config is written only when
narrowing actually drops a reviewer**, never as a routine step of every round: a
narrowing set equal to the configured list — a single-reviewer setup, or a round
in which nobody approved — runs on the config exactly as it stands. That keeps the
window off the rounds the write would gain nothing on, and it is also what makes
an unset `review.reviewers` safe. Unset means one reviewer on OpenCode's own
default model, launched with no `-m` flag, and that reviewer has no
`provider/model` string that any command could write back — but a set of one is
never narrowed, so the loop never needs one. There is consequently no
`devgeta config unset` step in the protocol: the loop never writes a state that
unsetting would be the way back from. **And the loop states how to undo the
narrowing before it performs any of it**, not only in the terminal report — see
the trade-off in Consequences for why that ordering is the whole of what prose can
guarantee here.

### What this amends in ADR-0017

Two of the rules above change decisions that
[ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md) records
as `ACCEPTED`, so this ADR **amends ADR-0017 §1 and §3** — the same relationship
[ADR-0020](ADR-0020-a-reviewer-that-reports-nothing-is-retried-once.md) has to
ADR-0017's retry alternative, recorded the same way: a pointer on both ADRs'
`Related` lines and a note at each amended section of the older one. This is an
amendment, **not** a supersession. ADR-0017 §2 (any single blocking verdict
blocks), §4 (sequential reviewers, round-start journal snapshot), §5 and §6
(ratification is the human's alone) govern unchanged, and nothing here replaces
them.

- **§1, "Bounded rounds, never a convergence condition"** reads "`review.rounds`
  defaults to **3** and is capped at **5**. The loop stops at the cap regardless of
  what the reviewers say." The cap is now per phase, so a run can take up to
  `rounds + 2` rounds — the opening and confirming rounds are one round each by
  construction, and the narrowing phase gets the configured number. `review.rounds`'s
  default and its validated 1–5 range are unchanged, and so is what §1 was written to
  rule out: the bound is still fixed before the loop starts, and there is still no
  "keep going until they agree" branch.
- **§3, "Exactly two terminal states"** puts "any reviewer process failure
  (`ERROR` / `NO VERDICT`)" in the report state and ends "`ERROR` and `NO VERDICT`
  are not retried in v1." That last sentence is what changes, and only outside the
  confirming round. The two terminal states survive, and so does the property §3
  names for them — a failure can never be mistaken for approval: a reviewer that
  failed has not approved, so it stays in the narrowing set, and a reviewer dropped
  after two consecutive failures is named as failed in the report, which is §3's
  second terminal state.

**ADR-0017 did not overlook auto-retry — it considered and rejected it, so this
decision has to answer that rejection rather than replace it.** Its "Auto-retry a
failed reviewer" alternative reads: "Rejected for v1. A retry hides the difference
between a flaky provider and a misconfigured one, and Step 0's probing found
OpenCode's error text is generic (`"Unexpected server error"`) even for an
unusable model — so a retry loop would have nothing reliable to decide on. Failures
surface by name instead." Both of its reasons still hold. What changed is a third
thing it had no data on.

- **"A retry hides the difference between a flaky provider and a misconfigured one"
  survives intact, and no evidence here contradicts it.** Nothing in this design can
  tell those two apart from a single failure, and it does not claim to. It removes
  the need to: two consecutive failures by the same reviewer end that reviewer's
  participation and put it in the report by name, so a misconfigured provider still
  "surfaces by name" exactly as ADR-0017 required — one round later than before. What
  the delay costs is one extra reviewer run; what it buys is that a flaky failure no
  longer costs the whole loop.
- **"The error text is generic, so a retry loop would have nothing reliable to decide
  on" also survives, and this rule never reads the error text.** The decision input is
  a per-reviewer count of consecutive rounds that produced no verdict — a structural
  fact about outcomes the loop already parses, not a matched string. That is the same
  move ADR-0020 made one level down, where the trigger is "no report at all" rather
  than any wording. So the thing ADR-0017 refused to build, a retry decision resting
  on provider error strings, is still not built.
- **What changed is the observed base rate, which ADR-0017 was written before anyone
  had.** It predates the loop running unattended. The run this ADR is drawn from
  produced five reviewer failures; all five succeeded on a later round, three of them
  discarded an otherwise-complete round, and none was a misconfiguration. ADR-0017's
  own 2026-08-07 revision note describes the mechanism: a headless run auto-rejects a
  permission it cannot ask a human about, writes that only to stderr, and exits 0 —
  which is a property of running unattended, not a broken provider, and therefore
  recurs. At five for five, "never retry" is not the cautious reading of ADR-0017's
  reasons; it is the rule that throws away good rounds while protecting against a case
  that did not occur.

The confirming round keeps ADR-0017 §3 verbatim, for §3's own reason: that round
exists to produce a complete verdict from every configured reviewer, and an
incomplete one cannot supply it, so a failure there stops the loop instead of being
retried.

### Rejected alternatives

**A `--models` flag on `review-run` instead of editing config.** This is the
technically better answer and it was rejected by the maintainer in favour of
keeping the change to prose alone. Worth recording plainly, because the trade is
real: `resolveReviewerRuns` (`internal/tooling/task/reviewrun.go:609`) reads
`review.reviewers` and nothing else, so with no flag the loop must mutate a
user's global config, and **any interruption — Ctrl-C, a crashed session, an
unhandled error path — leaves `review.reviewers` narrowed.** That happened during
the manual run this ADR is drawn from: the config sat narrowed to one reviewer
across several turns, and a restore attempt then corrupted it by writing the two
models as one comma-joined string, which `devgeta config get` renders
indistinguishably from the correct list. The per-round restore above narrows the
window but cannot close it. If this bites, the flag is the fix.

**What the review of this plan added to that trade, since the interruption window was
the only cost visible when it was made.** Document review produced four independent
findings that all reduce to one cause: the loop has to reach `review.reviewers` through
a config surface built for a person to read, which offers neither a machine-readable
result nor an escaped one.

- The stored list cannot be recovered from `devgeta config get` — its `", "` join makes
  one entry and two print identically — and not from the YAML either, which prose has no
  parser for. Hence the record from verdict labels, and its two-command proof.
- That YAML is also unreachable in practice: no shipped agent permission grants the
  user's config directory, and a headless run auto-rejects the read rather than
  prompting. Hence "everything comes from `devgeta` commands".
- Every command that prints a stored entry prints it raw — `get`, `review-run`'s verdict
  and progress lines, `config set`'s previous-value line — so a stored ESC or `\r` can
  rewrite the output a human reads. Hence the escaped rendering, the `sed -n l` filter,
  the control-byte refusal, and the admission that `review-run`'s own lines stay
  unprotected.
- And `config set`'s previous-value line, the loop's only view of what a write replaced,
  is an unstructured sentence whose separator is legal inside an entry, so it can be
  compared but never parsed — and cannot see a replacement whose joined form is
  unchanged. Hence a detection step that has to describe its own blind spot.

Each was answerable in prose, by failing closed or by naming a residual, and none of them
needed the flag to be answered. The count is the evidence, not any single one: a `--models`
flag removes the config write, and with it the record, the proof, both key checks, the
check-to-set gap and every one of the display rules — several of this decision's longest
paragraphs exist only because the write exists. The decision stands as the maintainer made
it, prose-only. This is the case to reopen it with if it bites.

**A record file on disk, so an interrupted run self-heals on the next one.** The
attractive version of the idea: the loop already has to record the full list, so
write that record to a fixed path, read it before doing anything, and restore from it
when it is found. The interrupted run still emits no report, but
"narrowed forever" becomes "narrowed until the next run, which fixes it and says
so". Rejected, because prose-only there is nowhere to put the file:

- `.git/devgeta/` is where the review journals live, but **both agents deny agent
  edits there** — `Edit(.git/**)` in `configs/claude/settings.json.tmpl`,
  `".git/**": "deny"` in `configs/opencode/opencode.json.tmpl`. The journals are
  written by Go, not by the agent.
- The **only** directory outside the project that either agent grants is the
  scratch root ([ADR-0015](ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md);
  see also [docs/guides/agent-permission-matching.md](../guides/agent-permission-matching.md),
  which notes that grant is what makes the `~/`-anchored deny rules moot). That
  root is a **cache**: explicitly disposable, pruned by `dg configure`, and
  allocated one unique directory per call, so there is no fixed path a later run
  could look in — ADR-0015 §3 has commands use whatever `devgeta task scratch`
  prints and never name the root themselves.
- A file in the working tree would land in the diff the loop is reviewing.
- Anywhere durable would need a Go change, which is what the maintainer's
  prose-only decision rules out.

The bookkeeping is solvable, and half of it is already done: the adopted protocol
above writes only onto a value it has just checked — the record before narrowing, the
narrowed list before restoring — so a user's deliberate change survives whether a record
file exists or not, outside the one-command gap named in Consequences below.
Storage is what is not solvable. And a
record kept in a location that may be swept is worse than none: when the record is
gone the loop restores nothing and silently adopts the narrowed list as its
baseline, while the mechanism's existence implies it did not. Half a guarantee
here reads as a whole one.

**Drop a reviewer permanently once it approves.** Cheapest, and wrong for the
reason phase 3 exists: it converts a provisional approval into a final one and
would have shipped a false clean approval in the motivating run.

**Keep failures terminal.** ADR-0017 §3's rule as written, and the status quo.
Preserves its honesty about not being able to distinguish flaky from broken.
Rejected because the evidence says these failures are overwhelmingly transient —
five for five recovered — and because the two-consecutive-failures rule keeps the
protection that mattered: a genuinely broken reviewer still gets reported as broken
rather than silently ignored. ADR-0017's stated reasons for rejecting a retry are
answered one at a time under
[What this amends in ADR-0017](#what-this-amends-in-adr-0017); this entry is the
summary, not the argument.

**Retry failures indefinitely while rounds remain.** Simplest rule, but a
reviewer that can never run burns the entire narrowing phase and still reports
nothing usable.

## Consequences

**Easier.** Rounds are spent where findings come from. A reviewer that has said
its piece stops being paid to repeat it. Transient reviewer failures — the common
kind, on the evidence — no longer discard a complete round's work. The narrowing
phase gets a real round budget instead of a fraction of a shared one.

**Harder.** The loop gains phase state on top of its round counter, and three
places where the reviewer set changes. "Which reviewers ran this round" becomes
something the terminal report has to say explicitly, because it is no longer
always "all of them". The failure rule needs a per-reviewer consecutive-failure
count, which is more bookkeeping than a single stop condition.

**Accepted trade-off.** A run costs at least two full-reviewer rounds (opening
and confirming) even when the first one is unanimous, so the cheapest possible
clean approval gets slightly more expensive. That is the price of never trusting
a stale approval, and it is small next to the six redundant rounds this removes.

**Accepted trade-off — and the one to watch.** An interrupted loop leaves
`review.reviewers` narrowed, and the user's own config is the thing at risk. The
terminal report must state the config's current value in every exit path, and beside
it exactly one of two things: the command to restore the recorded list, or the reason
there is no such command. Never neither — the property the requirement exists for is
that a human always learns whether the loop touched their key, and both branches keep
it. The second branch is not a loophole but the only consistent answer where
narrowing was refused or the key is unset: no write happened, so the value the report
just printed is the user's own and there is nothing to restore, and in the
control-byte case a paste-exact command could not be printed safely at all (see the
quoting rule below). But a report is not enough on its own, because the case
that loses the config is exactly the case that produces no report. A Ctrl-C, a
crash, or a killed session between a `config set` and its restore ends the
process, and a process that is gone prints nothing.

So the restore command is stated **twice, and the first time is before any
mutation**: as soon as the opening round's labels give the loop a record it has proved
and accepted, it prints
them with the exact `devgeta config set` that puts them back — and the opening round
runs the full configured list and writes nothing, so that print still lands before
the config is ever touched. Both prints take the reason branch together or neither
does: the refusals are decided on the record, before the first write, so a run that
prints no command up front is the same run that prints none in its report, and it is
a run that never touched the key. Step 0, earlier still, prints the `get` line — through a
filter rather than by running the `get` bare, since at that point the loop has proved
nothing about the value it is showing and a bare `get` would emit it raw itself.
So even a run that dies inside the opening round leaves the configured value on
screen. Only what is printed before the mutation survives an interruption. This does
not make the loop safe, and
the ADR should not be read as claiming it does — the loop cannot promise it never
leaves `review.reviewers` narrowed without saying so. It promises the weaker,
achievable thing: the recovery instruction is on the human's screen before the
config is ever touched.

**The residual, stated plainly.** After an interruption the config stays narrowed
until a human runs that command, and the next run of the loop records whatever
list it finds as its baseline — so a narrowing nobody undoes becomes permanent and
invisible, and no amount of prose detects it, because a narrowed list is a
perfectly valid configuration. The `--models` flag rejected above is what closes
this. If it bites, that is the fix, and this paragraph is the evidence for it.

Restoring the right list also has a precondition worth naming, because getting it
wrong is how the config was corrupted in the motivating run: **the record of what to
restore cannot be `devgeta config get`'s output split on `", "`.** `get` joins the
list with a comma and a space, and the validator accepts an entry that itself
contains a comma and a space, so one entry and two entries can print the same string
— splitting that string restores a list the user never had.

**The record is the opening round's own verdict labels, and reading the config file
is not an option.** `review-run` prints one `<label> → <outcome>` line per reviewer,
and for a configured model the label is the entry: `resolveReviewerRuns` sets a run's
label and model to the same string. So the round the loop has to run anyway tells it
the list, one entry per line, with none of `get`'s ambiguity. The alternative —
reading `review.reviewers` out of `global_config.yaml` — was rejected on two
independent grounds. It is outside the repository, and the only external directory
either shipped agent grants is the disposable scratch root, so a headless run
auto-rejects the read rather than prompting; the run this ADR is drawn from produced
four `NO VERDICT(! permission requested: external_directory ...)` outcomes doing
exactly this kind of thing. And the loop is prose with no parser, so YAML's several
spellings of the same scalar are not something it can be trusted to decode.
Everything the loop knows about the key therefore comes from `devgeta config get` and
`devgeta config set`, which are ordinary commands under both agents' `bash` policy.
Widening the permission instead would be an agent-permission change — never-silently
under CLAUDE.md §10, and required to be symmetric across both agents — for a read the
loop does not need.

**The record is proved before it is used, by two commands rather than a judgement.**
The labels are the configured list after trimming, after blank items are dropped, and
after duplicates are collapsed, so they are not automatically what is stored. The
loop checks that `get`'s output is a single line, and that the labels joined with
`", "` are byte-identical to it. That is sufficient, and the reason is worth keeping:
each of those transformations only ever removes characters, so if the labels differ
from the stored entries at all their join is strictly shorter and the comparison
fails — order included, since a join is order-sensitive. The single-line check is
what closes the one hole in that argument, an entry stored with a trailing newline
that command substitution would strip before comparing. A failed check turns
narrowing off for the whole run, exactly as an unwritable entry does, which also
means a misread label can never be acted on: a wrong parse changes the join and the
check fails.

Two rules follow from that, and both are about the user's config rather than the
loop's economy, so both win over the saving when they conflict with it.

**Narrowing fails closed.** Three things can make a config one the loop refuses to
narrow, and all of them end the same way — every reviewer every round,
`review.reviewers` never written, and the report naming what tripped it. One is a
record the checks above could not prove. Another is an entry `config set` cannot write
back: it re-validates, so an entry only reachable by hand-editing the file — one with
no `/`, or one whose only `/` is its first or last character — is rejected. The third
is an entry holding a control byte, for the reason given under the quoting rule below.
All three tests are needed, because each catches what the others cannot: a hand-edited
`noslash` entry reaches a label unchanged and only validation stops it, a hand-edited
blank item never reaches a label at all and only the join check stops it, and an entry
with an interior ESC or `\r` passes both of those — it is correctly shaped, writes back
fine, survives `TrimSpace` because it is not whitespace, and is not a newline for the
single-line check to catch. The tempting alternative
— narrow anyway, then restore the entries that validate and report the one that did
not — silently and permanently deletes that entry from a config the user owns, and
can only admit to it in a terminal report, which is precisely what the interruption
case above never reaches. Checking restorability first is also the smaller
mechanism: the loop already holds the complete list before its first write, so the
check is one pass, where the repair path is a second code path on the one edge case
nobody exercises.

Two more things stop narrowing, and unlike those three they stop it part-way instead of
before it starts: the key not holding the record when a narrowing round is about to
write, and the key not holding the narrowed list when that round returns. The
loop then writes nothing, leaves the value as found, and narrows no further, because
the record was proved against the configured list and a key rewritten underneath it
voids that proof — with no way to re-prove it, since labels only come from a round
and only the opening round runs the full list. The report names the round and which
side of it the loop noticed on, prints
what was expected beside what was found, and repeats the recorded restore command;
these are the only cases where the key ends the run holding something the loop did not
write. One cause is worth telling apart there rather than guessing: after a round, a
current value byte-identical to the record means the narrowing write never landed, so
nothing was lost.

**The residual in that guard, stated plainly.** A check and a write are two commands, so
a `devgeta config set` from another shell landing between them is still overwritten.
`internal/config/lock.go` cannot be reached from prose: its sidecar lock is taken inside
one `config.Update` call and released when that call returns, so it makes one
`config set` atomic against another and cannot span the loop's separate `get` and `set` —
and no command compares and sets in a single step, which is again the `--models` flag's
job rather than prose's. What is reduced is the silence, not the gap: `config set` prints
the value it replaced, read inside the same lock as the write, so a write that landed on
a list the loop never recorded can be named in the report and stop all further narrowing —
a third part-way stop on top of the two above, and the only one where the key still ends
holding the record.
The loop does not try to put that value back — the printed string is `get`-joined and
therefore ambiguous, and rebuilding a list from such a string is precisely how the config
was corrupted in the run this ADR is drawn from.

That line has to be read a particular way, and its detection is partial rather than
reliable — both worth stating here, because the protocol depends on the first and must not
be read as promising the second. It is printed as one unstructured sentence,
`fmt.Fprintf(out, "%s: %s -> %s\n", …)`, and `" -> "` is legal inside a reviewer entry, so
the previous value can be **compared** but never **parsed**: the loop captures the write's
output and compares the whole string against a line it composed, which cannot be forged,
where cutting at the first `" -> "` both hides real changes and invents false ones.
Capturing also keeps that value — which came from outside the loop and was screened by
nothing — off the terminal until it can go through the same filter as a displayed `get`.
What none of that reaches is the `", "` join again: a replacement that prints the same
string as the expected line is indistinguishable from no change at all, since one entry
`anthropic/a, openai/b` prints what two entries print. So this check is a chance of
noticing, not a guard, and the report says as much — silence from it is not evidence that
nobody wrote the key. Making it reliable needs a machine-readable result, which is a Go
change, and removing the need for it is the `--models` flag.

**And the count of these residuals matters more than any one of them.** Four things in
this decision cannot be closed prose-only — the interruption window, the check-to-set gap,
that gap's blind spot where the joined form is unchanged, and `review-run`'s raw labels —
and every one of them exists because narrowing goes through the user's global config.
They were found one at a time while this plan was reviewed; the `--models` entry under
Rejected alternatives now carries the list. That accumulation is the standing case for the
flag, and it is why this ADR states each residual outright instead of describing the
protocol as safe.

**Every entry is quoted wherever it reaches a command line.** The validator requires
nothing beyond an interior `/`, so spaces, `;`, `&&`, `$( )` and backticks are legal
inside a stored reviewer entry. Interpolated bare into the narrowing write, the
restore, or the restore command printed for a human to paste, such an entry splits
into two reviewers or runs as a second command. A config value read off disk is
untrusted input to a command line, and the printed command is not the lesser case —
it is the one a human runs by hand, and the only recovery an interrupted run leaves
behind.

**And every entry is escaped wherever it reaches the screen, which is a different
rule.** Quoting stops a shell; it does nothing to a terminal, because printing a
quoted string still prints its bytes. Nothing filters characters on the way in — `Set`
validates the shape and stores the argument verbatim, `yaml.v3` round-trips it exactly,
and `dg config get` prints the joined list through a bare `fmt.Fprintln` — so a stored
entry can hold ESC, `\r` or BEL, and the loop displays config values in several places:
the `get` line before the first round, the recorded entries, the expected-beside-found
pair after a mid-round change, and the current value in every report. Printed raw, an
entry like `anthropic/a\x1b[2K` blanks the line it appears on and
`anthropic/a\rall reviewers approved` overwrites it with a sentence of the entry's own
choosing — including the line whose sole job is to be the interrupted run's recovery
instruction. So every value the loop displays is made visible on the way out, by
whichever of two routes fits where the string came from: a string the loop composed is
rendered with its control bytes shown as escapes, and a value read straight out of the
config is displayed by piping that `get` through `LC_ALL=C sed -n l` rather than
running it bare — because to escape a value the loop must first have it, and it can
only get it by having `get` print it, at which point the raw bytes are already on
screen. The copies used for the proof checks are compared inside `$( )` and counted
with `| wc -l`, so a `get` is either compared or filtered and never simply emitted.

**And this promise stops at the loop's own output, which is narrower than it first
looks.** `review-run` prints each reviewer's label raw twice — the verdict line on
stdout (`internal/tooling/task/reviewrun.go:306`) and the progress lines on stderr
(`internal/tooling/task/reviewprogress.go:137`) — and both come **before** the loop
has read anything, so before the record exists and therefore before any refusal can
fire. The loop cannot escape those lines, because it did not compose them, and it
cannot filter them either: it is required to read the verdicts first-hand and
unfiltered, the stderr heartbeat is live on purpose, and any byte-level filter also
rewrites the `→` the lines are parsed on. So a config holding an ESC or a `\r` will
scramble one opening round's output whatever the prose says. That is accepted rather
than promised away: the damage is cosmetic and bounded to that round, because the
fail-closed refusal below means the config is never written, and the report that
follows is escaped and readable. Escaping at the source would mean changing what
`review-run` prints, which is a Go change the prose-only decision rules out — the same
shape of trade as the `--models` flag above, and if it bites, that is the fix.

The **one** string that cannot be escaped is the restore command a human pastes: it has
to reproduce the stored bytes exactly, so escaping it would make it write the wrong
list. That is why a control byte in a recorded entry is a fail-closed refusal rather
than a rendering problem — the loop cannot print such a command both safely and
correctly, so it never creates the need for one. That is also what settles the
apparent conflict with the report requirement above: a refused run never writes the
key, so the report takes the reason branch — the key was never written, the current
value shown is the user's own — and prints no command at all. The requirement stays
total without ever asking for a command the loop cannot print. A shell-specific quoting form
(bash/zsh `$'…\e…'`) would satisfy both at once and was rejected: a second quoting rule
on top of the first, wrong in POSIX `sh`, needed in three places, and spent on a config
that is one byte from malformed.

**Not implementable until the maintainer says so — and the status line above cannot be
that signal.** `configs/shared/commands/review-loop.md` ships into every user's other
repos, and [CLAUDE.md §10](../../CLAUDE.md) requires two things before implementation
starts: the decision recorded as an ADR (item 2), and the maintainer's approval
(item 3). The first is met by this file existing. The second is what the gate is
waiting on, and the cycle doc's Step 0 is where it is recorded.

The gate deliberately does **not** read "until this status says `ACCEPTED`", because in
this repo that condition is circular. [docs/decisions/README.md](README.md) defines
`ACCEPTED` as "Decided **and implemented**" — so the status cannot honestly reach
`ACCEPTED` until the work exists, and the work is exactly what the gate is holding
back. An implementer following a status-word gate literally would have to either
falsify the status or never start. So this ADR stays `PROPOSED` until the change ships,
and `PROPOSED` here means "decided, not yet built", not "still being argued about". The
repo's vocabulary has no better word: four ADRs in this directory whose work shipped
long ago are still `PROPOSED` (0008, 0009, 0014, 0015), so the status field has never
tracked implementation reliably in either direction. Adding a state for "decided, not
yet built" to that README's table would change what every ADR's status means and is the
maintainer's call, not this cycle's.

What the gate is, then: the maintainer's explicit go-ahead on the plan, given while this
decision stands recorded here. Only the maintainer gives it, and an unattended run never
gives it to itself. Until it lands, the work is two planning documents, and that is a
finished state to review.

**Follow-on.** This governs
[2026-08-12-review-loop-narrows-to-blocking-reviewers.md](../plans/cycles/2026-08-12-review-loop-narrows-to-blocking-reviewers.md).
It revises step 0 of `configs/shared/commands/review-loop.md` (which now keeps the
`devgeta config get review.reviewers` line the record is checked against), step 1
(which builds the record from the opening round's own verdict labels, proves it
against that line, prints its restore command before anything is narrowed, makes every
config value **it** displays visible rather than raw, narrows only while the key still
holds the list it recorded, and restores only while the key still holds the list it
wrote),
step 2 (failures were terminal), step 3 (a clean approval did not require a confirming round, and a
round that withheld approval without recording a finding always stopped the loop —
which is every failed round, so step 2's revision does nothing without this one),
and step 5 (the cap was loop-wide).

It also **amends
[ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md) §1 and
§3** — the round cap and the no-retry rule — as set out under
[What this amends in ADR-0017](#what-this-amends-in-adr-0017); that ADR now carries
a note at each of those two sections and at its "Auto-retry a failed reviewer"
alternative, so a reader of the older decision cannot follow a rule this one
replaced. ADR-0017 stays `ACCEPTED` and is not superseded: its escalation rules
(§2), its sequential-reviewers and round-snapshot rules (§4), §5, and its §6
human-only ratification rule are untouched here. Nor does this touch
[ADR-0020](ADR-0020-a-reviewer-that-reports-nothing-is-retried-once.md)'s
within-round retry, which still runs one level below and is already reflected in
the outcome the loop reads.
