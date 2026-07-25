package tuicomponents

import (
	"strings"
	"testing"
)

func testItems() []PaletteItem {
	return []PaletteItem{
		{Command: "gitmoji", Hint: "~/code/gitmoji"},
		{Command: "worktrunk", Hint: "~/code/worktrunk"},
		{Command: "gitignore-tool", Hint: "~/code/gitignore-tool"},
	}
}

func TestFuzzyPickerFiltering(t *testing.T) {
	t.Run("empty query keeps all items in original order", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		if len(p.filtered) != 3 {
			t.Fatalf("expected 3 filtered items, got %d", len(p.filtered))
		}
		if p.filtered[0] != 0 || p.filtered[1] != 1 || p.filtered[2] != 2 {
			t.Errorf("expected original order, got %v", p.filtered)
		}
	})

	t.Run("substring query ranks gitmoji and gitignore-tool above worktrunk", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		for _, k := range []string{"g", "i", "t"} {
			p.HandleKey(k)
		}
		if len(p.filtered) != 2 {
			t.Fatalf(
				"expected 2 matches for %q, got %d: %v",
				p.Query(),
				len(p.filtered),
				p.filtered,
			)
		}
		item, ok := p.Selected()
		if !ok {
			t.Fatal("expected a selected item")
		}
		if item.Command != "gitmoji" && item.Command != "gitignore-tool" {
			t.Errorf("unexpected top match %q", item.Command)
		}
		if p.cursor != 0 {
			t.Errorf("expected cursor reset to 0, got %d", p.cursor)
		}
	})

	t.Run("exact prefix outranks substring", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", []PaletteItem{
			{Command: "worktrunk"},
			{Command: "trunk-tool"},
		})
		for _, r := range []string{"t", "r", "u", "n"} {
			p.HandleKey(r)
		}
		item, ok := p.Selected()
		if !ok {
			t.Fatal("expected a selected item")
		}
		if item.Command != "trunk-tool" {
			t.Errorf("expected exact-prefix match trunk-tool ranked first, got %q", item.Command)
		}
	})

	t.Run("non-matching query empties the filtered list", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		for _, k := range []string{"z", "z", "z"} {
			p.HandleKey(k)
		}
		if len(p.filtered) != 0 {
			t.Fatalf("expected no matches, got %v", p.filtered)
		}
		if _, ok := p.Selected(); ok {
			t.Error("expected no selection when filtered list is empty")
		}
	})

	t.Run("backspace widens the filtered list back", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.HandleKey("g")
		p.HandleKey("i")
		p.HandleKey("t")
		narrowed := len(p.filtered)
		p.HandleKey("backspace")
		p.HandleKey("backspace")
		p.HandleKey("backspace")
		if p.Query() != "" {
			t.Fatalf("expected empty query, got %q", p.Query())
		}
		if len(p.filtered) != 3 {
			t.Fatalf(
				"expected all 3 items back, got %d (was %d narrowed)",
				len(p.filtered),
				narrowed,
			)
		}
	})

	t.Run("backspace on empty query is a no-op", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.HandleKey("backspace")
		if p.Query() != "" || len(p.filtered) != 3 {
			t.Error("expected no change from backspace on empty query")
		}
	})
}

func TestFuzzyPickerNavigation(t *testing.T) {
	t.Run("down and ctrl+j move the cursor forward", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.HandleKey("down")
		if p.cursor != 1 {
			t.Fatalf("expected cursor 1, got %d", p.cursor)
		}
		p.HandleKey("ctrl+j")
		if p.cursor != 2 {
			t.Fatalf("expected cursor 2, got %d", p.cursor)
		}
	})

	t.Run("up and ctrl+k move the cursor backward", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.HandleKey("down")
		p.HandleKey("down")
		p.HandleKey("up")
		if p.cursor != 1 {
			t.Fatalf("expected cursor 1, got %d", p.cursor)
		}
		p.HandleKey("ctrl+k")
		if p.cursor != 0 {
			t.Fatalf("expected cursor 0, got %d", p.cursor)
		}
	})

	t.Run("cursor wraps past the end and start", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.HandleKey("up")
		if p.cursor != 2 {
			t.Fatalf("expected wrap to last item (2), got %d", p.cursor)
		}
		p.HandleKey("down")
		if p.cursor != 0 {
			t.Fatalf("expected wrap to first item (0), got %d", p.cursor)
		}
	})

	t.Run("bare j and k are appended to the query, not treated as nav", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.HandleKey("j")
		p.HandleKey("k")
		if p.Query() != "jk" {
			t.Errorf("expected j/k to edit the query, got query %q", p.Query())
		}
	})

	t.Run("typing k narrows the filtered list to matching names", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", []PaletteItem{
			{Command: "kubernetes"},
			{Command: "devgeta"},
			{Command: "postgres"},
		})
		p.HandleKey("k")
		if len(p.filtered) != 1 {
			t.Fatalf(
				"expected 1 match for query %q, got %d: %v",
				p.Query(),
				len(p.filtered),
				p.filtered,
			)
		}
		item, ok := p.Selected()
		if !ok || item.Command != "kubernetes" {
			t.Errorf("expected kubernetes selected, got %+v (ok=%v)", item, ok)
		}
	})

	t.Run("typing j narrows the filtered list to matching names", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", []PaletteItem{
			{Command: "jira-sync"},
			{Command: "devgeta"},
			{Command: "worktrunk"},
		})
		p.HandleKey("j")
		if len(p.filtered) != 1 {
			t.Fatalf(
				"expected 1 match for query %q, got %d: %v",
				p.Query(),
				len(p.filtered),
				p.filtered,
			)
		}
		item, ok := p.Selected()
		if !ok || item.Command != "jira-sync" {
			t.Errorf("expected jira-sync selected, got %+v (ok=%v)", item, ok)
		}
	})
}

func TestFuzzyPickerSelectAndCancel(t *testing.T) {
	t.Run("enter reports the selected item", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.HandleKey("down")
		result := p.HandleKey("enter")
		if result.Action != FuzzyPickerSelected {
			t.Fatalf("expected FuzzyPickerSelected, got %v", result.Action)
		}
		if result.Item.Command != "worktrunk" {
			t.Errorf("expected worktrunk selected, got %q", result.Item.Command)
		}
	})

	t.Run("enter on an empty filtered list reports no action", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		for _, k := range []string{"z", "z", "z"} {
			p.HandleKey(k)
		}
		result := p.HandleKey("enter")
		if result.Action != FuzzyPickerNone {
			t.Errorf("expected FuzzyPickerNone, got %v", result.Action)
		}
	})

	t.Run("esc reports cancellation", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		result := p.HandleKey("esc")
		if result.Action != FuzzyPickerCancelled {
			t.Fatalf("expected FuzzyPickerCancelled, got %v", result.Action)
		}
	})
}

func TestFuzzyPickerSetItems(t *testing.T) {
	p := NewFuzzyPicker("Repo", testItems())
	p.HandleKey("g")
	if len(p.filtered) != 2 {
		t.Fatalf("expected 2 matches before SetItems, got %d", len(p.filtered))
	}
	p.SetItems([]PaletteItem{{Command: "alpha"}, {Command: "beta"}})
	if len(p.items) != 2 {
		t.Fatalf("expected 2 items after SetItems, got %d", len(p.items))
	}
	// query "g" no longer matches either replacement item.
	if len(p.filtered) != 0 {
		t.Errorf("expected re-filter against new items, got %v", p.filtered)
	}
}

func TestFuzzyPickerView(t *testing.T) {
	widths := []int{0, 1, 5, 6, 20, 40, 80}
	for _, w := range widths {
		p := NewFuzzyPicker("Pick a repo", testItems())
		p.HandleKey("g")
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("View(%d) panicked: %v", w, r)
				}
			}()
			_ = p.View(w)
		}()
	}
}

func TestFuzzyPickerViewRendersEmptyState(t *testing.T) {
	p := NewFuzzyPicker("Pick a repo", testItems())
	for _, k := range []string{"z", "z", "z"} {
		p.HandleKey(k)
	}
	out := p.View(40)
	if out == "" {
		t.Error("expected non-empty view even with no matches")
	}
}

// Regression test for the View width-guard bug: View computed the width it
// handed to CommandPalette independently of BorderedPane's own internal
// clamp (floor outer width to 6, then subtract 2 for the border), so the two
// could disagree about how much interior space actually exists.
//
// CommandPalette itself refuses to render anything below width 6, and
// BorderedPane's border consumes 2 columns, so content can only ever appear
// once the outer width reaches 8 — narrower widths (including 6 and 7) are
// always a blank interior by CommandPalette's own contract, independent of
// this fix. What this test guards is the boundary itself: at the smallest
// width where content CAN legitimately appear (8), it must actually appear,
// and View must not panic or silently blank the box at any narrower width.
func TestFuzzyPickerViewNarrowWidthDoesNotDropContent(t *testing.T) {
	// No Hint, and a Command short enough to survive truncation at the
	// smallest renderable width (6 columns of interior), so a failure here
	// can only be the width-guard bug, not truncation of a long label.
	items := []PaletteItem{{Command: "git"}}
	p := NewFuzzyPicker("Repo", items)
	p.HandleKey("g")
	if len(p.filtered) != 1 {
		t.Fatalf("expected 1 match, got %d", len(p.filtered))
	}

	t.Run("widths below the render floor never panic", func(t *testing.T) {
		for _, w := range []int{0, 1, 5, 6, 7} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("View(%d) panicked: %v", w, r)
					}
				}()
				_ = p.View(w)
			}()
		}
	})

	t.Run("width 8 is the render floor and shows the match", func(t *testing.T) {
		out := p.View(8)
		if !strings.Contains(out, "git") {
			t.Errorf("expected View(8) to contain the matching item text, got:\n%s", out)
		}
	})

	t.Run("wider widths keep showing the match", func(t *testing.T) {
		for _, w := range []int{20, 40, 80} {
			out := p.View(w)
			if !strings.Contains(out, "git") {
				t.Errorf("expected View(%d) to contain the matching item text, got:\n%s", w, out)
			}
		}
	})
}

// Regression test for the HandleKey byte-length bug: the query-append guard
// used len(key) == 1, which counts bytes, not runes. A non-ASCII rune such as
// "é" or "日" arrives as a multi-byte UTF-8 string, so it was silently
// dropped instead of being appended to the query.
func TestFuzzyPickerHandleKeyAcceptsUnicodeRunes(t *testing.T) {
	t.Run("a single non-ASCII rune is appended to the query", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.HandleKey("é")
		if p.Query() != "é" {
			t.Fatalf("expected query %q, got %q", "é", p.Query())
		}
	})

	t.Run("a multi-byte CJK rune is appended to the query", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.HandleKey("日")
		if p.Query() != "日" {
			t.Fatalf("expected query %q, got %q", "日", p.Query())
		}
	})

	t.Run("a unicode query filters down to the matching item", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", []PaletteItem{
			{Command: "café-tool"},
			{Command: "devgeta"},
		})
		p.HandleKey("é")
		if len(p.filtered) != 1 {
			t.Fatalf(
				"expected 1 match for query %q, got %d: %v",
				p.Query(),
				len(p.filtered),
				p.filtered,
			)
		}
		item, ok := p.Selected()
		if !ok || item.Command != "café-tool" {
			t.Errorf("expected café-tool selected, got %+v (ok=%v)", item, ok)
		}
	})
}

func TestFuzzyPickerInsertText(t *testing.T) {
	t.Run("inserts pasted text in one shot and refilters", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.InsertText("git")
		if p.Query() != "git" {
			t.Fatalf("expected query %q, got %q", "git", p.Query())
		}
		if len(p.filtered) != 2 {
			t.Fatalf("expected refiltered to 2 matches, got %d: %v", len(p.filtered), p.filtered)
		}
	})

	t.Run("strips control characters from the pasted content", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.InsertText("dev\ngeta\r")
		if p.Query() != "devgeta" {
			t.Fatalf("expected control chars stripped, got %q", p.Query())
		}
	})

	t.Run("all-control paste is a no-op", func(t *testing.T) {
		p := NewFuzzyPicker("Repo", testItems())
		p.InsertText("\n\r")
		if p.Query() != "" {
			t.Fatalf("expected empty query, got %q", p.Query())
		}
	})
}
