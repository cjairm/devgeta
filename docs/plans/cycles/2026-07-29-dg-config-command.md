# Cycle: `dg config` — make settings discoverable and settable

**Date:** 2026-07-29
**Estimated Duration:** ~6 hours (was ~4 before review; locking added)
**Status:** Done

---

## 1. Domain Context

`~/.config/devgeta/global_config.yaml` holds two very different kinds of data in one file:

- **State devgeta writes for itself** — what it installed (`installed`, `already_installed`),
  what failed (`failed_installations`), which shell features are on (`shell`), the MRU repo
  list (`worktree.recent_repos`), derived paths (`app_path`, `config_path`), and the record of
  an rtk hook opt-in (`integrations.rtk_claude_hook`).
- **Settings a user is meant to tune** — `worktree.default_ai`, `worktree.search_paths`,
  `worktree.scan_depth`, `worktree.default_layout`, and (as of `69348e4`)
  `worktree.attach_after_create`.

Every one of those settings follows the same convention: **absent (or empty) means "use the
default."** That is deliberate and worth keeping — it means a default improved in code reaches
every existing user, instead of being frozen into their file at install time.

The cost is that **a setting is invisible until you already know it exists.** Four of the five
carry `omitempty`, so they never appear in a real config file; the fifth, `default_ai`, is
tagged `yaml:"default_ai"` with no `omitempty`, so it persists as an empty key that documents
nothing about its valid values. The only place any of them is written down is `docs/spec.md`.
There is no command to list them and no command to set one — the documented workflow is
"hand-edit YAML," which you can only do if you knew the key was there.

This surfaced concretely with `attach_after_create`: it shipped, and the obvious moves
(`dg install`, `dg configure devgeta --force`) did not reveal it — correctly, since
`Create()` early-returns when the config already exists (`internal/config/fromFile.go:234`),
so nothing backfills keys into an existing file.

Two approaches were considered and rejected before this one (§7): writing a default skeleton
into the file, and writing YAML comments documenting each key.

---

## 2. Engineer Context

### The import direction that constrains this design

**`internal/config` cannot import `internal/tooling/worktree`.** The dependency already runs
the other way, in four files: `layout.go:14`, `repo_candidates.go:15`, `scan.go:16`,
`worktree.go:24`. This is the single most important constraint on where the registry lives —
round 1 of this plan put validation in `internal/config/settings.go` calling worktree
resolvers, which could not have compiled.

Consequences for validation, all verified:

- The AI-alias validator is `worktree.ResolveAICoder` (`aicoder.go:91`) — exported, but in
  worktree. (Round 1 of this plan called it `ResolveAIAlias`; no such function exists.)
- Layout-name validation is `lookupBuiltinLayout` (`layout.go:275`) — **private**.
  `worktree.BuiltinLayoutNames()` (`layout.go:265`) and `worktree.ResolveLayout`
  (`layout.go:337`) are exported.
- `defaultScanDepth = 4` is a **private** const (`worktree/scan.go:23`).

So the registry lives in `cmd/`, which already imports both packages. See §7 for why a new
dependency-neutral package was rejected.

### Where each default actually lives

The registry must not restate these; it must call them (§5 Step 1):

| Setting               | Default owned by                                                       |
| --------------------- | ---------------------------------------------------------------------- |
| `attach_after_create` | `config.(*GlobalConfig).ShouldAttachAfterCreate()` (`fromFile.go:124`) |
| `scan_depth`          | `defaultScanDepth` const, `worktree/scan.go:23` (private today)        |
| `default_layout`      | `worktree.ResolveLayout` precedence ladder (`layout.go:337`)           |
| `default_ai`          | same ladder; empty falls back to `opencode`                            |
| `search_paths`        | empty = scanning disabled (the only off-switch; `fromFile.go:92`)      |

### Persistence

- `internal/config/fromFile.go:214` — `Save()` is `yaml.Marshal(gc)` + `WriteFileAtomic`.
  Plain struct marshal, so **any comment in the file is destroyed on the next save**, and
  saves are frequent (`recent_repos` is rewritten on every worktree create). This is why the
  comment approach is not viable.
- **`Save()` is read-modify-write with no lock.** Atomic rename prevents a torn file; it does
  **not** prevent a lost update. Two processes that both `Load()` then `Save()` will have the
  later writer silently discard the earlier one's change. There are **65 production `Save()`
  call sites across 33 files** (86 including tests, across 40 files) — see §4 for what this
  cycle converts and what it deliberately does not.
- `WriteFileAtomic` replaces the file by rename, so the inode changes. A lock must therefore
  be taken on a **sidecar** file, never on `global_config.yaml` itself.

### Other files

- `internal/config/fromFile.go:184` — `GlobalConfig`; `:89` — `WorktreeConfig`; `:63` —
  `IntegrationsConfig`.
- `internal/apps/claude/claude.go:245` sets `RtkClaudeHook`; `internal/apps/rtk/rtk.go:152`
  clears it. Neither is a plain user setting — see §4.
- `cmd/task.go:46` — the model for a parent command with subcommands; `:325-338` for
  registration on `rootCmd`.
- `cmd/list.go:155,188,212` — the `--plain` + `isInteractiveTerminal()` convention.
- `go.mod:38` — `golang.org/x/sys v0.45.0` is already present as an **indirect** dependency,
  so `unix.Flock` needs no new module, only promotion to direct.

**Testing patterns:** [testing-patterns.md](../../guides/testing-patterns.md). Config tests
cannot use `internal/testutil` (import cycle — see `internal/config/fromFile_test.go:13`); use
the local `setupIsolatedConfigPaths(t)` helper. Every test that writes config must isolate
`paths.Paths` first.

```bash
go test ./internal/config/ ./cmd/
go test ./...
make lint
```

---

## 3. Objective

`dg config` lists every user-settable key with its default, current value, and what it does;
`dg config set`/`unset` change one without hand-editing YAML, under a lock that makes
read-modify-write safe; and a new setting cannot be added without appearing there, because the
build fails if it isn't registered.

---

## 4. Scope Boundary

### In Scope

- [x] A settings registry in `cmd/`, with each entry's default **derived by calling the code
      that owns it**, not restated as a literal
- [x] `dg config` / `dg config list` — every key with default, current value, description
- [x] `dg config get <key>` — the effective value only, script-friendly
- [x] `dg config set <key> <value...>` — validate, then persist transactionally
- [x] `dg config unset <key>` — remove the key so "absent = default" applies again
- [x] `config.Update(fn)` — a lock-held load→mutate→save transaction, and an advisory
      `flock` on a sidecar lock file
- [x] Convert the writers that realistically race a human running `dg config set`: the
      worktree `recent_repos` path (`internal/tooling/worktree/worktree.go`)
- [x] Add `omitempty` to `worktree.default_ai` so `unset` removes the key uniformly
- [x] A reflection test that fails the build when a field in a settings-bearing struct is
      neither registered nor explicitly declared state
- [x] Tests: registry, each subcommand, validation failures, round-trips, missing config,
      malformed config, concurrent update
- [x] `docs/spec.md` (command reference; settings table points at `dg config`) and `README.md`

### Explicitly Out of Scope

- **Converting all 65 production `Save()` call sites to `config.Update`.** This cycle adds the
  transaction API and converts `dg config` plus the one path that actually races it. The
  install/configure flows are long-running, user-initiated, one-at-a-time operations; a
  33-file mechanical conversion is its own cycle and would dwarf this one. **The plan states
  plainly that the race is only closed for converted callers** — no claim of a
  transactional codebase.
- **`integrations.rtk_claude_hook` as a settable key.** ADR-0004:64-70 defines the opt-in as
  `dg configure claude --force --only=rtk`, which both records the flag _and_ re-renders
  `settings.json` from a template honoring it; `dg uninstall rtk` clears it. A bare `set`
  would flip the record without touching the deployed file, so persisted state and deployed
  config would disagree. It is state-of-a-deploy, not a preference: it goes in the
  registry's state denylist with that reasoning, and `dg config set` refuses it.
- **Editing state.** `installed`, `already_installed`, `failed_installations`, `shell`,
  `worktree.recent_repos`, `app_path`, `config_path`, `shortcuts`, `current_font`,
  `current_theme` are written by install/configure flows. `dg config` refuses them.
- **Writing defaults into the file.** The "absent = default" convention stays; see §7.
- **A TUI for settings**, **per-repo config**, **renaming existing keys**, and
  **`dg config edit`** (which would reinstate hand-editing as the primary path).

**Scope is locked.**

---

## 5. Implementation Plan

### File Changes

| Action | File Path                               | Description                                       |
| ------ | --------------------------------------- | ------------------------------------------------- |
| Create | `internal/config/lock.go`               | Sidecar `flock` + `Update(fn)` transaction        |
| Create | `internal/config/lock_test.go`          | Lock held across load→save; concurrent writers    |
| Modify | `internal/config/fromFile.go`           | `omitempty` on `default_ai`                       |
| Modify | `internal/tooling/worktree/scan.go`     | Export the scan-depth default for the registry    |
| Modify | `internal/tooling/worktree/worktree.go` | `recent_repos` write goes through `Update`        |
| Create | `cmd/config.go`                         | `configCmd` + four subcommands                    |
| Create | `cmd/config_settings.go`                | The registry (needs both config and worktree)     |
| Create | `cmd/config_test.go`                    | Subcommands, validation, output shape             |
| Create | `cmd/config_settings_test.go`           | Registry + reflection completeness test           |
| Modify | `cmd/root.go`                           | Register `configCmd`                              |
| Modify | `docs/spec.md`, `README.md`             | Reference + command list                          |
| Modify | `go.mod`                                | Promote `golang.org/x/sys` to a direct dependency |

### Step 1: Lock + transaction

Create `internal/config/lock.go`:

```go
// Update runs fn against a freshly loaded config while holding an exclusive
// lock, then saves. Load and Save inside one lock is the point: Save alone is
// atomic (temp + rename) but not lost-update-safe, so a plain
// Load/mutate/Save pair can silently discard a concurrent writer's change.
//
// A missing config file is initialized BY Update, in memory, under the same
// lock: Load's os.IsNotExist becomes a zero-value GlobalConfig{} that fn then
// mutates, and the single Save at the end is the file's first write. Callers
// must NOT call Create() first — see the note below.
func Update(fn func(gc *GlobalConfig) error) error
```

**`Update` owns first-run initialization; no caller may call `Create()` around it.**
`Create()` (`fromFile.go:232-249`) delegates to `Reset()`, which does `*gc = GlobalConfig{}`
and writes a blank file. Called before `Update`, that write lands **outside** the lock, so on
a machine with no config yet two writers race like this:

1. A: `Create()` → file absent → begins `Reset()`
2. B: `Create()` → file still absent → begins `Reset()`
3. A: `Update` → lock, load, set its key, save
4. B: `Reset()`'s `WriteFileAtomic` completes → **blank file replaces A's saved setting**

The lock cannot help, because the destructive write happens before anyone takes it. Nothing
about the ordering above is exotic — step 2 only needs to interleave anywhere inside step 1.
`Reset()` also zeroes the receiver, so a shared `gc` handed to `Create()` and then reused
would be blanked in memory too.

So `Update` absorbs the two halves of `Create()` separately, by whether they destroy data:

- **Directory creation** (`MkdirAll` of `<config-root>/devgeta`) is idempotent and destroys
  nothing, and has to happen before the sidecar lock file can be opened at all. `Update` does
  it first, unlocked.
- **The blank-file write** never happens. `Load()` returning `os.IsNotExist` is not an error
  inside `Update`; it means "start from `GlobalConfig{}`". Exactly one write occurs per
  `Update` call, inside the lock, and it already contains fn's change.

`Create()` keeps its existing callers (install/configure bootstrap) unchanged — it is only
barred from wrapping `Update`.

Locking notes, all load-bearing:

- The lock is a **sidecar** (`global_config.lock`), never `global_config.yaml` — `Save()`
  replaces the config by rename, so the inode changes and a lock on it would be orphaned.
- `unix.Flock(fd, LOCK_EX)` from `golang.org/x/sys` (already in `go.mod` as indirect).
  Chosen over an `O_CREATE|O_EXCL` lockfile because the OS releases a flock when the process
  exits, **including on crash** — an exclusive-create lockfile leaves a stale lock that then
  needs its own staleness heuristic.
- macOS and Linux both support flock; those are the only supported platforms (CLAUDE.md §8),
  so no Windows fallback is needed.
- Block with a timeout so a wedged holder produces an actionable error rather than a hang.

Tests for this step must include **two concurrent `Update` calls with no config file present**,
each setting a different key, asserting both survive — the first-run race above, which a
concurrency test that pre-creates the file cannot catch.

- Verify: `go test ./internal/config/ -run TestUpdate`

### Step 2: `omitempty` on `default_ai`, and convert the racing writer

Add `omitempty` to `WorktreeConfig.DefaultAI` so `unset` removes it like every other key.
Backward-compatible: `""` and absent already both mean "fall back to opencode" to every
reader, so no migration and no reader change. (A config-format change per CLAUDE.md §10 —
called out here for explicit approval.)

Convert `internal/tooling/worktree/worktree.go`'s `recent_repos` write to `config.Update`.
That is the write that fires on every worktree create and is the one realistically racing a
human typing `dg config set`.

- Verify: `go test ./internal/config/ ./internal/tooling/worktree/`

### Step 3: The registry

Create `cmd/config_settings.go` — in `cmd/` because it needs both `internal/config` and
`internal/tooling/worktree`, and only `cmd/` may import both:

```go
type Setting struct {
	Key         string // dotted path, e.g. "worktree.attach_after_create"
	Description string
	Kind        string // "bool" | "int" | "string" | "stringlist"
	// Default returns the live default by calling whatever owns it, so the
	// registry cannot drift from the runtime behavior. Never a literal.
	Default func() string
	Get     func(gc *config.GlobalConfig) (value string, isSet bool)
	Set     func(gc *config.GlobalConfig, raw []string) error // parse + validate + assign
	Unset   func(gc *config.GlobalConfig)
}
```

Five entries: `worktree.default_ai`, `.search_paths`, `.scan_depth`, `.default_layout`,
`.attach_after_create`.

`Default` calls the owner — `(*config.GlobalConfig)(nil).ShouldAttachAfterCreate()`,
`worktree.DefaultScanDepth()`, `worktree.ResolveLayout("", "", &config.GlobalConfig{})` for
the layout/AI fallback. A hand-typed default string is what round 1 got wrong; deriving it
makes drift impossible rather than merely detectable.

`Set` delegates validation to the existing resolvers — `worktree.ResolveAICoder` for
`default_ai`, the same lookup `ResolveLayout` uses for `default_layout` (so an unknown name
produces the identical error listing valid names), non-negative for `scan_depth`.

`Set` takes `[]string` so `search_paths` is **variadic** —
`dg config set worktree.search_paths ~/code ~/work` — letting the shell split and avoiding
any delimiter that a valid path could contain. Scalar settings reject more than one value.

- Verify: `go test ./cmd/ -run TestSettings`

### Step 4: The completeness test

In `cmd/config_settings_test.go`, reflect over the settings-bearing structs
(`config.WorktreeConfig`, `config.IntegrationsConfig`) and require every field to be either
registered or listed in an explicit `knownStateFields` map with a comment saying why —
`recent_repos` is MRU state, `rtk_claude_hook` is a deploy record per ADR-0004.

This is the step that makes the original problem structurally impossible (CLAUDE.md §4:
prefer enforced-by-code over a convention people must remember). Adding a field to
`WorktreeConfig` then fails the build until the author exposes it or declares it state.

Also assert: no duplicate keys, every `Key` resolves to a real YAML tag path, every entry has
a non-empty `Description`, and every `Default()` returns non-empty.

- Verify: `go test ./cmd/ -run TestSettingsCompleteness`

### Step 5: The subcommands

`cmd/config.go`, modeled on `taskCmd` (`cmd/task.go:46`). Bare `dg config` runs `list`.

- `list` — table of key, current value (or `(default)`), default, description. Follows
  `cmd/list.go`'s `--plain` + `isInteractiveTerminal()` convention. Works with **no config
  file**: defaults are code-owned, so it lists them all as `(default)`.
- `get <key>` — prints the **effective** value and nothing else, so scripts can consume it.
  For `default_layout` that means resolving through `ResolveLayout`, so an unset layout with
  `default_ai: claude` prints `claude`. `list` is where set-vs-default is visible.
- `set <key> <value...>` — `Update(fn)` wrapping lookup → `Set` (parse + validate) → save, and
  **only** `Update`: a machine with no config yet is handled inside the lock by Step 1, never by
  a `Create()` call around it. Invalid value → the validator's error, nothing written. Prints
  the previous and new value.
- `unset <key>` — `Update(fn)` → `Unset` → save; confirms which default now applies.

Unknown or state keys → error listing valid keys, non-zero exit.

- Verify: `go test ./cmd/ -run TestConfig`

### Step 6: Docs

`docs/spec.md` gains a `dg config` section; the existing `worktree.*` settings list gains a
pointer to `dg config` as the supported way to change them. `README.md` gains `dg config`.

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/config/ ./cmd/ ./internal/tooling/worktree/
go test ./... -cover
make lint
```

New tests must cover, beyond the happy paths: **no config file** (`list`/`get` work, `set`
creates), **malformed YAML** (a clear error, not a panic, and no truncation of the bad file),
and **concurrent update** (two `Update` calls in goroutines both land — the lost-update case
the lock exists to prevent), **including the first-run variant where no config file exists when
both start**, which is the case a `Create()`-then-`Update` sequence would lose.

### Manual

1. `make build`, then install it: `go build -o ~/.local/bin/devgeta .`. `dg` resolves to
   `~/.local/bin/devgeta` and `make build` only writes `./devgeta`, so a plain `make build`
   leaves the old binary running. (This trap is what made `attach_after_create` look broken;
   see §8.)
2. `dg config` — all five keys, each `(default)` on a fresh config.
3. `dg config set worktree.attach_after_create false` → confirms old→new; the key appears in
   the file; `dg config` shows `false`.
4. `dg ws` → `n` → create → dashboard stays open (setting actually in effect).
5. `dg config unset worktree.attach_after_create` → key **gone** from the file (not `false`),
   `dg config` shows `(default)`, `n` attaches again.
6. `dg config unset worktree.default_ai` → key gone entirely, proving the `omitempty` change.
7. `dg config set worktree.default_layout nonsense` → the same "unknown layout" error the CLI
   already gives, file unchanged.
8. `dg config set worktree.scan_depth -1` → rejected, file unchanged.
9. `dg config set worktree.search_paths ~/code ~/work` → both stored; a path containing a
   comma or a space round-trips intact.
10. `dg config get worktree.scan_depth` → prints `4` and nothing else.
11. `dg config get worktree.default_layout` on an unset config → prints the effective layout.
12. `dg config set integrations.rtk_claude_hook true` → refused as not a setting.
13. `dg config | cat` → plain parseable output.
14. **Lock check:** in one shell hold the lock (a test binary sleeping inside `Update`), then
    run `dg config set` in another → it waits, then succeeds, and both changes survive.
15. Move `global_config.yaml` aside → `dg config` still lists defaults; `dg config set`
    recreates the file.
16. Corrupt the YAML → clear error, original file not truncated.

### Regression Check

- `dg install` and `dg configure devgeta --force` behave exactly as before; no backfill added,
  no default changed.
- A config written before this cycle loads unchanged. `default_ai: ""` in an existing file
  keeps resolving to opencode after the `omitempty` change.
- Creating a worktree still updates `recent_repos` (now via `Update`) and clobbers no setting.

---

## 7. Risks & Trade-offs

### Rejected: writing a default skeleton into `global_config.yaml`

1. **It freezes defaults.** Once `scan_depth: 4` is materialized, improving that default in
   code never reaches that user. The config is built on "absent = default" to avoid exactly
   this.
2. **`Create()` cannot do it for existing users** (`fromFile.go:234` early-returns), so it
   would need a backfill on every `configure` — a write path mutating user config as a side
   effect of an unrelated command.
3. **It duplicates the default**, free to drift from the accessor that owns it.

### Rejected: YAML comments documenting each key

`Save()` is `yaml.Marshal(gc)` (`fromFile.go:215`), which emits fresh YAML with no comments,
and `recent_repos` is rewritten on every worktree create — any comment would be erased within
minutes. Preserving comments means round-tripping through `yaml.Node`, a rewrite of the config
layer for a benefit `dg config list` delivers directly.

### Rejected: a dependency-neutral `internal/settings` package

Round 1's reviewer suggested extracting validation and default metadata into a package both
`config` and `worktree` could import. It would work, but it means relocating
`defaultScanDepth`, layout-name validation, and AI-alias validation out of the packages that
own them, to serve **one** consumer. `cmd/` already imports both, so putting the registry
there closes the cycle with no new package and no worktree refactor (CLAUDE.md: prefer
existing structure; don't add for speculative reuse). If a second consumer appears — a
settings pane in `dg ws` is the plausible one — extract then, with two real callers to shape
the API.

### Risks

| Risk                                                        | Likelihood | Mitigation                                                                                         |
| ----------------------------------------------------------- | ---------- | -------------------------------------------------------------------------------------------------- |
| Registry drifts from the structs as fields are added        | High       | Step 4's reflection test fails the build; the cycle's core guarantee                               |
| A stated default drifts from the runtime default            | Med        | `Default` is a func calling the owner, never a literal — drift is unrepresentable, not just tested |
| Lost update between `dg config set` and a concurrent write  | Med        | `Update` holds one lock across load→save; **only for converted callers** — 64 others still race    |
| Lock taken on the config file instead of a sidecar          | Med        | Explicit in Step 1: `Save()` renames, so the inode changes and such a lock would be orphaned       |
| Stale lock wedges the CLI after a crash                     | Low        | `flock` is released by the OS on process exit, including crash; plus a blocking timeout            |
| A state field gets exposed and users corrupt state          | Med        | Allowlist only; the denylist is asserted in a test with per-field reasoning                        |
| `default_ai`'s `omitempty` change breaks an existing config | Low        | `""` and absent are already equivalent to every reader; asserted in the regression check           |
| `get` output grows decoration and breaks scripts            | Low        | Test asserts `get` prints the bare value and nothing else                                          |

### Trade-offs Made

- **Allowlist, not reflection-driven exposure.** Every settable key is hand-written. More
  typing, but the only way to keep state unsettable; the reflection test recovers the safety
  hand-listing would otherwise lose.
- **`get` prints the effective value, `list` shows set-vs-default.** Two shapes, because one
  command that is both scriptable and readable is good at neither.
- **Partial lock conversion.** Two writers are transactional; 64 call sites are not. Stated
  outright rather than implying the codebase is safe. Converting the rest is a follow-up.
- **Registry in `cmd/`.** Not reusable outside the CLI. Accepted until a second consumer.

---

## 8. Cross-Model Review Notes

- [ ] Should `current_font` / `current_theme` be settings? They are user-facing choices but
      are written by install/configure, and setting one without re-running configure would
      leave the file disagreeing with what is deployed — the same failure mode that excluded
      `rtk_claude_hook`. Currently out of scope; worth a second opinion.
- [ ] Should `Setting` carry `RequiresReconfigure bool` so a future setting consumed by a
      template can tell the user to run `dg configure <app> --force` after `set`? Not needed
      by any of today's five, so omitted per YAGNI — but it is the natural extension point if
      `current_font`/`current_theme` are ever admitted.
- [ ] Is a blocking-with-timeout lock the right call versus failing fast when contended? A
      wait is friendlier for a human at a prompt; fail-fast is better for scripts. Possibly
      `--no-wait`.
- [ ] **Separate from this cycle, found while diagnosing it:** there is no `make install`
      target. `make build` writes `./devgeta` while `dg` resolves to `~/.local/bin/devgeta`,
      so "rebuild and test" silently requires a manual copy — the trap that made
      `attach_after_create` appear not to work. Worth a Makefile target or a CONTRIBUTING
      note.

**Reviewer notes:**

**Round 1 (2026-07-29).** Six findings, all verified against the codebase and all correct.

- **Import cycle in the validation boundary (CRITICAL, correct).** Step 1 had
  `internal/config/settings.go` calling worktree resolvers; worktree already imports config
  (four files), so it could not compile. The named function `ResolveAIAlias` also did not
  exist (`ResolveAICoder` does), and the layout lookup is private. Fixed by moving the
  registry to `cmd/`, the one layer that may import both — and the reviewer's suggested
  neutral package was rejected as premature for a single consumer (§7).
- **`rtk_claude_hook` exposed as a plain bool (correct).** ADR-0004:64-70 makes it a record of
  `dg configure claude --force --only=rtk`, which also re-renders `settings.json`. A bare
  `set` would desync state from deployment. Removed from scope; now an explicitly documented
  state field.
- **Defaults not actually single-sourced (correct, and the fix is stronger than the
  suggestion).** `defaultScanDepth`, `ShouldAttachAfterCreate`, and `ResolveLayout` each own a
  default that a hand-typed `Default string` would duplicate. Rather than adding a
  drift-detecting test, `Default` became a func that calls the owner, so drift cannot be
  expressed.
- **Atomic rename cited as lost-update mitigation (correct).** It only prevents torn files.
  Added `config.Update` under a sidecar `flock`, and stated plainly that only converted
  callers are safe — 65 production `Save()` sites exist and converting all of them is its own
  cycle.
- **`search_paths` syntax unresolved while `set` was locked in scope (correct).** Resolved as
  variadic args, so the shell splits and no delimiter can collide with a valid path.
- **`omitempty` claim wrong for `default_ai` (correct).** §1 said all five settings carry it;
  `default_ai` does not (`fromFile.go:90`). Fixed, and `omitempty` is now added to that field
  so `unset` removes keys uniformly — a §10 config-format change, flagged for approval and
  backward-compatible because `""` and absent already mean the same thing.

**Round 2 (2026-07-29).** Two findings, both verified and both correct.

- **`set` calling `Create()` before `Update` reintroduces the lost update (IMPORTANT,
  correct).** Round 1's fix put a lock around load→save but left a `Create()` call outside it,
  and `Create()` writes a blank file via `Reset()` (`fromFile.go:232-249`). On first run two
  writers interleave and the later `Reset()` clobbers the earlier `Update`'s save — the lock is
  irrelevant because the destructive write precedes it. Fixed in Step 1: `Update` now owns
  missing-file initialization, splitting `Create()` by whether a half destroys data —
  `MkdirAll` (idempotent, and required before the sidecar lock can be opened) stays, the blank
  write is gone, and a `Load()` that returns `os.IsNotExist` becomes an in-memory
  `GlobalConfig{}` instead of an error. Exactly one write per `Update`, inside the lock. Step 5
  no longer mentions `Create()`, and the reviewer's suggested first-run concurrency test is now
  required by Step 1 and §6.
  - _Answering the reviewer's question:_ yes — in memory, one save. That is the fix as
    implemented above. `Create()` is not called by `Update` at all; it keeps its existing
    install/configure callers untouched.
- **`Save()` call-site counts unsupported (MINOR, correct).** The doc claimed 130 sites across
  33 files, and derived "128 others still race" from it. Actual: **65 production call sites
  across 33 files**, 86 including tests across 40 files. The "33 files" figure was right by
  coincidence; 130 matched nothing. Corrected in §2, §4, §7's risk table, §7's trade-offs, and
  the Round 1 note. Step 2 converts one site (`worktree.go:1144`, the only `Save()` in that
  file and the only writer of `recent_repos`), so the remainder is **64**.
