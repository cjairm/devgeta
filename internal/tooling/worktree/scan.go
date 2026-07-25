// Filesystem scan for the worktree create-flow's repo picker: walks the
// user's configured search_paths looking for git repositories, so the
// picker can offer repos beyond whatever cwd/cursor/recent/zoxide already
// know about. See docs/plans/cycles/2026-07-17-wt-ui-repo-scan-and-layouts.md
// for the scanner validation rules this file implements.

package worktree

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/logger"
)

// defaultScanDepth is used whenever scan_depth is unset, zero, or negative.
// Disabling the scan entirely is done only by leaving search_paths empty,
// so there is exactly one off-switch — scan_depth never doubles as one.
const defaultScanDepth = 4

// excludedScanComponents are directory names the walk never descends into:
// node_modules/vendor/target/dist/.cache are large dependency/build trees
// unlikely to contain repos worth offering, and .git is the marker itself
// (once found, its parent is already recorded and the whole subtree is
// pruned — see scanRoot). This list applies only to directories discovered
// while walking a search-path root, not to the roots themselves: a user who
// points search_paths directly at ".../vendor" means it.
var excludedScanComponents = map[string]bool{
	"node_modules": true,
	".cache":       true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	".git":         true,
}

// scanRepos walks searchPaths, up to maxDepth directory levels below each
// root, looking for git repositories (a directory with a ".git" entry) to
// offer as candidates in the worktree create-flow's repo picker.
//
// Roots are canonicalized (config.CanonicalRepoPath) and deduped before
// walking: a root nested inside another configured root is dropped, since
// walking the outer root already reaches everything under the inner one. A
// missing, non-directory, or unreadable search-path entry is skipped with a
// debug log — never an error — so one stale entry never blanks the other
// configured paths. scan_depth <= 0 defaults to defaultScanDepth. Walk
// errors on an individual subtree (e.g. permissions) skip that subtree and
// the scan continues.
func scanRepos(searchPaths []string, maxDepth int) []string {
	if maxDepth <= 0 {
		maxDepth = defaultScanDepth
	}

	roots := dedupeNestedRoots(canonicalizeRoots(searchPaths))

	var repos []string
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			// Stale/typo'd config entry: never surface this as a TUI
			// status or per-run warning (it would nag on every picker
			// open), and never let it blank the other search paths.
			logger.L().Debugw("scan: skipping search path", "path", root, "error", err)
			continue
		}
		repos = append(repos, scanRoot(root, maxDepth)...)
	}
	return repos
}

// canonicalizeRoots resolves every configured search path to the one
// canonical form config.CanonicalRepoPath produces elsewhere in this
// package, so the nesting/dedup comparisons below compare consistent
// strings regardless of how the user wrote the path (~-expansion, trailing
// slash, symlink, ..).
func canonicalizeRoots(searchPaths []string) []string {
	canonical := make([]string, 0, len(searchPaths))
	for _, p := range searchPaths {
		canonical = append(canonical, config.CanonicalRepoPath(p))
	}
	return canonical
}

// dedupeNestedRoots drops any root equal to, or nested inside, another
// configured root, keeping the outermost. Walking the outer root already
// reaches everything below the inner one, so walking both would re-walk
// the same subtree and could hand the same repo back twice before
// RepoCandidates' own dedupe (a later step) ever gets a chance to run.
func dedupeNestedRoots(roots []string) []string {
	sorted := make([]string, len(roots))
	copy(sorted, roots)
	// Shortest path first: an ancestor directory's string is always a
	// prefix-length-or-shorter of any of its descendants, so processing in
	// this order guarantees an ancestor is already in `accepted` by the
	// time a descendant is checked against it.
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) < len(sorted[j]) })

	var accepted []string
	for _, r := range sorted {
		nested := false
		for _, a := range accepted {
			if r == a || strings.HasPrefix(r, a+string(filepath.Separator)) {
				nested = true
				break
			}
		}
		if !nested {
			accepted = append(accepted, r)
		}
	}
	return accepted
}

// scanRoot walks a single, already-validated, canonical root and returns
// the canonicalized git repo roots found within maxDepth levels below it.
func scanRoot(root string, maxDepth int) []string {
	var found []string

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Per-subtree error tolerance: a permission error (or similar)
			// reading one directory shouldn't abort the rest of the scan.
			// Returning nil (rather than the error) tells WalkDir to move
			// on to the next sibling instead of stopping entirely.
			logger.L().Debugw("scan: skipping subtree", "path", path, "error", err)
			return nil
		}
		// d is Lstat-based (os.ReadDir/fs.DirEntry never follows a
		// symlink to stat it), so a symlinked directory entry reports
		// IsDir() == false here regardless of what it points to — this
		// guard is what keeps the walk from ever descending into one.
		// filepath.WalkDir itself never calls ReadDir on an entry unless
		// this callback both leaves it unhandled (no error, no SkipDir)
		// AND d.IsDir() is true, so a symlink can never be followed and a
		// cycle (e.g. a/link_to_a -> a) structurally cannot hang the
		// scan: WalkDir simply never recurses into the link in the first
		// place. The configured root is unaffected by any of this — it
		// was already resolved by config.CanonicalRepoPath before the
		// walk started, so a root that is itself a symlink is scanned
		// once at its resolved, non-symlink location.
		if !d.IsDir() {
			return nil
		}

		isRoot := path == root

		if !isRoot && excludedScanComponents[d.Name()] {
			return filepath.SkipDir
		}

		// Peek for a ".git" entry directly, rather than waiting for
		// WalkDir to visit it as its own tree entry: .git is normally a
		// directory, and SkipDir returned from a directory's own callback
		// only prunes that directory's contents, not its siblings. If we
		// instead waited to notice ".git" as a sibling entry inside path,
		// returning SkipDir on it would fail to prune a sibling like
		// "path/nested-repo/" — defeating the "prune the whole subtree"
		// rule (nested repos/submodules below a repo root must not be
		// listed). Checking here, before WalkDir ever descends into path,
		// lets one SkipDir prune everything below path in a single step.
		if hasGitEntry(path) {
			found = append(found, config.CanonicalRepoPath(path))
			return filepath.SkipDir
		}

		// A directory at maxDepth is as deep as we look for a repo: any
		// child of it would sit at maxDepth+1, one level past the bound,
		// so stop descending here rather than reading its contents only
		// to have the depth check discard every one of them one level
		// down. This is the only depth check needed — no directory the
		// walk visits can ever exceed maxDepth, since its parent already
		// stopped descent here.
		if scanDepth(root, path) >= maxDepth {
			return filepath.SkipDir
		}

		return nil
	})
	if walkErr != nil {
		logger.L().Debugw("scan: walk failed", "root", root, "error", walkErr)
	}
	return found
}

// scanDepth reports how many directory levels path sits below root, with
// root itself at depth 0 (root/a is depth 1, root/a/b is depth 2). A repo's
// depth is the depth of the directory containing its .git entry — not the
// .git entry's own depth — since maxDepth's job is to bound how far below
// the root we look for repo directories, not how far below the repo
// directory .git itself happens to sit.
func scanDepth(root, path string) int {
	if path == root {
		return 0
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		// path is always produced by WalkDir(root, ...), so it is always
		// under root; this is unreachable in practice, but returning 0
		// rather than panicking keeps a defensive fallback harmless.
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// hasGitEntry reports whether dir has an immediate ".git" child. Checked
// with Lstat (not Stat) so a ".git" that is itself a symlink still counts
// as the marker without following it. Both directory .git (a normal repo)
// and file .git (a worktree or submodule pointer) count — the task only
// needs the boundary, not which form it takes.
func hasGitEntry(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
