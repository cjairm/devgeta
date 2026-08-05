// Branch-name encoding for journal filenames (ADR-0012 §5).
//
// The filename must be derived from the branch name reversibly and without
// collisions: a lossy slug (slash-to-hyphen) maps "fix/a-b" and "fix/a/b" to
// the same file, silently merging two branches' review memory. Percent-encoding
// keeps [A-Za-z0-9._-] and encodes every other byte (including '/' and '%'
// itself), so distinct branches always get distinct files, the branch name is
// recoverable for listings, and the encoded form can never contain a path
// separator — a hostile branch name ("../../x") cannot escape the review
// directory.

package reviewjournal

import (
	"fmt"
	"strings"
)

// isPlain reports whether b needs no encoding. '%' is deliberately NOT plain:
// it is the escape character, so a literal '%' in a branch name must itself be
// encoded for DecodeBranch to be unambiguous.
func isPlain(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '.', b == '_', b == '-':
		return true
	}
	return false
}

// EncodeBranch returns the collision-free filename form of a branch name
// (without the ".md" suffix).
func EncodeBranch(branch string) string {
	var sb strings.Builder
	for i := 0; i < len(branch); i++ {
		b := branch[i]
		if isPlain(b) {
			sb.WriteByte(b)
			continue
		}
		fmt.Fprintf(&sb, "%%%02X", b)
	}
	return sb.String()
}

// DecodeBranch reverses EncodeBranch, for listing journals by their original
// branch names (e.g. prune output).
func DecodeBranch(encoded string) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(encoded); i++ {
		b := encoded[i]
		if b != '%' {
			if !isPlain(b) {
				return "", fmt.Errorf("invalid journal filename %q: unencoded byte %q", encoded, b)
			}
			sb.WriteByte(b)
			continue
		}
		if i+2 >= len(encoded) {
			return "", fmt.Errorf("invalid journal filename %q: truncated escape", encoded)
		}
		var v byte
		if _, err := fmt.Sscanf(encoded[i+1:i+3], "%02X", &v); err != nil {
			return "", fmt.Errorf(
				"invalid journal filename %q: bad escape %q",
				encoded,
				encoded[i:i+3],
			)
		}
		sb.WriteByte(v)
		i += 2
	}
	return sb.String(), nil
}
