package tuiinventory

import (
	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/inventory"
)

// Run starts the interactive inventory dashboard for dg list.
func Run(gc *config.GlobalConfig, opts Options) error {
	c := &inventory.Collector{Cmd: commands.NewCommand(), Base: commands.NewBaseCommand()}
	items := c.Collect(gc)
	m := newModel(items, opts)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
