package commands_test

import (
	"errors"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/commands"
)

// Like exec_pipes_test.go, these drive the real ExecCommand: a mock records
// the CommandParams a caller built and never reaches exec.CommandContext, so
// it cannot fork a grandchild, hold a pipe open past a child's exit, or be
// asked about a process group — a mocked version of every assertion below
// would pass against the unfixed executor. internal/commands is the boundary
// that shells out (CLAUDE.md §6). testutil.VerifyNoRealCommands deliberately
// does not appear here: it asserts a mock base ran nothing, and these tests
// run something on purpose. Every command is a hermetic `bash -c` that touches
// nothing outside the process.

// TestExecCommandDeadlineErrorOnlyOnFailure covers what a Timeout is allowed
// to say about a command's outcome.
//
// ExecCommand used to rewrite err to "command timed out" whenever the deadline
// had passed, without looking at whether Wait had failed. That is reachable
// with a command that succeeded: the child exits 0, something it left behind
// holds the inherited pipe past the deadline, Wait returns nil — and the
// caller is told its command timed out. Callers that roll back on error then
// roll back completed work.
func TestExecCommandDeadlineErrorOnlyOnFailure(t *testing.T) {
	cases := []struct {
		name      string
		script    string
		timeout   time.Duration
		wantErr   bool
		wantOut   string
		maxWait   time.Duration
		errSubstr string
	}{
		{
			name:    "plain success well inside the deadline",
			script:  "echo done",
			timeout: 10 * time.Second,
			wantOut: "done",
			maxWait: 5 * time.Second,
		},
		{
			// The command finished; only its output outlived the deadline.
			name:    "success whose output is held past the deadline",
			script:  "echo done; (sleep 10) & exit 0",
			timeout: 300 * time.Millisecond,
			wantOut: "done",
			maxWait: 6 * time.Second,
		},
		{
			// The command really was still running, so the deadline really is
			// the diagnosis.
			name:      "still running at the deadline",
			script:    "sleep 10",
			timeout:   300 * time.Millisecond,
			wantErr:   true,
			errSubstr: "timed out",
			maxWait:   6 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

			type result struct {
				out string
				err error
			}
			done := make(chan result, 1)
			go func() {
				out, _, err := b.ExecCommand(commands.CommandParams{
					Command: "bash",
					Args:    []string{"-c", tc.script},
					Timeout: tc.timeout,
				})
				done <- result{out: out, err: err}
			}()

			select {
			case r := <-done:
				if tc.wantErr {
					if r.err == nil {
						t.Fatalf("expected an error mentioning %q, got none", tc.errSubstr)
					}
					if !strings.Contains(r.err.Error(), tc.errSubstr) {
						t.Fatalf("expected an error mentioning %q, got: %v", tc.errSubstr, r.err)
					}
					return
				}
				if r.err != nil {
					t.Fatalf("the command succeeded; the deadline must not be reported: %v", r.err)
				}
				if r.out != tc.wantOut {
					t.Fatalf("expected captured stdout %q, got %q", tc.wantOut, r.out)
				}
			case <-time.After(tc.maxWait):
				t.Fatalf("ExecCommand did not return within %s", tc.maxWait)
			}
		})
	}
}

// TestExecCommandCancelKillsTheProcessGroup covers what the deadline reaches.
//
// exec.CommandContext's default cancel is Process.Kill on the direct child
// alone, and everything devgeta shells out to forks — so a timeout used to
// leave the forks running with nothing to reap them. The executor gives a
// child its own process group and signals the group instead, gated on NoStdin
// because a child in its own group that reads the terminal is stopped by the
// kernel with SIGTTIN (see isolateProcessGroup).
//
// The shell prints the pid of a background child that outlives the deadline by
// far, then waits on it. After the timeout that pid must be gone.
func TestExecCommandCancelKillsTheProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups and negative-pid signals are Unix-only")
	}
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	stdout, _, err := b.ExecCommand(commands.CommandParams{
		Command: "bash",
		Args:    []string{"-c", "sleep 30 & echo $!; wait"},
		Timeout: 400 * time.Millisecond,
		NoStdin: true,
	})
	if err == nil {
		t.Fatal("expected the deadline to be reported for a command still running at it")
	}

	pid, convErr := strconv.Atoi(strings.TrimSpace(stdout))
	if convErr != nil {
		t.Fatalf("expected the grandchild's pid on stdout, got %q: %v", stdout, convErr)
	}

	// Signal 0 checks for the process's existence without sending anything.
	// Poll rather than sample once: the grandchild is reparented on its
	// parent's death and stays a zombie until whoever adopted it reaps.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Don't leave it behind for the next test to trip over.
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Logf("cleaning up the surviving grandchild: %v", err)
	}
	t.Fatalf("grandchild %d outlived the deadline: the kill did not reach the process group", pid)
}
