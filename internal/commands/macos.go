package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	// Drop the cached listing on the attempted mutation, not only a
	// successful one — a failed `brew uninstall` can still have changed
	// package state. See ADR-0029 and InvalidateInstalledPackageCache's doc.
	InvalidateInstalledPackageCache()
	_, _, err := m.ExecCommand(CommandParams{Command: "brew", Args: []string{"uninstall", pkg}})
	return err
}

func (m *MacOSCommand) UninstallDesktopApp(pkg string) error {
	InvalidateInstalledPackageCache()
	_, _, err := m.ExecCommand(
		CommandParams{Command: "brew", Args: []string{"uninstall", "--cask", pkg}},
	)
	return err
}

func (m *MacOSCommand) InstallPackage(packageName string) error {
	InvalidateInstalledPackageCache()
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
	InvalidateInstalledPackageCache()
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

// HomebrewPrefixes is where addHomebrewToPath looks for a `brew` that is
// installed but not on PATH. Exported and a variable for the same reason
// CommandFn is: tests point it at a temp directory instead of the real
// /opt/homebrew, which exists on most macOS dev machines.
var HomebrewPrefixes = constants.HomebrewPrefixes

// addHomebrewToPath makes an installed Homebrew reachable from this process,
// reporting whether `brew` resolves afterwards.
//
// Homebrew installs into /opt/homebrew (Apple Silicon) or /usr/local (Intel)
// and leaves putting that directory on PATH to the user's shell profile —
// advice its installer prints at the end and that only the *next* shell acts
// on. devgeta never re-reads a profile mid-run, so on a fresh machine every
// `brew` call after the install would fail with exit 127 exactly as it did
// before Homebrew existed. Prepending the directory here is what lets
// `dg install` keep going in the same run that installed the package manager.
func addHomebrewToPath() bool {
	if _, err := LookPathFn("brew"); err == nil {
		return true
	}
	for _, prefix := range HomebrewPrefixes {
		binDir := filepath.Join(prefix, "bin")
		info, err := os.Stat(filepath.Join(binDir, "brew"))
		if err != nil || info.IsDir() {
			continue
		}
		newPath := binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		if err := os.Setenv("PATH", newPath); err != nil {
			logger.L().Debugw("could not add Homebrew to PATH", "dir", binDir, "error", err)
			continue
		}
		logger.L().Debugw("added Homebrew to this run's PATH", "dir", binDir)
		return true
	}
	return false
}

func (m *MacOSCommand) IsPackageManagerInstalled() bool {
	// Do the PATH repair before the probe, not only after an install: a
	// Homebrew that this process cannot see is indistinguishable from a
	// missing one here, and answering "missing" sends `dg install` off to
	// re-run the installer over a working Homebrew.
	if !addHomebrewToPath() {
		return false
	}
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

// homebrewInstallURL is Homebrew's official installer script.
const homebrewInstallURL = "https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh"

// homebrewInstallScript is the shell devgeta runs to install Homebrew.
//
// Homebrew documents the one-liner `/bin/bash -c "$(curl -fsSL <url>)"`, and
// both halves of that — the command substitution and the quotes keeping its
// output a single word — are the work of the shell you type it into. devgeta
// has no such shell: ExecCommand runs /bin/bash directly, so handing
// `$(curl …)` over as the -c argument made *that* bash expand the
// substitution itself, split the downloaded script on whitespace, and try to
// execute its shebang line as a command:
//
//	/bin/bash: #!/bin/bash: No such file or directory   (exit 127)
//
// The outer shell below is the one the documented recipe assumes. It also
// turns a failed or empty download into an error: an empty substitution runs
// an empty script and exits 0, which would have reported Homebrew installed
// and then failed on the next `brew` call instead.
const homebrewInstallScript = `set -u
script="$(curl -fsSL ` + homebrewInstallURL + `)" || exit 1
if [ -z "$script" ]; then
  echo "downloaded an empty installer from ` + homebrewInstallURL + `" >&2
  exit 1
fi
exec /bin/bash -c "$script"`

func (m *MacOSCommand) InstallPackageManager() error {
	logger.L().Debugw("installing Homebrew", "url", homebrewInstallURL)
	cmd := CommandParams{
		PreExecMsg:  "Installing Homebrew",
		PostExecMsg: "Homebrew installed ✔",
		IsSudo:      false,
		Command:     "/bin/bash",
		Args:        []string{"-c", homebrewInstallScript},
		// Homebrew's installer is interactive: it lists what it will do, waits
		// for RETURN, and asks for a sudo password. Without streaming, all of
		// that goes to the debug log and the user watches a silent hang.
		Stream: true,
	}
	if _, _, err := m.ExecCommand(cmd); err != nil {
		return fmt.Errorf("failed to install Homebrew: %w", err)
	}
	if !addHomebrewToPath() {
		return fmt.Errorf(
			"homebrew installer finished but no `brew` command was found under %s — open a new terminal and run `dg install` again",
			strings.Join(HomebrewPrefixes, " or "),
		)
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

// IsPackageInstalled answers from the process-wide cached `brew list`
// listing (ADR-0029), populating it on first use rather than running `brew
// list` again for every package probed. Do not add a targeted
// `brew list <packageName>` path here or as a fallback — the ADR measured
// it 6x slower than listing everything, because it resolves the formula
// rather than reading the Cellar directory.
func (m *MacOSCommand) IsPackageInstalled(packageName string) (bool, error) {
	lines, err := listingFrom(&installedListingCache.macFormulae, func() *exec.Cmd {
		logger.L().Debug("executing: brew list")
		return CommandFn("brew", "list")
	})
	if err != nil {
		return false, err
	}
	return findPackageInBrewOutput(lines, packageName), nil
}

// IsDesktopAppInstalled answers from the process-wide cached
// `brew list --cask` listing (ADR-0029) — see IsPackageInstalled's doc.
func (m *MacOSCommand) IsDesktopAppInstalled(desktopAppName string) (bool, error) {
	lines, err := listingFrom(&installedListingCache.macCasks, func() *exec.Cmd {
		logger.L().Debug("executing: brew list --cask")
		return CommandFn("brew", "list", "--cask")
	})
	if err != nil {
		return false, err
	}
	return findPackageInBrewOutput(lines, desktopAppName), nil
}
