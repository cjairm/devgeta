# devgeta: repair PATH when inherited from a bare launchd/tmux environment.
# Non-login shells never run path_helper (/etc/zprofile is login-only), so a
# shell born with a PATH missing /usr/bin would stay broken and poison every
# tmux session it creates. No-op when PATH is sane, and on Linux (no path_helper).
if [[ ":$PATH:" != *":/usr/bin:"* ]] && [ -x /usr/libexec/path_helper ]; then
  eval "$(/usr/libexec/path_helper -s)"
fi
