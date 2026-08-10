package constants

// AI-coder launch recipes: the single definition of how devgeta launches a
// coder, in BOTH forms it is launched in.
//
// Until ADR-0021 the only definition lived in devgeta.zsh's alias lines, and
// everything that launched a coder typed the alias at an interactive shell -
// which made the shell config the de facto source of truth. A pane devgeta
// creates now execs the resolved binary instead (tmux runs a pane's
// shell-command through a NON-INTERACTIVE shell, where an alias does not
// exist), so the alias stopped being able to hold that role. The recipe below
// takes it over and RENDERS the alias line, keeping one definition behind both
// forms rather than two that can drift.
//
// This lives in pkg/constants because both sides need it and neither can own
// it: internal/config renders devgeta.zsh and internal/tooling/worktree builds
// the pane command, and worktree already imports config - so config importing
// worktree would be an import cycle. pkg/constants is a leaf package both of
// them already import, so neither gains an edge.

// CoderLaunch is how devgeta launches one AI coder:
//
//   - Binary is the executable name. It is what the preflight probe resolves
//     (`command -v <binary>` prints an absolute path for a binary, but only
//     alias text for an alias - ADR-0021 part 3, rule 1) and what a created
//     pane execs.
//   - EnvPrefix is the environment assignment that launch needs, empty when it
//     needs none. It is devgeta-owned and never interpolated from user data,
//     which is why ADR-0021's quoting table embeds it unquoted.
//   - Alias is the short name devgeta writes into devgeta.zsh for the user to
//     type by hand. devgeta itself no longer types it anywhere: since
//     ADR-0021's 2026-08-07 amendment, every path that types a coder command
//     into a pane that already exists sends Command() - the un-aliased
//     binary, plus EnvPrefix - instead.
type CoderLaunch struct {
	Alias     string
	Binary    string
	EnvPrefix string
}

// Command returns the recipe as a shell command word list: the env prefix, if
// any, then the binary. This is what the alias expands to, and what a pane
// runs when no absolute path resolved.
func (c CoderLaunch) Command() string {
	if c.EnvPrefix == "" {
		return c.Binary
	}
	return c.EnvPrefix + " " + c.Binary
}

// AliasLine renders this recipe's zsh alias statement for devgeta.zsh - the
// line internal/config's shell-config template emits, so the alias a user
// types and the command a created pane execs come from the same values.
//
// The value is double-quoted uniformly, including when there is no env prefix
// (`alias oc="opencode"`, where devgeta.zsh used to hold a bare
// `alias oc=opencode`). Both are the same alias to zsh, and one rule with no
// exception is what keeps a future recipe whose value gains a space from
// silently rendering a broken line.
//
// No escaping is applied because none of these values can need it: they are
// devgeta-owned constants declared in this file, and the alias line is pinned
// against this function by a test over the EMBEDDED template (package main's
// TestShellConfigTemplateRendersCoderAliasesFromConstants). A recipe that ever
// needs a quote or a shell metacharacter in it needs that decided here, not
// worked around at a call site.
func (c CoderLaunch) AliasLine() string {
	return "alias " + c.Alias + `="` + c.Command() + `"`
}

// OpenCodeLaunch and ClaudeLaunch are the two coders devgeta launches.
//
// Only claude carries an env prefix: CLAUDE_CODE_NO_FLICKER=1 suppresses the
// redraw flicker its TUI otherwise shows inside tmux.
var (
	OpenCodeLaunch = CoderLaunch{
		Alias:  "oc",
		Binary: OpenCode,
	}

	ClaudeLaunch = CoderLaunch{
		Alias:     "cc",
		Binary:    Claude,
		EnvPrefix: "CLAUDE_CODE_NO_FLICKER=1",
	}
)
