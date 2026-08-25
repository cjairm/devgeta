package commands_test

import (
	"os"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/commands"
)

// These tests exercise the real ExecCommand rather than a mock, for the same
// reason as env_overlay_test.go and exec_longline_test.go: what is under test
// is the wiring of exec.Cmd itself — how long its output capture outlives the
// command, and what the child finds on fd 0 — and a mock can only prove a
// caller filled in CommandParams. internal/commands is the boundary that
// shells out (CLAUDE.md §6). Every command below is a hermetic `bash -c` that
// touches nothing outside the process: no packages, no user files, no network.

// TestExecCommandCapturesOutputAfterChildExits is a regression test for
// truncated output. ExecCommand used to stop reading the moment the direct
// child exited, cutting off anything a process it left behind wrote next.
//
// The command makes that deterministic: the shell forks a background child
// that writes only after the shell itself has exited. The write has to land
// inside the post-exit drain grace, which is what makes the grace a grace and
// not just a timeout — 400ms against outputDrainGrace's two seconds.
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

// TestExecCommandReturnsWhileAGrandchildHoldsThePipes guards the other side of
// that ordering, and it needs no Timeout to do it.
//
// Not truncating means ExecCommand cannot return while a writer still holds
// the pipe — right for output, but on its own it lets any process that outlives
// the command block ExecCommand for that process's whole lifetime. The escape
// hatch used to be the deadline closing the pipes, and only 5 of ExecCommand's
// ~107 callers set one, so everything else was unbounded: `curl … | sh` that
// installs a daemon, `brew services start`, an agent that spawns a helper.
//
// exec.Cmd's WaitDelay is that bound now, and it applies to every call. Below,
// the shell writes a line, forks a child that holds both pipes far longer than
// the grace, and exits 0. The call must come back about a grace after the
// shell's exit — not ten seconds later — with the pre-exit line intact and no
// error: the command succeeded, and what a grandchild does with a descriptor
// it inherited is not a failure of the command.
func TestExecCommandReturnsWhileAGrandchildHoldsThePipes(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	type result struct {
		out     string
		err     error
		elapsed time.Duration
	}
	done := make(chan result, 1)
	go func() {
		start := time.Now()
		out, _, err := b.ExecCommand(commands.CommandParams{
			Command: "bash",
			Args:    []string{"-c", "echo before-exit; (sleep 10) & exit 0"},
		})
		done <- result{out: out, err: err, elapsed: time.Since(start)}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf(
				"the command exited 0; a grandchild holding its pipes is not its failure: %v",
				r.err,
			)
		}
		if r.out != "before-exit" {
			t.Fatalf("output written before the child exited was lost: got %q", r.out)
		}
		if r.elapsed >= 6*time.Second {
			t.Fatalf(
				"ExecCommand waited on the grandchild instead of the drain grace: returned after %s",
				r.elapsed,
			)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ExecCommand blocked for the grandchild's lifetime instead of the drain grace")
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
