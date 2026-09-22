# `devgeta` not found by agents, hooks, or cron

## What changed

`install.sh` puts `~/.local/bin` on your PATH by writing to your **shell
config** (`~/.zshrc` for zsh), which only _interactive_ shells read. Anything
that shells out without a profile — an AI coding agent running `zsh -c`, a git
or editor hook, cron, launchd, an app started from Finder — inherited a PATH
without it and reported `devgeta` as missing while it sat installed and working.

devgeta's `~/.zshenv` script now puts the install directory on PATH as well.
`~/.zshenv` is the one file every zsh reads, login or not, interactive or not.

Upgrading the binary is not enough on its own: that script ships **inside** the
binary and is only written to disk when a `dg` command re-extracts the embedded
configs.

**zsh only.** devgeta does not wire `~/.zshenv` for bash users, and bash has no
unconditional equivalent (`$BASH_ENV` applies to non-interactive bash only, and
the parent process has to export it). On bash, add the directory to PATH
yourself in whatever file your non-interactive shells read.

## Does this affect you?

Ask a shell with no profile whether it can find devgeta:

```bash
env -i HOME="$HOME" PATH=/usr/bin:/bin zsh -c 'command -v devgeta || echo MISSING'
```

`MISSING` means yes. A path means you are already fine.

You can also check the script directly — if this prints nothing, it predates the
change:

```bash
grep -c 'local/bin' ~/.local/share/devgeta/configs/zsh/zshenv.zsh
```

## Fix it

1. Upgrade devgeta, so the binary carries the new script:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/cjairm/devgeta/main/install.sh | bash
   ```

2. Re-extract the embedded configs and re-check the `~/.zshenv` wiring:

   ```bash
   dg configure devgeta
   ```

   Any `dg configure` re-extracts the configs tree when it belongs to an older
   build; naming `devgeta` also re-checks the `~/.zshenv` line itself. Neither
   overwrites your own edits.

3. Confirm, with the same no-profile check:

   ```bash
   env -i HOME="$HOME" PATH=/usr/bin:/bin zsh -c 'command -v devgeta'
   ```

Already-running programs keep the PATH they started with. Restart the agent,
editor, or tmux server that could not find devgeta — a new shell is not enough
if its parent is the one holding the old environment.

## If it still cannot find it

- **`~/.zshenv` does not source devgeta at all.** Check:

  ```bash
  grep devgeta ~/.zshenv
  ```

  Nothing means the wiring was never written — devgeta only writes it when your
  shell config is `~/.zshrc`. Run `dg configure devgeta` and check again.

- **Your tool does not use zsh.** If it runs `sh -c` or `bash -c`, `~/.zshenv`
  is never read. Point the tool at the absolute path, `~/.local/bin/devgeta`, or
  set PATH in the tool's own environment.

- **Something later in startup replaces PATH** rather than appending to it. A
  line like `export PATH=/usr/bin:/bin` in `~/.zshrc` or a framework's config
  discards what `~/.zshenv` set. `echo $PATH` in an interactive shell and look
  for `~/.local/bin`; if it is missing there too, the replacement is the
  problem, not this change.
