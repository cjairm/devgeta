package task

import (
	"fmt"
	"strings"
	"testing"

	gitapp "github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

// newTaskSetup creates a TaskManager with isolated mock bases for git and npm.
func newTaskSetup() (tm *TaskManager, gitBase *commands.MockBaseCommand, npmBase *commands.MockBaseCommand) {
	gitBase = commands.NewMockBaseCommand()
	npmBase = commands.NewMockBaseCommand()
	tm = &TaskManager{
		Git: &gitapp.Git{
			Cmd:  commands.NewMockCommand(),
			Base: gitBase,
		},
		Base: npmBase,
	}
	return
}

func TestRefreshBranch(t *testing.T) {
	t.Run("default target main", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()

		if err := tm.RefreshBranch("", false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		calls := gitBase.ExecCommandCalls
		if len(calls) != 4 {
			t.Fatalf("expected 4 ExecCommand calls, got %d", len(calls))
		}
		assertCmd(t, calls[0], "git", "checkout", "main")
		assertCmd(t, calls[1], "git", "pull", "origin", "main")
		assertCmd(t, calls[2], "git", "checkout", "-")
		assertCmd(t, calls[3], "git", "merge", "main")
	})

	t.Run("custom target", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()

		if err := tm.RefreshBranch("develop", false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		calls := gitBase.ExecCommandCalls
		assertCmd(t, calls[0], "git", "checkout", "develop")
		assertCmd(t, calls[3], "git", "merge", "develop")
	})

	t.Run("propagates error", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResult("", "fatal", fmt.Errorf("not a git repo"))

		err := tm.RefreshBranch("", false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "refresh-branch") {
			t.Fatalf("expected error to mention refresh-branch, got: %v", err)
		}
	})

	// --rebase (Slice B): the merge path stays the default and byte-for-byte
	// unchanged (the two cases above) — this only adds a second option.
	t.Run("rebase rebases instead of merging", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()

		if err := tm.RefreshBranch("", true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		calls := gitBase.ExecCommandCalls
		if len(calls) != 4 {
			t.Fatalf("expected 4 ExecCommand calls, got %d", len(calls))
		}
		assertCmd(t, calls[0], "git", "checkout", "main")
		assertCmd(t, calls[1], "git", "pull", "origin", "main")
		assertCmd(t, calls[2], "git", "checkout", "-")
		assertCmd(t, calls[3], "git", "rebase", "main")
	})

	t.Run("rebase with a custom target", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()

		if err := tm.RefreshBranch("develop", true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		calls := gitBase.ExecCommandCalls
		assertCmd(t, calls[0], "git", "checkout", "develop")
		assertCmd(t, calls[3], "git", "rebase", "develop")
	})

	t.Run("rebase propagates error", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResult("", "fatal", fmt.Errorf("not a git repo"))

		err := tm.RefreshBranch("", true)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "refresh-branch") {
			t.Fatalf("expected error to mention refresh-branch, got: %v", err)
		}
	})
}

func TestResetMainBranch(t *testing.T) {
	t.Run("runs expected commands", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()

		if err := tm.ResetMainBranch(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		calls := gitBase.ExecCommandCalls
		if len(calls) != 2 {
			t.Fatalf("expected 2 ExecCommand calls, got %d", len(calls))
		}
		assertCmd(t, calls[0], "git", "checkout", "main")
		assertCmd(t, calls[1], "git", "reset", "--hard", "origin/main")
	})

	t.Run("propagates error", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResult("", "", fmt.Errorf("checkout failed"))

		err := tm.ResetMainBranch()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "reset-main-branch") {
			t.Fatalf("expected error to mention reset-main-branch, got: %v", err)
		}
	})
}

func TestReinstallLibraries(t *testing.T) {
	t.Run("runs expected commands", func(t *testing.T) {
		tm, gitBase, npmBase := newTaskSetup()

		if err := tm.ReinstallLibraries(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// git clean goes through the git app's base
		gitCalls := gitBase.ExecCommandCalls
		if len(gitCalls) != 1 {
			t.Fatalf("expected 1 git ExecCommand call, got %d", len(gitCalls))
		}
		assertCmd(t, gitCalls[0], "git", "clean", "-Xdf")

		// npm install goes through the task's own base
		npmCalls := npmBase.ExecCommandCalls
		if len(npmCalls) != 1 {
			t.Fatalf("expected 1 npm ExecCommand call, got %d", len(npmCalls))
		}
		assertCmd(t, npmCalls[0], "npm", "install")
	})

	t.Run("propagates git error", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResult("", "", fmt.Errorf("git clean failed"))

		err := tm.ReinstallLibraries()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "reinstall-libraries") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("propagates npm error", func(t *testing.T) {
		tm, _, npmBase := newTaskSetup()
		npmBase.SetExecCommandResult("", "", fmt.Errorf("npm not found"))

		err := tm.ReinstallLibraries()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "npm install failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestReinstallLibrary(t *testing.T) {
	t.Run("runs npm install", func(t *testing.T) {
		tm, _, npmBase := newTaskSetup()

		if err := tm.ReinstallLibrary("lodash"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		npmCalls := npmBase.ExecCommandCalls
		if len(npmCalls) != 1 {
			t.Fatalf("expected 1 npm ExecCommand call, got %d", len(npmCalls))
		}
		assertCmd(t, npmCalls[0], "npm", "install")
	})

	t.Run("requires name", func(t *testing.T) {
		tm, gitBase, npmBase := newTaskSetup()

		err := tm.ReinstallLibrary("")
		if err == nil {
			t.Fatal("expected error for empty name")
		}
		testutil.VerifyNoRealCommands(t, gitBase)
		testutil.VerifyNoRealCommands(t, npmBase)
	})

	t.Run("propagates npm error", func(t *testing.T) {
		tm, _, npmBase := newTaskSetup()
		npmBase.SetExecCommandResult("", "", fmt.Errorf("npm not found"))

		err := tm.ReinstallLibrary("lodash")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "reinstall-library") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestStreamsOutput(t *testing.T) {
	t.Run("npm install streams output", func(t *testing.T) {
		tm, _, npmBase := newTaskSetup()

		if err := tm.ReinstallLibrary("lodash"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		npmCalls := npmBase.ExecCommandCalls
		if len(npmCalls) != 1 {
			t.Fatalf("expected 1 npm ExecCommand call, got %d", len(npmCalls))
		}
		if !npmCalls[0].Stream {
			t.Error("expected npm install to run with Stream=true for real-time progress")
		}
	})

	t.Run("New enables git streaming", func(t *testing.T) {
		tm := New()
		if !tm.Git.Stream {
			t.Error("expected New() to enable Git.Stream so task git output is shown live")
		}
	})
}

func TestDeleteBranch_Setup(t *testing.T) {
	t.Run("setup propagates error on checkout failure", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResult("", "not a git repo", fmt.Errorf("not a git repo"))

		err := tm.DeleteBranch("")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "delete-branch setup") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("default target is main", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		// Return empty branch list so DeleteBranch exits before reaching fzf.
		// This lets us verify the setup commands used "main" without blocking on stdin.
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil), // git checkout main
			commands.ExecCommandResult("", "", nil), // git fetch origin
			commands.ExecCommandResult("", "", nil), // git pull origin main
			commands.ExecCommandResult("", "", nil), // git branch → empty → "no branches" error
		)

		err := tm.DeleteBranch("")
		if err == nil {
			t.Fatal("expected error (no branches)")
		}

		calls := gitBase.ExecCommandCalls
		if len(calls) < 3 {
			t.Fatalf("expected at least 3 setup calls, got %d", len(calls))
		}
		assertCmd(t, calls[0], "git", "checkout", "main")
		assertCmd(t, calls[1], "git", "fetch", "origin")
		assertCmd(t, calls[2], "git", "pull", "origin", "main")
	})
}

// assertCmd checks that a CommandParams has the expected command and args.
func assertCmd(t *testing.T, p commands.CommandParams, wantCmd string, wantArgs ...string) {
	t.Helper()
	if p.Command != wantCmd {
		t.Errorf("expected command %q, got %q", wantCmd, p.Command)
	}
	if len(p.Args) != len(wantArgs) {
		t.Errorf("expected args %v, got %v", wantArgs, p.Args)
		return
	}
	for i, a := range wantArgs {
		if p.Args[i] != a {
			t.Errorf("arg[%d]: expected %q, got %q", i, a, p.Args[i])
		}
	}
}
