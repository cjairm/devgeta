package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/commands"
)

// toolEvent renders one tool_use NDJSON event in the shape real
// `opencode run --format json` captures carry (see
// ~/.cache/devgeta/scratch/codex-doc.json, described in reviewrun_test.go):
// part.tool names the tool, part.callID identifies the call, and
// part.state.input holds that tool's own arguments.
func toolEvent(tool, callID string, input map[string]any) string {
	part := map[string]any{
		"type":   "tool",
		"tool":   tool,
		"callID": callID,
		"state":  map[string]any{"status": "completed", "input": input},
	}
	encoded, err := json.Marshal(map[string]any{"type": "tool_use", "part": part})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// stepFinishEvent renders a step_finish event carrying what that step cost,
// the field the closing progress line's dollar figure adds up.
func stepFinishEvent(cost float64) string {
	encoded, err := json.Marshal(map[string]any{
		"type": "step_finish",
		"part": map[string]any{"type": "step-finish", "cost": cost},
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// clockAt returns a func() time.Time reporting each offset from a fixed base,
// in order, repeating the last one once exhausted — so a test can place every
// clock read at an exact moment instead of counting steps.
//
// The reads come in a known order: newReviewerProgress takes the first (the
// reviewer's start, so offsets[0] should be 0), a quiet round takes one per
// counted tool call, and finished() takes the last.
func clockAt(offsets ...time.Duration) func() time.Time {
	base := time.Unix(0, 0)
	i := 0
	return func() time.Time {
		off := offsets[len(offsets)-1]
		if i < len(offsets) {
			off = offsets[i]
		}
		i++
		return base.Add(off)
	}
}

// --- live tool lines through ReviewRun -------------------------------------

// The whole point of the progress lines: they must appear WHILE the reviewer
// runs, not in a batch once it is over. The mock executor replays stdout
// through OnStdoutLine before returning (mirroring the real one), so a tool
// line landing above the reviewer's own closing line is what proves the
// caller saw it mid-run rather than after.
func TestReviewRunVerboseReportsEachToolCallAsItHappens(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		toolEvent("bash", "call_1", map[string]any{
			"command": "devgeta task review-notes",
			"workdir": "/tmp/wt",
		}),
		toolEvent("read", "call_2", map[string]any{"filePath": "internal/x.go"}),
		toolEvent("glob", "call_3", map[string]any{"pattern": "docs/**/*.md"}),
		stepFinishEvent(0.25),
		textEvent("### Recommendation\n\n**Status:** APPROVE\n"),
		stepFinishEvent(0.17),
	}, "\n") + "\n"

	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: stream})

	var progress bytes.Buffer
	tm.ProgressOut = &progress
	tm.Verbose = true
	tm.NowFn = fixedClock(time.Unix(0, 0), 90*time.Second)

	out, err := tm.ReviewRun("", "")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	// stdout is untouched by any of this.
	if out != "OpenCode default model → APPROVE" {
		t.Errorf("stdout contract changed:\n%s", out)
	}

	want := strings.Join([]string{
		"[1/1] OpenCode default model: running",
		"[1/1]   bash devgeta task review-notes",
		"[1/1]   read internal/x.go",
		"[1/1]   glob docs/**/*.md",
		"[1/1] OpenCode default model: APPROVE (1m30s, 3 tools, $0.42)",
		"",
	}, "\n")
	if progress.String() != want {
		t.Errorf("progress got:\n%s\nwant:\n%s", progress.String(), want)
	}
}

// The default is quiet: a round samples its tool calls instead of printing
// one line each. A real round makes hundreds, and /review-loop pays tokens for
// every one of them. What must survive the sampling is the information — the
// counters keep counting every call, and each heartbeat names the tool call
// the reviewer was on when it fired.
func TestReviewRunQuietDefaultSamplesToolCalls(t *testing.T) {
	stream := strings.Join([]string{
		toolEvent("bash", "call_1", map[string]any{"command": "devgeta task review-scope"}),
		toolEvent("read", "call_2", map[string]any{"filePath": "internal/a.go"}),
		toolEvent("read", "call_3", map[string]any{"filePath": "internal/b.go"}),
		stepFinishEvent(0.25),
		toolEvent("glob", "call_4", map[string]any{"pattern": "docs/**/*.md"}),
		toolEvent("read", "call_5", map[string]any{"filePath": "internal/c.go"}),
		textEvent("**Status:** APPROVE\n"),
	}, "\n") + "\n"

	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: stream})

	var progress bytes.Buffer
	tm.ProgressOut = &progress
	// Start, then one read per tool call, then the close. Only the calls at
	// 35s and 70s are a full interval past the last printed line.
	tm.NowFn = clockAt(0, 5*time.Second, 35*time.Second, 40*time.Second,
		70*time.Second, 75*time.Second, 80*time.Second)

	out, err := tm.ReviewRun("", "")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if out != "OpenCode default model → APPROVE" {
		t.Errorf("stdout contract changed:\n%s", out)
	}

	want := strings.Join([]string{
		"[1/1] OpenCode default model: running",
		"[1/1]   ... 35s, 2 tools - read internal/a.go",
		"[1/1]   ... 1m10s, 4 tools, $0.25 - glob docs/**/*.md",
		"[1/1] OpenCode default model: APPROVE (1m20s, 5 tools, $0.25)",
		"",
	}, "\n")
	if progress.String() != want {
		t.Errorf("progress got:\n%s\nwant:\n%s", progress.String(), want)
	}
}

// The reviewer's own prose must never reach the progress output. Its findings
// belong in the journal; dumping the report to the terminal is what this
// command deliberately stopped doing.
func TestReviewRunProgressNeverPrintsReviewerProse(t *testing.T) {
	const prose = "I found a race in the retry path and three nits in the docs"
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: textEvent(prose+"\n\n**Status:** REQUEST CHANGES\n") + "\n",
	})

	var progress bytes.Buffer
	tm.ProgressOut = &progress
	tm.NowFn = fixedClock(time.Unix(0, 0), time.Second)

	if _, err := tm.ReviewRun("", ""); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if strings.Contains(progress.String(), prose) {
		t.Errorf("the reviewer's prose must stay out of progress, got:\n%s", progress.String())
	}
	// The verdict still reaches the closing line — that is not prose.
	if !strings.Contains(progress.String(), "REQUEST CHANGES") {
		t.Errorf("expected the outcome on the closing line, got:\n%s", progress.String())
	}
}

// A run whose stream reports no cost (a local model, or a failure before the
// first step finished) must not print "$0.00": that reads as a claim the round
// was free, rather than as an absent number.
func TestReviewRunProgressOmitsCostWhenNoneReported(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: toolEvent("read", "call_1", map[string]any{"filePath": "x.go"}) + "\n" +
			statusReport("APPROVE"),
	})

	var progress bytes.Buffer
	tm.ProgressOut = &progress
	tm.NowFn = clockAt(0, time.Second, 2*time.Second)

	if _, err := tm.ReviewRun("", ""); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if !strings.Contains(progress.String(), "APPROVE (2s, 1 tools)") {
		t.Errorf("expected elapsed and tool count with no cost, got:\n%s", progress.String())
	}
	if strings.Contains(progress.String(), "$") {
		t.Errorf("expected no dollar figure when none was reported, got:\n%s", progress.String())
	}
}

// The progress hook must be handed to the OpenCode wrapper, not left nil:
// without it the executor has nothing to call and every line above would
// silently disappear.
func TestReviewRunPassesTheProgressHookToOpenCode(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: statusReport("APPROVE"),
		onCall: func(t *testing.T, c commands.CommandParams) {
			if c.OnStdoutLine == nil {
				t.Error("expected review-run to ask for line-by-line stdout")
			}
		},
	})

	if _, err := tm.ReviewRun("", ""); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
}

// --- reviewerProgress in isolation ----------------------------------------

// newVerboseProgress builds a verbose reviewerProgress on a clock that never
// moves — the sampling rule is off, so these tests assert what a tool call
// prints without a heartbeat interval in the picture.
func newVerboseProgress(out io.Writer) *reviewerProgress {
	return newReviewerProgress(out, "[1/1]", "model", true, func() time.Time {
		return time.Unix(0, 0)
	})
}

// One tool call is one line, however many events it emits. OpenCode can
// report the same callID more than once (pending, running, completed); a line
// per event would triple the output and inflate the tool count.
func TestReviewerProgressReportsEachCallOnce(t *testing.T) {
	var buf bytes.Buffer
	p := newVerboseProgress(&buf)

	p.line(toolEvent("read", "call_1", map[string]any{"filePath": "x.go"}))
	p.line(toolEvent("read", "call_1", map[string]any{"filePath": "x.go"}))
	p.line(toolEvent("read", "call_2", map[string]any{"filePath": "y.go"}))

	want := "[1/1]   read x.go\n[1/1]   read y.go\n"
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
	if p.tools != 2 {
		t.Errorf("expected 2 tool calls counted, got %d", p.tools)
	}
}

// An event with no callID counts as its own call rather than as a repeat of
// the last one — collapsing every such event into one line would report a
// long run as a single tool call.
func TestReviewerProgressCountsEventsWithoutACallID(t *testing.T) {
	var buf bytes.Buffer
	p := newVerboseProgress(&buf)

	p.line(toolEvent("bash", "", map[string]any{"command": "go build ./..."}))
	p.line(toolEvent("bash", "", map[string]any{"command": "go test ./..."}))

	if p.tools != 2 {
		t.Errorf("expected 2 tool calls counted, got %d", p.tools)
	}
	if !strings.Contains(buf.String(), "go build") || !strings.Contains(buf.String(), "go test") {
		t.Errorf("expected both commands reported, got:\n%s", buf.String())
	}
}

// Progress reporting must never turn a malformed stream into noise or an
// error: it is commentary on a run whose verdict is read from the captured
// stdout afterwards.
func TestReviewerProgressIgnoresWhatItCannotRead(t *testing.T) {
	var buf bytes.Buffer
	p := newVerboseProgress(&buf)

	for _, line := range []string{
		"",
		"   ",
		"zoxide: detected a possible configuration issue.",
		`{"type":"tool_use","part":"not-an-object"}`,
		`{"type":"tool_use","part":{"type":"tool","callID":"c"}}`, // no tool name
		`{"type":"step_finish","part":"not-an-object"}`,
		`{"broken json`,
	} {
		p.line(line)
	}

	if buf.Len() != 0 {
		t.Errorf("expected nothing reported, got:\n%s", buf.String())
	}
	if p.tools != 0 || p.cost != 0 {
		t.Errorf("expected no tools or cost counted, got tools=%d cost=%v", p.tools, p.cost)
	}
}

func TestReviewerProgressSummary(t *testing.T) {
	tests := []struct {
		name    string
		tools   int
		cost    float64
		elapsed time.Duration
		want    string
	}{
		{"elapsed alone", 0, 0, 250 * time.Millisecond, "250ms"},
		{"with tools", 4, 0, 90 * time.Second, "1m30s, 4 tools"},
		{"with tools and cost", 4, 1.5, 90 * time.Second, "1m30s, 4 tools, $1.50"},
		{"cost without tools", 0, 0.07, time.Second, "1s, $0.07"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newVerboseProgress(&bytes.Buffer{})
			p.tools, p.cost = tt.tools, tt.cost
			if got := p.summary(tt.elapsed); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- the quiet heartbeat ---------------------------------------------------

// The gate is the time since the LAST PRINTED line, not since the last tool
// call: a reviewer calling a tool every few seconds for ten minutes must still
// print one line per interval, and nothing in between.
func TestReviewerProgressHeartbeatRespectsTheInterval(t *testing.T) {
	var buf bytes.Buffer
	// Construction, then one read per tool call below.
	now := clockAt(
		0,
		progressHeartbeatInterval-time.Second, // too early
		progressHeartbeatInterval,             // fires
		progressHeartbeatInterval+time.Second, // too early — the gate now runs
		2*progressHeartbeatInterval-time.Second, // from the line above, so still too early
		2*progressHeartbeatInterval+time.Second, // fires
	)
	p := newReviewerProgress(&buf, "[1/1]", "model", false, now)

	for i, path := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		p.line(toolEvent("read", fmt.Sprintf("call_%d", i), map[string]any{"filePath": path}))
	}

	want := "[1/1]   ... 30s, 2 tools - read b.go\n" +
		"[1/1]   ... 1m1s, 5 tools - read e.go\n"
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
	// Every call is still counted, printed or not — the closing line's tool
	// count must not turn into "tool calls we happened to sample".
	if p.tools != 5 {
		t.Errorf("expected all 5 tool calls counted, got %d", p.tools)
	}
}

// A reviewer that finishes inside one interval prints no heartbeat at all —
// its start and closing lines already say everything there is to say.
func TestReviewerProgressHeartbeatStaysQuietWithinOneInterval(t *testing.T) {
	var buf bytes.Buffer
	p := newReviewerProgress(&buf, "[1/1]", "model", false,
		clockAt(0, time.Second, 2*time.Second, 3*time.Second))

	p.line(toolEvent("read", "call_1", map[string]any{"filePath": "a.go"}))
	p.line(toolEvent("read", "call_2", map[string]any{"filePath": "b.go"}))
	p.line(toolEvent("read", "call_3", map[string]any{"filePath": "c.go"}))

	if buf.Len() != 0 {
		t.Errorf("expected no heartbeat inside one interval, got:\n%s", buf.String())
	}
	if p.tools != 3 {
		t.Errorf("expected 3 tool calls counted, got %d", p.tools)
	}
}

// The interval runs from when this reviewer began, not from the zero time: a
// round starting at any wall-clock moment must not fire a heartbeat on its
// very first tool call, which is what an unstamped clock would do.
func TestReviewerProgressHeartbeatIsMeasuredFromTheStart(t *testing.T) {
	var buf bytes.Buffer
	p := newReviewerProgress(&buf, "[1/1]", "model", false,
		clockAt(20*time.Second, 40*time.Second))
	p.started()

	p.line(toolEvent("read", "call_1", map[string]any{"filePath": "a.go"}))

	// 40s on the clock, but only 20s into this reviewer.
	want := "[1/1] model: running\n"
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

// --- tool labels ----------------------------------------------------------

func TestToolLabel(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"bash reports its command", map[string]any{
			"command": "devgeta task review-scope",
			"workdir": "/tmp/wt",
		}, "devgeta task review-scope"},
		{"read reports its path", map[string]any{"filePath": "internal/x.go"}, "internal/x.go"},
		{"glob reports its pattern", map[string]any{
			"pattern": "**/*.md",
			"path":    "/tmp/wt",
		}, "**/*.md"},
		{
			"skill reports its name",
			map[string]any{"name": "using-superpowers"},
			"using-superpowers",
		},
		// todowrite's input is a whole todo list: nothing to summarize, so the
		// tool is reported by name alone rather than with a JSON fragment.
		{"unrecognized input yields no label", map[string]any{
			"todos": []any{map[string]any{"content": "do the thing"}},
		}, ""},
		{"empty input yields no label", map[string]any{}, ""},
		{"blank value yields no label", map[string]any{"command": "   "}, ""},
		// A key whose value is not a string means the shape changed; printing
		// nothing beats printing a fragment of JSON.
		{"non-string value yields no label", map[string]any{"filePath": 42}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := map[string]json.RawMessage{}
			for k, v := range tt.input {
				encoded, err := json.Marshal(v)
				if err != nil {
					t.Fatalf("marshaling %s: %v", k, err)
				}
				raw[k] = encoded
			}
			if got := toolLabel(raw); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// A long command is folded onto one line and cut, so one tool call can never
// spill across several progress lines.
func TestToolLabelStaysOneShortLine(t *testing.T) {
	long := "go test ./... " + strings.Repeat("-run TestSomethingWithAVeryLongName ", 10)
	raw := map[string]json.RawMessage{"command": json.RawMessage(`"` + long + `\n more"`)}

	got := toolLabel(raw)
	if strings.Contains(got, "\n") {
		t.Errorf("expected one line, got %q", got)
	}
	if len([]rune(got)) > maxToolLabelLen+1 { // +1 for the ellipsis
		t.Errorf("expected at most %d runes plus an ellipsis, got %q", maxToolLabelLen, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected a truncation marker, got %q", got)
	}
}

// An absolute path inside the current directory is shown relative to it: the
// full path of a `dg wt` worktree is long enough on its own to crowd out the
// part of the line that identifies the file.
func TestShortenPath(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	inside := filepath.Join(cwd, "internal", "tooling", "task", "scope.go")
	if got := shortenPath(inside); got != filepath.Join("internal", "tooling", "task", "scope.go") {
		t.Errorf("expected a path relative to cwd, got %q", got)
	}
	// Outside the current directory, the path is left exactly as reported: a
	// path the reader cannot place is worse than a long one.
	outside := filepath.Join(filepath.Dir(cwd), "elsewhere", "x.go")
	if got := shortenPath(outside); got != outside {
		t.Errorf("expected an outside path untouched, got %q", got)
	}
	if got := shortenPath("already/relative.go"); got != "already/relative.go" {
		t.Errorf("expected a relative path untouched, got %q", got)
	}
}
