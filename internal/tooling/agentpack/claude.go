package agentpack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
)

// ClaudeCLI is the part of the Claude app wrapper the installer drives. The
// real *claude.Claude satisfies it.
type ClaudeCLI interface {
	ExecuteCommand(args ...string) error
	ExecuteCommandOutput(args ...string) (string, error)
}

// ClaudeInstaller installs a built tree into Claude Code as a plugin of a
// local directory marketplace named "devgeta" (ADR-0068 §5).
//
// It goes through `claude plugin ...` rather than editing settings.json,
// because that is what records the install for `claude plugin list` and
// `/plugin`. The same CLI writes extraKnownMarketplaces and enabledPlugins into
// settings.json; the Claude app renders the same two keys from agent_packs
// state, so `dg configure claude --force` keeps them.
type ClaudeInstaller struct {
	CLI ClaudeCLI
	// PacksRoot is paths.Paths.App.AgentPacks: the marketplace folder, whose
	// subfolders are the built packs.
	PacksRoot string
	// ConfigDir is ~/.claude (paths.Paths.Config.Claude): where the devgeta
	// pack's DEVGETA.md is copied and where CLAUDE.md lives.
	ConfigDir string
	// LookPath finds the claude binary; cmd.LookPathFn when nil.
	LookPath func(file string) (string, error)

	notes []string
}

var _ Installer = (*ClaudeInstaller)(nil)
var (
	_ Inspector    = (*ClaudeInstaller)(nil)
	_ Finisher     = (*ClaudeInstaller)(nil)
	_ RecordMerger = (*ClaudeInstaller)(nil)
	_ Noter        = (*ClaudeInstaller)(nil)
)

// Name is the coder's name.
func (c *ClaudeInstaller) Name() string { return constants.Claude }

// Available reports whether the claude binary is on PATH.
func (c *ClaudeInstaller) Available() bool {
	look := c.LookPath
	if look == nil {
		look = cmd.LookPathFn
	}
	_, err := look(constants.Claude)
	return err == nil
}

// Skipped is empty: a Claude Code plugin loads hooks, skills and commands.
func (c *ClaudeInstaller) Skipped(Tree) []string { return nil }

type claudePlugin struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
}

type claudeMarketplace struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func listJSON[T any](c *ClaudeInstaller, args ...string) ([]T, error) {
	out, err := c.CLI.ExecuteCommandOutput(args...)
	if err != nil {
		return nil, err
	}
	var items []T
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		return nil, fmt.Errorf("could not read the output of `claude %s`: %w", strings.Join(args, " "), err)
	}
	return items, nil
}

func (c *ClaudeInstaller) plugins() ([]claudePlugin, error) {
	return listJSON[claudePlugin](c, "plugin", "list", "--json")
}

func pluginID(pack string) string { return pack + "@" + constants.AgentPackMarketplace }

// Conflicts lists the commands that remove another copy of the same upstream:
// the plugin installed from a different marketplace, such as
// claude-plugins-official. devgeta never runs them (ADR-0070 §5). When the
// list can't be read, no conflict is reported rather than invented; the
// install's own calls will surface a real problem.
func (c *ClaudeInstaller) Conflicts(pack Manifest) []string {
	if pack.Embedded() {
		return nil
	}
	installed, err := c.plugins()
	if err != nil {
		logger.L().Warnw("could not check Claude Code for another copy", "pack", pack.Name, "err", err)
		return nil
	}
	var out []string
	for _, p := range installed {
		if strings.HasPrefix(p.ID, pack.Name+"@") && p.ID != pluginID(pack.Name) {
			scope := p.Scope
			if scope == "" {
				scope = "user"
			}
			out = append(out, fmt.Sprintf("claude plugin uninstall %s -s %s", p.ID, scope))
		}
	}
	sort.Strings(out)
	return out
}

// Install installs the tree as a plugin of the devgeta marketplace. The devgeta
// pack, which also carries instructions a plugin cannot (a CLAUDE.md), gets
// DEVGETA.md copied and its import line added after the plugin is in; if that
// fails, the plugin is undone too, so nothing is left half-installed.
func (c *ClaudeInstaller) Install(tree Tree) (Record, error) {
	rec, added, err := c.installPlugin(tree)
	if err != nil {
		return Record{}, err
	}
	if !tree.Pack.Embedded() {
		return rec, nil
	}
	_, statErr := os.Stat(c.instructionsPath())
	hadInstructions := statErr == nil
	instructions, err := c.installInstructions(tree)
	if err != nil {
		// Undo only what this run added. The engine never rolls back a coder
		// that already had the pack, so a failed update must leave the earlier
		// install as it was, not tear it down under saved state.
		c.undoPlugin(rec.Plugin, !added.pluginExisted, added.marketplace)
		// DEVGETA.md is written before the import line, so it may be there.
		if !hadInstructions {
			if rmErr := os.Remove(c.instructionsPath()); rmErr != nil && !os.IsNotExist(rmErr) {
				logger.L().Warnw("could not undo DEVGETA.md", "path", c.instructionsPath(), "err", rmErr)
			}
		}
		return Record{}, err
	}
	instructions.Marketplace, instructions.Plugin = rec.Marketplace, rec.Plugin
	return instructions, nil
}

// installPlugin lists the tree in the devgeta marketplace, registers the
// marketplace if needed, and installs the plugin at user scope. An already
// installed plugin is uninstalled first: `claude plugin update` leaves
// Claude's cache copy stale, and a reinstall refreshes it.
//
// On failure it undoes what this call did, so nothing is left registered: the
// plugin if it got that far, and the marketplace only if this call added it
// (another pack may be using one that was already there). It reports what was
// already there, so a caller that fails later undoes only its own work.
func (c *ClaudeInstaller) installPlugin(tree Tree) (Record, pluginPrior, error) {
	name := tree.Pack.Name
	if info, err := os.Stat(filepath.Join(tree.Dir, ".claude-plugin", "plugin.json")); err != nil || !info.Mode().IsRegular() {
		return Record{}, pluginPrior{}, fmt.Errorf("pack %s has no .claude-plugin/plugin.json, so Claude Code has nothing to load", name)
	}
	if err := c.writeMarketplace(""); err != nil {
		return Record{}, pluginPrior{}, err
	}
	addedMarketplace, err := c.ensureMarketplace()
	if err != nil {
		return Record{}, pluginPrior{}, err
	}
	prior := pluginPrior{marketplace: addedMarketplace}
	id := pluginID(name)
	fail := func(cause error) (Record, pluginPrior, error) {
		c.undoPlugin(id, true, addedMarketplace)
		return Record{}, pluginPrior{}, fmt.Errorf(
			"installing %s into Claude Code failed: %w\nRe-run: dg agent-pack install %s --coder claude",
			name, cause, name,
		)
	}

	installed, err := c.plugins()
	if err != nil {
		return fail(err)
	}
	for _, p := range installed {
		if p.ID == id {
			prior.pluginExisted = true
			if err := c.CLI.ExecuteCommand("plugin", "uninstall", id, "-s", scopeOrUser(p.Scope)); err != nil {
				return fail(err)
			}
		}
	}
	if err := c.CLI.ExecuteCommand("plugin", "install", id, "-s", "user"); err != nil {
		return fail(err)
	}
	return Record{Marketplace: constants.AgentPackMarketplace, Plugin: id}, prior, nil
}

// pluginPrior records what installPlugin found, so an undo removes only what
// the call itself added: the marketplace if it registered it, and the plugin
// unless it was already installed (an update).
type pluginPrior struct {
	pluginExisted bool
	marketplace   bool // this call added the marketplace
}

// undoPlugin uninstalls the plugin id when removePlugin is set, and removes the
// marketplace when removeMarketplace is set. Failures are logged: the caller
// is already reporting the error that made it undo.
func (c *ClaudeInstaller) undoPlugin(id string, removePlugin, removeMarketplace bool) {
	if installed, err := c.plugins(); removePlugin && err == nil {
		for _, p := range installed {
			if p.ID == id {
				if uerr := c.CLI.ExecuteCommand("plugin", "uninstall", id, "-s", scopeOrUser(p.Scope)); uerr != nil {
					logger.L().Warnw("could not undo a failed plugin install", "plugin", id, "err", uerr)
				}
			}
		}
	}
	if removeMarketplace {
		if merr := c.CLI.ExecuteCommand("plugin", "marketplace", "remove", constants.AgentPackMarketplace); merr != nil {
			logger.L().Warnw("could not undo the marketplace registration", "err", merr)
		}
	}
}

func scopeOrUser(scope string) string {
	if scope == "" {
		return "user"
	}
	return scope
}

// ensureMarketplace registers the devgeta marketplace at PacksRoot, replacing
// a registration that points somewhere else (the data dir moved). It reports
// whether this call added one.
func (c *ClaudeInstaller) ensureMarketplace() (bool, error) {
	existing, err := listJSON[claudeMarketplace](c, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return false, fmt.Errorf("could not list Claude Code's marketplaces: %w", err)
	}
	for _, m := range existing {
		if m.Name != constants.AgentPackMarketplace {
			continue
		}
		if samePath(m.Path, c.PacksRoot) {
			return false, nil
		}
		if err := c.CLI.ExecuteCommand("plugin", "marketplace", "remove", m.Name); err != nil {
			return false, fmt.Errorf("could not replace the stale %s marketplace: %w", m.Name, err)
		}
	}
	if err := c.CLI.ExecuteCommand("plugin", "marketplace", "add", c.PacksRoot); err != nil {
		return false, fmt.Errorf("could not add the %s marketplace: %w", constants.AgentPackMarketplace, err)
	}
	return true, nil
}

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// Uninstall removes the recorded plugin, then rewrites the marketplace file
// without it: the engine deletes the pack's tree next, and a marketplace that
// still named it would point at a folder that is gone. A plugin the user
// already removed is not an error. The devgeta pack's record holds instructions
// (DEVGETA.md and its CLAUDE.md line) and the plugin; a record from before the
// pack shipped skills holds only the instructions.
func (c *ClaudeInstaller) Uninstall(r Record) error {
	if r.InstructionsFile != "" {
		if err := c.uninstallInstructions(r); err != nil {
			return err
		}
	}
	if r.Plugin == "" {
		return nil
	}
	installed, err := c.plugins()
	if err != nil {
		return fmt.Errorf("could not check whether %s is installed: %w", r.Plugin, err)
	}
	for _, p := range installed {
		if p.ID == r.Plugin {
			if err := c.CLI.ExecuteCommand("plugin", "uninstall", r.Plugin, "-s", scopeOrUser(p.Scope)); err != nil {
				return fmt.Errorf("could not uninstall %s from Claude Code: %w", r.Plugin, err)
			}
		}
	}
	pack, _, _ := strings.Cut(r.Plugin, "@")
	return c.writeMarketplace(pack)
}

// Finish removes the devgeta marketplace and its file once no pack is left in
// Claude Code (ADR-0070 §4).
func (c *ClaudeInstaller) Finish() error {
	existing, err := listJSON[claudeMarketplace](c, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return fmt.Errorf("could not list Claude Code's marketplaces: %w", err)
	}
	for _, m := range existing {
		if m.Name == constants.AgentPackMarketplace {
			if err := c.CLI.ExecuteCommand("plugin", "marketplace", "remove", m.Name); err != nil {
				return fmt.Errorf("could not remove the %s marketplace: %w", m.Name, err)
			}
		}
	}
	return os.RemoveAll(filepath.Join(c.PacksRoot, ".claude-plugin"))
}

// writeMarketplace writes <PacksRoot>/.claude-plugin/marketplace.json listing
// every built pack that is a Claude Code plugin, by relative path. Hidden
// folders (the clone cache, a build in progress) and packs without a plugin
// manifest are not listed; skip names a pack being removed.
func (c *ClaudeInstaller) writeMarketplace(skip string) error {
	entries, err := os.ReadDir(c.PacksRoot)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", c.PacksRoot, err)
	}
	type plugin struct {
		Name        string `json:"name"`
		Source      string `json:"source"`
		Description string `json:"description"`
	}
	list := []plugin{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == skip {
			continue
		}
		manifest := filepath.Join(c.PacksRoot, e.Name(), ".claude-plugin", "plugin.json")
		if info, err := os.Stat(manifest); err != nil || !info.Mode().IsRegular() {
			continue
		}
		list = append(list, plugin{
			Name:        e.Name(),
			Source:      "./" + e.Name(),
			Description: "devgeta agent pack: " + e.Name(),
		})
	}
	data, err := json.MarshalIndent(map[string]any{
		"name":    constants.AgentPackMarketplace,
		"owner":   map[string]string{"name": constants.AgentPackMarketplace},
		"plugins": list,
	}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(c.PacksRoot, ".claude-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", dir, err)
	}
	return files.WriteFileAtomic(filepath.Join(dir, "marketplace.json"), append(data, '\n'), 0o644)
}

func (c *ClaudeInstaller) claudeMD() string { return filepath.Join(c.ConfigDir, "CLAUDE.md") }

func (c *ClaudeInstaller) instructionsPath() string {
	return filepath.Join(c.ConfigDir, instructionsFile)
}

func (c *ClaudeInstaller) note(format string, args ...any) {
	c.notes = append(c.notes, fmt.Sprintf(format, args...))
}

// TakeNotes returns what the installer wants the user to know since the last
// call, and clears it.
func (c *ClaudeInstaller) TakeNotes() []string {
	out := c.notes
	c.notes = nil
	return out
}

// installInstructions is the devgeta pack's Claude Code side. A plugin cannot
// carry a CLAUDE.md, so the pack's DEVGETA.md is copied to ~/.claude/DEVGETA.md
// and an @DEVGETA.md import is added to ~/.claude/CLAUDE.md unless a line
// already imports it (ADR-0058, ADR-0068 §7). The copy is byte for byte what
// the other coders load.
func (c *ClaudeInstaller) installInstructions(tree Tree) (Record, error) {
	data, err := os.ReadFile(filepath.Join(tree.Dir, instructionsFile))
	if err != nil {
		return Record{}, fmt.Errorf("pack %s has no %s: %w", tree.Pack.Name, instructionsFile, err)
	}
	if err := os.MkdirAll(c.ConfigDir, 0o755); err != nil {
		return Record{}, fmt.Errorf("could not create %s: %w", c.ConfigDir, err)
	}
	if err := files.WriteFileAtomic(c.instructionsPath(), data, 0o644); err != nil {
		return Record{}, fmt.Errorf("could not write %s: %w", c.instructionsPath(), err)
	}
	added, err := addImportLine(c.claudeMD())
	if err != nil {
		return Record{}, err
	}
	if added {
		c.note("Added %s to %s", instructionsImport, c.claudeMD())
	} else {
		c.note("%s already imports %s, so devgeta left it as it is", c.claudeMD(), instructionsFile)
	}
	return Record{InstructionsFile: c.instructionsPath(), ClaudeMDLineAdded: added}, nil
}

// uninstallInstructions removes the DEVGETA.md devgeta wrote, and the
// @DEVGETA.md line only when devgeta added it and it is still exactly
// devgeta's line (ADR-0070 §3). Each case says what it did.
//
// The path comes from state, which a person can edit, so only the one file
// devgeta writes is ever deleted.
func (c *ClaudeInstaller) uninstallInstructions(r Record) error {
	if filepath.Clean(r.InstructionsFile) != filepath.Clean(c.instructionsPath()) {
		return fmt.Errorf(
			"refusing to delete %s: devgeta only ever deletes %s", r.InstructionsFile, c.instructionsPath(),
		)
	}
	if err := os.Remove(c.instructionsPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not delete %s: %w", c.instructionsPath(), err)
	}
	if !r.ClaudeMDLineAdded {
		if data, err := os.ReadFile(c.claudeMD()); err == nil && hasImport(string(data)) {
			c.note("Left %s alone: devgeta did not add its %s line", c.claudeMD(), instructionsImport)
		}
		return nil
	}
	removed, err := removeImportLine(c.claudeMD())
	if err != nil {
		return err
	}
	if removed {
		c.note("Removed the %s line from %s", instructionsImport, c.claudeMD())
		return nil
	}
	if data, err := os.ReadFile(c.claudeMD()); err == nil && hasImport(string(data)) {
		c.note(
			"Left the %s line in %s: it is no longer exactly the line devgeta added",
			instructionsImport, c.claudeMD(),
		)
	}
	return nil
}

// MergeRecord keeps "devgeta added the @DEVGETA.md line" across a reinstall.
// A second install finds the line already there and records that it added
// nothing, which would stop uninstall from ever removing a line devgeta did add.
// A plugin record has nothing to carry.
func (c *ClaudeInstaller) MergeRecord(prior, next Record) Record {
	if next.InstructionsFile != "" && prior.ClaudeMDLineAdded {
		next.ClaudeMDLineAdded = true
	}
	return next
}

// Inspect runs `claude plugin validate` on the built tree, which is what Claude
// Code would load. A machine without Claude Code has nothing to validate. A
// validation error fails the check: it is exactly the kind of upstream change
// `check` exists to catch.
func (c *ClaudeInstaller) Inspect(tree Tree) Inspection {
	if !c.Available() {
		return Inspection{}
	}
	if _, err := c.CLI.ExecuteCommandOutput("plugin", "validate", tree.Dir); err != nil {
		return Inspection{Err: fmt.Errorf("claude plugin validate rejected %s: %w", tree.Pack.Name, err)}
	}
	return Inspection{Lines: []string{"claude plugin validate: passed"}}
}
