package lats

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNode_QValue tests Q-value calculation.
func TestNode_QValue(t *testing.T) {
	tests := []struct {
		name       string
		visits     int
		totalValue float64
		expected   float64
	}{
		{"zero visits", 0, 0, 0},
		{"one visit", 1, 0.5, 0.5},
		{"multiple visits", 10, 7.0, 0.7},
		{"high value", 5, 4.5, 0.9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{Visits: tt.visits, TotalValue: tt.totalValue}
			assert.InDelta(t, tt.expected, node.QValue(), 0.001)
		})
	}
}

// TestNode_UCTValue tests UCT calculation.
func TestNode_UCTValue(t *testing.T) {
	tests := []struct {
		name         string
		visits       int
		totalValue   float64
		parentVisits int
		exploration  float64
		wantInfinite bool
	}{
		{
			name:         "unexplored node",
			visits:       0,
			parentVisits: 10,
			exploration:  1.414,
			wantInfinite: true,
		},
		{
			name:         "explored node",
			visits:       5,
			totalValue:   3.0,
			parentVisits: 20,
			exploration:  1.414,
			wantInfinite: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{Visits: tt.visits, TotalValue: tt.totalValue}
			uct := node.UCTValue(tt.parentVisits, tt.exploration)

			if tt.wantInfinite {
				assert.True(t, math.IsInf(uct, 1), "Expected infinite UCT for unexplored node")
			} else {
				assert.False(t, math.IsInf(uct, 0), "Expected finite UCT")
				assert.Greater(t, uct, 0.0, "UCT should be positive")
			}
		})
	}
}

// TestNode_Update tests node value updates.
func TestNode_Update(t *testing.T) {
	node := &Node{}

	node.Update(0.8)
	assert.Equal(t, 1, node.Visits)
	assert.InDelta(t, 0.8, node.TotalValue, 0.001)

	node.Update(0.6)
	assert.Equal(t, 2, node.Visits)
	assert.InDelta(t, 1.4, node.TotalValue, 0.001)
	assert.InDelta(t, 0.7, node.QValue(), 0.001)
}

// TestAgentState_Clone tests state cloning.
func TestAgentState_Clone(t *testing.T) {
	original := &AgentState{
		Query:          "test query",
		CurrentContext: "context",
		TokensUsed:     100,
		Observations: []Observation{
			{Action: &Action{Tool: "repl"}, Result: "ok", Success: true},
		},
	}

	clone := original.Clone()

	assert.Equal(t, original.Query, clone.Query)
	assert.Equal(t, original.CurrentContext, clone.CurrentContext)
	assert.Equal(t, original.TokensUsed, clone.TokensUsed)
	assert.Len(t, clone.Observations, 1)

	// Modify clone, original should be unchanged
	clone.Query = "modified"
	clone.Observations = append(clone.Observations, Observation{})
	assert.Equal(t, "test query", original.Query)
	assert.Len(t, original.Observations, 1)
}

// TestTree_Basic tests tree operations.
func TestTree_Basic(t *testing.T) {
	root := &Node{ID: "root"}
	tree := NewTree(root)

	assert.Equal(t, root, tree.Root())
	assert.Equal(t, 1, tree.Size())

	child := &Node{ID: "child", ParentID: "root"}
	tree.AddNode(child)

	assert.Equal(t, 2, tree.Size())
	assert.Equal(t, child, tree.GetNode("child"))
}

// TestTree_Stats tests tree statistics.
func TestTree_Stats(t *testing.T) {
	root := &Node{ID: "root", Depth: 0}
	tree := NewTree(root)

	// Add children
	child1 := &Node{ID: "c1", ParentID: "root", Depth: 1}
	child2 := &Node{ID: "c2", ParentID: "root", Depth: 1}
	root.Children = []*Node{child1, child2}
	tree.AddNode(child1)
	tree.AddNode(child2)

	// Add grandchild
	grandchild := &Node{ID: "gc1", ParentID: "c1", Depth: 2}
	child1.Children = []*Node{grandchild}
	tree.AddNode(grandchild)

	stats := tree.Stats()
	assert.Equal(t, 4, stats.NodesCreated)
	assert.Equal(t, 2, stats.MaxDepth)
	assert.Greater(t, stats.AvgBranching, 0.0)
}

// TestBudget tests budget tracking.
func TestBudget(t *testing.T) {
	budget := NewBudget(100)

	assert.Equal(t, 100, budget.Remaining())
	assert.False(t, budget.Exhausted())

	budget.Deduct(30)
	assert.Equal(t, 70, budget.Remaining())
	assert.Equal(t, 30, budget.Used())

	budget.Deduct(70)
	assert.Equal(t, 0, budget.Remaining())
	assert.True(t, budget.Exhausted())

	budget.Deduct(10)
	assert.True(t, budget.Exhausted())
}

// TestDefaultConfig tests default configuration.
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 50, config.MaxIterations)
	assert.Equal(t, 10, config.MaxDepth)
	assert.InDelta(t, 1.414, config.ExplorationConstant, 0.01)
	assert.Equal(t, 100000, config.TokenBudget)
}

// TestController_SelectNode tests UCB1 selection.
func TestController_SelectNode(t *testing.T) {
	config := DefaultConfig()
	config.ExplorationConstant = 1.414

	ctrl := NewController(nil, nil, nil, nil, config)

	// Build tree with varying visit counts
	root := &Node{ID: "root", Visits: 10}
	child1 := &Node{ID: "c1", ParentID: "root", Visits: 5, TotalValue: 2.5}
	child2 := &Node{ID: "c2", ParentID: "root", Visits: 3, TotalValue: 2.1}
	child3 := &Node{ID: "c3", ParentID: "root", Visits: 0} // Unexplored

	root.Children = []*Node{child1, child2, child3}
	tree := NewTree(root)
	tree.AddNode(child1)
	tree.AddNode(child2)
	tree.AddNode(child3)

	selected := ctrl.selectNode(tree, root)

	// Should select unexplored node (infinite UCT)
	assert.Equal(t, "c3", selected.ID)
}

// TestController_SelectNode_AllExplored tests selection with all nodes explored.
func TestController_SelectNode_AllExplored(t *testing.T) {
	config := DefaultConfig()
	ctrl := NewController(nil, nil, nil, nil, config)

	root := &Node{ID: "root", Visits: 10}
	child1 := &Node{ID: "c1", ParentID: "root", Visits: 3, TotalValue: 2.4} // Q=0.8
	child2 := &Node{ID: "c2", ParentID: "root", Visits: 5, TotalValue: 2.0} // Q=0.4

	root.Children = []*Node{child1, child2}
	tree := NewTree(root)
	tree.AddNode(child1)
	tree.AddNode(child2)

	selected := ctrl.selectNode(tree, root)

	// Should prefer higher Q-value with exploration bonus
	// c1: 0.8 + 1.414*sqrt(ln(10)/3) ≈ 0.8 + 1.27 = 2.07
	// c2: 0.4 + 1.414*sqrt(ln(10)/5) ≈ 0.4 + 0.96 = 1.36
	assert.Equal(t, "c1", selected.ID)
}

// TestController_Backpropagate tests value backpropagation.
func TestController_Backpropagate(t *testing.T) {
	config := DefaultConfig()
	config.ValueDecay = 0.9
	ctrl := NewController(nil, nil, nil, nil, config)

	root := &Node{ID: "root"}
	child := &Node{ID: "child", ParentID: "root"}
	grandchild := &Node{ID: "grandchild", ParentID: "child"}

	tree := NewTree(root)
	tree.AddNode(child)
	tree.AddNode(grandchild)

	ctrl.backpropagate(tree, grandchild, 1.0)

	// Check values propagated with decay
	assert.Equal(t, 1, grandchild.Visits)
	assert.InDelta(t, 1.0, grandchild.TotalValue, 0.01)

	assert.Equal(t, 1, child.Visits)
	assert.InDelta(t, 0.9, child.TotalValue, 0.01)

	assert.Equal(t, 1, root.Visits)
	assert.InDelta(t, 0.81, root.TotalValue, 0.01)
}

// TestController_Solve tests the full MCTS solve loop.
func TestController_Solve(t *testing.T) {
	config := Config{
		MaxIterations:       20,
		MaxDepth:            5,
		ExplorationConstant: 1.414,
		TokenBudget:         10000,
		Timeout:             5 * time.Second,
		ValueDecay:          0.95,
	}

	// Mock expander that generates actions
	expander := &MockExpander{
		ActionsPerNode: 2,
		Tools:          []string{"repl", "search"},
	}

	// Mock simulator that marks depth 3 as terminal
	simulator := &MockSimulator{
		ValueFunc: func(n *Node) float64 {
			if n.Depth >= 3 {
				return 0.9
			}
			return 0.5
		},
		TerminalFunc: func(n *Node) bool {
			return n.Depth >= 3
		},
	}

	ctrl := NewController(expander, simulator, nil, nil, config)
	ctx := context.Background()

	solution, err := ctrl.Solve(ctx, "Test query")
	require.NoError(t, err)
	require.NotNil(t, solution)

	assert.Greater(t, solution.Stats.Iterations, 0)
	assert.Greater(t, solution.Stats.NodesCreated, 1)
}

// TestController_Solve_Timeout tests timeout handling.
func TestController_Solve_Timeout(t *testing.T) {
	config := Config{
		MaxIterations:       1000,
		MaxDepth:            10,
		ExplorationConstant: 1.414,
		TokenBudget:         100000,
		Timeout:             50 * time.Millisecond,
		ValueDecay:          0.95,
	}

	// Slow expander
	expander := &MockExpander{ActionsPerNode: 3}
	simulator := &MockSimulator{
		ValueFunc: func(n *Node) float64 {
			time.Sleep(10 * time.Millisecond)
			return 0.5
		},
	}

	ctrl := NewController(expander, simulator, nil, nil, config)
	ctx := context.Background()

	solution, err := ctrl.Solve(ctx, "Test query")

	// Should timeout, may have error from context
	if err == nil {
		assert.Equal(t, TerminatedTimeout, solution.Stats.TerminatedBy)
	}
	assert.LessOrEqual(t, solution.Stats.Duration, 200*time.Millisecond)
}

// TestController_Solve_BudgetExhausted tests budget handling.
func TestController_Solve_BudgetExhausted(t *testing.T) {
	config := Config{
		MaxIterations:       100,
		MaxDepth:            5,
		ExplorationConstant: 1.414,
		TokenBudget:         50, // Very small budget
		Timeout:             5 * time.Second,
		ValueDecay:          0.95,
	}

	expander := &MockExpander{ActionsPerNode: 2}
	simulator := &MockSimulator{
		ValueFunc: func(n *Node) float64 { return 0.5 },
	}

	ctrl := NewController(expander, simulator, nil, nil, config)
	ctx := context.Background()

	solution, err := ctrl.Solve(ctx, "Test query")
	require.NoError(t, err)

	assert.Equal(t, TerminatedBudget, solution.Stats.TerminatedBy)
}

// TestMockExpander tests mock expander.
func TestMockExpander(t *testing.T) {
	expander := &MockExpander{
		ActionsPerNode: 3,
		Tools:          []string{"tool1", "tool2"},
	}

	parent := &Node{
		ID:    "parent",
		Depth: 1,
		State: &AgentState{Query: "test"},
	}

	children, err := expander.Expand(context.Background(), parent)
	require.NoError(t, err)
	assert.Len(t, children, 3)

	// Check actions are set
	for _, child := range children {
		assert.NotNil(t, child.Action)
		assert.Contains(t, []string{"tool1", "tool2"}, child.Action.Tool)
	}
}

// TestMockSimulator tests mock simulator.
func TestMockSimulator(t *testing.T) {
	var visitCount int64

	simulator := &MockSimulator{
		ValueFunc: func(n *Node) float64 {
			atomic.AddInt64(&visitCount, 1)
			return 0.7
		},
		TerminalFunc: func(n *Node) bool {
			return n.Depth >= 3
		},
	}

	node := &Node{
		Depth:  2,
		Action: &Action{Tool: "test", Input: "input"},
	}

	value, tokens, err := simulator.Simulate(context.Background(), node)
	require.NoError(t, err)

	assert.InDelta(t, 0.7, value, 0.01)
	assert.Equal(t, 10, tokens) // Default mock tokens
	assert.False(t, node.IsTerminal)

	// Simulate at depth 3 should be terminal
	node.Depth = 3
	_, _, _ = simulator.Simulate(context.Background(), node)
	assert.True(t, node.IsTerminal)
}

// TestHeuristicValuator tests heuristic valuation.
func TestHeuristicValuator(t *testing.T) {
	valuator := NewHeuristicValuator()

	tests := []struct {
		name         string
		observations []Observation
		depth        int
		minValue     float64
		maxValue     float64
	}{
		{
			name:         "empty state",
			observations: nil,
			depth:        0,
			minValue:     0.4,
			maxValue:     0.6,
		},
		{
			name: "success increases value",
			observations: []Observation{
				{Success: true},
				{Success: true},
			},
			depth:    1,
			minValue: 0.7,
			maxValue: 0.9,
		},
		{
			name: "failure decreases value",
			observations: []Observation{
				{Success: false},
				{Success: false},
			},
			depth:    0,
			minValue: 0.0,
			maxValue: 0.2,
		},
		{
			name: "depth penalty",
			observations: []Observation{
				{Success: true},
			},
			depth:    5,
			minValue: 0.4,
			maxValue: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{
				Depth: tt.depth,
				State: &AgentState{Observations: tt.observations},
			}

			value, err := valuator.Value(context.Background(), node)
			require.NoError(t, err)

			assert.GreaterOrEqual(t, value, tt.minValue)
			assert.LessOrEqual(t, value, tt.maxValue)
		})
	}
}

// TestToolRegistry tests tool registration and execution.
func TestToolRegistry(t *testing.T) {
	registry := NewToolRegistry()

	// Register mock tool
	tool := NewMockTool("test", "Test tool", func(ctx context.Context, input string) (*ToolResult, error) {
		return &ToolResult{
			Output:  "Result: " + input,
			Success: true,
			Tokens:  5,
		}, nil
	})

	registry.Register(tool)

	assert.True(t, registry.Has("test"))
	assert.False(t, registry.Has("unknown"))
	assert.Equal(t, 1, registry.Count())
	assert.Contains(t, registry.Names(), "test")
	assert.Contains(t, registry.Describe(), "test")

	// Execute
	result, err := registry.Execute(context.Background(), "test", "hello")
	require.NoError(t, err)
	assert.Equal(t, "Result: hello", result.Output)
	assert.True(t, result.Success)

	// Unknown tool
	_, err = registry.Execute(context.Background(), "unknown", "input")
	assert.Error(t, err)
}

// TestCapabilityMatcher tests capability matching.
func TestCapabilityMatcher(t *testing.T) {
	profiles := DefaultToolProfiles()
	matcher := NewCapabilityMatcher(profiles)

	// Find by single capability
	codeTools := matcher.FindByCapability(CapCodeExecution)
	assert.Contains(t, codeTools, "repl")

	searchTools := matcher.FindByCapability(CapSearch)
	assert.Contains(t, searchTools, "search")

	// Find best tool
	best, found := matcher.BestToolFor(CapFileRead)
	assert.True(t, found)
	assert.Equal(t, "file_read", best)
}

// TestLLMExpander_ParseActions tests action parsing.
func TestLLMExpander_ParseActions(t *testing.T) {
	expander := &LLMExpander{}

	response := `[Action 1]
Tool: repl
Input: print("hello")
Reasoning: Test output

[Action 2]
Tool: search
Input: find documentation
Reasoning: Need more info`

	actions := expander.parseActions(response)

	require.Len(t, actions, 2)
	assert.Equal(t, "repl", actions[0].Tool)
	assert.Equal(t, `print("hello")`, actions[0].Input)
	assert.Equal(t, "search", actions[1].Tool)
}

// TestRealSimulator_IsTerminal tests terminal detection.
func TestRealSimulator_IsTerminal(t *testing.T) {
	simulator := &RealSimulator{}

	tests := []struct {
		name     string
		depth    int
		result   string
		expected bool
	}{
		{"max depth", 10, "anything", true},
		{"final answer", 2, "FINAL_ANSWER: 42", true},
		{"task complete", 2, "TASK_COMPLETE", true},
		{"solution found", 2, "Solution: done", true},
		{"normal result", 2, "partial result", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{
				Depth: tt.depth,
				State: &AgentState{
					Observations: []Observation{
						{Result: tt.result, Success: true},
					},
				},
			}

			assert.Equal(t, tt.expected, simulator.isTerminal(node))
		})
	}
}

// TestTruncate tests string truncation.
func TestTruncate(t *testing.T) {
	assert.Equal(t, "short", truncate("short", 10))
	assert.Equal(t, "long stri...", truncate("long string", 9))
	assert.Equal(t, "", truncate("", 5))
}

// TestClamp tests value clamping.
func TestClamp(t *testing.T) {
	assert.InDelta(t, 0.5, clamp(0.5, 0, 1), 0.01)
	assert.InDelta(t, 0.0, clamp(-0.5, 0, 1), 0.01)
	assert.InDelta(t, 1.0, clamp(1.5, 0, 1), 0.01)
}

// BenchmarkController_SelectNode benchmarks UCB1 selection.
func BenchmarkController_SelectNode(b *testing.B) {
	config := DefaultConfig()
	ctrl := NewController(nil, nil, nil, nil, config)

	// Build tree with many children
	root := &Node{ID: "root", Visits: 100}
	for i := 0; i < 50; i++ {
		child := &Node{
			ID:         fmt.Sprintf("child-%d", i),
			ParentID:   "root",
			Visits:     i + 1,
			TotalValue: float64(i) * 0.1,
		}
		root.Children = append(root.Children, child)
	}
	tree := NewTree(root)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctrl.selectNode(tree, root)
	}
}

// BenchmarkNode_UCTValue benchmarks UCT calculation.
func BenchmarkNode_UCTValue(b *testing.B) {
	node := &Node{Visits: 10, TotalValue: 5.0}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		node.UCTValue(100, 1.414)
	}
}
