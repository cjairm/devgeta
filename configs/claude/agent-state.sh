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

exit 0
