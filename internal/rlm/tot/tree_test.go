package tot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeStatus_String(t *testing.T) {
	tests := []struct {
		status   NodeStatus
		expected string
	}{
		{StatusPending, "pending"},
		{StatusExpanded, "expanded"},
		{StatusEvaluated, "evaluated"},
		{StatusTerminal, "terminal"},
		{StatusPruned, "pruned"},
		{NodeStatus(99), "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.status.String())
	}
}

func TestThoughtNode_New(t *testing.T) {
	node := NewThoughtNode("node-1", "Initial thought", nil)

	assert.Equal(t, "node-1", node.ID)
	assert.Equal(t, "Initial thought", node.Thought)
	assert.Equal(t, StatusPending, node.Status)
	assert.Equal(t, 0, node.Depth)
	assert.Nil(t, node.Parent)
	assert.Empty(t, node.Children)
	assert.NotZero(t, node.CreatedAt)
}

func TestThoughtNode_NewWithParent(t *testing.T) {
	parent := NewThoughtNode("parent", "Parent thought", nil)
	child := NewThoughtNode("child", "Child thought", parent)

	assert.Equal(t, parent, child.Parent)
	assert.Equal(t, 1, child.Depth)
}

func TestThoughtNode_AddChild(t *testing.T) {
	parent := NewThoughtNode("parent", "Parent", nil)
	child := NewThoughtNode("child", "Child", nil)

	parent.AddChild(child)

	assert.Len(t, parent.Children, 1)
	assert.Equal(t, parent, child.Parent)
	assert.Equal(t, 1, child.Depth)
}

func TestThoughtNode_SetValue(t *testing.T) {
	node := NewThoughtNode("node", "Thought", nil)

	node.SetValue(0.8, 0.9)

	assert.Equal(t, 0.8, node.ValueEstimate)
	assert.Equal(t, 0.9, node.Confidence)
	assert.Equal(t, StatusEvaluated, node.Status)
	assert.NotZero(t, node.EvaluatedAt)
}

func TestThoughtNode_MarkTerminal(t *testing.T) {
	node := NewThoughtNode("node", "Thought", nil)
	node.MarkTerminal()
	assert.Equal(t, StatusTerminal, node.Status)
}

func TestThoughtNode_MarkPruned(t *testing.T) {
	node := NewThoughtNode("node", "Thought", nil)
	node.MarkPruned()
	assert.Equal(t, StatusPruned, node.Status)
}

func TestThoughtNode_IsLeaf(t *testing.T) {
	parent := NewThoughtNode("parent", "Parent", nil)
	assert.True(t, parent.IsLeaf())

	child := NewThoughtNode("child", "Child", nil)
	parent.AddChild(child)
	assert.False(t, parent.IsLeaf())
	assert.True(t, child.IsLeaf())
}

func TestThoughtNode_Path(t *testing.T) {
	root := NewThoughtNode("root", "Root", nil)
	child1 := NewThoughtNode("child1", "Child 1", nil)
	child2 := NewThoughtNode("child2", "Child 2", nil)

	root.AddChild(child1)
	child1.AddChild(child2)

	path := child2.Path()
	require.Len(t, path, 3)
	assert.Equal(t, root, path[0])
	assert.Equal(t, child1, path[1])
	assert.Equal(t, child2, path[2])
}

func TestThoughtNode_PathThoughts(t *testing.T) {
	root := NewThoughtNode("root", "Step 1", nil)
	child := NewThoughtNode("child", "Step 2", nil)
	root.AddChild(child)

	thoughts := child.PathThoughts()
	assert.Equal(t, []string{"Step 1", "Step 2"}, thoughts)
}

func TestThoughtNode_BestChild(t *testing.T) {
	parent := NewThoughtNode("parent", "Parent", nil)
	assert.Nil(t, parent.BestChild())

	child1 := NewThoughtNode("child1", "Child 1", nil)
	child1.ValueEstimate = 0.5
	child2 := NewThoughtNode("child2", "Child 2", nil)
	child2.ValueEstimate = 0.8
	child3 := NewThoughtNode("child3", "Child 3", nil)
	child3.ValueEstimate = 0.3

	parent.AddChild(child1)
	parent.AddChild(child2)
	parent.AddChild(child3)

	best := parent.BestChild()
	assert.Equal(t, child2, best)
}

func TestThoughtTree_Initialize(t *testing.T) {
	tree := NewThoughtTree(DefaultToTConfig(), &MockEvaluator{}, &MockGenerator{})

	root := tree.Initialize("Solve this problem")

	assert.NotNil(t, root)
	assert.Equal(t, "Solve this problem", root.Thought)
	assert.Equal(t, root, tree.Root())
	assert.Equal(t, int64(1), tree.NodeCount())
}

func TestThoughtTree_Branch(t *testing.T) {
	config := DefaultToTConfig()
	config.MaxBranches = 3
	config.EvaluateBeforeExpand = false

	tree := NewThoughtTree(config, &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	children, err := tree.Branch(context.Background(), root)
	require.NoError(t, err)

	assert.Len(t, children, 3)
	assert.Len(t, root.Children, 3)
	assert.Equal(t, StatusExpanded, root.Status)
	assert.Equal(t, int64(4), tree.NodeCount()) // root + 3 children
}

func TestThoughtTree_Branch_RequiresEvaluation(t *testing.T) {
	config := DefaultToTConfig()
	config.EvaluateBeforeExpand = true

	tree := NewThoughtTree(config, &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	_, err := tree.Branch(context.Background(), root)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "evaluated before branching")
}

func TestThoughtTree_Branch_MaxDepth(t *testing.T) {
	config := DefaultToTConfig()
	config.MaxDepth = 2
	config.EvaluateBeforeExpand = false

	tree := NewThoughtTree(config, &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	// Branch from root (depth 0)
	children, err := tree.Branch(context.Background(), root)
	require.NoError(t, err)
	assert.NotEmpty(t, children)

	// Branch from child (depth 1)
	children2, err := tree.Branch(context.Background(), children[0])
	require.NoError(t, err)
	assert.NotEmpty(t, children2)

	// Cannot branch from depth 2
	children3, err := tree.Branch(context.Background(), children2[0])
	require.NoError(t, err)
	assert.Empty(t, children3)
}

func TestThoughtTree_Branch_MaxNodes(t *testing.T) {
	config := DefaultToTConfig()
	config.MaxNodes = 4 // root + 3 children = 4, then at limit
	config.MaxBranches = 3
	config.EvaluateBeforeExpand = false

	tree := NewThoughtTree(config, &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	// First branch: 1 + 3 = 4 nodes (at limit)
	children, err := tree.Branch(context.Background(), root)
	require.NoError(t, err)
	require.NotEmpty(t, children)

	// Second branch should fail - already at max nodes
	_, err = tree.Branch(context.Background(), children[0])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max nodes")
}

func TestThoughtTree_Evaluate(t *testing.T) {
	evaluator := &MockEvaluator{
		ValueFunc: func(n *ThoughtNode) float64 {
			return 0.75
		},
		TerminalFunc: func(n *ThoughtNode) bool {
			return n.Depth >= 2
		},
	}

	tree := NewThoughtTree(DefaultToTConfig(), evaluator, &MockGenerator{})
	root := tree.Initialize("Problem")

	err := tree.Evaluate(context.Background(), root)
	require.NoError(t, err)

	assert.Equal(t, 0.75, root.ValueEstimate)
	assert.Equal(t, StatusEvaluated, root.Status)
	assert.False(t, root.Status == StatusTerminal) // depth 0
}

func TestThoughtTree_Evaluate_Terminal(t *testing.T) {
	evaluator := &MockEvaluator{
		ValueFunc: func(n *ThoughtNode) float64 {
			return 0.9
		},
		TerminalFunc: func(n *ThoughtNode) bool {
			return true
		},
	}

	tree := NewThoughtTree(DefaultToTConfig(), evaluator, &MockGenerator{})
	root := tree.Initialize("Problem")

	err := tree.Evaluate(context.Background(), root)
	require.NoError(t, err)

	assert.Equal(t, StatusTerminal, root.Status)
}

func TestThoughtTree_Evaluate_Pruned(t *testing.T) {
	config := DefaultToTConfig()
	config.ValueThreshold = 0.5

	evaluator := &MockEvaluator{
		ValueFunc: func(n *ThoughtNode) float64 {
			return 0.2 // Below threshold
		},
	}

	tree := NewThoughtTree(config, evaluator, &MockGenerator{})
	root := tree.Initialize("Problem")

	err := tree.Evaluate(context.Background(), root)
	require.NoError(t, err)

	assert.Equal(t, StatusPruned, root.Status)
}

func TestThoughtTree_Backtrack(t *testing.T) {
	tree := NewThoughtTree(DefaultToTConfig(), &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	// Can't backtrack from root
	assert.Nil(t, tree.Backtrack(root))

	child := NewThoughtNode("child", "Child", nil)
	root.AddChild(child)

	parent := tree.Backtrack(child)
	assert.Equal(t, root, parent)

	metrics := tree.Metrics()
	assert.Equal(t, int64(1), metrics.Backtracks)
}

func TestThoughtTree_FindBestPath(t *testing.T) {
	tree := NewThoughtTree(DefaultToTConfig(), &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	// Build a tree with multiple paths
	child1 := NewThoughtNode("c1", "Path 1", nil)
	child1.ValueEstimate = 0.5
	child1.Status = StatusEvaluated
	root.AddChild(child1)

	child2 := NewThoughtNode("c2", "Path 2", nil)
	child2.ValueEstimate = 0.8
	child2.Status = StatusTerminal
	root.AddChild(child2)

	child1_1 := NewThoughtNode("c1_1", "Path 1 branch", nil)
	child1_1.ValueEstimate = 0.9
	child1_1.Status = StatusTerminal
	child1.AddChild(child1_1)

	bestPath := tree.FindBestPath()
	require.Len(t, bestPath, 3)
	assert.Equal(t, child1_1, bestPath[2]) // Best terminal
}

func TestThoughtTree_FindBestPath_NoTerminal(t *testing.T) {
	tree := NewThoughtTree(DefaultToTConfig(), &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	child := NewThoughtNode("child", "Child", nil)
	child.Status = StatusEvaluated
	root.AddChild(child)

	path := tree.FindBestPath()
	assert.Nil(t, path)
}

func TestThoughtTree_AllTerminals(t *testing.T) {
	tree := NewThoughtTree(DefaultToTConfig(), &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	term1 := NewThoughtNode("t1", "Terminal 1", nil)
	term1.Status = StatusTerminal
	term1.ValueEstimate = 0.8
	root.AddChild(term1)

	term2 := NewThoughtNode("t2", "Terminal 2", nil)
	term2.Status = StatusTerminal
	term2.ValueEstimate = 0.9
	root.AddChild(term2)

	nonTerm := NewThoughtNode("nt", "Non-terminal", nil)
	nonTerm.Status = StatusEvaluated
	root.AddChild(nonTerm)

	terminals := tree.AllTerminals()
	assert.Len(t, terminals, 2)
}

func TestThoughtTree_Metrics(t *testing.T) {
	config := DefaultToTConfig()
	config.EvaluateBeforeExpand = false

	tree := NewThoughtTree(config, &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	// Perform operations
	_ = tree.Evaluate(context.Background(), root)
	children, _ := tree.Branch(context.Background(), root)
	_ = tree.Backtrack(children[0])

	metrics := tree.Metrics()
	assert.Equal(t, int64(4), metrics.NodeCount) // root + 3 children
	assert.Equal(t, int64(1), metrics.Expansions)
	assert.Equal(t, int64(1), metrics.Evaluations)
	assert.Equal(t, int64(1), metrics.Backtracks)
}

func TestThoughtTree_ConcurrentOperations(t *testing.T) {
	config := DefaultToTConfig()
	config.MaxNodes = 1000
	config.EvaluateBeforeExpand = false

	tree := NewThoughtTree(config, &MockEvaluator{}, &MockGenerator{})
	root := tree.Initialize("Problem")

	var wg sync.WaitGroup

	// Concurrent evaluations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tree.Evaluate(context.Background(), root)
		}()
	}

	// Concurrent metric reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tree.Metrics()
		}()
	}

	wg.Wait()
}

// Evaluator tests

func TestParseEvaluationResponse(t *testing.T) {
	response := `VALUE: 0.85
CONFIDENCE: 0.9
TERMINAL: true
REASONING: This step correctly identifies the key insight.
CRITIQUE: Could be more detailed.`

	result, err := parseEvaluationResponse(response)
	require.NoError(t, err)

	assert.InDelta(t, 0.85, result.Value, 0.001)
	assert.InDelta(t, 0.9, result.Confidence, 0.001)
	assert.True(t, result.IsTerminal)
	assert.Contains(t, result.Reasoning, "correctly identifies")
}

func TestParseEvaluationResponse_PartialResponse(t *testing.T) {
	response := `VALUE: 0.6`

	result, err := parseEvaluationResponse(response)
	require.NoError(t, err)

	assert.InDelta(t, 0.6, result.Value, 0.001)
	assert.InDelta(t, 0.5, result.Confidence, 0.001) // default
	assert.False(t, result.IsTerminal)
}

func TestParseThoughtList(t *testing.T) {
	response := `1. First approach: try X
2. Second approach: consider Y
3. Third approach: explore Z
4. Fourth approach: analyze W`

	thoughts := parseThoughtList(response, 3)
	require.Len(t, thoughts, 3)
	assert.Contains(t, thoughts[0], "First approach")
	assert.Contains(t, thoughts[1], "Second approach")
	assert.Contains(t, thoughts[2], "Third approach")
}

func TestMockEvaluator(t *testing.T) {
	evaluator := &MockEvaluator{
		ValueFunc: func(n *ThoughtNode) float64 {
			return float64(n.Depth) * 0.2
		},
		TerminalFunc: func(n *ThoughtNode) bool {
			return n.Depth >= 3
		},
	}

	node := NewThoughtNode("test", "Test", nil)
	node.Depth = 2

	result, err := evaluator.EvaluateThought(context.Background(), node)
	require.NoError(t, err)

	assert.InDelta(t, 0.4, result.Value, 0.001)
	assert.False(t, result.IsTerminal)

	node.Depth = 3
	result, err = evaluator.EvaluateThought(context.Background(), node)
	require.NoError(t, err)
	assert.True(t, result.IsTerminal)
}

func TestMockGenerator(t *testing.T) {
	generator := &MockGenerator{
		ThoughtsFunc: func(node *ThoughtNode, n int) []string {
			thoughts := make([]string, n)
			for i := 0; i < n; i++ {
				thoughts[i] = fmt.Sprintf("Custom thought %d", i)
			}
			return thoughts
		},
	}

	node := NewThoughtNode("test", "Test", nil)
	thoughts, err := generator.GenerateThoughts(context.Background(), node, 3)
	require.NoError(t, err)

	assert.Len(t, thoughts, 3)
	assert.Equal(t, "Custom thought 0", thoughts[0])
}

func TestMockGenerator_Default(t *testing.T) {
	generator := &MockGenerator{}
	node := NewThoughtNode("test", "Test", nil)
	node.Depth = 2

	thoughts, err := generator.GenerateThoughts(context.Background(), node, 3)
	require.NoError(t, err)

	assert.Len(t, thoughts, 3)
	assert.Contains(t, thoughts[0], "depth 2")
}

func TestHeuristicEvaluator(t *testing.T) {
	evaluator := NewHeuristicEvaluator()

	// Test positive keywords
	node := NewThoughtNode("test", "Therefore, the solution is correct", nil)
	result, err := evaluator.EvaluateThought(context.Background(), node)
	require.NoError(t, err)
	assert.Greater(t, result.Value, 0.5)

	// Test negative keywords
	node2 := NewThoughtNode("test2", "This approach is incorrect and wrong", nil)
	result2, err := evaluator.EvaluateThought(context.Background(), node2)
	require.NoError(t, err)
	assert.Less(t, result2.Value, 0.5)

	// Test terminal pattern
	node3 := NewThoughtNode("test3", "The answer is 42", nil)
	result3, err := evaluator.EvaluateThought(context.Background(), node3)
	require.NoError(t, err)
	assert.True(t, result3.IsTerminal)
}

func TestHeuristicEvaluator_DepthBonus(t *testing.T) {
	evaluator := NewHeuristicEvaluator()

	node1 := NewThoughtNode("test1", "Neutral thought", nil)
	node1.Depth = 0
	result1, _ := evaluator.EvaluateThought(context.Background(), node1)

	node2 := NewThoughtNode("test2", "Neutral thought", nil)
	node2.Depth = 5
	result2, _ := evaluator.EvaluateThought(context.Background(), node2)

	// Deeper node should have higher value due to depth bonus
	assert.Greater(t, result2.Value, result1.Value)
}

func TestClamp(t *testing.T) {
	assert.Equal(t, 0.5, clamp(0.5, 0, 1))
	assert.Equal(t, 0.0, clamp(-0.5, 0, 1))
	assert.Equal(t, 1.0, clamp(1.5, 0, 1))
}

// Integration test

func TestThoughtTree_FullExploration(t *testing.T) {
	config := ToTConfig{
		MaxBranches:          2,
		MaxDepth:             3,
		ValueThreshold:       0.3,
		MaxNodes:             20,
		EvaluateBeforeExpand: true,
	}

	evaluator := &MockEvaluator{
		ValueFunc: func(n *ThoughtNode) float64 {
			// Higher value for "good" paths
			if strings.Contains(n.Thought, "good") {
				return 0.8
			}
			return 0.5
		},
		TerminalFunc: func(n *ThoughtNode) bool {
			return n.Depth >= 2 && strings.Contains(n.Thought, "solution")
		},
	}

	generator := &MockGenerator{
		ThoughtsFunc: func(node *ThoughtNode, n int) []string {
			if node.Depth == 0 {
				return []string{"good approach", "bad approach"}
			}
			if strings.Contains(node.Thought, "good") {
				return []string{"good solution found", "good alternative"}
			}
			return []string{"dead end", "another dead end"}
		},
	}

	tree := NewThoughtTree(config, evaluator, generator)
	root := tree.Initialize("Solve the problem")

	// Manual exploration for test
	ctx := context.Background()

	// Evaluate root
	err := tree.Evaluate(ctx, root)
	require.NoError(t, err)

	// Branch from root
	children, err := tree.Branch(ctx, root)
	require.NoError(t, err)
	require.Len(t, children, 2)

	// Evaluate children
	for _, child := range children {
		err := tree.Evaluate(ctx, child)
		require.NoError(t, err)
	}

	// Branch from "good approach"
	grandchildren, err := tree.Branch(ctx, children[0])
	require.NoError(t, err)

	// Evaluate grandchildren
	for _, gc := range grandchildren {
		err := tree.Evaluate(ctx, gc)
		require.NoError(t, err)
	}

	// Find best path
	bestPath := tree.FindBestPath()
	require.NotNil(t, bestPath)

	// Should end at terminal node with "solution"
	lastNode := bestPath[len(bestPath)-1]
	assert.Equal(t, StatusTerminal, lastNode.Status)
	assert.Contains(t, lastNode.Thought, "solution")

	// Check metrics
	metrics := tree.Metrics()
	assert.Greater(t, metrics.Expansions, int64(0))
	assert.Greater(t, metrics.Evaluations, int64(0))
}

func TestDefaultToTConfig(t *testing.T) {
	config := DefaultToTConfig()

	assert.Equal(t, 3, config.MaxBranches)
	assert.Equal(t, 5, config.MaxDepth)
	assert.Equal(t, 0.3, config.ValueThreshold)
	assert.Equal(t, StrategyBestFirst, config.Strategy)
	assert.Equal(t, 100, config.MaxNodes)
	assert.Equal(t, 5*time.Minute, config.Timeout)
	assert.True(t, config.EvaluateBeforeExpand)
}
