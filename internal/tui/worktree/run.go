package tuiworktree

import (
	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/logger"
)

// Run starts the worktree TUI dashboard.
func Run() error {
	gc := &config.GlobalConfig{}
	// Best-effort: load global config for AI alias resolution; if it fails, defaults apply.
	_ = gc.Load()

	mgr := worktree.New()
	// WorktreeManager.WarnFn defaults to utils.PrintWarning, a raw stdout
	// print that would corrupt the running bubbletea alt-screen display if
	// it ever fired. The create flow itself routes this to a toast: the
	// model's createFn (internal/tui/worktree/model.go) temporarily swaps
	// mgr.WarnFn to capture the message and surfaces it via m.status after a
	// successful create. This default only covers any other path that might
	// invoke WarnFn outside a create (none exists today) — a debug-log
	// fallback still satisfies "never silently swallowed" without risking a
	// raw print corrupting the display.
	mgr.WarnFn = func(msg string) {
		logger.L().Debugw("worktree: non-fatal warning outside create flow", "msg", msg)
	}
	m := newModel(mgr, mgr.Tmux, mgr.Git, gc)

	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
