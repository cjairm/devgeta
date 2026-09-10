# Step 1 probe notes — OpenCode `permission.ask` and the rtk rewrite, 1.18.30

**Date:** 2026-09-10
**Method:** ADR-0007's — an isolated scratch project
(`~/.cache/devgeta/scratch/opencode-probe/`), a project-local plugin
(`.opencode/plugin/probe.js`) logging every `tool.execute.before` and
`permission.ask` invocation to a fixed file, `OPENCODE_CONFIG` pointed at a
throwaway policy file per run, `--print-logs --log-level DEBUG` read for the
`evaluated permission=...` line, always against a control. Model:
`github-copilot/claude-haiku-4.5`. rtk's real global OpenCode plugin
(`~/.config/opencode/plugins/rtk.ts`, rtk 0.43.0) was left installed and active
for every run except the one control that explicitly says otherwise.

## 1a — does `permission.ask` fire, and is the command recoverable?

Throwaway config: `"bash": {"gh api *": "ask", "*": "allow"}`. Prompt: run
`gh api rate_limit` and nothing else.

Probe plugin's log (`probe.log`), the only entry for the run:

```json
{"hook":"tool.execute.before","input":{"tool":"bash",...},"output":{"args":{"command":"rtk gh api rate_limit"}}}
```

No `permission.ask` entry. `--print-logs` for the same run:

```
message=evaluated permission=bash pattern="rtk gh api rate_limit" action.permission=bash action.pattern=* action.action=allow
```

**Two findings, and the second is the new one this probe exists to catch:**

1. **`permission.ask` still does not fire for bash**, on 1.18.30 as on 1.18.9
   (ADR-0007). Only `tool.execute.before` fired — for both the probe plugin
   and rtk's, per the invariant plugins are all invoked.
2. **The permission engine evaluated the _rewritten_ command, not the one the
   model wrote.** rtk's global plugin rewrote `gh api rate_limit` to
   `rtk gh api rate_limit` before the permission check ran, and the pattern
   `"gh api *"` was tested against the rewritten string — which does not start
   with `gh`, so it fell through to the `*` catch-all and allowed. This is the
   **opposite** of ADR-0038's Claude Code finding, where a hook's rewrite does
   **not** affect what the permission engine matches (matching happens against
   the model's original text). On OpenCode, rewrite happens **before** the
   match, and the match sees the rewritten text.

**Control**, isolating exactly this: same throwaway project, config
`"bash": {"rtk gh api *": "deny", "*": "allow"}` — a pattern written against
the _rewritten_ form. Same prompt, same rtk plugin active:

```
message=evaluated permission=bash pattern="rtk gh api rate_limit" action.permission=bash action.pattern="rtk gh api *" action.action=deny
```

Model's own summary: "I need permission to run that command... May I proceed?"
— i.e. the command was blocked. This isolates the mechanism precisely: the
**only** difference between the two runs is which string the `ask`/`deny`
pattern was written against, and the rewritten-pattern version is the one that
fires. Nothing else changed.

**Gate outcome (per ADR-0039 / this cycle's step 1 gate): 1a fails.**
`permission.ask` does not fire, so the read carve-out
(`gh-read-gate.sh` / `gh-read-gate.js`, steps 8–10) is **dropped from both
agents**. Steps 1–7 proceed.

## 1b — does a matching `ask` stall an unattended run, or resolve?

ADR-0007 (1.18.9, interactive terminal) measured an indefinite stall (~4 min,
killed by hand). Re-measured here under `opencode run` (headless CLI, no TTY)
on 1.18.30, using a family rtk never touches so the match isn't defeated by 1a's
mechanism: config `"bash": {"echo probe-1b*": "ask", "*": "allow"}`, prompt to
run `echo probe-1b-hello`.

```
message=evaluated permission=bash pattern="echo probe-1b-hello" action.permission=bash action.pattern="echo probe-1b*" action.action=ask
message=asking id=per_... permission=bash patterns="[\"echo probe-1b-hello\"]"
! permission requested: bash (echo probe-1b-hello); auto-rejecting
✗ echo probe-1b-hello failed
Error: The user rejected permission to use this specific tool call.
```

Run exited 0 in ~2s — **no stall**. `opencode run`'s headless mode
auto-rejects an `ask` immediately rather than hanging, which is a change from
ADR-0007's interactive-terminal measurement (or a mode difference ADR-0007
didn't test; that run wasn't `opencode run`). Practically: for unattended
work, an `ask` on OpenCode now degrades to an immediate deny, not a hang. This
does not change 1a's gate outcome — `permission.ask` (the plugin hook) still
never fired; the "asking"/"auto-rejecting" lines are the CLI's own built-in
prompt flow, a separate mechanism from the typed plugin hook this design would
have needed.

## 1c — how large is the rtk-override gap for the four planned write asks?

Confirmed first that rtk rewrites all four target commands (`rtk rewrite`,
no execution):

| Input                      | rtk rewrite output             |
| -------------------------- | ------------------------------ |
| `gh pr merge 12 --squash`  | `rtk gh pr merge 12 --squash`  |
| `gh pr close 5`            | `rtk gh pr close 5`            |
| `gh issue delete 5`        | `rtk gh issue delete 5`        |
| `gh release delete v1.0.0` | `rtk gh release delete v1.0.0` |

Then the live probe: throwaway config `"bash": {"gh pr merge *": "ask", "*":
"allow"}` — exactly the rule step 7 adds — prompt to run
`gh pr merge 12 --squash` in the scratch project (not a git repo, so a
successful match-and-allow still cannot merge anything real).

```
message=evaluated permission=bash pattern="rtk gh pr merge 12 --squash" action.permission=bash action.pattern=* action.action=allow
```

Ran **unprompted**, straight to `*` → allow — the `ask` rule never had a
chance, same mechanism as 1a. It failed only at the `gh`/git layer
(`fatal: not a git repository`), not from any permission gate.

**This settles §4's "unknown, not zero."** The rtk-override gap on OpenCode is
not smaller or different in kind from Claude Code's (ADR-0038) — it is the
same size, for anyone with rtk's OpenCode plugin enabled: every `gh`/`git`
family rtk rewrites (`gh pr merge`, `gh pr create`, `gh pr edit`, `gh pr
close`, `gh issue delete`, `gh release delete`, `gh run rerun`, `git commit`,
`git push origin main`, and `gh api` itself) bypasses **any** ask/deny rule
written against the un-prefixed form, including the four this cycle adds. The
mechanism differs (text rewrite defeating a string match, not an explicit
decision field), but the practical effect — and the fact that it is
unclosable from devgeta's side, since rtk owns its own plugin file on OpenCode
— is identical to the Claude Code finding.

## Step 6/11 — the rtk-shim does not actually restore the policy on Claude Code

Step 6's manual probe (deny + shim vs. deny + raw `rtk hook claude`) passed and
was reported as confirming the fix. Step 11's live check of the new write
gates (`gh pr merge 1 --squash`) then failed to prompt — against the **real**
deployed `~/.claude/settings.json`, not a throwaway one — which is what
surfaced this. Root-caused with four isolated `claude -p --setting-sources ''`
A/B runs, each against a scratch (non-git) directory so a false allow could
never do anything real:

| `permissions.allow` in the throwaway settings | Hook rewriting the command | Result                                                         |
| --------------------------------------------- | -------------------------- | -------------------------------------------------------------- |
| _(none)_                                      | yes (`rtk-shim.sh`)        | `ask`/`deny` fire correctly, against the original command      |
| `["Bash(*)"]`                                 | **no** (no hook at all)    | `deny` fires correctly                                         |
| `["Bash(*)"]`                                 | **yes** (`rtk-shim.sh`)    | **`deny`/`ask` silently bypassed** — the command actually runs |

The third row reproduces with a real `deny` (`Bash(git status*)`, the exact
step-6 rule) and, separately, with a real `ask` (`Bash(gh pr merge *)`, one of
this cycle's new write gates): both are bypassed the same way. Full runs saved
under `~/.cache/devgeta/scratch/opencode-probe/` as
`deny-with-allow-probe.jsonl` (bypassed) against
`deny-with-allow-nohook-probe.jsonl` (correctly denied, only the hook removed)
and `twoblock-allow-probe.jsonl` (bypassed) against `twoblock-probe.jsonl`
(correctly asked, only the `"allow": ["Bash(*)"]` line removed) — each pair
differs in exactly one line.

**Root cause: `permissions.allow: ["Bash(*)"]` — which is devgeta's own
unconditional baseline entry, present in every shipped Claude Code config,
with or without the rtk integration — appears to be matched against the
_rewritten_ command whenever any `PreToolUse` hook supplies `updatedInput`,
and an allow match there wins over an `ask`/`deny` rule that only matches the
_original_ text.** This holds however the hook mutates the command, with no
`permissionDecision` involved at all — it reproduced with `rtk-shim.sh`, whose
entire purpose is to strip that field. It is very likely not specific to
`rtk-shim.sh` either: `output-budget.sh` (`configs/claude/output-budget.sh`)
also rewrites `tool_input.command` via `updatedInput` for an unrelated reason
(capping output), so any command it rewrites is a candidate for the same
bypass — not measured here, flagged as a follow-up.

**This means step 6's own result was a false positive.** That probe used a
throwaway settings file carrying `deny` alone, no `allow` line — a condition
that does not exist in the real shipped config, where `"allow": ["Bash(*)"]`
is always present. ADR-0038's original probe table has the same gap: every
run in it used a throwaway settings file with no broad `allow`, so
"permissions are evaluated against the command the model wrote" was true in
that narrower condition and is not true in the shipped one.

**Consequence for this cycle's objective:** the four new write-`ask` gates
(`gh pr merge *`, `gh release delete *`, `gh pr close *`, `gh issue delete *`)
do **not** actually prompt for a user with the rtk Claude Code integration
enabled, and neither does the pre-existing `gh api *` ask — the rtk-shim
removes the _specific_ bypass ADR-0038 documented (rtk's own
`permissionDecision`) but a second, independent bypass with an identical
practical effect exists underneath it, rooted in Claude Code's own handling
of `Bash(*)` plus a hook rewrite, not in anything devgeta's shim controls.
`rtk-shim.sh` and the four write-`ask` entries are still correct and still
narrow what a _non-rtk_ user, or a command _no hook rewrites_, would see
prompted — they are not wrong to ship — but the objective as stated ("gh pr
merge, gh release delete, gh pr close, gh issue delete reach a human gate
that actually fires there whether or not the rtk integration is enabled") is
**not met** for a user with the rtk integration on.

## Summary for the cycle

| Question                                                    | Answer                                                                                                                                         |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| Does `permission.ask` fire for bash on 1.18.30?             | No (unchanged from ADR-0007's 1.18.9 finding)                                                                                                  |
| Is the command recoverable from its payload?                | N/A — hook never fires                                                                                                                         |
| Does a matching `ask` stall an unattended run?              | No — auto-rejects immediately (a change from ADR-0007's interactive measurement)                                                               |
| Does rtk's OpenCode plugin defeat command-pattern rules?    | Yes — via pre-match rewrite, not a decision field                                                                                              |
| Gate decision                                               | Drop the read carve-out from **both** agents; steps 1–7 proceed                                                                                |
| Size of the OpenCode rtk-override gap (§4)                  | Same as Claude Code's — every rtk-rewritten `gh`/`git` family, including the four new write asks                                               |
| Does the rtk-shim actually restore ask/deny on Claude Code? | **No** — `Bash(*)` allow + any command-rewriting hook bypasses ask/deny independent of rtk's own decision field; not fixable by devgeta's shim |
