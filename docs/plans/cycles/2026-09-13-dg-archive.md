# Cycle: `dg archive` — pack a folder onto an external drive for a machine move

**Date:** 2026-09-13
**Estimated Duration:** ~10 hours
**Status:** Done — shipped in v1.27.0. Two items planned here landed in a
follow-up (see [§9](#9-follow-up-after-v1270)): the byte progress meter §5
called for, and a widened overwrite guard.

---

## 1. Domain Context

Devgeta moves a developer's _environment_ to a new machine. Their _files_ still
move by hand, and without a cloud service that usually means compressing
`~/Documents` onto an external SSD. That often takes days, for two reasons that
have nothing to do with the compressor:

- **File count.** One `node_modules` or virtualenv holds 100k+ tiny files; every
  file costs a lookup and a tar header. Most of them can be rebuilt.
- **Wasted work.** Slow compressors (gzip, zip) re-compressing photos and video.

`dg archive <folder> <destination-dir>` writes one `.tar.zst` straight onto the
drive, skipping only folders that are provably regenerable, and then proves the
archive matches the source. It is a one-shot archive for a move — not an
incremental backup (restic and borg do that well).

Decisions this cycle implements, and does not re-argue:

- [ADR-0040](../../decisions/ADR-0040-an-archive-is-written-in-go-not-by-shelling-out-to-tar.md) — written in Go (`archive/tar` + `klauspost/compress/zstd`), not by shelling out.
- [ADR-0041](../../decisions/ADR-0041-an-archive-skips-only-what-is-provably-regenerable.md) — the skip rules, and what is never skipped.

Research behind the format choices (2026-09-13):

- tar opens natively on all three OSes; Windows 10+ ships a libarchive-based `tar`
  ([Microsoft Learn](https://learn.microsoft.com/en-us/windows/tar/)). Windows 11
  File Explorer extracts `.tar.zst` since 23H2
  ([Pureinfotech](https://pureinfotech.com/window-11-extract-rar-7zip-archival-formats/));
  Windows `tar.exe` may lack zstd, hence `--gzip`.
- pax extended headers carry long and UTF-8 names; GNU tar and libarchive read
  them fully, BusyBox and 7-Zip less so ([mgorny](https://mgorny.pl/articles/portability-of-tar-features.html)).
- macOS `tar` stores xattrs as AppleDouble `._` entries that appear as junk files
  elsewhere ([aruljohn](https://aruljohn.com/blog/macos-created-tar-files-linux-errors/)) —
  so metadata is off by default, and when on, is written as pax xattr records.
- Windows cannot create names containing `:<>"?*|`, ending in `.` or space,
  reserved names (`CON`, `NUL`, `COM1`…), or names differing only by case
  ([Microsoft Learn](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file)).
- exFAT is the only filesystem all three OSes read and write natively; FAT32 caps
  files at 4 GiB.
- iCloud "Optimize Mac Storage" leaves _dataless_ placeholder files
  (`SF_DATALESS` stat flag) whose content is not on disk.

---

## 2. Engineer Context

**Relevant files:**

- `cmd/workspace.go` — shape of a small top-level command to copy.
- `docs/guides/cli-patterns.md` — command structure; "commands orchestrate, logic
  lives in `internal/tooling/`".
- `pkg/utils/print.go` — `PrintInfo/Success/Warning/Error`; `pkg/utils/exit.go` —
  `MaybeExitWithError`.
- `pkg/promptui/selector.go` — `DisplayInstructions(label, text, isConfirm)` for the
  pre-write confirmation.
- `golang.org/x/sys/unix` (already a dependency) — `Statfs` (darwin + linux),
  `SF_DATALESS`, `Listxattr`/`Getxattr`, `Access`.

**New package:** `internal/tooling/archive/`. No app wrapper and no command
executor: nothing external is run (ADR-0040).

**Key facts an implementer will otherwise rediscover:**

- Go's `tar.Writer` picks USTAR, then PAX, then GNU per header when
  `Header.Format` is unset — leave it unset. Set `ModTime` truncated to the second
  unless PAX is already required, or every header becomes PAX.
- Go's tar writer does not detect hard links. Track `(dev, ino)` for regular files
  with `Nlink > 1`; write later occurrences as `TypeLink` to the first path.
- `filepath.WalkDir` does not follow symlinks — good; store them as `TypeSymlink`
  with `os.Readlink`.
- A `CACHEDIR.TAG` counts only if it is a regular file whose first 43 bytes are
  exactly `Signature: 8a477f597d28d172789f06886806bc55`.
- On darwin, `Statfs_t.Fstypename` is `msdos` for FAT; on linux, `Statfs_t.Type`
  `0x4d44` is MSDOS/FAT. exFAT is `exfat` / `0x2011BAB0` and is fine.
- The manifest uses `sha256sum` format (`<hex>  <path>`) so a restore can be
  checked with `shasum -a 256 -c` without devgeta. Paths containing `\` or a
  newline must use the GNU escaped form: line starts with `\`, and `\\` / `\n` in
  the path.
- Tests may write real files — only under `t.TempDir()`. The `pkg/paths` sandbox
  already guards HOME; nothing here should touch HOME at all.

**Commands to run tests** (targeted, CLAUDE.md §6):

```bash
go test ./internal/tooling/archive/
go test -run TestArchive ./cmd/
make lint
```

---

## 3. Objective

`dg archive ~/Documents /Volumes/SSD` writes `Documents-2026-09-13.tar.zst`, its
checksum manifest and skip report onto the drive, verifies the archive against
the manifest, and a plain `tar` on macOS, Linux and Windows extracts every
non-skipped file byte-for-byte.

---

## 4. Scope Boundary

### In Scope

- [x] `dg archive <source> <destination-dir>` with flags `--dry-run`, `--gzip`,
      `--no-skip`, `--no-verify`, `--mac-metadata`, `--yes`
- [x] `dg archive verify <archive-file>` — re-check an archive against its manifest
- [x] Skip rules exactly as ADR-0041, with a positive and negative test per rule
- [x] Scan phase report: kept size and file count, each skip with rule and size,
      unreadable files, special files, iCloud-only files, Windows-incompatible names
- [x] Atomic outputs (`.partial` then rename), refusal checks (below), verification
- [x] Docs: `docs/spec.md` command reference, README, `ROADMAP.md`, `cli-patterns.md`
      planned-commands table, `docs/recent-changes.md`

### Explicitly Out of Scope

- Extraction/restore command — plain `tar -xf` is the restore path.
- Incremental or deduplicated backups; "already copied" detection.
- Encryption (users can use an encrypted volume, or `age`/`gpg` the file).
- Splitting output into chunks (FAT32 is refused, not worked around).
- User-supplied `--exclude` patterns.
- Downloading iCloud placeholder files for the user.
- Sparse-file entries (ADR-0040, accepted).

**Scope is locked.** Anything else found necessary goes to a follow-up cycle.

---

## 5. Implementation Plan

### Behavior

**Three phases, one pass each:**

1. **Scan (metadata only).** Walk the source with `WalkDir`, applying skip rules at
   each directory so skipped trees are never descended. `Lstat` each kept entry;
   `unix.Access(R_OK)` each file and directory. Produces the ordered entry list,
   total bytes, and the report. `--dry-run` stops here and prints it. The scan
   never reads file contents, so it is fast even on large trees.
2. **Write.** Stream `tar → zstd/gzip → <dest>/<name>.tar.zst.partial` in scan
   order, hashing each file's bytes as they are copied. Manifest to
   `<name>.sha256.partial`, skip report to `<name>.skipped.txt.partial`. Progress
   by bytes against the scan total. On success, fsync and rename all three.
3. **Verify** (unless `--no-verify`). Re-open the renamed archive **from the
   destination drive**, decompress, and hash every regular-file entry; compare
   against the manifest (same set of paths, same hashes). Then write
   `<name>.tar.zst.sha256` holding the hash of the archive file itself.

**Output name:** `<basename(source)>-<YYYY-MM-DD>.tar.zst` (`.tar.gz` with `--gzip`).

**Refuse before writing (error, nothing created):**

- source is not a directory; destination is not an existing directory
- destination is inside source (the archive would archive itself)
- the final archive name already exists (never overwrite; a stale `.partial` from a
  crashed run is overwritten)
- destination filesystem is FAT (4 GiB file limit) — message suggests exFAT
- scan found **unreadable** files or **iCloud-only** files — list them (first 20 +
  count) and explain the fix: fix permissions, or download in Finder
  ("Download Now"). Aborting before the write avoids failing hours in.

**Warn, continue:**

- Windows-incompatible names — listed; archived unchanged (renaming would alter files)
- special files (sockets, FIFOs, devices) — skipped and listed
- a file whose size or mtime changed between scan and write, or disappeared —
  listed; the manifest hashes what was actually archived

**Failure during write** (disk full, drive unplugged, read error): delete the
`.partial` files, exit non-zero with the path that failed. Never leave a
non-`.partial` file that is incomplete.

**Confirmation:** after the scan summary, confirm with `DisplayInstructions` when
stdin is a TTY; `--yes` skips it; non-TTY without `--yes` refuses (reuse `cmd/tty.go`).

**`--mac-metadata`** (darwin only; flag errors on linux): add each file's xattrs as
`SCHILY.xattr.<name>` pax records. Never AppleDouble entries.

### File Changes

| Action | File Path                                                                          | Description                                                       |
| ------ | ---------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| Modify | `go.mod`, `go.sum`                                                                 | Add `github.com/klauspost/compress`                               |
| Create | `internal/tooling/archive/skiprules.go`                                            | ADR-0041 rule table + `Match(dir) (rule, skip bool)`              |
| Create | `internal/tooling/archive/scan.go`                                                 | Phase 1: walk, entry list, report                                 |
| Create | `internal/tooling/archive/write.go`                                                | Phase 2: tar stream, hashing, hard links, symlinks, atomic rename |
| Create | `internal/tooling/archive/verify.go`                                               | Phase 3 and `dg archive verify`                                   |
| Create | `internal/tooling/archive/manifest.go`                                             | sha256sum-format read/write incl. escaped paths                   |
| Create | `internal/tooling/archive/winnames.go`                                             | Windows-name check                                                |
| Create | `internal/tooling/archive/platform_darwin.go`                                      | FAT detection, `SF_DATALESS`, xattr read                          |
| Create | `internal/tooling/archive/platform_linux.go`                                       | FAT detection; dataless = never; xattrs unsupported               |
| Create | `internal/tooling/archive/*_test.go`                                               | One test file per source file                                     |
| Create | `cmd/archive.go`, `cmd/archive_test.go`                                            | Cobra command + `verify` subcommand; flag parsing and refusals    |
| Modify | `docs/spec.md`                                                                     | `#### dg archive` under Command Reference                         |
| Modify | `README.md`, `ROADMAP.md`, `docs/guides/cli-patterns.md`, `docs/recent-changes.md` | Document; note `dg backup` stays reserved for config snapshots    |

### Step-by-Step

#### Step 1: Skip rules

- `skiprules.go`: rule table from ADR-0041; `CACHEDIR.TAG` signature check;
  `pyvenv.cfg` content rule; AppleDouble magic check for `._*`.
- Tests: for every rule, a fixture that skips **and** a fixture without the proof
  that does not (e.g. `vendor/` with `modules.txt` + `composer.json` is kept;
  `CACHEDIR.TAG` with a wrong signature is kept).
- Verify: `go test ./internal/tooling/archive/ -run TestSkip`

#### Step 2: Manifest and Windows names

- `manifest.go` round-trip including a path with `\` and a newline; output is
  accepted by `shasum -a 256 -c` (checked manually once, recorded in §8).
- `winnames.go`: reserved chars, trailing dot/space, reserved names with and
  without extension, case-only collisions within one directory.
- Verify: `go test ./internal/tooling/archive/ -run 'TestManifest|TestWinNames'`

#### Step 3: Scan

- Entry list order is deterministic (WalkDir lexical order). Report totals, skips
  with sizes (size of skipped trees computed only in `--dry-run`, since it costs a
  walk of exactly what we avoid), unreadable, special files, Windows names.
- Platform hooks: dataless detection (darwin), FAT detection.
- Tests: fixture tree in `t.TempDir()` with an unreadable file (`chmod 000`, skip
  when running as root), a FIFO, a symlink, a hard link.
- Verify: `go test ./internal/tooling/archive/ -run TestScan`

#### Step 4: Write

- `klauspost/compress/zstd` encoder at `SpeedDefault`, concurrency `GOMAXPROCS`;
  `compress/gzip` for `--gzip`. `.partial` files, fsync, rename.
- Hard links as `TypeLink`, symlinks as `TypeSymlink`, directories with mode and
  mtime, changed/vanished files recorded.
- Tests: archive a fixture, read back with `archive/tar` + decoder, compare every
  byte, mode, mtime (second precision), link target; failure mid-write (writer
  that errors after N bytes) leaves no final-named file.
- Verify: `go test ./internal/tooling/archive/ -run TestWrite`

#### Step 5: Verify

- Phase 3 + standalone `Verify(archivePath)` reading the sibling manifest; detects
  a flipped byte, a missing entry, an extra entry.
- Verify: `go test ./internal/tooling/archive/ -run TestVerify`

#### Step 6: Command

- `cmd/archive.go`: args, flags, refusals, TTY confirmation, output through
  `pkg/utils` print helpers, errors through `MaybeExitWithError`.
- `--mac-metadata` on linux → clear error.
- Verify: `go test -run TestArchive ./cmd/` and `go build ./...`

#### Step 7: `--mac-metadata`

- darwin: `Listxattr`/`Getxattr` → `SCHILY.xattr.*` records.
- Test (darwin-only build tag): set `com.apple.metadata:_kMDItemUserTags` on a
  fixture file, archive, confirm the record is present and no `._` entry exists.

#### Step 8: Docs

- `docs/spec.md`, `README.md`, `ROADMAP.md`, `cli-patterns.md` table,
  `docs/recent-changes.md`. Include the restore commands for each OS.

---

## 6. Verification Plan

### Automated Verification

```bash
go test ./internal/tooling/archive/ ./cmd/
make lint
go test ./internal/tooling/archive/ -cover
```

`go.mod` changes, so also `go build ./...` and `make all` (cross-compile must still
work without cgo).

### Manual Verification

1. Build a fixture folder with a Node project (`package.json` + `node_modules`), a
   Go project with committed `vendor/modules.txt`, a `.venv`, a `.env` file, a
   `.git` dir, a file named `notes:draft.txt`, a symlink, a hard link, and a
   non-ASCII long path.
2. `dg archive <fixture> <dest> --dry-run` → `node_modules` and `.venv` listed as
   skipped with rule; `vendor/`, `.env`, `.git` kept; `notes:draft.txt` warned.
3. `dg archive <fixture> <dest>` → three files on the destination, verify passes.
4. Extract with `tar -xf` on macOS (bsdtar) and in a Debian container (GNU tar
   ≥ 1.31 or `zstd -d | tar -x`); `diff -r` against the fixture minus skips;
   `shasum -a 256 -c` the manifest from the extracted root.
5. Extract the same file in Windows 11 File Explorer (or ask a Windows user) and
   with `--gzip` via `tar -xf` in PowerShell; confirm only `notes:draft.txt` fails.
6. Corrupt one byte of the archive → `dg archive verify` fails naming the entry.
7. Pull the destination mid-write (or fill a small disk image) → no final-named
   file remains, clear error.
8. Real run on `~/Documents` to an exFAT SSD; record file count, skipped size and
   wall time in §8.

### Regression Check

- `dg install --help`, `dg version` unchanged; `make all` builds all targets.

---

## 7. Risks & Trade-offs

| Risk                                                         | Likelihood | Mitigation                                                            |
| ------------------------------------------------------------ | ---------- | --------------------------------------------------------------------- |
| A skip rule removes a hand-written folder                    | Low        | Proof-only rules, negative test per rule, dry-run report, `--no-skip` |
| Output some extractor cannot read (pax, zstd from Go)        | Low        | Manual extraction on bsdtar, GNU tar, Windows Explorer before Done    |
| Dataless iCloud files read as empty or trigger huge download | Med        | Detected in scan; run refuses before writing                          |
| Hours-long run fails on one unreadable file                  | Med        | Readability checked in scan, before any write                         |
| Files changed while archiving                                | Med        | Listed in report; manifest reflects archived bytes                    |
| Verify doubles total time                                    | High       | SSD sequential read is fast; `--no-verify` exists; default stays on   |

### Trade-offs Made

- **Refuse vs. skip unreadable/iCloud-only files:** refuse. Silent skips break the
  "nothing I wrote is lost" promise; the user fixes it and re-runs.
- **Warn vs. rename Windows-incompatible names:** warn. Renaming alters the user's
  files and breaks the manifest's meaning.
- **Metadata off by default:** portability over Finder tags; `--mac-metadata`
  opts in.
- **zstd default over gzip:** speed over zero-install extraction on Windows'
  command line; `--gzip` opts out.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope locked?
- [ ] Steps actionable?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**

---

## 9. Follow-up after v1.27.0

v1.27.0 shipped the three phases but not the progress reporting §5 specified,
and its overwrite guard turned out to be narrower than §5 intended. Both were
closed in a follow-up:

- [x] **Byte progress for the write and the verify.** §5 called for "progress
      by bytes against the scan total" and it was never wired up — a large
      archive printed one line and then nothing for hours. Both long phases now
      report a bar, percent, bytes done over total, rate, and time left. The
      renderer is `pkg/progress`, deliberately outside this package: the
      archive phases emit a `ProgressFunc` callback and know nothing about
      terminals.

      Cost was the constraint, since this runs over hundreds of GB. The hot
          path is one atomic add per buffer; a single goroutine does all
          formatting and I/O on a fixed tick. There is no second pass over the
          data — both phases already stream every byte through a reader.
          Measured against the scan total for the write, and against the
          archive's size on disk for the verify (that pass reads compressed
          bytes back off the drive, so the source total would be the wrong
          denominator).

- [x] **The overwrite guard now covers every output, not just the archive.**
      §5 said "the final archive name already exists (never overwrite)", and
      the implementation checked exactly that one path — but the write phase
      renames all three `.partial` files into place unconditionally, and the
      verify phase writes a fourth file. A destination holding a previous
      run's `<name>.sha256` or `<name>.skipped.txt` without its archive had
      those replaced silently. All four paths are checked before anything is
      created.

- [x] **Free space is reported, and flagged when it is short.** A warning
      rather than a refusal: the output is compressed and the ratio is not
      knowable until it is written, so refusing on the uncompressed total
      would block runs that fit comfortably. Running out mid-write was
      already safe — every output is a `.partial` until the end, and a
      failure deletes them.

- [x] **Source reads use a 1 MiB buffer.** The default 32 KiB `io.Copy` chunk
      is ~32,000 reads per GiB, which an external drive answers far slower
      than the same bytes asked for in large sequential runs — and this
      command exists to move large files onto exactly that kind of drive.

Not changed, and worth stating because it is the guarantee the guard rests on:
the source is only ever opened for reading, and the only removal a run performs
is of the `.partial` files it created itself, on failure.

---

## Notes for Implementers

- **Cycle document is your spec.** Update it if requirements change; call out scope changes.
- **Commit after each step** once its verify check passes.
- **Verification must pass before "done":** targeted tests + manual extraction on
  all three OSes + regression check.
- **Escalate risks immediately.**
