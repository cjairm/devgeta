package worktree

import (
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/internal/commands"
)

// AICoder represents an AI coding assistant that can be launched in a worktree window
type AICoder interface {
	Name() string
	Command() string

	// PromptCommand returns the shell command that launches this coder with
	// prompt as its opening message, for `dg wt create --prompt`. It is a
	// method rather than a package-level switch on coder type so a coder added
	// later cannot be registered without deciding its own prompt form - see
	// ADR-0011.
	//
	// The two current forms differ because the two CLIs differ (verified
	// against the installed binaries, 2026-07-31): claude takes the prompt
	// positionally, opencode takes --prompt. Do not "unify" them.
	PromptCommand(prompt string) string

	EnsureInstalled() error
}

// ensureToolInstalled reports whether launchToken resolves in the user's
// interactive shell and returns a consistent, actionable error naming
// displayName if it doesn't. Shared by every EnsureInstalled below (opencode,
// claude) and by layout.go's nvim check (nvim has no AICoder wrapper since it
// isn't an AI coder) - one lookup + error-format shape instead of three
// hand-rolled copies of it.
//
// It goes through commands.ShellCommandExistsFn, NOT commands.LookPathFn /
// exec.LookPath, on purpose: a worktree window launches its coder by sending a
// shell command to an interactive tmux pane, and that pane's PATH (repaired via
// ~/.zshenv) can differ from dg's own process PATH when dg ws was started
// from a non-login pane. Checking with exec.LookPath there gives a false "not
// installed" for a tool that would actually launch fine. Resolving the tool the
// same way the pane will is the only check that matches reality. The seam is
// swappable in tests (see setShellCommandExistsFn), same as LookPathFn.
//
// launchToken is the exact token the window build will send to the pane (the
// cc/oc alias for a coder, "nvim" for the editor), NOT the underlying binary -
// so the check can't pass while the launch fails. A coder installed outside
// devgeta (so its cc/oc alias was never written to devgeta.zsh) correctly fails
// this check up front with an actionable message, rather than building a window
// whose pane then dies on `cc: command not found`. displayName is the binary the
// message names (claude/opencode/nvim), which reads better than the alias.
func ensureToolInstalled(launchToken, displayName string) error {
	if !commands.ShellCommandExistsFn(launchToken) {
		return fmt.Errorf(
			"%s is not installed. Install it with: dg install --only terminal",
			displayName,
		)
	}
	return nil
}

// OpenCodeCoder implements AICoder for OpenCode
type OpenCodeCoder struct{}

func (o *OpenCodeCoder) Name() string { return "opencode" }

// Command returns the devgeta shell alias (oc), not the raw binary, so the one
// definition of how to launch opencode lives in devgeta.zsh (alias oc=opencode)
// rather than being duplicated here. The command is sent to an interactive tmux
// pane where that alias is defined.
func (o *OpenCodeCoder) Command() string { return "oc" }

// PromptCommand launches opencode with prompt as its opening message, using
// opencode's --prompt flag ("prompt to use" in its --help).
func (o *OpenCodeCoder) PromptCommand(prompt string) string {
	return o.promptCommandWithAgent("", prompt)
}

// promptCommandWithAgent is the single author of opencode's
// launch-with-a-prompt form, shared by PromptCommand (no agent) and
// layout.go's ReviewCommand (which pins a reviewer agent). Before this existed
// the `--prompt '<quoted>'` fragment had two authors; CLAUDE.md's DRY rule
// requires the extraction happen in the change that introduces the second use.
//
// prompt is single-quoted because the returned string is typed into an
// interactive shell verbatim by tmux send-keys - there is no Go-side shell
// parser to lean on. See ADR-0011.
func (o *OpenCodeCoder) promptCommandWithAgent(agent, prompt string) string {
	command := o.Command()
	if agent != "" {
		command += " --agent " + agent
	}
	return command + " --prompt " + shellSingleQuote(prompt)
}

// EnsureInstalled checks the exact launch token (the oc alias), not the raw
// "opencode" binary, so a pass guarantees the pane launch will resolve too; the
// error still names "opencode" as the thing to install.
func (o *OpenCodeCoder) EnsureInstalled() error {
	return ensureToolInstalled(o.Command(), o.Name())
}

// ClaudeCoder implements AICoder for Claude Code
type ClaudeCoder struct{}

func (c *ClaudeCoder) Name() string { return "claude" }

// Command returns the devgeta shell alias (cc), not the raw binary. The alias
// (alias cc="CLAUDE_CODE_NO_FLICKER=1 claude" in devgeta.zsh) owns both the
// binary name and the no-flicker env var, so that launch recipe lives in one
// place instead of being duplicated here. The command is sent to an interactive
// tmux pane where the alias is defined.
func (c *ClaudeCoder) Command() string { return "cc" }

// PromptCommand launches Claude Code with prompt as its opening message.
// Claude takes the prompt POSITIONALLY (`claude [options] [command] [prompt]`,
// which "starts an interactive session by default") - it has no --prompt flag,
// unlike opencode. Verified against the installed binary, 2026-07-31.
//
// prompt is single-quoted because the returned string is typed into an
// interactive shell verbatim by tmux send-keys. See ADR-0011.
func (c *ClaudeCoder) PromptCommand(prompt string) string {
	return c.Command() + " " + shellSingleQuote(prompt)
}

// EnsureInstalled checks the exact launch token (the cc alias), not the raw
// "claude" binary, so a pass guarantees the pane launch will resolve too; the
// error still names "claude" as the thing to install.
func (c *ClaudeCoder) EnsureInstalled() error {
	return ensureToolInstalled(c.Command(), c.Name())
}

// ResolveAICoder resolves an alias to an AICoder implementation
// Valid aliases (case-insensitive):
//   - opencode, oc -> OpenCodeCoder
//   - claude, cc, claudecode -> ClaudeCoder
func ResolveAICoder(alias string) (AICoder, error) {
	switch strings.ToLower(alias) {
	case "opencode", "oc":
		return &OpenCodeCoder{}, nil
	case "claude", "cc", "claudecode":
		return &ClaudeCoder{}, nil
	default:
		return nil, fmt.Errorf(
			"unknown AI coder alias %q. Valid aliases: opencode, oc, claude, cc, claudecode",
			alias,
		)
	}
}
