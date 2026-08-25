# ADR-0032 — Session continuity is a durable note, not a longer session

**Date:** 2026-08-25
**Status:** PROPOSED

## Context

[ADR-0031](ADR-0031-context-is-reduced-at-write-time-not-at-send-time.md) closes
off request rewriting and caps the cost of _new_ verbose output. It cannot touch
the other half of the problem: a session that has been running for hours carries
its whole history in every request, so a one-line question late in the day draws
usage for the entire conversation.

There are three ways to carry work across that boundary, and they are genuinely
different bets:

**(a) Keep the session alive and compact it.** `/compact` summarizes history in
place. But compaction reads the conversation it is summarizing, so compacting a
large context is itself one of the largest requests you will make — and it
invalidates the cached prefix from the compaction point on. It also loses
information silently, which is the failure mode [Local-Splitter][localsplitter]
measured: aggressive summarization made agents redo work, and simple truncation
outperformed it.

**(b) Install an automatic memory layer.** The mem0/claude-mem class of tool
captures what happened and injects relevant context into future sessions. The
structural problem is that a memory file dumped in full on every call is a
permanent per-request tax, and one stale entry costs you on every future
inference for as long as it lives. Reported traces of continuously-running
agents reach 80k–120k token contexts within two to three weeks. The tools that
handle this well do it with an index-then-fetch shape — a ~130-token index for a
200-entry store, versus ~4,600 tokens for a naive dump — but that is a
retrieval system, with a retrieval system's failure modes, installed into every
user's agent loop. Same shipped-trust objection as ADR-0031's constraint 3.

**(c) End the session and start a fresh one from a small durable note.** `/clear`
costs nothing. The cost is that whatever was in the agent's head is gone unless
something wrote it down.

Devgeta has already answered a structurally identical question once. Review
knowledge could have lived in the session, or in an injected memory store;
[ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md) put it in a
per-branch markdown file under `<git common dir>/devgeta/review/`, written and
read through `devgeta task` commands. That journal is now load-bearing for the
review loop, `worktree-finish`, and the merge gate. The pattern works.

## Decision

**Work-in-progress state carried between sessions is an explicit, per-branch
markdown note that devgeta owns, written and read on demand — never an
auto-injected memory store, and never a reason to keep a session open.**

Five parts, deliberately mirroring ADR-0012 rather than inventing a second
shape.

### 1. One file per branch, in the git common dir

`<git common dir>/devgeta/handoff/<encoded-branch>.md`, beside
`devgeta/review/`. Keyed by **branch**, not worktree path and not session id: a
branch survives worktree churn, `/clear`, a machine reboot, and switching
between Claude Code and OpenCode. The common dir means every worktree of the
same repo sees the same note.

The storage concern — encoded branch filenames, atomic write, locking — is
already solved in `internal/tooling/reviewjournal/manager.go`. That layer gets
extracted and shared rather than copied ([CLAUDE.md §6, reuse before
writing](../../CLAUDE.md#coding-standards)).

### 2. Plain markdown, hand-editable, bounded

Human-readable, editable in an editor, and **small by contract** — a size cap
enforced on write, with the note rejected rather than silently truncated when it
is exceeded. A handoff note that grows without bound becomes the memory-file tax
this ADR exists to avoid. The cap is the mechanism that keeps it honest.

### 3. Read on demand, never auto-injected

Nothing appends the note to a prompt. It is read when a session asks for it, the
same way the review journal is read. This is the single most important line in
this ADR: an auto-injected note is a memory store wearing a different hat, and
it re-acquires the per-request tax whether or not the current task needs it.

### 4. Written explicitly, at a checkpoint

The note is written by an explicit command, at a moment the human or the agent
decides is a checkpoint — not by a `SessionEnd` hook firing on every exit.
Automatic capture is what produces the accumulating, low-signal store; a
checkpoint that someone chose to take is what produces a note worth reading. It
is the same discipline as a commit message.

### 5. Append-stable content

The note records what was decided and what is left, not a transcript. Because it
is read into a fresh session's early context, its content should be stable
across reads so the new session's own prefix caches cleanly.

## Consequences

**Easier:**

- The expensive path stops being the default. `/clear` plus a 500-token note
  replaces resuming a 200k-token session, and the note is the same few hundred
  tokens on day one and day ten.
- Continuity outlives the session, the worktree, and the agent. A note written
  from Claude Code is readable from OpenCode; a note written before a reboot is
  there after it.
- It composes with what exists. Same directory, same storage layer, same
  `devgeta task` surface, same per-branch key as the review journal — so
  `worktree-start` can read it and `worktree-finish` can retire it without new
  plumbing.
- Nothing new sits in the agent's request path, which keeps ADR-0031's
  shipped-trust position intact.

**Harder:**

- It requires a habit. An explicit checkpoint that nobody takes leaves an empty
  note, and the failure is silent — you only notice when the fresh session does
  not know something. Mitigating this with automatic capture is exactly the
  trade this ADR refuses, so the answer has to be making the checkpoint cheap
  and visible, not making it implicit.
- Notes can go stale. The branch moves on and the note does not. The review
  journal's answer — stamping entries against the head they were judged at — is
  the available precedent if this bites.
- We get no automatic recall. A fact the note does not mention is gone. That is
  the deliberate cost of not running a retrieval system.

**Accepted trade-offs:**

- **Manual beats automatic here, against the usual instinct.** Devgeta normally
  prefers making the right thing structural over relying on a convention people
  must remember ([CLAUDE.md §4](../../CLAUDE.md#engineering-discipline)). This
  is a documented exception: the structural version of "capture everything" has
  a measured cost that grows every week, and the mechanism cannot tell signal
  from noise. So the human picks the checkpoint. What _is_ structural is the
  size cap and the read-on-demand rule — the two places where drift would
  actually hurt.
- **A user who wants claude-mem or mem0 can install one.** As with ADR-0031, we
  are choosing what devgeta ships and recommends.

[localsplitter]: https://arxiv.org/pdf/2604.12301
