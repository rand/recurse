package orchestrator

import (
	"context"
	"time"

	"github.com/rand/recurse/internal/rlm/checkpoint"
)

// CheckpointConfig configures checkpoint behavior.
type CheckpointConfig struct {
	// Enabled determines if checkpointing is active.
	Enabled bool

	// Interval is how often to save checkpoints.
	Interval time.Duration

	// Path is the directory to store checkpoints.
	Path string

	// MaxAge is the maximum age of a checkpoint before it's considered stale.
	MaxAge time.Duration
}

// DefaultCheckpointConfig returns sensible defaults for checkpointing.
func DefaultCheckpointConfig() CheckpointConfig {
	return CheckpointConfig{
		Enabled:  true,
		Interval: 30 * time.Second,
		MaxAge:   24 * time.Hour,
	}
}

// CheckpointManager wraps the checkpoint.Manager with orchestrator-specific methods.
type CheckpointManager struct {
	manager *checkpoint.Manager
}

// NewCheckpointManager creates a new checkpoint manager.
func NewCheckpointManager(cfg CheckpointConfig) *CheckpointManager {
	return &CheckpointManager{
		manager: checkpoint.NewManager(checkpoint.Config{
			Enabled:  cfg.Enabled,
			Interval: cfg.Interval,
			Path:     cfg.Path,
			MaxAge:   cfg.MaxAge,
		}),
	}
}

// Start begins periodic checkpointing.
func (cm *CheckpointManager) Start(ctx context.Context) {
	cm.manager.Start(ctx)
}

// Stop stops periodic checkpointing.
func (cm *CheckpointManager) Stop() error {
	return cm.manager.Stop()
}

// Save writes the current checkpoint to disk.
func (cm *CheckpointManager) Save() error {
	return cm.manager.Save()
}

// Load reads the most recent checkpoint from disk.
func (cm *CheckpointManager) Load() (*checkpoint.Checkpoint, error) {
	return cm.manager.Load()
}

// Clear removes the checkpoint file.
func (cm *CheckpointManager) Clear() error {
	return cm.manager.Clear()
}

// HasCheckpoint returns true if a valid checkpoint exists.
func (cm *CheckpointManager) HasCheckpoint() bool {
	return cm.manager.HasCheckpoint()
}

// SetSessionID sets the session ID for checkpoints.
func (cm *CheckpointManager) SetSessionID(sessionID string) {
	cm.manager.SetSessionID(sessionID)
}

// UpdateRLMState updates the RLM state portion of the checkpoint.
func (cm *CheckpointManager) UpdateRLMState(iteration, maxIterations int, task string, mode string, replActive bool) {
	cm.manager.UpdateRLMState(&checkpoint.RLMState{
		CurrentIteration: iteration,
		MaxIterations:    maxIterations,
		LastTask:         task,
		REPLActive:       replActive,
		Mode:             mode,
	})
}

// UpdateTaskState updates the task state portion of the checkpoint.
func (cm *CheckpointManager) UpdateTaskState(taskID string, taskStarted time.Time, nodeCount, factCount, entityCount int) {
	cm.manager.UpdateTaskState(&checkpoint.TaskState{
		TaskID:      taskID,
		TaskStarted: taskStarted,
		NodeCount:   nodeCount,
		FactCount:   factCount,
		EntityCount: entityCount,
	})
}

// UpdateServiceStats updates the service statistics portion.
func (cm *CheckpointManager) UpdateServiceStats(totalExecs, totalTokens, tasksCompleted, sessionsEnded, errors int, totalDuration time.Duration) {
	cm.manager.UpdateServiceStats(&checkpoint.ServiceStats{
		TotalExecutions: totalExecs,
		TotalTokens:     totalTokens,
		TotalDuration:   totalDuration,
		TasksCompleted:  tasksCompleted,
		SessionsEnded:   sessionsEnded,
		Errors:          errors,
	})
}

// Inner returns the underlying checkpoint.Manager for direct access.
func (cm *CheckpointManager) Inner() *checkpoint.Manager {
	return cm.manager
}

// Re-export checkpoint types for convenience.

// Checkpoint re-exports checkpoint.Checkpoint.
type Checkpoint = checkpoint.Checkpoint

// RLMState re-exports checkpoint.RLMState.
type RLMState = checkpoint.RLMState

// TaskState re-exports checkpoint.TaskState.
type TaskState = checkpoint.TaskState

// ServiceStats re-exports checkpoint.ServiceStats.
type ServiceStats = checkpoint.ServiceStats
