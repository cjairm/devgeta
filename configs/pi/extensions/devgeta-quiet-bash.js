// devgeta-quiet-bash: show only the command for each bash call, not its output.
// ctrl+o (app.tools.expand) toggles the full output, rendered by Pi's own bash
// renderer. Execution is Pi's built-in bash tool, unchanged. Rewritten by
// `dg configure pi`, so to opt out add a later extension that registers bash
// again instead of editing this file.
//
// Adapted from Pi's examples/extensions/minimal-mode.ts (MIT,
// @earendil-works/pi-coding-agent). Only bash is overridden: minimal-mode's
// edit and write overrides replace Pi's diff view with plain text.
import { createBashToolDefinition } from "@earendil-works/pi-coding-agent";
import { Text } from "@earendil-works/pi-tui";

export default function (pi) {
  const builtIn = createBashToolDefinition(process.cwd());

  pi.registerTool({
    ...builtIn,
    renderResult(result, options, theme, context) {
      if (!options.expanded) return new Text("", 0, 0);
      // The built-in renderer reuses context.lastComponent as its own container;
      // after a collapsed render that is our empty Text, which it cannot reuse.
      return builtIn.renderResult(result, options, theme, {
        ...context,
        lastComponent: undefined,
      });
    },
  });
}
