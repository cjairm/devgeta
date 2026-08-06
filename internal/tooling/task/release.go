package task

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// releaseVersionPattern enforces devgeta's own tag policy (CLAUDE.md §9):
// tags are always vMAJOR.MINOR.PATCH — strict semver only, no prerelease or
// build-metadata suffixes.
var releaseVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// Release automates the CLAUDE.md §9 push-and-tag workflow: verify a clean
// working tree on the default branch, count commits ahead of
// origin/<default>, squash 2+ of them into one commit using messageFile,
// create an annotated tag with the same message, and push commit+tag
// together only when push is true.
//
// Every guard (version format, clean tree, default branch, message file,
// tag-not-exists) runs — in that order — before any mutation starts. reset
// --soft, the squash commit, the tag, and the push are the only
// state-changing steps, and each is hard to reverse once it runs, so a
// failure partway through reports exactly what state was left and the raw
// git command to finish or undo it by hand.
func (tm *TaskManager) Release(version, messageFile string, push bool) (string, error) {
	if err := validateReleaseVersion(version); err != nil {
		return "", err
	}
	if err := tm.checkCleanTree(); err != nil {
		return "", err
	}
	defaultBranch, err := tm.checkOnDefaultBranch()
	if err != nil {
		return "", err
	}
	if err := checkMessageFile(messageFile); err != nil {
		return "", err
	}
	if err := tm.checkTagAvailable(version); err != nil {
		return "", err
	}

	// --- all guards passed; mutations start here ---

	ahead, err := tm.releaseAheadCount(defaultBranch)
	if err != nil {
		return "", fmt.Errorf("release: %w", err)
	}

	squashed := 0
	if ahead >= 2 {
		if err := tm.Git.ExecuteCommand(
			"reset",
			"--soft",
			fmt.Sprintf("HEAD~%d", ahead),
		); err != nil {
			return "", fmt.Errorf(
				"release: git reset --soft HEAD~%d failed, no commits were changed: %w",
				ahead, err,
			)
		}
		if err := tm.Git.ExecuteCommand("commit", "-F", messageFile); err != nil {
			return "", fmt.Errorf(
				"release: commit failed after 'git reset --soft HEAD~%d'; your %d commits are staged "+
					"but uncommitted — run 'git commit -F %s' to finish, or 'git reset --hard ORIG_HEAD' "+
					"to undo the reset: %w",
				ahead,
				ahead,
				messageFile,
				err,
			)
		}
		squashed = ahead
	}

	if err := tm.Git.ExecuteCommand("tag", "-a", version, "-F", messageFile); err != nil {
		// squashed > 0 only when the ahead >= 2 block above actually ran a
		// commit in this invocation; otherwise nothing was committed and the
		// message must not claim it was.
		if squashed > 0 {
			return "", fmt.Errorf(
				"release: commit succeeded but the tag was not created — run "+
					"'git tag -a %s -F %s' to finish: %w",
				version, messageFile, err,
			)
		}
		return "", fmt.Errorf(
			"release: the tag was not created — run 'git tag -a %s -F %s' to finish: %w",
			version, messageFile, err,
		)
	}

	summary := releaseSummary(version, ahead, squashed)

	if !push {
		return fmt.Sprintf(
			"%s Not pushed — run: git push origin %s --tags",
			summary, defaultBranch,
		), nil
	}

	if err := tm.Git.ExecuteCommand("push", "origin", defaultBranch, "--tags"); err != nil {
		return "", fmt.Errorf(
			"release: tag %s was created locally but the push failed — run "+
				"'git push origin %s --tags' to finish: %w",
			version, defaultBranch, err,
		)
	}
	return fmt.Sprintf("%s Pushed to origin/%s.", summary, defaultBranch), nil
}

// validateReleaseVersion is the cheapest guard (a regex, no git call), so it
// runs first.
func validateReleaseVersion(version string) error {
	if !releaseVersionPattern.MatchString(version) {
		return fmt.Errorf(
			"release: invalid version %q — must match vMAJOR.MINOR.PATCH (e.g. v0.12.0), "+
				"no prerelease or build-metadata suffixes",
			version,
		)
	}
	return nil
}

// checkCleanTree refuses when the working tree has uncommitted changes —
// `git reset --soft` would fold them into the squash commit unpredictably.
func (tm *TaskManager) checkCleanTree() error {
	dirty, err := tm.Git.IsWorktreeDirty("")
	if err != nil {
		return fmt.Errorf("release: %w", err)
	}
	if dirty {
		return fmt.Errorf(
			"release: working tree is dirty — commit or stash your changes before releasing",
		)
	}
	return nil
}

// checkOnDefaultBranch refuses when HEAD isn't the repository's default
// branch, and returns that branch name for the rest of the flow. A detached
// HEAD resolves to an empty current branch, which is refused with its own
// message naming the detachment — "" is not a branch that exists, so telling
// the user to check it out would be wrong.
func (tm *TaskManager) checkOnDefaultBranch() (string, error) {
	current, defaultBranch, err := tm.resolveHead()
	if err != nil {
		return "", fmt.Errorf("release: %w", err)
	}
	if current == "" {
		return "", fmt.Errorf(
			"release: HEAD is detached, must be on the default branch %q — run 'git checkout %s' first",
			defaultBranch,
			defaultBranch,
		)
	}
	if current != defaultBranch {
		return "", fmt.Errorf(
			"release: on branch %q, must be on the default branch %q — run 'git checkout %s' first",
			current, defaultBranch, defaultBranch,
		)
	}
	return defaultBranch, nil
}

// checkMessageFile refuses when messageFile is missing, unreadable, or
// blank. The same file backs both the squash commit and the tag annotation.
func checkMessageFile(messageFile string) error {
	data, err := os.ReadFile(messageFile)
	if err != nil {
		return fmt.Errorf("release: failed to read --message-file %q: %w", messageFile, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return fmt.Errorf(
			"release: --message-file %q is empty — provide a release message",
			messageFile,
		)
	}
	return nil
}

// checkTagAvailable refuses when version already names an existing tag.
func (tm *TaskManager) checkTagAvailable(version string) error {
	out, err := tm.Git.RunCapture("tag", "-l", version)
	if err != nil {
		return fmt.Errorf("release: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf(
			"release: tag %s already exists — choose a different version or delete the existing tag first",
			version,
		)
	}
	return nil
}

// releaseAheadCount returns how many commits HEAD is ahead of
// origin/<defaultBranch>. This flow only needs the one-sided ahead count (how
// many commits to squash), unlike review-scope's two-sided ahead/behind, so
// it runs its own direct `rev-list --count` rather than reusing
// aheadBehind/parseAheadBehind.
func (tm *TaskManager) releaseAheadCount(defaultBranch string) (int, error) {
	out, err := tm.Git.RunCapture("rev-list", "--count", "origin/"+defaultBranch+"..HEAD")
	if err != nil {
		return 0, err
	}
	ahead, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("unexpected rev-list --count output %q: %w", out, err)
	}
	return ahead, nil
}

// releaseSummary renders the leading sentence of Release's confirmation
// message; the caller appends the push/no-push tail.
func releaseSummary(version string, ahead, squashed int) string {
	switch {
	case squashed > 0:
		return fmt.Sprintf("Tagged %s (squashed %d commits).", version, squashed)
	case ahead == 1:
		return fmt.Sprintf("Tagged %s (1 commit, no squash needed).", version)
	default:
		return fmt.Sprintf("Tagged %s (no unpushed commits).", version)
	}
}
