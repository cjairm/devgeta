# Cycle: Re-scope the worktree redirects and fix the deny wording

**Date:** 2026-07-29
**Estimated Duration:** ~3 hours
**Status:** Done

---

## 1. Domain Context

Devgeta installs a **PreToolUse hook** into both AI coders — a small script that
runs before every terminal command the agent tries to run, and can allow it or
block it. The relevant one is `configs/claude/task-redirect.sh` and its OpenCode
twin `configs/opencode/plugin/task-redirect.js`. When an agent types a raw git or
`gh` command that has a better `devgeta task` equivalent, the hook denies it
(exit 2 / throw) and prints the replacement to run instead.

These hooks deploy to the user's **global** config (`~/.claude/`,
`~/.config/opencode/`), so every rule fires in **every repository on the
machine** unless it is deliberately gated.

[ADR-0006](../../decisions/ADR-0006-hook-guardrails-scope-and-sharing.md)
(ACCEPTED, 2026-07-29) fixed the test for deciding that gating:

> is this a better/compressed form of a universal operation (global), or does it
> encode _devgeta's own_ policy that would be wrong to impose on someone else's
> project (devgeta-repo-only)?

Two rules predate that ADR and were never re-checked against it. This cycle
applies the existing test to them, and fixes the bypass wording alongside.

**Origin:** a real breakage. A separate repository (`flux`) runs a multi-command
AI workflow suite that manages its own worktrees, in `<repo>/.claude/worktrees/`.
Devgeta's global hook denies its worktree cleanup and fan-out commands.
Investigation showed devgeta was imposing its own worktree layout on a repo that
has a different one — the exact failure mode ADR-0006 was written to prevent.

---

## 2. Engineer Context

Read these before touching code:

| What                                     | Where                                                                        |
| ---------------------------------------- | ---------------------------------------------------------------------------- |
| The hook and its rule table              | `configs/claude/task-redirect.sh`                                            |
| The OpenCode twin (must stay in sync)    | `configs/opencode/plugin/task-redirect.js`                                   |
| Shared bash helpers (from ADR-0006)      | `configs/claude/lib/segments.sh`, `configs/claude/lib/devgeta-repo.sh`       |
| Scoping test                             | [ADR-0006](../../decisions/ADR-0006-hook-guardrails-scope-and-sharing.md)    |
| Two-agent sync rule                      | CLAUDE.md § "Keeping the two AI agents in sync"                              |
| Hook contract docs                       | `docs/apps/claude.md` ("Command redirect (PreToolUse hook)"), `docs/spec.md` |
| Behavioral tests (the real parity proof) | `task_redirect_test.go`, `task-redirect.test.mjs`                            |

**Where devgeta puts worktrees** (`internal/tooling/worktree/worktree.go:132`):

```
~/.local/share/devgeta/worktrees/<repo-slug>/<flat-name>
```

`GetWorktreeBasePath()` (`worktree.go:137`) returns that shared root, and
`WorktreeManager.List()` (`worktree.go:458-533`) enumerates every repo's
worktrees by scanning **only** that root.

### Prerequisite: rebase on the guardrails cycle

[2026-07-29-hook-guardrails.md](2026-07-29-hook-guardrails.md) is **In Progress**
and owns `task-redirect.sh`, `task-redirect.js`, `task_redirect_test.go`, and the
`configs/claude/lib/` helpers. Its work is **uncommitted in the working tree**
and **not yet deployed** to `~/.claude/`. This cycle starts from that state and
must reuse `lib/segments.sh` / `lib/devgeta-repo.sh` rather than duplicating
either — CLAUDE.md's DRY rule.

**That cycle already closed the `git -C` / `gh -R` anchor gap.** It added
`DEVGETA_GIT_GLOBAL_OPT` and `DEVGETA_GH_GLOBAL_OPT` to `lib/segments.sh` and
folded them into `GIT_ANCHOR` / `GH_ANCHOR` (`task-redirect.sh:105-111`), with
`GIT_PREFIX` / `GH_PREFIX` on the JS side. Verified against the repo copy:
`git -C . worktree add`, `git -C . diff main..HEAD`, and `gh -R o/r pr checks 1`
all deny. There is **no anchor work left in this cycle** — only regression
verification (step 5).

---

## 3. Objective

Stop devgeta's global hook from breaking repositories that manage worktrees their
own way, and make its bypass instructions actionable.

Two defects:

1. **The two worktree rules are mis-scoped as global.** `worktree-start` /
   `worktree-finish` redirect to `devgeta task`, which creates worktrees in
   devgeta's own shared root. That root **is** devgeta's convention. By
   ADR-0006's test these rules encode devgeta's own policy and must be
   devgeta-repo-only.
2. **The deny message names an action the reader cannot take.** Every deny ends
   with `set DEVGETA_SKIP_TASK_REDIRECT=1 to bypass this session`. The hook reads
   its **own process environment**, fixed before the command runs — so an agent
   exporting the variable inside a command has no effect, and it cannot fix this
   mid-session. Observed consequences: agents prefix unrelated commands with a
   useless `export`, and a reviewer concluded the only remaining option was
   hardcoding a repository name into the hook.

---

## 4. Scope Boundary

### In Scope

- Gate `worktree-start` and `worktree-finish` to the devgeta repo via the
  existing `is_devgeta_repo` / `isDevgetaRepo` helpers.
- Reword the bypass hint to name **who** sets the variable and **where** — in
  **all three hook pairs**, not just the redirects. `task-redirect`,
  `secret-guard`, and `suppression-guard` carry the same defective phrasing in six
  places (`configs/claude/{task-redirect,secret-guard,suppression-guard}.sh` and
  the three matching `configs/opencode/plugin/*.js`), so fixing only the redirects
  would leave the other two telling readers to do something that has no effect.
  Confirmed in practice: `secret-guard` blocked an agent from committing the
  guardrails cycle's own test fixtures (a placeholder RSA key in
  `secret_guard_test.go:185` and `secret-guard.test.mjs`), and the agent could not
  act on the printed bypass because the variable must already be in the hook's
  environment. Nothing is wrong with the detection — the guard did its job — the
  message just names an action the reader can't take.
- A parity test asserting the two hooks carry the same deny-rule set and the same
  bypass text (see step 5 — no such test exists today). Landed as two tests: a
  deny-message-set comparison scoped to task-redirect (the only pair with a
  comparable "list of redirect messages"), and a separate bypass-text-only
  comparison spanning all three hook pairs.
- Regression-verify the anchor coverage the guardrails cycle already added.
- Mirror every change across both agents, with cases in `task_redirect_test.go`
  **and** `task-redirect.test.mjs`.
- Update `docs/apps/claude.md` and `docs/spec.md` scope tables.

### Explicitly Out of Scope

- **Extending the git/gh anchors.** Already done by the guardrails cycle.
- **Switching the redirects to `permissionDecision: "ask"`.** Investigated and
  rejected in
  [ADR-0007](../../decisions/ADR-0007-task-redirects-stay-hard-deny.md) — the
  mechanism stays exit 2 / throw in both agents, so there is no work here. See §A
  below.
- **Changing where devgeta stores worktrees.** Not approved and not owned by any
  document — see §B below. This cycle fixes the breakage without it.
- The `gh` redirects (`pr-checks`, `review-threads`, `submit-review`) and
  `review-package`. These stay global: they compress the output of a universal
  operation and impose no layout.
- `GetWorktreeDir()` (`worktree.go:951`) — returns `.worktrees`, marked
  deprecated, zero non-test callers. Dead code; delete in a separate cleanup.

**Scope is locked.** Anything else discovered goes to a future cycle.

---

## 5. Implementation Plan

### File Changes

| File                                       | Change                                             |
| ------------------------------------------ | -------------------------------------------------- |
| `configs/claude/task-redirect.sh`          | Gate 2 rules; reword `BYPASS_HINT`; header comment |
| `configs/opencode/plugin/task-redirect.js` | Same, mirrored                                     |
| `task_redirect_test.go`                    | Scope cases, wording case, parity test             |
| `task-redirect.test.mjs`                   | Same cases, JS side                                |
| `docs/apps/claude.md`                      | Scope table + bypass section                       |
| `docs/spec.md`                             | Redirect summary                                   |

### Step-by-Step

#### Step 1: Gate the two worktree rules in `task-redirect.sh`

- In `check_segment`, add `&& is_devgeta_repo` to the `worktree add` and
  `worktree remove` rules, matching the two release rules.
- Keep the gate **last** in the condition so the go.mod walk only runs after a
  pattern matches.
- Move both rules from the global list to the devgeta-repo-only list in the
  file-header "Rule scope" comment, stating why (devgeta's worktree root is
  devgeta's own layout).
- Expected outcome: bare `git worktree add` denies inside devgeta, allows outside.
- Verify:
  ```bash
  printf '{"cwd":"%s","tool_input":{"command":"git worktree add wt"}}' "$PWD" | bash configs/claude/task-redirect.sh; echo "exit=$? (want 2)"
  printf '{"cwd":"/tmp","tool_input":{"command":"git worktree add wt"}}' | bash configs/claude/task-redirect.sh; echo "exit=$? (want 0)"
  ```

#### Step 2: Mirror the gate in `task-redirect.js`

- Same two rules, gated with `isDevgetaRepo(projectDir)`; same header comment
  update.
- Expected outcome: JS hook matches bash exit-for-exit on both cases above.
- Verify: `node --test task-redirect.test.mjs`

#### Step 3: Reword the bypass hint in both files

Replace the `BYPASS_HINT` constant. It must say who acts and where:

```
bypass: export DEVGETA_SKIP_TASK_REDIRECT=1 in the shell that launches this
agent (e.g. the repo's .envrc), not inside the command — this hook reads its
own environment
```

Do **not** add an in-command bypass marker: a magic word inside the command
string would let any command switch the guard off by accident.

- Expected outcome: both hooks emit the new text; neither mentions "this session".
- Verify:
  ```bash
  printf '{"cwd":"/tmp","tool_input":{"command":"gh pr checks 1"}}' | bash configs/claude/task-redirect.sh 2>&1 | rg -q 'shell that launches' && echo OK
  ```

#### Step 4: Tests for steps 1-3, both suites

Add to `task_redirect_test.go` and `task-redirect.test.mjs`:

- bare `git worktree add` / `worktree remove` in devgeta → denied
- the same two **outside** devgeta → allowed
- `gh pr checks`, `git diff a..b` outside devgeta → still denied (global
  unchanged)
- deny text contains the new wording
- Expected outcome: new cases fail before steps 1-3, pass after.
- Verify: `go test ./... && node --test task-redirect.test.mjs`

#### Step 5: Add a deny-message and bypass-text parity test (and regression-verify the anchors)

`internal/apps/opencode/permissions_test.go` does **not** cover the hooks at all:
it asserts permission-list parity (`TestClaudeAndOpenCodePermissionParity`) and
formatter extension parity (`TestClaudeAndOpenCodeFormatterParity`) only.

- Add a test over the embedded configs FS asserting `task-redirect.sh` and
  `task-redirect.js` contain the same **set of deny messages** and the same
  **bypass text**.
- Scope this claim honestly. A set comparison catches the most likely drift — a
  rule added to one agent and not the other, or the bypass wording fixed in one
  file — and nothing enforces even that today. It does **not** prove the rule
  tables are equivalent: it cannot detect duplicate rules, rule ordering
  differences, divergent match patterns behind identical messages, or one side's
  rule being gated to the devgeta repo while the other stays global. Those
  remain covered only by the behavioral cases in `task_redirect_test.go` and
  `task-redirect.test.mjs`, which must be added in pairs.
- Bypass text is shared across all three hook pairs (task-redirect, secret-guard,
  suppression-guard — see the In Scope section above), so the bypass-text half of
  this parity test covers all three pairs, table-driven; the deny-message-set
  half stays task-redirect-only, since the other two pairs don't have a
  comparable "list of redirect messages" to compare.
- A stronger option, **not** in scope here: drive both suites from one shared
  declarative fixture (command in, expected decision out) so pattern and scope
  drift fail the build too. Worth its own cycle if rule drift ever actually bites.
- Regression-verify the anchor coverage the guardrails cycle added — assert, do
  not re-implement:
  ```bash
  for c in 'git -C . worktree add wt' 'git -C . diff main..HEAD' 'gh -R o/r pr checks 1'; do
    printf '{"cwd":"%s","tool_input":{"command":"%s"}}' "$PWD" "$c" | bash configs/claude/task-redirect.sh >/dev/null 2>&1
    echo "$c -> exit=$? (want 2)"
  done
  ```
- Expected outcome: parity test passes; all three `-C`/`-R` forms still deny.
- Verify: `go test ./...`

#### Step 6: Update docs

- `docs/apps/claude.md`: move the two worktree rules to the devgeta-repo-only
  side of the scope table; rewrite the bypass section to match step 3.
- `docs/spec.md`: same correction in the redirect summary.
- Expected outcome: no doc still describes the worktree rules as global.
- Verify: `rg -n 'worktree-start|worktree-finish' docs/apps/claude.md docs/spec.md`

#### Step 7: Rebuild, redeploy, verify end to end

`configure` extracts configs from the **running binary**, so an old binary
silently deploys the old hook. `dg` is an alias for `devgeta`, which resolves to
the **installed** binary on `PATH` (`~/.local/bin/devgeta`) — not the one
`make build` just produced (`./devgeta` in the repo root). Run the freshly built
one explicitly:

```bash
make build                              # produces ./devgeta (Makefile:60)
./devgeta configure claude --force      # NOT `dg configure` — see the warning below
./devgeta configure opencode --force
./devgeta --version                     # confirm it's the build you just made
```

Install to `PATH` only once the change is verified, so `dg` keeps pointing at a
known-good binary while testing.

- Expected outcome: the two originally-broken worktree call sites in the other
  repository (a bare `git worktree remove` in its cleanup command, a bare `git
worktree add` in its fan-out script — see §8) now pass, since neither is the
  devgeta repo. `gh pr review` is **not** fixed by this cycle and is expected
  to still deny there — `submit-review` stays global by explicit scope
  decision (see "Explicitly Out of Scope" above), so the bypass export is the
  intended answer for that one, not a further code change. Devgeta's own
  redirects still fire in devgeta.
- Verify: the commands above, then re-run the manual matrix in §6 against the
  **deployed** `~/.claude/task-redirect.sh`.

> **Warning for this machine:** the deployed `~/.claude/settings.json` carries
> company telemetry settings devgeta's template does not have, and devgeta
> re-renders that file wholesale. `dg configure claude --force` will drop them.
> The operator handles this manually — confirm with them before running it.

---

## 6. Verification Plan

### Automated Verification

```bash
go test ./...                       # task_redirect_test.go + the new parity test
node --test task-redirect.test.mjs  # OpenCode twin
make lint
```

Parity between the two hooks is proven by the two behavioral suites above plus
the new parity test from step 5 — **not** by `permissions_test.go`.

### Manual Verification

Exit 2 is deny, 0 is allow:

```bash
H=configs/claude/task-redirect.sh
p() { printf '{"cwd":"%s","tool_input":{"command":"%s"}}' "$1" "$2" | bash "$H"; echo "exit=$?"; }

# in devgeta: worktree rules fire, in both flag forms
p "$PWD" 'git worktree add wt'                 # 2
p "$PWD" 'git -C . worktree add wt'            # 2

# outside devgeta: worktree rules stay silent
p /tmp 'git worktree add wt'                   # 0
p /tmp 'git worktree remove wt'                # 0

# global rules unchanged everywhere
p /tmp 'gh pr checks 1'                        # 2
p /tmp 'git diff main..HEAD'                   # 2
```

### Regression Check

- `dg wt create` / `list` / `remove` / `prune` still work in the devgeta repo
- redirects still fire in the devgeta repo (the feature's main use)
- `dg ws` dashboard still lists worktrees

---

## 7. Risks & Trade-offs

| Risk                                                    | Mitigation                                                    |
| ------------------------------------------------------- | ------------------------------------------------------------- |
| Merge conflict with the in-progress guardrails cycle    | Land after it; reuse `lib/` helpers; anchors are already done |
| Changing only one agent                                 | Step 5's new parity test — nothing enforced this before       |
| Gating too much, silencing rules inside devgeta itself  | §6 asserts both directions, inside and outside the repo       |
| `dg configure claude --force` wiping operator telemetry | Called out in step 7; operator confirms first                 |

### Honest limitation

These hooks match command **text** with pattern matching. That is a heuristic and
will never be complete — the file's own header says it "is not a shell parser."
Acceptable **only** because these rules steer toward better output. Nothing here
is a security control, and nothing that matters for safety may depend on it.

---

## Settled and follow-up

### A. Hard-deny vs `permissionDecision: "ask"` — SETTLED, no work

**Decision: keep hard-deny in both agents.** Recorded in
[ADR-0007](../../decisions/ADR-0007-task-redirects-stay-hard-deny.md).

`"ask"` was investigated and rejected on evidence: OpenCode's only
`"ask"`-capable hook (`permission.ask`) does not fire for auto-allowed bash, and
devgeta's OpenCode template allows bash broadly — so it would never see the
traffic the redirect intercepts. Where `"ask"` does apply, it converts a hard
break into an indefinite stall for unattended runs, which is worse for the
automated-workflow case that motivated this cycle. See the ADR for the probe
results and the revisit conditions.

Consequence for this cycle: **no change.** The mechanism stays exit 2 / throw and
the existing tests keep asserting it. The ADR also raises the stakes on scope
correctness, which is exactly what steps 1-2 fix.

### B. Should devgeta adopt Claude Code's worktree location?

Claude Code's native worktree tool uses `<repo>/.claude/worktrees/<name>`
(confirmed in flux's `scripts/claude-hooks/worktree-create.sh:14`: "the location
the native EnterWorktree tool uses"). Devgeta uses
`~/.local/share/devgeta/worktrees/<repo-slug>/<name>`. A user of both ends up
with two separate sets of worktrees.

**For:** aligns with a tool devgeta already configures; "prefer existing over
new."

**Against, and this blocks making it the default:** worktrees **inside** the
user's repository litter their working tree unless their `.gitignore` covers
`.claude/worktrees/`. Devgeta cannot edit other people's `.gitignore`, so
switching the default would make `git status` noisy in repos that never opted in.
Devgeta also treats Claude Code and OpenCode symmetrically, and adopting one
agent's convention tilts that. And the shared root is what lets `dg wt list` /
`dg ws` scan every repo at once: `List()` (`worktree.go:458-533`) reads only that
root, so per-repo folders require layout-aware enumeration (possible via the
recent-repos store from ADR-0002, but not free).

**Status: an option to evaluate, not an approved direction.** Nothing here is
committed, nothing is scheduled, and no document owns it. Recording the analysis
so it isn't re-derived — not as a plan.

If it is ever pursued, the shape worth evaluating first is a **setting** with the
current shared root as default (both conventions work, nobody is forced to
migrate) rather than switching the default outright, since switching would be a
breaking path change (CLAUDE.md §10) and a **v2.0.0** major bump. Any version of
this needs its own ADR before implementation, and that ADR would have to cover
four things this cycle does not touch: path selection, layout-aware enumeration
in `List()`, how the layout is configured, and migration behavior.

[docs/migrations/v1-to-v2.md](../../migrations/v1-to-v2.md) sketches what the
migration would say. It is unpublished, unusable, and blocked on all of the
above; delete it if the direction is dropped.

---

## 8. Cross-Model Review Notes

Findings recorded so they are not re-litigated:

- A proposal to add an `is_flux_repo` guard naming a specific repository was
  **rejected**: CLAUDE.md §3.8 forbids features whose value exists only for one
  narrow case. The general gate already existed (`is_devgeta_repo`).
- **For the guardrails cycle, not this one:** both new guards fired on
  self-referential content while committing that cycle's own work.
  `secret-guard` matched the placeholder RSA key in its own test fixtures, and
  `suppression-guard` matched a commit message that merely _named_ a
  suppression directive in prose. Detection is working correctly in both cases —
  the content really does contain the signature — but writing about a directive
  and introducing one are different acts, and neither guard can currently tell
  them apart. Decide there whether that is acceptable (bypass is the intended
  human-in-the-loop answer) or whether the suppression check should look at code
  context rather than any occurrence. Recorded here only because it is the
  evidence behind widening the bypass-wording fix to all three hook pairs.
- The claim that the hook blocks flux's worktree **creation** was false — that
  call site uses `git -C`, which the **deployed** hook misses. The genuinely
  blocked sites are a bare `git worktree remove` in a cleanup command, a bare
  `git worktree add` in a fan-out script, and `gh pr review`.
- **Correction:** the first draft of this cycle planned to close that `-C` gap.
  It was already closed in the working tree by the guardrails cycle; the earlier
  evidence came from testing the **deployed** `~/.claude/task-redirect.sh`, which
  is the pre-guardrails copy. Always test `configs/claude/task-redirect.sh` when
  reasoning about current behavior, and the deployed copy only when reasoning
  about what the operator is running right now.
- **Correction:** the first draft claimed `permissions_test.go` fails the build on
  hook asymmetry. It does not — it covers permission lists and formatter
  extensions only. Hence step 5.
- `.envrc.local` is the wrong place for a repo-wide opt-out: it is gitignored, so
  `git worktree add` never copies it into new worktrees. A repo's **tracked**
  `.envrc` propagates. Caveat: direnv exports on shell init or `cd`, so a
  `make init && agent` chain in a brand-new worktree may launch before the export
  lands.
