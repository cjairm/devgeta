package task

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	git_app "github.com/cjairm/devgeta/internal/apps/git"
)

// reviewFetchTimeout bounds the network fetch every review-oriented task makes
// before reading refs, so a hung remote can't block a caller expecting a fast,
// single-call response. Shared by ReviewScope and PRReviewTarget: they differ
// in what a failed fetch MEANS (ReviewScope reports it and carries on;
// PRReviewTarget ends with an error, ADR-0023 §1), not in how long either is
// willing to wait.
const reviewFetchTimeout = 10 * time.Second

// commitFieldSep and commitRecordSep delimit commitLog's git log format:
// %h/%as/%s/%b are joined with commitFieldSep and each commit is terminated
// with commitRecordSep. Subjects and (especially) bodies can contain spaces
// and newlines, so a plain-space/newline join would be ambiguous to parse
// back apart — these ASCII unit/record separators can't appear in commit
// text, so the format is unambiguous regardless of --bodies.
const (
	commitFieldSep  = "\x1f"
	commitRecordSep = "\x1e"
)

// commitLogFormat requests short SHA, ISO author date (%as — git formats
// this for us, no Go-side date parsing needed), subject, and body per commit.
const commitLogFormat = "%h" + commitFieldSep + "%as" + commitFieldSep + "%s" + commitFieldSep + "%b" + commitRecordSep

// commit is one commit from a base..HEAD range.
type commit struct {
	SHA     string
	Date    string
	Subject string
	Body    string
}

// fileChange is one file's row from a merged --numstat / --name-status pair.
// Status is left empty until merged with --name-status output. Binary files
// report Added/Removed as 0 (numstat uses "-" for both, per its format).
type fileChange struct {
	Path    string
	Status  string
	Added   int
	Removed int
	Binary  bool
}

// scopeData is the orchestration result handed to formatReviewScope.
type scopeData struct {
	OnDefaultBranch bool
	Detached        bool
	DetachedSHA     string

	CurrentBranch string
	DefaultBranch string
	FetchFailed   bool

	Behind int
	Ahead  int

	Commits []commit
	Bodies  bool

	Files    []fileChange
	Excluded []fileChange
	// Uncommitted is the subset of the branch's changes that no commit
	// carries yet (working tree against HEAD). Those files ARE in Files —
	// the file table covers the whole branch state, committed or not — so this
	// exists only to say which of them a reviewer will not find in the commit
	// list above.
	Uncommitted []fileChange
	// Untracked is the files git does not track yet. They are NOT in Files:
	// `git diff` cannot see them, so there are no counts to report, only names.
	Untracked []string
}

// ReviewScope fetches origin (best-effort, bounded), resolves the comparison
// base against the repo's default branch, and returns a compact orientation
// report: ahead/behind, commit subjects (with dates, and bodies when
// requested), and a per-file stat table with lockfile-style noise pulled into
// a separate excluded-files note.
//
// The file table covers everything the branch would merge — its commits AND
// its uncommitted work (ADR-0019), the same as BranchDiff, whose diff a
// reviewer reads right after this. Which of those files are not in any commit
// yet, and which are untracked, are each called out on their own line: a file
// the commit list above never mentions would otherwise look like a mistake.
func (tm *TaskManager) ReviewScope(bodies bool) (string, error) {
	fetchFailed := tm.Git.FetchOriginTimeout(reviewFetchTimeout) != nil

	currentBranch, err := tm.Git.CurrentBranch()
	if err != nil {
		return "", fmt.Errorf("review-scope: %w", err)
	}

	if currentBranch == "" {
		sha, err := tm.Git.ShortHead()
		if err != nil {
			return "", fmt.Errorf("review-scope: %w", err)
		}
		return formatReviewScope(scopeData{Detached: true, DetachedSHA: sha}), nil
	}

	defaultBranch := tm.Git.DefaultBranch()
	if currentBranch == defaultBranch {
		return formatReviewScope(scopeData{
			OnDefaultBranch: true,
			DefaultBranch:   defaultBranch,
		}), nil
	}

	base, err := tm.mergeBase(defaultBranch)
	if err != nil {
		return "", fmt.Errorf("review-scope: %w", err)
	}

	behind, ahead, err := tm.aheadBehind(defaultBranch)
	if err != nil {
		return "", fmt.Errorf("review-scope: %w", err)
	}

	commits, err := tm.commitLog(base)
	if err != nil {
		return "", fmt.Errorf("review-scope: %w", err)
	}

	// Two dots, not three: `git diff <base>` compares the merge-base against
	// the WORKING TREE, so staged and unstaged edits are counted alongside
	// committed ones. `<base>...HEAD` would report only what is committed.
	files, err := fileChanges(tm.Git, base)
	if err != nil {
		return "", fmt.Errorf("review-scope: %w", err)
	}
	reviewable, excluded := partitionExcluded(files)

	// HEAD against the working tree: what the branch changes that no commit
	// carries yet. Excluded paths are dropped here too, so a lockfile nobody
	// is reviewing does not show up as uncommitted work.
	uncommitted, err := fileChanges(tm.Git, "HEAD")
	if err != nil {
		return "", fmt.Errorf("review-scope: %w", err)
	}
	uncommittedReviewable, _ := partitionExcluded(uncommitted)

	return formatReviewScope(scopeData{
		CurrentBranch: currentBranch,
		DefaultBranch: defaultBranch,
		FetchFailed:   fetchFailed,
		Behind:        behind,
		Ahead:         ahead,
		Commits:       commits,
		Bodies:        bodies,
		Files:         reviewable,
		Excluded:      excluded,
		Uncommitted:   uncommittedReviewable,
		Untracked:     untrackedFiles(tm.Git, ""),
	}), nil
}

// mergeBase resolves the merge-base between origin/<defaultBranch> and HEAD —
// the comparison base reused for ahead/behind, commit log, and the diff.
func (tm *TaskManager) mergeBase(defaultBranch string) (string, error) {
	return tm.Git.MergeBase("origin/"+defaultBranch, "HEAD")
}

// aheadBehind returns how many commits HEAD is ahead/behind origin/<defaultBranch>.
func (tm *TaskManager) aheadBehind(defaultBranch string) (behind, ahead int, err error) {
	out, err := tm.Git.RunCapture(
		"rev-list", "--left-right", "--count", "origin/"+defaultBranch+"...HEAD",
	)
	if err != nil {
		return 0, 0, err
	}
	return parseAheadBehind(out)
}

// parseAheadBehind parses `git rev-list --left-right --count A...B` output
// ("<only-in-A>\t<only-in-B>") into (behind, ahead) when A is the upstream
// default branch and B is HEAD.
func parseAheadBehind(raw string) (behind, ahead int, err error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list --left-right --count output: %q", raw)
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("unexpected rev-list count %q: %w", fields[0], err)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("unexpected rev-list count %q: %w", fields[1], err)
	}
	return behind, ahead, nil
}

// commitLog returns commits for base..HEAD, oldest first, each with its short
// SHA, ISO author date, subject, and body. The body is always fetched (one
// git invocation regardless of --bodies); formatReviewScope decides whether
// to render it.
func (tm *TaskManager) commitLog(base string) ([]commit, error) {
	out, err := tm.Git.RunCapture("log", "--format="+commitLogFormat, "--reverse", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	return parseCommitLog(out)
}

// parseCommitLog parses commitLogFormat's delimited output into commits.
func parseCommitLog(raw string) ([]commit, error) {
	var out []commit
	for _, rec := range strings.Split(raw, commitRecordSep) {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, commitFieldSep, 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("malformed commit log record: %q", rec)
		}
		out = append(out, commit{
			SHA:     parts[0],
			Date:    parts[1],
			Subject: parts[2],
			Body:    strings.Trim(parts[3], "\n"),
		})
	}
	return out, nil
}

// fileChanges runs numstat + name-status over rangeSpec (both --no-renames,
// so a rename deterministically renders as a D + A pair) and merges them by
// path into one fileChange list.
//
// It takes the git app rather than hanging off TaskManager (the same shape as
// untrackedFiles) because PRManager gathers the same table for a pull request
// it has fetched but not checked out — one gather, two callers, no second
// copy of the numstat/name-status merge to drift.
func fileChanges(g *git_app.Git, rangeSpec string) ([]fileChange, error) {
	numstatOut, err := g.RunCapture("diff", "--numstat", "--no-renames", rangeSpec)
	if err != nil {
		return nil, err
	}
	nameStatusOut, err := g.RunCapture("diff", "--name-status", "--no-renames", rangeSpec)
	if err != nil {
		return nil, err
	}

	changes, err := parseNumstat(numstatOut)
	if err != nil {
		return nil, err
	}
	statuses, err := parseNameStatus(nameStatusOut)
	if err != nil {
		return nil, err
	}
	for i := range changes {
		if s, ok := statuses[changes[i].Path]; ok {
			changes[i].Status = s
		} else {
			changes[i].Status = "?"
		}
	}
	return changes, nil
}

// parseNumstat parses `git diff --numstat` output into fileChange entries.
// Status is left unset here; callers merge in --name-status separately.
func parseNumstat(raw string) ([]fileChange, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []fileChange
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("malformed numstat line: %q", line)
		}
		fc := fileChange{Path: parts[2]}
		if parts[0] == "-" && parts[1] == "-" {
			fc.Binary = true
		} else {
			added, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("malformed numstat added count in %q: %w", line, err)
			}
			removed, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("malformed numstat removed count in %q: %w", line, err)
			}
			fc.Added, fc.Removed = added, removed
		}
		out = append(out, fc)
	}
	return out, nil
}

// parseNameStatus parses `git diff --name-status` output into a path->status
// map. A status wider than one letter (e.g. a similarity score on a copy) is
// truncated to its leading letter.
func parseNameStatus(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	statuses := map[string]string{}
	if raw == "" {
		return statuses, nil
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed name-status line: %q", line)
		}
		status := parts[0]
		if len(status) > 1 {
			status = status[:1]
		}
		statuses[parts[1]] = status
	}
	return statuses, nil
}

// partitionExcluded splits changes into reviewable and excluded (default
// lockfile/generated-asset patterns), preserving each list's relative order.
func partitionExcluded(changes []fileChange) (reviewable, excluded []fileChange) {
	for _, c := range changes {
		if isExcludedPath(c.Path) {
			excluded = append(excluded, c)
		} else {
			reviewable = append(reviewable, c)
		}
	}
	return reviewable, excluded
}

// formatFileStats renders one row per file (status, path, and either
// "binary" or "+added/-removed") followed by a running "total: +X/-Y" line,
// with no leading or trailing newline. Shared by formatReviewScope and
// formatReviewPackage, whose only difference is what each does for an empty
// file list: formatReviewScope always wants the "total: +0/-0" line, so an
// empty list here just yields that with no rows above it; formatReviewPackage
// wants a sentinel line instead of any of this, so it skips calling this
// function entirely when there are no included files rather than passing a
// flag through it.
func formatFileStats(files []fileChange) string {
	var b strings.Builder
	var totalAdded, totalRemoved int
	for _, f := range files {
		if f.Binary {
			fmt.Fprintf(&b, "%-2s %s  binary\n", f.Status, f.Path)
			continue
		}
		fmt.Fprintf(&b, "%-2s %s  +%d/-%d\n", f.Status, f.Path, f.Added, f.Removed)
		totalAdded += f.Added
		totalRemoved += f.Removed
	}
	fmt.Fprintf(&b, "total: +%d/-%d", totalAdded, totalRemoved)
	return b.String()
}

// formatExclusionNotes renders the "excluded (see `<hint>` to inspect): ..."
// line listing every excluded file (binary files noted as such, others with
// their +added/-removed counts), with no leading or trailing newline. Returns
// "" when excluded is empty so callers can test the result instead of the
// input slice. hint is the follow-up command to print inside the backticks —
// it differs per call site (review-package needs its own base/head range;
// branch-diff and review-scope both point at `dg task branch-diff --file`),
// so callers own their surrounding whitespace and pass their own hint text.
func formatExclusionNotes(excluded []fileChange, hint string) string {
	if len(excluded) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"excluded (see `%s` to inspect): %s",
		hint, formatFileNotes(excluded),
	)
}

// sameFileSet reports whether two file lists name exactly the same paths,
// order and counts aside. Lengths alone are not enough: two lists of equal
// length can name different files, and collapsing the uncommitted note on that
// basis would claim the whole table is uncommitted when it is not.
func sameFileSet(a, b []fileChange) bool {
	if len(a) != len(b) {
		return false
	}
	paths := make(map[string]struct{}, len(a))
	for _, f := range a {
		paths[f.Path] = struct{}{}
	}
	for _, f := range b {
		if _, ok := paths[f.Path]; !ok {
			return false
		}
	}
	return true
}

// formatFileNotes renders files as a comma-joined inline list, each with its
// +added/-removed counts (or "binary"). Shared by every note line that names a
// set of files without giving them their own table rows — the excluded-files
// note and review-scope's uncommitted-files note — so the two can't drift into
// two spellings of the same thing.
func formatFileNotes(files []fileChange) string {
	notes := make([]string, len(files))
	for i, f := range files {
		if f.Binary {
			notes[i] = fmt.Sprintf("%s (binary)", f.Path)
		} else {
			notes[i] = fmt.Sprintf("%s (+%d/-%d)", f.Path, f.Added, f.Removed)
		}
	}
	return strings.Join(notes, ", ")
}

// formatReviewScope renders scopeData as the compact, LLM-oriented report
// (or one of the two stable edge sentinels). Output is payload-only: no
// leading prose, no markdown headers/tables — see the cycle doc's output
// design principles.
func formatReviewScope(s scopeData) string {
	if s.Detached {
		return fmt.Sprintf(
			"Detached HEAD at %s — no branch to compare. Check out a branch or name a target.",
			s.DetachedSHA,
		)
	}
	if s.OnDefaultBranch {
		return fmt.Sprintf(
			"On %s — no branch to compare. Review uncommitted changes or name a target.",
			s.DefaultBranch,
		)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "branch: %s -> %s (default)  [ahead %d, behind %d]",
		s.CurrentBranch, s.DefaultBranch, s.Ahead, s.Behind)
	if s.FetchFailed {
		b.WriteString("  (fetch failed — comparing against local refs)")
	}
	b.WriteString("\n")

	b.WriteString("commits:\n")
	if len(s.Commits) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, c := range s.Commits {
			fmt.Fprintf(&b, "- %s %s %s\n", c.SHA, c.Date, c.Subject)
			if s.Bodies && c.Body != "" {
				for _, line := range strings.Split(c.Body, "\n") {
					if line == "" {
						b.WriteString("\n")
						continue
					}
					fmt.Fprintf(&b, "    %s\n", line)
				}
			}
		}
	}

	fmt.Fprintf(&b, "files (%d):\n", len(s.Files))
	b.WriteString(formatFileStats(s.Files))

	// Both notes name files the commit list above cannot account for, so they
	// sit directly under the table those files appear in (or, for untracked
	// files, do not).
	if len(s.Uncommitted) > 0 {
		// A branch of pure work-in-progress has every table row uncommitted, and
		// there the per-file list is the table again, path for path — pure cost
		// for a reader who already has it. Say it once instead.
		if sameFileSet(s.Uncommitted, s.Files) {
			fmt.Fprintf(&b, "\nuncommitted: every file above, in no commit yet")
		} else {
			fmt.Fprintf(
				&b,
				"\nuncommitted (in the table above, in no commit yet): %s",
				formatFileNotes(s.Uncommitted),
			)
		}
	}
	if len(s.Untracked) > 0 {
		fmt.Fprintf(
			&b,
			"\nuntracked (not in the table above, no diff — read them directly): %s",
			strings.Join(s.Untracked, ", "),
		)
	}

	if note := formatExclusionNotes(s.Excluded, "dg task branch-diff --file <path>"); note != "" {
		b.WriteString("\n")
		b.WriteString(note)
	}

	return b.String()
}
