# Cycle: ws dashboard — agents get their own section

**Date:** 2026-09-28
**Estimated Duration:** ~3–4 days (12 steps, most of them in the 110 KB `model.go`)
**Status:** Done

Decisions: [ADR-0055](../../decisions/ADR-0055-an-agent-says-what-it-is.md) ·
[ADR-0056](../../decisions/ADR-0056-the-dashboard-lists-agents-in-their-own-section.md) ·
[ADR-0057](../../decisions/ADR-0057-the-dashboard-takes-its-pane-move-keys-from-tmux.md).
Revises [ADR-0008](../../decisions/ADR-0008-agent-state-on-every-pane-row.md) and
[ADR-0050](../../decisions/ADR-0050-dashboard-view-state-lives-in-a-tmux-server-option.md).

Mockups: https://claude.ai/artifact/K9jS9aw5yyRZkNNbKUzJ3d

---

## 1. Domain Context

`dg ws` lists repos, worktrees and tmux sessions, and marks each row with the agent state
its panes report ([ADR-0005](../../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md),
[ADR-0008](../../decisions/ADR-0008-agent-state-on-every-pane-row.md)). Every window gets
a status glyph, including ones that only run Neovim or a shell. Finding the agent that
needs you means reading dot colors across the whole tree.

This cycle splits the left column into **spaces** (the tree, with no markers) and
**agents** (one flat list of every agent pane, most urgent first, with a state word and the
coder's name). Folding, the split, filtering and pane-move keys work across both sections.

The stale-dot bug that started this conversation shipped separately in `3013bd0`
(ADR-0005's 2026-09-28 revision). It is not part of this cycle.

---

## 2. Engineer Context

- **Relevant files:**
  - `internal/tui/worktree/model.go`: keys (`handleKey`, `handleDiffKey`), `rebuildRows`,
    `placeCursorOnActive`, rendering (`renderLeft`, `renderWorktreeRow`,
    `renderSessionLikeRow`, `renderPaneRow`, `renderHint`, `renderHelpPopup`), the existing
    divider drag (`tea.MouseClickMsg` / `MouseMotionMsg` / `MouseReleaseMsg`).
  - `internal/tui/worktree/tree.go`: `buildRows`, row kinds, `rowKey`, the repo
    `agentState` aggregate.
  - `internal/tui/worktree/viewstate.go`: `viewStateV1` (ADR-0050).
  - `internal/tui/components/`: `statusdot.go` (state → glyph/color), `listnav.go`,
    `VisibleWindow`, `selection.go` (`Palette.SoftSelectedLine`, the selection stripe).
  - `internal/apps/tmux/tmux.go`: `PaneState`, `PaneStates()` / the fast scan format and its
    trailing-field tolerance, `ClearAgentStateForPane`.
  - `configs/claude/agent-state.sh` + `configs/claude/settings.json.tmpl`: the Claude hooks.
  - `configs/opencode/plugin/notify.js`: the OpenCode plugin. Its tests are
    `notify.test.mjs` **at the repo root**, not next to the plugin.
  - `configs/tmux/tmux.conf.tmpl`: `is_vim` and the `C-h/j/k/l` bindings.
- **Key rules:**
  - ADR-0024: the fast tick (3s) is tmux only. Agent rows come from the pane layer it already
    scans; no new exec per tick.
  - Every tmux call goes through the tmux wrapper (CLAUDE.md §6).
  - Both coders change together: CLAUDE.md "Keeping the two AI agents in sync",
    [docs/guides/agent-sync.md](../../guides/agent-sync.md).
  - Tests use `testutil.MockApp` and the model's injected `...Fn` seams; never real tmux.
- **Tests:** see §6.

---

## 3. Objective

`dg ws` shows every agent pane in its own section, sorted by who needs you most, with the
coder's name and a state word. Space rows carry no status markers, and you can fold,
resize, filter and move between the two sections with the keys you already use in
tmux/Neovim.

---

## 4. Scope Boundary

### In Scope

- [x] Coders write `@dg_agent_kind` on start and every state write, and unset it on end (ADR-0055)
- [x] The fast scan reads the kind; "is an agent" = kind set and not a plain shell
- [x] Agents section: flat list, urgency sort, two-line rows, `↵` jumps to the pane, diff on the right
- [x] State words: blocked / error / done / working / idle (display only; stored values unchanged)
- [x] Spaces: no status markers on worktree, session or pane rows; repos with no open window start folded, and an explicit unfold is saved
- [x] Fold keys `a` (agents) / `w` (spaces); folded bar at the bottom, with per-state counts for agents
- [x] Split: `+` / `-` and mouse drag on the agents header; each section scrolls on its own
- [x] Filter searches only open sections; agents match location, coder name, state word
- [x] Saved view state: `expanded`, both section folds, split (ADR-0050, still `v: 1`; the cursor is still not saved)
- [x] Pane-move keys read from `tmux list-keys`; in-dashboard moves plus edge hand-off; never while a text input has focus (ADR-0057)
- [x] Shipped tmux bindings pass the keys through to `devgeta ws`
- [x] Help popup, hint bar, and the docs listed in File Changes

### Explicitly Out of Scope

- Opening the dashboard as a tmux popup, or as a left split (a later, separate decision)
- A live preview of the agent's screen in the right pane (dropped during design)
- An alert history, or saving error logs to a file (a separate feature; gets a ROADMAP entry)
- Configuring the dashboard's keys in devgeta's own config (keys come from tmux for now)
- Grouping agents by repo (a flat urgency list was chosen)
- Coders other than Claude Code and OpenCode

**Scope is locked.** Anything else found along the way goes to a follow-up.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                                                                                                           | Description                                                                                                                                                                                            |
| ------ | ----------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Modify | `configs/claude/agent-state.sh`                                                                                                     | Write `@dg_agent_kind claude` with every state write (one `tmux` call via `\;`); new `start` / `end` arguments                                                                                         |
| Modify | `configs/claude/settings.json.tmpl`                                                                                                 | Register `SessionStart` → `agent-state.sh start`, `SessionEnd` → `agent-state.sh end`                                                                                                                  |
| Modify | `configs/opencode/plugin/notify.js`, `notify.test.mjs`                                                                              | Write the kind on load and with every state write; unset kind + state and recompute the mirror on `server.instance.disposed` and on process exit                                                       |
| Modify | `internal/apps/tmux/tmux.go` (+ test)                                                                                               | `PaneState.Kind` from `#{@dg_agent_kind}`; `IsAgent()`; one shell-name constant; `RootPaneMoveKeys()` from `list-keys -T root`; rewrite the `CurrentCommand` field comment (ADR-0055 narrows ADR-0008) |
| Modify | `internal/tui/components/statusdot.go` (+ test)                                                                                     | Agent state words and glyphs, including done vs idle                                                                                                                                                   |
| Create | `internal/tui/worktree/agents.go` (+ test)                                                                                          | Agent rows from the pane layer: filter, label, urgency sort                                                                                                                                            |
| Modify | `internal/tui/worktree/tree.go`                                                                                                     | Stop rendering the repo `agentState` aggregate; default-fold repos with no open window, once per repo per launch                                                                                       |
| Modify | `internal/tui/worktree/model.go`                                                                                                    | Two sections: cursor, scroll, folds, split, filter scope, keys, rendering                                                                                                                              |
| Modify | `internal/tui/worktree/viewstate.go` (+ test)                                                                                       | Optional `expanded`, `agentsFolded`, `spacesFolded`, `split` fields                                                                                                                                    |
| Modify | `configs/tmux/tmux.conf.tmpl` (+ golden, + test)                                                                                    | `is_dgws` check next to `is_vim` in the four `C-h/j/k/l` bindings                                                                                                                                      |
| Modify | `docs/decisions/ADR-0008-…`, `docs/decisions/ADR-0050-…`                                                                            | Revision notes (written with the proposal)                                                                                                                                                             |
| Modify | `docs/spec.md`, `docs/apps/claude.md`, `docs/apps/opencode.md`, `docs/guides/agent-sync.md`, `docs/recent-changes.md`, `ROADMAP.md` | Document the section, the kind option, the keys; ROADMAP line for alerts / error logs                                                                                                                  |

### Step-by-Step

Every step follows red → green: write the failing test, watch it fail, then implement.

#### Step 0: Check the facts the design leans on

Throwaway tmux server (`tmux -L`, as for the stale-dot fix) and real coders. No shipped code.

- Claude Code: `SessionStart` and `SessionEnd` run with `$TMUX_PANE` set; `SessionEnd`
  runs on `/exit` and on ctrl+c-twice.
- OpenCode: the plugin factory runs at startup; `server.instance.disposed` fires on quit;
  whether a process `exit` handler can still run a synchronous `tmux` call.
- `pane_current_command` while each coder is idle, while it's working, **while Claude Code
  runs a Bash tool command** (a foreground child shell would make the shell backstop hide a
  live agent), and after it exits (should be the shell).
- `tmux list-keys -T root` output for the shipped `if-shell` bindings, and for a plain
  `bind -n M-h select-pane -L`.
- A `ps` pattern that matches `devgeta ws` on a pane's tty and not `devgeta install`.
- Record any surprise in the relevant ADR before Step 1. If the Bash-tool check shows a
  shell name, stop and revisit ADR-0055's backstop before building on it.

#### Step 1: Coders report their kind (ADR-0055)

- `agent-state.sh`: every state write also sets `@dg_agent_kind claude` in the same `tmux`
  invocation; `start` writes only the kind; `end` unsets kind and state and recomputes the
  window mirror with the focus hooks' rule. Register the two new hooks.
- `notify.js`: the same three behaviors, with the same "never throw" tolerance.
- Tests: the root package's hook tests for `agent-state.sh` (stubbed `tmux`); `notify.test.mjs`.
- Verify: `go test .` and `node --test notify.test.mjs` (from the repo root).

#### Step 2: The scan reads the kind

- Append `#{@dg_agent_kind}` as the **last** field of the pane scan format. `ExecCommand`
  trims trailing whitespace off the whole output, so the final line can lose one tab (kind
  unset) or two (state and kind both unset). The parser must accept 5, 6 and 7 fields.
  **A 6-field line now means "state set, kind unset"**: that's the shape of every pane
  written by a coder from before this change, and it must parse that way, not as a malformed
  line.
- `PaneState.Kind`; `IsAgent()` = kind set and `CurrentCommand` not in the shell-name
  constant (`sh`, `bash`, `zsh`, `fish`, `dash`). ADR-0055 points at the constant rather
  than repeating it.
- Rewrite the `CurrentCommand` field comment: still never used to detect an agent, only to
  rule one out (ADR-0055, ADR-0008 revision).
- Tests: 5 / 6 / 7-field lines, including the last line of the output trimmed in each shape;
  `IsAgent` cases.
- Verify: `go test ./internal/apps/tmux/ ./internal/tooling/worktree/`.

#### Step 3: State words

- `statusdot.go`: map `(isAgent, state)` → word + glyph + color, with done (`idle`) ≠ idle (unset).
- Tests: the table in ADR-0055.

#### Step 4: Agent rows

- `agents.go`: build agent rows from the pane layer. The label is `repo/worktree` for a
  worktree window, otherwise the session name. Sort by urgency, then label, then pane index
  (ADR-0056). Row identity (`rowKey`) is the pane id.
- Tests: ordering including ties, labels, a non-agent pane skipped, two agents in one window.

#### Step 5: Two sections on screen

- Render the agents section under spaces: yellow header with count for the focused section,
  a rule between the two, two-line agent rows with one selection stripe. `↵` on an agent row
  uses the pane-row switch. The right pane shows the agent's worktree diff.
- `j` / `k` walk across the boundary between sections; the cursor clamps within visible
  rows (the `rebuildRows` clamping comment still applies).
- Tests: rendering snapshots for A1 in the mockups; `↵` on an agent calls the pane switch.

#### Step 6: Quiet spaces and default folds

- Remove state glyphs from worktree, session, repo-session and pane rows.
- Default-fold a repo with no open window **once**, the first time this launch sees the repo
  (first worktree load, or the repo appearing later), and only if the saved state has no
  explicit choice for it. Later `rebuildRows` calls never apply the default again.
- Unfolding a default-folded repo records it in `expanded`; folding records it in
  `collapsed`, as today.
- Tests:
  - A windowless repo starts folded on a fresh launch.
  - The user unfolds it, the 3-second tick rebuilds, and it stays open.
  - A saved `expanded` entry opens it on the next launch.
  - A repo that loses its last window mid-launch is not re-folded.

#### Step 7: Folding sections

- `a` / `w` toggle folds; a folded section becomes a bottom bar (the agents bar keeps
  per-state counts); the last open section can't fold; the cursor moves to the open section.
- Tests: each key from each section; last-section refusal; bar counts.

#### Step 8: Split and scroll

- Per-section scroll via `VisibleWindow`; `+` / `-`; drag on the agents header line, reusing
  the divider-drag pattern (write the saved state on release only, ADR-0050).
- Tests: split bounds (each open section keeps at least one row); drag start/move/release.

#### Step 9: Moving with the tmux keys (ADR-0057)

- `RootPaneMoveKeys()` parses `list-keys -T root` (both binding forms), falling back to
  `ctrl+h/j/k/l`. The model maps the four directions per ADR-0057's table; at an edge it
  calls `select-pane` through the wrapper. While a text input has focus, the keys go to the
  input and nothing moves.
- `tmux.conf.tmpl`: add an `is_dgws` check to the four bindings; regenerate the golden.
- Tests: parser fixtures; each direction from list and diff pane; folded section treated as
  an edge; edge hand-off calls `select-pane`; `ctrl+h` in the filter reaches the input and
  never calls `select-pane`; a config test that each `C-h/j/k/l` binding passes through for
  `devgeta ws`.
- Verify: `go test ./internal/apps/tmux/ ./internal/tui/worktree/ .`

#### Step 10: Filter and saved view state

- `/` filters only open sections; agents match location, coder name and state word.
- `viewStateV1` gains the optional fields. An old value reads as "both open, default split,
  no expanded repos". The cursor is still not saved; with spaces folded it starts on the
  first agent.
- Tests: filter per fold combination; round-trip; old-value decoding; a value with the new
  fields decoded by today's struct (to prove older binaries still read it).

#### Step 11: Help, hints, docs

- Help popup and hint bar list `a`, `w`, `+/-` and the move keys. Update the docs in File
  Changes, including the ROADMAP line.

---

## 6. Verification Plan

### Automated Verification

The changed packages plus their direct importers (CLAUDE.md §6):

```bash
go test ./internal/tui/worktree/ ./internal/tui/components/ ./internal/tui/inventory/ \
        ./internal/apps/tmux/ ./internal/tooling/worktree/ ./internal/tooling/terminal/ \
        ./internal/apps/registry/ ./cmd/
go test .                    # Steps 1 and 9 change configs/ and hook scripts (~5 min)
node --test notify.test.mjs  # from the repo root; the plugin's tests
make lint
```

Steps 1 and 9 put the root package in the run, which costs close to a full run. Say so
when running it.

### Manual Verification

Rebuild, reinstall, then `dg configure claude --force`, `dg configure opencode --force` and
`dg configure tmux --force`.

1. Start one Claude Code and one OpenCode agent, with no prompt yet: both are listed as **idle**, with the right coder name.
2. Prompt each one: **working**, then **done** when it finishes; open it and it becomes **idle**.
3. Trigger a permission prompt: **blocked**, and it's first in the list.
4. `/exit` a coder: its row disappears, and the window's status-bar dot clears.
5. `a` / `w` fold in any order, the counts on the agents bar are right, and the last open section refuses to fold.
6. Resize with `+` / `-` and by dragging the agents header, then quit and reopen: folds, split and `expanded` repos come back.
7. Unfold a windowless repo and wait more than 3 seconds: it stays open.
8. `/blocked` and `/opencode` in agents; with a section folded, only the open one is searched.
9. `ctrl+h/j/k/l` inside the dashboard, and out of it from a split; `ctrl+h` while filtering edits the filter.

### Regression Check

- Worktree actions on space rows work as before: `↵`, `n` / `N`, `s`, `d` / `D` / `F`, `r`, `R`, `$`, rename, and pane rows.
- Dragging the left/right divider, `e`, `space` / `esc`, and the diff scroll keys work as before.
- Pane moves outside the dashboard work as before: `ctrl+h/j/k/l` in Neovim, in a shell, and in `devgeta install`.
- The stale-dot fix (`3013bd0`) still clears state on focus.
- A saved view state from before this change still restores its folds and width.

---

## 7. Risks & Trade-offs

| Risk                                                                                      | Likelihood        | Mitigation                                                                                                                                                        |
| ----------------------------------------------------------------------------------------- | ----------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| One more `ps` spawn per pane-move key in every non-Vim pane                               | High (by design)  | Same cost model as `is_vim` today. Check `is_dgws` only after `is_vim` fails. Step 0 measures it; if the lag is noticeable, revisit ADR-0057's rejected pane flag |
| Move keys arriving while a text input has focus move you out mid-typing                   | Med               | ADR-0057: inputs keep the keys; Step 9 test                                                                                                                       |
| The shell backstop hides a live agent whose foreground process is a shell                 | Low–Med           | Step 0 checks it, including a Claude Code Bash tool run; stop and revisit ADR-0055 if it happens                                                                  |
| Stale agent row after a coder is `SIGKILL`ed and a non-shell program takes the foreground | Low               | Accepted in ADR-0055; the row goes when that program exits or the pane closes                                                                                     |
| Default folds fight explicit unfolds                                                      | Low once designed | Default applied once per repo per launch; unfolds saved in `expanded`; Step 6 tests                                                                               |
| Two-section cursor handling breaks existing flows in `model.go`                           | Med               | Regression list above; the existing model tests stay green at every step                                                                                          |
| The seventh scan field misparses the last pane in the output                              | Low               | Step 2 tests every trimmed shape                                                                                                                                  |

### Trade-offs Made

- **Coder writes its name vs guessing from the process:** we chose the coder's own word
  (ADR-0055). The cost is two coder integrations to keep in sync.
- **Process match vs pane flag for key passthrough:** we chose the process match
  (ADR-0057). The cost is a `ps` spawn per key, in exchange for no stale flag that would
  turn `ctrl+h` into a backspace.
- **Sections in one dashboard vs a tmux sidebar:** we chose one dashboard (ADR-0056). The
  cost is resizing with dashboard keys instead of tmux ones.
- **Version stays `1`:** we chose additive optional fields. The cost is that an older
  binary writing the state drops the new fields while two versions run side by side.
