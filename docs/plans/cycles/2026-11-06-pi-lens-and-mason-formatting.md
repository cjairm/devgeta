# Cycle: pi-lens, with Pi formatting through the shared Mason script

**Date:** 2026-11-06
**Estimated Duration:** ~4 hours
**Status:** Phase 1 done (steps 1–6); awaiting manual check (§6), then Phase 2 tests

---

## 1. Domain Context

Pi's curated extension set (ADR-0060) is being filled one entry at a time; the
[investigation](../../investigations/2026-11-05-pi-extension-set.md#pi-lens)
leaves `pi-lens` as the last entry. pi-lens adds language-server diagnostics,
but it also formats and autofixes with tools that differ from the Mason ones
Claude Code and OpenCode use, and it downloads anything missing.

Design: [ADR-0063](../../decisions/ADR-0063-pi-formats-through-devgetas-shared-mason-script-and-pi-lens-only-diagnoses.md).
In short: the formatter table becomes one agent-neutral script, Claude's hook
wraps it, a devgeta Pi extension calls it after each edit, and pi-lens is
installed with its own formatting, autofix and downloads off.

## 2. Engineer Context

- `configs/claude/format.sh` — today's Claude hook; holds the per-language
  table, the bounded linter runner, and the grandfathered `hire2` HTML skip.
- `configs/devgeta/` — agent-neutral scripts (`output-budget-run.sh`).
- `internal/apps/baseapp/agentruntime.go` — `EnsureAgentRuntime`, the pattern
  for deploying a shared script to `paths.Paths.Config.Devgeta`.
- `internal/apps/claude/claude.go` (script copy loop, `EnsureAgentRuntime`
  call).
- `internal/apps/pi/pi.go` — `piExtensions`, `configure(force)`,
  `writeFooterOverride` (a devgeta-owned file under
  `~/.pi/agent/extensions/`), `selectTheme` (merge one key into another tool's
  JSON without losing the rest).
- `internal/apps/pi/pi_test.go` — docs-mirror test over `piExtensions`.
- `embedded_test.go` — `TestEmbeddedConfigsNameNoPersonalCheckoutPaths`
  grandfathers `configs/claude/format.sh` for `$HOME/lever`. It must still
  point at the file that holds the skip.
- `agent_config_guard_test.go` `runAgentConfigGuardJSHook` — the pattern for
  running an embedded JS plugin under `node` with a fake host.
- Pi extension API: `pi.on("tool_result", …)`, `pi.exec(cmd, args, {signal,
timeout})`, `isEditToolResult` / `isWriteToolResult`; `edit` and `write`
  inputs carry `path`. Returning `content` drops `structuredContent` unless it
  is returned too.

Targeted tests (`baseapp` has many importers, and `configs/` changes pull in
the root package, so the full suite runs at the end of Phase 2 — say so in
that commit):

```bash
go test ./internal/apps/pi/ ./internal/apps/claude/ ./internal/apps/baseapp/   # while iterating
go test .                                                                     # script + extension tests
go test ./...                                                                 # end of Phase 2
make lint
```

## 3. Objective

After `dg configure pi`, Pi formats and lints every file it edits with the same
Mason binaries and settings as Claude Code, tells the model what changed, and
pi-lens adds diagnostics without formatting, autofixing or downloading
anything.

## 4. Scope Boundary

### In Scope

- [ ] `configs/devgeta/format-file.sh` — the shared script
- [ ] `configs/claude/format.sh` reduced to a wrapper; behavior unchanged for
      Claude, `hire2` skip kept there
- [ ] Deploying the script from both Claude's and Pi's configure
- [ ] `configs/pi/extensions/devgeta-format.js` and its deployment
- [ ] pi-lens config merge (`format.enabled`, `autofix.enabled`)
- [ ] `npm:pi-lens` appended last to `piExtensions`
- [ ] Tests for all of the above, in Phase 2 after the first version has been
      tried by hand; docs (`docs/apps/pi.md`, `docs/apps/claude.md`,
      `docs/guides/agent-sync.md`,
      investigation, ADR-0059 §7 and ADR-0060 §8 amendment notes)

### Explicitly Out of Scope

- Moving OpenCode's `formatter` block onto the script (follow-up, ADR-0063 §7)
- Fixing the missing `eslint_d` / `clang-format` in Mason (investigation's
  "unrelated bug")
- Removing the `hire2` exception (maintainer's decision stands)
- Deleting tools pi-lens downloaded earlier (`~/.pi-lens/tools`, `bin`) —
  documented, not automated
- `pi-schedule`

## 5. Implementation Plan

### File Changes

| Action | File Path                                    | Description                                                                                                                                   |
| ------ | -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Create | `configs/devgeta/format-file.sh`             | Format + lint one path; findings on stdout; always exit 0                                                                                     |
| Modify | `configs/claude/format.sh`                   | Parse hook JSON, `hire2` skip, call script, wrap output                                                                                       |
| Create | `internal/apps/baseapp/formatter.go`         | `EnsureFormatScript()` — copy + chmod to `Config.Devgeta`                                                                                     |
| Modify | `internal/apps/claude/claude.go`             | Call `EnsureFormatScript()`                                                                                                                   |
| Create | `configs/pi/extensions/devgeta-format.js`    | `tool_result` handler; PATH + pi-lens env at load                                                                                             |
| Modify | `internal/apps/pi/pi.go`                     | Deploy script + extension, merge pi-lens config, add `pi-lens`                                                                                |
| Modify | `internal/apps/opencode/permissions_test.go` | Phase 1: `TestClaudeAndOpenCodeFormatterParity` scrapes the `case` table from `format.sh`; point `shPath` at `configs/devgeta/format-file.sh` |
| Modify | `docs/apps/pi.md`, `docs/apps/claude.md`     | New sections; mirror list                                                                                                                     |
| Modify | `docs/guides/agent-sync.md`                  | "Formatting on save" row and "lint feedback loop is Claude-only" (lines 19, 80)                                                               |
| Modify | ADR-0059, ADR-0060, investigation            | Amendment notes on §7 / §8; mark pi-lens done                                                                                                 |
| Modify | `internal/apps/pi/pi_test.go`                | Phase 2: config merge, deployment, order, docs mirror                                                                                         |
| Create | `format_file_test.go` (root)                 | Phase 2: script behavior with fake Mason bins                                                                                                 |
| Create | `pi_format_extension_test.go` (root)         | Phase 2: extension under `node` with a fake Pi host                                                                                           |

### Step-by-Step

**Phase 1 builds a first working version with no new tests** (steps 1–6).
The maintainer tries it by hand (§6), adjusts, and only then does Phase 2
(step 7) add tests, so the tests capture the behavior that was kept. Each Phase 1 step's
check is `go build ./...` plus reading the result. The one existing-test
edit in Phase 1 (`permissions_test.go`) is there to keep the current suite
from going red, not to add coverage. Phase 1 steps are committed as they
land, after the targeted run of the existing tests for what changed. The full
suite runs once, at the end of Phase 2.

#### Step 1: Extract the shared script

- Move the `case` table, `fmt`, `lint`, `lint_go`, `run_bounded` and the
  timeout lookup into `configs/devgeta/format-file.sh`. Usage:
  `format-file.sh <path>`. A missing or non-file path exits 0 silently.
  Findings print as `[<linter>]` blocks on stdout. Nothing else goes to
  stdout.
- Mason directory: `MASON_BIN="${XDG_DATA_HOME:-$HOME/.local/share}/nvim/mason/bin"`.
  Note: `format.sh` hard-codes `$HOME/.local/share`. Honoring
  `XDG_DATA_HOME` matches where Mason actually installs (Neovim's `stdpath("data")`).
- No `hire2` case in this file.
- Verify: `bash -n configs/devgeta/format-file.sh`.

#### Step 2: Reduce `format.sh` to a wrapper

- Read `.tool_input.file_path` from the hook JSON (unchanged). Apply the
  `hire2` HTML skip. Call `~/.config/devgeta/format-file.sh "$FILE"`. When it
  prints findings, emit the existing `hookSpecificOutput.additionalContext`
  JSON with the same `Linter findings for $FILE — please fix:` prefix.
- If the shared script is missing, exit 0, as the hook does today when Mason
  is missing.
- In `internal/apps/opencode/permissions_test.go`, point
  `TestClaudeAndOpenCodeFormatterParity`'s `shPath` at
  `configs/devgeta/format-file.sh` and update its comment. It reads the `case`
  table, which now lives there. Without this the existing suite goes red.
- Verify: `bash -n configs/claude/format.sh`;
  `go test -run TestClaudeAndOpenCodeFormatterParity ./internal/apps/opencode/`.

#### Step 3: Deploy the script

- `baseapp.EnsureFormatScript()`: `MkdirAll(Config.Devgeta)`, `CopyFile`,
  `Chmod 0o755`, following `EnsureAgentRuntime`. Called from Claude's
  configure (next to `EnsureAgentRuntime`) and Pi's configure.
- Verify: `go build ./...`.

#### Step 4: The Pi extension

`configs/pi/extensions/devgeta-format.js`, an ES module exporting
`default function (pi)`:

- **At load:** if the Mason dir exists and is not already on `PATH`, prepend
  it to `process.env.PATH`. Set `PI_LENS_DISABLE_TOOL_INSTALL` and
  `PI_LENS_DISABLE_LSP_INSTALL` to `"1"` only when unset.
- **`tool_result`:** only for `edit`/`write` with `isError` false. Resolve
  `event.input.path` against `ctx.cwd`. Read the bytes, run
  `pi.exec(<home>/.config/devgeta/format-file.sh, [abs], {signal: ctx.signal,
timeout: 60000})`, read the bytes again. Resolve `~/.config` with
  `XDG_CONFIG_HOME` the same way `paths.Paths.Config.Devgeta` does. Check this
  in `pkg/paths` before writing it.
- **What it adds** (only when there is something to say):
  - Changed bytes: `Formatted by devgeta: lines N–M changed. They now read:`
    followed by those lines, capped at 60 lines / 6,000 bytes. Over the cap:
    `…reformatted beyond what fits here; read the file before editing it.`
    The span is the first to last differing line (plain prefix/suffix
    comparison, no diff library).
  - Findings: `Linter findings for <path> — please fix:` + the script's stdout.
- Returns `{ content: [...event.content, {type: "text", text}],
structuredContent: event.structuredContent }`. Returns nothing when there is
  nothing to add. Any failure (missing script, exec error, timeout) returns
  nothing. It never sets `isError` or fails the edit.
- Mason path constant identical to the script's (ADR-0063 §6; a test is
  added in Step 7).
- Verify: `node --check configs/pi/extensions/devgeta-format.js`.

#### Step 5: Pi configure wiring

- Always write `~/.pi/agent/extensions/devgeta-format.js` (atomic), soft and
  force. Write it before extensions install so the first Pi start after
  configure already has it.
- `piLensConfigPath()`: `PI_LENS_CONFIG_PATH` if set, else
  `~/.pi-lens/config.json`. `PI_CODING_AGENT_DIR` is not followed, for the
  same reason devgeta ignores it everywhere else (ADR-0059 §5). A relocated
  Pi would also not load the extension, which goes to the default agent dir.
- `disablePiLensMutations(force)`: same merge approach as `selectTheme` (raw
  JSON preserved, HTML escaping off, unparseable file is an error). Set
  `format.enabled` / `autofix.enabled` to `false`, keeping sibling keys inside
  `format` / `autofix`. Without force, an existing `enabled` value stays.
- Append `"npm:pi-lens"` last in `piExtensions` and in `docs/apps/pi.md`.
- Order in `configure`: theme → selectTheme → footer → format script →
  extension file → pi-lens config → extensions → register.
- Verify: `go build ./...`. The existing docs-mirror test in `pi_test.go`
  must still pass: `go test ./internal/apps/pi/`.

#### Step 6: Docs

- `docs/apps/pi.md`: pi-lens entry; "Formatting" section (what runs, Mason as
  the only source, the PATH side effect, how to opt back into downloads by
  exporting the switches as `0`, deleting `~/.pi-lens/tools` and `~/.pi-lens/bin`
  to stop old downloads winning, that the extension file is rewritten on every
  configure).
- `docs/apps/claude.md`: hook now delegates to the shared script.
- `docs/guides/agent-sync.md`: the "Formatting on save" row (line 19) names
  the shared script behind Claude's wrapper. The "lint feedback loop is
  Claude-only" note (line 80) now says Claude and Pi.
- Amendment notes: ADR-0059 §7 and ADR-0060 §8 → ADR-0063. Investigation:
  mark pi-lens done.

**End of Phase 1.** The maintainer runs §6's manual checks and adjusts.

#### Step 7 (Phase 2): Tests

Written against the behavior kept after the manual run.

- `format_file_test.go` (root): fake `mason/bin` in `t.TempDir()` (stub
  `gofumpt` that rewrites the file, stub `flake8` that prints a finding, stub
  linter that sleeps past a shortened timeout). Assert: in-place change,
  findings on stdout under `[flake8]`, silence on timeout, exit 0 always,
  nothing on stdout for an unknown extension. `XDG_DATA_HOME` and `HOME`
  pointed at temp dirs.
- `format.sh` wrapper: findings → valid JSON with the old prefix. No findings
  → no output. `hire2` path → script not called.
- `pi_format_extension_test.go` (root): skip without `node`. Driver imports
  the embedded extension with a fake `pi` (`on`, `exec` backed by a stub
  script). Assert: changed span reported; cap message; no-change →
  `undefined`; `isError` result untouched; non-edit tool untouched; env set
  only when unset; PATH prepend not duplicated; `structuredContent` preserved.
- Mason path in the extension equals the script's.
- `pi_test.go`: pi-lens config merge (soft keeps, force sets, siblings kept,
  unparseable file errors), extension file deployed, `pi-lens` last.
- Then the full suite and lint (§6). If the manual read-guard check failed,
  amend ADR-0063 before writing these tests.

## 6. Verification Plan

### Automated Verification (Phase 2)

```bash
go test ./...      # baseapp + configs/ changes: full suite, see §2
make lint
```

### Manual Verification (end of Phase 1; maintainer runs, on the real machine)

Needs `make build`, then `dg configure claude --force` and
`dg configure pi --force`. Both write to `~/.claude`, `~/.config/devgeta`,
`~/.pi`, `~/.pi-lens`, so ask before running them.

1. Claude: edit a Go file with bad formatting → it is reformatted; a Python
   file with a flake8 issue → finding reaches the model. (Same as before.)
2. Pi, in a scratch Go repo: ask for an edit that leaves bad formatting →
   result shows `Formatted by devgeta: lines …`.
3. **The read-guard check (ADR-0063 risk):** in the same file, ask for a second
   edit right after the reformat. pi-lens must not block it as "File modified
   since read/write".
4. pi-lens diagnostics appear (e.g. a Go type error from gopls), and
   `~/.pi-lens/tools` gains nothing new during the session.
5. `~/.pi-lens/config.json` has `format.enabled: false` and
   `autofix.enabled: false`, and any other keys it had before are still there.

### Regression Check

- Claude hook output unchanged for the same inputs (manual step 1; Step 7 test).
- `dg configure pi` (soft) twice in a row: second run installs nothing,
  rewrites only the extension file.
- OpenCode's config is untouched. Its parity test changes only to read the
  `case` table from the shared script (Step 2):
  `go test ./internal/apps/opencode/`.

## 7. Risks & Trade-offs

| Risk                                                       | Likelihood | Mitigation                                                                                                                      |
| ---------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------- |
| pi-lens read guard blocks the edit after a reformat        | Med        | Manual step 3 before Phase 2; amend ADR-0063 if it fails. Fallback: report the formatted lines and let the guard ask for a read |
| pi-lens's native build fails during configure              | Med        | Installed last; failure names it; earlier extensions stay                                                                       |
| Old pi-lens downloads keep winning over Mason              | Med        | Documented cleanup; not automated (user data)                                                                                   |
| Pi changes `tool_result` / load order                      | Low        | Phase 2 behavior test pins our side; load order rechecked when Pi bumps a major                                                 |
| `XDG_DATA_HOME` change alters where Claude looks for Mason | Low        | Matches Neovim's own `stdpath("data")`; same result when unset                                                                  |

### Trade-offs Made

- **Immediate formatting over pi-lens's turn-end formatting:** same timing as
  the other agents, so the line-span report is required.
- **Extension file rewritten on every configure:** devgeta code, not user
  config; users customize with their own extension file.
- **No downloads:** consistency over coverage for languages Mason lacks.

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope locked?
- [ ] Steps actionable?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**
