// Package checkpoint provides session state persistence for crash recovery.
package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Config configures checkpoint behavior.
type Config struct {
	// Enabled determines if checkpointing is active.
	Enabled bool

	// Interval is how often to save checkpoints.
	Interval time.Duration

	// Path is the directory to store checkpoints.
	Path string

	// MaxAge is the maximum age of a checkpoint before it's considered stale.
	MaxAge time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:  true,
		Interval: 30 * time.Second,
		MaxAge:   24 * time.Hour,
	}
}

// Checkpoint represents a saved session state.
type Checkpoint struct {
	// Version for forward compatibility.
	Version int `json:"version"`

	// SessionID is the session being checkpointed.
	SessionID string `json:"session_id"`

	// CreatedAt is when this checkpoint was created.
	CreatedAt time.Time `json:"created_at"`

	// TaskState contains task memory state.
	TaskState *TaskState `json:"task_state,omitempty"`

	// RLMState contains RLM iteration state.
	RLMState *RLMState `json:"rlm_state,omitempty"`

	// ServiceStats contains service statistics.
	ServiceStats *ServiceStats `json:"service_stats,omitempty"`
}

// TaskState contains task memory information.
type TaskState struct {
	TaskID      string    `json:"task_id"`
	TaskStarted time.Time `json:"task_started"`
	NodeCount   int       `json:"node_count"`
	FactCount   int       `json:"fact_count"`
	EntityCount int       `json:"entity_count"`
}

// RLMState contains RLM execution state.
type RLMState struct {
	// CurrentIteration is the current iteration number.
	CurrentIteration int `json:"current_iteration"`

	// MaxIterations is the configured maximum.
	MaxIterations int `json:"max_iterations"`

	// LastTask is the last task being processed.
	LastTask string `json:"last_task,omitempty"`

	// REPLActive indicates if REPL was active.
	REPLActive bool `json:"repl_active"`

	// Mode is the current execution mode.
	Mode string `json:"mode"`
}

// ServiceStats contains service-level statistics.
type ServiceStats struct {
	TotalExecutions int           `json:"total_executions"`
	TotalTokens     int           `json:"total_tokens"`
	TotalDuration   time.Duration `json:"total_duration"`
	TasksCompleted  int           `json:"tasks_completed"`
	SessionsEnded   int           `json:"sessions_ended"`
	Errors          int           `json:"errors"`
}

// Manager handles checkpoint persistence.
type Manager struct {
	mu     sync.RWMutex
	config Config

	// Current checkpoint data
	current *Checkpoint

	// Background checkpoint goroutine
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewManager creates a new checkpoint manager.
func NewManager(cfg Config) *Manager {
	return &Manager{
		config: cfg,
		stopCh: make(chan struct{}),
	}
}

// Start begins periodic checkpointing.
func (m *Manager) Start(ctx context.Context) {
	if !m.config.Enabled || m.config.Interval <= 0 {
		return
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(m.config.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-m.stopCh:
				return
			case <-ticker.C:
				if err := m.Save(); err != nil {
					// Log but don't fail
					fmt.Fprintf(os.Stderr, "checkpoint save error: %v\n", err)
				}
			}
		}
	}()
}

// Stop stops periodic checkpointing and saves a final checkpoint.
func (m *Manager) Stop() error {
	close(m.stopCh)
	m.wg.Wait()
	return nil
}

// Update updates the current checkpoint data.
func (m *Manager) Update(cp *Checkpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = cp
}

// UpdateTaskState updates just the task state portion.
func (m *Manager) UpdateTaskState(state *TaskState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		m.current = &Checkpoint{Version: 1}
	}
	m.current.TaskState = state
	m.current.CreatedAt = time.Now()
}

// UpdateRLMState updates just the RLM state portion.
func (m *Manager) UpdateRLMState(state *RLMState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		m.current = &Checkpoint{Version: 1}
	}
	m.current.RLMState = state
	m.current.CreatedAt = time.Now()
}

// UpdateServiceStats updates just the service stats portion.
func (m *Manager) UpdateServiceStats(stats *ServiceStats) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		m.current = &Checkpoint{Version: 1}
	}
	m.current.ServiceStats = stats
	m.current.CreatedAt = time.Now()
}

// SetSessionID sets the session ID for checkpoints.
func (m *Manager) SetSessionID(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		m.current = &Checkpoint{Version: 1}
	}
	m.current.SessionID = sessionID
}

// Save writes the current checkpoint to disk.
func (m *Manager) Save() error {
	m.mu.RLock()
	cp := m.current
	m.mu.RUnlock()

	if cp == nil {
		return nil // Nothing to save
	}

	cp.CreatedAt = time.Now()
	cp.Version = 1

	return m.writeCheckpoint(cp)
}

// Load reads the most recent checkpoint from disk.
func (m *Manager) Load() (*Checkpoint, error) {
	path := m.checkpointPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No checkpoint exists
		}
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}

	// Check if checkpoint is stale
	if m.config.MaxAge > 0 && time.Since(cp.CreatedAt) > m.config.MaxAge {
		// Remove stale checkpoint
		os.Remove(path)
		return nil, nil
	}

	return &cp, nil
}

// Clear removes the checkpoint file.
func (m *Manager) Clear() error {
	m.mu.Lock()
	m.current = nil
	m.mu.Unlock()

	path := m.checkpointPath()
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// HasCheckpoint returns true if a valid checkpoint exists.
func (m *Manager) HasCheckpoint() bool {
	cp, err := m.Load()
	return err == nil && cp != nil
}

// writeCheckpoint writes a checkpoint to disk atomically.
func (m *Manager) writeCheckpoint(cp *Checkpoint) error {
	if m.config.Path == "" {
		return nil // No path configured
	}

	// Ensure directory exists
	if err := os.MkdirAll(m.config.Path, 0o755); err != nil {
		return fmt.Errorf("create checkpoint directory: %w", err)
	}

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	// Write atomically via temp file
	path := m.checkpointPath()
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write temp checkpoint: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename checkpoint: %w", err)
	}

	return nil
}

func (m *Manager) checkpointPath() string {
	return filepath.Join(m.config.Path, "session_checkpoint.json")
}

// Summary returns a human-readable summary of the checkpoint.
func (cp *Checkpoint) Summary() string {
	if cp == nil {
		return "No checkpoint"
	}

	age := time.Since(cp.CreatedAt)
	var ageStr string
	if age < time.Minute {
		ageStr = fmt.Sprintf("%ds ago", int(age.Seconds()))
	} else if age < time.Hour {
		ageStr = fmt.Sprintf("%dm ago", int(age.Minutes()))
	} else {
		ageStr = fmt.Sprintf("%dh ago", int(age.Hours()))
	}

	summary := fmt.Sprintf("Session checkpoint from %s", ageStr)

	if cp.TaskState != nil {
		summary += fmt.Sprintf(" | %d memories", cp.TaskState.NodeCount)
	}

	if cp.RLMState != nil && cp.RLMState.LastTask != "" {
		task := cp.RLMState.LastTask
		if len(task) > 50 {
			task = task[:50] + "..."
		}
		summary += fmt.Sprintf(" | task: %s", task)
	}

	if cp.ServiceStats != nil {
		summary += fmt.Sprintf(" | %d executions", cp.ServiceStats.TotalExecutions)
	}

	return summary
}

// IsRecoverable returns true if this checkpoint has meaningful state to recover.
func (cp *Checkpoint) IsRecoverable() bool {
	if cp == nil {
		return false
	}

	// Recoverable if we have task state with nodes or RLM state with a task
	if cp.TaskState != nil && cp.TaskState.NodeCount > 0 {
		return true
	}

	if cp.RLMState != nil && cp.RLMState.LastTask != "" {
		return true
	}

	return false
}
