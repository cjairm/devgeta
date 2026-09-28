# ADR-0057 — The dashboard takes its pane-move keys from tmux

**Date:** 2026-09-28
**Status:** PROPOSED

## Context

The dashboard now has more than one area to move between: spaces, agents, and the diff
pane ([ADR-0056](ADR-0056-the-dashboard-lists-agents-in-their-own-section.md)). The
natural keys are the ones people already use to move between tmux panes and Neovim
windows. In devgeta's own config that's `ctrl+h/j/k/l` (vim-tmux-navigator, in both
`configs/tmux/tmux.conf.tmpl` and the Neovim config).

Two facts shape the design:

1. **tmux takes those keys first.** The shipped bindings are
   `bind -n C-h if-shell "$is_vim" "send-keys C-h" "select-pane -L"`. `is_vim` matches the
   pane's foreground process against a Vim pattern. For any other program, including
   `devgeta ws`, tmux runs `select-pane` itself, and the dashboard never sees the key.
2. **The keys belong to the user.** Someone who rebinds pane moves to other keys expects
   every tool to follow. A copy of the keys hard-coded in the dashboard would drift the
   moment they change.

## Decision

**Read the keys from tmux.** At startup the dashboard runs `tmux list-keys -T root` through
the tmux wrapper, and takes whichever keys run `select-pane -L / -D / -U / -R`, directly or
as the fallback branch of an `if-shell`. Those become left / down / up / right. If there's
no tmux, no such binding, or the output can't be parsed, it falls back to `ctrl+h/j/k/l`.
Neovim's mappings are not read: the keys reach the dashboard through tmux, so tmux is the
one place that decides them.

**Moves inside the dashboard:**

| Direction | From the list (spaces or agents)      | From the diff pane |
| --------- | ------------------------------------- | ------------------ |
| down      | spaces → agents; from agents: edge    | edge               |
| up        | agents → spaces; from spaces: edge    | edge               |
| right     | → diff pane (what `space` does today) | edge               |
| left      | edge                                  | → list             |

A folded section counts as absent: "down" from spaces with agents folded is an edge.

At an edge the dashboard hands the move back to tmux with `select-pane` in that direction,
the same way vim-tmux-navigator does from Neovim. With the shipped launch (`ctrl+t` opens
the dashboard alone in its own `[workspace]` window), there is no other pane, so an edge
move does nothing visible. That's expected, not a bug. It matters when the dashboard runs
in a split.

**While a text input has focus** (the `/` filter, a rename, the create flow's prompts), the
four keys are not moves. They go to the input exactly as today, and the dashboard never
hands them to tmux. Moving mid-typing would drop you into another pane with a half-typed
filter.

**Let the keys through to the dashboard.** The shipped tmux bindings pass the key through
when the pane runs Vim **or the dashboard**. The dashboard is recognised by its process,
the same way `is_vim` works: a `ps` match on the pane's tty for `devgeta ws` in the
command's arguments. (`dg` is an alias, so the process is always `devgeta`.)

**Rejected: a pane flag the dashboard sets on itself** (e.g. `@dg_nav_passthrough`). It's
cheaper to check than `ps`, but a dashboard that crashes, or quits along a path that forgets
to clear it, leaves the flag on a pane that may still run a shell. tmux would then send
`ctrl+h` to that shell, where it's a backspace, and pane moves would silently stop working
there. The process check can't go stale.

**Rejected: matching the `devgeta` process name alone.** Every devgeta command would then
swallow the keys, including installs and other TUIs that don't handle them. The user would
be stuck unable to leave the pane.

## Consequences

- Easier: one set of moves across tmux, Neovim and the dashboard, and a rebind in tmux
  carries over without touching devgeta.
- Harder: the shipped tmux bindings grow a second `ps` pattern, so a pane-move key costs
  one more process spawn when the pane isn't Vim. The `list-keys` parse has to accept the
  shipped `if-shell` form as well as a plain `select-pane` binding.
- Accepted: users who don't run the shipped tmux config and bind `ctrl+h/j/k/l` straight to
  `select-pane` won't reach the dashboard with those keys. The dashboard keeps working for
  them with its own keys (`space`/`esc` for the diff, `a`/`w` for folds, `j`/`k` to walk
  across sections).
