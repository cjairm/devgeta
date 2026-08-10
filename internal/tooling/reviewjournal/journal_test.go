package reviewjournal

import (
	"strings"
	"testing"
)

// --- PR keys (ADR-0023 §4: the journal's second key namespace) ---

// The writer of a PR key and the reader that has to tell one from a branch
// must agree, so what PRKey builds is exactly what IsPRKey recognizes — and a
// branch name is not mistaken for a PR.
func TestPRKeyIsRecognizedAndBranchesAreNot(t *testing.T) {
	key := PRKey("Employ-Inc", "employ-agent", "213")
	if key != "pr/Employ-Inc/employ-agent/213" {
		t.Fatalf("unexpected journal key %q", key)
	}
	if !IsPRKey(key) {
		t.Errorf("IsPRKey(%q) = false, want true", key)
	}
	for _, branch := range []string{"main", "fix/pr-review-loop", "print/pr", ""} {
		if IsPRKey(branch) {
			t.Errorf("IsPRKey(%q) = true, want false", branch)
		}
	}
}

// --- encoding (ADR-0012 acceptance gate: hostile names, no collision, no escape) ---

func TestEncodeBranchRoundTripsAndNeverCollides(t *testing.T) {
	cases := []string{
		"main",
		"fix/retry-context",
		"fix/a-b",
		"fix/a/b",
		"feature/añadir-ñ", // multibyte
		"weird %25 percent",
		"../../etc/passwd",
		"..",
		"a..b",
		"tab\tand space",
	}
	seen := map[string]string{}
	for _, branch := range cases {
		enc := EncodeBranch(branch)
		if strings.ContainsAny(enc, "/\\") {
			t.Errorf("EncodeBranch(%q) = %q contains a path separator", branch, enc)
		}
		if prev, dup := seen[enc]; dup {
			t.Errorf("collision: %q and %q both encode to %q", prev, branch, enc)
		}
		seen[enc] = branch
		dec, err := DecodeBranch(enc)
		if err != nil {
			t.Fatalf("DecodeBranch(%q): %v", enc, err)
		}
		if dec != branch {
			t.Errorf("round trip lost data: %q -> %q -> %q", branch, enc, dec)
		}
	}
}

// The two names a lossy slash-to-hyphen slug would merge must stay distinct —
// this is the collision the ADR names explicitly.
func TestEncodeBranchSlashVsHyphenDistinct(t *testing.T) {
	if EncodeBranch("fix/a-b") == EncodeBranch("fix-a/b") {
		t.Fatal("fix/a-b and fix-a/b must encode differently")
	}
}

func TestDecodeBranchRejectsMalformed(t *testing.T) {
	for _, enc := range []string{"a%2", "a%ZZ", "raw/slash", "sp ace"} {
		if _, err := DecodeBranch(enc); err == nil {
			t.Errorf("DecodeBranch(%q) should fail", enc)
		}
	}
}

// --- model ---

func TestNextIDNeverReusesAfterDeletion(t *testing.T) {
	j := &Journal{Entries: []Entry{{ID: "n1"}, {ID: "n7"}}}
	if got := j.nextID(); got != "n8" {
		t.Fatalf("expected n8 (sequence from the highest ever assigned), got %s", got)
	}
}

// --- render/parse round trip ---

func TestRenderParseRoundTrip(t *testing.T) {
	j := &Journal{
		Branch:     "fix/retry-context",
		Base:       "origin/main",
		LastReview: "2026-08-05",
		Entries: []Entry{
			{
				ID: "n1", Resolution: ResolutionRejected, Cite: "client.go:42",
				Note:   "N+1 query in the retry loop",
				Answer: "intentional, batch size is capped by config.MaxBatch",
				Blob:   "4485816", Head: "a1b2c3d",
			},
			{
				ID: "n2", Resolution: ResolutionAnswered,
				Note: "Does the retry reuse the outer context?", Answer: "Yes",
				Head: "a1b2c3d",
			},
			{
				ID: "n3", Cite: "store.go:12",
				Note: "[IMPORTANT] write is not atomic;\na crash mid-write truncates the file",
				Blob: "77aa091", Head: "d4e5f6a",
			},
		},
	}

	got := Parse(j.Branch, []byte(j.Render()))
	if got.Base != j.Base || got.LastReview != j.LastReview {
		t.Fatalf("frontmatter lost: %+v", got)
	}
	if len(got.Entries) != len(j.Entries) {
		t.Fatalf("expected %d entries, got %d:\n%s", len(j.Entries), len(got.Entries), j.Render())
	}
	byID := map[string]Entry{}
	for _, e := range got.Entries {
		byID[e.ID] = e
	}
	for _, want := range j.Entries {
		g, ok := byID[want.ID]
		if !ok {
			t.Fatalf("entry %s lost in round trip", want.ID)
		}
		// Multi-line note continuation joins on \n after trimming.
		wantNote := strings.ReplaceAll(want.Note, ";\n", ";\n")
		if g.Resolution != want.Resolution || g.Cite != want.Cite ||
			g.Blob != want.Blob || g.Head != want.Head ||
			g.Answer != want.Answer || g.Note != wantNote {
			t.Errorf("entry %s mismatch:\n got %+v\nwant %+v", want.ID, g, want)
		}
	}
}

func TestParseToleratesHandEditedProse(t *testing.T) {
	data := `---
branch: feat
---

Some hand-written preamble nobody asked for.

## Open

- [n1] ` + "`a.go`" + ` — the note
  a hand-added clarification line
  (blob abc123, head def456)
`
	j := Parse("feat", []byte(data))
	if len(j.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(j.Entries))
	}
	e := j.Entries[0]
	if !strings.Contains(e.Note, "hand-added clarification") {
		t.Errorf("hand-edited continuation lost: %q", e.Note)
	}
	if e.Blob != "abc123" || e.Head != "def456" {
		t.Errorf("stamp lost: %+v", e)
	}
}

func TestCitedFileStripsLine(t *testing.T) {
	if got := (Entry{Cite: "store.go:12"}).CitedFile(); got != "store.go" {
		t.Fatalf("expected store.go, got %q", got)
	}
	if got := (Entry{}).CitedFile(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
