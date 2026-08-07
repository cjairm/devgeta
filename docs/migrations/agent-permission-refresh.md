# Agent permission refresh (worktrees unblocked, scratch dir added)

`dg configure` only re-renders an agent's config the first time it runs, or
when you pass `--force` — that is what lets your own edits to `settings.json`
or `opencode.json` survive an ordinary devgeta upgrade. This release changes
what both agents' default config denies and grants, and an existing install
needs `--force` to receive it.

## What changed

Two problems in the shipped permission policy are fixed:

1. **In-repo worktrees were denied.** The blanket `Edit(.claude/**)` /
   `Edit(.opencode/**)` deny matched a directory of that name at any depth,
   so every file inside `<repo>/.claude/worktrees/<name>/` — where
   `claude --worktree` and devgeta's own `worktree.location=in-repo` put
   checkouts — was wrongly denied too. It is replaced by a hook
   (`agent-config-guard.sh`/`.js`) that keeps the same protection for the
   files that actually matter (settings, hooks, agent/command/skill
   definitions, plugins) while carrying a real exception for worktrees.
2. **`/review-pr` and `/create-pr` wrote scratch files to `/tmp`,** which
   both agents prompt on. They now allocate under a devgeta-owned scratch
   directory instead (`~/.cache/devgeta/scratch` by default), granted
   through `permissions.additionalDirectories` (Claude Code) and
   `permission.external_directory` (OpenCode).
3. **Claude Code's local memory was denied.** The blanket `Edit(~/.claude/**)`
   deny — and the guard's own `.claude` rule — both covered
   `~/.claude/projects/<slug>/memory/`, so the agent could not write a memory
   file. Memory holds notes, not permissions or hooks, so both layers now
   carve it out. Everything else under `~/.claude/` stays denied.

See [ADR-0014](../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)
and [ADR-0015](../decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md)
for the full design.

## Does this affect you?

Each agent carries a different marker, because the two deploy the guard
differently: Claude Code names the hook script inside `settings.json`, while
OpenCode discovers plugins from a directory and never mentions them in
`opencode.json`. Check both the guard and the scratch grant:

```bash
# Claude Code — the first two should print a number greater than 0,
# the third should print 0 (the blanket deny that blocked memory is gone)
grep -c agent-config-guard ~/.claude/settings.json
grep -c additionalDirectories ~/.claude/settings.json
grep -c 'Edit(~/.claude/\*\*)' ~/.claude/settings.json

# OpenCode — the first should list a file, the second print a number greater
# than 0, the third print 0
ls ~/.config/opencode/plugin/agent-config-guard.js
grep -c external_directory ~/.config/opencode/opencode.json
grep -c '"~/.claude/\*\*"' ~/.config/opencode/opencode.json
```

For either agent: a `0` on one of the first two, a `1` on the third, or a
"No such file or directory" means that install predates this change and needs
the refresh below. All checks satisfied means it already has it — nothing to
do, and **do not run `--force`**, which would re-render over any hand-edits
for no gain.

An install from the first release of this guide passes the guard and scratch
checks but fails the third: the memory fix came later, and it needs the same
refresh.

## Run these

**Run only the command for an agent whose checks above failed.** The two are
independent: refreshing an agent that is already current gains nothing and
costs you a full re-render of its config. Refreshing an agent you don't even
use is worse — `dg configure opencode --force` creates OpenCode's config
directory whether or not OpenCode is installed, so a Claude-only setup would
end up with config for an agent it never had.

**If you have hand-edited the file you're about to refresh, diff it first** —
`--force` fully re-renders `settings.json` / `opencode.json` from the
template, which discards edits that were never tracked as a devgeta setting.
Save anything you'd lose before continuing.

```bash
# only if the Claude Code checks failed
dg configure claude --force

# only if the OpenCode checks failed
dg configure opencode --force
```

Then re-run the checks above for whichever agent you refreshed; all of its
checks should now be satisfied.

## If something goes wrong

**A worktree edit is still denied after refreshing.** Confirm the deployed
guard is actually the new one: `~/.claude/agent-config-guard.sh` should
exist and be executable (`ls -l ~/.claude/agent-config-guard.sh`). If it's
missing, `--force` didn't complete — rerun it and check for an error.

**A memory write is still denied after refreshing.** Check the deny list
itself — `grep '~/.claude' ~/.claude/settings.json` should list the
enumerated entries (`*.json`, `*.sh`, `*.md`, `agents/**`, `commands/**`,
`skills/**`, `plugins/**`, `hooks/**`, `lib/**`) and **no** bare
`~/.claude/**`. If the blanket is still there, `--force` didn't run against
a rebuilt binary: `dg configure` deploys configs from the running binary, so
`make build` (or reinstalling) has to happen first.

**You use a relocated `CLAUDE_CONFIG_DIR`.** This refresh does not reach
you: devgeta deploys to a fixed `~/.claude/`, so a relocated Claude config
root gets none of devgeta's Claude integration, not just this change. That
is a separate, larger gap this release does not close.

**You want the old behavior back.** Restore `Edit(.claude/**)` and
`Edit(.opencode/**)` in `configs/claude/settings.json.tmpl` and
`configs/opencode/opencode.json.tmpl`, remove the `agent-config-guard`
hook/plugin registration, rebuild (`make build` — `dg configure` deploys
from the running binary, so skipping this redeploys the change you just
reverted), then re-run `dg configure --force` for the agent(s) you had
refreshed — same rule as above, only the ones you actually use.
