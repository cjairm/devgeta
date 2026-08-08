// review-run's live progress reporting: what a human (or an agent) watching a
// headless review round sees while it is still running.
//
// This is deliberately separate from the round's stdout. stdout is the
// parseable contract (one verdict line per reviewer) and must not move;
// everything here goes to stderr and exists only so a multi-minute run reads
// as working rather than wedged. Two rules follow from that split, and both
// are load-bearing:
//
//   - Nothing here is ever the source of a verdict. classifyReviewerRun reads
//     the whole captured stdout after the run; this only summarizes events as
//     they pass by.
//   - The reviewer's own prose is never printed. A reviewer's report — its
//     findings and its reasoning — belongs in the journal (`review-notes`),
//     not spilled across the terminal at the end of a round where it is
//     unreadable and, for an agent caller, pure token cost.
//
// The same token cost is why the tool-by-tool stream is not the default. A real
// round makes on the order of 200 tool calls, and /review-loop captures this
// stderr for every round it runs, so a line each was paying tokens for a
// scrolling log nobody reads back. By default a round prints a sampled
// heartbeat instead — see progressHeartbeatInterval — and only `--verbose`
// restores the line-per-tool-call stream.
package task

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxToolLabelLen caps a tool's label (the file it read, the command it ran)
// so one tool call stays one short line. Shorter than maxReasonLen: a reason
// is the only explanation of a failure and worth the width, while a tool
// label is orientation — the reader needs to recognize what the reviewer is
// doing, not read the whole command.
const maxToolLabelLen = 70

// toolInputKeys lists the input fields a tool label is taken from, in the
// order they are tried. These are OpenCode's own tool argument names, observed
// in real `opencode run --format json` captures: `command` (bash), `filePath`
// (read/write/edit), `pattern` (glob/grep), `name` (skill). A tool whose input
// carries none of them (e.g. todowrite, whose input is a whole todo list) is
// reported by name alone rather than with a guessed-at summary.
var toolInputKeys = []string{"command", "filePath", "pattern", "name"}

// progressHeartbeatInterval is the least time between two heartbeat lines: a
// quiet (non-verbose) round prints at most one line per reviewer per interval,
// however many tool calls happened inside it. 30s is short enough that a
// reader glancing at a stalled-looking round gets an answer quickly, and long
// enough that a multi-minute reviewer costs a handful of lines instead of a
// couple of hundred.
//
// It is deliberately a constant and not a flag: the choice a caller actually
// has is "sampled or everything" (--verbose), and a tunable interval would be
// a third thing to explain for no decision anyone needs to make.
const progressHeartbeatInterval = 30 * time.Second

// reviewerProgress renders one reviewer's run as it happens: a start line,
// progress while it works, and a closing line carrying the outcome, the
// elapsed time, and what the run cost.
//
// What "progress while it works" means depends on verbose. Quiet (the
// default) samples: a tool call prints a heartbeat only when
// progressHeartbeatInterval has passed since the last printed line, and that
// line carries the running counters plus whichever tool call happened to
// trigger it. Verbose prints every tool call, which is what a human debugging
// a reviewer wants and what an agent capturing stderr does not.
//
// Sampling is driven by the tool calls themselves rather than by a timer, so
// a reviewer that goes quiet prints nothing until it acts again. That is the
// deliberate trade for staying single-threaded: a ticker would need a
// goroutine writing to p.out concurrently with the stdout drain below, and the
// lock that would then be required buys nothing a reader misses.
//
// It is written to from two places — the round loop (started/finished) and the
// executor's stdout-draining goroutine (line, via
// CommandParams.OnStdoutLine) — but never from both at once: the round loop
// is blocked inside OpenCode.Run for the entire time lines can arrive, and
// ExecCommand joins that goroutine before returning. So no lock is needed
// here, and adding one would suggest a concurrency this type does not have.
type reviewerProgress struct {
	out     io.Writer
	prefix  string // "[1/2]" — which reviewer of how many
	label   string // the model name, or defaultModelLabel
	verbose bool   // print every tool call instead of sampling
	now     func() time.Time

	// start is when this reviewer began, and lastLine when a progress line was
	// last printed for it. Both are stamped at construction rather than by
	// started(), so a reviewerProgress used without it still has a real clock
	// instead of a zero time that would read as decades elapsed.
	start    time.Time
	lastLine time.Time

	// reported is the callIDs already counted, so a tool that emits more than
	// one event (pending, then running, then completed) is still one call.
	reported map[string]bool
	tools    int
	// cost accumulates every step's own cost, as OpenCode reports it. Zero
	// means the stream never reported any — a local model, or a run that died
	// before its first step finished — and is omitted rather than printed as
	// "$0.00", which would read as a claim that the round was free.
	cost float64
}

// newReviewerProgress starts the clock for one reviewer. now is the caller's
// clock (TaskManager.now), so a test drives the heartbeat with a fixed one
// rather than racing the wall clock; verbose comes from the root --verbose
// flag.
func newReviewerProgress(
	out io.Writer,
	prefix, label string,
	verbose bool,
	now func() time.Time,
) *reviewerProgress {
	started := now()
	return &reviewerProgress{
		out:      out,
		prefix:   prefix,
		label:    label,
		verbose:  verbose,
		now:      now,
		start:    started,
		lastLine: started,
		reported: map[string]bool{},
	}
}

// started announces the reviewer before its first event arrives — the run can
// spend a while resolving a provider before it does anything worth reporting.
func (p *reviewerProgress) started() {
	fmt.Fprintf(p.out, "%s %s: running\n", p.prefix, p.label)
}

// retrying announces that an attempt produced no report and the reviewer is
// being launched one more time, naming the outcome that attempt would have
// been reported as.
//
// This is printed rather than left silent because the retry is otherwise
// invisible: the reviewer's line would simply take twice as long, which reads
// as a slow provider rather than a failed attempt being paid for again. The
// reason is carried along so a permanent cause (an auto-rejected permission,
// say) is visible on the FIRST failure instead of only in the closing line
// after both attempts have been spent.
// It prints in quiet mode too, unlike a tool call: this is an event, not a
// sample of ongoing work, and a round that silently paid for two attempts is
// exactly what the sampling is not meant to hide. It stamps lastLine for the
// same reason a heartbeat does — the field means "when a progress line was
// last printed", so leaving it stale here would let the sampler follow this
// announcement with a heartbeat that adds nothing.
func (p *reviewerProgress) retrying(outcome string) {
	fmt.Fprintf(p.out, "%s %s: %s — no report, retrying once\n", p.prefix, p.label, outcome)
	p.lastLine = p.now()
}

// finished closes the reviewer out with the outcome the round will report.
// The elapsed time is measured from construction rather than passed in, so the
// closing line and the heartbeats above it can never disagree about when this
// reviewer began.
func (p *reviewerProgress) finished(outcome string) {
	fmt.Fprintf(
		p.out, "%s %s: %s (%s)\n",
		p.prefix, p.label, outcome, p.summary(p.now().Sub(p.start)),
	)
}

// summary is the parenthetical on the closing line: always the elapsed time,
// plus the tool count and accrued cost when the run reported them.
func (p *reviewerProgress) summary(elapsed time.Duration) string {
	parts := []string{formatElapsed(elapsed)}
	if p.tools > 0 {
		parts = append(parts, fmt.Sprintf("%d tools", p.tools))
	}
	if p.cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", p.cost))
	}
	return strings.Join(parts, ", ")
}

// line takes one raw stdout line from the run and reports it if it says
// something a reader would want to know. Anything it cannot read — a
// non-event line, an event with an unexpected payload shape — is skipped
// silently: this is progress reporting, and a malformed event must never turn
// into an error or noise on top of a round that is otherwise fine.
func (p *reviewerProgress) line(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	var ev ocEvent
	if json.Unmarshal([]byte(raw), &ev) != nil {
		return
	}
	switch ev.Type {
	case "tool_use":
		p.reportTool(ev.Part)
	case "step_finish":
		var part struct {
			Cost float64 `json:"cost"`
		}
		if json.Unmarshal(ev.Part, &part) == nil {
			p.cost += part.Cost
		}
	}
}

// reportTool takes one tool call the reviewer made, the first time that call
// is seen: it always counts, and it prints according to the sampling rule
// above — every call under --verbose, at most one line per
// progressHeartbeatInterval otherwise.
func (p *reviewerProgress) reportTool(rawPart json.RawMessage) {
	var part struct {
		Tool   string `json:"tool"`
		CallID string `json:"callID"`
		State  struct {
			Input map[string]json.RawMessage `json:"input"`
		} `json:"state"`
	}
	if json.Unmarshal(rawPart, &part) != nil || part.Tool == "" {
		return
	}
	// A stream that omits callID would otherwise collapse every tool call into
	// one reported line, so an absent id counts as its own call rather than a
	// repeat of the last one.
	if part.CallID != "" {
		if p.reported[part.CallID] {
			return
		}
		p.reported[part.CallID] = true
	}
	p.tools++

	desc := part.Tool
	if label := toolLabel(part.State.Input); label != "" {
		desc += " " + label
	}
	if p.verbose {
		fmt.Fprintln(p.out, p.prefix+"   "+desc)
		return
	}
	now := p.now()
	if now.Sub(p.lastLine) < progressHeartbeatInterval {
		return
	}
	p.lastLine = now
	// The heartbeat reuses summary() rather than restating the counters: it is
	// the same "where is this run" answer the closing line gives, taken
	// mid-run, and two formatters for it would be free to drift apart. The
	// tool that happened to trigger the sample follows, so the line says what
	// the reviewer is doing and not only that it is still doing something.
	fmt.Fprintf(p.out, "%s   ... %s - %s\n", p.prefix, p.summary(now.Sub(p.start)), desc)
}

// toolLabel summarizes a tool call's arguments into a few words, or "" when
// the input carries nothing recognizable. Only string values are used: the
// keys in toolInputKeys are all strings in OpenCode's own tools, and a
// non-string value there means the shape changed, which is a reason to print
// nothing rather than to render a JSON fragment.
func toolLabel(input map[string]json.RawMessage) string {
	for _, key := range toolInputKeys {
		raw, ok := input[key]
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			continue
		}
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		if key == "filePath" {
			value = shortenPath(value)
		}
		return truncateOneLine(value, maxToolLabelLen)
	}
	return ""
}

// shortenPath renders an absolute path inside the current directory as a
// relative one, so a progress line reads "internal/tooling/task/scope.go"
// instead of the full worktree path — which, for a `dg wt` worktree, is long
// enough on its own to fill the whole line and push out the part that
// identifies the file. Anything outside the current directory (or an
// unresolvable working directory) is left exactly as OpenCode reported it: a
// path the reader cannot place is worse than a long one.
func shortenPath(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}
