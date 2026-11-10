package main

import (
	"io/fs"
	"strings"
	"testing"
)

// The review skills load into other people's repositories, so they may not
// depend on this one (CLAUDE.md principle 8, §12). The wrapper must keep
// upstream's description exactly: the build compares them on every pin bump.

const upstreamReceivingDescription = "Use when receiving code review feedback, before implementing suggestions, " +
	"especially if feedback seems unclear or technically questionable - requires technical rigor " +
	"and verification, not performative agreement or blind implementation"

// skillFrontmatter returns the name and description of an embedded SKILL.md.
func skillFrontmatter(t *testing.T, path string) (name, description string, text string) {
	t.Helper()
	data, err := fs.ReadFile(ConfigsFS, path)
	if err != nil {
		t.Fatalf("%s must ship: %v", path, err)
	}
	text = string(data)
	front, _, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---")
	if !strings.HasPrefix(text, "---\n") || !ok {
		t.Fatalf("%s has no frontmatter", path)
	}
	for _, line := range strings.Split(front, "\n") {
		if v, found := strings.CutPrefix(line, "name:"); found {
			name = strings.TrimSpace(v)
		}
		if v, found := strings.CutPrefix(line, "description:"); found {
			description = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return name, description, text
}

func TestReviewSkills_AreShippedAndGeneral(t *testing.T) {
	skills := map[string]string{
		"review":                "configs/agent-packs/devgeta/skills/review/SKILL.md",
		"receiving-code-review": "configs/agent-packs/superpowers/skills/receiving-code-review/SKILL.md",
	}
	for want, path := range skills {
		t.Run(want, func(t *testing.T) {
			name, description, text := skillFrontmatter(t, path)
			if name != want {
				t.Errorf("name = %q, want %q", name, want)
			}
			if description == "" {
				t.Error("a skill with no description is never triggered")
			}
			for _, banned := range []string{"docs/plans", "docs/decisions", "docs/guides", "dg ", "devgeta", "cycle"} {
				if strings.Contains(text, banned) {
					t.Errorf("%s names %q, which means nothing in another repository", path, banned)
				}
			}
			if !strings.Contains(text, "names a pull request") {
				t.Error("each skill must say in its own prose when it may post: when the person names a pull request")
			}
		})
	}

	_, description, text := skillFrontmatter(t, skills["receiving-code-review"])
	if description != upstreamReceivingDescription {
		t.Errorf("the wrapper must keep upstream's description exactly, got %q", description)
	}
	if !strings.Contains(text, "UPSTREAM.md") {
		t.Error("the wrapper must point at UPSTREAM.md, which marks it as a wrapper")
	}
}

const upstreamWorktreesDescription = "Use when starting feature work that needs isolation from current workspace " +
	"or before executing implementation plans - ensures an isolated workspace exists via native " +
	"tools or git worktree fallback"

// The worktree wrapper ships to strangers too: it may name `devgeta worktree`, which
// ships with devgeta, but nothing of this repository's own process.
func TestWorktreeWrapper_IsShippedAndGeneral(t *testing.T) {
	name, description, text := skillFrontmatter(t, "configs/agent-packs/superpowers/skills/using-git-worktrees/SKILL.md")
	if name != "using-git-worktrees" {
		t.Errorf("name = %q", name)
	}
	if description != upstreamWorktreesDescription {
		t.Errorf("the wrapper must keep upstream's description exactly, got %q", description)
	}
	if !strings.Contains(text, "UPSTREAM.md") {
		t.Error("the wrapper must point at UPSTREAM.md, which marks it as a wrapper")
	}
	if !strings.Contains(text, "command -v devgeta") {
		t.Error("the wrapper must fall back to upstream when devgeta is not on PATH")
	}
	// `dg` is a shell alias, which an agent's non-interactive shell does not
	// load; only the `devgeta` binary resolves there.
	for _, banned := range []string{"dg ", "`dg`", "docs/plans", "docs/decisions", "docs/guides", "cycle"} {
		if strings.Contains(text, banned) {
			t.Errorf("the wrapper names %q, which means nothing in another repository", banned)
		}
	}
}
