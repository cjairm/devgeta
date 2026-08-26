package contextreport

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func TestResolveImportsRecursiveCountsTheRootFile(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "CLAUDE.md")
	writeFile(t, root, "just a plain file\n")

	visited := map[string]bool{}
	items := resolveImportsRecursive(root, 1, visited, dir)
	if len(items) != 1 || items[0].Path != root || items[0].Bytes != len("just a plain file\n") {
		t.Fatalf("items = %+v, want one item for %s", items, root)
	}
}

func TestResolveImportsRecursiveFollowsAnImport(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "CLAUDE.md")
	imported := filepath.Join(dir, "docs", "extra.md")
	writeFile(t, root, "See @docs/extra.md for more.\n")
	writeFile(t, imported, "extra content\n")

	visited := map[string]bool{}
	items := resolveImportsRecursive(root, 1, visited, dir)
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2 (root + imported)", items)
	}
	if items[1].Path != imported {
		t.Errorf("items[1].Path = %q, want %q", items[1].Path, imported)
	}
}

func TestResolveImportsRecursiveHandlesACycleWithoutHanging(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	writeFile(t, a, "@b.md\n")
	writeFile(t, b, "@a.md\n")

	visited := map[string]bool{}
	items := resolveImportsRecursive(a, 1, visited, dir)
	// Each file visited exactly once, however many times it's referenced.
	if len(items) != 2 {
		t.Fatalf("items = %+v, want exactly 2 (a and b, each once)", items)
	}
}

func TestResolveImportsRecursiveStopsAtMaxDepth(t *testing.T) {
	dir := t.TempDir()
	// A chain of 6 files, each importing the next — deeper than the 4-hop
	// cap (Claude Code's own documented limit, confirmed by Step 0).
	var files []string
	for i := 0; i < 6; i++ {
		files = append(files, filepath.Join(dir, string(rune('a'+i))+".md"))
	}
	for i := 0; i < len(files)-1; i++ {
		next := filepath.Base(files[i+1])
		writeFile(t, files[i], "@"+next+"\n")
	}
	writeFile(t, files[len(files)-1], "leaf\n")

	visited := map[string]bool{}
	items := resolveImportsRecursive(files[0], 1, visited, dir)
	if len(items) >= len(files) {
		t.Errorf(
			"items = %+v, expected the chain to be cut off before all %d files",
			items,
			len(files),
		)
	}
	if len(items) > maxImportDepth+1 {
		t.Errorf(
			"resolved %d files, want at most maxImportDepth+1 (%d)",
			len(items),
			maxImportDepth+1,
		)
	}
}

func TestResolveImportsRecursiveMissingFileIsSkippedSilently(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "CLAUDE.md")
	writeFile(t, root, "See @does/not/exist.md please.\n")

	visited := map[string]bool{}
	items := resolveImportsRecursive(root, 1, visited, dir)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want just the root (missing import skipped)", items)
	}
}

func TestAncestorDirsRootToLeafOrdersFromRootDown(t *testing.T) {
	dirs := ancestorDirsRootToLeaf("/a/b/c")
	want := []string{"/", "/a", "/a/b", "/a/b/c"}
	if len(dirs) != len(want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	for i := range want {
		if dirs[i] != want[i] {
			t.Errorf("dirs[%d] = %q, want %q", i, dirs[i], want[i])
		}
	}
}
