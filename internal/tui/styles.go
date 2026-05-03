package tui

import "github.com/charmbracelet/lipgloss"

var (
	focusedBorder       lipgloss.Style
	unfocusedBorder     lipgloss.Style
	navActive           lipgloss.Style
	navInactive         lipgloss.Style
	statusRunning       lipgloss.Style
	statusStopped       lipgloss.Style
	statusStarting      lipgloss.Style
	statusError         lipgloss.Style
	statusUnknown       lipgloss.Style
	statusBarStyle      lipgloss.Style
	titleStyle          lipgloss.Style
	selectedStyle       lipgloss.Style
	overlayStyle        lipgloss.Style
	actionKeyStyle      lipgloss.Style
	actionDescStyle     lipgloss.Style
	profileHeaderStyle  lipgloss.Style
	bulkHintStyle       lipgloss.Style
	sectionDividerStyle lipgloss.Style
	commandItemStyle    lipgloss.Style
	logoStyle           lipgloss.Style
)

func init() {
	ApplyTheme()
}

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
