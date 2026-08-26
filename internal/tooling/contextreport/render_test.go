package contextreport

import (
	"strings"
	"testing"
)

func TestRenderIncludesAgentRowsAndEstimateLabel(t *testing.T) {
	r := &Report{
		Agent: "claude",
		Rows: []Row{
			{
				Layer: "Memory (CLAUDE.md)",
				Items: []Item{{Path: "/a/CLAUDE.md", Bytes: 100}},
				Note:  "concatenated",
			},
			{Layer: "Plugins (informational)", Note: "informational only, not counted"},
		},
	}
	out := r.Render()

	if !strings.Contains(out, "claude") {
		t.Errorf("expected the agent name in the output, got:\n%s", out)
	}
	if !strings.Contains(out, "Memory (CLAUDE.md)") {
		t.Errorf("expected the row layer name, got:\n%s", out)
	}
	if !strings.Contains(out, "100") {
		t.Errorf("expected the row's byte count, got:\n%s", out)
	}
	if !strings.Contains(out, "concatenated") {
		t.Errorf("expected the row's note, got:\n%s", out)
	}
	if !strings.Contains(out, "estimate") {
		t.Errorf("expected the token figure to be explicitly labeled an estimate, got:\n%s", out)
	}
}

func TestRenderWithNoItemsStillShowsTheRow(t *testing.T) {
	r := &Report{
		Agent: "opencode",
		Rows:  []Row{{Layer: "Settings (opencode.json)", Note: "none found"}},
	}
	out := r.Render()
	if !strings.Contains(out, "Settings (opencode.json)") {
		t.Errorf("expected the empty row's layer name, got:\n%s", out)
	}
	if !strings.Contains(out, "none found") {
		t.Errorf("expected the empty row's note, got:\n%s", out)
	}
}
