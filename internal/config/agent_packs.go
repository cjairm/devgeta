package config

import "sort"

// AgentPackState records one agent pack devgeta installed (ADR-0070 §1): the
// version it was built from, the user's saved exclusions, and per coder
// exactly what was added. Uninstall removes only what is listed here, and
// `dg configure` renders the coder config entries from it so `--force` keeps
// them.
type AgentPackState struct {
	// Version is the pinned tag the installed tree was built from.
	Version string `yaml:"version"`
	// SHA is the full commit Version resolved to.
	SHA string `yaml:"sha"`
	// Exclude is the skill list saved by `--exclude`. It is applied on every
	// rebuild until a later --exclude replaces it or `--exclude=` clears it.
	Exclude []string `yaml:"exclude,omitempty"`
	// ReplacedHashes maps each fully replaced upstream file to upstream's hash
	// at build time, so `check` can flag a replacement upstream has outrun.
	ReplacedHashes map[string]string `yaml:"replaced_hashes,omitempty"`
	// Coders is keyed by coder name (constants.Claude, OpenCode, Pi).
	Coders map[string]AgentPackCoderState `yaml:"coders,omitempty"`
}

// AgentPackCoderState is what one pack added to one coder. Each coder uses
// only its own fields; the rest stay empty and are not written.
type AgentPackCoderState struct {
	// Marketplace and Plugin are Claude Code's: the marketplace devgeta
	// registered and the "<pack>@<marketplace>" plugin id it installed.
	Marketplace string `yaml:"marketplace,omitempty"`
	Plugin      string `yaml:"plugin,omitempty"`
	// ClaudeMDLineAdded is true when devgeta added the `@DEVGETA.md` line to
	// ~/.claude/CLAUDE.md itself, as opposed to finding one already there.
	ClaudeMDLineAdded bool `yaml:"claude_md_line_added,omitempty"`
	// InstructionsFile is the copy of DEVGETA.md devgeta wrote into the
	// coder's own folder (Claude Code's ~/.claude/DEVGETA.md). It is devgeta's
	// file, so uninstall deletes it (ADR-0070 §3).
	InstructionsFile string `yaml:"instructions_file,omitempty"`
	// OpenCodePlugins, OpenCodeInstructions and OpenCodeSkills are the absolute
	// paths this pack contributed to opencode.json's `plugins`, `instructions`
	// and `skills`.
	OpenCodePlugins      []string `yaml:"opencode_plugins,omitempty"`
	OpenCodeInstructions []string `yaml:"opencode_instructions,omitempty"`
	OpenCodeSkills       []string `yaml:"opencode_skills,omitempty"`
	// PiPackage is the path given to `pi install`.
	PiPackage string `yaml:"pi_package,omitempty"`
}

// ForgetAgentPackCoder drops coder from every pack's record, for a coder whose
// config directory was just deleted (ADR-0070 §6). A pack left with no coder
// is removed too, and its name is returned so the caller can delete the built
// tree it no longer needs.
func (gc *GlobalConfig) ForgetAgentPackCoder(coder string) []string {
	var orphaned []string
	for name, pack := range gc.AgentPacks {
		if _, ok := pack.Coders[coder]; !ok {
			continue
		}
		delete(pack.Coders, coder)
		if len(pack.Coders) == 0 {
			delete(gc.AgentPacks, name)
			orphaned = append(orphaned, name)
		}
	}
	sort.Strings(orphaned)
	return orphaned
}
