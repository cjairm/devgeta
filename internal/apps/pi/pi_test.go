package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
// root this app keys its detection and uninstall on - into a throwaway
// directory, and restores it via t.Cleanup.
func isolateInstallRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".pi", "agent", "install")
	old := paths.Paths.Home.PiInstall
	t.Cleanup(func() { paths.Paths.Home.PiInstall = old })
	paths.Paths.Home.PiInstall = root
	// The relocated agent dir wins over the install root, so clear it: a test
	// that wants it sets its own value.
	t.Setenv("PI_CODING_AGENT_DIR", "")
	return root
}

// stubLookPath swaps the package-level PATH-lookup seam every app uses for
// binary detection (commands.LookPathFn), so a test decides whether pi
// resolves on PATH instead of inheriting whatever the machine running the
// test has installed.
func stubLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := commands.LookPathFn
	t.Cleanup(func() { commands.LookPathFn = orig })
	commands.LookPathFn = fn
}

// stubNodeAvailable makes the npm PATH check pass — the `node --version`
// mock output is set per-test on the mock base.
func stubNodeAvailable(t *testing.T) {
	t.Helper()
	stubLookPath(t, func(name string) (string, error) {
		if name == "npm" {
			return "/usr/local/bin/npm", nil
		}
		return "", fmt.Errorf("%s not found", name)
	})
}

func TestRequireCompatibleNode(t *testing.T) {
	t.Run("fails with an actionable error when node is missing", func(t *testing.T) {
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("not found")
		})
		mockApp := testutil.NewMockApp()
		mockApp.Base.ExecCommandError = fmt.Errorf("exec: node: not found")
		app := &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.requireCompatibleNode()
		if err == nil {
			t.Fatal("expected an error when node is missing")
		}
		// The error must point at the fix, not just the fact.
		if !strings.Contains(err.Error(), "dg install --only languages") {
			t.Errorf("expected the error to suggest dg install --only languages, got: %v", err)
		}
	})

	t.Run("fails when node is older than 22.19", func(t *testing.T) {
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("not found")
		})
		mockApp := testutil.NewMockApp()
		mockApp.Base.ExecCommandStdout = "v18.0.0\n"
		app := &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.requireCompatibleNode(); err == nil {
			t.Fatal("expected an error for an outdated node")
		}
	})

	t.Run("passes with node 22.19+ and npm on PATH", func(t *testing.T) {
		for _, version := range []string{"v22.19.0", "v22.20.1", "v23.1.0"} {
			stubNodeAvailable(t)
			mockApp := testutil.NewMockApp()
			mockApp.Base.ExecCommandStdout = version + "\n"
			app := &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}

			if err := app.requireCompatibleNode(); err != nil {
				t.Errorf("node %s should satisfy the requirement: %v", version, err)
			}
		}
	})

	t.Run("fails when npm is missing", func(t *testing.T) {
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("not found")
		})
		mockApp := testutil.NewMockApp()
		mockApp.Base.ExecCommandStdout = "v22.19.0\n"
		app := &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.requireCompatibleNode(); err == nil {
			t.Fatal("expected an error when npm is missing")
		}
	})
}

func TestParseNodeVersion(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"v22.19.0", 22, 19, true},
		{"v18.0.0", 18, 0, true},
		{"v23.1.0", 23, 1, true},
		{"garbage", 0, 0, false},
		{"v22", 0, 0, false},
		{"v22.19", 0, 0, false},
	}
	for _, tc := range cases {
		major, minor, ok := parseNodeVersion(tc.in)
		if ok != tc.ok || (ok && (major != tc.major || minor != tc.minor)) {
			t.Errorf("parseNodeVersion(%q) = (%d, %d, %v), want (%d, %d, %v)",
				tc.in, major, minor, ok, tc.major, tc.minor, tc.ok)
		}
	}
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

func TestSoftInstall(t *testing.T) {
	testutil.IsolateXDGDirs(t)

	t.Run("skips when the managed marker is present, even off PATH", func(t *testing.T) {
		root := isolateInstallRoot(t)
		writeManagedMarker(t, root)
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("not found")
		})

		mockApp := testutil.NewMockApp()
		app := &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no commands when pi is already installed, got %d", got)
		}
		if mockApp.Cmd.MaybeInstalled != "" || mockApp.Cmd.InstalledPkg != "" {
			t.Errorf(
				"expected no package-manager calls, got maybe=%q install=%q",
				mockApp.Cmd.MaybeInstalled, mockApp.Cmd.InstalledPkg,
			)
		}
	})

	t.Run("skips when pi resolves on PATH from elsewhere", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(string) (string, error) {
			return "/usr/local/bin/pi", nil
		})

		mockApp := testutil.NewMockApp()
		app := &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}

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
		// Satisfy the runtime preflight so the mock install is reached.
		stubNodeAvailable(t)
		mockApp := testutil.NewMockApp()
		mockApp.Base.ExecCommandStdout = "v22.19.0\n"
		app := &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 3 {
			t.Fatalf("expected 3 ExecCommand calls (node check, download, execute), got %d", len(calls))
		}
		if calls[0].Command != "node" || calls[1].Command != "curl" || calls[2].Command != "sh" {
			t.Errorf(
				"expected staged node-check, curl, then sh install, got %q, %q, then %q",
				calls[0].Command, calls[1].Command, calls[2].Command,
			)
		}
		if mockApp.Cmd.MaybeInstalled != "" {
			t.Errorf(
				"expected no package-manager install, got MaybeInstallPackage(%q)",
				mockApp.Cmd.MaybeInstalled,
			)
		}
	})
}

func TestUninstall(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	root := isolateInstallRoot(t)
	writeManagedMarker(t, root)

	mockApp := testutil.NewMockApp()
	app := &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	// Only the managed root goes: ~/.pi/agent itself holds Pi's config and
	// sessions and must survive (ADR-0059 decision 3).
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("expected the managed install root to be removed, stat err: %v", err)
	}
	agentDir := filepath.Dir(root)
	if _, err := os.Stat(agentDir); err != nil {
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

// writeAgentSettings writes Pi's settings.json into the agent home the tests
// redirect — the parent of the managed install root — matching the file
// `pi install` maintains.
func writeAgentSettings(t *testing.T, settingsJSON string) {
	t.Helper()
	dir := filepath.Dir(paths.Paths.Home.PiInstall)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settingsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

// piInstalledSources returns the source argument of every `pi install <source>`
// call the mock recorded, in call order.
func piInstallSources(mockApp *testutil.MockApp) []string {
	var sources []string
	for _, call := range mockApp.Base.ExecCommandCalls {
		if filepath.Base(call.Command) != "pi" {
			continue
		}
		if len(call.Args) == 2 && call.Args[0] == "install" {
			sources = append(sources, call.Args[1])
		}
	}
	return sources
}

func TestConfigureExtensions(t *testing.T) {
	newApp := func() (*Pi, *testutil.MockApp) {
		mockApp := testutil.NewMockApp()
		return &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}, mockApp
	}

	t.Run("installs every curated source when settings have none", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		})
		app, mockApp := newApp()

		if err := app.configureExtensions(false); err != nil {
			t.Fatalf("configureExtensions error: %v", err)
		}
		if got := piInstallSources(mockApp); !slices.Equal(got, piExtensions) {
			t.Errorf("expected every curated source installed in order, got %v", got)
		}
	})

	t.Run("skips sources settings already declare", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		})
		installed := piExtensions[:len(piExtensions)-1]
		quoted, err := json.Marshal(installed)
		if err != nil {
			t.Fatal(err)
		}
		writeAgentSettings(t, `{"packages": `+string(quoted)+`}`)
		app, mockApp := newApp()

		if err := app.configureExtensions(false); err != nil {
			t.Fatalf("configureExtensions error: %v", err)
		}
		want := piExtensions[len(piExtensions)-1:]
		if got := piInstallSources(mockApp); !slices.Equal(got, want) {
			t.Errorf("expected only %v installed, got %v", want, got)
		}
	})

	// Pi's settings accept an object carrying the source, not just a bare
	// string. Treating those entries as missing would re-install on every run.
	t.Run("recognizes object-shaped package entries", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		})
		writeAgentSettings(t, `{"packages": [{"source": "npm:pi-subagents"}]}`)
		app, mockApp := newApp()

		if err := app.configureExtensions(false); err != nil {
			t.Fatalf("configureExtensions error: %v", err)
		}
		if got := piInstallSources(mockApp); slices.Contains(got, "npm:pi-subagents") {
			t.Errorf("expected an already-declared source to be skipped, got %v", got)
		}
	})

	t.Run("installs nothing when every source is already declared", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("pi must not be looked up when nothing is pending")
		})
		quoted, err := json.Marshal(piExtensions)
		if err != nil {
			t.Fatal(err)
		}
		writeAgentSettings(t, `{"packages": `+string(quoted)+`}`)
		app, mockApp := newApp()

		if err := app.configureExtensions(false); err != nil {
			t.Fatalf("configureExtensions error: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no commands, got %d", got)
		}
	})

	t.Run("force re-installs sources already declared", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		})
		quoted, err := json.Marshal(piExtensions)
		if err != nil {
			t.Fatal(err)
		}
		writeAgentSettings(t, `{"packages": `+string(quoted)+`}`)
		app, mockApp := newApp()

		if err := app.configureExtensions(true); err != nil {
			t.Fatalf("configureExtensions error: %v", err)
		}
		if got := piInstallSources(mockApp); !slices.Equal(got, piExtensions) {
			t.Errorf("expected force to re-install every source, got %v", got)
		}
	})

	t.Run("fails with the fix named when pi is not installed", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("not found")
		})
		app, mockApp := newApp()

		err := app.configureExtensions(false)
		if err == nil {
			t.Fatal("expected an error when pi is not on PATH")
		}
		if !strings.Contains(err.Error(), "dg install --only pi") {
			t.Errorf("expected the error to name the fix, got: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no install attempts, got %d", got)
		}
	})

	// A settings file devgeta cannot parse is reported, never overwritten and
	// never treated as empty: silently re-installing over it would hide a
	// corrupted file the user needs to see.
	t.Run("reports an unparseable settings file instead of installing", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		})
		writeAgentSettings(t, `{not json`)
		app, mockApp := newApp()

		err := app.configureExtensions(false)
		if err == nil {
			t.Fatal("expected an error for an unparseable settings file")
		}
		if !strings.Contains(err.Error(), "settings.json") {
			t.Errorf("expected the error to name the settings file, got: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no install attempts, got %d", got)
		}
	})

	t.Run("names the extension that failed to install", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		})
		app, mockApp := newApp()
		mockApp.Base.ExecCommandError = fmt.Errorf("npm exited 1")

		err := app.configureExtensions(false)
		if err == nil {
			t.Fatal("expected an install failure to surface")
		}
		if !strings.Contains(err.Error(), "npm:pi-subagents") {
			t.Errorf("expected the error to name the first extension, got: %v", err)
		}
	})
}

func TestConfigure(t *testing.T) {
	t.Run("registers pi only after the extensions install", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		isolateInstallRoot(t)
		stubLookPath(t, func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		})
		app, mockApp := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}, tc.MockApp

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
		if got := len(piInstallSources(mockApp)); got != len(piExtensions) {
			t.Errorf("expected %d extension installs, got %d", len(piExtensions), got)
		}
	})

	t.Run("does not register pi when an extension fails", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		isolateInstallRoot(t)
		stubLookPath(t, func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		})
		tc.MockApp.Base.ExecCommandError = fmt.Errorf("npm exited 1")
		app := &Pi{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.ForceConfigure(); err == nil {
			t.Fatal("expected ForceConfigure to fail")
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

func TestPiAgentDir(t *testing.T) {
	root := isolateInstallRoot(t)
	home := paths.Paths.Home.Root

	t.Run("defaults to the managed install root's parent", func(t *testing.T) {
		t.Setenv("PI_CODING_AGENT_DIR", "")
		if got, want := piAgentDir(), filepath.Dir(root); got != want {
			t.Errorf("piAgentDir() = %q, want %q", got, want)
		}
	})

	// Pi relocates settings.json and its npm tree together when this is set,
	// so devgeta has to read from the same directory or it never sees what Pi
	// installed.
	t.Run("honors PI_CODING_AGENT_DIR", func(t *testing.T) {
		t.Setenv("PI_CODING_AGENT_DIR", "/relocated/agent")
		if got, want := piAgentDir(), "/relocated/agent"; got != want {
			t.Errorf("piAgentDir() = %q, want %q", got, want)
		}
		if got, want := piSettingsPath(), "/relocated/agent/settings.json"; got != want {
			t.Errorf("piSettingsPath() = %q, want %q", got, want)
		}
	})

	// Pi expands a leading tilde itself, so "~/agent" must not be read as a
	// path relative to the working directory.
	t.Run("expands a leading tilde", func(t *testing.T) {
		for _, tc := range []struct{ in, want string }{
			{"~", home},
			{"~/.config/pi", filepath.Join(home, ".config", "pi")},
		} {
			t.Setenv("PI_CODING_AGENT_DIR", tc.in)
			if got := piAgentDir(); got != tc.want {
				t.Errorf("piAgentDir() with %q = %q, want %q", tc.in, got, tc.want)
			}
		}
	})
}

// setenvAgentDir points PI_CODING_AGENT_DIR at a fresh directory and returns
// it, so a test speaks to the relocated settings file Pi would actually write.
func setenvAgentDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	return dir
}

func TestConfigureExtensionsRollback(t *testing.T) {
	newApp := func(fn func(commands.CommandParams) (string, string, error)) (*Pi, *testutil.MockApp) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.ExecCommandFn = fn
		return &Pi{Cmd: mockApp.Cmd, Base: mockApp.Base}, mockApp
	}
	allowPi := func(name string) (string, error) { return "/usr/local/bin/" + name, nil }

	// piSequence renders every pi call as "<action> <source>", so a test asserts
	// on the exact install/remove order rather than on call counts.
	piSequence := func(mockApp *testutil.MockApp) []string {
		var seq []string
		for _, call := range mockApp.Base.ExecCommandCalls {
			if filepath.Base(call.Command) != "pi" || len(call.Args) != 2 {
				continue
			}
			seq = append(seq, call.Args[0]+" "+call.Args[1])
		}
		return seq
	}

	// A partial extension set is not an end state (§4, atomic installation), so
	// a failure after some installs must undo exactly those installs.
	t.Run("rolls back this run's installs when a later one fails", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, allowPi)
		app, mockApp := newApp(func(c commands.CommandParams) (string, string, error) {
			if len(c.Args) == 2 && c.Args[0] == "install" && c.Args[1] == piExtensions[2] {
				return "", "", fmt.Errorf("npm exited 1")
			}
			return "", "", nil
		})

		err := app.configureExtensions(false)
		if err == nil {
			t.Fatal("expected the failed install to surface")
		}
		if !strings.Contains(err.Error(), piExtensions[2]) {
			t.Errorf("expected the failing extension named in the error, got: %v", err)
		}

		want := []string{
			"install " + piExtensions[0],
			"install " + piExtensions[1],
			"install " + piExtensions[2],
			"remove " + piExtensions[1],
			"remove " + piExtensions[0],
		}
		if got := piSequence(mockApp); !slices.Equal(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	// A force re-install of something Pi already listed is not ours to remove:
	// undoing it would delete an extension the user already had.
	t.Run("never removes extensions that were already declared", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, allowPi)
		declared, err := json.Marshal(piExtensions[:2])
		if err != nil {
			t.Fatal(err)
		}
		writeAgentSettings(t, `{"packages": `+string(declared)+`}`)
		app, mockApp := newApp(func(c commands.CommandParams) (string, string, error) {
			if len(c.Args) == 2 && c.Args[0] == "install" && c.Args[1] == piExtensions[3] {
				return "", "", fmt.Errorf("npm exited 1")
			}
			return "", "", nil
		})

		if err := app.configureExtensions(true); err == nil {
			t.Fatal("expected the failed install to surface")
		}
		// The two declared sources are re-installed and stay; only the one this
		// run added is rolled back.
		want := []string{
			"install " + piExtensions[0],
			"install " + piExtensions[1],
			"install " + piExtensions[2],
			"install " + piExtensions[3],
			"remove " + piExtensions[2],
		}
		if got := piSequence(mockApp); !slices.Equal(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	// A rollback that itself fails must report the failure, name the package it
	// could not remove, and still undo the rest of the set.
	t.Run("keeps rolling back after a removal fails, and names what is left", func(t *testing.T) {
		isolateInstallRoot(t)
		stubLookPath(t, allowPi)
		app, mockApp := newApp(func(c commands.CommandParams) (string, string, error) {
			if len(c.Args) != 2 {
				return "", "", nil
			}
			switch {
			case c.Args[0] == "install" && c.Args[1] == piExtensions[4]:
				return "", "", fmt.Errorf("npm exited 1")
			case c.Args[0] == "remove" && c.Args[1] == piExtensions[1]:
				return "", "", fmt.Errorf("npm uninstall exited 1")
			}
			return "", "", nil
		})

		err := app.configureExtensions(false)
		if err == nil {
			t.Fatal("expected a failure")
		}
		if !strings.Contains(err.Error(), piExtensions[1]) || !strings.Contains(err.Error(), "pi remove") {
			t.Errorf("expected the stuck package and the manual fix in the error, got: %v", err)
		}
		want := []string{
			"install " + piExtensions[0],
			"install " + piExtensions[1],
			"install " + piExtensions[2],
			"install " + piExtensions[3],
			"install " + piExtensions[4],
			"remove " + piExtensions[3],
			"remove " + piExtensions[2],
			"remove " + piExtensions[1],
			"remove " + piExtensions[0],
		}
		if got := piSequence(mockApp); !slices.Equal(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})
}
