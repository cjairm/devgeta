package agentpack

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/paths"
)

// isolateAgentPackPaths points every path NewEngine reads at a throwaway
// location, with the repo's real shipped configs as the extracted configs.
func isolateAgentPackPaths(t *testing.T, shipped string) {
	t.Helper()
	old := struct{ packs, configs, user, claude, openCode string }{
		paths.Paths.App.AgentPacks, paths.Paths.App.Configs.AgentPacks, paths.Paths.Config.AgentPacks,
		paths.Paths.Config.Claude, paths.Paths.Config.OpenCode,
	}
	t.Cleanup(func() {
		paths.Paths.App.AgentPacks, paths.Paths.App.Configs.AgentPacks = old.packs, old.configs
		paths.Paths.Config.AgentPacks, paths.Paths.Config.Claude = old.user, old.claude
		paths.Paths.Config.OpenCode = old.openCode
	})
	dir := t.TempDir()
	paths.Paths.App.AgentPacks = filepath.Join(dir, "agent-packs")
	paths.Paths.App.Configs.AgentPacks = shipped
	paths.Paths.Config.AgentPacks = filepath.Join(dir, "user-packs")
	paths.Paths.Config.Claude = filepath.Join(dir, ".claude")
	paths.Paths.Config.OpenCode = filepath.Join(dir, "opencode")
}

func TestNewEngine_KnowsTheShippedPacksAndAllThreeCoders(t *testing.T) {
	isolateAgentPackPaths(t, filepath.Join("..", "..", "..", "configs", "agent-packs"))

	e, err := NewEngine()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e == nil {
		t.Fatal("expected an engine")
	}
	var packs, coders []string
	for _, m := range e.Packs() {
		packs = append(packs, m.Name)
	}
	for _, i := range e.Installers {
		coders = append(coders, i.Name())
	}
	if strings.Join(packs, ",") != "devgeta,ponytail,superpowers" {
		t.Errorf("packs %v", packs)
	}
	if strings.Join(coders, ",") != "claude,opencode,pi" {
		t.Errorf("coders %v", coders)
	}
	if e.Trees == nil || e.State == nil {
		t.Error("the engine needs a tree source and a state store")
	}
}

func TestNewEngine_MissingConfigsSaysWhatToDo(t *testing.T) {
	isolateAgentPackPaths(t, filepath.Join(t.TempDir(), "not-extracted"))

	_, err := NewEngine()
	if err == nil || !strings.Contains(err.Error(), "agent pack") {
		t.Fatalf("expected an error about the agent pack configs, got %v", err)
	}
}
