package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() {
	testutil.InitLogger()
}

func TestNew(t *testing.T) {
	app := New()
	if app == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNameAndKind(t *testing.T) {
	g := &Git{}
	if g.Name() != constants.Git {
		t.Errorf("expected Name() %q, got %q", constants.Git, g.Name())
	}
	if g.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", g.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mockApp.Cmd.InstalledPkg != constants.Git {
		t.Fatalf("expected InstallPackage(%s), got %q", constants.Git, mockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldConfigGit := paths.Paths.Config.Git
	oldAppConfigsGit := paths.Paths.App.Configs.Git
	t.Cleanup(func() {
		paths.Paths.Config.Git = oldConfigGit
		paths.Paths.App.Configs.Git = oldAppConfigsGit
	})
	paths.Paths.Config.Git = filepath.Join(t.TempDir(), "git-config")
	paths.Paths.App.Configs.Git = filepath.Join(t.TempDir(), "git-app-configs")

	app := &Git{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledPkg != constants.Git {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mockApp.Cmd.MaybeInstalled != constants.Git {
		t.Fatalf(
			"expected MaybeInstallPackage(%s), got %q",
			constants.Git,
			mockApp.Cmd.MaybeInstalled,
		)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldConfigGit := paths.Paths.Config.Git
	oldAppConfigsGit := paths.Paths.App.Configs.Git
	t.Cleanup(func() {
		paths.Paths.Config.Git = oldConfigGit
		paths.Paths.App.Configs.Git = oldAppConfigsGit
	})
	paths.Paths.Config.Git = filepath.Join(t.TempDir(), "git-config")
	paths.Paths.App.Configs.Git = filepath.Join(t.TempDir(), "git-app-configs")

	app := &Git{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}
	if tc.MockApp.Cmd.UninstalledPkg != constants.Git {
		t.Errorf(
			"expected UninstallPackage(%s), got %q",
			constants.Git,
			tc.MockApp.Cmd.UninstalledPkg,
		)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	err := app.Update()
	if err == nil {
		t.Fatal("expected Update to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	src := filepath.Join(tc.AppDir, "git-src")
	dst := filepath.Join(tc.ConfigDir, "git")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	oldAppDir, oldLocalDir := paths.Paths.App.Configs.Git, paths.Paths.Config.Git
	t.Cleanup(func() {
		paths.Paths.App.Configs.Git, paths.Paths.Config.Git = oldAppDir, oldLocalDir
	})
	paths.Paths.App.Configs.Git, paths.Paths.Config.Git = src, dst

	originalContent := "[user]\n\tname = Test User"
	if err := os.WriteFile(
		filepath.Join(src, ".gitconfig"),
		[]byte(originalContent),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	app := &Git{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	check := filepath.Join(dst, ".gitconfig")
	if _, err := os.Stat(check); err != nil {
		t.Fatalf("expected copied file at %s: %v", check, err)
	}

	copiedContent, err := os.ReadFile(check)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if string(copiedContent) != originalContent {
		t.Fatalf("content mismatch: expected %q, got %q", originalContent, string(copiedContent))
	}

	modifiedContent := "[user]\n\tname = Modified User"
	if err := os.WriteFile(check, []byte(modifiedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("second ForceConfigure error: %v", err)
	}

	finalContent, err := os.ReadFile(check)
	if err != nil {
		t.Fatalf("failed to read file after second configure: %v", err)
	}
	if string(finalContent) == string(modifiedContent) {
		t.Fatalf(
			"ForceConfigure did not overwrite: expected %q, got %q",
			originalContent,
			string(finalContent),
		)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	testutil.IsolateXDGDirs(t)

	src := t.TempDir()
	dst := t.TempDir()

	oldAppDir, oldLocalDir := paths.Paths.App.Configs.Git, paths.Paths.Config.Git
	paths.Paths.App.Configs.Git, paths.Paths.Config.Git = src, dst
	t.Cleanup(func() {
		paths.Paths.App.Configs.Git, paths.Paths.Config.Git = oldAppDir, oldLocalDir
	})

	originalContent := "[user]\n\tname = Test User"
	if err := os.WriteFile(
		filepath.Join(src, ".gitconfig"),
		[]byte(originalContent),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	check := filepath.Join(dst, ".gitconfig")
	if _, err := os.Stat(check); err != nil {
		t.Fatalf("expected copied file at %s: %v", check, err)
	}

	modifiedContent := "[user]\n\tname = Modified User"
	if err := os.WriteFile(check, []byte(modifiedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("second SoftConfigure error: %v", err)
	}

	finalContent, err := os.ReadFile(check)
	if err != nil {
		t.Fatalf("failed to read file after second configure: %v", err)
	}
	if string(finalContent) == string(originalContent) {
		t.Fatalf(
			"SoftConfigure overwrote existing file: expected %q, got %q",
			modifiedContent,
			string(finalContent),
		)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful execution", func(t *testing.T) {
		mockApp.Base.SetExecCommandResult("git version 2.39.0", "", nil)

		if err := app.ExecuteCommand("--version"); err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 ExecCommand call, got %d", mockApp.Base.GetExecCommandCallCount())
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		if lastCall.Command != "git" {
			t.Fatalf("Expected command 'git', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "--version" {
			t.Fatalf("Expected args ['--version'], got %v", lastCall.Args)
		}
		if lastCall.IsSudo {
			t.Fatal("Expected IsSudo to be false")
		}
	})

	t.Run("command execution error", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"",
			"command not found",
			fmt.Errorf("command not found: git"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "git: command not found") {
			t.Fatalf("Expected error to contain 'git: command not found', got: %v", err)
		}
	})

	t.Run("clone command", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("Cloning into...", "", nil)

		if err := app.Clone("https://github.com/user/repo.git", "/tmp/repo"); err != nil {
			t.Fatalf("Clone failed: %v", err)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		expectedArgs := []string{"clone", "https://github.com/user/repo.git", "/tmp/repo"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	t.Run("Stream defaults to false", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)

		if err := app.ExecuteCommand("status"); err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}
		if mockApp.Base.GetLastExecCommandCall().Stream {
			t.Error("expected Stream=false by default")
		}
	})

	t.Run("Stream propagates to ExecCommand", func(t *testing.T) {
		streaming := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base, Stream: true}
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)

		if err := streaming.ExecuteCommand("status"); err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}
		if !mockApp.Base.GetLastExecCommandCall().Stream {
			t.Error("expected Git.Stream=true to set CommandParams.Stream")
		}

		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)
		if err := streaming.ExecuteCommandAt("/tmp/repo", "status"); err != nil {
			t.Fatalf("ExecuteCommandAt failed: %v", err)
		}
		if !mockApp.Base.GetLastExecCommandCall().Stream {
			t.Error("expected Git.Stream=true to set CommandParams.Stream for ExecuteCommandAt")
		}
	})
}

func TestBranchExists(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("branch exists returns true", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("  feature-a\n", "", nil)

		exists, err := app.BranchExists("feature-a")
		if err != nil {
			t.Fatalf("BranchExists failed: %v", err)
		}
		if !exists {
			t.Error("expected branch to exist")
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		expectedArgs := []string{"branch", "--list", "feature-a"}
		if len(last.Args) != len(expectedArgs) {
			t.Fatalf("expected args %v, got %v", expectedArgs, last.Args)
		}
	})

	t.Run("branch does not exist returns false", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)

		exists, err := app.BranchExists("no-such-branch")
		if err != nil {
			t.Fatalf("BranchExists failed: %v", err)
		}
		if exists {
			t.Error("expected branch to not exist")
		}
	})

	t.Run("exec error propagates", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "fatal: error", fmt.Errorf("git error"))

		_, err := app.BranchExists("any")
		if err == nil {
			t.Error("expected error to propagate")
		}
	})
}

func TestListWorktreesAt(t *testing.T) {
	const porcelain = "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n" +
		"worktree /repo/.worktrees/feature-a\nHEAD def456\nbranch refs/heads/feature-a\n\n"

	t.Run("lists worktrees from given directory", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(porcelain, "", nil)
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

		worktrees, err := app.ListWorktreesAt("/repo")
		if err != nil {
			t.Fatalf("ListWorktreesAt failed: %v", err)
		}
		if len(worktrees) != 2 {
			t.Fatalf("expected 2 worktrees, got %d", len(worktrees))
		}
		if worktrees[1].Branch != "feature-a" {
			t.Errorf("expected branch 'feature-a', got %q", worktrees[1].Branch)
		}

		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		// Must use -C flag with the given directory
		if len(last.Args) < 2 || last.Args[0] != "-C" || last.Args[1] != "/repo" {
			t.Errorf("expected -C /repo as first args, got %v", last.Args)
		}
	})

	t.Run("exec error returns error", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "fatal", fmt.Errorf("not a git repo"))
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

		_, err := app.ListWorktreesAt("/not-a-repo")
		if err == nil {
			t.Error("expected error for non-git directory")
		}
	})
}

func TestPruneWorktreesAt(t *testing.T) {
	t.Run("runs prune in given directory", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.PruneWorktreesAt("/repo"); err != nil {
			t.Fatalf("PruneWorktreesAt failed: %v", err)
		}

		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		if len(last.Args) < 2 || last.Args[0] != "-C" || last.Args[1] != "/repo" {
			t.Errorf("expected -C /repo as first args, got %v", last.Args)
		}
		// Must include worktree prune
		found := false
		for _, arg := range last.Args {
			if arg == "prune" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'prune' in args, got %v", last.Args)
		}
	})

	t.Run("returns error on failure", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "error", fmt.Errorf("not a git repo"))
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.PruneWorktreesAt("/bad"); err == nil {
			t.Error("expected error")
		}
	})
}

func TestRemoteBranchExists(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("remote branch exists", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("  origin/feature-A\n", "", nil)

		exists, err := app.RemoteBranchExists("feature-A")
		if err != nil {
			t.Fatalf("RemoteBranchExists failed: %v", err)
		}
		if !exists {
			t.Error("Expected remote branch to exist")
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall.Command != "git" {
			t.Fatalf("Expected command 'git', got %q", lastCall.Command)
		}
		expectedArgs := []string{"branch", "-r", "--list", "origin/feature-A"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	t.Run("remote branch does not exist", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)

		exists, err := app.RemoteBranchExists("feature-B")
		if err != nil {
			t.Fatalf("RemoteBranchExists failed: %v", err)
		}
		if exists {
			t.Error("Expected remote branch to not exist")
		}
	})
}

func TestCreateWorktree(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("new branch creation - neither local nor remote exists", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)

		if err := app.CreateWorktree("/path/to/worktree", "feature-branch"); err != nil {
			t.Fatalf("CreateWorktree failed: %v", err)
		}

		callCount := mockApp.Base.GetExecCommandCallCount()
		if callCount < 3 {
			t.Fatalf("Expected at least 3 command calls, got %d", callCount)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall.Command != "git" {
			t.Fatalf("Expected command 'git', got %q", lastCall.Command)
		}

		hasWorktreeAdd := false
		hasNewBranchFlag := false
		for i, arg := range lastCall.Args {
			if arg == "worktree" && i+1 < len(lastCall.Args) && lastCall.Args[i+1] == "add" {
				hasWorktreeAdd = true
			}
			if arg == "-b" {
				hasNewBranchFlag = true
			}
		}
		if !hasWorktreeAdd {
			t.Fatal("Expected 'git worktree add' command")
		}
		if !hasNewBranchFlag {
			t.Fatal("Expected -b flag for creating new branch")
		}
	})

	t.Run("new branch bases off origin default branch when available", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		// Call sequence inside CreateWorktree:
		// 1. fetch origin
		// 2. BranchExists(branch)        -> none (empty)
		// 3. RemoteBranchExists(branch)  -> none (empty)
		// 4. DefaultBranch symbolic-ref  -> "origin/main"
		// 5. RemoteBranchExists("main")  -> exists
		// 6. worktree add ... -b ... origin/main
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("  origin/main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		if err := app.CreateWorktree("/path/to/worktree", "feature-branch"); err != nil {
			t.Fatalf("CreateWorktree failed: %v", err)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("Expected a worktree add call")
		}
		if lastCall.Args[len(lastCall.Args)-1] != "origin/main" {
			t.Fatalf("Expected new branch to base off 'origin/main', got args: %v", lastCall.Args)
		}
		hasNewBranchFlag := false
		for _, arg := range lastCall.Args {
			if arg == "-b" {
				hasNewBranchFlag = true
			}
		}
		if !hasNewBranchFlag {
			t.Fatalf("Expected -b flag for creating new branch, got args: %v", lastCall.Args)
		}
	})

	t.Run("new branch falls back to HEAD when origin default missing", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		// origin/main does not exist (offline / no remote): all checks empty.
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),
		)

		if err := app.CreateWorktree("/path/to/worktree", "feature-branch"); err != nil {
			t.Fatalf("CreateWorktree failed: %v", err)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("Expected a worktree add call")
		}
		// Should end at "-b feature-branch" with no base ref appended.
		last := lastCall.Args[len(lastCall.Args)-1]
		if last != "feature-branch" {
			t.Fatalf("Expected fallback to HEAD (no base ref), got args: %v", lastCall.Args)
		}
	})

	t.Run("existing local branch is fast-forwarded to remote", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		// 1. fetch origin
		// 2. BranchExists(branch)             -> exists
		// 3. ListWorktreesAt("")               -> no holder for "feature-branch"
		//    (DefaultBranchIn is never called: freeBranchIfHeldElsewhere short-circuits
		//    as soon as no holder is found, before computing the default branch)
		// 4. worktree add path branch
		// 5. RemoteBranchExists(branch)        -> exists
		// 6. merge --ff-only origin/branch
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("  feature-branch\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("  origin/feature-branch\n", "", nil),
			commands.ExecCommandResult("Updating abc..def\n", "", nil),
		)

		if err := app.CreateWorktree("/path/to/worktree", "feature-branch"); err != nil {
			t.Fatalf("CreateWorktree failed: %v", err)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("Expected a merge call")
		}
		joined := strings.Join(lastCall.Args, " ")
		if !strings.Contains(joined, "merge --ff-only origin/feature-branch") {
			t.Fatalf(
				"Expected ff-only merge against origin/feature-branch, got args: %v",
				lastCall.Args,
			)
		}
	})

	t.Run("diverged local branch warns but still succeeds", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil), // fetch
			commands.ExecCommandResult(
				"  feature-branch\n",
				"",
				nil,
			), // BranchExists -> exists
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // ListWorktreesAt -> no holder (DefaultBranchIn never called)
			commands.ExecCommandResult("", "", nil), // worktree add
			commands.ExecCommandResult(
				"  origin/feature-branch\n",
				"",
				nil,
			), // RemoteBranchExists -> exists
			commands.ExecCommandResult(
				"",
				"fatal: Not possible to fast-forward",
				fmt.Errorf("diverged"),
			),
		)

		// Diverged ff-merge must not fail the operation: the worktree was created.
		if err := app.CreateWorktree("/path/to/worktree", "feature-branch"); err != nil {
			t.Fatalf("Expected success despite divergence, got: %v", err)
		}
	})

	t.Run("existing local branch with no remote skips sync", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),               // fetch
			commands.ExecCommandResult("  local-only\n", "", nil), // BranchExists -> exists
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // ListWorktreesAt -> no holder (DefaultBranchIn never called)
			commands.ExecCommandResult("", "", nil), // worktree add
			commands.ExecCommandResult("", "", nil), // RemoteBranchExists -> none
		)

		if err := app.CreateWorktree("/path/to/worktree", "local-only"); err != nil {
			t.Fatalf("CreateWorktree failed: %v", err)
		}

		// No merge should have been attempted (last call is RemoteBranchExists).
		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("Expected at least one call")
		}
		for _, arg := range lastCall.Args {
			if arg == "merge" {
				t.Fatalf(
					"Expected no merge for a branch with no remote, got args: %v",
					lastCall.Args,
				)
			}
		}
	})

	t.Run("creation error on worktree add", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"",
			"fatal: worktree exists",
			fmt.Errorf("worktree exists"),
		)

		err := app.CreateWorktree("/path/to/worktree", "existing-branch")
		if err == nil {
			t.Fatal("Expected error but got none")
		}
		if !strings.Contains(err.Error(), "worktree exists") {
			t.Errorf("Expected error to contain 'worktree exists', got: %v", err)
		}
	})
}

func TestCreateWorktreeIn_AdoptsBranchHeldElsewhere(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run(
		"branch checked out in main clone with clean tree is freed then adopted",
		func(t *testing.T) {
			mockApp.Base.ResetExecCommand()
			const porcelain = "worktree /repo\nHEAD abc123\nbranch refs/heads/feature-branch\n\n" +
				"worktree /repo/.worktrees/other\nHEAD def456\nbranch refs/heads/other-branch\n\n"
			mockApp.Base.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil), // fetch origin
				commands.ExecCommandResult(
					"  feature-branch\n",
					"",
					nil,
				), // BranchExistsIn -> exists
				commands.ExecCommandResult(
					porcelain,
					"",
					nil,
				), // ListWorktreesAt -> holder is /repo
				commands.ExecCommandResult(
					"origin/main\n",
					"",
					nil,
				), // DefaultBranchIn symbolic-ref -> main
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // IsWorktreeDirty(/repo) -> clean
				commands.ExecCommandResult("", "", nil), // checkout main at /repo
				commands.ExecCommandResult("", "", nil), // worktree add
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // RemoteBranchExistsIn (sync) -> none
			)

			err := app.CreateWorktreeIn(
				"/repo",
				"/repo/.worktrees/feature-branch",
				"feature-branch",
			)
			if err != nil {
				t.Fatalf("CreateWorktreeIn failed: %v", err)
			}

			calls := mockApp.Base.ExecCommandCalls
			if len(calls) != 8 {
				t.Fatalf("expected 8 exec calls, got %d: %+v", len(calls), calls)
			}

			checkoutCall := calls[5]
			if checkoutCall.Args[0] != "-C" || checkoutCall.Args[1] != "/repo" ||
				checkoutCall.Args[2] != "checkout" || checkoutCall.Args[3] != "main" {
				t.Fatalf("expected checkout main at /repo, got args: %v", checkoutCall.Args)
			}

			worktreeAddCall := calls[6]
			joined := strings.Join(worktreeAddCall.Args, " ")
			if !strings.Contains(
				joined,
				"worktree add /repo/.worktrees/feature-branch feature-branch",
			) {
				t.Fatalf(
					"expected worktree add for the new path, got args: %v",
					worktreeAddCall.Args,
				)
			}
		},
	)

	t.Run("dirty holder blocks the switch and returns an error", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		const porcelain = "worktree /repo\nHEAD abc123\nbranch refs/heads/feature-branch\n\n"
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),                   // fetch origin
			commands.ExecCommandResult("  feature-branch\n", "", nil), // BranchExistsIn -> exists
			commands.ExecCommandResult(
				porcelain,
				"",
				nil,
			), // ListWorktreesAt -> holder is /repo
			commands.ExecCommandResult(
				"origin/main\n",
				"",
				nil,
			), // DefaultBranchIn symbolic-ref -> main
			commands.ExecCommandResult(
				" M dirty-file.go\n",
				"",
				nil,
			), // IsWorktreeDirty(/repo) -> dirty
		)

		err := app.CreateWorktreeIn("/repo", "/repo/.worktrees/feature-branch", "feature-branch")
		if err == nil {
			t.Fatal("expected an error for a dirty holder, got none")
		}
		if !strings.Contains(err.Error(), "uncommitted changes") {
			t.Errorf("expected error to mention uncommitted changes, got: %v", err)
		}

		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 5 {
			t.Fatalf(
				"expected exactly 5 exec calls (no checkout, no worktree add), got %d: %+v",
				len(calls),
				calls,
			)
		}
		for _, call := range calls {
			joined := strings.Join(call.Args, " ")
			if strings.Contains(joined, "checkout") {
				t.Errorf("expected no checkout call when holder is dirty, got: %v", call.Args)
			}
			if strings.Contains(joined, "worktree add") {
				t.Errorf("expected no worktree add call when holder is dirty, got: %v", call.Args)
			}
		}
	})

	t.Run("branch checked out nowhere behaves exactly as before", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		const porcelain = "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n"
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),                   // fetch origin
			commands.ExecCommandResult("  feature-branch\n", "", nil), // BranchExistsIn -> exists
			commands.ExecCommandResult(
				porcelain,
				"",
				nil,
			), // ListWorktreesAt -> no holder (DefaultBranchIn never called)
			commands.ExecCommandResult("", "", nil), // worktree add
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // RemoteBranchExistsIn (sync) -> none
		)

		err := app.CreateWorktreeIn("/repo", "/repo/.worktrees/feature-branch", "feature-branch")
		if err != nil {
			t.Fatalf("CreateWorktreeIn failed: %v", err)
		}

		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 5 {
			t.Fatalf("expected 5 exec calls (no checkout switch), got %d: %+v", len(calls), calls)
		}
		for _, call := range calls {
			joined := strings.Join(call.Args, " ")
			if strings.Contains(joined, "checkout") {
				t.Errorf(
					"expected no checkout call when branch is held nowhere, got: %v",
					call.Args,
				)
			}
		}
	})

	t.Run("branch absent locally and remotely still creates a new branch", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil), // fetch origin
			commands.ExecCommandResult("", "", nil), // BranchExistsIn -> none
			commands.ExecCommandResult("", "", nil), // RemoteBranchExistsIn -> none
			commands.ExecCommandResult("", "", nil), // DefaultBranchIn symbolic-ref -> unset
			commands.ExecCommandResult("", "", nil), // probe RemoteBranchExistsIn(main) -> none
			commands.ExecCommandResult("", "", nil), // probe RemoteBranchExistsIn(master) -> none
			commands.ExecCommandResult("", "", nil), // probe RemoteBranchExistsIn(develop) -> none
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // baseExists = RemoteBranchExistsIn(repoDir, "main") -> none
			commands.ExecCommandResult("", "", nil), // worktree add -b
		)

		err := app.CreateWorktreeIn("/repo", "/repo/.worktrees/new-branch", "new-branch")
		if err != nil {
			t.Fatalf("CreateWorktreeIn failed: %v", err)
		}

		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 9 {
			t.Fatalf("expected 9 exec calls, got %d: %+v", len(calls), calls)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("expected a worktree add call")
		}
		joined := strings.Join(lastCall.Args, " ")
		if !strings.Contains(joined, "worktree add") || !strings.Contains(joined, "-b") {
			t.Fatalf("expected a new-branch worktree add, got args: %v", lastCall.Args)
		}
	})

	t.Run(
		"branch held elsewhere but equal to the default branch is left to git",
		func(t *testing.T) {
			mockApp.Base.ResetExecCommand()
			const porcelain = "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n"
			mockApp.Base.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil),         // fetch origin
				commands.ExecCommandResult("  main\n", "", nil), // BranchExistsIn -> exists
				commands.ExecCommandResult(
					porcelain,
					"",
					nil,
				), // ListWorktreesAt -> holder is /repo
				commands.ExecCommandResult(
					"origin/main\n",
					"",
					nil,
				), // DefaultBranchIn symbolic-ref -> main
				commands.ExecCommandResult(
					"",
					"fatal: 'main' is already used by worktree at '/repo'",
					fmt.Errorf("already used"),
				), // worktree add -> git's own refusal surfaces unmodified
			)

			err := app.CreateWorktreeIn("/repo", "/repo/.worktrees/main", "main")
			if err == nil {
				t.Fatal("expected the existing git error to surface, got none")
			}
			if !strings.Contains(err.Error(), "already used") {
				t.Errorf("expected the original git error to surface, got: %v", err)
			}

			calls := mockApp.Base.ExecCommandCalls
			if len(calls) != 5 {
				t.Fatalf(
					"expected 5 exec calls (short-circuit, no dirty check), got %d: %+v",
					len(calls),
					calls,
				)
			}
			for _, call := range calls {
				joined := strings.Join(call.Args, " ")
				if strings.Contains(joined, "checkout") {
					t.Errorf(
						"expected no checkout call when branch equals the default branch, got: %v",
						call.Args,
					)
				}
			}
		},
	)

	t.Run("ListWorktreesAt error is propagated and blocks worktree add", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),                   // fetch origin
			commands.ExecCommandResult("  feature-branch\n", "", nil), // BranchExistsIn -> exists
			commands.ExecCommandResult(
				"",
				"fatal: not a git repository",
				fmt.Errorf("boom"),
			), // ListWorktreesAt -> error
		)

		err := app.CreateWorktreeIn("/repo", "/repo/.worktrees/feature-branch", "feature-branch")
		if err == nil {
			t.Fatal("expected an error when ListWorktreesAt fails, got none")
		}
		if !strings.Contains(err.Error(), "failed to list worktrees") {
			t.Errorf("expected a wrapped list-worktrees error, got: %v", err)
		}

		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 3 {
			t.Fatalf(
				"expected exactly 3 exec calls (no worktree add), got %d: %+v",
				len(calls),
				calls,
			)
		}
		for _, call := range calls {
			joined := strings.Join(call.Args, " ")
			if strings.Contains(joined, "worktree add") {
				t.Errorf(
					"expected no worktree add call when ListWorktreesAt fails, got: %v",
					call.Args,
				)
			}
		}
	})
}

func TestDefaultBranch(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("resolves origin/HEAD", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("origin/develop\n", "", nil),
		)
		if got := app.DefaultBranch(); got != "develop" {
			t.Fatalf("Expected 'develop', got %q", got)
		}
	})

	t.Run("falls back to main when origin/HEAD unset and origin/main exists", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: ref not found", fmt.Errorf("no origin/HEAD")),
			commands.ExecCommandResult("origin/main", "", nil), // RemoteBranchExists("main")
		)
		if got := app.DefaultBranch(); got != "main" {
			t.Fatalf("Expected 'main', got %q", got)
		}
	})

	t.Run("probes master when origin/HEAD unset and no origin/main", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: ref not found", fmt.Errorf("no origin/HEAD")),
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // RemoteBranchExists("main") -> none
			commands.ExecCommandResult(
				"origin/master",
				"",
				nil,
			), // RemoteBranchExists("master") -> exists
		)
		if got := app.DefaultBranch(); got != "master" {
			t.Fatalf("Expected 'master', got %q", got)
		}
	})

	t.Run("probes develop when origin/HEAD unset and no main/master", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: ref not found", fmt.Errorf("no origin/HEAD")),
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // RemoteBranchExists("main") -> none
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // RemoteBranchExists("master") -> none
			commands.ExecCommandResult(
				"origin/develop",
				"",
				nil,
			), // RemoteBranchExists("develop") -> exists
		)
		if got := app.DefaultBranch(); got != "develop" {
			t.Fatalf("Expected 'develop', got %q", got)
		}
	})

	t.Run("falls back to main when origin/HEAD unset and nothing else exists", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: ref not found", fmt.Errorf("no origin/HEAD")),
			commands.ExecCommandResult("", "", nil), // RemoteBranchExists("main") -> none
			commands.ExecCommandResult("", "", nil), // RemoteBranchExists("master") -> none
			commands.ExecCommandResult("", "", nil), // RemoteBranchExists("develop") -> none
		)
		if got := app.DefaultBranch(); got != "main" {
			t.Fatalf("Expected fallback 'main', got %q", got)
		}
	})
}

func TestListWorktrees(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful list", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		porcelainOutput := `worktree /Users/test/repo
HEAD abc123def456
branch refs/heads/main

worktree /Users/test/repo/.worktrees/feature
HEAD def456abc789
branch refs/heads/feature
`
		mockApp.Base.SetExecCommandResult(porcelainOutput, "", nil)

		worktrees, err := app.ListWorktrees()
		if err != nil {
			t.Fatalf("ListWorktrees failed: %v", err)
		}
		if len(worktrees) != 2 {
			t.Fatalf("Expected 2 worktrees, got %d", len(worktrees))
		}

		if worktrees[0].Path != "/Users/test/repo" {
			t.Errorf("Expected path '/Users/test/repo', got %q", worktrees[0].Path)
		}
		if worktrees[0].Branch != "main" {
			t.Errorf("Expected branch 'main', got %q", worktrees[0].Branch)
		}
		if worktrees[0].Commit != "abc123def456" {
			t.Errorf("Expected commit 'abc123def456', got %q", worktrees[0].Commit)
		}

		if worktrees[1].Path != "/Users/test/repo/.worktrees/feature" {
			t.Errorf(
				"Expected path '/Users/test/repo/.worktrees/feature', got %q",
				worktrees[1].Path,
			)
		}
		if worktrees[1].Branch != "feature" {
			t.Errorf("Expected branch 'feature', got %q", worktrees[1].Branch)
		}
	})

	t.Run("list error", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"",
			"fatal: not a git repository",
			fmt.Errorf("not a repo"),
		)

		_, err := app.ListWorktrees()
		if err == nil {
			t.Fatal("Expected error but got none")
		}
		if !strings.Contains(err.Error(), "failed to list worktrees") {
			t.Fatalf("Expected error message to contain 'failed to list worktrees', got: %v", err)
		}
	})
}

func TestRemoveWorktree(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful removal without branch deletion", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		// First call is getMainWorktree (worktree list), subsequent calls succeed with same output
		mockApp.Base.SetExecCommandResult(
			"worktree /main/repo\nHEAD abc123\nbranch refs/heads/main\n",
			"",
			nil,
		)

		if err := app.RemoveWorktree("/path/to/worktree", false, ""); err != nil {
			t.Fatalf("RemoveWorktree failed: %v", err)
		}
		if mockApp.Base.GetExecCommandCallCount() != 2 {
			t.Fatalf("Expected 2 calls, got %d", mockApp.Base.GetExecCommandCallCount())
		}

		// First call: getMainWorktree
		firstCall := mockApp.Base.ExecCommandCalls[0]
		expectedFirst := []string{"-C", "/path/to/worktree", "worktree", "list", "--porcelain"}
		if len(firstCall.Args) != len(expectedFirst) {
			t.Fatalf(
				"Expected %d args for first call, got %d",
				len(expectedFirst),
				len(firstCall.Args),
			)
		}

		// Second call: worktree remove from main repo
		secondCall := mockApp.Base.ExecCommandCalls[1]
		expectedSecond := []string{"-C", "/main/repo", "worktree", "remove", "/path/to/worktree"}
		if len(secondCall.Args) != len(expectedSecond) {
			t.Fatalf(
				"Expected %d args for second call, got %d: %v",
				len(expectedSecond),
				len(secondCall.Args),
				secondCall.Args,
			)
		}
		for i, arg := range expectedSecond {
			if secondCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, secondCall.Args[i])
			}
		}
	})

	t.Run("successful removal with branch deletion", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"worktree /main/repo\nHEAD abc123\nbranch refs/heads/main\n",
			"",
			nil,
		)

		if err := app.RemoveWorktree("/path/to/worktree", true, "feature-branch"); err != nil {
			t.Fatalf("RemoveWorktree failed: %v", err)
		}
		if mockApp.Base.GetExecCommandCallCount() != 3 {
			t.Fatalf("Expected 3 calls, got %d", mockApp.Base.GetExecCommandCallCount())
		}
	})

	t.Run("removal error", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "fatal: worktree not found", fmt.Errorf("not found"))

		if err := app.RemoveWorktree("/nonexistent/path", false, ""); err == nil {
			t.Fatal("Expected error but got none")
		}
	})
}

func TestMoveWorktree(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful move runs from the main worktree", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"worktree /main/repo\nHEAD abc123\nbranch refs/heads/main\n",
			"",
			nil,
		)

		if err := app.MoveWorktree("/old/path", "/new/path"); err != nil {
			t.Fatalf("MoveWorktree failed: %v", err)
		}
		if mockApp.Base.GetExecCommandCallCount() != 2 {
			t.Fatalf("Expected 2 calls, got %d", mockApp.Base.GetExecCommandCallCount())
		}

		// First call: getMainWorktree, resolved from the source path
		firstCall := mockApp.Base.ExecCommandCalls[0]
		expectedFirst := []string{"-C", "/old/path", "worktree", "list", "--porcelain"}
		if len(firstCall.Args) != len(expectedFirst) {
			t.Fatalf(
				"Expected %d args for first call, got %d",
				len(expectedFirst),
				len(firstCall.Args),
			)
		}

		// Second call: worktree move, executed from the main worktree, not /old/path
		secondCall := mockApp.Base.ExecCommandCalls[1]
		expectedSecond := []string{"-C", "/main/repo", "worktree", "move", "/old/path", "/new/path"}
		if len(secondCall.Args) != len(expectedSecond) {
			t.Fatalf(
				"Expected %d args for second call, got %d: %v",
				len(expectedSecond),
				len(secondCall.Args),
				secondCall.Args,
			)
		}
		for i, arg := range expectedSecond {
			if secondCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, secondCall.Args[i])
			}
		}
	})

	t.Run("GetMainWorktree failure aborts before any move attempt", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"",
			"fatal: not a git repository",
			fmt.Errorf("not a repo"),
		)

		err := app.MoveWorktree("/old/path", "/new/path")
		if err == nil {
			t.Fatal("Expected error but got none")
		}
		if !strings.Contains(err.Error(), "cannot resolve main worktree") {
			t.Fatalf("Expected error to mention resolving main worktree, got: %v", err)
		}
		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Fatalf(
				"Expected only the GetMainWorktree call, got %d calls",
				mockApp.Base.GetExecCommandCallCount(),
			)
		}
	})

	t.Run("git's move refusal surfaces to the caller", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree /main/repo\nHEAD abc123\nbranch refs/heads/main\n",
				"",
				nil,
			),
			commands.ExecCommandResult(
				"",
				"fatal: '/old/path' is locked",
				fmt.Errorf("exit status 128"),
			),
		)

		err := app.MoveWorktree("/old/path", "/new/path")
		if err == nil {
			t.Fatal("Expected error but got none")
		}
		if !strings.Contains(err.Error(), "locked") {
			t.Fatalf("Expected git's refusal message to surface, got: %v", err)
		}
	})
}

// exitError builds a real *exec.ExitError with the given exit code by
// actually running a trivial subprocess (sh -c "exit N") - the standard way
// to construct one in Go, since exec.ExitError's fields are unexported and
// there is no public constructor. This is not "executing a real command"
// under this repo's test-safety rule (which forbids exercising real
// git/tmux/etc business logic in tests): it never touches git, only
// synthesizes a realistic error value to inject into MockBaseCommand, so
// IsPathIgnored's exit-code branch is exercised against the same error shape
// ExecCommand really returns, not a hand-rolled stand-in.
func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	if err == nil {
		t.Fatalf("expected a non-nil error for exit code %d", code)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	return exitErr
}

func TestIsPathIgnored(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("exit 0 means ignored", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)

		ignored, err := app.IsPathIgnored("/repo", ".claude/worktrees")
		if err != nil {
			t.Fatalf("IsPathIgnored failed: %v", err)
		}
		if !ignored {
			t.Error("expected ignored=true on exit 0")
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		expectedArgs := []string{"-C", "/repo", "check-ignore", "-q", ".claude/worktrees"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf(
				"Expected %d args, got %d: %v",
				len(expectedArgs),
				len(lastCall.Args),
				lastCall.Args,
			)
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	// This is the detail most likely to be gotten wrong: `git check-ignore`
	// exits 1 to mean "not ignored" - a normal, expected result, not a
	// failure. IsPathIgnored must return (false, nil), never propagate exit
	// 1 as an error.
	t.Run("exit 1 means not ignored, and is not an error", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", exitError(t, 1))

		ignored, err := app.IsPathIgnored("/repo", ".claude/worktrees")
		if err != nil {
			t.Fatalf("expected no error for exit code 1, got: %v", err)
		}
		if ignored {
			t.Error("expected ignored=false on exit 1")
		}
	})

	t.Run("any other non-zero exit is a real error", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "fatal: not a git repository", exitError(t, 128))

		_, err := app.IsPathIgnored("/repo", ".claude/worktrees")
		if err == nil {
			t.Fatal("expected an error for exit code 128")
		}
	})
}

func TestGetRepoRoot(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful get repo root", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("/Users/test/my-repo\n", "", nil)

		root, err := app.GetRepoRoot()
		if err != nil {
			t.Fatalf("GetRepoRoot failed: %v", err)
		}
		if root != "/Users/test/my-repo" {
			t.Errorf("Expected '/Users/test/my-repo', got %q", root)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		expectedArgs := []string{"rev-parse", "--show-toplevel"}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	t.Run("not a git repo", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"",
			"fatal: not a git repository",
			fmt.Errorf("not a repo"),
		)

		_, err := app.GetRepoRoot()
		if err == nil {
			t.Fatal("Expected error but got none")
		}
		if !strings.Contains(err.Error(), "failed to get repo root") {
			t.Fatalf("Expected error message to contain 'failed to get repo root', got: %v", err)
		}
	})
}

func TestParseWorktreeOutput(t *testing.T) {
	t.Run("multiple worktrees", func(t *testing.T) {
		output := `worktree /Users/test/repo
HEAD abc123
branch refs/heads/main

worktree /Users/test/repo/.worktrees/feature
HEAD def456
branch refs/heads/feature
`
		worktrees := parseWorktreeOutput(output)
		if len(worktrees) != 2 {
			t.Fatalf("Expected 2 worktrees, got %d", len(worktrees))
		}
		if worktrees[0].Path != "/Users/test/repo" {
			t.Errorf("Expected path '/Users/test/repo', got %q", worktrees[0].Path)
		}
		if worktrees[0].Commit != "abc123" {
			t.Errorf("Expected commit 'abc123', got %q", worktrees[0].Commit)
		}
		if worktrees[0].Branch != "main" {
			t.Errorf("Expected branch 'main', got %q", worktrees[0].Branch)
		}
	})

	t.Run("empty output", func(t *testing.T) {
		worktrees := parseWorktreeOutput("")
		if len(worktrees) != 0 {
			t.Errorf("Expected 0 worktrees for empty output, got %d", len(worktrees))
		}
	})

	t.Run("single worktree without trailing newline", func(t *testing.T) {
		output := `worktree /Users/test/repo
HEAD abc123
branch refs/heads/main`
		worktrees := parseWorktreeOutput(output)
		if len(worktrees) != 1 {
			t.Fatalf("Expected 1 worktree, got %d", len(worktrees))
		}
		if worktrees[0].Path != "/Users/test/repo" {
			t.Errorf("Expected path '/Users/test/repo', got %q", worktrees[0].Path)
		}
	})

	t.Run("detached HEAD", func(t *testing.T) {
		output := `worktree /Users/test/repo
HEAD abc123
detached
`
		worktrees := parseWorktreeOutput(output)
		if len(worktrees) != 1 {
			t.Fatalf("Expected 1 worktree, got %d", len(worktrees))
		}
		if worktrees[0].Branch != "" {
			t.Errorf("Expected empty branch for detached HEAD, got %q", worktrees[0].Branch)
		}
	})
}

func TestIsWorktreeDirty(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("clean worktree", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)

		dirty, err := app.IsWorktreeDirty("/path/to/worktree")
		if err != nil {
			t.Fatalf("IsWorktreeDirty failed: %v", err)
		}
		if dirty {
			t.Error("Expected clean worktree")
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		expectedArgs := []string{"-C", "/path/to/worktree", "status", "--porcelain"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	t.Run("dirty worktree", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("M file.go\n", "", nil)

		dirty, err := app.IsWorktreeDirty("/path/to/worktree")
		if err != nil {
			t.Fatalf("IsWorktreeDirty failed: %v", err)
		}
		if !dirty {
			t.Error("Expected dirty worktree")
		}
	})
}

func TestCheckHookCompatibility(t *testing.T) {
	t.Run("no hooks directory returns no warnings", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult("", "exit status 1", fmt.Errorf("exit status 1"))

		warnings := app.CheckHookCompatibility(t.TempDir())
		if len(warnings) != 0 {
			t.Errorf("Expected no warnings, got %v", warnings)
		}
	})

	t.Run("hook with [ -d .git pattern triggers warning", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult("", "exit status 1", fmt.Errorf("exit status 1"))

		tmpDir := t.TempDir()
		hooksDir := filepath.Join(tmpDir, ".git", "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		hookContent := "#!/bin/bash\n[ -d .git ] || { echo 'no .git directory found'; exit 1; }\n"
		if err := os.WriteFile(
			filepath.Join(hooksDir, "pre-commit"),
			[]byte(hookContent),
			0o755,
		); err != nil {
			t.Fatal(err)
		}

		warnings := app.CheckHookCompatibility(tmpDir)
		if len(warnings) != 1 {
			t.Fatalf("Expected 1 warning, got %d: %v", len(warnings), warnings)
		}
		if !strings.Contains(warnings[0], "pre-commit") {
			t.Errorf("Expected warning to mention pre-commit, got %q", warnings[0])
		}
		if !strings.Contains(warnings[0], `[ -d .git`) {
			t.Errorf("Expected warning to mention the pattern, got %q", warnings[0])
		}
	})

	t.Run("hook with test -d .git pattern triggers warning", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult("", "exit status 1", fmt.Errorf("exit status 1"))

		tmpDir := t.TempDir()
		hooksDir := filepath.Join(tmpDir, ".git", "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		hookContent := "#!/bin/bash\ntest -d .git || exit 1\n"
		if err := os.WriteFile(
			filepath.Join(hooksDir, "commit-msg"),
			[]byte(hookContent),
			0o755,
		); err != nil {
			t.Fatal(err)
		}

		warnings := app.CheckHookCompatibility(tmpDir)
		if len(warnings) != 1 {
			t.Fatalf("Expected 1 warning, got %d: %v", len(warnings), warnings)
		}
		if !strings.Contains(warnings[0], "commit-msg") {
			t.Errorf("Expected warning to mention commit-msg, got %q", warnings[0])
		}
	})

	t.Run("hook using git rev-parse is compatible", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult("", "exit status 1", fmt.Errorf("exit status 1"))

		tmpDir := t.TempDir()
		hooksDir := filepath.Join(tmpDir, ".git", "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		hookContent := "#!/bin/bash\ngit_dir=$(git rev-parse --git-dir)\necho \"git dir: $git_dir\"\n"
		if err := os.WriteFile(
			filepath.Join(hooksDir, "pre-commit"),
			[]byte(hookContent),
			0o755,
		); err != nil {
			t.Fatal(err)
		}

		warnings := app.CheckHookCompatibility(tmpDir)
		if len(warnings) != 0 {
			t.Errorf("Expected no warnings for compatible hook, got %v", warnings)
		}
	})

	t.Run("respects custom core.hooksPath", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

		tmpDir := t.TempDir()
		huskyDir := filepath.Join(tmpDir, ".husky")
		if err := os.MkdirAll(huskyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		hookContent := "#!/bin/sh\n[ -d .git ] || exit 1\n"
		if err := os.WriteFile(
			filepath.Join(huskyDir, "pre-commit"),
			[]byte(hookContent),
			0o755,
		); err != nil {
			t.Fatal(err)
		}
		mockApp.Base.SetExecCommandResult(".husky\n", "", nil)

		warnings := app.CheckHookCompatibility(tmpDir)
		if len(warnings) != 1 {
			t.Fatalf("Expected 1 warning for husky hook, got %d: %v", len(warnings), warnings)
		}
		if !strings.Contains(warnings[0], "pre-commit") {
			t.Errorf("Expected pre-commit warning, got %q", warnings[0])
		}
	})

	t.Run("multiple hooks each trigger one warning", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult("", "exit status 1", fmt.Errorf("exit status 1"))

		tmpDir := t.TempDir()
		hooksDir := filepath.Join(tmpDir, ".git", "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		badHook := "#!/bin/bash\n[ -d .git ] || exit 1\n"
		for _, name := range []string{"pre-commit", "commit-msg"} {
			if err := os.WriteFile(
				filepath.Join(hooksDir, name),
				[]byte(badHook),
				0o755,
			); err != nil {
				t.Fatal(err)
			}
		}

		warnings := app.CheckHookCompatibility(tmpDir)
		if len(warnings) != 2 {
			t.Errorf("Expected 2 warnings, got %d: %v", len(warnings), warnings)
		}
	})

	// Affiance's own worktree failure is fixed at the source by
	// NormalizeWorktreeGitfile, so it must NOT be reported here - warning about
	// a problem devgeta already prevents is a nag the user cannot act on. See
	// ADR-0013.
	t.Run("affiance hooks alone produce no warning", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult("", "exit status 1", fmt.Errorf("exit status 1"))

		tmpDir := t.TempDir()
		hooksDir := filepath.Join(tmpDir, ".git", "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}

		// The real affiance stub: every hook is symlinked to it, and none of
		// them test for a .git DIRECTORY, so there is nothing to warn about.
		affianceContent := "#!/bin/bash\nhook=`basename \"$0\"`\nnode $DIR/affiance-hook.js \"$hook\" \"$@\"\n"
		for _, name := range []string{"affiance-hook", "pre-commit", "commit-msg"} {
			if err := os.WriteFile(
				filepath.Join(hooksDir, name),
				[]byte(affianceContent),
				0o755,
			); err != nil {
				t.Fatal(err)
			}
		}

		if warnings := app.CheckHookCompatibility(tmpDir); len(warnings) != 0 {
			t.Errorf("Expected no warnings for affiance-only hooks, got %v", warnings)
		}
	})

	// Dropping the old affiance early-return also unmasked a real bug: while
	// that branch returned early, a hook genuinely requiring .git to be a
	// directory - which normalization can NOT fix - was skipped entirely
	// whenever affiance happened to be installed.
	t.Run("affiance presence no longer masks a directory-requiring hook", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult("", "exit status 1", fmt.Errorf("exit status 1"))

		tmpDir := t.TempDir()
		hooksDir := filepath.Join(tmpDir, ".git", "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(
			filepath.Join(hooksDir, "affiance-hook"),
			[]byte("#!/bin/bash\nnode $DIR/affiance-hook.js \"$@\"\n"),
			0o755,
		); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(hooksDir, "pre-commit"),
			[]byte("#!/bin/bash\n[ -d .git ] || exit 1\n"),
			0o755,
		); err != nil {
			t.Fatal(err)
		}

		warnings := app.CheckHookCompatibility(tmpDir)
		if len(warnings) != 1 {
			t.Fatalf(
				"Expected 1 warning for the pre-commit hook, got %d: %v",
				len(warnings),
				warnings,
			)
		}
		if !strings.Contains(warnings[0], "pre-commit") {
			t.Errorf("Expected the warning to name pre-commit, got %q", warnings[0])
		}
		if strings.Contains(strings.ToLower(warnings[0]), "affiance") {
			t.Errorf("Expected no affiance warning, got %q", warnings[0])
		}
	})
}

func TestPruneWorktrees(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful prune", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)

		if err := app.PruneWorktrees(); err != nil {
			t.Fatalf("PruneWorktrees failed: %v", err)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		expectedArgs := []string{"worktree", "prune"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	t.Run("prune error", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"",
			"fatal: not a git repository",
			fmt.Errorf("not a repo"),
		)

		err := app.PruneWorktrees()
		if err == nil {
			t.Fatal("Expected error but got none")
		}
		if !strings.Contains(err.Error(), "git: fatal: not a git repository") {
			t.Fatalf(
				"Expected error message to contain 'git: fatal: not a git repository', got: %v",
				err,
			)
		}
	})
}

func TestListBranches(t *testing.T) {
	t.Run("parses branch output", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("  main\n  feature-a\n* current", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		branches, err := g.ListBranches()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := []string{"main", "feature-a", "current"}
		if len(branches) != len(expected) {
			t.Fatalf("expected %v, got %v", expected, branches)
		}
		for i, v := range expected {
			if branches[i] != v {
				t.Errorf("index %d: expected %q, got %q", i, v, branches[i])
			}
		}
	})

	t.Run("strips current branch marker", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("* main\n  other", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		branches, err := g.ListBranches()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(branches) != 2 || branches[0] != "main" || branches[1] != "other" {
			t.Errorf("unexpected branches: %v", branches)
		}
	})

	t.Run("returns empty slice for no branches", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		branches, err := g.ListBranches()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(branches) != 0 {
			t.Fatalf("expected empty, got %v", branches)
		}
	})

	t.Run("returns error on git failure", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "fatal: not a repo", fmt.Errorf("exit 128"))
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		_, err := g.ListBranches()
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFetchOriginTimeout(t *testing.T) {
	t.Run("passes the timeout through to ExecCommand", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if err := g.FetchOriginTimeout(10 * time.Second); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		call := mockBase.GetLastExecCommandCall()
		if call == nil {
			t.Fatal("expected an ExecCommand call")
		}
		if call.Timeout != 10*time.Second {
			t.Fatalf("expected Timeout=10s, got %v", call.Timeout)
		}
		if call.Command != "git" || len(call.Args) != 2 || call.Args[0] != "fetch" ||
			call.Args[1] != "origin" {
			t.Fatalf("expected git fetch origin, got %s %v", call.Command, call.Args)
		}
	})

	t.Run("propagates error (simulates a timed-out fetch)", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "", fmt.Errorf("command timed out after 10s"))
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if err := g.FetchOriginTimeout(10 * time.Second); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFetchOriginRefspecsTimeout(t *testing.T) {
	t.Run("fetches only the named refspecs, no tags, bounded", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		err := g.FetchOriginRefspecsTimeout(
			10*time.Second,
			"+refs/pull/42/head:refs/devgeta/pr/42/head",
			"+refs/heads/main:refs/devgeta/pr/42/base",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		call := mockBase.GetLastExecCommandCall()
		if call == nil {
			t.Fatal("expected an ExecCommand call")
		}
		if call.Timeout != 10*time.Second {
			t.Fatalf("expected Timeout=10s, got %v", call.Timeout)
		}
		want := []string{
			"fetch", "--no-tags", "origin",
			"+refs/pull/42/head:refs/devgeta/pr/42/head",
			"+refs/heads/main:refs/devgeta/pr/42/base",
		}
		if call.Command != "git" || !slices.Equal(call.Args, want) {
			t.Fatalf("expected git %v, got %s %v", want, call.Command, call.Args)
		}
	})

	t.Run("every destination is a non-branch ref, so nothing local moves", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if err := g.FetchOriginRefspecsTimeout(
			time.Second,
			"+refs/pull/7/head:refs/devgeta/pr/7/head",
		); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// A refspec that wrote refs/heads/* or refs/remotes/* would move a
		// branch out from under the user — the thing this method exists to
		// avoid — and no argument here may name a checkout-changing verb.
		for _, arg := range mockBase.GetLastExecCommandCall().Args {
			if strings.Contains(arg, ":refs/heads/") || strings.Contains(arg, ":refs/remotes/") {
				t.Fatalf("refspec writes a branch ref: %q", arg)
			}
			if arg == "checkout" || arg == "switch" || arg == "reset" {
				t.Fatalf("unexpected working-tree command: %q", arg)
			}
		}
	})

	t.Run("refuses an empty refspec list before running anything", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if err := g.FetchOriginRefspecsTimeout(time.Second); err == nil {
			t.Fatal("expected error")
		}
		testutil.VerifyNoRealCommands(t, mockBase)
	})

	t.Run("reports git's stderr", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult(
			"",
			"couldn't find remote ref refs/pull/9/head",
			fmt.Errorf("exit 128"),
		)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		err := g.FetchOriginRefspecsTimeout(time.Second, "+refs/pull/9/head:refs/devgeta/pr/9/head")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "couldn't find remote ref") {
			t.Fatalf("expected git's stderr in the error, got %v", err)
		}
	})
}

func TestMergeBase(t *testing.T) {
	t.Run("returns the common ancestor", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("9f2c1ab8bc0d1e2f3a4b5c6d7e8f90a1b2c3d4e5\n", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		got, err := g.MergeBase("refs/devgeta/pr/42/base", "refs/devgeta/pr/42/head")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "9f2c1ab8bc0d1e2f3a4b5c6d7e8f90a1b2c3d4e5" {
			t.Fatalf("unexpected sha %q", got)
		}
		want := []string{"merge-base", "refs/devgeta/pr/42/base", "refs/devgeta/pr/42/head"}
		if call := mockBase.GetLastExecCommandCall(); !slices.Equal(call.Args, want) {
			t.Fatalf("expected git %v, got %v", want, call.Args)
		}
	})

	t.Run("errors when the two refs share no history", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "", fmt.Errorf("exit status 1"))
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := g.MergeBase("a", "b"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("errors on empty output rather than returning an empty sha", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("\n", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := g.MergeBase("a", "b"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestResolveCommit(t *testing.T) {
	t.Run("peels the ref to a commit sha", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("2f38a274cd0e1f2a3b4c5d6e7f8091a2b3c4d5e6\n", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		got, err := g.ResolveCommit("refs/devgeta/pr/42/head")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "2f38a274cd0e1f2a3b4c5d6e7f8091a2b3c4d5e6" {
			t.Fatalf("unexpected sha %q", got)
		}
		want := []string{"rev-parse", "--verify", "refs/devgeta/pr/42/head^{commit}"}
		if call := mockBase.GetLastExecCommandCall(); !slices.Equal(call.Args, want) {
			t.Fatalf("expected git %v, got %v", want, call.Args)
		}
	})

	t.Run("errors on an unknown ref", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult(
			"",
			"fatal: Needed a single revision",
			fmt.Errorf("exit status 128"),
		)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := g.ResolveCommit("refs/devgeta/pr/42/head"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("errors on empty output rather than returning an empty sha", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("  \n", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := g.ResolveCommit("HEAD"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestCurrentBranch(t *testing.T) {
	t.Run("returns the checked-out branch", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("feat/x\n", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		got, err := g.CurrentBranch()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "feat/x" {
			t.Fatalf("expected 'feat/x', got %q", got)
		}
	})

	t.Run("returns empty string when detached", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		got, err := g.CurrentBranch()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "fatal: not a repo", fmt.Errorf("exit 128"))
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := g.CurrentBranch(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShortHead(t *testing.T) {
	t.Run("returns the short sha", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("abc1234\n", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		got, err := g.ShortHead()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "abc1234" {
			t.Fatalf("expected 'abc1234', got %q", got)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "fatal: bad revision", fmt.Errorf("exit 128"))
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := g.ShortHead(); err == nil {
			t.Fatal("expected error")
		}
	})
}

// TestCommonDirIn pins the two flags ADR-0012's probes proved load-bearing:
// --git-common-dir (never --git-dir, which is per-worktree and would split
// per-branch state across checkouts) and --path-format=absolute (without it
// the main checkout answers a relative ".git" that would resolve against the
// caller's cwd).
func TestCommonDirIn(t *testing.T) {
	t.Run("asks for the absolute common dir and trims", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("/repos/app/.git\n", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		got, err := g.CommonDirIn("/repos/app/wt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/repos/app/.git" {
			t.Fatalf("expected '/repos/app/.git', got %q", got)
		}
		last := mockBase.GetLastExecCommandCall()
		for _, want := range []string{"--path-format=absolute", "--git-common-dir", "-C", "/repos/app/wt"} {
			if !slices.Contains(last.Args, want) {
				t.Errorf("expected %q in args %v", want, last.Args)
			}
		}
		if slices.Contains(last.Args, "--git-dir") {
			t.Errorf(
				"--git-dir is the per-worktree dir; must query --git-common-dir, args %v",
				last.Args,
			)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "fatal: not a git repository", fmt.Errorf("exit 128"))
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := g.CommonDirIn("/nowhere"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestHashObjectIn(t *testing.T) {
	t.Run("hashes the working-tree file with a -- guard", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("44858168\n", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		got, err := g.HashObjectIn("/repos/app", "pkg/file.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "44858168" {
			t.Fatalf("expected '44858168', got %q", got)
		}
		last := mockBase.GetLastExecCommandCall()
		for _, want := range []string{"hash-object", "--", "pkg/file.go"} {
			if !slices.Contains(last.Args, want) {
				t.Errorf("expected %q in args %v", want, last.Args)
			}
		}
	})

	t.Run("propagates error for a missing file", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "fatal: Cannot open", fmt.Errorf("exit 128"))
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		if _, err := g.HashObjectIn("/repos/app", "gone.go"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShortHeadIn(t *testing.T) {
	mockBase := commands.NewMockBaseCommand()
	mockBase.SetExecCommandResult("abc1234\n", "", nil)
	g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

	got, err := g.ShortHeadIn("/repos/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "abc1234" {
		t.Fatalf("expected 'abc1234', got %q", got)
	}
	last := mockBase.GetLastExecCommandCall()
	if !slices.Contains(last.Args, "-C") || !slices.Contains(last.Args, "/repos/app") {
		t.Errorf("expected -C /repos/app in args %v", last.Args)
	}
}

func TestRunCapture(t *testing.T) {
	t.Run("returns stdout on success", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("abc123\n", "", nil)
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		out, err := g.RunCapture("merge-base", "origin/main", "HEAD")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "abc123\n" {
			t.Fatalf("unexpected output: %q", out)
		}

		call := mockBase.GetLastExecCommandCall()
		if call == nil || call.Command != "git" {
			t.Fatalf("expected a git call, got %+v", call)
		}
	})

	t.Run("wraps stderr on failure", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		mockBase.SetExecCommandResult("", "fatal: bad object", fmt.Errorf("exit 128"))
		g := &Git{Cmd: commands.NewMockCommand(), Base: mockBase}

		_, err := g.RunCapture("merge-base", "origin/main", "HEAD")
		if err == nil || !strings.Contains(err.Error(), "bad object") {
			t.Fatalf("expected error containing stderr, got %v", err)
		}
	})
}

// TestParseWorktreeOutputPrunable covers the field this parser used to drop
// entirely. `git worktree list --porcelain` marks a registration whose
// working directory is gone with a "prunable" line; ignoring it made every
// caller treat administrative debris as a live worktree, so deleted
// worktrees kept appearing in `dg wt list` and the `dg ws` dashboard and
// every command run against their path failed with "cannot change to
// '<path>': No such file or directory".
func TestParseWorktreeOutputPrunable(t *testing.T) {
	t.Run("marks a prunable entry and captures git's reason", func(t *testing.T) {
		out := "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n" +
			"worktree /gone/wt\nHEAD def456\nbranch refs/heads/feature\n" +
			"prunable gitdir file points to non-existent location\n\n"

		worktrees := parseWorktreeOutput(out)
		if len(worktrees) != 2 {
			t.Fatalf("expected 2 worktrees, got %d: %+v", len(worktrees), worktrees)
		}
		if worktrees[0].Prunable {
			t.Error("main checkout must not be marked prunable")
		}
		if !worktrees[1].Prunable {
			t.Fatal("stale entry was not marked prunable — the marker is being dropped again")
		}
		const wantReason = "gitdir file points to non-existent location"
		if worktrees[1].PrunableReason != wantReason {
			t.Errorf("PrunableReason = %q, want %q", worktrees[1].PrunableReason, wantReason)
		}
		// The rest of the entry must still parse: callers need the path to
		// prune it and the name to report which worktree went missing.
		if worktrees[1].Path != "/gone/wt" {
			t.Errorf("Path = %q, want /gone/wt", worktrees[1].Path)
		}
		if worktrees[1].Branch != "feature" {
			t.Errorf("Branch = %q, want feature", worktrees[1].Branch)
		}
	})

	t.Run("marks a bare prunable line with no reason", func(t *testing.T) {
		out := "worktree /gone/wt\nHEAD def456\nbranch refs/heads/feature\nprunable\n\n"

		worktrees := parseWorktreeOutput(out)
		if len(worktrees) != 1 {
			t.Fatalf("expected 1 worktree, got %d", len(worktrees))
		}
		if !worktrees[0].Prunable {
			t.Fatal("bare 'prunable' line was not recognized")
		}
		if worktrees[0].PrunableReason != "" {
			t.Errorf("PrunableReason = %q, want empty for a bare marker",
				worktrees[0].PrunableReason)
		}
	})

	t.Run("a healthy worktree is never marked prunable", func(t *testing.T) {
		out := "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n" +
			"worktree /repo/.claude/worktrees/feat\nHEAD def456\nbranch refs/heads/feat\n\n"

		for _, wt := range parseWorktreeOutput(out) {
			if wt.Prunable {
				t.Errorf("healthy worktree %q was marked prunable", wt.Path)
			}
		}
	})

	// "prunable" must not be confused with any other porcelain line that
	// happens to start with the same letters, and a branch literally named
	// "prunable-something" must not trip the marker.
	t.Run("a branch whose name starts with 'prunable' does not set the flag", func(t *testing.T) {
		out := "worktree /repo/wt\nHEAD abc\nbranch refs/heads/prunable-cleanup\n\n"

		worktrees := parseWorktreeOutput(out)
		if len(worktrees) != 1 {
			t.Fatalf("expected 1 worktree, got %d", len(worktrees))
		}
		if worktrees[0].Prunable {
			t.Error("branch named 'prunable-cleanup' wrongly set the prunable flag")
		}
		if worktrees[0].Branch != "prunable-cleanup" {
			t.Errorf("Branch = %q, want prunable-cleanup", worktrees[0].Branch)
		}
	})
}

// TestGitWarnFn locks in that git's non-fatal advisories go through WarnFn
// rather than a raw fmt.Printf. A raw print from inside a create scribbles
// straight into a running Bubble Tea alt-screen (the `dg ws` dashboard),
// scrolling the frame and leaving two frames' worth of rows interleaved on
// screen — which is what made the dashboard look like it had duplicated and
// nested worktrees that did not exist.
func TestGitWarnFn(t *testing.T) {
	t.Run("New wires a non-nil default so warn never panics", func(t *testing.T) {
		if New().WarnFn == nil {
			t.Fatal("New() left WarnFn nil; git advisories would panic or fall back silently")
		}
	})

	t.Run("syncExistingBranch reports a diverged branch via WarnFn", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		var warnings []string
		app := &Git{
			Cmd:    mockApp.Cmd,
			Base:   mockApp.Base,
			WarnFn: func(msg string) { warnings = append(warnings, msg) },
		}

		mockApp.Base.SetExecCommandResults(
			// RemoteBranchExistsIn -> the remote branch exists
			commands.ExecCommandResult("origin/feature\n", "", nil),
			// merge --ff-only fails: histories diverged
			commands.ExecCommandResult("", "fatal: not possible to fast-forward",
				fmt.Errorf("merge failed")),
		)

		if err := app.syncExistingBranch("/wt", "feature"); err != nil {
			t.Fatalf("syncExistingBranch must not fail on a diverged branch: %v", err)
		}
		if len(warnings) != 1 {
			t.Fatalf("expected exactly 1 warning via WarnFn, got %d: %v", len(warnings), warnings)
		}
		if !strings.Contains(warnings[0], "diverged") {
			t.Errorf("warning should explain the divergence, got: %q", warnings[0])
		}
		// Every git call this drove went through mockApp.Base, so nothing
		// real ran. VerifyNoRealCommands is deliberately NOT used here: it
		// asserts zero exec calls, and this test exists precisely to drive
		// two of them.
	})

	t.Run("warn falls back to a print when WarnFn is nil, without panicking", func(t *testing.T) {
		// A zero-value Git (struct literal, as tests and some callers build)
		// must not panic the moment an advisory fires.
		app := &Git{}
		app.warn("advisory from a zero-value Git")
	})
}

// TestNoRawPrintsInGitPackage makes the display-corruption bug structurally
// impossible to reintroduce rather than merely fixed once. A single
// fmt.Print/Println/Printf anywhere in this package writes straight to stdout
// — and when the caller is the `dg ws` dashboard, that means writing into a
// running Bubble Tea alt-screen, which scrolls the frame and leaves stale
// rows interleaved with live ones. Advisories must go through Git.WarnFn
// (see the warn helper), which a TUI caller can redirect.
//
// A convention in a comment would not have survived; this fails the build.
func TestNoRawPrintsInGitPackage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("failed to read package directory: %v", err)
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatalf("failed to read %s: %v", name, readErr)
		}
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // a comment mentioning fmt.Print is not a call
			}
			for _, banned := range []string{"fmt.Print(", "fmt.Printf(", "fmt.Println("} {
				if strings.Contains(line, banned) {
					t.Errorf(
						"%s:%d uses %s — route the message through g.warn() instead, "+
							"or it will corrupt a TUI caller's alt-screen display:\n\t%s",
						name, i+1, banned, trimmed,
					)
				}
			}
		}
	}
}

// TestFreeBranchIfHeldElsewhereSkipsPrunable covers the last way a stale
// registration could block a user: it still holds the BRANCH, so recreating
// the worktree hits this pre-flight check first. Treating the dead entry as a
// live holder ran IsWorktreeDirty against a path that no longer exists, and
// that failure's message is not one create() recognizes as a stale-entry
// problem — so creation dead-ended before prune-and-retry could rescue it,
// leaving the branch name permanently unusable.
func TestFreeBranchIfHeldElsewhereSkipsPrunable(t *testing.T) {
	t.Run("a prunable holder is ignored, leaving nothing to free", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base, WarnFn: func(string) {}}

		// The only entry holding "feature" is stale: its directory is gone.
		porcelain := "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n" +
			"worktree /gone/wt\nHEAD def\nbranch refs/heads/feature\n" +
			"prunable gitdir file points to non-existent location\n\n"
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult(porcelain, "", nil), // ListWorktreesAt
		)

		if err := app.freeBranchIfHeldElsewhere(
			"/repo",
			"/repo/wt/feature",
			"feature",
		); err != nil {
			t.Fatalf("a stale holder must be a no-op, not an error: %v", err)
		}

		// Exactly one call: the listing. Anything more means it tried to
		// inspect or check out a directory that does not exist.
		if n := mockApp.Base.GetExecCommandCallCount(); n != 1 {
			t.Errorf(
				"expected only the worktree listing (1 call), got %d — "+
					"a git command was run against the missing directory",
				n,
			)
		}
	})

	t.Run("a live holder is still freed", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		var warnings []string
		app := &Git{
			Cmd:    mockApp.Cmd,
			Base:   mockApp.Base,
			WarnFn: func(msg string) { warnings = append(warnings, msg) },
		}

		porcelain := "worktree /repo\nHEAD abc\nbranch refs/heads/feature\n\n"
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult(porcelain, "", nil), // ListWorktreesAt
			commands.ExecCommandResult("main\n", "", nil),  // DefaultBranchIn
			commands.ExecCommandResult("", "", nil),        // IsWorktreeDirty - clean
			commands.ExecCommandResult("", "", nil),        // checkout main
		)

		if err := app.freeBranchIfHeldElsewhere(
			"/repo",
			"/repo/wt/feature",
			"feature",
		); err != nil {
			t.Fatalf("freeBranchIfHeldElsewhere failed on a live holder: %v", err)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "was moved to") {
			t.Errorf("expected the source-checkout-moved notice, got %v", warnings)
		}
	})

	// Both a live and a stale holder can claim the same branch after a
	// worktree was recreated by hand. The live one must win.
	t.Run("a live holder is chosen over a stale one for the same branch", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &Git{Cmd: mockApp.Cmd, Base: mockApp.Base, WarnFn: func(string) {}}

		porcelain := "worktree /gone/wt\nHEAD def\nbranch refs/heads/feature\n" +
			"prunable gitdir file points to non-existent location\n\n" +
			"worktree /repo\nHEAD abc\nbranch refs/heads/feature\n\n"
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult(porcelain, "", nil), // ListWorktreesAt
			commands.ExecCommandResult("main\n", "", nil),  // DefaultBranchIn
			commands.ExecCommandResult("", "", nil),        // IsWorktreeDirty
			commands.ExecCommandResult("", "", nil),        // checkout
		)

		if err := app.freeBranchIfHeldElsewhere(
			"/repo",
			"/repo/wt/feature",
			"feature",
		); err != nil {
			t.Fatalf("expected the live holder to be freed: %v", err)
		}
		// The checkout must target the live path, never the missing one.
		for _, call := range mockApp.Base.ExecCommandCalls {
			for i, a := range call.Args {
				if a == "-C" && i+1 < len(call.Args) && call.Args[i+1] == "/gone/wt" {
					t.Errorf("a git command was run against the stale path: %v", call.Args)
				}
			}
		}
	})
}

// NormalizeWorktreeGitfile is the structural fix for strict third-party parsers
// of the .git file (ADR-0013). These tests are pure filesystem work - no git is
// executed - so the mock executor stays untouched throughout.
func TestNormalizeWorktreeGitfile(t *testing.T) {
	newApp := func() (*Git, *testutil.MockApp) {
		mockApp := testutil.NewMockApp()
		return &Git{Cmd: mockApp.Cmd, Base: mockApp.Base}, mockApp
	}

	t.Run("strips the trailing newline git writes", func(t *testing.T) {
		app, mockApp := newApp()
		wtPath := t.TempDir()
		gitfile := filepath.Join(wtPath, ".git")
		// Byte-for-byte what `git worktree add` produces.
		if err := os.WriteFile(
			gitfile,
			[]byte("gitdir: /repo/.git/worktrees/foo\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		if err := app.NormalizeWorktreeGitfile(wtPath); err != nil {
			t.Fatalf("NormalizeWorktreeGitfile returned error: %v", err)
		}

		got, err := os.ReadFile(gitfile)
		if err != nil {
			t.Fatal(err)
		}
		if want := "gitdir: /repo/.git/worktrees/foo"; string(got) != want {
			t.Errorf("gitfile = %q, want %q", got, want)
		}
		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("result matches the strict end-of-line regex that broke", func(t *testing.T) {
		app, _ := newApp()
		wtPath := t.TempDir()
		gitfile := filepath.Join(wtPath, ".git")
		if err := os.WriteFile(
			gitfile,
			[]byte("gitdir: /repo/.git/worktrees/foo\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		// Go equivalent of affiance's /^gitdir: (.*)$/ WITHOUT multiline: `$`
		// binds to end of text and `.` excludes newlines, exactly as in JS. This
		// is the assertion that actually protects the user-visible behavior - if
		// a future edit stops trimming, commits break again in worktrees.
		strict := regexp.MustCompile(`\Agitdir: ([^\n]*)\z`)

		before, err := os.ReadFile(gitfile)
		if err != nil {
			t.Fatal(err)
		}
		if strict.Match(before) {
			t.Fatal("precondition failed: git's own output should NOT match the strict regex")
		}

		if err := app.NormalizeWorktreeGitfile(wtPath); err != nil {
			t.Fatalf("NormalizeWorktreeGitfile returned error: %v", err)
		}

		after, err := os.ReadFile(gitfile)
		if err != nil {
			t.Fatal(err)
		}
		m := strict.FindSubmatch(after)
		if m == nil {
			t.Fatalf("normalized gitfile %q still does not match the strict regex", after)
		}
		if got := string(m[1]); got != "/repo/.git/worktrees/foo" {
			t.Errorf("captured gitdir = %q, want %q", got, "/repo/.git/worktrees/foo")
		}
	})

	t.Run("is idempotent on already-trimmed content", func(t *testing.T) {
		app, _ := newApp()
		wtPath := t.TempDir()
		gitfile := filepath.Join(wtPath, ".git")
		want := "gitdir: /repo/.git/worktrees/foo"
		if err := os.WriteFile(gitfile, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}

		for i := 0; i < 3; i++ {
			if err := app.NormalizeWorktreeGitfile(wtPath); err != nil {
				t.Fatalf("call %d returned error: %v", i, err)
			}
		}

		got, err := os.ReadFile(gitfile)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("gitfile = %q, want %q", got, want)
		}
	})

	t.Run("strips trailing CRLF and blank lines too", func(t *testing.T) {
		app, _ := newApp()
		wtPath := t.TempDir()
		gitfile := filepath.Join(wtPath, ".git")
		if err := os.WriteFile(
			gitfile,
			[]byte("gitdir: /repo/.git/worktrees/foo\r\n\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		if err := app.NormalizeWorktreeGitfile(wtPath); err != nil {
			t.Fatalf("NormalizeWorktreeGitfile returned error: %v", err)
		}

		got, err := os.ReadFile(gitfile)
		if err != nil {
			t.Fatal(err)
		}
		if want := "gitdir: /repo/.git/worktrees/foo"; string(got) != want {
			t.Errorf("gitfile = %q, want %q", got, want)
		}
	})

	t.Run("preserves the file mode", func(t *testing.T) {
		app, _ := newApp()
		wtPath := t.TempDir()
		gitfile := filepath.Join(wtPath, ".git")
		if err := os.WriteFile(
			gitfile,
			[]byte("gitdir: /repo/.git/worktrees/foo\n"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}

		if err := app.NormalizeWorktreeGitfile(wtPath); err != nil {
			t.Fatalf("NormalizeWorktreeGitfile returned error: %v", err)
		}

		info, err := os.Stat(gitfile)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("mode = %o, want %o", got, 0o600)
		}
	})

	t.Run("no-ops on a main checkout where .git is a directory", func(t *testing.T) {
		app, _ := newApp()
		wtPath := t.TempDir()
		gitDir := filepath.Join(wtPath, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := app.NormalizeWorktreeGitfile(wtPath); err != nil {
			t.Fatalf("expected no error for a main checkout, got %v", err)
		}

		info, err := os.Stat(gitDir)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Error(".git should still be a directory")
		}
	})

	t.Run("no-ops when there is no .git entry at all", func(t *testing.T) {
		app, _ := newApp()
		if err := app.NormalizeWorktreeGitfile(t.TempDir()); err != nil {
			t.Errorf("expected no error for a path with no .git, got %v", err)
		}
	})
}
