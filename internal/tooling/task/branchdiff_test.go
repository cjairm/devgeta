package task

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/commands"
)

// --- Pure formatter tests (fixtures, no mocks) ---

func TestFormatBranchDiff(t *testing.T) {
	t.Run("returns diff as-is when non-empty and nothing excluded", func(t *testing.T) {
		got := formatBranchDiff("abc123...HEAD", "diff --git a/x b/x\n+hi\n", nil)
		if got != "diff --git a/x b/x\n+hi\n" {
			t.Fatalf("unexpected output: %q", got)
		}
	})

	t.Run("appends exclusion notes after a non-empty diff", func(t *testing.T) {
		got := formatBranchDiff("abc123...HEAD", "diff --git a/x b/x\n+hi\n", []fileChange{
			{Path: "go.sum", Added: 40, Removed: 12},
		})
		want := "diff --git a/x b/x\n+hi\n\n" +
			"excluded from this diff: go.sum (+40/-12)"
		if got != want {
			t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
		}
	})

	t.Run("binary excluded file notes without counts", func(t *testing.T) {
		got := formatBranchDiff("abc123...HEAD", "diff --git a/x b/x\n+hi\n", []fileChange{
			{Path: "bun.lockb", Binary: true},
		})
		if !strings.Contains(got, "bun.lockb (binary)") {
			t.Fatalf("expected binary note, got: %q", got)
		}
	})

	t.Run("all-excluded sentinel when diff empty but exclusions exist", func(t *testing.T) {
		got := formatBranchDiff("abc123...HEAD", "", []fileChange{
			{Path: "go.sum", Added: 40, Removed: 12},
		})
		want := "No reviewable changes in abc123...HEAD (all changes excluded — see notes below).\n" +
			"excluded from this diff: go.sum (+40/-12)"
		if got != want {
			t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
		}
	})

	t.Run("no-changes sentinel when diff empty and nothing excluded", func(t *testing.T) {
		got := formatBranchDiff("abc123...HEAD", "  \n", nil)
		want := "No changes in abc123...HEAD."
		if got != want {
			t.Fatalf("unexpected output: %q", got)
		}
	})
}

func TestChangedFiles(t *testing.T) {
	t.Run("returns parsed numstat output", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("120\t30\tx\n40\t12\tgo.sum\n", "", nil),
		)

		got, err := changedFiles(tm.Git, "", "abc123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got[0].Path != "x" || got[0].Added != 120 || got[0].Removed != 30 {
			t.Fatalf("unexpected entry 0: %+v", got)
		}
		if got[1].Path != "go.sum" || got[1].Added != 40 || got[1].Removed != 12 {
			t.Fatalf("unexpected entry 1: %+v", got)
		}

		call := gitBase.ExecCommandCalls[0]
		joined := strings.Join(call.Args, " ")
		if !strings.Contains(joined, "diff --numstat --no-renames abc123") {
			t.Fatalf("expected the numstat diff call, got: %v", call.Args)
		}
	})

	t.Run("RunCapture failure is returned unwrapped", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: numstat exploded", fmt.Errorf("exit 1")),
		)

		_, err := changedFiles(tm.Git, "", "abc123")
		if err == nil {
			t.Fatal("expected an error")
		}
		// changedFiles must hand back whatever RunCapture returned, with no
		// extra wrapping of its own — wrapping (the "branch-diff:" prefix)
		// happens in the caller, collectWorktreeDiff.
		if err.Error() != "git: fatal: numstat exploded" {
			t.Fatalf("expected the RunCapture error returned as-is, got: %v", err)
		}
		if strings.Contains(err.Error(), "branch-diff") {
			t.Fatalf("expected no caller-level wrapping in changedFiles, got: %v", err)
		}
	})
}

// soleCall returns the one recorded git call whose arguments match, failing the
// test if there isn't exactly one — an assertion about "the diff call" only
// means something if the call it names is unambiguous. Calls are found by what
// they ask git for rather than by their position, because collectWorktreeDiff
// runs its two diffs concurrently and so the order they are recorded in is
// undefined. `what` names the call in the failure message.
func soleCall(
	t *testing.T,
	gitBase *commands.MockBaseCommand,
	what string,
	match func(args []string) bool,
) commands.CommandParams {
	t.Helper()
	var found []commands.CommandParams
	for _, call := range gitBase.ExecCommandCalls {
		if match(call.Args) {
			found = append(found, call)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one %s, got %d: %v", what, len(found), found)
	}
	return found[0]
}

// untrackedLookupCall returns the one `git ls-files` call branch-diff made to
// find untracked files.
func untrackedLookupCall(
	t *testing.T,
	gitBase *commands.MockBaseCommand,
) commands.CommandParams {
	t.Helper()
	return soleCall(t, gitBase, "untracked lookup", func(args []string) bool {
		return slices.Contains(args, "ls-files")
	})
}

// renderedDiffCall returns the `git diff` that produces the rendered diff. The
// `--numstat` diff runs alongside it and is also a `git diff`, so the absence
// of that flag is what tells the two apart.
func renderedDiffCall(
	t *testing.T,
	gitBase *commands.MockBaseCommand,
) commands.CommandParams {
	t.Helper()
	return soleCall(t, gitBase, "rendered diff call", func(args []string) bool {
		return slices.Contains(args, "diff") && !slices.Contains(args, "--numstat")
	})
}

// gitAnswer is one canned reply to a git call.
type gitAnswer struct {
	stdout string
	stderr string
	err    error
}

// worktreeDiffScript answers the git calls collectWorktreeDiff makes by what
// each call ASKS FOR, not by when it arrives. The rendered diff and the
// `--numstat` diff run concurrently, so their order is undefined and a
// positional sequence would hand the rendered diff the numstat's stdout about
// half the time. The zero value of any field is an empty, successful reply.
type worktreeDiffScript struct {
	defaultBranch gitAnswer // symbolic-ref, and anything else not matched below
	mergeBase     gitAnswer
	diff          gitAnswer // the rendered diff, with exclusions applied
	numstat       gitAnswer
	untracked     gitAnswer // ls-files --others
}

// install wires the script onto the mock's argument-keyed hook.
func (s worktreeDiffScript) install(gitBase *commands.MockBaseCommand) {
	gitBase.ExecCommandFn = func(cmd commands.CommandParams) (string, string, error) {
		a := s.answerFor(cmd.Args)
		return a.stdout, a.stderr, a.err
	}
}

func (s worktreeDiffScript) answerFor(args []string) gitAnswer {
	switch classifyGitCall(args) {
	case callMergeBase:
		return s.mergeBase
	case callUntracked:
		return s.untracked
	case callNumstat:
		return s.numstat
	case callDiff:
		return s.diff
	default:
		return s.defaultBranch
	}
}

// gitCallKind names which of collectWorktreeDiff's git calls a set of
// arguments belongs to.
type gitCallKind int

const (
	callOther gitCallKind = iota // symbolic-ref, and anything else
	callMergeBase
	callDiff // the rendered diff
	callNumstat
	callUntracked
)

// classifyGitCall identifies a call by its arguments. The rendered diff and the
// numstat are both `git diff`, so the `--numstat` flag is what separates them.
func classifyGitCall(args []string) gitCallKind {
	switch {
	case slices.Contains(args, "merge-base"):
		return callMergeBase
	case slices.Contains(args, "ls-files"):
		return callUntracked
	case slices.Contains(args, "--numstat"):
		return callNumstat
	case slices.Contains(args, "diff"):
		return callDiff
	default:
		return callOther
	}
}

// --- Orchestration tests (mocked git.Base, no real commands) ---

func TestBranchDiffAt(t *testing.T) {
	t.Run("diffs merge-base against working tree with -C dir and totals stats", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			defaultBranch: gitAnswer{stdout: "origin/main\n"}, // symbolic-ref
			mergeBase:     gitAnswer{stdout: "abc123\n"},
			diff:          gitAnswer{stdout: "diff --git a/x b/x\n+hi\n"},
			numstat:       gitAnswer{stdout: "5\t2\tmain.go\n40\t12\tgo.sum\n"},
			untracked:     gitAnswer{stdout: "notes.txt\x00"}, // ls-files --others -z
		}.install(gitBase)

		res, err := BranchDiffAt(tm.Git, "/tmp/wt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(res.Content, "diff --git a/x b/x") {
			t.Errorf("expected diff content, got: %q", res.Content)
		}
		if !strings.Contains(res.Content, "go.sum (+40/-12)") {
			t.Errorf("expected exclusion note for go.sum, got: %q", res.Content)
		}
		if !strings.Contains(
			res.Content,
			"Untracked files (no diff — read them directly):\n  notes.txt",
		) {
			t.Errorf("expected untracked file listing, got: %q", res.Content)
		}
		// main.go (included) + notes.txt (untracked); go.sum excluded from totals.
		if res.Files != 2 || res.Added != 5 || res.Removed != 2 {
			t.Errorf(
				"unexpected stats: files=%d added=%d removed=%d",
				res.Files,
				res.Added,
				res.Removed,
			)
		}
		// Base metadata labels the comparison; abc123 is already short.
		if res.BaseBranch != "main" || res.BaseSHA != "abc123" {
			t.Errorf("unexpected base metadata: branch=%q sha=%q", res.BaseBranch, res.BaseSHA)
		}
		// Per-file stats cover included files only.
		if len(res.FileStats) != 1 || res.FileStats[0].Path != "main.go" ||
			res.FileStats[0].Added != 5 || res.FileStats[0].Removed != 2 {
			t.Errorf("unexpected file stats: %+v", res.FileStats)
		}

		// Every git call must target the worktree dir via -C.
		for _, call := range gitBase.ExecCommandCalls {
			if len(call.Args) < 2 || call.Args[0] != "-C" || call.Args[1] != "/tmp/wt" {
				t.Errorf("expected every call to start with '-C /tmp/wt', got %v", call.Args)
			}
		}

		// The diff call must target the bare merge-base (working tree diff,
		// committed + uncommitted), keep colors, and carry the exclusions.
		diffCall := renderedDiffCall(t, gitBase)
		joined := strings.Join(diffCall.Args, " ")
		if !strings.Contains(joined, "diff --color=always abc123 -- .") {
			t.Errorf("expected colored working-tree diff against merge-base, got %v", diffCall.Args)
		}
		if !strings.Contains(joined, ":(exclude,glob)**/go.sum") {
			t.Errorf("expected exclusion pathspecs, got %v", diffCall.Args)
		}
	})

	t.Run("merge-base failure surfaces error", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			defaultBranch: gitAnswer{stdout: "origin/main\n"},
			mergeBase:     gitAnswer{stderr: "fatal: no merge base", err: fmt.Errorf("exit 1")},
		}.install(gitBase)
		if _, err := BranchDiffAt(tm.Git, "/tmp/wt"); err == nil {
			t.Fatal("expected error when merge-base fails")
		}
	})
}

// TestBranchStatsAt covers ADR-0051: the ws dashboard's per-row diffstat.
// Unlike BranchDiffAt, it takes defaultBranch as a parameter rather than
// resolving it itself - the caller resolves it once per repo, not once per
// worktree - and skips the rendered diff and color pass entirely.
func TestBranchStatsAt(t *testing.T) {
	t.Run(
		"counts only, no diff body, and never re-resolves the default branch",
		func(t *testing.T) {
			tm, gitBase, _ := newTaskSetup()
			worktreeDiffScript{
				mergeBase: gitAnswer{stdout: "abc123\n"},
				numstat:   gitAnswer{stdout: "5\t2\tmain.go\n40\t12\tgo.sum\n"},
				untracked: gitAnswer{stdout: "notes.txt\x00"},
			}.install(gitBase)

			res, err := BranchStatsAt(tm.Git, "/tmp/wt", "main")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// main.go (included) + notes.txt (untracked); go.sum excluded from totals -
			// the exact fixture TestBranchDiffAt uses, so the two can be compared.
			if res.Files != 2 || res.Added != 5 || res.Removed != 2 {
				t.Errorf("unexpected stats: %+v", res)
			}

			for _, call := range gitBase.ExecCommandCalls {
				if len(call.Args) < 2 || call.Args[0] != "-C" || call.Args[1] != "/tmp/wt" {
					t.Errorf("expected every call to target -C /tmp/wt, got %v", call.Args)
				}
				if slices.Contains(call.Args, "symbolic-ref") {
					t.Errorf(
						"expected BranchStatsAt not to re-resolve the default branch, got %v",
						call.Args,
					)
				}
			}
		},
	)

	t.Run("no changes yields zero counts, not an error", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{mergeBase: gitAnswer{stdout: "abc123\n"}}.install(gitBase)

		res, err := BranchStatsAt(tm.Git, "/tmp/wt", "main")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Files != 0 || res.Added != 0 || res.Removed != 0 {
			t.Errorf("expected zero stats for no changes, got %+v", res)
		}
	})

	t.Run("merge-base failure surfaces error", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			mergeBase: gitAnswer{stderr: "fatal: no merge base", err: fmt.Errorf("exit 1")},
		}.install(gitBase)
		if _, err := BranchStatsAt(tm.Git, "/tmp/wt", "main"); err == nil {
			t.Fatal("expected error when merge-base fails")
		}
	})

	// The row's numbers and the diff pane's header must never disagree
	// (ADR-0051) - proved here by computing both from the identical fixture
	// and comparing.
	t.Run("matches BranchDiffAt's counts on the same fixture", func(t *testing.T) {
		fixture := func() worktreeDiffScript {
			return worktreeDiffScript{
				defaultBranch: gitAnswer{stdout: "origin/main\n"},
				mergeBase:     gitAnswer{stdout: "abc123\n"},
				diff:          gitAnswer{stdout: "diff --git a/x b/x\n+hi\n"},
				numstat:       gitAnswer{stdout: "5\t2\tmain.go\n40\t12\tgo.sum\n"},
				untracked:     gitAnswer{stdout: "notes.txt\x00"},
			}
		}

		tm, gitBase, _ := newTaskSetup()
		fixture().install(gitBase)
		full, err := BranchDiffAt(tm.Git, "/tmp/wt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tm2, gitBase2, _ := newTaskSetup()
		fixture().install(gitBase2)
		stats, err := BranchStatsAt(tm2.Git, "/tmp/wt", "main")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if stats.Files != full.Files || stats.Added != full.Added || stats.Removed != full.Removed {
			t.Errorf("expected stats %+v to match the full diff's counts %+v", stats, full)
		}
	})
}

// rendezvousWait bounds how long one diff waits for the other. It is only ever
// reached when the two do NOT overlap, so it trades a slow failure for a
// readable one instead of hanging until the test binary's timeout.
const rendezvousWait = 5 * time.Second

// diffRendezvous makes collectWorktreeDiff's two diffs meet before either may
// return, and then releases them in a chosen order. Meeting is what proves they
// actually overlap: a sequential implementation would run one to completion
// while the other had not started, so the first would wait out rendezvousWait
// and the case fails instead of quietly passing. The ordering is what lets the
// same case be run both ways round, so "whichever finishes first" can be
// asserted rather than assumed.
type diffRendezvous struct {
	diffArrived    chan struct{}
	numstatArrived chan struct{}
	firstDone      chan struct{}

	// Written by the two diff goroutines, read only after they have joined.
	diffSawNumstat bool
	numstatSawDiff bool
}

func newDiffRendezvous() *diffRendezvous {
	return &diffRendezvous{
		diffArrived:    make(chan struct{}),
		numstatArrived: make(chan struct{}),
		firstDone:      make(chan struct{}),
	}
}

// meet announces this side's arrival on mine, waits for the other side on
// theirs, and then orders the two returns: the side named by isFirst returns
// immediately, the other waits for it. It reports whether the other side really
// showed up.
func (r *diffRendezvous) meet(mine, theirs chan struct{}, isFirst bool) bool {
	close(mine)
	overlapped := closedWithin(theirs)
	if isFirst {
		close(r.firstDone)
	} else {
		closedWithin(r.firstDone)
	}
	return overlapped
}

func closedWithin(c chan struct{}) bool {
	select {
	case <-c:
		return true
	case <-time.After(rendezvousWait):
		return false
	}
}

// install answers like the given script, but holds the two diffs at the
// rendezvous first.
func (r *diffRendezvous) install(
	gitBase *commands.MockBaseCommand,
	script worktreeDiffScript,
	diffFinishesFirst bool,
) {
	gitBase.ExecCommandFn = func(cmd commands.CommandParams) (string, string, error) {
		switch classifyGitCall(cmd.Args) {
		case callDiff:
			r.diffSawNumstat = r.meet(r.diffArrived, r.numstatArrived, diffFinishesFirst)
		case callNumstat:
			r.numstatSawDiff = r.meet(r.numstatArrived, r.diffArrived, !diffFinishesFirst)
		}
		a := script.answerFor(cmd.Args)
		return a.stdout, a.stderr, a.err
	}
}

func (r *diffRendezvous) assertOverlapped(t *testing.T) {
	t.Helper()
	if !r.diffSawNumstat || !r.numstatSawDiff {
		t.Fatalf(
			"the two diffs never overlapped (diff saw numstat: %v, numstat saw diff: %v); "+
				"they are supposed to run concurrently",
			r.diffSawNumstat,
			r.numstatSawDiff,
		)
	}
}

// The rendered diff and the numstat are two reads of the same commit range, so
// collectWorktreeDiff runs them together. Each case below is run both ways
// round: nothing about the answer may depend on which one wins the race.
func TestBranchDiffAtConcurrentDiffs(t *testing.T) {
	for _, diffFirst := range []bool{true, false} {
		name := "numstat finishes first"
		if diffFirst {
			name = "rendered diff finishes first"
		}

		t.Run("result is assembled the same way when the "+name, func(t *testing.T) {
			tm, gitBase, _ := newTaskSetup()
			r := newDiffRendezvous()
			r.install(gitBase, worktreeDiffScript{
				defaultBranch: gitAnswer{stdout: "origin/main\n"},
				mergeBase:     gitAnswer{stdout: "abc123\n"},
				diff:          gitAnswer{stdout: "diff --git a/main.go b/main.go\n+hi\n"},
				numstat:       gitAnswer{stdout: "5\t2\tmain.go\n40\t12\tgo.sum\n"},
				untracked:     gitAnswer{stdout: "notes.txt\x00"},
			}, diffFirst)

			res, err := BranchDiffAt(tm.Git, "/tmp/wt")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			r.assertOverlapped(t)

			// The rendered diff kept its own stdout — the failure this guards
			// against is the numstat's output being handed to the diff.
			if !strings.Contains(res.Content, "diff --git a/main.go b/main.go") {
				t.Errorf("expected the rendered diff's own output, got: %q", res.Content)
			}
			if !strings.Contains(res.Content, "go.sum (+40/-12)") {
				t.Errorf(
					"expected the numstat's counts in the exclusion note, got: %q",
					res.Content,
				)
			}
			// main.go (included) + notes.txt (untracked); go.sum excluded.
			if res.Files != 2 || res.Added != 5 || res.Removed != 2 {
				t.Errorf(
					"unexpected stats: files=%d added=%d removed=%d",
					res.Files,
					res.Added,
					res.Removed,
				)
			}
		})

		t.Run("both diffs failing reports the rendered diff when the "+name, func(t *testing.T) {
			tm, gitBase, _ := newTaskSetup()
			r := newDiffRendezvous()
			r.install(gitBase, worktreeDiffScript{
				defaultBranch: gitAnswer{stdout: "origin/main\n"},
				mergeBase:     gitAnswer{stdout: "abc123\n"},
				diff:          gitAnswer{stderr: "fatal: diff exploded", err: fmt.Errorf("exit 1")},
				numstat: gitAnswer{
					stderr: "fatal: numstat exploded",
					err:    fmt.Errorf("exit 1"),
				},
			}, diffFirst)

			_, err := BranchDiffAt(tm.Git, "/tmp/wt")
			if err == nil {
				t.Fatal("expected an error when both diffs fail")
			}
			r.assertOverlapped(t)

			// Whichever goroutine lost the race, the caller must get the same
			// error every run — the rendered diff is the pane's primary content,
			// so its failure is the one reported.
			if !strings.Contains(err.Error(), "diff exploded") {
				t.Errorf("expected the rendered diff's error, got: %v", err)
			}
			if strings.Contains(err.Error(), "numstat exploded") {
				t.Errorf("the reported error depends on which diff finished first: %v", err)
			}
		})
	}
}
