# Agent layer v1 → v2: upgrading devgeta 1.x to 2.0

devgeta 2.0 replaces everything it used to put into Claude Code and OpenCode.
The bundled skills, commands, agents, hooks, `dg task` agent subcommands and
rtk are gone. Superpowers, Ponytail and `DEVGETA.md` now arrive as **agent
packs** (`dg agent-pack`), installed through each coder's own plugin system.
OpenCode moves to version 2, and both coders are pinned.

Updating the binary changes none of this on its own. This page is the whole
upgrade in one pass, in the order that works. Each step links to the note that
explains it in full.

## Does this affect you?

Only if a 1.x devgeta installed or configured Claude Code or OpenCode:

```bash
ls ~/.claude/settings.json ~/.config/opencode/opencode.json 2>/dev/null
```

Nothing printed means nothing to do. Just update devgeta.

## Before you start

`dg configure <coder> --force` (step 4) rewrites `~/.claude/settings.json` and
`~/.config/opencode/opencode.json` from scratch. **Hand edits to those two files
are lost.** Copy out anything you want to keep.

## Steps

1. **Remove rtk, if you have it** (`command -v rtk` prints a path). Do this with
   the old devgeta still installed, and before removing the binary, or Claude
   Code runs a missing hook on every Bash call:

   ```bash
   rtk init -g --uninstall
   brew uninstall rtk                    # Debian: sudo rm /usr/local/bin/rtk
   ```

   Its data folder holds command output and history. Deleting it can't be
   undone. Details: [rtk-removal.md](rtk-removal.md).

2. **Update devgeta** to 2.0:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/cjairm/devgeta/main/install.sh | bash
   ```

3. **Upgrade the coders** to the pinned versions (OpenCode 2.x, the pinned
   Claude Code). A newer version you installed yourself is left alone.

   ```bash
   dg install --only opencode
   dg install --only claude
   ```

4. **Rewrite both configs.** This drops the old hooks, plugins, rtk entries
   and permission rules, and applies the new permission floor:

   ```bash
   dg configure claude --force
   dg configure opencode --force
   opencode service restart
   ```

5. **Delete what 1.x deployed and 2.0 no longer manages.** `dg configure` does
   not delete files it stopped shipping. This removes only the names devgeta
   1.x put there. Your own skills, commands and agents in the same folders are
   kept.

   ```bash
   for dir in ~/.claude ~/.config/opencode; do
     rm -rf "$dir"/skills/{brainstorming,dg-worktree,diagnosing-superpowers,dispatching-parallel-agents,executing-plans,finishing-a-development-branch,ponytail,ponytail-debt,post-to-tracker,receiving-code-review,requesting-code-review,skill-creator,subagent-driven-development,systematic-debugging,test-driven-development,using-git-worktrees,using-superpowers,verification-before-completion,writing-plans,writing-skills}
     rm -f "$dir"/commands/{address-feedback,approve-pr,context-report,create-pr,explain-simply,finish-work,human,load-context,pr-review-loop,review-loop,review-pr,save-context,smart-commit,teach}.md
     rm -f "$dir"/agents/{code-reviewer,document-reviewer,skill-reviewer}.md
   done
   rm -f ~/.config/opencode/DEVGETA.md
   rm -f ~/.claude/task-redirect.sh ~/.claude/output-budget.sh \
         ~/.claude/secret-guard.sh ~/.claude/suppression-guard.sh ~/.claude/rtk-shim.sh
   rm -rf ~/.claude/lib
   rm -f ~/.config/devgeta/agent-runtime.json ~/.config/devgeta/output-budget-run.sh
   ```

   Skip this and the old skills load **next to** the Superpowers and Ponytail
   packs, so every skill shows up twice. The old commands and agents still
   call `dg task` subcommands that no longer exist. Run this after step 4,
   while `settings.json` still points at the hook scripts.

6. **Install the agent packs** into every configured coder:

   ```bash
   dg install --only agent-packs
   dg agent-pack list
   ```

   Your existing `@DEVGETA.md` line in `~/.claude/CLAUDE.md` is kept, and
   `~/.claude/DEVGETA.md` is replaced with the 2.0 version. Details:
   [agent-packs.md](agent-packs.md).

7. **Restart every Claude Code and OpenCode session.** Plugins, hooks and
   `@` imports load only when a session starts. `claude --continue` resumes a
   session, so Superpowers' start-up instructions don't run there. Start a
   fresh `claude` to get them.

## Check it worked

```bash
dg agent-pack list                    # each pack installed for claude and opencode
claude plugin list                    # superpowers@devgeta, ponytail@devgeta
opencode --version                    # 2.x
opencode debug config | head -40      # no warnings
```

## If you don't want the packs

Skip step 6. Then also delete the old instructions, which nothing updates
any more: `rm ~/.claude/DEVGETA.md`, and remove the `@DEVGETA.md` line from
`~/.claude/CLAUDE.md`. Future installs: `dg install --skip agent-packs`.

## What else changed

- **`dg task` agent subcommands are gone** (`review-*`, `handoff`,
  `create-pr` and the other PR commands, `worktree-start`/`finish`, …). So are
  `dg configure --force --only`, `dg task review-run`, the `dg ws` R key, and
  the `ai-tools` install category. Full list:
  [agent-customizations-rollback.md](agent-customizations-rollback.md).
- **The permission floor is looser.** `curl`, `rm -rf`, interpreter one-liners
  and cloud CLIs are no longer blocked, and nothing scans an agent's commits
  for secrets. Add a pre-commit scanner such as gitleaks if that matters.
  Details: [agent-customizations-rollback.md](agent-customizations-rollback.md#the-permission-floor-is-looser).
- **The coders no longer update themselves.** Updates come with devgeta
  releases. Details: [opencode-2.md](opencode-2.md#what-changes-for-you).
- **Old keys in `global_config.yaml`** (`rtk_claude_hook`,
  `integrations.output_budget`, `review.reviewers`, `review.rounds`) are
  ignored, and the next save drops them. `rtk` under `installed: packages:`
  stays until you delete it.
