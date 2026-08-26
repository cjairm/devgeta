// Behavioral test for configs/opencode/plugin/output-budget.js — the
// OpenCode-side mirror of configs/claude/output-budget.sh (see
// output_budget_test.go, package main, for the bash hook's equivalent
// end-to-end test).
//
// This file is deliberately NOT under configs/opencode/plugin/ — see
// task-redirect.test.mjs's header comment for why (that directory is copied
// byte-for-byte to the user's OpenCode plugin dir).
//
// Run with: node --test output-budget.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, mkdirSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { OutputBudget } from "./configs/opencode/plugin/output-budget.js";

function defaultRules() {
  return [
    { name: "go-test", match: ["go", "test"], head: 30, tail: 120 },
    { name: "npm-test", match: ["npm", "test"], head: 30, tail: 120 },
    { name: "npm-run", match: ["npm", "run"], head: 30, tail: 100 },
    { name: "make", match: ["make"], head: 20, tail: 100 },
  ];
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
    rules: defaultRules(),
    ...overrides,
  };
  writeFileSync(join(dir, "agent-runtime.json"), JSON.stringify(sidecar));
  return sidecar;
}

function writeMalformedSidecar(homeDir, raw) {
  const dir = join(homeDir, ".config", "devgeta");
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "agent-runtime.json"), raw);
}

async function runHook(command, env, extraArgs = {}) {
  const previous = {};
  for (const [key, value] of Object.entries(env)) {
    previous[key] = process.env[key];
    if (value === undefined) {
      delete process.env[key];
    } else {
      process.env[key] = value;
    }
  }
  try {
    const plugin = await OutputBudget({});
    const hook = plugin["tool.execute.before"];
    const output = { args: { command, ...extraArgs } };
    await hook({ tool: "bash" }, output);
    return output.args;
  } finally {
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = value;
      }
    }
  }
}

function newHome() {
  return mkdtempSync(join(tmpdir(), "output-budget-test-"));
}

function envFor(homeDir, extra = {}) {
  return {
    HOME: homeDir,
    XDG_CONFIG_HOME: join(homeDir, ".config"),
    DEVGETA_OUTPUT_BUDGET: undefined,
    ...extra,
  };
}

test("no sidecar allows the command unmodified", async () => {
  const home = newHome();
  const args = await runHook("go test ./...", envFor(home));
  assert.equal(args.command, "go test ./...");
});

test("gate false allows the command unmodified", async () => {
  const home = newHome();
  writeSidecar(home, { outputBudget: false });
  const args = await runHook("go test ./...", envFor(home));
  assert.equal(args.command, "go test ./...");
});

test("escape hatch allows the command unmodified", async () => {
  const home = newHome();
  writeSidecar(home);
  const args = await runHook(
    "go test ./...",
    envFor(home, { DEVGETA_OUTPUT_BUDGET: "off" }),
  );
  assert.equal(args.command, "go test ./...");
});

test("matched command rewrites to call the runner", async () => {
  const home = newHome();
  const sidecar = writeSidecar(home);
  const args = await runHook("go test ./...", envFor(home));
  assert.notEqual(args.command, "go test ./...");
  for (const piece of [
    sidecar.runner,
    "30",
    "120",
    "1984",
    "65536",
    "16777088",
    "go test ./...",
  ]) {
    assert.ok(
      args.command.includes(piece),
      `rewritten command missing ${JSON.stringify(piece)}: ${args.command}`,
    );
  }
});

test("unmatched command passes through unmodified", async () => {
  const home = newHome();
  writeSidecar(home);
  const args = await runHook("ls -la", envFor(home));
  assert.equal(args.command, "ls -la");
});

test("precedence: npm test selects npm-test, not npm-run", async () => {
  const home = newHome();
  writeSidecar(home);
  const args = await runHook("npm test", envFor(home));
  assert.ok(args.command.includes(" 30 120 "), args.command);
});

test("env-prefixed commands still match", async () => {
  const home = newHome();
  writeSidecar(home);
  for (const cmd of ["CI=1 go test ./...", "env CI=1 go test ./..."]) {
    const args = await runHook(cmd, envFor(home));
    assert.notEqual(args.command, cmd, `expected ${cmd} to match go-test`);
  }
});

test("quoted executable refuses to match", async () => {
  const home = newHome();
  writeSidecar(home);
  const cmd = `"/opt/my go/bin/go" test`;
  const args = await runHook(cmd, envFor(home));
  assert.equal(args.command, cmd);
});

test("quoted assignment refuses to match", async () => {
  const home = newHome();
  writeSidecar(home);
  const cmd = `FOO="a b" go test`;
  const args = await runHook(cmd, envFor(home));
  assert.equal(args.command, cmd);
});

test("metacharacters in arguments do not block a match", async () => {
  const home = newHome();
  writeSidecar(home);
  for (const cmd of [`make -j$(nproc)`, `npm test -- --grep "foo bar"`]) {
    const args = await runHook(cmd, envFor(home));
    assert.notEqual(args.command, cmd, `expected ${cmd} to still match`);
  }
});

test("not-stripped prefixes do not match", async () => {
  const home = newHome();
  writeSidecar(home);
  for (const cmd of ["command go test", "timeout 60 make"]) {
    const args = await runHook(cmd, envFor(home));
    assert.equal(args.command, cmd);
  }
});

test("other output.args fields are untouched by a rewrite", async () => {
  const home = newHome();
  writeSidecar(home);
  const args = await runHook("go test ./...", envFor(home), {
    description: "run the suite",
    timeout: 60000,
  });
  assert.equal(args.description, "run the suite");
  assert.equal(args.timeout, 60000);
});

test("degenerate sidecar cases all pass through unmodified", async () => {
  const cases = {
    "unreadable/malformed JSON": (home) =>
      writeMalformedSidecar(home, "{not valid json"),
    "outputBudget key missing": (home) =>
      writeMalformedSidecar(
        home,
        JSON.stringify({
          runner: "/bin/true",
          lineContentLimit: 100,
          maxTotalBytes: 100,
          captureContentLimit: 100,
          rules: [],
        }),
      ),
    "outputBudget wrong type": (home) =>
      writeSidecar(home, { outputBudget: "yes" }),
    "runner names a nonexistent path": (home) =>
      writeSidecar(home, { runner: "/no/such/file/exists/anywhere" }),
    "limit violates the width contract (16 digits)": (home) =>
      writeSidecar(home, { captureContentLimit: "1000000000000000" }),
    "rules is not an array": (home) =>
      writeSidecar(home, { rules: "not-an-array" }),
    "one rule entry is malformed": (home) =>
      writeSidecar(home, {
        rules: [
          ...defaultRules(),
          { name: "bad", match: [], head: 1, tail: 2 },
        ],
      }),
  };
  for (const [name, setup] of Object.entries(cases)) {
    await test(name, async () => {
      const home = newHome();
      setup(home);
      const args = await runHook("go test ./...", envFor(home));
      assert.equal(args.command, "go test ./...");
    });
  }
});

// The OpenCode mirror of the compound-command security guard in
// output_budget_test.go. Matching scans every segment, but the rewrite
// replaces the WHOLE command string, so one benign matching segment would
// otherwise wrap — and textually launder — everything beside it. Capping
// output is never a reason to change what a command is allowed to do.
test("compound commands are left exactly as they came in", async () => {
  const home = newHome();
  writeSidecar(home);

  for (const command of [
    "rm -rf /tmp/whatever && go test ./...",
    "go test ./... && rm -rf /tmp/whatever",
    "cd somewhere; go test ./...",
    "go test ./... || echo failed",
    "go test ./... | tee out.log",
  ]) {
    const args = await runHook(command, envFor(home));
    assert.equal(
      args.command,
      command,
      `compound command was rewritten: ${command}`,
    );
  }
});

// ...and the guard must not become a blanket refusal to do the job.
test("single-segment commands are still rewritten", async () => {
  const home = newHome();
  writeSidecar(home);

  for (const command of ["go test ./...", "go test ./... -run TestFoo -v", "make"]) {
    const args = await runHook(command, envFor(home));
    assert.notEqual(args.command, command, `not rewritten: ${command}`);
    assert.ok(
      args.command.includes(command),
      `rewrite should wrap the original: ${args.command}`,
    );
  }
});
