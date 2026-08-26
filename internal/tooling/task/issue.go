package task

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	git_app "github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/githubcli"
)

// IssueManager wires the githubcli issue primitives and local git state
// together so issue-scope can answer "what already exists for this issue"
// in one call. Mirrors PRManager's orchestrate-then-format pattern.
type IssueManager struct {
	Gh *githubcli.GithubCli
	// Git resolves local branches and worktrees — the two signals gh has no
	// concept of at all (ADR-0035: a branch is a textual candidate, never a
	// GitHub-confirmed association).
	Git *git_app.Git
}

// NewIssue creates an IssueManager with real executors.
func NewIssue() *IssueManager {
	return &IssueManager{Gh: githubcli.New(), Git: git_app.New()}
}

// validateIssueNumber rejects anything that is not a bare positive integer
// in its canonical form — the same rule validatePRNumber enforces for pull
// requests, because GitHub issues and pull requests share one per-repository
// number sequence and both need to reach gh as a positional argument (never
// something a leading dash could turn into a flag of its own).
func validateIssueNumber(issue string) error {
	return validateGitHubItemNumber("issue", issue)
}

// resolveOwnerRepoIssue validates the caller-supplied issue number and
// resolves owner/name from the current repository. Unlike resolveOwnerRepoPR,
// there is no "current issue" to infer from a checked-out branch —
// issue-scope always takes an explicit number.
func (im *IssueManager) resolveOwnerRepoIssue(
	issueNumber string,
) (owner, name, issue string, err error) {
	if err := validateIssueNumber(issueNumber); err != nil {
		return "", "", "", err
	}
	repo, err := im.Gh.CurrentRepo()
	if err != nil {
		return "", "", "", err
	}
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("unexpected repo format %q (want owner/name)", repo)
	}
	return parts[0], parts[1], issueNumber, nil
}

// issueRefNeighbor reports whether b is a character ADR-0035's grammar
// forbids immediately before or after a matching digit run: a letter,
// digit, or '.'. Any of these glues the run to something else — a longer
// number, a version string, a word — so a match against them is rejected.
func issueRefNeighbor(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '.'
}

// branchReferencesIssue reports whether branch contains a reference to issue
// number, per ADR-0035's grammar: the exact digit run of number, with the
// characters immediately before and after (when they exist) outside
// [0-9A-Za-z.], and an optional single '#' immediately before the run. A
// true result is a CANDIDATE only — this function makes no claim about
// whether the branch is actually about the issue, only that its name
// contains a reference shaped like one.
func branchReferencesIssue(branch, number string) bool {
	if number == "" {
		return false
	}
	for i := 0; i+len(number) <= len(branch); i++ {
		if branch[i:i+len(number)] != number {
			continue
		}
		start, end := i, i+len(number)
		beforeOK := start == 0 || !issueRefNeighbor(branch[start-1])
		afterOK := end == len(branch) || !issueRefNeighbor(branch[end])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

// matchingBranches returns every branch that references number, in the
// order they were given, per branchReferencesIssue's candidate-only grammar.
func matchingBranches(branches []string, number string) []string {
	var matches []string
	for _, b := range branches {
		if branchReferencesIssue(b, number) {
			matches = append(matches, b)
		}
	}
	return matches
}

// issuePRRef is one pull request GitHub's own cross-reference timeline
// confirmed as referencing the issue (ADR-0035) — never a text-grep guess.
type issuePRRef struct {
	Number string
	State  string
	Title  string
}

// issueCrossRefPage is one page of issueCrossReferencesQuery's JSON —
// gh emits one such document per page under --paginate.
type issueCrossRefPage struct {
	Data struct {
		Repository struct {
			Issue struct {
				TimelineItems struct {
					Nodes []issueCrossRefNode `json:"nodes"`
				} `json:"timelineItems"`
			} `json:"issue"`
		} `json:"repository"`
	} `json:"data"`
}

// issueCrossRefNode is one timeline node. Source is only populated when the
// cross-reference's source is a pull request (the query's `... on
// PullRequest` fragment) — when GitHub resolves it to an Issue instead
// (issue-to-issue references, which this query is not asking about), every
// field inside stays at its zero value, and Source.Number == 0 is what lets
// parseIssueCrossReferences tell the two apart.
type issueCrossRefNode struct {
	IsCrossRepository bool `json:"isCrossRepository"`
	Source            struct {
		Number     int    `json:"number"`
		Title      string `json:"title"`
		State      string `json:"state"`
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
	} `json:"source"`
}

// parseIssueCrossReferences parses the raw, multi-page JSON
// FetchIssueCrossReferences returns, merges every page's nodes, and filters
// to same-repository pull request sources only (ADR-0035: cross-repository
// events are dropped, reported neither as a candidate nor confirmed).
// Results are deduplicated by PR number (a page boundary can repeat a node)
// and returned sorted by number, ascending, for a deterministic answer.
func parseIssueCrossReferences(raw, owner, name string) ([]issuePRRef, error) {
	wantRepo := owner + "/" + name
	seen := map[int]issuePRRef{}

	dec := json.NewDecoder(strings.NewReader(raw))
	sawPage := false
	for dec.More() {
		var page issueCrossRefPage
		if err := dec.Decode(&page); err != nil {
			return nil, fmt.Errorf("issue-scope: could not read cross-references from gh: %w", err)
		}
		sawPage = true
		for _, node := range page.Data.Repository.Issue.TimelineItems.Nodes {
			if node.IsCrossRepository || node.Source.Number == 0 {
				continue
			}
			if !strings.EqualFold(node.Source.Repository.NameWithOwner, wantRepo) {
				continue
			}
			seen[node.Source.Number] = issuePRRef{
				Number: strconv.Itoa(node.Source.Number),
				State:  node.Source.State,
				Title:  node.Source.Title,
			}
		}
	}
	if !sawPage {
		return nil, fmt.Errorf(
			"issue-scope: could not read cross-references from gh: no JSON document found",
		)
	}

	numbers := make([]int, 0, len(seen))
	for n := range seen {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)

	refs := make([]issuePRRef, 0, len(numbers))
	for _, n := range numbers {
		refs = append(refs, seen[n])
	}
	return refs, nil
}

// issueWorktreeRef is a worktree holding a branch that matched
// branchReferencesIssue.
type issueWorktreeRef struct {
	Branch string
	Path   string
}

// worktreesForBranches returns every worktree whose branch is in
// candidateBranches, in worktree-list order.
func worktreesForBranches(
	worktrees []git_app.WorktreeInfo,
	candidateBranches []string,
) []issueWorktreeRef {
	candidates := make(map[string]bool, len(candidateBranches))
	for _, b := range candidateBranches {
		candidates[b] = true
	}
	var refs []issueWorktreeRef
	for _, wt := range worktrees {
		if candidates[wt.Branch] {
			refs = append(refs, issueWorktreeRef{Branch: wt.Branch, Path: wt.Path})
		}
	}
	return refs
}

// issueScopeData is the orchestration result handed to formatIssueScope.
type issueScopeData struct {
	Number             string
	State              string
	Title              string
	ConfirmedPRs       []issuePRRef
	CandidateBranches  []string
	CandidateWorktrees []issueWorktreeRef
}

// issueViewResponse is gh's answer to issueViewFields.
type issueViewResponse struct {
	State string `json:"state"`
	Title string `json:"title"`
}

// IssueScope answers "what already exists for this issue" in one call: its
// state and title, the pull requests GitHub's own cross-reference confirms
// reference it, the local branches whose name is a textual candidate
// (ADR-0035 — never confirmed), and any worktree holding one of those
// branches.
func (im *IssueManager) IssueScope(issueNumber string) (string, error) {
	owner, name, issue, err := im.resolveOwnerRepoIssue(issueNumber)
	if err != nil {
		return "", err
	}

	rawView, err := im.Gh.IssueView(issue)
	if err != nil {
		return "", err
	}
	var view issueViewResponse
	if err := json.Unmarshal([]byte(rawView), &view); err != nil {
		return "", fmt.Errorf("issue-scope: could not read issue #%s from gh: %w", issue, err)
	}

	rawRefs, err := im.Gh.FetchIssueCrossReferences(owner, name, issue)
	if err != nil {
		return "", err
	}
	confirmedPRs, err := parseIssueCrossReferences(rawRefs, owner, name)
	if err != nil {
		return "", err
	}

	branches, err := im.Git.ListBranches()
	if err != nil {
		return "", err
	}
	candidateBranches := matchingBranches(branches, issue)

	worktrees, err := im.Git.ListWorktrees()
	if err != nil {
		return "", err
	}
	candidateWorktrees := worktreesForBranches(worktrees, candidateBranches)

	return formatIssueScope(issueScopeData{
		Number:             issue,
		State:              view.State,
		Title:              view.Title,
		ConfirmedPRs:       confirmedPRs,
		CandidateBranches:  candidateBranches,
		CandidateWorktrees: candidateWorktrees,
	}), nil
}

// formatIssueScope renders issue-scope's answer as labeled plain text, no
// markdown scaffolding (task-design.md principle 1). Every section prints a
// stable "none" sentinel when empty, so a caller can always find the label
// on its own line rather than the section vanishing.
func formatIssueScope(d issueScopeData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "issue: #%s (%s)\n", d.Number, strings.ToLower(d.State))
	fmt.Fprintf(&b, "title: %s\n", d.Title)

	prLines := make([]string, len(d.ConfirmedPRs))
	for i, pr := range d.ConfirmedPRs {
		prLines[i] = fmt.Sprintf("#%s %s %q", pr.Number, strings.ToLower(pr.State), pr.Title)
	}
	writeIssueScopeSection(&b, "prs (confirmed)", prLines)

	writeIssueScopeSection(&b, "branches (candidate)", d.CandidateBranches)

	wtLines := make([]string, len(d.CandidateWorktrees))
	for i, wt := range d.CandidateWorktrees {
		wtLines[i] = fmt.Sprintf("%s -> %s", wt.Branch, wt.Path)
	}
	writeIssueScopeSection(&b, "worktrees", wtLines)

	return strings.TrimRight(b.String(), "\n")
}

// writeIssueScopeSection appends one labeled section: "label: none" when
// items is empty, or "label:" followed by one indented line per item.
func writeIssueScopeSection(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		fmt.Fprintf(b, "%s: none\n", label)
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(b, "  %s\n", item)
	}
}
