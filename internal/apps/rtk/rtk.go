// rtk token-compressing CLI proxy with devgeta integration
//
// rtk ("Rust Token Killer") is a CLI proxy that filters and compresses the
// output of 100+ common dev commands (git, test runners, docker, cat/grep, …)
// before an LLM agent reads it, cutting up to 90% of bash output. It
// complements `dg task`: rtk is generic lossy compression, `dg task` is
// semantic orchestration + policy (see docs/guides/task-design.md).
//
// Devgeta installs the binary only. rtk's command-rewriting hook
// (`rtk init -g`) intercepts every agent Bash call and stays opt-in —
// see ADR-0004 and docs/apps/rtk.md.
//
// References:
// - rtk Repository: https://github.com/rtk-ai/rtk
// - rtk User Guide: https://www.rtk-ai.app/guide
//
// Common rtk commands available through ExecuteCommand():
//   - rtk --version - Show rtk version information
//   - rtk gain - Show token-savings dashboard
//   - rtk init -g - Install the agent hook (opt-in, user-run)
//   - rtk git status - Compact git status

package rtk

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/downloader"
	gh "github.com/cjairm/devgeta/pkg/github"
	"github.com/cjairm/devgeta/pkg/logger"
)

var _ apps.App = (*Rtk)(nil)

type Rtk struct {
	Cmd          cmd.Command
	Base         cmd.BaseCommandExecutor
	fetchVersion func(owner, repo string) (string, error)                                      // injectable for tests
	downloadFn   func(ctx context.Context, url, dest string, cfg downloader.RetryConfig) error // injectable for tests
}

func (r *Rtk) Name() string       { return constants.Rtk }
func (r *Rtk) Kind() apps.AppKind { return apps.KindTerminal }

func (r *Rtk) getVersion(owner, repo string) (string, error) {
	if r.fetchVersion != nil {
		return r.fetchVersion(owner, repo)
	}
	return gh.FetchLatestRelease(owner, repo)
}

func New() *Rtk {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Rtk{Cmd: osCmd, Base: baseCmd}
}

func (r *Rtk) Install() error {
	if r.Base.IsMac() {
		return r.Cmd.InstallPackage(constants.Rtk)
	}
	return r.installDebianRtk()
}

func (r *Rtk) SoftInstall() error {
	if r.Base.IsMac() {
		return r.Cmd.MaybeInstallPackage(constants.Rtk)
	}
	// On Debian: rtk is not in apt — skip if already in PATH, otherwise install from GitHub
	if _, err := cmd.LookPathFn(constants.Rtk); err == nil {
		logger.L().Infow("rtk already installed, skipping")
		return nil
	}
	return r.installDebianRtk()
}

// linuxTarget returns the Rust target triple used in rtk release artifact names.
func linuxTarget() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64-unknown-linux-gnu"
	}
	return "x86_64-unknown-linux-musl"
}

// installDebianRtk fetches the latest rtk release from GitHub, downloads the
// Linux tar.gz for the current architecture, and installs the binary to /usr/local/bin
func (r *Rtk) installDebianRtk() error {
	version, err := r.getVersion("rtk-ai", "rtk")
	if err != nil {
		return fmt.Errorf("failed to fetch rtk version: %w", err)
	}

	url := fmt.Sprintf(
		"https://github.com/rtk-ai/rtk/releases/download/v%s/rtk-%s.tar.gz",
		version, linuxTarget(),
	)
	checksumsURL := fmt.Sprintf(
		"https://github.com/rtk-ai/rtk/releases/download/v%s/checksums.txt",
		version,
	)
	logger.L().Infow("Downloading rtk for Debian", "version", version, "url", url)

	if err := cmd.InstallGitHubBinary(
		r.Base,
		constants.Rtk,
		url,
		checksumsURL,
		r.downloadFn,
	); err != nil {
		return fmt.Errorf("rtk installation failed: %w", err)
	}

	logger.L().Infow("rtk installed successfully for Debian", "version", version)
	return nil
}

func (r *Rtk) ForceInstall() error {
	return baseapp.Reinstall(r.Install, r.Uninstall)
}

func (r *Rtk) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if r.Base.IsMac() {
		if err := r.Cmd.UninstallPackage(constants.Rtk); err != nil {
			return fmt.Errorf("failed to uninstall rtk: %w", err)
		}
	} else {
		if _, _, err := r.Base.ExecCommand(cmd.CommandParams{
			Command: "rm",
			Args:    []string{"-f", "/usr/local/bin/rtk"},
			IsSudo:  true,
		}); err != nil {
			return fmt.Errorf("failed to remove rtk binary: %w", err)
		}
	}
	gc.RemoveFromInstalled(constants.Rtk, "package")
	// The binary is gone, so drop the Claude hook opt-in — otherwise the next
	// `dg configure claude --force` would re-render a hook entry pointing at a
	// missing command.
	if gc.Integrations.RtkClaudeHook {
		gc.Integrations.RtkClaudeHook = false
		logger.L().Infow(
			"cleared rtk Claude hook opt-in",
			"hint", "run `dg configure claude --force` to drop the hook entry from settings.json, or `rtk init -g --uninstall` (before uninstalling rtk) to remove all rtk artifacts",
		)
	}
	return gc.Save()
}

func (r *Rtk) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Rtk, "package")
	return gc.Save()
}

func (r *Rtk) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if gc.IsInstalledByDevgeta(constants.Rtk, "package") {
		return nil
	}
	return r.ForceConfigure()
}

func (r *Rtk) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		Command: constants.Rtk,
		Args:    args,
	}
	if _, _, err := r.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run rtk command: %w", err)
	}
	return nil
}

// InitClaude wires rtk's Claude Code integration (hook + RTK.md + global
// CLAUDE.md reference). See initAgent for why this must stream.
func (r *Rtk) InitClaude() error {
	return r.initAgent("init", "-g", "--auto-patch")
}

// InitOpenCode installs rtk's OpenCode plugin. See initAgent for why this
// must stream.
func (r *Rtk) InitOpenCode() error {
	return r.initAgent("init", "-g", "--opencode")
}

// initAgent runs an `rtk init` invocation with streamed output. `rtk init`
// asks a one-time interactive question when stdin is a terminal — its GDPR
// telemetry consent, which --auto-patch does NOT suppress. The executor
// passes the caller's stdin through, so with captured (non-streamed) output
// the question lands in an invisible buffer while rtk blocks on the answer
// forever. Streaming makes the prompt visible and answerable; without a
// terminal rtk skips the question on its own.
func (r *Rtk) initAgent(args ...string) error {
	if _, _, err := r.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Rtk,
		Args:    args,
		Stream:  true,
	}); err != nil {
		return fmt.Errorf("failed to run rtk %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (r *Rtk) Update() error {
	return fmt.Errorf("%w for rtk", apps.ErrUpdateNotSupported)
}
