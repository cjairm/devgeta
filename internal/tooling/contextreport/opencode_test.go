package contextreport

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeMemoryRowCountsBothLayersAndFlagsTheAmbiguity(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "opencode", "AGENTS.md"), "global agents\n")
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "project agents\n")

	row := openCodeMemoryRow(repo, home)
	if len(row.Items) != 2 {
		t.Fatalf("items = %+v, want 2", row.Items)
	}
	if !strings.Contains(row.Note, "22020") {
		t.Errorf("expected the note to flag the upstream ambiguity, got: %q", row.Note)
	}
}

func TestOpenCodeMemoryRowSingleLayerHasNoAmbiguityNote(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "project agents only\n")

	row := openCodeMemoryRow(repo, home)
	if len(row.Items) != 1 {
		t.Fatalf("items = %+v, want 1", row.Items)
	}
	if strings.Contains(row.Note, "22020") {
		t.Errorf("expected no ambiguity note with only one layer present, got: %q", row.Note)
	}
}

func TestOpenCodeSettingsRowCountsWhicheverLayersExist(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"a":1}`)
	// No project opencode.json.

	row := openCodeSettingsRow(repo, home)
	if len(row.Items) != 1 {
		t.Fatalf("items = %+v, want 1 (global only)", row.Items)
	}
}

func TestOpenCodePluginsRowIsInformationalOnly(t *testing.T) {
	home := t.TempDir()
	writeFile(
		t,
		filepath.Join(home, ".config", "opencode", "plugin", "foo.js"),
		"export const Foo = () => ({});\n",
	)

	row := openCodePluginsRow(home)
	if row.TotalBytes() != 0 {
		t.Errorf(
			"TotalBytes = %d, want 0 (informational only, not added to the total)",
			row.TotalBytes(),
		)
	}
	if !strings.Contains(row.Note, "informational") {
		t.Errorf("expected the note to say informational, got: %q", row.Note)
	}
}

func TestDiscoverOpenCodeReturnsEveryRow(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()

	report := DiscoverOpenCode(repo, home)
	if report.Agent != "opencode" {
		t.Errorf("Agent = %q, want opencode", report.Agent)
	}
	for _, want := range []string{"Memory", "Settings", "Plugins"} {
		findRow(t, report.Rows, want)
	}
}
