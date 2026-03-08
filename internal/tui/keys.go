package tui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Quit       key.Binding
	Help       key.Binding
	Tab        key.Binding
	ViewToggle key.Binding
	Profile    key.Binding

	Up    key.Binding
	Down  key.Binding
	Enter key.Binding

	// Service actions (lowercase)
	ServiceUp      key.Binding
	ServiceDown    key.Binding
	ServiceRebuild key.Binding
	ServiceLogs    key.Binding
	ServiceShell   key.Binding

	// Bulk profile actions (uppercase)
	BulkUp      key.Binding
	BulkDown    key.Binding
	BulkRebuild key.Binding

	// Output panel
	Clear    key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	GoTop    key.Binding
	GoBottom key.Binding

	Cancel key.Binding
}

var Keys = KeyMap{
	Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Tab:        key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch panel")),
	ViewToggle: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "switch view")),
	Profile:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "profiles")),

	Up:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),

	ServiceUp:      key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "start svc")),
	ServiceDown:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "stop svc")),
	ServiceRebuild: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rebuild svc")),
	ServiceLogs:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
	ServiceShell:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "shell")),

	BulkUp:      key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "up all")),
	BulkDown:    key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "down all")),
	BulkRebuild: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rebuild all")),

	Clear:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear")),
	PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
	PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
	GoTop:    key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "top")),
	GoBottom: key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "bottom")),
	Cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit, k.Tab, k.ViewToggle, k.Profile}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.Help, k.Tab, k.ViewToggle, k.Profile},
		{k.Up, k.Down, k.Enter, k.Cancel},
		{k.ServiceUp, k.ServiceDown, k.ServiceRebuild, k.ServiceLogs, k.ServiceShell},
		{k.BulkUp, k.BulkDown, k.BulkRebuild},
		{k.Clear, k.PageUp, k.PageDown, k.GoTop, k.GoBottom},
	}
}
