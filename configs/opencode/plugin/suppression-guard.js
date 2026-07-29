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
// Escape hatch: set DEVGETA_SKIP_SUPPRESSION_GUARD=1 in the environment.

import { isDevgetaRepo } from "./task-redirect.js";

const BYPASS_HINT =
  "set DEVGETA_SKIP_SUPPRESSION_GUARD=1 to bypass this session if this is a false positive";

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
  const projectDir = ctx.directory || ctx.worktree || process.cwd();

  // Memoized per plugin instance, same pattern task-redirect.js's
  // findDenyMessage uses for its release-rule gate: computed at most once,
  // and only if an Edit/Write on this tool call actually needs it.
  let repoMemo;
  const inDevgetaRepo = () => {
    if (repoMemo === undefined) {
      repoMemo = isDevgetaRepo(projectDir);
    }
    return repoMemo;
  };

  return {
    "tool.execute.before": async (input, output) => {
      if (process.env.DEVGETA_SKIP_SUPPRESSION_GUARD) {
        return;
      }
      if (input.tool !== "edit" && input.tool !== "write") {
        return;
      }
      if (!inDevgetaRepo()) {
        return;
      }

      const args = output?.args ?? {};
      const newText = input.tool === "edit" ? args.newString : args.content;
      const oldText = input.tool === "edit" ? args.oldString : "";
      if (!newText || typeof newText !== "string") {
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
