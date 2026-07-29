# ADR-0007 — The task redirects stay hard-deny, not `ask`

**Date:** 2026-07-29
**Status:** ACCEPTED

## Context

`configs/claude/task-redirect.sh` and `configs/opencode/plugin/task-redirect.js`
intercept raw git/`gh` commands that have a better `devgeta task` equivalent, and
**deny** them (exit 2 on the Claude side, a thrown `Error` on the OpenCode side)
with the replacement command in the message.

A hard deny breaks any workflow that legitimately needs the raw command. That
really happens: an unrelated repository with its own worktree conventions had
four call sites denied by devgeta's globally-deployed hook, which is what
prompted the re-scoping work in
[the 2026-07-29 cycle](../plans/cycles/2026-07-29-hook-rescope-and-worktree-location.md).

That raised a broader question, distinct from scoping: should these rules deny at
all, or should they **prompt**? Claude Code's PreToolUse hooks support
`permissionDecision: "ask"`, which turns an over-broad match into a question
instead of a failure. rtk's hook demonstrably uses that JSON shape on every Bash
call, so the mechanism works in practice — ADR-0006 §2's preference for exit-code
deny was about avoiding JSON-escaping risk, not a finding that the JSON path is
unreliable.

ADR-0006 §2 chose deny for the **safety** guardrails (secret-commit,
lint-suppression), where the thing being prevented is irreversible. These
redirects are different in kind: they are **steering**. The raw command would
have worked correctly — just more verbosely than the `devgeta task` equivalent.
Nothing irreversible is at stake, which is a real argument for prompting rather
than blocking.

The constraint is CLAUDE.md's two-agent rule: any change here must be expressible
in **both** agents, and "If a rule genuinely cannot be expressed in one agent,
drop it from both."

## Decision

**Keep hard-deny in both agents. Do not adopt `"ask"`.**

The decision rests on a probe of the installed OpenCode 1.18.9 plugin API, run in
an isolated scratch project with a plugin that logged both candidate hooks:

| Run | Config                 | `tool.execute.before`                            | `permission.ask`                      |
| --- | ---------------------- | ------------------------------------------------ | ------------------------------------- |
| 1   | default (bash allowed) | **fired** — `{"tool":"bash","command":"echo …"}` | **did not fire**                      |
| 2   | `"bash": {"*": "ask"}` | did not fire                                     | did not fire — run **blocked** ~4 min |

Three findings, in order of weight:

1. **OpenCode's only `"ask"`-capable hook does not fire for the commands that
   matter.** The redirect lives in `tool.execute.before`, whose output type is
   `{args: any}` — no decision field, so the only way to block is to throw. The
   hook that _can_ return `{status: "ask" | "deny" | "allow"}` is
   `permission.ask` (`@opencode-ai/plugin/dist/index.d.ts:225`), and run 1 shows
   it does not fire for auto-allowed bash. That run is conclusive rather than a
   registration failure: the same returned object's `tool.execute.before` handler
   fired, proving the plugin was loaded and active. Devgeta's OpenCode template
   allows bash broadly, so `permission.ask` would never see the traffic the
   redirect exists to intercept.

2. **Where `"ask"` does take effect, it converts a break into a stall.** Run 2
   blocked indefinitely on a prompt no unattended run can answer, and had to be
   killed. For automated multi-session workflows — the exact case that motivated
   the re-scoping — an indefinite stall is worse than a deny that prints the fix
   and exits immediately.

3. **The payload is untyped.** `Permission` is
   `{id, type, pattern?, sessionID, messageID, callID?, title, metadata}` with no
   `command` field. A `permission.ask` implementation would have to recover the
   command from `title` or the untyped `metadata` bag — a much weaker contract
   than `output.args.command`.

The rejected alternative was **`"ask"` on Claude Code only, deny on OpenCode.**
Declined: OpenCode users would keep the broken workflows, so it buys half a fix
while adding a permanent asymmetry to maintain. CLAUDE.md's rule points the other
way — if one agent can't express it, drop it from both.

Scope is not a substitute for this decision, and this decision is not a
substitute for scope: correctly scoping a rule (ADR-0006's test) is what stops it
firing where it doesn't belong. `"ask"` would only have softened the symptom when
scoping is wrong.

## Consequences

- The redirects' mechanism is unchanged, so the re-scoping cycle needs no
  behavioral rework and its existing tests keep asserting exit 2 / throw.
- Both agents stay identical in mechanism, which keeps the mirrored rule tables
  and their two test suites comparable case for case.
- **A mis-scoped or over-broad rule remains a hard break, not a prompt.** That
  raises the cost of getting scope wrong, so scope correctness carries the whole
  burden: every new rule must be argued through ADR-0006's global-vs-devgeta test
  before it ships, and rules that encode devgeta's own conventions must be gated.
- The documented bypass stays the only escape hatch, which makes its wording
  load-bearing — hence the re-scoping cycle's fix to state who sets
  `DEVGETA_SKIP_TASK_REDIRECT` and where.
- **Revisit if** OpenCode gains a decision field on `tool.execute.before`, or
  `permission.ask` starts firing for auto-allowed tools. Both are observable with
  the same probe: a plugin logging each hook, one auto-allowed bash command, and
  a check of which handlers ran.
- Not re-litigated without new evidence. The probe above is the bar: a claim that
  `"ask"` is feasible needs a run showing `permission.ask` firing for an
  auto-allowed bash command with the command text recoverable.
