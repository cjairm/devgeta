# Cycle: Token and context efficiency

**Date:** 2026-08-25
**Estimated Duration:** ~10–14 hours
**Status:** Approved (2026-08-26) — ready to implement, no work started
**Review:** nine cross-model rounds settled, n1–n14 (n10 rejected with reasoning)
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
      [docs/guides/output-budget-runner.md](../../guides/output-budget-runner.md)
      already exists and holds the Scope A contract; on completion its status
      header flips from "not implemented" to describing shipped behavior.

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
| Modify | `docs/guides/output-budget-runner.md`                                           | Flip the status header once Scope A ships; the contract itself is already written                                |

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

**The cap is a number, not an intention.** ADR-0032 §2 makes the cap the
mechanism that stops the note becoming the per-request tax the ADR exists to
avoid. Left unspecified, neither the public contract nor the cap-exceeded test
can be written, and each would invent its own answer:

| Clause       | Value                                                                                 |
| ------------ | ------------------------------------------------------------------------------------- |
| `MaxBytes`   | `8 * 1024`                                                                            |
| Measured on  | UTF-8 byte length of the **fully rendered file**, front matter included               |
| Over the cap | `ErrNoteTooLarge`; nothing is written and the previous note is left exactly as it was |
| Never        | Silent truncation — the failure ADR-0032 §2 names outright                            |

8 KiB is roughly 2,000 tokens, about four times ADR-0032's "500-token note"
framing: generous enough that an honest checkpoint never reaches it, small
enough to be a real ceiling. 16 KiB was considered and rejected — at ~4,000
tokens read into every fresh session, the note starts becoming the cost it was
meant to replace. Measuring the rendered file rather than the body means front
matter cannot be used to slip past the cap. The value is one constant in one
package, so revising it later is a one-line change, not a format change.

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
redirects to a wrapper, and **the wrapper is the feature**.

**The contract lives in
[docs/guides/output-budget-runner.md](../../guides/output-budget-runner.md) and
is binding.** It carries the argv shape, the sidecar schema, the shell-quoting
rule, the capture bound and its rejected alternatives, the reduction order and
marker reserves, the numeric-width contract, the tokenization rule, and the
conformance test matrix — with the measurements behind each. It is a separate
document because it outlives this cycle: it describes the behavior of three
shipped artifacts, while this step only describes building them.

Do not restate any of it here. Every number and rule has exactly one home, which
is what keeps the two hooks from disagreeing.

**The invariants this step must not violate** — each one has a section in the
guide and a test in its §10:

| Invariant                                                                                    | Guide |
| -------------------------------------------------------------------------------------------- | ----- |
| The wrapped command's own exit status is what the wrapper returns                            | §3    |
| The naive `\| head` pipeline is banned outright                                               | §1    |
| Nothing devgeta interpolates into shell reaches it unquoted or unvalidated                   | §2.1  |
| The runner path is resolved from the sidecar, never hardcoded                                | §2.2  |
| The capture is bounded, and bounding it never kills or blocks the command                    | §4    |
| Markers and notices count **inside** the budgets they report                                 | §6    |
| Every transported integer matches `^[1-9][0-9]{0,14}$`, checked before any arithmetic         | §5    |
| Both hooks reach the same decision and emit the same command for the same input              | §8    |
| Every degenerate sidecar case leaves the command unmodified                                  | §5.4  |

**Build order:**

1. `internal/apps/baseapp/outputrules.go` — the rule table and the derived
   limits, with the generation-time invariants (guide §5.4, §8.1).
2. `configs/claude/output-budget-run.sh` — the runner. Write the failing-command
   exit-status test first (guide §10).
3. `configs/claude/output-budget.sh` — matching and the rewrite, sourcing
   `lib/segments.sh` for segmentation and adding `devgeta_shell_quote()`.

**Scope decisions that belong to this cycle, not the guide:**

- **v1 ships built-in defaults only.** No user-defined rules, no settings surface
  for the array. The schema is designed so a user-supplied list can be merged in
  later without a format change, but building that now is speculative work
  ([CLAUDE.md §6](../../../CLAUDE.md#coding-standards)) and would need its own
  design for precedence between user and built-in entries. This corrects Scope
  A's earlier "config-driven rules" phrasing, which implied a user surface that
  is not in this cycle.
- **Hook ordering with `rtk`:** both are `PreToolUse`/`Bash` and both rewrite
  `updatedInput.command`. Decide and test the composition — the intended
  behavior is that a command rtk has already rewritten into a compact form is
  _not_ matched as verbose, so ordering is observable. Register after rtk's block
  and add a test for both enabled together. Step 0 answers the chaining question
  first.

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
unreadable, malformed JSON, missing key, wrong type, naming a `runner` path that
does not exist, or carrying a budget that violates one of Step 4's
reserve relationships: the plugin returns the command **unmodified**. Off means
no rewriting, so the command runs exactly as it would with no plugin at all —
the safe direction, and the one that matches the feature's default. Every one of
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

**The case list is
[docs/guides/output-budget-runner.md §10](../../guides/output-budget-runner.md#10-conformance-tests).**
It sits with the contract on purpose: a clause and the test that pins it drift
apart the moment they live in different documents, which is how three of this
cycle's review findings happened.

This step is the work of implementing it:

- Go tests in the root package mirroring `secret_guard_test.go`; Node tests
  mirroring `secret-guard.test.mjs`. Both run the real scripts against real temp
  dirs — the established rigor for hooks in this repo.
- **Drive the parity groups from one shared case table**, not two hand-kept
  lists. The guide marks which groups are parity tests; for those, a rule or
  tokenization case added on one side without the other must fail the build.
- **Write the failing-command exit-status test first.** It is the regression that
  makes this feature dangerous rather than merely disappointing.
- Two cases in the guide's list are cycle-level rather than contract-level and
  belong here:
  - the gate-off path is a true no-op under **both** agents — the behavioral
    equivalence Step 5's asymmetric gating makes necessary
  - **sidecar degradation, one test each**: file absent, unreadable, malformed
    JSON, `outputBudget` key missing, key present but wrong type, `runner` naming
    a nonexistent path, and a budget violating the width contract. All seven
    return the command unmodified
  - the runner exists at `paths.Paths.Config.Devgeta` after
    `dg configure opencode --force` with **no Claude config present**
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
- `docs/guides/output-budget-runner.md` — flip the "not implemented" status
  header. Do **not** re-explain the contract anywhere else; that duplication is
  what this cycle's review rounds kept catching.
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
   present and naming how to get the full output. Then a command emitting one
   very long single line (e.g. a minified bundle to stdout): the replayed line
   is bounded, and the full line is recoverable from the file the marker names.
4. `dg task handoff --write --note "..."` then `--read` from a _different
   worktree of the same repo_ → same note (common-dir behavior).
5. `dg task handoff --read` on a branch with no note → clean "no note" line,
   exit 0.
6. Write a note exceeding 8 KiB → rejected with an actionable message, and the
   note that was already there is unchanged.
7. `dg task context-report` in this repo and in a non-Go repo → sensible output
   in both.
8. Both agents behave identically in 2–3.
9. **OpenCode with no Claude installed**: `dg configure opencode --force` alone
   produces a runner at `~/.config/devgeta/` and a valid sidecar; a wrapped
   command works end to end.
10. **Degraded sidecar**: delete it, then corrupt it with invalid JSON. Both
    times commands run unmodified, with no error surfaced to the agent.
11. **Runaway output**: with the feature on, run a matched command that emits
    far past `maxCaptureBytes` (e.g. `make` wrapping a loop that prints
    endlessly). The scratch file stops at the capture limit, the command still
    finishes with its own exit status, and both the file and the marker say the
    capture is incomplete. Check disk use before and after.
12. **Configure order**: `dg config set agent.output_budget true`, then
    `dg configure claude --force` **only**. OpenCode must already be enabled —
    the sidecar is shared and either path writes it. Repeat with `opencode`
    only, and with `false` and `unset`.
13. **Emitted command**: with the feature on, confirm the rewritten command
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

| Risk                                                                                                                                                            | Likelihood | Mitigation                                                                                                                                                    |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Step 1 extraction silently changes review-journal behavior                                                                                                      | **Med**    | Existing reviewjournal tests pass **unmodified**; editing one is the signal it broke                                                                          |
| Output filter cuts something load-bearing → agent re-runs, net loss                                                                                             | **Med**    | Conservative defaults, mandatory truncation marker, gate defaults off                                                                                         |
| Bash/JS mirrors drift                                                                                                                                           | Med        | Paired behavioral suites, same cases both sides — the ADR-0006 discipline                                                                                     |
| Two hooks match or cap differently for the same command                                                                                                         | **Med**    | Rules defined once in Go and generated; token-prefix matching, not regex (no ERE-vs-JS dialect gap); parity test over every built-in rule (Step 4/6)          |
| A config path with spaces or metacharacters breaks or executes                                                                                                  | **Med**    | Every interpolated field quoted or integer-validated by one shared routine per language; hostile-path tests assert emitted string **and** execution (Step 4)  |
| Hook rewriting breaks an unrelated command                                                                                                                      | Low        | Pass-through must be byte-identical; explicit test for it                                                                                                     |
| Handoff note goes stale and misleads a fresh session                                                                                                            | Med        | Stamp `head` on write; `--read` reports drift from current head                                                                                               |
| Token estimate in context-report read as exact                                                                                                                  | Low        | Label it an approximation in the output itself                                                                                                                |
| **Wrapped command swallows a failure's exit status**                                                                                                            | **Med**    | Explicit contract clause; test written before the feature; naive pipeline banned in Step 4                                                                    |
| `context-report` misses a load path → confidently wrong numbers                                                                                                 | **Med**    | Step 0 verifies discovery upstream; Step 7 validates against live `/context`                                                                                  |
| OpenCode has no command-rewriting hook at all                                                                                                                   | Med        | Step 0 answers first; fallback rule follows it — Claude-only, recorded in agent-sync.md's accepted differences                                                |
| Runner leaks one output file per wrapped command                                                                                                                | **Med**    | Allocate a `task-*` dir via the existing scratch mechanism; test asserts the path shape (Step 4)                                                              |
| Feature ships with no way to turn it on or off                                                                                                                  | **Med**    | Settings-registry entry with `Set` **and** `Unset`, not a one-way configure part (Step 3b)                                                                    |
| OpenCode rewrites commands to a runner that was never deployed                                                                                                  | **Med**    | Runner lives in `Config.Devgeta`, deployed by a shared helper both installers call; OpenCode-only path tested (Steps 5–6)                                     |
| Sidecar missing or malformed silently breaks every Bash call                                                                                                    | **Med**    | All seven degenerate cases return the command unmodified; one test each (Step 6)                                                                              |
| Configure order leaves the two agents on different effective state                                                                                              | **Med**    | One `EnsureAgentRuntime` writer called by both paths; both-orders convergence test (Step 5)                                                                   |
| A hook reads the sidecar mid-`dg configure` and sees a partial file                                                                                             | Low        | Atomic temp-plus-rename write, same discipline as `branchstore` (Step 1)                                                                                      |
| Two `PreToolUse` Bash hooks (rtk + this) rewrite the same field                                                                                                 | Med        | Step 0 determines chaining; explicit both-enabled test in Step 6                                                                                              |
| Wrapper breaks TTY / streaming commands                                                                                                                         | Low        | Defaults match non-interactive runners only; limitation documented in Step 4                                                                                  |
| **A line cap lets unbounded bytes through** — one huge line, or a few large ones                                                                                | **Med**    | `maxLineBytes` + `maxTotalBytes` with a stated order of operations; property test that no reduced result exceeds `maxTotalBytes` (Steps 4/6)                  |
| **Bash and JS tokenize the same command differently** — `segments.sh` splits compound commands, it does not tokenize                                            | **Med**    | Neither hook parses shell: whitespace-only split plus refuse-on-quoting for the compared prefix; shared fixture table drives both suites (Steps 4/6)          |
| Refuse-on-quoting under-matches a command the user expected to be capped                                                                                        | Low        | Fails safe — the command runs correctly and in full; the refused prefixes are listed in Step 4                                                                |
| **A runaway matched command fills the user's disk** — the wrapper invents a disk write that did not exist unwrapped, and the reaper is configure-time only      | **Med**    | `maxCaptureBytes` 16 MiB enforced by a drain that never blocks or signals the child; tests assert bounded file size **and** surviving exit status (Steps 4/6) |
| Enforcing the capture bound kills the command instead of capping it                                                                                             | **Med**    | `ulimit -f` and bare `head -c` both rejected for exactly this; `${PIPESTATUS[0]}` with `pipefail` off, drain-after-limit (Step 4)                             |
| Markers push the result past the cap they were just counted against                                                                                             | **Med**    | Both budgets reserve their marker before selecting content; property tests run with a long scratch path and a large omitted count (Steps 4/6)                 |
| Agent greps a capture-truncated file and trusts the absence of a match                                                                                          | Med        | Notice in the file **and** in the replay marker; documented as the one case where "nothing is lost" does not hold (Step 4)                                    |
| Runner cannot tell that the capture was capped at all                                                                                                           | **Med**    | Read one byte past the content budget; a file at `probe_limit` proves the cap was reached (Steps 4/6)                                                         |
| **Notice claims confirmed loss when nothing was discarded** — output of exactly `content_limit + 1` bytes                                                       | **Med**    | Flag named `capture_capped`; both notices state the cap, never truncation; boundary tests assert the wording at `content_limit + 1` (Steps 4/6)               |
| **A nonpositive budget reaches `head -c`** — an error on BSD, a silent unbounded buffer on GNU | **Med** | Positivity checked in `EnsureAgentRuntime`, in both hooks before rewriting, and in the runner; zero and negative tests per budget (Steps 4/6) |
| A hook or the runner hardcodes a marker reserve and they drift apart | Low | Reserves never leave Go; the sidecar transports limits already net of them, so there is no constant to duplicate (Step 4) |
| Devgeta itself writes a wrong-but-positive derived limit | Low | `EnsureAgentRuntime` asserts each derived limit equals its expected derivation before writing — Go-side, no hook constant (Step 4) |
| **Bash and JS disagree on a large transported integer** — JS rounds above 2^53 and renders `1e+21`; bash `+1` wraps, at 2^63 to a negative | **Med** | 15-digit decimal-width contract checked against the rendered string in both hooks, the runner, and `EnsureAgentRuntime`; cross-hook parity table (Steps 4/6) |

### Trade-offs Made

- **Hook defaults off.** Costs adoption, but a filter that surprises someone by
  eating output they needed is worse than one they turned on deliberately. See
  §8's open question — this is the call most worth challenging.
- **Handoff written explicitly, not on `SessionEnd`.** ADR-0032 §4. Devgeta
  usually prefers structural over conventional; this is the documented
  exception, and the reasoning is in the ADR.
- **A user-raised capture limit is honored, not clamped — inside the width
  contract.** The shipped 16 MiB is a default, not a policy enforced against the
  machine's owner; 15 decimal digits is not a policy either, it is where bash and
  JavaScript stop agreeing. Clamping
  would mean the literal living in bash and JS as well as Go, and a default that
  can never change. Recorded because it reads like a gap until you notice that
  "large positive" means exactly what it says, unlike zero or negative.
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

4. **The budget is bounded in bytes as well as lines** — `maxLineBytes` 2048,
   `maxTotalBytes` 65536, with the three-step reduction order pinned (Step 4).
   A line cap alone bounds nothing: one line has no newlines, and 25 lines of
   1 MB is "under" a 30-line cap.

5. **Neither hook parses shell.** Tokenization is a whitespace-only split, and
   any quote, backslash, or `$` in the tokens being compared refuses the match
   (Step 4). This replaced two rejected options: specifying a full
   quoting/escaping contract twice, and shelling out to a Go matcher on every
   Bash call — the latter being the process spawn Step 5 already rejected for
   the gate.

6. **The handoff cap is 8 KiB of rendered file** (Step 2), front matter
   included, rejecting rather than truncating.

7. **Markers are inside the budgets, not appended to them** (Step 4). Both
   caps reserve their marker before selecting content, with the inline
   reservation a constant so the rule stays one subtraction in both languages.
   The reserves stay **inside Go**: the sidecar transports
   `lineContentLimit` and `captureContentLimit` already net of them, so no hook
   and no runner holds a reserve constant, and the relationship check reduces to
   one field test at every layer: `^[1-9][0-9]{0,14}$`, which is positivity and
   the bash/JS shared numeric range in a single pattern. A type check alone admits `0`,
   which reaches `head -c -N`: an error on BSD, and on GNU a silent "all but the
   last N bytes" that buffers the whole stream and defeats the bound.

8. **The capture is bounded at 16 MiB, and bounding it must not touch the
   command** (Step 4). `ulimit -f` and a bare `head -c` were both rejected —
   each enforces the bound by killing or signalling the child, which breaks the
   exit-status clause. The accepted form caps the file and keeps draining. When
   it trips, both the file and the replay marker say the full output is
   incomplete, because "nothing is lost" stops being true there.

   Detecting that it tripped is its own decision: the runner reads **one byte
   past** the content budget, so a file at `probe_limit` proves the cap was
   reached. `head`'s exit status cannot answer this, and neither can a size
   check at the budget itself, which is ambiguous for output landing exactly on
   it. The extra byte stays in the file — it is real output, and the notice
   reserve is sized to keep it inside `maxCaptureBytes`.

   What that probe proves is "capped", **not** "lost": a command emitting
   exactly `content_limit + 1` bytes fills the probe and loses nothing. So the
   flag is `capture_capped` and both notices state the cap rather than asserting
   truncation. Getting certainty instead would mean trimming the byte back off
   (needs a non-portable in-place `truncate`) or counting the remainder through
   the drain (unsound — `head -c` may swallow a block, so the count can read 0
   when bytes really were dropped). A false "complete" is the worst failure
   available here, so the claim is narrowed to what the evidence supports.

**Still open — needs your call before Step 4 ships, not before it starts:**

9. **Does the output-budget hook default on or off?** Drafted **off**. Off is
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
