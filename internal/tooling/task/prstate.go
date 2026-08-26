package task

import (
	"encoding/json"
	"fmt"
	"strings"
)

// checksBucketCounts is a PR's CI checks reduced to gh's own
// pass/fail/pending/skipping/cancel buckets (see prCheck's doc comment) —
// counts only, never the per-check names, links, or log digests PRChecks
// renders (ADR-0034: pr-state's checks gatherer stops at the tally and makes
// zero RunFailedJobLog calls).
type checksBucketCounts struct {
	Pass, Fail, Pending, Skipping, Cancel int
}

// tallyChecksBuckets reduces a PR's parsed checks to their bucket counts.
func tallyChecksBuckets(checks []prCheck) checksBucketCounts {
	var c checksBucketCounts
	for _, check := range checks {
		switch strings.ToLower(strings.TrimSpace(check.Bucket)) {
		case "pass":
			c.Pass++
		case "fail":
			c.Fail++
		case "pending":
			c.Pending++
		case "skipping":
			c.Skipping++
		case "cancel":
			c.Cancel++
		}
	}
	return c
}

// reviewThreadsPage is the one field countUnresolvedReviewThreads needs from
// each page of FetchReviewThreads' raw JSON.
type reviewThreadsPage struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						IsResolved bool `json:"isResolved"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// countUnresolvedReviewThreads counts unresolved threads across every page
// of FetchReviewThreads' raw, multi-document JSON (--paginate emits one
// document per page). A plain count needs no rendering, so this reduces in
// Go rather than adding a jq filter for it — the same call task.IssueScope's
// parseIssueCrossReferences makes for the same shape of gh output.
func countUnresolvedReviewThreads(raw string) (int, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	count := 0
	sawPage := false
	for dec.More() {
		var page reviewThreadsPage
		if err := dec.Decode(&page); err != nil {
			return 0, fmt.Errorf("pr-state: could not read review threads from gh: %w", err)
		}
		sawPage = true
		for _, n := range page.Data.Repository.PullRequest.ReviewThreads.Nodes {
			if !n.IsResolved {
				count++
			}
		}
	}
	if !sawPage {
		return 0, fmt.Errorf(
			"pr-state: could not read review threads from gh: no JSON document found",
		)
	}
	return count, nil
}

// gatherUnresolvedThreadCount fetches a PR's review threads and reduces them
// to the unresolved count only — no discussion fetch (ADR-0034 §2: the count
// comes from the review-threads query alone).
func (p *PRManager) gatherUnresolvedThreadCount(owner, name, pr string) (int, error) {
	raw, err := p.Gh.FetchReviewThreads(owner, name, pr)
	if err != nil {
		return 0, err
	}
	return countUnresolvedReviewThreads(raw)
}

// prStateData is pr-state's compact answer (ADR-0034), composed entirely
// from the same three gatherers PRReviewState/PRChecks/ReviewThreads already
// use internally. Each gatherer's result is independent: one failing does
// not fail the whole command, it degrades that gatherer's own field(s) to an
// "unavailable" sentinel (see formatPRState) while the rest still render.
type prStateData struct {
	ReviewState    prReviewStateData
	ReviewStateOK  bool
	ReviewStateErr error

	Checks    checksBucketCounts
	ChecksOK  bool
	ChecksErr error

	UnresolvedThreads int
	ThreadsOK         bool
	ThreadsErr        error
}

// PRState answers "where does this PR stand?" in one call: PR state, a CI
// checks summary (counts only), the unresolved review-thread count, and the
// authenticated user's own review state. It composes the same gatherers the
// four existing pr-view/pr-checks/review-threads/pr-review-state commands
// use — never a fresh gh query, never a parse of one of those commands' own
// rendered text (ADR-0034) — and never triggers PRChecks' per-failure log
// digest or ReviewThreads' discussion fetch, neither of which this payload
// needs.
//
// owner/repo/PR resolution happens exactly once, here, then the resolved
// identifiers are passed into every gatherer — see the ADR's call-budget
// table (5 gh calls with --pr given, 6 when it must be inferred).
func (p *PRManager) PRState(prNumber string) (string, error) {
	owner, name, pr, err := p.resolveOwnerRepoPR(prNumber)
	if err != nil {
		return "", err
	}
	if err := validatePRNumber(pr); err != nil {
		return "", fmt.Errorf("pr-state: %w", err)
	}

	var d prStateData

	if _, checks, err := p.gatherPRChecks(pr); err != nil {
		d.ChecksErr = err
	} else {
		d.Checks, d.ChecksOK = tallyChecksBuckets(checks), true
	}

	if n, err := p.gatherUnresolvedThreadCount(owner, name, pr); err != nil {
		d.ThreadsErr = err
	} else {
		d.UnresolvedThreads, d.ThreadsOK = n, true
	}

	if rs, err := p.gatherPRReviewState(pr); err != nil {
		d.ReviewStateErr = err
	} else {
		d.ReviewState, d.ReviewStateOK = rs, true
	}

	return formatPRState(d), nil
}

// formatPRState renders pr-state's answer as five labeled lines, no
// markdown scaffolding (task-design.md principle 1). Every line is always
// present: a gatherer that failed renders its field(s) as "unavailable"
// (with the reason on the first such line) rather than omitting them, so a
// caller can always find a label at a fixed position.
func formatPRState(d prStateData) string {
	var b strings.Builder

	if d.ReviewStateOK {
		fmt.Fprintf(&b, "pr: %s\n", d.ReviewState.Lifecycle)
		requested := "no"
		if d.ReviewState.Requested {
			requested = "yes"
		}
		fmt.Fprintf(&b, "requested: %s\n", requested)
		fmt.Fprintf(&b, "my-review: %s\n", d.ReviewState.MyReview)
	} else {
		fmt.Fprintf(&b, "pr: unavailable: %s\n", d.ReviewStateErr)
		b.WriteString("requested: unavailable\n")
		b.WriteString("my-review: unavailable\n")
	}

	if d.ChecksOK {
		fmt.Fprintf(&b, "checks: pass=%d fail=%d pending=%d skipping=%d cancel=%d\n",
			d.Checks.Pass, d.Checks.Fail, d.Checks.Pending, d.Checks.Skipping, d.Checks.Cancel)
	} else {
		fmt.Fprintf(&b, "checks: unavailable: %s\n", d.ChecksErr)
	}

	if d.ThreadsOK {
		fmt.Fprintf(&b, "threads-unresolved: %d\n", d.UnresolvedThreads)
	} else {
		fmt.Fprintf(&b, "threads-unresolved: unavailable: %s\n", d.ThreadsErr)
	}

	return strings.TrimRight(b.String(), "\n")
}
