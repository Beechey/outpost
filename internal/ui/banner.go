package ui

import "charm.land/lipgloss/v2"

// Banner is the logo from docs/logo.svg drawn in text, a flag planted on a
// summit, with the name and tagline beside it.
var Banner = lipgloss.JoinHorizontal(lipgloss.Center,
	lipgloss.JoinVertical(lipgloss.Left,
		"    ┃"+Accent.Render("▶")+"       ",
		"    ▟▙        ",
		"   ▟██▙  ▗▖   ",
		"  ▟████▙▟██▖  ",
		" ▟███████████▖",
	),
	"  ",
	lipgloss.JoinVertical(lipgloss.Left,
		Bold.Render("outpost"),
		Muted.Render("always-on servers for your AI coding agents"),
	),
)
