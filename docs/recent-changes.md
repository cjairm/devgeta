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

**Last updated:** 2026-09-16

---

## Recent changes

- opencode installs the same way on both platforms (2026-09-16). opencode
  1.18.30 crashed while building its system prompt, before any request reached a
  model, which turned every `dg task review-run` reviewer into an opaque
  `ERROR(Unexpected server error...)` — the cause was visible only in opencode's
  own log. Recovery on macOS meant leaving devgeta's channel entirely, because
  the Homebrew formula offers only `stable`: no pin, no downgrade. Investigating
  that exposed three further defects on the Debian side, all of which a naive
  macOS switch would have inherited — the install script ran under `sh` though
  its shebang is bash (and on Debian `sh` is dash), the idempotency check asked
  `dpkg -l` about a binary that is never in dpkg so every `dg install` re-ran
  the installer, and uninstall ran `apt-get remove` against a file in
  `~/.opencode`. Both platforms now run the official script with bash and key
  detection, idempotency and uninstall on the binary it writes. devgeta owns the
  `~/.opencode/bin` PATH entry because the script edits the user's own rc files,
  which devgeta does not own and cannot keep consistent. Nothing is pinned; that
  trade, and the cost of executing an unverified script from a moving branch
  head, are recorded in ADR-0047.

- An alias can go missing from a working install, and now devgeta says so
  (2026-09-15). Two unrelated causes with one symptom. `alias cat="bat"` was
  written inside the template's `{{if .Eza}}` block, so `cat` tracked eza
  instead of bat — lost on a machine with bat and no eza, and pointing at a
  missing binary on a machine with eza and no bat. Separately, a shell config
  that loads anything _after_ `devgeta.zsh` gives that file the last word:
  oh-my-zsh's `lib/theme-and-appearance.zsh` runs a plain `alias ls='ls -G'`
  which does not preserve an existing alias, so a devgeta block above it loses
  `ls` to plain `/bin/ls` with no error anywhere, while the same block appended
  at the end keeps eza. `dg configure` now reports that ordering and names the
  lines, but deliberately does not move anything: the shell config belongs to
  the user, and a dotfiles or provisioning tool that generates it would undo
  the move on its next run. Only a line devgeta itself wrote is ever removed
  from that file. eza and bat were also registered as apps — they implemented
  the whole contract but were missing `Name()`/`Kind()`, so `dg configure eza`
  answered "unknown app" and a wrong shell-feature flag could only be repaired
  by a full `dg install`.

- The output-budget runner works on a stock Mac again (2026-09-14).
  `output-budget-run.sh` starts with `#!/usr/bin/env bash`, which on a Mac
  without Homebrew bash is `/bin/bash` 3.2, and since v1.23.0 it used
  `mapfile` (bash 4) plus plain `"${arr[@]}"` on arrays that can be empty,
  which `set -u` rejects before bash 4.4. With `integrations.output_budget`
  on, every wrapped command exited 1 with no output, hiding both its output
  and its real exit status. Lines are now read with a `while read` helper and
  empty arrays are expanded as `${a[@]+"${a[@]}"}`. The behavior tests only
  caught this because the machine running them had no newer bash on PATH, so
  two guards back it now: `TestEmbeddedShellScriptsAvoidBash4OnlySyntax` bans
  bash-4-only syntax in every embedded `.sh`, and
  `TestOutputBudgetRun_WorksUnderBash3` runs the runner under a real
  `/bin/bash` 3 wherever one exists. The new test also caught a smaller,
  older contract violation: a command with no output replayed a lone newline
  instead of nothing.

- `dg archive <source> <destination-dir>` packs a folder onto an external
  drive for a machine move (2026-09-13) — a one-shot archive, not an
  incremental backup (`dg backup` stays reserved for devgeta's own config
  snapshots). Written entirely in Go (`archive/tar` +
  `github.com/klauspost/compress/zstd`), not by shelling out to `tar`: no
  external binary to install on a fresh Mac, one code path on both supported
  platforms, and each file is hashed while it is copied into the archive so
  the checksum manifest costs no extra read of the source. See
  [ADR-0040](decisions/ADR-0040-an-archive-is-written-in-go-not-by-shelling-out-to-tar.md).
  Skips only what's _proven_ regenerable — a `CACHEDIR.TAG` with the exact
  standard signature, a `pyvenv.cfg` virtualenv, or a named folder
  (`node_modules`, `target`, `vendor`, …) next to the manifest file that
  proves a tool generates it, never by folder name or `.gitignore` alone,
  so a `.env` file or a Go project's committed `vendor/` is never silently
  dropped. See
  [ADR-0041](decisions/ADR-0041-an-archive-skips-only-what-is-provably-regenerable.md).
  Refuses to write anything if the scan finds unreadable files or
  undownloaded iCloud placeholders — hours into a large archive is the wrong
  time to discover a permission problem — and every output (archive,
  `sha256sum`-format manifest, skip report) is written to a `.partial` file
  and only renamed into place once all three have synced, so a failed run
  never leaves a file that looks complete but isn't. Verifies by default by
  re-reading the archive from the destination and hashing every entry against
  the manifest. Full design, the tar/zstd/Windows-naming research behind it,
  and the manual cross-OS extraction checklist:
  [2026-09-13-dg-archive cycle doc](plans/cycles/2026-09-13-dg-archive.md).

- A long `--prompt` no longer gets silently dropped by `dg wt create`
  (2026-08-07). Every pane's command was typed into the pane with
  `tmux send-keys` right after creation; macOS/BSD's tty input queue caps at
  1024 bytes, so a write past it discarded the excess **and the trailing
  Enter** while `send-keys` still exited 0 — the window looked fine, the coder
  sat at an empty session, and `dg wt create` reported success. See
  [ADR-0021](decisions/ADR-0021-pane-commands-are-exec-d-not-typed.md). Now
  every tmux call that brings a pane into existence (`CreateWindow`,
  `CreateWindowInSession`, `CreateSessionWithWindow`, `SplitWindow`) carries
  that pane's command as a shell-command (process arguments, ~1 MiB of
  headroom) instead of keystrokes; the paths that write into an
  already-existing pane (`ensureWindow`'s repair branch,
  `launchReviewInLiveWindow`'s idle-shell reuse, `dg wt move`'s retarget)
  still use `send-keys`, now guarded to reject any payload over 1023 bytes
  rather than truncate it silently. The devgeta-owned launch also stopped
  depending on the `cc`/`oc` shell alias — a created pane execs the coder's
  resolved binary path directly, and the alias devgeta still writes into
  `devgeta.zsh` is now rendered from the same `pkg/constants.CoderLaunch`
  recipe the launch itself reads, so the two cannot drift. One user-visible
  consequence of that: the pre-create install check probes the **binary**
  instead of the alias, so a coder installed outside devgeta now passes it and
  launches correctly, and a failure there no longer means "devgeta never
  configured this tool" — only that the tool does not resolve.

- Dedup suppresses duplicate comments, never the verdict (2026-08-07). A review
  approved a PR whose missing route coverage was still live: the finding had
  been deduplicated against an existing Copilot comment raising the same point,
  and "already raised" was then read as "non-blocking". The hole was in
  `/review-pr` step 3 — three of its four drop rules require the concern to be
  handled, and the fourth ("the same point already appears in a review summary
  or a conversation comment") required nothing, so a finding it dropped left the
  verdict entirely. The same step then told the agent to settle that finding's
  journal entry `--as answered`, deleting the concern from the only record that
  would raise it next round. Step 3 now states that dedup decides what you post
  and never the verdict, and forbids settling an entry dropped only because
  someone else raised the same still-live point. Step 6 spells out three cases,
  mirrored in `/approve-pr`: a live blocker **anyone** raised means no approval
  plus an `--event comment` naming the one outstanding item and committing to
  approve once it is addressed; a live non-blocking comment means `LGWC` approve
  naming who left it; a failing check means `LGWC` approve naming the check —
  `/approve-pr`'s call, since `/review-pr` never fetches check status.
  `/review-loop` gets the journal-side half: "already raised" is never grounds
  to settle a finding, stated in step 4 and in the fix subagent's never-do list,
  which the dispatch carries verbatim. Five guard tests in
  `internal/apps/opencode/permissions_test.go` pin the prose, each verified by
  removing the sentence and confirming the test fails.

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
