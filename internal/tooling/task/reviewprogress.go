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

// reviewerProgress renders one reviewer's run as it happens: a start line,
// one line per tool call, and a closing line carrying the outcome, the
// elapsed time, and what the run cost.
//
// It is written to from two places — the round loop (started/finished) and the
// executor's stdout-draining goroutine (line, via
// CommandParams.OnStdoutLine) — but never from both at once: the round loop
// is blocked inside OpenCode.Run for the entire time lines can arrive, and
// ExecCommand joins that goroutine before returning. So no lock is needed
// here, and adding one would suggest a concurrency this type does not have.
type reviewerProgress struct {
	out    io.Writer
	prefix string // "[1/2]" — which reviewer of how many
	label  string // the model name, or defaultModelLabel

	// reported is the callIDs already printed, so a tool that emits more than
	// one event (pending, then running, then completed) still costs one line.
	reported map[string]bool
	tools    int
	// cost accumulates every step's own cost, as OpenCode reports it. Zero
	// means the stream never reported any — a local model, or a run that died
	// before its first step finished — and is omitted rather than printed as
	// "$0.00", which would read as a claim that the round was free.
	cost float64
}

func newReviewerProgress(out io.Writer, prefix, label string) *reviewerProgress {
	return &reviewerProgress{
		out:      out,
		prefix:   prefix,
		label:    label,
		reported: map[string]bool{},
	}
}

// started announces the reviewer before its first event arrives — the run can
// spend a while resolving a provider before it does anything worth reporting.
func (p *reviewerProgress) started() {
	fmt.Fprintf(p.out, "%s %s: running\n", p.prefix, p.label)
}

// finished closes the reviewer out with the outcome the round will report.
func (p *reviewerProgress) finished(outcome string, elapsed time.Duration) {
	fmt.Fprintf(p.out, "%s %s: %s (%s)\n", p.prefix, p.label, outcome, p.summary(elapsed))
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

// reportTool prints one line for a tool call the reviewer made, the first
// time that call is seen.
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

	line := p.prefix + "   " + part.Tool
	if label := toolLabel(part.State.Input); label != "" {
		line += " " + label
	}
	fmt.Fprintln(p.out, line)
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
