package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
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
