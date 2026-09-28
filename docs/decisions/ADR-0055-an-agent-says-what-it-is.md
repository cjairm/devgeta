# ADR-0055 — An agent says what it is, separately from what it's doing

**Date:** 2026-09-28
**Status:** ACCEPTED

## Context

The dashboard is getting an agents section
([ADR-0056](ADR-0056-the-dashboard-lists-agents-in-their-own-section.md)): one row per pane
that runs an AI coder, with the coder's name ("claude", "opencode") and a state word.

Today the only thing that marks a pane as an agent is `@dg_agent_state`
([ADR-0005](ADR-0005-agent-activity-state-in-tmux-pane-options.md)), and it can't carry
that job:

- **Acknowledging erases it.** Attaching from `dg ws`, and since ADR-0005's 2026-09-28
  revision any focus change, unsets the state. If "has a state" meant "is an agent", an
  agent would drop off the list the moment you looked at it.
- **A fresh agent has no state yet.** Nothing is written until the first prompt, so a
  coder you just started wouldn't be listed.
- **It doesn't say which coder it is.** The process name is no substitute. Claude Code
  often shows up in `pane_current_command` as its version number or as `node`, and each
  future coder would need its own guessing rule.

## Decision

Each coder writes its own name into a second pane option, **`@dg_agent_kind`**, on the pane
it runs in. It's written through `$TMUX_PANE` exactly like `@dg_agent_state`, with the same
"never fail the agent" tolerance.

| When              | Claude Code (`agent-state.sh`) | OpenCode (`notify.js`)                                   | Write                                                      |
| ----------------- | ------------------------------ | -------------------------------------------------------- | ---------------------------------------------------------- |
| Agent starts      | `SessionStart` hook            | plugin load                                              | `@dg_agent_kind` = name                                    |
| Every state write | the existing three hooks       | the existing events                                      | kind again (cheap, self-healing)                           |
| Agent ends        | `SessionEnd` hook              | `server.instance.disposed`, plus a process-exit fallback | unset kind **and** state, then recompute the window mirror |

Ending recomputes the window mirror (`@dg_window_agent_state`) with the same rule as the
focus hooks (ADR-0005's 2026-09-28 revision): it's cleared once no pane in the window still
holds idle / blocked / error. Otherwise an agent that quits while flagged leaves the status
bar flagging its window until some unrelated write clears it.

A pane is an **agent** when `@dg_agent_kind` is set **and** its `pane_current_command` is
not a plain shell. The shell names live in one constant in `internal/apps/tmux`; this ADR
doesn't repeat the list. The shell check is the backstop for a coder that is killed before
its end hook runs. It needs no extra tmux call, because the fast scan already reads
`pane_current_command`.

**This narrows ADR-0008, it doesn't contradict it.** ADR-0008 rejected
`pane_current_command` as a way to _detect_ agents, "even as a supplementary signal",
because the name is unreliable (`2.1.220` for Claude Code) and a stateless pane can't be
told apart from a non-agent. Here the name never says a pane _is_ an agent; only the
coder's own `@dg_agent_kind` does. The name is only used to _rule a pane out_, and only when
it's a plain shell, which means the coder has gone and the kind is left over. A coder
reporting a shell name while it runs would hide a live agent, so Step 0 of the cycle checks
that directly, including while Claude Code runs a Bash tool command. ADR-0008 gets a
revision note pointing here, and the `CurrentCommand` field comment in
`internal/apps/tmux/tmux.go` is rewritten to match.

The stored state values don't change. The dashboard maps them to words:

| `@dg_agent_state` | Shown as                                         |
| ----------------- | ------------------------------------------------ |
| `blocked`         | **blocked**                                      |
| `error`           | **error**                                        |
| `idle`            | **done**: finished, and you haven't looked       |
| `busy`            | **working**                                      |
| _(unset)_         | **idle**: finished and seen, or not prompted yet |

`@dg_agent_kind` never inherits: it is written with `-p` only, never at window level, for
the same reason ADR-0005 keeps the window mirror under its own name.

**Rejected: a separate "seen" option next to the state.** It was the first idea for keeping
an acknowledged agent listed. Once the kind carries identity, clearing the state no longer
loses anything: unset simply reads as "idle". A seen flag would be a third option to keep
in sync with the other two, and it would buy nothing.

**Rejected: guessing from `pane_current_command`.** It's unreliable for Claude Code and
needs a new rule per coder. The kind is the coder's own word.

## Consequences

- Easier: the agents list works for any coder that has a hook. Adding a third is one write
  of the same two options. No migration: the state values and their writers keep their
  meaning.
- Harder: both coders' integrations change together (CLAUDE.md, "Keeping the two AI
  agents in sync"). The kind is written on every state write, which adds one `tmux` call
  per hook run unless it's folded into the state write's call (`\;`), and it should be.
- Accepted: OpenCode's `server.instance.disposed` event and a plugin's own `process.on('exit')`
  handler are **unreliable in practice, not just in theory** — verified against a real
  `opencode` TUI (v1.18.33) in a scripted tmux pane: neither fired on a bare `SIGTERM`, nor
  on the documented double-ctrl-c quit gesture, even though the process fully exited both
  times and the pane's foreground command correctly became the shell. So "unset kind + state
  ... on `server.instance.disposed`, plus a process-exit fallback" (the cycle's Step 1) is
  best-effort belt-and-suspenders, not the mechanism this design actually depends on. The
  shell-name backstop above is what keeps the agents list correct once a coder exits by any
  means — `@dg_agent_kind` staying set no longer matters the moment `pane_current_command`
  reads back as a shell. What the disposed/exit path failing to fire actually costs: (1)
  `@dg_agent_kind` itself lingers as a dead pane option until the pane closes — harmless
  unless a later non-shell program (e.g. `less`) runs in that same pane, which would then
  read as an agent until it exits, same as the already-accepted `SIGKILL` case; (2) the
  window-level status-bar mirror (`@dg_window_agent_state`) doesn't get recomputed on quit
  specifically — but it never did before this cycle either, since there was no `SessionEnd`-
  equivalent hook; it's still cleaned up by the pane-focus-in/out hooks (`3013bd0`), unchanged
  from today. Claude Code's hooks do not share this problem: `SessionStart`/`SessionEnd`
  reliably inherit `$TMUX_PANE` and `SessionEnd` fires with `reason: prompt_input_exit` on
  both `/exit` and double ctrl-c — verified the same way, against a real `claude` session.
