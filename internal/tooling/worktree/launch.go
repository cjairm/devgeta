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
// rendered from. paneCommandFor is the single door from a launch to a pane
// command: it picks the recipe from the launch's kind, so a create path never
// pairs the two by hand.

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
// a kind, a program, its arguments, and an optional environment prefix.
//
// It is structured rather than a "program plus prompt" pair because the create
// paths do not share one argument shape - claude takes its prompt positionally,
// opencode takes --prompt, and the reviewer launch adds --agent <name> before
// it. A two-slot template cannot express those three, which is exactly why the
// three of them used to hand-assemble their own strings (and why an --agent flag
// could not share the prompt fragment's single author).
//
// The zero value means "no command at all" - the shell pane, which gets the
// shell tmux would have started anyway. It renders to "", and paneCommandFor
// passes that through as "" rather than building a command around nothing.
//
// Every field is set by a constructor below; kind is the one those constructors
// exist to get right (see launchKind).
type paneLaunch struct {
	kind      launchKind
	envPrefix string
	program   string
	args      []string
}

// launchKind records which KIND of program a launch names. It is not a quoting
// style knob: it decides how the program word is rendered AND which recipe the
// launch runs under (see paneCommandFor), and getting it backwards breaks the
// launch in a way no mocked test would notice - an alias does not exist in the
// non-interactive shell tmux runs a pane command in, and an unquoted resolved
// path splits on the first space inside it.
//
// Its zero value carries a second load-bearing property: "empty" is the zero
// KIND, not a property of the other fields. Every constructor below sets a
// non-zero kind, so only a launch nobody constructed is empty. A malformed
// launch - arguments but no program, the shape a resolution handing over an
// empty path would produce - is therefore NOT empty: it still renders, and
// fails visibly in the pane, instead of collapsing to "" and taking the prompt
// with it. That collapse is this cycle's original bug (success reported, prompt
// never delivered, nothing logged), and ADR-0011 and ADR-0016 both prefer a
// launch that fails visibly to one that vanishes. Keying emptiness on the
// program alone made it representable again; keying it on the kind cannot
// (CLAUDE.md §4).
type launchKind int

const (
	// launchNone is the zero value: no command at all. The shell pane, which
	// gets the shell tmux would have started anyway.
	launchNone launchKind = iota

	// launchAlias names a program the shell running it must resolve - a devgeta
	// shell alias ("cc"/"oc") or a bare binary name ("nvim"). See aliasLaunch.
	launchAlias

	// launchBinary names a resolved absolute path a pane execs directly. See
	// binaryLaunch.
	launchBinary
)

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
// and the inner script of interactivePaneCommand - which is also where
// paneCommandFor routes it, because that is the only shell that has the alias.
func aliasLaunch(program string, args ...string) paneLaunch {
	return paneLaunch{kind: launchAlias, program: program, args: args}
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
//
// An empty path is a caller bug, not a shell pane: a resolution that produced no
// path must select the alias form and the interactive recipe (ADR-0020 part 3 -
// the resolved path is an optimization, and its absence is what the fallback
// exists for), never hand "" over here. This constructor cannot refuse it - Go
// has no non-empty string type and this value has no error channel - so the
// launch is built anyway and the failure is made LOUD rather than silent: the
// kind is set, so the launch is not empty, so it renders as an empty program
// word followed by its quoted arguments, and the pane reports a command it could
// not run with the prompt visible beside it. The prompt is never quietly
// discarded. See launchKind.
func binaryLaunch(path string, args ...string) paneLaunch {
	return paneLaunch{kind: launchBinary, program: path, args: args}
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

// isEmpty reports whether this launch has no command to run: the zero value,
// and only the zero value - the shell pane.
//
// It keys on the KIND, not on the program, and that distinction is the whole
// point. Keying it on `program == ""` made a launch with arguments but no
// program (binaryLaunch("", "fix issue 1082")) read as "no command at all", so
// it rendered to "" and the pane became a bare shell with the prompt gone and
// nothing logged - this cycle's original bug reached by a second route. With the
// kind, that launch is not empty and cannot become "". See launchKind.
func (l paneLaunch) isEmpty() bool { return l.kind == launchNone }

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
	if l.kind == launchBinary {
		parts = append(parts, shellSingleQuote(l.program))
	} else {
		parts = append(parts, l.program)
	}
	for _, arg := range l.args {
		parts = append(parts, shellSingleQuote(arg))
	}
	return strings.Join(parts, " ")
}

// paneCommandFor returns the shell-command tmux should run as a created pane's
// process for launch, choosing the recipe from the launch's KIND:
//
//	launchBinary -> execPaneCommand        - <rendered>; exec '<shell>'
//	launchAlias  -> interactivePaneCommand - '<shell>' -ic '<rendered>; exec '<shell>' -i'
//	launchNone   -> ""                     - no command; the pane keeps the shell
//	                                         tmux would have started anyway
//
// This is the only place a launch is paired with a recipe, which is the point:
// the two are NOT interchangeable, and the wrong pairing used to be something
// any caller could write. tmux runs a pane's shell-command through a
// NON-INTERACTIVE shell (measured: flags `569X`, no `i`), which has no aliases -
// so an alias launch sent through the exec recipe emits `cc 'fix it'; exec
// '/bin/zsh'` and dies on `command not found`, which is exactly the case
// ADR-0020 part 3's interactive fallback exists to serve. In the other
// direction, a resolved absolute path needs none of the user's interactive
// startup and should not pay for it (ADR-0020 rejects `-ic` as the default for
// precisely that reason). Routing on the discriminator the value already carries
// makes both mistakes unrepresentable rather than merely avoidable
// (CLAUDE.md §4).
//
// shell must come from resolveShell - both recipes interpolate it, twice in the
// interactive one.
//
// A --pane value does NOT come through here: it is a raw user command line, not
// a paneLaunch at all, and it goes to interactivePaneCommand directly (ADR-0011
// keeps it unparsed and unsplit).
func paneCommandFor(launch paneLaunch, shell string) string {
	switch launch.kind {
	case launchBinary:
		return execPaneCommand(launch.render(), shell)
	case launchAlias:
		return interactivePaneCommand(launch.render(), shell)
	default:
		// launchNone: the shell pane. Passing no command at all is what gives
		// the pane the shell tmux would have started on its own - today's
		// behavior exactly. Wrapping nothing in a recipe would instead run an
		// empty command first.
		return ""
	}
}

// execPaneCommand returns the shell-command tmux should run as a created pane's
// process for an already-rendered devgeta-owned launch whose binary path
// resolved:
//
//	<command>; exec '<shell>'
//
// paneCommandFor is its only caller, and deliberately so: it takes the rendered
// string rather than a paneLaunch so that "which recipe does this launch get"
// is answered in exactly one place (see paneCommandFor for why an alias launch
// must never reach this recipe).
//
// The trailing `exec` preserves today's pane lifetime: exec'ing the command
// alone would close the pane when it exits, and the last pane closing takes the
// window with it - so quitting your coder would destroy the window instead of
// dropping you at a shell (both measured, ADR-0020 part 2).
//
// shell must come from resolveShell: it is interpolated into the command and
// only that resolution guarantees it is an existing, executable, absolute path
// rather than a $SHELL pointing at something uninstalled.
//
// A blank command yields "" rather than a bare `; exec '<shell>'`, whose first
// statement would be empty.
func execPaneCommand(command, shell string) string {
	if strings.TrimSpace(command) == "" {
		return ""
	}
	return command + "; exec " + shellSingleQuote(shell)
}

// interactivePaneCommand returns the shell-command tmux should run as a created
// pane's process for a script that needs the user's INTERACTIVE shell:
//
//	'<shell>' -ic '<script>; exec '<shell>' -i'
//
// Two kinds of script land here, and one mechanism serving both is deliberate
// (ADR-0020 part 3) rather than one of them being a special case:
//
//   - A user-authored --pane value, passed to this function DIRECTLY. It is a
//     command line the user wrote for their own shell and may use their own
//     aliases and functions, so running it non-interactively would silently
//     change what it means. It is not a paneLaunch and is never turned into one:
//     ADR-0011 keeps it unparsed and unsplit.
//   - A devgeta-owned alias-form launch, when the preflight probe could not
//     resolve an absolute path, routed here by paneCommandFor. The alias (cc/oc)
//     only exists in an interactive shell, so this is what keeps an inconclusive
//     probe costing the pane nothing - ADR-0016's fail-open, preserved.
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
// yields "" for the same reason a blank command does there.
func interactivePaneCommand(script, shell string) string {
	if strings.TrimSpace(script) == "" {
		return ""
	}
	quotedShell := shellSingleQuote(shell)
	return quotedShell + " -ic " + shellSingleQuote(script+"; exec "+quotedShell+" -i")
}
