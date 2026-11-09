package baseapp

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/apps"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
)

// Reinstall implements the "force reinstall" flow correctly:
// if uninstall is supported, run it then install; if not, just install.
// This replaces the previously-broken pattern that failed whenever
// Uninstall returned ErrUninstallNotSupported.
func Reinstall(install, uninstall func() error) error {
	if err := uninstall(); err != nil && !errors.Is(err, apps.ErrUninstallNotSupported) {
		return err
	}
	return install()
}

// SoftInstall runs install only when the app is not already there, for apps
// whose official installer owns a directory of its own.
//
// managedPresent is the app's own proof that its installer already ran — a
// marker file, or the binary the script writes — and may be nil when the app
// has none. It is checked before PATH, and the two checks are not redundant: a
// fresh managed install's bin dir is not on the running process's PATH (nor on
// the user's, until they open a new shell), so a PATH-only check re-runs the
// installer on every soft install; and an app the user installed by other means
// (npm, brew, their own build) must never get a second copy laid over it.
func SoftInstall(managedPresent func() bool, binary string, install func() error) error {
	if managedPresent != nil && managedPresent() {
		return nil
	}
	if _, err := cmd.LookPathFn(binary); err == nil {
		return nil
	}
	return install()
}

// ForgetAgentPackCoder is the part of a coder's uninstall that follows its
// agent packs (ADR-0070 §6): the coder's config directory is gone, so its
// pack records go too, and a pack no coder has left loses its built tree.
// gc is changed in memory; the caller's own Save persists it.
func ForgetAgentPackCoder(gc *config.GlobalConfig, coder string) {
	for _, pack := range gc.ForgetAgentPackCoder(coder) {
		tree := filepath.Join(paths.Paths.App.AgentPacks, pack)
		// A built tree is derived data (the clone cache can rebuild it), so a
		// failed delete is worth a warning but must not fail the uninstall.
		if err := os.RemoveAll(tree); err != nil {
			logger.L().Warnw("could not delete a built agent pack", "tree", tree, "err", err)
		}
	}
}
