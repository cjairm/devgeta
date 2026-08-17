// Layout model and built-in registry for `dg ws` window creation/repair.
//
// This file owns the layout resolution contract that later steps (tmux
// window building, the TUI's N picker, and CLI --layout flags) all call
// into. See ResolveLayout's doc comment for the precedence ladder and,
// critically, for what a caller must pass as aiAlias to get it right.

package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/internal/config"
)

// nvimCommand is the shell command used to launch Neovim in a tmux pane.
// Neovim has no AICoder wrapper (it isn't an AI coder), so built-in layouts
// that include an editor pane reference this constant directly instead of
// each hardcoding the string "nvim".
const nvimCommand = "nvim"

// splitVertical is tmux-pane-split direction "vertical" in this codebase's
// vocabulary: panes SIDE BY SIDE (tmux's own -h flag - see tmux.SplitWindow for
// why the naming is inverted from tmux's). It's a constant because two places
// need the same value (the claude-nvim built-in and WithExtraPanes), and a typo
// in either would only surface as a runtime "unknown split direction" error
// from tmux.
const splitVertical = "vertical"

// Pane describes a single tmux pane within a Layout: the command to run in
// it, and how it should be split off from the previous pane. Split is empty
// for the first pane in a layout (there is nothing to split from yet) and
// "vertical" or "horizontal" for every subsequent pane.
//
// Command is the form TYPED into a pane that ALREADY EXISTS, which since
// ADR-0021 is exactly two paths: ensureWindow's repair branch, and
// launchReviewInLiveWindow's idle-shell reuse (ADR-0021 part 4). A pane devgeta
// CREATES never receives it - it gets a different rendering, built by
// creationCommand (launch.go) from the fields below and exec'd as the pane's
// process. Two representations, both intentional; neither is a substitute for
// the other.
//
// check, prompt and launch are the pane's three behaviors, and they are
// unexported for the same reason: the exported shape of a Pane is just what tmux
// needs to build it. They live ON the pane rather than in slices parallel to
// Layout.Panes so they cannot fall out of step with the pane they describe -
// a mismatch used to be possible and was caught by a length check in a
// constructor; now it cannot be written at all (CLAUDE.md §4: prefer
// structurally impossible over guarded).
//
//   - check verifies the pane's underlying tool is installed AND returns the
//     absolute path the probe resolved for it (empty when it resolved none - see
//     ensureToolInstalled). nil means this pane has nothing to check (see
//     WithExtraPanes for one such case).
//   - prompt renders the pane's typed command with an opening prompt, for
//     `dg wt create --prompt`. nil means this pane does not take a prompt -
//     true for every non-AI pane, and load-bearing: WithPrompt uses nil to
//     tell a coder pane apart from an editor pane, so it never turns `nvim`
//     into `nvim 'explain issue 1082'` (which opens a file by that name).
//   - launch builds the structured launch a CREATED pane execs, from the probe's
//     resolution and the prompt text. nil means this pane is not devgeta-owned:
//     a --pane value or a bare shell (see creationCommand).
//
// resolvedPath and promptText are the two pieces of per-create state, and both
// are written onto a CLONE (by Layout.EnsureInstalled and WithPrompt), never
// onto a layout a caller still holds. That matters because `dg ws` resolves a
// layout ONCE and creates from it repeatedly: without the clone, one worktree's
// resolution or prompt would leak into the next (see Layout.clone).
//
// promptText is stored alongside Command rather than derived from it because the
// two forms need the prompt differently: Command needs it already rendered into
// the typed string ("CLAUDE_CODE_NO_FLICKER=1 claude 'fix it'"), while the exec
// form needs the RAW text so it
// can quote it as its own argv element. Recovering the raw text by unquoting
// Command would be parsing devgeta's own output back, and would break on the
// escape path (an embedded single quote).
//
// Carrying all of this as constructor-time state also avoids having to
// reverse-engineer "what checks this pane" from a bare Command string later
// (e.g. distinguishing the literal command "CLAUDE_CODE_NO_FLICKER=1 claude"
// from "nvim" by string matching, which would break the moment either command
// string changes).
type Pane struct {
	Command string
	Split   string

	check  func() (resolvedPath string, err error)
	prompt func(prompt string) string
	launch func(resolvedPath, prompt string) paneLaunch

	resolvedPath string
	promptText   string
}

// Layout is a named collection of panes describing a tmux window shape for
// `dg ws` create/repair. The exported shape is just {Name, Panes}; everything
// a pane knows how to do lives on the Pane itself.
type Layout struct {
	Name  string
	Panes []Pane
}

// pane0CreatedCommand returns pane 0's CREATED command - the form tmux execs as
// the pane's process when devgeta creates it (Pane.creationCommand). This is
// NOT the form typed into a pane that already exists; see pane0TypedCommand for
// that one and Pane's doc comment for why the two are different strings for the
// same pane. Every `layout.Panes[0]` index in worktree.go goes through this
// accessor or pane0TypedCommand instead of indexing directly.
//
// Returns "" for an empty layout (len(l.Panes) == 0) rather than an error or a
// (string, bool) result. No caller can reach an empty layout today: every
// validateLayout call site gates on len(Panes) > 0 before this could run, and
// LaunchReviewInRepo always builds a one-pane layout - so a new error path here
// would be unreachable code. If that ever changed, "" is still the right
// fallback: it's already the shell pane's legitimate value - creationCommand
// returns "" for a pane with no launch and no Command, and tmux then starts the
// pane's own shell and nothing else. That beats a panic. The cost of that
// choice: an empty layout becomes indistinguishable from a shell pane, since
// both yield "" here, and validateLayout is the only thing keeping them apart.
// Concretely, if a future caller reached createWindowWithLayout with an empty
// layout, this returns "", buildWindowPanes' len(layout.Panes) > 1 guard skips
// the loop, and the user gets an empty tmux window with no error anywhere -
// where today an empty layout would panic in the first test that tried it.
func (l Layout) pane0CreatedCommand(shell string) string {
	if len(l.Panes) == 0 {
		return ""
	}
	return l.Panes[0].creationCommand(shell)
}

// pane0TypedCommand returns pane 0's TYPED command (Pane.Command) - the form
// send-keys writes into a pane that already exists. This is NOT the form tmux
// execs when creating a pane; see pane0CreatedCommand for that one and Pane's
// doc comment for why the two are different strings for the same pane. Every
// `layout.Panes[0]` index in worktree.go goes through this accessor or
// pane0CreatedCommand instead of indexing directly.
//
// Returns "" for an empty layout (len(l.Panes) == 0) rather than an error or a
// (string, bool) result. No caller can reach an empty layout today: every
// validateLayout call site gates on len(Panes) > 0 before this could run, and
// LaunchReviewInRepo always builds a one-pane layout - so a new error path here
// would be unreachable code. If that ever changed, "" is still the right
// fallback: it makes ensureWindow's repair branch a no-op, which is exactly
// what it already does for the shell layout (see ensureWindow). The cost of
// that choice is the same one pane0CreatedCommand's doc comment spells out: an
// empty layout becomes indistinguishable from a shell pane, and validateLayout
// is the only thing keeping them apart.
func (l Layout) pane0TypedCommand() string {
	if len(l.Panes) == 0 {
		return ""
	}
	return l.Panes[0].Command
}

// launchFor builds a Pane.launch closure from a tool's two launch forms, and it
// is the ONE place the choice between them is made: an empty resolvedPath means
// the probe produced no usable path, so the pane runs the interactive form,
// which is what keeps an inconclusive probe from costing the pane anything
// (ADR-0016's fail-open, ADR-0021 part 3's fallback). A resolved path takes the
// exec form.
//
// Every devgeta-owned pane goes through this rather than writing the branch
// itself: the fallback is the case the ADR calls easy to get wrong (launching the
// bare name in tmux's NON-interactive shell is not the same as launching it in
// the user's own - that shell reads no `.zshrc`, so it gets none of its PATH
// repair, and none of the user's own aliases or functions either), so there is
// exactly one copy of it to be right.
func launchFor(
	interactiveForm func(prompt string) paneLaunch,
	execForm func(binaryPath, prompt string) paneLaunch,
) func(resolvedPath, prompt string) paneLaunch {
	return func(resolvedPath, prompt string) paneLaunch {
		if resolvedPath == "" {
			return interactiveForm(prompt)
		}
		return execForm(resolvedPath, prompt)
	}
}

// coderPane builds the pane that launches an AI coder. All four of the pane's
// facets - the typed command, the install check, the prompt form, and the
// created-pane launch - are read off the one AICoder, so they cannot describe
// different tools.
func coderPane(coder AICoder, split string) Pane {
	return Pane{
		Command: coder.Command(),
		Split:   split,
		check:   coder.EnsureInstalled,
		prompt:  coder.PromptCommand,
		launch:  launchFor(coder.interactiveLaunch, coder.execLaunch),
	}
}

// nvimPane builds the editor pane. prompt is deliberately left nil: nvim is not
// an AI coder and has no notion of an opening prompt, so a layout containing
// only this pane must reject --prompt rather than pass the text to nvim as a
// filename.
func nvimPane(split string) Pane {
	return Pane{
		Command: nvimCommand,
		Split:   split,
		check:   ensureNvimInstalled,
		launch:  launchFor(nvimInteractiveLaunch, nvimExecLaunch),
	}
}

// nvimInteractiveLaunch and nvimExecLaunch are nvim's two launch forms, standing
// in for the AICoder methods nvim has no wrapper to provide (it isn't an AI
// coder). Both DROP the prompt argument, and that is the point rather than an
// oversight: `nvim 'explain issue 1082'` opens a file by that name. Pane.prompt
// being nil already keeps WithPrompt away from this pane, so these two are the
// second, structural guard on the same mistake - the one that holds even if a
// caller hands a prompt to a launch closure directly.
//
// The interactive form is a nameLaunch: the shell running it has to resolve
// "nvim" itself, which is the property that name form describes (an unquoted
// program word, routed to the interactive recipe).
func nvimInteractiveLaunch(string) paneLaunch { return nameLaunch(nvimCommand) }

func nvimExecLaunch(binaryPath, _ string) paneLaunch { return binaryLaunch(binaryPath) }

// shellPane builds a pane that launches nothing: you get the shell tmux
// already started in the worktree directory. All four of Pane's behaviors are
// zero on purpose, and each says something:
//
//   - Command is empty, so there is nothing for ensureWindow's repair branch to
//     type into a live window either - repairing a shell window is a no-op
//     rather than a bare Enter pressed into whatever pane is active.
//   - check is nil because there is nothing to install. A shell is the one
//     "tool" every layout already depends on to run the others.
//   - prompt is nil, so `--prompt` correctly rejects a shell-only layout
//     instead of typing the prompt text at a bash prompt.
//   - launch is nil, and with an empty Command that makes creationCommand
//     return no command at all - so a created shell pane gets the shell tmux
//     would have started anyway, rather than a recipe wrapped around nothing.
func shellPane(split string) Pane {
	return Pane{Split: split}
}

// EnsureInstalled verifies every pane's underlying tool is present, so a
// layout referencing a missing tool fails with one actionable message
// before the caller touches tmux (building the window is a later step's
// job, not this file's). A pane with no check (check == nil) is skipped.
//
// It returns a RESOLVED COPY of the layout: each checked pane's probe also
// resolves the absolute path of the tool it found, and that path is written onto
// the copy's pane so the pane's created command can be built from it. This is
// ADR-0021's "one probe per pane per create" - the check and the launch share one
// answer instead of probing twice and possibly disagreeing. Nothing downstream
// re-probes; creationCommand only reads what this wrote.
//
// The resolution lands on a clone, never on the receiver, because `dg ws`
// resolves a layout once and creates from it repeatedly - see clone.
func (l Layout) EnsureInstalled() (Layout, error) {
	out := l.clone()
	for i := range out.Panes {
		pane := &out.Panes[i]
		if pane.check == nil {
			continue
		}
		resolvedPath, err := pane.check()
		if err != nil {
			return Layout{}, fmt.Errorf("layout %q, pane %d: %w", l.Name, i+1, err)
		}
		// May be empty, and that is a normal answer, not a failure: the pane
		// then takes the interactive fallback (see launchFor).
		pane.resolvedPath = resolvedPath
	}
	return out, nil
}

// clone returns a copy of l whose Panes sit in a fresh backing array, so a
// transformation below can never write through to the receiver's slice.
// Callers hold resolved layouts and reuse them (the TUI resolves once and
// creates repeatedly), so an in-place edit would leak one worktree's prompt,
// extra panes, or probe resolution into the next.
//
// A Pane is copied by value and holds no pointers or slices of its own, so this
// one-level copy is a full one: promptText and resolvedPath are strings, and
// each of the three behaviors is a func value the copy shares with the original.
//
// Sharing those func values is safe because of a property the CONSTRUCTORS hold,
// not because the closures are argument-only - reviewerPane's two launch
// closures capture the reviewer's agent name and its OpenCodeCoder, and coderPane
// captures its coder through the method values it takes. The invariant to check a
// new behavior against is therefore: whatever a pane's closure captures is fixed
// at construction, immutable, and belongs to that pane alone. A closure that
// captured mutable state, or state shared with another pane, would be shared
// through this copy and needs a deeper one - as would adding a reference type to
// Pane itself.
func (l Layout) clone() Layout {
	out := l
	out.Panes = append([]Pane(nil), l.Panes...)
	return out
}

// promptableLayoutNames lists the built-in layouts that have a pane able to
// take an opening prompt, in the registry's stable order. It's derived from the
// registry rather than hardcoded so WithPrompt's error message cannot drift
// from what the layouts actually support.
func promptableLayoutNames() []string {
	layouts := builtinLayouts()
	names := make([]string, 0, len(builtinLayoutNames))
	for _, name := range builtinLayoutNames {
		for _, pane := range layouts[name].Panes {
			if pane.prompt != nil {
				names = append(names, name)
				break
			}
		}
	}
	return names
}

// WithPrompt returns a copy of l whose AI-coder pane launches with prompt as
// its opening message, so the coder is already working when the user attaches
// instead of sitting at an empty session. The prompt is delivered as a launch
// argument, never as keystrokes typed into a booted TUI - see ADR-0011.
//
// An empty prompt returns l unchanged with no error, so a caller can apply this
// unconditionally without first testing whether the user passed --prompt.
//
// It errors rather than doing something approximate in both awkward cases:
//
//   - No pane takes a prompt (e.g. the nvim-only layout): fail loudly. Silently
//     dropping the prompt would leave a session that looks correctly created
//     but was never given its task.
//   - More than one pane takes a prompt: ambiguous. No such layout exists
//     today; this guard means the first one added fails visibly instead of
//     prompting whichever pane happens to come first.
func (l Layout) WithPrompt(prompt string) (Layout, error) {
	if prompt == "" {
		return l, nil
	}

	target := -1
	for i, pane := range l.Panes {
		if pane.prompt == nil {
			continue
		}
		if target != -1 {
			return Layout{}, fmt.Errorf(
				"layout %q has more than one AI coder pane (panes %d and %d), "+
					"so it is ambiguous which one --prompt should start",
				l.Name, target+1, i+1,
			)
		}
		target = i
	}

	if target == -1 {
		return Layout{}, fmt.Errorf(
			"layout %q has no AI coder pane, so there is nothing for --prompt to start. "+
				"Layouts that accept a prompt: %s",
			l.Name, strings.Join(promptableLayoutNames(), ", "),
		)
	}

	out := l.clone()
	// Both forms of the prompt are recorded, because the two representations
	// need it differently: Command gets it RENDERED into the typed string
	// (`CLAUDE_CODE_NO_FLICKER=1 claude 'fix it'`), while a created pane's exec
	// form needs the RAW text so it
	// can quote it as its own argv element. See Pane's doc comment on why the raw
	// text is not recovered from Command by unquoting it.
	out.Panes[target].Command = out.Panes[target].prompt(prompt)
	out.Panes[target].promptText = prompt
	return out, nil
}

// WithExtraPanes returns a copy of l with one additional shell pane per
// command, for `dg wt create --pane` - the bootstrap command a worktree usually
// needs running next to the coder (`make finit`, a dev server).
//
// Each command is used AS WRITTEN, not shell-quoted. Unlike a prompt (one
// literal argument handed to a coder), a --pane value IS a shell command line:
// quoting it would break the compound commands that make the flag worth having,
// e.g. `cd api && make dev`. The user is handing devgeta a command to run in
// their own shell - the same trust level as a shell alias. See ADR-0011 for the
// full reasoning on the asymmetry.
//
// An empty or whitespace-only command is an error. `--pane "$BOOTSTRAP"` with an
// unset variable is a far likelier cause than a deliberate request for an idle
// shell, and an empty value would otherwise reach creationCommand as a pane with
// no command at all - which is the shell pane's own shape, so the pane would come
// up as a bare shell that looks like the feature half-worked (nothing send-keys a
// --pane value any more; creationCommand's empty-Command branch and
// interactivePaneCommand's blank-script guard are the two places it would be
// swallowed). Validation lives here rather than in the CLI so every present and
// future caller inherits it.
//
// These panes get no install check (check stays nil, which EnsureInstalled
// skips) on purpose: the command can be a shell builtin, a compound, or a
// Makefile target, so probing its first token would reject legitimate commands.
// A built-in layout's pane is checked because devgeta chose that command; this
// one is the user's, and its own pane shows any error.
//
// launch stays nil for the same reason, and that is what routes the value to the
// INTERACTIVE recipe as one unparsed script when the pane is created (see
// creationCommand): there is nothing to resolve here, because devgeta is not
// parsing the user's command line and does not need to.
//
// Every appended pane splits "vertical" (side by side), matching the
// claude-nvim built-in. With two or more extra panes each splits the previous
// one, so they get progressively narrower - existing buildWindowPanes behavior.
// The common case (coder plus one shell) is a clean 50/50.
func (l Layout) WithExtraPanes(commands []string) (Layout, error) {
	if len(commands) == 0 {
		return l, nil
	}

	out := l.clone()
	for _, command := range commands {
		if strings.TrimSpace(command) == "" {
			return Layout{}, fmt.Errorf(
				"--pane needs a command to run, got an empty one " +
					"(a --pane value that came from a shell variable is the usual cause; " +
					"check that it is set)",
			)
		}
		out.Panes = append(out.Panes, Pane{Command: command, Split: splitVertical})
	}
	return out, nil
}

// ensureNvimInstalled checks that nvim resolves in the user's interactive
// shell. Neovim has no AICoder wrapper (it isn't an AI coder), so it can't call
// an existing coder.EnsureInstalled() directly - but it reuses the same
// ensureToolInstalled helper aicoder.go's OpenCodeCoder/ClaudeCoder use (an
// interactive-shell probe, not exec.LookPath - see ensureToolInstalled for
// why), rather than a third hand-rolled copy. Neovim installs under the
// "terminal" category (see internal/tooling/terminal/terminal.go), matching the
// hint ensureToolInstalled already gives for opencode/claude.
func ensureNvimInstalled() (string, error) {
	// nvim never had a cc/oc-style alias indirection: nvimCommand IS the binary,
	// which is what every check probes and every launch names (ADR-0021 part 3,
	// rule 1).
	return ensureToolInstalled(nvimCommand)
}

// builtinLayoutNames lists the valid layout names in a stable order, used
// both to build the registry and to render "valid layouts" in error
// messages.
var builtinLayoutNames = []string{"opencode", "claude", "claude-nvim", "nvim", "shell"}

// builtinLayouts returns the registry of layouts ResolveLayout can return by
// name. It's rebuilt on every call (cheap: four small structs) rather than
// cached as a package var, so each caller gets its own AICoder instances -
// there is no shared mutable state to worry about.
//
// Each pane is built by coderPane/nvimPane rather than as a bare struct
// literal, so a pane's command, install check, and prompt form always come
// from one source and cannot describe different tools.
func builtinLayouts() map[string]Layout {
	opencode := &OpenCodeCoder{}
	claude := &ClaudeCoder{}

	return map[string]Layout{
		"opencode": {
			Name:  "opencode",
			Panes: []Pane{coderPane(opencode, "")},
		},
		"claude": {
			Name:  "claude",
			Panes: []Pane{coderPane(claude, "")},
		},
		"claude-nvim": {
			Name: "claude-nvim",
			Panes: []Pane{
				coderPane(claude, ""),
				nvimPane(splitVertical),
			},
		},
		"nvim": {
			Name:  "nvim",
			Panes: []Pane{nvimPane("")},
		},
		"shell": {
			Name:  "shell",
			Panes: []Pane{shellPane("")},
		},
	}
}

// Reviewer describes one of the reviewer agents `dg ws`'s (future) R
// keybinding can launch: the OpenCode agent name passed to `opencode --agent
// <name>`, and a human-readable label for the picker UI.
type Reviewer struct {
	Agent string
	Label string
}

// reviewerKeys lists the valid reviewer picker keys in a stable order,
// "code" first per the cycle plan (the common case) - used both to build the
// registry and, later, to render the picker in a fixed order.
var reviewerKeys = []string{"code", "document", "skill"}

// builtinReviewers returns the registry of reviewer agents the (future) R
// keybinding can launch, keyed by short picker key. Each Reviewer.Agent must
// equal a filename (minus ".md") in configs/shared/agents/ -
// TestBuiltinReviewersAgentNamesMatchAgentFiles (this package, on-disk read)
// and TestBuiltinReviewersAgentNamesMatchEmbeddedAgentFiles (package main,
// embedded ConfigsFS read) both enforce that, per CLAUDE.md's "Changing an
// embedded config" rule: a constraint an external tool imposes (here,
// `opencode --agent <name>` failing silently on a typo) gets enforced by a
// test, because a comment would not survive future edits.
func builtinReviewers() map[string]Reviewer {
	return map[string]Reviewer{
		"code":     {Agent: "code-reviewer", Label: "code — bugs, security"},
		"document": {Agent: "document-reviewer", Label: "document — plans, specs"},
		"skill":    {Agent: "skill-reviewer", Label: "skill — agents/commands"},
	}
}

// ReviewerAgentNames returns the OpenCode agent name for each built-in
// reviewer, in reviewerKeys order. Exported so the root package's
// TestBuiltinReviewersAgentNamesMatchEmbeddedAgentFiles can assert the same
// names against the embedded configs/shared/agents/ filenames that this
// package's own on-disk test (TestBuiltinReviewersAgentNamesMatchAgentFiles)
// checks - embedded.go's ConfigsFS lives in package main, which this package
// cannot import without an import cycle, so package main needs its own way
// to see these names.
func ReviewerAgentNames() []string {
	reviewers := builtinReviewers()
	names := make([]string, 0, len(reviewerKeys))
	for _, key := range reviewerKeys {
		names = append(names, reviewers[key].Agent)
	}
	return names
}

// ReviewPrompt is the single fixed opening prompt sent to every reviewer
// agent, regardless of which one (code/document/skill) was picked, and
// regardless of how it is launched — the `dg ws` R keybinding's tmux pane or
// `dg task review-run`'s headless run both send exactly this. It's
// deliberately short and generic: each reviewer agent's own instructions
// already run `devgeta task review-scope` and `devgeta task branch-diff` to
// scope itself (see configs/shared/agents/*.md), so this prompt only needs
// to start the conversation, not describe the diff. It is also deliberately
// free of shell metacharacters, even though shellSingleQuote below makes the
// command safe either way - keeping the shipped prompt simple means the
// common case never depends on the escaping path being exercised.
const ReviewPrompt = "Review this branch against the default branch."

// shellSingleQuote wraps s in single quotes so it is safe to embed as one
// literal word in a POSIX shell command line, escaping any embedded single
// quote with the standard close/escape/reopen trick (quote, backslash,
// quote, quote) - this closes the
// current quoted string, appends an escaped literal single quote, then
// reopens quoting for the rest of s.
//
// It is needed because devgeta assembles shell command lines as STRINGS and
// hands them to a shell whole, so there is no Go-side shell parser to lean on
// the way an exec.Command argument list has. That is true of both delivery
// mechanisms:
//
//   - A created pane's command, which tmux takes as a single shell-command and
//     runs through a shell (launch.go's two recipes - the dominant reason this
//     helper exists since ADR-0021, and the one ADR-0021 discharges as a closed
//     list of every interpolated value).
//   - A typed command sent to a live tmux pane with send-keys, which lands in an
//     interactive shell exactly as written (ADR-0021 part 4's three paths, plus
//     cdCommand's `cd <path>`).
//
// There is no existing shell-quoting helper in this codebase (internal/ and
// pkg/ have no shellescape/shellquote hits), so this stays a few lines here
// rather than pulling in a library - see CLAUDE.md's "prefer existing over
// new." It is no longer a single call site: there are six, in four functions
// (paneLaunch.render x2, execPaneCommand, interactivePaneCommand x2,
// cdCommand).
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// posixShell is the floor of resolveShell's ladder. POSIX requires it to exist,
// so shell resolution can never come back empty and no worktree create is ever
// blocked on it.
const posixShell = "/bin/sh"

// resolveShell picks the shell a created pane's command is run by and re-exec'd
// into (see launch.go's two recipes, where it is interpolated twice). It returns
// the first USABLE candidate, or posixShell if none of them is.
//
// Usable means an absolute path that stats as an existing, executable, regular
// file. An absolute-path shape check alone is not enough for this value:
// "/bin/zsh" that was uninstalled, or a $SHELL pointing at a directory, passes
// a shape check and would sail into both interpolation sites (ADR-0021).
//
// It takes its candidates as INPUT rather than reading $SHELL or tmux's
// default-shell itself. Two reasons, both structural:
//
//   - This file has no tmux dependency, and acquiring one just to read an
//     option would invert the layering (the tmux wrapper is the caller's, and
//     the caller already holds it).
//   - A failed or empty tmux query is not an error here, it is simply one fewer
//     candidate. Callers pass $SHELL followed by tmux's default-shell only if
//     that query succeeded, so "no answer" needs no representation in this
//     function at all.
//
// One honest consequence of the floor: falling all the way to /bin/sh means the
// interactive-shell fallback recipe runs the coder's bare name in a shell that is
// not the user's, so it gets neither their `.zshrc` PATH repair nor any
// definition of that name they made themselves. That combination - no resolved
// binary path AND no usable user shell - is a badly broken environment, and the
// pane reports it itself when the command fails to resolve. Blocking the create
// pre-emptively would be worse (ADR-0011, ADR-0016).
func resolveShell(candidates ...string) string {
	for _, candidate := range candidates {
		if isUsableShell(candidate) {
			return candidate
		}
	}
	return posixShell
}

// isUsableShell reports whether path can be interpolated into a pane command as
// the shell to run. os.Stat (not Lstat) so a shell installed as a symlink
// resolves to its target's mode; any execute bit counts, since devgeta cannot
// know which of user/group/other it will run as.
func isUsableShell(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// ReviewerChoice is one entry of the reviewer registry as callers outside
// this package see it: the short key passed to LaunchReviewInRepo (and to
// `dg task review-run --reviewer`), the reviewer's human label for a picker,
// and the OpenCode agent name a headless run passes to `--agent`.
type ReviewerChoice struct {
	Key   string
	Label string
	Agent string
}

// BuiltinReviewerChoices returns the reviewer registry in reviewerKeys order
// ("code" first, the common case), so callers can validate a key and resolve
// its agent without duplicating this package's registry — the TUI's R picker
// builds its item list from it, and `dg task review-run` validates
// --reviewer against it.
func BuiltinReviewerChoices() []ReviewerChoice {
	reviewers := builtinReviewers()
	choices := make([]ReviewerChoice, 0, len(reviewerKeys))
	for _, key := range reviewerKeys {
		choices = append(choices, ReviewerChoice{
			Key:   key,
			Label: reviewers[key].Label,
			Agent: reviewers[key].Agent,
		})
	}
	return choices
}

// lookupBuiltinReviewer resolves key ("code", "document", or "skill") against
// the reviewer registry, producing the one "unknown reviewer" error message.
//
// It exists so that everything which needs a Reviewer gets it from a lookup that
// can FAIL, rather than from a second index into the registry whose safety
// depends on some earlier call having validated the key. reviewerPane used to do
// exactly that (`builtinReviewers()[key].Agent` after a separate command build),
// and a map miss there yields a zero Reviewer with an empty Agent - which builds an
// opencode launch with no --agent at all, i.e. a plain coder session that looks
// like a review. Structural rather than comment-guarded (CLAUDE.md §4).
func lookupBuiltinReviewer(key string) (Reviewer, error) {
	reviewer, ok := builtinReviewers()[key]
	if !ok {
		return Reviewer{}, fmt.Errorf(
			"unknown reviewer %q. Valid reviewers: %s",
			key, strings.Join(reviewerKeys, ", "),
		)
	}
	return reviewer, nil
}

// reviewCommandFor renders reviewer's TYPED command - the form sent to a pane
// that already exists, via send-keys. It is the pure, probe-free half of
// reviewerPane, kept separate so the exact command can be pinned by a test
// without a shell lookup.
//
// It takes the coder rather than building its own so a reviewer pane's typed
// command and its two launch closures all come from ONE OpenCodeCoder. A second
// instance would be harmless today (the type is stateless and zero-size), but a
// pane's behaviors sharing state with anything outside that pane is exactly what
// Layout.clone's invariant asks a reader to check for, and threading the coder
// through means there is nothing to check.
//
// The command is built by OpenCodeCoder.promptCommandWithAgent, which is also
// what PromptCommand (the --prompt flag's path) delegates to - so the
// `--prompt '<quoted>'` fragment, including the single-quoting a send-keys
// command line requires, has exactly one author.
//
// That author renders opencode's structured launch (see launch.go), which quotes
// every argument uniformly - so this emits
// `opencode '--agent' '<name>' '--prompt' '<text>'`. It names the BINARY, not the
// oc alias, even though it is typed at an interactive shell: preflight probes the
// binary, and sending a live pane something the check never verified is what
// ADR-0021's 2026-08-07 amendment removes. Reviewer launches are OpenCode-only by
// design (see the cycle plan's scope boundary: the reviewer agents' permission:
// frontmatter is enforced by OpenCode and ignored by Claude Code), so unlike
// deriveLayoutFromAlias this does not accept an aiAlias.
func reviewCommandFor(opencode *OpenCodeCoder, reviewer Reviewer) string {
	return opencode.promptCommandWithAgent(reviewer.Agent, ReviewPrompt)
}

// reviewerPane builds the pane that launches the reviewer agent registered under
// key, probing opencode ONCE and carrying that probe's resolution on the pane it
// returns.
//
// This is the review path's counterpart to coderPane, and it exists because the
// review path does its own preflight instead of going through validateLayout: it
// used to call EnsureInstalled and throw the answer away, leaving its launches to
// either probe a second time or drop the path and always fall back - the two
// options ADR-0021 rules out. One constructor, one probe, and BOTH of
// LaunchReviewInRepo's launches (the create branch with no live window, and the
// live window's split) build from the same pane, so they cannot disagree about
// what was verified.
//
// Ordering is load-bearing: the registry lookup runs FIRST, so an unknown
// reviewer key fails before the probe rather than costing a shell lookup (and
// before any git or tmux state is touched).
//
// check is left nil deliberately. The probe has already run here, so a nil check
// is what makes a second one impossible even if this pane is later put in a
// Layout that goes through EnsureInstalled - it skips nil checks, and skipping
// preserves the resolution set below rather than overwriting it. prompt is nil
// too: the reviewer's opening prompt is fixed (ReviewPrompt), so there is no
// `--prompt` to retarget onto this pane.
func reviewerPane(key string) (Pane, error) {
	reviewer, err := lookupBuiltinReviewer(key)
	if err != nil {
		return Pane{}, err
	}
	agent := reviewer.Agent

	opencode := &OpenCodeCoder{}
	resolvedPath, err := opencode.EnsureInstalled()
	if err != nil {
		return Pane{}, err
	}

	return Pane{
		Command: reviewCommandFor(opencode, reviewer),
		launch: launchFor(
			func(prompt string) paneLaunch {
				return opencode.interactiveLaunchWithAgent(agent, prompt)
			},
			func(binaryPath, prompt string) paneLaunch {
				return opencode.execLaunchWithAgent(binaryPath, agent, prompt)
			},
		),
		resolvedPath: resolvedPath,
		promptText:   ReviewPrompt,
	}, nil
}

// BuiltinLayoutNames returns the valid built-in layout names, in a stable
// order, for callers outside this package that need to list them (e.g. the
// TUI's N layout picker). Returns a copy so a caller can't mutate the
// package's own registry order.
func BuiltinLayoutNames() []string {
	return append([]string(nil), builtinLayoutNames...)
}

// lookupBuiltinLayout resolves a name against the built-in registry,
// producing a consistent "unknown layout" error (listing valid names) for
// both an explicit --layout/N-picker name and an invalid default_layout
// config value.
func lookupBuiltinLayout(name string) (Layout, error) {
	layout, ok := builtinLayouts()[name]
	if !ok {
		return Layout{}, fmt.Errorf(
			"unknown layout %q. Valid layouts: %s",
			name, strings.Join(builtinLayoutNames, ", "),
		)
	}
	return layout, nil
}

// deriveLayoutFromAlias builds a single-pane Layout for the AI coder named
// by alias, reusing ResolveAICoder so an unknown alias produces the same
// error message as every other AI-alias resolution path in this package.
func deriveLayoutFromAlias(alias string) (Layout, error) {
	coder, err := ResolveAICoder(alias)
	if err != nil {
		return Layout{}, err
	}
	return Layout{
		Name:  coder.Name(),
		Panes: []Pane{coderPane(coder, "")},
	}, nil
}

// ResolveLayout implements the layout resolution contract shared by create,
// repair, and TUI auto-repair (those call sites are later steps; this is
// just the resolver):
//
//  1. layoutName (explicit --layout flag / N-picker selection) - wins over
//     everything if non-empty.
//  2. aiAlias, derived into a single-pane layout - wins over config.
//  3. gc.Worktree.DefaultLayout config.
//  4. gc.Worktree.DefaultAI config, derived into a single-pane layout.
//  5. Built-in fallback: opencode, single-pane.
//
// IMPORTANT - what to pass as aiAlias:
//
// aiAlias must be resolved from ONLY the flag and DEVGETA_WORKTREE_AI env
// var, e.g.:
//
//	aiAlias := flagValue
//	if aiAlias == "" {
//		aiAlias = os.Getenv("DEVGETA_WORKTREE_AI")
//	}
//
// Do NOT pass ResolveAIAlias(flag, gc) here. ResolveAIAlias already folds
// flag -> env -> gc.Worktree.DefaultAI -> "opencode" into one string, with
// no way to tell which rule fired - by the time it returns, "opencode" could
// mean "the user asked for opencode" or "nothing was set and it defaulted".
// If ResolveLayout's aiAlias parameter received that folded string, an
// empty aiAlias could never be observed (it always resolves to at least
// "opencode"), which would make gc.Worktree.DefaultLayout completely
// unreachable - rule 3 would never fire because rule 2 always wins. The
// contract requires default_layout to sit BETWEEN flag/env and default_ai
// (beating default_ai, losing to flag/env), which is only expressible if
// this function can see "was a flag/env alias actually given" as a separate
// signal from "what does config say". So callers must resolve flag/env
// precedence themselves (or via a future helper that mirrors ResolveAIAlias
// but stops before folding in gc.Worktree.DefaultAI) and pass "" when
// neither is set, letting ResolveLayout consult config itself for rules 3-5.
func ResolveLayout(layoutName, aiAlias string, gc *config.GlobalConfig) (Layout, error) {
	if layoutName != "" {
		return lookupBuiltinLayout(layoutName)
	}

	if aiAlias != "" {
		return deriveLayoutFromAlias(aiAlias)
	}

	if gc != nil && gc.Worktree.DefaultLayout != "" {
		return lookupBuiltinLayout(gc.Worktree.DefaultLayout)
	}

	if gc != nil && gc.Worktree.DefaultAI != "" {
		return deriveLayoutFromAlias(gc.Worktree.DefaultAI)
	}

	return deriveLayoutFromAlias("opencode")
}
