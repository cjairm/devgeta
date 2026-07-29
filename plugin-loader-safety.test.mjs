// Regression test for the risk ADR-0006 documents: OpenCode's plugin loader
// (packages/opencode/src/plugin/index.ts's getLegacyPlugins) imports EVERY
// .js file under the deployed plugin directory and invokes EVERY exported
// value as if it were a plugin factory, throwing `TypeError: Plugin export
// is not a function` for any export that isn't callable.
//
// This test enforces the two halves of that constraint for every file
// actually shipped under configs/opencode/plugin/ (non-recursive — the exact
// shape CopyDir deploys):
//   1. Every export is a function (a non-function export would crash
//      OpenCode's config load entirely).
//   2. Calling that function with a plausible plugin ctx does not throw
//      synchronously (a helper written for internal reuse — like
//      isDevgetaRepo or splitCommandSegments — must tolerate being
//      accidentally invoked this way; see task-redirect.js's export
//      comments).
//
// A future contributor who adds a new shared-helpers file to plugin/ (the
// exact mistake ADR-0006 warns against) will trip rule 1 immediately.
//
// Run with: node --test plugin-loader-safety.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const PLUGIN_DIR = join(HERE, "configs", "opencode", "plugin");
const DEVGETA_DIR = HERE; // this repo's own root — a plausible, real ctx.directory

const pluginFiles = readdirSync(PLUGIN_DIR).filter((f) => f.endsWith(".js"));

test("every deployed plugin file has at least one export", () => {
  assert.ok(
    pluginFiles.length > 0,
    "expected at least one .js file under configs/opencode/plugin/",
  );
});

for (const file of pluginFiles) {
  test(`${file}: every export is a function callable with a plugin-shaped ctx`, async () => {
    const mod = await import(join(PLUGIN_DIR, file));
    const exportNames = Object.keys(mod);
    assert.ok(exportNames.length > 0, `${file} has no exports at all`);

    for (const name of exportNames) {
      const value = mod[name];
      assert.equal(
        typeof value,
        "function",
        `${file}'s export "${name}" is not a function — OpenCode's plugin loader would throw "Plugin export is not a function" on this file`,
      );

      // Call it the way the loader would: with a single ctx-shaped argument.
      // Some exports (isDevgetaRepo) expect a plain string instead of a
      // ctx object — either call shape must not throw synchronously.
      await assert.doesNotReject(
        (async () => value({ directory: DEVGETA_DIR }))(),
        `${file}'s export "${name}" threw when called with a ctx object`,
      );
    }
  });
}
