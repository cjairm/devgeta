package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/constants"
)

// Test paths for the launch forms. They are fake on purpose: these are pure
// string builders, so most of this file touches no filesystem and no real
// binary. The two real-shell round-trip tests at the bottom are the exception,
// and they build their own fixtures under t.TempDir().
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
			// Through nvim's PRODUCTION builder, not a hand-built launch: that
			// is what makes this row the stated regression net. A builder that
			// started passing the prompt through - turning the editor pane into
			// `nvim 'fix issue 1082'`, which opens a file by that name - now
			// fails here, which a locally constructed paneLaunch could never
			// catch. The prompt is passed in deliberately for that reason.
			name:   "nvim takes no arguments, prompt or not",
			launch: nvimExecLaunch(testNvimPath, prompt),
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

// TestNameLaunchIsTheUnaliasedCommandAndLeavesTheProgramUnquoted is the
// counterpart to the binary form above. Two properties, both deliberate:
//
//   - The program is the BINARY name, never the cc/oc alias (ADR-0020's
//     2026-08-07 amendment). These are the strings devgeta send-keys into a pane
//     that already exists, so an alias here would send a live pane something
//     preflight never checked.
//   - The program word is left unquoted. That no longer BREAKS a plain binary
//     name (`'claude'` still resolves on PATH), but this form is what a probe
//     answering with alias text or a shell-function name selects, and in that
//     case the user's OWN definition of the name has to expand - which a quoted
//     program word suppresses. Arguments stay quoted either way.
func TestNameLaunchIsTheUnaliasedCommandAndLeavesTheProgramUnquoted(t *testing.T) {
	tests := []struct {
		name   string
		launch paneLaunch
		want   string
	}{
		{
			"claude, no prompt, env prefix spelled out",
			(&ClaudeCoder{}).interactiveLaunch(""),
			"CLAUDE_CODE_NO_FLICKER=1 claude",
		},
		{
			"claude with prompt",
			(&ClaudeCoder{}).interactiveLaunch("fix it"),
			`CLAUDE_CODE_NO_FLICKER=1 claude 'fix it'`,
		},
		{"opencode, no prompt", (&OpenCodeCoder{}).interactiveLaunch(""), "opencode"},
		{
			"opencode with prompt",
			(&OpenCodeCoder{}).interactiveLaunch("fix it"),
			`opencode '--prompt' 'fix it'`,
		},
		{
			"opencode with a reviewer agent",
			(&OpenCodeCoder{}).interactiveLaunchWithAgent("skill-reviewer", "fix it"),
			`opencode '--agent' 'skill-reviewer' '--prompt' 'fix it'`,
		},
		// nvim goes through the same form, for the same reason: the shell running
		// it has to resolve the name itself.
		{"nvim as a bare name", nameLaunch(nvimCommand), "nvim"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.launch.render()
			if got != tt.want {
				t.Errorf("render() = %q, want %q", got, tt.want)
			}
			if strings.HasPrefix(got, "'") {
				t.Errorf(
					"name-form program (or its env prefix) must not be quoted, got %q",
					got,
				)
			}
			for _, alias := range []string{"cc ", "oc "} {
				if strings.HasPrefix(got, alias) || got == strings.TrimSpace(alias) {
					t.Errorf("the typed form must name the binary, not the %q alias: %q",
						strings.TrimSpace(alias), got)
				}
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

// TestALaunchWithArgumentsButNoProgramDoesNotVanish is the regression net for
// the one way this cycle's original bug could come back through this file: a
// launch built from a resolution that handed over an EMPTY path, with a
// non-empty prompt.
//
// While "empty" meant `program == ""`, such a launch reported itself empty, so
// render() and the recipe both returned "" - the pane became a bare shell, the
// prompt was gone, and nothing errored or logged. Emptiness is now the zero
// KIND, so a constructed launch is never empty: it renders, and the pane reports
// a command it could not run with the prompt visible beside it.
func TestALaunchWithArgumentsButNoProgramDoesNotVanish(t *testing.T) {
	const prompt = "fix issue 1082"

	launches := []struct {
		name   string
		launch paneLaunch
	}{
		{"binary launch with no path", binaryLaunch("", prompt)},
		{
			"binary launch with an env prefix and no path",
			binaryLaunchWithEnv(constants.ClaudeLaunch.EnvPrefix, "", prompt),
		},
		{"claude's exec form with no path", (&ClaudeCoder{}).execLaunch("", prompt)},
		{"opencode's exec form with no path", (&OpenCodeCoder{}).execLaunch("", prompt)},
		{"name launch with no program", nameLaunch("", prompt)},
	}

	for _, tt := range launches {
		t.Run(tt.name, func(t *testing.T) {
			if tt.launch.isEmpty() {
				t.Fatalf("a launch carrying %q reported itself empty", prompt)
			}
			rendered := tt.launch.render()
			if rendered == "" {
				t.Fatalf("render() = \"\", so the prompt %q was silently dropped", prompt)
			}
			if !strings.Contains(rendered, prompt) {
				t.Errorf(
					"render() = %q, expected it to still carry the prompt %q",
					rendered,
					prompt,
				)
			}
			// The pane command must fail visibly rather than come back as "no
			// command", which tmux reads as "just start a shell".
			if got := paneCommandFor(tt.launch, testShell); got == "" {
				t.Errorf("paneCommandFor() = \"\", so the pane would silently be a bare shell")
			}
		})
	}
}

// TestZeroLaunchIsTheOnlyEmptyLaunch states the other half of the same property
// positively: the shell pane is the zero value, and nothing a constructor
// produces is empty.
//
// The render() assertion is not redundant with isEmpty(). Emptiness being the
// kind only stops a launch from REPORTING itself empty; a launch can still render
// to "" and vanish anyway, which is what nameLaunch("") did while the program
// word was left unquoted for every name launch. So this loop asserts both, and
// it is the assertion that pins the quoting of an empty program word.
func TestZeroLaunchIsTheOnlyEmptyLaunch(t *testing.T) {
	zero := paneLaunch{}
	if !zero.isEmpty() {
		t.Error("the zero launch must be empty - it is the shell pane")
	}
	if got := zero.render(); got != "" {
		t.Errorf("the zero launch must render to \"\", got %q", got)
	}
	for _, launch := range []paneLaunch{
		nameLaunch(""),
		nameLaunchWithEnv(constants.ClaudeLaunch.EnvPrefix, ""),
		binaryLaunch(""),
		binaryLaunchWithEnv(constants.ClaudeLaunch.EnvPrefix, ""),
	} {
		if launch.isEmpty() {
			t.Errorf("a constructed launch (%+v) must never report itself empty", launch)
		}
		if launch.render() == "" {
			t.Errorf(
				"a constructed launch (%+v) rendered to \"\", so it vanishes into a bare "+
					"shell pane despite not reporting itself empty",
				launch,
			)
		}
		if paneCommandFor(launch, testShell) == "" {
			t.Errorf("a constructed launch (%+v) produced no pane command", launch)
		}
	}
}

// --- routing a launch to its recipe ---

// TestPaneCommandForRoutesOnLaunchKind pins the pairing that used to be the
// caller's to get right. A NAME launch handed to the exec recipe emits
// `claude 'fix it'; exec '/bin/zsh'`, which tmux runs non-interactively - a shell
// that reads no `.zshrc`, so it has neither its PATH repair nor any definition of
// the name the user made themselves. That is exactly the case the interactive
// recipe serves (ADR-0020 part 3), and routing on the kind the value already
// carries is what makes the wrong pairing unrepresentable.
func TestPaneCommandForRoutesOnLaunchKind(t *testing.T) {
	tests := []struct {
		name   string
		launch paneLaunch
		want   string
	}{
		{
			name:   "a binary launch gets the exec recipe",
			launch: (&ClaudeCoder{}).execLaunch(testClaudePath, "fix it"),
			want: `CLAUDE_CODE_NO_FLICKER=1 '/Users/dev/.local/bin/claude' ` +
				`'fix it'; exec '/bin/zsh'`,
		},
		{
			name:   "a name launch gets the interactive recipe",
			launch: (&ClaudeCoder{}).interactiveLaunch("fix it"),
			want: `'/bin/zsh' -ic 'CLAUDE_CODE_NO_FLICKER=1 claude '\''fix it'\''; ` +
				`exec '\''/bin/zsh'\'' -i'`,
		},
		{
			name:   "nvim's bare name is a name launch too",
			launch: nameLaunch(nvimCommand),
			want:   `'/bin/zsh' -ic 'nvim; exec '\''/bin/zsh'\'' -i'`,
		},
		{
			// A shell pane must stay a shell pane: no command at all is what
			// gives it the shell tmux would have started anyway.
			name:   "the zero launch gets no command",
			launch: paneLaunch{},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paneCommandFor(tt.launch, testShell)
			if got != tt.want {
				t.Errorf("paneCommandFor() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPaneCommandForNeverSendsANameThroughTheExecRecipe is the negative form
// of the routing test, written against the shape rather than the bytes: whatever
// the name form renders to, it must arrive wrapped in the interactive shell and
// must NOT arrive as the bare `<command>; exec '<shell>'` the exec recipe
// produces.
func TestPaneCommandForNeverSendsANameThroughTheExecRecipe(t *testing.T) {
	nameLaunches := []paneLaunch{
		(&ClaudeCoder{}).interactiveLaunch("fix it"),
		(&OpenCodeCoder{}).interactiveLaunch("fix it"),
		(&OpenCodeCoder{}).interactiveLaunchWithAgent("code-reviewer", ReviewPrompt),
		nameLaunch(nvimCommand),
	}

	for _, launch := range nameLaunches {
		got := paneCommandFor(launch, testShell)
		if got == execPaneCommand(launch.render(), testShell) {
			t.Errorf(
				"name launch %q got the exec recipe (%q), which runs "+
					"non-interactively - no .zshrc PATH repair, and none of the "+
					"user's own definitions of that name",
				launch.render(), got,
			)
		}
		if !strings.HasPrefix(got, shellSingleQuote(testShell)+" -ic ") {
			t.Errorf("name launch must be wrapped in the interactive shell, got %q", got)
		}
	}
}

// --- recipe 1: the resolved-path pane command ---

// TestExecPaneCommandAppendsTrailingExec pins the whole recipe. The trailing
// exec is what keeps today's pane lifetime: without it the pane closes when the
// coder quits, and the last pane closing takes the window with it (ADR-0020
// part 2).
func TestExecPaneCommandAppendsTrailingExec(t *testing.T) {
	launch := (&ClaudeCoder{}).execLaunch(testClaudePath, "fix issue 1082")

	got := paneCommandFor(launch, testShell)
	want := `CLAUDE_CODE_NO_FLICKER=1 '/Users/dev/.local/bin/claude' 'fix issue 1082'; exec '/bin/zsh'`
	if got != want {
		t.Errorf("paneCommandFor() = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, "; exec "+shellSingleQuote(testShell)) {
		t.Errorf("recipe must end in the trailing exec, got %q", got)
	}
}

// TestExecPaneCommandQuotesAShellWithASpace covers the shell interpolation site
// of this recipe. $SHELL and tmux's default-shell are both values devgeta does
// not control, so neither site may assume a space-free path.
func TestExecPaneCommandQuotesAShellWithASpace(t *testing.T) {
	got := paneCommandFor(binaryLaunch(testNvimPath), "/opt/my shells/zsh")
	want := `'/opt/homebrew/bin/nvim'; exec '/opt/my shells/zsh'`
	if got != want {
		t.Errorf("paneCommandFor() = %q, want %q", got, want)
	}
}

// TestExecPaneCommandOnABlankCommandIsEmpty: a shell pane must stay a shell
// pane. Building "; exec '<shell>'" around nothing would run an empty command
// first, and passing no command at all is what gives the pane the shell tmux
// would have started anyway - today's behavior exactly.
func TestExecPaneCommandOnABlankCommandIsEmpty(t *testing.T) {
	for _, command := range []string{"", "   ", "\t\n"} {
		if got := execPaneCommand(command, testShell); got != "" {
			t.Errorf("execPaneCommand(%q) = %q, want \"\"", command, got)
		}
	}
	if got := paneCommandFor(paneLaunch{}, testShell); got != "" {
		t.Errorf("paneCommandFor(zero launch) = %q, want \"\"", got)
	}
}

// --- recipe 2: the interactive fallback / --pane pane command ---

// TestInteractivePaneCommandWrapsScriptInTheUserShell pins the fallback recipe
// exactly, for both of the scripts that land in it: a devgeta name-form launch
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
			name:   "name-form fallback for a coder pane",
			script: (&ClaudeCoder{}).interactiveLaunch("fix it").render(),
			shell:  testShell,
			want: `'/bin/zsh' -ic 'CLAUDE_CODE_NO_FLICKER=1 claude '\''fix it'\''; ` +
				`exec '\''/bin/zsh'\'' -i'`,
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
// TestExecPaneCommandOnABlankCommandIsEmpty: with nothing to run, the pane gets
// no command rather than a wrapper around an empty script (which would run
// `; exec ...` as its first statement).
func TestInteractivePaneCommandOnABlankScriptIsEmpty(t *testing.T) {
	for _, script := range []string{"", "   ", "\t\n"} {
		if got := interactivePaneCommand(script, testShell); got != "" {
			t.Errorf("interactivePaneCommand(%q) = %q, want \"\"", script, got)
		}
	}
}

// --- both recipes against a real shell parser ---
//
// The tests above pin the recipes against hand-computed literals, which proves
// they are stable but not that they are CORRECT: a wrong expectation and a wrong
// builder agree with each other. tmux hands a pane command to a real shell, so
// these two run the built command through one and check what the argv on the
// other side actually is. Same precedent and same justification as
// TestShellSingleQuoteRoundTripsThroughRealShell in layout_test.go: this is
// pure shell-syntax validation of a built string, exercising no devgeta
// tmux/git behavior, so it needs no testutil.MockApp. Nothing is installed and
// nothing outside t.TempDir() is written or read.

// argvEchoFixture writes an executable script that prints each argument it was
// given on its own line, and returns its path. It stands in for both
// interpolated executables a recipe names - the shell and the resolved binary -
// so a test can read back exactly how the real parser split the command.
//
// dirName is a subdirectory created under t.TempDir(); passing one with a space
// in it puts the quoting of the interpolated path under test too, which is the
// case ADR-0020 calls out ("/Users/Jane Doe/.local/bin/claude").
func argvEchoFixture(t *testing.T, dirName, name string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create fixture dir: %v", err)
	}
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nfor arg in \"$@\"; do printf '%s\\n' \"$arg\"; done\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to create fixture %q: %v", path, err)
	}
	return path
}

// runThroughRealShell runs command the way tmux does - as one shell-command
// string handed to a shell - and returns its stdout lines.
func runThroughRealShell(t *testing.T, command string) []string {
	t.Helper()

	out, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		t.Fatalf("sh -c %q failed: %v", command, err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

// TestInteractivePaneCommandWrapperSurvivesARealShellParser is the round trip
// for the wrapper's load-bearing claim: everything after -ic must reach the
// inner shell as ONE argument, with every character of the script intact -
// embedded single quote included. A hand-computed literal cannot prove that; the
// parser can.
//
// It substitutes an argv-echoing script for the shell, so nothing interactive
// runs and the trailing `exec` inside the quoted word is never executed - it is
// simply printed back as part of the argument, which is exactly the thing being
// asserted.
func TestInteractivePaneCommandWrapperSurvivesARealShellParser(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available in PATH, skipping shell round-trip test")
	}

	// A directory with a space in it: the shell path is interpolated twice, and
	// both sites have to survive it.
	fakeShell := argvEchoFixture(t, "my shells", "fake-shell")

	tests := []struct {
		name   string
		script string
	}{
		{
			name:   "a devgeta name-form launch with a quoted prompt",
			script: (&ClaudeCoder{}).interactiveLaunch("it's fine").render(),
		},
		{
			// ADR-0020 measured this exact value: a naive embedding ends the
			// -ic wrapper early and the command breaks.
			name:   "a --pane value containing a single quote",
			script: `printf %s "it's fine"`,
		},
		{
			name:   "a compound --pane value",
			script: "cd api && make dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := interactivePaneCommand(tt.script, fakeShell)
			argv := runThroughRealShell(t, command)

			wantInner := tt.script + "; exec " + shellSingleQuote(fakeShell) + " -i"
			if len(argv) != 2 {
				t.Fatalf("expected exactly 2 arguments, got %d: %q (command was %q)",
					len(argv), argv, command)
			}
			if argv[0] != "-ic" {
				t.Errorf("argv[0] = %q, want %q", argv[0], "-ic")
			}
			if argv[1] != wantInner {
				t.Errorf("inner script arrived as %q, want %q", argv[1], wantInner)
			}
		})
	}
}

// TestExecPaneCommandSurvivesARealShellParser is the same round trip for the
// resolved-path recipe: the quoted binary path and the quoted prompt must arrive
// as exactly two argv elements, and the trailing `exec` must then run in the
// SAME shell rather than being swallowed by the first command's quoting.
func TestExecPaneCommandSurvivesARealShellParser(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available in PATH, skipping shell round-trip test")
	}

	const prompt = "it's fine"
	fakeBinary := argvEchoFixture(t, "my bins", "fake-claude")
	// The exec'd shell is the same kind of fixture, so it prints nothing when it
	// runs with no arguments - its only job here is to prove the trailing exec
	// was a runnable command and not part of the first one.
	fakeShell := argvEchoFixture(t, "my shells", "fake-shell")

	launch := binaryLaunchWithEnv(constants.ClaudeLaunch.EnvPrefix, fakeBinary, prompt)
	command := paneCommandFor(launch, fakeShell)
	argv := runThroughRealShell(t, command)

	if len(argv) != 1 || argv[0] != prompt {
		t.Fatalf("the binary received %q, want exactly one argument %q (command was %q)",
			argv, prompt, command)
	}
}
