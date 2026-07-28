# Cross-Platform Installation Architecture

This document describes devgeta's architecture for installing packages across macOS (Homebrew) and Debian/Ubuntu (apt) systems.

---

## Overview

Devgeta uses a **strategy pattern** combined with **package name mappings** to provide seamless installation across platforms. The codebase is designed with macOS as the primary platform, with translation layers for Debian/Ubuntu.

### Key Components

| Component               | Location                                 | Purpose                                                 |
| ----------------------- | ---------------------------------------- | ------------------------------------------------------- |
| Package Mappings        | `pkg/constants/package_mappings.go`      | Translate Homebrew names to apt names                   |
| Installation Strategies | `internal/commands/debian_strategies.go` | Different installation methods for Debian               |
| Command Interfaces      | `internal/commands/base.go`              | Platform-agnostic installation contracts                |
| Platform Detection      | `internal/commands/factory.go`           | Detect OS and return appropriate command implementation |

---

## Package Name Mappings

### Problem

macOS Homebrew and Debian apt often use different names for the same package:

| Package  | Homebrew (macOS) | apt (Debian/Ubuntu) |
| -------- | ---------------- | ------------------- |
| gdbm     | `gdbm`           | `libgdbm-dev`       |
| jemalloc | `jemalloc`       | `libjemalloc2`      |
| ncurses  | `ncurses`        | `libncurses5-dev`   |
| zlib     | `zlib`           | `zlib1g-dev`        |

### Solution

The `PackageMappings` map in `pkg/constants/package_mappings.go` provides translations:

```go
type PackageMapping struct {
    MacOS  string // Homebrew package name
    Debian string // Debian/Ubuntu package name
}

var PackageMappings = map[string]PackageMapping{
    Gdbm: {
        MacOS:  "gdbm",
        Debian: "libgdbm-dev",
    },
    // ... more mappings
}
```

### Usage

The `GetDebianPackageName()` function handles translation with fallback:

```go
func GetDebianPackageName(packageConstant string) string {
    if mapping, exists := PackageMappings[packageConstant]; exists {
        return mapping.Debian
    }
    return packageConstant // Fallback to original name if not mapped
}
```

### When to Add Mappings

Add a new mapping when:

1. A library/tool has different package names across platforms
2. The Homebrew name doesn't work with apt
3. Debian requires a `-dev` suffix for development headers

---

## Installation Strategies

Devgeta uses the **Strategy Pattern** to handle different installation methods on Debian/Ubuntu. Each strategy implements the `InstallationStrategy` interface.

### Interface Contract

```go
type InstallationStrategy interface {
    Install(packageName string) error
    IsInstalled(packageName string) (bool, error)
}
```

### Available Strategies

#### 1. AptStrategy

**Purpose:** Standard apt package installation with name translation.

**Use when:** Package is available in default Debian/Ubuntu repositories.

```go
type AptStrategy struct {
    cmd *DebianCommand
}

func (s *AptStrategy) Install(packageName string) error {
    debianName := constants.GetDebianPackageName(packageName)
    return s.cmd.installWithApt(debianName)
}
```

**Example packages:** curl, unzip, git, most system libraries

---

#### 2. PPAStrategy

**Purpose:** Install from a Personal Package Archive with custom GPG key.

**Use when:** Package requires adding a third-party repository with explicit key configuration.

```go
type PPAStrategy struct {
    cmd       *DebianCommand
    ppaConfig apt.PPAConfig
}
```

**PPAConfig structure:**

```go
type PPAConfig struct {
    Name        string   // Repository identifier (e.g., "eza")
    KeyURL      string   // GPG key URL
    KeyringPath string   // Where to save the key
    RepoLine    string   // sources.list entry
    Components  []string // apt components (main, universe, etc.)
}
```

**Example packages:** eza, custom repositories

---

#### 3. LaunchpadPPAStrategy

**Purpose:** Install from Launchpad PPA using `add-apt-repository`.

**Use when:** Package is hosted on Launchpad (ppa:owner/name format).

```go
type LaunchpadPPAStrategy struct {
    cmd    *DebianCommand
    ppaRef string // e.g., "ppa:zhangsongcui3371/fastfetch"
}
```

**Key behavior:**

1. Ensures `software-properties-common` is installed
2. Runs `add-apt-repository -y ppa:owner/name`
3. Updates apt cache
4. Installs package

**Example packages:** fastfetch, neovim (unstable PPA)

---

#### 4. InstallScriptStrategy

**Purpose:** Install via curl | sh style scripts.

**Use when:** Package provides an official install script (no apt package available).

```go
type InstallScriptStrategy struct {
    cmd       *DebianCommand
    scriptURL string
}
```

**Example packages:** mise (formerly rtx), starship

**Security note:** Only use for trusted, official install scripts.

---

#### 5. NerdFontStrategy

**Purpose:** Download and install Nerd Fonts from GitHub releases.

**Use when:** Installing patched fonts with programming ligatures and icons.

```go
type NerdFontStrategy struct {
    cmd        *DebianCommand
    archiveURL string // GitHub release URL for tar.xz
}
```

**Key behavior:**

1. Downloads tar.xz from GitHub releases
2. Extracts to `~/.local/share/fonts/`
3. Runs `fc-cache -fv` to register fonts

**Example packages:** JetBrainsMono Nerd Font, Hack Nerd Font

---

#### 6. GitCloneStrategy

**Purpose:** Clone a Git repository to a specific path.

**Use when:** Package is a collection of scripts/configs (not a binary).

```go
type GitCloneStrategy struct {
    cmd         *DebianCommand
    repoURL     string
    installPath string
}
```

**Key behavior:**

- Uses `git clone --depth 1` for shallow clone
- Skips if path already exists
- Returns error if clone fails

**Example packages:** zsh plugins (autosuggestions, syntax-highlighting), powerlevel10k

---

### Helper Function: InstallGitHubBinary

For binaries distributed via GitHub releases (not a strategy, but a helper):

```go
func InstallGitHubBinary(
    base BaseCommandExecutor,
    binaryName string,
    archiveURL string,
    checksumsURL string,
    downloadFn func(ctx, url, dest, cfg) error,
) error
```

**Key behavior:**

1. Downloads tar.gz from GitHub
2. Downloads the release's sha256sum-format checksums file and verifies the
   archive's SHA-256 against it — verification is mandatory (the binary is
   installed with sudo), so a missing or mismatched checksum aborts the
   install (CLAUDE.md §4)
3. Extracts the binary
4. Installs to `/usr/local/bin/` with sudo

The upstream project must publish a checksums asset (rtk, lazygit, and
lazydocker all ship `checksums.txt`); a release without one cannot be
installed through this helper.

**Example packages:** lazygit, lazydocker, rtk

---

## Retry Mechanism

The `pkg/downloader` package provides retry logic with exponential backoff:

```go
type RetryConfig struct {
    MaxAttempts int
    BaseDelay   time.Duration
    MaxDelay    time.Duration
    UseJitter   bool
}

func DefaultRetryConfig() RetryConfig {
    return RetryConfig{
        MaxAttempts: 3,
        BaseDelay:   1 * time.Second,
        MaxDelay:    4 * time.Second,
        UseJitter:   true,
    }
}
```

This is used by `NerdFontStrategy` and `InstallGitHubBinary` for reliable downloads.

---

## Platform Detection

### Factory Pattern

The `internal/commands/factory.go` file provides platform-specific command implementations:

```go
func NewCommand() Command {
    if runtime.GOOS == "darwin" {
        return NewMacCommand()
    }
    return NewDebianCommand()
}
```

### IsMac() Method

Apps can check the current platform:

```go
type BaseCommandExecutor interface {
    // ... other methods
    IsMac() bool
}
```

**Usage in apps:**

```go
func (app *MyApp) Install() error {
    if app.Base.IsMac() {
        // macOS-specific logic
    } else {
        // Debian/Ubuntu logic
    }
}
```

---

## Adding Support for a New Package

### Step 1: Define the constant

```go
// pkg/constants/constants.go
const MyPackage = "my-package"
```

### Step 2: Add mapping if needed

```go
// pkg/constants/package_mappings.go
var PackageMappings = map[string]PackageMapping{
    // ... existing mappings
    MyPackage: {
        MacOS:  "my-package",
        Debian: "libmy-package-dev",
    },
}
```

### Step 3: Choose installation strategy

| If package is...         | Use strategy                                      |
| ------------------------ | ------------------------------------------------- |
| In default apt repos     | `AptStrategy` (automatic via MaybeInstallPackage) |
| In a Launchpad PPA       | `LaunchpadPPAStrategy`                            |
| In a custom PPA with GPG | `PPAStrategy`                                     |
| Installed via script     | `InstallScriptStrategy`                           |
| A Nerd Font              | `NerdFontStrategy`                                |
| A git repository         | `GitCloneStrategy`                                |
| A GitHub release binary  | `InstallGitHubBinary` helper                      |

### Step 4: Implement in DebianCommand

For non-apt packages, add strategy selection in `internal/commands/debian.go`:

```go
func (d *DebianCommand) MaybeInstallPackage(packageName string, aliases ...string) error {
    // Check if installed first
    // ...

    // Select strategy based on package
    switch packageName {
    case constants.MyPackage:
        strategy := &LaunchpadPPAStrategy{
            cmd:    d,
            ppaRef: "ppa:myorg/mypackage",
        }
        return strategy.Install(packageName)
    default:
        // Default to apt
        strategy := &AptStrategy{cmd: d}
        return strategy.Install(packageName)
    }
}
```

---

## Architecture Decisions

### Why macOS-first?

1. **Developer prevalence:** Most devgeta users are on macOS
2. **Homebrew consistency:** Single package manager, consistent naming
3. **Translation layer:** Easier to translate from one source to many targets

### Why Strategy Pattern?

1. **Extensibility:** Easy to add new installation methods
2. **Testability:** Each strategy can be tested independently
3. **Separation of concerns:** Installation logic isolated from detection logic

### Why Package Mappings?

1. **Centralized:** All translations in one place
2. **Fallback:** Unknown packages use original name
3. **Documentable:** Clear mapping between platforms

---

## Testing Strategies

Each strategy should be testable with mocked dependencies:

```go
func TestAptStrategy_Install(t *testing.T) {
    mockCmd := NewMockDebianCommand()
    strategy := &AptStrategy{cmd: mockCmd}

    err := strategy.Install("gdbm")

    // Verify translated name was used
    assert.Equal(t, "libgdbm-dev", mockCmd.LastInstalledPackage)
}
```

See `docs/guides/testing-patterns.md` for comprehensive testing documentation.

---

## Related Documentation

- **Testing Patterns:** `docs/guides/testing-patterns.md`
- **Error Handling:** `docs/guides/error-handling.md`
- **Product Spec:** `docs/spec.md`
- **Individual App Docs:** `docs/apps/` directory
