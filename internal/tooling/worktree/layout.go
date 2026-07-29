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

// Pane describes a single tmux pane within a Layout: the command to run in
// it, and how it should be split off from the previous pane. Split is empty
// for the first pane in a layout (there is nothing to split from yet) and
// "vertical" or "horizontal" for every subsequent pane.
type Pane struct {
	Command string
	Split   string
}

// Layout is a named collection of panes describing a tmux window shape for
// `dg ws` create/repair.
//
// paneCheckers mirrors Panes 1:1 and holds the install-check for each pane's
// underlying tool. It's unexported: the plan mandates the exported shape be
// just {Name, Panes}, and carrying the checkers as constructor-time state
// avoids having to reverse-engineer "what checks this pane" from a bare
// Command string later (e.g. distinguishing the literal command
// "CLAUDE_CODE_NO_FLICKER=1 claude" from "nvim" by string matching, which
// would break the moment either command string changes).
type Layout struct {
	Name  string
	Panes []Pane

	paneCheckers []func() error
}

// EnsureInstalled verifies every pane's underlying tool is present, so a
// layout referencing a missing tool fails with one actionable message
// before the caller touches tmux (building the window is a later step's
// job, not this file's).
func (l Layout) EnsureInstalled() error {
	for i, check := range l.paneCheckers {
		if check == nil {
			continue
		}
		if err := check(); err != nil {
			return fmt.Errorf("layout %q, pane %d: %w", l.Name, i+1, err)
		}
	}
	return nil
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

// newLayout pairs panes with their install checkers at construction time.
// It panics if the two slices don't line up 1:1: that can only happen from
// a bug in this file's own registry construction (a built-in layout with
// mismatched Panes/checkers), never from bad user input, so failing fast
// here is preferable to EnsureInstalled silently misreporting which pane
// failed later because the indices had drifted.
func newLayout(name string, panes []Pane, checkers []func() error) Layout {
	if len(panes) != len(checkers) {
		panic(fmt.Sprintf(
			"layout %q: %d panes but %d install checkers - built-in layout registry bug",
			name, len(panes), len(checkers),
		))
	}
	return Layout{Name: name, Panes: panes, paneCheckers: checkers}
}

// builtinLayoutNames lists the valid layout names in a stable order, used
// both to build the registry and to render "valid layouts" in error
// messages.
var builtinLayoutNames = []string{"opencode", "claude", "claude-nvim", "nvim"}

// builtinLayouts returns the registry of layouts ResolveLayout can return by
// name. It's rebuilt on every call (cheap: four small structs) rather than
// cached as a package var, so each caller gets its own AICoder instances -
// there is no shared mutable state to worry about.
func builtinLayouts() map[string]Layout {
	opencode := &OpenCodeCoder{}
	claude := &ClaudeCoder{}

	return map[string]Layout{
		"opencode": newLayout(
			"opencode",
			[]Pane{{Command: opencode.Command()}},
			[]func() error{opencode.EnsureInstalled},
		),
		"claude": newLayout(
			"claude",
			[]Pane{{Command: claude.Command()}},
			[]func() error{claude.EnsureInstalled},
		),
		"claude-nvim": newLayout(
			"claude-nvim",
			[]Pane{
				{Command: claude.Command()},
				{Command: nvimCommand, Split: "vertical"},
			},
			[]func() error{claude.EnsureInstalled, ensureNvimInstalled},
		),
		"nvim": newLayout(
			"nvim",
			[]Pane{{Command: nvimCommand}},
			[]func() error{ensureNvimInstalled},
		),
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
	return newLayout(
		coder.Name(),
		[]Pane{{Command: coder.Command()}},
		[]func() error{coder.EnsureInstalled},
	), nil
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
