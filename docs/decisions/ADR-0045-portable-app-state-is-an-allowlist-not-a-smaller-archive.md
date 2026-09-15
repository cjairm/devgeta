# ADR-0045 — Portable app state is an allowlist, not a smaller archive

**Date:** 2026-09-15
**Status:** ACCEPTED

## Context

Changing machines is not a one-time event, and devgeta's whole job is standing a
new machine up. Installing the apps is solved (`dg install`) and configuring them
from devgeta's own templates is solved (`dg configure`). What is not solved is the
state the user themselves accumulated inside those apps: browser bookmarks, editor
state, shell history. Today that is hand-copied, once per machine, from memory.

`dg archive` (ADR-0040, ADR-0041, ADR-0042) already packs a named folder onto a
drive for exactly this move. So the question is narrow: **can `dg archive` carry an
app's state, or does app state need a mechanism whose default is the opposite of
`dg archive`'s?**

Brave's profile on the maintainer's machine is the measurable case:

| Path (under the profile)                        | Size | What it is                          |
| ----------------------------------------------- | ---- | ----------------------------------- |
| `Bookmarks`                                     | 8K   | JSON, stable across versions        |
| `Preferences`                                   | 320K | settings, incl. the extension list  |
| `Top Sites`                                     | 20K  | new-tab tiles                       |
| `Sessions/`                                     | 11M  | the open tabs                       |
| `Extension State/`, `Local Extension Settings/` | 12M  | per-extension data                  |
| `Extensions/`                                   | 207M | the extension payloads              |
| `Favicons`                                      | 59M  | rebuilt on its own from browsing    |
| `History`                                       | 95M  | the browsing record                 |
| `Login Data`, `Cookies`, `Web Data`             | —    | credentials, sessions, autofill     |
| whole `BraveSoftware/` tree                     | 3.8G | the above plus caches and GPU blobs |

The state worth moving is about **22M inside a 3.8G tree**.

ADR-0041 governs what `dg archive` skips, and its rule is _skip only on proof;
when in doubt, keep_ — correct for a documents folder, where a wrongly skipped file
is unrecoverable data loss. An app's data directory inverts that premise. It is
mostly machine-local derived state, and some of it is not merely wasteful to copy
but **actively wrong on the destination**:

- `Secure Preferences` carries HMACs bound to the machine that wrote it.
- `Login Data` and `Cookies` are encrypted against the local macOS Keychain item
  ("Brave Safe Storage"), so they arrive undecryptable.
- `Local State` holds that machine's encryption key.
- `GPUCache/`, `ShaderCache/`, `GraphiteDawnCache/`, `Crashpad/` describe hardware
  that no longer exists once the user has moved.

Three options were considered:

1. **Use `dg archive` unchanged** on the app's folder. It works today, with no new
   code. It moves 3.8G to deliver 22M of value, and it faithfully reinstates
   machine-bound files the new machine cannot use.
2. **Teach `dg archive` app-aware skip rules.** This puts "when in doubt, keep" and
   "when in doubt, drop" inside one command as competing defaults, and it does so
   in the exact command ADR-0041 governs. One command cannot have both defaults.
3. **A separate mechanism whose default is the inverse:** name what moves.

## Decision

**An allowlist.** A new capability, `dg export <app>` / `dg import <app>`, moves
only paths an app's adapter names. Anything not named does not move. `dg archive`
is not modified and ADR-0041 is not weakened — the two commands answer different
questions and are allowed to disagree.

Codified:

- **The unit is a named state group, not a path list in prose.** A group is
  (name, paths, default on/off, one-line reason). `dg export brave --dry-run`
  prints every group with its paths, its size, and whether it is on, so what moves
  is checkable before anything is written — the same contract as `dg archive
--dry-run` under ADR-0041.
- **Credential and machine-bound files can never be in a group.** The allowlist is
  the actual guarantee; a denylist checked against every adapter's allowlist by a
  test is what stops a future edit from quietly widening it. Both exist, because a
  "no sensitive data" promise that depends on a reviewer noticing is not a promise
  (CLAUDE.md §4 — make the class of mistake structurally impossible). **The same
  check runs on the way in.** An allowlist constrains what devgeta packed, not what
  an arbitrary tar contains, so an import validates every member of a bundle against
  the selected groups' paths and the denylist before writing anything — otherwise a
  clean relative member named `Login Data` lands in the profile and the guarantee
  only ever held for export.
- **Default-on requires version-stable and machine-independent.** `Bookmarks`
  (plain JSON) qualifies. `Secure Preferences` never qualifies, at any opt-in
  level — its HMACs are meaningless off the machine that wrote them.
- **Privacy-sensitive-but-legitimate groups are opt-in, off by default.** `History`
  is the user's browsing record; moving it is a decision they make, not a default
  they discover. Open tabs (`Sessions/`) are **on** by default, because carrying
  the work in progress is the point of a machine move and a tab list is state the
  user is actively looking at, not a record of where they have been.
- **Regenerable groups are omitted entirely** — not opt-in, not skipped-with-a-note.
  `Favicons` is 59M that rebuilds itself. There is no flag for it because there is
  no reason to want it.
- **`archive.Scan` is not used; `archive.Write`, `archive.Verify`,
  `archive.WriteManifest` and the progress meter are reused verbatim.** Skip rules
  live in `Scan`, which is the part that must not apply here; the writer, the
  sha256 manifest and the verifier are policy-free. So a bundle is still one tar
  with a `shasum -a 256 -c`-able manifest, extractable with plain `tar` and no
  devgeta (ADR-0040), and there is exactly one packer in the codebase.
- **The adapter is an optional interface on the existing app contract**, alongside
  `SelectiveConfigurer`, `ThemedConfigurer` and `LiveThemeApplier` in
  `internal/apps/contract.go`. An app without one is simply not exportable, and
  `dg export <app>` says so and names the apps that are.
- **Both directions refuse while the app is running.** `Sessions/`, `History` and
  `Local Extension Settings/` are SQLite and LevelDB; copied out from under a live
  process they are corrupt on arrival. The refusal names the app and says to quit
  it (ADR-0042's refuse-on-proof, not warn-on-guess: a running process is proof).
- **Import never clobbers, and never half-finishes.** Each file it would replace is
  backed up beside itself first, and without `--force` an import into a profile that
  already has state refuses. Every backup is taken before the first file is written,
  so a failure partway restores all of them and the profile is exactly as it was —
  CLAUDE.md §4 requires complete or fully rolled back, and a profile is many files. `dg archive` never writes over anything on the destination drive; an
  import writes into a live app directory, so the equivalent promise has to be
  "backed up, then replaced" rather than "never touched".

### Brave's groups

The rules above applied to the measured profile. This table is the whole first
adapter — six groups, each with the four fields a group is made of. Paths are
relative to a profile directory (`Default`, `Profile 1`, …). Nothing outside this
table moves, and adding, renaming or re-defaulting a row is an amendment to this
ADR, not an implementation choice.

| Group                | Paths                                           | Default | Why                                                                                                                     |
| -------------------- | ----------------------------------------------- | ------- | ----------------------------------------------------------------------------------------------------------------------- |
| `bookmarks`          | `Bookmarks`                                     | on      | Plain JSON, stable across versions. The thing a user most expects to survive a machine move.                            |
| `preferences`        | `Preferences`                                   | on      | Settings, including the extension list. Chromium ignores keys it does not know, so a newer Brave drops a few.           |
| `tabs`               | `Sessions/`                                     | on      | The work in progress, which is the point of a machine move. State the user is looking at now, not a record of the past. |
| `extension-settings` | `Extension State/`, `Local Extension Settings/` | on      | 12M of per-extension data. Without it the extensions arrive on the new machine reset to defaults.                       |
| `extensions`         | `Extensions/`                                   | off     | 207M of payload the store re-downloads on first launch. Turned on for a machine that will be offline.                   |
| `history`            | `History`                                       | off     | 95M, and the user's browsing record. Moving it is a decision they make, not a default they discover.                    |

Two measured paths are deliberately **not** groups, so they have no flag:
`Favicons` (59M) and `Top Sites` (20K) are both derived from browsing and rebuild
themselves on the new machine — the regenerable rule above.

`Login Data`, `Cookies`, `Web Data`, `Secure Preferences` and `Local State` are not
omissions but denials: they are on the denylist, so no group may ever name them and
no bundle may restore them.

## Consequences

- **Easier:** the move is ~22M instead of 3.8G. "No sensitive info" becomes a
  property of the code rather than of the user picking the right flags. The same
  mechanism serves any app with a state directory, so the second and third adapters
  are a data table, not a design.
- **Harder:** every app needs a hand-written, researched adapter. Deciding which of
  Chromium's ~200 profile files matter is not derivable from the filesystem — it is
  reading and testing. We will be wrong about some file, and someone will report a
  setting that did not come across. Accepted: the failure mode of an allowlist is a
  missing preference the user re-sets in a second, and the failure mode of the
  alternative is moving credential stores between machines.
- **Accepted — Brave Sync already covers Brave-to-Brave.** Brave's own sync chain
  moves bookmarks, settings, extensions, history and open tabs, end-to-end
  encrypted, with no drive and no files. Brave is therefore the weakest possible
  case for this feature on its own, and it is the first adapter only because it is
  the one we can measure. The mechanism earns its place on the apps with no sync at
  all (shell history, editor state, `known_hosts`, tmux), on machines where the
  user will not sign in, and when the user wants one auditable file they can keep.
  If adapter two and three never materialize, this decision was wrong and `dg
archive` on the profile folder was the right answer all along.
- **Accepted:** a default-on `Preferences` can be partly rejected by a much newer
  Brave than the one that wrote it. Chromium ignores keys it does not know, so the
  realistic failure is a few settings not applying — and the import's backup means
  the user can put the old file back.
- **Accepted:** `Extensions/` (207M) is opt-in rather than default-on, so the
  default import leaves a user with their extension _settings_ and the extension
  list in `Preferences`, but not the payloads. Brave re-downloads them from the
  store on first launch, which is the better path anyway; `--group extensions`
  exists for a machine that will be offline.
