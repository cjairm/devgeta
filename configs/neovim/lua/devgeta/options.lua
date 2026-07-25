-- [devgeta]
-- Extra vim options and filetype overrides loaded after init.lua core settings.

-- Treat .blade.php files as HTML for syntax and LSP purposes
vim.filetype.add({
	pattern = { [".*%.blade%.php"] = "html" },
})

-- [[ Indentation ]]
vim.o.autoindent = true  -- copy indent from current line when starting a new one
vim.o.shiftwidth = 2     -- spaces per indent level

-- [[ Display ]]
vim.o.numberwidth = 1    -- minimum width of the line number column
vim.o.cc = "80"          -- highlight column 80 as a soft line-length guide
vim.o.ruler = true       -- show cursor position in the status line
vim.o.relativenumber = true

-- [/devgeta]
