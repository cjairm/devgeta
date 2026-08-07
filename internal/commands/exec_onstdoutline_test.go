package commands_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/commands"
)

// CommandParams.OnStdoutLine must deliver each stdout line DURING the run, not
// in a batch afterwards — that is the whole reason it exists (a headless
// reviewer run takes minutes, and its caller needs to report progress before
// it ends). The child below writes a line, waits, then writes another, so a
// callback that only fired at the end could not observe the first line before
// the second was written.
func TestExecCommandOnStdoutLineDeliversLinesDuringTheRun(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	var mu sync.Mutex
	var lines []string
	firstSeen := make(chan struct{})

	out, _, err := b.ExecCommand(commands.CommandParams{
		Command: "bash",
		Args:    []string{"-c", "echo first; sleep 0.3; echo second"},
		OnStdoutLine: func(line string) {
			mu.Lock()
			lines = append(lines, line)
			isFirst := len(lines) == 1
			mu.Unlock()
			if isFirst {
				close(firstSeen)
			}
		},
	})
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	select {
	case <-firstSeen:
	case <-time.After(time.Second):
		t.Fatal("the first line was never delivered")
	}

	mu.Lock()
	got := strings.Join(lines, "|")
	mu.Unlock()
	if got != "first|second" {
		t.Errorf("expected both lines in order, got %q", got)
	}
	// The captured return value is unaffected: callers read the whole output
	// after the run as they always have.
	if out != "first\nsecond" {
		t.Errorf("expected the full captured stdout, got %q", out)
	}
}

// Lines arrive newline-trimmed, so a caller can parse one without stripping
// the terminator itself, and stderr is never mixed in — the hook is stdout's
// alone (a tool that logs to stderr would otherwise corrupt an event stream a
// caller is parsing).
func TestExecCommandOnStdoutLineIsTrimmedAndStdoutOnly(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	var lines []string
	if _, _, err := b.ExecCommand(commands.CommandParams{
		Command:      "bash",
		Args:         []string{"-c", "echo to-stdout; echo to-stderr 1>&2"},
		OnStdoutLine: func(line string) { lines = append(lines, line) },
	}); err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	if len(lines) != 1 || lines[0] != "to-stdout" {
		t.Fatalf("expected exactly the stdout line, untrimmed of nothing else, got %v", lines)
	}
}

// A long single line must still reach the callback whole: the reader is a
// bufio.Reader precisely so a line past bufio's 64KB token limit does not
// abort the read, and an event stream can carry lines that long.
func TestExecCommandOnStdoutLineHandlesLongLines(t *testing.T) {
	const n = 200000 // > bufio.MaxScanTokenSize (65536)
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	var total int
	if _, _, err := b.ExecCommand(commands.CommandParams{
		Command:      "bash",
		Args:         []string{"-c", fmt.Sprintf("head -c %d /dev/zero | tr '\\0' 'A'; echo", n)},
		OnStdoutLine: func(line string) { total += len(line) },
	}); err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}
	if total != n {
		t.Errorf("expected the whole %d-byte line delivered, got %d bytes", n, total)
	}
}
