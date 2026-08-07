package worktree

import (
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/logger"
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

	// EnsureInstalled verifies this coder is installed and RETURNS THE PATH the
	// probe resolved, so the pane that launches it can exec exactly what was
	// checked. An empty path with a nil error is a normal outcome, not a
	// half-answer: the probe may have been inconclusive (ADR-0016 - it must
	// never block a create), or `command -v` may have answered with something
	// that is not a path at all (alias text, a function or builtin name). Every
	// such outcome selects the interactive fallback (ADR-0020 part 3).
	//
	// It returns the path rather than only an error because ADR-0020 requires
	// ONE probe per pane per create, with the command that runs built from that
	// probe's answer. An error-only signature leaves only two options, and both
	// break that: probe again at launch (two answers that can disagree, so the
	// check verified something other than what ran), or drop the path and always
	// launch the bare name (every pane silently reduced to the fallback).
	EnsureInstalled() (string, error)

	// interactiveLaunch and execLaunch are this coder's two launch forms as
	// structured values (see launch.go's paneLaunch): the alias form a shell
	// resolves, and the resolved-binary form a created pane execs. Both take an
	// empty prompt to mean "no opening prompt".
	//
	// They are interface methods for the same reason PromptCommand is one
	// (ADR-0011): a coder added later cannot be registered without deciding
	// its own argument shape, whereas a package-level switch on coder type
	// would silently fall through and produce a promptless - or, now, a
	// prefix-less - launch with no compile error.
	//
	// BOTH are needed here, even though interactiveLaunch(p).render() is
	// string-identical to PromptCommand(p) today. A created pane's command is a
	// function of the preflight probe's resolution - a resolved path takes the
	// exec form, no path takes the alias form (ADR-0020 part 3) - and the code
	// that must express that, coderPane in layout.go, holds a coder only as an
	// AICoder. With just execLaunch on the interface it could not reach the
	// alias form polymorphically, leaving two options that are both worse: a
	// type switch on the concrete coder (what ADR-0011's argument above
	// rejects), or building the fallback from the PromptCommand STRING and
	// handing it to interactivePaneCommand directly - which bypasses
	// paneCommandFor, the single place that pairs a launch kind with its recipe,
	// and blurs the two representations launch.go's header comment keeps
	// separate (PromptCommand is the form TYPED into a live interactive shell).
	//
	// They are unexported, which also seals the interface: every AICoder is one
	// of this file's, and ResolveAICoder is the only way to get one. Nothing
	// outside this package implements it today, and a launch form defined
	// elsewhere would sit outside the checks this package makes on them.
	interactiveLaunch(prompt string) paneLaunch
	execLaunch(binaryPath, prompt string) paneLaunch
}

// ensureToolInstalled reports whether launchToken resolves in the user's
// interactive shell, returning the absolute path the probe resolved for it and a
// consistent, actionable error naming displayName if it doesn't resolve. Shared
// by every EnsureInstalled below (opencode, claude) and by layout.go's nvim
// check (nvim has no AICoder wrapper since it isn't an AI coder) - one lookup +
// error-format shape instead of three hand-rolled copies of it.
//
// The returned path is what a created pane execs, so this probe is the SINGLE
// one behind both the check and the launch (ADR-0020: one probe per pane per
// create, and the command that runs is built from that probe's answer). It is
// empty whenever the probe produced no path - a NotFound or Inconclusive
// outcome, or a Found outcome whose output was not path-shaped (alias text, a
// shell function or builtin name; see commands.lastPathLine). Every one of those
// selects the interactive fallback, and none of them is an error on its own.
//
// It goes through commands.ShellCommandLookupFn, NOT commands.LookPathFn /
// exec.LookPath, on purpose: a worktree window launches its coder by sending a
// shell command to an interactive tmux pane, and that pane's PATH (repaired via
// ~/.zshenv) can differ from dg's own process PATH when dg ws was started
// from a non-login pane. Checking with exec.LookPath there gives a false "not
// installed" for a tool that would actually launch fine. Resolving the tool the
// same way the pane will is the only check that matches reality. The seam is
// swappable in tests (see setShellCommandExistsFn), same as LookPathFn.
//
// Only a probe that PROVED the tool absent blocks the caller. An inconclusive
// probe — the shell didn't answer within the deadline, or couldn't run at
// all — proceeds instead (ADR-0016): this check exists to pre-empt a cosmetic
// `command not found` inside the pane, and blocking a whole worktree create on
// evidence the probe doesn't have is strictly worse than the failure it
// softens. On a loaded machine (interactive-shell startup slower than the
// probe's deadline) the old bool seam turned every create into a false
// "opencode is not installed" with an install suggestion that fixed nothing.
//
// launchToken is the exact token the window build will send to the pane (the
// cc/oc alias for a coder, "nvim" for the editor), NOT the underlying binary -
// so the check can't pass while the launch fails. A coder installed outside
// devgeta (so its cc/oc alias was never written to devgeta.zsh) correctly fails
// this check up front with an actionable message, rather than building a window
// whose pane then dies on `cc: command not found`. displayName is the binary the
// message names (claude/opencode/nvim), which reads better than the alias.
func ensureToolInstalled(launchToken, displayName string) (string, error) {
	resolvedPath, result := commands.ShellCommandLookupFn(launchToken)
	switch result {
	case commands.ShellLookupNotFound:
		return "", fmt.Errorf(
			"%s is not installed. Install it with: dg install --only terminal",
			displayName,
		)
	case commands.ShellLookupInconclusive:
		// Debug, not Warn: this runs under the dg ws bubbletea alt-screen,
		// where anything printed to the terminal corrupts the TUI, and there
		// is nothing the user needs to do — if the tool really is missing,
		// the pane says so the moment it launches.
		logger.L().Debugw(
			"tool probe inconclusive, proceeding without blocking (ADR-0016)",
			"launchToken", launchToken,
			"tool", displayName,
		)
		// No path, no error: the pane takes the interactive fallback, which
		// leaves it exactly as well off as it is today (ADR-0016, ADR-0020).
		return "", nil
	}
	return resolvedPath, nil
}

// OpenCodeCoder implements AICoder for OpenCode
type OpenCodeCoder struct{}

func (o *OpenCodeCoder) Name() string { return "opencode" }

// Command returns the devgeta shell alias (oc), not the raw binary, so the one
// definition of how to launch opencode lives in devgeta.zsh (alias oc=opencode)
// rather than being duplicated here. This is the form typed into an interactive
// shell (a repaired window, `dg wt move`'s retarget), where that alias is
// defined.
//
// A pane devgeta CREATES no longer uses this: it execs the resolved binary (see
// execLaunch, and ADR-0020 for why a created pane cannot rely on an alias
// existing).
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
// It renders the structured alias-form launch, so the argument shape now has
// one author across BOTH representations (this typed string and the exec'd pane
// command) rather than one per representation.
//
// The arguments are single-quoted because the returned string is typed into an
// interactive shell verbatim by tmux send-keys - there is no Go-side shell
// parser to lean on. See ADR-0011. The flags are quoted too, which is why this
// now emits `oc '--prompt' '<text>'` rather than `oc --prompt '<text>'`: same
// command to the shell, uniform rule (see paneLaunch.render).
func (o *OpenCodeCoder) promptCommandWithAgent(agent, prompt string) string {
	return o.interactiveLaunchWithAgent(agent, prompt).render()
}

// interactiveLaunch is the alias form: the oc alias, resolved by an interactive
// shell.
func (o *OpenCodeCoder) interactiveLaunch(prompt string) paneLaunch {
	return o.interactiveLaunchWithAgent("", prompt)
}

// interactiveLaunchWithAgent is interactiveLaunch with a reviewer agent pinned,
// for the review launches (see layout.go's ReviewCommand). Claude has no
// equivalent: reviewer launches are OpenCode-only by design, because the
// reviewer agents' permission: frontmatter is enforced by OpenCode and ignored
// by Claude Code.
func (o *OpenCodeCoder) interactiveLaunchWithAgent(agent, prompt string) paneLaunch {
	return aliasLaunch(o.Command(), o.launchArgs(agent, prompt)...)
}

// execLaunch is the resolved-binary form: the pane execs binaryPath, so its own
// PATH stops mattering (ADR-0020).
func (o *OpenCodeCoder) execLaunch(binaryPath, prompt string) paneLaunch {
	return o.execLaunchWithAgent(binaryPath, "", prompt)
}

// execLaunchWithAgent is execLaunch with a reviewer agent pinned - the review
// window's create and split branches both exec opencode rather than typing the
// oc alias at a pane (ADR-0020 part 3).
func (o *OpenCodeCoder) execLaunchWithAgent(binaryPath, agent, prompt string) paneLaunch {
	return binaryLaunch(binaryPath, o.launchArgs(agent, prompt)...)
}

// launchArgs is the one place opencode's argument shape is decided, for every
// form above: --agent before --prompt, and each flag omitted entirely when its
// value is empty (a bare "--agent" with no value would be a broken command
// line, not a no-op).
func (o *OpenCodeCoder) launchArgs(agent, prompt string) []string {
	args := make([]string, 0, 4)
	if agent != "" {
		args = append(args, "--agent", agent)
	}
	if prompt != "" {
		args = append(args, "--prompt", prompt)
	}
	return args
}

// EnsureInstalled checks the exact launch token (the oc alias), not the raw
// "opencode" binary, so a pass guarantees the pane launch will resolve too; the
// error still names "opencode" as the thing to install.
//
// It returns whatever path the probe resolved for that token. Probing an ALIAS
// means that path is empty in practice today (`command -v oc` prints
// `alias oc=opencode`, which is not path-shaped), so every created coder pane
// takes the interactive fallback for now - correct, and exactly today's
// behavior. Switching the probed token to the BINARY is the step that starts
// producing a path here, and it comes with the launch recipe becoming the source
// of the alias (ADR-0020's part 3, rule 1).
func (o *OpenCodeCoder) EnsureInstalled() (string, error) {
	return ensureToolInstalled(o.Command(), o.Name())
}

// ClaudeCoder implements AICoder for Claude Code
type ClaudeCoder struct{}

func (c *ClaudeCoder) Name() string { return "claude" }

// Command returns the devgeta shell alias (cc), not the raw binary. The alias
// (alias cc="CLAUDE_CODE_NO_FLICKER=1 claude" in devgeta.zsh) carries both the
// binary name and the no-flicker env var, and this is the form typed into an
// interactive shell (a repaired window, `dg wt move`'s retarget), where that
// alias is defined.
//
// A pane devgeta CREATES no longer uses this: it execs the binary, spelling the
// env var out from claudeNoFlickerEnv instead (see execLaunch, and ADR-0020 for
// why a created pane cannot rely on an alias existing).
func (c *ClaudeCoder) Command() string { return "cc" }

// PromptCommand launches Claude Code with prompt as its opening message.
// Claude takes the prompt POSITIONALLY (`claude [options] [command] [prompt]`,
// which "starts an interactive session by default") - it has no --prompt flag,
// unlike opencode. Verified against the installed binary, 2026-07-31.
//
// prompt is single-quoted because the returned string is typed into an
// interactive shell verbatim by tmux send-keys. See ADR-0011.
func (c *ClaudeCoder) PromptCommand(prompt string) string {
	return c.interactiveLaunch(prompt).render()
}

// interactiveLaunch is the alias form: the cc alias, resolved by an interactive
// shell. No environment prefix, because the alias definition already carries
// CLAUDE_CODE_NO_FLICKER=1 itself.
func (c *ClaudeCoder) interactiveLaunch(prompt string) paneLaunch {
	return aliasLaunch(c.Command(), c.launchArgs(prompt)...)
}

// execLaunch is the resolved-binary form: the pane execs binaryPath, so its own
// PATH stops mattering (ADR-0020). This is the one launch that needs the env
// prefix spelled out, because exec'ing the binary directly bypasses the alias
// that used to supply it.
func (c *ClaudeCoder) execLaunch(binaryPath, prompt string) paneLaunch {
	return binaryLaunchWithEnv(claudeNoFlickerEnv, binaryPath, c.launchArgs(prompt)...)
}

// launchArgs is the one place claude's argument shape is decided: the prompt is
// POSITIONAL (claude has no --prompt flag, unlike opencode), and absent when
// empty - launching claude with an empty quoted argument would hand the coder an
// empty opening message rather than none.
//
// That argument is deliberately DESCRIBED rather than spelled out: gofmt's doc
// comment printer converts a pair of adjacent single quotes into a typographic
// closing quote, so writing the literal form here is silently mangled on every
// format (which is how this sentence became gibberish in the first place).
func (c *ClaudeCoder) launchArgs(prompt string) []string {
	if prompt == "" {
		return nil
	}
	return []string{prompt}
}

// EnsureInstalled checks the exact launch token (the cc alias), not the raw
// "claude" binary, so a pass guarantees the pane launch will resolve too; the
// error still names "claude" as the thing to install.
//
// As with opencode's, the returned path is empty in practice while the probed
// token is the ALIAS - see OpenCodeCoder.EnsureInstalled.
func (c *ClaudeCoder) EnsureInstalled() (string, error) {
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
