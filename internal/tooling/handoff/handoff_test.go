package handoff

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderIncludesFrontMatterAndBody(t *testing.T) {
	n := &Note{
		Branch:  "feat/login",
		Updated: "2026-08-26T10:00:00Z",
		Head:    "deadbeef",
		Body:    "Left off wiring the OAuth callback.\n",
	}
	got, err := n.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"branch: feat/login",
		"updated: 2026-08-26T10:00:00Z",
		"head: deadbeef",
		"Left off wiring the OAuth callback.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render output missing %q, got:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("Render output does not start with front matter delimiter, got:\n%s", got)
	}
}

func TestParseRoundTripsWithRender(t *testing.T) {
	want := &Note{
		Branch:  "feat/login",
		Updated: "2026-08-26T10:00:00Z",
		Head:    "deadbeef",
		Body:    "Left off wiring the OAuth callback.\nNext: add the token refresh path.\n",
	}
	rendered, err := want.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := Parse(want.Branch, []byte(rendered))
	if got.Branch != want.Branch || got.Updated != want.Updated || got.Head != want.Head ||
		got.Body != want.Body {
		t.Errorf("round trip mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestParseOnEmptyDataReturnsAnEmptyNoteForBranch(t *testing.T) {
	got := Parse("feat/login", nil)
	if got.Branch != "feat/login" {
		t.Errorf("Branch = %q, want %q", got.Branch, "feat/login")
	}
	if got.Updated != "" || got.Head != "" || got.Body != "" {
		t.Errorf("expected an empty note, got %+v", got)
	}
}

func TestParseIgnoresFrontMatterBranchAndUsesTheParamInstead(t *testing.T) {
	// The frontmatter's own branch copy is informational, same convention as
	// reviewjournal.Parse: the caller's branch (the file's identity) wins.
	data := "---\nbranch: some-other-branch\n---\nbody text\n"
	got := Parse("feat/login", []byte(data))
	if got.Branch != "feat/login" {
		t.Errorf("Branch = %q, want the param %q", got.Branch, "feat/login")
	}
	if got.Body != "body text\n" {
		t.Errorf("Body = %q, want %q", got.Body, "body text\n")
	}
}

func TestRenderRefusesOverMaxBytesAndLeavesNothingRendered(t *testing.T) {
	n := &Note{Branch: "feat", Body: strings.Repeat("x", MaxBytes)}
	got, err := n.Render()
	if !errors.Is(err, ErrNoteTooLarge) {
		t.Fatalf("err = %v, want ErrNoteTooLarge", err)
	}
	if got != "" {
		t.Errorf("Render returned content alongside the error: %q", got)
	}
}

func TestRenderAcceptsExactlyMaxBytes(t *testing.T) {
	// Binary search the body length that renders to exactly MaxBytes, to pin
	// the boundary as inclusive: "over the cap" errors, "at the cap" does not.
	n := &Note{Branch: "feat"}
	lo, hi := 0, MaxBytes
	for lo < hi {
		mid := (lo + hi + 1) / 2
		n.Body = strings.Repeat("x", mid)
		rendered, err := n.Render()
		if err == nil && len(rendered) <= MaxBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	n.Body = strings.Repeat("x", lo)
	rendered, err := n.Render()
	if err != nil {
		t.Fatalf("Render at the boundary: %v", err)
	}
	if len(rendered) != MaxBytes {
		t.Fatalf(
			"boundary search landed on %d bytes, not exactly MaxBytes (%d)",
			len(rendered),
			MaxBytes,
		)
	}

	n.Body = strings.Repeat("x", lo+1)
	if _, err := n.Render(); !errors.Is(err, ErrNoteTooLarge) {
		t.Errorf("one byte past the boundary should refuse, err = %v", err)
	}
}

func TestRenderCountsFrontMatterTowardTheCap(t *testing.T) {
	// A body alone under MaxBytes must still be refused once front matter
	// pushes the FULL rendered file over the cap — the measurement is on the
	// rendered file, not the body alone.
	n := &Note{
		Branch:  "feat",
		Updated: "2026-08-26T10:00:00Z",
		Head:    "deadbeef",
		Body:    strings.Repeat("x", MaxBytes-1),
	}
	if _, err := n.Render(); !errors.Is(err, ErrNoteTooLarge) {
		t.Errorf("front matter should count toward the cap, err = %v", err)
	}
}
