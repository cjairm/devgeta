package worktree

// Regression suite for the "ghost worktree" family of bugs: a worktree whose
// directory was deleted, or which lives at a path devgeta's configured
// location shapes cannot produce, used to be listed forever and could not be
// removed.
//
// Three defects combined to produce it, and each has a test here:
//
//  1. parseWorktreeOutput dropped git's "prunable" marker, so enumerateWorktrees
//     turned administrative debris into dashboard rows. Every diff against one
//     failed with "cannot change to '<path>': No such file or directory".
//  2. Mutations recomputed the worktree path from the CONFIGURED
//     worktree.location instead of reading it from git, so a worktree at any
//     other path (an older data-directory name, a hand-made worktree, one moved
//     with plain `git worktree move`) was invisible to remove/repair — and
//     removeByRepo returned a bare nil for "found nothing", reporting success
//     while doing nothing. The row came straight back on the next refresh.
//  3. Every prune ran from filepath.Dir(wtPath) — the worktree's parent — which
//     for the shared shape is not a git repository at all, so the prune failed
//     and the error was discarded. The stale entry survived every removal.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

// stalePorcelain builds `git worktree list --porcelain` output where every
// path in stale carries git's prunable marker and every path in live does
// not — the exact mixture the real repro had (two dead registrations left by
// an older data-directory name, alongside one healthy worktree).
func stalePorcelain(mainRoot string, live []string, stale []string) string {
	var b strings.Builder
	b.WriteString("worktree " + mainRoot + "\n")
	b.WriteString("HEAD 0000000000000000000000000000000000000000\n")
	b.WriteString("branch refs/heads/main\n\n")
	for _, p := range live {
		b.WriteString("worktree " + p + "\n")
		b.WriteString("HEAD 1111111111111111111111111111111111111111\n")
		b.WriteString("branch refs/heads/" + filepath.Base(p) + "\n\n")
	}
	for _, p := range stale {
		b.WriteString("worktree " + p + "\n")
		b.WriteString("HEAD 2222222222222222222222222222222222222222\n")
		b.WriteString("branch refs/heads/" + filepath.Base(p) + "\n")
		b.WriteString("prunable gitdir file points to non-existent location\n\n")
	}
	return b.String()
}

// gitCallsContaining returns every recorded git call whose args include arg —
// used to assert a prune actually happened, and to check what directory it
// was anchored at.
func gitCallsContaining(
	mockBase *commands.MockBaseCommand,
	arg string,
) []commands.CommandParams {
	var found []commands.CommandParams
	for _, c := range mockBase.ExecCommandCalls {
		for _, a := range c.Args {
			if a == arg {
				found = append(found, c)
				break
			}
		}
	}
	return found
}

// dirArgOf returns the value following "-C" in a recorded call, i.e. the
// directory git was run from. Empty when the call had no -C.
func dirArgOf(call commands.CommandParams) string {
	for i, a := range call.Args {
		if a == "-C" && i+1 < len(call.Args) {
			return call.Args[i+1]
		}
	}
	return ""
}

// TestEnumerateWorktreesSkipsPrunable is the direct regression test for the
// ghost rows: git reports three registrations, two of them prunable, and only
// the healthy one may become a row. Before the fix all three appeared, and
// the two dead ones failed their diff on every 3-second dashboard refresh.
func TestEnumerateWorktreesSkipsPrunable(t *testing.T) {
	t.Run("a prunable registration never becomes a row", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		t.Chdir(t.TempDir())

		mainRoot := filepath.Join(t.TempDir(), "taskqueue2")
		livePath := createWorktreeDir(t, "taskqueue2", "CST-7172")
		// The real repro's dead paths: an older data-directory name that no
		// longer exists anywhere on disk.
		staleA := "/Users/someone/.local/share/oldname/worktrees/taskqueue2/cst-8621-gone"
		staleB := "/Users/someone/.local/share/oldname/worktrees/taskqueue2/feat-cxe-105-gone"

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist),
			commands.ExecCommandResult(
				stalePorcelain(mainRoot, []string{livePath}, []string{staleA, staleB}),
				"", nil,
			),
		)

		statuses := wm.enumerateWorktrees()
		if len(statuses) != 1 {
			t.Fatalf(
				"expected only the healthy worktree, got %d rows: %+v",
				len(statuses), statuses,
			)
		}
		if statuses[0].Path != livePath {
			t.Errorf("surviving row is %q, want the healthy worktree %q",
				statuses[0].Path, livePath)
		}
		for _, s := range statuses {
			if strings.Contains(s.Path, "oldname") {
				t.Errorf("a stale registration leaked into the dashboard as %q", s.Path)
			}
		}
	})

	t.Run("a repo whose every worktree is stale produces no rows at all", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		t.Chdir(t.TempDir())

		mainRoot := filepath.Join(t.TempDir(), "allgone")
		// One real directory is still needed as a shared-root anchor for
		// forEachKnownRepo to have something to query git about.
		anchor := createWorktreeDir(t, "allgone", "anchor")

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist),
			commands.ExecCommandResult(
				stalePorcelain(mainRoot, nil, []string{anchor}),
				"", nil,
			),
		)

		if statuses := wm.enumerateWorktrees(); len(statuses) != 0 {
			t.Fatalf("expected no rows when every registration is stale, got: %+v", statuses)
		}
	})
}

// TestGitWorktreePathResolvesAnyLocation covers the split that made removal a
// no-op: List() read the path from git while every mutation recomputed it
// from the configured location. A worktree at neither configured shape (the
// real repro: a path under a previous data-directory name) has to resolve, or
// nothing can act on it.
func TestGitWorktreePathResolvesAnyLocation(t *testing.T) {
	t.Run("resolves a worktree at a path neither location shape produces", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		repoRoot := t.TempDir()
		const name = "feat-cxe-105"
		// Deliberately not sharedWorktreePath and not inRepoWorktreePath.
		foreign := "/Users/someone/.local/share/oldname/worktrees/taskqueue2/" + name

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				stalePorcelain(repoRoot, []string{foreign}, nil), "", nil,
			),
		)

		got, ok := wm.gitWorktreePath(repoRoot, name)
		if !ok {
			t.Fatal("git reported the worktree but resolution failed — " +
				"this is the split that made `d` a silent no-op")
		}
		if got != foreign {
			t.Errorf("resolved to %q, want git's own answer %q", got, foreign)
		}
	})

	t.Run("a flattened name matches the directory git reports", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		repoRoot := t.TempDir()
		// A slash-bearing branch name is flattened on disk, so lookup must
		// flatten too or a `feat/x` worktree is unreachable by its own name.
		flat := FlattenName("feat/cxe-105-faceoff")
		wtPath := filepath.Join(repoRoot, ".claude", "worktrees", flat)

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				stalePorcelain(repoRoot, []string{wtPath}, nil), "", nil,
			),
		)

		got, ok := wm.gitWorktreePath(repoRoot, "feat/cxe-105-faceoff")
		if !ok || got != wtPath {
			t.Errorf("gitWorktreePath = (%q, %v), want (%q, true)", got, ok, wtPath)
		}
	})

	t.Run("never resolves to a prunable registration", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		repoRoot := t.TempDir()
		const name = "gone"
		ghost := "/Users/someone/.local/share/oldname/worktrees/repo/" + name

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				stalePorcelain(repoRoot, nil, []string{ghost}), "", nil,
			),
		)

		// Handing a mutation a path whose directory is gone only produces a
		// confusing downstream failure; the entry is debris to prune instead.
		if got, ok := wm.gitWorktreePath(repoRoot, name); ok {
			t.Errorf("resolved to a stale registration %q; it must be skipped", got)
		}
	})

	t.Run("never resolves to the repo's own main checkout", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		repoRoot := t.TempDir()

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				stalePorcelain(repoRoot, nil, nil), "", nil,
			),
		)

		if got, ok := wm.gitWorktreePath(repoRoot, filepath.Base(repoRoot)); ok {
			t.Errorf("resolved the main checkout %q as a worktree", got)
		}
	})

	t.Run("an unknown repo root is 'no answer', not 'no such worktree'", func(t *testing.T) {
		wm, _, _ := newListTestWM(t)
		if _, ok := wm.gitWorktreePath("", "anything"); ok {
			t.Error("empty repo root must not resolve")
		}
	})
}

// TestPruneStaleWorktreesAnchor pins the anchor bug. Running the prune from
// filepath.Dir(wtPath) meant, for the shared shape,
// `git -C ~/.local/share/devgeta/worktrees/<slug> worktree prune` — a
// directory that is not a git repository, so git refused and the discarded
// error hid it. The prune must run from the repo root.
func TestPruneStaleWorktreesAnchor(t *testing.T) {
	t.Run("prunes from the repo root", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		repoRoot := t.TempDir()

		mockGitBase.SetExecCommandResults(commands.ExecCommandResult("", "", nil))

		if err := wm.pruneStaleWorktrees(repoRoot); err != nil {
			t.Fatalf("pruneStaleWorktrees failed: %v", err)
		}

		calls := gitCallsContaining(mockGitBase, "prune")
		if len(calls) != 1 {
			t.Fatalf("expected exactly 1 prune call, got %d", len(calls))
		}
		if got := dirArgOf(calls[0]); got != repoRoot {
			t.Errorf("prune ran from %q, want the repo root %q — "+
				"a non-repo anchor is why pruning silently did nothing", got, repoRoot)
		}
	})

	t.Run("an unknown repo root is an error, not a silently skipped prune", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)

		if err := wm.pruneStaleWorktrees(""); err == nil {
			t.Fatal("expected an error when the repo root is unknown")
		}
		if n := mockGitBase.GetExecCommandCallCount(); n != 0 {
			t.Errorf("expected no git calls for an unknown root, got %d", n)
		}
	})
}

// TestStaleWorktreePaths covers the lookup removeByRepo uses to tell "this
// worktree is already gone, clean up the leftover entry" apart from "no such
// worktree at all" — the distinction that turns a silent no-op into either a
// real cleanup or an honest error.
func TestStaleWorktreePaths(t *testing.T) {
	t.Run("finds the stale registration matching the name", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		repoRoot := t.TempDir()
		const name = "cst-8621-gone"
		ghost := "/Users/someone/.local/share/oldname/worktrees/taskqueue2/" + name
		live := filepath.Join(repoRoot, ".claude", "worktrees", "still-here")

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				stalePorcelain(repoRoot, []string{live}, []string{ghost}), "", nil,
			),
		)

		got := wm.staleWorktreePaths(repoRoot, name)
		if len(got) != 1 || got[0] != ghost {
			t.Fatalf("staleWorktreePaths = %v, want [%s]", got, ghost)
		}
	})

	t.Run("a healthy worktree is never reported as stale", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		repoRoot := t.TempDir()
		const name = "still-here"
		live := filepath.Join(repoRoot, ".claude", "worktrees", name)

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				stalePorcelain(repoRoot, []string{live}, nil), "", nil,
			),
		)

		if got := wm.staleWorktreePaths(repoRoot, name); len(got) != 0 {
			t.Errorf("healthy worktree reported as stale: %v", got)
		}
	})
}

// TestRemoveByRepoNeverSilentlySucceeds is the top-level regression test for
// the reported bug: pressing `d` on a worktree the mutation path could not
// resolve returned nil, so the dashboard reported success, pruned nothing,
// and re-drew the same row on the next refresh. "Found nothing" must never
// be reported as a successful removal.
func TestRemoveByRepoNeverSilentlySucceeds(t *testing.T) {
	t.Run("errors, naming the worktree, when there is nothing to remove", func(t *testing.T) {
		wm, mockGitBase, mockTmuxBase := newListTestWM(t)
		t.Chdir(t.TempDir())
		mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)
		mockTmuxBase.SetExecCommandResult("", "no server running", os.ErrNotExist)

		err := wm.removeByRepo("some-repo", "not-a-worktree", true)
		if err == nil {
			t.Fatal("removal of a nonexistent worktree reported success — " +
				"this is exactly what made a ghost row unkillable")
		}
		if !strings.Contains(err.Error(), "not-a-worktree") {
			t.Errorf("error must name the worktree, got: %v", err)
		}
	})
}

// TestSetWarnFnCoversBothLayers guards the display-corruption half of the
// bug. The dashboard overrode only the manager's WarnFn, leaving the git app
// underneath it still defaulting to a raw stdout print. When a create hit a
// diverged branch, git printed straight into the running alt-screen: the
// frame scrolled, and the previous frame's rows stayed on screen underneath
// the new ones — which is what made the dashboard appear to show duplicated
// and nested worktrees that did not exist.
func TestSetWarnFnCoversBothLayers(t *testing.T) {
	t.Run("installs the sink on the manager and the git app together", func(t *testing.T) {
		wm, _, _ := newListTestWM(t)

		var got []string
		wm.SetWarnFn(func(msg string) { got = append(got, msg) })

		if wm.WarnFn == nil {
			t.Fatal("manager WarnFn was not set")
		}
		if wm.Git.WarnFn == nil {
			t.Fatal("git WarnFn was not set — git advisories would still print " +
				"raw to stdout underneath a running TUI")
		}

		wm.WarnFn("from the manager")
		wm.Git.WarnFn("from git")
		if len(got) != 2 {
			t.Fatalf("expected both layers to reach the same sink, got %v", got)
		}
	})

	t.Run("New leaves both layers with a non-nil default", func(t *testing.T) {
		wm := New()
		if wm.WarnFn == nil {
			t.Error("manager WarnFn default is nil")
		}
		if wm.Git.WarnFn == nil {
			t.Error("git WarnFn default is nil")
		}
	})
}

// TestIsStaleRegistrationError covers the last way a ghost entry can bite:
// after the directory is gone, git still holds the worktree path AND the
// branch, so `dg wt new <same-name>` fails. create() recovers by pruning and
// retrying once — but only for the error phrasings it recognizes, so a
// missed phrasing leaves that name permanently unusable.
func TestIsStaleRegistrationError(t *testing.T) {
	staleMessages := []string{
		"failed to create worktree: git: fatal: '/wt/x' is a missing but already registered worktree directory",
		"git: fatal: 'feat/cxe-105' is already used by worktree at '/gone/wt'",
		"git: fatal: 'CST-7172' is already checked out at '/gone/wt'",
		"git: fatal: '/wt/x' already exists and is not an empty directory",
	}
	for _, msg := range staleMessages {
		if !isStaleRegistrationError(errors.New(msg)) {
			t.Errorf("unrecognized stale-registration error, so create cannot recover:\n\t%s", msg)
		}
	}

	unrelated := []string{
		"git: fatal: not a git repository",
		"git: fatal: invalid reference: nope",
		"permission denied",
	}
	for _, msg := range unrelated {
		if isStaleRegistrationError(errors.New(msg)) {
			t.Errorf("unrelated failure treated as a stale registration: %s", msg)
		}
	}
}

// TestStaleWorktreesAreReachable closes the hole that hiding stale rows would
// otherwise open: filtering them out of enumerateWorktrees also removes them
// from every by-name lookup, so a ghost stops being visible AND stops being
// removable at the same moment. Something must still be able to find and
// clear one, or the leftover git entry survives forever with no devgeta
// command able to touch it.
func TestStaleWorktreesAreReachable(t *testing.T) {
	// staleRepoFixture wires a repo whose only registration is stale, which
	// is the shape both enumerateStaleWorktrees and PruneStale walk.
	staleRepoFixture := func(t *testing.T) (*WorktreeManager, *commands.MockBaseCommand, string, string) {
		t.Helper()
		wm, mockGitBase, _ := newListTestWM(t)
		t.Chdir(t.TempDir())
		mainRoot := filepath.Join(t.TempDir(), "taskqueue2")
		anchor := createWorktreeDir(t, "taskqueue2", "cst-8621-gone")
		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist),
			commands.ExecCommandResult(
				stalePorcelain(mainRoot, nil, []string{anchor}), "", nil,
			),
			commands.ExecCommandResult("", "", nil), // worktree prune
		)
		return wm, mockGitBase, mainRoot, anchor
	}

	t.Run("enumerateStaleWorktrees sees exactly what enumerateWorktrees hides", func(t *testing.T) {
		wm, _, mainRoot, anchor := staleRepoFixture(t)

		stale := wm.enumerateStaleWorktrees()
		if len(stale) != 1 {
			t.Fatalf("expected 1 stale entry, got %d: %+v", len(stale), stale)
		}
		if stale[0].Path != anchor {
			t.Errorf("Path = %q, want %q", stale[0].Path, anchor)
		}
		if stale[0].RepoRoot != mainRoot {
			t.Errorf("RepoRoot = %q, want %q — pruning needs a repo root, not the dead path",
				stale[0].RepoRoot, mainRoot)
		}
		if stale[0].Reason == "" {
			t.Error("Reason is empty; git's own explanation should be carried through")
		}
	})

	t.Run("PruneStale clears them and reports what it cleared", func(t *testing.T) {
		wm, mockGitBase, mainRoot, _ := staleRepoFixture(t)

		cleared, err := wm.PruneStale()
		if err != nil {
			t.Fatalf("PruneStale failed: %v", err)
		}
		if len(cleared) != 1 {
			t.Fatalf("expected 1 cleared entry, got %d", len(cleared))
		}
		prunes := gitCallsContaining(mockGitBase, "prune")
		if len(prunes) != 1 {
			t.Fatalf("expected exactly 1 prune call, got %d", len(prunes))
		}
		if got := dirArgOf(prunes[0]); got != mainRoot {
			t.Errorf("prune ran from %q, want the repo root %q", got, mainRoot)
		}
	})

	t.Run("PruneStale is a no-op when nothing is stale", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		t.Chdir(t.TempDir())
		mainRoot := filepath.Join(t.TempDir(), "clean")
		live := createWorktreeDir(t, "clean", "healthy")
		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist),
			commands.ExecCommandResult(
				stalePorcelain(mainRoot, []string{live}, nil), "", nil,
			),
		)

		cleared, err := wm.PruneStale()
		if err != nil {
			t.Fatalf("PruneStale failed: %v", err)
		}
		if len(cleared) != 0 {
			t.Errorf("expected nothing cleared, got %+v", cleared)
		}
		if n := len(gitCallsContaining(mockGitBase, "prune")); n != 0 {
			t.Errorf("expected no prune call for a clean repo, got %d", n)
		}
	})

	// The reported flow: the user knows the name of a worktree they deleted
	// and asks devgeta to remove it. Before this, that returned
	// "nothing to remove" and left git's entry untouched.
	t.Run("Remove by name clears a stale entry instead of giving up", func(t *testing.T) {
		wm, mockGitBase, _ := newListTestWM(t)
		t.Chdir(t.TempDir())
		mainRoot := filepath.Join(t.TempDir(), "taskqueue2")
		anchor := createWorktreeDir(t, "taskqueue2", "cst-8621-gone")
		porcelain := stalePorcelain(mainRoot, nil, []string{anchor})

		mockGitBase.SetExecCommandResults(
			// Remove's current-repo probe: cwd is not a repo.
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist),
			// findRepoForWorktree -> enumerateWorktrees (stale rows filtered out)
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist),
			commands.ExecCommandResult(porcelain, "", nil),
			// pruneStaleWorktreeNamed -> enumerateStaleWorktrees
			commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist),
			commands.ExecCommandResult(porcelain, "", nil),
			// the prune itself
			commands.ExecCommandResult("", "", nil),
		)

		if err := wm.Remove("cst-8621-gone", false); err != nil {
			t.Fatalf("Remove should clear a stale entry, not fail: %v", err)
		}
		prunes := gitCallsContaining(mockGitBase, "prune")
		if len(prunes) != 1 {
			t.Fatalf("expected the stale entry to be pruned, got %d prune calls", len(prunes))
		}
		if got := dirArgOf(prunes[0]); got != mainRoot {
			t.Errorf("prune ran from %q, want the repo root %q", got, mainRoot)
		}
	})
}

// assertRemovedButUnprunable states the contract removeByRepo has when git is
// not answering usefully: `git worktree remove` never completes, removal
// falls back to deleting the directory itself, and that fallback LEAVES git
// holding a registration pointing at a path that no longer exists. The
// directory is gone (the caller's main intent), but the cleanup is knowingly
// incomplete — so it must be reported.
//
// Swallowing it is the same silent-success failure this whole change set
// exists to remove: the caller is told everything worked while a ghost entry
// is left behind, which is precisely how they accumulated unnoticed. The
// message must also make clear the directory DID come out, so the user is not
// left thinking nothing happened.
func assertRemovedButUnprunable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the unprunable stale entry to be reported, got nil")
	}
	if !strings.Contains(err.Error(), "directory was removed") {
		t.Errorf("error must confirm the directory did come out, got: %v", err)
	}
}

// TestPruneFailureIsEscalatedOnlyWhenItMatters pins both halves of the
// prune-failure contract. Getting either half wrong reintroduces a bug:
// swallowing the failure on the fallback path leaves a ghost while reporting
// success, and escalating it after a clean removal fails an operation that
// fully worked.
func TestPruneFailureIsEscalatedOnlyWhenItMatters(t *testing.T) {
	t.Run("a clean git removal does not fail over a defensive prune error", func(t *testing.T) {
		wm, mockGitBase, mockTmuxBase := newListTestWM(t)
		t.Chdir(t.TempDir())
		repoRoot := t.TempDir()
		repoSlug := filepath.Base(repoRoot)
		const name = "feature-a"
		wtPath := createWorktreeDir(t, repoSlug, name)
		mockTmuxBase.SetExecCommandResult("", "no window", os.ErrNotExist)

		// Keyed on the command, not its position: every git call succeeds
		// EXCEPT `worktree prune`. That is the combination the escalation
		// boundary turns on and the one a positional sequence cannot express
		// without hardcoding how many lookups the implementation happens to
		// make.
		porcelain := stalePorcelain(repoRoot, []string{wtPath}, nil)
		var pruneCalls int
		mockGitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
			for _, a := range c.Args {
				if a == "prune" {
					pruneCalls++
					return "", "fatal: prune refused", os.ErrPermission
				}
			}
			return porcelain, "", nil
		}

		// `git worktree remove` succeeded, so git already dropped its own
		// registration and the trailing prune had nothing left to do. Failing
		// the whole removal over it would report an error for an operation
		// that fully worked.
		if err := wm.removeByRepo(repoSlug, name, true); err != nil {
			t.Fatalf(
				"a clean removal must not fail over a prune that had nothing to do: %v", err,
			)
		}
		if pruneCalls == 0 {
			t.Fatal("the prune never ran, so this test proved nothing")
		}
	})

	// The escalating half is covered by assertRemovedButUnprunable at every
	// fallback-path call site in worktree_test.go; this pins the wording those
	// assertions depend on, so a reworded message fails here rather than
	// silently weakening them.
	t.Run("the fallback-path error names the worktree and confirms the removal", func(t *testing.T) {
		wm, mockGitBase, mockTmuxBase := newListTestWM(t)
		t.Chdir(t.TempDir())
		repoSlug := "somerepo"
		const name = "feature-b"
		wtPath := createWorktreeDir(t, repoSlug, name)
		mockGitBase.SetExecCommandResult("", "git is broken", os.ErrNotExist)
		mockTmuxBase.SetExecCommandResult("", "no window", os.ErrNotExist)

		err := wm.removeByRepo(repoSlug, name, true)
		assertRemovedButUnprunable(t, err)
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error must name the worktree, got: %v", err)
		}
		if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
			t.Error("the directory should still have been removed via the fallback")
		}
	})
}
