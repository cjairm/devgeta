# Agent packs

devgeta now installs **agent packs** (`superpowers`, `ponytail`, `devgeta`) into
Claude Code, OpenCode and Pi. Upgrading changes nothing by itself; this guide is
for adding the packs to coders you already had, and for opting out.

## What changed

- `dg install` installs the three packs, but **only into coders that run
  installed**. A coder you already had is left alone, even if it is upgraded to
  the pinned version.
- `global_config.yaml` gains an `agent_packs:` section recording what devgeta
  installed. It is additive: a config without it loads and saves as before.
- The `devgeta` pack puts an `@DEVGETA.md` line in `~/.claude/CLAUDE.md` when no
  line there imports it, and copies `~/.claude/DEVGETA.md`.

## The devgeta pack now ships skills

If you installed the `devgeta` pack before it carried skills, run
`dg agent-pack install devgeta` (or `dg agent-pack update`) to add them. Updating
devgeta alone changes nothing on disk.

## Check whether it affects you

```bash
dg agent-pack list
```

Every coder showing `not installed` for a pack, and no `agent_packs:` in
`~/.config/devgeta/global_config.yaml`, means you have no packs yet. Nothing
breaks without them.

## Add the packs to coders you already have

1. Run the commands `dg install` printed, or these, for each coder
   (`claude`, `opencode`, `pi`):

   ```bash
   dg agent-pack install devgeta --coder claude
   dg agent-pack install superpowers --coder claude
   dg agent-pack install ponytail --coder claude
   ```

   Leave out `--coder` to install into every installed coder.
2. Check with `dg agent-pack list`.
3. Start a new session in each coder. Claude Code also lists them in `/plugin`.

## Skip the packs

```bash
dg install --skip agent-packs
```

To remove one later: `dg agent-pack uninstall <pack>`. To stop `@DEVGETA.md`
coming back on every install or update, uninstall the `devgeta` pack: deleting
the line by hand does not stick.

## If something goes wrong

- **"already installed another way."** You have Superpowers from
  `claude-plugins-official`, or `pi install git:…`. devgeta stops and prints the
  command that removes that copy, then run `dg agent-pack install` again. It never
  removes a copy it didn't install.
- **A warning that `node` or `bash` is not on PATH.** The pack is installed; the
  hooks that need that command won't run until it is.
- **You already had an `@DEVGETA.md` line, or your own `DEVGETA.md`.** devgeta
  adds nothing to `CLAUDE.md` when a line already imports the file, and on
  uninstall removes the line only if it added it and it is unchanged. It does
  overwrite `~/.claude/DEVGETA.md`, which belongs to devgeta; put your own text in
  `~/.config/devgeta/agent-packs/devgeta/DEVGETA.md` and run
  `dg agent-pack update devgeta`.
- **Hand edits to `~/.claude/settings.json` or `~/.config/opencode/opencode.json`**
  are lost when devgeta re-renders them (`dg configure --force`, and for
  `opencode.json` any `dg agent-pack install` or `uninstall`). The pack entries are
  rendered back from `agent_packs:`.
