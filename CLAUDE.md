# CLAUDE.md — Devgeta Development Guide

⚠️ **DOCUMENTATION CURRENCY:** This file is the source of truth for development practices. Keep it up to date as patterns and decisions evolve. Stale documentation is worse than no documentation—it misleads contributors. If you change how we do something, update this file and/or linked files in the same PR.

---

## 1. What this is

Devgeta is a cross-platform development environment manager that automates installation and configuration of terminal tools, programming language runtimes, database systems, and desktop applications on macOS and Debian/Ubuntu.

**Core functionality:**

- **Installation automation** — `dg install` with interactive category selection
- **Category-based setup** — terminal tools, languages, databases, desktop apps
- **Cross-platform support** — Single command syntax works on macOS and Linux
- **Smart state tracking** — `global_config.yaml` tracks what was installed by devgeta
- **Configuration templates** — Embedded configs applied consistently across machines
- **Idempotent operations** — Safe to re-run; detects existing packages

For planned features and roadmap, see [ROADMAP.md](ROADMAP.md)

---

## 2. Source of truth

Read these **in order** before starting work:

| File                    | Governs                                                                  |
| ----------------------- | ------------------------------------------------------------------------ |
| `docs/spec.md`          | What features exist, how they work, edge cases, testing strategy         |
| `CLAUDE.md` (this file) | Development practices, tech stack, architecture patterns, code standards |
| `docs/decisions/`       | Individual architectural decisions and their rationale                   |
| `docs/plans/cycles/`    | Current cycle scope and priorities (always check if a cycle is active)   |
| `CONTRIBUTING.md`       | Setup, build, test, and release workflows                                |
| `ROADMAP.md`            | Planned features, future commands, open questions                        |

---

## 3. Product principles

1. **Zero-dependency installer** — Installation happens via shell script alone; no pre-installed tools required beyond bash/curl
2. **Idempotent operations** — Running `dg install` twice produces the same result; safe to re-run
3. **Cross-platform consistency** — Same command syntax and behavior on macOS and Linux; platform differences are transparent to users
4. **Configuration persistence** — User edits to configs are never overwritten; new installs preserve existing customizations
5. **Modular architecture** — Each app is independent; failures in one app don't cascade to others
6. **Transparent state** — All installation state tracked in `~/.config/devgeta/global_config.yaml`; users can inspect what was installed
7. **Visual consistency** — Alacritty, tmux, Neovim, and the AI-coder configs share one palette (Gruvbox dark) and a transparency convention; a color/theme change in one must be mirrored in the others. See [docs/guides/theming.md](docs/guides/theming.md)
8. **Everything general, never bespoke** — Every feature devgeta ships — commands, installers, app modules, configs, TUIs, `dg task` subcommands, hooks, plugins, aliases — is built to be general-purpose and reusable by anyone. We never add a custom, one-off feature that only serves a single person, repo, or situation. This protects every user's experience: what ships has to work for all of them, not just whoever asked for it. (Opinionated _defaults_ are not "custom" and are expected — a curated tool set, the Gruvbox palette, ready-made app configs — because those are a general setup anyone can adopt; what's forbidden is a feature whose value exists only for one narrow case.) Where devgeta's own internal process is unavoidably specific (e.g. the §9 release flow), gate it so it never imposes itself on a user's environment — see the `release` redirect's `go.mod` gate in `configs/claude/task-redirect.sh` — never ship it as a global default. When a change would only help one narrow case: generalize it, or don't build it.

   **This applies to every change, not just new features.** A bug fix, a refactor, a doc tweak, a one-line default, a hook edit — each one is subject to the same test: _would this still make sense to someone who has never seen this repo?_ The rule runs in both directions, and the second is the one that gets missed: as well as not building one-off features for one user, never bend a shipped artifact to carry a devgeta-specific decision. Our test policy, our release chain, our branch conventions are **project law, not product** — they belong in `CLAUDE.md` and `docs/`, never inside something a stranger installs. The trap that keeps catching us is `configs/shared/`; see [§12](#anything-we-ship-is-built-for-strangers).

---

## 4. Non-negotiable rules

Hard constraints that override all other considerations:

### Engineering Discipline

- Fix root causes, never symptoms. When something misbehaves, find the underlying cause and fix it so the problem cannot recur — a fix that only hides or defers the failure is not done.
- Temporary fixes, workarounds, and hacks are not acceptable. If a proper fix is genuinely impossible right now, say so explicitly and get agreement on the gap before shipping anything less; never ship it silently.
- Where a class of mistake keeps being possible, prefer making it structurally impossible (enforced by code) over documenting a convention people must remember.
- **Nothing devgeta ships is customized for devgeta.** Every feature, bug fix, refactor, doc, config, hook, skill, alias, theme, and `dg task` subcommand is built for the people who install it — not for this repository, this maintainer, or the task in front of you. Before writing anything, apply the test in [principle 8](#3-product-principles): _would this still make sense to someone who has never seen this repo?_ If the answer is no, it does not ship — generalize it, or put it in `CLAUDE.md` / `docs/` where project-specific rules belong. This applies in both directions and to every size of change: no one-off features for one user, and no devgeta policy pushed into an artifact everybody installs.

### Security

- Never execute arbitrary downloaded code without verification
- All shell scripts (`install.sh`, embedded configs) must be reviewed before execution
- Credentials and secrets must never be stored in configs or committed to git
- User input must always be validated before use (especially paths, command arguments)

### Data Integrity

- Installation state must be atomic: either complete or fully roll back (no partial installations)
- Tests must never read or write real user directories. Under `go test`, `pkg/paths` automatically redirects HOME and all XDG roots into a throwaway sandbox so this cannot happen even when a test forgets to isolate; that guard must never be weakened or bypassed
- User home directory must never be assumed writable in global locations; respect XDG Base Directory if needed
- Config files installed by devgeta must be distinguishable from user edits (version markers, checksums)

### Platform Support

- macOS 13+ (Ventura or newer) must be supported; don't use features that break on older versions
- Debian 12+ (Bookworm) and Ubuntu 24+ must be supported; test on both
- Only amd64 and arm64 architectures; drop support only with major version bump

---

## 5. Tech stack

| Layer                       | Technology                    | Notes                                                                                               |
| --------------------------- | ----------------------------- | --------------------------------------------------------------------------------------------------- |
| **Language**                | Go 1.25+ (toolchain 1.26.3)   | stdlib, no cgo where possible (cross-compilation)                                                   |
| **Build System**            | Make                          | See Makefile for targets                                                                            |
| **CLI Framework**           | Cobra                         | Used in `cmd/` for command structure                                                                |
| **Config Format**           | YAML (`gopkg.in/yaml.v3`)     | State stored in `~/.config/devgeta/global_config.yaml`                                              |
| **Config Generation**       | Go `text/template` + `embed`  | Templates in `configs/` embedded at compile time                                                    |
| **Package Manager**         | Homebrew (macOS), APT (Linux) | With package name translation (see `pkg/constants/package_mappings.go`)                             |
| **Logging**                 | Custom (zap-like logger)      | Initialized with `logger.Init(verbose)`                                                             |
| **Testing**                 | Go `testing` package          | Unit tests in `*_test.go` alongside code                                                            |
| **Installation Strategies** | Strategy pattern              | In `internal/commands/debian_strategies.go` (AptStrategy, PPAStrategy, InstallScriptStrategy, etc.) |
| **CI/CD**                   | GitHub Actions                | `.github/workflows/release.yml` builds multiplatform binaries on git tag                            |

---

## 6. Implementation behavior

### Coding standards

- Follow [Effective Go](https://golang.org/doc/effective_go) conventions
- **Reuse before writing (DRY):** Before adding any function, helper, or logic, search the codebase for something that already does the job and build on it — extend or delegate rather than re-implement. When a change would make two code paths share the same logic, extract that logic into one place instead of copying it; do the extraction in the same PR that introduces the second use. Duplication found during review is a defect to fix, not a style preference.
- **Investigate before designing new state or mechanisms:** Before introducing new state, persistence, or a new signal to solve a problem, actively explore whether something reachable already tracks or exposes it — an existing mechanism in this codebase, or one already offered live by a tool/app this project integrates with (e.g. tmux, git). Prefer reading and building on that existing source of truth over creating a parallel one that can drift from it or duplicate its job. Do this exploration up front, before implementing — not as a correction after a first attempt turns out more complex than it needed to be.
- **Prefer existing over new:** When new code is unavoidable, prefer in this order: this codebase's existing helpers and patterns, then the Go standard library, then a dependency the project already uses, and only then new custom code. Never add a new dependency for something the standard library or an existing dependency covers; introducing one is a decision to surface in the PR, not a default. Also question whether the code needs to exist at all — speculative or "for later" code is not written until something needs it. Simplicity never overrides the non-negotiable rules (section 4), correctness, or error handling.
- **Route external tools through their app wrappers:** Every invocation of an external binary that already has a devgeta app wrapper must go through that wrapper, not a raw `exec.Command` or a hand-assembled `CommandParams`. This keeps timeout, streaming, directory, and error-wrapping behavior consistent and mockable in tests. When the wrapper lacks a capability the caller needs (a directory, a timeout, a new subcommand), extend the wrapper — and, if the gap is in the shared executor, the executor — rather than reaching around it. The only exception is the low-level platform-install layer, which is itself the boundary that shells out. When a class of external tool acquires a wrapper, reaching around it is a defect to fix in review.
- Naming: camelCase for functions/variables, PascalCase for exports
- Run `go fmt` before committing (make lint does this)
- Comments explain WHY, not WHAT (code should be self-documenting)
- Never ignore errors; always handle or return them explicitly

### Communication style

Applies to every reply the agent writes in this repo — answers, summaries, PR text, commit messages:

- Answer straight and keep it short. Lead with the answer or outcome, then only the detail that changes what the reader does next.
- Brevity comes from cutting filler, never substance. Do not omit important information, caveats, or failures to make a reply shorter.
- Plain language, no fancy wording. Any engineer on the team — regardless of seniority or context — should be able to read a reply once and understand it. Spell out terms instead of assuming shared shorthand.
- No decoration for its own sake: headers, tables, and bullet lists only when they genuinely make the answer easier to scan.
- State uncertainty and problems plainly instead of softening or padding them.

### Lint issues

**Fix lint issues — never suppress them with `//nolint` comments.**

`//nolint` bypasses the linter without addressing the problem; it is never acceptable.

| Issue                                              | Correct fix                                                                                                                                                                            |
| -------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Unchecked error (`errcheck`)                       | Handle the error: `if err := f(); err != nil { ... }`. Use `_ = f()` only when the error is genuinely non-actionable (e.g. closing a read-only file) and add a comment explaining why. |
| Empty branch (`SA9003`)                            | Replace the empty `if err != nil {}` with `_ = call()` and a comment if the ignore is intentional.                                                                                     |
| De Morgan's law (`QF1001`)                         | Rewrite: `!(a && b)` → `!a \|\| !b`.                                                                                                                                                   |
| Unchecked `defer` cleanup in tests                 | Use `t.Cleanup(func() { if err := os.RemoveAll(...); err != nil { t.Logf(...) } })` instead of a bare `defer`.                                                                         |
| Cross-file "undefined" from the per-file lint hook | These are false positives — the hook lints a single file and cannot see other files in the same package. Verify with `go build ./...`; if it passes, the issue is not real.            |

### Logger usage

- Initialize once at startup: `logger.Init(verbose)` in cmd/root.go
- Use logger for all output, not println/fmt.Print
- Log levels: error (always), warn (important), info (user actions), debug (detailed)

### Error handling

**See [docs/guides/error-handling.md](docs/guides/error-handling.md) for detailed patterns.**

Key principles:

- Always check errors: `if err != nil { return err }` or `logger.Fatal(err)`
- Use `MaybeExitWithError()` for user-facing errors
- Provide actionable error messages: tell users what went wrong and how to fix it
- Never expose raw Go errors to users; wrap and clarify

### Feature workflow (implement → verify → test → commit)

Follow this order for every non-trivial change:

1. **Implement** the feature or fix.
2. **Verify manually** that it works end-to-end (run the binary, use the UI, confirm the golden path).
3. **Add or update tests** — only after the feature is confirmed working. Tests written against a broken feature encode the wrong behavior.
4. **Commit** once manual verification passes and the **targeted tests** for what you changed are green (see below — not the whole suite).

> Before committing, always ask: _"Does this change have tests? Should it?"_ If the answer is yes and tests are missing, write them first. A working feature without tests is a regression waiting to happen.

### Which tests to run

**Run the targeted tests, not the whole suite.** The suite is ~2,500 tests across
~80 packages and takes about five and a half minutes from a cold cache — paying
that after every edit buys almost no signal. While implementing, run the package
you changed plus the in-repo packages that import it:

Don't guess at the importers — ask the toolchain. For a change in
`internal/apps/claude`:

```bash
go list -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{range .TestImports}} {{.}}{{end}}' ./... \
  | grep 'devgeta/internal/apps/claude' | cut -d' ' -f1
# → internal/apps/claude, internal/apps/registry, internal/tooling/terminal
```

Then run exactly that list — the whole set above takes 3.6s:

```bash
go test -run TestName ./internal/apps/claude/                                          # while iterating on one behavior
go test ./internal/apps/claude/ ./internal/apps/registry/ ./internal/tooling/terminal/ # before committing
```

Use `.Imports` (direct importers), **not** `.Deps`. `.Deps` is transitive, so it
lists the root `main` package for nearly every package in the repo — and the root
package's tests are the bash-spawning hook tests, 4.8 minutes on their own. A
transitive list turns every "targeted" run back into the full suite. If your
change alters behavior that importers rely on rather than package internals, run
one level further out as well.

When the **root package** (`github.com/cjairm/devgeta`, i.e. `go test .`) shows up
in the list, include it only if you changed something under `configs/` or one of
the hook scripts. Its test files cover exactly that — embedded configs, the
reviewer agents, and the `agent-config-guard` / `secret-guard` /
`suppression-guard` / `task-redirect` / `agent-state` shells — and nothing about
app or tooling logic, so for any other change it is 4.8 minutes of pure cost.

The full `go test ./...` belongs in exactly two situations:

- **Release — mandatory.** §9 step 1. Never tag on a partial run.
- **A change whose blast radius is most of the tree.** `pkg/paths` has 24 direct
  importers (71 transitive); `internal/commands` and `internal/testutil` are
  similar. Anything under `configs/` is read by the embedded-config tests in the
  root package, `internal/apps/claude`, and `internal/apps/opencode` — and the
  root package is the slow one, so a config change costs most of a full run
  anyway. In these cases run the suite and say that you did.

Be honest about the trade this makes: a targeted run cannot catch cross-package
interference, and it will not notice a test that only fails under the full
suite's parallel load. Those surface at the release gate — which is why that gate
stays a full run.

### Testing requirements

**CRITICAL: Always use mocks. Never execute real commands in tests.**

**See [docs/guides/testing-patterns.md](docs/guides/testing-patterns.md) for complete patterns, examples, and reference.**

Accidental real command execution is a common mistake. It can:

- Break tests on different systems (missing tools, platform differences)
- Modify user state (install packages, create files)
- Cause CI failures in shared environments
- Hide bugs (tests pass only if side effects succeed)

**Common mock-safety traps to avoid:**

- Using `foo.New()` in a test that calls state-changing methods — always use `&foo.Foo{Cmd: mockApp.Cmd, Base: mockApp.Base}` instead.
- Calling `testutil.VerifyNoRealCommands(t, tc.MockApp.Base)` after creating a separate `app := foo.New()` — the check targets the wrong base and silently passes even when real commands run.
- Not setting `t.Setenv("TMUX", "")` (or similar env vars) in tests that don't intend to exercise an env-triggered path.

**Testing checklist** (full patterns and examples → [docs/guides/testing-patterns.md](docs/guides/testing-patterns.md)):

- [ ] All public functionality has tests
- [ ] Use `testutil.MockApp` for command mocking — never call `foo.New()` in a test that invokes state-changing methods
- [ ] Verify no real commands executed: `testutil.VerifyNoRealCommands(t, mockApp.Base)` — confirm it's checking the **same** base the app uses
- [ ] **Isolate every path a test mutates (`testutil.SetupCompleteTest` or explicit `paths.Paths.*` overrides) in any test that calls Uninstall, ForceInstall, ForceConfigure, or SoftConfigure — and isolate ALL the roots the operation touches, not just the obvious one.** The automatic `pkg/paths` test sandbox protects real user data as a last resort, but unisolated tests still leak state into other tests through the shared sandbox
- [ ] **Save and restore every `paths.Paths.*` mutation via `t.Cleanup` — prevents cross-test state leakage**
- [ ] Use `t.Helper()` in test helper functions
- [ ] Test both success and failure paths
- [ ] No real Homebrew/apt calls in tests
- [ ] Initialize logger in test file: `func init() { testutil.InitLogger() }`

### App interface pattern

All apps in `internal/apps/` implement the `App` interface defined in `internal/apps/contract.go`. The full contract, sentinel errors, `AppKind` enum, `baseapp.Reinstall`, and constructor patterns are documented in **[docs/guides/app-interface.md](docs/guides/app-interface.md)**.

Quick reference:

- Every app adds `var _ apps.App = (*X)(nil)` for compile-time enforcement
- `ForceInstall` must use `baseapp.Reinstall(a.Install, a.Uninstall)` — never call `Uninstall` directly without handling `ErrUninstallNotSupported`
- Unsupported ops return sentinel errors (`apps.ErrUninstallNotSupported`, `apps.ErrUpdateNotSupported`, …) — **never free-form strings**
- `Fonts` satisfies `FontInstaller`, not `App` — see the guide for details

```go
// Minimal new app pattern
var _ apps.App = (*MyApp)(nil)

func (a *MyApp) Name() string       { return constants.MyApp }
func (a *MyApp) Kind() apps.AppKind { return apps.KindTerminal }

func (a *MyApp) ForceInstall() error { return baseapp.Reinstall(a.Install, a.Uninstall) }
func (a *MyApp) Uninstall() error    { return fmt.Errorf("%w for myapp", apps.ErrUninstallNotSupported) }
func (a *MyApp) Update() error       { return fmt.Errorf("%w for myapp", apps.ErrUpdateNotSupported) }
```

---

## 7. Critical surfaces

Verify these before changing code in these areas:

### Installation State Management

- [ ] Global config file is updated atomically (write to temp, then rename)
- [ ] Duplicate installations are prevented (check global_config.yaml before install)
- [ ] Rollback on failure works (test by simulating install failure partway through)
- [ ] Installation state persists across shell restarts

### Cross-platform Installation (macOS ↔ Debian/Ubuntu)

- [ ] Package names translated correctly (check `pkg/constants/package_mappings.go`)
- [ ] Debian strategies handle all cases (apt, PPA, Launchpad, script, git clone)
- [ ] Platform detection works reliably (use `BaseCommandExecutor.IsMac()`)
- [ ] Both platforms produce equivalent results (same tool versions, configs)

### Shell Integration

- [ ] Shell config files (`devgeta.zsh`) are sourced correctly
- [ ] Mise activation works after install (test: `eval "$(mise activate zsh)"`)
- [ ] User shell customizations are not overwritten
- [ ] Aliases and functions don't conflict with user's existing setup

---

## 8. Platform scope

**Supported platforms:**

- macOS 13+ (Ventura or newer) with Homebrew
- Debian 12+ (Bookworm) and Ubuntu 24+ with APT
- Architectures: amd64, arm64 (Apple Silicon)

**Supported categories:**

- Terminal tools (40+): shells, editors, utilities, runtime managers
- Languages: Node.js, Python, Go, Rust, PHP (via Mise or native)
- Databases: PostgreSQL, Redis, MySQL, MongoDB, SQLite
- Desktop apps: Platform-specific GUIs (Docker, browsers, window managers, etc.)
- AI tools: agent-support CLIs (rtk today; Ollama, Gemini CLI planned)

**Single-command installation:**

- `dg install` — interactive full setup
- `dg install --only <category>` — install specific category
- `dg install --skip <category>` — install all except category

See [ROADMAP.md](ROADMAP.md) for planned features and future platform support

---

## 9. Versioning & tagging

Devgeta follows [Semantic Versioning](https://semver.org/) strictly: **`vMAJOR.MINOR.PATCH`**

### Which bump to use

| Change type                                                                      | Bump              | Example                |
| -------------------------------------------------------------------------------- | ----------------- | ---------------------- |
| Bug fix, typo, test fix, docs correction                                         | **PATCH** `x.x.^` | `v0.10.2` -> `v0.10.3` |
| New feature, new app installer, new command, new flag                            | **MINOR** `x.^.x` | `v0.10.3` -> `v0.11.0` |
| Breaking change to CLI interface, config format change, removed platform support | **MAJOR** `^.x.x` | `v0.11.0` -> `v1.0.0`  |

**Rules:**

- Tags always start with `v` (e.g., `v0.10.3`, not `0.10.3`)
- PATCH resets to 0 on MINOR bump; MINOR and PATCH reset to 0 on MAJOR bump
- Refactoring with no behavior change = PATCH (conservative)
- Multiple bug fixes in one release = single PATCH bump
- A release mixing features and fixes = MINOR bump (the higher bump wins)
- When in doubt, ask before tagging

### What "release" means

**"Release" is a request for the whole chain, not one step.** When the maintainer
says "release" — or "commit, push and tag", or any subset of those words — run all
six steps below. Do not stop after committing, and do not ask which steps were
meant; that question has been answered and asking again is friction:

1. **Verify** — `go build ./...`, `make lint`, and the **full** `go test ./...`.
   This is the one place the whole suite is mandatory; day-to-day work runs
   targeted tests (§6, "Which tests to run"), so this is the only run that sees
   the entire tree. Never tag on a partial run. If any of the three fail, stop
   and report. Never tag over a red test.
2. **Notes** — write the message file from [docs/guides/RELEASE-NOTES-TEMPLATE.md](docs/guides/RELEASE-NOTES-TEMPLATE.md).
3. **Commit** — `git add` and `git commit` as **two separate commands**. A
   pre-commit hook rejects them combined, because it can only scan for secrets
   that are already staged.
4. **Tag** — `devgeta task release <version> --message-file <file>`.
5. **Push** — `git push origin main --tags` (or pass `--push` in step 4).
6. **Confirm** — the Release workflow ran, and the release page shows the tag's
   notes with all four binaries attached.

Only two things are still worth raising instead of deciding alone: a version bump
the table above leaves genuinely ambiguous, and anything that failed step 1.

Two ordering rules are load-bearing — each has already caused a bad release.
[docs/guides/releasing.md](docs/guides/releasing.md) carries the incident detail
and the retry order:

- **Tag before pushing.** `devgeta task release` counts commits ahead of
  `origin/<default>` to decide what to squash. Push first and that count is 0, so
  it skips the squash and tags whatever HEAD is — no error, no warning.
- **Never run a bare `git tag`.** A lightweight tag has no message, and the
  release workflow publishes the release body out of the annotation — so the page
  comes out empty. Deleting the tag to retry does not delete the release; it
  becomes a permanent draft.

### Push & tag workflow

**Always squash the unpushed commits into one before tagging.** `devgeta task release`
does that — never run the raw `git reset --soft` / `git tag` / `git push` sequence
by hand.

Write the notes file first, from [docs/guides/RELEASE-NOTES-TEMPLATE.md](docs/guides/RELEASE-NOTES-TEMPLATE.md),
preserving each original commit's context as bullets. It is not just a commit
message: the workflow reads it back out of the annotated tag and publishes it as
the GitHub release body, and it is the **only** thing that puts content there —
GitHub's auto-generated notes come from merged pull requests, and devgeta tags
straight from `main`. Then, from a clean tree on the default branch:

```bash
devgeta task release v0.11.0 --message-file release-notes.txt --push
```

`version` must match `vMAJOR.MINOR.PATCH` exactly, no prerelease suffixes —
machine-enforced. Without `--push` nothing is pushed and the tool prints exactly
what remains. What the command checks, what the workflow builds, and the retry
order when a release goes wrong: [docs/guides/releasing.md](docs/guides/releasing.md).

---

## 10. Change discipline

### Never silently

Things that must never happen silently (always require explicit PR discussion and test):

- Altering command signatures (`dg install --something`) without deprecation plan
- Adding new package categories without updating installer logic and tests
- Changing config file format (`global_config.yaml`) without migration strategy
- Removing support for a platform (macOS or Linux) without major version bump
- Modifying what "terminal tools" category includes — users depend on this being stable
- Changing installation paths or config directories — affects existing installations
- Changing an AI agent's permissions or formatters in only one of the two agents — see [Keeping the two AI agents in sync](#keeping-the-two-ai-agents-in-sync)
- Bending any shipped artifact — a config, hook, plugin, command, skill, alias, theme — to a devgeta-specific decision, in a change of any size; see [Anything we ship is built for strangers](#anything-we-ship-is-built-for-strangers)

### Spec-driven development

When to write documentation **before** code:

- **Write a cycle doc** when tackling a feature that spans multiple commands or touches multiple layers (see `docs/plans/TEMPLATE.md`)
- **Write an ADR** when choosing between technologies, patterns, or approaches with lasting impact (see `docs/decisions/TEMPLATE.md`)
- **Skip both** for bug fixes, incremental improvements to existing features, or obvious changes
- **Quick ADR** (one page) for local design decisions; full ADR for platform-level choices

**Required workflow before implementing ANY code changes:**

1. If the change is substantial, create a cycle document in `docs/plans/cycles/YYYY-MM-DD-<name>.md`
2. If the design chose between competing approaches with lasting impact, record each such choice as an ADR in `docs/decisions/` **before implementation starts** — a cycle doc's trade-offs section does not replace an ADR, and a design discussion that ends in an approved choice is not done until the ADR exists
3. Get user/team approval before implementing
4. Track progress by checking off steps as you go
5. When all steps are complete, mark all tasks/checkboxes in the cycle doc as done and update the status field in the document header to **Done**

---

## 11. Architecture Patterns

### Cross-platform installation

See `docs/guides/cross-platform-installation.md` for full details.

**Package Mappings:** `pkg/constants/package_mappings.go`

- Translates Homebrew package names → APT package names (e.g., `gdbm` → `libgdbm-dev`)

**Installation Strategies:** `internal/commands/debian_strategies.go`

- `AptStrategy` — Standard apt install with automatic name translation
- `PPAStrategy` — Personal Package Archives with GPG key configuration
- `LaunchpadPPAStrategy` — Launchpad PPA via `add-apt-repository`
- `InstallScriptStrategy` — Executable install scripts (`curl | sh`)
- `NerdFontStrategy` — GitHub release downloads for fonts
- `GitCloneStrategy` — Git repository cloning and setup

**Strategy pattern flow:**

1. Each app has a `GetInstallStrategy()` method
2. Strategy is platform-aware (returns different strategy on macOS vs. Debian)
3. Strategies handle error cases and retries (exponential backoff for downloads)
4. No knowledge of specific tools in strategy base—fully generic

### Testing pattern

```go
func init() { testutil.InitLogger() }

func TestFeature(t *testing.T) {
    mockApp := testutil.NewMockApp()
    app := &MyApp{Cmd: mockApp.Cmd, Base: mockApp.Base}

    // Test logic here

    // Always verify no real commands executed
    testutil.VerifyNoRealCommands(t, mockApp.Base)
}
```

---

## 12. Codebase landmarks

Where to find and add code:

| Purpose                    | Location                       | Notes                                                                                                                                                                                                                 |
| -------------------------- | ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **CLI commands**           | `cmd/`                         | Entry points; register in cmd/root.go                                                                                                                                                                                 |
| **App modules**            | `internal/apps/{appname}/`     | 2 files per app: `{appname}.go` + `{appname}_test.go`                                                                                                                                                                 |
| **Category coordinators**  | `internal/tooling/`            | terminal, languages, databases, worktree                                                                                                                                                                              |
| **Platform installers**    | `internal/commands/`           | Strategy implementations for Debian, Darwin                                                                                                                                                                           |
| **Configuration logic**    | `internal/config/`             | Global state management                                                                                                                                                                                               |
| **TUI components**         | `internal/tui/`                | TUIs live here; `internal/tui/components` is the shared toolkit (palette, hint bar, help overlay, filter field, list navigation) — new TUIs must be assembled from it, and logic needed by a second TUI moves into it |
| **Shared utilities**       | `pkg/`                         | Logger, paths, file ops, constants, package mappings                                                                                                                                                                  |
| **Embedded configs**       | `configs/`                     | Templates and static files (embedded at compile time)                                                                                                                                                                 |
| **Shared agent artifacts** | `configs/shared/`              | Skills, commands, agents shipped to both AI coders **and to every user's other repos** — nothing here may carry a devgeta-specific decision (see below)                                                               |
| **Tests**                  | `*_test.go` alongside impl     | Use testutil mocks; never execute real commands                                                                                                                                                                       |
| **User docs**              | `docs/`                        | Feature docs, architecture, app guides, tooling details                                                                                                                                                               |
| **Developer docs**         | `CLAUDE.md`, `CONTRIBUTING.md` | This file and contributor guide                                                                                                                                                                                       |

### Adding a new command

**See [docs/guides/cli-patterns.md](docs/guides/cli-patterns.md) for detailed patterns, examples, and best practices.**

1. Read cli-patterns.md to understand command structure and patterns
2. Create handler in `cmd/{command}.go` (or create subdirectory for complex commands with subcommands)
3. Implement command logic using Cobra following patterns from cli-patterns.md
4. Add tests alongside implementation (`*_test.go`) using [docs/guides/testing-patterns.md](docs/guides/testing-patterns.md)
5. Register in `cmd/root.go`
6. Document in README.md and `docs/spec.md` if user-facing
7. If substantial, create a cycle doc first (see §10, "Spec-driven development")

### Adding a new app installer

1. Create directory `internal/apps/{appname}/`
2. Implement `{appname}.go` with app interface
3. Implement `{appname}_test.go` with tests
4. Add config templates to `configs/{appname}/` if applicable
5. Register in appropriate category in `internal/tooling/{category}/`
6. Document in `docs/apps/{appname}.md`

### Changing an embedded config

1. Edit the file under `configs/`
2. Rebuild and reinstall the binary before deploying — `dg configure` extracts configs from the running binary, so an old binary silently deploys the old config
3. Deploy with `dg configure <app> --force`
4. If the config must satisfy a constraint imposed by an external tool (a plugin or program that parses, splices, or re-executes the value), enforce that constraint with a test against the embedded configs FS — a comment in the config alone will not survive future edits
5. If the config governs an AI coding agent, apply the change to **both** agents — see below
6. If the file lives under `configs/shared/skills/`, stop and read the next section first — it is almost certainly the wrong file to change

### Anything we ship is built for strangers

Everything under `configs/`, every command, hook, plugin, alias, theme, and
`dg task` subcommand lands on machines that are not ours, in repos that are not
this one — most not even Go. So **no change of any kind may bend a shipped
artifact to a devgeta decision.** Not a feature, not a bug fix, not a one-line
default, not a doc tweak. Before editing anything under `configs/`, ask: would
this still make sense to someone who has never seen this repo? If not, the change
belongs in `CLAUDE.md` or `docs/` — project law, not product ([principle 8](#3-product-principles),
[§4 Engineering Discipline](#engineering-discipline)).

Two places where the line is easy to cross:

- **`configs/shared/skills/`** — these run inside other people's repositories,
  which have no test suite of ours, no release gate, no branch conventions.
  Putting our policy in one is not just useless there, it is a wrong instruction.
  Most are also **vendored from upstream Superpowers** (see
  [ADR-0015 §7](docs/decisions/ADR-0015-agent-scratch-files-get-a-devgeta-owned-directory.md#7-vendored-skills-keep-tmp-knowingly)),
  so local edits buy a merge conflict on every sync. `dg-worktree` is the
  exception that shows the shape of a legitimate one: devgeta's own skill about
  devgeta's own command.
- **Devgeta's own process, when it has to be automated** — gate it so it can
  never fire in a user's environment, the way the `release` redirect is gated on
  `go.mod` in `configs/claude/task-redirect.sh`. Never ship it as a global
  default.

### Keeping the two AI agents in sync

Devgeta configures two AI coding agents, Claude Code and OpenCode, and they must
behave the same. **Never add a deny/ask rule, a formatter language, or any other
policy to one agent only** — `internal/apps/opencode/permissions_test.go` fails
the build on asymmetry in either direction, and weakening it to land a one-sided
change is not an option. If a rule can't be expressed in one agent, drop it from
both. Deploy both after any change: `dg configure claude --force` **and**
`dg configure opencode --force`.

Two traps that have each cost a debugging session:

- That test compares pattern **strings**, not what they match — identical rules
  can still enforce nothing (see
  [docs/guides/agent-permission-matching.md](docs/guides/agent-permission-matching.md)).
- Command frontmatter is ignored by both agents, so a command that posts outward
  or acts unattended must grant that authorization **in its own prose**.

Which file holds which concern, and the deliberate differences that must not be
"fixed" by halves: [docs/guides/agent-sync.md](docs/guides/agent-sync.md).

---

## Quick Reference: Common Commands

| Task           | Command                                 | Location                                                                 |
| -------------- | --------------------------------------- | ------------------------------------------------------------------------ |
| Build          | `make build`                            | Current platform                                                         |
| Build all      | `make all`                              | darwin-arm64, darwin-amd64, linux-amd64                                  |
| Test (default) | `go test <changed pkg> <its importers>` | The day-to-day run — get the importer list from §6, "Which tests to run" |
| Test single    | `go test -run TestName ./pkg/package`   | Specific test                                                            |
| Test all       | `go test ./...`                         | Full suite, ~5.5 min — release gate (§9), not every edit                 |
| Lint           | `make lint`                             | Format + vet                                                             |
| Format         | `go fmt ./...`                          | Auto-format code                                                         |
| Clean          | `make clean`                            | Remove binaries                                                          |

---

## Documentation Index

Quick reference to where things live:

| Topic                   | Location                                     | Description                                                                                                                   |
| ----------------------- | -------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| **Development Guides**  | `docs/guides/README.md`                      | Index of all guides with quick-start by task                                                                                  |
| **Feature Spec**        | `docs/spec.md`                               | What features exist, architecture, edge cases, testing strategy                                                               |
| **Testing Patterns**    | `docs/guides/testing-patterns.md`            | Mocking, dependency injection, test isolation                                                                                 |
| **Error Handling**      | `docs/guides/error-handling.md`              | Error patterns, user-facing messages                                                                                          |
| **CLI Patterns**        | `docs/guides/cli-patterns.md`                | Command structure, Cobra patterns, flags, subcommands                                                                         |
| **Task Design**         | `docs/guides/task-design.md`                 | AI-first, token-wise `dg task` output — when to build a task, output principles, rtk stance                                   |
| **Permission matching** | `docs/guides/agent-permission-matching.md`   | What the agents' permission patterns actually match at runtime — dead `~/` rules, `*` crossing `/`, the two resolution orders |
| **Agent sync**          | `docs/guides/agent-sync.md`                  | Which file holds which concern for Claude Code vs OpenCode, and the deliberate differences between them                       |
| **Recent changes**      | `docs/recent-changes.md`                     | Prose summaries of recent work, last two releases only — the changelog that used to live at the bottom of this file           |
| **Cross-Platform**      | `docs/guides/cross-platform-installation.md` | Strategy pattern, package mappings, Debian strategies                                                                         |
| **Theming**             | `docs/guides/theming.md`                     | Shared Gruvbox palette, `.Theme` flow, transparency convention, the "match the others" rule                                   |
| **Claude Code app**     | `docs/apps/claude.md`                        | Claude config, format/lint hook (reuses neovim Mason), statusline                                                             |
| **Releasing**           | `docs/guides/releasing.md`                   | GitHub releases workflow, versioning                                                                                          |
| **Release notes**       | `docs/guides/RELEASE-NOTES-TEMPLATE.md`      | Template + structure for the `--message-file` that becomes the GitHub release body                                            |
| **Migrations**          | `docs/migrations/README.md`                  | Upgrade steps a user must run by hand (paths/folders that move)                                                               |
| **Roadmap**             | `ROADMAP.md`                                 | Planned commands, future features, open questions                                                                             |
| **Decisions**           | `docs/decisions/README.md`                   | Architectural decisions with rationale                                                                                        |
| **Contributing**        | `CONTRIBUTING.md`                            | Dev setup, build, test, git workflow, release process                                                                         |

---

## Recent changes

Prose summaries of recent changes live in
[docs/recent-changes.md](docs/recent-changes.md). They were moved out of this
file because this one is loaded into every session, and a changelog is not
context anyone needs up front. That file keeps only the last two releases —
the permanent record is `docs/decisions/` (why), `docs/plans/cycles/` (what the
work was), and git history.
