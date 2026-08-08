# Cycle: Pane commands are exec'd at creation, not typed

**Date:** 2026-08-07
**Estimated Duration:** ~6-8 hours
**Status:** Done

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

[ADR-0021](../../decisions/ADR-0021-pane-commands-are-exec-d-not-typed.md)
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
- `configs/templates/devgeta.zsh.tmpl` — `{{.OpenCodeAlias}}`, `{{.ClaudeAlias}}`
  (lines ~191, ~194), rendered by `internal/config.NewShellTemplateData` from
  `pkg/constants.CoderLaunch.AliasLine()` (`OpenCodeLaunch`/`ClaudeLaunch`) —
  not the literal `alias oc=opencode` / `alias cc="CLAUDE_CODE_NO_FLICKER=1 claude"`
  this section originally described.

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

- [x] `CreateWindow`, `CreateWindowInSession`, `CreateSessionWithWindow`,
      `SplitWindow` accept an optional pane command
- [x] `SendKeysTo*` reject a payload over 1023 bytes with an actionable error
- [x] `ShellCommandLookupFn` can return the resolved absolute path
- [x] Shell resolution (`$SHELL` → tmux `default-shell` → `/bin/sh`, each
      validated as an existing executable regular file) and the two launch
      recipes (resolved-path non-interactive; alias-based interactive fallback)
- [x] `Pane.check` / `Layout.EnsureInstalled` carry the probe's result to the
      launch instead of returning only `error`
- [x] The three create paths in `worktree.go` pass commands at creation
- [x] `launchReviewInLiveWindow`'s **split** branch execs; its idle-shell-reuse
      branch keeps `send-keys`
- [x] `LaunchReviewInRepo`'s create path builds its pane through a constructor,
      not a bare `Pane{}` literal
- [x] `devgeta.zsh` alias lines generated from the same Go constant as the launch
      recipe, pinned by a test against the embedded configs FS
- [x] Tests for all of the above; `docs/spec.md` updated if user-visible behavior
      changes

### Explicitly Out of Scope

- Making **repair** able to carry a long command. It sends into a live pane by
  nature; ADR-0021 bounds it with the 1023-byte guard and says a repair that ever
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

| Action | File                                     | Description                                                         |
| ------ | ---------------------------------------- | ------------------------------------------------------------------- |
| Modify | `internal/apps/tmux/tmux.go`             | command param on the 4 pane-creating calls; send-keys length guard  |
| Modify | `internal/apps/tmux/tmux_test.go`        | tests for both                                                      |
| Modify | `internal/commands/shell_lookup.go`      | resolved-path variant; path-shaped validation                       |
| Modify | `internal/commands/shell_lookup_test.go` | classification tests incl. alias/function/builtin text              |
| Modify | `internal/tooling/worktree/layout.go`    | shell resolution, recipe builders, `Pane` resolution, quoting       |
| Modify | `internal/tooling/worktree/aicoder.go`   | `Command()` semantics; reads the recipe constants                   |
| Modify | `pkg/constants/`                         | launch recipe constants (the one package both sides already import) |
| Modify | `internal/config/fromFile.go:590`        | `RegenerateShellConfig` passes the recipes into the template data   |
| Modify | `internal/tooling/worktree/worktree.go`  | create paths exec; review split converts; `validateLayout` returns  |
| Modify | `configs/templates/devgeta.zsh.tmpl`     | alias literals replaced by template values                          |
| Modify | `internal/tooling/worktree/*_test.go`    | ordered tmux-call sequences updated                                 |
| Modify | `docs/spec.md`                           | only if user-visible behavior changes                               |

### Step-by-Step

#### Step 1: tmux wrapper — carry a command on the pane-creating calls

- Add an optional pane command to `CreateWindow`, `CreateWindowInSession`,
  `CreateSessionWithWindow`, `SplitWindow`. An empty command must produce the
  **exact** argument list these produce today (a plain shell pane), so existing
  behavior is byte-identical when no command is given.
- Append the command as the final argument, after the existing flags.
- Add `DefaultShell() (string, bool)` — `show-options -gv default-shell` — for
  step 4's shell resolution. The wrapper has **no** option-reading API today, so
  this is new surface. Model it on `CurrentSession`: return `("", false)` on a
  failed or empty query rather than an error, because every caller treats "no
  answer" as "skip this candidate", not as a failure.
- Verify: `go build ./...` and `go test ./internal/apps/tmux/` — assert the
  empty-command argument list is byte-identical to today's.

#### Step 2: tmux wrapper — send-keys refuses to truncate

- In `SendKeysToWindow`, `SendKeysToWindowInSession`, `SendKeysToPane`,
  `SendKeys`: reject `len(keys) > 1023` with an error naming the limit and the
  actual length. Cite ADR-0021 in the comment, with the reason (the pty input
  queue discards the excess **and the Enter** while tmux exits 0).
- Verify: `go test ./internal/apps/tmux/` — add a case at 1023 (accepted) and
  1024 (rejected).

#### Step 3: `shell_lookup` returns the resolved path

- **The probe script has to change first — today it throws the path away.** The
  script is `command -v -- "$1" >/dev/null 2>&1; printf '\n<marker>%d\n' "$?"`
  (`shell_lookup.go:97`): stdout is redirected to `/dev/null`, so there is no
  path to extract and no amount of parsing recovers one. Drop the `>/dev/null`
  and keep stderr suppressed (`2>/dev/null`), so stdout carries the resolved
  path and then the marker line.
- The marker line stays the proof the lookup ran (ADR-0016) — unchanged.
- Read the path as the **last non-empty line before the marker**, not simply
  "the output before it": the existing comment on that script documents rc-file
  banner noise landing on stdout, and that noise now shares the stream with the
  path.
- Add a variant returning `(path string, result ShellLookupResult)`. Keep the
  existing three-valued function working for callers that only need the verdict.
- Accept the path **only if it begins with `/`**. Alias text
  (`alias cc='…'`), a bare function name, a bare builtin name, and empty output
  are all "no path".
- Verify: `go test ./internal/commands/`
  - table-test the pure classifier against real observed shapes: an absolute
    path, `alias cc='…'`, a bare name, empty, a mangled/truncated marker, and
    **rc-file noise preceding a valid path**.
  - **plus a test that exercises the probe script itself**, not only fixture
    strings — fixtures cannot catch a script that redirects its own output away,
    which is exactly the defect this step was written with. Run the real probe
    against a command known to exist and assert an absolute path comes back.

#### Step 4: Shell resolution + the two launch recipes

- **Shell resolution takes its candidates as input; it does not reach for tmux.**
  `layout.go` imports only `fmt`, `strings` and `internal/config` — it has no
  tmux dependency, and giving it one to read an option would invert the layering.
  So:
  - Step 1 adds `Tmux.DefaultShell() (string, bool)` to the wrapper
    (`show-options -gv default-shell`), alongside the other option-free
    accessors. There is **no** option-reading API on the wrapper today; this is
    new surface, not a call that already exists.
  - `resolveShell(candidates ...string) string` is a pure function next to
    `shellSingleQuote`: first usable of what it is handed, then `/bin/sh`.
    Usable = absolute **and** stats as an existing executable regular file.
  - The caller (which already holds `w.Tmux`) supplies
    `$SHELL, <tmux default-shell if the query succeeded>`. **A failed or empty
    tmux query simply drops that candidate** — it is never an error, never
    logged as one, and never blocks a create. `/bin/sh` is the floor, so
    resolution cannot fail.
- **A devgeta-owned launch is structured, not a "path plus prompt" pair.** The
  create paths do not all have the same argument shape, so a two-slot template
  cannot express them. The forms that must survive:

  | Pane                  | Program    | Arguments                                    |
  | --------------------- | ---------- | -------------------------------------------- |
  | nvim                  | `nvim`     | none                                         |
  | claude, no prompt     | `claude`   | none (env `CLAUDE_CODE_NO_FLICKER=1`)        |
  | claude, with prompt   | `claude`   | `<prompt>` (positional)                      |
  | opencode, no prompt   | `opencode` | none                                         |
  | opencode, with prompt | `opencode` | `--prompt`, `<prompt>`                       |
  | reviewer              | `opencode` | `--agent`, `<agent>`, `--prompt`, `<prompt>` |
  | shell pane            | —          | no command at all                            |

  So introduce a small devgeta-owned launch value — **program, argument list, and
  optional env prefix** — and build the command string by quoting each element
  independently and joining. The env prefix stays a devgeta-owned constant and is
  never interpolated from user data.

  This replaces string concatenation as the representation: `AICoder.Command()` /
  `PromptCommand()` and `ReviewCommand()` currently each hand-assemble a string
  (`aicoder.go:112`, `:114`, `:144`), which is exactly why a prompt and an
  `--agent` flag cannot share one template today.

  **`--pane` values stay unparsed.** They are command lines, not argument lists —
  devgeta never splits them, and the structured form does not apply to them.
  This is the same boundary ADR-0011 drew for quoting, in a second place.

- Both recipes end with the **same-shell trailing `exec`**, not just the
  interactive one. ADR-0021 part 2 applies to every created pane, so a coder that
  quits leaves a shell behind whether or not a path resolved. Only the
  interactive fallback adds `-ic`:

  | Case                | Recipe                                        |
  | ------------------- | --------------------------------------------- |
  | Resolved path       | `<rendered launch>; exec '<shell>'`           |
  | Fallback / `--pane` | `'<shell>' -ic '<script>; exec '<shell>' -i'` |

  where `<rendered launch>` is the structured value above, each element quoted.

- Apply ADR-0021's quoting table exactly: resolved path quoted, prompt quoted,
  shell quoted at **both** interpolation sites, the assembled inner script quoted
  as a whole, `--pane` value unquoted _within_ its script.
- The trailing `exec` must be inside the **same** shell invocation — a nested one
  loses a `cd` from a `--pane` value.
- Verify: `go test ./internal/tooling/worktree/` — unit-test the builders as pure
  string functions:
  - **One case per row of the launch table above** (nvim, claude ±prompt,
    opencode ±prompt, reviewer, shell pane). This is the regression net for
    n18: a builder that silently drops `--agent` or turns nvim into
    `nvim '<prompt>'` must fail here.
  - A `--pane` value containing a single quote, and a shell path containing a
    space.
  - **Both recipes end in the trailing exec.**
  - `resolveShell` handed candidates directly (a non-existent absolute path, a
    directory, a non-executable file, an empty string), asserting the `/bin/sh`
    floor — no tmux, no env mutation needed.

#### Step 5: Carry the probe result from check to launch

- Change `Pane.check` and `Layout.EnsureInstalled` so the probe's result reaches
  the pane's command construction. `validateLayout` becomes
  `validateLayout(Layout) (Layout, error)` and its 4 call sites take the returned
  layout.
- **One probe per pane per create.** No re-probing at build time.
- Store the resolution on the pane the same way `check`/`prompt` already live
  there (constructor-set, unexported), and make sure `clone` still gives each
  create its own copy.
- **`validateLayout` is not the only probe site — the review path has its own.**
  `LaunchReviewInRepo` calls `(&OpenCodeCoder{}).EnsureInstalled()` directly
  (`worktree.go:1967`) before building its ad-hoc pane, and that call's result is
  currently discarded. Routing resolution only through `validateLayout` would
  leave the reviewer launch to re-probe or silently fall back — the exact
  "one probe, one recipe" invariant ADR-0021 requires, broken in the one path
  that does its own checking.

  Give the reviewer a **single resolution-carrying constructor** used by both of
  its launches — the create branch (no live window) and the split branch (live
  window) — so the one probe feeds both. This composes with step 7's requirement
  that `LaunchReviewInRepo` stop building a bare `Pane{}` literal: the
  constructor is where the resolution lands.

- Verify: `go build ./... && go test ./internal/tooling/worktree/` — include a
  test that a reviewer launch probes **once**, by counting probe invocations
  through the existing `ShellCommandLookupFn` seam (`setShellCommandExistsFn`).

#### Step 6: The launch recipe becomes the source of the alias

**The constant cannot live in `internal/tooling/worktree`.** The only renderer of
`devgeta.zsh` is `GlobalConfig.RegenerateShellConfig`
(`internal/config/fromFile.go:590`), and `internal/config` **cannot** import
`internal/tooling/worktree` — worktree already imports config in four files, so
that edge would be a cycle. The repo has already solved this exact problem once:
`WorktreeLocationShared` / `WorktreeLocationInRepo` live in
`internal/config/fromFile.go` with a comment explaining that config is the owner
which adds no new edge, and worktree reads them back through the existing one.

- Put the recipe constants in **`pkg/constants`**. Verified that both
  `internal/config` (`fromFile.go`, `reconcile.go`) and
  `internal/tooling/worktree` (`repo_candidates.go`) already import it, so
  neither side gains an edge and neither has to own the other's value. (If that
  turns out not to fit, the fallback is `internal/config`, following the
  `WorktreeLocation*` precedent above — **not** worktree.)
- **`RegenerateShellConfig` must actually pass them to the template.** Today it
  hands the template only `ShellFeatures`; the alias lines are literals in
  `devgeta.zsh.tmpl`. Extend the template data with the recipe values and replace
  those literals. Without this the constant exists but changes nothing — the plan
  previously said "render from it" without naming the renderer or its data, which
  is not an implementable instruction.
- `ensureToolInstalled` now probes the **binary**, since that is what the pane
  execs. Update its doc comment — its stated invariant ("probe exactly what the
  pane will launch") is preserved, but the thing being probed changed.
- Add a test against the embedded configs FS asserting the rendered alias matches
  the constant, so the two cannot drift.
- Verify: `go test ./internal/config/ ./internal/tooling/worktree/ ./internal/apps/...`
  and `go build ./...` (a cycle shows up here immediately if the constant landed
  in the wrong package).

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

**Run and passed (2026-08-07).** The maintainer ran this section's checks against
a real tmux and reported them working. Per-check results were not recorded
individually, so treat the list below as "the maintainer confirmed this set",
not as eight separately attested lines.

Independently, the mechanism itself was verified on the same machine (macOS 25.6,
tmux 3.5a, `/bin/zsh`) using a throwaway tmux server on a private socket — no
devgeta install, no `dg configure`, no worktrees:

| Claim                                                                | Result                                         |
| -------------------------------------------------------------------- | ---------------------------------------------- |
| 1800-byte command as a tmux shell-command at pane creation           | arrived **intact**                             |
| trailing `exec <shell>` keeps the pane alive after the command exits | **holds**                                      |
| same-shell `exec` preserves a `cd`                                   | **preserved**                                  |
| nested form loses the `cd` (the construction part 2 rejects)         | **lost it**, matching part 2's measurement     |
| the original bug, same payload via `send-keys`                       | **reproduced — never executed, tmux exited 0** |

That last row is the one worth keeping: the failure this cycle removes was
confirmed live on the hardware, not merely cited from the ADR.

**Two things remain unattested by the independent check** — the review-launch
branches (item 7) and `dg wt move`'s retarget (Regression Check) were exercised
only by the maintainer's run, so if that run skipped either, they are unverified.

A measurement that came out of this verification is recorded in
[ADR-0021](../../decisions/ADR-0021-pane-commands-are-exec-d-not-typed.md)'s
Positive and Negative sections: a `<shell> -ic` pane spends ~23s in `.zshrc`
before its command runs on this machine, which is what `--pane` and the no-path
fallback pay. Measure that path by waiting for the command, never with a fixed
sleep — a 3-second sleep gave a false "the `cd` was lost" reading here.

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
| Probing the binary instead of the alias changes preflight semantics                          | Low        | Accepted and recorded in ADR-0021: a coder installed outside devgeta now passes preflight and launches fine. Note it in the step-6 commit.              |
| Falling back to `/bin/sh` yields a pane with no `cc`/`oc`                                    | Low        | Deliberate (ADR-0021): a badly broken environment fails visibly in the pane rather than blocking a create.                                              |
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

- [x] Domain context clear?
- [x] Engineer context sufficient?
- [x] Objective unambiguous?
- [x] Scope actually locked?
- [x] Steps actionable?
- [x] Verification executable?
- [x] Risks realistic?

**Reviewer notes:**

Two things reviewers of ADR-0021 got wrong repeatedly and are worth checking here
too: **which call sites create a pane versus write to a live one** (answer it from
`grep`, not memory — ADR-0021 part 4 has the table), and **every value
interpolated into a pane command** (ADR-0021 rule 3 has the closed list; three
separate findings landed there before it was made exhaustive).

---

## Notes for Implementers

- **Read ADR-0021 first.** Its rules are measured, and several exist because an
  earlier draft asserted the opposite.
- **Commit after each step** once its verify check passes.
- **Manual verification is where this bug lives.** Mocked tests cannot see a pty.
- **Do not chunk a send-keys payload** to sneak under the cap — explicitly ruled
  out by the ADR and by the request that started this work.

---

## Deferred Follow-ups

Raised by this cycle's reviews, triaged as non-blocking, and deliberately not done.
None affects behavior today. Each carries the coupling that makes it safe to pick up —
those warnings are the reason this list is written down rather than rediscovered.

1. **A `Layout` accessor for pane 0's created command.** `buildWindowFromLayout`,
   `createWindowWithLayout`, and `ensureWindow` each index `layout.Panes[0]` unguarded.
   The count went from one site to three during this cycle, all relying on
   `validateLayout`'s caller-side emptiness check, so the invariant is now spread rather
   than held in one place. No reachable panic today: all four `validateLayout` call sites
   gate on `len(Panes) > 0`, and `LaunchReviewInRepo` builds a one-pane layout.
   **Coupling:** the accessor must return pane 0's _created_ command and must **not**
   absorb `ensureWindow`'s `Panes[0].Command == ""` test — that one reads the **typed**
   form, which is a different string (see ADR-0021's 2026-08-07 amendment).

2. **Split `window_build_test.go`.** It is ~66 KB after this cycle. Its helpers are
   shared rather than copy-pasted, so it is coherent as it stands; the next addition of
   comparable size should move the review-launch tests into their own file.

3. **Make `paneShell`'s tmux query conditional.** It issues `show-options -gv
default-shell` unconditionally, so a shell-layout create pays a tmux round trip plus a
   stat whose result is interpolated nowhere, and the query is wasted whenever `$SHELL`
   is already usable (`resolveShell` short-circuits on the first usable candidate).
   Deliberate today: querying unconditionally keeps the ordered `ExecCommandResult`
   sequences in `window_build_test.go` independent of the machine's `$SHELL`.
   **Coupling:** `TestPaneShellCandidateLadder` asserts the query **is** issued, so
   making it conditional must update that test — and that test is the only thing pinning
   which candidates `paneShell` supplies, so weakening it silently un-pins the shell
   ladder that every created pane's command interpolates.
