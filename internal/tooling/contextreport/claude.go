// Claude Code base-context discovery (cycle doc Step 7). Every path here
// was confirmed or corrected against upstream docs in Step 0 — see
// docs/plans/cycles/2026-08-25-token-and-context-efficiency.md's Step 7
// table for the source of each row.

package contextreport

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cjairm/devgeta/internal/apps/git"
)

// DiscoverClaude measures Claude Code's base context for repoDir, resolving
// user-level paths under home. gitApp resolves the common git directory for
// the auto-memory project slug (Step 0: "all worktrees ... share one auto
// memory directory", keyed off the main checkout, not the worktree).
func DiscoverClaude(repoDir, home string, gitApp *git.Git) *Report {
	return &Report{
		Agent: "claude",
		Rows: []Row{
			claudeMemoryRow(repoDir, home),
			claudeAutoMemoryRow(repoDir, home, gitApp),
			claudeProjectRulesRow(repoDir),
			claudeSettingsRow(repoDir, home),
			claudeFrontmatterOnlyRow("Skills", repoDir, home, "skills", "SKILL.md"),
			claudeCommandsRow(repoDir, home),
			claudeWholeFileRow(
				"Agents",
				repoDir,
				home,
				"agents",
				"whole file per agent — Step 0 did not verify whether agent bodies are lazy-loaded like skills; treat as an upper bound",
			),
			claudePluginsRow(home),
			claudeMCPRow(repoDir, home),
			claudeHooksRow(repoDir, home),
		},
	}
}

// claudeMemoryRow covers ~/.claude/CLAUDE.md, every CLAUDE.md/.claude/CLAUDE.md
// from the filesystem root down to repoDir, and CLAUDE.local.md at each of
// those levels — all concatenated (never overriding), with @imports
// followed transitively (Step 0-confirmed precedence).
func claudeMemoryRow(repoDir, home string) Row {
	visited := map[string]bool{}
	var items []Item

	userClaudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
	items = append(items, resolveImportsRecursive(userClaudeMD, 1, visited, home)...)

	for _, dir := range ancestorDirsRootToLeaf(repoDir) {
		items = append(
			items,
			resolveImportsRecursive(filepath.Join(dir, "CLAUDE.md"), 1, visited, home)...,
		)
		items = append(
			items,
			resolveImportsRecursive(
				filepath.Join(dir, ".claude", "CLAUDE.md"),
				1,
				visited,
				home,
			)...,
		)
		items = append(
			items,
			resolveImportsRecursive(filepath.Join(dir, "CLAUDE.local.md"), 1, visited, home)...,
		)
	}

	return Row{
		Layer: "Memory (CLAUDE.md)",
		Items: items,
		Note: "concatenated root-to-leaf (managed policy, user, project, local — never overriding); " +
			"@imports followed transitively up to 4 hops, cycles guarded",
	}
}

// claudeAutoMemoryRow covers MEMORY.md's own launch-time read cap: the
// first 200 lines or 25KB, whichever comes first (Step 0-confirmed).
func claudeAutoMemoryRow(repoDir, home string, gitApp *git.Git) Row {
	const layer = "Auto memory (MEMORY.md)"
	root, err := mainCheckoutRoot(repoDir, gitApp)
	if err != nil {
		return Row{Layer: layer, Note: "could not resolve the project slug: " + err.Error()}
	}
	memoryPath := filepath.Join(
		home,
		".claude",
		"projects",
		slugifyPath(root),
		"memory",
		"MEMORY.md",
	)
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		return Row{Layer: layer, Note: "none found at " + memoryPath}
	}
	capped, truncated := capByLinesOrBytes(data, 200, 25*1024)
	note := "loaded in full (under the 200-line/25KB cap)"
	if truncated {
		note = "capped at 200 lines or 25KB, whichever came first — the file on disk is larger"
	}
	return Row{Layer: layer, Items: []Item{{Path: memoryPath, Bytes: len(capped)}}, Note: note}
}

// claudeProjectRulesRow covers .claude/rules/*.md files WITHOUT a `paths:`
// frontmatter key — those load unconditionally at launch, the same
// priority as .claude/CLAUDE.md (Step 0-confirmed). A path-scoped rule
// loads only when a matching file is opened, so it is excluded here.
func claudeProjectRulesRow(repoDir string) Row {
	rulesDir := filepath.Join(repoDir, ".claude", "rules")
	var items []Item
	_ = filepath.WalkDir(rulesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if frontmatterHasPathsKey(string(data)) {
			return nil
		}
		items = append(items, Item{Path: path, Bytes: len(data)})
		return nil
	})
	return Row{
		Layer: "Project rules (.claude/rules/*.md)",
		Items: items,
		Note:  "excludes files carrying a `paths:` frontmatter key — those load on demand, not at launch",
	}
}

// claudeSettingsRow covers the three settings layers, whole file (they are
// parsed in full regardless of which keys matter).
func claudeSettingsRow(repoDir, home string) Row {
	candidates := []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(repoDir, ".claude", "settings.json"),
		filepath.Join(repoDir, ".claude", "settings.local.json"),
	}
	return Row{
		Layer: "Settings (settings.json)",
		Items: existingFileItems(candidates),
		Note:  "whole file per layer present; layers merge, not override, so each present layer is counted",
	}
}

// claudeFrontmatterOnlyRow builds a row for a home+repo pair of directories
// (skills, and — via claudeCommandsRow — commands) whose files are docs-
// confirmed to load only their frontmatter at launch, the body on
// invocation.
func claudeFrontmatterOnlyRow(label, repoDir, home, subdir, filename string) Row {
	var items []Item
	for _, base := range []string{
		filepath.Join(home, ".claude", subdir),
		filepath.Join(repoDir, ".claude", subdir),
	} {
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Base(path) != filename {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if n := frontmatterBytes(string(data)); n > 0 {
				items = append(items, Item{Path: path, Bytes: n})
			}
			return nil
		})
	}
	return Row{
		Layer: label + " (frontmatter only)",
		Items: items,
		Note:  "only the YAML frontmatter block counts; the body loads on invocation, not at session start",
	}
}

// claudeCommandsRow: any .md file under commands/ (not just a fixed
// filename, unlike skills' SKILL.md), frontmatter only — commands are
// documented to work the same way skills do.
func claudeCommandsRow(repoDir, home string) Row {
	var items []Item
	for _, base := range []string{
		filepath.Join(home, ".claude", "commands"),
		filepath.Join(repoDir, ".claude", "commands"),
	} {
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if n := frontmatterBytes(string(data)); n > 0 {
				items = append(items, Item{Path: path, Bytes: n})
			}
			return nil
		})
	}
	return Row{
		Layer: "Commands (frontmatter only)",
		Items: items,
		Note:  "commands are documented to work the same way skills do: only the frontmatter counts",
	}
}

// claudeWholeFileRow covers a home+repo directory tree counted in full,
// for a layer whose lazy-loading behavior (if any) Step 0 did not verify.
func claudeWholeFileRow(label, repoDir, home, subdir, note string) Row {
	var items []Item
	for _, base := range []string{
		filepath.Join(home, ".claude", subdir),
		filepath.Join(repoDir, ".claude", subdir),
	} {
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			items = append(items, Item{Path: path, Bytes: len(data)})
			return nil
		})
	}
	return Row{Layer: label, Items: items, Note: note}
}

// claudePluginsRow is informational only, not added to the report's total
// (bytes: 0 per item) — Step 0 did not verify how much of a plugin bundle's
// on-disk size actually reaches base context versus loading lazily like a
// top-level skill or command, so a raw directory size would overstate it,
// possibly by a wide margin.
func claudePluginsRow(home string) Row {
	base := filepath.Join(home, ".claude", "plugins")
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
	note := "informational only, not counted in the total: on-disk size overstates actual base-context " +
		"cost if plugin contents load lazily the way top-level skills and commands do (unverified)"
	if count > 0 {
		note += " — " + strconv.Itoa(
			count,
		) + " files, " + strconv.Itoa(
			totalOnDisk,
		) + " bytes on disk"
	}
	return Row{Layer: "Plugins (informational)", Note: note}
}

// claudeMCPRow counts .mcp.json (a real, additive cost) and notes the
// mcpServers key some settings layers may also carry — already counted
// under Settings, so not added again here.
func claudeMCPRow(repoDir, home string) Row {
	mcpPath := filepath.Join(repoDir, ".mcp.json")
	items := existingFileItems([]string{mcpPath})
	note := "mcpServers entries inside a settings.json layer are already counted under Settings, not added again here"
	return Row{Layer: "MCP (.mcp.json)", Items: items, Note: note}
}

// claudeHooksRow is informational only: hook entries live inside the
// settings layers already counted under Settings.
func claudeHooksRow(repoDir, home string) Row {
	count := 0
	for _, p := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(repoDir, ".claude", "settings.json"),
		filepath.Join(repoDir, ".claude", "settings.local.json"),
	} {
		if data, err := os.ReadFile(p); err == nil {
			count += strings.Count(string(data), `"type": "command"`)
		}
	}
	return Row{
		Layer: "Hooks (informational)",
		Note: "hook entries live inside the settings layers already counted under Settings, not added again here; " +
			strconv.Itoa(
				count,
			) + " command hook entries found (a rough count, not a parsed one)",
	}
}

func existingFileItems(paths []string) []Item {
	var items []Item
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		items = append(items, Item{Path: p, Bytes: len(data)})
	}
	return items
}

// capByLinesOrBytes returns the prefix of data bounded by whichever of
// maxLines/maxBytes is reached first, and whether that cut anything off.
func capByLinesOrBytes(data []byte, maxLines, maxBytes int) ([]byte, bool) {
	if len(data) <= maxBytes {
		lines := strings.Count(string(data), "\n")
		if lines <= maxLines {
			return data, false
		}
	}
	lineCount := 0
	limit := len(data)
	if limit > maxBytes {
		limit = maxBytes
	}
	for i := 0; i < limit; i++ {
		if data[i] == '\n' {
			lineCount++
			if lineCount >= maxLines {
				return data[:i+1], true
			}
		}
	}
	if limit < len(data) {
		return data[:limit], true
	}
	return data[:limit], false
}
