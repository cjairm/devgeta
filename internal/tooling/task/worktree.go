package task

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/logger"
)

// taskWorktreePath returns worktree.GetWorktreeBasePath()/<repoSlug>/<flat-name>
// — the exact same location `dg wt` uses (internal/tooling/worktree's
// unexported worktreePath) — so a worktree created by worktree-start is
// immediately visible to `dg wt list`, and one created/managed by `dg wt` is
// visible here. See the design decision recorded in
// docs/plans/cycles/2026-07-22-agent-task-expansion.md (Slice C).
func taskWorktreePath(repoSlug, name string) string {
	return filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, worktree.FlattenName(name))
}

// WorktreeStart creates a new git worktree with a new branch, in the same
// base path `dg wt` uses. It refuses to run from a dirty tree so nothing is
// left half set up. When base is empty, the new branch is based on the
// repo's freshly-fetched default branch (reusing Git.CreateWorktreeIn's
// local/remote-branch-reuse logic verbatim); when base is given explicitly,
// the branch is created fresh from exactly that ref.
func (tm *TaskManager) WorktreeStart(name, base string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("worktree-start: name is required")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("worktree-start: %w", err)
	}

	dirty, err := tm.Git.IsWorktreeDirty(cwd)
	if err != nil {
		return "", fmt.Errorf("worktree-start: %w", err)
	}
	if dirty {
		return "", fmt.Errorf(
			"worktree-start: refusing to start from a dirty tree; commit or stash your changes first",
		)
	}

	repoRoot, err := tm.Git.GetRepoRootIn(cwd)
	if err != nil {
		return "", fmt.Errorf("worktree-start: not in a git repository: %w", err)
	}

	if err := tm.Git.FetchOrigin(); err != nil {
		return "", fmt.Errorf("worktree-start: %w", err)
	}

	repoSlug := filepath.Base(repoRoot)
	wtPath := taskWorktreePath(repoSlug, name)

	if _, statErr := os.Stat(wtPath); statErr == nil {
		return "", fmt.Errorf("worktree-start: %s already exists", wtPath)
	}

	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return "", fmt.Errorf("worktree-start: failed to create worktree directory: %w", err)
	}

	ref := base
	if base == "" {
		// Label the implicit base for the confirmation line. CreateWorktreeIn
		// may actually reuse an existing local/remote branch instead (see its
		// doc comment); the label still communicates the common case clearly.
		ref = "origin/" + tm.Git.DefaultBranchIn(repoRoot)
		if err := tm.Git.CreateWorktreeIn(repoRoot, wtPath, name); err != nil {
			return "", fmt.Errorf("worktree-start: %w", err)
		}
	} else {
		if err := tm.Git.CreateWorktreeAtBaseIn(repoRoot, wtPath, name, base); err != nil {
			return "", fmt.Errorf("worktree-start: %w", err)
		}
	}

	return fmt.Sprintf("Created worktree %s (branch %s, base %s)", wtPath, name, ref), nil
}

// WorktreeFinish tears down a worktree via exactly one of merge or discard.
// Target resolution is deterministic: an explicit name wins; otherwise cwd
// resolves to the linked worktree it's inside; otherwise the command errors
// and lists the worktrees it found rather than guessing from the main
// checkout.
//
// merge: refuses if either the worktree or the main checkout has uncommitted
// changes (no --force escape — see worktreeFinishMerge), rebases the
// worktree's branch onto the default branch if diverged, fast-forward-merges
// it into the default branch from the main checkout, then removes the
// worktree and deletes the branch (safe only because the fast-forward already
// made it fully merged).
//
// discard: refuses on a dirty worktree unless force is set, then removes the
// worktree and deletes the branch unconditionally.
//
// check: computes worktreeFinishCheck's read-only readiness report and
// returns it — never rebases, never moves a ref, never fetches. A report
// whose own "ready:" line says no is not a Go error in the usual sense (the
// report was computed successfully); it comes back as a *CheckNotReadyError so
// the cmd layer can print the full report before surfacing the non-zero exit.
func (tm *TaskManager) WorktreeFinish(
	name string,
	merge, discard, check, force bool,
) (string, error) {
	count := 0
	for _, b := range []bool{merge, discard, check} {
		if b {
			count++
		}
	}
	if count != 1 {
		return "", fmt.Errorf(
			"worktree-finish: exactly one of --merge, --discard, or --check is required",
		)
	}

	wtPath, branch, err := tm.resolveWorktreeTarget(name)
	if err != nil {
		return "", fmt.Errorf("worktree-finish: %w", err)
	}

	if check {
		report, ready, checkErr := tm.worktreeFinishCheck(wtPath, branch)
		if checkErr != nil {
			return "", fmt.Errorf("worktree-finish: %w", checkErr)
		}
		if !ready {
			return "", &CheckNotReadyError{Report: report}
		}
		return report, nil
	}

	var out string
	if discard {
		out, err = tm.worktreeFinishDiscard(wtPath, branch, force)
	} else {
		out, err = tm.worktreeFinishMerge(wtPath, branch)
	}
	if err != nil {
		return "", fmt.Errorf("worktree-finish: %w", err)
	}
	return out, nil
}

// CheckNotReadyError signals that worktree-finish --check computed a full
// report successfully, but the report's own ready: line says no. It carries
// the report text so the cmd layer can print it BEFORE surfacing the
// non-zero exit — emitPRResult's usual "err != nil means nothing to print"
// contract does not apply to this one, deliberate exception.
type CheckNotReadyError struct{ Report string }

func (e *CheckNotReadyError) Error() string { return "worktree is not ready to merge" }

// worktreeFinishDiscard removes the worktree and its branch unconditionally,
// refusing first when the worktree has uncommitted changes and force wasn't
// passed.
func (tm *TaskManager) worktreeFinishDiscard(wtPath, branch string, force bool) (string, error) {
	if !force {
		dirty, err := tm.Git.IsWorktreeDirty(wtPath)
		if err != nil {
			return "", err
		}
		if dirty {
			return "", fmt.Errorf(
				"%s has uncommitted changes; use --force to discard anyway", wtPath,
			)
		}
	}

	// Resolved BEFORE the removal: afterwards wtPath is gone, and the journal's
	// location can only be resolved by running git somewhere that still exists.
	mainWorktree, mainErr := tm.Git.GetMainWorktree(wtPath)

	if err := tm.Git.RemoveWorktree(wtPath, true, branch); err != nil {
		if !force {
			return "", fmt.Errorf("failed to discard %s: %w", wtPath, err)
		}
		return tm.forceDiscardFallback(wtPath, branch, err)
	}

	if mainErr == nil {
		tm.dropReviewJournal(mainWorktree, branch)
	}
	return fmt.Sprintf("Discarded worktree %s (branch %s deleted)", wtPath, branch), nil
}

// forceDiscardFallback handles the one case RemoveWorktree can't: `git
// worktree remove` refuses when the worktree has modified or untracked
// files, and RemoveWorktree deliberately never passes --force through (that
// refusal is the safety net for every other caller, e.g. worktree-finish
// --merge). --force here means the caller explicitly wants this thrown away
// regardless, so remove the directory directly, prune the now-stale git
// metadata, and force-delete the branch from the main checkout —
// RemoveWorktree never reached its own branch-delete step, since `worktree
// remove` failed before that.
func (tm *TaskManager) forceDiscardFallback(
	wtPath, branch string,
	removeErr error,
) (string, error) {
	mainWorktree, mainErr := tm.Git.GetMainWorktree(wtPath)
	if mainErr != nil {
		return "", fmt.Errorf("failed to discard %s: %w", wtPath, removeErr)
	}

	if err := os.RemoveAll(wtPath); err != nil {
		return "", fmt.Errorf("failed to discard %s: %w", wtPath, err)
	}
	if err := tm.Git.PruneWorktreesAt(mainWorktree); err != nil {
		return "", fmt.Errorf(
			"removed %s but failed to prune stale worktree metadata: %w", wtPath, err,
		)
	}
	if branch != "" {
		if err := tm.Git.ExecuteCommandAt(mainWorktree, "branch", "-D", branch); err != nil {
			return "", fmt.Errorf(
				"removed %s but failed to delete branch %q: %w", wtPath, branch, err,
			)
		}
		tm.dropReviewJournal(mainWorktree, branch)
	}

	return fmt.Sprintf("Discarded worktree %s (branch %s deleted)", wtPath, branch), nil
}

// The five refusal-reason messages shared between worktreeFinishMerge (which
// returns each of these as this function's own blocking error) and
// worktreeFinishCheck (which surfaces the identical text as the first
// blocking reason in a "ready: no —" line). Defined once, each as a function
// building a real error, rather than as a literal duplicated at both call
// sites: worktreeFinishCheck reads back .Error() to get the EXACT text
// worktreeFinishMerge would have returned, so the two paths are structurally
// unable to diverge on wording when only one side is edited later (CLAUDE.md
// §6) — nothing has to remember to keep them in sync.
func refusalDirtyWorktree() error {
	return fmt.Errorf("refusing to merge a dirty worktree; commit or stash your changes first")
}

func refusalMainCheckoutWrongBranch(mainWorktree, mainBranch, defaultBranch string) error {
	return fmt.Errorf(
		"main checkout %s is on %q, not %q; check out %q there first",
		mainWorktree, mainBranch, defaultBranch, defaultBranch,
	)
}

func refusalMainCheckoutDirty(mainWorktree string) error {
	return fmt.Errorf(
		"main checkout %s has uncommitted changes; commit or stash them there first",
		mainWorktree,
	)
}

func refusalBlockingJournalFindings(branch string, blocking []reviewjournal.Entry) error {
	return fmt.Errorf(
		"refusing to merge %s: open review-journal finding(s) %s;"+
			" settle them with `devgeta task review-note --settle <id>`",
		branch, strings.Join(entryIDs(blocking), ", "),
	)
}

// refusalDivergenceProbeUnanswerable wraps ancestorErr with %w (rather than
// %v) so worktreeFinishMerge's returned error keeps the chain to the
// underlying git failure for any future errors.Is/As caller; worktreeFinishCheck
// only reads back .Error(), which renders identically either way — %w and %v
// produce the same formatted text, %w only additionally makes the argument
// unwrappable.
func refusalDivergenceProbeUnanswerable(branch, defaultBranch string, ancestorErr error) error {
	return fmt.Errorf(
		"cannot determine whether %s needs a rebase against %s: %w",
		branch, defaultBranch, ancestorErr,
	)
}

// worktreeFinishMerge rebases (if needed), fast-forward-merges from the main
// checkout, then removes the worktree and deletes its branch. Every step is
// sequenced so a failure partway through leaves inspectable state and an
// actionable message: nothing is removed until the fast-forward merge has
// actually landed the branch's commits on the default branch.
func (tm *TaskManager) worktreeFinishMerge(wtPath, branch string) (string, error) {
	dirty, err := tm.Git.IsWorktreeDirty(wtPath)
	if err != nil {
		return "", fmt.Errorf("failed to check worktree status: %w", err)
	}
	if dirty {
		return "", refusalDirtyWorktree()
	}

	defaultBranch := tm.Git.DefaultBranchIn(wtPath)

	mainWorktree, err := tm.Git.GetMainWorktree(wtPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve main worktree: %w", err)
	}

	mainBranch, err := tm.Git.CurrentBranchIn(mainWorktree)
	if err != nil {
		return "", fmt.Errorf("failed to check main checkout's branch: %w", err)
	}
	if mainBranch != defaultBranch {
		return "", refusalMainCheckoutWrongBranch(mainWorktree, mainBranch, defaultBranch)
	}

	mainDirty, err := tm.Git.IsWorktreeDirty(mainWorktree)
	if err != nil {
		return "", fmt.Errorf("failed to check main checkout status: %w", err)
	}
	if mainDirty {
		return "", refusalMainCheckoutDirty(mainWorktree)
	}

	blocking, err := tm.openBlockingFindings(mainWorktree, wtPath, branch)
	if err != nil {
		return "", fmt.Errorf("failed to check the review journal: %w", err)
	}
	if len(blocking) > 0 {
		return "", refusalBlockingJournalFindings(branch, blocking)
	}

	// IsAncestor reports whether defaultBranch is an ancestor of the branch's
	// HEAD, i.e. whether the branch has diverged (default gained commits
	// since the branch point) and needs a rebase before it can
	// fast-forward-merge. Shared with worktreeFinishCheck's advisory
	// "rebase:" line so the two paths can never disagree about this
	// precondition (plan §7's top risk).
	isAncestor, ancestorErr := tm.Git.IsAncestor(wtPath, defaultBranch, "HEAD")
	if ancestorErr != nil {
		return "", refusalDivergenceProbeUnanswerable(branch, defaultBranch, ancestorErr)
	}
	if !isAncestor {
		if err := tm.Git.ExecuteCommandAt(wtPath, "rebase", defaultBranch); err != nil {
			return "", fmt.Errorf(
				"%s diverged from %s and rebase failed: %w"+
					" (resolve conflicts in %s, or run `git -C %s rebase --abort`)",
				branch, defaultBranch, err, wtPath, wtPath,
			)
		}
	}

	if err := tm.Git.ExecuteCommandAt(mainWorktree, "merge", "--ff-only", branch); err != nil {
		return "", fmt.Errorf(
			"fast-forward merge of %s into %s failed: %w (worktree left in place at %s)",
			branch, defaultBranch, err, wtPath,
		)
	}

	if err := tm.Git.RemoveWorktree(wtPath, true, branch); err != nil {
		// RemoveWorktree fails in two distinct sub-cases that must not share a
		// message: `worktree remove` itself failing (the worktree is genuinely
		// still there) vs. `worktree remove` succeeding and only the following
		// `branch -D` failing (the worktree is already gone). Only the latter
		// carries git.ErrBranchDeleteFailed — anything else means removal
		// itself never completed.
		//
		// This used to match on a substring of RemoveWorktree's message, which
		// silently stopped working the moment that wording was reworded: the
		// merge path then reported "worktree still at <path>" for a worktree
		// git had already deleted. errors.Is cannot rot that way.
		if errors.Is(err, git.ErrBranchDeleteFailed) {
			return "", fmt.Errorf(
				"merged %s into %s and removed the worktree, but failed to delete branch %s: %w",
				branch, defaultBranch, branch, err,
			)
		}
		return "", fmt.Errorf(
			"merged %s into %s, but failed to remove worktree/delete branch: %w"+
				" (worktree still at %s)",
			branch, defaultBranch, err, wtPath,
		)
	}

	tm.dropReviewJournal(mainWorktree, branch)
	return fmt.Sprintf("Merged %s into %s; removed worktree %s", branch, defaultBranch, wtPath), nil
}

// worktreeFinishCheck computes worktree-finish --check's read-only readiness
// report. It never rebases, never moves HEAD or any ref, and never fetches —
// the only git write its calls may cause is MergeTreeConflicts writing
// unreferenced loose objects, which that method's own design already accounts
// for.
//
// Every field is gathered unconditionally, even once a blocking condition is
// known: the report's whole value is showing the complete picture, so nothing
// here short-circuits the way worktreeFinishMerge does. Only the "ready:"
// line reflects a first-blocking-reason order, and that order deliberately
// mirrors worktreeFinishMerge's own refusal order (dirty worktree, main
// checkout branch mismatch, main checkout dirty, blocking journal findings,
// unanswerable divergence probe) so the two commands can never disagree about
// which reason surfaces first, worded identically to worktreeFinishMerge's
// own refusal messages for the same conditions.
//
// A genuine failure (any gather step returning an unexpected error) returns
// ("", false, err) — the report could not be computed at all. "ready: no" is
// a SUCCESSFULLY computed report whose payload says not ready; that returns
// (report, false, nil).
func (tm *TaskManager) worktreeFinishCheck(
	wtPath, branch string,
) (report string, ready bool, err error) {
	defaultBranch := tm.Git.DefaultBranchIn(wtPath)

	dirty, err := tm.Git.IsWorktreeDirty(wtPath)
	if err != nil {
		return "", false, fmt.Errorf("failed to check worktree status: %w", err)
	}

	mainWorktree, err := tm.Git.GetMainWorktree(wtPath)
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve main worktree: %w", err)
	}

	mainBranch, err := tm.Git.CurrentBranchIn(mainWorktree)
	if err != nil {
		return "", false, fmt.Errorf("failed to check main checkout's branch: %w", err)
	}

	mainDirty, err := tm.Git.IsWorktreeDirty(mainWorktree)
	if err != nil {
		return "", false, fmt.Errorf("failed to check main checkout status: %w", err)
	}

	// Ahead/behind against the LOCAL default branch, never origin/ — this
	// reuses parseAheadBehind (scope.go) rather than tm.aheadBehind, which
	// hardcodes origin/ and takes no directory (wrong for this command twice
	// over).
	//
	// Degrades to "unknown" rather than aborting the whole report: this probes
	// the EXACT SAME ref (defaultBranch) that Git.IsAncestor probes just below,
	// so a repo with no LOCAL branch by that name (only feature branches ever
	// checked out, or DefaultBranchIn's "main" fallback in a repo whose real
	// default is "trunk") fails HERE first. That is the "unanswerable
	// divergence probe" scenario the plan calls out as reachable, not
	// hypothetical — aborting here (returning ("", false, err)) would mean
	// IsAncestor's own designed rebase:/ready: handling of that exact scenario
	// could never actually render. Never affects ready: — IsAncestor's own
	// failure below is what blocks, exactly as designed; this is display-only.
	var ahead, behind int
	aheadBehindOut, aheadBehindErr := tm.Git.RunCapture(
		atDir(wtPath, "rev-list", "--left-right", "--count", defaultBranch+"...HEAD")...,
	)
	if aheadBehindErr == nil {
		behind, ahead, aheadBehindErr = parseAheadBehind(aheadBehindOut)
	}

	// The SAME probe Part 0 wired into worktreeFinishMerge — never a second
	// implementation.
	isAncestor, ancestorErr := tm.Git.IsAncestor(wtPath, defaultBranch, "HEAD")

	// Advisory information, always reported regardless of whether a rebase is
	// needed.
	conflicts, conflictsErr := tm.Git.MergeTreeConflicts(wtPath, defaultBranch, "HEAD")

	blocking, stale, err := tm.journalOpenFindings(mainWorktree, wtPath, branch)
	if err != nil {
		return "", false, fmt.Errorf("failed to check the review journal: %w", err)
	}

	docStatusLines, err := worktreeDocStatusLines(tm.Git, wtPath, defaultBranch)
	if err != nil {
		return "", false, err
	}

	// First-blocking-reason ordering, pinned to worktreeFinishMerge's own
	// refusal order and worded identically to its refusal messages for the
	// same conditions.
	var reason string
	switch {
	case dirty:
		reason = refusalDirtyWorktree().Error()
	case mainBranch != defaultBranch:
		reason = refusalMainCheckoutWrongBranch(mainWorktree, mainBranch, defaultBranch).Error()
	case mainDirty:
		reason = refusalMainCheckoutDirty(mainWorktree).Error()
	case len(blocking) > 0:
		reason = refusalBlockingJournalFindings(branch, blocking).Error()
	case ancestorErr != nil:
		reason = refusalDivergenceProbeUnanswerable(branch, defaultBranch, ancestorErr).Error()
	}
	ready = reason == ""

	dirtyLabel := "no"
	if dirty {
		dirtyLabel = "yes"
	}
	mainState := "clean"
	if mainDirty {
		mainState = "dirty"
	}

	var aheadBehindLine string
	switch {
	case aheadBehindErr != nil:
		aheadBehindLine = fmt.Sprintf(
			"ahead: unknown  behind: unknown (%s)",
			aheadBehindErr.Error(),
		)
	default:
		aheadBehindLine = fmt.Sprintf("ahead: %d  behind: %d", ahead, behind)
	}

	var rebaseLine string
	switch {
	case ancestorErr != nil:
		rebaseLine = fmt.Sprintf("unknown (%s)", ancestorErr.Error())
	case isAncestor:
		rebaseLine = "not needed"
	default:
		rebaseLine = "needed"
	}

	var conflictsLine string
	switch {
	case conflictsErr != nil:
		conflictsLine = fmt.Sprintf(
			"unknown (git merge-tree failed: %s — advisory, does not block)", conflictsErr.Error(),
		)
	case len(conflicts) == 0:
		conflictsLine = "none (advisory, does not block)"
	default:
		conflictsLine = fmt.Sprintf(
			"%d (%s — advisory, does not block)", len(conflicts), strings.Join(conflicts, ", "),
		)
	}

	journalOpenLine := "0"
	if len(blocking) > 0 {
		journalOpenLine = fmt.Sprintf(
			"%d (%s)",
			len(blocking),
			strings.Join(entryIDs(blocking), ", "),
		)
	}
	journalStaleLine := "0"
	if len(stale) > 0 {
		journalStaleLine = fmt.Sprintf(
			"%d (%s — advisory, does not block)", len(stale), strings.Join(entryIDs(stale), ", "),
		)
	}

	lines := []string{
		fmt.Sprintf("worktree: %s", wtPath),
		fmt.Sprintf("branch: %s", branch),
		fmt.Sprintf("default: %s", defaultBranch),
		fmt.Sprintf("dirty: %s", dirtyLabel),
		aheadBehindLine,
		fmt.Sprintf("main-checkout: %s (%s)", mainBranch, mainState),
		fmt.Sprintf("rebase: %s", rebaseLine),
		fmt.Sprintf("conflicts: %s", conflictsLine),
		fmt.Sprintf("journal-open: %s", journalOpenLine),
		fmt.Sprintf("journal-stale-open: %s", journalStaleLine),
	}
	lines = append(lines, docStatusLines...)
	if ready {
		lines = append(lines, "ready: yes")
	} else {
		lines = append(lines, "ready: no — "+reason)
	}

	return strings.Join(lines, "\n"), ready, nil
}

// worktreeDocStatusLines sweeps every changed-or-untracked markdown file
// between wtPath and its local merge-base with defaultBranch (never origin/),
// and renders one "doc-status: <path>: <value>" line per file that still
// exists in the working tree, sorted by path for deterministic output.
//
// changedFiles and untrackedFiles are reused verbatim rather than
// re-implemented: changedFiles reads committed+working-tree diffs against a
// committed base, and untrackedFiles lists files git doesn't track at all, so
// their union is every markdown path the branch touches. A path is skipped
// (no line at all) when the branch or an uncommitted edit deleted it — there
// is nothing to read a status marker out of.
func worktreeDocStatusLines(g *git.Git, wtPath, defaultBranch string) ([]string, error) {
	baseOut, err := g.RunCapture(atDir(wtPath, "merge-base", defaultBranch, "HEAD")...)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve merge base with %s: %w", defaultBranch, err)
	}
	base := strings.TrimSpace(baseOut)

	changes, err := changedFiles(g, wtPath, base)
	if err != nil {
		return nil, fmt.Errorf("failed to gather changed files: %w", err)
	}
	untracked := untrackedFiles(g, wtPath)

	paths := make(map[string]struct{})
	for _, c := range changes {
		if strings.HasSuffix(c.Path, ".md") {
			paths[c.Path] = struct{}{}
		}
	}
	for _, u := range untracked {
		if strings.HasSuffix(u, ".md") {
			paths[u] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	var lines []string
	for _, p := range sorted {
		full := filepath.Join(wtPath, p)
		if _, statErr := os.Stat(full); statErr != nil {
			if os.IsNotExist(statErr) {
				// Deleted by the branch or by an uncommitted edit — nothing to
				// read a status marker out of, and not an error.
				continue
			}
			return nil, fmt.Errorf("failed to check %s: %w", p, statErr)
		}
		content, readErr := os.ReadFile(full)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read %s: %w", p, readErr)
		}
		marker := statusMarker(string(content))
		if marker == "" {
			marker = "no status marker"
		}
		lines = append(lines, fmt.Sprintf("doc-status: %s: %s", p, marker))
	}
	return lines, nil
}

// journalOpenFindings loads branch's journal once (via mainWorktree, per the
// original openBlockingFindings' doc comment below) and classifies every open
// entry by freshness judged against wtPath (see that same comment for why
// wtPath and not mainWorktree). blocking is Fresh+Dateless; stale is
// FreshnessStale. It is the shared probe behind worktreeFinishMerge's refusal
// (openBlockingFindings, kept below as a thin wrapper so that call site and
// its tests are untouched) and worktreeFinishCheck's readiness report
// (ADR-0027) — both need the same classification of the same load, so it
// happens in exactly one place.
//
// The journal's LOCATION resolves through mainWorktree, matching
// dropReviewJournal: Manager.PathFor resolves the common git directory, which
// the main checkout and any linked worktree of the same repo share, so
// mainWorktree finds the right file. Freshness is judged separately, and
// deliberately NOT against that same directory: Manager.Verdict hashes the
// cited file's CURRENT on-disk content in whatever directory it's given, so it
// has to run against wtPath — the checkout that actually holds the branch's
// commits. Judging it against mainWorktree would hash the file as it sits on
// the (unrelated) default branch, so a finding citing a file the branch just
// fixed would almost always compare against the old, pre-fix blob and read
// stale — silently letting a real, unresolved finding through (ADR-0027).
//
// Settled entries are never considered — only Open() ones can block or go
// stale. A FreshnessStale open entry does not block (its cited file has
// already changed since the finding was raised); FreshnessFresh and
// FreshnessDateless (a pathless, design-level question with nothing
// mechanical to invalidate it) both do.
func (tm *TaskManager) journalOpenFindings(
	mainWorktree, wtPath, branch string,
) (blocking, stale []reviewjournal.Entry, err error) {
	jm := reviewjournal.New(tm.Git)
	j, err := jm.Load(mainWorktree, branch)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range j.Entries {
		if !e.Open() {
			continue
		}
		if jm.Verdict(wtPath, e) == reviewjournal.FreshnessStale {
			stale = append(stale, e)
		} else {
			blocking = append(blocking, e)
		}
	}
	return blocking, stale, nil
}

// openBlockingFindings returns every open review-journal entry for branch that
// must block a merge. Kept as a thin wrapper over journalOpenFindings so
// worktreeFinishMerge's existing call site and tests are untouched.
func (tm *TaskManager) openBlockingFindings(
	mainWorktree, wtPath, branch string,
) ([]reviewjournal.Entry, error) {
	blocking, _, err := tm.journalOpenFindings(mainWorktree, wtPath, branch)
	return blocking, err
}

// entryIDs returns each entry's ID, in order, for a report line or error
// message that must name which findings are involved.
func entryIDs(entries []reviewjournal.Entry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

// dropReviewJournal deletes branch's review journal once the branch itself is
// gone (ADR-0012). Every path that deletes a worktree's branch must call this,
// or the journal outlives the work it describes: worktree-finish deletes the
// branch through Git.RemoveWorktree directly rather than through
// WorktreeManager.removeByRepo, so it does not inherit that path's cleanup and
// needs its own.
//
// repoDir must be a path inside the repo that still exists after the removal —
// the main checkout, never the deleted worktree — because resolving the
// journal's location runs git there.
//
// Best-effort: the worktree and branch are already gone by the time this runs,
// so failing the command over a leftover text file would report failure for an
// operation that succeeded. A survivor is self-correcting —
// `devgeta task review-notes --prune` drops journals whose branch no longer
// exists, which this one now satisfies.
func (tm *TaskManager) dropReviewJournal(repoDir, branch string) {
	if branch == "" {
		return
	}
	if err := reviewjournal.New(tm.Git).Delete(repoDir, branch); err != nil {
		logger.L().Debugw(
			"worktree-finish: could not delete the branch's review journal",
			"branch", branch,
			"error", err,
		)
	}
}

// resolveWorktreeTarget implements worktree-finish's deterministic target
// selection: an explicit name wins; otherwise cwd resolving inside a linked
// worktree wins; otherwise it errors listing the worktrees it found. It never
// falls back to guessing from a main checkout.
func (tm *TaskManager) resolveWorktreeTarget(name string) (wtPath, branch string, err error) {
	if name != "" {
		wtPath, err = tm.findWorktreePath(name)
		if err != nil {
			return "", "", err
		}
		branch, err = tm.branchForWorktree(wtPath)
		return wtPath, branch, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}

	repoRoot, rootErr := tm.Git.GetRepoRootIn(cwd)
	if rootErr != nil {
		return "", "", fmt.Errorf(
			"not inside a git repository; specify <name>.%s", tm.availableWorktreesNote(),
		)
	}

	mainWorktree, mainErr := tm.Git.GetMainWorktree(cwd)
	if mainErr != nil {
		return "", "", fmt.Errorf("failed to resolve main worktree: %w", mainErr)
	}

	if config.CanonicalRepoPath(repoRoot) == config.CanonicalRepoPath(mainWorktree) {
		return "", "", fmt.Errorf(
			"not inside a linked worktree (this is the main checkout); specify <name>.%s",
			tm.availableWorktreesNote(),
		)
	}

	wtPath = repoRoot
	branch, err = tm.branchForWorktree(wtPath)
	return wtPath, branch, err
}

// findWorktreePath resolves an explicit worktree name to its full path by
// scanning the centralized worktree base path (the same one `dg wt` uses)
// for a repo slug containing it.
func (tm *TaskManager) findWorktreePath(name string) (string, error) {
	base := worktree.GetWorktreeBasePath()
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no worktree named %q found; no worktrees exist yet", name)
		}
		return "", err
	}

	flat := worktree.FlattenName(name)
	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(base, e.Name(), flat)
		if _, statErr := os.Stat(candidate); statErr == nil {
			matches = append(matches, candidate)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no worktree named %q found.%s", name, tm.availableWorktreesNote())
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf(
			"multiple worktrees named %q found (%s); remove the ambiguity and retry",
			name, strings.Join(matches, ", "),
		)
	}
}

// branchForWorktree resolves the branch checked out at wtPath via `git
// worktree list --porcelain`, run from wtPath itself so it works regardless
// of the process's actual working directory.
func (tm *TaskManager) branchForWorktree(wtPath string) (string, error) {
	return tm.Git.BranchForWorktree(wtPath)
}

// availableWorktreesNote renders the "never guess, list what's available"
// half of a target-resolution error.
func (tm *TaskManager) availableWorktreesNote() string {
	names := listAvailableWorktrees()
	if len(names) == 0 {
		return " No worktrees exist yet."
	}
	return " Available worktrees: " + strings.Join(names, ", ")
}

// listAvailableWorktrees walks the centralized worktree base path
// (repo-slug/flat-name directories) for display in an error message only —
// it does not query git or tmux, so it stays cheap and dependency-free; `dg
// wt list` remains the one command that reports live git/tmux status.
func listAvailableWorktrees() []string {
	base := worktree.GetWorktreeBasePath()
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var names []string
	for _, repoEntry := range entries {
		if !repoEntry.IsDir() {
			continue
		}
		repoDir := filepath.Join(base, repoEntry.Name())
		wtEntries, err := os.ReadDir(repoDir)
		if err != nil {
			continue
		}
		for _, wtEntry := range wtEntries {
			if !wtEntry.IsDir() {
				continue
			}
			names = append(names, repoEntry.Name()+"/"+wtEntry.Name())
		}
	}
	return names
}
