package contextreport

import (
	"path/filepath"
	"regexp"
	"strings"
)

// frontmatterBytes returns the byte length of content's YAML frontmatter
// block (the "---\n...\n---\n" prefix, delimiters included), or 0 if there
// is none. Skills and commands both count only this — the body loads on
// invocation, not at session start (cycle doc Step 7: "a skill's body
// loads on invocation; only its frontmatter description sits in base
// context", confirmed by Step 0 against upstream docs; commands are
// documented to work the same way as skills).
func frontmatterBytes(content string) int {
	if !strings.HasPrefix(content, "---\n") && content != "---" {
		return 0
	}
	rest := content[len("---\n"):]
	closeIdx := strings.Index(rest, "\n---")
	if closeIdx == -1 {
		return 0
	}
	// Include the closing delimiter line itself ("\n---") plus its own
	// trailing newline if present.
	end := len("---\n") + closeIdx + len("\n---")
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return end
}

// frontmatterHasPathsKey reports whether content's frontmatter block
// contains a top-level "paths:" key — the signal that a Claude Code
// project rule is path-scoped and therefore loads on demand (when a
// matching file is read), not at launch (cycle doc Step 7's Project rules
// row excludes these).
func frontmatterHasPathsKey(content string) bool {
	n := frontmatterBytes(content)
	if n == 0 {
		return false
	}
	block := content[:n]
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "paths:") {
			return true
		}
	}
	return false
}

// importRef matches an "@path" reference: '@' followed by any run of
// non-whitespace, non-backtick characters.
var importRef = regexp.MustCompile("@([^\\s`]+)")

// maskInlineCode blanks out backtick-delimited spans in one line, so an
// import reference escaped as `@README` (per Claude Code's own docs: "wrap
// it in backticks to keep the text literal") is never matched.
func maskInlineCode(line string) string {
	var sb strings.Builder
	inCode := false
	for _, r := range line {
		if r == '`' {
			inCode = !inCode
			sb.WriteRune(' ')
			continue
		}
		if inCode {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// extractImports returns every "@path" import reference in content, in
// order, skipping fenced code blocks (``` ... ```) entirely and masking
// inline code spans on every other line (guide-equivalent rule: "Import
// parsing skips Markdown code spans and fenced code blocks").
func extractImports(content string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		masked := maskInlineCode(line)
		for _, m := range importRef.FindAllStringSubmatch(masked, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// resolveImportPath resolves an "@path" reference to an absolute path.
// Relative paths resolve against fromDir — the directory of the file
// containing the import, not the working directory (Claude Code's own
// documented rule) — and a "~/" prefix expands against home.
func resolveImportPath(ref, fromDir, home string) string {
	if strings.HasPrefix(ref, "~/") {
		return filepath.Join(home, ref[len("~/"):])
	}
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(fromDir, ref)
}
