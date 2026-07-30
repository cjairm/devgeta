# ADR-0010 — Worktree layout is a setting, and git is the worktree index

**Date:** 2026-07-29
**Status:** ACCEPTED

## Context

Devgeta stores every worktree in one shared root outside your projects:
`~/.local/share/devgeta/worktrees/<repo-slug>/<flat-name>`. Claude Code's native
worktree feature uses `<repo>/.claude/worktrees/<name>` instead, so a user of both
ends up with two disjoint sets of worktrees and `dg ws` only ever sees one of them.

[docs/migrations/v1-to-v2.md](../migrations/v1-to-v2.md) sketched a hand-migration
to the in-repo layout. It was applied for real on 2026-07-29 and the result proved
the guide's own warning: **the worktree subsystem does not survive a worktree
moving.** Five call sites compute a worktree's location from convention instead of
asking anything authoritative:

| Site                                        | Assumption                    | Broke                       |
| ------------------------------------------- | ----------------------------- | --------------------------- |
| `List()` (`worktree.go:459`)                | scans only the shared root    | `dg wt ls`, `dg ws`         |
| `findRepoForWorktree` (`worktree.go:586`)   | scans only the shared root    | `dg wt remove <name>`       |
| `worktreePath()` (`worktree.go:131`)        | computes the shared-root path | `repair`, `remove`, `state` |
| `repo_candidates.go:111`                    | joins the shared root         | `dg ws` repo picker         |
| `GetWorktreeBasePath()` (`worktree.go:136`) | the one hard-coded root       | new worktrees               |

Worse, `List()` also produced a **phantom row with an empty branch** whenever a
git-ignored leftover (an empty `.superpowers/` scratch dir was enough) stayed
behind in the old directory: it enumerates _directories_, so a husk still counts
as a worktree it can no longer read.

Two questions have to be answered together, because answering either alone leaves
the split state that broke: **where do worktrees live**, and **how does devgeta
find them**.

### Constraints

- **The shared root cannot stop being the default.** Changing it is a breaking path
  change (CLAUDE.md §10) and a v2.0.0 bump. Existing installs must not move.
- **In-repo worktrees litter the user's repo** unless their `.gitignore` covers
  `.claude/worktrees/`, and devgeta cannot edit other people's `.gitignore`. So the
  in-repo layout can only ever be opt-in.
- **Devgeta treats Claude Code and OpenCode symmetrically.** Adopting one agent's
  convention as _the_ layout tilts that; offering it as a choice does not.
- **`List()` runs on `dg ws`'s 3-second refresh.** ADR-0005's work removed a
  per-worktree tmux exec from that path; enumeration must not reintroduce a
  per-item exec cost.

## Decision

### 1. The layout is a `dg config` setting, defaulting to today's behavior

Register `worktree.layout` in the `dg config` registry with values `shared`
(default, `~/.local/share/devgeta/worktrees/<repo-slug>/<name>`) and `in-repo`
(`<repo>/.claude/worktrees/<name>`). Absent means `shared`, so no existing install
changes and no major bump is needed.

Rejected: **auto-detecting** the layout from the presence of
`<repo>/.claude/worktrees/`. It makes `dg wt create`'s destination depend on
whether some other tool happened to create a directory, which is invisible and
unpredictable. A setting the user can read with `dg config` is the whole point of
v1.6.0.

### 2. Git is the worktree index — devgeta stops deriving paths from convention

`git worktree list --porcelain`, run once in any worktree of a repo, already
returns **every** worktree of that repo with its absolute path and branch. That is
an authoritative, layout-independent index that devgeta currently ignores while
maintaining a weaker one by directory-scanning.

So enumeration becomes: for each known repo, ask git once. Known repos are the
union of the recent-repos store (ADR-0002), the repo-slug directories in the shared
root, and the current repo — no new state, per CLAUDE.md's "prefer an existing
source of truth over a parallel one that can drift."

This is **cheaper than today, not dearer**. `List()` currently calls
`ListWorktreesAt` once _per worktree_ (`worktree.go:506`) to recover each branch —
N git execs. One call per _repo_ returns the same data for all of that repo's
worktrees, so the exec count drops from per-worktree to per-repo, and the 3-second
refresh gets faster wherever a repo has more than one worktree.

It also kills the phantom-row class of bug outright: a directory git does not call
a worktree stops being listed as one, whatever is left in it.

Rejected: **scanning both locations.** It fixes only the layouts we hard-code,
keeps two conventions in the code forever, and still can't see a worktree git moved
anywhere else. Rejected: **a registry of worktree paths in `global_config.yaml`** —
a second index that can drift from git, which is the problem, not the fix.

### 3. Relocating an existing worktree is a devgeta command

Add `dg wt move <name> [--to <layout>]`, wrapping `git worktree move` (via a new
`Git.MoveWorktree`, since CLAUDE.md requires external tools go through their app
wrapper) and then retargeting the worktree's tmux window at the new path. The
migration proved that moving by hand leaves the window pointing at a path that no
longer exists — one shell fell back to `$HOME`, where an agent would have run git
against the wrong repo.

## Consequences

**Easier.** `dg ws` and every `dg wt` command work on a worktree wherever it lives,
including one created by Claude Code's own tool, which devgeta has never been able
to see. Phantom rows become unrepresentable. The 3-second refresh does fewer execs.
`docs/migrations/v1-to-v2.md` can finally become a real, listed migration instead of
a draft nobody may follow.

**Harder.** Enumeration now depends on knowing which repos to ask, so a repo that
is in neither the recent-repos store nor the shared root is invisible until it is
used once — where today a directory in the shared root is enough. Accepted: that is
the same reachability the picker already has, and `dg wt create` records the repo.

**Trade-offs accepted.** Two layouts must be supported forever rather than one, and
`worktreePath()` becomes layout-dependent — every caller must go through it rather
than rebuilding the path. Git becomes a hard dependency of _listing_ worktrees, not
just mutating them, so a repo whose git metadata is broken lists as empty instead of
showing husks; that is the honest answer and it is what the phantom row was hiding.

**Not decided here.** Whether the in-repo layout should ever become the default.
This ADR makes it selectable and reversible; promoting it would be a separate
decision and a major bump.
