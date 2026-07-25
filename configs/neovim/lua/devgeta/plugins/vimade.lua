-- [devgeta]
-- Dim Neovim when its tmux pane loses focus, matching the tmux-level dim on
-- plain panes (tmux's window-style fg only affects default-colored text,
-- which Neovim's colorscheme overrides — so Neovim must dim itself).
-- Requires `focus-events on` in tmux.conf so FocusLost/FocusGained arrive.
-- Also dims inactive splits inside Neovim (vimade's default), keeping one
-- "inactive = dimmed" convention across tmux and Neovim.
local gh = require("kickstart.utils").gh

vim.pack.add({ gh("TaDaa/vimade") })
require("vimade").setup({
	-- Our Normal bg is transparent (see devgeta.transparent), so vimade needs
	-- the real backdrop to blend faded colors against: the shared Gruvbox
	-- dark0_hard used by Alacritty and the tmux status bar.
	basebg = "#282828",
	-- Matches the tmux inactive-pane dim: #ebdbb2 -> #928374 over #282828
	-- is ~0.55 of the way to the background.
	fadelevel = 0.55,
	-- Fade the whole editor when the terminal/tmux pane loses focus.
	enablefocusfading = true,
})
-- [/devgeta]
