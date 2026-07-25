-- [devgeta]
-- vim-obsession keeps a Session.vim continuously updated so tmux-resurrect
-- can restore Neovim with files and splits intact after a reboot
-- (`@resurrect-strategy-nvim 'session'` in tmux.conf). Run `:Obsession`
-- once in a project to start tracking; it re-tracks automatically after
-- every restore.
local gh = require("kickstart.utils").gh

vim.pack.add({ gh("tpope/vim-obsession") })
-- [/devgeta]
