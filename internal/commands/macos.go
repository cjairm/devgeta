package commands

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/cjairm/devgeta/pkg/utils"
)

type MacOSCommand struct {
	BaseCommand
}

func (m *MacOSCommand) MaybeInstallPackage(packageName string, alias ...string) error {
	return m.MaybeInstall(
		packageName,
		alias,
		m.IsPackageInstalled,
		m.InstallPackage,
		nil,
		"package",
	)
}

func (m *MacOSCommand) MaybeInstallDesktopApp(desktopAppName string, alias ...string) error {
	return m.MaybeInstall(desktopAppName, alias, func(name string) (bool, error) {
		isInstalled, err := m.IsDesktopAppInstalled(name)
		if !isInstalled {
			isInstalled, err = m.IsDesktopAppPresent(paths.Paths.User.Applications, name)
		}
		return isInstalled, err
	}, m.InstallDesktopApp, nil, "desktop_app")
}

func (m *MacOSCommand) MaybeInstallFont(
	url, fontFileName string,
	runCache bool,
	alias ...string,
) error {
	return m.MaybeInstall(fontFileName, alias, func(name string) (bool, error) {
		isInstalled, err := m.IsDesktopAppInstalled(name)
		if !isInstalled {
			isInstalled, err = m.IsFontPresent(name)
		}
		return isInstalled, err
	}, m.InstallDesktopApp, nil, "font")
}

func (m *MacOSCommand) UninstallPackage(pkg string) error {
	_, _, err := m.ExecCommand(CommandParams{Command: "brew", Args: []string{"uninstall", pkg}})
	return err
}

func (m *MacOSCommand) UninstallDesktopApp(pkg string) error {
	_, _, err := m.ExecCommand(CommandParams{Command: "brew", Args: []string{"uninstall", "--cask", pkg}})
	return err
}

func (m *MacOSCommand) InstallPackage(packageName string) error {
	logger.L().Debug(fmt.Sprintf("executing: brew install %s", packageName))
	cmd := CommandParams{
		PreExecMsg:  fmt.Sprintf("Installing %s...", strings.ToLower(packageName)),
		PostExecMsg: "",
		IsSudo:      false,
		Command:     "brew",
		Args:        []string{"install", packageName},
	}
	if _, _, err := m.ExecCommand(cmd); err != nil {
		return fmt.Errorf("failed to install package %s: %w", packageName, err)
	}
	return nil
}

func (m *MacOSCommand) InstallDesktopApp(packageName string) error {
	logger.L().Debug(fmt.Sprintf("executing: brew install --cask %s", packageName))
	cmd := CommandParams{
		PreExecMsg:  fmt.Sprintf("Installing %s...", strings.ToLower(packageName)),
		PostExecMsg: "",
		IsSudo:      false,
		Command:     "brew",
		Args:        []string{"install", "--cask", packageName},
	}
	if _, _, err := m.ExecCommand(cmd); err != nil {
		return fmt.Errorf("failed to install desktop app %s: %w", packageName, err)
	}
	return nil
}

func (m *MacOSCommand) IsPackageManagerInstalled() bool {
	logger.L().Debug("executing: brew --version")
	err := exec.Command("brew", "--version").Run()
	return err == nil
}

func (m *MacOSCommand) MaybeInstallPackageManager() error {
	isInstalled := m.IsPackageManagerInstalled()
	if isInstalled {
		return nil
	}
	return m.InstallPackageManager()
}

func (m *MacOSCommand) InstallPackageManager() error {
	logger.L().
		Debug("executing: /bin/bash -c $(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)")
	cmd := CommandParams{
		PreExecMsg:  "Installing Homebrew",
		PostExecMsg: "Homebrew installed ✔",
		IsSudo:      false,
		Command:     "/bin/bash",
		Args: []string{
			"-c",
			"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)",
		},
	}
	if _, _, err := m.ExecCommand(cmd); err != nil {
		return fmt.Errorf("failed to install Homebrew: %w", err)
	}
	return nil
}

func (m *MacOSCommand) ValidateOSVersion() error {
	utils.PrintSecondary("Getting macOS version")

	cmd := CommandParams{
		Command: "sw_vers",
		Args:    []string{"-productVersion"},
	}

	version, _, err := m.BaseCommand.ExecCommand(cmd)
	if err != nil {
		err := fmt.Errorf("unable to get macOS version")
		return err
	}

	utils.PrintSecondary("Parsing OS version")

	versionStr := strings.TrimSpace(version)
	versionParts := strings.Split(versionStr, ".")
	if len(versionParts) < 2 {
		err := fmt.Errorf("invalid macOS version format: %s", versionStr)
		return err
	}
	logger.L().Debugw("macOS version info", "version", versionStr)

	utils.PrintSecondary("Extracting major and minor version")
	major, err := strconv.Atoi(versionParts[0])
	if err != nil {
		return fmt.Errorf("invalid major version: %w", err)
	}
	minor, err := strconv.Atoi(versionParts[1])
	if err != nil {
		return fmt.Errorf("invalid minor version: %w", err)
	}
	logger.L().Debugw("macOS version", "major_version", major, "minor_version", minor)
	logger.L().
		Debugw("supported_macos_version", "supported_version", constants.SupportedVersion.MacOS.Number)
	if major < constants.SupportedVersion.MacOS.Number ||
		(major == constants.SupportedVersion.MacOS.Number && minor < 0) {
		err := fmt.Errorf(
			"OS requirement not met\nmacOS %s (%d.0) or higher required",
			constants.SupportedVersion.MacOS.Name,
			constants.SupportedVersion.MacOS.Number,
		)
		return err
	}

	utils.PrintSecondary(fmt.Sprintf("✅ macOS %s is supported", versionStr))
	return nil
}

func (m *MacOSCommand) IsPackageInstalled(packageName string) (bool, error) {
	logger.L().Debug("executing: brew list")
	cmd := exec.Command("brew", "list")
	return m.IsPackagePresent(cmd, packageName)
}

func (m *MacOSCommand) IsDesktopAppInstalled(desktopAppName string) (bool, error) {
	logger.L().Debug("executing: brew list --cask")
	cmd := exec.Command("brew", "list", "--cask")
	return m.IsPackagePresent(cmd, desktopAppName)
}
