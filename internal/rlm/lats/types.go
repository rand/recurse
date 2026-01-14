// Package lats implements Language Agent Tree Search for intelligent tool orchestration.
//
// LATS combines Monte Carlo Tree Search (MCTS) with LLM-based reasoning to explore
// tool usage strategies, learning from outcomes to improve future decisions.
// Based on Zhou et al., 2023: "Language Agent Tree Search Unifies Reasoning,
// Acting and Planning in Language Models".
package lats

import (
	"math"
	"sync"
	"time"
)

// Node represents a state in the MCTS tree.
type Node struct {
	// ID uniquely identifies this node.
	ID string

	// ParentID is the ID of the parent node (empty for root).
	ParentID string

	// Action is the tool action that led to this node.
	Action *Action

	// State is the agent state after action execution.
	State *AgentState

	// Children are the child nodes.
	Children []*Node

	// Visits is the number of times this node was visited.
	Visits int

	// TotalValue is the sum of backpropagated values.
	TotalValue float64

	// Depth is the level in the tree (root = 0).
	Depth int

	// IsTerminal indicates if this is a terminal state.
	IsTerminal bool

	// CreatedAt is when this node was created.
	CreatedAt time.Time

	mu sync.RWMutex
}

// QValue returns the average value of this node.
func (n *Node) QValue() float64 {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.Visits == 0 {
		return 0
	}
	return n.TotalValue / float64(n.Visits)
}

// UCTValue computes the Upper Confidence Bound for Trees.
// UCT(node) = Q(node) + C * sqrt(ln(parent.visits) / node.visits)
func (n *Node) UCTValue(parentVisits int, explorationConstant float64) float64 {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.Visits == 0 {
		return math.Inf(1) // Unexplored nodes have infinite value
	}

	exploitation := n.TotalValue / float64(n.Visits)
	exploration := explorationConstant * math.Sqrt(math.Log(float64(parentVisits))/float64(n.Visits))
	return exploitation + exploration
}

// Update increments visit count and adds value.
func (n *Node) Update(value float64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Visits++
	n.TotalValue += value
}

// AddChild adds a child node.
func (n *Node) AddChild(child *Node) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Children = append(n.Children, child)
}

// Action represents a tool invocation.
type Action struct {
	// Tool is the tool name (repl, search, file, etc.).
	Tool string

	// Input is the tool input.
	Input string

	// Reasoning explains why this action was chosen.
	Reasoning string
}

// AgentState captures the agent's current context.
type AgentState struct {
	// Query is the original user query.
	Query string

	// Observations are results from previous actions.
	Observations []Observation

	// CurrentContext holds additional context.
	CurrentContext string

	// TokensUsed tracks token consumption.
	TokensUsed int
}

// Clone creates a deep copy of the state.
func (s *AgentState) Clone() *AgentState {
	if s == nil {
		return nil
	}

	clone := &AgentState{
		Query:          s.Query,
		CurrentContext: s.CurrentContext,
		TokensUsed:     s.TokensUsed,
		Observations:   make([]Observation, len(s.Observations)),
	}
	copy(clone.Observations, s.Observations)
	return clone
}

// Observation records the result of an action.
type Observation struct {
	// Action is the action that was executed.
	Action *Action

	// Result is the output from the tool.
	Result string

	// Success indicates if the action succeeded.
	Success bool

	// Tokens is the token count for this observation.
	Tokens int

	// Duration is how long the action took.
	Duration time.Duration
}

// Tree manages the MCTS tree structure.
type Tree struct {
	root  *Node
	nodes map[string]*Node
	mu    sync.RWMutex
}

// NewTree creates a new tree with the given root.
func NewTree(root *Node) *Tree {
	return &Tree{
		root:  root,
		nodes: map[string]*Node{root.ID: root},
	}
}

// Root returns the root node.
func (t *Tree) Root() *Node {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.root
}

// AddNode adds a node to the tree.
func (t *Tree) AddNode(node *Node) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nodes[node.ID] = node
}

// GetNode returns a node by ID.
func (t *Tree) GetNode(id string) *Node {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.nodes[id]
}

// Size returns the number of nodes in the tree.
func (t *Tree) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.nodes)
}

// Stats returns tree statistics.
func (t *Tree) Stats() TreeStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := TreeStats{
		NodesCreated: len(t.nodes),
	}

	var totalChildren int
	var nonLeafNodes int

	for _, node := range t.nodes {
		if node.Depth > stats.MaxDepth {
			stats.MaxDepth = node.Depth
		}
		if len(node.Children) > 0 {
			totalChildren += len(node.Children)
			nonLeafNodes++
		}
	}

	if nonLeafNodes > 0 {
		stats.AvgBranching = float64(totalChildren) / float64(nonLeafNodes)
	}

	return stats
}

// TreeStats contains tree statistics.
type TreeStats struct {
	NodesCreated int
	MaxDepth     int
	AvgBranching float64
}

// Solution represents the result of LATS solving.
type Solution struct {
	// Path is the sequence of nodes from root to solution.
	Path []*Node

	// FinalAnswer is the extracted answer.
	FinalAnswer string

	// TotalTokens is the total token consumption.
	TotalTokens int

	// Stats contains solve statistics.
	Stats SolveStats
}

// SolveStats contains statistics about the solve process.
type SolveStats struct {
	// Iterations is the number of MCTS iterations.
	Iterations int

	// NodesCreated is the total nodes in the tree.
	NodesCreated int

	// MaxDepth is the deepest node reached.
	MaxDepth int

	// AvgBranching is the average branching factor.
	AvgBranching float64

	// Duration is the total solve time.
	Duration time.Duration

	// TerminatedBy indicates why solving stopped.
	TerminatedBy TerminationReason
}

// TerminationReason indicates why LATS stopped.
type TerminationReason string

const (
	TerminatedSolution  TerminationReason = "solution_found"
	TerminatedBudget    TerminationReason = "budget_exhausted"
	TerminatedMaxIter   TerminationReason = "max_iterations"
	TerminatedTimeout   TerminationReason = "timeout"
	TerminatedExhausted TerminationReason = "tree_exhausted"
)

// Config configures the LATS controller.
type Config struct {
	// MaxIterations is the maximum MCTS iterations.
	MaxIterations int

	// MaxDepth is the maximum action sequence length.
	MaxDepth int

	// ExplorationConstant is the UCB1 exploration weight (typically sqrt(2)).
	ExplorationConstant float64

	// TokenBudget is the total token budget.
	TokenBudget int

	// SimulationDepth is the rollout depth.
	SimulationDepth int

	// Timeout limits solve duration.
	Timeout time.Duration

	// ValueDecay is the decay factor for backpropagation (0-1).
	ValueDecay float64
}

// DefaultConfig returns default configuration.
func DefaultConfig() Config {
	return Config{
		MaxIterations:       50,
		MaxDepth:            10,
		ExplorationConstant: 1.414, // sqrt(2)
		TokenBudget:         100000,
		SimulationDepth:     3,
		Timeout:             3 * time.Minute,
		ValueDecay:          0.95,
	}
}

// Budget tracks token consumption.
type Budget struct {
	total     int
	used      int
	mu        sync.Mutex
}

// NewBudget creates a new budget tracker.
func NewBudget(total int) *Budget {
	return &Budget{total: total}
}

// Deduct subtracts tokens from the budget.
func (b *Budget) Deduct(tokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used += tokens
}

// Remaining returns remaining tokens.
func (b *Budget) Remaining() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total - b.used
}

// Exhausted returns true if budget is exhausted.
func (b *Budget) Exhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used >= b.total
}

// Used returns tokens used.
func (b *Budget) Used() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}
