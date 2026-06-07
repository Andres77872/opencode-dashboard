// Package tui provides the Bubble Tea terminal dashboard for opencode-dashboard.
package tui

import (
	tea "charm.land/bubbletea/v2"

	"opencode-dashboard/internal/source"
)

// Options describe shell metadata shown by the terminal dashboard.
type Options struct {
	Version string
}

// Run starts the Bubble Tea dashboard program against the given source registry.
// The active source is chosen at runtime (starting from the registry's startup ID)
// and can be switched live via the source picker.
func Run(reg *source.Registry, opts Options) error {
	p := tea.NewProgram(newModel(reg, reg.StartupID(), opts))
	_, err := p.Run()
	return err
}
