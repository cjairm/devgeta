package fonts

import (
	"errors"
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
)

var _ apps.FontInstaller = (*Fonts)(nil)

type Fonts struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (f *Fonts) Name() string       { return constants.Fonts }
func (f *Fonts) Kind() apps.AppKind { return apps.KindFont }

func New() *Fonts {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Fonts{Cmd: osCmd, Base: baseCmd}
}

func (f *Fonts) InstallFont(name string) error {
	return f.Cmd.InstallDesktopApp(name)
}

func (f *Fonts) ForceInstallFont(name string) error {
	if err := f.UninstallFont(name); err != nil && !errors.Is(err, apps.ErrUninstallNotSupported) {
		return fmt.Errorf("failed to uninstall font %s: %w", name, err)
	}
	return f.InstallFont(name)
}

func (f *Fonts) SoftInstallFont(name string) error {
	if f.Base.IsMac() {
		return f.Cmd.MaybeInstallFont("", name, false)
	}

	fc := constants.GetFontConfigByPackageName(name)
	if fc == nil {
		logger.L().Warnw("No Debian font config found, skipping", "font", name)
		return nil
	}

	fontURL := constants.GetNerdFontURL(fc.ArchiveName)
	return f.Cmd.MaybeInstallFont(fontURL, fc.InstallName, true)
}

func (f *Fonts) UninstallFont(_ string) error {
	return fmt.Errorf("%w for fonts", apps.ErrUninstallNotSupported)
}

func (f *Fonts) Available() []string {
	fontConfigs := constants.GetFontConfigs()
	names := make([]string, len(fontConfigs))
	for i, fc := range fontConfigs {
		names[i] = fc.PackageName
	}
	return names
}

func (f *Fonts) SoftInstallAll() {
	availableFonts := f.Available()
	for _, font := range availableFonts {
		if err := f.SoftInstallFont(font); err != nil {
			logger.L().Warnw("Font installation failed, continuing", "font", font, "error", err)
		}
	}
}
