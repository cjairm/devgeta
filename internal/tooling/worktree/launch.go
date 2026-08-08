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
//     "CLAUDE_CODE_NO_FLICKER=1 claude", "opencode --prompt '...'", "nvim", or a
//     raw --pane value. The repair path and `dg wt move`'s retarget still send
//     exactly that with send-keys, because those panes already exist (ADR-0020
//     part 4). It names the BINARY, not the cc/oc alias: devgeta.zsh's alias is
//     for the user to type, and a send-keys path that relied on it would launch
//     something the preflight probe never checked (ADR-0020's 2026-08-07
//     amendment).
//   - The recipes below are the form EXEC'd as a created pane's process. They
//     are what a create path passes to tmux.
//
// This file owns the second one, plus the structured launch value both are
// rendered from. paneCommandFor is the single door from a launch to a pane
// command: it picks the recipe from the launch's kind, so a create path never
// pairs the two by hand.

package worktree

import "strings"

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
// launch in a way no mocked test would notice - a bare name gets no PATH repair
// in the non-interactive shell tmux runs a pane command in, and an unquoted
// resolved path splits on the first space inside it.
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

	// launchName names a program the shell running it must resolve for itself -
	// a bare command name ("claude", "opencode", "nvim"). See nameLaunch.
	launchName

	// launchBinary names a resolved absolute path a pane execs directly. See
	// binaryLaunch.
	launchBinary
)

// nameLaunch builds a launch whose program is a NAME the shell running it must
// resolve for itself: "claude", "opencode", "nvim". This is the form typed into
// (or run by) an interactive shell - Pane.Command, and the inner script of
// interactivePaneCommand, which is where paneCommandFor routes it.
//
// It used to be aliasLaunch, and its program used to be the cc/oc devgeta alias.
// It is the binary name now (ADR-0020's 2026-08-07 amendment): a send-keys path
// launching the alias launched something the preflight probe - which checks the
// binary - never verified, so a coder on PATH with no devgeta alias passed the
// check and then failed in the pane.
//
// The program word is NOT quoted, and the reason changed with the rename.
// Quoting no longer BREAKS a plain binary name - `'claude'` is still resolved on
// PATH - so this is a choice, with one case behind it: an interactive-form launch
// is what a probe that answered with something non-path-shaped selects, and
// "alias text" and "a shell function name" are exactly those answers (ADR-0020
// part 3). In that case the user's own `alias claude=...` is what has to expand,
// and a quoted program word suppresses alias expansion. Arguments are still
// quoted; only the program word is special.
func nameLaunch(program string, args ...string) paneLaunch {
	return paneLaunch{kind: launchName, program: program, args: args}
}

// nameLaunchWithEnv is nameLaunch plus a devgeta-owned environment prefix - the
// typed/interactive counterpart of binaryLaunchWithEnv, and what a coder recipe
// that carries an env prefix needs in BOTH of its forms now that the typed form
// is the binary rather than an alias that carried the prefix in its own
// definition (ADR-0020's 2026-08-07 amendment).
func nameLaunchWithEnv(envPrefix, program string, args ...string) paneLaunch {
	return withEnvPrefix(nameLaunch(program, args...), envPrefix)
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
// path must select the name form and the interactive recipe (ADR-0020 part 3 -
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
// (today only claude's, constants.ClaudeLaunch.EnvPrefix - the same value that
// renders into devgeta.zsh's `alias cc=` line, so the alias a user types and the
// command devgeta runs cannot carry different environments).
//
// Both forms take a prefix now. While the typed form was the cc alias, the prefix
// lived inside the alias DEFINITION, so an alias launch carrying a second one
// would have double-set it and the prefix was deliberately kept off that
// constructor. The typed form is the binary itself since ADR-0020's 2026-08-07
// amendment, so it has to spell the prefix out exactly as the exec form does -
// see nameLaunchWithEnv.
func binaryLaunchWithEnv(envPrefix, path string, args ...string) paneLaunch {
	return withEnvPrefix(binaryLaunch(path, args...), envPrefix)
}

// withEnvPrefix attaches a devgeta-owned environment assignment to a launch. It
// is the one writer of that field, shared by the two WithEnv constructors so
// neither can set it differently.
func withEnvPrefix(launch paneLaunch, envPrefix string) paneLaunch {
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
// The env prefix is emitted unquoted, and it has to be: it is a shell variable
// ASSIGNMENT, and `'CLAUDE_CODE_NO_FLICKER=1' claude` asks the shell for a command
// literally named `CLAUDE_CODE_NO_FLICKER=1`. It needs no quoting either way -
// every prefix is a devgeta constant in pkg/constants, never user data
// (ADR-0020's quoting table).
//
// EVERY argument is quoted, flags included - so opencode's prompt form renders
// as `opencode '--prompt' 'text'`, not `opencode --prompt 'text'`. Those are shell
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
	// An EMPTY program word is quoted whichever kind it is, so it survives as a
	// literal word the shell then fails to resolve. Left unquoted it disappears
	// from the command line entirely: nameLaunch("") renders to "" (the vanish
	// isEmpty's kind check exists to prevent, reached one layer down), and
	// nameLaunch("", "fix issue 1082") renders to " 'fix issue 1082'", which
	// makes the PROMPT the command being run. Quoting it mirrors what
	// binaryLaunch("") already did and keeps a caller bug loud in the pane
	// rather than silent. No production launch can hit this - every program
	// value is a devgeta constant - so this is the invariant holding, not a live
	// bug (see binaryLaunch on why the constructors cannot refuse an empty
	// program outright).
	if l.kind == launchBinary || l.program == "" {
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
//	launchName   -> interactivePaneCommand - '<shell>' -ic '<rendered>; exec '<shell>' -i'
//	launchNone   -> ""                     - no command; the pane keeps the shell
//	                                         tmux would have started anyway
//
// This is the only place a launch is paired with a recipe, which is the point:
// the two are NOT interchangeable, and the wrong pairing used to be something
// any caller could write. A name launch is one whose program NOTHING has resolved
// - it is what a probe that came back without a path selects - and tmux runs a
// pane's shell-command through a NON-INTERACTIVE shell (measured: flags `569X`,
// no `i`), which reads no `.zshrc` and therefore gets none of its PATH repair.
// bash has no unconditional equivalent of zsh's `.zshenv` either ($BASH_ENV is
// unset by default), so `claude 'fix it'; exec '/bin/zsh'` can die on `command
// not found` for a tool that launches fine from the user's own shell - exactly
// the case ADR-0020 part 3's interactive fallback exists to serve. In the other
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
	case launchName:
		return interactivePaneCommand(launch.render(), shell)
	default:
		// launchNone: the shell pane. Passing no command at all is what gives
		// the pane the shell tmux would have started on its own - today's
		// behavior exactly. Wrapping nothing in a recipe would instead run an
		// empty command first.
		return ""
	}
}

// creationCommand returns the shell-command tmux should run as this pane's
// process when devgeta CREATES the pane, using shell (which must come from
// resolveShell). It is the counterpart of Pane.Command, never a replacement for
// it: Command stays the form TYPED into a pane that already exists (the repair
// path, `dg wt move`'s retarget - ADR-0020 part 4).
//
// Three kinds of pane, decided by what the pane's constructor put on it rather
// than by inspecting its Command string:
//
//   - launch != nil - a devgeta-owned pane (a coder, the editor, a reviewer).
//     The launch closure is handed the probe's resolution and this pane's prompt
//     text, and IT decides exec-vs-interactive by whether the path is empty
//     (see layout.go's launchFor). That is the one probe's answer being spent,
//     not a second lookup: ADR-0020 requires the command that runs to be built
//     from the check's own result, and nothing here re-probes.
//   - launch == nil with a non-empty Command - a user-authored --pane value. It
//     goes to the interactive recipe UNPARSED and UNSPLIT (ADR-0011): it is a
//     command line the user wrote for their own shell, which may use their own
//     aliases and functions.
//   - launch == nil with an empty Command - the shell pane. No command at all,
//     so tmux starts the pane's shell and nothing else, exactly as today.
//
// Reading the resolution off the pane (rather than taking it as a parameter) is
// what keeps one create's resolution from reaching another: it was written onto
// a CLONE by Layout.EnsureInstalled, and clone gives every create its own
// backing array (see Layout.clone).
func (p Pane) creationCommand(shell string) string {
	if p.launch != nil {
		return paneCommandFor(p.launch(p.resolvedPath, p.promptText), shell)
	}
	if p.Command != "" {
		return interactivePaneCommand(p.Command, shell)
	}
	return ""
}

// execPaneCommand returns the shell-command tmux should run as a created pane's
// process for an already-rendered devgeta-owned launch whose binary path
// resolved:
//
//	<command>; exec '<shell>'
//
// paneCommandFor is its only caller, and deliberately so: it takes the rendered
// string rather than a paneLaunch so that "which recipe does this launch get"
// is answered in exactly one place (see paneCommandFor for why a name launch
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
//   - A devgeta-owned NAME-form launch, when the preflight probe could not
//     resolve an absolute path, routed here by paneCommandFor. The interactive
//     shell is what gives that bare name the `.zshrc` PATH repair the probe's own
//     shell had - and, when the probe's non-path answer was alias text or a shell
//     function name, the only shell where that definition exists at all. This is
//     what keeps an inconclusive probe costing the pane nothing - ADR-0016's
//     fail-open, preserved. It is NOT about devgeta's own cc/oc alias, which no
//     devgeta launch has named since ADR-0020's 2026-08-07 amendment.
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
