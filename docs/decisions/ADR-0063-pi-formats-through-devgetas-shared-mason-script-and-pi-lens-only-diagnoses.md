# ADR-0063 — Pi formats through devgeta's shared Mason script; pi-lens only diagnoses

**Date:** 2026-11-06
**Status:** ACCEPTED

## Context

Claude Code and OpenCode format and lint every file the agent edits with the
binaries Neovim's Mason installs (`~/.local/share/nvim/mason/bin`): Claude
through the `format.sh` PostToolUse hook, OpenCode through its `formatter`
block. Pi has no hook config, and its curated set (ADR-0060) is about to gain
`pi-lens`, which adds language-server diagnostics but also formats and
autofixes files with its own tool choices (gofmt instead of
goimports/gofumpt/golines, ruff instead of isort/black, biome or prettier only
when a project config exists). It also downloads any tool it cannot find.

Left alone, the three agents would write differently formatted code in the same
repo, and Pi would quietly download a second copy of tools devgeta already
installs.

What was checked (investigation `2026-11-05-pi-extension-set.md`, `pi-lens`
section; Pi 1.5.0 and pi-lens 4.4.9 source):

- Pi's extension API is the hook mechanism. A `tool_result` handler runs after
  `edit`/`write`, can run commands (`pi.exec`), and can rewrite the result the
  model sees. Handlers chain in extension load order.
- Pi sorts extensions as project files → user files in
  `~/.pi/agent/extensions/` → npm packages → built-ins
  (`resourcePrecedenceRank`). A user file always runs before pi-lens; a
  `pi install <dir>` local package is ranked with the npm packages and would
  not.
- pi-lens reads `PI_LENS_DISABLE_TOOL_INSTALL` and `PI_LENS_DISABLE_LSP_INSTALL`
  from `process.env` at each install attempt, and finds tools in its own
  install directories first, then `PATH`. Pi runs all extensions in one
  process.
- pi-lens turns off its own file changes with `format.enabled: false` and
  `autofix.enabled: false` in its global config. It does not write that file.
- Pi's `edit` needs an exact `oldText` match, so a reformat the agent is not
  told about breaks its next edit.

## Decision

**1. One agent-neutral formatter script.** The per-language table in
`configs/claude/format.sh` moves to `configs/devgeta/format-file.sh`, deployed
to `~/.config/devgeta/format-file.sh` (`paths.Paths.Config.Devgeta`, where
`output-budget-run.sh` already lives). It takes a file path, formats it in
place with the Mason binaries, prints any lint findings as plain text on
stdout, and always exits 0. It knows nothing about any agent's payload format.
Claude's `format.sh` becomes a wrapper: read the hook JSON, call the script,
wrap its output as `additionalContext`. The `hire2` HTML exception stays in
the Claude wrapper, where it applies today, so it does not spread to Pi.

**2. Pi gets one devgeta-owned extension file,**
`~/.pi/agent/extensions/devgeta-format.js`. It is a user-level auto-discovered
file, not a package, because that is the only location guaranteed to load
before pi-lens. Devgeta owns exactly this path (like the theme file,
ADR-0061 §2). It is code, not user config, so every configure rewrites it, the
same way Claude's hook scripts are copied on every configure. Plain JavaScript
(ES module), not TypeScript, so a test can run it under `node` with no build
step.

**3. Formatting runs right after each successful `edit`/`write`, and the agent
is told.** The extension runs the script on the edited path. If the bytes
changed, it appends the changed line span and the new text of that span
(capped) to the tool result, so the next `oldText` can match without a
re-read. Lint findings are appended the same way Claude gets them. This keeps
Pi's timing the same as Claude's and OpenCode's. pi-lens's own choice, to
delay formatting until the turn ends, is not used.

**4. pi-lens is installed last and only diagnoses.** It is appended to
`piExtensions` last (its native build is the likeliest install to fail).
Devgeta sets two keys in pi-lens's global config, `format.enabled: false` and
`autofix.enabled: false`, keeping every other key. Configure without `--force`
keeps an existing value the user set; `--force` sets both. The file is
`PI_LENS_CONFIG_PATH` when set, else `~/.pi-lens/config.json`. pi-lens can
also read `$PI_CODING_AGENT_DIR/extensions/pi-lens.json`, but devgeta does not
follow `PI_CODING_AGENT_DIR` anywhere (ADR-0059 §5), so it does not follow it
here either. ADR-0060 §1 stays true: this is pi-lens's config, not Pi's
settings.

**Amends:** ADR-0059 §7 and ADR-0060 §8 (formatter parity with the other
agents is now in scope for Pi, via the shared script).

**5. No tool downloads. Mason is the only source.** When the extension loads,
it puts the Mason bin directory first on `process.env.PATH` and sets both
pi-lens install switches to `1`, unless the user already set them. A
language Mason does not cover gets no language server from pi-lens.

**6. One Mason path.** The script owns the Mason directory. The extension
keeps a copy, and a test (added in the cycle's test phase) checks the two
match.

**7. OpenCode is not changed in this decision.** It keeps its inline
`formatter` commands. Moving it to the shared script is a follow-up. Pi is
outside the two-agent sync contract (ADR-0059), so that test does not change.

## Consequences

- All three agents format with the same binaries and settings. Claude and Pi
  read one table, and OpenCode keeps its own copy until the follow-up.
- Pi's `bash` tool children also see Mason first on `PATH`. That is a
  visible behavior change for commands the agent runs.
- Tools pi-lens downloaded before this change still win over Mason, because
  pi-lens checks its own directories before `PATH`. `docs/apps/pi.md` tells
  users to delete `~/.pi-lens/tools` and `~/.pi-lens/bin` to switch. Devgeta
  does not delete them.
- Without Mason (Neovim never configured), the script skips every formatter
  silently, as `format.sh` does today. pi-lens then has no language servers
  either.
- pi-lens's read-before-edit guard should accept the formatted bytes, because
  it hashes the file in its own `tool_result` handler, which runs after
  ours. This is read from source, not run. The cycle's manual run must
  confirm it. If it fails, this ADR is amended before Phase 2.
- Some lint findings can come twice (golangci-lint from the script and from
  pi-lens when a project has a golangci config). Accepted. Removing it would
  mean per-linter special cases in a general script.
