# ADR-0040 — An archive is written in Go, not by shelling out to `tar`

**Date:** 2026-09-13
**Status:** ACCEPTED

## Context

`dg archive <folder> <destination>` packs a folder (typically `~/Documents`) into
one compressed file on an external drive, so a developer can carry their files to
a new machine. The archive must extract on macOS, Linux and Windows with standard
tools, and devgeta must be able to prove every file in it matches the source.

Formats were settled by research before this ADR (see the cycle doc
[2026-09-13-dg-archive](../plans/cycles/2026-09-13-dg-archive.md) §1):

- **Container: tar**, pax records only where a header needs them (long or
  non-ASCII names). GNU tar and libarchive/bsdtar — the tars shipped by Linux,
  macOS and Windows 10+ — read it fully.
- **Compression: zstd by default, gzip on request.** zstd is several times faster
  than gzip at similar ratio and passes incompressible data (photos, video) through
  cheaply. Windows 11 File Explorer opens `.tar.zst` since 23H2; Windows'
  `tar.exe` may not, so `--gzip` exists for zero-install Windows restores.

The open question is **how devgeta produces that file**. Two shapes were
considered.

**A. Shell out to the system `tar` (and `zstd`).** Walk in Go to decide what to
skip, pass the kept list with `-T`, pipe through `zstd -T0`.

- macOS ships bsdtar, Debian ships GNU tar; their flags for format, xattrs and
  file lists differ, so there are two command lines to keep correct.
- `zstd` is not installed on a stock Mac — a new install dependency for a
  command that should work on a fresh machine.
- tar cannot report a per-file checksum, so integrity verification needs a
  second full read of every source file.
- Tests could only mock the command, never check that the bytes in the archive
  are right.

**B. Write the archive in Go.** `archive/tar` (stdlib) writes the container;
`compress/gzip` (stdlib) or `github.com/klauspost/compress/zstd` compresses it.

- One code path on both platforms; no external binary, so no app wrapper is
  needed (CLAUDE.md §6 "route external tools through their app wrappers" does not
  apply).
- Each file is hashed while it is copied into the archive (an `io.TeeReader`
  into SHA-256), so the checksum manifest costs no extra read of the source.
- Tests build a real folder in `t.TempDir()`, archive it, read it back, and
  compare bytes — the thing that actually matters — with no mocks.
- Costs one new dependency.

## Decision

**B.** Devgeta writes the tar stream itself and compresses it with
`github.com/klauspost/compress/zstd` (or stdlib gzip with `--gzip`).

The dependency is surfaced here deliberately (CLAUDE.md §6 "prefer existing over
new"): the standard library has no zstd encoder, and no dependency devgeta
already uses has one. `klauspost/compress` is pure Go (no cgo, so cross-compiles
keep working), BSD-3 licensed, marked stable, continuously fuzzed, and the
de-facto Go zstd implementation. Its encoder compresses blocks on several
goroutines. Its output is not bit-identical to the reference `zstd` CLI but is a
standard zstd stream with frame checksums that `zstd`, bsdtar, 7-Zip and Windows
File Explorer decode.

## Consequences

- **Easier:** identical behavior on macOS and Linux; integrity checked with a
  byte-level round-trip in unit tests; checksums computed in the same pass that
  writes the archive; nothing to install before the command works.
- **Harder:** devgeta owns tar-writing details that `tar` would have handled —
  hard links (detected by device+inode and written as link entries), symlinks
  (stored, never followed), special files (sockets, FIFOs, devices: skipped and
  reported), and xattrs for `--mac-metadata` (written as `SCHILY.xattr.*` pax
  records, which GNU tar and libarchive both read, instead of AppleDouble `._`
  entries).
- **Accepted:** sparse files are archived at full size (Go's tar writer does not
  emit sparse entries); rare in a documents folder. Compression is somewhat
  slower than the C `zstd` at the same level; the dominant cost of these runs is
  file count and skipped junk, not the compressor.
- **Not a lock-in:** the output is a plain `.tar.zst`/`.tar.gz`; restoring never
  needs devgeta.
