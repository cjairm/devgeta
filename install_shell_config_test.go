package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// install.sh is the zero-dependency installer users pipe into bash before
// devgeta exists on their machine, so nothing in it can be exercised through
// Go. These tests run the real shell functions instead of asserting on the
// file's text: each one is lifted out of install.sh by name and sourced into
// a bash harness with a throwaway $HOME, which is why renaming a function
// fails the test loudly rather than silently stopping to test anything.
//
// What they pin is the bug that shipped through v1.23.0: the installer wrote
// a bare `source $HOME/.local/share/devgeta/devgeta.zsh` into the user's
// shell config, but that file is generated later by `dg install`. Every shell
// opened in between started with
//
//	.zshrc:source:4: no such file or directory: …/devgeta.zsh

// extractShellFunction returns the definition of a top-level function from
// install.sh: the line `name() {` through the closing `}` in column 0.
func extractShellFunction(t *testing.T, script, name string) string {
	t.Helper()
	start := name + "() {"
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		if line != start {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if lines[j] == "}" {
				return strings.Join(lines[i:j+1], "\n")
			}
		}
		t.Fatalf("function %s in install.sh has no closing brace in column 0", name)
	}
	t.Fatalf("function %s is not defined in install.sh (renamed or removed?)", name)
	return ""
}

// extractShellAssignment returns the whole `NAME=…` line from install.sh, so
// the harness runs against the real value rather than a copy of it that can
// drift.
func extractShellAssignment(t *testing.T, script, name string) string {
	t.Helper()
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, name+"=") {
			return line
		}
	}
	t.Fatalf("assignment %s= is not present in install.sh (renamed or removed?)", name)
	return ""
}

func readInstallScript(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatalf("failed to read install.sh: %v", err)
	}
	return string(body)
}

// runInstallHarness sources the named install.sh pieces plus body into bash,
// with HOME pointed at home so nothing touches the real one.
func runInstallHarness(t *testing.T, home string, pieces []string, body string) string {
	t.Helper()

	harness := strings.Join(append([]string{
		"set -u",
		// install.sh's own reporting helpers; the functions under test call
		// them and would otherwise abort with "command not found".
		"print_info() { :; }",
		"print_success() { :; }",
		// Not silenced: a repair that gives up must be visible in the output
		// the assertions read.
		`print_error() { echo "ERROR: $*"; }`,
	}, append(pieces, body)...), "\n")

	dir := t.TempDir()
	path := filepath.Join(dir, "harness.sh")
	if err := os.WriteFile(path, []byte(harness), 0o600); err != nil {
		t.Fatalf("failed to write harness: %v", err)
	}

	cmd := exec.Command("bash", path)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	return string(out)
}

// TestInstallerShellBlockSurvivesAMissingDevgetaZsh is the regression test for
// the reported failure: the block the installer writes must load cleanly
// before `dg install` has generated devgeta.zsh — no error, and no non-zero
// status left behind for the user's first prompt to render as a failure.
func TestInstallerShellBlockSurvivesAMissingDevgetaZsh(t *testing.T) {
	script := readInstallScript(t)
	pieces := []string{
		extractShellAssignment(t, script, "PATH_EXPORT"),
		extractShellAssignment(t, script, "ALIAS_EXPORT"),
		extractShellAssignment(t, script, "DEVGETA_SHELL_CONFIG"),
		extractShellFunction(t, script, "write_devgeta_block"),
	}
	home := t.TempDir()

	out := runInstallHarness(t, home, pieces, strings.Join([]string{
		`write_devgeta_block > "$HOME/.zshrc"`,
		// A subshell so a stray `exit` in the block cannot fake success.
		`( . "$HOME/.zshrc" ) 2>&1`,
		`echo "status=$?"`,
	}, "\n"))

	if !strings.Contains(out, "status=0") {
		t.Errorf("sourcing the installer block with no devgeta.zsh must succeed, got:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "no such file") {
		t.Errorf("installer block errors when devgeta.zsh is absent:\n%s", out)
	}
}

// TestInstallerShellBlockSourcesDevgetaZshWhenPresent is the other half: the
// guard must not be so defensive that the config never loads once it exists.
func TestInstallerShellBlockSourcesDevgetaZshWhenPresent(t *testing.T) {
	script := readInstallScript(t)
	pieces := []string{
		extractShellAssignment(t, script, "PATH_EXPORT"),
		extractShellAssignment(t, script, "ALIAS_EXPORT"),
		extractShellAssignment(t, script, "DEVGETA_SHELL_CONFIG"),
		extractShellFunction(t, script, "write_devgeta_block"),
	}
	home := t.TempDir()
	shellDir := filepath.Join(home, ".local", "share", "devgeta")
	if err := os.MkdirAll(shellDir, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", shellDir, err)
	}
	marker := "devgeta-zsh-was-sourced"
	if err := os.WriteFile(
		filepath.Join(shellDir, "devgeta.zsh"),
		[]byte(fmt.Sprintf("echo %s\n", marker)),
		0o600,
	); err != nil {
		t.Fatalf("failed to write devgeta.zsh: %v", err)
	}

	out := runInstallHarness(t, home, pieces, strings.Join([]string{
		`write_devgeta_block > "$HOME/.zshrc"`,
		`. "$HOME/.zshrc"`,
	}, "\n"))

	if !strings.Contains(out, marker) {
		t.Errorf("installer block did not source an existing devgeta.zsh, got:\n%s", out)
	}
}

// TestInstallerRepairsAnUnguardedSourceLine covers the users who already ran
// an older installer. Re-running install.sh must fix their config: the
// "already configured" branch skips writing the block, so the repair is the
// only path that reaches them.
func TestInstallerRepairsAnUnguardedSourceLine(t *testing.T) {
	script := readInstallScript(t)
	pieces := []string{
		extractShellAssignment(t, script, "DEVGETA_SHELL_CONFIG"),
		extractShellFunction(t, script, "repair_unguarded_source_line"),
	}
	home := t.TempDir()
	zshrc := filepath.Join(home, ".zshrc")
	// Both forms an older installer could have written — it expanded $HOME at
	// install time, but a hand-copied config can carry the literal.
	original := strings.Join([]string{
		"# Added by devgeta installer",
		`export PATH="$HOME/.local/bin:$PATH"`,
		"alias dg='devgeta'",
		"source " + home + "/.local/share/devgeta/devgeta.zsh",
		`source $HOME/.local/share/devgeta/devgeta.zsh`,
		"# a line the user added afterwards",
		"export EDITOR=nvim",
	}, "\n") + "\n"
	if err := os.WriteFile(zshrc, []byte(original), 0o600); err != nil {
		t.Fatalf("failed to write .zshrc: %v", err)
	}

	out := runInstallHarness(t, home, pieces, strings.Join([]string{
		`repair_unguarded_source_line "$HOME/.zshrc"`,
		// Twice: the repair has to be idempotent, since install.sh runs it on
		// every re-install.
		`repair_unguarded_source_line "$HOME/.zshrc"`,
		`( . "$HOME/.zshrc" ) 2>&1`,
		`echo "status=$?"`,
	}, "\n"))

	if !strings.Contains(out, "status=0") {
		t.Errorf("repaired .zshrc still fails to load:\n%s", out)
	}
	if strings.Contains(out, "ERROR:") {
		t.Errorf("repair reported a failure:\n%s", out)
	}

	repaired, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatalf("failed to read repaired .zshrc: %v", err)
	}
	got := string(repaired)
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "source ") &&
			strings.Contains(line, "devgeta.zsh") {
			t.Errorf("unguarded source line survived the repair: %q\n%s", line, got)
		}
	}
	if !strings.Contains(got, "export EDITOR=nvim") {
		t.Errorf("repair dropped the user's own lines:\n%s", got)
	}
	if strings.Count(got, "devgeta.zsh") != 4 {
		t.Errorf(
			"expected both old lines replaced by a two-mention guard each, got:\n%s",
			got,
		)
	}
}

// TestInstallerRepairLeavesUnrelatedSourceLinesAlone keeps the repair from
// growing into a rewrite of anything that mentions devgeta.zsh — a user who
// sources their own copy from another path must keep it.
func TestInstallerRepairLeavesUnrelatedSourceLinesAlone(t *testing.T) {
	script := readInstallScript(t)
	pieces := []string{
		extractShellAssignment(t, script, "DEVGETA_SHELL_CONFIG"),
		extractShellFunction(t, script, "repair_unguarded_source_line"),
	}
	home := t.TempDir()
	zshrc := filepath.Join(home, ".zshrc")
	original := "source $HOME/dotfiles/devgeta.zsh\n"
	if err := os.WriteFile(zshrc, []byte(original), 0o600); err != nil {
		t.Fatalf("failed to write .zshrc: %v", err)
	}

	runInstallHarness(t, home, pieces, `repair_unguarded_source_line "$HOME/.zshrc"`)

	after, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatalf("failed to read .zshrc: %v", err)
	}
	if string(after) != original {
		t.Errorf("repair rewrote a line it does not own:\nbefore: %q\nafter:  %q", original, after)
	}
}
