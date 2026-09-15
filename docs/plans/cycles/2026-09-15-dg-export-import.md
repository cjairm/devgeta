# Cycle: dg export / dg import — moving app state between machines

**Date:** 2026-09-15
**Estimated Duration:** ~10 hours
**Status:** Done

---

## 1. Domain Context

Devgeta installs apps (`dg install`) and applies devgeta's own configs to them
(`dg configure`). Neither carries the state a user accumulated _inside_ an app —
browser bookmarks, open tabs, extension settings. On every hardware change that is
hand-copied from memory, which is the recurring gap this cycle closes.

`dg archive` (ADR-0040/0041/0042) already packs a named folder onto a drive for a
machine move, and it can technically pack a browser profile today. It should not:
the Brave profile measured on the maintainer's machine is **3.8G, of which the
state worth moving is ~22M**, and a faithful copy also reinstates files that are
bound to the machine that wrote them (`Secure Preferences` HMACs, `Login Data`
encrypted against the local Keychain, `Local State`'s encryption key).

**[ADR-0045](../../decisions/ADR-0045-portable-app-state-is-an-allowlist-not-a-smaller-archive.md)
decides the shape: an allowlist, in a command separate from `dg archive`.** Read it
before this doc — it is the spec for _what_ moves and _why_ each group is on, off,
or absent. This doc is the spec for how it gets built.

Related: [docs/spec.md](../../spec.md#dg-archive) (`dg archive` reference),
[CLAUDE.md §3 principle 8](../../../CLAUDE.md) (the feature must be general —
Brave is one adapter of a mechanism, not a Brave feature).

---

## 2. Engineer Context

**Relevant files and their purposes:**

- `internal/tooling/archive/write.go` — `Write(sourceRoot, destDir, name string, scan *ScanResult, opts WriteOptions) (*WriteResult, error)`. Streams entries into one `.tar.zst`/`.tar.gz`, hashes as it copies, writes to `.partial` files and renames atomically. **Reused verbatim.**
- `internal/tooling/archive/verify.go` — `Verify(archivePath string, opts VerifyOptions)`. Re-reads and hashes against the sibling manifest. **Reused verbatim.** Note its one side effect: on success it also writes `<archive>.sha256` beside the archive — the archive file's own checksum, a different file from the `.sha256` manifest it read. Step 5 has to account for that, because a bundle can arrive on a read-only drive.
- `internal/tooling/archive/manifest.go` — `WriteManifest` / `ReadManifest`, sha256sum text format. **Reused verbatim.**
- `internal/tooling/archive/scan.go` — `Scan(root, ScanOptions) (*ScanResult, error)`, and `ScanResult{Entries []Entry, TotalBytes int64, …}`. **Not called by this cycle** — `Scan` is where ADR-0041's skip rules live, and those must not apply here. This cycle builds a `*ScanResult` from the allowlist and hands it to `Write`.
- `internal/tooling/archive/skiprules.go` — **do not touch.** ADR-0041 stays as it is.
- `internal/tooling/archive/progress.go` — `ProgressFunc`, wired to `pkg/progress` in `cmd/archive.go`. Reused for the byte meter.
- `internal/apps/contract.go` — `App` plus the optional capability interfaces (`SelectiveConfigurer`, `ThemedConfigurer`, `LiveThemeApplier`, `FontInstaller`). The new adapter interface goes here, following exactly that pattern.
- `internal/apps/registry/registry.go` — `GetApp(name string) (apps.App, error)`, `Names()`, `Meta`. How `dg configure <app>` resolves a name; `dg export <app>` must resolve identically.
- `internal/apps/brave/brave.go` — the Brave module. `SoftConfigure` is currently a no-op ("No configuration needed for GUI-based browser"), and the struct holds only `Cmd` — so it has no platform check yet, which Step 6 needs to pick Brave's data dir. `internal/apps/ghostty/ghostty.go` is the shape to copy: `Cmd` plus `Base cmd.BaseCommandExecutor`, branching on `Base.IsMac()`.
- `cmd/archive.go` — the closest CLI precedent: `--dry-run`, size/free-space checks, progress bars, the `printRestoreHint` pattern, the `archiveGOOS` / `archiveNow` / `archiveFreeBytes` indirections that let tests exercise OS- and disk-dependent paths, and `archiveOutputPaths` / `refuseIfAnyOutputExists` — the "nothing already on the drive is touched" check. `cmd/export.go` is in the same package, so it calls those two rather than copying them (Step 4).

**Key types this cycle adds:**

- `apps.StateGroup` — `{Name string, Paths []string, Default bool, Why string}`. Declared in `internal/apps/contract.go` (not in the new tooling package) so `internal/tooling/appstate` can import `internal/apps` without a cycle. Same reason `internal/theme` is a leaf that `internal/apps` imports for `ThemedConfigurer`.
- `apps.StatePorter` — the optional interface: `StateRoots() (map[string]string, error)`, `StateGroups() []StateGroup`, `IsRunning() (bool, error)`. `StateRoots` maps a **profile key** to that profile's absolute root, and every root it returns must be a direct child of one shared base directory (Brave's profiles all sit under its user data dir). That base is what `archive.Write` receives as `sourceRoot`, and the key is the bundle's top-level directory — which is the only way one tar can hold two profiles. `archive.Entry.Path` is relative to a single scan root (`scan.go:29`) and `write.go` writes it as the tar member name verbatim, so without the key as a prefix both profiles' `Bookmarks` are the same member.
- `internal/tooling/appstate` — orchestration: resolve profiles and groups → build `*archive.ScanResult` → `archive.Write` → `archive.Verify`; and the import direction as an all-or-nothing backup-then-replace.
- `internal/theme/recovery.go` — `dg theme set`'s backup/restore primitives (`BackupPath`, `RestorePath`, `DiscardBackup`): rename the target aside to a sibling, or drop an "absent" marker when there was no target, so an undo is a same-filesystem rename. Import needs exactly this, so Step 5 moves the three into `pkg/files` with the suffix pair as a parameter rather than copying them (CLAUDE.md §6 — extract on the second use).

**Testing patterns used in this area:**

- [docs/guides/testing-patterns.md](../../guides/testing-patterns.md) — `testutil.MockApp`, `testutil.VerifyNoRealCommands(t, mockApp.Base)`, `func init() { testutil.InitLogger() }`.
- Both directions touch real paths, so **every test must isolate its roots** with `testutil.SetupCompleteTest` or explicit `paths.Paths.*` overrides restored via `t.Cleanup` — this is the checklist item CLAUDE.md §6 calls out for anything Uninstall/ForceConfigure-shaped, and an import is more destructive than either.
- `IsRunning` must be injectable. Follow `archive.go`'s `archiveGOOS`/`archiveFreeBytes` indirection rather than calling `pgrep` in a test.

**Commands to run tests** (targeted — see §6 below for the derivation):

```bash
go test ./internal/tooling/appstate/ ./internal/apps/brave/ ./internal/apps/registry/ \
        ./internal/tooling/archive/ ./internal/tooling/desktop/ ./cmd/
make lint
```

---

## 3. Objective

`dg export brave <dest-dir>` writes one verifiable bundle holding only Brave's
allowlisted state, and `dg import brave <bundle>` restores it onto another machine
without ever moving a credential store or a machine-bound file — built as a general
mechanism any app can adopt by implementing one interface.

---

## 4. Scope Boundary

### In Scope

- [x] `apps.StateGroup` + `apps.StatePorter` in `internal/apps/contract.go`
- [x] `internal/tooling/appstate` — allowlist collection into an `*archive.ScanResult` with profiles namespaced in the bundle, export via `archive.Write` + `archive.Verify`, import as an all-or-nothing backup-then-replace
- [x] The denylist test: no adapter's allowlist may name a credential or machine-bound file (ADR-0045)
- [x] Brave adapter in `internal/apps/brave/state.go` — the six groups in [ADR-0045's group table](../../decisions/ADR-0045-portable-app-state-is-an-allowlist-not-a-smaller-archive.md#braves-groups) with its names, paths and defaults, and nothing else; multi-profile (`Default`, `Profile 1`, `Profile 5`, …)
- [x] `cmd/export.go` and `cmd/import.go` with `--dry-run`, `--group`, `--profile`, `--force`, registered in `cmd/root.go`
- [x] Running-app refusal in both directions
- [x] Bundle app identity: the bundle's name says which app wrote it, and `dg import <app>` refuses a bundle written for a different app before it touches anything
- [x] Tests for all of the above, fully isolated, no real commands
- [x] Docs: `docs/spec.md` reference sections, `README.md` command list, new `docs/apps/brave.md`

### Explicitly Out of Scope

- **Any second adapter.** Chrome, VS Code, shell history, `known_hosts` and tmux are the reason the mechanism is general, but shipping them is a later cycle. If the interface turns out to need a change to fit adapter two, that is the signal — not a reason to widen this cycle.
- **Credentials and cookies, at any opt-in level.** ADR-0045 forbids them outright.
- **Changing `dg archive` in any way**, including its skip rules, its flags, or `Scan`.
- **A sync/daemon mode.** This is an explicit two-command move, not continuous replication; Brave Sync already occupies that space.
- **`Favicons` and `Top Sites`** — omitted entirely, no flag; both rebuild themselves (ADR-0045).
- **Windows.** Same as `dg archive`: macOS and Debian/Ubuntu only.

**Scope is locked.** If you discover something out of scope is needed, document it
for a future cycle and reference it here.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                    | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| ------ | -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Modify | `internal/apps/contract.go`                  | Add `StateGroup` type and `StatePorter` optional interface                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Create | `internal/tooling/appstate/collect.go`       | Allowlist walk → `*archive.ScanResult`; profile and group resolution, sizing                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Create | `internal/tooling/appstate/collect_test.go`  | Profile and group selection, the `<profile>/…` layout, a file-path group and a directory-path group each collected, sizing, missing-path tolerance                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Create | `internal/tooling/appstate/export.go`        | Collect → `archive.Write` → `archive.Verify`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Create | `internal/tooling/appstate/export_test.go`   | Bundle contents, two profiles side by side, manifest, verify, failure cleanup                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Create | `internal/tooling/appstate/import.go`        | Check the bundle is named for this app, read bundle, validate every member, all-or-nothing backup-then-replace, refuse without `--force`                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Create | `internal/tooling/appstate/import_test.go`   | Verify runs before the first backup: an altered bundle and a bundle with no sibling manifest are both refused with nothing on disk touched; backups taken before the first write and restored when a later write fails; group selection (default restores an opt-in group the bundle carries, `--group` narrows without refusing the rest); refusal paths (unknown profile, unknown group, group absent from the bundle, no `--force`); a bundle named for another app refused before it is hashed; a crafted bundle refused: link member, `..` member, denied member |
| Modify | `pkg/files/files.go`                         | Move `BackupPath` / `RestorePath` / `DiscardBackup` here from `internal/theme/recovery.go`, with the suffix pair as a parameter                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Modify | `internal/theme/recovery.go`                 | Call the moved helpers with theme's own suffixes — one implementation, not two                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| Create | `internal/tooling/appstate/denylist.go`      | Credential / machine-bound patterns that no group may name and no bundle may restore                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Create | `internal/tooling/appstate/denylist_test.go` | Every registered adapter's allowlist checked against the denylist                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Create | `internal/apps/brave/state.go`               | `StatePorter` impl: the per-platform data dir, roots per profile, groups, `IsRunning`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| Modify | `internal/apps/brave/brave.go`               | Add the `Base cmd.BaseCommandExecutor` field `state.go` selects the platform with, set in `New()`                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Create | `internal/apps/brave/state_test.go`          | Group table, both platforms' data roots via `IsMacResult`, multi-profile discovery, running-check injection                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| Create | `cmd/export.go`                              | `dg export <app> <dest-dir>` + flags; the `<app>-state-<date>` name and the pre-write collision refusal                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| Create | `cmd/export_test.go`                         | Flag parsing, dry-run output, the bundle name at a fixed `archiveNow`, refusals including an output file already present                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Create | `cmd/import.go`                              | `dg import <app> <bundle>` + flags                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Create | `cmd/import_test.go`                         | Flag parsing, `--force` gate, refusals                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Modify | `cmd/root.go`                                | Register both commands                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Modify | `docs/spec.md`                               | `dg export` / `dg import` reference sections                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Modify | `README.md`                                  | Command list entries, next to `dg archive`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Create | `docs/apps/brave.md`                         | What moves, what does not, and why (user-facing side of ADR-0045)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |

### Step-by-Step

#### Step 1: The contract

- In `internal/apps/contract.go`, add `StateGroup` and `StatePorter`, with a doc
  comment in the house style of `ThemedConfigurer` — say _why_ it is optional and
  what a non-implementing app means.
- Expected outcome: compiles; no app implements it yet.
- Verify: `go build ./internal/apps/` and `go test ./internal/apps/...`

#### Step 2: The denylist, before any adapter exists

- `internal/tooling/appstate/denylist.go`: the patterns no group may ever name —
  `Login Data*`, `Cookies*`, `Web Data*`, `Secure Preferences`, `Local State`,
  `Affiliation Database`, `Network/`, plus a comment tying each to ADR-0045.
- `denylist_test.go`: a table test that will iterate every adapter registered in
  `internal/apps/registry` and assert none of its groups names a denied path.
- Export and import share this one list: the same patterns gate what an adapter may
  declare (here) and what a bundle may restore (Step 5).
- Written first deliberately: the guard exists before the thing it guards, so the
  Brave adapter cannot be authored against a green test that does not yet check it.
- Expected outcome: test passes trivially (no adapters yet) but fails loudly the
  moment one names a denied path.
- Verify: `go test ./internal/tooling/appstate/`

#### Step 3: Collection

- `collect.go`: given a profile selection, a group selection, and an adapter, walk
  only the allowlisted paths under each selected root and emit `[]archive.Entry` +
  `TotalBytes` as an `*archive.ScanResult`. A named path that does not exist is not
  an error — a fresh profile with no `Extension State/` is normal.
- **One bundle, profiles namespaced.** Each entry's `Path` is
  `<profile key>/<path under that profile>` — `Default/Bookmarks`,
  `Profile 1/Sessions/…` — and `archive.Write` is given the profiles' shared parent
  directory as `sourceRoot` (see §2). The prefix is not cosmetic: `Entry.Path` is
  relative to a single scan root and `write.go` uses it as the tar member name
  verbatim, so without it two profiles' `Bookmarks` collide into one member and the
  second silently wins.
- Profiles are collected in sorted key order, so the same selection always produces
  the same bundle layout.
- **Stat each allowlisted path first**, because a group's path is a single file as
  often as it is a directory — `Bookmarks` and `Preferences` are files, `Sessions/`
  and `Extension State/` are directories. For a directory, call
  `archive.Scan(subPath, ScanOptions{NoSkip: true})` and merge its entries with a
  path-prefix rewrite, rather than writing a second walker. For a regular file, emit
  one `archive.Entry` from the stat directly: `Scan` returns nothing at all for a
  file root, because its walk callback returns early on `path == root`
  (`scan.go`) — so handing it `…/Default/Bookmarks` yields an empty result and the
  two most important default-on groups would export empty. Only hand-roll the
  directory walk if the prefix rewrite turns out worse than the duplication — and
  say so in a comment if you do (CLAUDE.md §6, reuse before writing).
- Emit only `KindFile` and `KindDir`. `archive.Scan` reports symlinks and hard links
  too, and Step 5's import rejects any bundle containing one, so a link found under
  an allowlisted path is dropped here rather than packed into a bundle that cannot
  be imported.
- Expected outcome: a `ScanResult` whose entries are exactly the selected groups of
  the selected profiles, each under its profile prefix, files and directories only —
  including a group whose path is one file (`Bookmarks` is present as a single
  entry, not missing) as well as one whose path is a directory (`Sessions/`).
- Verify: `go test ./internal/tooling/appstate/ -run TestCollect`

#### Step 4: Export

- `export.go`: refuse if `IsRunning`, collect, `archive.Write`, `archive.Verify`,
  and return what was written. `--dry-run` prints, per profile, group / paths /
  size / on-off, and writes nothing.
- Profiles: the default is **every** profile `StateRoots` reports. `--profile`
  narrows it to the keys named (comma-separated, like `--group`); a key
  `StateRoots` does not report is an error that lists the ones it does, rather than
  an empty bundle.
- **The bundle's name is `<app>-state-<YYYY-MM-DD>`**, so `dg export brave /tmp/x`
  writes `brave-state-2026-09-15.tar.zst` next to its `.sha256`. Deterministic, the
  same shape as `dg archive`'s `<folder>-<date>`, and taken through the same
  `archiveNow` indirection so a test can fix the date. The `-state-` part is what
  keeps it apart from a `dg archive` of a folder that happens to be named `brave`.
  The `<app>-` part is also the bundle's app identifier: Step 5 reads it back and
  refuses a bundle written for a different app.
- **Refuse before writing if any output file is already there.** `archive.Write`
  composes its paths from the caller's `name` alone and renames its `.partial`
  files into place unconditionally (`write.go`), so a second export on the same day
  into the same directory would replace the first without a word. Call
  `cmd/archive.go`'s existing `refuseIfAnyOutputExists(destDir, name, ext)` — same
  package, and it already covers all four files a run produces, including the
  `<archive>.sha256` the verify phase adds. The refusal says to move or delete what
  is there, or to export into another directory. `--force` is the import's gate and
  does not override this: `dg export` writes onto a drive, where `dg archive`'s
  never-overwrite rule (ADR-0040) applies unchanged.
- Expected outcome: `brave-state-<date>.tar.zst` + `.sha256` that `tar --zstd -tf`
  lists and `shasum -a 256 -c` checks; a second run into the same directory refuses
  and leaves the first bundle untouched.
- Verify: `go test ./internal/tooling/appstate/ -run TestExport`

#### Step 5: Import

- `import.go`: refuse if `IsRunning`; refuse without `--force` when target state
  exists.
- **Refuse a bundle written for a different app, before it is hashed or written.** Every
  Chromium browser — Chrome, Edge, Vivaldi, Brave — uses the same profile schema,
  so `Bookmarks`, `Preferences`, `Sessions/` and `History` are the same file names
  under the same `Default` / `Profile N` directories in all of them. A Chrome
  bundle therefore passes every path rule below — allowlist, denylist, profile
  key — and would be written straight into the Brave profile. So `dg import brave`
  requires the bundle's base name to start with `brave-state-`, the name Step 4
  writes, and refuses otherwise, naming the app the bundle is for and the app on
  the command line. The check is one string comparison and runs before the verify
  and before the first backup, because there is no reason to hash a whole bundle
  that is for the wrong app.
  - The base name is already load-bearing, so this costs nothing new:
    `archive.Verify` finds the manifest by stripping the bundle's extension and
    adding `.sha256` (`verify.go`), so a renamed bundle already fails to import
    unless its manifest was renamed with it. The refusal says to rename the bundle
    back to the name the export gave it.
  - **A name is not proof.** This stops the realistic accident — two bundles on one
    drive and the wrong path typed — and nothing else. A hand-built tar named
    `brave-state-….tar.zst` still passes, and making it not pass means signing a
    bundle, which this cycle does not do. The member gate below is what carries the
    safety guarantees; this bullet only stops a wrong-app restore.
  - A header file _inside_ the tar was the other option and does not fit: `Write`
    builds every member by opening a real file at `sourceRoot` + the entry's path
    (`write.go`), so a synthetic member means changing the writer this cycle reuses
    verbatim, and it would put a devgeta-only file inside a bundle ADR-0040 wants
    extractable with plain `tar`. If a later cycle needs identity a rename cannot
    break — an app _version_, say, for a cross-version import — that is where it
    belongs.
- **Verify the bundle before anything is backed up or written.** `archive.Verify`
  re-reads every member against the bundle's sibling `.sha256` manifest, which is
  the whole reason the export writes one (ADR-0045 reuses the writer and the
  verifier verbatim). It runs before the first backup and the first write, so a
  damaged transfer refuses instead of restoring corrupted bytes over good ones. Two cases the command has to handle
  rather than hand the user a raw error:
  - **The manifest has to travel with the bundle.** Without the sibling `.sha256`,
    `Verify` cannot run at all, and the refusal says to copy it next to the
    bundle — the check is not skipped.
  - **`Verify` writes as well as reads.** On success it drops
    `<bundle>.tar.zst.sha256` beside the bundle (`verify.go`) — the archive's own
    checksum, a different file from the manifest it read, so nothing is clobbered.
    But an import commonly runs straight off a USB drive, and on a read-only or
    write-protected one that write fails even though the bundle is sound. So check
    the bundle's directory is writable before verifying, and if it is not, refuse
    with that reason — copy the bundle and its `.sha256` somewhere writable — rather
    than letting a write error read as corruption.
- Profiles: the default is **every** profile in the bundle, each restored into the
  destination root with the same key; `--profile` narrows it to the keys named. A
  bundle profile the destination does not have is a refusal, not a directory
  devgeta creates: Brave's profile registry lives in `Local State`, which is on the
  denylist, so a profile directory Brave was never told about is one it never
  shows. The refusal names the missing keys and says to create the profile in Brave
  first or to pass `--profile` for the ones that exist.
- Groups: the default is **every** group the bundle carries, including the opt-in
  ones — the export already decided what was worth carrying, so a bundle written
  with `--group history` restores its history rather than silently dropping it.
  A group's export default has no say on the way in. `--group` narrows the restore
  to the groups named. A name that is not one of the adapter's groups is an error
  listing the ones that are; a name that is a real group but is not in the bundle is
  a refusal naming it, rather than an import that quietly restores less than it was
  asked for.
- **All or nothing.** CLAUDE.md §4 requires state to be complete or fully rolled
  back, and a profile is many files: a failure on the fourth of seven writes must
  not leave a half-imported profile. So back up **every** path the import will
  write before writing **any** of them, then write; on any failure, restore every
  backup and the profile is exactly as it was. This is the same shape
  `archive.Write` already uses in the other direction — `.partial` files, renamed
  into place only once everything succeeded.
- Reuse `dg theme set`'s primitives rather than writing a second copy (§2): a
  target is renamed aside to a sibling, and a path the profile did not have gets an
  "absent" marker instead, because a rename-aside cannot record "there was nothing
  here" and the rollback of an added file has to be a delete. Move the three
  functions to `pkg/files` with the suffix pair as a parameter, so `internal/theme`
  keeps `.dg-theme-backup` / `.dg-theme-absent`, `appstate` passes
  `.dg-import-backup` / `.dg-import-absent`, and there is one implementation.
- A restore is a rename between siblings on one filesystem, so it is about as
  reliable as an undo gets — but if one still fails, name the backup paths left
  behind and where they belong instead of stopping silently.
- On success the backups stay — that is ADR-0045's recoverability promise — and the
  command prints the suffix and where they are, so the user can delete them.
- **Not in this cycle: recovering from a crash** (SIGKILL, power loss) between the
  first and last write. `dg theme set` can recover because its backup manifest is
  fixed and a `pending_theme` flag records which way to go; an import's manifest
  depends on the bundle, so recovery would need per-import state written to disk.
  That is a future cycle. What this cycle gives instead is a by-hand undo: every
  backup carries the one suffix, documented in `docs/apps/brave.md`.
- **Validate every tar member before writing anything.** The allowlist constrains
  what export packed, not what the bundle in front of us contains, so the import
  side needs its own gate. Without one, ADR-0045's two promises — never outside the
  roots, never a credential — hold for export only. A member is accepted only when
  all of these pass:
  - **It is a regular file or a directory.** Any symlink or hard-link member
    (`tar.TypeSymlink`, `tar.TypeLink`) rejects the bundle: a link written early
    redirects a later, perfectly relative member outside the root, which is exactly
    the escape "reject `..` and absolute paths" does not catch. None of the
    allowlisted Brave state is a link, so nothing legitimate is lost — and `Write`
    does emit both types (`write.go`, `KindSymlink` / `KindHardLink`), so
    `collect.go` must emit only `KindFile` / `KindDir` for devgeta's own bundles to
    pass this gate.
  - **Its path is relative and has no `..` element.**
  - **Its first element is one path element — the bundle's profile key** — and the
    rest falls under one of that profile's resolved group paths in the adapter's
    allowlist, and matches nothing on the denylist. Without this, a member named
    `Default/Login Data` is clean by every path rule and gets restored straight into
    the profile. The gate is the adapter's **whole** allowlist, not the selection:
    `--group` and `--profile` decide what gets written, so a member outside the
    selection is left alone, while a member outside the allowlist rejects the
    bundle. Otherwise narrowing a full bundle with `--group bookmarks` would refuse
    it for carrying the groups the user chose not to restore.
  - **The joined destination is still inside the root** after the parent directory's
    symlinks are resolved — the live profile may already contain one.
- A failing member rejects the whole bundle rather than being skipped: a bundle
  carrying something it should not is not one to trust the rest of.
- Expected outcome: a restore into a temp root, with backups present; a bundle
  carrying an opt-in group restores it by default, and `--group` narrowing that same
  bundle restores only what was named and refuses nothing; a write rigged to fail on
  a later file leaves every path as it was; a bundle hand-built with a link member, a
  `..` member, a `Login Data` member, or a profile the destination does not have is
  refused and writes nothing; a bundle whose bytes were altered after export, and
  one with no sibling manifest, are both refused before the first backup exists; a
  bundle named for another app is refused before it is even hashed.
- Verify: `go test ./internal/tooling/appstate/ -run TestImport && go test ./internal/theme/ ./pkg/files/`

#### Step 6: The Brave adapter

- `internal/apps/brave/state.go`: `StateRoots` discovers every `Default` /
  `Profile N` under the platform's Brave data dir, keyed by the directory name —
  those keys are what the bundle namespaces profiles by, and the shared data dir is
  the base `archive.Write` gets (§2); `StateGroups` returns
  [ADR-0045's group table](../../decisions/ADR-0045-portable-app-state-is-an-allowlist-not-a-smaller-archive.md#braves-groups)
  transcribed — `bookmarks`, `preferences`, `tabs`, `extension-settings` on;
  `extensions`, `history` off — with the table's one-line reason as each group's
  `Why`; `IsRunning` checks for a live process through an injectable indirection.
- The group table is not restated here on purpose. One authoritative copy, in the
  ADR, is what keeps the adapter and the docs from drifting apart.
- **Brave's data dir, per platform.** On macOS it is
  `paths.GetHomeDir("Library", "Application Support", "BraveSoftware", "Brave-Browser")`.
  On Debian/Ubuntu it is `paths.GetConfigDir("BraveSoftware", "Brave-Browser")` —
  `$XDG_CONFIG_HOME` or `~/.config`, which is Chromium's own rule on Linux, so
  going through `paths.GetConfigDir` gets it right for free. Build both through
  `pkg/paths` and never a literal `~`: that is the layer the `go test` sandbox
  redirects, so a test gets a throwaway root without touching the real profile
  (CLAUDE.md §4).
- **Which of the two is `Base.IsMac()`** — the repo's platform check (CLAUDE.md
  §7), not `runtime.GOOS` in the adapter. `Brave` currently holds only `Cmd`, so it
  gains a `Base cmd.BaseCommandExecutor` field in the shape `Ghostty` already has,
  set by `New()`. `state_test.go` then flips platforms with `MockBaseCommand`'s
  `IsMacResult` and asserts both roots, so neither platform's path is left untested
  on the other.
- `state_test.go` asserts the returned groups match that table exactly — names,
  paths and defaults — so a silent edit to the adapter fails rather than ships.
- Expected outcome: `denylist_test.go` now exercises a real adapter and passes.
- Verify: `go test ./internal/apps/brave/ ./internal/tooling/appstate/`

#### Step 7: The commands

- `cmd/export.go` / `cmd/import.go`, resolving the app via `registry.GetApp` and
  type-asserting to `apps.StatePorter`; if the assertion fails, say the app has no
  portable state and list the apps that do. Follow `cmd/archive.go` for progress
  bars, free-space checks and the restore hint.
- Register in `cmd/root.go`.
- Expected outcome: `dg export --help`, `dg import --help`.
- Verify: `go build ./... && ./devgeta export --help`

#### Step 8: Manual verification (before tests are called done)

- Per CLAUDE.md §6: the feature is confirmed by hand _before_ the test pass is
  considered final. Run the real round trip — see §6 Manual Verification.

#### Step 9: Docs

- `docs/spec.md` reference sections, `README.md` entries beside `dg archive`, and
  `docs/apps/brave.md` stating plainly what moves, what does not, that credentials
  are Brave Sync's job, and what the import's backups are called and where they
  are, so the user can undo one by hand.
- Verify: read them back; a stranger should be able to tell what lands on the new
  machine without opening the ADR.

---

## 6. Verification Plan

### Automated Verification

Derived with the `go list` query from CLAUDE.md §6 — the importers of
`internal/tooling/archive`, `internal/apps/registry` and `internal/apps/brave` are
`cmd`, `internal/apps/brave`, `internal/apps/registry`, `internal/tooling/archive`
and `internal/tooling/desktop` — plus the new `internal/tooling/appstate`:

```bash
go test ./internal/tooling/appstate/ ./internal/apps/brave/ ./internal/apps/registry/ \
        ./internal/tooling/archive/ ./internal/tooling/desktop/ ./cmd/ \
        ./pkg/files/ ./internal/theme/

make lint

go test ./internal/tooling/appstate/ ./internal/apps/brave/ ./cmd/ -cover
```

`internal/apps/contract.go` gains a type and an interface, which is additive — no
existing importer can break at compile time, so the list above does not widen to
every app package. `pkg/files` has 21 direct importers and `internal/theme` 10, but
Step 5 only adds functions to `pkg/files` and turns theme's copies into calls to
them: additive in one, unchanged public behavior in the other, both covered by
their own package's tests. This is not a full-suite change; `go test ./...` is the
release gate (CLAUDE.md §9), not a step here.

### Manual Verification

1. `dg export brave --dry-run` with Brave **running** → refuses, names Brave, says to quit it
2. Quit Brave, `dg export brave --dry-run` → lists each group with its paths and size; total in the tens of MB, **not** gigabytes; the six groups are the ones in ADR-0045's table; `Favicons`, `Top Sites`, `Login Data` and `Cookies` appear nowhere
3. `dg export brave /tmp/x` → writes `brave-state-<today>.tar.zst` + `.sha256`; `shasum -a 256 -c` passes; `tar --zstd -tf` lists only allowlisted paths, every one under a profile directory, so two profiles each have their own `Bookmarks` member; the single-file groups are actually in there (`Default/Bookmarks`, `Default/Preferences`), not just the directory ones. Run the same command again → refuses, names the files already in `/tmp/x`, and the first bundle is byte-identical afterwards
4. `dg export brave /tmp/x --group extensions,history` → bundle grows by the opt-in groups
5. `dg export brave /tmp/x --profile "Profile 1"` → only that profile's paths; `--profile nope` → error naming the profiles that do exist
6. `dg import brave <bundle>` into a profile that already has state → refuses without `--force`
7. `dg import brave <bundle> --force` → backups exist beside each replaced file; launch Brave → bookmarks and open tabs are there. Then: flip a byte in a copy of the bundle → refuses, and no backup file appears; delete that copy's `.sha256` → refuses and says to bring the manifest along; `chmod a-w` the directory holding a copy → refuses asking for a writable location, not "corrupt"; rename a copy and its `.sha256` to `chrome-state-<today>` → refuses naming both apps, and nothing is written
8. `dg import brave <bundle>` of the step-4 bundle → the opt-in `history` it carries is restored without asking; `--group bookmarks` on that same bundle restores only bookmarks and refuses nothing; `--group nope` → error listing the real groups; `--group history` against the step-3 bundle, which has none → refuses and names it
9. `dg import brave <bundle>` whose bundle holds a profile this machine lacks → refuses, names it, and nothing is written
10. `dg export claude` (an app with no adapter) → says so and lists the apps that do
11. Confirm no keychain prompt appears at any point in the round trip

### Regression Check

- `dg archive ~/some/dir /tmp/x` still behaves identically — skip rules, dry-run output, verify
- `go test ./internal/tooling/archive/` unchanged and green
- `dg install --help`, `dg configure --help`, `dg version` unaffected

---

## 7. Risks & Trade-offs

| Risk                                                                   | Likelihood | Mitigation                                                                                                                                                                                                                                      |
| ---------------------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| An allowlist misses a file, so a setting silently does not come across | High       | Accepted by ADR-0045; `--dry-run` shows exactly what moves, and the gap is a re-set preference, not data loss                                                                                                                                   |
| A future adapter names a credential file                               | Med        | `denylist_test.go` iterates every registered adapter, and it exists before the first adapter (Step 2)                                                                                                                                           |
| A live app corrupts SQLite/LevelDB mid-copy                            | Med        | Both directions refuse while the app is running; the check is injectable so the refusal itself is tested                                                                                                                                        |
| A crafted bundle escapes the roots, or restores a denied file          | Low        | Step 5 validates every member — links rejected, path relative and `..`-free, inside a selected group, not on the denylist — and refuses the whole bundle; tested with a hand-built bundle per case                                              |
| Another Chromium browser's bundle restored over Brave state            | Med        | Every Chromium profile uses the same file names, so a Chrome bundle passes every path rule; Step 5 refuses unless the bundle is named for the app on the command line. A name stops the wrong-file accident, not a renamed or hand-built bundle |
| `Preferences` rejected by a much newer Brave                           | Med        | Chromium ignores unknown keys; import backs up the file it replaces                                                                                                                                                                             |
| An import fails partway and leaves a half-imported profile             | Med        | Step 5 backs every path up before the first write and restores all of them on any failure; a crash mid-write is not auto-recovered, and the by-hand undo is one documented backup suffix (Step 5)                                               |
| Duplicating archive's walker in `collect.go`                           | Med        | Step 3 reuses `archive.Scan(NoSkip)` per allowlisted directory and stats a file path into one entry; hand-rolling the walk needs a comment justifying it                                                                                        |
| Brave Sync makes the Brave adapter redundant                           | —          | Named in ADR-0045 as accepted; the mechanism is justified by apps with no sync, and adapter one is the measurable one                                                                                                                           |

### Trade-offs Made

- **Allowlist vs. `dg archive`'s exclude-on-proof:** two commands with opposite
  defaults, rather than one command with two moods. Costs a mechanism; buys a
  22M move instead of 3.8G and a "no credentials" guarantee in code.
- **Open tabs on by default, history off:** carrying work in progress is the point
  of a machine move; carrying a browsing record is the user's call.
- **`Extensions/` opt-in:** 207M of payload the store re-downloads anyway; the flag
  exists for a machine that will be offline.
- **Backup-then-replace on import, not refuse-always:** `dg archive` never writes
  over anything, but an import writes into a live app directory by definition, so
  the honest equivalent is a recoverable overwrite behind `--force`.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope is actually locked?
- [ ] Steps are actionable?
- [ ] Verification is executable?
- [ ] Risks are realistic?

**Open questions for the reviewer:**

1. Is `dg export` / `dg import` the right naming, or should it be one noun-scoped
   command (`dg state export …`)? Flat verbs match `install` / `configure` /
   `archive`; a noun keeps the pair discoverable together and the root namespace
   smaller. The branch this was designed on is called `port-brave`, so `dg port`
   was also on the table and rejected as ambiguous with network ports.
2. Is `Preferences` really safe default-on, or should it be opt-in for a first
   release until we have seen a cross-version import?

Settled while reviewing: whether the bundle should record which app wrote it. It
has to, because every Chromium profile uses the same file names, so a Chrome
bundle passes all of Brave's path rules. It is now in scope and in Step 5, as a
check on the bundle's name rather than a header inside the tar.

**Reviewer notes:**

Both open questions were answered by building it:

1. **Flat verbs, `dg export` / `dg import`.** They sit beside `install`,
   `configure` and `archive`, which is the shape every other top-level command
   in this CLI already has, and the pair reads as a pair without a noun scope to
   hold them together. `dg port` stayed rejected.
2. **`Preferences` stays default-on.** Chromium ignores keys it does not know,
   so the realistic failure of a cross-version import is a few settings not
   applying — and the import backs the file up, so the user can put the old one
   back. Making it opt-in would have meant a default import that carries
   bookmarks and tabs but not the settings around them, which is the odder
   outcome.

Three defects surfaced in Step 8 that the tests had agreed with rather than
caught; each is now covered by a test of its own. See the implementation
commits: absent markers left in a fresh profile, the never-overwrite refusal
printing after the profile walk, and the verify meter drawing above refusals
that never read the bundle.

---

## Notes for Implementers

- **ADR-0045 is the spec for what moves.** This doc is the spec for how. If you
  want to change a group's default, that is an ADR amendment, not an implementation
  detail.
- **Commit after each step**, once its verify check passes.
- **Manual verification (Step 8) comes before the tests are final** — CLAUDE.md §6.
  Tests written against a broken round trip encode the wrong behavior.
- **Never touch `internal/tooling/archive/skiprules.go`.**
