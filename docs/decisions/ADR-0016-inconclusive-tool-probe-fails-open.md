# ADR-0016 — An inconclusive tool probe must not block worktree creation

**Date:** 2026-08-05
**Status:** ACCEPTED

## Context

Before building a worktree window, every pane's tool is checked up front
(`Layout.EnsureInstalled` → `ensureToolInstalled` →
`commands.ShellCommandExistsFn`), so a missing coder fails with one actionable
message instead of a window whose pane dies on `command not found`. The probe
spawns the user's interactive shell (`$SHELL -i -c 'command -v -- <tool>'`),
because that shell — not dg's own process PATH — is what the pane will actually
resolve the launch alias in (see `shell_lookup.go`).

The probe is bounded at 5 seconds so a hung shell startup can't stall a create
forever. The defect: the seam is a `func(string) bool`, so a timeout and a
genuine "no such command" both come back `false`, and the caller reports both
as **"opencode is not installed. Install it with: dg install --only terminal"**.

This happened in practice on 2026-08-05: a machine under heavy load (process
spawn ~96ms instead of ~3ms; `zsh -i` startup 10–18s, dominated by the user's
pyenv/rbenv init) pushed every probe past 5s, and every `n`/`N` create in
`dg ws` failed with the install message — for a tool that was installed, whose
alias resolved with exit 0 every single time, just slowly. The message wasn't
merely unhelpful; it prescribed a fix (`dg install`) for a problem that didn't
exist.

The constraint that shapes the decision: **what does the check actually
protect?** A pane whose tool is missing prints `oc: command not found` in the
pane and sits at a shell prompt — visible, cheap, recoverable. The check
trades a few probe-seconds to catch that early. It was never meant to be a
gate that can veto a create on evidence it doesn't have.

## Decision

The probe reports three outcomes, not two, and the caller fails open on the
third:

1. `Found` — the lookup ran and resolved the tool. Proceed.
2. `NotFound` — the lookup ran and said the tool is absent. This is the only
   outcome that blocks the create with the "not installed" message, because
   it is the only one that _proved_ anything.
3. `Inconclusive` — the probe could not determine an answer. Proceed with the
   create, recording the skip at debug level.

"The lookup ran" is proven by a marker, not inferred from the shell's exit
status. The probe script prints one marker line carrying `command -v`'s exit
status _after_ running it, and classification trusts only that line: marker
with status 0 → `Found`; marker with non-zero status → `NotFound`; no marker
(or a mangled one) → `Inconclusive`. The shell process's own exit status is
deliberately ignored — it cannot distinguish "the lookup said no" from "an rc
file or plugin exited non-zero during init, before the lookup ever ran", and
the second is just another way for a machine to produce the false "not
installed" this ADR exists to prevent. The marker's absence also covers the
deadline killing the shell, a `$SHELL` that cannot parse the POSIX probe
script (e.g. fish), and a shell that never started — all Inconclusive.

Failing open is deliberate, not lenient-by-default: on `Inconclusive` the two
possible worlds are (a) the tool exists and the create would have been wrongly
blocked, or (b) the tool is missing and the user sees `command not found` in
the pane — the exact failure the check exists to soften, undiminished, one
step later. Blocking is the worse outcome in both worlds.

The 5s timeout stays. It no longer decides correctness, only how long a create
can be delayed by a slow probe (worst case: one timeout per checked pane).

**Considered and rejected — caching probe results per process.** A positive
cache would only save time on a healthy machine, where the probe costs well
under a second; on a degraded machine there are no `Found` results to cache
(they all time out first), so it does nothing for the failure this ADR is
about. Caching `NotFound`/`Inconclusive` is worse: `dg ws` is long-running,
and a stale negative would keep blocking (or a stale inconclusive keep
skipping) after the user installs the tool or the machine recovers. Package
-level cache state also leaks across tests. Cost without benefit — dropped.

## Consequences

- A slow machine can no longer make `dg ws` lie about what's installed. The
  only way to see "X is not installed" is for the user's own shell to have
  said so.
- A genuinely missing tool that also times out (rather than resolving to
  "not found" in time) now slips past the check and surfaces as
  `command not found` in the pane instead of the friendly message. Accepted:
  that is the check's pre-existing failure mode for anything it can't see,
  and the pane message still names the missing command.
- The seam changes type (`ShellCommandExistsFn func(string) bool` →
  `ShellCommandLookupFn func(string) ShellLookupResult`). Tests keep a
  bool-shaped helper for the common found/not-found cases, plus a tri-state
  helper for the new one.
- Outcome classification lives in a pure function (`classifyShellLookup`)
  over the captured stdout, so the marker rule — no marker, no `NotFound` —
  is unit-testable without spawning a shell.
- The probe's stdout is captured instead of discarded, and rc-file banner
  noise on it is expected: the marker only has to be findable, not alone.
