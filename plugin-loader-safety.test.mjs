// Loader-shape test for every plugin folder devgeta ships under
// configs/opencode/plugins/, against what OpenCode 2's loader expects
// (docs/apps/opencode.md; checked against OpenCode 2.2.6):
//
//   1. A folder's entry files are named for where they run: server.js (or
//      index.js) in OpenCode's server, tui.js in each terminal client. Any
//      other top-level .js file is never loaded, so it is a mistake.
//   2. Each entry's default export is `{ id, setup }`: a non-empty string id
//      and a setup function. Nothing else is read; extra named exports are
//      tolerated (the guard exports helpers for its Go test).
//   3. Entries import only Node's standard library (`node:` specifiers) or
//      relative files. A bare package import would make OpenCode resolve a
//      dependency, which is exactly the startup stall devgeta used to work
//      around with a lockfile; keeping plugins dependency-free removes it.
//
// Run with: node --test plugin-loader-safety.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const PLUGINS_DIR = join(HERE, "configs", "opencode", "plugins");
const ENTRY_FILES = new Set(["server.js", "index.js", "tui.js"]);

const folders = readdirSync(PLUGINS_DIR).filter((name) =>
  statSync(join(PLUGINS_DIR, name)).isDirectory(),
);

test("configs/opencode/plugins ships at least one plugin folder", () => {
  assert.ok(folders.length > 0);
});

test("no loose files next to the plugin folders", () => {
  const loose = readdirSync(PLUGINS_DIR).filter(
    (name) => !statSync(join(PLUGINS_DIR, name)).isDirectory(),
  );
  assert.deepEqual(
    loose,
    [],
    "OpenCode 2 local plugins are folders; loose files load as separate plugins",
  );
});

for (const folder of folders) {
  const dir = join(PLUGINS_DIR, folder);
  const jsFiles = readdirSync(dir).filter((f) => f.endsWith(".js"));

  test(`${folder}: has an entry file and nothing the loader would skip`, () => {
    const entries = jsFiles.filter((f) => ENTRY_FILES.has(f));
    assert.ok(
      entries.length > 0,
      `${folder} has no server.js, index.js or tui.js`,
    );
    const stray = jsFiles.filter((f) => !ENTRY_FILES.has(f));
    assert.deepEqual(stray, [], `${folder} has .js files OpenCode never loads`);
  });

  for (const file of jsFiles) {
    test(`${folder}/${file}: default export is { id, setup }`, async () => {
      const mod = await import(join(dir, file));
      const plugin = mod.default;
      assert.ok(
        plugin && typeof plugin === "object",
        "missing default export object",
      );
      assert.equal(typeof plugin.id, "string");
      assert.ok(plugin.id.length > 0, "id is empty");
      assert.equal(typeof plugin.setup, "function");
    });

    test(`${folder}/${file}: imports only node: builtins or relative files`, () => {
      const source = readFileSync(join(dir, file), "utf8");
      const specifiers = [
        ...source.matchAll(/^\s*import\s[^;]*?from\s+["']([^"']+)["']/gm),
        ...source.matchAll(/^\s*import\s+["']([^"']+)["']/gm),
      ].map((m) => m[1]);
      const bare = specifiers.filter(
        (s) => !s.startsWith("node:") && !s.startsWith("."),
      );
      assert.deepEqual(
        bare,
        [],
        "bare package imports make OpenCode resolve dependencies",
      );
    });
  }
}

test("the two plugin ids are distinct", async () => {
  const ids = [];
  for (const folder of folders) {
    for (const file of readdirSync(join(PLUGINS_DIR, folder)).filter((f) =>
      ENTRY_FILES.has(f),
    )) {
      ids.push((await import(join(PLUGINS_DIR, folder, file))).default.id);
    }
  }
  assert.equal(new Set(ids).size, ids.length, `duplicate plugin ids: ${ids}`);
});
