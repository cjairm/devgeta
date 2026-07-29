// Behavioral test for configs/opencode/plugin/secret-guard.js — the
// OpenCode-side mirror of configs/claude/secret-guard.sh (see
// secret_guard_test.go, package main, for the bash hook's equivalent
// end-to-end test).
//
// This file is deliberately NOT under configs/opencode/plugin/ — see
// task-redirect.test.mjs's header comment for why (that directory is copied
// byte-for-byte to the user's OpenCode plugin dir).
//
// Run with: node --test secret-guard.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { execFileSync } from "node:child_process";
import { SecretGuard } from "./configs/opencode/plugin/secret-guard.js";

// initGitRepoWithStaged creates a fresh git repo in a temp dir, writes and
// stages each file in `files` (path -> content), and returns the repo dir.
// Real `git` is genuinely exercised here — same posture as
// task-redirect.test.mjs's release-gating tests reading a real go.mod.
function initGitRepoWithStaged(files) {
  const dir = mkdtempSync(join(tmpdir(), "secret-guard-test-"));
  const run = (...args) =>
    execFileSync("git", args, { cwd: dir, stdio: "ignore" });
  run("init", "-q");
  run("config", "user.email", "test@example.com");
  run("config", "user.name", "test");
  for (const [path, content] of Object.entries(files)) {
    const full = join(dir, path);
    mkdirSync(dirname(full), { recursive: true });
    writeFileSync(full, content);
    run("add", path);
  }
  return dir;
}

// initGitRepo creates a fresh, empty git repo (no staged files) — the
// "clean, unrelated" side of the -C target-resolution tests below.
function initGitRepo() {
  return initGitRepoWithStaged({});
}

// runHook drives the plugin exactly as OpenCode would: build the plugin,
// invoke its tool.execute.before hook with a bash tool call, and report
// whether it denied (threw) or allowed (returned normally).
async function runHook(command, dir, env = {}) {
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
    const plugin = await SecretGuard({ directory: dir });
    const hook = plugin["tool.execute.before"];
    try {
      await hook({ tool: "bash" }, { args: { command } });
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

test("allows non-commit commands even with a staged secret", async () => {
  const dir = initGitRepoWithStaged({ ".env": "SECRET=1" });
  for (const command of [
    "git status",
    "git add .env",
    "git diff --cached",
    "git log",
  ]) {
    const result = await runHook(command, dir);
    assert.equal(
      result.denied,
      false,
      `expected allow for ${command}, got: ${result.message}`,
    );
  }
});

test("denies sensitive staged filenames", async () => {
  const cases = [
    ".env",
    ".env.production",
    "certs/server.pem",
    "id_rsa",
    "id_ed25519",
    "cert.pfx",
  ];
  for (const path of cases) {
    const dir = initGitRepoWithStaged({ [path]: "secret-shaped-content" });
    const result = await runHook('git commit -m "add file"', dir);
    assert.equal(result.denied, true, `expected deny for staged ${path}`);
    assert.match(result.message, /DEVGETA_SKIP_SECRET_GUARD/);
  }
});

test("allows excluded filename shapes", async () => {
  const cases = [".env.example", ".env.sample", ".env.template", "id_rsa.pub"];
  for (const path of cases) {
    const dir = initGitRepoWithStaged({ [path]: "not actually secret" });
    const result = await runHook('git commit -m "add file"', dir);
    assert.equal(
      result.denied,
      false,
      `expected allow for staged ${path}, got: ${result.message}`,
    );
  }
});

test("denies secret-shaped content in added lines", async () => {
  const cases = [
    "-----BEGIN RSA PRIVATE KEY-----\nMIIBogIBAAJ...\n-----END RSA PRIVATE KEY-----\n",
    "AWS_ACCESS_KEY_ID=AKIAABCDEFGHIJKLMNOP\n",
    `TOKEN=ghp_${"a".repeat(36)}\n`,
    "SLACK_TOKEN=xoxb-1234567890-abcdefghij\n",
  ];
  for (const content of cases) {
    const dir = initGitRepoWithStaged({ "config.go": content });
    const result = await runHook('git commit -m "add config"', dir);
    assert.equal(
      result.denied,
      true,
      `expected deny for content ${JSON.stringify(content)}`,
    );
    assert.match(result.message, /DEVGETA_SKIP_SECRET_GUARD/);
  }
});

test("recognizes commit despite -c/--git-dir= global options (regression: bare anchor bypass)", async () => {
  const dir = initGitRepoWithStaged({ ".env": "SECRET=1" });
  const cases = [
    'git -c user.name=x commit -m "x"',
    'git --git-dir=/tmp/unrelated/.git commit -m "x"',
  ];
  for (const command of cases) {
    const result = await runHook(command, dir);
    assert.equal(
      result.denied,
      true,
      `expected deny for ${command}, got: ${result.message}`,
    );
  }
});

test("scans the -C target, not projectDir (regression: wrong-repo scan)", async () => {
  const secretDir = initGitRepoWithStaged({ ".env": "SECRET=1" });
  const cleanDir = initGitRepo();

  {
    const command = `git -C ${secretDir} commit -m "x"`;
    const result = await runHook(command, cleanDir);
    assert.equal(
      result.denied,
      true,
      `expected deny for -C targeting the secret repo from a clean cwd, got: ${result.message}`,
    );
  }
  {
    const command = `git -C ${cleanDir} commit -m "x"`;
    const result = await runHook(command, secretDir);
    assert.equal(
      result.denied,
      false,
      `expected allow for -C targeting a clean repo from the secret's own cwd, got: ${result.message}`,
    );
  }
});

test("scans the cd target, not projectDir (regression: found via live manual verification)", async () => {
  const secretDir = initGitRepoWithStaged({ ".env": "SECRET=1" });
  const cleanDir = initGitRepo();

  {
    const command = `cd ${secretDir} && git commit -m "x"`;
    const result = await runHook(command, cleanDir);
    assert.equal(
      result.denied,
      true,
      `expected deny for cd targeting the secret repo from a clean cwd, got: ${result.message}`,
    );
  }
  {
    const command = `cd ${cleanDir} && git commit -m "x"`;
    const result = await runHook(command, secretDir);
    assert.equal(
      result.denied,
      false,
      `expected allow for cd targeting a clean repo from the secret's own cwd, got: ${result.message}`,
    );
  }
  {
    // Chained cd: proves relative segments chain off the PREVIOUS
    // effectiveDir, not off the original projectDir.
    const parent = dirname(secretDir);
    const base = secretDir.split("/").pop();
    const command = `cd ${parent} && cd ${base} && git commit -m "x"`;
    const result = await runHook(command, cleanDir);
    assert.equal(
      result.denied,
      true,
      `expected deny for chained cd, got: ${result.message}`,
    );
  }
});

test("denies compound staging+commit even with a clean index (regression: check-before-staging timing)", async () => {
  const dir = initGitRepo();
  writeFileSync(join(dir, "readme.txt"), "hello\n");
  const result = await runHook('git add -A && git commit -m "x"', dir);
  assert.equal(
    result.denied,
    true,
    "expected deny for compound add+commit even with a clean index",
  );
  assert.match(result.message, /separate commands/);
});

test("denies self-staging commit flags even with a clean index; --amend is unaffected", async () => {
  const dir = initGitRepo();
  for (const command of [
    'git commit -a -m "x"',
    'git commit -am "x"',
    'git commit --all -m "x"',
  ]) {
    const result = await runHook(command, dir);
    assert.equal(
      result.denied,
      true,
      `expected deny for ${command} even with a clean index, got: ${result.message}`,
    );
    assert.match(result.message, /separate commands/);
  }

  const amendResult = await runHook('git commit --amend -m "x"', dir);
  assert.equal(
    amendResult.denied,
    false,
    `expected --amend (no -a) to be unaffected, got: ${amendResult.message}`,
  );
});

test("allows staging the deletion of a sensitive file (regression: name-only listed deletions)", async () => {
  const dir = initGitRepoWithStaged({ ".env": "SECRET=1" });
  execFileSync("git", ["commit", "-q", "-m", "add secret (test fixture)"], {
    cwd: dir,
  });
  execFileSync("git", ["rm", "--cached", "-q", ".env"], { cwd: dir });
  const result = await runHook('git commit -m "remove secret"', dir);
  assert.equal(
    result.denied,
    false,
    `expected allow for staging a deletion, got: ${result.message}`,
  );
});

test("scans the whole repo, not just the invoking cwd's subtree (regression: -- . pathspec bypass)", async () => {
  const dir = initGitRepoWithStaged({
    "secret.txt": `TOKEN=ghp_${"a".repeat(36)}\n`,
  });
  const sub = join(dir, "sub");
  mkdirSync(sub, { recursive: true });
  const result = await runHook('git commit -m "add secret from subdir"', sub);
  assert.equal(
    result.denied,
    true,
    `expected deny for a secret staged at repo root when committing from a subdirectory, got: ${result.message}`,
  );
});

test("denies a secret buried in a staged diff exceeding execFileSync's default 1 MiB buffer (regression: silent buffer-limit bypass)", async () => {
  const line = "x".repeat(200) + "\n";
  let big = "AKIAABCDEFGHIJKLMNOP\n";
  while (Buffer.byteLength(big) < 2 * 1024 * 1024) {
    big += line;
  }
  const dir = initGitRepoWithStaged({ "big.txt": big });
  const result = await runHook('git commit -m "add big file"', dir);
  assert.equal(
    result.denied,
    true,
    `expected deny for a secret buried in a ${Buffer.byteLength(big)}-byte staged diff, got: ${result.message}`,
  );
});

test("allows a clean commit", async () => {
  const dir = initGitRepoWithStaged({ "readme.txt": "hello world\n" });
  const result = await runHook('git commit -m "add readme"', dir);
  assert.equal(result.denied, false, `expected allow, got: ${result.message}`);
});

test("compound command still denies", async () => {
  const dir = initGitRepoWithStaged({ ".env": "SECRET=1" });
  const result = await runHook('git add -A && git commit -m "x"', dir);
  assert.equal(result.denied, true, "expected deny for compound command");
});

test("bypass env var allows everything", async () => {
  const dir = initGitRepoWithStaged({ ".env": "SECRET=1" });
  const result = await runHook('git commit -m "x"', dir, {
    DEVGETA_SKIP_SECRET_GUARD: "1",
  });
  assert.equal(
    result.denied,
    false,
    `expected bypass to allow, got: ${result.message}`,
  );
});

test("allows when tool is not bash, and when command is missing", async () => {
  const dir = initGitRepoWithStaged({ ".env": "SECRET=1" });
  const plugin = await SecretGuard({ directory: dir });
  const hook = plugin["tool.execute.before"];
  await assert.doesNotReject(
    hook({ tool: "edit" }, { args: { command: "git commit -m x" } }),
  );
  await assert.doesNotReject(hook({ tool: "bash" }, { args: {} }));
});
