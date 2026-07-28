# Cycle: Fix Installation Bugs (fd-find, fontconfig, Aerospace, logger)

**Date:** 2025-04-17
**Estimated Duration:** ~1.5 hours
**Status:** DONE

---

## 1. Domain Context

During `dg install` testing, four bugs were discovered:

1. **fd-find package name**: On macOS, the fd tool is installed with an unnecessary alias "fd-find", but Homebrew package name is just "fd"
2. **fontconfig SoftConfigure**: Returns "not implemented" error instead of succeeding (fontconfig doesn't need devgeta-managed config)
3. **Aerospace detection**: `MaybeInstallDesktopApp("nikitabobko/tap/aerospace")` fails to detect existing `AeroSpace.app` because the cask name doesn't match the filesystem app name
4. **Logger verbosity**: Debug/info logs appear without `--verbose` flag because production logger defaults to INFO level

These are independent, simple fixes that can be batched into one cycle.

---

## 2. Engineer Context

### Relevant Files

- `internal/tooling/terminal/dev_tools/fdfind/fdfind.go` - fd-find app module
- `internal/tooling/terminal/core/fontconfig/fontconfig.go` - fontconfig app module
- `internal/apps/aerospace/aerospace.go` - Aerospace app module
- `pkg/logger/logger.go` - Logger initialization
- `pkg/constants/constants.go` - Package name constants (FdFind = "fd")

### Key Patterns

- `MaybeInstallPackage(constant, optionalAlias)` - Second param is used as the actual install name
- `MaybeInstallDesktopApp(caskName, optionalAlias)` - Second param is used for filesystem presence check
- `SoftConfigure()` should return `nil` for apps that don't need configuration
- `zap.NewProductionConfig()` allows customizing log level

### Test Commands

```bash
go test ./internal/tooling/terminal/dev_tools/fdfind/...
go test ./internal/tooling/terminal/core/fontconfig/...
go test ./internal/apps/aerospace/...
go test ./pkg/logger/...
go build -o devgeta main.go
```

---

## 3. Objective

Fix four installation bugs so `dg install` completes without errors, warnings, or unexpected log output.

---

## 4. Scope Boundary

### In Scope

- [x] Fix fd-find package name alias issue
- [x] Fix fontconfig SoftConfigure to return nil
- [x] Fix Aerospace detection to use correct app name for filesystem check
- [x] Fix logger to only show ERROR level in production mode

### Explicitly Out of Scope

- Debian package name mappings - separate concern
- Adding new tests - existing tests cover these scenarios

**Scope is locked.**

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                                       | Description                                      |
| ------ | --------------------------------------------------------------- | ------------------------------------------------ |
| Modify | `internal/tooling/terminal/dev_tools/fdfind/fdfind.go:46`       | Remove unnecessary "fd-find" alias parameter     |
| Modify | `internal/tooling/terminal/core/fontconfig/fontconfig.go:55-67` | Change SoftConfigure to return nil               |
| Modify | `internal/apps/aerospace/aerospace.go:40`                       | Add "AeroSpace" alias for filesystem detection   |
| Modify | `pkg/logger/logger.go:21-26`                                    | Configure production logger for ERROR level only |

### Step-by-Step

#### Step 1: Fix fd-find package name

**File:** `internal/tooling/terminal/dev_tools/fdfind/fdfind.go:46`

**Change:**

```go
// Before:
return f.Cmd.MaybeInstallPackage(constants.FdFind, "fd-find")

// After:
return f.Cmd.MaybeInstallPackage(constants.FdFind)
```

**Rationale:** The constant `FdFind` is already `"fd"` which is the correct Homebrew package name. The "fd-find" alias is unnecessary and causes confusion.

#### Step 2: Fix fontconfig SoftConfigure

**File:** `internal/tooling/terminal/core/fontconfig/fontconfig.go:55-67`

**Change:**

```go
// Before:
func (fc *FontConfig) SoftConfigure() error {
    return fmt.Errorf("not implemented: SoftConfigure")
}

// After:
func (fc *FontConfig) SoftConfigure() error {
    // fontconfig doesn't require devgeta-managed configuration
    // It uses system defaults which are appropriate for most use cases
    return nil
}
```

**Rationale:** fontconfig is a system library that doesn't need custom configuration from devgeta. Returning an error breaks the installation flow unnecessarily.

#### Step 3: Fix Aerospace detection

**File:** `internal/apps/aerospace/aerospace.go:40`

**Change:**

```go
// Before:
func (a *Aerospace) SoftInstall() error {
	return a.Cmd.MaybeInstallDesktopApp("nikitabobko/tap/aerospace")
}

// After:
func (a *Aerospace) SoftInstall() error {
	return a.Cmd.MaybeInstallDesktopApp("nikitabobko/tap/aerospace", "AeroSpace")
}
```

**Rationale:** The cask name `nikitabobko/tap/aerospace` contains slashes and doesn't match the filesystem app name `AeroSpace.app`. The alias parameter is used by `IsDesktopAppPresent()` to check if the app exists in `/Applications/`. Without this alias, the detection fails and Homebrew tries to install over an existing app.

#### Step 4: Fix logger verbosity

**File:** `pkg/logger/logger.go:21-26`

**Change:**

```go
// Before:
func Init(verbose bool) {
    var err error
    if verbose {
        zapLogger, err = zap.NewDevelopment()
    } else {
        zapLogger, err = zap.NewProduction()
    }
    // ...
}

// After:
func Init(verbose bool) {
    var err error
    if verbose {
        zapLogger, err = zap.NewDevelopment()
    } else {
        cfg := zap.NewProductionConfig()
        cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
        zapLogger, err = cfg.Build()
    }
    // ...
}
```

**Rationale:** Production logger should only show errors. Info/warn logs are useful for debugging but not for normal operation.

#### Step 5: Build and verify

```bash
go build -o devgeta main.go
go test ./internal/tooling/terminal/dev_tools/fdfind/...
go test ./internal/tooling/terminal/core/fontconfig/...
go test ./internal/apps/aerospace/...
go test ./pkg/logger/...
go vet ./...
```

---

## 6. Verification Plan

### Automated Verification

```bash
# Must all pass
go build -o devgeta main.go
go test ./...
go vet ./...
```

### Manual Verification

#### Bug 1 (fd-find):

1. Run `./devgeta install --only terminal` on macOS
2. Observe no warning about "fd-find" package name
3. Verify `fd --version` works after installation

#### Bug 2 (fontconfig):

1. Run `./devgeta install --only terminal`
2. Observe no "not implemented: SoftConfigure" error
3. Installation should proceed past fontconfig without error

#### Bug 3 (Aerospace):

1. Ensure `AeroSpace.app` is already in `/Applications/`
2. Run `./devgeta install --only desktop`
3. Observe no "Error: It seems there is already an App at '/Applications/AeroSpace.app'" error
4. Aerospace should be detected as already installed and skipped

#### Bug 4 (logger):

1. Run `./devgeta install` WITHOUT `--verbose`
2. Observe NO debug/info log lines (only errors if any)
3. Run `./devgeta install --verbose`
4. Observe debug/info logs appear (development mode)

### Regression Check

- fontconfig should still install correctly (package installation unchanged)
- fd should still install correctly (just removed unnecessary alias)
- Aerospace should still install correctly on fresh systems (Install() unchanged)
- Verbose mode should still show detailed logs

---

## 7. Risks & Trade-offs

| Risk                                               | Likelihood | Mitigation                                       |
| -------------------------------------------------- | ---------- | ------------------------------------------------ |
| Logger change hides useful warnings                | Low        | Users can use --verbose; errors still shown      |
| fontconfig might need config later                 | Low        | Can add config support in future cycle if needed |
| Aerospace alias might not match in future versions | Very Low   | App name is stable; update alias if it changes   |

### Trade-offs Made

- Chose ERROR-only for production logger (vs WARN) for cleaner output
- fontconfig returns nil without any logging (vs logging "no config needed")
- Aerospace uses hardcoded "AeroSpace" alias (vs dynamic cask-to-app name mapping)

---

## 8. Cross-Model Review Notes

- [x] Root cause confirmed for all 4 bugs
- [x] All affected files identified (4 files, 4 changes)
- [x] Verification steps are executable
- [x] Scope is appropriately bounded (~1.5 hour work)

**Reviewer notes:**
Ready for implementation approval.
