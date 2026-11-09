# devgeta removed its agent hooks, agent-only `dg task` commands and most permission rules

devgeta no longer ships the layer it had built around Claude Code and OpenCode:
the command-rewriting and output-capping hooks, the two guard hooks that blocked
secrets and lint suppressions, the agent-only `dg task` subcommands, and about
110 permission rules per coder. Both coders now get a small permission floor.
See [ADR-0066](../decisions/ADR-0066-devgeta-removes-its-token-reduction-layer.md)
and [ADR-0067](../decisions/ADR-0067-agent-permission-floor.md) for why.

Upgrading changes nothing on its own. The new settings and plugins arrive when
you run `dg configure claude --force` and `dg configure opencode --force`, and
`dg configure` does not delete Claude hook scripts it no longer ships.

## Do you need this?

```bash
ls ~/.claude/task-redirect.sh ~/.claude/output-budget.sh ~/.claude/secret-guard.sh \
   ~/.claude/suppression-guard.sh ~/.claude/lib ~/.config/devgeta/agent-runtime.json 2>/dev/null
```

If this prints nothing, you never had the removed hooks and there is nothing to do.

## Steps

```bash
dg configure claude --force
dg configure opencode --force
rm -f ~/.claude/task-redirect.sh ~/.claude/output-budget.sh \
      ~/.claude/secret-guard.sh ~/.claude/suppression-guard.sh
rm -rf ~/.claude/lib
rm -f ~/.config/devgeta/agent-runtime.json ~/.config/devgeta/output-budget-run.sh
```

1. `dg configure claude --force` rewrites `~/.claude/settings.json` without the
   removed hooks and with the new permission floor. It rewrites the whole file,
   so **any edits you made to `settings.json` by hand are lost**.
2. `dg configure opencode --force` does the same for `opencode.json`, and
   clears the old plugin files from `~/.config/opencode/plugin/`. Other tools'
   plugins in `~/.config/opencode/plugins/` are not touched.
3. The `rm` commands remove the Claude hook scripts and helper library that the
   new settings no longer reference. Left in place they do nothing, but they are
   dead files. Do this after step 1: until then `settings.json` still points at
   them.

## What else changed

- **`dg task` subcommands.** These now fail with "unknown command":
  `review-scope`, `branch-diff`, `review-package`, `review-notes`,
  `review-note`, `handoff`, `context-report`, `commit-trailers`, `issue-scope`,
  `worktree-start`, `worktree-finish`, and every PR subcommand (`review-threads`,
  `create-pr`, `merge-pr`, `pr-view`, and the rest). Still there:
  `refresh-branch`, `reset-main-branch`, `delete-branch`, `reinstall-libraries`,
  `reinstall-library`, `release`, `scratch`. Use `dg wt` for worktrees and `gh`
  directly for pull requests.
- **`integrations.output_budget`.** The setting is gone. If it is in your
  `global_config.yaml` it is ignored and dropped the next time devgeta saves
  the file.
- **Review journals.** `dg wt` removal no longer deletes a branch's review
  journal, and nothing reads one. Journal files from earlier reviews are
  harmless leftovers.

## The permission floor is looser

Both coders still deny reading secret and credential files (`.env`, SSH keys,
`.aws`, `.kube`, `.npmrc`, `.netrc` and similar) and shell history, still ask
before `git push --force`, `sudo`, `gh repo delete` and `gh release delete`, and
still deny edits under `.git/`. `agent-config-guard` still protects agent config.

No longer blocked: `curl`, `wget`, `rm -rf`, `aws`/`gcloud`/`az`, `kubectl delete`,
interpreter one-liners (`python -c`, `node -e`, …), `crontab` and `launchctl`,
and edits to `~/.zshrc`, `~/.bashrc` and `~/.profile`.
No longer asked about: `ssh`, `scp`, `rsync`, `gh api`, `gh pr merge`,
`gh pr close`, `gh issue delete`, `terraform apply` and `kubectl apply`.

**Nothing checks an agent's commits for secrets any more.** An agent can stage
and commit a secret file by name if `.gitignore` does not cover it. If that
matters to you, add a pre-commit scanner such as gitleaks to your repos.
