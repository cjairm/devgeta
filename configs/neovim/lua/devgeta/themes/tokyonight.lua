local gh = require("kickstart.utils").gh

vim.pack.add({ gh("folke/tokyonight.nvim") })
---@diagnostic disable-next-line: missing-fields
require("tokyonight").setup({
	styles = {
		comments = { italic = false }, -- Disable italics in comments
	},
})

-- Load the colorscheme here.
-- Like many other themes, this one has different styles, and you could load
-- any other, such as 'tokyonight-storm', 'tokyonight-moon', or 'tokyonight-day'.
vim.cmd.colorscheme("tokyonight-night")
