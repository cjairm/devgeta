package tuiworktree

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/cjairm/devgeta/internal/tooling/task"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

// fileHeaderPrefix marks lines produced by rewriteFileHeaders. Diff body
// lines always start with '+', '-', ' ', '@', or '\', so the prefix cannot
// collide with pass-through content.
const fileHeaderPrefix = "─── "

// preamblePrefixes are the git metadata lines that follow a "diff --git"
// line and get collapsed into the single styled header.
var preamblePrefixes = []string{
	"index ",
	"old mode",
	"new mode",
	"deleted file mode",
	"new file mode",
	"copy from ",
	"copy to ",
	"rename from ",
	"rename to ",
	"similarity index ",
	"dissimilarity index ",
	"--- ",
	"+++ ",
}

func isPreambleLine(plain string) bool {
	for _, p := range preamblePrefixes {
		if strings.HasPrefix(plain, p) {
			return true
		}
	}
	return false
}

// parseDiffGitPaths extracts the old and new paths from a
// "diff --git a/OLD b/NEW" line. Paths containing " b/" are ambiguous in
// this format; the ---/+++ and rename lines refine the result afterwards.
func parseDiffGitPaths(plain string) (oldPath, newPath string) {
	rest := strings.TrimPrefix(plain, "diff --git ")
	i := strings.Index(rest, " b/")
	if i < 0 {
		return "", ""
	}
	return strings.TrimPrefix(rest[:i], "a/"), rest[i+3:]
}

// rewriteFileHeaders replaces each raw git file preamble (diff --git, index,
// mode/rename/similarity lines, ---/+++) with one styled header line:
// "─── path +a -r", renames as "old → new", plus a "(new)"/"(deleted)" tag.
// Hunks, "Binary files ... differ" notices, exclusion notes, and the
// untracked list pass through untouched. The input keeps git's ANSI colors,
// so every structural check runs on the stripped line.
func rewriteFileHeaders(
	content string,
	stats []task.FileStat,
	p *tuicomponents.Palette,
) string {
	statsByPath := make(map[string]task.FileStat, len(stats))
	for _, s := range stats {
		statsByPath[s.Path] = s
	}

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		plain := ansi.Strip(lines[i])
		if !strings.HasPrefix(plain, "diff --git ") {
			out = append(out, lines[i])
			i++
			continue
		}

		oldPath, newPath := parseDiffGitPaths(plain)
		isNew, isDeleted := false, false
		j := i + 1
		for ; j < len(lines); j++ {
			meta := ansi.Strip(lines[j])
			if !isPreambleLine(meta) {
				break
			}
			switch {
			case strings.HasPrefix(meta, "rename from "):
				oldPath = strings.TrimPrefix(meta, "rename from ")
			case strings.HasPrefix(meta, "rename to "):
				newPath = strings.TrimPrefix(meta, "rename to ")
			case strings.HasPrefix(meta, "new file mode"):
				isNew = true
			case strings.HasPrefix(meta, "deleted file mode"):
				isDeleted = true
			case strings.HasPrefix(meta, "--- a/"):
				oldPath = strings.TrimPrefix(meta, "--- a/")
			case strings.HasPrefix(meta, "+++ b/"):
				newPath = strings.TrimPrefix(meta, "+++ b/")
			}
		}

		label := newPath
		lookup := newPath
		switch {
		case isDeleted:
			label = oldPath
			lookup = oldPath
		case oldPath != "" && newPath != "" && oldPath != newPath:
			label = oldPath + " → " + newPath
		}

		header := p.Divider.Render(fileHeaderPrefix) + p.DiffFileHeader.Render(label)
		switch {
		case isNew:
			header += " " + p.Inactive.Render("(new)")
		case isDeleted:
			header += " " + p.Inactive.Render("(deleted)")
		}
		if s, ok := statsByPath[lookup]; ok && !s.Binary {
			header += " " + p.DiffStat(s.Added, s.Removed)
		}

		// Blank line between files so headers are scannable.
		if len(out) > 0 && strings.TrimSpace(ansi.Strip(out[len(out)-1])) != "" {
			out = append(out, "")
		}
		out = append(out, header)
		i = j
	}
	return strings.Join(out, "\n")
}

// diffFileHeaderLines returns the line indexes of rewriteFileHeaders output,
// for [ / ] jumps between files in the focused diff pane.
func diffFileHeaderLines(content string) []int {
	var out []int
	for i, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(ansi.Strip(line), fileHeaderPrefix) {
			out = append(out, i)
		}
	}
	return out
}
