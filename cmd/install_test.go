package cmd

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

// --- helpers ---

func assertRunsOnly(t *testing.T, cfg *installConfig, categories ...string) {
	t.Helper()
	want := make(map[string]bool, len(categories))
	for _, c := range categories {
		want[c] = true
	}
	got := map[string]bool{
		"terminal":  cfg.runTerminal,
		"languages": cfg.runLanguages,
		"databases": cfg.runDatabases,
		"desktop":   cfg.runDesktop,
		"ai-tools":  cfg.runAITools,
	}
	for cat, shouldRun := range got {
		if shouldRun != want[cat] {
			if shouldRun {
				t.Errorf("expected category %q NOT to run", cat)
			} else {
				t.Errorf("expected category %q to run", cat)
			}
		}
	}
}

// --- category-level tests (existing behavior) ---

func TestParseFlags_NoFlags_RunsAll(t *testing.T) {
	cfg, err := parseInstallFlags(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal", "languages", "databases", "desktop", "ai-tools")
	if cfg.terminalAppFilter != nil {
		t.Error("expected terminalAppFilter nil with no flags")
	}
	if cfg.desktopAppFilter != nil {
		t.Error("expected desktopAppFilter nil with no flags")
	}
	if cfg.aiToolsAppFilter != nil {
		t.Error("expected aiToolsAppFilter nil with no flags")
	}
}

func TestParseFlags_OnlyTerminal(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"terminal"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal")
	if cfg.terminalAppFilter != nil {
		t.Error("expected terminalAppFilter nil for category-only flag")
	}
}

func TestParseFlags_SkipDatabases(t *testing.T) {
	cfg, err := parseInstallFlags(nil, []string{"databases"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal", "languages", "desktop", "ai-tools")
}

func TestParseFlags_OnlyDesktop(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"desktop"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "desktop")
	if cfg.desktopAppFilter != nil {
		t.Error("expected desktopAppFilter nil for category-only flag")
	}
}

// --- app-level targeting ---

func TestParseFlags_OnlyNeovim_RunsTerminal(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"neovim"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal")
	if !cfg.terminalAppFilter["neovim"] {
		t.Error("expected terminalAppFilter to include neovim")
	}
	if len(cfg.terminalAppFilter) != 1 {
		t.Errorf("expected terminalAppFilter length 1, got %d", len(cfg.terminalAppFilter))
	}
}

func TestParseFlags_OnlyDocker_RunsDesktop(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"docker"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "desktop")
	if !cfg.desktopAppFilter["docker"] {
		t.Error("expected desktopAppFilter to include docker")
	}
}

func TestParseFlags_OnlyAlacritty_RunsDesktop(t *testing.T) {
	// alacritty is KindTerminal but installed by desktop coordinator
	cfg, err := parseInstallFlags([]string{"alacritty"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "desktop")
	if !cfg.desktopAppFilter["alacritty"] {
		t.Error("expected desktopAppFilter to include alacritty")
	}
}

func TestParseFlags_SkipGit_TerminalRunsWithSkipFilter(t *testing.T) {
	cfg, err := parseInstallFlags(nil, []string{"git"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal", "languages", "databases", "desktop", "ai-tools")
	if cfg.terminalAppFilter != nil {
		t.Error("expected terminalAppFilter nil when only skip flags given")
	}
	if !cfg.terminalSkipFilter["git"] {
		t.Error("expected terminalSkipFilter to include git")
	}
}

func TestParseFlags_OnlyAITools(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"ai-tools"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "ai-tools")
	if cfg.aiToolsAppFilter != nil {
		t.Error("expected aiToolsAppFilter nil for category-only flag")
	}
}

func TestParseFlags_OnlyRtk_RunsAITools(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"rtk"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "ai-tools")
	if !cfg.aiToolsAppFilter["rtk"] {
		t.Error("expected aiToolsAppFilter to include rtk")
	}
	if len(cfg.aiToolsAppFilter) != 1 {
		t.Errorf("expected aiToolsAppFilter length 1, got %d", len(cfg.aiToolsAppFilter))
	}
}

func TestParseFlags_SkipAITools(t *testing.T) {
	cfg, err := parseInstallFlags(nil, []string{"ai-tools"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal", "languages", "databases", "desktop")
}

// --- mixed category + app ---

func TestParseFlags_OnlyTerminalSkipNeovim(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"terminal"}, []string{"neovim"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal")
	// Category selected → full install (no appFilter), but neovim in skipFilter
	if cfg.terminalAppFilter != nil {
		t.Error("expected terminalAppFilter nil when category is in only-set")
	}
	if !cfg.terminalSkipFilter["neovim"] {
		t.Error("expected terminalSkipFilter to include neovim")
	}
}

func TestParseFlags_OnlyNeovimAndDocker(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"neovim", "docker"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal", "desktop")
	if !cfg.terminalAppFilter["neovim"] {
		t.Error("expected terminalAppFilter to include neovim")
	}
	if !cfg.desktopAppFilter["docker"] {
		t.Error("expected desktopAppFilter to include docker")
	}
}

func TestParseFlags_OnlyNeovimSkipNeovim(t *testing.T) {
	// Skip wins over only: neovim in both sets → filtered out of appFilter
	cfg, err := parseInstallFlags([]string{"neovim"}, []string{"neovim"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// terminal would "run" but appFilter is empty because neovim was excluded from it
	// hasAppsForCoordinator checks onlyAppSet (before skip), so terminal still runs
	if cfg.terminalAppFilter == nil && cfg.runTerminal {
		// appFilter is nil or empty - neovim was filtered out
		if cfg.terminalAppFilter != nil && cfg.terminalAppFilter["neovim"] {
			t.Error("expected neovim NOT in terminalAppFilter when also in skip")
		}
	}
}

// --- validation ---

func TestParseFlags_UnknownOnly_ReturnsError(t *testing.T) {
	_, err := parseInstallFlags([]string{"bogus"}, nil)
	if err == nil {
		t.Fatal("expected error for unknown --only value")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected error to mention %q, got: %v", "bogus", err)
	}
	if !strings.Contains(err.Error(), "Valid categories") {
		t.Errorf("expected error to list valid categories, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Valid apps") {
		t.Errorf("expected error to list valid apps, got: %v", err)
	}
}

func TestParseFlags_UnknownSkip_ReturnsError(t *testing.T) {
	_, err := parseInstallFlags(nil, []string{"notanapp"})
	if err == nil {
		t.Fatal("expected error for unknown --skip value")
	}
	if !strings.Contains(err.Error(), "notanapp") {
		t.Errorf("expected error to mention %q, got: %v", "notanapp", err)
	}
}

func TestParseFlags_Devgeta_IsNotTargetable(t *testing.T) {
	// devgeta maps to "" coordinator; it's not a valid --only or --skip target
	_, err := parseInstallFlags([]string{"devgeta"}, nil)
	if err == nil {
		t.Fatal("expected error: devgeta should not be targetable via --only")
	}
}

func TestParseFlags_AllKnownApps_Valid(t *testing.T) {
	for appName, coord := range appToCoordinator {
		if coord == "" {
			continue // devgeta not targetable
		}
		t.Run(appName, func(t *testing.T) {
			cfg, err := parseInstallFlags([]string{appName}, nil)
			if err != nil {
				t.Fatalf("--only %q returned unexpected error: %v", appName, err)
			}
			if cfg == nil {
				t.Fatalf("--only %q returned nil config", appName)
			}
		})
	}
}
