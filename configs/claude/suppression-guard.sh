#!/usr/bin/env bash
# PreToolUse hook: deny an Edit or Write that INTRODUCES a lint-suppression
# comment (//nolint, # noqa, @ts-ignore, eslint-disable, ...) instead of
# fixing the underlying issue. CLAUDE.md's "Lint issues" section calls
# //nolint "never acceptable"; this makes that rule structurally impossible
# to violate instead of relying on the agent remembering it (CLAUDE.md §4:
# "prefer making a mistake class structurally impossible... over documenting
# a convention people must remember").
#
# Scope: DEVGETA-REPO-ONLY (see ADR-0006). This encodes DEVGETA's OWN stance —
# plenty of other codebases deliberately allow suppression comments — so it
# must not impose itself on unrelated repos the way secret-guard.sh's global
# hook does. Gated by devgeta_is_repo (lib/devgeta-repo.sh), which fails
# toward NOT firing on any uncertainty.
#
# Claude Code delivers the hook payload as JSON on stdin for the Edit and
# Write tools: `.tool_name` ("Edit" or "Write"), `.tool_input.file_path`, and
# — for Edit — `.tool_input.old_string`/`.tool_input.new_string`, or — for
# Write — `.tool_input.content`. Top-level `.cwd` is the same field
# task-redirect.sh already relies on. Denies via exit 2 with a one-line
# reason on stderr, the same mechanism the sibling hooks in this file's
# family use. A missing/unparseable payload, or jq being unavailable, falls
# through to exit 0 (allow) — this hook must never accidentally block all
# edits.
#
# For Edit: only a needle present in new_string but ABSENT from old_string
# counts as "introduced" — an edit that merely moves code around an existing
# (untouched) suppression elsewhere in the file is unaffected. For Write: any
# needle present in the new content denies, since Write always replaces the
# whole file and there is no "before" to diff against — CLAUDE.md's ban has
# no carve-out for a suppression comment surviving a rewrite.
#
# Escape hatch: set DEVGETA_SKIP_SUPPRESSION_GUARD=1 in the shell that launches this
# agent (e.g. in the repo's .envrc or your shell profile) BEFORE invoking this
# hook — this hook reads its own environment, not one set inside the command.
#
# Keep this file's PATTERNS list and
# configs/opencode/plugin/suppression-guard.js's in sync — they mirror each
# other one-for-one.

if [ -n "${DEVGETA_SKIP_SUPPRESSION_GUARD:-}" ]; then
	exit 0
fi

input=$(cat)
TOOL=$(printf '%s' "$input" | jq -r '.tool_name // empty' 2>/dev/null)
[ -z "$TOOL" ] && exit 0

CWD=$(printf '%s' "$input" | jq -r '.cwd // empty' 2>/dev/null)
DIR="${CWD:-$PWD}"

# Scope by the FILE being written, not by where the session is rooted.
# ADR-0006 gates this ban to devgeta's own code; for Edit/Write the payload
# names the exact target, so the proxy is unnecessary and wrong in both
# directions — a session rooted elsewhere could write a suppression INTO
# devgeta unchecked, and a devgeta-rooted session had the ban imposed on
# files in unrelated repos. A relative path resolves against the session cwd
# (the common case, unchanged); an absent one falls back to the cwd, which is
# all a malformed payload leaves to go on.
FILE_PATH=$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null)
case "$FILE_PATH" in
/*) SCOPE_DIR=$(dirname "$FILE_PATH") ;;
"") SCOPE_DIR="$DIR" ;;
*) SCOPE_DIR=$(dirname "$DIR/$FILE_PATH") ;;
esac

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/devgeta-repo.sh"

devgeta_is_repo "$SCOPE_DIR" || exit 0

BYPASS_HINT="bypass: export DEVGETA_SKIP_SUPPRESSION_GUARD=1 in the shell that launches this agent (e.g. the repo's .envrc), not inside the command — this hook reads its own environment"

case "$TOOL" in
Edit)
	OLD=$(printf '%s' "$input" | jq -r '.tool_input.old_string // empty' 2>/dev/null)
	NEW=$(printf '%s' "$input" | jq -r '.tool_input.new_string // empty' 2>/dev/null)
	;;
Write)
	OLD=""
	NEW=$(printf '%s' "$input" | jq -r '.tool_input.content // empty' 2>/dev/null)
	;;
*)
	exit 0
	;;
esac
[ -z "$NEW" ] && exit 0

# label|||needle pairs (a delimiter unlikely to appear in either half — some
# needles below already contain a literal colon, so a colon delimiter would
# not work). Checked as plain substrings, not regexes.
PATTERNS=(
	"Go|||//nolint"
	"Python|||# noqa"
	"Python|||# type: ignore"
	"Python|||# pylint: disable"
	"JS/TS|||eslint-disable"
	"JS/TS|||@ts-ignore"
	"JS/TS|||@ts-nocheck"
	"Java/Kotlin|||@SuppressWarnings"
	"Ruby|||rubocop:disable"
)

# count_occurrences counts non-overlapping occurrences of $2 in $1. Needed
# instead of a presence check: if OLD already has one occurrence of a needle
# and NEW adds a second, different one, a presence check ("is the needle in
# NEW, and not in OLD") sees the needle in both and wrongly allows it — an
# actually-new suppression slips through because an unrelated old one of the
# same kind already existed elsewhere in the touched span.
count_occurrences() {
	local haystack="$1" needle="$2" count=0
	while true; do
		case "$haystack" in
		*"$needle"*) ;;
		*) break ;;
		esac
		count=$((count + 1))
		haystack="${haystack#*"$needle"}"
	done
	printf '%d' "$count"
}

for entry in "${PATTERNS[@]}"; do
	label="${entry%%|||*}"
	needle="${entry#*|||}"
	old_count=$(count_occurrences "$OLD" "$needle")
	new_count=$(count_occurrences "$NEW" "$needle")
	if [ "$new_count" -gt "$old_count" ]; then
		echo "Introduces a $label suppression comment ($needle) — fix the underlying issue instead (CLAUDE.md 'Lint issues' calls this never acceptable) — $BYPASS_HINT" >&2
		exit 2
	fi
done

exit 0
