// The context-report task: measures what actually loads into a Claude Code
// or OpenCode session before the first prompt (cycle doc Step 7). Read-only,
// no network — it only reads files already on disk.

package task

import (
	"fmt"
	"os"

	"github.com/cjairm/devgeta/internal/tooling/contextreport"
)

// ContextReport renders both agents' base-context measurements for the
// current directory's repo. Reported per agent, never merged (they load
// different trees — a combined number would describe neither).
func (tm *TaskManager) ContextReport() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("context-report: failed to resolve the home directory: %w", err)
	}
	repoDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("context-report: failed to resolve the working directory: %w", err)
	}

	claude := contextreport.DiscoverClaude(repoDir, home, tm.Git)
	openCode := contextreport.DiscoverOpenCode(repoDir, home)

	return claude.Render() + "\n" + openCode.Render(), nil
}
