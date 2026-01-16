package editor

import (
	"charm.land/bubbles/v2/key"
	"github.com/rand/recurse/internal/config"
)

type EditorKeyMap struct {
	AddFile     key.Binding
	SendMessage key.Binding
	OpenEditor  key.Binding
	Newline     key.Binding
	PrevHistory key.Binding
	NextHistory key.Binding
}

func DefaultEditorKeyMap() EditorKeyMap {
	return EditorKeyMap{
		AddFile: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "add file"),
		),
		SendMessage: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		OpenEditor: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("ctrl+o", "open editor"),
		),
		Newline: key.NewBinding(
			key.WithKeys("shift+enter", "ctrl+j"),
			// "ctrl+j" is a common keybinding for newline in many editors. If
			// the terminal supports "shift+enter", we substitute the help text
			// to reflect that.
			key.WithHelp("ctrl+j", "newline"),
		),
		PrevHistory: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "prev input"),
		),
		NextHistory: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "next input"),
		),
	}
}

// ApplyConfig applies custom keybindings from configuration.
func (k *EditorKeyMap) ApplyConfig(cfg *config.KeybindingsConfig) {
	if cfg == nil {
		return
	}
	if cfg.AddFile != "" {
		k.AddFile = key.NewBinding(key.WithKeys(cfg.AddFile), key.WithHelp(cfg.AddFile, "add file"))
	}
	if cfg.SendMessage != "" {
		k.SendMessage = key.NewBinding(key.WithKeys(cfg.SendMessage), key.WithHelp(cfg.SendMessage, "send"))
	}
	if cfg.OpenEditor != "" {
		k.OpenEditor = key.NewBinding(key.WithKeys(cfg.OpenEditor), key.WithHelp(cfg.OpenEditor, "open editor"))
	}
	if cfg.Newline != "" {
		k.Newline = key.NewBinding(key.WithKeys(cfg.Newline), key.WithHelp(cfg.Newline, "newline"))
	}
	if cfg.PrevHistory != "" {
		k.PrevHistory = key.NewBinding(key.WithKeys(cfg.PrevHistory), key.WithHelp(cfg.PrevHistory, "prev input"))
	}
	if cfg.NextHistory != "" {
		k.NextHistory = key.NewBinding(key.WithKeys(cfg.NextHistory), key.WithHelp(cfg.NextHistory, "next input"))
	}
}

// KeyBindings implements layout.KeyMapProvider
func (k EditorKeyMap) KeyBindings() []key.Binding {
	return []key.Binding{
		k.AddFile,
		k.SendMessage,
		k.OpenEditor,
		k.Newline,
		k.PrevHistory,
		k.NextHistory,
		AttachmentsKeyMaps.AttachmentDeleteMode,
		AttachmentsKeyMaps.DeleteAllAttachments,
		AttachmentsKeyMaps.Escape,
	}
}

type DeleteAttachmentKeyMaps struct {
	AttachmentDeleteMode key.Binding
	Escape               key.Binding
	DeleteAllAttachments key.Binding
}

// TODO: update this to use the new keymap concepts
var AttachmentsKeyMaps = DeleteAttachmentKeyMaps{
	AttachmentDeleteMode: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r+{i}", "delete attachment at index i"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc", "alt+esc"),
		key.WithHelp("esc", "cancel delete mode"),
	),
	DeleteAllAttachments: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("ctrl+r+r", "delete all attachments"),
	),
}
