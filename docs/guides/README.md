# Development Guides

Detailed guides for implementing features, tests, and commands in devgeta. These supplement [CLAUDE.md](../../CLAUDE.md) with practical examples and deep dives.

---

## Guide Index

### [CLI Patterns](cli-patterns.md) — Building commands with Cobra

Everything about designing and building devgeta commands:

- Command hierarchy and structure
- Flag handling (string slices, booleans, validation)
- Subcommands and aliases
- Error handling and output patterns
- Context usage
- Testing commands

**When to read:** Before building a new command or modifying existing command structure.

**Referenced by:** CLAUDE.md section 7 (adding new commands)

---

### [Task Design](task-design.md) — AI-first, token-wise `dg task` subcommands

How to design `dg task` output for LLM agents:

- When a task earns its existence (round-trips, policy, rendering) — and when not
- Output principles: labeled plain text, payload only, one-line confirmations, stable sentinels, lossy-with-a-receipt
- Orchestrate/format separation (jq for JSON, pure Go for text) and its testing payoff
- Measuring token cost before and after
- Future: rtk and how it complements `dg task`

**When to read:** Before adding or changing any `dg task` subcommand whose consumer is an agent.

**Referenced by:** CLAUDE.md Documentation Index; cycle docs 2026-06-18 and 2026-07-14

---

### [Testing Patterns](testing-patterns.md) — Isolation, mocks, and reliability

Complete testing architecture for devgeta:

- Three levels of test isolation
- Mock injection patterns
- `testutil` package reference
- Test examples for different app types
- Running tests with coverage
- Common issues and solutions

**When to read:** When writing tests or adding test infrastructure.

**Referenced by:** CLAUDE.md section 6 (testing requirements)

---

### [Error Handling](error-handling.md) — Consistent error messages and exit behavior

Principles and patterns for error handling:

- Return errors instead of panicking
- Add context when propagating errors
- Centralize CLI exit logic
- Rollback on failure
- Verbose/debug logging
- User-friendly messages

**When to read:** When designing error flows or implementing error handling in commands.

**Referenced by:** CLAUDE.md section 6 (error handling)

---

### [Cross-Platform Installation](cross-platform-installation.md) — Strategy pattern and package mappings

Technical deep dive into cross-platform installation:

- Package name translations (Homebrew ↔ apt)
- Installation strategy pattern
- Available strategies (Apt, PPA, script download, git clone, etc.)
- When to add new strategies
- Platform detection and fallback logic

**When to read:** When adding support for a new package, tool, or platform.

**Referenced by:** CLAUDE.md section 11 (architecture patterns)

---

### [Theming & Visual Consistency](theming.md) — Shared palette and transparency convention

How the visual layer stays cohesive across tools:

- The "match the others" rule for color/theme changes
- Which configs are templated (`.Theme`) vs. hardcoded (tmux, Neovim)
- The shared Gruvbox dark palette (and the classic vs. Material mismatch)
- Transparency convention — no opaque backgrounds over Alacritty's blur
- Known gaps to converge (`current_theme` is unwired, etc.)

**When to read:** Before changing any color, font, or theme behavior in Alacritty, tmux, Neovim, OpenCode, or Claude configs.

**Referenced by:** CLAUDE.md section 3 (product principles)

---

### [App Interface](app-interface.md) — The `App` contract every app must satisfy

The formal interface all apps implement:

- `App` interface and method semantics
- `AppKind` enum and when to use each kind
- Sentinel errors (`ErrUninstallNotSupported`, etc.) and `errors.Is` usage
- `baseapp.Reinstall` for the correct `ForceInstall` pattern
- Two constructor patterns (with/without `Base`)
- The `FontInstaller` outlier for parameterized font methods

**When to read:** Before implementing a new app installer or modifying existing install/configure logic.

**Referenced by:** CLAUDE.md section 6 (app interface pattern)

---

### [Agent Permission Matching](agent-permission-matching.md) — What the permission patterns actually match

How the two AI agents resolve permission patterns at runtime, and where that
differs from how they read:

- `~/`-anchored patterns never fire on OpenCode — paths arrive project-relative, so 26 shipped deny rules are inert
- `*` crosses `/`, so depth-based narrowing does not work and `dir/*` grants a whole subtree
- The two agents' conflict resolution differs (first-match-by-action vs. longest-pattern-wins), so a carve-out expressible in one is impossible in the other
- Why making the dead rules fire would re-break agent memory
- How to probe any of this against the real binary instead of guessing

**When to read:** Before adding, re-anchoring, or relying on any rule in `settings.json.tmpl` or `opencode.json.tmpl` — the parity test compares pattern strings and cannot tell you whether a pattern matches anything.

**Referenced by:** CLAUDE.md section 12 (keeping the two AI agents in sync)

---

### [Agent Sync](agent-sync.md) — Which file holds which concern, for both agents

The detail behind CLAUDE.md's one-line rule that a policy change goes into both
agents:

- The concern-by-concern table: permissions, formatting, redirects, activity state, config protection, scratch grant
- What `permissions_test.go` enforces — and the trap that it compares pattern strings, not what they match
- The deliberate differences: agent frontmatter is OpenCode-only, command frontmatter is inert in both, the lint feedback loop is Claude-only, `statusLine` has no OpenCode equivalent
- Why a command that posts outward must grant its own authorization in prose

**When to read:** Before changing anything under `configs/claude/` or `configs/opencode/`.

**Referenced by:** CLAUDE.md section 12 (keeping the two AI agents in sync)

---

### [Output-budget hook and runner](output-budget-runner.md) — The write-time output cap contract

**Status: not implemented** — the contract for a feature still in an unapproved
cycle. Read it before writing any of the three artifacts it governs:

- The argv shape and sidecar schema, and which numbers are transported vs. Go-only constants
- Why the naive `2>&1 | head` pipeline is banned: it returns `head`'s exit status, so a red test suite reports green
- How the capture is bounded without killing or blocking the command, and why `ulimit -f` and a bare `head -c` were rejected
- Why markers count inside the budgets they report, not on top of them
- The 15-digit decimal contract: what bash and JavaScript actually do to large integers, measured
- The tokenization contract — under-match rather than parse, because `segments.sh` segments but does not tokenize
- The conformance test matrix, kept beside the clauses it pins

**When to read:** Before touching `configs/claude/output-budget.sh`,
`configs/opencode/plugin/output-budget.js`, or
`configs/claude/output-budget-run.sh`.

**Referenced by:** [the token-and-context-efficiency cycle](../plans/cycles/2026-08-25-token-and-context-efficiency.md), Steps 4 and 6

---

### [Releasing](releasing.md) — Version management and GitHub Actions

Complete release workflow:

- Semantic versioning scheme
- Creating release tags
- GitHub Actions workflow automation
- Verifying releases
- Publishing binaries

**When to read:** When preparing a new release.

---

## Quick Start by Task

### [Release Notes Template](RELEASE-NOTES-TEMPLATE.md) — Structure for release notes

The `--message-file` passed to `devgeta task release` becomes the squashed commit, the annotated tag, **and** the GitHub release page body. Start here so every release page reads the same way.

### I'm adding a new command

1. Read [CLI Patterns](cli-patterns.md) — overall structure
2. Check [CLAUDE.md](../../CLAUDE.md) section 12 — where to add code
3. Implement using patterns from [CLI Patterns](cli-patterns.md)
4. Test using [Testing Patterns](testing-patterns.md)

### I'm adding a new app/tool installer

1. Read [App Interface](app-interface.md) — the contract every app must satisfy
2. Read [Cross-Platform Installation](cross-platform-installation.md) — strategy pattern
3. Check [CLAUDE.md](../../CLAUDE.md) section 6 — testing requirement (always use mocks)
4. Implement using patterns from [Testing Patterns](testing-patterns.md)

### I'm fixing error handling

1. Read [Error Handling](error-handling.md) — principles
2. Use `utils.MaybeExitWithError()` in commands
3. Reference [CLAUDE.md](../../CLAUDE.md) section 6 for patterns

### I'm releasing a new version

1. Read [Releasing](releasing.md) — complete workflow
2. Follow semantic versioning scheme
3. Create tag and push to trigger GitHub Actions

---

## How Guides Fit Into Devgeta's Documentation

```
CLAUDE.md (Source of Truth)
├── Section 6: Implementation behavior (brief overview)
│   └─> Links to [Error Handling](error-handling.md) for details
├── Section 6: Testing requirements (brief overview)
│   └─> Links to [Testing Patterns](testing-patterns.md) for details
├── Section 7: Command patterns (brief overview)
│   └─> Links to [CLI Patterns](cli-patterns.md) for details
├── Section 11: Architecture patterns
│   └─> Links to [Cross-Platform Installation](cross-platform-installation.md) for details
└── Section 12: Code landmarks (brief overview)

docs/spec.md (What features exist)
└─> Links to ROADMAP.md for future features

ROADMAP.md (What's planned)
└─> Links to docs/plans/cycles/ for active work

docs/decisions/ (How & why we decided)
└─> Individual ADRs for significant technical choices
```

---

## Keeping Guides Current

Guides are the detailed implementation reference. When you:

1. **Change a pattern** — Update the relevant guide immediately
2. **Discover a better practice** — Document it here, then update CLAUDE.md summary
3. **Add a new pattern** — Create a new guide subsection with examples
4. **Find duplication** — Link to the authoritative guide instead of duplicating

Stale guides are worse than no guides — they mislead developers. If a guide describes something that changed, update or remove it in the same PR.

---

## Modern Practices (2026)

These guides follow current industry standards for CLI tool development:

✓ **Type safety** — Go's type system enforces patterns  
✓ **Composition over inheritance** — Interface-based design  
✓ **Dependency injection** — Testable via mocks, not global state  
✓ **Progressive disclosure** — Help shows what matters, examples show workflows  
✓ **Fast feedback** — Validation early, clear error messages  
✓ **Observability** — Verbose/debug modes, structured logging  
✓ **Consistency** — Shared utilities for output, flags, context  
✓ **Zero-downtime** — No breaking changes without deprecation plan

---

## Contributing a Guide

If you're adding new guides:

1. **Use markdown with clear hierarchy** — H2/H3 only, descriptive anchors
2. **Start with context** — Why does this matter?
3. **Show patterns with examples** — Not theory, concrete code
4. **Include a checklist** — Quick reference for "did I do this?"
5. **Link to related guides** — Help developers navigate
6. **Add to this README** — Update the index and quick start

There is no guide template — copy the structure of an existing guide instead;
[testing-patterns.md](testing-patterns.md) is a good model for a long one,
[error-handling.md](error-handling.md) for a short one.
