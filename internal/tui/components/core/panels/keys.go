package panels

import (
	"charm.land/bubbles/v2/key"
)

// KeyMap defines key bindings for panel navigation.
type KeyMap struct {
	NextPanel     key.Binding
	PrevPanel     key.Binding
	Panel1        key.Binding
	Panel2        key.Binding
	Panel3        key.Binding
	Panel4        key.Binding
	TogglePanel   key.Binding
	ClosePanel    key.Binding
}

// DefaultKeyMap returns the default key bindings for panel navigation.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		NextPanel: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next panel"),
		),
		PrevPanel: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev panel"),
		),
		Panel1: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "panel 1"),
		),
		Panel2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "panel 2"),
		),
		Panel3: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "panel 3"),
		),
		Panel4: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "panel 4"),
		),
		TogglePanel: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "toggle panel"),
		),
		ClosePanel: key.NewBinding(
			key.WithKeys("esc", "q"),
			key.WithHelp("esc", "close"),
		),
	}
}

// KeyBindings returns all key bindings for iteration.
func (k KeyMap) KeyBindings() []key.Binding {
	return []key.Binding{
		k.NextPanel,
		k.PrevPanel,
		k.Panel1,
		k.Panel2,
		k.Panel3,
		k.Panel4,
		k.ClosePanel,
	}
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab/1-4", "switch panel"),
		),
		k.ClosePanel,
	}
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.NextPanel, k.PrevPanel},
		{k.Panel1, k.Panel2, k.Panel3, k.Panel4},
		{k.ClosePanel},
	}
}
