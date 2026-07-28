# Cycle: Add Claude Code CLI Installer

**Date:** 2026-04-28  
**Estimated Duration:** ~4 hours  
**Status:** Done

---

## 1. Domain Context

Devgeta installs and configures terminal development tools. Claude Code (`claude`) is Anthropic's AI-powered CLI that runs in the terminal. This cycle adds Claude Code as a first-class installable tool in devgeta's terminal tools category, and consolidates the shared skills/commands/agents used by both Claude Code and OpenCode into a single source location.

**What exists already:**

- `configs/claude/settings.json` — Claude Code settings (theme, deny rules, statusline config)
- `configs/claude/statusline.sh` — Custom statusline script (model, context bar, git status)
- `configs/claude/themes/` — Theme files
- `configs/opencode/skills/`, `configs/opencode/commands/`, `configs/opencode/agents/` — Currently owned by OpenCode, but compatible with Claude Code paths too
- `configs/templates/devgeta.zsh.tmpl` — already has `{{if .Claude}}` and `{{if .Opencode}}` blocks, but `ShellFeatures` in `internal/config/fromFile.go` is missing both fields — the alias `cc` and `oc` never render currently
- No `internal/apps/claude/` module, no constant, no path entry, no tooling registration yet

**Installation method:**  
Claude Code is installed via the official installer script:

```bash
curl -fsSL https://claude.ai/install.sh | bash
```

This is an `InstallScriptStrategy` case — the same pattern used by other script-installed tools in the codebase.

**Shared content consolidation:**  
Skills, commands, and agents work identically in both Claude Code and OpenCode. Rather than duplicating them:

- Move `configs/opencode/skills|commands|agents/` → `configs/shared/skills|commands|agents/`
- Both apps deploy from `configs/shared/` to their respective config dirs during `ForceConfigure`

Deploy targets:
| Content | OpenCode destination | Claude Code destination |
|---------|---------------------|------------------------|
| skills | `~/.config/opencode/skills/` | `~/.claude/skills/` |
| commands | `~/.config/opencode/commands/` | `~/.claude/commands/` |
| agents | `~/.config/opencode/agents/` | `~/.claude/agents/` |

**`~/.claude/` path note:**  
Claude Code uses `~/.claude/` (a home dotdir), **not** `~/.config/claude/`. `GetConfigDir("claude")` would produce the wrong path. Use `paths.GetHomeDir(".claude")` instead.

**Reference:**

- [docs/spec.md](../../spec.md)
- [CLAUDE.md](../../../CLAUDE.md) — Section 11 (Architecture Patterns), Section 12 (Codebase Landmarks)
- Similar app: `internal/apps/opencode/opencode.go`

---

## 2. Engineer Context

**Relevant files and their purposes:**

| File                                    | Purpose                                                                                                  |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `pkg/constants/constants.go`            | String constants; add `Claude = "claude"` and `Shared = "shared"`                                        |
| `pkg/paths/paths.go`                    | Path struct; add `Claude` to `App.Configs` and a dedicated `Config.Claude` using `GetHomeDir(".claude")` |
| `internal/config/fromFile.go`           | `ShellFeatures` struct — add `Opencode bool` and `Claude bool` fields (both missing today)               |
| `internal/apps/claude/claude.go`        | New app module                                                                                           |
| `internal/apps/claude/claude_test.go`   | Mocked unit tests                                                                                        |
| `internal/apps/opencode/opencode.go`    | Update `ForceConfigure`: set `gc.Shell.Opencode = true` + `RegenerateShellConfig()`; update shared path  |
| `internal/tooling/terminal/terminal.go` | Register claude app                                                                                      |
| `configs/opencode/`                     | Remove `skills/`, `commands/`, `agents/` subdirs after moving                                            |
| `configs/shared/`                       | New directory — move skills/commands/agents here                                                         |

**Key patterns to follow:**

- `internal/apps/opencode/opencode.go` — identical struct and method signatures; copy as the starting point
- Config deployment: `ForceConfigure` removes old dir → creates fresh → copies files → `gc.AddToInstalled()`
- `SoftConfigure` guards via `gc.IsAlreadyInstalled()` + marker file check
- `InstallScriptStrategy` is available for curl-pipe installs but the simplest approach is `Base.ExecCommand(["sh", "-c", "curl -fsSL https://claude.ai/install.sh | bash"])`
- `SoftInstall` checks if the binary is already in PATH before running the installer

**Copying a directory tree (for skills/commands/agents):**  
Use `files.CopyDir` if it exists, or `filepath.WalkDir` + `files.CopyFile` per entry. Check `pkg/files/` for available helpers before writing new ones.

**Testing patterns:**

- See [docs/guides/testing-patterns.md](../guides/testing-patterns.md)
- Always use `testutil.NewMockApp()` — never run real curl or file operations in tests
- End every test with `testutil.VerifyNoRealCommands(t, mockApp.Base)`
- `func init() { testutil.InitLogger() }` at top of test file

**Commands to run tests:**

```bash
go test ./internal/apps/claude/
go test ./internal/apps/opencode/      # regression after opencode changes
go test ./internal/tooling/terminal/
go test ./...
make lint
```

---

## 3. Objective

Implement a `claude` app module that installs Claude Code via the official install script, deploys shared skills/commands/agents alongside Claude-specific configs (`settings.json`, `statusline.sh` with +x, themes) to `~/.claude/`, and registers in the terminal tools category so `dg install` picks it up automatically. Simultaneously move shared AI tool content out of `configs/opencode/` into `configs/shared/` so both apps draw from one source.

---

## 4. Scope Boundary

### In Scope

- [ ] Add `Claude = "claude"` and `Shared = "shared"` constants to `pkg/constants/constants.go`
- [ ] Add `Claude` to `App.Configs` and `Config` path structs in `pkg/paths/paths.go` (using `GetHomeDir(".claude")` for Config.Claude — **not** GetConfigDir)
- [ ] Add `Shared` to `App.Configs` path struct for `configs/shared/` source path
- [ ] Add `Opencode bool` and `Claude bool` to `ShellFeatures` in `internal/config/fromFile.go`
- [ ] Move `configs/opencode/skills/`, `configs/opencode/commands/`, `configs/opencode/agents/` → `configs/shared/`
- [ ] Update OpenCode's `ForceConfigure`: set `gc.Shell.Opencode = true`, call `gc.RegenerateShellConfig()`, update shared content source path
- [ ] Create `internal/apps/claude/claude.go` with full app interface
- [ ] Create `internal/apps/claude/claude_test.go` with mocked tests
- [ ] Register Claude in `internal/tooling/terminal/terminal.go` alongside OpenCode
- [ ] Claude's `ForceConfigure` deploys: `settings.json`, `statusline.sh` (with `chmod 0755`), `themes/`, shared `skills/`, `commands/`, `agents/`; sets `gc.Shell.Claude = true` and calls `gc.RegenerateShellConfig()`

### Explicitly Out of Scope

- `claude auth login` or API key setup — authentication is manual
- A `dg claude` subcommand wrapper
- Linux snap or any install path other than the official installer script
- `claude` update support — defer to the official updater (`claude update` or re-running the install script)
- Converting `settings.json` to a template — copy as-is for now

**Scope is locked.** Discoveries out of scope go in ROADMAP.md.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                               | Description                            |
| ------ | --------------------------------------- | -------------------------------------- | -------------------------- | ----------------------------- |
| Modify | `pkg/constants/constants.go`            | Add `Claude` and `Shared` constants    |
| Modify | `pkg/paths/paths.go`                    | Add `Claude` and `Shared` path entries |
| Move   | `configs/opencode/skills                | commands                               | agents/`→`configs/shared/` | Consolidate shared AI content |
| Modify | `internal/apps/opencode/opencode.go`    | Update ForceConfigure source paths     |
| Create | `internal/apps/claude/claude.go`        | Full app module                        |
| Create | `internal/apps/claude/claude_test.go`   | Mocked unit tests                      |
| Modify | `internal/tooling/terminal/terminal.go` | Import and register claude             |

### Step-by-Step

#### Step 1: Fix ShellFeatures — add missing Opencode and Claude fields

In `internal/config/fromFile.go`, add to the `ShellFeatures` struct:

```go
Opencode   bool `yaml:"opencode"`
Claude bool `yaml:"claude"`
```

Also add cases for `Opencode` and `Claude` in the `SetShellFeature` / `GetShellFeature` switch blocks — follow the same pattern as `Tmux` and `Neovim` immediately above.

- Verify: `go build ./internal/config/` and `go test ./internal/config/`

#### Step 2: Add constants

In `pkg/constants/constants.go`, add after the `OpenCode` constant:

```go
Claude = "claude"
Shared = "shared"
```

- Verify: `go build ./pkg/constants/`

#### Step 3: Add path entries

In `pkg/paths/paths.go`:

1. Add `Claude string` and `Shared string` to the `App.Configs` struct declaration (alongside `OpenCode`); initialize:
   ```go
   Claude: GetAppDir(constants.App.Dir.Configs, constants.Claude),
   Shared: GetAppDir(constants.App.Dir.Configs, constants.Shared),
   ```
2. Add `Claude string` to the `Config` struct declaration; initialize — **use GetHomeDir, not GetConfigDir**:
   ```go
   Claude: GetHomeDir(".claude"),
   ```

- Verify: `go build ./pkg/paths/`

#### Step 4: Move shared content and update OpenCode

1. Move files:
   ```
   configs/opencode/skills/   → configs/shared/skills/
   configs/opencode/commands/ → configs/shared/commands/
   configs/opencode/agents/   → configs/shared/agents/
   ```
2. In `internal/apps/opencode/opencode.go`, update `ForceConfigure`:
   - Deploy skills/commands/agents from `paths.Paths.App.Configs.Shared` instead of `paths.Paths.App.Configs.OpenCode`
   - After `gc.AddToInstalled(...)`, set `gc.Shell.Opencode = true` and call `gc.RegenerateShellConfig()` (same pattern as lazygit, neovim, tmux)
3. Verify: `go test ./internal/apps/opencode/ -v`

#### Step 5: Create the claude app module

Create `internal/apps/claude/claude.go` modeled on `internal/apps/opencode/opencode.go`.

Key differences:

- `Install()` — runs the official installer script:
  ```go
  params := cmd.CommandParams{
      Command: "sh",
      Args:    []string{"-c", "curl -fsSL https://claude.ai/install.sh | bash"},
  }
  _, _, err := o.Base.ExecCommand(params)
  ```
- `SoftInstall()` — checks PATH before running installer:
  ```go
  if _, err := exec.LookPath("claude"); err == nil {
      return nil
  }
  return o.Install()
  ```
- `ForceConfigure()`:
  1. `os.RemoveAll(paths.Paths.Config.Claude)` then `os.MkdirAll(..., 0755)`
  2. Copy `settings.json` → `~/.claude/settings.json`
  3. Copy `statusline.sh` → `~/.claude/statusline.sh`, then `os.Chmod(dest, 0755)`
  4. Copy `themes/` → `~/.claude/themes/`
  5. Copy `configs/shared/skills/` → `~/.claude/skills/`
  6. Copy `configs/shared/commands/` → `~/.claude/commands/`
  7. Copy `configs/shared/agents/` → `~/.claude/agents/`
  8. `gc.AddToInstalled(constants.Claude, "package")`
  9. `gc.Shell.Claude = true` then `gc.RegenerateShellConfig()`
- `Uninstall()` / `Update()` — return descriptive errors
- Verify: `go build ./internal/apps/claude/`

#### Step 6: Add unit tests

Create `internal/apps/claude/claude_test.go`. Test cases:

- `TestInstall_Success` — mocked sh/curl call succeeds
- `TestSoftInstall_AlreadyInstalled` — binary found in PATH, Install not called
- `TestSoftInstall_NotInstalled` — binary not found, Install called
- `TestForceConfigure_Success` — all files deployed, global_config updated
- `TestSoftConfigure_AlreadyConfigured` — skips when `IsAlreadyInstalled` returns true
- `TestUninstall_ReturnsError`
- `TestUpdate_ReturnsError`

End every test with `testutil.VerifyNoRealCommands(t, mockApp.Base)`.

- Verify: `go test ./internal/apps/claude/ -v`

#### Step 7: Register in terminal tooling

In `internal/tooling/terminal/terminal.go`:

1. Add import: `"github.com/cjairm/devgeta/internal/apps/claude"`
2. In `InstallTerminalApps()`, after the OpenCode block:
   ```go
   c := claude.New()
   if err := c.SoftInstall(); err != nil {
       displayMessage(err, constants.Claude)
       trackResult(summary, constants.Claude, err)
   } else {
       trackResult(summary, constants.Claude, nil)
       if err := c.SoftConfigure(); err != nil {
           displayMessage(err, constants.Claude, true)
       }
   }
   ```

- Verify: `go build ./internal/tooling/terminal/`

#### Step 8: Full test suite and lint

```bash
go test ./...
make lint
```

All tests must pass, lint must be clean before marking the cycle complete.

---

## 6. Verification Plan

### Automated Verification

```bash
# New module
go test ./internal/apps/claude/ -v

# Regression: opencode after shared content move
go test ./internal/apps/opencode/ -v

# Terminal tooling
go test ./internal/tooling/terminal/ -v

# Full suite
go test ./...

# Lint
make lint
```

---

## 7. Risks & Trade-offs

| Risk                                                                         | Likelihood | Mitigation                                                                                                  |
| ---------------------------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------- |
| `GetHomeDir(".claude")` vs XDG convention breaks on systems with custom HOME | Low        | `GetHomeDir` already uses `os.UserHomeDir()` which is reliable                                              |
| OpenCode tests break after shared content move                               | Med        | Run `go test ./internal/apps/opencode/` immediately after Step 3 before proceeding                          |
| Install script URL changes or requires interactive auth                      | Low        | Script only installs the binary; auth is separate (`claude`)                                                |
| `files.CopyDir` doesn't exist — need to implement tree copy                  | Med        | Check `pkg/files/` first; if missing, write a minimal `CopyDir` helper in Step 4                            |
| `exec.LookPath` in SoftInstall makes test isolation harder                   | Low        | Mock `Base.ExecCommand` for the PATH check; or accept that LookPath is side-effect-free and skip mocking it |

### Trade-offs Made

- **Shared content location:** `configs/shared/` over `configs/opencode/` — avoids Claude depending on OpenCode's config dir, makes ownership neutral
- **Install script over npm:** Official Anthropic script handles platform detection, PATH setup, and future changes transparently. Direct npm calls would require us to track version and npm availability manually.
- **`~/.claude/` hardcoded as `GetHomeDir(".claude")`:** Claude Code doesn't follow XDG; forcing it into `~/.config/` would break the tool.

---

## Notes for Implementers

- **Resolve Step 2 path ambiguity first** — everything else depends on the correct `~/.claude` path.
- **Step 3 before Step 4** — the shared path must exist before claude.go references it.
- **Check `pkg/files/` for a CopyDir helper** before writing one — avoid duplication.
- **Cycle document is your spec.** Update it if requirements change, but don't change scope without calling it out.
