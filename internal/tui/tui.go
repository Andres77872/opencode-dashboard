// Package tui provides the Bubble Tea terminal dashboard for opencode-dashboard.
package tui

import (
	tea "charm.land/bubbletea/v2"

	"opencode-dashboard/internal/store"
)

// Options describe shell metadata shown by the terminal dashboard.
type Options struct {
	DBPath   string
	DBSource string
	Version  string
}

// Run starts the Bubble Tea dashboard program.
func Run(st *store.Store, opts Options) error {
	p := tea.NewProgram(newModel(st, opts))
	_, err := p.Run()
	return err
}
