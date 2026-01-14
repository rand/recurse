package tot

import (
	"container/heap"
	"context"
	"fmt"
	"time"
)

// ExplorationResult contains the result of tree exploration.
type ExplorationResult struct {
	// Solution is the terminal node found (nil if no solution).
	Solution *ThoughtNode

	// BestPath is the path to the best node found.
	BestPath []*ThoughtNode

	// NodesExplored is the total nodes visited.
	NodesExplored int

	// MaxDepthReached is the deepest level explored.
	MaxDepthReached int

	// Duration is how long exploration took.
	Duration time.Duration

	// TerminatedBy indicates why exploration stopped.
	TerminatedBy TerminationReason
}

// TerminationReason indicates why exploration stopped.
type TerminationReason string

const (
	TerminatedSolution  TerminationReason = "solution_found"
	TerminatedMaxNodes  TerminationReason = "max_nodes"
	TerminatedMaxDepth  TerminationReason = "max_depth"
	TerminatedTimeout   TerminationReason = "timeout"
	TerminatedExhausted TerminationReason = "exhausted"
	TerminatedCancelled TerminationReason = "cancelled"
)

// Explore runs exploration using the configured strategy.
func (t *ThoughtTree) Explore(ctx context.Context, goal string) (*ExplorationResult, error) {
	switch t.config.Strategy {
	case StrategyBFS:
		return t.ExploreWithBFS(ctx, goal)
	case StrategyDFS:
		return t.ExploreWithDFS(ctx, goal)
	case StrategyBestFirst:
		return t.ExploreWithBestFirst(ctx, goal)
	default:
		return t.ExploreWithBestFirst(ctx, goal)
	}
}

// ExploreWithBFS explores the tree breadth-first.
// Explores all nodes at depth d before any at depth d+1.
// Good for finding shortest solution paths.
func (t *ThoughtTree) ExploreWithBFS(ctx context.Context, goal string) (*ExplorationResult, error) {
	start := time.Now()
	result := &ExplorationResult{}

	if t.root == nil {
		t.Initialize(goal)
	}

	// Initialize root
	if err := t.Evaluate(ctx, t.root); err != nil {
		return nil, fmt.Errorf("evaluate root: %w", err)
	}
	result.NodesExplored++

	if t.root.Status == StatusTerminal {
		result.Solution = t.root
		result.BestPath = t.root.Path()
		result.Duration = time.Since(start)
		result.TerminatedBy = TerminatedSolution
		return result, nil
	}

	// BFS queue
	queue := []*ThoughtNode{t.root}
	var maxDepth int

	for len(queue) > 0 {
		// Check context
		select {
		case <-ctx.Done():
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedCancelled
			result.BestPath = t.findBestLeafPath()
			return result, ctx.Err()
		default:
		}

		// Check timeout
		if t.config.Timeout > 0 && time.Since(start) > t.config.Timeout {
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedTimeout
			result.BestPath = t.findBestLeafPath()
			return result, nil
		}

		// Dequeue
		node := queue[0]
		queue = queue[1:]

		// Track depth
		if node.Depth > maxDepth {
			maxDepth = node.Depth
		}

		// Skip pruned nodes
		if node.Status == StatusPruned {
			continue
		}

		// Check depth limit
		if node.Depth >= t.config.MaxDepth {
			continue
		}

		// Branch
		children, err := t.Branch(ctx, node)
		if err != nil {
			// Max nodes or other limit - continue with what we have
			continue
		}

		// Evaluate and enqueue children
		for _, child := range children {
			if err := t.Evaluate(ctx, child); err != nil {
				continue
			}
			result.NodesExplored++

			// Check for solution
			if child.Status == StatusTerminal {
				result.Solution = child
				result.BestPath = child.Path()
				result.Duration = time.Since(start)
				result.MaxDepthReached = maxDepth
				result.TerminatedBy = TerminatedSolution
				return result, nil
			}

			// Enqueue if not pruned
			if child.Status != StatusPruned {
				queue = append(queue, child)
			}
		}
	}

	// No solution found
	result.Duration = time.Since(start)
	result.MaxDepthReached = maxDepth
	result.TerminatedBy = TerminatedExhausted
	result.BestPath = t.findBestLeafPath()
	return result, nil
}

// ExploreWithDFS explores the tree depth-first.
// Follows one path to max depth before backtracking.
// Good for finding any solution quickly, uses less memory.
func (t *ThoughtTree) ExploreWithDFS(ctx context.Context, goal string) (*ExplorationResult, error) {
	start := time.Now()
	result := &ExplorationResult{}

	if t.root == nil {
		t.Initialize(goal)
	}

	// Initialize root
	if err := t.Evaluate(ctx, t.root); err != nil {
		return nil, fmt.Errorf("evaluate root: %w", err)
	}
	result.NodesExplored++

	if t.root.Status == StatusTerminal {
		result.Solution = t.root
		result.BestPath = t.root.Path()
		result.Duration = time.Since(start)
		result.TerminatedBy = TerminatedSolution
		return result, nil
	}

	// DFS stack
	stack := []*ThoughtNode{t.root}
	var maxDepth int

	for len(stack) > 0 {
		// Check context
		select {
		case <-ctx.Done():
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedCancelled
			result.BestPath = t.findBestLeafPath()
			return result, ctx.Err()
		default:
		}

		// Check timeout
		if t.config.Timeout > 0 && time.Since(start) > t.config.Timeout {
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedTimeout
			result.BestPath = t.findBestLeafPath()
			return result, nil
		}

		// Pop
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// Track depth
		if node.Depth > maxDepth {
			maxDepth = node.Depth
		}

		// Skip pruned nodes
		if node.Status == StatusPruned {
			continue
		}

		// Check depth limit
		if node.Depth >= t.config.MaxDepth {
			continue
		}

		// Branch
		children, err := t.Branch(ctx, node)
		if err != nil {
			continue
		}

		// Evaluate and push children (reverse order for left-to-right exploration)
		for i := len(children) - 1; i >= 0; i-- {
			child := children[i]
			if err := t.Evaluate(ctx, child); err != nil {
				continue
			}
			result.NodesExplored++

			// Check for solution
			if child.Status == StatusTerminal {
				result.Solution = child
				result.BestPath = child.Path()
				result.Duration = time.Since(start)
				result.MaxDepthReached = maxDepth
				result.TerminatedBy = TerminatedSolution
				return result, nil
			}

			// Push if not pruned
			if child.Status != StatusPruned {
				stack = append(stack, child)
			}
		}
	}

	// No solution found
	result.Duration = time.Since(start)
	result.MaxDepthReached = maxDepth
	result.TerminatedBy = TerminatedExhausted
	result.BestPath = t.findBestLeafPath()
	return result, nil
}

// ExploreWithBestFirst explores highest-value nodes first.
// Uses a priority queue ordered by value estimate.
// Good for finding high-quality solutions efficiently.
func (t *ThoughtTree) ExploreWithBestFirst(ctx context.Context, goal string) (*ExplorationResult, error) {
	start := time.Now()
	result := &ExplorationResult{}

	if t.root == nil {
		t.Initialize(goal)
	}

	// Initialize root
	if err := t.Evaluate(ctx, t.root); err != nil {
		return nil, fmt.Errorf("evaluate root: %w", err)
	}
	result.NodesExplored++

	if t.root.Status == StatusTerminal {
		result.Solution = t.root
		result.BestPath = t.root.Path()
		result.Duration = time.Since(start)
		result.TerminatedBy = TerminatedSolution
		return result, nil
	}

	// Priority queue (max-heap by value)
	pq := &nodeHeap{}
	heap.Init(pq)
	heap.Push(pq, t.root)

	var maxDepth int

	for pq.Len() > 0 {
		// Check context
		select {
		case <-ctx.Done():
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedCancelled
			result.BestPath = t.findBestLeafPath()
			return result, ctx.Err()
		default:
		}

		// Check timeout
		if t.config.Timeout > 0 && time.Since(start) > t.config.Timeout {
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedTimeout
			result.BestPath = t.findBestLeafPath()
			return result, nil
		}

		// Pop highest value node
		node := heap.Pop(pq).(*ThoughtNode)

		// Track depth
		if node.Depth > maxDepth {
			maxDepth = node.Depth
		}

		// Skip pruned nodes
		if node.Status == StatusPruned {
			continue
		}

		// Check depth limit
		if node.Depth >= t.config.MaxDepth {
			continue
		}

		// Branch
		children, err := t.Branch(ctx, node)
		if err != nil {
			continue
		}

		// Evaluate and add to heap
		for _, child := range children {
			if err := t.Evaluate(ctx, child); err != nil {
				continue
			}
			result.NodesExplored++

			// Check for solution
			if child.Status == StatusTerminal {
				result.Solution = child
				result.BestPath = child.Path()
				result.Duration = time.Since(start)
				result.MaxDepthReached = maxDepth
				result.TerminatedBy = TerminatedSolution
				return result, nil
			}

			// Add to heap if not pruned
			if child.Status != StatusPruned {
				heap.Push(pq, child)
			}
		}
	}

	// No solution found
	result.Duration = time.Since(start)
	result.MaxDepthReached = maxDepth
	result.TerminatedBy = TerminatedExhausted
	result.BestPath = t.findBestLeafPath()
	return result, nil
}

// ExploreWithBeam uses beam search - keep top-k nodes at each level.
// Balance between BFS completeness and best-first efficiency.
func (t *ThoughtTree) ExploreWithBeam(ctx context.Context, goal string, beamWidth int) (*ExplorationResult, error) {
	start := time.Now()
	result := &ExplorationResult{}

	if t.root == nil {
		t.Initialize(goal)
	}

	// Initialize root
	if err := t.Evaluate(ctx, t.root); err != nil {
		return nil, fmt.Errorf("evaluate root: %w", err)
	}
	result.NodesExplored++

	if t.root.Status == StatusTerminal {
		result.Solution = t.root
		result.BestPath = t.root.Path()
		result.Duration = time.Since(start)
		result.TerminatedBy = TerminatedSolution
		return result, nil
	}

	// Current beam (nodes at current level)
	beam := []*ThoughtNode{t.root}
	var maxDepth int

	for len(beam) > 0 {
		// Check context
		select {
		case <-ctx.Done():
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedCancelled
			result.BestPath = t.findBestLeafPath()
			return result, ctx.Err()
		default:
		}

		// Check timeout
		if t.config.Timeout > 0 && time.Since(start) > t.config.Timeout {
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedTimeout
			result.BestPath = t.findBestLeafPath()
			return result, nil
		}

		// Check depth
		if beam[0].Depth >= t.config.MaxDepth {
			break
		}

		maxDepth = beam[0].Depth

		// Expand all nodes in current beam
		var candidates []*ThoughtNode
		for _, node := range beam {
			if node.Status == StatusPruned {
				continue
			}

			children, err := t.Branch(ctx, node)
			if err != nil {
				continue
			}

			for _, child := range children {
				if err := t.Evaluate(ctx, child); err != nil {
					continue
				}
				result.NodesExplored++

				// Check for solution
				if child.Status == StatusTerminal {
					result.Solution = child
					result.BestPath = child.Path()
					result.Duration = time.Since(start)
					result.MaxDepthReached = maxDepth
					result.TerminatedBy = TerminatedSolution
					return result, nil
				}

				if child.Status != StatusPruned {
					candidates = append(candidates, child)
				}
			}
		}

		// Select top-k candidates for next beam
		beam = selectTopK(candidates, beamWidth)
	}

	// No solution found
	result.Duration = time.Since(start)
	result.MaxDepthReached = maxDepth
	result.TerminatedBy = TerminatedExhausted
	result.BestPath = t.findBestLeafPath()
	return result, nil
}

// findBestLeafPath finds the path to the highest-value leaf node.
func (t *ThoughtTree) findBestLeafPath() []*ThoughtNode {
	if t.root == nil {
		return nil
	}

	var bestLeaf *ThoughtNode
	var bestValue float64 = -1

	var findBest func(*ThoughtNode)
	findBest = func(node *ThoughtNode) {
		if node.IsLeaf() && node.ValueEstimate > bestValue {
			bestLeaf = node
			bestValue = node.ValueEstimate
		}
		for _, child := range node.Children {
			findBest(child)
		}
	}
	findBest(t.root)

	if bestLeaf == nil {
		return []*ThoughtNode{t.root}
	}
	return bestLeaf.Path()
}

// selectTopK selects the top k nodes by value.
func selectTopK(nodes []*ThoughtNode, k int) []*ThoughtNode {
	if len(nodes) <= k {
		return nodes
	}

	// Build max-heap
	h := &nodeHeap{}
	heap.Init(h)
	for _, node := range nodes {
		heap.Push(h, node)
	}

	// Extract top k
	result := make([]*ThoughtNode, 0, k)
	for i := 0; i < k && h.Len() > 0; i++ {
		result = append(result, heap.Pop(h).(*ThoughtNode))
	}
	return result
}

// nodeHeap implements a max-heap for nodes ordered by value.
type nodeHeap []*ThoughtNode

func (h nodeHeap) Len() int           { return len(h) }
func (h nodeHeap) Less(i, j int) bool { return h[i].ValueEstimate > h[j].ValueEstimate } // max-heap
func (h nodeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *nodeHeap) Push(x any) {
	*h = append(*h, x.(*ThoughtNode))
}

func (h *nodeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// IterativeDeepeningDFS combines DFS memory efficiency with BFS completeness.
// Runs DFS with increasing depth limits.
func (t *ThoughtTree) ExploreWithIDDFS(ctx context.Context, goal string) (*ExplorationResult, error) {
	start := time.Now()
	finalResult := &ExplorationResult{}

	for depth := 1; depth <= t.config.MaxDepth; depth++ {
		// Create a config with current depth limit
		originalMaxDepth := t.config.MaxDepth
		t.config.MaxDepth = depth

		result, err := t.ExploreWithDFS(ctx, goal)

		t.config.MaxDepth = originalMaxDepth

		if err != nil {
			return result, err
		}

		finalResult.NodesExplored += result.NodesExplored
		finalResult.MaxDepthReached = result.MaxDepthReached

		if result.Solution != nil {
			finalResult.Solution = result.Solution
			finalResult.BestPath = result.BestPath
			finalResult.Duration = time.Since(start)
			finalResult.TerminatedBy = TerminatedSolution
			return finalResult, nil
		}

		// Check timeout
		if t.config.Timeout > 0 && time.Since(start) > t.config.Timeout {
			finalResult.Duration = time.Since(start)
			finalResult.TerminatedBy = TerminatedTimeout
			finalResult.BestPath = result.BestPath
			return finalResult, nil
		}
	}

	finalResult.Duration = time.Since(start)
	finalResult.TerminatedBy = TerminatedExhausted
	finalResult.BestPath = t.findBestLeafPath()
	return finalResult, nil
}

// MonteCarloTreeSearch uses MCTS for exploration.
// Balances exploration vs exploitation using UCB1.
func (t *ThoughtTree) ExploreWithMCTS(ctx context.Context, goal string, iterations int) (*ExplorationResult, error) {
	start := time.Now()
	result := &ExplorationResult{}

	if t.root == nil {
		t.Initialize(goal)
	}

	// Initialize root
	if err := t.Evaluate(ctx, t.root); err != nil {
		return nil, fmt.Errorf("evaluate root: %w", err)
	}
	result.NodesExplored++

	// Track visit counts and total values
	visits := make(map[string]int)
	totalValue := make(map[string]float64)
	visits[t.root.ID] = 1
	totalValue[t.root.ID] = t.root.ValueEstimate

	var maxDepth int

	for iter := 0; iter < iterations; iter++ {
		// Check context
		select {
		case <-ctx.Done():
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedCancelled
			result.BestPath = t.findBestLeafPath()
			return result, ctx.Err()
		default:
		}

		// Check timeout
		if t.config.Timeout > 0 && time.Since(start) > t.config.Timeout {
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedTimeout
			result.BestPath = t.findBestLeafPath()
			return result, nil
		}

		// Selection: traverse tree using UCB1
		node := t.root
		for !node.IsLeaf() && node.Depth < t.config.MaxDepth {
			node = selectUCB1(node, visits, totalValue)
		}

		if node.Depth > maxDepth {
			maxDepth = node.Depth
		}

		// Expansion: if not at max depth, expand
		if node.Depth < t.config.MaxDepth && node.Status != StatusPruned {
			children, err := t.Branch(ctx, node)
			if err == nil && len(children) > 0 {
				// Pick first unexplored child
				node = children[0]
			}
		}

		// Simulation: evaluate the node
		if err := t.Evaluate(ctx, node); err != nil {
			continue
		}
		result.NodesExplored++

		// Check for solution
		if node.Status == StatusTerminal {
			result.Solution = node
			result.BestPath = node.Path()
			result.Duration = time.Since(start)
			result.MaxDepthReached = maxDepth
			result.TerminatedBy = TerminatedSolution
			return result, nil
		}

		// Backpropagation: update statistics up the tree
		value := node.ValueEstimate
		for n := node; n != nil; n = n.Parent {
			visits[n.ID]++
			totalValue[n.ID] += value
		}
	}

	result.Duration = time.Since(start)
	result.MaxDepthReached = maxDepth
	result.TerminatedBy = TerminatedExhausted
	result.BestPath = t.findBestLeafPath()
	return result, nil
}

// selectUCB1 selects the child with highest UCB1 score.
func selectUCB1(node *ThoughtNode, visits map[string]int, totalValue map[string]float64) *ThoughtNode {
	if len(node.Children) == 0 {
		return node
	}

	parentVisits := visits[node.ID]
	if parentVisits == 0 {
		parentVisits = 1
	}

	var best *ThoughtNode
	var bestScore float64 = -1

	explorationConstant := 1.41 // sqrt(2)

	for _, child := range node.Children {
		childVisits := visits[child.ID]
		if childVisits == 0 {
			// Unvisited child - prioritize exploration
			return child
		}

		avgValue := totalValue[child.ID] / float64(childVisits)
		exploration := explorationConstant * sqrt(ln(float64(parentVisits))/float64(childVisits))
		score := avgValue + exploration

		if score > bestScore {
			bestScore = score
			best = child
		}
	}

	if best == nil {
		return node.Children[0]
	}
	return best
}

// Simple math helpers to avoid math import for these basic operations
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

func ln(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Taylor series approximation for ln(x) around x=1
	// More accurate for values close to 1
	if x > 0.5 && x < 1.5 {
		y := x - 1
		return y - y*y/2 + y*y*y/3 - y*y*y*y/4
	}
	// For larger values, use the identity ln(x) = 2*ln(sqrt(x))
	if x >= 1.5 {
		return 2 * ln(sqrt(x))
	}
	// For smaller values, use ln(x) = -ln(1/x)
	return -ln(1 / x)
}
