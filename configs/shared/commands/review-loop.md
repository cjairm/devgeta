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
- `devgeta config set review.reviewers <entry> <entry> …` — rewrite that list. It takes
  **one argument per entry**, never one comma-joined string, and stores each argument
  verbatim: no trimming, no de-duplication, no reordering. This is the only way this
  loop ever writes the key, and step 1 below is what governs when it may.

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
stored config directly. The rule that governs every one of those reads and writes is
stated here once in full, and the flow steps below carry the mechanics that implement
it:

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

Then, still before the first round, put the configured reviewer list on screen:

```sh
devgeta config get review.reviewers | LC_ALL=C sed -n l
```

Print it through that filter. Never run the `get` bare and restate what it said: a bare
`get` prints the stored bytes with no escaping at all, so an entry holding an escape
sequence has already moved the cursor by the time the loop sees it, and there is no later
point at which the loop could take that back. `sed -n l` prints ESC as `\033`, a carriage
return as `\r`, and marks the end of the line with a `$`, so nothing raw leaves the pipe.
BSD `sed` also wraps long output at about 70 columns, inserting a trailing backslash and
a newline — that wrap is the filter's own, not a stored byte. Step 0 has run no checks
and knows nothing about the value it is showing, so the filtered
form is the only form it emits. Use `sed`, not `cat -v` — `l` is POSIX `sed`, while `cat`
is commonly a user's alias for something else entirely.

This is not the restore command. That one names the recorded entries, and those only
exist once the opening round has printed its verdict labels (step 1). Nothing is lost by
the wait: the opening round runs the configured list and writes nothing, so a run
interrupted at step 0 or inside the opening round finds a config nobody touched — with
its configured value already on the human's screen.

### 1. Run a round

Run `devgeta task review-run`, passing `--reviewer <key>` and `--note <text>` when step 0
resolved them — omit each flag entirely otherwise. Read its stdout exactly as printed: one
verdict line per reviewer, nothing else. Never guess at, soften, or invent a verdict the
line does not state. Its stderr progress lines (per reviewer, plus periodic heartbeats)
are not verdicts and carry no findings — do not read anything into them.

Run this yourself, in the main session — not in a subagent. The verdict lines are the one
thing this loop must never take second-hand, and stdout is two or three lines.

**On the opening round, those same verdict lines are the record of what to restore.**
Narrowing rewrites `review.reviewers`, and the config-restore invariant in `## Phases`
allows that only while the loop can put back exactly the list it found. What it puts back
is the **record**. Derive it, prove it, screen it and print it the moment the opening
round's verdict lines are read — before that round's journal read, and always before the
first narrowing write. Everything from here to this step's closing journal read is that
protocol; it is stated once and applies to every round.

**Nothing about the key is read off disk, and here is why**, so nobody "simplifies" it
back into a step that cannot run. The file behind `devgeta config` sits outside the
repository under review, and the only directory outside it the agent running this loop is
granted is its own disposable scratch root. A read anywhere else asks the human for
permission, and an unattended run has nobody to answer, so the request is auto-rejected —
a step that reads devgeta's stored config does not merely lack a parser, it cannot
reliably reach its first round at all.
And there is no parser: this loop is prose, and a stored value has more
legal spellings on disk than prose can be trusted to decode. Everything the loop learns
about `review.reviewers` therefore comes from `devgeta config get`, from
`devgeta config set`, and from the verdict lines themselves.

**Where the record comes from.** The opening round runs every configured reviewer and
prints one `<label> → <outcome>` line each. For a configured model that label _is_ the
entry — the same string stored in `review.reviewers` — so the labels, in the order
printed, are the record. Every reviewer prints a line whatever its outcome, `ERROR` and
`NO VERDICT` included, so a reviewer that fails the opening round still contributes its
label. A round that fails as a whole prints no verdict lines at all, so a half-record is
not a state that can exist: either the loop has every label, or it has none and narrowing
never starts.

**The `get` line is not the record, and must never be split into one.**
`devgeta config get review.reviewers` prints the list joined with a comma and a space,
and an entry only has to contain a `/` that is neither its first nor its last character —
so a comma and a space are legal _inside a single entry_. The two-entry list
`anthropic/a` + `openai/b` and the one-entry list `anthropic/a, openai/b` therefore print
exactly the same string, and rebuilding a list by splitting that string restores a list
the user never had. The labels carry no such ambiguity: they arrive one per line, one per
reviewer, already separated by the command that ran them. So the `get` line is used
**only whole**, as the string the joined labels are checked against — the one direction
the ambiguity cannot hurt, because that check asks "does this list join to that string",
never "what list did that string come from".

**One label in, one argument out.** `devgeta config set review.reviewers` takes one
argument per entry, never one comma-joined string; a joined string is written as a single
bogus model id and the next round fails with a server error. One recorded label per
printed verdict line in, one `config set` argument per recorded label out — the same
shape at both ends, which is what makes the round-trip exact once the proof below has
passed.

**Check for an unset key first, so it is never reported as a failed proof.** If
`[ -z "$(devgeta config get review.reviewers)" ]` succeeds, `review.reviewers` is unset.
That is an **empty** list — a valid configuration, not an error. An unset key runs one
reviewer on OpenCode's own default model, and that reviewer has no `provider/model`
string, so no command could write it into `review.reviewers` and none has to: a set of
one is never narrowed (a narrowing write happens only when narrowing actually drops a
reviewer), so the key stays unset for the whole run and there is nothing to restore.
Print exactly that — `review.reviewers` is unset, one reviewer ran on OpenCode's default
model, nothing will be changed — and print **no** restore command.
`devgeta config set review.reviewers` with nothing after it is not a runnable command;
zero values are rejected outright.

**Prove the record before using it.** The labels are the configured list after
surrounding whitespace is trimmed, after blank items are dropped, and after duplicates
are collapsed to their first occurrence, so they are not automatically what the user has
stored. Two commands establish that they are, and both have an answer to read rather than
a judgement to make:

1. `devgeta config get review.reviewers | wc -l` reads **1**. The output is a single
   line, so no stored entry contains a newline.
2. `[ "$(devgeta config get review.reviewers)" = '<the labels joined with ", ">' ]`
   succeeds — the joined labels are byte-identical to the `get` line, with the joined
   string single-quoted per the quoting rule below.

Neither puts the stored bytes on screen: `$( )` captures and `| wc -l` counts. Read that
together with step 0's filtered display as one rule — **a `get` is either compared or
filtered, never emitted.**

Both checks are needed, and the pair is exactly sufficient. `get` prints the stored
entries joined with `", "` and nothing else. Trimming, dropping and collapsing only ever
remove characters — collapsing a duplicate removes an entry and its separator too — so if
the labels differ from the stored entries in any way at all, their join is **strictly
shorter** than the `get` line and check 2 fails. Check 1 is what makes that argument
airtight: command substitution strips trailing newlines, so without it an entry stored as
`anthropic/a` followed by a newline would compare equal to the trimmed label. Together,
check 2 passing is the same statement as "the labels are the stored list, in order, byte
for byte" — order included, because a join is order-sensitive.

**Any mismatch turns narrowing off for the whole run.** Every round then runs the full
configured list, `review.reviewers` is never written, and the report says the record
could not be proved and shows the current `get` line through the filter defined below —
filtered rather than bare, because the values that fail this check are exactly the
malformed ones, and one of the things that fails it is an entry containing a newline.
This is the fail-closed direction on purpose. Each case it catches is real, and each is a
config the loop must not rewrite: a duplicated entry (the labels collapse it, so
restoring would silently delete one), an entry with surrounding whitespace (restoring
would silently trim it), a blank item from a hand-edit (restoring would silently drop
it), and an entry containing a newline or its own `→` (which would make the round's own
output ambiguous to read). That last pair buys something else too: a misparsed label
cannot slip through, because a wrong parse changes the join and check 2 fails. The loop
cannot narrow on a record it has not proved — which is the point, since a fail-closed run
costs money and a bad restore costs the user their configuration.

**Quote every entry — in the command the loop runs and in the command it prints.** "One
argument per entry" is only true if each entry survives the shell as one word, and a
stored reviewer entry is not tame text. The only thing required of it is a `/` that is
neither its first nor its last character, so spaces, `;`, `&&`, `$( )`, backticks, quotes
and newlines are all legal inside one, before or after the slash. Interpolated bare,
`anthropic/a b` becomes two reviewers and `anthropic/a; <command>` becomes a second
command that runs with whatever this agent's shell can do. A config value is untrusted
input to a command line, and untrusted input is validated before use. So single-quote
each entry and escape any single quote inside it the usual way (`'\''`), in **all three**
places entries appear: the narrowing write, the restore, and the restore command printed
for a human. The printed one is not the lesser case — it is the one a human pastes into
their own shell, and it is the only recovery an interrupted run leaves behind.

**Quoting protects the shell; it does nothing for the terminal.** Single quotes stop
`anthropic/a; echo INJECTED` from becoming a second command, and stop nothing about an
entry ending in an "erase line" escape sequence: the ESC bytes reach the terminal either
way, because printing a quoted string still prints its bytes. A stored entry can hold
them — `devgeta config set` filters no characters, the stored form round-trips them
exactly, and `get` prints the joined list with no escaping. The proof above does not
catch them either: ESC, BEL and `\x7f` are not whitespace, so trimming leaves them, and
`wc -l` counts only newlines. So an entry ending in an erase-line sequence, or one
holding a carriage return followed by `all reviewers approved`, is a **proved** record
whose raw display erases or overwrites the line it was printed on — including the line
whose whole job is to be an interrupted run's only recovery, and the expected-beside-found
pair that is the entire diagnosis of a mid-round change. A config value is untrusted
input to the report as much as to the command line. So **every value this loop displays
goes there visible instead of raw**, by whichever of two routes fits where the string
came from.

**The escaped rendering** is the route for a value the loop composed. Print the value
inside backticks, and before printing replace every byte that is not printable ASCII
(`0x20` through `0x7e`) **and every backtick (`0x60`)** with a visible escape: `\t`,
`\n`, `\r`, `\e` for ESC (`0x1b`), and `\xNN` with two lowercase hex digits for
anything else — a backtick prints as `\x60`. A backtick is printable, but it is also
the delimiter of the span the value is printed in, so leaving it literal lets the
value close that span early and format the rest of the line as markdown. A value
holding a newline
therefore prints on one line as `` `anthropic/a\nopenai/b` ``, and a value holding ESC
prints the two letters `\e` instead of moving the cursor. Non-ASCII bytes are escaped per
byte too, so a model id with a non-ASCII character displays as hex — ugly, and
deliberate: printable non-ASCII is where bidirectional overrides and zero-width
characters live, and the line a human reads to decide whether their config was mangled is
the wrong place to render those faithfully. Two honest limits: this is an instruction, so
it binds only as far as the rest of this file does, and it protects the terminal the
report is read in, not a downstream viewer that re-interprets `\e` itself.

Apply the escaped rendering to every value the loop **composes**: the recorded entries
named before the first narrowing write, the expected string in any mismatch, and the
entry that turned narrowing off. The one exception is the copy-paste restore command
below, whose entries stay literal.

**The filter** is the route for a value that comes straight out of
`devgeta config get`. Such a value is not composed, and the escaped rendering cannot
reach it — to escape a value the loop has to hold it, and it can only get it by having
the command print it, at which point the raw bytes are already on screen. That is the
whole limit of the rule above. So never run that `get` bare when its output is going to
be seen; display it through `devgeta config get review.reviewers | LC_ALL=C sed -n l`
instead, which prints ESC as `\033` and a carriage return as `\r` and marks the end of
the line with `$`, so nothing raw leaves the pipe. Four displays take this route: the
`get` line at step 0, the found value in a mid-round mismatch, the current value in both
report templates, and the value `devgeta config set` reports it replaced — which is a
config value the loop did not compose either, captured first and then filtered (below).
The proof checks take neither route, because `$( )` and `| wc -l` never put the stored
bytes on screen.

**What neither route covers.** `devgeta task review-run`
prints each reviewer's label raw on stdout and again in its stderr progress lines, and
the loop can neither escape those (it did not compose them) nor filter them (it must read
the verdicts first-hand, and a byte-level filter rewrites the `→` they are read on). They
also arrive **before** the record exists, so the control-byte refusal below cannot get
ahead of them. A config holding an ESC therefore scrambles the opening round's own output
for one round on its way to being refused. What these rules buy is the narrower thing: a
stored entry cannot scramble the loop's **own** report, which is the artifact a human
reads to decide anything. Do not read the rule as covering everything printed during a
run. `devgeta config set`'s own `<key>: <previous> -> <new>` line is **not** in this
category and must not be filed with it — nothing reads it live and nothing parses a `→`
out of it, so it is capturable and takes the filter route.

**The copy-paste restore command stays literal, so a control byte in a recorded entry
turns narrowing off for the whole run.** The printed
`devgeta config set review.reviewers 'a' 'b'` is not a value being displayed; it is a
command a human pastes, so escaping its entries would make it write the wrong list. It
has to keep the single quoting above **and** the exact bytes. Those two requirements
cannot both hold for an entry containing a control byte: literal bytes let the entry
rewrite the recovery line, and escaped bytes make the paste wrong. So the loop does not
choose between them. If any recorded entry contains a byte below `0x20`, or `0x7f`,
**narrowing is off for the whole run** — the same branch as an unwritable entry and an
unprovable record: full configured list every round, `review.reviewers` never written,
and the report naming the offending entry in the escaped rendering. Nothing then needs a
paste-exact command carrying a raw ESC, because there is nothing to restore.

Note what the trigger is and is not: control bytes only. A non-ASCII entry is displayed
escaped but narrows normally, because it cannot move a cursor and it pastes back
correctly.

A second quoting form does not fix this, and was already considered rather than
overlooked. `bash` and `zsh` write `$'anthropic/a\e[2K'`, which is both printable and
paste-exact. It was rejected: it is a second quoting rule layered on the single-quoting
one, correct in neither POSIX `sh` nor every place the entries appear, and it buys extra
rounds on a config that is one byte away from malformed. Refusing is the smaller rule. If
someone genuinely stores an ESC in a model id, that is the door.

**Check the record is restorable before narrowing anything, and refuse to narrow if it is
not.** `devgeta config set` re-validates every value, so an entry only reachable by
hand-editing devgeta's stored config cannot be written back at all: an item with no `/`,
and one whose only `/` is its first or last character, are both rejected. If the record
holds one of those, **narrowing is off for the whole run** — full configured list every
round, `review.reviewers` never written, and the report naming the entry that turned
narrowing off.

Three screens reach that one branch, and all three are needed, because each catches
something the other two cannot:

- A hand-edited entry with **no slash** survives to a label unchanged, so the join check
  passes and only this restorability check stops it.
- A hand-edited **blank** item is the mirror image: it never reaches a label at all, so
  this check never sees it and only the join check stops it.
- An entry holding an **interior ESC or carriage return** passes both of those — it is
  shaped like `provider/model`, `devgeta config set` writes it back happily, trimming
  does not touch a non-whitespace control byte, and `wc -l` counts only newlines — so
  only the control-byte check stops it.

Three refusals, one effect: narrowing off for the whole run. The loop needs one branch,
but it needs all three tests to reach it.

They fail closed on purpose. Do **not** narrow first and then restore the entries that
happen to validate: that writes a list the user never had, permanently drops the entry
that failed, and can only own up to it in a terminal report — which is exactly the thing
an interrupted run never reaches. Verifying first is also the smaller rule, and it is
always possible: the loop holds the complete record before its first write, because a
narrowing write only happens once narrowing drops a reviewer, and that cannot happen
before the opening round has printed every label. The checks therefore have everything
they need before the config is touched, and they cost one pass over two or three strings
plus one string comparison. A repair path, by contrast, is a second mechanism with its
own failure modes on the exact edge case nobody exercises. What refusing gives up is the
money saved on a config that cannot be restored — the right trade, because the config is
the user's, and a run that saves money by mangling it has not saved anything.

**Then print the record and the restore command — after every refusal check above, and
before the first narrowing write.** One line naming the recorded entries **in the escaped
rendering**, and the exact `devgeta config set review.reviewers <entry> <entry> …` that
puts them back, its entries single-quoted and **literal** because it is pasted rather
than read. The two lines can look different without contradicting each other, and by the
control-byte rule above they only ever differ for a non-ASCII entry.

Print it here rather than only in the terminal report, and know why: this is the one piece
of the restore story that survives an interruption. A session killed between a
`devgeta config set` and its restore emits no terminal report at all, so anything stated
only in the report is lost, while anything printed before the first mutation is already
on the human's screen.

The order matters the other way too. When any refusal above fires, the loop prints **no
restore command at all** — it prints the refusal and the offending value in the escaped
rendering, and nothing else. Nothing is lost by that: a refused run never writes the key,
so there is nothing to restore and no interruption window to leave a recovery line for.

**Narrow onto the recorded list and nothing else: re-check the key immediately before
every narrowing write.** The record is proved once, from the opening round's labels. The
write that uses it comes later, and not by seconds — between the proof and the next
narrowing write sit the journal read, the triage of every open finding, and every fix
subagent the round dispatched, which is where this loop spends nearly all of its wall
time. So this window is **wider** than the one during a round, not narrower, and it
reopens before every narrowing write rather than only the first. Guard it with the same
two commands, run against the **record**:
`devgeta config get review.reviewers | wc -l` reads 1, and
`[ "$(devgeta config get review.reviewers)" = '<the record joined with ", ">' ]`
succeeds. Both passing means the key still holds the list the loop recorded, so the
narrowed list overwrites nothing the user added and the restore afterwards puts back what
is actually there. Either failing means something outside the loop changed the key: **do
not write, and do not narrow again for the rest of the run.**

Here is what that prevents, because the failure is not obvious and it is the whole reason
a check after the round does not cover it. Without a pre-write check, a
`devgeta config set review.reviewers …` typed in the human's own shell during that window
is overwritten by the narrowing write. The key then holds exactly the narrowed list the
loop wrote, so the post-round check **passes** — and the restore writes the stale record
over the user's new list. Two writes the user never asked for, and the check meant to
catch one of them reports success.

Three key-state checks exist, and each covers a window no other one does. Do not drop one
as a duplicate of another:

- The **pre-write** check covers the gap between rounds: the journal read and the fixes.
- The **post-round** check covers the gap during a round, while the reviewers run.
- The **post-restore** verification covers neither. It catches a restore that did not land
  at all, and it is the only check the last round's restore has after it, since no later
  write follows.

The write's own report of what it replaced, below, is a fourth reading of the key, and it
covers the one gap no check can — the command between the pre-write check and the write
itself. What does **not** repeat is the rest of the proof: the record is derived once and
never again, so restorability and the control-byte screen are properties of the record
rather than of the key, and re-running them would answer a question that cannot have
changed.

**What the pre-write check cannot close: one command's worth of race.** The check and the
write are two commands, and no instruction can fuse them into one, so a
`devgeta config set` from another shell landing between them is still overwritten.
Devgeta's own locking does not reach this — it makes one `set` atomic against another
`set` and is released when that command returns, so it cannot span the loop's separate
`get` and `set`, and no command compares and sets in a single step. What the loop gets
instead of total silence is a partial detection, partial in a way the next paragraph
spells out and the report has to admit. `devgeta config set` prints
`<key>: <previous> -> <new>`, reading that previous value under the same lock as the
write, so it is exactly the value the write replaced. Read it the only way that line can
be read soundly: **capture the write's stdout and compare the whole string, byte for
byte, against a line the loop composes itself** — `<key>: <what the check just confirmed>
-> <what this write set>`, the confirmed value being the record's join before a narrowing
write and the narrowed list before a restore, since both writes have the same
one-command gap in front of them. The write, the comparison and any display of the result
are **one** shell command, because a captured variable does not survive to the next one:

```sh
out=$(devgeta config set review.reviewers 'anthropic/a') || exit 1
if [ "$out" = 'review.reviewers: anthropic/a, openai/b -> anthropic/a' ]; then
  echo 'replaced the recorded list, as expected'
else
  printf 'replaced something else: '; printf '%s\n' "$out" | LC_ALL=C sed -n l
fi
```

Two things must never be done to that string, and each has a concrete failure. **Never
split a previous value out of it** by cutting at the first `" -> "`: an entry may legally
contain `" -> "`, so that read both hides a real change (a concurrent single entry
`anthropic/a, openai/b -> anthropic/a` cuts to exactly the record's join) and invents a
false one (a record whose own entry is `anthropic/a -> b` cuts to `anthropic/a`, which
matches nothing). The whole-string comparison is right in both directions, and cannot be
forged, because the previous value is the only free field in the line. **And never print
it bare** — capture it as above. The replaced value came from outside the loop, so it may
hold a control byte the record's refusal never screened. Capturing loses nothing: unlike
the verdict lines this output is not read live and has no `→` a filter would mangle, a
validation error still lands on stderr and exits non-zero, and no expected previous value
ever carries the ` (default)` suffix `devgeta config set` adds for an unset key — the
state both checks have just ruled out.

On a mismatch, the write landed on a value the loop never recorded. The end state is the
same from either write: the key holds the **record** — a narrowing write's round runs on
and its normal post-round restore puts the record back, and a restore write has already
put it back. Narrowing stops from there, and the report names the recorded list in the
escaped rendering, since the loop composed that one, beside the replaced value put
through the filter. Leaving the key narrowed instead would be strictly worse, which is
why the round is not aborted. And the loop must **not** try to put the replaced value
back: that printed string is joined exactly the way `get`'s is, so it carries the same
one-entry-or-two ambiguity the record's proof exists to avoid, and rebuilding a list from
a joined string is the exact mistake this whole protocol is built to prevent. Detection
and an honest report, not a repair.

**Exactly how much that detection is worth, since one part of it does not work.**
The comparison catches every replaced value whose bytes differ from the expected line — a
different list, a re-ordered one, one carrying an ESC or a carriage return; all of them
simply fail the compare. What it cannot tell apart is a replaced value whose join is
byte-identical to the expected string but came from a different list: the one entry
`anthropic/a, openai/b` prints exactly what the two entries print, so a user's edit to
that value inside the one-command gap is overwritten and the check reports success. That
is the same `", "` ambiguity that makes `get` unusable as a record — it is why the record
comes from labels — and it cannot be closed from either side by prose, because every
reading of the key the loop has is joined. So the honest statement of this rule is
narrower than "the loop detects a write it did not expect": it detects one whose printed
form differs, and the gap itself stays open. What prose does close here, and what makes
this different from the raw verdict lines, is the display: capturing the output means a
control byte in the replaced value cannot rewrite the line reporting it.

**Restore onto the narrowed list and nothing else: check before writing, and never
overwrite a change the loop did not make.** A round takes minutes, and nothing stops the
human — nominally unattended, not absent — from running
`devgeta config set review.reviewers …` in another shell while one runs. So before
writing the record back, run the same two checks against the **narrowed** list instead of
the record: the `get` output is one line, and it is byte-identical to the narrowed
entries joined with `", "`. Both passing means the key still holds exactly what the loop
wrote, so writing the record back restores the user's own list and touches nothing else.
Either failing means something other than the loop wrote the key during the round: **do
not write.** Leave the value exactly as found, and narrowing is off for the rest of the
run.

That comparison is exact even though `get` is lossy in general, because the loop is never
inferring a list from the string. The narrowed list is one it composed from the proved
record and wrote itself, and `devgeta config set` stores every argument verbatim, so the
loop can compute the exact bytes `get` must print. The check asks "does the current value
equal this string I already hold", never "what list did this string come from" — the one
direction the ambiguity cannot hurt. The single-line check earns its place here too:
command substitution strips trailing newlines, so without it a value replaced by an entry
ending in a newline would compare equal to the narrowed list and be overwritten.

A mismatch stops narrowing for the **whole rest of the run**, not just this one restore —
do not soften it into a retry. It is the same fail-closed branch as an unprovable record,
for the same reason: the record was proved by matching the labels against the configured
list, and a key something else has rewritten voids that proof, so every later narrowing
write would be composed from a stale record and every later restore would clobber the new
value. There is nothing to re-prove it with, either — labels only arrive from a round,
and only the opening round runs the full list. From that point every round runs the key
exactly as it now stands, `review.reviewers` is never written again, and the report
carries that fact.

Report the state, rather than guessing at its cause. The loop normally cannot know _who_
wrote the key, so it reports what it expected and what it found, each by the route that
string's origin calls for: the expected value is one the loop composed, so it goes in the
escaped rendering, and the found value comes out of `get`, so it is displayed through the
filter and never by running that `get` bare — it is whatever some other process wrote,
which makes it the least trustworthy string in the whole report. Say also that the loop
wrote nothing. One cause is cheap enough to tell apart that it is worth naming rather than
guessing at: if the current value is byte-identical to the **record's** join, the narrowing
write never landed, so the config already holds the recorded list and nothing was lost —
say that, rather than reporting a change the user did not make. Narrowing still stops,
because a write that does not land is not a state to keep narrowing on.

**Verify every restore that does happen** by re-running the two checks against the record:
the `get` output is still one line, and still byte-identical to the joined record. A match
means the config holds the record. A mismatch means the restore did not land, which is
worth catching for the same reason the pre-write check exists — it is a state only a human
can fix, and the restore command already printed above is what fixes it.

**The order a narrowing round runs in.** When this round narrows — the conditions are
below, and they are not met on every round — run these six steps in this order, and do not
reorder them:

1. **Check.** The key still holds the recorded list: the two checks above, run against the
   record. If either fails, this round does not narrow at all — go straight to step 3 and
   run on the config as it stands, with narrowing off for the rest of the run.
2. **Narrow.** `devgeta config set review.reviewers` with one single-quoted argument per
   still-blocking entry, captured and compared as above.
3. **Run.** `devgeta task review-run`, with whatever step 0 resolved.
4. **Check again.** The key still holds the narrowed list: the same two checks, run against
   the narrowed list this time. If either fails, do not restore — leave the value exactly
   as found, go to step 6, and narrowing is off for the rest of the run.
5. **Restore, immediately.** `devgeta config set review.reviewers` with one single-quoted
   argument per recorded entry, captured and compared the same way, then verified.
6. **Then read the journal.**

Both writes pass one quoted argument per entry. Nothing in this sequence interpolates an
entry bare.

**Why the restore comes before the journal read, and the check before the write.** They are
the same fact from opposite ends. The journal read is where this loop starts spending time
— the triage after it, and the fix subagent after that, are most of a round's wall clock —
so a config left narrowed across it stays narrowed far longer than the round it was
narrowed for, and an interruption anywhere in that stretch leaves the user narrowed. The
pre-write check is the mirror image: the gap between rounds is the widest window the key can
change in, wider than a round itself, which is why that check runs immediately before the
write rather than once after the opening round.

**Both checks are conditions on the write beside them, not formalities recorded
afterwards.** Neither failure branch is "carry on anyway". If the key no longer holds the
**recorded** list before the write, the loop does not narrow this round at all. If it no
longer holds the **narrowed** list after the round, the loop does not restore. Either way
the value is left exactly as found, and narrowing stops for the rest of the run. These are
the only two points in a round that can end with `review.reviewers` holding something the
loop did not write, which is why the report names both.

**The loop writes the key only when narrowing actually drops a reviewer.** The narrowing
set has to be a **strict subset** of the recorded list, **and** every check above has to
have passed:

- the record proved — the `get` output one line, and byte-identical to the labels joined
  with `", "`;
- every recorded entry one `devgeta config set` can write back;
- every recorded entry free of control bytes;
- no earlier round having ended with the key holding something other than the narrowed list
  it was given;
- no earlier narrowing write having reported replacing a value the loop never recorded;
- and the key still holding the recorded list when checked immediately before this write.

Otherwise the round runs on the config exactly as the user left it: no write, no restore.
Eight cases land there, each for its own reason.

- **A one-reviewer set, configured or the unset default.** A single reviewer that withheld
  approval is already the whole set, so there is nothing to drop. This is what makes an
  unset key harmless rather than a hole: the loop never reaches for a `provider/model`
  string that does not exist.
- **A round in which every reviewer withheld approval.** The narrowing set is the full list
  again, so the write would set the config to what it already holds.
- **A recorded list holding an entry `devgeta config set` cannot write back.** Narrowing is
  off for the entire run, so every round is a full-list round.
- **A record the checks could not prove.** Same destination and the same reason: the loop
  will not write a list it cannot show is the one the user had.
- **A recorded list holding an entry with a control byte.** Also off for the entire run: the
  restore command for such a list cannot be both pasteable and safe to print, so the loop
  never creates the need for one.
- **A round whose pre-write check found the key no longer holding the record.** Narrowing is
  off from that round onward. Nothing was overwritten, because the check runs before the
  write.
- **A round that returned with the key no longer holding the narrowed list.** Narrowing is
  off from that round onward, so every later round is a full-list round.
- **A round whose own write reported replacing a value the loop never recorded.** The
  one-command race no check can close: the previous value `devgeta config set` reported was
  not what the check immediately before it had just confirmed, so the write landed on a
  change made in between. Narrowing is off from that round onward. Unlike the two above,
  this one ends with the key holding the **record** — the round's restore still runs, or has
  already run — because leaving it narrowed would be strictly worse; but the value that was
  replaced is gone and cannot be reconstructed, which is why the report names it.

Those last three are the only ones of the eight that stop narrowing part-way through a run
rather than before it starts; the other five refuse before the first write. The first two of
the three are also the only cases in the whole protocol that leave the key holding a value
the loop did not write, and what separates them is only when the change was noticed: before
the loop overwrote anything, or after the round it ran alongside. The report names all
three.

A round that does not narrow also has no interruption window at all, because it never
touches the key. That window opens only on the rounds that drop someone.

**A write that exits non-zero did not happen, and it is not the end of the run.**
`devgeta config set` validates before it stores anything and leaves the key untouched when
it refuses, so a failed write changes nothing. That is what the `|| exit 1` in the
captured-write snippet above ends: the one shell command, so the comparison never runs
against output the command never produced. It does not end the loop — never read it as an
instruction to abandon the run. Treat it as a refusal: the loop does not narrow this round,
narrowing is off for the rest of the run, and the report names the write that failed along
with whatever `devgeta config set` put on stderr. That is the same branch as the refusals
above — full configured list every round from here, `review.reviewers` never written again.
A rejected value should be unreachable by this point, since every recorded entry was
screened for writability before the first narrowing write; this is the branch for a write
that fails anyway. One thing to state rather than leave to be worked out: if it was the
**restore** that failed, the key is still holding the narrowed list, so say that outright in
the report — the restore command printed before the first write is then the only way back.

**Never narrow to an empty set by clearing the key.** `devgeta config set review.reviewers`
with no values after it is rejected outright, and `devgeta config unset review.reviewers` —
the command that error points at — means "one reviewer on OpenCode's own default model", not
"run nobody". The loop needs neither: with the strict-subset condition above it never writes
a state that a `config unset` would be the way back from, so there is no `config unset` step
anywhere in this protocol.

**Then read the journal.** Run `devgeta task review-notes` to see what this round left
open. `review-run` does not print ids, so this is how the loop learns them: every id under
the journal's `open:` section is an unanswered finding, and "nothing under `open:`" (or the
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
