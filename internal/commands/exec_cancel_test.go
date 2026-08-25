//go:build unix

// The process-group assertions below need syscall.Kill and the executor's own
// SysProcAttr{Setpgid: true}, neither of which exists on Windows — the package
// does not build there at all, and devgeta targets macOS and Debian/Ubuntu
// (CLAUDE.md §8). The constraint above states that, rather than a runtime skip
// that could never run.

package commands_test

import (
	"context"
	"errors"
	"os"
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
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	stdout, _, err := b.ExecCommand(commands.CommandParams{
		Command: "bash",
		Args:    []string{"-c", "sleep 30 & echo $!; wait"},
		Timeout: 400 * time.Millisecond,
		NoStdin: true,
	})

	// Read the pid and arm the cleanup before asserting anything about the
	// outcome. Every assertion below ends in t.Fatal, so a cleanup placed after
	// one is a cleanup the failing path skips — and the process it would have
	// reaped is a `sleep 30` left running for the rest of the suite.
	pid, convErr := strconv.Atoi(strings.TrimSpace(stdout))
	if convErr != nil {
		t.Fatalf("expected the grandchild's pid on stdout, got %q: %v", stdout, convErr)
	}
	t.Cleanup(func() {
		// ESRCH is the passing case: the group kill already reached it.
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil &&
			!errors.Is(err, syscall.ESRCH) {
			t.Logf("cleaning up the surviving grandchild %d: %v", pid, err)
		}
	})

	if err == nil {
		t.Fatal("expected the deadline to be reported for a command still running at it")
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

	t.Fatalf("grandchild %d outlived the deadline: the kill did not reach the process group", pid)
}

// TestExecCommandRootContextCancelKillsProcessGroup covers task 1b's fix: a
// Ctrl-C (SIGINT) or SIGTERM must reach a NoStdin command's process group the
// same way a Timeout already does, by cancelling the process-level context
// SetRootContext installs (in production, cmd.Execute wires this to
// signal.NotifyContext). Without that wiring — the regression this task
// fixes — a killed devgeta left the whole child tree running.
//
// Asserting on the grandchild specifically matters: a test that only checked
// the direct bash child would still pass against the bug, since the direct
// child's own Cancel-driven kill was never in question — only whether it
// reaches what that child forked.
func TestExecCommandRootContextCancelKillsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	commands.SetRootContext(ctx)
	t.Cleanup(func() { commands.SetRootContext(context.Background()) })

	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	pidLine := make(chan string, 1)
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, _, err := b.ExecCommand(commands.CommandParams{
			Command: "bash",
			Args:    []string{"-c", "sleep 30 & echo $!; wait"},
			NoStdin: true,
			OnStdoutLine: func(line string) {
				select {
				case pidLine <- line:
				default:
				}
			},
		})
		done <- result{out: out, err: err}
	}()

	var pid int
	select {
	case line := <-pidLine:
		p, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr != nil {
			t.Fatalf("expected the grandchild's pid on stdout, got %q: %v", line, convErr)
		}
		pid = p
	case <-time.After(5 * time.Second):
		t.Fatal("never saw the grandchild's pid")
	}
	// Arm the cleanup before cancelling: every assertion below ends in
	// t.Fatal, and a cleanup placed after one would be skipped, leaving a
	// `sleep 30` running for the rest of the suite.
	t.Cleanup(func() {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil &&
			!errors.Is(err, syscall.ESRCH) {
			t.Logf("cleaning up the surviving grandchild %d: %v", pid, err)
		}
	})

	cancel() // stands in for cmd.Execute's SIGINT/SIGTERM handler firing

	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("expected an error once the root context is cancelled mid-run")
		}
		if !strings.Contains(r.err.Error(), "interrupted") {
			t.Fatalf("expected an interrupted error, got: %v", r.err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("ExecCommand did not return after the root context was cancelled")
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

	t.Fatalf(
		"grandchild %d outlived root-context cancellation: the kill did not reach the process group",
		pid,
	)
}

// TestExecCommandDefaultRootContextLeavesProcessGroupUntouched covers the
// other half of task 1b's requirement: a test binary (or any library caller
// that never goes through cmd.Execute) must see exactly the behavior that
// existed before SetRootContext did — an uncancellable root, so a
// zero-Timeout NoStdin call still gets no process group. The Setpgid gate is
// `cmd.NoStdin && ctx.Done() != nil`; ctx.Done() is nil here because there is
// no Timeout and nothing has called commands.SetRootContext in this test
// binary process (every other test that does restores the default via
// t.Cleanup, so this one sees it too).
//
// isolateProcessGroup is verified indirectly, by comparing the child's own
// process group to this test process's — the only externally observable
// effect of Setpgid.
func TestExecCommandDefaultRootContextLeavesProcessGroupUntouched(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	pidLine := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, _, err := b.ExecCommand(commands.CommandParams{
			Command: "bash",
			Args:    []string{"-c", "echo $$; sleep 5"},
			NoStdin: true,
			OnStdoutLine: func(line string) {
				select {
				case pidLine <- line:
				default:
				}
			},
		})
		done <- err
	}()

	var pid int
	select {
	case line := <-pidLine:
		p, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr != nil {
			t.Fatalf("expected the child's pid on stdout, got %q: %v", line, convErr)
		}
		pid = p
	case <-time.After(5 * time.Second):
		t.Fatal("never saw the child's pid")
	}
	t.Cleanup(func() {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil &&
			!errors.Is(err, syscall.ESRCH) {
			t.Logf("cleaning up child %d: %v", pid, err)
		}
	})

	childPgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid(%d): %v", pid, err)
	}
	ownPgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Getpgid(self): %v", err)
	}
	if childPgid != ownPgid {
		t.Fatalf(
			"child got its own process group (%d, test process group %d) with no "+
				"Timeout and an uninitialized root context; the Setpgid gate must "+
				"stay closed when ctx.Done() is nil",
			childPgid, ownPgid,
		)
	}

	// End the child ourselves rather than waiting out its sleep; it is in
	// this test's own process group, so a plain single-pid kill is safe and
	// cannot touch anything else.
	if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil &&
		!errors.Is(killErr, syscall.ESRCH) {
		t.Fatalf("failed to end the test child: %v", killErr)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ExecCommand did not return after its child was killed")
	}
}
