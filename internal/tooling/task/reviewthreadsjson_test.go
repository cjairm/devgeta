package task

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

// reviewThreadPage builds one page of reviewThreadsQuery's raw JSON with a
// single thread, so tests can control its comments-truncation signal
// directly via hasNextPage rather than actually generating 100+ nodes.
func reviewThreadPage(id string, isResolved bool, commentsHasNextPage bool) string {
	return fmt.Sprintf(
		`{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[`+
			`{"id":%q,"isResolved":%t,"isOutdated":false,"resolvedBy":null,`+
			`"path":"a.go","line":5,"originalLine":5,`+
			`"comments":{"nodes":[{"id":"C1","author":{"login":"cjairm"},"body":"hi","createdAt":"2026-01-01T00:00:00Z"}],`+
			`"pageInfo":{"hasNextPage":%t}},`+
			`"firstComment":{"nodes":[{"diffHunk":"@@ -1 +1 @@"}]}}`+
			`],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`,
		id, isResolved, commentsHasNextPage,
	)
}

func discussionDoc(reviewsHasNextPage, commentsHasNextPage bool) string {
	return fmt.Sprintf(
		`{"data":{"repository":{"pullRequest":{`+
			`"reviews":{"nodes":[{"author":{"login":"cjairm"},"body":"lgtm","state":"APPROVED","submittedAt":"2026-01-01T00:00:00Z"}],`+
			`"pageInfo":{"hasNextPage":%t}},`+
			`"comments":{"nodes":[{"author":{"login":"cjairm"},"body":"thanks","createdAt":"2026-01-01T00:00:00Z"}],`+
			`"pageInfo":{"hasNextPage":%t}}}}}}`,
		reviewsHasNextPage, commentsHasNextPage,
	)
}

func TestParseReviewThreadsJSON(t *testing.T) {
	t.Run("merges multiple pages, in order", func(t *testing.T) {
		raw := reviewThreadPage("T1", false, false) + reviewThreadPage("T2", true, false)
		got, err := parseReviewThreadsJSON(raw, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got[0].ID != "T1" || got[1].ID != "T2" {
			t.Fatalf("unexpected threads: %+v", got)
		}
	})

	t.Run("filters by resolved state", func(t *testing.T) {
		raw := reviewThreadPage("T1", false, false) + reviewThreadPage("T2", true, false)
		resolvedTrue := true
		got, err := parseReviewThreadsJSON(raw, &resolvedTrue)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "T2" {
			t.Fatalf("expected only the resolved thread, got %+v", got)
		}
	})

	t.Run("exactly 100 comments is not truncated, 101+ is", func(t *testing.T) {
		notTruncated, err := parseReviewThreadsJSON(reviewThreadPage("T1", false, false), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notTruncated[0].CommentsTruncated {
			t.Fatalf(
				"expected commentsTruncated=false when hasNextPage=false, got %+v",
				notTruncated[0],
			)
		}

		truncated, err := parseReviewThreadsJSON(reviewThreadPage("T1", false, true), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !truncated[0].CommentsTruncated {
			t.Fatalf("expected commentsTruncated=true when hasNextPage=true, got %+v", truncated[0])
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := parseReviewThreadsJSON("not json", nil); err == nil {
			t.Error("expected an error for malformed json")
		}
	})

	t.Run("no threads marshals to an empty array, not null", func(t *testing.T) {
		raw := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`
		got, err := parseReviewThreadsJSON(raw, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("unexpected marshal error: %v", err)
		}
		if string(out) != "[]" {
			t.Fatalf("expected an empty JSON array, got %s", out)
		}
	})
}

func TestParseDiscussionJSON(t *testing.T) {
	t.Run("parses reviews and comments with their own truncation flags", func(t *testing.T) {
		got, err := parseDiscussionJSON(discussionDoc(false, true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Reviews) != 1 || got.ReviewsTruncated {
			t.Fatalf("unexpected reviews: %+v", got)
		}
		if len(got.Comments) != 1 || !got.CommentsTruncated {
			t.Fatalf("unexpected comments: %+v", got)
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := parseDiscussionJSON("not json"); err == nil {
			t.Error("expected an error for malformed json")
		}
	})
}

func TestReviewThreadsJSON(t *testing.T) {
	t.Run("merges threads and discussion into one versioned document", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(
				reviewThreadPage("T1", false, false)+reviewThreadPage("T2", true, false), "", nil,
			),
			commands.ExecCommandResult(discussionDoc(false, false), "", nil),
		)

		out, err := pm.ReviewThreadsJSON("42", "all")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var doc reviewThreadsDocument
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("--json output did not parse as JSON: %v\n%s", err, out)
		}
		if doc.SchemaVersion != 1 {
			t.Fatalf("expected schemaVersion 1, got %d", doc.SchemaVersion)
		}
		if len(doc.Threads) != 2 {
			t.Fatalf("expected 2 threads, got %d", len(doc.Threads))
		}
		if len(doc.Discussion.Reviews) != 1 || len(doc.Discussion.Comments) != 1 {
			t.Fatalf("unexpected discussion: %+v", doc.Discussion)
		}
	})

	t.Run("honors --state unresolved", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(
				reviewThreadPage("T1", false, false)+reviewThreadPage("T2", true, false), "", nil,
			),
			commands.ExecCommandResult(discussionDoc(false, false), "", nil),
		)

		out, err := pm.ReviewThreadsJSON("42", "unresolved")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var doc reviewThreadsDocument
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("--json output did not parse as JSON: %v", err)
		}
		if len(doc.Threads) != 1 || doc.Threads[0].ID != "T1" {
			t.Fatalf("expected only the unresolved thread, got %+v", doc.Threads)
		}
	})

	t.Run("propagates an error from gh", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResult("", "boom", fmt.Errorf("exit 1"))

		if _, err := pm.ReviewThreadsJSON("42", "all"); err == nil {
			t.Fatal("expected error")
		}
	})
}
