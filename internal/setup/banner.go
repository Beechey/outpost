package setup

import "charm.land/lipgloss/v2"

// The logo from docs/logo.svg, drawn in text: a beacon sending signal over an outpost tent.
var (
	signal = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A"))
	tent   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7"))
	ground = lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89"))
	title  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0CAF5"))
	muted  = lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89"))

	logo = lipgloss.JoinVertical(lipgloss.Left,
		signal.Render(" ((  ●  )) "),
		tent.Render("     ▲     "),
		tent.Render("    ▟█▙    "),
		tent.Render("   ▟█▀█▙   "),
		tent.Render("  ▟██ ██▙  "),
		ground.Render(" ▔▔▔▔▔▔▔▔▔ "),
	)

	banner = lipgloss.JoinHorizontal(lipgloss.Center, logo, "   ",
		lipgloss.JoinVertical(lipgloss.Left,
			title.Render("outpost"),
			muted.Render("an always-on home for your AI coding agents"),
		),
	)
)
