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
	"github.com/cjairm/devgeta/pkg/files"
)

// multiFixture is a mocked manager over two fake repos. gitFn lets a test fail
// specific git calls; worktree add creates the target directory so rollback has
// something on disk to find.
type multiFixture struct {
	wm       *WorktreeManager
	gitBase  *commands.MockBaseCommand
	tmuxBase *commands.MockBaseCommand
	repoA    string
	repoB    string
	branches map[string]bool // repo root -> branch exists (adopted, or made by worktree add)
	failAdd  string          // repo root whose `worktree add` fails after creating the dir
	failWin  string          // substring of a tmux window name whose creation fails
}

func newMultiFixture(t *testing.T) *multiFixture {
	t.Helper()
	t.Setenv("TMUX", "")
	f := &multiFixture{
		gitBase:  commands.NewMockBaseCommand(),
		tmuxBase: commands.NewMockBaseCommand(),
		repoA:    filepath.Join(t.TempDir(), "api"),
		repoB:    filepath.Join(t.TempDir(), "web"),
		branches: map[string]bool{},
	}
	// The worktree dirs land in the shared test sandbox, so remove them or the
	// next test sees an "existing worktree".
	t.Cleanup(func() {
		for _, root := range []string{f.repoA, f.repoB} {
			if err := os.RemoveAll(worktreePathIn(root, "fix-auth")); err != nil {
				t.Logf("cleanup: %v", err)
			}
		}
	})
	f.gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case strings.Contains(joined, "--show-toplevel"):
			return c.Args[1] + "\n", "", nil
		case strings.Contains(joined, "worktree list --porcelain"):
			// Rollback asks for the main worktree of the path it removes.
			for _, root := range []string{f.repoA, f.repoB} {
				if strings.Contains(c.Args[1], string(filepath.Separator)+filepath.Base(root)+string(filepath.Separator)) {
					return "worktree " + root + "\n", "", nil
				}
			}
		case strings.Contains(joined, "branch --list"):
			for root := range f.branches {
				if strings.HasPrefix(joined, "-C "+root+" ") {
					return "  branch\n", "", nil
				}
			}
		case strings.Contains(joined, "worktree add"):
			for _, root := range []string{f.repoA, f.repoB} {
				if strings.HasPrefix(joined, "-C "+root+" ") {
					_ = os.MkdirAll(worktreePathIn(root, "fix-auth"), 0o755)
					f.branches[root] = true
					if root == f.failAdd {
						return "", "boom", errors.New("add failed")
					}
				}
			}
		}
		return "", "", nil
	}
	f.tmuxBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		joined := strings.Join(c.Args, " ")
		if f.failWin != "" && strings.Contains(joined, f.failWin) &&
			(strings.HasPrefix(joined, "new-window") || strings.HasPrefix(joined, "new-session")) {
			return "", "no server", errors.New("tmux failed")
		}
		if strings.HasPrefix(joined, "has-session") {
			return "", "no session", os.ErrNotExist
		}
		return "", "", nil
	}
	f.wm = &WorktreeManager{
		Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: f.gitBase},
		Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: f.tmuxBase},
		Base: commands.NewMockBaseCommand(),
	}
	return f
}

func (f *multiFixture) gitCalls(substr string) []string {
	var out []string
	for _, c := range f.gitBase.ExecCommandCalls {
		if j := strings.Join(c.Args, " "); strings.Contains(j, substr) {
			out = append(out, j)
		}
	}
	return out
}

func TestCreateManySucceedsInEveryRepo(t *testing.T) {
	f := newMultiFixture(t)

	got, err := f.wm.CreateMany([]string{f.repoA, f.repoB}, "fix-auth", "", stubLayout, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].Repo != "api" || got[1].Repo != "web" {
		t.Fatalf("unexpected results: %+v", got)
	}
	for _, r := range got {
		if r.Window == "" || r.Branch != "fix-auth" {
			t.Errorf("expected a window and branch for %s, got %+v", r.Repo, r)
		}
	}
	if n := len(f.gitCalls("worktree add")); n != 2 {
		t.Errorf("expected 2 worktree add calls, got %d", n)
	}
}

func TestCreateManyRollsBackWhenSecondRepoFails(t *testing.T) {
	f := newMultiFixture(t)
	f.failAdd = f.repoB

	_, err := f.wm.CreateMany([]string{f.repoA, f.repoB}, "fix-auth", "", stubLayout, true, false)
	if err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("expected an error naming the failing repo, got %v", err)
	}
	removals := f.gitCalls("worktree remove")
	if len(removals) != 2 {
		t.Fatalf("expected both worktrees removed (the failing repo's partial one too), got %v", removals)
	}
	if n := len(f.gitCalls("branch -D fix-auth")); n != 2 {
		t.Errorf("expected both created branches deleted, got %d", n)
	}
	for _, c := range f.tmuxBase.ExecCommandCalls {
		if strings.HasPrefix(strings.Join(c.Args, " "), "new-") {
			t.Errorf("phase 1 failure must launch nothing, saw %v", c.Args)
		}
	}
}

func TestCreateManyRollbackKeepsAdoptedBranch(t *testing.T) {
	f := newMultiFixture(t)
	f.branches[f.repoB] = true
	f.failAdd = f.repoB

	_, err := f.wm.CreateMany([]string{f.repoA, f.repoB}, "fix-auth", "", stubLayout, true, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	deletes := f.gitCalls("branch -D fix-auth")
	if len(deletes) != 1 || !strings.HasPrefix(deletes[0], "-C "+f.repoA+" ") {
		t.Fatalf("expected exactly one branch delete (repo api's), got %v", deletes)
	}
}

func TestCreateManyReportsRollbackFailureWithSurvivingPath(t *testing.T) {
	f := newMultiFixture(t)
	f.failAdd = f.repoB
	inner := f.gitBase.ExecCommandFn
	f.gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if strings.Contains(strings.Join(c.Args, " "), "worktree remove") {
			return "", "locked", errors.New("remove failed")
		}
		return inner(c)
	}

	_, err := f.wm.CreateMany([]string{f.repoA, f.repoB}, "fix-auth", "", stubLayout, true, false)
	if err == nil || !strings.Contains(err.Error(), "still on disk") ||
		!strings.Contains(err.Error(), worktreePathIn(f.repoA, "fix-auth")) {
		t.Fatalf("expected the surviving path in the error, got %v", err)
	}
}

func TestCreateManyWindowFailureKeepsEveryWorktree(t *testing.T) {
	f := newMultiFixture(t)
	f.failWin = "wt-api-fix-auth"

	got, err := f.wm.CreateMany([]string{f.repoA, f.repoB}, "fix-auth", "", stubLayout, true, false)
	if err == nil || !strings.Contains(err.Error(), "wt-api-fix-auth") ||
		!strings.Contains(err.Error(), "devgeta worktree repair fix-auth") {
		t.Fatalf("expected the failed window and repair hint, got %v", err)
	}
	if len(got) != 2 || got[0].Window != "" || got[1].Window == "" {
		t.Errorf("expected only the web window to launch, got %+v", got)
	}
	if n := len(f.gitCalls("worktree remove")); n != 0 {
		t.Errorf("a launch failure must not remove worktrees, saw %d removals", n)
	}
}

func TestCreateManyRejectsDuplicateRepo(t *testing.T) {
	f := newMultiFixture(t)
	_, err := f.wm.CreateMany([]string{f.repoA, f.repoA}, "fix-auth", "", stubLayout, true, false)
	if err == nil || !strings.Contains(err.Error(), "same repository") {
		t.Fatalf("expected a duplicate error, got %v", err)
	}
	if n := len(f.gitCalls("worktree add")); n != 0 {
		t.Errorf("nothing may be created when validation fails, saw %d adds", n)
	}
}

func TestCreateManyBaseWithExistingBranchFailsBeforeCreating(t *testing.T) {
	f := newMultiFixture(t)
	f.branches[f.repoB] = true
	_, err := f.wm.CreateMany([]string{f.repoA, f.repoB}, "fix-auth", "main", stubLayout, true, false)
	if err == nil || !strings.Contains(err.Error(), "--base") {
		t.Fatalf("expected a --base error, got %v", err)
	}
	if n := len(f.gitCalls("worktree add")); n != 0 {
		t.Errorf("repo api must not be created when web fails validation, saw %d adds", n)
	}
}

func TestCreateManyNoWindowLaunchesNothing(t *testing.T) {
	f := newMultiFixture(t)
	got, err := f.wm.CreateMany([]string{f.repoA}, "fix-auth", "", Layout{}, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Window != "" {
		t.Errorf("unexpected results: %+v", got)
	}
	for _, c := range f.tmuxBase.ExecCommandCalls {
		if strings.HasPrefix(strings.Join(c.Args, " "), "new-") {
			t.Errorf("--no-window must create no window, saw %v", c.Args)
		}
	}
}

func TestResolveAICoderPi(t *testing.T) {
	c, err := ResolveAICoder("pi")
	if err != nil || c.Name() != "pi" {
		t.Fatalf("expected the pi coder, got %v, %v", c, err)
	}
	if got := c.PromptCommand("fix it"); got != "pi 'fix it'" {
		t.Errorf("unexpected prompt command %q", got)
	}
	if _, err := ResolveLayout("pi", "", nil); err != nil {
		t.Errorf("pi layout should resolve: %v", err)
	}
}

// Repo dirs "api.app" and "api_app" sanitize to the same tmux window name, and
// two repos both named "api" share one shared-location worktree path. Either
// collision would make the second repo reuse the first one's window or dir.
func TestCreateManyRejectsCollidingWindowOrPath(t *testing.T) {
	for _, pair := range [][2]string{{"api.app", "api_app"}, {"one/api", "two/api"}} {
		t.Run(pair[0], func(t *testing.T) {
			f := newMultiFixture(t)
			base := t.TempDir()
			f.repoA, f.repoB = filepath.Join(base, pair[0]), filepath.Join(base, pair[1])
			_, err := f.wm.CreateMany([]string{f.repoA, f.repoB}, "fix-auth", "", stubLayout, true, false)
			if err == nil || !strings.Contains(err.Error(), "collide") {
				t.Fatalf("expected a collision error, got %v", err)
			}
			if n := len(f.gitCalls("worktree add")); n != 0 {
				t.Errorf("nothing may be created, saw %d adds", n)
			}
		})
	}
}

// Git can create the branch, fail the checkout, and remove the worktree dir
// itself. Rollback must still delete the branch this call created.
func TestCreateManyRollbackDeletesBranchWhenDirIsGone(t *testing.T) {
	f := newMultiFixture(t)
	inner := f.gitBase.ExecCommandFn
	failed := false
	f.gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		joined := strings.Join(c.Args, " ")
		if strings.HasPrefix(joined, "-C "+f.repoB+" ") {
			if strings.Contains(joined, "worktree add") {
				failed = true
				return "", "filter failed", errors.New("checkout failed")
			}
			if failed && strings.Contains(joined, "branch --list") {
				return "  fix-auth\n", "", nil // the branch git left behind
			}
		}
		return inner(c)
	}

	_, err := f.wm.CreateMany([]string{f.repoA, f.repoB}, "fix-auth", "", stubLayout, true, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	var sawWebDelete bool
	for _, d := range f.gitCalls("branch -D fix-auth") {
		if strings.HasPrefix(d, "-C "+f.repoB+" ") {
			sawWebDelete = true
		}
	}
	if !sawWebDelete {
		t.Errorf("expected web's created branch deleted, got %v", f.gitCalls("branch -D"))
	}
}

// Two overlapping creates for the same repo and branch must not both pass
// validation: the loser's rollback would treat the winner's worktree and branch
// as its own. While another holder has the lock, CreateMany waits, then fails
// having created nothing.
func TestCreateManyWaitsForConcurrentCreateOfSameBranch(t *testing.T) {
	f := newMultiFixture(t)
	old := createLockTimeout
	createLockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { createLockTimeout = old })

	err := files.WithLock(createLockPath(f.repoB, "fix-auth"), time.Second, func() error {
		_, err := f.wm.CreateMany([]string{f.repoA, f.repoB}, "fix-auth", "", stubLayout, true, false)
		return err
	})
	if err == nil {
		t.Fatal("expected CreateMany to fail while another create holds the lock")
	}
	if n := len(f.gitCalls("worktree add")); n != 0 {
		t.Errorf("nothing may be created without the lock, saw %d adds", n)
	}
}

// "feat/auth" and "feat-auth" are different branches but flatten to the same
// worktree directory, so the destination must be locked too, not only the
// branch: otherwise the loser's rollback removes the winner's checkout.
func TestCreateManyWaitsForConcurrentCreateOfSameDirectory(t *testing.T) {
	f := newMultiFixture(t)
	old := createLockTimeout
	createLockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { createLockTimeout = old })

	held := createPathLockPath(worktreePathIn(f.repoA, "fix/auth"))
	err := files.WithLock(held, time.Second, func() error {
		_, err := f.wm.CreateMany([]string{f.repoA}, "fix-auth", "", stubLayout, true, false)
		return err
	})
	if err == nil {
		t.Fatal("expected CreateMany to fail while another create holds the same directory")
	}
	if n := len(f.gitCalls("worktree add")); n != 0 {
		t.Errorf("nothing may be created without the lock, saw %d adds", n)
	}
}
