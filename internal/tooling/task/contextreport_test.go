package task

import (
	"strings"
	"testing"
)

func TestContextReport_RendersBothAgents(t *testing.T) {
	tm, _, _ := newRepoSetup(t, "feat")
	out, err := tm.ContextReport()
	if err != nil {
		t.Fatalf("ContextReport: %v", err)
	}
	if !strings.Contains(out, "claude base context") {
		t.Errorf("expected the claude section, got:\n%s", out)
	}
	if !strings.Contains(out, "opencode base context") {
		t.Errorf("expected the opencode section, got:\n%s", out)
	}
	if !strings.Contains(out, "estimate") {
		t.Errorf("expected the token estimate label, got:\n%s", out)
	}
}
