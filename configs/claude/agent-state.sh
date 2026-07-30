#!/usr/bin/env bash
# Stop / UserPromptSubmit / Notification hook: report this coder's activity
# into the tmux pane it is running in, so `dg ws`'s status dot can show
# working / finished / blocked without switching to the window. This is Step
# 3 of the cycle in docs/plans/cycles/2026-07-28-agent-activity-notifications.md;
# the governing design doc is
# docs/decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md — read
# it for the full value table and for why "busy" must be an explicit write
# rather than a cleared/absent value. The OpenCode counterpart is
# configs/opencode/plugin/notify.js; both write the same tmux pane option and
# must stay behaviourally matched.
#
# Since docs/decisions/ADR-0009-audible-agent-notifications.md (Step 10 of
# docs/plans/cycles/2026-07-29-ws-agent-panes-and-sound.md), this script also
# plays a sound on idle/blocked/error, gated so it never dings for the agent
# you're already watching. See the dedicated comment block near the bottom of
# this file for that logic; it reuses the same three-state set as the window
# mirror below, for the same reason.
#
# Unlike format.sh and task-redirect.sh, this script deliberately reads NO
# field from the hook's JSON stdin payload — it doesn't even read stdin.
# "Which value to write" is fully determined by which of the three
# registrations in settings.json.tmpl invoked it, each passing a different
# literal argument (idle / busy / blocked) in its command string. That
# argument is $1.
#
# Same no-op contract as the OpenCode plugin: if TMUX_PANE is unset or empty,
# do nothing and exit 0 — Claude Code run outside tmux must not error.
[ -n "${TMUX_PANE:-}" ] || exit 0

# Never let a fallible external call fail the hook (format.sh's fmt()
# pattern, adapted to this script's single call): a dead tmux server or a
# missing tmux binary must not exit non-zero and block/confuse Claude Code. A
# missing dot on the dashboard is cosmetic; a blocked hook chain is not.
tmux set-option -p -t "$TMUX_PANE" @dg_agent_state "$1" >/dev/null 2>&1 || true

# Window-level display-only mirror for the tmux status bar - a DIFFERENT
# option name than @dg_agent_state; see ADR-0005 for why reusing the
# pane-level name would corrupt other panes' reads via tmux's option
# inheritance. busy clears the mirror instead of writing it: the
# status-bar flag must disappear the instant a new turn starts, matching
# why busy is an explicit pane-level write too.
case "$1" in
busy)
	tmux set-option -w -u -t "$TMUX_PANE" @dg_window_agent_state >/dev/null 2>&1 || true
	;;
idle | blocked | error)
	tmux set-option -w -t "$TMUX_PANE" @dg_window_agent_state "$1" >/dev/null 2>&1 || true
	;;
esac

# ADR-0009: play a sound for idle/blocked/error — "wants you" states, same
# set as the window mirror above and for the same reason; busy never dings.
#
# Picks the file for state $1, probes afplay then paplay (first match on
# PATH wins), and falls back to the terminal bell if neither is present.
# Fired detached and backgrounded so a 1-second sound never adds a second to
# hook latency (Claude Code waits for this hook to exit). Nothing in here may
# ever fail the hook: a missing player, missing file, or no audio device is
# silence, not an error — the same `|| true` tolerance already used above.
play_notify_sound() {
	case "$1" in
	idle)
		afplay_file="/System/Library/Sounds/Glass.aiff"
		paplay_file="/usr/share/sounds/freedesktop/stereo/complete.oga"
		;;
	blocked)
		afplay_file="/System/Library/Sounds/Ping.aiff"
		paplay_file="/usr/share/sounds/freedesktop/stereo/message.oga"
		;;
	error)
		afplay_file="/System/Library/Sounds/Basso.aiff"
		paplay_file="/usr/share/sounds/freedesktop/stereo/dialog-error.oga"
		;;
	esac

	if command -v afplay >/dev/null 2>&1; then
		play_cmd=(afplay "$afplay_file")
	elif command -v paplay >/dev/null 2>&1; then
		play_cmd=(paplay "$paplay_file")
	else
		play_cmd=(printf '\a')
	fi

	if [ "${play_cmd[0]}" = "printf" ]; then
		# The fallback IS the bell, but the hook's OWN stdout never reaches
		# the pane's terminal: Claude Code captures every hook's stdout to
		# parse for JSON on exit (docs/apps/claude.md: "The hook prints only
		# JSON to stdout"), so a byte written there lands in that capture,
		# not the tty. Resolve the pane's REAL tty device instead (same
		# -p -t "$TMUX_PANE" pattern the gate check above already uses) and
		# write the byte directly there. Only reached from this branch, so
		# the common afplay/paplay path never pays for this extra tmux call.
		# A failed/empty resolution (dead server, no server) must be
		# silence, same as every other tmux call in this script - hence the
		# guard before ever attempting the write. Still backgrounded and
		# `|| true`-tolerant, consistent with the afplay/paplay branch, even
		# though printf is effectively instant.
		pane_tty="$(tmux display-message -p -t "$TMUX_PANE" '#{pane_tty}' 2>/dev/null)"
		if [ -n "$pane_tty" ]; then
			(printf '\a' >"$pane_tty" 2>/dev/null &) >/dev/null 2>&1 || true
		fi
	else
		("${play_cmd[@]}" >/dev/null 2>&1 &) >/dev/null 2>&1 || true
	fi
}

case "$1" in
idle | blocked | error)
	# Opt-in, off by default, and checked FIRST so the common (off) case
	# costs exactly one tmux call before exiting.
	[ "$(tmux show-option -gqv @dg_notify_sound 2>/dev/null)" = "on" ] || exit 0

	# Same predicate window-status-format in configs/tmux/tmux.conf.tmpl
	# already uses to flag an unattended window, so the audible and visual
	# signals can never disagree about whether you've seen this.
	[ "$(tmux display-message -p -t "$TMUX_PANE" '#{window_active_clients}' 2>/dev/null)" = "0" ] || exit 0

	play_notify_sound "$1"
	;;
esac

exit 0
