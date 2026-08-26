# ADR-0006 — Scope and code-sharing for two new PreToolUse guardrail hooks

**Date:** 2026-07-29
**Status:** ACCEPTED

## Context

Two gaps existed in devgeta's hook-based guardrails (`configs/claude/`,
`configs/opencode/plugin/`):

1. Nothing stopped an agent from staging and committing a secret — the deny
   list blocks _reading_ files like `.env` or `~/.aws/**`, but not committing
   one that's already staged.
2. CLAUDE.md's "Lint issues" section calls `//nolint` (and by the same logic,
   `# noqa`, `@ts-ignore`, `eslint-disable`, …) "never acceptable," but nothing
   enforced it — it relied on the agent remembering a documented convention,
   which CLAUDE.md §4 itself says to avoid ("prefer making a mistake class
   structurally impossible... over documenting a convention people must
   remember").

Two design questions needed answering before implementation, both with
lasting impact on every repo these hooks run in (they deploy to the user's
**global** `~/.claude/` and `~/.config/opencode/`, so they fire in every
project unless scoped otherwise — see `docs/apps/claude.md`'s existing
global-vs-devgeta-repo split for task-redirect.sh).

## Decision

### 1. Scope: secret-commit guard is global; suppression-comment guard is devgeta-repo-only

Same test task-redirect.sh's existing rules already use: is this a
better/compressed form of a universal operation (global), or does it encode
_devgeta's own_ policy that would be wrong to impose on someone else's
project (devgeta-repo-only, gated by the existing `is_devgeta_repo` /
`isDevgetaRepo` go.mod walk)?

- **Secret-commit guard → global.** "Never commit a secret" is universal
  engineering practice with no devgeta-specific opinion in it — nobody wants
  secrets committed, in any repo.
- **Suppression-comment guard → devgeta-repo-only.** Banning `//nolint` et al.
  outright is a specific, non-universal stance — many legitimate codebases use
  these directives deliberately, and CLAUDE.md itself frames the ban as this
  project's own non-negotiable rule, not a general truth. Firing this
  everywhere would impose devgeta's own conviction on unrelated repos, exactly
  the failure mode the existing release-rule gating was built to avoid.

**Amended 2026-08-26 — "which repo" is the target file's, not the session's.**
As first written, this section said to reuse "the existing `is_devgeta_repo` /
`isDevgetaRepo` go.mod walk," and both suppression guards did so literally:
they walked up from the **session** directory (`.cwd` / `ctx.directory`). That
is the only thing `task-redirect.sh` can do — a Bash command names no file —
but Edit and Write both carry `file_path`/`filePath`, so the exact target was
available and was being discarded for a proxy. Live verification of
[cycle 2026-07-29-hook-guardrails](../plans/cycles/2026-07-29-hook-guardrails.md)
found it wrong in both directions:

- **Under-enforcement, the serious one.** A session rooted anywhere outside
  devgeta could write a suppression **into** a devgeta source file and the
  guard never fired — the ban this ADR calls structurally impossible to
  violate was bypassable by where the agent happened to be started.
- **Over-reach.** A devgeta-rooted session writing to a file in an unrelated
  repo had devgeta's stance imposed on it — precisely what §1 above forbids.

So both guards now resolve the scope directory from the target file (relative
paths against the session directory; an absent path still falls back to it)
and walk up from there. The go.mod walk itself, its fail-toward-false posture,
and the sharing arrangement in §3 are unchanged — only the directory handed
to it. `secret-guard.sh` is unaffected: it is global, and a commit has no
single target file. `task-redirect.sh` is likewise unchanged, for the reason
above.

This is why the automated suites did not catch it: every scope test passed a
**relative** `"main.go"`, so the session and the target were always the same
repo and could never disagree. Both suites now pin the divergent case in both
directions.

### 2. Mechanism: PreToolUse deny, not PostToolUse advisory

Both hooks deny _before_ the action happens (exit 2 / throw), rather than
warning after the fact the way `format.sh`'s lint feedback does. CLAUDE.md §4:
"prefer making a mistake class structurally impossible... over documenting a
convention" — a secret already committed or a suppression comment already
written to disk is a fact that happened; only a Pre-hook prevents it from
happening.

### 3. Code sharing: bash gets a real shared lib; OpenCode does NOT

Both new hooks need logic task-redirect.sh/js already implement
(`split_command_segments`/`splitCommandSegments`, `is_devgeta_repo`/
`isDevgetaRepo`) — CLAUDE.md's DRY rule requires extracting shared logic
rather than duplicating it, in the same PR that introduces the second use.

- **Bash:** extracted into `configs/claude/lib/segments.sh` and
  `configs/claude/lib/devgeta-repo.sh` — plain function/constant definitions,
  sourced by every script that needs them (`task-redirect.sh`,
  `secret-guard.sh`, `suppression-guard.sh`). Safe: Claude Code only executes
  the exact script paths registered in `settings.json`'s hooks; a file sitting
  in `~/.claude/lib/` is never auto-invoked.
- **OpenCode: deliberately NOT a parallel `lib.js` file in `plugin/`.**
  Read OpenCode's actual loader
  (`packages/opencode/src/plugin/index.ts`, `getLegacyPlugins`): it imports
  **every** `.js` file under the plugin directory and invokes **every
  exported value** as if it were a plugin factory, throwing
  `TypeError: Plugin export is not a function` for any export that isn't
  callable. A dedicated helpers-only file would either crash the load (a
  non-function export) or get silently mis-invoked as a bogus plugin (a
  helper function called with an unrelated `ctx` argument). The existing
  `isDevgetaRepo` export in `task-redirect.js` already relies on this being
  harmless _by accident_ (it degrades to returning `false` on a
  non-string argument) — that's tolerable for one already-shipped export, not
  a pattern to build a whole shared module on.

  Instead, `secret-guard.js` and `suppression-guard.js` **import the named
  exports directly from `task-redirect.js`**
  (`import { splitCommandSegments } from "./task-redirect.js"`). This adds
  nothing to either file's own _export_ list — only `task-redirect.js`'s
  existing exports (`TaskRedirect`, `isDevgetaRepo`, now also
  `splitCommandSegments`) are ever loader-visible, and all three are already
  proven safe to call with an unexpected argument. `secret-guard.js` and
  `suppression-guard.js` each export exactly one thing: their own real plugin
  factory.

## Consequences

- Adding a fourth devgeta-repo-only rule later reuses `devgeta_is_repo` /
  `isDevgetaRepo` with zero new duplication on the bash side, and a plain
  import on the OpenCode side — as long as it imports from an existing
  plugin file rather than introducing a new helpers-only one.
- A future contributor must NOT "clean up" the OpenCode side by factoring
  `splitCommandSegments`/`isDevgetaRepo` into a new `configs/opencode/plugin/lib.js`
  — that would reintroduce the loader risk this ADR documents. If OpenCode's
  plugin loader is ever confirmed to support non-plugin helper modules (e.g.
  scoped to a subdirectory it doesn't scan), this decision should be revisited.
- The two guardrails' pattern lists (sensitive filenames/content signatures,
  suppression-comment needles) are each kept in exactly two places — one bash,
  one JS — and must be kept in sync by hand, the same maintenance burden
  `task-redirect.sh`/`.js` already carries and documents.
