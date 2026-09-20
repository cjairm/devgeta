# Cycle: an import creates the profiles a bundle names

**Date:** 2026-09-15
**Estimated Duration:** ~4 hours
**Status:** Done

---

## 1. Domain Context

`dg export` / `dg import` shipped in v1.31.0 to carry an app's own state to a new
machine. The first real move failed on the destination:

```
Error: brave has no profile "Profile 5" on this machine
create the profile in brave first, or pass --profile with the ones that exist
```

The feature works when the profiles already line up. On a new machine they never
do, which is the case it was built for.

## 2. Engineer Context

Two facts from the source machine decide the design, both verified:

- Chromium allocates profile directories as `Profile N` from a monotonic counter
  in `Local State` (`profiles_created: 6` with only `Default`, `Profile 1`,
  `Profile 5` alive). Numbers of deleted profiles are never reused, so **no
  sequence of UI actions produces `Profile 5` on a fresh machine.** The refusal's
  advice is a dead end.
- A profile's display name is not in the profile directory. It is in
  `Local State` under `profile.info_cache[<dir>].name`. That file is on
  ADR-0045's denylist, so names never travel.

[ADR-0046](../../decisions/ADR-0046-a-bundle-carries-the-profile-registry-and-an-import-may-write-it.md)
records the decision and the rejected alternatives.

## 3. Objective

A bundle restores onto a machine that has none of its profiles, with each profile
present and under its real name, without weakening ADR-0045's denylist.

## 4. Scope Boundary

### In Scope

- A `<name>.profiles.json` sidecar beside the bundle: `dir`, `name`, `avatar` per profile.
- An optional `StateProfileRegistrar` adapter capability; Brave implements it.
- Export writes the registry; import creates missing directories and merges
  `info_cache` / `profiles_order` / `profiles_created` into the destination's own
  `Local State`.
- `Local State` backed up and rolled back like any other restored path.

### Explicitly Out of Scope

- `--profile-map` (ADR-0046 rejects it as the answer here; possible later).
- Copying `Local State`, or any key of it, out of the source. The denylist is
  unchanged in both directions.
- Merging state into a profile that already exists — unchanged behaviour.
- Adapters other than Brave.

## 5. Implementation Plan

### File Changes

| File                                    | Change                                             |
| --------------------------------------- | -------------------------------------------------- |
| `internal/apps/contract.go`             | add `StateProfileInfo`, `StateProfileRegistrar`    |
| `internal/apps/brave/state.go`          | implement registry read + ensure via `Local State` |
| `internal/tooling/appstate/registry.go` | new: sidecar path, encode/decode                   |
| `internal/tooling/appstate/export.go`   | collect the registry, write the sidecar            |
| `internal/tooling/appstate/import.go`   | read the sidecar; create+register missing profiles |
| `docs/apps/brave.md`, `docs/spec.md`    | document what now moves                            |

### Step-by-Step

1. Contract: `StateProfileInfo{Dir,Name,Avatar}` and the optional interface.
2. Registry codec in `appstate`, with the member name as one exported constant
   both directions read.
3. Brave: read `info_cache` into the registry; `EnsureProfiles` merges, never
   replaces, and raises the counter past the highest imported index.
4. Export: collect the registry for the selected profiles, write it beside the
   bundle.
5. Import: read the sidecar, and when a profile is missing, create and register
   it instead of refusing. Back up `Local State` first.
6. Refusal message: when the adapter has no registrar, list bundle profiles vs
   existing and print the runnable `--profile` line.

## 6. Verification Plan

### Automated

- Registry codec round-trips, including a name with a newline and a `"`.
- `EnsureProfiles` merges: an existing entry is untouched, a new one is added,
  the counter only ever rises.
- Import into a sandbox with no profiles creates and registers them.
- Rollback: a write failure after registration restores the original `Local
State` byte for byte.
- The denylist test still passes: no group names `Local State`, and a bundle
  member named `Local State` is still refused.

### Manual

- Export on the source, import on the new Mac, open Brave, confirm three
  profiles by name.

## 7. Risks & Trade-offs

- **Writing a file we refuse to copy.** Mitigated by naming three fields we
  rebuild from, rather than filtering keys out of a copy — fails closed when
  Chromium adds a key.
- **A corrupt or hand-edited destination `Local State`.** The merge parses it
  first; a parse failure refuses the import rather than replacing the file.
- **Counter gaps** on the destination. Cosmetic; accepted in ADR-0046.
