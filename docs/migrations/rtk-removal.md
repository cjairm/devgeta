# rtk was removed from devgeta

devgeta no longer installs or configures rtk. The `ai-tools` category is gone
too, because rtk was its only app. See
[ADR-0065](../decisions/ADR-0065-devgeta-does-not-ship-rtk.md) for why.

Upgrading doesn't remove anything rtk put on your machine. The new devgeta
doesn't know rtk exists, so `dg uninstall rtk` doesn't work anymore either.
Whatever was installed stays installed, and whatever rtk wired into your AI
coders keeps running until you remove it yourself.

## Do you need this?

```bash
command -v rtk
ls ~/.claude/RTK.md ~/.claude/rtk-shim.sh ~/.config/opencode/plugins/rtk.ts 2>/dev/null
grep -n rtk ~/.claude/settings.json ~/.claude/CLAUDE.md 2>/dev/null
```

If none of these print anything, there is nothing to do.

## Steps

Run them in this order. If you remove the binary while its hook is still
registered, Claude Code tries to run a missing command on every Bash call.

```bash
rtk init -g --uninstall
brew uninstall rtk                    # Debian: sudo rm /usr/local/bin/rtk
rm -rf ~/Library/Application\ Support/rtk
dg configure claude --force
rm ~/.claude/rtk-shim.sh
```

1. `rtk init -g --uninstall` removes four things: rtk's hook from
   `~/.claude/settings.json`, `~/.claude/RTK.md`, the `@RTK.md` line in
   `~/.claude/CLAUDE.md`, and rtk's OpenCode plugin
   (`~/.config/opencode/plugins/rtk.ts`). It changes nothing else in those
   files. Add `--dry-run` first to see the changes without writing them.
2. Remove the binary. rtk has no dependents in Homebrew.
3. Delete rtk's data folder. It holds recent command output and history, which
   can include anything a command printed, secrets included. **This can't be
   undone.** Run `rtk gain` before step 2 if you want to keep the numbers. On
   Linux the folder's location wasn't checked; look in `~/.local/share/rtk` and
   `~/.config/rtk`.
4. `dg configure claude --force`, from the upgraded devgeta, re-renders
   `settings.json` without devgeta's rtk entries: the shim hook (if you had
   turned it on) and two deny rules. It rewrites the whole file, so any edits
   you made to `settings.json` by hand are lost. Run
   `dg configure opencode --force` too, to drop the matching OpenCode rules.
5. `rm ~/.claude/rtk-shim.sh` removes the shim. devgeta copied it to every
   Claude Code install, but `dg configure` never deletes a script it no longer
   ships. Do this after step 4: if `settings.json` still points at the shim,
   deleting the file makes the hook fail on every Bash call.

Then restart every Claude Code and OpenCode session. Hooks and `@` imports load
when a session starts.

## What stays behind

- `rtk_claude_hook` under `integrations:` in `global_config.yaml`. devgeta
  ignores it on load and drops it the next time it saves the file.
- `rtk` under `installed: packages:` in `global_config.yaml`. devgeta no longer
  acts on it. Delete the line by hand if you want it gone.

## If you skip this

rtk keeps working exactly as before, and keeps rewriting your agent's commands.
While its own hook is registered (step 1), it also answers `allow` for the
commands it rewrites, which overrides the ask and deny rules devgeta ships.
Skipping step 5 only leaves an unused file behind.
