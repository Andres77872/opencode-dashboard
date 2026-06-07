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
	ContentArea    lipgloss.Style
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
	OverlayTitle   lipgloss.Style
	FilterPrompt   lipgloss.Style
	Footer         lipgloss.Style
	FooterKey      lipgloss.Style
	HelpPanel      lipgloss.Style
	EmptyState     lipgloss.Style
	// Cost-provenance semantics (mirrors the web markers).
	CostApprox  lipgloss.Style
	CostMixed   lipgloss.Style
	CostUnknown lipgloss.Style
}

// Palette — deeper charcoal base with a warm amber accent. Every separation also
// carries a foreground/bold/border cue so the UI stays legible when a terminal
// downsamples the near-luminance bg0/bg1/bg2 trio to 16 colors.
const (
	bg0          = "#0E1013" // app base (deepest)
	bg1          = "#15181D" // content / panels
	bg2          = "#23262C" // chrome bands, active rows, header strip (neutral, no blue cast)
	line         = "#363A41" // borders / dividers (neutral gray — amber is reserved for accents)
	text         = "#E7EAF0"
	muted        = "#98A2B3"
	accent       = "#F59E0B" // amber — used sparingly for focus/accent only
	accentBright = "#FBBF24" // brighter amber for focal markers
	accentDim    = "#B7791F"
	green        = "#6EE7B7"
	red          = "#F87171"
	blue         = "#7DD3FC"
)

func newStyles() styles {
	return styles{
		App: lipgloss.NewStyle().
			Background(lipgloss.Color(bg0)).
			Foreground(lipgloss.Color(text)).
			Padding(0, 1),

		StatusBar: lipgloss.NewStyle().
			Background(lipgloss.Color(bg2)).
			Foreground(lipgloss.Color(text)).
			Padding(0, 1),

		StatusTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(accentBright)).
			Bold(true),

		StatusMeta: lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),

		StatusAccent: lipgloss.NewStyle().Foreground(lipgloss.Color(blue)),

		BannerWarn: lipgloss.NewStyle().
			Foreground(lipgloss.Color(bg0)).
			Background(lipgloss.Color(accent)).
			Bold(true).
			Padding(0, 1),

		BannerError: lipgloss.NewStyle().
			Foreground(lipgloss.Color(bg0)).
			Background(lipgloss.Color(red)).
			Bold(true).
			Padding(0, 1),

		TabRow: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Padding(0, 0, 1, 0),

		TabActive: lipgloss.NewStyle().
			Foreground(lipgloss.Color(bg0)).
			Background(lipgloss.Color(accent)).
			Bold(true).
			Border(lipgloss.Border{
				Top:    "─",
				Bottom: "━", // thicker bottom = clear active affordance
				Left:   "│",
				Right:  "│",
			}).
			BorderForeground(lipgloss.Color(accent)).
			BorderBackground(lipgloss.Color(accent)).
			Padding(0, 1),

		TabInactive: lipgloss.NewStyle().
			Foreground(lipgloss.Color(muted)).
			Background(lipgloss.Color(bg2)).
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(line)).
			BorderBackground(lipgloss.Color(bg2)).
			Padding(0, 1),

		ContentArea: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)),

		Panel: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(line)).
			BorderBackground(lipgloss.Color(bg1)).
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
			BorderForeground(lipgloss.Color(line)).
			BorderBackground(lipgloss.Color(bg1)).
			Padding(0, 1),

		MetricLabel: lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		MetricValue: lipgloss.NewStyle().Foreground(lipgloss.Color(text)).Bold(true),

		// Header strip: filled band + bold amber for strong column separation.
		TableHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color(accent)).
			Background(lipgloss.Color(bg2)).
			Bold(true),

		TableRow: lipgloss.NewStyle().Foreground(lipgloss.Color(text)),

		TableRowActive: lipgloss.NewStyle().
			Foreground(lipgloss.Color(text)).
			Background(lipgloss.Color(bg2)).
			Bold(true),

		OverlayPanel: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(accent)).
			BorderBackground(lipgloss.Color(bg1)).
			Padding(0, 1),

		OverlayTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(bg0)).
			Background(lipgloss.Color(accent)).
			Bold(true).
			Padding(0, 1),

		FilterPrompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color(text)).
			Background(lipgloss.Color(bg2)).
			Bold(true),

		Footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color(muted)).
			Background(lipgloss.Color(bg2)).
			Padding(0, 1),

		FooterKey: lipgloss.NewStyle().
			Foreground(lipgloss.Color(accent)).
			Background(lipgloss.Color(bg2)).
			Bold(true),

		HelpPanel: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color(accent)).
			BorderBackground(lipgloss.Color(bg1)).
			Padding(1, 2),

		EmptyState: lipgloss.NewStyle().
			Background(lipgloss.Color(bg1)).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(line)).
			BorderBackground(lipgloss.Color(bg1)).
			Foreground(lipgloss.Color(muted)).
			Padding(1, 2),

		CostApprox:  lipgloss.NewStyle().Foreground(lipgloss.Color(accent)),
		CostMixed:   lipgloss.NewStyle().Foreground(lipgloss.Color(blue)),
		CostUnknown: lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
	}
}
