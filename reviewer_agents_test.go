package main

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// TestBuiltinReviewersAgentNamesMatchEmbeddedAgentFiles is the companion to
// worktree.TestBuiltinReviewersAgentNamesMatchAgentFiles (internal/tooling/
// worktree/layout_test.go), which reads configs/shared/agents/ off disk
// because that package cannot import ConfigsFS (declared here in package
// main; importing it from internal/tooling/worktree would be an import
// cycle). This test instead reads the same directory through the embedded
// ConfigsFS, so a file that fails to embed (see main.go's `//go:embed
// all:configs`) is still caught even though the on-disk test alone would
// pass.
func TestBuiltinReviewersAgentNamesMatchEmbeddedAgentFiles(t *testing.T) {
	entries, err := fs.ReadDir(ConfigsFS, "configs/shared/agents")
	if err != nil {
		t.Fatalf("failed to read embedded configs/shared/agents: %v", err)
	}

	embedded := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		embedded[strings.TrimSuffix(entry.Name(), ".md")] = true
	}

	names := worktree.ReviewerAgentNames()
	if len(names) == 0 {
		t.Fatal("expected worktree.ReviewerAgentNames() to return at least one name, got none")
	}
	for _, name := range names {
		if !embedded[name] {
			t.Errorf(
				"reviewer agent %q has no matching file in embedded configs/shared/agents",
				name,
			)
		}
	}
}

// The review journal is local bookkeeping: it lives on one machine, and the
// command that settles an entry only exists where devgeta is installed. The
// reviewer agents used to close their report with a bare "Settle when
// answered: devgeta task review-note --settle ..." trailer, and /review-pr
// composed its inline comments straight from those reports — so the trailer
// rode along onto a pull request in a repository that is not devgeta's, where
// it read as an instruction the author could not run (they replied saying
// devgeta was not on their PATH and asking someone else to settle the entry).
//
// Prompt text is the only place this can be enforced, so each side states it:
// the agents mark the trailer and the ids as report-only, and every command
// that composes text for a shared surface says that local tooling is stripped
// before posting. This guard keeps both sides present — a file that prints the
// trailer without the rule, or a posting command that loses the rule, is the
// same leak returning.
func TestShippedPromptsKeepTheReviewJournalOffPostedText(t *testing.T) {
	const (
		settleTrailer = "Settle when answered:"
		// The heading marker is part of the literal on purpose: the agents also
		// cross-reference this section by name from their findings format, and a
		// bare title match would be satisfied by that cross-reference alone
		// even if the section itself were deleted.
		agentRule = "### Keep the journal out of what gets posted"
	)

	entries, err := fs.ReadDir(ConfigsFS, "configs/shared/agents")
	if err != nil {
		t.Fatalf("failed to read embedded configs/shared/agents: %v", err)
	}
	sawTrailer := false
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := "configs/shared/agents/" + entry.Name()
		data, err := fs.ReadFile(ConfigsFS, path)
		if err != nil {
			t.Fatalf("failed to read embedded %s: %v", path, err)
		}
		content := string(data)
		if !strings.Contains(content, settleTrailer) {
			continue
		}
		sawTrailer = true
		if !strings.Contains(content, agentRule) {
			t.Errorf(
				"%s prints the %q trailer but has no %q section — without it the trailer gets copied "+
					"into posted review comments, where the reader has no journal and cannot run the command",
				path,
				settleTrailer,
				agentRule,
			)
		}
	}
	if !sawTrailer {
		t.Errorf(
			"no agent under configs/shared/agents prints the %q trailer — if it was renamed, update "+
				"this guard and the posting commands' strip rule together",
			settleTrailer,
		)
	}

	// Each posting command carries the rule in its own words; this pins the
	// sentence that states it, so rewording the section has to come past this
	// test rather than silently dropping the rule.
	postingRules := []struct {
		path    string
		literal string
		why     string
	}{
		{
			path:    "configs/shared/commands/review-pr.md",
			literal: "None of that goes on the PR",
			why:     "it composes the inline comments from reviewer reports, so it is where the trailer and the ids get stripped",
		},
		{
			path:    "configs/shared/commands/address-feedback.md",
			literal: "The journal is local, and it stays out of the replies",
			why:     "it replies to reviewers, and a failed journal call must not turn into a reply asking the reviewer to run one",
		},
		{
			path:    "configs/shared/commands/approve-pr.md",
			literal: "no review-journal ids",
			why:     "it posts the approval body and the decline comment",
		},
		{
			path:    "configs/shared/skills/post-to-tracker/SKILL.md",
			literal: "Nothing that only works on your machine",
			why:     "it posts summaries to pull requests and tickets read by people outside this machine",
		},
	}
	for _, rule := range postingRules {
		data, err := fs.ReadFile(ConfigsFS, rule.path)
		if err != nil {
			t.Errorf("failed to read embedded %s: %v", rule.path, err)
			continue
		}
		if !strings.Contains(string(data), rule.literal) {
			t.Errorf(
				"%s must keep the rule that local tooling stays out of posted text (looked for %q) — %s",
				rule.path,
				rule.literal,
				rule.why,
			)
		}
	}
}
