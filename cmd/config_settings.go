package cmd

// The `dg config` settings registry: the single place that knows about every
// user-settable devgeta configuration key. Task 5 (a later step, not this
// file) builds the `dg config` list/get/set/unset subcommands on top of it;
// Task 4 (also later) adds a reflection-based completeness test on top of it.
// This file only defines the registry and its own ordinary behavior tests.
//
// It lives in cmd/, not internal/config, because internal/config cannot
// import internal/tooling/worktree - the dependency already runs the other
// way (worktree imports config in layout.go, repo_candidates.go, scan.go,
// worktree.go). cmd/ is the only layer that imports both packages, so it is
// the only place this registry can compile.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// Setting describes one user-settable devgeta configuration key: how to read
// its live default, read/write it on a loaded config, and validate a new
// value before assigning it.
type Setting struct {
	Key         string // dotted path, e.g. "worktree.attach_after_create"
	Description string
	Kind        string // "bool" | "int" | "string" | "stringlist"

	// Default returns the live default by calling whatever owns it, so the
	// registry cannot drift from the runtime behavior it describes. It must
	// never restate a default as a literal.
	Default func() string

	// Get reads the setting's current value off gc. isSet is false when
	// nothing has been configured (the zero value) - callers should show
	// Default() in that case rather than value, which is meaningless then.
	Get func(gc *config.GlobalConfig) (value string, isSet bool)

	// Set parses and validates raw, then assigns it on gc. Validation
	// delegates to the same resolver the rest of devgeta uses for this
	// value, so an invalid value produces the identical error a user would
	// hit elsewhere (e.g. `dg wt create --ai <bad-alias>`).
	Set func(gc *config.GlobalConfig, raw []string) error

	// Unset clears the setting back to its zero value, letting Default()
	// govern again.
	Unset func(gc *config.GlobalConfig)

	// Effective, when non-nil, resolves this setting's real effective value
	// against the actual loaded gc when it's unset - for the one kind of
	// setting whose "what applies when unset" answer depends on another live
	// setting, not just this entry's own context-free Default() (e.g.
	// worktree.default_layout falling back through worktree.default_ai before
	// falling back to "opencode"). EffectiveDefault is the single place
	// list/get/set/unset all read this through, so they can never disagree
	// about it the way list's DEFAULT column once did with get/unset. Nil for
	// every setting whose effective-when-unset value is simply Default().
	Effective func(gc *config.GlobalConfig) string
}

// EffectiveDefault returns what s resolves to when unset: s.Effective(gc) if
// the registry entry supplies cross-setting resolution, otherwise s's
// context-free Default(). This is the one code path `dg config list`'s
// DEFAULT column and get/set/unset's "value in effect when unset" logic both
// go through.
func (s *Setting) EffectiveDefault(gc *config.GlobalConfig) string {
	if s.Effective != nil {
		return s.Effective(gc)
	}
	return s.Default()
}

// requireExactlyOne rejects a Set call with anything but a single value, for
// every setting whose Kind is not "stringlist".
func requireExactlyOne(key string, raw []string) (string, error) {
	if len(raw) != 1 {
		return "", fmt.Errorf("%s accepts exactly one value, got %d", key, len(raw))
	}
	return raw[0], nil
}

// requireAtLeastOne rejects a Set call with zero values, for a "stringlist"
// setting whose empty state is reached through `dg config unset` instead of
// `dg config set` with no values. noun names one entry of the list in the
// error message (e.g. "path", "reviewer").
func requireAtLeastOne(key, noun string, raw []string) error {
	if len(raw) == 0 {
		return fmt.Errorf(
			"%s requires at least one %s; use `dg config unset %s` to clear it",
			key, noun, key,
		)
	}
	return nil
}

// resolvedAIFallbackName returns the layout/AI name worktree.ResolveLayout
// falls back to when no layout name, AI alias, default_layout, or default_ai
// is available - "opencode", derived by calling the real resolution ladder
// rather than restating that string as a literal here. worktree.default_ai
// and worktree.default_layout share this same fallback (ResolveLayout's
// precedence ladder derives a single-pane layout from default_ai, or from
// "opencode" if that's empty too - either way the resulting Layout.Name is
// what both settings report as their live default), so both settings' Default
// call this one helper instead of each duplicating the ResolveLayout call.
func resolvedAIFallbackName() string {
	// Discarding the error is safe here specifically: layoutName="" and
	// aiAlias="" never reach ResolveLayout's builtin-lookup branches (the
	// only branches that can fail), so this fixed call always succeeds.
	layout, _ := worktree.ResolveLayout("", "", &config.GlobalConfig{})
	return layout.Name
}

// Settings is the registry of every user-settable devgeta configuration key.
var Settings = []Setting{
	{
		Key:         "worktree.default_ai",
		Description: "Default AI coder for `dg ws` create when no --layout/--ai/default_layout applies",
		Kind:        "string",
		Default:     resolvedAIFallbackName,
		Get: func(gc *config.GlobalConfig) (string, bool) {
			return gc.Worktree.DefaultAI, gc.Worktree.DefaultAI != ""
		},
		Set: func(gc *config.GlobalConfig, raw []string) error {
			value, err := requireExactlyOne("worktree.default_ai", raw)
			if err != nil {
				return err
			}
			if _, err := worktree.ResolveAICoder(value); err != nil {
				return err
			}
			gc.Worktree.DefaultAI = value
			return nil
		},
		Unset: func(gc *config.GlobalConfig) { gc.Worktree.DefaultAI = "" },
	},
	{
		Key:         "worktree.search_paths",
		Description: "Directories to scan for git repos in the `dg ws` repo picker; empty disables scanning",
		Kind:        "stringlist",
		// The true default is the zero value itself (an empty list, the only
		// scan off-switch) - there is no owner function to call because
		// nothing computes this default, it's simply "unset".
		Default: func() string { return "" },
		Get: func(gc *config.GlobalConfig) (string, bool) {
			return strings.Join(gc.Worktree.SearchPaths, ", "), len(gc.Worktree.SearchPaths) > 0
		},
		Set: func(gc *config.GlobalConfig, raw []string) error {
			if err := requireAtLeastOne("worktree.search_paths", "path", raw); err != nil {
				return err
			}
			paths := make([]string, len(raw))
			copy(paths, raw)
			gc.Worktree.SearchPaths = paths
			return nil
		},
		Unset: func(gc *config.GlobalConfig) { gc.Worktree.SearchPaths = nil },
	},
	{
		Key:         "worktree.scan_depth",
		Description: "Max directory depth for the `dg ws` repo scan; 0 (or unset) uses the default",
		Kind:        "int",
		Default:     func() string { return strconv.Itoa(worktree.DefaultScanDepth()) },
		Get: func(gc *config.GlobalConfig) (string, bool) {
			return strconv.Itoa(gc.Worktree.ScanDepth), gc.Worktree.ScanDepth != 0
		},
		Set: func(gc *config.GlobalConfig, raw []string) error {
			value, err := requireExactlyOne("worktree.scan_depth", raw)
			if err != nil {
				return err
			}
			depth, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("worktree.scan_depth must be an integer, got %q: %w", value, err)
			}
			if depth < 0 {
				return fmt.Errorf("worktree.scan_depth must be non-negative, got %d", depth)
			}
			gc.Worktree.ScanDepth = depth
			return nil
		},
		Unset: func(gc *config.GlobalConfig) { gc.Worktree.ScanDepth = 0 },
	},
	{
		Key:         "worktree.default_layout",
		Description: "Default tmux window layout for `dg ws` create when no --layout/--ai applies",
		Kind:        "string",
		Default:     resolvedAIFallbackName,
		Get: func(gc *config.GlobalConfig) (string, bool) {
			return gc.Worktree.DefaultLayout, gc.Worktree.DefaultLayout != ""
		},
		Set: func(gc *config.GlobalConfig, raw []string) error {
			value, err := requireExactlyOne("worktree.default_layout", raw)
			if err != nil {
				return err
			}
			// ResolveLayout's rule 1 (layoutName != "") resolves purely
			// against the built-in registry via lookupBuiltinLayout,
			// producing the same "unknown layout" error (listing valid
			// names) that --layout/the N-picker would hit for the same
			// typo - lookupBuiltinLayout itself is private, so this is the
			// exported path to that validation.
			if _, err := worktree.ResolveLayout(value, "", gc); err != nil {
				return err
			}
			gc.Worktree.DefaultLayout = value
			return nil
		},
		Unset: func(gc *config.GlobalConfig) { gc.Worktree.DefaultLayout = "" },
		// worktree.default_layout is the one setting whose true effective
		// value, when unset, depends on another live setting
		// (worktree.default_ai) rather than just its own context-free
		// Default(): ResolveLayout's fallback ladder falls through
		// gc.Worktree.DefaultAI before "opencode", so an unset default_layout
		// with default_ai set to "claude" must report "claude", not
		// "opencode". On a ResolveLayout error (should not happen for
		// layoutName="" - see resolvedAIFallbackName's comment on the same
		// call), Default() is used as a safe fallback.
		Effective: func(gc *config.GlobalConfig) string {
			layout, err := worktree.ResolveLayout("", "", gc)
			if err != nil {
				return resolvedAIFallbackName()
			}
			return layout.Name
		},
	},
	{
		Key:         "worktree.attach_after_create",
		Description: "Whether `dg ws` create's n/N attaches into the new window (true) or stays in the dashboard (false)",
		Kind:        "bool",
		Default: func() string {
			return strconv.FormatBool((*config.GlobalConfig)(nil).ShouldAttachAfterCreate())
		},
		Get: func(gc *config.GlobalConfig) (string, bool) {
			if gc.Worktree.AttachAfterCreate == nil {
				return "", false
			}
			return strconv.FormatBool(*gc.Worktree.AttachAfterCreate), true
		},
		Set: func(gc *config.GlobalConfig, raw []string) error {
			value, err := requireExactlyOne("worktree.attach_after_create", raw)
			if err != nil {
				return err
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf(
					"worktree.attach_after_create must be a boolean (true/false), got %q: %w",
					value, err,
				)
			}
			gc.Worktree.AttachAfterCreate = &parsed
			return nil
		},
		Unset: func(gc *config.GlobalConfig) { gc.Worktree.AttachAfterCreate = nil },
	},
	{
		Key:         "worktree.notify_sound",
		Description: "Whether an agent pane finishing/blocking/erroring plays a sound while its window is unattended (ADR-0009)",
		Kind:        "bool",
		// Unlike worktree.attach_after_create, there is no separate resolver
		// to call here: NotifySound's zero value (false) IS the true default
		// (off), the same reasoning fromFile.go's own comment on the field
		// gives, so restating it as a literal isn't restating something else
		// owns - nothing else owns it. worktree.search_paths' Default takes
		// the same shape for the same reason.
		Default: func() string { return strconv.FormatBool(false) },
		Get: func(gc *config.GlobalConfig) (string, bool) {
			// isSet mirrors the value itself, same as worktree.scan_depth:
			// a plain bool whose zero value is the default can't distinguish
			// "never configured" from "explicitly set to false" - both read
			// as isSet=false, which is the documented, accepted trade-off of
			// choosing a plain bool over a *bool here (see fromFile.go).
			return strconv.FormatBool(gc.Worktree.NotifySound), gc.Worktree.NotifySound
		},
		Set: func(gc *config.GlobalConfig, raw []string) error {
			value, err := requireExactlyOne("worktree.notify_sound", raw)
			if err != nil {
				return err
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf(
					"worktree.notify_sound must be a boolean (true/false), got %q: %w",
					value, err,
				)
			}
			gc.Worktree.NotifySound = parsed
			return nil
		},
		Unset: func(gc *config.GlobalConfig) { gc.Worktree.NotifySound = false },
	},
	{
		Key: "worktree.location",
		Description: "Where worktrees are created, and looked for by remove/repair/state checks: " +
			"`shared` (default) or `in-repo`",
		Kind: "string",
		// Read by worktreePath/worktreeStateIn (new worktree creation),
		// findRepoForWorktree/cursorRepoRoot (locating an existing one), and
		// removeByRepo/Repair/RepairInRepo (mutating one) - see worktree.go.
		// A mutation's actual path resolution goes through the git-verified
		// currentWorktreePath first, falling back to this setting only when
		// no real worktree exists at either location shape (see
		// realWorktreePathOrConfigured's doc comment).
		Default: func() string { return config.WorktreeLocationShared },
		Get: func(gc *config.GlobalConfig) (string, bool) {
			return gc.Worktree.Location, gc.Worktree.Location != ""
		},
		Set: func(gc *config.GlobalConfig, raw []string) error {
			value, err := requireExactlyOne("worktree.location", raw)
			if err != nil {
				return err
			}
			switch value {
			case config.WorktreeLocationShared, config.WorktreeLocationInRepo:
				gc.Worktree.Location = value
				return nil
			default:
				return fmt.Errorf(
					"unknown worktree.location %q. Valid values: %s, %s",
					value, config.WorktreeLocationShared, config.WorktreeLocationInRepo,
				)
			}
		},
		Unset: func(gc *config.GlobalConfig) { gc.Worktree.Location = "" },
	},
	{
		Key:         "review.reviewers",
		Description: "AI reviewer agents `dg task review-run` runs headless, each as provider/model (e.g. anthropic/claude-opus-4-6)",
		Kind:        "stringlist",
		// The true default is the zero value itself (an empty list, "no
		// reviewers configured") - there is no owner function to call
		// because nothing computes this default, it's simply "unset",
		// same reasoning as worktree.search_paths above.
		Default: func() string { return "" },
		Get: func(gc *config.GlobalConfig) (string, bool) {
			return strings.Join(gc.Review.Reviewers, ", "), len(gc.Review.Reviewers) > 0
		},
		Set: func(gc *config.GlobalConfig, raw []string) error {
			if err := requireAtLeastOne("review.reviewers", "reviewer", raw); err != nil {
				return err
			}
			reviewers := make([]string, len(raw))
			for i, r := range raw {
				if !isProviderModelShaped(r) {
					return fmt.Errorf(
						"review.reviewers entries must look like provider/model, got %q",
						r,
					)
				}
				reviewers[i] = r
			}
			gc.Review.Reviewers = reviewers
			return nil
		},
		Unset: func(gc *config.GlobalConfig) { gc.Review.Reviewers = nil },
	},
	{
		Key:         "review.rounds",
		Description: "Max review rounds `dg task review-run` performs before settling on a verdict (1-5)",
		Kind:        "int",
		Default:     func() string { return strconv.Itoa(config.DefaultReviewRounds) },
		Get: func(gc *config.GlobalConfig) (string, bool) {
			return strconv.Itoa(gc.Review.Rounds), gc.Review.Rounds != 0
		},
		Set: func(gc *config.GlobalConfig, raw []string) error {
			value, err := requireExactlyOne("review.rounds", raw)
			if err != nil {
				return err
			}
			rounds, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("review.rounds must be an integer, got %q: %w", value, err)
			}
			if rounds < config.ReviewRoundsMin || rounds > config.ReviewRoundsMax {
				return fmt.Errorf(
					"review.rounds must be between %d and %d, got %d",
					config.ReviewRoundsMin, config.ReviewRoundsMax, rounds,
				)
			}
			gc.Review.Rounds = rounds
			return nil
		},
		Unset: func(gc *config.GlobalConfig) { gc.Review.Rounds = 0 },
	},
}

// isProviderModelShaped reports whether spec looks like "provider/model": at
// least one "/" with non-empty text on both sides of the first one. Anything
// after that first "/" is accepted as-is (e.g. "openrouter/anthropic/opus"
// stays valid) - no other offline validation is performed, since model names
// are passed through by design (cycle doc's Explicitly Out of Scope section).
func isProviderModelShaped(spec string) bool {
	idx := strings.IndexByte(spec, '/')
	return idx > 0 && idx < len(spec)-1
}

// FindSetting looks up a registered setting by its dotted key.
func FindSetting(key string) (*Setting, bool) {
	for i := range Settings {
		if Settings[i].Key == key {
			return &Settings[i], true
		}
	}
	return nil, false
}
