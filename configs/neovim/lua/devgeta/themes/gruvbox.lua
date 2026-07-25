-- [devgeta]
local gh = require("kickstart.utils").gh

vim.pack.add({ gh("ellisonleao/gruvbox.nvim") })
vim.o.background = "dark"
vim.cmd.colorscheme("gruvbox")
-- [/devgeta]
