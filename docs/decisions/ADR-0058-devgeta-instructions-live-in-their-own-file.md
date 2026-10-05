# ADR-0058 — Devgeta's agent instructions live in their own file

**Date:** 2026-10-05
**Status:** ACCEPTED

## Context

Devgeta ships an opinionated default for how the two AI coders answer and
write code: short answers shaped to the task (a quick answer, an
investigation, an explanation of something new), reuse before writing, short
readable functions, and names that still make sense years later. These are
general defaults anyone can adopt, in the same way as the shared Gruvbox palette, so they belong in
the product (principle 8).

Each agent already has a global instructions file, but **the user owns it**:
`~/.claude/CLAUDE.md` and `~/.config/opencode/AGENTS.md`. Principle 4 says
devgeta never overwrites user edits. rtk faces the same problem and solves it
by owning `~/.claude/RTK.md` and adding a single `@RTK.md` import line to
`CLAUDE.md`.

Options considered:

1. **Write devgeta's text into the user's file** (replace or splice a marked
   block). Rejected: it edits content the user owns, and a marked block
   breaks as soon as the user edits around it.
2. **Own a separate file and point each agent at it.** Chosen.

There was a second question: if a user deletes the import line, should the
next `dg configure` add it back?

## Decision

- The text lives once, in `configs/shared/DEVGETA.md`. Every configure
  overwrites both copies, `~/.claude/DEVGETA.md` and
  `~/.config/opencode/DEVGETA.md`, because devgeta owns them.
- **OpenCode** loads its copy through the `instructions` field of
  `opencode.json`, which devgeta already renders. The path is absolute, so it
  respects `XDG_CONFIG_HOME`. OpenCode loads these files in addition to
  `AGENTS.md`, never in place of it.
- **Claude Code** has no equivalent setting, so devgeta appends `@DEVGETA.md`
  to `~/.claude/CLAUDE.md`. Nothing else in that file is touched.
- The import line is added **on first configure only**. Global config records
  that it was added (`integrations.claude_instructions_imported`). After that,
  devgeta never re-adds it, so a user who deletes the line keeps it deleted.
  If the line is already there, it is not added a second time.

## Consequences

- One source file means the two agents cannot drift apart. A test checks that
  both deploy the same bytes.
- Users get the defaults without editing anything themselves, and their own
  instruction files stay theirs.
- Opting out of the import on Claude is permanent and takes one line. Opting
  out on OpenCode means removing the `instructions` entry, but devgeta
  re-renders `opencode.json` on `--force`. That is no different from any other
  setting in that file.
- A user who already wrote similar rules into their own file loads them
  twice until they remove their copy.
