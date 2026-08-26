// Name encoding for per-branch filenames, extracted from
// reviewjournal.EncodeBranch/DecodeBranch (ADR-0012 §5) so both reviewjournal
// and handoff resolve filenames the same way.
//
// The filename must be derived from the name reversibly and without
// collisions: a lossy slug (slash-to-hyphen) maps "fix/a-b" and "fix/a/b" to
// the same file, silently merging two branches' state. Percent-encoding keeps
// [A-Za-z0-9._-] and encodes every other byte (including '/' and '%' itself),
// so distinct names always get distinct files, the name is recoverable for
// listings, and the encoded form can never contain a path separator — a
// hostile name ("../../x") cannot escape the store's directory.

package branchstore

import (
	"fmt"
	"strings"
)

// isPlain reports whether b needs no encoding. '%' is deliberately NOT plain:
// it is the escape character, so a literal '%' in a name must itself be
// encoded for DecodeName to be unambiguous.
func isPlain(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '.', b == '_', b == '-':
		return true
	}
	return false
}

// EncodeName returns the collision-free filename form of name (without any
// extension).
func EncodeName(name string) string {
	var sb strings.Builder
	for i := 0; i < len(name); i++ {
		b := name[i]
		if isPlain(b) {
			sb.WriteByte(b)
			continue
		}
		fmt.Fprintf(&sb, "%%%02X", b)
	}
	return sb.String()
}

// DecodeName reverses EncodeName, for listing stored files by their original
// names.
func DecodeName(encoded string) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(encoded); i++ {
		b := encoded[i]
		if b != '%' {
			if !isPlain(b) {
				return "", fmt.Errorf("invalid stored filename %q: unencoded byte %q", encoded, b)
			}
			sb.WriteByte(b)
			continue
		}
		if i+2 >= len(encoded) {
			return "", fmt.Errorf("invalid stored filename %q: truncated escape", encoded)
		}
		var v byte
		if _, err := fmt.Sscanf(encoded[i+1:i+3], "%02X", &v); err != nil {
			return "", fmt.Errorf(
				"invalid stored filename %q: bad escape %q",
				encoded,
				encoded[i:i+3],
			)
		}
		sb.WriteByte(v)
		i += 2
	}
	return sb.String(), nil
}
