#!/usr/bin/env bash
# PostToolUse hook: format the file Claude just edited, then lint the result and
# feed any findings back to Claude so it can self-correct.
#
# The formatter table lives in the agent-neutral ~/.config/devgeta/format-file.sh
# (ADR-0063) so Pi formats identically. This wrapper only adapts it to Claude:
# it reads the hook payload, applies the one path exception below, and wraps the
# script's findings in hook JSON.
#
# Claude Code delivers the hook payload as JSON on stdin; the edited file path
# lives at `.tool_input.file_path`. On exit 0, Claude parses this hook's stdout
# for JSON, so ONLY the final JSON may go to stdout — the shared script prints
# findings only, and they are emitted once as hookSpecificOutput.additionalContext.
input=$(cat)

FILE=$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null)
[ -z "$FILE" ] && exit 0
[ -f "$FILE" ] || exit 0

SCRIPT="${XDG_CONFIG_HOME:-$HOME/.config}/devgeta/format-file.sh"
[ -x "$SCRIPT" ] || exit 0

# TODO: remove hire2 exception once the repo is public
case "$FILE" in
*.html)
	HIRE2_WT="${XDG_DATA_HOME:-$HOME/.local/share}/devgeta/worktrees/hire2"
	case "$FILE" in
	"$HOME/lever/hire2"/* | "$HIRE2_WT"/*) exit 0 ;;
	esac
	;;
esac

FINDINGS=$("$SCRIPT" "$FILE" 2>/dev/null)

# Surface lint findings (if any) as context Claude sees on its next turn.
if [ -n "$FINDINGS" ]; then
	jq -n --arg ctx "Linter findings for $FILE — please fix:"$'\n'"$FINDINGS" \
		'{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $ctx}}'
fi

exit 0
