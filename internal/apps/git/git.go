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
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	"github.com/cjairm/devgeta/pkg/utils"
)

var _ apps.App = (*Git)(nil)

// ErrBranchDeleteFailed marks a RemoveWorktree failure in which the WORKTREE
// was removed successfully and only the follow-up branch deletion failed.
//
// The distinction is load-bearing for callers with a filesystem fallback:
// "git refused to remove the worktree" means the directory is still there and
// removing it by hand leaves git holding a stale registration to prune, while
// this error means the directory and its registration are already gone and
// there is nothing to clean up - only a branch left behind. Callers that
// cannot tell the two apart end up either pruning for no reason or, worse,
// swallowing this error as success because their fallback os.RemoveAll
// trivially "succeeds" on a path git already deleted.
var ErrBranchDeleteFailed = errors.New("worktree removed but branch deletion failed")

// WorktreeInfo contains information about a git worktree.
//
// Prunable mirrors the "prunable" line `git worktree list --porcelain` emits
// for a registration whose working directory git can no longer find (it was
// deleted or moved out from under git). Such an entry is administrative
// debris, not a worktree: every command run against its Path fails with
// "cannot change to '<path>': No such file or directory". Callers that
// enumerate worktrees for display or for a mutation MUST honor this field —
// dropping it (as this parser once did) is what let deleted worktrees keep
// appearing in `dg wt list` and the `dg ws` dashboard forever.
type WorktreeInfo struct {
	Path   string
	Branch string
	Commit string
	// Prunable reports that git flagged this registration as stale — its
	// directory is gone and `git worktree prune` would remove it.
	Prunable bool
	// PrunableReason is git's own explanation (e.g. "gitdir file points to
	// non-existent location"), empty when Prunable is false or when git
	// emitted the marker with no reason.
	PrunableReason string
}

type Git struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
	// Stream, when true, tees git command output (clone/pull/fetch/merge/…) to
	// the terminal in real time. Used by `dg task` utilities so humans and
	// agents see progress as it happens. Commands whose output is parsed
	// (e.g. ListBranches) intentionally stay non-streaming.
	Stream bool
	// WarnFn reports a non-fatal advisory that the user should see but that
	// must not fail the operation (e.g. "your local branch diverged from
	// origin and was not fast-forwarded"). It defaults to a CLI-safe print in
	// New(); a caller rendering a TUI must override it, since printing
	// directly to stdout underneath a running Bubble Tea alt-screen program
	// corrupts the display. Use WorktreeManager.SetWarnFn to override this
	// together with the manager's own WarnFn — overriding only one of the two
	// is what let raw git advisories scribble over the `dg ws` dashboard.
	WarnFn func(msg string)
}

func (g *Git) Name() string       { return constants.Git }
func (g *Git) Kind() apps.AppKind { return apps.KindTerminal }

func New() *Git {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Git{Cmd: osCmd, Base: baseCmd, WarnFn: utils.PrintWarning}
}

// warn reports a non-fatal advisory via WarnFn, falling back to a plain
// stdout print when unset (a zero-value Git built by a test or a struct
// literal). Every advisory in this package goes through here rather than
// fmt.Print* directly, so a TUI caller that overrides WarnFn cannot be
// bypassed by one straggler call site.
func (g *Git) warn(msg string) {
	if g.WarnFn != nil {
		g.WarnFn(msg)
		return
	}
	utils.PrintWarning(msg)
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
	return g.fetchTimeout(timeout, "origin")
}

// FetchOriginRefspecsTimeout fetches ONLY the named refspecs from origin,
// bounded by timeout. Unlike FetchOriginTimeout it never touches the rest of
// the remote: each refspec names its own destination, so the caller decides
// exactly which refs are written and nothing else moves.
//
// Written for reviewing a pull request that is not checked out (ADR-0021 §1):
// a refspec such as "+refs/pull/12/head:refs/devgeta/pr/12/head" updates a
// non-branch ref, so no local branch moves, no upstream tracking changes, and
// the working tree is untouched. The leading "+" is what makes a second fetch
// survive a force-push on the source ref; callers that must NOT overwrite
// simply omit it. --no-tags keeps the fetch to what was asked for, rather than
// dragging in every tag that points into the fetched history.
func (g *Git) FetchOriginRefspecsTimeout(timeout time.Duration, refspecs ...string) error {
	if len(refspecs) == 0 {
		return fmt.Errorf("fetch requires at least one refspec")
	}
	return g.fetchTimeout(timeout, append([]string{"--no-tags", "origin"}, refspecs...)...)
}

// fetchTimeout runs `git fetch <args...>` bounded by timeout, with the error
// wrapping every fetch entry point shares. A zero timeout is unbounded.
func (g *Git) fetchTimeout(timeout time.Duration, args ...string) error {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    append([]string{"fetch"}, args...),
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

// MergeBase returns the best common ancestor of two refs (`git merge-base a b`).
//
// It is the base of any range that must equal what GitHub shows for a pull
// request: `git diff a..b` compares two ENDPOINTS, so when a is a branch tip
// that advanced after b forked off, everything merged into a in the meantime
// appears in the diff reversed, as if b were undoing it. Anchoring the range
// at the merge base removes that class of phantom change entirely.
func (g *Git) MergeBase(a, b string) (string, error) {
	out, err := g.RunCapture("merge-base", a, b)
	if err != nil {
		return "", fmt.Errorf("failed to resolve the merge base of %s and %s: %w", a, b, err)
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("%s and %s have no common ancestor", a, b)
	}
	return sha, nil
}

// ResolveCommit resolves ref to the full SHA of the commit it names
// (`git rev-parse --verify <ref>^{commit}`).
//
// The ^{commit} peel is the difference from a bare `rev-parse --verify`: it
// makes an annotated tag or any other non-commit object resolve to the commit
// it points at, and fail outright when it points at none. Callers that only
// need "does this ref exist" keep using RunCapture directly; this one exists
// for callers that need the SHA itself, because a ref name resolved twice
// during a long-running review can name two different commits.
func (g *Git) ResolveCommit(ref string) (string, error) {
	out, err := g.RunCapture("rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s to a commit: %w", ref, err)
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("failed to resolve %s to a commit: git returned no sha", ref)
	}
	return sha, nil
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
	return g.ShortHeadIn("")
}

// ShortHeadIn is ShortHead evaluated against the repository at dir
// ("" = current directory).
func (g *Git) ShortHeadIn(dir string) (string, error) {
	stdout, _, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Git,
		Args:    dirArgs(dir, "rev-parse", "--short", "HEAD"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to resolve HEAD: %w", err)
	}
	return strings.TrimSpace(stdout), nil
}

// CommonDirIn returns the repository's COMMON git directory as an absolute
// path — the main checkout's .git — from any checkout of the repo, main or
// linked worktree alike ("" = current directory).
//
// --path-format=absolute is load-bearing, not cosmetic: in the main checkout
// git answers the bare query with a RELATIVE ".git", which would silently
// anchor anything joined onto it to the caller's cwd instead of the repo
// (verified against git 2.x during ADR-0012's probes). And it must be
// --git-common-dir, never --git-dir: the latter resolves to the per-worktree
// directory (".git/worktrees/<name>"), which would split per-branch state
// (e.g. the ADR-0012 review journal) into one copy per checkout.
func (g *Git) CommonDirIn(dir string) (string, error) {
	stdout, _, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Git,
		Args:    dirArgs(dir, "rev-parse", "--path-format=absolute", "--git-common-dir"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to resolve the common git directory: %w", err)
	}
	return strings.TrimSpace(stdout), nil
}

// BranchForWorktree returns the branch checked out in the worktree at path.
//
// This is NOT the same as the worktree's directory name: devgeta flattens "/"
// out of a name to build the directory and tmux window (worktree.FlattenName),
// so branch "feat/login" lives in a directory called "feat-login". Anything
// keyed by the real branch — the ADR-0012 review journal, `branch -D` — must
// ask git rather than reuse the row/directory name.
func (g *Git) BranchForWorktree(path string) (string, error) {
	worktrees, err := g.ListWorktreesAt(path)
	if err != nil {
		return "", err
	}
	for _, wt := range worktrees {
		if wt.Path == path {
			return wt.Branch, nil
		}
	}
	return "", fmt.Errorf("could not determine branch for worktree %s", path)
}

// HashObjectIn returns the git blob hash of path's CURRENT working-tree
// content in the repository at dir ("" = current directory). Unlike anything
// keyed on HEAD, this identity moves with dirty and staged edits too — two
// different uncommitted versions of a file get two different hashes — which
// is exactly what ADR-0012's per-entry staleness needs: `git diff <sha>..HEAD`
// cannot see an uncommitted change, `git hash-object` cannot miss one.
func (g *Git) HashObjectIn(dir, path string) (string, error) {
	stdout, _, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Git,
		Args:    dirArgs(dir, "hash-object", "--", path),
	})
	if err != nil {
		return "", fmt.Errorf("failed to hash %s: %w", path, err)
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
//
// Every successful creation is followed by NormalizeWorktreeGitfile. That is
// deliberately here, at the single choke point, rather than at each of the
// `worktree add` calls below: the inner function has five separate success
// paths, and normalizing per-path is how one of them silently ends up missing
// the invariant later. See ADR-0013.
func (g *Git) CreateWorktreeIn(repoDir, path, branch string) error {
	if err := g.createWorktreeIn(repoDir, path, branch); err != nil {
		return err
	}
	return g.NormalizeWorktreeGitfile(path)
}

// CreateWorktreeAtBaseIn creates a worktree holding a NEW branch rooted at an
// explicit base ref, rather than letting createWorktreeIn pick the base (which
// prefers reusing an existing local/remote branch of the same name).
//
// It exists so callers that need an explicit base go through the git wrapper and
// pick up the same post-creation invariants as CreateWorktreeIn — before this,
// `devgeta task worktree-start --base` assembled its own `worktree add` and so
// was the one creation path that produced an un-normalized gitfile.
func (g *Git) CreateWorktreeAtBaseIn(repoDir, path, branch, base string) error {
	if err := g.ExecuteCommandAt(repoDir, "worktree", "add", "-b", branch, path, base); err != nil {
		return err
	}
	return g.NormalizeWorktreeGitfile(path)
}

// NormalizeWorktreeGitfile rewrites a linked worktree's .git file to carry no
// trailing newline — "gitdir: <path>" rather than the "gitdir: <path>\n" that
// `git worktree add` writes.
//
// Both forms are valid and git treats them identically: its own parser
// (read_gitfile_gently) trims trailing whitespace, and `rev-parse
// --absolute-git-dir`, `status`, and `commit` were all verified against a
// stripped gitfile before this was adopted. The no-newline form is simply the
// one that strict third-party parsers can read too, so writing it costs nothing
// and buys compatibility.
//
// The class of breakage it prevents is a hook framework that parses the gitfile
// with a regex anchored at end-of-line. Affiance (github.com/l8on/affiance,
// still broken in 1.8.0, the latest published version) matches it with
// /^gitdir: (.*)$/g in lib/gitRepo.js. JavaScript's `$` without the `m` flag
// matches only at end-of-input — NOT before a trailing newline, the way Perl and
// Python do — and `.` never matches a newline, so that pattern cannot match what
// git writes and gitDir() throws InvalidGitRepo("no .git directory found").
// It was not confined to merges: the pre-commit context's setupEnvironment()
// calls storeMergeState() -> mergeState() -> gitDir() unconditionally, so EVERY
// commit inside such a worktree failed while creating it appeared to succeed.
//
// Safe to call on any checkout and idempotent: it no-ops when path has no .git
// entry, when .git is a directory (a main checkout has nothing to normalize),
// and when the content is already trimmed.
func (g *Git) NormalizeWorktreeGitfile(path string) error {
	gitfile := filepath.Join(path, ".git")
	// Stat, not Lstat: a .git symlinked to a directory is still a main-checkout
	// shape with nothing to normalize, and should be skipped rather than read.
	info, err := os.Stat(gitfile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to inspect %s: %w", gitfile, err)
	}
	if info.IsDir() {
		return nil
	}
	content, err := os.ReadFile(gitfile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", gitfile, err)
	}
	trimmed := strings.TrimRight(string(content), " \t\r\n")
	if trimmed == string(content) {
		return nil
	}
	if err := files.WriteFileAtomic(gitfile, []byte(trimmed), info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to normalize %s: %w", gitfile, err)
	}
	return nil
}

func (g *Git) createWorktreeIn(repoDir, path, branch string) error {
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
		// A prunable registration holds nothing: its directory is already
		// gone, so there is no checkout to switch off the branch and no
		// working tree to inspect. Treating one as a live holder sends
		// IsWorktreeDirty below at a path that does not exist, which fails
		// with a message no caller recognizes as a stale-entry problem — so
		// create() dead-ends here instead of reaching its prune-and-retry.
		// Skipping it lets `git worktree add` produce its own "already used
		// by worktree at" error, which create() DOES recognize, prunes, and
		// retries successfully.
		if wt.Prunable {
			continue
		}
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
	g.warn(fmt.Sprintf(
		"Note: source checkout at %s was moved to %s so %q could be adopted into the new worktree.",
		holderPath,
		defaultBranch,
		branch,
	))
	return nil
}

// syncExistingBranch brings a worktree's already-existing local branch up to
// date with its remote counterpart. It only fast-forwards, so unpushed local
// commits are never discarded. When there is no remote counterpart there is
// nothing to sync. When histories have diverged the fast-forward fails; we
// leave the branch untouched and warn the user how to reconcile manually
// (logger is suppressed below ERROR in normal runs, so this goes through
// WarnFn — never a raw print, which would corrupt a TUI caller's display).
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
		g.warn(fmt.Sprintf(
			"Warning: local branch %q diverged from %s and was not updated.\n"+
				"  The worktree was created at the branch's current local state.\n"+
				"  If the local branch has no unique work, sync it with:\n"+
				"    git -C %s fetch origin %s\n"+
				"    git -C %s reset --hard %s",
			branch, base, path, branch, path, base,
		))
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
//
// A failure to delete the branch is wrapped with ErrBranchDeleteFailed, so a
// caller can tell it apart from "the worktree itself could not be removed" -
// by that point the worktree and its git registration are already gone, and
// treating the two the same makes a filesystem-fallback caller swallow this
// error as success (os.RemoveAll trivially succeeds on an already-deleted
// path).
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
				"%w: branch '%s': %w",
				ErrBranchDeleteFailed,
				branchName,
				err,
			)
		}
	}

	return nil
}

// MoveWorktree relocates a worktree on disk via `git worktree move`.
// Resolves the main worktree first so the move command doesn't run from
// within the worktree being relocated. Does not pass --force: git's
// refusal to move a locked worktree (or one with submodule complications)
// is the safety net and must reach the caller unchanged. Does not create
// the destination's parent directory; git requires it to exist and fails
// clearly if it doesn't.
func (g *Git) MoveWorktree(from, to string) error {
	mainWorktree, err := g.GetMainWorktree(from)
	if err != nil {
		return fmt.Errorf("cannot resolve main worktree for %s: %w", from, err)
	}

	if err := g.ExecuteCommandAt(mainWorktree, "worktree", "move", from, to); err != nil {
		return fmt.Errorf("failed to move worktree from %s to %s: %w", from, to, err)
	}

	return nil
}

// IsPathIgnored reports whether relPath (relative to repoRoot) is matched by
// a .gitignore rule in repoRoot, via `git check-ignore`. That command's exit
// code carries the answer: 0 means the path IS ignored, 1 means it is NOT (a
// normal, expected outcome - not a failure), and anything else is a real
// error (e.g. not a git repository). ExecCommand surfaces a non-zero exit as
// the raw *exec.ExitError from exec.Cmd.Wait(), so exit 1 is distinguished
// from a genuine failure by its ExitCode(), not by treating every non-nil
// error as "not ignored". Used by `dg wt move --to in-repo` to warn (never
// refuse - devgeta must never edit another repo's .gitignore itself) when
// the target repo doesn't already ignore its in-repo worktrees directory.
func (g *Git) IsPathIgnored(repoRoot, relPath string) (bool, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Git,
		Args:    []string{"-C", repoRoot, "check-ignore", "-q", relPath},
	}
	_, _, err := g.Base.ExecCommand(execCommand)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf(
		"failed to check gitignore status for %s in %s: %w",
		relPath,
		repoRoot,
		err,
	)
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
// It deliberately does NOT warn about hook frameworks that fail only because
// they parse the .git FILE with a too-strict regex (Affiance being the case that
// prompted this). Those are fixed at the source instead, by
// NormalizeWorktreeGitfile, so warning about them here would nag about a problem
// devgeta has already prevented. Only patterns devgeta cannot fix — a hook
// genuinely requiring .git to be a directory — belong in this list. See
// ADR-0013.
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
		// git emits the marker bare ("prunable") or with a reason
		// ("prunable gitdir file points to non-existent location"), so both
		// spellings are matched here rather than only the reason-bearing one.
		case line == "prunable":
			current.Prunable = true
		case strings.HasPrefix(line, "prunable "):
			current.Prunable = true
			current.PrunableReason, _ = strings.CutPrefix(line, "prunable ")
		}
	}

	// Don't forget the last worktree if output doesn't end with blank line
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}
