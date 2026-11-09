# OpenCode 2, and pinned coder versions

devgeta now installs exact versions of the AI coders and turns their
self-updates off:

- **OpenCode 2.2.6.** OpenCode 2 has a new installer, reads a new config
  format, and cannot run devgeta's OpenCode 1 plugins. devgeta now writes its
  config in OpenCode 2's format. See [docs/apps/opencode.md](../apps/opencode.md).
- **Claude Code 2.1.319.** `settings.json` now sets `DISABLE_AUTOUPDATER=1`.
  See [docs/apps/claude.md](../apps/claude.md).
- **Pi** is not pinned. Its installer always installs the latest release.

An upgrade does not apply any of this by itself: the coders' binaries and
config files only change when you re-run the commands below.

## Does this affect you?

```bash
~/.opencode/bin/opencode --version                   # 1.x means yes
ls ~/.config/opencode/plugin 2>/dev/null             # devgeta's OpenCode 1 plugins
grep -c DISABLE_AUTOUPDATER ~/.claude/settings.json  # 0 means yes
```

If you installed neither coder through devgeta, there is nothing to do.

## Steps

1. **Update devgeta** to this release.

2. **Upgrade the binaries.** This replaces an OpenCode 1 install, or an
   older Claude Code, with the pinned version. A newer version you installed
   yourself is left alone.

   ```bash
   dg install --only opencode
   dg install --only claude
   ```

3. **Rewrite the configs.** `--force` regenerates each coder's config, so hand
   edits to `opencode.json` and `~/.claude/settings.json` are lost. Copy out
   anything you want to keep first.

   ```bash
   dg configure opencode --force
   dg configure claude --force
   ```

   For OpenCode this rewrites `opencode.json`, re-renders the theme, sets
   `theme.name` in `cli.json` (your other `cli.json` settings stay), installs
   the two plugins under `~/.config/opencode/plugins/`, and deletes the old
   `plugin/` folder.

4. **Restart OpenCode's background service,** so it loads the new config and
   the agent-config guard:

   ```bash
   opencode service restart
   ```

5. **Restart every Claude Code and OpenCode terminal.** Settings and the
   OpenCode activity plugin (the `dg ws` status dot) load when a session
   starts.

## Check it worked

```bash
opencode --version                    # opencode v2.2.6
opencode debug config | head -40      # the loaded config, with warnings for anything ignored
ls ~/.config/opencode/plugins         # devgeta-agent-config-guard  devgeta-notify
```

In OpenCode, asking it to create `.opencode/test.md` should be refused with
the guard's message.

## What changes for you

- **Updates come from devgeta.** OpenCode (`"update": "disable"`) and Claude
  Code (`DISABLE_AUTOUPDATER`) no longer update themselves. To run another
  version anyway, install it with the coder's own script. devgeta leaves a
  newer version alone.
- **Bypassing the agent-config guard in OpenCode** is set on its background
  service, not in your shell, because plugins run inside that service:

  ```bash
  opencode service set env DEVGETA_SKIP_AGENT_CONFIG_GUARD 1
  opencode service restart
  # undo: opencode service unset env DEVGETA_SKIP_AGENT_CONFIG_GUARD && opencode service restart
  ```

  With `opencode --standalone`, exporting it in the launching shell still
  works. Claude Code is unchanged.

- **Paths outside the project still ask in OpenCode.** OpenCode 2 would allow
  them by default; devgeta's config keeps the prompt.
- **The OpenCode status dot follows the session the pane is showing.** A
  session left running in a background tab does not update it.

## Left behind, and harmless

OpenCode 1 left `package.json`, `package-lock.json` and `node_modules/` in
`~/.config/opencode` and `~/.opencode`. OpenCode 2 does not use them for
devgeta's plugins and starts just as fast with them present. devgeta does not
delete them, because it did not create them. Remove them by hand if you want
the space back and no plugin of your own depends on them.

## If something goes wrong

- **OpenCode answers every prompt with "User not found."** That is the model
  provider rejecting your API key (OpenRouter returns it for a deleted or
  revoked key), not this upgrade. Save a working key with
  `opencode auth login`, then `opencode service restart`.
- **The theme or a permission rule seems ignored.** `opencode debug config`
  shows what OpenCode actually loaded. If it still shows a `theme` key in
  `opencode.json` or a `permission` object, step 3 has not run with the new
  devgeta binary.
