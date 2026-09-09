package apps

import "errors"

var (
	ErrUninstallNotSupported = errors.New("uninstall not supported")
	ErrUpdateNotSupported    = errors.New("update not supported")
	ErrConfigureNotSupported = errors.New("configure not supported")
	ErrExecuteNotSupported   = errors.New("execute not supported")
	// ErrUnsupportedPlatform is returned by an app whose install route only
	// exists on one platform (e.g. Shottr, a macOS-only screenshot tool) when
	// called on any other. The desktop coordinator's chooser already filters
	// this app out of the prompt by availability before it ever gets called,
	// but a direct caller (--only, or a future code path bypassing the
	// chooser) must still fail clearly instead of attempting a doomed
	// package-manager install.
	ErrUnsupportedPlatform = errors.New("not supported on this platform")
)
