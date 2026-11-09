# ADR-0069 — Pack customization happens only through a build-time override layer

**Date:** 2026-11-08
**Status:** ACCEPTED

## Context

Claude Code can't drop one skill from a plugin ("plugin skills are not
affected by `skillOverrides`"), and no coder can customize a skill without
editing it. Pi and OpenCode can filter or deny a skill, but that works in two
coders only. We also want the fetched upstream to stay byte-for-byte what
upstream published, so a version bump is "fetch the new tag, re-apply our
layer" ([ADR-0068](ADR-0068-agent-packs-are-a-pinned-upstream-plus-a-build-layer.md)).

## Decision

1. **All removal and customization happens when the tree is built,** on a copy
   of the cached clone. The clone is never edited. One mechanism, the same
   result in all three coders.
2. **Removal:** `dg agent-pack install <pack> --exclude <skill>,...` deletes
   those skills from the built tree. The list is saved in `agent_packs:`. A
   later `--exclude` replaces it, and `--exclude=` clears it. No skill is
   excluded by default.
3. **Overrides are files laid out like the pack,** in two layers copied on top
   in this order: devgeta's (`configs/agent-packs/<pack>/`), then the user's
   (`~/.config/devgeta/agent-packs/<pack>/`). The user's layer wins. An
   override replaces a file at the same path, usually
   `skills/<name>/SKILL.md`, or adds a file inside an existing skill's folder
   that the override needs. It can't add a new skill; devgeta's own skills
   ship in the `devgeta` pack instead.
4. **An override is a full replacement or a wrapper.** A wrapper is a
   `SKILL.md` that contains the text `UPSTREAM.md`. The build keeps
   upstream's original `SKILL.md` beside it under that name. `UPSTREAM.md` is
   always upstream's file, never devgeta's override, so a user's wrapper over
   a skill devgeta also overrides wraps upstream, not devgeta's version.
   Every coder treats only `SKILL.md` as a skill, so `UPSTREAM.md` never loads
   as a second one.
5. **An override keeps upstream's skill `name`,** so other skills' calls still
   reach it (and `superpowers:<name>` still resolves in Claude Code). A wrapper
   also keeps upstream's `description`.
6. **Build checks stop the build and delete the temp tree:**
   - every excluded or overridden skill exists upstream;
   - no kept skill names an excluded one, as `superpowers:<name>` or bare;
   - override names (and wrapper descriptions) match upstream;
   - the files the installers need exist: `.claude-plugin/plugin.json`,
     `package.json` with `pi` and `main`, and every hook file the plugin
     declares. The `hooks` key in `plugin.json` names them (a path or a list of
     paths); when the key is absent Claude Code reads `hooks/hooks.json`, so
     that is the file checked then. Ponytail declares two files and has no
     `hooks/hooks.json`, so a fixed file name would reject it. Inline hooks
     name no file. *(Amended 2026-11-08, during Step 6, after the real
     Ponytail tree contradicted the first wording. Maintainer agreed.)*
7. **A full replacement doesn't follow upstream.** The build records upstream's
   hash for each replaced file; `check` lists skills whose hash changed on a
   version bump so someone re-reads the upstream change.
8. **The new tree replaces the old one by rename.** The new tree is built
   next to the old one. The old tree is renamed aside, the new one is renamed
   into place, and only then is the old one deleted. If the second rename
   fails, the old tree is renamed back. A directory can't be renamed over a
   non-empty one, so this takes two renames, not one.
9. **devgeta's own overrides must pass principle 8:** each has to make sense to
   someone who has never seen this repo. A change that only suits this repo's
   workflow belongs in the user layer or in `CLAUDE.md`. None ship in this
   cycle; each is a separate, reviewed change.

## Consequences

- Fetched upstream bytes stay exact and auditable.
- Every change to exclusions or overrides needs a rebuild. `update` always
  rebuilds. A build is a local copy, so there is no need to track whether the
  user's layer changed.
- Wrappers follow upstream edits better than replacements do; replacements need
  a human at each version bump.
- Excluding a skill that others call is refused at build time, with both
  skills named, instead of failing at runtime.
- Replaces the earlier plan for a wrapper-only ADR and the "Known differences"
  practice.
