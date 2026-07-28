# OpenCode App

Installs and configures [OpenCode](https://opencode.ai/docs) — a terminal-based AI code editor.

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
