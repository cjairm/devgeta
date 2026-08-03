# v1 → v2: worktree location (moving between shared and in-repo)

A **worktree** is a second working folder for the same repository, so you can
have more than one branch checked out at once. Devgeta supports two places to
keep them:

```
v1: shared    ~/.local/share/devgeta/worktrees/<repo-slug>/<name>   (the default)
v2: in-repo   <your-repo>/.claude/worktrees/<name>
```

The in-repo path is the folder Claude Code's own worktree feature uses, so
both tools find the same worktrees when you opt in.

## What changed

`worktree.location` is a `dg config` setting (`shared` default, or `in-repo`)
that decides where **new** worktrees are created. `dg wt move` relocates an
**existing** worktree to match — it wraps `git worktree move` and then
retargets that worktree's tmux window at the new path, so `dg wt list` and
`dg ws` keep seeing it correctly no matter where it lives. The setting is
reversible: switch back to `shared` and move worktrees back the same way.

## Does this affect you?

```bash
dg wt list
```

Empty list → you have no worktrees yet; whichever location you configure
applies automatically to the next one you create, nothing to move. If it
lists worktrees you want at the other location, keep reading.

## Run these

### 1. Set the location you want new worktrees to use

```bash
dg config set worktree.location in-repo
```

Use `dg config set worktree.location shared` to go back — this is a two-way
setting, not a one-way jump. `dg config get worktree.location` prints the
current value; `dg config` (no arguments) lists it alongside every other
setting.

### 2. Move each worktree you want at the new location

```bash
dg wt move <name>
```

With no `--to`, `move` relocates `<name>` to match whatever
`worktree.location` is now configured to be — exactly the migration case.
Pass `--to shared` or `--to in-repo` to move one worktree without changing
the global default, and `--force` if the worktree has uncommitted changes
(without it, `move` refuses on a dirty worktree).

If the worktree already has a live tmux window, `move` sends every pane a
`cd` to the new path — but only when every pane in the window is an idle
shell. If any pane is running something else (an editor, a build, an AI
agent), nothing is sent to any pane, and `move` prints a warning naming the
busy pane so you can `cd` it yourself afterward. The move itself still
succeeds either way — a busy window is a follow-up inconvenience, never a
reason the command fails.

If you're moving `--to in-repo` and the target repo's `.gitignore` doesn't
cover `.claude/worktrees/`, `move` warns about it (it doesn't refuse, and it
never edits another repo's `.gitignore` for you) — add that line yourself so
the worktree doesn't show up as untracked files in your own repo.

If a worktree is already at the target location, `move` says so and exits
without touching git.

### 3. Confirm

```bash
dg wt list
dg ws
```

Both now resolve a worktree correctly regardless of which location it lives
in — this is the fix that makes the move reliable: earlier versions of this
enumeration only scanned the shared root, so a worktree moved elsewhere by
hand would drop out of `dg wt list` and `dg ws` entirely, or show as a
phantom row with an empty branch column.

## Leftover worktrees from an older devgeta install

If devgeta ever wrote worktrees under a **different data directory name** than
the one it uses today (for example an install that predates the current
`~/.local/share/devgeta/` path), git may still hold registrations pointing at
those old paths. The directories are long gone, but git does not forget them
on its own.

Devgeta no longer shows these. A registration whose directory is missing is
what git calls **prunable**, and `dg wt list` and `dg ws` now skip them, so
they cannot appear as rows you can't get rid of. Removing one through
`dg wt remove` (or `d` in the dashboard) also cleans up the leftover git entry
instead of silently doing nothing.

To clear the old entries in one go, across every repo devgeta knows about:

```bash
dg wt prune --stale
```

It prints each entry it cleared, and says so when there is nothing to clean.
Unlike a bare `dg wt prune` (which removes **all** your worktrees and asks for
confirmation first), `--stale` cannot remove a worktree, a directory, or a
branch — it only drops bookkeeping for directories that are already gone — so
it does not prompt.

`dg wt remove <name>` also clears a leftover entry now, if you happen to know
the name of a worktree you deleted by hand. Previously that reported
`nothing to remove` and left the entry in place.

To see what git thinks, the underlying check is:

```bash
git -C <your-repo> worktree list    # look for the word "prunable"
```

## If something goes wrong

**`move` refuses with a message about uncommitted changes.** Commit or stash
inside the worktree, then retry — or pass `--force` to move it dirty anyway.

**A tmux pane didn't follow.** That pane was busy (not an idle shell) when
you ran `move` — the warning names it. `cd` that pane to the new path by
hand; the worktree itself already moved correctly.

**You want to go back.** Run `dg config set worktree.location shared` and
`dg wt move <name>` again for each worktree — `move` works in both
directions.

`dg wt move` shells out to `git worktree move`, so it needs git 2.17 or newer.
Check with `git --version`.
