#!/usr/bin/env bash
# PreToolUse hook: docs/guides/output-budget-runner.md. Matches a Bash
# command against the built-in rule table generated into
# ~/.config/devgeta/agent-runtime.json, and — on a match — rewrites the
# command to call output-budget-run.sh, which does the actual capping (this
# hook only decides WHETHER and HOW to rewrite; see the guide §1).
#
# Every degenerate case here (gate off, missing/malformed sidecar, missing
# runner, out-of-range numbers, malformed rules) allows the command through
# UNMODIFIED — never blocks, never mutates anything but the command string.
#
# Escape hatch: DEVGETA_OUTPUT_BUDGET=off skips rewriting entirely, matching
# the runner's own pass-through for the same variable (guide §3).
set -u

if [ "${DEVGETA_OUTPUT_BUDGET:-}" = "off" ]; then
	exit 0
fi

command -v jq >/dev/null 2>&1 || exit 0

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/segments.sh"

input=$(cat)
COMMAND=$(printf '%s' "$input" | jq -r '.tool_input.command // empty' 2>/dev/null)
[ -z "$COMMAND" ] && exit 0

SIDECAR="${XDG_CONFIG_HOME:-$HOME/.config}/devgeta/agent-runtime.json"
[ -f "$SIDECAR" ] || exit 0

sidecar_json=$(cat "$SIDECAR" 2>/dev/null) || exit 0
printf '%s' "$sidecar_json" | jq -e . >/dev/null 2>&1 || exit 0

# jq is lenient about a leading-zero JSON number literal (`0099`) — invalid
# per the JSON grammar, but jq parses it anyway and silently renormalizes it
# to "99" on output, which would make the width check below see a corrected,
# in-range value instead of rejecting the malformed literal that produced it.
# JSON.parse on the JS side throws on the same input outright, so without this
# check the two hooks would disagree on a hand-edited sidecar containing one
# (guide §8: "both hooks reach the same decision"). A plain grep over the raw
# text catches it before jq ever gets to normalize it away.
if printf '%s' "$sidecar_json" | grep -qE ':[[:space:]]*-?0[0-9]'; then
	exit 0
fi

enabled=$(printf '%s' "$sidecar_json" | jq -r '.outputBudget // false' 2>/dev/null)
[ "$enabled" = "true" ] || exit 0

RUNNER=$(printf '%s' "$sidecar_json" | jq -r '.runner // empty' 2>/dev/null)
[ -n "$RUNNER" ] && [ -f "$RUNNER" ] || exit 0

WIDTH_RE='^[1-9][0-9]{0,14}$'

LINE_LIMIT=$(printf '%s' "$sidecar_json" | jq -r '.lineContentLimit // empty' 2>/dev/null)
TOTAL_LIMIT=$(printf '%s' "$sidecar_json" | jq -r '.maxTotalBytes // empty' 2>/dev/null)
CAPTURE_LIMIT=$(printf '%s' "$sidecar_json" | jq -r '.captureContentLimit // empty' 2>/dev/null)
for n in "$LINE_LIMIT" "$TOTAL_LIMIT" "$CAPTURE_LIMIT"; do
	[[ "$n" =~ $WIDTH_RE ]] || exit 0
done

# Malformed rules disable rewriting entirely — the whole array, not just
# the bad entry (guide §8.3) — so the array is validated up front, before
# any matching is attempted.
rules_is_array=$(printf '%s' "$sidecar_json" | jq -r '.rules | type == "array"' 2>/dev/null)
[ "$rules_is_array" = "true" ] || exit 0

RULES_RAW=$(printf '%s' "$sidecar_json" | jq -c '.rules[]?' 2>/dev/null)
[ -n "$RULES_RAW" ] || exit 0

while IFS= read -r rule; do
	[ -z "$rule" ] && continue
	name=$(printf '%s' "$rule" | jq -r '.name // empty' 2>/dev/null)
	match_is_array=$(printf '%s' "$rule" | jq -r '.match | type == "array"' 2>/dev/null)
	[ "$match_is_array" = "true" ] || exit 0
	mlen=$(printf '%s' "$rule" | jq -r '.match | length' 2>/dev/null)
	[ -n "$name" ] || exit 0
	{ [ "$mlen" = "1" ] || [ "$mlen" = "2" ]; } || exit 0
	rhead=$(printf '%s' "$rule" | jq -r '.head // empty' 2>/dev/null)
	rtail=$(printf '%s' "$rule" | jq -r '.tail // empty' 2>/dev/null)
	[[ "$rhead" =~ $WIDTH_RE ]] || exit 0
	[[ "$rtail" =~ $WIDTH_RE ]] || exit 0
done <<<"$RULES_RAW"

# devgeta_ob_token_is_hostile reports whether a token contains a single
# quote, double quote, backslash, or $ — any of which means this best-effort
# whitespace split cannot be trusted for THIS token (guide §8.2 step 5).
devgeta_ob_token_is_hostile() {
	case "$1" in
	*"'"* | *'"'* | *'\'* | *'$'*) return 0 ;;
	esac
	return 1
}

# devgeta_ob_match_segment applies the tokenization contract (guide §8.2) to
# one command segment and, on a match against RULES_RAW (first rule in
# array order wins), prints "<head> <tail>" and returns 0. Returns 1 with no
# output otherwise — including every refusal case, which the caller must
# treat identically to "no rule matched" (under-matching is always safe).
devgeta_ob_match_segment() {
	local seg
	seg=$(devgeta_trim "$1")
	[ -z "$seg" ] && return 1

	local -a toks=()
	IFS=$' \t' read -ra toks <<<"$seg"
	[ "${#toks[@]}" -eq 0 ] && return 1

	# Strip a leading run of VAR=value assignments, then a leading `env`
	# together with its own assignment run.
	local idx=0
	while [ "$idx" -lt "${#toks[@]}" ] && [[ "${toks[$idx]}" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; do
		devgeta_ob_token_is_hostile "${toks[$idx]}" && return 1
		idx=$((idx + 1))
	done
	if [ "$idx" -lt "${#toks[@]}" ] && [ "${toks[$idx]}" = "env" ]; then
		idx=$((idx + 1))
		while [ "$idx" -lt "${#toks[@]}" ] && [[ "${toks[$idx]}" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; do
			devgeta_ob_token_is_hostile "${toks[$idx]}" && return 1
			idx=$((idx + 1))
		done
	fi

	local cmp0="${toks[$idx]:-}"
	local cmp1="${toks[$((idx + 1))]:-}"
	[ -z "$cmp0" ] && return 1

	# Hostility is checked PER RULE ATTEMPT, using only that rule's own
	# match length — never a blanket check of the first two tokens. This is
	# what lets `make -j$(nproc)` still match the one-token "make" rule: the
	# $ lives in the second token, which a one-token rule never examines at
	# all (guide §8.2: "the check covers only the compared tokens").
	local rule
	while IFS= read -r rule; do
		[ -z "$rule" ] && continue
		local mlen m0 m1 rhead rtail
		mlen=$(printf '%s' "$rule" | jq -r '.match | length')
		rhead=$(printf '%s' "$rule" | jq -r '.head')
		rtail=$(printf '%s' "$rule" | jq -r '.tail')
		if [ "$mlen" = "1" ]; then
			devgeta_ob_token_is_hostile "$cmp0" && continue
			m0=$(printf '%s' "$rule" | jq -r '.match[0]')
			if [ "$cmp0" = "$m0" ]; then
				printf '%s %s' "$rhead" "$rtail"
				return 0
			fi
		elif [ "$mlen" = "2" ]; then
			[ -z "$cmp1" ] && continue
			devgeta_ob_token_is_hostile "$cmp0" && continue
			devgeta_ob_token_is_hostile "$cmp1" && continue
			m0=$(printf '%s' "$rule" | jq -r '.match[0]')
			m1=$(printf '%s' "$rule" | jq -r '.match[1]')
			if [ "$cmp0" = "$m0" ] && [ "$cmp1" = "$m1" ]; then
				printf '%s %s' "$rhead" "$rtail"
				return 0
			fi
		fi
	done <<<"$RULES_RAW"

	return 1
}

# Only ever rewrite a command that is ONE segment.
#
# The rewrite replaces the whole command string, so wrapping a compound
# command would take everything beside the matched segment along with it:
#
#   rm -rf ~/wherever && go test ./...
#     -> output-budget-run.sh 30 120 ... 'rm -rf ~/wherever && go test ./...'
#
# A deny rule written against the original text does not match that, and
# whether the host re-checks a rewritten tool_input at all is undocumented
# (guide §2.3). Capping output is never a reason to change what a command is
# permitted to do, so a compound command is left exactly as it came in. The
# cost is a missed capping opportunity, which is the direction this feature
# is allowed to fail in.
SEGMENTS=$(devgeta_split_command_segments "$COMMAND")
[ "$(printf '%s\n' "$SEGMENTS" | wc -l)" -eq 1 ] || exit 0

MATCHED=$(devgeta_ob_match_segment "$SEGMENTS") || exit 0
[ -n "$MATCHED" ] || exit 0
read -r RHEAD RTAIL <<<"$MATCHED"

# Defence in depth: re-validate everything about to be interpolated, right
# before interpolating it (guide §5.4) — the rules array was already
# checked as a whole above, but nothing stops this from being cheap too.
[[ "$RHEAD" =~ $WIDTH_RE ]] || exit 0
[[ "$RTAIL" =~ $WIDTH_RE ]] || exit 0

REWRITTEN="$(devgeta_shell_quote "$RUNNER") $RHEAD $RTAIL $LINE_LIMIT $TOTAL_LIMIT $CAPTURE_LIMIT $(devgeta_shell_quote "$COMMAND")"

# updatedInput replaces Claude's WHOLE tool_input object, not just the
# changed field (guide §2.3) — starting from the original object and
# overwriting only .command is what keeps every other field (description,
# timeout, ...) intact.
updated_input=$(printf '%s' "$input" | jq --arg cmd "$REWRITTEN" '.tool_input | .command = $cmd' 2>/dev/null)
[ -n "$updated_input" ] || exit 0

# No permissionDecision, deliberately. This hook caps output; it has no
# business deciding whether the command may run, and an "allow" here would be
# an unconditional grant issued off a rule match — handing the host a decision
# it never asked this hook to make. Supplying only updatedInput leaves the
# normal permission evaluation exactly as it would have been. If a host ever
# declines to apply a rewrite that carries no decision, the feature no-ops and
# the command runs uncapped, which is the correct way for it to fail.
jq -n --argjson ui "$updated_input" \
	'{hookSpecificOutput: {hookEventName: "PreToolUse", updatedInput: $ui}}'
exit 0
