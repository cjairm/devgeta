// OpenCode base-context discovery (cycle doc Step 7). Simpler than
// Claude's: no @import mechanism, no auto-memory, no per-file lazy
// frontmatter distinction verified for anything here.

package contextreport

import (
	"io/fs"
	"path/filepath"
	"strconv"
)

// DiscoverOpenCode measures OpenCode's base context for repoDir, resolving
// user-level paths under home.
func DiscoverOpenCode(repoDir, home string) *Report {
	return &Report{
		Agent: "opencode",
		Rows: []Row{
			openCodeMemoryRow(repoDir, home),
			openCodeSettingsRow(repoDir, home),
			openCodePluginsRow(home),
		},
	}
}

// openCodeMemoryRow covers ~/.config/opencode/AGENTS.md and
// <repo>/AGENTS.md. Whether the global file loads at all when a project
// one also exists is disputed — OpenCode's own docs describe the two as
// merging, but an open upstream issue (anomalyco/opencode#22020, still open
// as of Step 0's research) reports the global file being silently skipped
// when a project one exists. This reports what exists on disk rather than
// assuming either behavior, and says so explicitly when both are present.
func openCodeMemoryRow(repoDir, home string) Row {
	globalPath := filepath.Join(home, ".config", "opencode", "AGENTS.md")
	projectPath := filepath.Join(repoDir, "AGENTS.md")
	items := existingFileItems([]string{globalPath, projectPath})

	note := "global and project AGENTS.md, concatenated per OpenCode's own docs"
	if len(items) == 2 {
		note = "both a global and a project AGENTS.md exist; OpenCode's docs say they merge, but an open " +
			"upstream issue (anomalyco/opencode#22020) reports the global file being skipped when a " +
			"project one exists — this total assumes both load; treat it as an upper bound until that's resolved"
	}
	return Row{Layer: "Memory (AGENTS.md)", Items: items, Note: note}
}

// openCodeSettingsRow covers opencode.json, whole file, both layers.
func openCodeSettingsRow(repoDir, home string) Row {
	candidates := []string{
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(repoDir, "opencode.json"),
	}
	return Row{
		Layer: "Settings (opencode.json)",
		Items: existingFileItems(candidates),
		Note:  "whole file per layer present",
	}
}

// openCodePluginsRow is informational only — same reasoning as Claude's
// plugins row: on-disk size is not verified to equal what actually loads
// into context, so it is reported but not added to the total.
func openCodePluginsRow(home string) Row {
	base := filepath.Join(home, ".config", "opencode", "plugin")
	count := 0
	totalOnDisk := 0
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		count++
		if info, statErr := d.Info(); statErr == nil {
			totalOnDisk += int(info.Size())
		}
		return nil
	})
	note := "informational only, not counted in the total: on-disk size, not a verified base-context cost"
	if count > 0 {
		note += " — " + strconv.Itoa(
			count,
		) + " files, " + strconv.Itoa(
			totalOnDisk,
		) + " bytes on disk"
	}
	return Row{Layer: "Plugins (informational)", Note: note}
}
