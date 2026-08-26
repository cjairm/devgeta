package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/tooling/task"
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

	reviewScopeCalled bool
	reviewScopeBodies bool
	reviewScopeSizes  bool
	reviewScopeRet    string
	reviewScopeErr    error

	branchDiffArg    string
	branchDiffCalled bool
	branchDiffRet    string
	branchDiffErr    error

	reviewPackageBaseArg string
	reviewPackageHeadArg string
	reviewPackageFileArg string
	reviewPackageCalled  bool
	reviewPackageRet     string
	reviewPackageErr     error

	reviewNotesBranchArg string
	reviewNotesRevArg    string
	reviewNotesPathArg   bool
	reviewNotesPruneArg  bool
	reviewNotesCalled    bool
	reviewNotesRet       string
	reviewNotesErr       error

	reviewNoteOpenBranchArg string
	reviewNoteOpenRevArg    string
	reviewNoteOpenCiteArg   string
	reviewNoteOpenNoteArg   string
	reviewNoteOpenCalled    bool
	reviewNoteOpenRet       string
	reviewNoteOpenErr       error

	reviewNoteSettleBranchArg string
	reviewNoteSettleRevArg    string
	reviewNoteSettleIDArg     string
	reviewNoteSettleAsArg     string
	reviewNoteSettleAtArg     string
	reviewNoteSettleNoteArg   string
	reviewNoteSettleCalled    bool
	reviewNoteSettleRet       string
	reviewNoteSettleErr       error

	reviewNoteRatifyBranchArg string
	reviewNoteRatifyIDArg     string
	reviewNoteRatifyCalled    bool
	reviewNoteRatifyRet       string
	reviewNoteRatifyErr       error

	reviewNoteReopenBranchArg string
	reviewNoteReopenIDArg     string
	reviewNoteReopenCalled    bool
	reviewNoteReopenRet       string
	reviewNoteReopenErr       error

	reviewRunReviewerArg string
	reviewRunNoteArg     string
	reviewRunRangeArg    task.ReviewRange
	reviewRunCalled      bool
	reviewRunRet         string
	reviewRunErr         error

	worktreeStartNameArg string
	worktreeStartBaseArg string
	worktreeStartCalled  bool
	worktreeStartRet     string
	worktreeStartErr     error

	worktreeFinishNameArg    string
	worktreeFinishMergeArg   bool
	worktreeFinishDiscardArg bool
	worktreeFinishCheckArg   bool
	worktreeFinishForceArg   bool
	worktreeFinishCalled     bool
	worktreeFinishRet        string
	worktreeFinishErr        error

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

func (m *mockTaskRunner) ReviewScope(bodies, sizes bool) (string, error) {
	m.reviewScopeCalled = true
	m.reviewScopeBodies = bodies
	m.reviewScopeSizes = sizes
	return m.reviewScopeRet, m.reviewScopeErr
}

func (m *mockTaskRunner) BranchDiff(file string) (string, error) {
	m.branchDiffCalled = true
	m.branchDiffArg = file
	return m.branchDiffRet, m.branchDiffErr
}

func (m *mockTaskRunner) ReviewPackage(base, head, file string) (string, error) {
	m.reviewPackageCalled = true
	m.reviewPackageBaseArg = base
	m.reviewPackageHeadArg = head
	m.reviewPackageFileArg = file
	return m.reviewPackageRet, m.reviewPackageErr
}

func (m *mockTaskRunner) ReviewNotes(
	branch, rev string,
	showPath, prune bool,
) (string, error) {
	m.reviewNotesCalled = true
	m.reviewNotesBranchArg = branch
	m.reviewNotesRevArg = rev
	m.reviewNotesPathArg = showPath
	m.reviewNotesPruneArg = prune
	return m.reviewNotesRet, m.reviewNotesErr
}

func (m *mockTaskRunner) ReviewNoteOpen(branch, rev, cite, note string) (string, error) {
	m.reviewNoteOpenCalled = true
	m.reviewNoteOpenBranchArg = branch
	m.reviewNoteOpenRevArg = rev
	m.reviewNoteOpenCiteArg = cite
	m.reviewNoteOpenNoteArg = note
	return m.reviewNoteOpenRet, m.reviewNoteOpenErr
}

func (m *mockTaskRunner) ReviewNoteSettle(
	branch, rev, id, resolution, cite, note string,
) (string, error) {
	m.reviewNoteSettleCalled = true
	m.reviewNoteSettleBranchArg = branch
	m.reviewNoteSettleRevArg = rev
	m.reviewNoteSettleIDArg = id
	m.reviewNoteSettleAsArg = resolution
	m.reviewNoteSettleAtArg = cite
	m.reviewNoteSettleNoteArg = note
	return m.reviewNoteSettleRet, m.reviewNoteSettleErr
}

func (m *mockTaskRunner) ReviewNoteRatify(branch, id string) (string, error) {
	m.reviewNoteRatifyCalled = true
	m.reviewNoteRatifyBranchArg = branch
	m.reviewNoteRatifyIDArg = id
	return m.reviewNoteRatifyRet, m.reviewNoteRatifyErr
}

func (m *mockTaskRunner) ReviewNoteReopen(branch, id string) (string, error) {
	m.reviewNoteReopenCalled = true
	m.reviewNoteReopenBranchArg = branch
	m.reviewNoteReopenIDArg = id
	return m.reviewNoteReopenRet, m.reviewNoteReopenErr
}

func (m *mockTaskRunner) ReviewRun(
	reviewer, note string,
	rng task.ReviewRange,
) (string, error) {
	m.reviewRunCalled = true
	m.reviewRunReviewerArg = reviewer
	m.reviewRunNoteArg = note
	m.reviewRunRangeArg = rng
	return m.reviewRunRet, m.reviewRunErr
}

func (m *mockTaskRunner) WorktreeStart(name, base string) (string, error) {
	m.worktreeStartCalled = true
	m.worktreeStartNameArg = name
	m.worktreeStartBaseArg = base
	return m.worktreeStartRet, m.worktreeStartErr
}

func (m *mockTaskRunner) WorktreeFinish(
	name string,
	merge, discard, check, force bool,
) (string, error) {
	m.worktreeFinishCalled = true
	m.worktreeFinishNameArg = name
	m.worktreeFinishMergeArg = merge
	m.worktreeFinishDiscardArg = discard
	m.worktreeFinishCheckArg = check
	m.worktreeFinishForceArg = force
	return m.worktreeFinishRet, m.worktreeFinishErr
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

func TestTask_ReviewScope(t *testing.T) {
	t.Run("calls ReviewScope and prints its output", func(t *testing.T) {
		mock := &mockTaskRunner{
			reviewScopeRet: "branch: feat/x -> main (default)  [ahead 1, behind 0]",
		}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewScopeBodiesFlag = false

		err := taskReviewScopeCmd.RunE(taskReviewScopeCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reviewScopeCalled {
			t.Error("expected ReviewScope to be called")
		}
		if mock.reviewScopeBodies {
			t.Error("expected bodies=false when --bodies not passed")
		}
	})

	t.Run("passes --bodies flag", func(t *testing.T) {
		mock := &mockTaskRunner{
			reviewScopeRet: "branch: feat/x -> main (default)  [ahead 1, behind 0]",
		}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewScopeBodiesFlag = true
		defer func() { taskReviewScopeBodiesFlag = false }()

		err := taskReviewScopeCmd.RunE(taskReviewScopeCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reviewScopeBodies {
			t.Error("expected bodies=true when --bodies passed")
		}
	})

	t.Run("passes --sizes flag", func(t *testing.T) {
		mock := &mockTaskRunner{
			reviewScopeRet: "branch: feat/x -> main (default)  [ahead 1, behind 0]",
		}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewScopeSizesFlag = true
		defer func() { taskReviewScopeSizesFlag = false }()

		err := taskReviewScopeCmd.RunE(taskReviewScopeCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reviewScopeSizes {
			t.Error("expected sizes=true when --sizes passed")
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{reviewScopeErr: fmt.Errorf("git failed")}
		restore := setupTaskMock(t, mock)
		defer restore()

		err := taskReviewScopeCmd.RunE(taskReviewScopeCmd, []string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_BranchDiff(t *testing.T) {
	t.Run("no --file passes empty string", func(t *testing.T) {
		mock := &mockTaskRunner{branchDiffRet: "diff --git a/x b/x"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskBranchDiffFileFlag = ""

		err := taskBranchDiffCmd.RunE(taskBranchDiffCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.branchDiffCalled {
			t.Error("expected BranchDiff to be called")
		}
		if mock.branchDiffArg != "" {
			t.Errorf("expected empty file arg, got %q", mock.branchDiffArg)
		}
	})

	t.Run("passes --file flag", func(t *testing.T) {
		mock := &mockTaskRunner{branchDiffRet: "diff --git a/go.sum b/go.sum"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskBranchDiffFileFlag = "go.sum"
		defer func() { taskBranchDiffFileFlag = "" }()

		err := taskBranchDiffCmd.RunE(taskBranchDiffCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.branchDiffArg != "go.sum" {
			t.Errorf("expected file arg 'go.sum', got %q", mock.branchDiffArg)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{branchDiffErr: fmt.Errorf("diff failed")}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskBranchDiffFileFlag = ""

		err := taskBranchDiffCmd.RunE(taskBranchDiffCmd, []string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_ReviewPackage(t *testing.T) {
	t.Run("passes positional base/head and empty file arg", func(t *testing.T) {
		mock := &mockTaskRunner{reviewPackageRet: "range: main..feat"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewPackageFileFlag = ""

		err := taskReviewPackageCmd.RunE(taskReviewPackageCmd, []string{"main", "feat"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reviewPackageCalled {
			t.Error("expected ReviewPackage to be called")
		}
		if mock.reviewPackageBaseArg != "main" || mock.reviewPackageHeadArg != "feat" {
			t.Errorf(
				"expected base=main head=feat, got base=%q head=%q",
				mock.reviewPackageBaseArg, mock.reviewPackageHeadArg,
			)
		}
		if mock.reviewPackageFileArg != "" {
			t.Errorf("expected empty file arg, got %q", mock.reviewPackageFileArg)
		}
	})

	t.Run("passes --file flag", func(t *testing.T) {
		mock := &mockTaskRunner{reviewPackageRet: "diff --git a/go.sum b/go.sum"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewPackageFileFlag = "go.sum"
		defer func() { taskReviewPackageFileFlag = "" }()

		err := taskReviewPackageCmd.RunE(taskReviewPackageCmd, []string{"main", "feat"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.reviewPackageFileArg != "go.sum" {
			t.Errorf("expected file arg 'go.sum', got %q", mock.reviewPackageFileArg)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{reviewPackageErr: fmt.Errorf("unrecognized ref")}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewPackageFileFlag = ""

		err := taskReviewPackageCmd.RunE(taskReviewPackageCmd, []string{"bogus", "feat"})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("requires exactly two positional args", func(t *testing.T) {
		if err := taskReviewPackageCmd.Args(taskReviewPackageCmd, []string{"main"}); err == nil {
			t.Fatal("expected error for one arg")
		}
		if err := taskReviewPackageCmd.Args(
			taskReviewPackageCmd,
			[]string{"main", "feat", "extra"},
		); err == nil {
			t.Fatal("expected error for three args")
		}
		if err := taskReviewPackageCmd.Args(
			taskReviewPackageCmd,
			[]string{"main", "feat"},
		); err != nil {
			t.Fatalf("expected no error for two args, got: %v", err)
		}
	})
}

// resetReviewNoteFlags clears the package-level flag vars both review-note
// tests and any prior test may have set, so a leaked value cannot make a later
// case pass for the wrong reason.
func resetReviewNoteFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		taskReviewNotesBranchFlag, taskReviewNotesPathFlag, taskReviewNotesPruneFlag = "", false, false
		taskReviewNotesRevFlag, taskReviewNoteRevFlag = "", ""
		taskReviewNoteBranchFlag, taskReviewNoteOpenFlag, taskReviewNoteSettleFlag = "", false, false
		taskReviewNoteRatifyFlag, taskReviewNoteReopenFlag = false, false
		taskReviewNoteIDFlag, taskReviewNoteAsFlag, taskReviewNoteAtFlag, taskReviewNoteNoteFlag = "", "", "", ""
	}
	reset()
	t.Cleanup(reset)
}

func TestTask_ReviewNotes(t *testing.T) {
	t.Run("passes branch, path and prune flags through", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{reviewNotesRet: "branch: feat"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNotesBranchFlag = "fix/retry"
		taskReviewNotesPathFlag = true

		if err := taskReviewNotesCmd.RunE(taskReviewNotesCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reviewNotesCalled {
			t.Fatal("expected ReviewNotes to be called")
		}
		if mock.reviewNotesBranchArg != "fix/retry" {
			t.Errorf("expected branch 'fix/retry', got %q", mock.reviewNotesBranchArg)
		}
		if !mock.reviewNotesPathArg || mock.reviewNotesPruneArg {
			t.Errorf("expected path=true prune=false, got path=%v prune=%v",
				mock.reviewNotesPathArg, mock.reviewNotesPruneArg)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{reviewNotesErr: fmt.Errorf("not a git repository")}
		restore := setupTaskMock(t, mock)
		defer restore()

		if err := taskReviewNotesCmd.RunE(taskReviewNotesCmd, []string{}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("--path and --prune are mutually exclusive", func(t *testing.T) {
		if err := taskReviewNotesCmd.ValidateFlagGroups(); err != nil {
			t.Fatalf("unexpected group error with no flags set: %v", err)
		}
		// The declaration is what enforces it at parse time; assert it exists
		// so removing MarkFlagsMutuallyExclusive fails here.
		ann := taskReviewNotesCmd.Flags().Lookup("path").Annotations
		if _, ok := ann["cobra_annotation_mutually_exclusive"]; !ok {
			t.Error("--path must be declared mutually exclusive with --prune")
		}
	})

	// --rev is what makes freshness mean anything for code that is not checked
	// out (ADR-0023 §4): it must reach the task layer, and it must not be
	// silently accepted beside the two flags that compute no freshness at all.
	t.Run("passes --rev through", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{reviewNotesRet: "branch: pr/acme/api/213"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNotesBranchFlag = "pr/acme/api/213"
		taskReviewNotesRevFlag = "9f2c1ab"

		if err := taskReviewNotesCmd.RunE(taskReviewNotesCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.reviewNotesRevArg != "9f2c1ab" {
			t.Errorf("expected rev '9f2c1ab', got %q", mock.reviewNotesRevArg)
		}
	})

	// Asserted by actually setting the flags and running cobra's validation,
	// not by looking for an annotation key: an annotation-only check passes
	// even when one of the two groups has been deleted, since the key is
	// present as soon as ANY group names --rev.
	t.Run("--rev excludes --path and --prune", func(t *testing.T) {
		flags := taskReviewNotesCmd.Flags()
		setFlag := func(t *testing.T, name, value string) {
			t.Helper()
			if err := flags.Set(name, value); err != nil {
				t.Fatalf("setting --%s: %v", name, err)
			}
			t.Cleanup(func() { flags.Lookup(name).Changed = false })
		}

		for _, other := range []string{"path", "prune"} {
			t.Run("with --"+other, func(t *testing.T) {
				resetReviewNoteFlags(t)
				setFlag(t, "rev", "9f2c1ab")
				setFlag(t, other, "true")
				if err := taskReviewNotesCmd.ValidateFlagGroups(); err == nil {
					t.Errorf("--rev and --%s must be refused together, not silently ignored", other)
				}
			})
		}

		// The guard is only meaningful if --rev alone still validates.
		resetReviewNoteFlags(t)
		setFlag(t, "rev", "9f2c1ab")
		if err := taskReviewNotesCmd.ValidateFlagGroups(); err != nil {
			t.Errorf("--rev on its own must be accepted: %v", err)
		}
	})
}

func TestTask_ReviewNote(t *testing.T) {
	t.Run("--open passes cite and note", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{reviewNoteOpenRet: "Noted n4"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteOpenFlag = true
		taskReviewNoteAtFlag = "store.go:12"
		taskReviewNoteNoteFlag = "write is not atomic"

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reviewNoteOpenCalled {
			t.Fatal("expected ReviewNoteOpen to be called")
		}
		if mock.reviewNoteOpenCiteArg != "store.go:12" ||
			mock.reviewNoteOpenNoteArg != "write is not atomic" {
			t.Errorf("args not passed through: %+v", mock)
		}
		if mock.reviewNoteSettleCalled {
			t.Error("--open must not settle")
		}
	})

	// --rev is what makes a review of code that is not checked out possible
	// (ADR-0023 §4); it has to reach the task layer on both entry-creating
	// paths or the entry is stamped against the wrong source.
	t.Run("--rev reaches --open and --settle", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{reviewNoteOpenRet: "Noted n1"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteOpenFlag = true
		taskReviewNoteRevFlag = "9f2c1ab"
		taskReviewNoteAtFlag = "store.go:12"
		taskReviewNoteNoteFlag = "write is not atomic"

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.reviewNoteOpenRevArg != "9f2c1ab" {
			t.Errorf("expected rev '9f2c1ab' on --open, got %q", mock.reviewNoteOpenRevArg)
		}

		resetReviewNoteFlags(t)
		taskReviewNoteSettleFlag = true
		taskReviewNoteRevFlag = "9f2c1ab"
		taskReviewNoteIDFlag = "n1"
		taskReviewNoteAsFlag = "fixed"
		taskReviewNoteNoteFlag = "atomic rename added"

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.reviewNoteSettleRevArg != "9f2c1ab" {
			t.Errorf("expected rev '9f2c1ab' on --settle, got %q", mock.reviewNoteSettleRevArg)
		}
	})

	// Ratify and reopen never restamp, so a revision has nothing to act on
	// there. Naming the mistake beats accepting the flag and ignoring it.
	t.Run("--rev with --ratify or --reopen errors", func(t *testing.T) {
		for _, mode := range []string{"ratify", "reopen"} {
			resetReviewNoteFlags(t)
			mock := &mockTaskRunner{}
			restore := setupTaskMock(t, mock)
			taskReviewNoteRevFlag = "9f2c1ab"
			taskReviewNoteIDFlag = "n7"
			taskReviewNoteRatifyFlag = mode == "ratify"
			taskReviewNoteReopenFlag = mode == "reopen"

			err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{})
			if err == nil {
				t.Errorf("--rev with --%s should error", mode)
			}
			if mock.reviewNoteRatifyCalled || mock.reviewNoteReopenCalled {
				t.Errorf("--%s must not run with --rev", mode)
			}
			restore()
		}
	})

	t.Run("--settle with an id passes the id and resolution", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{reviewNoteSettleRet: "Settled n4 (fixed)"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteSettleFlag = true
		taskReviewNoteIDFlag = "n4"
		taskReviewNoteAsFlag = "fixed"
		taskReviewNoteNoteFlag = "atomic rename added"

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.reviewNoteSettleIDArg != "n4" || mock.reviewNoteSettleAsArg != "fixed" {
			t.Errorf("expected id=n4 as=fixed, got id=%q as=%q",
				mock.reviewNoteSettleIDArg, mock.reviewNoteSettleAsArg)
		}
		if mock.reviewNoteOpenCalled {
			t.Error("--settle must not open")
		}
	})

	t.Run("--settle without an id is the direct form", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{reviewNoteSettleRet: "Settled n1 (answered)"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteSettleFlag = true
		taskReviewNoteAsFlag = "answered"
		taskReviewNoteNoteFlag = "yes, ctx is threaded through"

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.reviewNoteSettleIDArg != "" {
			t.Errorf("expected no id, got %q", mock.reviewNoteSettleIDArg)
		}
	})

	// --id without --settle is a caller mistake worth naming: it would
	// otherwise be silently ignored on the --open path.
	t.Run("--id without --settle errors", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteOpenFlag = true
		taskReviewNoteIDFlag = "n4"
		taskReviewNoteNoteFlag = "x"

		err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{})
		if err == nil {
			t.Fatal("expected an error for --id without --settle")
		}
		if mock.reviewNoteOpenCalled || mock.reviewNoteSettleCalled {
			t.Error("nothing should have been written")
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{
			reviewNoteSettleErr: fmt.Errorf("no entry n9 in the journal"),
		}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteSettleFlag = true
		taskReviewNoteIDFlag = "n9"
		taskReviewNoteAsFlag = "answered"
		taskReviewNoteNoteFlag = "x"

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err == nil {
			t.Fatal("expected error")
		}
	})

	// --open/--settle/--ratify/--reopen must be exclusive and one required,
	// declared on the command so cobra enforces it during parsing (RunE never
	// sees the bad combination). --note is deliberately NOT cobra-required:
	// --ratify and --reopen take no note at all, so requiring it globally
	// would make those modes unusable. --open and --settle enforce a
	// non-empty note themselves (see reviewnotes_test.go).
	t.Run("open/settle/ratify/reopen are exclusive and one is required", func(t *testing.T) {
		openAnn := taskReviewNoteCmd.Flags().Lookup("open").Annotations
		if _, ok := openAnn["cobra_annotation_mutually_exclusive"]; !ok {
			t.Error("--open must be declared mutually exclusive with --settle/--ratify/--reopen")
		}
		if _, ok := openAnn["cobra_annotation_one_required"]; !ok {
			t.Error("one of --open/--settle/--ratify/--reopen must be declared required")
		}
	})

	t.Run("--ratify passes branch and id", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{reviewNoteRatifyRet: "Ratified n7"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteRatifyFlag = true
		taskReviewNoteIDFlag = "n7"
		taskReviewNoteBranchFlag = "fix/retry"

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reviewNoteRatifyCalled {
			t.Fatal("expected ReviewNoteRatify to be called")
		}
		if mock.reviewNoteRatifyBranchArg != "fix/retry" || mock.reviewNoteRatifyIDArg != "n7" {
			t.Errorf("args not passed through: %+v", mock)
		}
		if mock.reviewNoteOpenCalled || mock.reviewNoteSettleCalled || mock.reviewNoteReopenCalled {
			t.Error("--ratify must not call any other mode")
		}
	})

	t.Run("--reopen passes branch and id", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{reviewNoteReopenRet: "Reopened n7"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteReopenFlag = true
		taskReviewNoteIDFlag = "n7"

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reviewNoteReopenCalled {
			t.Fatal("expected ReviewNoteReopen to be called")
		}
		if mock.reviewNoteReopenIDArg != "n7" {
			t.Errorf("expected id n7, got %q", mock.reviewNoteReopenIDArg)
		}
		if mock.reviewNoteOpenCalled || mock.reviewNoteSettleCalled || mock.reviewNoteRatifyCalled {
			t.Error("--reopen must not call any other mode")
		}
	})

	// Both new modes require --id — unlike --open/--settle, there is no
	// direct form.
	t.Run("--ratify without --id errors before calling the manager", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteRatifyFlag = true

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err == nil {
			t.Fatal("expected an error for --ratify without --id")
		}
		if mock.reviewNoteRatifyCalled {
			t.Error("the manager should not have been called")
		}
	})

	t.Run("--reopen without --id errors before calling the manager", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteReopenFlag = true

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err == nil {
			t.Fatal("expected an error for --reopen without --id")
		}
		if mock.reviewNoteReopenCalled {
			t.Error("the manager should not have been called")
		}
	})

	t.Run("--ratify propagates the manager's error", func(t *testing.T) {
		resetReviewNoteFlags(t)
		mock := &mockTaskRunner{
			reviewNoteRatifyErr: fmt.Errorf("entry n7 is already an ordinary rejection"),
		}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskReviewNoteRatifyFlag = true
		taskReviewNoteIDFlag = "n7"

		if err := taskReviewNoteCmd.RunE(taskReviewNoteCmd, []string{}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_ReviewRun(t *testing.T) {
	// setReviewRunReviewer sets the flag var and restores it, so a leaked
	// value cannot make a later case pass for the wrong reason.
	setReviewRunReviewer := func(t *testing.T, value string) {
		t.Helper()
		orig := taskReviewRunReviewerFlag
		taskReviewRunReviewerFlag = value
		t.Cleanup(func() { taskReviewRunReviewerFlag = orig })
	}

	// setReviewRunNote does the same for --note.
	setReviewRunNote := func(t *testing.T, value string) {
		t.Helper()
		orig := taskReviewRunNoteFlag
		taskReviewRunNoteFlag = value
		t.Cleanup(func() { taskReviewRunNoteFlag = orig })
	}

	t.Run("passes the reviewer flag through", func(t *testing.T) {
		mock := &mockTaskRunner{reviewRunRet: "openai/gpt-5.2 → APPROVE"}
		restore := setupTaskMock(t, mock)
		defer restore()
		setReviewRunReviewer(t, "document")

		if err := taskReviewRunCmd.RunE(taskReviewRunCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.reviewRunCalled {
			t.Fatal("expected ReviewRun to be called")
		}
		if mock.reviewRunReviewerArg != "document" {
			t.Errorf("expected reviewer 'document', got %q", mock.reviewRunReviewerArg)
		}
	})

	t.Run("passes the note flag through", func(t *testing.T) {
		const note = "focus on docs/spec.md"
		mock := &mockTaskRunner{reviewRunRet: "openai/gpt-5.2 → APPROVE"}
		restore := setupTaskMock(t, mock)
		defer restore()
		setReviewRunNote(t, note)

		if err := taskReviewRunCmd.RunE(taskReviewRunCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.reviewRunNoteArg != note {
			t.Errorf("expected the note passed through, got %q", mock.reviewRunNoteArg)
		}
	})

	// No --note means no note — an empty string, which review-run reads as
	// "the flag was not used" rather than as a blank note to refuse.
	t.Run("omitting --note passes an empty note", func(t *testing.T) {
		mock := &mockTaskRunner{reviewRunRet: "openai/gpt-5.2 → APPROVE"}
		restore := setupTaskMock(t, mock)
		defer restore()
		setReviewRunNote(t, "")

		if err := taskReviewRunCmd.RunE(taskReviewRunCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.reviewRunNoteArg != "" {
			t.Errorf("expected an empty note, got %q", mock.reviewRunNoteArg)
		}
		if flag := taskReviewRunCmd.Flags().Lookup("note"); flag == nil {
			t.Error("expected a --note flag")
		} else if flag.DefValue != "" {
			t.Errorf("expected --note to default to empty, got %q", flag.DefValue)
		}
	})

	// The default is declared on the flag, not restated in the command body,
	// so `dg task review-run` with no flags runs the code reviewer.
	t.Run("--reviewer defaults to code", func(t *testing.T) {
		got := taskReviewRunCmd.Flags().Lookup("reviewer")
		if got == nil {
			t.Fatal("expected a --reviewer flag")
		}
		if got.DefValue != "code" {
			t.Errorf("expected --reviewer to default to 'code', got %q", got.DefValue)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{
			reviewRunErr: fmt.Errorf("review-run: HEAD is detached"),
		}
		restore := setupTaskMock(t, mock)
		defer restore()
		setReviewRunReviewer(t, "code")

		if err := taskReviewRunCmd.RunE(taskReviewRunCmd, []string{}); err == nil {
			t.Fatal("expected error")
		}
	})

	// setReviewRunRange sets the four explicit-range flags and restores them, so
	// a leaked value cannot turn a later case's branch review into a range one.
	setReviewRunRange := func(t *testing.T, rng task.ReviewRange) {
		t.Helper()
		base, head := taskReviewRunBaseFlag, taskReviewRunHeadFlag
		journal, dir := taskReviewRunJournalFlag, taskReviewRunReportDirFlag
		taskReviewRunBaseFlag = rng.Base
		taskReviewRunHeadFlag = rng.Head
		taskReviewRunJournalFlag = rng.Journal
		taskReviewRunReportDirFlag = rng.ReportDir
		t.Cleanup(func() {
			taskReviewRunBaseFlag, taskReviewRunHeadFlag = base, head
			taskReviewRunJournalFlag, taskReviewRunReportDirFlag = journal, dir
		})
	}

	t.Run("passes the explicit-range flags through", func(t *testing.T) {
		want := task.ReviewRange{
			Base:      "4a1c2ef",
			Head:      "9f2c1ab",
			Journal:   "pr/acme/web/213",
			ReportDir: "/tmp/reports",
		}
		mock := &mockTaskRunner{reviewRunRet: "openai/gpt-5.2 → APPROVE  report: /tmp/reports/x.md"}
		restore := setupTaskMock(t, mock)
		defer restore()
		setReviewRunRange(t, want)

		if err := taskReviewRunCmd.RunE(taskReviewRunCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.reviewRunRangeArg != want {
			t.Errorf("expected the range %+v passed through, got %+v", want, mock.reviewRunRangeArg)
		}
	})

	// Omitting them all is branch mode, and the zero value is what says so.
	t.Run("omitting the range flags passes the zero range", func(t *testing.T) {
		mock := &mockTaskRunner{reviewRunRet: "openai/gpt-5.2 → APPROVE"}
		restore := setupTaskMock(t, mock)
		defer restore()
		setReviewRunRange(t, task.ReviewRange{})

		if err := taskReviewRunCmd.RunE(taskReviewRunCmd, []string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if (mock.reviewRunRangeArg != task.ReviewRange{}) {
			t.Errorf("expected an empty range, got %+v", mock.reviewRunRangeArg)
		}
	})

	// The four flags exist and are declared as one group, so cobra refuses a
	// partial one at parse time — before ReviewRun's own refusal, which is what
	// protects every other caller.
	t.Run("the four range flags are declared required together", func(t *testing.T) {
		for _, name := range []string{"base", "head", "journal", "report-dir"} {
			flag := taskReviewRunCmd.Flags().Lookup(name)
			if flag == nil {
				t.Fatalf("expected a --%s flag", name)
			}
			if flag.DefValue != "" {
				t.Errorf("expected --%s to default to empty, got %q", name, flag.DefValue)
			}
		}

		if err := taskReviewRunCmd.Flags().Set("base", "4a1c2ef"); err != nil {
			t.Fatalf("setting --base: %v", err)
		}
		t.Cleanup(func() {
			if err := taskReviewRunCmd.Flags().Set("base", ""); err != nil {
				t.Logf("restoring --base: %v", err)
			}
			taskReviewRunBaseFlag = ""
		})

		err := taskReviewRunCmd.ValidateFlagGroups()
		if err == nil {
			t.Fatal("expected --base alone to be refused by the flag group")
		}
		for _, want := range []string{"head", "journal", "report-dir"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("expected %q named in the group error, got: %v", want, err)
			}
		}
	})
}

func TestTask_WorktreeStart(t *testing.T) {
	t.Run("passes name and default (empty) base", func(t *testing.T) {
		mock := &mockTaskRunner{
			worktreeStartRet: "Created worktree /path (branch x, base origin/main)",
		}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskWorktreeStartBaseFlag = ""

		err := taskWorktreeStartCmd.RunE(taskWorktreeStartCmd, []string{"add-retry"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.worktreeStartCalled {
			t.Error("expected WorktreeStart to be called")
		}
		if mock.worktreeStartNameArg != "add-retry" {
			t.Errorf("expected name arg 'add-retry', got %q", mock.worktreeStartNameArg)
		}
		if mock.worktreeStartBaseArg != "" {
			t.Errorf("expected empty base arg, got %q", mock.worktreeStartBaseArg)
		}
	})

	t.Run("passes --base flag", func(t *testing.T) {
		mock := &mockTaskRunner{
			worktreeStartRet: "Created worktree /path (branch x, base origin/release)",
		}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskWorktreeStartBaseFlag = "origin/release"
		defer func() { taskWorktreeStartBaseFlag = "" }()

		err := taskWorktreeStartCmd.RunE(taskWorktreeStartCmd, []string{"hotfix"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.worktreeStartBaseArg != "origin/release" {
			t.Errorf("expected base arg 'origin/release', got %q", mock.worktreeStartBaseArg)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{worktreeStartErr: fmt.Errorf("dirty tree")}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskWorktreeStartBaseFlag = ""

		err := taskWorktreeStartCmd.RunE(taskWorktreeStartCmd, []string{"add-retry"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTask_WorktreeFinish(t *testing.T) {
	t.Run("no name, --merge", func(t *testing.T) {
		mock := &mockTaskRunner{worktreeFinishRet: "Merged x into main; removed worktree /path"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskWorktreeFinishMergeFlag = true
		taskWorktreeFinishDiscardFlag = false
		taskWorktreeFinishForceFlag = false
		defer func() { taskWorktreeFinishMergeFlag = false }()

		err := taskWorktreeFinishCmd.RunE(taskWorktreeFinishCmd, []string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !mock.worktreeFinishCalled {
			t.Error("expected WorktreeFinish to be called")
		}
		if mock.worktreeFinishNameArg != "" {
			t.Errorf("expected empty name arg, got %q", mock.worktreeFinishNameArg)
		}
		if !mock.worktreeFinishMergeArg || mock.worktreeFinishDiscardArg {
			t.Errorf("expected merge=true discard=false, got merge=%v discard=%v",
				mock.worktreeFinishMergeArg, mock.worktreeFinishDiscardArg)
		}
	})

	t.Run("name + --discard --force", func(t *testing.T) {
		mock := &mockTaskRunner{worktreeFinishRet: "Discarded worktree /path (branch x deleted)"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskWorktreeFinishMergeFlag = false
		taskWorktreeFinishDiscardFlag = true
		taskWorktreeFinishForceFlag = true
		defer func() {
			taskWorktreeFinishDiscardFlag = false
			taskWorktreeFinishForceFlag = false
		}()

		err := taskWorktreeFinishCmd.RunE(taskWorktreeFinishCmd, []string{"stale-spike"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.worktreeFinishNameArg != "stale-spike" {
			t.Errorf("expected name arg 'stale-spike', got %q", mock.worktreeFinishNameArg)
		}
		if !mock.worktreeFinishDiscardArg || !mock.worktreeFinishForceArg {
			t.Errorf("expected discard=true force=true, got discard=%v force=%v",
				mock.worktreeFinishDiscardArg, mock.worktreeFinishForceArg)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := &mockTaskRunner{worktreeFinishErr: fmt.Errorf("ambiguous target")}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskWorktreeFinishMergeFlag = true
		defer func() { taskWorktreeFinishMergeFlag = false }()

		err := taskWorktreeFinishCmd.RunE(taskWorktreeFinishCmd, []string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("name + --check", func(t *testing.T) {
		mock := &mockTaskRunner{worktreeFinishRet: "worktree: /path\n...\nready: yes"}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskWorktreeFinishMergeFlag = false
		taskWorktreeFinishDiscardFlag = false
		taskWorktreeFinishCheckFlag = true
		defer func() { taskWorktreeFinishCheckFlag = false }()

		err := taskWorktreeFinishCmd.RunE(taskWorktreeFinishCmd, []string{"add-retry-logic"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.worktreeFinishNameArg != "add-retry-logic" {
			t.Errorf("expected name arg 'add-retry-logic', got %q", mock.worktreeFinishNameArg)
		}
		if !mock.worktreeFinishCheckArg || mock.worktreeFinishMergeArg ||
			mock.worktreeFinishDiscardArg {
			t.Errorf(
				"expected check=true merge=false discard=false, got check=%v merge=%v discard=%v",
				mock.worktreeFinishCheckArg,
				mock.worktreeFinishMergeArg,
				mock.worktreeFinishDiscardArg,
			)
		}
	})

	// The CheckNotReadyError -> stdout-then-error path: the full report must
	// reach stdout even though the command also returns a non-nil error
	// (emitPRResult's usual "err != nil means nothing to print" contract does
	// not apply to this one, deliberate exception — see WorktreeFinish's doc
	// comment in internal/tooling/task/worktree.go).
	t.Run("--check not ready prints the full report then returns an error", func(t *testing.T) {
		report := "worktree: /path\nbranch: add-retry\nready: no — refusing to merge a dirty worktree; commit or stash your changes first"
		mock := &mockTaskRunner{
			worktreeFinishErr: &task.CheckNotReadyError{Report: report},
		}
		restore := setupTaskMock(t, mock)
		defer restore()
		taskWorktreeFinishMergeFlag = false
		taskWorktreeFinishDiscardFlag = false
		taskWorktreeFinishCheckFlag = true
		defer func() { taskWorktreeFinishCheckFlag = false }()

		var out bytes.Buffer
		taskWorktreeFinishCmd.SetOut(&out)
		defer taskWorktreeFinishCmd.SetOut(nil)

		err := taskWorktreeFinishCmd.RunE(taskWorktreeFinishCmd, []string{})
		if err == nil {
			t.Fatal("expected a non-nil error so the process exits non-zero")
		}
		if !strings.Contains(out.String(), report) {
			t.Errorf("expected stdout to contain the full report, got %q", out.String())
		}
	})

	t.Run("--check is mutually exclusive with --merge and --discard", func(t *testing.T) {
		resetWorktreeFinishFlags(t)
		ann := taskWorktreeFinishCmd.Flags().Lookup("check").Annotations
		if _, ok := ann["cobra_annotation_mutually_exclusive"]; !ok {
			t.Error("--check must be declared mutually exclusive with --merge and --discard")
		}

		flags := taskWorktreeFinishCmd.Flags()
		setFlag := func(t *testing.T, name string) {
			t.Helper()
			if err := flags.Set(name, "true"); err != nil {
				t.Fatalf("setting --%s: %v", name, err)
			}
			t.Cleanup(func() { flags.Lookup(name).Changed = false })
		}

		for _, other := range []string{"merge", "discard"} {
			t.Run("with --"+other, func(t *testing.T) {
				resetWorktreeFinishFlags(t)
				setFlag(t, "check")
				setFlag(t, other)
				if err := taskWorktreeFinishCmd.ValidateFlagGroups(); err == nil {
					t.Errorf(
						"--check and --%s must be refused together, not silently accepted",
						other,
					)
				}
			})
		}

		resetWorktreeFinishFlags(t)
		setFlag(t, "check")
		if err := taskWorktreeFinishCmd.ValidateFlagGroups(); err != nil {
			t.Errorf("--check on its own must be accepted: %v", err)
		}
	})

	t.Run("none of --merge, --discard, --check is rejected", func(t *testing.T) {
		resetWorktreeFinishFlags(t)
		if err := taskWorktreeFinishCmd.ValidateFlagGroups(); err == nil {
			t.Error("expected an error when none of --merge, --discard, --check is set")
		}
	})
}

// resetWorktreeFinishFlags clears worktree-finish's flags (both the package
// vars RunE reads and pflag's own Changed bookkeeping, which
// ValidateFlagGroups consults) so each subtest starts from a clean slate.
func resetWorktreeFinishFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		taskWorktreeFinishMergeFlag = false
		taskWorktreeFinishDiscardFlag = false
		taskWorktreeFinishCheckFlag = false
		taskWorktreeFinishForceFlag = false
		for _, name := range []string{"merge", "discard", "check", "force"} {
			taskWorktreeFinishCmd.Flags().Lookup(name).Changed = false
		}
	}
	reset()
	t.Cleanup(reset)
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

// mockPRRunner records calls to the PR task methods.
type mockPRRunner struct {
	calls   []string
	lastArg map[string]string
	ret     string
	err     error
}

func newMockPRRunner() *mockPRRunner {
	return &mockPRRunner{lastArg: map[string]string{}, ret: "ok"}
}

func (m *mockPRRunner) record(name string, kv ...string) (string, error) {
	m.calls = append(m.calls, name)
	for i := 0; i+1 < len(kv); i += 2 {
		m.lastArg[kv[i]] = kv[i+1]
	}
	return m.ret, m.err
}

func (m *mockPRRunner) ReviewThreads(pr, state string) (string, error) {
	return m.record("ReviewThreads", "pr", pr, "state", state)
}

func (m *mockPRRunner) ReviewThreadsJSON(pr, state string) (string, error) {
	return m.record("ReviewThreadsJSON", "pr", pr, "state", state)
}

func (m *mockPRRunner) ResolveThread(id string) (string, error) {
	return m.record("ResolveThread", "id", id)
}

func (m *mockPRRunner) UnresolveThread(id string) (string, error) {
	return m.record("UnresolveThread", "id", id)
}

func (m *mockPRRunner) ReplyThread(id, body string) (string, error) {
	return m.record("ReplyThread", "id", id, "body", body)
}

func (m *mockPRRunner) SubmitReview(pr, verdict, body, comments, commit string) (string, error) {
	return m.record(
		"SubmitReview",
		"pr", pr,
		"verdict", verdict,
		"body", body,
		"comments", comments,
		"commit", commit,
	)
}

func (m *mockPRRunner) CreatePR(title, body, base string) (string, error) {
	return m.record("CreatePR", "title", title, "body", body, "base", base)
}

func (m *mockPRRunner) UpdatePRDescription(pr, body string) (string, error) {
	return m.record("UpdatePRDescription", "pr", pr, "body", body)
}

func (m *mockPRRunner) ApprovePR(pr, body, commit string) (string, error) {
	return m.record("ApprovePR", "pr", pr, "body", body, "commit", commit)
}

func (m *mockPRRunner) RequestChangesPR(pr, body string) (string, error) {
	return m.record("RequestChangesPR", "pr", pr, "body", body)
}

func (m *mockPRRunner) RequestReviewPR(pr string, reviewers []string) (string, error) {
	return m.record("RequestReviewPR", "pr", pr, "reviewers", strings.Join(reviewers, ","))
}

func (m *mockPRRunner) CommentPR(pr, body string) (string, error) {
	return m.record("CommentPR", "pr", pr, "body", body)
}

func (m *mockPRRunner) MergePR(pr, method string) (string, error) {
	return m.record("MergePR", "pr", pr, "method", method)
}
func (m *mockPRRunner) PRView(pr string) (string, error)   { return m.record("PRView", "pr", pr) }
func (m *mockPRRunner) PRChecks(pr string) (string, error) { return m.record("PRChecks", "pr", pr) }
func (m *mockPRRunner) PRReviewTarget(pr string) (string, error) {
	return m.record("PRReviewTarget", "pr", pr)
}

func (m *mockPRRunner) PRReviewState(pr string) (string, error) {
	return m.record("PRReviewState", "pr", pr)
}

func (m *mockPRRunner) PRState(pr string) (string, error) {
	return m.record("PRState", "pr", pr)
}
func (m *mockPRRunner) CurrentPR() (string, error)   { return m.record("CurrentPR") }
func (m *mockPRRunner) CurrentRepo() (string, error) { return m.record("CurrentRepo") }

func setupPRMock(t *testing.T, mock prRunner) func() {
	t.Helper()
	orig := newPRTasks
	newPRTasks = func() prRunner { return mock }
	return func() { newPRTasks = orig }
}

// mockIssueRunner records calls to the issue task methods.
type mockIssueRunner struct {
	calls   []string
	lastArg map[string]string
	ret     string
	err     error
}

func newMockIssueRunner() *mockIssueRunner {
	return &mockIssueRunner{lastArg: map[string]string{}, ret: "ok"}
}

func (m *mockIssueRunner) record(name string, kv ...string) (string, error) {
	m.calls = append(m.calls, name)
	for i := 0; i+1 < len(kv); i += 2 {
		m.lastArg[kv[i]] = kv[i+1]
	}
	return m.ret, m.err
}

func (m *mockIssueRunner) IssueScope(n string) (string, error) {
	return m.record("IssueScope", "n", n)
}

func setupIssueMock(t *testing.T, mock issueRunner) func() {
	t.Helper()
	orig := newIssueTasks
	newIssueTasks = func() issueRunner { return mock }
	return func() { newIssueTasks = orig }
}

func TestTask_IssueScope(t *testing.T) {
	t.Run("passes the issue number", func(t *testing.T) {
		mock := newMockIssueRunner()
		defer setupIssueMock(t, mock)()

		if err := taskIssueScopeCmd.RunE(taskIssueScopeCmd, []string{"3"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.calls) != 1 || mock.calls[0] != "IssueScope" {
			t.Fatalf("expected IssueScope, got %v", mock.calls)
		}
		if mock.lastArg["n"] != "3" {
			t.Errorf("expected issue number %q, got %q", "3", mock.lastArg["n"])
		}
	})

	t.Run("requires exactly one argument", func(t *testing.T) {
		if err := taskIssueScopeCmd.Args(taskIssueScopeCmd, []string{}); err == nil {
			t.Error("expected an error with no arguments")
		}
		if err := taskIssueScopeCmd.Args(taskIssueScopeCmd, []string{"1", "2"}); err == nil {
			t.Error("expected an error with two arguments")
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := newMockIssueRunner()
		mock.err = fmt.Errorf("gh failed")
		defer setupIssueMock(t, mock)()

		if err := taskIssueScopeCmd.RunE(taskIssueScopeCmd, []string{"3"}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPRTask_Dispatch(t *testing.T) {
	t.Run("review-threads passes flags", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()
		prFlag, prStateFlag = "7", "all"
		defer func() { prFlag, prStateFlag = "", "unresolved" }()

		if err := taskReviewThreadsCmd.RunE(taskReviewThreadsCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.calls) != 1 || mock.calls[0] != "ReviewThreads" {
			t.Fatalf("expected ReviewThreads call, got %v", mock.calls)
		}
		if mock.lastArg["pr"] != "7" || mock.lastArg["state"] != "all" {
			t.Fatalf("unexpected args: %v", mock.lastArg)
		}
	})

	t.Run(
		"review-threads --json dispatches to ReviewThreadsJSON, not ReviewThreads",
		func(t *testing.T) {
			mock := newMockPRRunner()
			defer setupPRMock(t, mock)()
			prFlag, prStateFlag, prJSONFlag = "7", "all", true
			defer func() { prFlag, prStateFlag, prJSONFlag = "", "unresolved", false }()

			if err := taskReviewThreadsCmd.RunE(taskReviewThreadsCmd, nil); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(mock.calls) != 1 || mock.calls[0] != "ReviewThreadsJSON" {
				t.Fatalf("expected ReviewThreadsJSON call, got %v", mock.calls)
			}
			if mock.lastArg["pr"] != "7" || mock.lastArg["state"] != "all" {
				t.Fatalf("unexpected args: %v", mock.lastArg)
			}
		},
	)

	t.Run("review-threads without --json still dispatches to ReviewThreads", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()
		prJSONFlag = false

		if err := taskReviewThreadsCmd.RunE(taskReviewThreadsCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mock.calls) != 1 || mock.calls[0] != "ReviewThreads" {
			t.Fatalf("expected ReviewThreads call, got %v", mock.calls)
		}
	})

	t.Run("resolve-thread passes id", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		if err := taskResolveThreadCmd.RunE(taskResolveThreadCmd, []string{"PRRT_x"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls[0] != "ResolveThread" || mock.lastArg["id"] != "PRRT_x" {
			t.Fatalf("unexpected dispatch: %v / %v", mock.calls, mock.lastArg)
		}
	})

	t.Run("reply-thread passes id and body", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		if err := taskReplyThreadCmd.RunE(
			taskReplyThreadCmd,
			[]string{"PRRT_x", "fixed"},
		); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.lastArg["id"] != "PRRT_x" || mock.lastArg["body"] != "fixed" {
			t.Fatalf("unexpected args: %v", mock.lastArg)
		}
	})

	t.Run("create-pr passes title/body/base", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()
		prTitleFlag, prBodyFlag, prBaseFlag = "T", "B", "main"
		defer func() { prTitleFlag, prBodyFlag, prBaseFlag = "", "", "" }()

		if err := taskCreatePRCmd.RunE(taskCreatePRCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.lastArg["title"] != "T" || mock.lastArg["body"] != "B" ||
			mock.lastArg["base"] != "main" {
			t.Fatalf("unexpected args: %v", mock.lastArg)
		}
	})

	t.Run("merge-pr passes method", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()
		prFlag, prMethodFlag = "9", "rebase"
		defer func() { prFlag, prMethodFlag = "", "squash" }()

		if err := taskMergePRCmd.RunE(taskMergePRCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.lastArg["pr"] != "9" || mock.lastArg["method"] != "rebase" {
			t.Fatalf("unexpected args: %v", mock.lastArg)
		}
	})

	t.Run("request-review passes pr and reviewers", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()
		prFlag = "7"
		defer func() { prFlag = "" }()

		if err := taskRequestReviewCmd.RunE(
			taskRequestReviewCmd,
			[]string{"octocat", "hubot"},
		); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls[0] != "RequestReviewPR" {
			t.Fatalf("expected RequestReviewPR, got %v", mock.calls)
		}
		if mock.lastArg["pr"] != "7" || mock.lastArg["reviewers"] != "octocat,hubot" {
			t.Fatalf("unexpected args: %v", mock.lastArg)
		}
	})

	t.Run("pr-view dispatches", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()
		prFlag = ""

		if err := taskPRViewCmd.RunE(taskPRViewCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls[0] != "PRView" {
			t.Fatalf("expected PRView, got %v", mock.calls)
		}
	})

	t.Run("pr-state dispatches with the pr flag", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()
		prFlag = "42"
		defer func() { prFlag = "" }()

		if err := taskPRStateCmd.RunE(taskPRStateCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls[0] != "PRState" {
			t.Fatalf("expected PRState, got %v", mock.calls)
		}
		if mock.lastArg["pr"] != "42" {
			t.Fatalf("expected pr flag %q, got %q", "42", mock.lastArg["pr"])
		}
	})

	t.Run("pr-review-target passes the pr flag", func(t *testing.T) {
		mock := newMockPRRunner()
		mock.ret = "base: 9f2c\nhead: 2f38\njournal: pr/octocat/hello/42\nfiles:\n- a.go"
		defer setupPRMock(t, mock)()
		prFlag = "42"
		defer func() { prFlag = "" }()

		out := &bytes.Buffer{}
		taskPRReviewTargetCmd.SetOut(out)
		defer taskPRReviewTargetCmd.SetOut(nil)

		if err := taskPRReviewTargetCmd.RunE(taskPRReviewTargetCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls[0] != "PRReviewTarget" || mock.lastArg["pr"] != "42" {
			t.Fatalf("unexpected dispatch: %v / %v", mock.calls, mock.lastArg)
		}
		// The target is printed verbatim: every later step parses this block,
		// so the command may not reformat or decorate it.
		if got := out.String(); got != mock.ret+"\n" {
			t.Fatalf("target not printed verbatim:\ngot:  %q\nwant: %q", got, mock.ret+"\n")
		}
	})

	t.Run("pr-review-state passes the pr flag", func(t *testing.T) {
		mock := newMockPRRunner()
		mock.ret = "pr: open\nrequested: yes\nmy-review: none"
		defer setupPRMock(t, mock)()
		prFlag = "42"
		defer func() { prFlag = "" }()

		out := &bytes.Buffer{}
		taskPRReviewStateCmd.SetOut(out)
		defer taskPRReviewStateCmd.SetOut(nil)

		if err := taskPRReviewStateCmd.RunE(taskPRReviewStateCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls[0] != "PRReviewState" || mock.lastArg["pr"] != "42" {
			t.Fatalf("unexpected dispatch: %v / %v", mock.calls, mock.lastArg)
		}
		// The three lines are printed verbatim: a tick matches all of them to
		// select exactly one row, so the command may not reformat or decorate
		// them.
		if got := out.String(); got != mock.ret+"\n" {
			t.Fatalf("state not printed verbatim:\ngot:  %q\nwant: %q", got, mock.ret+"\n")
		}
	})

	t.Run("current-pr dispatches", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		if err := taskCurrentPRCmd.RunE(taskCurrentPRCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls[0] != "CurrentPR" {
			t.Fatalf("expected CurrentPR, got %v", mock.calls)
		}
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := newMockPRRunner()
		mock.err = fmt.Errorf("boom")
		defer setupPRMock(t, mock)()

		if err := taskPRChecksCmd.RunE(taskPRChecksCmd, nil); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPRTask_BodyFile(t *testing.T) {
	t.Run("create-pr reads markdown body from file verbatim", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		md := "## Summary\n\n- Adds `dg task`\n- **Bold** and a list\n\n```go\nfmt.Println(\"hi\")\n```\n"
		dir := t.TempDir()
		path := dir + "/body.md"
		if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
			t.Fatalf("write temp: %v", err)
		}

		prTitleFlag, prBodyFlag, prBodyFileFlag, prBaseFlag = "T", "", path, ""
		defer func() { prTitleFlag, prBodyFlag, prBodyFileFlag, prBaseFlag = "", "", "", "" }()

		if err := taskCreatePRCmd.RunE(taskCreatePRCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.lastArg["body"] != md {
			t.Fatalf(
				"markdown body not passed verbatim.\nwant: %q\ngot:  %q",
				md,
				mock.lastArg["body"],
			)
		}
	})

	t.Run("both --body and --body-file is an error", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		dir := t.TempDir()
		path := dir + "/b.md"
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write temp: %v", err)
		}
		prTitleFlag, prBodyFlag, prBodyFileFlag = "T", "inline", path
		defer func() { prTitleFlag, prBodyFlag, prBodyFileFlag = "", "", "" }()

		if err := taskCreatePRCmd.RunE(taskCreatePRCmd, nil); err == nil {
			t.Fatal("expected error when both --body and --body-file are set")
		}
		if len(mock.calls) != 0 {
			t.Fatal("expected no dispatch when flags conflict")
		}
	})

	t.Run("missing body-file surfaces a read error", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		prTitleFlag, prBodyFlag, prBodyFileFlag = "T", "", "/no/such/file.md"
		defer func() { prTitleFlag, prBodyFlag, prBodyFileFlag = "", "", "" }()

		if err := taskCreatePRCmd.RunE(taskCreatePRCmd, nil); err == nil {
			t.Fatal("expected error for unreadable --body-file")
		}
	})

	t.Run("submit-review reads event, body-file, and comments-file", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		dir := t.TempDir()
		bodyPath := dir + "/review.md"
		commentsPath := dir + "/comments.json"
		comments := `[{"path":"a.go","line":1,"body":"x"}]`
		if err := os.WriteFile(bodyPath, []byte("## Review"), 0o644); err != nil {
			t.Fatalf("write body: %v", err)
		}
		if err := os.WriteFile(commentsPath, []byte(comments), 0o644); err != nil {
			t.Fatalf("write comments: %v", err)
		}

		prFlag, prEventFlag, prBodyFileFlag, prCommentsFile = "42", "request-changes", bodyPath, commentsPath
		defer func() {
			prFlag, prEventFlag, prBodyFlag, prBodyFileFlag, prCommentsFile = "", "", "", "", ""
		}()

		if err := taskSubmitReviewCmd.RunE(taskSubmitReviewCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls[0] != "SubmitReview" {
			t.Fatalf("expected SubmitReview, got %v", mock.calls)
		}
		if mock.lastArg["pr"] != "42" || mock.lastArg["verdict"] != "request-changes" ||
			mock.lastArg["body"] != "## Review" || mock.lastArg["comments"] != comments {
			t.Fatalf("unexpected args: %v", mock.lastArg)
		}
	})

	t.Run("submit-review surfaces an unreadable comments-file", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		prEventFlag, prCommentsFile = "comment", "/no/such/comments.json"
		defer func() { prEventFlag, prCommentsFile, prBodyFlag = "", "", "" }()

		if err := taskSubmitReviewCmd.RunE(taskSubmitReviewCmd, nil); err == nil {
			t.Fatal("expected error for unreadable --comments-file")
		}
		if len(mock.calls) != 0 {
			t.Fatal("expected no dispatch when comments-file is unreadable")
		}
	})

	// --commit is what makes a review of a commit that is not checked out
	// name the commit it actually judged. It must reach the runner verbatim,
	// and must stay empty for the ordinary on-branch call.
	t.Run("submit-review forwards --commit, and empty without it", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		prFlag, prEventFlag, prBodyFlag, prCommitFlag = "42", "approve", "LGTM", "9f2c1ab"
		defer func() { prFlag, prEventFlag, prBodyFlag, prCommitFlag = "", "", "", "" }()

		if err := taskSubmitReviewCmd.RunE(taskSubmitReviewCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.lastArg["commit"] != "9f2c1ab" {
			t.Fatalf("expected commit forwarded, got %q", mock.lastArg["commit"])
		}

		prCommitFlag = ""
		if err := taskSubmitReviewCmd.RunE(taskSubmitReviewCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.lastArg["commit"] != "" {
			t.Fatalf("expected no commit without --commit, got %q", mock.lastArg["commit"])
		}
	})

	t.Run("approve-pr forwards --commit, and empty without it", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		prFlag, prBodyFlag, prCommitFlag = "42", "LGTM", "9f2c1ab"
		defer func() { prFlag, prBodyFlag, prCommitFlag = "", "", "" }()

		if err := taskApprovePRCmd.RunE(taskApprovePRCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls[0] != "ApprovePR" {
			t.Fatalf("expected ApprovePR, got %v", mock.calls)
		}
		if mock.lastArg["commit"] != "9f2c1ab" {
			t.Fatalf("expected commit forwarded, got %q", mock.lastArg["commit"])
		}

		prCommitFlag = ""
		if err := taskApprovePRCmd.RunE(taskApprovePRCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.lastArg["commit"] != "" {
			t.Fatalf("expected no commit without --commit, got %q", mock.lastArg["commit"])
		}
	})

	t.Run("reply-thread accepts --body-file", func(t *testing.T) {
		mock := newMockPRRunner()
		defer setupPRMock(t, mock)()

		dir := t.TempDir()
		path := dir + "/reply.md"
		if err := os.WriteFile(path, []byte("Fixed in **abc123**"), 0o644); err != nil {
			t.Fatalf("write temp: %v", err)
		}
		prBodyFileFlag = path
		defer func() { prBodyFileFlag = "" }()

		if err := taskReplyThreadCmd.RunE(taskReplyThreadCmd, []string{"PRRT_x"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.lastArg["body"] != "Fixed in **abc123**" {
			t.Fatalf("unexpected body: %q", mock.lastArg["body"])
		}
	})
}
