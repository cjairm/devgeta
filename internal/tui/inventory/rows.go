// Package tuiinventory provides the shared Bubble Tea dashboard for browsing
// devgeta's tracked inventory, used by the `dg list` dashboard.
package tuiinventory

import (
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/inventory"
)

type rowKind int

const (
	rowGroup rowKind = iota
	rowItem
)

type groupMode int

const (
	groupByCategory groupMode = iota
	groupByStatus
)

type row struct {
	kind  rowKind
	group string // display label of the group (T1/T3 style)
	count int    // set for rowGroup only — total items in the group, even when collapsed
	item  inventory.Item
}

var categoryLabels = func() map[string]string {
	m := map[string]string{}
	for _, c := range inventory.Categories {
		m[c.Key] = c.Label
	}
	return m
}()

var categoryOrder = func() []string {
	order := make([]string, len(inventory.Categories))
	for i, c := range inventory.Categories {
		order[i] = c.Label
	}
	return order
}()

var statusOrder = []string{"MISSING", "UNKNOWN", "OK"}

func groupLabel(item inventory.Item, mode groupMode) string {
	if mode == groupByStatus {
		return item.State.String()
	}
	return categoryLabels[item.Category]
}

// buildRows filters items (problems-only, text filter), groups them per mode,
// sorts items alphabetically within each group, and returns header+item rows
// in display order. Groups with zero visible items are omitted entirely.
func buildRows(
	items []inventory.Item,
	mode groupMode,
	collapsed map[string]bool,
	filter string,
	problemsOnly bool,
) []row {
	filter = strings.ToLower(filter)

	groups := map[string][]inventory.Item{}
	for _, it := range items {
		if problemsOnly && it.State == inventory.StateOK {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(it.Name), filter) {
			continue
		}
		label := groupLabel(it, mode)
		groups[label] = append(groups[label], it)
	}

	order := categoryOrder
	if mode == groupByStatus {
		order = statusOrder
	}

	var rows []row
	for _, label := range order {
		visible := groups[label]
		if len(visible) == 0 {
			continue
		}
		sort.Slice(visible, func(i, j int) bool { return visible[i].Name < visible[j].Name })
		rows = append(rows, row{kind: rowGroup, group: label, count: len(visible)})
		if !collapsed[label] {
			for _, it := range visible {
				rows = append(rows, row{kind: rowItem, group: label, item: it})
			}
		}
	}
	return rows
}

// itemIndices returns row indices that are rowItem kind.
func itemIndices(rows []row) []int {
	var out []int
	for i, r := range rows {
		if r.kind == rowItem {
			out = append(out, i)
		}
	}
	return out
}

// navigableIndices returns indices that j/k visit: all item rows, plus
// collapsed group headers (so the user can reach a collapsed header and
// press l to expand it).
func navigableIndices(rows []row, collapsed map[string]bool) []int {
	var out []int
	for i, r := range rows {
		if r.kind == rowItem || (r.kind == rowGroup && collapsed[r.group]) {
			out = append(out, i)
		}
	}
	return out
}
