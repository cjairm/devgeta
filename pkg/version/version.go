// Package version compares the dotted numeric versions external tools print,
// such as "0.11.1" or "2.2.6".
package version

import (
	"strconv"
	"strings"
)

// AtLeast reports whether current is equal to or newer than required.
//
// Parts are compared left to right and the first difference decides, so
// "2.0.0" is newer than "1.9.0". A missing part counts as 0 ("2.2" equals
// "2.2.0"). A leading "v" and anything after a "-" or "+" (a prerelease or
// build suffix) are ignored. A part that is not a number makes the comparison
// fail closed: AtLeast returns false, so a caller that installs on false
// installs rather than trusting an unreadable version.
func AtLeast(current, required string) bool {
	cur, ok := parse(current)
	if !ok {
		return false
	}
	req, ok := parse(required)
	if !ok {
		return false
	}
	for i := 0; i < max(len(cur), len(req)); i++ {
		c, r := part(cur, i), part(req, i)
		if c != r {
			return c > r
		}
	}
	return true
}

// Find returns the first whitespace-separated token in output that reads as a
// version, such as "v2.2.6" in "opencode v2.2.6" or "2.1.319" in
// "2.1.319 (Claude Code)", and false when there is none.
func Find(output string) (string, bool) {
	for _, field := range strings.Fields(output) {
		if parts, ok := parse(field); ok && len(parts) > 1 {
			return field, true
		}
	}
	return "", false
}

func parse(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, false
	}
	fields := strings.Split(v, ".")
	parts := make([]int, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil, false
		}
		parts[i] = n
	}
	return parts, true
}

func part(parts []int, i int) int {
	if i < len(parts) {
		return parts[i]
	}
	return 0
}
