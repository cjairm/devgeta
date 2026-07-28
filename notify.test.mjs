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
// pane-level write (@dg_agent_state). Both go through the SAME injected exec
// seam (execFn(args: string[])) — there is exactly one seam in this plugin,
// not one per write, so a test can't forget to mock a second one and let a
// real `tmux` invocation slip through.
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
// library" rule applied to this repo's JS files. The real `tmux` binary is
// never invoked: every test that calls a handler injects a stub exec
// function in place of the plugin's default execFile-based implementation
// (CLAUDE.md: "never execute real commands in tests"). The one test that
// omits the stub (the last one below) only checks that the returned hook
// object has the right shape — it never calls a handler, so it can never
// reach the real execTmux/execFileAsync path.

import { test } from "node:test";
import assert from "node:assert/strict";
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

// makeExecStub returns a fake exec function that records every raw argv array
// it was called with, instead of shelling out to a real tmux process. `impl`,
// if given, runs after recording the call — used to simulate a tmux failure
// (a rejected promise) for the error-swallowing tests. Because this is the
// SINGLE seam both the pane write and the window mirror write/clear go
// through, one stub covers both call sites in every test below.
function makeExecStub(impl) {
  const calls = [];
  const exec = async (args) => {
    calls.push(args);
    if (impl) {
      await impl(args);
    }
  };
  exec.calls = calls;
  return exec;
}

const paneWrite = (pane, value) => [
  "set-option",
  "-p",
  "-t",
  pane,
  "@dg_agent_state",
  value,
];
const mirrorSet = (pane, value) => [
  "set-option",
  "-w",
  "-t",
  pane,
  "@dg_window_agent_state",
  value,
];
const mirrorClear = (pane) => [
  "set-option",
  "-w",
  "-u",
  "-t",
  pane,
  "@dg_window_agent_state",
];

test("session.idle writes idle to the pane and sets the window mirror", async () => {
  await withTmuxPane("%3", async () => {
    const exec = makeExecStub();
    const plugin = await Notify({}, exec);
    await plugin.event({
      event: { type: "session.idle", properties: { sessionID: "s1" } },
    });
    assert.deepEqual(exec.calls, [
      paneWrite("%3", "idle"),
      mirrorSet("%3", "idle"),
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
    // Both calls were attempted (and recorded) even though every call threw:
    // the pane write's failure did not prevent the mirror write from being
    // attempted, and neither failure propagated out of the handler.
    assert.deepEqual(exec.calls, [
      paneWrite("%4", "idle"),
      mirrorSet("%4", "idle"),
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
