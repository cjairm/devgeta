# Recent Changes

Short prose summaries of changes whose _reasoning_ isn't obvious from the code
or the commit. This is a reading aid, not a record — the permanent record is
`docs/decisions/` (why we chose it), `docs/plans/cycles/` (what the work was),
and git history (what changed).

**Pruning rule:** keep entries from the last two releases. Delete anything older
in the same pass that adds a new entry. If deleting an entry would lose
something a reader still needs, that thing belongs in an ADR or a guide — put it
there first, then delete. This file must not grow without bound; that is exactly
why it no longer lives in `CLAUDE.md`.

**Last updated:** 2026-11-08

---

## Recent changes

- devgeta installs agent packs (2026-11-08). Superpowers and Ponytail are not
  folders of skills: Superpowers' bootstrap is a hook in Claude Code, a plugin in
  OpenCode and an extension in Pi, and Ponytail lives mostly in hooks, so copying
  `skills/` (what devgeta did before the reset) breaks both. `dg agent-pack`
  instead fetches each at a pinned tag and commit, builds one tree devgeta owns
  (excluded skills removed, override layers copied on top, never editing the
  clone), and installs it through each coder's own system: a local `devgeta`
  marketplace for Claude Code, a `plugins` entry for OpenCode, `pi install` for
  Pi. Three things are not obvious. `dg configure --force` re-renders
  `settings.json` and `opencode.json` whole, so the pack entries are rendered from
  `agent_packs:` state or the next `--force` would silently disable every pack.
  Install is all-or-nothing across coders but a coder that already had the pack is
  not undone (all coders share one tree). And `dg install` installs packs only
  into coders it installed in that run, so re-running it never changes an existing
  coder's agent content. The `devgeta` pack brings `DEVGETA.md` back, to all three
  coders, with `@DEVGETA.md` added to `CLAUDE.md` only if no line imports it.
  Pi joined the coder parity where it has an equivalent; it has no permission
  system, so those gaps are listed in `docs/guides/agent-sync.md`. Checked in a
  real Claude Code session; OpenCode and Pi sessions not yet. Superpowers'
  bootstrap hook skips resumed sessions (`claude --continue`) by upstream design.
  Shipped as v2.0.0; upgrading from 1.x: `docs/migrations/agent-layer-v1-to-v2.md`.
  Decisions: ADR-0068 to 0070.

- devgeta pins the AI coders and moves to OpenCode 2 (2026-11-08). OpenCode
  2's installer is a new script, and 2.x silently ignores much of the 1.x
  config devgeta wrote: the theme, the permission format and the plugins all
  stopped working without an error. So devgeta now installs exact versions
  (OpenCode 2.2.6, Claude Code 2.1.319, in `constants.PinnedVersion`), turns
  their self-updates off, and writes OpenCode's config in its native 2.x
  form. Moving a pin is a devgeta change, checked in a sandbox `HOME` first.
  Pi is not pinned: its installer takes no version. The non-obvious part is
  OpenCode 2's shared background service. Every terminal connects to one
  server, and server plugins run inside it with the environment of whichever
  terminal started it, so the activity notifier became a TUI plugin (its
  `TMUX_PANE` is its own pane) that acts only on the session its client
  shows, and the agent-config guard's bypass variable is set on the service.
  Users upgrading must run the steps in `docs/migrations/opencode-2.md`.

- devgeta removed its agent layer (2026-11-08). The command-rewriting and
  output-capping hooks, both guard hooks (secret and lint-suppression), the
  agent-only `dg task` subcommands (review, handoff, PR, issue,
  worktree-start/finish) and the review journal are gone, and both coders
  now get a small permission floor: secret-file read denies, five Bash
  `ask`s, and `Edit(.git/**)`. The reason is measured: the token saving was
  under 2% of the bill, while blocked or shortened commands made agents
  redo work. What came out is recorded in ADR-0066 and ADR-0067; the last
  commit that still has both guards is `abd4fd6`, for anyone rebuilding
  them. `dg configure` does not delete the old Claude hook scripts, so the
  migration note lists them to remove by hand. Nothing now checks an agent's
  commits for secrets.

- Pi is a devgeta-managed terminal app (2026-11-04). `dg install` now sets up
  Pi (`@earendil-works/pi-coding-agent`) alongside Claude and OpenCode, from
  its official installer run under `sh` — its shebang is `#!/bin/sh`, the
  inverse of opencode's bash contract, so the same `RunInstallScript` staging
  needed a different interpreter. Three things make the app shape different
  from its neighbors. First, the script requires Node.js 22.19+ and installs
  Node itself only interactively, which devgeta never is — so the app runs
  its own preflight matching the script's, before anything is downloaded,
  and a clean-machine full install (terminal phase runs before languages)
  fails with "install Node first (`dg install --only languages`), then
  re-run" instead of a wasted download. Second, the managed install root
  (`~/.pi/agent/install`) shares a home with Pi's own state
  (`~/.pi/agent` — sessions, config, agent state), so uninstall removes
  only the `install` subtree; deleting all of `~/.pi` would take the user's
  sessions with it. Third, unlike opencode and claude, Pi gets no themes, no
  shared skills, no permission sync, and no shell feature — how devgeta
  should configure it is undecided until a later cycle, and the two-agent
  sync contract was deliberately not extended to a third (parity for Pi came
  in the agent packs cycle, above). The channel repeats
  ADR-0047's accepted opencode trade knowingly; ADR-0059 records it. Review
  follow-ups live in `docs/plans/cycles/2026-11-04-pi-app.md`.

## Recent specs completed

- `specs/001-binary-dist-audit/` — Go embed, text/template for config generation
- `specs/002-debian-package-fixes/` — Strategy pattern, package mappings, exponential backoff downloads
