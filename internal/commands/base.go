package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/cjairm/devgeta/pkg/utils"
)

var (
	LookPathFn = exec.LookPath
	CommandFn  = exec.Command
)

// BaseCommandExecutor defines the interface for executing commands and managing system state
// This interface allows for dependency injection and mocking in tests
type BaseCommandExecutor interface {
	// Command execution
	ExecCommand(cmd CommandParams) (string, string, error)

	// Platform detection
	IsMac() bool

	// Shell configuration
	Setup(line string) error
	MaybeSetup(line, toSearch string) error
	MaybeSetupInFile(line, toSearch, filePath string) error

	// System checks
	IsDesktopAppPresent(dirPath, appName string) (bool, error)
	IsPackagePresent(cmd *exec.Cmd, packageName string) (bool, error)
	IsFontPresent(fontName string) (bool, error)

	// Installation helpers
	MaybeInstall(
		itemName string,
		alias []string,
		checkInstalled func(string) (bool, error),
		installFunc func(string) error,
		installURLFunc func(string) error,
		itemType string,
	) error
	InstallFontFromURL(url, fontFileName string, runCache bool) error
}

type BaseCommand struct {
	Platform CustomizablePlatform
}

type CommandParams struct {
	PreExecMsg  string
	PostExecMsg string
	IsSudo      bool
	Command     string
	Args        []string
	// Stream, when true, tees the command's stdout/stderr to the process's
	// own stdout/stderr as it runs, so callers (e.g. `dg task` utilities) see
	// real-time progress instead of nothing. Output is still captured into the
	// returned strings for error context. Default false preserves the quiet,
	// debug-log-only behavior used by the installers.
	Stream bool
	// Timeout, when non-zero, bounds the command's execution: it is killed if
	// still running once the timeout elapses. Used for network calls (e.g. git
	// fetch) where a hang would otherwise block a caller expecting a fast
	// response.
	//
	// "It is killed" means the whole process group when NoStdin is also set,
	// and the direct child alone otherwise — see NoStdin, which is what a timed
	// call needs to actually reap the forks its command spawns.
	//
	// Zero is no longer unbounded. Every call, timed or not, is additionally
	// bounded by outputDrainGrace: exec.Cmd.WaitDelay stops a process that
	// outlived the command — a daemon holding the inherited pipe — from
	// blocking ExecCommand for that process's lifetime. So the effective
	// wall-clock bound of a timed call is Timeout + outputDrainGrace, not
	// Timeout: a Timeout of 300ms whose output is held past the deadline
	// returns at roughly 2.3s, not 300ms. Size a Timeout with that tail in
	// mind, and don't read a zero as "this can hang forever" — it can't.
	Timeout time.Duration
	// Dir, when non-empty, sets the command's working directory, so a caller
	// can run a tool that has no directory flag (e.g. gh) against a specific
	// repo or worktree. Empty preserves the process's current directory.
	Dir string
	// Env, when non-empty, adds child-only environment variables on top of
	// devgeta's own environment (an overlay, not a replacement) — so a caller
	// can pass one variable to a single spawned process without disturbing
	// anything else. Empty leaves exec.Cmd.Env nil, preserving today's
	// behavior of full inheritance from the current process.
	//
	// Combining it with IsSudo does NOT reliably deliver the overlay: real
	// sudo resets the environment it passes to the child unless it is invoked
	// with -E or the variable is listed in the sudoers env_keep, so the child
	// can silently run without it. No caller pairs the two today; a caller
	// that needs to must arrange for sudo to preserve the variable rather than
	// assume this field is enough.
	Env []string
	// OnStdoutLine, when non-nil, is called with each complete stdout line
	// (newline trimmed) as it is read, BEFORE the command finishes — so a
	// caller can report live progress from a long-running tool whose output it
	// still wants captured and returned whole. Stream tees raw bytes to the
	// terminal instead; this hands the caller lines it can interpret and
	// summarize (e.g. `dg task review-run` turning an `opencode run --format
	// json` event stream into one short progress line per tool call).
	//
	// It is called on the goroutine draining stdout, one line at a time, so it
	// never runs concurrently with itself — but it DOES run while
	// ExecCommand's own caller is blocked inside ExecCommand, so anything it
	// writes to must not also be written by that caller until ExecCommand
	// returns. It is ignored when Stream is true: that path copies bytes with
	// no line framing at all, and no caller needs both.
	OnStdoutLine func(string)
	// NoStdin, when true, leaves the child's stdin disconnected — exec.Cmd
	// wires a nil Stdin to os.DevNull — instead of handing it devgeta's own
	// stdin. Use it for commands that must never stop and wait for a human:
	// anything run from a TUI or on a timeout, where an interactive prompt
	// (a git credential prompt, an ssh passphrase) would silently wedge the
	// caller instead of failing.
	//
	// It does a second thing, and a caller setting it on a timed call has to
	// know: when a Timeout is also set, the child is put in its own process
	// group and the deadline kills that whole group instead of just the direct
	// child. That is what makes a Timeout reach the forks — `sh -c`, brew, apt,
	// an agent CLI's helpers — rather than leaving them running with nothing to
	// reap them. The cost is that the child is no longer in the terminal's
	// foreground process group, so a Ctrl-C typed at devgeta does not reach it;
	// only the deadline stops it. On an untimed call NoStdin means the stdin
	// opt-out alone, since there is no deadline to widen.
	//
	// Default false preserves the inherited stdin, which the installers
	// depend on: `dg install` shells out to sudo, and sudo must be able to
	// read the password from the terminal.
	NoStdin bool
}

func NewBaseCommand() *BaseCommand {
	return &BaseCommand{
		Platform: NewPlatform(),
	}
}

func NewBaseCommandCustom(p CustomizablePlatform) *BaseCommand {
	return &BaseCommand{
		Platform: p,
	}
}

// IsMac returns true if the current platform is macOS
func (b *BaseCommand) IsMac() bool {
	return b.Platform.IsMac()
}

func (b *BaseCommand) Setup(line string) error {
	return files.AddLineToFile(line, paths.Files.ShellConfig)
}

func (b *BaseCommand) MaybeSetup(line, toSearch string) error {
	return b.MaybeSetupInFile(line, toSearch, paths.Files.ShellConfig)
}

// MaybeSetupInFile is MaybeSetup generalized to an arbitrary file, so callers
// that need to wire a line into a shell startup file other than
// paths.Files.ShellConfig (e.g. ~/.zshenv) don't need a second code path.
// The target file is allowed not to exist yet: a missing file has never had
// the line added, so it's treated as "not set up" and gets created.
func (b *BaseCommand) MaybeSetupInFile(line, toSearch, filePath string) error {
	isAlreadySetup, err := files.ContentExistsInFile(filePath, toSearch)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		isAlreadySetup = false
	}
	if isAlreadySetup {
		return nil
	}
	return files.AddLineToFile(line, filePath)
}

func (b *BaseCommand) IsDesktopAppPresent(dirPath, appName string) (bool, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return false, fmt.Errorf("failed to read directory: %v", err)
	}
	for _, file := range files {
		filename := strings.ToLower(file.Name())
		if strings.Contains(filename, strings.ToLower(appName)) {
			if b.Platform.IsLinux() && strings.HasSuffix(filename, ".desktop") {
				return true, nil
			}
			if b.Platform.IsMac() {
				return true, nil
			}
		}
	}
	return false, nil
}

func (b *BaseCommand) IsPackagePresent(cmd *exec.Cmd, packageName string) (bool, error) {
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("failed running command: %w", err)
	}
	lines := bytes.Split(out.Bytes(), []byte{'\n'})
	if b.Platform.IsMac() {
		return findPackageInBrewOutput(lines, packageName), nil
	} else if b.Platform.IsLinux() {
		return findPackageInDpkgOutput(lines, packageName), nil
	}
	return false, nil
}

func findPackageInBrewOutput(lines [][]byte, packageName string) bool {
	for _, line := range lines {
		if string(line) == packageName {
			return true
		}
	}
	return false
}

func findPackageInDpkgOutput(lines [][]byte, packageName string) bool {
	for _, line := range lines {
		// The package name is typically the second column in the output
		fields := bytes.Fields(line)
		if len(fields) > 1 && string(fields[1]) == packageName {
			return true
		}
	}
	return false
}

func (b *BaseCommand) IsFontPresent(fontName string) (bool, error) {
	if _, err := LookPathFn("fc-list"); err == nil {
		cmd := CommandFn("fc-list", ":", "family")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			lines := bytes.Split(out.Bytes(), []byte{'\n'})
			fontNameLower := strings.ToLower(fontName)
			for _, line := range lines {
				if strings.Contains(strings.ToLower(string(line)), fontNameLower) {
					return true, nil
				}
			}
			return false, nil
		}
	}
	// Fallback: scan known font directories
	fontDirs := []string{paths.Paths.User.Fonts, paths.Paths.System.Fonts}
	for _, dir := range fontDirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue // ignore unreadable dirs
		}
		for _, file := range files {
			if strings.Contains(strings.ToLower(file.Name()), strings.ToLower(fontName)) &&
				hasFontExtension(strings.ToLower(file.Name())) {
				return true, nil
			}
		}
	}
	return false, nil
}

func hasFontExtension(filename string) bool {
	return strings.HasSuffix(filename, ".ttf") ||
		strings.HasSuffix(filename, ".otf") ||
		strings.HasSuffix(filename, ".woff") ||
		strings.HasSuffix(filename, ".woff2")
}

func (b *BaseCommand) ExecCommand(cmd CommandParams) (string, string, error) {
	if cmd.PreExecMsg != "" {
		utils.Print(cmd.PreExecMsg, "")
	}

	command := cmd.Command
	args := cmd.Args

	if cmd.IsSudo {
		// Prepend the original command to args, then use sudo as the command
		args = append([]string{command}, args...)
		command = "sudo"
	}

	logger.L().
		Debugw("Executing command", "command", strings.Join(append([]string{command}, args...), " "))

	ctx, cancel := commandTimeoutContext(cmd.Timeout)
	defer cancel()

	execCommand := exec.CommandContext(ctx, command, args...)
	if cmd.Dir != "" {
		execCommand.Dir = cmd.Dir
	}
	// Overlay, not a replacement: append onto the inherited environment so the
	// child still gets PATH/HOME/etc. Leaving Env nil when cmd.Env is empty
	// preserves today's behavior of full inheritance from the current
	// process — exec.Cmd.Env replaces rather than extends when set, so
	// assigning only the extra variables here would silently drop everything
	// else the child needs.
	if len(cmd.Env) > 0 {
		execCommand.Env = append(os.Environ(), cmd.Env...)
	}
	// Inheriting devgeta's stdin is what lets `dg install` hand the terminal
	// to sudo for a password. Callers that must never block on a human set
	// NoStdin; a nil Stdin makes exec.Cmd give the child os.DevNull, so an
	// interactive prompt fails immediately instead of hanging.
	if !cmd.NoStdin {
		execCommand.Stdin = os.Stdin
	}

	// Let exec own the pipes and the copying (see outputWriter): that is what
	// arms WaitDelay below, and it also removes the descriptor leak that came
	// with owning them here — exec.Cmd.Start closes every pipe it created when
	// it fails, so a command that never runs can no longer strand one.
	var teeStdout, teeStderr io.Writer
	if cmd.Stream {
		teeStdout, teeStderr = os.Stdout, os.Stderr
	}
	stdout := newOutputWriter("stdout", teeStdout, cmd.OnStdoutLine)
	stderr := newOutputWriter("stderr", teeStderr, nil)
	execCommand.Stdout = stdout
	execCommand.Stderr = stderr

	// Bound how long the command's output can outlive the command. Without
	// this, ExecCommand returns only once every holder of the inherited
	// stdout/stderr write end has closed it — not when the child exits — so
	// anything that backgrounds a worker (`curl … | sh` installing a daemon,
	// `brew services start`, an agent spawning a helper) blocks devgeta for
	// the grandchild's lifetime.
	execCommand.WaitDelay = outputDrainGrace

	// The group kill is only useful where there is a cancellation to widen,
	// and only safe where the child has no terminal to be stopped by; see
	// isolateProcessGroup. ctx.Done() is nil exactly when no Timeout was
	// asked for, so this reads as "there is a deadline that will fire".
	if cmd.NoStdin && ctx.Done() != nil {
		isolateProcessGroup(execCommand)
	}

	// Start command
	if err := execCommand.Start(); err != nil {
		logger.L().Errorf("failed to start command: %v", err)
		return "", "", err
	}

	// Wait joins exec's copying goroutines on every path before it returns, so
	// the writers are ours alone again from here — no truncation, and nothing
	// still writing to them.
	err := execCommand.Wait()
	stdout.flushPending()
	stderr.flushPending()

	// ErrWaitDelay means the child itself exited and only the grace period for
	// draining its output ran out: something it left behind still holds the
	// inherited pipe. That is not a failure of the command the caller asked
	// for, and reporting it as one would make every daemon-spawning install
	// look broken.
	if errors.Is(err, exec.ErrWaitDelay) {
		logger.L().Debugw(
			"command exited but left a process holding its output pipes",
			"command", command,
			"grace", outputDrainGrace,
		)
		err = nil
	}
	// Report the deadline only when the command actually failed under it.
	// Overriding unconditionally used to tell callers a command timed out even
	// when Wait returned nil — reachable whenever a grandchild held the pipes
	// past the deadline — so callers that roll back on error rolled back
	// completed work.
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("command timed out after %s: %w", cmd.Timeout, ctx.Err())
	}
	if err != nil {
		logger.L().
			Debugw("command finished with error", "error", err, "stderr", stderr.captured.String())
	}

	if cmd.PostExecMsg != "" && err == nil {
		utils.Print(cmd.PostExecMsg, "")
	}

	capturedStdout := strings.TrimSpace(stdout.captured.String())
	capturedStderr := strings.TrimSpace(stderr.captured.String())
	return capturedStdout, capturedStderr, err
}

// outputDrainGrace is how long ExecCommand keeps reading a command's output
// after the command itself has exited, before it gives up and closes the
// pipes.
//
// Two seconds because it has to cover both ends of a narrow trade. Too short
// and it truncates: a child can exit while a short-lived helper it spawned is
// still flushing the tail of its output, and this package's own regression
// test for that (TestExecCommandCapturesOutputAfterChildExits) writes 400ms
// after the child exits. Too long and it becomes the delay itself: a command
// that leaves a long-lived daemon holding the descriptor pays this on every
// invocation. Two seconds is several times the observed flush window and still
// short enough to read as a pause rather than a hang.
//
// Changing it is not a local edit: this package's tests bound their timing
// assertions against this value rather than restating it, so raising it makes
// them fail on their elapsed-time checks with messages about pipes and
// deadlines that say nothing about the constant. The coupled assertions are
// exec_cancel_test.go's maxWait values for the held-output and still-running
// cases, and exec_pipes_test.go's "returned after %s" check on the grandchild
// path. Adjust them in the same change.
const outputDrainGrace = 2 * time.Second

// isolateProcessGroup puts the child in its own process group and widens the
// deadline kill from the child alone to that whole group.
//
// Without it a Timeout kills only the direct child, because
// exec.CommandContext's default Cancel is Process.Kill(). Everything devgeta
// shells out to forks — `sh -c`, brew, apt, the agent CLIs — so the forks
// outlive the deadline with nothing left to reap them. The worst case is a
// 30-minute headless agent run, the largest process tree devgeta spawns.
//
// SIGKILL, not SIGTERM followed by SIGKILL. The whole finding is that
// processes survive the deadline they were given, so the cancel has to be the
// one signal that cannot be caught or ignored — shells and supervisors
// routinely trap SIGTERM. A ladder would also need a second grace period and
// would double the worst-case shutdown, to protect cleanup that nothing here
// depends on: a timed-out command's output is discarded. This also keeps
// exec's own default semantics (Process.Kill is SIGKILL), widened from the
// process to its group and nothing more.
//
// Callers that inherit devgeta's stdin must never get this, and the reason is
// not just Ctrl-C. A child in a new process group is not the terminal's
// foreground group, so the first time it reads the controlling terminal the
// kernel stops it with SIGTTIN — no error, no output, a stopped process the
// user cannot type into. That is exactly sudo's password prompt, which
// `dg install` depends on. Terminal-inheriting commands therefore stay in
// devgeta's group and keep the single-process kill; they are also the ones a
// human is sitting in front of and can interrupt themselves.
//
// Unix-only by construction: SysProcAttr.Setpgid and a negative-pid Kill do
// not exist on Windows, which devgeta does not build for (CLAUDE.md §8).
func isolateProcessGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return os.ErrProcessDone
		}
		// Setpgid with no Pgid makes the child's group id its own pid, so the
		// negative pid addresses the child and every descendant that has not
		// left the group.
		err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			// The group finished between the deadline firing and the signal.
			// exec reads ErrProcessDone as "nothing to interrupt" rather than
			// a failed cancellation, which is what happened.
			return os.ErrProcessDone
		}
		return err
	}
}

// commandTimeoutContext builds the context used to bound a command's
// execution. A zero timeout preserves unbounded execution (today's
// behavior); a positive timeout returns a context that cancels once it
// elapses, causing exec.CommandContext to kill the still-running process.
func commandTimeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (b *BaseCommand) MaybeInstall(
	itemName string,
	alias []string,
	checkInstalled func(string) (bool, error),
	installFunc func(string) error,
	installURLFunc func(string) error, // Optional: Handle URL-based installation (e.g., for fonts)
	itemType string,
) error {
	var isInstalled bool
	var err error
	pkgToInstall := itemName
	if len(alias) > 0 {
		pkgToInstall = alias[0]
	}

	globalConfig := &config.GlobalConfig{}
	if err := globalConfig.Load(); err != nil {
		// HACK: If global config doesn't exist and we're trying to install git,
		// we can assume it's a fresh install and create the global config
		if pkgToInstall == constants.Git {
			globalConfig.Create()
		} else {
			logger.L().Errorw("Could not load global config", "error", err)
			return err
		}
	} else {
		if globalConfig.IsInstalledByDevgeta(pkgToInstall, itemType) {
			logger.L().
				Debugw("Item already tracked as installed by devgeta", "item", pkgToInstall, "type", itemType)
			return nil
		}
		if globalConfig.IsAlreadyInstalled(pkgToInstall, itemType) {
			logger.L().
				Debugw("Item is already installed, skipping", "item", pkgToInstall, "type", itemType)
			return nil
		}
	}

	isInstalled, err = checkInstalled(pkgToInstall)
	if err != nil {
		return err
	}

	if isInstalled {
		logger.L().
			Debugw("Item is already installed, marking as such in global config", "item", pkgToInstall, "type", itemType)
		globalConfig.AddToAlreadyInstalled(pkgToInstall, itemType)
		globalConfig.Save()
		return nil
	}

	var installErr error
	if installURLFunc != nil {
		installErr = installURLFunc(pkgToInstall)
	} else {
		installErr = installFunc(pkgToInstall)
	}

	if installErr == nil {
		globalConfig.AddToInstalled(pkgToInstall, itemType)
		if err := globalConfig.Save(); err != nil {
			logger.L().Errorw("Failed to update global config after installation", "error", err)
		}
	} else {
		logger.L().
			Warnw("Installation failed", "item", pkgToInstall, "type", itemType, "error", installErr)
	}

	return installErr
}

func (b *BaseCommand) InstallFontFromURL(url, fontFileName string, runCache bool) error {
	tmpPath := fmt.Sprintf("/tmp/%s.ttf", fontFileName)

	// 1. Download font
	if _, _, err := b.ExecCommand(CommandParams{
		PreExecMsg: fmt.Sprintf("Downloading %s...", fontFileName),
		Command:    "curl",
		Args:       []string{"-o", tmpPath, url},
	}); err != nil {
		return err
	}

	// 2. Move font
	if _, _, err := b.ExecCommand(CommandParams{
		PreExecMsg: "Installing font...",
		Command:    "mv",
		Args:       []string{tmpPath, filepath.Join(paths.Paths.User.Fonts, fontFileName+".ttf")},
	}); err != nil {
		return err
	}

	// 3. Update font cache if needed
	if runCache {
		if _, _, err := b.ExecCommand(CommandParams{
			PreExecMsg: "Refreshing font cache...",
			Command:    "fc-cache",
			Args:       []string{"-fv"},
			IsSudo:     true,
		}); err != nil {
			return fmt.Errorf("failed to refresh font cache: %w", err)
		}
	}
	return nil
}
