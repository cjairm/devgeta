package commands

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

// TestCloseUnstartedPipes covers the descriptor leak on ExecCommand's setup
// failure path. StdoutPipe and StderrPipe each allocate an OS pipe: exec.Cmd
// closes the child's (write) ends in Start and the parent's (read) ends in
// Wait. When StderrPipe fails after StdoutPipe succeeded, the command is
// abandoned before Start, so neither ever runs and both descriptors stay open
// for the life of the process — one leak per failed call.
//
// The failure cannot be injected through the public API, which is why the
// cleanup lives in a helper: the helper is what is testable. Closing an
// *os.File twice reports os.ErrClosed, so a second Close is what proves each
// end was already closed.
func TestCloseUnstartedPipes(t *testing.T) {
	c := exec.Command("true")

	stdoutPipe, err := c.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe failed: %v", err)
	}
	stderrPipe, err := c.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe failed: %v", err)
	}

	// Grab the write ends before the helper runs: exec.Cmd holds them as
	// Stdout/Stderr, and they leak just as surely as the read ends.
	stdoutWriter, ok := c.Stdout.(*os.File)
	if !ok {
		t.Fatalf("expected exec.Cmd.Stdout to be the pipe's write end, got %T", c.Stdout)
	}
	stderrWriter, ok := c.Stderr.(*os.File)
	if !ok {
		t.Fatalf("expected exec.Cmd.Stderr to be the pipe's write end, got %T", c.Stderr)
	}

	closeUnstartedPipes(c, stdoutPipe, stderrPipe)

	ends := map[string]interface{ Close() error }{
		"stdout read":  stdoutPipe,
		"stderr read":  stderrPipe,
		"stdout write": stdoutWriter,
		"stderr write": stderrWriter,
	}
	for name, end := range ends {
		if err := end.Close(); !errors.Is(err, os.ErrClosed) {
			t.Errorf("%s end was left open: second Close returned %v, want os.ErrClosed", name, err)
		}
	}
}

// TestCloseUnstartedPipesToleratesPartialSetup covers the shape the caller
// actually uses: StdoutPipe succeeded and StderrPipe did not, so there is only
// one reader to close and Stderr was never assigned. The helper must not panic
// on the nil half.
func TestCloseUnstartedPipesToleratesPartialSetup(t *testing.T) {
	c := exec.Command("true")

	stdoutPipe, err := c.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe failed: %v", err)
	}

	closeUnstartedPipes(c, stdoutPipe)

	if err := stdoutPipe.Close(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("stdout read end was left open: second Close returned %v, want os.ErrClosed", err)
	}
}
