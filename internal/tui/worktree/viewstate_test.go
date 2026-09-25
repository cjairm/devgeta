package tuiworktree

// Pure-function tests for viewstate.go (ADR-0050 / step 6 of the ws
// dashboard cycle): encode/decode round-tripping and pruning stale keys.
// Model-level wiring (load on start, write triggers, best-effort failure) is
// covered in viewstate_model_test.go.

import (
	"testing"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestEncodeDecodeViewStateRoundTrip(t *testing.T) {
	collapsed := map[string]bool{
		"repo:hire2":       true,
		"wt:/path/to/tree": true,
		"sess:misc":        true,
		"repo:not-folded":  false, // must not appear in the encoded value
	}
	raw, err := encodeViewState(collapsed, 42)
	if err != nil {
		t.Fatalf("unexpected encode error: %v", err)
	}

	vs, ok := decodeViewState(raw)
	if !ok {
		t.Fatalf("expected decode to succeed for a freshly encoded value, raw=%q", raw)
	}
	if vs.Left != 42 {
		t.Errorf("expected Left=42, got %d", vs.Left)
	}
	got := map[string]bool{}
	for _, k := range vs.Collapsed {
		got[k] = true
	}
	want := map[string]bool{"repo:hire2": true, "wt:/path/to/tree": true, "sess:misc": true}
	if len(got) != len(want) {
		t.Fatalf("expected %d collapsed keys, got %d: %v", len(want), len(got), vs.Collapsed)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("expected collapsed key %q to round-trip, got %v", k, vs.Collapsed)
		}
	}
}

func TestDecodeViewStateIgnoresEmptyCorruptOrOldVersion(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty (unset option)", ""},
		{"corrupt json", "{not json"},
		{"unknown version", `{"v":99,"collapsed":["repo:a"],"left":40}`},
		{"missing version defaults to 0", `{"collapsed":["repo:a"],"left":40}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := decodeViewState(tc.raw); ok {
				t.Errorf("expected decode to report ok=false for %q", tc.raw)
			}
		})
	}
}

func TestPruneCollapsedDropsKeysForRowsThatNoLongerExist(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", Path: "/tmp/a"},
	}
	sessions := []worktree.SessionStatus{{Name: "misc"}}

	collapsed := map[string]bool{
		"repo:repo-a":    true, // still exists
		"wt:/tmp/a":      true, // still exists
		"sess:misc":      true, // still exists
		"repo:repo-gone": true, // repo deleted
		"wt:/tmp/gone":   true, // worktree deleted
		"sess:gone":      true, // session killed
		"pane:%1":        true, // pane keys are never persisted at all
	}

	valid := validCollapseKeys(statuses, sessions, nil)
	pruned := map[string]bool{}
	for k, v := range collapsed {
		if v && valid[k] {
			pruned[k] = true
		}
	}

	want := map[string]bool{"repo:repo-a": true, "wt:/tmp/a": true, "sess:misc": true}
	if len(pruned) != len(want) {
		t.Fatalf("expected pruned set %v, got %v", want, pruned)
	}
	for k := range want {
		if !pruned[k] {
			t.Errorf("expected %q to survive pruning, got %v", k, pruned)
		}
	}
}

// TestValidCollapseKeysIncludesRepoSessions guards a gap caught while
// implementing rename (ADR-0052 landed repo-session rows after this
// function was first written): a repo-session row's fold key is
// "sess:<name>", the exact same namespace a standalone session's pane-fold
// key uses (ADR-0050's own example JSON persists "sess:misc"), so a
// repo-session's key must survive pruning too, not just m.sessions'.
func TestValidCollapseKeysIncludesRepoSessions(t *testing.T) {
	repoSessions := []worktree.RepoSessionStatus{{Repo: "repo-a", Name: "repo-a-tien"}}

	valid := validCollapseKeys(nil, nil, repoSessions)
	if !valid["sess:repo-a-tien"] {
		t.Errorf("expected a repo-session's fold key to be valid, got %v", valid)
	}
}
