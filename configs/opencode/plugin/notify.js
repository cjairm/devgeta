// OpenCode plugin: report this coder's activity into the tmux pane it is
// running in, so `dg ws`'s status dot can show working / finished / blocked /
// errored without switching to the window. This is Step 2 of the cycle in
// docs/plans/cycles/2026-07-28-agent-activity-notifications.md; the governing
// design doc is docs/decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md
// — read it for the full value table and for why `busy` must be an explicit
// write rather than a cleared/absent value (a `claude-nvim` window's editor
// pane never writes, and an unset pane must not be mistaken for a working
// agent). The Go read side (Tmux.PaneStates() in internal/apps/tmux/tmux.go)
// is a separate, already-shipped piece of this cycle — this file only writes.
//
// Value table (ADR-0005's "Decision" section), driven by OpenCode's `event`
// hook and its `chat.message` hook:
//
//   session.idle (event)       -> idle
//   permission.updated (event) -> blocked
//   session.error (event)      -> error
//   chat.message (any call)    -> busy
//
// Event name note: the SDK also has an unrelated `permission.replied` event
// and a separate `permission.ask` DECISION hook (used to auto-allow/deny
// prompts). Neither is used here — only the `event` hook's
// `permission.updated` case, which fires when a Permission request record is
// created (the moment a permission dialog appears). Verified against the
// installed @opencode-ai/sdk (1.17.20) at
// ~/.config/opencode/node_modules/@opencode-ai/sdk/dist/gen/types.gen.d.ts.
// Any event type not in the table above is a deliberate no-op (no write, no
// throw) so this plugin stays resilient to the SDK's event names changing
// across OpenCode versions.
//
// Write mechanism: `tmux set-option -p -t "$TMUX_PANE" @dg_agent_state
// <value>` (ADR-0005). `$TMUX_PANE` is the pane's own id, so this hook needs
// no devgeta path knowledge and cannot target the wrong pane. Implemented via
// node:child_process's execFile (array args, no shell interpolation) rather
// than OpenCode's Bun-specific `ctx.$` shell tag: this repo's plugin tests run
// under Node's built-in test runner (`node --test`), not Bun, where `$` has
// no meaning. No-ops (does not attempt the write, does not throw) when
// process.env.TMUX_PANE is unset or empty — OpenCode run outside tmux must
// produce no error and no output about tmux.
//
// Step 8 (status-bar signal for unattended worktrees) adds a second,
// display-only write alongside the pane write above: a WINDOW-level mirror,
// `@dg_window_agent_state`, that configs/tmux/tmux.conf's
// `window-status-format` reads to flag a window nobody is looking at. This is
// deliberately a DIFFERENT option name than the pane-level `@dg_agent_state`
// — see ADR-0005's Step 8 note and the cycle doc for why: tmux options
// cascade window -> pane, so a window-level write under the pane-level name
// would be inherited by every pane in that window without its own override
// (e.g. the nvim pane in a `claude-nvim` layout), corrupting the exact
// per-pane distinction Tmux.PaneStates() depends on. The mirror's write
// semantics also differ from the pane write: `idle`/`blocked`/`error` (the
// "wants you" states) SET the mirror to that value, but `busy` CLEARS it
// (unsets it, does not write the string "busy") — the status-bar flag must
// disappear the instant a new turn begins, the same reasoning that makes the
// pane-level `busy` write explicit rather than a clear. The mirror targets
// the pane's own window via `set-option -w -t <pane>`: tmux resolves a pane
// target to its owning window when `-w` is given, so no separate window-name
// lookup is needed.
//
// Guard against a plugin failure taking down the session: every write is
// wrapped in try/catch and the error is swallowed. A missing dot on the
// dashboard is a cosmetic bug; a crashed coder is not.
//
// Testability: BOTH the pane write and the window mirror write/clear go
// through exactly ONE injected exec seam, `execFn(args: string[])` — a raw
// argv array for any tmux invocation — rather than one seam per write. A
// second, separately-defaulted injection point (e.g. a distinct `mirrorFn`
// parameter) would create a class of test bug where a test mocks one seam
// and forgets the other, letting a real `tmux` invocation slip through a test
// run; collapsing to a single seam makes that structurally impossible
// instead of relying on the test author remembering to override both
// (CLAUDE.md §6: "never execute real commands in tests"). OpenCode's real
// invocation (`Notify(ctx)`, one argument) is unaffected: `execFn` defaults
// to `execTmux`, the real execFile-based implementation below.
// notify.test.mjs passes a stub in place of the second argument in every
// test that calls a handler, so no test ever spawns a real tmux process.

import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

// execTmux is the real (non-test) exec implementation: runs `tmux <args>`
// with array args, so there is no shell to interpolate through. Both the
// pane write and the window mirror write/clear go through this same
// function.
async function execTmux(args) {
  await execFileAsync("tmux", args);
}

export const Notify = async (ctx = {}, execFn = execTmux) => {
  // writeState is the choke point for the pane-level, authoritative write: it
  // is where the "no TMUX_PANE" no-op and the "swallow any failure" rule both
  // live, so no handler can forget either one.
  const writeState = async (value) => {
    const pane = process.env.TMUX_PANE;
    if (!pane) {
      return;
    }
    try {
      await execFn(["set-option", "-p", "-t", pane, "@dg_agent_state", value]);
    } catch {
      // Swallow: a failed tmux write must never crash the OpenCode session.
    }
  };

  // mirrorWindowState sets (value a string) or clears (value === null) the
  // window-level display-only mirror. Targets the window via `-w -t <pane>`:
  // tmux resolves a pane target to its owning window when -w is given, so no
  // separate window-name lookup is needed. A DIFFERENT option name than
  // @dg_agent_state deliberately - see the header comment / ADR-0005 / this
  // task's brief for why reusing the pane-level name would corrupt reads for
  // other panes in the same window. Failure here is independent of
  // writeState's: each has its own try/catch, so one failing never prevents
  // the other from being attempted.
  const mirrorWindowState = async (value) => {
    const pane = process.env.TMUX_PANE;
    if (!pane) {
      return;
    }
    try {
      const args =
        value === null
          ? ["set-option", "-w", "-u", "-t", pane, "@dg_window_agent_state"]
          : ["set-option", "-w", "-t", pane, "@dg_window_agent_state", value];
      await execFn(args);
    } catch {
      // Swallow: same rule as writeState.
    }
  };

  return {
    event: async ({ event } = {}) => {
      switch (event?.type) {
        case "session.idle":
          await writeState("idle");
          await mirrorWindowState("idle");
          break;
        case "permission.updated":
          await writeState("blocked");
          await mirrorWindowState("blocked");
          break;
        case "session.error":
          await writeState("error");
          await mirrorWindowState("error");
          break;
        default:
          // Unrecognized event type: no-op by design (see header comment).
          break;
      }
    },
    "chat.message": async () => {
      await writeState("busy");
      await mirrorWindowState(null);
    },
  };
};
