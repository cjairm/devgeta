#!/usr/bin/env bash
# PostToolUse hook: format the file Claude just edited, then lint the result and
# feed any findings back to Claude so it can self-correct.
#
# Claude Code delivers the hook payload as JSON on stdin; the edited file path
# lives at `.tool_input.file_path`. (The old `$CLAUDE_FILE_PATHS` env var no
# longer exists in Claude Code 2.x.) On exit 0, Claude parses this hook's stdout
# for JSON, so ONLY the final JSON may go to stdout — every formatter/linter's
# own output is routed to /dev/null and lint findings are collected into a var,
# then emitted once as hookSpecificOutput.additionalContext.
input=$(cat)

FILE=$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null)
[ -z "$FILE" ] && exit 0
[ -f "$FILE" ] || exit 0

BIN="$HOME/.local/share/nvim/mason/bin"

# fmt <tool> [args...] — run an in-place formatter, discarding all its output
# (its stdout must not pollute our JSON). Never fails the hook.
fmt() {
	local tool="$1"
	shift
	if [ -x "$tool" ]; then
		"$tool" "$@" >/dev/null 2>&1 || true
	fi
}

LINT_OUT=""
# lint <label> <tool> [args...] — run a linter, capturing any output under a
# labelled header so it can be surfaced to Claude. Never fails the hook.
lint() {
	local label="$1" tool="$2"
	shift 2
	[ -x "$tool" ] || return 0
	local out
	out=$("$tool" "$@" 2>&1) || true
	[ -n "$out" ] && LINT_OUT="${LINT_OUT}[$label]"$'\n'"$out"$'\n\n'
}

case "$FILE" in
*.js | *.jsx | *.ts | *.tsx | *.mjs | *.cjs)
	fmt "$BIN/eslint_d" "$FILE" --fix
	fmt "$BIN/prettier" "$FILE" --write
	lint "eslint" "$BIN/eslint_d" "$FILE"
	;;
*.json | *.css | *.scss | *.less | *.yaml | *.yml)
	fmt "$BIN/prettier" "$FILE" --write
	;;
*.html)
	# TODO: remove hire2 exception once the repo is public
	HIRE2_WT="${XDG_DATA_HOME:-$HOME/.local/share}/devgeta/worktrees/hire2"
	case "$FILE" in
	"$HOME/lever/hire2"/* | "$HIRE2_WT"/*) ;;
	*) fmt "$BIN/prettier" "$FILE" --write ;;
	esac
	;;
*.md | *.markdown)
	fmt "$BIN/prettier" "$FILE" --write
	;;
*.py)
	fmt "$BIN/isort" "$FILE"
	fmt "$BIN/black" "$FILE"
	lint "flake8" "$BIN/flake8" "$FILE"
	;;
*.go)
	fmt "$BIN/goimports" -w "$FILE"
	fmt "$BIN/gofumpt" -w "$FILE"
	fmt "$BIN/golines" -w "$FILE"
	lint "golangci-lint" "$BIN/golangci-lint" run "$FILE"
	;;
*.lua)
	fmt "$BIN/stylua" "$FILE"
	;;
*.sh | *.bash)
	fmt "$BIN/shfmt" -w "$FILE"
	;;
*.c | *.h | *.cpp | *.hpp)
	fmt "$BIN/clang-format" -i "$FILE"
	;;
esac

# Surface lint findings (if any) as context Claude sees on its next turn.
if [ -n "$LINT_OUT" ]; then
	jq -n --arg ctx "Linter findings for $FILE — please fix:"$'\n'"$LINT_OUT" \
		'{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $ctx}}'
fi

exit 0
