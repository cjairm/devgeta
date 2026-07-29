# Devgeta Roadmap

Public roadmap of planned features, improvements, and discussion topics for future releases.

---

## Planned Commands

The following commands are planned but not yet implemented:

### Configuration & Management

- **Per-item version/install-timestamp tracking** — Requires a `global_config.yaml` schema
  change + migration strategy (`dg list` shipped without this; it shows name + category only)

- **`dg validate` repair actions** — Add in-TUI repair/reinstall actions to the validate
  dashboard (shipped read-only: presence/drift detection only)

- **`dg update [app] [options]`** — Check and apply updates
  - Complex due to breaking changes
  - Support app-specific flags: `dg update --neovim=[options] --aerospace=[options]`
  - Must handle version compatibility

- **`dg check-updates`** — Find available updates for all installed packages
  - Report which apps have updates available
  - Depends on version tracking existing first

### Customization

- **`dg change --theme=[options] --font=[options]`** — Modify environment
  - Change terminal theme (with background image updates)
  - Change font selection
  - Persist selections in global config

### Backup & Recovery

- **`dg backup [name]`** — Backup current configurations
  - Create snapshots of current installation state
  - Enable rollback workflows

- **`dg restore [backup]`** — Revert to previous configuration
  - Restore from saved backup point

### Worktree Enhancements

Shipped so far: `dg wt ui` TUI dashboard (attach, destroy + session-hop, repair, filter,
branch-diff pane), `dg wt new --repo` for cross-repo creation, and `n` in `dg wt ui` to
create a worktree without leaving the dashboard — a floating repo picker (cursor repo →
recent-repos store, MRU → zoxide → free-typed path) followed by a floating name prompt,
then attach-and-quit on success. Every create (TUI or CLI) records the repo root in
`global_config.yaml`'s `worktree.recent_repos`, which is what lets the picker rank repos
that have no live worktrees. Cycle doc:
[docs/plans/cycles/2026-07-15-wt-ui-create-flow.md](docs/plans/cycles/2026-07-15-wt-ui-create-flow.md).

Also shipped: filesystem repo discovery via `worktree.search_paths` (opt-in; the picker
also offers repos it's never seen a worktree or zoxide entry for), and window layouts —
built-in `opencode`/`claude`/`claude-nvim`/`nvim` layouts selectable via
`worktree.default_layout`, `--layout` on `dg wt new`/`dg wt repair` (mutually exclusive
with `--ai`), and `N` in `dg wt ui` (same flow as `n`, plus a layout-picker step). Cycle
doc:
[docs/plans/cycles/2026-07-17-wt-ui-repo-scan-and-layouts.md](docs/plans/cycles/2026-07-17-wt-ui-repo-scan-and-layouts.md).

Next steps below come from a July 2026 investigation of how competing multi-session tools
(Claude Squad, worktrunk, uzi, sesh, gwq, Crystal) minimize keystrokes for session
creation — the common winning patterns are single-key in-TUI create, create+attach
collapsed into one action, and "the name is the only thing you type; everything else is
inferred."

- ⚪ **Prompt-first create** — Optionally capture a task prompt at creation and pass it to
  the AI coder so the agent starts working immediately (Claude Squad's `N`, worktrunk's
  `wt switch -x claude -- 'task'`, uzi's `uzi prompt`).
- ⚪ **Auto-naming** — Generate a branch/worktree name when none is given (uzi generates
  names automatically; gwq/ccmanager derive paths from templates so the branch name is the
  only input).
- ⚪ **User-defined custom layouts** — Let users declare their own named layouts in
  `global_config.yaml` beyond the built-in `opencode`/`claude`/`claude-nvim`/`nvim` set,
  once that built-in set and the pane model have proven out in practice.
- ⚪ **Per-worktree layout memory** — Have `dg wt repair` (and TUI auto-repair) rebuild the
  layout a worktree was originally _created_ with, instead of always re-resolving via
  `--layout`/`--ai`/env/`default_layout`/`default_ai`. Requires storing a layout name per
  worktree in `global_config.yaml`.

The three items below come from a July 2026 review of herdr.dev, a standalone (non-tmux)
multiplexer built specifically for orchestrating coding agents. Its headline feature —
per-agent state (blocked/working/done/idle) surfaced at a glance — is already covered by
[ADR-0005](docs/decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md); herdr's
persistent sidebar isn't something tmux panes can replicate (a pane belongs to one window,
full stop — herdr can only do it because it's its own multiplexer, not a tmux client), so
these lean on tmux's own idioms (status bar, popups) instead of chasing that literally.

- ⚪ **Cross-session status-bar indicator** — Aggregate `@dg_window_agent_state` across every
  session (not just the current one) into `status-right`, e.g. via a `dg status --brief`
  one-liner, so "N sessions want attention" is visible without opening `dg ws` or switching
  sessions.
- ⚪ **`dg ws` as a tmux popup** — Add a `worktree.dashboard_mode: window|popup` config option
  so `C-t` can open `dg ws` via `display-popup -E` instead of `new-window`: a dismissable
  overlay that doesn't disrupt the current window/pane layout, closer to what a "sidebar"
  is actually used for. Opt-in, since it changes the existing `C-t` binding's behavior
  (CLAUDE.md §10).
- ⚪ **Agent-orchestrable `dg ws` state** — Expose the per-worktree/session agent-state data
  ADR-0005 already collects through a scriptable interface (list sessions, wait-for-idle,
  last output), so an agent or wrapper script can drive `dg ws` the way herdr's socket API
  lets agents drive it. Highest-value gap versus herdr; most of the underlying plumbing
  already exists.

---

## Planned Categories & Tools

### AI & Development Tools

**Category `ai-tools`** — shipped 2026-07-23 with rtk as first app ([ADR-0004](docs/decisions/ADR-0004-ai-tools-install-category.md))

- ~~**rtk** — Token-compressing CLI proxy~~ ✅ Shipped: `dg install --only rtk` (brew on macOS, GitHub release binary on Debian); the command-rewriting hook (`rtk init -g`) stays opt-in — see [docs/apps/rtk.md](docs/apps/rtk.md). Revisit automating the hook once the project stabilizes.
- **Ollama** — Local LLM inference engine
- Gemini CLI integration
- IDE AI integrations (Claude Code, Opencode configuration)
- Terminal app for running AI commands (TBD)

**Related Questions:**

- ~~Should this be a separate category or extend existing categories?~~ Answered: separate `ai-tools` category (ADR-0004)
- How to integrate with Opencode / Claude Code setup?

### Alternative Tools

Users should be able to choose between alternatives:

- **Multiplexer:** Tmux vs. Zellij
- **IDE:** Opencode vs. Claude Code (Claude Code preferred)
- **Window Manager:** Existing options, possibly add more

### Additional Apps (Backlog)

- **Postman** — API testing
- **DevPod** — Dev containers with SSH/Vim support
- **Snap packages** — Slack, Spotify, VS Code (with `sudo apt install snapd`)
- **Music players** — TBD
- **Email clients** — TBD
- **Language-specific tools:** PHP/Go/Python dev tools
- **System utilities:** Font management (fc-list support)

---

## Known Open Questions

### Development & Architecture

- **Mise management** — What's the best approach? Documentation is difficult to follow
- **Git integration** — Should git-related commands be namespaced as `dg git clean --flags`, `dg git revert`, or just `dg clean-branch`?
- **NPM integration** — Should we offer `dg npm clean` for fresh installs and full cleanup?
- **Update strategy** — How to handle updates when apps can have breaking changes?
- **Shortcut creation** — Do we need to create shell shortcuts during installation?

### Features & Scope

- **Cleanup utilities** — Extend [existing cleanup script](https://github.com/cjairm/devgeta/blob/038da72eec456e0a60c50dce2bc9ab615c795fb2/configs/bash/init.zsh#L15) to remove node_modules, docs, examples, unused files?
- **Opencode integrations** — Check [Opencode.nvim](https://github.com/nickjvandyke/opencode.nvim) for nvim integration
- **Super powers** — How to extend [super powers](https://github.com/obra/superpowers) for Opencode?
- **Auto Claude integration** — Reuse or build [Auto Claude](https://github.com/AndyMik90/Auto-Claude)?

### References & Templates

- https://www.aitmpl.com/agents — Agent templates
- https://huggingface.co/pricing — HuggingFace pricing options
- https://a2ui.org/ — AI UI patterns
- https://genai.owasp.org/resource/cheatsheet-a-practical-guide-for-securely-using-third-party-mcp-servers-1-0/ — MCP security

---

## Release Timeline

TBD — To be determined based on:

- Community feedback
- Priority user requests
- Available development resources
- Dependencies (e.g., external library updates)

---

## How to Contribute Ideas

Have a feature request or idea?

1. Check this roadmap — it might already be planned
2. Check [GitHub Issues](https://github.com/cjairm/devgeta/issues) — it might be discussed
3. [Open a new issue](https://github.com/cjairm/devgeta/issues/new) with the `feature-request` label
4. Or contribute directly — see `CONTRIBUTING.md`

---

## Status Legend

- 🟢 **Implemented** — Available in current release
- 🟡 **In Progress** — Active development in a cycle
- 🔵 **Planned** — Decided, scheduled for development
- ⚪ **Proposed** — Under discussion, not yet committed
- ❌ **Deferred** — Decided not to implement

---
