# Recent Changes

Short prose summaries of changes whose _reasoning_ isn't obvious from the code
or the commit. This is a reading aid, not a record — the permanent record is
`docs/decisions/` (why we chose it), `docs/plans/cycles/` (what the work was),
and git history (what changed).

**Pruning rule:** keep entries from the last two releases. Delete anything older
in the same pass that adds a new entry. If deleting an entry would lose
something a reader still needs, that thing belongs in an ADR or a guide — put it
there first, then delete. This file must not grow without bound; that is exactly
why it no longer lives in `CLAUDE.md`.

**Last updated:** 2026-08-07

---

## Recent changes

- Tests are targeted by default; the full suite is the release gate (2026-08-07).
  The suite grew to ~2,500 tests in ~80 packages (~5.5 min cold), and the docs
  told every contributor and agent to run all of it before every commit — a cost
  paid dozens of times a day for signal a targeted run already gives. CLAUDE.md
  §6 now has **"Which tests to run"**: the changed package plus its in-repo
  importers, found with a `go list` query rather than guessed. `go test ./...` is
  required in §9 step 1 (release) and when a change's blast radius really is the
  whole tree (`pkg/paths` has 71 dependents; anything under `configs/` that the
  embedded-config tests read). The same edit went through CONTRIBUTING.md,
  [testing-patterns.md](guides/testing-patterns.md),
  [releasing.md](guides/releasing.md), [plans/TEMPLATE.md](plans/TEMPLATE.md),
  and the Makefile/README `make test` labels — but **not** the skills under
  `configs/shared/skills/`: a first pass edited four of them and was reverted,
  because those ship to every user and run in repos that have no such policy.
  That boundary is now written down as a §4 non-negotiable, in principle 8, and
  in CLAUDE.md §12 "Anything we ship is built for strangers" — it covers every
  shipped artifact and every size of change, not just skills. Known trade:
  targeted runs cannot see
  cross-package interference or load-dependent flakiness — those now surface only
  at the release gate, and nothing in CI catches them earlier, because there is
  no CI test job. Measured cause of the runtime: the root package alone is 319s
  of the 328s suite (bash-spawning hook tests, no `t.Parallel()`); everything
  else combined is ~30s.

- Agent memory is writable again (2026-08-07). Both permission layers denied
  `~/.claude/projects/<slug>/memory/`, Claude Code's per-project memory
  directory, so the agent could not write a memory file. Memory holds notes,
  not permissions or hooks, so `agent-config-guard.sh`/`.js` clause 1 gained a
  second exception beside `worktrees` (scoped to a file strictly under
  `projects/<slug>/memory/`, `.claude`-only), and the settings floor's blanket
  `Edit(~/.claude/**)` was replaced by an enumeration of the config surfaces
  under that root — deny beats allow with no specificity tiebreak, so no
  carve-out could re-open it otherwise. See
  [ADR-0014](decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)'s
  memory amendment. Both agents changed symmetrically, and
  `TestGlobalClaudeFloorLeavesMemoryWritable` stops the blanket coming back.

- Review scope, output, and steering changed together (2026-08-07). A review now
  covers the branch's **working state** — commits AND uncommitted work, untracked
  files included — so `review-scope` and `branch-diff` diff `git diff <merge-base>`
  (two dots, against the worktree) and `review-run` only refuses a branch with no
  commits ahead **and** a clean tree. See
  [ADR-0019](decisions/ADR-0019-a-review-covers-the-branch-working-state.md);
  `collectWorktreeDiff` is the single gather behind both `BranchDiff` and the
  `dg ws` diff pane, so the two cannot drift. `review-run` also gained
  `--note <text>` (the human's own emphasis for every reviewer of the round,
  framed so it cannot narrow the review; forwarded by `/review-loop --note`),
  dropped its trailing `open:` line (findings live in the journal — `review-notes`
  is what lists them, and `/review-loop` reads its ids from there), and reports
  progress **as it happens** via the new `CommandParams.OnStdoutLine`, which hands
  a caller each stdout line while the child still runs. That progress is
  **sampled**: at most one heartbeat every 30s
  (`progressHeartbeatInterval`), naming the running counters and the tool call
  that triggered it, because the line-per-tool-call version measured ~200 lines a
  round that `/review-loop` captured and paid tokens for. The full stream is
  behind the existing root `--verbose` flag — no new flag — which `cmd/task.go`
  copies onto `TaskManager.Verbose`; every tool call is still counted while quiet,
  so the closing line totals the whole run.

- `dg wt create` gained `--prompt <text>` and repeatable `--pane <command>`
  (2026-07-31). `--prompt` starts the layout's AI coder already working on a
  task; it is delivered as a **launch argument** (`cc '<text>'`,
  `oc --prompt '<text>'`), never as keystrokes after the TUI boots — see
  [ADR-0011](decisions/ADR-0011-agent-prompt-as-launch-argument.md). It
  errors on a layout with no AI pane rather than silently dropping the prompt.
  `--pane` adds a shell pane beside the layout and its value is used **unquoted**
  (it is a shell command line, so `'cd api && make dev'` works); an empty value
  is rejected. Both are create-only — repair takes neither.
  Implemented as transformations on a resolved `worktree.Layout`
  (`WithPrompt`/`WithExtraPanes`), so no `Create`/`CreateAt`/TUI signature
  changed. In the same change, `Layout`'s parallel `paneCheckers` slice was
  folded into unexported `Pane.check`/`Pane.prompt` fields — a pane's command,
  install check, and prompt form now come from one `AICoder` via `coderPane`,
  which is why `Pane` is no longer comparable with `==`.

## Recent specs completed

- `specs/001-binary-dist-audit/` — Go embed, text/template for config generation
- `specs/002-debian-package-fixes/` — Strategy pattern, package mappings, exponential backoff downloads
