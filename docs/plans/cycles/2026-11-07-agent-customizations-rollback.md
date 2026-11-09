# Cycle: Agent customizations rollback

**Date:** 2026-11-07
**Estimated Duration:** ~9 hours, plus the maintainer's sandbox deploy
**Status:** Complete (code and docs done 2026-11-08; maintainer sandbox deploy pending)
**Branch:** `agentsv2`
**Investigations:**

- [2026-11-06-agent-customizations-rollback.md](../../investigations/2026-11-06-agent-customizations-rollback.md)
  — what this cycle implements (findings 1–5)
- [2026-11-06-skills-and-agents-reset.md](../../investigations/2026-11-06-skills-and-agents-reset.md)
  — the skills/commands/agents removal before it (`1906e9e`), and the
  follow-up on parity across every coder
- [2026-11-06-rtk-value-and-risk.md](../../investigations/2026-11-06-rtk-value-and-risk.md)
  — rtk removal before it (`d7adb89`, ADR-0065); its recommendation 2 is
  acted on here

**ADRs:** [ADR-0066](../../decisions/ADR-0066-devgeta-removes-its-token-reduction-layer.md),
[ADR-0067](../../decisions/ADR-0067-agent-permission-floor.md) (both accepted 2026-11-08)

**Sequence on `agentsv2`:** `d7adb89` (rtk) → `1906e9e` (skills, commands,
agents) → **this cycle** → version pinning (planned, no doc yet) →
agent-skill packages (planned, no doc yet).

---

## 1. Domain Context

Since July, devgeta has added its own layer around the AI coders (Claude Code,
OpenCode, Pi): hooks that rewrite or block commands, agent-only `dg task`
subcommands, a handoff note, a context report, two guard hooks, and a
permission list of about 110 rules per coder. Most of it was built to cut
token costs. The rtk investigation found the measurable saving was under 2%
of the bill, and blocked or shortened commands made agents redo work.

`agentsv2` already removed rtk (`d7adb89`) and the shared skills, commands,
agents and `DEVGETA.md` (`1906e9e`). This cycle removes the rest of the
layer and resets the permissions to a small floor, so the next work (pinned
versions, agent-skill packages) starts from a clean base. Anything that comes
back later is rebuilt for every coder devgeta configures, Pi included.

Background: the investigations above,
[ADR-0006](../../decisions/ADR-0006-hook-guardrails-scope-and-sharing.md),
[ADR-0014](../../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md),
[agent-sync.md](../../guides/agent-sync.md),
[agent-permission-matching.md](../../guides/agent-permission-matching.md).

---

## 2. Engineer Context

- **Relevant files:**
  - `configs/claude/settings.json.tmpl` — Claude Code permissions and hooks
  - `configs/opencode/opencode.json.tmpl` — OpenCode permissions
  - Removed: `configs/claude/{task-redirect,output-budget,secret-guard,suppression-guard}.sh`,
    `configs/opencode/plugin/{task-redirect,output-budget,secret-guard,suppression-guard}.js`,
    `configs/devgeta/output-budget-run.sh`, `configs/claude/lib/`
  - Kept: `configs/claude/agent-config-guard.sh`,
    `configs/opencode/plugin/agent-config-guard.js`
  - `cmd/task.go`, `cmd/task_pr.go`, `cmd/task_issue.go` — `dg task` subcommands
  - `internal/tooling/{handoff,contextreport,reviewjournal,branchstore,task}/`
  - `internal/config/fromFile.go:62-69` — `integrations.output_budget`
  - `internal/apps/claude/claude.go:26-44,165` — `OutputBudgetEnabled`
  - `internal/apps/opencode/permissions_test.go` — parity and rule tests
  - `internal/apps/claude/testdata/golden_settings.json` — rendered settings
- **Must keep working:** the tmux dashboard (`internal/tui/worktree`) calls
  `task.BranchDiffAt`, `task.BranchStatsAt` and their result types;
  `agent-config-guard`; `format.sh`, `agent-state.sh`, `notify.js`, the
  statusline; the human `dg task` subcommands (`refresh-branch`,
  `reset-main-branch`, `delete-branch`, `reinstall-libraries`,
  `reinstall-library`) and `release`.
- **Old config keys:** `gopkg.in/yaml.v3` ignores fields the struct no
  longer has, and the next save drops them. Deleting the `OutputBudget`
  field is enough; no migration code is needed (the same way `1906e9e`
  handled `review:`).
- **Two coders, one change:** every hook or permission change lands in both
  coders in the same commit (CLAUDE.md §12). Widening that rule and the
  parity tests to Pi is out of scope (see §4).
- **The suppression guard is live in this session.** Until step 3 lands,
  any edit whose text contains a lint-suppression directive is denied, even
  in Markdown. Write around the tokens in docs, or do step 3 early.
- **Tests:** targeted per step (CLAUDE.md §6). This cycle touches `configs/`,
  `cmd/` and `internal/tooling`, most of the tree, so step 10 runs the full
  suite once.

  ```bash
  go list -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{range .TestImports}} {{.}}{{end}}' ./... \
    | grep 'devgeta/internal/tooling/task' | cut -d' ' -f1
  ```

---

## 3. Objective

Devgeta ships no token-reduction machinery, no agent-only `dg task`
subcommands and no guards beyond `agent-config-guard`, and both coders get
the same small permission floor, with every person-facing feature still
working.

---

## 4. Scope Boundary

### In Scope

- [x] Maintainer's calls decided; ADR-0066 and ADR-0067 accepted (step 0)
- [x] Remove `task-redirect` (both coders) and its `release` redirect
- [x] Remove output-budget (both coders, runner, flag, template branch)
- [x] Remove `secret-guard`, `suppression-guard` and `configs/claude/lib/`
- [x] Remove the agent `dg task` group, `handoff`, `context-report`, and the
      review journal; keep the Go functions the dashboard calls; drop the
      review-journal delete in `dg wt` teardown
- [x] Reset permissions to the floor in ADR-0067, both coders
- [x] Trim `permissions_test.go` to the new floor
- [x] Docs, ADR statuses, migration note

### Explicitly Out of Scope

- Version pinning for coders, Pi extensions and skill packages (next cycle)
- Agent-skill packages: Superpowers, Ponytail (cycle after that)
- `DEVGETA.md` v2 and Pi delivery of it (skills-reset investigation)
- Widening parity tests and CLAUDE.md §12 from "both coders" to every coder
  (Pi included). Recorded in the skills-reset investigation, Shared work.
- Rebuilding a secret check or a suppression ban. Rebuild notes are in the
  rollback investigation, finding 5.

**Scope is locked.** If you discover something out of scope is needed,
document it for a future cycle and reference here.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                                                                                                      | Description                                    |
| ------ | ------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------- |
| Create | `docs/decisions/ADR-0066-devgeta-removes-its-token-reduction-layer.md`                                                         | Done (accepted)                                |
| Create | `docs/decisions/ADR-0067-agent-permission-floor.md`                                                                            | Done (accepted)                                |
| Delete | `configs/claude/task-redirect.sh`, `configs/opencode/plugin/task-redirect.js`                                                  | Step 1                                         |
| Delete | `configs/claude/output-budget.sh`, `configs/opencode/plugin/output-budget.js`, `configs/devgeta/output-budget-run.sh`          | Step 2                                         |
| Delete | `configs/claude/{secret,suppression}-guard.sh`, `configs/opencode/plugin/{secret,suppression}-guard.js`, `configs/claude/lib/` | Step 3                                         |
| Modify | `configs/claude/settings.json.tmpl`                                                                                            | Steps 1, 2, 3, 6                               |
| Modify | `configs/opencode/opencode.json.tmpl`                                                                                          | Step 6                                         |
| Modify | `internal/config/fromFile.go`, `internal/apps/claude/claude.go`                                                                | Step 2                                         |
| Modify | `cmd/task.go`, `cmd/task_pr.go`; delete `cmd/task_issue.go`                                                                    | Step 5                                         |
| Delete | `internal/tooling/{handoff,contextreport,reviewjournal}/`                                                                      | Step 5                                         |
| Modify | `internal/tooling/task/`, `internal/tooling/branchstore/`                                                                      | Step 5 (delete what only removed commands use) |
| Modify | `internal/tooling/worktree/worktree.go`                                                                                        | Step 5 (drop the review-journal delete)        |
| Modify | `internal/apps/opencode/permissions_test.go`                                                                                   | Step 7                                         |
| Create | `docs/migrations/agent-customizations-rollback.md`                                                                             | Step 8                                         |
| Modify | docs listed in step 9                                                                                                          | Step 9                                         |

### Step-by-Step

Each step is one commit and leaves the tree building and its targeted tests
green.

#### Step 0: Decide the open calls, accept the ADRs

- The maintainer confirms or changes each proposed decision below. Copy the
  answers into ADR-0067's Decision section and set both ADRs to ACCEPTED.
- Verify: no "pending" left in ADR-0067; both ADRs ACCEPTED in
  `docs/decisions/README.md`.

| Call                                                                                                                       | Proposed decision                                              | Confirmed       |
| -------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- | --------------- |
| Read deny for credential files (`.aws`, `.kube`, `.npmrc`, `.netrc`, …)                                                    | Keep: same kind as the `.env`/SSH list                         | Yes, 2026-11-08 |
| Read deny for shell history                                                                                                | Keep: history often holds pasted tokens; agents rarely need it | Yes, 2026-11-08 |
| `ask` for `git push --force`, `sudo`, `gh repo delete`, `gh release delete`                                                | Keep: irreversible or acts outside the machine                 | Yes, 2026-11-08 |
| `ask` for `ssh`/`scp`/`rsync`, `gh api`, `gh pr merge/close`, `gh issue delete`, `terraform apply`, `kubectl apply/delete` | Drop                                                           | Yes, 2026-11-08 |
| Edit deny for shell rc files (`.zshrc`, `.bashrc`, `.profile`)                                                             | Drop: legitimate setup tasks edit them                         | Yes, 2026-11-08 |
| `secret-guard`                                                                                                             | Remove, rebuild later at the git level (notes in finding 5)    | Yes, 2026-11-08 |
| `suppression-guard`                                                                                                        | Remove, rebuild later in this repo's own config                | Yes, 2026-11-08 |
| `worktree-start` / `worktree-finish`                                                                                       | Remove: `dg wt` covers people                                  | Yes, 2026-11-08 |
| Scratch directory (`dg task scratch`, `additionalDirectories`)                                                             | Keep until the skill-package work shows whether it's used      | Yes, 2026-11-08 |
| Version bump                                                                                                               | MAJOR: removes CLI subcommands                                 | Yes, 2026-11-08 |

#### Step 1: Remove `task-redirect` from both coders

- Delete both files and the `PreToolUse` entry in `settings.json.tmpl`.
  `opencode.go` copies the whole `configs/opencode/plugin/` folder, so
  deleting the file is enough there.
- CLAUDE.md §3 principle 8 and §12 use the `release` redirect's `go.mod`
  gate as the example of a gated process; replace the example or drop it.
- Delete the hook's tests: `cmd/task_redirect_hook_test.go` and the
  root-package tests that run the script.
- Verify: `go build ./... && go test . ./cmd/ ./internal/apps/claude/ ./internal/apps/opencode/`

#### Step 2: Remove output-budget

- Delete the three files, the `{{if .OutputBudgetEnabled}}` block, the
  `OutputBudgetEnabled` field and its computation, and
  `IntegrationsConfig.OutputBudget` with its helper.
- Verify: same packages as step 1, plus `./internal/config/`.

#### Step 3: Remove `secret-guard` and `suppression-guard`

- First record the current commit hash in the rollback investigation's
  rebuild notes ("the last commit with the guards").
- Delete both guards in both coders, their `settings.json.tmpl` entries, and
  `configs/claude/lib/` (only these two guards and `task-redirect` source
  it; check with `grep -rl 'lib/segments.sh\|lib/devgeta-repo.sh' configs/`).
- Delete their root-package tests.
- CLAUDE.md §6 ("Which tests to run") lists the guard shells; §9 step 3
  says to run `git add` and `git commit` separately because of
  `secret-guard`. Update both.
- Verify: `go build ./... && go test . ./internal/apps/claude/ ./internal/apps/opencode/`

#### Step 4: Trace the remaining `dg task` Go callers

- For each subcommand being removed, list the `internal/tooling/task`
  functions it calls, then grep for other callers. Known keepers:
  `BranchDiffAt`, `BranchStatsAt`, `BranchStatsResult`, `BranchDiffResult`,
  `FileStat`.
- Same for each package being deleted: list its importers outside `cmd`.
  Known: `reviewjournal` is imported by `internal/tooling/worktree`
  (teardown journal delete), which step 5 edits; `branchstore` is imported by
  `task/handoff.go` and `reviewjournal` only.
- Write the list of functions to delete into this step before step 5.
- **Result (done 2026-11-08):**
  - Dashboard keepers in `internal/tooling/task`: `branchdiff.go`
    (`BranchDiffAt`, `BranchStatsAt`, their result types, `FileStat`), plus
    what it needs: `filechange.go` (renamed from `scope.go`: `fileChange`,
    `parseNumstat`, `partitionExcluded`, the exclusion-note formatters) and
    `exclusions.go`. Human-command keepers: `task.go`, `release.go`,
    `scratch.go`.
  - Deleted with their tests: `committrailers`, `contextreport`,
    `docstatus`, `handoff`, `issue`, `pr`, `pr_checks_digest`,
    `prreviewstate`, `prreviewtarget`, `prstate`, `reviewnotes`,
    `reviewpackage`, `reviewthreadsjson`, `worktree` (worktree-start and
    -finish), and from `scope.go`/`branchdiff.go`: `ReviewScope`,
    `BranchDiff`, `branchDiffFile`, `mergeBase`, `aheadBehind`,
    `parseAheadBehind`, `commitLog`, `parseCommitLog`, `commitSizes`,
    `reviewablePaths`, `diffByteSize`, `fileChanges`, `parseNameStatus`,
    `formatReviewScope`, `formatFileStats`, `sameFileSet`.
  - Packages deleted: `handoff`, `contextreport`, `reviewjournal`,
    `branchstore`. Only `internal/tooling/worktree` imported one of them
    (`reviewjournal`); that call is gone.
  - Follow-up, not done here: `githubcli` and `jq` wrappers still carry
    methods only the removed PR/issue tasks called.
- Verify: the list exists here; no code change.

#### Step 5: Remove the agent `dg task` group

- Remove: `review-scope`, `branch-diff`, `review-package`, `review-notes`,
  `review-note`, `handoff`, `context-report`, `commit-trailers`, every
  subcommand in `task_pr.go`, `issue-scope`, and `worktree-start` /
  `worktree-finish` if step 0 confirms it.
- Delete `internal/tooling/{handoff,contextreport,reviewjournal}` and the
  functions listed in step 4; delete `branchstore` if nothing still imports it
  (after `reviewjournal` goes, only `task/handoff.go` used it, and that goes
  too).
- `internal/tooling/worktree` (`dg wt`) deletes the branch's review journal in
  teardown (`worktree.go`, `reviewjournal.New(...).Delete`). Drop that call
  with `reviewjournal`; `dg wt` teardown otherwise unchanged.
- Update the `dg task` help text in `cmd/task.go:78-100`.
- Verify: `go build ./... && go vet ./... && go test ./cmd/ ./internal/tooling/task/ ./internal/tooling/worktree/ ./internal/tui/worktree/`

#### Step 6: Reset permissions to the floor

- Apply ADR-0067 to both templates in one commit. Home paths ship in both
  spellings (`~/…` and `**/…`).
- Drop the enumerated agent-config Edit denies; `agent-config-guard` stays.
- Regenerate `internal/apps/claude/testdata/golden_settings.json`.
- Verify: `go test ./internal/apps/opencode/ ./internal/apps/claude/`

#### Step 7: Trim the permission tests

- Keep: `TestEmbeddedConfigCatchAllIsFirst`,
  `TestHomeAnchoredDeniesHaveGlobstarTwins`,
  `TestClaudeAndOpenCodePermissionParity`,
  `TestClaudeAndOpenCodeFormatterParity`, the two scratch-directory tests.
- Rewrite `TestEmbeddedConfigGuardsDangerousCommands` to assert the floor:
  each secret read is denied in both coders, each kept `ask` is present.
- Remove or rewrite `TestGhWriteGatesResolve` and the two
  `TestGlobalClaudeFloor…` tests to match step 6.
- Verify: `go test ./internal/apps/opencode/`

#### Step 8: Migration note

- `docs/migrations/agent-customizations-rollback.md`:
  - what disappears on the next `dg configure claude --force` /
    `dg configure opencode --force`;
  - that `dg configure` doesn't delete scripts it no longer ships, so the
    old hook scripts stay in `~/.claude/` until removed by hand (list them);
  - that `integrations.output_budget` is ignored and dropped on next save;
  - that the removed `dg task` subcommands now fail;
  - that the floor is looser: what is no longer blocked or asked, and that
    nothing checks agent commits for secrets any more.
- Link it from `docs/migrations/README.md`.

#### Step 9: Docs

- Remove: `docs/guides/token-efficiency.md`,
  `docs/guides/output-budget-runner.md`, `docs/guides/task-design.md`.
- Rewrite: the `dg task` sections of `docs/spec.md` and `README.md`; hook
  and permission sections of `docs/apps/claude.md` and
  `docs/apps/opencode.md`; `docs/guides/agent-sync.md`;
  `docs/guides/agent-permission-matching.md` (examples that cite removed
  rules).
- CLAUDE.md: drop the task-design and output-budget rows from the
  documentation index; the §3, §6, §9 and §12 edits from steps 1 and 3.
- Mark the ADRs that ADR-0066 and ADR-0067 supersede, and update
  `docs/decisions/README.md`.
- `docs/recent-changes.md` entry.
- Verify: `grep -rn 'task-redirect\|output-budget\|context-report\|handoff\|review-run\|secret-guard\|suppression-guard' docs/ CLAUDE.md README.md configs/`
  returns only history (ADRs, investigations, cycles, migrations).

#### Step 10: Full verification

- `go build ./...`, `make lint`, full `go test ./...` (this cycle's blast
  radius is most of the tree, CLAUDE.md §6).
- Mark the rollback investigation **Done**, this cycle **Complete**, and
  link the final commits from the investigation.

---

## 6. Verification Plan

### Automated Verification

```bash
go build ./...
make lint
go test ./...        # once, at step 10 — touches configs/, cmd/, internal/tooling
```

### Manual Verification

Maintainer's steps; each command needs a yes before it runs (CLAUDE.md §4).
Use a sandboxed `HOME` first:

1. `make build`, then `HOME=<sandbox> ./devgeta configure claude --force` and
   `HOME=<sandbox> ./devgeta configure opencode --force`.
2. Start each coder against the sandbox: no hook errors at startup.
3. Ask the agent to read a `.env` file → denied in both coders.
4. Ask it to edit `~/.claude/settings.json` → `agent-config-guard` denies.
5. Ask it to run `curl -sI https://example.com` → runs without a prompt.
6. Ask it to run `git add -A && git commit -m test` in a scratch repo →
   runs (no guard any more).
7. Ask it to run `git push --force` in a scratch repo → asks first.
8. `dg ws` dashboard shows branch stats and agent state as before.
9. `dg wt` create and remove still run; remove no longer deletes a review
   journal (gone with `reviewjournal`).

### Regression Check

- `dg task --help` lists only the kept subcommands; `dg task release --help`
  still works.
- `dge` wrapper subcommands (`refresh-branch`, `reset-main-branch`,
  `delete-branch`, `reinstall-libraries`) still run.
- Formatting on save still runs in both coders and Pi.

---

## 7. Risks & Trade-offs

| Risk                                                     | Likelihood | Mitigation                                                                                                         |
| -------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------ |
| A removed `task` function is still used by the dashboard | Med        | Step 4 trace before step 5; `go build` and dashboard tests                                                         |
| An agent commits a secret file that `.gitignore` misses  | Low        | Read denies keep secret content out of context; migration note says no commit check runs; rebuild at the git level |
| Looser permissions let an agent do something harmful     | Med        | Keep secret reads, irreversible `ask`s and `agent-config-guard`; migration note lists what changed                 |
| Coders drift apart                                       | Low        | Parity test stays; both coders change in the same commit                                                           |
| Users rely on a removed `dg task` subcommand             | Low        | MAJOR bump, migration note                                                                                         |

### Trade-offs Made

- **Bash pattern denies vs. friction:** dropping them loses a speed bump
  that a determined agent already bypasses (`command curl`), and gains
  fewer wasted turns.
- **Agent-hook guards vs. a clean start:** `secret-guard` and
  `suppression-guard` covered two coders out of three, one of them enforced
  devgeta's own rule on every user, and both blocked legitimate work. They
  go now and come back, if needed, built for every coder or at the git
  level.
- **Path denies vs. guard for agent config:** one mechanism
  (`agent-config-guard`, ADR-0014) instead of a guard plus a 40-line list
  that has to track every file devgeta ships.
- **Delete vs. deprecate subcommands:** delete outright; their only callers
  were removed on this branch already.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope is actually locked?
- [ ] Steps are actionable?
- [ ] Verification is executable?
- [ ] Risks are realistic?

**Reviewer notes:**

---

## Notes for Implementers

- **Cycle document is your spec.** Update it if requirements change, but don't
  change scope without calling it out.
- **Commit after each step**, once its verify check passes.
- **Never run `dg configure` or install against the real machine** without
  the maintainer's yes for that command (CLAUDE.md §4).
