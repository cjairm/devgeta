// Branch-name encoding for journal filenames (ADR-0012 §5).
//
// The actual encode/decode logic lives in internal/tooling/branchstore, which
// handoff shares. These two exports stay in this package as thin wrappers
// because EncodeBranch is public API already consumed outside this package
// (internal/tooling/task/reviewrun.go), so removing it would ripple beyond
// this extraction's scope.

package reviewjournal

import "github.com/cjairm/devgeta/internal/tooling/branchstore"

// EncodeBranch returns the collision-free filename form of a branch name
// (without the ".md" suffix).
func EncodeBranch(branch string) string {
	return branchstore.EncodeName(branch)
}

// DecodeBranch reverses EncodeBranch, for listing journals by their original
// branch names (e.g. prune output).
func DecodeBranch(encoded string) (string, error) {
	return branchstore.DecodeName(encoded)
}
