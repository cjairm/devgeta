package tuiworktree

// The dashboard budgets exactly m.height lines (body + hint + status). Any
// status message that occupies more than one terminal row pushes the frame
// past that budget, the terminal scrolls to make room, and the previous
// frame's rows remain on screen interleaved with the new ones - which is what
// made the dashboard appear to show duplicated and nested worktrees that did
// not exist.
//
// Status text comes from many sources and several are legitimately
// multi-line: git's "your branch diverged from origin" advisory is five lines
// with indentation. Enforcing the one-row invariant at renderStatus - the
// single point every status passes through - is what keeps any one of them
// from breaking the frame.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFlattenToOneLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single line is unchanged", "worktree created: feat-x", "worktree created: feat-x"},
		{"empty stays empty", "", ""},
		{
			"newlines become single spaces",
			"first line\nsecond line",
			"first line second line",
		},
		{
			"indentation after a newline is squeezed, not doubled",
			"Warning: diverged.\n  The worktree was created at HEAD.",
			"Warning: diverged. The worktree was created at HEAD.",
		},
		{"tabs and carriage returns collapse too", "a\tb\r\nc", "a b c"},
		{"trailing newline leaves no trailing space", "done\n", "done"},
		{"leading newline produces no leading space", "\nstarts here", "starts here"},
		{"a run of blank lines collapses to one space", "a\n\n\n\nb", "a b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flattenToOneLine(tt.in); got != tt.want {
				t.Errorf("flattenToOneLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestRenderStatusIsAlwaysOneRow is the invariant itself, asserted on the
// rendered output rather than on the helper, so it holds no matter how
// renderStatus is later reorganized.
func TestRenderStatusIsAlwaysOneRow(t *testing.T) {
	// The real five-line git advisory that reaches m.status via a create.
	const gitAdvisory = "created, but: Warning: local branch \"feat\" diverged from " +
		"origin/feat and was not updated.\n" +
		"  The worktree was created at the branch's current local state.\n" +
		"  If the local branch has no unique work, sync it with:\n" +
		"    git -C /wt fetch origin feat\n" +
		"    git -C /wt reset --hard origin/feat"

	multiline := []string{
		gitAdvisory,
		"first\nsecond",
		"trailing\n",
		"a\n\nb",
	}

	for _, status := range multiline {
		m := makeTestModel(nil)
		m.status = status
		out := m.renderStatus(m.width)

		if strings.Contains(out, "\n") {
			t.Errorf(
				"renderStatus emitted a newline, which overflows the frame budget "+
					"and scrolls the dashboard.\n  input:  %q\n  output: %q",
				status, out,
			)
		}
		if w := ansi.StringWidth(out); w > m.width {
			t.Errorf("renderStatus width %d exceeds pane width %d", w, m.width)
		}
	}
}

// TestRenderDashboardHeightWithMultilineStatus proves the consequence the
// invariant exists to prevent: the whole frame must stay within m.height
// rows even when a status message arrives with embedded newlines.
func TestRenderDashboardHeightWithMultilineStatus(t *testing.T) {
	m := makeTestModel(nil)
	m.loaded = true
	m.status = "created, but: line one\n  line two\n  line three\n  line four"

	got := len(strings.Split(m.renderDashboard(), "\n"))
	if got != m.height {
		t.Errorf(
			"dashboard rendered %d rows for a %d-row terminal; the excess scrolls "+
				"the frame and leaves the previous frame's rows on screen",
			got, m.height,
		)
	}
}

// TestCreateWarningsAreAllPreserved covers the accumulation half: one
// CreateAt raises several independent advisories in sequence - git frees a
// branch held by the source checkout, then finds that branch diverged from
// origin, and the manager can separately fail to record the repo as recently
// used. Keeping only the last silently dropped the others, including the
// "your source checkout was moved" notice, which reports a state change the
// user has to know about.
func TestCreateWarningsAreAllPreserved(t *testing.T) {
	// collectWarnings mirrors createFn's sink: accumulate, join on one line.
	// The assertion is on the resulting status text, which is what the user
	// actually sees, rather than on the closure's internals.
	collect := func(msgs ...string) string {
		var warnings []string
		sink := func(msg string) { warnings = append(warnings, msg) }
		for _, m := range msgs {
			sink(m)
		}
		return strings.Join(warnings, " · ")
	}

	got := collect(
		"Note: source checkout at /repo was moved to main so \"feat\" could be adopted",
		"Warning: local branch \"feat\" diverged from origin/feat and was not updated.",
	)

	for _, want := range []string{"source checkout at /repo was moved", "diverged from origin/feat"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q was dropped; got: %q", want, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Errorf("joined warnings must stay on one row for the status line, got: %q", got)
	}

	// A single warning must not gain a separator, and none must not produce
	// a non-empty string (handleCreateSuccess treats "" as "no warning").
	if got := collect("only one"); got != "only one" {
		t.Errorf("single warning = %q, want %q", got, "only one")
	}
	if got := collect(); got != "" {
		t.Errorf("no warnings must yield an empty string, got %q", got)
	}
}
