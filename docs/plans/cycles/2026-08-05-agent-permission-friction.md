# Cycle: Remove agent permission friction without widening the blast radius

**Date:** 2026-08-05
**Estimated Duration:** ~2 days
**Status:** Done — code, tests, and docs complete; the manual live-agent
checklist below still needs a human to run through `claude`/`opencode`
interactively before this is called verified end-to-end.

---

## 1. Domain Context

Devgeta ships permission policy to two AI coding agents (CLAUDE.md §12). Two
places in that policy currently cost users approval prompts they should never
see, and neither is fixable by loosening a rule:

1. **In-repo worktrees are denied.** `Edit(.claude/**)` blocks every file under
   `<repo>/.claude/worktrees/<name>/`, which is where `claude --worktree` and
   devgeta's own `worktree.location=in-repo` (ADR-0010) put checkouts. Claude
   Code evaluates deny before allow with no specificity tiebreak, so an allow
   carve-out is a no-op — the rule has to move somewhere exceptions exist.
   Not hypothetical: `worktree.location` is already `in-repo` on the maintainer's
   machine, so every in-repo worktree is hitting this today.
2. **Shipped commands write scratch files to `/tmp`.** `/review-pr` and
   `/create-pr` tell the agent to `Write` outside the working directory, which
   both agents prompt on (Claude Code: `additionalDirectories`; OpenCode:
   `permission.external_directory`).

Decisions and full rationale: [ADR-0014](../../decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md)
and [ADR-0015](../../decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md).
Read both before starting — this doc is the how, they are the why.

---

## 2. Engineer Context

**Relevant files:**

- `configs/claude/settings.json.tmpl`, `configs/opencode/opencode.json.tmpl` — the two policy surfaces
- `configs/claude/suppression-guard.sh` + `configs/opencode/plugin/suppression-guard.js` — closest model for the new guard pair
- `configs/claude/lib/devgeta-repo.sh`, `lib/segments.sh` — bash hook helpers (ADR-0006)
- `internal/apps/claude/claude.go:109` — the script list that deploys hooks, chmod 0755
- `internal/apps/baseapp/configure.go:28` — `SyncSharedParts`; note its contract: parts outside the selection stay untouched
- `pkg/paths/paths.go:224` — `Config.OpenCode` = `GetConfigDir("opencode")`, i.e. `~/.config/opencode` with **no** dotted segment
- `internal/apps/opencode/permissions_test.go` — parity + pinned-policy tests
- `pkg/paths/paths.go:318` — `GetCacheDir`

**Conventions that are non-obvious:**

- OpenCode's plugin loader imports every `.js` under `plugin/` and invokes every
  export as a plugin factory. Do **not** add a `lib.js`; import from
  `task-redirect.js` (ADR-0006 §3).
- Guards fail **open** on missing `jq` / unparseable payload. That is deliberate
  and is why ADR-0014 keeps a settings-level floor.
- Claude renders `settings.json.tmpl` with `gc.Integrations`; OpenCode renders
  with a `map[string]string`. Both need a new `ScratchDir` value.

**Commands:**

```bash
go test ./internal/apps/opencode/ ./internal/apps/claude/ ./internal/apps/baseapp/
go test ./...
make lint
```

---

## 3. Objective

In-repo worktrees and the shipped PR commands run without a permission prompt,
with the agent-config protection strictly broader than it is today and the two
agents provably still in sync.

---

## 4. Scope Boundary

### In Scope

- [x] `agent-config-guard.sh` + `agent-config-guard.js` implementing ADR-0014's segment-walk rule
- [x] Replace the `.claude/**` / `.opencode/**` blanket denies with ADR-0014 §3's floor list, incl. `**/.mcp.json`
- [x] Deploy + chmod the new bash guard; register both in their hook configs
- [x] `paths.EnsureScratchDir()` called by both the allocator and a named `baseapp` maintenance helper; granted (JSON-escaped) in both configs; stale-subdir pruning
- [x] Behavioral test over ADR-0014's table × {`.claude`, `.opencode`} × {Edit, Write}, against both the `.sh` and the `.js`
- [x] `devgeta task scratch` / `--clean <path>` in `internal/tooling/task`, removal limited to an allocated direct child
- [x] `/review-pr` and `/create-pr` switched onto it
- [x] Tests: updated `want["edit"]`, grant parity check, `/tmp` ban on `configs/shared/commands/*.md`, deployment expectations
- [x] Docs: `docs/apps/claude.md` guard section + known-limits note; CLAUDE.md §12 sync table row; `docs/spec.md` entry for `dg task scratch`; `docs/migrations/` upgrade note

### Explicitly Out of Scope

- Guarding Bash-based writes to agent config (ADR-0014 §5 — rejected, documented)
- Rewriting vendored Superpowers skills off `/tmp` (ADR-0015 §7)
- Any change to `secret-guard`, `suppression-guard`, or `task-redirect` behavior
- Enabling `/sandbox` or any OS-level sandbox work

**Scope is locked.**

---

## 5. Implementation Plan

### File Changes

| Action | File                                               | Description                                                 |
| ------ | -------------------------------------------------- | ----------------------------------------------------------- |
| Create | `configs/claude/agent-config-guard.sh`             | Segment-walk deny, global scope, exit 2                     |
| Create | `configs/opencode/plugin/agent-config-guard.js`    | Mirror; imports nothing new                                 |
| Modify | `configs/claude/settings.json.tmpl`                | Floor denies, `additionalDirectories`, hook entry           |
| Modify | `configs/opencode/opencode.json.tmpl`              | Floor denies, `external_directory`                          |
| Modify | `internal/apps/claude/claude.go`                   | Deploy guard; render `ScratchDir`                           |
| Modify | `internal/apps/opencode/opencode.go`               | Render `ScratchDir`                                         |
| Modify | `pkg/paths/paths.go`                               | `EnsureScratchDir()` — MkdirAll + unconditional Chmod       |
| Modify | `internal/apps/baseapp/configure.go`               | Named scratch-maintenance helper (NOT in `SyncSharedParts`) |
| Create | `agent_config_guard_test.go` (package main)        | Behavioral: full ADR-0014 matrix × both guards              |
| Modify | `internal/tooling/task/`                           | Scratch allocation + `--clean` containment logic            |
| Modify | `cmd/task.go`                                      | `scratch` Cobra wiring; `taskRunner` interface method       |
| Modify | `cmd/task_test.go`                                 | Subcommand wiring against the injected mock                 |
| Modify | `configs/shared/commands/{review-pr,create-pr}.md` | Allocate via `devgeta task scratch`, clean at the end       |
| Modify | `internal/apps/opencode/permissions_test.go`       | `want["edit"]`, grant parity, `/tmp` ban                    |
| Modify | `internal/apps/claude/claude_test.go`              | New script in deployment expectations                       |
| Modify | `internal/apps/baseapp/baseapp_test.go`            | Scratch dir creation + prune                                |
| Modify | `internal/apps/opencode/opencode.go`, `claude.go`  | Both `Configure` call the maintenance helper                |
| Create | `docs/migrations/agent-permission-refresh.md`      | `--force` upgrade note for existing installs                |
| Modify | `docs/migrations/README.md`                        | Index the new guide — at ship time, per its own rule        |
| Modify | `docs/apps/claude.md`, `CLAUDE.md`, `docs/spec.md` | Guard docs, §12 sync row, `dg task scratch` entry           |

### Step-by-Step

#### Step 1: `agent-config-guard.sh`

Header comment in the house style: rule, GLOBAL scope + why (ADR-0014 §2),
fail-open note, `DEVGETA_SKIP_AGENT_CONFIG_GUARD=1` escape hatch, and a pointer
to the `.js` mirror. Read `.tool_name` / `.tool_input.file_path` from stdin like
`suppression-guard.sh`.

Canonicalize before walking (ADR-0014 §1 — this is the step that makes the guard
correct, not an optimization): absolute against the payload `cwd`, lexically
clean `.`/`..`, then resolve symlinks on the deepest **existing** ancestor and
re-append the tail, since a `Write` target usually does not exist yet. If
resolution errors, fall back to the lexically cleaned path — never to skipping
the check. Then apply ADR-0014 §1's four clauses. Deny when the canonical path
has:

1. a `.claude` segment not followed by `worktrees`;
2. **any** `.opencode` segment — no exception, deliberately unlike clause 1;
3. a prefix of `${XDG_CONFIG_HOME:-$HOME/.config}/opencode`, or of
   `OPENCODE_CONFIG_DIR` when set;
4. equality with `OPENCODE_CONFIG` when set, after resolving that value to an
   absolute canonical path — it may be relative or a symlink, and clause 4
   compares against an already-canonicalized target.

Clauses 3–4 are the only thing covering OpenCode's global config, which has no
dotted segment for clause 1 to catch. `CLAUDE_CONFIG_DIR` is **not** handled —
see ADR-0014 §1a; the guard is not loaded from a relocated Claude root, so a
clause for it would assert protection that cannot exist.

Verify: step 3's behavioral test, not a manual pass.

#### Step 2: `agent-config-guard.js`

Mirror exactly, exporting only its own plugin factory. `fs.realpathSync` on the
nearest existing parent, same fallback.
Verify: step 3's behavioral test.

#### Step 3: `agent_config_guard_test.go` — behavioral, both guards

Every row of ADR-0014 §1's table as one table-driven test, run against **both**
files with real payloads on stdin and real symlink fixtures under `t.TempDir()`.

The matrix is the cross product, not just the table as written: each shape in
both `.claude` and `.opencode` spellings, and each case for **both `Edit` and
`Write`** — the rule covers both tools and only `Edit` payloads would exercise
half of it. Include a case per clause, with `XDG_CONFIG_HOME`, `OPENCODE_CONFIG_DIR`, and
`OPENCODE_CONFIG` each pointed at `t.TempDir()` — the only way to exercise the
resolved roots without touching the real ones. For clause 4, cover a **relative**
value and a **symlinked** value, since those are the forms a naive string compare
gets wrong.

Assert `.opencode/worktrees/foo/src/main.go` is **denied**: it is the one place
the two directory names deliberately diverge, so a mirror that copied clause 1
across would pass every other row.

These tests prove the guard's _logic_ under overrides. They do **not** prove
either agent loads the guard when a config root moves, and nothing here should
claim to — ADR-0014 §1a scopes the guarantee to devgeta-managed default roots.
Model it on `suppression_guard_test.go`, which already does this for the shell
guard. Also cover the fail-open cases the family shares (malformed JSON, missing
`jq`) and the `DEVGETA_SKIP_AGENT_CONFIG_GUARD=1` bypass.

Executing the `.js` mirror is **new** — today the JS side is only compared
statically (`task_redirect_test.go:654` diffs deny-message strings). That is
adequate for a pattern list and useless here: string parity cannot tell that one
mirror resolves symlinks and the other forgot to. Skip the JS half via
`exec.LookPath("node")` when Node is absent, following `layout_test.go`'s
`LookPath("sh")` precedent.

Verify: `go test -run AgentConfigGuard ./...` — the full matrix passes on both,
and deliberately breaking canonicalization in one file fails the test.

#### Step 4: Swap the policy in both templates

Drop `Edit(.claude/**)` / `Edit(.opencode/**)` and add ADR-0014 §3's **eight**
floor denies to both configs, verbatim:

```
Edit(**/.claude/settings.json)        Edit(**/.opencode/opencode.json)
Edit(**/.claude/settings.local.json)  Edit(**/.opencode/plugin/**)
Edit(**/.claude/hooks/**)             Edit(**/.mcp.json)
Edit(~/.claude/**)                    Edit(~/.config/opencode/**)
```

Then register the guard in Claude's `PreToolUse` `Edit|Write` matcher (alongside
`suppression-guard.sh`).
Verify: `go test ./internal/apps/opencode/` fails only on the pinned `want` set.

#### Step 5: Update the pinned policy

Replace `want["edit"]`'s three entries with the eight from step 4.

**The floor cannot be verified automatically, and the plan must not pretend
otherwise.** `permissions_test.go` parses JSON and compares rule strings; there
is no permission matcher anywhere in the Go tree, and
`DEVGETA_SKIP_AGENT_CONFIG_GUARD` only disables devgeta's guard — it has no
effect on either agent's permission engine, which is the thing that evaluates
the floor. Writing a matcher in the test would verify a reimplementation of
Claude Code's and OpenCode's glob semantics, not their actual behavior.

So the coverage splits by what each layer can honestly prove:

- **Automated (here):** the eight patterns are present, in both configs, with the
  right action — the pinned `want["edit"]` set plus the existing parity test.
  That catches a dropped or mistyped pattern, which is the realistic regression.
- **End-to-end (manual, checks 9b and 14b):** that a present pattern actually
  denies. Only a real agent session can show this, and only with the guard
  bypassed so the floor is what is being observed.

No separate mirror check — step 3's behavioral test covers the two guards by
running them, which subsumes the string-level "both files mention `worktrees`"
assertion an earlier draft proposed.
Verify: `go test ./internal/apps/opencode/`.

#### Step 6: Deploy the bash guard

Add `agent-config-guard.sh` to `claude.go`'s script list and to
`claude_test.go`'s deployment + executable-bit expectations.
Verify: `go test ./internal/apps/claude/`.

#### Step 7: `paths.EnsureScratchDir()`, then wire configure to it

`EnsureScratchDir()` in `pkg/paths`: `MkdirAll` under
`GetCacheDir("devgeta", "scratch")` **followed by an unconditional
`Chmod(0o700)`** (ADR-0015 §1 — `MkdirAll` skips an existing directory and is
umask-masked), returning the path. It goes in `pkg/paths`, not next to the task
logic, because configure must call it too and
`baseapp` → `tooling/task` → `apps/git` → `baseapp` is an import cycle.

A **named `baseapp` maintenance helper** then calls it and prunes subdirectories
older than 24h, and **both** agents' `Configure` call that helper. Do not hang
this off `SyncSharedParts`: its contract promises that config outside the
selected parts is untouched, so `dg configure claude --only skills` would
otherwise prune the scratch root as a side effect (ADR-0015 §4). Add a test that
both `Configure` paths call it, which is the property piggybacking would have
given for free.

Configure is **not** what creates the directory — step 9's allocator calls the
same `paths.EnsureScratchDir()`, so a wiped `~/.cache` self-heals.

Note when this actually runs: `SoftConfigure` returns at the marker file, so an
ordinary `dg configure` on an existing install does **neither** the tighten nor
the prune — both happen only on `--force` or a first install. That is why
allocation must self-heal, and why the prune is a long-horizon sweep rather than
a guarantee.

Document that lifecycle at the **call sites** and in `docs/apps/claude.md`, not
in the helper's own doc comment. The helper cannot know when it is invoked — a
comment there claiming "runs only on --force" would be describing its callers,
and would silently become wrong the moment a third caller appeared.

Tests must isolate the cache root via `paths.Paths.*` with `t.Cleanup` restore
(CLAUDE.md testing checklist). Cover: created when absent, mode tightened when
pre-created at 0755, and stale-vs-fresh subdirectory pruning.
Verify: `go test ./pkg/paths/ ./internal/apps/baseapp/`.

#### Step 8: Grant it in both configs

Claude: a render-data struct in `claude.go` embedding `IntegrationsConfig` plus
`ScratchDir` — field promotion keeps `{{if .RtkClaudeHook}}` working. OpenCode:
one more `map[string]string` key.

**Pass the value through `json.Marshal` before handing it to the template.**
Both files are JSON rendered by `text/template`, which escapes nothing, and this
is the first user-influenced value either template interpolates (ADR-0015 §2) —
a quote or backslash in `XDG_CACHE_HOME` would otherwise emit a config the agent
cannot parse at all.

Add the grant-parity test comparing granted roots across the two shapes.
Verify: `go test ./internal/apps/{claude,opencode}/` — including a render with
`XDG_CACHE_HOME` containing a space, a `\"`, and a `\\`, asserting `json.Valid`
on **both** rendered files. `permissions_test.go` already validates the rendered
OpenCode config this way; the Claude side has never been checked.

#### Step 9: `devgeta task scratch`

Logic goes in `internal/tooling/task`, not `cmd/`. `cmd/task.go` gets the Cobra
wiring plus a method on the `taskRunner` interface (`cmd/task.go:32`) so
`setupTaskMock` can inject it — the shape every existing task subcommand uses.

Allocation: `paths.EnsureScratchDir()` **first**, then `os.MkdirTemp` under what
it returns, printing the absolute path on one line and nothing else
(`docs/guides/task-design.md`). The ensure call is not belt-and-braces — the
cache root is a location a user or a cleaner may empty at any time, which is the
premise ADR-0015 §1 chose it on, so allocation has to re-create it rather than
assume configure did.

`--clean <path>` accepts only a **direct child** of the scratch root whose name
matches the allocation prefix, checked after canonicalization (ADR-0015 §3).
Refuse the root itself, a grandchild, a directory _beside_ the root
(`~/.cache/devgeta/other`), a child whose name lacks the prefix, and anything
reached by `..` or a symlink. "Inside the root" alone is not the contract — it
would let one invocation delete every concurrent session's directory at once.

**Not refused:** another live session's directory, which is a prefixed direct
child and therefore accepted. ADR-0015 §3 takes that bound deliberately — the
only way to close it is per-invocation ownership state, and the parties are the
same user's own agent sessions. Do not add an ownership check here; the sibling
case is a decision, not a gap.

Confirm `task-redirect.sh`/`.js` do not intercept the new subcommand.

Verify: `go test ./internal/tooling/task/ ./cmd/` — allocation returns a fresh
path each call **and succeeds with the scratch root deleted beforehand**;
`--clean` removes its own directory; and the root, a grandchild, a directory
beside the root, an unprefixed child, a `..` escape, and a symlink pointing out
are each refused with the target still on disk. No test asserts a sibling
session's directory is refused — see above.

#### Step 10: Switch the commands, ban `/tmp`

Rewrite both command files to allocate with `devgeta task scratch` and clean up
with `--clean` at the end. **No frontmatter change** — `devgeta task *` is
already allowed in both; if a step seems to need one, re-read ADR-0015 §3.
Add the `configs/shared/commands/*.md` `/tmp/` ban test.
Note both commands must pass `--clean` the exact path `scratch` printed —
a reconstructed or parent path is refused by design.
Verify: `go test ./internal/apps/opencode/`.

#### Step 11: Docs

`docs/apps/claude.md`: guard section next to its siblings, and extend the
existing "Known limits" with ADR-0014 §1's Bash gap. `CLAUDE.md` §12: a sync
table row for the new pair. `docs/spec.md`: an entry for `dg task scratch` —
CLAUDE.md's "Adding a new command" requires it for anything user-facing, and the
`dge` wrapper makes this reachable by hand.

`docs/migrations/agent-permission-refresh.md`: the upgrade note from step 12,
plus a row in `docs/migrations/README.md`'s table. That README states a guide
appears in the table **only once the change has shipped** — so the row lands
with the release, not with this cycle's first commit.

#### Step 12: Ship it to existing installs

Nothing above reaches a user who already has devgeta. Both agents'
`SoftConfigure` returns at the marker file (`claude.go:167` checks
`settings.json`), so an upgrade re-renders nothing and every existing install
keeps the blanket `.claude/**` deny — worktrees stay broken and OpenCode's
global config stays unprotected.

No automatic migration: re-rendering `settings.json` on upgrade would discard
hand-edits, which is exactly what the marker-file guard exists to prevent, and a
merge of two JSON permission policies is not something to attempt silently.

So this ships as a documented manual step, in `docs/migrations/` alongside
`v1-to-v2.md`:

```bash
dg configure claude --force && dg configure opencode --force
```

State plainly that `--force` overwrites `settings.json` / `opencode.json`, and
that a user with hand-edits should diff first. Link it from the release notes —
without that line the fix ships to new installs only.

---

## 6. Verification Plan

### Automated

```bash
go build ./... && make lint && go test ./... -cover
```

### Manual

Rebuild first — `dg configure` extracts configs from the running binary
(CLAUDE.md §12), so an old binary deploys the old config:

```bash
make build && ./devgeta configure claude --force && ./devgeta configure opencode --force
```

#### In Claude Code

1. `claude --worktree perm-test`, then edit a source file in it → **no prompt**
2. In that worktree, edit `.claude/settings.json` → **denied by the floor**
3. In that worktree, edit `.claude/agents/x.md` → **denied by the guard**
4. From the main checkout, edit `~/.claude/settings.json` → **denied** (today: only prompts)
5. Edit `.mcp.json` → **denied** (today: allowed)
6. `/create-pr` on a throwaway branch → runs to the confirmation with **no scratch prompt and no denied command**
   6b. `rm -rf ~/.cache/devgeta`, then `/create-pr` again → still works, no `dg configure` in between
7. Two `/review-pr` runs in parallel worktrees → distinct scratch dirs, neither body wrong
   7b. `devgeta task scratch --clean` against `/etc`, the scratch root itself, a
   grandchild under an allocated directory, a directory beside the root
   (`~/.cache/devgeta/other`), and a child whose name lacks the allocation
   prefix → each refused, each target still on disk. A sibling **session's**
   directory is **not** in this list: ADR-0015 §3 accepts that case by design,
   so testing for its refusal would encode a contract we did not choose.
   7c. `ln -s "$PWD/.claude/agents" .claude/worktrees/perm-test/link`, edit `link/x.md` → **denied**
8. `DEVGETA_SKIP_AGENT_CONFIG_GUARD=1 claude`, edit `.claude/agents/x.md` → allowed
9. Hide `jq` from the hook, then edit `.claude/settings.json` → **still denied**
   (the floor holds when the guard cannot run). Resolve the binary **before**
   restricting `PATH`, or the shell cannot find it either:

   ```bash
   CLAUDE_BIN="$(command -v claude)"
   env PATH="$(mktemp -d)" "$CLAUDE_BIN"
   ```

   If the CLI will not start with an empty `PATH`, rename the `jq` binary aside
   for the duration instead. `PATH=` alone does not work — it leaves nothing to
   launch.

   9b. **Floor isolation.** `DEVGETA_SKIP_AGENT_CONFIG_GUARD=1 claude`, then one
   path per floor pattern (`.claude/settings.json`, `.claude/hooks/x.sh`,
   `.mcp.json`, `~/.claude/settings.json`, `~/.config/opencode/opencode.json`) →
   **each still denied**. Then `.claude/agents/x.md` → **allowed**. That last one
   is the control: without it, a bypass that silently failed would make every
   other line pass for the wrong reason. This is the only place the floor's
   behavior — as opposed to its presence — is ever observed.

#### In OpenCode

The parity tests prove the two config _files_ agree; they cannot prove OpenCode's
plugin loader fires the guard or that `external_directory` behaves as documented.
This design already got OpenCode's boundary wrong once, so these run for real:

10. With `worktree.location=in-repo` (`dg config get worktree.location`; set it if
    not), `dg wt create perm-test-oc --layout opencode`, then edit a source file
    in the worktree → **no prompt**
11. In that worktree, edit `.opencode/opencode.json` → **denied by the floor**
12. In that worktree, edit `.claude/agents/x.md` → **denied by the guard**
    (proves `agent-config-guard.js` loaded and OpenCode's longest-match resolution
    did not defeat it)
13. `/create-pr` on a throwaway branch → **no `Access external directory` prompt**
    (this is the exact prompt that started this work)
14. `DEVGETA_SKIP_AGENT_CONFIG_GUARD=1 opencode`, edit `.claude/agents/x.md` → allowed
15. Floor isolation in OpenCode — the same five floor paths as check 9b, still
    denied under the same bypass, with `.claude/agents/x.md` allowed as the
    control. OpenCode resolves longest-pattern-first rather than deny-first, so
    a floor that holds in Claude Code proves nothing here.

### Regression

- `dg install --help`, `dg ws`, `dg wt create` unaffected
- `secret-guard` still blocks a staged-secret commit; `suppression-guard` still
  blocks an introduced lint-suppression comment
- `worktree.location=shared` unchanged

### Rollback

This change ships a permission policy that runs on every edit in every repo, so
a bad render or an over-eager guard blocks ordinary work everywhere. Recovery,
in order of cost:

1. **Per-session:** `DEVGETA_SKIP_AGENT_CONFIG_GUARD=1` in the launching shell
   disables the guard without touching any config.
2. **Full revert:** restore `Edit(.claude/**)` / `Edit(.opencode/**)` in both
   templates, drop the guard's hook registration and its `plugin/` file, then
   **`make build` and reinstall before configuring**:

   ```bash
   make build && ./devgeta configure claude --force && ./devgeta configure opencode --force
   ```

   The rebuild is not optional — `dg configure` extracts configs from the
   running binary, so reverting the templates and configuring with the old
   binary redeploys the faulty config and looks like the rollback failed.
   Worktrees go back to being denied: the state before this cycle, not a broken
   one.

Deleting the deployed `~/.claude/agent-config-guard.sh` alone is **not** a
rollback: `settings.json` still registers it, and a missing hook command is an
error on every matching tool call.

---

## 7. Risks & Trade-offs

| Risk                                                 | Likelihood | Mitigation                                                                    |
| ---------------------------------------------------- | ---------- | ----------------------------------------------------------------------------- |
| Guard denies a legitimate edit the floor also covers | Low        | Manual checks 1–5 walk the whole table; env-var bypass documented             |
| Canonicalization differs between the two mirrors     | **Med**    | Step 3 runs the full ADR matrix against both files, not just diffs it         |
| Floor pattern silently no-ops in one agent's matcher | **Med**    | `**/`-prefixed patterns only — both understand `**`; check 2 verifies         |
| A command needs a binary its frontmatter denies      | **Med**    | Step 10 changes no frontmatter by design; check 6 is the proof                |
| `--clean` reaches the root or escapes it             | Low        | Prefixed-direct-child only, after canonicalization; step 9, check 7b          |
| OpenCode's global config left unprotected            | **Med**    | Clauses 3–4 + `Edit(~/.config/opencode/**)` floor; step 3 tests both          |
| OpenCode's extra config sources escape the rule      | Med        | Clauses 3–4 resolve them at runtime; step 3 overrides them                    |
| A relocated `CLAUDE_CONFIG_DIR` is unprotected       | Low        | Out of scope and documented (ADR-0014 §1a) — devgeta-wide, not guard-specific |
| A floor pattern is dead and nothing notices          | Med        | Presence pinned automatically; behavior only via checks 9b/15 (manual)        |
| A hostile path renders invalid JSON                  | Med        | `json.Marshal` before templating; `json.Valid` on both, step 8                |
| Existing installs never receive the change           | **High**   | Step 12 migration doc; `SoftConfigure` stops at the marker by design          |
| A cache wipe breaks both PR commands                 | Med        | Allocation calls `EnsureScratchDir` every time; step 9 + check 6b             |
| OpenCode loads or resolves the guard differently     | **Med**    | Checks 10–14 run it for real; parity tests compare files, not behavior        |
| Two more hand-mirrored files drift                   | Med        | Step 3's behavioral test over both; ADR-0006 accepts this burden              |
| Pinned policy weakened during the swap               | Med        | Steps 4–5 land in one commit — never a commit apart                           |

### Trade-offs Made

- **Guard over a longer deny list** — new code and a fourth mirror pair, bought
  for default-deny against config surface upstream has not shipped yet
  (ADR-0014 §4).
- **Bash writes stay unguarded** — documented limit over a partial matcher that
  invites over-trust (ADR-0014 §5).
- **Vendored skills keep `/tmp`** — one rare prompt over a merge conflict on
  every upstream sync (ADR-0015 §7).
- **A `dg task` subcommand over a frontmatter allowlist** — one more task to
  maintain, bought for zero frontmatter churn now or later and a global
  `rm -rf` deny that stays intact (ADR-0015 §3).

---

## 8. Cross-Model Review Notes

Reviewed twice (GPT-5.6, document-reviewer) against the ADR drafts.

Round 1, four findings folded in: the Bash-bypass overclaim (ADR-0014 §1, §5),
OpenCode's `external_directory` boundary (ADR-0015 §2), scratch-file collisions
across parallel sessions (§3), and shared-vs-per-agent directory creation (§4).

Round 2, one blocking finding: the `mktemp` allocation could not run, because
both command files carry `bash: {"*": "deny", …}` frontmatter that overrides the
global catch-all. Cleanup was worse — `Bash(rm -rf *)` is a global deny no
command-scope allow can lift. Replaced with `devgeta task scratch` (ADR-0015 §3),
which both commands can already invoke and which confines deletion in Go.
Verified separately that the vendored skills in §7 declare no `permission:`
frontmatter, so that section's reasoning is unaffected.
_(Correction, 2026-08-06: the premise above was wrong — command frontmatter is
ignored by both agents, so `mktemp` would have been allowed, not denied; see
`docs/plans/cycles/2026-08-05-shared-command-permissions.md`. The decision to
use `devgeta task scratch` stands on its other merits regardless.)_

Round 11, three findings folded in.

The floor-isolation test was **not executable as written** — `permissions_test.go`
only parses and compares rule strings, there is no permission matcher in the Go
tree, and `DEVGETA_SKIP_AGENT_CONFIG_GUARD` does not reach either agent's
permission engine. Writing a matcher would test a reimplementation of the agents'
glob semantics rather than the agents. Split honestly instead: automated tests
pin that the eight patterns are _present_ in both configs; new manual checks 9b
and 15 are the only place their _behavior_ is observed, each with an
`.claude/agents/x.md`-allowed control so a silently-failed bypass cannot make the
other lines pass for the wrong reason.

Clause 4's base directory for a relative `OPENCODE_CONFIG`: I could not establish
OpenCode's rule — `debug config` does not report whether the file loaded, and
absolute and relative values gave identical output, so the probe cannot
distinguish the cases. Designed around it rather than guessing: canonicalize
against every candidate base and deny on any match.

And the force/first-install lifecycle note moved from the helper's doc comment to
its call sites — a helper cannot describe when its callers invoke it.

Round 10, six findings folded in — and one corrected in the other direction.

The CRITICAL was **half right**, and checking it changed the design. The claim
was that both OpenCode overrides relocate its config/plugin root, so a guard
deployed to `~/.config/opencode/plugin/` would not load. Tested instead of
accepted:

```
$ OPENCODE_CONFIG_DIR=/tmp/oc-alt opencode debug paths
config     /Users/jair.mendez/.config/opencode      # unchanged
```

Both OpenCode variables are **additive**, not relocating — the root does not
move, the plugin still loads, and clauses 3–4 are enforceable. Those stay.

`CLAUDE_CONFIG_DIR` was the real problem and it was worse than reported:
`paths.Paths.Config.Claude` is `GetHomeDir(".claude")`, and `settings.json.tmpl`
hardcodes eight literal `~/.claude/*.sh` hook commands. Under a relocated Claude
root nothing devgeta ships reaches the agent, so a clause promising protection
there was asserting something impossible. Removed, and replaced with §1a stating
the limit and pointing at the separate cycle that would fix it.

Also: clause 4 now resolves relative and symlinked `OPENCODE_CONFIG` values
before comparing; "8 cases" became "the full ADR matrix" in the file table and
risk row; a malformed sentence in step 1 was rewritten as a clause list; and
step 7 now says scratch maintenance runs only on `--force` or first install,
since `SoftConfigure` returns at the marker.

Round 9, seven findings folded in. Two changed the rule itself: relocated config
roots (`CLAUDE_CONFIG_DIR`, `OPENCODE_CONFIG_DIR`, `OPENCODE_CONFIG` — the latter
two verified in the installed OpenCode 1.18.14 binary) escaped every clause, and
the `worktrees` exception was being granted to `.opencode` where nothing creates
worktrees. The rule is now four clauses and deliberately asymmetric. Also: the
floor could never be tested independently, since the guard denies everything it
denies — step 5 now re-runs with the guard bypassed, asserting guard-only paths
flip to allowed so the negative result means something. Plus the stale "six floor
denies" (it is eight), the migrations README index, a missing `make build` in the
rollback that would have redeployed the faulty config and looked like a failed
revert, and a 5-hour estimate for 12 steps.

Round 8, seven findings folded in. The structural one: clause 1's dotted-segment
rule never covered OpenCode's **actual** global config — `GetConfigDir("opencode")`
resolves to `~/.config/opencode/`, with no dotted segment — so the file holding
the permission policy, and the `plugin/` directory holding the guards, were
outside the rule. Verified against `paths.Paths.Config.OpenCode` and the real
directory. Fixed in round 9's clauses 3-4 (resolved roots) plus two `~/`-anchored
floor rules; `~/.opencode/` exists on this machine but is a `bin/` + npm
directory, not config. Also: the test matrix only exercised `.claude` and only
`Edit` (now × `.opencode` × `Write`); `ScratchDir` would have been interpolated
into JSON unescaped, the first user-influenced value either template has ever
carried; existing installs would never have received any of it, since
`SoftConfigure` stops at the marker file (step 12); scratch maintenance was
hanging off `SyncSharedParts` against its documented contract (now a named
helper); `dg task scratch` was missing from `docs/spec.md`; and there was no
rollback procedure.

Round 7, one finding folded in: the plan asserted `--clean` refuses a sibling
session's directory, which ADR-0015 §3 explicitly accepts — check 7b and a risk
row claimed a contract the design never chose. Corrected toward the ADR rather
than toward ownership state, and step 9 now names the accepted case so the next
reader cannot repeat the conflation. The ambiguous phrase "a sibling of the
root" is what caused it; it now reads "a directory beside the root".

Round 6, two findings folded in: every manual check invoked `claude`, so the
cross-agent claim the whole design rests on was never exercised — checks 10–14
now run the source-edit, config-denial, `/create-pr`, and bypass cases in
OpenCode, where the parity tests can only compare files and not behavior. And
check 9's `PATH= claude` could never have run at all, since an empty `PATH`
hides `claude` along with `jq`; it now resolves the binary first.

Round 5, one finding folded in: allocation assumed the scratch root already
existed, contradicting §1's own reason for choosing the cache root — anything
may empty it. `paths.EnsureScratchDir()` is now called by the allocator on every
run, with configure demoted to tightening and pruning (ADR-0015 §1, §4). It sits
in `pkg/paths` because `baseapp` → `tooling/task` → `apps/git` → `baseapp` would
cycle.

Round 4, three findings folded in: `--clean`'s contract was only "inside the
scratch root", which admits deleting the root or a concurrent session's
directory — now an allocated direct child only (ADR-0015 §3, step 9). The eight
canonicalization cases were verified by hand only, and the mirror check merely
asserts both files mention `worktrees` — now a behavioral test over both guards
(step 3), which also extends the JS side past static string parity for the first
time. And scratch logic moved from `cmd/task.go` into `internal/tooling/task`,
matching the `taskRunner` shape every other subcommand uses.

Round 3, two findings folded in: the segment walk had to canonicalize first, or
`.claude/worktrees/../agents/x.md` and a symlink under `worktrees/` would both
pass it — making the guard weaker than the deny it replaces, since Claude Code
resolves symlinks for its own deny rules (ADR-0014 §1, now three more table rows
and a stated fallback). And `MkdirAll` alone cannot tighten a pre-existing
`scratch`, so an unconditional `Chmod` follows it (ADR-0015 §1).

Two questions carried across several rounds are now **decided**, not open:

- **`.opencode/agent/`, `command/`, `skills/` stay guard-only.** Clause 2 denies
  every `.opencode` segment, so they are covered whenever the guard runs. The
  floor stays the short list of paths that are never legitimately agent-edited;
  growing it to mirror the guard would recreate the enumerated-deny list ADR-0014
  §4 rejected, and buy coverage only in the narrow window where `jq` is broken.
- **Both cleanup paths stay.** `--clean` is the normal path and runs per
  invocation; the configure-time prune is the net for a run that was interrupted
  before it. Different jobs, and the prune is cheap. Note step 7's caveat: it
  only fires on `--force` or first install.

Still open:

- [ ] `suppression-guard` fired on this very document for quoting a suppression
      directive in prose. It does not exempt Markdown. Out of scope here; worth
      its own fix.
