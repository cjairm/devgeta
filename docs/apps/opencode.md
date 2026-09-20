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

Two caveats on picking from a leaderboard. OpenRouter's programming collection
ranks by **tokens processed over a trailing week**, which measures adoption, not
quality — a cheap model doing bulk work outranks a better one used sparingly.
And third-party benchmark numbers in blog roundups are frequently unsourced;
check the model's own page before believing one.

### Choosing a reviewer

A second reviewer is only worth paying for if it fails differently from your
daily driver, so pick it from a **different lineage** than whatever fills the
daily-coding row. A model that misses what yours misses is a rubber stamp.

Reviewing models default to agreeable. Give a reviewer a rubric that forces a
verdict — "list at least three concrete defects with file/line references; if
you find none, state exactly what you checked" — or you get praise instead of
review.

## Fallbacks

Prefer **OpenRouter's own failover** over remembering a backup slug: pass a
`models` array on the request and it moves on when your first choice is down,
rate-limited, or erroring. The mechanism keeps working when the list goes stale;
a memorized slug does not. Current request shape is in
[OpenRouter's docs](https://openrouter.ai/docs).

The same different-lineage rule applies as for reviewers, for a different
reason: a fallback sharing a provider with its primary is taken out by the same
outage, the same price change, and the same bad release. Pair each role's pick
with one from another lineage.

Two tiers are worth having underneath that:

- **Open weights** — Alibaba's Qwen (the Coder line) and Mistral both publish
  weights, so they can run locally through Ollama. That is the only tier that
  still works when OpenRouter itself is what's down, and nobody can deprecate or
  reprice it out from under you.
- **Free tier** — OpenRouter carries a rotating set of no-cost models, some with
  very large context windows. Useful as a zero-cost floor for bulk work, but
  check the context window before committing: some free variants are capped far
  below their paid siblings and are too small for repo-scale tasks.

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
