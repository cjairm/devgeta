package tuiinventory

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cjairm/devgeta/internal/inventory"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

// Options configures the dashboard's initial filter state.
type Options struct {
	Category string // pre-filter to a single category key (e.g. "fonts"); "" = all
}

type model struct {
	items []inventory.Item
	title string

	rows      []row
	cursor    int
	collapsed map[string]bool
	groupMode groupMode

	problemsOnly bool
	filter       tuicomponents.FilterField
	showHelp     bool

	width, height int

	palette *tuicomponents.Palette
}

func newModel(items []inventory.Item, opts Options) model {
	title := "inventory"
	if opts.Category != "" {
		if label, ok := categoryLabels[opts.Category]; ok {
			title = label
		}
		var filtered []inventory.Item
		for _, it := range items {
			if it.Category == opts.Category {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	m := model{
		items:     items,
		title:     title,
		collapsed: map[string]bool{},
		groupMode: groupByCategory,
		palette:   tuicomponents.NewPalette(),
	}
	m.rebuildRows()
	return m
}

func (m *model) rebuildRows() {
	m.rows = buildRows(m.items, m.groupMode, m.collapsed, m.filter.Value(), m.problemsOnly)
	m.cursor = tuicomponents.ClampCursor(itemIndices(m.rows), m.cursor)
}

func (m *model) moveCursor(delta int) {
	m.cursor = tuicomponents.MoveCursor(navigableIndices(m.rows, m.collapsed), m.cursor, delta)
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		return m.handlePaste(msg.Content)
	}
	return m, nil
}

// handlePaste inserts bracketed-paste content into the active filter in one
// shot. Bubble Tea delivers a paste as a single tea.PasteMsg carrying the
// whole clipboard content, and FilterField.HandleKey's default case only
// accepts single-rune keys, so routing a paste through handleKey would
// silently drop all but its first rune.
func (m model) handlePaste(text string) (tea.Model, tea.Cmd) {
	if m.showHelp {
		return m, nil
	}
	if m.filter.Active {
		if m.filter.InsertText(text) {
			m.rebuildRows()
		}
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Help overlay: any key dismisses it
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	if m.filter.Active {
		if m.filter.HandleKey(key) {
			m.rebuildRows()
		}
		return m, nil
	}

	switch key {
	case "?":
		m.showHelp = true
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j":
		m.moveCursor(1)
	case "k":
		m.moveCursor(-1)
	case "h":
		if g := m.cursorGroup(); g != "" {
			m.collapsed[g] = true
			m.rebuildRows()
			m.landCursorOnGroup(g)
		}
	case "l":
		if g := m.cursorGroup(); g != "" {
			m.collapsed[g] = false
			m.rebuildRows()
		}
	case "/":
		m.filter.Active = true
	case "p":
		m.problemsOnly = !m.problemsOnly
		m.rebuildRows()
	case "g":
		if m.groupMode == groupByCategory {
			m.groupMode = groupByStatus
		} else {
			m.groupMode = groupByCategory
		}
		m.collapsed = map[string]bool{}
		m.rebuildRows()
	}
	return m, nil
}

func (m model) cursorGroup() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	return m.rows[m.cursor].group
}

func (m *model) landCursorOnGroup(group string) {
	for i, r := range m.rows {
		if r.kind == rowGroup && r.group == group {
			m.cursor = i
			return
		}
	}
}

func (m model) View() tea.View {
	v := tea.NewView(m.renderContent())
	v.AltScreen = true
	return v
}

func (m model) renderContent() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.showHelp {
		return m.renderHelpOverlay()
	}

	hint := m.renderHint(m.width)
	summary := m.renderSummary(m.width)

	// Reserve 1 line for hint, 1 for summary, 2 for the pane's own top/bottom border.
	viewportHeight := m.height - 4
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	start, end := visibleWindow(len(m.rows), m.cursor, viewportHeight)

	var lines []string
	for i := start; i < end; i++ {
		lines = append(lines, m.renderRow(i))
	}

	// Problems-only with nothing wrong would render an empty pane; say so instead.
	if len(lines) == 0 && m.problemsOnly && m.filter.Value() == "" {
		lines = []string{
			"",
			m.palette.Inactive.Render(
				fmt.Sprintf(
					"  ✓ no problems — all %d tracked items are present · press p to show everything",
					len(m.items),
				),
			),
		}
	}

	title := m.title
	if m.problemsOnly {
		title += " · problems only"
	}

	return m.palette.BorderedPane(title, m.width, lines) + "\n" + summary + "\n" + hint
}

func (m model) renderHelpOverlay() string {
	entries := []tuicomponents.WhichKeyEntry{
		{Key: "j / k", Desc: "move down / up"},
		{Key: "h / l", Desc: "collapse / expand group"},
		{Key: "g", Desc: "group by category / by status"},
		{Key: "p", Desc: "toggle problems only (MISSING/UNKNOWN)"},
		{Key: "/", Desc: "filter by name  esc:clear  enter:keep"},
		{Key: "?", Desc: "toggle this help"},
		{Key: "q / ctrl+c", Desc: "quit"},
	}
	return m.palette.HelpOverlay("Keybindings", entries, m.width, m.height)
}

// visibleWindow returns [start, end) into a rowsLen-length list such that the
// window has at most viewportHeight rows and always contains cursor.
func visibleWindow(rowsLen, cursor, viewportHeight int) (start, end int) {
	if rowsLen <= viewportHeight {
		return 0, rowsLen
	}
	start = cursor - viewportHeight/2
	if start < 0 {
		start = 0
	}
	end = start + viewportHeight
	if end > rowsLen {
		end = rowsLen
		start = end - viewportHeight
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func (m model) renderRow(i int) string {
	r := m.rows[i]
	innerWidth := m.width - 2
	if r.kind == rowGroup {
		collapse := "▾"
		if m.collapsed[r.group] {
			collapse = "▸"
		}
		text := collapse + " " + r.group
		count := fmt.Sprintf("%d", r.count)
		pad := innerWidth - ansi.StringWidth(text) - ansi.StringWidth(count)
		if pad < 1 {
			pad = 1
		}
		plain := text + strings.Repeat(" ", pad) + count
		if i == m.cursor {
			return m.palette.Selected.Render(plain)
		}
		return m.palette.RepoHeader.Render(
			text,
		) + strings.Repeat(
			" ",
			pad,
		) + m.palette.SectionHead.Render(
			count,
		)
	}

	glyph := statusGlyph(r.item.State)
	name := "  " + glyph + " " + r.item.Name
	if i == m.cursor {
		plain := name
		if r.item.Source == "pre-existing" {
			plain += " (pre-existing)"
		}
		return m.palette.Selected.Render(plain)
	}
	line := "  " + statusDot(
		m.palette,
		r.item.State,
	) + " " + r.item.Name + sourceTag(
		m.palette,
		r.item.Source,
	)
	return line
}

func (m model) renderSummary(width int) string {
	categories := map[string]bool{}
	missing := 0
	for _, it := range m.items {
		categories[it.Category] = true
		if it.State == inventory.StateMissing {
			missing++
		}
	}
	text := fmt.Sprintf(
		"%d CATEGORIES · %d ITEMS · %d MISSING",
		len(categories),
		len(m.items),
		missing,
	)
	return m.palette.SectionHead.Render(ansi.Truncate(text, width, ""))
}

func (m model) renderHint(width int) string {
	if m.filter.Active {
		return m.palette.FilterHint(m.filter, width)
	}
	problemsDesc := "problems"
	if m.problemsOnly {
		problemsDesc = "show all"
	}
	hints := []tuicomponents.KeyHint{
		{Key: "j/k", Desc: "move"},
		{Key: "h/l", Desc: "collapse/expand"},
		{Key: "/", Desc: "filter"},
		{Key: "p", Desc: problemsDesc},
		{Key: "g", Desc: "group"},
		{Key: "?", Desc: "help"},
		{Key: "q", Desc: "quit"},
	}
	return m.palette.HintBar(hints, width)
}
