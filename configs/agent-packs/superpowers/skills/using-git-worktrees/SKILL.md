---
name: using-git-worktrees
description: Use when starting feature work that needs isolation from current workspace or before executing implementation plans - ensures an isolated workspace exists via native tools or git worktree fallback
---

# Using git worktrees: `devgeta worktree`

Follow UPSTREAM.md (in this skill's folder), with the changes below. Where they conflict, this file wins.

**Which tool.** Run `command -v devgeta`. If it finds `devgeta`, then `devgeta worktree` is the native worktree tool for upstream's Step 1a: use it instead of harness tools (such as `EnterWorktree`) and instead of `git worktree add`. Worktrees made this way show up in `devgeta worktree list` and can be reopened with `devgeta worktree repair <branch>`. If `devgeta` is not found, follow UPSTREAM.md unchanged.

**Step 0** (already in a worktree? consent) applies per repository.

## One issue across several repositories

Ask which mode the person wants, unless they already said:

- **One agent does the whole issue**: create the worktrees without windows, listing every repository including the current one (`.`):

  `devgeta worktree create <branch> --no-window --repo . --repo <other-repo>`

  Then work in the paths it prints, one per repository.

- **Each repository is a separate task with its own coder**: windows open and a coder starts in each:

  `devgeta worktree create <branch> --repo . --repo <other-repo> [--ai <coder>] [--prompt '<task>']`

  If the tasks need different prompts, run one command per repository. Add `--ai` only when the person named a coder (`opencode`, `claude` or `pi`).

One repository: `devgeta worktree create <branch>`, with `--no-window` to skip the window and coder.

The same branch name is created in every repository. If any repository fails, the command removes what it made and says which one failed.

Upstream's Steps 2 and 3 (project setup, baseline tests) run in each new worktree.
