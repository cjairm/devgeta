package task

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

// --- Pure formatter / parser tests (fixtures, no mocks) ---

func TestParseNumstat(t *testing.T) {
	t.Run("parses regular and binary lines", func(t *testing.T) {
		raw := "120\t30\tinternal/tooling/task/task.go\n" +
			"200\t0\tinternal/tooling/task/scope.go\n" +
			"-\t-\tassets/logo.png\n"
		got, err := parseNumstat(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(got))
		}
		if got[0].Path != "internal/tooling/task/task.go" || got[0].Added != 120 ||
			got[0].Removed != 30 {
			t.Fatalf("unexpected entry 0: %+v", got[0])
		}
		if got[2].Path != "assets/logo.png" || !got[2].Binary {
			t.Fatalf("expected entry 2 to be binary: %+v", got[2])
		}
	})

	t.Run("empty input yields nil", func(t *testing.T) {
		got, err := parseNumstat("   \n  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("malformed line errors", func(t *testing.T) {
		if _, err := parseNumstat("not-a-numstat-line"); err == nil {
			t.Fatal("expected error for malformed line")
		}
	})

	t.Run("non-numeric count errors", func(t *testing.T) {
		if _, err := parseNumstat("abc\t1\tfile.go"); err == nil {
			t.Fatal("expected error for non-numeric added count")
		}
	})
}

func TestParseNameStatus(t *testing.T) {
	t.Run("parses statuses", func(t *testing.T) {
		raw := "M\tinternal/tooling/task/task.go\nA\tinternal/tooling/task/scope.go\n"
		got, err := parseNameStatus(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["internal/tooling/task/task.go"] != "M" ||
			got["internal/tooling/task/scope.go"] != "A" {
			t.Fatalf("unexpected statuses: %+v", got)
		}
	})

	t.Run("truncates multi-char status to leading letter", func(t *testing.T) {
		got, err := parseNameStatus("R100\told.go\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["old.go"] != "R" {
			t.Fatalf("expected status 'R', got %q", got["old.go"])
		}
	})

	t.Run("empty input yields empty map", func(t *testing.T) {
		got, err := parseNameStatus("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty map, got %+v", got)
		}
	})

	t.Run("malformed line errors", func(t *testing.T) {
		if _, err := parseNameStatus("nofield"); err == nil {
			t.Fatal("expected error for malformed line")
		}
	})
}

func TestParseAheadBehind(t *testing.T) {
	t.Run("parses behind and ahead", func(t *testing.T) {
		behind, ahead, err := parseAheadBehind("3\t5\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if behind != 3 || ahead != 5 {
			t.Fatalf("expected behind=3 ahead=5, got behind=%d ahead=%d", behind, ahead)
		}
	})

	t.Run("wrong field count errors", func(t *testing.T) {
		if _, _, err := parseAheadBehind("3"); err == nil {
			t.Fatal("expected error for wrong field count")
		}
	})

	t.Run("non-numeric field errors", func(t *testing.T) {
		if _, _, err := parseAheadBehind("x\t5"); err == nil {
			t.Fatal("expected error for non-numeric field")
		}
	})
}

func TestParseCommitLog(t *testing.T) {
	t.Run("parses multiple commits with bodies", func(t *testing.T) {
		raw := "abc123" + commitFieldSep + "2026-07-14" + commitFieldSep +
			"feat(task): add review-scope" + commitFieldSep +
			"Collapses a 6-call orientation dance into one." + commitRecordSep + "\n" +
			"def456" + commitFieldSep + "2026-07-15" + commitFieldSep +
			"test(task): cover offline fetch" + commitFieldSep + "" + commitRecordSep + "\n"
		got, err := parseCommitLog(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 commits, got %d: %+v", len(got), got)
		}
		want0 := commit{
			SHA: "abc123", Date: "2026-07-14", Subject: "feat(task): add review-scope",
			Body: "Collapses a 6-call orientation dance into one.",
		}
		if got[0] != want0 {
			t.Fatalf("unexpected commit 0: %+v, want %+v", got[0], want0)
		}
		want1 := commit{
			SHA:     "def456",
			Date:    "2026-07-15",
			Subject: "test(task): cover offline fetch",
		}
		if got[1] != want1 {
			t.Fatalf("unexpected commit 1: %+v, want %+v", got[1], want1)
		}
	})

	t.Run("parses a multi-line body", func(t *testing.T) {
		raw := "abc123" + commitFieldSep + "2026-07-14" + commitFieldSep +
			"feat(task): add review-scope" + commitFieldSep +
			"line one\nline two" + commitRecordSep + "\n"
		got, err := parseCommitLog(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].Body != "line one\nline two" {
			t.Fatalf("unexpected result: %+v", got)
		}
	})

	t.Run("empty input yields nil", func(t *testing.T) {
		got, err := parseCommitLog("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("malformed record errors", func(t *testing.T) {
		raw := "abc123" + commitFieldSep + "2026-07-14" + commitRecordSep + "\n"
		if _, err := parseCommitLog(raw); err == nil {
			t.Fatal("expected error for malformed record")
		}
	})
}

func TestPartitionExcluded(t *testing.T) {
	changes := []fileChange{
		{Path: "internal/tooling/task/task.go", Status: "M", Added: 120, Removed: 30},
		{Path: "go.sum", Status: "M", Added: 40, Removed: 12},
		{Path: "packages/app/package-lock.json", Status: "M", Added: 5, Removed: 1},
	}
	reviewable, excluded := partitionExcluded(changes)
	if len(reviewable) != 1 || reviewable[0].Path != "internal/tooling/task/task.go" {
		t.Fatalf("unexpected reviewable: %+v", reviewable)
	}
	if len(excluded) != 2 {
		t.Fatalf("expected 2 excluded, got %+v", excluded)
	}
}

func TestFormatFileStats(t *testing.T) {
	t.Run("renders rows and running totals", func(t *testing.T) {
		got := formatFileStats([]fileChange{
			{Path: "internal/tooling/task/task.go", Status: "M", Added: 120, Removed: 30},
			{Path: "internal/tooling/task/scope.go", Status: "A", Added: 200, Removed: 0},
		})
		want := "M  internal/tooling/task/task.go  +120/-30\n" +
			"A  internal/tooling/task/scope.go  +200/-0\n" +
			"total: +320/-30"
		if got != want {
			t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
		}
	})

	t.Run("binary file renders without counts and doesn't affect totals", func(t *testing.T) {
		got := formatFileStats([]fileChange{
			{Path: "assets/logo.png", Status: "A", Binary: true},
			{Path: "x", Status: "M", Added: 5, Removed: 1},
		})
		want := "A  assets/logo.png  binary\n" +
			"M  x  +5/-1\n" +
			"total: +5/-1"
		if got != want {
			t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
		}
	})

	t.Run("empty list yields just the zero total, no rows", func(t *testing.T) {
		got := formatFileStats(nil)
		if got != "total: +0/-0" {
			t.Fatalf("unexpected output: %q", got)
		}
	})
}

func TestFormatExclusionNotes(t *testing.T) {
	t.Run("empty list yields empty string", func(t *testing.T) {
		if got := formatExclusionNotes(nil, "dg task branch-diff --file <path>"); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run(
		"renders binary and counted notes joined by comma, using the given hint",
		func(t *testing.T) {
			got := formatExclusionNotes([]fileChange{
				{Path: "go.sum", Added: 40, Removed: 12},
				{Path: "bun.lockb", Binary: true},
			}, "dg task review-package main feat --file <path>")
			want := "excluded (see `dg task review-package main feat --file <path>` to inspect): " +
				"go.sum (+40/-12), bun.lockb (binary)"
			if got != want {
				t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
			}
		},
	)
}

func TestFormatReviewScope(t *testing.T) {
	t.Run("detached HEAD sentinel", func(t *testing.T) {
		got := formatReviewScope(scopeData{Detached: true, DetachedSHA: "abc1234"})
		want := "Detached HEAD at abc1234 — no branch to compare. Check out a branch or name a target."
		if got != want {
			t.Fatalf("unexpected output: %q", got)
		}
	})

	t.Run("on default branch sentinel", func(t *testing.T) {
		got := formatReviewScope(scopeData{OnDefaultBranch: true, DefaultBranch: "main"})
		want := "On main — no branch to compare. Review uncommitted changes or name a target."
		if got != want {
			t.Fatalf("unexpected output: %q", got)
		}
	})

	t.Run("full report with excluded file", func(t *testing.T) {
		got := formatReviewScope(scopeData{
			CurrentBranch: "feat/x",
			DefaultBranch: "main",
			Ahead:         2,
			Behind:        0,
			Commits: []commit{
				{SHA: "abc123", Date: "2026-07-14", Subject: "feat(task): add review-scope"},
				{SHA: "def456", Date: "2026-07-15", Subject: "test(task): cover offline fetch"},
			},
			Files: []fileChange{
				{Path: "internal/tooling/task/task.go", Status: "M", Added: 120, Removed: 30},
				{Path: "internal/tooling/task/scope.go", Status: "A", Added: 200, Removed: 0},
			},
			Excluded: []fileChange{
				{Path: "go.sum", Status: "M", Added: 40, Removed: 12},
			},
		})
		want := "branch: feat/x -> main (default)  [ahead 2, behind 0]\n" +
			"commits:\n" +
			"- abc123 2026-07-14 feat(task): add review-scope\n" +
			"- def456 2026-07-15 test(task): cover offline fetch\n" +
			"files (2):\n" +
			"M  internal/tooling/task/task.go  +120/-30\n" +
			"A  internal/tooling/task/scope.go  +200/-0\n" +
			"total: +320/-30\n" +
			"excluded (see `dg task branch-diff --file <path>` to inspect): go.sum (+40/-12)"
		if got != want {
			t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
		}
	})

	t.Run("bodies flag renders indented body lines", func(t *testing.T) {
		got := formatReviewScope(scopeData{
			CurrentBranch: "feat/x",
			DefaultBranch: "main",
			Ahead:         1,
			Bodies:        true,
			Commits: []commit{
				{
					SHA:     "abc123",
					Date:    "2026-07-14",
					Subject: "feat(task): add review-scope",
					Body:    "Collapses a 6-call orientation dance into one.\nSee cycle doc for rationale.",
				},
				{SHA: "def456", Date: "2026-07-15", Subject: "test(task): cover offline fetch"},
			},
		})
		want := "branch: feat/x -> main (default)  [ahead 1, behind 0]\n" +
			"commits:\n" +
			"- abc123 2026-07-14 feat(task): add review-scope\n" +
			"    Collapses a 6-call orientation dance into one.\n" +
			"    See cycle doc for rationale.\n" +
			"- def456 2026-07-15 test(task): cover offline fetch\n" +
			"files (0):\n" +
			"total: +0/-0"
		if got != want {
			t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
		}
	})

	t.Run("blank line within a body renders without trailing whitespace", func(t *testing.T) {
		got := formatReviewScope(scopeData{
			CurrentBranch: "feat/x",
			DefaultBranch: "main",
			Bodies:        true,
			Commits: []commit{
				{
					SHA: "abc123", Date: "2026-07-14", Subject: "feat(task): add review-scope",
					Body: "first paragraph\n\nCo-Authored-By: someone <x@example.com>",
				},
			},
		})
		if !strings.Contains(got, "    first paragraph\n\n    Co-Authored-By") {
			t.Fatalf("expected blank body line with no trailing whitespace, got: %q", got)
		}
	})

	t.Run("bodies flag false does not render bodies even if present", func(t *testing.T) {
		got := formatReviewScope(scopeData{
			CurrentBranch: "feat/x",
			DefaultBranch: "main",
			Commits: []commit{
				{
					SHA:     "abc123",
					Date:    "2026-07-14",
					Subject: "feat(task): add review-scope",
					Body:    "hidden body",
				},
			},
		})
		if strings.Contains(got, "hidden body") {
			t.Fatalf("expected body to be hidden when Bodies is false, got: %q", got)
		}
	})

	t.Run("marks fetch failure", func(t *testing.T) {
		got := formatReviewScope(scopeData{
			CurrentBranch: "feat/x",
			DefaultBranch: "main",
			FetchFailed:   true,
		})
		if !strings.Contains(got, "(fetch failed — comparing against local refs)") {
			t.Fatalf("expected fetch-failed marker, got: %q", got)
		}
	})

	t.Run("no commits renders (none)", func(t *testing.T) {
		got := formatReviewScope(scopeData{CurrentBranch: "feat/x", DefaultBranch: "main"})
		if !strings.Contains(got, "commits:\n(none)\n") {
			t.Fatalf("expected '(none)' for empty commits, got: %q", got)
		}
	})

	t.Run("binary file renders without counts", func(t *testing.T) {
		got := formatReviewScope(scopeData{
			CurrentBranch: "feat/x",
			DefaultBranch: "main",
			Files:         []fileChange{{Path: "assets/logo.png", Status: "A", Binary: true}},
		})
		if !strings.Contains(got, "A  assets/logo.png  binary") {
			t.Fatalf("expected binary file row, got: %q", got)
		}
	})
}

// --- Orchestration tests (mocked git.Base, no real commands) ---

func TestReviewScope(t *testing.T) {
	t.Run("happy path renders full report", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),              // fetch origin
			commands.ExecCommandResult("feat/x\n", "", nil),      // branch --show-current
			commands.ExecCommandResult("origin/main\n", "", nil), // symbolic-ref (default branch)
			commands.ExecCommandResult("abc123\n", "", nil),      // merge-base
			commands.ExecCommandResult("0\t2\n", "", nil),        // rev-list --left-right --count
			commands.ExecCommandResult(
				"abc123"+commitFieldSep+"2026-07-14"+commitFieldSep+
					"feat(task): add review-scope"+commitFieldSep+commitRecordSep+"\n"+
					"def456"+commitFieldSep+"2026-07-15"+commitFieldSep+
					"test(task): cover offline fetch"+commitFieldSep+commitRecordSep+"\n",
				"", nil,
			), // log --format=commitLogFormat --reverse
			commands.ExecCommandResult(
				"120\t30\tinternal/tooling/task/task.go\n"+
					"200\t0\tinternal/tooling/task/scope.go\n"+
					"40\t12\tgo.sum\n", "", nil,
			), // diff --numstat --no-renames
			commands.ExecCommandResult(
				"M\tinternal/tooling/task/task.go\n"+
					"A\tinternal/tooling/task/scope.go\n"+
					"M\tgo.sum\n", "", nil,
			), // diff --name-status --no-renames
		)

		out, err := tm.ReviewScope(false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantLines := []string{
			"branch: feat/x -> main (default)  [ahead 2, behind 0]",
			"- abc123 2026-07-14 feat(task): add review-scope",
			"- def456 2026-07-15 test(task): cover offline fetch",
			"files (2):",
			"M  internal/tooling/task/task.go  +120/-30",
			"A  internal/tooling/task/scope.go  +200/-0",
			"total: +320/-30",
			"excluded (see `dg task branch-diff --file <path>` to inspect): go.sum (+40/-12)",
		}
		for _, want := range wantLines {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("bodies flag renders commit bodies", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("feat/x\n", "", nil),
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("abc123\n", "", nil),
			commands.ExecCommandResult("0\t1\n", "", nil),
			commands.ExecCommandResult(
				"abc123"+commitFieldSep+"2026-07-14"+commitFieldSep+
					"feat(task): add review-scope"+commitFieldSep+
					"Collapses a 6-call orientation dance into one."+commitRecordSep+"\n",
				"", nil,
			),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		out, err := tm.ReviewScope(true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantLines := []string{
			"- abc123 2026-07-14 feat(task): add review-scope",
			"    Collapses a 6-call orientation dance into one.",
		}
		for _, want := range wantLines {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("fetch failure does not abort, marks output", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: unreachable", fmt.Errorf("dial tcp: timeout")),
			commands.ExecCommandResult("feat/x\n", "", nil),
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("abc123\n", "", nil),
			commands.ExecCommandResult("0\t0\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		out, err := tm.ReviewScope(false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "(fetch failed — comparing against local refs)") {
			t.Fatalf("expected fetch-failed marker, got:\n%s", out)
		}
	})

	t.Run("on default branch returns sentinel without diffing", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),              // fetch origin
			commands.ExecCommandResult("main\n", "", nil),        // branch --show-current
			commands.ExecCommandResult("origin/main\n", "", nil), // symbolic-ref (default branch)
		)

		out, err := tm.ReviewScope(false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "On main — no branch to compare. Review uncommitted changes or name a target."
		if out != want {
			t.Fatalf("unexpected output: %q", out)
		}
		if got := gitBase.GetExecCommandCallCount(); got != 3 {
			t.Fatalf("expected exactly 3 git calls (no diffing), got %d", got)
		}
	})

	t.Run("detached HEAD returns sentinel without diffing", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil), // fetch origin
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // branch --show-current -> empty (detached)
			commands.ExecCommandResult("abc1234\n", "", nil), // rev-parse --short HEAD
		)

		out, err := tm.ReviewScope(false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "Detached HEAD at abc1234 — no branch to compare. Check out a branch or name a target."
		if out != want {
			t.Fatalf("unexpected output: %q", out)
		}
		if got := gitBase.GetExecCommandCallCount(); got != 3 {
			t.Fatalf("expected exactly 3 git calls (no diffing), got %d", got)
		}
	})
}
