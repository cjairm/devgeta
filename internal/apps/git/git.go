// Git version control system with devgeta integration
//
// Git is the distributed version control system that tracks changes in source code
// during software development. This module provides installation and configuration
// management for Git with devgeta integration.
//
// References:
// - Git Documentation: https://git-scm.com/doc
// - Git Commands Reference: https://git-scm.com/docs
//
// Common Git commands available through ExecuteCommand():
//   - git status - Show working tree status
//   - git clone <url> <dir> - Clone repository
//   - git checkout <branch> - Switch branch
//   - git add . - Stage changes
//   - git commit -m "msg" - Commit changes
//   - git push origin <branch> - Push changes
//   - git pull origin <branch> - Pull changes
//   - git stash pop - Apply stashed changes
//   - git log - View commit history
//   - git clean -X -d -f - Clean ignored files

package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

var _ apps.App = (*Git)(nil)

// WorktreeInfo contains information about a git worktree.
type WorktreeInfo struct {
	Path   string
	Branch string
	Commit string
}

type Git struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
	// Stream, when true, tees git command output (clone/pull/fetch/merge/…) to
	// the terminal in real time. Used by `dg task` utilities so humans and
	// agents see progress as it happens. Commands whose output is parsed
	// (e.g. ListBranches) intentionally stay non-streaming.
	Stream bool
}

func (g *Git) Name() string       { return constants.Git }
func (g *Git) Kind() apps.AppKind { return apps.KindTerminal }

func New() *Git {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Git{Cmd: osCmd, Base: baseCmd}
}

func (g *Git) Install() error {
	return g.Cmd.InstallPackage(constants.Git)
}

func (g *Git) SoftInstall() error {
	return g.Cmd.MaybeInstallPackage(constants.Git)
}

func (g *Git) ForceInstall() error {
	return baseapp.Reinstall(g.Install, g.Uninstall)
}

func (g *Git) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := g.Cmd.UninstallPackage(constants.Git); err != nil {
		return fmt.Errorf("failed to uninstall git: %w", err)
	}
	_ = os.RemoveAll(paths.Paths.Config.Git)
	gc.RemoveFromInstalled(constants.Git, "package")
	return gc.Save()
}

func (g *Git) ForceConfigure() error {
	if err := files.CopyDir(paths.Paths.App.Configs.Git, paths.Paths.Config.Git); err != nil {
		return err
	}
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Git, "package")
	return gc.Save()
}

func (g *Git) SoftConfigure() error {
	configFile := filepath.Join(paths.Paths.Config.Git, ".gitconfig")
	if files.FileAlreadyExist(configFile) {
		return nil
	}
	return files.CopyDir(paths.Paths.App.Configs.Git, paths.Paths.Config.Git)
}

func (g *Git) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Git,
		Args:    args,
		Stream:  g.Stream,
	}
	if _, stderr, err := g.Base.ExecCommand(execCommand); err != nil {
		if stderr != "" {
			return fmt.Errorf("git: %s", stderr)
		}
		return fmt.Errorf("failed to run git command: %w", err)
	}
	return nil
}

// ExecuteCommandAt runs a git command with -C <dir> so it operates in the
// given directory regardless of the process's current working directory.
func (g *Git) ExecuteCommandAt(dir string, args ...string) error {
	fullArgs := append([]string{"-C", dir}, args...)
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Git,
		Args:    fullArgs,
		Stream:  g.Stream,
	}
	if _, stderr, err := g.Base.ExecCommand(execCommand); err != nil {
		if stderr != "" {
			return fmt.Errorf("git: %s", stderr)
		}
		return fmt.Errorf("failed to run git command at %s: %w", dir, err)
	}
	return nil
}

func (g *Git) Clone(url, dstPath string) error {
	return g.ExecuteCommand("clone", url, dstPath)
}

func (g *Git) DeleteBranch(branch string, isForced bool) error {
	deleteArg := "-d"
	if isForced {
		deleteArg = "-D"
	}
	return g.ExecuteCommand("branch", deleteArg, branch)
}

func (g *Git) DeepClean(url, dstPath string) error {
	// -X: This option tells Git to remove only the files that are ignored by Git (i.e., files that are listed in your .gitignore file). It does not remove untracked files that are not ignored.
	// -d: This option allows Git to remove untracked directories in addition to untracked files.
	// -f: This option stands for "force." Git requires this option to actually perform the clean operation, as a safety measure to prevent accidental data loss.
	return g.ExecuteCommand("clean", "-X", "-d", "-f")
}

func (g *Git) FetchOrigin() error {
	return g.ExecuteCommand("fetch", "origin")
}

// FetchOriginTimeout runs `git fetch origin` bounded by timeout, so a hung
// network call can't block a caller expecting a fast response (e.g.
// TaskManager.ReviewScope). A zero timeout is unbounded, same as FetchOrigin.
func (g *Git) FetchOriginTimeout(timeout time.Duration) error {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    []string{"fetch", "origin"},
		Stream:  g.Stream,
		Timeout: timeout,
	}
	if _, stderr, err := g.Base.ExecCommand(execCommand); err != nil {
		if stderr != "" {
			return fmt.Errorf("git: %s", stderr)
		}
		return fmt.Errorf("failed to run git command: %w", err)
	}
	return nil
}

func (g *Git) Pop(branch string) error {
	return g.ExecuteCommand("stash", "pop")
}

func (g *Git) Pull(branch string) error {
	if branch == "" {
		return g.ExecuteCommand("pull")
	}
	return g.ExecuteCommand("pull", "origin", branch)
}

func (g *Git) SwitchBranch(branch string) error {
	return g.ExecuteCommand("checkout", branch)
}

func (g *Git) Restore(branch, files string) error {
	if branch == "" {
		branch = "main"
	}
	return g.ExecuteCommand("restore", "--source", branch, "--", files)
}

func (g *Git) Update() error {
	return fmt.Errorf("%w for git", apps.ErrUpdateNotSupported)
}

// ListBranches returns all local branch names, stripping the current-branch
// marker (* ) and surrounding whitespace.
func (g *Git) ListBranches() ([]string, error) {
	stdout, _, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Git,
		Args:    []string{"branch"},
	})
	if err != nil {
		return nil, fmt.Errorf("git: failed to list branches: %w", err)
	}
	var branches []string
	for line := range strings.SplitSeq(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

// BranchExists checks if a branch exists in the repository
func (g *Git) BranchExists(branch string) (bool, error) {
	return g.BranchExistsIn("", branch)
}

// BranchExistsIn is BranchExists evaluated against the repository at dir
// ("" = current directory).
func (g *Git) BranchExistsIn(dir, branch string) (bool, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    dirArgs(dir, "branch", "--list", branch),
	}
	stdout, _, err := g.Base.ExecCommand(execCommand)
	if err != nil {
		return false, err
	}
	// If output contains the branch name, it exists
	return strings.TrimSpace(stdout) != "", nil
}

// RemoteBranchExists checks if a remote branch exists (e.g., origin/feature-A)
func (g *Git) RemoteBranchExists(branch string) (bool, error) {
	return g.RemoteBranchExistsIn("", branch)
}

// RemoteBranchExistsIn is RemoteBranchExists evaluated against the repository
// at dir ("" = current directory).
func (g *Git) RemoteBranchExistsIn(dir, branch string) (bool, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    dirArgs(dir, "branch", "-r", "--list", fmt.Sprintf("origin/%s", branch)),
	}
	stdout, _, err := g.Base.ExecCommand(execCommand)
	if err != nil {
		return false, err
	}
	// If output contains the remote branch name, it exists
	return strings.TrimSpace(stdout) != "", nil
}

// dirArgs prefixes git args with "-C dir" when dir is non-empty, so the same
// command can run against the current directory or an arbitrary repo path.
func dirArgs(dir string, args ...string) []string {
	if dir == "" {
		return args
	}
	return append([]string{"-C", dir}, args...)
}

// CurrentBranch returns the checked-out branch name, or "" when HEAD is
// detached (mirrors `git branch --show-current`).
func (g *Git) CurrentBranch() (string, error) {
	return g.CurrentBranchIn("")
}

// CurrentBranchIn is CurrentBranch evaluated against the repository at dir
// ("" = current directory).
func (g *Git) CurrentBranchIn(dir string) (string, error) {
	stdout, _, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Git,
		Args:    dirArgs(dir, "branch", "--show-current"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(stdout), nil
}

// ShortHead returns HEAD's short commit SHA (mirrors `git rev-parse --short HEAD`).
func (g *Git) ShortHead() (string, error) {
	stdout, _, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Git,
		Args:    []string{"rev-parse", "--short", "HEAD"},
	})
	if err != nil {
		return "", fmt.Errorf("failed to resolve HEAD: %w", err)
	}
	return strings.TrimSpace(stdout), nil
}

// RunCapture runs a git command and returns its stdout, for callers (e.g.
// `dg task`) that need to parse output rather than just check for an error.
func (g *Git) RunCapture(args ...string) (string, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    args,
	}
	stdout, stderr, err := g.Base.ExecCommand(execCommand)
	if err != nil {
		if stderr != "" {
			return stdout, fmt.Errorf("git: %s", stderr)
		}
		return stdout, fmt.Errorf("failed to run git command: %w", err)
	}
	return stdout, nil
}

// defaultBranchProbeOrder is tried, in order, when origin/HEAD is unset —
// covers the common default-branch names beyond "main".
var defaultBranchProbeOrder = []string{"main", "master", "develop"}

// DefaultBranch returns the repository's default branch name (e.g. "main").
// It resolves origin/HEAD when available; when unset it probes
// origin/main, origin/master, origin/develop in order via RemoteBranchExists
// and returns the first that exists, falling back to "main" as a last resort
// so callers always get a usable branch name.
func (g *Git) DefaultBranch() string {
	return g.DefaultBranchIn("")
}

// DefaultBranchIn is DefaultBranch evaluated against the repository at dir
// ("" = current directory).
func (g *Git) DefaultBranchIn(dir string) string {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    dirArgs(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"),
	}
	stdout, _, err := g.Base.ExecCommand(execCommand)
	if err == nil {
		// Output looks like "origin/main"; strip the remote prefix.
		ref := strings.TrimSpace(stdout)
		if i := strings.LastIndex(ref, "/"); i != -1 {
			ref = ref[i+1:]
		}
		if ref != "" {
			return ref
		}
	}

	for _, candidate := range defaultBranchProbeOrder {
		if exists, probeErr := g.RemoteBranchExistsIn(dir, candidate); probeErr == nil && exists {
			return candidate
		}
	}
	return "main"
}

// CreateWorktree creates a new worktree with a branch
// Handles three cases:
// 1. Local branch exists: checkout that branch
// 2. Remote branch exists: create tracking branch (after fetch)
// 3. Neither exists: create new branch from the freshly-fetched default branch
func (g *Git) CreateWorktree(path, branch string) error {
	return g.CreateWorktreeIn("", path, branch)
}

// CreateWorktreeIn is CreateWorktree evaluated against the repository at
// repoDir ("" = current directory), so worktrees can be created for a repo
// the caller is not inside.
func (g *Git) CreateWorktreeIn(repoDir, path, branch string) error {
	// Fetch latest remote refs to ensure we see recent branches.
	// Best-effort: ignore errors (user may be offline or have no remote).
	_ = g.ExecuteCommand(dirArgs(repoDir, "fetch", "origin")...)

	// Check if local branch already exists
	localExists, err := g.BranchExistsIn(repoDir, branch)
	if err != nil {
		return fmt.Errorf("failed to check if local branch exists: %w", err)
	}

	// If local branch exists, free it first if another worktree (typically the
	// main clone) already has it checked out — git refuses to check out a
	// branch that's checked out elsewhere — then check it out in the worktree
	// and bring it up to date with its remote counterpart.
	if localExists {
		if err := g.freeBranchIfHeldElsewhere(repoDir, path, branch); err != nil {
			return err
		}
		if err := g.ExecuteCommand(
			dirArgs(repoDir, "worktree", "add", path, branch)...,
		); err != nil {
			return err
		}
		return g.syncExistingBranch(path, branch)
	}

	// Check if remote branch exists
	remoteExists, err := g.RemoteBranchExistsIn(repoDir, branch)
	if err != nil {
		return fmt.Errorf("failed to check if remote branch exists: %w", err)
	}

	// If remote branch exists, checkout it (creates tracking branch automatically)
	if remoteExists {
		return g.ExecuteCommand(dirArgs(repoDir, "worktree", "add", path, branch)...)
	}

	// Neither local nor remote exists: create a new branch. Prefer basing it on
	// the freshly-fetched default branch (origin/<default>) so new worktrees are
	// deterministic and never inherit a stale or unrelated HEAD. Fall back to
	// HEAD when the remote default isn't available (e.g. offline, no origin).
	defaultBranch := g.DefaultBranchIn(repoDir)
	baseExists, err := g.RemoteBranchExistsIn(repoDir, defaultBranch)
	if err != nil {
		return fmt.Errorf("failed to check if remote default branch exists: %w", err)
	}
	if baseExists {
		base := fmt.Sprintf("origin/%s", defaultBranch)
		return g.ExecuteCommand(dirArgs(repoDir, "worktree", "add", path, "-b", branch, base)...)
	}
	return g.ExecuteCommand(dirArgs(repoDir, "worktree", "add", path, "-b", branch)...)
}

// freeBranchIfHeldElsewhere checks whether branch is already checked out in
// another worktree of the repo at repoDir (typically the main clone) and, if
// so, frees it by switching that checkout to the repo's default branch. Git
// refuses `worktree add` for a branch checked out elsewhere, so without this
// an ordinary "I made a branch, now give it a worktree" flow dead-ends.
//
// Returns nil when there is nothing to free — branch held nowhere, held by
// the target path itself (can't happen in practice: worktree.go rejects an
// existing worktree before git is invoked, but guarded defensively here), or
// branch is the repo default (adopting the default branch into a side
// worktree isn't a real workflow, so we leave the existing git error to
// surface) — and the caller proceeds to `worktree add` exactly as before.
func (g *Git) freeBranchIfHeldElsewhere(repoDir, path, branch string) error {
	worktrees, err := g.ListWorktreesAt(repoDir)
	if err != nil {
		return fmt.Errorf("failed to list worktrees at %s: %w", repoDir, err)
	}

	var holderPath string
	for _, wt := range worktrees {
		if wt.Branch == branch {
			holderPath = wt.Path
			break
		}
	}
	if holderPath == "" || holderPath == path {
		return nil
	}

	// Only compute the default branch once we know there's actually a holder
	// to free — this check is meaningless (and the git call wasted) otherwise.
	defaultBranch := g.DefaultBranchIn(repoDir)
	if branch == defaultBranch {
		return nil
	}

	dirty, err := g.IsWorktreeDirty(holderPath)
	if err != nil {
		return fmt.Errorf("failed to check worktree status at %s: %w", holderPath, err)
	}
	if dirty {
		return fmt.Errorf(
			"branch %q has uncommitted changes in %s; commit or stash them, then retry",
			branch, holderPath,
		)
	}

	if err := g.ExecuteCommandAt(holderPath, "checkout", defaultBranch); err != nil {
		return fmt.Errorf("failed to switch %s off %q: %w", holderPath, branch, err)
	}
	fmt.Printf(
		"Note: source checkout at %s was moved to %s so %q could be adopted into the new worktree.\n",
		holderPath,
		defaultBranch,
		branch,
	)
	return nil
}

// syncExistingBranch brings a worktree's already-existing local branch up to
// date with its remote counterpart. It only fast-forwards, so unpushed local
// commits are never discarded. When there is no remote counterpart there is
// nothing to sync. When histories have diverged the fast-forward fails; we
// leave the branch untouched and warn the user how to reconcile manually
// (logger is suppressed below ERROR in normal runs, so we print directly).
func (g *Git) syncExistingBranch(path, branch string) error {
	// Check refs through the worktree itself so this works no matter which
	// repository (if any) the process's working directory is in.
	remoteExists, err := g.RemoteBranchExistsIn(path, branch)
	if err != nil {
		return fmt.Errorf("failed to check if remote branch exists: %w", err)
	}
	if !remoteExists {
		return nil
	}

	base := fmt.Sprintf("origin/%s", branch)
	if ffErr := g.ExecuteCommandAt(path, "merge", "--ff-only", base); ffErr != nil {
		fmt.Printf(
			"Warning: local branch %q diverged from %s and was not updated.\n"+
				"  The worktree was created at the branch's current local state.\n"+
				"  If the local branch has no unique work, sync it with:\n"+
				"    git -C %s fetch origin %s\n"+
				"    git -C %s reset --hard %s\n",
			branch, base, path, branch, path, base,
		)
	}
	return nil
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

// ListWorktreesAt lists worktrees for the git repository at the given directory.
// This avoids depending on the current working directory.
func (g *Git) ListWorktreesAt(dir string) ([]WorktreeInfo, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    []string{"-C", dir, "worktree", "list", "--porcelain"},
	}
	stdout, _, err := g.Base.ExecCommand(execCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees at %s: %w", dir, err)
	}
	return parseWorktreeOutput(stdout), nil
}

// RemoveWorktree removes a worktree and optionally its associated branch.
// Resolves the main worktree first so the remove command doesn't run from
// within the worktree being deleted.
func (g *Git) RemoveWorktree(path string, deleteBranch bool, branchName string) error {
	// Find the main worktree by resolving git-common-dir from the target path
	mainWorktree, err := g.GetMainWorktree(path)
	if err != nil {
		return fmt.Errorf("cannot resolve main worktree for %s: %w", path, err)
	}

	// Remove the worktree from the main worktree context
	if err := g.ExecuteCommandAt(mainWorktree, "worktree", "remove", path); err != nil {
		return err
	}

	// Delete the branch if requested
	if deleteBranch && branchName != "" {
		if err := g.ExecuteCommandAt(mainWorktree, "branch", "-D", branchName); err != nil {
			return fmt.Errorf(
				"removed worktree but failed to delete branch '%s': %w",
				branchName,
				err,
			)
		}
	}

	return nil
}

// GetMainWorktree resolves the main worktree (repo root) path from any
// worktree in the repo, via `git worktree list --porcelain`'s first
// "worktree <path>" line (always the main worktree). Exported so callers
// outside this package (e.g. the worktree tooling's repo-candidate
// resolution) can reuse the same mechanism instead of duplicating it.
func (g *Git) GetMainWorktree(fromPath string) (string, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    []string{"-C", fromPath, "worktree", "list", "--porcelain"},
	}
	stdout, _, err := g.Base.ExecCommand(execCommand)
	if err != nil {
		return "", err
	}
	// First "worktree <path>" line is always the main worktree
	for line := range strings.SplitSeq(stdout, "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			return path, nil
		}
	}
	return "", fmt.Errorf("could not find main worktree")
}

// GetRepoRoot returns the root directory of the current git repository
func (g *Git) GetRepoRoot() (string, error) {
	return g.GetRepoRootIn("")
}

// GetRepoRootIn is GetRepoRoot evaluated against the repository at dir
// ("" = current directory). It also validates that dir is inside a git repo.
func (g *Git) GetRepoRootIn(dir string) (string, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    dirArgs(dir, "rev-parse", "--show-toplevel"),
	}
	stdout, _, err := g.Base.ExecCommand(execCommand)
	if err != nil {
		return "", fmt.Errorf("failed to get repo root: %w", err)
	}
	return strings.TrimSpace(stdout), nil
}

// IsWorktreeDirty checks if a worktree has uncommitted changes
func (g *Git) IsWorktreeDirty(path string) (bool, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    []string{"-C", path, "status", "--porcelain"},
	}
	stdout, _, err := g.Base.ExecCommand(execCommand)
	if err != nil {
		return false, fmt.Errorf("failed to check worktree status: %w", err)
	}
	return strings.TrimSpace(stdout) != "", nil
}

// PruneWorktrees removes stale worktree entries
func (g *Git) PruneWorktrees() error {
	return g.ExecuteCommand("worktree", "prune")
}

// PruneWorktreesAt removes stale worktree entries, running from the given directory.
func (g *Git) PruneWorktreesAt(dir string) error {
	return g.ExecuteCommandAt(dir, "worktree", "prune")
}

// CheckHookCompatibility scans the repo's effective hooks directory for scripts
// that use `[ -d .git ]` or `test -d .git`. In a git worktree the .git entry is
// a FILE, not a directory, so those checks always fail and block git commit.
//
// Also detects Affiance hooks which have a known bug where the .git file regex
// fails to match due to trailing newlines, causing "no .git directory found".
// See: https://github.com/mariusbutuc/affiance/issues/XXX
//
// Returns one warning string per offending hook file, or nil if all clear.
func (g *Git) CheckHookCompatibility(repoRoot string) []string {
	hooksDir := g.hooksDir(repoRoot)

	hookFiles := []string{
		"pre-commit",
		"commit-msg",
		"prepare-commit-msg",
		"post-commit",
		"pre-push",
	}
	incompatiblePatterns := []string{"[ -d .git", "test -d .git"}

	var warnings []string

	// Check for Affiance hooks (known worktree incompatibility)
	affianceHook := filepath.Join(hooksDir, "affiance-hook")
	if _, err := os.Stat(affianceHook); err == nil {
		warnings = append(
			warnings,
			"affiance-hook (Affiance has a bug parsing .git files in worktrees; use --no-verify to bypass)",
		)
		// If Affiance is present, all hooks delegate to it, so skip individual checks
		return warnings
	}

	for _, hookFile := range hookFiles {
		content, err := os.ReadFile(filepath.Join(hooksDir, hookFile))
		if err != nil {
			continue
		}
		contentStr := string(content)
		for _, pattern := range incompatiblePatterns {
			if strings.Contains(contentStr, pattern) {
				warnings = append(warnings, fmt.Sprintf("%s (contains %q)", hookFile, pattern))
				break
			}
		}
	}
	return warnings
}

// hooksDir returns the effective hooks directory for repoRoot.
// Respects core.hooksPath if configured; falls back to <repoRoot>/.git/hooks.
func (g *Git) hooksDir(repoRoot string) string {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    []string{"-C", repoRoot, "config", "--get", "core.hooksPath"},
	}
	if stdout, _, err := g.Base.ExecCommand(execCommand); err == nil {
		p := strings.TrimSpace(stdout)
		if p != "" {
			if !filepath.IsAbs(p) {
				p = filepath.Join(repoRoot, p)
			}
			return p
		}
	}
	return filepath.Join(repoRoot, ".git", "hooks")
}

// parseWorktreeOutput parses the porcelain output of git worktree list
func parseWorktreeOutput(output string) []WorktreeInfo {
	var worktrees []WorktreeInfo
	var current WorktreeInfo

	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = WorktreeInfo{}
			}
			continue
		}

		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path, _ = strings.CutPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			current.Commit, _ = strings.CutPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			branchRef, _ := strings.CutPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(branchRef, "refs/heads/")
		}
	}

	// Don't forget the last worktree if output doesn't end with blank line
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}
