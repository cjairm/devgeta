# ADR-0029 — Installed state is one cached listing per run, not a probe per package

**Date:** 2026-08-24
**Status:** ACCEPTED

Part of the cycle
[2026-08-24 — performance and resource hardening](../plans/cycles/2026-08-24-performance-and-resource-hardening.md).

## Context

Every idempotency check in `dg install` shells out a **complete** package-manager
listing to answer one yes/no question about one package.

`internal/commands/macos.go:179` runs `brew list` — the entire Cellar — each time
a single formula is checked. `internal/commands/macos.go:185` runs
`brew list --cask` the same way. `internal/commands/debian.go:219` does it with
`dpkg -l`. All three hand the `*exec.Cmd` to `internal/commands/base.go:191`
(`IsPackagePresent`), which buffers the whole listing, scans it linearly for one
name, and throws it away. The next package re-runs the identical command. The
call site is `internal/commands/base.go:500`, `checkInstalled(pkgToInstall)`
inside `MaybeInstall`.

A full macOS `dg install` executes roughly **55** of those probes (9 terminal
apps + 14 devtools + 16 core libs + 6 desktop apps + fonts + languages +
databases; 96 `MaybeInstall*` references exist in `internal/`).

Measured on this machine (macOS, 242 formulae / 15 casks), read-only probes only:

| Command                               | Runs (s)           | Mean     |
| ------------------------------------- | ------------------ | -------- |
| `brew list` (full)                    | 0.90 / 0.69 / 1.43 | **1.01** |
| `brew list --formula`                 | 1.24 / 1.05 / 0.96 | 1.08     |
| `brew list --cask`                    | 0.52 / 0.47        | 0.50     |
| `brew list jq` (targeted, one pkg)    | 6.36 / 7.35 / 4.43 | **6.05** |
| `brew --version` (brew startup floor) | 0.31 / 0.32 / 0.24 | 0.29     |

55 probes × 1.01 s = **~55.6 s of pure detection**. The same information costs
one `brew list --formula` (1.08 s) plus one `brew list --cask` (0.50 s) =
**1.58 s**. Saving: **~54 s**. On a fresh Mac with an empty Cellar the listing
collapses to brew's startup floor, so 55 × 0.29 s ≈ 16 s becomes ~0.6 s — still
**~15 s**.

**Scoping caveat, stated up front.** `internal/commands/base.go:488` and `:493`
short-circuit on `global_config.yaml` _before_ `checkInstalled` runs, so re-runs
mostly skip this entirely. The cost lands on the **first** run — which is exactly
the fresh-machine case `dg install` exists for. That is why this is worth an ADR
rather than being a micro-optimization.

## Decision

Devgeta answers "is this package already installed?" from an installed-package
set captured **once per run** and reused for every subsequent check, instead of
executing a package-manager probe per package.

The set is populated lazily — nothing is executed if no probe is ever needed —
and holds the formula set, the cask set, and the Debian equivalent as separate
listings, matching the three probe functions they replace.

**The cache is process-wide, not per-`Command`-instance.** This is part of the
decision, not an implementation detail left to the builder.
`internal/commands/factory.go:27-41` returns a **fresh** `&MacOSCommand{...}` /
`&DebianCommand{...}` from every `NewCommand()` call, and callers construct one
per app rather than threading a single value through. A `sync.Once` field on the
struct would therefore be populated once _per instance_ and would save close to
none of the ~54 s — the listing would simply run again for the next app. The
lifetime this ADR commits to ("once per run") is only achievable with state that
outlives any one `Command` value: package-level in `internal/commands`, guarded
by a `sync.Once`-style populate and a mutex for the invalidation below.

That choice carries two obligations, both consequences of the state being
package-level:

- **Test isolation.** Package-level cache state persists across tests in the
  same binary. It needs an explicit reset seam that tests call via
  `t.Cleanup`, in the spirit of the `paths.Paths.*` save/restore rule
  (CLAUDE.md §6) — otherwise one test's cached listing silently answers
  another's probe.
- **Concurrency.** Package-level state is reachable from any goroutine, so the
  populate and the invalidation must both be guarded. The concurrency primitive
  is still an implementation choice; the requirement that there be one is not.

**The set is invalidated after any install or uninstall in the same run.** This
is not optional: without it, devgeta believes a package it just installed is
still missing, and the idempotency check the cache exists to serve becomes
wrong. Staleness is bounded by two rules together — the cache never outlives the
process, and any mutation of package state drops it. There is no time-based
expiry and no cross-run persistence; both would reintroduce a window in which
the cached answer can disagree with reality and neither is needed, because a
`dg install` run is short and every mutation inside it goes through devgeta.

### Considered and rejected — a targeted probe (`brew list <pkg>`)

This is the obvious fix, and it is **6x slower**: measured 6.36 / 7.35 / 4.43 s,
mean **6.05 s**, against 1.01 s for listing everything. `brew list <pkg>`
resolves the formula rather than reading the Cellar directory. Swapping the
current code to targeted probes would take the install path from ~55 s of
detection to roughly ~330 s.

This measurement is the single most important thing this ADR preserves. It is
counterintuitive enough that a future contributor looking at "we list 242
formulae to check one name" will read it as obviously wasteful and "optimize"
the cache back into per-package calls, making the problem dramatically worse.
The narrower call is not the remedy; the one-time listing is.

### Considered and rejected — caching across runs on disk

A persisted set would help re-runs, which are already fast because
`base.go:488`/`:493` short-circuit on `global_config.yaml` before any probe. It
would help the first run not at all — there is nothing cached yet — and the
first run is the entire measured cost. It also creates a file that can disagree
with the real package manager after any `brew install` performed outside
devgeta, with no reliable way to notice. Cost and a correctness hazard for no
win on the case that matters.

### Considered and rejected — leaving it alone

~54 s is a quarter to a third of the perceived duration of a fresh-machine
install, spent entirely on detection that produces no output. It is not a
rounding error and it lands on the flagship first-run experience.

## Consequences

**Easier.** The measured ~54 s (or ~15 s on an empty Cellar) leaves the install
path. Detection stops being O(packages) subprocesses and becomes O(1) per
package manager. Anything else that needs installed-package state within a run
can read the same set rather than adding another listing.

**Harder.** Correctness now depends on an invalidation rule that is invisible at
the call site: a future code path that installs a package without going through
the mutation seam will leave the cache stale, and the symptom (devgeta trying to
install something it just installed, or skipping something it did not) points
nowhere near the cause. The invalidation must therefore live at the mutation
boundary itself rather than being remembered by each caller — that is a
constraint on how the fix is built, not a note for reviewers.

**Trade-off accepted.** The first probe in a run gets slower in the trivial
case: a `dg install` that checks a single package now pays for the full listing
of both formulae and casks (1.58 s) where it previously paid 1.01 s for one.
Every run that checks two or more packages is ahead, and the real workload
checks ~55.

**Not decided here.** Whether the detection phase should also run concurrently
across packages. The audit noted the coordinators are serial for-loops
(`internal/tooling/terminal/terminal.go:158`, `:217`, `:286`;
`internal/tooling/desktop/desktop.go:115`), but with a cached listing the
detection component of that cost is essentially gone, so parallelism there is a
separate and much smaller question.
