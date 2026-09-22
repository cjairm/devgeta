# devgeta: repair PATH when inherited from a bare launchd/tmux environment.
# Non-login shells never run path_helper (/etc/zprofile is login-only), so a
# shell born with a PATH missing /usr/bin would stay broken and poison every
# tmux session it creates. No-op when PATH is sane, and on Linux (no path_helper).
if [[ ":$PATH:" != *":/usr/bin:"* ]] && [ -x /usr/libexec/path_helper ]; then
  eval "$(/usr/libexec/path_helper -s)"
fi

# devgeta: put devgeta's own install directory on PATH.
#
# install.sh writes that PATH entry into ~/.zshrc, which ONLY interactive
# shells read. Anything that shells out without a profile — an AI coding agent
# running `zsh -c`, a git or editor hook, cron, launchd, an app started from
# Finder — therefore inherits a PATH with no ~/.local/bin in it and cannot run
# `devgeta` at all, while `devgeta` sits installed and working one directory
# away. The failure reads as "devgeta is not installed", which is the wrong
# thing to go looking for.
#
# The repair belongs here because ~/.zshenv is the one file every zsh reads —
# login or not, interactive or not — which is also why the block above lives
# here. Guarded on absence so the ~/.zshrc entry, which runs later, leaves one
# PATH entry rather than two, and on the directory existing so a shell never
# carries a path to nothing.
if [ -d "$HOME/.local/bin" ] && [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
  export PATH="$HOME/.local/bin:$PATH"
fi
