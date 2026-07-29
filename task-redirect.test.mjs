// Behavioral test for configs/opencode/plugin/task-redirect.js — the
// OpenCode-side mirror of configs/claude/task-redirect.sh (see
// task_redirect_test.go, package main, for the bash script's equivalent
// end-to-end test).
//
// This file is deliberately NOT under configs/opencode/plugin/: that
// directory is copied byte-for-byte to the user's OpenCode plugin dir
// (internal/apps/opencode/opencode.go's ForceConfigure step, via
// files.CopyDir) — a test file living there would ship to every installed
// machine. Keeping this test at the repo root (sibling to
// task_redirect_test.go) mirrors the Go test's placement without polluting
// the deployed plugin directory.
//
// Run with: node --test task-redirect.test.mjs
// Uses only Node's built-in test runner and assert module (available since
// Node 18) — no new dependency for a single test file, per CLAUDE.md's
// "prefer the standard library" rule applied to this repo's one JS file.

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  TaskRedirect,
  isDevgetaRepo,
} from "./configs/opencode/plugin/task-redirect.js";

// This test file lives at the repo root, so its own directory is a devgeta
// repo (repo-root go.mod has module github.com/cjairm/devgeta). Used as the
// devgeta-cwd for the release- and worktree-gating tests below.
const DEVGETA_DIR = dirname(fileURLToPath(import.meta.url));

// runHook drives the plugin exactly as OpenCode would: build the plugin,
// invoke its tool.execute.before hook with a bash tool call, and report
// whether it denied (threw) or allowed (returned normally). `ctx` is the
// OpenCode plugin context (defaults to the devgeta repo dir so the release
// and worktree rules are exercised in their firing state unless a test
// overrides it).
async function runHook(command, env = {}, ctx = { directory: DEVGETA_DIR }) {
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
    const plugin = await TaskRedirect(ctx);
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

test("allows legitimate single commands", async () => {
  const allowed = [
    "git diff",
    "git diff HEAD~1",
    "git diff --stat",
    "git log",
    "git log -5",
    "git log --oneline",
    "git tag",
    "git tag v1.0.0",
    "git tag -l",
    "git reset --soft HEAD",
    "git worktree list",
    "git worktree prune",
    "git status",
    "git push origin main",
    'git commit -m "fix: something"',
    // Compound commands where no segment matches any rule.
    "cd some/dir && git status",
    "git fetch && git log -5",
    // A commit message mentioning a trigger word must not itself trigger a
    // rule — "worktree" here is message text, not a `git worktree` call.
    'git commit -m "fix: worktree stuff"',
    // A commit message containing separator-like characters (';', '&&')
    // must not be split apart: the quoted span is one segment, and neither
    // half looks like a git invocation on its own.
    'git commit -m "fix: a; b"',
    // Pathological case for quote-aware splitting: this commit message
    // literally contains "&& git worktree" as text. A naive
    // (non-quote-aware) splitter would slice this into a segment starting
    // with "git worktree" and falsely deny it. Still a single `git commit`
    // command — must be allowed.
    'git commit -m "notes && git worktree stuff"',
    // gh commands that must NOT match the new gh rules: `pr view` is a
    // different subcommand from `pr review`/`pr checks`; `pr status`/`pr list`
    // are neither; a graphql query without reviewThreads and a bare `gh api`
    // are not the review-threads fetch.
    "gh pr view",
    "gh pr view --json title",
    "gh pr status",
    "gh pr list",
    "gh api graphql -f query='{ viewer { login } }'",
    "gh api repos/cjairm/devgeta",
    // Bare long-flag sanity check for the space-separated-long-flag
    // alternative added to GH_GLOBAL_OPT/GIT_GLOBAL_OPT: a bare boolean flag
    // followed by an unrelated command must not have that command mistaken
    // for the flag's "value".
    "git --no-pager status",
    "gh --paginate pr view",
  ];
  for (const command of allowed) {
    const result = await runHook(command);
    assert.equal(
      result.denied,
      false,
      `expected allow for ${JSON.stringify(command)}, got deny: ${result.message}`,
    );
  }
});

test("denies narrow patterns, including compound commands and env-var prefixes", async () => {
  const cases = [
    ["git diff main..feature", "devgeta task review-package"],
    ["git diff v1.2.0..v1.3.0", "devgeta task review-package"],
    ["git diff --stat A..B", "devgeta task review-package"],
    ["git log --oneline base..head", "devgeta task review-package"],
    // The two worktree cases below also rely on runHook's default ctx
    // (the devgeta dir): the devgeta-repo gate they share with the release
    // rules resolves to "yes" here, same as the "worktree rules gated to
    // devgeta repo" test asserts explicitly.
    ["git worktree add ../wt -b feature-x", "devgeta task worktree-start"],
    ["git worktree remove ../wt", "devgeta task worktree-finish"],
    // New gh rules — all GLOBAL, so they deny regardless of cwd. (runHook
    // defaults ctx to the devgeta dir, but these rules never consult it; the
    // release-gating tests below cover the cwd-dependent rules.)
    ["gh pr checks", "devgeta task pr-checks"],
    ["gh pr checks --watch", "devgeta task pr-checks"],
    ["gh pr review --approve", "devgeta task submit-review"],
    ["gh pr review --request-changes -b bad", "devgeta task submit-review"],
    [
      "gh api graphql --paginate -f query='{ repository { pullRequest { reviewThreads { nodes { id } } } } }'",
      "devgeta task review-threads",
    ],
    // Compound commands: a matching segment anywhere in the chain must
    // deny, not just a bare command at position 0.
    [
      "cd some/dir && git worktree add ../wt -b x",
      "devgeta task worktree-start",
    ],
    ["git status; git worktree remove ../wt", "devgeta task worktree-finish"],
    ["git fetch && git diff main..feature", "devgeta task review-package"],
    ["gh pr view && gh pr checks", "devgeta task pr-checks"],
    // git diff a..b | less: the LHS of the pipe is still a git invocation
    // itself, so this must deny too.
    ["git diff main..feature | less", "devgeta task review-package"],
    // Env-var-prefix case (no separator character before `git` at all):
    // deliberately handled now that GIT_PREFIX is being reworked anyway — a
    // simple `NAME=value` prefix, single or repeated, in front of `git`
    // still denies.
    ["GIT_PAGER=cat git diff main..feature", "devgeta task review-package"],
    [
      "FOO=bar BAZ=qux git worktree add ../wt -b x",
      "devgeta task worktree-start",
    ],
    // Global-option-prefix case (same class of bypass a reviewer found in
    // secret-guard.js, fixed here too): a git/gh global option between the
    // binary and its subcommand must not defeat the anchor.
    ["git -C ../wt worktree add ../other -b x", "devgeta task worktree-start"],
    ["gh -R owner/repo pr checks", "devgeta task pr-checks"],
    // Space-separated long-flag case (reviewer-found: the alternation only
    // recognized `--flag=value`/bare `--flag`, never `--flag value`).
    ["gh --repo owner/repo pr checks", "devgeta task pr-checks"],
    ["gh --repo=owner/repo pr checks", "devgeta task pr-checks"],
    // Regression-verify the three exact -C/-R anchor shapes named by this
    // cycle's hook-rescope plan (assert, do not re-implement — the fix
    // itself is the GIT_GLOBAL_OPT/GH_GLOBAL_OPT global-option handling
    // above). `git -C .` uses "." as the dir arg rather than a relative
    // path, and the gh case carries a trailing PR-number argument after
    // `checks` — neither shape was covered by an existing case above.
    ["git -C . worktree add wt", "devgeta task worktree-start"],
    ["git -C . diff main..HEAD", "devgeta task review-package"],
    ["gh -R o/r pr checks 1", "devgeta task pr-checks"],
  ];
  for (const [command, wantReplacement] of cases) {
    const result = await runHook(command);
    assert.equal(
      result.denied,
      true,
      `expected deny for ${JSON.stringify(command)}`,
    );
    assert.ok(
      result.message.includes(wantReplacement),
      `expected deny reason for ${JSON.stringify(command)} to mention ${JSON.stringify(wantReplacement)}, got ${JSON.stringify(result.message)}`,
    );
    assert.ok(
      result.message.includes("DEVGETA_SKIP_TASK_REDIRECT"),
      `expected deny reason for ${JSON.stringify(command)} to state the bypass escape hatch, got ${JSON.stringify(result.message)}`,
    );
    assert.ok(
      result.message.includes("shell that launches this agent") &&
        result.message.includes("this hook reads its own environment"),
      `expected deny reason for ${JSON.stringify(command)} to contain the reworded bypass hint, got ${JSON.stringify(result.message)}`,
    );
  }
});

// isDevgetaRepo is unit-tested directly with injected paths so the release
// gate's core decision is verified independently of the plugin plumbing.
test("isDevgetaRepo detects the devgeta go.mod by walking up", () => {
  // The repo root itself is devgeta.
  assert.equal(isDevgetaRepo(DEVGETA_DIR), true);
  // A nested subdirectory still resolves upward to the devgeta go.mod.
  assert.equal(isDevgetaRepo(join(DEVGETA_DIR, "cmd")), true);

  // A dir with no go.mod anywhere up the (temp) tree: not devgeta.
  const noGoMod = mkdtempSync(join(tmpdir(), "no-gomod-"));
  assert.equal(isDevgetaRepo(noGoMod), false);

  // A dir whose go.mod is a different module: not devgeta.
  const otherModule = mkdtempSync(join(tmpdir(), "other-mod-"));
  writeFileSync(
    join(otherModule, "go.mod"),
    "module github.com/other/thing\n\ngo 1.25\n",
  );
  assert.equal(isDevgetaRepo(otherModule), false);

  // Indeterminate inputs fail toward false (release rules do not fire).
  assert.equal(isDevgetaRepo(undefined), false);
  assert.equal(isDevgetaRepo(""), false);
  assert.equal(isDevgetaRepo(123), false);
});

test("release rules deny inside devgeta, allow everywhere else", async () => {
  const releaseCommands = [
    "git reset --soft HEAD~1",
    "git reset --soft HEAD~3",
    "git tag -a v0.12.0 -m release",
    "git tag -a -m release v0.12.0",
    "cd wt && git reset --soft HEAD~2",
    "git status && git tag -a v1.0.0 -m release",
  ];

  const noGoMod = mkdtempSync(join(tmpdir(), "no-gomod-"));
  const otherModule = mkdtempSync(join(tmpdir(), "other-mod-"));
  writeFileSync(
    join(otherModule, "go.mod"),
    "module github.com/other/thing\n\ngo 1.25\n",
  );

  for (const command of releaseCommands) {
    // Inside devgeta: deny.
    const inside = await runHook(command, {}, { directory: DEVGETA_DIR });
    assert.equal(
      inside.denied,
      true,
      `expected deny inside devgeta for ${JSON.stringify(command)}`,
    );
    assert.ok(inside.message.includes("devgeta task release"));

    // Outside devgeta (no go.mod, and a different module): allow.
    for (const dir of [noGoMod, otherModule]) {
      const outside = await runHook(command, {}, { directory: dir });
      assert.equal(
        outside.denied,
        false,
        `expected allow outside devgeta (${dir}) for ${JSON.stringify(command)}, got: ${outside.message}`,
      );
    }
  }
});

test("worktree rules gated to devgeta repo, allow elsewhere", async () => {
  const worktreeCommands = [
    ["git worktree add ../wt -b x", "devgeta task worktree-start"],
    ["git worktree remove ../wt", "devgeta task worktree-finish"],
    [
      "cd some/dir && git worktree add ../wt -b x",
      "devgeta task worktree-start",
    ],
  ];

  const noGoMod = mkdtempSync(join(tmpdir(), "no-gomod-"));
  const otherModule = mkdtempSync(join(tmpdir(), "other-mod-"));
  writeFileSync(
    join(otherModule, "go.mod"),
    "module github.com/other/thing\n\ngo 1.25\n",
  );

  for (const [command, wantReplacement] of worktreeCommands) {
    // Inside devgeta: deny.
    const inside = await runHook(command, {}, { directory: DEVGETA_DIR });
    assert.equal(
      inside.denied,
      true,
      `expected deny inside devgeta for ${JSON.stringify(command)}`,
    );
    assert.ok(
      inside.message.includes(wantReplacement),
      `expected deny reason for ${JSON.stringify(command)} to mention ${JSON.stringify(wantReplacement)}, got ${JSON.stringify(inside.message)}`,
    );

    // Outside devgeta (no go.mod, and a different module): allow.
    for (const dir of [noGoMod, otherModule]) {
      const outside = await runHook(command, {}, { directory: dir });
      assert.equal(
        outside.denied,
        false,
        `expected allow outside devgeta (${dir}) for ${JSON.stringify(command)}, got: ${outside.message}`,
      );
    }
  }
});

test("global rules unaffected by cwd: gh pr checks and git diff <range> still deny outside devgeta", async () => {
  const nonDevgetaDir = mkdtempSync(join(tmpdir(), "no-gomod-"));

  const cases = [
    ["gh pr checks", "devgeta task pr-checks"],
    ["git diff main..feature", "devgeta task review-package"],
  ];
  for (const [command, wantReplacement] of cases) {
    const result = await runHook(command, {}, { directory: nonDevgetaDir });
    assert.equal(
      result.denied,
      true,
      `expected deny outside devgeta for global rule ${JSON.stringify(command)}`,
    );
    assert.ok(
      result.message.includes(wantReplacement),
      `expected deny reason for ${JSON.stringify(command)} to mention ${JSON.stringify(wantReplacement)}, got ${JSON.stringify(result.message)}`,
    );
  }
});

test("release gate uses worktree fallback and fails toward not firing", async () => {
  // No `directory` in ctx: falls back to `worktree`.
  const viaWorktree = await runHook(
    "git reset --soft HEAD~1",
    {},
    { worktree: DEVGETA_DIR },
  );
  assert.equal(viaWorktree.denied, true);

  // Empty ctx and a non-devgeta process.cwd() would allow; here we assert the
  // explicit non-devgeta directory path allows (the fail-toward-not-firing
  // posture), which is the safety-critical direction.
  const noGoMod = mkdtempSync(join(tmpdir(), "no-gomod-"));
  const failOpen = await runHook(
    "git tag -a v1.2.3 -m r",
    {},
    { directory: noGoMod },
  );
  assert.equal(failOpen.denied, false);
});

test("bypass env var allows everything", async () => {
  const result = await runHook("git worktree add ../wt -b x", {
    DEVGETA_SKIP_TASK_REDIRECT: "1",
  });
  assert.equal(result.denied, false);
});

test("ignores non-bash tool calls and non-string commands", async () => {
  const plugin = await TaskRedirect();
  const hook = plugin["tool.execute.before"];

  // Non-bash tool: never even inspects the command.
  await assert.doesNotReject(() =>
    hook(
      { tool: "edit" },
      { args: { command: "git worktree add ../wt -b x" } },
    ),
  );

  // Missing/non-string command: falls through without throwing.
  await assert.doesNotReject(() => hook({ tool: "bash" }, { args: {} }));
  await assert.doesNotReject(() =>
    hook({ tool: "bash" }, { args: { command: 123 } }),
  );
});
