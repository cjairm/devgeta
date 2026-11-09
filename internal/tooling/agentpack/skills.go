package agentpack

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// skillMeta is the part of a SKILL.md frontmatter the build compares.
type skillMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// parseSkillMeta reads name and description from a SKILL.md's frontmatter.
//
// Real skills often carry descriptions that are not valid YAML (an unquoted
// "Use when: ..."), and the coders accept them, so a YAML failure falls back to
// reading the lines directly. The same function reads both sides of every
// comparison, so what matters is that it is consistent, not that it is a full
// YAML parser. Whitespace is collapsed so a folded description compares by its
// words.
func parseSkillMeta(data []byte) (skillMeta, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return skillMeta{}, fmt.Errorf("no frontmatter: the file must start with ---")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return skillMeta{}, fmt.Errorf("the frontmatter is never closed with ---")
	}
	front := rest[:end]

	var m skillMeta
	if err := yaml.Unmarshal([]byte(front), &m); err != nil || m.Name == "" {
		m = lenientSkillMeta(front)
	}
	m.Name = strings.TrimSpace(m.Name)
	m.Description = strings.Join(strings.Fields(m.Description), " ")
	if m.Name == "" {
		return skillMeta{}, fmt.Errorf("the frontmatter has no name")
	}
	return m, nil
}

var frontmatterKey = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*):[ \t]*(.*)$`)

func lenientSkillMeta(front string) skillMeta {
	var m skillMeta
	current := ""
	for _, line := range strings.Split(front, "\n") {
		if match := frontmatterKey.FindStringSubmatch(line); match != nil {
			current = match[1]
			value := strings.Trim(strings.TrimSpace(match[2]), `"'`)
			if value == ">" || value == "|" || value == ">-" || value == "|-" {
				value = ""
			}
			switch current {
			case "name":
				m.Name = value
			case "description":
				m.Description = value
			}
			continue
		}
		if current == "description" {
			m.Description += " " + strings.TrimSpace(line)
		}
	}
	return m
}

// listSkills returns the skill folders directly under <tree>/skills, sorted.
// Only directories count: a pack may keep other files there (Ponytail has a
// plugin.json), and a symlink is an alias, not a skill of its own.
func listSkills(tree string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(tree, "skills"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// mentionsSkill reports whether text names the skill, as a bare name or after
// a pack prefix ("pack:name"). The name must stand alone: "alpha-two" and
// "alphabet" don't mention "alpha", because skill names are hyphenated words.
func mentionsSkill(text, name string) bool {
	re := regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(name) + `(?:[^A-Za-z0-9_-]|$)`)
	return re.MatchString(text)
}

// markdownIn returns every .md file under dir, as paths relative to base.
func markdownIn(base, dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && strings.EqualFold(filepath.Ext(p), ".md") {
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}
