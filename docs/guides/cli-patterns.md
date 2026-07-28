# CLI Command Patterns

Guide to building consistent, user-friendly commands using Cobra. Covers all current and planned commands with patterns, conventions, and examples.

---

## Overview

Every devgeta command follows these principles:

1. **Clear hierarchy** — Root → Category → Action (e.g., `dg worktree create`)
2. **Consistent flags** — Predictable naming and behavior across commands
3. **Smart help** — Detailed `Long` descriptions with examples, not verbose prose
4. **Progressive disclosure** — Show only relevant options per command
5. **Fail fast** — Validate early, exit with actionable error messages
6. **Human output** — Progress indicators, success/error colors, no raw debug output

---

## Root Command Structure

**File:** `cmd/root.go`

Every devgeta command starts with the root command that:

- Initializes logging: `logger.Init(verbose)` via `PersistentPreRunE`
- Provides global flags: `--verbose`, `--debug` (alias for verbose)
- Handles help customization with `SetHelpFunc()`
- Centralizes error exit via `utils.MaybeExitWithError()`

```go
var rootCmd = &cobra.Command{
    Use:   "dg",
    Short: "Short description (one line)",
    Long: `Full description with key features.

Key Features:
  • Feature 1
  • Feature 2

Available Commands:
  command1       Description of command1
  command2       Description of command2`,
}

func init() {
    // Global flags (inherited by all subcommands)
    rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose logging")

    // Initialization hook (runs before any subcommand)
    rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
        logger.Init(verbose)
        return nil
    }
}
```

**Pattern:** Always use `PersistentPreRunE` for one-time initialization (logging, config loading).

---

## Simple Commands (No Subcommands)

**Example:** `dg version`

For commands with no subcommands, use simple `Run` function:

```go
var versionCmd = &cobra.Command{
    Use:   "version",
    Short: "Print the version number",
    Long: `Print the version number of devgeta.

Examples:
  dg version`,
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Println("devgeta v0.1.0")
    },
}

func init() {
    rootCmd.AddCommand(versionCmd)
}
```

---

## Commands with Flags

**Example:** `dg install --only terminal --skip databases`

### String Slice Flags (Multi-value)

For flags that accept multiple values (comma-separated or repeatable):

```go
var (
    only []string  // Multiple values: --only terminal,languages
    skip []string  // Multiple values: --skip databases
)

var installCmd = &cobra.Command{
    Use:   "install",
    Short: "Install devgeta and all required tools",
    Long: `Installs the devgeta platform and sets up your development environment.

This command performs the following steps:
  1. Validates your OS version
  2. Installs the package manager
  3. Installs terminal tools, languages, and databases
  4. Optionally installs desktop applications

Supported platforms:
  - macOS 13+ (Ventura) via Homebrew
  - Debian 12+ (Bookworm) / Ubuntu 24+ via apt

Examples:
  dg install                           # Interactive mode
  dg install --only terminal          # Only terminal tools
  dg install --only terminal,languages # Multiple categories
  dg install --skip desktop           # Skip desktop apps`,
    Run: run,
}

func init() {
    rootCmd.AddCommand(installCmd)

    installCmd.Flags().StringSliceVar(&only, "only", []string{},
        "Only install specific categories (comma-separated: terminal, languages, databases, desktop)")
    installCmd.Flags().StringSliceVar(&skip, "skip", []string{},
        "Skip specific categories (comma-separated: terminal, languages, databases, desktop)")
}

func run(cmd *cobra.Command, args []string) {
    // Convert slice to map for O(1) lookup
    onlySet := make(map[string]bool)
    for _, item := range only {
        onlySet[item] = true
    }

    skipSet := make(map[string]bool)
    for _, item := range skip {
        skipSet[item] = true
    }

    // Validate flags
    if len(onlySet) > 0 && len(skipSet) > 0 {
        utils.MaybeExitWithError(fmt.Errorf("cannot use both --only and --skip"))
    }

    // Proceed with installation
}
```

### Boolean Flags

For enable/disable options:

```go
var force bool

var configureCmd = &cobra.Command{
    Use:   "configure [app]",
    Short: "Update configuration files",
    Long: `Update configuration files after changes.

By default, existing files are never overwritten.
Use --force to overwrite even if the file already exists.

Examples:
  dg configure neovim          # Update neovim config if missing
  dg configure neovim --force  # Overwrite existing config`,
    Args: cobra.ExactArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        app := args[0]
        if force {
            // ForceConfigure behavior
        } else {
            // SoftConfigure behavior
        }
    },
}

func init() {
    installCmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config files")
}
```

### Flag Validation

Always validate flags early in the command's Run function:

```go
func run(cmd *cobra.Command, args []string) {
    // Validate flag combinations
    if force && soft {
        utils.MaybeExitWithError(fmt.Errorf("cannot use both --force and --soft"))
    }

    // Validate required arguments
    if len(args) == 0 {
        utils.MaybeExitWithError(fmt.Errorf("app name required"))
    }

    // Proceed if validation passes
}
```

---

## Hierarchical Commands (Subcommands)

**Example:** `dg worktree create`, `dg worktree list`, `dg worktree remove`

For commands with multiple related actions, create a parent command with subcommands:

### Parent Command

```go
var worktreeCmd = &cobra.Command{
    Use:     "worktree",
    Aliases: []string{"wt"},      // Shortcut: dg wt
    Short:   "Manage git worktrees",
    Long: `Manage git worktrees with tmux windows (alias: wt).

Worktrees are created in .worktrees/ with associated tmux windows.

Examples:
  dg worktree create feature-x       # Create worktree + window
  dg wt c feature-x                  # Same, using short form
  dg wt l                            # List all worktrees
  dg wt ui                           # Open TUI dashboard`,
}

func init() {
    rootCmd.AddCommand(worktreeCmd)

    // Add subcommands to parent
    worktreeCmd.AddCommand(worktreeCreateCmd)
    worktreeCmd.AddCommand(worktreeListCmd)
    worktreeCmd.AddCommand(worktreeRemoveCmd)
}
```

### Subcommand (Action)

```go
var worktreeCreateCmd = &cobra.Command{
    Use:     "create <name>",
    Aliases: []string{"c", "new"},  // dg wt c or dg wt new
    Short:   "Create a new worktree",
    Long: `Create a new git worktree with an associated tmux window (aliases: c, new).

This command:
  1. Creates a git worktree in .worktrees/<name>
  2. Creates a new branch <name>
  3. Creates tmux window wt-<name>
  4. Launches editor in the window

After creation, switch to the window with:
  <prefix> + w (select from list)

Examples:
  dg worktree create feature-login
  dg wt new fix-bug-123`,
    Args: cobra.ExactArgs(1),    // Require exactly 1 argument
    Run: func(cmd *cobra.Command, args []string) {
        name := args[0]
        wm := worktree.New()

        if err := wm.Create(name); err != nil {
            utils.MaybeExitWithError(err)
        }

        utils.PrintSuccess(fmt.Sprintf("Created worktree: %s", name))
    },
}
```

### Listing Subcommand (with formatted output)

```go
var worktreeListCmd = &cobra.Command{
    Use:     "list",
    Aliases: []string{"l", "ls"},
    Short:   "List all worktrees",
    Long: `List all git worktrees with their status.

Shows:
  - Worktree name and path
  - Associated branch
  - Tmux window name
  - Active status

Examples:
  dg worktree list
  dg wt ls`,
    Run: func(cmd *cobra.Command, args []string) {
        wm := worktree.New()

        statuses, err := wm.List()
        utils.MaybeExitWithError(err)

        // Format output as table
        w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
        fmt.Fprintln(w, "NAME\tBRANCH\tTMUX WINDOW\tACTIVE")
        for _, s := range statuses {
            active := ""
            if s.Active {
                active = "✓"
            }
            fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, s.Branch, s.WindowName, active)
        }
        w.Flush()
    },
}
```

---

## Error Handling in Commands

**Reference:** See [error-handling.md](error-handling.md) for detailed patterns.

In commands, use `utils.MaybeExitWithError()` which:

- Handles nil errors (no-op)
- Prints errors with context
- Exits with code 1
- Respects verbose/debug mode for stack traces

```go
func run(cmd *cobra.Command, args []string) {
    // Early validation
    if err := validateInput(); err != nil {
        utils.MaybeExitWithError(fmt.Errorf("validation failed: %w", err))
    }

    // Long-running operation with error handling
    if err := performInstallation(); err != nil {
        utils.MaybeExitWithError(fmt.Errorf("installation failed: %w", err))
    }

    // Success message
    utils.PrintSuccess("Installation complete!")
}
```

---

## Output Patterns

Commands use these utilities from `pkg/utils` for consistent output:

```go
// Progress/info messages
utils.PrintInfo("Validating version...")      // [INFO] Validating version...

// Success messages (typically green)
utils.PrintSuccess("Installation complete")  // [✓] Installation complete

// Error messages (typically red)
utils.PrintError("Something went wrong")      // [✗] Something went wrong

// Bold headers
utils.PrintBold("=== Setup Beginning ===")    // Bold text

// Generic print (no prefix)
utils.Print("Output line", "")               // Output line

// Debug (only in verbose mode)
logger.L().Debugw("Debug message", "key", value)
```

**Pattern:** Use appropriate output function for each scenario. Never use `fmt.Println()` directly in commands.

---

## Context Usage for State Passing

For commands that coordinate multiple steps or pass configuration between functions, use `context.Context`:

```go
func run(cmd *cobra.Command, args []string) {
    ctx := context.Background()

    // Pass shared state through context
    ctx = context.WithValue(ctx, "verbose", verbose)
    ctx = context.WithValue(ctx, "platform", platform)

    installTerminalTools(ctx)
    installLanguages(ctx)
    installDatabases(ctx)
}

func installLanguages(ctx context.Context) {
    verbose := ctx.Value("verbose").(bool)
    // Use verbose for logging
}
```

---

## Command Registration

All commands must be registered in `init()` functions within their files:

**File:** `cmd/install.go`

```go
func init() {
    rootCmd.AddCommand(installCmd)
    installCmd.Flags().StringSliceVar(&only, "only", []string{}, "...")
}
```

This keeps command definition and registration together.

---

## Testing Commands

**Reference:** See [testing-patterns.md](testing-patterns.md) for testing commands and mocking.

Test commands by:

1. Mocking internal dependencies (don't execute real commands)
2. Testing flag parsing with various inputs
3. Verifying error cases and exit codes
4. Testing output formatting

```go
func TestInstallCommand(t *testing.T) {
    // Setup mocks
    mockCmd := commands.NewMockCommand()

    // Test flag parsing
    installCmd.SetArgs([]string{"--only", "terminal,languages"})
    if err := installCmd.Execute(); err != nil {
        t.Fatalf("Expected success, got error: %v", err)
    }
}
```

---

## Planned Commands Quick Reference

| Command                         | Pattern                 | Status        |
| ------------------------------- | ----------------------- | ------------- |
| `dg install`                    | Flags + categories      | ✓ Implemented |
| `dg configure [app] --force`    | Single arg + bool flag  | Planned       |
| `dg uninstall [app] --category` | Single arg + flag       | Planned       |
| `dg list / installed`           | No args                 | Planned       |
| `dg update [app]`               | Single arg              | Planned       |
| `dg check-updates`              | No args                 | Planned       |
| `dg change --theme --font`      | Multiple flags          | Planned       |
| `dg backup [name]`              | Single arg              | Planned       |
| `dg restore [backup]`           | Single arg              | Planned       |
| `dg worktree create [name]`     | Hierarchical subcommand | ✓ Implemented |

---

## Best Practices Checklist

- [ ] Command uses `Short` (one line) and `Long` (detailed with examples)
- [ ] Flags have descriptions that fit in 60 characters
- [ ] Validation happens early in `Run()` function
- [ ] Errors use `utils.MaybeExitWithError()`
- [ ] Output uses `utils.PrintInfo/Success/Error()`
- [ ] Subcommands use `Aliases` for common shortcuts
- [ ] Help includes platform support information
- [ ] Help includes concrete examples
- [ ] Global flags (`--verbose`, `--debug`) are tested
- [ ] Commands don't call `os.Exit()` directly
- [ ] Long descriptions don't exceed 80 chars per line

---

## Architecture Decision

Commands are kept lightweight — business logic lives in `internal/tooling/` or `internal/apps/`. Commands orchestrate, don't implement. This keeps commands testable and reusable.

**File structure:**

```
cmd/
├── root.go          # Root command + global setup
├── install.go       # install command + orchestration
├── worktree.go      # worktree command + subcommands
└── version.go       # Simple single-action command

internal/
├── tooling/         # Command logic here (terminal, languages, databases)
├── apps/            # App implementations
└── commands/        # Platform-agnostic command execution
```
