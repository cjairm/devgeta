# Cycle: Kick a review from `dg ws` with `R`

**Date:** 2026-07-28
**Estimated Duration:** ~5 hours
**Status:** Approved — **Unblocked** (cycle 1 shipped 2026-07-29; per-pane `@dg_agent_state`
with explicit `busy` confirmed in `internal/tooling/worktree/worktree.go`)
**Order:** **Cycle 2 of 2.**
[2026-07-28-agent-activity-notifications.md](2026-07-28-agent-activity-notifications.md) has
shipped.
**Governed by:** [ADR-0005](../../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md)
(ACCEPTED)

> **Why blocked, specifically.** Two reasons, both hard:
>
> 1. This cycle's objective (§3) is that the row reports the review's completion on its own.
>    That signal is built by cycle 1. Manual checks 5 and 7 cannot pass without it, so the
>    cycle cannot reach "done" under CLAUDE.md's implement → verify → test → commit order.
> 2. The split-pane decision in §7 requires ADR-0005's **per-pane** state. If cycle 1 ships
>    window-level state instead, fall back to refuse-when-busy — see §7.
>
> **Step 0 is the exception** and should be run early, ideally alongside cycle 1's Step 0. It
> is pure investigation, touches nothing, and one of its answers (`--agent` resolving a
> user-level agent) can invalidate this entire cycle. Find that out now, not later.

---

## 1. Domain Context

Devgeta ships three reviewer agents in `configs/shared/agents/` — `code-reviewer`,
`document-reviewer`, `skill-reviewer` — deployed to both AI coders. Starting one today is
manual: switch to the worktree's window, launch the coder, remember the agent's name, type
a prompt that scopes the review, wait, and keep checking back.

Every piece of that is already automatable. The reviewer agents know how to scope
themselves — all three instruct the agent to run `devgeta task review-scope` and then
`devgeta task branch-diff` (`code-reviewer.md:38`, `document-reviewer.md:28`,
`skill-reviewer.md:26`), so the kicked prompt does not need to compute or embed a diff. And
OpenCode's CLI takes both an agent and an opening prompt as flags, so the whole thing
collapses to one command.

This cycle turns that into one keystroke from the dashboard: `R` on a worktree row, pick
which kind of review, and walk away. The companion cycle supplies the "it's done" signal,
so the row turns purple when the reviewer finishes.

---

## 2. Engineer Context

**Relevant files:**

- `internal/tui/worktree/model.go:855` — the key switch. `r` is repair, `d`/`D` delete,
  `n`/`N` create; **`R` is free**.
- `internal/tui/worktree/create_flow.go:253` — `enterLayoutPick`. A `FuzzyPicker` over a
  fixed name list, default first; the reviewer picker is the same shape and should be
  written by analogy, not from scratch.
- `internal/tui/worktree/model.go:1087` — `handleRepair`. The template for "act on the
  cursor row, resolve a layout, dispatch async, report via `statusMsg`."
- `internal/tooling/worktree/layout.go:104` — `builtinLayouts()`, and `newLayout` /
  `Pane{Command, Split}`. A pane's `Command` is a string sent to the pane by `send-keys`.
- `internal/tooling/worktree/worktree.go:333` — `buildWindowPanes`, which already handles
  splitting and per-pane launch.
- `internal/tooling/worktree/worktree.go:732` — `ensureWindow`, the create-if-missing path.
- `internal/tooling/worktree/aicoder.go:59` — `OpenCodeCoder.Command()` returns the `oc`
  alias, not the raw binary, so the launch recipe stays in `devgeta.zsh`.

**Verified CLI surface** (from the installed `opencode --help`):

```
--prompt   prompt to use     [string]
--agent    agent to use      [string]
```

Both exist. Whether `--prompt` auto-submits or merely pre-fills the composer is **not**
verified — Step 0 exists to settle that before anything is built on it.

**Testing patterns:** [testing-patterns.md](../../guides/testing-patterns.md). All tmux and
git interaction mocked; `testutil.VerifyNoRealCommands` against the same base the code under
test uses.

```bash
go test ./internal/tui/worktree/ ./internal/tooling/worktree/
make lint
```

---

## 3. Objective

Pressing `R` on a worktree row in `dg ws` and choosing a review type starts that reviewer
agent against the branch, in that worktree, with no further typing — and the row reports
completion on its own.

---

## 4. Scope Boundary

### In Scope

- [ ] `R` keybinding on worktree rows, plus hint-bar and help-popup entries
- [ ] A reviewer picker (code / document / skill) modeled on `enterLayoutPick`
- [ ] A review launch that runs `oc --agent <reviewer> --prompt <text>` in the worktree
- [ ] Correct behavior when the worktree's window already exists and is busy (see the
      open design question in §7 — resolve it before Step 3)
- [ ] Correct behavior when the worktree has no live window (create one)
- [ ] Tests; `docs/spec.md` and the `dg ws` docs updated

### Explicitly Out of Scope

- **Claude Code as the review coder.** OpenCode only, per the maintainer's call. It is also
  the safer default: the reviewer agents' `permission:` frontmatter is enforced by OpenCode
  and ignored by Claude Code (CLAUDE.md, "Accepted differences"), so an auto-kicked review
  is read-only under OpenCode and unrestricted under Claude. Fixing that asymmetry needs
  per-agent frontmatter rendering — an ADR-level change, explicitly not this cycle.
- **`R` on a repo-header or session row.** Worktree rows only; no pickers for repo or
  worktree, since the cursor row already identifies both.
- **Editing the prompt before it fires.** The point is zero typing. A prompt-editing variant
  (`⇧R`?) is a follow-on if it is ever wanted.
- **Non-reviewer agents.** Three fixed choices. A general "run any agent" launcher is a
  different feature.
- **Reading the review's findings in the dashboard.** The dot says done; you attach to read.
- **Any change to the completion signal.** That is entirely the companion cycle's.

**Scope is locked.**

---

## 5. Implementation Plan

### Step 0: Settle the launch mechanics before writing Go

Manually, in a scratch worktree:

```sh
opencode --agent code-reviewer --prompt 'Review this branch.'
```

Answer three questions and record them in this doc before proceeding:

1. Does `--prompt` submit the turn, or pre-fill the composer awaiting Enter? If it
   pre-fills, the launch needs a follow-up `send-keys Enter`, which changes Step 3.
2. Does `--agent` resolve a user-level agent from `~/.config/opencode/agent/`, or only a
   project-level one? If only project-level, the reviewer agents' deploy target has to
   change — a much larger scope, so stop and re-plan.
3. Does the reviewer, launched this way, actually run `devgeta task review-scope` on its own?

This step is deliberately first. Everything downstream assumes all three answers are
favorable, and each has a cheap check and an expensive discovery-later cost.

**Answers (2026-07-29, verified manually in a scratch worktree via tmux
`send-keys`/`capture-pane`, matching how the real feature will launch it):**

1. **`--prompt` submits the turn**, no follow-up `Enter` needed. The prompt appeared in the
   transcript and the agent began responding within the same capture that showed the command
   land — no composer pre-fill state was observed.
2. **`--agent` resolves a user-level agent.** `~/.config/opencode/agents/code-reviewer.md`
   (deployed there by `dg configure opencode`) loaded correctly; the status bar showed
   "Code-Reviewer · GPT-5.6 Sol" and `tab agents` in the footer. No project-level agent
   directory existed in the scratch clone, so this could only have come from the user-level
   path.
3. **The agent ran `devgeta task review-scope` on its own**, unprompted, as its first todo
   item ("Run review-scope and identify branch intent and changed files"), and the pane
   showed the actual command output (branch, commit list, changed files) before moving to
   the next todo (reading repo standards).

All three favorable. No fallback needed; proceeding as planned.

### Step 1: Reviewer registry

Add a small registry beside `builtinLayouts()` in `layout.go` — reviewer key → agent name
and human label:

| Key        | Agent               | Label                   |
| ---------- | ------------------- | ----------------------- |
| `code`     | `code-reviewer`     | code — bugs, security   |
| `document` | `document-reviewer` | document — plans, specs |
| `skill`    | `skill-reviewer`    | skill — agents/commands |

Names must match the filenames in `configs/shared/agents/`. Assert that with a test rather
than a hand-copied list — CLAUDE.md's "Changing an embedded config" rule item 4: a
constraint an external tool imposes gets enforced by a test, because a comment will not
survive future edits. If someone renames an agent file, this must fail the build rather
than ship an `--agent` flag naming an agent that no longer exists.

**Where the test lives matters.** `ConfigsFS` is declared in `embedded.go`, which is
**package main** at the repo root, so `internal/tooling/worktree` cannot import it. The repo
already has an established two-part pattern for exactly this — see the comment at
`cmd/task_redirect_hook_test.go:11-18`:

- The in-package test (here, `internal/tooling/worktree`) reads the agent files **off disk**
  via a relative path (`filepath.Join("..", "..", "..", "configs", "shared", "agents")`),
  with a comment explaining the import boundary. This is sound because `//go:embed
all:configs` embeds the files byte-for-byte with no transformation.
- A companion test in **package main** (root, beside `task_redirect_test.go`) asserts the
  same names against the embedded `ConfigsFS`, so a file that fails to embed is still caught.

Do both. The on-disk test alone would pass even if the file never made it into the binary.

- Verify: `go test ./internal/tooling/worktree/ .` (the trailing `.` runs the root package)

### Step 2: Build the review pane command

A function producing the pane command for a reviewer, reusing `OpenCodeCoder.Command()` for
the launch token rather than hardcoding `oc` or `opencode`.

The prompt is short, because the agent already carries its own instructions and scoping
procedure. Something on the order of: `Review this branch against the default branch.`

**Shell quoting is a correctness requirement, not a detail.** The command goes to
`send-keys`, which types it into an interactive shell. The prompt must be single-quoted and
any embedded quote escaped. Keep the prompt free of shell metacharacters and add a test
asserting the built command survives a round trip.

- Verify: `go test ./internal/tooling/worktree/` with cases for quoting.

### Step 3: Launch into the worktree

Two cases:

- **No live window** — create one whose single pane is the review command. `ensureWindow`
  already does create-if-missing with a `Layout`; a review `Layout` is one `Pane`.
- **Live window** — split a new pane and launch there (§7). Never `send-keys` the review
  command into a pane already running a coder's TUI; that types it into the composer.

**Rollback must not reuse the existing path.** `buildWindowFromLayout`
(`worktree.go:308`) rolls back a failed launch with `KillWindow` plus a worktree removal —
correct for a window it just created, catastrophic here, where the window is the user's and
already holds their work. A failed review launch must undo **only what it added**:

1. Capture the new pane's `pane_id` from `SplitWindow` (or an `ActivePaneID` call
   immediately after, the way `buildWindowPanes` captures pane 0 at `worktree.go:341`).
2. On any failure after the split, kill **that pane id** and nothing else.
3. Never touch the worktree — `R` does not create one, so it has none to roll back.

`Tmux` has no kill-pane wrapper today. Add one rather than reaching around it (CLAUDE.md §6,
"Route external tools through their app wrappers"); same for anything `SplitWindow` does not
already expose.

- Verify: `go test ./internal/tooling/worktree/`, all mocked. Include a test where the launch
  fails after a successful split and assert the window survives with only the new pane killed.

### Step 4: The `R` handler and picker

`handleKickReview` on the cursor row, modeled on `handleRepair`: bail if the selection is not
a worktree, set an in-progress `m.status`, dispatch async, report via `statusMsg`.

The picker follows `enterLayoutPick`: a `FuzzyPicker` over the three labels, `code` first as
the common case. Add a `reviewPick` mode alongside `createMode`'s values, or a sibling field
— match whichever the existing code makes cleaner; do not invent a second state machine.

Guard re-entry the way `startNewWorktree` guards with `m.creating` (`create_flow.go:89`), so
a second `R` while a launch is in flight is a no-op.

- Verify: `go test ./internal/tui/worktree/`

### Step 5: Hints and help

Add `{Key: "R", Desc: "review"}` to the hint bar (`model.go:1472`) and a fuller line to the
help popup (`model.go:1506`). The hint bar is width-constrained — check that the new entry
does not push others off at a narrow terminal.

- Verify: `go test ./internal/tui/worktree/`; run `dg ws` at 80 columns.

### Step 6: Docs

`docs/spec.md` for the feature, the `dg ws` keybinding table, and a note in the reviewer
agents' docs that they can be launched this way.

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/tui/worktree/ ./internal/tooling/worktree/
go test ./... -cover
make lint
```

### Manual

1. `make build` first — `dg configure` extracts configs from the running binary.
2. `dg ws` → cursor on a worktree with real commits on its branch → `R` → pick `code`.
3. The window opens, OpenCode starts on `code-reviewer`, and the review runs with no typing.
4. The reviewer runs `devgeta task review-scope` itself (confirms Step 0's answer 3).
5. Detach. Within one refresh the row shows the purple `◆` from the companion cycle.
6. `R` on a worktree whose window is already running a coder → a new pane splits and the
   review starts there. Nothing is typed into the running coder's composer.
7. **With that coder mid-turn, let the review finish** → the row shows `◆`. This is the
   case the window-level design could not do; it is the reason for ADR-0005's revision.
8. **Force a launch failure after the split** (e.g. rename the `oc` alias) → the new pane is
   gone, the user's coder pane and window are untouched, and the status line says why.
9. `R` on a repo header row and on a session row → no-op, no crash.
10. `R` on a brand-new worktree with no commits → the reviewer reports an empty diff cleanly
    rather than erroring.

### Regression Check

- `r` still repairs; `n`/`N`/`s`/`d`/`D` unchanged.
- Worktrees created before this cycle behave identically.

---

## 7. Risks & Trade-offs

### Resolved: `R` into a window that already has a coder

**Decision: split a new pane.** This was an open question in the first draft; review found
the reason it was open, and the fix landed in the companion cycle rather than here.

`send-keys` into a live OpenCode TUI types the text into its composer instead of running it,
so the review needs a pane of its own. Four options:

| Option                                      | Cost                                                                                |
| ------------------------------------------- | ----------------------------------------------------------------------------------- |
| **Split a new pane in the existing window** | Keeps 1 window ↔ 1 worktree; needs per-pane state (now the case)                    |
| Open a second, review-only window           | Breaks the 1:1 window↔worktree assumption `GetWindowName` and the dashboard rely on |
| Kill and rebuild the window                 | Destroys an in-progress session. Unacceptable                                       |
| Refuse unless the window is free            | Simplest, but makes `R` unavailable exactly when you are mid-work                   |

**Why it is safe now.** The first draft recommended the split while the companion cycle
stored agent state per _window_ with an implicit "busy". Those two are incompatible: the
working coder and the reviewer would overwrite each other's value, so the row could not
reliably report that the review finished — which is this cycle's entire objective (§3).

[ADR-0005](../../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md) was
revised to store state **per pane** with an explicit `busy` value, verified on tmux 3.5a to
cost the same single exec the window-level design did. The reviewer's pane now holds its own
state that nothing else can clobber, and the row's aggregation rule ranks `idle` above
`busy`, so a finished review shows `◆` even while the coder beside it keeps working.

**What this cycle must not do:** proceed if that ADR revision is rejected. Per-pane state is
a hard prerequisite, not a nice-to-have. If the companion cycle ships window-level state
after all, fall back to "refuse unless the window is free" — an honest limitation beats a
dot that lies.

A fifth option exists and is worth knowing about but not doing here: OpenCode's SDK defines
`tui.prompt.append` and `tui.command.execute` events, so a plugin could inject a prompt into
an already-running TUI. That needs a server connection and a plugin round trip — real
machinery for a case a pane split handles.

### Remaining open question

**Does `◆` on a row mean "the review finished" or "some agent in this window finished"?**
Strictly the latter: one dot aggregates every pane. With a reviewer and a coder in one
window, `◆` tells you something wants you, not which. That is accepted for v1 — the state is
collected per pane, so per-pane display is a later cycle with no rework. Flag it if the
distinction needs to be visible in v1, because that changes the row renderer, not the storage.

### Risks

| Risk                                                     | Likelihood | Mitigation                                                                   |
| -------------------------------------------------------- | ---------- | ---------------------------------------------------------------------------- |
| `--prompt` pre-fills rather than submits                 | Med        | Step 0 settles it before any code; fallback is a follow-up `Enter`           |
| `--agent` will not resolve a user-level agent            | Low        | Step 0; if it fails, stop and re-plan — the deploy target must change        |
| Prompt text broken by shell quoting through `send-keys`  | Med        | Single-quote, keep metacharacters out, round-trip test in Step 2             |
| Agent filename renamed, `--agent` silently wrong         | Med        | Step 1's paired on-disk + embedded-FS tests fail the build                   |
| Failed launch leaves an orphan pane in the user's window | Med        | Step 3 kills only the captured pane id; never the window. Explicit test      |
| ADR-0005's per-pane revision is rejected                 | Low        | Hard prerequisite (§7). Fall back to refuse-when-busy rather than ship a lie |
| Review kicked on a worktree with nothing to review       | High       | Harmless — the agent reports an empty diff. Manual check 8                   |

### Trade-offs Made

- **Cursor row over pickers.** `R` acts on the selected row, like `r` and `d`, rather than
  running repo and worktree pickers. Fewer keystrokes and consistent with every other
  row action; the cost is that `R` does nothing useful from a repo header row.
- **Fixed prompt, no editing.** Zero typing is the whole feature. The reviewer agents carry
  their own instructions, so a per-invocation prompt would mostly duplicate them.
- **OpenCode only.** Deliberate, and better than parity here: the reviewer agents are
  actually read-only under OpenCode and would not be under Claude Code.

---

## 8. Cross-Model Review Notes

- [ ] Should `R` fall back to `r`'s repair behavior when the window is missing, or is
      creating a review-only window there confusing?
- [ ] Is a three-item `FuzzyPicker` overkill versus a which-key style single-keypress prompt
      (`c` / `d` / `s`)?
- [ ] Does the fixed prompt need to differ per reviewer, or is one line enough for all three?
- [ ] Should the split be vertical or horizontal, and should the reviewer pane take focus or
      leave the user in the coder pane they were in?

**Reviewer notes:**

**Round 1 (2026-07-28).** Two findings landed here; both verified and accepted.

- **Pane-split vs. window-level state (rated CRITICAL, correct).** The split recommendation
  and the companion cycle's window-level, last-writer-wins state genuinely could not coexist,
  and the objective at §3 is what broke. Fixed at the storage layer instead of by retreating:
  ADR-0005 now stores per pane with an explicit `busy`, verified on tmux 3.5a to cost the same
  single exec. The reviewer's suggested fallback (refuse while another coder runs) is recorded
  in §7 as the contingency if that revision is rejected.
- **`ConfigsFS` package boundary (correct).** `embedded.go` is package main; the planned test
  location could not have compiled. Step 1 now follows the pattern documented at
  `cmd/task_redirect_hook_test.go:11-18` — on-disk read in the internal package, plus a
  companion embedded-FS assertion in the root package so a file that fails to embed is caught.
- **Pane rollback (correct, and worse than reported).** Not merely undefined: the existing
  `buildWindowFromLayout` rollback calls `KillWindow` _and_ removes the worktree, which
  against a user's live window would destroy their session. Step 3 now requires capturing the
  new pane id and killing only it, and adds a kill-pane wrapper rather than reaching around
  the tmux app wrapper.
