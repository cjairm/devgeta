-- [devgeta]
-- Forces transparent background across UI elements regardless of colorscheme.
-- Wrapped in a ColorScheme autocmd so overrides reapply when the theme changes.

local groups = {
  -- Core editor
  "Normal", "NormalFloat", "NormalNC", "FloatBorder",
  "Pmenu", "Terminal", "EndOfBuffer",
  "FoldColumn", "Folded", "SignColumn",

  -- which-key
  "WhichKeyFloat",

  -- Telescope
  "TelescopeBorder", "TelescopeNormal",
  "TelescopePromptBorder", "TelescopePromptTitle",

  -- Neo-tree
  "NeoTreeNormal", "NeoTreeNormalNC", "NeoTreeVertSplit",
  "NeoTreeWinSeparator", "NeoTreeEndOfBuffer",

  -- nvim-tree
  "NvimTreeNormal", "NvimTreeVertSplit", "NvimTreeEndOfBuffer",

  -- nvim-notify
  "NotifyINFOBody",  "NotifyERRORBody",  "NotifyWARNBody",
  "NotifyTRACEBody", "NotifyDEBUGBody",
  "NotifyINFOTitle", "NotifyERRORTitle", "NotifyWARNTitle",
  "NotifyTRACETitle","NotifyDEBUGTitle",
  "NotifyINFOBorder","NotifyERRORBorder","NotifyWARNBorder",
  "NotifyTRACEBorder","NotifyDEBUGBorder",
}

local function apply()
  for _, g in ipairs(groups) do
    vim.api.nvim_set_hl(0, g, { bg = "none" })
  end
end

apply()
-- Re-applies on theme change so transparency survives `:colorscheme X` mid-session
vim.api.nvim_create_autocmd("ColorScheme", { callback = apply })

-- [/devgeta]
