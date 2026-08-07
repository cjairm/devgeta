// Pane command construction for panes devgeta creates.
//
// A pane's command reaches tmux as a shell-command (process arguments), not as
// keystrokes typed into the pane - see ADR-0020 for why (the pty input queue
// silently discards everything past 1024 bytes, so a long --prompt was lost
// with tmux still reporting success). tmux hands that shell-command to a shell
// as one string, so there is still no Go-side shell parser in between and every
// fragment devgeta assembles into it has to be shell-quoted.
//
// Two representations of a pane's command coexist on purpose, and they are not
// interchangeable:
//
//   - Pane.Command (layout.go) is the form TYPED into a live interactive shell -
//     "cc", "oc --prompt '...'", "nvim", or a raw --pane value. The repair path
//     and `dg wt move`'s retarget still send exactly that with send-keys,
//     because those panes already exist (ADR-0020 part 4).
//   - The recipes below are the form EXEC'd as a created pane's process. They
//     are what a create path passes to tmux.
//
// This file owns the second one, plus the structured launch value both are
// rendered from.

package worktree

import "strings"

// claudeNoFlickerEnv is the environment prefix Claude Code is launched with -
// the same one the devgeta.zsh alias carries
// (alias cc="CLAUDE_CODE_NO_FLICKER=1 claude"). It is a devgeta-owned constant
// and is never interpolated from user data, which is why it is the one value in
// ADR-0020's quoting table that is embedded unquoted.
//
// It lives here for now; making this constant the single source that ALSO
// renders devgeta.zsh's alias line is a later step of this cycle, and it has to
// move to pkg/constants to get there (internal/config renders devgeta.zsh and
// cannot import this package - that edge would be an import cycle).
const claudeNoFlickerEnv = "CLAUDE_CODE_NO_FLICKER=1"

// paneLaunch is one devgeta-owned command to run in a pane, in structured form:
// a program, its arguments, and an optional environment prefix.
//
// It is structured rather than a "program plus prompt" pair because the create
// paths do not share one argument shape - claude takes its prompt positionally,
// opencode takes --prompt, and the reviewer launch adds --agent <name> before
// it. A two-slot template cannot express those three, which is exactly why the
// three of them used to hand-assemble their own strings (and why an --agent flag
// could not share the prompt fragment's single author).
//
// The zero value means "no command at all" - the shell pane, which gets the
// shell tmux would have started anyway. It renders to "", and both recipes
// below pass that through as "" rather than building a command around nothing.
//
// Every field is set by a constructor below. quoteProgram in particular is not
// a style knob: it records which KIND of program this is, and getting it
// backwards breaks the launch in a way no mocked test would notice (see
// aliasLaunch and binaryLaunch).
type paneLaunch struct {
	envPrefix    string
	program      string
	quoteProgram bool
	args         []string
}

// aliasLaunch builds a launch whose program is a NAME the shell running it must
// resolve: a devgeta shell alias ("cc"/"oc", defined in devgeta.zsh) or a bare
// binary name ("nvim").
//
// The program is deliberately NOT quoted. A shell does not expand an alias that
// was quoted - `'cc' 'text'` looks for a command literally named cc and fails,
// while `cc 'text'` expands the alias - so quoting here would break the exact
// case this form exists to serve. Arguments are still quoted; only the program
// word is special.
//
// This is the form typed into (or run by) an interactive shell: Pane.Command,
// and the inner script of interactivePaneCommand.
func aliasLaunch(program string, args ...string) paneLaunch {
	return paneLaunch{program: program, args: args}
}

// binaryLaunch builds a launch whose program is a resolved absolute path, for a
// pane that execs the binary directly instead of relying on its own PATH
// (ADR-0020: tmux runs a pane's shell-command non-interactively, which has no
// equivalent of zsh's ~/.zshenv PATH repair, and the probe's shell need not even
// be the shell tmux launches).
//
// The path IS quoted, and that is not symmetry with the arguments - it is
// required. "/Users/Jane Doe/.local/bin/claude" is a perfectly valid resolved
// path and would otherwise split into two words.
func binaryLaunch(path string, args ...string) paneLaunch {
	return paneLaunch{program: path, quoteProgram: true, args: args}
}

// binaryLaunchWithEnv is binaryLaunch plus a devgeta-owned environment prefix
// (today only claudeNoFlickerEnv).
//
// The prefix is only available on the binary form on purpose: an alias launch
// runs the devgeta alias, which already carries its own env prefix in its
// definition, so an alias launch with a second prefix would be double-setting
// it. Keeping the prefix out of aliasLaunch's signature makes that
// unrepresentable rather than merely discouraged (CLAUDE.md §4).
func binaryLaunchWithEnv(envPrefix, path string, args ...string) paneLaunch {
	launch := binaryLaunch(path, args...)
	launch.envPrefix = envPrefix
	return launch
}

// isEmpty reports whether this launch has no command to run (the zero value -
// the shell pane).
func (l paneLaunch) isEmpty() bool { return l.program == "" }

// render turns the launch into a shell command line: the env prefix, the
// program, then every argument, joined by spaces.
//
// EVERY argument is quoted, flags included - so opencode's prompt form renders
// as `oc '--prompt' 'text'`, not `oc --prompt 'text'`. Those are shell
// equivalent (each is still exactly one word to the shell) and the uniform rule
// is the point: ADR-0020 records that stating a quoting rule and then applying
// it selectively already failed three times during its own review, and a
// "don't quote things that look like flags" exception is precisely the kind of
// judgement call that fails a fourth time. A flag never needs the exception,
// so it does not get one.
func (l paneLaunch) render() string {
	if l.isEmpty() {
		return ""
	}

	parts := make([]string, 0, len(l.args)+2)
	if l.envPrefix != "" {
		parts = append(parts, l.envPrefix)
	}
	if l.quoteProgram {
		parts = append(parts, shellSingleQuote(l.program))
	} else {
		parts = append(parts, l.program)
	}
	for _, arg := range l.args {
		parts = append(parts, shellSingleQuote(arg))
	}
	return strings.Join(parts, " ")
}

// execPaneCommand returns the shell-command tmux should run as a created pane's
// process for a devgeta-owned launch whose binary path resolved:
//
//	<rendered launch>; exec '<shell>'
//
// The trailing `exec` preserves today's pane lifetime: exec'ing the launch
// alone would close the pane when the command exits, and the last pane closing
// takes the window with it - so quitting your coder would destroy the window
// instead of dropping you at a shell (both measured, ADR-0020 part 2).
//
// shell must come from resolveShell: it is interpolated into the command and
// only that resolution guarantees it is an existing, executable, absolute path
// rather than a $SHELL pointing at something uninstalled.
//
// An empty launch (the shell pane) yields "", meaning "no command" - the pane
// gets the shell tmux would have started on its own, which is exactly today's
// behavior. Wrapping nothing in `; exec` would instead run an empty command.
func execPaneCommand(launch paneLaunch, shell string) string {
	if launch.isEmpty() {
		return ""
	}
	return launch.render() + "; exec " + shellSingleQuote(shell)
}

// interactivePaneCommand returns the shell-command tmux should run as a created
// pane's process for a script that needs the user's INTERACTIVE shell:
//
//	'<shell>' -ic '<script>; exec '<shell>' -i'
//
// Two kinds of script land here, and one mechanism serving both is deliberate
// (ADR-0020 part 3) rather than one of them being a special case:
//
//   - A user-authored --pane value. It is a command line the user wrote for
//     their own shell and may use their own aliases and functions, so running
//     it non-interactively would silently change what it means.
//   - A devgeta-owned alias-form launch, when the preflight probe could not
//     resolve an absolute path. The alias (cc/oc) only exists in an interactive
//     shell, so this is what keeps an inconclusive probe costing the pane
//     nothing - ADR-0016's fail-open, preserved.
//
// Three quoting facts, each load-bearing:
//
//   - The whole inner script is quoted as ONE word. A --pane value stays
//     unquoted WITHIN the script (it is a command line - `cd api && make dev`
//     must run two commands), but a single quote anywhere inside it would end
//     the -ic wrapper early; quoting the assembled script keeps every character
//     intact for the inner shell. Measured with --pane value
//     `printf %s "it's fine"`: naive embedding breaks, this runs (ADR-0020).
//   - The shell is quoted at BOTH interpolation sites. Both are equally capable
//     of breaking, and its value comes from $SHELL or tmux's default-shell -
//     neither of which devgeta controls.
//   - The trailing `exec` is inside the SAME shell invocation as the script.
//     The nested form ('<shell>' -ic '<script>'; exec '<shell>' -i) silently
//     drops a directory change, because the cd happened in a child that then
//     exited - measured, and it would break --pane's headline use case while
//     still looking like it worked.
//
// shell must come from resolveShell, as with execPaneCommand. A blank script
// yields "" for the same reason an empty launch does there.
func interactivePaneCommand(script, shell string) string {
	if strings.TrimSpace(script) == "" {
		return ""
	}
	quotedShell := shellSingleQuote(shell)
	return quotedShell + " -ic " + shellSingleQuote(script+"; exec "+quotedShell+" -i")
}
