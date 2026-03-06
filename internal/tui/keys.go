package tui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Quit       key.Binding
	Help       key.Binding
	Tab        key.Binding
	ShiftTab   key.Binding
	NavService key.Binding
	NavProject key.Binding
	NavCommand key.Binding
	Profile    key.Binding
	Up         key.Binding
	Down       key.Binding
	Enter      key.Binding
	ServiceUp      key.Binding
	ServiceDown    key.Binding
	ServiceRebuild key.Binding
	ServiceLogs    key.Binding
	ServiceShell   key.Binding
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
	Tab:        key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
	ShiftTab:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev panel")),
	NavService: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "services")),
	NavProject: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "projects")),
	NavCommand: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "commands")),
	Profile:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "profiles")),
	Up:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	ServiceUp:      key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "start")),
	ServiceDown:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "stop")),
	ServiceRebuild: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rebuild")),
	ServiceLogs:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
	ServiceShell:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "shell")),
	Clear:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear")),
	PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
	PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
	GoTop:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top")),
	GoBottom: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
	Cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit, k.Tab, k.Profile}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.Help, k.Tab, k.ShiftTab},
		{k.NavService, k.NavProject, k.NavCommand, k.Profile},
		{k.Up, k.Down, k.Enter, k.Cancel},
		{k.ServiceUp, k.ServiceDown, k.ServiceRebuild, k.ServiceLogs, k.ServiceShell},
		{k.Clear, k.PageUp, k.PageDown, k.GoTop, k.GoBottom},
	}
}
