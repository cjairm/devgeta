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
	// The manager's and the git app's WarnFn both default to
	// utils.PrintWarning, a raw stdout print that would corrupt the running
	// bubbletea alt-screen display if it ever fired. SetWarnFn silences both
	// in one call — assigning mgr.WarnFn alone used to leave git's own
	// advisories (a diverged branch during create, an adopted source
	// checkout) printing straight into the dashboard. The create flow routes
	// this to a toast: the model's createFn (model.go) temporarily swaps the
	// sink to capture the message and surfaces it via m.status after a
	// successful create. This default covers every other path that might
	// invoke it — a debug-log fallback still satisfies "never silently
	// swallowed" without risking a raw print corrupting the display.
	mgr.SetWarnFn(func(msg string) {
		logger.L().Debugw("worktree: non-fatal warning outside create flow", "msg", msg)
	})
	m := newModel(mgr, mgr.Tmux, mgr.Git, gc)

	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
