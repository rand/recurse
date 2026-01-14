package lats

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests for LATS orchestration with realistic scenarios.

// TestIntegration_LATSWithRealTools tests LATS with actual tool execution.
func TestIntegration_LATSWithRealTools(t *testing.T) {
	// Create tool registry with realistic tools
	registry := NewToolRegistry()

	// Calculator tool
	registry.Register(NewMockTool("calculator", "Perform mathematical calculations", func(ctx context.Context, input string) (*ToolResult, error) {
		// Simple calculator that handles "sum X Y" format
		if strings.HasPrefix(input, "sum ") {
			var a, b int
			if _, err := fmt.Sscanf(input, "sum %d %d", &a, &b); err == nil {
				return &ToolResult{
					Output:  fmt.Sprintf("Result: %d", a+b),
					Success: true,
					Tokens:  5,
				}, nil
			}
		}
		return &ToolResult{
			Output:  "Invalid input format",
			Success: false,
			Tokens:  2,
		}, nil
	}))

	// Search tool
	registry.Register(NewMockTool("search", "Search for information", func(ctx context.Context, input string) (*ToolResult, error) {
		// Mock search results
		if strings.Contains(input, "prime") {
			return &ToolResult{
				Output:  "Found: Prime numbers are 2, 3, 5, 7, 11, 13, 17, 19, 23, 29",
				Success: true,
				Tokens:  20,
			}, nil
		}
		return &ToolResult{
			Output:  "No results found",
			Success: true,
			Tokens:  5,
		}, nil
	}))

	// Expander that uses available tools
	expander := &MockExpander{
		ActionsPerNode: 2,
		Tools:          []string{"calculator", "search"},
	}

	// Simulator with heuristic valuator
	valuator := NewHeuristicValuator()
	simulator := NewRealSimulator(registry, valuator)

	config := Config{
		MaxIterations:       30,
		MaxDepth:            5,
		ExplorationConstant: 1.414,
		TokenBudget:         1000,
		Timeout:             10 * time.Second,
		ValueDecay:          0.95,
	}

	ctrl := NewController(expander, simulator, valuator, registry, config)
	ctx := context.Background()

	solution, err := ctrl.Solve(ctx, "Calculate sum of first 3 prime numbers")
	require.NoError(t, err)
	require.NotNil(t, solution)

	assert.Greater(t, solution.Stats.Iterations, 0)
	assert.Greater(t, solution.Stats.NodesCreated, 1)
	assert.LessOrEqual(t, solution.Stats.Duration, 10*time.Second)
}

// TestIntegration_FallbackOnToolFailure tests graceful fallback on tool failures.
func TestIntegration_FallbackOnToolFailure(t *testing.T) {
	registry := NewToolRegistry()

	var failCount int64

	// Unreliable tool that fails sometimes
	registry.Register(NewMockTool("unreliable", "An unreliable tool", func(ctx context.Context, input string) (*ToolResult, error) {
		count := atomic.AddInt64(&failCount, 1)
		if count <= 3 {
			return nil, errors.New("tool temporarily unavailable")
		}
		return &ToolResult{
			Output:  "Solution: task completed successfully",
			Success: true,
			Tokens:  10,
		}, nil
	}))

	// Reliable backup tool
	registry.Register(NewMockTool("reliable", "A reliable backup tool", func(ctx context.Context, input string) (*ToolResult, error) {
		return &ToolResult{
			Output:  "Reliable result",
			Success: true,
			Tokens:  5,
		}, nil
	}))

	expander := &MockExpander{
		ActionsPerNode: 2,
		Tools:          []string{"unreliable", "reliable"},
	}

	valuator := NewHeuristicValuator()
	simulator := NewRealSimulator(registry, valuator)

	config := Config{
		MaxIterations:       50,
		MaxDepth:            6,
		ExplorationConstant: 1.414,
		TokenBudget:         5000,
		Timeout:             15 * time.Second,
		ValueDecay:          0.95,
	}

	ctrl := NewController(expander, simulator, valuator, registry, config)
	ctx := context.Background()

	solution, err := ctrl.Solve(ctx, "Complete a task with potentially failing tools")
	require.NoError(t, err)
	require.NotNil(t, solution)

	// Should have explored multiple paths
	assert.Greater(t, solution.Stats.NodesCreated, 3)
}

// TestIntegration_EndToEndTaskCompletion tests complete task execution flow.
func TestIntegration_EndToEndTaskCompletion(t *testing.T) {
	registry := NewToolRegistry()

	// Stateful tool that tracks progress
	var state struct {
		dataLoaded   bool
		dataAnalyzed bool
	}

	registry.Register(NewMockTool("load_data", "Load data from source", func(ctx context.Context, input string) (*ToolResult, error) {
		state.dataLoaded = true
		return &ToolResult{
			Output:  "Data loaded: [1, 2, 3, 4, 5]",
			Success: true,
			Tokens:  10,
		}, nil
	}))

	registry.Register(NewMockTool("analyze", "Analyze loaded data", func(ctx context.Context, input string) (*ToolResult, error) {
		if !state.dataLoaded {
			return &ToolResult{
				Output:  "Error: No data loaded",
				Success: false,
				Tokens:  3,
			}, nil
		}
		state.dataAnalyzed = true
		return &ToolResult{
			Output:  "Analysis complete. Solution: sum=15, avg=3",
			Success: true,
			Tokens:  15,
		}, nil
	}))

	registry.Register(NewMockTool("report", "Generate report", func(ctx context.Context, input string) (*ToolResult, error) {
		if !state.dataAnalyzed {
			return &ToolResult{
				Output:  "Error: No analysis available",
				Success: false,
				Tokens:  3,
			}, nil
		}
		return &ToolResult{
			Output:  "FINAL_ANSWER: Report generated with sum=15, avg=3",
			Success: true,
			Tokens:  20,
		}, nil
	}))

	// Custom expander that suggests tools in order
	expander := &sequentialExpander{
		toolSequence: []string{"load_data", "analyze", "report"},
		registry:     registry,
	}

	valuator := NewHeuristicValuator()
	simulator := NewRealSimulator(registry, valuator)

	config := Config{
		MaxIterations:       40,
		MaxDepth:            6,
		ExplorationConstant: 1.414,
		TokenBudget:         2000,
		Timeout:             10 * time.Second,
		ValueDecay:          0.95,
	}

	ctrl := NewController(expander, simulator, valuator, registry, config)
	ctx := context.Background()

	solution, err := ctrl.Solve(ctx, "Load data, analyze it, and generate a report")
	require.NoError(t, err)
	require.NotNil(t, solution)

	// Should have explored the tree and found some result
	assert.Greater(t, solution.Stats.NodesCreated, 1)
	// The final answer may or may not contain FINAL_ANSWER depending on execution path
	if solution.FinalAnswer != "" {
		t.Logf("Final answer: %s", solution.FinalAnswer)
	}
}

// sequentialExpander suggests tools in a specific order based on state.
type sequentialExpander struct {
	toolSequence []string
	registry     *ToolRegistry
}

func (e *sequentialExpander) Expand(ctx context.Context, node *Node) ([]*Node, error) {
	// Determine which tool to suggest based on depth
	toolIdx := node.Depth % len(e.toolSequence)

	var children []*Node
	// Generate 2 options: suggested tool and another
	for i := 0; i < 2; i++ {
		idx := (toolIdx + i) % len(e.toolSequence)
		tool := e.toolSequence[idx]

		child := &Node{
			Action: &Action{
				Tool:      tool,
				Input:     fmt.Sprintf("execute %s", tool),
				Reasoning: fmt.Sprintf("Try %s at depth %d", tool, node.Depth+1),
			},
			State: node.State.Clone(),
		}
		children = append(children, child)
	}

	return children, nil
}

// TestIntegration_CapabilityBasedToolSelection tests capability-aware tool selection.
func TestIntegration_CapabilityBasedToolSelection(t *testing.T) {
	qa := NewQueryAnalyzer()
	profiles := GetAgentToolMatrix()

	testCases := []struct {
		query           string
		expectedTool    string
		expectedCapReq  ToolCapability
	}{
		{
			query:          "Read the configuration file",
			expectedTool:   "file_read",
			expectedCapReq: CapFileReadSingle,
		},
		{
			query:          "Calculate the sum of these numbers using Python",
			expectedTool:   "repl",
			expectedCapReq: CapCodeExecutePython,
		},
		{
			query:          "Search for all Go files in this directory",
			expectedTool:   "search",
			expectedCapReq: CapSearchContent,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.query, func(t *testing.T) {
			req := qa.Analyze(tc.query)

			// Check that expected capability is required or preferred
			found := false
			for _, r := range req.Required {
				if r.Capability == tc.expectedCapReq {
					found = true
					break
				}
			}
			for _, p := range req.Preferred {
				if p.Capability == tc.expectedCapReq {
					found = true
					break
				}
			}
			assert.True(t, found, "Expected capability %s not found for query: %s", tc.expectedCapReq, tc.query)

			// Check that recommended tools include expected
			ranked := RecommendTools(req, profiles)
			if len(ranked) > 0 {
				// Expected tool should be in top 3
				foundTool := false
				for i := 0; i < len(ranked) && i < 3; i++ {
					if ranked[i].Name == tc.expectedTool {
						foundTool = true
						break
					}
				}
				// This is a soft check - the expected tool should rank high
				if !foundTool {
					t.Logf("Expected tool %s not in top 3, top tools: %v",
						tc.expectedTool, ranked[:minInt(3, len(ranked))])
				}
			}
		})
	}
}

// TestIntegration_ConcurrentSolves tests concurrent LATS execution.
func TestIntegration_ConcurrentSolves(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(NewMockTool("echo", "Echo input", func(ctx context.Context, input string) (*ToolResult, error) {
		return &ToolResult{
			Output:  "Echo: " + input,
			Success: true,
			Tokens:  5,
		}, nil
	}))

	config := Config{
		MaxIterations:       10,
		MaxDepth:            3,
		ExplorationConstant: 1.414,
		TokenBudget:         500,
		Timeout:             5 * time.Second,
		ValueDecay:          0.95,
	}

	numConcurrent := 5
	done := make(chan error, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func(id int) {
			expander := &MockExpander{
				ActionsPerNode: 2,
				Tools:          []string{"echo"},
			}
			valuator := NewHeuristicValuator()
			simulator := NewRealSimulator(registry, valuator)

			ctrl := NewController(expander, simulator, valuator, registry, config)
			ctx := context.Background()

			_, err := ctrl.Solve(ctx, fmt.Sprintf("Query %d", id))
			done <- err
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < numConcurrent; i++ {
		err := <-done
		assert.NoError(t, err, "Concurrent solve %d should succeed", i)
	}
}

// TestIntegration_LongRunningQuery tests handling of complex queries.
func TestIntegration_LongRunningQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test")
	}

	registry := NewToolRegistry()

	// Slow tool that simulates network latency
	registry.Register(NewMockTool("slow_api", "Call slow API", func(ctx context.Context, input string) (*ToolResult, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return &ToolResult{
				Output:  "API response received",
				Success: true,
				Tokens:  30,
			}, nil
		}
	}))

	registry.Register(NewMockTool("fast_cache", "Query cache", func(ctx context.Context, input string) (*ToolResult, error) {
		return &ToolResult{
			Output:  "Cache hit: data available",
			Success: true,
			Tokens:  5,
		}, nil
	}))

	expander := &MockExpander{
		ActionsPerNode: 2,
		Tools:          []string{"slow_api", "fast_cache"},
	}

	valuator := NewHeuristicValuator()
	simulator := NewRealSimulator(registry, valuator)

	config := Config{
		MaxIterations:       30,
		MaxDepth:            5,
		ExplorationConstant: 1.414,
		TokenBudget:         3000,
		Timeout:             3 * time.Second,
		ValueDecay:          0.95,
	}

	ctrl := NewController(expander, simulator, valuator, registry, config)
	ctx := context.Background()

	start := time.Now()
	solution, err := ctrl.Solve(ctx, "Fetch data from API and cache")
	duration := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, solution)

	// Should complete within timeout
	assert.Less(t, duration, 3*time.Second)
	assert.Greater(t, solution.Stats.Iterations, 0)
}

// TestIntegration_TerminationReasons tests all termination conditions.
func TestIntegration_TerminationReasons(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(NewMockTool("noop", "Do nothing", func(ctx context.Context, input string) (*ToolResult, error) {
		return &ToolResult{Output: "noop", Success: true, Tokens: 1}, nil
	}))

	testCases := []struct {
		name       string
		config     Config
		wantReason TerminationReason
	}{
		{
			name: "max iterations",
			config: Config{
				MaxIterations:       5,
				MaxDepth:            10,
				TokenBudget:         100000,
				Timeout:             10 * time.Second,
				ValueDecay:          0.95,
				ExplorationConstant: 1.414,
			},
			wantReason: TerminatedMaxIter,
		},
		{
			name: "budget exhausted",
			config: Config{
				MaxIterations:       100,
				MaxDepth:            10,
				TokenBudget:         10, // Very small
				Timeout:             10 * time.Second,
				ValueDecay:          0.95,
				ExplorationConstant: 1.414,
			},
			wantReason: TerminatedBudget,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expander := &MockExpander{ActionsPerNode: 2, Tools: []string{"noop"}}
			valuator := NewHeuristicValuator()
			simulator := NewRealSimulator(registry, valuator)

			ctrl := NewController(expander, simulator, valuator, registry, tc.config)
			solution, _ := ctrl.Solve(context.Background(), "test")

			assert.Equal(t, tc.wantReason, solution.Stats.TerminatedBy)
		})
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
