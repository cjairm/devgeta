package agentpack

import (
	"fmt"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/apps/claude"
	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/opencode"
	"github.com/cjairm/devgeta/internal/apps/pi"
	"github.com/cjairm/devgeta/pkg/paths"
)

// DefaultPacks are the packs `dg install` installs, in this order: devgeta's
// own rules first, then the two upstreams (ADR-0068 §8).
var DefaultPacks = []string{"devgeta", "superpowers", "ponytail"}

// NewEngine builds the engine `dg agent-pack` runs, from the real coder apps
// and the real paths. The shipped pack configs must already be extracted
// (paths.Paths.App.Configs.AgentPacks); the command refreshes them first.
//
// Every coder CLI and git call goes through the app wrappers, which is also
// what lets tests swap the executor underneath them.
func NewEngine() (*Engine, error) {
	manifests, err := LoadManifests(paths.Paths.App.Configs.AgentPacks)
	if err != nil {
		return nil, fmt.Errorf(
			"could not load the agent pack definitions: %w\n"+
				"They are extracted from the devgeta binary; run any `dg configure <app>` to refresh them", err,
		)
	}
	root := paths.Paths.App.AgentPacks
	return &Engine{
		Manifests: manifests,
		Installers: []Installer{
			&ClaudeInstaller{CLI: claude.New(), PacksRoot: root, ConfigDir: paths.Paths.Config.Claude},
			&OpenCodeInstaller{
				App:       opencode.New(),
				OwnConfig: filepath.Join(paths.Paths.Config.OpenCode, "opencode.json"),
			},
			&PiInstaller{App: pi.New(), PacksRoot: root},
		},
		Trees: &PackBuilder{
			Fetcher:         &Fetcher{Git: git.New(), Root: root},
			Builder:         &Builder{Root: root},
			ShippedLayerDir: paths.Paths.App.Configs.AgentPacks,
			UserLayerDir:    paths.Paths.Config.AgentPacks,
		},
		State: ConfigStore{},
	}, nil
}
