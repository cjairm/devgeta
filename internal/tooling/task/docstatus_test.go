package task

// statusMarker is a pure string-parsing function (no git, no filesystem, no
// logger) — see ADR-0028. These tests are plain Go string constants only, no
// fixtures, no mocks, no testutil.

import "testing"

func TestStatusMarker_HeaderBlockLabel_AllRenderings(t *testing.T) {
	cases := map[string]string{
		"double-star colon inside":  "# Title\n\n**Status:** Draft\n\n## Context\n",
		"double-star colon outside": "# Title\n\n**Status**: Draft\n\n## Context\n",
		"plain":                     "# Title\n\nStatus: Draft\n\n## Context\n",
		"list item":                 "# Title\n\n* Status: Draft\n\n## Context\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if got := statusMarker(content); got != "Draft" {
				t.Errorf("statusMarker(%q) = %q, want %q", content, got, "Draft")
			}
		})
	}
}

func TestStatusMarker_FrontMatter(t *testing.T) {
	content := "---\nstatus: accepted\n---\n\n# Title\n\nBody text.\n"
	if got := statusMarker(content); got != "accepted" {
		t.Errorf("statusMarker(%q) = %q, want %q", content, got, "accepted")
	}
}

func TestStatusMarker_Precedence_FrontMatterBeatsHeaderBlock(t *testing.T) {
	content := "---\nstatus: accepted\n---\n\n# Title\n\n**Status:** Draft\n\n## Context\n"
	if got := statusMarker(content); got != "accepted" {
		t.Errorf("statusMarker(%q) = %q, want %q (front matter must win)", content, got, "accepted")
	}
}

func TestStatusMarker_StatusSection_Nygard(t *testing.T) {
	content := "# ADR Title\n\n## Status\n\nAccepted\n\n## Context\n\nSome context.\n"
	if got := statusMarker(content); got != "Accepted" {
		t.Errorf("statusMarker(%q) = %q, want %q", content, got, "Accepted")
	}
}

// --- Negative cases, each modeled on a real false-positive shape in this repo ---

func TestStatusMarker_StatusLegendHeading_DoesNotMatch(t *testing.T) {
	// Modeled on ROADMAP.md's "## Status Legend" section.
	content := "# Roadmap\n\n## Status Legend\n\n- Implemented — shipped\n- Planned — scheduled\n"
	if got := statusMarker(content); got != "" {
		t.Errorf(
			"statusMarker(%q) = %q, want \"\" (Status Legend must not match Status)",
			content,
			got,
		)
	}
}

func TestStatusMarker_StatusSectionOverTable_DoesNotMatch(t *testing.T) {
	// Modeled on docs/decisions/README.md: "## Status" directly over a legend table.
	content := "# Decisions\n\n## Status\n\n| Status | Meaning |\n| --- | --- |\n| **PROPOSED** | Under discussion |\n"
	if got := statusMarker(content); got != "" {
		t.Errorf(
			"statusMarker(%q) = %q, want \"\" (a table row is not a status value)",
			content,
			got,
		)
	}
}

func TestStatusMarker_LabelBelowHeaderBlock_DoesNotMatch(t *testing.T) {
	// Modeled on configs/shared/agents/code-reviewer.md: a **Status:** line
	// that appears only inside a report template, below the doc's first
	// "##" heading — not the document's own status.
	content := "# Code Reviewer\n\n## Output\n\n### Recommendation\n\n" +
		"**Status:** APPROVE | REQUEST CHANGES | NEEDS DISCUSSION\n\n" +
		"- Approve with minors: state it plainly\n"
	if got := statusMarker(content); got != "" {
		t.Errorf(
			"statusMarker(%q) = %q, want \"\" (label below header block must not match)",
			content,
			got,
		)
	}
}

func TestStatusMarker_LabelInAmendmentSection_DoesNotMatch(t *testing.T) {
	// Modeled on ADR-0014: a second "**Status:** ACCEPTED" inside an
	// Amendment section, below the document's real header-block status.
	content := "# ADR-0014 — Some decision\n\n**Date:** 2026-08-05\n**Status:** PROPOSED\n\n" +
		"## Context\n\nSome context.\n\n" +
		"## Amendment (2026-08-07) — a later note\n\n**Status:** ACCEPTED\n\nMore text.\n"
	if got := statusMarker(content); got != "PROPOSED" {
		t.Errorf(
			"statusMarker(%q) = %q, want %q (only the header-block status counts)",
			content,
			got,
			"PROPOSED",
		)
	}
}

func TestStatusMarker_LabelOnlyInFencedBlock_DoesNotMatch(t *testing.T) {
	content := "# Title\n\n```\n**Status:** Draft\n```\n\n## Context\n\nBody.\n"
	if got := statusMarker(content); got != "" {
		t.Errorf(
			"statusMarker(%q) = %q, want \"\" (a fenced example is not a real marker)",
			content,
			got,
		)
	}
}

func TestStatusMarker_ProseStartingWithStatus_DoesNotMatch(t *testing.T) {
	// Modeled on a docs/plans/cycles/ file: a paragraph that starts
	// "**Status: an option to evaluate..." as prose, not a marker line —
	// and it sits below the header block besides.
	content := "# Cycle\n\n**Date:** 2026-07-29\n**Status:** Done\n\n## 1. Domain Context\n\n" +
		"Some analysis.\n\n**Status: an option to evaluate, not an approved direction.** " +
		"Nothing here is committed.\n"
	if got := statusMarker(content); got != "Done" {
		t.Errorf(
			"statusMarker(%q) = %q, want %q (real header-block status, not the prose paragraph)",
			content,
			got,
			"Done",
		)
	}
}

func TestStatusMarker_WrappedValue_ReturnsFirstLineOnly(t *testing.T) {
	content := "# Title\n\n**Status:** Draft and\nstill evolving.\n\n## Context\n"
	if got := statusMarker(content); got != "Draft and" {
		t.Errorf(
			"statusMarker(%q) = %q, want %q (must not join the wrapped line)",
			content,
			got,
			"Draft and",
		)
	}
}

func TestStatusMarker_NoneOfTheShapes_ReturnsEmpty(t *testing.T) {
	content := "# Title\n\nJust a document with no status marker of any kind.\n\n## Context\n\nBody text.\n"
	if got := statusMarker(content); got != "" {
		t.Errorf("statusMarker(%q) = %q, want \"\"", content, got)
	}
}
