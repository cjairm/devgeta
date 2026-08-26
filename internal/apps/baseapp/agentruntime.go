// EnsureAgentRuntime: the one writer of the output-budget runtime both
// agents share — the runner script and the sidecar describing it
// (docs/guides/output-budget-runner.md §2.2, §5.4; cycle doc Step 5).
//
// Called by BOTH internal/apps/claude's and internal/apps/opencode's
// configure paths, deliberately: the sidecar lives in devgeta's own config
// directory, and shared state written by only one of two callers goes
// stale. Deploy and sidecar are written by the same function so they can
// never disagree about the runner's path.

package baseapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// agentRuntimeSidecar is agent-runtime.json's shape (guide §8.1's transported
// schema). Field order here is Go struct order, not sidecar precedence —
// json.Marshal serializes struct fields in declaration order, which is fine
// since jq/JSON.parse read by key, not position.
type agentRuntimeSidecar struct {
	OutputBudget        bool               `json:"outputBudget"`
	Runner              string             `json:"runner"`
	LineContentLimit    int                `json:"lineContentLimit"`
	MaxTotalBytes       int                `json:"maxTotalBytes"`
	CaptureContentLimit int                `json:"captureContentLimit"`
	Rules               []OutputBudgetRule `json:"rules"`
}

// EnsureAgentRuntime deploys output-budget-run.sh to
// paths.Paths.Config.Devgeta (agent-neutral: not ~/.claude/, so an
// OpenCode-only install still gets a working runner — guide §5) and
// atomically writes agent-runtime.json describing it, resolving the gate
// from gc via OutputBudgetEnabled(). Refuses to write if DerivedLimits
// reports the generation-time invariants (guide §5.4) are violated, so a
// bad value never leaves this function.
func EnsureAgentRuntime(gc *config.GlobalConfig) error {
	if err := os.MkdirAll(paths.Paths.Config.Devgeta, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", paths.Paths.Config.Devgeta, err)
	}

	runnerDst := filepath.Join(paths.Paths.Config.Devgeta, "output-budget-run.sh")
	if err := files.CopyFile(
		filepath.Join(paths.Paths.App.Configs.Devgeta, "output-budget-run.sh"),
		runnerDst,
	); err != nil {
		return fmt.Errorf("failed to deploy the output-budget runner: %w", err)
	}
	if err := os.Chmod(runnerDst, 0o755); err != nil {
		return fmt.Errorf("failed to chmod the output-budget runner: %w", err)
	}

	lineContentLimit, maxTotalBytes, captureContentLimit, err := DerivedLimits()
	if err != nil {
		return fmt.Errorf("failed to derive output-budget limits: %w", err)
	}

	sidecar := agentRuntimeSidecar{
		OutputBudget:        gc.OutputBudgetEnabled(),
		Runner:              runnerDst,
		LineContentLimit:    lineContentLimit,
		MaxTotalBytes:       maxTotalBytes,
		CaptureContentLimit: captureContentLimit,
		Rules:               DefaultOutputBudgetRules,
	}
	data, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode agent-runtime.json: %w", err)
	}
	sidecarPath := filepath.Join(paths.Paths.Config.Devgeta, "agent-runtime.json")
	if err := files.WriteFileAtomic(sidecarPath, data, files.FilePermission); err != nil {
		return fmt.Errorf("failed to write agent-runtime.json: %w", err)
	}
	return nil
}
