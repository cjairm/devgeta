# Cycle: Token and context efficiency

**Date:** 2026-08-25
**Estimated Duration:** ~10–14 hours
**Status:** Draft
**ADRs:** [ADR-0031](../../decisions/ADR-0031-context-is-reduced-at-write-time-not-at-send-time.md),
[ADR-0032](../../decisions/ADR-0032-session-continuity-is-a-durable-note-not-a-longer-session.md)
**Origin:** maintainer repeatedly hitting plan usage limits; investigated whether
to adopt a compression proxy ([Headroom][headroom]) and rejected it on caching,
attack-surface, and shipped-trust grounds (ADR-0031).

---

## 1. Domain Context

Devgeta configures two AI coding agents (Claude Code, OpenCode) and ships the
hooks, permissions, and `dg task` commands they run inside. Users pay for those
agents by token, against a per-seat allowance that resets on a five-hour and a
weekly window. Running out mid-week is the concrete failure this cycle addresses.

Three things drive that spend, and only the first is currently addressed:

| Driver                                      | Devgeta today                                                                   |
| ------------------------------------------- | ------------------------------------------------------------------------------- |
| Verbose tool output entering context        | `rtk` covers common CLI reads, **opt-in**, no coverage of test/build/log output |
| Long sessions resending accumulated history | nothing                                                                         |
| Base context loaded into every session      | nothing; no way to even measure it                                              |

The research position behind the approach is recorded in the two ADRs above and
is not re-argued here. The one-line rule that governs every step in this cycle:
**reduce content when it is produced, never rewrite a request in flight.**

Relevant reading: [ADR-0004](../../decisions/ADR-0004-ai-tools-install-category.md)
(rtk, the existing write-time filter), [ADR-0012](../../decisions/ADR-0012-review-knowledge-in-a-local-journal.md)
(the per-branch journal this cycle's storage mirrors), [docs/apps/rtk.md](../../apps/rtk.md),
[docs/apps/claude.md](../../apps/claude.md).

---

## 2. Engineer Context

**Relevant files:**

- `configs/claude/settings.json.tmpl` — hook registration; already has
  `PreToolUse` matchers for `Bash` and `Edit|Write`, and a `{{if .RtkClaudeHook}}`
  conditional block that is the precedent for gating a new hook behind a setting
- `configs/claude/*.sh` — five existing hook scripts; `configs/claude/lib/`
  holds shared bash (`segments.sh`, `devgeta-repo.sh`)
- `configs/opencode/plugin/*.js` — the mirror side; every export of every file
  in this directory is invoked as a plugin (see ADR-0006)
- `internal/apps/claude/claude.go` — deploys hook scripts; a new script must be
  added here or it never lands on disk
- `internal/tooling/reviewjournal/manager.go` — per-branch markdown storage in
  `<git common dir>/devgeta/review/`: branch-name encoding, atomic write, lock
  file, snapshot reservation. **The storage layer to extract and share**, not to
  copy
- `internal/tooling/reviewjournal/journal.go` — the parse/render half; the
  handoff note's own format is separate, only storage is shared
- `cmd/task.go` — `dg task` subcommand registration (`scratch`,
  `worktree-start`, `worktree-finish`, `review-note*` are the closest shapes)
- `pkg/paths/paths.go` — `EnsureScratchDir`, `ScratchAllocPrefix`

**Key types involved:**

- `reviewjournal.Manager` — `PathFor(repoDir, branch)`, the atomic-write and
  locking helpers around it
- `cobra.Command` — subcommand definitions in `cmd/task.go`

**Testing patterns:**

- [docs/guides/testing-patterns.md](../../guides/testing-patterns.md) —
  `testutil.MockApp`, never real commands, isolate every mutated
  `paths.Paths.*` and restore via `t.Cleanup`
- Hook scripts get **two** behavioral test suites: Go in the root package
  (see `secret_guard_test.go`) and Node `.test.mjs` for the OpenCode mirror
  (see `secret-guard.test.mjs`). Both run the real script against real temp
  dirs — that is the established rigor for hooks and this cycle matches it

**Commands to run tests** — this cycle touches `configs/`, which the embedded
config tests in the root package read, and the root package is the slow one
(~4.8 min). Per [CLAUDE.md §6](../../../CLAUDE.md#which-tests-to-run) a config
change costs most of a full run anyway, so:

```bash
go test ./...                      # this cycle's default, not the exception
node --test *.test.mjs
make lint
```

While iterating on a single Go package, the targeted run still applies:

```bash
go test ./internal/tooling/handoff/ ./internal/tooling/reviewjournal/ ./cmd/
```

---

## 3. Objective

Give devgeta users three local, deterministic mechanisms that cut agent token
spend without adding anything to the request path: a **write-time filter** for
verbose tool output, a **durable per-branch handoff note** so sessions can be
ended instead of grown, and a **context budget report** so base context is
measurable.

---

## 4. Scope Boundary

### In Scope

- [ ] **A. Output-budget hook** — `configs/claude/output-budget.sh` +
      `configs/opencode/plugin/output-budget.js`, a `PreToolUse` Bash hook that
      caps verbose command output at write time via the runner in A2.
      Rules defined once in Go and generated into the sidecar — schema, defaults,
      and precedence in Step 4. Built-in defaults only in v1; no user-defined
      rule surface.
- [ ] **A1. One opt-in field, toggled through the settings registry** —
      `IntegrationsConfig.OutputBudget *bool` (`yaml:"output_budget,omitempty"`)
      in `internal/config/fromFile.go`, exposed as the registry setting
      `agent.output_budget` in `cmd/config_settings.go`. Defaults **off** (see
      §8). The two agents consume the one field through different enforcement
      points because their deployment models differ — see Step 5. Full
      enablement surface in Step 3b.
- [ ] **A2. `output-budget-run.sh`** — the wrapper the hook rewrites commands
      to call. This is where capping actually happens; the hook only redirects
      to it. Contract in Step 4.
- [ ] **B. `dg task handoff`** — `--write --note <text>` / `--read` /
      `--clear`, per-branch markdown under `<git common dir>/devgeta/handoff/`,
      with a size cap enforced on write.
- [ ] **C. Storage-layer extraction** — lift git-common-dir resolution, branch
      encoding, atomic write, and the lock _transaction wrapper_ out of
      `internal/tooling/reviewjournal/manager.go` into a shared package both it
      and `handoff` use. Explicit API and an explicit stays-behind list in
      Step 1. Review-journal behavior unchanged; existing tests pass untouched.
- [ ] **D. `dg task context-report`** — report what loads into a session before
      the first prompt, per agent, from an explicit path list (Step 7).
      Read-only, no network.
- [ ] **E. Tests** — Go + Node behavioral suites for the hook; Go unit tests for
      `handoff`, the extracted storage layer, and `context-report`.
- [ ] **F. Docs** — `docs/apps/claude.md` (new hook), `docs/spec.md` (three new
      surfaces), `docs/guides/token-efficiency.md` (new: what to do, in what
      order, with the measured reasoning), ADR index entries, ROADMAP update.

### Explicitly Out of Scope

- **Any proxy, wrapper, or middleware in the request path.** ADR-0031. Not a
  deferral — a decision.
- **Automatic memory capture / retrieval / injection.** ADR-0032.
- **A `SessionEnd` hook that writes the handoff note automatically.** ADR-0032
  §4; the checkpoint is explicit by design.
- **Making the `rtk` hook default-on.** Separate decision, tracked in ROADMAP.
- **Shrinking devgeta's own 46KB CLAUDE.md.** Real and probably this repo's
  single biggest win, but it is _project law, not product_ — it changes no
  shipped artifact and belongs in its own cycle. Step D exists so that cycle can
  be driven by a measurement instead of a guess.
- **Model/effort defaults.** User preference, not something devgeta should set.

**Scope is locked.** Anything discovered outside it gets documented for a future
cycle and referenced here.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                                                       | Description                                                                                                     |
| ------ | ------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| Create | `internal/tooling/branchstore/store.go`                                         | Extracted per-branch file storage (encode, atomic write, lock)                                                  |
| Create | `internal/tooling/branchstore/store_test.go`                                    | Unit tests for the extracted layer                                                                              |
| Modify | `internal/tooling/reviewjournal/manager.go`                                     | Delegate storage to `branchstore`; no behavior change                                                           |
| Create | `internal/tooling/handoff/handoff.go`                                           | Note format: parse, render, size cap                                                                            |
| Create | `internal/tooling/handoff/handoff_test.go`                                      | Unit tests incl. cap-exceeded rejection                                                                         |
| Create | `internal/tooling/contextreport/report.go`                                      | Base-context measurement                                                                                        |
| Create | `internal/tooling/contextreport/report_test.go`                                 | Unit tests against a fixture tree                                                                               |
| Modify | `internal/config/fromFile.go`                                                   | Add `IntegrationsConfig.OutputBudget *bool` opt-in field                                                        |
| Modify | `cmd/config_settings.go`                                                        | Register `agent.output_budget` (bool, Set + Unset)                                                              |
| Modify | `docs/guides/agent-sync.md`                                                     | Record the Claude-only gap **if** Step 0's fallback rule fires                                                  |
| Modify | `internal/apps/opencode/opencode.go`                                            | Plugin reads the opt-in field at runtime                                                                        |
| Create | `configs/claude/output-budget.sh`                                               | PreToolUse hook: match + redirect to the runner                                                                 |
| Create | `configs/claude/output-budget-run.sh`                                           | The runner: executes, captures, caps, re-raises exit status. Deployed to `~/.config/devgeta/`, not `~/.claude/` |
| Modify | `internal/apps/baseapp/`                                                        | `EnsureAgentRuntime`: deploys the runner **and** atomically writes the sidecar; called by both configure paths  |
| Create | `internal/apps/baseapp/outputrules.go`                                          | The built-in rule table — the single definition both hooks consume                                              |
| Modify | `internal/apps/opencode/opencode.go`                                            | Call `EnsureAgentRuntime` during configure                                                                      |
| Create | `configs/opencode/plugin/output-budget.js`                                      | Mirror (OpenCode)                                                                                               |
| Modify | `configs/claude/settings.json.tmpl`                                             | Register hook behind a gate                                                                                     |
| Modify | `internal/apps/claude/claude.go`                                                | Deploy `output-budget.sh`                                                                                       |
| Modify | `cmd/task.go`                                                                   | Register `handoff`, `context-report`                                                                            |
| Create | `output_budget_test.go` (root)                                                  | Go behavioral tests for the hook                                                                                |
| Create | `output-budget.test.mjs` (root)                                                 | Node behavioral tests for the mirror                                                                            |
| Modify | `docs/apps/claude.md`, `docs/spec.md`, `ROADMAP.md`, `docs/decisions/README.md` | Docs                                                                                                            |
| Create | `docs/guides/token-efficiency.md`                                               | User-facing guide                                                                                               |

### Step-by-Step

#### Step 0: Verify the upstream contracts this cycle assumes

Same discipline as the hook-guardrails cycle, which verified payload field
names before writing a line of hook. Nothing below is safe to assume from
memory; each answer changes an implementation detail downstream.

- [ ] `PreToolUse` `updatedInput.command` semantics for Bash under **both**
      agents: is the replacement re-permission-checked, and do multiple hooks on
      the same matcher chain their rewrites or does the last one win? Decides
      the rtk-composition rule in Step 4.
- [ ] Whether OpenCode exposes a `PreToolUse` equivalent with command rewriting
      at all. If it does not, see the fallback rule directly below.
- [ ] Exact base-context load order and precedence for each agent — the Step 7
      tables are drafted from current docs and must be confirmed, especially
      `@import` resolution and whether project settings override or merge.
- [ ] Whether skill bodies or only frontmatter enter base context (Step 7
      assumes frontmatter only).

**Fallback rule if OpenCode cannot rewrite commands.** Scope A ships for Claude
only, **and the gap is recorded in the "Accepted differences" list of
[docs/guides/agent-sync.md](../../guides/agent-sync.md)** — that recording is
the deliverable, not an afterthought.

This is not the asymmetry [CLAUDE.md §12](../../../CLAUDE.md#keeping-the-two-ai-agents-in-sync)
forbids. That rule, and the "drop it from both" clause in agent-sync.md, are
scoped to **deny/ask permission rules and formatter languages** — the two lists
`internal/apps/opencode/permissions_test.go` compares string for string. An
output cap is neither: it changes nothing about what either agent is permitted
to do, so no policy goes out of sync and that test is untouched.

The governing precedent is the existing accepted-differences list, where a
capability gap in one agent already produces a one-sided **feature**:

- _"The lint feedback loop is Claude-only"_ — `format.sh` returns findings via
  `hookSpecificOutput.additionalContext`; OpenCode's `formatter` block cannot
  return context. Same shape as this: a hook capability one agent lacks.
- _"`statusLine` has no OpenCode equivalent."_

Dropping a working capability from Claude because OpenCode lacks the hook would
make both agents worse and match no precedent in this repo. What is forbidden is
shipping the gap **silently**; writing it into the accepted-differences list is
how devgeta has handled this twice already.

- Expected outcome: the Step 4, 5, and 7 tables are confirmed or corrected in
  place before implementation starts.

#### Step 1: Extract `branchstore` from `reviewjournal`

The lock primitive is **already shared** — `files.WithLock(path, timeout, fn)`
in `pkg/files`. `reviewjournal.withJournalLock` only adds the directory and the
`journals.lock` filename to it. So this extraction is smaller than "move the
locking": it moves _where the file lives_ and _how a mutation is serialized_,
nothing else.

**The API.** `branchstore.Store` is constructed with a subdirectory name and
resolves `<git common dir>/devgeta/<subdir>/`:

```go
func New(g *git.Git, subdir string) *Store   // subdir: "review" | "handoff"

func (s *Store) PathFor(repoDir, branch string) (string, error)
func (s *Store) Read(repoDir, branch string) ([]byte, error)     // absent → (nil, nil)
func (s *Store) Write(repoDir, branch string, data []byte) error // temp + rename
func (s *Store) Remove(repoDir, branch string) error

// WithLock holds the subdirectory's exclusive lock for the duration of fn.
// The CALLER owns the whole read-modify-write inside fn; the store never
// loads, mutates, or saves on the caller's behalf.
func (s *Store) WithLock(repoDir string, fn func() error) error
```

`WithLock` taking a bare `func() error` — not a
`func(data []byte) ([]byte, error)` — is deliberate. `reviewjournal`'s mutations
do more inside the lock than transform bytes (`citeBlob` shells out to git,
`stamp`/`restamp` read blobs, `WriteSnapshot` reserves a sibling file). A
byte-transform signature would force that work outside the lock and change
behavior. A bare closure lets `reviewjournal.withJournalLock` become a
one-line delegation with its transaction fully intact.

**What stays in `reviewjournal`** — explicitly, so the boundary is not
re-litigated during implementation:

| Stays                                                                            | Why                                      |
| -------------------------------------------------------------------------------- | ---------------------------------------- |
| `SnapshotPrefixFor`, `WriteSnapshot`, `reserveSnapshotPath`, `removeSnapshotsOf` | Review-only concept; no handoff analogue |
| `citeBlob`, `citeSource`, `stamp`, `stampHead`, `restamp`                        | Review-entry semantics                   |
| `ensureBase`, `Open`, `SettleByID`, `Ratify`, `Reopen`, `SettleDirect`           | Journal domain operations                |
| `Journal` parse/render, `Entry`, resolution vocabulary                           | Format, not storage                      |
| `NewAtRev` / revision-pinned reads                                               | Review-only                              |

**Lock scope note:** the lock is per _directory_, not per file (the reasoning is
in `manager.go:245`). Because `handoff` uses a different subdirectory, it gets
its own lock file and never contends with review writes. State this in the
package doc so nobody "fixes" it into a shared lock later.

- `reviewjournal.Manager` delegates; `reviewDir`, `PathFor`, `save`, and
  `withJournalLock` become thin wrappers.
- **No behavior change** — the existing reviewjournal tests must pass
  unmodified. If a test needs editing, the extraction changed behavior and is
  wrong.
- Verify: `go test ./internal/tooling/reviewjournal/ ./internal/tooling/branchstore/`

#### Step 2: `handoff` note format

- `Note` type, `Parse`, `Render`, and a `MaxBytes` cap that returns a sentinel
  error rather than truncating (ADR-0032 §2).
- Format: front matter (`branch`, `updated`, `head`) plus free markdown body.
- Verify: `go test ./internal/tooling/handoff/`

#### Step 3: `dg task handoff` subcommand

- `--write --note <text>` (also accepts `--note-file` for multi-line, mirroring
  `release --message-file`), `--read`, `--clear`.
- `--read` on a missing note exits 0 with a clear "no handoff note for
  `<branch>`" line, not an error — a fresh branch is the normal case.
- Verify: `go test ./cmd/` and `./devgeta task handoff --help`

#### Step 3b: the enablement surface

A default-off feature with no way to turn it on is not a feature. Specifying it
properly rules out the obvious choice.

**Not a `ConfigurableParts` entry.** That is how `rtk` opts in
(`claude.go:228`, dispatched in `ForceConfigureParts` at `claude.go:243`), but
it is **one-way**: `--only=rtk` sets the flag and nothing in `dg configure`
clears it. Rtk gets away with that because `dg uninstall rtk` clears the flag as
a side effect (`rtk.go:151`) — an escape hatch output-budget does not have,
since there is no binary to uninstall. Copying that pattern reproduces exactly
the gap.

**Use the settings registry** (`cmd/config_settings.go`). It already exists for
this, already has boolean entries with the same tri-state shape
(`worktree.attach_after_create`, `worktree.notify_sound`), and gives `Set` and
`Unset` as first-class operations rather than requiring a bolted-on disable.

| Element   | Value                                                                                                        |
| --------- | ------------------------------------------------------------------------------------------------------------ |
| Key       | `agent.output_budget`                                                                                        |
| Kind      | `bool`                                                                                                       |
| Field     | `gc.Integrations.OutputBudget *bool` — pointer for the unset/true/false tri-state                            |
| `Default` | Calls the same resolver the hook consults, never a restated literal (registry rule, `config_settings.go:32`) |
| `Get`     | `nil` → `("", false)`; otherwise the formatted bool                                                          |
| `Set`     | `requireExactlyOne` + `strconv.ParseBool`, mirroring `worktree.attach_after_create`                          |
| `Unset`   | Sets the field back to `nil`                                                                                 |

**Persistence and render order** — the two-step is real and must be documented,
not glossed:

The verbs are `set` / `unset` — `cmd/config.go` defines `set <key> <value...>`
and `unset <key>`, not a bare `config <key> <value>`:

```bash
dg config set agent.output_budget true    # persists to global_config.yaml
dg configure claude --force               # re-renders settings.json from the stored value
dg configure opencode --force             # regenerates the runtime sidecar (Step 5)

dg config set agent.output_budget false   # explicit disable
dg config unset agent.output_budget       # back to the default
# then re-run the two `dg configure … --force` commands above
```

Setting the value does **not** re-render either agent's config; `dg configure`
already renders whatever is stored, which is why no new configure surface is
needed. Because Claude's gate is render-time, disabling makes the hook entry
disappear from `settings.json` entirely rather than linger as a no-op.

- Tests: registry round-trip (set/get/unset), `dg config list` shows the entry
  with the right default, the rendered `settings.json` contains the hook entry
  when true and omits it when false/unset, and the OpenCode plugin no-ops when
  false/unset. `config_settings_registry_test.go` already enforces registry
  invariants — the new entry must satisfy them, not be exempted.
- Verify: `go test ./cmd/ ./internal/config/ ./internal/apps/claude/ ./internal/apps/opencode/`

#### Step 4: the rewriting contract, then `output-budget.sh`

`PreToolUse` sees the command _before_ it runs and can only replace the command
string. It never sees output. So capping cannot be done in the hook — the hook
redirects to a wrapper, and **the wrapper is the feature**. Settle this contract
before choosing a single default pattern.

**The naive pipeline is wrong and must not be used.** Appending
`2>&1 | grep … | head -100` (including the form in Anthropic's own cost docs)
breaks three things at once:

1. **Exit status becomes `head`'s.** A failing test suite reports success. This
   alone disqualifies it — the agent would proceed on red.
2. **Compound commands mis-bind.** In `a && b`, the pipe attaches to `b` only.
3. **Quoting.** Splicing an arbitrary command into a JSON string field is an
   injection surface, not just an escaping nuisance.

**The rewrite.** The hook emits:

```
'<runner>' <head-lines> <tail-lines> '<original command>'
```

**Every interpolated field is made shell-safe — not just the command.** The
emitted string is shell source, so any value spliced into it is code until
quoted. Three fields go in, and all three are handled:

| Field                | Treatment                                                                                      |
| -------------------- | ---------------------------------------------------------------------------------------------- |
| `<runner>`           | Single-quote wrapped, inner `'` → `'\''` — the **same routine** as the command                 |
| `<original command>` | Single-quote wrapped, inner `'` → `'\''`                                                       |
| `<head>` / `<tail>`  | Validated as non-negative integers; a non-integer disables rewriting rather than interpolating |

The runner path comes from `paths.Paths.Config.Devgeta`, which resolves through
`GetConfigDir` — and that reads `XDG_CONFIG_HOME` directly (`paths.go:412`),
falling back to `~/.config`. So it is arbitrary user-controlled input: a space
(`/Users/Some Name/.config/...`) breaks argument splitting, and a `;`, backtick,
or `$(…)` turns a config path into executable shell. Unquoted interpolation here
is the same defect class as the naive pipeline this step already bans, reached
by a different route.

Head and tail are included because they are interpolated too. They are generated
by devgeta today, but the sidecar is a plain file a user can edit, and "our own
generated value" is not a security property — validating is one comparison.

**One escaping routine per language, shared, not reimplemented.** Add
`devgeta_shell_quote()` to `configs/claude/lib/segments.sh` (already sourced by
this hook for splitting) and export a `shellQuote` counterpart from the OpenCode
plugin side, following the `splitCommandSegments` precedent that `secret-guard.js`
already imports. Note the ADR-0006 loader constraint: every export in a plugin
file is invoked as a plugin, so `shellQuote` must tolerate being called with an
arbitrary `ctx` and satisfy `plugin-loader-safety.test.mjs`. Two call sites per
hook using one routine cannot drift the way two hand-written escapes would.

**Tests:** hostile paths containing a space, a single quote, `$`, a semicolon,
and a backtick — asserting both the **exact emitted string** and that executing
it end to end runs the original command and returns its real exit status. An
exact-string assertion alone would not catch a path that quotes correctly but
executes wrongly; an execution test alone would not pin the emitted form. Both,
in both hooks.

**`<runner>` is never hardcoded in either hook.** Both resolve it from the
generated sidecar (Step 5), which is the single artifact that knows where the
runner was deployed:

| Hook               | Resolution                                                                                                                                                                                                                    |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `output-budget.sh` | `jq -r '.runner' "$SIDECAR"` — `jq` is already a hard dependency of all five shipped Claude hooks (`task-redirect.sh`, `secret-guard.sh`, `suppression-guard.sh`, `agent-config-guard.sh`, `format.sh`), so this adds nothing |
| `output-budget.js` | the `runner` field of the sidecar object it already parses for the gate                                                                                                                                                       |

A hardcoded `~/.claude/output-budget-run.sh` — which an earlier draft of this
step emitted — breaks every OpenCode-only installation, because that path only
exists when Claude is configured. Resolving through the sidecar is what makes
the agent-neutral deployment in Step 5 actually reachable.

If the sidecar is absent or its `runner` names a nonexistent path, the hook
rewrites nothing (Step 5's failure table). **Test the exact emitted command
string for both agents**, not just that a rewrite happened — an assertion on
"was rewritten" would have passed against the hardcoded path.

**The wrapper's contract**, each clause testable:

| Clause        | Behavior                                                                                                                                                                                               |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Execution     | `bash -c "$cmd"` — the whole string, so `&&`, `\|\|`, `;`, and pipes keep their original semantics. Not `eval`                                                                                         |
| Exit status   | Captured immediately after the run and re-raised as the wrapper's own exit code. **Non-negotiable**                                                                                                    |
| Allocation    | One `task-*` directory per invocation via the existing `dg task scratch` mechanism — **not** a bare file at the scratch root. See the lifecycle note below                                             |
| Capture       | stdout and stderr both redirected to one file **inside that directory** (ADR-0015), preserving interleave order                                                                                        |
| Under the cap | File replayed verbatim. Byte-identical to running the command unwrapped                                                                                                                                |
| Over the cap  | First `<head-lines>`, a marker line, last `<tail-lines>` — tail matters because failures land at the end                                                                                               |
| Marker        | Names the omitted line count **and the absolute path to the full output**                                                                                                                              |
| Full output   | Left on disk under the scratch root, which `settings.json.tmpl` already lists in `additionalDirectories` — so the agent can `grep` it with no permission prompt and **without re-running the command** |
| Cleanup       | Falls to the existing `task-*` directory reaper; no new lifecycle. Requires the Allocation clause above to hold                                                                                        |
| Escape hatch  | `DEVGETA_OUTPUT_BUDGET=off` in the environment makes the wrapper a pass-through, and the hook skips rewriting entirely                                                                                 |

That "full output on disk" clause is what makes this safe. ADR-0031's stated
risk is that a lossy cut costs more than it saves when the agent re-runs the
command; here nothing is lost, so the recovery is a targeted `grep` on a file
rather than a second full run.

**Scratch lifecycle — the allocation shape is load-bearing.** Both reapers
match on a directory carrying `paths.ScratchAllocPrefix` (`"task-"`):
`MaintainScratchDir` skips any entry failing `entry.IsDir() &&
strings.HasPrefix(entry.Name(), ScratchAllocPrefix)` (`baseapp/configure.go:86`),
and `ScratchClean` refuses anything that is not a direct child of the scratch
root carrying that prefix (`task/scratch.go:150`). So a file written straight to
the scratch root is **never** collected — it would leak one file per wrapped
command, forever, in a feature that runs on every test invocation. The runner
therefore allocates through the same path `dg task scratch` uses
(`os.MkdirTemp(root, ScratchAllocPrefix+"*")`, `task/scratch.go:25`) and writes
inside it. This is reuse of an existing owned lifecycle, not a new one — add a
test asserting the output path is a direct `task-*` child of the scratch root,
so a later refactor cannot quietly reintroduce the leak.

**Known limitations — document, do not paper over:**

- stdout and stderr are merged; a caller that needs them separated loses that.
- Output no longer streams — it appears when the command finishes. Fine for test
  and build runs, wrong for anything long-running and watched.
- Anything needing a TTY must not be matched. Default patterns cover
  non-interactive runners only.

**Then the hook itself:**

- Match the command against the rules in the sidecar (schema below); source
  `lib/segments.sh` for splitting rather than re-implementing it.

**Rule schema — one source, generated into the sidecar.** "Config-driven with
general defaults" is not a specification: two independent implementations would
invent different matchers and different caps. The rules are therefore **defined
once in Go**, rendered into `agent-runtime.json` by `EnsureAgentRuntime`, and
both hooks consume that generated array. Neither hook contains a pattern
literal.

```json
{
  "outputBudget": true,
  "runner": "/abs/path/to/output-budget-run.sh",
  "rules": [
    { "name": "go-test", "match": ["go", "test"], "head": 30, "tail": 120 },
    {
      "name": "cargo-test",
      "match": ["cargo", "test"],
      "head": 30,
      "tail": 120
    },
    { "name": "pytest", "match": ["pytest"], "head": 30, "tail": 120 },
    { "name": "npm-test", "match": ["npm", "test"], "head": 30, "tail": 120 },
    { "name": "npm-run", "match": ["npm", "run"], "head": 30, "tail": 100 },
    { "name": "make", "match": ["make"], "head": 20, "tail": 100 },
    {
      "name": "cargo-build",
      "match": ["cargo", "build"],
      "head": 20,
      "tail": 100
    },
    { "name": "gradle", "match": ["gradle"], "head": 20, "tail": 100 },
    { "name": "maven", "match": ["mvn"], "head": 20, "tail": 100 }
  ]
}
```

| Field   | Meaning                                                                         |
| ------- | ------------------------------------------------------------------------------- |
| `name`  | Stable identifier. Appears in the truncation marker and is what tests assert on |
| `match` | **Token prefix**, not a regex — see below                                       |
| `head`  | Lines kept from the start                                                       |
| `tail`  | Lines kept from the end. Larger than `head` throughout: failures land last      |

**`match` is a token prefix, deliberately not a regex.** Bash EREs and
JavaScript `RegExp` differ in escaping, character classes, and anchoring, so the
same pattern string can match differently in the two hooks — the exact
divergence [CLAUDE.md §12](../../../CLAUDE.md#keeping-the-two-ai-agents-in-sync)
warns about, where a rule is identical in both configs and still enforces
something different. A rule matches when the command segment's leading tokens
equal `match` element for element, after `lib/segments.sh` splitting and after
skipping env-var assignments and the global options `lib/segments.sh` already
recognizes. Equality of string arrays behaves identically in bash and JS; regex
does not.

**Precedence: first match in array order wins.** Not longest-match, not
most-specific — array order is the Go slice order, so it is stable, obvious in
the generated file, and trivially identical in both implementations. `npm test`
precedes `npm run` in the array for that reason.

**Malformed rules disable rewriting entirely** — the whole array, not just the
bad entry. Skipping individual bad entries would require both implementations to
agree, field by field, on what "bad" means, which reintroduces exactly the
divergence the token-prefix decision removes. All-or-nothing is one comparison
and is trivially identical across the two. It also fails in the safe direction:
no rewriting means commands run untouched.

**v1 ships built-in defaults only.** No user-defined rules, no settings surface
for the array. The schema is designed so a user-supplied list can be merged in
later without a format change, but building that now is speculative work
([CLAUDE.md §6](../../../CLAUDE.md#coding-standards)) and would need its own
design for precedence between user and built-in entries. This corrects Scope A's
earlier "config-driven rules" phrasing, which implied a user surface that is not
in this cycle.

The defaults above are general-purpose runners across ecosystems, with no
devgeta-specific or Go-specific bias — this ships to strangers
([CLAUDE.md §12](../../../CLAUDE.md#anything-we-ship-is-built-for-strangers)).
Deliberately **not** included: `tail`/`cat` of log files (frequently the whole
point of the command), anything interactive, and anything with a TTY
requirement.

- **Hook ordering with `rtk`:** both are `PreToolUse`/`Bash` and both rewrite
  `updatedInput.command`. Decide and test the composition — the intended
  behavior is that a command rtk has already rewritten into a compact form is
  _not_ matched as verbose, so ordering is observable. Register after rtk's
  block and add a test for both enabled together.
- Verify: run the script by hand against captured payloads, including a
  deliberately failing command to prove the exit status survives.

#### Step 5: `output-budget.js` mirror + the two gates

The two agents deploy differently, so one opt-in field (A1) needs two
mechanisms. This asymmetry is deliberate and gets a comment in both files:

| Agent    | Deployment                                                                     | Gate                                                                                             |
| -------- | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| Claude   | `settings.json.tmpl` rendered per install                                      | Render-time: `{{if .OutputBudget}}` — when off, the hook is not registered and no process spawns |
| OpenCode | `configs/opencode/plugin/` copied wholesale (`opencode.go:162`), always loaded | Run-time: the plugin reads the generated runtime sidecar (below) and returns a no-op when off    |

Selective copying was rejected: it would need a delete path to remove a stale
plugin on disable, with worse failure modes than an early return.

**Correction to an earlier draft of this step**, recorded so it is not
reintroduced: it claimed "there is no OpenCode template to gate on." That is
wrong — `configs/opencode/opencode.json.tmpl` exists and is rendered through
`files.GenerateFromTemplate` (`opencode.go:140`). It is still not the right
place for this flag: `opencode.json` is validated against OpenCode's own schema,
so an unrecognized devgeta key risks rejection by a tool we do not control. The
sidecar below gets the same render-time resolution without touching a
schema-governed file.

**The runtime sidecar — solves both the gate and the runner path.**

`<paths.Paths.Config.Devgeta>/agent-runtime.json`:

```json
{ "outputBudget": false, "runner": "/abs/path/to/output-budget-run.sh" }
```

**One writer, called by both configure paths.** The sidecar lives in
devgeta's own config directory and both agents read it, so it is shared state —
and shared state written by only one of two callers goes stale. A single
`baseapp` helper does both jobs together:

```go
// EnsureAgentRuntime deploys the runner and writes the sidecar describing it.
// Called by BOTH claude and opencode configure paths.
func EnsureAgentRuntime(gc *config.GlobalConfig) error
```

- It resolves the setting from `gc`, deploys the runner (the Step 5 deploy), and
  writes the sidecar **atomically** — temp file plus rename, the same discipline
  `branchstore` uses in Step 1 — so a concurrent reader never sees a partial
  file. A hook firing during `dg configure` is an ordinary race here, not a
  hypothetical.
- Because either configure path writes the whole file, **running either one
  converges both agents.** `dg configure claude --force` alone no longer leaves
  OpenCode reading a stale flag, which is what the split-ownership draft did.
- Deploy and sidecar are in one function on purpose: they cannot disagree about
  the runner's path if the same call produces both.

`dg config set` deliberately does **not** write the sidecar. The registry's
`Set` functions only mutate `gc`, and reaching from a settings entry into agent
deployment would couple two layers that are otherwise independent. Storing the
value and rendering it stay separate steps, exactly as they already are for
`settings.json` — which is why Step 3b spells out the re-render.

- Tests: configure claude-then-opencode and opencode-then-claude both end in
  identical sidecar contents; `set true`, `set false`, and `unset` each converge
  after a single configure of **either** agent; the written file is valid JSON
  with an absolute `runner` that exists.

The plugin reads it with `readFileSync` + `JSON.parse` — both Node built-ins,
no dependency and no subprocess. This follows existing practice rather than
inventing it: `task-redirect.js:65` already imports `existsSync`/`readFileSync`
from `node:fs` and reads a file synchronously in the same hot path
(`task-redirect.js:96` reads `go.mod` per invocation).

Rejected alternatives, for the record: parsing `global_config.yaml` directly
(Node has no built-in YAML parser, and adding one violates
[CLAUDE.md §6](../../../CLAUDE.md#coding-standards)'s no-new-dependency rule),
and spawning `dg config get` per Bash call (a process spawn and a second failure
mode on every tool call, in a feature meant to reduce cost).

**Failure behavior — off, in every degenerate case.** Sidecar missing,
unreadable, malformed JSON, missing key, wrong type, or naming a `runner` path
that does not exist: the plugin returns the command **unmodified**. Off means no
rewriting, so the command runs exactly as it would with no plugin at all — the
safe direction, and the one that matches the feature's default. Every one of
those cases gets a test in Step 6.

**The runner is agent-neutral.** `output-budget-run.sh` deploys to
`paths.Paths.Config.Devgeta` (`~/.config/devgeta/`), **not** `~/.claude/`, and
**both** `internal/apps/claude` and `internal/apps/opencode` ensure it exists
during configure. An earlier draft had only `claude.go` deploy it, which meant a
user who configured OpenCode without ever installing Claude got a plugin
rewriting commands to a script that does not exist. Extract one
`baseapp`-level helper both installers call rather than duplicating the deploy.

Note this is **not** the kind of asymmetry
[CLAUDE.md §12](../../../CLAUDE.md#keeping-the-two-ai-agents-in-sync) forbids:
the policy is identical and driven by one field, only the enforcement point
differs. `internal/apps/opencode/permissions_test.go` compares permission
strings and is unaffected; the equivalence that needs a test here is behavioral,
in Step 6.

- Mirror the bash exactly. Watch the OpenCode loader rule (every export is
  invoked as a plugin — ADR-0006) and the `plugin-loader-safety.test.mjs`
  contract.
- Deploy `output-budget.sh` to `~/.claude/` (Claude-specific hook) via
  `claude.go`; deploy `output-budget-run.sh` to `paths.Paths.Config.Devgeta`
  via the shared helper both installers call. One runner, no agent-specific
  behavior in it.
- Verify: `dg configure claude --force && dg configure opencode --force`, then
  `/hooks` shows it; flip the field and confirm both go quiet.
- **Verify the OpenCode-only path explicitly**: on a machine (or sandbox) with
  no Claude config at all, `dg configure opencode --force` must produce a
  working runner and a valid sidecar. This is the case the earlier draft broke.

#### Step 6: Hook behavioral tests, both languages

- Go tests in the root package mirroring `secret_guard_test.go`; Node tests
  mirroring `secret-guard.test.mjs`.
- Cover, per the Step 4 contract — each clause is a test:
  - a **failing** command wrapped by the runner still exits non-zero (the
    regression that makes this feature dangerous if wrong — write it first)
  - output under the cap is byte-identical to running unwrapped
  - output over the cap has head, marker, and tail, and the marker names a path
    that exists and holds the complete output
  - a compound command (`a && b`, `a; b`) keeps its semantics and caps as a
    whole
  - a command containing single quotes, `$`, and backticks survives the rewrite
    unchanged
  - an unmatched command passes through byte-identical
  - `DEVGETA_OUTPUT_BUDGET=off` is a true pass-through
  - the gate-off path is a true no-op under **both** agents — the behavioral
    equivalence that Step 5's asymmetric gating makes necessary
  - **sidecar degradation, one test each**: file absent, unreadable, malformed
    JSON, `outputBudget` key missing, key present but wrong type, and `runner`
    naming a nonexistent path. All six return the command unmodified
  - the runner exists at `paths.Paths.Config.Devgeta` after
    `dg configure opencode --force` with **no Claude config present**
  - **rule-decision parity, table-driven over every built-in rule**: for each
    rule, a matching command and a near-miss (e.g. `npm testx`, `go tests`), the
    bash hook and the JS hook produce the **same** decision and the **same**
    emitted command. Drive both suites from the same case table so a rule added
    in Go without a mirrored decision fails the build
  - precedence: `npm test` selects `npm-test`, not `npm-run`
  - env-var prefixes and global options don't defeat a match
    (`CI=1 go test ./...`)
  - a malformed `rules` array disables rewriting entirely, under both hooks
  - **hostile config paths**, both hooks: runner path containing a space, a
    single quote, `$`, `;`, and a backtick — exact emitted string asserted
    **and** executed end to end, returning the original command's exit status
  - non-integer `head`/`tail` in the sidecar disables rewriting rather than
    interpolating the value
  - rtk enabled together with this hook: composition is what Step 0 determined
- Verify: `go test .` and `node --test output-budget.test.mjs`

#### Step 7: `dg task context-report`

A report that measures the wrong files is worse than none — it produces
precise-looking numbers nobody can act on. So the discovery list is part of the
spec, and Step 0 has already verified it against upstream docs.

**Claude Code:**

| Layer    | Paths                                                                                                                               |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| Memory   | `~/.claude/CLAUDE.md`, `<repo>/CLAUDE.md`, `<repo>/.claude/CLAUDE.md`, plus every file reached transitively through `@path` imports |
| Settings | `~/.claude/settings.json`, `<repo>/.claude/settings.json`, `.claude/settings.local.json`                                            |
| Skills   | `~/.claude/skills/*/SKILL.md`, `<repo>/.claude/skills/*/SKILL.md`                                                                   |
| Commands | `~/.claude/commands/**`, `<repo>/.claude/commands/**`                                                                               |
| Agents   | `~/.claude/agents/**`, `<repo>/.claude/agents/**`                                                                                   |
| Plugins  | `~/.claude/plugins/**`                                                                                                              |
| MCP      | `<repo>/.mcp.json`, plus `mcpServers` in any settings layer                                                                         |
| Hooks    | `hooks` entries across the settings layers                                                                                          |

**OpenCode:**

| Layer    | Paths                                                      |
| -------- | ---------------------------------------------------------- |
| Memory   | `~/.config/opencode/AGENTS.md`, `<repo>/AGENTS.md`         |
| Settings | `~/.config/opencode/opencode.json`, `<repo>/opencode.json` |
| Plugins  | `~/.config/opencode/plugin/**`                             |
| Shared   | whatever `configs/shared/` deploys into each agent's tree  |

**Rules the implementation must follow:**

- **Report per agent, not merged.** They load different trees; one combined
  number describes neither.
- **Follow imports transitively, and report the resolved set** — an unfollowed
  `@import` is the single easiest way to under-report badly. Guard against
  import cycles.
- **Apply the same precedence each agent applies.** Where a project layer
  overrides a global one, count the effective file once, not both. Where
  layers concatenate, count both. Say which rule was applied per row.
- **Skills and commands count only their always-loaded part.** A skill's body
  loads on invocation; only its frontmatter description sits in base context.
  Counting whole skill bodies would overstate by an order of magnitude and
  argue for deleting exactly the things that cost nothing.
- **Label the token figure an estimate in the output**, with the method named.
  It is a character-based approximation, not a tokenizer.

**Validation** (this is what makes the numbers trustworthy, so it is a required
step, not a nicety): run `context-report` in this repo, then compare against
`/context` in a live Claude Code session in the same repo. Record both in the
cycle doc. Any row off by more than a stated tolerance is a discovery bug, not a
rounding difference. Repeat for OpenCode with its equivalent introspection.

- Verify: `go test ./internal/tooling/contextreport/`, then the live comparison
  above.

#### Step 8: Docs

- `docs/guides/token-efficiency.md` — ordered by measured impact, with the
  reasoning and the sources.
- `docs/apps/claude.md`, `docs/spec.md`, `ROADMAP.md`, ADR index.
- Verify: read them cold; would a stranger to this repo follow them?

---

## 6. Verification Plan

### Automated Verification

```bash
go build ./...
go test ./...                      # config changes → root package is in scope
node --test *.test.mjs
make lint
```

### Manual Verification

1. `dg configure claude --force` and `dg configure opencode --force`; `/hooks`
   lists `output-budget` under PreToolUse.
2. With the gate **off**, run a large test command in a live session — output is
   byte-identical to no hook at all.
3. With the gate **on**, same command — output capped, truncation marker
   present and naming how to get the full output.
4. `dg task handoff --write --note "..."` then `--read` from a _different
   worktree of the same repo_ → same note (common-dir behavior).
5. `dg task handoff --read` on a branch with no note → clean "no note" line,
   exit 0.
6. Write a note exceeding the cap → rejected with an actionable message, nothing
   written.
7. `dg task context-report` in this repo and in a non-Go repo → sensible output
   in both.
8. Both agents behave identically in 2–3.
9. **OpenCode with no Claude installed**: `dg configure opencode --force` alone
   produces a runner at `~/.config/devgeta/` and a valid sidecar; a wrapped
   command works end to end.
10. **Degraded sidecar**: delete it, then corrupt it with invalid JSON. Both
    times commands run unmodified, with no error surfaced to the agent.
11. **Configure order**: `dg config set agent.output_budget true`, then
    `dg configure claude --force` **only**. OpenCode must already be enabled —
    the sidecar is shared and either path writes it. Repeat with `opencode`
    only, and with `false` and `unset`.
12. **Emitted command**: with the feature on, confirm the rewritten command
    names the runner under `~/.config/devgeta/` under both agents — not
    `~/.claude/`.

### Regression Check

- `dg task review-note` / `review-notes` / `review-run` unchanged after the
  Step 1 extraction — this is the highest-risk part of the cycle.
- Existing hooks still fire: stage a secret → denied; add a lint-suppression
  comment → denied; `devgeta task`-redirected commands still redirect.
- `dg install`, `dg version`, `dg wt` unaffected.

---

## 7. Risks & Trade-offs

| Risk                                                                | Likelihood | Mitigation                                                                                                                                                   |
| ------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Step 1 extraction silently changes review-journal behavior          | **Med**    | Existing reviewjournal tests pass **unmodified**; editing one is the signal it broke                                                                         |
| Output filter cuts something load-bearing → agent re-runs, net loss | **Med**    | Conservative defaults, mandatory truncation marker, gate defaults off                                                                                        |
| Bash/JS mirrors drift                                               | Med        | Paired behavioral suites, same cases both sides — the ADR-0006 discipline                                                                                    |
| Two hooks match or cap differently for the same command             | **Med**    | Rules defined once in Go and generated; token-prefix matching, not regex (no ERE-vs-JS dialect gap); parity test over every built-in rule (Step 4/6)         |
| A config path with spaces or metacharacters breaks or executes      | **Med**    | Every interpolated field quoted or integer-validated by one shared routine per language; hostile-path tests assert emitted string **and** execution (Step 4) |
| Hook rewriting breaks an unrelated command                          | Low        | Pass-through must be byte-identical; explicit test for it                                                                                                    |
| Handoff note goes stale and misleads a fresh session                | Med        | Stamp `head` on write; `--read` reports drift from current head                                                                                              |
| Token estimate in context-report read as exact                      | Low        | Label it an approximation in the output itself                                                                                                               |
| **Wrapped command swallows a failure's exit status**                | **Med**    | Explicit contract clause; test written before the feature; naive pipeline banned in Step 4                                                                   |
| `context-report` misses a load path → confidently wrong numbers     | **Med**    | Step 0 verifies discovery upstream; Step 7 validates against live `/context`                                                                                 |
| OpenCode has no command-rewriting hook at all                       | Med        | Step 0 answers first; fallback rule follows it — Claude-only, recorded in agent-sync.md's accepted differences                                               |
| Runner leaks one output file per wrapped command                    | **Med**    | Allocate a `task-*` dir via the existing scratch mechanism; test asserts the path shape (Step 4)                                                             |
| Feature ships with no way to turn it on or off                      | **Med**    | Settings-registry entry with `Set` **and** `Unset`, not a one-way configure part (Step 3b)                                                                   |
| OpenCode rewrites commands to a runner that was never deployed      | **Med**    | Runner lives in `Config.Devgeta`, deployed by a shared helper both installers call; OpenCode-only path tested (Steps 5–6)                                    |
| Sidecar missing or malformed silently breaks every Bash call        | **Med**    | All six degenerate cases return the command unmodified; one test each (Step 6)                                                                               |
| Configure order leaves the two agents on different effective state  | **Med**    | One `EnsureAgentRuntime` writer called by both paths; both-orders convergence test (Step 5)                                                                  |
| A hook reads the sidecar mid-`dg configure` and sees a partial file | Low        | Atomic temp-plus-rename write, same discipline as `branchstore` (Step 1)                                                                                     |
| Two `PreToolUse` Bash hooks (rtk + this) rewrite the same field     | Med        | Step 0 determines chaining; explicit both-enabled test in Step 6                                                                                             |
| Wrapper breaks TTY / streaming commands                             | Low        | Defaults match non-interactive runners only; limitation documented in Step 4                                                                                 |

### Trade-offs Made

- **Hook defaults off.** Costs adoption, but a filter that surprises someone by
  eating output they needed is worse than one they turned on deliberately. See
  Open Question 1 — this is the call most worth challenging.
- **Handoff written explicitly, not on `SessionEnd`.** ADR-0032 §4. Devgeta
  usually prefers structural over conventional; this is the documented
  exception, and the reasoning is in the ADR.
- **Extract storage rather than copy it.** More upfront risk than a copy, but a
  second per-branch store diverging from the first is the defect
  [CLAUDE.md §6](../../../CLAUDE.md#coding-standards) tells us to fix in the PR
  that introduces the second use.
- **Three features, one cycle.** They share one design decision and one
  verification story. Splitting them would fragment both.

---

## 8. Decisions taken, and the one question left

The enablement _mechanism_ is now specified (Scope A1, Step 5) independently of
the default _value_ — those were conflated in the first draft, which is what
made the gate look undesigned.

**Decided — implement as written:**

1. **`dg task handoff` does not call a model.** The command takes
   `--note`/`--note-file` from whoever runs it; the agent calls it at a
   checkpoint, or a human writes one by hand. No existing `dg task` makes a
   model call, and adding the first one here would put a token cost inside the
   feature whose purpose is reducing token cost. Self-summarizing is a skill's
   job, not a task's, and a skill can already compose the two.

2. **The runner, not the hook, does the capping**, and the naive pipeline is
   banned outright (Step 4). This was implicit and is now a contract.

3. **The gate is one config field with two enforcement points** (Step 5), not
   one mechanism forced onto both agents.

**Still open — needs your call before Step 4 ships, not before it starts:**

4. **Does the output-budget hook default on or off?** Drafted **off**. Off is
   safe but most users never find it; on delivers the savings by default and
   risks a surprising cap on someone's machine. The Step 4 contract weakens the
   argument for "off" — full output stays on disk at a path the agent can
   already read, so the failure mode is a redirect, not a loss. Still leaning
   off for v1 to match the call ROADMAP already records for the rtk hook, then
   revisiting with real usage. **Nothing in Steps 0–3 or 6–7 depends on the
   answer**; only the default in Step 5's template does.

---

## 9. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope actually locked?
- [ ] Steps actionable (5–15 min each, clear success criteria)?
- [ ] Verification executable?
- [ ] Risks realistic?
- [ ] **Principle 8 check:** does every shipped artifact here make sense to
      someone who has never seen this repo?

**Reviewer notes:**
(Fill in during review.)

---

## Notes for Implementers

- **The two ADRs are binding.** If an implementation detail wants a proxy or an
  auto-injected memory, it is out of scope by decision, not by omission.
- **Commit after each step** once its verify check passes.
- **Step 1 before anything else.** Two steps depend on it and it carries the
  regression risk.
- **Every hook change is two files.** Claude and OpenCode, always, with paired
  tests — `internal/apps/opencode/permissions_test.go` and the mirror discipline
  in [CLAUDE.md §12](../../../CLAUDE.md#keeping-the-two-ai-agents-in-sync).

[headroom]: https://github.com/headroomlabs-ai/headroom
