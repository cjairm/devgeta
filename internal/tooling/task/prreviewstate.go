package task

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The values `pr:` can take. These are the left column of /pr-review-loop's
// decision table, so they are a contract with the shared command file
// (task-design.md principle 4) — renaming one silently changes which row a
// tick lands on.
const (
	prLifecycleOpen   = "open"
	prLifecycleDraft  = "draft"
	prLifecycleMerged = "merged"
	prLifecycleClosed = "closed"
)

// The values `my-review:` can take — the authenticated user's latest submitted
// review. Same contract as the lifecycle values above.
const (
	myReviewApproved         = "approved"
	myReviewChangesRequested = "changes-requested"
	myReviewCommented        = "commented"
	myReviewNone             = "none"
)

// prReviewStateFields is the slice of `gh pr view --json` one tick reads.
//
// Four fields, and deliberately only four: each one decides a column of the
// decision table (`state` + `isDraft` → `pr:`, `reviewRequests` → `requested:`,
// `reviews` → `my-review:`). The PR's baseRefName, headRefName and head
// repository owner are NOT requested here even though a review needs them —
// pr-review-target owns that resolution, fetches the refs, and returns immutable
// SHAs, so asking for them again in the trigger read would be data with no
// consumer and a second spelling of a fact that already has one.
var prReviewStateFields = []string{"state", "isDraft", "reviewRequests", "reviews"}

// prReviewRequest is one entry of a PR's current reviewRequests.
//
// gh emits `login` for a USER request and only `name`/`slug` for a TEAM
// request, so a team entry unmarshals here with an empty Login. That is why
// ADR-0020 §3's personal-request-only rule needs no filter of its own: a
// request that does not name a user cannot match a login.
type prReviewRequest struct {
	Login string `json:"login"`
}

// prSubmittedReview is one entry of a PR's reviews list — a review someone
// submitted, with the state it carried and when it landed.
type prSubmittedReview struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State       string `json:"state"`
	SubmittedAt string `json:"submittedAt"`
}

// prStateView is gh's answer to prReviewStateFields.
type prStateView struct {
	State          string              `json:"state"`
	IsDraft        bool                `json:"isDraft"`
	ReviewRequests []prReviewRequest   `json:"reviewRequests"`
	Reviews        []prSubmittedReview `json:"reviews"`
}

// prReviewStateData is the orchestration result handed to
// formatPRReviewState.
type prReviewStateData struct {
	Lifecycle string
	Requested bool
	MyReview  string
}

// PRReviewState prints one pull request's current review-request state as the
// three lines a /pr-review-loop tick decides from:
//
//	pr: open
//	requested: yes
//	my-review: none
//
// This is THE trigger read (ADR-0020): every tick calls it first and takes
// exactly one row of the decision table from it, and it is called again
// immediately before posting as a re-check. Three properties make that safe:
//
//   - It is a read of GitHub's own current state, never a local record. Whether
//     a review is wanted, whether one was already answered, and whether the
//     author asked again are all one field — submitting any review removes the
//     user from reviewRequests and the re-request button puts them back — so
//     there is no "already reviewed" bookkeeping to go stale.
//   - `draft` is reported distinctly from `open`, because the table checks draft
//     BEFORE the request state: a requested draft waits. A caller given only
//     `open` could not recover that.
//   - Nothing is guessed. A state gh reports that this does not recognize ends
//     the command with an error rather than resolving to a row.
//
// prNumber may be empty, in which case the current branch's PR is used.
func (p *PRManager) PRReviewState(prNumber string) (string, error) {
	// The full family resolver, not just the PR-number half: owner/name go
	// unused here (gh resolves the repo from the checkout, same as pr-view and
	// pr-checks), but this is the one place `--pr` inference, its error text,
	// and any future `--repo` override live for every PR task command.
	_, _, pr, err := p.resolveOwnerRepoPR(prNumber)
	if err != nil {
		return "", err
	}
	// A `--pr` value reaches gh as a positional argument, so the digits-only
	// rule is input validation here too: it keeps a value that begins with a
	// dash from being read by gh as a flag of its own.
	if err := validatePRNumber(pr); err != nil {
		return "", fmt.Errorf("pr-review-state: %w", err)
	}

	raw, err := p.Gh.PRView(pr, prReviewStateFields...)
	if err != nil {
		return "", fmt.Errorf("pr-review-state: %w", err)
	}
	var view prStateView
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		return "", fmt.Errorf(
			"pr-review-state: could not read PR #%s's review state from gh: %w",
			pr, err,
		)
	}
	lifecycle, err := prLifecycle(view.State, view.IsDraft)
	if err != nil {
		return "", fmt.Errorf("pr-review-state: PR #%s: %w", pr, err)
	}

	me, err := p.Gh.AuthenticatedLogin()
	if err != nil {
		return "", fmt.Errorf("pr-review-state: %w", err)
	}

	return formatPRReviewState(prReviewStateData{
		Lifecycle: lifecycle,
		Requested: requestsReviewFrom(view.ReviewRequests, me),
		MyReview:  latestReviewBy(view.Reviews, me),
	}), nil
}

// prLifecycle maps gh's `state` and `isDraft` onto the single `pr:` value.
//
// The order of the cases is load-bearing. GitHub lets a draft be closed, so
// CLOSED and MERGED are answered before the draft check — the decision table
// evaluates its terminal row before its draft row, and reporting `draft` for a
// closed draft would park the loop on a pull request that is already over,
// waiting for a request that can never come.
//
// An unrecognized state is an error, not a default: `pr:` selects the row, so a
// guess here is a wrong action, and stopping with a message a human can act on
// beats reviewing or terminating on made-up state.
func prLifecycle(state string, isDraft bool) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "MERGED":
		return prLifecycleMerged, nil
	case "CLOSED":
		return prLifecycleClosed, nil
	case "OPEN":
		if isDraft {
			return prLifecycleDraft, nil
		}
		return prLifecycleOpen, nil
	case "":
		// Reaching here means `gh pr view` already exited zero and its JSON
		// parsed, so auth works and the PR exists — neither can be the cause,
		// and suggesting them would send the reader after a problem they do not
		// have. What is left is gh no longer filling the field this asked for.
		return "", fmt.Errorf(
			"gh returned no state for the PR (the read itself succeeded, so this is gh's `--json state` output shape changing); check `gh --version` and update gh",
		)
	default:
		return "", fmt.Errorf(
			"gh reported the unknown state %q (expected OPEN, CLOSED, or MERGED)",
			strings.TrimSpace(state),
		)
	}
}

// requestsReviewFrom reports whether login is named in the PR's current
// review requests.
//
// Logins are compared case-insensitively because GitHub treats them that way,
// and an entry with no login (a team request) can never match — which is
// ADR-0020 §3: a request addressed to a team is a staffing judgment the loop
// does not make.
func requestsReviewFrom(requests []prReviewRequest, login string) bool {
	for _, r := range requests {
		if r.Login != "" && strings.EqualFold(r.Login, login) {
			return true
		}
	}
	return false
}

// latestReviewBy returns the `my-review:` value for login: the state of that
// user's most recently submitted review, or `none` when they have not submitted
// one.
//
// Newest wins by submittedAt rather than by list order, and equal timestamps
// resolve to the later-listed entry. An entry whose submittedAt does not parse
// is skipped: a review still being drafted (PENDING) carries no submittedAt,
// and a timestamp that cannot be ordered cannot be shown to be the newest.
// Skipping can only move the answer toward `none`, which is the table's wait
// row — the safe direction; treating it as newest could report an approval that
// is not the user's current position.
func latestReviewBy(reviews []prSubmittedReview, login string) string {
	state := myReviewNone
	var latestAt time.Time
	var found bool
	for _, r := range reviews {
		if r.Author.Login == "" || !strings.EqualFold(r.Author.Login, login) {
			continue
		}
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(r.SubmittedAt))
		if err != nil {
			continue
		}
		if found && at.Before(latestAt) {
			continue
		}
		latestAt, found, state = at, true, myReviewLabel(r.State)
	}
	return state
}

// myReviewLabel maps one GitHub review state onto the `my-review:` vocabulary.
//
// Anything outside the three standing states reports `none` — DISMISSED (an
// approval that was dismissed and no longer counts) and any state GitHub adds
// later. That is a decision, not a fallthrough: `none` lands the caller on the
// wait row, whereas reporting a standing state that no longer holds could stop
// the loop on an approval GitHub has already thrown away.
func myReviewLabel(state string) string {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "APPROVED":
		return myReviewApproved
	case "CHANGES_REQUESTED":
		return myReviewChangesRequested
	case "COMMENTED":
		return myReviewCommented
	default:
		return myReviewNone
	}
}

// formatPRReviewState renders the tick's state read as three labeled lines and
// nothing else. Every line is always present, with a value from a fixed
// vocabulary: the caller matches on all three to select exactly one row, so an
// omitted line would be an unselectable state.
func formatPRReviewState(d prReviewStateData) string {
	requested := "no"
	if d.Requested {
		requested = "yes"
	}
	return fmt.Sprintf(
		"pr: %s\nrequested: %s\nmy-review: %s",
		d.Lifecycle, requested, d.MyReview,
	)
}
