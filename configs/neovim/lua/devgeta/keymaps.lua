-- [devgeta]
-- Personal keybindings not covered by kickstart or individual plugin configs.

-- [[ Plugins ]]
vim.keymap.set("n", "<leader>pu", function()
	vim.pack.update()
end, { desc = "[P]lugins [U]pdate" })
vim.keymap.set("n", "<leader>ps", function()
	vim.pack.update(nil, { offline = true })
end, { desc = "[P]lugins [S]tatus" })

-- [[ Diagnostics ]]
vim.keymap.set("n", "<leader>e", vim.diagnostic.open_float, { desc = "Show diagnostic [E]rror messages" })

-- [[ Buffers ]]
vim.keymap.set("n", "<leader>bd", ":bd<CR>", { desc = "Close current buffer" })

-- [[ Path copying — useful for sharing file references across tools ]]
vim.keymap.set(
	"n",
	"<leader>cpr",
	':let @+ = fnamemodify(expand("%:p"), ":.")<CR>',
	{ desc = "[C]opy [P]ath [R]elative" }
)
vim.keymap.set("n", "<leader>cpa", ':let @+ = expand("%:p")<CR>', { desc = "[C]opy [P]ath [A]bsolute" })

-- [[ Windows ]]
vim.keymap.set("n", "<leader>wv", ":wincmd v<CR>", { desc = "Creates [W]indow [V]ertical" })
vim.keymap.set("n", "<leader>wr", ":wincmd r<CR>", { desc = "[W]indow [R]otates" })

-- [/devgeta]
