# ADR-0065 — Devgeta does not ship rtk

**Date:** 2026-11-06
**Status:** ACCEPTED

## Context

Devgeta installed [rtk](https://github.com/rtk-ai/rtk) as the only app in an
`ai-tools` category (ADR-0004) and could wire its command-rewriting hook into
Claude Code and OpenCode, behind a shim that stripped rtk's permission
decision (ADR-0038). The evidence is in
[the rtk investigation](../investigations/2026-11-06-rtk-value-and-risk.md).
In short:

- **It barely saved anything.** rtk's own estimate was 0.3% of the real bill,
  and under 2% even counting cache re-reads. Independent benchmarks ranged
  from 7.6% more expensive to a non-significant 4.8% cheaper.
- **It made the agent redo work.** Hidden output forced reruns, and rtk's
  stats can't see them, so its savings figure was optimistic.
- **It slipped past devgeta's permission rules.** `rtk init` replaced the shim
  with its own hook, and that hook answered `allow` for commands it rewrote.
- **Its direction is one ADR-0031 rejects.** rtk 1.0 adds an LLM proxy that
  rewrites requests at send time and borrows the agent's credentials, with
  known security issues, under a BUSL licence.

## Decision

Remove rtk from devgeta completely: the app, the `ai-tools` category (rtk was
its only app), the `--only=rtk` configure part, the shim, the
`integrations.rtk_claude_hook` flag, and the related permission entries. No
uninstall path is kept. `dg install rtk` and `dg install --only ai-tools` are
now unknown targets, so devgeta cannot put rtk back.

Don't re-add rtk, or any tool that rewrites or shortens an agent's command
output, unless a new ADR shows a measured A/B result on real sessions. That
result has to include the reruns, and the tool must never make permission
decisions.

## Consequences

- With no hook rewriting commands, devgeta's own `ask`/`deny` rules apply to
  every command again. That includes the `gh api` gates from ADR-0039, which
  couldn't fire while rtk was rewriting.
- Users who installed rtk through devgeta keep the binary, whatever
  `rtk init` wrote, and `~/.claude/rtk-shim.sh` (`dg configure` doesn't
  delete scripts it no longer ships). Devgeta no longer knows about any of
  them. [The migration note](../migrations/rtk-removal.md) has the cleanup
  steps and the order they must run in.
- An old `rtk_claude_hook` key in `global_config.yaml` is ignored on load and
  dropped on the next save.
- Supersedes ADR-0004 and ADR-0038, and amends ADR-0031's line naming rtk as
  its reference implementation.
