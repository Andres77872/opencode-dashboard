package tui

import lipgloss "charm.land/lipgloss/v2"

type styles struct {
	App            lipgloss.Style
	StatusBar      lipgloss.Style
	StatusTitle    lipgloss.Style
	StatusMeta     lipgloss.Style
	StatusAccent   lipgloss.Style
	BannerWarn     lipgloss.Style
	BannerError    lipgloss.Style
	TabRow         lipgloss.Style
	TabActive      lipgloss.Style
	TabInactive    lipgloss.Style
	Panel          lipgloss.Style
	PanelTitle     lipgloss.Style
	Muted          lipgloss.Style
	Text           lipgloss.Style
	Accent         lipgloss.Style
	Success        lipgloss.Style
	Danger         lipgloss.Style
	Info           lipgloss.Style
	MetricCard     lipgloss.Style
	MetricLabel    lipgloss.Style
	MetricValue    lipgloss.Style
	TableHeader    lipgloss.Style
	TableRow       lipgloss.Style
	TableRowActive lipgloss.Style
	OverlayPanel   lipgloss.Style
	FilterPrompt   lipgloss.Style
	Footer         lipgloss.Style
	HelpPanel      lipgloss.Style
	EmptyState     lipgloss.Style
}

func newStyles() styles {
	const (
		bg0       = "#111315"
		bg1       = "#171A1F"
		bg2       = "#20252B"
		line      = "#313842"
		text      = "#E7EAF0"
		muted     = "#98A2B3"
		accent    = "#F59E0B"
		accentDim = "#B7791F"
		green     = "#6EE7B7"
		red       = "#F87171"
		blue      = "#7DD3FC"
	)

	return styles{
		App: lipgloss.NewStyle().
			Background(lipgloss.Color(bg0)).
			Foreground(lipgloss.Color(text)).
			Padding(0, 1),

		StatusBar: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Foreground(lipgloss.Color(text)).
			Padding(0, 1),

		StatusTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(accent)).
			Bold(true),

		StatusMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(muted)),

		StatusAccent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(blue)),

		BannerWarn: lipgloss.NewStyle().
			Foreground(lipgloss.Color(accent)).
			Background(lipgloss.Color(bg1)).
			Padding(0, 1),

		BannerError: lipgloss.NewStyle().
			Foreground(lipgloss.Color(red)).
			Background(lipgloss.Color(bg1)).
			Padding(0, 1),

		TabRow: lipgloss.NewStyle().
			Padding(0, 0, 1, 0),

		TabActive: lipgloss.NewStyle().
			Foreground(lipgloss.Color(bg0)).
			Background(lipgloss.Color(accent)).
			Bold(true).
			Padding(0, 1),

		TabInactive: lipgloss.NewStyle().
			Foreground(lipgloss.Color(muted)).
			Background(lipgloss.Color(bg2)).
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(line)).
			Padding(0, 1),

		Panel: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(line)).
			Padding(0, 1),

		PanelTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(accent)).
			Bold(true),

		Muted: lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		Text:  lipgloss.NewStyle().Foreground(lipgloss.Color(text)),
		Accent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(accent)).
			Bold(true),
		Success: lipgloss.NewStyle().Foreground(lipgloss.Color(green)),
		Danger:  lipgloss.NewStyle().Foreground(lipgloss.Color(red)),
		Info:    lipgloss.NewStyle().Foreground(lipgloss.Color(blue)),

		MetricCard: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(accentDim)).
			Padding(0, 1),

		MetricLabel: lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		MetricValue: lipgloss.NewStyle().Foreground(lipgloss.Color(text)).Bold(true),

		TableHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color(accent)).
			Bold(true),

		TableRow: lipgloss.NewStyle().Foreground(lipgloss.Color(text)),

		TableRowActive: lipgloss.NewStyle().
			Foreground(lipgloss.Color(text)).
			Background(lipgloss.Color(bg2)).
			Bold(true),

		OverlayPanel: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color(accent)).
			Padding(1, 2),

		FilterPrompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color(text)).
			Background(lipgloss.Color(bg2)).
			Bold(true),

		Footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color(muted)).
			Background(lipgloss.Color(bg1)).
			Padding(0, 1),

		HelpPanel: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color(accent)).
			Padding(1, 2),

		EmptyState: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(line)).
			Foreground(lipgloss.Color(muted)).
			Padding(1, 2),
	}
}
