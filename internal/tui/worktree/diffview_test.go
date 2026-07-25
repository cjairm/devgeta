package tuiworktree

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cjairm/devgeta/internal/tooling/task"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func plainRewrite(t *testing.T, content string, stats []task.FileStat) string {
	t.Helper()
	return ansi.Strip(rewriteFileHeaders(content, stats, tuicomponents.NewPalette()))
}

func TestRewriteFileHeaders(t *testing.T) {
	t.Run("collapses preamble into one header with stats", func(t *testing.T) {
		in := strings.Join([]string{
			"diff --git a/main.go b/main.go",
			"index f780ad5..08dc1e2 100644",
			"--- a/main.go",
			"+++ b/main.go",
			"@@ -1,2 +1,2 @@",
			"-old",
			"+new",
		}, "\n")
		got := plainRewrite(t, in, []task.FileStat{{Path: "main.go", Added: 5, Removed: 2}})

		if !strings.Contains(got, fileHeaderPrefix+"main.go +5 -2") {
			t.Errorf("expected styled header with stats, got:\n%s", got)
		}
		for _, raw := range []string{"diff --git", "index ", "--- a/", "+++ b/"} {
			if strings.Contains(got, raw) {
				t.Errorf("raw preamble %q should be gone, got:\n%s", raw, got)
			}
		}
		if !strings.Contains(got, "@@ -1,2 +1,2 @@") || !strings.Contains(got, "-old") {
			t.Errorf("hunk content must pass through, got:\n%s", got)
		}
	})

	t.Run("detects headers under ANSI color", func(t *testing.T) {
		in := strings.Join([]string{
			"\x1b[1mdiff --git a/x.go b/x.go\x1b[m",
			"\x1b[1mindex 111..222 100644\x1b[m",
			"\x1b[1m--- a/x.go\x1b[m",
			"\x1b[1m+++ b/x.go\x1b[m",
			"@@ -1 +1 @@",
		}, "\n")
		got := plainRewrite(t, in, nil)
		if !strings.Contains(got, fileHeaderPrefix+"x.go") {
			t.Errorf("expected header despite ANSI codes, got:\n%s", got)
		}
		if strings.Contains(got, "diff --git") {
			t.Errorf("colored preamble should be collapsed, got:\n%s", got)
		}
	})

	t.Run("rename renders old → new", func(t *testing.T) {
		in := strings.Join([]string{
			"diff --git a/old.go b/new.go",
			"similarity index 95%",
			"rename from old.go",
			"rename to new.go",
		}, "\n")
		got := plainRewrite(t, in, nil)
		if !strings.Contains(got, "old.go → new.go") {
			t.Errorf("expected rename label, got:\n%s", got)
		}
	})

	t.Run("new and deleted files are tagged", func(t *testing.T) {
		in := strings.Join([]string{
			"diff --git a/added.go b/added.go",
			"new file mode 100644",
			"index 0000000..1111111",
			"--- /dev/null",
			"+++ b/added.go",
			"@@ -0,0 +1 @@",
			"+hi",
			"diff --git a/gone.go b/gone.go",
			"deleted file mode 100644",
			"index 1111111..0000000",
			"--- a/gone.go",
			"+++ /dev/null",
			"@@ -1 +0,0 @@",
			"-bye",
		}, "\n")
		got := plainRewrite(t, in, nil)
		if !strings.Contains(got, "added.go (new)") {
			t.Errorf("expected (new) tag, got:\n%s", got)
		}
		if !strings.Contains(got, "gone.go (deleted)") {
			t.Errorf("expected (deleted) tag with old path, got:\n%s", got)
		}
	})

	t.Run("binary notice passes through after header", func(t *testing.T) {
		in := strings.Join([]string{
			"diff --git a/img.png b/img.png",
			"index 111..222 100644",
			"Binary files a/img.png and b/img.png differ",
		}, "\n")
		got := plainRewrite(t, in, []task.FileStat{{Path: "img.png", Binary: true}})
		if !strings.Contains(got, "Binary files a/img.png and b/img.png differ") {
			t.Errorf("binary notice must remain, got:\n%s", got)
		}
		if strings.Contains(got, "+0 -0") {
			t.Errorf("binary files must not render numeric stats, got:\n%s", got)
		}
	})

	t.Run("non-diff content is untouched", func(t *testing.T) {
		in := "No changes in main..worktree.\nUntracked files:\n  notes.txt"
		if got := plainRewrite(t, in, nil); got != in {
			t.Errorf("expected passthrough, got:\n%s", got)
		}
	})
}

func TestDiffFileHeaderLines(t *testing.T) {
	in := strings.Join([]string{
		"diff --git a/a.go b/a.go",
		"index 1..2 100644",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -1 +1 @@",
		"-x",
		"+y",
		"diff --git a/b.go b/b.go",
		"index 3..4 100644",
		"--- a/b.go",
		"+++ b/b.go",
		"@@ -1 +1 @@",
		" ctx",
	}, "\n")
	out := rewriteFileHeaders(in, nil, tuicomponents.NewPalette())
	lines := diffFileHeaderLines(out)
	if len(lines) != 2 {
		t.Fatalf("expected 2 header lines, got %d (%v)", len(lines), lines)
	}
	split := strings.Split(ansi.Strip(out), "\n")
	for _, ln := range lines {
		if !strings.HasPrefix(split[ln], fileHeaderPrefix) {
			t.Errorf("line %d should be a header, got %q", ln, split[ln])
		}
	}
}
