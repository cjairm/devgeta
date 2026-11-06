package pi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
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

// useFakeExtensions swaps in a fixed multi-entry list, so the skip, order and
// stop-at-failure behavior stays covered however short the curated set is.
func useFakeExtensions(t *testing.T) {
	t.Helper()
	original := piExtensions
	piExtensions = []string{"npm:ext-a", "npm:ext-b", "npm:ext-c", "npm:ext-d"}
	t.Cleanup(func() { piExtensions = original })
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
		useFakeExtensions(t)
		writeAgentSettings(t, `{"packages": [
			"npm:ext-a@0.84.0",
			{"source": "npm:ext-b", "extensions": ["!*"]},
			"npm:ext-c"
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
		useFakeExtensions(t)
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
		useRepoThemes(t)
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
		useRepoThemes(t)
		isolateInstallRoot(t)
		piOnPath(t)
		useFakeExtensions(t)
		writeAgentSettings(t, `{"packages": ["npm:ext-a@0.84.0"]}`)
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
		useRepoThemes(t)
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

	t.Run("ForceConfigure writes the theme, selects it, then installs extensions", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		useRepoThemes(t)
		isolateInstallRoot(t)
		piOnPath(t)
		writeAgentSettings(t, `{"theme": "dark"}`)
		app := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.ForceConfigure(); err != nil {
			t.Fatalf("ForceConfigure error: %v", err)
		}
		if _, err := os.Stat(themeFilePath()); err != nil {
			t.Errorf("theme file not written: %v", err)
		}
		if got := string(readSettingsKeys(t)["theme"]); got != `"devgeta"` {
			t.Errorf("force should select devgeta, got %s", got)
		}
		if got := len(piInstallSources(tc.MockApp)); got != len(piExtensions) {
			t.Errorf("expected %d extension installs, got %d", len(piExtensions), got)
		}
	})

	t.Run("SoftConfigure writes the theme and selects it when neither exists", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		useRepoThemes(t)
		isolateInstallRoot(t)
		piOnPath(t)
		app := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}
		if _, err := os.Stat(themeFilePath()); err != nil {
			t.Errorf("theme file not written: %v", err)
		}
		if got := string(readSettingsKeys(t)["theme"]); got != `"devgeta"` {
			t.Errorf("expected theme devgeta, got %s", got)
		}
	})

	t.Run("SoftConfigure keeps the user's theme choice and an existing theme file", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		useRepoThemes(t)
		isolateInstallRoot(t)
		piOnPath(t)
		writeAgentSettings(t, `{"theme": "dark"}`)
		if err := os.MkdirAll(filepath.Dir(themeFilePath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(themeFilePath(), []byte(`{"edited":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		app := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}
		if got := string(readSettingsKeys(t)["theme"]); got != `"dark"` {
			t.Errorf("soft replaced the user's theme: %s", got)
		}
		if got, _ := os.ReadFile(themeFilePath()); string(got) != `{"edited":true}` {
			t.Errorf("soft overwrote an existing theme file: %s", got)
		}
	})

	t.Run("an unparseable settings file stops configure before any extension install", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		useRepoThemes(t)
		isolateInstallRoot(t)
		piOnPath(t)
		writeAgentSettings(t, `{"packages": [`)
		app := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		err := app.ForceConfigure()
		if err == nil || !strings.Contains(err.Error(), "settings.json") {
			t.Fatalf("expected an error naming settings.json, got %v", err)
		}
		if got := len(piInstallSources(tc.MockApp)); got != 0 {
			t.Errorf("no extensions should install after the failure, got %d", got)
		}
	})
}

// piRequiredThemeColors is the 51 roles Pi's theme schema requires under
// "colors", from https://pi.dev/schemas/theme.schema.json (read 2026-11-06).
// Pi rejects a theme missing one, so when Pi adds a role this list - and the
// template - must follow.
var piRequiredThemeColors = []string{
	"accent", "border", "borderAccent", "borderMuted", "success", "error",
	"warning", "muted", "dim", "text", "thinkingText",
	"selectedBg", "userMessageBg", "userMessageText", "customMessageBg",
	"customMessageText", "customMessageLabel",
	"toolPendingBg", "toolSuccessBg", "toolErrorBg", "toolTitle", "toolOutput",
	"mdHeading", "mdLink", "mdLinkUrl", "mdCode", "mdCodeBlock",
	"mdCodeBlockBorder", "mdQuote", "mdQuoteBorder", "mdHr", "mdListBullet",
	"toolDiffAdded", "toolDiffRemoved", "toolDiffContext",
	"syntaxComment", "syntaxKeyword", "syntaxFunction", "syntaxVariable",
	"syntaxString", "syntaxNumber", "syntaxType", "syntaxOperator",
	"syntaxPunctuation",
	"thinkingOff", "thinkingMinimal", "thinkingLow", "thinkingMedium",
	"thinkingHigh", "thinkingXhigh", "bashMode",
}

// piOptionalThemeColors are the schema's optional "colors" keys. The schema
// rejects any key outside required + optional.
var piOptionalThemeColors = []string{
	"scrollbarTrack", "scrollbarThumb", "searchMatchBg", "searchMatchText", "thinkingMax",
}

// repoRoot locates the repository from this test file's own path.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
}

// useRepoThemes points the theme loader and Pi's template directory at the
// real repo's configs/, restoring everything in t.Cleanup, so a test renders
// the shipped template with the shipped themes rather than fixtures.
func useRepoThemes(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	origThemes := paths.Paths.App.Configs.Themes
	origNeovim := paths.Paths.App.Configs.Neovim
	origNvim := paths.Paths.Config.Nvim
	origPi := paths.Paths.App.Configs.Pi
	t.Cleanup(func() {
		paths.Paths.App.Configs.Themes = origThemes
		paths.Paths.App.Configs.Neovim = origNeovim
		paths.Paths.Config.Nvim = origNvim
		paths.Paths.App.Configs.Pi = origPi
	})
	paths.Paths.App.Configs.Themes = filepath.Join(root, "configs", "themes")
	paths.Paths.App.Configs.Neovim = filepath.Join(root, "configs", "neovim")
	paths.Paths.Config.Nvim = filepath.Join(root, "does-not-exist-nvim")
	paths.Paths.App.Configs.Pi = filepath.Join(root, "configs", "pi")
	return root
}

func shippedThemeNames(t *testing.T, root string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "configs", "themes", "*.yaml"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no shipped themes found: %v", err)
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, strings.TrimSuffix(filepath.Base(m), ".yaml"))
	}
	return names
}

var piHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// The template must satisfy Pi's schema for every shipped theme: Pi falls back
// to its own colors on an invalid theme, which would silently undo this whole
// feature.
func TestThemeTemplateSatisfiesPiSchema(t *testing.T) {
	root := useRepoThemes(t)
	for _, name := range shippedThemeNames(t, root) {
		t.Run(name, func(t *testing.T) {
			def, err := theme.Load(name)
			if err != nil {
				t.Fatalf("load theme: %v", err)
			}
			palette, err := def.PaletteFor(constants.Pi)
			if err != nil {
				t.Fatalf("palette: %v", err)
			}
			out := filepath.Join(t.TempDir(), "devgeta.json")
			if err := files.GenerateFromTemplate(
				filepath.Join(paths.Paths.App.Configs.Pi, "themes", "devgeta.json.tmpl"),
				out,
				map[string]any{"Palette": palette},
			); err != nil {
				t.Fatalf("render: %v", err)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			var parsed struct {
				Name       string            `json:"name"`
				Appearance string            `json:"appearance"`
				Vars       map[string]string `json:"vars"`
				Colors     map[string]string `json:"colors"`
			}
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("rendered theme is not valid JSON: %v\n%s", err, data)
			}
			if parsed.Name != "devgeta" {
				t.Errorf("name = %q, want devgeta", parsed.Name)
			}
			if parsed.Appearance != "dark" {
				t.Errorf("appearance = %q, want dark", parsed.Appearance)
			}
			for _, key := range piRequiredThemeColors {
				if _, ok := parsed.Colors[key]; !ok {
					t.Errorf("missing required color %q", key)
				}
			}
			allowed := append(slices.Clone(piRequiredThemeColors), piOptionalThemeColors...)
			for key, value := range parsed.Colors {
				if !slices.Contains(allowed, key) {
					t.Errorf("color %q is not in Pi's schema", key)
				}
				if _, isVar := parsed.Vars[value]; value != "" && !isVar && !piHexColor.MatchString(value) {
					t.Errorf("color %q = %q is neither \"\", a var, nor hex", key, value)
				}
			}
			for key, value := range parsed.Vars {
				if !piHexColor.MatchString(value) {
					t.Errorf("var %q = %q is not a #RRGGBB hex", key, value)
				}
			}
			if parsed.Colors["text"] != "" {
				t.Errorf("text = %q, want \"\" (terminal foreground)", parsed.Colors["text"])
			}
		})
	}
}

func themeFilePath() string {
	return filepath.Join(paths.PiAgentDir(), "themes", "devgeta.json")
}

func footerOverrideFilePath() string {
	return filepath.Join(paths.PiAgentDir(), "extensions", "powerline-footer", "theme.json")
}

func settingsFilePath() string {
	return filepath.Join(paths.PiAgentDir(), "settings.json")
}

func readSettingsKeys(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(settingsFilePath())
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("settings is not valid JSON: %v\n%s", err, data)
	}
	return keys
}

func defaultDefinition(t *testing.T) theme.Definition {
	t.Helper()
	def, err := theme.Load(theme.DefaultThemeName)
	if err != nil {
		t.Fatalf("load default theme: %v", err)
	}
	return def
}

func TestForceConfigureTheme(t *testing.T) {
	// `dg theme set` calls this directly for the eighth surface, so it must
	// write the theme file and nothing else: not Pi's settings, and no pi
	// command.
	t.Run("renders the theme file and touches nothing else", func(t *testing.T) {
		useRepoThemes(t)
		isolateInstallRoot(t)
		app, mockApp := newApp()
		def := defaultDefinition(t)

		if err := app.ForceConfigureTheme(def); err != nil {
			t.Fatalf("ForceConfigureTheme error: %v", err)
		}
		data, err := os.ReadFile(themeFilePath())
		if err != nil {
			t.Fatalf("theme file not written: %v", err)
		}
		palette, err := def.PaletteFor(constants.Pi)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), palette.Blue) {
			t.Errorf("rendered theme does not carry the palette's blue %s", palette.Blue)
		}
		if _, err := os.Stat(settingsFilePath()); !os.IsNotExist(err) {
			t.Errorf("settings.json must not be written by a theme switch (stat err: %v)", err)
		}
		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("re-rendering follows the new theme but keeps the file name", func(t *testing.T) {
		useRepoThemes(t)
		isolateInstallRoot(t)
		app, _ := newApp()
		for _, name := range []string{"default", "tokyonight"} {
			def, err := theme.Load(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := app.ForceConfigureTheme(def); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			palette, _ := def.PaletteFor(constants.Pi)
			data, err := os.ReadFile(themeFilePath())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), palette.Blue) {
				t.Errorf("%s: theme file does not carry blue %s", name, palette.Blue)
			}
		}
	})

	t.Run("leaves the user's own themes in the directory alone", func(t *testing.T) {
		useRepoThemes(t)
		isolateInstallRoot(t)
		themesDir := filepath.Dir(themeFilePath())
		if err := os.MkdirAll(themesDir, 0o755); err != nil {
			t.Fatal(err)
		}
		mine := filepath.Join(themesDir, "mine.json")
		if err := os.WriteFile(mine, []byte(`{"mine":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		app, _ := newApp()
		if err := app.ForceConfigureTheme(defaultDefinition(t)); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(mine); err != nil || string(got) != `{"mine":true}` {
			t.Errorf("user theme was disturbed: %q, %v", got, err)
		}
	})
}

func TestSelectTheme(t *testing.T) {
	t.Run("creates settings with only the theme key when absent", func(t *testing.T) {
		isolateInstallRoot(t)
		if err := selectTheme(false); err != nil {
			t.Fatalf("selectTheme error: %v", err)
		}
		keys := readSettingsKeys(t)
		if len(keys) != 1 || string(keys["theme"]) != `"devgeta"` {
			t.Errorf("expected only theme=devgeta, got %v", keys)
		}
	})

	t.Run("soft keeps a theme the user already chose", func(t *testing.T) {
		isolateInstallRoot(t)
		writeAgentSettings(t, `{"theme": "dark"}`)
		if err := selectTheme(false); err != nil {
			t.Fatal(err)
		}
		if got := string(readSettingsKeys(t)["theme"]); got != `"dark"` {
			t.Errorf("soft replaced the user's theme: %s", got)
		}
	})

	t.Run("soft adds theme when settings has other keys but no theme", func(t *testing.T) {
		isolateInstallRoot(t)
		writeAgentSettings(t, `{"defaultModel": "x"}`)
		if err := selectTheme(false); err != nil {
			t.Fatal(err)
		}
		keys := readSettingsKeys(t)
		if string(keys["theme"]) != `"devgeta"` || string(keys["defaultModel"]) != `"x"` {
			t.Errorf("unexpected settings: %v", keys)
		}
	})

	t.Run("force replaces the user's theme", func(t *testing.T) {
		isolateInstallRoot(t)
		writeAgentSettings(t, `{"theme": "dark"}`)
		if err := selectTheme(true); err != nil {
			t.Fatal(err)
		}
		if got := string(readSettingsKeys(t)["theme"]); got != `"devgeta"` {
			t.Errorf("force did not set devgeta: %s", got)
		}
	})

	// The packages value is Pi's own state (ADR-0060). Compare compacted JSON
	// so indentation changes pass but any change of content - including an
	// HTML-escaped "&" in a source URL - fails.
	t.Run("preserves packages and every other key as written", func(t *testing.T) {
		isolateInstallRoot(t)
		original := `{"packages": ["npm:a@1", {"source": "git:https://x.test/r?a=1&b=<2>", "skills": []}], "other": {"n": 1.50}}`
		writeAgentSettings(t, original)
		if err := selectTheme(true); err != nil {
			t.Fatal(err)
		}
		var want map[string]json.RawMessage
		if err := json.Unmarshal([]byte(original), &want); err != nil {
			t.Fatal(err)
		}
		got := readSettingsKeys(t)
		for _, key := range []string{"packages", "other"} {
			var wantCompact, gotCompact bytes.Buffer
			if err := json.Compact(&wantCompact, want[key]); err != nil {
				t.Fatal(err)
			}
			if err := json.Compact(&gotCompact, got[key]); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
			if wantCompact.String() != gotCompact.String() {
				t.Errorf("%s changed:\n want %s\n  got %s", key, wantCompact.String(), gotCompact.String())
			}
		}
	})

	t.Run("an unparseable settings file is an error naming it, left untouched", func(t *testing.T) {
		isolateInstallRoot(t)
		writeAgentSettings(t, `{"packages": [`)
		for _, force := range []bool{false, true} {
			err := selectTheme(force)
			if err == nil {
				t.Fatalf("force=%v: expected an error", force)
			}
			if !strings.Contains(err.Error(), "settings.json") {
				t.Errorf("force=%v: error does not name the file: %v", force, err)
			}
		}
		if got, _ := os.ReadFile(settingsFilePath()); string(got) != `{"packages": [` {
			t.Errorf("unparseable file was modified: %q", got)
		}
	})
}

// ADR-0062 §2: configure ships the pi-powerline-footer color override beside
// the extension. It is not part of a theme switch: it names Pi roles, so it
// never changes with the palette, and `dg theme set` must leave it alone.
func TestConfigureWritesFooterOverride(t *testing.T) {
	run := func(t *testing.T, force bool, existing string) (string, error) {
		t.Helper()
		tc := testutil.SetupCompleteTest(t)
		t.Cleanup(tc.Cleanup)
		useRepoThemes(t)
		isolateInstallRoot(t)
		piOnPath(t)
		if existing != "" {
			if err := os.MkdirAll(filepath.Dir(footerOverrideFilePath()), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(footerOverrideFilePath(), []byte(existing), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		app := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		var err error
		if force {
			err = app.ForceConfigure()
		} else {
			err = app.SoftConfigure()
		}
		got, _ := os.ReadFile(footerOverrideFilePath())
		return string(got), err
	}
	shipped := func(t *testing.T) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repoRoot(t), "configs", "pi", "powerline-footer", "theme.json"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	t.Run("soft writes it when absent", func(t *testing.T) {
		got, err := run(t, false, "")
		if err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}
		if got != shipped(t) {
			t.Errorf("override not written from the embedded file, got %q", got)
		}
	})

	t.Run("soft keeps an existing file byte for byte", func(t *testing.T) {
		got, err := run(t, false, `{"colors":{"model":"#ffffff"}}`)
		if err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}
		if got != `{"colors":{"model":"#ffffff"}}` {
			t.Errorf("soft overwrote the user's override: %s", got)
		}
	})

	t.Run("force replaces an existing file", func(t *testing.T) {
		got, err := run(t, true, `{"colors":{"model":"#ffffff"}}`)
		if err != nil {
			t.Fatalf("ForceConfigure error: %v", err)
		}
		if got != shipped(t) {
			t.Errorf("force did not restore the shipped override, got %q", got)
		}
	})

	t.Run("a theme switch does not create it", func(t *testing.T) {
		useRepoThemes(t)
		isolateInstallRoot(t)
		app, _ := newApp()
		if err := app.ForceConfigureTheme(defaultDefinition(t)); err != nil {
			t.Fatalf("ForceConfigureTheme error: %v", err)
		}
		if _, err := os.Stat(footerOverrideFilePath()); !os.IsNotExist(err) {
			t.Errorf("dg theme set must not touch the footer override, stat err: %v", err)
		}
	})
}
