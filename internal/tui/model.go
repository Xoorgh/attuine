package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"oxorg/attuine/internal/config"
	"oxorg/attuine/internal/docker"
)

// NavSection represents which section of the nav panel is active.
type NavSection int

const (
	NavServices NavSection = iota
	NavProjects
	NavCommands
)

// Panel represents which panel currently has focus.
type Panel int

const (
	PanelNav Panel = iota
	PanelContext
	PanelOutput
)

// Service holds the name and current status of a compose service.
type Service struct {
	Name  string
	State string
	Ports []string
}

// Message types for Bubble Tea.

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

// ProfileSwitchMsg signals that a profile switch has completed.
type ProfileSwitchMsg struct {
	Err error
}

// logBatchMsg carries a batch of log lines to append at once.
type logBatchMsg struct {
	Lines []string
}

const navWidth = 14

// Model is the top-level Bubble Tea model for attuine.
type Model struct {
	cfg     *config.Config
	compose *docker.Compose

	// dimensions
	width  int
	height int

	// panel sizing (calculated in recalcLayout)
	contextWidth int
	outputWidth  int
	contentHeight int

	// navigation
	activeNav   NavSection
	activePanel Panel

	// service list
	services      []Service
	serviceCursor int

	// project list
	projectNames    []string
	projectCursor   int
	selectedProject string

	// command list
	commandCursor int

	// output viewport
	output   viewport.Model
	outputLines []string

	// overlay state (e.g. profile picker)
	showOverlay    bool
	overlayType    string
	overlayCursor  int
	showHelp       bool

	// active profile
	activeProfile string
	profileNames  []string

	// spinner for loading indicators
	spinner spinner.Model

	// cancel function for streaming logs/output
	cancelStream context.CancelFunc

	// status tracking
	runningCount int
}

// New creates a new Model from the loaded config.
func New(cfg *config.Config) *Model {
	// Extract sorted project names.
	var projectNames []string
	for name := range cfg.Projects {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)

	// Extract profile names.
	var profileNames []string
	for _, p := range cfg.Profiles {
		profileNames = append(profileNames, p.Name)
	}

	// Default active profile.
	activeProfile := ""
	if len(profileNames) > 0 {
		activeProfile = profileNames[0]
	}

	// Set up spinner.
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	// Set up viewport.
	vp := viewport.New(80, 20)

	m := &Model{
		cfg:           cfg,
		compose:       docker.NewCompose(cfg.ComposeFile, cfg.ComposeEnv, cfg.Dir),
		activeNav:     NavServices,
		activePanel:   PanelContext,
		projectNames:  projectNames,
		profileNames:  profileNames,
		activeProfile: activeProfile,
		spinner:       sp,
		output:        vp,
	}

	return m
}

// Init starts the spinner and kicks off the first status poll.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.pollStatus,
	)
}

// Update handles all incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()
		m.output.Width = m.outputWidth
		m.output.Height = m.contentHeight
		return m, nil

	case tea.KeyMsg:
		// If overlay is showing, handle overlay keys first.
		if m.showOverlay {
			return m.updateOverlay(msg)
		}
		// If help is showing, any key dismisses it.
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		return m.updateKeypress(msg)

	case ServiceStatusMsg:
		if msg.Err == nil {
			m.updateServices(msg.Statuses)
		}
		cmds = append(cmds, m.scheduleStatusPoll())
		return m, tea.Batch(cmds...)

	case LogLineMsg:
		m.appendOutput(msg.Line)
		return m, nil

	case OutputLineMsg:
		m.appendOutput(msg.Line)
		return m, nil

	case OutputDoneMsg:
		m.appendOutput("[done]")
		return m, nil

	case logBatchMsg:
		for _, line := range msg.Lines {
			m.appendOutput(line)
		}
		return m, nil

	case TickMsg:
		cmds = append(cmds, m.pollStatus)
		return m, tea.Batch(cmds...)

	case ProfileSwitchMsg:
		if msg.Err != nil {
			m.appendOutput(fmt.Sprintf("[profile switch error: %v]", msg.Err))
		} else {
			m.appendOutput(fmt.Sprintf("[switched to profile: %s]", m.activeProfile))
		}
		m.showOverlay = false
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the entire TUI.
func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	// If help is showing, render full-screen help.
	if m.showHelp {
		return m.renderHelp()
	}

	nav := m.renderNav()
	ctx := m.renderContext()
	out := m.renderOutputPanel()

	// Join panels horizontally.
	panels := lipgloss.JoinHorizontal(lipgloss.Top, nav, ctx, out)

	// Add status bar at the bottom.
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, panels, statusBar)
}

// updateKeypress handles global key presses when no overlay is active.
func (m *Model) updateKeypress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Quit):
		if m.cancelStream != nil {
			m.cancelStream()
			m.cancelStream = nil
		}
		return m, tea.Quit

	case key.Matches(msg, Keys.Help):
		m.showHelp = true
		return m, nil

	case key.Matches(msg, Keys.Tab):
		m.activePanel = (m.activePanel + 1) % 3
		return m, nil

	case key.Matches(msg, Keys.ShiftTab):
		m.activePanel = (m.activePanel + 2) % 3
		return m, nil

	case key.Matches(msg, Keys.NavService):
		m.activeNav = NavServices
		m.activePanel = PanelContext
		return m, nil

	case key.Matches(msg, Keys.NavProject):
		m.activeNav = NavProjects
		m.activePanel = PanelContext
		return m, nil

	case key.Matches(msg, Keys.NavCommand):
		m.activeNav = NavCommands
		m.activePanel = PanelContext
		return m, nil

	case key.Matches(msg, Keys.Profile):
		if len(m.profileNames) > 0 {
			m.showOverlay = true
			m.overlayType = "profile"
			m.overlayCursor = 0
			// Set cursor to current profile.
			for i, name := range m.profileNames {
				if name == m.activeProfile {
					m.overlayCursor = i
					break
				}
			}
		}
		return m, nil

	case key.Matches(msg, Keys.Clear):
		m.outputLines = nil
		m.output.SetContent("")
		m.output.GotoTop()
		return m, nil
	}

	// Delegate to panel-specific handlers.
	switch m.activePanel {
	case PanelContext:
		return m.updateContext(msg)
	case PanelOutput:
		return m.updateOutput(msg)
	case PanelNav:
		return m.updateNav(msg)
	}

	return m, nil
}

// updateNav handles key presses when the nav panel has focus.
func (m *Model) updateNav(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Down):
		if m.activeNav < NavCommands {
			m.activeNav++
		}
	case key.Matches(msg, Keys.Up):
		if m.activeNav > NavServices {
			m.activeNav--
		}
	case key.Matches(msg, Keys.Enter):
		m.activePanel = PanelContext
	}
	return m, nil
}

// updateContext handles key presses when the context panel has focus.
func (m *Model) updateContext(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.activeNav {
	case NavServices:
		return m.updateServiceKeys(msg)
	case NavProjects:
		return m.updateProjectKeys(msg)
	case NavCommands:
		return m.updateCommandKeys(msg)
	}
	return m, nil
}

// updateOutput handles key presses when the output panel has focus.
func (m *Model) updateOutput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.PageUp):
		m.output.HalfViewUp()
	case key.Matches(msg, Keys.PageDown):
		m.output.HalfViewDown()
	case key.Matches(msg, Keys.GoTop):
		m.output.GotoTop()
	case key.Matches(msg, Keys.GoBottom):
		m.output.GotoBottom()
	case key.Matches(msg, Keys.Up):
		m.output.LineUp(1)
	case key.Matches(msg, Keys.Down):
		m.output.LineDown(1)
	}
	return m, nil
}

// updateOverlay handles key presses when an overlay is visible.
func (m *Model) updateOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Cancel):
		m.showOverlay = false
		return m, nil

	case key.Matches(msg, Keys.Down):
		if m.overlayType == "profile" && m.overlayCursor < len(m.profileNames)-1 {
			m.overlayCursor++
		}

	case key.Matches(msg, Keys.Up):
		if m.overlayType == "profile" && m.overlayCursor > 0 {
			m.overlayCursor--
		}

	case key.Matches(msg, Keys.Enter):
		if m.overlayType == "profile" && m.overlayCursor < len(m.profileNames) {
			selected := m.profileNames[m.overlayCursor]
			if selected != m.activeProfile {
				m.activeProfile = selected
				return m, m.switchProfile(selected)
			}
			m.showOverlay = false
		}
	}

	return m, nil
}

// updateServiceKeys handles keys for the service list (stub for now).
func (m *Model) updateServiceKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Down):
		if m.serviceCursor < len(m.services)-1 {
			m.serviceCursor++
		}
	case key.Matches(msg, Keys.Up):
		if m.serviceCursor > 0 {
			m.serviceCursor--
		}
	}
	return m, nil
}

// updateProjectKeys handles keys for the project list (stub for now).
func (m *Model) updateProjectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Down):
		if m.projectCursor < len(m.projectNames)-1 {
			m.projectCursor++
		}
	case key.Matches(msg, Keys.Up):
		if m.projectCursor > 0 {
			m.projectCursor--
		}
	case key.Matches(msg, Keys.Enter):
		if m.projectCursor < len(m.projectNames) {
			m.selectedProject = m.projectNames[m.projectCursor]
			m.commandCursor = 0
			m.activeNav = NavCommands
		}
	}
	return m, nil
}

// updateCommandKeys handles keys for the command list (stub for now).
func (m *Model) updateCommandKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m, nil
}

// renderNav renders the left navigation panel.
func (m *Model) renderNav() string {
	sections := []struct {
		label string
		nav   NavSection
	}{
		{"Services", NavServices},
		{"Projects", NavProjects},
		{"Commands", NavCommands},
	}

	var lines []string
	lines = append(lines, titleStyle.Render("attuine"))
	lines = append(lines, "")

	for _, s := range sections {
		style := navInactive
		prefix := "  "
		if s.nav == m.activeNav {
			style = navActive
			prefix = "▸ "
		}
		lines = append(lines, style.Render(prefix+s.label))
	}

	content := strings.Join(lines, "\n")

	border := unfocusedBorder
	if m.activePanel == PanelNav {
		border = focusedBorder
	}

	return border.
		Width(navWidth).
		Height(m.contentHeight).
		Render(content)
}

// renderContext renders the middle context panel based on activeNav.
func (m *Model) renderContext() string {
	var content string

	switch m.activeNav {
	case NavServices:
		content = m.renderServiceList()
	case NavProjects:
		content = m.renderProjectList()
	case NavCommands:
		content = m.renderCommandList()
	}

	border := unfocusedBorder
	if m.activePanel == PanelContext {
		border = focusedBorder
	}

	return border.
		Width(m.contextWidth).
		Height(m.contentHeight).
		Render(content)
}

// renderOutputPanel renders the right output viewport panel.
func (m *Model) renderOutputPanel() string {
	border := unfocusedBorder
	if m.activePanel == PanelOutput {
		border = focusedBorder
	}

	return border.
		Width(m.outputWidth).
		Height(m.contentHeight).
		Render(m.output.View())
}

// renderServiceList renders the list of services with status indicators.
func (m *Model) renderServiceList() string {
	if len(m.services) == 0 {
		return titleStyle.Render("Services") + "\n\n" +
			navInactive.Render("  (no services)")
	}

	var lines []string
	lines = append(lines, titleStyle.Render("Services"))
	lines = append(lines, "")

	for i, svc := range m.services {
		indicator := StatusIndicator(svc.State)
		name := svc.Name
		if len(svc.Ports) > 0 {
			name += " :" + strings.Join(svc.Ports, ",:")
		}

		style := navInactive
		prefix := "  "
		if i == m.serviceCursor {
			style = selectedStyle
			prefix = "▸ "
		}

		lines = append(lines, fmt.Sprintf("%s %s", indicator, style.Render(prefix+name)))
	}

	return strings.Join(lines, "\n")
}

// renderProjectList renders the list of configured projects.
func (m *Model) renderProjectList() string {
	if len(m.projectNames) == 0 {
		return titleStyle.Render("Projects") + "\n\n" +
			navInactive.Render("  (no projects)")
	}

	var lines []string
	lines = append(lines, titleStyle.Render("Projects"))
	lines = append(lines, "")

	for i, name := range m.projectNames {
		style := navInactive
		prefix := "  "
		if i == m.projectCursor {
			style = selectedStyle
			prefix = "▸ "
		}
		lines = append(lines, style.Render(prefix+name))
	}

	return strings.Join(lines, "\n")
}

// renderCommandList renders the commands for the selected project.
func (m *Model) renderCommandList() string {
	if m.selectedProject == "" {
		return titleStyle.Render("Commands") + "\n\n" +
			navInactive.Render("  (select a project first)")
	}

	proj, ok := m.cfg.Projects[m.selectedProject]
	if !ok || len(proj.Commands) == 0 {
		return titleStyle.Render("Commands: "+m.selectedProject) + "\n\n" +
			navInactive.Render("  (no commands)")
	}

	var lines []string
	lines = append(lines, titleStyle.Render("Commands: "+m.selectedProject))
	lines = append(lines, "")

	for i, cmd := range proj.Commands {
		style := navInactive
		prefix := "  "
		if i == m.commandCursor {
			style = selectedStyle
			prefix = "▸ "
		}
		lines = append(lines, style.Render(prefix+cmd.Name))
	}

	return strings.Join(lines, "\n")
}

// renderOverlay renders a centered overlay dialog (e.g., profile picker).
func (m *Model) renderOverlay() string {
	if m.overlayType != "profile" {
		return ""
	}

	var lines []string
	lines = append(lines, titleStyle.Render("Select Profile"))
	lines = append(lines, "")

	for i, name := range m.profileNames {
		style := navInactive
		prefix := "  "
		if i == m.overlayCursor {
			style = selectedStyle
			prefix = "▸ "
		}
		suffix := ""
		if name == m.activeProfile {
			suffix = " (active)"
		}
		lines = append(lines, style.Render(prefix+name+suffix))
	}

	lines = append(lines, "")
	lines = append(lines, navInactive.Render("  esc to cancel"))

	content := strings.Join(lines, "\n")
	return overlayStyle.Render(content)
}

// renderHelp renders a full-screen help view.
func (m *Model) renderHelp() string {
	groups := Keys.FullHelp()

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

// renderStatusBar renders the bottom status bar.
func (m *Model) renderStatusBar() string {
	var parts []string

	// Help hint.
	parts = append(parts, actionKeyStyle.Render("?")+" "+actionDescStyle.Render("help"))

	// Active profile.
	if m.activeProfile != "" {
		parts = append(parts, actionKeyStyle.Render("profile:")+" "+actionDescStyle.Render(m.activeProfile))
	}

	// Running count.
	parts = append(parts, actionDescStyle.Render(fmt.Sprintf("%d running", m.runningCount)))

	// Nav hints.
	parts = append(parts, actionKeyStyle.Render("1")+" "+actionDescStyle.Render("svc")+
		" "+actionKeyStyle.Render("2")+" "+actionDescStyle.Render("proj")+
		" "+actionKeyStyle.Render("3")+" "+actionDescStyle.Render("cmd"))

	bar := strings.Join(parts, "  │  ")

	return statusBarStyle.
		Width(m.width).
		Render(bar)
}

// updateServices refreshes the internal service list from a status poll result.
func (m *Model) updateServices(statuses []docker.ServiceStatus) {
	m.services = make([]Service, len(statuses))
	m.runningCount = 0
	for i, s := range statuses {
		m.services[i] = Service{
			Name:  s.Service,
			State: s.State,
			Ports: s.Ports,
		}
		if s.State == "running" {
			m.runningCount++
		}
	}
	// Keep cursor in bounds.
	if m.serviceCursor >= len(m.services) && len(m.services) > 0 {
		m.serviceCursor = len(m.services) - 1
	}
}

// appendOutput adds a line to the output buffer and updates the viewport.
func (m *Model) appendOutput(line string) {
	m.outputLines = append(m.outputLines, line)
	content := strings.Join(m.outputLines, "\n")
	m.output.SetContent(content)
	m.output.GotoBottom()
}

// recalcLayout recalculates panel dimensions based on terminal size.
func (m *Model) recalcLayout() {
	// Reserve 1 row for status bar, 2 for borders top/bottom on each panel.
	m.contentHeight = m.height - 3
	if m.contentHeight < 1 {
		m.contentHeight = 1
	}

	// Nav panel takes navWidth + 2 for borders.
	remaining := m.width - navWidth - 4 // -4 for borders on nav and spacing

	// Context panel gets ~30% of remaining, output gets the rest.
	m.contextWidth = remaining * 30 / 100
	if m.contextWidth < 15 {
		m.contextWidth = 15
	}
	m.outputWidth = remaining - m.contextWidth - 4 // -4 for borders on context and output
	if m.outputWidth < 10 {
		m.outputWidth = 10
	}
}

// pollStatus polls docker compose for current service statuses.
// This is a tea.Cmd — it takes no arguments and returns a tea.Msg.
func (m *Model) pollStatus() tea.Msg {
	statuses, err := m.compose.Status(context.Background())
	return ServiceStatusMsg{Statuses: statuses, Err: err}
}

// scheduleStatusPoll returns a tea.Cmd that fires a TickMsg after a delay.
func (m *Model) scheduleStatusPoll() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return TickMsg{}
	})
}

// switchProfile returns a tea.Cmd that downs the current stack and brings up
// the new profile, running any pre_up hooks.
func (m *Model) switchProfile(profileName string) tea.Cmd {
	compose := m.compose
	cfg := m.cfg

	// Find the profile config.
	var profiles []string
	for _, p := range cfg.Profiles {
		if p.Name == profileName {
			profiles = p.Profiles
			break
		}
	}

	return func() tea.Msg {
		ctx := context.Background()

		// Down current stack.
		if err := compose.Down(ctx); err != nil {
			return ProfileSwitchMsg{Err: err}
		}

		// Up with new profile.
		if err := compose.Up(ctx, profiles); err != nil {
			return ProfileSwitchMsg{Err: err}
		}

		return ProfileSwitchMsg{}
	}
}
