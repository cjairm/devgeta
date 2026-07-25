package tuiworktree

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// TestRenderRightShowsPRTitleWhenCached verifies the PR title is rendered as
// the first header line when a non-empty title is cached for the selected
// worktree's path, and that no title line appears when the cached title is
// empty (the no-PR case).
func TestRenderRightShowsPRTitleWhenCached(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.diffBase = "main @abc1234"
	m.diffBranch = "feature-a" // display label only, not the cache key
	m.diffPath = "/tmp/a"      // matches testStatuses()'s feature-a; this is the cache key
	m.diffContent = "diff content"

	m.prTitles["/tmp/a"] = "Add cool feature"
	out := m.renderRight(80)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "Add cool feature") {
		t.Errorf("expected first line to contain PR title, got %q", lines[0])
	}

	// Empty cached title (no-PR branch): no title line, header stays first line.
	m.prTitles["/tmp/a"] = ""
	out2 := m.renderRight(80)
	lines2 := strings.Split(out2, "\n")
	if strings.Contains(lines2[0], "Add cool feature") {
		t.Errorf("did not expect stale title in output: %q", lines2[0])
	}
	if !strings.Contains(lines2[0], "main @abc1234") {
		t.Errorf("expected header line first when no title, got %q", lines2[0])
	}
}

// TestSelectionDispatchesPRTitleLookupWhenUncached verifies that selecting a
// worktree whose path has no cache entry and no pending lookup dispatches a
// prTitleCmd alongside the diff cmd, that the branch argument passed to
// prTitleFn is the display branch (s.Branch falling back to s.Name), and that
// feeding the resulting prTitleMsg back through Update populates the cache
// keyed by path.
func TestSelectionDispatchesPRTitleLookupWhenUncached(t *testing.T) {
	var calls int
	var gotBranch, gotPath string
	m := makeTestModel(testStatuses())
	m.prTitleFn = func(branch, path string) string {
		calls++
		gotBranch = branch
		gotPath = path
		return "PR for " + branch
	}

	updated, cmd := m.Update(statusesMsg(testStatuses()))
	m = updated.(Model)

	msgs := flattenCmd(cmd)
	var gotDiff, gotTitle bool
	var titleMsg prTitleMsg
	for _, msg := range msgs {
		switch v := msg.(type) {
		case diffMsg:
			gotDiff = true
		case prTitleMsg:
			gotTitle = true
			titleMsg = v
		}
	}
	if !gotDiff {
		t.Error("expected a diffMsg-producing cmd")
	}
	if !gotTitle {
		t.Fatal("expected a prTitleMsg-producing cmd for an uncached, non-pending path")
	}
	if titleMsg.path != "/tmp/a" {
		t.Errorf("expected prTitleMsg path %q, got %q", "/tmp/a", titleMsg.path)
	}
	if calls != 1 {
		t.Errorf("expected prTitleFn to be called exactly once, got %d", calls)
	}
	if gotBranch != "feature-a" {
		t.Errorf("expected prTitleFn branch arg %q, got %q", "feature-a", gotBranch)
	}
	if gotPath != "/tmp/a" {
		t.Errorf("expected prTitleFn path arg %q, got %q", "/tmp/a", gotPath)
	}

	// Feed the message back through Update: cache should be populated (keyed
	// by path) and pending cleared.
	updated2, _ := m.Update(titleMsg)
	m = updated2.(Model)
	if got := m.prTitles["/tmp/a"]; got != "PR for feature-a" {
		t.Errorf("expected cached title %q, got %q", "PR for feature-a", got)
	}
	if m.prTitlePending["/tmp/a"] {
		t.Error("expected pending flag cleared after prTitleMsg processed")
	}
}

// TestSelectionUsesBranchFieldWhenPresent verifies the branch argument passed
// to prTitleFn is s.Branch when set, not s.Name — testStatuses() entries all
// have an empty Branch, so this exercises the non-fallback path that those
// don't cover.
func TestSelectionUsesBranchFieldWhenPresent(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "wt-dir-name", Repo: "repo-a", Path: "/tmp/branched", Branch: "feat/real-branch"},
	}
	var gotBranch string
	m := makeTestModel(statuses)
	m.prTitleFn = func(branch, _ string) string {
		gotBranch = branch
		return ""
	}

	_, cmd := m.Update(statusesMsg(statuses))
	flattenCmd(cmd) // runs the cmd, populating gotBranch via the stub

	if gotBranch != "feat/real-branch" {
		t.Errorf(
			"expected prTitleFn branch arg %q (s.Branch), got %q",
			"feat/real-branch",
			gotBranch,
		)
	}
}

// TestPRTitleCacheKeyedByPathNotBranch is the regression test for the
// cross-repo collision bug: two worktrees in different repos sharing a
// branch name but with different paths must each get their own lookup and
// cache entry, and each must render its own title when selected — not the
// other's.
func TestPRTitleCacheKeyedByPathNotBranch(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "wt-one", Repo: "repo-a", Path: "/tmp/repo-a/main", Branch: "main"},
		{Name: "wt-two", Repo: "repo-b", Path: "/tmp/repo-b/main", Branch: "main"},
	}
	calls := map[string]int{}
	titles := map[string]string{
		"/tmp/repo-a/main": "repo-a's PR",
		"/tmp/repo-b/main": "repo-b's PR",
	}
	m := makeTestModel(statuses)
	m.prTitleFn = func(_, path string) string {
		calls[path]++
		return titles[path]
	}

	// Select the first worktree and process its prTitleMsg.
	updated, cmd := m.Update(statusesMsg(statuses))
	m = updated.(Model)
	for _, msg := range flattenCmd(cmd) {
		if tm, ok := msg.(prTitleMsg); ok {
			updated, _ = m.Update(tm)
			m = updated.(Model)
		}
	}
	m.diffPath = "/tmp/repo-a/main"
	m.diffBranch = "main"
	out := m.renderRight(80)
	if !strings.Contains(strings.Split(out, "\n")[0], "repo-a's PR") {
		t.Errorf("expected repo-a's own title rendered, got %q", out)
	}

	// Move to the second worktree (same branch name, different repo/path).
	updated2, cmd2 := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = updated2.(Model)
	var sawSecondLookup bool
	for _, msg := range flattenCmd(cmd2) {
		if tm, ok := msg.(prTitleMsg); ok {
			sawSecondLookup = true
			if tm.path != "/tmp/repo-b/main" {
				t.Errorf(
					"expected second lookup keyed by path %q, got %q",
					"/tmp/repo-b/main",
					tm.path,
				)
			}
			updated2, _ = m.Update(tm)
			m = updated2.(Model)
		}
	}
	if !sawSecondLookup {
		t.Fatal("expected a distinct prTitleMsg lookup for the second repo's same-named branch")
	}

	m.diffPath = "/tmp/repo-b/main"
	m.diffBranch = "main"
	out2 := m.renderRight(80)
	if !strings.Contains(strings.Split(out2, "\n")[0], "repo-b's PR") {
		t.Errorf("expected repo-b's own title rendered, got %q", out2)
	}

	if calls["/tmp/repo-a/main"] != 1 || calls["/tmp/repo-b/main"] != 1 {
		t.Errorf(
			"expected exactly one lookup per path, got repo-a=%d repo-b=%d",
			calls["/tmp/repo-a/main"], calls["/tmp/repo-b/main"],
		)
	}
	// Cache entries must be distinct despite the shared branch name.
	if m.prTitles["/tmp/repo-a/main"] != "repo-a's PR" ||
		m.prTitles["/tmp/repo-b/main"] != "repo-b's PR" {
		t.Errorf(
			"expected distinct cache entries per path, got repo-a=%q repo-b=%q",
			m.prTitles["/tmp/repo-a/main"], m.prTitles["/tmp/repo-b/main"],
		)
	}
}

// TestCachedPathNotReLookedUp verifies a worktree path already present in
// m.prTitles (even with an empty title) is not looked up again on a later
// selection.
func TestCachedPathNotReLookedUp(t *testing.T) {
	var calls int
	m := makeTestModel(testStatuses())
	m.prTitleFn = func(_, _ string) string {
		calls++
		return "should not be called"
	}
	// Cache every path in testStatuses() up front (feature-a/"/tmp/a" is the
	// one under direct test, but j moves the cursor to feature-b/"/tmp/b",
	// which must also be pre-cached or that move would trigger a legitimate
	// lookup of its own and confound this test).
	m.prTitles["/tmp/a"] = "" // cached: no PR
	m.prTitles["/tmp/b"] = "Some cached title"

	updated, cmd := m.Update(statusesMsg(testStatuses()))
	m = updated.(Model)

	for _, msg := range flattenCmd(cmd) {
		if _, ok := msg.(prTitleMsg); ok {
			t.Fatal("did not expect a prTitleMsg for an already-cached path")
		}
	}
	if calls != 0 {
		t.Errorf("expected prTitleFn not to be called, got %d calls", calls)
	}

	// Also verify j selection (moving to the also-cached /tmp/b) doesn't
	// dispatch a new lookup.
	updated2, cmd2 := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = updated2.(Model)
	for _, msg := range flattenCmd(cmd2) {
		if _, ok := msg.(prTitleMsg); ok {
			t.Fatal("did not expect a prTitleMsg after moving to a still-cached path context")
		}
	}
	if calls != 0 {
		t.Errorf(
			"expected prTitleFn not to be called after moving between cached paths, got %d calls",
			calls,
		)
	}
}

// TestPendingPathNotReLookedUp verifies a path already marked pending is not
// dispatched again (no duplicate concurrent lookups).
func TestPendingPathNotReLookedUp(t *testing.T) {
	var calls int
	m := makeTestModel(testStatuses())
	m.prTitleFn = func(_, _ string) string {
		calls++
		return "x"
	}
	m.prTitlePending["/tmp/a"] = true

	_, cmd := m.Update(statusesMsg(testStatuses()))
	for _, msg := range flattenCmd(cmd) {
		if _, ok := msg.(prTitleMsg); ok {
			t.Fatal("did not expect a prTitleMsg for a path already pending")
		}
	}
	if calls != 0 {
		t.Errorf("expected prTitleFn not to be called while pending, got %d calls", calls)
	}
}

// TestEmptyTitleCachedAndNotRetried verifies that a worktree with no PR
// (empty title returned by prTitleFn) is cached as "" (keyed by path) and is
// not retried on a subsequent selection.
func TestEmptyTitleCachedAndNotRetried(t *testing.T) {
	var calls int
	m := makeTestModel(testStatuses())
	m.prTitleFn = func(_, _ string) string {
		calls++
		return "" // no PR for this worktree
	}

	updated, cmd := m.Update(statusesMsg(testStatuses()))
	m = updated.(Model)
	msgs := flattenCmd(cmd)

	var titleMsg prTitleMsg
	found := false
	for _, msg := range msgs {
		if v, ok := msg.(prTitleMsg); ok {
			titleMsg = v
			found = true
		}
	}
	if !found {
		t.Fatal("expected a prTitleMsg on first selection")
	}
	if calls != 1 {
		t.Errorf("expected exactly one prTitleFn call, got %d", calls)
	}

	updated2, _ := m.Update(titleMsg)
	m = updated2.(Model)
	title, cached := m.prTitles["/tmp/a"]
	if !cached || title != "" {
		t.Errorf("expected path cached with empty title, got cached=%v title=%q", cached, title)
	}

	// Re-selecting (e.g. simulating a tick reload with the same statuses)
	// must not call prTitleFn again.
	_, cmd2 := m.Update(statusesMsg(testStatuses()))
	for _, msg := range flattenCmd(cmd2) {
		if _, ok := msg.(prTitleMsg); ok {
			t.Fatal("did not expect a prTitleMsg for a path already cached with an empty title")
		}
	}
	if calls != 1 {
		t.Errorf("expected prTitleFn call count to remain 1 after re-selection, got %d", calls)
	}
}
