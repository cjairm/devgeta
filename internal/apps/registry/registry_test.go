package registry

import (
	"sort"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

var expectedApps = []string{
	"aerospace", "alacritty", "brave", "claude", "devgeta", "docker",
	"fastfetch", "flameshot", "gimp", "git", "i3", "lazydocker",
	"lazygit", "mise", "neovim", "opencode", "raycast", "rtk", "tmux", "ulauncher",
}

func TestGetApp_KnownApp(t *testing.T) {
	app, err := GetApp("git")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil app")
	}
	if app.Name() != "git" {
		t.Errorf("expected Name() == %q, got %q", "git", app.Name())
	}
}

func TestGetApp_UnknownApp(t *testing.T) {
	_, err := GetApp("notanapp")
	if err == nil {
		t.Fatal("expected error for unknown app, got nil")
	}
}

func TestGetApp_AllRegisteredApps(t *testing.T) {
	for _, name := range expectedApps {
		t.Run(name, func(t *testing.T) {
			app, err := GetApp(name)
			if err != nil {
				t.Fatalf("GetApp(%q) returned error: %v", name, err)
			}
			if app == nil {
				t.Fatalf("GetApp(%q) returned nil app", name)
			}
			if app.Name() != name {
				t.Errorf("GetApp(%q).Name() = %q, want %q", name, app.Name(), name)
			}
		})
	}
}

func TestGetAppsByKind_Terminal(t *testing.T) {
	names := GetAppsByKind(apps.KindTerminal)
	expected := []string{
		"alacritty",
		"claude",
		"fastfetch",
		"git",
		"lazydocker",
		"lazygit",
		"mise",
		"neovim",
		"opencode",
		"rtk",
		"tmux",
	}
	if len(names) != len(expected) {
		t.Errorf(
			"GetAppsByKind(KindTerminal) returned %d names, want %d: %v",
			len(names),
			len(expected),
			names,
		)
	}
	if !sort.StringsAreSorted(names) {
		t.Error("GetAppsByKind(KindTerminal) is not sorted")
	}
	got := make(map[string]bool, len(names))
	for _, n := range names {
		got[n] = true
	}
	for _, name := range expected {
		if !got[name] {
			t.Errorf("GetAppsByKind(KindTerminal) missing %q", name)
		}
	}
}

func TestGetAppsByKind_Desktop(t *testing.T) {
	names := GetAppsByKind(apps.KindDesktop)
	expected := []string{
		"aerospace",
		"brave",
		"docker",
		"flameshot",
		"gimp",
		"i3",
		"raycast",
		"ulauncher",
	}
	if len(names) != len(expected) {
		t.Errorf(
			"GetAppsByKind(KindDesktop) returned %d names, want %d: %v",
			len(names),
			len(expected),
			names,
		)
	}
	got := make(map[string]bool, len(names))
	for _, n := range names {
		got[n] = true
	}
	for _, name := range expected {
		if !got[name] {
			t.Errorf("GetAppsByKind(KindDesktop) missing %q", name)
		}
	}
}

func TestGetAppsByKind_NoMeta(t *testing.T) {
	terminal := GetAppsByKind(apps.KindTerminal)
	desktop := GetAppsByKind(apps.KindDesktop)
	all := append(terminal, desktop...)
	for _, name := range all {
		if name == "devgeta" {
			t.Errorf("KindMeta app %q must not appear in terminal or desktop results", name)
		}
	}
}

func TestMeta_ConsistencyWithFactories(t *testing.T) {
	for name, meta := range Meta {
		if meta.Coordinator == "" {
			continue // devgeta sentinel — no factory requirement
		}
		if _, ok := factories[name]; !ok {
			t.Errorf(
				"Meta entry %q has Coordinator %q but no factory entry",
				name,
				meta.Coordinator,
			)
		}
	}
	for name := range factories {
		if _, ok := Meta[name]; !ok {
			t.Errorf("factory entry %q has no Meta entry", name)
		}
	}
}

func TestIsKnownApp(t *testing.T) {
	if !IsKnownApp("git") {
		t.Error("IsKnownApp(git) = false, want true")
	}
	if IsKnownApp("devgeta") {
		t.Error("IsKnownApp(devgeta) = true, want false")
	}
	if IsKnownApp("notanapp") {
		t.Error("IsKnownApp(notanapp) = true, want false")
	}
}

func TestIsKnownCategory(t *testing.T) {
	if !IsKnownCategory("terminal") {
		t.Error("IsKnownCategory(terminal) = false, want true")
	}
	if !IsKnownCategory("desktop") {
		t.Error("IsKnownCategory(desktop) = false, want true")
	}
	if IsKnownCategory("languages") {
		t.Error("IsKnownCategory(languages) = true, want false")
	}
}

func TestAppsByCoordinator(t *testing.T) {
	terminal := AppsByCoordinator("terminal")
	if len(terminal) == 0 {
		t.Error("AppsByCoordinator(terminal) returned empty slice")
	}
	for _, name := range terminal {
		if Meta[name].Coordinator != "terminal" {
			t.Errorf(
				"AppsByCoordinator(terminal) includes %q which has coordinator %q",
				name,
				Meta[name].Coordinator,
			)
		}
	}
	if !sort.StringsAreSorted(terminal) {
		t.Error("AppsByCoordinator(terminal) is not sorted")
	}
}

func TestNames_ContainsAllApps(t *testing.T) {
	names := Names()
	if len(names) != len(expectedApps) {
		t.Errorf("Names() returned %d names, want %d", len(names), len(expectedApps))
	}
	if !sort.StringsAreSorted(names) {
		t.Error("Names() is not sorted")
	}
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range expectedApps {
		if !nameSet[expected] {
			t.Errorf("Names() missing %q", expected)
		}
	}
}
