package tui

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
)

// Theme holds the semantic color palette for the TUI.
type Theme struct {
	Accent    lipgloss.Color
	Muted     lipgloss.Color
	Text      lipgloss.Color
	Highlight lipgloss.Color
	Ok        lipgloss.Color
	Warn      lipgloss.Color
	Error     lipgloss.Color
}

var currentTheme = defaultTheme()

func defaultTheme() Theme {
	return Theme{
		Accent:    "62",
		Muted:     "240",
		Text:      "250",
		Highlight: "255",
		Ok:        "42",
		Warn:      "214",
		Error:     "196",
	}
}

// themeFile is the TOML structure read from disk.
type themeFile struct {
	Accent    string `toml:"accent"`
	Muted     string `toml:"muted"`
	Text      string `toml:"text"`
	Highlight string `toml:"highlight"`
	Ok        string `toml:"ok"`
	Warn      string `toml:"warn"`
	Error     string `toml:"error"`
}

func themeConfigPath() string {
	if runtime.GOOS == "windows" {
		dir := os.Getenv("LOCALAPPDATA")
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			dir = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(dir, "attuine", "theme.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "attuine", "theme.toml")
}

func loadThemeFrom(path string) Theme {
	theme := defaultTheme()
	if path == "" {
		return theme
	}

	var f themeFile
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return theme
	}

	if f.Accent != "" {
		theme.Accent = lipgloss.Color(f.Accent)
	}
	if f.Muted != "" {
		theme.Muted = lipgloss.Color(f.Muted)
	}
	if f.Text != "" {
		theme.Text = lipgloss.Color(f.Text)
	}
	if f.Highlight != "" {
		theme.Highlight = lipgloss.Color(f.Highlight)
	}
	if f.Ok != "" {
		theme.Ok = lipgloss.Color(f.Ok)
	}
	if f.Warn != "" {
		theme.Warn = lipgloss.Color(f.Warn)
	}
	if f.Error != "" {
		theme.Error = lipgloss.Color(f.Error)
	}

	return theme
}

// LoadTheme reads the theme from the default config path into currentTheme.
func LoadTheme() {
	currentTheme = loadThemeFrom(themeConfigPath())
}

// ApplyTheme rebuilds all style vars from currentTheme.
func ApplyTheme() {
	focusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(currentTheme.Accent)

	unfocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(currentTheme.Muted)

	navActive = lipgloss.NewStyle().
		Bold(true).
		Foreground(currentTheme.Accent)

	navInactive = lipgloss.NewStyle().
		Foreground(currentTheme.Muted)

	statusRunning = lipgloss.NewStyle().Foreground(currentTheme.Ok).SetString("●")
	statusStopped = lipgloss.NewStyle().Foreground(currentTheme.Muted).SetString("○")
	statusStarting = lipgloss.NewStyle().Foreground(currentTheme.Warn).SetString("◐")
	statusError = lipgloss.NewStyle().Foreground(currentTheme.Error).SetString("✕")
	statusUnknown = lipgloss.NewStyle().Foreground(currentTheme.Muted).SetString("?")

	statusBarStyle = lipgloss.NewStyle().
		Foreground(currentTheme.Muted).
		PaddingLeft(1)

	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(currentTheme.Accent).
		PaddingLeft(1)

	selectedStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(currentTheme.Highlight)

	overlayStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.DoubleBorder()).
		BorderForeground(currentTheme.Accent).
		Padding(1, 2)

	actionKeyStyle = lipgloss.NewStyle().
		Foreground(currentTheme.Accent).
		Bold(true)

	actionDescStyle = lipgloss.NewStyle().
		Foreground(currentTheme.Text)

	profileHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(currentTheme.Accent).
		PaddingLeft(1)

	bulkHintStyle = lipgloss.NewStyle().
		Foreground(currentTheme.Muted).
		PaddingLeft(1)

	sectionDividerStyle = lipgloss.NewStyle().
		Foreground(currentTheme.Muted).
		PaddingLeft(1)

	commandItemStyle = lipgloss.NewStyle().
		Foreground(currentTheme.Text).
		PaddingLeft(4)

	logoStyle = lipgloss.NewStyle().
		Foreground(currentTheme.Accent).
		Bold(true)
}
