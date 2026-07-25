package cmd

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { testutil.InitLogger() }

func TestFormatInstalled_EmptyConfig(t *testing.T) {
	gc := &config.GlobalConfig{}

	out, err := formatInstalled(gc, "")

	require.NoError(t, err)
	assert.Contains(t, out, "Nothing installed yet")
}

func TestFormatInstalled_SingleCategory(t *testing.T) {
	gc := &config.GlobalConfig{}
	gc.Installed.Fonts = []string{"JetBrainsMono"}

	out, err := formatInstalled(gc, "")

	require.NoError(t, err)
	assert.Contains(t, out, "Fonts:")
	assert.Contains(t, out, "JetBrainsMono")
	assert.NotContains(t, out, "Packages:")
}

func TestFormatInstalled_MultipleCategories(t *testing.T) {
	gc := &config.GlobalConfig{}
	gc.Installed.Packages = []string{"git", "tmux"}
	gc.Installed.TerminalTools = []string{"neovim"}
	gc.AlreadyInstalled.Databases = []string{"postgres"}

	out, err := formatInstalled(gc, "")

	require.NoError(t, err)

	packagesIdx := strings.Index(out, "Packages:")
	toolsIdx := strings.Index(out, "Terminal Tools:")
	alreadyIdx := strings.Index(out, "Already on this machine")
	databasesIdx := strings.Index(out, "Databases:")

	require.NotEqual(t, -1, packagesIdx)
	require.NotEqual(t, -1, toolsIdx)
	require.NotEqual(t, -1, alreadyIdx)
	require.NotEqual(t, -1, databasesIdx)

	// Fixed category order: Packages before Terminal Tools.
	assert.Less(t, packagesIdx, toolsIdx)
	// Already-installed section comes after the installed section.
	assert.Less(t, toolsIdx, alreadyIdx)
	assert.Less(t, alreadyIdx, databasesIdx)

	assert.Contains(t, out, "git")
	assert.Contains(t, out, "tmux")
	assert.Contains(t, out, "neovim")
	assert.Contains(t, out, "postgres")
}

func TestFormatInstalled_CategoryFilter(t *testing.T) {
	gc := &config.GlobalConfig{}
	gc.Installed.Fonts = []string{"JetBrainsMono"}
	gc.Installed.Packages = []string{"git"}
	gc.AlreadyInstalled.Fonts = []string{"Menlo"}

	out, err := formatInstalled(gc, "fonts")

	require.NoError(t, err)
	assert.Contains(t, out, "Fonts:")
	assert.Contains(t, out, "JetBrainsMono")
	assert.Contains(t, out, "Menlo")
	assert.NotContains(t, out, "Packages:")
}

func TestFormatInstalled_InvalidCategory(t *testing.T) {
	gc := &config.GlobalConfig{}

	_, err := formatInstalled(gc, "bogus")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid category")
	assert.Contains(t, err.Error(), "packages")
	assert.Contains(t, err.Error(), "databases")
}

func TestListCmd_InvalidCategoryFlag(t *testing.T) {
	listCategoryFlag = "bogus"
	t.Cleanup(func() { listCategoryFlag = "" })

	gc := &config.GlobalConfig{}
	_, err := formatInstalled(gc, listCategoryFlag)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid category")
}

func TestListCmd_PlainFlagRegistered(t *testing.T) {
	flag := listCmd.Flags().Lookup("plain")
	if flag == nil {
		t.Fatal("expected --plain flag to be registered on dg list")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected --plain to default to false, got %q", flag.DefValue)
	}
}

func TestListCmd_LoadFailure(t *testing.T) {
	origRoot := paths.Paths.Config.Root
	paths.Paths.Config.Root = t.TempDir()
	t.Cleanup(func() { paths.Paths.Config.Root = origRoot })

	gc := &config.GlobalConfig{}
	err := gc.Load()

	require.Error(t, err)
}
