package buildinfo_test

import (
	"testing"

	"github.com/cjairm/devgeta/pkg/buildinfo"
)

// TestDefaults confirms the zero values a plain `go build` (no ldflags)
// produces. cmd.resolveVersionInfo relies on these exact sentinels to decide
// whether to fall back to runtime/debug.BuildInfo.
func TestDefaults(t *testing.T) {
	if buildinfo.Version != "dev" {
		t.Errorf("Version: want %q, got %q", "dev", buildinfo.Version)
	}
	if buildinfo.Commit != "unknown" {
		t.Errorf("Commit: want %q, got %q", "unknown", buildinfo.Commit)
	}
	if buildinfo.BuildDate != "unknown" {
		t.Errorf("BuildDate: want %q, got %q", "unknown", buildinfo.BuildDate)
	}
}
