# Cycle: Pane commands are exec'd at creation, not typed

**Date:** 2026-08-07
**Estimated Duration:** ~6-8 hours
**Status:** Draft

---

## 1. Domain Context

`dg wt create` builds a worktree plus a tmux window and fills that window from a
**layout** — a named list of panes, each with a command (an AI coder, `nvim`, or
nothing for a plain shell). `dg ws` uses the same machinery for repair and for
launching a reviewer with `R`.

Today every pane's command is **typed into the pane** with `tmux send-keys` right
after the pane is created. That is why `dg wt create --prompt '<long text>'`
silently loses the prompt: the macOS/BSD tty input queue caps at 1024 bytes, and
the terminal driver discards everything past it — the rest of the command **and
the Enter** — while `tmux send-keys` still exits 0. The window comes up looking
correct with an AI coder sitting at an empty session, and `dg wt create` reports
success.

[ADR-0020](../../decisions/ADR-0020-pane-commands-are-exec-d-not-typed.md)
(ACCEPTED) is the decision this cycle implements: **any tmux call that brings a
pane into existence carries that pane's command**, as a shell-command (process
arguments) instead of keystrokes. Read it before starting — it carries the
measurements behind every rule here, and several of those rules exist because an
earlier draft got them wrong.

Related: [ADR-0011](../../decisions/ADR-0011-agent-prompt-as-launch-argument.md)
(a prompt is a launch argument; quoting rules),
[ADR-0016](../../decisions/ADR-0016-inconclusive-tool-probe-fails-open.md) (an
inconclusive probe must never block a create).

---

## 2. Engineer Context

**Relevant files:**

- `internal/apps/tmux/tmux.go` — the tmux wrapper. `CreateWindow`,
  `CreateWindowInSession`, `CreateSessionWithWindow`, `SplitWindow`,
  `SendKeysTo*`, and `ExecuteCommand` (which builds `cmd.CommandParams`).
- `internal/commands/shell_lookup.go` — `ShellCommandLookupFn`, the interactive
  `command -v` probe. Three-valued result (`Found` / `NotFound` /
  `Inconclusive`), classified from a marker line on stdout.
- `internal/tooling/worktree/layout.go` — `Pane` (`Command`, `Split`, and the
  unexported `check` / `prompt`), `Layout`, `coderPane` / `nvimPane` /
  `shellPane`, `Layout.EnsureInstalled`, `clone`, `WithPrompt`,
  `WithExtraPanes`, `ReviewCommand`, `shellSingleQuote`.
- `internal/tooling/worktree/aicoder.go` — `AICoder` (`Command`,
  `PromptCommand`, `EnsureInstalled`), `OpenCodeCoder`, `ClaudeCoder`,
  `ensureToolInstalled`.
- `internal/tooling/worktree/worktree.go` — `validateLayout` (:313, called from
  4 sites), `buildWindowFromLayout` (:518), `buildWindowPanes` (:543),
  `retargetWindowAfterMove` (:1717, its send-keys at :1741),
  `LaunchReviewInRepo` (:1952),
  `launchReviewInLiveWindow` (:2034), `ensureWindow` (:2101),
  `createWindowWithLayout` (:2143).
- `configs/templates/devgeta.zsh.tmpl` — `alias oc=opencode`,
  `alias cc="CLAUDE_CODE_NO_FLICKER=1 claude"` (lines ~179, ~183).

**Key mechanics to know before touching anything:**

- `buildWindowPanes` takes a `sendKeys func(command string) error` closure so the
  same builder serves the current-session and named-session paths. That seam is
  what changes shape in this cycle.
- Pane commands are built strictly in order (pane 0, then split, then the next),
  relying on `split-window` making the new pane active. That ordering property
  survives this cycle unchanged.
- `Layout` is resolved once and created repeatedly by `dg ws`, which is why
  `clone` exists. Anything this cycle stores on a resolved layout inherits that
  hazard — see Risks.

**Testing patterns:** [testing-patterns.md](../../guides/testing-patterns.md).
Always `testutil.MockApp`; never a real command. Note that
`internal/tooling/worktree/window_build_test.go` asserts **ordered sequences** of
`commands.ExecCommandResult(...)` — changing how many tmux calls a build makes
will break those sequences, and that is the intended signal, not noise.

**Commands:**

```bash
go build ./...
go test ./internal/apps/tmux/
go test ./internal/commands/
go test ./internal/tooling/worktree/
go test ./...
make lint
```

---

## 3. Objective

Every pane devgeta creates receives its command at creation time as a tmux
shell-command, so a `--prompt` (or any pane command) of any length arrives
intact; the paths that legitimately send into an already-live pane keep
`send-keys` but refuse to truncate silently.

---

## 4. Scope Boundary

### In Scope

- [ ] `CreateWindow`, `CreateWindowInSession`, `CreateSessionWithWindow`,
      `SplitWindow` accept an optional pane command
- [ ] `SendKeysTo*` reject a payload over 1023 bytes with an actionable error
- [ ] `ShellCommandLookupFn` can return the resolved absolute path
- [ ] Shell resolution (`$SHELL` → tmux `default-shell` → `/bin/sh`, each
      validated as an existing executable regular file) and the two launch
      recipes (resolved-path non-interactive; alias-based interactive fallback)
- [ ] `Pane.check` / `Layout.EnsureInstalled` carry the probe's result to the
      launch instead of returning only `error`
- [ ] The three create paths in `worktree.go` pass commands at creation
- [ ] `launchReviewInLiveWindow`'s **split** branch execs; its idle-shell-reuse
      branch keeps `send-keys`
- [ ] `LaunchReviewInRepo`'s create path builds its pane through a constructor,
      not a bare `Pane{}` literal
- [ ] `devgeta.zsh` alias lines generated from the same Go constant as the launch
      recipe, pinned by a test against the embedded configs FS
- [ ] Tests for all of the above; `docs/spec.md` updated if user-visible behavior
      changes

### Explicitly Out of Scope

- Making **repair** able to carry a long command. It sends into a live pane by
  nature; ADR-0020 bounds it with the 1023-byte guard and says a repair that ever
  needs to carry a prompt needs its own decision.
- Adding a prompt to repair (ADR-0011 already deferred this — re-sending a prompt
  starts a _new_ conversation, which is a different feature).
- Any new coder, layout, or flag.
- Changing `dg wt move`'s retarget behavior beyond the send-keys guard applying
  to it.

**Scope is locked.**

---

## 5. Implementation Plan

Bottom-up, so every step compiles and its tests pass before the next begins.

### File Changes

| Action | File                                     | Description                                                        |
| ------ | ---------------------------------------- | ------------------------------------------------------------------ |
| Modify | `internal/apps/tmux/tmux.go`             | command param on the 4 pane-creating calls; send-keys length guard |
| Modify | `internal/apps/tmux/tmux_test.go`        | tests for both                                                     |
| Modify | `internal/commands/shell_lookup.go`      | resolved-path variant; path-shaped validation                      |
| Modify | `internal/commands/shell_lookup_test.go` | classification tests incl. alias/function/builtin text             |
| Modify | `internal/tooling/worktree/layout.go`    | shell resolution, recipe builders, `Pane` resolution, quoting      |
| Modify | `internal/tooling/worktree/aicoder.go`   | launch recipe constant; `Command()` semantics                      |
| Modify | `internal/tooling/worktree/worktree.go`  | create paths exec; review split converts; `validateLayout` returns |
| Modify | `configs/templates/devgeta.zsh.tmpl`     | alias lines fed by the Go constant                                 |
| Modify | `internal/tooling/worktree/*_test.go`    | ordered tmux-call sequences updated                                |
| Modify | `docs/spec.md`                           | only if user-visible behavior changes                              |

### Step-by-Step

#### Step 1: tmux wrapper — carry a command on the pane-creating calls

- Add an optional pane command to `CreateWindow`, `CreateWindowInSession`,
  `CreateSessionWithWindow`, `SplitWindow`. An empty command must produce the
  **exact** argument list these produce today (a plain shell pane), so existing
  behavior is byte-identical when no command is given.
- Append the command as the final argument, after the existing flags.
- Verify: `go build ./...`

#### Step 2: tmux wrapper — send-keys refuses to truncate

- In `SendKeysToWindow`, `SendKeysToWindowInSession`, `SendKeysToPane`,
  `SendKeys`: reject `len(keys) > 1023` with an error naming the limit and the
  actual length. Cite ADR-0020 in the comment, with the reason (the pty input
  queue discards the excess **and the Enter** while tmux exits 0).
- Verify: `go test ./internal/apps/tmux/` — add a case at 1023 (accepted) and
  1024 (rejected).

#### Step 3: `shell_lookup` returns the resolved path

- Add a variant returning `(path string, result ShellLookupResult)`. Keep the
  existing three-valued function working for callers that only need the verdict.
- The marker line stays the proof the lookup ran (ADR-0016). Read the path from
  stdout **before** the marker.
- Accept the path **only if it begins with `/`**. Alias text
  (`alias cc='…'`), a bare function name, a bare builtin name, and empty output
  are all "no path".
- Verify: `go test ./internal/commands/` — table-test `classifyShellLookup`'s new
  path extraction against real observed shapes: an absolute path, `alias cc='…'`,
  a bare name, empty, and a mangled/truncated marker.

#### Step 4: Shell resolution + the two launch recipes

- In `layout.go` (next to `shellSingleQuote`), add:
  - `resolveShell()` — first usable of `$SHELL`, tmux `default-shell`, `/bin/sh`;
    usable means absolute **and** stats as an existing executable regular file.
    `/bin/sh` is the floor, so this cannot fail.
  - A recipe builder producing `<shell> -ic '<script>; exec <shell> -i'` for the
    interactive form and the direct form for a resolved path.
- Apply ADR-0020's quoting table exactly: resolved path quoted, prompt quoted,
  shell quoted at **both** interpolation sites, the assembled inner script quoted
  as a whole, `--pane` value unquoted _within_ its script.
- The trailing `exec` must be inside the **same** shell invocation — a nested one
  loses a `cd` from a `--pane` value.
- Verify: `go test ./internal/tooling/worktree/` — unit-test the builders as pure
  string functions, including a `--pane` value containing a single quote and a
  shell path containing a space.

#### Step 5: Carry the probe result from check to launch

- Change `Pane.check` and `Layout.EnsureInstalled` so the probe's result reaches
  the pane's command construction. `validateLayout` becomes
  `validateLayout(Layout) (Layout, error)` and its 4 call sites take the returned
  layout.
- **One probe per pane per create.** No re-probing at build time.
- Store the resolution on the pane the same way `check`/`prompt` already live
  there (constructor-set, unexported), and make sure `clone` still gives each
  create its own copy.
- Verify: `go build ./... && go test ./internal/tooling/worktree/`

#### Step 6: The launch recipe becomes the source of the alias

- Move the recipe (`CLAUDE_CODE_NO_FLICKER=1 claude`, `opencode`) into a Go
  constant, and render `devgeta.zsh`'s alias lines from it.
- `ensureToolInstalled` now probes the **binary**, since that is what the pane
  execs. Update its doc comment — its stated invariant ("probe exactly what the
  pane will launch") is preserved, but the thing being probed changed.
- Add a test against the embedded configs FS asserting the rendered alias matches
  the constant, so the two cannot drift.
- Verify: `go test ./internal/tooling/worktree/ ./internal/apps/...`

#### Step 7: Create paths pass commands at creation

- `buildWindowPanes`: replace the `sendKeys` closure with a create-with-command
  seam. Preserve the strict pane ordering and the pane-0 reselect.
- `buildWindowFromLayout` and `createWindowWithLayout` pass their pane commands
  through the new wrapper parameters.
- `launchReviewInLiveWindow`: the **split** branch passes the review command to
  `SplitWindow`; the idle-shell-reuse branch keeps `SendKeysToPane`. Keep the
  existing rollback (kill only the pane this call added).
- `LaunchReviewInRepo`'s create branch builds its pane via a constructor instead
  of `Pane{Command: reviewCmd}`.
- Verify: `go test ./internal/tooling/worktree/` — the ordered
  `ExecCommandResult` sequences in `window_build_test.go` must be updated to the
  new call shape; each edit should be a deliberate "this call no longer happens /
  now carries a command", not a resequencing until it passes.

#### Step 8: Manual verification, then docs

- Run the manual checks in section 6 against a real tmux.
- Update `docs/spec.md` only if user-visible behavior changed.
- Verify: `go test ./... && make lint`

---

## 6. Verification Plan

### Automated

```bash
go build ./...
go test ./internal/apps/tmux/ ./internal/commands/ ./internal/tooling/worktree/
go test ./...
make lint
```

### Manual (needs a real tmux; this is the bug's home turf)

1. **The actual bug:** `dg wt create adr20-a --prompt "$(python3 -c "print('x '*900)")"`
   → the coder starts **with the full prompt**. This is >1800 bytes; before this
   cycle it was cut at 1024 and never ran.
2. **Short prompt still fine:** `dg wt create adr20-b --prompt 'hello'`.
3. **Pane survives the command exiting:** quit the coder → you land at a shell in
   that pane, and the window is still there.
4. **`--pane` keeps its directory:** `dg wt create adr20-c --pane 'cd docs && pwd'`
   → the extra pane ends up in `docs`, not the worktree root. (Check the pane
   process's own cwd — `#{pane_current_path}` was measured unreliable for this.)
5. **`--pane` with a quote:** `--pane $'printf %s "it\'s fine"'` → runs, prints
   `it's fine`, does not break the wrapper.
6. **New repo session path:** create the first worktree for a repo that has no
   tmux session yet (this is `new-session`, the path an earlier ADR draft missed).
7. **Review launch:** `R` from `dg ws` on a window whose coder is busy → new pane
   splits and the reviewer runs in it; on a shell-only window → reuses the idle
   pane.
8. **Layout with no AI pane still refuses a prompt:** `--layout nvim --prompt x`
   → clear error, not a silent drop.

### Regression Check

- `dg wt create` with no flags, every built-in layout
- `dg ws` repair on an existing window
- `dg wt move` — panes still get `cd`'d (this is `retargetWindowAfterMove`, a
  send-keys path the guard now covers)
- `dg wt create` when the coder is genuinely not installed → still one actionable
  error before any git/tmux state is touched

---

## 7. Risks & Trade-offs

| Risk                                                                                         | Likelihood | Mitigation                                                                                                                                              |
| -------------------------------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A resolved launch recipe leaks between worktrees (`dg ws` resolves once, creates repeatedly) | **Med**    | This is exactly what `Layout.clone` exists for. Add a test that creates twice from one resolved layout with different prompts and asserts independence. |
| `Pane.check` signature change ripples through every constructor and its tests                | High       | Expected and desirable — the compiler enumerates the call sites. Do step 5 in one pass; do not add a compatibility shim.                                |
| Ordered `ExecCommandResult` sequences get "fixed" until green rather than reasoned about     | **Med**    | Each sequence edit must correspond to a named change in tmux calls. If a test passes for a reason you can't state, it is not passing.                   |
| Probing the binary instead of the alias changes preflight semantics                          | Low        | Accepted and recorded in ADR-0020: a coder installed outside devgeta now passes preflight and launches fine. Note it in the step-6 commit.              |
| Falling back to `/bin/sh` yields a pane with no `cc`/`oc`                                    | Low        | Deliberate (ADR-0020): a badly broken environment fails visibly in the pane rather than blocking a create.                                              |
| Manual verification skipped because unit tests are green                                     | **Med**    | The original bug is invisible to mocked tests — it lives in a real pty. Section 6's manual list is not optional.                                        |

### Trade-offs Made

- **Two shell invocation modes** (non-interactive for devgeta-owned panes,
  interactive for `--pane` and the no-path fallback) instead of one uniform rule.
  Costs a distinction a reader must learn; buys determinism and speed where
  devgeta owns the command, and faithful semantics where the user owns it.
- **The absolute path is an optimization, not a requirement.** The truncation fix
  holds in both branches; treating the path as required is what produced a broken
  fallback in an ADR draft.
- **`send-keys` is guarded rather than eliminated.** Three call sites legitimately
  target already-live panes.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope actually locked?
- [ ] Steps actionable?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**

Two things reviewers of ADR-0020 got wrong repeatedly and are worth checking here
too: **which call sites create a pane versus write to a live one** (answer it from
`grep`, not memory — ADR-0020 part 4 has the table), and **every value
interpolated into a pane command** (ADR-0020 rule 3 has the closed list; three
separate findings landed there before it was made exhaustive).

---

## Notes for Implementers

- **Read ADR-0020 first.** Its rules are measured, and several exist because an
  earlier draft asserted the opposite.
- **Commit after each step** once its verify check passes.
- **Manual verification is where this bug lives.** Mocked tests cannot see a pty.
- **Do not chunk a send-keys payload** to sneak under the cap — explicitly ruled
  out by the ADR and by the request that started this work.
