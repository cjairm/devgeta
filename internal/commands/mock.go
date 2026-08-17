package commands

import (
	"os/exec"
	"strings"
	"sync"
)

// MockCommand provides a mock implementation of the Command interface for testing
type MockCommand struct {
	InstalledPkg          string
	UninstalledPkg        string
	UninstalledDesktopApp string
	MaybeInstalled        string
	InstalledDesktopApp   string
	MaybeInstalledDesktop string
	FontURL               string
	FontName              string

	// Error fields to simulate various failure scenarios
	InstallError        error
	UninstallError      error
	MaybeInstallError   error
	DesktopInstallError error
	FontInstallError    error
	ValidationError     error

	// State tracking
	PackageManagerInstalled bool
	PackageInstalled        bool
	DesktopAppInstalled     bool

	// Call history for MaybeInstallPackage
	MaybeInstalledPkgs []string         // ordered history of all MaybeInstallPackage calls
	MaybeInstallErrors map[string]error // per-package error injection: pkg -> error

	// Per-name presence/error overrides — map hit wins over the global bool/error above.
	PackageInstalledMap       map[string]bool
	DesktopAppInstalledMap    map[string]bool
	PackageInstalledErrors    map[string]error
	DesktopAppInstalledErrors map[string]error
}

// NewMockCommand creates a new MockCommand with sensible defaults
func NewMockCommand() *MockCommand {
	return &MockCommand{
		PackageManagerInstalled:   true,
		PackageInstalled:          false,
		DesktopAppInstalled:       false,
		MaybeInstalledPkgs:        []string{},
		MaybeInstallErrors:        map[string]error{},
		PackageInstalledMap:       map[string]bool{},
		DesktopAppInstalledMap:    map[string]bool{},
		PackageInstalledErrors:    map[string]error{},
		DesktopAppInstalledErrors: map[string]error{},
	}
}

func (m *MockCommand) InstallPackage(pkg string) error {
	m.InstalledPkg = pkg
	return m.InstallError
}

func (m *MockCommand) UninstallPackage(pkg string) error {
	m.UninstalledPkg = pkg
	return m.UninstallError
}

func (m *MockCommand) UninstallDesktopApp(pkg string) error {
	m.UninstalledDesktopApp = pkg
	return m.UninstallError
}

func (m *MockCommand) MaybeInstallPackage(pkg string, alias ...string) error {
	m.MaybeInstalled = pkg
	m.MaybeInstalledPkgs = append(m.MaybeInstalledPkgs, pkg)
	if m.MaybeInstallErrors != nil {
		if err, ok := m.MaybeInstallErrors[pkg]; ok {
			return err
		}
	}
	return m.MaybeInstallError
}

func (m *MockCommand) InstallDesktopApp(packageName string) error {
	m.InstalledDesktopApp = packageName
	return m.DesktopInstallError
}

func (m *MockCommand) MaybeInstallDesktopApp(desktopAppName string, alias ...string) error {
	m.MaybeInstalledDesktop = desktopAppName
	return m.DesktopInstallError
}

func (m *MockCommand) MaybeInstallFont(url, fontName string, runCache bool, alias ...string) error {
	m.FontURL = url
	m.FontName = fontName
	return m.FontInstallError
}

func (m *MockCommand) ValidateOSVersion() error {
	return m.ValidationError
}

func (m *MockCommand) MaybeInstallPackageManager() error {
	return m.ValidationError
}

func (m *MockCommand) InstallPackageManager() error {
	return m.ValidationError
}

func (m *MockCommand) IsPackageManagerInstalled() bool {
	return m.PackageManagerInstalled
}

func (m *MockCommand) IsPackageInstalled(packageName string) (bool, error) {
	if m.PackageInstalledErrors != nil {
		if err, ok := m.PackageInstalledErrors[packageName]; ok {
			return false, err
		}
	}
	if m.PackageInstalledMap != nil {
		if v, ok := m.PackageInstalledMap[packageName]; ok {
			return v, nil
		}
	}
	return m.PackageInstalled, nil
}

func (m *MockCommand) IsDesktopAppInstalled(desktopAppName string) (bool, error) {
	if m.DesktopAppInstalledErrors != nil {
		if err, ok := m.DesktopAppInstalledErrors[desktopAppName]; ok {
			return false, err
		}
	}
	if m.DesktopAppInstalledMap != nil {
		if v, ok := m.DesktopAppInstalledMap[desktopAppName]; ok {
			return v, nil
		}
	}
	return m.DesktopAppInstalled, nil
}

// Helper methods for testing

// Reset clears all tracked state for reuse in multiple tests
func (m *MockCommand) Reset() {
	m.InstalledPkg = ""
	m.UninstalledPkg = ""
	m.UninstalledDesktopApp = ""
	m.MaybeInstalled = ""
	m.InstalledDesktopApp = ""
	m.MaybeInstalledDesktop = ""
	m.FontURL = ""
	m.FontName = ""

	m.InstallError = nil
	m.UninstallError = nil
	m.MaybeInstallError = nil
	m.DesktopInstallError = nil
	m.FontInstallError = nil
	m.ValidationError = nil

	m.MaybeInstalledPkgs = []string{}
	m.MaybeInstallErrors = map[string]error{}
	m.PackageInstalledMap = map[string]bool{}
	m.DesktopAppInstalledMap = map[string]bool{}
	m.PackageInstalledErrors = map[string]error{}
	m.DesktopAppInstalledErrors = map[string]error{}
}

// SetError configures error scenarios for different operations
func (m *MockCommand) SetError(operation string, err error) {
	switch operation {
	case "install":
		m.InstallError = err
	case "uninstall":
		m.UninstallError = err
	case "maybe-install":
		m.MaybeInstallError = err
	case "desktop":
		m.DesktopInstallError = err
	case "font":
		m.FontInstallError = err
	case "validation":
		m.ValidationError = err
	}
}

// execResult holds a single canned response for ExecCommand.
type execResult struct {
	Stdout, Stderr string
	Err            error
}

// MockBaseCommand provides a mock implementation for BaseCommand methods
// This allows tests to avoid running actual system commands
//
// # Concurrency
//
// ExecCommand is safe to call from several goroutines at once: the call
// recorder and the positional result queue are guarded by mu, so code under
// test may run two mocked executions in parallel. That is the whole of the
// guarantee — it does NOT make every field safe to touch at any moment. The
// contract the mutex assumes, and which every caller in this repo already
// honors, is:
//
//   - ExecCommandFn, ExecCommandStdout/Stderr/Error and the positional queue
//     (SetExecCommandResults) are CONFIGURED BEFORE the call under test starts.
//     No production code assigns them; test setup does, ahead of the call it
//     scripts.
//   - ExecCommandCalls is READ AFTER that call has joined — every read is a
//     post-call assertion, including testutil.VerifyNoRealCommands.
//
// Neither during. Assigning a field or reading the recorder while a mocked
// execution is in flight is outside the contract and is a defect in the test,
// not something the mock defends against.
type MockBaseCommand struct {
	// mu guards ExecCommandCalls and the positional queue below against
	// concurrent ExecCommand calls. See the type's Concurrency section for what
	// it does and does not cover.
	mu sync.Mutex

	// Tracks all ExecCommand calls for verification
	ExecCommandCalls []CommandParams

	// Return values for ExecCommand (single fixed result — used when execResults is empty)
	ExecCommandStdout string
	ExecCommandStderr string
	ExecCommandError  error

	// ExecCommandFn, when set, answers every ExecCommand call and takes
	// precedence over execResults and the fixed fields. Use it when a test
	// needs to key its answer off WHAT was run rather than the position of
	// the call — e.g. "succeed at everything except `worktree prune`".
	//
	// Positional sequences are unavoidably coupled to the exact number and
	// order of the commands under test, so an implementation change that adds
	// one lookup silently shifts every later answer and the test fails for a
	// reason unrelated to what it asserts. Prefer this hook whenever the
	// assertion is about a specific command.
	ExecCommandFn func(cmd CommandParams) (string, string, error)

	// Per-call results: when non-empty, each call pops the next entry.
	// After all entries are consumed the last one is repeated.
	execResults []execResult
	execCallIdx int

	// Return values for presence checks
	IsDesktopAppPresentResult bool
	IsPackagePresentResult    bool
	IsFontPresentResult       bool
	IsFontPresentError        error

	// Platform detection mock
	IsMacResult bool

	// Return values for other methods
	SetupError            error
	MaybeSetupError       error
	MaybeSetupInFileError error
	MaybeInstallError     error
	InstallFontURLError   error
}

// NewMockBaseCommand creates a new MockBaseCommand with sensible defaults
func NewMockBaseCommand() *MockBaseCommand {
	return &MockBaseCommand{
		ExecCommandCalls:          []CommandParams{},
		ExecCommandStdout:         "",
		ExecCommandStderr:         "",
		ExecCommandError:          nil,
		IsDesktopAppPresentResult: false,
		IsPackagePresentResult:    false,
		IsFontPresentResult:       false,
		IsMacResult:               false,
		SetupError:                nil,
		MaybeSetupError:           nil,
		MaybeSetupInFileError:     nil,
		MaybeInstallError:         nil,
		InstallFontURLError:       nil,
	}
}

// IsMac mocks the BaseCommand.IsMac method
func (m *MockBaseCommand) IsMac() bool {
	return m.IsMacResult
}

// SetExecCommandResults configures a per-call sequence of results. Each call
// to ExecCommand returns the next entry; once exhausted the last entry repeats.
// Falls back to the single ExecCommandStdout/Stderr/Error fields when empty.
func (m *MockBaseCommand) SetExecCommandResults(results ...execResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execResults = results
	m.execCallIdx = 0
}

// ExecCommandResult is a convenience constructor for SetExecCommandResults.
func ExecCommandResult(stdout, stderr string, err error) execResult {
	return execResult{Stdout: stdout, Stderr: stderr, Err: err}
}

// ExecCommand mocks the BaseCommand.ExecCommand method.
// It records the call and returns the next canned result (or the fixed result).
//
// When the caller asked for line-by-line stdout (CommandParams.OnStdoutLine),
// the canned stdout is replayed through that callback before being returned —
// the same order the real executor produces (every line during the run, the
// whole string after it). Without this, a test could script a tool's output
// and still never exercise the caller's live-progress path.
//
// The lock is held for BOOKKEEPING ONLY — recording the call, and popping the
// positional queue when no ExecCommandFn is set. Both callbacks this method
// invokes belong to the caller and may reach back into the mock
// (ExecCommandFn is a test's own function, OnStdoutLine is production code),
// so they run after the unlock. Holding the lock across them would deadlock
// any callback that touches the mock, and would serialize every mocked
// execution so concurrent callers could never actually overlap.
func (m *MockBaseCommand) ExecCommand(cmd CommandParams) (string, string, error) {
	m.mu.Lock()
	m.ExecCommandCalls = append(m.ExecCommandCalls, cmd)
	// Snapshot the hook under the lock but call it outside: it outranks every
	// other source of an answer, so nothing else may be consumed when it is set.
	fn := m.ExecCommandFn
	var stdout, stderr string
	var err error
	if fn == nil {
		stdout, stderr, err = m.execCommandResultLocked()
	}
	m.mu.Unlock()

	if fn != nil {
		stdout, stderr, err = fn(cmd)
	}
	if cmd.OnStdoutLine != nil {
		for _, line := range strings.Split(stdout, "\n") {
			cmd.OnStdoutLine(line)
		}
	}
	return stdout, stderr, err
}

// execCommandResultLocked resolves the canned answer for one call from the
// per-call sequence, falling back to the fixed fields. ExecCommandFn is not
// consulted here: it outranks both, and ExecCommand handles it separately so
// the caller's function runs outside the lock.
//
// The caller MUST already hold m.mu — this helper never takes it. ExecCommand
// calls it on the same goroutine that locked, and Go's sync.Mutex is not
// reentrant, so locking here too would deadlock every mocked execution in the
// repo rather than only a concurrent one.
func (m *MockBaseCommand) execCommandResultLocked() (string, string, error) {
	if len(m.execResults) > 0 {
		idx := m.execCallIdx
		if idx >= len(m.execResults) {
			idx = len(m.execResults) - 1
		}
		m.execCallIdx++
		r := m.execResults[idx]
		return r.Stdout, r.Stderr, r.Err
	}
	return m.ExecCommandStdout, m.ExecCommandStderr, m.ExecCommandError
}

// Setup mocks the BaseCommand.Setup method
func (m *MockBaseCommand) Setup(line string) error {
	return m.SetupError
}

// MaybeSetup mocks the BaseCommand.MaybeSetup method
func (m *MockBaseCommand) MaybeSetup(line, toSearch string) error {
	return m.MaybeSetupError
}

// MaybeSetupInFile mocks the BaseCommand.MaybeSetupInFile method
func (m *MockBaseCommand) MaybeSetupInFile(line, toSearch, filePath string) error {
	return m.MaybeSetupInFileError
}

// IsDesktopAppPresent mocks the BaseCommand.IsDesktopAppPresent method
func (m *MockBaseCommand) IsDesktopAppPresent(dirPath, appName string) (bool, error) {
	return m.IsDesktopAppPresentResult, nil
}

// IsPackagePresent mocks the BaseCommand.IsPackagePresent method
func (m *MockBaseCommand) IsPackagePresent(cmd *exec.Cmd, packageName string) (bool, error) {
	return m.IsPackagePresentResult, nil
}

// IsFontPresent mocks the BaseCommand.IsFontPresent method
func (m *MockBaseCommand) IsFontPresent(fontName string) (bool, error) {
	return m.IsFontPresentResult, m.IsFontPresentError
}

// MaybeInstall mocks the BaseCommand.MaybeInstall method
func (m *MockBaseCommand) MaybeInstall(
	itemName string,
	alias []string,
	checkInstalled func(string) (bool, error),
	installFunc func(string) error,
	installURLFunc func(string) error,
	itemType string,
) error {
	return m.MaybeInstallError
}

// InstallFontFromURL mocks the BaseCommand.InstallFontFromURL method
func (m *MockBaseCommand) InstallFontFromURL(url, fontFileName string, runCache bool) error {
	return m.InstallFontURLError
}

// Reset clears all tracked state for reuse in multiple tests
func (m *MockBaseCommand) ResetExecCommand() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecCommandCalls = []CommandParams{}
	m.ExecCommandStdout = ""
	m.ExecCommandStderr = ""
	m.ExecCommandError = nil
	m.execResults = nil
	m.execCallIdx = 0
	// Cleared with the rest: ExecCommandFn outranks every other field in
	// ExecCommand, so leaving it set would make a "reset" mock keep answering
	// from the previous subtest's callback and silently ignore whatever the
	// next one configures.
	m.ExecCommandFn = nil
}

// SetExecCommandResult configures the return values for ExecCommand
func (m *MockBaseCommand) SetExecCommandResult(stdout, stderr string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecCommandStdout = stdout
	m.ExecCommandStderr = stderr
	m.ExecCommandError = err
}

// GetLastExecCommandCall returns the most recent ExecCommand call parameters
// Returns nil if no calls have been made
func (m *MockBaseCommand) GetLastExecCommandCall() *CommandParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.ExecCommandCalls) == 0 {
		return nil
	}
	return &m.ExecCommandCalls[len(m.ExecCommandCalls)-1]
}

// GetExecCommandCallCount returns the number of times ExecCommand was called
func (m *MockBaseCommand) GetExecCommandCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ExecCommandCalls)
}

// MockGit provides a mock implementation for Git operations
// This allows tests to avoid running actual git commands
type MockGit struct {
	// Track Clone calls
	CloneCalls []struct {
		URL     string
		DstPath string
	}
	CloneError error

	// Track other Git operations
	ExecuteCommandCalls [][]string
	ExecuteCommandError error
}

// NewMockGit creates a new MockGit with sensible defaults
func NewMockGit() *MockGit {
	return &MockGit{
		CloneCalls:          []struct{ URL, DstPath string }{},
		CloneError:          nil,
		ExecuteCommandCalls: [][]string{},
		ExecuteCommandError: nil,
	}
}

// Clone mocks the Git.Clone method
func (m *MockGit) Clone(url, dstPath string) error {
	m.CloneCalls = append(m.CloneCalls, struct{ URL, DstPath string }{URL: url, DstPath: dstPath})
	return m.CloneError
}

// ExecuteCommand mocks the Git.ExecuteCommand method
func (m *MockGit) ExecuteCommand(args ...string) error {
	m.ExecuteCommandCalls = append(m.ExecuteCommandCalls, args)
	return m.ExecuteCommandError
}

// Reset clears all tracked state for reuse in multiple tests
func (m *MockGit) ResetGit() {
	m.CloneCalls = []struct{ URL, DstPath string }{}
	m.CloneError = nil
	m.ExecuteCommandCalls = [][]string{}
	m.ExecuteCommandError = nil
}

// SetCloneError configures the error returned by Clone
func (m *MockGit) SetCloneError(err error) {
	m.CloneError = err
}

// SetExecuteCommandError configures the error returned by ExecuteCommand
func (m *MockGit) SetExecuteCommandError(err error) {
	m.ExecuteCommandError = err
}

// GetLastCloneCall returns the most recent Clone call parameters
// Returns nil URL and path if no calls have been made
func (m *MockGit) GetLastCloneCall() (url string, dstPath string) {
	if len(m.CloneCalls) == 0 {
		return "", ""
	}
	lastCall := m.CloneCalls[len(m.CloneCalls)-1]
	return lastCall.URL, lastCall.DstPath
}

// GetCloneCallCount returns the number of times Clone was called
func (m *MockGit) GetCloneCallCount() int {
	return len(m.CloneCalls)
}
