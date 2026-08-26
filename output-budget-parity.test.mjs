// The JS half of the shared parity suite (guide §10: "drive the parity
// groups from one shared case table"). Reads the SAME
// testdata/output-budget-cases.json the Go suite
// (output_budget_parity_test.go) reads — see that file's
// TestOutputBudgetCasesRulesMatchTheGoSource for the half of this contract
// that keeps the fixture in sync with baseapp.DefaultOutputBudgetRules.
//
// Run with: node --test output-budget-parity.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  readFileSync,
  mkdtempSync,
  writeFileSync,
  mkdirSync,
  chmodSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { OutputBudget } from "./configs/opencode/plugin/output-budget.js";

const cases = JSON.parse(
  readFileSync("./testdata/output-budget-cases.json", "utf8"),
);

function newHome() {
  return mkdtempSync(join(tmpdir(), "output-budget-parity-test-"));
}

function writeRunnerStub(homeDir) {
  const runnerPath = join(homeDir, "runner.sh");
  writeFileSync(runnerPath, "#!/usr/bin/env bash\n");
  chmodSync(runnerPath, 0o755);
  return runnerPath;
}

function writeSidecar(homeDir, overrides = {}) {
  const dir = join(homeDir, ".config", "devgeta");
  mkdirSync(dir, { recursive: true });
  const sidecar = {
    outputBudget: true,
    runner: writeRunnerStub(homeDir),
    lineContentLimit: 1984,
    maxTotalBytes: 65536,
    captureContentLimit: 16777088,
    rules: cases.rules.map(({ name, match, head, tail }) => ({
      name,
      match,
      head,
      tail,
    })),
    ...overrides,
  };
  writeFileSync(join(dir, "agent-runtime.json"), JSON.stringify(sidecar));
  return sidecar;
}

async function runHook(command, homeDir) {
  const previous = {
    HOME: process.env.HOME,
    XDG_CONFIG_HOME: process.env.XDG_CONFIG_HOME,
  };
  process.env.HOME = homeDir;
  process.env.XDG_CONFIG_HOME = join(homeDir, ".config");
  try {
    const plugin = await OutputBudget({});
    const hook = plugin["tool.execute.before"];
    const output = { args: { command } };
    await hook({ tool: "bash" }, output);
    return output.args.command;
  } finally {
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
}

test("rule-decision parity over every built-in rule", async () => {
  const home = newHome();
  writeSidecar(home);

  for (const rule of cases.rules) {
    await test(rule.name, async () => {
      const matched = await runHook(rule.matchingCommand, home);
      assert.notEqual(
        matched,
        rule.matchingCommand,
        `expected ${JSON.stringify(rule.matchingCommand)} to match rule ${rule.name}`,
      );
      assert.ok(matched.includes(String(rule.head)), matched);
      assert.ok(matched.includes(String(rule.tail)), matched);

      const nearMiss = await runHook(rule.nearMissCommand, home);
      assert.equal(
        nearMiss,
        rule.nearMissCommand,
        `near-miss ${JSON.stringify(rule.nearMissCommand)} for rule ${rule.name} unexpectedly matched`,
      );
    });
  }
});

test("tokenization contract parity", async () => {
  const home = newHome();
  writeSidecar(home);

  for (const tc of cases.tokenizationCases) {
    await test(tc.description, async () => {
      const result = await runHook(tc.command, home);
      const matched = result !== tc.command;
      assert.equal(
        matched,
        tc.shouldMatch,
        `${JSON.stringify(tc.command)}: matched=${matched}, want ${tc.shouldMatch} (got ${JSON.stringify(result)})`,
      );
    });
  }
});

test("numeric case parity", async () => {
  for (const nc of cases.numericCases) {
    await test(nc.description, async () => {
      const home = newHome();
      const runnerPath = writeRunnerStub(home);
      const dir = join(home, ".config", "devgeta");
      mkdirSync(dir, { recursive: true });
      // Written as raw text, not via JSON.stringify, so the exact literal
      // from the shared fixture reaches JSON.parse unmodified — the same
      // reason the Go side builds this sidecar by string formatting rather
      // than json.Marshal.
      const raw =
        `{"outputBudget": true, "runner": ${JSON.stringify(runnerPath)}, ` +
        `"lineContentLimit": ${nc.value}, "maxTotalBytes": 65536, ` +
        `"captureContentLimit": 16777088, "rules": [{"name":"go-test","match":["go","test"],"head":30,"tail":120}]}`;
      writeFileSync(join(dir, "agent-runtime.json"), raw);

      const result = await runHook("go test ./...", home);
      const matched = result !== "go test ./...";
      assert.equal(
        matched,
        nc.valid,
        `value ${JSON.stringify(nc.value)}: matched=${matched}, want ${nc.valid} (got ${JSON.stringify(result)})`,
      );
    });
  }
});

// The OpenCode half of the rtk-composition case (cycle doc Step 6). Unlike
// Claude Code, OpenCode's `tool.execute.before` plugins run in SEQUENCE and
// mutate a shared object (guide §2.3) — a plugin registered after rtk's
// genuinely sees rtk's already-rewritten command. Simulated here with a
// stand-in "rtk-like" hook rather than the real rtk binary, since this
// dev environment has no rtk install to depend on; what's under test is
// OpenCode's plugin-chaining behavior itself, not rtk's own logic.
test("registering after an rtk-like plugin sees its rewrite (real chaining, unlike Claude Code)", async () => {
  const home = newHome();
  writeSidecar(home);

  // Stands in for rtk expanding a shorthand into the real invocation —
  // output-budget has no "npm-t" rule, so it can only ever match this
  // command if it sees the EXPANDED form, which is exactly what chaining
  // is supposed to guarantee.
  const rtkLike = {
    "tool.execute.before": async (input, output) => {
      if (output?.args?.command === "npm t") {
        output.args.command = "npm test";
      }
    },
  };

  const plugin = await OutputBudget({});
  const outputBudgetHook = plugin["tool.execute.before"];

  const previous = {
    HOME: process.env.HOME,
    XDG_CONFIG_HOME: process.env.XDG_CONFIG_HOME,
  };
  process.env.HOME = home;
  process.env.XDG_CONFIG_HOME = join(home, ".config");
  try {
    // Control: output-budget alone, given the ORIGINAL shorthand, does not
    // match — proving the eventual match below comes from seeing rtk's
    // rewrite, not from some other rule matching "npm t" on its own.
    const unchained = { args: { command: "npm t" } };
    await outputBudgetHook({ tool: "bash" }, unchained);
    assert.equal(unchained.args.command, "npm t");

    // Chained: rtk-like runs first (registration order — mirroring
    // "register after rtk's block" in opencode.json), output-budget second,
    // on the SAME output object.
    const chained = { args: { command: "npm t" } };
    await rtkLike["tool.execute.before"]({ tool: "bash" }, chained);
    assert.equal(chained.args.command, "npm test");

    await outputBudgetHook({ tool: "bash" }, chained);
    assert.notEqual(
      chained.args.command,
      "npm test",
      "output-budget should have rewritten the post-rtk command to call the runner",
    );
    assert.ok(chained.args.command.includes("npm test"), chained.args.command);
  } finally {
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
});
