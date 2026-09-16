# OpenCode App

Installs and configures [OpenCode](https://opencode.ai/docs) — a terminal-based AI code editor.

## Install channel

devgeta runs opencode's official install script,
`https://opencode.ai/install`, on both macOS and Linux — it is the only channel
opencode supports. The script puts everything under `~/.opencode`, with the
binary at `~/.opencode/bin/opencode`; that location is hardcoded upstream and
has no override.

Because nothing else adds that directory to your PATH, devgeta's generated
`devgeta.zsh` prepends it. Everything else follows from the same path:

- `dg install` skips opencode when `~/.opencode/bin/opencode` exists, or when
  `opencode` already resolves on your PATH from somewhere else.
- `dg uninstall opencode` removes `~/.opencode` and `~/.config/opencode`. No
  package manager is involved, so on macOS a copy from an older
  `brew install opencode` is left behind — see
  [the migration note](../../../docs/migrations/opencode-install-channel.md).

Why this channel, and the security trade it accepts:
[ADR-0047](../../../docs/decisions/ADR-0047-opencode-installs-from-its-official-script-on-every-platform.md).

### Holding a version back

devgeta installs whatever the script installs; it pins nothing, so a broken
upstream release reaches you on the next `dg install`. opencode 1.18.30, for
example, crashed while building its system prompt and turned every headless
agent run into an opaque server error. Recovery is manual — re-run the script
with a version you know works:

```bash
curl -fsSL https://opencode.ai/install | VERSION=1.18.20 bash
```

devgeta then leaves that binary alone, because it only installs when the binary
is missing. Run the script without `VERSION=` to move back to the latest.

## After Installation

**Start OpenCode:**

```bash
opencode
```

**View configuration:**

```bash
ls ~/.config/opencode/
```

---

## Provider

Use **[OpenRouter](https://openrouter.ai)** — one API key, access to all models below.

```bash
export OPENROUTER_API_KEY="your-key-here"
```

---

## Models

| Model                        | Role                       |
| ---------------------------- | -------------------------- |
| `anthropic/claude-fable-5`   | Hardest reasoning          |
| `anthropic/claude-opus-5`    | Daily coding (default)     |
| `moonshotai/kimi-k3`         | Agents + large repos       |
| `z-ai/glm-5.2`               | Deep review + architecture |
| `deepseek/deepseek-v4-flash` | Cheap bulk tasks           |

### When to switch

- **claude-fable-5** — when nothing else solves it
- **claude-opus-5** — everyday coding, bug fixes, PR reviews
- **kimi-k3** — large codebases, multi-file refactors, long agent workflows
- **glm-5.2** — architecture decisions, hard debugging, critical reviews
- **deepseek-v4-flash** — background or non-critical automation

---

## Plugins

`~/.config/opencode/plugin/notify.js` reports this coder's activity into the
tmux pane it's running in — working / finished / blocked / errored — so
`dg ws`'s status dot and tmux's status bar can show it without switching
windows.

Event mapping:

| Event                | Writes    |
| -------------------- | --------- |
| `chat.message`       | `busy`    |
| `session.idle`       | `idle`    |
| `permission.updated` | `blocked` |
| `session.error`      | `error`   |

It writes via `tmux set-option -p -t "$TMUX_PANE" @dg_agent_state <value>`,
plus a window-level mirror (`@dg_window_agent_state`) that tmux's status bar
reads to flag a window nobody is looking at. It no-ops silently outside tmux
(`TMUX_PANE` unset) — running `opencode` without tmux produces no error and
no output about tmux.

See [ADR-0005](../../../docs/decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md)
for the full design.
