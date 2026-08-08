# Keeping the two AI agents in sync

Devgeta configures two AI coding agents, Claude Code and OpenCode, and they must
behave the same. Every policy change goes into **both** files, expressed in each
one's own format.

The hard rule lives in [CLAUDE.md §12](../../CLAUDE.md) ("Keeping the two AI
agents in sync"); this guide is the detail behind it: where each concern lives,
what the enforcing test does and does not check, and which differences are
deliberate.

---

## Where each concern lives

| Concern                 | Claude Code                                                       | OpenCode                                                                   |
| ----------------------- | ----------------------------------------------------------------- | -------------------------------------------------------------------------- |
| Permissions             | `configs/claude/settings.json.tmpl`                               | `configs/opencode/opencode.json.tmpl`                                      |
| Formatting on save      | `configs/claude/format.sh`                                        | `formatter` block in `opencode.json.tmpl`                                  |
| Command redirects       | `configs/claude/task-redirect.sh`                                 | `configs/opencode/plugin/task-redirect.js`                                 |
| Agent activity state    | `configs/claude/agent-state.sh`                                   | `configs/opencode/plugin/notify.js`                                        |
| Agent-config protection | `configs/claude/agent-config-guard.sh` + settings.json.tmpl floor | `configs/opencode/plugin/agent-config-guard.js` + opencode.json.tmpl floor |
| Scratch dir grant       | `additionalDirectories` in `settings.json.tmpl`                   | `external_directory` in `opencode.json.tmpl`                               |
| Agents / commands       | `configs/shared/` (synced to both)                                | `configs/shared/` (synced to both)                                         |

`configs/shared/skills/` is synced to both agents too, but it is **not** a place
to put devgeta policy: those skills run in every user's other repositories, and
most are vendored from upstream Superpowers. A devgeta rule belongs in CLAUDE.md
or a guide. That holds for everything else in this table as well — see CLAUDE.md
§12, "Anything we ship is built for strangers".

## Rules

- **Never add a deny/ask rule, or a formatter language, to one agent only.** The
  two permission sets must stay the same rule for rule, and the two formatter
  language lists must cover the same file extensions.
- `internal/apps/opencode/permissions_test.go` enforces this and fails the build
  on any asymmetry, in either direction. It is the reason the lists cannot drift
  again — do not weaken it to land a one-sided change. If a rule genuinely cannot
  be expressed in one agent, drop it from both.
- **That test compares pattern STRINGS, not what they match.** A rule can be
  present in both configs, identical, and still enforce nothing — the
  `~/`-anchored rules are inert on OpenCode today, because it matches paths
  project-relative. Read
  [agent-permission-matching.md](agent-permission-matching.md) before adding or
  re-anchoring a rule; symmetry on paper is not symmetry in effect.
- Deploy both after any change: `dg configure claude --force` **and**
  `dg configure opencode --force`.

## Accepted differences

Deliberate — do not "fix" these by halves.

- **Agent frontmatter is enforced by OpenCode only; command frontmatter is
  ignored by both.** Agent `.md` files use OpenCode's `permission:` schema;
  Claude Code ignores it. OpenCode's `tools:` is `object<string, boolean>`,
  Claude Code's is comma-separated — the two schemas can't share one key, so
  the reviewer agents are read-only in OpenCode but unrestricted in Claude
  Code. Command-level `permission:` and `tools:` blocks have no effect in
  either agent. A command's permissions come from the agent it runs under,
  which a command can select with `agent:`. Unifying this needs per-agent
  frontmatter rendering from one policy source (an ADR-level change, not yet
  made). Because of this, **a command that posts outward — to a PR, a ticket,
  anywhere a human sees it — must grant that authorization in its own prose.**
  The frontmatter that used to imply it was removed once it turned out to
  enforce nothing, and with nothing in its place the agent falls back to asking
  the human before every post. Say plainly in the command file that running it
  _is_ the authorization and that the agent must not ask first;
  `TestPostingCommandsDeclareStandingAuthorization` fails the build for a
  posting command that doesn't. The same holds for a command that acts locally
  without a further prompt — committing, pushing, or running unattended —
  guarded by `TestCommittingCommandsDeclareStandingAuthorization` and
  `TestReviewLoopRunsUnattendedWithoutAsking`.
- **The lint feedback loop is Claude-only.** `format.sh` returns linter findings
  via `hookSpecificOutput.additionalContext`; OpenCode's `formatter` block cannot
  return context, and OpenCode surfaces LSP diagnostics instead.
- **`statusLine` has no OpenCode equivalent.**
