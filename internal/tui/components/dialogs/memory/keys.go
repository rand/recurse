package memory

import (
	"charm.land/bubbles/v2/key"
)

// KeyMap defines key bindings for the memory inspector dialog.
type KeyMap struct {
	Select,
	Next,
	Previous,
	Search,
	Recent,
	Stats,
	Close key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "view details"),
		),
		Next: key.NewBinding(
			key.WithKeys("down", "ctrl+n", "j"),
			key.WithHelp("↓/j", "next"),
		),
		Previous: key.NewBinding(
			key.WithKeys("up", "ctrl+p", "k"),
			key.WithHelp("↑/k", "previous"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Recent: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "recent"),
		),
		Stats: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "stats"),
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
		k.Search,
		k.Recent,
		k.Stats,
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
		k.Search,
		k.Recent,
		k.Close,
	}
}
