package commands

import (
	"bytes"
	"io"
	"strings"

	"github.com/cjairm/devgeta/pkg/logger"
)

// outputWriter is what ExecCommand hands exec.Cmd as a child's Stdout or
// Stderr. It captures everything the child writes for the strings ExecCommand
// returns and, unless the caller asked for raw streaming, frames that byte
// stream into lines for the debug log and CommandParams.OnStdoutLine.
//
// Handing exec a plain io.Writer instead of taking StdoutPipe/StderrPipe and
// draining them here is load-bearing, not a refactor. exec.Cmd's WaitDelay —
// the grace period that stops a process outliving the command from blocking
// Wait forever — only polices copying goroutines exec itself owns, and
// StdoutPipe/StderrPipe create none: they allocate a pipe, hand the caller the
// read end, and leave the copying to the caller. With no copy of its own to
// time out, exec never arms the grace period, so setting WaitDelay while the
// caller owns the pipes does nothing at all. Any writer that is not an
// *os.File makes exec create both the pipe and the copying goroutine, which is
// what arms it.
type outputWriter struct {
	// captured is everything the child wrote, verbatim. It is what ExecCommand
	// hands back to its caller.
	captured strings.Builder
	// tee, when non-nil (CommandParams.Stream), also copies the raw bytes to
	// devgeta's own stdout/stderr so a caller sees real-time progress. That
	// path has no line framing at all, so label and onLine go unused on it —
	// no caller needs both, and Stream's contract is raw bytes.
	tee io.Writer
	// label names the stream in the debug log ("stdout" or "stderr").
	label string
	// onLine, when non-nil, receives each complete line, newline trimmed, as
	// soon as it arrives.
	onLine func(string)
	// pending holds the bytes written since the last newline: writes do not
	// arrive on line boundaries, and a line is not a line until one does.
	//
	// It grows without bound on purpose. A fixed-size framer — bufio.Scanner's
	// 64KB token limit is the usual one — gives up on an over-long line and
	// stops consuming the stream, which leaves the child blocked writing to a
	// full pipe. gh's compact JSON is emitted as a single line and routinely
	// exceeds 64KB on busy PRs, so arbitrarily long lines have to work.
	pending []byte
}

func newOutputWriter(label string, tee io.Writer, onLine func(string)) *outputWriter {
	return &outputWriter{label: label, tee: tee, onLine: onLine}
}

func (w *outputWriter) Write(p []byte) (int, error) {
	w.captured.Write(p)

	if w.tee != nil {
		if _, err := w.tee.Write(p); err != nil {
			return 0, err
		}
		return len(p), nil
	}

	rest := p
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			break
		}
		w.pending = append(w.pending, rest[:i]...)
		w.emit()
		rest = rest[i+1:]
	}
	w.pending = append(w.pending, rest...)
	return len(p), nil
}

// flushPending emits whatever the child wrote after its last newline. A
// process is under no obligation to terminate its final line, and dropping the
// fragment would silently lose the last line of any tool that does not — so it
// is delivered once the stream is known to be over. Call it only after
// exec.Cmd.Wait has returned: Wait joins the copying goroutines on every path,
// which is what makes reading and mutating this writer safe from here.
func (w *outputWriter) flushPending() {
	if len(w.pending) > 0 {
		w.emit()
	}
}

func (w *outputWriter) emit() {
	line := string(w.pending)
	w.pending = w.pending[:0]
	logger.L().Debugw(w.label, "line", line)
	if w.onLine != nil {
		w.onLine(line)
	}
}
