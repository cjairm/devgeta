# ADR-0064 — Pi hides thinking blocks by default

**Date:** 2026-11-06
**Status:** ACCEPTED

## Context

Pi prints a model's whole thinking text into the transcript by default. Claude
Code and OpenCode, the other two coders devgeta sets up, collapse it to a short
label. Pi already supports the same thing (checked against Pi 1.5.0's
`docs/settings.md`, `docs/keybindings.md` and
`dist/modes/interactive/components/assistant-message.js`):

- `hideThinkingBlock: true` in `~/.pi/agent/settings.json` renders each thinking
  block as a single muted `Thinking...` line.
- Left-clicking that line expands just that block, and clicking again collapses
  it. This works in fullscreen mode, Pi's default.
- `Ctrl+T` toggles every block and saves the choice back to `hideThinkingBlock`.

ADR-0061 §3 limits devgeta to one key in that file, `theme`.

## Decision

**1. Devgeta writes a second key in Pi's settings: `"hideThinkingBlock": true`.
This amends ADR-0061 §3.** Both keys live in one table (`piSettingDefaults` in
`internal/apps/pi/pi.go`) and go through the same read-modify-write that keeps
every other key and refuses a file it cannot parse.

**2. The same soft/force rules apply as for `theme`.** `dg configure pi` writes
a key only when it is missing, so a choice the user made with `Ctrl+T` (which
writes this key) stays. `dg configure pi --force` writes both. A soft run that
finds every key present does not rewrite the file.

**3. The collapsed label is left at Pi's default.** Pi lets an extension change
the label (`ctx.ui.setHiddenThinkingLabel`), but the label is fixed text and
cannot show elapsed time the way Claude Code does, so a custom extension would
add nothing worth maintaining.

## Consequences

- New installs collapse thinking, which matches the other two coders. Nothing
  is lost, because one click opens a block.
- Existing users whose settings file has no `hideThinkingBlock` key get it on
  their next `dg configure pi`. That changes what they see, but `Ctrl+T` undoes
  it and their choice is kept from then on.
- `--force` resets a user who chose to show thinking. That is what `--force`
  means everywhere else in devgeta.
- This adds a second key devgeta writes in a file Pi owns, which means more
  exposure if Pi renames the setting. That is the same kind of risk ADR-0061
  already accepted for `theme`.
