package reploutput

import (
	"charm.land/bubbles/v2/key"
)

// KeyMap defines key bindings for the REPL output dialog.
type KeyMap struct {
	Select,
	Next,
	Previous,
	Details,
	Clear,
	Copy,
	Close key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "view output"),
		),
		Next: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "next"),
		),
		Previous: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "previous"),
		),
		Details: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "toggle details"),
		),
		Clear: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "clear history"),
		),
		Copy: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy output"),
		),
		Close: key.NewBinding(
			key.WithKeys("esc", "q"),
			key.WithHelp("esc/q", "close"),
		),
	}
}

// KeyBindings returns all key bindings for iteration.
func (k KeyMap) KeyBindings() []key.Binding {
	return []key.Binding{
		k.Select,
		k.Next,
		k.Previous,
		k.Details,
		k.Clear,
		k.Copy,
		k.Close,
	}
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	m := [][]key.Binding{}
	slice := k.KeyBindings()
	for i := 0; i < len(slice); i += 4 {
		end := min(i+4, len(slice))
		m = append(m, slice[i:end])
	}
	return m
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("down", "up"),
			key.WithHelp("↑↓", "navigate"),
		),
		k.Details,
		k.Clear,
		k.Close,
	}
}
