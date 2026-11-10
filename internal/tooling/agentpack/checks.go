package agentpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// checkNoDanglingReferences fails when a kept skill names an excluded one. An
// excluded skill is gone from every coder, so a kept skill that tells the
// agent to "use alpha" would fail at runtime; refusing at build time names both
// skills instead. Every offending pair is reported, not just the first.
func checkNoDanglingReferences(tree string, excluded []string) error {
	if len(excluded) == 0 {
		return nil
	}
	kept, err := listSkills(tree)
	if err != nil {
		return err
	}
	var problems []error
	for _, skill := range kept {
		files, err := markdownIn(tree, filepath.Join(tree, "skills", skill))
		if err != nil {
			return err
		}
		sort.Strings(files)
		for _, rel := range files {
			data, err := os.ReadFile(filepath.Join(tree, rel))
			if err != nil {
				return err
			}
			for _, name := range excluded {
				if mentionsSkill(string(data), name) {
					problems = append(problems, fmt.Errorf(
						"skill %q (%s) refers to the excluded skill %q: "+
							"exclude %q too, or override %q so it no longer does",
						skill, filepath.ToSlash(rel), name, skill, skill,
					))
				}
			}
		}
	}
	return errors.Join(problems...)
}

// checkRequiredFiles verifies the files the three installers read:
// .claude-plugin/plugin.json and the hook files it declares for Claude Code,
// and a package.json with the `pi` and `main` keys for Pi and OpenCode.
//
// A plugin names its hook files in plugin.json's "hooks" key, as a path or a
// list of paths, and Claude Code falls back to hooks/hooks.json only when the
// key is absent. Ponytail declares two files and has no hooks/hooks.json, so
// checking the fixed name would reject it. Inline hooks (an object) name no
// file, so there is nothing to check.
func checkRequiredFiles(tree string) error {
	pluginPath := filepath.Join(tree, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(pluginPath)
	if err != nil {
		return fmt.Errorf("the pack has no .claude-plugin/plugin.json, which Claude Code needs: %w", err)
	}
	var plugin struct {
		Hooks json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(data, &plugin); err != nil {
		return fmt.Errorf(".claude-plugin/plugin.json is not valid JSON: %w", err)
	}
	hookFiles, err := declaredHookFiles(plugin.Hooks)
	if err != nil {
		return err
	}
	for _, rel := range hookFiles {
		info, err := os.Stat(filepath.Join(tree, filepath.FromSlash(rel)))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("the pack's hooks file %s is missing: Claude Code loads its hooks from it", rel)
		}
	}

	data, err = os.ReadFile(filepath.Join(tree, "package.json"))
	if err != nil {
		return fmt.Errorf("the pack has no package.json, which Pi and OpenCode need: %w", err)
	}
	var pkg map[string]json.RawMessage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return fmt.Errorf("package.json is not valid JSON: %w", err)
	}
	var main string
	if raw, ok := pkg["main"]; !ok || json.Unmarshal(raw, &main) != nil || main == "" {
		return fmt.Errorf(`package.json has no "main" entry, which OpenCode needs`)
	}
	if _, ok := pkg["pi"]; !ok {
		return fmt.Errorf(`package.json has no "pi" entry, which Pi needs`)
	}
	return nil
}

// checkEmbeddedFiles verifies what an embedded pack (the devgeta pack) needs:
// a non-empty DEVGETA.md, which every coder is pointed at, a plugin.json so
// Claude Code can load its skills, and a package.json with a `pi` entry
// (carrying `skills` when the pack has any), because the Pi side is an
// ordinary Pi package. It has no hooks file to check.
func checkEmbeddedFiles(tree string) error {
	text, err := os.ReadFile(filepath.Join(tree, instructionsFile))
	if err != nil {
		return fmt.Errorf("the pack has no %s: %w", instructionsFile, err)
	}
	if strings.TrimSpace(string(text)) == "" {
		return fmt.Errorf("%s is empty, so every coder would load nothing", instructionsFile)
	}
	data, err := os.ReadFile(filepath.Join(tree, "package.json"))
	if err != nil {
		return fmt.Errorf("the pack has no package.json, which Pi needs: %w", err)
	}
	var pkg map[string]json.RawMessage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return fmt.Errorf("package.json is not valid JSON: %w", err)
	}
	rawPi, ok := pkg["pi"]
	if !ok {
		return fmt.Errorf(`package.json has no "pi" entry, which Pi needs`)
	}
	if info, err := os.Stat(filepath.Join(tree, ".claude-plugin", "plugin.json")); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("the pack has no .claude-plugin/plugin.json, which Claude Code needs to load its skills")
	}
	if skills, err := listSkills(tree); err == nil && len(skills) > 0 {
		var pi struct {
			Skills []string `json:"skills"`
		}
		if json.Unmarshal(rawPi, &pi) != nil || len(pi.Skills) == 0 {
			return fmt.Errorf(`package.json's "pi" section has no "skills" entry (pi.skills), so Pi would not load the pack's skills`)
		}
	}
	return nil
}

// declaredHookFiles turns plugin.json's "hooks" value into the files it names,
// as slash paths relative to the tree.
func declaredHookFiles(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []string{"hooks/hooks.json"}, nil
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return cleanTreePaths([]string{single})
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		var names []string
		for _, item := range list {
			var s string
			if json.Unmarshal(item, &s) == nil {
				names = append(names, s)
			} // an object is an inline definition: no file to check
		}
		return cleanTreePaths(names)
	}
	return nil, nil // an inline object
}

func cleanTreePaths(names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		clean := path.Clean(strings.TrimPrefix(filepath.ToSlash(n), "./"))
		if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return nil, fmt.Errorf("plugin.json names the hooks file %q, which is outside the pack", n)
		}
		out = append(out, clean)
	}
	return out, nil
}

// hashFile is the form replaced_hashes are recorded in.
func hashFile(p string) (string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ChangedReplacements lists the fully replaced files whose upstream hash is
// not what was recorded when the replacement was written: the file changed
// upstream, or is gone from it. A person has to re-read those, because a full
// replacement does not follow upstream. Sorted for stable output.
func ChangedReplacements(recorded, current map[string]string) []string {
	var changed []string
	for file, was := range recorded {
		if now, ok := current[file]; !ok || now != was {
			changed = append(changed, file)
		}
	}
	sort.Strings(changed)
	return changed
}
