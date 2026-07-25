package task

import (
	"fmt"
	"strings"
	"testing"

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

// --- Orchestration tests (mocked git.Base, no real commands) ---

func TestBranchDiff(t *testing.T) {
	t.Run("no file: excludes lockfiles in one call and notes them", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"origin/main\n",
				"",
				nil,
			), // symbolic-ref (default branch)
			commands.ExecCommandResult("abc123\n", "", nil), // merge-base
			commands.ExecCommandResult(
				"diff --git a/x b/x\n+hi\n",
				"",
				nil,
			), // diff w/ exclusions
			commands.ExecCommandResult(
				"120\t30\tx\n40\t12\tgo.sum\n",
				"",
				nil,
			), // numstat (unfiltered)
		)

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
		// and the exclusion pathspecs — not one diff per pattern.
		diffCall := gitBase.ExecCommandCalls[2]
		joined := strings.Join(diffCall.Args, " ")
		if !strings.Contains(joined, "abc123...HEAD -- .") {
			t.Fatalf("expected range and pathspec base, got: %v", diffCall.Args)
		}
		if !strings.Contains(joined, ":(exclude,glob)**/go.sum") {
			t.Fatalf("expected go.sum exclusion pathspec, got: %v", diffCall.Args)
		}
	})

	t.Run("no file: all-excluded sentinel when only lockfiles changed", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("abc123\n", "", nil),
			commands.ExecCommandResult("", "", nil),                 // filtered diff is empty
			commands.ExecCommandResult("40\t12\tgo.sum\n", "", nil), // numstat shows only go.sum
		)

		out, err := tm.BranchDiff("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(
			out,
			"No reviewable changes in abc123...HEAD (all changes excluded — see notes below).",
		) {
			t.Fatalf("expected all-excluded sentinel, got: %q", out)
		}
		if !strings.Contains(out, "go.sum (+40/-12)") {
			t.Fatalf("expected exclusion note, got: %q", out)
		}
	})

	t.Run("no file: no-changes case when nothing changed at all", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("abc123\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		out, err := tm.BranchDiff("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "No changes in abc123...HEAD." {
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
		if out != "No changes for unrelated.go in abc123...HEAD." {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("does not fetch", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("abc123\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

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
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"origin/main\n",
				"",
				nil,
			), // symbolic-ref (default branch)
			commands.ExecCommandResult("abc123\n", "", nil),                        // merge-base
			commands.ExecCommandResult("diff --git a/x b/x\n+hi\n", "", nil),       // diff
			commands.ExecCommandResult("5\t2\tmain.go\n40\t12\tgo.sum\n", "", nil), // numstat
			commands.ExecCommandResult(
				"?? notes.txt\n",
				"",
				nil,
			), // status --porcelain
		)

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
		if !strings.Contains(res.Content, "Untracked files:\n  notes.txt") {
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
		diffCall := gitBase.ExecCommandCalls[2]
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
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("", "fatal: no merge base", fmt.Errorf("exit 1")),
		)
		if _, err := BranchDiffAt(tm.Git, "/tmp/wt"); err == nil {
			t.Fatal("expected error when merge-base fails")
		}
	})
}
