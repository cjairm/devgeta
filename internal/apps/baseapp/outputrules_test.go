package baseapp

import (
	"fmt"
	"testing"
)

func TestDefaultOutputBudgetRulesHaveNoDuplicateNames(t *testing.T) {
	seen := make(map[string]bool, len(DefaultOutputBudgetRules))
	for _, r := range DefaultOutputBudgetRules {
		if seen[r.Name] {
			t.Errorf("duplicate rule name %q", r.Name)
		}
		seen[r.Name] = true
	}
}

func TestDefaultOutputBudgetRulesTailExceedsHead(t *testing.T) {
	// "Tail is kept larger than head throughout: failures land last"
	// (docs/guides/output-budget-runner.md §3, §8.1).
	for _, r := range DefaultOutputBudgetRules {
		if r.Tail <= r.Head {
			t.Errorf("rule %q: tail (%d) must exceed head (%d)", r.Name, r.Tail, r.Head)
		}
	}
}

func TestDefaultOutputBudgetRulesEveryMatchIsOneOrTwoTokens(t *testing.T) {
	// §8.2: match is the executable name and (optionally) its subcommand —
	// never more, since only the compared prefix is examined.
	for _, r := range DefaultOutputBudgetRules {
		if len(r.Match) < 1 || len(r.Match) > 2 {
			t.Errorf("rule %q: match has %d tokens, want 1 or 2", r.Name, len(r.Match))
		}
	}
}

func TestNpmTestPrecedesNpmRun(t *testing.T) {
	// §8.3: precedence is array order, first match wins. npm test must not
	// be shadowed by the more general npm-run rule.
	testIdx, runIdx := -1, -1
	for i, r := range DefaultOutputBudgetRules {
		switch r.Name {
		case "npm-test":
			testIdx = i
		case "npm-run":
			runIdx = i
		}
	}
	if testIdx == -1 || runIdx == -1 {
		t.Fatalf(
			"expected both npm-test and npm-run in the rule table, got test=%d run=%d",
			testIdx,
			runIdx,
		)
	}
	if testIdx >= runIdx {
		t.Errorf("npm-test (index %d) must precede npm-run (index %d)", testIdx, runIdx)
	}
}

func TestDerivedLimitsAreAllPositiveAndWithinTheWidthContract(t *testing.T) {
	lineContentLimit, maxTotalBytesOut, captureContentLimit, err := DerivedLimits()
	if err != nil {
		t.Fatalf("DerivedLimits: %v", err)
	}
	for name, v := range map[string]int{
		"lineContentLimit":    lineContentLimit,
		"maxTotalBytes":       maxTotalBytesOut,
		"captureContentLimit": captureContentLimit,
	} {
		if v <= 0 {
			t.Errorf("%s = %d, want positive", name, v)
		}
		if !widthPattern.MatchString(fmt.Sprintf("%d", v)) {
			t.Errorf("%s = %d fails the width contract ^[1-9][0-9]{0,14}$", name, v)
		}
	}
}

func TestDerivedLimitsAreStrictlyUnderTheirCeilings(t *testing.T) {
	// The reserve must actually reserve something — a limit equal to its
	// ceiling would mean the marker has no room at all.
	lineContentLimit, _, captureContentLimit, err := DerivedLimits()
	if err != nil {
		t.Fatalf("DerivedLimits: %v", err)
	}
	if lineContentLimit >= maxLineBytes {
		t.Errorf(
			"lineContentLimit %d must be less than maxLineBytes %d",
			lineContentLimit,
			maxLineBytes,
		)
	}
	if captureContentLimit >= maxCaptureBytes {
		t.Errorf(
			"captureContentLimit %d must be less than maxCaptureBytes %d",
			captureContentLimit,
			maxCaptureBytes,
		)
	}
}

func TestDerivedLimitsMaxTotalBytesIsTheRawCeiling(t *testing.T) {
	// §2: maxTotalBytes is transported raw — its marker is only known at
	// runtime (it embeds the scratch path), so it is never pre-reserved.
	_, maxTotalBytesOut, _, err := DerivedLimits()
	if err != nil {
		t.Fatalf("DerivedLimits: %v", err)
	}
	if maxTotalBytesOut != maxTotalBytesCeiling {
		t.Errorf(
			"maxTotalBytes = %d, want the raw ceiling %d",
			maxTotalBytesOut,
			maxTotalBytesCeiling,
		)
	}
}

func TestCaptureNoticeReserveCoversTheNotice(t *testing.T) {
	if captureNoticeReserve < len(captureNotice)+1 {
		t.Errorf(
			"captureNoticeReserve %d is smaller than the notice (%d bytes) + 1",
			captureNoticeReserve, len(captureNotice),
		)
	}
}

func TestInlineMarkerReserveCoversTheWidestByteCount(t *testing.T) {
	// The template's own fixed text plus 20 digits (an int64 ceiling) — see
	// the doc comment on inlineMarkerReserve.
	want := len(inlineMarkerTemplate) + 20
	if inlineMarkerReserve != want {
		t.Errorf("inlineMarkerReserve = %d, want %d", inlineMarkerReserve, want)
	}
}
