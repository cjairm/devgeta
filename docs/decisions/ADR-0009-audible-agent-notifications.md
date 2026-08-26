# ADR-0009 — An agent that wants you makes a sound, from the hook that already knows

**Date:** 2026-07-29
**Status:** ACCEPTED

## Context

Nothing devgeta ships makes a sound. Verified across every layer that could:

- No `afplay`, `paplay`, `aplay`, `osascript display notification`, or bare `\a` anywhere in
  the repo. `osascript` appears only as a **permission rule** in both agents' configs.
- `configs/tmux/tmux.conf.tmpl` sets no `visual-bell`, `bell-action`, `monitor-activity`, or
  `monitor-bell`. Its only bell line (`:151`) styles the window-status flag _if_ something
  else rings.
- `configs/alacritty/alacritty.toml.tmpl` has no `[bell]` section, so `bell.command` is
  Alacritty's default `None` — even a real BEL byte reaching the terminal is silent.

So the visual signal from [ADR-0005](ADR-0005-agent-activity-state-in-tmux-pane-options.md)
is the whole notification story, and it only works if you are looking. The
[implementing cycle](../plans/cycles/2026-07-28-agent-activity-notifications.md) put
**desktop notifications** out of scope on the grounds that "the status-bar signal covers the
'tell me while I'm in another window' case." It does not cover the case that actually
happens: the status bar only paints windows of the **attached** session, so an agent blocked
in another session is invisible there, and neither the status bar nor the dot helps at all
when the terminal is not the focused window.

The ask is narrow and it is the right one: a sound, so state does not have to be tracked
visually. Speakers are on; the eyes are elsewhere.

Constraints:

1. **It must fire on the transition, not on a poll.** The dashboard refreshes every 3
   seconds and only when open. The event already exists as a hook invocation.
2. **It must not fire for the agent you are watching.** A ding on every turn end in the pane
   you are staring at is noise that gets the feature turned off within a day.
3. **It must not slow the hook chain.** Claude Code waits for a hook command to exit.
   `afplay` on a 1-second sound would add a second to every turn end.
4. **It must stay agent-neutral and symmetric.** CLAUDE.md forbids a behaviour that exists
   in one AI agent's config and not the other's, and
   `internal/apps/opencode/permissions_test.go` fails the build on asymmetry.
5. **The hook has no devgeta context.** It knows `$TMUX_PANE` and its own cwd. It cannot
   parse `global_config.yaml` — not in bash, not sanely in the OpenCode plugin.
6. **Silence must be safe.** No audio device, no player binary, headless CI, a container —
   all must be a no-op, never an error, never a blocked hook.

## Decision

**The two agent-state hooks play a sound, gated on the window being unattended, opt-in via a
tmux global option.** No new process, no daemon, no polling, no bundled audio.

### Where it fires

`configs/claude/agent-state.sh` and `configs/opencode/plugin/notify.js` — the same two files
that already write `@dg_agent_state`, at the same moment, for the same three values:

| State     | Sound | Why                                                       |
| --------- | ----- | --------------------------------------------------------- |
| `blocked` | yes   | you are the only thing that unblocks it                   |
| `idle`    | yes   | it finished; your move                                    |
| `error`   | yes   | it failed and stopped                                     |
| `busy`    | never | the agent starting work is not an event you asked to hear |

This is the same set that writes the window-level mirror, and for the same reason: these
three are "wants you," `busy` is "leave me alone."

### The gate: `window_active_clients == 0`

A sound fires only when no client is currently viewing that pane's window:

```sh
[ "$(tmux display-message -p -t "$TMUX_PANE" '#{window_active_clients}')" = "0" ] || exit 0
```

This is deliberately the **exact predicate `window-status-format` already uses**
(see `window-status-format` in `configs/tmux/tmux.conf.tmpl`) to decide whether to flag a
window on the status bar. Reusing it means the
audible and visual signals can never disagree about whether you have seen something, and it
gives the behaviour everyone actually wants for free: no ding for the agent on screen, a ding
for one in another window or another session, including while the whole session is detached.

### The switch: a tmux global option

```sh
[ "$(tmux show-option -gqv @dg_notify_sound)" = "on" ] || exit 0
```

**Off by default.** A tool that starts making noise after an upgrade is a bug.

The tmux option is the _runtime_ source; `global_config.yaml` stays the durable one.
`dg config set worktree.notify_sound true` writes the YAML value only — it does not by
itself affect a running tmux server. `configs/tmux/tmux.conf.tmpl` renders the persisted
value so a fresh server starts correct; making an already-running server pick up a change
takes `dg configure tmux --force` plus a config reload (or a fresh server), or setting the
tmux option directly by hand (`tmux set-option -g @dg_notify_sound on`). This mirrors
ADR-0005's reasoning — tmux already owns the runtime state that dies with the server — and
it keeps the hook to `tmux`-only calls it was already making, with no YAML parsing in bash
or JavaScript.

### The player: probe, background, never fail

Ship **no audio files**. Use what the OS already has, first match wins:

| Platform | Command                                                                           |
| -------- | --------------------------------------------------------------------------------- |
| macOS    | `afplay /System/Library/Sounds/{Glass,Ping,Basso}.aiff`                           |
| Linux    | `paplay /usr/share/sounds/freedesktop/stereo/{complete,message,dialog-error}.oga` |
| fallback | `printf '\a'` — the terminal bell, whatever the user has bound to it              |

Distinct sound per state (idle / blocked / error, in that column order) so the three are
distinguishable without looking, which is the entire point.

Fired **detached and backgrounded**, so the hook returns immediately regardless of how long
the sound is or whether the player hangs:

```sh
( play_cmd >/dev/null 2>&1 & ) >/dev/null 2>&1 || true
```

Every failure mode — missing binary, missing file, no audio device, no PulseAudio session —
lands in the same place: silence and exit 0. This is the `fmt()`/`|| true` tolerance
`agent-state.sh` already applies to its tmux call, for the same reason: a missing sound is
cosmetic, a blocked hook chain is not.

### Rejected alternatives

**Desktop notifications** (`osascript display notification` / `notify-send`). What the
previous cycle deferred, and still not the ask. They queue in Notification Center, need
per-OS wording and an entitlement prompt on macOS, and both agents' configs currently
**deny** `osascript` outright — enabling it for this would widen a permission surface for a
banner nobody requested. A sound needs no permission and no widening.

**Ring the bell and let the terminal decide** (`printf '\a'`, plus a `[bell]` section in
`alacritty.toml.tmpl`). Attractive — one line, no player probing — but tmux's bell handling
sits between the pane and the terminal, and a single bell character cannot carry
idle-vs-blocked-vs-error. Kept as the last-resort fallback, not the mechanism.

**Sound from the `dg ws` TUI.** Only works while the dashboard is open and on a 3-second
poll, which is the opposite of "so I do not have to watch it."

**Sound from tmux's own `monitor-bell` / `monitor-activity`.** ADR-0005 already rejected
these for state, and the reason carries: they fire on output, not on turn completion.

**A `notify_sound` value read from `global_config.yaml` by the hook.** Requires YAML parsing
in bash, or a `dg config get` subprocess on every turn end. The tmux option costs one call
of a binary the hook already invokes twice.

**Configurable sound files / a user-supplied command.** Speculative until someone asks.
`@dg_notify_sound` accepts `on`/`off` today; widening it later to accept a command string is
additive and breaks nothing.

## Consequences

**Easier.** State no longer has to be watched. The signal reaches you when the terminal is
not even visible, which no visual mechanism in the tool can do. It composes with
[ADR-0008](ADR-0008-agent-state-on-every-pane-row.md) rather than duplicating it: sound says
_something_ wants you, the dashboard says _which_. It also removes the need for a
cross-session status-bar aggregate, which ADR-0008 drops on exactly this basis.

**Harder.** Two more files must stay behaviourally matched, and the symmetry is now about
runtime behaviour (does a sound play, on which states, under which gate) rather than a
declarative list a reflection test can compare. The OpenCode plugin has to reproduce the
gate, the switch, the player probe, and the detach — in JavaScript, testable only through the
`execFn` seam `notify.js` already has.

**Harder.** Two agents finishing at once make two sounds. No throttle, no coalescing —
that is a real annoyance in a wide fan-out and the honest answer is that we will find out
whether it matters before building a debounce for it.

**Accepted trade-off.** The gate reads `window_active_clients`, so being attached to a window
counts as having seen it even if you are looking at a different monitor. Same accepted
imprecision as the status bar's, and the same one ADR-0005 already accepted when it made
attaching clear a `blocked` state whose prompt is still open.

**Accepted trade-off.** No sound is bundled, so what you hear depends on the OS's stock
sounds, and on Linux a system without freedesktop sounds falls all the way through to the
terminal bell. Bundling audio means picking a licence, growing the binary, and shipping
assets through `embed` for a feature whose default is off.

**Follow-on.** This ADR governs
[2026-07-29-ws-agent-panes-and-sound.md](../plans/cycles/2026-07-29-ws-agent-panes-and-sound.md),
Part B. It adds one setting that
[2026-07-29-dg-config-command.md](../plans/cycles/2026-07-29-dg-config-command.md)'s registry
must carry; if that cycle has not landed, the option is settable by hand in `tmux.conf` and
the YAML field simply has no CLI yet.
