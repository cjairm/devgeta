package worktree

import (
	"strings"
	"testing"
)

// Test paths for the launch forms. They are fake on purpose: these are pure
// string builders, so nothing here touches the filesystem or a real binary
// (resolveShell's tests in layout_test.go are the only ones that need fixtures).
const (
	testClaudePath   = "/Users/dev/.local/bin/claude"
	testOpenCodePath = "/opt/homebrew/bin/opencode"
	testNvimPath     = "/opt/homebrew/bin/nvim"
	testShell        = "/bin/zsh"
)

// --- the exec'd launch forms, one case per pane kind ---

// TestExecLaunchFormPerPaneKind pins the rendered command for every pane kind
// devgeta creates, since each one has a DIFFERENT argument shape and they are
// deliberately not unified: claude takes its prompt positionally, opencode takes
// --prompt, the reviewer adds --agent before it, nvim takes nothing, and a shell
// pane has no command at all.
//
// This is the regression net for the whole file: a builder that silently drops
// --agent, or that turns the nvim pane into `nvim '<prompt>'` (which opens a
// file by that name), fails here rather than in a tmux pane.
func TestExecLaunchFormPerPaneKind(t *testing.T) {
	const prompt = "fix issue 1082"

	tests := []struct {
		name   string
		launch paneLaunch
		want   string
	}{
		{
			name:   "nvim takes no arguments",
			launch: binaryLaunch(testNvimPath),
			want:   `'/opt/homebrew/bin/nvim'`,
		},
		{
			name:   "claude with no prompt still carries the no-flicker env",
			launch: (&ClaudeCoder{}).execLaunch(testClaudePath, ""),
			want:   `CLAUDE_CODE_NO_FLICKER=1 '/Users/dev/.local/bin/claude'`,
		},
		{
			name:   "claude takes the prompt positionally",
			launch: (&ClaudeCoder{}).execLaunch(testClaudePath, prompt),
			want:   `CLAUDE_CODE_NO_FLICKER=1 '/Users/dev/.local/bin/claude' 'fix issue 1082'`,
		},
		{
			name:   "opencode with no prompt",
			launch: (&OpenCodeCoder{}).execLaunch(testOpenCodePath, ""),
			want:   `'/opt/homebrew/bin/opencode'`,
		},
		{
			name:   "opencode takes --prompt",
			launch: (&OpenCodeCoder{}).execLaunch(testOpenCodePath, prompt),
			want:   `'/opt/homebrew/bin/opencode' '--prompt' 'fix issue 1082'`,
		},
		{
			name: "reviewer pins an agent before the prompt",
			launch: (&OpenCodeCoder{}).execLaunchWithAgent(
				testOpenCodePath,
				"code-reviewer",
				ReviewPrompt,
			),
			want: `'/opt/homebrew/bin/opencode' '--agent' 'code-reviewer' ` +
				`'--prompt' 'Review this branch against the default branch.'`,
		},
		{
			name:   "a shell pane has no command at all",
			launch: paneLaunch{},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.launch.render(); got != tt.want {
				t.Errorf("render() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAliasLaunchLeavesTheProgramUnquoted is the counterpart trap to the binary
// form above, and it is the one a mocked test would otherwise accept while the
// real thing broke: a shell does NOT expand an alias that was quoted, so
// `'cc' 'text'` fails with "command not found: cc" where `cc 'text'` works.
// Arguments are still quoted in this form.
func TestAliasLaunchLeavesTheProgramUnquoted(t *testing.T) {
	tests := []struct {
		name   string
		launch paneLaunch
		want   string
	}{
		{"claude alias, no prompt", (&ClaudeCoder{}).interactiveLaunch(""), "cc"},
		{"claude alias with prompt", (&ClaudeCoder{}).interactiveLaunch("fix it"), `cc 'fix it'`},
		{"opencode alias, no prompt", (&OpenCodeCoder{}).interactiveLaunch(""), "oc"},
		{
			"opencode alias with prompt",
			(&OpenCodeCoder{}).interactiveLaunch("fix it"),
			`oc '--prompt' 'fix it'`,
		},
		{
			"opencode alias with a reviewer agent",
			(&OpenCodeCoder{}).interactiveLaunchWithAgent("skill-reviewer", "fix it"),
			`oc '--agent' 'skill-reviewer' '--prompt' 'fix it'`,
		},
		// nvim has no alias - it is a bare binary name - but it goes through the
		// same form, and for the same reason: the shell has to resolve it.
		{"nvim as a bare name", aliasLaunch(nvimCommand), "nvim"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.launch.render()
			if got != tt.want {
				t.Errorf("render() = %q, want %q", got, tt.want)
			}
			if strings.HasPrefix(got, "'") {
				t.Errorf(
					"alias-form program must not be quoted "+
						"(quoting suppresses alias expansion), got %q",
					got,
				)
			}
		})
	}
}

// TestBinaryLaunchQuotesAPathWithASpace covers the case the "begins with /"
// shape check happily accepts and that would then split into two words:
// a resolved path inside a home directory whose name contains a space.
func TestBinaryLaunchQuotesAPathWithASpace(t *testing.T) {
	path := "/Users/Jane Doe/.local/bin/claude"

	got := (&ClaudeCoder{}).execLaunch(path, "hi").render()
	want := `CLAUDE_CODE_NO_FLICKER=1 '/Users/Jane Doe/.local/bin/claude' 'hi'`
	if got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

// TestLaunchQuotesEmbeddedSingleQuotes checks the close/escape/reopen escape
// reaches both the program and the arguments of a launch, not just whichever one
// a caller remembered.
func TestLaunchQuotesEmbeddedSingleQuotes(t *testing.T) {
	got := binaryLaunch("/Users/o'brien/bin/claude", "it's broken").render()
	want := `'/Users/o'\''brien/bin/claude' 'it'\''s broken'`
	if got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

// --- recipe 1: the resolved-path pane command ---

// TestExecPaneCommandAppendsTrailingExec pins the whole recipe. The trailing
// exec is what keeps today's pane lifetime: without it the pane closes when the
// coder quits, and the last pane closing takes the window with it (ADR-0020
// part 2).
func TestExecPaneCommandAppendsTrailingExec(t *testing.T) {
	launch := (&ClaudeCoder{}).execLaunch(testClaudePath, "fix issue 1082")

	got := execPaneCommand(launch, testShell)
	want := `CLAUDE_CODE_NO_FLICKER=1 '/Users/dev/.local/bin/claude' 'fix issue 1082'; exec '/bin/zsh'`
	if got != want {
		t.Errorf("execPaneCommand() = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, "; exec "+shellSingleQuote(testShell)) {
		t.Errorf("recipe must end in the trailing exec, got %q", got)
	}
}

// TestExecPaneCommandQuotesAShellWithASpace covers the shell interpolation site
// of this recipe. $SHELL and tmux's default-shell are both values devgeta does
// not control, so neither site may assume a space-free path.
func TestExecPaneCommandQuotesAShellWithASpace(t *testing.T) {
	got := execPaneCommand(binaryLaunch(testNvimPath), "/opt/my shells/zsh")
	want := `'/opt/homebrew/bin/nvim'; exec '/opt/my shells/zsh'`
	if got != want {
		t.Errorf("execPaneCommand() = %q, want %q", got, want)
	}
}

// TestExecPaneCommandOnAnEmptyLaunchIsEmpty: a shell pane must stay a shell
// pane. Building "; exec '<shell>'" around nothing would run an empty command
// first, and passing no command at all is what gives the pane the shell tmux
// would have started anyway - today's behavior exactly.
func TestExecPaneCommandOnAnEmptyLaunchIsEmpty(t *testing.T) {
	if got := execPaneCommand(paneLaunch{}, testShell); got != "" {
		t.Errorf("execPaneCommand(empty launch) = %q, want \"\"", got)
	}
}

// --- recipe 2: the interactive fallback / --pane pane command ---

// TestInteractivePaneCommandWrapsScriptInTheUserShell pins the fallback recipe
// exactly, for both of the scripts that land in it: a devgeta alias-form launch
// (used when the preflight probe resolved no absolute path) and a user-authored
// --pane command line.
func TestInteractivePaneCommandWrapsScriptInTheUserShell(t *testing.T) {
	tests := []struct {
		name   string
		script string
		shell  string
		want   string
	}{
		{
			name:   "alias-form fallback for a coder pane",
			script: (&ClaudeCoder{}).interactiveLaunch("fix it").render(),
			shell:  testShell,
			want:   `'/bin/zsh' -ic 'cc '\''fix it'\''; exec '\''/bin/zsh'\'' -i'`,
		},
		{
			name:   "a compound --pane value keeps both commands",
			script: "cd api && make dev",
			shell:  testShell,
			want:   `'/bin/zsh' -ic 'cd api && make dev; exec '\''/bin/zsh'\'' -i'`,
		},
		{
			// The value stays unquoted WITHIN the script (it is a command
			// line), so its own quote has to survive the wrapper's quoting.
			// Measured in ADR-0020: naive embedding ends the -ic wrapper early.
			name:   "a --pane value containing a single quote",
			script: `printf %s "it's fine"`,
			shell:  testShell,
			want:   `'/bin/zsh' -ic 'printf %s "it'\''s fine"; exec '\''/bin/zsh'\'' -i'`,
		},
		{
			name:   "a shell path containing a space, quoted at both sites",
			script: "make dev",
			shell:  "/opt/my shells/zsh",
			want:   `'/opt/my shells/zsh' -ic 'make dev; exec '\''/opt/my shells/zsh'\'' -i'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interactivePaneCommand(tt.script, tt.shell)
			if got != tt.want {
				t.Errorf("interactivePaneCommand() = %q, want %q", got, tt.want)
			}
			// The trailing exec belongs INSIDE the wrapper's quoting, so the
			// command ends with the quote that closes it.
			if !strings.HasSuffix(got, ` -i'`) {
				t.Errorf("recipe must end in the trailing exec inside the wrapper, got %q", got)
			}
		})
	}
}

// TestInteractivePaneCommandIsTheSameShellForm is the assertion a "does it
// contain exec?" check would let through. The nested construction
// (`<shell> -ic '<script>'; exec <shell> -i`) runs the script in a child that
// then exits, so a `cd` inside it is lost - measured, and `--pane 'cd api &&
// make dev'` is the documented headline use of that flag, so the nested form
// would break it while still appearing to work.
func TestInteractivePaneCommandIsTheSameShellForm(t *testing.T) {
	const script = "cd api && make dev"
	quotedShell := shellSingleQuote(testShell)

	got := interactivePaneCommand(script, testShell)

	nested := quotedShell + " -ic " + shellSingleQuote(script) + "; exec " + quotedShell + " -i"
	if got == nested {
		t.Fatalf("built the NESTED form, which loses a cd from the script: %q", got)
	}

	// Positively: everything after -ic is ONE quoted word containing both the
	// script and the exec, so both run in the same shell invocation.
	wrapper := quotedShell + " -ic "
	if !strings.HasPrefix(got, wrapper) {
		t.Fatalf("expected the command to start with %q, got %q", wrapper, got)
	}
	inner := strings.TrimPrefix(got, wrapper)
	if !strings.HasPrefix(inner, "'") || !strings.HasSuffix(inner, "'") {
		t.Fatalf("expected the script and its exec inside one quoted word, got %q", inner)
	}
	if strings.Contains(inner[1:len(inner)-1], `'; exec`) {
		t.Errorf("the trailing exec escaped the wrapper's quoting: %q", got)
	}
}

// TestInteractivePaneCommandOnABlankScriptIsEmpty mirrors
// TestExecPaneCommandOnAnEmptyLaunchIsEmpty: with nothing to run, the pane gets
// no command rather than a wrapper around an empty script (which would run
// `; exec ...` as its first statement).
func TestInteractivePaneCommandOnABlankScriptIsEmpty(t *testing.T) {
	for _, script := range []string{"", "   ", "\t\n"} {
		if got := interactivePaneCommand(script, testShell); got != "" {
			t.Errorf("interactivePaneCommand(%q) = %q, want \"\"", script, got)
		}
	}
}
