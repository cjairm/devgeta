package worktree

import (
	"strings"
	"testing"
)

func TestResolveAICoder(t *testing.T) {
	tests := []struct {
		name     string
		alias    string
		wantName string
		wantErr  bool
	}{
		{"opencode full", "opencode", "opencode", false},
		{"opencode short", "oc", "opencode", false},
		{"claude full", "claude", "claude", false},
		{"claude short cc", "cc", "claude", false},
		{"claudecode", "claudecode", "claude", false},
		{"case insensitive OPENCODE", "OPENCODE", "opencode", false},
		{"case insensitive Claude", "Claude", "claude", false},
		{"case insensitive CC", "CC", "claude", false},
		{"unknown alias", "cursor", "", true},
		{"empty alias", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coder, err := ResolveAICoder(tt.alias)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if coder.Name() != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, coder.Name())
			}
		})
	}
}

// Command() returns the devgeta shell alias, not the raw binary, so the launch
// recipe stays defined once in devgeta.zsh (see the Command() doc comments).
func TestOpenCodeCoderCommand(t *testing.T) {
	coder := &OpenCodeCoder{}
	if coder.Command() != "oc" {
		t.Errorf("expected command 'oc', got %q", coder.Command())
	}
}

func TestClaudeCoderCommand(t *testing.T) {
	coder := &ClaudeCoder{}
	if coder.Command() != "cc" {
		t.Errorf("expected command 'cc', got %q", coder.Command())
	}
}

// OpenCodeCoder/ClaudeCoder.EnsureInstalled route through the shared
// ensureToolInstalled helper, which resolves the underlying binary through the
// swappable commands.ShellCommandExistsFn (see setShellCommandExistsFn in
// repo_candidates_test.go) - the interactive-shell probe, not exec.LookPath -
// so both the success and failure paths are exercisable here without spawning a
// real shell. The check targets the binary name (opencode/claude), not the
// alias, since that binary is the real dependency.

func TestOpenCodeCoderEnsureInstalledOK(t *testing.T) {
	// EnsureInstalled checks the launch token (the oc alias), not the raw binary.
	setShellCommandExistsFn(t, func(name string) bool { return name == "oc" })

	if err := (&OpenCodeCoder{}).EnsureInstalled(); err != nil {
		t.Fatalf("unexpected error when the oc alias resolves in the shell: %v", err)
	}
}

func TestOpenCodeCoderEnsureInstalledMissing(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	err := (&OpenCodeCoder{}).EnsureInstalled()
	if err == nil {
		t.Fatal("expected error when opencode does not resolve in the shell, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "opencode") {
		t.Errorf("expected error to mention opencode, got %q", got)
	}
}

func TestClaudeCoderEnsureInstalledOK(t *testing.T) {
	// EnsureInstalled checks the launch token (the cc alias), not the raw binary.
	setShellCommandExistsFn(t, func(name string) bool { return name == "cc" })

	if err := (&ClaudeCoder{}).EnsureInstalled(); err != nil {
		t.Fatalf("unexpected error when the cc alias resolves in the shell: %v", err)
	}
}

func TestClaudeCoderEnsureInstalledMissing(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	err := (&ClaudeCoder{}).EnsureInstalled()
	if err == nil {
		t.Fatal("expected error when claude does not resolve in the shell, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "claude") {
		t.Errorf("expected error to mention claude, got %q", got)
	}
}
