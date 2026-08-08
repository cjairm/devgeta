package task

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// prLocalRefNamespace is where a PR fetch parks the refs it resolves. Under
// refs/devgeta/ rather than refs/heads/ or refs/remotes/ on purpose: nothing
// git or the user drives off these, so writing them moves no branch, changes
// no upstream tracking, and leaves the working tree exactly as the human left
// it — which is the point when a review loop runs unattended on an interval
// (ADR-0022 §1).
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
	if pr == "" {
		return fmt.Errorf("a pull request number is required")
	}
	for _, r := range pr {
		if r < '0' || r > '9' {
			return fmt.Errorf("invalid pull request number %q (expected digits only)", pr)
		}
	}
	if len(pr) > 1 && pr[0] == '0' {
		return fmt.Errorf(
			"invalid pull request number %q (leading zeros are not part of a pull request number; pass %s)",
			pr,
			strings.TrimLeft(pr, "0"),
		)
	}
	n, err := strconv.Atoi(pr)
	if err != nil {
		return fmt.Errorf(
			"invalid pull request number %q (too large to be a pull request number)",
			pr,
		)
	}
	if n < 1 {
		return fmt.Errorf("invalid pull request number %q (pull requests are numbered from 1)", pr)
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
// go — the fork-remote path they exist for is the one ADR-0022 rejected.
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
// (ADR-0022):
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
	base, err := p.Git.MergeBase(baseRef, headRef)
	if err != nil {
		return "", fmt.Errorf("pr-review-target: %w", err)
	}

	files, err := fileChanges(p.Git, base+".."+head)
	if err != nil {
		return "", fmt.Errorf("pr-review-target: %w", err)
	}
	reviewable, excluded := partitionExcluded(files)

	return formatPRReviewTarget(prReviewTargetData{
		Base:     base,
		Head:     head,
		Journal:  prJournalKey(owner, name, pr),
		Files:    reviewable,
		Excluded: excluded,
	}), nil
}

// prJournalKey builds the PR-scoped review journal key, "pr/<owner>/<repo>/<n>".
//
// It is a scoped exception to the branch keying in ADR-0012 §5, not a
// replacement: a PR reviewed from someone else's fork has no local branch to
// key on, and borrowing whatever branch happens to be checked out would read
// another piece of work's settled decisions into this review and write this
// PR's findings where branch teardown deletes them. The journal's encoder
// percent-encodes every byte outside [A-Za-z0-9._-], so the separators encode
// and the key cannot escape the review directory.
func prJournalKey(owner, repo, prNumber string) string {
	return fmt.Sprintf("pr/%s/%s/%s", owner, repo, prNumber)
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
