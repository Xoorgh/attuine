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

	"oxorg/attuine/internal/config"
	"oxorg/attuine/internal/docker"
	"oxorg/attuine/internal/runner"
	"oxorg/attuine/internal/state"
)

// ServiceView holds all service/Docker state and implements View.
type ServiceView struct {
	cfg     *config.Config
	compose *docker.Compose

	services     []Service
	entries      []sidebarEntry
	cursor       int
	expandedName string
	projectNames []string

	output      viewport.Model
	outputLines []string

	showOverlay   bool
	overlayType   string
	overlayCursor int

	activeProfile string
	profileNames  []string

	spinner   spinner.Model
	logCancel context.CancelFunc

	stateDir     string
	runningCount int

	activePanel *Panel // pointer to shell's activePanel
	initialized bool   // tracks whether Init has seeded services
}

// NewServiceView creates a ServiceView from config, mirroring the old Model.New.
func NewServiceView(cfg *config.Config, stateDir string, activePanel *Panel) *ServiceView {
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

	return &ServiceView{
		cfg:           cfg,
		compose:       docker.NewCompose(cfg.ComposeFile, cfg.ComposeEnv, cfg.Dir),
		projectNames:  projectNames,
		profileNames:  profileNames,
		activeProfile: activeProfile,
		spinner:       sp,
		output:        vp,
		stateDir:      stateDir,
		activePanel:   activePanel,
	}
}

// ---------------------------------------------------------------------------
// View interface
// ---------------------------------------------------------------------------

// Init seeds the service list and starts the spinner + poll. Safe to call
// multiple times — re-polls status but only seeds services on the first call.
func (sv *ServiceView) Init() tea.Cmd {
	if !sv.initialized {
		// Seed service list from compose file so services appear even when stopped.
		profileSet := make(map[string]bool)
		for _, p := range sv.cfg.Profiles {
			for _, name := range p.Profiles {
				profileSet[name] = true
			}
		}
		allProfiles := make([]string, 0, len(profileSet))
		for name := range profileSet {
			allProfiles = append(allProfiles, name)
		}
		if names, err := sv.compose.ListServices(context.Background(), allProfiles); err == nil {
			for _, name := range names {
				sv.services = append(sv.services, Service{Name: name})
			}
		}

		sv.buildEntries()
		sv.initialized = true
	}

	return tea.Batch(
		sv.spinner.Tick,
		sv.pollStatus,
	)
}

// Update handles all messages relevant to the service view.
// Returns (updated view, cmd, handled). If handled is false the shell should
// process the message itself.
func (sv *ServiceView) Update(msg tea.Msg) (View, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if sv.showOverlay {
			v, cmd := sv.updateOverlay(msg)
			return v, cmd, true
		}
		return sv.updateKeypress(msg)

	case ServiceStatusMsg:
		if msg.Err == nil {
			sv.updateServices(msg.Statuses)
		}
		return sv, sv.scheduleStatusPoll(), true

	case LogLineMsg:
		sv.appendOutput(msg.Line)
		return sv, nil, true

	case OutputLineMsg:
		sv.appendOutput(msg.Line)
		return sv, nil, true

	case OutputDoneMsg:
		sv.appendOutput("[done]")
		return sv, nil, true

	case logBatchMsg:
		sv.appendOutput(msg.line)
		return sv, readLogLine(msg.ch), true

	case cmdStreamMsg:
		sv.appendOutput(msg.line)
		return sv, readStream(msg.ch), true

	case TickMsg:
		return sv, sv.pollStatus, true

	case ProfileDownMsg:
		v, cmd := sv.handleProfileDown(msg)
		return v, cmd, true

	case HookStartMsg:
		v, cmd := sv.handleHookStart(msg)
		return v, cmd, true

	case HookStreamMsg:
		v, cmd := sv.handleHookStream(msg)
		return v, cmd, true

	case HookDoneMsg:
		v, cmd := sv.handleHookDone(msg)
		return v, cmd, true

	case ProfileUpMsg:
		v, cmd := sv.handleProfileUp(msg)
		return v, cmd, true

	case spinner.TickMsg:
		var cmd tea.Cmd
		sv.spinner, cmd = sv.spinner.Update(msg)
		return sv, cmd, true
	}

	return sv, nil, false
}

// RenderSidebar renders the left sidebar panel.
func (sv *ServiceView) RenderSidebar(width, height int, focused bool) string {
	var lines []string

	// Profile header.
	lines = append(lines, profileHeaderStyle.Render("Profile: "+sv.activeProfile+"  [p]"))
	// Bulk action hints.
	lines = append(lines, bulkHintStyle.Render("[U]p All  [D]own  [R]ebuild"))
	// Blank line.
	lines = append(lines, "")

	// Render each sidebar entry.
	for i, entry := range sv.entries {
		isCursor := i == sv.cursor

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
	if focused {
		border = focusedBorder
	}

	return border.
		Width(width).
		Height(height).
		Render(content)
}

// RenderOutput renders the right output viewport panel.
func (sv *ServiceView) RenderOutput(width, height int, focused bool) string {
	sv.output.Width = width
	sv.output.Height = height

	border := unfocusedBorder
	if focused {
		border = focusedBorder
	}

	return border.
		Width(width).
		Height(height).
		Render(sv.output.View())
}

// Overlay returns the overlay string if one is active, or "".
func (sv *ServiceView) Overlay() string {
	if !sv.showOverlay {
		return ""
	}
	return sv.renderOverlay()
}

// StatusBarHints returns status bar hint strings for the current cursor entry.
func (sv *ServiceView) StatusBarHints() []string {
	var parts []string

	entry := sv.cursorEntry()
	if entry != nil {
		switch entry.kind {
		case entryService:
			parts = append(parts, actionKeyStyle.Render("u/d/r")+" "+actionDescStyle.Render("service"))
			parts = append(parts, actionKeyStyle.Render("U/D/R")+" "+actionDescStyle.Render("profile"))
			parts = append(parts, actionKeyStyle.Render("l")+" "+actionDescStyle.Render("logs"))
			parts = append(parts, actionKeyStyle.Render("s")+" "+actionDescStyle.Render("shell"))
		case entryCommand:
			parts = append(parts, actionKeyStyle.Render("enter")+" "+actionDescStyle.Render("run"))
			parts = append(parts, actionKeyStyle.Render("esc")+" "+actionDescStyle.Render("back"))
		case entryProject:
			parts = append(parts, actionKeyStyle.Render("enter")+" "+actionDescStyle.Render("expand"))
			parts = append(parts, actionKeyStyle.Render("U/D/R")+" "+actionDescStyle.Render("profile"))
		}
	} else {
		parts = append(parts, actionKeyStyle.Render("U/D/R")+" "+actionDescStyle.Render("profile"))
	}

	parts = append(parts, actionKeyStyle.Render("?")+" "+actionDescStyle.Render("help"))

	if sv.activeProfile != "" {
		parts = append(parts, actionKeyStyle.Render("profile:")+" "+actionDescStyle.Render(sv.activeProfile))
	}
	parts = append(parts, actionDescStyle.Render(fmt.Sprintf("%d/%d running", sv.runningCount, len(sv.services))))

	return parts
}

// HelpBindings returns service-specific key binding groups for the help view.
func (sv *ServiceView) HelpBindings() [][]key.Binding {
	return [][]key.Binding{
		{Keys.ServiceUp, Keys.ServiceDown, Keys.ServiceRebuild, Keys.ServiceLogs, Keys.ServiceShell},
		{Keys.BulkUp, Keys.BulkDown, Keys.BulkRebuild},
		{Keys.Clear, Keys.PageUp, Keys.PageDown, Keys.GoTop, Keys.GoBottom},
	}
}

// ---------------------------------------------------------------------------
// buildEntries — construct the flat sidebar list
// ---------------------------------------------------------------------------

func (sv *ServiceView) buildEntries() {
	// Remember what was under the cursor so we can restore position.
	var oldEntry *sidebarEntry
	if sv.cursor >= 0 && sv.cursor < len(sv.entries) {
		cp := sv.entries[sv.cursor]
		oldEntry = &cp
	}

	sv.entries = nil

	// Build a set of service names for quick lookup.
	serviceNameSet := make(map[string]bool, len(sv.services))
	for _, svc := range sv.services {
		serviceNameSet[svc.Name] = true
	}

	// 1. Service entries (and their commands when expanded).
	for _, svc := range sv.services {
		sv.entries = append(sv.entries, sidebarEntry{
			kind:  entryService,
			name:  svc.Name,
			state: svc.State,
			ports: svc.Ports,
		})

		if sv.expandedName == svc.Name {
			// Find a project whose name matches this service.
			if proj, ok := sv.cfg.Projects[svc.Name]; ok {
				for i := range proj.Commands {
					cmd := proj.Commands[i]
					sv.entries = append(sv.entries, sidebarEntry{
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
	for _, pn := range sv.projectNames {
		if !serviceNameSet[pn] {
			standalone = append(standalone, pn)
		}
	}

	if len(standalone) > 0 {
		sv.entries = append(sv.entries, sidebarEntry{
			kind: entryHeader,
			name: "Projects",
		})

		for _, pn := range standalone {
			sv.entries = append(sv.entries, sidebarEntry{
				kind:    entryProject,
				name:    pn,
				project: pn,
			})

			if sv.expandedName == pn {
				if proj, ok := sv.cfg.Projects[pn]; ok {
					for i := range proj.Commands {
						cmd := proj.Commands[i]
						sv.entries = append(sv.entries, sidebarEntry{
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
	sv.restoreCursor(oldEntry)
}

// restoreCursor tries to place the cursor back on the same logical item after
// a rebuild. If the item no longer exists (e.g. collapsed command), move to
// the parent service/project.
func (sv *ServiceView) restoreCursor(old *sidebarEntry) {
	if old == nil || len(sv.entries) == 0 {
		// Ensure cursor is valid.
		if sv.cursor >= len(sv.entries) {
			sv.cursor = len(sv.entries) - 1
		}
		if sv.cursor < 0 {
			sv.cursor = 0
		}
		return
	}

	// Try exact match.
	for i, e := range sv.entries {
		if e.kind == old.kind && e.name == old.name && e.project == old.project {
			sv.cursor = i
			return
		}
	}

	// If old was a command entry, move to the parent service or project.
	if old.kind == entryCommand {
		parentName := old.service
		if parentName == "" {
			parentName = old.project
		}
		for i, e := range sv.entries {
			if (e.kind == entryService || e.kind == entryProject) && e.name == parentName {
				sv.cursor = i
				return
			}
		}
	}

	// Clamp.
	if sv.cursor >= len(sv.entries) {
		sv.cursor = len(sv.entries) - 1
	}
	if sv.cursor < 0 {
		sv.cursor = 0
	}
}

// ---------------------------------------------------------------------------
// Cursor navigation
// ---------------------------------------------------------------------------

func (sv *ServiceView) cursorEntry() *sidebarEntry {
	if sv.cursor >= 0 && sv.cursor < len(sv.entries) {
		return &sv.entries[sv.cursor]
	}
	return nil
}

func (sv *ServiceView) moveCursorDown() {
	for i := sv.cursor + 1; i < len(sv.entries); i++ {
		if sv.entries[i].kind != entryHeader {
			sv.cursor = i
			return
		}
	}
}

func (sv *ServiceView) moveCursorUp() {
	for i := sv.cursor - 1; i >= 0; i-- {
		if sv.entries[i].kind != entryHeader {
			sv.cursor = i
			return
		}
	}
}

// selectedServiceName returns the name of the service related to the current
// cursor entry, or "" if the cursor is on a non-service item.
func (sv *ServiceView) selectedServiceName() string {
	entry := sv.cursorEntry()
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
// Key handling
// ---------------------------------------------------------------------------

func (sv *ServiceView) updateKeypress(msg tea.KeyMsg) (View, tea.Cmd, bool) {
	// Profile key is a service-view concern.
	if key.Matches(msg, Keys.Profile) {
		if len(sv.profileNames) > 0 {
			sv.showOverlay = true
			sv.overlayType = "profile"
			sv.overlayCursor = 0
			for i, name := range sv.profileNames {
				if name == sv.activeProfile {
					sv.overlayCursor = i
					break
				}
			}
		}
		return sv, nil, true
	}

	// Bulk actions.
	switch {
	case key.Matches(msg, Keys.BulkUp):
		sv.appendOutput(fmt.Sprintf("[bringing up profile %s...]", sv.activeProfile))
		return sv, sv.bringUpProfile(), true

	case key.Matches(msg, Keys.BulkDown):
		compose := sv.compose
		var allProfiles []string
		for _, p := range sv.cfg.Profiles {
			allProfiles = append(allProfiles, p.Profiles...)
		}
		sv.appendOutput("[bringing down all services...]")
		return sv, func() tea.Msg {
			if err := compose.Down(context.Background(), allProfiles); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error: %v]", err)}
			}
			return OutputLineMsg{Line: "[all services stopped]"}
		}, true

	case key.Matches(msg, Keys.BulkRebuild):
		compose := sv.compose
		sv.appendOutput("[rebuilding all services...]")
		return sv, func() tea.Msg {
			if err := compose.Build(context.Background()); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error: %v]", err)}
			}
			return OutputLineMsg{Line: "[all services rebuilt]"}
		}, true
	}

	// Delegate to panel-specific handlers.
	switch *sv.activePanel {
	case PanelSidebar:
		return sv.updateSidebar(msg)
	case PanelOutput:
		return sv.updateOutput(msg)
	}

	return sv, nil, false
}

// updateSidebar handles key presses when the sidebar has focus.
func (sv *ServiceView) updateSidebar(msg tea.KeyMsg) (View, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.Down):
		sv.moveCursorDown()
		return sv, nil, true

	case key.Matches(msg, Keys.Up):
		sv.moveCursorUp()
		return sv, nil, true

	case key.Matches(msg, Keys.Enter):
		v, cmd := sv.handleSidebarEnter()
		return v, cmd, true

	case key.Matches(msg, Keys.Cancel):
		if sv.expandedName != "" {
			sv.expandedName = ""
			sv.buildEntries()
		}
		return sv, nil, true

	case key.Matches(msg, Keys.ServiceUp):
		svc := sv.selectedServiceName()
		if svc == "" {
			return sv, nil, true
		}
		profiles := sv.activeProfiles()
		compose := sv.compose
		sv.appendOutput(fmt.Sprintf("[starting %s...]", svc))
		return sv, func() tea.Msg {
			if err := compose.Up(context.Background(), profiles, svc); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error starting %s: %v]", svc, err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[%s started]", svc)}
		}, true

	case key.Matches(msg, Keys.ServiceDown):
		svc := sv.selectedServiceName()
		if svc == "" {
			return sv, nil, true
		}
		compose := sv.compose
		sv.appendOutput(fmt.Sprintf("[stopping %s...]", svc))
		return sv, func() tea.Msg {
			if err := compose.Stop(context.Background(), svc); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error stopping %s: %v]", svc, err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[%s stopped]", svc)}
		}, true

	case key.Matches(msg, Keys.ServiceRebuild):
		svc := sv.selectedServiceName()
		if svc == "" {
			return sv, nil, true
		}
		compose := sv.compose
		sv.appendOutput(fmt.Sprintf("[rebuilding %s...]", svc))
		return sv, func() tea.Msg {
			if err := compose.Build(context.Background(), svc); err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[error rebuilding %s: %v]", svc, err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[%s rebuilt]", svc)}
		}, true

	case key.Matches(msg, Keys.ServiceLogs):
		svc := sv.selectedServiceName()
		if svc == "" {
			return sv, nil, true
		}
		sv.cancelLogs()
		sv.appendOutput(fmt.Sprintf("[streaming logs for %s...]", svc))
		ch, cancel := sv.compose.Logs(context.Background(), svc)
		sv.logCancel = cancel
		return sv, readLogLine(ch), true

	case key.Matches(msg, Keys.ServiceShell):
		svc := sv.selectedServiceName()
		if svc == "" {
			return sv, nil, true
		}
		c := sv.compose.Shell(context.Background(), svc)
		return sv, tea.ExecProcess(c, func(err error) tea.Msg {
			if err != nil {
				return OutputLineMsg{Line: fmt.Sprintf("[shell error: %v]", err)}
			}
			return OutputLineMsg{Line: fmt.Sprintf("[shell for %s closed]", svc)}
		}), true
	}

	return sv, nil, false
}

// handleSidebarEnter handles Enter on a sidebar entry.
func (sv *ServiceView) handleSidebarEnter() (View, tea.Cmd) {
	entry := sv.cursorEntry()
	if entry == nil {
		return sv, nil
	}

	switch entry.kind {
	case entryService, entryProject:
		name := entry.name
		if sv.expandedName == name {
			sv.expandedName = "" // collapse
		} else {
			sv.expandedName = name // expand
		}
		sv.buildEntries()
		return sv, nil

	case entryCommand:
		projectName := entry.project
		cmd := *entry.command
		sv.cancelLogs()
		sv.appendOutput(fmt.Sprintf("[running %s: %s]", cmd.Name, cmd.Run))
		return sv, sv.runCommand(projectName, cmd)
	}

	return sv, nil
}

// updateOutput handles key presses when the output panel has focus.
func (sv *ServiceView) updateOutput(msg tea.KeyMsg) (View, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.Clear):
		sv.outputLines = nil
		sv.output.SetContent("")
		sv.output.GotoTop()
	case key.Matches(msg, Keys.PageUp):
		sv.output.HalfViewUp()
	case key.Matches(msg, Keys.PageDown):
		sv.output.HalfViewDown()
	case key.Matches(msg, Keys.GoTop):
		sv.output.GotoTop()
	case key.Matches(msg, Keys.GoBottom):
		sv.output.GotoBottom()
	case key.Matches(msg, Keys.Up):
		sv.output.LineUp(1)
	case key.Matches(msg, Keys.Down):
		sv.output.LineDown(1)
	}
	return sv, nil, true
}

// updateOverlay handles key presses when an overlay is visible.
func (sv *ServiceView) updateOverlay(msg tea.KeyMsg) (View, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Cancel):
		sv.showOverlay = false
		return sv, nil

	case key.Matches(msg, Keys.Down):
		if sv.overlayType == "profile" && sv.overlayCursor < len(sv.profileNames)-1 {
			sv.overlayCursor++
		}

	case key.Matches(msg, Keys.Up):
		if sv.overlayType == "profile" && sv.overlayCursor > 0 {
			sv.overlayCursor--
		}

	case key.Matches(msg, Keys.Enter):
		if sv.overlayType == "profile" && sv.overlayCursor < len(sv.profileNames) {
			selected := sv.profileNames[sv.overlayCursor]
			if selected != sv.activeProfile {
				sv.activeProfile = selected
				sv.showOverlay = false
				sv.appendOutput(fmt.Sprintf("[switching profile to %s...]", selected))
				return sv, sv.switchProfile(selected)
			}
			sv.showOverlay = false
		}
	}

	return sv, nil
}

// renderOverlay renders a centered overlay dialog (e.g., profile picker).
func (sv *ServiceView) renderOverlay() string {
	if sv.overlayType != "profile" {
		return ""
	}

	var lines []string
	lines = append(lines, titleStyle.Render("Select Profile"))
	lines = append(lines, "")

	for i, name := range sv.profileNames {
		style := navInactive
		prefix := "  "
		if i == sv.overlayCursor {
			style = selectedStyle
			prefix = "▸ "
		}
		suffix := ""
		if name == sv.activeProfile {
			suffix = " (active)"
		}
		lines = append(lines, style.Render(prefix+name+suffix))
	}

	lines = append(lines, "")
	lines = append(lines, navInactive.Render("  esc to cancel"))

	content := strings.Join(lines, "\n")
	return overlayStyle.Render(content)
}

// ---------------------------------------------------------------------------
// Service status
// ---------------------------------------------------------------------------

// updateServices refreshes the internal service list from a status poll result.
func (sv *ServiceView) updateServices(statuses []docker.ServiceStatus) {
	statusMap := make(map[string]docker.ServiceStatus)
	for _, s := range statuses {
		statusMap[s.Service] = s
	}

	sv.runningCount = 0
	for i := range sv.services {
		if s, ok := statusMap[sv.services[i].Name]; ok {
			sv.services[i].State = s.State
			sv.services[i].Ports = s.Ports
			delete(statusMap, s.Service)
		} else {
			sv.services[i].State = ""
			sv.services[i].Ports = nil
		}
		if sv.services[i].State == "running" {
			sv.runningCount++
		}
	}

	// Add any new services we haven't seen before.
	for name, s := range statusMap {
		svc := Service{Name: name, State: s.State, Ports: s.Ports}
		sv.services = append(sv.services, svc)
		if s.State == "running" {
			sv.runningCount++
		}
	}

	// Keep cursor in bounds.
	if sv.cursor >= len(sv.entries) && len(sv.entries) > 0 {
		sv.cursor = len(sv.entries) - 1
	}

	sv.buildEntries()
}

// pollStatus polls docker compose for current service statuses.
func (sv *ServiceView) pollStatus() tea.Msg {
	statuses, err := sv.compose.Status(context.Background())
	return ServiceStatusMsg{Statuses: statuses, Err: err}
}

// scheduleStatusPoll returns a tea.Cmd that fires a TickMsg after a delay.
func (sv *ServiceView) scheduleStatusPoll() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return TickMsg{}
	})
}

// ---------------------------------------------------------------------------
// Profile switching
// ---------------------------------------------------------------------------

// switchProfile returns a tea.Cmd that starts the profile switch by downing
// the current stack. The rest of the flow is driven by message handlers.
func (sv *ServiceView) switchProfile(profileName string) tea.Cmd {
	compose := sv.compose
	cfg := sv.cfg

	var targetProfiles []string
	var allProfiles []string
	for _, p := range cfg.Profiles {
		allProfiles = append(allProfiles, p.Profiles...)
		if p.Name == profileName {
			targetProfiles = p.Profiles
		}
	}

	return func() tea.Msg {
		if err := compose.Down(context.Background(), allProfiles); err != nil {
			return ProfileUpMsg{name: profileName, err: fmt.Errorf("compose down: %w", err)}
		}
		return ProfileDownMsg{profiles: targetProfiles, name: profileName}
	}
}

// bringUpProfile starts the active profile without first bringing down
// the current stack. It runs pre_up hooks then compose up.
func (sv *ServiceView) bringUpProfile() tea.Cmd {
	profiles := sv.activeProfiles()
	hooks := sv.cfg.Hooks.PreUp
	name := sv.activeProfile // capture before closure
	if len(hooks) > 0 {
		return func() tea.Msg {
			return HookStartMsg{
				hook:      hooks[0],
				remaining: hooks[1:],
				profiles:  profiles,
				name:      name,
			}
		}
	}
	compose := sv.compose
	return func() tea.Msg {
		if err := compose.Up(context.Background(), profiles); err != nil {
			return ProfileUpMsg{name: name, err: err}
		}
		return ProfileUpMsg{name: name}
	}
}

func (sv *ServiceView) handleProfileDown(msg ProfileDownMsg) (View, tea.Cmd) {
	hooks := sv.cfg.Hooks.PreUp
	if len(hooks) > 0 {
		return sv, func() tea.Msg {
			return HookStartMsg{
				hook:      hooks[0],
				remaining: hooks[1:],
				profiles:  msg.profiles,
				name:      msg.name,
			}
		}
	}

	compose := sv.compose
	return sv, func() tea.Msg {
		if err := compose.Up(context.Background(), msg.profiles); err != nil {
			return ProfileUpMsg{name: msg.name, err: err}
		}
		return ProfileUpMsg{name: msg.name}
	}
}

func (sv *ServiceView) handleHookStart(msg HookStartMsg) (View, tea.Cmd) {
	sv.appendOutput(fmt.Sprintf("[running hook: %s...]", msg.hook.Name))
	dir := sv.cfg.Dir
	return sv, func() tea.Msg {
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

func (sv *ServiceView) handleHookStream(msg HookStreamMsg) (View, tea.Cmd) {
	sv.appendOutput(msg.line)
	return sv, func() tea.Msg {
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

func (sv *ServiceView) handleHookDone(msg HookDoneMsg) (View, tea.Cmd) {
	sv.appendOutput("[hooks completed]")
	compose := sv.compose
	return sv, func() tea.Msg {
		if err := compose.Up(context.Background(), msg.profiles); err != nil {
			return ProfileUpMsg{name: msg.name, err: err}
		}
		return ProfileUpMsg{name: msg.name}
	}
}

func (sv *ServiceView) handleProfileUp(msg ProfileUpMsg) (View, tea.Cmd) {
	if msg.err != nil {
		sv.appendOutput(fmt.Sprintf("[profile switch error: %v]", msg.err))
	} else {
		sv.appendOutput(fmt.Sprintf("[services started with profile %s]", msg.name))
	}
	// Save state on successful profile up.
	if msg.err == nil {
		sv.saveProfileState(msg.name)
	}
	return sv, sv.pollStatus
}

// ---------------------------------------------------------------------------
// State persistence
// ---------------------------------------------------------------------------

func (sv *ServiceView) saveProfileState(profileName string) {
	if sv.stateDir != "" {
		s := &state.State{LastProfile: profileName}
		_ = s.Save(sv.stateDir) // best-effort
	}
}

// ---------------------------------------------------------------------------
// Output & logging
// ---------------------------------------------------------------------------

// appendOutput adds a line to the output buffer and updates the viewport.
// The buffer is capped at 10000 lines to prevent unbounded memory growth.
func (sv *ServiceView) appendOutput(line string) {
	sv.outputLines = append(sv.outputLines, line)
	if len(sv.outputLines) > 10000 {
		sv.outputLines = sv.outputLines[len(sv.outputLines)-10000:]
	}
	content := strings.Join(sv.outputLines, "\n")
	sv.output.SetContent(content)
	sv.output.GotoBottom()
}

// cancelLogs cancels any active log or output stream.
func (sv *ServiceView) cancelLogs() {
	if sv.logCancel != nil {
		sv.logCancel()
		sv.logCancel = nil
	}
}

// activeProfiles returns the compose profiles for the currently active profile name.
func (sv *ServiceView) activeProfiles() []string {
	for _, p := range sv.cfg.Profiles {
		if p.Name == sv.activeProfile {
			return p.Profiles
		}
	}
	return nil
}

// runCommand returns a tea.Cmd that executes a project command.
func (sv *ServiceView) runCommand(projectName string, cmd config.Command) tea.Cmd {
	compose := sv.compose
	cfg := sv.cfg

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
		sv.logCancel = cancel
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
