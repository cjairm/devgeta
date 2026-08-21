#!/usr/bin/env bash
input=$(cat)

eval "$(printf '%s' "$input" | jq -r '
  @sh "MODEL=\(.model.display_name // "?")",
  @sh "DIR=\(.workspace.current_dir // "")",
  @sh "AGENT=\(.agent.name // "")",
  @sh "DUR_MS=\(.cost.total_duration_ms // 0)",
  @sh "ADDED=\(.cost.total_lines_added // 0)",
  @sh "REMOVED=\(.cost.total_lines_removed // 0)",
  @sh "PCT=\((.context_window.used_percentage // 0) | floor)",
  @sh "FIVE_H=\((.rate_limits.five_hour.used_percentage // -1) | floor)",
  @sh "SEVEN_D=\((.rate_limits.seven_day.used_percentage // -1) | floor)"')"

EFFORT=$(jq -r '.effortLevel // empty' "$HOME/.claude/settings.json" 2>/dev/null)

: "${MODEL:=?}" "${DIR:=}" "${PCT:=0}" "${FIVE_H:=-1}" "${SEVEN_D:=-1}" "${IS_WT:=0}" "${WT_NAME:=}"
: "${ADDED:=0}" "${REMOVED:=0}" "${DUR_MS:=0}"

DIM=$'\033[2m'
BOLD=$'\033[1m'
RESET=$'\033[0m'
CYAN=$'\033[36m'
GREEN=$'\033[32m'
YELLOW=$'\033[33m'
RED=$'\033[31m'
MAGENTA=$'\033[35m'
BLUE=$'\033[34m'

# Base dir where `dg worktree` creates worktrees. Anything living under it gets
# the compact "wt:<repo>" display, whether or not git treats it as a *linked*
# worktree (devgeta-created dirs may be standalone clones with a real .git dir).
WT_BASE="${XDG_DATA_HOME:-$HOME/.local/share}/devgeta/worktrees"

# Repo name behind a linked worktree, read off the main repo's git dir. The
# path cannot be trusted for this: a worktree may sit under a directory named
# for its repo, or under a generic "worktrees" directory that names nothing, so
# walking up from $DIR gets the wrong answer for some layouts. The common git
# dir always points at the main repo, whatever the layout. Pure parameter
# expansion - no external command, and it only runs on a cache refresh.
repo_name_from_common_dir() {
	local common="${1%/}" name parent
	name="${common##*/}"
	if [ "$name" = ".git" ]; then
		parent="${common%/*}"
		name="${parent##*/}"
	else
		# A bare main repo has no ".git" segment: /srv/foo.git -> foo
		name="${name%.git}"
	fi
	printf '%s' "$name"
}

TMP_DIR="${TMPDIR:-/tmp}"
TMP_DIR="${TMP_DIR%/}"
[ -n "$TMP_DIR" ] || TMP_DIR="/tmp"
# Everything cached below (branch, counts, worktree-ness) is a pure function of
# $DIR, so the key is the directory only. Keying it per session instead would
# mint a fresh file for every session in every directory and never reuse one.
# $UID keeps two users sharing one /tmp from fighting over the same filename.
DIR_HASH=$(printf '%s' "$DIR" | cksum | awk '{print $1}')
CACHE_FILE="${TMP_DIR}/cc-statusline-${UID:-0}-${DIR_HASH}"
CACHE_MAX_AGE=5
# A directory that is never opened again would still leave its cache file
# behind forever, so reclaim files nothing has touched in this many days. A
# directory still in use rewrites its own file every CACHE_MAX_AGE seconds, so
# it can never age out from under an active session.
CACHE_TTL_DAYS=1
# Sweeping on every render would mean scanning the whole temp directory
# hundreds of times a session for nothing. Sweep on ~1 in this many cache
# refreshes instead; refreshes are already throttled to CACHE_MAX_AGE.
SWEEP_ODDS=32

cache_stale() {
	[ ! -f "$CACHE_FILE" ] && return 0
	local mtime
	mtime=$(stat -f %m "$CACHE_FILE" 2>/dev/null || stat -c %Y "$CACHE_FILE" 2>/dev/null || echo 0)
	[ $(($(date +%s) - mtime)) -gt "$CACHE_MAX_AGE" ]
}

# Delete only our own regular files, and let find do the removing so that a
# hostile or malformed name in a shared temp directory is never expanded by the
# shell. -maxdepth 1 keeps it off subdirectories, -type f keeps it off
# directories and symlinks, and the fixed prefix keeps it off everything else.
# Concurrent sweeps are harmless: a file already gone just yields ENOENT.
sweep_cache() {
	find "$TMP_DIR" -maxdepth 1 -type f -name 'cc-statusline-*' \
		-mtime "+${CACHE_TTL_DAYS}" -delete 2>/dev/null
}

# Write via a private name then rename, so a reader never sees a half-written
# line. Failure is not fatal - the values are already in memory for this render.
# WT_NAME goes last: read splits on "|" and hands the tail to the final
# variable intact, so a repo name containing one cannot shift the other fields.
write_cache() {
	local tmp="${CACHE_FILE}.$$"
	if printf '%s|%s|%s|%s|%s|%s\n' "$GB" "$GS" "$GM" "$GU" "$IS_WT" "$WT_NAME" >"$tmp" 2>/dev/null; then
		mv -f "$tmp" "$CACHE_FILE" 2>/dev/null || rm -f "$tmp" 2>/dev/null
	else
		rm -f "$tmp" 2>/dev/null
	fi
}

if cache_stale; then
	GB="" GS="" GM="" GU="" IS_WT=0 WT_NAME=""
	if [ -n "$DIR" ] && git -C "$DIR" rev-parse --git-dir >/dev/null 2>&1; then
		GB=$(git -C "$DIR" branch --show-current 2>/dev/null)
		GS=$(git -C "$DIR" diff --cached --numstat 2>/dev/null | wc -l | tr -d ' ')
		GM=$(git -C "$DIR" diff --numstat 2>/dev/null | wc -l | tr -d ' ')
		GU=$(git -C "$DIR" ls-files --others --exclude-standard 2>/dev/null | wc -l | tr -d ' ')
		GIT_DIR=$(git -C "$DIR" rev-parse --git-dir 2>/dev/null)
		GIT_COMMON=$(git -C "$DIR" rev-parse --git-common-dir 2>/dev/null)
		# rev-parse may answer with a path relative to $DIR (plain ".git" in a
		# main repo), and a relative answer would silently yield a wrong name
		# rather than an error. Absolutise both here rather than asking git for
		# --path-format=absolute, which only exists from git 2.31.
		case "$GIT_DIR" in /* | "") ;; *) GIT_DIR="$DIR/$GIT_DIR" ;; esac
		case "$GIT_COMMON" in /* | "") ;; *) GIT_COMMON="$DIR/$GIT_COMMON" ;; esac
		if [ -n "$GIT_DIR" ] && [ "$GIT_DIR" != "$GIT_COMMON" ]; then
			# Linked worktree: the main repo names it, in every layout.
			IS_WT=1
			WT_NAME=$(repo_name_from_common_dir "$GIT_COMMON")
		elif [ "${DIR#"$WT_BASE"/}" != "$DIR" ]; then
			# Standalone clone parked under WT_BASE. Its common git dir is its
			# own .git, which would name the checkout, not the repo - so leave
			# WT_NAME empty and let the display fall back to the parent dir,
			# which is the repo slug in this layout.
			IS_WT=1
		fi
	fi
	write_cache
	if [ -d "$TMP_DIR" ] && [ $((RANDOM % SWEEP_ODDS)) -eq 0 ]; then
		sweep_cache
	fi
else
	# A cache file written before WT_NAME existed has five fields; WT_NAME then
	# reads as empty and the display falls back, rather than rendering garbage.
	IFS='|' read -r GB GS GM GU IS_WT WT_NAME <"$CACHE_FILE" 2>/dev/null
fi

BAR_WIDTH=12
FILLED=$((PCT * BAR_WIDTH / 100))
[ "$FILLED" -gt "$BAR_WIDTH" ] && FILLED=$BAR_WIDTH
EMPTY=$((BAR_WIDTH - FILLED))
BAR_FILL=""
BAR_EMPTY=""
[ "$FILLED" -gt 0 ] && printf -v BAR_FILL "%${FILLED}s" "" && BAR_FILL="${BAR_FILL// /█}"
[ "$EMPTY" -gt 0 ] && printf -v BAR_EMPTY "%${EMPTY}s" "" && BAR_EMPTY="${BAR_EMPTY// /░}"

if [ "$PCT" -ge 90 ]; then
	BAR_COLOR="$RED"
elif [ "$PCT" -ge 70 ]; then
	BAR_COLOR="$YELLOW"
else
	BAR_COLOR="$GREEN"
fi

color_for_limit() {
	local p="$1"
	if [ "$p" -ge 90 ]; then
		printf '%s' "$RED"
	elif [ "$p" -ge 75 ]; then
		printf '%s' "$YELLOW"
	else
		printf '%s' "$GREEN"
	fi
}

MINS=$((DUR_MS / 60000))
SECS=$(((DUR_MS % 60000) / 1000))
SEP=" ${DIM}|${RESET} "

# Compact display for worktrees - show the repo name instead of the full path,
# since the branch already conveys the worktree identity. WT_NAME is the name
# git gave us; the parent directory is the fallback for the layouts where git
# cannot answer (standalone clone under WT_BASE, whose parent is the repo slug).
if [ "${IS_WT:-0}" = "1" ]; then
	REPO_LABEL="$WT_NAME"
	if [ -z "$REPO_LABEL" ]; then
		PARENT_DIR="${DIR%/*}"
		REPO_LABEL="${PARENT_DIR##*/}"
		[ -n "$REPO_LABEL" ] || REPO_LABEL="${DIR##*/}"
	fi
	if [ ${#REPO_LABEL} -gt 30 ]; then
		DISPLAY_DIR="${REPO_LABEL:0:27}..."
	else
		DISPLAY_DIR="$REPO_LABEL"
	fi
	L="${DIM}wt:${RESET}${CYAN}${DISPLAY_DIR}${RESET}"
elif [ -n "$HOME" ] && [ "$DIR" = "$HOME" ]; then
	DISPLAY_DIR="~"
	L="${DIM}${DISPLAY_DIR}${RESET}"
elif [ -n "$HOME" ] && [ "${DIR#"$HOME"/}" != "$DIR" ]; then
	DISPLAY_DIR="~/${DIR#"$HOME"/}"
	L="${DIM}${DISPLAY_DIR}${RESET}"
else
	DISPLAY_DIR="$DIR"
	L="${DIM}${DISPLAY_DIR}${RESET}"
fi
if [ -n "$GB" ]; then
	# Head-truncate long branch names (keep the prefix, e.g. ticket id) at 30 chars
	if [ ${#GB} -gt 30 ]; then
		GB="${GB:0:27}..."
	fi
	L+="${SEP}${BLUE}${GB}${RESET}"
	[ "${GS:-0}" -gt 0 ] && L+=" ${GREEN}+${GS}${RESET}"
	[ "${GM:-0}" -gt 0 ] && L+=" ${YELLOW}~${GM}${RESET}"
	[ "${GU:-0}" -gt 0 ] && L+=" ${CYAN}?${GU}${RESET}"
fi
if [ "${ADDED:-0}" -gt 0 ] || [ "${REMOVED:-0}" -gt 0 ]; then
	L+="${SEP}${GREEN}+${ADDED}${RESET}${DIM}/${RESET}${RED}-${REMOVED}${RESET}"
fi
SESSION=""
[ -n "$AGENT" ] && SESSION+="${MAGENTA}@${AGENT}${RESET}"
[ -n "$SESSION" ] && L+="${SEP}${SESSION}"
L+="${SEP}${BAR_COLOR}${BAR_FILL}${DIM}${BAR_EMPTY}${RESET} ${PCT}% ctx"
if [ "$FIVE_H" -ge 0 ] || [ "$SEVEN_D" -ge 0 ]; then
	L+="${SEP}"
	[ "$FIVE_H" -ge 0 ] && L+="$(color_for_limit "$FIVE_H")5h:${FIVE_H}%${RESET}"
	[ "$SEVEN_D" -ge 0 ] && L+=" $(color_for_limit "$SEVEN_D")7d:${SEVEN_D}%${RESET}"
fi
L+="${SEP}${MINS}m${SECS}s"
L+="${SEP}${CYAN}${BOLD}${MODEL}${RESET}"
[ -n "$EFFORT" ] && L+=" ${DIM}·${RESET} ${CYAN}${EFFORT}${RESET}"

echo "$L"
