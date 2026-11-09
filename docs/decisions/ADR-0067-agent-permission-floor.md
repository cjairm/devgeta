# ADR-0067 — Agents get a small permission floor, not a deny list

**Date:** 2026-11-07
**Status:** ACCEPTED (2026-11-08, step 0 of the
[rollback cycle](../plans/cycles/2026-11-07-agent-customizations-rollback.md),
which implements it)

## Context

Devgeta's permission lists started small in May (`784d03a` for Claude Code,
`4b2fcf0` for OpenCode). After the July overhaul (`125b9d9`) and later
additions, Claude Code ships 12 `ask` and 104 `deny` rules, mirrored in both
coders and held in step by `internal/apps/opencode/permissions_test.go`.
Next to them run three guard hooks: `secret-guard`, `agent-config-guard` and
`suppression-guard` (ADR-0006, ADR-0014). Full history and reasoning:
[the rollback investigation, finding 5](../investigations/2026-11-06-agent-customizations-rollback.md).

The problems:

- **Bash pattern denies are weak.** `Bash(curl *)` is skipped by
  `command curl` or `bash -c '…'`, yet it blocks ordinary work and costs a
  turn each time.
- **About 30 denies duplicate a guard.** They list agent config paths one by
  one, though ADR-0014 decided agent config is protected by
  `agent-config-guard`.
- **Two guards don't fit what devgeta ships.** `suppression-guard` enforces
  devgeta's own lint rule on every user's machine (gated to this repo), and
  it matches text, so it blocks Markdown that only names a suppression
  directive. `secret-guard` blocks `git add … && git commit` in one call and
  duplicates real secret scanners. Neither runs in Pi.
- **Read denies are different.** A secret file has no reason to enter an
  agent's context, and the coder's own read tool enforces the rule.

## Decision

Each coder devgeta configures with permissions (today Claude Code and
OpenCode) gets the same floor:

- **Read deny:** `**/.env`, `**/.env.*`, `*.env`, `**/envs/*.js`,
  `./secrets/**`, `*.pem`, `*.key`, `~/.ssh/**`, `**/.ssh/**`, `**/id_rsa`,
  `**/id_ed25519`, `**/id_ecdsa`; credential files (`.aws`, `.kube`,
  `gcloud`, `.azure`, `.npmrc`, `.pypirc`, `.netrc`, `.git-credentials`,
  `.docker/config.json`, `.pgpass`); shell history (`.zsh_history`,
  `.bash_history`), which often holds pasted tokens. Home paths ship in both
  spellings, as `TestHomeAnchoredDeniesHaveGlobstarTwins` requires.
- **Bash `ask`:** `git push --force*`, `git push -f*`, `sudo *`,
  `gh repo delete *`, `gh release delete *`. The other current `ask` rules
  (`ssh`, `scp`, `rsync`, `gh api`, `gh pr merge/close`, `gh issue delete`,
  `terraform apply`, `kubectl apply/delete`) are dropped.
- **Bash deny:** none.
- **Edit deny:** `.git/**` only. Shell rc files are no longer denied, since
  legitimate setup tasks edit them. The enumerated agent config paths are
  dropped; `agent-config-guard` protects agent config.
- **Guards:** keep `agent-config-guard`. Remove `secret-guard` and
  `suppression-guard`, and the `configs/claude/lib/` scripts only they use.
  Rebuild notes are in the investigation.
- **Unchanged:** `defaultMode: acceptEdits`,
  `ENABLE_CLAUDEAI_MCP_SERVERS=false`, the scratch directory grant.

New permission rules or guards come back one at a time, each with a reason
it is needed, and for every coder devgeta configures or for none.

## Consequences

- Fewer denied commands and prompts. Agents can run `curl`, interpreters
  with `-c`/`-e`, `rm -rf` and cloud CLIs without asking.
- Secret protection rests on read denies only. An agent can still stage and
  commit a secret file by name if `.gitignore` doesn't cover it; nothing
  checks commits until a secret check is rebuilt, preferably at the git
  level so it covers people and every coder.
- Devgeta's lint-suppression rule is no longer enforced on agents. It stays
  in CLAUDE.md as project law until it is rebuilt in this repo's own config.
- CLAUDE.md §9's "add and commit as two separate commands" exists because of
  `secret-guard` and can go with it.
- Pi still gets no permissions or guards. Widening parity to every coder is
  follow-up work (skills-reset investigation, Shared work).
- Supersedes ADR-0006 (both guards it governs are removed) and ADR-0039
  (the `gh api` `ask` is dropped). Amends ADR-0014: the guard stays, the
  path denies around it go.
