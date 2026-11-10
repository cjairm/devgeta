// OpenCode TUI plugin: report this coder's activity into the tmux pane it is
// running in, so `dg ws`'s status dot can show working / finished / blocked /
// errored without switching to the window. The governing design doc is
// docs/decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md — read
// it for the full value table and for why `busy` must be an explicit write
// rather than a cleared/absent value (a `claude-nvim` window's editor pane
// never writes, and an unset pane must not be mistaken for a working agent).
// The Go read side (Tmux.PaneStates() in internal/apps/tmux/tmux.go) only
// reads; this file only writes.
//
// Why a TUI plugin (docs/apps/opencode.md): OpenCode 2 runs one shared
// background server for every terminal, and server plugins run inside it,
// with the environment of whichever pane happened to start it. This folder's
// tui.js runs in the terminal client instead — the process in THIS pane — so
// process.env.TMUX_PANE is this pane. The shared server still sends every
// session's events to every client, so each handler first checks the session
// belongs to this pane (`ownsSession`): the session the client is showing,
// or one of its subagents.
//
// Value table (ADR-0005's "Decision" section), driven by OpenCode 2 events:
//
//   session.execution.started            (root session) -> busy
//   session.execution.succeeded          (root session) -> idle
//   session.execution.interrupted        (root session) -> idle
//   session.execution.failed             (root session) -> error
//   permission.asked / form.created      (any of ours)  -> blocked
//   permission.replied / form.replied /
//   session.tool.success / .tool.failed  (any of ours)  -> busy, if not already
//   the plugin's cleanup (client quits)                  -> state cleared
//
// "Root session" filters out subagents: a subagent finishing is not the turn
// finishing. A question or permission prompt from a subagent still needs you,
// so blocked counts any session whose root is ours. Event types not in the
// table are ignored, so a new event type upstream changes nothing here.
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
// `@dg_window_agent_state`, that configs/tmux/tmux.conf.tmpl's
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
// Since docs/decisions/ADR-0009-audible-agent-notifications.md (Step 11 of
// docs/plans/cycles/2026-07-29-ws-agent-panes-and-sound.md), this plugin also
// plays a sound for the same three "wants you" states (idle/blocked/error;
// `busy` never dings), gated so it never fires for the agent you're already
// watching. See `playNotifySound`/`firePlayer` below — they must stay
// behaviourally matched with the bash counterpart, configs/devgeta/agent-state.sh.
//
// Guard against a plugin failure taking down the session: every write is
// wrapped in try/catch and the error is swallowed. A missing dot on the
// dashboard is a cosmetic bug; a crashed coder is not. The same tolerance
// governs the sound gate reads and the player below.
//
// Testability: the pane write, the window mirror write/clear, the sound
// gate reads, AND the player invocation all go through exactly ONE injected
// exec seam, `execFn(cmd: string, args: string[]) -> stdout` — one argv per
// call, cmd separated from args so any binary (`tmux`, `afplay`, `paplay`)
// can be invoked, not just tmux. A second, separately-defaulted injection
// point (e.g. a distinct `playerFn` parameter) would create a class of test
// bug where a test mocks one seam and forgets the other, letting a real
// process slip through a test run; collapsing to a single seam makes that
// structurally impossible instead of relying on the test author remembering
// to override both (CLAUDE.md §6: "never execute real commands in tests").
// OpenCode's real call (`setup(context)`, which calls `createNotify(context)`)
// is unaffected: `execFn` defaults to `execTmux`, the real execFile-based
// implementation below — despite the name, it runs whatever `cmd` it's given
// (tmux or a sound player), not only tmux; the name is kept because every
// call site but the player ones targets tmux. notify.test.mjs passes a stub
// as createNotify's second argument in every test, so no test ever spawns a
// real tmux or player process.

import { execFile, execFileSync } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

// execTmux is the real (non-test) exec implementation: runs `cmd` with array
// args via execFile (no shell to interpolate through) and resolves to its
// stdout. Every real call this plugin makes — the pane write, the window
// mirror, the sound gate reads, and the player invocation — goes through
// this same function; only the gate reads and the player probe below
// actually use the returned stdout.
async function execTmux(cmd, args) {
  const { stdout } = await execFileAsync(cmd, args);
  return stdout;
}

// readTmux is a thin convenience wrapper around the execFn(cmd, args) seam
// for the common "run tmux, read trimmed stdout" case the sound gate and the
// pane_tty resolution both need. Resolves to null on ANY failure (missing
// tmux binary, dead server, unknown option) rather than throwing — mirroring
// bash's `$(tmux ... 2>/dev/null)`, where a failed command substitution
// reads as an empty string that then fails the caller's `= "on"` /
// `= "0"` comparison. Never throws, so callers don't need their own
// try/catch around it.
async function readTmux(execFn, args) {
  try {
    return (await execFn("tmux", args)).trim();
  } catch {
    return null;
  }
}

// WANTS_YOU_STATES: the three @dg_agent_state values that mean "an agent is
// asking for you" - shared by the window mirror's write/clear decision
// (ADR-0005) and by endAgent's mirror recompute below (ADR-0055), so the two
// can never drift into checking a different set.
const WANTS_YOU_STATES = ["idle", "blocked", "error"];

// exitFallbackSync is ADR-0055's "Agent ends" cleanup, attempted from
// `process.on("exit", ...)` as an absolute last resort. Node's `exit` event
// forbids async work - every listener must finish synchronously, since the
// process terminates the instant the last one returns, before any promise
// or callback it scheduled can ever run (see Node's process docs on the
// `exit` event) - so this cannot reuse the async execFn seam every other
// call in this file goes through. It needs its own, clearly-separate
// synchronous exec function (syncExecFn, real default: execFileSync). This
// is NOT a violation of the single-seam design the header comment describes
// for execFn: sync vs. async is a capability the JS runtime itself draws
// the line on, not the same call injected twice for convenience.
//
// The normal "agent ends" path is the plugin's cleanup (endAgent), which
// OpenCode 2 calls when the client quits. This is the backstop for a client
// that dies without cleanup (a bare SIGTERM, an uncaught exception); the
// shell-name backstop in internal/apps/tmux keeps the agents list correct
// even if neither runs (ADR-0055). It skips the window-mirror recompute
// endAgent does: that needs a second synchronous call for a path that is
// already a last resort.
//
// Not exported: tests reach it through createNotify's syncExecFn seam.
function exitFallbackSync(syncExecFn = execFileSync) {
  const pane = process.env.TMUX_PANE;
  if (!pane) {
    return;
  }
  try {
    syncExecFn("tmux", [
      "set-option",
      "-p",
      "-u",
      "-t",
      pane,
      "@dg_agent_state",
      ";",
      "set-option",
      "-p",
      "-u",
      "-t",
      pane,
      "@dg_agent_kind",
    ]);
  } catch {
    // Swallow: the process is exiting either way; nothing here may throw.
  }
}

// SOUND_FILES: the per-state stock sound, one pair per platform (ADR-0009's
// player table, idle/blocked/error column order). No bundled audio, no
// configurable file — see the ADR's "Rejected alternatives" for why.
const SOUND_FILES = {
  idle: {
    afplay: "/System/Library/Sounds/Glass.aiff",
    paplay: "/usr/share/sounds/freedesktop/stereo/complete.oga",
  },
  blocked: {
    afplay: "/System/Library/Sounds/Ping.aiff",
    paplay: "/usr/share/sounds/freedesktop/stereo/message.oga",
  },
  error: {
    afplay: "/System/Library/Sounds/Basso.aiff",
    paplay: "/usr/share/sounds/freedesktop/stereo/dialog-error.oga",
  },
};

// The terminal-bell fallback byte (`\a` / BEL), written directly to the
// pane's tty device — see firePlayer's comment for why it can't just be
// this process's own stdout.
const BELL_BYTE = "\x07";

// firePlayer probes afplay then paplay (first match wins) and falls back to
// the terminal bell, exactly ADR-0009's player table and probe order. Unlike
// bash's `command -v`, there is no cheap, non-spawning existence check
// available through the one exec seam here, so "available" is determined by
// attempting the invocation itself — but the fallthrough must reproduce
// `command -v`'s EXISTENCE-ONLY semantics, not "any failure falls through."
// bash's agent-state.sh picks a player by `command -v` alone and then fires
// it detached (`&`), so once a player is chosen, a runtime failure of that
// player (missing audio device, no PulseAudio session, missing sound file)
// is never observed and never triggers a fallback to the next candidate —
// bash has no way to know. Matching that: only a rejection whose `.code` is
// `"ENOENT"` (Node's execFile/child_process code for "no such file or
// directory," i.e. the binary itself does not exist) falls through here; any
// other rejection means the binary was found and invocation failed for some
// other reason, so this stops right there, silently, exactly like bash's
// backgrounded call. Every step is independently try/caught so a failure
// anywhere in the chain can never escape as an unhandled rejection.
//
// Callers must NOT await this function to completion (see playNotifySound):
// a real afplay/paplay invocation only resolves once the sound finishes
// playing, and this plugin must return immediately regardless of that
// duration or of the player hanging — the JS equivalent of bash's
// `( play_cmd & ) || true`. Node's execFile-based execFn genuinely does wait
// for the child process to exit, so "detached" here means "the caller
// doesn't wait for this promise," not that the underlying invocation is
// literally backgrounded at the OS level.
async function firePlayer(execFn, pane, files) {
  try {
    await execFn("afplay", [files.afplay]);
    return;
  } catch (err) {
    if (err?.code !== "ENOENT") {
      // afplay exists but the invocation itself failed (bad audio device,
      // missing sound file, killed, etc.): bash would have chosen afplay via
      // `command -v` and fired it detached, never learning this either, so
      // no fallback to paplay/the bell here — stop exactly where bash would.
      return;
    }
    // afplay is not installed (ENOENT): fall through to paplay, matching
    // bash's `command -v afplay` returning nonzero.
  }
  try {
    await execFn("paplay", [files.paplay]);
    return;
  } catch (err) {
    if (err?.code !== "ENOENT") {
      // Same reasoning as the afplay branch above: paplay exists but failed
      // at runtime, which bash could never observe either — stop here.
      return;
    }
    // paplay is not installed either: fall through to the terminal bell.
  }

  // The fallback IS the bell, but writing it to this process's own stdout
  // never reaches the pane's terminal: OpenCode may capture/pipe a plugin's
  // stdout upstream of the terminal, the same problem agent-state.sh hits on
  // the Claude Code side (Claude Code parses hook stdout for JSON on exit —
  // see docs/apps/claude.md); empirically, writing the bell to this
  // process's stdout does not reach the pane either. Resolve the pane's REAL
  // tty device (same -p -t <pane> pattern the gate
  // checks below use) and write the byte directly there via fs, not through
  // the exec seam — this isn't spawning a process, so it needs no seam of
  // its own. A failed/empty resolution (dead server, no server) is silence,
  // same as every other step here.
  const paneTty = await readTmux(execFn, [
    "display-message",
    "-p",
    "-t",
    pane,
    "#{pane_tty}",
  ]);
  if (!paneTty) {
    return;
  }
  try {
    await writeFile(paneTty, BELL_BYTE);
  } catch {
    // Resolving pane_tty succeeded but the write itself failed: silence.
  }
}

// createNotify is the plugin body. setup() calls it with only the context;
// the tests pass stubs for execFn (and syncExecFn) so no test ever runs a real
// tmux or sound player. syncExecFn defaults to a REAL synchronous exec only
// when execFn itself was left at its real default, so a test that stubs
// execFn can never register a live process.on("exit") handler that would fire
// a genuine tmux call against whatever pane the test runner sits in (ADR-0055).
export function createNotify(
  context,
  execFn = execTmux,
  syncExecFn = execFn === execTmux ? execFileSync : null,
) {
  // rootOf maps a subagent session to the session the user opened.
  const rootOf = (sessionID) => {
    try {
      return context.data.session.root(sessionID) || sessionID;
    } catch {
      return sessionID;
    }
  };

  // ownsSession: is this session (or the session it belongs to) the one this
  // client is showing? Read fresh on every event, since the user switches
  // sessions at will. Only the route counts. Tabs are not per pane: OpenCode
  // syncs them across every client and keeps old sessions in them, and on
  // connect the server replays pending prompts for those old sessions, so
  // counting tabs made every pane claim every session (checked with two
  // panes on one service, OpenCode 2.2.6).
  const ownsSession = (sessionID) => {
    if (!sessionID) {
      return false;
    }
    try {
      const route = context.ui.router.current();
      return (
        route?.type === "session" &&
        rootOf(route.sessionID) === rootOf(sessionID)
      );
    } catch {
      // A context that cannot answer owns nothing: writing another pane's
      // state into this one is worse than a missing dot.
      return false;
    }
  };

  const isRootSession = (sessionID) => rootOf(sessionID) === sessionID;

  // writeKind stamps this pane's coder identity alone - ADR-0055's "Agent
  // starts" row. Called once below, when the client loads the plugin, before
  // any state has ever been written.
  const writeKind = async () => {
    const pane = process.env.TMUX_PANE;
    if (!pane) {
      return;
    }
    try {
      await execFn("tmux", [
        "set-option",
        "-p",
        "-t",
        pane,
        "@dg_agent_kind",
        "opencode",
      ]);
    } catch {
      // Swallow: same rule as every other write in this file.
    }
  };

  // writeState is the choke point for the pane-level, authoritative write: it
  // is where the "no TMUX_PANE" no-op and the "swallow any failure" rule both
  // live, so no handler can forget either one.
  //
  // ADR-0055: every state write also stamps @dg_agent_kind, folded into this
  // SAME exec call via a literal ";" argv element - tmux's own command
  // separator, understood identically whether it arrives via a shell or, as
  // here, via execFile's array argv (no shell is ever involved) - rather
  // than a second process spawn. Kept fresh on every write so it survives
  // @dg_agent_state being cleared by an attach/focus/ack.
  const writeState = async (value) => {
    const pane = process.env.TMUX_PANE;
    if (!pane) {
      return;
    }
    try {
      await execFn("tmux", [
        "set-option",
        "-p",
        "-t",
        pane,
        "@dg_agent_state",
        value,
        ";",
        "set-option",
        "-p",
        "-t",
        pane,
        "@dg_agent_kind",
        "opencode",
      ]);
    } catch {
      // Swallow: a failed tmux write must never crash the OpenCode client.
    }
  };

  // mirrorWindowState sets (value a string) or clears (value === null) the
  // window-level display-only mirror. Targets the window via `-w -t <pane>`:
  // tmux resolves a pane target to its owning window when -w is given, so no
  // separate window-name lookup is needed. A DIFFERENT option name than
  // @dg_agent_state deliberately - see the header comment / ADR-0005 for why
  // reusing the pane-level name would corrupt reads for other panes in the
  // same window. Failure here is independent of writeState's.
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
      await execFn("tmux", args);
    } catch {
      // Swallow: same rule as writeState.
    }
  };

  // playNotifySound is ADR-0009's audible signal: fires for the same
  // idle/blocked/error "wants you" set as the window mirror; `busy` never
  // calls it. The two gate reads are awaited because they're cheap tmux
  // queries and the decision needs their result; the player itself is NOT
  // awaited (see firePlayer) so a slow or hanging sound never delays anything.
  const playNotifySound = async (state) => {
    const pane = process.env.TMUX_PANE;
    if (!pane) {
      return;
    }

    // Opt-in, off by default, and checked FIRST so the common (off) case
    // costs exactly one tmux call before returning.
    const soundOn = await readTmux(execFn, [
      "show-option",
      "-gqv",
      "@dg_notify_sound",
    ]);
    if (soundOn !== "on") {
      return;
    }

    // Same predicate window-status-format in configs/tmux/tmux.conf.tmpl
    // already uses to flag an unattended window, so the audible and visual
    // signals can never disagree about whether you've seen this.
    const activeClients = await readTmux(execFn, [
      "display-message",
      "-p",
      "-t",
      pane,
      "#{window_active_clients}",
    ]);
    if (activeClients !== "0") {
      return;
    }

    const files = SOUND_FILES[state];
    if (!files) {
      return;
    }

    firePlayer(execFn, pane, files).catch(() => {
      // Swallow: see firePlayer's own comment for the full tolerance.
    });
  };

  // wantsYou records one of the three "wants you" states: pane state, window
  // mirror, and the sound - always together, in that order.
  const wantsYou = async (state) => {
    await writeState(state);
    await mirrorWindowState(state);
    await playNotifySound(state);
  };

  // recomputeWindowMirror applies the SAME rule the pane-focus-in/out hooks
  // use (configs/tmux/tmux.conf.tmpl's "ackAgentState") to the window
  // mirror: unset it only if no pane in this window still holds a
  // WANTS_YOU_STATES value. Callers first change this pane's own state, so
  // it can't hold the mirror open by itself.
  const recomputeWindowMirror = async (pane) => {
    const remaining = await readTmux(execFn, [
      "list-panes",
      "-t",
      pane,
      "-F",
      "#{@dg_agent_state}",
    ]);
    const stillWantsYou = (remaining ?? "")
      .split("\n")
      .map((s) => s.trim())
      .some((s) => WANTS_YOU_STATES.includes(s));
    if (!stillWantsYou) {
      await mirrorWindowState(null);
    }
  };

  // endAgent is ADR-0055's "Agent ends" row: unsets both options on this pane
  // in one folded call, then recomputes the window mirror. It runs from the
  // plugin's cleanup, which OpenCode 2 calls when the client quits.
  const endAgent = async () => {
    const pane = process.env.TMUX_PANE;
    if (!pane) {
      return;
    }
    try {
      await execFn("tmux", [
        "set-option",
        "-p",
        "-u",
        "-t",
        pane,
        "@dg_agent_state",
        ";",
        "set-option",
        "-p",
        "-u",
        "-t",
        pane,
        "@dg_agent_kind",
      ]);
    } catch {
      // Swallow: same rule as every other write in this file.
    }
    await recomputeWindowMirror(pane);
  };

  // resumeAgent runs when a tool finishes or a prompt is answered: the turn
  // is still running. It is the one signal after a permission prompt is
  // answered, so without it a pane whose `blocked` was cleared by looking at
  // it would read as idle for the rest of the turn. Claude Code's
  // PostToolUse `agent-state.sh resume` is the same rule. Reads first, so the
  // common case (already busy) is one tmux call.
  const resumeAgent = async () => {
    const pane = process.env.TMUX_PANE;
    if (!pane) {
      return;
    }
    const current = await readTmux(execFn, [
      "display-message",
      "-p",
      "-t",
      pane,
      "#{@dg_agent_state}",
    ]);
    if (current === "busy") {
      return;
    }
    await writeState("busy");
    await recomputeWindowMirror(pane);
  };

  // Each handler returns its promise so tests can await it; OpenCode ignores
  // the return value of an event handler.
  const sessionOf = (event) => event?.data?.sessionID;
  const handlers = {
    "session.execution.started": async (event) => {
      const id = sessionOf(event);
      if (ownsSession(id) && isRootSession(id)) {
        await writeState("busy");
        await mirrorWindowState(null);
      }
    },
    "session.execution.succeeded": async (event) => {
      const id = sessionOf(event);
      if (ownsSession(id) && isRootSession(id)) {
        await wantsYou("idle");
      }
    },
    "session.execution.interrupted": async (event) => {
      const id = sessionOf(event);
      if (ownsSession(id) && isRootSession(id)) {
        await wantsYou("idle");
      }
    },
    "session.execution.failed": async (event) => {
      const id = sessionOf(event);
      if (ownsSession(id) && isRootSession(id)) {
        await wantsYou("error");
      }
    },
    "permission.asked": async (event) => {
      if (ownsSession(sessionOf(event))) {
        await wantsYou("blocked");
      }
    },
    "form.created": async (event) => {
      if (ownsSession(sessionOf(event))) {
        await wantsYou("blocked");
      }
    },
  };
  // A reply or a finished tool only means "still working" while the turn is
  // still running. A rejected permission ends the turn, and its
  // permission.replied can arrive after session.execution.interrupted; without
  // this check it would flip a finished pane back to busy.
  const stillRunning = (sessionID) => {
    try {
      return context.data.session.status(rootOf(sessionID)) === "running";
    } catch {
      return false;
    }
  };
  for (const type of [
    "permission.replied",
    "form.replied",
    "session.tool.success",
    "session.tool.failed",
  ]) {
    handlers[type] = async (event) => {
      const id = sessionOf(event);
      if (ownsSession(id) && stillRunning(id)) {
        await resumeAgent();
      }
    };
  }

  // Handlers run one at a time, in the order events arrive. Each one awaits
  // tmux calls, so run concurrently a slow one finishes last and wins: a
  // tool.failed's resume (read, then write busy) used to land after the
  // execution.interrupted that followed it had written idle, leaving a
  // finished pane on busy. Queued, every check also sees the state left by
  // the events before it. A failing handler never stalls the queue.
  let queue = Promise.resolve();
  const enqueue = (handler) => (event) => {
    queue = queue.then(() => handler(event)).catch(() => {});
    return queue;
  };

  enqueue(writeKind)();
  if (syncExecFn) {
    process.on("exit", () => exitFallbackSync(syncExecFn));
  }

  const unsubscribe = Object.entries(handlers).map(([type, handler]) =>
    context.data.on(type, enqueue(handler)),
  );

  const cleanup = async () => {
    for (const off of unsubscribe) {
      try {
        off();
      } catch {
        // Swallow: the client is shutting down either way.
      }
    }
    // Behind any write still in the queue, so nothing re-sets the pane after
    // it was cleared.
    await enqueue(endAgent)();
  };
  // settled resolves once every queued write so far has finished (tests).
  return { cleanup, settled: () => queue };
}

export default {
  id: "devgeta.notify",
  setup(context) {
    // Outside tmux there is no pane to report into: load nothing.
    if (!process.env.TMUX_PANE) {
      return;
    }
    return createNotify(context).cleanup;
  },
};
