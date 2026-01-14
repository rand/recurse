# LATS Tool Orchestration Design

> Design document for `recurse-0ef`: [SPEC] LATS Tool Orchestration Design

## Overview

This document specifies the integration of Language Agent Tree Search (LATS) for intelligent tool orchestration in the RLM system. LATS combines Monte Carlo Tree Search (MCTS) with LLM-based reasoning to explore tool usage strategies, learning from outcomes to improve future decisions.

## Research Background

### LATS (Zhou et al., 2023)

LATS applies MCTS to LLM agents:
1. **Selection**: Use UCB1 to balance exploration vs exploitation
2. **Expansion**: Generate candidate actions via LLM
3. **Simulation**: Execute actions and observe outcomes
4. **Backpropagation**: Update value estimates based on results

**Key insight**: Tool execution provides ground-truth feedback, enabling learning within a single session.

### Application to RLM

The RLM system uses tools (REPL, file operations, search) but currently lacks:
- Adaptive tool selection based on task type
- Learning from tool execution failures
- Strategic multi-tool planning
- Exploration of alternative tool sequences

## Problem Statement

### Current Tool Usage

```
Query → Meta-Controller → Select Tool → Execute → Return
```

**Issues**:
- No learning from failed tool calls
- Single-path execution (no alternatives tried)
- Tool selection is heuristic-based
- No long-term strategy for multi-step tasks

### LATS Enhancement

```
Query → LATS Controller →
    Select (UCB1) → Node with highest UCT value
    Expand → Generate candidate tool actions
    Simulate → Execute tool, observe result
    Backpropagate → Update Q-values
    → Repeat until solution or budget exhausted
```

## Design Goals

1. **Intelligent selection**: UCB1-based tool choice balancing exploration/exploitation
2. **Outcome learning**: Update value estimates from execution results
3. **Multi-step planning**: Build tool execution trees
4. **Budget efficiency**: Prune unpromising branches early
5. **Integration**: Work with existing tool infrastructure

## Core Types

### Action Node

```go
// internal/rlm/lats/node.go

// Node represents a state in the MCTS tree.
type Node struct {
    ID          string
    ParentID    string
    Action      *Action       // Tool action that led to this node
    State       *AgentState   // State after action execution
    Children    []*Node
    Visits      int
    TotalValue  float64       // Sum of backpropagated values
    Depth       int
    IsTerminal  bool
}

// Action represents a tool invocation.
type Action struct {
    Tool       string         // Tool name (repl, search, file, etc.)
    Input      string         // Tool input
    Reasoning  string         // LLM's reasoning for this action
}

// AgentState captures the agent's current context.
type AgentState struct {
    Query          string
    Observations   []Observation  // Results from previous actions
    CurrentContext string
    TokensUsed     int
}

type Observation struct {
    Action  *Action
    Result  string
    Success bool
    Tokens  int
}

// QValue returns the average value of this node.
func (n *Node) QValue() float64 {
    if n.Visits == 0 {
        return 0
    }
    return n.TotalValue / float64(n.Visits)
}

// UCTValue computes the Upper Confidence Bound for Trees.
func (n *Node) UCTValue(parentVisits int, explorationConstant float64) float64 {
    if n.Visits == 0 {
        return math.Inf(1) // Unexplored nodes have infinite value
    }
    exploitation := n.QValue()
    exploration := explorationConstant * math.Sqrt(math.Log(float64(parentVisits))/float64(n.Visits))
    return exploitation + exploration
}
```

### MCTS Controller

```go
// internal/rlm/lats/controller.go

// Controller implements LATS for tool orchestration.
type Controller struct {
    expander   Expander
    simulator  Simulator
    valuator   Valuator
    tools      ToolRegistry
    config     Config
}

type Config struct {
    MaxIterations       int           // MCTS iterations
    MaxDepth            int           // Maximum action sequence length
    ExplorationConstant float64       // UCB1 exploration weight (typically sqrt(2))
    TokenBudget         int           // Total token budget
    SimulationDepth     int           // Rollout depth
    Timeout             time.Duration
}

func DefaultConfig() Config {
    return Config{
        MaxIterations:       50,
        MaxDepth:            10,
        ExplorationConstant: 1.414, // sqrt(2)
        TokenBudget:         100000,
        SimulationDepth:     3,
        Timeout:             3 * time.Minute,
    }
}

// Solve runs LATS to find an optimal tool sequence.
func (c *Controller) Solve(ctx context.Context, query string) (*Solution, error) {
    ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
    defer cancel()

    // Initialize root
    root := &Node{
        ID:    generateID(),
        State: &AgentState{Query: query},
        Depth: 0,
    }

    tree := &Tree{root: root, nodes: map[string]*Node{root.ID: root}}
    budget := NewBudget(c.config.TokenBudget)

    for iter := 0; iter < c.config.MaxIterations; iter++ {
        if budget.Exhausted() {
            break
        }

        // Selection: traverse to promising leaf
        leaf := c.select(tree, root)

        // Check if terminal
        if leaf.IsTerminal {
            if c.isSolution(leaf) {
                return c.extractSolution(tree, leaf), nil
            }
            continue
        }

        // Expansion: generate candidate actions
        children, err := c.expand(ctx, leaf)
        if err != nil {
            continue
        }

        for _, child := range children {
            tree.AddNode(child)
            leaf.Children = append(leaf.Children, child)
        }

        // Simulation: evaluate one child
        if len(children) > 0 {
            child := children[0] // Could also be random
            value, tokens := c.simulate(ctx, child)
            budget.Deduct(tokens)

            // Backpropagation: update values up the tree
            c.backpropagate(child, value)
        }
    }

    // Return best solution found
    return c.findBestSolution(tree), nil
}
```

### Selection Phase

```go
// internal/rlm/lats/selection.go

func (c *Controller) select(tree *Tree, root *Node) *Node {
    current := root

    for len(current.Children) > 0 && !current.IsTerminal {
        // Select child with highest UCT value
        var best *Node
        var bestUCT float64 = math.Inf(-1)

        for _, child := range current.Children {
            uct := child.UCTValue(current.Visits, c.config.ExplorationConstant)
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
```

### Expansion Phase

```go
// internal/rlm/lats/expansion.go

// Expander generates candidate actions.
type Expander interface {
    Expand(ctx context.Context, node *Node) ([]*Node, error)
}

type LLMExpander struct {
    client LLMClient
    tools  ToolRegistry
}

func (e *LLMExpander) Expand(ctx context.Context, node *Node) ([]*Node, error) {
    prompt := e.buildExpansionPrompt(node)

    response, _, err := e.client.Complete(ctx, prompt)
    if err != nil {
        return nil, err
    }

    actions := e.parseActions(response)
    children := make([]*Node, 0, len(actions))

    for _, action := range actions {
        // Validate action uses available tool
        if !e.tools.Has(action.Tool) {
            continue
        }

        child := &Node{
            ID:       generateID(),
            ParentID: node.ID,
            Action:   action,
            State:    node.State.Clone(),
            Depth:    node.Depth + 1,
        }
        children = append(children, child)
    }

    return children, nil
}

func (e *LLMExpander) buildExpansionPrompt(node *Node) string {
    var history strings.Builder
    for _, obs := range node.State.Observations {
        history.WriteString(fmt.Sprintf("Action: %s(%s)\nResult: %s\n\n",
            obs.Action.Tool, obs.Action.Input, truncate(obs.Result, 500)))
    }

    return fmt.Sprintf(`You are solving this task: %s

Previous actions and results:
%s

Available tools:
%s

Generate 3-5 different next actions to try. For each action, explain your reasoning.

Format:
[Action 1]
Tool: <tool_name>
Input: <tool_input>
Reasoning: <why this action>

[Action 2]
...`, node.State.Query, history.String(), e.tools.Describe())
}
```

### Simulation Phase

```go
// internal/rlm/lats/simulation.go

// Simulator executes actions and returns values.
type Simulator interface {
    Simulate(ctx context.Context, node *Node) (float64, int, error)
}

type RealSimulator struct {
    tools    ToolRegistry
    valuator Valuator
}

// Simulate executes the action and evaluates the result.
func (s *RealSimulator) Simulate(
    ctx context.Context,
    node *Node,
) (float64, int, error) {
    // Execute the tool action
    result, err := s.tools.Execute(ctx, node.Action.Tool, node.Action.Input)

    observation := Observation{
        Action:  node.Action,
        Success: err == nil,
    }

    if err != nil {
        observation.Result = fmt.Sprintf("Error: %v", err)
    } else {
        observation.Result = result.Output
        observation.Tokens = result.Tokens
    }

    // Update node state
    node.State.Observations = append(node.State.Observations, observation)
    node.State.TokensUsed += observation.Tokens

    // Check for terminal state
    node.IsTerminal = s.isTerminal(node)

    // Evaluate state value
    value, err := s.valuator.Value(ctx, node)
    if err != nil {
        // Use heuristic on error
        if observation.Success {
            value = 0.6
        } else {
            value = 0.2
        }
    }

    return value, observation.Tokens, nil
}

func (s *RealSimulator) isTerminal(node *Node) bool {
    // Terminal if answer found or max depth reached
    if node.Depth >= 10 {
        return true
    }

    // Check if last observation indicates completion
    if len(node.State.Observations) > 0 {
        last := node.State.Observations[len(node.State.Observations)-1]
        return strings.Contains(last.Result, "FINAL_ANSWER") ||
               strings.Contains(last.Result, "TASK_COMPLETE")
    }

    return false
}
```

### Valuation

```go
// internal/rlm/lats/valuator.go

// Valuator estimates the value of a state.
type Valuator interface {
    Value(ctx context.Context, node *Node) (float64, error)
}

type LLMValuator struct {
    client LLMClient
}

func (v *LLMValuator) Value(ctx context.Context, node *Node) (float64, error) {
    prompt := v.buildValuePrompt(node)

    response, _, err := v.client.Complete(ctx, prompt)
    if err != nil {
        return 0, err
    }

    return v.parseValue(response), nil
}

func (v *LLMValuator) buildValuePrompt(node *Node) string {
    var history strings.Builder
    for _, obs := range node.State.Observations {
        history.WriteString(fmt.Sprintf("- %s: %s\n",
            obs.Action.Tool, summarize(obs.Result, 100)))
    }

    return fmt.Sprintf(`Task: %s

Actions taken:
%s

Rate the current progress toward solving the task on a scale of 0.0 to 1.0:
- 0.0: No progress, wrong direction
- 0.5: Some progress, unclear if right path
- 1.0: Task appears solved

Provide a single number between 0.0 and 1.0:`, node.State.Query, history.String())
}

// HeuristicValuator uses rules instead of LLM.
type HeuristicValuator struct {
    successBonus   float64
    failurePenalty float64
    depthPenalty   float64
}

func (v *HeuristicValuator) Value(ctx context.Context, node *Node) (float64, error) {
    value := 0.5 // Neutral baseline

    // Reward successful actions
    for _, obs := range node.State.Observations {
        if obs.Success {
            value += v.successBonus
        } else {
            value -= v.failurePenalty
        }
    }

    // Penalize deep searches
    value -= float64(node.Depth) * v.depthPenalty

    // Clamp to [0, 1]
    return math.Max(0, math.Min(1, value)), nil
}
```

### Backpropagation

```go
// internal/rlm/lats/backprop.go

func (c *Controller) backpropagate(node *Node, value float64) {
    current := node
    for current != nil {
        current.Visits++
        current.TotalValue += value

        // Decay value as we go up (optional)
        value *= 0.95

        if current.ParentID == "" {
            break
        }
        current = c.tree.nodes[current.ParentID]
    }
}
```

## Tool Registry

### Tool Interface

```go
// internal/rlm/lats/tools.go

type Tool interface {
    Name() string
    Description() string
    Execute(ctx context.Context, input string) (*ToolResult, error)
}

type ToolResult struct {
    Output    string
    Tokens    int
    Success   bool
    Metadata  map[string]any
}

type ToolRegistry struct {
    tools map[string]Tool
}

func (r *ToolRegistry) Register(tool Tool) {
    r.tools[tool.Name()] = tool
}

func (r *ToolRegistry) Execute(ctx context.Context, name, input string) (*ToolResult, error) {
    tool, ok := r.tools[name]
    if !ok {
        return nil, fmt.Errorf("unknown tool: %s", name)
    }
    return tool.Execute(ctx, input)
}

func (r *ToolRegistry) Describe() string {
    var desc strings.Builder
    for name, tool := range r.tools {
        desc.WriteString(fmt.Sprintf("- %s: %s\n", name, tool.Description()))
    }
    return desc.String()
}
```

### Built-in Tools

```go
// REPL tool for code execution
type REPLTool struct {
    repl *repl.Manager
}

func (t *REPLTool) Name() string { return "repl" }
func (t *REPLTool) Description() string {
    return "Execute Python code and return the result"
}

func (t *REPLTool) Execute(ctx context.Context, input string) (*ToolResult, error) {
    result, err := t.repl.Execute(ctx, input)
    if err != nil {
        return &ToolResult{Output: err.Error(), Success: false}, nil
    }
    return &ToolResult{Output: result.Output, Tokens: result.Tokens, Success: true}, nil
}

// Search tool for information retrieval
type SearchTool struct {
    memory *hypergraph.Store
}

func (t *SearchTool) Name() string { return "search" }
func (t *SearchTool) Description() string {
    return "Search memory for relevant information"
}

func (t *SearchTool) Execute(ctx context.Context, input string) (*ToolResult, error) {
    results, err := t.memory.Search(ctx, input, hypergraph.SearchOptions{Limit: 5})
    if err != nil {
        return &ToolResult{Output: err.Error(), Success: false}, nil
    }

    var output strings.Builder
    for _, r := range results {
        output.WriteString(fmt.Sprintf("- %s (score: %.2f)\n", r.Node.Content, r.Score))
    }
    return &ToolResult{Output: output.String(), Success: true}, nil
}
```

## Integration with RLM

### Controller Integration

```go
// internal/rlm/controller.go

func (c *Controller) orchestrate(ctx context.Context, state meta.State, parentID string) (string, int, error) {
    decision, err := c.metaController.Decide(ctx, state)
    if err != nil {
        return "", 0, err
    }

    // Use LATS for complex tool-use tasks
    if decision.Mode == ModeLATS {
        return c.executeWithLATS(ctx, state)
    }

    // ... existing orchestration logic ...
}

func (c *Controller) executeWithLATS(ctx context.Context, state meta.State) (string, int, error) {
    latsController := lats.NewController(
        lats.NewLLMExpander(c.llmClient, c.tools),
        lats.NewRealSimulator(c.tools, lats.NewLLMValuator(c.llmClient)),
        lats.NewLLMValuator(c.llmClient),
        c.tools,
        lats.DefaultConfig(),
    )

    solution, err := latsController.Solve(ctx, state.Query)
    if err != nil {
        return "", 0, fmt.Errorf("LATS solve: %w", err)
    }

    if solution == nil {
        return "", 0, errors.New("no solution found")
    }

    // Log action sequence to memory
    c.storeActionTrace(ctx, solution)

    return solution.FinalAnswer, solution.TotalTokens, nil
}
```

### Solution Type

```go
// internal/rlm/lats/solution.go

type Solution struct {
    Path        []*Node
    FinalAnswer string
    TotalTokens int
    Stats       SolveStats
}

type SolveStats struct {
    Iterations   int
    NodesCreated int
    MaxDepth     int
    AvgBranching float64
}

func (c *Controller) extractSolution(tree *Tree, terminal *Node) *Solution {
    path := c.getPath(tree, terminal)

    // Extract final answer from last observation
    var answer string
    if len(terminal.State.Observations) > 0 {
        last := terminal.State.Observations[len(terminal.State.Observations)-1]
        answer = last.Result
    }

    return &Solution{
        Path:        path,
        FinalAnswer: answer,
        TotalTokens: terminal.State.TokensUsed,
        Stats:       tree.Stats(),
    }
}
```

## Observability

### Metrics

```go
var (
    latsIterations = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_lats_iterations",
            Help:    "MCTS iterations per solve",
            Buckets: prometheus.LinearBuckets(5, 10, 10),
        },
    )

    latsTreeSize = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_lats_tree_size",
            Help:    "Nodes in MCTS tree",
            Buckets: prometheus.ExponentialBuckets(1, 2, 10),
        },
    )

    latsToolCalls = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_lats_tool_calls_total",
            Help: "Tool calls during LATS",
        },
        []string{"tool", "success"},
    )

    latsUCTValues = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_lats_uct_values",
            Help:    "Distribution of UCT values at selection",
            Buckets: prometheus.LinearBuckets(0, 0.2, 10),
        },
    )
)
```

## Testing Strategy

### Unit Tests

```go
func TestNode_UCTValue(t *testing.T) {
    tests := []struct {
        name          string
        visits        int
        totalValue    float64
        parentVisits  int
        exploration   float64
        wantInfinite  bool
        wantApprox    float64
    }{
        {
            name:         "unexplored node",
            visits:       0,
            parentVisits: 10,
            wantInfinite: true,
        },
        {
            name:         "balanced node",
            visits:       5,
            totalValue:   3.0,
            parentVisits: 20,
            exploration:  1.414,
            wantApprox:   0.6 + 1.414*math.Sqrt(math.Log(20)/5),
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            node := &Node{Visits: tt.visits, TotalValue: tt.totalValue}
            uct := node.UCTValue(tt.parentVisits, tt.exploration)

            if tt.wantInfinite {
                assert.True(t, math.IsInf(uct, 1))
            } else {
                assert.InDelta(t, tt.wantApprox, uct, 0.01)
            }
        })
    }
}

func TestController_Selection(t *testing.T) {
    ctrl := &Controller{config: Config{ExplorationConstant: 1.414}}

    // Build tree with varying visit counts
    root := &Node{ID: "root", Visits: 10}
    child1 := &Node{ID: "c1", ParentID: "root", Visits: 5, TotalValue: 2.5}
    child2 := &Node{ID: "c2", ParentID: "root", Visits: 3, TotalValue: 2.1}
    child3 := &Node{ID: "c3", ParentID: "root", Visits: 0} // Unexplored

    root.Children = []*Node{child1, child2, child3}
    tree := &Tree{root: root, nodes: map[string]*Node{
        "root": root, "c1": child1, "c2": child2, "c3": child3,
    }}

    selected := ctrl.select(tree, root)

    // Should select unexplored node (infinite UCT)
    assert.Equal(t, "c3", selected.ID)
}
```

### Integration Tests

```go
func TestLATS_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    tools := NewToolRegistry()
    tools.Register(NewREPLTool(newTestREPL(t)))
    tools.Register(NewSearchTool(newTestMemory(t)))

    ctrl := NewController(
        NewLLMExpander(newTestLLMClient(t), tools),
        NewRealSimulator(tools, NewHeuristicValuator()),
        NewHeuristicValuator(),
        tools,
        Config{
            MaxIterations: 20,
            MaxDepth:      5,
            Timeout:       30 * time.Second,
        },
    )

    solution, err := ctrl.Solve(context.Background(),
        "Calculate the sum of the first 10 prime numbers")

    require.NoError(t, err)
    assert.NotNil(t, solution)
    assert.Contains(t, solution.FinalAnswer, "129") // Sum of first 10 primes
}
```

## Success Criteria

1. **Tool efficiency**: 30% fewer tool calls for complex tasks
2. **Exploration quality**: UCT selection outperforms random by 40%
3. **Learning**: Value estimates improve over iterations
4. **Latency**: <3x overhead vs greedy tool selection
5. **Integration**: Seamless with existing RLM tools

## Appendix: UCB1 Formula

```
UCT(node) = Q(node) + C * sqrt(ln(parent.visits) / node.visits)

Where:
- Q(node) = average value of node
- C = exploration constant (typically sqrt(2))
- parent.visits = times parent was visited
- node.visits = times node was visited
```

For unexplored nodes (visits=0), UCT is infinite, ensuring all children are tried at least once.
