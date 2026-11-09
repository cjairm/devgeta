package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// mockTaskRunner records calls to each task method.
type mockTaskRunner struct {
	refreshBranchArg       string
	refreshBranchRebaseArg bool
	refreshBranchCalled    bool
	refreshBranchErr       error

	resetMainCalled bool
	resetMainErr    error

	reinstallLibsCalled bool
	reinstallLibsErr    error

	reinstallLibArg    string
	reinstallLibCalled bool
	reinstallLibErr    error

	deleteBranchArg    string
	deleteBranchCalled bool
	deleteBranchErr    error

	releaseVersionArg     string
	releaseMessageFileArg string
	releasePushArg        bool
	releaseCalled         bool
	releaseRet            string
	releaseErr            error

	scratchCalled bool
	scratchKeyArg string
	scratchRet    string
	scratchErr    error

	scratchCleanArg    string
	scratchCleanCalled bool
	scratchCleanErr    error
}

func (m *mockTaskRunner) RefreshBranch(target string, rebase bool) error {
	m.refreshBranchCalled = true
	m.refreshBranchArg = target
	m.refreshBranchRebaseArg = rebase
	return m.refreshBranchErr
}

func (m *mockTaskRunner) ResetMainBranch() error {
	m.resetMainCalled = true
	return m.resetMainErr
}

func (m *mockTaskRunner) ReinstallLibraries() error {
	m.reinstallLibsCalled = true
	return m.reinstallLibsErr
}

func (m *mockTaskRunner) ReinstallLibrary(name string) error {
	m.reinstallLibCalled = true
	m.reinstallLibArg = name
	return m.reinstallLibErr
}

func (m *mockTaskRunner) DeleteBranch(target string) error {
	m.deleteBranchCalled = true
	m.deleteBranchArg = target
	return m.deleteBranchErr
}

func (m *mockTaskRunner) Release(version, messageFile string, push bool) (string, error) {
	m.releaseCalled = true
	m.releaseVersionArg = version
	m.releaseMessageFileArg = messageFile
	m.releasePushArg = push
	return m.releaseRet, m.releaseErr
}

func (m *mockTaskRunner) Scratch(key string) (string, error) {
	m.scratchCalled = true
	m.scratchKeyArg = key
	return m.scratchRet, m.scratchErr
}

func (m *mockTaskRunner) ScratchClean(target string) error {
	m.scratchCleanCalled = true
	m.scratchCleanArg = target
	return m.scratchCleanErr
}

func setupTaskMock(t *testing.T, mock taskRunner) func() {
	t.Helper()
	orig := newTaskManager
	newTaskManager = func() taskRunner { return mock }
	return func() { newTaskManager = orig }
}

func TestTask_RefreshBranch(t *testing.T) {
	t.Run("no args defaults to empty target", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskRefreshBranchCmd.RunE(taskRefreshBranchCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.refreshBranchCalled {
			t.Error("expected RefreshBranch to be called")
		}
		if mock.refreshBranchArg != "" {
			t.Errorf("expected empty target, got %q", mock.refreshBranchArg)
		}
	})

	t.Run("passes target arg", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskRefreshBranchCmd.RunE(taskRefreshBranchCmd, []string{"develop"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.refreshBranchArg != "develop" {
			t.Errorf("expected target 'develop', got %q", mock.refreshBranchArg)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{refreshBranchErr: fmt.Errorf("git failed")}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskRefreshBranchCmd.RunE(taskRefreshBranchCmd, []string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("--rebase defaults to false", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskRefreshBranchRebaseFlag = false

		if err := taskRefreshBranchCmd.RunE(taskRefreshBranchCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.refreshBranchRebaseArg {
			t.Error("expected rebase=false by default")
		}
	})

	t.Run("--rebase is passed through", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskRefreshBranchRebaseFlag = true
		defer func() { taskRefreshBranchRebaseFlag = false }()

		if err := taskRefreshBranchCmd.RunE(taskRefreshBranchCmd, []string{"develop"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.refreshBranchRebaseArg {
			t.Error("expected rebase=true to be passed through")
		}
		if mock.refreshBranchArg != "develop" {
			t.Errorf("expected target 'develop', got %q", mock.refreshBranchArg)
		}
	})
}

func TestTask_ResetMainBranch(t *testing.T) {
	t.Run("calls ResetMainBranch", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskResetMainBranchCmd.RunE(taskResetMainBranchCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.resetMainCalled {
			t.Error("expected ResetMainBranch to be called")
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{resetMainErr: fmt.Errorf("reset failed")}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskResetMainBranchCmd.RunE(taskResetMainBranchCmd, []string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_ReinstallLibraries(t *testing.T) {
	t.Run("calls ReinstallLibraries", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskReinstallLibrariesCmd.RunE(taskReinstallLibrariesCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reinstallLibsCalled {
			t.Error("expected ReinstallLibraries to be called")
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{reinstallLibsErr: fmt.Errorf("clean failed")}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskReinstallLibrariesCmd.RunE(taskReinstallLibrariesCmd, []string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_ReinstallLibrary(t *testing.T) {
	t.Run("passes library name", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskReinstallLibraryCmd.RunE(taskReinstallLibraryCmd, []string{"lodash"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reinstallLibCalled {
			t.Error("expected ReinstallLibrary to be called")
		}
		if mock.reinstallLibArg != "lodash" {
			t.Errorf("expected 'lodash', got %q", mock.reinstallLibArg)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{reinstallLibErr: fmt.Errorf("rm failed")}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskReinstallLibraryCmd.RunE(taskReinstallLibraryCmd, []string{"lodash"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_DeleteBranch(t *testing.T) {
	t.Run("no args passes empty target", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskDeleteBranchCmd.RunE(taskDeleteBranchCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.deleteBranchCalled {
			t.Error("expected DeleteBranch to be called")
		}
		if mock.deleteBranchArg != "" {
			t.Errorf("expected empty target, got %q", mock.deleteBranchArg)
		}
	})

	t.Run("passes target arg", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskDeleteBranchCmd.RunE(taskDeleteBranchCmd, []string{"develop"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.deleteBranchArg != "develop" {
			t.Errorf("expected 'develop', got %q", mock.deleteBranchArg)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{deleteBranchErr: fmt.Errorf("delete failed")}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskDeleteBranchCmd.RunE(taskDeleteBranchCmd, []string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_Release(t *testing.T) {
	t.Run("passes version, message-file, and push flags", func(t *testing.T) {
		mock := &mockTaskRunner{releaseRet: "Tagged v0.12.0 (squashed 3 commits)."}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReleaseMessageFileFlag, taskReleasePushFlag = "release-notes.txt", true
		defer func() { taskReleaseMessageFileFlag, taskReleasePushFlag = "", false }()

		err := taskReleaseCmd.RunE(taskReleaseCmd, []string{"v0.12.0"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.releaseCalled {
			t.Error("expected Release to be called")
		}
		if mock.releaseVersionArg != "v0.12.0" {
			t.Errorf("expected version 'v0.12.0', got %q", mock.releaseVersionArg)
		}
		if mock.releaseMessageFileArg != "release-notes.txt" {
			t.Errorf(
				"expected message-file 'release-notes.txt', got %q",
				mock.releaseMessageFileArg,
			)
		}
		if !mock.releasePushArg {
			t.Error("expected push=true to be passed through")
		}
	})

	t.Run("defaults push to false", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReleaseMessageFileFlag, taskReleasePushFlag = "release-notes.txt", false
		defer func() { taskReleaseMessageFileFlag, taskReleasePushFlag = "", false }()

		err := taskReleaseCmd.RunE(taskReleaseCmd, []string{"v0.12.0"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.releasePushArg {
			t.Error("expected push=false by default")
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{releaseErr: fmt.Errorf("dirty tree")}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReleaseMessageFileFlag = "release-notes.txt"
		defer func() { taskReleaseMessageFileFlag = "" }()

		err := taskReleaseCmd.RunE(taskReleaseCmd, []string{"v0.12.0"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_Scratch(t *testing.T) {
	t.Run("bare call allocates", func(t *testing.T) {
		mock := &mockTaskRunner{scratchRet: "/home/user/.cache/devgeta/scratch/task-abc123"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskScratchCleanFlag = ""

		err := taskScratchCmd.RunE(taskScratchCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.scratchCalled {
			t.Error("expected Scratch to be called")
		}
		if mock.scratchCleanCalled {
			t.Error("expected ScratchClean NOT to be called")
		}
	})

	t.Run("bare call propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{scratchErr: fmt.Errorf("failed to ensure scratch dir")}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskScratchCleanFlag = ""

		err := taskScratchCmd.RunE(taskScratchCmd, []string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("--key is passed through to Scratch", func(t *testing.T) {
		mock := &mockTaskRunner{scratchRet: "/home/user/.cache/devgeta/scratch/key-demo"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskScratchCleanFlag = ""
		taskScratchKeyFlag = "demo"
		defer func() { taskScratchKeyFlag = "" }()

		err := taskScratchCmd.RunE(taskScratchCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.scratchCalled {
			t.Error("expected Scratch to be called")
		}
		if mock.scratchKeyArg != "demo" {
			t.Errorf(
				"expected Scratch to be called with key %q, got %q",
				"demo",
				mock.scratchKeyArg,
			)
		}
	})

	t.Run("--clean calls ScratchClean with the flag value, not Scratch", func(t *testing.T) {
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskScratchCleanFlag = "/home/user/.cache/devgeta/scratch/task-abc123"
		defer func() { taskScratchCleanFlag = "" }()

		err := taskScratchCmd.RunE(taskScratchCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.scratchCleanCalled {
			t.Error("expected ScratchClean to be called")
		}
		if mock.scratchCalled {
			t.Error("expected Scratch NOT to be called")
		}
		if mock.scratchCleanArg != taskScratchCleanFlag {
			t.Errorf(
				"expected ScratchClean arg %q, got %q",
				taskScratchCleanFlag,
				mock.scratchCleanArg,
			)
		}
	})

	t.Run("--clean propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{scratchCleanErr: fmt.Errorf("not under the scratch root")}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskScratchCleanFlag = "/etc"
		defer func() { taskScratchCleanFlag = "" }()

		err := taskScratchCmd.RunE(taskScratchCmd, []string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_UnknownSubcommandFails(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	rootCmd.SetArgs([]string{"task", "handoff"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown command "handoff"`) {
		t.Fatalf("expected an unknown command error for a removed subcommand, got %v", err)
	}

	rootCmd.SetArgs([]string{"task"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("bare `dg task` should print help and succeed, got %v", err)
	}
}
