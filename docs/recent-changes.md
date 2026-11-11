# Recent Changes

Short prose summaries of changes whose _reasoning_ isn't obvious from the code
or the commit. This is a reading aid, not a record — the permanent record is
`docs/decisions/` (why we chose it), `docs/plans/cycles/` (what the work was),
and git history (what changed).

**Pruning rule:** keep entries from the last two releases. Delete anything older
in the same pass that adds a new entry. If deleting an entry would lose
something a reader still needs, that thing belongs in an ADR or a guide — put it
there first, then delete. This file must not grow without bound; that is exactly
why it no longer lives in `CLAUDE.md`.

**Last updated:** 2026-11-11

---

## Recent changes

- tmux-resurrect restores Pi panes and keeps Claude's env (2026-11-11, v2.5.0).
  Restored panes now relaunch `pi --continue` next to claude and opencode. Pi is
  a node script, but it sets its process title to `pi`, so resurrect's plain
  `pi` match works without a `~` substring pattern (which would also catch
  `pip`, `api…`). The claude restore line is rendered from
  `constants.ClaudeLaunch`, the recipe behind the `cc` alias, so a restored pane
  carries `CLAUDE_CODE_NO_FLICKER=1` like a fresh one. The template data is now
  one `tmux.TemplateData` type that the golden test also builds, so a new field
  can't leave the test rendering a different shape than `dg configure tmux`.

- Pi reports its activity to tmux (2026-11-10, v2.4.0). Pi panes now show in
  the `dg ws` agents section, flag their window in the status bar, and can ding,
  like Claude Code and OpenCode. Rather than a third copy of the state logic,
  Claude Code's `agent-state.sh` moved to `configs/devgeta/` and takes the
  coder's name as an optional second argument; Pi's `devgeta-notify.js`
  extension calls it on Pi's lifecycle events. With no second argument the
  script still reports `claude`, because Claude Code's `settings.json` is only
  re-rendered on `--force` and older files call it with one argument. Two Pi
  details are not obvious: `agent_end` is not the end of a run (Pi may retry or
  continue), so idle is written on `agent_settled`, and since that event only
  says whether the run was aborted, the extension remembers the last
  `turn_end` outcome to tell done from error. Pi has no permission prompts, so
  "blocked" means an extension's dialog is waiting on you during a run.
  Checked live in a real Pi session: start, busy, idle, blocked, quit.

## Recent specs completed

- `specs/001-binary-dist-audit/` — Go embed, text/template for config generation
- `specs/002-debian-package-fixes/` — Strategy pattern, package mappings, exponential backoff downloads
