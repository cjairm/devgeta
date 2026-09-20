# OpenCode

Devgeta installs and configures [OpenCode](https://opencode.ai/docs), a
terminal-based AI code editor, and deploys a curated config to
`~/.config/opencode/`.

- **Module:** `internal/apps/opencode/`
- **Config source:** `configs/opencode/` (+ shared content in `configs/shared/`)

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

OpenCode pins no model of its own — devgeta ships no default here, so these are
suggestions, not configuration. Pick one per role and set it in OpenCode.

| Role                       | Family                    | When to reach for it                                    |
| -------------------------- | ------------------------- | ------------------------------------------------------- |
| Daily coding (default)     | `anthropic/claude-opus`   | Everyday coding, bug fixes, PR reviews                  |
| Hardest reasoning          | `anthropic/claude-fable`  | When nothing else solves it                             |
| Agents + large repos       | `moonshotai/kimi`         | Large codebases, multi-file refactors, long agent runs  |
| Deep review + architecture | `z-ai/glm`                | Architecture decisions, hard debugging, critical review |
| Cheap bulk tasks           | `deepseek/deepseek-flash` | Background or non-critical automation                   |

**Resolve the exact slug and point release yourself at
[openrouter.ai/models](https://openrouter.ai/models).** Families are listed
here without a version on purpose: point releases land every few weeks, and a
version pinned in prose is wrong long before anyone notices. Check the price
and the context window there too — both move.

A second reviewer is only worth paying for if it fails differently from your
daily driver, so pick it from a **different lineage** than whatever fills the
daily-coding row. A model that misses what yours misses is a rubber stamp.

Reviewing models default to agreeable. Give a reviewer a rubric that forces a
verdict — "list at least three concrete defects with file/line references; if
you find none, state exactly what you checked" — or you get praise instead of
review.

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

See [ADR-0005](../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md)
for the full design.
