package task

import (
	"encoding/json"
	"fmt"
	"strings"
)

// reviewThreadsJSONSchemaVersion is review-threads --json's schema version.
// A consumer pins against this; bump it, and document the change, whenever
// the shape below changes in a way that matters to a machine reader.
const reviewThreadsJSONSchemaVersion = 1

// reviewThreadJSON is one review thread in the --json document.
type reviewThreadJSON struct {
	ID           string              `json:"id"`
	IsResolved   bool                `json:"isResolved"`
	IsOutdated   bool                `json:"isOutdated"`
	ResolvedBy   string              `json:"resolvedBy,omitempty"`
	Path         string              `json:"path"`
	Line         *int                `json:"line"`
	OriginalLine *int                `json:"originalLine"`
	DiffHunk     string              `json:"diffHunk,omitempty"`
	Comments     []reviewCommentJSON `json:"comments"`
	// CommentsTruncated reflects this thread's own comments(first: 100)
	// connection's pageInfo.hasNextPage — NEVER len(Comments) == 100, which
	// cannot distinguish exactly 100 from more (Slice G, n14).
	CommentsTruncated bool `json:"commentsTruncated"`
}

// reviewCommentJSON is one inline comment within a review thread.
type reviewCommentJSON struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// reviewSummaryJSON is one submitted review's summary body (Approve/
// Request-changes/Comment), distinct from the inline threads above.
type reviewSummaryJSON struct {
	Author      string `json:"author"`
	Body        string `json:"body"`
	State       string `json:"state"`
	SubmittedAt string `json:"submittedAt"`
}

// conversationCommentJSON is one top-level PR conversation comment.
type conversationCommentJSON struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// reviewDiscussionJSON is the non-inline feedback surface: review summaries
// and top-level conversation comments (see githubcli.prDiscussionQuery).
// Each connection carries its own truncation flag, for the same reason
// reviewThreadJSON's does.
type reviewDiscussionJSON struct {
	Reviews           []reviewSummaryJSON       `json:"reviews"`
	ReviewsTruncated  bool                      `json:"reviewsTruncated"`
	Comments          []conversationCommentJSON `json:"comments"`
	CommentsTruncated bool                      `json:"commentsTruncated"`
}

// reviewThreadsDocument is review-threads --json's whole payload: every
// review thread across every page (already merged), plus the discussion,
// under one schema version a consumer can pin against.
type reviewThreadsDocument struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Threads       []reviewThreadJSON   `json:"threads"`
	Discussion    reviewDiscussionJSON `json:"discussion"`
}

// rawReviewThreadsPage is one page of githubcli.reviewThreadsQuery's raw
// JSON — gh emits one such document per page under --paginate.
type rawReviewThreadsPage struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []rawReviewThreadNode `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

type rawReviewThreadNode struct {
	ID         string `json:"id"`
	IsResolved bool   `json:"isResolved"`
	IsOutdated bool   `json:"isOutdated"`
	ResolvedBy *struct {
		Login string `json:"login"`
	} `json:"resolvedBy"`
	Path         string `json:"path"`
	Line         *int   `json:"line"`
	OriginalLine *int   `json:"originalLine"`
	Comments     struct {
		Nodes    []rawReviewCommentNode `json:"nodes"`
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
	} `json:"comments"`
	FirstComment struct {
		Nodes []struct {
			DiffHunk string `json:"diffHunk"`
		} `json:"nodes"`
	} `json:"firstComment"`
}

type rawReviewCommentNode struct {
	ID     string `json:"id"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// parseReviewThreadsJSON parses the raw, multi-page JSON
// FetchReviewThreads returns, merges every page's threads (in page order —
// cursor-based pagination never repeats a thread across pages, so this is a
// plain concatenation, not a dedup), and filters by resolved exactly like
// resolvedPtrForState's markdown path: nil keeps every thread, &true keeps
// only resolved ones, &false only unresolved ones.
func parseReviewThreadsJSON(raw string, resolved *bool) ([]reviewThreadJSON, error) {
	// Non-nil from the start so a PR with no threads marshals to "[]", not
	// "null" — a machine consumer should never need a null-check on this
	// field on top of a length check.
	out := []reviewThreadJSON{}
	dec := json.NewDecoder(strings.NewReader(raw))
	sawPage := false
	for dec.More() {
		var page rawReviewThreadsPage
		if err := dec.Decode(&page); err != nil {
			return nil, fmt.Errorf("review-threads --json: could not read threads from gh: %w", err)
		}
		sawPage = true
		for _, n := range page.Data.Repository.PullRequest.ReviewThreads.Nodes {
			if resolved != nil && n.IsResolved != *resolved {
				continue
			}
			out = append(out, rawThreadToJSON(n))
		}
	}
	if !sawPage {
		return nil, fmt.Errorf(
			"review-threads --json: could not read threads from gh: no JSON document found",
		)
	}
	return out, nil
}

func rawThreadToJSON(n rawReviewThreadNode) reviewThreadJSON {
	t := reviewThreadJSON{
		ID:                n.ID,
		IsResolved:        n.IsResolved,
		IsOutdated:        n.IsOutdated,
		Path:              n.Path,
		Line:              n.Line,
		OriginalLine:      n.OriginalLine,
		CommentsTruncated: n.Comments.PageInfo.HasNextPage,
	}
	if n.ResolvedBy != nil {
		t.ResolvedBy = n.ResolvedBy.Login
	}
	if len(n.FirstComment.Nodes) > 0 {
		t.DiffHunk = n.FirstComment.Nodes[0].DiffHunk
	}
	t.Comments = make([]reviewCommentJSON, len(n.Comments.Nodes))
	for i, c := range n.Comments.Nodes {
		t.Comments[i] = reviewCommentJSON{
			ID: c.ID, Author: c.Author.Login, Body: c.Body, CreatedAt: c.CreatedAt,
		}
	}
	return t
}

// rawDiscussionDoc is githubcli.prDiscussionQuery's raw JSON — a single
// document, never paginated (see its own doc comment for why).
type rawDiscussionDoc struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				Reviews struct {
					Nodes    []rawReviewSummaryNode `json:"nodes"`
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"reviews"`
				Comments struct {
					Nodes    []rawConversationCommentNode `json:"nodes"`
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"comments"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

type rawReviewSummaryNode struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body        string `json:"body"`
	State       string `json:"state"`
	SubmittedAt string `json:"submittedAt"`
}

type rawConversationCommentNode struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

// parseDiscussionJSON parses FetchPRDiscussion's raw JSON into
// reviewDiscussionJSON, carrying each connection's own truncation flag.
func parseDiscussionJSON(raw string) (reviewDiscussionJSON, error) {
	var doc rawDiscussionDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return reviewDiscussionJSON{}, fmt.Errorf(
			"review-threads --json: could not read discussion from gh: %w", err,
		)
	}
	pr := doc.Data.Repository.PullRequest

	d := reviewDiscussionJSON{
		ReviewsTruncated:  pr.Reviews.PageInfo.HasNextPage,
		CommentsTruncated: pr.Comments.PageInfo.HasNextPage,
	}
	d.Reviews = make([]reviewSummaryJSON, len(pr.Reviews.Nodes))
	for i, r := range pr.Reviews.Nodes {
		d.Reviews[i] = reviewSummaryJSON{
			Author: r.Author.Login, Body: r.Body, State: r.State, SubmittedAt: r.SubmittedAt,
		}
	}
	d.Comments = make([]conversationCommentJSON, len(pr.Comments.Nodes))
	for i, c := range pr.Comments.Nodes {
		d.Comments[i] = conversationCommentJSON{
			Author: c.Author.Login, Body: c.Body, CreatedAt: c.CreatedAt,
		}
	}
	return d, nil
}

// ReviewThreadsJSON is review-threads' --json path: the same two gh fetches
// ReviewThreads makes (FetchReviewThreads, FetchPRDiscussion), merged into
// one versioned JSON document instead of markdown. --state filters threads
// identically to the markdown path. The markdown path and its sentinels are
// untouched by this method's existence.
func (p *PRManager) ReviewThreadsJSON(prNumber, state string) (string, error) {
	resolved, err := resolvedPtrForState(state)
	if err != nil {
		return "", err
	}
	owner, name, pr, err := p.resolveOwnerRepoPR(prNumber)
	if err != nil {
		return "", err
	}

	rawThreads, err := p.Gh.FetchReviewThreads(owner, name, pr)
	if err != nil {
		return "", err
	}
	threads, err := parseReviewThreadsJSON(rawThreads, resolved)
	if err != nil {
		return "", err
	}

	rawDiscussion, err := p.Gh.FetchPRDiscussion(owner, name, pr)
	if err != nil {
		return "", err
	}
	discussion, err := parseDiscussionJSON(rawDiscussion)
	if err != nil {
		return "", err
	}

	out, err := json.Marshal(reviewThreadsDocument{
		SchemaVersion: reviewThreadsJSONSchemaVersion,
		Threads:       threads,
		Discussion:    discussion,
	})
	if err != nil {
		return "", fmt.Errorf("review-threads --json: failed to build output: %w", err)
	}
	return string(out), nil
}
