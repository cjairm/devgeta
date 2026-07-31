# Cycle: An opening prompt and extra panes for `dg wt create`

**Date:** 2026-07-31
**Estimated Duration:** ~5 hours
**Status:** Done — implemented and verified 2026-07-31 (all four open questions resolved, see §8)

---

## 1. Domain Context

`dg wt create <name>` builds a git worktree plus a tmux window, and fills that
window from a **Layout** — a named list of panes, each with a shell command and
a split direction ([layout.go](../../../internal/tooling/worktree/layout.go)).
Four built-in layouts exist: `opencode`, `claude`, `claude-nvim`, `nvim`.

Two things are missing when you want to open a worktree _already working on
something_:

1. **No way to give the AI coder an opening prompt.** The pane launches `cc` /
   `oc` and sits at an empty session waiting for you to type. Anything that
   wants to hand the new session a task (a triage skill, a wrapper script, you
   with a ticket number in hand) has to `tmux send-keys` the prompt in
   afterwards — which means polling `capture-pane` until the coder's TUI has
   booted, because keys sent before it is ready are dropped.
2. **No way to add a plain shell pane.** A worktree usually needs a bootstrap
   command run next to the coder (`make finit`, `npm install`, a dev server).
   `claude-nvim` proves the window shape is already buildable; there is just no
   way to ask for it with a command devgeta doesn't already know about.

Both are per-invocation, task-specific inputs, so both are flags — not
settings. Prior art for the mechanism is already in this package:
`ReviewCommand` ([layout.go:247](../../../internal/tooling/worktree/layout.go))
builds `oc --agent code-reviewer --prompt '…'` and hands it to a pane, quoted
via `shellSingleQuote`. This cycle generalizes that one hardcoded case.

Related: [ADR-0010](../../decisions/ADR-0010-worktree-layout-is-a-setting-git-is-the-index.md)
(layout is a setting), [docs/spec.md](../../spec.md) §`dg worktree create`.

---

## 2. Engineer Context

**Relevant files:**

| File                                        | Purpose                                                                                                                               |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/tooling/worktree/aicoder.go`      | `AICoder` interface + `OpenCodeCoder`/`ClaudeCoder`; each `Command()` returns the devgeta shell alias (`oc`/`cc`), not the raw binary |
| `internal/tooling/worktree/layout.go`       | `Pane`, `Layout`, `newLayout` (removed by Step 3), `builtinLayouts`, `ResolveLayout`, `shellSingleQuote`                              |
| `internal/tooling/worktree/worktree.go:294` | `validateLayout` — rejects an empty layout, runs every pane's install checker before any git/tmux state is touched                    |
| `internal/tooling/worktree/worktree.go:471` | `buildWindowPanes` — loops `layout.Panes` in order, splitting from the active pane and sending each command, then reselects pane 0    |
| `internal/apps/tmux/tmux.go:494`            | `SplitWindow` — `"vertical"` = side by side (tmux `-h`), `"horizontal"` = stacked (tmux `-v`)                                         |
| `cmd/worktree.go:43`                        | `worktreeCreateCmd` — flags and `RunE`                                                                                                |
| `cmd/worktree.go:398`                       | `resolveWorktreeLayout` — the shared load+resolve sequence create and repair both use                                                 |

**Key structural facts this plan depends on:**

- `Layout` today carries an unexported `paneCheckers []func() error` slice that
  mirrors `Panes` 1:1, and `newLayout` panics if the two lengths disagree
  (registry-bug guard, unreachable from user input). This cycle **removes** that
  parallel slice rather than adding a second one beside it — see §8's resolved
  question 3 and Step 3.
- `EnsureInstalled` **already skips a nil checker**
  ([layout.go:55](../../../internal/tooling/worktree/layout.go)) — so a pane with
  no install check needs no new code path.
- Every caller — CLI create, CLI repair, all four TUI sites — obtains a `Layout`
  from `ResolveLayout` and passes it down unchanged. If the two new flags are
  expressed as **transformations on a resolved `Layout`**, applied only in
  create's `RunE`, then `Create`/`CreateAt`/`create`/`buildWindowFromLayout`/
  `buildWindowPanes` signatures and every TUI call site stay untouched.

**Verified launch forms** (checked against the installed binaries, 2026-07-31):

- `claude [options] [command] [prompt]` — positional prompt, "starts an
  interactive session by default". So: `cc 'prompt'`.
- `opencode` TUI accepts `--prompt <string>` ("prompt to use"). So:
  `oc --prompt 'prompt'`. This is the form `ReviewCommand` already ships.

The two forms differ, which is why the prompt-command shape belongs on the
`AICoder` interface and not in a `fmt.Sprintf` at the call site.

**Testing:** `testutil.MockApp` for anything touching tmux/git; never a real
command. See [testing-patterns.md](../../guides/testing-patterns.md).

```bash
go test ./internal/tooling/worktree/
go test ./cmd/
go test ./...
make lint
```

---

## 3. Objective

`dg wt create <name> --prompt '<text>' --pane '<command>'` creates the worktree,
launches the resolved layout's AI coder **with `<text>` as its opening prompt**,
and adds a shell pane running `<command>` beside it — with the prompt delivered
as a launch argument, so there is no wait-for-TUI-boot race to lose.

---

## 4. Scope Boundary

### In Scope

- [x] ADR-0011 recording the delivery choice: prompt as a launch argument, not
      keystrokes sent after the coder's TUI boots
- [x] `AICoder.PromptCommand(prompt string) string` + both implementations
- [x] `Layout.WithPrompt(prompt) (Layout, error)` — retargets the coder pane's
      command; errors when the layout has no coder pane
- [x] `Layout.WithExtraPanes(commands []string) (Layout, error)` — appends shell
      panes; rejects an empty/whitespace-only command
- [x] `--prompt` and repeatable `--pane` on `dg wt create`
- [x] `applyCreateLayoutOptions` — a pure helper in `cmd` that both `RunE` and
      the command tests call, so apply-order and the fail-before-side-effects
      guarantee are testable without reaching `worktree.New()`
- [x] `resetWorktreeFlags` extended to cover the two new flag vars, using
      `SliceValue.Replace` for the repeatable one (see Step 6 — `Flags().Set`
      is wrong here)
- [x] Tests for all of the above, including the no-coder-pane error and the
      quoting path
- [x] Docs: create's `Long` help, `docs/spec.md`, `README.md`, and CLAUDE.md's
      "Recent Changes" — the AI-facing docs a future session reads first
      (CLAUDE.md §2)

### Explicitly Out of Scope

- **`dg wt repair --prompt`.** Repair relaunches a coder in a window whose panes
  died. Re-sending an opening prompt there would start a _new_ conversation, not
  restore the old one — a different feature with a different meaning. See §7.
- **A `worktree.default_prompt` setting.** A prompt is task-specific by nature;
  a persistent default one is meaningless. `--pane` likewise stays a flag —
  a persistent extra pane is what a user-definable layout would be for, and
  that is not this cycle.
- **User-definable layouts in `global_config.yaml`.** Deliberately not built:
  these two flags cover the known need without a config-schema change, a
  layout-name registry rework, or a validation story for arbitrary user panes.
  Revisit only when a second use case actually asks (CLAUDE.md §6 — no
  speculative code).
- **A split direction for `--pane`.** Every appended pane splits `"vertical"`
  (side by side), matching `claude-nvim`. Adding direction syntax before anyone
  has wanted it is speculative.
- **TUI surface for either flag.** No `dg ws` keybinding, no create-flow field.
- **A dependency-injection seam for `worktreeCreateCmd.RunE`.** Deliberately not
  added — see §8's resolved review notes for the reasoning and what replaces it.
- **A way to ask for a bare idle shell pane.** `--pane ""` is rejected, not
  repurposed into one (Step 5). If that turns out to be wanted, it gets its own
  spelling and its own decision.

**Scope is locked.**

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                                    | Description                                                                               |
| ------ | ------------------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| Create | `docs/decisions/ADR-0011-agent-prompt-as-launch-argument.md` | Why argv, not post-boot send-keys                                                         |
| Modify | `internal/tooling/worktree/aicoder.go`                       | `PromptCommand` on `AICoder` + both coders                                                |
| Modify | `internal/tooling/worktree/aicoder_test.go`                  | Per-coder prompt-command form, quoting                                                    |
| Modify | `internal/tooling/worktree/layout.go`                        | `panePrompters` slice, `newLayout` arity guard, `WithPrompt`, `WithExtraPanes`            |
| Modify | `internal/tooling/worktree/layout_test.go`                   | Both transformations, error cases, immutability of the source layout                      |
| Modify | `cmd/worktree.go`                                            | `--prompt` / `--pane` flags, `applyCreateLayoutOptions`, call it in `RunE`, update `Long` |
| Modify | `cmd/worktree_test.go`                                       | Extend `resetWorktreeFlags`; flag registration, repeat-parse, helper behavior             |
| Modify | `docs/spec.md:273`                                           | Document both flags in create's flag list + an example                                    |
| Modify | `README.md:188`                                              | Add both flags to the create bullet list                                                  |
| Modify | `CLAUDE.md`                                                  | "Recent Changes" entry + date bump (its own currency rule)                                |

### Step-by-Step

#### Step 0: Write ADR-0011

- Decision: an opening prompt is passed to the coder **as a launch argument**
  (`cc '<text>'`, `oc --prompt '<text>'`), never typed into a running TUI.
- Alternative rejected: `send-keys` after launch, gated on polling
  `capture-pane` for a ready prompt — best-effort by construction (a slow boot
  silently drops the prompt), and it makes devgeta depend on the coder's TUI
  rendering rather than on its documented CLI.
- Consequence: the set of supported coders is bounded by "accepts a prompt on
  the command line". Both current coders do; a future coder that doesn't must
  fail loudly rather than fall back to keystrokes.
- Verify: file exists, listed in `docs/decisions/README.md`.

#### Step 1: `AICoder.PromptCommand`

- Add to the interface in `aicoder.go`; implement:
  - `ClaudeCoder`: `cc 'text'` — positional.
  - `OpenCodeCoder`: `oc --prompt 'text'`.
- Both use `shellSingleQuote` (already in `layout.go`, same package) because the
  string is typed literally into an interactive shell by `send-keys`.
- Comment WHY the forms differ (per-coder CLI), citing the verified `--help`
  output, so nobody "unifies" them later.
- Verify: `go build ./... && go test ./internal/tooling/worktree/`

#### Step 2: Refactor `ReviewCommand` onto `PromptCommand`

- `ReviewCommand` currently hand-builds `oc --agent X --prompt '<quoted>'`. Once
  `PromptCommand` exists, that `--prompt '…'` fragment has two authors —
  CLAUDE.md §6 requires the extraction happen in the PR that introduces the
  second use. Rebuild it as `PromptCommand(reviewPrompt)` plus the `--agent`
  fragment (or add an agent-aware seam if that reads cleaner).
- Existing `ReviewCommand` tests must pass **unchanged** — the emitted string is
  identical; only its author moves.
- Verify: `go test ./internal/tooling/worktree/ -run Review`

#### Step 3: Fold the per-pane functions into `Pane`

Resolves §8 question 3 — no third parallel slice.

- `Pane` grows two unexported fields: `check func() error` and
  `prompt func(prompt string) string`. `Layout.paneCheckers` is **deleted**; the
  exported shape stays `{Command, Split}` on `Pane` and `{Name, Panes}` on
  `Layout`, as the existing comment requires.
- `newLayout` and its arity panic are **deleted** with it. Their whole reason to
  exist was keeping two slices in step; with the functions living on the pane
  they describe, that class of drift is structurally impossible rather than
  merely detected (CLAUDE.md §4). `TestNewLayoutPanicsOnPaneCheckerMismatch` is
  deleted for the same reason — it tests a bug that can no longer be written.
- Add two pane constructors so a pane's three facets come from one source:
  - `coderPane(coder AICoder, split string) Pane` — `Command`, `check`, and
    `prompt` all read off the one `AICoder`, so they cannot disagree.
  - `nvimPane(split string) Pane` — `check` set, `prompt` left nil.
- `EnsureInstalled` loops `Panes` and skips a nil `check` (same behavior, one
  less indirection).
- **Consequence to expect:** `Pane` contains funcs, so it is no longer
  comparable with `==`. `layout_test.go:65`'s `layout.Panes[i] != wantPane` must
  compare `Command`/`Split` field-wise. No production code compares panes.
- Add `const splitVertical = "vertical"` — `claude-nvim` and `WithExtraPanes`
  are two users of that literal, so it gets one author (CLAUDE.md §6 DRY).
- Verify: `go test ./internal/tooling/worktree/`

#### Step 4: `Layout.WithPrompt`

- Returns a **copy** with the single prompt-taking pane's `Command` replaced by
  `prompter(prompt)`. Copy `Panes` (and both parallel slices) into fresh
  backing arrays — never alias the receiver's.
- No prompt-taking pane (e.g. layout `nvim`) → error naming the layout and
  saying which layouts accept a prompt. That list is **derived** from the
  registry (the built-ins that have a prompt-taking pane), never hardcoded, so
  it cannot drift. Fail loudly; do not silently drop the prompt, and do not
  append it to a non-coder command (`nvim 'explain issue 1082'` would open a
  file by that name).
- **More than one prompt-taking pane → error** (resolves §8 question 2). No such
  layout exists today; three lines now mean a future two-coder layout fails
  loudly instead of prompting whichever pane happens to come first.
- Empty `prompt` → return the layout unchanged, no error (lets the caller apply
  it unconditionally).
- Verify: `go test ./internal/tooling/worktree/ -run Prompt`

#### Step 5: `Layout.WithExtraPanes`

- Signature is `WithExtraPanes(commands []string) (Layout, error)`. Appends one
  `Pane{Command: c, Split: "vertical"}` per command, each with a `nil` checker
  and `nil` prompter. Returns a copy, same rules as Step 4.
- **An empty or whitespace-only command is an error**, naming the flag. Rationale:
  `--pane ""` almost always means an unset variable in a caller's script
  (`--pane "$BOOTSTRAP"`), and send-keys with an empty string would silently
  produce a bare idle shell pane that looks like the feature half-worked.
  Failing loudly is the CLAUDE.md §4 read. Validation lives here rather than in
  the `cmd` helper so every present and future caller inherits it.
- The command is **not** shell-quoted — unlike a prompt (which is one literal
  argument to a coder), a `--pane` value _is_ a shell command line, and quoting
  it would break the compound commands that make the flag worth having
  (`cd api && make dev`). Say so in the doc comment: the user is handing devgeta
  a command to run in their own shell, which is the same trust level as a shell
  alias. Note it in ADR-0011 too, since it is the asymmetry a reader will
  question.
- **No install check for these panes, on purpose** — and the comment must say
  why: the command can be a shell builtin, a compound (`cd x && make y`), or a
  Makefile target, so a first-token probe would reject legitimate commands. A
  built-in layout's pane is checked because devgeta chose that command; this one
  is the user's, and its own pane shows any error. `EnsureInstalled` already
  skips nil checkers, so no new code path.
- Note in the doc comment: with 2+ extra panes each splits the previous one, so
  they get progressively narrower — existing `buildWindowPanes` behavior, and
  the common case (coder + one shell) is a clean 50/50.
- Verify: `go test ./internal/tooling/worktree/ -run ExtraPanes`

#### Step 6: Wire the flags

- `createPromptFlag string`, `createPaneFlags []string` (`StringArrayVar`, so
  `--pane` repeats and commas inside a command are not split).
- **No shorthands for either** — `-p` is ambiguous between `--prompt` and
  `--pane`, and picking one would read as a typo for the other.
- Add a **pure helper** in `cmd/worktree.go`:

  ```go
  func applyCreateLayoutOptions(layout worktree.Layout, prompt string, panes []string) (worktree.Layout, error)
  ```

  It applies `WithPrompt` then `WithExtraPanes`, returning on the first error.
  `RunE` becomes `layout, err = applyCreateLayoutOptions(layout, createPromptFlag, createPaneFlags)` immediately
  after `resolveWorktreeLayout` and before `worktree.New()`. This is what makes
  the ordering and the fail-before-side-effects guarantee testable — see §8.

- Extend `resetWorktreeFlags` in `cmd/worktree_test.go` with `createPromptFlag`
  and `createPaneFlags`. **`Flags().Set("pane", "")` is the wrong reset for the
  repeatable flag**: pflag's `stringArrayValue.Set` _appends_ once `changed` is
  true (`pflag@v1.0.9/string_array.go:16`), so it would leave a stray `""`
  behind — exactly the leak the reset exists to prevent. Use pflag's
  `SliceValue` interface instead:

  ```go
  if sv, ok := worktreeCreateCmd.Flags().Lookup("pane").Value.(pflag.SliceValue); ok {
      _ = sv.Replace(nil)
  }
  worktreeCreateCmd.Flags().Lookup("pane").Changed = false
  ```

  Clearing `Changed` matters independently: pflag keys its append-vs-replace
  behavior off it, so a stale `true` makes the _next_ test's first `--pane`
  append instead of replace.

- Update create's `Long` help: both flags, that `--prompt` needs a layout with an
  AI pane, and that `--pane` repeats.
- Verify: `make build && ./devgeta wt create --help`

#### Step 7: Tests

- `layout_test.go`: prompt lands on the coder pane for each single-coder layout
  and for `claude-nvim`; `nvim` layout errors; empty prompt is a no-op; extra
  panes append in order with `"vertical"`; empty and whitespace-only `--pane`
  commands error; **the source layout is unmodified after each transformation**;
  quoting survives a prompt containing `'`.
- `aicoder_test.go`: exact string per coder.
- `cmd/worktree_test.go`:
  - Both flags are registered, with no shorthand, and `--pane` accumulates
    across repeats (pins the command shape, per CLAUDE.md §10).
  - **Two parses in sequence** with `resetWorktreeFlags` between them, asserting
    the second sees only its own `--pane` values — the regression test for the
    `Set`-appends trap above.
  - `applyCreateLayoutOptions` directly: order (a `WithPrompt` error wins over a
    later bad `--pane`), both flags composing, and that an `nvim`-layout prompt
    returns an error. Because the helper is pure and returns before `RunE` ever
    constructs a manager, this proves the no-side-effect guarantee without
    touching git or tmux.
- Every test touching tmux/git uses `testutil.MockApp` +
  `testutil.VerifyNoRealCommands(t, mockApp.Base)`.
- Verify: `go test ./... && make lint`

#### Step 8: Docs

- `docs/spec.md:273` flag list + a worked example, including the rejected-empty
  `--pane` rule and that `--pane` values are not quoted.
- `README.md:188` bullets.
- `CLAUDE.md` — add the flags to "Recent Changes" and bump **Last updated**. This
  is the file a future AI session reads first (CLAUDE.md §2), and its own
  DOCUMENTATION CURRENCY rule requires the update land in the same PR.
- Verify: read all three; confirm the `--prompt`-needs-an-AI-pane rule and the
  empty-`--pane` rule are both stated where a reader meets the flags.

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/tooling/worktree/
go test ./cmd/
go test ./... -cover
make lint
```

### Manual (the golden path this cycle exists for)

```bash
make build
./devgeta wt create prompt-smoke --ai claude \
  --prompt "say hello and stop" --pane "echo pane-ok"
```

1. Window `wt-devgeta-prompt-smoke` exists with two side-by-side panes.
2. Left pane: Claude Code booted **and already answering** — no typing needed.
3. Right pane: `echo pane-ok` ran in the worktree directory.
4. Cursor lands on the left (coder) pane.
5. `./devgeta wt create x --layout nvim --prompt hi` → clear error, **no
   worktree and no window created** (check `git worktree list`).
6. `./devgeta wt create q --ai opencode --prompt "it's quoted"` → the apostrophe
   survives; OpenCode receives the whole sentence.
7. Repeat 1–2 with `--ai opencode` (the `--prompt` flag form).
8. `./devgeta wt create y --ai claude --pane ""` → clear error naming `--pane`,
   nothing created.
9. `./devgeta wt create z --ai claude --pane "cd .. && pwd"` → the compound
   command runs as written (proves `--pane` values are not quoted).

### Regression

- `dg wt create` with no new flags behaves exactly as before (all four layouts).
- `dg wt repair`, `dg ws` create flow, `dg config get worktree.default_layout`
  unchanged — none of them see the new flags.
- `dg ws` R-keybinding review launch still works (Step 2 refactor).

---

### Results (2026-07-31)

Automated: `go test ./...` → 2533 pass, 80 packages. `make lint` clean.

Manual, run inside tmux against the real binary. Every numbered check above
passed:

| Check                                                 | Result                                                                                                                                                                                            |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1–4 golden path (`--prompt` + `--pane`, claude)       | Two panes at 112 cols each (clean 50/50), both in the worktree dir. Claude booted with the prompt **already answered** ("Hello!") — nothing typed. Shell pane printed `pane-ok`. Cursor on pane 1 |
| 5 `--layout nvim --prompt hi`                         | Clear error listing promptable layouts; `git worktree list` unchanged — nothing created                                                                                                           |
| 6–7 `--ai opencode --prompt "it's quoted"`            | OpenCode received `it's quoted` — apostrophe intact through the `--prompt` form                                                                                                                   |
| 8 `--pane ""`                                         | Clear error naming `--pane`; nothing created                                                                                                                                                      |
| 9 `--pane "cd .. && pwd"`                             | Ran as a compound command, printed the parent dir — proves the value is not quoted                                                                                                                |
| Regression: bare `create --layout claude-nvim`        | Pane 1 plain `cc` (no prompt appended), pane 2 `nvim` — unchanged shape                                                                                                                           |
| Regression: `ReviewCommand` after the Step 2 refactor | Its exact-string tests passed **unmodified**, which was the stated criterion                                                                                                                      |

All test worktrees, branches, and windows removed afterwards.

---

## 7. Risks & Trade-offs

| Risk                                                                      | Likelihood | Mitigation                                                                                                                                    |
| ------------------------------------------------------------------------- | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| A coder's CLI changes its prompt form (positional ↔ flag)                 | Low        | Form lives in exactly one method per coder; ADR-0011 records the constraint. A wrong form fails visibly in the pane, not silently             |
| Unquoted metacharacters in a prompt reach the shell                       | Low        | `shellSingleQuote` on every prompt, with a test using an embedded `'`                                                                         |
| `--pane` command fails at runtime with no pre-check                       | Med        | Accepted (see Step 5): the pane shows the error, the worktree is still good. A pre-check would produce false rejections for compound commands |
| Parallel-slice drift between `Panes` and its per-pane functions           | —          | Eliminated, not mitigated: Step 3 moves `check`/`prompt` onto `Pane`, so there are no parallel slices left to drift                           |
| Step 2 refactor changes the review command string                         | Low        | Existing `ReviewCommand` tests must pass unmodified                                                                                           |
| Repeatable `--pane` leaks state between tests via pflag's append-on-`Set` | Med        | Step 6's `SliceValue.Replace(nil)` + `Changed = false` reset, plus a two-parses-in-sequence regression test                                   |
| `applyCreateLayoutOptions` drifts from what `RunE` actually does          | Low        | `RunE` has no logic of its own between resolve and `worktree.New()` — it is one call to the helper, so there is nothing to drift from         |

### Trade-offs Made

- **Layout transformations over new parameters.** `WithPrompt`/`WithExtraPanes`
  return modified layouts instead of threading `prompt`/`panes` through
  `Create → CreateAt → create → buildWindowFromLayout → buildWindowPanes`. Five
  signatures and every TUI call site stay untouched, and `validateLayout` keeps
  seeing one complete layout.
- **Flags, not config.** Both inputs are per-invocation. Anything persistent is
  a layout-definition feature, which this cycle deliberately declines to build.
- **Error, not silent drop, on `--prompt` with `--layout nvim`.** Slightly
  ruder; the alternative loses the user's prompt with no signal.
- **Create only, not repair.** Deferred rather than dropped — if it turns out
  repair should accept a prompt, `WithPrompt` already works there; only repair's
  meaning needs deciding first.

---

## 8. Cross-Model Review Notes

### Resolved — review round 1 (2026-07-31)

**[IMPORTANT] The CLI test plan could not verify `RunE` safely.** Confirmed
against the code: `worktreeCreateCmd.RunE` builds `worktree.New()` inline, and
`cmd/worktree_test.go` avoids `RunE` throughout on purpose — the comments at
`cmd/worktree_test.go:48-55` and `:222` both state it, the second calling out the
package-wide split ("cmd tests pin the command's shape so it doesn't silently
drift"; behavior is proven in the package that can inject mocked Git/Tmux).
Step 7's "a prompt on the `nvim` layout fails before anything is created" was
unwritable as specified.

Resolution: the **pure helper** (`applyCreateLayoutOptions`, Step 6), not a
manager/factory seam. Reasoning:

- The no-side-effect guarantee comes from the helper returning an error before
  `worktree.New()` is reached, plus `Create`/`CreateAt` already calling
  `validateLayout` before `GetRepoRoot` (`worktree.go:262`, `:277`). Testing the
  helper tests the actual guarantee.
- An injectable manager factory would be new production machinery whose only
  consumer is one test. That is what CLAUDE.md §6 rules out, and it would cut
  against the deliberate cmd-vs-package test split above rather than with it.
- If a future change puts real logic inside `RunE`, that is when a seam earns its
  place. Today `RunE` between resolve and construct is one function call.

**[MINOR] `resetWorktreeFlags` not updated.** Confirmed and folded into Step 6 —
with a correction to the suggested fix: `Flags().Set("pane", "")` would _append_
an empty string rather than clear the slice (pflag's `stringArrayValue.Set`
appends once `changed` is true, `pflag@v1.0.9/string_array.go:16`), so the reset
itself would have introduced the leak. Step 6 uses `SliceValue.Replace(nil)` plus
clearing `Changed`, with the two-parses-in-sequence test the review asked for.

**Empty `--pane` was undefined.** Now specified: **rejected** before creation,
including whitespace-only (Step 5). `--pane "$VAR"` with an unset `VAR` is the
overwhelmingly likelier cause than a deliberate request for an idle shell, and a
silent empty pane looks like a half-working feature. A bare-shell pane, if ever
wanted, gets its own spelling — noted in §4 as out of scope.

The review also surfaced an asymmetry worth stating outright, now in Step 5 and
ADR-0011: a **prompt is shell-quoted** (one literal argument to a coder) while a
**`--pane` command is not** (it _is_ a shell command line; quoting it would break
`cd api && make dev`, the case that justifies the flag).

### Resolved — open questions (2026-07-31)

All four decided with the user before implementation started.

1. **`PromptCommand` on `AICoder`, not a package-level type switch.** It mirrors
   `Command()`/`EnsureInstalled()`, so the compiler forces any coder added later
   to decide its own prompt form instead of silently falling through a switch.
2. **More than one prompt-taking pane is an error.** Cheap now, and it means the
   first multi-coder layout fails loudly rather than prompting an arbitrary pane.
   Folded into Step 4.
3. **The functions fold into `Pane`; no third parallel slice.** `paneCheckers`,
   `newLayout`, and its arity panic all go away — the drift they guarded against
   becomes unwritable instead of merely detected (CLAUDE.md §4: prefer
   structurally impossible over guarded). Step 3 rewritten; the cost (a
   non-comparable `Pane`, so one test compares field-wise) is recorded there.
4. **The flag is `--pane`.** Shortest, and it matches the vocabulary the package
   already speaks (`Pane`, `buildWindowPanes`).

**Reviewer notes:**
Round 1 (external reviewer, 2026-07-31): risk rated Medium, "implementation is
sound, but the test plan cannot safely prove its main CLI failure guarantee."
Both findings verified and resolved above; the risk they described is closed by
Step 6's helper and reset. Re-review welcome on the helper placement (`cmd` vs
the worktree package) and on the four open questions.

---

## Notes for Implementers

- Commit after each step once its verify check passes.
- Step 0 (ADR) lands before Step 1 — CLAUDE.md §11 requires the decision be
  recorded before implementation, not after.
- Step 2 is not optional cleanup: it is the DRY requirement that the second use
  of the prompt-fragment logic collapses into one place in the same PR.
- Step 8 is part of "done", not a follow-up. `docs/spec.md` and `CLAUDE.md` are
  what the next AI session reads before touching this code (CLAUDE.md §2); flags
  that exist only in `--help` are invisible to it.
