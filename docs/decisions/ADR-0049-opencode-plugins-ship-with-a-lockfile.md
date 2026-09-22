# ADR-0049 — OpenCode plugins ship with a lockfile

**Date:** 2026-09-22
**Status:** ACCEPTED

## Context

Devgeta deploys six plugins into `~/.config/opencode/plugin/`. OpenCode
resolves its plugin dependency (`@opencode-ai/plugin`, declared in a
`package.json` OpenCode writes itself) with its embedded bun on **every**
startup, whenever at least one plugin file exists. With no lockfile present,
that resolution is a registry round-trip.

Measured on a machine with devgeta's plugins installed, timing
`opencode models`:

| Configuration                      | Wall  | CPU  |
| ---------------------------------- | ----- | ---- |
| Normal                             | 62.6s | 5.5s |
| `--pure` (plugins disabled)        | 1.25s | 1.2s |
| All plugin files removed           | 0.98s | 1.2s |
| **One** plugin file only           | 56.0s | 5.0s |
| + lockfile in `~/.config/opencode` | 9.6s  | 2.3s |
| + lockfile in `~/.opencode`        | 0.87s | 1.0s |

Two things the table settles. The one-plugin row shows the cost is a fixed
penalty triggered by the plugin subsystem waking up, not per-file work — so
shipping fewer plugins would not have helped. And ~90% of the time is idle
wait, not computation, which is what identifies it as a network round-trip.

The dependency was already vendored at exactly the right version. The
resolution was pure waste. The penalty also grows as a machine's npm cache
ages: this user's logs show 14s on 2026-09-16 degrading to 47s by 2026-09-21.

Two directories are involved, and both must be fixed — the config dir alone
leaves ~10s on the table. Devgeta already knows both as
`paths.Paths.Config.OpenCode` and `paths.Paths.Home.OpenCode`.

Three options were considered:

1. **Generate the lockfile at configure time** with
   `npm install --package-lock-only --offline`.
2. **Ship a prebuilt lockfile** under `configs/`.
3. **Report upstream only** and document a manual workaround.

## Decision

**Option 1.** After deploying plugins, devgeta ensures a lockfile exists in
both OpenCode directories, generating one from the already-vendored
`node_modules` when `package.json` exists and no lockfile does.

Option 2 was rejected outright: a checked-in lockfile hard-pins a version that
drifts the moment the user upgrades OpenCode, so it would install the wrong
dependency or fail — a shipped artifact carrying a decision that is wrong for
the stranger who installs it ([principle 8](../../CLAUDE.md#3-product-principles)).
Option 3 leaves every devgeta user paying a 45–60s cold-cache penalty; the
upstream report is still worth filing, but it is not a fix we control.

`--offline` is load-bearing: it resolves purely from the vendored
`node_modules`, so generating the lockfile cannot itself make the network call
we are trying to eliminate, and cannot change which version is installed.

npm is invoked through a raw `cmd.CommandParams` rather than a new app
wrapper. CLAUDE.md §6 forbids reaching _around_ an existing wrapper; npm has
none, and `internal/apps/neovim/deps.go:78` already sets the precedent for
invoking it directly. Adding a wrapper for one call site would be speculative.

The step is **best-effort**. A missing npm, or a failing generation, logs a
warning and continues — a slow OpenCode is a far better outcome than a failed
`dg configure`.

### A --force reconfigure deletes by allowlist

Implementing the above surfaced a second defect that made the first
unfixable. `ForceConfigureTheme` opened with `os.RemoveAll` over the whole of
`~/.config/opencode` — so `dg configure opencode --force` deleted the ~60MB of
vendored `node_modules`, the `package.json` and lockfile describing them, and
any lockfile devgeta had just generated. It guaranteed a full cold install on
the next launch: the worst case of the stall this ADR removes.

devgeta is one of several writers in that directory and by far the least
entitled. OpenCode keeps its own `tui.json` there, users keep backups, and
`rtk init -g --opencode` installs rtk's plugin into `plugins/` — the plural
sibling of devgeta's `plugin/`, which devgeta never creates. The blanket wipe
took all of it, silently tearing out an integration the user opted into.

So the wipe is now an **allowlist of devgeta-generated entries**
(`opencode.json`, `themes/`, `plugin/`, plus `baseapp.SharedConfigParts`),
not an enumeration of what to spare. The direction matters: an allowlist fails
safe, because a file devgeta has not heard of survives by default, while the
inverse rule silently destroys anything new OpenCode or an integration starts
writing. The cost is that `devgetaManagedEntries` must be kept in step with
what `ForceConfigureTheme` writes; a stale entry there leaks a generated file
instead of destroying a foreign one, which is the better failure.

## Consequences

Cold OpenCode startup drops from ~45–60s to under a second for every devgeta
user who installs the OpenCode config, and stops degrading as their npm cache
ages.

Devgeta now writes one file into `~/.opencode`, a directory OpenCode's own
installer owns. This is a deliberate exception, accepted because the
measurement shows the fix is incomplete without it. Devgeta creates the
lockfile only when absent and never rewrites or deletes an existing one, so it
cannot clobber state OpenCode or the user established.

`dg configure opencode --force` stops destroying OpenCode's dependency state,
its `tui.json`, user backups, and rtk's `plugins/` directory. Users who ran it
before this change lost those files; there is no recovery path for them beyond
re-running `rtk init -g --opencode`, which reinstalls the rtk plugin.

The lockfile pins the dependency version current at configure time. After an
OpenCode upgrade it is stale, and bun will fall back to resolving — the old
slow path, not a broken one. Re-running `dg configure opencode --force` after
an upgrade restores the fast path. Making devgeta detect staleness on its own
would mean tracking OpenCode's version as new state; that is deliberately not
built until something needs it (CLAUDE.md §6).

Devgeta gains a soft dependency on npm for this one optimization. Users
without npm keep today's behavior exactly.
