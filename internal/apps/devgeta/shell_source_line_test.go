package devgeta

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

// setupShellConfigTest isolates the shell config and moves devgeta's data
// directory to a path shaped like the real one.
//
// That second part is load-bearing. testutil's default app directory is named
// "app", but install.sh hardcodes `$HOME/.local/share/devgeta/devgeta.zsh`, and
// the behaviour under test is precisely whether devgeta recognises that line as
// naming the SAME file as its own absolute path. Under an app directory called
// "app" the two really are different files, so a test left on the default would
// be asserting against a situation that cannot occur in production.
//
// Cleanups are registered so they unwind innermost-first: the App.Root
// restore runs before testutil's own path restore.
func setupShellConfigTest(t *testing.T) {
	t.Helper()

	tc := testutil.SetupCompleteTest(t)
	t.Cleanup(tc.Cleanup)
	setupZshenvPaths(t, ".zshrc")

	origAppRoot := paths.Paths.App.Root
	t.Cleanup(func() { paths.Paths.App.Root = origAppRoot })
	paths.Paths.App.Root = filepath.Join(t.TempDir(), ".local", "share", "devgeta")
	if err := os.MkdirAll(paths.Paths.App.Root, 0o755); err != nil {
		t.Fatalf("failed to create the test app directory: %v", err)
	}
}

// installerBlock is the block install.sh appends to a shell config, spelled
// exactly as write_devgeta_block (install.sh) emits it. $HOME is left
// UNEXPANDED on purpose there, so the config survives being copied to another
// machine or user - and that is precisely why devgeta's own absolute-path
// source line could not see it.
const installerBlock = `# Added by devgeta installer
export PATH="$HOME/.local/bin:$PATH"
alias dg='devgeta'
if [ -f "$HOME/.local/share/devgeta/devgeta.zsh" ]; then
	. "$HOME/.local/share/devgeta/devgeta.zsh"
fi`

// installerSourceLine is the single line inside installerBlock that actually
// sources devgeta.zsh.
const installerSourceLine = `. "$HOME/.local/share/devgeta/devgeta.zsh"`

// seedShellConfig writes content to the isolated paths.Files.ShellConfig, so a
// test can start from a shell config in a particular real-world state.
func seedShellConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(paths.Files.ShellConfig, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to seed shell config: %v", err)
	}
}

// devgetaSourceStatements returns every uncommented line of the shell config
// that sources devgeta.zsh.
//
// It counts SOURCE STATEMENTS, not mentions: the installer's guarded block
// names the path twice (once in its `[ -f ... ]` test, once in the `.` that
// follows), and only the second of those actually loads the file. Counting
// mentions would report a correctly-wired config as a duplicate.
func devgetaSourceStatements(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(paths.Files.ShellConfig)
	if err != nil {
		t.Fatalf("failed to read shell config: %v", err)
	}

	var found []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "devgeta.zsh") {
			continue
		}
		if strings.HasPrefix(trimmed, "source ") || strings.HasPrefix(trimmed, ". ") {
			found = append(found, trimmed)
		}
	}
	return found
}

// assertSourcedOnce asserts the shell config loads devgeta.zsh exactly once,
// via the expected line.
func assertSourcedOnce(t *testing.T, want string) {
	t.Helper()

	statements := devgetaSourceStatements(t)
	if len(statements) != 1 {
		t.Fatalf(
			"Expected devgeta.zsh to be sourced exactly once, got %d source statements: %q",
			len(statements),
			statements,
		)
	}
	if statements[0] != want {
		t.Errorf("Expected the surviving source statement to be %q, got %q", want, statements[0])
	}
}

// newFileOnlyDevgeta builds a Devgeta over a real *commands.BaseCommand, which
// is safe here for the same reason TestForceConfigure_WiresZshenv does it: the
// shell-config wiring is pure file work against sandboxed paths, and
// MockBaseCommand no-ops MaybeSetupInFile so it cannot show what was written.
func newFileOnlyDevgeta() *Devgeta {
	return &Devgeta{
		Base:            commands.NewBaseCommandCustom(fakePlatform{}),
		ExtractEmbedded: mockExtractor,
	}
}

// TestForceConfigure_DoesNotAddASecondSourceLineWhenTheInstallerWroteOne is the
// regression test for the duplicate-source-line bug reported on a fresh
// machine.
//
// Two code paths write the source line and they spell the path differently:
// install.sh writes `. "$HOME/.local/share/devgeta/devgeta.zsh"`, and
// ForceConfigure writes `source "<absolute path>"`. ForceConfigure deduped by
// asking for a substring search on the ABSOLUTE path, which never
// appears in the installer's line, so every fresh install ended up sourcing
// devgeta.zsh twice: mise activate, zoxide init and three plugin sources all
// ran twice, measured at ~230ms of waste on every shell, tmux pane and
// subshell.
func TestForceConfigure_DoesNotAddASecondSourceLineWhenTheInstallerWroteOne(t *testing.T) {
	setupShellConfigTest(t)
	seedShellConfig(t, installerBlock)

	dg := newFileOnlyDevgeta()
	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	// The installer's line is the portable one ($HOME survives the config being
	// copied to another machine), so it is the one that must survive.
	assertSourcedOnce(t, installerSourceLine)
}

// TestForceConfigure_RemovesADuplicateLeftByAnEarlierRun repairs machines that
// already have both lines. Fixing the dedup check alone would leave every
// machine installed before the fix paying the doubled startup cost forever,
// because the redundant line is already on disk and nothing would ever remove
// it.
func TestForceConfigure_RemovesADuplicateLeftByAnEarlierRun(t *testing.T) {
	setupShellConfigTest(t)
	seedShellConfig(t, fmt.Sprintf("%s\n\nsource \"%s\"\n", installerBlock, getZshConfigPath()))

	dg := newFileOnlyDevgeta()
	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	assertSourcedOnce(t, installerSourceLine)
}

// TestSoftConfigure_RemovesADuplicateLeftByAnEarlierRun covers the same repair
// on a plain `dg configure`. An existing install has both a global config and a
// devgeta.zsh, so SoftConfigure does not fall through to ForceConfigure - and
// `dg configure` with no --force is what most users will run after upgrading.
func TestSoftConfigure_RemovesADuplicateLeftByAnEarlierRun(t *testing.T) {
	setupShellConfigTest(t)

	dg := newFileOnlyDevgeta()
	// Establish a complete install first, so the SoftConfigure below takes the
	// "already installed" path rather than delegating to ForceConfigure.
	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("initial ForceConfigure() failed: %v", err)
	}
	seedShellConfig(t, fmt.Sprintf("%s\n\nsource \"%s\"\n", installerBlock, getZshConfigPath()))

	if err := dg.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure() failed: %v", err)
	}

	assertSourcedOnce(t, installerSourceLine)
}

// TestForceConfigure_KeepsALoneAbsoluteSourceLine is the backwards-compatibility
// guard. A machine whose shell config carries only devgeta's own absolute-path
// line - no installer block, e.g. a `go install` user, or an older installer
// whose block has since been edited - must keep working. The repair removes a
// REDUNDANT line, never the last one.
func TestForceConfigure_KeepsALoneAbsoluteSourceLine(t *testing.T) {
	setupShellConfigTest(t)

	ownLine := fmt.Sprintf(`source "%s"`, getZshConfigPath())
	seedShellConfig(t, fmt.Sprintf("# my own config\n%s\n", ownLine))

	dg := newFileOnlyDevgeta()
	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	assertSourcedOnce(t, ownLine)
}

// TestForceConfigure_AddsTheSourceLineWhenTheShellConfigHasNone pins the
// original behaviour: a shell config that does not source devgeta.zsh still
// gets the line. Without this, a dedup check made too eager (or a repair that
// removed too much) would silently stop wiring devgeta up at all.
func TestForceConfigure_AddsTheSourceLineWhenTheShellConfigHasNone(t *testing.T) {
	setupShellConfigTest(t)
	seedShellConfig(t, "# a shell config that has never seen devgeta\n")

	dg := newFileOnlyDevgeta()
	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	assertSourcedOnce(t, fmt.Sprintf(`source "%s"`, getZshConfigPath()))

	// And running it again must not add a second one.
	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("second ForceConfigure() failed: %v", err)
	}
	assertSourcedOnce(t, fmt.Sprintf(`source "%s"`, getZshConfigPath()))
}

// TestForceConfigure_LeavesEverythingElseInTheShellConfigAlone guards the blast
// radius of the repair. It rewrites the user's ~/.zshrc, so it must touch
// nothing but devgeta's own redundant line - not their exports, not their own
// source lines, and not a devgeta line they deliberately commented out.
func TestForceConfigure_LeavesEverythingElseInTheShellConfigAlone(t *testing.T) {
	setupShellConfigTest(t)

	keep := []string{
		`export EDITOR="nvim"`,
		`source "$HOME/my-own-stuff.zsh"`,
		`# source "$HOME/.local/share/devgeta/devgeta.zsh"  # disabled on purpose`,
		`alias please='sudo'`,
	}
	seeded := fmt.Sprintf(
		"%s\n%s\n\nsource \"%s\"\n",
		installerBlock,
		strings.Join(keep, "\n"),
		getZshConfigPath(),
	)
	seedShellConfig(t, seeded)

	dg := newFileOnlyDevgeta()
	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	data, err := os.ReadFile(paths.Files.ShellConfig)
	if err != nil {
		t.Fatalf("failed to read shell config: %v", err)
	}
	got := string(data)
	for _, line := range keep {
		if !strings.Contains(got, line) {
			t.Errorf(
				"Expected the repair to leave %q in place, but it is gone.\nGot:\n%s",
				line,
				got,
			)
		}
	}
	// The whole installer block, comment and guard included, must survive too.
	if !strings.Contains(got, installerBlock) {
		t.Errorf("Expected the installer's block to survive intact.\nGot:\n%s", got)
	}
	assertSourcedOnce(t, installerSourceLine)
}

// TestForceConfigure_PreservesTheShellConfigFileMode checks the repair does not
// quietly re-permission the user's ~/.zshrc. install.sh writes its repair back
// with `cat` over the original file for this reason; a rewrite that forced 0644
// would be a silent permission change on a file some people keep at 0600.
func TestForceConfigure_PreservesTheShellConfigFileMode(t *testing.T) {
	setupShellConfigTest(t)
	seedShellConfig(t, fmt.Sprintf("%s\n\nsource \"%s\"\n", installerBlock, getZshConfigPath()))

	const mode os.FileMode = 0o600
	if err := os.Chmod(paths.Files.ShellConfig, mode); err != nil {
		t.Fatalf("failed to chmod the seeded shell config: %v", err)
	}

	dg := newFileOnlyDevgeta()
	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	info, err := os.Stat(paths.Files.ShellConfig)
	if err != nil {
		t.Fatalf("failed to stat the shell config: %v", err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Errorf("Expected the shell config to keep mode %o, got %o", mode, got)
	}
}
