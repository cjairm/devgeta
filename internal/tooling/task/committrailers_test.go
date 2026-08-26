package task

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

func TestParseTrailerLines(t *testing.T) {
	t.Run("parses multiple trailers", func(t *testing.T) {
		got := parseTrailerLines("Co-authored-by: A <a@x.com>\nSigned-off-by: B <b@y.com>\n")
		want := []trailerLine{
			{Key: "Co-authored-by", Value: "A <a@x.com>"},
			{Key: "Signed-off-by", Value: "B <b@y.com>"},
		}
		if len(got) != len(want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("entry %d: got %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("empty input yields nil", func(t *testing.T) {
		if got := parseTrailerLines("   \n  "); got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})
}

func TestCheckRequiredTrailers(t *testing.T) {
	parsed := []trailerLine{{Key: "Co-authored-by", Value: "A <a@x.com>"}}

	t.Run("no required keys always passes", func(t *testing.T) {
		if err := checkRequiredTrailers(parsed, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("present key, case-insensitive, passes", func(t *testing.T) {
		if err := checkRequiredTrailers(parsed, []string{"co-authored-by"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("absent key fails", func(t *testing.T) {
		err := checkRequiredTrailers(parsed, []string{"Signed-off-by"})
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "Signed-off-by") {
			t.Fatalf("expected the missing key named in the error, got: %v", err)
		}
	})

	t.Run("one present, one absent: reports only the absent one", func(t *testing.T) {
		err := checkRequiredTrailers(parsed, []string{"Co-authored-by", "Signed-off-by"})
		if err == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(err.Error(), "Co-authored-by,") ||
			strings.Contains(err.Error(), "Co-authored-by ") {
			t.Fatalf("did not expect the present key to be reported missing: %v", err)
		}
		if !strings.Contains(err.Error(), "Signed-off-by") {
			t.Fatalf("expected Signed-off-by in the error, got: %v", err)
		}
	})
}

// scriptCommitTrailersGit intercepts the interpret-trailers call, capturing
// the temp file's content at call time (before CommitTrailers' own defer
// removes it) so tests can assert what was actually written, and returns
// trailersOut as git's canned response.
func scriptCommitTrailersGit(
	gitBase *commands.MockBaseCommand,
	trailersOut string,
) (capturedContent *string) {
	var content string
	gitBase.ExecCommandFn = func(cmd commands.CommandParams) (string, string, error) {
		if len(cmd.Args) > 0 {
			path := cmd.Args[len(cmd.Args)-1]
			if data, err := os.ReadFile(path); err == nil {
				content = string(data)
			}
		}
		return trailersOut, "", nil
	}
	return &content
}

func TestCommitTrailers(t *testing.T) {
	t.Run("writes the message to a temp file and parses it via git", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		message := "Subject\n\nCo-authored-by: A <a@x.com>\n"
		captured := scriptCommitTrailersGit(gitBase, "Co-authored-by: A <a@x.com>\n")

		out, err := tm.CommitTrailers(message, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "Co-authored-by: A <a@x.com>" {
			t.Fatalf("unexpected output: %q", out)
		}
		if *captured != message {
			t.Fatalf("expected the temp file to carry the message verbatim, got %q", *captured)
		}

		call := gitBase.GetLastExecCommandCall()
		if call.Args[0] != "interpret-trailers" || call.Args[1] != "--parse" ||
			call.Args[2] != "--only-trailers" {
			t.Fatalf("unexpected git args: %v", call.Args)
		}
	})

	t.Run("no trailers renders the sentinel", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		scriptCommitTrailersGit(gitBase, "")

		out, err := tm.CommitTrailers("Subject\n\nJust a body.\n", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "(no trailers)" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	// The round-4 regression: a prose label near the body, with NO --require,
	// must not be rejected. Verified against real git 2.51.1: this shape
	// parses to zero trailers, so the mock mirrors that here.
	t.Run("prose label with no --require is not rejected", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		scriptCommitTrailersGit(gitBase, "")

		out, err := tm.CommitTrailers("Subject\n\nReason: this is needed\nMore explanation.\n", nil)
		if err != nil {
			t.Fatalf("expected no error for an ordinary commit message, got: %v", err)
		}
		if out != "(no trailers)" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("a required trailer glued to the body fails", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		// Verified against real git: a trailer with no blank-line separator
		// from the body parses to nothing.
		scriptCommitTrailersGit(gitBase, "")

		_, err := tm.CommitTrailers(
			"Subject\nCo-authored-by: A <a@x.com>\n",
			[]string{"Co-authored-by"},
		)
		if err == nil {
			t.Fatal("expected an error for a required trailer git did not parse")
		}
	})

	t.Run("the same trailer, blank-separated, passes", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		scriptCommitTrailersGit(gitBase, "Co-authored-by: A <a@x.com>\n")

		out, err := tm.CommitTrailers(
			"Subject\n\nCo-authored-by: A <a@x.com>\n", []string{"Co-authored-by"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "Co-authored-by: A <a@x.com>" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("a required key genuinely absent fails", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		scriptCommitTrailersGit(gitBase, "")

		_, err := tm.CommitTrailers("Subject\n\nBody only.\n", []string{"Signed-off-by"})
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "Signed-off-by") {
			t.Fatalf("expected the missing key named, got: %v", err)
		}
	})

	t.Run("propagates a git error", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResult("", "fatal: bad object", fmt.Errorf("exit 1"))

		_, err := tm.CommitTrailers("Subject\n\nBody.\n", nil)
		if err == nil {
			t.Fatal("expected an error")
		}
	})
}
