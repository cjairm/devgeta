// Package agentpack is the engine behind `dg agent-pack`: it fetches a pinned
// upstream, builds a tree devgeta owns, and installs that tree into each AI
// coder through the coder's own plugin or package system (ADR-0068).
//
// Nothing here is specific to a coder except the three installers. A future
// pack aimed at another app reuses the manifest, fetch, build and state with
// a different installer.
package agentpack

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/pkg/constants"
	"gopkg.in/yaml.v3"
)

// sourceEmbedded marks a pack whose content ships inside the devgeta binary
// instead of being fetched from a repository (the `devgeta` pack).
const sourceEmbedded = "embedded"

const manifestFile = "pack.yaml"

var (
	// A pack name becomes a folder name and a Claude plugin id, so it is kept
	// to a conservative alphabet that cannot climb out of a directory.
	packNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	// The pin is the commit, not the tag: both upstreams use annotated tags,
	// whose own hash is a tag object that `git rev-parse HEAD` never returns.
	commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Manifest is a pack's pin: where its upstream lives and the exact commit
// that was checked against devgeta's overrides.
type Manifest struct {
	Name string `yaml:"name"`
	Repo string `yaml:"repo,omitempty"`
	Tag  string `yaml:"tag,omitempty"`
	// SHA is the full 40-character commit that Tag must resolve to.
	SHA string `yaml:"sha,omitempty"`
	// Source is "embedded" for a pack that ships in the binary, empty otherwise.
	Source string `yaml:"source,omitempty"`
	// Requires lists commands the pack needs on PATH to work fully. A missing
	// one is named when the pack is installed; it is a warning, not a failure.
	Requires []Requirement `yaml:"requires,omitempty"`
	// Notices are things to tell the user after an install.
	Notices []Notice `yaml:"notices,omitempty"`
}

// Requirement is a command a pack needs and why.
type Requirement struct {
	Command string `yaml:"command"`
	Reason  string `yaml:"reason"`
}

// Notice is a message shown after an install, to everyone or only when the
// pack went into one coder.
type Notice struct {
	Coder string `yaml:"coder,omitempty"`
	Text  string `yaml:"text"`
}

// MissingRuntimes returns the required commands look cannot find, in the
// manifest's order.
func (m Manifest) MissingRuntimes(look func(file string) (string, error)) []Requirement {
	var missing []Requirement
	for _, r := range m.Requires {
		if _, err := look(r.Command); err != nil {
			missing = append(missing, r)
		}
	}
	return missing
}

// Embedded reports whether the pack ships inside the binary and has no upstream.
func (m Manifest) Embedded() bool { return m.Source == sourceEmbedded }

// ParseManifest decodes and validates one pack.yaml. Unknown keys are an
// error, so a typo such as `sah:` can't silently leave a pack unpinned.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return Manifest{}, fmt.Errorf("pack manifest is empty")
		}
		return Manifest{}, fmt.Errorf("invalid pack manifest: %w", err)
	}
	if err := m.validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) validate() error {
	if !packNamePattern.MatchString(m.Name) {
		return fmt.Errorf(
			"invalid pack name %q: use lower-case letters, digits and '-', starting with a letter",
			m.Name,
		)
	}
	if err := m.validateExtras(); err != nil {
		return err
	}
	switch m.Source {
	case sourceEmbedded:
		if m.Repo != "" || m.Tag != "" || m.SHA != "" {
			return fmt.Errorf(
				"pack %s is embedded, so it cannot also set repo, tag or sha", m.Name,
			)
		}
		return nil
	case "":
	default:
		return fmt.Errorf("pack %s has unknown source %q (only %q is supported)",
			m.Name, m.Source, sourceEmbedded)
	}
	if !strings.HasPrefix(m.Repo, "https://") {
		return fmt.Errorf("pack %s: repo must be an https:// URL, got %q", m.Name, m.Repo)
	}
	if m.Tag == "" || strings.HasPrefix(m.Tag, "-") || strings.ContainsAny(m.Tag, " \t\n") {
		return fmt.Errorf("pack %s: tag %q is not a usable git tag", m.Name, m.Tag)
	}
	if !commitSHAPattern.MatchString(m.SHA) {
		return fmt.Errorf(
			"pack %s: sha must be the full 40-character lower-case commit hash "+
				"(git rev-parse HEAD in a clone of the tag), got %q",
			m.Name, m.SHA,
		)
	}
	return nil
}

var commandPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]*$`)

// validateExtras checks the optional requires and notices. A required command
// is a bare name looked up on PATH, so a path or a command line is refused.
func (m Manifest) validateExtras() error {
	for i, r := range m.Requires {
		if !commandPattern.MatchString(r.Command) {
			return fmt.Errorf(
				"pack %s: requires[%d].command %q must be a bare command name such as node",
				m.Name, i, r.Command,
			)
		}
		if strings.TrimSpace(r.Reason) == "" {
			return fmt.Errorf("pack %s: requires[%d] (%s) needs a reason", m.Name, i, r.Command)
		}
	}
	for i, n := range m.Notices {
		if strings.TrimSpace(n.Text) == "" {
			return fmt.Errorf("pack %s: notices[%d] needs text", m.Name, i)
		}
		switch n.Coder {
		case "", constants.Claude, constants.OpenCode, constants.Pi:
		default:
			return fmt.Errorf(
				"pack %s: notices[%d].coder %q is not a coder (use %s, %s or %s)",
				m.Name, i, n.Coder, constants.Claude, constants.OpenCode, constants.Pi,
			)
		}
	}
	return nil
}

// LoadManifests reads every <root>/<pack>/pack.yaml, sorted by pack name. A
// folder without a pack.yaml is skipped, and a manifest whose name differs
// from its folder is an error: the folder is what the user's override layer
// is keyed on, so the two must never drift apart.
func LoadManifests(root string) ([]Manifest, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("failed to read the agent pack configs at %s: %w", root, err)
	}
	var out []Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, e.Name(), manifestFile))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read the manifest of pack %s: %w", e.Name(), err)
		}
		m, err := ParseManifest(data)
		if err != nil {
			return nil, fmt.Errorf("pack %s: %w", e.Name(), err)
		}
		if m.Name != e.Name() {
			return nil, fmt.Errorf(
				"pack folder %s holds a manifest named %q: they must match", e.Name(), m.Name,
			)
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
