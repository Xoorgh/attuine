package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// ViewKind identifies which view is active.
type ViewKind int

const (
	ViewServices ViewKind = iota
	ViewGit
)

// View is implemented by each view (Services, Git).
type View interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (View, tea.Cmd, bool)
	RenderSidebar(width, height int, focused bool) string
	RenderOutput(width, height int, focused bool) string
	Overlay() string
	StatusBarHints() []string
	HelpBindings() [][]key.Binding
}
