package opencode

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The permission and formatter tests in permissions_test.go compare Claude Code
// and OpenCode, the two coders that can express the same policy. Pi can't
// express most of it (it does not ask before tool calls), and the agent packs
// reach all three. These tests keep that map honest: docs/guides/agent-sync.md
// must name where every concern lives for every coder, and a coder with no
// equivalent must be listed under "Accepted differences" instead of being
// skipped silently (CLAUDE.md §12).

const agentSyncGuide = "../../../docs/guides/agent-sync.md"

var syncCoders = []string{"Claude Code", "OpenCode", "Pi"}

type concernRow struct {
	concern string
	cells   map[string]string // coder -> cell text
}

func readGuide(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(agentSyncGuide)
	if err != nil {
		t.Fatalf("failed to read the agent sync guide: %v", err)
	}
	return string(data)
}

// section returns the text under a "## <title>" heading, up to the next one.
func section(t *testing.T, guide, title string) string {
	t.Helper()
	start := strings.Index(guide, "\n## "+title)
	if start < 0 {
		t.Fatalf("agent-sync.md has no %q section", title)
	}
	rest := guide[start+1:]
	if end := strings.Index(rest[3:], "\n## "); end >= 0 {
		rest = rest[:end+3]
	}
	return rest
}

func tableCells(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func parseConcernTable(t *testing.T) (header []string, rows []concernRow) {
	t.Helper()
	for _, line := range strings.Split(section(t, readGuide(t), "Where each concern lives"), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := tableCells(line)
		if strings.HasPrefix(cells[0], "---") {
			continue
		}
		if header == nil {
			header = cells
			continue
		}
		row := concernRow{concern: cells[0], cells: map[string]string{}}
		for i, coder := range header[1:] {
			if i+1 < len(cells) {
				row.cells[coder] = cells[i+1]
			}
		}
		rows = append(rows, row)
	}
	return header, rows
}

var (
	backticked = regexp.MustCompile("`([^`]+)`")
	repoPath   = regexp.MustCompile(`^(configs|internal|docs)/\S+$`)
	// "- **Permissions: Pi has none.** ..." names a gap and the concern it is in.
	gapLead = regexp.MustCompile(`^- \*\*(.+?): (Claude Code|OpenCode|Pi) has none`)
)

func isGap(cell string) bool { return strings.HasPrefix(cell, "None") }

func TestAgentSyncGuideHasAColumnForEveryCoder(t *testing.T) {
	header, rows := parseConcernTable(t)

	want := append([]string{"Concern"}, syncCoders...)
	if strings.Join(header, "|") != strings.Join(want, "|") {
		t.Fatalf("the concern table's columns are %v, want %v", header, want)
	}
	if len(rows) == 0 {
		t.Fatal("the concern table has no rows")
	}
	for _, row := range rows {
		for _, coder := range syncCoders {
			if row.cells[coder] == "" {
				t.Errorf("%q has no entry for %s: name where it lives, or write None", row.concern, coder)
			}
		}
	}
}

func TestEveryConcernNamesRealFilesOrIsAnAcceptedDifference(t *testing.T) {
	_, rows := parseConcernTable(t)
	accepted := section(t, readGuide(t), "Accepted differences")

	for _, row := range rows {
		for _, coder := range syncCoders {
			cell := row.cells[coder]
			if isGap(cell) {
				want := "- **" + row.concern + ": " + coder + " has none"
				if !strings.Contains(accepted, want) {
					t.Errorf(
						"%s has no equivalent for %q, so \"Accepted differences\" must say why, "+
							"in a bullet that starts %q",
						coder, row.concern, want,
					)
				}
				continue
			}
			tokens := backticked.FindAllStringSubmatch(cell, -1)
			if len(tokens) == 0 {
				t.Errorf("%s / %q: %q names nothing in backticks", coder, row.concern, cell)
			}
			for _, tok := range tokens {
				if repoPath.MatchString(tok[1]) {
					if _, err := os.Stat(filepath.Join("..", "..", "..", tok[1])); err != nil {
						t.Errorf("%s / %q names %s, which does not exist", coder, row.concern, tok[1])
					}
				}
			}
		}
	}
}

// The other direction: a gap listed under "Accepted differences" must still be
// a gap in the table, or the entry has gone stale.
func TestAcceptedGapsAreStillGapsInTheTable(t *testing.T) {
	_, rows := parseConcernTable(t)
	gaps := map[string]bool{}
	for _, row := range rows {
		for _, coder := range syncCoders {
			if isGap(row.cells[coder]) {
				gaps[row.concern+"|"+coder] = true
			}
		}
	}

	for _, line := range strings.Split(section(t, readGuide(t), "Accepted differences"), "\n") {
		if m := gapLead.FindStringSubmatch(line); m != nil && !gaps[m[1]+"|"+m[2]] {
			t.Errorf("%q says %s has no %s, but the table lists something for it", line, m[2], m[1])
		}
	}
}

func TestAgentSyncGuideCoversEveryAgentPackDelivery(t *testing.T) {
	_, rows := parseConcernTable(t)
	found := map[string]bool{}
	for _, row := range rows {
		found[row.concern] = true
	}
	// The packs reach all three coders (ADR-0068), so none of these may be a gap.
	for _, concern := range []string{"Agent packs (skills, hooks, commands)", "Shared instructions (`DEVGETA.md`)"} {
		if !found[concern] {
			t.Errorf("the concern table must have a row %q", concern)
			continue
		}
		for _, row := range rows {
			if row.concern != concern {
				continue
			}
			for _, coder := range syncCoders {
				if isGap(row.cells[coder]) {
					t.Errorf("%q reaches %s through its own mechanism; it is not a gap", concern, coder)
				}
			}
		}
	}
}
