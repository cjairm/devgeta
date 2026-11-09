package task

import (
	"fmt"
	"strconv"
	"strings"
)

// fileChange is one file's row from a merged --numstat / --name-status pair.
// Status is left empty until merged with --name-status output. Binary files
// report Added/Removed as 0 (numstat uses "-" for both, per its format).
type fileChange struct {
	Path    string
	Status  string
	Added   int
	Removed int
	Binary  bool
}

// parseNumstat parses `git diff --numstat` output into fileChange entries.
// Status is left unset here; callers merge in --name-status separately.
func parseNumstat(raw string) ([]fileChange, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []fileChange
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("malformed numstat line: %q", line)
		}
		fc := fileChange{Path: parts[2]}
		if parts[0] == "-" && parts[1] == "-" {
			fc.Binary = true
		} else {
			added, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("malformed numstat added count in %q: %w", line, err)
			}
			removed, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("malformed numstat removed count in %q: %w", line, err)
			}
			fc.Added, fc.Removed = added, removed
		}
		out = append(out, fc)
	}
	return out, nil
}

// partitionExcluded splits changes into reviewable and excluded (default
// lockfile/generated-asset patterns), preserving each list's relative order.
func partitionExcluded(changes []fileChange) (reviewable, excluded []fileChange) {
	for _, c := range changes {
		if isExcludedPath(c.Path) {
			excluded = append(excluded, c)
		} else {
			reviewable = append(reviewable, c)
		}
	}
	return reviewable, excluded
}

// formatExclusionNotes renders the "excluded from this diff: ..." line
// listing every excluded file (binary files noted as such, others with their
// +added/-removed counts), with no leading or trailing newline. Returns ""
// when excluded is empty so callers can test the result instead of the input
// slice; callers own their surrounding whitespace.
func formatExclusionNotes(excluded []fileChange) string {
	if len(excluded) == 0 {
		return ""
	}
	return "excluded from this diff: " + formatFileNotes(excluded)
}

// formatFileNotes renders files as a comma-joined inline list, each with its
// +added/-removed counts (or "binary"). Used by the excluded-files note.
func formatFileNotes(files []fileChange) string {
	notes := make([]string, len(files))
	for i, f := range files {
		if f.Binary {
			notes[i] = fmt.Sprintf("%s (binary)", f.Path)
		} else {
			notes[i] = fmt.Sprintf("%s (+%d/-%d)", f.Path, f.Added, f.Removed)
		}
	}
	return strings.Join(notes, ", ")
}
