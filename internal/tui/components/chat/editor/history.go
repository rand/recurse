package editor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// PromptHistoryStore defines the interface for persistent prompt history storage.
// This allows integration with the hypergraph memory system.
type PromptHistoryStore interface {
	// AddPrompt stores a prompt in persistent storage.
	AddPrompt(ctx context.Context, prompt string) error
	// ListPrompts returns the most recent prompts, newest last.
	ListPrompts(ctx context.Context, limit int) ([]string, error)
}

// MemoryPromptStore implements PromptHistoryStore using the hypergraph memory.
type MemoryPromptStore struct {
	store *hypergraph.Store
}

// NewMemoryPromptStore creates a prompt store backed by hypergraph memory.
func NewMemoryPromptStore(store *hypergraph.Store) *MemoryPromptStore {
	return &MemoryPromptStore{store: store}
}

// AddPrompt stores a prompt as an experience node with subtype "prompt".
func (m *MemoryPromptStore) AddPrompt(ctx context.Context, prompt string) error {
	node := hypergraph.NewNode(hypergraph.NodeTypeExperience, prompt)
	node.Subtype = "prompt"
	node.Tier = hypergraph.TierSession // Keep in session tier for faster access
	return m.store.CreateNode(ctx, node)
}

// ListPrompts returns the most recent prompts from memory.
// Returns prompts oldest-first so the most recent is at the end.
func (m *MemoryPromptStore) ListPrompts(ctx context.Context, limit int) ([]string, error) {
	filter := hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeExperience},
		Subtypes: []string{"prompt"},
		Limit:    limit,
	}
	nodes, err := m.store.ListNodes(ctx, filter)
	if err != nil {
		return nil, err
	}
	// ListNodes returns newest first (DESC), reverse to get oldest first
	prompts := make([]string, len(nodes))
	for i, node := range nodes {
		prompts[len(nodes)-1-i] = node.Content
	}
	return prompts, nil
}

// InputHistory manages the history of user inputs with persistent storage.
type InputHistory struct {
	entries   []string
	index     int    // Current position in history (-1 means not navigating)
	draft     string // Current input before starting navigation
	maxItems  int
	filePath  string
	mu        sync.RWMutex
	persisted bool
	store     PromptHistoryStore // Optional memory-backed storage
}

// NewInputHistory creates a new input history manager.
// If filePath is empty, history is not persisted to file.
func NewInputHistory(filePath string, maxItems int, persistent bool) *InputHistory {
	h := &InputHistory{
		entries:   make([]string, 0),
		index:     -1,
		maxItems:  maxItems,
		filePath:  filePath,
		persisted: persistent,
	}
	if persistent && filePath != "" {
		h.load()
	}
	return h
}

// NewInputHistoryWithStore creates a history manager backed by a memory store.
func NewInputHistoryWithStore(store PromptHistoryStore, maxItems int) *InputHistory {
	h := &InputHistory{
		entries:   make([]string, 0),
		index:     -1,
		maxItems:  maxItems,
		persisted: true,
		store:     store,
	}
	// Load from store
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if prompts, err := store.ListPrompts(ctx, maxItems); err == nil {
		h.entries = prompts
	}
	return h
}

// Add adds a new entry to the history.
func (h *InputHistory) Add(entry string) {
	if entry == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	// Don't add duplicate of most recent entry
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == entry {
		return
	}

	h.entries = append(h.entries, entry)

	// Trim to max size
	if len(h.entries) > h.maxItems {
		h.entries = h.entries[len(h.entries)-h.maxItems:]
	}

	// Reset navigation state
	h.index = -1
	h.draft = ""

	// Persist to store (memory-backed) or file
	if h.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.store.AddPrompt(ctx, entry) // Best effort, don't block on errors
	} else if h.persisted {
		h.save()
	}
}

// StartNavigation begins history navigation, saving the current draft.
func (h *InputHistory) StartNavigation(currentInput string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.index == -1 {
		h.draft = currentInput
		h.index = len(h.entries)
	}
}

// Previous returns the previous history entry.
// Returns the entry and true if there is a previous entry, or empty string and false otherwise.
func (h *InputHistory) Previous() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.entries) == 0 {
		return "", false
	}

	if h.index > 0 {
		h.index--
		return h.entries[h.index], true
	}
	return "", false
}

// Next returns the next history entry or the original draft if at the end.
// Returns the entry and true if successful.
func (h *InputHistory) Next() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.index == -1 {
		return "", false
	}

	if h.index < len(h.entries)-1 {
		h.index++
		return h.entries[h.index], true
	}

	// At the end, return to draft
	if h.index == len(h.entries)-1 {
		h.index = len(h.entries)
		return h.draft, true
	}

	return "", false
}

// IsNavigating returns true if currently navigating history.
func (h *InputHistory) IsNavigating() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.index != -1 && h.index < len(h.entries)
}

// Reset stops navigation and clears navigation state.
func (h *InputHistory) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.index = -1
	h.draft = ""
}

// Len returns the number of history entries.
func (h *InputHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.entries)
}

// load reads history from the file.
func (h *InputHistory) load() {
	if h.filePath == "" {
		return
	}

	data, err := os.ReadFile(h.filePath)
	if err != nil {
		return // File doesn't exist or can't be read
	}

	var entries []string
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}

	h.entries = entries
	if len(h.entries) > h.maxItems {
		h.entries = h.entries[len(h.entries)-h.maxItems:]
	}
}

// save writes history to the file.
func (h *InputHistory) save() {
	if h.filePath == "" {
		return
	}

	// Ensure directory exists
	dir := filepath.Dir(h.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	data, err := json.Marshal(h.entries)
	if err != nil {
		return
	}

	_ = os.WriteFile(h.filePath, data, 0o600)
}
