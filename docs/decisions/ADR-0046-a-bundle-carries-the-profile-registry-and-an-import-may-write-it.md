# ADR-0046 — A bundle carries the profile registry, and an import may write it

**Date:** 2026-09-15
**Status:** ACCEPTED

## Context

[ADR-0045](ADR-0045-portable-app-state-is-an-allowlist-not-a-smaller-archive.md)
made an app's state portable as an allowlist of paths _inside_ a profile
directory. It said nothing about the profile directories themselves, because on
the machine it was designed against they already existed.

They do not exist on a new machine, which is the case the feature was built for.
The first real move failed:

```
$ dg import brave ./Brave/brave-state-2026-09-15.tar.zst
Verifying  2.5 MiB in 0s  (97.6 MiB/s)
Error: brave has no profile "Profile 5" on this machine
create the profile in brave first, or pass --profile with the ones that exist
```

The advice in that message cannot be followed. Chromium does not name a profile
directory after anything the user chooses; it allocates `Profile N` from a
monotonic counter it keeps in `Local State`. On the source machine:

```
profiles_created : 6
profiles_order   : ['Default', 'Profile 1', 'Profile 5']
```

Six profiles have been created over the machine's life and three survive — 2, 3
and 4 were deleted and their numbers are never reused. A fresh Brave starts at
`Default` and counts up, so the first profile a user creates on the new machine
is `Profile 1`, the second `Profile 2`. **There is no sequence of actions in
Brave's UI that produces a directory named `Profile 5`** short of creating five
profiles and deleting none. The refusal is therefore a dead end, not a
redirection.

The second half of the problem is that a profile's human-readable name is not in
the profile directory at all. It is in `Local State`, beside the counter:

```
Default   -> name='Jair - Lever'
Profile 1 -> name='Jair - Employ'
Profile 5 -> name='sadmin'
```

`Local State` is on ADR-0045's denylist, so it never moves. Even if the
directories did line up, every profile would arrive as "Person 1".

The constraint that makes this hard is the one worth keeping. The denylist is
what makes "a credential can never move" a property of the code rather than of
care, and `Local State` is on it for good reason: it is machine-bound. On Linux
and Windows it holds `os_crypt.encrypted_key`, the key the local credential
stores are encrypted against. On macOS that key lives in the Keychain and is
absent from the file — verified on the source machine, where `os_crypt` is not
a key in `Local State` at all — but the file still holds hardware identifiers,
metrics state and per-machine settings. Copying one machine's `Local State` onto
another is exactly the class of thing ADR-0045 exists to prevent.

## Decision

**The bundle carries a profile registry, and an import may write named keys into
the destination's own `Local State` — but no bundle ever contains that file.**

Three parts, and the third is the one that keeps ADR-0045 intact.

### 1. The registry rides as a sidecar beside the bundle

One new file next to the archive, named after it:

```
brave-state-2026-09-15.tar.zst
brave-state-2026-09-15.tar.zst.sha256    the checksum manifest
brave-state-2026-09-15.skipped.txt       the skip report
brave-state-2026-09-15.profiles.json     the profile registry
```

A sidecar rather than a member inside the tar, for two reasons. The export
already writes its manifest and skip report this way, so this is the existing
shape rather than a new one. And an import already refuses a bundle whose
manifest sidecar is missing, so "these files travel together" is the contract
today, not something this ADR introduces.

The alternative — injecting a synthetic member at the bundle root — would mean
teaching the shared `archive` package to write a file that is not on disk under
the source root. That package is `dg archive`'s too, and widening its writer for
one caller's metadata is the wrong direction for a self-contained bundle that is
already not self-contained.

It holds, for each profile in the bundle, only what is needed to recreate its
registration:

```json
{
  "profiles": [
    {
      "dir": "Default",
      "name": "Jair - Lever",
      "avatar": "chrome://theme/IDR_PROFILE_AVATAR_68"
    },
    {
      "dir": "Profile 1",
      "name": "Jair - Employ",
      "avatar": "chrome://theme/IDR_PROFILE_AVATAR_57"
    },
    {
      "dir": "Profile 5",
      "name": "sadmin",
      "avatar": "chrome://theme/IDR_PROFILE_AVATAR_26"
    }
  ]
}
```

Three fields. Not a copy of `info_cache`, which also carries signed-in account
ids, hosted-domain state and gaia pictures — a registry entry is rebuilt from
the named fields, never transplanted.

### 2. An import creates and registers a missing profile

When the bundle names a profile the destination lacks, the import creates the
directory and registers it, rather than refusing. Registration is a **merge**
into the destination's existing `Local State`:

- add `profile.info_cache[<dir>]` with the name and avatar from `profiles.json`,
  for profiles being created and no others;
- append `<dir>` to `profile.profiles_order`;
- raise `profile.profiles_created` past the highest imported index, so Chromium
  does not later hand out a directory name that is already on disk.

Entries already present on the destination are never modified. An import that
restores into an existing profile does not touch the registry at all.

### 3. "Never copy it" is the rule, not "never touch it"

`Local State` stays on the denylist, unchanged and in both directions: no
export may pack it, and no bundle member may restore to it. Nothing about the
guarantee moves. What this ADR adds is a different operation on a different
file — devgeta writing two named keys into _the destination's own_ registry,
from a three-field description, having copied nothing.

The distinction is the whole decision. A copied `Local State` carries the source
machine's encryption key, identifiers and settings. A merged registry entry
carries a display name and an avatar id.

The write is covered by the same safety the rest of the import has: `Local
State` is backed up to a `.dg-import-backup` sibling before the first write, so
a failure rolls the registry back with everything else, and the import still
refuses to run while Brave is open — which is what makes writing the file safe
at all.

### 4. It is an optional adapter capability

Profiles registered outside the profile directory are a Chromium shape, not a
universal one. The contract gains an optional interface beside `StatePorter`,
in the same spirit as `ThemedConfigurer`:

```go
// StateProfileRegistrar is implemented by an adapter whose profiles are
// registered somewhere outside the profile directory itself.
type StateProfileRegistrar interface {
    ReadProfileRegistry() (map[string]StateProfileInfo, error)
    EnsureProfiles(want map[string]StateProfileInfo) ([]string, error)
    RegistryPaths() []string
}
```

An adapter that does not implement it behaves exactly as today: a bundle with an
unknown profile is refused. No adapter is forced to grow a registry it does not
have.

## Consequences

- **Easier:** the case the feature exists for works end to end. A bundle from one
  machine restores onto a fresh one with its profiles present and named, without
  the user reverse-engineering Chromium's directory numbering.
- **Easier:** the fix is the same shape for any future Chromium adapter, because
  the registry is described in the adapter, not in the bundle format.
- **Harder:** devgeta now writes a file it refuses to copy. That is a genuinely
  new category and it has to be stated precisely every time, or the next reader
  concludes the denylist has an exception. It does not: the denial is on copying,
  and this ADR never copies.
- **Harder:** a fourth file must survive the copy to the new machine. Losing it
  is not silent — the import falls back to today's refusal and names the
  profiles it cannot create — but it is one more thing to carry, and the reason
  the guide says to copy the folder rather than the archive alone.
- **Accepted:** raising `profiles_created` can leave gaps in the destination's
  numbering — importing `Profile 5` onto a machine with one profile jumps the
  counter to 6. That is cosmetic, invisible in the UI, and mirrors what the
  source machine already looked like.
- **Accepted:** an avatar id from a much newer Brave may not resolve on an older
  one. Chromium falls back to a default avatar; the name, which is what
  identifies the profile to the user, still arrives.
- **Rejected:** `--profile-map <src>=<dst>`, letting the user map a bundle
  profile onto a directory that already exists. It avoids touching `Local State`,
  but it makes the user do the registry's job by hand, still loses every name,
  and requires creating the destination profiles first. Worth adding later as a
  narrowing tool; wrong as the answer to "my profiles do not exist yet".
- **Rejected:** copying `Local State` and stripping the sensitive keys. A
  denylist of keys inside a file devgeta does not own is the fragile direction —
  it fails open the day Chromium adds a key. Naming the three fields we rebuild
  from fails closed.
