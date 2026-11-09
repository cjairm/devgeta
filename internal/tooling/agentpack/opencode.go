package agentpack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
)

// OpenCodeApp is the part of the OpenCode app wrapper the installer uses. The
// real *opencode.OpenCode satisfies it.
type OpenCodeApp interface {
	IsInstalled() bool
	// ConfigureWithPacks re-renders opencode.json with the given pack state.
	ConfigureWithPacks(packs map[string]config.AgentPackState) error
	ExecuteCommandOutput(args ...string) (string, error)
}

// OpenCodeInstaller puts a built tree into OpenCode as an entry in
// opencode.json's `plugins`: an absolute folder path (OpenCode 2 rejects a
// file path and does not expand "~").
//
// It never edits opencode.json. That file is rendered by devgeta from
// agent_packs state; Install only reports what to list, and Render, which the
// engine calls with the state as it will be, re-renders it through the app.
type OpenCodeInstaller struct {
	App OpenCodeApp
	// OwnConfig is the opencode.json devgeta renders. Its plugins are
	// devgeta's own, so a conflict check never counts them.
	OwnConfig string
}

var (
	_ Installer = (*OpenCodeInstaller)(nil)
	_ Renderer  = (*OpenCodeInstaller)(nil)
)

// Name is the coder's name.
func (o *OpenCodeInstaller) Name() string { return constants.OpenCode }

// Available reports whether opencode is installed.
func (o *OpenCodeInstaller) Available() bool { return o.App.IsInstalled() }

// pluginFolder returns the folder OpenCode 2 should load a tree from: the first
// of <tree>/.opencode/plugins and the tree root that holds an index.js, which
// is the directory form OpenCode 2 requires. Superpowers keeps its entry at the
// root and Ponytail in .opencode/plugins; this finds either without naming a
// pack. It is empty when the pack has no OpenCode plugin.
func pluginFolder(treeDir string) string {
	for _, dir := range []string{filepath.Join(treeDir, ".opencode", "plugins"), treeDir} {
		if info, err := os.Stat(filepath.Join(dir, "index.js")); err == nil && info.Mode().IsRegular() {
			return dir
		}
	}
	return ""
}

// Skipped names what OpenCode cannot load from the tree.
func (o *OpenCodeInstaller) Skipped(tree Tree) []string {
	if tree.Pack.Embedded() || pluginFolder(tree.Dir) != "" {
		return nil
	}
	return []string{
		"the OpenCode plugin (no index.js in .opencode/plugins/ or at the pack root)",
	}
}

// Install reports the plugin folder to list. A pack with no OpenCode plugin
// gets an empty record and a Skipped entry rather than an error: the pack's
// skills and commands may still be useful elsewhere, and nothing is faked.
func (o *OpenCodeInstaller) Install(tree Tree) (Record, error) {
	if tree.Pack.Embedded() {
		// The devgeta pack is instructions, not a plugin: OpenCode reads the
		// file through an `instructions` entry in opencode.json.
		file := filepath.Join(tree.Dir, instructionsFile)
		if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
			return Record{}, fmt.Errorf("pack %s has no %s, so OpenCode has nothing to load", tree.Pack.Name, instructionsFile)
		}
		return Record{OpenCodeInstructions: []string{file}}, nil
	}
	folder := pluginFolder(tree.Dir)
	if folder == "" {
		return Record{}, nil
	}
	return Record{OpenCodePlugins: []string{folder}}, nil
}

// Uninstall has nothing of its own to do: the engine drops the record from
// state and re-renders opencode.json through Render, which removes the entry.
func (o *OpenCodeInstaller) Uninstall(Record) error { return nil }

// Render re-renders opencode.json with the given state.
func (o *OpenCodeInstaller) Render(packs map[string]config.AgentPackState) error {
	return o.App.ConfigureWithPacks(packs)
}

// Conflicts finds a plugin spec for the same upstream in a config other than
// devgeta's own, such as a project's opencode.json, and says which file to
// edit. devgeta never edits them. It reads `opencode debug config
// --standalone`: --standalone because OpenCode 2 otherwise tries the user's
// shared background service first. When that can't be read, no conflict is
// reported rather than invented.
func (o *OpenCodeInstaller) Conflicts(pack Manifest) []string {
	if pack.Embedded() {
		return nil
	}
	out, err := o.App.ExecuteCommandOutput("debug", "config", "--standalone")
	if err != nil {
		logger.L().Warnw("could not check OpenCode for another copy", "pack", pack.Name, "err", err)
		return nil
	}
	var sources []struct {
		Type string `json:"type"`
		Path string `json:"path"`
		Info struct {
			Plugins []string `json:"plugins"`
		} `json:"info"`
	}
	if err := json.Unmarshal([]byte(out), &sources); err != nil {
		logger.L().Warnw("could not read `opencode debug config`", "pack", pack.Name, "err", err)
		return nil
	}
	var found []string
	for _, src := range sources {
		if src.Type != "document" || filepath.Clean(src.Path) == filepath.Clean(o.OwnConfig) {
			continue
		}
		for _, spec := range src.Info.Plugins {
			if sourceNamesPack(spec, pack) {
				found = append(found, fmt.Sprintf(
					"remove %q from \"plugins\" in %s", spec, src.Path,
				))
			}
		}
	}
	return found
}
