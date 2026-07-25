local gh = require("kickstart.utils").gh

-- [[ Formatting ]]
vim.pack.add({ gh("stevearc/conform.nvim") })
require("conform").setup({
	notify_on_error = false,
	-- [devgeta]
	-- format_on_save = function(bufnr)
	-- 	-- You can specify filetypes to autoformat on save here:
	-- 	local enabled_filetypes = {
	-- 		-- lua = true,
	-- 		-- python = true,
	-- 	}
	-- 	if enabled_filetypes[vim.bo[bufnr].filetype] then
	-- 		return { timeout_ms = 500 }
	-- 	else
	-- 		return nil
	-- 	end
	-- end,
	-- [/devgeta]
	default_format_opts = {
		lsp_format = "fallback", -- Use external formatters if configured below, otherwise use LSP formatting. Set to `false` to disable LSP formatting entirely.
	},
	-- You can also specify external formatters in here.
	-- [devgeta]
	formatters_by_ft = {
		lua = { "stylua" },
		-- You can use 'stop_after_first' to run the first available formatter from the list
		javascript = { "prettierd", "prettier", stop_after_first = true },
		typescript = { "prettierd", "prettier", stop_after_first = true },
		javascriptreact = { "prettierd", "prettier", stop_after_first = true },
		typescriptreact = { "prettierd", "prettier", stop_after_first = true },
		svelte = { "prettierd", "prettier", stop_after_first = true },
		css = { "prettierd", "prettier", stop_after_first = true },
		html = { "prettierd", "prettier", stop_after_first = true },
		json = { "prettierd", "prettier", stop_after_first = true },
		yaml = { "prettierd", "prettier", stop_after_first = true },
		markdown = { "prettierd", "prettier", stop_after_first = true },
		graphql = { "prettierd", "prettier", stop_after_first = true },
		go = { "goimports", "gofmt", "golines" },
		sh = { "shfmt" },
		-- Conform can also run multiple formatters sequentially
		python = { "isort", "black" },
	},
	-- Only run prettier when a config file is present in the project
	formatters = {
		prettier = {
			condition = function(_, ctx)
				return vim.fs.find({
					"prettier.config.js",
					"prettier.config.cjs",
					".prettierrc",
					".prettierrc.js",
					".prettierrc.json",
					".prettierrc.yml",
				}, { path = ctx.filename, upward = true })[1] ~= nil
			end,
		},
	},
	-- [/devgeta]
})

vim.keymap.set({ "n", "v" }, "<leader>f", function()
	require("conform").format({ async = true })
end, { desc = "[F]ormat buffer" })
