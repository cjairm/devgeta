package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func init() { testutil.InitLogger() }

// knownStateFields lists every field on a settings-bearing struct that is
// intentionally NOT in the Settings registry, because devgeta writes it
// itself - it is state, not a user preference. Each entry names why, so the
// reasoning survives independent of this comment. This map, together with
// Settings, must account for every field on every struct in
// settingsBearingStructs below; TestSettingsCompleteness_* fails the build
// the moment a new field is neither registered nor listed here.
var knownStateFields = map[string]string{
	"worktree.recent_repos": "MRU list of repos used for worktree creation, " +
		"written by internal/tooling/worktree on every `dg ws` create - not a " +
		"user preference (cycle doc docs/plans/cycles/2026-07-29-dg-config-command.md " +
		"§4 Explicitly Out of Scope: \"Editing state\").",
	"integrations.rtk_claude_hook": "a deploy record, not a preference: it is " +
		"set by `dg configure claude --force --only=rtk` (ADR-0004:64-70), which " +
		"also re-renders settings.json. A bare `set` here would desync the " +
		"recorded flag from what's actually deployed, so it is refused as a " +
		"settable key rather than exposed.",
}

// settingsBearingStructs pairs each struct this completeness test reflects
// over with the dotted-key prefix its fields live under in
// global_config.yaml (see GlobalConfig's own yaml tags in fromFile.go:
// `worktree` and `integrations`). Add a struct here if a future cycle adds
// another settings-bearing section of GlobalConfig.
var settingsBearingStructs = []struct {
	prefix string
	value  any
}{
	{"worktree", config.WorktreeConfig{}},
	{"integrations", config.IntegrationsConfig{}},
}

// yamlKeyFromTag extracts the field name a yaml tag serializes as, stripping
// options like ",omitempty". ok is false for an empty tag or an explicit "-"
// (the field isn't serialized at all, so it can't be a dg config key).
func yamlKeyFromTag(tag string) (name string, ok bool) {
	if tag == "" {
		return "", false
	}
	name = strings.Split(tag, ",")[0]
	if name == "-" {
		return "", false
	}
	return name, true
}

// registeredKeys returns the set of dotted keys currently in Settings.
func registeredKeys() map[string]bool {
	keys := make(map[string]bool, len(Settings))
	for _, s := range Settings {
		keys[s.Key] = true
	}
	return keys
}

// realDottedKeys reflects over settingsBearingStructs and returns every real
// dotted key (e.g. "worktree.attach_after_create") those structs' yaml tags
// produce. This is the single source of truth both the completeness test and
// the "keys resolve to real paths" test compare against - neither hand-lists
// field names, so a struct field can't silently drift out of coverage.
func realDottedKeys(t *testing.T) map[string]string {
	t.Helper()
	keys := make(map[string]string)
	for _, sbs := range settingsBearingStructs {
		typ := reflect.TypeOf(sbs.value)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			yamlName, ok := yamlKeyFromTag(field.Tag.Get("yaml"))
			if !ok {
				t.Fatalf(
					"%s.%s has no usable yaml tag (%q); every settings-bearing "+
						"struct field must have one so it can be classified as a "+
						"setting or as state",
					typ.Name(), field.Name, field.Tag.Get("yaml"),
				)
			}
			keys[sbs.prefix+"."+yamlName] = typ.Name() + "." + field.Name
		}
	}
	return keys
}

// TestSettingsCompleteness_EveryFieldRegisteredOrDeclaredState is the
// enforcement mechanism the cycle exists to add: it fails the build the
// moment a field is added to a settings-bearing struct without either
// registering it in Settings (cmd/config_settings.go) or declaring it in
// knownStateFields above with a reason. Without this test, a new field is
// invisible until someone remembers to expose it - exactly the problem
// worktree.attach_after_create ran into before this cycle.
func TestSettingsCompleteness_EveryFieldRegisteredOrDeclaredState(t *testing.T) {
	registered := registeredKeys()

	for dottedKey, fieldName := range realDottedKeys(t) {
		_, isRegistered := registered[dottedKey]
		_, isKnownState := knownStateFields[dottedKey]

		switch {
		case isRegistered && isKnownState:
			t.Errorf(
				"%s (%s) is both a registered setting and a declared state field - it can only be one",
				dottedKey,
				fieldName,
			)
		case !isRegistered && !isKnownState:
			t.Errorf(
				"%s (%s) is neither registered in Settings (cmd/config_settings.go) "+
					"nor declared in knownStateFields (cmd/config_settings_test.go) - "+
					"add it to Settings if it's a user preference, or to "+
					"knownStateFields with a reason if it's devgeta-written state",
				dottedKey, fieldName,
			)
		}
	}
}

// TestSettingsRegistry_NoDuplicateKeys guards against a copy-paste error
// registering the same dotted key twice, which would make the second entry
// permanently unreachable via FindSetting.
func TestSettingsRegistry_NoDuplicateKeys(t *testing.T) {
	seen := make(map[string]bool, len(Settings))
	for _, s := range Settings {
		assert.Falsef(t, seen[s.Key], "duplicate registry key %q", s.Key)
		seen[s.Key] = true
	}
}

// TestSettingsRegistry_EveryKeyResolvesToARealYAMLTagPath guards against a
// typo'd Key (e.g. "worktree.default_layout" misspelled) that would silently
// register a setting nothing ever reads or writes. It compares against the
// same reflected struct data TestSettingsCompleteness_* uses, not a second
// hand-maintained list.
func TestSettingsRegistry_EveryKeyResolvesToARealYAMLTagPath(t *testing.T) {
	real := realDottedKeys(t)
	for _, s := range Settings {
		assert.Truef(
			t,
			real[s.Key] != "",
			"registry key %q does not resolve to any real yaml tag path",
			s.Key,
		)
	}
}

// TestSettingsRegistry_EveryEntryHasADescription guards against a Setting
// added with its Description left blank, which would make `dg config list`
// show a key nobody can tell the purpose of.
func TestSettingsRegistry_EveryEntryHasADescription(t *testing.T) {
	for _, s := range Settings {
		assert.NotEmptyf(
			t,
			strings.TrimSpace(s.Description),
			"setting %q has an empty Description",
			s.Key,
		)
	}
}

// settingsWithEmptyDefaultAllowed is the one documented exception to "every
// Default() is non-empty": worktree.search_paths's real, correct default IS
// the empty string (an empty list disables repo scanning - its only
// off-switch; see cmd/config_settings.go's own comment on this entry). The
// non-empty check below exists to catch an ACCIDENTALLY blank Default (one
// that forgot to call its owning function), not to forbid a setting whose
// true default is legitimately empty. Widening this exception for a future
// setting requires the same explicit, reasoned case made here - not a
// silent addition.
var settingsWithEmptyDefaultAllowed = map[string]bool{
	"worktree.search_paths": true,
}

// TestSettingsRegistry_EveryDefaultIsNonEmpty guards against a Default that
// was left as a stub (e.g. `func() string { return "" }` never wired up to
// its real owner), except for the one setting where empty is the genuine
// value.
func TestSettingsRegistry_EveryDefaultIsNonEmpty(t *testing.T) {
	for _, s := range Settings {
		if settingsWithEmptyDefaultAllowed[s.Key] {
			continue
		}
		assert.NotEmptyf(t, s.Default(), "setting %q's Default() returned an empty string", s.Key)
	}
}
