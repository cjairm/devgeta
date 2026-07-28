# Cycle: `dg task` — agent- and human-callable dev utilities

**Date:** 2026-06-18
**Estimated Duration:** ~4–5 hours
**Status:** Done

---

## 1. Domain Context

Devgeta generates a shell-integration file (`~/.local/share/devgeta/devgeta.zsh`) from
the Go `text/template` at `configs/templates/devgeta.zsh.tmpl`. Inside it, gated by the
`ExtendedCapabilities` feature flag, lives a **shell function** named `dge()` (template
lines 72–208). `dge` is a subcommand dispatcher with an allowlist guard that exposes a
handful of developer utilities:

- **Git:** `delete-branch`, `refresh-branch`, `reset-main-branch`
- **NPM:** `reinstall-libraries`, `reinstall-library <name>`
- **GitHub:** `fetch-pr-comments <repo> <pr> [file]` (a `gh api graphql` + `jq` pipeline)

**Problem.** `dge` is a shell function, so it only exists in an interactive shell that has
sourced `devgeta.zsh`. It cannot be invoked as an executable: agents (Claude Code, CI,
any non-interactive `bash -c`) cannot reliably call it, you can't `which` it, pipe to it,
or call it from another process. The logic also lives as untested template text — outside
the Go test suite this repo mandates.

**Goal.** Move the utility logic into the `dg` binary as a `dg task` command group, so the
same logic is callable by **both** agents (a real executable on `PATH`) and humans (the
`dge` function becomes a thin wrapper that forwards to `dg task`). As part of this, the
PR-comments use case is reframed: instead of porting `fetch-pr-comments`, we extend the
existing `githubcli` tool so callers can pass arbitrary GraphQL queries and get the output
back — a reusable capability rather than a one-off task.

Related docs: [CLAUDE.md](../../../CLAUDE.md) §6/§12, [docs/guides/cli-patterns.md](../../guides/cli-patterns.md),
[docs/guides/testing-patterns.md](../../guides/testing-patterns.md).

---

## 2. Engineer Context

**Relevant files and their purposes:**

- `configs/templates/devgeta.zsh.tmpl` (72–208) — current `dge()` function; will become a wrapper
- `internal/config/fromFile.go` (39–57, 381–388) — `ShellFeatures` struct + `RegenerateShellConfig()`
- `internal/config/reconcile.go` (98–123) — `ExtendedCapabilities` hardcoded `true`
- `cmd/worktree.go` — reference pattern for a parent command with subcommands, fzf selection, registration
- `cmd/root.go` — command registration (`rootCmd.AddCommand(...)`)
- `internal/tooling/terminal/dev_tools/githubcli/githubcli.go` — `GithubCli` tool; `ExecuteCommand` runs `gh` but **discards** stdout
- `internal/tooling/terminal/dev_tools/fzf/fzf.go` (93) — `SelectFromList(items []string, prompt string) (string, error)` interactive picker
- `internal/commands/base.go` (191) — `ExecCommand(CommandParams) (stdout, stderr, error)` already captures stdout

**Key functions/types involved:**

- `cobra.Command` — parent `taskCmd` + per-task subcommands
- `cmd.BaseCommandExecutor.ExecCommand` — captured-output command execution
- `fzf.SelectFromList` — interactive branch picker (replaces `fzf-tmux` in shell)
- `githubcli.GithubCli` — gains a GraphQL/output-returning method

**Testing patterns used in this area:**

- See [docs/guides/testing-patterns.md](../../guides/testing-patterns.md). Always
  `&Type{Cmd: mockApp.Cmd, Base: mockApp.Base}` (never `New()` in state-changing tests),
  `testutil.VerifyNoRealCommands(t, mockApp.Base)` against the **same** base, and
  `func init() { testutil.InitLogger() }`.
- Tests live in `*_test.go` alongside implementations.

**Commands to run tests:**

```bash
go test ./internal/tooling/...
go test ./internal/tooling/terminal/dev_tools/githubcli/
go test ./cmd/
go test ./...
make lint
```

---

## 3. Objective

Implement a `dg task <subcommand>` command group in the `dg` binary that covers the git
and npm utilities currently in `dge`, extend `githubcli` to run arbitrary GraphQL queries
and return their output, and rewrite the `dge()` shell function as a thin wrapper that
forwards to `dg task` — so the logic is callable by both agents (as an executable) and
humans (via `dge`), and is covered by the Go test suite.

---

## 4. Scope Boundary

### In Scope

- [x] `dg task` parent command + subcommands: `delete-branch`, `refresh-branch`, `reset-main-branch`, `reinstall-libraries`, `reinstall-library`
- [x] Interactive branch picker for `delete-branch` via `fzf.SelectFromList` (no `fzf-tmux` dependency)
- [x] Extend `internal/tooling/terminal/dev_tools/githubcli/githubcli.go` so callers can pass a GraphQL query (with variables) and receive stdout back
- [x] Rewrite the `dge()` function in `devgeta.zsh.tmpl` to a thin wrapper: `dge() { dg task "$@"; }` (preserving the allowlist UX, or delegating validation to Cobra)
- [x] Tests (mocked) for every `dg task` subcommand and the new githubcli method
- [x] Register `taskCmd` (in `cmd/task.go`'s own `init()`, following the same self-registering pattern as `cmd/worktree.go`, rather than in `cmd/root.go` directly)
- [x] Document `dg task` in `docs/spec.md` and README; note the `dge` → `dg task` relationship

### Explicitly Out of Scope

- Porting `fetch-pr-comments` as a `dg task` subcommand — **replaced** by the generic GraphQL capability on `githubcli`. The PR-comment fetch/`jq`-shaping pipeline is deferred; callers compose it from the GraphQL primitive for now.
- Removing the `ExtendedCapabilities` flag or restructuring the template into multiple files.
- Adding new utilities beyond the existing `dge` set.

**Scope is locked.** If something out of scope surfaces, document it here for a future cycle.

**Post-hoc note (closing this cycle):** Scope expanded during implementation beyond what's listed
above — `cmd/task_pr.go` and `internal/tooling/task/pr.go` add a full PR review workflow under
`dg task` (`review-threads`, `resolve-thread`, `unresolve-thread`, `reply-thread`, `submit-review`,
`create-pr`, `update-pr-description`, `approve-pr`, `request-changes-pr`, `comment-pr`, `merge-pr`,
`pr-view`, `pr-checks`, `current-pr`, `current-repo`), and `dg task`/`dge` output now streams in
real time (see commits `131cd30`, `d6c28a7`). This was a deliberate widening of the original
"agent-callable dev utilities" goal into a full PR workflow surface, not documented as a separate
cycle at the time. Recorded here for traceability; a future cycle doc should retroactively (or the
next time this area changes) capture the PR-workflow design decisions properly instead of relying
on commit messages alone.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                                                     | Description                                                                                                     |
| ------ | ----------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| Modify | `internal/tooling/terminal/dev_tools/githubcli/githubcli.go`                  | Add output-returning run + `GraphQL(query string, fields, intFields map[string]string)` method                  |
| Create | `internal/tooling/terminal/dev_tools/githubcli/githubcli_test.go` (or extend) | Tests for GraphQL/output method (mocked)                                                                        |
| Create | `internal/tooling/task/task.go`                                               | `TaskManager` with `DeleteBranch`, `RefreshBranch`, `ResetMainBranch`, `ReinstallLibraries`, `ReinstallLibrary` |
| Create | `internal/tooling/task/task_test.go`                                          | Mocked unit tests for each method                                                                               |
| Create | `cmd/task.go`                                                                 | `taskCmd` parent + subcommands                                                                                  |
| Create | `cmd/task_test.go`                                                            | Subcommand arg-parsing/dispatch tests (mocked)                                                                  |
| Modify | `cmd/root.go`                                                                 | Register `taskCmd`                                                                                              |
| Modify | `configs/templates/devgeta.zsh.tmpl` (72–208)                                 | Replace `dge()` body with wrapper forwarding to `dg task`                                                       |
| Modify | `configs/templates/devgeta.zsh.tmpl` (related test)                           | Update template golden/expectation tests if present                                                             |
| Modify | `docs/spec.md`, `README.md`                                                   | Document `dg task`; explain `dge` wrapper                                                                       |

### Step-by-Step

#### Step 1: Extend githubcli for GraphQL/output

- Add a method that returns captured stdout (the current `ExecuteCommand` discards it). Suggested:
  `RunWithOutput(args ...string) (string, error)` using `Base.ExecCommand` and returning stdout,
  plus a convenience `GraphQL(query string, stringVars, intVars map[string]string) (string, error)`
  that assembles `gh api graphql -f query=... -f k=v -F k=v` and returns the raw JSON.
- Keep `ExecuteCommand` as-is for callers that don't need output.
- Verify: `go build ./internal/tooling/terminal/dev_tools/githubcli/`

#### Step 2: Test githubcli GraphQL/output method

- Use `testutil` mocks; assert the assembled `gh` args (query + `-f`/`-F` flags) and that the
  mocked stdout is returned. `VerifyNoRealCommands` against the mock base.
- Verify: `go test ./internal/tooling/terminal/dev_tools/githubcli/`

#### Step 3: Create TaskManager (git + npm logic)

- Create `internal/tooling/task/task.go` with a `TaskManager{ Cmd, Base }` and methods mirroring
  the shell logic:
  - `RefreshBranch(target string)` (default `main`): checkout target → pull → checkout - → merge
  - `ResetMainBranch()`: checkout main → `reset --hard origin/main`
  - `ReinstallLibraries()`: `git clean -Xdf` → rm `node_modules/` → `npm install` → rm tsbuildinfo
  - `ReinstallLibrary(name string)` (required): rm `node_modules/<name>` → `npm install`
  - `DeleteBranch(target string)` (default `main`): checkout target → fetch → pull → list branches →
    `fzf.SelectFromList` → `git branch -D <selected>`
- All execution through `Base.ExecCommand` so it's mockable.
- Verify: `go build ./internal/tooling/task/`

#### Step 4: Test TaskManager

- One test per method: success + failure path, mocked. Validate the exact git/npm args.
- For `DeleteBranch`, inject/mocked branch list and selection (mirror the worktree fzf test approach).
- `VerifyNoRealCommands(t, mockApp.Base)`.
- Verify: `go test ./internal/tooling/task/ -v`

#### Step 5: Create cmd/task.go

- `taskCmd` parent (`Use: "task"`), one subcommand per method, following `cmd/worktree.go`.
- Arg rules: `reinstall-library` → `cobra.ExactArgs(1)`; `delete-branch`/`refresh-branch` → optional target.
- Cobra's unknown-subcommand handling replaces the shell allowlist guard.
- Verify: `go build ./cmd/`

#### Step 6: Register + cmd tests

- `rootCmd.AddCommand(taskCmd)` and attach subcommands in `cmd/task.go` init (worktree pattern).
- `cmd/task_test.go`: assert each subcommand parses args and dispatches to a mocked TaskManager.
- Verify: `go test ./cmd/ -v` and `./devgeta task --help`

#### Step 7: Rewrite dge() wrapper

- Replace the `dge()` body in `devgeta.zsh.tmpl` with a thin forwarder, e.g.:
  ```sh
  dge() { dg task "$@"; }
  ```
  Keep it inside `{{if .ExtendedCapabilities}}`. Decide whether to keep the friendly
  `valid commands are: ...` echo (call `dg task --help` on no args) — recommended for parity.
- Update any template expectation/golden tests (`internal/config`, `internal/apps/devgeta`).
- Verify: `go test ./internal/config/ ./internal/apps/devgeta/`

#### Step 8: Documentation

- Add `dg task` to `docs/spec.md` (features + subcommand table) and `README.md`.
- Note that `dge` is now a wrapper over `dg task`, and that agents should prefer `dg task` directly.
- Verify: re-read for clarity.

---

## 6. Verification Plan

### Automated Verification

```bash
go test ./internal/tooling/task/
go test ./internal/tooling/terminal/dev_tools/githubcli/
go test ./cmd/
go test ./internal/config/ ./internal/apps/devgeta/
make lint
go test ./... -cover
```

### Manual Verification

1. `make build && ./devgeta task --help` → lists all subcommands with descriptions
2. `./devgeta task reset-main-branch` (in a throwaway repo) → resets correctly or clear error
3. `./devgeta task refresh-branch` → defaults to `main`, merges into current branch
4. `./devgeta task delete-branch` → opens fzf picker, deletes selected branch
5. `./devgeta task reinstall-library lodash` (in a node project) → reinstalls just that package
6. GraphQL: call the new `githubcli` method (via a small harness or a real `gh api graphql`) → returns JSON
7. Regenerate shell config, `source ~/.local/share/devgeta/devgeta.zsh`, run `dge refresh-branch` → behaves identically to `dg task refresh-branch`

### Regression Check

- `dg install --help`, `dg worktree --help`, `dg version` still work
- Existing `dge` muscle-memory invocations still function (now via wrapper)
- No unintended changes to other generated shell content

---

## 7. Risks & Trade-offs

| Risk                                                                                  | Likelihood | Mitigation                                                                                                                 |
| ------------------------------------------------------------------------------------- | ---------- | -------------------------------------------------------------------------------------------------------------------------- |
| Interactive fzf in a binary subcommand behaves differently than `fzf-tmux`            | Med        | Reuse the proven `fzf.SelectFromList` + worktree-remove pattern; document that selection is plain fzf, not tmux-popup      |
| `dge` users relied on the allowlist echo for discoverability                          | Low        | Forward no-arg `dge` to `dg task --help`                                                                                   |
| GraphQL method API too narrow/too broad                                               | Med        | Mirror `gh api graphql` flags (`-f` string vars, `-F` typed vars); keep raw-JSON return so callers shape with their own jq |
| Template expectation tests drift                                                      | Med        | Run `internal/config` + `internal/apps/devgeta` tests in Step 7 before commit                                              |
| Destructive git ops (`reset --hard`, `branch -D`) run from a binary feel less guarded | Med        | Preserve current behavior; do not add new auto-confirmation in this cycle (note for future)                                |

### Trade-offs Made

- **GraphQL primitive vs. ported `fetch-pr-comments`:** We expose a reusable GraphQL capability on `githubcli` instead of a bespoke task. More flexible and testable; the convenience of the old one-liner (jq shaping, default output file) is deferred to callers.
- **Cobra validation vs. shell allowlist:** Drop the hand-rolled allowlist in favor of Cobra's subcommand resolution — less code, standard UX.
- **Keep `ExtendedCapabilities` flag:** Wrapper stays gated by the existing flag; no template restructuring.

---

## 8. Cross-Model Review Notes

- [x] Domain context clear?
- [x] Engineer context sufficient?
- [x] Objective unambiguous?
- [x] Scope locked (esp. fetch-pr-comments deferral)?
- [x] Steps actionable (5–15 min each)?
- [x] Verification executable?
- [x] Risks realistic (destructive git ops, fzf behavior)?

**Reviewer notes:**
Shipped across commits `317e5be`, `da3cb43`, `131cd30`, `d6c28a7`, `72bfd60`, `731e9b5`. Scope
grew to include a full PR review workflow (see post-hoc note in section 4) beyond the original
git/npm task list — see that note before treating this doc as a template for similar work.

---

## Notes for Implementers

- **This doc is the spec.** Update it if requirements change; don't expand scope silently.
- **Commit after each step** once its verify check passes.
- **Mocks only** — never execute real git/npm/gh in tests; `VerifyNoRealCommands` against the same base the code uses.
- **Verification must pass before "done"** — automated + manual + regression.
