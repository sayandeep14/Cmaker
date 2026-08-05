package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent  = lipgloss.Color("#7DD3FC") // sky
	colorAccent2 = lipgloss.Color("#C4B5FD") // violet
	colorGood    = lipgloss.Color("#4ADE80") // green
	colorBad     = lipgloss.Color("#F87171") // red
	colorMuted   = lipgloss.Color("#6B7280") // gray
	colorText    = lipgloss.Color("#E5E7EB") // near-white

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Padding(0, 1)

	styleTagline = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	styleSidebar = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(1, 2)

	styleMainPane = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(1, 2)

	styleMenuItem = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(2)

	styleMenuItemSelected = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#0B1120")).
				Background(colorAccent).
				Bold(true).
				PaddingLeft(1).
				PaddingRight(1)

	styleMenuDesc = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(2)

	styleStatusBar = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleHint = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleSuccess = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorGood)

	styleFailure = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBad)

	styleSpinnerLabel = lipgloss.NewStyle().
				Foreground(colorAccent2).
				Bold(true)

	styleLog = lipgloss.NewStyle().
			Foreground(colorText)

	styleFormLabel = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent2)

	styleDotGood = lipgloss.NewStyle().Foreground(colorGood)
	styleDotBad  = lipgloss.NewStyle().Foreground(colorBad)
	styleDotWait = lipgloss.NewStyle().Foreground(colorMuted)
)
