# ripgrep

Devgeta installs [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) — a
recursive regex search that respects `.gitignore` and replaces `grep -r`. It
also installs the two tools `rg` pairs with:
[`fd`](https://github.com/sharkdp/fd) (find files) and
[`fzf`](https://github.com/junegunn/fzf) (interactive filter).

- **Module:** `internal/tooling/terminal/dev_tools/ripgrep/`
- **Install:** `brew install ripgrep` / `apt install ripgrep`
- **Configuration:** none — flags and `$RIPGREP_CONFIG_PATH` only

## Search

```bash
rg <pattern>              # current directory tree
rg <pattern> path/to/dir
rg -i <pattern>           # case-insensitive
rg -w <pattern>           # whole word
rg -F '?.'                # fixed string, no regex
```

`rg` skips gitignored, hidden, and binary files. `-u` relaxes that one level at a
time (`-uu` includes hidden, `-uuu` includes binaries); `--no-ignore` relaxes
just the gitignore rules.

## Context

```bash
rg '// TODO' -C 2    # 2 lines around
rg '// TODO' -A 3    # 3 lines after
rg '// TODO' -B 3    # 3 lines before
```

## Narrow by file

```bash
rg -t js '@format'              # by language; rg --type-list shows them all
rg -T test <pattern>            # every type EXCEPT test
rg calledWith -g '*Test.ts'     # glob include
rg promisedRun -g '!*Test.ts'   # glob exclude
```

Use `-t` for languages, `-g` for names.

## Lists instead of matches

```bash
rg -l '// TODO'                       # files that match
rg -l '// TODO' --sort path           # stable order
rg --files-without-match '\b(var|let|const)\b'
rg -c <pattern>                       # match count per file

rg <pattern> -g '!vendor' -l | cut -d/ -f1 | sort -u   # just the top-level dirs
```

## Multiline and look-around

The default engine is line-by-line with no look-around. Two flags fix that:

```bash
rg -U 'foo\n\s*bar'      # -U/--multiline: span newlines
rg --pcre2 'foo(?!bar)'  # --pcre2: look-ahead/behind, backreferences
```

Both together — `target="_blank"` links missing `rel="noopener noreferrer"`,
even when the tag wraps:

```bash
rg --pcre2 -Ul '(?s)<a.*?target="_blank"(?!.*noopener noreferrer).*?>'
```

PCRE2 is slower, so use it only when you need look-around.

## With fd and fzf

`rg` searches contents, `fd` searches names, `fzf` filters either. Devgeta ships
two aliases:

| Alias | Does                                                  |
| ----- | ----------------------------------------------------- |
| `ns`  | pick a file by name (`fd` + `fzf-tmux`), open in nvim |
| `ff`  | pick a file with a `bat` syntax-highlighted preview   |

Pick from files whose _contents_ match:

```bash
rg -l <pattern> | fzf --preview 'bat --style=numbers --color=always {}' | xargs nvim
```

## Uninstall

ripgrep installs with the `terminal` category's dev tools and isn't a registered
app, so `dg uninstall ripgrep` reports an unknown target. Use the package
manager:

```bash
brew uninstall ripgrep     # macOS
sudo apt remove ripgrep    # Debian/Ubuntu
```
