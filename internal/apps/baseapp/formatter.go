// Scripts in configs/devgeta are shared by more than one coder. format-file.sh
// is the one place the per-language formatter and linter table lives
// (ADR-0063): Claude's hook wrapper and Pi's devgeta-format extension both call
// it from paths.Paths.Config.Devgeta. agent-state.sh writes a coder's activity
// into its tmux pane (ADR-0005, ADR-0055): Claude's hooks call their own copy in
// ~/.claude, Pi's devgeta-notify extension the one in paths.Paths.Config.Devgeta.

package baseapp

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// FormatScriptName is the deployed format script's file name.
const FormatScriptName = "format-file.sh"

// AgentStateScriptName is the deployed agent-state script's file name.
const AgentStateScriptName = "agent-state.sh"

func EnsureFormatScript() error {
	return DeploySharedScript(FormatScriptName, paths.Paths.Config.Devgeta)
}

// DeploySharedScript copies the shared script name from configs/devgeta into
// dir and makes it executable.
func DeploySharedScript(name, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	dst := filepath.Join(dir, name)
	if err := files.CopyFile(
		filepath.Join(paths.Paths.App.Configs.Devgeta, name),
		dst,
	); err != nil {
		return fmt.Errorf("failed to deploy %s: %w", name, err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("failed to chmod %s: %w", name, err)
	}
	return nil
}
