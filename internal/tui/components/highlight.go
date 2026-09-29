package tuicomponents

import (
	"strings"

	lip "charm.land/lipgloss/v2"
)

// HighlightMatch renders text with the first case-insensitive occurrence of
// query in the Match style and the rest in base, so a filtered list shows
// why each row is still there. An empty query, or one text doesn't contain,
// renders text in base alone. Case folding is ASCII-only on purpose: the
// match is located by byte offset, and only ASCII folding keeps the folded
// string's offsets identical to text's.
func (p *Palette) HighlightMatch(text, query string, base lip.Style) string {
	if query == "" {
		return base.Render(text)
	}
	i := strings.Index(asciiLower(text), asciiLower(query))
	if i < 0 {
		return base.Render(text)
	}
	j := i + len(query)
	var sb strings.Builder
	if i > 0 {
		sb.WriteString(base.Render(text[:i]))
	}
	sb.WriteString(p.Match.Render(text[i:j]))
	if j < len(text) {
		sb.WriteString(base.Render(text[j:]))
	}
	return sb.String()
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
