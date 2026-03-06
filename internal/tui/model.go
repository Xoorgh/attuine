package tui

import (
	"context"
	"fmt"
	"path/filepath"
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
	"oxorg/attuine/internal/runner"
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
	logCancel context.CancelFunc

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

// Init starts the spinner, seeds the service list, and kicks off the first status poll.
func (m *Model) Init() tea.Cmd {
	// Seed service list from compose file so services appear even when stopped.
	// Collect all unique profile names so profiled services are included.
	profileSet := make(map[string]bool)
	for _, p := range m.cfg.Profiles {
		for _, name := range p.Profiles {
			profileSet[name] = true
		}
	}
	allProfiles := make([]string, 0, len(profileSet))
	for name := range profileSet {
		allProfiles = append(allProfiles, name)
	}
	if names, err := m.compose.ListServices(context.Background(), allProfiles); err == nil {
		for _, name := range names {
			m.services = append(m.services, Service{Name: name})
		}
	}

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
		m.appendOutput(msg.line)
		return m, readLogLine(msg.ch)

	case cmdStreamMsg:
		m.appendOutput(msg.line)
		return m, readStream(msg.ch)

	case TickMsg:
		cmds = append(cmds, m.pollStatus)
		return m, tea.Batch(cmds...)

	case ProfileDownMsg:
		return m.handleProfileDown(msg)

	case HookStartMsg:
		return m.handleHookStart(msg)

	case HookStreamMsg:
		return m.handleHookStream(msg)

	case HookDoneMsg:
		return m.handleHookDone(msg)

	case ProfileUpMsg:
		return m.handleProfileUp(msg)

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

	dashboard := lipgloss.JoinVertical(lipgloss.Left, panels, statusBar)

	// If overlay is active, render it centered on screen.
	if m.showOverlay {
		overlay := m.renderOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
		)
	}

	return dashboard
}

// updateKeypress handles global key presses when no overlay is active.
func (m *Model) updateKeypress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Quit):
		m.cancelLogs()
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
	case key.Matches(msg, Keys.Clear):
		m.outputLines = nil
		m.output.SetContent("")
		m.output.GotoTop()
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
				m.showOverlay = false
				m.appendOutput(fmt.Sprintf("[switching profile to %s...]", selected))
				return m, m.switchProfile(selected)
			}
			m.showOverlay = false
		}
	}

	return m, nil
}

// updateServiceKeys handles keys for the service list.
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

	case key.Matches(msg, Keys.ServiceUp):
		if len(m.services) == 0 {
			return m, nil
		}
		svc := m.services[m.serviceCursor].Name
		profiles := m.activeProfiles()
		compose := m.compose
		m.appendOutput(fmt.Sprintf("[starting %s...]", svc))
		return m, func() tea.Msg {
			if err := compose.Up(context.Background(), profiles, svc); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error starting %s: %v]", svc, err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[%s started]", svc)}
		}

	case key.Matches(msg, Keys.ServiceDown):
		if len(m.services) == 0 {
			return m, nil
		}
		svc := m.services[m.serviceCursor].Name
		compose := m.compose
		m.appendOutput(fmt.Sprintf("[stopping %s...]", svc))
		return m, func() tea.Msg {
			if err := compose.Stop(context.Background(), svc); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error stopping %s: %v]", svc, err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[%s stopped]", svc)}
		}

	case key.Matches(msg, Keys.ServiceRebuild):
		if len(m.services) == 0 {
			return m, nil
		}
		svc := m.services[m.serviceCursor].Name
		compose := m.compose
		m.appendOutput(fmt.Sprintf("[rebuilding %s...]", svc))
		return m, func() tea.Msg {
			if err := compose.Build(context.Background(), svc); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error rebuilding %s: %v]", svc, err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[%s rebuilt]", svc)}
		}

	case key.Matches(msg, Keys.ServiceLogs):
		if len(m.services) == 0 {
			return m, nil
		}
		svc := m.services[m.serviceCursor].Name
		m.cancelLogs()
		m.appendOutput(fmt.Sprintf("[streaming logs for %s...]", svc))
		ch, cancel := m.compose.Logs(context.Background(), svc)
		m.logCancel = cancel
		return m, readLogLine(ch)

	case key.Matches(msg, Keys.ServiceShell):
		if len(m.services) == 0 {
			return m, nil
		}
		svc := m.services[m.serviceCursor].Name
		c := m.compose.Shell(context.Background(), svc)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			if err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[shell error: %v]", err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[shell for %s closed]", svc)}
		})
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

// updateCommandKeys handles keys for the command list.
func (m *Model) updateCommandKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.selectedProject == "" {
		return m, nil
	}
	proj, ok := m.cfg.Projects[m.selectedProject]
	if !ok || len(proj.Commands) == 0 {
		return m, nil
	}

	switch {
	case key.Matches(msg, Keys.Down):
		if m.commandCursor < len(proj.Commands)-1 {
			m.commandCursor++
		}
	case key.Matches(msg, Keys.Up):
		if m.commandCursor > 0 {
			m.commandCursor--
		}
	case key.Matches(msg, Keys.Enter):
		if m.commandCursor < len(proj.Commands) {
			cmd := proj.Commands[m.commandCursor]
			m.cancelLogs()
			m.appendOutput(fmt.Sprintf("[running %s: %s]", cmd.Name, cmd.Run))
			return m, m.runCommand(m.selectedProject, cmd)
		}
	}
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
	parts = append(parts, actionDescStyle.Render(fmt.Sprintf("%d/%d running", m.runningCount, len(m.services))))

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
	// Build a map of current statuses.
	statusMap := make(map[string]docker.ServiceStatus)
	for _, s := range statuses {
		statusMap[s.Service] = s
	}

	// Update existing services with new status, or mark as stopped.
	m.runningCount = 0
	for i := range m.services {
		if s, ok := statusMap[m.services[i].Name]; ok {
			m.services[i].State = s.State
			m.services[i].Ports = s.Ports
			delete(statusMap, s.Service)
		} else {
			m.services[i].State = ""
			m.services[i].Ports = nil
		}
		if m.services[i].State == "running" {
			m.runningCount++
		}
	}

	// Add any new services we haven't seen before.
	for _, s := range statuses {
		if _, exists := statusMap[s.Service]; exists {
			svc := Service{Name: s.Service, State: s.State, Ports: s.Ports}
			m.services = append(m.services, svc)
			if s.State == "running" {
				m.runningCount++
			}
		}
	}

	// Keep cursor in bounds.
	if m.serviceCursor >= len(m.services) && len(m.services) > 0 {
		m.serviceCursor = len(m.services) - 1
	}
}

// appendOutput adds a line to the output buffer and updates the viewport.
// The buffer is capped at 10000 lines to prevent unbounded memory growth.
func (m *Model) appendOutput(line string) {
	m.outputLines = append(m.outputLines, line)
	if len(m.outputLines) > 10000 {
		m.outputLines = m.outputLines[len(m.outputLines)-10000:]
	}
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

// switchProfile returns a tea.Cmd that starts the profile switch by downing
// the current stack. The rest of the flow (hooks, up) is driven by message
// handlers in Update forming a state machine.
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
		if err := compose.Down(context.Background()); err != nil {
			return ProfileUpMsg{name: profileName, err: fmt.Errorf("compose down: %w", err)}
		}
		return ProfileDownMsg{profiles: profiles, name: profileName}
	}
}

// handleProfileDown processes a completed compose down and either starts
// running pre_up hooks or proceeds directly to compose up.
func (m *Model) handleProfileDown(msg ProfileDownMsg) (tea.Model, tea.Cmd) {
	hooks := m.cfg.Hooks.PreUp
	if len(hooks) > 0 {
		// Kick off the first hook via HookStartMsg; handleHookStart will
		// run the command and start streaming its output.
		return m, func() tea.Msg {
			return HookStartMsg{
				hook:      hooks[0],
				remaining: hooks[1:],
				profiles:  msg.profiles,
				name:      msg.name,
			}
		}
	}

	// No hooks — go straight to compose up.
	compose := m.compose
	return m, func() tea.Msg {
		if err := compose.Up(context.Background(), msg.profiles); err != nil {
			return ProfileUpMsg{name: msg.name, err: err}
		}
		return ProfileUpMsg{name: msg.name}
	}
}

// handleHookStart logs the hook start and begins streaming its output.
func (m *Model) handleHookStart(msg HookStartMsg) (tea.Model, tea.Cmd) {
	m.appendOutput(fmt.Sprintf("[running hook: %s...]", msg.hook.Name))
	dir := m.cfg.Dir
	return m, func() tea.Msg {
		ch, err := runner.RunHost(context.Background(), dir, msg.hook.Run)
		if err != nil {
			return ProfileUpMsg{name: msg.name, err: fmt.Errorf("hook %s: %w", msg.hook.Name, err)}
		}
		line, ok := <-ch
		if !ok {
			// Hook produced no output — proceed.
			if len(msg.remaining) > 0 {
				return HookStartMsg{
					hook:      msg.remaining[0],
					remaining: msg.remaining[1:],
					profiles:  msg.profiles,
					name:      msg.name,
				}
			}
			return HookDoneMsg{profiles: msg.profiles, name: msg.name}
		}
		return HookStreamMsg{
			line:      line,
			ch:        ch,
			remaining: msg.remaining,
			profiles:  msg.profiles,
			name:      msg.name,
		}
	}
}

// handleHookStream appends one line of hook output and reads the next.
func (m *Model) handleHookStream(msg HookStreamMsg) (tea.Model, tea.Cmd) {
	m.appendOutput(msg.line)
	return m, func() tea.Msg {
		line, ok := <-msg.ch
		if !ok {
			// Channel closed — hook finished. Start next hook or signal done.
			// TODO: Hook exit status is not checked here. If a hook exits
			// non-zero, the runner sends "[exited with code N]" as the last
			// line on the channel, but we currently proceed to the next hook
			// regardless. To properly abort on failure, the runner would need
			// to return exit status separately (e.g. via a struct or second
			// channel).
			if len(msg.remaining) > 0 {
				return HookStartMsg{
					hook:      msg.remaining[0],
					remaining: msg.remaining[1:],
					profiles:  msg.profiles,
					name:      msg.name,
				}
			}
			return HookDoneMsg{profiles: msg.profiles, name: msg.name}
		}
		return HookStreamMsg{
			line:      line,
			ch:        msg.ch,
			remaining: msg.remaining,
			profiles:  msg.profiles,
			name:      msg.name,
		}
	}
}

// handleHookDone runs compose up after all hooks have completed.
func (m *Model) handleHookDone(msg HookDoneMsg) (tea.Model, tea.Cmd) {
	m.appendOutput("[hooks completed]")
	compose := m.compose
	return m, func() tea.Msg {
		if err := compose.Up(context.Background(), msg.profiles); err != nil {
			return ProfileUpMsg{name: msg.name, err: err}
		}
		return ProfileUpMsg{name: msg.name}
	}
}

// handleProfileUp finalises the profile switch and triggers a status poll.
func (m *Model) handleProfileUp(msg ProfileUpMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.appendOutput(fmt.Sprintf("[profile switch error: %v]", msg.err))
	} else {
		m.appendOutput(fmt.Sprintf("[services started with profile %s]", msg.name))
	}
	return m, m.pollStatus
}

// readLogLine returns a tea.Cmd that reads one line from a log channel.
func readLogLine(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return OutputLineMsg{Line: "[stream ended]"}
		}
		return logBatchMsg{line: line, ch: ch}
	}
}

// readStream returns a tea.Cmd that reads one line from a command output channel.
func readStream(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return OutputLineMsg{Line: "[done]"}
		}
		return cmdStreamMsg{line: line, ch: ch}
	}
}

// cancelLogs cancels any active log or output stream.
func (m *Model) cancelLogs() {
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
}

// activeProfiles returns the compose profiles for the currently active profile name.
func (m *Model) activeProfiles() []string {
	for _, p := range m.cfg.Profiles {
		if p.Name == m.activeProfile {
			return p.Profiles
		}
	}
	return nil
}

// runCommand returns a tea.Cmd that executes a project command.
func (m *Model) runCommand(projectName string, cmd config.Command) tea.Cmd {
	compose := m.compose
	cfg := m.cfg

	proj := cfg.Projects[projectName]
	dir := proj.Path
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cfg.Dir, dir)
	}

	if cmd.Interactive {
		// Interactive commands suspend the TUI.
		if cmd.Service != "" {
			c := compose.ExecInteractive(context.Background(), cmd.Service, cmd.Run)
			return tea.ExecProcess(c, func(err error) tea.Msg {
				if err != nil {
					return OutputLineMsg{Line: fmt.Sprintf("[error: %v]", err)}
				}
				return OutputLineMsg{Line: fmt.Sprintf("[%s finished]", cmd.Name)}
			})
		}
		c := runner.RunHostInteractive(context.Background(), dir, cmd.Run)
		return tea.ExecProcess(c, func(err error) tea.Msg {
			if err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error: %v]", err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[%s finished]", cmd.Name)}
		})
	}

	// Non-interactive commands stream output.
	if cmd.Service != "" {
		ch, cancel := compose.Exec(context.Background(), cmd.Service, cmd.Run)
		m.logCancel = cancel
		return readStream(ch)
	}

	return func() tea.Msg {
		ch, err := runner.RunHost(context.Background(), dir, cmd.Run)
		if err != nil {
			return OutputLineMsg{Line: fmt.Sprintf("[error: %v]", err)}
		}
		line, ok := <-ch
		if !ok {
			return OutputLineMsg{Line: "[done]"}
		}
		return cmdStreamMsg{line: line, ch: ch}
	}
}
