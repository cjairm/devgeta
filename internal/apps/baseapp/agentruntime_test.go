package baseapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/paths"
)

// setupAgentRuntimeTestPaths redirects the two path roots EnsureAgentRuntime
// touches to temp directories, seeded with a fake runner source (standing in
// for the embedded configs extracted to paths.Paths.App.Configs.Devgeta —
// the runner's own source dir, agent-neutral like its deploy destination,
// not configs/claude/ despite output-budget.sh, the hook, living there),
// and restores the originals on cleanup.
func setupAgentRuntimeTestPaths(t *testing.T) (configsDir, deployDir string) {
	t.Helper()
	configsDir = t.TempDir()
	deployDir = t.TempDir()

	if err := os.WriteFile(
		filepath.Join(configsDir, "output-budget-run.sh"),
		[]byte("#!/usr/bin/env bash\necho fake runner\n"),
		0o644,
	); err != nil {
		t.Fatalf("failed to write fake runner source: %v", err)
	}

	origConfigsDevgeta := paths.Paths.App.Configs.Devgeta
	origConfigDevgeta := paths.Paths.Config.Devgeta
	paths.Paths.App.Configs.Devgeta = configsDir
	paths.Paths.Config.Devgeta = deployDir
	t.Cleanup(func() {
		paths.Paths.App.Configs.Devgeta = origConfigsDevgeta
		paths.Paths.Config.Devgeta = origConfigDevgeta
	})
	return configsDir, deployDir
}

func readSidecar(t *testing.T, deployDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(deployDir, "agent-runtime.json"))
	if err != nil {
		t.Fatalf("failed to read sidecar: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("sidecar is not valid JSON: %v\n%s", err, data)
	}
	return m
}

func TestEnsureAgentRuntimeDeploysAnExecutableRunner(t *testing.T) {
	_, deployDir := setupAgentRuntimeTestPaths(t)
	gc := &config.GlobalConfig{}

	if err := EnsureAgentRuntime(gc); err != nil {
		t.Fatalf("EnsureAgentRuntime: %v", err)
	}

	runnerPath := filepath.Join(deployDir, "output-budget-run.sh")
	info, err := os.Stat(runnerPath)
	if err != nil {
		t.Fatalf("expected the runner to be deployed: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("deployed runner is not executable: mode %o", info.Mode().Perm())
	}
}

func TestEnsureAgentRuntimeSidecarReflectsGlobalConfig(t *testing.T) {
	tests := []struct {
		name       string
		outputFlag *bool
		want       bool
	}{
		{"unset defaults to off", nil, false},
		{"explicit true", boolPtr(true), true},
		{"explicit false", boolPtr(false), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, deployDir := setupAgentRuntimeTestPaths(t)
			gc := &config.GlobalConfig{}
			gc.Integrations.OutputBudget = tc.outputFlag

			if err := EnsureAgentRuntime(gc); err != nil {
				t.Fatalf("EnsureAgentRuntime: %v", err)
			}
			sidecar := readSidecar(t, deployDir)
			got, ok := sidecar["outputBudget"].(bool)
			if !ok {
				t.Fatalf("outputBudget is not a bool: %+v", sidecar["outputBudget"])
			}
			if got != tc.want {
				t.Errorf("outputBudget = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEnsureAgentRuntimeSidecarCarriesDerivedLimitsAndRules(t *testing.T) {
	_, deployDir := setupAgentRuntimeTestPaths(t)
	gc := &config.GlobalConfig{}

	if err := EnsureAgentRuntime(gc); err != nil {
		t.Fatalf("EnsureAgentRuntime: %v", err)
	}
	sidecar := readSidecar(t, deployDir)

	wantLine, wantTotal, wantCapture, err := DerivedLimits()
	if err != nil {
		t.Fatalf("DerivedLimits: %v", err)
	}
	assertNumField(t, sidecar, "lineContentLimit", wantLine)
	assertNumField(t, sidecar, "maxTotalBytes", wantTotal)
	assertNumField(t, sidecar, "captureContentLimit", wantCapture)

	rules, ok := sidecar["rules"].([]any)
	if !ok {
		t.Fatalf("rules is not an array: %+v", sidecar["rules"])
	}
	if len(rules) != len(DefaultOutputBudgetRules) {
		t.Fatalf("rules has %d entries, want %d", len(rules), len(DefaultOutputBudgetRules))
	}

	runnerPath := filepath.Join(deployDir, "output-budget-run.sh")
	if sidecar["runner"] != runnerPath {
		t.Errorf("runner = %v, want %q", sidecar["runner"], runnerPath)
	}
}

func TestEnsureAgentRuntimeSidecarWriteIsAtomic(t *testing.T) {
	_, deployDir := setupAgentRuntimeTestPaths(t)
	gc := &config.GlobalConfig{}

	if err := EnsureAgentRuntime(gc); err != nil {
		t.Fatalf("EnsureAgentRuntime: %v", err)
	}

	entries, err := os.ReadDir(deployDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if len(e.Name()) > 0 && e.Name()[0] == '.' {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}

func TestEnsureAgentRuntimeIsIdempotentAcrossRepeatedCalls(t *testing.T) {
	_, deployDir := setupAgentRuntimeTestPaths(t)
	gc := &config.GlobalConfig{}
	trueVal := true
	gc.Integrations.OutputBudget = &trueVal

	if err := EnsureAgentRuntime(gc); err != nil {
		t.Fatalf("first EnsureAgentRuntime: %v", err)
	}
	first := readSidecar(t, deployDir)

	// Simulating the SECOND configure path (e.g. opencode after claude, or
	// vice versa) calling the same function again must converge to
	// identical content, not drift.
	if err := EnsureAgentRuntime(gc); err != nil {
		t.Fatalf("second EnsureAgentRuntime: %v", err)
	}
	second := readSidecar(t, deployDir)

	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Errorf(
			"sidecar drifted across repeated calls:\nfirst:  %s\nsecond: %s",
			firstJSON,
			secondJSON,
		)
	}
}

func boolPtr(b bool) *bool { return &b }

func assertNumField(t *testing.T, m map[string]any, key string, want int) {
	t.Helper()
	got, ok := m[key].(float64)
	if !ok {
		t.Fatalf("%s is not a number: %+v", key, m[key])
	}
	if int(got) != want {
		t.Errorf("%s = %v, want %d", key, got, want)
	}
}
