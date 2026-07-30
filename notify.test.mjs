// Behavioral test for configs/opencode/plugin/notify.js — the OpenCode-side
// half of the agent-activity signal governed by
// docs/decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md. See
// that ADR for the value table (session.idle -> idle, permission.updated ->
// blocked, session.error -> error, chat.message -> busy) and for why `busy`
// must be an explicit write rather than a cleared/absent value.
//
// Step 8 (docs/plans/cycles/2026-07-28-agent-activity-notifications.md) adds a
// second, additive write per handler: a window-level display-only mirror
// (@dg_window_agent_state) for the tmux status bar, alongside the existing
// pane-level write (@dg_agent_state).
//
// Step 11 (docs/decisions/ADR-0009-audible-agent-notifications.md) adds a
// sound for the same idle/blocked/error states, gated on an opt-in tmux
// option and on the window being unattended. This is why the exec seam
// widened from `execFn(args: string[])` to `execFn(cmd: string, args:
// string[]) -> stdout`: the gate reads (`show-option`, `display-message`)
// need stdout, and the player (`afplay`/`paplay`) is a second binary — a
// separate seam for it would be exactly the "test mocks one, forgets the
// other" bug the plugin's one-seam design prevents. EVERY call in this file
// — the pane write, the window mirror, the gate reads, and the player —
// goes through this same single seam, so every stub below now records
// `[cmd, ...args]` per call instead of a bare argv array.
//
// This file is deliberately NOT under configs/opencode/plugin/: that
// directory is copied byte-for-byte to the user's OpenCode plugin dir
// (internal/apps/opencode/opencode.go's ForceConfigure step, via
// files.CopyDir) — a test file living there would ship to every installed
// machine. Keeping this test at the repo root (sibling to
// task-redirect.test.mjs) mirrors that test's placement without polluting
// the deployed plugin directory.
//
// Run with: node --test notify.test.mjs
// Uses only Node's built-in test runner and assert module (available since
// Node 18) — no new dependency, per CLAUDE.md's "prefer the standard
// library" rule applied to this repo's JS files. The real `tmux`/`afplay`/
// `paplay` binaries are never invoked: every test that calls a handler
// injects a stub exec function in place of the plugin's default
// execFile-based implementation (CLAUDE.md: "never execute real commands in
// tests"). The one test that omits the stub (the last one below) only
// checks that the returned hook object has the right shape — it never calls
// a handler, so it can never reach the real execTmux/execFileAsync path.
//
// The sound tests DO perform one real, local filesystem write: the terminal-
// bell fallback writes a byte to a path resolved from `#{pane_tty}` via
// `node:fs/promises.writeFile`, a separate mechanism from the exec seam (see
// notify.js's firePlayer). That test points the stubbed `#{pane_tty}` at a
// throwaway file under node:os's tmpdir(), created and removed by the test
// itself — never a real device, never a path under the user's home or repo
// (CLAUDE.md §4: tests must never read/write real user directories).

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { Notify } from "./configs/opencode/plugin/notify.js";

// withTmuxPane sets process.env.TMUX_PANE for the duration of fn and restores
// the previous value afterward (even if fn throws), the same env-save/restore
// discipline task-redirect.test.mjs's runHook uses for its env vars — so no
// test leaks TMUX_PANE state into the next one.
async function withTmuxPane(value, fn) {
  const previous = process.env.TMUX_PANE;
  if (value === undefined) {
    delete process.env.TMUX_PANE;
  } else {
    process.env.TMUX_PANE = value;
  }
  try {
    return await fn();
  } finally {
    if (previous === undefined) {
      delete process.env.TMUX_PANE;
    } else {
      process.env.TMUX_PANE = previous;
    }
  }
}

// makeExecStub returns a fake exec function that records every (cmd, args)
// call it received, instead of shelling out to a real tmux/afplay/paplay
// process. Each recorded entry is `[cmd, ...args]` — a full argv, cmd
// included — so assertions can compare against one flat array per call.
// `impl(cmd, args)`, if given, runs after recording and controls what the
// stub resolves/rejects with — used both to simulate a failure (a rejected
// promise, for the error-swallowing tests) and to answer gate reads like
// `show-option`/`display-message` with a specific stdout value (for the
// sound tests). Because this is the SINGLE seam every write, gate read, and
// player invocation goes through, one stub covers every call site in every
// test below.
function makeExecStub(impl) {
  const calls = [];
  const exec = async (cmd, args) => {
    calls.push([cmd, ...args]);
    if (impl) {
      return await impl(cmd, args);
    }
    return "";
  };
  exec.calls = calls;
  return exec;
}

const paneWrite = (pane, value) => [
  "tmux",
  "set-option",
  "-p",
  "-t",
  pane,
  "@dg_agent_state",
  value,
];
const mirrorSet = (pane, value) => [
  "tmux",
  "set-option",
  "-w",
  "-t",
  pane,
  "@dg_window_agent_state",
  value,
];
const mirrorClear = (pane) => [
  "tmux",
  "set-option",
  "-w",
  "-u",
  "-t",
  pane,
  "@dg_window_agent_state",
];
const showOption = () => ["tmux", "show-option", "-gqv", "@dg_notify_sound"];
const activeClientsCheck = (pane) => [
  "tmux",
  "display-message",
  "-p",
  "-t",
  pane,
  "#{window_active_clients}",
];
const paneTtyCheck = (pane) => [
  "tmux",
  "display-message",
  "-p",
  "-t",
  pane,
  "#{pane_tty}",
];
const afplayCall = (file) => ["afplay", file];
const paplayCall = (file) => ["paplay", file];

// SOUND_FILES mirrors notify.js's own table (idle/blocked/error), so the
// "distinct sound file per state" test can assert against the exact
// per-state path without hand-duplicating three literals inline.
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

// makeSoundStub builds on makeExecStub for the sound-path tests: it answers
// the two gate reads (show-option, display-message) with caller-supplied
// values, answers a pane_tty lookup with `paneTty` (or empty, meaning
// unresolved), and applies `playerBehavior` (a map of "afplay"/"paplay" to
// one of three strings) to decide how each player invocation responds:
//
//   - "resolve" (or omitted): the call succeeds, same as the plain stub.
//   - "missing": the binary itself does not exist — rejects with an Error
//     whose `.code` is `"ENOENT"`, exactly what Node's execFile/child_process
//     attaches when the executable can't be found. This is the ONLY shape
//     that should make firePlayer fall through to the next candidate,
//     mirroring bash's `command -v` existence check.
//   - "fail": the binary exists but the invocation itself fails at runtime
//     (bad audio device, missing sound file, killed, ...) — rejects with an
//     Error that has no `.code` (or a different one), the shape a real
//     execFile failure-to-run-cleanly would have. This must NOT trigger a
//     fallthrough: bash could never observe this either, since by the time
//     the player is chosen it's already fired-and-forgotten.
//
// Every other call (the pane write, the window mirror) resolves normally,
// same as the plain stub, so these tests can still assert the FULL ordered
// call sequence per the brief's "every seam call's argv is asserted"
// requirement.
function makeSoundStub({
  soundOn = "on",
  activeClients = "0",
  paneTty = "/tmp/does-not-matter-tty",
  playerBehavior = {},
} = {}) {
  return makeExecStub(async (cmd, args) => {
    if (cmd === "tmux" && args[0] === "show-option") {
      return soundOn;
    }
    if (cmd === "tmux" && args[0] === "display-message") {
      const field = args[args.length - 1];
      if (field === "#{window_active_clients}") {
        return activeClients;
      }
      if (field === "#{pane_tty}") {
        return paneTty;
      }
    }
    if (cmd === "afplay" || cmd === "paplay") {
      if (playerBehavior[cmd] === "missing") {
        const err = new Error(`${cmd}: no such file or directory`);
        err.code = "ENOENT";
        throw err;
      }
      if (playerBehavior[cmd] === "fail") {
        throw new Error(`${cmd}: exited with a runtime failure`);
      }
      return "";
    }
    return "";
  });
}

// neverResolves returns a promise that never settles within a test's
// lifetime, for finding 7's regression test: it pins the property that the
// handler resolves without waiting for the player to finish, so an
// accidental future `await` on the player call would hang the test (and, in
// production, would block every hook invocation on however long the sound
// takes to play - the entire point of "detached and backgrounded").
function neverResolves() {
  return new Promise(() => {});
}

test("session.idle writes idle to the pane and sets the window mirror", async () => {
  await withTmuxPane("%3", async () => {
    // The plain stub answers show-option with "" (not "on"), so the sound
    // gate is checked (one extra tmux call, asserted below) and then exits
    // before any player is invoked — off by default.
    const exec = makeExecStub();
    const plugin = await Notify({}, exec);
    await plugin.event({
      event: { type: "session.idle", properties: { sessionID: "s1" } },
    });
    assert.deepEqual(exec.calls, [
      paneWrite("%3", "idle"),
      mirrorSet("%3", "idle"),
      showOption(),
    ]);
  });
});

test("permission.updated writes blocked to the pane and sets the window mirror", async () => {
  await withTmuxPane("%7", async () => {
    const exec = makeExecStub();
    const plugin = await Notify({}, exec);
    await plugin.event({
      event: {
        type: "permission.updated",
        properties: { id: "p1", sessionID: "s1" },
      },
    });
    assert.deepEqual(exec.calls, [
      paneWrite("%7", "blocked"),
      mirrorSet("%7", "blocked"),
      showOption(),
    ]);
  });
});

test("session.error writes error to the pane and sets the window mirror", async () => {
  await withTmuxPane("%1", async () => {
    const exec = makeExecStub();
    const plugin = await Notify({}, exec);
    await plugin.event({
      event: { type: "session.error", properties: {} },
    });
    assert.deepEqual(exec.calls, [
      paneWrite("%1", "error"),
      mirrorSet("%1", "error"),
      showOption(),
    ]);
  });
});

test("chat.message writes busy to the pane and CLEARS the window mirror", async () => {
  await withTmuxPane("%9", async () => {
    const exec = makeExecStub();
    const plugin = await Notify({}, exec);
    await plugin["chat.message"](
      { sessionID: "s1" },
      { message: {}, parts: [] },
    );
    // busy clears the mirror rather than writing "busy" to it - the
    // status-bar flag must disappear the instant a new turn begins.
    assert.deepEqual(exec.calls, [paneWrite("%9", "busy"), mirrorClear("%9")]);
  });
});

test("an unrecognized event type is a no-op: no write, no throw", async () => {
  await withTmuxPane("%2", async () => {
    const exec = makeExecStub();
    const plugin = await Notify({}, exec);
    await assert.doesNotReject(() =>
      plugin.event({
        event: { type: "session.compacted", properties: {} },
      }),
    );
    assert.deepEqual(exec.calls, []);
  });
});

test("TMUX_PANE unset: no error, no write attempted (pane or mirror)", async () => {
  await withTmuxPane(undefined, async () => {
    const exec = makeExecStub();
    const plugin = await Notify({}, exec);

    await assert.doesNotReject(() =>
      plugin.event({
        event: { type: "session.idle", properties: { sessionID: "s1" } },
      }),
    );
    assert.deepEqual(exec.calls, []);

    await assert.doesNotReject(() =>
      plugin["chat.message"]({ sessionID: "s1" }, { message: {}, parts: [] }),
    );
    assert.deepEqual(exec.calls, []);
  });
});

test("TMUX_PANE empty string: no error, no write attempted (pane or mirror)", async () => {
  await withTmuxPane("", async () => {
    const exec = makeExecStub();
    const plugin = await Notify({}, exec);
    await assert.doesNotReject(() =>
      plugin.event({
        event: { type: "session.error", properties: {} },
      }),
    );
    assert.deepEqual(exec.calls, []);
  });
});

test("a rejected exec call is swallowed for BOTH the pane write and the mirror write independently (session.idle)", async () => {
  await withTmuxPane("%4", async () => {
    const exec = makeExecStub(async () => {
      throw new Error("tmux: no server running on /tmp/tmux-501/default");
    });
    const plugin = await Notify({}, exec);
    await assert.doesNotReject(() =>
      plugin.event({
        event: { type: "session.idle", properties: { sessionID: "s1" } },
      }),
    );
    // All three calls were attempted (and recorded) even though every call
    // threw: the pane write's failure did not prevent the mirror write from
    // being attempted, and the sound gate's own failed read (readTmux
    // swallows the rejection and reads as "not on") did not prevent it from
    // being attempted either. No failure propagated out of the handler.
    assert.deepEqual(exec.calls, [
      paneWrite("%4", "idle"),
      mirrorSet("%4", "idle"),
      showOption(),
    ]);
  });
});

test("a rejected exec call is swallowed for BOTH the pane write and the mirror clear independently (chat.message)", async () => {
  await withTmuxPane("%5", async () => {
    const exec = makeExecStub(async () => {
      throw new Error("ENOENT: tmux not found");
    });
    const plugin = await Notify({}, exec);
    await assert.doesNotReject(() =>
      plugin["chat.message"]({ sessionID: "s1" }, { message: {}, parts: [] }),
    );
    assert.deepEqual(exec.calls, [paneWrite("%5", "busy"), mirrorClear("%5")]);
  });
});

test("sound: silence when @dg_notify_sound is unset (empty stdout)", async () => {
  await withTmuxPane("%10", async () => {
    const exec = makeSoundStub({ soundOn: "" });
    const plugin = await Notify({}, exec);
    await plugin.event({
      event: { type: "session.idle", properties: {} },
    });
    assert.deepEqual(exec.calls, [
      paneWrite("%10", "idle"),
      mirrorSet("%10", "idle"),
      showOption(),
    ]);
  });
});

test("sound: silence when @dg_notify_sound is explicitly off", async () => {
  await withTmuxPane("%11", async () => {
    const exec = makeSoundStub({ soundOn: "off" });
    const plugin = await Notify({}, exec);
    await plugin.event({
      event: { type: "session.idle", properties: {} },
    });
    assert.deepEqual(exec.calls, [
      paneWrite("%11", "idle"),
      mirrorSet("%11", "idle"),
      showOption(),
    ]);
  });
});

test("sound: silence when window_active_clients != 0 (window is attended)", async () => {
  await withTmuxPane("%12", async () => {
    const exec = makeSoundStub({ soundOn: "on", activeClients: "1" });
    const plugin = await Notify({}, exec);
    await plugin.event({
      event: { type: "session.idle", properties: {} },
    });
    // Both gate reads were attempted; the sound gate exits after the second
    // one because the window is attended, so no player call was ever made.
    assert.deepEqual(exec.calls, [
      paneWrite("%12", "idle"),
      mirrorSet("%12", "idle"),
      showOption(),
      activeClientsCheck("%12"),
    ]);
  });
});

test("sound: busy never dings, even with the option on and the window unattended", async () => {
  await withTmuxPane("%13", async () => {
    const exec = makeSoundStub({ soundOn: "on", activeClients: "0" });
    const plugin = await Notify({}, exec);
    await plugin["chat.message"](
      { sessionID: "s1" },
      { message: {}, parts: [] },
    );
    // busy's handler never calls playNotifySound at all - no gate read, no
    // player call, regardless of what the gate would have answered.
    assert.deepEqual(exec.calls, [
      paneWrite("%13", "busy"),
      mirrorClear("%13"),
    ]);
  });
});

test("sound: distinct sound file per state (idle, blocked, error) via afplay", async () => {
  for (const [eventType, state] of [
    ["session.idle", "idle"],
    ["permission.updated", "blocked"],
    ["session.error", "error"],
  ]) {
    await withTmuxPane("%20", async () => {
      const exec = makeSoundStub({ soundOn: "on", activeClients: "0" });
      const plugin = await Notify({}, exec);
      await plugin.event({ event: { type: eventType, properties: {} } });
      assert.deepEqual(exec.calls, [
        paneWrite("%20", state),
        mirrorSet("%20", state),
        showOption(),
        activeClientsCheck("%20"),
        afplayCall(SOUND_FILES[state].afplay),
      ]);
    });
  }
});

test("sound: falls through to paplay when afplay is missing (ENOENT)", async () => {
  await withTmuxPane("%21", async () => {
    const exec = makeSoundStub({
      soundOn: "on",
      activeClients: "0",
      playerBehavior: { afplay: "missing" },
    });
    const plugin = await Notify({}, exec);
    await plugin.event({
      event: { type: "session.idle", properties: {} },
    });
    assert.deepEqual(exec.calls, [
      paneWrite("%21", "idle"),
      mirrorSet("%21", "idle"),
      showOption(),
      activeClientsCheck("%21"),
      afplayCall(SOUND_FILES.idle.afplay),
      paplayCall(SOUND_FILES.idle.paplay),
    ]);
  });
});

test("sound: does NOT fall through to paplay when afplay EXISTS but fails at runtime (non-ENOENT rejection)", async () => {
  // This is the crux of the bash-parity fix: agent-state.sh picks a player
  // via `command -v` (existence only) and fires it detached, so a runtime
  // failure of the CHOSEN player (bad audio device, missing sound file,
  // etc.) is never observed and never falls back to the next candidate.
  // notify.js must reproduce that: only ENOENT (binary missing) may fall
  // through; any other rejection must stop right there, silently - no
  // paplay call, no bell.
  await withTmuxPane("%24", async () => {
    const exec = makeSoundStub({
      soundOn: "on",
      activeClients: "0",
      playerBehavior: { afplay: "fail" },
    });
    const plugin = await Notify({}, exec);
    await assert.doesNotReject(() =>
      plugin.event({
        event: { type: "session.idle", properties: {} },
      }),
    );
    assert.deepEqual(exec.calls, [
      paneWrite("%24", "idle"),
      mirrorSet("%24", "idle"),
      showOption(),
      activeClientsCheck("%24"),
      afplayCall(SOUND_FILES.idle.afplay),
    ]);
  });
});

test("sound: does NOT fall through to the bell when paplay EXISTS but fails at runtime, after afplay is missing", async () => {
  // Same bash-parity rule one level down the chain: afplay missing (ENOENT)
  // falls through to paplay as usual, but paplay existing-and-failing must
  // stop there rather than falling through to the terminal bell.
  await withTmuxPane("%25", async () => {
    const exec = makeSoundStub({
      soundOn: "on",
      activeClients: "0",
      playerBehavior: { afplay: "missing", paplay: "fail" },
    });
    const plugin = await Notify({}, exec);
    await assert.doesNotReject(() =>
      plugin.event({
        event: { type: "session.idle", properties: {} },
      }),
    );
    assert.deepEqual(exec.calls, [
      paneWrite("%25", "idle"),
      mirrorSet("%25", "idle"),
      showOption(),
      activeClientsCheck("%25"),
      afplayCall(SOUND_FILES.idle.afplay),
      paplayCall(SOUND_FILES.idle.paplay),
    ]);
  });
});

test("sound: still resolves without throwing when the player is missing entirely, and falls back to the terminal bell written to pane_tty", async () => {
  const dir = await mkdtemp(path.join(tmpdir(), "notify-test-"));
  const fakePaneTty = path.join(dir, "fake-pane-tty");
  try {
    await withTmuxPane("%22", async () => {
      const exec = makeSoundStub({
        soundOn: "on",
        activeClients: "0",
        paneTty: fakePaneTty,
        playerBehavior: { afplay: "missing", paplay: "missing" },
      });
      const plugin = await Notify({}, exec);

      await assert.doesNotReject(() =>
        plugin.event({
          event: { type: "session.idle", properties: {} },
        }),
      );

      // The bell write happens through a fire-and-forget promise chain (see
      // notify.js's playNotifySound/firePlayer): the handler resolves before
      // that chain necessarily finishes, so poll briefly for the file to
      // appear rather than asserting immediately after the await above.
      let content = "";
      for (let attempt = 0; attempt < 50; attempt += 1) {
        try {
          content = await readFile(fakePaneTty, "utf8");
          if (content.length > 0) {
            break;
          }
        } catch {
          // File not written yet: keep polling.
        }
        await delay(10);
      }
      assert.equal(content, "\x07");

      assert.deepEqual(exec.calls, [
        paneWrite("%22", "idle"),
        mirrorSet("%22", "idle"),
        showOption(),
        activeClientsCheck("%22"),
        afplayCall(SOUND_FILES.idle.afplay),
        paplayCall(SOUND_FILES.idle.paplay),
        paneTtyCheck("%22"),
      ]);
    });
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("sound: still resolves without throwing when pane_tty cannot be resolved (empty stdout)", async () => {
  await withTmuxPane("%23", async () => {
    const exec = makeSoundStub({
      soundOn: "on",
      activeClients: "0",
      paneTty: "",
      playerBehavior: { afplay: "missing", paplay: "missing" },
    });
    const plugin = await Notify({}, exec);
    await assert.doesNotReject(() =>
      plugin.event({
        event: { type: "session.error", properties: {} },
      }),
    );
  });
});

test("sound: the event handler resolves promptly even when the player promise never settles", async () => {
  // Regression guard for the "detached and backgrounded" contract
  // (firePlayer's header comment / ADR-0009): playNotifySound deliberately
  // does not await firePlayer's promise. Every other test's player stub
  // resolves or rejects immediately, so none of them would catch a future
  // edit that accidentally added an `await` in front of the `firePlayer(...)`
  // call in playNotifySound - that would silently make every hook
  // invocation block for however long the sound takes to play. This stub's
  // afplay call returns a promise that never settles, so if the handler
  // were ever changed to await it, this test would hang until its own
  // timeout instead of resolving promptly.
  await withTmuxPane("%26", async () => {
    const exec = makeExecStub(async (cmd, args) => {
      if (cmd === "tmux" && args[0] === "show-option") {
        return "on";
      }
      if (cmd === "tmux" && args[0] === "display-message") {
        return "0";
      }
      if (cmd === "afplay") {
        return neverResolves();
      }
      return "";
    });
    const plugin = await Notify({}, exec);

    const start = Date.now();
    await Promise.race([
      plugin.event({ event: { type: "session.idle", properties: {} } }),
      delay(1000).then(() => {
        throw new Error(
          "event handler did not resolve within 1000ms - it must not await " +
            "the player (see firePlayer's fire-and-forget contract)",
        );
      }),
    ]);
    const elapsed = Date.now() - start;
    assert.ok(
      elapsed < 500,
      `expected the handler to resolve promptly without waiting for the ` +
        `player; took ${elapsed}ms`,
    );
  });
});

test("Notify(ctx) with no injected exec still returns hooks (real invocation shape)", async () => {
  // OpenCode calls Notify(ctx) with exactly one argument; the exec parameter
  // must default so that shape keeps working. We don't exercise the real
  // execTmux path here (that would spawn a real process) — just confirm the
  // factory doesn't require the second argument. No handler is invoked, so
  // this test cannot reach the real tmux binary.
  const plugin = await Notify({ directory: process.cwd() });
  assert.equal(typeof plugin.event, "function");
  assert.equal(typeof plugin["chat.message"], "function");
});
