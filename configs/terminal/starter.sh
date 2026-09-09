#!/bin/bash

# Alacritty launches this script straight from launchd's bare GUI
# environment on macOS — no shell profile has run, so PATH may miss
# /usr/local/bin, /opt/homebrew/bin, etc. The tmux SERVER started below
# inherits this environment for its whole lifetime: every non-login pane
# and every plugin script (tmux-continuum autosave needs `tmux` on PATH)
# resolves commands through it. Rebuild the system PATH here, at server
# birth, so all of that works. No-op on Linux (no path_helper).
# Per-shell PATH self-repair (for panes/sessions born from a broken-PATH
# client rather than this script) lives in configs/zsh/zshenv.zsh, wired into
# ~/.zshenv by `dg configure`.
if [ -x /usr/libexec/path_helper ]; then
	eval "$(/usr/libexec/path_helper -s)"
fi

# Get the list of existing tmux sessions
sessions=$(tmux list-sessions -F "#{session_name}" 2>/dev/null)

if [ -n "$sessions" ]; then
	# If there are existing sessions, attach to the first one
	tmux attach-session
else
	# If no sessions exist, start a new session
	tmux new-session -s "misc"
fi
