package agentpack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
)

// PiApp is the part of the Pi app wrapper the installer uses. The real *pi.Pi
// satisfies it.
type PiApp interface {
	IsInstalled() bool
	// PackageSources lists every source in Pi's settings, with local paths
	// resolved to absolute (Pi stores them relative to its agent dir).
	PackageSources() ([]string, error)
	ExecuteCommand(args ...string) error
	ExecuteCommandOutput(args ...string) (string, error)
}

// PiInstaller installs a built tree as a Pi package through `pi install <path>`
// and removes it with `pi remove <path>`: Pi's own package manager, which is
// what records it in Pi's settings (ADR-0060 §7). Unlike the curated
// extensions, which devgeta leaves in Pi on uninstall, a pack was installed for
// a specific purpose and tracked, so devgeta removes it.
type PiInstaller struct {
	App PiApp
	// PacksRoot is paths.Paths.App.AgentPacks; a pack's tree is PacksRoot/<pack>.
	PacksRoot string
}

var (
	_ Installer = (*PiInstaller)(nil)
	_ Inspector = (*PiInstaller)(nil)
	_ Verifier  = (*PiInstaller)(nil)
)

// Name is the coder's name.
func (p *PiInstaller) Name() string { return constants.Pi }

// Available reports whether pi is installed.
func (p *PiInstaller) Available() bool { return p.App.IsInstalled() }

// Skipped is empty: a Pi package loads the pack's extension and skills.
func (p *PiInstaller) Skipped(Tree) []string { return nil }

// hasSource reports whether Pi already lists the path as a package.
func hasSource(sources []string, path string) bool {
	clean := filepath.Clean(path)
	for _, s := range sources {
		if s == clean {
			return true
		}
	}
	return false
}

// Install runs `pi install <tree>` unless Pi already lists the tree, then reads
// Pi's settings back to confirm it was recorded. A local package is referenced
// in place, so an already-listed tree has nothing to refresh. On failure it
// removes a package Pi managed to record before failing, so nothing is left.
func (p *PiInstaller) Install(tree Tree) (Record, error) {
	name := tree.Pack.Name
	if info, err := os.Stat(filepath.Join(tree.Dir, "package.json")); err != nil || !info.Mode().IsRegular() {
		return Record{}, fmt.Errorf("pack %s has no package.json, so Pi has no package to install", name)
	}
	before, err := p.App.PackageSources()
	if err != nil {
		return Record{}, fmt.Errorf("could not read Pi's packages before installing %s: %w", name, err)
	}
	if hasSource(before, tree.Dir) {
		return Record{PiPackage: tree.Dir}, nil
	}
	fail := func(cause error) (Record, error) {
		if after, rerr := p.App.PackageSources(); rerr == nil && hasSource(after, tree.Dir) {
			if uerr := p.App.ExecuteCommand("remove", tree.Dir); uerr != nil {
				logger.L().Warnw("could not undo a failed Pi install", "package", tree.Dir, "err", uerr)
			}
		}
		return Record{}, fmt.Errorf(
			"installing %s into Pi failed: %w\nRe-run: dg agent-pack install %s --coder pi", name, cause, name,
		)
	}
	if err := p.App.ExecuteCommand("install", tree.Dir); err != nil {
		return fail(err)
	}
	after, err := p.App.PackageSources()
	if err != nil {
		return fail(err)
	}
	if !hasSource(after, tree.Dir) {
		return fail(fmt.Errorf("`pi install` finished but Pi's settings do not list %s; check `pi list`", tree.Dir))
	}
	return Record{PiPackage: tree.Dir}, nil
}

// Uninstall runs `pi remove` for the recorded package, and only if Pi still
// lists it: one the user already removed is not an error. Pi's settings must be
// readable, or it could not tell, and claiming success would hide that.
func (p *PiInstaller) Uninstall(r Record) error {
	if r.PiPackage == "" {
		return nil
	}
	sources, err := p.App.PackageSources()
	if err != nil {
		return fmt.Errorf("could not check whether Pi still lists %s: %w", r.PiPackage, err)
	}
	if !hasSource(sources, r.PiPackage) {
		return nil
	}
	if err := p.App.ExecuteCommand("remove", r.PiPackage); err != nil {
		return fmt.Errorf("could not remove %s from Pi: %w", r.PiPackage, err)
	}
	return nil
}

// Conflicts lists the commands that remove another copy of the same upstream:
// a git: or npm: package, or the user's own checkout. devgeta's own tree for
// the pack is not one. devgeta never runs them (ADR-0070 §5). When Pi's
// settings can't be read, no conflict is reported rather than invented.
func (p *PiInstaller) Conflicts(pack Manifest) []string {
	if pack.Embedded() {
		return nil
	}
	sources, err := p.App.PackageSources()
	if err != nil {
		logger.L().Warnw("could not check Pi for another copy", "pack", pack.Name, "err", err)
		return nil
	}
	ours := filepath.Join(p.PacksRoot, pack.Name)
	var out []string
	for _, s := range sources {
		if s == filepath.Clean(ours) || !sourceNamesPack(s, pack) {
			continue
		}
		out = append(out, "pi remove "+s)
	}
	return out
}

// Inspect reports the Pi version the check ran against next to the one devgeta's
// Pi support was last checked against: Pi is not pinned, so a Pi release is the
// likeliest thing to break this side. Failing to read it is a note, not a
// failed check, and a machine without Pi has nothing to report.
func (p *PiInstaller) Inspect(Tree) Inspection {
	if !p.Available() {
		return Inspection{}
	}
	out, err := p.App.ExecuteCommandOutput("--version")
	if err != nil {
		return Inspection{Lines: []string{fmt.Sprintf("pi: could not read its version: %v", err)}}
	}
	return Inspection{Lines: []string{fmt.Sprintf(
		"pi %s (devgeta's Pi support was last checked against %s)",
		strings.TrimSpace(out), constants.PinnedVersion.Pi,
	)}}
}

// Verify compares the record with Pi's settings, which devgeta does not render
// and so cannot assume still match (ADR-0070). It says what differs and the
// command that puts it right.
func (p *PiInstaller) Verify(pack string, r Record) string {
	if r.PiPackage == "" {
		return ""
	}
	sources, err := p.App.PackageSources()
	if err != nil {
		return fmt.Sprintf("could not read Pi's settings to check %s: %v", r.PiPackage, err)
	}
	if hasSource(sources, r.PiPackage) {
		return ""
	}
	return fmt.Sprintf(
		"Pi's settings no longer list %s; run: dg agent-pack install %s --coder pi", r.PiPackage, pack,
	)
}
