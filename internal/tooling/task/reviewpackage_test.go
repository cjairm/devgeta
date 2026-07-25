package task

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

// --- Pure formatter tests (fixtures, no mocks) ---

func TestParseReviewPackageCommitLog(t *testing.T) {
	t.Run("parses SHA, date, subject", func(t *testing.T) {
		raw := "abc1234\t2026-07-20\tfeat: add thing\n" +
			"def5678\t2026-07-21\tfix: correct thing\n"
		got, err := parseReviewPackageCommitLog(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(got))
		}
		if got[0].SHA != "abc1234" || got[0].Date != "2026-07-20" ||
			got[0].Subject != "feat: add thing" {
			t.Fatalf("unexpected entry 0: %+v", got[0])
		}
		if got[1].SHA != "def5678" || got[1].Subject != "fix: correct thing" {
			t.Fatalf("unexpected entry 1: %+v", got[1])
		}
	})

	t.Run("subject containing tabs is preserved via SplitN(3)", func(t *testing.T) {
		got, err := parseReviewPackageCommitLog("abc1234\t2026-07-20\tsubject\twith\ttabs\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].Subject != "subject\twith\ttabs" {
			t.Fatalf("unexpected entry: %+v", got)
		}
	})

	t.Run("empty input yields nil", func(t *testing.T) {
		got, err := parseReviewPackageCommitLog("   \n  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("malformed line errors", func(t *testing.T) {
		if _, err := parseReviewPackageCommitLog("not-a-commit-line"); err == nil {
			t.Fatal("expected error for malformed line")
		}
	})
}

func TestFormatReviewPackage(t *testing.T) {
	t.Run("no commits sentinel", func(t *testing.T) {
		got := formatReviewPackage(reviewPackageData{Base: "main", Head: "feat"})
		want := "range: main..feat\n" +
			"commits:\n" +
			"No commits in range.\n" +
			"files (0):\n" +
			"No file changes in range."
		if got != want {
			t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
		}
	})

	t.Run("full report with commits, files, exclusions, and diff", func(t *testing.T) {
		got := formatReviewPackage(reviewPackageData{
			Base: "main",
			Head: "feat",
			Commits: []reviewPackageCommit{
				{SHA: "abc1234", Date: "2026-07-20", Subject: "feat: add thing"},
				{SHA: "def5678", Date: "2026-07-21", Subject: "fix: correct thing"},
			},
			Included: []fileChange{
				{Path: "internal/tooling/task/task.go", Status: "M", Added: 120, Removed: 30},
				{Path: "internal/tooling/task/scope.go", Status: "A", Added: 200, Removed: 0},
			},
			Excluded: []fileChange{
				{Path: "go.sum", Status: "M", Added: 40, Removed: 12},
			},
			Diff: "diff --git a/x b/x\n+hi\n",
		})
		want := "range: main..feat\n" +
			"commits:\n" +
			"- abc1234 2026-07-20 feat: add thing\n" +
			"- def5678 2026-07-21 fix: correct thing\n" +
			"files (2):\n" +
			"M  internal/tooling/task/task.go  +120/-30\n" +
			"A  internal/tooling/task/scope.go  +200/-0\n" +
			"total: +320/-30\n" +
			"excluded (see `dg task review-package main feat --file <path>` to inspect): go.sum (+40/-12)\n" +
			"\n```diff\n" +
			"diff --git a/x b/x\n+hi\n" +
			"```"
		if got != want {
			t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
		}
	})

	t.Run("binary file renders without counts", func(t *testing.T) {
		got := formatReviewPackage(reviewPackageData{
			Base:     "main",
			Head:     "feat",
			Included: []fileChange{{Path: "assets/logo.png", Status: "A", Binary: true}},
			Diff:     "diff --git a/assets/logo.png b/assets/logo.png\nBinary files differ\n",
		})
		if !strings.Contains(got, "A  assets/logo.png  binary") {
			t.Fatalf("expected binary file row, got: %q", got)
		}
	})

	t.Run(
		"no included files but excluded ones exist: files sentinel plus receipt, no diff",
		func(t *testing.T) {
			got := formatReviewPackage(reviewPackageData{
				Base: "main",
				Head: "feat",
				Commits: []reviewPackageCommit{
					{SHA: "abc1234", Date: "2026-07-20", Subject: "chore: bump lockfile"},
				},
				Excluded: []fileChange{
					{Path: "go.sum", Status: "M", Added: 40, Removed: 12},
				},
			})
			want := "range: main..feat\n" +
				"commits:\n" +
				"- abc1234 2026-07-20 chore: bump lockfile\n" +
				"files (0):\n" +
				"No file changes in range.\n" +
				"excluded (see `dg task review-package main feat --file <path>` to inspect): go.sum (+40/-12)"
			if got != want {
				t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
			}
		},
	)

	t.Run("diff without trailing newline still gets a clean fence", func(t *testing.T) {
		got := formatReviewPackage(reviewPackageData{
			Base:     "main",
			Head:     "feat",
			Included: []fileChange{{Path: "x", Status: "M", Added: 1, Removed: 0}},
			Diff:     "diff --git a/x b/x\n+hi",
		})
		if !strings.HasSuffix(got, "diff --git a/x b/x\n+hi\n```") {
			t.Fatalf("expected clean fence close, got: %q", got)
		}
	})
}

// --- Orchestration tests (mocked git.Base, no real commands) ---

func TestReviewPackage(t *testing.T) {
	t.Run("happy path: verifies both refs, then renders full report", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("abc1234\n", "", nil), // rev-parse --verify base
			commands.ExecCommandResult("def5678\n", "", nil), // rev-parse --verify head
			commands.ExecCommandResult(
				"abc1234\t2026-07-20\tfeat: add thing\n"+
					"def5678\t2026-07-21\tfix: correct thing\n", "", nil,
			), // log
			commands.ExecCommandResult(
				"120\t30\tinternal/tooling/task/task.go\n"+
					"40\t12\tgo.sum\n", "", nil,
			), // numstat
			commands.ExecCommandResult(
				"M\tinternal/tooling/task/task.go\n"+
					"M\tgo.sum\n", "", nil,
			), // name-status
			commands.ExecCommandResult("diff --git a/x b/x\n+hi\n", "", nil), // diff -U10
		)

		out, err := tm.ReviewPackage("main", "feat", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantLines := []string{
			"range: main..feat",
			"- abc1234 2026-07-20 feat: add thing",
			"- def5678 2026-07-21 fix: correct thing",
			"files (1):",
			"M  internal/tooling/task/task.go  +120/-30",
			"total: +120/-30",
			"excluded (see `dg task review-package main feat --file <path>` to inspect): go.sum (+40/-12)",
			"```diff",
			"diff --git a/x b/x",
			"```",
		}
		for _, want := range wantLines {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got:\n%s", want, out)
			}
		}

		// Verify rev-parse --verify ran for both refs before anything else.
		calls := gitBase.ExecCommandCalls
		if len(calls) < 2 {
			t.Fatalf("expected at least 2 calls, got %d", len(calls))
		}
		assertCmd(t, calls[0], "git", "rev-parse", "--verify", "main")
		assertCmd(t, calls[1], "git", "rev-parse", "--verify", "feat")

		// The diff call must carry -U10, the range, and the exclusion pathspecs.
		diffCall := calls[len(calls)-1]
		joined := strings.Join(diffCall.Args, " ")
		if !strings.Contains(joined, "-U10 main..feat -- .") {
			t.Fatalf("expected -U10 and range/pathspec base, got: %v", diffCall.Args)
		}
		if !strings.Contains(joined, ":(exclude,glob)**/go.sum") {
			t.Fatalf("expected go.sum exclusion pathspec, got: %v", diffCall.Args)
		}
	})

	t.Run("unrecognized base ref fails fast with an actionable error", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"", "fatal: Needed a single revision", fmt.Errorf("exit 128"),
			),
		)

		_, err := tm.ReviewPackage("bogus", "feat", "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), `unrecognized ref "bogus"`) {
			t.Fatalf("expected error to name the bad ref, got: %v", err)
		}
		if got := gitBase.GetExecCommandCallCount(); got != 1 {
			t.Fatalf("expected exactly 1 call (fail fast on base), got %d", got)
		}
	})

	t.Run(
		"unrecognized head ref fails after verifying base, before any gathering",
		func(t *testing.T) {
			tm, gitBase, _ := newTaskSetup()
			gitBase.SetExecCommandResults(
				commands.ExecCommandResult("abc1234\n", "", nil),
				commands.ExecCommandResult(
					"", "fatal: Needed a single revision", fmt.Errorf("exit 128"),
				),
			)

			_, err := tm.ReviewPackage("main", "bogus", "")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), `unrecognized ref "bogus"`) {
				t.Fatalf("expected error to name the bad ref, got: %v", err)
			}
			if got := gitBase.GetExecCommandCallCount(); got != 2 {
				t.Fatalf("expected exactly 2 calls (base verify + head verify), got %d", got)
			}
		},
	)

	t.Run("no included files: skips the diff call entirely", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("abc1234\n", "", nil),
			commands.ExecCommandResult("def5678\n", "", nil),
			commands.ExecCommandResult("abc1234\t2026-07-20\tchore: bump lockfile\n", "", nil),
			commands.ExecCommandResult("40\t12\tgo.sum\n", "", nil),
			commands.ExecCommandResult("M\tgo.sum\n", "", nil),
		)

		out, err := tm.ReviewPackage("main", "feat", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "No file changes in range.") {
			t.Fatalf("expected no-file-changes sentinel, got: %q", out)
		}
		if !strings.Contains(out, "go.sum (+40/-12)") {
			t.Fatalf("expected exclusion note, got: %q", out)
		}
		if got := gitBase.GetExecCommandCallCount(); got != 5 {
			t.Fatalf("expected exactly 5 calls (no diff call made), got %d", got)
		}
	})

	t.Run("no commits, no files: both sentinels, no diff call", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("abc1234\n", "", nil),
			commands.ExecCommandResult("abc1234\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		out, err := tm.ReviewPackage("HEAD", "HEAD", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "range: HEAD..HEAD\n" +
			"commits:\n" +
			"No commits in range.\n" +
			"files (0):\n" +
			"No file changes in range."
		if out != want {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("--file bypasses exclusions and the log/stat gathering", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("abc1234\n", "", nil),
			commands.ExecCommandResult("def5678\n", "", nil),
			commands.ExecCommandResult("diff --git a/go.sum b/go.sum\n+entry\n", "", nil),
		)

		out, err := tm.ReviewPackage("main", "feat", "go.sum")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "diff --git a/go.sum b/go.sum\n+entry\n" {
			t.Fatalf("unexpected output: %q", out)
		}

		if got := gitBase.GetExecCommandCallCount(); got != 3 {
			t.Fatalf("expected exactly 3 calls (2 verifies + 1 diff), got %d", got)
		}
		diffCall := gitBase.ExecCommandCalls[2]
		if len(diffCall.Args) != 5 || diffCall.Args[0] != "diff" || diffCall.Args[1] != "-U10" ||
			diffCall.Args[2] != "main..feat" || diffCall.Args[3] != "--" ||
			diffCall.Args[4] != "go.sum" {
			t.Fatalf("expected file passed as its own argv element, got: %v", diffCall.Args)
		}
	})

	t.Run("--file not in range yields sentinel", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("abc1234\n", "", nil),
			commands.ExecCommandResult("def5678\n", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		out, err := tm.ReviewPackage("main", "feat", "unrelated.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "No changes for unrelated.go in main..feat." {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("--file still verifies both refs first", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"", "fatal: Needed a single revision", fmt.Errorf("exit 128"),
			),
		)

		_, err := tm.ReviewPackage("bogus", "feat", "some/file.go")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), `unrecognized ref "bogus"`) {
			t.Fatalf("expected error to name the bad ref, got: %v", err)
		}
		if got := gitBase.GetExecCommandCallCount(); got != 1 {
			t.Fatalf("expected exactly 1 call (fail fast, no diff attempted), got %d", got)
		}
	})
}
