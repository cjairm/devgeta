// OpenCode mirror of configs/claude/output-budget.sh's matching/rewrite
// decision (docs/guides/output-budget-runner.md). The runner itself
// (output-budget-run.sh) is bash-only and shared by both agents — this file
// only decides whether and how to rewrite the command to call it.
//
// Reads the generated sidecar (~/.config/devgeta/agent-runtime.json) with
// readFileSync + JSON.parse, both Node built-ins — no dependency, no
// subprocess, following task-redirect.js's own existsSync/readFileSync
// precedent (guide §2.3, §5).
//
// Escape hatch: DEVGETA_OUTPUT_BUDGET=off skips rewriting entirely, matching
// the runner's own pass-through for the same variable.

import { existsSync, readFileSync } from "node:fs";
import { splitCommandSegments, shellQuote } from "./task-redirect.js";

// The bash/JS shared numeric-transport contract (guide §5.3): a positive
// integer of at most 15 decimal digits, checked against the RENDERED
// string — never the parsed number, since JSON.parse silently changes a
// value above 2^53 and switches to exponential notation above 1e21.
const WIDTH_RE = /^[1-9][0-9]{0,14}$/;

function isWidthValid(n) {
  return WIDTH_RE.test(String(n));
}

function sidecarPath() {
  const base = process.env.XDG_CONFIG_HOME || `${process.env.HOME}/.config`;
  return `${base}/devgeta/agent-runtime.json`;
}

// loadSidecar returns the validated sidecar object, or null for every
// degenerate case (absent, unreadable, malformed JSON, outputBudget
// missing/false/wrong-type, runner missing, any limit or rule failing the
// width/shape contract) — every one of those must leave the command
// unmodified, never throw.
function loadSidecar() {
  try {
    const path = sidecarPath();
    if (!existsSync(path)) return null;
    const parsed = JSON.parse(readFileSync(path, "utf8"));
    if (parsed.outputBudget !== true) return null;
    if (typeof parsed.runner !== "string" || !existsSync(parsed.runner)) {
      return null;
    }
    if (
      !isWidthValid(parsed.lineContentLimit) ||
      !isWidthValid(parsed.maxTotalBytes) ||
      !isWidthValid(parsed.captureContentLimit)
    ) {
      return null;
    }
    if (!Array.isArray(parsed.rules) || parsed.rules.length === 0) return null;
    for (const rule of parsed.rules) {
      if (!rule || typeof rule.name !== "string" || !rule.name) return null;
      if (
        !Array.isArray(rule.match) ||
        rule.match.length < 1 ||
        rule.match.length > 2
      ) {
        return null;
      }
      if (!isWidthValid(rule.head) || !isWidthValid(rule.tail)) return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

// isHostileToken mirrors devgeta_ob_token_is_hostile in output-budget.sh:
// a token containing a single quote, double quote, backslash, or $ means
// this best-effort whitespace split cannot be trusted for it (guide §8.2
// step 5) — under-match rather than parse.
function isHostileToken(token) {
  return /['"\\$]/.test(token);
}

// matchSegment applies the tokenization contract (guide §8.2) to one
// command segment and returns the first matching rule (array order —
// guide §8.3), or null. Hostility is checked PER RULE ATTEMPT using only
// that rule's own match length, never a blanket check of the first two
// tokens — otherwise `make -j$(nproc)` would wrongly refuse to match the
// one-token "make" rule over a $ in the SECOND token, which that rule
// never examines.
function matchSegment(segment, rules) {
  const trimmed = segment.trim();
  if (!trimmed) return null;

  const toks = trimmed.split(/[ \t]+/).filter((t) => t.length > 0);
  if (toks.length === 0) return null;

  const assignRe = /^[A-Za-z_][A-Za-z0-9_]*=/;
  let idx = 0;
  while (idx < toks.length && assignRe.test(toks[idx])) {
    if (isHostileToken(toks[idx])) return null;
    idx++;
  }
  if (idx < toks.length && toks[idx] === "env") {
    idx++;
    while (idx < toks.length && assignRe.test(toks[idx])) {
      if (isHostileToken(toks[idx])) return null;
      idx++;
    }
  }

  const cmp0 = toks[idx];
  const cmp1 = toks[idx + 1];
  if (!cmp0) return null;

  for (const rule of rules) {
    if (rule.match.length === 1) {
      if (isHostileToken(cmp0)) continue;
      if (cmp0 === rule.match[0]) return rule;
    } else if (rule.match.length === 2) {
      if (!cmp1) continue;
      if (isHostileToken(cmp0) || isHostileToken(cmp1)) continue;
      if (cmp0 === rule.match[0] && cmp1 === rule.match[1]) return rule;
    }
  }
  return null;
}

export const OutputBudget = async () => ({
  "tool.execute.before": async (input, output) => {
    if (process.env.DEVGETA_OUTPUT_BUDGET === "off") return;
    if (input.tool !== "bash") return;

    const command = output?.args?.command;
    if (!command || typeof command !== "string") return;

    const sidecar = loadSidecar();
    if (!sidecar) return;

    let matched = null;
    for (const segment of splitCommandSegments(command)) {
      matched = matchSegment(segment, sidecar.rules);
      if (matched) break;
    }
    if (!matched) return;

    // Defence in depth: re-validate everything about to be interpolated,
    // right before interpolating it (guide §5.4) — the rules array was
    // already checked as a whole in loadSidecar, but nothing stops this
    // from being cheap too.
    if (!isWidthValid(matched.head) || !isWidthValid(matched.tail)) return;

    output.args.command = [
      shellQuote(sidecar.runner),
      String(matched.head),
      String(matched.tail),
      String(sidecar.lineContentLimit),
      String(sidecar.maxTotalBytes),
      String(sidecar.captureContentLimit),
      shellQuote(command),
    ].join(" ");
  },
});
