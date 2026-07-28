# rtk

[rtk](https://github.com/rtk-ai/rtk) ("Rust Token Killer", Apache-2.0) is a CLI
proxy that filters and compresses the output of 100+ common dev commands (git,
test runners, docker, `cat`/`grep`, …) before an LLM agent reads it, cutting up
to 90% of bash output. It is the first app in devgeta's `ai-tools` category
(see [ADR-0004](../decisions/ADR-0004-ai-tools-install-category.md)).

## Relationship to `dg task`

Complementary, not competing — see
[task-design.md](../guides/task-design.md): `dg task` provides semantic
orchestration and policy (one-call flows, stable sentinels, atomic reviews);
rtk provides generic lossy compression for the long tail of commands devgeta
deliberately doesn't wrap.

## Install

```bash
dg install --only rtk        # or: dg install --only ai-tools
dg uninstall rtk             # or: dg uninstall ai-tools
```

- **macOS:** official Homebrew formula (`brew install rtk`).
- **Debian/Ubuntu:** official GitHub release binary for the current
  architecture, installed to `/usr/local/bin/rtk` (rtk is not in apt).

Verify: `rtk --version` and `rtk gain` (savings dashboard).

## The agent hook is opt-in

`rtk init -g` installs a PreToolUse hook that rewrites **every** agent Bash
call (`git status` → `rtk git status`). Devgeta installs the binary only and
never runs `rtk init` for you, because lossy compression must not silently
apply inside flows that need full payloads (e.g. a reviewer agent reading a
diff — task-design.md output principle 5).

If you want the hook, opt in explicitly through the AI coder it belongs to:

```bash
dg configure claude --force --only=rtk      # wire the hook into Claude Code
dg configure opencode --force --only=rtk    # install rtk's OpenCode plugin
```

Each part runs the matching `rtk init` under the hood (`init -g --auto-patch`
for Claude Code, `init -g --opencode` for OpenCode), so rtk owns the
integration files and their migrations. Restart the AI coder afterwards —
hooks load at startup.

The Claude opt-in is also recorded in `global_config.yaml`
(`integrations.rtk_claude_hook`), and `~/.claude/settings.json` is rendered
from a template that includes the hook entry whenever that flag is set — so
`dg configure claude --force` preserves the hook instead of wiping it.
`dg uninstall rtk` clears the flag again. OpenCode needs no such flag: rtk's
plugin is its own file (`plugins/rtk.ts`), which devgeta never touches.

Or use rtk directly:

```bash
rtk init -g          # Claude Code (restart it afterwards)
rtk init --show      # verify what was installed
rtk init -g --uninstall   # remove hook + RTK.md + settings entry
```

To verify it's active: `rtk init --show` reports each integration `[ok]`, and
in a fresh agent session a plain `git status` visibly executes as
`rtk git status`. `rtk gain` accumulates savings once traffic flows.

Without the hook, agents still benefit whenever they call `rtk` directly
(`rtk git diff`, `rtk go test`, `rtk read <file>`, …).

## Configuration

rtk works with no config. Its optional config file lives at
`~/.config/rtk/config.toml` (Linux) / `~/Library/Application Support/rtk/config.toml`
(macOS); devgeta does not ship one. Useful keys:

```toml
[hooks]
exclude_commands = ["curl"]   # commands the hook must never rewrite

[tee]
enabled = true                # save raw output on failure (default)
mode = "failures"
```

When a command fails, rtk saves the full unfiltered output under
`~/.local/share/rtk/tee/` so the agent can read it without re-running.

## Privacy

Telemetry is disabled by default and opt-in only (`rtk telemetry enable`);
aggregates are anonymous — no command arguments, paths, or repo contents.
`RTK_TELEMETRY_DISABLED=1` blocks it regardless of consent.

Security review notes (v0.43.0, 2026-07-23) are recorded in
[ADR-0004](../decisions/ADR-0004-ai-tools-install-category.md).
