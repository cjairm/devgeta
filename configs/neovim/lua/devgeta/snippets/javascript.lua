local ls = require("luasnip")
local rep = require("luasnip.extras").rep

local s = ls.snippet
local t = ls.text_node
local i = ls.insert_node
local f = ls.function_node

local filename = function()
	return vim.fn.expand("%:t")
end

local current_function_name = function()
	local ok, node = pcall(vim.treesitter.get_node)
	if not ok or not node then
		return "function"
	end

	while node do
		if node:type():match("function") then
			-- Get the text of the function name node
			local name_node = node:field("name")[1]
			if name_node then
				local ok_text, text = pcall(vim.treesitter.get_node_text, name_node, 0)
				if ok_text then
					return text
				end
			end
		end
		node = node:parent()
	end
	return "function"
end

local js_snippets = {
	s("dlog", {
		t("console.log('["),
		f(filename),
		t("] "),
		f(current_function_name),
		t(":"),
		i(1, "variable"),
		t(" -> ', "),
		rep(1),
		t(");"),
	}),
}

ls.add_snippets("javascript", js_snippets)
ls.add_snippets("typescript", js_snippets)
