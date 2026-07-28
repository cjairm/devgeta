# Worktree Management Feature Plan

## Overview

Add git worktree management to devgeta with tmux session integration. Each worktree gets its own tmux session with OpenCode, enabling parallel AI-assisted development.

## Goals

- Enable parallel development across multiple branches
- Leverage tmux sessions for workspace isolation
- Keep implementation simple - extend existing apps, minimal new code
- Use existing patterns (coordinator + apps)

## Non-Goals

- Complex orchestration or agent coordination
- Custom TUI frameworks (use existing promptui)
- Auto-restore sessions on reboot
- Worktree metadata tracking

---

## MVP Scope

### Commands

```bash
dg worktree create <name>     # Create worktree + tmux session + launch opencode
dg worktree list              # List worktrees with session status
dg worktree remove <name>     # Remove worktree and kill session
```

### What MVP Does NOT Include

- `--prompt` flag for initial OpenCode prompt
- `--from <branch>` flag to create from existing branch
- `dg worktree jump` (fzf selector)
- `dg worktree cleanup` (bulk removal)
- Uncommitted changes warnings

---

## Architecture

### File Structure

```
devgeta/
├── cmd/
│   └── worktree.go                      # Cobra command
├── internal/
│   ├── apps/
│   │   ├── git/git.go                   # Add 3 worktree methods
│   │   └── tmux/tmux.go                 # Add 4 session methods
│   └── tooling/
│       └── worktree/worktree.go         # Coordinator (orchestrates git + tmux + opencode)
```

### Pattern: Follows Languages Coordinator

Like `internal/tooling/languages/languages.go`:

- Coordinator in `tooling/` orchestrates multiple apps
- Uses existing app modules (git, tmux, opencode)
- Minimal new code - mostly wiring existing functionality

---

## Implementation Details

### 1. Extend Git App (`internal/apps/git/git.go`)

Add three methods using existing `ExecuteCommand()` pattern:

```go
// CreateWorktree creates a new worktree with a new branch
func (g *Git) CreateWorktree(path, branch string) error {
    return g.ExecuteCommand("worktree", "add", path, "-b", branch)
}

// ListWorktrees returns parsed worktree information
func (g *Git) ListWorktrees() ([]WorktreeInfo, error) {
    execCommand := cmd.CommandParams{
        Command: constants.Git,
        Args:    []string{"worktree", "list", "--porcelain"},
    }
    stdout, _, err := g.Base.ExecCommand(execCommand)
    if err != nil {
        return nil, fmt.Errorf("failed to list worktrees: %w", err)
    }
    return parseWorktreeOutput(stdout), nil
}

// RemoveWorktree removes a worktree
func (g *Git) RemoveWorktree(path string) error {
    return g.ExecuteCommand("worktree", "remove", path)
}

type WorktreeInfo struct {
    Path   string
    Branch string
    Commit string
}
```

### 2. Extend Tmux App (`internal/apps/tmux/tmux.go`)

Add four methods using existing `ExecuteCommand()` pattern:

```go
// CreateSession creates a new detached tmux session in the given directory
func (t *Tmux) CreateSession(name, workdir string) error {
    return t.ExecuteCommand("new-session", "-d", "-s", name, "-c", workdir)
}

// KillSession terminates a tmux session
func (t *Tmux) KillSession(name string) error {
    return t.ExecuteCommand("kill-session", "-t", name)
}

// HasSession checks if a session exists
func (t *Tmux) HasSession(name string) bool {
    err := t.ExecuteCommand("has-session", "-t", name)
    return err == nil
}

// SendKeys sends keystrokes to a session
func (t *Tmux) SendKeys(session, keys string) error {
    return t.ExecuteCommand("send-keys", "-t", session, keys, "Enter")
}
```

### 3. Create Worktree Coordinator (`internal/tooling/worktree/worktree.go`)

```go
package worktree

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/cjairm/devgeta/internal/apps/git"
    "github.com/cjairm/devgeta/internal/apps/opencode"
    "github.com/cjairm/devgeta/internal/apps/tmux"
    cmd "github.com/cjairm/devgeta/internal/commands"
    "github.com/cjairm/devgeta/pkg/constants"
)

const (
    worktreeDir   = ".worktrees"
    sessionPrefix = "wt-"
)

type WorktreeManager struct {
    Git      *git.Git
    Tmux     *tmux.Tmux
    OpenCode *opencode.OpenCode
    Base     cmd.BaseCommandExecutor
}

func New() *WorktreeManager {
    return &WorktreeManager{
        Git:      git.New(),
        Tmux:     tmux.New(),
        OpenCode: opencode.New(),
        Base:     cmd.NewBaseCommand(),
    }
}

// Create creates a new worktree with tmux session and launches OpenCode
func (w *WorktreeManager) Create(name string) error {
    // 1. Validate we're in a git repo
    repoRoot, err := w.getRepoRoot()
    if err != nil {
        return fmt.Errorf("not in a git repository: %w", err)
    }

    // 2. Check worktree doesn't exist
    wtPath := filepath.Join(repoRoot, worktreeDir, name)
    if _, err := os.Stat(wtPath); err == nil {
        return fmt.Errorf("worktree '%s' already exists", name)
    }

    // 3. Check session doesn't exist
    sessionName := sessionPrefix + name
    if w.Tmux.HasSession(sessionName) {
        return fmt.Errorf("tmux session '%s' already exists", sessionName)
    }

    // 4. Create worktree
    if err := w.Git.CreateWorktree(wtPath, name); err != nil {
        return fmt.Errorf("failed to create worktree: %w", err)
    }

    // 5. Create tmux session
    if err := w.Tmux.CreateSession(sessionName, wtPath); err != nil {
        return fmt.Errorf("failed to create tmux session: %w", err)
    }

    // 6. Launch OpenCode in session
    if err := w.Tmux.SendKeys(sessionName, constants.OpenCode); err != nil {
        return fmt.Errorf("failed to launch opencode: %w", err)
    }

    return nil
}

// List returns all worktrees with their session status
func (w *WorktreeManager) List() ([]WorktreeStatus, error) {
    worktrees, err := w.Git.ListWorktrees()
    if err != nil {
        return nil, err
    }

    var statuses []WorktreeStatus
    for _, wt := range worktrees {
        // Skip main worktree (not in .worktrees/)
        if !strings.Contains(wt.Path, worktreeDir) {
            continue
        }
        name := filepath.Base(wt.Path)
        sessionName := sessionPrefix + name
        statuses = append(statuses, WorktreeStatus{
            Name:          name,
            Path:          wt.Path,
            Branch:        wt.Branch,
            TmuxSession:   sessionName,
            SessionActive: w.Tmux.HasSession(sessionName),
        })
    }
    return statuses, nil
}

// Remove removes a worktree and its tmux session
func (w *WorktreeManager) Remove(name string) error {
    repoRoot, err := w.getRepoRoot()
    if err != nil {
        return fmt.Errorf("not in a git repository: %w", err)
    }

    wtPath := filepath.Join(repoRoot, worktreeDir, name)
    sessionName := sessionPrefix + name

    // Kill tmux session if exists
    if w.Tmux.HasSession(sessionName) {
        if err := w.Tmux.KillSession(sessionName); err != nil {
            return fmt.Errorf("failed to kill tmux session: %w", err)
        }
    }

    // Remove worktree
    if err := w.Git.RemoveWorktree(wtPath); err != nil {
        return fmt.Errorf("failed to remove worktree: %w", err)
    }

    return nil
}

func (w *WorktreeManager) getRepoRoot() (string, error) {
    execCommand := cmd.CommandParams{
        Command: constants.Git,
        Args:    []string{"rev-parse", "--show-toplevel"},
    }
    stdout, _, err := w.Base.ExecCommand(execCommand)
    if err != nil {
        return "", err
    }
    return strings.TrimSpace(stdout), nil
}

type WorktreeStatus struct {
    Name          string
    Path          string
    Branch        string
    TmuxSession   string
    SessionActive bool
}
```

### 4. Create Cobra Command (`cmd/worktree.go`)

```go
package cmd

import (
    "fmt"
    "os"
    "text/tabwriter"

    "github.com/cjairm/devgeta/internal/tooling/worktree"
    "github.com/cjairm/devgeta/pkg/utils"
    "github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
    Use:   "worktree",
    Short: "Manage git worktrees with tmux sessions",
    Long: `Create and manage git worktrees with isolated tmux sessions.

Each worktree gets its own tmux session with OpenCode running,
enabling parallel AI-assisted development across multiple branches.

Examples:
  dg worktree create feature-login    # Create worktree + session
  dg worktree list                    # List all worktrees
  dg worktree remove feature-login    # Remove worktree + session`,
}

var createCmd = &cobra.Command{
    Use:   "create <name>",
    Short: "Create a new worktree with tmux session",
    Args:  cobra.ExactArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        name := args[0]
        wm := worktree.New()

        if err := wm.Create(name); err != nil {
            utils.MaybeExitWithError(err)
        }

        utils.PrintSuccess(fmt.Sprintf("Created worktree: .worktrees/%s", name))
        utils.PrintSuccess(fmt.Sprintf("Created tmux session: wt-%s", name))
        utils.PrintInfo(fmt.Sprintf("Attach with: tmux attach -t wt-%s", name))
    },
}

var listCmd = &cobra.Command{
    Use:   "list",
    Short: "List all worktrees with session status",
    Run: func(cmd *cobra.Command, args []string) {
        wm := worktree.New()

        statuses, err := wm.List()
        utils.MaybeExitWithError(err)

        if len(statuses) == 0 {
            utils.PrintInfo("No worktrees found")
            return
        }

        w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
        fmt.Fprintln(w, "WORKTREE\tBRANCH\tSESSION\tSTATUS")
        for _, s := range statuses {
            status := "No session"
            if s.SessionActive {
                status = "Active"
            }
            fmt.Fprintf(w, ".worktrees/%s\t%s\t%s\t%s\n",
                s.Name, s.Branch, s.TmuxSession, status)
        }
        w.Flush()
    },
}

var removeCmd = &cobra.Command{
    Use:   "remove <name>",
    Short: "Remove a worktree and its tmux session",
    Args:  cobra.ExactArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        name := args[0]
        wm := worktree.New()

        if err := wm.Remove(name); err != nil {
            utils.MaybeExitWithError(err)
        }

        utils.PrintSuccess(fmt.Sprintf("Removed worktree: .worktrees/%s", name))
    },
}

func init() {
    rootCmd.AddCommand(worktreeCmd)
    worktreeCmd.AddCommand(createCmd)
    worktreeCmd.AddCommand(listCmd)
    worktreeCmd.AddCommand(removeCmd)
}
```

---

## Testing Strategy

### Unit Tests

Follow existing testing patterns from `docs/guides/testing-patterns.md`:

```go
// internal/apps/git/git_test.go
func TestGit_CreateWorktree(t *testing.T) {
    mockApp := testutil.NewMockApp()
    g := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

    err := g.CreateWorktree("/path/to/.worktrees/test", "test")
    if err != nil {
        t.Fatalf("CreateWorktree failed: %v", err)
    }

    lastCall := mockApp.Base.GetLastExecCommandCall()
    if lastCall.Command != "git" {
        t.Errorf("Expected git command, got %s", lastCall.Command)
    }

    testutil.VerifyNoRealCommands(t, mockApp.Base)
}

// internal/apps/tmux/tmux_test.go
func TestTmux_CreateSession(t *testing.T) {
    mockApp := testutil.NewMockApp()
    tm := &Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

    err := tm.CreateSession("wt-test", "/path/to/worktree")
    if err != nil {
        t.Fatalf("CreateSession failed: %v", err)
    }

    testutil.VerifyNoRealCommands(t, mockApp.Base)
}

// internal/tooling/worktree/worktree_test.go
func TestWorktreeManager_Create(t *testing.T) {
    // Use testutil.SetupCompleteTest for isolated paths
    tc := testutil.SetupCompleteTest(t)
    defer tc.Cleanup()

    // Test create flow with mocked apps
}
```

### Manual Testing Checklist

```
- [ ] Create worktree in fresh repo
- [ ] List shows correct status
- [ ] Remove worktree and session
- [ ] Error handling: not in git repo
- [ ] Error handling: worktree already exists
- [ ] Error handling: session already exists
- [ ] OpenCode launches in correct directory
```

---

## Error Handling

| Scenario               | Detection                   | User Message                                   |
| ---------------------- | --------------------------- | ---------------------------------------------- |
| Not in git repo        | `git rev-parse` fails       | "Error: Not in a git repository"               |
| Worktree exists        | `os.Stat(path)` succeeds    | "Error: Worktree 'name' already exists"        |
| Session exists         | `tmux has-session` succeeds | "Error: Tmux session 'wt-name' already exists" |
| OpenCode not installed | Command execution fails     | "Error: Failed to launch opencode"             |

---

## Dependencies

All dependencies already exist in devgeta:

- `internal/apps/git/git.go` - Git operations
- `internal/apps/tmux/tmux.go` - Tmux operations
- `internal/apps/opencode/opencode.go` - OpenCode launch
- `github.com/spf13/cobra` - CLI framework
- `pkg/utils` - Error handling utilities

---

## Implementation Checklist

### MVP Tasks

- [ ] Add `CreateWorktree()`, `ListWorktrees()`, `RemoveWorktree()` to `git.go`
- [ ] Add `CreateSession()`, `KillSession()`, `HasSession()`, `SendKeys()` to `tmux.go`
- [ ] Create `internal/tooling/worktree/worktree.go` coordinator
- [ ] Create `cmd/worktree.go` with create/list/remove commands
- [ ] Add tests for new git methods
- [ ] Add tests for new tmux methods
- [ ] Add tests for worktree coordinator
- [ ] Manual testing on macOS
- [ ] Manual testing on Debian/Ubuntu

### Post-MVP (Future)

- [ ] `--prompt` flag for initial OpenCode prompt
- [ ] `--from <branch>` flag
- [ ] `dg worktree jump` with fzf
- [ ] `dg worktree cleanup` bulk removal
- [ ] Uncommitted changes warnings

---

## Timeline Estimate

**MVP**: 4-6 hours

- Git methods: 1 hour
- Tmux methods: 1 hour
- Worktree coordinator: 1-2 hours
- Cobra command: 30 min
- Tests: 1-2 hours

---

## References

- [Git Worktree Documentation](https://git-scm.com/docs/git-worktree)
- [Tmux Manual](https://man7.org/linux/man-pages/man1/tmux.1.html)
- [devgeta Testing Patterns](../guides/testing-patterns.md)
- [devgeta Languages Coordinator](../../internal/tooling/languages/languages.go) - Pattern reference

---

**Status**: Superseded — Done via later cycles
**Last Updated**: 2026-07-06

**Superseded by:**

- [2026-05-01-worktree-multi-ai-and-panel.md](2026-05-01-worktree-multi-ai-and-panel.md) (Done) —
  multi-AI launcher, centralized storage, `--from`/`--prompt`-equivalent flags, enhanced jump dialog
- [2026-06-07-worktree-v2-tui-dashboard.md](2026-06-07-worktree-v2-tui-dashboard.md) (Done) —
  TUI dashboard (`dg wt ui`) replacing the fzf jump flow, plus `repair`/`prune`

This doc's MVP (`create`/`list`/`remove`) shipped essentially as scoped in `cmd/worktree.go`; every
item under "What MVP Does NOT Include" (`--prompt`, `--from`, `jump`, `cleanup`/bulk removal) was
picked up and completed by the two cycles above. Left here for historical context only — do not
use as an active spec.
