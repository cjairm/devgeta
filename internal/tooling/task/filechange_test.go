package task

import (
	"testing"
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

func TestFormatExclusionNotes(t *testing.T) {
	t.Run("empty list yields empty string", func(t *testing.T) {
		if got := formatExclusionNotes(nil); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run(
		"renders binary and counted notes joined by comma",
		func(t *testing.T) {
			got := formatExclusionNotes([]fileChange{
				{Path: "go.sum", Added: 40, Removed: 12},
				{Path: "bun.lockb", Binary: true},
			})
			want := "excluded from this diff: " +
				"go.sum (+40/-12), bun.lockb (binary)"
			if got != want {
				t.Fatalf("unexpected output:\n%s\n---want---\n%s", got, want)
			}
		},
	)
}

// --- Orchestration tests (mocked git.Base, no real commands) ---
