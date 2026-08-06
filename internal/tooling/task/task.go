// Package task provides developer utility commands for git branch management
// and npm dependency management, callable by both agents (as a real executable)
// and humans (via the dge shell wrapper).
package task

import (
	"fmt"
	"os"
	"path/filepath"

	git_app "github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/opencode"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/fzf"
)

// TaskManager executes developer utility tasks.
// Git operations go through the Git app; npm operations go through Base.
type TaskManager struct {
	Git  *git_app.Git
	Base cmd.BaseCommandExecutor
	Fzf  *fzf.Fzf
	// OpenCode runs headless reviewer agents for review-run. It is the app
	// wrapper, not a raw executor, because CLAUDE.md §6 keeps the
	// `opencode run` command line assembled in exactly one place.
	OpenCode *opencode.OpenCode
}

// New creates a TaskManager with real executors. Git output is streamed so
// humans (via dge) and agents see real-time progress from these utilities.
func New() *TaskManager {
	git := git_app.New()
	git.Stream = true
	return &TaskManager{
		Git:      git,
		Base:     cmd.NewBaseCommand(),
		Fzf:      fzf.New(),
		OpenCode: opencode.New(),
	}
}

// resolveHead resolves HEAD once for every guard that cares which branch is
// checked out: release's "must be ON the default branch" and review-run's
// "must NOT be" both read the answer from here rather than each calling git
// and each deciding for itself what an empty branch name means.
//
// current is "" when HEAD is detached — `git branch --show-current` prints
// nothing and exits 0, so a guard written only as `current != defaultBranch`
// silently lets a detached HEAD through. Callers must test for "" explicitly.
func (tm *TaskManager) resolveHead() (current, defaultBranch string, err error) {
	current, err = tm.Git.CurrentBranch()
	if err != nil {
		return "", "", err
	}
	return current, tm.Git.DefaultBranch(), nil
}

// RefreshBranch checks out target (default "main"), pulls, returns to the previous
// branch, and merges target into it — equivalent to the dge refresh-branch shell task.
func (tm *TaskManager) RefreshBranch(target string) error {
	if target == "" {
		target = "main"
	}
	if err := tm.Git.SwitchBranch(target); err != nil {
		return fmt.Errorf("refresh-branch: %w", err)
	}
	if err := tm.Git.Pull(target); err != nil {
		return fmt.Errorf("refresh-branch: %w", err)
	}
	if err := tm.Git.SwitchBranch("-"); err != nil {
		return fmt.Errorf("refresh-branch: %w", err)
	}
	if err := tm.Git.ExecuteCommand("merge", target); err != nil {
		return fmt.Errorf("refresh-branch: %w", err)
	}
	return nil
}

// ResetMainBranch checks out main and hard-resets to origin/main.
func (tm *TaskManager) ResetMainBranch() error {
	if err := tm.Git.SwitchBranch("main"); err != nil {
		return fmt.Errorf("reset-main-branch: %w", err)
	}
	if err := tm.Git.ExecuteCommand("reset", "--hard", "origin/main"); err != nil {
		return fmt.Errorf("reset-main-branch: %w", err)
	}
	return nil
}

// ReinstallLibraries removes all cached/ignored build artifacts and node_modules,
// then runs npm install — equivalent to the dge reinstall-libraries shell task.
func (tm *TaskManager) ReinstallLibraries() error {
	if err := tm.Git.ExecuteCommand("clean", "-Xdf"); err != nil {
		return fmt.Errorf("reinstall-libraries: %w", err)
	}
	if err := os.RemoveAll("node_modules"); err != nil {
		return fmt.Errorf("reinstall-libraries: failed to remove node_modules: %w", err)
	}
	if _, _, err := tm.Base.ExecCommand(cmd.CommandParams{
		Command: "npm",
		Args:    []string{"install"},
		Stream:  true,
	}); err != nil {
		return fmt.Errorf("reinstall-libraries: npm install failed: %w", err)
	}
	_ = os.Remove("tsconfig.tsbuildinfo")
	return nil
}

// ReinstallLibrary removes a single package from node_modules and re-runs npm install.
func (tm *TaskManager) ReinstallLibrary(name string) error {
	if name == "" {
		return fmt.Errorf("library name is required")
	}
	if err := os.RemoveAll(filepath.Join("node_modules", name)); err != nil {
		return fmt.Errorf("reinstall-library: failed to remove node_modules/%s: %w", name, err)
	}
	if _, _, err := tm.Base.ExecCommand(cmd.CommandParams{
		Command: "npm",
		Args:    []string{"install"},
		Stream:  true,
	}); err != nil {
		return fmt.Errorf("reinstall-library: npm install failed: %w", err)
	}
	return nil
}

// DeleteBranch checks out target (default "main"), fetches, pulls, then opens an
// interactive fzf picker to select and force-delete a local branch.
func (tm *TaskManager) DeleteBranch(target string) error {
	if target == "" {
		target = "main"
	}
	if err := tm.Git.SwitchBranch(target); err != nil {
		return fmt.Errorf("delete-branch setup: %w", err)
	}
	if err := tm.Git.FetchOrigin(); err != nil {
		return fmt.Errorf("delete-branch setup: %w", err)
	}
	if err := tm.Git.Pull(target); err != nil {
		return fmt.Errorf("delete-branch setup: %w", err)
	}

	branches, err := tm.Git.ListBranches()
	if err != nil {
		return fmt.Errorf("delete-branch: %w", err)
	}
	if len(branches) == 0 {
		return fmt.Errorf("delete-branch: no local branches to delete")
	}

	selected, err := tm.Fzf.SelectFromList(branches, "Select branch to delete:")
	if err != nil {
		return fmt.Errorf("delete-branch: selection cancelled: %w", err)
	}

	if err := tm.Git.DeleteBranch(selected, true); err != nil {
		return fmt.Errorf("delete-branch: %w", err)
	}
	return nil
}
