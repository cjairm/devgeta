// Layout model and built-in registry for `dg ws` window creation/repair.
//
// This file owns the layout resolution contract that later steps (tmux
// window building, the TUI's N picker, and CLI --layout flags) all call
// into. See ResolveLayout's doc comment for the precedence ladder and,
// critically, for what a caller must pass as aiAlias to get it right.

package worktree

import (
	"fmt"
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
// check and prompt are the pane's two behaviors, and they are unexported for
// the same reason: the exported shape of a Pane is just what tmux needs to
// build it. They live ON the pane rather than in slices parallel to
// Layout.Panes so they cannot fall out of step with the pane they describe -
// a mismatch used to be possible and was caught by a length check in a
// constructor; now it cannot be written at all (CLAUDE.md §4: prefer
// structurally impossible over guarded).
//
//   - check verifies the pane's underlying tool is installed. nil means this
//     pane has nothing to check (see WithExtraPanes for the one such case).
//   - prompt renders the pane's command with an opening prompt, for
//     `dg wt create --prompt`. nil means this pane does not take a prompt -
//     true for every non-AI pane, and load-bearing: WithPrompt uses nil to
//     tell a coder pane apart from an editor pane, so it never turns `nvim`
//     into `nvim 'explain issue 1082'` (which opens a file by that name).
//
// Carrying them as constructor-time state also avoids having to
// reverse-engineer "what checks this pane" from a bare Command string later
// (e.g. distinguishing the literal command "CLAUDE_CODE_NO_FLICKER=1 claude"
// from "nvim" by string matching, which would break the moment either command
// string changes).
type Pane struct {
	Command string
	Split   string

	check  func() error
	prompt func(prompt string) string
}

// Layout is a named collection of panes describing a tmux window shape for
// `dg ws` create/repair. The exported shape is just {Name, Panes}; everything
// a pane knows how to do lives on the Pane itself.
type Layout struct {
	Name  string
	Panes []Pane
}

// coderPane builds the pane that launches an AI coder. All three of the pane's
// facets - the launch command, the install check, and the prompt form - are
// read off the one AICoder, so they cannot describe different tools.
func coderPane(coder AICoder, split string) Pane {
	return Pane{
		Command: coder.Command(),
		Split:   split,
		check:   coder.EnsureInstalled,
		prompt:  coder.PromptCommand,
	}
}

// nvimPane builds the editor pane. prompt is deliberately left nil: nvim is not
// an AI coder and has no notion of an opening prompt, so a layout containing
// only this pane must reject --prompt rather than pass the text to nvim as a
// filename.
func nvimPane(split string) Pane {
	return Pane{Command: nvimCommand, Split: split, check: ensureNvimInstalled}
}

// shellPane builds a pane that launches nothing: you get the shell tmux
// already started in the worktree directory. All three of Pane's behaviors are
// zero on purpose, and each says something:
//
//   - Command is empty, so no command is ever typed into the pane. The window
//     builder skips send-keys entirely for a pane like this (see
//     buildWindowPanes) rather than sending a bare Enter.
//   - check is nil because there is nothing to install. A shell is the one
//     "tool" every layout already depends on to run the others.
//   - prompt is nil, so `--prompt` correctly rejects a shell-only layout
//     instead of typing the prompt text at a bash prompt.
func shellPane(split string) Pane {
	return Pane{Split: split}
}

// EnsureInstalled verifies every pane's underlying tool is present, so a
// layout referencing a missing tool fails with one actionable message
// before the caller touches tmux (building the window is a later step's
// job, not this file's). A pane with no check (check == nil) is skipped.
func (l Layout) EnsureInstalled() error {
	for i, pane := range l.Panes {
		if pane.check == nil {
			continue
		}
		if err := pane.check(); err != nil {
			return fmt.Errorf("layout %q, pane %d: %w", l.Name, i+1, err)
		}
	}
	return nil
}

// clone returns a copy of l whose Panes sit in a fresh backing array, so a
// transformation below can never write through to the receiver's slice.
// Callers hold resolved layouts and reuse them (the TUI resolves once and
// creates repeatedly), so an in-place edit would leak one worktree's prompt or
// extra panes into the next.
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
	out.Panes[target].Command = out.Panes[target].prompt(prompt)
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
// shell, and send-keys with an empty string would quietly produce a bare shell
// pane that looks like the feature half-worked. Validation lives here rather
// than in the CLI so every present and future caller inherits it.
//
// These panes get no install check (check stays nil, which EnsureInstalled
// skips) on purpose: the command can be a shell builtin, a compound, or a
// Makefile target, so probing its first token would reject legitimate commands.
// A built-in layout's pane is checked because devgeta chose that command; this
// one is the user's, and its own pane shows any error.
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
func ensureNvimInstalled() error {
	// nvim's launch token and display name are the same - it's the raw binary,
	// with no cc/oc-style alias indirection.
	return ensureToolInstalled(nvimCommand, nvimCommand)
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

// reviewPrompt is the single fixed opening prompt sent to every reviewer
// agent, regardless of which one (code/document/skill) was picked. It's
// deliberately short and generic: each reviewer agent's own instructions
// already run `devgeta task review-scope` and `devgeta task branch-diff` to
// scope itself (see configs/shared/agents/*.md), so this prompt only needs
// to start the conversation, not describe the diff. It is also deliberately
// free of shell metacharacters, even though shellSingleQuote below makes the
// command safe either way - keeping the shipped prompt simple means the
// common case never depends on the escaping path being exercised.
const reviewPrompt = "Review this branch against the default branch."

// shellSingleQuote wraps s in single quotes so it is safe to embed as one
// literal word in a POSIX shell command line, escaping any embedded single
// quote with the standard close/escape/reopen trick (quote, backslash,
// quote, quote) - this closes the
// current quoted string, appends an escaped literal single quote, then
// reopens quoting for the rest of s. This is needed because ReviewCommand's
// output is sent to a live tmux pane via send-keys, which types it into an
// interactive shell exactly as written - unlike a Go exec.Command argument
// list, there is no shell parser on devgeta's side to lean on.
//
// There is no existing shell-quoting helper in this codebase (internal/ and
// pkg/ have no shellescape/shellquote hits) and this is the only call site,
// so this stays a few lines here rather than pulling in a library - see
// CLAUDE.md's "prefer existing over new."
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ReviewerChoice is one entry for the R-keybinding picker: the short key
// passed to LaunchReviewInRepo, and the reviewer's human label.
type ReviewerChoice struct {
	Key   string
	Label string
}

// BuiltinReviewerChoices returns the reviewer picker's choices in
// reviewerKeys order ("code" first, the common case), so the TUI can build
// its picker without duplicating this package's registry.
func BuiltinReviewerChoices() []ReviewerChoice {
	reviewers := builtinReviewers()
	choices := make([]ReviewerChoice, 0, len(reviewerKeys))
	for _, key := range reviewerKeys {
		choices = append(choices, ReviewerChoice{Key: key, Label: reviewers[key].Label})
	}
	return choices
}

// ReviewCommand returns the shell command to send to a tmux pane (via
// send-keys) to launch the reviewer agent registered under key ("code",
// "document", or "skill") against the current branch, or an error if key is
// not a registered reviewer.
//
// It reuses OpenCodeCoder.Command() for the launch token (the "oc" devgeta
// alias) rather than hardcoding "oc" or the raw "opencode" binary, so the
// one definition of how to launch OpenCode stays in devgeta.zsh - the same
// reasoning builtinLayouts() already applies to its own OpenCode pane.
// Reviewer launches are OpenCode-only by design (see the cycle plan's scope
// boundary: the reviewer agents' permission: frontmatter is enforced by
// OpenCode and ignored by Claude Code), so unlike deriveLayoutFromAlias this
// does not accept an aiAlias.
//
// The command is built by OpenCodeCoder.promptCommandWithAgent, which is also
// what PromptCommand (the --prompt flag's path) delegates to - so the
// `--prompt '<quoted>'` fragment, including the single-quoting a send-keys
// command line requires, has exactly one author. The emitted string is
// unchanged from when this function assembled it itself.
func ReviewCommand(key string) (string, error) {
	reviewer, ok := builtinReviewers()[key]
	if !ok {
		return "", fmt.Errorf(
			"unknown reviewer %q. Valid reviewers: %s",
			key, strings.Join(reviewerKeys, ", "),
		)
	}

	opencode := &OpenCodeCoder{}
	return opencode.promptCommandWithAgent(reviewer.Agent, reviewPrompt), nil
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
