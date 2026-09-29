package tuiworktree

// Pure-function tests for viewstate.go (ADR-0050 / step 6 of the ws
// dashboard cycle): encode/decode round-tripping and pruning stale keys.
// Model-level wiring (load on start, write triggers, best-effort failure) is
// covered in viewstate_model_test.go.

import (
	"encoding/json"
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
	expanded := map[string]bool{
		"repo:windowless": true,
		"repo:untouched":  false, // must not appear in the encoded value
	}
	raw, err := encodeViewState(collapsed, expanded, 42, true, false, 5)
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
	if !vs.AgentsFolded {
		t.Errorf("expected AgentsFolded=true, got false")
	}
	if vs.SpacesFolded {
		t.Errorf("expected SpacesFolded=false, got true")
	}
	if vs.Split != 5 {
		t.Errorf("expected Split=5, got %d", vs.Split)
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

	gotExpanded := map[string]bool{}
	for _, k := range vs.Expanded {
		gotExpanded[k] = true
	}
	if len(gotExpanded) != 1 || !gotExpanded["repo:windowless"] {
		t.Errorf("expected expanded=[repo:windowless], got %v", vs.Expanded)
	}
}

// TestDecodeViewStateOldValueReadsAsDefaults confirms Step 10 of
// docs/plans/cycles/2026-09-28-ws-agents-section.md: a value encoded before
// the new fields existed (only v/collapsed/left) decodes as "both open,
// default split, no expanded repos" - an older binary's blob must still be
// readable by today's struct.
func TestDecodeViewStateOldValueReadsAsDefaults(t *testing.T) {
	oldValue := `{"v":1,"collapsed":["repo:a"],"left":40}`
	vs, ok := decodeViewState(oldValue)
	if !ok {
		t.Fatalf("expected an old-shape value to decode, raw=%q", oldValue)
	}
	if vs.AgentsFolded || vs.SpacesFolded {
		t.Errorf("expected both sections to read as open, got AgentsFolded=%v SpacesFolded=%v",
			vs.AgentsFolded, vs.SpacesFolded)
	}
	if vs.Split != 0 {
		t.Errorf("expected Split=0 (default), got %d", vs.Split)
	}
	if len(vs.Expanded) != 0 {
		t.Errorf("expected no expanded repos, got %v", vs.Expanded)
	}
}

// TestOlderStructStillReadsANewerValue confirms the ADR-0050/0056 forward-
// compatibility contract from the other direction: a value written WITH the
// new fields must still decode cleanly into the pre-Step-10 struct shape
// (an older binary running alongside a newer one) - the older binary simply
// never sees the new fields, exactly like any unknown JSON key.
func TestOlderStructStillReadsANewerValue(t *testing.T) {
	raw, err := encodeViewState(
		map[string]bool{"repo:a": true},
		map[string]bool{"repo:b": true},
		42, true, true, 7,
	)
	if err != nil {
		t.Fatalf("unexpected encode error: %v", err)
	}

	type oldViewStateV1 struct {
		V         int      `json:"v"`
		Collapsed []string `json:"collapsed"`
		Left      int      `json:"left"`
	}
	var old oldViewStateV1
	if err := json.Unmarshal([]byte(raw), &old); err != nil {
		t.Fatalf("expected the old struct shape to decode a newer value without error, got %v", err)
	}
	if old.V != viewStateVersion || old.Left != 42 || len(old.Collapsed) != 1 {
		t.Errorf("expected the old fields to still read correctly, got %+v", old)
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

// TestValidCollapseKeysIncludesRepoWindows guards the same gap for window
// rows (ADR-0052, amended): a window's pane-fold key is "win:<id>", and it
// must survive pruning like a standalone session's "sess:<name>" does.
func TestValidCollapseKeysIncludesRepoWindows(t *testing.T) {
	repoWindows := []worktree.RepoWindowStatus{
		{Repo: "repo-a", Session: "repo-a-tien", Window: "node", WindowID: "@1"},
	}

	valid := validCollapseKeys(nil, nil, repoWindows)
	if !valid["win:@1"] {
		t.Errorf("expected a window's fold key to be valid, got %v", valid)
	}
	if valid["sess:repo-a-tien"] {
		t.Errorf("a window row must not keep its session's key alive, got %v", valid)
	}
}
