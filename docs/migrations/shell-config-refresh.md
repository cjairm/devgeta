# Shell config refresh (`ls <path>` fixed)

`dg install` regenerates `devgeta.zsh` only when the file is missing — that is
what keeps an ordinary upgrade from clobbering a shell config you are living
in. This release fixes a line inside that file, so an existing install needs
one `--force` to receive it.

## What changed

The shipped `ls` alias ended in a bare `--icons`:

```zsh
alias ls='eza -lh --group-directories-first --icons'
```

eza's `--icons` takes an _optional_ value, so on some eza versions the flag
consumes whatever follows it as that value. `ls` alone worked; `ls ~/somewhere`
died with

```
error: invalid value '/Users/you/somewhere' for '--icons [<WHEN>]'
  [possible values: always, auto, never]
```

The value is now spelled `--icons=auto`, which no eza version can misread.
`auto` is what the bare flag already meant, so your listings look exactly the
same.

## Do you need this?

```bash
grep -n -- "--icons" "${XDG_DATA_HOME:-$HOME/.local/share}/devgeta/devgeta.zsh"
```

A line ending in `--icons` with no `=` means you are on the old file. A line
with `--icons=auto` means you already have the fix and there is nothing to do.

## Steps

```bash
dg configure devgeta --force
source "${XDG_DATA_HOME:-$HOME/.local/share}/devgeta/devgeta.zsh"
```

`--force` is the part that matters. Without it, `dg configure devgeta`
regenerates `devgeta.zsh` only when the file is missing or the
`devgeta-extended` shell feature was never enabled, so on a working install it
does nothing to the alias. (`dg configure eza --force` works too — every
`dg configure` re-extracts the embedded templates from the running binary
first — but `devgeta` also re-checks the `source` line and `~/.zshenv`.)

Upgrade devgeta first. `dg configure` reads the template out of the binary that
runs it, so an old binary redeploys the old alias.

## If you edited `devgeta.zsh` by hand

`--force` overwrites it. The file is generated (`DO NOT EDIT MANUALLY` at the
top); put your own aliases in `~/.zshrc` after the `source` line instead, where
nothing devgeta does will touch them.
