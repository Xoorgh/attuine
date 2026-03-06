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
	"oxorg/attuine/internal/state"
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
// Model
// ---------------------------------------------------------------------------

// Model is the top-level Bubble Tea model for attuine.
type Model struct {
	cfg     *config.Config
	compose *docker.Compose

	// dimensions
	width, height int
	sidebarWidth  int
	outputWidth   int
	contentHeight int

	activePanel Panel

	// Sidebar
	services     []Service      // raw service data
	entries      []sidebarEntry // flat rendered list
	cursor       int            // position in entries
	expandedName string         // which service/project is expanded ("" for none)

	// Projects
	projectNames []string // sorted project names from config

	// Output
	output      viewport.Model
	outputLines []string

	// Overlay
	showOverlay   bool
	overlayType   string
	overlayCursor int
	showHelp      bool

	// Profile
	activeProfile string
	profileNames  []string

	// Components
	spinner   spinner.Model
	logCancel context.CancelFunc

	// State persistence
	stateDir string

	// Status tracking
	runningCount int
}

// New creates a new Model from the loaded config.
func New(cfg *config.Config, stateDir string) *Model {
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

	// Load last profile from state if stateDir is set.
	if stateDir != "" {
		if s, err := state.Load(stateDir); err == nil && s.LastProfile != "" {
			for _, name := range profileNames {
				if name == s.LastProfile {
					activeProfile = s.LastProfile
					break
				}
			}
		}
	}

	// Set up spinner.
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	// Set up viewport.
	vp := viewport.New(80, 20)

	m := &Model{
		cfg:           cfg,
		compose:       docker.NewCompose(cfg.ComposeFile, cfg.ComposeEnv, cfg.Dir),
		activePanel:   PanelSidebar,
		projectNames:  projectNames,
		profileNames:  profileNames,
		activeProfile: activeProfile,
		spinner:       sp,
		output:        vp,
		stateDir:      stateDir,
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

	m.buildEntries()

	return tea.Batch(
		m.spinner.Tick,
		m.pollStatus,
	)
}

// ---------------------------------------------------------------------------
// buildEntries — construct the flat sidebar list
// ---------------------------------------------------------------------------

func (m *Model) buildEntries() {
	// Remember what was under the cursor so we can restore position.
	var oldEntry *sidebarEntry
	if m.cursor >= 0 && m.cursor < len(m.entries) {
		cp := m.entries[m.cursor]
		oldEntry = &cp
	}

	m.entries = nil

	// Build a set of service names for quick lookup.
	serviceNameSet := make(map[string]bool, len(m.services))
	for _, svc := range m.services {
		serviceNameSet[svc.Name] = true
	}

	// 1. Service entries (and their commands when expanded).
	for _, svc := range m.services {
		m.entries = append(m.entries, sidebarEntry{
			kind:  entryService,
			name:  svc.Name,
			state: svc.State,
			ports: svc.Ports,
		})

		if m.expandedName == svc.Name {
			// Find a project whose name matches this service.
			if proj, ok := m.cfg.Projects[svc.Name]; ok {
				for i := range proj.Commands {
					cmd := proj.Commands[i]
					m.entries = append(m.entries, sidebarEntry{
						kind:    entryCommand,
						name:    cmd.Name,
						service: svc.Name,
						project: svc.Name,
						command: &cmd,
					})
				}
			}
		}
	}

	// 2. Standalone projects (project name doesn't match any service).
	var standalone []string
	for _, pn := range m.projectNames {
		if !serviceNameSet[pn] {
			standalone = append(standalone, pn)
		}
	}

	if len(standalone) > 0 {
		m.entries = append(m.entries, sidebarEntry{
			kind: entryHeader,
			name: "Projects",
		})

		for _, pn := range standalone {
			m.entries = append(m.entries, sidebarEntry{
				kind:    entryProject,
				name:    pn,
				project: pn,
			})

			if m.expandedName == pn {
				if proj, ok := m.cfg.Projects[pn]; ok {
					for i := range proj.Commands {
						cmd := proj.Commands[i]
						m.entries = append(m.entries, sidebarEntry{
							kind:    entryCommand,
							name:    cmd.Name,
							service: "", // standalone project, no service
							project: pn,
							command: &cmd,
						})
					}
				}
			}
		}
	}

	// Restore cursor position.
	m.restoreCursor(oldEntry)
}

// restoreCursor tries to place the cursor back on the same logical item after
// a rebuild. If the item no longer exists (e.g. collapsed command), move to
// the parent service/project.
func (m *Model) restoreCursor(old *sidebarEntry) {
	if old == nil || len(m.entries) == 0 {
		// Ensure cursor is valid.
		if m.cursor >= len(m.entries) {
			m.cursor = len(m.entries) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return
	}

	// Try exact match.
	for i, e := range m.entries {
		if e.kind == old.kind && e.name == old.name && e.project == old.project {
			m.cursor = i
			return
		}
	}

	// If old was a command entry, move to the parent service or project.
	if old.kind == entryCommand {
		parentName := old.service
		if parentName == "" {
			parentName = old.project
		}
		for i, e := range m.entries {
			if (e.kind == entryService || e.kind == entryProject) && e.name == parentName {
				m.cursor = i
				return
			}
		}
	}

	// Clamp.
	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// ---------------------------------------------------------------------------
// Cursor navigation
// ---------------------------------------------------------------------------

func (m *Model) cursorEntry() *sidebarEntry {
	if m.cursor >= 0 && m.cursor < len(m.entries) {
		return &m.entries[m.cursor]
	}
	return nil
}

func (m *Model) moveCursorDown() {
	for i := m.cursor + 1; i < len(m.entries); i++ {
		if m.entries[i].kind != entryHeader {
			m.cursor = i
			return
		}
	}
}

func (m *Model) moveCursorUp() {
	for i := m.cursor - 1; i >= 0; i-- {
		if m.entries[i].kind != entryHeader {
			m.cursor = i
			return
		}
	}
}

// selectedServiceName returns the name of the service related to the current
// cursor entry, or "" if the cursor is on a non-service item.
func (m *Model) selectedServiceName() string {
	entry := m.cursorEntry()
	if entry == nil {
		return ""
	}
	switch entry.kind {
	case entryService:
		return entry.name
	case entryCommand:
		return entry.service // parent service (may be "" for host commands)
	}
	return ""
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

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
		if m.showOverlay {
			return m.updateOverlay(msg)
		}
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

// ---------------------------------------------------------------------------
// Key handling
// ---------------------------------------------------------------------------

func (m *Model) updateKeypress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys (work regardless of panel focus).
	switch {
	case key.Matches(msg, Keys.Quit):
		m.cancelLogs()
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

	case key.Matches(msg, Keys.Profile):
		if len(m.profileNames) > 0 {
			m.showOverlay = true
			m.overlayType = "profile"
			m.overlayCursor = 0
			for i, name := range m.profileNames {
				if name == m.activeProfile {
					m.overlayCursor = i
					break
				}
			}
		}
		return m, nil

	case key.Matches(msg, Keys.BulkUp):
		m.appendOutput(fmt.Sprintf("[bringing up profile %s...]", m.activeProfile))
		return m, m.bringUpProfile()

	case key.Matches(msg, Keys.BulkDown):
		compose := m.compose
		m.appendOutput("[bringing down all services...]")
		return m, func() tea.Msg {
			if err := compose.Down(context.Background()); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error: %v]", err)}
			}
			return OutputLineMsg{Line: "[all services stopped]"}
		}

	case key.Matches(msg, Keys.BulkRebuild):
		compose := m.compose
		m.appendOutput("[rebuilding all services...]")
		return m, func() tea.Msg {
			if err := compose.Build(context.Background()); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error: %v]", err)}
			}
			return OutputLineMsg{Line: "[all services rebuilt]"}
		}
	}

	// Delegate to panel-specific handlers.
	switch m.activePanel {
	case PanelSidebar:
		return m.updateSidebar(msg)
	case PanelOutput:
		return m.updateOutput(msg)
	}

	return m, nil
}

// updateSidebar handles key presses when the sidebar has focus.
func (m *Model) updateSidebar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Down):
		m.moveCursorDown()
		return m, nil

	case key.Matches(msg, Keys.Up):
		m.moveCursorUp()
		return m, nil

	case key.Matches(msg, Keys.Enter):
		return m.handleSidebarEnter()

	case key.Matches(msg, Keys.ServiceUp):
		svc := m.selectedServiceName()
		if svc == "" {
			return m, nil
		}
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
		svc := m.selectedServiceName()
		if svc == "" {
			return m, nil
		}
		compose := m.compose
		m.appendOutput(fmt.Sprintf("[stopping %s...]", svc))
		return m, func() tea.Msg {
			if err := compose.Stop(context.Background(), svc); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error stopping %s: %v]", svc, err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[%s stopped]", svc)}
		}

	case key.Matches(msg, Keys.ServiceRebuild):
		svc := m.selectedServiceName()
		if svc == "" {
			return m, nil
		}
		compose := m.compose
		m.appendOutput(fmt.Sprintf("[rebuilding %s...]", svc))
		return m, func() tea.Msg {
			if err := compose.Build(context.Background(), svc); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error rebuilding %s: %v]", svc, err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[%s rebuilt]", svc)}
		}

	case key.Matches(msg, Keys.ServiceLogs):
		svc := m.selectedServiceName()
		if svc == "" {
			return m, nil
		}
		m.cancelLogs()
		m.appendOutput(fmt.Sprintf("[streaming logs for %s...]", svc))
		ch, cancel := m.compose.Logs(context.Background(), svc)
		m.logCancel = cancel
		return m, readLogLine(ch)

	case key.Matches(msg, Keys.ServiceShell):
		svc := m.selectedServiceName()
		if svc == "" {
			return m, nil
		}
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

// handleSidebarEnter handles Enter on a sidebar entry.
func (m *Model) handleSidebarEnter() (tea.Model, tea.Cmd) {
	entry := m.cursorEntry()
	if entry == nil {
		return m, nil
	}

	switch entry.kind {
	case entryService, entryProject:
		name := entry.name
		if m.expandedName == name {
			m.expandedName = "" // collapse
		} else {
			m.expandedName = name // expand
		}
		m.buildEntries()
		return m, nil

	case entryCommand:
		projectName := entry.project
		cmd := *entry.command
		m.cancelLogs()
		m.appendOutput(fmt.Sprintf("[running %s: %s]", cmd.Name, cmd.Run))
		return m, m.runCommand(projectName, cmd)
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
				// Save state on profile switch.
				m.saveProfileState(selected)
				m.appendOutput(fmt.Sprintf("[switching profile to %s...]", selected))
				return m, m.switchProfile(selected)
			}
			m.showOverlay = false
		}
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// View renders the entire TUI.
func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	if m.showHelp {
		return m.renderHelp()
	}

	sidebar := m.renderSidebar()
	output := m.renderOutputPanel()
	panels := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, output)

	statusBar := m.renderStatusBar()
	dashboard := lipgloss.JoinVertical(lipgloss.Left, panels, statusBar)

	if m.showOverlay {
		overlay := m.renderOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(lipgloss.Color("0")),
		)
	}

	return dashboard
}

// renderSidebar renders the left sidebar panel.
func (m *Model) renderSidebar() string {
	var lines []string

	// Profile header.
	lines = append(lines, profileHeaderStyle.Render("Profile: "+m.activeProfile+"  [p]"))
	// Bulk action hints.
	lines = append(lines, bulkHintStyle.Render("[U]p All  [D]own  [R]ebuild"))
	// Blank line.
	lines = append(lines, "")

	// Render each sidebar entry.
	for i, entry := range m.entries {
		isCursor := i == m.cursor

		switch entry.kind {
		case entryService:
			indicator := StatusIndicator(entry.state)
			name := entry.name
			if len(entry.ports) > 0 {
				name += " " + strings.Join(entry.ports, ",")
			}
			prefix := "  "
			style := navInactive
			if isCursor {
				prefix = "▸ "
				style = selectedStyle
			}
			lines = append(lines, fmt.Sprintf("%s %s", indicator, style.Render(prefix+name)))

		case entryCommand:
			prefix := "    "
			style := commandItemStyle
			if isCursor {
				prefix = "  ▸ "
				style = selectedStyle
			}
			lines = append(lines, style.Render(prefix+entry.name))

		case entryHeader:
			lines = append(lines, sectionDividerStyle.Render("── "+entry.name+" ──"))

		case entryProject:
			prefix := "  "
			style := navInactive
			if isCursor {
				prefix = "▸ "
				style = selectedStyle
			}
			lines = append(lines, style.Render(prefix+entry.name))
		}
	}

	content := strings.Join(lines, "\n")

	border := unfocusedBorder
	if m.activePanel == PanelSidebar {
		border = focusedBorder
	}

	return border.
		Width(m.sidebarWidth).
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

// renderStatusBar renders the bottom status bar.
func (m *Model) renderStatusBar() string {
	var parts []string

	parts = append(parts, actionKeyStyle.Render("u/d/r")+" "+actionDescStyle.Render("service"))
	parts = append(parts, actionKeyStyle.Render("U/D/R")+" "+actionDescStyle.Render("profile"))
	parts = append(parts, actionKeyStyle.Render("l")+" "+actionDescStyle.Render("logs"))
	parts = append(parts, actionKeyStyle.Render("s")+" "+actionDescStyle.Render("shell"))
	parts = append(parts, actionKeyStyle.Render("?")+" "+actionDescStyle.Render("help"))

	if m.activeProfile != "" {
		parts = append(parts, actionKeyStyle.Render("profile:")+" "+actionDescStyle.Render(m.activeProfile))
	}

	parts = append(parts, actionDescStyle.Render(fmt.Sprintf("%d/%d running", m.runningCount, len(m.services))))

	bar := strings.Join(parts, "  │  ")

	return statusBarStyle.
		Width(m.width).
		Render(bar)
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

// ---------------------------------------------------------------------------
// Service status
// ---------------------------------------------------------------------------

// updateServices refreshes the internal service list from a status poll result.
func (m *Model) updateServices(statuses []docker.ServiceStatus) {
	statusMap := make(map[string]docker.ServiceStatus)
	for _, s := range statuses {
		statusMap[s.Service] = s
	}

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
	if m.cursor >= len(m.entries) && len(m.entries) > 0 {
		m.cursor = len(m.entries) - 1
	}

	m.buildEntries()
}

// pollStatus polls docker compose for current service statuses.
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

// ---------------------------------------------------------------------------
// Profile switching
// ---------------------------------------------------------------------------

// switchProfile returns a tea.Cmd that starts the profile switch by downing
// the current stack. The rest of the flow is driven by message handlers.
func (m *Model) switchProfile(profileName string) tea.Cmd {
	compose := m.compose
	cfg := m.cfg

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

// bringUpProfile starts the active profile without first bringing down
// the current stack. It runs pre_up hooks then compose up.
func (m *Model) bringUpProfile() tea.Cmd {
	profiles := m.activeProfiles()
	hooks := m.cfg.Hooks.PreUp
	if len(hooks) > 0 {
		return func() tea.Msg {
			return HookStartMsg{
				hook:      hooks[0],
				remaining: hooks[1:],
				profiles:  profiles,
				name:      m.activeProfile,
			}
		}
	}
	compose := m.compose
	name := m.activeProfile
	return func() tea.Msg {
		if err := compose.Up(context.Background(), profiles); err != nil {
			return ProfileUpMsg{name: name, err: err}
		}
		return ProfileUpMsg{name: name}
	}
}

func (m *Model) handleProfileDown(msg ProfileDownMsg) (tea.Model, tea.Cmd) {
	hooks := m.cfg.Hooks.PreUp
	if len(hooks) > 0 {
		return m, func() tea.Msg {
			return HookStartMsg{
				hook:      hooks[0],
				remaining: hooks[1:],
				profiles:  msg.profiles,
				name:      msg.name,
			}
		}
	}

	compose := m.compose
	return m, func() tea.Msg {
		if err := compose.Up(context.Background(), msg.profiles); err != nil {
			return ProfileUpMsg{name: msg.name, err: err}
		}
		return ProfileUpMsg{name: msg.name}
	}
}

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

func (m *Model) handleHookStream(msg HookStreamMsg) (tea.Model, tea.Cmd) {
	m.appendOutput(msg.line)
	return m, func() tea.Msg {
		line, ok := <-msg.ch
		if !ok {
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

func (m *Model) handleProfileUp(msg ProfileUpMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.appendOutput(fmt.Sprintf("[profile switch error: %v]", msg.err))
	} else {
		m.appendOutput(fmt.Sprintf("[services started with profile %s]", msg.name))
	}
	// Save state on successful profile up.
	if msg.err == nil {
		m.saveProfileState(msg.name)
	}
	return m, m.pollStatus
}

// ---------------------------------------------------------------------------
// State persistence
// ---------------------------------------------------------------------------

func (m *Model) saveProfileState(profileName string) {
	if m.stateDir != "" {
		s := &state.State{LastProfile: profileName}
		_ = s.Save(m.stateDir) // best-effort
	}
}

// ---------------------------------------------------------------------------
// Output & logging
// ---------------------------------------------------------------------------

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
