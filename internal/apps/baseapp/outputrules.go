// The output-budget hook's rule table and derived limits — the single Go
// definition both configs/claude/output-budget.sh and
// configs/opencode/plugin/output-budget.js consume through the generated
// agent-runtime.json sidecar. Neither hook contains a pattern literal or a
// budget literal (docs/guides/output-budget-runner.md §8.1).
//
// Everything in this file is generation-time: EnsureAgentRuntime (Step 5)
// is the only caller of DerivedLimits, and it refuses to write a sidecar
// DerivedLimits returns an error for.

package baseapp

import (
	"fmt"
	"regexp"
)

// OutputBudgetRule is one built-in matching rule (guide §8.1). Match is a
// token prefix, never a regex (guide §8.2) — both hooks compare it against a
// command's leading whitespace-split tokens, element for element.
type OutputBudgetRule struct {
	Name  string   `json:"name"`
	Match []string `json:"match"`
	Head  int      `json:"head"`
	Tail  int      `json:"tail"`
}

// DefaultOutputBudgetRules is devgeta's built-in rule table: general-purpose
// runners across ecosystems, with no devgeta- or Go-specific bias (guide
// §8.4 — this ships to strangers, CLAUDE.md §12). Order is precedence: the
// first matching rule wins (guide §8.3), which is why npm-test precedes the
// more general npm-run. v1 ships this list only — no user-defined rule
// surface (cycle doc Step 4, "Scope decisions").
var DefaultOutputBudgetRules = []OutputBudgetRule{
	{Name: "go-test", Match: []string{"go", "test"}, Head: 30, Tail: 120},
	{Name: "cargo-test", Match: []string{"cargo", "test"}, Head: 30, Tail: 120},
	{Name: "pytest", Match: []string{"pytest"}, Head: 30, Tail: 120},
	{Name: "npm-test", Match: []string{"npm", "test"}, Head: 30, Tail: 120},
	{Name: "npm-run", Match: []string{"npm", "run"}, Head: 30, Tail: 100},
	{Name: "make", Match: []string{"make"}, Head: 20, Tail: 100},
	{Name: "cargo-build", Match: []string{"cargo", "build"}, Head: 20, Tail: 100},
	{Name: "gradle", Match: []string{"gradle"}, Head: 20, Tail: 100},
	{Name: "maven", Match: []string{"mvn"}, Head: 20, Tail: 100},
}

// Authored ceilings (cycle doc §8 "Decisions taken" #4, #8). Go-side only:
// never transported to the sidecar, only what DerivedLimits computes from
// them is (guide §8.1 — shipping the ceilings and reserves too would be more
// numbers a hand-edited sidecar could disagree with).
const (
	maxLineBytes         = 2048
	maxTotalBytesCeiling = 65536
	maxCaptureBytes      = 16 * 1024 * 1024 // 16 MiB (guide §4)
)

// inlineMarkerTemplate is the per-line truncation marker's fixed text (guide
// §6); %d is the omitted byte count, filled in at reduction time, never here.
const inlineMarkerTemplate = " [devgeta: truncated, %d bytes omitted]"

// inlineMarkerReserve reserves the template's fixed text plus the widest
// possible decimal byte count (20 digits covers an int64). A compile-time
// constant, not a per-write measurement — the guarantee is that the
// formatted marker is never longer than this, so the retained line is never
// longer than maxLineBytes (guide §6).
var inlineMarkerReserve = len(inlineMarkerTemplate) + 20

// captureNotice is appended to a capped capture file (guide §4.4). Unlike
// the inline marker it names no number — "if any" is deliberately load-
// bearing wording, see the guide — so its reserve is an exact length, not an
// estimate.
const captureNotice = "\n[devgeta: capture stopped at the limit; " +
	"anything the command produced past this point, if any, is gone.]\n"

var captureNoticeReserve = len(captureNotice) + 1

// widthPattern is the bash/JS shared numeric-transport contract (guide
// §5.3): a positive integer of at most 15 decimal digits — exact in
// IEEE-754 and +1-safe in 64-bit bash. Checked against the rendered decimal
// string, never the parsed number.
var widthPattern = regexp.MustCompile(`^[1-9][0-9]{0,14}$`)

// DerivedLimits computes lineContentLimit and captureContentLimit from the
// authored ceilings and reserves above, and validates the generation-time
// invariants guide §5.4 assigns to EnsureAgentRuntime: every derived limit
// positive, every derived limit within the width contract, and the capture
// reserve large enough to hold the notice it exists for. maxTotalBytes is
// returned unreserved (guide §2 — its marker embeds a scratch path only
// known at runtime, so it cannot be pre-reserved here).
func DerivedLimits() (lineContentLimit, maxTotalBytesOut, captureContentLimit int, err error) {
	lineContentLimit = maxLineBytes - inlineMarkerReserve
	captureContentLimit = maxCaptureBytes - captureNoticeReserve
	maxTotalBytesOut = maxTotalBytesCeiling

	if lineContentLimit <= 0 {
		return 0, 0, 0, fmt.Errorf(
			"output-budget: lineContentLimit %d is not positive (maxLineBytes %d, inlineMarkerReserve %d)",
			lineContentLimit,
			maxLineBytes,
			inlineMarkerReserve,
		)
	}
	if captureContentLimit <= 0 {
		return 0, 0, 0, fmt.Errorf(
			"output-budget: captureContentLimit %d is not positive (maxCaptureBytes %d, captureNoticeReserve %d)",
			captureContentLimit,
			maxCaptureBytes,
			captureNoticeReserve,
		)
	}
	if captureNoticeReserve < len(captureNotice)+1 {
		return 0, 0, 0, fmt.Errorf(
			"output-budget: captureNoticeReserve %d is smaller than the notice (%d bytes) + 1",
			captureNoticeReserve, len(captureNotice),
		)
	}
	for _, v := range []int{lineContentLimit, maxTotalBytesOut, captureContentLimit} {
		if !widthPattern.MatchString(fmt.Sprintf("%d", v)) {
			return 0, 0, 0, fmt.Errorf(
				"output-budget: derived limit %d fails the width contract ^[1-9][0-9]{0,14}$", v,
			)
		}
	}
	return lineContentLimit, maxTotalBytesOut, captureContentLimit, nil
}
