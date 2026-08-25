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

// A long single line must still reach the callback whole. The framing buffer
// grows to hold whatever arrives before a newline, with no token-size ceiling
// to give up at, so an event stream can carry lines far past the 64KB a
// fixed-size framer would abort on.
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

// A process is under no obligation to terminate its last line, and everything
// the framer delivers is triggered by a newline — so without the post-Wait
// flush the tail of any tool that does not end in one is silently dropped from
// both OnStdoutLine and the captured output. `printf` writes exactly that: one
// unterminated line and nothing else, so a callback that only fires on newlines
// would receive nothing at all here.
func TestExecCommandOnStdoutLineDeliversAnUnterminatedFinalLine(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	var lines []string
	out, _, err := b.ExecCommand(commands.CommandParams{
		Command:      "bash",
		Args:         []string{"-c", "printf 'no-newline'"},
		OnStdoutLine: func(line string) { lines = append(lines, line) },
	})
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	if len(lines) != 1 || lines[0] != "no-newline" {
		t.Fatalf("expected the unterminated final line delivered, got %v", lines)
	}
	if out != "no-newline" {
		t.Errorf("expected the full captured stdout, got %q", out)
	}
}

// The same guarantee for a line that follows terminated ones: the flush must
// add the trailing fragment, not replace or duplicate what already arrived.
func TestExecCommandOnStdoutLineFlushesTheTailAfterCompleteLines(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	var lines []string
	if _, _, err := b.ExecCommand(commands.CommandParams{
		Command:      "bash",
		Args:         []string{"-c", "printf 'first\\nsecond\\ntail'"},
		OnStdoutLine: func(line string) { lines = append(lines, line) },
	}); err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}

	if got := strings.Join(lines, "|"); got != "first|second|tail" {
		t.Errorf("expected every line including the unterminated tail, got %q", got)
	}
}
