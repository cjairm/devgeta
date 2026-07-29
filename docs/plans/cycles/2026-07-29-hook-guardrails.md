# Cycle: Secret-commit and lint-suppression guardrail hooks

**Date:** 2026-07-29
**Status:** In Progress
**ADR:** [ADR-0006](../../decisions/ADR-0006-hook-guardrails-scope-and-sharing.md)
**Origin:** user request to close two gaps in the AI-agent guardrails identified
while auditing what devgeta's Claude/OpenCode configs already enforce.

## Goal

Add two PreToolUse guardrail hooks, mirrored across both AI coders exactly the
way `task-redirect.sh`/`.js` already are:

1. **Secret-commit guard** — deny a `git commit` whose staged changes contain a
   filename or content pattern that looks like a committed secret. Global
   scope (fires in every repo).
2. **Suppression-comment guard** — deny an Edit/Write that introduces a
   lint-suppression comment (`//nolint`, `# noqa`, `@ts-ignore`,
   `eslint-disable`, …). Devgeta-repo-only scope (CLAUDE.md's own stance, not
   a universal one — see ADR-0006).

## Scope

- New shared bash libs: `configs/claude/lib/segments.sh`,
  `configs/claude/lib/devgeta-repo.sh` — `task-redirect.sh` refactored to
  source them instead of defining the same functions locally.
- New hook scripts: `configs/claude/secret-guard.sh`,
  `configs/claude/suppression-guard.sh`.
- New OpenCode plugins: `configs/opencode/plugin/secret-guard.js`,
  `configs/opencode/plugin/suppression-guard.js` — importing
  `splitCommandSegments`/`isDevgetaRepo` from `task-redirect.js` rather than a
  new shared file (ADR-0006 explains why).
- `configs/claude/settings.json.tmpl`: register both new hooks
  (`secret-guard.sh` alongside `task-redirect.sh` under the existing `Bash`
  matcher; `suppression-guard.sh` under a new `Edit|Write` PreToolUse matcher).
- `internal/apps/claude/claude.go`: deploy the two new scripts and the new
  `lib/` directory.
- Tests: Go behavioral tests (root package `main`, mirroring
  `task_redirect_test.go`) and Node behavioral tests (mirroring
  `task-redirect.test.mjs`) for both new hooks, plus a loader-safety test that
  every plugin file's exports tolerate being called with an arbitrary `ctx`
  (the risk ADR-0006 documents).
- `docs/apps/claude.md`: document both new hooks alongside the existing ones.

Out of scope: a real secret-scanner integration (gitleaks/trufflehog) — this
is a narrow, high-confidence pattern list, documented as a safety net, not a
replacement. Extending the suppression ban's pattern list beyond the
languages devgeta already formats (Go, JS/TS, Python) plus a couple of common
universal directives (Java/Kotlin, Ruby) for reasonable general coverage.

## Steps

- [x] Verify exact Claude Code PreToolUse payload fields for Edit/Write
      (`tool_name`, `tool_input.{file_path,old_string,new_string,content}`)
      and OpenCode's exact tool ids/arg field names (`edit`/`write`,
      `filePath`/`oldString`/`newString`/`content`) against upstream docs and
      source
- [x] Confirm OpenCode's plugin-directory loader semantics (every export of
      every file is invoked as a plugin) — decides the code-sharing approach
- [x] ADR-0006 written and indexed
- [x] `configs/claude/lib/segments.sh`, `configs/claude/lib/devgeta-repo.sh`
- [x] Refactor `configs/claude/task-redirect.sh` to source the libs (no
      behavior change — existing tests keep passing unchanged)
- [x] `configs/claude/secret-guard.sh`
- [x] `configs/claude/suppression-guard.sh`
- [x] Export `splitCommandSegments` from `configs/opencode/plugin/task-redirect.js`
- [x] `configs/opencode/plugin/secret-guard.js`
- [x] `configs/opencode/plugin/suppression-guard.js`
- [x] Wire both into `configs/claude/settings.json.tmpl`
- [x] Deploy new files in `internal/apps/claude/claude.go`
      (OpenCode needs no code change — `plugin/` is already copied wholesale)
- [x] Go + Node behavioral tests for both hooks
- [x] Loader-safety regression test for OpenCode plugin exports
      (`plugin-loader-safety.test.mjs`)
- [x] `docs/apps/claude.md` updated
- [x] `go build ./...`, `go test ./...` (root, `cmd/`, `internal/apps/claude`,
      `internal/apps/opencode`, whole repo), `make lint`, `golangci-lint run
./...` (39 pre-existing issues, none in touched files) all green
- [x] `node --test` on all four `.test.mjs` files green
- [ ] Manual verification: real `git commit` with a staged secret denies; a
      clean commit allows; a `//nolint` Edit denies inside this repo and
      allows outside it (covered by automated behavioral tests above with
      real `git`/`jq`/`node`, but not yet exercised through an actual live
      Claude Code / OpenCode session)

## Code review round 1 (fixed before merge)

External review (via `/receiving-code-review`) found three correctness bugs
in both hook mirrors, all confirmed and fixed with regression tests added:

1. `secret-guard.sh`/`.js`: `GIT_ANCHOR`/`GIT_COMMIT_PATTERN` required
   `commit` to immediately follow `git`, so `git -C <dir> commit`
   (also `-c`, `--git-dir=`) bypassed the guard entirely — an everyday
   invocation, not an adversarial one. Fixed by tolerating common git
   global options between `git` and the subcommand.
2. `secret-guard.sh`/`.js`: the staged-filename check
   (`git diff --cached --name-only`) listed deleted paths too, so staging
   the _removal_ of a secret — the correct remediation — was denied as if
   it were being added. Fixed with `--diff-filter=ACMR`.
3. `suppression-guard.sh`/`.js`: the "introduced" check was presence-based
   (needle in `NEW`, absent from `OLD`), so a second, genuinely new
   suppression was allowed whenever an unrelated one of the same kind
   already existed in the touched span. Fixed with an occurrence-count
   comparison (`new_count > old_count`).

All three regressions have dedicated Go and Node tests
(`TestSecretGuardHook_DeniesGitDashCInvocation`,
`TestSecretGuardHook_AllowsStagedDeletionOfSensitiveFile`,
`TestSuppressionGuardHook_DeniesAdditionalSuppressionWhenOneAlreadyExisted`,
and their `.test.mjs` counterparts).

Fix 1's bypass class (a global option between the binary and its subcommand
defeating the anchor) also existed in `task-redirect.sh`/`.js`'s
pre-existing rules (`git -C ../wt worktree add x`, `gh -R owner/repo pr
checks`, ...) — out of scope for the review since it predates this cycle,
but the same fix was requested and applied here too. `DEVGETA_GIT_GLOBAL_OPT`
and `DEVGETA_GH_GLOBAL_OPT` were added to `lib/segments.sh` (now a second
bash use, so extracted rather than duplicated a third time) and folded into
`task-redirect.sh`'s `GIT_ANCHOR`/`GH_ANCHOR`; `task-redirect.js` got its own
`GIT_GLOBAL_OPT`/`GH_GLOBAL_OPT` consts folded into `GIT_PREFIX`/`GH_PREFIX`
(kept duplicated from secret-guard.js's copy, per ADR-0006's accepted
JS-side trade-off — these constants can't be shared as plugin-file exports
without becoming non-function exports, which the loader would reject).
Two more cases added to the existing `TestTaskRedirectHook_DeniesNarrowPatterns`
table (Go) and its `.test.mjs` counterpart, mirroring how the earlier
env-var-prefix cases were folded into the same table. Full suite re-verified
green after the fixes: `go build ./...`, `go test ./...`, `make lint`, and
all four `node --test` files.

## Code review round 2 (fixed before merge)

A second external review found three more issues, all confirmed by tracing
the actual regex/control-flow (no live OpenCode/Claude runtime available to
reproduce interactively — verified instead by executing the real scripts
against real temp git repos, same rigor as round 1):

1. **`secret-guard.sh`/`.js`: `-C` recognized but the wrong repo scanned.**
   Round 1 fixed `git -C <dir> commit` being _recognized_ as a commit; this
   round's finding was that the hook then scanned its own payload
   cwd/`projectDir` regardless — never `<dir>`. Fixed by resolving the LAST
   `-C` in the matched commit segment (uppercase only; `-c` is git's
   unrelated config-value flag) into a `TARGET_DIR`/`targetDir` used for
   every subsequent git call.
2. **`secret-guard.sh`/`.js`: compound stage-then-commit checked before
   staging happens.** This is a PreToolUse hook — it fires before ANY part
   of the Bash command runs, so `git add -A && git commit` is checked
   against a stale index that doesn't yet reflect the not-yet-run `add`.
   Tracing this further (not named by the reviewer) surfaced the identical
   flaw in a BARE `git commit -a`/`-am`/`--all`, which self-stages at commit
   time with no compound command needed. Both shapes are now denied
   outright (not checked) with a message asking for two separate Bash
   calls — the only way this hook can make a reliable assessment, since it
   cannot simulate what a not-yet-run `git add`/`-a` would stage without a
   much heavier (and still incomplete) pathspec-matching simulation.
   `--amend` alone is verified unaffected.
3. **`task-redirect.js`'s `GH_GLOBAL_OPT`: space-separated `--repo <repo>`
   bypassed redirects.** The alternation only ever consumed a long flag's
   value in `--flag=value` form, never `--flag value` — confirmed systemic
   (affects any long flag in space form, not just `--repo`), so fixed
   generally rather than special-casing `--repo` the way the suggested
   patch did: added a `--[A-Za-z-]+\s+\S+` alternative alongside the
   existing bare `--[A-Za-z-]+` one. Relies on the regex engine trying both
   readings and keeping whichever lets the overall anchored pattern match
   (verified this is safe: `--no-pager status` only matches under the
   "bare flag" reading, since treating `status` as `--no-pager`'s value
   leaves nothing to satisfy a required subsequent literal). Fixed in the
   shared bash lib (`DEVGETA_GIT_GLOBAL_OPT`/`DEVGETA_GH_GLOBAL_OPT`, one
   edit covers both `secret-guard.sh` and `task-redirect.sh`) and
   independently in both JS files' own copies (per ADR-0006's accepted
   duplication trade-off).

New/rewritten tests: `TestSecretGuardHook_ScansDashCTargetNotPayloadCwd`,
`TestSecretGuardHook_DeniesCompoundStagingWithCommit`,
`TestSecretGuardHook_DeniesSelfStagingCommitFlag`, plus two new cases in the
`gh --repo`/bare-long-flag-sanity spots of `TestTaskRedirectHook_*`, and
their `.test.mjs` counterparts (`secret-guard.test.mjs` gained 4 tests,
`task-redirect.test.mjs` gained 4 cases). The prior round's
`TestSecretGuardHook_DeniesGitDashCInvocation` was split into
`TestSecretGuardHook_RecognizesCommitDespiteGlobalOptions` (kept) and
`TestSecretGuardHook_ScansDashCTargetNotPayloadCwd` (new) — its original `-C`
case had encoded the pre-fix bug as expected behavior and needed rewriting,
not just extending. Full suite re-verified green: `go build ./...`,
`go test ./...`, `make lint`, `golangci-lint run ./...` (pre-existing issues
only, none in touched files), and all four `node --test` files (32 tests
total).
