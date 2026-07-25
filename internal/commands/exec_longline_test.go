package commands_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/commands"
)

// TestExecCommandCapturesLongSingleLine is a regression test for a deadlock in
// ExecCommand's output capture. It previously drained each pipe with a
// bufio.Scanner, whose 64KB per-line token limit makes Scan abort on an
// over-long line. Aborting stops draining the pipe, so the child blocks
// writing to a full pipe and Wait() hangs forever. gh's compact JSON is
// emitted as one long line and exceeds 64KB on busy PRs (this is what hung
// `dg task review-threads` on a PR with a large review comment).
//
// The command emits a single line of 200000 bytes with no newline — well past
// bufio.MaxScanTokenSize (65536). The whole line must be captured and the call
// must return; the select guards the suite against re-hanging if this regresses.
func TestExecCommandCapturesLongSingleLine(t *testing.T) {
	const n = 200000 // > bufio.MaxScanTokenSize (65536)
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, _, err := b.ExecCommand(commands.CommandParams{
			Command: "bash",
			Args:    []string{"-c", fmt.Sprintf("head -c %d /dev/zero | tr '\\0' 'A'", n)},
		})
		done <- result{out: out, err: err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("ExecCommand returned error: %v", r.err)
		}
		if len(r.out) != n {
			t.Fatalf("expected %d captured bytes, got %d", n, len(r.out))
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ExecCommand hung capturing a >64KB single line (scanner deadlock regression)")
	}
}
