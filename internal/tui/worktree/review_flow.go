// The R → picker → launch flow: kicking a reviewer agent (code/document/
// skill) against the cursor row's worktree. Unlike the n/N create flow or
// the s session flow, this is a single-step flow triggered directly from an
// existing worktree row — no repo-pick, no name-input — so it gets its own
// small sibling mode (reviewMode) next to createMode/sessionMode rather than
// a new createXxx value, mirroring how sessionMode already sits alongside
// createMode for the same reason (see Model.sessionMode's doc comment).

package tuiworktree

import (
	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

// reviewMode tracks progress through the R -> picker -> launch flow. It's
// its own type (not a createMode value) because every createMode value is a
// step of the n/N repo-pick/name-input/layout-pick flow specifically, and
// review has none of those steps.
type reviewMode int

const (
	reviewNone reviewMode = iota
	reviewPick
)

// reviewLaunchedMsg reports the result of a LaunchReviewInRepo call (success
// or failure, already formatted as the final status text) as its own typed
// message - not the generic statusMsg every other operation's plain status
// update uses - so Update can clear m.reviewLaunching specifically, the same
// reasoning createdMsg/createFailedMsg use for m.creating (see
// createFailedMsg's doc comment in create_flow.go).
type reviewLaunchedMsg string

// handleKickReview opens the reviewer picker for the R keybinding, offering
// the three built-in reviewers (code/document/skill) over the cursor row's
// worktree. It's a no-op while a launch is already in flight (the same
// m.reviewLaunching guard dispatchReviewLaunch arms) and on any row that
// isn't a worktree: selectedStatus() already reports ok=false for
// repo-header and session rows, so no separate row-kind check is needed
// here, matching how startNewWorktree relies on the same helper.
func (m Model) handleKickReview() (tea.Model, tea.Cmd) {
	if m.reviewLaunching {
		return m, nil
	}
	sel, ok := m.selectedStatus()
	if !ok {
		return m, nil
	}
	m.reviewRepo = sel.Repo
	m.reviewWorktreeName = sel.Name

	choices := worktree.BuiltinReviewerChoices()
	items := make([]tuicomponents.PaletteItem, 0, len(choices))
	for _, c := range choices {
		items = append(items, tuicomponents.PaletteItem{Command: c.Key, Hint: c.Label})
	}
	m.reviewPicker = tuicomponents.NewFuzzyPicker("Kick a review", items)
	m.reviewMode = reviewPick
	return m, nil
}

// handleReviewPickKey delegates to the FuzzyPicker. Unlike
// handleLayoutPickKey/handleRepoPickKey there is no free-typed-query
// fallback (FuzzyPickerNone + enter): the three reviewer keys are fixed and
// always all present in the list, so there's no free-typed reviewer name the
// way a free-typed layout name or repo path makes sense - FuzzyPickerNone
// needs no special handling beyond leaving the picker open.
func (m Model) handleReviewPickKey(key string) (tea.Model, tea.Cmd) {
	result := m.reviewPicker.HandleKey(key)
	switch result.Action {
	case tuicomponents.FuzzyPickerCancelled:
		m.reviewMode = reviewNone
		m.reviewPicker = nil

	case tuicomponents.FuzzyPickerSelected:
		return m.dispatchReviewLaunch(result.Item.Command)
	}
	return m, nil
}

// dispatchReviewLaunch captures the flow's accumulated state (repo, name,
// picked reviewer key) and kicks off the async launchReviewFn call. It arms
// m.reviewLaunching synchronously, before the returned tea.Cmd ever runs, so
// a second R can't start a concurrent launch while this one is still in
// flight - the same reasoning dispatchCreate's m.creating arming uses.
// reviewLaunching is cleared once reviewLaunchedMsg is processed (Update).
func (m Model) dispatchReviewLaunch(reviewerKey string) (tea.Model, tea.Cmd) {
	launchFn := m.launchReviewFn
	repo := m.reviewRepo
	name := m.reviewWorktreeName
	label := reviewerLabel(reviewerKey)

	m.reviewMode = reviewNone
	m.reviewPicker = nil
	m.reviewLaunching = true
	m.status = "review: " + name + " (" + label + ")…"

	return m, func() tea.Msg {
		if err := launchFn(repo, name, reviewerKey); err != nil {
			return reviewLaunchedMsg("review failed: " + err.Error())
		}
		return reviewLaunchedMsg("review started: " + name)
	}
}

// reviewerLabel looks up reviewerKey's human label via
// worktree.BuiltinReviewerChoices() rather than hardcoding the three label
// strings a second time here. Falls back to the bare key if it's somehow not
// found (defensive only - the key always comes from a picker item built from
// the same choices list).
func reviewerLabel(reviewerKey string) string {
	for _, c := range worktree.BuiltinReviewerChoices() {
		if c.Key == reviewerKey {
			return c.Label
		}
	}
	return reviewerKey
}

// renderReviewPickPopup builds the raw (uncentered) review-picker popup
// content; the caller composites it over the dashboard background via
// Overlay, same as renderRepoPickPopup/renderLayoutPickPopup.
func (m Model) renderReviewPickPopup() string {
	maxW := min(m.width-2, 64)
	return m.reviewPicker.View(maxW)
}
