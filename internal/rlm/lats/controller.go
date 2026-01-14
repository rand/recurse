package lats

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// Controller implements LATS for tool orchestration.
type Controller struct {
	expander  Expander
	simulator Simulator
	valuator  Valuator
	tools     *ToolRegistry
	config    Config

	// Metrics
	iterations int64
	nodeCount  int64
}

// NewController creates a new LATS controller.
func NewController(
	expander Expander,
	simulator Simulator,
	valuator Valuator,
	tools *ToolRegistry,
	config Config,
) *Controller {
	return &Controller{
		expander:  expander,
		simulator: simulator,
		valuator:  valuator,
		tools:     tools,
		config:    config,
	}
}

// Solve runs LATS to find an optimal tool sequence.
func (c *Controller) Solve(ctx context.Context, query string) (*Solution, error) {
	start := time.Now()

	// Apply timeout
	if c.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.Timeout)
		defer cancel()
	}

	// Initialize root
	root := &Node{
		ID:        c.generateID(),
		State:     &AgentState{Query: query},
		Depth:     0,
		CreatedAt: time.Now(),
	}

	tree := NewTree(root)
	budget := NewBudget(c.config.TokenBudget)

	var terminatedBy TerminationReason

	for iter := 0; iter < c.config.MaxIterations; iter++ {
		atomic.AddInt64(&c.iterations, 1)

		// Check context cancellation
		select {
		case <-ctx.Done():
			terminatedBy = TerminatedTimeout
			return c.buildSolution(tree, nil, start, terminatedBy), ctx.Err()
		default:
		}

		// Check budget
		if budget.Exhausted() {
			terminatedBy = TerminatedBudget
			break
		}

		// Selection: traverse to promising leaf
		leaf := c.selectNode(tree, root)

		// Check if terminal solution
		if leaf.IsTerminal {
			if c.isSolution(leaf) {
				terminatedBy = TerminatedSolution
				return c.buildSolution(tree, leaf, start, terminatedBy), nil
			}
			continue
		}

		// Expansion: generate candidate actions
		children, err := c.expand(ctx, leaf)
		if err != nil {
			// Log error but continue
			continue
		}

		if len(children) == 0 {
			// No valid expansions, mark as terminal
			leaf.IsTerminal = true
			continue
		}

		// Add children to tree
		for _, child := range children {
			tree.AddNode(child)
			leaf.AddChild(child)
			atomic.AddInt64(&c.nodeCount, 1)
		}

		// Simulation: evaluate first child (could also be random)
		child := children[0]
		value, tokens, err := c.simulate(ctx, child)
		if err != nil {
			// Use default value on error
			value = 0.3
		}
		budget.Deduct(tokens)

		// Backpropagation: update values up the tree
		c.backpropagate(tree, child, value)
	}

	if terminatedBy == "" {
		terminatedBy = TerminatedMaxIter
	}

	// Return best solution found
	bestTerminal := c.findBestTerminal(tree)
	return c.buildSolution(tree, bestTerminal, start, terminatedBy), nil
}

// selectNode traverses tree using UCB1 to find a leaf to expand.
func (c *Controller) selectNode(tree *Tree, root *Node) *Node {
	current := root

	for len(current.Children) > 0 && !current.IsTerminal {
		// Select child with highest UCT value
		var best *Node
		var bestUCT float64 = -1

		current.mu.RLock()
		parentVisits := current.Visits
		children := current.Children
		current.mu.RUnlock()

		if parentVisits == 0 {
			parentVisits = 1
		}

		for _, child := range children {
			uct := child.UCTValue(parentVisits, c.config.ExplorationConstant)
			if uct > bestUCT {
				bestUCT = uct
				best = child
			}
		}

		if best == nil {
			break
		}
		current = best
	}

	return current
}

// expand generates candidate actions for a node.
func (c *Controller) expand(ctx context.Context, node *Node) ([]*Node, error) {
	if node.Depth >= c.config.MaxDepth {
		return nil, nil
	}

	children, err := c.expander.Expand(ctx, node)
	if err != nil {
		return nil, fmt.Errorf("expand: %w", err)
	}

	// Assign IDs and parent references
	for _, child := range children {
		if child.ID == "" {
			child.ID = c.generateID()
		}
		child.ParentID = node.ID
		child.Depth = node.Depth + 1
		child.CreatedAt = time.Now()
	}

	return children, nil
}

// simulate executes an action and returns its value.
func (c *Controller) simulate(ctx context.Context, node *Node) (float64, int, error) {
	return c.simulator.Simulate(ctx, node)
}

// backpropagate updates values from node to root.
func (c *Controller) backpropagate(tree *Tree, node *Node, value float64) {
	current := node
	decay := c.config.ValueDecay
	if decay <= 0 || decay > 1 {
		decay = 0.95
	}

	for current != nil {
		current.Update(value)

		// Decay value as we go up
		value *= decay

		if current.ParentID == "" {
			break
		}
		current = tree.GetNode(current.ParentID)
	}
}

// isSolution checks if a terminal node is a valid solution.
func (c *Controller) isSolution(node *Node) bool {
	if !node.IsTerminal {
		return false
	}

	// Check if we have observations indicating success
	if node.State != nil && len(node.State.Observations) > 0 {
		last := node.State.Observations[len(node.State.Observations)-1]
		return last.Success && last.Result != ""
	}

	return false
}

// findBestTerminal finds the terminal node with highest value.
func (c *Controller) findBestTerminal(tree *Tree) *Node {
	tree.mu.RLock()
	defer tree.mu.RUnlock()

	var best *Node
	var bestValue float64 = -1

	for _, node := range tree.nodes {
		if node.IsTerminal && node.QValue() > bestValue {
			bestValue = node.QValue()
			best = node
		}
	}

	// If no terminal, find highest value leaf
	if best == nil {
		for _, node := range tree.nodes {
			if len(node.Children) == 0 && node.QValue() > bestValue {
				bestValue = node.QValue()
				best = node
			}
		}
	}

	return best
}

// buildSolution constructs a Solution from the tree and terminal node.
func (c *Controller) buildSolution(
	tree *Tree,
	terminal *Node,
	start time.Time,
	terminatedBy TerminationReason,
) *Solution {
	treeStats := tree.Stats()

	solution := &Solution{
		Stats: SolveStats{
			Iterations:   int(atomic.LoadInt64(&c.iterations)),
			NodesCreated: treeStats.NodesCreated,
			MaxDepth:     treeStats.MaxDepth,
			AvgBranching: treeStats.AvgBranching,
			Duration:     time.Since(start),
			TerminatedBy: terminatedBy,
		},
	}

	if terminal == nil {
		return solution
	}

	// Build path from root to terminal
	solution.Path = c.getPath(tree, terminal)

	// Extract final answer
	if terminal.State != nil && len(terminal.State.Observations) > 0 {
		last := terminal.State.Observations[len(terminal.State.Observations)-1]
		solution.FinalAnswer = last.Result
		solution.TotalTokens = terminal.State.TokensUsed
	}

	return solution
}

// getPath returns the path from root to node.
func (c *Controller) getPath(tree *Tree, node *Node) []*Node {
	var path []*Node
	current := node

	for current != nil {
		path = append([]*Node{current}, path...)
		if current.ParentID == "" {
			break
		}
		current = tree.GetNode(current.ParentID)
	}

	return path
}

// generateID generates a unique node ID.
func (c *Controller) generateID() string {
	return fmt.Sprintf("lats-%d", atomic.AddInt64(&c.nodeCount, 1))
}

// Metrics returns controller metrics.
func (c *Controller) Metrics() ControllerMetrics {
	return ControllerMetrics{
		Iterations: atomic.LoadInt64(&c.iterations),
		NodeCount:  atomic.LoadInt64(&c.nodeCount),
	}
}

// ControllerMetrics contains controller statistics.
type ControllerMetrics struct {
	Iterations int64
	NodeCount  int64
}

// Errors for LATS operations.
var (
	ErrNoSolution     = errors.New("no solution found")
	ErrBudgetExhausted = errors.New("token budget exhausted")
	ErrMaxIterations  = errors.New("maximum iterations reached")
	ErrTimeout        = errors.New("solve timeout")
)
