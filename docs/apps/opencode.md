# OpenCode

Devgeta installs and configures [OpenCode](https://opencode.ai/docs), a
terminal-based AI code editor, and deploys a curated config to
`~/.config/opencode/`.

- **Module:** `internal/apps/opencode/`
- **Config source:** `configs/opencode/` (+ shared content in `configs/shared/`)
- **Install:** opencode's official script (`https://opencode.ai/install`)

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
  [the migration note](../migrations/opencode-install-channel.md).

Why this channel, and the security trade it accepts:
[ADR-0047](../decisions/ADR-0047-opencode-installs-from-its-official-script-on-every-platform.md).

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

OpenCode pins no model of its own — devgeta ships no default here, so these are
suggestions, not configuration. Pick one per role and set it in OpenCode.

| Role                       | Family                    | Fallback, different lineage | When to reach for it                                    |
| -------------------------- | ------------------------- | --------------------------- | ------------------------------------------------------- |
| Daily coding (default)     | `anthropic/claude-opus`   | `openai/gpt-5.6-luna`       | Everyday coding, bug fixes, PR reviews                  |
| Hardest reasoning          | `anthropic/claude-fable`  | OpenAI's reasoning line     | When nothing else solves it                             |
| Agents + large repos       | `moonshotai/kimi`         | `deepseek/deepseek-v4-pro`  | Large codebases, multi-file refactors, long agent runs  |
| Deep review + architecture | `z-ai/glm`                | `openai/gpt-5.6-luna`       | Architecture decisions, hard debugging, critical review |
| Cheap bulk tasks           | `deepseek/deepseek-flash` | `z-ai/glm-5.3-flash`        | Background or non-critical automation                   |

**Fallbacks verified against OpenRouter on 2026-09-19 — re-check before
trusting one.** The left column names families on purpose, because point
releases land every few weeks and a version pinned in prose is wrong long
before anyone notices. The fallback column has to name specific models to be
useful, so it carries a date instead; treat an old date as a reason to look
rather than a recommendation. Resolve every current slug, price, and context
window at [openrouter.ai/models](https://openrouter.ai/models) — all three
move.

Luna appears twice on purpose: it is the only pick here from a lineage that
differs from **both** Anthropic and the Chinese open-weight cluster, which is
exactly what you want in the reviewer slot.

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

Wire the fallback column above into **OpenRouter's own failover** rather than
switching models by hand: pass a `models` array on the request and it moves on
when your first choice is down, rate-limited, or erroring. Current request
shape is in [OpenRouter's docs](https://openrouter.ai/docs).

The different-lineage rule from the reviewer section applies here too, for a
different reason: a fallback sharing a provider with its primary is taken out
by the same outage, the same price change, and the same bad release. That is
why the column crosses lineages on every row rather than falling back to a
sibling model.

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
