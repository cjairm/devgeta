package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() { testutil.InitLogger() }

// isolateInstallRoot points paths.Paths.Home.PiInstall - the managed install
// root this app keys its detection, uninstall and settings lookup on - into a
// throwaway directory, and restores it via t.Cleanup.
func isolateInstallRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".pi", "agent", "install")
	old := paths.Paths.Home.PiInstall
	t.Cleanup(func() { paths.Paths.Home.PiInstall = old })
	paths.Paths.Home.PiInstall = root
	return root
}

// stubLookPath swaps the package-level PATH-lookup seam every app uses for
// binary detection (commands.LookPathFn), so a test decides whether pi
// resolves on PATH instead of inheriting whatever the machine has installed.
func stubLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := commands.LookPathFn
	t.Cleanup(func() { commands.LookPathFn = orig })
	commands.LookPathFn = fn
}

func piOnPath(t *testing.T) {
	t.Helper()
	stubLookPath(t, func(name string) (string, error) { return "/usr/local/bin/" + name, nil })
}

func piNotOnPath(t *testing.T) {
	t.Helper()
	stubLookPath(t, func(string) (string, error) { return "", fmt.Errorf("not found") })
}

// writeManagedMarker creates the marker file the official install script
// would leave inside the managed install root.
func writeManagedMarker(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "managed-install.json"),
		[]byte(`{}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

// writeAgentSettings writes Pi's settings.json into the agent home - the parent
// of the managed install root - matching the file `pi install` maintains.
func writeAgentSettings(t *testing.T, settingsJSON string) {
	t.Helper()
	dir := filepath.Dir(paths.Paths.Home.PiInstall)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "settings.json"),
		[]byte(settingsJSON),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

// piInstallSources returns the source argument of every `pi install <source>`
// call the mock recorded, in call order.
func piInstallSources(mockApp *testutil.MockApp) []string {
	var sources []string
	for _, call := range mockApp.Base.ExecCommandCalls {
		if filepath.Base(call.Command) == "pi" && len(call.Args) == 2 && call.Args[0] == "install" {
			sources = append(sources, call.Args[1])
		}
	}
	return sources
}

func newApp() (*Pi, *testutil.MockApp) {
	mockApp := testutil.NewMockApp()
	return &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}, mockApp
}

func TestSoftInstall(t *testing.T) {
	testutil.IsolateXDGDirs(t)

	t.Run("skips when the managed marker is present, even off PATH", func(t *testing.T) {
		root := isolateInstallRoot(t)
		writeManagedMarker(t, root)
		piNotOnPath(t)
		app, mockApp := newApp()

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no commands when pi is already installed, got %d", got)
		}
	})

	t.Run("skips when pi resolves on PATH from elsewhere", func(t *testing.T) {
		isolateInstallRoot(t)
		piOnPath(t)
		app, mockApp := newApp()

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no commands when pi is on PATH, got %d", got)
		}
	})

	// The installer runs under `sh`, not bash: the script's shebang is
	// #!/bin/sh (ADR-0059), the inverse of opencode's bash contract.
	t.Run("installs the official script with sh when nothing is present", func(t *testing.T) {
		isolateInstallRoot(t)
		piNotOnPath(t)
		app, mockApp := newApp()

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 2 || calls[0].Command != "curl" || calls[1].Command != "sh" {
			t.Fatalf("expected staged curl download then sh install, got %v", calls)
		}
	})
}

func TestUninstall(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	root := isolateInstallRoot(t)
	writeManagedMarker(t, root)
	app, mockApp := newApp()

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	// Only the managed root goes: ~/.pi/agent itself holds Pi's config and
	// sessions and must survive (ADR-0059 decision 3).
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("expected the managed install root to be removed, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(root)); err != nil {
		t.Errorf("expected ~/.pi/agent to survive uninstall, stat err: %v", err)
	}
	if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
		t.Errorf("expected uninstall to shell nothing out, got %d command calls", got)
	}
	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUpdateNotSupported(t *testing.T) {
	app := New()
	if err := app.Update(); err == nil {
		t.Fatal("expected Update to return an error")
	}
}

// extensionSourcePattern matches a concrete npm source in prose. The generic
// forms the doc also uses (`npm:<name>`, `npm:<pkg>@<version>`) cannot match,
// so only real package names are collected.
var extensionSourcePattern = regexp.MustCompile(`npm:([a-z0-9][a-z0-9-]*)`)

// ADR-0060 makes docs/apps/pi.md the user-facing mirror of piExtensions, and
// nothing but this test notices when the two drift.
func TestPiExtensionsDocumented(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "apps", "pi.md"))
	if err != nil {
		t.Fatalf("read docs/apps/pi.md: %v", err)
	}
	documented := map[string]bool{}
	for _, match := range extensionSourcePattern.FindAllStringSubmatch(string(data), -1) {
		documented["npm:"+match[1]] = true
	}
	for _, source := range piExtensions {
		if !documented[source] {
			t.Errorf("%s is in piExtensions but not documented in docs/apps/pi.md", source)
		}
	}
	for source := range documented {
		if !slices.Contains(piExtensions, source) {
			t.Errorf("docs/apps/pi.md documents %s, which piExtensions does not install", source)
		}
	}
}

func TestConfigureExtensions(t *testing.T) {
	t.Run("installs every curated source when settings have none", func(t *testing.T) {
		isolateInstallRoot(t)
		piOnPath(t)
		app, mockApp := newApp()

		if err := app.configureExtensions(); err != nil {
			t.Fatalf("configureExtensions error: %v", err)
		}
		if got := piInstallSources(mockApp); !slices.Equal(got, piExtensions) {
			t.Errorf("expected every curated source installed in order, got %v", got)
		}
	})

	// Pi matches npm sources by package name and accepts a bare string or an
	// object carrying "source". Any declared form counts as installed, so a
	// pinned version is never replaced and nothing is re-installed per run.
	t.Run("skips sources settings already declare, in any form", func(t *testing.T) {
		isolateInstallRoot(t)
		piOnPath(t)
		writeAgentSettings(t, `{"packages": [
			"npm:pi-subagents@0.84.0",
			{"source": "npm:pi-web-access", "extensions": ["!*"]},
			"npm:pi-schedule-prompt"
		]}`)
		app, mockApp := newApp()

		if err := app.configureExtensions(); err != nil {
			t.Fatalf("configureExtensions error: %v", err)
		}
		want := piExtensions[3:]
		if got := piInstallSources(mockApp); !slices.Equal(got, want) {
			t.Errorf("expected only %v installed, got %v", want, got)
		}
	})

	t.Run("does not look up pi when nothing is pending", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("pi must not be looked up when nothing is pending")
		})
		declared, err := json.Marshal(piExtensions)
		if err != nil {
			t.Fatal(err)
		}
		writeAgentSettings(t, `{"packages": `+string(declared)+`}`)
		app, mockApp := newApp()

		if err := app.configureExtensions(); err != nil {
			t.Fatalf("configureExtensions error: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no commands, got %d", got)
		}
	})

	t.Run("fails with the fix named when pi is not installed", func(t *testing.T) {
		isolateInstallRoot(t)
		piNotOnPath(t)
		app, mockApp := newApp()

		err := app.configureExtensions()
		if err == nil || !strings.Contains(err.Error(), "dg install --only pi") {
			t.Fatalf("expected an error naming dg install --only pi, got: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no install attempts, got %d", got)
		}
	})

	// Pi's launcher is written beside the managed root, so a configure run right
	// after install works before the new bin dir is on PATH.
	t.Run("falls back to the managed launcher when pi is off PATH", func(t *testing.T) {
		root := isolateInstallRoot(t)
		piNotOnPath(t)
		launcher := managedLauncher()
		if err := os.MkdirAll(filepath.Dir(launcher), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(launcher, []byte("stub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(filepath.Dir(root), "bin", "pi"); launcher != want {
			t.Fatalf("managedLauncher() = %q, want %q", launcher, want)
		}
		app, mockApp := newApp()

		if err := app.configureExtensions(); err != nil {
			t.Fatalf("configureExtensions error: %v", err)
		}
		if len(mockApp.Base.ExecCommandCalls) != len(piExtensions) {
			t.Fatalf(
				"expected %d installs, got %d",
				len(piExtensions),
				len(mockApp.Base.ExecCommandCalls),
			)
		}
		for _, call := range mockApp.Base.ExecCommandCalls {
			if call.Command != launcher {
				t.Errorf("command = %q, want managed launcher %q", call.Command, launcher)
			}
		}
	})

	// A settings file devgeta cannot parse is reported, never treated as empty:
	// silently re-installing over it would hide a corrupted file.
	t.Run("reports an unparseable settings file instead of installing", func(t *testing.T) {
		isolateInstallRoot(t)
		piOnPath(t)
		writeAgentSettings(t, `{not json`)
		app, mockApp := newApp()

		err := app.configureExtensions()
		if err == nil || !strings.Contains(err.Error(), "settings.json") {
			t.Fatalf("expected an error naming settings.json, got: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no install attempts, got %d", got)
		}
	})

	// No rollback: extensions are Pi's own state and a re-run installs whatever
	// is still missing, so a failure stops, names the extension and removes nothing.
	t.Run("stops at the first failure and names the extension", func(t *testing.T) {
		isolateInstallRoot(t)
		piOnPath(t)
		app, mockApp := newApp()
		mockApp.Base.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
			if len(c.Args) == 2 && c.Args[1] == piExtensions[2] {
				return "", "", fmt.Errorf("npm exited 1")
			}
			return "", "", nil
		}

		err := app.configureExtensions()
		if err == nil || !strings.Contains(err.Error(), piExtensions[2]) {
			t.Fatalf("expected an error naming %s, got: %v", piExtensions[2], err)
		}
		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 3 {
			t.Fatalf("expected installs to stop at the failing one (3 calls), got %d", len(calls))
		}
		for _, call := range calls {
			if call.Args[0] != "install" {
				t.Errorf("unexpected non-install call %v", call.Args)
			}
		}
	})
}

func TestConfigure(t *testing.T) {
	t.Run("SoftConfigure registers pi after the extensions install", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		isolateInstallRoot(t)
		piOnPath(t)
		app := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}
		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			t.Fatalf("load global config: %v", err)
		}
		if !gc.IsInstalledByDevgeta(constants.Pi, "package") {
			t.Error("expected pi in the installed packages list")
		}
		if got := len(piInstallSources(tc.MockApp)); got != len(piExtensions) {
			t.Errorf("expected %d extension installs, got %d", len(piExtensions), got)
		}
	})

	// Updating installed extensions is Pi's job (`pi update`), so a forced
	// configure only fills in what is missing and never unpins a version.
	t.Run("ForceConfigure installs only the missing extensions", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		isolateInstallRoot(t)
		piOnPath(t)
		writeAgentSettings(t, `{"packages": ["npm:pi-subagents@0.84.0"]}`)
		app := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.ForceConfigure(); err != nil {
			t.Fatalf("ForceConfigure error: %v", err)
		}
		if got := piInstallSources(tc.MockApp); !slices.Equal(got, piExtensions[1:]) {
			t.Errorf("expected only the missing extensions installed, got %v", got)
		}
	})

	t.Run("does not register pi when an extension fails", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		isolateInstallRoot(t)
		piOnPath(t)
		tc.MockApp.Base.ExecCommandError = fmt.Errorf("npm exited 1")
		app := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.SoftConfigure(); err == nil {
			t.Fatal("expected SoftConfigure to fail")
		}
		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			t.Fatalf("load global config: %v", err)
		}
		if gc.IsInstalledByDevgeta(constants.Pi, "package") {
			t.Error("expected a failed configure to leave pi out of the installed list")
		}
	})
}
