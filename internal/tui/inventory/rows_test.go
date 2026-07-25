package tuiinventory

import (
	"testing"

	"github.com/cjairm/devgeta/internal/inventory"
)

func sampleItems() []inventory.Item {
	return []inventory.Item{
		{Name: "git", Category: "packages", Source: "installed", State: inventory.StateOK},
		{Name: "tmux", Category: "packages", Source: "installed", State: inventory.StateMissing},
		{Name: "docker", Category: "desktop_apps", Source: "installed", State: inventory.StateOK},
		{
			Name:     "JetBrainsMono",
			Category: "fonts",
			Source:   "installed",
			State:    inventory.StateUnknown,
		},
	}
}

func TestBuildRows_GroupedByCategory(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{}, "", false)
	// Packages(2) header+2 items, Desktop Apps(1) header+1, Fonts(1) header+1 = 3 headers + 4 items = 7
	if len(rows) != 7 {
		t.Fatalf("expected 7 rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].kind != rowGroup || rows[0].group != "Packages" || rows[0].count != 2 {
		t.Errorf("expected first row to be Packages header with count 2, got %+v", rows[0])
	}
}

func TestBuildRows_ProblemsOnlyHidesOK(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{}, "", true)
	for _, r := range rows {
		if r.kind == rowItem && r.item.State == inventory.StateOK {
			t.Errorf("problems-only filter should hide OK items, found %+v", r.item)
		}
	}
}

func TestBuildRows_TextFilterMatchesItemName(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{}, "git", false)
	itemCount := 0
	for _, r := range rows {
		if r.kind == rowItem {
			itemCount++
			if r.item.Name != "git" {
				t.Errorf("filter 'git' should only match item 'git', got %q", r.item.Name)
			}
		}
	}
	if itemCount != 1 {
		t.Errorf("expected exactly 1 matching item, got %d", itemCount)
	}
}

func TestBuildRows_CollapsedGroupHidesItemsButKeepsCount(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{"Packages": true}, "", false)
	for _, r := range rows {
		if r.kind == rowGroup && r.group == "Packages" {
			if r.count != 2 {
				t.Errorf("collapsed Packages header should still report count 2, got %d", r.count)
			}
		}
		if r.kind == rowItem && r.item.Category == "packages" {
			t.Errorf("collapsed group should hide its item rows, found %+v", r.item)
		}
	}
}

func TestBuildRows_GroupedByStatus(t *testing.T) {
	rows := buildRows(sampleItems(), groupByStatus, map[string]bool{}, "", false)
	if rows[0].kind != rowGroup || rows[0].group != "MISSING" {
		t.Errorf("status grouping should show MISSING first, got %+v", rows[0])
	}
}

func TestItemIndices_OnlySkipsGroupRows(t *testing.T) {
	rows := buildRows(sampleItems(), groupByCategory, map[string]bool{}, "", false)
	indices := itemIndices(rows)
	for _, i := range indices {
		if rows[i].kind != rowItem {
			t.Errorf("index %d should point to a rowItem, got kind %v", i, rows[i].kind)
		}
	}
	if len(indices) != 4 {
		t.Errorf("expected 4 item rows, got %d", len(indices))
	}
}
