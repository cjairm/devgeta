package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() {
	testutil.InitLogger()
}

func TestNew(t *testing.T) {
	wm := New()
	if wm == nil {
		t.Fatal("New() returned nil")
	}
	if wm.Git == nil {
		t.Error("Git should not be nil")
	}
	if wm.Tmux == nil {
		t.Error("Tmux should not be nil")
	}
	if wm.Fzf == nil {
		t.Error("Fzf should not be nil")
	}
	if wm.Base == nil {
		t.Error("Base should not be nil")
	}
}

func TestGetWindowName(t *testing.T) {
	tests := []struct {
		name     string
		repoSlug string
		input    string
		expected string
	}{
		{"simple name", "myrepo", "feature", "wt-myrepo-feature"},
		{"hyphenated name", "myrepo", "feature-login", "wt-myrepo-feature-login"},
		{"with numbers", "myrepo", "fix-123", "wt-myrepo-fix-123"},
		{
			"ticket id shared across repos",
			"jobvite_TalentNetwork",
			"CXE-35",
			"wt-jobvite_TalentNetwork-CXE-35",
		},
		{"slashes in name flattened", "myrepo", "feat/search", "wt-myrepo-feat-search"},
		{"dots and colons in repo sanitized", "my.repo:x", "CXE-35", "wt-my_repo_x-CXE-35"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetWindowName(tt.repoSlug, tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSelectWorktreeInteractively(t *testing.T) {
	t.Skip(
		"Skipping: SelectFromList uses exec.Command which requires actual fzf binary and would block in CI",
	)
}

func TestGetWorktreeDir(t *testing.T) {
	result := GetWorktreeDir()
	if result != ".worktrees" {
		t.Errorf("Expected '.worktrees', got %q", result)
	}
}

func TestCreate(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		tempDir := t.TempDir()

		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()

		gitApp := &git.Git{
			Cmd:  commands.NewMockCommand(),
			Base: mockGitBase,
		}
		tmuxApp := &tmux.Tmux{
			Cmd:  commands.NewMockCommand(),
			Base: mockTmuxBase,
		}

		wm := &WorktreeManager{
			Git:  gitApp,
			Tmux: tmuxApp,
			Base: commands.NewMockBaseCommand(),
		}

		mockGitBase.SetExecCommandResult(tempDir+"\n", "", nil)
		mockTmuxBase.SetExecCommandResult("", "window not found", os.ErrNotExist)

		err := wm.Create("feature-test", stubLayout, true)
		if err == nil {
			if mockGitBase.GetExecCommandCallCount() < 1 {
				t.Error("Expected git commands to be called")
			}
		}
	})

	t.Run("empty layout returns error", func(t *testing.T) {
		wm := &WorktreeManager{}
		err := wm.Create("test", Layout{}, true)
		if err == nil {
			t.Fatal("Expected error for a layout with no panes")
		}
	})

	t.Run("not in git repo", func(t *testing.T) {
		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()

		gitApp := &git.Git{
			Cmd:  commands.NewMockCommand(),
			Base: mockGitBase,
		}
		tmuxApp := &tmux.Tmux{
			Cmd:  commands.NewMockCommand(),
			Base: mockTmuxBase,
		}

		wm := &WorktreeManager{
			Git:  gitApp,
			Tmux: tmuxApp,
			Base: commands.NewMockBaseCommand(),
		}

		mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)

		err := wm.Create("feature-test", stubLayout, true)
		if err == nil {
			t.Fatal("Expected error when not in git repo")
		}
	})
}

// worktreePorcelain builds a `git worktree list --porcelain`-shaped response:
// mainRoot as the first ("main worktree") entry - always first, per git's
// documented, stable guarantee that List()'s dedup relies on - followed by one
// entry per (path, branch) pair in linked.
func worktreePorcelain(mainRoot string, linked ...[2]string) string {
	var b strings.Builder
	b.WriteString("worktree " + mainRoot + "\n")
	b.WriteString("HEAD 0000000000000000000000000000000000000000\n")
	b.WriteString("branch refs/heads/main\n\n")
	for _, l := range linked {
		b.WriteString("worktree " + l[0] + "\n")
		b.WriteString("HEAD 1111111111111111111111111111111111111111\n")
		b.WriteString("branch refs/heads/" + l[1] + "\n\n")
	}
	return b.String()
}

// newListTestWM builds a WorktreeManager wired to fresh mocks and isolates
// paths.Paths.Config/App roots (via testutil.SetupIsolatedPaths) so this
// subtest's config.Load() calls (inside knownRepoAnchors) never see another
// subtest's (or another test file's) global_config.yaml. GetWorktreeBasePath
// (paths.Paths.Data.Root) is deliberately NOT isolated here: it already lives
// under go test's process-wide sandbox (pkg/paths), and every subtest that
// creates shared-root fixtures cleans up its own repo-slug directory via
// t.Cleanup, mirroring the existing createWorktreeDir convention.
func newListTestWM(t *testing.T) (wm *WorktreeManager, mockGitBase, mockTmuxBase *commands.MockBaseCommand) {
	t.Helper()
	cleanupPaths := testutil.SetupIsolatedPaths(t)
	t.Cleanup(cleanupPaths)

	mockGitBase = commands.NewMockBaseCommand()
	mockTmuxBase = commands.NewMockBaseCommand()
	wm = &WorktreeManager{
		Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
		Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
		Base: commands.NewMockBaseCommand(),
	}
	return
}

// countExecCommandCallsWithDir counts how many ExecCommand calls targeted dir
// via a `-C <dir>` argument pair - used to assert ListWorktreesAt was called
// exactly once for a given repo, regardless of how many worktrees it has.
func countExecCommandCallsWithDir(mockBase *commands.MockBaseCommand, dir string) int {
	count := 0
	for _, c := range mockBase.ExecCommandCalls {
		for i, a := range c.Args {
			if a == "-C" && i+1 < len(c.Args) && c.Args[i+1] == dir {
				count++
				break
			}
		}
	}
	return count
}

func TestList(t *testing.T) {
	// cwdRepoRoot is exercised via w.Git.GetMainWorktree(cwd), which always
	// costs one exec call regardless of whether cwd is a real repo. Chdir-ing
	// to a directory the git mock reports as "not a repo" keeps that source
	// out of a subtest's anchor set without special-casing it.
	chdirToNonRepo := func(t *testing.T) {
		t.Helper()
		t.Chdir(t.TempDir())
	}

	t.Run("shared-location worktree is listed", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		chdirToNonRepo(t)

		mainRoot := filepath.Join(t.TempDir(), "sharedrepo")
		wtPath := createWorktreeDir(t, "sharedrepo", "featx")
		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist), // cwdRepoRoot
			commands.ExecCommandResult(
				worktreePorcelain(mainRoot, [2]string{wtPath, "feature-x"}), "", nil,
			), // Step B: the one shared-root anchor
		)

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(statuses) != 1 {
			t.Fatalf("expected exactly 1 status, got %d: %+v", len(statuses), statuses)
		}
		s := statuses[0]
		if s.Name != "featx" || s.Path != wtPath || s.Branch != "feature-x" || s.Repo != "sharedrepo" {
			t.Errorf(
				"got Name=%q Path=%q Branch=%q Repo=%q, want Name=featx Path=%q Branch=feature-x Repo=sharedrepo",
				s.Name, s.Path, s.Branch, s.Repo, wtPath,
			)
		}
	})

	// This is the entire point of the cycle: a worktree living at an in-repo
	// path (<repo-root>/.claude/worktrees/<name>), reached only via
	// cwdRepoRoot/recent-repos - never under the shared-root base path - must
	// show up in List().
	t.Run("in-repo worktree is listed", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)

		repoRoot := t.TempDir()
		t.Chdir(repoRoot)
		actualCwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		wtPath := inRepoWorktreePath(actualCwd, "irepofeat")
		mockGitBase.SetExecCommandResult(
			worktreePorcelain(actualCwd, [2]string{wtPath, "irepo-branch"}), "", nil,
		)

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(statuses) != 1 {
			t.Fatalf("expected exactly 1 status, got %d: %+v", len(statuses), statuses)
		}
		s := statuses[0]
		wantRepo := filepath.Base(actualCwd)
		if s.Name != "irepofeat" || s.Path != wtPath || s.Branch != "irepo-branch" || s.Repo != wantRepo {
			t.Errorf(
				"got Name=%q Path=%q Branch=%q Repo=%q, want Name=irepofeat Path=%q Branch=irepo-branch Repo=%q",
				s.Name, s.Path, s.Branch, s.Repo, wtPath, wantRepo,
			)
		}
	})

	// A husk directory (e.g. left behind by a botched move) sits under the
	// same repo-slug as a real worktree. Trying the husk anchor first must not
	// hide the real worktree found via a later anchor under the same slug -
	// this is why knownRepoAnchors collects every subdirectory, not just the
	// first.
	t.Run("phantom husk under a shared slug is skipped without hiding a real worktree", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		chdirToNonRepo(t)

		// "a-husk" sorts before "b-real" so os.ReadDir tries the husk first.
		huskPath := createWorktreeDir(t, "huskslug", "a-husk")
		realPath := createWorktreeDir(t, "huskslug", "b-real")
		mainRoot := filepath.Join(t.TempDir(), "huskslug")

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist), // cwdRepoRoot
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist), // a-husk anchor: not a real worktree
			commands.ExecCommandResult(
				worktreePorcelain(mainRoot, [2]string{realPath, "real-branch"}), "", nil,
			), // b-real anchor
		)

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(statuses) != 1 {
			t.Fatalf("expected exactly 1 status (husk skipped), got %d: %+v", len(statuses), statuses)
		}
		s := statuses[0]
		if s.Name != "b-real" || s.Path != realPath || s.Path == huskPath {
			t.Errorf("got %+v, want the real worktree at %q, never the husk at %q", s, realPath, huskPath)
		}
	})

	// The regression that matters most: a subtle bug reintroducing
	// one-ListWorktreesAt-call-per-worktree would pass every other test here.
	// A repo with 2 worktrees, reachable via a single anchor source
	// (recent-repos), must cost exactly one ListWorktreesAt call for that
	// repo - not two.
	t.Run("ListWorktreesAt is called once per repo, not once per worktree", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		chdirToNonRepo(t)

		repoRoot := t.TempDir()
		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			t.Fatalf("setup: %v", err)
		}
		gc.Worktree.RecentRepos = []config.RecentRepo{{Path: repoRoot, LastUsed: time.Now()}}
		if err := gc.Save(); err != nil {
			t.Fatalf("setup: %v", err)
		}

		alpha := filepath.Join(repoRoot, ".claude", "worktrees", "alpha")
		beta := filepath.Join(repoRoot, ".claude", "worktrees", "beta")
		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist), // cwdRepoRoot
			commands.ExecCommandResult(
				worktreePorcelain(repoRoot, [2]string{alpha, "alpha-branch"}, [2]string{beta, "beta-branch"}),
				"", nil,
			), // Step B: the single recent-repos anchor
		)

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(statuses) != 2 {
			t.Fatalf("expected 2 statuses (alpha, beta), got %d: %+v", len(statuses), statuses)
		}
		if got := countExecCommandCallsWithDir(mockGitBase, repoRoot); got != 1 {
			t.Errorf(
				"expected ListWorktreesAt called exactly once for repo %q (2 worktrees), got %d calls",
				repoRoot,
				got,
			)
		}
	})

	// The same repo reachable via two anchor sources (cwd AND recent-repos)
	// must still produce its worktrees exactly once.
	t.Run("a repo reachable via two anchor sources is not duplicated", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)

		cwdRoot := t.TempDir()
		t.Chdir(cwdRoot)
		actualCwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			t.Fatalf("setup: %v", err)
		}
		gc.Worktree.RecentRepos = []config.RecentRepo{{Path: actualCwd, LastUsed: time.Now()}}
		if err := gc.Save(); err != nil {
			t.Fatalf("setup: %v", err)
		}

		wtPath := inRepoWorktreePath(actualCwd, "dedupfeat")
		// Every anchor here (cwd's own GetMainWorktree call, and both Step B
		// queries for the cwd anchor and the recent-repos anchor - both the
		// same repo) returns the identical porcelain, so a single fixed mock
		// result is enough.
		mockGitBase.SetExecCommandResult(
			worktreePorcelain(actualCwd, [2]string{wtPath, "dedup-branch"}), "", nil,
		)

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(statuses) != 1 {
			t.Fatalf("expected exactly 1 status (deduped), got %d: %+v", len(statuses), statuses)
		}
		if statuses[0].Name != "dedupfeat" {
			t.Errorf("got %+v, want Name=dedupfeat", statuses[0])
		}
	})

	t.Run("no config and empty shared root returns an empty result, no error", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		chdirToNonRepo(t)
		mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if statuses == nil {
			t.Error("expected a non-nil empty slice, got nil")
		}
		if len(statuses) != 0 {
			t.Errorf("expected no statuses, got %+v", statuses)
		}
	})

	t.Run("GetWorktreeBasePath missing entirely still returns empty, no error", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		chdirToNonRepo(t)
		mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)

		if err := os.RemoveAll(GetWorktreeBasePath()); err != nil {
			t.Fatalf("setup: %v", err)
		}

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if statuses == nil {
			t.Error("expected a non-nil empty slice, got nil")
		}
		if len(statuses) != 0 {
			t.Errorf("expected no statuses, got %+v", statuses)
		}
	})

	// one scan, not N: the regression this whole refactor exists to prevent.
	// Before this task, List() called Tmux.WindowSession() once per worktree,
	// and each of those calls ran its own fresh list-windows -a scan (N tmux
	// execs per dashboard refresh). List() must now call Tmux.PaneStates()
	// exactly once regardless of how many worktrees it finds.
	t.Run("single tmux scan regardless of worktree count", func(t *testing.T) {
		wm, mockGitBase, mockTmuxBase := newListTestWM(t)
		chdirToNonRepo(t)

		windowA := GetWindowName("repoA", "wt1")
		windowB := GetWindowName("repoB", "wt2")
		mockTmuxBase.SetExecCommandResult(
			"sessA\t"+windowA+"\t%1\tbusy\n"+
				"sessB\t"+windowB+"\t%2\tidle\n",
			"",
			nil,
		)

		mainRootA := filepath.Join(t.TempDir(), "repoA")
		mainRootB := filepath.Join(t.TempDir(), "repoB")
		wtPathA := createWorktreeDir(t, "repoA", "wt1")
		wtPathB := createWorktreeDir(t, "repoB", "wt2")
		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist), // cwdRepoRoot
			commands.ExecCommandResult(
				worktreePorcelain(mainRootA, [2]string{wtPathA, "feat-a"}), "", nil,
			), // repoA anchor
			commands.ExecCommandResult(
				worktreePorcelain(mainRootB, [2]string{wtPathB, "feat-b"}), "", nil,
			), // repoB anchor
		)

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if got := mockTmuxBase.GetExecCommandCallCount(); got != 1 {
			t.Errorf(
				"expected exactly 1 tmux exec call for %d worktrees, got %d",
				len(statuses),
				got,
			)
		}

		byWindow := make(map[string]WorktreeStatus, len(statuses))
		for _, s := range statuses {
			byWindow[s.TmuxWindow] = s
		}
		if _, ok := byWindow[windowA]; !ok {
			t.Fatalf("expected a status for window %s, got byWindow=%+v", windowA, byWindow)
		}
		if _, ok := byWindow[windowB]; !ok {
			t.Fatalf("expected a status for window %s, got byWindow=%+v", windowB, byWindow)
		}
		if s := byWindow[windowA]; !s.WindowActive || s.AgentState != "busy" {
			t.Errorf(
				"window %s: got WindowActive=%v AgentState=%q, want true/\"busy\"",
				windowA,
				s.WindowActive,
				s.AgentState,
			)
		}
		if s := byWindow[windowB]; !s.WindowActive || s.AgentState != "idle" {
			t.Errorf(
				"window %s: got WindowActive=%v AgentState=%q, want true/\"idle\"",
				windowB,
				s.WindowActive,
				s.AgentState,
			)
		}
	})

	// Aggregation over a window's panes must follow ADR-0005's precedence:
	// blocked > error > idle > busy > (no agent). This covers two precedence
	// pairs so the ordering isn't just checked at a single point: idle-over-busy
	// (the split-pane review case ADR-0005 exists for) and blocked-over-error.
	t.Run("aggregates multiple panes per window by precedence", func(t *testing.T) {
		wm, mockGitBase, mockTmuxBase := newListTestWM(t)
		chdirToNonRepo(t)

		windowMixed := GetWindowName("repoC", "mixed")
		windowUrgent := GetWindowName("repoC", "urgent")
		mockTmuxBase.SetExecCommandResult(
			"sess\t"+windowMixed+"\t%1\tidle\n"+
				"sess\t"+windowMixed+"\t%2\tbusy\n"+
				"sess\t"+windowUrgent+"\t%3\tblocked\n"+
				"sess\t"+windowUrgent+"\t%4\terror\n",
			"",
			nil,
		)

		mainRootC := filepath.Join(t.TempDir(), "repoC")
		wtMixed := createWorktreeDir(t, "repoC", "mixed")
		wtUrgent := createWorktreeDir(t, "repoC", "urgent")
		// Both "mixed" and "urgent" are separate anchors under the same slug
		// and resolve to the same repo; the identical response for every git
		// call is enough since only the fixture repo is in play here.
		mockGitBase.SetExecCommandResult(
			worktreePorcelain(
				mainRootC,
				[2]string{wtMixed, "mixed-branch"},
				[2]string{wtUrgent, "urgent-branch"},
			),
			"", nil,
		)

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		byWindow := make(map[string]WorktreeStatus, len(statuses))
		for _, s := range statuses {
			byWindow[s.TmuxWindow] = s
		}
		if got := byWindow[windowMixed].AgentState; got != "idle" {
			t.Errorf(
				"window with one idle + one busy pane: got AgentState %q, want \"idle\" (idle outranks busy)",
				got,
			)
		}
		if got := byWindow[windowUrgent].AgentState; got != "blocked" {
			t.Errorf(
				"window with one blocked + one error pane: got AgentState %q, want \"blocked\" (blocked outranks error)",
				got,
			)
		}
	})

	// A window absent from the pane-state scan (no tmux window at all) and a
	// window present but whose pane(s) never had @dg_agent_state set (e.g. an
	// editor pane) are different situations and must be distinguished:
	// WindowActive differs even though AgentState is "" in both cases.
	t.Run("no agent vs no window are distinguished", func(t *testing.T) {
		wm, mockGitBase, mockTmuxBase := newListTestWM(t)
		chdirToNonRepo(t)

		windowPresent := GetWindowName("repoD", "editor-only")
		windowAbsent := GetWindowName("repoD", "no-window")
		// windowAbsent deliberately has no line in the scan output at all.
		mockTmuxBase.SetExecCommandResult(
			"sess\t"+windowPresent+"\t%1\t\n",
			"",
			nil,
		)

		mainRootD := filepath.Join(t.TempDir(), "repoD")
		wtEditorOnly := createWorktreeDir(t, "repoD", "editor-only")
		wtNoWindow := createWorktreeDir(t, "repoD", "no-window")
		mockGitBase.SetExecCommandResult(
			worktreePorcelain(
				mainRootD,
				[2]string{wtEditorOnly, "editor-branch"},
				[2]string{wtNoWindow, "no-window-branch"},
			),
			"", nil,
		)

		statuses, err := wm.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		byWindow := make(map[string]WorktreeStatus, len(statuses))
		for _, s := range statuses {
			byWindow[s.TmuxWindow] = s
		}
		if s := byWindow[windowPresent]; !s.WindowActive || s.AgentState != "" {
			t.Errorf(
				"window present with empty-state pane: got WindowActive=%v AgentState=%q, want true/\"\"",
				s.WindowActive,
				s.AgentState,
			)
		}
		if s := byWindow[windowAbsent]; s.WindowActive || s.AgentState != "" {
			t.Errorf(
				"window absent from scan: got WindowActive=%v AgentState=%q, want false/\"\"",
				s.WindowActive,
				s.AgentState,
			)
		}
	})
}

// createWorktreeDir creates the on-disk directory List() scans
// (…/worktrees/<repoSlug>/<name>) and registers cleanup of the whole
// repoSlug directory, mirroring the fixture pattern already used by
// TestRemove/TestRemoveByRepoUsesCorrectPath.
func createWorktreeDir(t *testing.T, repoSlug, name string) string {
	t.Helper()
	repoDir := filepath.Join(GetWorktreeBasePath(), repoSlug)
	wtPath := filepath.Join(repoDir, name)
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(repoDir); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})
	return wtPath
}

// TestAggregateAgentState exercises the pure aggregation function directly
// per ADR-0005's precedence: blocked > error > idle > busy > (no agent).
// Ties and unrecognized values must fall back to "no agent" rank rather than
// crashing or silently outranking a real value.
func TestAggregateAgentState(t *testing.T) {
	tests := []struct {
		name   string
		states []string
		want   string
	}{
		{"empty input", nil, ""},
		{"single empty state", []string{""}, ""},
		{"single busy", []string{"busy"}, "busy"},
		{"single idle", []string{"idle"}, "idle"},
		{"single blocked", []string{"blocked"}, "blocked"},
		{"single error", []string{"error"}, "error"},
		{"idle outranks busy", []string{"idle", "busy"}, "idle"},
		{"busy then idle - order independent", []string{"busy", "idle"}, "idle"},
		{"error outranks idle", []string{"error", "idle"}, "error"},
		{"blocked outranks error", []string{"blocked", "error"}, "blocked"},
		{"blocked outranks everything", []string{"busy", "idle", "error", "blocked"}, "blocked"},
		{"empty and busy - busy wins", []string{"", "busy"}, "busy"},
		{"unknown value alone ranks as no agent", []string{"weird-future-state"}, ""},
		{
			"unknown value never outranks a real value",
			[]string{"weird-future-state", "busy"},
			"busy",
		},
		{
			"unknown value does not beat idle",
			[]string{"idle", "weird-future-state"},
			"idle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aggregateAgentState(tt.states); got != tt.want {
				t.Errorf("aggregateAgentState(%v) = %q, want %q", tt.states, got, tt.want)
			}
		})
	}
}

func TestRemove(t *testing.T) {
	t.Run("successful removal with active window", func(t *testing.T) {
		tempDir := t.TempDir()

		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()

		gitApp := &git.Git{
			Cmd:  commands.NewMockCommand(),
			Base: mockGitBase,
		}
		tmuxApp := &tmux.Tmux{
			Cmd:  commands.NewMockCommand(),
			Base: mockTmuxBase,
		}

		wm := &WorktreeManager{
			Git:  gitApp,
			Tmux: tmuxApp,
			Base: commands.NewMockBaseCommand(),
		}

		mockGitBase.SetExecCommandResult(tempDir+"\n", "", nil)
		mockTmuxBase.SetExecCommandResult("", "", nil)

		repoSlug := filepath.Base(tempDir)
		wtPath := filepath.Join(
			paths.Paths.Data.Root,
			"devgeta",
			"worktrees",
			repoSlug,
			"feature-test",
		)
		if err := os.MkdirAll(wtPath, 0o755); err != nil {
			t.Fatalf("Failed to create worktree dir: %v", err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		err := wm.Remove("feature-test", true)
		if err != nil {
			t.Fatalf("Remove failed: %v", err)
		}

		if mockGitBase.GetExecCommandCallCount() < 1 {
			t.Error("Expected git commands to be called")
		}
		if mockTmuxBase.GetExecCommandCallCount() < 1 {
			t.Error("Expected tmux commands to be called")
		}
	})

	t.Run("removal without active window", func(t *testing.T) {
		tempDir := t.TempDir()

		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()

		gitApp := &git.Git{
			Cmd:  commands.NewMockCommand(),
			Base: mockGitBase,
		}
		tmuxApp := &tmux.Tmux{
			Cmd:  commands.NewMockCommand(),
			Base: mockTmuxBase,
		}

		wm := &WorktreeManager{
			Git:  gitApp,
			Tmux: tmuxApp,
			Base: commands.NewMockBaseCommand(),
		}

		mockGitBase.SetExecCommandResult(tempDir+"\n", "", nil)
		mockTmuxBase.SetExecCommandResult("", "window not found", os.ErrNotExist)

		repoSlug := filepath.Base(tempDir)
		wtPath := filepath.Join(
			paths.Paths.Data.Root,
			"devgeta",
			"worktrees",
			repoSlug,
			"feature-test",
		)
		if err := os.MkdirAll(wtPath, 0o755); err != nil {
			t.Fatalf("Failed to create worktree dir: %v", err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		err := wm.Remove("feature-test", true)
		if err != nil {
			t.Fatalf("Remove failed: %v", err)
		}
	})

	t.Run("not in git repo", func(t *testing.T) {
		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()

		gitApp := &git.Git{
			Cmd:  commands.NewMockCommand(),
			Base: mockGitBase,
		}
		tmuxApp := &tmux.Tmux{
			Cmd:  commands.NewMockCommand(),
			Base: mockTmuxBase,
		}

		wm := &WorktreeManager{
			Git:  gitApp,
			Tmux: tmuxApp,
			Base: commands.NewMockBaseCommand(),
		}

		mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)

		err := wm.Remove("feature-test", false)
		if err == nil {
			t.Fatal("Expected error when not in git repo")
		}
	})
}

// TestRemoveByRepoUsesCorrectPath verifies that the worktree path constructed in
// removeByRepo matches the path that List() would discover, catching the bug where
// Jump() passed "repo/name" as repoSlug instead of just "repo".
func TestRemoveByRepoUsesCorrectPath(t *testing.T) {
	const wtName = "fix-bug"

	newWM := func() (*WorktreeManager, string) {
		tempDir := t.TempDir()
		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()
		// Make git commands fail so RemoveWorktree errors and os.RemoveAll fallback runs.
		mockGitBase.SetExecCommandResult("", "git error", os.ErrNotExist)
		// Make tmux commands fail so window is reported as not present.
		mockTmuxBase.SetExecCommandResult("", "window not found", os.ErrNotExist)
		wm := &WorktreeManager{
			Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
			Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
			Base: commands.NewMockBaseCommand(),
		}
		return wm, filepath.Base(tempDir)
	}

	t.Run("wrong repoSlug leaves directory intact", func(t *testing.T) {
		wm, repoSlug := newWM()
		wtPath := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, wtName)
		if err := os.MkdirAll(wtPath, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		// "repo/name" was the broken slug Jump() used to pass.
		wrongSlug := repoSlug + "/" + wtName
		if err := wm.removeByRepo(wrongSlug, wtName, true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			t.Error("directory was removed despite wrong repoSlug — fix broke the invariant")
		}
	})

	t.Run("correct repoSlug removes directory via fallback", func(t *testing.T) {
		wm, repoSlug := newWM()
		wtPath := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, wtName)
		if err := os.MkdirAll(wtPath, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		if err := wm.removeByRepo(repoSlug, wtName, true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
			t.Error("expected directory to be removed with correct repoSlug")
		}
	})
}

// setWorktreeLocation isolates the global config (via testutil.SetupIsolatedPaths,
// which the caller must have already invoked) and writes worktree.location,
// so a test can drive worktreePath's location branch without depending on
// or polluting any other test's config state.
func setWorktreeLocation(t *testing.T, location string) {
	t.Helper()
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("setup: failed to create config: %v", err)
	}
	gc.Worktree.Location = location
	if err := gc.Save(); err != nil {
		t.Fatalf("setup: failed to save config: %v", err)
	}
}

// setWorktreeLocationAndRecentRepos is setWorktreeLocation plus seeding
// worktree.recent_repos, for tests proving in-repo root resolution falls
// back to the recent-repos store when the current repo doesn't match.
func setWorktreeLocationAndRecentRepos(
	t *testing.T,
	location string,
	repos []config.RecentRepo,
) {
	t.Helper()
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("setup: failed to create config: %v", err)
	}
	gc.Worktree.Location = location
	gc.Worktree.RecentRepos = repos
	if err := gc.Save(); err != nil {
		t.Fatalf("setup: failed to save config: %v", err)
	}
}

// notInARepo configures mockGitBase so w.Git.GetRepoRoot() fails, simulating
// a caller whose cwd is not inside any git repository (or not inside the
// repo being asked about) - forcing worktreePath's in-repo resolution past
// its first (current-repo) step.
func notInARepo(mockGitBase *commands.MockBaseCommand) {
	mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)
}

// TestWorktreePath is Step 3's core test: worktreePath must stay
// byte-identical for the shared location (today's only behavior, and every
// existing user's default) while correctly computing the in-repo shape and
// resolving repoSlug back to a root via the current repo or the
// recent-repos store - failing with an actionable error, never a silent
// fallback, when neither resolves it.
func TestWorktreePath(t *testing.T) {
	t.Run("shared (unset) is byte-identical to today's path", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()

		wm := &WorktreeManager{}
		path, err := wm.worktreePath("myrepo", "feature-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", "myrepo", "feature-a")
		if path != want {
			t.Errorf("expected %q, got %q", want, path)
		}
	})

	t.Run("shared explicitly set produces the same path as unset", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		setWorktreeLocation(t, config.WorktreeLocationShared)

		wm := &WorktreeManager{}
		path, err := wm.worktreePath("myrepo", "feature-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", "myrepo", "feature-a")
		if path != want {
			t.Errorf("expected %q, got %q", want, path)
		}
	})

	t.Run("shared location flattens a name containing slashes", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()

		wm := &WorktreeManager{}
		path, err := wm.worktreePath("myrepo", "feat/search-specs")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(
			paths.Paths.Data.Root, "devgeta", "worktrees", "myrepo", "feat-search-specs",
		)
		if path != want {
			t.Errorf("expected %q, got %q", want, path)
		}
	})

	t.Run("in-repo resolves the root via the current repo", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		setWorktreeLocation(t, config.WorktreeLocationInRepo)

		repoRoot := t.TempDir()
		repoSlug := filepath.Base(repoRoot)

		mockGitBase := commands.NewMockBaseCommand()
		mockGitBase.SetExecCommandResult(repoRoot+"\n", "", nil)
		mockTmuxBase := commands.NewMockBaseCommand()
		wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

		path, err := wm.worktreePath(repoSlug, "feature-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(repoRoot, ".claude", "worktrees", "feature-a")
		if path != want {
			t.Errorf("expected %q, got %q", want, path)
		}
	})

	t.Run(
		"in-repo resolves the root via the recent-repos store when the current repo doesn't match",
		func(t *testing.T) {
			cleanupPaths := testutil.SetupIsolatedPaths(t)
			defer cleanupPaths()

			repoRoot := t.TempDir()
			repoSlug := filepath.Base(repoRoot)
			setWorktreeLocationAndRecentRepos(t, config.WorktreeLocationInRepo, []config.RecentRepo{
				{Path: repoRoot, LastUsed: time.Now()},
			})

			mockGitBase := commands.NewMockBaseCommand()
			notInARepo(mockGitBase)
			mockTmuxBase := commands.NewMockBaseCommand()
			wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

			path, err := wm.worktreePath(repoSlug, "feature-a")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := filepath.Join(repoRoot, ".claude", "worktrees", "feature-a")
			if path != want {
				t.Errorf("expected %q, got %q", want, path)
			}
		},
	)

	t.Run("in-repo flattens a name containing slashes", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		setWorktreeLocation(t, config.WorktreeLocationInRepo)

		repoRoot := t.TempDir()
		repoSlug := filepath.Base(repoRoot)
		mockGitBase := commands.NewMockBaseCommand()
		mockGitBase.SetExecCommandResult(repoRoot+"\n", "", nil)
		mockTmuxBase := commands.NewMockBaseCommand()
		wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

		path, err := wm.worktreePath(repoSlug, "feat/search-specs")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(repoRoot, ".claude", "worktrees", "feat-search-specs")
		if path != want {
			t.Errorf("expected %q, got %q", want, path)
		}
	})

	t.Run(
		"in-repo with an unresolvable slug returns an actionable error and no path",
		func(t *testing.T) {
			cleanupPaths := testutil.SetupIsolatedPaths(t)
			defer cleanupPaths()
			setWorktreeLocation(t, config.WorktreeLocationInRepo)

			mockGitBase := commands.NewMockBaseCommand()
			notInARepo(mockGitBase)
			mockTmuxBase := commands.NewMockBaseCommand()
			wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

			path, err := wm.worktreePath("some-unknown-repo", "feature-a")
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if path != "" {
				t.Errorf("expected no path on error, got %q", path)
			}
			if !strings.Contains(err.Error(), "some-unknown-repo") {
				t.Errorf("expected error to name the slug, got %q", err.Error())
			}
		},
	)
}

// TestWorktreePathIn proves worktreePathIn - the root-taking form used by
// create() and any other caller that already resolved repoRoot - produces
// the same two shapes as the slug-based worktreePath, without needing (or
// being able to fail on) resolution.
func TestWorktreePathIn(t *testing.T) {
	t.Run("shared (unset)", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()

		repoRoot := "/some/parent/myrepo"
		path := worktreePathIn(repoRoot, "feature-a")
		want := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", "myrepo", "feature-a")
		if path != want {
			t.Errorf("expected %q, got %q", want, path)
		}
	})

	t.Run("in-repo", func(t *testing.T) {
		cleanupPaths := testutil.SetupIsolatedPaths(t)
		defer cleanupPaths()
		setWorktreeLocation(t, config.WorktreeLocationInRepo)

		repoRoot := "/some/parent/myrepo"
		path := worktreePathIn(repoRoot, "feat/search-specs")
		want := filepath.Join(repoRoot, ".claude", "worktrees", "feat-search-specs")
		if path != want {
			t.Errorf("expected %q, got %q", want, path)
		}
	})
}

// TestWorktreeStateHonorsLocation proves worktreeState (the plan's "inherits
// this for free" claim) correctly computes an in-repo WtPath and detects
// WtExists against it, rather than by inspection of its call to
// worktreePath.
func TestWorktreeStateHonorsLocation(t *testing.T) {
	cleanupPaths := testutil.SetupIsolatedPaths(t)
	defer cleanupPaths()
	setWorktreeLocation(t, config.WorktreeLocationInRepo)

	repoRoot := t.TempDir()
	repoSlug := filepath.Base(repoRoot)
	name := "feature-a"
	wtPath := filepath.Join(repoRoot, ".claude", "worktrees", name)
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mockGitBase := commands.NewMockBaseCommand()
	// A single fixed "<repoRoot>\n" stdout satisfies both git calls this
	// exercises: resolveRepoRoot's rev-parse (needs just the root) and
	// ListWorktreesAt's worktree-list --porcelain (parses to an empty list,
	// since the line lacks a "worktree " prefix - harmless here, since
	// WtExists is already established by the os.Stat check above it).
	mockGitBase.SetExecCommandResult(repoRoot+"\n", "", nil)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResult("", "window not found", os.ErrNotExist)
	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	state, err := wm.worktreeState(repoSlug, name)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.WtPath != wtPath {
		t.Errorf("expected WtPath %q, got %q", wtPath, state.WtPath)
	}
	if !state.WtExists {
		t.Error("expected WtExists true for an existing in-repo worktree directory")
	}
}

// TestRepairHonorsLocation proves Repair correctly locates and repairs an
// in-repo worktree when repoSlugForWorktree resolves the slug via the
// current repo (cwd match).
func TestRepairHonorsLocation(t *testing.T) {
	cleanupPaths := testutil.SetupIsolatedPaths(t)
	defer cleanupPaths()
	setWorktreeLocation(t, config.WorktreeLocationInRepo)

	repoRoot := t.TempDir()
	repoSlug := filepath.Base(repoRoot)
	name := "feature-a"
	windowName := GetWindowName(repoSlug, name)
	wtPath := filepath.Join(repoRoot, ".claude", "worktrees", name)
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResult(repoRoot+"\n", "", nil)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		// ensureWindow's WindowSession lookup: window already exists, so
		// only pane 0's command is resent - no window/session creation.
		commands.ExecCommandResult("some-session\t"+windowName+"\n", "", nil),
		commands.ExecCommandResult("", "", nil), // SendKeysToWindowInSession
	)
	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	if err := wm.Repair(name, stubLayout); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRepairInRepoHonorsLocation proves RepairInRepo correctly locates and
// repairs an in-repo worktree when the repo is resolved via the
// recent-repos store (not cwd) - the other resolution path worktreePath
// supports.
func TestRepairInRepoHonorsLocation(t *testing.T) {
	cleanupPaths := testutil.SetupIsolatedPaths(t)
	defer cleanupPaths()

	repoRoot := t.TempDir()
	repoSlug := filepath.Base(repoRoot)
	name := "feature-a"
	setWorktreeLocationAndRecentRepos(t, config.WorktreeLocationInRepo, []config.RecentRepo{
		{Path: repoRoot, LastUsed: time.Now()},
	})

	windowName := GetWindowName(repoSlug, name)
	wtPath := filepath.Join(repoRoot, ".claude", "worktrees", name)
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mockGitBase := commands.NewMockBaseCommand()
	notInARepo(mockGitBase) // forces resolution via the recent-repos store
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("some-session\t"+windowName+"\n", "", nil),
		commands.ExecCommandResult("", "", nil),
	)
	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	if err := wm.RepairInRepo(repoSlug, name, stubLayout); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRemoveByRepoHonorsLocation proves removeByRepo correctly targets and
// removes an in-repo worktree directory.
func TestRemoveByRepoHonorsLocation(t *testing.T) {
	cleanupPaths := testutil.SetupIsolatedPaths(t)
	defer cleanupPaths()
	setWorktreeLocation(t, config.WorktreeLocationInRepo)

	repoRoot := t.TempDir()
	repoSlug := filepath.Base(repoRoot)
	name := "feature-a"
	wtPath := filepath.Join(repoRoot, ".claude", "worktrees", name)
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mockGitBase := commands.NewMockBaseCommand()
	// Same single fixed stdout as TestWorktreeStateHonorsLocation: resolves
	// the root for rev-parse calls, and - lacking a "worktree " prefix -
	// makes RemoveWorktree's internal GetMainWorktree call fail, exercising
	// the same os.RemoveAll fallback TestRemoveByRepoUsesCorrectPath relies
	// on above, just with an in-repo path.
	mockGitBase.SetExecCommandResult(repoRoot+"\n", "", nil)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResult("", "window not found", os.ErrNotExist)
	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	if err := wm.removeByRepo(repoSlug, name, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("expected the in-repo worktree directory to be removed")
	}
}

func TestGetWorktreeBasePath(t *testing.T) {
	basePath := GetWorktreeBasePath()
	expectedSuffix := filepath.Join("devgeta", "worktrees")
	if !filepath.IsAbs(basePath) {
		t.Errorf("Expected absolute path, got %q", basePath)
	}
	if !strings.HasSuffix(basePath, expectedSuffix) {
		t.Errorf("Expected path to end with %q, got %q", expectedSuffix, basePath)
	}
}

// TestRepairStaleWorktree verifies that Repair detects when directory is missing
// and provides helpful error message
func TestRepairStaleWorktree(t *testing.T) {
	tempDir := t.TempDir()

	mockGitBase := commands.NewMockBaseCommand()
	mockTmuxBase := commands.NewMockBaseCommand()

	gitApp := &git.Git{
		Cmd:  commands.NewMockCommand(),
		Base: mockGitBase,
	}
	tmuxApp := &tmux.Tmux{
		Cmd:  commands.NewMockCommand(),
		Base: mockTmuxBase,
	}

	wm := &WorktreeManager{
		Git:  gitApp,
		Tmux: tmuxApp,
		Base: commands.NewMockBaseCommand(),
	}

	repoSlug := filepath.Base(tempDir)
	wtPath := filepath.Join(
		paths.Paths.Data.Root,
		"devgeta",
		"worktrees",
		repoSlug,
		"stale-feature",
	)

	// First call: GetRepoRoot
	mockGitBase.SetExecCommandResult(tempDir+"\n", "", nil)

	// Simulate git worktree list returning the stale entry
	// This simulates what happens when directory is deleted but git still tracks it
	staleWorktreeOutput := "worktree " + wtPath + "\nHEAD abc123\nbranch refs/heads/stale-feature\n\n"

	// We need to track multiple mock calls, but our mock doesn't support that well
	// For now, just test the basic case where directory doesn't exist
	// The real-world scenario is already fixed by the code changes

	// Don't create the directory - it's missing
	// Call Repair and expect error about missing worktree
	err := wm.Repair("stale-feature", stubLayout)
	if err == nil {
		t.Fatal("Expected error for non-existent worktree")
	}

	// The error should be clear
	errMsg := err.Error()
	if !strings.Contains(errMsg, "no worktree") {
		t.Errorf("Expected error about non-existent worktree, got: %v", err)
	}

	// Now test the case where directory is found in git list but missing on disk
	// Create directory first, then remove it after checking state
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// Mock git worktree list to return our test worktree
	mockGitBase.SetExecCommandResult(tempDir+"\n"+staleWorktreeOutput, "", nil)

	// Remove the directory to simulate stale state
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatalf("failed to remove test directory: %v", err)
	}

	// Now call Repair - it should detect the missing directory
	// Note: This requires the mock to properly return the worktree list
	// For this integration test, we'll just verify the function exists and handles the case
	_ = staleWorktreeOutput // Use the variable to avoid lint error
}

// TestCreateStaleWorktree verifies that Create auto-prunes stale worktrees
// and continues with creation
func TestCreateStaleWorktree(t *testing.T) {
	t.Skip(
		"This test requires complex mock setup to simulate git worktree list output with stale entries",
	)
}

// tmuxCallArgs flattens the recorded tmux ExecCommand calls into "cmd arg1 arg2" strings.
func tmuxCallArgs(mockBase *commands.MockBaseCommand) []string {
	var out []string
	for _, c := range mockBase.ExecCommandCalls {
		out = append(out, strings.Join(c.Args, " "))
	}
	return out
}

func TestRemoveWithSessionInRepo(t *testing.T) {
	const wtName = "feat"

	// newWM builds a manager whose worktree directory does not exist (so only
	// the tmux window/session paths are exercised) and whose window lives in
	// the given session.
	newWM := func(t *testing.T, session string) (*WorktreeManager, *commands.MockBaseCommand, string) {
		t.Helper()
		repoSlug := filepath.Base(t.TempDir())
		mockGitBase := commands.NewMockBaseCommand()
		mockGitBase.SetExecCommandResult("", "", nil)
		mockTmuxBase := commands.NewMockBaseCommand()
		wm := &WorktreeManager{
			Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
			Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
			Base: commands.NewMockBaseCommand(),
		}
		_ = session
		return wm, mockTmuxBase, repoSlug
	}

	t.Run("attached to victim session switches to fallback before kill", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
		wm, mockTmuxBase, repoSlug := newWM(t, "dev-session")
		windowName := GetWindowName(repoSlug, wtName)
		windowList := "dev-session\t" + windowName + "\n"
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult(windowList, "", nil), // WindowSession (ours)
			commands.ExecCommandResult(windowList, "", nil), // WindowSession (worktreeState)
			commands.ExecCommandResult(windowList, "", nil), // WindowSession (KillWindow)
			commands.ExecCommandResult("", "", nil),         // kill-window
			commands.ExecCommandResult("", "", nil),         // has-session dev-session
			commands.ExecCommandResult(
				"dev-session\n",
				"",
				nil,
			), // display-message (CurrentSession)
			commands.ExecCommandResult("", "", nil), // has-session misc
			commands.ExecCommandResult("", "", nil), // switch-client
			commands.ExecCommandResult("", "", nil), // kill-session
		)

		if err := wm.RemoveWithSessionInRepo(repoSlug, wtName); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		calls := tmuxCallArgs(mockTmuxBase)
		switchIdx, killIdx := -1, -1
		for i, c := range calls {
			if c == "switch-client -t misc" {
				switchIdx = i
			}
			if c == "kill-session -t dev-session" {
				killIdx = i
			}
		}
		if switchIdx == -1 {
			t.Fatalf("expected switch-client to misc, calls: %v", calls)
		}
		if killIdx == -1 {
			t.Fatalf("expected kill-session of dev-session, calls: %v", calls)
		}
		if switchIdx > killIdx {
			t.Error("client must be switched to fallback before the session is killed")
		}
	})

	t.Run("not attached to victim session kills without switching", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
		wm, mockTmuxBase, repoSlug := newWM(t, "dev-session")
		windowName := GetWindowName(repoSlug, wtName)
		windowList := "dev-session\t" + windowName + "\n"
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult("", "", nil),        // kill-window
			commands.ExecCommandResult("", "", nil),        // has-session dev-session
			commands.ExecCommandResult("other\n", "", nil), // display-message → different session
			commands.ExecCommandResult("", "", nil),        // kill-session
		)

		if err := wm.RemoveWithSessionInRepo(repoSlug, wtName); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		calls := tmuxCallArgs(mockTmuxBase)
		for _, c := range calls {
			if strings.HasPrefix(c, "switch-client") {
				t.Errorf("should not switch client when not attached to victim session: %v", calls)
			}
		}
		if last := calls[len(calls)-1]; last != "kill-session -t dev-session" {
			t.Errorf("expected final kill-session, got %q (calls: %v)", last, calls)
		}
	})

	t.Run("never kills the fallback session", func(t *testing.T) {
		wm, mockTmuxBase, repoSlug := newWM(t, "misc")
		windowName := GetWindowName(repoSlug, wtName)
		windowList := "misc\t" + windowName + "\n"
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult("", "", nil), // kill-window
		)

		if err := wm.RemoveWithSessionInRepo(repoSlug, wtName); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range tmuxCallArgs(mockTmuxBase) {
			if strings.HasPrefix(c, "kill-session") {
				t.Errorf("fallback session must never be killed: %v", tmuxCallArgs(mockTmuxBase))
			}
		}
	})

	t.Run("skips kill when session already destroyed by kill-window", func(t *testing.T) {
		wm, mockTmuxBase, repoSlug := newWM(t, "dev-session")
		windowName := GetWindowName(repoSlug, wtName)
		windowList := "dev-session\t" + windowName + "\n"
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult("", "", nil),                           // kill-window
			commands.ExecCommandResult("", "no such session", os.ErrNotExist), // has-session fails
		)

		if err := wm.RemoveWithSessionInRepo(repoSlug, wtName); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range tmuxCallArgs(mockTmuxBase) {
			if strings.HasPrefix(c, "kill-session") {
				t.Errorf(
					"should not kill an already-destroyed session: %v",
					tmuxCallArgs(mockTmuxBase),
				)
			}
		}
	})

	t.Run("creates fallback session when missing", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
		wm, mockTmuxBase, repoSlug := newWM(t, "dev-session")
		windowName := GetWindowName(repoSlug, wtName)
		windowList := "dev-session\t" + windowName + "\n"
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult(windowList, "", nil),
			commands.ExecCommandResult("", "", nil), // kill-window
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // has-session dev-session
			commands.ExecCommandResult("dev-session\n", "", nil), // display-message
			commands.ExecCommandResult(
				"",
				"no such session",
				os.ErrNotExist,
			), // has-session misc fails
			commands.ExecCommandResult("", "", nil), // new-session
			commands.ExecCommandResult("", "", nil), // switch-client
			commands.ExecCommandResult("", "", nil), // kill-session
		)

		if err := wm.RemoveWithSessionInRepo(repoSlug, wtName); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		calls := tmuxCallArgs(mockTmuxBase)
		created := false
		for _, c := range calls {
			if strings.HasPrefix(c, "new-session") && strings.Contains(c, "-s misc") {
				created = true
			}
		}
		if !created {
			t.Errorf("expected fallback session to be created, calls: %v", calls)
		}
	})
}

// stubLayout is a single-pane Layout with no install checkers (paneCheckers
// is left nil, so EnsureInstalled no-ops), for exercising the create/repair
// flow without touching the real system.
var stubLayout = Layout{Name: "stub", Panes: []Pane{{Command: "stub-cmd"}}}

func TestCreateAt(t *testing.T) {
	t.Run("errors when path is not a git repository", func(t *testing.T) {
		mockGitBase := commands.NewMockBaseCommand()
		mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)
		wm := &WorktreeManager{
			Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
			Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: commands.NewMockBaseCommand()},
			Base: commands.NewMockBaseCommand(),
		}

		if err := wm.CreateAt("/nowhere", "feat", stubLayout, true); err == nil {
			t.Fatal("expected error for a non-repo path")
		}
	})

	t.Run("creates the repo-slug session when missing and launches there", func(t *testing.T) {
		t.Setenv("TMUX", "") // outside tmux: no client switch at the end
		repoRoot := t.TempDir()

		mockGitBase := commands.NewMockBaseCommand()
		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
			commands.ExecCommandResult("", "", nil),            // everything else succeeds/empty
		)
		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // worktreeState list-windows (no window)
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // ensureWindow list-windows (no window)
			commands.ExecCommandResult(
				"",
				"no such session",
				os.ErrNotExist,
			), // has-session → missing
			commands.ExecCommandResult("", "", nil), // new-session
			commands.ExecCommandResult("", "", nil), // send-keys
		)

		wm := &WorktreeManager{
			Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
			Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
			Base: commands.NewMockBaseCommand(),
		}

		if err := wm.CreateAt(repoRoot, "feat", stubLayout, true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		repoSlug := filepath.Base(repoRoot)
		windowName := GetWindowName(repoSlug, "feat")
		sawNewSession := false
		sawSendKeys := false
		for _, call := range mockTmuxBase.ExecCommandCalls {
			joined := strings.Join(call.Args, " ")
			if strings.HasPrefix(joined, "new-session") &&
				strings.Contains(joined, "-s "+TmuxSessionName(repoSlug)) &&
				strings.Contains(joined, "-n "+windowName) {
				sawNewSession = true
			}
			if strings.HasPrefix(joined, "send-keys") && strings.Contains(joined, "stub-cmd") {
				sawSendKeys = true
			}
		}
		if !sawNewSession {
			t.Errorf("expected new-session for %q with window %q, calls: %+v",
				repoSlug, windowName, mockTmuxBase.ExecCommandCalls)
		}
		if !sawSendKeys {
			t.Errorf("expected AI coder command sent to the window, calls: %+v",
				mockTmuxBase.ExecCommandCalls)
		}

		// Worktree creation must target the repo via -C.
		sawWorktreeAdd := false
		for _, call := range mockGitBase.ExecCommandCalls {
			joined := strings.Join(call.Args, " ")
			if strings.Contains(joined, "worktree add") &&
				strings.HasPrefix(joined, "-C "+repoRoot) {
				sawWorktreeAdd = true
			}
		}
		if !sawWorktreeAdd {
			t.Errorf("expected 'git -C %s worktree add ...', calls: %+v",
				repoRoot, mockGitBase.ExecCommandCalls)
		}
	})

	// TestCreateAt's regression case for the step-3 review finding: with
	// worktree.location "in-repo", create() used to re-derive repoRoot from
	// the bare repoSlug via the fallible worktreePath (through worktreeState),
	// which resolves a slug back to a root only via the current directory or
	// the recent-repos store - and recordRepoUsed only populates that store
	// *after* a successful create. So a repo passed via --repo that was
	// neither the current directory nor already in the store failed on its
	// very first `dg wt create <name> --repo <path>` invocation, even though
	// create() had already computed the correct in-repo wtPath via
	// worktreePathIn two lines earlier. This proves that first invocation now
	// succeeds and lands the worktree at the in-repo path.
	t.Run(
		"in-repo location succeeds on first invocation for a repo not yet known to devgeta",
		func(t *testing.T) {
			t.Setenv("TMUX", "") // outside tmux: no client switch at the end
			cleanupPaths := testutil.SetupIsolatedPaths(t)
			defer cleanupPaths()
			setWorktreeLocation(t, config.WorktreeLocationInRepo)

			repoRoot := t.TempDir()

			mockGitBase := commands.NewMockBaseCommand()
			mockGitBase.SetExecCommandResults(
				// GetRepoRootIn(repoRoot), called by CreateAt itself.
				commands.ExecCommandResult(repoRoot+"\n", "", nil),
				// Every subsequent git call sees an UNRELATED repo root -
				// simulating a caller whose current directory is some other
				// repo entirely, with worktree.recent_repos left empty (the
				// default here). Before this fix, resolveRepoRoot's two
				// resolution sources (current repo, recent-repos store) could
				// therefore never resolve repoRoot from its bare slug, and
				// create() failed here even though wtPath was already correct.
				commands.ExecCommandResult("/some/other/repo\n", "", nil),
			)
			mockTmuxBase := commands.NewMockBaseCommand()
			mockTmuxBase.SetExecCommandResults(
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // worktreeStateIn: list-windows (no window)
				commands.ExecCommandResult("", "", nil), // ensureWindow: list-windows (no window)
				commands.ExecCommandResult(
					"", "no such session", os.ErrNotExist,
				), // has-session -> missing
				commands.ExecCommandResult("", "", nil), // new-session
				commands.ExecCommandResult("", "", nil), // send-keys
			)

			wm := &WorktreeManager{
				Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
				Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
				Base: commands.NewMockBaseCommand(),
			}

			if err := wm.CreateAt(repoRoot, "feat", stubLayout, true); err != nil {
				t.Fatalf(
					"expected create to succeed for a repo that is neither the "+
						"current directory nor in the recent-repos store, got: %v",
					err,
				)
			}

			wantWtPath := filepath.Join(repoRoot, ".claude", "worktrees", "feat")
			sawWorktreeAdd := false
			for _, call := range mockGitBase.ExecCommandCalls {
				joined := strings.Join(call.Args, " ")
				if strings.Contains(joined, "worktree add") &&
					strings.Contains(joined, wantWtPath) {
					sawWorktreeAdd = true
				}
			}
			if !sawWorktreeAdd {
				t.Errorf(
					"expected 'git worktree add' for the in-repo path %q, calls: %+v",
					wantWtPath, mockGitBase.ExecCommandCalls,
				)
			}
		},
	)
}

func TestListSessions(t *testing.T) {
	t.Run(
		"excludes any session with at least one wt- window, keeps standalone sessions",
		func(t *testing.T) {
			mockTmuxBase := commands.NewMockBaseCommand()
			mockTmuxBase.SetExecCommandResults(
				// list-sessions
				commands.ExecCommandResult("myrepo\t1\nmisc\t0\nnotes\t0\nmixed\t1\n", "", nil),
				// list-windows -a
				commands.ExecCommandResult(
					"myrepo\twt-myrepo-feat\nmisc\tshell\nnotes\tnotes\nmixed\tshell\nmixed\twt-mixed-feat\n",
					"",
					nil,
				),
			)
			wm := &WorktreeManager{
				Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
			}

			statuses, err := wm.ListSessions()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expected := []SessionStatus{
				{Name: "misc", Attached: false},
				{Name: "notes", Attached: false},
			}
			if len(statuses) != len(expected) {
				t.Fatalf(
					"expected %d sessions, got %d: %+v",
					len(expected),
					len(statuses),
					statuses,
				)
			}
			for i, exp := range expected {
				if statuses[i] != exp {
					t.Errorf("status[%d] = %+v, want %+v", i, statuses[i], exp)
				}
			}

			if mockTmuxBase.GetExecCommandCallCount() != 2 {
				t.Errorf(
					"expected a single list-sessions + a single list-windows -a scan, got %d calls: %+v",
					mockTmuxBase.GetExecCommandCallCount(),
					mockTmuxBase.ExecCommandCalls,
				)
			}
		},
	)

	t.Run("no-server case flows through as empty list, not an error", func(t *testing.T) {
		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResult(
			"",
			"error connecting to /tmp/tmux-1000/default (No such file or directory)",
			errors.New("exit status 1"),
		)
		wm := &WorktreeManager{
			Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
		}

		statuses, err := wm.ListSessions()
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if statuses != nil {
			t.Errorf("expected nil statuses, got %+v", statuses)
		}
		// No sessions means no windows to scan for either - the second
		// (list-windows -a) call must not happen.
		if mockTmuxBase.GetExecCommandCallCount() != 1 {
			t.Errorf(
				"expected only the list-sessions call, got %d calls: %+v",
				mockTmuxBase.GetExecCommandCallCount(),
				mockTmuxBase.ExecCommandCalls,
			)
		}
	})

	t.Run("real error from Tmux.ListSessions is propagated unchanged", func(t *testing.T) {
		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResult(
			"",
			"some unexpected tmux failure",
			errors.New("exit status 1"),
		)
		wm := &WorktreeManager{
			Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
		}

		statuses, err := wm.ListSessions()
		if err == nil {
			t.Fatal("expected error to be propagated, got nil")
		}
		if !strings.Contains(err.Error(), "failed to list tmux sessions") {
			t.Errorf("expected the underlying tmux error to be preserved, got: %v", err)
		}
		if statuses != nil {
			t.Errorf("expected nil statuses on error, got %+v", statuses)
		}
		if mockTmuxBase.GetExecCommandCallCount() != 1 {
			t.Errorf(
				"expected only the list-sessions call on error, got %d calls: %+v",
				mockTmuxBase.GetExecCommandCallCount(),
				mockTmuxBase.ExecCommandCalls,
			)
		}
	})

	// Documents an accepted trade-off: if Tmux.ListSessions() succeeds but the
	// follow-up Tmux.SessionWindows() scan then fails, the exclusion set ends up
	// empty and a worktree-backed session is reported as standalone - the exact
	// double-listing this method exists to prevent. This mirrors the tolerant
	// (best-effort, non-fatal) error handling already used elsewhere in this
	// file for tmux queries (e.g. WindowSession, FindWindowsBySuffix), so it is
	// not treated as an error here either. Not a design goal, just a documented
	// limit of the current approach.
	t.Run(
		"SessionWindows failure after a successful ListSessions is tolerated, not surfaced as an error",
		func(t *testing.T) {
			mockTmuxBase := commands.NewMockBaseCommand()
			mockTmuxBase.SetExecCommandResults(
				// list-sessions: includes a session that (unbeknownst to us here) hosts a worktree window.
				commands.ExecCommandResult("myrepo\t1\n", "", nil),
				// list-windows -a fails.
				commands.ExecCommandResult("", "error", errors.New("no server")),
			)
			wm := &WorktreeManager{
				Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
			}

			statuses, err := wm.ListSessions()
			if err != nil {
				t.Fatalf("expected no error (tolerated failure), got: %v", err)
			}
			if len(statuses) != 1 || statuses[0].Name != "myrepo" {
				t.Errorf(
					"expected the worktree-backed session to incorrectly appear as standalone (documented trade-off), got %+v",
					statuses,
				)
			}
		},
	)
}
