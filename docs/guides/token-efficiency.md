# Token and context efficiency

Three local, deterministic mechanisms cut agent token spend, built in
[docs/plans/cycles/2026-08-25-token-and-context-efficiency.md](../plans/cycles/2026-08-25-token-and-context-efficiency.md).
None of them touch the request path — ADR-0031 rejected that approach outright
(see [docs/decisions/ADR-0031-context-is-reduced-at-write-time-not-at-send-time.md](../decisions/ADR-0031-context-is-reduced-at-write-time-not-at-send-time.md)).
This guide orders them by measured impact, not by how they were built.

## 1. Measure first: `dg task context-report`

Before trimming anything, run:

```bash
dg task context-report
```

It prints, per agent, every layer that loads into a session before the first
prompt — CLAUDE.md and its `@imports`, auto-memory, settings, skills,
commands, and more — with a byte count for each and a labeled
character-based token estimate. See
[docs/spec.md's context-report entry](../spec.md) for the full breakdown and
[docs/guides/output-budget-runner.md's runner contract](output-budget-runner.md)
if you're auditing the output-budget hook itself rather than base context.

**A validation run in this repo found real numbers worth knowing:** this
project's own `CLAUDE.md` alone is around 47 KB (roughly 12,000 tokens by the
report's estimate) — and if you run `dg task context-report` from inside a
devgeta worktree (`.claude/worktrees/<name>/`, devgeta's own convention),
expect close to **double** that. Worktrees nest inside the main checkout, so
Claude Code's own documented "every directory above it" `CLAUDE.md` walk picks
up the main checkout's `CLAUDE.md` _and_ the worktree's own — two copies of
nearly the same 47 KB file, every session. That's a real cost specific to this
repo's own worktree layout, not a devgeta-wide default; it's recorded here
because `context-report` is what surfaced it, and fixing it is a worktree-
layout decision for a future cycle, not a code change this guide can hand you.

## 2. The biggest lever: trim what loads every session

Base context — CLAUDE.md files, skill/command frontmatter, settings, MCP
config — loads on **every** session before you type anything, whether that
session runs for one message or two hours. A verbose test run costs tokens
once, when it happens; a 47 KB CLAUDE.md costs tokens on session #1, session
#50, and every session after that, whether or not anything in it is relevant
to what you're doing that day.

This is real and measurable with `context-report`, but devgeta ships no
automated tool for it: shrinking a specific CLAUDE.md is a judgment call about
what that project's contributors actually need restated versus what a
capable agent already infers from the codebase, and that call is yours to
make per-repo, not devgeta's to make for you. What the report gives you is the
number to act on, instead of a vague "this file feels big."

## 3. The output-budget hook: caps verbose command output at write time

Off by default. Turn it on with:

```bash
dg config set integrations.output_budget true
dg configure claude --force      # if you use Claude Code
dg configure opencode --force    # if you use OpenCode
```

Either `configure` command alone re-converges **both** agents — the runner
and the sidecar describing it are agent-neutral and written by whichever
configure path runs. `dg config unset integrations.output_budget` (followed
by re-running the `configure` command(s) above) turns it back off.

When on, a matched command (`go test`, `npm test`, `make`, and similar
general-purpose test/build runners — see
[the runner contract's default rule set](output-budget-runner.md#84-the-default-rule-set)
for the full list) is rewritten to run through a shared script that captures
its output, preserves its exit status exactly, and — only if the output
actually exceeds the budget — replays a head/marker/tail reduction while
leaving the complete output on disk at a path the marker names. Nothing is
silently dropped: a full test suite's output is still there to `grep`, just
not all sitting in the transcript by default.

This is a **recurring** saving — every matched command run, every session —
which can outweigh a one-time base-context trim over a long working session,
but it only helps commands devgeta's built-in rules recognize, and it starts
at zero until you turn it on.

**One known limitation, by design, not by oversight:** on Claude Code, this
hook and a separately-installed `rtk` command-rewrite hook race if both are
enabled and both would rewrite the same call — `PreToolUse` hooks run in
parallel there with no chaining between them, confirmed against Claude Code's
own hook execution model. If rtk's hook happens to finish last, this hook's
cap is silently skipped for that one call; the command still runs correctly
and in full, just uncapped. There is no way to make this deterministic from
either hook's own code. See
[the runner contract's §9](output-budget-runner.md#9-known-limitations---document-do-not-paper-over)
for the full reasoning. OpenCode has no equivalent limitation — its plugin
hooks run in sequence, so a plugin registered after rtk's genuinely sees
rtk's already-rewritten command.

## 4. Session handoff: end a session instead of growing it

```bash
dg task handoff --write --note "left off wiring the OAuth callback, next: token refresh"
dg task handoff --read      # from a fresh session, or a different worktree of the same repo
dg task handoff --clear     # once the branch is done
```

A long session resends its accumulated history on every turn — the third
driver of token spend this cycle's origin cites, alongside verbose tool
output and base context (see the cycle doc's §1). The structural fix devgeta
chose is a durable, per-branch note a session writes **explicitly** before
ending, not a longer session and not an automatically-captured memory
(ADR-0032 rejected both). Nothing here calls a model: `--write` stores
whatever text it's given, and deciding what's worth carrying forward is a
skill's job, composed on top of this command, not something baked into it.

The note is capped at 8 KiB, rendered — a write that would exceed it is
refused outright, never silently truncated, and the previous note is left
exactly as it was. It lives under the repo's common git directory, so it's
visible identically from the main checkout and any linked worktree, and it's
never committed.

## What's deliberately not here

- **No compression proxy or request-path rewriting.** Investigated and
  rejected on caching, attack-surface, and shipped-trust grounds — see
  [ADR-0031](../decisions/ADR-0031-context-is-reduced-at-write-time-not-at-send-time.md).
- **No automatic memory capture, retrieval, or injection.** See
  [ADR-0032](../decisions/ADR-0032-session-continuity-is-a-durable-note-not-a-longer-session.md) §4.
- **`rtk`'s command-rewriting hook stays opt-in**, tracked separately in
  [ROADMAP.md](../../ROADMAP.md) — not something this cycle changed.
- **User-defined output-budget rules.** v1 ships built-in defaults only; see
  [the runner contract](output-budget-runner.md#84-the-default-rule-set).
