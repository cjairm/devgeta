# ADR-0028 — Status recognition is a shape, not a vocabulary

**Date:** 2026-08-18
**Status:** ACCEPTED

## Context

The `finish-work` cycle needs a document-approval sweep: `worktree-finish --check`
(a later task) walks the markdown files a branch changed and reports each one's
status marker so the command can flag docs that still look unapproved. The
sweep runs from a shipped binary, so per CLAUDE.md principle 8 and §12
("anything we ship is built for strangers") it must work in any repo the
binary is installed into — not just devgeta's own.

Devgeta's own ADRs and cycle docs spell a status as `**Status:** ACCEPTED` in
the header block. That is one Markdown rendering of a status label, not the
only one, and it happens to be devgeta's own convention:

- **MADR 3.x** (the current Markdown ADR standard) puts the status in a leading
  YAML front-matter block: `---\nstatus: accepted\n---`.
- **MADR 2.x** writes it as a list item: `* Status: accepted`.
- **Nygard's original ADR template** — the most widely copied ADR form there
  is — carries no inline label at all: only a `## Status` heading whose body
  is the value.

A recognizer that only compiles in the literal `**Status:**` rendering would
make the sweep's headline feature a no-op in most repos it ships to. That is
exactly the violation principle 8 and §12 forbid: a "general" feature that in
practice only serves this one repository's spelling.

The recognizer also has to avoid the opposite failure — treating any
status-shaped text anywhere in a file as the document's status. Real files in
this repo already contain status-shaped lines that are NOT the file's own
status marker:

- `configs/shared/agents/code-reviewer.md` has `**Status:** APPROVE | REQUEST
CHANGES | NEEDS DISCUSSION` inside a report template the agent prints to
  the user — not a status of that file.
- `docs/decisions/ADR-0014-agent-config-protection-is-a-guard-not-a-path-deny.md`
  has a second `**Status:** ACCEPTED` inside an Amendment section, well below
  its real header-block status.
- `docs/decisions/README.md` has a `## Status` heading directly over a status
  _legend_ table, not a status value.
- `ROADMAP.md` has a `## Status Legend` heading that is not a `## Status`
  section at all.
- A `docs/plans/cycles/` file can contain a paragraph that starts
  `**Status: an option to evaluate...` as prose, not a marker line.

A whole-file, best-effort scan for anything status-shaped would misidentify
several of these as the document's status. The recognizer needs an explicit
boundary rule, not just a pattern to match.

## Decision

Recognize a small, fixed set of status marker **shapes** — never a vocabulary
of literal strings to try one at a time — and treat anything that matches none
of them as "no marker" rather than guessing or inventing a fourth shape:

1. **YAML front matter** — a `status` key (case-insensitive, top-level,
   scalar-only) inside a leading `---`/`---` block, parsed with
   `gopkg.in/yaml.v3` rather than hand-rolled scanning.
2. **A header-block label line** — one shape-matching rule that accepts an
   optional leading list marker, optional emphasis around the word `status` in
   any of the four common placements (`**Status:**`, `**Status**:`, `Status:`,
   `_Status_:`, etc.), a colon, and the trimmed remainder as the value. This is
   scoped to the lines above the document's first `##`-or-deeper heading — the
   "header block" — specifically to exclude the code-reviewer template line,
   ADR-0014's amendment-section repeat, and the cycle-doc prose paragraph
   above, all of which sit below that boundary.
3. **A `Status` section** at any heading level, matched by exact heading text
   (after stripping emphasis and trailing punctuation) — `## Status Legend`
   does not match `## Status` — whose value is the section's first non-blank
   line, and only when that line is plain text: not a table row, not a list
   item, not a fenced-code-block opener, and not another heading. This
   excludes `docs/decisions/README.md`'s `## Status` legend table.

Fenced code blocks (` ``` ` or `~~~`) are skipped on every scan, in every
shape, since example `**Status:**` lines are quoted inside fences elsewhere in
this repo. The value returned is always one line, trimmed, never joined across
a wrap.

The values themselves are never judged against a vocabulary — `statusMarker`
returns whatever text follows the marker, verbatim, and a later task's prose
layer decides what counts as "approved."

### Alternatives considered

**(a) Keep the literal `**Status:**` rule, document the supported format as
devgeta's own.** Rejected: this makes the sweep's headline feature a no-op in
most of the repos this binary ships to (MADR 3.x, MADR 2.x, and Nygard-style
repos alike), which principle 8 and §12 forbid.

**(b) Drop status parsing from Go entirely — emit only the changed markdown
paths, and let the command's prose layer read each file's status.** Rejected
as the mechanism for the normal case: a separate, already-settled design
decision ("doc-status is data, not a verdict") requires Go to emit a
machine-readable `doc-status: <path>: <value>` line, verbatim, without judging
finality — dropping parsing entirely would leave `--check`'s output with
nothing to report. **(b) is kept as the fallback**, though: when none of the
three shapes above match, `statusMarker` reports no marker, and the later
`--check` command's prose layer may still try to read a status from whatever
template it finds by other means. That fallback path is not implemented by
this task — this ADR only records that it is the accepted answer to "what
happens when the recognizer's three shapes don't match."

## Consequences

- The sweep works in any Markdown ADR/cycle-doc convention a stranger's repo
  already uses, not just devgeta's own — satisfying §12 for a shipped feature.
- Adding a fourth real-world shape later is a deliberate ADR-worthy decision,
  not a quiet regex tweak, because the whole point of "shape, not vocabulary"
  is that the set of recognized shapes is closed and reviewed.
- The header-block boundary and the Status-section exact-match rule are each a
  little more code than a naive whole-file grep, but each is load-bearing:
  removing either one reintroduces one of the real false positives named
  above.
- Nothing in `statusMarker` reads git, the filesystem, or judges whether a
  status value counts as "approved" — that stays in a later, separate layer,
  so this function can be tested with plain Go string constants and no
  fixtures.
