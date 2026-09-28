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
// behaviourally matched with the bash counterpart, configs/claude/agent-state.sh.
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
// OpenCode's real invocation (`Notify(ctx)`, one argument) is unaffected:
// `execFn` defaults to `execTmux`, the real execFile-based implementation
// below — despite the name, it now runs whatever `cmd` it's given (tmux or a
// sound player), not only tmux; the name is kept because every actual call
// site but the player ones targets tmux. notify.test.mjs passes a stub in
// place of the second argument in every test that calls a handler, so no
// test ever spawns a real tmux or player process.

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
// Empirically confirmed NOT reliable (Step 0 of
// docs/plans/cycles/2026-09-28-ws-agents-section.md, recorded in
// docs/decisions/ADR-0055-an-agent-says-what-it-is.md): a real opencode TUI's
// own double-ctrl-c quit gesture, and a bare SIGTERM, both exit the process
// WITHOUT any `exit` listener ever running. It is kept anyway for whatever
// OTHER termination path does let it run (an uncaught exception, a future
// OpenCode version's own signal handling) - the shell-name backstop in
// internal/apps/tmux is what actually keeps the agents list correct
// regardless of whether this ever fires. Because of that, this deliberately
// skips the window-mirror recompute endAgent does on the async path: it's
// already established as non-load-bearing, and recomputing it here would
// need a second synchronous call (a list-panes read) for a path that may
// never run at all.
export function exitFallbackSync(syncExecFn = execFileSync) {
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

// Notify's third parameter, syncExecFn, defaults to a REAL synchronous exec
// implementation only when execFn ITSELF was left at its real default
// (execTmux) - i.e. only when the caller passed at most one argument, the
// exact shape OpenCode's real plugin loader uses (`Notify(ctx)`). Every test
// in this file passes an explicit stub execFn, so this expression evaluates
// to `null` for all of them, and Notify never touches `process.on("exit")`
// during a test run - see ADR-0055 and the "Notify with a stubbed execFn
// never registers..." test in notify.test.mjs for why that matters: a real
// default here would let running the test SUITE itself register a live
// exit handler capable of firing a genuine tmux call against whatever real
// pane the test process happens to be running in.
export const Notify = async (
  ctx = {},
  execFn = execTmux,
  syncExecFn = execFn === execTmux ? execFileSync : null,
) => {
  // writeKind stamps this pane's coder identity alone - ADR-0055's "Agent
  // starts" row. Called once below, right after this factory is invoked
  // (OpenCode calls Notify(ctx) exactly once per session), before any state
  // has ever been written - a fresh agent has no state yet, so there is
  // nothing to fold this into.
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
      await execFn("tmux", args);
    } catch {
      // Swallow: same rule as writeState.
    }
  };

  // playNotifySound is ADR-0009's audible signal: fires for the same
  // idle/blocked/error "wants you" set as the window mirror, `busy` is never
  // called with it. Independently checks TMUX_PANE itself (same pattern as
  // writeState/mirrorWindowState) so the sound path stays a strict no-op
  // outside tmux. The two gate reads are awaited here because they're cheap
  // tmux queries (matching bash's synchronous `$(tmux ...)` checks) and the
  // decision needs their result; the actual player is NOT awaited (see
  // firePlayer's comment and the dispatch below) so a slow or hanging sound
  // never delays the caller.
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

    // Fire-and-forget: deliberately not awaited, and wrapped in a
    // last-resort .catch even though firePlayer already catches every step
    // internally — nothing here may ever throw or delay the caller.
    firePlayer(execFn, pane, files).catch(() => {
      // Swallow: see firePlayer's own comment for the full tolerance.
    });
  };

  // endAgent is ADR-0055's "Agent ends" row, on the async path: unsets both
  // options on this pane in one folded call (same `\;`-via-argv pattern as
  // writeState), then recomputes the window mirror with the SAME rule the
  // pane-focus-in/out hooks use (configs/tmux/tmux.conf.tmpl's
  // "ackAgentState"): unset it only if no pane left in this window still
  // holds a WANTS_YOU_STATES value. This pane's own state was already
  // cleared above, so it can't hold the mirror open by itself. Wired to the
  // `server.instance.disposed` event below - see ADR-0055's Consequences for
  // why this (and the exit-fallback) are best-effort, not load-bearing.
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
    const wantsYou = (remaining ?? "")
      .split("\n")
      .map((s) => s.trim())
      .some((s) => WANTS_YOU_STATES.includes(s));
    if (!wantsYou) {
      await mirrorWindowState(null);
    }
  };

  // resumeAgent runs after every tool (the `tool.execute.after` hook): a
  // tool finishing means the turn is still running. It is the one signal
  // after a permission prompt is answered, so without it a pane whose
  // `blocked` was cleared by looking at it read as idle for the rest of the
  // turn. Claude Code's PostToolUse `agent-state.sh resume` is the same
  // rule. Reads first, so the common case (already busy) is one tmux call.
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

  await writeKind();
  if (syncExecFn) {
    process.on("exit", () => exitFallbackSync(syncExecFn));
  }

  return {
    event: async ({ event } = {}) => {
      switch (event?.type) {
        case "session.idle":
          await writeState("idle");
          await mirrorWindowState("idle");
          await playNotifySound("idle");
          break;
        case "permission.updated":
          await writeState("blocked");
          await mirrorWindowState("blocked");
          await playNotifySound("blocked");
          break;
        case "session.error":
          await writeState("error");
          await mirrorWindowState("error");
          await playNotifySound("error");
          break;
        case "server.instance.disposed":
          await endAgent();
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
    "tool.execute.after": async () => {
      await resumeAgent();
    },
  };
};
