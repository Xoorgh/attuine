package tui

import "github.com/charmbracelet/lipgloss"

var (
	focusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62"))

	unfocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))

	navActive = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("62"))

	navInactive = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	statusRunning  = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).SetString("●")
	statusStopped  = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).SetString("○")
	statusStarting = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).SetString("◐")
	statusError    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).SetString("✕")
	statusUnknown  = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).SetString("?")

	statusBarStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		PaddingLeft(1)

	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("62")).
		PaddingLeft(1)

	selectedStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255"))

	overlayStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2)

	actionKeyStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("62")).
		Bold(true)

	actionDescStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("250"))

	profileHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("62")).
		PaddingLeft(1)

	bulkHintStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		PaddingLeft(1)

	sectionDividerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		PaddingLeft(1)

	commandItemStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("250")).
		PaddingLeft(4)
)

func StatusIndicator(state string) string {
	switch state {
	case "running":
		return statusRunning.String()
	case "exited":
		return statusStopped.String()
	case "restarting":
		return statusStarting.String()
	case "paused":
		return statusStopped.String()
	default:
		return statusUnknown.String()
	}
}
