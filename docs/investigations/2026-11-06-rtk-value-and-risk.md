# Investigation — Is rtk worth keeping?

**Date:** 2026-11-06
**Status:** Open
**Feeds:** none yet. If acted on, an ADR that removes rtk and amends ADR-0004 and ADR-0038.
**Branch:** `agentsv2`. Work that acts on this investigation, together with
[2026-11-06-skills-and-agents-reset.md](2026-11-06-skills-and-agents-reset.md),
happens there. This file was committed to `main` before the branch existed.

## Question

rtk (`rtk-ai/rtk`) is devgeta's only `ai-tools` app (ADR-0004). It rewrites an
agent's shell commands (`git status` → `rtk git status`) and returns a
shortened output, to save tokens. In day-to-day use it doesn't seem to save
much, and rtk 1.0 added an LLM proxy, which is the kind of tool ADR-0031
rejected. Does rtk pay for itself, what does it cost, and should devgeta keep
it or rely on its own tools instead?

## Scope

- **In:** savings measured on the maintainer's machine, what rtk costs in
  practice, rtk 1.0's proxy and license change, and how devgeta currently
  wires rtk in.
- **Out:** designing a replacement. Devgeta's own output-budget hook already
  exists and is only compared here.

## Method

- Sources: `rtk gain`, `rtk gain --recalls`, `rtk gain -F`, `rtk gain -m`,
  `rtk cc-economics`, `rtk init --show`, `rtk telemetry status`,
  `rtk llm-proxy status`, `brew info rtk`, and test payloads piped to
  `rtk hook claude` (it only prints JSON and runs nothing). The repo:
  ADR-0004, ADR-0031, ADR-0038, `docs/apps/rtk.md`,
  `internal/apps/rtk/rtk.go`, `configs/claude/settings.json.tmpl`,
  `configs/claude/rtk-shim.sh`. The live `~/.claude/settings.json`, read only.
  Upstream: rtk's README (`develop` branch), PR #4551 (the 1.0 release), and
  the Wavect 1.0 review (2026-10-15).
- When: 2026-11-06. Local rtk is 1.0.1; Homebrew offers 1.1.0.
- Criteria: worth keeping if it saves a noticeable share of the real bill
  (more than about 5%), doesn't make the agent redo work, and doesn't weaken
  devgeta's permission rules or add a component that sees provider traffic.
- Not done: no A/B run with and without rtk. Nothing was changed on the
  machine. `rtk cc-economics` fetched `ccusage` through `npx` to read local
  Claude logs. That was a download that should have been asked about first.
  It only reads local files.

## Summary

| Item                      | Finding                                                                       |
| ------------------------- | ----------------------------------------------------------------------------- |
| Savings on the real bill  | 0.3% by rtk's own estimate; under about 2% even if cache re-reads are counted |
| Cost in practice          | Hidden output forces reruns; rtk's recall stats can't see them                |
| Independent benchmarks    | Mixed: from 7.6% _more_ expensive to 4.8% cheaper (not significant)           |
| Live hook on this machine | Raw `rtk hook claude`, not devgeta's shim: ADR-0038 is not in force           |
| rtk 1.0 proxy             | Opt-in, separate BUSL-1.1 binary; not installed here; known credential issue  |
| License                   | Hook and filters are Apache-2.0; engine BUSL-1.1 in the full edition          |
| Devgeta's own alternative | Output-budget hook already exists, follows ADR-0031, off by default           |
| **Recommendation**        | Remove rtk; decide separately whether to turn output-budget on by default     |

---

## Findings

### 1. Savings are small next to the real bill

- **Evidence:**
  - `rtk gain`: 3,075 commands since 2026-10-09, 268.8K tokens saved (25.3%
    of the output it filtered). 38 oversized outputs are excluded from that
    rate.
  - `rtk gain -m`: 138.2K saved in October, 130.6K in November.
  - `rtk cc-economics` puts that next to Claude usage from `ccusage`: $252.68
    spent, with 3.0M input, 3.2M output, 16.4M cache-write and 557.4M
    cache-read tokens. Estimated savings: **$0.71 (0.3%)**.
- **Verdict:** rtk prices a saved token once, at the input rate. Each token
  saved early in a session is also read back from cache on every later turn,
  so the real figure is higher. Say it's re-read on 50 later turns: one
  cache write (1.25×) plus 50 reads (0.1× each) is about 6× the input rate,
  or roughly $4.50 (about 1.8%). That is still well under the 5% bar. rtk's
  README says the same: it reduces "the bash output your agent reads ... not
  the same as cutting your bill by the same amount".
- **Independent numbers** (from the Wavect review; none of them measured
  1.0.1):
  - JetBrains, rtk 0.43.0, 80 low-effort task pairs: median cost per task
    **7.6% higher** with rtk (p=0.004).
  - rtk team, 0.45.0: 4.8% lower pooled cost, not significant (p=0.305).
  - Quesma, 0.45.0, 1,740 Terminal-Bench attempts: cost per passed attempt
    3% lower for one model, 7% higher for another.

### 2. Hidden output makes the agent redo work, and that isn't counted

- **Evidence:**
  - `rtk gain --recalls` shows 0 recalls across 300 elisions. It only counts
    an agent running `rtk recall`.
  - In this session the agent hit `... (15 more)` and `[+N hidden: rtk recall
…]` markers on directory listings (the ADR list, `configs/shared/commands`,
    the OpenCode plugin folder). Each time it re-ran the command with
    `rtk bypass` or a different command. That happened five or six times in
    one session, and none of them show up as a recall.
  - `rtk gain -F`: 117 parse failures (99.1% recovered), mostly
    `python3 -` and `head`.
- **Verdict:** Every rerun costs an extra turn plus the full output, which
  cancels the saving on that command. That is the effect behind the
  JetBrains result. rtk's own dashboard can't see it, so its savings figure
  is optimistic.

### 3. The live hook bypasses devgeta's permission rules (ADR-0038)

- **Evidence:**
  - ADR-0038 says rtk must run through `configs/claude/rtk-shim.sh`, which
    removes rtk's `permissionDecision`. A hook's `allow` overrides the `ask`
    and `deny` lists. The template renders the shim when
    `integrations.rtk_claude_hook` is set
    (`configs/claude/settings.json.tmpl:172-178`).
  - The live `~/.claude/settings.json:186-194` has `"command": "rtk hook
claude"` and no shim entry, even though `global_config.yaml` has
    `rtk_claude_hook: true`. `rtk init --show` reports it as rtk's own hook.
  - Piping test payloads to `rtk hook claude`:
    - `git status` and `git push origin main` both come back as
      `"permissionDecision":"allow"`, with the command rewritten to
      `rtk git …`.
    - `git push --force origin main` (in devgeta's `ask` list) comes back
      rewritten but without `allow`. rtk 1.0 appears to respect `ask`
      patterns itself.
  - `internal/apps/rtk/rtk.go:201` runs `rtk init -g --auto-patch` during
    configure.
- **Verdict:** On this machine devgeta's rule ("rtk never decides
  permissions") is not in force. rtk is making allow decisions for every
  command it rewrites.
  - **Likely cause (not confirmed):** rtk 1.0's `init -g --auto-patch`
    replaced the shim entry with its own hook.
  - **Unverified risk:** whether Claude Code checks `ask`/`deny` patterns
    against the rewritten command (`rtk git push --force …`) or the original
    one. If it checks the rewritten one, `Bash(git push --force*)` no longer
    matches.

### 4. rtk 1.0's proxy and license

- **Evidence:**
  - README and PR #4551: `rtk-proxy` is a separate daemon that sits between
    the agent and the provider, "compresses payloads" on Anthropic's
    `/v1/messages`, and "borrow[s] the credential your agent already sends".
  - It is opt-in. `rtk init -g` "offers to set it up", and hooks and proxy
    work independently.
  - PR #4551's approver lists known issues shipped as-is: "Proxy credential
    lending with no caller/Host check, `relay_borrowing` default `true`".
    Also, `mode="answer"` runs `rg`/`curl` in the hook "without `--`" and
    reads `file_path` "with no permission gate".
  - Editions: `rtk-oss` (Apache-2.0, hook and filters only), `rtk` (adds a
    BUSL-1.1 compression engine), and `rtk-proxy` (BUSL-1.1, never bundled).
    `brew info rtk` reports the homebrew-core formula as Apache-2.0. It
    wasn't checked whether that build includes the engine.
  - On this machine `rtk llm-proxy status` reports "rtk-proxy not found", so
    the daemon isn't installed. `rtk telemetry status` reports consent no,
    enabled no.
  - rtk keeps full command outputs in
    `~/Library/Application Support/rtk/recall.db` (540 KB) and history for
    90 days (`config.toml`, `history_days = 90`).
- **Verdict:** The proxy is exactly the send-time rewriting ADR-0031 ruled
  out. It isn't active today, but devgeta runs `rtk init -g --auto-patch`,
  and whether `--auto-patch` accepts the proxy offer on a machine where the
  daemon _is_ present wasn't verified. The recall store also means a command
  that prints a secret keeps it on disk. That's outside what devgeta's
  `secret-guard` covers.

### 5. Devgeta already has its own write-time tools

- **Evidence:**
  - The output-budget hook (`configs/claude/output-budget.sh`,
    `configs/opencode/plugin/output-budget.js`, runner
    `configs/devgeta/output-budget-run.sh`) is marked shipped in
    `docs/guides/output-budget-runner.md`. It is off by default
    (`docs/guides/token-efficiency.md` §3).
  - It caps output at the moment the command runs, keeps the command's exit
    status, and never decides permissions, which is the design ADR-0031 asks
    for.
  - `dg task` covers the structured flows (task-design.md).
  - `docs/guides/token-efficiency.md` §2 says the biggest lever is trimming
    what loads every session, which the skills reset
    ([2026-11-06-skills-and-agents-reset.md](2026-11-06-skills-and-agents-reset.md))
    is doing.
- **Verdict:** Removing rtk doesn't leave a gap that needs new code. What
  output-budget lacks are rtk's per-tool formatters, which are the part that
  hides lines. Its caps are coarser but predictable: the end of the output
  is marked, not lines dropped from the middle.

---

## Recommendation

1. **Remove rtk from devgeta.** The measured saving is a few percent at best,
   it costs reruns that its own stats can't see, its integration has already
   slipped out of devgeta's permission design, and its direction (a BUSL
   proxy that borrows credentials) is against ADR-0031. Removing it means
   deleting:
   - the `rtk` app (`internal/apps/rtk/`) and its registry and `contract.go`
     entries, which empties the `ai-tools` category
   - `configs/claude/rtk-shim.sh` and the `RtkClaudeHook` template branch
   - `--only=rtk` in claude and opencode configure
   - the `integrations.rtk_claude_hook` flag
   - the rtk notes in `task-design.md`, `output-budget-runner.md` §9,
     `token-efficiency.md`, and CLAUDE.md §8
   - `docs/apps/rtk.md`
2. **Decide separately** whether output-budget becomes the default. It is
   devgeta's own, follows ADR-0031, and is already tested. Turning it on is
   a behavior change for every user, so it needs its own ADR.
3. **Existing users** need a migration note: devgeta never ran `rtk init` for
   them unless they opted in, and `rtk init -g --uninstall` removes the hook,
   `RTK.md` and the `@RTK.md` line. That would go in `docs/migrations/`.

## Verification still needed

1. Whether Claude Code matches `ask`/`deny` against a hook's rewritten
   command or the original one (sandboxed probe, as in ADR-0038).
2. Whether `rtk init -g --auto-patch` turns on the proxy when `rtk-proxy` is
   on PATH (sandboxed `HOME`).
3. Whether rtk 1.0's `init` removes the devgeta shim entry (sandboxed
   `HOME`). This confirms the cause in finding 3.
4. Optional: one week with output-budget on and rtk's hook off, compared with
   `ccusage`, to put a real number on the difference.

## Decisions this will need

- An ADR removing rtk, which supersedes ADR-0004's rtk entry and ADR-0038.
- An ADR on turning output-budget on by default, if wanted.
- The maintainer's own machine: the raw `rtk hook claude` entry is live now.
  Removing it (`rtk init -g --uninstall`) changes `~/.claude/` and needs the
  maintainer's go-ahead.
