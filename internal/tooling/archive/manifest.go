// The checksum manifest uses the sha256sum text format so a restore can be
// verified with `shasum -a 256 -c` without devgeta. A path containing a
// backslash or a newline is written GNU-escaped: the line starts with `\`,
// and within the path `\\` stands for a literal backslash and `\n` for a
// literal newline (cycle doc 2026-09-13-dg-archive.md §2).
package archive

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ManifestEntry is one archived file's path (forward-slash separated,
// relative to the archive root) and its SHA-256 hash, hex-encoded.
type ManifestEntry struct {
	Hash string
	Path string
}

// WriteManifest writes entries in sha256sum text format, one per line.
func WriteManifest(w io.Writer, entries []ManifestEntry) error {
	for _, e := range entries {
		line, escaped := encodeManifestPath(e.Path)
		prefix := ""
		if escaped {
			prefix = `\`
		}
		if _, err := fmt.Fprintf(w, "%s%s  %s\n", prefix, e.Hash, line); err != nil {
			return err
		}
	}
	return nil
}

// ReadManifest parses sha256sum text-format lines written by WriteManifest.
func ReadManifest(r io.Reader) ([]ManifestEntry, error) {
	var entries []ManifestEntry
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		escaped := false
		if strings.HasPrefix(line, `\`) {
			escaped = true
			line = line[1:]
		}
		sep := strings.Index(line, "  ")
		if sep < 0 {
			return nil, fmt.Errorf("invalid manifest line: %q", line)
		}
		path := line[sep+2:]
		if escaped {
			path = decodeManifestPath(path)
		}
		entries = append(entries, ManifestEntry{Hash: line[:sep], Path: path})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func encodeManifestPath(path string) (encoded string, escaped bool) {
	if !strings.ContainsAny(path, "\\\n") {
		return path, false
	}
	var sb strings.Builder
	for _, r := range path {
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String(), true
}

func decodeManifestPath(encoded string) string {
	var sb strings.Builder
	for i := 0; i < len(encoded); i++ {
		if encoded[i] == '\\' && i+1 < len(encoded) {
			switch encoded[i+1] {
			case '\\':
				sb.WriteByte('\\')
				i++
				continue
			case 'n':
				sb.WriteByte('\n')
				i++
				continue
			}
		}
		sb.WriteByte(encoded[i])
	}
	return sb.String()
}
