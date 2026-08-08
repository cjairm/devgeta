# Cycle: [Short Descriptive Name]

**Date:** YYYY-MM-DD  
**Estimated Duration:** ~X hours  
**Status:** Draft | In Progress | Complete | Blocked

---

## 1. Domain Context

Brief background for someone unfamiliar with this area:

- What system/feature does this relate to? (e.g., "the installation workflow", "git worktree management", "cross-platform package support")
- What problem exists or what's being added?
- Link to any relevant docs: [docs/spec.md](../spec.md), [ROADMAP.md](../../ROADMAP.md), [CLAUDE.md](../../CLAUDE.md), or prior issues

**Example:** "The worktree feature lets developers create isolated git branches with associated tmux windows. Currently, users manually manage tmux windows — this cycle automates that."

---

## 2. Engineer Context

What the implementer needs to know:

- **Relevant files and their purposes:**
  - `internal/tooling/worktree/worktree.go` — Worktree coordination logic
  - `cmd/worktree.go` — CLI command definitions
  - `internal/commands/base.go` — Platform-agnostic command execution

- **Key functions/types involved:**
  - `WorktreeManager.Create()` — Creates worktree + tmux window
  - `cobra.Command` — Cobra command structure

- **Testing patterns used in this area:**
  - See [docs/guides/testing-patterns.md](../guides/testing-patterns.md) — always use `testutil.MockApp`, never execute real commands
  - Test files in `*_test.go` alongside implementations

- **Commands to run tests** (targeted — the packages this cycle touches and the
  ones that import them; the full `go test ./...` is the release gate, not a
  per-step run — see CLAUDE.md §6 "Which tests to run"):
  ```bash
  go test ./internal/tooling/worktree/
  go test ./cmd/
  make lint                         # Format + vet
  ```

---

## 3. Objective

One clear sentence: what does "done" look like?

**Example:** "Implement `dg worktree create <name>` command that automatically creates a git worktree and associated tmux window, so developers can manage branches without manual tmux setup."

---

## 4. Scope Boundary

### In Scope

- [ ] Specific deliverable 1 (e.g., "Implement `dg worktree create` subcommand")
- [ ] Specific deliverable 2 (e.g., "Add tests for Create, List, Remove commands")
- [ ] Specific deliverable 3 (e.g., "Update docs/spec.md with worktree feature")

### Explicitly Out of Scope

- Thing that might seem related but we're NOT doing (e.g., "Worktree auto-cleanup on shell exit")
- Another thing we're deferring (e.g., "Integrating with VS Code remote")

**Scope is locked.** If you discover something out of scope is needed, document it for a future cycle and reference here.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                    | Description                                      |
| ------ | -------------------------------------------- | ------------------------------------------------ |
| Modify | `cmd/worktree.go:50`                         | Add `worktreeCreateCmd` subcommand definition    |
| Create | `internal/tooling/worktree/worktree.go`      | New WorktreeManager type with Create/List/Remove |
| Create | `internal/tooling/worktree/worktree_test.go` | Unit tests (all mocked, no real commands)        |
| Modify | `cmd/root.go:line-X`                         | Register worktree command                        |
| Modify | `docs/spec.md`                               | Document worktree feature in Features section    |

### Step-by-Step

Each step should be completable in 5-15 minutes. Start here:

#### Step 1: Create WorktreeManager type and Create() method

- Create `internal/tooling/worktree/worktree.go` with:
  - `WorktreeManager` struct with embedded `BaseCommandExecutor`
  - `Create(name string)` method that creates worktree + tmux window
  - Use mocked command execution (see [testing-patterns.md](../guides/testing-patterns.md))
- Expected outcome: Method compiles, stubbed to return success
- Verify: `go build ./internal/tooling/worktree/`

#### Step 2: Add unit tests for Create()

- Create `internal/tooling/worktree/worktree_test.go`
- Test Create() with mocked success and error cases
- Use `testutil.NewMockApp()` for command mocking
- Always verify: `testutil.VerifyNoRealCommands(t, mockApp.Base)`
- Expected outcome: Tests pass, no real commands executed
- Verify: `go test ./internal/tooling/worktree/`

#### Step 3: Implement List() and Remove() methods

- Add to `worktree.go`: `List()` and `Remove(name string)` methods
- Follow same patterns: mocked, tested
- Expected outcome: All three methods implemented and tested
- Verify: `go test ./internal/tooling/worktree/ -v`

#### Step 4: Create CLI subcommands

- In `cmd/worktree.go`, add:
  - `worktreeCreateCmd` with `Args: cobra.ExactArgs(1)`
  - `worktreeListCmd` with `Aliases: []string{"l", "ls"}`
  - `worktreeRemoveCmd` with fzf selection
- Follow patterns from [docs/guides/cli-patterns.md](../guides/cli-patterns.md)
- Expected outcome: Commands compile, parse flags correctly
- Verify: `dg worktree --help` shows all subcommands

#### Step 5: Register commands in root

- In `cmd/root.go`, add worktreeCmd and all subcommands to rootCmd
- Expected outcome: `dg worktree` command available
- Verify: `go build ./cmd/` and `./devgeta worktree --help`

#### Step 6: Add command tests

- Create `cmd/worktree_test.go`
- Test that each subcommand parses args correctly
- Use mocked dependencies (don't create real worktrees)
- Expected outcome: Command tests pass
- Verify: `go test ./cmd/ -v`

#### Step 7: Update documentation

- Add worktree feature to `docs/spec.md` Features section
- Include command examples and platform notes
- Expected outcome: Feature documented with examples
- Verify: Read docs/spec.md and confirm clarity

---

## 6. Verification Plan

### Automated Verification

List the packages this cycle actually touches, plus the ones that import them —
not `./...`. Get the importer list from the `go list` query in CLAUDE.md §6
("Which tests to run") rather than guessing; for `internal/tooling/worktree` it
is the five below. (If the cycle changes something most of the repo depends on,
say so here and run the suite; that is the exception, not the default.)

```bash
# Unit tests must pass — changed package + its direct importers
go test ./internal/tooling/worktree/ ./internal/tooling/task/ ./internal/tui/worktree/ \
        ./internal/tui/components/ ./cmd/

# Format and vet must pass
make lint

# Coverage for the packages this cycle touches
go test ./internal/tooling/worktree/ ./cmd/ -cover
```

### Manual Verification

1. Run `dg worktree --help` → Shows all subcommands with descriptions
2. Run `dg worktree create test-feature` → Creates worktree + window (or shows clear error)
3. Run `dg worktree list` → Shows created worktrees in table format
4. Run `dg worktree remove` → Opens fzf, lets you select & delete
5. Verify tmux window name matches `wt-<name>` pattern
6. Verify `ls -la .worktrees/` shows new branch directory

### Regression Check

- Does `dg install` still work? (Verify: `make build && ./devgeta install --help`)
- Do existing commands work? (Verify: `dg version`, `dg worktree --help`)
- No unintended changes to other commands

---

## 7. Risks & Trade-offs

| Risk                          | Likelihood | Mitigation                                                  |
| ----------------------------- | ---------- | ----------------------------------------------------------- |
| Tmux window naming conflicts  | Low        | Prefix with `wt-` and check for duplicates before create    |
| Git worktree path assumptions | Med        | Test on both macOS and Linux with different repo structures |
| Mock completeness hides bugs  | Low        | Verify commands don't execute real git/tmux in tests        |

### Trade-offs Made

- **Tmux window auto-launch vs. user control:** Auto-launching (user can switch with `<prefix> + w`) vs. requiring manual tmux selection. We're choosing auto-launch for convenience.
- **Worktree naming:** Using branch name as worktree name (simpler) vs. separate naming scheme. We're choosing same-name for consistency.
- **Error recovery:** On failure, leave worktree in place (manual cleanup) vs. auto-rollback. We're choosing manual for safety.

---

## 8. Cross-Model Review Notes

Space for review feedback when handing off between sessions or models:

- [ ] Domain context clear? (Do I understand the problem being solved?)
- [ ] Engineer context sufficient? (Do I know which files to touch and patterns to follow?)
- [ ] Objective unambiguous? (Is "done" crystal clear?)
- [ ] Scope is actually locked? (No ambiguity, no scope creep temptation?)
- [ ] Steps are actionable? (Each 5-15 min, with clear success criteria?)
- [ ] Verification is executable? (Can someone actually run these steps?)
- [ ] Risks are realistic? (Have similar risks appeared before?)

**Reviewer notes:**
(Fill in during review — what's unclear, what assumptions should be validated, etc.)

---

## Notes for Implementers

- **Cycle document is your spec.** Update it if requirements change, but don't change scope without calling it out.
- **Commit after each step.** Run `/smart-commit` once a step's verify check passes — keeps history granular and each commit leaves the system working.
- **Verification must pass before "done."** Automated tests (targeted — see above) + manual checks + regression check.
- **If you hit a risk, escalate immediately.** Don't try to handle it silently.
- **Ask questions early.** Better to clarify now than discover issues at the end.
