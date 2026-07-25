package tuiinventory

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/inventory"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

func testItems() []inventory.Item {
	return []inventory.Item{
		{Name: "git", Category: "packages", Source: "installed", State: inventory.StateOK},
		{Name: "tmux", Category: "packages", Source: "installed", State: inventory.StateMissing},
		{Name: "docker", Category: "desktop_apps", Source: "installed", State: inventory.StateOK},
	}
}

func TestNewModel_InitialCursorOnItemRow(t *testing.T) {
	m := newModel(testItems(), Options{})
	if m.rows[m.cursor].kind != rowItem {
		t.Error("initial cursor should be on an item row")
	}
}

func TestNewModel_CategoryPreFilter(t *testing.T) {
	m := newModel(testItems(), Options{Category: "packages"})
	for _, it := range m.items {
		if it.Category != "packages" {
			t.Errorf("expected only packages items after pre-filter, found %+v", it)
		}
	}
	if m.title != "Packages" {
		t.Errorf("expected title %q, got %q", "Packages", m.title)
	}
}

func TestUpdate_HelpOverlay(t *testing.T) {
	m := newModel(testItems(), Options{})
	m.width, m.height = 80, 24

	m2, _ := m.Update(tea.KeyPressMsg{Code: '?'})
	m3 := m2.(model)
	if !m3.showHelp {
		t.Fatal("? should open the help overlay")
	}
	content := m3.renderContent()
	if !strings.Contains(content, "problems only") {
		t.Error("help overlay should explain the p keybinding")
	}

	// Any key closes it without acting on the list
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'p'})
	m5 := m4.(model)
	if m5.showHelp {
		t.Error("any key should close the help overlay")
	}
	if m5.problemsOnly {
		t.Error("the closing key must not also toggle problems-only")
	}
}

func TestView_ProblemsOnlyStateIsVisible(t *testing.T) {
	// All items OK: problems-only shows a message instead of an empty pane.
	items := []inventory.Item{
		{Name: "git", Category: "packages", Source: "installed", State: inventory.StateOK},
	}
	m := newModel(items, Options{})
	m.width, m.height = 80, 24

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'p'})
	m3 := m2.(model)
	content := m3.renderContent()
	if !strings.Contains(content, "no problems") {
		t.Error("problems-only with nothing missing should say so instead of rendering empty")
	}
	if !strings.Contains(content, "problems only") {
		t.Error("pane title should show the problems-only mode")
	}
	if !strings.Contains(content, "show all") {
		t.Error("hint bar should flip p to 'show all' while problems-only is active")
	}
}

func TestUpdate_QuitOnQ(t *testing.T) {
	m := newModel(testItems(), Options{})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
}

func TestUpdate_ToggleProblemsOnly(t *testing.T) {
	m := newModel(testItems(), Options{})
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'p'})
	m3 := m2.(model)
	if !m3.problemsOnly {
		t.Error("p should toggle problemsOnly on")
	}
	for _, r := range m3.rows {
		if r.kind == rowItem && r.item.State == inventory.StateOK {
			t.Error("after toggling problems-only, OK items should be hidden")
		}
	}
}

func TestUpdate_ToggleGroupMode(t *testing.T) {
	m := newModel(testItems(), Options{})
	if m.groupMode != groupByCategory {
		t.Fatal("expected initial groupMode to be groupByCategory")
	}
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'g'})
	m3 := m2.(model)
	if m3.groupMode != groupByStatus {
		t.Error("g should toggle groupMode to groupByStatus")
	}
}

func TestUpdate_CollapseExpandGroup(t *testing.T) {
	m := newModel(testItems(), Options{})
	// h collapses the selected item's group and lands the cursor on its header.
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m3 := m2.(model)
	if m3.rows[m3.cursor].kind != rowGroup {
		t.Fatalf(
			"after h, cursor should be on a group header, got kind %v",
			m3.rows[m3.cursor].kind,
		)
	}
	collapsedGroup := m3.rows[m3.cursor].group
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'l'})
	m5 := m4.(model)
	if m5.rows[m5.cursor].kind != rowItem {
		t.Fatal("after l, cursor should return to an item row")
	}
	if m5.collapsed[collapsedGroup] {
		t.Error("l should have expanded the group")
	}
}

func TestUpdate_FilterMode(t *testing.T) {
	m := newModel(testItems(), Options{})
	m2, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	m3 := m2.(model)
	if !m3.filter.Active {
		t.Fatal("/ should enter filtering mode")
	}
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'g'})
	m5 := m4.(model)
	if m5.filter.Value() != "g" {
		t.Errorf("expected filter %q, got %q", "g", m5.filter.Value())
	}
	m6, _ := m5.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m7 := m6.(model)
	if m7.filter.Active || m7.filter.Value() != "" {
		t.Error("esc should clear filter and exit filtering mode")
	}
}

func TestUpdate_FilterModePaste(t *testing.T) {
	m := newModel(testItems(), Options{})
	m2, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	m3 := m2.(model)

	m4, _ := m3.Update(tea.PasteMsg{Content: "git"})
	m5 := m4.(model)
	if m5.filter.Value() != "git" {
		t.Errorf("expected filter %q, got %q", "git", m5.filter.Value())
	}
}

// testItems() groups into rows: [group Packages(2), git, tmux, group Desktop
// Apps(1), docker] — item rows at indices 1 (git), 2 (tmux), 4 (docker).
// newModel starts the cursor on the first item row (git, index 1).

func TestUpdate_CursorWrapsForwardPastLastItem(t *testing.T) {
	m := newModel(testItems(), Options{})
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j'}) // git -> tmux
	m3 := m2.(model)
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'j'}) // tmux -> docker (last item row)
	m5 := m4.(model)
	if m5.rows[m5.cursor].item.Name != "docker" {
		t.Fatalf("expected cursor on docker before wraparound, got %+v", m5.rows[m5.cursor])
	}
	m6, _ := m5.Update(tea.KeyPressMsg{Code: 'j'}) // wrap: docker -> git
	m7 := m6.(model)
	if m7.rows[m7.cursor].kind != rowItem || m7.rows[m7.cursor].item.Name != "git" {
		t.Errorf("j past last item should wrap to first item row, got %+v", m7.rows[m7.cursor])
	}
}

func TestUpdate_CursorWrapsBackwardPastFirstItem(t *testing.T) {
	m := newModel(testItems(), Options{})
	if m.rows[m.cursor].item.Name != "git" {
		t.Fatalf("expected initial cursor on git (first item row), got %+v", m.rows[m.cursor])
	}
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k'}) // wrap: git -> docker (last item row)
	m3 := m2.(model)
	if m3.rows[m3.cursor].kind != rowItem || m3.rows[m3.cursor].item.Name != "docker" {
		t.Errorf("k before first item should wrap to last item row, got %+v", m3.rows[m3.cursor])
	}
}

func TestUpdate_JKNavigateOverCollapsedGroupHeader(t *testing.T) {
	m := newModel(testItems(), Options{})
	// Collapse the Packages group (cursor starts on git, inside Packages).
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m3 := m2.(model)
	if m3.rows[m3.cursor].kind != rowGroup || m3.rows[m3.cursor].group != "Packages" {
		t.Fatalf(
			"expected cursor on collapsed Packages header, got %+v",
			m3.rows[m3.cursor],
		)
	}

	// j from the collapsed header should skip the (expanded) Desktop Apps
	// header — which isn't navigable — and land on docker, its only item.
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'j'})
	m5 := m4.(model)
	if m5.rows[m5.cursor].kind != rowItem || m5.rows[m5.cursor].item.Name != "docker" {
		t.Errorf(
			"j from collapsed header should land on docker item row, got %+v",
			m5.rows[m5.cursor],
		)
	}

	// k back from docker should return to the collapsed Packages header,
	// confirming navigableIndices treats it as a stop even though its
	// item rows are hidden.
	m6, _ := m5.Update(tea.KeyPressMsg{Code: 'k'})
	m7 := m6.(model)
	if m7.rows[m7.cursor].kind != rowGroup || m7.rows[m7.cursor].group != "Packages" {
		t.Errorf(
			"k from docker should return to collapsed Packages header, got %+v",
			m7.rows[m7.cursor],
		)
	}
}

func TestUpdate_JKOnEmptyRowsDoesNotPanic(t *testing.T) {
	m := newModel(testItems(), Options{})
	// Enter filter mode and type a filter that matches nothing.
	m2, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	m3 := m2.(model)
	for _, c := range "zzz" {
		next, _ := m3.Update(tea.KeyPressMsg{Code: c})
		m3 = next.(model)
	}
	if len(m3.rows) != 0 {
		t.Fatalf("expected filter %q to match zero rows, got %d", m3.filter.Value(), len(m3.rows))
	}
	// Exit filtering mode so j/k reach the navigation branch.
	m4, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(model)
	if m5.filter.Active {
		t.Fatal("enter should exit filtering mode")
	}

	m6, _ := m5.Update(tea.KeyPressMsg{Code: 'j'})
	m7 := m6.(model)
	if m7.cursor != 0 {
		t.Errorf("j on empty rows should leave cursor at 0, got %d", m7.cursor)
	}

	m8, _ := m7.Update(tea.KeyPressMsg{Code: 'k'})
	m9 := m8.(model)
	if m9.cursor != 0 {
		t.Errorf("k on empty rows should leave cursor at 0, got %d", m9.cursor)
	}
}

func TestView_NoPanicAtVariousSizes(t *testing.T) {
	m := newModel(testItems(), Options{})
	sizes := []tea.WindowSizeMsg{
		{Width: 0, Height: 0},
		{Width: 20, Height: 10},
		{Width: 120, Height: 40},
	}
	for _, sz := range sizes {
		m2, _ := m.Update(sz)
		mm := m2.(model)
		v := mm.View()
		_ = v
	}
}
