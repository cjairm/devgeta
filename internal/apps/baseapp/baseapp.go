package baseapp

import (
	"errors"

	"github.com/cjairm/devgeta/internal/apps"
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
