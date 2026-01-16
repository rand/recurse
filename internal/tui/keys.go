package tui

import (
	"charm.land/bubbles/v2/key"
	"github.com/rand/recurse/internal/config"
)

type KeyMap struct {
	Quit       key.Binding
	Help       key.Binding
	Commands   key.Binding
	Suspend    key.Binding
	Models     key.Binding
	Sessions   key.Binding
	RLMTrace   key.Binding
	Memory     key.Binding
	REPLOutput key.Binding
	PanelView  key.Binding

	pageBindings []key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("ctrl+g", "more"),
		),
		Commands: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "commands"),
		),
		Suspend: key.NewBinding(
			key.WithKeys("ctrl+z"),
			key.WithHelp("ctrl+z", "suspend"),
		),
		Models: key.NewBinding(
			key.WithKeys("ctrl+l", "ctrl+m"),
			key.WithHelp("ctrl+l", "models"),
		),
		Sessions: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "sessions"),
		),
		RLMTrace: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("ctrl+t", "trace"),
		),
		Memory: key.NewBinding(
			key.WithKeys("ctrl+b"),
			key.WithHelp("ctrl+b", "memory"),
		),
		REPLOutput: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "repl"),
		),
		PanelView: key.NewBinding(
			key.WithKeys("ctrl+e"),
			key.WithHelp("ctrl+e", "panels"),
		),
	}
}

// ApplyConfig applies custom keybindings from configuration.
func (km *KeyMap) ApplyConfig(cfg *config.KeybindingsConfig) {
	if cfg == nil {
		return
	}
	if cfg.Quit != "" {
		km.Quit = key.NewBinding(key.WithKeys(cfg.Quit), key.WithHelp(cfg.Quit, "quit"))
	}
	if cfg.Help != "" {
		km.Help = key.NewBinding(key.WithKeys(cfg.Help), key.WithHelp(cfg.Help, "more"))
	}
	if cfg.Commands != "" {
		km.Commands = key.NewBinding(key.WithKeys(cfg.Commands), key.WithHelp(cfg.Commands, "commands"))
	}
	if cfg.Suspend != "" {
		km.Suspend = key.NewBinding(key.WithKeys(cfg.Suspend), key.WithHelp(cfg.Suspend, "suspend"))
	}
	if cfg.Models != "" {
		km.Models = key.NewBinding(key.WithKeys(cfg.Models), key.WithHelp(cfg.Models, "models"))
	}
	if cfg.Sessions != "" {
		km.Sessions = key.NewBinding(key.WithKeys(cfg.Sessions), key.WithHelp(cfg.Sessions, "sessions"))
	}
	if cfg.RLMTrace != "" {
		km.RLMTrace = key.NewBinding(key.WithKeys(cfg.RLMTrace), key.WithHelp(cfg.RLMTrace, "trace"))
	}
	if cfg.Memory != "" {
		km.Memory = key.NewBinding(key.WithKeys(cfg.Memory), key.WithHelp(cfg.Memory, "memory"))
	}
	if cfg.REPLOutput != "" {
		km.REPLOutput = key.NewBinding(key.WithKeys(cfg.REPLOutput), key.WithHelp(cfg.REPLOutput, "repl"))
	}
	if cfg.PanelView != "" {
		km.PanelView = key.NewBinding(key.WithKeys(cfg.PanelView), key.WithHelp(cfg.PanelView, "panels"))
	}
}
