// devgeta-format: after Pi edits or writes a file, run devgeta's shared
// format-file.sh (the same Mason tools Claude Code uses, ADR-0063) and tell the
// model what changed. Rewritten by `dg configure pi`; customize with your own
// extension file instead of editing this one.
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { delimiter, isAbsolute, join, resolve } from "node:path";

const MAX_LINES = 60;
const MAX_BYTES = 6000;
const EXEC_TIMEOUT_MS = 60000;

const home = homedir();
// Same locations format-file.sh and paths.Paths.Config.Devgeta use.
const masonBin = join(
  process.env.XDG_DATA_HOME || join(home, ".local", "share"),
  "nvim",
  "mason",
  "bin",
);
const formatScript = join(
  process.env.XDG_CONFIG_HOME || join(home, ".config"),
  "devgeta",
  "format-file.sh",
);

function readOrNull(path) {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return null;
  }
}

// changedSpan finds the first to last differing line by plain prefix/suffix
// comparison. Returns null when the texts are equal.
function changedSpan(before, after) {
  if (before === after) return null;
  const a = before.split("\n");
  const b = after.split("\n");
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let endA = a.length - 1;
  let endB = b.length - 1;
  while (endA >= start && endB >= start && a[endA] === b[endB]) {
    endA--;
    endB--;
  }
  return {
    from: start + 1,
    to: Math.max(endB, start) + 1,
    lines: b.slice(start, endB + 1),
  };
}

function describeChange(span) {
  const header = `Formatted by devgeta: lines ${span.from}–${span.to} changed. They now read:`;
  const body = span.lines.join("\n");
  if (span.lines.length > MAX_LINES || Buffer.byteLength(body) > MAX_BYTES) {
    return `${header}\n…reformatted beyond what fits here; read the file before editing it.`;
  }
  return `${header}\n${body}`;
}

export default function (pi) {
  // Mason's binaries on PATH so pi-lens finds them instead of downloading its own.
  const entries = (process.env.PATH || "").split(delimiter);
  if (existsSync(masonBin) && !entries.includes(masonBin)) {
    process.env.PATH = [masonBin, ...entries].filter(Boolean).join(delimiter);
  }
  for (const name of [
    "PI_LENS_DISABLE_TOOL_INSTALL",
    "PI_LENS_DISABLE_LSP_INSTALL",
  ]) {
    if (process.env[name] === undefined) process.env[name] = "1";
  }

  pi.on("tool_result", async (event, ctx) => {
    try {
      if (event.isError) return;
      if (event.toolName !== "edit" && event.toolName !== "write") return;
      const target = event.input && event.input.path;
      if (typeof target !== "string" || target === "") return;
      const absolute = isAbsolute(target) ? target : resolve(ctx.cwd, target);

      const before = readOrNull(absolute);
      if (before === null) return;
      const result = await pi.exec(formatScript, [absolute], {
        signal: ctx.signal,
        timeout: EXEC_TIMEOUT_MS,
      });
      const after = readOrNull(absolute);

      const parts = [];
      const span = after === null ? null : changedSpan(before, after);
      if (span) parts.push(describeChange(span));
      const findings = (
        result && result.stdout ? String(result.stdout) : ""
      ).trim();
      if (findings)
        parts.push(`Linter findings for ${target} — please fix:\n${findings}`);
      if (parts.length === 0) return;

      return {
        content: [...event.content, { type: "text", text: parts.join("\n\n") }],
        structuredContent: event.structuredContent,
      };
    } catch {
      // Formatting is a convenience: never fail or alter the edit.
      return;
    }
  });
}
