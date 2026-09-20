package i3

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
)

var (
	_ apps.App              = (*I3)(nil)
	_ apps.ThemedConfigurer = (*I3)(nil)
)

type I3 struct {
	Cmd commands.Command
}

func (i *I3) Name() string       { return constants.I3 }
func (i *I3) Kind() apps.AppKind { return apps.KindDesktop }

func New() *I3 {
	return &I3{Cmd: commands.NewCommand()}
}

func (i *I3) Install() error {
	return i.Cmd.InstallPackage(constants.I3)
}

func (i *I3) SoftInstall() error {
	return i.Cmd.MaybeInstallPackage(constants.I3)
}

func (i *I3) ForceInstall() error {
	return baseapp.Reinstall(i.Install, i.Uninstall)
}

func (i *I3) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := i.Cmd.UninstallPackage(constants.I3); err != nil {
		return fmt.Errorf("failed to uninstall i3: %w", err)
	}
	_ = os.RemoveAll(paths.Paths.Config.I3)
	gc.RemoveFromInstalled(constants.I3, "package")
	return gc.Save()
}

// ForceConfigure resolves the theme for current_theme (falling back to
// theme.DefaultThemeName when it is empty) and renders with it. `dg theme
// set` does not go through here: it resolves the target theme itself and
// calls ForceConfigureTheme directly, because current_theme is deliberately
// not written yet at that point (docs/plans/cycles/2026-09-14-dg-theme.md
// Step 5) - reading it here would render the theme being replaced.
func (i *I3) ForceConfigure() error {
	def, err := theme.CurrentDefinition()
	if err != nil {
		return fmt.Errorf("failed to resolve current theme: %w", err)
	}
	return i.ForceConfigureTheme(def)
}

func (i *I3) ForceConfigureTheme(def theme.Definition) error {
	palette, err := def.PaletteFor(i.Name())
	if err != nil {
		return fmt.Errorf("failed to resolve theme palette: %w", err)
	}
	if err := files.GenerateFromTemplate(
		filepath.Join(paths.Paths.App.Configs.I3, "config.tmpl"),
		filepath.Join(paths.Paths.Config.I3, "config"),
		map[string]any{"Palette": palette},
	); err != nil {
		return fmt.Errorf("failed to generate i3 config: %w", err)
	}
	logger.L().
		Infow("i3 configuration applied", "source", paths.Paths.App.Configs.I3, "dest", paths.Paths.Config.I3)
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.I3, "package")
	return gc.Save()
}

func (i *I3) SoftConfigure() error {
	// Check for marker file (config) in i3 config directory
	markerFile := filepath.Join(paths.Paths.Config.I3, "config")
	if files.FileAlreadyExist(markerFile) {
		logger.L().Infow("i3 config already exists", "path", markerFile)
		return nil
	}
	return i.ForceConfigure()
}

func (i *I3) ExecuteCommand(args ...string) error {
	// i3 commands could be useful for window management automation
	// For now, return nil for interface compliance
	return nil
}

func (i *I3) Update() error {
	return fmt.Errorf("%w for i3 — use system package manager", apps.ErrUpdateNotSupported)
}
