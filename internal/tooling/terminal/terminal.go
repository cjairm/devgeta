package terminal

import (
	"fmt"
	"time"

	"github.com/cjairm/devgeta/internal/apps/claude"
	"github.com/cjairm/devgeta/internal/apps/fastfetch"
	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/lazydocker"
	"github.com/cjairm/devgeta/internal/apps/lazygit"
	"github.com/cjairm/devgeta/internal/apps/mise"
	"github.com/cjairm/devgeta/internal/apps/neovim"
	"github.com/cjairm/devgeta/internal/apps/opencode"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/autoconf"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/bison"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/fontconfig"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/gdbm"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/jemalloc"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/libffi"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/libyaml"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/mupdf"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/ncurses"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/openssl"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/pkgconfig"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/readline"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/unzip"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/vips"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/xcode"
	"github.com/cjairm/devgeta/internal/tooling/terminal/core/zlib"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/autosuggestions"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/bat"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/btop"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/curl"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/eza"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/fdfind"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/fzf"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/githubcli"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/jq"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/powerlevel10k"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/ripgrep"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/syntaxhighlighting"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/tldr"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/zoxide"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/promptui"
	"github.com/cjairm/devgeta/pkg/utils"
)

// InstallationStatus represents the status of a package installation
type InstallationStatus string

const (
	StatusSuccess InstallationStatus = "Success"
	StatusFailed  InstallationStatus = "Failed"
	StatusSkipped InstallationStatus = "Skipped"
)

// InstallationResult tracks the outcome of a single package installation attempt
type InstallationResult struct {
	PackageName  string             // Name of the package attempted
	Status       InstallationStatus // Success | Failed | Skipped
	ErrorMessage string             // Error details if failed (optional)
	Duration     time.Duration      // Time taken for installation
	Attempt      int                // Retry attempt number (1-based)
}

// InstallationSummary aggregates installation results for final report
type InstallationSummary struct {
	Installed int                  // Count of successfully installed packages
	Failed    int                  // Count of failed installations
	Skipped   int                  // Count of skipped installations (already present)
	Results   []InstallationResult // Detailed results for each package
}

// Total returns the total number of packages processed
func (s *InstallationSummary) Total() int {
	return s.Installed + s.Failed + s.Skipped
}

// FormatSummary returns a formatted summary string
func (s *InstallationSummary) FormatSummary() string {
	return fmt.Sprintf("Installed: %d, Failed: %d, Skipped: %d", s.Installed, s.Failed, s.Skipped)
}

// softInstallable is the subset of apps.App used by the terminal coordinator.
type softInstallable interface {
	SoftInstall() error
	SoftConfigure() error
}

// namedInstallable pairs an app name with its installer. Used for injection in tests.
type namedInstallable struct {
	name string
	app  softInstallable
}

type Terminal struct {
	Cmd  commands.Command
	Base commands.BaseCommand
	// appsOverride replaces the default terminal app list when non-nil (used in tests).
	appsOverride []namedInstallable
}

func New() *Terminal {
	osCmd := commands.NewCommand()
	baseCmd := commands.NewBaseCommand()
	return &Terminal{Cmd: osCmd, Base: *baseCmd}
}

// defaultApps returns the production list of registry apps managed by this coordinator.
func defaultApps() []namedInstallable {
	return []namedInstallable{
		{constants.Fastfetch, fastfetch.New()},
		{constants.Git, git.New()},
		{constants.Mise, mise.New()},
		{constants.Neovim, neovim.New()},
		{constants.Tmux, tmux.New()},
		{constants.OpenCode, opencode.New()},
		{constants.Claude, claude.New()},
		{constants.LazyDocker, lazydocker.New()},
		{constants.LazyGit, lazygit.New()},
	}
}

func (t *Terminal) getApps() []namedInstallable {
	if t.appsOverride != nil {
		return t.appsOverride
	}
	return defaultApps()
}

// InstallAndConfigure runs the full terminal setup.
// appFilter: when non-empty, only those registry apps are installed (devtools/corelibs skipped).
// skipFilter: those registry apps are always skipped regardless of appFilter.
func (t *Terminal) InstallAndConfigure(appFilter, skipFilter map[string]bool) {
	summary := &InstallationSummary{}

	err := t.DisplayGithubInstructions()
	displayMessage(err, "instructions", true)
	t.InstallTerminalApps(summary, appFilter, skipFilter)
	if len(appFilter) == 0 {
		t.InstallDevTools(summary)
		t.InstallCoreLibs(summary)
	}

	utils.PrintInfo(fmt.Sprintf("Installation complete: %s", summary.FormatSummary()))
}

// InstallTerminalApps installs registry-managed terminal apps with optional filtering.
func (t *Terminal) InstallTerminalApps(
	summary *InstallationSummary,
	appFilter, skipFilter map[string]bool,
) {
	for _, entry := range t.getApps() {
		if commands.Interrupted() {
			utils.PrintWarning("Installation interrupted; stopping terminal app installs.")
			break
		}
		if skipFilter[entry.name] {
			continue
		}
		if len(appFilter) > 0 && !appFilter[entry.name] {
			continue
		}
		if err := entry.app.SoftInstall(); err != nil {
			displayMessage(err, entry.name)
			trackResult(summary, entry.name, err)
			continue
		}
		trackResult(summary, entry.name, nil)
		if err := entry.app.SoftConfigure(); err != nil {
			displayMessage(err, entry.name, true)
		}
	}
}

func (t *Terminal) InstallDevTools(summary *InstallationSummary) {
	// should install:
	// - bat
	// - btop
	// - curl
	// - eza
	// - fd-find
	// - fzf
	// - gh
	// - jq
	// - powerlevel10k
	// - plocate (Debian only - replaces locate)
	// - apache2-utils (Debian only)
	// - ripgrep
	// - syntaxhighlighting
	// - tldr
	// - zoxide
	// - zsh-autosuggestions
	devtools := []struct {
		name string
		app  interface {
			SoftInstall() error
			SoftConfigure() error
		}
	}{
		{constants.Bat, bat.New()},
		{constants.Btop, btop.New()},
		{constants.Curl, curl.New()},
		{constants.Eza, eza.New()},
		{constants.FdFind, fdfind.New()},
		{constants.Fzf, fzf.New()},
		{constants.GithubCli, githubcli.New()},
		{constants.Jq, jq.New()},
		{constants.Powerlevel10k, powerlevel10k.New()},
		{constants.Ripgrep, ripgrep.New()},
		{constants.Syntaxhighlighting, syntaxhighlighting.New()},
		{constants.Tldr, tldr.New()},
		{constants.Zoxide, zoxide.New()},
		{constants.ZshAutosuggestions, autosuggestions.New()},
	}
	for _, devtool := range devtools {
		if commands.Interrupted() {
			utils.PrintWarning("Installation interrupted; stopping dev tool installs.")
			return
		}
		if err := devtool.app.SoftInstall(); err != nil {
			displayMessage(err, devtool.name)
			trackResult(summary, devtool.name, err)
			continue
		}
		trackResult(summary, devtool.name, nil)
		if err := devtool.app.SoftConfigure(); err != nil {
			displayMessage(err, devtool.name, true)
		}
	}

	// Install Debian-only packages (no dedicated app modules)
	if !t.Base.Platform.IsMac() {
		debianOnlyPackages := []string{constants.Plocate, constants.ApacheUtils}
		for _, pkg := range debianOnlyPackages {
			if commands.Interrupted() {
				utils.PrintWarning(
					"Installation interrupted; stopping Debian-only package installs.",
				)
				break
			}
			if err := t.Cmd.MaybeInstallPackage(pkg); err != nil {
				displayMessage(err, pkg)
				trackResult(summary, pkg, err)
			} else {
				trackResult(summary, pkg, nil)
			}
		}
	}
}

func (t *Terminal) InstallCoreLibs(summary *InstallationSummary) {
	// installs libs:
	// - autoconf
	// - bison
	// - fontconfig
	// - gdbm
	// - jemalloc
	// - libffi
	// - libyaml
	// - mupdf
	// - ncurses
	// - openssl
	// - pkg-config
	// - readline
	// - unzip
	// - vips
	// - xcode (only on mac)
	// - zlib
	libs := []struct {
		name string
		app  interface {
			SoftInstall() error
			SoftConfigure() error
		}
	}{
		{constants.Autoconf, autoconf.New()},
		{constants.Bison, bison.New()},
		{constants.FontConfig, fontconfig.New()},
		{constants.Gdbm, gdbm.New()},
		{constants.Jemalloc, jemalloc.New()},
		{constants.Libffi, libffi.New()},
		{constants.Libyaml, libyaml.New()},
		{constants.Mupdf, mupdf.New()},
		{constants.Ncurses, ncurses.New()},
		{constants.OpenSSL, openssl.New()},
		{constants.PkgConfig, pkgconfig.New()},
		{constants.Readline, readline.New()},
		{constants.Unzip, unzip.New()},
		{constants.Vips, vips.New()},
		{constants.Xcode, xcode.New()},
		{constants.Zlib, zlib.New()},
	}
	for _, lib := range libs {
		if commands.Interrupted() {
			utils.PrintWarning("Installation interrupted; stopping core lib installs.")
			break
		}
		switch lib.name {
		case constants.Xcode:
			if t.Base.Platform.IsMac() {
				if err := lib.app.SoftInstall(); err != nil {
					displayMessage(err, lib.name)
					trackResult(summary, lib.name, err)
					continue
				}
				trackResult(summary, lib.name, nil)
				if err := lib.app.SoftConfigure(); err != nil {
					displayMessage(err, lib.name, true)
				}
			}
			continue
		default:
			if err := lib.app.SoftInstall(); err != nil {
				displayMessage(err, lib.name)
				trackResult(summary, lib.name, err)
				continue
			}
			trackResult(summary, lib.name, nil)
			if err := lib.app.SoftConfigure(); err != nil {
				displayMessage(err, lib.name, true)
			}
		}
	}
}

func (t *Terminal) DisplayGithubInstructions() error {
	var sshAddCmd, copyCmd string

	if t.Base.Platform.IsMac() {
		sshAddCmd = "ssh-add --apple-use-keychain ~/.ssh/id_ed25519"
		copyCmd = "pbcopy < ~/.ssh/id_ed25519.pub"
	} else {
		sshAddCmd = "ssh-add ~/.ssh/id_ed25519"
		copyCmd = "cat ~/.ssh/id_ed25519.pub  # Copy the output"
	}

	instructions := fmt.Sprintf(`
		1. Generate a new SSH key:
		   ssh-keygen -t ed25519 -C "me@devita.com"
		2. Start the SSH agent:
		   eval "$(ssh-agent -s)"
		3. Add your SSH key to the agent:
		   %s
		4. Copy the SSH key to your clipboard:
		   %s
		5. Go to GitHub → Settings → SSH and GPG keys → New SSH key and paste it.
		6. To test it out, run:
		   ssh -T git@github.com

		See documentation: https://docs.github.com/en/authentication/connecting-to-github-with-ssh/generating-a-new-ssh-key-and-adding-it-to-the-ssh-agent
	`, sshAddCmd, copyCmd)

	return promptui.DisplayInstructions(
		"Let's set up GitHub SSH access - essential for working with repositories",
		instructions,
		false,
	)
}

// trackResult records an installation result in the summary
func trackResult(summary *InstallationSummary, name string, err error) {
	result := InstallationResult{
		PackageName: name,
		Attempt:     1,
	}
	if err != nil {
		result.Status = StatusFailed
		result.ErrorMessage = err.Error()
		summary.Failed++
	} else {
		result.Status = StatusSuccess
		summary.Installed++
	}
	summary.Results = append(summary.Results, result)
}

func displayMessage(err error, name string, displayOnlyErrors ...bool) {
	if err != nil {
		logger.L().Errorw("Error installing ", "package_name", name, "error", err)
		utils.PrintWarning(
			fmt.Sprintf(
				"Install (%s) errored... To halt the installation, press ctrl+c or use --debug flag to see more details",
				name,
			),
		)
	} else {
		if displayOnlyErrors != nil && displayOnlyErrors[0] {
			return
		}
		msg := fmt.Sprintf("Installing %s (if no previously installed)...", name)
		utils.PrintInfo(msg)
	}
}
