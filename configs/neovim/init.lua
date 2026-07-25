--[[

=====================================================================
==================== READ THIS BEFORE CONTINUING ====================
=====================================================================
========                                    .-----.          ========
========         .----------------------.   | === |          ========
========         |.-""""""""""""""""""-.|   |-----|          ========
========         ||                    ||   | === |          ========
========         ||   KICKSTART.NVIM   ||   |-----|          ========
========         ||                    ||   | === |          ========
========         ||                    ||   |-----|          ========
========         ||:Tutor              ||   |:::::|          ========
========         |'-..................-'|   |____o|          ========
========         `"")----------------(""`   ___________      ========
========        /::::::::::|  |::::::::::\  \ no mouse \     ========
========       /:::========|  |==hjkl==:::\  \ required \    ========
========      '""""""""""""'  '""""""""""""'  '""""""""""'   ========
========                                                     ========
=====================================================================
=====================================================================

What is Kickstart?

  Kickstart.nvim is *not* a distribution.

  Kickstart.nvim is a starting point for your own configuration.
    The goal is that you can read every line of code, top-to-bottom, understand
    what your configuration is doing, and modify it to suit your needs.

    Once you've done that, you can start exploring, configuring and tinkering to
    make Neovim your own! That might mean leaving Kickstart just the way it is for a while
    or immediately breaking it into modular pieces. It's up to you!

    If you don't know anything about Lua, I recommend taking some time to read through
    a guide. One possible example which will only take 10-15 minutes:
      - https://learnxinyminutes.com/docs/lua/

    After understanding a bit more about Lua, you can use `:help lua-guide` as a
    reference for how Neovim integrates Lua.
    - :help lua-guide
    - (or HTML version): https://neovim.io/doc/user/lua-guide.html

Kickstart Guide:

  TODO: The very first thing you should do is to run the command `:Tutor` in Neovim.

    If you don't know what this means, type the following:
      - <escape key>
      - :
      - Tutor
      - <enter key>

    (If you already know the Neovim basics, you can skip this step.)

  Once you've completed that, you can continue working through **AND READING** the rest
  of the kickstart init.lua.

  Next, run AND READ `:help`.
    This will open up a help window with some basic information
    about reading, navigating and searching the builtin help documentation.

    This should be the first place you go to look when you're stuck or confused
    with something. It's one of my favorite Neovim features.

    MOST IMPORTANTLY, we provide a keymap "<space>sh" to [s]earch the [h]elp documentation,
    which is very useful when you're not exactly sure of what you're looking for.

  I have left several `:help X` comments throughout the init.lua
    These are hints about where to find more information about the relevant settings,
    plugins or Neovim features used in Kickstart.

   NOTE: Look for lines like this

    Throughout the file. These are for you, the reader, to help you understand what is happening.
    Feel free to delete them once you know what you're doing, but they should serve as a guide
    for when you are first encountering a few different constructs in your Neovim config.

If you experience any errors while trying to install kickstart, run `:checkhealth` for more info.

I hope you enjoy your Neovim journey,
- TJ

P.S. You can delete this when you're done too. It's your config now! :)
--]]

-- ============================================================
-- SECTION 1: FOUNDATION
-- Core Neovim settings, leaders, options, basic keymaps, basic autocmds
-- ============================================================
do
	require("kickstart.foundation")
end

-- ============================================================
-- SECTION 2: PLUGIN MANAGER INTRO
-- vim.pack intro, build hooks
-- ============================================================
do
	require("kickstart.plugin_manager")
end

-- ============================================================
-- SECTION 3: UI / CORE UX PLUGINS
-- guess-indent, gitsigns, which-key, colorscheme, todo-comments, mini modules
-- ============================================================
do
	-- [[ Installing and Configuring Plugins ]]
	--
	-- To install a plugin simply call `vim.pack.add` with its git url.
	-- This will download the default branch of the plugin, which will usually be `main` or `master`
	-- You can also have more advanced specs, which we will talk about later.
	--
	-- For most plugins its not enough to install them, you also need to call their `.setup()` to start them.
	--
	-- For example, lets say we want to install `guess-indent.nvim` - a plugin for
	-- automatically detecting and setting the indentation.
	--
	-- We first install it from https://github.com/NMAC427/guess-indent.nvim
	-- and then call its `setup()` function to start it with default settings.
	require("kickstart.plugins.guess_indent")

	-- Here is a more advanced configuration example that passes options to `gitsigns.nvim`
	--
	-- See `:help gitsigns` to understand what each configuration key does.
	-- Adds git related signs to the gutter, as well as utilities for managing changes
	require("kickstart.plugins.gitsigns")

	-- Useful plugin to show you pending keybinds.
	require("kickstart.plugins.which_key")

	-- [[ Colorscheme ]]
	-- You can easily change to a different colorscheme.
	-- Change the name of the colorscheme plugin below, and then
	-- change the command under that to load whatever the name of that colorscheme is.
	--
	-- If you want to see what colorschemes are already installed, you can use `:Telescope colorscheme`.
	require("devgeta.themes.gruvbox")

	-- Highlight todo, notes, etc in comments
	require("kickstart.plugins.todo_comments")

	-- [[ mini.nvim ]]
	--  A collection of various small independent plugins/modules
	require("kickstart.plugins.mini")

	-- ... and there is more!
	--  Check out: https://github.com/nvim-mini/mini.nvim
end

-- ============================================================
-- SECTION 4: SEARCH & NAVIGATION
-- Telescope setup, keymaps, LSP picker mappings
-- ============================================================
do
	require("kickstart.plugins.telescope")
end

-- ============================================================
-- SECTION 5: LSP
-- LSP keymaps, server configuration, Mason tools installations
-- ============================================================
do
	require("kickstart.plugins.lspconfig")
end

-- ============================================================
-- SECTION 6: FORMATTING
-- conform.nvim setup and keymap
-- ============================================================
do
	require("kickstart.plugins.conform")
end

-- ============================================================
-- SECTION 7: AUTOCOMPLETE & SNIPPETS
-- blink.cmp and luasnip setup
-- ============================================================
do
	require("kickstart.plugins.autocompletion")
end

-- ============================================================
-- SECTION 8: TREESITTER
-- Parser installation, syntax highlighting, folds, indentation
-- ============================================================
do
	require("kickstart.plugins.treesitter")
end

-- ============================================================
-- SECTION 9: OPTIONAL EXAMPLES / NEXT STEPS
-- kickstart.plugins.* examples
-- ============================================================
do
	-- The following comments only work if you have downloaded the kickstart repo, not just copy pasted the
	-- init.lua. If you want these files, they are in the repository, so you can just download them and
	-- place them in the correct locations.

	-- NOTE: Next step on your Neovim journey: Add/Configure additional plugins for Kickstart
	--
	--  Here are some example plugins that I've included in the Kickstart repository.
	--  Uncomment any of the lines below to enable them (you will need to restart nvim).
	--
	-- require 'kickstart.plugins.debug'
	-- require 'kickstart.plugins.indent_line'
	-- require 'kickstart.plugins.autopairs'
	-- [devgeta]
	require("kickstart.plugins.gitsigns")
	require("kickstart.plugins.lint")
	require("kickstart.plugins.neotree")
	-- [/devgeta]

	-- NOTE: You can add your own plugins, configuration, etc from `lua/custom/plugins/*.lua`
	--
	--  Uncomment the following line and add your plugins to `lua/custom/plugins/*.lua` to get going.
	-- [devgeta]
	require("devgeta.plugins.coffee_script")
	require("devgeta.plugins.copilot")
	require("devgeta.plugins.lazygit")
	require("devgeta.plugins.obsession")
	require("devgeta.plugins.tmux_navigator")
	require("devgeta.plugins.vimade")
	-- [/devgeta]
end

-- The line beneath this is called `modeline`. See `:help modeline`
-- vim: ts=2 sts=2 sw=2 et

-- [devgeta] Personal options, keymaps and transparency overrides loaded after plugins
require("devgeta.options")
require("devgeta.keymaps")
require("devgeta.transparent")
-- [/devgeta]
