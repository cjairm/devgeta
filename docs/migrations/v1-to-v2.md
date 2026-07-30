# v1 → v2: worktree location (DRAFT — do not follow yet)

**Status: DRAFT. Not usable.** The default has not changed, and step 3's
verification does not work yet: `WorktreeManager.List()`
(`internal/tooling/worktree/worktree.go:458-533`) only scans the shared root, so
`dg wt list` cannot see a worktree moved inside a repository. Following this
guide today leaves worktrees that devgeta can no longer find.

**Blocked on work that does not exist yet.** Making this real needs layout-aware
enumeration in `List()`, a way to choose the layout, an ADR approving the change,
and **a `dg wt move` command, which does not exist** — `dg wt` has only create,
list, remove, repair, and prune, and `internal/apps/git` has no `MoveWorktree`.
Step 3 below therefore falls back to raw `git worktree move`, which git handles
correctly but devgeta does not know about: the moved worktree drops out of
`dg wt list` and its tmux window keeps pointing at the old path. There is no way
to finish this migration with devgeta commands alone.

Of the four, only "a way to choose the layout" has a mechanism as of v1.6.0:
`dg config` ships a settings registry, so the layout could be a registered key
instead of new bespoke plumbing. Nothing registers such a key yet.

No cycle owns any of this, no ADR governs it, and the direction itself has not
been approved — it is an option under evaluation, not a commitment. The
[2026-07-29 hook re-scope cycle](../plans/cycles/2026-07-29-hook-rescope-and-worktree-location.md)
explicitly excludes it and does **not** implement any of it.

This file is only the design draft of what the migration would say, kept so the
reasoning isn't lost. It is deliberately **not** listed in
[the migration index](README.md). If the direction is dropped, delete this file.

## What changed

A **worktree** is a second working folder for the same repository, so you can
have more than one branch checked out at once.

Devgeta keeps all of them in one shared folder outside your projects. The
proposed v2 layout puts each repository's worktrees inside that repository:

```
v1:  ~/.local/share/devgeta/worktrees/<repo>/<name>
v2:  <your-repo>/.claude/worktrees/<name>
```

The v2 path is the folder Claude Code's own worktree feature uses, so both tools
would find the same worktrees.

Nothing moves on its own. Git records each worktree's location internally, so
copying folders by hand breaks them — use `git worktree move`.

## Does this affect you?

```bash
dg wt list
```

Empty list → nothing to do.

## Run these

### 1. Finish anything you no longer need

**This step deletes work. Read it before running anything.**

Go worktree by worktree — there is no bulk command that is safe here. For each
one you are done with:

```bash
dg task worktree-finish <name> --merge     # merge the branch, then remove it
dg task worktree-finish <name> --discard   # throw the branch away
```

Or remove the worktree and its window directly:

```bash
dg wt remove <name>
```

**This deletes the branch too.** Despite reading like the gentler option, `remove`
force-deletes the branch with `git branch -D`
(`internal/tooling/worktree/worktree.go:1036`, passing `deleteBranch: true`) — the
same loss as `--discard`. If the branch name contains a `/`, the delete targets
the flattened worktree name instead, fails to match, and the failure is swallowed,
so whether your branch survives depends on its name. Always pass an explicit
`<name>`: with no argument this opens an fzf picker, which an agent cannot answer.

Anything you finish here is one less thing to move.

> **Do not use `dg wt prune` for this.** Despite the name, it removes **every**
> worktree devgeta manages (`internal/tooling/worktree/worktree.go:971-1004`) — it
> does not merge, and it does not let you pick. Running it here would delete the
> worktrees you were about to migrate.
>
> **It does not prompt an agent.** The `Remove all? [y/N]` gate is
> `confirmFromTTY()` (`worktree.go:1053-1069`), which returns `true` immediately
> when `$TMUX` is set. Devgeta runs every agent in a tmux window, so for an agent
> there is no confirmation at all — prune is silent, unconditional deletion of
> every worktree in the shared root.

If that empties `dg wt list`, you are done — skip the rest.

### 2. Create the destination folder

```bash
mkdir -p <your-repo>/.claude/worktrees
```

`git worktree move` does **not** create parent directories. Without this it
fails with `fatal: failed to move ... No such file or directory` and moves
nothing.

### 3. Move each leftover

```bash
git -C <your-repo> worktree move <old-path> <your-repo>/.claude/worktrees/<name>
```

`git worktree move` updates git's own bookkeeping as it moves the folder. Use it
instead of `mv`.

### 4. Check it worked

```bash
git -C <your-repo> worktree list
```

Git is the source of truth here and should show the new path.

`dg wt list` will **not** show it correctly — see the status note at the top of
this file. Both shapes of wrong were confirmed by actually running this:

- The worktree disappears from `dg wt list` and `dg ws` entirely, or
- it shows as a **phantom row with an empty branch column**, if anything
  git-ignored was left behind in the old directory (an empty `.superpowers/`
  scratch dir is enough). `List()` enumerates _directories_ under the shared
  root, so a leftover still registers as a worktree it can no longer read.

Your tmux window also ends up pointing somewhere wrong — see "If something goes
wrong" below; that section understated this.

### 5. Ignore the new folder

Add to that repository's `.gitignore`, so the worktrees don't appear as
untracked files in your own repo:

```
.claude/worktrees/
```

Skip only if the line is already there.

### 6. There is no step 6, and that is the whole problem

Nothing above makes devgeta _use_ the new location going forward. The path is
hard-coded (`worktree.go:132` and `:137`), so once you migrate you are in a split
layout permanently: the worktrees you moved sit at the new path, while every
`dg wt create` and every `n`/`N` in `dg ws` keeps creating at the old shared root.
`dg wt remove`, `dg wt repair`, and `dg wt prune` also stop working on anything
you moved — `repair` cannot rebuild a moved worktree's tmux window, so a window
broken by step 3 has to be fixed by hand.

The setting that would close this does not exist. `dg config` (v1.6.0) is the
mechanism it would use, but no layout key is registered, so there is nothing to
set. Until that lands, this migration is a one-way trip into a split layout, and
that — more than any individual step below — is why this guide is not usable.

## If something goes wrong

**`fatal: failed to move ... No such file or directory`** — you skipped step 2.
Nothing was moved; create the folder and retry.

**`fatal: validation failed, cannot move working directory`** — the worktree has
uncommitted changes or is locked. Commit or stash inside it, then retry. Nothing
was moved.

**Your tmux windows point at the old folder.** Moving a worktree doesn't move
the terminal windows devgeta opened for it. Close them and reopen — no work is
lost, the windows just point at a path that no longer exists.

**You want to go back.** Run step 3 with the old and new paths swapped.
`git worktree move` works in both directions.

Requires git 2.17 or newer (`git worktree move`). Check with `git --version`.
