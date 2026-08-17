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
			"excluded (see `dg task branch-diff --file <path>` to inspect): go.sum (+40/-12)"
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
			"excluded (see `dg task branch-diff --file <path>` to inspect): go.sum (+40/-12)"
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

func TestBranchDiff(t *testing.T) {
	t.Run("no file: excludes lockfiles in one call and notes them", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			defaultBranch: gitAnswer{stdout: "origin/main\n"},
			mergeBase:     gitAnswer{stdout: "abc123\n"},
			diff:          gitAnswer{stdout: "diff --git a/x b/x\n+hi\n"},
			numstat:       gitAnswer{stdout: "120\t30\tx\n40\t12\tgo.sum\n"},
		}.install(gitBase)

		out, err := tm.BranchDiff("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "diff --git a/x b/x") {
			t.Fatalf("expected diff content, got: %q", out)
		}
		if !strings.Contains(
			out,
			"excluded (see `dg task branch-diff --file <path>` to inspect): go.sum (+40/-12)",
		) {
			t.Fatalf("expected exclusion note, got: %q", out)
		}

		// The filtered diff call must be a single invocation carrying "--", ".",
		// and the exclusion pathspecs — not one diff per pattern. The range is
		// the bare merge-base, NOT "<base>...HEAD": two dots is what includes
		// uncommitted work (ADR-0019).
		diffCall := renderedDiffCall(t, gitBase)
		joined := strings.Join(diffCall.Args, " ")
		if !strings.Contains(joined, "diff abc123 -- .") {
			t.Fatalf("expected a working-tree diff against the merge-base, got: %v", diffCall.Args)
		}
		if strings.Contains(joined, "abc123...HEAD") {
			t.Fatalf("committed-only range leaked back in: %v", diffCall.Args)
		}
		if !strings.Contains(joined, ":(exclude,glob)**/go.sum") {
			t.Fatalf("expected go.sum exclusion pathspec, got: %v", diffCall.Args)
		}
		// No dir was given, so nothing may carry an empty "-C".
		for _, call := range gitBase.ExecCommandCalls {
			if slices.Contains(call.Args, "-C") {
				t.Errorf("expected no -C without a dir, got: %v", call.Args)
			}
		}
	})

	t.Run("no file: uncommitted and untracked work is part of the diff", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			defaultBranch: gitAnswer{stdout: "origin/main\n"},
			mergeBase:     gitAnswer{stdout: "abc123\n"},
			diff:          gitAnswer{stdout: "diff --git a/x b/x\n+hi\n"},
			numstat:       gitAnswer{stdout: "2\t0\tx\n"},
			untracked:     gitAnswer{stdout: "notes.txt\x00"}, // ls-files --others -z
		}.install(gitBase)

		out, err := tm.BranchDiff("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Untracked files (no diff — read them directly):\n  notes.txt") {
			t.Fatalf("expected untracked files named, got: %q", out)
		}
	})

	t.Run("no file: all-excluded sentinel when only lockfiles changed", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			defaultBranch: gitAnswer{stdout: "origin/main\n"},
			mergeBase:     gitAnswer{stdout: "abc123\n"},
			// filtered diff is empty; numstat shows only go.sum
			numstat: gitAnswer{stdout: "40\t12\tgo.sum\n"},
		}.install(gitBase)

		out, err := tm.BranchDiff("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(
			out,
			"No reviewable changes in main..worktree (all changes excluded — see notes below).",
		) {
			t.Fatalf("expected all-excluded sentinel, got: %q", out)
		}
		if !strings.Contains(out, "go.sum (+40/-12)") {
			t.Fatalf("expected exclusion note, got: %q", out)
		}
	})

	t.Run("no file: no-changes case when nothing changed at all", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			defaultBranch: gitAnswer{stdout: "origin/main\n"},
			mergeBase:     gitAnswer{stdout: "abc123\n"},
		}.install(gitBase)

		out, err := tm.BranchDiff("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "No changes in main..worktree." {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("--file bypasses exclusions", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("abc123\n", "", nil),
			commands.ExecCommandResult("diff --git a/go.sum b/go.sum\n+entry\n", "", nil),
		)

		out, err := tm.BranchDiff("go.sum")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "diff --git a/go.sum b/go.sum\n+entry\n" {
			t.Fatalf("unexpected output: %q", out)
		}

		diffCall := gitBase.ExecCommandCalls[2]
		if len(diffCall.Args) != 4 || diffCall.Args[3] != "go.sum" {
			t.Fatalf("expected file passed as its own argv element, got: %v", diffCall.Args)
		}
	})

	t.Run("--file not in range yields sentinel", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("abc123\n", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		out, err := tm.BranchDiff("unrelated.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "No changes for unrelated.go in main..worktree." {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	// An untracked file has no diff for git to print, so the empty result must
	// not be reported as "no changes" — that would tell a reviewer to skip a
	// file that is entirely new work.
	t.Run("--file on an untracked file says so instead of no-changes", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("abc123\n", "", nil),
			commands.ExecCommandResult("", "", nil), // empty diff
			// -z output: NUL-terminated records, paths verbatim.
			commands.ExecCommandResult("brand-new.go\x00", "", nil), // ls-files --others -z
		)

		out, err := tm.BranchDiff("brand-new.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "brand-new.go is untracked") ||
			!strings.Contains(out, "read the file directly") {
			t.Fatalf("expected the untracked answer, got: %q", out)
		}
	})

	// Regression (journal n1, round 1): the untracked lookup must be read with
	// `-z`. Without it git QUOTES and C-escapes any path with a space, quote,
	// tab, or non-ASCII byte, so `docs/my draft.md` came back as
	// `"docs/my draft.md"` — which never matched the caller's --file value,
	// flipping the answer above to "No changes" for a file that is entirely new
	// work. These are the exact paths git was observed to quote.
	t.Run("--file on an untracked path with spaces or quotes still matches", func(t *testing.T) {
		for _, path := range []string{"docs/my draft.md", `docs/quotes"odd.md`, "docs/tab\todd.md"} {
			t.Run(path, func(t *testing.T) {
				tm, gitBase, _ := newTaskSetup()
				gitBase.SetExecCommandResults(
					commands.ExecCommandResult("origin/main\n", "", nil),
					commands.ExecCommandResult("abc123\n", "", nil),
					commands.ExecCommandResult("", "", nil), // empty diff
					// git echoes only what the pathspec matched.
					commands.ExecCommandResult(path+"\x00", "", nil),
				)

				out, err := tm.BranchDiff(path)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !strings.Contains(out, path+" is untracked") {
					t.Errorf("expected %q reported as untracked, got: %q", path, out)
				}
				if strings.Contains(out, "No changes for") {
					t.Errorf("an untracked path must never report no-changes, got: %q", out)
				}
			})
		}
	})

	// The same parse feeds the untracked note, so a quoted path would also be
	// shown to a reader in a form they cannot pass back to any command.
	t.Run("the untracked note prints paths verbatim", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			defaultBranch: gitAnswer{stdout: "origin/main\n"},
			mergeBase:     gitAnswer{stdout: "abc123\n"},
			diff:          gitAnswer{stdout: "diff --git a/x b/x\n+hi\n"},
			numstat:       gitAnswer{stdout: "1\t0\tx\n"},
			untracked:     gitAnswer{stdout: "docs/my draft.md\x00"},
		}.install(gitBase)

		out, err := tm.BranchDiff("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "\n  docs/my draft.md") {
			t.Errorf("expected the path listed verbatim, got: %q", out)
		}
		if strings.Contains(out, `"docs/my draft.md"`) {
			t.Errorf("expected no git quoting in the note, got: %q", out)
		}
	})

	// The listing lookup: `ls-files --others --exclude-standard -z`, no
	// pathspec. -z is what makes the verbatim paths above possible, and
	// asserting the flag keeps a future edit from dropping it and reintroducing
	// quoting that only shows up on paths a fixture might not cover.
	t.Run("the untracked listing asks git for -z output", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			defaultBranch: gitAnswer{stdout: "origin/main\n"},
			mergeBase:     gitAnswer{stdout: "abc123\n"},
		}.install(gitBase)

		if _, err := tm.BranchDiff(""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		call := untrackedLookupCall(t, gitBase)
		for _, want := range []string{"--others", "--exclude-standard", "-z"} {
			if !slices.Contains(call.Args, want) {
				t.Errorf("expected %s in the untracked lookup, got: %v", want, call.Args)
			}
		}
		if slices.Contains(call.Args, "--") {
			t.Errorf("the whole-branch listing must not be limited by a pathspec: %v", call.Args)
		}
	})

	// Regression (journal n1, round 2): --file must ask GIT whether that one
	// path is untracked, passing it as a pathspec, instead of string-matching
	// the caller's spelling against a listing. Only git knows that
	// `./docs/new.md`, an absolute path, and `new.md` from inside `docs/` are
	// all the same file — matching strings answered "No changes" for every
	// spelling but the one git happened to print.
	t.Run("--file asks git about that one path", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("abc123\n", "", nil),
			commands.ExecCommandResult("", "", nil), // empty diff
			// git resolved the caller's "./docs/new.md" to the file it tracks.
			commands.ExecCommandResult("docs/new.md\x00", "", nil),
		)

		out, err := tm.BranchDiff("./docs/new.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "is untracked") {
			t.Errorf("expected the untracked answer for an equivalent path form, got: %q", out)
		}

		call := untrackedLookupCall(t, gitBase)
		sep := slices.Index(call.Args, "--")
		if sep < 0 || sep != len(call.Args)-2 || call.Args[sep+1] != "./docs/new.md" {
			t.Errorf(
				"expected the caller's path forwarded as a pathspec after --, got: %v",
				call.Args,
			)
		}
	})

	t.Run("does not fetch", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		worktreeDiffScript{
			defaultBranch: gitAnswer{stdout: "origin/main\n"},
			mergeBase:     gitAnswer{stdout: "abc123\n"},
		}.install(gitBase)

		if _, err := tm.BranchDiff(""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, call := range gitBase.ExecCommandCalls {
			if len(call.Args) > 0 && call.Args[0] == "fetch" {
				t.Fatalf("expected branch-diff to never call fetch, got: %v", call.Args)
			}
		}
	})
}

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
