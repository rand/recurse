package app

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rand/recurse/internal/rlm/repl"
	"github.com/rand/recurse/internal/tui/components/dialogs/reploutput"
)

// REPLHistoryAdapter tracks REPL execution history and provides it to the TUI.
type REPLHistoryAdapter struct {
	mu      sync.RWMutex
	manager *repl.Manager
	history []reploutput.ExecutionRecord
	maxSize int
	stats   reploutput.REPLStats
}

// NewREPLHistoryAdapter creates a new REPL history adapter.
func NewREPLHistoryAdapter(manager *repl.Manager, maxSize int) *REPLHistoryAdapter {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &REPLHistoryAdapter{
		manager: manager,
		history: make([]reploutput.ExecutionRecord, 0, maxSize),
		maxSize: maxSize,
	}
}

// Execute wraps the REPL manager's Execute and records the result.
func (a *REPLHistoryAdapter) Execute(ctx context.Context, code string) (*repl.ExecuteResult, error) {
	start := time.Now()
	result, err := a.manager.Execute(ctx, code)
	duration := time.Since(start)

	// Record the execution
	record := reploutput.ExecutionRecord{
		ID:        uuid.New().String(),
		Code:      code,
		Timestamp: start,
		Duration:  duration,
	}

	if result != nil {
		record.Output = result.Output
		record.ReturnVal = result.ReturnVal
		record.Error = result.Error
		// Use the duration from the result if available
		if result.Duration > 0 {
			record.Duration = time.Duration(result.Duration) * time.Millisecond
		}
	}
	if err != nil && record.Error == "" {
		record.Error = err.Error()
	}

	a.addRecord(record)

	return result, err
}

// RecordExecution manually records an execution (for external use).
func (a *REPLHistoryAdapter) RecordExecution(code string, result *repl.ExecuteResult, duration time.Duration, err error) {
	record := reploutput.ExecutionRecord{
		ID:        uuid.New().String(),
		Code:      code,
		Timestamp: time.Now(),
		Duration:  duration,
	}

	if result != nil {
		record.Output = result.Output
		record.ReturnVal = result.ReturnVal
		record.Error = result.Error
		if result.Duration > 0 {
			record.Duration = time.Duration(result.Duration) * time.Millisecond
		}
	}
	if err != nil && record.Error == "" {
		record.Error = err.Error()
	}

	a.addRecord(record)
}

func (a *REPLHistoryAdapter) addRecord(record reploutput.ExecutionRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Add to history (newest first)
	a.history = append([]reploutput.ExecutionRecord{record}, a.history...)

	// Trim if over max size
	if len(a.history) > a.maxSize {
		a.history = a.history[:a.maxSize]
	}

	// Update stats
	a.stats.TotalExecutions++
	a.stats.TotalDuration += record.Duration
	if record.Error != "" {
		a.stats.ErrorCount++
	} else {
		a.stats.SuccessCount++
	}
	a.stats.TotalMemoryUsed += record.MemoryUsed
}

// GetHistory implements reploutput.REPLHistoryProvider.
func (a *REPLHistoryAdapter) GetHistory(limit int) ([]reploutput.ExecutionRecord, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if limit <= 0 || limit > len(a.history) {
		limit = len(a.history)
	}

	// Return a copy to prevent modification
	result := make([]reploutput.ExecutionRecord, limit)
	copy(result, a.history[:limit])
	return result, nil
}

// GetRecord implements reploutput.REPLHistoryProvider.
func (a *REPLHistoryAdapter) GetRecord(id string) (*reploutput.ExecutionRecord, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, record := range a.history {
		if record.ID == id {
			return &record, nil
		}
	}
	return nil, nil
}

// ClearHistory implements reploutput.REPLHistoryProvider.
func (a *REPLHistoryAdapter) ClearHistory() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.history = make([]reploutput.ExecutionRecord, 0, a.maxSize)
	a.stats = reploutput.REPLStats{}
	return nil
}

// Stats implements reploutput.REPLHistoryProvider.
func (a *REPLHistoryAdapter) Stats() reploutput.REPLStats {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.stats
}

// Manager returns the underlying REPL manager.
func (a *REPLHistoryAdapter) Manager() *repl.Manager {
	return a.manager
}
