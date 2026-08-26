# ADR-0033 — A keyed scratch directory trades isolation for hand-off

**Date:** 2026-08-26
**Status:** PROPOSED

## Context

`devgeta task scratch` (ADR-0015) allocates a fresh, uniquely-named directory
under `~/.cache/devgeta/scratch` via `os.MkdirTemp(root,
paths.ScratchAllocPrefix+"*")` (`internal/tooling/task/scratch.go:20-30`). The
random suffix is not incidental — ADR-0015 §3 chose it specifically so that
"a session could still clean a sibling by guessing its `os.MkdirTemp` suffix"
stays hard, and so two concurrent sessions can never collide on the same
allocation.

That same property makes the directory useless for a hand-off. An agent
session that produces a working file today has no way to tell a _later,
separate_ session where that file landed, short of printing the random path
into a transcript the later session may never see. There is no way to
_re-derive_ a scratch path from something both sessions already know (an
issue number, a review round, a task name) — only the allocating process ever
learns the suffix.

`validScratchChild` (`scratch.go:146-154`) then closes off the workaround:
even a caller that hardcodes a stable path outside `os.MkdirTemp` cannot
`--clean` it, because cleanup refuses anything not carrying
`paths.ScratchAllocPrefix`. The function's own doc comment already names the
gap it leaves: "a session could still clean a sibling by guessing its
`os.MkdirTemp` suffix … the bound stops here deliberately rather than by
oversight." This ADR is about opening a second, deliberate door next to that
one — not about widening it.

## Decision

### 1. `Scratch` takes an optional key

`Scratch(key string)` (`cmd/task.go`'s scratch command threads it through).
An empty key keeps today's behavior byte-for-byte: `os.MkdirTemp` under
`paths.ScratchAllocPrefix`, unique every call, no re-derivation possible. A
non-empty key produces the deterministic path
`<root>/<paths.ScratchKeyPrefix><sanitized-key>` — same key in, same path out,
every time, from any process that knows the key.

That determinism is the whole point and the whole cost in the same sentence.
ADR-0015 §3 picked a random suffix explicitly so no invocation could ever
guess, and therefore never collide with, another session's directory. A
predictable name reopens exactly that: two of the _same user's own_ sessions
that happen to pick the same key share one directory, and whichever writes
last wins. That is the isolation property being traded, named plainly rather
than assumed away.

**Why the trade is acceptable:** the collision is scoped to one user's own
concurrent sessions, not to arbitrary processes on the machine — the scratch
root is already `0700` and per-user (ADR-0015 §1). Two of _your own_ agent
sessions deliberately sharing a key is the hand-off working as designed; two
of them _accidentally_ sharing a key is a naming collision the caller
controls by picking a specific-enough key (an issue number plus a task name,
not a bare "scratch"). Without this trade, hand-off across sessions is not
possible at all — there is no other way for one session to tell a later,
independent one where a file is, without a shared, re-derivable name. The
unkeyed path remains the default (see "Trade-offs Made" in the cycle doc)
precisely because it is the safer of the two and the hand-off case is the
minority.

### 2. Keyed directories get a distinct prefix, exempt from age-pruning

`paths.ScratchKeyPrefix` is a new constant, **distinct** from
`paths.ScratchAllocPrefix` — not a variant spelling of it. `MaintainScratchDir`
(`internal/apps/baseapp/configure.go:84-105`) prunes any `ScratchAllocPrefix`
entry older than `scratchStalePruneAge` (24h) as the safety net for a run
interrupted before its own `--clean`. A keyed directory must **never** be a
candidate for that prune: it skips any entry carrying `ScratchKeyPrefix`
entirely, not "prune it on a longer timer." Reusing `ScratchAllocPrefix` for
keyed dirs would let the very next `dg configure --force` (or first install)
delete a keyed hand-off out from under it — a later session re-deriving the
same key would find an empty, silently-recreated directory instead of the
file the earlier session left, with no error anywhere to say so.

**What breaks if two sessions pick the same key:** nothing crashes; whichever
session writes to a given filename last wins, and a session that only reads
back what it itself wrote under a key it alone knows is unaffected. The
directory is shared, not private — this must be stated in the command's own
`--help` text (Step A4), not only here.

**What happens after 24h:** nothing. An unkeyed (`ScratchAllocPrefix`)
directory older than a day is still pruned on the next `MaintainScratchDir`
pass, exactly as before. A keyed (`ScratchKeyPrefix`) directory is invisible
to that pass regardless of age and survives until a caller explicitly runs
`dg task scratch --clean <path>` on it. That is the durability a hand-off
needs — a session producing a file today for a session that might not run
until next week — bought at the cost of a directory that can now go stale
forever if nothing ever cleans it. `ScratchClean`'s bounds checks
(`validScratchChild`, `scratch.go:146-154`) are widened to accept **either**
prefix so `--clean` works uniformly on both forms; nothing else about its
containment checks changes.

### 3. Allocation is symlink-safe before an existing keyed directory is reused

The unkeyed path never revisits an existing directory — `os.MkdirTemp` always
creates a fresh one. The keyed path is the first place `Scratch` has to ask
"does this already exist, and is it safely mine to reuse?", because the whole
point of a keyed path is that a _second_ call with the same key must return
the _same_ directory rather than erroring.

"Safely mine to reuse" means the same defense `ScratchClean` already applies
on the delete side (`scratch.go:93-108`) applied here on the write side:
`Lstat` the keyed path before treating it as reusable. A symlink is refused
outright — never resolved-and-judged — because nothing this allocator ever
creates is a symlink, so one found there was substituted by something else,
and writing through it would write to whatever that something else points at.
A non-directory (a stray file) is refused the same way. Containment under the
resolved root is re-checked the same way `ScratchClean` re-checks it after
`EvalSymlinks`, so a symlinked _ancestor_ directory that quietly moved the
path outside the root is also caught. Any of these is an error, never a
silent fallback to creating a same-named-but-different directory — a caller
that hits this error has a real reason to stop and look, not a corner case to
paper over.

### 4. The sanitizer is a pure, adversarially-tested function

The key must survive as exactly one path element under the scratch root, so
it rejects: path separators (`/`, and `\` where relevant), `.`, `..`,
empty/whitespace-only keys, and — as a consequence of those rules rather than
a separate check — anything that would let the resulting name escape the
root. This is the same "pure function with an adversarial table" pattern
`ScratchClean`'s bounds checks already use, deliberately kept as a second,
independent layer rather than trusted to be the only one: the containment
re-check in §3 still runs even on a key the sanitizer accepted, the same way
`ScratchClean` still re-checks bounds after resolving symlinks even though the
lexical pass already ran.

## Consequences

- A file one agent session writes under a chosen key is findable by a later,
  independent session that knows the same key — the concrete capability this
  ADR exists to unlock.
- The unique-suffix path is completely unchanged for every existing caller
  that never passes a key: same allocation call, same prefix, same 24h prune,
  same `--clean` bounds (widened only to _additionally_ accept the new
  prefix, never narrowed).
- A keyed directory is a **shared** resource between whichever sessions use
  its key, not a private one — the command's help text must say this, and a
  caller choosing a low-entropy key (`"tmp"`, `"scratch"`) accepts the
  collision risk that entails.
- A keyed directory that nothing ever `--clean`s lives forever, unlike an
  unkeyed one, which is bounded by the 24h prune as a backstop even if a
  command forgets its own cleanup. This is the durability trade stated
  plainly in §2: a hand-off that cannot be silently reaped must be a hand-off
  that is never _automatically_ reaped either.
- Allocation gains one more failure mode — reusing an existing keyed path can
  now itself fail (symlink, non-directory, escaped containment) — where it
  previously could not fail short of a full disk or permissions error. That
  failure is intentional: it is the same class of defense `ScratchClean`
  already has on the delete side, now present on the write side too, so the
  two paths do not diverge in what they consider "ours to touch."
