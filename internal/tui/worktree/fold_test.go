package tuiworktree

// Regression tests for B3: z used to toggle a standalone m.allCollapsed flag
// instead of reading the per-repo fold state it's supposed to summarize, so
// the two could disagree - collapsing every repo by hand (h on each) left
// the flag at its zero value false, and z's first press then "expanded"
// (already-collapsed) repos back into collapse, needing a second press to
// actually reveal anything.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestZExpandsInOnePressWhenReposWereCollapsedByHand(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", Path: "/tmp/a", TmuxWindow: "wt-feature-a"},
		{Name: "feature-x", Repo: "repo-b", Path: "/tmp/x", TmuxWindow: "wt-feature-x"},
	}
	m := makeTestModel(statuses)
	m.mgr = newTestWorktreeManager()

	// Collapse both repos by hand (as h would), never through z - so any
	// flag tracking "z last collapsed everything" stays at its zero value.
	m.collapsed[repoKey("repo-a")] = true
	m.collapsed[repoKey("repo-b")] = true
	m.rebuildRows()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'z'})
	m = updated.(Model)

	if m.collapsed[repoKey("repo-a")] || m.collapsed[repoKey("repo-b")] {
		t.Errorf(
			"expected a single z to expand both repos (neither is actually expanded), got repo-a=%v repo-b=%v",
			m.collapsed[repoKey("repo-a")],
			m.collapsed[repoKey("repo-b")],
		)
	}
}

func TestZWithNewExpandedRepoAmongCollapsedOnesCollapsesFirst(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", Path: "/tmp/a", TmuxWindow: "wt-feature-a"},
		{Name: "feature-x", Repo: "repo-b", Path: "/tmp/x", TmuxWindow: "wt-feature-x"},
	}
	m := makeTestModel(statuses)
	m.mgr = newTestWorktreeManager()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'z'}) // collapse both
	m = updated.(Model)
	if !m.collapsed[repoKey("repo-a")] || !m.collapsed[repoKey("repo-b")] {
		t.Fatal("test setup: expected the first z to collapse both repos")
	}

	// A new repo appears, expanded by default since it was never folded.
	m.statuses = append(m.statuses, worktree.WorktreeStatus{
		Name: "feature-c", Repo: "repo-c", Path: "/tmp/c", TmuxWindow: "wt-feature-c",
	})
	m.rebuildRows()

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'z'})
	m = updated.(Model)

	if !m.collapsed[repoKey("repo-a")] || !m.collapsed[repoKey("repo-b")] ||
		!m.collapsed[repoKey("repo-c")] {
		t.Errorf(
			"expected z to collapse every repo since repo-c was still expanded, got repo-a=%v repo-b=%v repo-c=%v",
			m.collapsed[repoKey("repo-a")],
			m.collapsed[repoKey("repo-b")],
			m.collapsed[repoKey("repo-c")],
		)
	}
}
