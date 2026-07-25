package neovim

import (
	"errors"
	"testing"

	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
)

// TestInstallDeps_Mac verifies that on macOS, gcc and xclip are NOT installed,
// but make, ripgrep, fd-find, unzip, and tree-sitter-cli are.
func TestInstallDeps_Mac(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = true

	err := InstallDeps(mockApp.Base, mockApp.Cmd)
	if err != nil {
		t.Fatalf("InstallDeps on macOS should succeed, got: %v", err)
	}

	pkgs := mockApp.Cmd.MaybeInstalledPkgs
	expected := []string{
		constants.Make,
		constants.Ripgrep,
		constants.FdFind,
		constants.Unzip,
		constants.TreeSitterCli,
		constants.Markdownlint,
		constants.Flake8,
		constants.GolangciLint,
	}
	if len(pkgs) != len(expected) {
		t.Fatalf("expected %d MaybeInstallPackage calls, got %d: %v", len(expected), len(pkgs), pkgs)
	}
	for i, pkg := range expected {
		if pkgs[i] != pkg {
			t.Errorf("call[%d]: expected %q, got %q", i, pkg, pkgs[i])
		}
	}

	// gcc and xclip must NOT appear
	for _, pkg := range pkgs {
		if pkg == constants.Gcc {
			t.Errorf("gcc should not be installed on macOS")
		}
		if pkg == constants.Xclip {
			t.Errorf("xclip should not be installed on macOS")
		}
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

// TestInstallDeps_Linux verifies that on Linux, all deps including gcc and xclip are installed.
func TestInstallDeps_Linux(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false

	err := InstallDeps(mockApp.Base, mockApp.Cmd)
	if err != nil {
		t.Fatalf("InstallDeps on Linux should succeed, got: %v", err)
	}

	pkgs := mockApp.Cmd.MaybeInstalledPkgs
	expected := []string{
		constants.Make,
		constants.Gcc,
		constants.Ripgrep,
		constants.FdFind,
		constants.Unzip,
		constants.Xclip,
		constants.TreeSitterCli,
		constants.Markdownlint,
		constants.Flake8,
		constants.GolangciLint,
	}
	if len(pkgs) != len(expected) {
		t.Fatalf("expected %d MaybeInstallPackage calls, got %d: %v", len(expected), len(pkgs), pkgs)
	}
	for i, pkg := range expected {
		if pkgs[i] != pkg {
			t.Errorf("call[%d]: expected %q, got %q", i, pkg, pkgs[i])
		}
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

// TestInstallDeps_TreeSitterFallback_Linux verifies that when tree-sitter-cli primary install
// fails on Linux, npm fallback is attempted and nil is returned.
func TestInstallDeps_TreeSitterFallback_Linux(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false
	// primary tree-sitter-cli install fails
	mockApp.Cmd.MaybeInstallErrors[constants.TreeSitterCli] = errors.New("not found in apt")
	// npm fallback succeeds
	mockApp.Base.SetExecCommandResult("", "", nil)

	err := InstallDeps(mockApp.Base, mockApp.Cmd)
	if err != nil {
		t.Fatalf("InstallDeps should return nil even when tree-sitter primary fails, got: %v", err)
	}

	// Verify npm was called as fallback
	calls := mockApp.Base.ExecCommandCalls
	if len(calls) == 0 {
		t.Fatal("expected ExecCommand to be called for npm fallback")
	}
	if calls[0].Command != "npm" {
		t.Errorf("expected npm command for fallback, got %q", calls[0].Command)
	}
	if len(calls[0].Args) < 3 || calls[0].Args[0] != "install" || calls[0].Args[1] != "-g" || calls[0].Args[2] != "tree-sitter-cli" {
		t.Errorf("expected npm install -g tree-sitter-cli, got args: %v", calls[0].Args)
	}
	// Do NOT call VerifyNoRealCommands here — ExecCommand was intentionally used for npm fallback
}

// TestInstallDeps_TreeSitterBothFail verifies that when both primary and npm fallback
// fail for tree-sitter-cli, InstallDeps still returns nil (warn-and-continue).
func TestInstallDeps_TreeSitterBothFail(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false
	// primary tree-sitter-cli install fails
	mockApp.Cmd.MaybeInstallErrors[constants.TreeSitterCli] = errors.New("not found in apt")
	// npm fallback also fails
	mockApp.Base.SetExecCommandResult("", "npm error", errors.New("npm not found"))

	err := InstallDeps(mockApp.Base, mockApp.Cmd)
	if err != nil {
		t.Fatalf("InstallDeps should return nil when both tree-sitter paths fail, got: %v", err)
	}

	// npm was still attempted
	if len(mockApp.Base.ExecCommandCalls) == 0 {
		t.Fatal("expected ExecCommand to be called for npm fallback attempt")
	}
}

// TestInstallDeps_MarkdownlintFallback_Linux verifies that when markdownlint-cli primary
// install fails, npm fallback is attempted with the correct package name.
func TestInstallDeps_MarkdownlintFallback_Linux(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false
	mockApp.Cmd.MaybeInstallErrors[constants.Markdownlint] = errors.New("not found in apt")
	mockApp.Base.SetExecCommandResult("", "", nil)

	err := InstallDeps(mockApp.Base, mockApp.Cmd)
	if err != nil {
		t.Fatalf("InstallDeps should return nil when markdownlint primary fails, got: %v", err)
	}

	// Look for the npm install -g markdownlint-cli call
	found := false
	for _, call := range mockApp.Base.ExecCommandCalls {
		if call.Command == "npm" && len(call.Args) >= 3 &&
			call.Args[0] == "install" && call.Args[1] == "-g" && call.Args[2] == "markdownlint-cli" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected npm install -g markdownlint-cli call, got: %v", mockApp.Base.ExecCommandCalls)
	}
}

// TestInstallDeps_GolangciLintFallback_Linux verifies that when golangci-lint primary
// install fails, `go install` fallback is attempted with the correct module path.
func TestInstallDeps_GolangciLintFallback_Linux(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false
	mockApp.Cmd.MaybeInstallErrors[constants.GolangciLint] = errors.New("not found in apt")
	mockApp.Base.SetExecCommandResult("", "", nil)

	err := InstallDeps(mockApp.Base, mockApp.Cmd)
	if err != nil {
		t.Fatalf("InstallDeps should return nil when golangci-lint primary fails, got: %v", err)
	}

	// Look for the go install call
	found := false
	for _, call := range mockApp.Base.ExecCommandCalls {
		if call.Command == "go" && len(call.Args) >= 2 &&
			call.Args[0] == "install" &&
			call.Args[1] == "github.com/golangci/golangci-lint/cmd/golangci-lint@latest" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`, got: %v", mockApp.Base.ExecCommandCalls)
	}
}

// TestInstallDeps_Flake8Fails verifies that when flake8 install fails, InstallDeps
// still returns nil (soft install — warn and continue).
func TestInstallDeps_Flake8Fails(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false
	mockApp.Cmd.MaybeInstallErrors[constants.Flake8] = errors.New("flake8 install failed")

	err := InstallDeps(mockApp.Base, mockApp.Cmd)
	if err != nil {
		t.Fatalf("InstallDeps should return nil when flake8 fails (soft install), got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

// TestInstallDeps_MakeFails verifies that when make install fails, error is returned immediately
// and subsequent deps (gcc, etc.) are not attempted.
func TestInstallDeps_MakeFails(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false
	makeErr := errors.New("make install failed")
	mockApp.Cmd.MaybeInstallErrors[constants.Make] = makeErr

	err := InstallDeps(mockApp.Base, mockApp.Cmd)
	if err == nil {
		t.Fatal("expected error when make install fails")
	}

	// Only make should have been attempted
	if len(mockApp.Cmd.MaybeInstalledPkgs) != 1 {
		t.Errorf("expected only 1 MaybeInstall call (make), got %d: %v",
			len(mockApp.Cmd.MaybeInstalledPkgs), mockApp.Cmd.MaybeInstalledPkgs)
	}
	if mockApp.Cmd.MaybeInstalledPkgs[0] != constants.Make {
		t.Errorf("expected first call to be make, got %q", mockApp.Cmd.MaybeInstalledPkgs[0])
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

// TestInstallDeps_RipgrepFails verifies that when ripgrep fails, error is returned.
func TestInstallDeps_RipgrepFails(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false
	mockApp.Cmd.MaybeInstallErrors[constants.Ripgrep] = errors.New("ripgrep install failed")

	err := InstallDeps(mockApp.Base, mockApp.Cmd)
	if err == nil {
		t.Fatal("expected error when ripgrep install fails")
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
