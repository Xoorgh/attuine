package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"oxorg/attuine/internal/config"
	"oxorg/attuine/internal/git"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// gitEntryKind distinguishes sidebar entry types in the git view.
type gitEntryKind int

const (
	gitEntryRepo   gitEntryKind = iota
	gitEntryAction
)

// gitSidebarEntry is one item in the flat git sidebar list.
type gitSidebarEntry struct {
	kind   gitEntryKind
	name   string // display name (repo name or action label)
	repo   string // parent repo name
	action string // action identifier (checkout, pull, fetch, log)
}

// repoState holds the cached git status for a single repo.
type repoState struct {
	branch string
	clean  bool
	ahead  int
	behind int
	err    error
}

// ---------------------------------------------------------------------------
// Message types
// ---------------------------------------------------------------------------

// GitRepoStatusMsg carries updated statuses for all repos.
type GitRepoStatusMsg struct {
	States map[string]*repoState
}

// GitOutputLineMsg carries a single output line from a git operation.
type GitOutputLineMsg struct {
	Line string
}

// gitSyncStepMsg carries per-repo sync progress with chaining state.
type gitSyncStepMsg struct {
	line      string
	nextIdx   int
	synced    int
	skipped   int
	skipNames []string
	done      bool
}

// gitBranchStepMsg carries per-repo branch creation progress with chaining state.
type gitBranchStepMsg struct {
	line    string
	nextIdx int
	done    bool
}

// ---------------------------------------------------------------------------
// GitView
// ---------------------------------------------------------------------------

// GitView shows repo statuses in a sidebar with expandable per-repo actions.
type GitView struct {
	cfg        *config.Config
	repoNames  []string
	repoStates map[string]*repoState
	entries    []gitSidebarEntry
	cursor     int
	expandedRepo string
	output     viewport.Model
	outputLines []string
	activePanel *Panel

	// Overlay state for branch creation flow.
	showOverlay    bool
	overlayPhase   string // "branch_name" or "branch_select"
	branchInput    string
	repoSelections map[string]bool
	overlayCursor  int
	branchRepos    []string // selected repos for in-progress branch creation
}

// NewGitView creates a GitView from config.
func NewGitView(cfg *config.Config, activePanel *Panel) *GitView {
	var repoNames []string
	for name := range cfg.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	vp := viewport.New(80, 20)

	gv := &GitView{
		cfg:        cfg,
		repoNames:  repoNames,
		repoStates: make(map[string]*repoState),
		output:     vp,
		activePanel: activePanel,
	}
	gv.buildEntries()
	return gv
}

// ---------------------------------------------------------------------------
// View interface
// ---------------------------------------------------------------------------

// Init polls git status for all repos.
func (gv *GitView) Init() tea.Cmd {
	return gv.pollAllStatuses
}

// Update handles all messages relevant to the git view.
func (gv *GitView) Update(msg tea.Msg) (View, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Route to overlay handler first when overlay is visible.
		if gv.showOverlay {
			v, cmd := gv.updateOverlay(msg)
			return v, cmd, true
		}
		return gv.updateKeypress(msg)

	case GitRepoStatusMsg:
		gv.repoStates = msg.States
		gv.buildEntries()
		return gv, nil, true

	case GitOutputLineMsg:
		gv.appendOutput(msg.Line)
		return gv, gv.pollAllStatuses, true

	case gitSyncStepMsg:
		gv.appendOutput(msg.line)
		if msg.done {
			// Summarise and refresh.
			summary := fmt.Sprintf("[sync complete: %d synced, %d skipped]", msg.synced, msg.skipped)
			if len(msg.skipNames) > 0 {
				summary = fmt.Sprintf("[sync complete: %d synced, %d skipped (%s)]",
					msg.synced, msg.skipped, strings.Join(msg.skipNames, ", "))
			}
			gv.appendOutput(summary)
			return gv, gv.pollAllStatuses, true
		}
		return gv, gv.syncRepo(msg.nextIdx, msg.synced, msg.skipped, msg.skipNames), true

	case gitBranchStepMsg:
		gv.appendOutput(msg.line)
		if msg.done {
			gv.appendOutput("[branch creation complete]")
			return gv, gv.pollAllStatuses, true
		}
		return gv, gv.createBranchStep(msg.nextIdx), true
	}

	return gv, nil, false
}

// RenderSidebar renders the left sidebar panel for the git view.
func (gv *GitView) RenderSidebar(width, height int, focused bool) string {
	var lines []string

	lines = append(lines, profileHeaderStyle.Render("Git Repos"))
	lines = append(lines, bulkHintStyle.Render("[S]ync All  [B]ranch  [C]ommit subs"))
	lines = append(lines, "")

	for i, entry := range gv.entries {
		isCursor := i == gv.cursor

		switch entry.kind {
		case gitEntryRepo:
			st := gv.repoStates[entry.name]
			indicator := statusUnknown.String()
			detail := ""
			if st != nil {
				if st.err != nil {
					indicator = statusError.String()
					detail = st.err.Error()
				} else {
					indicator = statusRunning.String()
					detail = st.branch
					// Ahead/behind.
					var parts []string
					if st.ahead > 0 {
						parts = append(parts, fmt.Sprintf("\u2191%d", st.ahead))
					}
					if st.behind > 0 {
						parts = append(parts, fmt.Sprintf("\u2193%d", st.behind))
					}
					if st.clean {
						parts = append(parts, "clean")
					} else {
						parts = append(parts, "dirty")
					}
					detail += " " + strings.Join(parts, " ")
				}
			}

			prefix := "  "
			style := navInactive
			if isCursor {
				prefix = "\u25b8 "
				style = selectedStyle
			}
			line := fmt.Sprintf("%s %s", indicator, style.Render(prefix+entry.name))
			if detail != "" {
				line += "\n" + navInactive.Render("    "+detail)
			}
			lines = append(lines, line)

		case gitEntryAction:
			prefix := "    "
			style := commandItemStyle
			if isCursor {
				prefix = "  \u25b8 "
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
func (gv *GitView) RenderOutput(width, height int, focused bool) string {
	gv.output.Width = width
	gv.output.Height = height

	border := unfocusedBorder
	if focused {
		border = focusedBorder
	}

	return border.
		Width(width).
		Height(height).
		Render(gv.output.View())
}

// Overlay returns the overlay string if one is active, or "".
func (gv *GitView) Overlay() string {
	if !gv.showOverlay {
		return ""
	}
	return gv.renderOverlay()
}

// StatusBarHints returns status bar hint strings for the git view.
func (gv *GitView) StatusBarHints() []string {
	var parts []string

	entry := gv.cursorEntry()
	if entry != nil {
		switch entry.kind {
		case gitEntryRepo:
			parts = append(parts, actionKeyStyle.Render("enter")+" "+actionDescStyle.Render("expand"))
			parts = append(parts, actionKeyStyle.Render("S")+" "+actionDescStyle.Render("sync all"))
			parts = append(parts, actionKeyStyle.Render("C")+" "+actionDescStyle.Render("commit subs"))
		case gitEntryAction:
			parts = append(parts, actionKeyStyle.Render("enter")+" "+actionDescStyle.Render("run"))
			parts = append(parts, actionKeyStyle.Render("esc")+" "+actionDescStyle.Render("back"))
		}
	}

	parts = append(parts, actionKeyStyle.Render("?")+" "+actionDescStyle.Render("help"))

	return parts
}

// HelpBindings returns git-view-specific key binding groups.
func (gv *GitView) HelpBindings() [][]key.Binding {
	return [][]key.Binding{
		{gitKeys.SyncAll, gitKeys.CreateBranch, gitKeys.CommitSubs},
		{Keys.Clear, Keys.PageUp, Keys.PageDown, Keys.GoTop, Keys.GoBottom},
	}
}

// ---------------------------------------------------------------------------
// buildEntries — construct the flat sidebar list
// ---------------------------------------------------------------------------

func (gv *GitView) buildEntries() {
	var oldEntry *gitSidebarEntry
	if gv.cursor >= 0 && gv.cursor < len(gv.entries) {
		cp := gv.entries[gv.cursor]
		oldEntry = &cp
	}

	gv.entries = nil

	for _, name := range gv.repoNames {
		gv.entries = append(gv.entries, gitSidebarEntry{
			kind: gitEntryRepo,
			name: name,
			repo: name,
		})

		if gv.expandedRepo == name {
			repo := gv.cfg.Repos[name]
			gv.entries = append(gv.entries,
				gitSidebarEntry{kind: gitEntryAction, name: "Checkout " + repo.DefaultBranch, repo: name, action: "checkout"},
				gitSidebarEntry{kind: gitEntryAction, name: "Pull", repo: name, action: "pull"},
				gitSidebarEntry{kind: gitEntryAction, name: "Fetch", repo: name, action: "fetch"},
				gitSidebarEntry{kind: gitEntryAction, name: "View log", repo: name, action: "log"},
			)
		}
	}

	gv.restoreCursor(oldEntry)
}

func (gv *GitView) restoreCursor(old *gitSidebarEntry) {
	if old == nil || len(gv.entries) == 0 {
		if gv.cursor >= len(gv.entries) {
			gv.cursor = len(gv.entries) - 1
		}
		if gv.cursor < 0 {
			gv.cursor = 0
		}
		return
	}

	for i, e := range gv.entries {
		if e.kind == old.kind && e.name == old.name && e.repo == old.repo {
			gv.cursor = i
			return
		}
	}

	// If old was an action entry, move to the parent repo.
	if old.kind == gitEntryAction {
		for i, e := range gv.entries {
			if e.kind == gitEntryRepo && e.name == old.repo {
				gv.cursor = i
				return
			}
		}
	}

	if gv.cursor >= len(gv.entries) {
		gv.cursor = len(gv.entries) - 1
	}
	if gv.cursor < 0 {
		gv.cursor = 0
	}
}

// ---------------------------------------------------------------------------
// Cursor navigation
// ---------------------------------------------------------------------------

func (gv *GitView) cursorEntry() *gitSidebarEntry {
	if gv.cursor >= 0 && gv.cursor < len(gv.entries) {
		return &gv.entries[gv.cursor]
	}
	return nil
}

func (gv *GitView) moveCursorDown() {
	if gv.cursor < len(gv.entries)-1 {
		gv.cursor++
	}
}

func (gv *GitView) moveCursorUp() {
	if gv.cursor > 0 {
		gv.cursor--
	}
}

// ---------------------------------------------------------------------------
// Key handling
// ---------------------------------------------------------------------------

func (gv *GitView) updateKeypress(msg tea.KeyMsg) (View, tea.Cmd, bool) {
	// Git-view bulk actions.
	switch {
	case key.Matches(msg, gitKeys.SyncAll):
		gv.appendOutput("[starting sync all...]")
		return gv, gv.syncRepo(0, 0, 0, nil), true

	case key.Matches(msg, gitKeys.CreateBranch):
		gv.showOverlay = true
		gv.overlayPhase = "branch_name"
		gv.branchInput = ""
		return gv, nil, true

	case key.Matches(msg, gitKeys.CommitSubs):
		return gv, gv.commitSubmodules(), true
	}

	// Delegate to panel-specific handlers.
	switch *gv.activePanel {
	case PanelSidebar:
		return gv.updateSidebar(msg)
	case PanelOutput:
		return gv.updateOutput(msg)
	}

	return gv, nil, false
}

func (gv *GitView) updateSidebar(msg tea.KeyMsg) (View, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.Down):
		gv.moveCursorDown()
		return gv, nil, true

	case key.Matches(msg, Keys.Up):
		gv.moveCursorUp()
		return gv, nil, true

	case key.Matches(msg, Keys.Enter):
		v, cmd := gv.handleSidebarEnter()
		return v, cmd, true

	case key.Matches(msg, Keys.Cancel):
		if gv.expandedRepo != "" {
			gv.expandedRepo = ""
			gv.buildEntries()
		}
		return gv, nil, true
	}

	return gv, nil, false
}

func (gv *GitView) handleSidebarEnter() (View, tea.Cmd) {
	entry := gv.cursorEntry()
	if entry == nil {
		return gv, nil
	}

	switch entry.kind {
	case gitEntryRepo:
		if gv.expandedRepo == entry.name {
			gv.expandedRepo = ""
		} else {
			gv.expandedRepo = entry.name
		}
		gv.buildEntries()
		return gv, nil

	case gitEntryAction:
		return gv.executeAction(entry.repo, entry.action)
	}

	return gv, nil
}

func (gv *GitView) executeAction(repoName, action string) (View, tea.Cmd) {
	repo, ok := gv.cfg.Repos[repoName]
	if !ok {
		return gv, nil
	}
	dir := repo.Path
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(gv.cfg.Dir, dir)
	}

	switch action {
	case "checkout":
		branch := repo.DefaultBranch
		gv.appendOutput(fmt.Sprintf("[%s: checking out %s...]", repoName, branch))
		return gv, func() tea.Msg {
			if err := git.Checkout(context.Background(), dir, branch); err != nil {
				return GitOutputLineMsg{Line: fmt.Sprintf("[%s: error: %v]", repoName, err)}
			}
			return GitOutputLineMsg{Line: fmt.Sprintf("[%s: checked out %s]", repoName, branch)}
		}

	case "pull":
		gv.appendOutput(fmt.Sprintf("[%s: pulling...]", repoName))
		return gv, func() tea.Msg {
			out, err := git.Pull(context.Background(), dir)
			if err != nil {
				return GitOutputLineMsg{Line: fmt.Sprintf("[%s: error: %v]", repoName, err)}
			}
			return GitOutputLineMsg{Line: fmt.Sprintf("[%s: %s]", repoName, out)}
		}

	case "fetch":
		gv.appendOutput(fmt.Sprintf("[%s: fetching...]", repoName))
		return gv, func() tea.Msg {
			if err := git.Fetch(context.Background(), dir); err != nil {
				return GitOutputLineMsg{Line: fmt.Sprintf("[%s: error: %v]", repoName, err)}
			}
			return GitOutputLineMsg{Line: fmt.Sprintf("[%s: fetch complete]", repoName)}
		}

	case "log":
		gv.appendOutput(fmt.Sprintf("[%s: recent commits]", repoName))
		return gv, func() tea.Msg {
			entries, err := git.Log(context.Background(), dir, 20)
			if err != nil {
				return GitOutputLineMsg{Line: fmt.Sprintf("[%s: error: %v]", repoName, err)}
			}
			return GitOutputLineMsg{Line: strings.Join(entries, "\n")}
		}
	}

	return gv, nil
}

func (gv *GitView) updateOutput(msg tea.KeyMsg) (View, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.Clear):
		gv.outputLines = nil
		gv.output.SetContent("")
		gv.output.GotoTop()
	case key.Matches(msg, Keys.PageUp):
		gv.output.HalfViewUp()
	case key.Matches(msg, Keys.PageDown):
		gv.output.HalfViewDown()
	case key.Matches(msg, Keys.GoTop):
		gv.output.GotoTop()
	case key.Matches(msg, Keys.GoBottom):
		gv.output.GotoBottom()
	case key.Matches(msg, Keys.Up):
		gv.output.LineUp(1)
	case key.Matches(msg, Keys.Down):
		gv.output.LineDown(1)
	}
	return gv, nil, true
}

// ---------------------------------------------------------------------------
// Status polling
// ---------------------------------------------------------------------------

func (gv *GitView) pollAllStatuses() tea.Msg {
	states := make(map[string]*repoState)
	for name, repo := range gv.cfg.Repos {
		dir := repo.Path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(gv.cfg.Dir, dir)
		}
		st, err := git.Status(context.Background(), dir)
		if err != nil {
			states[name] = &repoState{err: err}
			continue
		}
		states[name] = &repoState{
			branch: st.Branch,
			clean:  st.Clean,
			ahead:  st.Ahead,
			behind: st.Behind,
		}
	}
	return GitRepoStatusMsg{States: states}
}

// ---------------------------------------------------------------------------
// Sync All — chained message pattern
// ---------------------------------------------------------------------------

// syncRepo processes one repo in the sync chain and returns a cmd.
func (gv *GitView) syncRepo(idx, synced, skipped int, skipNames []string) tea.Cmd {
	if idx >= len(gv.repoNames) {
		return func() tea.Msg {
			return gitSyncStepMsg{
				line:      "[sync: all repos processed]",
				done:      true,
				synced:    synced,
				skipped:   skipped,
				skipNames: skipNames,
			}
		}
	}

	repoName := gv.repoNames[idx]
	repo := gv.cfg.Repos[repoName]
	dir := repo.Path
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(gv.cfg.Dir, dir)
	}
	defaultBranch := repo.DefaultBranch

	return func() tea.Msg {
		ctx := context.Background()

		// Step 1: Fetch.
		if err := git.Fetch(ctx, dir); err != nil {
			return gitSyncStepMsg{
				line:      fmt.Sprintf("[%s: fetch error: %v — skipping]", repoName, err),
				nextIdx:   idx + 1,
				synced:    synced,
				skipped:   skipped + 1,
				skipNames: append(skipNames, repoName),
			}
		}

		// Step 2: Check if clean.
		clean, err := git.IsClean(ctx, dir)
		if err != nil {
			return gitSyncStepMsg{
				line:      fmt.Sprintf("[%s: status error: %v — skipping]", repoName, err),
				nextIdx:   idx + 1,
				synced:    synced,
				skipped:   skipped + 1,
				skipNames: append(skipNames, repoName),
			}
		}
		if !clean {
			return gitSyncStepMsg{
				line:      fmt.Sprintf("[%s: dirty — skipping]", repoName),
				nextIdx:   idx + 1,
				synced:    synced,
				skipped:   skipped + 1,
				skipNames: append(skipNames, repoName),
			}
		}

		// Step 3: Checkout default branch.
		if err := git.Checkout(ctx, dir, defaultBranch); err != nil {
			return gitSyncStepMsg{
				line:      fmt.Sprintf("[%s: checkout error: %v — skipping]", repoName, err),
				nextIdx:   idx + 1,
				synced:    synced,
				skipped:   skipped + 1,
				skipNames: append(skipNames, repoName),
			}
		}

		// Step 4: Pull.
		out, err := git.Pull(ctx, dir)
		if err != nil {
			return gitSyncStepMsg{
				line:      fmt.Sprintf("[%s: pull error: %v — skipping]", repoName, err),
				nextIdx:   idx + 1,
				synced:    synced,
				skipped:   skipped + 1,
				skipNames: append(skipNames, repoName),
			}
		}

		return gitSyncStepMsg{
			line:      fmt.Sprintf("[%s: synced — %s]", repoName, out),
			nextIdx:   idx + 1,
			synced:    synced + 1,
			skipped:   skipped,
			skipNames: skipNames,
		}
	}
}

// ---------------------------------------------------------------------------
// Commit Submodules
// ---------------------------------------------------------------------------

func (gv *GitView) commitSubmodules() tea.Cmd {
	cfg := gv.cfg

	// Verify a parent repo (path=".") is configured.
	hasParent := false
	for _, repo := range cfg.Repos {
		if repo.Path == "." {
			hasParent = true
			break
		}
	}
	if !hasParent {
		return func() tea.Msg {
			return GitOutputLineMsg{Line: "[commit subs: error — no parent repo configured (need a repo with path: .)]"}
		}
	}

	return func() tea.Msg {
		ctx := context.Background()
		parentDir := cfg.Dir

		// Stage submodule paths only (exclude the parent repo itself).
		var paths []string
		for _, repo := range cfg.Repos {
			if repo.Path == "." {
				continue
			}
			p := repo.Path
			if filepath.IsAbs(p) {
				rel, err := filepath.Rel(parentDir, p)
				if err != nil {
					p = repo.Path
				} else {
					p = rel
				}
			}
			paths = append(paths, p)
		}

		if err := git.Add(ctx, parentDir, paths...); err != nil {
			return GitOutputLineMsg{Line: fmt.Sprintf("[commit subs: add error: %v]", err)}
		}

		// Check if anything is actually staged.
		clean, err := git.IsClean(ctx, parentDir)
		if err != nil {
			return GitOutputLineMsg{Line: fmt.Sprintf("[commit subs: status error: %v]", err)}
		}
		if clean {
			return GitOutputLineMsg{Line: "[commit subs: nothing to commit — submodules already up to date]"}
		}

		if err := git.Commit(ctx, parentDir, "Update submodule pointers"); err != nil {
			return GitOutputLineMsg{Line: fmt.Sprintf("[commit subs: commit error: %v]", err)}
		}

		return GitOutputLineMsg{Line: "[commit subs: committed — Update submodule pointers]"}
	}
}

// ---------------------------------------------------------------------------
// Overlay — branch creation flow
// ---------------------------------------------------------------------------

// updateOverlay handles key presses when the overlay is visible.
func (gv *GitView) updateOverlay(msg tea.KeyMsg) (View, tea.Cmd) {
	switch gv.overlayPhase {
	case "branch_name":
		return gv.updateOverlayBranchName(msg)
	case "branch_select":
		return gv.updateOverlayBranchSelect(msg)
	}
	gv.showOverlay = false
	return gv, nil
}

// updateOverlayBranchName handles typing in the branch name input.
func (gv *GitView) updateOverlayBranchName(msg tea.KeyMsg) (View, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		gv.showOverlay = false
		return gv, nil

	case tea.KeyEnter:
		if gv.branchInput == "" {
			return gv, nil
		}
		// Transition to repo selection phase.
		gv.overlayPhase = "branch_select"
		gv.repoSelections = make(map[string]bool, len(gv.repoNames))
		for _, name := range gv.repoNames {
			gv.repoSelections[name] = true
		}
		gv.overlayCursor = 0
		return gv, nil

	case tea.KeyBackspace:
		if len(gv.branchInput) > 0 {
			runes := []rune(gv.branchInput)
			gv.branchInput = string(runes[:len(runes)-1])
		}
		return gv, nil

	case tea.KeyRunes:
		gv.branchInput += string(msg.Runes)
		return gv, nil
	}

	return gv, nil
}

// updateOverlayBranchSelect handles navigation and selection in the repo multi-select.
func (gv *GitView) updateOverlayBranchSelect(msg tea.KeyMsg) (View, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		gv.showOverlay = false
		return gv, nil

	case tea.KeyEnter:
		// Collect selected repos and start branch creation.
		gv.showOverlay = false
		var selected []string
		for _, name := range gv.repoNames {
			if gv.repoSelections[name] {
				selected = append(selected, name)
			}
		}
		if len(selected) == 0 {
			gv.appendOutput("[no repos selected — cancelled]")
			return gv, nil
		}
		// Store selected repos for the chained creation.
		gv.branchRepos = selected
		gv.appendOutput(fmt.Sprintf("[creating branch %s in %d repo(s)...]", gv.branchInput, len(selected)))
		return gv, gv.createBranchStep(0)

	case tea.KeySpace:
		if gv.overlayCursor >= 0 && gv.overlayCursor < len(gv.repoNames) {
			name := gv.repoNames[gv.overlayCursor]
			gv.repoSelections[name] = !gv.repoSelections[name]
		}
		return gv, nil

	case tea.KeyUp:
		if gv.overlayCursor > 0 {
			gv.overlayCursor--
		}
		return gv, nil

	case tea.KeyDown:
		if gv.overlayCursor < len(gv.repoNames)-1 {
			gv.overlayCursor++
		}
		return gv, nil

	case tea.KeyRunes:
		r := string(msg.Runes)
		switch r {
		case "j":
			if gv.overlayCursor < len(gv.repoNames)-1 {
				gv.overlayCursor++
			}
		case "k":
			if gv.overlayCursor > 0 {
				gv.overlayCursor--
			}
		case " ":
			if gv.overlayCursor >= 0 && gv.overlayCursor < len(gv.repoNames) {
				name := gv.repoNames[gv.overlayCursor]
				gv.repoSelections[name] = !gv.repoSelections[name]
			}
		}
		return gv, nil
	}

	return gv, nil
}

// renderOverlay renders the overlay dialog for the current phase.
func (gv *GitView) renderOverlay() string {
	switch gv.overlayPhase {
	case "branch_name":
		return gv.renderBranchNameOverlay()
	case "branch_select":
		return gv.renderBranchSelectOverlay()
	}
	return ""
}

// renderBranchNameOverlay renders the branch name text input overlay.
func (gv *GitView) renderBranchNameOverlay() string {
	var lines []string
	lines = append(lines, titleStyle.Render("New Branch"))
	lines = append(lines, "")
	lines = append(lines, navInactive.Render("  Name: ")+selectedStyle.Render(gv.branchInput)+selectedStyle.Render("\u2588"))
	lines = append(lines, "")
	lines = append(lines, navInactive.Render("  enter to continue"))
	lines = append(lines, navInactive.Render("  esc to cancel"))

	content := strings.Join(lines, "\n")
	return overlayStyle.Render(content)
}

// renderBranchSelectOverlay renders the repo multi-select overlay.
func (gv *GitView) renderBranchSelectOverlay() string {
	var lines []string
	lines = append(lines, titleStyle.Render("Create: "+gv.branchInput))
	lines = append(lines, "")

	for i, name := range gv.repoNames {
		check := "[ ]"
		if gv.repoSelections[name] {
			check = "[x]"
		}
		style := navInactive
		if i == gv.overlayCursor {
			style = selectedStyle
		}
		lines = append(lines, style.Render(fmt.Sprintf("  %s %s", check, name)))
	}

	lines = append(lines, "")
	lines = append(lines, navInactive.Render("  space toggle "+actionKeyStyle.Render("|")+" enter ok"))
	lines = append(lines, navInactive.Render("  esc cancel"))

	content := strings.Join(lines, "\n")
	return overlayStyle.Render(content)
}

// ---------------------------------------------------------------------------
// Branch creation — chained message pattern
// ---------------------------------------------------------------------------

// createBranchStep processes one repo in the branch creation chain.
func (gv *GitView) createBranchStep(idx int) tea.Cmd {
	if idx >= len(gv.branchRepos) {
		return func() tea.Msg {
			return gitBranchStepMsg{
				line: "[all repos processed]",
				done: true,
			}
		}
	}

	repoName := gv.branchRepos[idx]
	branchName := gv.branchInput
	repo := gv.cfg.Repos[repoName]
	dir := repo.Path
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(gv.cfg.Dir, dir)
	}

	return func() tea.Msg {
		if err := git.CreateBranch(context.Background(), dir, branchName); err != nil {
			return gitBranchStepMsg{
				line:    fmt.Sprintf("[%s: error: %v]", repoName, err),
				nextIdx: idx + 1,
			}
		}
		return gitBranchStepMsg{
			line:    fmt.Sprintf("[%s: created branch %s]", repoName, branchName),
			nextIdx: idx + 1,
		}
	}
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

func (gv *GitView) appendOutput(line string) {
	gv.outputLines = append(gv.outputLines, line)
	if len(gv.outputLines) > 10000 {
		gv.outputLines = gv.outputLines[len(gv.outputLines)-10000:]
	}
	content := strings.Join(gv.outputLines, "\n")
	gv.output.SetContent(content)
	gv.output.GotoBottom()
}
