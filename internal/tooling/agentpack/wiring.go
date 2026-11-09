package agentpack

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/config"
)

// PackBuilder is the Engine's TreeSource: it fetches a pack's pinned clone and
// builds the tree from it, with devgeta's override layer first and the user's
// on top.
type PackBuilder struct {
	Fetcher *Fetcher
	Builder *Builder
	// ShippedLayerDir is paths.Paths.App.Configs.AgentPacks, where
	// ExtractEmbeddedConfigs put devgeta's overrides.
	ShippedLayerDir string
	// UserLayerDir is paths.Paths.Config.AgentPacks.
	UserLayerDir string
}

// Prepare builds m's tree. An upstream pack is fetched at its pin and built
// with devgeta's override layer then the user's. An embedded pack has no
// upstream: its source is the folder ExtractEmbeddedConfigs wrote, built with
// the user's layer only, so a user can still replace its DEVGETA.md.
func (p *PackBuilder) Prepare(m Manifest, exclude []string) (Tree, BuildResult, error) {
	opts, _, err := p.options(m, "", exclude)
	if err != nil {
		return Tree{}, BuildResult{}, err
	}
	res, err := p.Builder.Build(opts)
	if err != nil {
		return Tree{}, BuildResult{}, err
	}
	return Tree{Pack: m, Dir: res.Dir}, res, nil
}

// Check builds m into a temp tree, as Prepare would, and shows it to inspect
// before deleting it. A non-empty ref is fetched instead of the pin, and the
// commit it resolved to is returned; for the pin that is the pinned commit.
func (p *PackBuilder) Check(
	m Manifest, ref string, exclude []string, inspect func(dir string) error,
) (BuildResult, string, error) {
	opts, sha, err := p.options(m, ref, exclude)
	if err != nil {
		return BuildResult{}, "", err
	}
	res, err := p.Builder.Check(opts, inspect)
	if err != nil {
		return BuildResult{}, "", err
	}
	return res, sha, nil
}

// options fetches what m builds from and lays out its override layers. It
// returns the commit the source is at ("" for an embedded pack).
func (p *PackBuilder) options(m Manifest, ref string, exclude []string) (BuildOptions, string, error) {
	opts := BuildOptions{Manifest: m, Exclude: exclude}
	if m.Embedded() {
		opts.Source = filepath.Join(p.ShippedLayerDir, m.Name)
		opts.Layers = []string{filepath.Join(p.UserLayerDir, m.Name)}
		return opts, "", nil
	}
	var sha string
	var err error
	if ref == "" {
		opts.Source, err = p.Fetcher.Fetch(m)
		sha = m.SHA
	} else {
		try := m
		try.Tag = ref
		opts.Source, sha, err = p.Fetcher.FetchRef(try)
	}
	if err != nil {
		return BuildOptions{}, "", err
	}
	opts.Layers = []string{
		filepath.Join(p.ShippedLayerDir, m.Name),
		filepath.Join(p.UserLayerDir, m.Name),
	}
	return opts, sha, nil
}

// Remove deletes a pack's built tree. The clone cache is left alone, so a
// reinstall at the same commit doesn't need the network (ADR-0070 §4). The
// name is validated like a manifest's, so it can never name a path outside the
// pack root.
func (p *PackBuilder) Remove(pack string) error {
	if !packNamePattern.MatchString(pack) {
		return fmt.Errorf("refusing to delete the tree of %q: not a pack name", pack)
	}
	return removeTree(filepath.Join(p.Builder.Root, pack))
}

// ConfigStore is the Engine's StateStore, over global_config.yaml.
type ConfigStore struct{}

// Load returns the agent_packs state. A machine that has never installed a
// pack has no config file yet, which is simply "no packs".
func (ConfigStore) Load() (map[string]config.AgentPackState, error) {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]config.AgentPackState{}, nil
		}
		return nil, fmt.Errorf("failed to read global config: %w", err)
	}
	if gc.AgentPacks == nil {
		return map[string]config.AgentPackState{}, nil
	}
	return gc.AgentPacks, nil
}

// Update applies fn to the freshly read state under the config's lock and
// saves it. An emptied map is saved as no key at all.
func (ConfigStore) Update(fn func(map[string]config.AgentPackState) error) error {
	return config.Update(func(gc *config.GlobalConfig) error {
		if gc.AgentPacks == nil {
			gc.AgentPacks = map[string]config.AgentPackState{}
		}
		if err := fn(gc.AgentPacks); err != nil {
			return err
		}
		if len(gc.AgentPacks) == 0 {
			gc.AgentPacks = nil
		}
		return nil
	})
}
