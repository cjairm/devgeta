#!/usr/bin/env bash
# format-file.sh <path> — format one file with the Mason tools, lint the result,
# and print any linter findings on stdout as `[<linter>]` blocks.
#
# Agent-neutral: Claude Code's PostToolUse hook and Pi's devgeta-format
# extension both call this, so every agent formats with the same binaries and
# settings (ADR-0063). It always exits 0 and prints nothing else to stdout;
# formatter and linter output is discarded so callers can embed the result.
#
# It runs synchronously after every edit, so linters run in their cheap,
# syntax-only mode under a hard time limit.
FILE="${1:-}"
[ -z "$FILE" ] && exit 0
[ -f "$FILE" ] || exit 0

# Neovim's stdpath("data"), which is where Mason installs.
BIN="${XDG_DATA_HOME:-$HOME/.local/share}/nvim/mason/bin"

# fmt <tool> [args...] — run an in-place formatter, discarding all its output
# (its stdout must not pollute our JSON). Never fails the hook.
fmt() {
	local tool="$1"
	shift
	if [ -x "$tool" ]; then
		"$tool" "$@" >/dev/null 2>&1 || true
	fi
}

# Hard limit on any single linter run. A linter that wedges would wedge the
# edit, so losing the findings is always the better trade.
LINT_TIMEOUT=15

# timeout(1) is coreutils, so it is always there on Debian/Ubuntu but not on
# macOS, where it is gtimeout and only if coreutils was installed at all.
# Resolved once; run_bounded falls back to a shell watchdog when it is missing.
TIMEOUT_BIN=""
for _t in timeout gtimeout; do
	if command -v "$_t" >/dev/null 2>&1; then
		TIMEOUT_BIN="$_t"
		break
	fi
done
unset _t

# run_bounded <seconds> <cmd> [args...] — run a command, killing it if it
# outruns the budget. Returns the command's own status, or 124 (timeout(1)'s
# convention) if the budget was hit.
#
# Only linters go through this. Formatters must not: they rewrite the file in
# place, and a formatter killed mid-write can leave a truncated file behind —
# far worse than the hang it would be guarding against.
run_bounded() {
	local secs="$1"
	shift

	if [ -n "$TIMEOUT_BIN" ]; then
		"$TIMEOUT_BIN" -k 2 "$secs" "$@"
		return $?
	fi

	# Job control gives the command its own process group, so the watchdog can
	# take down its children too. Signalling the command alone is not enough:
	# callers read it through a command substitution, and a surviving grandchild
	# (golangci-lint shells out to the go tool) keeps that pipe open, so the
	# substitution would block for the full runtime of a process we already
	# tried to kill.
	set -m
	"$@" &
	local pid=$!
	set +m

	# The watchdog's own output must go nowhere either, for the same reason.
	(
		sleep "$secs"
		kill -TERM "-$pid" 2>/dev/null
		sleep 2
		kill -KILL "-$pid" 2>/dev/null
	) >/dev/null 2>&1 &
	local watchdog=$!

	local rc=0
	wait "$pid" || rc=$?

	# Kill the sleep before its shell. The other order orphans the sleep, which
	# then lingers for the rest of the budget on every edit.
	pkill -P "$watchdog" >/dev/null 2>&1
	kill "$watchdog" >/dev/null 2>&1
	wait "$watchdog" 2>/dev/null

	# Report a budget kill the same way timeout(1) would.
	case "$rc" in
	137 | 143) rc=124 ;;
	esac
	return "$rc"
}

LINT_OUT=""
# lint <label> <tool> [args...] — run a linter, capturing any output under a
# labelled header so it can be surfaced to Claude. Never fails the hook.
lint() {
	local label="$1" tool="$2"
	shift 2
	[ -x "$tool" ] || return 0
	local out rc=0
	out=$(run_bounded "$LINT_TIMEOUT" "$tool" "$@" 2>&1) || rc=$?
	# 124 is a budget kill; anything above it is death by signal. Either way the
	# run never finished, so stay quiet rather than report half a result.
	[ "$rc" -ge 124 ] && return 0
	[ -n "$out" ] && LINT_OUT="${LINT_OUT}[$label]"$'\n'"$out"$'\n\n'
	return 0
}

# lint_go <tool> <file> — golangci-lint restricted to the linters that work off
# the syntax tree alone. A default run type-checks the whole enclosing package
# (and its dependencies) even when handed a single file, which is seconds of CPU
# on a large package — per edit.
#
# The flag is spelled --fast-only in golangci-lint v2 and --fast in v1. Asking
# the binary which it is costs a process launch on every edit (~300ms measured,
# against a ~1.4s run), so try v2's spelling and fall back only when it is
# rejected — free on v2, one cheap usage error on v1.
#
# No --timeout is passed: that bounds golangci-lint's analysis phase only, and
# it reports the expiry as an error we would then hand to Claude as if it were a
# lint finding. run_bounded bounds the whole process instead, and stays silent.
#
# v2 prints a "0 issues." summary on a clean run (--show-stats defaults to true),
# which would reach the agent as a finding to fix. v1 prints nothing when clean.
# --show-stats=false goes after --fast-only so a v1 binary rejects --fast-only
# first and the fallback below still matches.
lint_go() {
	local tool="$1" file="$2" saved="$LINT_OUT"
	lint "golangci-lint" "$tool" run --fast-only --show-stats=false "$file"
	case "$LINT_OUT" in
	*"unknown flag: --fast-only"*)
		LINT_OUT="$saved"
		lint "golangci-lint" "$tool" run --fast "$file"
		;;
	esac
}

case "$FILE" in
*.js | *.jsx | *.ts | *.tsx | *.mjs | *.cjs)
	# eslint_d keeps a node daemon alive between runs, and that is the point of
	# it — it exists to skip node's startup cost, so stopping it after every
	# edit would make this hook slower, not faster. Bound its lifetime instead.
	# Both variables are set explicitly rather than left to eslint_d's own
	# defaults: its idle timeout defaults to "never exit" whenever
	# ESLINT_D_PPID is set in the environment, so a user who exports that
	# elsewhere would otherwise be left with a daemon nothing ever reaps.
	# Parent monitoring is switched off because this hook process is gone
	# milliseconds later; tying a shared daemon to it would kill the daemon
	# after every single edit. Both are ignored by eslint_d versions too old to
	# know them, which is the same as today's behaviour.
	export ESLINT_D_PPID=0 ESLINT_D_IDLE=15
	fmt "$BIN/eslint_d" "$FILE" --fix
	fmt "$BIN/prettier" "$FILE" --write
	lint "eslint" "$BIN/eslint_d" "$FILE"
	;;
*.json | *.css | *.scss | *.less | *.yaml | *.yml)
	fmt "$BIN/prettier" "$FILE" --write
	;;
*.html)
	fmt "$BIN/prettier" "$FILE" --write
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
	lint_go "$BIN/golangci-lint" "$FILE"
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

[ -n "$LINT_OUT" ] && printf '%s' "$LINT_OUT"
exit 0
