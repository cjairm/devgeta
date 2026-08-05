# ADR-0013 — Normalize the worktree gitfile instead of warning about it

**Date:** 2026-08-05
**Status:** ACCEPTED

## Context

`dg wt create` warned on some repos that their hooks were incompatible with git
worktrees, naming `affiance-hook` and telling the user to commit with
`--no-verify`. The warning was accurate, but it was the wrong response.

The underlying failure is real and was verified against the installed module,
not inferred. Affiance (`github.com/l8on/affiance`) parses a worktree's `.git`
file in `lib/gitRepo.js` with:

```js
let gitDirRegex = /^gitdir: (.*)$/g;
```

JavaScript's `$` without the `m` flag matches only at end-of-input — it does
**not** match before a trailing newline the way Perl and Python do — and `.`
never matches a newline. `git worktree add` writes `gitdir: <path>\n`, so the
pattern can never match and `gitDir()` throws
`InvalidGitRepo("no .git directory found")`. Calling the installed 1.8.0's real
`gitDir()` against both forms:

```
WITH trailing newline (what git writes) => THREW: no .git directory found
WITHOUT trailing newline                => OK: /…/collections/.git/worktrees/probe
```

Two facts made the old warning worse than it looked:

- **It is not a merge-only path.** The pre-commit context's
  `setupEnvironment()` calls `storeMergeState()` → `mergeState()` → `gitDir()`
  unconditionally, so _every_ commit inside the worktree failed. Creating the
  worktree always succeeded, so the warning fired at the wrong moment for the
  wrong operation.
- **There is no upgrade out of it.** 1.8.0 is the latest published version, so
  "update your dependency" was not available.

That left three candidate responses.

**Keep warning and recommend `--no-verify`.** This is what shipped, and it fails
CLAUDE.md §4 twice: it treats a symptom, and it asks the user to remember a
convention forever. It is also actively harmful advice — `--no-verify` disables
_all_ hooks, so every commit silently skips the repo's lint and tests to dodge a
newline.

**Patch Affiance in each consuming repo** (`patch-package` or an npm override).
This is the most correct fix in the abstract: it repairs the actual defect where
the dependency is owned. It was rejected as devgeta's answer because it does not
scale — it fixes the two repos that happen to hurt today and nothing else. The
next repo with Affiance, and any other tool that parses the gitfile with an
end-of-line-anchored regex, reproduces the bug from scratch. Nothing stops us
also doing this upstream; it is just not what devgeta can rely on.

**Write the gitfile in the form strict parsers can read.** Chosen.

## Decision

`CreateWorktreeIn` normalizes the worktree's `.git` file after creation, via
`Git.NormalizeWorktreeGitfile`, writing `gitdir: <path>` with no trailing
newline.

This is a compatibility fix, not an Affiance workaround, and that distinction is
what makes it general enough to ship (CLAUDE.md §3.8): every consumer of the
gitfile benefits, and devgeta needs no knowledge of which hook framework a repo
uses. Affiance is the case that exposed it, not the thing being special-cased.

Both forms are valid and git treats them identically — its own parser
(`read_gitfile_gently`) trims trailing whitespace. Verified against a real
gitfile checkout before adopting the trim: after stripping the newline,
`rev-parse --absolute-git-dir`, `status`, and `commit` all behave exactly as
before. So the no-newline form costs nothing and is strictly more parseable.

Three supporting decisions:

- **Normalization happens at one choke point, not per `worktree add`.**
  `createWorktreeIn` has five separate success paths; normalizing in each is how
  one of them silently loses the invariant on the next edit. `CreateWorktreeIn`
  wraps it and normalizes once.
- **`devgeta task worktree-start --base` was rerouted through the wrapper.** It
  assembled its own `worktree add`, making it the one creation path that produced
  an un-normalized gitfile. It now calls `CreateWorktreeAtBaseIn`, so there is no
  creation path that bypasses the invariant (CLAUDE.md §6, route external tools
  through their wrappers).
- **`Repair` and `RepairInRepo` re-apply it.** This is the part that keeps the
  fix from regressing silently, which was the strongest objection to doing it at
  all: `git worktree repair` and `git worktree move` rewrite `.git` and restore
  git's trailing newline. Repair is what a user runs when a worktree misbehaves,
  so it heals the gitfile before touching tmux.

`CheckHookCompatibility` no longer reports Affiance. A warning about a problem
devgeta has already prevented is a nag the user cannot act on. It still warns
about hooks that genuinely require `.git` to be a directory (`[ -d .git ]`,
`test -d .git`), because normalization cannot fix those — `.git` in a linked
worktree is a file no matter how it is written.

## Consequences

**Easier.** Commits work in worktrees of Affiance repos with no per-commit
ceremony and no disabled hooks. Any other strict gitfile parser is fixed for
free, in every repo, without devgeta learning about it. Dropping the Affiance
branch also removed an early `return` that had been masking real
directory-requiring hooks whenever Affiance happened to be installed — those are
now reported.

**Harder / accepted trade-offs.**

- devgeta's worktrees differ by one byte from what bare `git worktree add`
  produces. Both are valid, but anything asserting byte-equality with git's
  output would notice.
- The invariant now has three call sites (create, create-at-base, the two repair
  paths) and must be preserved if a fourth creation path is ever added. It is
  documented on `NormalizeWorktreeGitfile` and enforced by tests, but it is not
  structurally impossible to bypass — a future `worktree add` that skips the
  wrapper would skip it. The wrapper routing rule in CLAUDE.md §6 is the
  backstop.
- Raw `git worktree repair`/`move` still reintroduces the newline until the next
  `dg wt repair`. This is a real gap, consciously accepted: devgeta cannot
  intercept git commands the user runs directly.
- This fixes the one verified failure mode. If Affiance carries further
  worktree-specific bugs beyond `gitDir()`, they will surface separately; the
  normalization does not claim to make Affiance worktree-clean in general.
