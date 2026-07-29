// Behavioral test for configs/opencode/plugin/suppression-guard.js — the
// OpenCode-side mirror of configs/claude/suppression-guard.sh (see
// suppression_guard_test.go, package main, for the bash hook's equivalent
// end-to-end test).
//
// This file is deliberately NOT under configs/opencode/plugin/ — see
// task-redirect.test.mjs's header comment for why.
//
// Run with: node --test suppression-guard.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { SuppressionGuard } from "./configs/opencode/plugin/suppression-guard.js";

// This test file lives at the repo root, so its own directory is a devgeta
// repo (repo-root go.mod has module github.com/cjairm/devgeta) — same trick
// task-redirect.test.mjs uses for its release-gating tests.
const DEVGETA_DIR = dirname(fileURLToPath(import.meta.url));

async function runHook(tool, args, dir = DEVGETA_DIR, env = {}) {
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
    const plugin = await SuppressionGuard({ directory: dir });
    const hook = plugin["tool.execute.before"];
    try {
      await hook({ tool }, { args });
      return { denied: false, message: null };
    } catch (err) {
      return { denied: true, message: err.message };
    }
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

test("denies introduced suppression comments inside the devgeta repo", async () => {
  const cases = [
    [
      "edit",
      {
        filePath: "main.go",
        oldString: "func f() error {\n\treturn nil\n}",
        newString: "func f() error { //nolint:errcheck\n\treturn nil\n}",
      },
    ],
    [
      "edit",
      {
        filePath: "x.py",
        oldString: "import os",
        newString: "import os  # noqa",
      },
    ],
    [
      "edit",
      {
        filePath: "x.py",
        oldString: "x = 1",
        newString: "x = 1  # type: ignore",
      },
    ],
    [
      "edit",
      {
        filePath: "x.ts",
        oldString: "const x = 1;",
        newString: "// eslint-disable-next-line\nconst x = 1;",
      },
    ],
    [
      "edit",
      {
        filePath: "x.ts",
        oldString: "foo();",
        newString: "// @ts-ignore\nfoo();",
      },
    ],
    [
      "write",
      {
        filePath: "main.go",
        content: "package main\n//nolint:unused\nfunc f() {}\n",
      },
    ],
  ];
  for (const [tool, args] of cases) {
    const result = await runHook(tool, args);
    assert.equal(
      result.denied,
      true,
      `expected deny for ${JSON.stringify(args)}`,
    );
    assert.match(result.message, /DEVGETA_SKIP_SUPPRESSION_GUARD/);
  }
});

test("denies a second, new suppression when one of the same kind already existed (regression: presence vs. count check)", async () => {
  const result = await runHook("edit", {
    filePath: "main.go",
    oldString: "func f() { //nolint:errcheck\n\treturn\n}",
    newString: "func f() { //nolint:errcheck\n\t//nolint:unused\n\treturn\n}",
  });
  assert.equal(
    result.denied,
    true,
    `expected deny for a genuinely new suppression, got: ${result.message}`,
  );
});

test("allows a preexisting suppression left untouched by an edit", async () => {
  const result = await runHook("edit", {
    filePath: "main.go",
    oldString: "func f() { //nolint:errcheck\n\treturn\n}",
    newString: "func f() { //nolint:errcheck\n\treturn nil\n}",
  });
  assert.equal(result.denied, false, `expected allow, got: ${result.message}`);
});

test("allows ordinary edits in the devgeta repo", async () => {
  const result = await runHook("edit", {
    filePath: "main.go",
    oldString: "func f() {}",
    newString: "func f() { doStuff() }",
  });
  assert.equal(result.denied, false, `expected allow, got: ${result.message}`);
});

test("allows outside the devgeta repo, even for a genuine suppression", async () => {
  const noGoMod = mkdtempSync(join(tmpdir(), "suppression-guard-test-"));
  const otherModule = mkdtempSync(join(tmpdir(), "suppression-guard-test-"));
  writeFileSync(
    join(otherModule, "go.mod"),
    "module github.com/other/thing\n\ngo 1.25\n",
  );

  for (const dir of [noGoMod, otherModule]) {
    const result = await runHook(
      "edit",
      {
        filePath: "main.go",
        oldString: "func f() {}",
        newString: "func f() { //nolint:errcheck\n}",
      },
      dir,
    );
    assert.equal(
      result.denied,
      false,
      `expected allow outside devgeta repo, got: ${result.message}`,
    );
  }
});

test("bypass env var allows everything", async () => {
  const result = await runHook(
    "edit",
    {
      filePath: "main.go",
      oldString: "func f() {}",
      newString: "func f() { //nolint:errcheck\n}",
    },
    DEVGETA_DIR,
    { DEVGETA_SKIP_SUPPRESSION_GUARD: "1" },
  );
  assert.equal(
    result.denied,
    false,
    `expected bypass to allow, got: ${result.message}`,
  );
});

test("allows when tool is neither edit nor write", async () => {
  const result = await runHook("bash", { command: "//nolint stuff" });
  assert.equal(result.denied, false, `expected allow, got: ${result.message}`);
});
