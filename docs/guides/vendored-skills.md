# Vendored Skills

Some skills under `configs/shared/skills/` are copied from upstream projects
rather than written here. We keep them as **byte-for-byte copies** of a pinned
upstream commit and update them by hand, from time to time, with the procedure
below. This file is the record of where each one came from — update it in the
same commit as every sync.

Why copies and not a plugin install: `configs/shared/skills/` is embedded in the
binary and deployed to **both** Claude Code and OpenCode by `dg configure`. An
upstream plugin would reach one agent only (see [agent-sync.md](agent-sync.md)).

## Manifest

| Skills                                                                                                                                                                                                                                                                                                                                                    | Upstream                                                              | Upstream path           | Pinned commit                                           | License                                 |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- | ----------------------- | ------------------------------------------------------- | --------------------------------------- |
| `brainstorming`, `diagnosing-superpowers`, `dispatching-parallel-agents`, `executing-plans`, `finishing-a-development-branch`, `receiving-code-review`, `requesting-code-review`, `subagent-driven-development`, `systematic-debugging`, `test-driven-development`, `using-git-worktrees`, `using-superpowers`, `verification-before-completion`, `writing-plans`, `writing-skills` | [obra/superpowers](https://github.com/obra/superpowers)               | `skills/<name>/`        | `2ce8aa15d05bd57213e6f746f953aafb7ded7a8b` (2026-10-21, v7.0.1) | MIT                                     |
| `skill-creator`                                                                                                                                                                                                                                                                                                                                           | [anthropics/skills](https://github.com/anthropics/skills)             | `skills/skill-creator/` | `1f630fd`                                               | Apache 2.0 (`LICENSE.txt` in the skill) |
| `ponytail`, `ponytail-debt`                                                                                                                                                                                                                                                                                                                               | [DietrichGebert/ponytail](https://github.com/DietrichGebert/ponytail) | `skills/<name>/`        | `09c434ab73ca43761c5d8b0c5d341926c2fba710` (2026-10-18) | MIT (`LICENSE` copied into each skill)  |

Not vendored — written here, never overwritten by a sync: `dg-worktree`,
`post-to-tracker`.

Only part of Ponytail is vendored. Its `ponytail-review` and `ponytail-audit`
compete with `review-pr` / `code-review` for the same requests, `ponytail-gain`
is upstream's benchmark display, and `ponytail-help` lists all six. The vendored
`ponytail` skill still mentions `/ponytail lite|full|ultra`, which works as a
skill argument; that line is upstream's and stays as is.

### Known differences from the pinned commit

A sync must end with these differences and no others. If a sync makes one
unnecessary (upstream fixed the same thing), delete it from this list.

| Skill                         | Difference                                                                                                    | Why                                                                                                             | Commit    |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- | --------- |
| `subagent-driven-development` | `scripts/task-brief` also accepts `Step N` headings and stops at the next heading of equal or shallower depth | Without it, `task-brief` cannot read plans whose units are headed `Step N`                                                             | `dccd7f3` |

The patch was carried forward on top of v7.0.1 with its comment made general;
the original named a devgeta path, which does not belong in a skill that runs
in other people's repos (CLAUDE.md §12).

## Sync procedure

Run from the repo root on a clean tree. `SCRATCH` is any empty directory
outside the repo. One upstream per commit, so a bad sync reverts cleanly.

1. **Clone upstream at the new ref** and write down its full commit hash:

   ```bash
   git clone https://github.com/obra/superpowers.git "$SCRATCH/upstream"
   git -C "$SCRATCH/upstream" log -1 --format='%H %cs'
   ```

2. **Read what changed upstream** since the pinned commit, before copying
   anything. Look in particular for new `/tmp` paths ([ADR-0015 §7](../decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md#7-vendored-skills-keep-tmp-knowingly)),
   new tool permissions, scripts that download or run code, and new skills or
   removed ones:

   ```bash
   git -C "$SCRATCH/upstream" log --oneline <pinned>..HEAD -- skills
   git -C "$SCRATCH/upstream" diff --stat <pinned> HEAD -- skills
   ```

3. **Replace each vendored skill wholesale.** `--delete` removes files upstream
   dropped; without it, stale files pile up (this is how the leftovers in the
   table above happened). Use `--exclude` only for files this repo adds on top,
   such as a copied `LICENSE`:

   ```bash
   for skill in brainstorming dispatching-parallel-agents ...; do
     rsync -a --delete "$SCRATCH/upstream/skills/$skill/" "configs/shared/skills/$skill/"
   done
   ```

   A skill that upstream added is not picked up automatically — decide whether
   to ship it, and add it to the manifest if so. A skill upstream removed:
   delete our copy, or keep it and mark it as no longer upstream in the manifest.

4. **Carry each known difference forward.** Re-apply it by hand or with
   `git show <commit> -- <path> | git apply -3`, and resolve conflicts against the
   new upstream text.

5. **Check the result is upstream plus the listed differences and nothing
   else:**

   ```bash
   diff -r "$SCRATCH/upstream/skills/<skill>" "configs/shared/skills/<skill>"
   ```

6. **Update this file**: the pinned commit and date, the differences table.

7. **Test.** Skills are embedded configs, so run the root package and the two
   agent app packages (CLAUDE.md §6, "Which tests to run"):

   ```bash
   go build ./... && go test . ./internal/apps/claude/ ./internal/apps/opencode/
   ```

8. **Deploy and try it** — maintainer's step: rebuild and reinstall the binary,
   then `dg configure claude --force` and `dg configure opencode --force`.
   Invoke one or two of the updated skills in each agent.

Commit message: `chore(skills): sync <upstream> skills to <short-hash>`, with
the upstream changes worth knowing about as bullets.
