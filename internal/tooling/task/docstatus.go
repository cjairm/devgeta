package task

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// headingRe matches a Markdown ATX heading of any level (1-6). Group 1 is the
// hashes, group 2 is the trimmed heading text.
var headingRe = regexp.MustCompile(`^\s{0,3}(#{1,6})\s+(.*?)\s*$`)

// fenceRe matches a fenced-code-block delimiter line (``` or ~~~, 3+ chars).
// The same regex opens and closes a fence: statusMarker tracks fence state by
// toggling a bool each time a line matches, rather than distinguishing open
// from close, because Markdown itself doesn't distinguish them either.
var fenceRe = regexp.MustCompile("^\\s{0,3}(`{3,}|~{3,})")

// listItemRe matches a Markdown list item marker: a bullet (-, *, +) or a
// numbered marker (1. / 1)), each followed by whitespace.
var listItemRe = regexp.MustCompile(`^\s*(?:[-*+]\s+|\d+[.)]\s+)`)

// emphasisMarkers are the Markdown emphasis delimiters statusMarker's
// header-block label parser recognizes, longest first so "**" is tried
// before "*" would otherwise match its first character.
var emphasisMarkers = []string{"**", "__", "*", "_"}

// statusMarker extracts a document's status marker value, verbatim and
// untrimmed of meaning (never judged against a vocabulary — see ADR-0028).
// Returns "" when none of the three recognized shapes match.
func statusMarker(content string) string {
	lines := strings.Split(content, "\n")

	if v, ok := frontMatterStatus(lines); ok {
		return oneLine(v)
	}
	if v, ok := headerBlockStatus(lines); ok {
		return oneLine(v)
	}
	if v, ok := statusSectionValue(lines); ok {
		return oneLine(v)
	}
	return ""
}

// oneLine is the single guard that every shape's return path goes through,
// so all three share the same one-line guarantee `--check`'s report (and
// finish-work.md's parsing of it) depends on. Shapes 2 and 3 already produce
// a single line on their own, but shape 1 (YAML front-matter) does not: a
// block scalar (`status: |` or `status: >`) parses to a multi-line
// val.Value, which would otherwise leak embedded newlines into the report.
func oneLine(v string) string {
	if i := strings.IndexByte(v, '\n'); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// frontMatterStatus recognizes shape 1: a top-level, scalar-only `status` key
// inside a leading YAML front-matter block delimited by `---` on line 1 and
// the `---` that closes it. A mapping or list value under `status` falls
// through (shape 1 does not match) so the caller tries shape 2 next.
func frontMatterStatus(lines []string) (string, bool) {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", false
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return "", false
	}

	body := strings.Join(lines[1:end], "\n")

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return "", false
	}
	if len(doc.Content) == 0 {
		return "", false
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return "", false
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		val := mapping.Content[i+1]
		if !strings.EqualFold(key.Value, "status") {
			continue
		}
		if val.Kind != yaml.ScalarNode {
			return "", false
		}
		return val.Value, true
	}
	return "", false
}

// headerBlockStatus recognizes shape 2: a status label line inside the
// document's header block — the lines above its first `##`-or-deeper heading.
// Scoping to that block is load-bearing: real files in this repo carry a
// status-shaped line BELOW their header block that is not the document's own
// status (a report template, an amendment section, prose that starts with
// "Status:") — see ADR-0028 for the three named examples this guards against.
func headerBlockStatus(lines []string) (string, bool) {
	for _, line := range headerBlockLines(lines) {
		if v, ok := parseStatusLabelLine(line); ok {
			return v, true
		}
	}
	return "", false
}

// headerBlockLines returns the lines above the document's first
// `##`-or-deeper heading, skipping any line inside a fenced code block
// (fenced content is never a real heading or a real label line).
func headerBlockLines(lines []string) []string {
	inFence := false
	var block []string
	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil && len(m[1]) >= 2 {
			break
		}
		block = append(block, line)
	}
	return block
}

// parseStatusLabelLine matches shape 2's label-line SHAPE as one rule rather
// than a list of literal renderings: an optional leading list marker, then an
// optional emphasis marker (singly or doubly, `*`/`_`) that may wrap either
// just the word "status" or "status:" together, then a colon, then the
// value. This one rule accepts `**Status:**`, `**Status**:`, `Status:`,
// `* Status:`, and `_Status_:` alike.
func parseStatusLabelLine(line string) (string, bool) {
	s := strings.TrimLeft(line, " \t")

	// Strip an optional leading list marker ("-", "*", or "+" followed by
	// whitespace). Checking for trailing whitespace after a single marker
	// character is what keeps this from misfiring on "**Status:**", whose
	// second "*" has no space after it.
	if len(s) > 1 && (s[0] == '-' || s[0] == '*' || s[0] == '+') && (s[1] == ' ' || s[1] == '\t') {
		s = strings.TrimLeft(s[1:], " \t")
	}

	lower := strings.ToLower(s)
	emph := ""
	for _, m := range emphasisMarkers {
		if strings.HasPrefix(s, m) {
			emph = m
			break
		}
	}

	rest := s[len(emph):]
	restLower := lower[len(emph):]
	if !strings.HasPrefix(restLower, "status") {
		return "", false
	}
	rest = rest[len("status"):]

	switch {
	case emph != "" && strings.HasPrefix(rest, ":"+emph):
		// "**Status:**" — the colon sits inside the emphasis markers.
		rest = rest[len(":"+emph):]
	case emph != "" && strings.HasPrefix(rest, emph+":"):
		// "**Status**:" — the colon sits outside the closing emphasis.
		rest = rest[len(emph+":"):]
	case emph == "" && strings.HasPrefix(rest, ":"):
		rest = rest[1:]
	default:
		return "", false
	}

	return strings.TrimSpace(rest), true
}

// statusSectionValue recognizes shape 3: a `Status` section at any heading
// level, matched by exact heading text (after stripping emphasis and
// trailing punctuation) rather than a prefix match — "## Status Legend" must
// not match. The value is the section's first non-blank line, and only when
// that line is plain text.
func statusSectionValue(lines []string) (string, bool) {
	inFence := false
	for i, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := headingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if !strings.EqualFold(normalizeHeadingText(m[2]), "status") {
			continue
		}
		return firstPlainLineAfter(lines, i)
	}
	return "", false
}

// normalizeHeadingText strips Markdown emphasis markers and trailing
// punctuation from a heading's text so "## **Status**" and "## Status:" are
// both recognized as exactly "Status", while "## Status Legend" is not.
func normalizeHeadingText(text string) string {
	stripped := strings.NewReplacer("*", "", "_", "").Replace(text)
	stripped = strings.TrimSpace(stripped)
	stripped = strings.TrimRight(stripped, ":.!?")
	return strings.TrimSpace(stripped)
}

// firstPlainLineAfter returns the first non-blank line after lines[headingIdx],
// but only if that line is plain text: not a table row, not a list item, not
// a fenced-code-block opener, and not another heading. Any of those disqualify
// the whole section — e.g. docs/decisions/README.md's "## Status" heading
// sits directly over a legend TABLE, not a status value, so it must yield no
// marker rather than skipping ahead to a later plain line.
func firstPlainLineAfter(lines []string, headingIdx int) (string, bool) {
	for j := headingIdx + 1; j < len(lines); j++ {
		line := lines[j]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "|"):
			return "", false
		case listItemRe.MatchString(line):
			return "", false
		case fenceRe.MatchString(line):
			return "", false
		case headingRe.MatchString(line):
			return "", false
		default:
			return trimmed, true
		}
	}
	return "", false
}
