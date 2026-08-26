package contextreport

import (
	"path/filepath"
	"regexp"

	"github.com/cjairm/devgeta/internal/apps/git"
)

// mainCheckoutRoot resolves the main checkout's root directory for repoDir
// — the common git directory's parent, so a worktree and the main checkout
// of the same repo resolve to the same root (the auto-memory directory is
// shared across worktrees — Step 0-confirmed).
func mainCheckoutRoot(repoDir string, gitApp *git.Git) (string, error) {
	commonDir, err := gitApp.CommonDirIn(repoDir)
	if err != nil {
		return "", err
	}
	return filepath.Dir(commonDir), nil
}

// nonSlugChar matches any character slugifyPath treats as a separator.
var nonSlugChar = regexp.MustCompile(`[^A-Za-z0-9]`)

// slugifyPath mirrors Claude Code's own project-directory naming for
// ~/.claude/projects/<slug>/. Verified during Step 7's live-comparison
// check against this session's own memory path from the system prompt:
// /Users/jair.mendez/Documents/golang/devgeta ->
// -Users-jair-mendez-Documents-golang-devgeta — note "jair.mendez" losing
// its dot, not just the path's own "/" separators. A first draft that only
// replaced "/" reproduced everything except that dot and silently
// under-reported (reported "no auto-memory found" for a file that exists).
// Not documented anywhere upstream; replacing every non-alphanumeric
// character is the simplest rule that reproduces the one real example
// checked, and the safe direction to be wrong in is under- rather than
// over-matching an unrelated directory.
func slugifyPath(absPath string) string {
	return nonSlugChar.ReplaceAllString(absPath, "-")
}
