package contextreport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

// findRow returns the row whose Layer starts with prefix, failing the test
// if none does.
func findRow(t *testing.T, rows []Row, prefix string) Row {
	t.Helper()
	for _, r := range rows {
		if strings.HasPrefix(r.Layer, prefix) {
			return r
		}
	}
	t.Fatalf("no row with layer prefix %q among %d rows", prefix, len(rows))
	return Row{}
}

func TestClaudeMemoryRowConcatenatesUserAndProjectLayers(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()

	writeFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), "user layer\n")
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), "project layer\n")
	writeFile(t, filepath.Join(repo, "CLAUDE.local.md"), "local layer\n")

	row := claudeMemoryRow(repo, home)
	if len(row.Items) != 3 {
		t.Fatalf("items = %+v, want 3 (user, project, local)", row.Items)
	}
	total := row.TotalBytes()
	want := len("user layer\n") + len("project layer\n") + len("local layer\n")
	if total != want {
		t.Errorf("TotalBytes = %d, want %d", total, want)
	}
}

func TestClaudeMemoryRowFollowsImportsFromTheUserLayer(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()

	writeFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), "@RTK.md\n")
	writeFile(t, filepath.Join(home, ".claude", "RTK.md"), "rtk instructions\n")

	row := claudeMemoryRow(repo, home)
	if len(row.Items) != 2 {
		t.Fatalf("items = %+v, want 2 (CLAUDE.md + imported RTK.md)", row.Items)
	}
}

func TestClaudeAutoMemoryRowCapsAt25KB(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	initBareGitRepo(t, repo)

	// Resolve the slug the same way claudeAutoMemoryRow does — via git's own
	// --path-format=absolute, which resolves symlinks (macOS's t.TempDir()
	// lives under /var/folders, a symlink to /private/var/folders; a plain
	// filepath.Abs would not follow it, and the slug would never match).
	root, err := mainCheckoutRoot(repo, realGitApp(t))
	if err != nil {
		t.Fatal(err)
	}
	slug := slugifyPath(root)
	memoryDir := filepath.Join(home, ".claude", "projects", slug, "memory")
	big := strings.Repeat("x", 30*1024)
	writeFile(t, filepath.Join(memoryDir, "MEMORY.md"), big)

	row := claudeAutoMemoryRow(repo, home, realGitApp(t))
	if row.TotalBytes() > 25*1024 {
		t.Errorf("TotalBytes = %d, want <= 25KB", row.TotalBytes())
	}
	if !strings.Contains(row.Note, "capped") {
		t.Errorf("expected the note to say capped, got: %q", row.Note)
	}
}

func TestClaudeProjectRulesRowExcludesPathScopedRules(t *testing.T) {
	repo := t.TempDir()
	writeFile(
		t,
		filepath.Join(repo, ".claude", "rules", "always.md"),
		"---\nname: x\n---\nalways loaded\n",
	)
	writeFile(
		t, filepath.Join(repo, ".claude", "rules", "scoped.md"),
		"---\npaths:\n  - \"src/**\"\n---\nonly when a matching file opens\n",
	)

	row := claudeProjectRulesRow(repo)
	if len(row.Items) != 1 {
		t.Fatalf("items = %+v, want exactly 1 (the unscoped rule)", row.Items)
	}
	if !strings.Contains(row.Items[0].Path, "always.md") {
		t.Errorf("expected always.md, got %s", row.Items[0].Path)
	}
}

func TestClaudeSettingsRowCountsEveryPresentLayer(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"a":1}`)
	writeFile(t, filepath.Join(repo, ".claude", "settings.local.json"), `{"b":2}`)
	// No project settings.json — must not error on the missing layer.

	row := claudeSettingsRow(repo, home)
	if len(row.Items) != 2 {
		t.Fatalf("items = %+v, want 2 (user + local; project settings.json absent)", row.Items)
	}
}

func TestClaudeSkillsRowCountsOnlyFrontmatter(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	frontmatter := "---\nname: demo\ndescription: a demo skill\n---\n"
	body := strings.Repeat("body text ", 200)
	writeFile(t, filepath.Join(home, ".claude", "skills", "demo", "SKILL.md"), frontmatter+body)

	row := claudeFrontmatterOnlyRow("Skills", repo, home, "skills", "SKILL.md")
	if row.TotalBytes() != len(frontmatter) {
		t.Errorf(
			"TotalBytes = %d, want %d (frontmatter only, not %d)",
			row.TotalBytes(),
			len(frontmatter),
			len(frontmatter)+len(body),
		)
	}
}

func TestClaudeCommandsRowCountsOnlyFrontmatter(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	frontmatter := "---\ndescription: a demo command\n---\n"
	body := strings.Repeat("body text ", 200)
	writeFile(t, filepath.Join(home, ".claude", "commands", "demo.md"), frontmatter+body)

	row := claudeCommandsRow(repo, home)
	if row.TotalBytes() != len(frontmatter) {
		t.Errorf("TotalBytes = %d, want %d", row.TotalBytes(), len(frontmatter))
	}
}

func TestClaudeMCPRowCountsTheProjectFile(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers":{}}`)

	row := claudeMCPRow(repo, home)
	if row.TotalBytes() != len(`{"mcpServers":{}}`) {
		t.Errorf("TotalBytes = %d, want %d", row.TotalBytes(), len(`{"mcpServers":{}}`))
	}
}

func TestDiscoverClaudeReturnsEveryRow(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	initBareGitRepo(t, repo)

	report := DiscoverClaude(repo, home, realGitApp(t))
	if report.Agent != "claude" {
		t.Errorf("Agent = %q, want claude", report.Agent)
	}
	wantLayers := []string{
		"Memory", "Auto memory", "Project rules", "Settings",
		"Skills", "Commands", "Agents", "Plugins", "MCP", "Hooks",
	}
	for _, want := range wantLayers {
		findRow(t, report.Rows, want) // fails the test if missing
	}
}

// initBareGitRepo creates a real git repo (using the real `git` binary, the
// same posture as other real-git fixtures in this codebase, e.g.
// reviewjournal's) so realGitApp's CommonDirIn resolves against actual git
// plumbing rather than a hand-faked .git layout.
func initBareGitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found on PATH")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
}

// realGitApp returns a *git.Git backed by the real command executor — this
// package's own discovery calls real git plumbing (CommonDirIn), which is
// the thing under test here, not something to mock away.
func realGitApp(t *testing.T) *git.Git {
	t.Helper()
	return git.New()
}
