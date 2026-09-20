// Package alacritty provides installation and configuration management for Alacritty terminal emulator.
// Alacritty is a fast, cross-platform terminal emulator written in Rust that uses GPU acceleration.
// This module follows the standardized devgeta app interface for consistent lifecycle management.

package alacritty

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

var (
	_ apps.App              = (*Alacritty)(nil)
	_ apps.ThemedConfigurer = (*Alacritty)(nil)
)

// alacrittyColors is alacritty.toml.tmpl's color data: theme.Palette's hex
// values reformatted to Alacritty's "0xRRGGBB" convention, since
// files.GenerateFromTemplate's templates get no custom functions. Alacritty
// has no dedicated "border" role, so its ANSI black/bright-black slot - which
// today renders "0x63605e", distinct from any other role in the palette -
// maps onto Palette.Border; magenta and cyan map onto Purple and Aqua, the
// closest named roles.
type alacrittyColors struct {
	Background string
	Foreground string
	Black      string
	Red        string
	Green      string
	Yellow     string
	Blue       string
	Magenta    string
	Cyan       string
}

func hex0x(hex string) string {
	return "0x" + strings.TrimPrefix(hex, "#")
}

func newAlacrittyColors(p theme.Palette) alacrittyColors {
	return alacrittyColors{
		Background: hex0x(p.Background),
		Foreground: hex0x(p.Foreground),
		Black:      hex0x(p.Border),
		Red:        hex0x(p.Red),
		Green:      hex0x(p.Green),
		Yellow:     hex0x(p.Yellow),
		Blue:       hex0x(p.Blue),
		Magenta:    hex0x(p.Purple),
		Cyan:       hex0x(p.Aqua),
	}
}

type Alacritty struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (a *Alacritty) Name() string       { return constants.Alacritty }
func (a *Alacritty) Kind() apps.AppKind { return apps.KindTerminal }

func New() *Alacritty {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Alacritty{Cmd: osCmd, Base: baseCmd}
}

func (a *Alacritty) Install() error {
	return a.Cmd.InstallDesktopApp(constants.Alacritty)
}

func (a *Alacritty) SoftInstall() error {
	return a.Cmd.MaybeInstallDesktopApp(constants.Alacritty)
}

func (a *Alacritty) ForceInstall() error {
	return baseapp.Reinstall(a.Install, a.Uninstall)
}

// ForceConfigure resolves the theme for current_theme (falling back to
// theme.DefaultThemeName when it is empty) and renders with it. `dg theme
// set` does not go through here: it resolves the target theme itself and
// calls ForceConfigureTheme directly, because current_theme is deliberately
// not written yet at that point (docs/plans/cycles/2026-09-14-dg-theme.md
// Step 5) - reading it here would render the theme being replaced.
func (a *Alacritty) ForceConfigure() error {
	def, err := theme.CurrentDefinition()
	if err != nil {
		return fmt.Errorf("failed to resolve current theme: %w", err)
	}
	return a.ForceConfigureTheme(def)
}

func (a *Alacritty) ForceConfigureTheme(def theme.Definition) error {
	p, err := def.PaletteFor(a.Name())
	if err != nil {
		return fmt.Errorf("failed to resolve theme palette: %w", err)
	}
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	font := "default"
	configFilePath := filepath.Join(
		paths.Paths.Config.Alacritty,
		fmt.Sprintf("%s.toml", constants.Alacritty),
	)
	tmplPath := filepath.Join(
		paths.Paths.App.Configs.Alacritty,
		fmt.Sprintf("%s.toml.tmpl", constants.Alacritty),
	)
	if err := files.GenerateFromTemplate(tmplPath, configFilePath, map[string]any{
		"Font":       font,
		"ConfigPath": paths.Paths.Config.Root,
		"Colors":     newAlacrittyColors(p),
	}); err != nil {
		return fmt.Errorf("failed to generate alacritty configuration: %w", err)
	}
	starterDst := filepath.Join(paths.Paths.Config.Alacritty, "starter.sh")
	if err := files.CopyFile(
		filepath.Join(paths.Paths.App.Configs.Terminal, "starter.sh"),
		starterDst,
	); err != nil {
		return fmt.Errorf("failed to copy alacritty starter script: %w", err)
	}
	// [terminal.shell] program in the template execs this script. Both the
	// embedded-config extractor and files.CopyFile write 0644, so it has to be
	// made executable here or Alacritty cannot launch it and comes up without
	// tmux. See files.CopyFile's doc for the established pattern.
	if err := os.Chmod(starterDst, 0o755); err != nil {
		return fmt.Errorf("failed to make alacritty starter script executable: %w", err)
	}
	gc.AddToInstalled(constants.Alacritty, "desktop_app")
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

// SoftConfigure deploys the config only when there isn't one already, so a
// hand-edited config is never overwritten.
//
// The guard is the config file itself, not the global config's installed
// tracking. Tracking is set by SoftInstall moments earlier — the desktop
// coordinator runs SoftInstall then SoftConfigure back to back — so reading it
// here would mean the config is never written on the one run that installed
// Alacritty, leaving a fresh install with default colors, no transparency and
// no tmux. This matches aerospace, tmux and neovim, which have always guarded
// on their own config file.
func (a *Alacritty) SoftConfigure() error {
	configFilePath := filepath.Join(
		paths.Paths.Config.Alacritty,
		fmt.Sprintf("%s.toml", constants.Alacritty),
	)
	if files.FileAlreadyExist(configFilePath) {
		return nil
	}
	if err := a.ForceConfigure(); err != nil {
		return fmt.Errorf("failed to configure alacritty: %w", err)
	}
	return nil
}

func (a *Alacritty) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := a.Cmd.UninstallDesktopApp(constants.Alacritty); err != nil {
		return fmt.Errorf("failed to uninstall alacritty: %w", err)
	}
	_ = os.RemoveAll(paths.Paths.Config.Alacritty)
	gc.RemoveFromInstalled(constants.Alacritty, "desktop_app")
	return gc.Save()
}

func (a *Alacritty) ExecuteCommand(args ...string) error {
	// No alacritty commands in terminal
	return nil
}

func (a *Alacritty) Update() error {
	return fmt.Errorf("%w for alacritty", apps.ErrUpdateNotSupported)
}
