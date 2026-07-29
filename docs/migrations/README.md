# Migrations

Upgrade notes for changes that need you to do something by hand — moving files,
moving folders, or re-running a command after you update devgeta.

Most devgeta releases need nothing from you. A file only lands here when an
upgrade cannot fix itself automatically.

| Guide      | Applies when |
| ---------- | ------------ |
| _none yet_ | —            |

A guide appears in that table only once the change it describes has shipped.
Drafts for unshipped changes stay out of it, so nobody follows steps that don't
work yet — [v1-to-v2.md](v1-to-v2.md) is one, and says so at the top.

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
