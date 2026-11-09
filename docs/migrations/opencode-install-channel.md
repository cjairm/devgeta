# opencode moved off Homebrew (macOS only)

devgeta used to install opencode with `brew install opencode` on macOS. It now
runs opencode's own install script on both macOS and Linux, which puts the
binary in `~/.opencode/bin` instead of Homebrew's prefix.

Nothing removes the Homebrew copy for you. `dg uninstall opencode` no longer
calls `brew uninstall`, so if you installed opencode through devgeta on macOS
before this release, you end up with two copies on disk until you remove the old
one by hand.

Linux users: nothing to do. That side already used the install script.

## Do you need this?

```bash
brew list opencode >/dev/null 2>&1 && echo "brew copy present"
```

Nothing printed means you have no Homebrew copy and there is nothing to do.

## Steps

```bash
brew uninstall opencode
dg install --only opencode
dg configure devgeta --force
```

1. `brew uninstall opencode` removes the old copy. This does not touch
   `~/.config/opencode`, so your config, themes and plugins stay.
2. `dg install --only opencode` runs the official install script, which puts the
   binary in `~/.opencode/bin`.
3. `dg configure devgeta --force` regenerates `devgeta.zsh`, which is what puts
   `~/.opencode/bin` on your PATH. `dg install` alone rewrites that file only
   when it is missing, so an existing install needs the `--force`.

Then open a new shell and check which copy you got:

```bash
which opencode   # expect ~/.opencode/bin/opencode
opencode --version
```

Upgrade devgeta before step 3. `dg configure` reads the template out of the
binary that runs it, so an old binary redeploys a `devgeta.zsh` without the PATH
entry.

## If you skip this

devgeta prepends `~/.opencode/bin` to PATH, so in any shell that sources
`devgeta.zsh` the devgeta-installed copy wins and the leftover Homebrew one is
just dead weight. It can still surface where `devgeta.zsh` is not loaded — a
plain login shell, a CI step, an editor that builds its own environment — and
there the two copies can be different versions. Removing it avoids the question.

## Running a different version

devgeta now installs a pinned OpenCode version and turns OpenCode's own
updates off (see [docs/apps/opencode.md](../apps/opencode.md#pinned-version)),
so a bad upstream release no longer reaches you through `dg install`. To run
another version anyway, use OpenCode 2's script:

```bash
curl -fsSL https://opencode.ai/v2/install | bash -s -- --version <version>
```

That overwrites `~/.opencode/bin/opencode` in place. `dg install` leaves a
version newer than the pin alone, and replaces an older one with the pin.

Use that rather than `dg uninstall opencode` followed by a reinstall —
`dg uninstall` also deletes `~/.config/opencode`, taking your config, themes and
plugins with it.
