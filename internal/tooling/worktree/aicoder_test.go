package worktree

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
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

// --- PromptCommand ---

// PromptCommand's form is per-coder and deliberately NOT unified: claude takes
// its prompt positionally, opencode takes --prompt (verified against the
// installed binaries, 2026-07-31; see ADR-0011). These assert the exact strings
// so a "cleanup" that unifies them fails here.
//
// opencode's flags are quoted (`oc '--prompt' '<text>'`) since PromptCommand
// started rendering the structured launch, which quotes every argument the same
// way rather than exempting anything that looks like a flag - see
// paneLaunch.render. Same command to the shell; the bytes changed.
func TestPromptCommandExactFormPerCoder(t *testing.T) {
	tests := []struct {
		name   string
		coder  AICoder
		prompt string
		want   string
	}{
		{"opencode uses --prompt", &OpenCodeCoder{}, "fix the bug", "oc '--prompt' 'fix the bug'"},
		{"claude takes the prompt positionally", &ClaudeCoder{}, "fix the bug", "cc 'fix the bug'"},
		{
			"opencode quotes an embedded single quote",
			&OpenCodeCoder{},
			"it's broken",
			`oc '--prompt' 'it'\''s broken'`,
		},
		{
			"claude quotes an embedded single quote",
			&ClaudeCoder{},
			"it's broken",
			`cc 'it'\''s broken'`,
		},
		{
			"shell metacharacters stay inert",
			&ClaudeCoder{},
			"$(rm -rf /); echo hi",
			`cc '$(rm -rf /); echo hi'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.coder.PromptCommand(tt.prompt); got != tt.want {
				t.Errorf("PromptCommand(%q) = %q, want %q", tt.prompt, got, tt.want)
			}
		})
	}
}

// promptCommandWithAgent is the one author of opencode's launch-with-a-prompt
// form, shared by PromptCommand (no agent) and layout.go's ReviewCommand (with
// one). This pins both branches so the shared helper can't drift from either
// caller's needs.
func TestOpenCodePromptCommandWithAgent(t *testing.T) {
	coder := &OpenCodeCoder{}

	withAgent := coder.promptCommandWithAgent("code-reviewer", "review it")
	if want := "oc '--agent' 'code-reviewer' '--prompt' 'review it'"; withAgent != want {
		t.Errorf("got %q, want %q", withAgent, want)
	}

	// An empty agent must omit the flag entirely, not emit a bare "--agent".
	noAgent := coder.promptCommandWithAgent("", "review it")
	if want := "oc '--prompt' 'review it'"; noAgent != want {
		t.Errorf("got %q, want %q", noAgent, want)
	}
	if strings.Contains(noAgent, "--agent") {
		t.Errorf("expected no --agent flag with an empty agent, got %q", noAgent)
	}
}

// OpenCodeCoder/ClaudeCoder.EnsureInstalled route through the shared
// ensureToolInstalled helper, which resolves the launch token through the
// swappable commands.ShellCommandLookupFn (see setShellCommandExistsFn /
// setShellCommandLookupFn in repo_candidates_test.go) - the interactive-shell
// probe, not exec.LookPath - so found, not-found, and inconclusive are all
// exercisable here without spawning a real shell. The check targets the
// alias the pane will launch (oc/cc), not the underlying binary; only the
// error message names the binary.

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

// An inconclusive probe — the interactive shell didn't answer within the
// deadline, or couldn't run — must NOT block the coder (ADR-0016): only a
// probe that proved the tool absent may. This is the regression test for the
// bug where a machine with slow shell startup turned every `dg ws` create
// into a false "opencode is not installed".
func TestEnsureInstalledInconclusiveProbeFailsOpen(t *testing.T) {
	setShellCommandLookupFn(t, func(string) commands.ShellLookupResult {
		return commands.ShellLookupInconclusive
	})

	if err := (&OpenCodeCoder{}).EnsureInstalled(); err != nil {
		t.Errorf("expected an inconclusive probe to fail open for opencode, got %v", err)
	}
	if err := (&ClaudeCoder{}).EnsureInstalled(); err != nil {
		t.Errorf("expected an inconclusive probe to fail open for claude, got %v", err)
	}
}
