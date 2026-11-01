package setup

import "charm.land/lipgloss/v2"

// The logo from docs/logo.svg, drawn in text: a flag planted on a summit.
var (
	flag  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80"))
	peak  = lipgloss.NewStyle() // terminal default, so it shows on light and dark themes
	title = lipgloss.NewStyle().Bold(true)
	muted = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B90A0"))

	logo = lipgloss.JoinVertical(lipgloss.Left,
		peak.Render("    ┃")+flag.Render("▶")+"       ",
		peak.Render("    ▟▙        "),
		peak.Render("   ▟██▙  ▗▖   "),
		peak.Render("  ▟████▙▟██▖  "),
		peak.Render(" ▟███████████▖"),
	)

	banner = lipgloss.JoinHorizontal(lipgloss.Center, logo, "  ",
		lipgloss.JoinVertical(lipgloss.Left,
			title.Render("outpost"),
			muted.Render("an always-on home for your AI coding agents"),
		),
	)
)
