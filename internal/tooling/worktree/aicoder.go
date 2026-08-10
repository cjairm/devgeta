package worktree

import (
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
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
	// such outcome selects the interactive fallback (ADR-0021 part 3).
	//
	// It returns the path rather than only an error because ADR-0021 requires
	// ONE probe per pane per create, with the command that runs built from that
	// probe's answer. An error-only signature leaves only two options, and both
	// break that: probe again at launch (two answers that can disagree, so the
	// check verified something other than what ran), or drop the path and always
	// launch the bare name (every pane silently reduced to the fallback).
	EnsureInstalled() (string, error)

	// interactiveLaunch and execLaunch are this coder's two launch forms as
	// structured values (see launch.go's paneLaunch): the NAME form a shell
	// resolves for itself, and the resolved-binary form a created pane execs. Both
	// take an empty prompt to mean "no opening prompt".
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
	// exec form, no path takes the name form (ADR-0021 part 3) - and the code
	// that must express that, coderPane in layout.go, holds a coder only as an
	// AICoder. With just execLaunch on the interface it could not reach the
	// name form polymorphically, leaving two options that are both worse: a
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

// ensureToolInstalled reports whether binary resolves in the user's interactive
// shell, returning the absolute path the probe resolved for it and a consistent,
// actionable error naming it if it doesn't resolve. Shared by every
// EnsureInstalled below (opencode, claude) and by layout.go's nvim check (nvim
// has no AICoder wrapper since it isn't an AI coder) - one lookup +
// error-format shape instead of three hand-rolled copies of it.
//
// The returned path is what a created pane execs, so this probe is the SINGLE
// one behind both the check and the launch (ADR-0021: one probe per pane per
// create, and the command that runs is built from that probe's answer). It is
// empty whenever the probe produced no path - a NotFound or Inconclusive
// outcome, or a Found outcome whose output was not path-shaped (alias text, a
// shell function or builtin name; see commands.lastPathLine). Every one of those
// selects the interactive fallback, and none of them is an error on its own.
//
// It goes through commands.ShellCommandLookupFn, NOT commands.LookPathFn /
// exec.LookPath, on purpose: dg's OWN process PATH is not the PATH the tool has
// to be reachable from. When `dg ws` is started from a non-login tmux pane, that
// PATH can be truncated and miss a tool that is installed (~/.local/bin/claude),
// so exec.LookPath gives a false "not installed" for a coder that would launch
// fine. A shell probe resolves the tool from the user's own shell, which repairs
// its PATH the way that shell does it - not via any one file, since the mechanism
// is shell-specific (zsh's ~/.zshenv has no bash equivalent, which is exactly why
// ADR-0021 part 3 refuses to lean on it) - and it is what makes a resolved
// absolute path available at all, so the pane needs no PATH of its own. The seam
// is swappable in tests (see setShellCommandExistsFn), same as LookPathFn.
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
// binary is the tool's EXECUTABLE name (claude/opencode/nvim), never the cc/oc
// alias, and the invariant "probe exactly what the pane will launch" holds on
// EVERY path - not only the exec ones. A created pane execs the binary through
// tmux's non-interactive shell, where an alias does not exist at all (ADR-0021
// part 3, rule 1); and since ADR-0021's 2026-08-07 amendment the two paths that
// still TYPE a command into a live pane (the repair branch of ensureWindow,
// launchReviewInLiveWindow's idle-shell reuse) send the binary too, so a pass
// here guarantees they resolve as well. Probing the alias would also make this
// function unable to resolve a path, because `command -v oc` prints
// `alias oc=opencode`, which is not path-shaped.
//
// One consequence of taking the binary is deliberate: the error has nothing left
// to name but the binary, so this takes one argument instead of a (token,
// displayName) pair. The pair existed only because the probed token was an alias
// the message should not mention.
//
// A coder installed OUTSIDE devgeta - one whose cc/oc alias was never written to
// devgeta.zsh - passes this check and launches correctly on every path. While the
// typed form was still the alias, that combination passed preflight and then
// failed in the pane with `cc: command not found`; ADR-0021 accepted that gap and
// its amendment removes it.
func ensureToolInstalled(binary string) (string, error) {
	resolvedPath, result := commands.ShellCommandLookupFn(binary)
	switch result {
	case commands.ShellLookupNotFound:
		return "", fmt.Errorf(
			"%s is not installed. Install it with: dg install --only terminal",
			binary,
		)
	case commands.ShellLookupInconclusive:
		// Debug, not Warn: this runs under the dg ws bubbletea alt-screen,
		// where anything printed to the terminal corrupts the TUI, and there
		// is nothing the user needs to do — if the tool really is missing,
		// the pane says so the moment it launches.
		logger.L().Debugw(
			"tool probe inconclusive, proceeding without blocking (ADR-0016)",
			"binary", binary,
		)
		// No path, no error: the pane takes the interactive fallback, which
		// leaves it exactly as well off as it is today (ADR-0016, ADR-0021).
		return "", nil
	}
	return resolvedPath, nil
}

// recipeLaunch builds the NAME-form launch for a coder's launch recipe: the
// recipe's binary name plus its environment prefix, which is exactly what that
// recipe's devgeta.zsh alias expands to. Both coders go through it, so the typed
// form and the alias line cannot describe different commands.
//
// It spells the recipe out instead of naming the alias because a send-keys path
// must launch what preflight probed - the binary (ADR-0021's 2026-08-07
// amendment). Nothing here depends on devgeta.zsh having been sourced.
func recipeLaunch(recipe constants.CoderLaunch, args ...string) paneLaunch {
	return nameLaunchWithEnv(recipe.EnvPrefix, recipe.Binary, args...)
}

// OpenCodeCoder implements AICoder for OpenCode
type OpenCodeCoder struct{}

// Name is also the name of the single-pane layout this coder derives
// (deriveLayoutFromAlias -> Layout.Name, matched against builtinLayoutNames), so
// it is sourced from the app-name constant rather than from the launch recipe's
// Binary: a layout name and a binary name happen to be the same string today and
// must not be coupled into one.
func (o *OpenCodeCoder) Name() string { return constants.OpenCode }

// Command returns the UN-ALIASED command that launches opencode - the binary
// name, plus any environment prefix the recipe carries - read off the launch
// recipe in pkg/constants, which also renders devgeta.zsh's `alias oc=` line.
//
// This is the form devgeta TYPES into a shell that already exists - today,
// ensureWindow's repair branch (ADR-0021 part 4). It is not the `oc` alias,
// and that is ADR-0021's 2026-08-07 amendment rather than an oversight: preflight
// probes the BINARY, so typing the alias meant sending a live pane something the
// check never verified - a user with opencode on PATH but no devgeta alias passed
// preflight and then got `oc: command not found`. devgeta.zsh still ships the
// alias, for the user to type; devgeta itself no longer depends on it.
func (o *OpenCodeCoder) Command() string { return constants.OpenCodeLaunch.Command() }

// PromptCommand launches opencode with prompt as its opening message, using
// opencode's --prompt flag ("prompt to use" in its --help).
func (o *OpenCodeCoder) PromptCommand(prompt string) string {
	return o.promptCommandWithAgent("", prompt)
}

// promptCommandWithAgent is the single author of opencode's
// launch-with-a-prompt form, shared by PromptCommand (no agent) and
// layout.go's reviewCommandFor (which pins a reviewer agent). Before this existed
// the `--prompt '<quoted>'` fragment had two authors; CLAUDE.md's DRY rule
// requires the extraction happen in the change that introduces the second use.
//
// It renders the structured name-form launch, so the argument shape now has
// one author across BOTH representations (this typed string and the exec'd pane
// command) rather than one per representation.
//
// The arguments are single-quoted because the returned string is typed into an
// interactive shell verbatim by tmux send-keys - there is no Go-side shell
// parser to lean on. See ADR-0011. The flags are quoted too, which is why this
// emits `opencode '--prompt' '<text>'` rather than `opencode --prompt '<text>'`:
// same command to the shell, uniform rule (see paneLaunch.render).
func (o *OpenCodeCoder) promptCommandWithAgent(agent, prompt string) string {
	return o.interactiveLaunchWithAgent(agent, prompt).render()
}

// interactiveLaunch is the name form: the opencode binary NAME, left for the
// shell running it to resolve.
func (o *OpenCodeCoder) interactiveLaunch(prompt string) paneLaunch {
	return o.interactiveLaunchWithAgent("", prompt)
}

// interactiveLaunchWithAgent is interactiveLaunch with a reviewer agent pinned,
// for the review launches (see layout.go's reviewCommandFor). Claude has no
// equivalent: reviewer launches are OpenCode-only by design, because the
// reviewer agents' permission: frontmatter is enforced by OpenCode and ignored
// by Claude Code.
func (o *OpenCodeCoder) interactiveLaunchWithAgent(agent, prompt string) paneLaunch {
	return recipeLaunch(constants.OpenCodeLaunch, o.launchArgs(agent, prompt)...)
}

// execLaunch is the resolved-binary form: the pane execs binaryPath, so its own
// PATH stops mattering (ADR-0021).
func (o *OpenCodeCoder) execLaunch(binaryPath, prompt string) paneLaunch {
	return o.execLaunchWithAgent(binaryPath, "", prompt)
}

// execLaunchWithAgent is execLaunch with a reviewer agent pinned - the review
// window's create and split branches both exec the resolved opencode binary
// rather than typing a command at a pane (ADR-0021 part 3).
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

// EnsureInstalled checks the "opencode" BINARY - what every devgeta launch of
// this coder names, exec'd or typed - not the oc alias, so a pass guarantees the
// launch will resolve too (ADR-0021 part 3, rule 1, and its 2026-08-07
// amendment).
//
// It returns the path the probe resolved for it. Probing the binary is what
// makes that path non-empty in practice: `command -v opencode` prints an
// absolute path, while `command -v oc` prints `alias oc=opencode`, which is not
// path-shaped and always selected the interactive fallback.
func (o *OpenCodeCoder) EnsureInstalled() (string, error) {
	return ensureToolInstalled(constants.OpenCodeLaunch.Binary)
}

// ClaudeCoder implements AICoder for Claude Code
type ClaudeCoder struct{}

// Name is the app-name constant rather than the launch recipe's Binary, for the
// reason spelled out on OpenCodeCoder.Name: this is also a layout name.
func (c *ClaudeCoder) Name() string { return constants.Claude }

// Command returns the UN-ALIASED command that launches Claude Code -
// `CLAUDE_CODE_NO_FLICKER=1 claude` - read off the launch recipe in
// pkg/constants, which also renders devgeta.zsh's `alias cc=` line from those
// same two values. So the alias a user types and the command devgeta runs cannot
// carry a different binary or a different environment.
//
// This is the form devgeta TYPES into a shell that already exists - today,
// ensureWindow's repair branch (ADR-0021 part 4). It is not the `cc` alias:
// see OpenCodeCoder.Command for why (ADR-0021's 2026-08-07 amendment). Because it
// names the binary, it also has to spell the env prefix out - the alias
// definition used to supply that.
func (c *ClaudeCoder) Command() string { return constants.ClaudeLaunch.Command() }

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

// interactiveLaunch is the name form: the claude binary NAME, left for the shell
// running it to resolve, WITH the no-flicker env prefix spelled out. The prefix is
// explicit in both forms now - while this form named the cc alias, the alias
// definition carried it.
func (c *ClaudeCoder) interactiveLaunch(prompt string) paneLaunch {
	return recipeLaunch(constants.ClaudeLaunch, c.launchArgs(prompt)...)
}

// execLaunch is the resolved-binary form: the pane execs binaryPath, so its own
// PATH stops mattering (ADR-0021). It carries the same env prefix
// interactiveLaunch does, from the same recipe.
func (c *ClaudeCoder) execLaunch(binaryPath, prompt string) paneLaunch {
	return binaryLaunchWithEnv(
		constants.ClaudeLaunch.EnvPrefix,
		binaryPath,
		c.launchArgs(prompt)...,
	)
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

// EnsureInstalled checks the "claude" BINARY - what every devgeta launch of this
// coder names - not the cc alias, for the reasons spelled out on
// OpenCodeCoder.EnsureInstalled.
func (c *ClaudeCoder) EnsureInstalled() (string, error) {
	return ensureToolInstalled(constants.ClaudeLaunch.Binary)
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
