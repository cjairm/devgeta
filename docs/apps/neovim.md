# Neovim

Devgeta installs and configures [Neovim](https://neovim.io/), a hyperextensible
Vim-based text editor.

- **Module:** `internal/apps/neovim/`

## After Installation

**Start Neovim:**

```bash
nvim
```

**Clone kickstart.nvim (recommended):**

If you want to start with the kickstart configuration:

```bash
git clone https://github.com/nvim-lua/kickstart.nvim.git "${XDG_CONFIG_HOME:-$HOME/.config}"/nvim
```

> **Note:** If forking the repo, replace `nvim-lua` with `<your_github_username>` in the command above.

**Track package lockfile:**

You likely want to remove `nvim-pack-lock.json` from your fork's `.gitignore` file — it's ignored in the kickstart repo to make maintenance easier, but it's recommended to track it in version control (see `:help vim.pack-lockfile`).

## Uninstall (Manual)

Neovim requires manual cleanup. Follow these steps:

**Step 1 — Remove config:**

```bash
rm -rf ~/.config/nvim
```

**Step 2 — Remove data and plugins:**

```bash
rm -rf ~/.local/share/nvim
```

**Step 3 — Remove state:**

```bash
rm -rf ~/.local/state/nvim
```

**Step 4 — Remove cache:**

```bash
rm -rf ~/.cache/nvim
```

Or as a single command:

```bash
rm -rf ~/.config/nvim ~/.local/share/nvim ~/.local/state/nvim ~/.cache/nvim
```
