# Output-budget hook and runner contract

> **Status: not implemented.** This is the binding contract for the
> output-budget feature planned in
> [docs/plans/cycles/2026-08-25-token-and-context-efficiency.md](../plans/cycles/2026-08-25-token-and-context-efficiency.md),
> which is approved but not yet started. Nothing here ships yet. It lives outside the cycle doc
> because the contract outlives the cycle: once built, this is the reference for
> `configs/claude/output-budget.sh`, `configs/opencode/plugin/output-budget.js`,
> and `configs/claude/output-budget-run.sh`, and the cycle becomes history.

Governed by [ADR-0031](../decisions/ADR-0031-context-is-reduced-at-write-time-not-at-send-time.md):
context is reduced when output is produced, never by rewriting a request in
flight.

---

## 1. Why there is a runner at all

`PreToolUse` sees the command _before_ it runs and can only replace the command
string. It never sees output. So capping cannot be done in the hook — the hook
redirects to a wrapper, and **the wrapper is the feature**.

**The naive pipeline is wrong and must not be used.** Appending
`2>&1 | grep … | head -100` (including the form in Anthropic's own cost docs)
breaks three things at once:

1. **Exit status becomes `head`'s.** A failing test suite reports success. This
   alone disqualifies it — the agent would proceed on red.
2. **Compound commands mis-bind.** In `a && b`, the pipe attaches to `b` only.
3. **Quoting.** Splicing an arbitrary command into a JSON string field is an
   injection surface, not just an escaping nuisance.

---

## 2. The rewrite

The hook emits:

```
'<runner>' <head-lines> <tail-lines> <line-content-limit> <max-total-bytes> <capture-content-limit> '<original command>'
```

The five integers are passed as argv rather than re-read from the sidecar by the
runner: the hook stays the only sidecar reader, and the runner needs no file
access to do its job.

**Two of them are already net of their marker reserve, and that is why the
reserves are not transported at all.** The runner's two reserve-dependent
budgets — the per-line content allowance and the capture content limit — are
computed **in Go**, at generation time, and the sidecar carries the results:

| Transported           | Derived in Go as                         | Runner uses it for                       |
| --------------------- | ---------------------------------------- | ---------------------------------------- |
| `lineContentLimit`    | `maxLineBytes - inlineMarkerReserve`     | Bytes of content kept per truncated line |
| `captureContentLimit` | `maxCaptureBytes - captureNoticeReserve` | `probe_limit` is this plus one           |
| `maxTotalBytes`       | authored directly                        | Subtracting the **runtime** final marker |

`maxTotalBytes` stays raw because its marker embeds the scratch path, which does
not exist until the runner allocates it — that one subtraction cannot be
precomputed. The other two can, so they are.

This is the same rule the matching design follows: arithmetic that can live in
the single Go definition does, and the shell and JS sides consume results.
Passing the reserves instead would mean seven positional integers where a silent
misordering is easy, and the runner recomputing a subtraction Go had already
done — two ways to disagree about one number, for no gain.

### 2.1 Shell safety

**Every interpolated field is made shell-safe — not just the command.** The
emitted string is shell source, so any value spliced into it is code until
quoted. Seven fields go in, and all seven are handled:

| Field                                                                                                  | Treatment                                                                                                                                                                                          |
| ------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `<runner>`                                                                                             | Single-quote wrapped, inner `'` → `'\''` — the **same routine** as the command                                                                                                                     |
| `<original command>`                                                                                   | Single-quote wrapped, inner `'` → `'\''`                                                                                                                                                           |
| `<head-lines>`, `<tail-lines>`, `<line-content-limit>`, `<max-total-bytes>`, `<capture-content-limit>` | Must match `^[1-9][0-9]{0,14}$` — positive, no leading zero, at most 15 digits. Any failure disables rewriting rather than interpolating. This subsumes the positivity check; the width half is §5 |

The runner path comes from `paths.Paths.Config.Devgeta`, which resolves through
`GetConfigDir` — and that reads `XDG_CONFIG_HOME` directly (`paths.go:412`),
falling back to `~/.config`. So it is arbitrary user-controlled input: a space
(`/Users/Some Name/.config/...`) breaks argument splitting, and a `;`, backtick,
or `$(…)` turns a config path into executable shell. Unquoted interpolation here
is the same defect class as the naive pipeline §1 bans, reached by a different
route.

The five integers are included because they are interpolated too. They are
generated by devgeta today, but the sidecar is a plain file a user can edit, and
"our own generated value" is not a security property — validating is one
comparison.

**One escaping routine per language, shared, not reimplemented.** Add
`devgeta_shell_quote()` to `configs/claude/lib/segments.sh` (already sourced by
this hook for splitting) and export a `shellQuote` counterpart from the OpenCode
plugin side, following the `splitCommandSegments` precedent that
`secret-guard.js` already imports. Note the ADR-0006 loader constraint: every
export in a plugin file is invoked as a plugin, so `shellQuote` must tolerate
being called with an arbitrary `ctx` and satisfy `plugin-loader-safety.test.mjs`.
Two call sites per hook using one routine cannot drift the way two hand-written
escapes would.

### 2.3 Delivering the rewrite to each host — the two agents differ here

The shell string §2 builds is the same on both sides; how each hook hands it
back to its host is not, and getting this wrong either drops fields silently
or loses the rewrite outright. Verified against upstream docs
(code.claude.com/docs/en/hooks, opencode.ai/docs/plugins/) plus independent
sources, since the official Claude docs have real gaps here — cross-checked
rather than taken from a single source.

**Claude.** The hook's stdout is JSON:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "allow",
    "updatedInput": { "command": "...", "description": "..." }
  }
}
```

`updatedInput` **replaces the whole `tool_input` object**, not just the
changed field — every other key the original call carried (`description`,
`timeout`, anything else Claude Code sends for `Bash`) must be echoed through
unchanged, or it is silently dropped. Passing through the input verbatim
except for `command` is the only safe implementation; it must never be
constructed as `{"command": rewritten}` alone.

**`PreToolUse` hooks on the same matcher run in parallel, not sequentially —
"All matching hooks run in parallel" per the docs — and there is no chaining
between them.** Each hook decides against the _original_ `tool_input`; none
of them see another hook's rewrite. When more than one hook on the same
matcher returns `updatedInput` for the same call, the last process to finish
wins, non-deterministically — registration order in `settings.json` has no
bearing on this. See §9 for the concrete consequence with rtk. Whether a
rewritten command is re-checked by the permission system before running is
undocumented in every source checked; treat it as unverified rather than
assume either answer.

**OpenCode.** The hook mutates the call in place — there is no return value to
construct:

```js
export const OutputBudgetPlugin = async ({
  project,
  client,
  $,
  directory,
  worktree,
}) => ({
  "tool.execute.before": async (input, output) => {
    if (input.tool !== "bash") return;
    output.args.command = rewritten; // other fields on output.args are untouched by this assignment
  },
});
```

Plugin hooks on `tool.execute.before` **run in sequence**, and later plugins
see earlier plugins' mutations to the same `output.args` object — confirmed
independently of the single-field caveat above, and the opposite of Claude's
behavior. This is what makes "register the output-budget plugin after rtk's"
a real, working ordering rule on OpenCode, even though the identically-worded
idea does nothing on Claude (§9).

Neither difference is the asymmetry CLAUDE.md §12 forbids: it compares
deny/ask permission strings and formatter languages, and this changes neither
— it is a capability and timing difference in how each host's hook API
works, not a devgeta policy that could go out of sync between the two agents.

### 2.2 Resolving the runner path

**`<runner>` is never hardcoded in either hook.** Both resolve it from the
generated sidecar, which is the single artifact that knows where the runner was
deployed:

| Hook               | Resolution                                                                                                                                                                                                                    |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `output-budget.sh` | `jq -r '.runner' "$SIDECAR"` — `jq` is already a hard dependency of all five shipped Claude hooks (`task-redirect.sh`, `secret-guard.sh`, `suppression-guard.sh`, `agent-config-guard.sh`, `format.sh`), so this adds nothing |
| `output-budget.js` | the `runner` field of the sidecar object it already parses for the gate                                                                                                                                                       |

A hardcoded `~/.claude/output-budget-run.sh` breaks every OpenCode-only
installation, because that path only exists when Claude is configured. Resolving
through the sidecar is what makes the agent-neutral deployment reachable.

If the sidecar is absent or its `runner` names a nonexistent path, the hook
rewrites nothing.

---

## 3. The wrapper's contract

Each clause is testable:

| Clause        | Behavior                                                                                                                                                                                                              |
| ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Execution     | `bash -c "$cmd"` — the whole string, so `&&`, `\|\|`, `;`, and pipes keep their original semantics. Not `eval`                                                                                                        |
| Exit status   | `${PIPESTATUS[0]}` captured immediately after the run and re-raised as the wrapper's own exit code, with `pipefail` **off**. **Non-negotiable**                                                                       |
| Allocation    | One `task-*` directory per invocation via the existing `dg task scratch` mechanism — **not** a bare file at the scratch root. See §7                                                                                  |
| Capture       | stdout and stderr both captured to one file **inside that directory** (ADR-0015), preserving interleave order, **bounded by `maxCaptureBytes`** — see §4                                                              |
| Under the cap | File replayed verbatim. Byte-identical to running the command unwrapped. "Under the cap" means under **both** bounds                                                                                                  |
| Over the cap  | The reduction pipeline in §6: per-line truncation, then head/tail by line count, then the byte refill. Tail is kept larger because failures land at the end                                                           |
| Marker        | Names the omitted line count, the omitted byte count, **and the absolute path to the full output**                                                                                                                    |
| Full output   | Left on disk under the scratch root, which `settings.json.tmpl` already lists in `additionalDirectories` — so the agent can `grep` it with no permission prompt and **without re-running the command**                |
| Cleanup       | Finished directories fall to the existing `task-*` reaper (24h age, configure-time); no new lifecycle. Requires the Allocation clause to hold. An **active** write is bounded by `maxCaptureBytes`, not by the reaper |
| Escape hatch  | `DEVGETA_OUTPUT_BUDGET=off` in the environment makes the wrapper a pass-through, and the hook skips rewriting entirely                                                                                                |

That "full output on disk" clause is what makes this safe. ADR-0031's stated
risk is that a lossy cut costs more than it saves when the agent re-runs the
command; here nothing is lost, so the recovery is a targeted `grep` on a file
rather than a second full run.

---

## 4. Bounding the capture

**The capture itself must be bounded, because this feature invents a disk write
that did not exist before.** Unwrapped, a command's output streams into the
agent's pipe and is the agent's problem. Wrapped, it lands in
`~/.cache/devgeta/scratch`. A matched command with a runaway logger — an
accidentally-matched watch mode, a test looping on a stack trace — writes until
the volume is full, and nothing reclaims it in the meantime:
`MaintainScratchDir` is called only from `claude.go:186` and `opencode.go:170`,
both **configure** paths, and prunes on a 24-hour age. So the existing reaper is
not a bound on an active write; it is cleanup for finished ones.

| Constant          | Value               | Purpose                                                                                                                                   |
| ----------------- | ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `maxCaptureBytes` | `16777216` (16 MiB) | The capture ceiling devgeta **ships**. Authored in Go; what reaches the hooks is `captureContentLimit`, already net of the notice reserve |

Note the wording: it is the ceiling devgeta ships, not a ceiling devgeta enforces
against the machine's owner. See §4.1.

16 MiB is deliberately far above `maxTotalBytes` — roughly 250× — because this is
a safety valve, not a tuning knob. It has to be large enough that the
`grep`-the-full-output recovery still works for a genuinely large test run, and
small enough that a runaway cannot fill a disk.

### 4.1 A user who raises the limit gets the limit they asked for

The sidecar is a generated file in the user's own config directory. If someone
edits `captureContentLimit` to something enormous, every check passes and the
capture is effectively unbounded — and that is the correct outcome, not a hole.
Two reasons it is not treated as one:

- **A large positive value means what it says.** The reason a nonpositive value
  is refused is that its effect is unrelated to its meaning: `0` does not produce
  "no capture", it produces silent total output loss on BSD and unbounded
  buffering on GNU. "16 GiB" produces a 16 GiB allowance. The first is a
  malformed value; the second is a configuration choice.
- **Enforcing a ceiling would cost more than it buys.** Checking
  `maxCaptureBytes == 16777216` in the hooks puts that literal back into bash
  **and** JS — the duplicated constant this design removed by deriving the limits
  in Go — and freezes a default that should be free to change. Signing the
  sidecar so it cannot be edited is not a boundary worth building either:
  anything able to write `~/.config/devgeta/` can equally rewrite the hook
  scripts and `settings.json` beside it, so there is nothing left to protect at
  that point.

What **is** worth guarding is devgeta writing a bad value itself, since that is
the only path where the user did not choose it: `EnsureAgentRuntime` asserts each
derived limit equals its expected derivation before writing. That is a Go-side
invariant with no hook-side constant, and it covers the case a hook-side ceiling
was reaching for. A hand-edit is also self-healing — the next
`dg configure … --force` regenerates the file from the shipped constants.

### 4.2 The mechanism must not change the command's behavior

Three approaches were considered and two rejected outright:

| Approach                              | Verdict                                                                                                                  |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `ulimit -f` in the subshell           | **Rejected.** Exceeding it sends `SIGXFSZ` and kills the command. Changes the exit status, which is non-negotiable       |
| Pipe to `head -c` alone               | **Rejected.** When `head` exits the producer takes `SIGPIPE`, so again the command dies of the cap rather than finishing |
| Pipe to `head -c`, then keep draining | **Accepted.** The producer never blocks and never sees a closed pipe, so it runs to its own completion                   |

### 4.3 Detecting the cap needs one deliberate extra byte

`head -c N` exits 0 whether the input was shorter than, exactly, or longer than
`N`, and the drain that follows it exits 0 too. So neither status says anything,
and a file of exactly `N` bytes is ambiguous between "the output happened to be
`N`" and "the output was cut at `N`". Without resolving that, the notices in §4.4
are not implementable: the runner would have to either warn whenever output lands
exactly on the limit or never warn at all.

The fix is to read **one byte past** the content budget and let the file size
answer a narrower question than "was anything lost":

```bash
# content_limit arrives as argv, already net of the notice reserve
probe_limit=$((content_limit + 1))

bash -c "$cmd" 2>&1 | { head -c "$probe_limit" > "$out"; cat > /dev/null; }
child_status=${PIPESTATUS[0]}

if [ "$(wc -c < "$out")" -eq "$probe_limit" ]; then
	capture_capped=1   # the cap was reached; loss beyond it is unknown
fi
```

`wc -c` reaching `probe_limit` proves the command produced **at least**
`content_limit + 1` bytes. It does **not** prove anything was discarded: a
command emitting exactly `content_limit + 1` bytes fills the probe with its last
byte and loses nothing. Measured, with a content limit of 100:

| Produced | Retained | Flag | Actually lost |
| -------- | -------- | ---- | ------------- |
| 100      | 100      | 0    | 0             |
| 101      | 101      | 1    | **0**         |
| 102      | 101      | 1    | 1             |

The group is one shell process reading the pipe: after `head` has written its
limit and exited, `cat` keeps consuming from the same descriptor, so the child is
never blocked on a full pipe and never signalled. `${PIPESTATUS[0]}` is the
command's own status, which is why `set -o pipefail` must **not** be used here —
it would let the drain's status win, reintroducing the exact defect the naive
pipeline was banned for.

**The reserve is sized so keeping the probe byte still fits the bound:**
`captureNoticeReserve >= len(notice) + 1`, hence
`probe_limit + len(notice) <= maxCaptureBytes`. That inequality is what makes
rejected option 1 below unnecessary as well as expensive — the byte is genuine
output and costs nothing to keep.

**Three ways to get certainty were considered and all three rejected.**

1. **Trim the probe byte back off**, so "truncated" is true by construction.
   Needs an in-place `truncate` or a rewrite of up to 16 MiB, and it manufactures
   a one-byte loss in order to have one to report.
2. **Replace the drain with `wc -c`** to count the discarded bytes exactly. The
   most tempting, and unsound: `head -c` may pull a whole block off the pipe and
   discard the excess, so the counter can read 0 while bytes really were dropped.
3. **Sniff with a second `head -c 1`** after the first. Fails for the same reason
   as (2), and in the same direction.

POSIX leaves the file offset after `head` unspecified on non-seekable input, so
neither macOS nor Debian owes us exact behavior — (2) and (3) are betting on an
implementation detail of a tool we do not ship. A false "complete" is the worst
failure available here, and both of them can produce it. One `head` writing one
file makes that file the evidence, which no buffering can distort; the price is
that the evidence answers "capped", not "lost".

Verified before this was written down: the boundary table above, plus a 5 MB
producer still leaving a 101-byte file, and the command's exit status intact in
every case. Not `status` as the variable name — that identifier is read-only in
zsh, and while the runner is bash, the trap costs nothing to avoid.

Above the limit, some discarded bytes are lost inside `head`'s buffer and the
rest are eaten by the drain, so the boundary between kept and discarded output is
approximate. Nothing depends on where the discarded region begins; the properties
worth testing are the file's size and the flag.

This is one bash implementation in the runner, so unlike the matching rules it has
no mirror to keep in step — the OpenCode plugin only rewrites a command to call
the same script.

### 4.4 What the notices may claim

The flag is named `capture_capped`, not `capture_incomplete`, and **both notices
state the cap rather than asserting loss.** One byte of output is the difference
between the second and third rows of the table in §4.3, and no signal available
here separates them — so the honest statement is the one that is true in both:

| Surface       | Wording                                                                                          |
| ------------- | ------------------------------------------------------------------------------------------------ |
| File notice   | capture stopped at the limit; anything the command produced past this point, **if any**, is gone |
| Replay marker | full output was **capped** at `N` bytes and may be incomplete                                    |

Note the "if any" — it is doing real work, not hedging. Without it the notice is
false for exactly one output length, and that is the length where a reader would
be least likely to check.

This is also the wording the agent needs operationally: once the cap is reached, a
failed `grep` of the full-output file is no longer evidence that the string is
absent. Claiming nothing about the cap would leave the agent trusting a file it
should not.

**The notice is appended after the replay is built, not before.** The replay's
tail comes from the end of the file, so a notice written first would be selected
into it — devgeta's own text interleaved into what reads as command output, and
one tail line spent on it. Order: capture, detect, build the replay from the
captured bytes, append the notice to the file, emit the replay with the cap
stated in its marker.

---

## 5. The numeric contract

### 5.1 A line cap is not a bound on bytes — in either direction

Counting only lines leaves the feature's central promise unenforced two different
ways:

- **One huge line.** A minified bundle, a generated source map, a JSON payload,
  or a single enormous stack-trace line has no newlines, so it is one line, so it
  is "under the cap" and gets replayed in full.
- **Many large lines.** 25 lines of 1 MB each is under a 30-line head cap. The
  line count says "small"; the agent receives 25 MB.

So the budget is expressed in both units, as **top-level** fields rather than
per-rule ones — there is no reason for these to differ per runner, and a field
duplicated across nine rules is nine chances to diverge. The authored ceilings
and what is actually transported are two different lists; see §8.1.

### 5.2 Type validation is not enough: zero and negatives are well-typed

"Non-negative integer" admits `0`, and a `0` or negative `captureContentLimit`
reaches `head -c` as a nonpositive byte count. Because the hook has already
rewritten the command by the time the runner could notice, a hand-edited sidecar
would break an otherwise fine command — the opposite of the degradation contract,
which says every degenerate sidecar case leaves the command untouched.

The concrete hazard is worth naming, because "invalid" undersells it. A
nonpositive `content_limit` makes the runner reach `head -c -N`, which is **not**
an error everywhere:

| Platform          | `head -c -N` behavior                                                                                                                                        |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| macOS (BSD head)  | `head: illegal byte count` — measured. `head` fails, the drain eats the stream, and the agent sees **no output at all** while the command reports success    |
| Debian (GNU head) | Documented as "print all but the last NUM bytes" — so it buffers the entire stream to withhold the tail, **defeating the bound** the field exists to enforce |

One malformed number, two different wrong behaviors, on the two platforms
[CLAUDE.md §8](../../CLAUDE.md#8-platform-scope) requires to agree. Neither is
acceptable, and neither is caught by a type check.

### 5.3 "Positive integer" is not a shared contract either

Bash and JavaScript disagree about large numbers, silently. Both hooks read the
same field and must reach the same byte count, so the range they agree on is part
of the contract. Measured on the two runtimes this ships to:

| Sidecar value            | bash `$((v + 1))`          | JS `String(JSON.parse(v))` | JS `Number.isInteger` |
| ------------------------ | -------------------------- | -------------------------- | --------------------- |
| `9007199254740993`       | `9007199254740994`         | **`9007199254740992`**     | true                  |
| `9223372036854775807`    | **`-9223372036854775808`** | `9223372036854775808`      | true                  |
| `99999999999999999999`   | **`7766279631452241920`**  | `100000000000000000000`    | true                  |
| `1000000000000000000000` | **`200376420520689665`**   | **`"1e+21"`**              | true                  |

Three distinct failures, none of them an error: JavaScript changes the value
above 2^53 and switches to exponential notation above 1e21; bash wraps at 64
bits, sometimes to a different positive number and — at 2^63 — to a **negative**
one, which walks straight back into the `head -c -N` hazard in §5.2. And
`Number.isInteger` is `true` for every row, so the obvious JS guard
(`Number.isInteger(v) && v > 0`) admits all four.

**The contract is therefore a decimal-width one, checked against the rendered
string, not the parsed number:**

```
^[1-9][0-9]{0,14}$
```

Fifteen digits, so the maximum is `999999999999999` — exact in IEEE-754,
`+1`-safe in 64-bit bash, and about 1 PB, which is past any capture limit anyone
will set on purpose. Two properties make this the right shape:

- **Checking the rendered string makes every parser's mangling fail safe.**
  Whatever `jq -r` or `JSON.parse` does to an out-of-range literal, the result
  either has too many digits or is not all digits — `1e+21` fails the pattern on
  its face. The check never has to model the parser it is defending against.
- **The bound is a property of the two languages, not a devgeta policy.** Unlike
  the 16 MiB default in §4, `2^53` is not ours to change, so a literal on each
  side is not the duplicated-constant risk that moved the reserves into Go. It
  cannot drift, because IEEE-754 will not.

### 5.4 Where each check lives

Deriving the two limits in Go collapses the relationship checks into the same
one-field check. `lineContentLimit > 0` is exactly
`maxLineBytes > inlineMarkerReserve`, and `captureContentLimit > 0` is exactly
`maxCaptureBytes > captureNoticeReserve` — the same conditions, expressed where
they cost one comparison instead of a subtraction against a constant each side
would have to know:

| Where                          | What                                                                                                                                                                            | Why there                                                                                      |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `EnsureAgentRuntime` (Go)      | Owns the reserves; refuses to **write** a sidecar whose derived limits are not positive, whose values fail the width pattern, or where `captureNoticeReserve < len(notice) + 1` | The shipped defaults can never be wrong, and this is the only layer that knows the reserves    |
| Both hooks, **before** rewrite | Every interpolated integer matches `^[1-9][0-9]{0,14}$`                                                                                                                         | The only layer that can still fail open on a hand-edited file                                  |
| The runner                     | Re-checks its five integers against the same pattern **before** any arithmetic; a failure means execute the command unwrapped, exit status preserved                            | Defence in depth — nothing nonpositive or out-of-range reaches `head -c`, and no `+1` can wrap |

Each layer checks only what it can see, and no layer needs a constant the layer
above it owns. An earlier draft put the two reserves in the sidecar so the hooks
could subtract them; that left the runner needing them too while argv carried only
five integers, so the runner would have had to hardcode both — the single-source
rule this design exists to protect, broken in the course of defending it.

**Degenerate budgets that are still valid.** Once the relationships hold, a
budget can still be small enough to leave no room for content: if
`contentBudget <= 0` after reserving the final marker, the marker alone is
emitted, because the path to the full output is the one thing that must survive.
That is a legitimate outcome; a violated relationship is not, and is refused
upstream instead.

---

## 6. The reduction pipeline

**The order of operations is part of the contract.** Left unstated, the two
implementations would compose these three steps differently and produce different
output from the same input — the divergence the token-prefix decision in §8
removes from matching, reintroduced in reduction:

1. **Per-line truncation.** Every retained line is cut to `lineContentLimit`
   bytes of content, then the inline marker is appended. `lineContentLimit` is
   already `maxLineBytes - inlineMarkerReserve` (computed in Go), so the
   **result** is what does not exceed `maxLineBytes`.
2. **Head/tail selection** by line count, exactly as the rule specifies.
3. **Byte refill.** If the result still exceeds `maxTotalBytes`, discard it and
   rebuild from fixed budgets computed from the **content budget**
   `contentBudget = maxTotalBytes - len(finalMarker) - 1`:
   `headBytes = contentBudget / 4`, `tailBytes` the remainder. Take whole lines
   greedily — head from the start, tail from the end — stopping before the line
   that would exceed each budget. The 1:3 split mirrors the head-under-tail bias
   already in the rule table.

Step 3 is why `maxLineBytes` alone is not enough: 150 retained lines (`head` 30 +
`tail` 120) at 2 KB each is 300 KB, roughly 75k tokens. The line cap stops one
pathological line; `maxTotalBytes` is what makes the feature a budget. Two greedy
loops over fixed integer budgets is arithmetic and line accumulation, so it is
trivially identical in bash and JS — no dialect risk.

**Markers count against the budgets, because they are bytes the agent receives.**
Stating a cap and then appending to the result is the same class of error as
counting lines and calling it a byte bound — the number is real, it just is not
the number that was promised. Left unreserved, step 1 produces lines of
`maxLineBytes + marker` and step 3 a result of `maxTotalBytes + marker`, and the
conformance property in §10 ("no reduced result exceeds `maxTotalBytes`") would
fail against the spec rather than against a bug.

Reserving the marker needs one wrinkle handled: the inline marker names the
omitted byte count, so its own length depends on how much was cut, which depends
on the reservation. Resolve it with a **constant**, not a second pass:

| Quantity              | Definition                                                                                                            |
| --------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `inlineMarkerReserve` | `len(template) + 20` — the template's fixed text plus the widest possible decimal byte count. A compile-time constant |
| Guarantee             | The formatted marker is never longer than the reserve, so the retained line is never longer than `maxLineBytes`       |
| `finalMarker`         | Measured exactly at runtime — it embeds the scratch path, whose length is known before any content is selected        |

Reserving a constant slightly over-reserves for small omission counts. That is the
right direction to be wrong in, and it keeps the rule a single subtraction that is
trivially identical in bash and JS — a two-pass "format, measure, re-cut" loop is
where the two implementations would diverge.

---

## 7. Scratch lifecycle — the allocation shape is load-bearing

Both reapers match on a directory carrying `paths.ScratchAllocPrefix` (`"task-"`):
`MaintainScratchDir` skips any entry failing `entry.IsDir() &&
strings.HasPrefix(entry.Name(), ScratchAllocPrefix)` (`baseapp/configure.go:86`),
and `ScratchClean` refuses anything that is not a direct child of the scratch root
carrying that prefix (`task/scratch.go:150`). So a file written straight to the
scratch root is **never** collected — it would leak one file per wrapped command,
forever, in a feature that runs on every test invocation. The runner therefore
allocates through the same path `dg task scratch` uses
(`os.MkdirTemp(root, ScratchAllocPrefix+"*")`, `task/scratch.go:25`) and writes
inside it. This is reuse of an existing owned lifecycle, not a new one.

---

## 8. Matching

The hook matches the command against the rules in the sidecar. It sources
`lib/segments.sh` to split a compound command into segments rather than
re-implementing that, then applies the tokenization contract in §8.2 to each
segment — **segmentation and tokenization are two different steps and only the
first one already exists.**

### 8.1 Rule schema — one source, generated into the sidecar

"Config-driven with general defaults" is not a specification: two independent
implementations would invent different matchers and different caps. The rules are
therefore **defined once in Go**, rendered into `agent-runtime.json` by
`EnsureAgentRuntime`, and both hooks consume that generated array. Neither hook
contains a pattern literal.

```json
{
  "outputBudget": true,
  "runner": "/abs/path/to/output-budget-run.sh",
  "lineContentLimit": 1984,
  "maxTotalBytes": 65536,
  "captureContentLimit": 16777088,
  "rules": [
    { "name": "go-test", "match": ["go", "test"], "head": 30, "tail": 120 },
    {
      "name": "cargo-test",
      "match": ["cargo", "test"],
      "head": 30,
      "tail": 120
    },
    { "name": "pytest", "match": ["pytest"], "head": 30, "tail": 120 },
    { "name": "npm-test", "match": ["npm", "test"], "head": 30, "tail": 120 },
    { "name": "npm-run", "match": ["npm", "run"], "head": 30, "tail": 100 },
    { "name": "make", "match": ["make"], "head": 20, "tail": 100 },
    {
      "name": "cargo-build",
      "match": ["cargo", "build"],
      "head": 20,
      "tail": 100
    },
    { "name": "gradle", "match": ["gradle"], "head": 20, "tail": 100 },
    { "name": "maven", "match": ["mvn"], "head": 20, "tail": 100 }
  ]
}
```

| Field                 | Meaning                                                                                                                          |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `name`                | Stable identifier. Appears in the truncation marker and is what tests assert on                                                  |
| `match`               | **Token prefix**, not a regex — see §8.2                                                                                         |
| `head`                | Lines kept from the start                                                                                                        |
| `tail`                | Lines kept from the end. Larger than `head` throughout: failures land last                                                       |
| `lineContentLimit`    | Top-level, not per-rule. Content bytes kept per truncated line — already net of the inline marker reserve                        |
| `maxTotalBytes`       | Top-level, not per-rule. Ceiling on the whole replayed result, marker included. Raw, because its marker is only known at runtime |
| `captureContentLimit` | Top-level, not per-rule. Content bytes captured to disk — already net of the notice reserve                                      |

**This table is the whole transported schema.** The authored ceilings
(`maxLineBytes`, `maxCaptureBytes`) and both reserves (`inlineMarkerReserve`,
`captureNoticeReserve`) are **Go-side constants, not sidecar fields.** They are
what the derived limits are computed from, and nothing downstream reads them, so
shipping them in the file would be more numbers to hand-edit into disagreement
with the values that are actually used.

### 8.2 `match` is a token prefix, deliberately not a regex

Bash EREs and JavaScript `RegExp` differ in escaping, character classes, and
anchoring, so the same pattern string can match differently in the two hooks — the
exact divergence
[CLAUDE.md §12](../../CLAUDE.md#keeping-the-two-ai-agents-in-sync) warns about,
where a rule is identical in both configs and still enforces something different.
Equality of string arrays behaves identically in bash and JS; regex does not.

**But array equality is the easy half. Producing the array is the contract.** Two
things that look like they solve tokenization do not, and the error is worth
recording so it is not reintroduced:

- `devgeta_split_command_segments` (`configs/claude/lib/segments.sh:62`) splits on
  unquoted `&&`, `||`, `;`, and `|` and returns each segment as a **raw string**.
  Its JS counterpart `splitCommandSegments`
  (`configs/opencode/plugin/task-redirect.js:254`) does the same. Neither produces
  tokens. Segmentation is not tokenization.
- The "global options `lib/segments.sh` already recognizes" are
  `DEVGETA_GIT_GLOBAL_OPT` and `DEVGETA_GH_GLOBAL_OPT` (`segments.sh:38,45`) —
  git- and gh-specific regex fragments. There is no generic global-option
  recognizer, and not one built-in rule here is `git` or `gh`.

So without a stated tokenization contract, each hook would invent its own
whitespace/quote/escape parsing, and the two would disagree — the divergence this
design exists to prevent, reached one layer lower down. `segments.sh`'s own header
already concedes the split is best-effort and does not handle escaped quotes,
command substitution, or heredocs.

**The contract: under-match rather than parse.** Neither hook implements a shell
parser. Given one segment, both apply exactly this:

1. Trim leading and trailing whitespace.
2. Strip a leading run of `VAR=value` tokens; also strip a leading `env` token
   together with its own following assignment run.
3. Split on runs of ASCII space and tab **only**. No quote handling, no escape
   handling, no substitution awareness.
4. Compare the first `len(match)` tokens against `match`, element for element.
5. **If any token in the compared prefix — or any assignment token stripped in
   step 2 — contains `'`, `"`, `\`, or `$`, the segment does not match.** The
   command runs unwrapped.

Step 5 is the trick that removes the parser. Rather than agreeing on quoting
semantics across two languages, both hooks decline anything quoted in the part
they are comparing. Under-matching costs a missed cap and nothing else; the
command still runs, correctly and in full.

**The check covers only the compared tokens, never the arguments.** This is what
keeps the rule from being lossy in practice, so it must be stated explicitly:
`match` is one or two tokens, so only the executable name and its subcommand are
ever examined. Arguments may contain anything.

| Command                              | Result                                          |
| ------------------------------------ | ----------------------------------------------- |
| `go test ./...`                      | matches `go-test`                               |
| `CI=1 go test ./...`                 | matches — assignment stripped in step 2         |
| `env CI=1 go test ./...`             | matches — `env` and its run stripped            |
| `make -j$(nproc)`                    | matches `make` — the `$` is in an argument      |
| `npm test -- --grep "foo bar"`       | matches `npm-test` — quotes are in arguments    |
| `"/opt/my go/bin/go" test`           | **no match** — quote in the compared prefix     |
| `command go test`, `timeout 60 make` | **no match** — not stripped; documented as such |

A tokenizer that ignores quoting is also only sound _because_ of step 5: without
it, `"/opt/my go/bin/go" test` would tokenize to `"/opt/my` and `go`, and a naive
`["go", ...]` comparison could match the wrong thing. Refusing is what makes the
cheap split safe rather than merely convenient.

Whitespace splitting with no quote semantics and a character-membership test are
both trivially identical in bash and JS.

### 8.3 Precedence and malformed rules

**Precedence: first match in array order wins.** Not longest-match, not
most-specific — array order is the Go slice order, so it is stable, obvious in the
generated file, and trivially identical in both implementations. `npm test`
precedes `npm run` in the array for that reason.

**Malformed rules disable rewriting entirely** — the whole array, not just the bad
entry. Skipping individual bad entries would require both implementations to
agree, field by field, on what "bad" means, which reintroduces exactly the
divergence the token-prefix decision removes. All-or-nothing is one comparison and
is trivially identical across the two. It also fails in the safe direction: no
rewriting means commands run untouched.

### 8.4 The default rule set

The defaults above are general-purpose runners across ecosystems, with no
devgeta-specific or Go-specific bias — this ships to strangers
([CLAUDE.md §12](../../CLAUDE.md#anything-we-ship-is-built-for-strangers)).
Deliberately **not** included: `tail`/`cat` of log files (frequently the whole
point of the command), anything interactive, and anything with a TTY requirement.

---

## 9. Known limitations — document, do not paper over

- **On Claude Code, this hook and rtk's race when both are enabled.** §2.3
  established that `PreToolUse` hooks on one matcher run in parallel with no
  chaining, and when two of them return `updatedInput` for the same call the
  last process to finish wins non-deterministically. If rtk's hook happens to
  finish last, this hook's cap is silently skipped for that call — the
  command runs through rtk's rewrite, uncapped. There is no code fix for this
  within Claude Code's current hook model; the accepted mitigation is that
  this hook never depends on rtk's behavior at all (it matches and rewrites
  purely against the original command, exactly as if rtk did not exist), so
  the failure mode on a race loss is "uncapped for this one call," not a
  wrong or corrupted rewrite. OpenCode has no equivalent limitation — its
  plugin hooks chain in registration order (§2.3).
- stdout and stderr are merged; a caller that needs them separated loses that.
- Output no longer streams — it appears when the command finishes. Fine for test
  and build runs, wrong for anything long-running and watched.
- Anything needing a TTY must not be matched. Default patterns cover
  non-interactive runners only.
- At `maxCaptureBytes` the full output stops being trustworthy, and the marker
  says so. This is the one case where the "nothing is lost, just `grep` the file"
  recovery does not hold, which is why the bound is set two orders of magnitude
  above the replay budget: reaching it should mean something went wrong, not that
  a test suite was chatty. The runner reports the cap, not a byte count — it can
  prove the cap was reached but not how much, if anything, came after it.

---

## 10. Conformance tests

These are the contract's own tests. Both suites — Go in the root package
(mirroring `secret_guard_test.go`) and Node `.test.mjs` (mirroring
`secret-guard.test.mjs`) — run the real scripts against real temp dirs.

**Exit status and pass-through**

- a **failing** command wrapped by the runner still exits non-zero (the regression
  that makes this feature dangerous if wrong — write it first)
- output under the cap is byte-identical to running unwrapped
- a compound command (`a && b`, `a; b`) keeps its semantics and caps as a whole
- a command containing single quotes, `$`, and backticks survives the rewrite
  unchanged
- an unmatched command passes through byte-identical
- `DEVGETA_OUTPUT_BUDGET=off` is a true pass-through

**Reduction and markers**

- output over the cap has head, marker, and tail, and the marker names a path that
  exists and holds the complete output
- a single line longer than `maxLineBytes` is truncated inline, its marker names
  the omitted byte count, and the full line is intact in the file
- output whose line count is **under** `head`+`tail` but whose byte count exceeds
  `maxTotalBytes` is still reduced — e.g. 25 lines of 1 MB
- the reduction order is observable: a case where per-line truncation alone brings
  the result under `maxTotalBytes` produces different output than one where the
  byte refill also has to run, and both are asserted exactly
- the byte refill splits its budget 1:3 head-to-tail and takes whole lines
- no reduced result exceeds `maxTotalBytes`, asserted as a property over every
  fixture in the table
- **markers are inside the budgets, not added to them**: a truncated line
  including its inline marker is `<= maxLineBytes`, and a reduced result including
  its final marker is `<= maxTotalBytes`. Run these with a long scratch path and a
  large omitted-byte count — the two inputs that grow the markers and would expose
  an unreserved budget
- degenerate-but-valid budget: `contentBudget <= 0` after reserving the runtime
  final marker emits the marker alone, and the path in it is still correct.
  (`lineContentLimit <= 0` is not this case — it cannot reach the runner, since Go
  refuses to write it and both hooks refuse to rewrite on it)

**Capture bound**

- a command emitting well beyond `maxCaptureBytes` leaves a bounded file — disk
  use does not track the command's output
- that command still **completes and reports its own exit status**; it is not
  killed by `SIGPIPE` or `SIGXFSZ`, and it does not deadlock. Assert a non-zero
  status survives a truncated capture, since that is the combination where both
  this clause and the exit-status clause could fail together
- **the boundaries, since the signal is exactly one byte wide** — output of
  `content_limit`, `content_limit + 1`, and `content_limit + 2` bytes. The first
  sets no flag; the second and third both set it. Assert the retained byte count
  too (`content_limit`, `content_limit + 1`, `content_limit + 1`), which is what
  shows the second case kept everything
- **the notices claim the cap, never confirmed loss.** Assert the exact wording in
  the `content_limit + 1` case, where nothing was discarded and an "output was
  truncated" claim would be false
- the final file never exceeds `maxCaptureBytes`, notice included — the reserve
  arithmetic asserted, not assumed
- when capped, the file carries the notice **and** the replay marker says the full
  output was capped
- the notice is not present in the replayed tail — it is appended after the replay
  is built
- the output path is a direct `task-*` child of the scratch root, so a later
  refactor cannot quietly reintroduce the file leak (§7)

**Numeric validation**

- a non-integer `head`, `tail`, `lineContentLimit`, `maxTotalBytes`, or
  `captureContentLimit` disables rewriting rather than interpolating the value —
  all five fields, since all five are interpolated
- **nonpositive budgets, under both hooks**: `0` and `-1` for each of
  `lineContentLimit`, `maxTotalBytes`, and `captureContentLimit`. All disable
  rewriting and the command runs **unmodified** — the failure must be upstream of
  the rewrite, not inside the runner
- **out-of-range budgets, under both hooks and the runner**, driven from one shared
  table: `999999999999999` (the maximum, accepted), `1000000000000000` (16 digits,
  rejected), `9007199254740993` (JS changes it to `…992`), `9223372036854775807`
  (bash `+1` wraps negative), `99999999999999999999` (bash `+1` wraps to a
  different positive), `1000000000000000000000` (JS renders `1e+21`), and a
  leading-zero form. Every rejected case runs the command unmodified. Assert the
  **two hooks agree** on each row — this is a parity test, not just a validation
  test, because the failure being prevented is the two of them emitting different
  byte counts from one file
- the runner validates **before** its `+1`, so no wrap is reachable even if a hook
  is bypassed
- a nonpositive value never reaches `head -c`: with `content_limit` forced negative
  in the runner's argv directly, the runner executes the command unwrapped and
  still reports its exit status. Worth a direct test because the two platforms fail
  differently here — BSD `head` errors, GNU `head` reinterprets `-N` as "all but
  the last N bytes"
- **a hand-raised `captureContentLimit` is honored, deliberately**: a large
  positive value **within the width contract** rewrites and captures to that limit
  rather than failing open. This test exists to pin the decision in §4.1 so it is
  not later "fixed" into a refusal that would put the 16 MiB literal back into both
  hooks

**Matching parity**

- **rule-decision parity, table-driven over every built-in rule**: for each rule, a
  matching command and a near-miss (e.g. `npm testx`, `go tests`), the bash hook
  and the JS hook produce the **same** decision and the **same** emitted command.
  Drive both suites from the same case table so a rule added in Go without a
  mirrored decision fails the build
- precedence: `npm test` selects `npm-test`, not `npm-run`
- **tokenization contract**, from the same shared table, every row asserted for
  both hooks: `CI=1 go test ./...` and `env CI=1 go test ./...` match;
  `make -j$(nproc)` and `npm test -- --grep "foo bar"` match (metacharacters and
  quotes in **arguments** never block a match); `"/opt/my go/bin/go" test` does
  **not** match; `FOO="a b" go test` does **not** match (quote in a stripped
  assignment token); `command go test` and `timeout 60 make` do **not** match;
  tab-separated and multiply-space-separated commands tokenize identically; a
  refused segment is byte-identical to running with no hook at all
- a malformed `rules` array disables rewriting entirely, under both hooks

**Emitted command and generation**

- **hostile config paths**, both hooks: runner path containing a space, a single
  quote, `$`, `;`, and a backtick — exact emitted string asserted **and** executed
  end to end, returning the original command's exit status
- **the emitted command carries the transported limits verbatim**: build a sidecar
  whose `lineContentLimit` and `captureContentLimit` are deliberately _not_ what
  the shipped reserves would produce, then assert the exact emitted string under
  both agents contains those values. An assertion on "five integers were passed"
  would still pass against a hook or runner that recomputed them from a hardcoded
  reserve, which is exactly what this shape exists to prevent
- `EnsureAgentRuntime` asserts each derived limit **equals** its expected
  derivation from the Go constants **and** satisfies the width pattern — the guard
  against devgeta itself writing a wrong-but-positive value, which is the only path
  to one the user did not choose
- `EnsureAgentRuntime` refuses to write a sidecar whose derived limits are
  nonpositive, or whose notice reserve is smaller than the notice, so the shipped
  defaults cannot reach either hook in that state
