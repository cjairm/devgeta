# ADR-0030 — `global_config.yaml` is loaded once per run and served from a cache

**Date:** 2026-08-24
**Status:** ACCEPTED

Part of the cycle
[2026-08-24 — performance and resource hardening](../plans/cycles/2026-08-24-performance-and-resource-hardening.md).

## Context

`internal/config/fromFile.go:306-312` — `GlobalConfig.Load()` is an `os.ReadFile`
followed by a `yaml.Unmarshal`, with **no cache anywhere in the config package**.
Every caller that wants to know anything about installed state re-reads and
re-parses the whole document.

There are **95 non-test `.Load()` call sites across 58 files**. Examples:
`internal/apps/mise/mise.go:54,74,85`,
`internal/apps/claude/claude.go:86,113,208,277`,
`internal/apps/alacritty/alacritty.go:54,92,107`,
`internal/apps/lazygit/lazygit.go:124,153,173`,
`internal/apps/neovim/deps.go:89,123,166`,
`internal/tooling/worktree/worktree.go:220,248,802,1769`. A single `dg install`
or `dg ws` parses the same YAML dozens of times.

The install path multiplies it. `internal/commands/base.go:477` loads the config
on every `MaybeInstall` and saves at `:509` (already-installed) or `:521`
(installed). Languages and databases then do their **own** extra `Load` + `Save`
round trip per item on top of that —
`internal/tooling/languages/languages.go:201,205` and
`internal/tooling/databases/databases.go:165,169`. With ~55 items that is roughly
**55 full-document reads plus up to 55 marshal-and-write cycles** in one run.

**The correct implementation already exists in this repo.** The worktree manager
memoizes one `Load()` keyed on the config file's `{path, modTime, size}`
(`internal/tooling/worktree/worktree.go:135-155`, hit and invalidated at
`:796-817`). That key is deliberately chosen: `path` is part of it because
`paths.Paths.Config.Root` gets repointed per test case, so an entry cached under
one root can never be served under another. A naive process-global singleton
would not survive that.

CLAUDE.md §6 requires building on an existing in-tree mechanism rather than
inventing a parallel one that can drift from it. So the question this ADR
answers is not "should we build a config cache" — one exists and works — but
"where does it belong".

## Decision

`global_config.yaml` is read and unmarshalled **once per run** and served from a
cache that lives in `internal/config`, alongside `Load()` itself. The
`{path, modTime, size}` memoization proven in the worktree manager is promoted
from that one consumer to the package every consumer already calls. This is a
generalization of working code, not new machinery: the worktree manager's local
cache is removed in favour of the package-level one. Two things change in
promotion — what a cache hit hands back, and what a write does to the entry — for
the reasons set out below.

### The write contract is the load-bearing part, and it is a refresh

`internal/config/lock.go`'s `Update(fn)` is the only mutation path that takes the
sidecar lock. The ~95 bare `Load()` / `Save()` sites bypass it entirely. So a
stat-based key alone is not sufficient: a write through a bare `Save()` can land
within the same filesystem timestamp granularity as the cached read, and every
subsequent reader in that run is served stale data — including
`base.go:477`/`:509`/`:521`, which read and write in a loop.

The obvious answer — drop the cached entry on `Save()` — is wrong, and the same
loop is why. `MaybeInstall` does one `Load()` per item and a `Save()` whenever the
item was not already tracked, with languages and databases adding a second
`Load`+`Save` per item. A drop-on-write cache is therefore cleared by every item
on a first run and misses on all ~55 of them, and hits only on a re-run, where the
early returns skip `Save()` and there is least to gain. "Once per run" would not
be delivered on the run that needs it.

So the cache is maintained by **two** signals:

1. **Stat change** — `{path, modTime, size}` differs from the cached entry's key.
   This covers an external edit and covers tests repointing
   `paths.Paths.Config.Root`.
2. **A write through the config package refreshes the entry** — `Save()` stores a
   deep copy of the document it just wrote, keyed on the file's post-write stat.
   The sequence is cache-mutex → write → stat → store → release, so the stored
   document and its key can never disagree and a concurrent reader sees either the
   pre-write entry or the post-write one. A failed write leaves the entry alone; a
   failed post-write stat drops it rather than storing one keyed on a guess.
   `Reset()` and `Create()` write the file without going through `Save()`, so they
   take the same path.

Rule 2 is a standing constraint on future code, not just on this change: any new
write path added to `internal/config` must refresh the cache with what it wrote,
and a write that bypasses the package's own save is outside the contract. That is
the same requirement the mutation seam in
[ADR-0029](ADR-0029-installed-state-is-one-cached-listing.md) carries, for the
same reason.

Refreshing from memory rather than re-reading the file is deliberate: re-parsing
the bytes just written would be exactly faithful but pays the ~203 µs parse on
every `Save()`, halving the parses instead of removing them. The price is a
measured, bounded divergence from what a fresh read would return — a nil slice
comes back as a non-nil empty slice, and `time.Time` fields lose their monotonic
reading (`Equal()` true, `==` and `reflect.DeepEqual` false). Neither is
observable through `GlobalConfig`'s accessors, so it changes no caller's
behavior; it only means tests must compare through the accessors rather than with
`reflect.DeepEqual` against a re-read.

### Callers get a deep copy, not the cached document

The other half of the contract is what a hit hands back. The answer is an
**independent deep copy of the cached `GlobalConfig`** — never the cached
pointer, and never a shallow struct copy of it.

The worktree manager can return its cached pointer directly
(`internal/tooling/worktree/worktree.go:135-155`) because its one consumer only
reads. That does not survive promotion to the package: `Load()` fills a
caller-owned struct today, so all 95 sites assume they own the result, and
several mutate it **in place**.

- `RemoveFromInstalled` (`internal/config/fromFile.go:425`) filters through the
  shared backing array — `result := (*slice)[:0]`.
- `AddToFailed` (`:447`) writes into an existing element.
- `Shortcuts` is a map, which even a shallow struct copy shares outright.

Reproduced against a cache handing out shallow copies: with
`[git tmux neovim fzf bat]` cached, one `RemoveFromInstalled("tmux", …)` left the
**cache** holding `[git neovim fzf bat bat]` — one entry lost, another duplicated
— and the next `Load()` served that corrupted list although the file on disk was
untouched. 24 non-test `Uninstall()` sites call that method. The corruption lands
in the cache at the instant of mutation, ahead of any `Save()`, so a shallow
cache would be correct only under an unwritten "every mutation is followed by a
successful `Save()`" invariant that nothing enforces and a failed `Save()`
breaks. A package-level cache is also shared across goroutines by construction —
the worktree manager needs `configMu` today precisely because its readers overlap
— and handing out shared memory that 24 sites mutate in place turns staleness
into a data race.

Two cheaper-looking answers were measured and rejected. **Caching the raw bytes**
and unmarshalling per call is safe but nearly pointless: on the real 4,313-byte
`global_config.yaml` (best-of-5, 200 iterations) the read is ~17 µs and the parse
~203 µs, so it removes ~8% of `Load()`'s cost and leaves the 92% this ADR is
about. **Re-marshalling the cached struct** to clone it pays that parse cost back
in full.

The clone is therefore explicit code in `internal/config`, and it is guarded
rather than remembered: a test populates every field, clones, mutates every
reachable slice element, map entry and pointee in the source by reflection, and
asserts the clone is unchanged — so adding a field to `GlobalConfig` without
extending the clone fails the build. CLAUDE.md §4 prefers that to a convention
contributors must keep in mind.

For the avoidance of a test that proves nothing: appending to a decoded slice
does **not** alias today, because `yaml.v3` allocates every sequence with
`cap == len` (`decode.go:735`), so `AddToInstalled` reallocates. That is a
property of the parser, not of this code; the deep copy neither relies on it nor
needs it to change.

Load once into a package var, serve it forever. This breaks test isolation,
which CLAUDE.md §4 makes non-negotiable: `pkg/paths` redirects HOME and every
XDG root into a throwaway sandbox under `go test`, and tests repoint
`paths.Paths.Config.Root` per case. A singleton with no path in its key serves
one test's config to the next. It also has no answer for a `Save()` inside the
same run. The existing worktree implementation already rejected this by
including `path` in the key, and its comment says exactly why — that reasoning
carries over unchanged.

### Considered and rejected — passing a loaded config through every call signature

Load once at the command entry point and thread it through. This is the
statically-correct answer and it is rejected on cost and on clarity: 95 call
sites across 58 files, a signature change on most of the app modules, and a very
large diff for a modest measured win. It also makes staleness **implicit** — the
question "is this config still current?" stops being asked anywhere and becomes
a property of how far the value has travelled down the call stack. The cache
makes that question explicit and answers it in one place.

### Considered and rejected — doing nothing

Be honest about the size of this: `BenchmarkLoad`
(`internal/config/fromFile_bench_test.go`), run against a representative
4,120-byte `global_config.yaml` fixture (same shape and neighbourhood as the
4,313-byte file the read/parse split above was measured against — every
section `GlobalConfig` has, populated with plausible content), puts `Load()`
at **~224 µs per call** (best of 5 runs at `go test -bench BenchmarkLoad
-benchmem -benchtime=3s`; 95,130 B/op, 1,822 allocs/op). That is consistent
with the ~17 µs read + ~203 µs parse split above — 220 µs combined, within
measurement noise of the 224 µs measured directly on the combined call — so
the indicative figures were in the right ballpark. Multiplied by the 95
non-test call sites this ADR's Context section counts, that is
**~224 µs × 95 ≈ 21 ms total** across a whole `dg install` or `dg ws` run.

Next to [ADR-0029](ADR-0029-installed-state-is-one-cached-listing.md)'s
measured ~54 s this is not the headline, and 21 ms genuinely is negligible on
the scale of a run that takes seconds to minutes — there is no case for
inflating it to make this ADR's argument look more urgent than the data
supports. This decision is therefore proposed almost entirely for
**correctness**, not speed: 95 independent reads of a file that is also being
written during the same run means different parts of one `dg install` can
observe different versions of the document, and nothing today makes that
visible or reasons about it. One consistent view per run is a property worth
having on its own, independent of the small amount of time it happens to
save.

## Consequences

**Easier.** One `dg install` or `dg ws` parses the YAML once instead of dozens of
times, and every consumer in that run sees the same document. The worktree
manager sheds its local cache and its `configMu` bookkeeping. Future config
readers get the benefit by default rather than by remembering to memoize.

**Harder.** Staleness becomes a property of `internal/config` that every future
contributor to that package must uphold. A new write path that forgets to refresh
the entry produces a bug whose symptom (a reader seeing pre-write state) is far
from its cause. The mitigation is that all writes go through one package, and the
refresh belongs on the write itself. `GlobalConfig` also acquires a clone that has
to cover every field it ever grows; that cost is real, and it is paid down by the
reflection test above failing the build when a field is missed rather than by
anyone remembering.

**Not free, and not claimed to be.** A hit still deep-copies the document, so
reads become an allocation instead of a parse. That is much cheaper than the
~203 µs parse it replaces, but it is not zero, and the copy grows with the
config. Callers that only read still pay it, because the alternative — an
"only if you promise not to mutate" variant of `Load()` — is exactly the
convention this ADR refuses to rely on.

**Trade-off accepted.** The cache cannot protect readers from a write that
bypasses `internal/config` entirely and lands inside the stat's timestamp
granularity. Within one run that is unreachable, because devgeta's own writes go
through the package; across processes the stat key catches it. That residual
window is accepted rather than papered over with a time-based expiry.

**Not decided here.** The duplicate `Load` + `Save` round trips at
`internal/commands/base.go:477,509,521`,
`internal/tooling/languages/languages.go:201,205`, and
`internal/tooling/databases/databases.go:165,169` are a separate defect — the
cache removes the redundant _reads_, but the O(n) full-document _rewrites_
remain and want their own fix. Also not decided: whether the ~95 bare `Save()`
sites should be migrated onto `internal/config/lock.go`'s `Update(fn)` so every
mutation takes the sidecar lock. That is the deeper correctness question this
ADR only bounds.
