# Migrations

Upgrade notes for changes that need you to do something by hand — moving files,
moving folders, or re-running a command after you update devgeta.

Most devgeta releases need nothing from you. A file only lands here when an
upgrade cannot fix itself automatically.

| Guide                                                      | Applies when                                                                                                                                                                                                                                    |
| ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [v1-to-v2.md](v1-to-v2.md)                                 | You want to move existing worktrees between the shared root and an in-repo `.claude/worktrees/` folder (or back) after changing `worktree.location`. Check with `dg wt list` — empty, or already at the location you want, means nothing to do. |
| [agent-permission-refresh.md](agent-permission-refresh.md) | You installed either agent before this release and want in-repo worktree edits unblocked and the `/tmp` scratch prompt gone. Check with the grep commands at the top of that guide.                                                             |

A guide appears in that table only once the change it describes has shipped.
Drafts for unshipped changes stay out of it, so nobody follows steps that don't
work yet.

## Naming

Files are named for the **layout version** they move you between, not for a
devgeta release number. One layout change can span several releases, and not
everyone upgrades in order.

## Writing a new one

Keep it to the commands. Someone reading it is mid-upgrade and wants to finish,
not study. The order that works:

1. What changed, in one or two sentences.
2. How to check whether it affects you at all.
3. The commands to run, numbered, one line of explanation each.
4. Anything that can go wrong, and what to do about it.

State plainly if a step can lose work.
