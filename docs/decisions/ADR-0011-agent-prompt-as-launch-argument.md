# ADR-0011: An opening prompt is a launch argument, not keystrokes

**Status:** ACCEPTED
**Date:** 2026-07-31
**Deciders:** cjairm
**Related:** [ADR-0010](ADR-0010-worktree-layout-is-a-setting-git-is-the-index.md), [cycle 2026-07-31-ws-create-prompt-and-pane](../plans/cycles/2026-07-31-ws-create-prompt-and-pane.md)

---

## Context

`dg wt create <name>` builds a worktree plus a tmux window and fills that window
from a layout, launching an AI coder (`cc` / `oc`) in one pane. The pane opens at
an empty session waiting for the user to type.

Anything that wants to hand the new session a task — a triage skill, a wrapper
script, a person holding a ticket number — needs a way to give the coder an
opening prompt at creation time. There are two ways to deliver one to a coder
running inside a tmux pane:

1. **As an argument on the launch command line**, so the prompt is part of the
   command the pane is told to run.
2. **As keystrokes sent after launch**, via `tmux send-keys` into the running
   pane.

The second option is what a caller has to do today, and it is why this decision
is worth recording: keys sent to a coder's TUI before it has finished booting are
dropped, so `send-keys` only works if devgeta first polls `capture-pane` until
the TUI looks ready.

Both currently supported coders accept a prompt on the command line (verified
against the installed binaries, 2026-07-31):

- `claude [options] [command] [prompt]` — positional; "starts an interactive
  session by default". So `cc 'prompt'`.
- `opencode --prompt <string>` — "prompt to use". So `oc --prompt 'prompt'`.

devgeta already builds one such command: `ReviewCommand` emits
`oc --agent code-reviewer --prompt '…'` for the `dg ws` review keybinding.

## Decision

**An opening prompt is passed to the coder as a launch argument. devgeta never
types a prompt into a running TUI.**

The exact argument form belongs to the coder: `PromptCommand(prompt string)` is a
method on the `AICoder` interface, alongside `Command()` and `EnsureInstalled()`,
so each coder owns its own spelling and a coder added later cannot be registered
without deciding one.

The prompt is **shell-single-quoted** (`shellSingleQuote`). The resulting command
is typed into an interactive shell verbatim by `send-keys`, with no Go-side shell
parser in between, so an unquoted prompt would let metacharacters in the user's
text corrupt or hijack the command line.

### The quoting asymmetry with `--pane`

The same cycle adds `--pane '<command>'`, whose value is **not** quoted. This
looks inconsistent and is deliberate:

- A **prompt** is one literal argument handed to a program. Quoting it preserves
  exactly the text the user wrote.
- A **`--pane` value is itself a shell command line.** Quoting it would break the
  compound commands that make the flag worth having — `--pane 'cd api && make
dev'` must run two commands, not look for a program whose name contains `&&`.

The trust levels differ accordingly, and match what the user asked for in each
case: a prompt is inert data, while a `--pane` value is a command the user
explicitly asked their own shell to run — the same trust level as a shell alias.

## Consequences

### Positive

- **No race to lose.** The prompt is part of the command that starts the coder,
  so it cannot arrive before the coder is ready to receive it. There is no
  polling loop, no timeout to tune, and no "the prompt sometimes doesn't land."
- **devgeta depends on each coder's documented CLI, not on its TUI rendering.** A
  `capture-pane`-based readiness probe would couple devgeta to how a coder draws
  its prompt on screen — something that changes without notice and cannot be
  tested without a real coder running in a real terminal.
- **A wrong form fails visibly.** If a coder changes its prompt flag, the pane
  shows the error immediately. A dropped keystroke fails silently, leaving a
  session that looks fine but was never given the task.
- **One author per coder.** `ReviewCommand` is rebuilt on `PromptCommand`, so the
  `--prompt '<quoted>'` fragment exists in exactly one place.

### Negative

- **The set of supported coders is bounded by "accepts a prompt on the command
  line."** Both current coders do. A future coder that does not must fail loudly
  when asked for a prompt, never fall back to keystrokes — falling back would
  reintroduce exactly the silent-drop failure this decision exists to remove.
- **A layout with no AI pane cannot take a prompt.** `--prompt` with
  `--layout nvim` is an error rather than a no-op. Accepted: losing the user's
  prompt with no signal is worse than a clear refusal, and appending it to a
  non-coder command would be actively wrong (`nvim 'explain issue 1082'` opens a
  file by that name).

### Neutral

- Repair does not accept a prompt. Re-sending an opening prompt to a repaired
  window would start a _new_ conversation rather than restore the old one — a
  different feature that needs its own decision. `PromptCommand` would work there
  unchanged if that meaning is ever settled.

## Alternatives Considered

### Send keystrokes after launch, gated on a readiness poll

Send the coder's launch command, then poll `capture-pane` until the pane's output
looks like a ready prompt, then `send-keys` the prompt text.

Rejected: it is best-effort by construction. A slow boot — a cold cache, a large
repo, a machine under load — silently drops the prompt, and the failure mode is
a session that looks correctly created but was never given its task. It also
makes devgeta's correctness depend on matching a coder's TUI output, which no
test can pin without running the real coder in a real terminal.

### A package-level function switching on coder type

One `promptCommand(coder AICoder, prompt string) string` with a type switch.

Rejected: it keeps the `AICoder` interface smaller but silently falls through for
any coder added later, producing a promptless launch with no compile error. The
interface method makes the omission impossible.

### A `worktree.default_prompt` setting

Rejected: a prompt is task-specific by nature, so a persistent default one is
meaningless. This stays a per-invocation flag. See ADR-0010 for what does belong
in settings (the layout).
