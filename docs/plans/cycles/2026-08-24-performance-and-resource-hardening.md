# Cycle: Performance and resource hardening — act on the 2026-08-24 audit

**Date:** 2026-08-24
**Estimated Duration:** ~12 hours
**Status:** Done

---

## 1. Domain Context

On 2026-08-24 six agents audited the repo in parallel for performance defects
and security defects: file I/O and memory, install path and network, process and
resource handling, startup hot path, security, and TUI/binary size. Every claim
they made carries a `file:line` citation and either a measurement or a
structural argument; anything that could not be argued was dropped.

This cycle acts on the P0 (correctness and security) and P1 (measured
performance) results. The audit's raw notes live in a cache directory that can
be wiped at any time, so every citation and every number is carried into this
document — this file is the record, not the scratch file.

Two of the audit's design choices were already settled and written up as ADRs
before this cycle:

- [ADR-0029 — installed state is one cached listing](../../decisions/ADR-0029-installed-state-is-one-cached-listing.md)
- [ADR-0030 — global config loads once per run](../../decisions/ADR-0030-global-config-loads-once-per-run.md)

**ADR-0030's performance case is unmeasured.** The 95 `Load()` call sites were
counted, but the per-`Load()` cost never was. It rests on the correctness
argument — one consistent view of config within a run — not on a number.
Producing that number is an in-scope task below (Step 8), so the ADR can be
amended with a real figure rather than an implied one.

Related reading: [CLAUDE.md](../../../CLAUDE.md) §4 (non-negotiable rules), §6
(reuse before writing), §12 ("Anything we ship is built for strangers"),
[docs/guides/agent-permission-matching.md](../../guides/agent-permission-matching.md).

---

## 2. Engineer Context

### The grouping that matters

**Four of the six P0 items are one change to one file.** P0-1 (drain ordering),
P0-2 (`DeadlineExceeded` overriding a nil error), P0-5's executor half, and the
missing `Setpgid` all land in `internal/commands/base.go` — the shared executor
every external binary in the repo routes through. Treat them as a single focused
edit to `ExecCommand`, not four separate patches. Doing them separately means
touching the same 40 lines four times and re-reasoning about the same
`Wait()`/drain/cancel interaction each time.

The remaining P0s are elsewhere: the release/install supply chain
(`install.sh` + `.github/workflows/release.yml`), the shipped agent permission
payload (`configs/claude/`, `configs/opencode/`), and the un-fixed `curl | sh`
sibling in `internal/commands/debian_strategies.go`.

### Relevant files

- `internal/commands/base.go` — the shared executor. `ExecCommand` at `:266-430`,
  drain goroutines at `:320-400`, the `context.AfterFunc` pipe-closer at `:392`,
  `commandTimeoutContext` at `:455`, `MaybeInstall`/`checkInstalled` at `:477-522`.
- `internal/commands/macos.go:179,185` and `internal/commands/debian.go:219` — the
  per-package listing probes.
- `internal/commands/debian_strategies.go:274` — the `curl … | sh` pipeline.
- `install.sh:134-180` and `.github/workflows/release.yml:68,80-86` — release
  artifact production and consumption.
- `configs/claude/settings.json.tmpl`, `configs/opencode/opencode.json.tmpl`,
  `configs/claude/agent-config-guard.sh` — the shipped permission model.
- `internal/config/fromFile.go:306-320` (`Load`/`Save`), `internal/config/lock.go`
  (`Update`, the only locking mutation path).
- `internal/apps/devgeta/devgeta.go:70-87,107-121,155` and `embedded.go:32,54,60` —
  the config re-extract and the uninstall that removes the extracted tree.
- `pkg/files/files.go:18-22,64,83,96` — permission constants and the copy path.
- `pkg/github/release.go:14-25`, `pkg/downloader/retry.go:128-138` — HTTP.

### Correct patterns already in-tree — reuse them (CLAUDE.md §6)

These make several fixes much smaller than they look:

| Need                                           | Existing implementation to reuse                                                                                                                                                                              |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Stat-keyed config memoization                  | `internal/tooling/worktree/worktree.go:135-155`, hit/invalidate at `:796-817`. Key is `{path, modTime, size}` and survives tests repointing `paths.Paths.Config.Root`. Generalize it into `internal/config`.  |
| Correct file/dir modes                         | `FilePermission = 0o644` (`pkg/files/files.go:18`) and `DirPermission = 0o755` (`:20`) already exist and have **no callers** on the copy path.                                                                |
| Selective exec bit                             | `internal/apps/claude/claude.go:149-157` already chmods only the hook scripts 0755. Copy that shape.                                                                                                          |
| Path sanitizing                                | `internal/tooling/task/scratch.go:90-125` (lexical bounds, symlink refusal, re-check after `EvalSymlinks`) and `internal/tooling/reviewjournal/encoding.go:34` (`filepath.Dir(path) == filepath.Clean(dir)`). |
| Temp staging instead of predictable `/tmp`     | `pkg/apt/ppa.go:237-249` (`withStagingDir`, `os.MkdirTemp` + `defer os.RemoveAll`).                                                                                                                           |
| Staged download-then-run instead of a pipeline | `pkg/apt/ppa.go:238` — e7104b4 already did this here and left the sibling at `debian_strategies.go:274`.                                                                                                      |

### Testing patterns

`testutil.MockApp`, never real commands — see
[docs/guides/testing-patterns.md](../../guides/testing-patterns.md). Executor
changes are the highest-risk area for accidental real execution: every test that
exercises `ExecCommand` behavior must go through the mock base and end with
`testutil.VerifyNoRealCommands(t, mockApp.Base)`.

`internal/apps/opencode/permissions_test.go` fails the build on any asymmetry
between the two agents' permission sets. The P0-4 work must change both agents
in the same commit (CLAUDE.md §12).

### Commands to run tests

`internal/commands` and `pkg/files` are wide-blast-radius packages, and P0-4
touches `configs/`, which the root package's embedded-config tests read. Get the
importer list from the toolchain rather than guessing:

```bash
go list -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{range .TestImports}} {{.}}{{end}}' ./... \
  | grep 'devgeta/internal/commands' | cut -d' ' -f1
```

Per CLAUDE.md §6, a change to `internal/commands`, `pkg/files`, or `configs/`
warrants the full suite; say so when you run it.

---

## 3. Objective

Fix the six P0 correctness and security defects the audit found, and the seven
P1 measured performance defects, so that: the shared executor returns when the
child exits rather than when the last grandchild closes a pipe, a successful
command is never reported as timed out, a timed-out command kills its whole
process tree, a failed download can never be reported as a successful install,
the release binary is verifiable, the shipped agent deny rules that currently
match nothing actually match (Step 5 — not a claim that the `Bash(*)` gap is
closed),
and a fresh `dg install` stops spending ~54 seconds re-listing the same package
manager output.

No temporary fixes. CLAUDE.md §4 forbids them and the maintainer restated it for
this cycle: every remedy here is root-cause or it does not ship.

---

## 4. Scope Boundary

### In Scope — P0, correctness and security

- [ ] **The shared executor, one change** — `internal/commands/base.go`. Fixes
      four findings at once (see Step 1): the drain-order regression at
      `:400-410`, the nil-error override at `:411`, the missing process group,
      and the executor half of the `curl | sh` fix.
- [ ] **Release artifact verification** — publish `checksums.txt` from
      `.github/workflows/release.yml:80-86`, verify it in `install.sh:158-177`
      before `chmod +x`, and pin the three actions to commit SHAs.
- [ ] **The shipped agent permission model, as Step 5 re-scoped it** — re-anchor
      the 26 dead `~/` rules in `configs/opencode/opencode.json.tmpl` to `**/`, and
      add `gh api` plus the interpreter one-liners to both agents' command denies
      as friction, never worded as a boundary. Both agents, same commit.
      Extending `configs/claude/agent-config-guard.sh:97-100` past `Edit|Write` is
      **out**: [ADR-0014](../../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)
      §5 already rejected it.
- [ ] **`curl … | sh` at `internal/commands/debian_strategies.go:274`** — staged
      download, status check, then execute, through the executor.
- [ ] Tests for each of the above, mocked, with `VerifyNoRealCommands`.

### In Scope — P1, measured performance

- [ ] **Cached installed-package listing** — ~54 s measured. Implements
      [ADR-0029](../../decisions/ADR-0029-installed-state-is-one-cached-listing.md).
- [ ] **Atomic, skippable config extract** — `dg configure` currently destroys
      and re-extracts 152 files non-atomically.
- [ ] **One `global_config.yaml` parse per run** — implements
      [ADR-0030](../../decisions/ADR-0030-global-config-loads-once-per-run.md).
- [ ] **Measure the per-`Load()` cost** and amend ADR-0030 with the number.
- [ ] **0777 → 0644/0755 on the config deploy path** — `pkg/files/files.go:64,83,96`.
- [ ] **HTTP timeout on `pkg/github/release.go:15`**.
- [ ] **Shared HTTP client in `pkg/downloader/retry.go:134`**, and a timeout that
      bounds the stall rather than the transfer.
- [ ] **Install `govulncheck` and check dependency CVEs.** Currently not
      installed, so CVE status is **unverified, not clean**.

### Explicitly Out of Scope

- Everything in `## Deferred — not in this cycle` below.
- Building the install-level rollback CLAUDE.md §4 describes but the code does
  not implement (audit S8). That is a decision — build it or amend §4 — and it
  needs its own cycle and its own ADR, not a checkbox here.
- Retiring `pkg/promptui` in favour of `tuicomponents.FuzzyPicker` (audit A1).
- Replacing `go.uber.org/zap` with `log/slog` (audit B4).

**Scope is locked.** Anything discovered mid-cycle that falls outside it gets
written into a future cycle doc and referenced here, not absorbed.

---

## 5. Implementation Plan

### File Changes

| Action        | File Path                                                                            | Description                                                                                                                                                                                                    |
| ------------- | ------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Modify        | `internal/commands/base.go:392,400-411,455`                                          | `WaitDelay` + unconditional pipe close, timeout error only on real failure, `Setpgid` + process-group `Cancel`                                                                                                 |
| Modify        | `internal/commands/base.go:191,206,215,500`                                          | Consume the cached installed-package set instead of re-listing                                                                                                                                                 |
| Modify        | `internal/commands/macos.go:179,185`, `internal/commands/debian.go:219`              | Build the listing once per process — process-wide, not a per-instance field (Step 6)                                                                                                                           |
| Modify        | `internal/commands/macos.go:54,59,64,79`, `internal/commands/debian.go:57,66,70,119` | Drop the cached listing at the mutation boundary — every install **and uninstall** method                                                                                                                      |
| Modify        | `internal/commands/debian_strategies.go:274`                                         | Staged download → status check → execute, through the executor                                                                                                                                                 |
| Modify        | `.github/workflows/release.yml:80-86`                                                | Emit and attach `checksums.txt`; pin actions to SHAs                                                                                                                                                           |
| Modify        | `install.sh:158-177`                                                                 | Fetch and verify the checksum before `chmod +x`                                                                                                                                                                |
| Modify        | `configs/claude/settings.json.tmpl:15-38,46-81`                                      | Add `gh api` and the interpreter one-liners to the command denies — friction only; mirror the re-anchored path rules so `permissions_test.go` parity holds (Step 5)                                            |
| Modify        | `configs/opencode/opencode.json.tmpl:29,34-47,101-114`                               | Re-anchor the 23 safe dead `~/` rules to `**/`; the `~/.claude/*.{json,sh,md}` trio needs a probed shape (Step 5); mirror the Claude command denies                                                            |
| Unchanged     | `configs/claude/agent-config-guard.sh:97-100`                                        | Deliberately **not** extended past `Edit\|Write` — ADR-0014 §5 rejected it (Step 5)                                                                                                                            |
| Modify        | `internal/config/fromFile.go:306-345`, `internal/config/lock.go`                     | Stat-keyed `Load()` cache; `Save()`/`Reset()`/`Create()` refresh the entry, `Update` bypasses on read                                                                                                          |
| Modify        | `internal/apps/devgeta/devgeta.go:70-87,107-121,155`                                 | Version-stamped skip (in a new stale-checking seam, **not** in `Install()`); extract to `configs-<stamp>` and commit by renaming a symlink over the pointer; uninstall the validated target, not just the link |
| Create        | `pkg/buildinfo/`                                                                     | Leaf package owning `Version`/`Commit`/`BuildDate` — `internal/apps/devgeta` cannot import `cmd` (cycle), Step 9 Part 1                                                                                        |
| Modify        | `cmd/version.go:14-21`, `Makefile:9-11`, `.github/workflows/release.yml:67-70`       | Read from / inject into `pkg/buildinfo` instead of `cmd`                                                                                                                                                       |
| Modify        | `cmd/configure.go:29-33,72-80`                                                       | Route `refreshEmbeddedConfigs` through the stale-checking seam so `--force` still re-extracts                                                                                                                  |
| Modify        | `pkg/files/files.go:64,83,96`                                                        | `FilePermission` / `DirPermission` instead of `AllPermissions`                                                                                                                                                 |
| Modify        | `pkg/github/release.go:14-25`                                                        | Client with timeout; drain body on the non-200 path                                                                                                                                                            |
| Modify        | `pkg/downloader/retry.go:126-138`                                                    | Transport-level timeouts + body-inactivity bound replacing the whole-request `Timeout`; package-level client for the custom transport, **not** for keep-alive                                                  |
| Modify        | `docs/decisions/ADR-0030-global-config-loads-once-per-run.md`                        | Add the measured per-`Load()` cost                                                                                                                                                                             |
| Create/Modify | `*_test.go` alongside each of the above                                              | Mocked tests, both paths                                                                                                                                                                                       |

### Step-by-Step

#### Step 1: The shared executor — findings P0-1, P0-2, P0-6 and half of P0-5

**Done** — see commit range `20c7106..dc34c60` (Step 1 plus Step 2's real-subprocess
tests, implemented together). The executor was reworked as this step describes:
`WaitDelay` plus a line-framing writer so `ExecCommand` returns when the child
exits rather than when the last grandchild closes a pipe, the nil-error override
fixed so a successful command is never reported as timed out, and a gated
`Setpgid` process group so a timed-out command kills its whole tree. The gate
that shipped is narrower than this text specifies: `NoStdin && ctx.Done() != nil`,
not `NoStdin` alone — approved, because without a deadline `Cancel` is never
invoked, so a process group buys nothing while still costing SIGINT delivery to
a terminal-inheriting child for no benefit. See Step 1b immediately below for a
consequence of `Setpgid` this text did not anticipate.

This is the grouped change. All of it is in `internal/commands/base.go`.

**P0-1, the drain-order regression (`:400-410`).** Before e7104b4 the order was
`execCommand.Wait()` then `wg.Wait()`; `Wait()` closes the parent's pipe read
ends, so the drain goroutines always unblocked once the direct child exited.
e7104b4 swapped it to `wg.Wait()` (`:409`) then `execCommand.Wait()` (`:410`) —
right for not truncating output, wrong in that `ExecCommand` now returns only
when **every** holder of the inherited stdout/stderr write end closes it, not
when the child exits. The one escape hatch, the `context.AfterFunc` pipe-closer
at `:392`, is gated on `ctx.Done() != nil`, and `commandTimeoutContext` (`:455`)
returns `context.Background()` for a zero timeout — so the hatch does not exist
unless a `Timeout` was requested. Only **5 of 107** non-test `ExecCommand(` call
sites set one.

Measured with a reproducer whose child is `sleep 5 & echo started; exit 0` —
that is, the child itself exits in ~0 ms:

```
Timeout=0   (unbounded): returned after 5.204s (child exited immediately), err=<nil>
Timeout=1s            : returned after 1.003s, err=<nil>
```

With a real daemon instead of `sleep 5`, 5.204s becomes unbounded. Anything that
backgrounds a worker — `curl … | sh` installing a daemon, `brew services start`,
an agent spawning a helper — blocks `dg` for the grandchild's lifetime, with no
deadline, no output, and no exit but Ctrl-C.

Fix: stop making the pipe close conditional on a timeout. `WaitDelay` is the
right primitive, but **setting it on today's code does nothing** — reordering
`:409`/`:410` is not enough either, and the reason decides the shape of the fix.

`WaitDelay`'s pipe-forcing branch lives in `Cmd.awaitGoroutines`, which returns
immediately at `os/exec/exec.go:975-977` ("No running goroutines to await")
whenever `c.goroutineErr == nil`. That channel is allocated only when exec owns
copying goroutines (`exec.go:744-745`, `if len(c.goroutine) > 0`), and
`StdoutPipe`/`StderrPipe` create none — they hand the caller a read end and append
it to `parentIOPipes` (`exec.go:1082-1097`), nothing more. `ExecCommand` drains
those two pipes with its own goroutines, so exec has no copy to time out and
`WaitDelay` never arms. Verified by reading Go 1.26.5's `exec.go`; the same source
shows `Wait()` closing `parentIOPipes` unconditionally on the way out
(`exec.go:954`), which is why the pre-e7104b4 order used to unblock the drainers.

What works is giving exec the copying job so it has something to police:

1. Replace `StdoutPipe`/`StderrPipe` and the two hand-rolled drain goroutines with
   `execCommand.Stdout`/`Stderr` set to a line-framing `io.Writer` — a small type
   that appends to the capture buffer, emits on each `\n` to the debug log and
   `OnStdoutLine`, and holds a partial trailing line. It must split on `\n` from an
   unbounded buffer, not `bufio.Scanner`, to preserve the >64 KB-line handling the
   current drainer documents at `base.go:342-348`. `Stream` keeps teeing raw bytes
   via `io.MultiWriter(buf, os.Stdout)`. Any non-`*os.File` writer makes exec
   create the pipe **and** the copying goroutine, which is what populates
   `c.goroutineErr`.
2. Set `execCommand.WaitDelay = <grace>`. It then behaves as advertised even with
   a zero `Timeout`: with no cancellable context `watchCtx` does not run
   (`exec.go:780`), so `Wait` passes a nil timer to `awaitGoroutines`, which starts
   its own `time.NewTimer(c.WaitDelay)` at `exec.go:993` and force-closes the pipes
   at `:998`. No change to `commandTimeoutContext` is needed for this.
3. `Wait()` returns `ErrWaitDelay` when the grace expires. The child exited fine in
   that case — the executor must not surface it as a command failure. Fold this
   into the P0-2 error handling below rather than leaving two error-shaping rules.
4. `wg`, the `:409`/`:410` ordering question, and the `context.AfterFunc` closer at
   `:391-397` all disappear with the drainers instead of being re-tuned.

That keeps e7104b4's no-truncation intent — the grace **is** the post-exit drain
window e7104b4 was protecting — without the unbounded wait. What the plan must not
do is assert `WaitDelay` alone fixes this: while the caller owns the pipes it is a
no-op regardless of where `Wait()` is called.

**P0-2, the nil-error override (`:411`).**
`if ctx.Err() == context.DeadlineExceeded { err = fmt.Errorf(...) }` overrides
`err` unconditionally, including when `execCommand.Wait()` returned `nil`. With
P0-1 in play it is reachable: the child succeeds, a grandchild holds the pipe
past the deadline, the AfterFunc closes the pipes, `Wait()` returns nil — and the
caller is told the command timed out. Callers that roll back on error roll back
completed work. Fix: synthesize the timeout error only when `err != nil`, or when
the process was actually killed by the cancel.

**P0-6, no process group.** `grep -rn 'SysProcAttr\|Setpgid\|Process.Kill\|Signal('`
over non-test code returns **zero hits**. Every timeout relies on
`exec.CommandContext`'s default `Cancel`, which is `Process.Kill()` on the direct
child only. `sh -c`, `brew`, `apt` and the agent CLIs all fork; the forks
survive. Measured: `grandchildren still alive after parent killed at deadline: 1`.
Worst case is `reviewRunTimeout = 30 * time.Minute`
(`internal/apps/opencode/opencode.go:366`, `internal/tooling/task/reviewrun.go:66,287`)
— the largest process tree devgeta spawns, left running with nothing to reap it.
Fix: `execCommand.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` plus a
`Cancel` that does `syscall.Kill(-execCommand.Process.Pid, syscall.SIGKILL)`
(SIGTERM then SIGKILL after a grace if a graceful stop matters).

**It cannot be applied to every invocation, and the reason is not just Ctrl-C.**
`base.go:303-305` gives the child devgeta's own stdin unless the caller sets
`NoStdin`. A child in a new process group is not the terminal's foreground group,
so the first time it _reads_ the controlling terminal the kernel raises SIGTTIN
and stops it — no error, no output, a stopped process the user cannot type into.
That is precisely `sudo`'s password prompt, which `dg install` depends on and
which `base.go:122-125` documents as the reason stdin is inherited by default.
Blanket `Setpgid` therefore converts every sudo install into a silent hang. The
same applies to any interactive child that reads the tty (`fzf`, `$EDITOR`), and
separately the group stops receiving the terminal's SIGINT.

The general rule that holds: **give a child its own process group only when it
does not inherit the terminal** — i.e. gate `Setpgid` and the group `Cancel` on
`cmd.NoStdin`. Terminal-inheriting commands stay in devgeta's group and keep
today's single-process kill; they are also the ones a human is sitting in front
of and can Ctrl-C.

That gate is worthless until one more change lands with it: `grep -rn NoStdin
--include=*.go | grep -v _test.go` finds exactly **one** non-test caller,
`internal/apps/git/git.go:281`. The 30-minute `opencode run` review trees — the
largest process tree devgeta spawns, and the whole reason this finding is P0 —
build their `CommandParams` at `internal/apps/opencode/opencode.go:362-368`
without `NoStdin`, so the gate would skip them. `Run` is headless, machine-read,
and always on a timeout, which is the case `NoStdin`'s own doc comment
(`base.go:115-125`) describes; set `NoStdin: true` there in the same commit. Audit
the other 106 call sites the same way: anything headless or on a timeout that
still inherits stdin is both a hang risk and outside the group-kill.

One place for the mechanism — every external binary already routes through this
executor — but the gate is what makes it safe to put there.

- Verify: `go build ./internal/commands/` and the reproducer shape as a test —
  a command that backgrounds a child returns promptly; a command that succeeds
  returns `nil`; a timed-out command leaves no live grandchildren. These are
  real-subprocess tests, not mocked ones; Step 2 says why and under what rules.

##### Step 1b: SIGINT handling — mid-cycle scope addition (not in the original plan)

**Done** — commit `263b0ba`, approved as a scope addition after Step 1 shipped
rather than deferred (this document's §4 says scope is locked; the addition was
argued and approved on its own merits, not smuggled in). `Setpgid` puts a
qualifying child outside the terminal's foreground process group, so terminal
SIGINT stops reaching it — Ctrl-C during a long `dg task review-run` killed
devgeta itself but left the `opencode` process tree running, up to 30 minutes,
billed the whole time. This was discovered only after Step 1 shipped; it does
not appear anywhere in this document's original text. Fix: a process-level
SIGINT handler in `cmd/` that cancels the executor's contexts, so Ctrl-C takes
the same group-kill path a timeout already does. Justification is CLAUDE.md §4
(fix the root cause; do not ship a known, undocumented regression) and this
document's own §7 instruction to escalate a materializing risk rather than
absorb it quietly — the §7 risk table's own "`Setpgid` changes signal delivery
for interactive children" row is exactly this, surfacing exactly the way §7
said it might.

#### Step 2: Executor tests

**Done** — see commit range `20c7106..dc34c60`, implemented in the same task as
Step 1. The three real-subprocess tests this step calls for shipped as
specified: backgrounded-grandchild, success-under-deadline, and cancel-kills-
the-group.

**These cannot be mocked, and the plan has to say so up front.** All three
behaviors under test are properties of `exec.Cmd` itself — whether `Wait`
returns before a grandchild closes the inherited pipe, whether a nil `Wait`
error survives a deadline, whether the kill reaches the process group. A
`testutil.MockApp` records the `CommandParams` a caller built and never runs
`exec.CommandContext` (`internal/commands/base.go:286`, which has no injectable
seam), so a mocked test can only assert that a caller filled in a struct. It
cannot fork a grandchild, cannot hold a pipe open past the child's exit, and
cannot observe a process group. A "mocked" version of these three assertions
would pass against the unfixed code.

The repo already has this exception and its rules: `internal/commands/exec_pipes_test.go`,
`env_overlay_test.go`, `exec_longline_test.go` and `exec_onstdoutline_test.go`
drive the real `ExecCommand` via `commands.NewBaseCommandCustom(FakePlatform{Linux: true})`,
with the rationale written at the top of `exec_pipes_test.go` — "what is under
test is the wiring of `exec.Cmd` itself … `internal/commands` is the boundary
that shells out (CLAUDE.md §6). Every command below is a hermetic `bash -c` that
touches nothing outside the process: no packages, no user files, no network."
These tests join that set and inherit those constraints exactly:

- Every command is a hermetic `bash -c` — no package manager, no network, no
  path outside the process. `(sleep 0.4; echo written-after-exit) & exit 0` is
  the existing shape for the grandchild case.
- Live in `package commands_test` beside the four above, not in an app package.
- Assert on timing bounds and on `os.FindProcess`/`syscall.Kill(pid, 0)` for the
  grandchild, not on any user-visible state.

`testutil.VerifyNoRealCommands` does **not** apply here — it asserts a mock base
ran nothing, and these tests deliberately run something. Using it would be the
mock-safety trap CLAUDE.md warns about in reverse: a check pointed at a base
nothing under test uses, passing for the wrong reason.

Three tests, all real-subprocess:

- Backgrounded grandchild: `ExecCommand` returns within the `WaitDelay` grace
  even though the grandchild outlives it, and the pre-exit output is not
  truncated (that second half is what `exec_pipes_test.go` already guards —
  extend it rather than duplicating it).
- Success under a deadline returns `nil`, not a synthesized timeout error.
- Cancel kills the group: with `NoStdin: true` set, a timed-out `bash -c` that
  forks leaves no live grandchild. Skip on non-Unix; `syscall.Kill` with a
  negative pid is not portable.

The rest of the executor's surface — that callers pass the right
`CommandParams`, including the new `NoStdin: true` at
`internal/apps/opencode/opencode.go:362-368` — stays mocked in the app packages,
where `testutil.VerifyNoRealCommands(t, mockApp.Base)` does apply and must
target the same base the app was built with.

- Verify: `go test ./internal/commands/`

#### Step 3: `curl … | sh` — P0-5

**Done** — see commit range `263b0ba..b789640`. `curl | sh` is now staged
through the executor (download → status check → execute) with two separate
timeouts, at both `debian_strategies.go` and `claude.go` as this step required,
and the class was swept as instructed — `tar`, `fc-cache`, and `git clone` all
route through the executor now too.

`internal/commands/debian_strategies.go:274`:

```go
curlCmd := exec.Command("sh", "-c", fmt.Sprintf("curl -fsSL %s | sh", s.scriptURL))
```

Three defects, all of which e7104b4 fixed in `pkg/apt/ppa.go` and left here:

1. No `set -o pipefail` — the pipeline's status is `sh`'s, not `curl`'s. A 404 or
   a truncated download pipes an empty or partial script into `sh`, which exits
   0, and the install is reported **successful**. Silent broken install.
2. No timeout and no context — a stalled TCP connection hangs `dg install`
   forever.
3. Raw `exec.Command`, bypassing the shared executor, so it gets none of the
   streaming/timeout/error-wrapping behavior and is unmockable (CLAUDE.md §6,
   "Route external tools through their app wrappers").

Fix: run `curl` and `sh` as two staged steps through the executor — download to a
temp file, check the status, then execute — the way `withStagingDir` already does
at `pkg/apt/ppa.go:238`. Adding `pipefail` alone is a symptom fix and does not
satisfy §4.

**Set an explicit download timeout; staging alone does not add one.**
`CommandParams.Timeout` is zero unless a caller sets it, and
`commandTimeoutContext` (`internal/commands/base.go:455`) returns
`context.Background()` for a zero timeout — so routing `curl` through
`ExecCommand` leaves defect 2 exactly where it was. Only 5 of the non-test
`ExecCommand` call sites set a `Timeout` at all
(`internal/tooling/terminal/dev_tools/githubcli/githubcli.go:477`,
`internal/tooling/task/reviewrun.go:287`, `internal/apps/git/git.go:280`,
`internal/apps/opencode/opencode.go:366`, `pkg/downloader/retry.go:135`), and
`withStagingDir` is not one of them. The staged `curl` step must carry a named
constant — the same shape `pkg/downloader/retry.go:135` already uses,
`30 * time.Second` — plus `--max-time` on `curl` itself so the bound holds even
if the process is later invoked outside this executor. The `sh` execution step
gets its own, longer budget: an install script legitimately runs for minutes, so
reusing the download bound would kill working installs.

Timeout is a **stall** bound, not a transfer bound, and install scripts are
small (kilobytes), so a whole-request deadline is safe here in a way it is not
for the multi-megabyte font archives in Step 11 — that step's body-stall
machinery is deliberately not needed at this size.

**The same defect exists at `internal/apps/claude/claude.go:61-71` and this step
covers it too**, or the "class is fixed" claim is false:

```go
params := cmd.CommandParams{
    Command: "sh",
    Args:    []string{"-c", "curl -fsSL https://claude.ai/install.sh | bash"},
}
```

It already routes through the executor, so defect 3 does not apply — but it has
no `pipefail` and no `Timeout`, so defects 1 and 2 are both live. Measured:
`bash -c 'false | bash'; echo "exit=$?"` prints `exit=0`, so a failed or
truncated Claude download is reported as a successful install today. Convert it
to the same staged download → status check → execute shape, with the same two
timeouts. Grep for the pattern before implementing (`rg -n 'curl [^|]*\| *(sh|bash)'
--glob '!*_test.go'`) and fold in anything else it finds rather than fixing two
instances and re-opening the class later.

Same file, same class, lower severity — raw `exec.Command` + `CombinedOutput`
with no timeout at `:333` (`tar -xf`), `:339` (`fc-cache -fv`), `:376`
(`git clone`, a network operation that can hang indefinitely). Route them through
the executor in the same pass.

- Verify: `go test ./internal/commands/ ./internal/apps/claude/`; a mocked 404
  must fail the install, in both `debian_strategies.go` and `claude.go`. Assert
  the timeout is actually set — the mocked `CommandParams` for the download step
  must carry a non-zero `Timeout` — because a test that only checks the staged
  shape passes with defect 2 still present.

#### Step 4: Release artifact verification — P0-3

**Done, workflow half only** — see commit range `21ee42f..14df903`. The three
existing GitHub Actions plus the newly-added `actions/attest-build-provenance`
are pinned to verified commit SHAs; `checksums.txt` is now a release asset; the
four binaries are attested. The `install.sh` verification half is deferred per
the rollout order this step already specifies below — it goes into the
follow-up cycle doc at
[docs/plans/cycles/2026-08-25-install-sh-verification.md](2026-08-25-install-sh-verification.md),
gated on confirming a real tagged release actually carries `checksums.txt`
before it starts. The verification-policy decision this step asks for is
recorded in ADR-0031.

`install.sh:158` fetches the release binary, `:166` checks only that it is
non-empty, `:171` chmods it +x, `:173` **executes it** (`"$TMP_BINARY" --version`),
and `:180` moves it onto PATH. No checksum, no signature anywhere in the script.

It could not verify one if it wanted to: `.github/workflows/release.yml:80-86`
lists only the four platform binaries in the `softprops/action-gh-release@v1`
`files:` block — no `checksums.txt` asset is produced. All three actions are
pinned to mutable tags (`actions/checkout@v4`, `actions/setup-go@v5`,
`softprops/action-gh-release@v1`), so the build itself has no supply-chain pin.

This is the highest blast radius in the repo — it is how every user gets devgeta
— and it contradicts CLAUDE.md §4, "Never execute arbitrary downloaded code
without verification".

**What a checksum can and cannot do here.** `install.sh:134` asks the API for
`releases/latest` and `:153` builds the download URL from whatever tag came back;
there is no pinned version anywhere in the script. A `checksums.txt` published as
an asset of that same release is fetched over the same channel, from the same
mutable release, under the same credentials — so an attacker who can replace
`devgeta-darwin-arm64` can replace `checksums.txt` in the same motion, and the
check passes. It buys real protection against a truncated or corrupted transfer
and against a network attacker who cannot also write to the release; it buys
**nothing** against a compromised token, workflow, or account. Calling that
"verification" of downloaded code would satisfy the letter of CLAUDE.md §4 while
leaving the threat it names untouched, so the plan must not claim it does.

Verification needs a trust root outside the release assets. The one that fits a
zero-dependency installer is GitHub's build-provenance attestation: add
`actions/attest-build-provenance` to the release job (`id-token: write`,
`attestations: write`), which signs a statement binding each binary's digest to
the workflow that produced it and records it in Sigstore's public transparency
log. The signature chains to that log and the workflow identity, not to anything
an attacker can rewrite by replacing a release asset — and it needs no
maintainer-held key.

Fix, in order:

1. Workflow: pin `actions/checkout`, `actions/setup-go`, and
   `softprops/action-gh-release` to commit SHAs; emit
   `sha256sum devgeta-* > checksums.txt` and add it to `files:`; attest the four
   binaries.
2. `install.sh`: verify the SHA-256 against `checksums.txt` before `chmod +x`
   (`:171`) — that is the corruption check, and it is the only one available with
   bash and curl alone. Then, **when `gh` is present**, run
   `gh attestation verify "$TMP_BINARY" --repo "$REPO"` and fail closed on a
   mismatch. Print which of the two checks actually ran; a script that says
   "verified" without saying what it verified is the overclaim moved from the plan
   into the product.
3. Document the residual gap in the release guide: with only bash and curl, the
   floor is integrity, not authenticity.

**Open decision for the maintainer** (release policy, not something this cycle can
settle): whether authenticity becomes mandatory — i.e. `install.sh` requires `gh`,
or ships an embedded public key and a `minisign`/`cosign` signature check. Both
break product principle 1 ("no pre-installed tools required beyond bash/curl") or
add key management, and `install.sh` is itself served unpinned from
`raw.githubusercontent.com/.../main/install.sh`, so an embedded key is only as
good as that fetch. Decide it before this step is implemented and record it as an
ADR; step 2 above is the default if it is not.

**Rollout order — the two changes cannot ship together.** `install.sh` is fetched
live from `main` (see its own usage line at `install.sh:10`), while the assets it
verifies only exist from the next tag onward. Merging the verification and the
workflow change in one release means every `curl … | bash` between that merge and
the next tag fetches a script that demands a `checksums.txt` the current `latest`
release does not have — a hard failure for every new install in that window, and
for anyone re-running the installer. Because the script always resolves `latest`
(`:134`, `:153`), older releases are not otherwise reachable, so the exposure is
that window rather than the whole release history. Sequence it:

- **Release N** — workflow change only: `checksums.txt`, attestation, SHA pins.
  `install.sh` untouched.
- **Release N+1** — `install.sh` starts verifying, after confirming release N's
  page actually carries the asset.

Never make the verification a soft "skip if missing": that is a downgrade an
attacker can trigger by withholding the file.

- Verify: run the workflow on a test tag; confirm the release page carries
  `checksums.txt` and that `gh attestation verify` passes against the published
  binary; confirm `install.sh` refuses a tampered binary and refuses a binary
  whose attestation does not verify.

#### Step 5: The shipped agent permission model — P0-4, re-scoped

**Done** — see commit range `14df903..50b0aae`, re-scoped per this step's own
text (ADR-0014 governs; this is not a broader path-deny). What shipped: the 23
safe dead OpenCode rules re-anchored, the probed `~/.claude/*.{json,sh,md}`
trio, and `gh api` plus the interpreter denies added to both agents as
friction.

**The probe this step calls for did not confirm what this document implicitly
assumed — it inverted it.** This step's re-scoping reasons entirely from
OpenCode's measured behavior (guide §1-§4) and treats Claude Code as the same
matcher, differing only in syntax. The live probe (guide §5, run against both
agents including the control run) found OpenCode and Claude Code are opposites
on both axes this step's reasoning was built on: `~/`-anchored patterns are
dead on OpenCode but live on Claude Code, and `*` crosses `/` on OpenCode but
not on Claude Code. Had the 23 "dead" OpenCode rules been _re-anchored_
(replaced) on the Claude Code side too — rather than _added to_, which is what
this step actually instructs and what shipped — it would have been a real
security regression on Claude Code: rules that currently fire there would have
been swapped for a glob shape that doesn't cross `/` on that agent and so
stops matching the paths they were written to catch. The shape that shipped —
both the original `~/` spelling and the `**/` spelling side by side, on both
agents, rather than a straight re-anchor — is a deliberate deviation from this
document's original instruction, made because the instruction's premise (one
matcher's behavior generalizes to the other) didn't hold. This is exactly the
kind of surprise CLAUDE.md and this document's own §7 ask to be surfaced, not
smoothed over. The full probe evidence and the corrected reasoning live in
[docs/guides/agent-permission-matching.md](../../guides/agent-permission-matching.md)
(substantially rewritten with the probe results) and in ADR-0014's dated
correction note — that is the durable record; it is not reproduced here.

`configs/claude/settings.json.tmpl:5` allows `Bash(*)` (with
`defaultMode: acceptEdits` at `:83`); `configs/opencode/opencode.json.tmpl:51`
has `"*": "allow"`. The 20 Read denies (`settings.json.tmpl:39-61`) and 20 Edit
denies (`:62-81`) constrain only the Read/Edit **tools**. `cat ~/.ssh/id_rsa`,
`sed -i ~/.claude/settings.json`, `tee`, `python3 -c`, `node -e` all bypass them
— and also bypass `configs/claude/agent-config-guard.sh:97-100`, which matches
`Edit|Write` only. The command denies at `settings.json.tmpl:15-38` are
prefix-shaped, so `env curl`, `/usr/bin/curl`, `xargs curl`, `bash -c 'curl …'`
all evade `Bash(curl *)`, and `gh api` is never denied — outbound exfiltration
survives the curl/wget bans.

**All of that is already known, already decided, and the decision went the other
way.** [ADR-0014](../../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)
§1 states the same `Bash(*)` gap in the same words ("`echo … > .claude/settings.json`,
`sed -i`, `cp`, or a `python3 -c` one-liner reaches every file discussed below
without touching Edit or Write at all"), calls the guard "friction and
defense-in-depth, **not** a security boundary", and names Claude Code's OS sandbox
(`/sandbox`, Seatbelt on macOS) as the real boundary. Its §5 then records
**"Rejected: extend the guard to Bash writes"** — because deciding whether an
arbitrary shell command writes to a path is undecidable (`sh -c`, `python3 -c`,
base64, variable indirection, heredocs), so a matcher "would stop only an agent
that was not trying, while reading as though the gap were closed". The audit's
P0-4 proposed exactly the rejected option, and the file-change rows for
`settings.json.tmpl` and `agent-config-guard.sh:97-100` encoded it, without this
plan citing ADR-0014 once. That is the conflict, and it resolves against the plan:
a pattern list cannot be made sound, so no version of it may be shipped as though
it closed the gap.

Re-scoped, this step is:

- **In, and still P0 — the 26 dead OpenCode rules.** `configs/opencode/opencode.json.tmpl:29,34-37,39-47`
  (read) and `:101-114` (edit) are `~/`-anchored; paths arrive project-relative
  (`../../../../.claude/…`), exactly as
  [docs/guides/agent-permission-matching.md](../../guides/agent-permission-matching.md)
  §1 documents, so none of them match anything. They are masked only by
  `external_directory` (`:117-119`), so widening that grant at all exposes reads
  _and_ writes. This is not a boundary claim — it is a rule that does not do what
  it says, which is a correctness defect on either reading of ADR-0014, and
  fixing it restores parity with the Claude side.

  **A blanket "re-anchor all 26 to `**/`" is wrong and must not be the
  instruction.** Three of the 26 —
  `~/.claude/*.json`, `~/.claude/*.sh`, `~/.claude/*.md` — walk into the trap
  that same guide documents in §4: because `*` **crosses `/`** (§2, verified —
  `**/probe/*.md` matched `probe/sub/deep.md`), `**/.claude/*.md` would also
  match `~/.claude/projects/<slug>/memory/notes.md`, and memory is data the
  agent is _meant_ to write per ADR-0014's memory amendment. Making the floor
  fire would re-break memory. Split the 26 accordingly:

  - **23 are safe to re-anchor verbatim** — every rule whose pattern is either
    an exact filename (`~/.npmrc`, `~/.netrc`, `~/.git-credentials`, `~/.pgpass`,
    `~/.pypirc`, `~/.zsh_history`, `~/.bash_history`, `~/.docker/config.json`,
    `~/.zshrc`, `~/.bashrc`, `~/.profile`) or a literal directory prefix with a
    `**` tail (`~/.ssh/**`, `~/.aws/**`, `~/.kube/**`, `~/.azure/**`,
    `~/.config/gcloud/**`, `~/.config/opencode/**`, `~/.claude/agents/**`,
    `commands/**`, `skills/**`, `plugins/**`, `hooks/**`, `lib/**`). None
    contains a bare `*` segment, so `*`-crossing-`/` cannot widen them, and none
    of the `.claude` subdirectory names collides with `projects/`.
  - **3 need a shape decided before they ship** — the `~/.claude/*.{json,sh,md}`
    trio. Two candidate shapes, and the step must **probe** the chosen one
    rather than reasoning about it, because guide §4 is explicitly marked
    "Derived, not probed":
    - Enumerate by **exact filename** (guide §2's own advice: "Narrow by exact
      filename instead"). Correct on both agents and expressible in Claude
      Code's format, but it rots — a new top-level file in `~/.claude/` is
      uncovered until someone notices.
    - A broad deny plus a longer-pattern allow carve-out for
      `**/.claude/projects/*/memory/*.md`. OpenCode resolves longest-pattern-wins
      so this can work, and unlike the bare-`.md` allow guide §4 dismisses, this
      one is anchored on the literal `projects/` and `memory/` segments so it
      cannot re-open `~/.claude/CLAUDE.md`. But Claude Code has **no negation**
      (guide §3), so the carve-out is inexpressible there — and
      `internal/apps/opencode/permissions_test.go` compares the two configs as
      strings, so this shape breaks parity by construction.
      Given the parity test, the exact-filename enumeration is the only one that
      can ship to both agents today; the carve-out shape needs the parity test's
      contract revisited first, which is out of this cycle's scope. Whichever is
      chosen, probe it with the procedure in guide §5 — including the control run
      with the rule absent — and update guide §4 to say **probed**, with the
      result. Probe **both** agents, not just OpenCode: the parity test forces
      the same strings into `configs/claude/settings.json.tmpl:69-71`, and
      whether Claude Code's `*` crosses `/` has never been measured, so
      re-anchoring there could break memory writes on the agent where the `~/`
      rules currently do fire.

- **In, as friction, explicitly not a boundary.** Adding `gh api` and the
  interpreter one-liners to the command denies is cheap and raises the bar on
  unsophisticated injection. It ships only if the same commit says so in the
  config's own comments and in `docs/guides/agent-permission-matching.md` — never
  worded as closing the `Bash(*)` gap. Anything more elaborate (matching redirect
  targets, `tee`/`sed -i` destinations) is the rejected option and does not ship.
- **Out — extending `agent-config-guard.sh` past `Edit|Write`.** Directly
  contradicts ADR-0014 §5. Reversing an ADR takes a superseding ADR, argued on
  new evidence, not a checkbox in a performance cycle.
- **Out, and the actual answer — the OS sandbox.** ADR-0014's Consequences already
  say "`/sandbox` is that control". Making it the default for high-autonomy work
  is the enforceable, general fix, it is a decision this cycle did not scope, and
  it belongs in its own ADR and cycle. Recorded here so the gap is not left
  looking unowned.

Also worth flagging before anything is argued on top of it: ADR-0014's own status
line is `PROPOSED` (its amendment is ACCEPTED). Whatever this cycle concludes,
that status needs resolving, or the repo has an unaccepted ADR being cited as
precedent.

This payload ships to every user's other repos — CLAUDE.md §12. Whatever lands
must be a general protection, and it must land in **both** agents in the same
commit or `internal/apps/opencode/permissions_test.go` fails the build. That test
compares pattern strings, not what they match, so a symmetric pair of rules that
enforces nothing still passes — check the matching behavior by hand against the
permission-matching guide.

- Verify: `go test .` (root package — embedded-config tests) and
  `go test ./internal/apps/opencode/ ./internal/apps/claude/`; then deploy both
  with `dg configure claude --force` and `dg configure opencode --force` and
  confirm a project-relative `../../.claude/settings.json` read is now refused by
  OpenCode. Do **not** add a test asserting that a `cat` of a denied path is
  refused — it is not, by design, and a test claiming otherwise would encode the
  overclaim ADR-0014 warns about.

---

#### Step 6: Cached installed-package listing — P1, ~54 s measured

**Done** — see commit range `b789640..2eb9427`. The installed-package listing
is now cached process-wide per ADR-0029, and invalidated on attempted (not just
successful) mutation at all 8 seam methods this step names —
`InstallPackage`/`InstallDesktopApp`/`UninstallPackage`/`UninstallDesktopApp`
on both `MacOSCommand` and `DebianCommand`.

Every idempotency probe shells out a full package-manager listing, once per
package. `internal/commands/macos.go:179` runs `brew list` (the entire Cellar)
and `:185` runs `brew list --cask` (every cask) each time a single package is
checked; `internal/commands/debian.go:219` does the same with `dpkg -l`. All
three hand the `*exec.Cmd` to `internal/commands/base.go:191`
(`IsPackagePresent`), which buffers the whole listing and linearly scans it for
one name (`base.go:206`, `:215`). The result is discarded; the next package
re-runs the identical command. Call site is `internal/commands/base.go:500`
(`checkInstalled(pkgToInstall)` inside `MaybeInstall`).

Measured on macOS, 242 formulae / 15 casks, read-only probes:

| Command                               | Runs (s)           | Mean     |
| ------------------------------------- | ------------------ | -------- |
| `brew list` (full)                    | 0.90 / 0.69 / 1.43 | **1.01** |
| `brew list --formula`                 | 1.24 / 1.05 / 0.96 | 1.08     |
| `brew list --cask`                    | 0.52 / 0.47        | 0.50     |
| `brew list jq` (targeted, one pkg)    | 6.36 / 7.35 / 4.43 | **6.05** |
| `brew --version` (brew startup floor) | 0.31 / 0.32 / 0.24 | 0.29     |

~55 of the 96 `MaybeInstall*` references execute on a full macOS `dg install`
(9 terminal apps + 14 devtools + 16 core libs + 6 desktop apps + fonts +
languages + databases). 55 × 1.01 s = **~55.6 s** of pure detection. One
populated set costs `brew list --formula` 1.08 s + `brew list --cask` 0.50 s =
**~1.6 s**. Saving: **~54 s**. On a fresh Mac with an empty Cellar the listing
collapses to brew's startup floor, so 55 × 0.29 s ≈ 16 s → ~0.6 s — still ~15 s.

Scoping caveat, stated plainly: `internal/commands/base.go:488` and `:493`
short-circuit on `global_config.yaml` _before_ `checkInstalled` runs, so this
cost is paid on the **first** run — which is exactly the fresh-machine
experience the command exists for. Re-runs mostly skip it.

Fix per [ADR-0029](../../decisions/ADR-0029-installed-state-is-one-cached-listing.md):
an installed-package set built once per process, lazily, and dropped after **any
install or uninstall** — the ADR's contract is "any mutation", not "a successful
install". The uninstall half is the one that is easy to omit and the one that
fails silently: `MaybeInstall` (`base.go:500`) reads a stale "already present"
as done, records it via `AddToAlreadyInstalled`, and returns `nil`, so a package
uninstalled earlier in the same process is never reinstalled and the run still
reports success. Drop the set on the **attempted** mutation, not only a
successful one — a failed `brew`/`apt` install or uninstall can still have
changed package state, and the cost of being wrong is that silent skip against
~1.6 s for one extra listing. **Do not switch to `brew list <pkg>`** — see
`## Investigated and rejected`.

The mutation seam is four methods per `Command` implementation, and all eight
need the drop: `InstallPackage`, `InstallDesktopApp`, `UninstallPackage`,
`UninstallDesktopApp` on `MacOSCommand` (`internal/commands/macos.go:54,59,64,79`)
and on `DebianCommand` (`internal/commands/debian.go:57,66,70,119`). That
placement is what ADR-0029's Consequences require — invalidation lives at the
mutation boundary, never in each caller's memory.

Note the lifetime this implies. `NewCommand()` (`internal/commands/factory.go:27`)
returns a **fresh** `MacOSCommand`/`DebianCommand` per call, with ~58 non-test
call sites — roughly one per app `New()` — so a `sync.Once` field on the struct
would be per-instance and would save almost none of the ~54 s. The set has to be
process-wide, and that is also why an uninstall through one instance has to
invalidate the set every other instance reads.

Reachability today, so nobody writes a check for a sequence they cannot trigger:
no shipped command both uninstalls and probes in one process. `dg uninstall`
(`cmd/uninstall.go:96`) never installs afterwards, `dg install` never uninstalls,
`dg list` is read-only. `ForceInstall` — which 21 apps implement as
`baseapp.Reinstall(install, uninstall)`, i.e. uninstall then install — has no CLI
caller yet. The hole opens the moment one is wired up, and it is contract-level
(`internal/apps/contract.go:23`), so it gets closed here rather than left for
whoever adds the flag.

- Verify: `go test ./internal/commands/`; then time a real `dg install` on a
  machine with a populated Cellar and compare against the ~55 s baseline. Cover
  invalidation with mocked tests in both directions — install then probe must not
  report the package missing, and **uninstall then probe must not report it
  present**, the case this plan previously left out.

#### Step 7: One `global_config.yaml` parse per run — P1

**Done** — see commit range `08805b3..6ec4783` (Step 8's benchmark ran first, as
a prerequisite, per the ADR-0030 sequencing this document already calls for).
Shipped exactly as specified: one parse per run per ADR-0030, stat-keyed,
refresh-on-write (not invalidate-on-write), deep-copy-on-read. The worktree
manager's own local `{path, modTime, size}` cache — the pattern this step
generalizes — was retired in favor of this one.

`internal/config/fromFile.go:306-312` — `Load()` is `os.ReadFile` +
`yaml.Unmarshal` with no cache anywhere in the config package. There are **95
non-test `.Load()` call sites across 58 files**: `internal/apps/mise/mise.go:54,74,85`,
`internal/apps/claude/claude.go:86,113,208,277`,
`internal/apps/lazygit/lazygit.go:124,153,173`,
`internal/apps/alacritty/alacritty.go:54,92,107`,
`internal/apps/neovim/deps.go:89,123,166`,
`internal/tooling/worktree/worktree.go:220,248,802,1769`, and the install path at
`internal/commands/base.go:477` (load) and `:509`/`:521-522` (save), plus the
extra per-item round trips at `internal/tooling/languages/languages.go:201,205,215,236`
and `internal/tooling/databases/databases.go:165,169,179,200`.

The fix already exists in-tree and needs generalizing, not inventing: the
worktree manager memoizes one `Load()` keyed on `{path, modTime, size}`
(`internal/tooling/worktree/worktree.go:135-155`, hit/invalidate at `:796-817`).
That key is correct — it survives tests repointing `paths.Paths.Config.Root` —
and belongs in `internal/config`, not in one consumer.

Write contract, load-bearing — and it is a **refresh**, not an invalidation.
`internal/config/lock.go`'s `Update(fn)` is the only mutation path that takes the
sidecar lock; the ~95 bare `Load()`/`Save()` sites bypass it. So the stat key
alone is not enough: a bare `Save()` can land inside the filesystem's timestamp
granularity and leave every later reader in the run holding pre-write state.

Dropping the entry on `Save()` does not work either, and this is the case that
decides the design. `MaybeInstall` (`internal/commands/base.go:477-522`) does one
`Load()` per item and a `Save()` whenever the item was not already tracked —
`:509` for already-installed, `:521` for installed — and
`internal/tooling/languages/languages.go:201,205` and
`internal/tooling/databases/databases.go:165,169` add another `Load`+`Save` round
trip per item on top. With a drop-on-write cache, every item's `Save()` clears the
entry the next item's `Load()` would have hit, so a first-run `dg install` misses
on all ~55 items. That is the run this step exists to speed up. The cache would
hit only on a re-run, where the early returns skip `Save()` and there is least to
gain — the opposite of the "parses the YAML once" result claimed for it.

So `Save()` refreshes the entry: on a successful write it stores a deep copy of
the document it just wrote, keyed on the file's **post-write** stat. Sequencing is
what makes that safe — take the cache mutex, write, stat, store, release — so the
stored document and its key can never disagree, and a concurrent reader sees
either the pre-write entry or the post-write one, never a document keyed on the
wrong stat. If the write fails, leave the entry alone; if the post-write stat
fails, drop the entry rather than store one that is unkeyed or keyed on a guess,
and the next `Load()` re-reads. `Reset()` (`internal/config/fromFile.go:322-330`)
marshals and writes the file **directly**, not through `Save()`, and `Create()`
(`:332`) delegates to `Reset()` — both must go through the same refresh or the
cache outlives a reset. `Update(fn)` must still **bypass the cache on read**: it
exists to hand `fn` a config read fresh under the lock, and serving it a cached
document would defeat the lock; its trailing `Save()` refreshes the entry like any
other write.

Storing the in-memory document rather than re-reading the file is the point.
Re-parsing the bytes just written would be exactly faithful but pays the ~203 µs
parse on every `Save()`, which halves the parses instead of removing them. The
price of not re-parsing is a small, measured divergence from what a fresh read
would return, and it has to be written down here or the Verify step below gets
written wrong. Measured against the real `GlobalConfig` (Go 1.26.5, `yaml.v3`):

- A **nil slice** marshals to `[]` and unmarshals back as a non-nil empty slice,
  so an untouched `Installed.Themes` is `nil` in the saved document and
  `[]string{}` after a re-read.
- **`time.Time` loses its monotonic reading** through the round trip —
  `FailedInstallation.FailedAt` and `RecentRepo.LastUsed` compare `Equal()` true
  but `==` and `reflect.DeepEqual` false.

Neither is observable through `GlobalConfig`'s accessors — `IsInstalledByDevgeta`,
`IsAlreadyInstalled`, the list getters and the time comparisons all behave
identically on both forms — so the refresh is correct for every caller. It is
`reflect.DeepEqual` against a fresh read that is the wrong assertion, not the
cache.

Ownership contract, equally load-bearing: the cache must hand each caller its own
copy, never the cached `*GlobalConfig`. `Load()` fills a caller-owned struct
today, so callers already assume they own the result — and several mutate it **in
place**. `RemoveFromInstalled` (`internal/config/fromFile.go:425`) filters through
the shared backing array (`result := (*slice)[:0]`), `AddToFailed` (`:447`) writes
into an existing element, and `Shortcuts` is a map, which a shallow struct copy
shares outright. Reproduced against a cache handing out shallow copies: with
`[git tmux neovim fzf bat]` cached, a single `RemoveFromInstalled("tmux", …)` left
the **cache** holding `[git neovim fzf bat bat]` — tmux lost, bat duplicated — and
the next `Load()` served that corrupted list although the file on disk was
untouched; the element write and the map leaked across handles the same way. With
24 non-test `Uninstall()` sites calling `RemoveFromInstalled`, corruption lands in
the cache at the moment of mutation, before the mutating caller reaches a `Save()`
at all — and under the refresh contract above a later `Save()` does not repair it,
it writes the corrupted document to disk. Correctness would rest on an unwritten
"every mutation is followed by a successful `Save()`" invariant that nothing
enforces and a failed `Save()` breaks.

So `Load()` returns an independent **deep copy** of the cached document. The two
cheaper-looking options do not work:

- Caching raw bytes and unmarshalling per call is safe but buys almost nothing.
  Benchmarked against the real 4,313-byte `~/.config/devgeta/global_config.yaml`
  (best-of-5, 200 iterations each) the read is ~17 µs and the parse ~203 µs — so
  bytes-only caching removes ~8% of `Load()`'s cost and leaves the 92% this step
  exists to remove. (Indicative only; Step 8 owns the real figure.)
- Re-marshalling the cached struct to clone it pays that same parse cost back.

The copy must be an explicit clone in `internal/config` beside the struct,
guarded by a test that fails when a field is added: populate every field, clone,
then mutate every reachable slice element, map entry and pointee in the source by
reflection and assert the clone is unchanged. A clone maintained by convention
rots the first time someone adds a field, and CLAUDE.md §4 asks for that class of
mistake to be made structurally impossible rather than documented.

One thing the copy is **not** needed for, so no one writes a test claiming it:
appending to a decoded slice does not alias today, because `yaml.v3` allocates
every sequence with `cap == len` (`decode.go:735`), so `AddToInstalled`
reallocates. That is a property of the parser, not of our code — the deep copy
does not rely on it either way.

- Verify: `go test ./internal/config/` plus its importers. Four behaviors, and the
  first two are the ones this step turns on:
  - A `Save()` followed by a `Load()` in the same process returns the **new**
    value, and does so **without re-reading the file** — assert on a read counter
    or a `Load()` that still succeeds after the file is made unreadable, not on
    the value alone, which a drop-on-write cache would also pass.
  - A `Load()` → `Save()` → `Load()` → `Save()` loop shaped like `MaybeInstall`
    over N items parses the document once, not N times.
  - Mutating one `Load()` result (`RemoveFromInstalled`, `AddToFailed`, a
    `Shortcuts` write) leaves a subsequent `Load()` unchanged.
  - An out-of-band write to the file — different size or mtime — is picked up on
    the next `Load()`.

  Compare through `GlobalConfig`'s accessors, **not** `reflect.DeepEqual` against
  a fresh read: per the measurements above, a refreshed entry legitimately differs
  from a re-parsed one in nil-vs-empty slices and in `time.Time` monotonic
  readings, so a `DeepEqual` assertion fails on a correct implementation.

#### Step 8: Measure the per-`Load()` cost and amend ADR-0030

**Done** — see commit range `2eb9427..08805b3`. `Load()` benchmarked at ~224
µs/call; ×95 call sites ≈ 21 ms total across a run. The real number turned out
negligible, and ADR-0030 states that plainly rather than inflating it — the
correctness argument stands on its own, as this step anticipated.

[ADR-0030](../../decisions/ADR-0030-global-config-loads-once-per-run.md) rests on
the correctness argument — one consistent view of config within a run. Its
performance case is **unmeasured**: the 95 call sites were counted, the cost per
call never was. Benchmark `Load()` against a representative
`global_config.yaml` (warm, best-of-N — the audit's own startup measurements
show a single cold call can read 20x high on page-fault noise), multiply by 95,
and write the number into the ADR. If the number turns out to be negligible, say
so in the ADR; the correctness argument stands on its own either way.

- Verify: `go test -bench . ./internal/config/`; ADR-0030 carries a real figure.

#### Step 9: Atomic, skippable config extract — P1

**Done** — Part 1 (the `pkg/buildinfo` prerequisite) shipped in commit range
`6ec4783..91cf996`; the atomic extract itself in `91cf996..6395c40`. Config
extract is now a symlink-pointer swap to a `configs-<stamp>` directory —
genuinely atomic for the steady-state case, with a recoverable (not atomic,
but self-repairing) one-time migration for every pre-existing real-directory
install, exactly as specified below. Two claims in this step's own text below
needed correcting after live implementation and probing, not just marking
done — both are called out in place.

`cmd/configure.go:78` calls `refreshEmbeddedConfigs()` (`cmd/configure.go:29-32`
→ `devgeta.New().Install()`) **before** it resolves which app was asked for.
`internal/apps/devgeta/devgeta.go:70-87` then does `os.RemoveAll(configsDir)`
followed by a full re-walk of the embedded FS, `fs.ReadFile` + `os.WriteFile` per
file (`embedded.go:54,60`). Configuring one app rewrites ~1.4 MB across 152 files
every time; `ForceConfigure` (`internal/apps/devgeta/devgeta.go:155`) does it a
second time in the same run.

Worse than slow: it violates CLAUDE.md §4 ("state must be atomic: either complete
or fully roll back"). Between the `RemoveAll` and the end of the re-extract the
config tree is absent or partial, and every app's `Configure` reads its templates
from that tree. Ctrl-C at the wrong moment leaves the user with no configs and no
error.

Fix, in two parts.

**Part 1 — stamp and skip.** Record the binary's version+commit with the extracted
tree and skip the extract when it matches. This removes the rewrite entirely in
the common case, including `ForceConfigure`'s second pass, so part 2 runs only on
a version change. Part 2 supplies the stamp for free — the extracted directory is
named for it — so the check is a `readlink`, not a stamp file to keep in sync with
the tree beside it.

**Where the version+commit comes from is a prerequisite, not a detail.** They do
not exist anywhere `internal/apps/devgeta` can read them today. `Version`,
`Commit` and `BuildDate` are package-level vars in **package `cmd`**
(`cmd/version.go:14-21`), and `cmd` imports `internal/apps/devgeta` — so
importing `cmd` back is an import cycle and will not compile. Both build paths
inject into that package:

- `Makefile:9-11` — `-X 'github.com/cjairm/devgeta/cmd.Version=…'` and siblings.
- `.github/workflows/release.yml:67-70` — the same three `-X` flags.

So the step must first move the metadata to a **leaf package with no devgeta
imports** — `pkg/buildinfo` alongside the other `pkg/` leaves is the obvious
home — repoint the `-X` flags in **both** the Makefile and the release workflow
at it, and leave `cmd/version.go` reading from there so `dg version` is
unchanged. Getting this wrong is not subtle (the build fails), but doing it
late means the stamp design is written against a value it cannot obtain. Note
also that `cmd/version.go:29-40` already falls back to
`runtime/debug.BuildInfo` when the ldflags were not applied — a plain
`go build` yields `Version = "dev"`, so the stamp must stay stable across two
consecutive `dev` builds of different code, which is exactly the staleness the
`--force` path in the trade-off below exists to repair.

**The skip must not be put inside `Install()`, or `--force` skips too.** Both
paths that refresh the tree call `Install()` unconditionally today:

- `cmd/configure.go:29-33` defines
  `var refreshEmbeddedConfigs = func() error { return devgeta.New().Install() }`
  and `:72-80` calls it at the top of `runConfigure` **before** `configureForce`
  is consulted at all.
- `internal/apps/devgeta/devgeta.go:158-163` — `ForceConfigure()` opens by
  calling `dg.Install()`.

So a stamp check placed in `Install()` makes `dg configure <app> --force` a
no-op for the extracted tree, and the trade-off this step accepts ("a hand-edited
extracted tree is not repaired until the version changes, and `--force` is the
repair path") becomes false: there would be **no** repair path short of a version
bump. Instead, put the skip in a new caller-selectable seam — e.g. `Install()`
keeps its current unconditional behavior and a `SoftInstall`-style
`InstallIfStale()` carries the stamp check — then route `refreshEmbeddedConfigs`
through the stale-checking variant and leave `ForceConfigure()`'s call on the
unconditional one. `Devgeta` already has exactly this `Install` / `SoftInstall`
shape at `:70` and `:89`, so the seam is a pattern the type already uses, not a
new concept. A test must assert the two directions separately: a `--force`
configure re-extracts even when the stamp matches, and a plain configure does
not.

**Part 2 — a commit protocol that actually works.** `os.Rename(configs.new,
configs)` **cannot** replace the existing tree: `rename(2)` refuses to overwrite a
directory that is not empty (POSIX `ENOTEMPTY`), and on macOS/APFS it refuses even
an empty one. Measured on this machine (macOS, APFS, Go 1.26.5) with a throwaway
program that renames one populated directory over another:

```
rename over NON-EMPTY dir: err=… configs.new → configs: file exists
rename over EMPTY dir:     err=… configs.new → configs: file exists
```

`configs` is populated in every refresh case, so the single-rename commit fails on
both supported platforms and every refresh would error out instead of swapping.

**Take the symlink pointer.** Extract to `configs-<version+commit>` — the stamp
part 1 needs, carried by the directory name. Commit by creating a temp
symlink beside it and renaming that over the existing `configs` symlink. Measured
on this machine (macOS, APFS, Go 1.26.5), same throwaway program:

```
rename tmp symlink over existing symlink: err=<nil>; read through pointer flips v1 → v2
os.Stat(pointer).IsDir():   true      os.Lstat(pointer).IsDir(): false
os.ReadDir(pointer):        1 entry, err=<nil>
filepath.WalkDir(pointer):  visits 1  (a real-directory root visits 2)
rename symlink over a REAL directory:    file exists
os.RemoveAll(pointer):      err=<nil>;  target directory still present
```

There is no window in which the tree is absent or partial: a reader resolves the
pointer to the old tree or the new one, never to a missing or partial one. That
is what CLAUDE.md §4 asks for, and of the two options that deliver it this is
the portable one — no platform-specific syscall, no recovery pass.

**Correction (Task 13, cycle close-out): "no window" is not literally true for
a reader racing the swap itself.** The claim above was measured single-threaded
— swap, then read. A reader actually racing the pointer's `rename(2)` sees a
transient `EINVAL` on roughly 1.3% of reads on macOS/APFS — never `ENOENT`,
never wrong or partial data, and self-healing on the next read. It is reachable
only across two concurrent `dg configure` processes, an exposure this document
already accepts as unmitigated elsewhere (see the stale-target discussion
below). Not mitigated here either: a retry loop in every reader, for a rare,
self-healing transient at this rate, is disproportionate to the problem.

The measurements also price the costs the audit guessed at. Three of the five are
real work this step owns:

- **Existing readers are unaffected.** `os.Stat` and `os.ReadDir` follow the link,
  so `files.DirAlreadyExist` and `files.IsDirEmpty` — the only checks
  `internal/apps/devgeta/devgeta.go:75,93,108` make — behave exactly as they do
  today, and every template read is an `os.ReadFile` through a path that happens
  to contain a symlinked component. The worry that "every reader must tolerate a
  symlink" was overstated: the sole disk walker in non-test code is
  `internal/tooling/worktree/scan.go:131`, rooted at a repo, and `embedded.go:27`
  walks the embedded FS, not the extracted tree. Nothing outside devgeta reads the
  tree either.
- **Forward constraint, not a current break:** `filepath.Walk`/`WalkDir` lstat the
  root, so a walker rooted at the pointer would visit the link and descend into
  nothing, silently. No such walker exists today; if one is added it must walk the
  resolved target. Worth a comment at the pointer's definition.
- **`Uninstall()` must remove the target, not just the pointer — and must
  validate the target first.** Measured: `os.RemoveAll` on a symlink removes the
  link and leaves the directory. As written
  (`internal/apps/devgeta/devgeta.go:107-114`) uninstall would orphan ~1.4 MB
  under `paths.Paths.App.Root`, and the "remove the app dir if it is empty"
  branch at `:118-121` would then never fire. So uninstall has to resolve the
  pointer, remove the target, then remove the link.

  Doing that naively converts a harmless link-removal into an
  **arbitrary-directory delete driven by a symlink in a user-writable
  directory**, which CLAUDE.md §4 ("user input must always be validated before
  use, especially paths") forbids. `paths.Paths.App.Root` is a normal directory
  under the user's home; anything that can write there can point `configs` at
  `~/Documents` and get devgeta to delete it. Measured with a throwaway program
  where `root/configs` was a symlink to a sibling `victim/`:

  ```
  after RemoveAll(link):             victim/precious.txt present=true
  after RemoveAll(resolved target):  victim/precious.txt present=false
  ```

  The resolve-then-remove step must therefore refuse anything that is not a
  tree devgeta itself created. Concretely, before removing: `os.Lstat` the
  pointer and require `ModeSymlink`; `filepath.EvalSymlinks` it; require the
  result to be `filepath.Clean`ed, to have `paths.Paths.App.Root` (itself
  `EvalSymlinks`'d, so a moved app root does not fail the check) as its
  **parent**, and to have a base name matching the `configs-<stamp>` pattern.
  Anything else: remove the link only, leave the target alone, and log. The same
  validation applies to the "collect the previous target" removal below, which
  deletes by the same rule. A test must assert that a pointer aimed outside the
  app root leaves the target intact.

- **One-time migration — and it must be atomic too.** Measured: a symlink cannot
  be renamed over an existing real directory. Every current install has `configs`
  as a real directory, so the migration has to get from "real directory" to
  "pointer" somehow. An earlier draft of this step did that by removing the old
  directory and then creating the pointer, and conceded "that single transition
  is not atomic; it is the last time the tree is briefly absent, and it happens
  once per machine."

  That concession does not hold. CLAUDE.md §4 does not carve out "only once per
  machine", and once per machine is once for **every** user on the version that
  ships this — it is the single most-executed path the change has, not an edge
  case. §4 also rules out shipping a known-broken transition on the grounds that
  it is brief. And the failure is not cosmetic: an interrupt in that window
  leaves the user with no config tree and no pointer, i.e. exactly the state
  Part 2 exists to make impossible, on the one run where the user has no prior
  devgeta version to fall back to.

  The same primitives already measured above are enough to do it properly,
  because renaming a **real directory** over nothing and renaming a **symlink**
  over a symlink are both atomic:

  1. Extract to `configs-<stamp>` (leaves the live real `configs` untouched).
  2. `os.Rename(configs, configs.legacy)` — atomic; a real directory renamed to
     a name that does not exist.
  3. `os.Symlink(configs-<stamp>, .configs.tmp)` then
     `os.Rename(.configs.tmp, configs)` — atomic, and this is the same
     symlink-over-nothing case, not the rejected symlink-over-real-directory
     one.
  4. `os.RemoveAll(configs.legacy)`.

  Steps 2 and 3 are separate syscalls, so there is still an instant with no
  `configs` — but unlike the removal-based version that instant is now
  **recoverable**, because `configs.legacy` still holds the complete old tree.
  Devgeta therefore runs a repair pass before any read of the tree, handling the
  three interruption points:

  - `configs` missing **and** `configs.legacy` present → rename
    `configs.legacy` back to `configs` and restart the migration.
  - `configs` present as a symlink **and** `configs.legacy` present → step 3
    completed; finish by removing `configs.legacy`.
  - `configs` present as a real directory **and** `configs.legacy` present →
    remove the now-redundant `configs.legacy` and re-run the migration.
    **Correction (Task 13, cycle close-out):** the cause originally stated here
    — "a previous attempt died between undoing step 2 and restarting" — is
    impossible: undoing step 2 is a single atomic rename, so there is no
    instant where both paths coexist from that cause. The state is still
    reachable, just not that way — via outside interference, such as an older
    devgeta binary that predates this protocol, or a user hand-restoring
    `configs` from a backup. The prescribed handling above (treat `configs` as
    authoritative, discard the leftover) is correct either way and did not
    need to change.

  That is a recovery contract, which the two-rename option below was rejected
  for — the difference is that here it covers a one-time transition that has no
  atomic alternative, rather than every single refresh, which does. The repair
  pass must be tested by starting from each of the three states directly, not by
  hoping an interruption test lands in the window.

- **Stale targets are collected on the next extract, not right after the swap —
  and the reason is not the one the audit first gave.** The original
  justification here was "removing `configs-<old>` at the start of the _next_
  extract means no reader from the run that swapped can still be pointed at it."
  That is unsound: it only holds for one generation. A reader that started
  before swap N is not made safe by delaying the delete until swap N+1; it is
  made _less_ safe, because the delete it is racing simply moves further away.
  Deferring cleanup does not bound reader lifetime at all.

  What the measurements actually show is that the danger being defended against
  does not exist:

  - **Devgeta readers are not pinned to a generation.** Every read is an
    `os.ReadFile` on a freshly-built path string (`pkg/paths/paths.go:203`
    builds `paths.Paths.*` at package init and never `EvalSymlinks`es them), so
    the `configs` component is re-resolved on every call. Measured: the same
    path string read `"A"` before a pointer swap and `"B"` after it. There is no
    long-lived handle on the old tree to protect.
  - **A reader that _does_ hold an open fd survives the delete anyway.** POSIX
    unlink semantics keep the inode alive for the open descriptor. Measured:
    `read through open fd after RemoveAll of its dir: "v2-contents" err=<nil>`.

  So the residual risk is neither of those. It is a **straddled read**: a
  process that reads file X, gets descheduled, the pointer swaps, and it then
  reads file Y from the new tree — ending up with a mix of two generations. No
  retention policy fixes that, because the swap, not the delete, is what causes
  it. It is bounded instead by the fact that devgeta's own reads happen inside a
  single `Configure` call and nothing outside devgeta reads the tree; concurrent
  `dg configure` runs on one machine are the only exposure, and they already
  race on the deployed config files themselves.

  The policy that follows from that: **remove the previous target immediately
  after the swap, in the same run**, subject to the same validation as
  `Uninstall()` above. Keeping exactly one generation costs ~1.4 MB and buys
  nothing. Deletion failure is not fatal — log it and let the next run's
  sweep-anything-matching-`configs-*`-that-is-not-the-current-target pass
  collect the leftovers, which is also what recovers debris from an interrupted
  extract. That sweep is the reason a stamp-named directory scheme is used at
  all; it is not a fallback for reader lifetime.

The other two options are recorded because the reasons matter, not as open
choices:

- **Two-rename swap** — `configs` → `configs.old`, `configs.new` → `configs`, then
  `os.RemoveAll(configs.old)`. Both renames are metadata-only, so the absent window
  shrinks from a 152-file rewrite to microseconds, but it is **not zero and not
  atomic**: a kill between the two renames leaves no `configs` at all, and the tree
  only comes back when some later devgeta run notices and repairs it. Nothing
  outside devgeta reads the tree, so "later" is survivable — but it is a recovery
  contract, not atomicity, and it would have to be described that way and tested by
  its own path (start from `configs` missing + `configs.old` present and assert the
  repair) rather than by the interruption check below. Rejected because the symlink
  option removes the failure mode instead of documenting a repair for it, which is
  what CLAUDE.md §4 asks for when both are available.
- **Directory swap syscall** — `renameat2(RENAME_EXCHANGE)` on Linux and
  `renamex_np(RENAME_SWAP)` on macOS do exchange two directories atomically, and
  `golang.org/x/sys` is already a direct dependency (`go.mod:15`). Also atomic, but
  it is two platform-specific files plus a fallback for filesystems that reject the
  flag, to reach the same place a portable `os.Symlink` + `os.Rename` reaches.
  Rejected on cost; revisit only if the pointer is rejected.

The result of the chosen protocol **is** a single atomic commit, and the plan says
so only because the measurement above shows it. The one-time migration reaches
the same guarantee through a recoverable sequence rather than a single rename,
for the reason given in its bullet — there is no atomic real-directory-to-symlink
transition to use instead.

- Verify: `go test ./internal/apps/devgeta/ ./cmd/`. Cover the pointer swap with a
  test that flips it under a reader and asserts the reader saw one complete tree or
  the other; an `Uninstall()` test asserting the target directory is gone and not
  merely the link; a second `Uninstall()` test asserting a pointer aimed **outside**
  `paths.Paths.App.Root` removes only the link and leaves the target intact; and
  three migration tests, one per interruption state in the repair table above,
  each asserting the tree is complete afterwards.
  By hand: interrupt a `dg configure` mid-extract — because the extract writes to
  `configs-<new stamp>`, the live pointer never moves, and both the tree and the
  pointer must be intact afterwards with the partial `configs-<new stamp>` the only
  debris; the next run must overwrite it and succeed.

#### Step 10: Config deploy permissions — P1 / security

**Done** — see commit range `6395c40..662e8ee`. `pkg/files.CopyFile`/`CopyDir`
now use `FilePermission`/`DirPermission` (0644/0755) instead of `AllPermissions`
(0777); the now-dead `AllPermissions` constant was removed.

`pkg/files/files.go:64` — `CopyFile` writes destinations with `AllPermissions`
(0o777, defined at `:22`). `CopyDir` does the same for directories at `:83` and
`:96`. This is the deploy path for essentially every app config:
`internal/apps/alacritty/alacritty.go:74`,
`internal/apps/claude/claude.go:149,160,170`,
`internal/apps/baseapp/configure.go:40`, `internal/apps/aerospace/aerospace.go:76`,
`internal/apps/i3/i3.go:58`, `internal/apps/neovim/neovim.go:180`,
`internal/apps/fastfetch/fastfetch.go:53`, `internal/apps/git/git.go:149,168`,
`internal/apps/opencode/opencode.go:114,163`.

Under the common umask 022 the result is 0755 — the execute bit on static config
files, already wrong. Under umask 002 or 000 (not rare in containers and on
shared boxes) these land group- or world-writable. The security-specific point:
this deploys `~/.config/claude/lib/*` (`internal/apps/claude/claude.go:160,170`,
`internal/apps/baseapp/configure.go:40`), and those files are **sourced by the
guard hook scripts on every agent tool call**. A group- or world-writable `lib/`
is arbitrary code execution in the user's agent session. Same exposure for the
git config at `internal/apps/git/git.go:149,168`.

Fix: use `FilePermission` (0o644, `pkg/files/files.go:18`) and `DirPermission`
(0o755, `:20`) in `CopyFile`/`CopyDir` — both already exist and have no callers
here — and chmod 0755 only where execution is genuinely needed.
`internal/apps/claude/claude.go:149-157` already does exactly that for the hook
scripts and is the model to copy. `AllPermissions` has no legitimate caller on
this path.

- Verify: `go test ./pkg/files/` and its importers (wide — run the suite); check
  deployed file modes after `dg configure claude --force`.

#### Step 11: HTTP hardening — P1

**Done** — see commit range `662e8ee..21ee42f`. `pkg/github` gained a timeout,
response draining on the non-200 path, and a size cap as specified.
`pkg/downloader` replaced its whole-request `Timeout` with a shared transport
plus a body-inactivity bound, and the retry loop was taught a real sentinel so
a stall is classified retryable instead of failing permanently on the first
attempt, closing the integration gap this step calls out below.

**`pkg/github/release.go:15`** uses bare `http.Get`, i.e. `http.DefaultClient`,
which has **no timeout**; `:25` does `io.ReadAll(resp.Body)` with no size cap. A
hung `api.github.com` connection hangs `dg install` forever with no output.
Callers: `internal/apps/lazygit/lazygit.go:50`,
`internal/apps/lazydocker/lazydocker.go:50`, `internal/apps/rtk/rtk.go:57`.
Secondary: the non-200 path returns without draining `resp.Body`, so the
connection cannot return to the idle pool — a wasted TCP+TLS handshake per
rate-limit response. Fix: a package-level `http.Client{Timeout: 30 * time.Second}`
(or `NewRequestWithContext` so callers can cancel), `io.Copy(io.Discard, resp.Body)`
on the non-200 path, and `json.NewDecoder(io.LimitReader(resp.Body, N))`.

**`pkg/downloader/retry.go:126-138`** constructs `client := &http.Client{Timeout: 30 * time.Second}`
**inside** `downloadFile`, i.e. once per retry attempt. The defect here is the
**timeout**, not connection reuse — an earlier draft of this step claimed the
per-attempt client also defeats keep-alive, and that claim is false. The
constructed client leaves `Transport` nil, and `(*http.Client).transport()`
(`net/http/client.go:203-208`) returns the process-wide `http.DefaultTransport`
whenever it is: no per-client transport is allocated, and every attempt shares
one idle-connection pool. Measured against an `httptest` server with
`httptrace.GotConn`, three separate `&http.Client{Timeout: 30 * time.Second}`
values reported `Reused: false / true / true` on one identical local address.
Allocating a package-level client is therefore not a performance fix; if it is
done at all it is for the timeout configuration below, and any future
keep-alive claim about this file has to be re-measured before it goes in the
plan.

The real defect: `http.Client.Timeout` covers the **entire** request including
the body read — and this function streams into `files.WriteFileAtomicFrom`, so a
Nerd Font archive (tens of MB) on a slow link is aborted mid-download at 30 s and
retried, failing identically every attempt. Fix: replace the whole-request
`Timeout` with a client whose `Transport` carries
`ResponseHeaderTimeout`/`TLSHandshakeTimeout`, plus the body-inactivity bound
below — bound the stall, not the transfer. Because that transport is no longer
`http.DefaultTransport`, it does need to be built once at package level and
shared, or each attempt really would get its own pool.

`ResponseHeaderTimeout` alone is **not enough**, and the plan must not pretend
otherwise: it stops applying the moment the response headers arrive, so a server
that sends `200 OK` and then stops sending body bytes leaves the transfer with no
deadline at all. Measured against an `httptest` server that writes headers and
then blocks — with `ResponseHeaderTimeout: 1s` and no client `Timeout`, the
headers arrived in 21 ms and `io.Copy` on the body was **still blocked after
10.0 s**. That trades "aborts a large download at 30 s" for "hangs forever",
which is the same failure this step opens with, moved from `pkg/github` into the
downloader. A context deadline sized to the download fails the other way: the
size is not known before the transfer starts, so the deadline is either a guess
that kills slow-but-progressing downloads or large enough to bound nothing.

So the body stream needs its own **inactivity** bound, which the stdlib does not
provide for HTTP/1 response bodies. Wrap `resp.Body` in a reader that resets a
timer on every `Read` that returns bytes and cancels the request context when no
bytes arrive within the idle window, then hand that wrapped reader to
`files.WriteFileAtomicFrom`. Measured with a 2 s window: `io.Copy` on a stalled
body returned after 2.2 s, while a body that keeps producing bytes is never
touched however long the transfer runs.

Considered and rejected — per-`Read` `net.Conn.SetReadDeadline` via a custom
`DialContext`. It bounds the stall just as well (returned after 2.0 s with an
`i/o timeout`) but it breaks the connection reuse that works today: measured with
`httptrace.GotConn`, a request issued after a 3 s idle gap reported
`Reused: false` on the deadline-wrapped transport where a plain transport
reported `Reused: true`, because the expired deadline poisons the pooled
connection. The two options are not interchangeable — only the first keeps both
properties.

One integration detail, or the retry loop swallows the fix: a cancelled context
surfaces as `context canceled`, and `IsRetryableError` (`pkg/downloader/retry.go`)
matches only the substrings `timeout`, `temporary failure`, `connection refused`,
`no such host` and `(retryable)`. A stalled download would therefore be classed
non-retryable and fail on the first attempt. The stall must be reported as an
error `IsRetryableError` recognises — a sentinel it is taught directly, not a
message shaped to hit a substring — with a test asserting it.

- Verify: `go test ./pkg/github/ ./pkg/downloader/`; a mocked slow-but-progressing
  body must not abort a large transfer, a mocked body that stops sending must fail
  within the idle window instead of hanging, and the error it produces must
  satisfy `IsRetryableError`.

#### Step 12: `govulncheck` and dependency CVEs — P1

**Done.** `govulncheck` was installed
(`env -u GOROOT go install golang.org/x/vuln/cmd/govulncheck@latest`, v1.7.0)
and run against the whole tree exactly as this step's Verify line specifies:
`govulncheck ./...`.

**First run — not clean.** It found 4 vulnerabilities reachable from this
module's code, plus 2 more affecting imported packages and 2 more affecting
required modules that the scan determined our code never calls:

```
Vulnerability #1: GO-2026-6218 (net/url resolvePath quadratic complexity)
    Found in: net/url@go1.26.5 — Fixed in: net/url@go1.26.6
    Reachable via: pkg/downloader/retry.go:245 -> http.Client.Do -> url.URL.Parse

Vulnerability #2: GO-2026-6090 (crypto/tls post-handshake message limit)
    Found in: crypto/tls@go1.26.5 — Fixed in: crypto/tls@go1.26.6
    Reachable via: pkg/downloader/retry.go (Do, stallDetectingReader.Read),
    internal/tooling/reviewjournal/journal.go:174 (Render -> tls.Conn.Write)

Vulnerability #3: GO-2026-5972 (encoding/asn1 missing recursion depth guard)
    Found in: encoding/asn1@go1.26.5 — Fixed in: encoding/asn1@go1.26.6
    Reachable via: pkg/downloader/retry.go:207 -> ... -> asn1.Unmarshal

Vulnerability #4: GO-2026-5026 (golang.org/x/net/idna Punycode label rejection,
surfaced through net/http)
    Found in: net/http@go1.26.5 — Fixed in: net/http@go1.26.6
    Reachable via: pkg/downloader/retry.go:245 and pkg/github/release.go:39
    (both via http.Client)

Your code is affected by 4 vulnerabilities from the Go standard library.
This scan also found 2 vulnerabilities in packages you import and 2
vulnerabilities in modules you require, but your code doesn't appear to call
these vulnerabilities.
```

All 4 reachable findings — and both of the non-reachable "package" findings
(GO-2026-6089, GO-2026-5942) and both non-reachable "module" findings
(GO-2026-6091, GO-2026-6088) — are Go **standard-library/toolchain** bugs, not
third-party dependency CVEs. Every one is already fixed in go1.26.6; this
environment was building with go1.26.5 (`toolchain go1.26.3` in `go.mod`, but a
newer local Go binary was on `PATH` ahead of it, so the effective build
toolchain was 1.26.5 — still older than the 1.26.6 fix).

**Remediation decision: upgrade the toolchain, not a dependency.** None of the
22 outdated modules from `go list -m -u all` are implicated — every finding
traces to the Go standard library itself, so the correct fix is a toolchain
bump, not a `go.mod` `require` change. Bumped `go.mod`'s `toolchain` line from
`go1.26.3` to `go1.26.6` (the exact version all 8 findings are fixed in). This
is a patch-level toolchain bump: `go build ./...` picked it up immediately
(Go's `GOTOOLCHAIN=auto` downloaded go1.26.6 on first build), the full
`go test ./...` suite stayed green, and `make lint` stayed clean — low risk,
mechanical, no dependency version changed.

**Re-run after the bump — clean:**

```
$ go version
go version go1.26.6 darwin/amd64
$ govulncheck ./...
No vulnerabilities found.
```

The 22 outdated modules `go list -m -u all` reports (oldest:
`github.com/chzyer/readline` at a 2018 pseudo-version, transitive via
`promptui`) remain outdated but **not** flagged by `govulncheck` — outdated and
vulnerable are different things, and this step's job was verified CVE status,
not general dependency freshness. No action taken on them.

Observation for the maintainer (not acted on in this task): nothing currently
re-runs `govulncheck` automatically, so a newly-disclosed CVE in the toolchain
or a dependency would go unnoticed until someone runs it by hand again. Whether
to wire it into CI or a `make` target is a decision for the maintainer, not
this step.

- Verify: `govulncheck ./...` ran; output above. Clean after the go1.26.6
  toolchain bump.

---

## 6. Verification Plan

### Automated Verification

This cycle touches `internal/commands`, `pkg/files`, `internal/config` and
`configs/` — four of the widest-blast-radius areas in the tree (CLAUDE.md §6
names the first three explicitly, and `configs/` is read by the root package's
embedded-config tests). **Run the full suite and say that you ran it.** Targeted
runs are fine while iterating on a single step:

```bash
# While iterating, per step
go test ./internal/commands/
go test ./internal/config/
go test ./pkg/files/ ./pkg/github/ ./pkg/downloader/
go test ./internal/apps/devgeta/ ./internal/apps/claude/ ./internal/apps/opencode/
go test .                                  # root: embedded configs + guard shells (~4.8 min)

# Before finishing the cycle
go build ./...
make lint
go test ./...                              # ~5.5 min, mandatory here
govulncheck ./...
```

### Manual Verification

1. A command that backgrounds a child returns immediately instead of after
   5.204s — reproduce the audit's `sleep 5 & echo started; exit 0` shape.
2. A command that succeeds under a deadline reports success, not a timeout.
3. Kill a timed-out command; confirm zero surviving grandchildren
   (the audit measured 1).
4. Point an install script strategy at a 404 URL; the install must **fail**, not
   report success.
5. `install.sh` against a tampered binary must refuse it; the release page must
   carry `checksums.txt`.
6. With both agents deployed (`dg configure claude --force`,
   `dg configure opencode --force`), OpenCode refuses a project-relative read of
   `../../.claude/settings.json` — the case the 26 `~/`-anchored rules silently
   missed. Then confirm the added command denies fire on `gh api` and on a
   `python3 -c` one-liner. Do **not** check whether `cat` / `sed -i` / `tee` at a
   denied path is refused: it is not, by design (Step 5; ADR-0014 §1 and §5), so
   phrasing that as a pass/fail would test for a boundary this cycle explicitly
   does not claim.
7. Time `dg install` on a machine with a populated Cellar; compare against the
   ~55.6 s detection baseline.
8. Interrupt `dg configure` mid-extract. The live `configs` pointer must still
   resolve to a complete tree — the extract writes to `configs-<new stamp>` and
   the pointer only moves in the final rename — and the next run must overwrite
   the partial `configs-<new stamp>` and succeed. Then `dg uninstall` and confirm
   no `configs-*` directory is left behind under the app root.
9. `ls -l` a deployed config: 0644, not 0755 or 0777.

### Regression Check

- `dg install`, `dg configure`, `dg list`, `dg ws`, `dg version` all still work.
- Output from long-running installs is not truncated — that is what e7104b4's
  drain reorder was protecting, and Step 1 must preserve it while removing the
  unbounded wait.
- The two agents stay symmetric: `go test ./internal/apps/opencode/` covers it.
- No test starts executing real commands: `testutil.VerifyNoRealCommands` in
  every new test, checking the same base the code under test uses.

---

## 7. Risks & Trade-offs

| Risk                                                                                  | Likelihood | Mitigation                                                                                                                                                                                                     |
| ------------------------------------------------------------------------------------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Step 1 reintroduces the output truncation e7104b4 fixed                               | Med        | `WaitDelay` gives drainers a grace period after the child exits rather than cutting them off; add a test that a chatty command's full output is captured                                                       |
| `Setpgid` changes signal delivery for interactive children (fzf, editors, agent CLIs) | Med        | A new process group does not receive the terminal's SIGINT. Verify Ctrl-C still interrupts an interactive subprocess before merging, or scope `Setpgid` to non-interactive invocations                         |
| Cached package listing goes stale mid-run                                             | Low        | ADR-0029 invalidates on any attempted install **or uninstall**, at the eight mutation methods themselves (Step 6); the set never outlives the process, so a second `dg` run re-lists                           |
| Config cache returns a stale document after an out-of-band edit                       | Low        | The `{path, modTime, size}` key already handles this in the worktree manager. Devgeta's own writes cannot go stale because `Save()` refreshes the entry with what it wrote rather than dropping it (Step 7)    |
| Tightening the agent permission model breaks a legitimate workflow                    | Med        | The denies ship to strangers' repos, so a false positive is expensive. Deploy to the maintainer's own machine first and use it for real work before release                                                    |
| Checksum verification breaks `install.sh` for existing users mid-rollout              | Low        | The checksum file must exist before `install.sh` requires it — ship the workflow change in one release, the verification in the next                                                                           |
| The full suite is the only gate that catches cross-package interference               | Certain    | Accepted; that is why §6 mandates the full run here rather than targeted tests                                                                                                                                 |
| A future walker rooted at the `configs` symlink silently visits nothing               | Low        | `filepath.Walk`/`WalkDir` lstat the root — measured in Step 9. No such walker exists today; the constraint gets a comment at the pointer's definition and a test that the pointer resolves to a populated tree |

### Trade-offs made

- **`WaitDelay` grace period length.** Too short truncates output on a slow
  drainer; too long reintroduces a bounded version of the same hang. Pick a value
  and justify it in the code comment rather than leaving it unexplained.
- **Process-group kill vs. graceful stop.** SIGKILL to the group is simple and
  certain; SIGTERM-then-SIGKILL is kinder to children that clean up after
  themselves. The audit suggests the second where a graceful stop matters. Choose
  once, in the executor, not per call site.
- **Skip-on-version-stamp for the config extract** trades a small staleness risk
  (a hand-edited extracted tree is not repaired until the version changes)
  against removing 152 file writes per `dg configure`. `--force` remains the
  explicit repair path — which is only true if the stamp check goes in a
  caller-selectable seam rather than inside `Install()`; see Step 9 Part 1, where
  both `cmd/configure.go:29-33` and `ForceConfigure()` call `Install()`
  unconditionally today.
- **Correctness before speed on ADR-0030.** The cache lands because one run
  should see one config, not because a benchmark demanded it. Step 8 supplies the
  number afterwards; if it is small, that is a fact to record, not a reason to
  revert.

---

## 8. Cross-Model Review Notes

- [ ] Is the executor grouping right — do P0-1, P0-2, P0-6 and the executor half
      of P0-5 genuinely belong in one commit, or does the `Setpgid` change want
      its own so it can be reverted independently?
- [ ] Is `WaitDelay` the correct Go primitive here, or does the codebase need an
      explicit closer channel for clarity?
- [ ] Does `Setpgid` break Ctrl-C for interactive subprocesses? This is the
      question most likely to be wrong in the plan above.
- [ ] Is Step 5's permission fix general enough to ship to strangers' repos
      (CLAUDE.md §12), or does it encode a devgeta-shaped assumption?
- [ ] Is the checksum rollout ordering safe for users mid-upgrade?
- [ ] Scope locked? P2 items are listed below as prose, deliberately not as
      checkboxes, so this cycle can reach Done.

**Reviewer notes:**
(Fill in during review.)

---

## Investigated and rejected

Recorded so nobody spends the time again. Each of these was the thing worth
looking for, and each came back clean or backwards.

**Startup needs nothing — keep Cobra registration eager.** Total package-init
CPU is **2–7 ms across 121 packages** (`GODEBUG=inittrace=1`), of which
devgeta's own packages account for under 1 ms. The entire `cmd` package init —
all **53** `AddCommand` calls across 12 `init()` funcs, every flag set, every
long help string, the completion registrations — measured **0.13–0.40 ms, 72 KB,
612 allocs**. The top eight inittrace rows are all third-party or stdlib
(`charmbracelet/x/ansi/parser` 3.136 ms, `vendor/golang.org/x/net/http/httpproxy`
2.602 ms, `errors` 2.003 ms, `crypto/sha256` 1.702 ms, `os` 1.247 ms,
`net/http` 0.696 ms, `go.uber.org/zap` 0.654 ms, `mattn/go-runewidth` 0.592 ms);
the first devgeta package is #9 at 0.398 ms. Making `dg task` or `dg worktree`
lazy would save a fraction of a millisecond and cost the `init()`-registration
idiom the whole repo uses. **Do not do it.**

Wall-clock is not measurable at this scale: 30 runs of `dg --version` averaged
86–139 ms/run, but 30 runs of a hello-world Go binary averaged 101–123 ms/run in
the same interleaved loop. Devgeta is indistinguishable from `fmt.Println` — the
~100 ms is process spawn, not devgeta.

Nothing on the path from `dg <anything>` to `RunE` reads a config file, stats a
tree, execs a subprocess, or touches the network. `rootCmd.PersistentPreRunE`
(`cmd/root.go:66-72`) calls `logger.Init(verbose)` and returns. No `embed.FS`
walk at init — `embedded.go:13` declares the FS, `fs.WalkDir` runs only inside
`ExtractEmbeddedConfigs` (`embedded.go:32`), which is call-triggered. All 12
`init()` funcs are pure registration; no package-level var does real work.
`logger.Init` costs **163 µs** against a ~100 ms process spawn — 0.15%, not worth
doing for speed.

**Targeted `brew list <pkg>` probes are 6x SLOWER, not faster.** Measured mean
**6.05 s** for `brew list jq` versus **1.01 s** for the full `brew list`, because
the targeted form resolves the formula rather than reading the Cellar directory.
Any "fix" that narrows the probe makes Step 6 worse. The remedy is one cached
listing.

**The 33.5 MB `crypto/internal/fips140` symbol is not a leak.**
`go tool nm -size` reports `crypto/internal/fips140/drbg.memory` at 33,554,432 B
in `__DATA.__noptrbss`, which is **zerofill** — address space, not resident
memory. Untouched pages are never faulted in, so RSS impact is approximately
zero. It arrives because `pkg/downloader` imports `net/http`, reached from
`internal/commands/debian_strategies.go`, `pkg/apt/ppa.go`, and the
lazygit/lazydocker/rtk/neovim modules. Splitting the download path behind an
interface to avoid linking `net/http` would buy a couple of milliseconds and cost
a real architectural seam. Recorded so the 33 MB symbol is not re-discovered and
mistaken for a leak.

**`devgeta` and `fontconfig.test` are not committed.** `git ls-files` returns
neither; `.gitignore:42` (`/devgeta`) and `.gitignore:12` (`*.test`) cover them,
confirmed with `git check-ignore -v`. The largest tracked file is
`docs/designs/dg_worktree_wireframes.pdf` at 766 KB, and `.git` at 53 MB is
consistent with history, not with a committed 20 MB blob. No defect.

**Release binaries are already stripped.** `.github/workflows/release.yml:68`
passes `-s -w` and `install.sh:153` downloads that artifact, so users get the
14.9 MB binary, not the 19.8 MB one. Only `Makefile:9-11` omits the flags, which
means a locally-built binary is 28% larger (20,721,200 B vs 14,926,752 B, a
5,794,448 B / 5.53 MB delta) — arguably correct, since a developer wants the
symbols. Not a user-facing defect.

**`pkg/paths` importing `testing` costs +130 KB and zero runtime.**
`pkg/paths/paths.go:8` imports it; `:66` calls `testing.Testing()` inside the
`testSandbox` initializer (`:65-92`). Outside `go test` that reads a runtime bool
and returns `""` at `:67` without touching the filesystem — `pkg/paths` init is
0.043–0.079 ms, 5.3 KB, all of it `filepath.Join`. Measured linkage cost with two
probe binaries differing only by the import: 2.59 MB vs 2.72 MB. **Do not
"fix" this by weakening the guard** — CLAUDE.md §4 forbids it.

**Security null results.** No command injection: every `sh -c` / `bash -c` string
in non-test Go is a compile-time constant with no interpolation, and
`internal/commands/base.go:266-305` builds arg slices with no shell (`IsSudo`
prepends into the slice at `:275-277`).
`internal/commands/shell_lookup.go:97-135` passes the tool name as positional
`$1`, never interpolated. The generated `devgeta.zsh` interpolates no
user-controlled string (`internal/config/fromFile.go:595-634`). No
`InsecureSkipVerify` anywhere; the only `http://` in the tree is a comment at
`internal/tooling/terminal/core/jemalloc/jemalloc.go:10`. No hardcoded secrets in
Go or `configs/`. No zip-slip — `embedded.go` extracts from an `embed.FS` whose
names are compile-time. `cmd/configure.go:73` resolves app names through a
registry allowlist and `cmd/list.go:80` uses a category allowlist. `go vet ./...`
exits 0.

**No goroutine or timer leaks.** Every non-test goroutine is a bounded worker
with `defer wg.Done()` and a matching `Wait()`. No `time.NewTicker`,
`time.NewTimer`, `time.Tick`, or `time.After` in non-test code; the only timers
are three one-shot `tea.Tick` calls in `internal/tui/worktree/model.go` (`:544`
3 s, `:550` 30 s, `:699` 180 ms). No `defer f.Close()` inside a loop. The only
fan-out, `internal/tooling/task/branchdiff.go:99-108`, is fixed at 2 goroutines
with `wg.Add(2)` before launch.

**`View()` does zero I/O.** No `os.Stat` / `os.ReadFile` / `os.ReadDir` /
`filepath.Walk` / `exec.Command` / `net/http` anywhere under `internal/tui/`, and
zero `regexp` in the whole TUI tree. The row tree is not rebuilt per render —
`buildRows` (`internal/tui/worktree/tree.go:128`) has one caller, `rebuildRows`
(`model.go:746`), reached only from data-change paths; `j`/`k`
(`model.go:1276-1284`) do not rebuild. Filtering lives inside `buildRows`, not
`View`. The 180 ms diff debounce (`model.go:1059-1073`) means holding `j` through
fifteen rows spends one branch diff, not fifteen.

**Terminal restore is guaranteed.** Bubble Tea v2.0.7 restores on every path:
panic via the deferred `recoverFromPanic` (`tea.go:1269`, deferred at `:725-726`
and `:1028-1030`), SIGINT/SIGTERM via `signal.Notify` at `tea.go:656`, normal
quit via the `q`/`ctrl+c` bindings (`model.go:1266,1465`,
`inventory/model.go:124`). Devgeta never calls `EnterAltScreen`, so there is no
alt-screen to leak. `internal/tui/worktree/run.go:29-31` also silences `WarnFn`
so a raw `utils.PrintWarning` cannot corrupt the live display.

**Other clean sweeps.** No `filepath.Walk` remains (all `WalkDir`). No
`regexp.MustCompile` in a hot function — all 10 occurrences are package-level
vars. No `defer` inside a loop. No O(n²) string accumulation;
`internal/commands/base.go:320-400` already uses `strings.Builder`. No large-file
slurps — downloads stream (`pkg/downloader/retry.go:128-137`) and checksums
stream (`internal/commands/debian_strategies.go:131`). No repeated `brew update`
(`dg install` never runs it) and no per-package `apt-get update` — the only
`apt update` is `pkg/apt/ppa.go:254`, bounded by new PPAs and short-circuited at
`ppa.go:109`. Retries do not fire on non-retryable errors: `pkg/downloader/retry.go:146-153`
retries only 429/502/503/504 and bails at `:102` otherwise.

---

## Deferred — not in this cycle

These are real and cited, but they are not in this cycle. They are deliberately
**not checkboxes** so this doc can reach Done. Citations are preserved here
because the audit that produced them lives in a cache directory that can be
wiped; this file is the only durable copy.

**Git refnames reach argv with no `--` terminator.** Git permits refnames
beginning with `-`, so a hostile or compromised remote can publish
`refs/heads/-f` or `refs/heads/--detach`. The name then flows `fetch` →
branch-list parsing (`internal/apps/git/git.go:361-382`) → picker →
`DeleteBranch` (`internal/tooling/task/task.go:172`). Eleven sites pass a
refname positionally with no terminator: `internal/apps/git/git.go:990`
(`branch -D <name>`), `:215`, `:345` (`checkout <branch>`), `:394`
(`branch --list <branch>`), `:589`, `:720`, `:802`, `:817`, `:831`, `:833`, and
`internal/tooling/task/worktree.go:226` (`worktree add <path> <branch>`).
`git worktree add <path> --detach` silently yields a detached worktree;
`git branch -D -r` retargets the delete at remote-tracking refs. Separately,
`BranchExistsIn(dir, "-a")` returns "exists" for any repo — a detection bypass
feeding `git.go:788`. Remedy when it is scheduled: `--` before every positional
refname plus one shared `rejectDashLeading()` at the `Git` wrapper boundary,
and the same shape in `internal/tooling/task/reviewpackage.go:62,73,111`,
`branchdiff.go:141,300`, and `scope.go:207`.

**`dg wt remove ..` deletes every worktree of every repo.**
`internal/tooling/worktree/worktree.go:67` — `FlattenName` is only
`strings.ReplaceAll(name, "/", "-")`. That neutralizes `../../x` (→ `..-..-x`)
but not the bare component `..`. `worktree.go:191` and `:200` join it unchecked
into `filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, FlattenName(name))`,
so `..` resolves to `~/.local/share/devgeta/worktrees` — or, in the in-repo
shape, to `<repoRoot>/.claude`. `worktree.go:730` sets `WtExists` from a bare
`os.Stat`, so the "nothing to remove" guard passes; the dirty check is skipped
because `IsWorktreeDirty` errors on a non-repo; `worktree.go:2590` then runs
`os.RemoveAll(wtPath)` as the fallback after `git worktree remove` fails. This
is P2 only because it is the user against themselves — the operator (or an agent
driving `dg wt`, e.g. the `dg-worktree` skill deriving a name from an issue
title) supplies the name; it is not remotely reachable. **Two correct sanitizers
already exist in-tree and are simply bypassed here:**
`internal/tooling/task/scratch.go:90-125` (lexical bounds, symlink refusal,
re-check after `EvalSymlinks`) and
`internal/tooling/reviewjournal/encoding.go:34`
(`filepath.Dir(path) == filepath.Clean(dir)` assertion). Remedy: validate at the
`Create`/`Remove`/`Repair`/`Move` boundary — reject `.`, `..`, empty, leading
`-`, separators — then assert `filepath.Rel(base, wtPath)` stays inside `base`
before any `RemoveAll`.

**TUI flow-entry key handlers run subprocesses on the Bubble Tea event loop.**
The dashboard's async discipline is strong everywhere else; the flow-entry
handlers were exempted, so they stall input and rendering. `n`/`N` reaches
`internal/tui/worktree/create_flow.go:93` (`m.repoCandidatesFn`) from the key
handler at `model.go:1426`/`:1429`; that call
(`internal/tooling/worktree/repo_candidates.go:34`) fans out to one
`git worktree list --porcelain` per known repo anchor group
(`worktree.go:927`), one `Git.GetMainWorktree(cwd)`, a `gc.Load()`, an uncached
`scanRepos(SearchPaths, ScanDepth)` filesystem walk (`repo_candidates.go:60`,
opt-in and empty by default but unbounded once configured), and one
`zoxide query -l` (`repo_candidates.go:65-67`) — **N+2 subprocesses plus an
optional FS walk, synchronously, on one keypress.** `s` hits the identical cost
at `session_flow.go:70`. `enter` on a worktree row costs **4-6 tmux
subprocesses**: `model.go:1545` (`Tmux.WindowSession` → `tmux list-windows -a`)
and `model.go:1556` (`Tmux.ClearAgentStateForWindow` → `tmux list-panes -a`
plus one `set-option -p -u` per matching pane plus one `set-option -w -u`).
Both of those are the clearest defects: `:1545` is **redundant** —
`attachToWindowCmd` already does the same lookup correctly inside its goroutine
at `model.go:1622` — and `:1556` contradicts its own sibling
`handleSwitchToPane`, which clears agent state correctly inside the closure at
`model.go:1594`. Lesser sites of the same class: `create_flow.go:244`
(`Git.CheckHookCompatibility`, `git.go:1236` — one `git config --get
core.hooksPath` plus 5 `os.ReadFile`), `create_flow.go:181` (`os.Stat` plus one
`git rev-parse`, `repo_candidates.go:185`), `session_flow.go:184` (one
`tmux list-sessions`). Not a finding: `model.go:861` `m.currentSessionFn()` is
guarded by `m.cursorPlaced` (`model.go:854`) and runs once per program lifetime.
The correct pattern is already in the same file — `computeDiffCmd`
(`model.go:574`), `attachToWindowCmd` (`model.go:1622`), `dispatchSessionCreate`
(`session_flow.go:225`). The structural fix CLAUDE.md §4 asks for is to make the
injected `*Fn` fields unreachable from a key handler's synchronous path — a
wrapper type that can only yield a `tea.Cmd`.

**A second, redundant prompt stack.** `github.com/manifoldco/promptui`, wrapped
by `pkg/promptui/selector.go` (84 LOC), duplicates
`tuicomponents.FuzzyPicker` (171 LOC). Callers:
`internal/tooling/terminal/terminal.go:341`,
`internal/tooling/desktop/desktop.go:196`,
`internal/tooling/languages/languages.go:77`,
`internal/tooling/databases/databases.go:67` — all on the install path. Cost:
**116,141 B ≈ 113 KB of binary** for promptui plus its `chzyer/readline`
dependency, and two different pickers users see in one session. It also carries a
latent correctness bug: `pkg/promptui/selector.go:30` does
`availableOptions := options`, sharing the caller's backing array, and
`:19-26` `removeItem` mutates through it; `:56` `availableOptions[3:]` panics if
a caller ever passes fewer than three options.

**The shell config template is re-parsed and rewritten once per installed tool.**
`internal/config/fromFile.go:627-634` — `RegenerateShellConfig()` calls
`files.GenerateFromTemplate`, which does a `template.ParseFiles` (fresh disk read
plus parse) and then an atomic write of `devgeta.zsh`. There are **30 non-test
call sites**, so `dg install` re-parses the same template and rewrites the same
output file about 30 times, discarding 29 results. Remedy: regenerate once at the
end of the install run, or debounce on a dirty flag, rather than per tool.

---

## Notes for Implementers

- **Implementation is complete.** All 12 steps below shipped, were individually
  reviewed and approved, and are recorded as done in place. The remaining
  bullets in this section are retained as a record of the constraints
  implementation actually operated under — they were accurate throughout the
  cycle and still are.
- **The source audit lives in a cache directory** that can be wiped at any time
  (`~/.cache/devgeta/scratch/perf-audit-2026-08-24.md`). This file is the durable
  copy — every `file.go:LINE` citation and every measurement above was carried
  forward on purpose. If you find a claim here that needs more detail, re-derive
  it from the code; do not assume the scratch file is still there.
- **Do the executor as one change.** Step 1 bundles four separate audit findings
  because they are four symptoms of the same function. Splitting them into four
  commits means four passes over the same 30 lines and four chances to
  reintroduce the one the previous pass fixed.
- **No temporary fixes.** CLAUDE.md §4 forbids workarounds, and the maintainer
  restated it for this cycle specifically. If a proper fix turns out to be
  impossible right now — most likely on `Setpgid` and interactive children — say
  so explicitly and get agreement on the gap. Do not ship something that hides
  the failure.
- **Reuse what already exists.** Several remedies here are "use the correct
  pattern that is already in the tree": the `{path,modTime,size}` cache at
  `internal/tooling/worktree/worktree.go:135-155`, the unused
  `FilePermission`/`DirPermission` at `pkg/files/files.go:18-20`, the staging-dir
  rename at `pkg/apt/ppa.go:237-238`, the sanitizers at
  `internal/tooling/task/scratch.go:90-125` and
  `internal/tooling/reviewjournal/encoding.go:34`. Extend those rather than
  writing new ones (CLAUDE.md §6, "Reuse before writing").
- **Read `## Investigated and rejected` before optimizing anything.** Three of
  the obvious-looking wins in this area were measured and came back neutral or
  backwards. Targeted `brew list <pkg>` probes in particular are 6x _slower_.
- **Both agents or neither.** Step 5 touches agent permissions;
  `internal/apps/opencode/permissions_test.go` fails the build on asymmetry, and
  weakening it to land a one-sided change is not an option (CLAUDE.md §12).
- **Anything under `configs/` ships to strangers' repos.** Steps 5 and 8 both
  edit files that land on machines that are not ours. Apply the test: would this
  make sense to someone who has never seen this repo?
- **Config changes cost a near-full test run.** Anything under `configs/` is read
  by the embedded-config tests in the root package (4.8 min on their own), plus
  `internal/apps/claude` and `internal/apps/opencode`. Budget for it on Steps 5
  and 8 rather than being surprised.
- **Commit after each step**, once that step's verify check passes. Each step is
  written to leave the tree building and green.
- **If a risk from §7 materializes, escalate.** Do not absorb it quietly — the
  `WaitDelay` and `Setpgid` risks in particular change behavior for every
  subprocess devgeta runs.
