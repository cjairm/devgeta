# ADR-0042 — An archive refuses on proof and warns on prediction

**Date:** 2026-09-14
**Status:** ACCEPTED

## Context

`dg archive` writes to an external drive that already holds things the user
cares about, and the run takes hours. Two failure conditions can be spotted
before the write starts, and they are not the same kind of thing:

1. **A file the run would write is already on the drive.** Checkable with a
   `stat`. If it is there, it is there.
2. **The destination has less free space than the source's uncompressed size.**
   Also checkable — but it does not mean the run will fail. The output is
   compressed, and by how much is not knowable until it is written. A folder of
   text and code routinely lands at a third of its size; a folder of photos and
   video barely moves.

v1.27.0 handled the first case too narrowly and the second not at all.

On the first: the implementation checked one path, the final archive name.
But the write phase renames **three** `.partial` files into place
unconditionally, and the verify phase writes a fourth (`<name>.tar.zst.sha256`).
A destination holding a previous run's `<name>.sha256` or `<name>.skipped.txt`
without its archive — the shape you get when someone deletes a big file to free
space and leaves the small ones — had those replaced with no message. Narrow,
but it is the one way the command could destroy data, and "never overwrites"
was already the documented promise.

On the second: a user with 120 GiB free and 200 GiB of Documents got no signal
at all until the write died hours in.

The question this ADR settles: **when does `dg archive` refuse, and when does
it warn?**

## Decision

**Refuse on proof. Warn on prediction.**

A condition that is _established fact at scan time_ aborts the run before
anything is created. A condition that is a _forecast_ — true now, possibly
irrelevant by the end — is reported and the run continues.

Concretely:

- **Refuse** if any of the four files a run writes already exists: the archive,
  the manifest, the skip report, or the archive checksum. The error names every
  one it found. (A stale `.partial` is still overwritten — it is this command's
  own scratch space, not the user's data.)
- **Warn** if free space is below the source's uncompressed size, reporting
  both numbers and stating that a run which does exhaust the drive is discarded
  whole.

The warning is honest about the guarantee behind it: running out mid-write was
already safe. Every output is a `.partial` until the very end, and any failure
deletes every `.partial` the run created. There is nothing to clean up and
nothing pre-existing is touched.

Two rejected alternatives:

- **Refuse when free space is below the uncompressed total.** Blocks runs that
  fit comfortably — for compressible sources, most of them. It converts a
  forecast into a veto, and the user's recourse is a flag to override it, which
  is a warning with extra steps.
- **Refuse below some fraction of the total** (say 40%, assuming a typical
  ratio). The threshold would be invented. There is no ratio that holds across
  a code folder and a photo library, and a number with no basis reads as
  authoritative when it is a guess.

## Consequences

**Easier.** The command's promise is now a single sentence that holds without
exception: it reads the source, writes four new files, and never replaces or
removes anything else on the destination. The one real path to data loss is
closed. A short-on-space user learns it in the scan summary rather than three
hours later, and can stop and free space before committing.

**Harder.** Re-running on a destination that already has a complete archive
from the same source on the same day now requires deleting all four files, not
just the big one. That is more friction, and it is the point: whatever is on
the drive stays there until a person removes it.

**Accepted trade-off.** The free-space warning will sometimes fire on a run
that would have finished with room to spare, because the source compresses
well. A warning that is occasionally unnecessary is the right cost for never
vetoing a run that would have worked. The inverse — staying silent and letting
someone discover at hour three — is the failure this avoids, and a warning is
the strongest signal available that does not require knowing the future.

**Scope.** This governs `dg archive` only, but the rule generalizes to any
devgeta command with a slow, destination-touching write: check what is
knowable, refuse only on what is known, and say the rest out loud.
