package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cjairm/devgeta/pkg/apt"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/cjairm/devgeta/pkg/utils"
)

type DebianCommand struct {
	BaseCommand
}

func (d *DebianCommand) MaybeInstallPackage(packageName string, alias ...string) error {
	return d.MaybeInstall(
		packageName,
		alias,
		d.IsPackageInstalled,
		d.InstallPackage,
		nil,
		"package",
	)
}

func (d *DebianCommand) MaybeInstallDesktopApp(desktopAppName string, alias ...string) error {
	return d.MaybeInstall(desktopAppName, alias, func(name string) (bool, error) {
		return d.IsDesktopAppInstalled(name)
	}, d.InstallDesktopApp, nil, "desktop_app")
}

func (d *DebianCommand) MaybeInstallFont(
	url, fontFileName string,
	runCache bool,
	alias ...string,
) error {
	return d.MaybeInstall(fontFileName, alias, func(name string) (bool, error) {
		isInstalled, err := d.IsFontPresent(name)
		return isInstalled, err
	}, d.InstallPackage, func(name string) error {
		// Use NerdFontStrategy for tar.xz archives (GitHub Nerd Fonts releases)
		if url != "" && strings.HasSuffix(url, ".tar.xz") {
			strategy := &NerdFontStrategy{cmd: d, archiveURL: url}
			return strategy.Install(name)
		}
		return d.InstallFontFromURL(url, name, runCache)
	}, "font")
}

func (d *DebianCommand) UninstallPackage(pkg string) error {
	// Drop the cached listing on the attempted mutation, not only a
	// successful one — a failed `apt-get remove` can still have changed
	// package state. See ADR-0029 and InvalidateInstalledPackageCache's doc.
	// UninstallDesktopApp delegates here, so it gets the drop for free.
	InvalidateInstalledPackageCache()
	_, _, err := d.ExecCommand(CommandParams{
		Command: "apt-get",
		Args:    []string{"remove", "-y", pkg},
		IsSudo:  true,
	})
	return err
}

func (d *DebianCommand) UninstallDesktopApp(pkg string) error {
	return d.UninstallPackage(pkg)
}

func (d *DebianCommand) InstallPackage(packageName string) error {
	InvalidateInstalledPackageCache()
	// Use strategy pattern to select appropriate installation method
	strategy := d.getInstallationStrategy(packageName)
	return strategy.Install(packageName)
}

// getInstallationStrategy selects the appropriate installation strategy based on package name
func (d *DebianCommand) getInstallationStrategy(packageName string) InstallationStrategy {
	switch packageName {
	// PPA installations
	case constants.Mise:
		return &PPAStrategy{
			cmd: d,
			ppaConfig: apt.PPAConfig{
				Name:         "mise",
				KeyURL:       "https://mise.jdx.dev/gpg-key.pub",
				RepoURL:      "https://mise.jdx.dev/deb",
				Distribution: "stable",
				Component:    "main",
			},
		}
	case constants.Fastfetch:
		return &LaunchpadPPAStrategy{
			cmd:    d,
			ppaRef: "ppa:zhangsongcui3371/fastfetch",
		}

	// Git clone installations
	case constants.Powerlevel10k:
		homeDir, _ := os.UserHomeDir()
		return &GitCloneStrategy{
			cmd:         d,
			repoURL:     "https://github.com/romkatv/powerlevel10k.git",
			installPath: filepath.Join(homeDir, "powerlevel10k"),
		}

	// Install script installations
	case constants.OpenCode:
		return &InstallScriptStrategy{
			cmd:       d,
			scriptURL: "https://opencode.ai/install",
		}

	// Default: use apt strategy (handles library mappings automatically)
	default:
		return &AptStrategy{cmd: d}
	}
}

func (d *DebianCommand) InstallDesktopApp(packageName string) error {
	InvalidateInstalledPackageCache()
	err := d.installWithApt(packageName)
	if err == nil {
		return nil
	}
	return d.installWithSnap(packageName)
}

func (d *DebianCommand) IsPackageManagerInstalled() bool {
	logger.L().Debug("executing: which apt")
	_, err := exec.LookPath("apt")
	if err != nil {
		return false
	}
	return true
}

func (d *DebianCommand) MaybeInstallPackageManager() error {
	isInstalled := d.IsPackageManagerInstalled()
	if isInstalled {
		return nil
	}
	return d.InstallPackageManager()
}

func (d *DebianCommand) InstallPackageManager() error {
	logger.L().Info("APT is preinstalled on Debian/Ubuntu systems")
	// APT is preinstalled on Debian/Ubuntu systems.
	return nil
}

func (d *DebianCommand) ValidateOSVersion() error {
	utils.PrintSecondary("Getting Linux version")

	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return fmt.Errorf("failed to read OS release info: %w", err)
	}

	utils.PrintSecondary("Parsing OS version")

	var name, versionStr string
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ID=") {
			name = strings.Trim(strings.SplitN(line, "=", 2)[1], `"`)
		} else if strings.HasPrefix(line, "VERSION_ID=") {
			versionStr = strings.Trim(strings.SplitN(line, "=", 2)[1], `"`)
		}
	}
	logger.L().Debugw("OS version info", "name", name, "version", versionStr)

	if name == "" || versionStr == "" {
		err := fmt.Errorf("unable to parse OS version information")
		return err
	}

	utils.PrintSecondary("Extracting major and minor version")
	versionParts := strings.Split(versionStr, ".")
	if len(versionParts) < 1 {
		err := fmt.Errorf("invalid version format: %s", versionStr)
		return err
	}

	major, err := strconv.Atoi(versionParts[0])
	if err != nil {
		return fmt.Errorf("invalid major version: %w", err)
	}
	logger.L().Debugw("OS major version", "os", name, "major_version", major)

	// Check supported versions for Debian and Ubuntu
	switch name {
	case "debian":
		logger.L().
			Debugw("supported_debian_version", "supported_version", constants.SupportedVersion.Debian.Number)
		if major < constants.SupportedVersion.Debian.Number {
			err := fmt.Errorf(
				"OS requirement not met\nOS required: Debian %s (%d.0) or higher",
				constants.SupportedVersion.Debian.Name,
				constants.SupportedVersion.Debian.Number,
			)
			return err
		}
	case "ubuntu":
		logger.L().
			Debugw("supported_ubuntu_version", "supported_version", constants.SupportedVersion.Ubuntu.Number)
		if major < constants.SupportedVersion.Ubuntu.Number {
			err := fmt.Errorf(
				"OS requirement not met\nOS required: Ubuntu %s (%d.0) or higher",
				constants.SupportedVersion.Ubuntu.Name,
				constants.SupportedVersion.Ubuntu.Number,
			)
			return err
		}
	}

	utils.PrintSecondary(fmt.Sprintf("✅ %s %s is supported", name, versionStr))
	return nil
}

// IsPackageInstalled answers from the process-wide cached `dpkg -l` listing
// (ADR-0029), populating it on first use rather than running `dpkg -l` again
// for every package probed. Do not add a targeted per-package probe here or
// as a fallback — see MacOSCommand.IsPackageInstalled's doc for the
// measurement that rules it out.
func (d *DebianCommand) IsPackageInstalled(packageName string) (bool, error) {
	lines, err := listingFrom(&installedListingCache.debianDpkg, func() *exec.Cmd {
		logger.L().Debug("executing: dpkg -l")
		return CommandFn("dpkg", "-l")
	})
	if err != nil {
		return false, err
	}
	return findPackageInDpkgOutput(lines, packageName), nil
}

func (d *DebianCommand) IsDesktopAppInstalled(appName string) (bool, error) {
	for _, applicationsDir := range []string{paths.Paths.User.Applications, paths.Paths.System.Applications} {
		isInstalled, err := d.IsDesktopAppPresent(applicationsDir, appName)
		if err != nil {
			return false, err
		}
		if isInstalled {
			return true, nil
		}
	}
	return false, nil
}

func (d *DebianCommand) installWithApt(packageName string) error {
	logger.L().Debug(fmt.Sprintf("executing: apt install -y %s", packageName))
	cmd := CommandParams{
		PreExecMsg:  fmt.Sprintf("Installing %s...", strings.ToLower(packageName)),
		PostExecMsg: "",
		IsSudo:      true,
		Command:     "apt",
		Args:        []string{"install", "-y", packageName},
	}
	if _, _, err := d.ExecCommand(cmd); err != nil {
		return fmt.Errorf("failed to install %s: %w", packageName, err)
	}
	return nil
}

func (d *DebianCommand) installWithSnap(packageName string) error {
	// Check if snap is available before attempting installation
	if _, err := exec.LookPath("snap"); err != nil {
		logger.L().Warnw("Snap not installed, skipping", "package", packageName)
		return nil
	}

	logger.L().Debug(fmt.Sprintf("executing: snap install %s", packageName))
	cmd := CommandParams{
		PreExecMsg:  fmt.Sprintf("Installing %s via Snap...", strings.ToLower(packageName)),
		PostExecMsg: "",
		IsSudo:      true,
		Command:     "snap",
		Args:        []string{"install", packageName},
	}
	if _, _, err := d.ExecCommand(cmd); err != nil {
		return fmt.Errorf("failed to install %s via Snap: %w", packageName, err)
	}
	return nil
}
