package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"oxorg/attuine/internal/config"
	"oxorg/attuine/internal/docker"
)

// Panel represents which panel currently has focus.
type Panel int

const (
	PanelSidebar Panel = iota
	PanelOutput
)

// entryKind distinguishes sidebar entry types.
type entryKind int

const (
	entryService entryKind = iota
	entryCommand
	entryHeader
	entryProject
)

// sidebarEntry is one item in the flat sidebar list.
type sidebarEntry struct {
	kind    entryKind
	name    string
	service string          // parent service name (for command entries under a service)
	project string          // project name (for project and command entries)
	command *config.Command // non-nil for command entries
	state   string          // service state
	ports   []string        // service ports
}

// Service holds the name and current status of a compose service.
type Service struct {
	Name  string
	State string
	Ports []string
}

// ---------------------------------------------------------------------------
// Message types for Bubble Tea
// ---------------------------------------------------------------------------

// ServiceStatusMsg carries updated service statuses from a poll.
type ServiceStatusMsg struct {
	Statuses []docker.ServiceStatus
	Err      error
}

// LogLineMsg carries a single log line from a streaming log.
type LogLineMsg struct {
	Line string
}

// OutputLineMsg carries a single output line from a command or action.
type OutputLineMsg struct {
	Line string
}

// OutputDoneMsg signals that an output stream has finished.
type OutputDoneMsg struct{}

// TickMsg is fired by the periodic status poll timer.
type TickMsg struct{}

// ProfileDownMsg signals compose down completed during a profile switch.
type ProfileDownMsg struct {
	profiles []string
	name     string
}

// HookStartMsg signals the start of a pre_up hook.
type HookStartMsg struct {
	hook      config.Hook
	remaining []config.Hook
	profiles  []string
	name      string
}

// HookStreamMsg carries one line of hook output and the channel for the next.
type HookStreamMsg struct {
	line      string
	ch        <-chan string
	remaining []config.Hook
	profiles  []string
	name      string
}

// HookDoneMsg signals all pre_up hooks have completed.
type HookDoneMsg struct {
	profiles []string
	name     string
}

// ProfileUpMsg signals compose up completed during a profile switch.
type ProfileUpMsg struct {
	name string
	err  error
}

// logBatchMsg carries a single log line and the channel to read the next from.
type logBatchMsg struct {
	line string
	ch   <-chan string
}

// cmdStreamMsg carries a single command output line and the channel to read next from.
type cmdStreamMsg struct {
	line string
	ch   <-chan string
}

// ---------------------------------------------------------------------------
// Model — thin routing shell
// ---------------------------------------------------------------------------

// Model is the top-level Bubble Tea model for attuine. It delegates
// rendering and most message handling to the active View.
type Model struct {
	cfg           *config.Config
	width, height int
	sidebarWidth  int
	outputWidth   int
	contentHeight int
	activePanel   Panel
	activeView    ViewKind
	views         map[ViewKind]View
	showHelp      bool
}

// New creates a new Model from the loaded config.
func New(cfg *config.Config, stateDir string) *Model {
	m := &Model{
		cfg:         cfg,
		activePanel: PanelSidebar,
		activeView:  ViewServices,
		views:       make(map[ViewKind]View),
	}

	m.views[ViewServices] = NewServiceView(cfg, stateDir, &m.activePanel)

	if len(cfg.Repos) > 0 {
		m.views[ViewGit] = NewGitView(cfg, &m.activePanel)
	}

	return m
}

// currentView returns the active view.
func (m *Model) currentView() View {
	return m.views[m.activeView]
}

// Init delegates to the active view.
func (m *Model) Init() tea.Cmd {
	return m.currentView().Init()
}

// Update handles incoming messages, routing most to the active view.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()
		// Pass to all views so they can update viewport sizes.
		for kind, view := range m.views {
			v, _, _ := view.Update(msg)
			m.views[kind] = v
		}
		return m, nil

	case tea.KeyMsg:
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		// When a view overlay is active, only allow ctrl+c quit and
		// delegate everything else to the view (so typing works).
		if view := m.currentView(); view != nil && view.Overlay() != "" {
			if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))) {
				return m, tea.Quit
			}
			v, cmd, _ := view.Update(msg)
			m.views[m.activeView] = v
			return m, cmd
		}

		// Shell-level keys.
		switch {
		case key.Matches(msg, Keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, Keys.Help):
			m.showHelp = true
			return m, nil

		case key.Matches(msg, Keys.Tab):
			if m.activePanel == PanelSidebar {
				m.activePanel = PanelOutput
			} else {
				m.activePanel = PanelSidebar
			}
			return m, nil

		case key.Matches(msg, Keys.ViewToggle):
			if m.activeView == ViewServices {
				m.activeView = ViewGit
			} else {
				m.activeView = ViewServices
			}
			// Init the newly active view if it exists.
			if v := m.views[m.activeView]; v != nil {
				return m, v.Init()
			}
			return m, nil
		}

		// Delegate to active view.
		v, cmd, _ := m.currentView().Update(msg)
		m.views[m.activeView] = v
		return m, cmd
	}

	// All other messages — delegate to active view.
	v, cmd, _ := m.currentView().Update(msg)
	m.views[m.activeView] = v
	return m, cmd
}

// View renders the entire TUI.
func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	if m.showHelp {
		return m.renderHelp()
	}

	view := m.currentView()
	if view == nil {
		return m.renderEmptyView()
	}

	sidebar := view.RenderSidebar(m.sidebarWidth, m.contentHeight, m.activePanel == PanelSidebar)
	output := view.RenderOutput(m.outputWidth, m.contentHeight, m.activePanel == PanelOutput)
	panels := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, output)

	statusBar := m.renderStatusBar()
	dashboard := lipgloss.JoinVertical(lipgloss.Left, panels, statusBar)

	if overlay := view.Overlay(); overlay != "" {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
		)
	}

	return dashboard
}

// renderEmptyView renders a placeholder when the active view doesn't exist yet.
func (m *Model) renderEmptyView() string {
	sidebar := unfocusedBorder.
		Width(m.sidebarWidth).
		Height(m.contentHeight).
		Render(navInactive.Render("  (no view)"))

	output := unfocusedBorder.
		Width(m.outputWidth).
		Height(m.contentHeight).
		Render("")

	panels := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, output)
	statusBar := m.renderStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, panels, statusBar)
}

// renderStatusBar renders the bottom status bar.
func (m *Model) renderStatusBar() string {
	var parts []string

	view := m.currentView()
	if view != nil {
		parts = append(parts, view.StatusBarHints()...)
	}

	// View indicator.
	viewName := "Services"
	if m.activeView == ViewGit {
		viewName = "Git"
	}
	parts = append(parts, actionKeyStyle.Render("g")+" "+actionDescStyle.Render(viewName))

	bar := strings.Join(parts, "  \u2502  ")
	return statusBarStyle.Width(m.width).Render(bar)
}

// renderHelp renders a full-screen help view.
func (m *Model) renderHelp() string {
	// Global bindings always shown.
	groups := [][]key.Binding{
		{Keys.Quit, Keys.Help, Keys.Tab, Keys.ViewToggle, Keys.Profile},
		{Keys.Up, Keys.Down, Keys.Enter, Keys.Cancel},
	}

	// Append view-specific bindings.
	view := m.currentView()
	if view != nil {
		groups = append(groups, view.HelpBindings()...)
	}

	var lines []string
	lines = append(lines, titleStyle.Render("Key Bindings"))
	lines = append(lines, "")

	for _, group := range groups {
		var items []string
		for _, b := range group {
			k := actionKeyStyle.Render(b.Help().Key)
			d := actionDescStyle.Render(b.Help().Desc)
			items = append(items, fmt.Sprintf("  %s %s", k, d))
		}
		lines = append(lines, strings.Join(items, "\n"))
		lines = append(lines, "")
	}

	lines = append(lines, navInactive.Render("  Press any key to close"))

	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

func (m *Model) recalcLayout() {
	m.contentHeight = m.height - 3
	if m.contentHeight < 1 {
		m.contentHeight = 1
	}
	m.sidebarWidth = m.width * 30 / 100
	if m.sidebarWidth < 25 {
		m.sidebarWidth = 25
	}
	if m.sidebarWidth > 40 {
		m.sidebarWidth = 40
	}
	m.outputWidth = m.width - m.sidebarWidth - 4
	if m.outputWidth < 10 {
		m.outputWidth = 10
	}
}
