# Cycle: `/review-loop` narrows to the reviewers still blocking

**Date:** 2026-08-12
**Estimated Duration:** ~3 hours
**Status:** Draft

---

## 1. Domain Context

`/review-loop` (`configs/shared/commands/review-loop.md`) drives an unattended
review cycle: it calls `devgeta task review-run` round after round, settles each
finding a reviewer opens, and stops at either a clean approval or a report to the
human. Every round runs every model listed in `review.reviewers`.

Four consecutive runs against one branch showed that this wastes most of its
money when reviewers disagree in a stable way. One reviewer produced all 21
findings across 9 rounds; the other approved all 7 rounds it completed and never
opened a single finding, while still being paid ~$0.4 a round to repeat itself.
Three further rounds were thrown away entirely because a transient reviewer
failure is currently terminal.

The design is governed by
[ADR-0026](../../decisions/ADR-0026-the-review-loop-narrows-to-the-reviewers-still-blocking.md)
— **read it first.** It carries the verdict tallies, the three-phase decision, the
per-phase round cap, the revised failure rule, and four rejected alternatives
including the `--models` flag that would have avoided touching user config at all.

This is a change to a file under `configs/shared/`, which ships to **every user's
other repos**, so nothing here may carry a devgeta-specific decision
([CLAUDE.md §12](../../../CLAUDE.md)). The narrowing protocol is general: it is
about reviewer verdicts, not about this repo.

---

## 2. Engineer Context

**Relevant files:**

- `configs/shared/commands/review-loop.md` — the loop's prose, 379 lines. Steps to
  change: `### 0` (the `config get` line the record is checked against — compared
  inside `$( )`, displayed only through a filter, never run bare), `### 1` (which
  reviewers run this round — and, on the opening round,
  where the record itself comes from: its own verdict labels, proved against that
  `get` line, printed with the restore command before anything is narrowed, refused
  outright unless every entry can be written back, narrowed only while the key still
  holds the record, and put back only while the key still holds the narrowed list),
  `### 2` (failures stop the loop — being revised), `### 3` (clean
  approval now requires the confirming round, and a round that recorded no finding
  is routed by whether it withheld approval or failed), `### 5` (round cap becomes
  per-phase), and the `## Terminal report` templates (must state the config's
  current value, displayed through the `sed -n l` filter, beside a literal restore
  command).
- `internal/apps/opencode/permissions_test.go` — **four guard tests already read
  this file and assert on exact substrings.** Read `readSharedCommand` and
  `markdownSection` (around :1250) before editing anything, and note that
  `markdownSection` cuts a section at the next line beginning with `#`.
- `internal/tooling/task/reviewrun.go:609` — `resolveReviewerRuns`, which reads
  `review.reviewers` and nothing else. This is why narrowing has to go through
  config; no code change is in scope (ADR-0026's rejected alternatives). Read it
  together with `:306` (`line := fmt.Sprintf("%s → %s", run.label, outcome)`) and
  the doc comment at `:201` ("Branch mode's line stays exactly `<label> →
<outcome>`"): for a configured model the label **is** the entry, `run.label` and
  `run.model` being the same string, so the round's own stdout carries the list.
  What it carries is the list after `strings.TrimSpace`, after blank items are
  dropped, and after duplicates are collapsed to their first occurrence — which is
  why Step 2 proves the record rather than trusting it.
- `cmd/config_settings.go:338-358` — `review.reviewers`'s `Get` is
  `strings.Join(list, ", ")` and its `Set` stores every argument **verbatim**, no
  trim and no dedup. Both halves matter to Step 2: the join is what makes `get`
  alone unusable as a record, and the verbatim write is what makes an exact restore
  possible once the record is known to be exact. "Verbatim" is literal, and it is
  the reason Step 2 has a rendering rule as well as a quoting rule: `Set` applies
  no character filtering at all beyond `isProviderModelShaped`, so an entry may
  hold ESC (`0x1b`), `\r`, BEL, `\x7f` — any byte but `NUL`, which no argument can
  carry. `yaml.v3` round-trips those exactly (as `"\e[31m"`, `"\r"`, `"\a"`), and
  `dg config get` prints the joined value through a bare `fmt.Fprintln`
  (`cmd/config.go:184`) with no escaping, so whatever is stored reaches the
  terminal as raw bytes. Step 2's two proof checks do not stop this: ESC, BEL and
  `\x7f` are not whitespace so `strings.TrimSpace` never touches them, and `wc -l`
  counts only `\n` — so an entry like `anthropic/a\x1b[2K` or
  `anthropic/a\rall reviewers approved` is a **proved** record whose display can
  clear or overwrite the line it is printed on.
- **Who prints a stored entry, and who prints it first.** Four writers put a
  stored `review.reviewers` entry on the terminal and **none of them is the loop**:
  `devgeta config get` prints the joined value through a bare `fmt.Fprintln`
  (`cmd/config.go:184`); `review-run` prints each verdict line as
  `fmt.Sprintf("%s → %s", run.label, outcome)` on stdout
  (`internal/tooling/task/reviewrun.go:306`); its progress writer prints
  `[i/n] <label>: running` plus a closing line on **stderr**
  (`internal/tooling/task/reviewprogress.go:137`, writer chosen at
  `reviewrun.go:440` — `os.Stderr` unless a Go caller overrides `ProgressOut`,
  which no flag does); and `devgeta config set` prints
  `<key>: <previous> -> <new>` (`cmd/config.go:245`). None of the four escapes
  anything. This is the hard bound
  on what Step 2's escaping rule can promise: **the loop can escape strings it
  composes, and it cannot escape output another process already wrote.** The
  verdict lines are the worst of it, on two counts. They arrive _before_ the record
  exists, so they are on screen before any check could refuse the config — a
  control-byte config still costs one scrambled opening round. And they cannot be
  filtered: the loop is required to read them first-hand and unfiltered
  (`configs/shared/commands/review-loop.md:86-87`), stderr is deliberately live for
  the heartbeat (`:29-31`), and any byte-level sanitizer also rewrites the `→` the
  contract is parsed on. Closing this would mean changing what `review-run` prints,
  which is a Go change (§4, Out of Scope).
- **A `get` whose output is _displayed_ can be filtered, and that is the one leak
  prose closes.** `devgeta config get review.reviewers | LC_ALL=C sed -n l` prints
  the value in `sed`'s visually unambiguous form — ESC as `\033`, `\r` as `\r`, a
  trailing `$` — so the raw bytes never leave the pipe. Verified on this machine:
  `printf 'anthropic/a\033[2K, openai/b\rall reviewers approved\n' | LC_ALL=C sed -n l`
  prints `anthropic/a\033[2K, openai/b\rall reviewers approved$` and moves no
  cursor. Use `sed`, not `cat -v`: `l` is POSIX `sed`, while `cat` is commonly a
  user alias (it is `bat` on this machine, where `cat -v` fails outright with
  `unexpected argument '-v'`). Accept two cosmetic limits — BSD `sed` wraps at 60
  columns with a trailing `\` and rejects GNU's `l 0`, and non-ASCII bytes come out
  as octal — because this is a display, never a value anything compares against.
  The copies used for the proof checks stay inside `$( )` and `| wc -l`, which
  print nothing.
- **What `config set` locks, and what its output reports.** `config set` runs its
  load-mutate-save inside `config.Update` (`cmd/config.go:212`), which holds a
  sidecar lock for exactly that cycle (`internal/config/lock.go`) and releases it
  when that call returns, then prints `<key>: <previous> -> <new>` with the previous
  value read **inside that same lock** (`cmd/config.go:245`). Two consequences, and
  they pull in opposite directions. The lock cannot be held across the loop's
  separate `get` and `set`, and no command compares-and-sets in one step, so prose
  cannot make "check the key, then write it" atomic — that gap is a residual to
  state, not something to design around (§3, §7). But the previous-value line is a
  truthful report of what the write replaced, so a narrowing write that landed on a
  list the loop never recorded is often detectable immediately after the fact. Two
  limits, and both come from the same place: that value is printed `get`-joined, so it
  must never be split back into a list, and a replacement whose join prints the same
  string as the expected one is not detectable at all — one entry
  `anthropic/a, openai/b` prints what two entries print (§3 states this residual).
- **How that line may be read, which the format string decides:**
  `fmt.Fprintf(out, "%s: %s -> %s\n", key, previousDisplay, newDisplay)`
  (`cmd/config.go:245`). Two properties follow, and the protocol depends on both.
  It is **unstructured** — the stored join is spliced into a sentence whose own
  separator is `" -> "`, and an entry may legally contain `" -> "`, since the validator
  requires nothing but an interior `/`. So the previous value cannot be recovered by
  splitting on that separator, and it fails in both directions: a concurrent single
  entry `anthropic/a, openai/b -> anthropic/a` makes the text between
  `review.reviewers: ` and the first `" -> "` equal the record's join, hiding a change
  the check exists to catch, while a record whose own entry is `anthropic/a -> b`
  makes that same read differ from the record it actually matches, inventing a
  change that never happened. Only a byte-exact comparison of the **whole** output
  against a line the loop composed is sound, and it cannot be forged: the previous
  value is the one free field, so `<key>: P -> N` equals `<key>: R -> N` only when
  `P` is `R` byte for byte. It is also **capturable**, which is what separates it
  from `review-run`'s lines: nothing needs this output live and nothing parses a `→`
  out of it, so `out=$(devgeta config set …)` still performs the write and keeps its
  bytes off the terminal. The capture loses nothing — a validator error goes to
  stderr through cobra and exits non-zero, and `config.Update` prints nothing itself
  — and whatever the loop shows of the captured string goes through the same
  `LC_ALL=C sed -n l` filter as a displayed `get`. One shell-mechanics consequence
  worth stating because it constrains the wording of the step: a shell variable does
  not survive to the next command, so the write, the comparison, and any display of
  the result are one command, not three.

**The loop may not read the config file, and this is not negotiable prose-side.**
`~/.config/devgeta/global_config.yaml` is outside the repository, and the only
external directory either shipped agent grants is the disposable scratch root —
`"external_directory": { {{.ScratchDirGlob}}: "allow" }` in
`configs/opencode/opencode.json.tmpl`, `"additionalDirectories": [{{.ScratchDir}}]`
in `configs/claude/settings.json.tmpl`. A read outside that prompts, and in a
headless run it auto-rejects: the run this cycle is drawn from produced four
`NO VERDICT(! permission requested: external_directory (...); auto-rejecting)`
outcomes from reviewers doing exactly this. So a step that reads the YAML does not
merely lack a parser — it cannot reliably reach its first round at all. Widening
the grant is an agent-permission change, which [CLAUDE.md §10](../../../CLAUDE.md)
lists as never-silently and `internal/apps/opencode/permissions_test.go` requires
be symmetric across both agents; it is out of scope here. **Everything the loop
learns about `review.reviewers` therefore comes from `devgeta` commands it already
runs.**

- `docs/decisions/ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md`
  — **ADR-0026 amends its §1 (the round cap) and its §3 (`ERROR` / `NO VERDICT`
  never retried), and this cycle already carries the pointers that say so**: a note
  at each of those two sections, a note at its "Auto-retry a failed reviewer"
  alternative, and ADR-0026 on its `Related` line. That is the whole of the edit to
  it — it stays `ACCEPTED` and is not superseded, since only two of its sections
  change. Read it for the two rules the new loop replaces, and for §6's human-only
  ratification rule, which one of the guard tests enforces structurally and this
  cycle must not weaken.

**The four anchors that must survive, or the build breaks:**

| Anchor                                                  | Must contain                                     | Guard test                                                                |
| ------------------------------------------------------- | ------------------------------------------------ | ------------------------------------------------------------------------- |
| `### 0. Resolve the reviewer selector` (heading prefix) | `$ARGUMENTS`, `--note`                           | `TestReviewLoopForwardsReviewerSelector`, `TestReviewLoopForwardsTheNote` |
| `### 1. Run a round`                                    | `--reviewer <key>`, `--note <text>`              | same two                                                                  |
| `### 3. Check for clean approval`                       | `open:`, `nothing under`, `APPROVE`              | `TestReviewLoopCleanApprovalRequiresNothingOpen`                          |
| `## Terminal report`                                    | `--ratify` / `--reopen` appear **only after** it | `TestReviewLoopOnlyInvokesRatifyOrReopenInTheReport`                      |

**Test command.** `configs/` changes are covered by the root package, which is
the slow one (~4.8 min) — CLAUDE.md §6 says to include it exactly when
`configs/` or a hook script changes, which is the case here:

```bash
go test . ./internal/apps/opencode/ ./internal/apps/claude/
make lint
```

---

## 3. Objective

`/review-loop` spends its narrowing rounds only on reviewers that have not
approved, retries a transient reviewer failure instead of dying on it, confirms
with every reviewer before it may call anything a clean approval, and prints the
recorded reviewer list with the exact command to restore it **before** it narrows
anything — and again in every terminal report. Where there is no such command to
print, because the loop refused to narrow or the key is unset, the report says that
outright and states that `review.reviewers` was never written, so the current value
it shows is the user's own. Every exit path carries one or the other; none is silent
about the key, and none is asked to print a command the loop cannot print both
safely and correctly. It does not narrow at all unless it
has first established that it can put back exactly the list it found, and every
reviewer entry it puts on a command line — one it runs or one it prints — goes
there as a single quoted argument. Every reviewer entry that reaches **screen
through the loop** goes there with its control bytes made visible: escaped when the
loop composes the line, and piped through `LC_ALL=C sed -n l` when the value comes
straight out of `devgeta config get`, so the raw bytes never leave the pipe. A
stored entry therefore cannot rewrite the loop's **own** report. The copy-paste
restore command is the one place the entries stay literal, because it has to work
when pasted, and a recorded entry holding a control byte turns narrowing off for
the whole run rather than being printed raw inside it. It also writes the key in only
two directions it has just re-checked: it **narrows only while the key still holds the
list it recorded** — checked immediately before every narrowing write, not once after
the opening round — and it **puts a list back only onto the narrowed list it wrote
itself**. A key changed by anyone else, between rounds or while one ran, is left exactly
as found, and the loop narrows no further.

It establishes that using **only `devgeta` commands it already runs**: it never
reads `global_config.yaml`, which it has no permission to reach and no parser for
(§2). The record is the opening round's own verdict labels, and it is only used
after a check proves it is byte-identical to the configured list (Step 2).

**What this deliberately does not promise.** Not that the loop never leaves
`review.reviewers` narrowed without saying so. A Ctrl-C, a crash, or a killed
session between `config set` and the restore ends the process, and a process that
is gone prints nothing — no prose can make it. Only the `--models` flag rejected
in ADR-0026 closes that window. So the loop states the restore command _before_
the first mutation instead of only after the last one: an interruption then finds
the instruction already on screen rather than owed. What stays broken: the config
remains narrowed until someone runs that command, and the next run of the loop
records whatever list it finds as its baseline — so an interruption nobody undoes
makes the narrowing permanent and silent.

And not that the key can never be overwritten. Each of those two checks is a separate
command from the write it guards, and prose cannot fuse two shell commands into one —
`internal/config/lock.go` makes a single `devgeta config set` atomic against another
one, but it is released when that call returns, and no command compares and sets in a
single step (§2). So a `config set` from another shell landing in that one-command gap
is still overwritten. What the loop promises is not that it catches this, only that it is
not silent when it can see it: `config set` reports the value it replaced, read under the
same lock as the write, and the loop captures that output and compares the whole string
against a line it composed itself — never splitting a previous value out of it, since an
entry may contain the `" -> "` the line separates on (§2). A write reported as landing on
something else is named in the report and stops all further narrowing, and the loop does
not try to put the value back, because the string it was reported in is `get`-joined and
therefore ambiguous. **And this promise has a hole prose cannot close, so it is stated
rather than glossed:** the same `", "` ambiguity means a replaced value whose printed form
equals the expected line is indistinguishable from no change at all — one entry
`anthropic/a, openai/b` prints what two entries print — so that write is overwritten and
the check reports success. Silence here is therefore not evidence that nothing happened.
Closing the gap, and closing that blind spot, both need the same `--models` flag as the
interruption window.

And not that a stored control byte never reaches the terminal at all. It does, and
prose cannot stop it: `devgeta task review-run` prints each reviewer's label raw on
stdout and again on stderr (§2), so the opening round emits every configured entry
verbatim before the loop has read a single line — before the record exists, and
therefore before any check could refuse it. Those lines cannot be filtered either:
the loop must read the verdict lines first-hand and unfiltered, the stderr progress
is live on purpose, and a byte-level filter mangles the `→` the lines are parsed
on. So the promise is the narrower, achievable one — **the loop escapes what the
loop renders** — and the residual is that a config holding an ESC or a `\r` will
scramble `review-run`'s own output for one round on its way to being refused.
Closing that means changing what `review-run` prints, which is a Go change and out
of scope (§4); the fail-closed refusal then keeps the damage cosmetic, because the
config is never written.

---

## 4. Scope Boundary

### In Scope

- [ ] **The gate first, before any of the rest:** no edit to
      `configs/shared/commands/review-loop.md` begins until the decision is recorded in
      ADR-0026 — it is — **and** the maintainer has given an explicit go-ahead on this
      plan, recorded in Step 0. The gate is deliberately **not** "ADR-0026's `Status`
      reads `ACCEPTED`": [docs/decisions/README.md](../../decisions/README.md) defines
      `ACCEPTED` as decided _and implemented_, so that condition cannot honestly be true
      before the work it gates exists (Step 0 spells this out). This is a precondition on
      the work, not a deliverable of it
- [ ] Three phases in `review-loop.md`: opening round (all), narrowing rounds
      (non-approvers only), confirming round (all)
- [ ] Only the confirming round can yield a clean approval; earlier approvals are
      provisional
- [ ] Every phase transition is stated, including the unanimous opening round that
      skips narrowing and goes straight to confirming, and **both** ways the
      narrowing set can empty — everyone approved, or everyone left in it was
      dropped for repeated failure — each with its destination named outright
      rather than inferred from an empty set vacuously satisfying "all approved"
- [ ] Round cap applies per phase
- [ ] A failure is a non-approval that keeps the reviewer in the narrowing set;
      two consecutive failures drop it and report it as failed; any other outcome
      resets that reviewer's count; a failure in the confirming round still stops
      the loop
- [ ] Step 3's "withheld approval but recorded no finding" stop splits: a stated
      position still stops the loop, a failed round goes on to the next one
- [ ] Narrowing via `review.reviewers`, restored after **every** round rather than
      held narrowed for a whole phase
- [ ] A restore writes only onto the narrowed list the loop itself wrote: the key is
      checked before that write, and a value changed during the round is left exactly
      as found, with narrowing off for the rest of the run and the fact reported
- [ ] A narrowing write happens only while the key still holds the **recorded** list —
      checked immediately before every write, not once after the opening round, because
      the journal read and the previous round's fixes sit in between. A value changed in
      that gap is left as found, narrowing stops, and the report names it. The
      one-command check-to-set gap that remains is reported rather than closed: each
      write's output is captured and compared whole against a line the loop composed,
      never field-split and never printed bare, the replaced value is never
      reconstructed, and the report states that a replacement printing the same string
      as the expected one is not detected at all
- [ ] The config is written only when the narrowing set is a strict subset of the
      recorded list. A one-reviewer set — configured, or the unset key that means
      OpenCode's default model — and a round in which nobody approved run with no
      write and no restore; the protocol has no `config unset` step
- [ ] The record of what to restore comes from `devgeta` commands only — the
      opening round's verdict labels, checked against `devgeta config get
review.reviewers` — and **never** from reading `global_config.yaml`, which no
      shipped agent permission reaches; restore passes one argument per recorded
      entry, never a comma-joined string
- [ ] The shipped command states **why** it does not read that file, and identifies
      it descriptively — never by filename and never by path — so Step 8's negative
      anchor on both literals stays an exact substring check. The reason for the
      wording rule stays in the cycle doc and the test comment, out of the shipped
      artifact (§12)
- [ ] The record is **proved before it is used**: single-line `get` output, and
      `get` output byte-identical to the labels joined with `", "`. Any mismatch
      turns narrowing off for the whole run
- [ ] The recorded list and the exact restore command are printed **before** the
      first narrowing write — the only part of the restore story that survives an
      interruption, since an interrupted run cannot print a report
- [ ] Narrowing is refused for the **whole run** unless every recorded entry is one
      `config set` can write back **and** free of control bytes; the loop never
      narrows first and repairs a partial restore afterwards
- [ ] Every reviewer entry the loop puts on a command line — the narrowing write,
      the restore, and the restore command printed for a human to paste — is one
      quoted shell word, because a valid entry may contain spaces and shell
      metacharacters
- [ ] Every reviewer entry or config value the loop **displays** has its control
      bytes made visible, by whichever of the two routes fits where the string came
      from: a string the loop composed — the recorded entries, the expected value in a
      mismatch, the entry that turned narrowing off — is printed in the escaped
      rendering defined in Step 2; a value that comes straight out of the config —
      the `get` line at step 0, the found value in a mismatch, the current value in
      every report, and the value `config set` reports it replaced — is displayed by
      piping through `LC_ALL=C sed -n l` rather than emitted bare, so the raw bytes
      never leave the pipe. Quoting
      keeps a stored entry out of the shell; this keeps it out of the terminal, as
      far as the loop's own output goes
- [ ] Terminal report states which reviewers ran per round, and the config's
      current value in every exit path. Beside that value it prints **exactly one**
      of two things: the recorded restore command, literal and quoted, whenever the
      loop holds a record it proved and accepted; or, when it holds no such record —
      narrowing refused, or `review.reviewers` unset — a statement that the key was
      never written and the value shown is the user's own. Never both, never neither,
      so no exit path is forced to print a command that would be unsafe or wrong
- [ ] New guard tests for the restore invariant and the confirming-round rule
- [ ] `docs/spec.md` brought to the new contract — it is
      [CLAUDE.md §2](../../../CLAUDE.md)'s **first** source of truth and it currently
      documents the loop this cycle replaces: a whole-loop round cap, a clean approval
      with no confirming round, and a reviewer failure as a terminal outcome. The
      narrowing itself also has to appear there, because it rewrites a key the user
      owns

### Explicitly Out of Scope

- A `--models` / `--skip-models` flag on `review-run` — rejected in ADR-0026 in
  favour of a prose-only change, with the trade recorded there
- A record file on disk that a later run restores from, so an interrupted run
  self-heals on the next invocation — rejected in ADR-0026: there is no durable
  path the loop may write prose-only (`.git/**` edits are denied in both agents,
  and the only external directory either agent grants is the disposable scratch
  cache), and a record that can go missing launders the narrowed list into the
  next run's baseline
- Sanitizing what `devgeta task review-run` itself prints. Its verdict lines
  (`reviewrun.go:306`) and its stderr progress lines
  (`reviewprogress.go:137`) emit a stored reviewer entry raw, before the loop has
  read anything, so a control-byte entry scrambles the opening round's own output
  no matter what the loop's rendering rules say. Escaping at the source is a Go
  change, which the prose-only decision rules out; the loop cannot filter those
  lines either, since it must read the verdicts first-hand and a byte-level filter
  rewrites the `→` they are parsed on (§2). Named as the residual in §3 and §7
  rather than promised away
- Any change to `internal/tooling/task/reviewrun.go` or `cmd/task.go`
- `configs/shared/commands/pr-review-loop.md` — a sibling loop with its own ADR
  (ADR-0022/0023) and its own trigger semantics; not touched here
- ADR-0017's escalation rules (§2), its sequential-reviewers and round-snapshot
  rules (§4), §5, and its §6 human-only ratification rule — and ADR-0020's
  within-round retry. **Its §1 and §3 are not out of scope**: ADR-0026 amends both,
  and the amendment pointers in ADR-0017 (two section notes, one note on its
  auto-retry alternative, one `Related` entry) are part of this cycle's planning
  documents rather than a later step. What stays out of scope is anything more than
  those pointers: no rewriting of its reasoning, no restructuring, and no change to
  its `Status`, which remains `ACCEPTED` because only two of its sections are amended
- Changing `review.rounds`'s default or its validated 1–5 range

**Scope is locked.**

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                    | Description                                                                                                                                                                                                                                                                                                                                                                                                |
| ------ | -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Modify | `docs/decisions/ADR-0026-…md`                | `Status: PROPOSED` → `ACCEPTED` — **last, after everything below has shipped**, because this repo defines `ACCEPTED` as decided _and implemented_; and **the maintainer's edit, not the implementer's** (Step 0). Nothing in this cycle waits on it                                                                                                                                                        |
| Modify | `configs/shared/commands/review-loop.md`     | Phases, per-phase cap, revised failure rule, restore invariant, report fields                                                                                                                                                                                                                                                                                                                              |
| Modify | `internal/apps/opencode/permissions_test.go` | Two new guard tests; confirm the four existing ones still pass                                                                                                                                                                                                                                                                                                                                             |
| Modify | `docs/spec.md`                               | The `/review-loop` paragraph (`:783-793`), the `review.rounds` key (`:345`) and its `dg config set` example (`:538`), the `review.reviewers` key (`:344`), and `/pr-review-loop`'s "counterpart … two deliberate differences" sentence (`:988-992`) — Step 9 names the exact sentences. **`review-run`'s own row (`:753`) must not be edited**: it describes the command, which this cycle does not change |
| Modify | `docs/decisions/ADR-0017-…md`                | Amendment pointers only — a note at §1 and at §3, a note on its "Auto-retry a failed reviewer" alternative, and ADR-0026 on its `Related` line. **Already written, with these planning documents**; no implementation step touches it, its `Status` stays `ACCEPTED`, and §2/§4/§5/§6 are untouched                                                                                                        |
| Modify | `docs/decisions/README.md`                   | The ADR-0026 index row. **Already written, with these planning documents** — `README.md`'s own creation workflow makes indexing step 3 of _creating_ an ADR, not a post-implementation chore; no implementation step touches it, and its status table is untouched                                                                                                                                         |

### Step-by-Step

#### Step 0: The decision is recorded and the maintainer has said go — before the first edit

Nothing below this step may start without both halves of
[CLAUDE.md §10](../../../CLAUDE.md)'s required workflow. Item 2 says to "record each
such choice as an ADR in `docs/decisions/` **before implementation starts**", and item 3
says "Get user/team approval before implementing."
`configs/shared/commands/review-loop.md` ships into **every user's other repos**
([§12](../../../CLAUDE.md)), and §10's never-silently list puts the same weight on
shipped artifacts: they change only with explicit discussion.

- **The gate is two conditions, and neither is a status word.** (1) The decision is
  recorded in ADR-0026, with the design the steps below are written against — this is
  already true. (2) The maintainer has said, explicitly, to proceed on this plan. Both,
  not either.
- **Why the gate does not say "`Status` reads `ACCEPTED`", even though that reads like
  the obvious check.** It would be circular.
  [docs/decisions/README.md](../../decisions/README.md) defines `ACCEPTED` as "Decided
  **and implemented**", so ADR-0026 cannot honestly say `ACCEPTED` until the work in
  this cycle exists — and that work is precisely what the gate holds back. An
  implementer following such a gate literally would have to falsify the status line or
  never start. So ADR-0026 stays `PROPOSED` through implementation, and here `PROPOSED`
  carries the repo's weakest available meaning of "decided, not yet built" rather than
  "still being argued about" — four ADRs whose work shipped months ago are still
  `PROPOSED` (0008, 0009, 0014, 0015), so the field has never tracked implementation
  reliably in either direction anyway. **Recommended to the maintainer, not decided
  here:** if the status table should gain a state for "decided, not yet built", that
  edit changes what every ADR's status means repo-wide and belongs to them, not to this
  cycle. This plan is written so it does not need one.
- **How the go-ahead is recorded, since it is now the whole gate.** Write it in §8's
  reviewer notes — the date and what was approved — before starting Step 1. That is the
  branch's own record of item 3 being satisfied; do not rely on it living only in a
  chat log.
- **Who flips the status, and when.** The maintainer, and **after** the work ships, as
  the §5 table's first row says. An implementer — human or agent — never edits that
  line on its own authority, and an unattended run never edits it at all.
- **If approval is withheld:** stop here. Do not start Step 1, and do not implement a
  subset "to have something ready" — a partial edit to the shipped command is the same
  cross-repo change, just harder to see. The branch stays exactly what it is today, two
  planning documents, which is a reviewable and complete state on its own. Record what
  the maintainer objected to in §8's reviewer notes so the next attempt starts from the
  objection rather than rediscovering it.
- **If the decision is approved with changes:** amend ADR-0026 and this plan to match
  before Step 1, rather than carrying the difference in someone's head. The steps
  below are written against the ADR as it stands, so a plan that no longer matches it
  is not the plan that was approved.
- Verify: §8 records the maintainer's go-ahead on this plan, and
  `grep -n '^\*\*Status:\*\*' docs/decisions/ADR-0026-the-review-loop-narrows-to-the-reviewers-still-blocking.md`
  prints `PROPOSED` — the expected state, since the work has not shipped. If it already
  prints `ACCEPTED` while `configs/shared/commands/review-loop.md` is unchanged, that is
  a stale status to fix, not a gate to walk through.

#### Step 1: Add the phase model, without touching the guarded sections yet

- New `## Phases` section between `## What this drives` and `## Flow`: the three
  phases, what set of reviewers each runs, and the rule that only the confirming
  round can produce a clean approval.
- State the config-restore invariant here once, in full, so later steps can refer
  to it rather than repeating it.
- Verify: `go test -run TestReviewLoop ./internal/apps/opencode/` — the four
  existing guards must still pass, since nothing they anchor on moved yet.

#### Step 2: The record comes from `devgeta`'s own output, and is proved before use

This step edits two sections of the skill — `### 0` keeps the `get` line, `### 1`
completes and proves the record on the opening round — because the record needs both
and the guarded headings are separate. Step 3 below covers the rest of `### 1`.

The record of what to restore is built out of two things the loop already reads,
and **nothing is read off disk**:

- **The `get` line.** At step 0, before any round,
  `devgeta config get review.reviewers` is the string the record gets checked
  against — it is not the record itself. The loop never has to hold those bytes,
  and never runs that `get` bare: the checks below compare inside `$( )` and count
  with `| wc -l`, neither of which prints anything, and the one display step 0 does
  make goes through `LC_ALL=C sed -n l`. Read those two uses as one rule — a `get`
  is either compared or filtered, never emitted — because a bare `get` prints the
  stored bytes with no escaping at all (`cmd/config.go:184`) and there is no
  later point at which the loop could take that back.
- **The labels.** The opening round runs every configured reviewer and prints one
  `<label> → <outcome>` line each (`review-run` line 306, contract at :201). For a
  configured model that label _is_ the entry: `resolveReviewerRuns` sets
  `run.label` and `run.model` to the same string
  (`internal/tooling/task/reviewrun.go:628`). The labels, in the order printed, are
  the record. Every reviewer prints a line whatever its outcome, `ERROR` and
  `NO VERDICT` included, so a reviewer failing the opening round still contributes
  its label. A round that fails as a whole prints no lines at all — `ReviewRun`
  discards the lines it had and returns `""` — so a half-record is not a state that
  exists; either the loop has every label or it has none and narrowing never starts.

Say plainly why the record is not read off disk, or the next reader will
"simplify" it back and reintroduce a step that cannot run: the file behind
`devgeta config` sits outside the repository, the only external directory either
agent grants is the scratch root, and in a headless run the read auto-rejects
rather than prompting (§2). There is also no parser — the loop is prose, and YAML
scalars have more spellings than prose can be trusted to decode.

**Write that explanation without spelling the file's name or its path.** Refer to
it the way the sentence above does — "the file behind `devgeta config`", "devgeta's
stored config" — so that neither of two literal strings, `global_config.yaml` and
`.config/devgeta`, ends up anywhere in the shipped file. That is not a stylistic
preference: Step 8's negative guard is an exact substring check for exactly those
two strings over the whole shipped file, which is what
makes the ban structurally enforced rather than a convention someone has to
remember ([CLAUDE.md §4](../../../CLAUDE.md)) — anyone reaching for the "obvious"
source has to type one of them, and the build fails. The guard therefore
over-catches an innocent mention too, this explanation included, which is exactly
why the wording rule is written down here instead of being discovered as a red
test.

Do **not** answer that failure by putting the guard's reason into the shipped
command. A note about devgeta's test suite is project law inside an artifact
strangers install, which [CLAUDE.md §12](../../../CLAUDE.md) forbids — this command
runs in repos that have neither the test nor the branch it guards. The reason lives
in Step 8's test comment and in this step; the shipped prose carries only the
permission facts above, which stand on their own for a reader who has never seen
this repo.

**Prove the record before using it.** The labels are the configured list after
`strings.TrimSpace`, after blank items are dropped, and after duplicates are
collapsed (`reviewrun.go:619-629`), so they are not automatically what the user has
stored. Two checks establish that they are, and both are commands with an answer to
read rather than a judgement to make:

1. `devgeta config get review.reviewers | wc -l` reads **1**. The output is a single
   line, so no stored entry contains a newline.
2. `[ "$(devgeta config get review.reviewers)" = '<labels joined with ", ">' ]`
   succeeds — the joined labels are byte-identical to the `get` line, single-quoted
   per the quoting rule below.

State why that pair is sufficient, so nobody weakens it to one check. `get` prints
exactly `strings.Join(entries, ", ")` (`cmd/config_settings.go:339`). Trimming and
dropping only ever remove characters, and collapsing a duplicate removes an entry
and its separator too, so if the labels differ from the stored entries in any way at
all, their join is **strictly shorter** than the `get` line and check 2 fails. Check
1 is what makes that argument airtight: command substitution strips trailing
newlines, so without it an entry stored as `anthropic/a` plus a trailing newline
would compare equal to the trimmed label. Together they mean check 2 passing is the
same statement as "the labels are the stored list, in order, byte for byte" — order
included, because a join is order-sensitive.

**Any mismatch turns narrowing off for the whole run**, exactly as an unrestorable
entry does below: every round runs the full configured list, `review.reviewers` is
never written, and the report says the record could not be proved and prints the
`get` line — through the `sed -n l` filter defined below, since the values that fail
this check are exactly the malformed ones, and one of the things that fails it is an
entry containing a newline. This is the fail-closed direction on purpose. The cases it catches
are real, and each one is a config the loop must not rewrite: a duplicated entry
(the labels collapse it, and restoring would silently delete one), an entry with
surrounding whitespace (restoring would silently trim it), a blank item from a
hand-edit (restoring would silently drop it), and an entry containing a newline or
its own `→` (which would make the round's output ambiguous to read). Note what
that last pair buys: a misparsed label cannot slip through, because a wrong parse
changes the join and check 2 fails. The loop cannot narrow on a record it has not
proved — which is the point, since a fail-closed run costs money and a bad restore
costs the user their configuration.

- Keep the heading starting `### 0. Resolve the reviewer selector`, and keep
  `$ARGUMENTS` and `--note` inside it.
- Say what an absent `review.reviewers` key records — and check for it **first**, so
  it is never reported as a failed proof. `get` prints an empty line for an unset
  key (its `Get` reports "not set" when the list is empty,
  `cmd/config_settings.go:339`), and an unset key is an **empty** list, which is a
  valid configuration and not an error. `resolveReviewerRuns`
  (`internal/tooling/task/reviewrun.go:609`) turns an unset or all-blank list into
  one reviewer on OpenCode's own default model, launched with no `-m` flag at all
  (`defaultModelLabel`, :41; `RunOptions.Model` only adds `-m` when non-empty,
  `internal/apps/opencode/opencode.go:357`). That reviewer has no `provider/model`
  string, so no command can write it into `review.reviewers` — and none has to.
  Step 3's write condition means a set of one is never narrowed, so an unset key
  stays unset for the whole run and there is nothing to restore. The line this
  step prints says exactly that — `review.reviewers` is unset, one reviewer on
  OpenCode's default model, nothing will be changed — instead of a
  `devgeta config set review.reviewers` with no arguments after it, which is not a
  runnable command — zero values are rejected (`requireAtLeastOne`,
  `cmd/config_settings.go:89`).
- State plainly that the `get` line is **not** the record and must never be split
  into one, and why, so a later reader does not shortcut past the labels. `get`
  prints the list joined with a comma and a space (`strings.Join(..., ", ")`,
  `cmd/config_settings.go:339`), and the validator accepts any entry containing a
  `/` that is neither first nor last character (`isProviderModelShaped`,
  `cmd/config_settings.go:395`) — so a comma and a space are legal **inside a single
  entry**. The two-entry list `anthropic/a` + `openai/b` and the one-entry list
  `anthropic/a, openai/b` therefore print identically, and splitting `get`'s output
  restores a list the user never had. The labels have no such ambiguity: they arrive
  one per line, one per reviewer, already separated by the command that ran them.
  `get` is used **only** whole, as the string the joined labels are checked against
  — which is exactly the direction the ambiguity does not hurt, since the check asks
  "does this list join to that string", never "what list did that string come from".
- Document the `config set` trap this pairs with: the setter takes **multiple
  arguments**, one per recorded entry, not one comma-joined string (a joined
  string is written as a single bogus model id and the round fails with a server
  error). One recorded label per printed line in, one `config set` argument per
  recorded label out — the same shape at both ends, which is what makes the
  round-trip exact once the proof above has passed.
- **Quote every entry — in the command the loop runs and in the command it
  prints.** "One argument per entry" is only true if each entry survives the shell
  as one word, and a saved reviewer entry is not tame text. The validator requires
  nothing but a `/` that is neither the first nor the last character
  (`isProviderModelShaped`, `cmd/config_settings.go:395`), so spaces, `;`, `&&`,
  `$( )`, backticks, quotes and newlines are all legal inside a stored entry,
  before or after the slash. Interpolated bare, `anthropic/a b` becomes two
  reviewers and `anthropic/a; <command>` becomes a second command that runs with
  whatever the agent's shell can do. A config value read off disk is untrusted
  input to a command line, which [CLAUDE.md §4](../../../CLAUDE.md) requires be
  validated before use. So single-quote each entry and escape any single quote it
  contains (the usual `'\''`), in **all three** places the entries appear: the
  narrowing write, the restore, and the restore line printed for a human. The
  printed one is not the lesser case — it is the one a human pastes into their own
  shell, and it is the only recovery an interrupted run leaves behind (§3).
- **Quoting protects the shell; it does nothing for the terminal. Make every value the
  loop displays visible instead of raw — by escaping it when the loop composed it, and
  by filtering the `get` when it did not.** Single quotes stop
  `anthropic/a; echo INJECTED` from becoming
  a second command, and stop nothing about `anthropic/a\x1b[2K` — the ESC bytes
  reach the terminal either way, because printing a quoted string still prints its
  bytes. And a stored entry can hold them: `Set` filters no characters, `yaml.v3`
  round-trips them exactly, and `get` prints the joined list through a bare
  `fmt.Fprintln` (§2). Worse, Step 2's proof does not catch them — ESC, BEL and
  `\x7f` are not whitespace, so `strings.TrimSpace` leaves them and the join still
  matches, and `wc -l` counts only `\n`. So `anthropic/a\x1b[2K` and
  `anthropic/a\rall reviewers approved` are **proved** records, and printing either
  one raw erases or overwrites the line it was printed on — including the line whose
  whole job is to be the interrupted run's only recovery (§3), and the
  expected-beside-found pair that is the entire diagnosis of a mid-round change. A
  config value is untrusted input to the report as much as to the command line,
  which is what [CLAUDE.md §4](../../../CLAUDE.md) requires be validated before use.

  **The escaped rendering** — define it once here, and use the term wherever a value
  the loop composed is displayed (Step 7 especially) rather than restating it. Print
  the value inside backticks, and before printing
  replace every byte that is not printable ASCII (`0x20`–`0x7e`) with a visible
  escape: `\t`, `\n`, `\r`, `\e` for ESC (`0x1b`), and `\xNN` with two lowercase hex
  digits for anything else. A value holding a newline therefore prints on one line
  as `` `anthropic/a\nopenai/b` ``, and a value holding ESC prints the two letters
  `\e` instead of moving the cursor. Non-ASCII bytes are escaped per byte too, so a
  model id with a UTF-8 character displays as hex — ugly, and deliberate: printable
  non-ASCII is where bidirectional overrides and zero-width characters live, and the
  line a human reads to decide whether their config was mangled is the wrong place
  to render those faithfully. Be honest about the limit: this is prose, so it binds
  an agent exactly as far as the rest of the file does (Step 8), and it protects the
  terminal the report is read in, not a downstream viewer that re-interprets `\e`
  itself.

  Apply it to every value the loop **composes**: the recorded entries named before
  the first narrowing write, the expected string in any mismatch, and the entry that
  turned narrowing off. The one exception is the next bullet's subject — the
  copy-paste restore command, whose entries stay literal.

  **A value that comes straight out of `devgeta config get` is not composed, so it
  needs the other route: never run that `get` bare when its output is going to be
  seen.** The escaped rendering cannot protect it, and the reason is worth stating
  because it is the whole limit of this rule: to escape a value the loop has to have
  the value, and it can only get it by having the command print it — at which point
  `get`'s bare `fmt.Fprintln` (`cmd/config.go:184`) has already put the raw bytes on
  screen. So display those through a filter instead:
  `devgeta config get review.reviewers | LC_ALL=C sed -n l`, which prints ESC as
  `\033` and `\r` as `\r` and marks the end of the line with `$`, so nothing raw
  leaves the pipe (§2 has the check, and says why `sed` and not `cat -v`). Four
  displays take this route — the `get` line at step 0, the found value in a
  mid-round mismatch, the current value in both report templates, and the value
  `config set` reports it replaced, which is a config value the loop did not compose
  either (captured first, then filtered — see the check-to-set bullet below) — and
  the proof checks take neither, because `$( )` and `| wc -l` print nothing at all.

  **What neither route covers, and say so rather than implying otherwise.**
  `review-run` prints each reviewer's label raw on stdout (`reviewrun.go:306`) and
  again on stderr (`reviewprogress.go:137`), and the loop can neither escape those
  lines (it did not compose them) nor filter them (it must read the verdicts
  first-hand, and a filter rewrites the `→`). They also come **before** the record
  exists, so the control-byte refusal below cannot get ahead of them. A config
  holding an ESC therefore scrambles the opening round's own output for one round on
  its way to being refused; what these rules buy is that it cannot scramble the
  loop's own report, which is the artifact a human reads to decide anything. Closing
  the rest needs a Go change (§4, Out of Scope) — do not write the rule as though
  the loop covers everything printed during a run. `config set`'s own
  `<key>: <previous> -> <new>` line is **not** in this category, and must not be
  filed with it: it is capturable, because nothing reads it live and nothing parses a
  `→` out of it, so it takes the filter route above.

- **The copy-paste restore command stays literal, so a control byte in a recorded
  entry turns narrowing off for the whole run.** The printed
  `devgeta config set review.reviewers 'a' 'b'` is not a value being displayed, it is
  a command a human pastes, so escaping its entries would make it write the wrong
  list — it has to keep the single quoting above and the exact bytes. Those two
  requirements cannot both hold for an entry containing a control byte: literal
  bytes let the entry rewrite the recovery line, and escaped bytes make the paste
  wrong. So the loop does not choose between them — if any recorded entry contains a
  byte below `0x20` or `0x7f`, **narrowing is off for the whole run**, joining the
  same branch as an unwritable entry and an unprovable record: full configured list
  every round, `review.reviewers` never written, and the report naming the entry in
  the escaped rendering. Nothing then needs a paste-exact command carrying a raw ESC,
  because there is nothing to restore.

  Note what the trigger is and is not: control bytes only. A non-ASCII entry is
  displayed escaped but narrows normally, because it cannot move a cursor and pastes
  back correctly.

  Say why this is not solved with a second quoting form, or it will be reopened.
  bash and zsh's `$'anthropic/a\e[2K'` is both printable and paste-exact, and it was
  rejected: it is a second quoting rule layered on the single-quoting one, correct in
  neither POSIX `sh` nor every place the entries appear, and it buys rounds on a config
  that is one byte away from malformed. Refusing is the smaller rule, and it is the
  same trade §7 already accepts for a config the loop cannot prove. If someone
  genuinely stores an ESC in a model id, that is the door.

- **Narrow onto the recorded list and nothing else: re-check the key immediately before
  every narrowing write.** The record is proved once, from the opening round's labels.
  The write that uses it comes later — and not by seconds: between the proof and the next
  narrowing write sit the journal read, the triage of every open finding, and every fix
  subagent the round dispatched, which is where this loop spends nearly all of its wall
  time. So this window is **wider** than the one guarded after the round, not narrower,
  and it reopens before every narrowing write rather than only the first. Guard it with
  the same two commands, run against the **record**:
  `devgeta config get review.reviewers | wc -l` reads 1, and
  `[ "$(devgeta config get review.reviewers)" = '<record joined with ", ">' ]` succeeds.
  Both passing means the key still holds the list the loop recorded, so the narrowed list
  overwrites nothing the user added and the restore afterwards puts back what is actually
  there. Either failing means something outside the loop changed the key — **do not
  write, and do not narrow again for the rest of the run**: the same branch and the same
  report as the post-round mismatch below.
- Trace what that check prevents, because the failure is not obvious and it is the whole
  reason a check after the round does not cover it. Without a pre-write check, a
  `devgeta config set review.reviewers …` from the human's own shell landing in that
  window is overwritten by the narrowing write. The key then holds exactly the narrowed
  list the loop wrote, so the post-round check **passes** — and the restore writes the
  stale record over the user's new list. Two writes the user never asked for, and the
  check meant to catch one of them reports success. [CLAUDE.md §4](../../../CLAUDE.md)'s
  Data Integrity rule is the standard: a check that passes on a config it has just
  clobbered does not meet it.
- Keep all three key-state checks, and say which window each covers, or one will be
  dropped as a duplicate of another. The **pre-write** check covers the gap between
  rounds — the journal read and the fixes. The **post-round** check covers the gap during
  a round, while the reviewers run. The **post-restore** verification further down covers
  neither: it catches a restore that did not land at all, and it is the only check the
  last round's restore has after it, since no later write follows. The write's own report
  of what it replaced, below, is a fourth reading of the key and covers the one gap no
  check can — the command between the pre-write check and the write itself. What does
  **not** repeat is the rest of the proof: the record is derived once and never again, so
  restorability and the control-byte screen are properties of the record rather than of
  the key, and re-running them would answer a question that cannot have changed.
- **Say what the pre-write check cannot close: one command's worth of race.** The check
  and the write are two commands, and prose cannot make them one, so a `config set` from
  another shell landing between them is still overwritten. `internal/config/lock.go` does
  not reach this — its sidecar lock is taken inside a single `config.Update` call
  (`cmd/config.go:212`) and released when that call returns, so it makes one `set`
  atomic against another `set` and cannot span the loop's separate `get` and `set`. No
  command compares and sets in one step, and adding one is a Go change the prose-only
  decision rules out (§4). What the loop does get is a partial detection instead of total
  silence — partial in a way the next bullet spells out and the report has to admit:
  `config set` prints `<key>: <previous> -> <new>` (`cmd/config.go:245`) and reads that
  previous value inside the same lock as the write, so it is exactly the value that write
  replaced. Read it the only way that line can be read soundly (§2): **capture the write's
  stdout and compare the whole string, byte for byte, against a line the loop composes
  itself** — `<key>: <what the check just confirmed> -> <what this write set>`, with the
  confirmed value being the record's join before a narrowing write and the narrowed list
  before a restore, since both writes have the same one-command gap in front of them. The
  write, the comparison and any display of the result are **one** shell command, because a
  captured variable does not survive to the next one:

  ```sh
  out=$(devgeta config set review.reviewers 'anthropic/a') || exit 1
  if [ "$out" = 'review.reviewers: anthropic/a, openai/b -> anthropic/a' ]; then
    echo 'replaced the recorded list, as expected'
  else
    printf 'replaced something else: '; printf '%s\n' "$out" | LC_ALL=C sed -n l
  fi
  ```

  Two things must not be done to that string, and each has a concrete failure. **Never
  split a previous value out of it** by cutting at the first `" -> "`: an entry may contain
  `" -> "`, so that read both hides a real change (a concurrent single entry
  `anthropic/a, openai/b -> anthropic/a` cuts to exactly the record's join) and invents a
  false one (a record whose own entry is `anthropic/a -> b` cuts to `anthropic/a`, which
  matches nothing). The whole-string comparison is right in both directions, and cannot be
  forged, because the previous value is the only free field in the line. **And never print
  it bare** — capture it as above; the replaced value came from outside the loop, so it may
  hold a control byte the record's refusal never screened. Nothing is lost by capturing:
  unlike `review-run`'s lines this output is not read live and has no `→` a filter would
  mangle, a validator error still lands on stderr and exits non-zero, and no expected
  previous value ever carries `config set`'s ` (default)` suffix, which only appears for an
  unset key (`cmd/config.go:215-217`) — the state both checks have just ruled out. On a
  mismatch the write landed on a value the loop never recorded, and the end state is the
  same from either write: the key still holds the **record** — a narrowing write's round
  runs on and its normal post-round restore puts the record back, and a restore write has
  already put it back — narrowing stops from there, and the report names the recorded list
  in the escaped rendering, since the loop composed that one, beside the replaced value
  put through the filter. Leaving the key narrowed instead would be strictly
  worse, which is why the round is not aborted. The loop must **not** try to put that
  replaced value back: the
  printed string is `get`-shaped (`strings.Join(..., ", ")`), so it carries the same
  one-entry-or-two ambiguity the record's proof exists to avoid, and rebuilding a list
  from a joined string is the exact mistake that corrupted the config in the motivating
  run (ADR-0026). Detection and an honest report, not a repair.

- **Say exactly how much that detection is worth, because one part of it does not work and
  saying so is the point.** The comparison catches every replaced value whose bytes differ
  from the expected line — a different list, a re-ordered one, one carrying ESC or `\r`, all
  of them simply fail the compare. What it cannot tell apart is a replaced value whose join
  is byte-identical to the expected string but came from a different list: one entry
  `anthropic/a, openai/b` prints exactly what the two entries print, so a user's edit to
  that value in the one-command gap is overwritten and the check reports success. That is
  the same `", "` ambiguity that makes `get` unusable as a record (§2) — it is why the
  record comes from labels — and it cannot be closed from either side prose-only, because
  every reading of the key the loop has is joined. So the honest statement of this rule is
  narrower than "the loop detects a write it did not expect": it detects one whose printed
  form differs, and the gap itself stays open. Both halves are the `--models` flag's
  (§4, Out of Scope), not prose's. What prose does close here, and what makes this
  different from `review-run`'s raw labels, is the display: the loop captures this output
  instead of letting it print, so a control byte in the replaced value cannot rewrite the
  line reporting it.
- **Restore onto the narrowed list and nothing else: check before writing, and never
  overwrite a change the loop did not make.** A round takes minutes, and nothing
  stops the human — nominally unattended, not absent — from running
  `devgeta config set review.reviewers …` in another shell while one runs. So before
  writing the record back, run the same two checks against the **narrowed** list
  instead of the record: `get` output is one line, and it is byte-identical to the
  narrowed entries joined with `", "`. Both passing means the key still holds exactly
  what the loop wrote, so writing the record back restores the user's own list and
  touches nothing else. Either failing means something other than the loop wrote the
  key during the round — **do not write.** Leave the value exactly as found, and
  narrowing is off for the rest of the run.
- Say why that comparison is exact even though `get` is lossy in general (the `", "`
  ambiguity above). The loop is never inferring a list from the string: the narrowed
  list is one it composed from the proved record and wrote itself, and `Set` stores
  every argument verbatim (`cmd/config_settings.go:341-357`), so it can compute the
  exact bytes `get` must print. The check asks "does the current value equal this
  string I already hold", never "what list did this string come from" — the one
  direction the ambiguity cannot hurt. The single-line check earns its place here
  too: command substitution strips trailing newlines, so without it a value replaced
  by an entry ending in a newline would compare equal to the narrowed list and get
  overwritten.
- Say why a mismatch stops narrowing for the **whole rest of the run** rather than
  skipping one restore, or it will be softened into a retry. It is the same
  fail-closed branch as an unprovable record, for the same reason: the record was
  proved by matching the labels against the configured list, and a key something else
  has rewritten voids that proof, so every later narrowing write would be composed
  from a stale record and every later restore would clobber the new value. There is
  nothing to re-prove it with either — labels only arrive from a round, and only the
  opening round runs the full list. From that point every round runs the key exactly
  as it now stands, `review.reviewers` is never written again, and the report carries
  it (Step 7).
- Report the state, and name the one cause that is cheap to tell apart instead of
  guessing. Normally the loop cannot know _who_ wrote the key, so it reports what it
  expected and what it found, each by the route that string's origin calls for: the
  expected value is one the loop composed, so it goes in the escaped rendering, and
  the found value comes out of `get`, so it is displayed through the `sed -n l`
  filter — never by running that `get` bare, since the found value is whatever some
  other process wrote and is the least trustworthy string in the whole report — and
  that it wrote nothing. The exception: if the current
  value is byte-identical to the **record's** join, the narrowing write never landed,
  so the config already holds the recorded list and nothing was lost — say that,
  rather than reporting a change the user did not make. Narrowing still stops, because
  a write that does not land is not a state to keep narrowing on.
- Verify each restore that does happen by re-running the two checks against the
  record: `get` output still one line, and still byte-identical to the joined record.
  A match means the config holds the record; a mismatch means the restore did not
  land, which is worth catching for the same reason the pre-write check exists — it
  is a state only a human can fix, and the printed restore command is what fixes it.
- **Print the record and the restore command as soon as the record exists, which is
  the moment the opening round's output is read — and always before the first
  narrowing write.** One line naming the recorded entries **in the escaped
  rendering**, and the exact
  `devgeta config set review.reviewers <entry> <entry> …` that puts them back, whose
  entries are single-quoted and literal because it is pasted rather than read. The
  two can differ in appearance without contradicting each other, and by the
  control-byte refusal above they only ever differ for a non-ASCII entry.
  This is the one piece of the restore story that survives an interruption: a
  session killed between a `config set` and its restore emits no terminal report,
  so anything stated only in the report is lost, while anything printed before the
  first mutation is already on the human's screen. Say in the prose _why_ it comes
  first, or a later reader will fold it into the report and think nothing changed.
- Order this print **after** the refusal checks, not before them, or the
  control-byte rule leaks: all of those checks need only the record, so they run the
  moment the labels arrive and still strictly before the first write. When any of
  them refuses narrowing, the loop prints **no restore command at all** — it prints
  the refusal and the offending value in the escaped rendering — and nothing is lost
  by that, because a refused run never writes the key, so there is nothing to
  restore and no interruption window to leave a recovery line for.
- Note plainly that this print cannot happen at step 0 itself, and that nothing is
  lost by that. The exact restore command needs the labels, and the labels come from
  the opening round. But the opening round runs the full configured list and writes
  nothing (Step 3's write condition), so the first mutation is still strictly later
  than the print: an interruption during step 0 or the opening round finds a config
  nobody touched. What step 0 can print, and should, is the `get` line, so even a run
  that dies inside the opening round leaves the configured value on screen. Print it
  by piping the `get` through `LC_ALL=C sed -n l` rather than running it bare and
  restating the output: step 0 has no record yet, so it has run none of the checks
  and knows nothing about the value it is showing, and a bare `get` would put the raw
  bytes on screen itself before the loop ever saw them. The filtered form is the only
  form step 0 emits.
- **Check the record is restorable before narrowing anything, and refuse to narrow
  if it is not.** `config set` re-validates every value, so an entry only reachable
  by hand-editing the file cannot be written back at all: an item with no `/`, and
  one whose only `/` is its first or last character, are rejected
  (`isProviderModelShaped`, `cmd/config_settings.go:395`). If the record holds one of
  those, **narrowing is off for the whole run** — every round runs the full
  configured list, `review.reviewers` is never written, and the report names the
  entry that turned narrowing off.
- Keep all three checks even though they end in one branch, and say which one catches
  what, or one of them will look redundant and get dropped. A hand-edited
  `noslash` entry survives to a label unchanged, so the join check passes and only
  this validity check stops it. A hand-edited **blank** item is the mirror image: it
  never reaches a label at all (`resolveReviewerRuns` drops it), so this check never
  sees it and only the join check stops it. And an entry holding an interior ESC or
  `\r` passes **both** of those — it is shaped like `provider/model`, `config set`
  writes it back happily, `TrimSpace` does not touch a non-whitespace control byte,
  and `wc -l` counts only `\n` — so only the control-byte check stops it (§2 has the
  evidence). Three refusals, one effect — narrowing off for the whole run — so the
  loop needs one branch, but it needs all three tests to reach it.
- Say why it fails closed, or someone will "fix" it back into a repair path. Do
  **not** narrow first and then restore the entries that validate: that writes a
  list the user never had, permanently drops the entry that failed validation, and
  can only own up to it in a terminal report — which is exactly the thing the
  interrupted case never reaches (§3). Verifying first is also the smaller rule.
  The loop always holds the complete record before its first write, because Step 3
  only writes when narrowing drops a reviewer and that cannot happen before the
  opening round has printed every label — so the checks have everything they need
  before the config is touched, and cost one pass over two or three strings plus one
  string comparison. A repair path,
  by contrast, is a second mechanism with its own failure modes on the exact edge
  case nobody tests. What it gives up is the money saved on a config that cannot be
  restored — the right trade, because the config is the user's and a run that saves
  money by mangling it has not saved anything. It also shrinks §7's first risk: a
  run with an unrestorable entry never opens the interruption window at all.
- Verify: `go test -run 'TestReviewLoopForwards' ./internal/apps/opencode/`

#### Step 3: Step 1 states which reviewers run this round

- Keep `--reviewer <key>` and `--note <text>` forwarding verbatim — both are
  guarded, and neither changes.
- Add the per-round sequence for a narrowing round: check the key still holds the
  recorded list, set the narrowed list, run, check the key still holds the narrowed
  list, restore immediately, then read the journal. Restoring before reading the
  journal matters: the journal read is where the loop can spend time, and the config
  should not stay narrowed across it. Checking before the write matters for the
  mirror-image reason — the time the loop spends between rounds is time the key can
  change in, and it is more time than a round takes (Step 2). Both
  the write and the restore pass one quoted argument per entry, per Step 2 —
  nothing here interpolates an entry bare.
- Both of those checks are conditions on the write beside them, not formalities
  recorded afterwards, and neither failure branch is "carry on anyway". If the key no
  longer holds the **recorded** list before the write, the loop does not narrow this
  round at all; if it no longer holds the **narrowed** list after the round, the loop
  does not restore. Either way the value is left exactly as found and narrowing stops
  for the rest of the run (Step 2). These are the only two points in a round that can
  end with `review.reviewers` holding something the loop did not write, which is why
  the report has to name both (Step 7).
- State the conditions on that write. The loop writes `review.reviewers` only when
  the narrowing set is a **strict subset** of the recorded list — only when
  narrowing actually drops a reviewer — **and** every Step 2 check passed: the
  record proved (single-line `get`, joined labels byte-identical to it), every
  recorded entry one `config set` can write back, every recorded entry free of
  control bytes, no earlier round having ended with the key holding something
  other than the narrowed list it was given, no earlier narrowing write having reported
  replacing a value the loop never recorded, **and** the key still holding the
  recorded list when checked immediately before this write.
  Otherwise the round runs on the config exactly as the user left it, no write and
  no restore. Eight cases land there:

  - **A one-reviewer set, configured or the unset default.** A single reviewer
    that withheld approval is already the whole set, so there is nothing to drop.
    This is what makes Step 2's unset key harmless rather than a hole: the loop
    never reaches for a `provider/model` string that does not exist.
  - **A round in which every reviewer withheld approval.** The narrowing set is
    the full list again, so the write would set the config to what it already
    holds.
  - **A recorded list holding an entry `config set` cannot write back.** Narrowing
    is off for the entire run (Step 2), so every round is a full-list round.
  - **A record the checks could not prove.** Same destination and the same reason:
    the loop will not write a list it cannot show is the one the user had (Step 2).
  - **A recorded list holding an entry with a control byte.** Also off for the
    entire run (Step 2): the restore command for such a list cannot be both
    pasteable and safe to print, so the loop never creates the need for one.
  - **A round whose pre-write check found the key no longer holding the record.**
    Narrowing is off from that round onward (Step 2). Nothing was overwritten,
    because the check runs before the write.
  - **A round that returned with the key no longer holding the narrowed list.**
    Narrowing is off from that round onward (Step 2), so every later round is a
    full-list round.
  - **A round whose own write reported replacing a value the loop never recorded.**
    The one-command race no check can close (Step 2): `config set`'s `<previous>` was
    not what the check immediately before it had just confirmed, so the write landed on
    a change made in between. Narrowing is off from that round onward. Unlike the two
    above, this one ends with the key holding the **record** — the round's restore
    still runs, or has already run — because leaving it narrowed would be strictly
    worse; but the value that was replaced is gone and cannot be reconstructed, which
    is why the report names it (Step 7).

  Those last three are the only ones of the eight that stop narrowing part-way through
  a run instead of before it starts; the other five refuse before the first write. The
  first two of the three are also the only cases in the whole protocol that leave the
  key holding a value the loop did not write, and what separates them is only when the
  change was noticed: before the loop overwrote anything, or after the round it ran
  alongside. The report has to name all three (Step 7).

  This also keeps §7's first risk off those rounds entirely: the interruption
  window opens only on rounds that drop someone.

- Never narrow to an empty set by clearing the key. `config set review.reviewers`
  with no values is rejected outright (`requireAtLeastOne`,
  `cmd/config_settings.go:89`), and `devgeta config unset review.reviewers` — the
  command that error points at — means "one reviewer on OpenCode's default model",
  not "run nobody" (Step 6's empty-set rule). The loop needs neither: with the
  strict-subset condition above it never writes a state that `config unset` would
  be the way back from, so there is no `config unset` step anywhere in the
  protocol.
- Verify: `go test -run 'TestReviewLoopForwards' ./internal/apps/opencode/`

#### Step 4: Rewrite step 2's failure rule

- A failure is a non-approval: the reviewer stays in the narrowing set and is
  retried next round.
- Two consecutive failures by the same reviewer drop it from the set; it is named
  in the report as failed, with its last reason verbatim.
- State what "consecutive" counts and what clears it, because the word alone does
  not say: the count is per reviewer, and any outcome from that reviewer other
  than `ERROR` or `NO VERDICT` resets it to zero. It counts rounds the reviewer
  **actually ran** — a round it sat out, because it had approved or was already
  dropped, neither adds to the count nor clears it. Without that sentence
  "consecutive" reads as consecutive rounds of the loop, and narrowing means
  reviewers routinely skip rounds.
- A failure in the **confirming** round still stops the loop.
- Rewriting this rule alone does not make a failure survivable: the skill's
  `### 3` also stops the loop on a round that recorded no finding, and a failed
  reviewer records none. Step 5 below splits that branch — the two edits ship
  together, or neither one works.
- Keep the existing `ERROR(<reason>)` / `NO VERDICT(<reason>)` / bare
  `NO VERDICT` reporting rules exactly as they are — including "do not invent a
  reason", which is the part that keeps the report honest.
- Keep the note that ADR-0020's within-round retry already happened one level
  below, so the loop's own retry is a second, different thing.

#### Step 5: Step 3 requires the confirming round, and routes a failed round onward

- Keep `open:`, `nothing under`, and `APPROVE` in the section — all three are
  guarded, and all three remain true conditions.
- Add the fourth condition: the round must be the confirming round. Spell out why
  with the motivating case — a reviewer approved in a narrowing round and then
  requested changes on the next look, having found a real defect.
- Split the existing "**Nothing under `open:`, and some outcome was not
  `APPROVE`**" branch in `### 3`, which stops the loop today. This is the branch a
  failed round lands in — a reviewer that errors or returns no verdict records no
  finding — so leaving it intact makes Step 4's retry unreachable and the first
  `ERROR` / `NO VERDICT` still terminal. Split it by _why_ approval was withheld:

  - **The outcome was `ERROR` or `NO VERDICT`:** nothing is open because the
    reviewer never finished, not because it had nothing to record. Do not stop —
    fall through to `### 5`, which applies the consecutive-failure count and the
    phase routing and runs the next round.
  - **The outcome was `REQUEST CHANGES` or `NEEDS DISCUSSION` with nothing
    open:** keep today's behaviour and stop.

  The branch's existing justification — the loop would change nothing between
  rounds, so the next round re-runs the same reviewers against the same tree and
  buys the same verdict — moves with the second case only. It is true of a
  reviewer that stated a position and false of one that failed: a retry is not
  betting on the tree changing, it is betting the reviewer completes this time.
  Say that in the prose, so a later reader does not collapse the two cases back
  together on the strength of the old reasoning.

- A round where every outcome was `APPROVE`, nothing is open, and this is **not**
  the confirming round matches neither branch as written. It is not a clean
  approval (the fourth condition fails) and it has no finding to triage, so `### 3`
  must hand it to the phase routing Step 6 adds to `### 5`, rather than letting it
  fall off the end of the flow.
- Leave the `agent:` rejection rule exactly as it is.

#### Step 6: Step 5's cap becomes per-phase, and the loop routes between phases

- The cap counts rounds within the current phase.
- State the transitions: opening → **confirming** when every reviewer approved;
  opening → narrowing when anyone withheld approval; narrowing → confirming when
  every remaining reviewer approves or the phase cap is hit; narrowing →
  confirming when the set empties because every reviewer left in it was dropped
  for two consecutive failures — **unless no reviewer approved at any point in the
  run**, in which case narrowing → report; confirming → report always.
- Spell out that failure-emptied case instead of leaving it to "every remaining
  reviewer approves". That phrase is true of an empty set only vacuously, and a
  vacuous reading is not something an executing agent can be relied on to reach —
  it is just as likely to conclude the flow has run out of rules and stop wherever
  it stands. The two states need separate destinations, because they differ in what
  a confirming round would have to work with. (Note the two conditions below are
  the same condition stated from either end: if the narrowing set is empty, every
  configured reviewer either approved or was dropped, so "nobody approved" and
  "every configured reviewer is dropped as failed" describe one state.)

  - **At least one reviewer approved → confirming round.** The confirming round's
    set is the full configured list, not the narrowing set, so it is not empty, and
    the reviewers that approved are known to run. Their approvals are provisional
    and the tree moved under them while the narrowing phase settled findings —
    which is the entire reason phase 3 exists. The failure-dropped reviewers get a
    third attempt as a side effect of that round running everyone; that is not a
    new indulgence, because the confirming round re-runs a failure-dropped reviewer
    in every scenario, so this case costs nothing the design has not already
    accepted.
  - **No reviewer approved → terminal report, no confirming round.** There is no
    approval to confirm, so phase 3 has no job here. And the round would consist
    entirely of reviewers that failed twice each, while a single failure in the
    confirming round stops the loop (Step 4) — so it cannot end anywhere but the
    report, after paying a full round to get there. Report instead, naming every
    reviewer as failed with its last reason verbatim. This is not an exotic path:
    it is what one configured reviewer with a dead model id does, failing the
    opening round and one narrowing round, which is the whole loop. Say that this
    is the **only** case in which the confirming round is skipped, so it does not
    read as contradicting the rule below that it is otherwise mandatory: that rule
    exists to stop an approval being trusted after the document moved under it, and
    here there is no approval to protect.

- Spell out the unanimous-opening case rather than leaving it to be inferred from
  the narrowing set being empty. Two ways it goes wrong quietly:

  - **It skips the narrowing phase; it does not end the loop.** The confirming
    round is mandatory even here. ADR-0026 accepts the cost of two full-reviewer
    rounds precisely so that no run ends on an approval given to a document that
    has since changed — and on a unanimous opening round the document has not
    changed, which is the one case where the confirming round is cheap and
    obviously redundant, and therefore the one most likely to get skipped.
  - **A phase with an empty reviewer set is skipped, never run, and the narrowed
    list is never written empty.** An empty set cannot be expressed through the
    config at all, and both ways of trying it end badly. `devgeta config set
review.reviewers` with no values is rejected outright (`requireAtLeastOne`,
    `cmd/config_settings.go:89`), so the round would die on a config error rather
    than skip. And clearing the key some other way — `devgeta config unset`, a
    hand-edit — does not run nobody either: `resolveReviewerRuns`
    (`internal/tooling/task/reviewrun.go:609`) treats an unset or all-blank
    `review.reviewers` as one reviewer on OpenCode's own default model, so the
    round runs a model the human never configured and reports its verdict as if
    it were theirs.

- Keep the existing "stop for anything escalated" rule ahead of the cap check.

#### Step 7: Terminal report carries the new facts

- The rounds table gains a phase column and names the reviewers that ran.
- A new required line in both templates: the current value of
  `review.reviewers`, stated whether or not the loop changed it. This is the second
  of the two places the loop reports on the key — step 2 above
  puts the restore command on screen before the first mutation, precisely
  because a run that never reaches a terminal report would otherwise say it
  nowhere. Repeating it here is not redundancy: the report is where a human
  reading only the outcome sees it, and it also reports the current value, which
  step 0 cannot know yet. The current value is read with `devgeta config get review.reviewers`,
  like every other thing the loop learns about the key — and because it is a `get`
  the report **displays**, it is displayed **through `LC_ALL=C sed -n l`** rather
  than by running the `get` bare (Step 2), which the restore command beside it is
  not. Say that in the template so the difference reads as deliberate: the value is
  there to be read, the command is there to be pasted, and this is the one line in
  the report where an entry's own bytes could otherwise erase the rest of it.
- **Beside that value, every exit path prints exactly one of two things: the recorded
  restore command, or the reason there is none.** The requirement is total — no exit
  path may be silent about the key, because a human always has to learn whether the
  loop touched their config — but it is not unconditional, because there are exit
  paths in which the loop must print no command at all. Which branch a report takes
  turns on one question the loop has already answered before its first round: does it
  hold a record it proved and accepted?

  - **It does** — the join and single-line checks passed, every entry is one
    `config set` can write back, and none holds a control byte (Step 2). Print the
    recorded `devgeta config set review.reviewers <entry> <entry> …`, entries
    single-quoted and **literal**, because this is the copy that exists to be pasted;
    it is the same string step 2 printed before the first mutation, quoted the same
    way. Print it whether or not any narrowing write actually happened — a
    one-reviewer set never writes the key and the command is still the right thing to
    have on screen — and say which of the two it was.
  - **It does not.** Four exit paths land here: an unprovable record, an entry
    `config set` cannot write back, an entry holding a control byte, and an unset
    `review.reviewers` (Step 2). Print **no command**, and in its place state that
    `review.reviewers` was never written this run, so the value printed above is the
    user's own, untouched — together with the reason narrowing was off (next bullet),
    or, for the unset key, that one reviewer ran on OpenCode's default model and the
    key is still unset.

  Say in the template why this branch prints no command rather than a best-effort
  one, or the unconditional version gets restored and the contradiction comes back. A
  command built from an unproved record writes a list the user may never have had —
  in the collapsed-duplicate case (§6 check 10) it would tell them to paste away one
  of their own two entries. A command built from an unwritable entry fails on paste.
  A command for an unset key has no arguments and is rejected outright (Step 2). And
  a command built from an entry holding a control byte cannot be printed both safely
  and correctly at once: literal bytes let the entry rewrite the very line meant to
  recover the config, and escaping them makes the paste write the wrong list — which
  is exactly why a control byte refuses narrowing instead of being rendered (Step 2,
  ADR-0026). In all four the key was never written, so a restore command would be
  answering a question nobody has. This is the same branch Step 2 already takes for
  the pre-mutation print, which prints no restore command when any refusal fires: one
  rule, stated in both the places it applies.

  Note the invariant that keeps the first branch safe, so nobody adds an exception it
  does not need: **the record is proved and accepted once, strictly before the first
  write** (Step 2), so every exit path that follows a write holds a record already
  screened for control bytes. A control-byte entry that someone writes into the key
  mid-run is caught by the pre-write or post-round check and displayed as the found
  value through the filter; it never enters the recorded command, which still prints
  literally and is still correct.

- When any Step 2 check turned narrowing off, the report says which one and names
  what tripped it — the entry `config set` cannot write back, the entry holding a
  control byte, the `get` line and the joined labels that did not match, the round
  before which the key no longer held the record, the round
  after which the key no longer held the narrowed list, or the round whose write
  reported replacing a value the loop never recorded — so the extra cost is
  explained rather than looking like the narrowing silently failing. The first three
  of those are refusals on the record itself, so they take the no-command branch
  above; the last three refuse part-way through a run on a record that was already
  accepted, so they take the first branch and repeat the recorded command (see the
  bullet after this one). Print both
  strings in any mismatch case: they are two short lines, and they are the whole
  diagnosis. Every one of those strings is made visible on the way out — the ones the
  loop composed in the escaped rendering, the ones read back out of `get` through the
  filter (Step 2). The control-byte case is the plainest reason why, since the value
  being named is a value that would otherwise rewrite the line naming it. Say in the
  template that this covers the report and not the whole run: `review-run` already
  printed those same entries raw in the opening round, and the report cannot reach
  back and fix that (§3's residual).
- The three cases where the key changed under the loop need one more sentence than the
  other refusals, because they are the only refusals about the **user's value** rather
  than about the record's shape: name the round it happened in, say which side of the
  round the loop noticed on, say what it wrote and what it left as found, and print the
  recorded restore command next to it so the human can put the original list back if the
  change was not theirs. Two of the three need their own sentence rather than that generic
  one. If a **post-round** mismatch found the record itself, the narrowing write never
  landed, so there is nothing to put back (Step 2). And if a write's captured
  `<key>: <previous> -> <new>` output did not match the line the loop composed for it, the
  write landed on a value the loop never recorded — the one-command race the pre-write
  check cannot close (Step 2): say that the value is gone, print the captured line through
  the filter, and say plainly that the loop cannot reconstruct it because that string is
  ambiguous. Say also that the round's normal restore still put the record back, so the key
  is not left narrowed. State the limit in the same breath, since a report that implies
  this check is airtight is worse than one that does not mention it: a replacement whose
  printed form is identical to the expected line is **not** detected at all, so a run that
  reports nothing here is not proof that nobody wrote the key (Step 2).
- Keep `--ratify` / `--reopen` **after** the `## Terminal report` heading and
  nowhere else.
- Verify: `go test -run TestReviewLoopOnlyInvokesRatifyOrReopen ./internal/apps/opencode/`

#### Step 8: New guard tests

- `TestReviewLoopRestoresReviewerConfig` — the file names the recording step, a
  restore, and the restore command in **both** places it is required: before any
  narrowing write, and the report section. Anchoring the first one matters most,
  because it is the only one an interrupted run ever reaches. Anchor the other half of
  that rule too, since it is the half a later edit is likely to drop as an
  inconsistency: that both places print **no** restore command when the loop holds no
  proved record, and say the key was never written instead (Step 7). It also anchors the
  rules that keep the restore honest rather than merely present: that each entry is
  passed as one quoted argument, that narrowing is refused outright when the recorded
  list holds an entry `config set` cannot write back, that it is refused when the
  joined labels do not match the `get` line, that it is refused when a recorded entry
  holds a control byte, that a value the loop composes is escaped rather than printed
  raw, that a displayed `get` goes through `sed -n l` rather than being run bare, that a
  narrowing write happens **only** while the key still holds the recorded list, that
  the restore happens **only** while the key still holds the narrowed list the loop
  wrote, and that a write's own output is captured and compared whole rather than split
  at `" -> "`.
- Anchor one more thing in that test, as a **negative**, and anchor it precisely:
  the shipped body must contain neither of the two literal strings
  `global_config.yaml` and `.config/devgeta` — two separate case-sensitive
  `strings.Contains` assertions over the **whole** file, not over one section, so the
  failure names which literal appeared. Both are needed: the filename alone misses
  `~/.config/devgeta/*.yaml`, and the directory alone misses
  `$XDG_CONFIG_HOME/devgeta/global_config.yaml`, since that variable need not expand
  to anything containing `.config`. Between them they cover every path-shaped way to
  reach the config, which is what the guard is actually for — the rule here most
  likely to be undone by someone reaching for the "obvious" source of the list, and
  the one rule a substring check can enforce properly rather than merely gesture at.
  It over-catches on purpose: a bare mention fails too, which is why Step 2 requires
  the rationale to identify that file descriptively instead of by name or path. Put
  both halves in the test comment — §2's permission facts, and that wording rule with
  a pointer to Step 2 — because the shipped command may not carry either (§12), so
  the test is the only place a puzzled implementer can read why the assertion exists
  and how to satisfy it.
  What none of it can catch: an executing agent skipping the restore, printing the
  command after the mutation instead of before it, running the checks and ignoring
  the answer, quoting the entries in the prose while interpolating them bare in
  practice, or naming the escaped rendering and then echoing a value raw. And what no
  test or prose can reach at all: the labels `review-run` prints raw before the loop
  reads them (§3's residual) — the assertions here bound the loop's own output, not
  everything a round puts on screen.
- `TestReviewLoopCleanApprovalRequiresConfirmingRound` — step 3 names the
  confirming round as a condition. Same substring-over-prose limitation as the
  existing guards; document it in the test comment the way they do.
- Verify: `go test ./internal/apps/opencode/`

#### Step 9: The spec and full verification

**Update `docs/spec.md` first.** It is [CLAUDE.md §2](../../../CLAUDE.md)'s first
source of truth and [§12](../../../CLAUDE.md) says user-facing behaviour is
documented there, so shipping the skill change without it does not leave a doc
chore behind — it leaves a spec that states the opposite of what the loop does.
The sentences that go stale, each with what it becomes:

- The `/review-loop` paragraph (`:783-793`), four edits:
  - "repeats — up to `review.rounds` rounds (default 3, max 5)" — the cap is per
    **phase** now. Say so and give the worst case in rounds (cap + 2), so the
    ceiling stays a number the reader can compute rather than an open-ended one.
  - "a clean approval (every reviewer APPROVEs, …)" — add the condition that makes
    it true: the round has to be one that ran **every** reviewer, and an approval
    from a narrowing round is provisional.
  - "or a report to the human (persistent disagreement, an open finding not yet
    settled, the round cap, **a reviewer failure**, or an unratified rejection)" —
    a single failure no longer reports. It is retried and keeps the reviewer in the
    narrowing set; what reports is two consecutive failures (the reviewer is dropped
    and named as failed), every reviewer failing, or a failure in the confirming
    round.
  - The paragraph says nothing about the phases or about the loop **rewriting
    `review.reviewers` and putting it back**. Both belong there: name the three
    phases, the per-round restore, and the interruption residual (§3). This is a key
    the user owns, and a user who finds it changed has to be able to learn why from
    the spec.
- The `review.rounds` key (`:345`) — "max review rounds `/review-loop` performs
  before settling on a verdict" becomes per phase, with the same worst case named.
  Default `3` and the validated `1`–`5` range are unchanged (§4).
- That key's example (`:538`), whose comment reads `# Cap /review-loop at 5 rounds`
  — per phase.
- The `review.reviewers` key (`:344`) — one sentence that `/review-loop` narrows
  this key mid-run and restores it, pointing at the `/review-loop` paragraph for the
  detail. Someone reading the key's own entry is the person most likely to be
  looking for that.
- `/pr-review-loop`'s "PR-side counterpart to `/review-loop` — the same cross-model
  reviewers … with two deliberate differences" (`:988-992`) — running every
  configured reviewer on every tick is now a third difference. Fix it **in
  `docs/spec.md` only**; `configs/shared/commands/pr-review-loop.md` stays out of
  scope (§4).

**Do not edit `review-run`'s table row (`:753`), or anything else describing
`review-run` itself.** Its contract is unchanged: one invocation is still one round
of every model configured _at that moment_, and "the round cap lives in the
agent-side loop, not here" is still exactly right. All the narrowing lives in what
the loop stores in the config before it calls the command. Rewriting the command's
row while meaning to rewrite the loop's is the one way this edit does damage.

Then:

- `go test . ./internal/apps/opencode/ ./internal/apps/claude/` — the root
  package is required here because `configs/` changed. The `docs/spec.md` edit adds
  nothing to that list: no Go file embeds or reads `docs/**` (the only mentions are
  comments and test fixtures), so it is verified by reading — §6's Docs checks.
- Mark this cycle Done.

---

## 6. Verification Plan

**Precondition — check it before running any of this.** Step 0's gate must already be
satisfied: the decision is recorded in ADR-0026, and §8 records the maintainer's
explicit go-ahead on this plan. Do **not** substitute a `grep` for `ACCEPTED` on
ADR-0026's status line — that status stays `PROPOSED` until this work ships, for the
reason Step 0 gives — and do not treat green tests as approval: they do not make an
unapproved cross-repo change approved.

### Automated

```bash
go test . ./internal/apps/opencode/ ./internal/apps/claude/
make lint
```

### Docs

Step 9's `docs/spec.md` edit is checked by reading — nothing in Go embeds or reads
`docs/**`, so no test covers it and a stale spec fails no build:

```bash
git diff docs/spec.md
grep -n 'per phase' docs/spec.md
grep -n 'review-run' docs/spec.md
```

- The `/review-loop` paragraph names the three phases, states the cap as per phase
  with the worst case in rounds, makes a clean approval conditional on a round that
  ran every reviewer, stops listing a single reviewer failure as terminal, and says
  that the loop rewrites and restores `review.reviewers`.
- `per phase` hits both the `/review-loop` paragraph and the `review.rounds` key
  entry, and no line still describes `review.rounds` as capping the whole loop.
- The `review.reviewers` key entry mentions the narrowing; the `dg config set
review.rounds 5` example's comment no longer says "at 5 rounds" flat.
- **No hunk of that diff lands in `review-run`'s own table row or any other
  `review-run` description** — the command's contract did not change, and quietly
  rewriting it here would make the spec wrong in the opposite direction.

### Manual

The only honest end-to-end check is running the loop, so run it on a branch with
a real disagreement:

1. Configure two reviewers where one reliably approves. Run `/review-loop`.
   Confirm the opening round runs both, the narrowing rounds run only the
   non-approver, and the final round runs both again.
2. During a narrowing round, check `~/.config/devgeta/global_config.yaml` after
   the round returns: `review.reviewers` must already be back to both entries.
   This is **your** read, not the loop's — which is the other half of the check.
   Confirm the transcript shows the loop reaching the key only through
   `devgeta config get` / `devgeta config set`, and that no
   `external_directory` / "read outside the project" prompt or auto-reject appears
   anywhere in the run. One of those in the log means the file read crept back in and
   the loop cannot run headless.
3. Interrupt the loop mid-narrowing (Ctrl-C). Expect **no** terminal report — the
   process is gone and cannot print one; a report here would mean the interruption
   was not real. The check is that the restore command the loop printed after the
   opening round — before the first narrowing write — is still on screen, that
   `review.reviewers` is in fact still narrowed, and that running that exact command
   restores it. **This is the known weak point** (ADR-0026): the test is that
   recovery is already stated, not that the narrowing cannot happen.
   Then re-run `/review-loop` **without** restoring first, and confirm it records
   the narrowed list as its baseline and says nothing about it. That is the
   residual §3 names, and seeing it once is what stops it being re-litigated as a
   bug: closing it takes the `--models` flag, not more prose.
4. Point one reviewer at a nonexistent model id, keeping one that works. Confirm it
   is retried once, dropped after the second consecutive failure, and named as
   failed in the report rather than blocking every round — and that the run still
   reaches the confirming round, because the working reviewer's approval is a
   provisional one that has to be re-checked.
5. Then point **every** configured reviewer at a nonexistent model id. Confirm the
   opposite destination: the loop stops after the second consecutive failure and
   reports, with **no** confirming round, naming each reviewer as failed with its
   last reason. A confirming round here would be a full round of reviewers already
   known to be broken.
6. Confirm a narrowing-round approval alone never prints the clean-approval
   template.
7. Point `XDG_CONFIG_HOME` at a scratch directory so your real config is never at
   risk, then hand-edit `review.reviewers` there to add a blank list item. Confirm
   the loop never writes `review.reviewers`, runs the full list every round, and
   names the failed check as the reason narrowing was off — not that it narrows and
   then restores a shorter list. This one is caught by the join check, since a blank
   item never reaches a label; check 9 covers the other refusal.
   Confirm the report prints **no** restore command and says the key was never
   written instead (Step 7). This is the shape all three refusals share, so checks 9,
   10 and 13 assert it too: the record here has the blank item dropped, so a command
   built from it would tell you to paste away part of your own value.
8. In that same scratch config, replace the list with three entries — one model that
   actually works, plus two crafted ones that leave a **file behind** if a shell ever
   runs them:

   ```bash
   devgeta config set review.reviewers 'anthropic/claude-opus-4-6' \
     'anthropic/a; touch /tmp/dg-review-loop-executed-semicolon' \
     'anthropic/b$(touch /tmp/dg-review-loop-executed-subshell)'
   rm -f /tmp/dg-review-loop-executed-*   # start from a clean slate
   ```

   Both crafted entries are values the validator accepts — it wants only an interior
   `/` (`cmd/config_settings.go:395`) — and both pass the join, single-line and
   control-byte checks, so **narrowing actually happens**, which is the whole point of
   the setup. The working model approves and the two crafted entries fail, so they are
   the narrowing set: their text goes onto the narrowing write's command line, onto
   the restore's, and into the printed restore command. The third entry is what makes
   the narrowing set a strict subset; with only the crafted two, nobody approves,
   nothing is written, and the check would exercise no quoting at all. The two
   payloads fail differently on purpose: the `;` one fires only if an entry is
   interpolated unquoted, while the `$( )` one fires under double quotes too, which is
   the near-miss single-quoting exists to prevent. The path is a literal `/tmp/…` and
   must stay one — a `$TMPDIR` in the entry would expand to nothing in whatever shell
   ran it, the `touch` would fail, and the check would pass on a broken loop.

   **The assertion is the two files, not the output.** When the run ends,
   `ls /tmp/dg-review-loop-executed-*` reports no such file: nothing executed either
   entry. Then confirm `devgeta config get review.reviewers` prints all three entries
   back unchanged and in order, and that the restore command the loop printed
   reproduces exactly that when pasted into a shell by hand.

   **Expect both entries to appear on screen, and do not read that as a failure.**
   `review-run` builds every output line as `<label> → <outcome>`
   (`reviewrun.go:306`), and for a configured model the label **is** the config entry,
   so every round prints `anthropic/a; touch /tmp/… → ERROR(…)` — and
   `[1/3] anthropic/a; touch /tmp/…: running` on stderr (`reviewprogress.go:137`) —
   whether or not anything ran. An earlier draft of this check asserted that no round
   prints the entry's own words; that assertion fails a **correct** implementation,
   which is worse than no check at all. Same residual as check 13, for the same reason
   (§3, §7).

   A second symptom usually lands with the first, and it is worth watching for
   because it appears before the run ends: whichever payload fired also **truncated
   the command it fired in** — an unquoted `;` ends the `config set` early, and a
   `$( )` that ran substitutes to nothing — so the key is left holding a shorter list
   than the loop composed, which its own captured-output comparison should report as a
   mismatch. If you also want the plain word-splitting case, add an entry with a space
   and no metacharacter (`'anthropic/a b'`): it splits into `anthropic/a` and `b`, `b`
   has no interior `/`, and `config set` rejects the whole list. A marker file, a
   truncated list, or a refused write — any of the three is this check failing.

9. Still in the scratch config, hand-edit an entry to `noslash`. This is the mirror
   case: it reaches a label unchanged, so the join check **passes** and the
   restorability check is the one that must refuse. Confirm narrowing is off for the
   whole run and the report names that entry — and prints no restore command, since a
   command carrying `noslash` fails the moment it is pasted (Step 7).
10. Last, configure the **same** model twice (`devgeta config set review.reviewers
'anthropic/a' 'anthropic/a'`, which the validator accepts). Confirm the opening
    round runs it once — `resolveReviewerRuns` collapses duplicates — that the join
    check therefore fails, that narrowing is off, and that the config still holds
    **two** items when the run ends. Restoring the collapsed record here would have
    silently deleted one of them, which is the exact class of damage the proof
    exists to prevent — so the report must print no restore command either, because a
    printed one is a restore the human performs by hand and does the same damage
    (Step 7).
11. Back in a two-reviewer scratch config, let the loop reach a narrowing round and,
    **while that round is running**, change the key from another shell:
    `devgeta config set review.reviewers 'anthropic/a'`. Confirm the round's restore
    does not fire — the key still holds `anthropic/a` when the round returns, not the
    recorded pair — that every later round runs your list, and that the report names
    the round it happened in, prints the narrowed value it expected next to the value
    it found, and repeats the recorded restore command. Overwriting your edit here is
    the failure this check exists for.
12. Same two-reviewer scratch config, but change the key **between** rounds instead of
    during one: let a round finish, and while the loop is settling that round's findings
    run `devgeta config set review.reviewers 'anthropic/a'` from another shell. Confirm
    the next round does **not** narrow — the pre-write check fails, so no `config set` is
    issued at all — that the key still holds `anthropic/a` when the run ends, that every
    later round runs it, and that the report names the round and prints the record beside
    what it found. An overwritten value here, or a report claiming the restore succeeded,
    is the failure this check exists for. This is the widest of the three windows: it
    spans every fix the previous round dispatched, so it is the easiest to hit by
    accident and the one check 11 does not cover.
13. In the scratch config, set one entry to `anthropic/a` followed by an ESC-bracket
    sequence that clears the line, and another to `anthropic/b` followed by a
    carriage return and the words `all reviewers approved`
    (`devgeta config set review.reviewers "anthropic/a"$'\e[2K' "anthropic/b"$'\r'"all
reviewers approved"` from bash or zsh). Both are values the validator accepts and
    both pass the join and single-line checks, which is the point of the check. Check
    the four things the loop can actually control: the step-0 `get` line arrives
    through `sed -n l`, showing `\033` rather than blanking the line; every value the
    loop itself writes — the recorded entries, the entry it names as the reason, the
    current value in the report — appears with its control bytes visible and no line
    of the loop's own output is blanked or overwritten; narrowing is off for the
    whole run, the report names those entries as the reason, and the config still holds
    both entries byte-for-byte at the end, since a refused run writes nothing; and
    **no `devgeta config set review.reviewers` line appears anywhere in the run or the
    report**, which says instead that the key was never written (Step 7). That last
    one is what this check exists for beyond the display rules: a pasteable command
    here would have to carry the raw ESC into the one line whose job is to recover the
    config, and an escaped one would paste the wrong list — the contradiction Step 7's
    two-branch rule removes by printing the reason instead.

    **Expect the opening round's own lines to be mangled, and do not read that as a
    failure.** `review-run` prints `anthropic/a\e[2K → APPROVE` on stdout and
    `[1/2] anthropic/a\e[2K: running` on stderr, both raw and both before the loop has
    a record to refuse (§2), so those lines will blank or overwrite themselves and no
    prose can prevent it — it is the residual §3 and §7 name, and seeing it once is
    what stops it being re-litigated as a bug. Closing it takes a Go change to what
    `review-run` prints (§4). What must still hold with those lines scrambled: the loop
    reports the refusal, and the config survives untouched.

    Read the output in a real terminal, not a log file — a pager or an editor may
    neutralise the escapes on its own and hide the failure this check exists for.

14. In the scratch config, set two entries where one contains the same separator
    `config set`'s own output uses: `devgeta config set review.reviewers 'anthropic/a -> b' 'openai/b'`.
    Both are valid (the validator wants only an interior `/`), both reach labels
    unchanged, so narrowing proceeds normally — which is the point. Confirm the run
    narrows and restores exactly as it does with ordinary entries, and that **no**
    report claims the key changed under the loop. A report of a mismatch here is the
    failure this check exists for: it means the loop split a previous value out of
    `<key>: <previous> -> <new>` at the first `" -> "` instead of comparing the whole
    captured line, which invents a change nobody made. The mirror direction — a
    concurrent write forging a match — cannot be provoked by hand, because it needs a
    write landing inside a one-command gap; it is covered by the wording rule in
    Step 2, not by a check.

### Regression Check

- `/review-loop` with a single configured reviewer still works: the opening round
  is the whole set, so narrowing is a no-op and the confirming round repeats it.
  Check that `review.reviewers` is never written during the run at all — a
  one-reviewer set has nothing to drop.
- `/review-loop` with `review.reviewers` unset still runs one reviewer on
  OpenCode's default model, and the key is still unset when the run ends. Nothing
  may try to write that reviewer back as a `provider/model`, and nothing may run
  `devgeta config unset`. The report takes Step 7's no-command branch here for the
  same reason: it says the key is unset and nothing was written, and prints no
  `devgeta config set review.reviewers` line, since one with no arguments after it is
  rejected outright rather than being a restore anyone could run.
- `--reviewer` and `--note` still reach every round (guarded, but check by hand).
- `/pr-review-loop` is untouched and still behaves as before.

---

## 7. Risks & Trade-offs

| Risk                                                               | Likelihood   | Mitigation                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ------------------------------------------------------------------ | ------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| An interrupted loop leaves `review.reviewers` narrowed             | **High**     | Per-round restore shrinks the window to one command; the write happens only on rounds that actually drop a reviewer, so a one-reviewer set or a round where nobody approved never opens the window; the loop prints the restore command **before** the first mutation (after the opening round, which writes nothing), and the report repeats it — or, in the exit paths where the loop holds no proved record and so has no command to print, states that the key was never written instead, so no report is ever silent about the key and none is forced to print an unsafe or unrunnable command (Step 7). **Not eliminated:** an interrupted run emits no report, the config stays narrowed until someone runs that command, and the next run takes the narrowed list as its baseline. Only `--models` closes it — see ADR-0026                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| A restore writes a different list than the user had                | Med          | Step 2's record is the opening round's verdict labels, and it is unusable until two commands prove it is byte-identical to the configured list: `get` output on one line, and joined labels equal to it. Trimming, a dropped blank and a collapsed duplicate all shorten the join, so every way the labels can differ from the stored list fails the check. Any failed check — that one, an entry `config set` cannot write back, or an entry holding a control byte — turns narrowing off for the whole run, so there is no restore to get wrong                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| The record's source is unreachable in a headless run               | Med          | Reading `global_config.yaml` would need an `external_directory` grant neither agent ships, and auto-rejects headless (§2) — four such rejections in the motivating run. So the loop only ever uses `devgeta config get` / `set`, both plain commands under the agents' `bash` policy, and both already listed in `## What this drives`. Step 8 anchors that neither the file's name nor its directory (`global_config.yaml`, `.config/devgeta`) appears anywhere in the shipped command, so a path-shaped read cannot be written back in without failing the build; the command still explains the ban, naming that file descriptively so the guard stays an exact substring check (Step 2)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| A restore overwrites a change the user made while a round ran      | Low          | The restore is conditional, not routine: before writing the record back the loop re-runs the two checks against the **narrowed** list, and a mismatch leaves the value exactly as found, turns narrowing off for the rest of the run, and is named in the report with both strings. The comparison is exact because the loop composed and wrote the narrowed list itself and `Set` stores arguments verbatim, so the expected `get` output is computable rather than inferred                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| A narrowing write overwrites a change the user made between rounds | Low          | The record is proved once but used many minutes later — the journal read and every fix subagent of the previous round sit in between, which is more wall time than a round takes — so the key is re-checked against the record immediately before **every** narrowing write, not once after the opening round. A mismatch means no write, no narrowing for the rest of the run, and both strings in the report. Without it the post-round check is not merely insufficient here, it is misleading: the loop's own write makes the key match the narrowed list, so that check passes and the restore then writes the stale record over the user's list. **Not eliminated:** check and write are two commands and prose cannot fuse them (`internal/config/lock.go`'s lock is per-`config set` invocation and nothing compares-and-sets), so a `set` landing in that one-command gap is still overwritten. Partly mitigated, and labelled as partial: `config set` reports the value it replaced, read inside the same lock as the write, and the loop captures that output and compares the **whole** string against a line it composed — never cutting a previous value out at the first `" -> "`, which an entry may itself contain (§2) — then names it, filters it, and stops narrowing. It cannot put it back, because that string is `get`-joined and ambiguous (§2). **And it cannot see the case where the joined form is unchanged** — one entry `anthropic/a, openai/b` prints what two print — so that write is silently overwritten and the check reports success; §3 says so, and only `--models` closes it |
| A reviewer entry's own text splits or executes as shell syntax     | Low          | `isProviderModelShaped` (`cmd/config_settings.go:395`) requires only an interior `/`, so spaces, `;`, `$( )` and backticks are all legal in a stored entry. Step 2 requires every entry to be one quoted argument in the narrowing write, the restore, **and** the restore command printed for a human to paste. §6 check 8 observes it as an **artefact** — an entry whose execution would create a file, absent afterwards — because absent output cannot be observed here: `review-run` prints the entry verbatim either way (`reviewrun.go:306`), so "no round printed it" would fail a correct loop                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| A reviewer entry's own text rewrites the report it appears in      | Low          | Quoting stops a shell, not a terminal: `Set` filters no characters, `yaml.v3` preserves them and `get` prints through a bare `fmt.Fprintln`, so a stored ESC or `\r` reaches the screen — and passes both proof checks, since neither is `\n` and `TrimSpace` ignores non-whitespace control bytes (§2). Step 2 covers the report by two routes: values the loop composes go out in the escaped rendering, and a config value the loop did not compose — a displayed `get`, or the previous value `config set` prints — is captured or piped through `sed -n l` instead of being emitted bare, so its raw bytes never leave the pipe. The control-byte refusal keeps the one string that must stay literal — the pasted restore command — from ever carrying one. Residual: a printable bidirectional override still pastes literally, and the escaping is prose-bound like every other rule here                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| A reviewer entry's own text mangles `review-run`'s own output      | **Accepted** | Not fixable prose-side, so it is not promised away (§3). `review-run` prints the label raw on stdout (`reviewrun.go:306`) and on stderr (`reviewprogress.go:137`), before the loop has read a line — so before the record exists and before any refusal can fire. The loop cannot filter them either: it must read the verdicts first-hand (`review-loop.md:86-87`), the stderr heartbeat is live on purpose, and a byte-level filter rewrites the `→` the lines are parsed on. Bounded rather than removed: it costs one scrambled opening round, the config is never written, and the report that follows is escaped/filtered and readable. Escaping at the source is a Go change — §4, Out of Scope, named the way the `--models` flag is                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Edits break one of the four existing guard tests                   | Med          | Anchor table in §2; each step names the guard to re-run                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| Per-phase cap makes a worst-case run much longer                   | Med          | Bounded: cap + 2 rounds. Named in the report so cost is visible                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Prose guards give false confidence                                 | **Accepted** | A substring check proves a concept is still named, never that it is obeyed. Every new test says so in its comment, as the existing four do                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |

### Trade-offs Made

- **The gate is the maintainer's recorded go-ahead, not ADR-0026's status word** — an
  earlier draft gated Step 1 on the status reading `ACCEPTED`, which cannot work here:
  [docs/decisions/README.md](../../decisions/README.md) defines `ACCEPTED` as decided
  _and implemented_, so the gate demanded a status that only the gated work could make
  true. Two other options were available. Adding a "decided, not yet built" state to
  that README's table is the cleaner long-term answer and was left to the maintainer —
  it redefines every ADR's status in the repo, which is a bigger call than this branch.
  Redefining `ACCEPTED` locally in ADR-0026 was rejected outright: a status word that
  means one thing in one file is worse than no status word. So the gate names the thing
  that can actually be true beforehand — the approval itself — and §8 is where it is
  written down.
- **Config mutation over a `--models` flag** — the maintainer's call; the risk it
  keeps is recorded in ADR-0026 and in the table above.
- **Two mandatory full rounds** — the cheapest clean approval costs slightly more,
  in exchange for never trusting an approval given to a document that has since
  changed.
- **Two consecutive failures before dropping** — one retry might be enough and
  would be cheaper; two is chosen because all three observed failures were
  transient and a single retry could still drop a reviewer that was about to work.
- **A control byte in an entry costs the whole run's narrowing, rather than being
  quoted around** — bash and zsh's `$'…\e…'` would keep both the money and the safe
  display, and it was still rejected: a second quoting form on top of the first, wrong in
  POSIX `sh`, and needed in three separate places, all to keep narrowing on a config
  that is one byte away from malformed. Refusing costs rounds on a config nobody has;
  getting the second quoting form subtly wrong costs the config itself.
- **A terminal-safety promise cut back to the loop's own output** — the first draft of
  this plan said every displayed reviewer entry is escaped, which no prose-only change
  can deliver: `review-run` prints the label raw on stdout and stderr before the loop
  reads anything (§2). Rather than keep a promise the plan cannot hold, the rule now
  covers the two things it does control — values the loop composes, and any `get` it
  displays, which goes through a filter instead of being run bare — and the rest is
  named as a residual in §3 and §7. Escaping at the source would need a Go change,
  which the prose-only decision rules out; the alternative of piping the round's own
  output through the same filter was rejected because it mangles the `→` the verdict
  lines are parsed on and defeats the first-hand-reading and live-heartbeat rules the
  skill already has.
- **Detection over repair for the one-command check-to-set gap** — the loop could try to
  put back the value `config set` reported it had replaced, and must not: that string is
  `strings.Join(entries, ", ")`, so splitting it to rebuild a list is the exact mistake
  that corrupted the config in the motivating run (ADR-0026). Naming the lost value and
  refusing to guess is worth less than closing the gap, and closing it takes the
  `--models` flag rather than more prose.
- **Partial detection, labelled as partial rather than dropped or oversold** — that same
  `", "` join means the detection above misses any replacement whose printed form equals
  the expected line, so it is not a guard, it is a chance of noticing. Two other options
  were on the table. Dropping the check entirely is cleaner to describe and loses the
  cases it does catch, which include every list the user is likely to type. Keeping it
  worded as a guard was rejected outright: a step that presents itself as dependable when
  it is not is the "hides or defers the failure" that [CLAUDE.md §4](../../../CLAUDE.md)
  rules out, and it would make a silent report read as an all-clear. So the check stays and
  the report says what its silence does not prove.
- **No narrowing at all on a config the loop cannot prove** — a duplicated entry, or
  one with stray whitespace, is enough to make the whole run a full-list run, and
  those configs are behaviourally identical to their normalised form
  (`resolveReviewerRuns` trims and dedups before running anything). Normalising them
  on restore would be safe for the reviewers and still wrong: it edits a file the
  user owns without being asked, which is the same objection that made narrowing fail
  closed in the first place. Paying for extra rounds is the cheaper mistake, and the
  report says which config caused it so the user can fix it in one command.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope actually locked?
- [ ] Steps actionable?
- [ ] Verification executable?
- [ ] Risks realistic?

**Maintainer go-ahead (Step 0's gate):**
**Given 2026-08-12**, after the cross-model review below reached a clean approval. The
maintainer's words: _"and after all green approve today's cycles and today's adrs"_ —
issued as the argument to `/review-loop`, conditional on the review going green, which it
then did.

That satisfies the second half of Step 0's gate: the decision is recorded (ADR-0026) and
the maintainer has said to proceed. **Step 1 may begin.**

It does **not** authorize a status change on any ADR. ADR-0026 stays `PROPOSED` until the
work below ships, for the reason the paragraph after this one explains — the same reason
the earlier attempt was reverted. The status flip is the last step, and the maintainer's.

On 2026-08-12 the maintainer, acting on Step 0, asked for the ADRs to be accepted, and
ADR-0024 and ADR-0026 were both edited from `PROPOSED` to `ACCEPTED`. Both edits were
reverted, because [docs/decisions/README.md](../../decisions/README.md) defines
`ACCEPTED` as decided **and implemented** and neither cycle has been implemented —
ADR-0024's plan
([2026-08-12-ws-pane-selection-width-and-refresh-load.md](2026-08-12-ws-pane-selection-width-and-refresh-load.md))
is also still Draft with all sixteen of its scope items unchecked. Step 0's gate was
reworded in the same change so it no longer asks for a status that only the gated work
could make true. The instruction is recorded here so it is not lost, and so the statuses
are not flipped back without the README gaining a state that fits; whether it counts as
the go-ahead above is the maintainer's to say.

**Reviewer notes:**
Reviewed by `github-copilot/gpt-5.6-terra` and `github-copilot/gemini-3.6-flash` via
`/review-loop --reviewer document` over seven rounds on 2026-08-12, ending in a clean
approval from both. **25 findings: 24 fixed, 1 rejected and ratified by the maintainer.**
The journal is the record (`devgeta task review-notes`); note that 25 of its settled notes
cite this ADR as "ADR-0025", the number it carried before `origin/main` took 0025 for an
unrelated decision — settled notes cannot be rewritten.

Four findings changed the design rather than the prose, and are worth knowing before
implementing: the failure-retry rule was unreachable until step 3's no-finding stop was
split (n2); `config get` cannot serve as the reviewer-list record (n5); the loop cannot
read the config file at all, for permission reasons (n11/n12); and two later findings were
collisions _between_ earlier fixes rather than gaps in the original plan (n21, n25) — a
signal that this plan has been revised more than it has been built.
