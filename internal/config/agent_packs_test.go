package config

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentPacks_RoundTripsThroughSaveAndLoad(t *testing.T) {
	setupIsolatedConfigPaths(t)
	want := newFullyPopulatedGlobalConfig().AgentPacks

	gc := &GlobalConfig{}
	require.NoError(t, gc.Create())
	gc.AgentPacks = want
	require.NoError(t, gc.Save())

	// Read the file, not the cache: the document on disk is what counts.
	ResetGlobalConfigCacheForTest()
	loaded := &GlobalConfig{}
	require.NoError(t, loaded.Load())
	assert.Equal(t, want, loaded.AgentPacks)
}

func TestAgentPacks_SavedKeysAreReadable(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	require.NoError(t, gc.Create())
	gc.AgentPacks = newFullyPopulatedGlobalConfig().AgentPacks
	require.NoError(t, gc.Save())

	raw, err := os.ReadFile(GlobalConfigFilePath())
	require.NoError(t, err)
	for _, key := range []string{
		"agent_packs:", "superpowers:", "version: v7.0.2", "exclude:", "replaced_hashes:",
		"coders:", "marketplace: devgeta", "plugin: superpowers@devgeta",
		"claude_md_line_added: true", "instructions_file: /home/u/.claude/DEVGETA.md", "opencode_plugins:", "opencode_instructions:", "pi_package:",
	} {
		assert.Contains(t, string(raw), key, "users read this file to see what devgeta installed")
	}
}

func TestAgentPacks_OldConfigsLoadAndSaveWithoutTheKey(t *testing.T) {
	setupIsolatedConfigPaths(t)
	legacy := `app_path: ""
config_path: ""
current_font: ""
current_theme: ""
shell:
  mise: false
`
	require.NoError(t, os.MkdirAll(strings.TrimSuffix(GlobalConfigFilePath(), "/global_config.yaml"), 0o755))
	require.NoError(t, os.WriteFile(GlobalConfigFilePath(), []byte(legacy), 0o644))

	gc := &GlobalConfig{}
	require.NoError(t, gc.Load())
	assert.Nil(t, gc.AgentPacks, "a config from before agent packs has none")

	require.NoError(t, gc.Save())
	raw, err := os.ReadFile(GlobalConfigFilePath())
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "agent_packs",
		"an install that never used packs must not gain an empty agent_packs key")
}

func TestAgentPacks_EmptyCoderFieldsAreOmitted(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	require.NoError(t, gc.Create())
	gc.AgentPacks = map[string]AgentPackState{
		"ponytail": {
			Version: "v5.3.3",
			SHA:     "7801d061243d7b753bad0e0b3c057cd250b11755",
			Coders:  map[string]AgentPackCoderState{"pi": {PiPackage: "/p"}},
		},
	}
	require.NoError(t, gc.Save())

	raw, err := os.ReadFile(GlobalConfigFilePath())
	require.NoError(t, err)
	for _, key := range []string{"exclude", "replaced_hashes", "marketplace", "plugin:", "claude_md_line_added", "instructions_file", "opencode_"} {
		assert.NotContains(t, string(raw), key, "unset fields should not clutter the file")
	}
}

func TestForgetAgentPackCoder(t *testing.T) {
	packs := func() map[string]AgentPackState {
		return map[string]AgentPackState{
			"superpowers": {Version: "v7.0.2", Coders: map[string]AgentPackCoderState{
				"claude": {Plugin: "superpowers@devgeta"},
				"pi":     {PiPackage: "/p/superpowers"},
			}},
			"ponytail": {Version: "v5.3.3", Coders: map[string]AgentPackCoderState{
				"claude": {Plugin: "ponytail@devgeta"},
			}},
			"devgeta": {Version: "1", Coders: map[string]AgentPackCoderState{
				"opencode": {OpenCodeInstructions: []string{"/d/DEVGETA.md"}},
			}},
		}
	}

	t.Run("drops the coder from every pack and returns packs left with none", func(t *testing.T) {
		gc := &GlobalConfig{AgentPacks: packs()}

		orphaned := gc.ForgetAgentPackCoder("claude")

		assert.Equal(t, []string{"ponytail"}, orphaned)
		_, hasPonytail := gc.AgentPacks["ponytail"]
		assert.False(t, hasPonytail, "a pack with no coder left has no state entry")
		assert.NotContains(t, gc.AgentPacks["superpowers"].Coders, "claude")
		assert.Contains(t, gc.AgentPacks["superpowers"].Coders, "pi", "other coders keep their records")
		assert.Contains(t, gc.AgentPacks["devgeta"].Coders, "opencode")
	})

	t.Run("a coder no pack uses changes nothing", func(t *testing.T) {
		gc := &GlobalConfig{AgentPacks: packs()}

		orphaned := gc.ForgetAgentPackCoder("zed")

		assert.Empty(t, orphaned)
		assert.Equal(t, packs(), gc.AgentPacks)
	})

	t.Run("no packs at all", func(t *testing.T) {
		gc := &GlobalConfig{}
		assert.Empty(t, gc.ForgetAgentPackCoder("claude"))
		assert.Nil(t, gc.AgentPacks, "must not create an empty map")
	})

	t.Run("returns names in a stable order", func(t *testing.T) {
		gc := &GlobalConfig{AgentPacks: map[string]AgentPackState{
			"b": {Coders: map[string]AgentPackCoderState{"claude": {}}},
			"a": {Coders: map[string]AgentPackCoderState{"claude": {}}},
		}}
		assert.Equal(t, []string{"a", "b"}, gc.ForgetAgentPackCoder("claude"))
	})
}
