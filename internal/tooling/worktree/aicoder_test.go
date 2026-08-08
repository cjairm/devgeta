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

// Command() is the TYPED form - what devgeta send-keys into a pane that already
// exists - and it is the UN-ALIASED command: the binary, plus the recipe's env
// prefix. Not "oc"/"cc" (ADR-0020's 2026-08-07 amendment): preflight probes the
// binary, so typing the alias sent a live pane something the check never verified,
// and a user with the coder on PATH but no devgeta alias got `cc: command not
// found` after passing preflight.
//
// The literals are spelled out rather than read off constants.*Launch: this is the
// pin, and deriving it from the value under test could not fail.
func TestOpenCodeCoderCommand(t *testing.T) {
	coder := &OpenCodeCoder{}
	if coder.Command() != "opencode" {
		t.Errorf("expected command 'opencode', got %q", coder.Command())
	}
}

func TestClaudeCoderCommand(t *testing.T) {
	coder := &ClaudeCoder{}
	if want := "CLAUDE_CODE_NO_FLICKER=1 claude"; coder.Command() != want {
		t.Errorf("expected command %q, got %q", want, coder.Command())
	}
}

// TestCommandMatchesTheInteractiveLaunchForm pins the two representations of the
// typed form to each other. Command() reads the recipe's Command() while
// interactiveLaunch builds a structured launch from the recipe's parts, so this is
// the assertion that keeps those two derivations from drifting - a recipe whose
// value gains a space, or an env prefix rendered differently by one of them, shows
// up here.
func TestCommandMatchesTheInteractiveLaunchForm(t *testing.T) {
	for _, coder := range []AICoder{&OpenCodeCoder{}, &ClaudeCoder{}} {
		if got, want := coder.interactiveLaunch("").render(), coder.Command(); got != want {
			t.Errorf("%s: interactiveLaunch(\"\").render() = %q, want Command()'s %q",
				coder.Name(), got, want)
		}
	}
}

// --- PromptCommand ---

// PromptCommand's form is per-coder and deliberately NOT unified: claude takes
// its prompt positionally, opencode takes --prompt (verified against the
// installed binaries, 2026-07-31; see ADR-0011). These assert the exact strings
// so a "cleanup" that unifies them fails here.
//
// opencode's flags are quoted (`opencode '--prompt' '<text>'`) since PromptCommand
// started rendering the structured launch, which quotes every argument the same
// way rather than exempting anything that looks like a flag - see
// paneLaunch.render. Same command to the shell; the bytes changed.
//
// The program word is the BINARY, not the cc/oc alias (ADR-0020's 2026-08-07
// amendment), and claude's carries the env prefix the alias definition used to
// supply.
func TestPromptCommandExactFormPerCoder(t *testing.T) {
	tests := []struct {
		name   string
		coder  AICoder
		prompt string
		want   string
	}{
		{
			"opencode uses --prompt",
			&OpenCodeCoder{},
			"fix the bug",
			"opencode '--prompt' 'fix the bug'",
		},
		{
			"claude takes the prompt positionally, after its env prefix",
			&ClaudeCoder{},
			"fix the bug",
			"CLAUDE_CODE_NO_FLICKER=1 claude 'fix the bug'",
		},
		{
			"opencode quotes an embedded single quote",
			&OpenCodeCoder{},
			"it's broken",
			`opencode '--prompt' 'it'\''s broken'`,
		},
		{
			"claude quotes an embedded single quote",
			&ClaudeCoder{},
			"it's broken",
			`CLAUDE_CODE_NO_FLICKER=1 claude 'it'\''s broken'`,
		},
		{
			"shell metacharacters stay inert",
			&ClaudeCoder{},
			"$(rm -rf /); echo hi",
			`CLAUDE_CODE_NO_FLICKER=1 claude '$(rm -rf /); echo hi'`,
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
// form, shared by PromptCommand (no agent) and layout.go's reviewCommandFor (with
// one). This pins both branches so the shared helper can't drift from either
// caller's needs.
func TestOpenCodePromptCommandWithAgent(t *testing.T) {
	coder := &OpenCodeCoder{}

	withAgent := coder.promptCommandWithAgent("code-reviewer", "review it")
	wantWithAgent := "opencode '--agent' 'code-reviewer' '--prompt' 'review it'"
	if withAgent != wantWithAgent {
		t.Errorf("got %q, want %q", withAgent, wantWithAgent)
	}

	// An empty agent must omit the flag entirely, not emit a bare "--agent".
	noAgent := coder.promptCommandWithAgent("", "review it")
	if want := "opencode '--prompt' 'review it'"; noAgent != want {
		t.Errorf("got %q, want %q", noAgent, want)
	}
	if strings.Contains(noAgent, "--agent") {
		t.Errorf("expected no --agent flag with an empty agent, got %q", noAgent)
	}
}

// OpenCodeCoder/ClaudeCoder.EnsureInstalled route through the shared
// ensureToolInstalled helper, which resolves the tool through the swappable
// commands.ShellCommandLookupFn (see setShellCommandExistsFn /
// setShellCommandLookupFn in repo_candidates_test.go) - the interactive-shell
// probe, not exec.LookPath - so found, not-found, and inconclusive are all
// exercisable here without spawning a real shell.
//
// The probed name is the BINARY (opencode/claude), not the oc/cc alias: a
// created pane execs the binary, and only a binary makes `command -v` answer
// with a path (ADR-0020 part 3, rule 1). A stub that answers for the alias
// instead would silently exercise the interactive fallback here rather than the
// exec path.

func TestOpenCodeCoderEnsureInstalledOK(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "opencode" })

	if _, err := (&OpenCodeCoder{}).EnsureInstalled(); err != nil {
		t.Fatalf("unexpected error when the opencode binary resolves in the shell: %v", err)
	}
}

// TestEnsureInstalledProbesTheBinaryNotTheAlias is the flip itself, asserted on
// the probe's INPUT rather than on its effects: an opencode/claude installed
// outside devgeta has no oc/cc alias in devgeta.zsh, and must still pass.
func TestEnsureInstalledProbesTheBinaryNotTheAlias(t *testing.T) {
	var probed []string
	setShellCommandLookupPathFn(t, func(name string) (string, commands.ShellLookupResult) {
		probed = append(probed, name)
		// Only the binaries resolve; the aliases do not exist at all here.
		switch name {
		case "opencode":
			return "/opt/homebrew/bin/opencode", commands.ShellLookupFound
		case "claude":
			return "/Users/dev/.local/bin/claude", commands.ShellLookupFound
		}
		return "", commands.ShellLookupNotFound
	})

	if _, err := (&OpenCodeCoder{}).EnsureInstalled(); err != nil {
		t.Errorf("opencode: unexpected error: %v", err)
	}
	if _, err := (&ClaudeCoder{}).EnsureInstalled(); err != nil {
		t.Errorf("claude: unexpected error: %v", err)
	}

	want := []string{"opencode", "claude"}
	if len(probed) != len(want) {
		t.Fatalf("probed %v, want %v", probed, want)
	}
	for i := range want {
		if probed[i] != want[i] {
			t.Errorf("probe %d asked for %q, want the binary %q", i, probed[i], want[i])
		}
	}
}

func TestOpenCodeCoderEnsureInstalledMissing(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	resolvedPath, err := (&OpenCodeCoder{}).EnsureInstalled()
	if err == nil {
		t.Fatal("expected error when opencode does not resolve in the shell, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "opencode") {
		t.Errorf("expected error to mention opencode, got %q", got)
	}
	// A failed check has nothing to launch, so it must not hand back a path a
	// caller could exec.
	if resolvedPath != "" {
		t.Errorf("expected no resolved path alongside the error, got %q", resolvedPath)
	}
}

func TestClaudeCoderEnsureInstalledOK(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "claude" })

	if _, err := (&ClaudeCoder{}).EnsureInstalled(); err != nil {
		t.Fatalf("unexpected error when the claude binary resolves in the shell: %v", err)
	}
}

func TestClaudeCoderEnsureInstalledMissing(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	resolvedPath, err := (&ClaudeCoder{}).EnsureInstalled()
	if err == nil {
		t.Fatal("expected error when claude does not resolve in the shell, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "claude") {
		t.Errorf("expected error to mention claude, got %q", got)
	}
	if resolvedPath != "" {
		t.Errorf("expected no resolved path alongside the error, got %q", resolvedPath)
	}
}

// TestEnsureInstalledReturnsTheProbesResolvedPath is the check-to-launch link
// ADR-0020 requires: the path the probe resolved must come BACK from the check,
// because that is the only way the pane can exec exactly what was verified
// without probing a second time.
func TestEnsureInstalledReturnsTheProbesResolvedPath(t *testing.T) {
	const claudePath = "/Users/dev/.local/bin/claude"
	const opencodePath = "/opt/homebrew/bin/opencode"

	setShellCommandLookupPathFn(t, func(name string) (string, commands.ShellLookupResult) {
		switch name {
		case "claude":
			return claudePath, commands.ShellLookupFound
		case "opencode":
			return opencodePath, commands.ShellLookupFound
		}
		return "", commands.ShellLookupNotFound
	})

	got, err := (&ClaudeCoder{}).EnsureInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != claudePath {
		t.Errorf("claude resolved path = %q, want %q", got, claudePath)
	}

	got, err = (&OpenCodeCoder{}).EnsureInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != opencodePath {
		t.Errorf("opencode resolved path = %q, want %q", got, opencodePath)
	}
}

// TestEnsureInstalledFoundWithNoPathIsNotAnError covers the outcome ADR-0020
// part 3 calls out as normal rather than exceptional: `command -v` answered, but
// with something that is not a path (alias text, a shell function or builtin
// name). That is "no path", never something to exec - and it must not fail the
// check, because the tool IS installed. The pane takes the interactive fallback.
func TestEnsureInstalledFoundWithNoPathIsNotAnError(t *testing.T) {
	setShellCommandLookupPathFn(t, func(string) (string, commands.ShellLookupResult) {
		return "", commands.ShellLookupFound
	})

	resolvedPath, err := (&OpenCodeCoder{}).EnsureInstalled()
	if err != nil {
		t.Fatalf("a Found outcome with no path must not error, got %v", err)
	}
	if resolvedPath != "" {
		t.Errorf("expected no resolved path, got %q", resolvedPath)
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

	// The path must come back empty as well as the error being nil: an
	// inconclusive probe resolved nothing, so anything non-empty here would be
	// fabricated, and the pane would exec it.
	if resolvedPath, err := (&OpenCodeCoder{}).EnsureInstalled(); err != nil {
		t.Errorf("expected an inconclusive probe to fail open for opencode, got %v", err)
	} else if resolvedPath != "" {
		t.Errorf("expected no resolved path from an inconclusive probe, got %q", resolvedPath)
	}
	if resolvedPath, err := (&ClaudeCoder{}).EnsureInstalled(); err != nil {
		t.Errorf("expected an inconclusive probe to fail open for claude, got %v", err)
	} else if resolvedPath != "" {
		t.Errorf("expected no resolved path from an inconclusive probe, got %q", resolvedPath)
	}
}
