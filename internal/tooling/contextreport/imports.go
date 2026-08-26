package contextreport

import (
	"os"
	"path/filepath"
)

// maxImportDepth is Claude Code's own documented @import recursion limit
// ("Imported files can recursively import other files, with a maximum
// depth of four hops" — confirmed by Step 0 against upstream docs).
const maxImportDepth = 4

// resolveImportsRecursive reads path, counts it, and follows every @import
// it contains up to maxImportDepth hops, skipping anything already in
// visited (guards both cycles and double-counting a file reachable from
// two places). A missing or unreadable file is skipped silently — an
// unresolved import is not this function's concern to report; devgeta
// cannot tell "this import intentionally names an optional file" apart
// from "this import is broken" without asking the agent itself.
//
// depth is 1-based: the root call passes 1, so maxImportDepth matches
// Claude Code's own "up to four hops beyond the file itself" framing.
func resolveImportsRecursive(path string, depth int, visited map[string]bool, home string) []Item {
	if depth > maxImportDepth+1 {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	if visited[abs] {
		return nil
	}
	visited[abs] = true

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}

	items := []Item{{Path: abs, Bytes: len(data)}}
	for _, ref := range extractImports(string(data)) {
		childPath := resolveImportPath(ref, filepath.Dir(abs), home)
		items = append(items, resolveImportsRecursive(childPath, depth+1, visited, home)...)
	}
	return items
}

// ancestorDirsRootToLeaf returns dir and every directory above it, ordered
// from the filesystem root down to dir itself — the order CLAUDE.md layers
// concatenate in (root-to-leaf, so a project instruction appears in
// context after the ones above it — Step 0's confirmed precedence).
func ancestorDirsRootToLeaf(dir string) []string {
	var dirs []string
	for {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// dirs is currently leaf-to-root; reverse in place for root-to-leaf.
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}
