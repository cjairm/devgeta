# ADR-0004 — New `ai-tools` install category, rtk as first app

**Date:** 2026-07-23
**Status:** ACCEPTED

## Context

ROADMAP.md plans an `ai-tools` category (Ollama, rtk, Gemini CLI, …) and
[task-design.md](../guides/task-design.md) names [rtk](https://github.com/rtk-ai/rtk)
a candidate installer: a generic token-compressing CLI proxy that complements
`dg task` by covering the long tail of commands we deliberately don't wrap.
The open question in ROADMAP.md was whether AI tools should be a separate
install category or fold into `terminal`.

Two sub-decisions were needed:

1. **Category:** new `ai-tools` category vs. registering rtk under `terminal`.
2. **Hook:** whether devgeta should run `rtk init -g`, which installs a hook
   that rewrites every agent Bash call (`git status` → `rtk git status`).

## Decision

1. **Create the `ai-tools` category** with its own coordinator
   (`internal/tooling/aitools/`), rtk as its first app. AI tools have a distinct
   audience (users running coding agents) and the category will grow (Ollama,
   Gemini CLI are already planned); burying them in `terminal` would make them
   impossible to `--only`/`--skip` as a group. Apps keep `Kind() = KindTerminal`
   (they are CLI tools); the coordinator, not the Kind, determines the install
   category — the same split alacritty already uses (KindTerminal, desktop
   coordinator).
2. **Install the binary only; the `rtk init -g` hook stays opt-in.** The hook
   applies lossy compression to _every_ Bash call, including a reviewer agent's
   diff payload, which violates task-design.md output principle 5 (lossy only
   with a receipt). Users who want the hook run `rtk init -g` themselves;
   `docs/apps/rtk.md` documents it.
3. **Install channels:** Homebrew formula on macOS; on Debian, the official
   GitHub release binary via the shared `InstallGitHubBinary` helper (devgeta
   downloads and installs the binary itself rather than piping the upstream
   install script into `sh`, per the §4 security rule against executing
   downloaded code unreviewed). Review of this change surfaced that the helper
   did not verify downloads; it now enforces SHA-256 verification against the
   release's `checksums.txt` for every caller (rtk, lazygit, lazydocker) and
   refuses to install when the checksum is missing or mismatched.

Security review (2026-07-23, v0.43.0): Apache-2.0, ~72k stars, active; CI-built
releases with SHA-256 checksums; formal security policy with automated
dependency/pattern scanning and a 2-reviewer rule on shell-execution code;
telemetry disabled by default, opt-in, anonymous aggregates.

## Consequences

- `dg install --only ai-tools`, `dg install --skip ai-tools`, and
  `dg uninstall ai-tools` work as a group; future AI tools slot in by adding
  one coordinator entry.
- One more coordinator and category to plumb through `cmd/install.go` and the
  registry; the per-app filter helpers were already coordinator-generic, so the
  cost is small.
- rtk items are tracked as `"package"` in global config, so `dg list` shows
  them under `terminal_tools` until list grows a category of its own — accepted
  for now to avoid a config-format change (§10 change discipline).
- Users don't get rtk's savings automatically: without the hook, agents only
  benefit when prompted to call `rtk` directly. That is the deliberate
  trade-off until the project stabilizes; revisit per ROADMAP.md.
- The explicit opt-in has a devgeta-native form on the AI coders themselves:
  `dg configure claude --force --only=rtk` and
  `dg configure opencode --force --only=rtk` (SelectiveConfigurer parts that
  delegate to `rtk init`). The Claude opt-in is persisted in
  `global_config.yaml` (`integrations.rtk_claude_hook`) and claude's
  `settings.json` is rendered from a template honoring it, so the hook
  survives `dg configure claude --force` instead of being wiped;
  `dg uninstall rtk` clears the flag. Install flows still never wire the hook.
