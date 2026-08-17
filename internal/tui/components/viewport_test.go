package tuicomponents

import "testing"

func TestVisibleWindow(t *testing.T) {
	t.Run("rows fewer than height shows everything", func(t *testing.T) {
		start, end := VisibleWindow(5, 2, 10)
		if start != 0 || end != 5 {
			t.Errorf("expected [0, 5), got [%d, %d)", start, end)
		}
	})

	t.Run("rows equal to height shows everything", func(t *testing.T) {
		start, end := VisibleWindow(10, 3, 10)
		if start != 0 || end != 10 {
			t.Errorf("expected [0, 10), got [%d, %d)", start, end)
		}
	})

	t.Run("cursor at 0 clamps window to the start", func(t *testing.T) {
		start, end := VisibleWindow(100, 0, 10)
		if start != 0 || end != 10 {
			t.Errorf("expected [0, 10), got [%d, %d)", start, end)
		}
	})

	t.Run("cursor at the end clamps window to the end", func(t *testing.T) {
		start, end := VisibleWindow(100, 99, 10)
		if start != 90 || end != 100 {
			t.Errorf("expected [90, 100), got [%d, %d)", start, end)
		}
		if end-start != 10 {
			t.Errorf("expected window of size 10, got %d", end-start)
		}
	})

	t.Run("cursor in the middle centers the window", func(t *testing.T) {
		start, end := VisibleWindow(100, 50, 10)
		if end-start != 10 {
			t.Errorf("expected window of size 10, got %d", end-start)
		}
		if start > 50 || end <= 50 {
			t.Errorf("expected window [%d, %d) to contain cursor 50", start, end)
		}
	})

	t.Run("window always contains the cursor near the start", func(t *testing.T) {
		start, end := VisibleWindow(100, 3, 10)
		if start > 3 || end <= 3 {
			t.Errorf("expected window [%d, %d) to contain cursor 3", start, end)
		}
	})

	t.Run("empty rows returns an empty window", func(t *testing.T) {
		start, end := VisibleWindow(0, 0, 10)
		if start != 0 || end != 0 {
			t.Errorf("expected [0, 0), got [%d, %d)", start, end)
		}
	})

	t.Run("zero height returns an empty window when rows overflow", func(t *testing.T) {
		start, end := VisibleWindow(10, 5, 0)
		if end-start != 0 {
			t.Errorf("expected an empty window, got [%d, %d)", start, end)
		}
	})
}
