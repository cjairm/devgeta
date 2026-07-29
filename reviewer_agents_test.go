package main

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// TestBuiltinReviewersAgentNamesMatchEmbeddedAgentFiles is the companion to
// worktree.TestBuiltinReviewersAgentNamesMatchAgentFiles (internal/tooling/
// worktree/layout_test.go), which reads configs/shared/agents/ off disk
// because that package cannot import ConfigsFS (declared here in package
// main; importing it from internal/tooling/worktree would be an import
// cycle). This test instead reads the same directory through the embedded
// ConfigsFS, so a file that fails to embed (see main.go's `//go:embed
// all:configs`) is still caught even though the on-disk test alone would
// pass.
func TestBuiltinReviewersAgentNamesMatchEmbeddedAgentFiles(t *testing.T) {
	entries, err := fs.ReadDir(ConfigsFS, "configs/shared/agents")
	if err != nil {
		t.Fatalf("failed to read embedded configs/shared/agents: %v", err)
	}

	embedded := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		embedded[strings.TrimSuffix(entry.Name(), ".md")] = true
	}

	names := worktree.ReviewerAgentNames()
	if len(names) == 0 {
		t.Fatal("expected worktree.ReviewerAgentNames() to return at least one name, got none")
	}
	for _, name := range names {
		if !embedded[name] {
			t.Errorf(
				"reviewer agent %q has no matching file in embedded configs/shared/agents",
				name,
			)
		}
	}
}
