# ADR-0015 — Agent scratch files get a devgeta-owned directory, not `/tmp`

**Date:** 2026-08-05
**Status:** PROPOSED

## Context

Several commands devgeta ships instruct the agent to write a scratch file to
`/tmp` and pass it to a CLI:

| File                                   | Path                                   |
| -------------------------------------- | -------------------------------------- |
| `configs/shared/commands/review-pr.md` | `/tmp/review.md`, `/tmp/comments.json` |
| `configs/shared/commands/create-pr.md` | `/tmp/pr-body.md`                      |

The pattern is right — `gh pr review --body-file` and
`devgeta task create-pr --body-file` exist because passing Markdown through a
shell argument mangles backticks and apostrophes. The destination is the problem.

Each is a `Write` tool call outside the working directory, and both agents stop
for approval on that. Claude Code's `acceptEdits` auto-accepts only

> file edits and common filesystem commands […] for paths in the working
> directory or `additionalDirectories`.

OpenCode has the same boundary under a different name: `permission.external_directory`
is a `PermissionRuleConfig` defaulting to `ask`, which is the
`Access external directory` prompt users see.

So devgeta ships commands guaranteed to stop mid-run, on every machine, for
every user, under both agents. Devgeta's own instruction causes it.

### Why not just allow `/tmp`

One `additionalDirectories` line would silence it. Two reasons not to:

- `/tmp` is world-writable and shared, and the grant covers **read** as well as
  write — every other process's and user's temp files. It is the same exposure
  class the read-deny list already guards for `~/.aws`, `~/.ssh`, `~/.netrc`.
- It is not the cause. Nothing requires the file to be in `/tmp`; we chose it.
  Allowing the directory treats the symptom and leaves the convention to spread
  to the next command (CLAUDE.md §4: fix root causes).

The honest counterweight: `Bash(*)` is allowed, so the agent can already `cat`
`/tmp` and the _incremental_ read exposure is smaller than it first looks. Not
zero — the deny list still blocks `Read` on secret paths a `/tmp` grant would
not reopen — but the decisive argument is the second one.

## Decision

### 1. Devgeta owns a scratch directory under the cache root

`paths.GetCacheDir("devgeta", "scratch")` — `~/.cache/devgeta/scratch`, honoring
`XDG_CACHE_HOME`. `GetCacheDir` already exists; no new path machinery.

**Cache, not state or data.** These files are disposable, read back only by the
CLI invoked seconds later, and safe to delete at any moment — which is the XDG
definition of cache, and not of state ("data that should persist between
restarts"). `XDG_RUNTIME_DIR` would be the ideal fit — per-user, `0700`, cleared
at logout — but it does not exist on macOS, a supported platform, so it would
mean two code paths for no gain over a cache subdirectory devgeta cleans itself.

**Which means the directory must be re-creatable, not created once.** Choosing
the cache root is choosing a location a user, a cleaner, or `rm -rf ~/.cache`
may empty at any time — that is the deal, not an edge case. A scratch root
created only at configure time would leave both PR commands broken until the
next `dg configure`, which is a worse failure than the prompt this ADR removes.

So one helper, `paths.EnsureScratchDir()`, does `MkdirAll` **and** an
unconditional `Chmod(dir, 0o700)`, and **every allocation calls it first**.
The `Chmod` is not redundant: `MkdirAll` applies its mode only to directories it
actually creates and masks it by the process umask, so a `scratch` left at `0755`
by an older devgeta keeps those bits. Only the leaf needs it — a `0755` parent is
harmless once traversing into `scratch` requires being its owner.

It lives in `pkg/paths` rather than beside the task logic because configure has
to call it too, and `internal/apps/baseapp` → `internal/tooling/task` →
`internal/apps/git` → `internal/apps/baseapp` is an import cycle. `pkg/` is the
documented home for shared path utilities, and one helper means one place to get
`0700` right.

Per-user and `0700` is what makes granting this materially different from
granting `/tmp`.

### 2. Both agents grant it, in their own key

| Agent       | Key                                                          |
| ----------- | ------------------------------------------------------------ |
| Claude Code | `permissions.additionalDirectories: ["<scratch>"]`           |
| OpenCode    | `permission.external_directory: { "<scratch>/**": "allow" }` |

Rendered from the resolved path rather than hardcoded, so an install with
`XDG_CACHE_HOME` set grants the directory it actually uses.

**Rendered as a JSON-escaped value, not a bare string.** Both files are JSON
produced by `text/template`, which does no escaping of its own. `ScratchDir` is
the first user-influenced value either template has ever interpolated — every
existing substitution is a fixed literal or a boolean — so a `$HOME` or
`XDG_CACHE_HOME` containing a quote or a backslash would emit a config that is
not valid JSON, breaking the agent outright rather than degrading. The template
receives a value already run through `json.Marshal`, and both rendered files are
asserted with `json.Valid` against paths containing spaces, quotes, and
backslashes.

`permissions_test.go` gains a parity check over this pair. It is a different
shape from the read/bash/edit blocks the existing parity test walks — an array
on one side, a pattern map on the other — so it needs its own normalization:
compare the set of granted roots, not the literal strings. Without it these two
grants are the one part of the permission surface that can silently diverge.

This corrects an earlier draft of this ADR, which claimed OpenCode needed no
grant because it had no working-directory boundary. It does; the prompt is just
named differently.

### 3. Each invocation gets a unique subdirectory

Fixed filenames in one user-wide directory collide. `dg ws` exists to run
parallel agent sessions, so two concurrent `/review-pr` runs would share
`review.md` — and the failure mode is posting one PR's review body to another
PR, which is worse than the prompt this ADR removes.

Allocation goes through a new `devgeta task scratch` subcommand, not a shell
command:

```bash
devgeta task scratch            # creates a unique dir, prints its absolute path
devgeta task scratch --clean <path>   # removes it
```

**A raw `mktemp` cannot work here.** These commands carry their own permission
frontmatter — `bash: {"*": "deny", "devgeta task *": "allow", "git diff*":
"allow", …}` — and a command-scope allowlist overrides the global `"*": allow`,
the same override `TestSharedAgentsInheritGlobalBashPolicy` exists to warn
about. `mktemp` is not on either list, so it would be **denied**, not prompted.
An earlier draft of this ADR claimed `mktemp` runs under `Bash(*)`; that is true
of an ordinary session and false inside these commands.

**Correction (2026-08-06):** the paragraph above is wrong. The cycle documented
in `docs/plans/cycles/2026-08-05-shared-command-permissions.md` established that
command frontmatter — including any `permission:`/`bash:` block — is ignored by
both agents; a command's real permissions come from the agent it runs under, or
the global policy if it has none. `mktemp` would in fact be **allowed** by the
global `Bash(*)` policy, not denied. This corrects the reasoning above, not the
decision below: it still holds on its other, independent legs (the global
`rm -rf *` deny, Go-confined deletion, and avoiding `/tmp`).

Cleanup is worse: `Bash(rm -rf *)` is denied globally in both configs, and deny
is evaluated before any allow from any scope, so no command-level rule can
re-enable it. Widening that deny to let a command clean up after itself would
trade a real global guardrail for a convenience.

The task form avoids both, and is better than an allowlist entry would have
been:

- **It needs no frontmatter change.** Command frontmatter is ignored anyway, so
  `devgeta task *` is available to every command by default via the global policy
  (not a command-level allowlist), meaning every future command that wants scratch
  space gets it free rather than repeating boilerplate — CLAUDE.md §3's "everything
  general, never bespoke".
- **Deletion is confined in Go, not in a glob.** CLAUDE.md §4 prefers a mistake
  class made structurally impossible over a shell rule that must be written
  correctly every time.

  "Inside the scratch root" is too loose to be the whole contract, though:
  it admits `--clean <root>`, which wipes every concurrent session's
  directories, and `--clean <root>/<other-session-dir>`. So `--clean` accepts
  only a **real directory** that is a **direct child** of the root and carries
  `paths.ScratchAllocPrefix` (`task-`) — the root itself, a grandchild, an
  unprefixed child, a relative path, and **any** symlink are refused.

  Bounds are checked **lexically first, before existence**, then re-checked
  after symlink resolution. Order matters in both directions: `filepath.Clean`
  collapses `..` up front, so an escape can name a path that does not exist —
  and if existence were tested first, the "already gone, nothing to do" branch
  would return success for an out-of-bounds target. The post-resolution
  re-check then catches what the lexical pass cannot see, a symlinked
  _ancestor_ that moves the target out of the root.

  Symlinks are refused outright rather than resolved-and-judged, because
  `os.MkdirTemp` never allocates one — so a symlink at that path was not ours
  whatever it points at. (An earlier version fell back to the lexical path
  whenever resolution failed, which refused a _live_ symlink out of the root
  but silently accepted a broken or looping one. The blast radius was smaller
  than it reads — `os.RemoveAll` never follows a top-level symlink, verified
  directly, so it would have unlinked the link and left the target intact —
  but the contract only held for the case that happened to resolve.)

  `--clean` is **idempotent**: an in-bounds target that no longer exists
  returns nil. A command's cleanup step runs on failure paths and retries too
  (§7), and a second call must not fail the command.

  The same prefix rule bounds configure's stale-directory prune (§4), for the
  same reason: the scratch root is granted to the agent, so a user may keep
  something of their own under it, and "old" is not a licence to delete what
  devgeta never allocated.

  What that does not buy: a session could still clean a sibling by guessing its
  `os.MkdirTemp` suffix. Closing that needs per-invocation ownership state, and
  the parties involved are the same user's own agent sessions, so the bound
  stops here deliberately rather than by oversight.

- **Allocation is self-healing.** It calls `paths.EnsureScratchDir()` before
  `os.MkdirTemp`, so a wiped `~/.cache` costs one `MkdirAll`, not a broken
  command until the next `dg configure` (§1).
- **Uniqueness comes from the OS** (`os.MkdirTemp`), not a naming convention.
- **The command never handles the path's expansion**, so `XDG_CACHE_HOME` or a
  `$HOME` containing spaces is the task's problem, solved once.

It also clears `docs/guides/task-design.md`'s bar on justification 2, enforcing
a policy an agent should always follow but might skip: scratch files land in the
one place devgeta prunes, and removal cannot escape it.

Each command calls `--clean` when it finishes. Configure prunes subdirectories
older than a day as the safety net for a run that was interrupted before it
could — two different jobs, so both stay.

### 4. Configure tightens and prunes; it is not what creates the directory

Given §1, allocation is the authority that the directory exists. Configure still
touches it, for two jobs allocation cannot do:

- **Tighten.** A `scratch` left at `0755` by an earlier devgeta is fixed on the
  next `dg configure`, not only on the next allocation.
- **Prune.** Remove **`task-`-prefixed** subdirectories older than a day — the
  safety net for a run interrupted before its own `--clean`. Bounded by the
  same ownership rule `--clean` uses (§3), and for the same reason: the root
  is granted to the agent, so anything a user parks under it stays, however
  old. Symlinks are skipped too — `os.MkdirTemp` never allocates one.

It runs in a named `baseapp` helper that **both** agents' `Configure` call, so an
OpenCode-only install gets it too; putting it in `claude.go`'s configure alone
would skip those users.

Not inside `SyncSharedParts`, despite that being the one function both already
call. Its contract is explicit — "Config that lives outside these parts […] is
left untouched, which is what makes `--only` safe to run against a hand-edited
config dir" — and hanging unrelated global maintenance off it would make
`dg configure claude --only skills` prune the scratch root as a side effect. A
separate helper keeps that promise intact; a test asserting both `Configure`
paths call it recovers the "impossible to forget" property piggybacking would
have given.

Both jobs call the same `paths.EnsureScratchDir()` the allocator does, so the
mode is decided in one place.

### 5. No command file names the path

Given §3, the commands never write the scratch root at all — not as a literal
`~/.cache/…` and not as a `${XDG_CACHE_HOME:-$HOME/.cache}/…` expansion. They
use whatever `devgeta task scratch` prints.

`paths.GetCacheDir` is then the single place the location is decided, shared by
the task, the configure-time creation (§4), and the rendered grants (§2). A
custom `XDG_CACHE_HOME` cannot desynchronize them, and moving the directory
later touches one function instead of every command file.

### 6. A test bans `/tmp` from devgeta-authored command files

CLAUDE.md §12: a constraint on an embedded config is enforced with a test
against the embedded configs FS, because a comment will not survive future
edits. A test asserts no `configs/shared/commands/*.md` contains a literal
`/tmp/`, so the next command cannot quietly reintroduce the prompt.

### 7. Vendored skills keep `/tmp`, knowingly

`configs/shared/skills/skill-creator/` and `.../brainstorming/` are upstream
Superpowers skills carrying their own `/tmp` paths, and are excluded from §6:
rewriting them buys a rare-path improvement for a merge conflict on every
upstream sync.

The residue is small. Most of their `/tmp` use is inside shell scripts they ship
(`brainstorming/scripts/start-server.sh`), and a shell redirect runs under
`Bash(*)` — it never prompts. Only agent-authored `Write` calls do, leaving
`skill-creator`'s eval-review HTML as the practical remainder. That prompt stays,
and this ADR does not claim otherwise.

## Consequences

- `/review-pr` and `/create-pr` run start to finish without a mid-command
  approval, on a fresh install, under both agents.
- Concurrent sessions cannot corrupt each other's scratch files, which was true
  of neither `/tmp` nor a fixed-name shared directory.
- One new grant per agent, scoped to a `0700` directory devgeta creates and only
  devgeta's commands write to — versus a shared, world-writable one.
- The first deliberate external-directory grant in either config. Future
  additions have a precedent to be measured against: a devgeta-owned, per-user
  path with a named writer, not a general-purpose location.
- A new parity surface. `additionalDirectories` and `external_directory` are
  compared by the same test that guards the rest of the permission policy, so
  the grant cannot drift to one agent only.
- One new `dg task` subcommand to maintain. In exchange, no command frontmatter
  changes here or in future — `devgeta task *` is already allowed everywhere —
  and no global deny gets weakened to let a command clean up after itself.
- **Correction (2026-08-06):** this ADR was drafted twice believing a command's
  `bash` frontmatter overrides the host policy. It does not — command
  frontmatter is ignored by both agents (see
  `docs/plans/cycles/2026-08-05-shared-command-permissions.md`), so "the global
  config allows it" was actually the right reason to believe a command can run
  something, not a mistake to guard against.
- Anything else needing scratch space has a documented home, so the next command
  does not reinvent one.
- The `skill-creator` prompt remains. If it becomes annoying, the fix is to fork
  that one path — a separately-argued change, not a widening of the grant.
