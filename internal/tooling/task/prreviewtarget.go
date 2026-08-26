package task

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
)

// prLocalRefNamespace is where a PR fetch parks the refs it resolves. Under
// refs/devgeta/ rather than refs/heads/ or refs/remotes/ on purpose: nothing
// git or the user drives off these, so writing them moves no branch, changes
// no upstream tracking, and leaves the working tree exactly as the human left
// it — which is the point when a review loop runs unattended on an interval
// (ADR-0023 §1).
const prLocalRefNamespace = "refs/devgeta/pr/"

// prLocalRefs names the two refs one PR's review target is resolved from.
// They are siblings under the PR's own directory, never a ref and a ref
// directory of the same name — git refuses that pairing outright.
func prLocalRefs(prNumber string) (headRef, baseRef string) {
	prefix := prLocalRefNamespace + prNumber
	return prefix + "/head", prefix + "/base"
}

// validatePRNumber rejects anything that is not a bare positive integer in its
// canonical form.
//
// The digits-only rule is input validation, not politeness, and two different
// callers need it. pr-review-target interpolates the number into a git refspec,
// so without it a caller's `--pr` value chooses which ref gets written under
// refs/devgeta/. pr-review-state hands the same number to gh as a positional
// argument, so without it a value beginning with a dash is read by gh as a flag
// of its own (`--repo=attacker/evil`). Only digits reach either.
//
// The range and leading-zero rules exist for a different reason. GitHub numbers
// pull requests from 1, and it echoes a number back canonically, so "0" and
// "007" name no PR that can exist. Both would otherwise pass the digit check and
// fail later as an opaque `git fetch` error about a missing refs/pull ref —
// a network-shaped failure for what is really a typo, so they are caught here
// where the message can say so.
func validatePRNumber(pr string) error {
	return validateGitHubItemNumber("pull request", pr)
}

// validateGitHubItemNumber is the shared rule behind validatePRNumber and
// validateIssueNumber: GitHub issues and pull requests are numbered from the
// same per-repository sequence and both need to reach gh as a positional
// argument, so the digits-only and no-leading-zero rules are identical for
// either — only the noun in the message differs. kind names that noun
// ("pull request" or "issue").
func validateGitHubItemNumber(kind, n string) error {
	if n == "" {
		return fmt.Errorf("a %s number is required", kind)
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return fmt.Errorf("invalid %s number %q (expected digits only)", kind, n)
		}
	}
	if len(n) > 1 && n[0] == '0' {
		return fmt.Errorf(
			"invalid %s number %q (leading zeros are not part of a %s number; pass %s)",
			kind, n, kind,
			strings.TrimLeft(n, "0"),
		)
	}
	v, err := strconv.Atoi(n)
	if err != nil {
		return fmt.Errorf(
			"invalid %s number %q (too large to be a %s number)",
			kind, n, kind,
		)
	}
	if v < 1 {
		return fmt.Errorf("invalid %s number %q (%ss are numbered from 1)", kind, n, kind)
	}
	return nil
}

// prReviewTargetData is the orchestration result handed to
// formatPRReviewTarget. Base and Head are resolved commit SHAs, never ref
// names — a review takes minutes across several models, and a ref name
// resolved twice inside that window can mean two different commits.
type prReviewTargetData struct {
	Base    string
	Head    string
	Journal string

	Files    []fileChange
	Excluded []fileChange
}

// prBaseBranch reads the branch a pull request targets.
//
// baseRefName is the ONLY field this command needs: the head comes from
// refs/pull/<n>/head, which the upstream repo serves for fork PRs too, so
// headRefName and the head repository's owner would be data with nowhere to
// go — the fork-remote path they exist for is the one ADR-0023 rejected.
func (p *PRManager) prBaseBranch(prNumber string) (string, error) {
	raw, err := p.Gh.PRView(prNumber, "baseRefName")
	if err != nil {
		return "", err
	}
	var view struct {
		BaseRefName string `json:"baseRefName"`
	}
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		return "", fmt.Errorf("could not read PR #%s's base branch from gh: %w", prNumber, err)
	}
	base := strings.TrimSpace(view.BaseRefName)
	if base == "" {
		return "", fmt.Errorf("gh reported no base branch for PR #%s", prNumber)
	}
	return base, nil
}

// PRReviewTarget resolves and prints the immutable review target for one pull
// request: the merge-base and head SHAs of its range, the PR-scoped journal
// key, and the range's noise-filtered changed-file list.
//
// This output is THE context for a PR review — reviewer runs, journal stamps,
// reviewer-type selection, finding verification, and posting all key off it,
// and none of them read the working tree. Three properties make that safe
// (ADR-0023):
//
//   - Every call fetches refs/pull/<n>/head and the base branch first, so the
//     target is what GitHub shows right now. A FAILED fetch ends the command
//     with an error and never falls back to whatever is on disk: a confident
//     review of code the PR no longer contains is worse than no review.
//   - base is the MERGE BASE, not the base branch tip. `git diff a..b`
//     compares two endpoints, so a base branch that advanced after the PR
//     opened would inject every commit merged into it meanwhile as a reversal,
//     and reviewers would report other people's work as this PR's deletions.
//   - Both ends are resolved SHAs, so nothing downstream can drift mid-review.
//
// prNumber may be empty, in which case the current branch's PR is used.
func (p *PRManager) PRReviewTarget(prNumber string) (string, error) {
	owner, name, pr, err := p.resolveOwnerRepoPR(prNumber)
	if err != nil {
		return "", err
	}
	if err := validatePRNumber(pr); err != nil {
		return "", fmt.Errorf("pr-review-target: %w", err)
	}

	baseBranch, err := p.prBaseBranch(pr)
	if err != nil {
		return "", fmt.Errorf("pr-review-target: %w", err)
	}

	headRef, baseRef := prLocalRefs(pr)
	// The leading "+" on each refspec is load-bearing: an author who force-pushes
	// rewrites refs/pull/<n>/head non-fast-forward, and without it every fetch
	// after that force-push fails and no tick could ever review the PR again.
	if err := p.Git.FetchOriginRefspecsTimeout(
		reviewFetchTimeout,
		"+refs/pull/"+pr+"/head:"+headRef,
		"+refs/heads/"+baseBranch+":"+baseRef,
	); err != nil {
		// Two unrelated causes reach this line, so the advice names both rather
		// than implying the first: the network or credentials failed, OR the
		// `+refs/heads/<base>` half found nothing because the PR's base branch
		// was deleted or renamed upstream after the PR opened. Git's own stderr
		// is interpolated above and is what tells the two apart, so this does
		// not try to guess which one happened.
		return "", fmt.Errorf(
			"pr-review-target: could not fetch PR #%s (base branch %q) from origin: %w"+
				" — refusing to review possibly-stale local refs;"+
				" check the network and `gh auth status`,"+
				" and that the base branch still exists on origin"+
				" (it may have been deleted or renamed since the PR opened), then retry",
			pr, baseBranch, err,
		)
	}

	head, err := p.Git.ResolveCommit(headRef)
	if err != nil {
		return "", fmt.Errorf("pr-review-target: %w", err)
	}
	// BOTH ends are resolved to SHAs before the merge base is computed, never
	// the ref names again. refs/devgeta/pr/<n>/head and .../base are mutable
	// local refs that every fetch of this PR force-updates, so a concurrent
	// tick or a second `pr-review-target` run can move either one between
	// these calls — and each name, used twice, fails a different way.
	//
	// Naming headRef twice would pair the head captured above with the merge
	// base of whatever the ref points at NOW. After a force-push that rebased
	// the PR, that base is not even an ancestor of the head returned, so
	// `base..head` shows the base branch's own commits as this PR's deletions.
	//
	// Naming baseRef twice fails more quietly. merge-base always returns an
	// ancestor of its operands, so a base ref that merely advanced still
	// yields SOME ancestor of this head — but if it advanced to CONTAIN the
	// head (the PR was merged, or the base branch merged this PR's branch),
	// that ancestor IS the head. base == head is an empty range, so a PR full
	// of changes prints `files:` as `(none)` and every reviewer downstream is
	// told there is nothing to look at.
	//
	// Resolving both does not make the fetch and the resolution one atomic
	// operation — only the server could offer that — but it shrinks the window
	// to two adjacent rev-parse calls instead of one spanning a merge-base
	// graph walk, and it is what makes the printed pair describe a single diff
	// (ADR-0023).
	baseCommit, err := p.Git.ResolveCommit(baseRef)
	if err != nil {
		return "", fmt.Errorf("pr-review-target: %w", err)
	}
	base, err := p.Git.MergeBase(baseCommit, head)
	if err != nil {
		return "", fmt.Errorf("pr-review-target: %w", err)
	}

	files, err := fileChanges(p.Git, base+".."+head)
	if err != nil {
		return "", fmt.Errorf("pr-review-target: %w", err)
	}
	reviewable, excluded := partitionExcluded(files)

	return formatPRReviewTarget(prReviewTargetData{
		Base: base,
		Head: head,
		// A scoped exception to the branch keying in ADR-0012 §5, not a
		// replacement: a PR reviewed from someone else's fork has no local
		// branch to key on, and borrowing whatever branch happens to be
		// checked out would read another piece of work's settled decisions
		// into this review and write this PR's findings where branch teardown
		// deletes them. The key's shape belongs to the journal package, which
		// also has to RECOGNIZE it — Prune may not apply its branch-existence
		// test to a PR — so the literal has one definition rather than one
		// here and a matching prefix over there that could drift apart.
		Journal:  reviewjournal.PRKey(owner, name, pr),
		Files:    reviewable,
		Excluded: excluded,
	}), nil
}

// prReviewTargetNoFiles is the sentinel under `files:` for a range whose every
// changed file was filtered out as noise (or that changes nothing at all).
// Empty output would be ambiguous to an agent — success, nothing found, or a
// crash all look the same.
const prReviewTargetNoFiles = "(none)"

// formatPRReviewTarget renders the target as labeled plain text: `base:`,
// `head:`, `journal:`, then the changed-file list. Paths only — the stat
// columns ReviewScope prints are orientation for a human reading a branch,
// while this list exists so the next step can judge what KIND of change this
// is, which counts do not answer.
//
// An excluded-files receipt follows the list whenever noise was filtered, per
// task-design.md's rule that lossy output announces itself and offers a way in.
func formatPRReviewTarget(d prReviewTargetData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "base: %s\n", d.Base)
	fmt.Fprintf(&b, "head: %s\n", d.Head)
	fmt.Fprintf(&b, "journal: %s\n", d.Journal)

	b.WriteString("files:")
	if len(d.Files) == 0 {
		b.WriteString("\n" + prReviewTargetNoFiles)
	}
	for _, f := range d.Files {
		fmt.Fprintf(&b, "\n- %s", f.Path)
	}

	hint := fmt.Sprintf("dg task review-package %s %s --file <path>", d.Base, d.Head)
	if note := formatExclusionNotes(d.Excluded, hint); note != "" {
		b.WriteString("\n")
		b.WriteString(note)
	}
	return b.String()
}
