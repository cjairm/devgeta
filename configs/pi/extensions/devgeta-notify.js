// devgeta-notify: report Pi's activity into the tmux pane it runs in, so the
// `devgeta ws` dashboard lists it as an agent (working / done / blocked /
// error) and the status bar and optional sound flag it when it wants you. The
// writes, the window mirror and the sound all live in devgeta's shared
// agent-state.sh, the same script Claude Code's hooks run; this file only maps
// Pi's events onto it. Rewritten by `dg configure pi`; customize with your own
// extension file instead of editing this one.
import { homedir } from "node:os";
import { join } from "node:path";

const EXEC_TIMEOUT_MS = 5000;

// Same location devgeta-format.js uses for format-file.sh.
const agentStateScript = join(
  process.env.XDG_CONFIG_HOME || join(homedir(), ".config"),
  "devgeta",
  "agent-state.sh",
);

export default function (pi) {
  // Writes run one at a time so a quick busy → idle can never land reversed.
  let queue = Promise.resolve();
  const report = (state) => {
    if (!process.env.TMUX_PANE) return queue;
    queue = queue
      .then(() =>
        pi.exec(agentStateScript, [state, "pi"], { timeout: EXEC_TIMEOUT_MS }),
      )
      // Reporting is cosmetic: a missing script or dead tmux must never fail Pi.
      .catch(() => {});
    return queue;
  };

  // turn_end carries each turn's outcome; agent_settled says only whether the
  // run was aborted, so the last outcome decides between done and error.
  let lastOutcome = "completed";

  pi.on("session_start", () => report("start"));
  pi.on("agent_start", () => {
    lastOutcome = "completed";
    return report("busy");
  });
  pi.on("turn_end", (event) => {
    lastOutcome = event.outcome;
  });
  pi.on("agent_settled", (event) =>
    report(!event.aborted && lastOutcome === "error" ? "error" : "idle"),
  );
  // Only a prompt during a run is Pi waiting on you; one opened from an idle
  // command is something you are already looking at.
  pi.on("ui_prompt_start", (_event, ctx) => {
    if (!ctx.isIdle()) report("blocked");
  });
  pi.on("ui_prompt_end", (_event, ctx) => {
    if (!ctx.isIdle()) report("busy");
  });
  pi.on("session_shutdown", () => report("end"));
}
