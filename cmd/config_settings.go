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
	"errors"
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
// every setting except worktree.search_paths (the one variadic entry).
func requireExactlyOne(key string, raw []string) (string, error) {
	if len(raw) != 1 {
		return "", fmt.Errorf("%s accepts exactly one value, got %d", key, len(raw))
	}
	return raw[0], nil
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
			if len(raw) == 0 {
				return errors.New(
					"worktree.search_paths requires at least one path; " +
						"use `dg config unset worktree.search_paths` to clear it",
				)
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
