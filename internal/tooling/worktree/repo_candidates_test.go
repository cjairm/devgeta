package worktree

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/paths"
)

// failingLookPath and okLookPath swap commands.LookPathFn for the duration of
// a test, matching the pattern in internal/commands/base_test.go.
func setLookPathFn(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := commands.LookPathFn
	commands.LookPathFn = fn
	t.Cleanup(func() { commands.LookPathFn = orig })
}

func failingLookPath(t *testing.T) {
	t.Helper()
	setLookPathFn(t, func(string) (string, error) {
		return "", os.ErrNotExist
	})
}

func okLookPath(t *testing.T) {
	t.Helper()
	setLookPathFn(t, func(string) (string, error) {
		return "/usr/bin/zoxide", nil
	})
}

// setShellCommandExistsFn swaps commands.ShellCommandExistsFn for the duration
// of a test, so the coder/nvim install checks (which resolve tools through the
// interactive shell, not exec.LookPath) can be driven to present/absent without
// spawning a real shell. Same swap-and-restore pattern as setLookPathFn.
func setShellCommandExistsFn(t *testing.T, fn func(string) bool) {
	t.Helper()
	orig := commands.ShellCommandExistsFn
	commands.ShellCommandExistsFn = fn
	t.Cleanup(func() { commands.ShellCommandExistsFn = orig })
}

// newRecordingWM builds a WorktreeManager wired to fresh mocks, mirroring the
// construction pattern already used across worktree_test.go.
func newRecordingWM() (wm *WorktreeManager, mockGitBase, mockTmuxBase, mockBase *commands.MockBaseCommand) {
	mockGitBase = commands.NewMockBaseCommand()
	mockTmuxBase = commands.NewMockBaseCommand()
	mockBase = commands.NewMockBaseCommand()
	wm = &WorktreeManager{
		Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
		Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
		Base: mockBase,
	}
	return
}

// TestCreateRecordsRepoOnSuccess proves both entry points funnel through the
// shared create() flow: a successful Create upserts the canonical repo root
// into global_config.yaml's worktree.recent_repos.
func TestCreateRecordsRepoOnSuccess(t *testing.T) {
	cleanupPaths := testutil.SetupIsolatedPaths(t)
	defer cleanupPaths()

	repoRoot := t.TempDir()
	wm, mockGitBase, mockTmuxBase, _ := newRecordingWM()

	mockGitBase.SetExecCommandResults(
		commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
		commands.ExecCommandResult("", "", nil),            // everything else succeeds/empty
	)
	mockTmuxBase.SetExecCommandResult("", "", nil)

	repoSlug := filepath.Base(repoRoot)
	wtPath := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, "feature-test")
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	if err := wm.Create("feature-test", stubLayout, true); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("failed to load global config: %v", err)
	}
	if len(gc.Worktree.RecentRepos) != 1 {
		t.Fatalf(
			"expected 1 recent repo, got %d: %+v",
			len(gc.Worktree.RecentRepos),
			gc.Worktree.RecentRepos,
		)
	}
	wantPath := config.CanonicalRepoPath(repoRoot)
	if gc.Worktree.RecentRepos[0].Path != wantPath {
		t.Errorf("expected recorded path %q, got %q", wantPath, gc.Worktree.RecentRepos[0].Path)
	}
}

// TestCreateRecordsRepoThroughUpdatePreservesOtherFields proves the
// recent_repos write (now routed through config.Update, the Task-1
// load-under-lock-then-save transaction) does not clobber unrelated config
// state that was set before Create ran - e.g. a human's
// worktree.attach_after_create setting must survive a concurrent-ish
// worktree creation untouched.
func TestCreateRecordsRepoThroughUpdatePreservesOtherFields(t *testing.T) {
	cleanupPaths := testutil.SetupIsolatedPaths(t)
	defer cleanupPaths()

	// Seed a non-default value for an unrelated field before Create runs, to
	// prove the Update-based write is a true read-modify-write and not a
	// blind overwrite.
	seed := &config.GlobalConfig{}
	if err := seed.Create(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	attachFalse := false
	seed.Worktree.AttachAfterCreate = &attachFalse
	seed.CurrentFont = "JetBrainsMono"
	if err := seed.Save(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	repoRoot := t.TempDir()
	wm, mockGitBase, mockTmuxBase, _ := newRecordingWM()

	mockGitBase.SetExecCommandResults(
		commands.ExecCommandResult(repoRoot+"\n", "", nil),
		commands.ExecCommandResult("", "", nil),
	)
	mockTmuxBase.SetExecCommandResult("", "", nil)

	repoSlug := filepath.Base(repoRoot)
	wtPath := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, "feature-test")
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	if err := wm.Create("feature-test", stubLayout, true); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("failed to load global config: %v", err)
	}
	if len(gc.Worktree.RecentRepos) != 1 {
		t.Fatalf(
			"expected 1 recent repo, got %d: %+v",
			len(gc.Worktree.RecentRepos),
			gc.Worktree.RecentRepos,
		)
	}
	wantPath := config.CanonicalRepoPath(repoRoot)
	if gc.Worktree.RecentRepos[0].Path != wantPath {
		t.Errorf("expected recorded path %q, got %q", wantPath, gc.Worktree.RecentRepos[0].Path)
	}
	if gc.Worktree.AttachAfterCreate == nil || *gc.Worktree.AttachAfterCreate {
		t.Errorf(
			"expected attach_after_create to remain false after recording a repo, got %+v",
			gc.Worktree.AttachAfterCreate,
		)
	}
	if gc.CurrentFont != "JetBrainsMono" {
		t.Errorf("expected current_font to remain unclobbered, got %q", gc.CurrentFont)
	}
}

// TestCreateSucceedsDespiteRecordFailure proves the recent-repos write is
// truly best-effort: Create must still report success (the worktree and tmux
// window already exist) even when the store write fails, but the failure
// must be surfaced via WarnFn rather than silently swallowed.
func TestCreateSucceedsDespiteRecordFailure(t *testing.T) {
	cleanupPaths := testutil.SetupIsolatedPaths(t)
	defer cleanupPaths()

	// Capture the isolated config root now: cleanupPaths (deferred) restores
	// paths.Paths.Config.Root to its original value before t.Cleanup funcs run,
	// so a cleanup that re-reads the package variable would chmod the wrong
	// (real) directory instead of this test's temp one.
	configRoot := paths.Paths.Config.Root
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(configRoot, 0o555); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(configRoot, 0o755); err != nil {
			t.Logf("cleanup chmod: %v", err)
		}
	})

	repoRoot := t.TempDir()
	wm, mockGitBase, mockTmuxBase, _ := newRecordingWM()

	mockGitBase.SetExecCommandResults(
		commands.ExecCommandResult(repoRoot+"\n", "", nil),
		commands.ExecCommandResult("", "", nil),
	)
	mockTmuxBase.SetExecCommandResult("", "", nil)

	var warned bool
	var warnMsg string
	wm.WarnFn = func(msg string) {
		warned = true
		warnMsg = msg
	}

	repoSlug := filepath.Base(repoRoot)
	wtPath := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, "feature-test")
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	err := wm.Create("feature-test", stubLayout, true)
	if err != nil {
		t.Fatalf("Create must succeed even when repo recording fails: %v", err)
	}
	if !warned {
		t.Error("expected WarnFn to be invoked when the recent-repos write fails")
	}
	if warnMsg == "" {
		t.Error("expected a non-empty warning message")
	}
}

func TestRepoCandidates(t *testing.T) {
	t.Run("cursor repo resolved from a worktree directory", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		failingLookPath(t)

		wm, mockGitBase, _, mockBase := newRecordingWM()

		cursorRoot := filepath.Join(t.TempDir(), "cursor-repo")
		porcelain := "worktree " + cursorRoot + "\nHEAD abc123\nbranch refs/heads/main\n\n"
		mockGitBase.SetExecCommandResult(porcelain, "", nil)

		repoSlug := "cursor-slug"
		wtDir := filepath.Join(GetWorktreeBasePath(), repoSlug, "some-worktree")
		if err := os.MkdirAll(wtDir, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(filepath.Join(GetWorktreeBasePath(), repoSlug)); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		candidates, err := wm.RepoCandidates(repoSlug)
		if err != nil {
			t.Fatalf("RepoCandidates failed: %v", err)
		}
		want := config.CanonicalRepoPath(cursorRoot)
		if len(candidates) != 1 || candidates[0] != want {
			t.Fatalf("expected [%q], got %+v", want, candidates)
		}
		if mockBase.GetExecCommandCallCount() != 0 {
			t.Errorf("expected zoxide not to be queried when LookPathFn fails, got %d calls",
				mockBase.GetExecCommandCallCount())
		}
	})

	t.Run("cwd repo is suggested first when cwd is inside a git repo", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		failingLookPath(t)

		wm, mockGitBase, _, _ := newRecordingWM()

		cwdRoot := t.TempDir()
		t.Chdir(cwdRoot)
		actualCwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		cursorRoot := filepath.Join(t.TempDir(), "cursor-repo")
		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+actualCwd+"\nHEAD abc123\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree(cwd)
			commands.ExecCommandResult(
				"worktree "+cursorRoot+"\nHEAD abc123\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree(cursor worktree dir)
		)

		repoSlug := "cwd-slug"
		wtDir := filepath.Join(GetWorktreeBasePath(), repoSlug, "some-worktree")
		if err := os.MkdirAll(wtDir, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(filepath.Join(GetWorktreeBasePath(), repoSlug)); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		candidates, err := wm.RepoCandidates(repoSlug)
		if err != nil {
			t.Fatalf("RepoCandidates failed: %v", err)
		}
		wantCwd := config.CanonicalRepoPath(actualCwd)
		wantCursor := config.CanonicalRepoPath(cursorRoot)
		if len(candidates) != 2 || candidates[0] != wantCwd || candidates[1] != wantCursor {
			t.Fatalf("expected [%q, %q], got %+v", wantCwd, wantCursor, candidates)
		}

		if len(mockGitBase.ExecCommandCalls) == 0 {
			t.Fatal("expected at least one git call")
		}
		firstArgs := mockGitBase.ExecCommandCalls[0].Args
		if len(firstArgs) < 2 || firstArgs[0] != "-C" || firstArgs[1] != actualCwd {
			t.Errorf(
				"expected first git call to target cwd %q via -C, got args %+v",
				actualCwd,
				firstArgs,
			)
		}
	})

	t.Run("cwd falls back to today's behavior when it is not a git repo", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		failingLookPath(t)

		wm, mockGitBase, _, _ := newRecordingWM()

		t.Chdir(t.TempDir())
		mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)

		dirA := t.TempDir()
		canonicalA := config.CanonicalRepoPath(dirA)
		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			t.Fatalf("setup: %v", err)
		}
		gc.Worktree.RecentRepos = []config.RecentRepo{{Path: canonicalA, LastUsed: time.Now()}}
		if err := gc.Save(); err != nil {
			t.Fatalf("setup: %v", err)
		}

		candidates, err := wm.RepoCandidates("")
		if err != nil {
			t.Fatalf("RepoCandidates failed: %v", err)
		}
		if len(candidates) != 1 || candidates[0] != canonicalA {
			t.Fatalf(
				"expected [%q] (cwd skipped, existing sources unaffected), got %+v",
				canonicalA, candidates,
			)
		}
	})

	t.Run("empty cursor slug is skipped without error", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		failingLookPath(t)

		wm, _, _, _ := newRecordingWM()

		candidates, err := wm.RepoCandidates("")
		if err != nil {
			t.Fatalf("RepoCandidates failed: %v", err)
		}
		if len(candidates) != 0 {
			t.Fatalf("expected no candidates, got %+v", candidates)
		}
	})

	t.Run("recent repos included in MRU order", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		failingLookPath(t)

		wm, _, _, _ := newRecordingWM()

		dirA := t.TempDir()
		dirB := t.TempDir()
		canonicalA := config.CanonicalRepoPath(dirA)
		canonicalB := config.CanonicalRepoPath(dirB)

		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// Most-recently-used first: A was used after B.
		gc.Worktree.RecentRepos = []config.RecentRepo{
			{Path: canonicalA, LastUsed: time.Now()},
			{Path: canonicalB, LastUsed: time.Now().Add(-time.Hour)},
		}
		if err := gc.Save(); err != nil {
			t.Fatalf("setup: %v", err)
		}

		candidates, err := wm.RepoCandidates("")
		if err != nil {
			t.Fatalf("RepoCandidates failed: %v", err)
		}
		if len(candidates) != 2 || candidates[0] != canonicalA || candidates[1] != canonicalB {
			t.Fatalf("expected [%q, %q] in MRU order, got %+v", canonicalA, canonicalB, candidates)
		}
	})

	t.Run("zoxide results included only when zoxide is installed", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()

		t.Run("zoxide present", func(t *testing.T) {
			okLookPath(t)
			wm, _, _, mockBase := newRecordingWM()
			mockBase.SetExecCommandResult("/zoxide/repo-1\n/zoxide/repo-2\n", "", nil)

			candidates, err := wm.RepoCandidates("")
			if err != nil {
				t.Fatalf("RepoCandidates failed: %v", err)
			}
			want1 := config.CanonicalRepoPath("/zoxide/repo-1")
			want2 := config.CanonicalRepoPath("/zoxide/repo-2")
			if len(candidates) != 2 || candidates[0] != want1 || candidates[1] != want2 {
				t.Fatalf("expected [%q, %q], got %+v", want1, want2, candidates)
			}
			if mockBase.GetExecCommandCallCount() != 1 {
				t.Errorf(
					"expected exactly 1 zoxide query, got %d",
					mockBase.GetExecCommandCallCount(),
				)
			}
		})

		t.Run("zoxide absent", func(t *testing.T) {
			failingLookPath(t)
			wm, _, _, mockBase := newRecordingWM()

			candidates, err := wm.RepoCandidates("")
			if err != nil {
				t.Fatalf("RepoCandidates failed: %v", err)
			}
			if len(candidates) != 0 {
				t.Fatalf("expected no candidates when zoxide is absent, got %+v", candidates)
			}
			testutil.VerifyNoRealCommands(t, mockBase)
		})
	})

	t.Run("empty SearchPaths means scan contributes nothing", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		failingLookPath(t)

		wm, _, _, mockBase := newRecordingWM()

		// A repo exists on disk, but it's never named in SearchPaths (the
		// zero value / default global config), so scanRepos must never find
		// it: scanning is opt-in and this proves the opt-out (default) case
		// is truly zero behavior change, not just "recents/zoxide happen to
		// dominate the list".
		scanRoot := t.TempDir()
		repoDir := filepath.Join(scanRoot, "some-repo")
		if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}

		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// SearchPaths intentionally left unset (zero value).
		if err := gc.Save(); err != nil {
			t.Fatalf("setup: %v", err)
		}

		candidates, err := wm.RepoCandidates("")
		if err != nil {
			t.Fatalf("RepoCandidates failed: %v", err)
		}
		if len(candidates) != 0 {
			t.Fatalf("expected no candidates when SearchPaths is empty, got %+v", candidates)
		}
		testutil.VerifyNoRealCommands(t, mockBase)
	})

	t.Run("SearchPaths set discovers a repo via scan", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		failingLookPath(t)

		wm, _, _, mockBase := newRecordingWM()

		scanRoot := t.TempDir()
		repoDir := filepath.Join(scanRoot, "scanned-repo")
		if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		canonicalRepo := config.CanonicalRepoPath(repoDir)

		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			t.Fatalf("setup: %v", err)
		}
		gc.Worktree.SearchPaths = []string{scanRoot}
		if err := gc.Save(); err != nil {
			t.Fatalf("setup: %v", err)
		}

		candidates, err := wm.RepoCandidates("")
		if err != nil {
			t.Fatalf("RepoCandidates failed: %v", err)
		}
		if len(candidates) != 1 || candidates[0] != canonicalRepo {
			t.Fatalf("expected [%q], got %+v", canonicalRepo, candidates)
		}
		testutil.VerifyNoRealCommands(t, mockBase)
	})

	t.Run("scan result already offered by recents is not duplicated", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		failingLookPath(t)

		wm, _, _, mockBase := newRecordingWM()

		scanRoot := t.TempDir()
		repoDir := filepath.Join(scanRoot, "shared-repo")
		if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		canonicalRepo := config.CanonicalRepoPath(repoDir)

		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			t.Fatalf("setup: %v", err)
		}
		gc.Worktree.SearchPaths = []string{scanRoot}
		gc.Worktree.RecentRepos = []config.RecentRepo{
			{Path: canonicalRepo, LastUsed: time.Now()},
		}
		if err := gc.Save(); err != nil {
			t.Fatalf("setup: %v", err)
		}

		candidates, err := wm.RepoCandidates("")
		if err != nil {
			t.Fatalf("RepoCandidates failed: %v", err)
		}
		// Recents rank ahead of scan, and end-to-end dedup (not just
		// scanRepos' own internal dedup) must collapse the two sources'
		// identical repo into a single entry.
		if len(candidates) != 1 || candidates[0] != canonicalRepo {
			t.Fatalf("expected [%q] deduped, got %+v", canonicalRepo, candidates)
		}
		testutil.VerifyNoRealCommands(t, mockBase)
	})

	t.Run("dedup across sources preserves priority order", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		okLookPath(t)

		wm, mockGitBase, _, mockBase := newRecordingWM()

		shared := filepath.Join(t.TempDir(), "shared-repo")
		onlyRecent := t.TempDir()
		canonicalShared := config.CanonicalRepoPath(shared)
		canonicalOnlyRecent := config.CanonicalRepoPath(onlyRecent)

		// Cursor repo resolves to `shared`.
		porcelain := "worktree " + shared + "\nHEAD abc123\nbranch refs/heads/main\n\n"
		mockGitBase.SetExecCommandResult(porcelain, "", nil)
		repoSlug := "dedup-slug"
		wtDir := filepath.Join(GetWorktreeBasePath(), repoSlug, "some-worktree")
		if err := os.MkdirAll(wtDir, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(filepath.Join(GetWorktreeBasePath(), repoSlug)); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		// Recents contain the same repo (as `shared`, unresolved of any
		// worktree) plus one repo only recents knows about.
		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			t.Fatalf("setup: %v", err)
		}
		gc.Worktree.RecentRepos = []config.RecentRepo{
			{Path: canonicalShared, LastUsed: time.Now()},
			{Path: canonicalOnlyRecent, LastUsed: time.Now().Add(-time.Hour)},
		}
		if err := gc.Save(); err != nil {
			t.Fatalf("setup: %v", err)
		}

		// zoxide also reports the shared repo again.
		mockBase.SetExecCommandResult(shared+"\n", "", nil)

		candidates, err := wm.RepoCandidates(repoSlug)
		if err != nil {
			t.Fatalf("RepoCandidates failed: %v", err)
		}
		want := []string{canonicalShared, canonicalOnlyRecent}
		if len(candidates) != len(want) {
			t.Fatalf("expected %+v, got %+v", want, candidates)
		}
		for i := range want {
			if candidates[i] != want[i] {
				t.Fatalf("expected %+v, got %+v", want, candidates)
			}
		}
	})
}

func TestValidateRepoPath(t *testing.T) {
	t.Run("valid git repo returns resolved root", func(t *testing.T) {
		repoRoot := t.TempDir()
		mockGitBase := commands.NewMockBaseCommand()
		mockGitBase.SetExecCommandResult(repoRoot+"\n", "", nil)
		wm := &WorktreeManager{
			Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
			Base: commands.NewMockBaseCommand(),
		}

		root, err := wm.ValidateRepoPath(repoRoot)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if root != repoRoot {
			t.Errorf("expected root %q, got %q", repoRoot, root)
		}
	})

	t.Run("nonexistent path errors", func(t *testing.T) {
		mockGitBase := commands.NewMockBaseCommand()
		wm := &WorktreeManager{
			Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
			Base: commands.NewMockBaseCommand(),
		}

		_, err := wm.ValidateRepoPath(filepath.Join(t.TempDir(), "does-not-exist"))
		if err == nil {
			t.Fatal("expected error for nonexistent path")
		}
		testutil.VerifyNoRealCommands(t, mockGitBase)
	})

	t.Run("path exists but is not a directory errors", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "not-a-dir")
		if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		mockGitBase := commands.NewMockBaseCommand()
		wm := &WorktreeManager{
			Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
			Base: commands.NewMockBaseCommand(),
		}

		_, err := wm.ValidateRepoPath(filePath)
		if err == nil {
			t.Fatal("expected error for non-directory path")
		}
		testutil.VerifyNoRealCommands(t, mockGitBase)
	})

	t.Run("directory that is not a git repo errors", func(t *testing.T) {
		dir := t.TempDir()
		mockGitBase := commands.NewMockBaseCommand()
		mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)
		wm := &WorktreeManager{
			Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
			Base: commands.NewMockBaseCommand(),
		}

		_, err := wm.ValidateRepoPath(dir)
		if err == nil {
			t.Fatal("expected error for non-repo directory")
		}
	})
}
