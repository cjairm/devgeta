package commands_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/commands"
)

// These tests exercise the real ExecCommand rather than a mock, for the same
// reason as env_overlay_test.go and exec_longline_test.go: what is under test
// is the wiring of exec.Cmd itself — the order of Wait against the pipe
// readers, and what the child finds on fd 0 — and a mock can only prove a
// caller filled in CommandParams. internal/commands is the boundary that
// shells out (CLAUDE.md §6). Every command below is a hermetic `bash -c` that
// touches nothing outside the process: no packages, no user files, no network.

// TestExecCommandCapturesOutputAfterChildExits is a regression test for
// truncated output. ExecCommand used to call Wait before joining the two
// goroutines draining stdout and stderr. Wait closes the parent's ends of
// those pipes as it returns, so it raced the readers — os/exec documents that
// it is incorrect to call Wait before all reads from the pipe have completed.
//
// The command makes the race deterministic: the shell forks a background child
// that writes only after the shell itself has exited. Under the old ordering
// Wait returned at the shell's exit and closed the pipes out from under the
// readers, losing the line entirely. The reader must instead be allowed to
// read to real EOF, which arrives once the last writer is gone.
func TestExecCommandCapturesOutputAfterChildExits(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, _, err := b.ExecCommand(commands.CommandParams{
			Command: "bash",
			Args:    []string{"-c", "(sleep 0.4; echo written-after-exit) & exit 0"},
		})
		done <- result{out: out, err: err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("ExecCommand returned error: %v", r.err)
		}
		if r.out != "written-after-exit" {
			t.Fatalf("output written after the child exited was lost: got %q", r.out)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ExecCommand hung draining the pipes")
	}
}

// TestExecCommandTimeoutUnblocksHeldPipes guards the other side of that
// ordering. Joining the readers first means ExecCommand no longer returns
// while a writer still holds the pipe — correct, but on its own it would let a
// process that outlives the command block ExecCommand forever and defeat the
// Timeout callers depend on. The deadline must close the read ends so the
// readers finish and the call returns.
//
// Here the shell exits immediately but leaves a background sleep holding both
// pipes far longer than the timeout. Without the deadline-driven close the
// call blocks for the sleep's full duration instead of the timeout's.
func TestExecCommandTimeoutUnblocksHeldPipes(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	done := make(chan error, 1)
	go func() {
		_, _, err := b.ExecCommand(commands.CommandParams{
			Command: "bash",
			Args:    []string{"-c", "(sleep 5) & exit 0"},
			Timeout: 300 * time.Millisecond,
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("expected the timeout to be reported, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout did not bound the call: a process holding the pipes blocked the readers")
	}
}

// TestExecCommandNoStdin covers both directions of the stdin opt-out, because
// only the pair is meaningful: the default MUST keep inheriting devgeta's
// stdin (`dg install` shells out to sudo, which reads the password from the
// terminal) while callers that must never block on a human — anything on a
// timeout or behind a TUI — must be able to disconnect it.
//
// os.Stdin is replaced with a pipe holding a known line so the two cases are
// distinguishable; the child just echoes whatever it can read.
func TestExecCommandNoStdin(t *testing.T) {
	cases := []struct {
		name    string
		noStdin bool
		want    string
	}{
		{name: "default inherits the process stdin", noStdin: false, want: "typed-by-a-human"},
		{name: "NoStdin disconnects it", noStdin: true, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe failed: %v", err)
			}
			if _, err := w.WriteString("typed-by-a-human\n"); err != nil {
				t.Fatalf("writing to the fake stdin failed: %v", err)
			}
			// Close the writer up front so the child sees EOF and the read
			// terminates; otherwise `cat` would wait on an open pipe.
			if err := w.Close(); err != nil {
				t.Fatalf("closing the fake stdin writer failed: %v", err)
			}

			realStdin := os.Stdin
			os.Stdin = r
			t.Cleanup(func() {
				os.Stdin = realStdin
				if err := r.Close(); err != nil {
					t.Logf("closing the fake stdin reader: %v", err)
				}
			})

			b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})
			out, _, err := b.ExecCommand(commands.CommandParams{
				Command: "bash",
				Args:    []string{"-c", "cat"},
				NoStdin: tc.noStdin,
			})
			if err != nil {
				t.Fatalf("ExecCommand returned error: %v", err)
			}
			if out != tc.want {
				t.Fatalf("expected child stdin to yield %q, got %q", tc.want, out)
			}
		})
	}
}
