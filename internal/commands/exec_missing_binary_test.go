package commands_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/logger"
)

// newObservedLogger installs a logger that records everything at debug level
// and above, and returns the recorded logs.
func newObservedLogger(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, recorded := observer.New(zap.DebugLevel)
	restore := logger.SetForTest(zap.New(core).Sugar())
	t.Cleanup(restore)
	return recorded
}

// TestExecCommandLogsMissingBinaryAtDebug pins the log level for a command
// that is not on PATH.
//
// Every "is X already installed?" probe in this codebase answers by running
// X's own version command, so a machine without X reaches ExecCommand's Start
// failure as the normal path. It used to be logged at error level, which put
// ten routine outcomes — bun, deno, elixir, erl, rustc, php, mongod, mysql,
// psql, redis-server — into one fresh-machine install's list of failures.
func TestExecCommandLogsMissingBinaryAtDebug(t *testing.T) {
	recorded := newObservedLogger(t)

	b := commands.NewBaseCommand()
	_, _, err := b.ExecCommand(commands.CommandParams{
		Command: "devgeta-no-such-binary-should-ever-exist",
		Args:    []string{"--version"},
		NoStdin: true,
	})

	// The caller still gets the error: only the logging changed.
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("expected exec.ErrNotFound to be returned to the caller, got %v", err)
	}
	if n := recorded.FilterLevelExact(zap.ErrorLevel).Len(); n != 0 {
		t.Errorf(
			"expected a missing binary to log nothing at error level, got %d entries: %v",
			n,
			recorded.FilterLevelExact(zap.ErrorLevel).All(),
		)
	}
	if recorded.FilterLevelExact(zap.DebugLevel).
		FilterMessage("command not found on PATH").
		Len() != 1 {
		t.Errorf(
			"expected one debug entry recording the missing binary, got %v",
			recorded.All(),
		)
	}
}

// TestExecCommandLogsOtherStartFailuresAtError is the other half of the same
// contract: only "not on PATH" is routine. A file that exists but cannot be
// executed is a real fault and must stay loud.
func TestExecCommandLogsOtherStartFailuresAtError(t *testing.T) {
	nonExecutable := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(nonExecutable, []byte("not a program"), 0o600); err != nil {
		t.Fatalf("failed to create the test file: %v", err)
	}

	recorded := newObservedLogger(t)

	b := commands.NewBaseCommand()
	_, _, err := b.ExecCommand(commands.CommandParams{
		Command: nonExecutable,
		NoStdin: true,
	})
	if err == nil {
		t.Fatal("expected running a non-executable file to fail")
	}
	if errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("test setup is wrong: the file should exist, got %v", err)
	}
	if recorded.FilterLevelExact(zap.ErrorLevel).Len() != 1 {
		t.Errorf(
			"expected a non-executable file to still log at error level, got %v",
			recorded.All(),
		)
	}
}
