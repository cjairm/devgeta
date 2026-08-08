# ADR-0014 — Agent-config protection is a capability guard, not a path deny

**Date:** 2026-08-05
**Status:** PROPOSED

## Context

Both agent configs deny edits to their own configuration directory:

```
settings.json.tmpl    "Edit(.claude/**)", "Edit(.opencode/**)"
opencode.json.tmpl    ".claude/**": "deny", ".opencode/**": "deny"
```

The intent is sound: an agent that rewrites `settings.json`, a hook script, or
an agent definition can grant itself any permission the deny list withholds.
The rule expressing that intent is a **path glob**, and the path is now wrong in
both directions.

**Too broad.** Claude Code creates worktrees at `<repo>/.claude/worktrees/<name>/`
(`claude --worktree`, `EnterWorktree`, subagent `isolation: worktree`, and every
desktop parallel session). ADR-0010 adopted the same path for devgeta's
`worktree.location=in-repo`. A deny rule matches a directory of that name **at
any depth** under the working directory, so every source file in an in-repo
worktree is denied. Those files are ordinary application code and carry none of
the risk the rule exists for. The result is that one of the two layouts devgeta
itself offers is unusable with devgeta's own shipped permissions.

**Too narrow.** `.mcp.json` at a repo root defines MCP servers as arbitrary
commands — the same risk, and nothing denies it. Any future config-bearing file
either tool adds has the same gap, and a hand-maintained glob list will always
trail the tools that define the surface.

### Why the obvious fix does not work

Adding `Edit(.claude/worktrees/**)` to `allow` is a no-op in Claude Code:

> Rules are evaluated in order: deny, then ask, then allow. The first match in
> that order determines the outcome, and rule specificity doesn't change the
> order. […] a deny rule can't carry allowlist exceptions.

OpenCode resolves the opposite way — longest matching pattern wins — so the same
allow rule _would_ take effect there. Writing the carve-out the natural way in
each config yields two agents enforcing different policy, and
`internal/apps/opencode/permissions_test.go` would pass it, because that test
compares pattern strings, not semantics.

### What this is and is not

This is **friction and defense-in-depth, not a security boundary** — the same
framing `docs/apps/claude.md`'s "Known limits" already applies to the Bash deny
list, and it applies here for the same reason. `Bash(*)` is allowed, so
`echo … > .claude/settings.json`, `sed -i`, `cp`, or a `python3 -c` one-liner
reaches every file discussed below without touching Edit or Write at all.

That gap is unchanged by this ADR — today's `Edit(.claude/**)` deny has it too.
It is stated here because the rest of this document would otherwise read as a
claim to prevent privilege escalation, which no Edit/Write-scoped control can
do. The real boundary for high-autonomy work is Claude Code's OS sandbox
(`/sandbox`, Seatbelt on macOS), as `docs/apps/claude.md` already recommends.

So the question this ADR answers is narrower than "how do we stop escalation":
**how do we keep today's defense-in-depth level while un-breaking worktrees, and
stop the covered set from trailing the tools?**

## Decision

### 1. The rule moves to a PreToolUse guard, where the exception is expressible

Add `agent-config-guard.sh` / `agent-config-guard.js` as a fourth member of the
guard family established by ADR-0006, following every convention that ADR set:
PreToolUse deny (exit 2 / throw), bash sources `configs/claude/lib/`, OpenCode
imports from `task-redirect.js` rather than introducing a `lib.js`, and a
`DEVGETA_SKIP_AGENT_CONFIG_GUARD=1` escape hatch read from the launching shell.

A hook can express what the rule language cannot:

> A hook that exits with code 2 stops the tool call before permission rules are
> evaluated.

**The rule, stated once and mirrored in both files:**

> **Canonicalize** the target path, then deny an Edit or Write whose result
> matches any of:
>
> 1. a `.claude` path segment **not** immediately followed by a `worktrees`
>    segment;
> 2. **any** `.opencode` path segment;
> 3. a path under the resolved OpenCode config root,
>    `${XDG_CONFIG_HOME:-$HOME/.config}/opencode`, or under `OPENCODE_CONFIG_DIR`
>    when set;
> 4. the file named by `OPENCODE_CONFIG`, when set — canonicalized first, and
>    matched against **every** candidate base if the value is relative (see
>    below), since clause 4 compares it to an already-canonicalized target.

**The `worktrees` exception is `.claude`-only (clause 1 vs clause 2).** Only
Claude Code creates `<repo>/.claude/worktrees/`, and ADR-0010 adopted that same
path for devgeta's in-repo layout. `.opencode/worktrees/` is not a location
anything writes to, so granting it an exception would widen the rule for no use
case — while leaving OpenCode's project config reachable through a directory
name an agent can simply create. Symmetry between the two agents is a goal for
_policy_, not for exceptions with only one owner.

**Clauses 3 and 4 exist because the segment heuristic is both asymmetric and
overridable.** Two separate problems:

| Agent       | Global config                                              | Dotted segment? | Extra config sources                     |
| ----------- | ---------------------------------------------------------- | --------------- | ---------------------------------------- |
| Claude Code | `~/.claude/settings.json`                                  | yes — `.claude` | —                                        |
| OpenCode    | `~/.config/opencode/opencode.json` (`GetConfigDir` + name) | **no**          | `OPENCODE_CONFIG_DIR`, `OPENCODE_CONFIG` |

Clauses 1–2 alone protect Claude's default global config and miss OpenCode's
entirely — the file containing the permission policy this ADR is about, plus the
`plugin/` directory holding the guards themselves.

Clauses 3–4 then cover OpenCode's two extra config sources. Both were checked
against the installed OpenCode 1.18.14 rather than assumed, because whether they
_relocate_ the root or _add_ to it decides whether the guard can enforce them at
all:

```
$ OPENCODE_CONFIG_DIR=/tmp/oc-alt opencode debug paths
config     /Users/jair.mendez/.config/opencode      # unchanged
```

Both are **additive**: the binary pushes `OPENCODE_CONFIG` onto a config list and
treats `OPENCODE_CONFIG_DIR` as one more entry beside project `.opencode` dirs.
The global root does not move, so devgeta's plugin still loads and clauses 3–4
are enforceable by a guard deployed to the default location.

Clauses 3 and 4 take no `worktrees` exception either — nobody checks out a
worktree inside a global config directory.

**A relative `OPENCODE_CONFIG` is resolved against every candidate base, not a
chosen one.** Which directory OpenCode resolves a relative value against is
**not established**: `opencode debug config` does not report whether the file was
loaded, so the obvious probe — set the value relative from one directory, then
from a subdirectory, and diff the resolved config — cannot distinguish "resolved
against a different base" from "not surfaced by this command". Absolute and
relative values produced identical output, so the probe proves nothing either
way.

Rather than pick a base and encode a guess, the guard canonicalizes the value
against each plausible base — the hook payload's `cwd` and the guard process's
own working directory — and denies on a match with any of them. The cost of the
extra candidate is at most one additional denied path, itself a config file path
either way. The cost of guessing wrong is clause 4 silently never matching. If
OpenCode's rule is later established, this collapses to the single correct base
and the test matrix stays valid unchanged.

### 1a. Known limit: the guard only protects roots devgeta deploys into

`CLAUDE_CONFIG_DIR` is deliberately **absent** from the rule. A guard cannot
protect a config root it was never loaded from, and devgeta deploys to a fixed
location twice over: `paths.Paths.Config.Claude` is `GetHomeDir(".claude")`, and
`settings.json.tmpl` hardcodes the hook commands themselves as literals —
`~/.claude/task-redirect.sh`, `~/.claude/format.sh`, and six more. If
`CLAUDE_CONFIG_DIR` moves where Claude Code reads `settings.json`, it reads a
file devgeta never wrote, so no devgeta hook is registered and the guard does not
run. Adding a clause for that root would assert a protection that cannot exist.

This is not a guard-specific gap. Under a relocated `CLAUDE_CONFIG_DIR`, none of
devgeta's Claude integration reaches the agent — not the statusline, not the
formatter, not `secret-guard`, not the shared commands. The guard is exactly as
covered as everything else devgeta ships, which is the most this ADR can honestly
claim.

Making deployment honor `CLAUDE_CONFIG_DIR` is a real improvement and a separate
cycle: it touches `pkg/paths`, every hook command literal in the template, and
the migration for anyone already relocated. It is not in scope here, and this
ADR should not be read as having addressed it.

Canonicalization is part of the rule, not an implementation detail. Without it
the `worktrees` exception is trivially escapable, in two ways:

- **Lexically:** `.claude/worktrees/../agents/x.md` has a `.claude` followed by
  `worktrees` and passes a naive walk, while resolving to `.claude/agents/x.md`.
- **Through a symlink:** a link under `worktrees/` pointing at `.claude/agents/`
  puts every allowed-looking path inside a denied directory.

Neither is hypothetical for the thing being replaced — Claude Code applies its
own deny rules to both the symlink and its target ("Deny rules: apply when
either the symlink path or its target matches"). A hook receives the raw
`tool_input.file_path` and gets none of that for free, so a guard that skipped
this step would be **weaker** than the deny it replaces, which would defeat the
whole ADR.

So, in order: make the path absolute against the payload's `cwd`, clean `.` and
`..` lexically, resolve symlinks on the deepest existing ancestor, and re-append
the not-yet-existing tail — the target of a `Write` usually does not exist yet,
so resolving the full path is not an option.

The lexical clean is pure string work and always succeeds. Symlink resolution
does I/O and can fail, and this is the one place the family's fail-open stance
is wrong: on failure the guard walks the lexically cleaned path rather than
giving up. That keeps `..` closed unconditionally, leaves only the symlink case
dependent on I/O, and still cannot block every edit.

| Path                                                                       | Outcome | Why                                                   |
| -------------------------------------------------------------------------- | ------- | ----------------------------------------------------- |
| `<repo>/.claude/worktrees/foo/src/main.go`                                 | allow   | the only `.claude` is followed by `worktrees`         |
| `<repo>/.claude/agents/x.md`                                               | deny    | followed by `agents`                                  |
| `<repo>/.claude/worktrees/foo/.claude/settings.json`                       | deny    | the second `.claude` is followed by `settings.json`   |
| `<repo>/.claude/worktrees/foo/.claude/worktrees/bar/a.go`                  | allow   | both are followed by `worktrees`                      |
| `~/.claude/settings.json`                                                  | deny    | the hook sees absolute paths                          |
| `<repo>/.claude/worktrees/../agents/x.md`                                  | deny    | cleans to `.claude/agents/x.md`                       |
| `<repo>/.claude/worktrees/foo/link/x.md`, `link` → `<repo>/.claude/agents` | deny    | resolves into `.claude/agents`                        |
| `<repo>/src/link/x.md`, `link` → `<repo>/.claude/agents`                   | deny    | a denied target reached from an innocent path         |
| `<repo>/.opencode/agent/x.md`                                              | deny    | clause 2 — any `.opencode` segment                    |
| `<repo>/.opencode/worktrees/foo/src/main.go`                               | deny    | clause 2 — the exception is `.claude`-only            |
| `$OPENCODE_CONFIG_DIR/opencode.json`                                       | deny    | clause 3 — an additional root, not a relocated one    |
| the file named by `$OPENCODE_CONFIG`, relative or symlinked                | deny    | clause 4 — resolved before comparison                 |
| `$CLAUDE_CONFIG_DIR/settings.json` (relocated)                             | _n/a_   | §1a — the guard is not loaded there at all            |
| `~/.config/opencode/opencode.json`                                         | deny    | clause 3 — no dotted segment for clause 2 to match    |
| `~/.config/opencode/plugin/secret-guard.js`                                | deny    | clause 3 — the guards' own directory                  |
| `$XDG_CONFIG_HOME/opencode/opencode.json` (custom root)                    | deny    | clause 3 resolves the root, not a literal `~/.config` |

The last row is the mirror image of the symlink case and falls out of the same
step: canonicalizing first means the rule sees where a write _lands_, not how it
was spelled.

A worktree is a checkout of some repository, so it has its own `.claude/` — as
sensitive as the outer one. Prefix-stripping misses it; a segment walk does not.

The durable property is the **default**: deny everything under those directories,
with one small, stable exception. The enumerated-deny alternative in §4 inverts
that — allow by default, with a list that must chase upstream.

### 2. Scope: global

ADR-0006's test is whether the rule encodes universal practice (global) or
devgeta's own opinion (devgeta-repo-only, gated on the `go.mod` walk). "An agent
should not rewrite its own configuration" is universal, so this guard is global,
like `secret-guard.sh` and unlike `suppression-guard.sh`.

It also _must_ be global: it replaces a deny that is already global today.
Gating it on the devgeta repo would remove the protection from every other
project on the machine.

### 3. A fail-closed floor stays in settings

The guard family fails **open** by design — a missing `jq`, an unparseable
payload, or a `PATH` problem exits 0 rather than blocking every edit. Correct
for a style rule; not sufficient alone here.

So the blanket deny is replaced, not removed. Both configs keep a deny on the
paths that are never legitimately agent-edited in any repo or worktree, and
therefore need no exception:

```
Edit(**/.claude/settings.json)        Edit(**/.opencode/opencode.json)
Edit(**/.claude/settings.local.json)  Edit(**/.opencode/plugin/**)
Edit(**/.claude/hooks/**)             Edit(**/.mcp.json)

Edit(~/.claude/**)                    Edit(~/.config/opencode/**)
```

The two `~/`-anchored rules are the floor's half of the asymmetry above. The
`**/`-prefixed patterns anchor at the working directory, so they never match
either agent's global configuration; without these, clause 2's coverage would
exist only in the guard, and a broken `jq` would take it down with it. They
match the default locations only. OpenCode's additive sources
(`OPENCODE_CONFIG_DIR`, `OPENCODE_CONFIG`) are the guard's job alone, since
permission patterns cannot resolve an environment variable — so a user who sets
either keeps the guard but loses the floor for those paths, and their fail-open
case is thinner. Accepted rather than papered over: rendering resolved values
into the templates at configure time would go stale the moment the variable
changed, which is worse than a documented gap. A relocated `CLAUDE_CONFIG_DIR`
is covered by neither layer, for the reason in §1a.

These are expressible as plain denies precisely because they carry no exception,
and they hold even if the guard never runs. `.mcp.json` closes the gap in §1.

Bounded, non-overlapping failure modes:

- **Floor (settings deny):** cannot express the worktree exception, so it covers
  only the enumerable never-edit set — but survives the guard failing open.
- **Ceiling (guard):** covers everything else under `.claude/` and `.opencode/`,
  including surface the tools have not shipped yet, and carries the exception.

Ordering constraint this creates: the floor must stay _narrower_ than the guard's
rule. A blanket deny left in settings wins over the guard unconditionally and
re-breaks worktrees, since deny is evaluated regardless of what a hook returns.

### 4. Rejected: widen the deny list and keep it current

Enumerating `agents/**`, `commands/**`, `skills/**`, `plugins/**`, `*.sh` and so
on in both configs fixes worktrees today with no new code, and — given §1's Bash
gap — is bypassable in exactly the same ways the guard is. It was still rejected
as the primary mechanism because it is default-allow against surface that does
not exist yet, and nothing can test for a file upstream has not shipped. CLAUDE.md
§4 asks for the class of mistake to be structurally impossible rather than tracked
by hand. It survives as the floor in §3, scoped to where enumeration is complete.

### 5. Rejected: extend the guard to Bash writes

A Bash matcher for `>`/`>>`, `tee`, `sed -i`, and `cp`/`mv` destinations would
catch the literal cases. Rejected: deciding whether an arbitrary shell command
writes to a path is undecidable in general (`sh -c`, `python3 -c`, base64,
variable indirection, heredocs), so the matcher would stop only an agent that
was not trying, while reading as though the gap were closed. `docs/apps/claude.md`
already names the OS sandbox as the answer for work that needs a real boundary.
An honest documented limit beats a partial control that invites over-trust.

## Consequences

- In-repo worktrees work under devgeta's shipped permissions, for both
  `claude --worktree` and `worktree.location=in-repo`. ADR-0010's second layout
  value stops being blocked by ADR-0006's siblings.
- The covered set stops trailing upstream: new files under `.claude/` or
  `.opencode/` are denied the day the tools add them, with no devgeta release.
- The policy is stated once in prose and mirrored in two files, joining the
  hand-sync burden ADR-0006 already accepted for three other pattern lists —
  but enforced more strictly than those are. A behavioral test runs the table
  above against **both** files, rather than diffing their text the way
  `task-redirect.sh`/`.js` are compared. String parity would not notice one
  mirror resolving symlinks and the other not, which is the whole risk here.
- `permissions_test.go`'s pinned `want["edit"]` set changes. Replacing a pinned
  policy entry is when the pin is weakest, so the floor list and that behavioral
  test land in the same change that drops `.claude/**` — never a commit apart.
- Protection for `~/.claude/` improves. Today's deny is anchored at the working
  directory and never matched the user's global config; the guard reads absolute
  paths and covers it.
- A repo that legitimately needs its own `.claude/agents/` edited now needs the
  documented env-var bypass. Deliberate tightening: it was blocked outright
  before, so no workflow regresses.
- OpenCode's global `opencode.json` and `plugin/` directory become protected for
  the first time. Today's `Edit(.opencode/**)` never matched them — the path is
  `~/.config/opencode/`, with no dotted segment — so the config holding the
  permission policy, and the directory holding the guards themselves, have been
  freely editable the whole time. That is a gap this ADR closes, not one it
  introduces.
- Symlinked writes into `.claude/` are denied from any spelling, including from
  a path with no `.claude` segment in it at all — coverage the deny rule it
  replaces had only because Claude Code resolves symlinks internally.
- Canonicalization is the guard's sharpest edge. It is the one step where a
  correct-looking mirror can be silently weaker than its twin, so the `..` and
  symlink rows of the table are test cases in both files, not prose.
- The Bash gap in §1 remains open and documented. Anyone reading this ADR as
  escalation prevention is reading it wrong; `/sandbox` is that control.

## Amendment (2026-08-07) — agent memory is data, not configuration

**Status:** ACCEPTED

Both layers of this ADR denied Claude Code's per-project memory directory,
`~/.claude/projects/<slug>/memory/`, so writing a memory file failed. Clause 1
denied it (a `.claude` segment followed by `projects`, not `worktrees`), and
the §3 floor's `Edit(~/.claude/**)` denied it independently.

That directory holds notes the agent is **designed** to write, and nothing in
it grants a permission, registers a hook, or defines an agent, command, or
skill. Denying it blocked a feature without protecting anything this ADR is
about. Both layers change.

### Clause 1 gains a second exception

The rule in §1 clause 1 now reads: a `.claude` segment not immediately
followed by **either**

- a `worktrees` segment, **or**
- `projects` / one slug segment / `memory`, with a file strictly below it.

Scoped exactly that tightly, on purpose. `projects/<slug>/` also holds session
transcripts and stays denied; `memory` as a plain file with nothing under it
stays denied; and a `.claude` segment appearing again anywhere below `memory/`
is evaluated on its own, so `…/memory/.claude/settings.json` is still denied.
Canonicalization already runs first, so `…/memory/../../settings.json` and a
symlink out of `memory/` are denied for the same reason every other escape in
§1's table is.

Like `worktrees`, this exception is `.claude`-only. OpenCode has no equivalent
directory, so granting `.opencode` one would widen clause 2 for no use case.

| Path                                           | Outcome | Why                                   |
| ---------------------------------------------- | ------- | ------------------------------------- |
| `~/.claude/projects/<slug>/memory/MEMORY.md`   | allow   | the memory exception                  |
| `~/.claude/projects/<slug>/memory/sub/note.md` | allow   | any depth below `memory/`             |
| `~/.claude/projects/<slug>/settings.json`      | deny    | the exception is `memory/` only       |
| `~/.claude/memory/x.md`                        | deny    | wrong shape — no `projects/<slug>/`   |
| `~/.claude/projects/<slug>/memory`             | deny    | nothing strictly below `memory/`      |
| `~/.claude/projects/<slug>/memory/.claude/…`   | deny    | the second `.claude` fails on its own |

### The floor stops using a blanket `~/.claude/**`

An allow carve-out cannot fix the floor — §1 already establishes that Claude
Code evaluates deny before allow with no specificity tiebreak, and permission
patterns have no negation operator. The deny simply must not match. So the
global Claude root is now **enumerated** instead of swept:

```
Edit(~/.claude/*.json)      Edit(~/.claude/agents/**)     Edit(~/.claude/plugins/**)
Edit(~/.claude/*.sh)        Edit(~/.claude/commands/**)   Edit(~/.claude/hooks/**)
Edit(~/.claude/*.md)        Edit(~/.claude/skills/**)     Edit(~/.claude/lib/**)
```

The three extension patterns cover every direct child devgeta or Claude Code
puts there — `settings.json`, `settings.local.json`, the global `CLAUDE.md`,
and all seven deployed `*.sh` hook scripts. A bare `~/.claude/*` would have
covered future direct children too, but a single-segment `*` next to the very
directory this amendment is unblocking is the wrong place to rely on a matcher
detail, so the shapes that provably cannot match `projects` were chosen
instead.

**This is a real narrowing of the floor**, and it is §4's rejected
enumerate-and-maintain approach applied to one root. A new config-bearing
subdirectory upstream adds under `~/.claude/` is not in the floor until
someone adds it. That cost is accepted because §3 already assigns exactly this
division of labor: the floor covers the enumerable never-edit set and survives
the guard failing open; the **guard** is the default-deny layer that covers
surface the tools have not shipped yet, and clause 1 still denies every such
path. The alternative — keeping the blanket and leaving memory broken — trades
a feature for coverage that only matters when `jq` is missing.

`internal/apps/opencode/permissions_test.go`'s
`TestGlobalClaudeFloorLeavesMemoryWritable` pins both halves: no `~/.claude/**`
in either config, and every enumerated surface still denied — so the blanket
cannot come back silently, and the enumeration cannot be deleted to "fix"
memory a second time.
