package task

import (
	"slices"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/tooling/handoff"
)

// withHeadResolvesTo overrides HEAD resolution (`rev-parse --verify
// HEAD^{commit}`) to answer sha, delegating every other git call to whatever
// newRepoSetup already wired up.
func withHeadResolvesTo(t *testing.T, tm *TaskManager, sha string) {
	t.Helper()
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "--verify") {
			return sha + "\n", "", nil
		}
		return orig(c)
	}
}

func TestHandoffWriteThenReadRoundTrips(t *testing.T) {
	tm, _, _ := newRepoSetup(t, "feat")
	withHeadResolvesTo(t, tm, "abc123abc123abc123abc123abc123abc123abc")

	if _, err := tm.HandoffWrite("", "Left off wiring the OAuth callback."); err != nil {
		t.Fatalf("HandoffWrite: %v", err)
	}
	out, err := tm.HandoffRead("")
	if err != nil {
		t.Fatalf("HandoffRead: %v", err)
	}
	if !strings.Contains(out, "Left off wiring the OAuth callback.") {
		t.Errorf("HandoffRead output missing the note body, got:\n%s", out)
	}
	if !strings.Contains(out, "feat") {
		t.Errorf("HandoffRead output missing the branch, got:\n%s", out)
	}
}

func TestHandoffReadWithNoNoteIsACleanSentinelNotAnError(t *testing.T) {
	tm, _, _ := newRepoSetup(t, "feat")
	out, err := tm.HandoffRead("")
	if err != nil {
		t.Fatalf("HandoffRead: %v", err)
	}
	if !strings.Contains(out, "feat") ||
		!strings.Contains(strings.ToLower(out), "no handoff note") {
		t.Errorf("expected a clean no-note message naming the branch, got: %q", out)
	}
}

func TestHandoffClearRemovesTheNote(t *testing.T) {
	tm, _, _ := newRepoSetup(t, "feat")
	withHeadResolvesTo(t, tm, "abc123abc123abc123abc123abc123abc123abc")

	if _, err := tm.HandoffWrite("", "some note"); err != nil {
		t.Fatalf("HandoffWrite: %v", err)
	}
	if _, err := tm.HandoffClear(""); err != nil {
		t.Fatalf("HandoffClear: %v", err)
	}
	out, err := tm.HandoffRead("")
	if err != nil {
		t.Fatalf("HandoffRead: %v", err)
	}
	if strings.Contains(out, "some note") {
		t.Errorf("note still present after Clear: %q", out)
	}
}

func TestHandoffClearOnAnAbsentNoteIsNotAnError(t *testing.T) {
	tm, _, _ := newRepoSetup(t, "feat")
	if _, err := tm.HandoffClear(""); err != nil {
		t.Errorf("HandoffClear on an absent note: %v", err)
	}
}

func TestHandoffWriteOverMaxBytesLeavesThePreviousNoteUntouched(t *testing.T) {
	tm, _, _ := newRepoSetup(t, "feat")
	withHeadResolvesTo(t, tm, "abc123abc123abc123abc123abc123abc123abc")

	if _, err := tm.HandoffWrite("", "the original note"); err != nil {
		t.Fatalf("HandoffWrite: %v", err)
	}

	huge := strings.Repeat("x", handoff.MaxBytes+1)
	if _, err := tm.HandoffWrite("", huge); err == nil {
		t.Fatal("expected an error writing an over-cap note")
	}

	out, err := tm.HandoffRead("")
	if err != nil {
		t.Fatalf("HandoffRead: %v", err)
	}
	if !strings.Contains(out, "the original note") {
		t.Errorf("the previous note was lost after a refused over-cap write, got:\n%s", out)
	}
}

func TestHandoffOnDetachedHeadReturnsTheNoBranchSentinel(t *testing.T) {
	tm, _, _ := newRepoSetup(t, "")

	writeOut, err := tm.HandoffWrite("", "some note")
	if err != nil {
		t.Fatalf("HandoffWrite: %v", err)
	}
	if writeOut != noBranchSentinel {
		t.Errorf("HandoffWrite on a detached HEAD = %q, want the no-branch sentinel", writeOut)
	}

	readOut, err := tm.HandoffRead("")
	if err != nil {
		t.Fatalf("HandoffRead: %v", err)
	}
	if readOut != noBranchSentinel {
		t.Errorf("HandoffRead on a detached HEAD = %q, want the no-branch sentinel", readOut)
	}
}

func TestHandoffReadReportsHeadDrift(t *testing.T) {
	tm, _, _ := newRepoSetup(t, "feat")
	withHeadResolvesTo(t, tm, "1111111111111111111111111111111111111111")

	if _, err := tm.HandoffWrite("", "some note"); err != nil {
		t.Fatalf("HandoffWrite: %v", err)
	}

	withHeadResolvesTo(t, tm, "2222222222222222222222222222222222222222")
	out, err := tm.HandoffRead("")
	if err != nil {
		t.Fatalf("HandoffRead: %v", err)
	}
	if !strings.Contains(out, "1111111111111111111111111111111111111111") ||
		!strings.Contains(out, "2222222222222222222222222222222222222222") {
		t.Errorf("expected the read to report drift between the two heads, got:\n%s", out)
	}
}

func TestHandoffExplicitBranchOverridesCurrent(t *testing.T) {
	tm, _, _ := newRepoSetup(t, "feat")
	withHeadResolvesTo(t, tm, "abc123abc123abc123abc123abc123abc123abc")

	if _, err := tm.HandoffWrite("other-branch", "note for another branch"); err != nil {
		t.Fatalf("HandoffWrite: %v", err)
	}

	// The current branch ("feat") must see no note of its own.
	out, err := tm.HandoffRead("")
	if err != nil {
		t.Fatalf("HandoffRead: %v", err)
	}
	if strings.Contains(out, "note for another branch") {
		t.Errorf("note written to another branch leaked into the current one: %q", out)
	}

	out, err = tm.HandoffRead("other-branch")
	if err != nil {
		t.Fatalf("HandoffRead: %v", err)
	}
	if !strings.Contains(out, "note for another branch") {
		t.Errorf("expected the note under its own branch, got: %q", out)
	}
}
