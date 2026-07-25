local gh = require("kickstart.utils").gh

vim.pack.add({ gh("folke/which-key.nvim") })
require("which-key").setup({
	-- Delay between pressing a key and opening which-key (milliseconds)
	delay = 0,
	icons = { mappings = vim.g.have_nerd_font },
	-- Document existing key chains
	spec = {
		{ "<leader>s", group = "[S]earch", mode = { "n", "v" } },
		{ "<leader>t", group = "[T]oggle" },
		{ "<leader>h", group = "Git [H]unk", mode = { "n", "v" } }, -- Enable gitsigns recommended keymaps first
		{ "gr", group = "LSP Actions", mode = { "n" } },
		-- [devgeta]
		{ "<leader>c", group = "[C]opy path" },
		{ "<leader>b", group = "[B]uffer" },
		{ "<leader>p", group = "[P]lugins" },
		{ "<leader>w", group = "[W]indow" },
		{ "<leader>l", group = "[L]azygit" },
		{ "<leader>f", desc = "[F]ormat buffer", mode = { "n", "v" } },
		-- [/devgeta]
	},
})
