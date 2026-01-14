// Package tot implements Tree of Thoughts for deliberate reasoning with exploration.
//
// Based on "Tree of Thoughts: Deliberate Problem Solving with Large Language Models"
// (NeurIPS 2023), which showed 4-74% improvement on complex reasoning tasks through
// deliberate exploration with backtracking.
package tot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Strategy defines the exploration strategy for the thought tree.
type Strategy string

const (
	// StrategyBFS explores breadth-first (level by level).
	StrategyBFS Strategy = "bfs"

	// StrategyDFS explores depth-first (follow one path deeply).
	StrategyDFS Strategy = "dfs"

	// StrategyBestFirst explores highest-value nodes first.
	StrategyBestFirst Strategy = "best_first"
)

// NodeStatus represents the status of a thought node.
type NodeStatus int

const (
	// StatusPending indicates the node has not been evaluated.
	StatusPending NodeStatus = iota

	// StatusExpanded indicates child thoughts have been generated.
	StatusExpanded

	// StatusEvaluated indicates the node has been scored.
	StatusEvaluated

	// StatusTerminal indicates this is a terminal/solution node.
	StatusTerminal

	// StatusPruned indicates the node was pruned due to low value.
	StatusPruned
)

func (s NodeStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusExpanded:
		return "expanded"
	case StatusEvaluated:
		return "evaluated"
	case StatusTerminal:
		return "terminal"
	case StatusPruned:
		return "pruned"
	default:
		return "unknown"
	}
}

// ThoughtNode represents a single thought in the reasoning tree.
type ThoughtNode struct {
	// ID uniquely identifies this node.
	ID string

	// Thought is the textual content of this reasoning step.
	Thought string

	// State holds any structured state at this point.
	State map[string]any

	// ValueEstimate is the evaluated quality score (0-1).
	ValueEstimate float64

	// Confidence is how confident the evaluation is (0-1).
	Confidence float64

	// Status indicates the current node status.
	Status NodeStatus

	// Depth is the level in the tree (root = 0).
	Depth int

	// Parent points to the parent node (nil for root).
	Parent *ThoughtNode

	// Children are the child thought nodes.
	Children []*ThoughtNode

	// Metadata holds additional information.
	Metadata map[string]any

	// CreatedAt is when this node was created.
	CreatedAt time.Time

	// EvaluatedAt is when this node was evaluated.
	EvaluatedAt time.Time

	mu sync.RWMutex
}

// NewThoughtNode creates a new thought node.
func NewThoughtNode(id, thought string, parent *ThoughtNode) *ThoughtNode {
	depth := 0
	if parent != nil {
		depth = parent.Depth + 1
	}

	return &ThoughtNode{
		ID:        id,
		Thought:   thought,
		State:     make(map[string]any),
		Status:    StatusPending,
		Depth:     depth,
		Parent:    parent,
		Children:  make([]*ThoughtNode, 0),
		Metadata:  make(map[string]any),
		CreatedAt: time.Now(),
	}
}

// AddChild adds a child node.
func (n *ThoughtNode) AddChild(child *ThoughtNode) {
	n.mu.Lock()
	defer n.mu.Unlock()
	child.Parent = n
	child.Depth = n.Depth + 1
	n.Children = append(n.Children, child)
}

// SetValue sets the value estimate and confidence.
func (n *ThoughtNode) SetValue(value, confidence float64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ValueEstimate = value
	n.Confidence = confidence
	n.Status = StatusEvaluated
	n.EvaluatedAt = time.Now()
}

// MarkTerminal marks this node as a terminal/solution node.
func (n *ThoughtNode) MarkTerminal() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Status = StatusTerminal
}

// MarkPruned marks this node as pruned.
func (n *ThoughtNode) MarkPruned() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Status = StatusPruned
}

// IsLeaf returns true if this node has no children.
func (n *ThoughtNode) IsLeaf() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.Children) == 0
}

// Path returns the path from root to this node.
func (n *ThoughtNode) Path() []*ThoughtNode {
	var path []*ThoughtNode
	current := n
	for current != nil {
		path = append([]*ThoughtNode{current}, path...)
		current = current.Parent
	}
	return path
}

// PathThoughts returns the thought strings from root to this node.
func (n *ThoughtNode) PathThoughts() []string {
	path := n.Path()
	thoughts := make([]string, len(path))
	for i, node := range path {
		thoughts[i] = node.Thought
	}
	return thoughts
}

// BestChild returns the child with highest value estimate.
func (n *ThoughtNode) BestChild() *ThoughtNode {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if len(n.Children) == 0 {
		return nil
	}

	best := n.Children[0]
	for _, child := range n.Children[1:] {
		if child.ValueEstimate > best.ValueEstimate {
			best = child
		}
	}
	return best
}

// ToTConfig configures the thought tree exploration.
type ToTConfig struct {
	// MaxBranches is the maximum children per node.
	MaxBranches int

	// MaxDepth is the maximum tree depth.
	MaxDepth int

	// ValueThreshold is the minimum value to continue exploring.
	ValueThreshold float64

	// Strategy is the exploration strategy.
	Strategy Strategy

	// MaxNodes limits total nodes in the tree.
	MaxNodes int

	// Timeout limits exploration time.
	Timeout time.Duration

	// EvaluateBeforeExpand requires evaluation before branching.
	EvaluateBeforeExpand bool
}

// DefaultToTConfig returns default configuration.
func DefaultToTConfig() ToTConfig {
	return ToTConfig{
		MaxBranches:          3,
		MaxDepth:             5,
		ValueThreshold:       0.3,
		Strategy:             StrategyBestFirst,
		MaxNodes:             100,
		Timeout:              5 * time.Minute,
		EvaluateBeforeExpand: true,
	}
}

// ThoughtTree manages the tree of thoughts.
type ThoughtTree struct {
	config    ToTConfig
	root      *ThoughtNode
	nodeCount int64
	evaluator Evaluator
	generator Generator

	// Metrics
	expansions  int64
	evaluations int64
	backtracks  int64

	mu sync.RWMutex
}

// NewThoughtTree creates a new thought tree.
func NewThoughtTree(config ToTConfig, evaluator Evaluator, generator Generator) *ThoughtTree {
	return &ThoughtTree{
		config:    config,
		evaluator: evaluator,
		generator: generator,
	}
}

// Initialize sets the root thought.
func (t *ThoughtTree) Initialize(problem string) *ThoughtNode {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.root = NewThoughtNode(t.generateID(), problem, nil)
	t.nodeCount = 1
	return t.root
}

// Root returns the root node.
func (t *ThoughtTree) Root() *ThoughtNode {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.root
}

// Branch generates child thoughts for a node.
func (t *ThoughtTree) Branch(ctx context.Context, node *ThoughtNode) ([]*ThoughtNode, error) {
	if node == nil {
		return nil, errors.New("cannot branch from nil node")
	}

	if node.Depth >= t.config.MaxDepth {
		return nil, nil // Max depth reached
	}

	if t.config.EvaluateBeforeExpand && node.Status == StatusPending {
		return nil, errors.New("node must be evaluated before branching")
	}

	// Check node limit
	if atomic.LoadInt64(&t.nodeCount) >= int64(t.config.MaxNodes) {
		return nil, errors.New("max nodes reached")
	}

	// Generate thoughts using the generator
	thoughts, err := t.generator.GenerateThoughts(ctx, node, t.config.MaxBranches)
	if err != nil {
		return nil, fmt.Errorf("generate thoughts: %w", err)
	}

	// Create child nodes
	children := make([]*ThoughtNode, 0, len(thoughts))
	for _, thought := range thoughts {
		child := NewThoughtNode(t.generateID(), thought, node)
		node.AddChild(child)
		children = append(children, child)
		atomic.AddInt64(&t.nodeCount, 1)
	}

	node.mu.Lock()
	node.Status = StatusExpanded
	node.mu.Unlock()

	atomic.AddInt64(&t.expansions, 1)

	return children, nil
}

// Evaluate scores a node using the evaluator.
func (t *ThoughtTree) Evaluate(ctx context.Context, node *ThoughtNode) error {
	if node == nil {
		return errors.New("cannot evaluate nil node")
	}

	// Get evaluation from evaluator
	result, err := t.evaluator.EvaluateThought(ctx, node)
	if err != nil {
		return fmt.Errorf("evaluate thought: %w", err)
	}

	node.SetValue(result.Value, result.Confidence)

	// Check for terminal state
	if result.IsTerminal {
		node.MarkTerminal()
	}

	// Check for pruning
	if result.Value < t.config.ValueThreshold && !result.IsTerminal {
		node.MarkPruned()
	}

	atomic.AddInt64(&t.evaluations, 1)

	return nil
}

// Backtrack returns to the parent node for alternative exploration.
func (t *ThoughtTree) Backtrack(node *ThoughtNode) *ThoughtNode {
	if node == nil || node.Parent == nil {
		return nil
	}

	atomic.AddInt64(&t.backtracks, 1)

	return node.Parent
}

// FindBestPath returns the path to the highest-value terminal node.
func (t *ThoughtTree) FindBestPath() []*ThoughtNode {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.root == nil {
		return nil
	}

	var bestTerminal *ThoughtNode
	var bestValue float64 = -1

	// DFS to find best terminal
	var search func(*ThoughtNode)
	search = func(node *ThoughtNode) {
		if node.Status == StatusTerminal && node.ValueEstimate > bestValue {
			bestTerminal = node
			bestValue = node.ValueEstimate
		}
		for _, child := range node.Children {
			search(child)
		}
	}
	search(t.root)

	if bestTerminal == nil {
		return nil
	}

	return bestTerminal.Path()
}

// AllTerminals returns all terminal nodes.
func (t *ThoughtTree) AllTerminals() []*ThoughtNode {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.root == nil {
		return nil
	}

	var terminals []*ThoughtNode
	var collect func(*ThoughtNode)
	collect = func(node *ThoughtNode) {
		if node.Status == StatusTerminal {
			terminals = append(terminals, node)
		}
		for _, child := range node.Children {
			collect(child)
		}
	}
	collect(t.root)

	return terminals
}

// NodeCount returns the total number of nodes.
func (t *ThoughtTree) NodeCount() int64 {
	return atomic.LoadInt64(&t.nodeCount)
}

// Metrics returns exploration metrics.
func (t *ThoughtTree) Metrics() TreeMetrics {
	return TreeMetrics{
		NodeCount:   atomic.LoadInt64(&t.nodeCount),
		Expansions:  atomic.LoadInt64(&t.expansions),
		Evaluations: atomic.LoadInt64(&t.evaluations),
		Backtracks:  atomic.LoadInt64(&t.backtracks),
	}
}

// TreeMetrics contains tree exploration metrics.
type TreeMetrics struct {
	NodeCount   int64
	Expansions  int64
	Evaluations int64
	Backtracks  int64
}

// generateID generates a unique node ID.
func (t *ThoughtTree) generateID() string {
	return fmt.Sprintf("node-%d", atomic.LoadInt64(&t.nodeCount))
}

// Errors for tree operations.
var (
	ErrMaxDepthReached = errors.New("maximum depth reached")
	ErrMaxNodesReached = errors.New("maximum nodes reached")
	ErrNodePruned      = errors.New("node was pruned")
	ErrNoSolution      = errors.New("no solution found")
	ErrTimeout         = errors.New("exploration timeout")
)
