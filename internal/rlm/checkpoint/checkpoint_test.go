package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 30*time.Second, cfg.Interval)
	assert.Equal(t, 24*time.Hour, cfg.MaxAge)
}

func TestNewManager(t *testing.T) {
	cfg := DefaultConfig()
	mgr := NewManager(cfg)
	require.NotNil(t, mgr)
	assert.Equal(t, cfg, mgr.config)
}

func TestManager_SaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		Enabled: true,
		Path:    tmpDir,
	}

	mgr := NewManager(cfg)

	// Update with test data
	mgr.SetSessionID("test-session-123")
	mgr.UpdateTaskState(&TaskState{
		TaskID:      "task-1",
		TaskStarted: time.Now(),
		NodeCount:   42,
		FactCount:   10,
		EntityCount: 5,
	})
	mgr.UpdateRLMState(&RLMState{
		CurrentIteration: 3,
		MaxIterations:    10,
		LastTask:         "Count the words",
		REPLActive:       true,
		Mode:             "rlm",
	})
	mgr.UpdateServiceStats(&ServiceStats{
		TotalExecutions: 5,
		TotalTokens:     1000,
		TasksCompleted:  3,
	})

	// Save
	err := mgr.Save()
	require.NoError(t, err)

	// Verify file exists
	path := filepath.Join(tmpDir, "session_checkpoint.json")
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Load in new manager
	mgr2 := NewManager(cfg)
	cp, err := mgr2.Load()
	require.NoError(t, err)
	require.NotNil(t, cp)

	assert.Equal(t, 1, cp.Version)
	assert.Equal(t, "test-session-123", cp.SessionID)

	require.NotNil(t, cp.TaskState)
	assert.Equal(t, "task-1", cp.TaskState.TaskID)
	assert.Equal(t, 42, cp.TaskState.NodeCount)

	require.NotNil(t, cp.RLMState)
	assert.Equal(t, 3, cp.RLMState.CurrentIteration)
	assert.Equal(t, "Count the words", cp.RLMState.LastTask)
	assert.True(t, cp.RLMState.REPLActive)

	require.NotNil(t, cp.ServiceStats)
	assert.Equal(t, 5, cp.ServiceStats.TotalExecutions)
}

func TestManager_Load_NoCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		Enabled: true,
		Path:    tmpDir,
	}

	mgr := NewManager(cfg)
	cp, err := mgr.Load()
	require.NoError(t, err)
	assert.Nil(t, cp)
}

func TestManager_Load_StaleCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		Enabled: true,
		Path:    tmpDir,
		MaxAge:  1 * time.Millisecond, // Very short for testing
	}

	mgr := NewManager(cfg)
	mgr.SetSessionID("old-session")
	err := mgr.Save()
	require.NoError(t, err)

	// Wait for checkpoint to become stale
	time.Sleep(10 * time.Millisecond)

	// Load should return nil (stale)
	cp, err := mgr.Load()
	require.NoError(t, err)
	assert.Nil(t, cp)

	// File should be removed
	path := filepath.Join(tmpDir, "session_checkpoint.json")
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestManager_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		Enabled: true,
		Path:    tmpDir,
	}

	mgr := NewManager(cfg)
	mgr.SetSessionID("test-session")
	err := mgr.Save()
	require.NoError(t, err)

	// Verify exists
	assert.True(t, mgr.HasCheckpoint())

	// Clear
	err = mgr.Clear()
	require.NoError(t, err)

	// Verify gone
	assert.False(t, mgr.HasCheckpoint())
}

func TestManager_HasCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		Enabled: true,
		Path:    tmpDir,
	}

	mgr := NewManager(cfg)

	// Initially no checkpoint
	assert.False(t, mgr.HasCheckpoint())

	// Create one
	mgr.SetSessionID("test")
	err := mgr.Save()
	require.NoError(t, err)

	assert.True(t, mgr.HasCheckpoint())
}

func TestManager_StartStop(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		Enabled:  true,
		Path:     tmpDir,
		Interval: 50 * time.Millisecond,
	}

	mgr := NewManager(cfg)
	mgr.SetSessionID("test-periodic")
	mgr.UpdateTaskState(&TaskState{NodeCount: 1})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr.Start(ctx)

	// Wait for at least one periodic save
	time.Sleep(100 * time.Millisecond)

	err := mgr.Stop()
	require.NoError(t, err)

	// Verify checkpoint was saved
	assert.True(t, mgr.HasCheckpoint())
}

func TestManager_Update(t *testing.T) {
	cfg := DefaultConfig()
	mgr := NewManager(cfg)

	cp := &Checkpoint{
		Version:   1,
		SessionID: "full-update",
		TaskState: &TaskState{NodeCount: 100},
	}

	mgr.Update(cp)

	// Verify internal state
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	assert.Equal(t, "full-update", mgr.current.SessionID)
	assert.Equal(t, 100, mgr.current.TaskState.NodeCount)
}

func TestCheckpoint_Summary(t *testing.T) {
	tests := []struct {
		name     string
		cp       *Checkpoint
		contains []string
	}{
		{
			name:     "nil checkpoint",
			cp:       nil,
			contains: []string{"No checkpoint"},
		},
		{
			name: "with task state",
			cp: &Checkpoint{
				CreatedAt: time.Now().Add(-30 * time.Second),
				TaskState: &TaskState{NodeCount: 42},
			},
			contains: []string{"30s ago", "42 memories"},
		},
		{
			name: "with RLM state",
			cp: &Checkpoint{
				CreatedAt: time.Now().Add(-5 * time.Minute),
				RLMState:  &RLMState{LastTask: "Count words in file"},
			},
			contains: []string{"5m ago", "task:", "Count words"},
		},
		{
			name: "with stats",
			cp: &Checkpoint{
				CreatedAt:    time.Now().Add(-2 * time.Hour),
				ServiceStats: &ServiceStats{TotalExecutions: 10},
			},
			contains: []string{"2h ago", "10 executions"},
		},
		{
			name: "long task truncated",
			cp: &Checkpoint{
				CreatedAt: time.Now(),
				RLMState: &RLMState{
					LastTask: "This is a very long task description that should be truncated in the summary output",
				},
			},
			contains: []string{"..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := tt.cp.Summary()
			for _, s := range tt.contains {
				assert.Contains(t, summary, s)
			}
		})
	}
}

func TestCheckpoint_IsRecoverable(t *testing.T) {
	tests := []struct {
		name        string
		cp          *Checkpoint
		recoverable bool
	}{
		{
			name:        "nil checkpoint",
			cp:          nil,
			recoverable: false,
		},
		{
			name:        "empty checkpoint",
			cp:          &Checkpoint{},
			recoverable: false,
		},
		{
			name: "task state with nodes",
			cp: &Checkpoint{
				TaskState: &TaskState{NodeCount: 5},
			},
			recoverable: true,
		},
		{
			name: "task state without nodes",
			cp: &Checkpoint{
				TaskState: &TaskState{NodeCount: 0},
			},
			recoverable: false,
		},
		{
			name: "RLM state with task",
			cp: &Checkpoint{
				RLMState: &RLMState{LastTask: "something"},
			},
			recoverable: true,
		},
		{
			name: "RLM state without task",
			cp: &Checkpoint{
				RLMState: &RLMState{LastTask: ""},
			},
			recoverable: false,
		},
		{
			name: "only stats",
			cp: &Checkpoint{
				ServiceStats: &ServiceStats{TotalExecutions: 10},
			},
			recoverable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.recoverable, tt.cp.IsRecoverable())
		})
	}
}

func TestManager_Save_NoPath(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Path:    "", // No path
	}

	mgr := NewManager(cfg)
	mgr.SetSessionID("test")

	// Should not error, just do nothing
	err := mgr.Save()
	require.NoError(t, err)
}

func TestManager_Save_NoCurrent(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		Enabled: true,
		Path:    tmpDir,
	}

	mgr := NewManager(cfg)

	// Save with no current checkpoint
	err := mgr.Save()
	require.NoError(t, err)

	// Should not create file
	assert.False(t, mgr.HasCheckpoint())
}

func TestManager_Clear_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		Enabled: true,
		Path:    tmpDir,
	}

	mgr := NewManager(cfg)

	// Clear when no file exists should not error
	err := mgr.Clear()
	require.NoError(t, err)
}

// Property-based tests

// TestProperty_SaveLoadRoundtrip verifies save/load preserves checkpoint data.
func TestProperty_SaveLoadRoundtrip(t *testing.T) {
	// Use a single temp dir for all tests in this property
	baseDir := t.TempDir()

	rapid.Check(t, func(t *rapid.T) {
		// Use alphanumeric for valid directory names
		dirName := rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(t, "dir")
		testDir := filepath.Join(baseDir, dirName)
		os.MkdirAll(testDir, 0o755)
		defer os.RemoveAll(testDir)

		cfg := Config{
			Enabled: true,
			Path:    testDir,
			MaxAge:  24 * time.Hour,
		}

		mgr := NewManager(cfg)

		// Generate random checkpoint data
		sessionID := rapid.String().Draw(t, "session_id")
		nodeCount := rapid.IntRange(0, 10000).Draw(t, "node_count")
		factCount := rapid.IntRange(0, nodeCount).Draw(t, "fact_count")
		entityCount := rapid.IntRange(0, nodeCount-factCount).Draw(t, "entity_count")
		currentIter := rapid.IntRange(0, 100).Draw(t, "current_iter")
		maxIter := rapid.IntRange(currentIter, 100).Draw(t, "max_iter")
		lastTask := rapid.String().Draw(t, "last_task")
		replActive := rapid.Bool().Draw(t, "repl_active")
		mode := rapid.SampledFrom([]string{"rlm", "direct", ""}).Draw(t, "mode")
		totalExec := rapid.IntRange(0, 10000).Draw(t, "total_exec")

		mgr.SetSessionID(sessionID)
		mgr.UpdateTaskState(&TaskState{
			NodeCount:   nodeCount,
			FactCount:   factCount,
			EntityCount: entityCount,
		})
		mgr.UpdateRLMState(&RLMState{
			CurrentIteration: currentIter,
			MaxIterations:    maxIter,
			LastTask:         lastTask,
			REPLActive:       replActive,
			Mode:             mode,
		})
		mgr.UpdateServiceStats(&ServiceStats{
			TotalExecutions: totalExec,
		})

		err := mgr.Save()
		if err != nil {
			t.Fatalf("save failed: %v", err)
		}

		// Load in new manager
		mgr2 := NewManager(cfg)
		cp, err := mgr2.Load()
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}
		if cp == nil {
			t.Fatal("checkpoint should not be nil after save")
		}

		// Verify data preserved
		assert.Equal(t, sessionID, cp.SessionID)
		assert.Equal(t, nodeCount, cp.TaskState.NodeCount)
		assert.Equal(t, factCount, cp.TaskState.FactCount)
		assert.Equal(t, entityCount, cp.TaskState.EntityCount)
		assert.Equal(t, currentIter, cp.RLMState.CurrentIteration)
		assert.Equal(t, maxIter, cp.RLMState.MaxIterations)
		assert.Equal(t, lastTask, cp.RLMState.LastTask)
		assert.Equal(t, replActive, cp.RLMState.REPLActive)
		assert.Equal(t, mode, cp.RLMState.Mode)
		assert.Equal(t, totalExec, cp.ServiceStats.TotalExecutions)
	})
}

// TestProperty_SummaryNeverPanics verifies Summary never panics on any checkpoint.
func TestProperty_SummaryNeverPanics(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random checkpoint (including nil fields)
		var cp *Checkpoint
		if rapid.Bool().Draw(t, "nil_checkpoint") {
			cp = nil
		} else {
			cp = &Checkpoint{
				Version:   rapid.IntRange(0, 10).Draw(t, "version"),
				SessionID: rapid.String().Draw(t, "session_id"),
				CreatedAt: time.Now().Add(-time.Duration(rapid.IntRange(0, 86400).Draw(t, "age_seconds")) * time.Second),
			}

			if rapid.Bool().Draw(t, "has_task_state") {
				cp.TaskState = &TaskState{
					NodeCount:   rapid.IntRange(0, 10000).Draw(t, "node_count"),
					FactCount:   rapid.IntRange(0, 1000).Draw(t, "fact_count"),
					EntityCount: rapid.IntRange(0, 1000).Draw(t, "entity_count"),
				}
			}

			if rapid.Bool().Draw(t, "has_rlm_state") {
				cp.RLMState = &RLMState{
					CurrentIteration: rapid.IntRange(0, 100).Draw(t, "current_iter"),
					MaxIterations:    rapid.IntRange(0, 100).Draw(t, "max_iter"),
					LastTask:         rapid.String().Draw(t, "last_task"),
					REPLActive:       rapid.Bool().Draw(t, "repl_active"),
					Mode:             rapid.String().Draw(t, "mode"),
				}
			}

			if rapid.Bool().Draw(t, "has_stats") {
				cp.ServiceStats = &ServiceStats{
					TotalExecutions: rapid.IntRange(0, 10000).Draw(t, "total_exec"),
					TotalTokens:     rapid.IntRange(0, 1000000).Draw(t, "total_tokens"),
				}
			}
		}

		// Should never panic
		summary := cp.Summary()
		assert.NotEmpty(t, summary, "Summary should never be empty")
	})
}

// TestProperty_IsRecoverableConsistent verifies IsRecoverable is consistent with state.
func TestProperty_IsRecoverableConsistent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cp := &Checkpoint{
			Version:   1,
			SessionID: rapid.String().Draw(t, "session_id"),
		}

		nodeCount := rapid.IntRange(0, 100).Draw(t, "node_count")
		lastTask := rapid.String().Draw(t, "last_task")

		if rapid.Bool().Draw(t, "has_task_state") {
			cp.TaskState = &TaskState{NodeCount: nodeCount}
		}

		if rapid.Bool().Draw(t, "has_rlm_state") {
			cp.RLMState = &RLMState{LastTask: lastTask}
		}

		// Verify consistency
		isRecoverable := cp.IsRecoverable()

		hasNodes := cp.TaskState != nil && cp.TaskState.NodeCount > 0
		hasTask := cp.RLMState != nil && cp.RLMState.LastTask != ""

		expectedRecoverable := hasNodes || hasTask
		assert.Equal(t, expectedRecoverable, isRecoverable,
			"IsRecoverable should match actual state (nodes=%d, task=%q)", nodeCount, lastTask)
	})
}

// TestProperty_UpdateMethodsNeverPanic verifies update methods never panic.
func TestProperty_UpdateMethodsNeverPanic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := DefaultConfig()
		mgr := NewManager(cfg)

		// Should never panic with any input
		mgr.SetSessionID(rapid.String().Draw(t, "session_id"))

		mgr.UpdateTaskState(&TaskState{
			TaskID:      rapid.String().Draw(t, "task_id"),
			NodeCount:   rapid.IntRange(-100, 10000).Draw(t, "node_count"),
			FactCount:   rapid.IntRange(-100, 1000).Draw(t, "fact_count"),
			EntityCount: rapid.IntRange(-100, 1000).Draw(t, "entity_count"),
		})

		mgr.UpdateRLMState(&RLMState{
			CurrentIteration: rapid.IntRange(-100, 1000).Draw(t, "current_iter"),
			MaxIterations:    rapid.IntRange(-100, 1000).Draw(t, "max_iter"),
			LastTask:         rapid.String().Draw(t, "last_task"),
			REPLActive:       rapid.Bool().Draw(t, "repl_active"),
			Mode:             rapid.String().Draw(t, "mode"),
		})

		mgr.UpdateServiceStats(&ServiceStats{
			TotalExecutions: rapid.IntRange(-100, 10000).Draw(t, "total_exec"),
			TotalTokens:     rapid.IntRange(-100, 1000000).Draw(t, "total_tokens"),
			Errors:          rapid.IntRange(-100, 1000).Draw(t, "errors"),
		})
	})
}

// TestProperty_ConcurrentUpdatesSafe verifies concurrent updates are safe.
func TestProperty_ConcurrentUpdatesSafe(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cfg := DefaultConfig()
		mgr := NewManager(cfg)

		numGoroutines := rapid.IntRange(2, 10).Draw(t, "num_goroutines")
		numUpdates := rapid.IntRange(1, 20).Draw(t, "num_updates")

		done := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				for j := 0; j < numUpdates; j++ {
					mgr.SetSessionID("session-" + string(rune(id+'0')))
					mgr.UpdateTaskState(&TaskState{NodeCount: id*100 + j})
					mgr.UpdateRLMState(&RLMState{CurrentIteration: j})
					mgr.UpdateServiceStats(&ServiceStats{TotalExecutions: id*1000 + j})
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < numGoroutines; i++ {
			<-done
		}

		// Should complete without race conditions or panics
	})
}
