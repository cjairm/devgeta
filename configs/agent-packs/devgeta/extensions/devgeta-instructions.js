// devgeta-instructions: appends DEVGETA.md, the pack's response and writing
// rules, to Pi's system prompt on every run. It is how the same file reaches
// Pi that reaches Claude Code through an @DEVGETA.md import and OpenCode
// through an `instructions` entry (ADR-0058, ADR-0059 §7).
//
// DEVGETA.md sits at the package root, one folder above this file. It is read
// on every run, so an edit shows up in the next prompt without a restart.
// `dg agent-pack` rebuilds this package; to change the rules, put your own
// DEVGETA.md under ~/.config/devgeta/agent-packs/devgeta/ and run
// `dg agent-pack update devgeta`.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const instructionsPath = resolve(dirname(fileURLToPath(import.meta.url)), "..", "DEVGETA.md");

// Returns the file's text, or null when it is missing or blank. An extension
// that throws can break a session, and a missing rules file is not worth that.
function readInstructions() {
  try {
    const text = readFileSync(instructionsPath, "utf8").trim();
    return text === "" ? null : text;
  } catch {
    return null;
  }
}

export default function (pi) {
  // The system prompt is rebuilt for each run, so text added here is never part
  // of the conversation that compaction summarizes away.
  pi.on("before_agent_start", async (event) => {
    if (typeof event.systemPrompt !== "string") return;
    const text = readInstructions();
    if (text === null) return;
    if (event.systemPrompt.includes(text)) return;
    return { systemPrompt: `${event.systemPrompt}\n\n${text}` };
  });
}
