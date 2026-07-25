package commands

import (
	"bufio"
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
	"sync"
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
	// still running once the timeout elapses. Zero preserves today's
	// unbounded behavior. Used for network calls (e.g. git fetch) where a
	// hang would otherwise block a caller expecting a fast response.
	Timeout time.Duration
	// Dir, when non-empty, sets the command's working directory, so a caller
	// can run a tool that has no directory flag (e.g. gh) against a specific
	// repo or worktree. Empty preserves the process's current directory.
	Dir string
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
	execCommand.Stdin = os.Stdin

	stdoutPipe, err := execCommand.StdoutPipe()
	if err != nil {
		logger.L().Errorf("failed to get stdout pipe: %v", err)
		return "", "", err
	}

	stderrPipe, err := execCommand.StderrPipe()
	if err != nil {
		logger.L().Errorf("failed to get stderr pipe: %v", err)
		return "", "", err
	}

	var stdoutBuf, stderrBuf strings.Builder

	// Start command
	if err := execCommand.Start(); err != nil {
		logger.L().Errorf("failed to start command: %v", err)
		return "", "", err
	}

	// Drain both pipes concurrently. When Stream is set we copy bytes straight
	// through to the terminal (real-time progress); otherwise we keep the
	// line-buffered, debug-log-only behavior. Either way output is captured into
	// the buffers for the returned strings.
	var wg sync.WaitGroup
	wg.Add(2)

	drain := func(pipe io.Reader, buf *strings.Builder, live io.Writer, label string) {
		defer wg.Done()
		if cmd.Stream {
			_, _ = io.Copy(io.MultiWriter(buf, live), pipe)
			return
		}
		// Read line-by-line via bufio.Reader (not bufio.Scanner) so a single
		// line longer than bufio's 64KB token limit does not abort the read.
		// A Scanner returns an error and stops on an over-long line, which
		// leaves the pipe undrained — the child then blocks writing to a full
		// pipe and Wait() deadlocks forever. gh's compact JSON is emitted as
		// one long line and routinely exceeds 64KB on busy PRs, so this path
		// must handle arbitrarily long lines.
		reader := bufio.NewReader(pipe)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				buf.WriteString(line)
				logger.L().Debugw(label, "line", strings.TrimRight(line, "\n"))
			}
			if err != nil {
				return
			}
		}
	}

	go drain(stdoutPipe, &stdoutBuf, os.Stdout, "stdout")
	go drain(stderrPipe, &stderrBuf, os.Stderr, "stderr")

	// Wait for command to complete, then for the readers to flush.
	err = execCommand.Wait()
	wg.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("command timed out after %s: %w", cmd.Timeout, ctx.Err())
	}
	if err != nil {
		logger.L().Debugw("command finished with error", "error", err, "stderr", stderrBuf.String())
	}

	if cmd.PostExecMsg != "" && err == nil {
		utils.Print(cmd.PostExecMsg, "")
	}

	return strings.TrimSpace(stdoutBuf.String()), strings.TrimSpace(stderrBuf.String()), err
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
