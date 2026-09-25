package tuiworktree

// Tests for ADR-0053 in the dashboard: d d never forces, a refused removal
// says what is at risk and offers F F, and only F F forces.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func focusWorktree(t *testing.T, m Model, name string) Model {
	t.Helper()
	if _, ok := m.focusRow(func(r row) bool {
		return r.kind == rowWorktree && r.status.Name == name
	}); !ok {
		t.Fatalf("test setup: no worktree row %q", name)
	}
	return m
}

func press(t *testing.T, m Model, key rune) (Model, tea.Cmd) {
	t.Helper()
	mi, cmd := m.Update(tea.KeyPressMsg{Code: key})
	return mi.(Model), cmd
}

func TestDeleteNeverForces(t *testing.T) {
	m := focusWorktree(t, makeTestModel(testStatuses()), "feature-a")
	var forced []bool
	m.removeFn = func(_, _ string, force bool) error {
		forced = append(forced, force)
		return nil
	}

	m, _ = press(t, m, 'd')
	_, cmd := press(t, m, 'd')
	if cmd == nil {
		t.Fatal("second d should run the removal")
	}
	cmd()
	if len(forced) != 1 || forced[0] {
		t.Errorf("d d must remove without force, got force=%v", forced)
	}
}

func TestRefusedDeleteOffersForce(t *testing.T) {
	m := focusWorktree(t, makeTestModel(testStatuses()), "feature-a")
	var forced []bool
	m.removeFn = func(_, name string, force bool) error {
		forced = append(forced, force)
		if !force {
			return &worktree.WouldLoseWorkError{
				Name: name, Risk: worktree.WorkAtRisk{Dirty: true, Unpushed: 2},
			}
		}
		return nil
	}

	m, _ = press(t, m, 'd')
	m, cmd := press(t, m, 'd')
	mi, _ := m.Update(cmd())
	m = mi.(Model)
	if !strings.Contains(m.status, "uncommitted changes and 2 unpushed commits") ||
		!strings.Contains(m.status, "F F") {
		t.Errorf("status should name the risk and offer F F, got %q", m.status)
	}

	m, _ = press(t, m, 'F')
	if hint := m.renderHint(200); !strings.Contains(hint, "lose uncommitted changes and 2 unpushed commits") {
		t.Errorf("F's armed hint should name what is lost, got %q", hint)
	}
	_, cmd = press(t, m, 'F')
	if cmd == nil {
		t.Fatal("second F should run the forced removal")
	}
	if _, ok := cmd().(deletedMsg); !ok {
		t.Error("expected the forced removal to report deletedMsg")
	}
	if len(forced) != 2 || !forced[1] {
		t.Errorf("F F must force, got %v", forced)
	}
}

// A lone F followed by any other key disarms, the same as d.
func TestForceDeleteDisarmsOnOtherKey(t *testing.T) {
	m := focusWorktree(t, makeTestModel(testStatuses()), "feature-a")
	m, _ = press(t, m, 'F')
	if m.pendingForceDelete == "" {
		t.Fatal("first F should arm")
	}
	m, _ = press(t, m, 'j')
	if m.pendingForceDelete != "" {
		t.Error("j should disarm F")
	}
}
