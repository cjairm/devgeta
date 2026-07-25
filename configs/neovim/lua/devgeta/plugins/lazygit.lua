-- [devgeta]
local gh = require("kickstart.utils").gh

vim.pack.add({ gh("nvim-lua/plenary.nvim") })
vim.pack.add({ gh("kdheepak/lazygit.nvim") })
vim.keymap.set("n", "<leader>lg", "<cmd>LazyGit<cr>", { desc = "LazyGit" })
-- [/devgeta]
