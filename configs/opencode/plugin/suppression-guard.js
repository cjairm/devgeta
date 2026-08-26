// OpenCode plugin: deny an Edit or Write that INTRODUCES a lint-suppression
// comment instead of fixing the underlying issue. Mirrors
// configs/claude/suppression-guard.sh — see that file's header comment for
// the full rationale, the DEVGETA-REPO-ONLY scope (ADR-0006), and the
// introduced-vs-preexisting distinction for Edit vs Write. Keep the two
// PATTERNS lists in sync.
//
// Reuses task-redirect.js's exported isDevgetaRepo instead of duplicating
// the go.mod walk — see secret-guard.js's header comment (and ADR-0006) for
// why this file does not introduce its own standalone "lib" module under
// plugin/.
//
// Escape hatch: set DEVGETA_SKIP_SUPPRESSION_GUARD=1 in the shell that launches this
// agent (e.g. in the repo's .envrc or your shell profile) BEFORE invoking this
// plugin — this plugin reads its own environment, not one set inside the command.

import { dirname, isAbsolute, join } from "node:path";

import { isDevgetaRepo } from "./task-redirect.js";

const BYPASS_HINT =
  "bypass: export DEVGETA_SKIP_SUPPRESSION_GUARD=1 in the shell that launches this agent (e.g. the repo's .envrc), not inside the command — this hook reads its own environment";

// PATTERNS mirrors suppression-guard.sh's PATTERNS exactly — keep the two
// lists in sync. Checked as plain substrings, not regexes.
const PATTERNS = [
  ["Go", "//nolint"],
  ["Python", "# noqa"],
  ["Python", "# type: ignore"],
  ["Python", "# pylint: disable"],
  ["JS/TS", "eslint-disable"],
  ["JS/TS", "@ts-ignore"],
  ["JS/TS", "@ts-nocheck"],
  ["Java/Kotlin", "@SuppressWarnings"],
  ["Ruby", "rubocop:disable"],
];

// countOccurrences counts non-overlapping occurrences of `needle` in
// `haystack`. Mirrors suppression-guard.sh's count_occurrences.
function countOccurrences(haystack, needle) {
  return haystack.split(needle).length - 1;
}

export const SuppressionGuard = async (ctx = {}) => {
  const sessionDir = ctx.directory || ctx.worktree || process.cwd();

  // Memoized per DIRECTORY, not per plugin instance: the scope gate now
  // answers for the file being written (see scopeDirFor below), so the answer
  // varies between calls and a single-slot memo would answer for whichever
  // directory happened to be asked about first.
  const repoMemo = new Map();
  const inDevgetaRepo = (dir) => {
    if (!repoMemo.has(dir)) {
      repoMemo.set(dir, isDevgetaRepo(dir));
    }
    return repoMemo.get(dir);
  };

  // Scope by the FILE being written, not by where the session is rooted —
  // mirrors suppression-guard.sh's SCOPE_DIR, see that file for why the
  // session directory is wrong in both directions. A relative path resolves
  // against the session directory; an absent one falls back to it.
  const scopeDirFor = (filePath) => {
    if (typeof filePath !== "string" || filePath === "") {
      return sessionDir;
    }
    return dirname(
      isAbsolute(filePath) ? filePath : join(sessionDir, filePath),
    );
  };

  return {
    "tool.execute.before": async (input, output) => {
      if (process.env.DEVGETA_SKIP_SUPPRESSION_GUARD) {
        return;
      }
      if (input.tool !== "edit" && input.tool !== "write") {
        return;
      }

      const args = output?.args ?? {};
      const newText = input.tool === "edit" ? args.newString : args.content;
      const oldText = input.tool === "edit" ? args.oldString : "";
      if (!newText || typeof newText !== "string") {
        return;
      }

      // Checked after the text, so a call with nothing to scan never pays for
      // the go.mod walk.
      if (!inDevgetaRepo(scopeDirFor(args.filePath))) {
        return;
      }

      // Count-based, not presence-based: mirrors suppression-guard.sh's
      // count_occurrences — see that comment for why a presence check
      // (needle in NEW, not in OLD) misses a genuinely new occurrence when
      // an unrelated old one of the same kind already existed.
      for (const [label, needle] of PATTERNS) {
        const oldCount = countOccurrences(oldText ?? "", needle);
        const newCount = countOccurrences(newText, needle);
        if (newCount > oldCount) {
          throw new Error(
            `Introduces a ${label} suppression comment (${needle}) — fix the underlying issue instead (CLAUDE.md 'Lint issues' calls this never acceptable) — ${BYPASS_HINT}`,
          );
        }
      }
    },
  };
};
