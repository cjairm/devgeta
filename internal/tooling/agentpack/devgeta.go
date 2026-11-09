package agentpack

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/cjairm/devgeta/pkg/files"
)

// instructionsFile is the file the devgeta pack carries and every coder loads.
const instructionsFile = "DEVGETA.md"

// instructionsImport is the line devgeta adds to ~/.claude/CLAUDE.md. Claude
// Code resolves @path imports relative to the file that holds them.
const instructionsImport = "@DEVGETA.md"

// importPattern matches an @-import of DEVGETA.md anywhere on a line, because
// Claude Code reads `@path` inline ("See @DEVGETA.md for style"), not only on
// a line of its own. The mention must stand alone: "@DEVGETA.md.bak" and
// "email@DEVGETA.md" are not imports. Matching too much is the safe error
// here, since it only means devgeta does not add a duplicate.
var importPattern = regexp.MustCompile(
	`(?:^|[\s(])@(?:\./|~/\.claude/)?DEVGETA\.md(?:$|\s|[,;:)!?]|\.(?:\s|$))`,
)

// importsDevgeta reports whether a CLAUDE.md line already imports DEVGETA.md.
func importsDevgeta(line string) bool { return importPattern.MatchString(line) }

// hasImport reports whether any line of text imports DEVGETA.md.
func hasImport(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if importsDevgeta(line) {
			return true
		}
	}
	return false
}

// addImportLine appends the @DEVGETA.md line to the CLAUDE.md at path unless a
// line already imports the file, and reports whether it added one. The user's
// text and file mode are kept as they are; a missing file is created.
func addImportLine(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("could not read %s: %w", path, err)
	}
	text := string(data)
	if hasImport(text) {
		return false, nil
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += instructionsImport + "\n"
	if err := files.WriteFileAtomic(path, []byte(text), mode); err != nil {
		return false, fmt.Errorf("could not add %s to %s: %w", instructionsImport, path, err)
	}
	return true, nil
}

// removeImportLine deletes the first line of the CLAUDE.md at path that is
// exactly devgeta's @DEVGETA.md line, and reports whether it did. Anything else
// that mentions the file, such as the user's edited version of the line, is
// left (ADR-0070 §3). A missing file is not an error.
func removeImportLine(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("could not read %s: %w", path, err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.TrimSuffix(line, "\r") != instructionsImport {
			continue
		}
		mode := os.FileMode(0o644)
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
		rest := strings.Join(append(lines[:i:i], lines[i+1:]...), "\n")
		if err := files.WriteFileAtomic(path, []byte(rest), mode); err != nil {
			return false, fmt.Errorf("could not remove %s from %s: %w", instructionsImport, path, err)
		}
		return true, nil
	}
	return false, nil
}
