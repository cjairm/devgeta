# Brave

Devgeta installs Brave as a desktop app (`dg install`, or `dg install --only desktop`)
and can move the state you built up inside it to a new machine with
`dg export brave` / `dg import brave`.

Brave is the first app with a state adapter. Everything below is Brave-specific;
the mechanism itself is general, and any app can adopt it by implementing one
interface — see [docs/spec.md](../spec.md#dg-export--dg-import).

---

## Moving Brave to a new machine

```bash
# On the old machine — quit Brave first.
dg export brave --dry-run              # see exactly what would move
dg export brave /Volumes/SSD           # writes brave-state-<date>.tar.zst + .sha256

# On the new machine — quit Brave first, and bring BOTH files across.
dg import brave /Volumes/SSD/brave-state-2026-09-15.tar.zst
```

Quit Brave on both machines. The commands refuse while it is running, and they
are right to: `Sessions/`, `History` and `Local Extension Settings/` are SQLite
and LevelDB databases, and a copy taken from under a live process arrives
corrupt.

Copy the `.sha256` file along with the bundle. Without it the bundle cannot be
checked against what was exported, and the import refuses rather than skipping
the check.

---

## What moves

Six named groups. `dg export brave --dry-run` prints this same list with the
real sizes on your machine, so you never have to take this table's word for it.

| Group                | What it is                                              | Default |
| -------------------- | ------------------------------------------------------- | ------- |
| `bookmarks`          | Your bookmarks                                          | **on**  |
| `preferences`        | Settings, including which extensions you have installed | **on**  |
| `tabs`               | Your open tabs                                          | **on**  |
| `extension-settings` | Each extension's own data, incl. password-manager state | off     |
| `extensions`         | The extension payloads themselves (~200M)               | off     |
| `history`            | Your browsing history (~95M)                            | off     |

### Profiles

An export writes a fourth file beside the bundle,
`<name>.profiles.json`, holding each profile's directory, display name and
avatar. **Copy the whole folder to the new machine, not just the `.tar.zst`** —
the checksum manifest is already required, and this file is what lets your
profiles arrive under their own names.

With it, `dg import` creates the profiles the new machine does not have yet and
registers them, so Brave opens showing "Jair - Employ" rather than "Person 2".
This matters because Chromium names profile directories from a counter it keeps
itself: if your old machine had `Profile 5`, there is no way to produce that
directory on a fresh install by hand. A profile that already exists is never
renamed — an import restores state into it and leaves its name alone.

Without the sidecar the import falls back to refusing, and tells you which
profiles the bundle holds, which this machine has, and the `--profile` line that
restores the overlap.

Narrow a run with `--group`:

```bash
dg export brave /Volumes/SSD --group extension-settings   # take extension data too
dg import brave BUNDLE --group bookmarks                  # restore only bookmarks
```

**`extension-settings` is off** because that directory is where extensions keep
their own private data — a password manager's session state lives there. The
denylist can police Chromium's own credential stores by name, but
`Local Extension Settings/<id>/` is an opaque database whose contents the
extension decides, so no rule can look inside it. Leaving it off means your
extensions arrive reset to defaults and you sign in once, which is what a
password manager expects on a new device. Turn it on with
`--group extension-settings` if you want that data to move.

**`extensions` is off** because Brave re-downloads extension payloads from the
store the first time you launch it, which is the better path anyway — the
extension list still comes across in `preferences`. Turn it on for a machine
that will be offline.

**`history` is off** because moving your browsing record is a decision you make,
not a default you discover. Open tabs are **on** by the opposite reasoning: they
are the work you are in the middle of, which is the point of a machine move, not
a record of where you have been.

On the way in, the default is **every group the bundle carries** — including the
opt-in ones. The export already decided what was worth carrying, so a bundle
written with `--group history` restores its history rather than silently dropping
it.

### Every profile, side by side

If you have several profiles (`Default`, `Profile 1`, …), all of them are
exported by default and each keeps its own copy of everything inside the bundle.
`--profile "Profile 1"` narrows either direction to the ones you name.

A profile that exists in the bundle but not on the new machine is refused, not
created. Brave's list of profiles lives in a file devgeta will not touch
(`Local State` — see below), so a profile directory Brave was never told about is
one it never shows you. **Create the profile in Brave first**, then import.

---

## What does not move, and why

### Never, at any flag

Passwords, cookies, autofill and payment data are not omissions you can turn on.
They are on a denylist: no group may name them, and no bundle may restore them.

| Not moved              | Why                                                         |
| ---------------------- | ----------------------------------------------------------- |
| `Login Data`           | Your saved passwords, encrypted against this Mac's keychain |
| `Cookies`              | Your logged-in sessions, encrypted the same way             |
| `Web Data`             | Autofill and payment data                                   |
| `Affiliation Database` | Password-manager data                                       |
| `Secure Preferences`   | Carries signatures bound to the machine that wrote it       |
| `Local State`          | Holds this machine's encryption key                         |
| `Network/`             | The network stack's own cookie and credential stores        |

The encrypted ones would not even work on the new machine — they are locked to
the keychain of the machine that wrote them, so they arrive as unreadable bytes.
**Passwords and logged-in sessions are Brave Sync's job**, which is end-to-end
encrypted and designed for exactly this.

### Left behind because it rebuilds itself

`Favicons` (~59M) and `Top Sites` (~20K) have no flag at all. Both are derived
from browsing and Brave regenerates them on the new machine, so there is no
reason to want them.

### Caches

`GPUCache/`, `ShaderCache/`, `Crashpad/` and the rest describe hardware that no
longer exists once you have moved. They are simply not named, so they do not
move.

---

## The undo

An import into a profile that already has state refuses unless you pass
`--force`. With `--force`, **nothing is deleted**: each path is renamed aside
first, and the command prints where the copies are.

```
Default/Bookmarks.dg-import-backup
Default/Sessions.dg-import-backup
```

To put one back by hand, quit Brave and rename it:

```bash
cd ~/Library/Application\ Support/BraveSoftware/Brave-Browser/Default
rm -rf Bookmarks && mv Bookmarks.dg-import-backup Bookmarks
```

Once Brave looks right, delete them:

```bash
find ~/Library/Application\ Support/BraveSoftware/Brave-Browser \
  -name '*.dg-import-backup' -maxdepth 2
```

Every backup carries that one suffix, so one `find` shows all of them.

The backups are taken for **every** path before **any** of them is written, so an
import that fails partway puts all of them back on its own and leaves the profile
exactly as it was. The one case that is not automatic is a crash or a power loss
in the middle of a write — there, the backups above are the undo.

A path that was not in the profile before an import (the usual case on a new
machine) has no backup, because there is nothing to put back. Deleting what was
imported is the undo there.

---

## Where Brave's data lives

- **macOS:** `~/Library/Application Support/BraveSoftware/Brave-Browser`
- **Debian/Ubuntu:** `$XDG_CONFIG_HOME/BraveSoftware/Brave-Browser`, i.e.
  `~/.config/BraveSoftware/Brave-Browser` by default

Profiles are the `Default` and `Profile N` directories inside it.

---

## Is this better than Brave Sync?

For Brave alone, usually not — and the design says so. Brave's own sync chain
moves bookmarks, settings, extensions, history and open tabs, end-to-end
encrypted, with no drive and no files, and it moves the passwords these commands
deliberately will not.

`dg export brave` is for when you want one auditable file you keep, when you will
not sign in on the new machine, or when the machine will be offline. The
mechanism itself earns its place on apps with no sync at all.

---

## Design notes

Why an allowlist rather than a smaller `dg archive`, what makes a group
default-on, and why credentials are denied rather than opt-in:
[ADR-0045](../decisions/ADR-0045-portable-app-state-is-an-allowlist-not-a-smaller-archive.md).
The group table in that ADR is the authoritative one — this page and the adapter
both follow it, and the adapter's test asserts it, so a change in one without the
others fails the build.
