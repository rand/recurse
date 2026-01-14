# Tree of Thoughts Integration Design

> Design document for `recurse-vst`: [SPEC] Tree of Thoughts Integration Design

## Overview

This document specifies the integration of Tree of Thoughts (ToT) reasoning into the RLM system. ToT enables deliberate, multi-path exploration of solution spaces with explicit backtracking, enabling the system to recover from reasoning dead-ends and find optimal solutions through structured search.

## Research Background

### Tree of Thoughts (Yao et al., 2023)

ToT extends chain-of-thought prompting by:
1. **Decomposing** problems into intermediate thought steps
2. **Generating** multiple candidate thoughts at each step
3. **Evaluating** thoughts using state valuations
4. **Searching** the tree via BFS or DFS with backtracking

**Key insight**: LLMs can self-evaluate intermediate reasoning, enabling tree search without external rewards.

### Application to RLM

The RLM system already decomposes tasks. ToT adds:
- Multiple candidate approaches per decomposition
- Self-evaluation of intermediate results
- Backtracking when approaches fail
- Structured exploration of the solution space

## Problem Statement

### Current Limitations

The current decomposition is single-path:

```
Task → Decompose → [Chunk1, Chunk2, Chunk3] → Execute → Synthesize
```

**Issues**:
- No recovery from bad decomposition choices
- Single approach per subtask
- No exploration of alternative solutions
- Failures propagate without mitigation

### ToT Enhancement

```
Task → Generate Thoughts → Evaluate → Select Best
         ↓
    [Thought1, Thought2, Thought3]
         ↓ (evaluate each)
    [0.8, 0.3, 0.6] → Select Thought1
         ↓
    Expand Thought1 → [Child1, Child2]
         ↓ (if all fail)
    Backtrack → Try Thought3
```

## Design Goals

1. **Multi-path exploration**: Generate and evaluate multiple approaches
2. **Self-evaluation**: Use LLM to score intermediate thoughts
3. **Efficient search**: Prune unpromising branches early
4. **Budget-aware**: Limit exploration based on token budget
5. **Integration**: Work within existing RLM orchestration

## Core Types

### Thought Representation

```go
// internal/rlm/tot/thought.go

// Thought represents a node in the thought tree.
type Thought struct {
    ID        string
    ParentID  string
    Content   string      // The reasoning/approach
    State     ThoughtState
    Value     float64     // Self-evaluated value [0, 1]
    Depth     int
    Children  []string    // Child thought IDs
    Metadata  map[string]any
}

type ThoughtState int

const (
    ThoughtStatePending ThoughtState = iota
    ThoughtStateExpanded
    ThoughtStateEvaluated
    ThoughtStatePruned
    ThoughtStateSucceeded
    ThoughtStateFailed
)

// ThoughtTree manages the full exploration tree.
type ThoughtTree struct {
    root     *Thought
    thoughts map[string]*Thought
    mu       sync.RWMutex
}

func NewThoughtTree(rootContent string) *ThoughtTree {
    root := &Thought{
        ID:      generateID(),
        Content: rootContent,
        State:   ThoughtStatePending,
        Depth:   0,
    }
    return &ThoughtTree{
        root:     root,
        thoughts: map[string]*Thought{root.ID: root},
    }
}
```

### Search Strategy

```go
// internal/rlm/tot/search.go

// SearchStrategy defines how to explore the thought tree.
type SearchStrategy interface {
    // NextThought returns the next thought to expand.
    NextThought(tree *ThoughtTree) *Thought

    // ShouldPrune decides if a thought should be pruned.
    ShouldPrune(thought *Thought, budget Budget) bool

    // ShouldBacktrack decides if we should backtrack.
    ShouldBacktrack(thought *Thought) bool
}

// BFSStrategy explores breadth-first with value-based ordering.
type BFSStrategy struct {
    minValue      float64 // Minimum value to continue expanding
    maxBreadth    int     // Maximum children per thought
    valueDecay    float64 // Decay factor for depth penalty
}

func (s *BFSStrategy) NextThought(tree *ThoughtTree) *Thought {
    // Get all expandable thoughts
    var candidates []*Thought
    for _, t := range tree.thoughts {
        if t.State == ThoughtStateEvaluated && len(t.Children) == 0 {
            candidates = append(candidates, t)
        }
    }

    if len(candidates) == 0 {
        return nil
    }

    // Sort by value (descending), then depth (ascending)
    sort.Slice(candidates, func(i, j int) bool {
        vi := candidates[i].Value * math.Pow(s.valueDecay, float64(candidates[i].Depth))
        vj := candidates[j].Value * math.Pow(s.valueDecay, float64(candidates[j].Depth))
        return vi > vj
    })

    return candidates[0]
}

func (s *BFSStrategy) ShouldPrune(thought *Thought, budget Budget) bool {
    // Prune low-value thoughts
    if thought.Value < s.minValue {
        return true
    }

    // Prune if budget exhausted
    if budget.Remaining() < budget.EstimatedCostPerExpansion() {
        return true
    }

    return false
}

func (s *BFSStrategy) ShouldBacktrack(thought *Thought) bool {
    return thought.State == ThoughtStateFailed
}
```

### Thought Generator

```go
// internal/rlm/tot/generator.go

// Generator produces candidate thoughts.
type Generator interface {
    // Generate creates candidate thoughts for a parent.
    Generate(ctx context.Context, parent *Thought, n int) ([]*Thought, error)
}

type LLMGenerator struct {
    client    LLMClient
    promptTpl *template.Template
}

func (g *LLMGenerator) Generate(
    ctx context.Context,
    parent *Thought,
    n int,
) ([]*Thought, error) {
    prompt := g.buildPrompt(parent, n)

    response, _, err := g.client.Complete(ctx, prompt)
    if err != nil {
        return nil, err
    }

    // Parse response into n distinct thoughts
    thoughts := g.parseThoughts(response, parent, n)
    return thoughts, nil
}

func (g *LLMGenerator) buildPrompt(parent *Thought, n int) string {
    // Template asks for n distinct approaches
    return fmt.Sprintf(`Given the current reasoning state:

%s

Generate %d distinct approaches to continue. Each approach should:
1. Be meaningfully different from the others
2. Be self-contained and actionable
3. Address the core problem directly

Format each approach as:
[Approach 1]
<approach content>

[Approach 2]
<approach content>
...`, parent.Content, n)
}
```

### Thought Evaluator

```go
// internal/rlm/tot/evaluator.go

// Evaluator scores thoughts for promise.
type Evaluator interface {
    // Evaluate returns a value in [0, 1] for a thought.
    Evaluate(ctx context.Context, thought *Thought, context *EvalContext) (float64, error)
}

type EvalContext struct {
    OriginalTask string
    PathToRoot   []*Thought
    Siblings     []*Thought
}

type LLMEvaluator struct {
    client    LLMClient
    criteria  []EvalCriterion
}

type EvalCriterion struct {
    Name        string
    Description string
    Weight      float64
}

func DefaultCriteria() []EvalCriterion {
    return []EvalCriterion{
        {Name: "correctness", Description: "Is the reasoning logically sound?", Weight: 0.3},
        {Name: "progress", Description: "Does this make progress toward the goal?", Weight: 0.3},
        {Name: "feasibility", Description: "Can this approach be completed?", Weight: 0.2},
        {Name: "efficiency", Description: "Is this an efficient approach?", Weight: 0.2},
    }
}

func (e *LLMEvaluator) Evaluate(
    ctx context.Context,
    thought *Thought,
    evalCtx *EvalContext,
) (float64, error) {
    prompt := e.buildEvalPrompt(thought, evalCtx)

    response, _, err := e.client.Complete(ctx, prompt)
    if err != nil {
        return 0, err
    }

    // Parse scores from response
    scores := e.parseScores(response)

    // Weighted average
    var total, weightSum float64
    for _, criterion := range e.criteria {
        if score, ok := scores[criterion.Name]; ok {
            total += score * criterion.Weight
            weightSum += criterion.Weight
        }
    }

    if weightSum == 0 {
        return 0.5, nil // Default to neutral
    }

    return total / weightSum, nil
}

func (e *LLMEvaluator) buildEvalPrompt(thought *Thought, ctx *EvalContext) string {
    var pathStr strings.Builder
    for i, t := range ctx.PathToRoot {
        pathStr.WriteString(fmt.Sprintf("Step %d: %s\n", i+1, t.Content))
    }

    return fmt.Sprintf(`Evaluate this reasoning step for solving the task.

Original Task: %s

Reasoning Path:
%s

Current Step: %s

Rate each criterion from 0.0 to 1.0:
- correctness: Is the reasoning logically sound?
- progress: Does this make progress toward the goal?
- feasibility: Can this approach be completed?
- efficiency: Is this an efficient approach?

Format:
correctness: <score>
progress: <score>
feasibility: <score>
efficiency: <score>`, ctx.OriginalTask, pathStr.String(), thought.Content)
}
```

## ToT Controller

### Main Controller

```go
// internal/rlm/tot/controller.go

// Controller orchestrates Tree of Thoughts exploration.
type Controller struct {
    generator  Generator
    evaluator  Evaluator
    strategy   SearchStrategy
    executor   Executor
    config     Config
}

type Config struct {
    MaxDepth         int           // Maximum tree depth
    BranchingFactor  int           // Thoughts to generate per expansion
    MinValue         float64       // Minimum value to continue
    MaxExpansions    int           // Maximum total expansions
    TokenBudget      int           // Token budget for exploration
    Timeout          time.Duration // Maximum exploration time
}

func DefaultConfig() Config {
    return Config{
        MaxDepth:        5,
        BranchingFactor: 3,
        MinValue:        0.3,
        MaxExpansions:   20,
        TokenBudget:     50000,
        Timeout:         2 * time.Minute,
    }
}

// Solve explores the thought tree to solve a task.
func (c *Controller) Solve(ctx context.Context, task string) (*Solution, error) {
    ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
    defer cancel()

    tree := NewThoughtTree(task)
    budget := NewBudget(c.config.TokenBudget)

    expansions := 0
    for expansions < c.config.MaxExpansions {
        // Get next thought to expand
        thought := c.strategy.NextThought(tree)
        if thought == nil {
            break // No more thoughts to expand
        }

        // Check if we should prune
        if c.strategy.ShouldPrune(thought, budget) {
            thought.State = ThoughtStatePruned
            continue
        }

        // Check depth limit
        if thought.Depth >= c.config.MaxDepth {
            // Try to execute this thought as final
            result, err := c.executor.Execute(ctx, thought)
            if err == nil && result.Success {
                return c.buildSolution(tree, thought, result), nil
            }
            thought.State = ThoughtStateFailed
            continue
        }

        // Generate children
        children, err := c.generator.Generate(ctx, thought, c.config.BranchingFactor)
        if err != nil {
            thought.State = ThoughtStateFailed
            if c.strategy.ShouldBacktrack(thought) {
                continue // Backtrack to try other branches
            }
            return nil, err
        }

        // Evaluate children
        for _, child := range children {
            evalCtx := &EvalContext{
                OriginalTask: task,
                PathToRoot:   c.getPathToRoot(tree, child),
                Siblings:     children,
            }

            value, err := c.evaluator.Evaluate(ctx, child, evalCtx)
            if err != nil {
                child.Value = 0.5 // Default on error
            } else {
                child.Value = value
            }
            child.State = ThoughtStateEvaluated

            tree.AddThought(child)
            thought.Children = append(thought.Children, child.ID)
        }

        thought.State = ThoughtStateExpanded
        expansions++
        budget.Deduct(c.estimateExpansionCost(children))
    }

    // Return best path found
    return c.findBestSolution(tree), nil
}

func (c *Controller) getPathToRoot(tree *ThoughtTree, thought *Thought) []*Thought {
    var path []*Thought
    current := thought
    for current != nil {
        path = append([]*Thought{current}, path...)
        if current.ParentID == "" {
            break
        }
        current = tree.thoughts[current.ParentID]
    }
    return path
}

func (c *Controller) buildSolution(tree *ThoughtTree, leaf *Thought, result *ExecutionResult) *Solution {
    return &Solution{
        Path:      c.getPathToRoot(tree, leaf),
        Result:    result,
        TreeStats: tree.Stats(),
    }
}

func (c *Controller) findBestSolution(tree *ThoughtTree) *Solution {
    var best *Thought
    var bestValue float64

    for _, t := range tree.thoughts {
        if t.State == ThoughtStateSucceeded || t.Depth == c.config.MaxDepth-1 {
            if t.Value > bestValue {
                best = t
                bestValue = t.Value
            }
        }
    }

    if best == nil {
        return nil
    }

    return &Solution{
        Path:      c.getPathToRoot(tree, best),
        TreeStats: tree.Stats(),
    }
}
```

### Solution Type

```go
// internal/rlm/tot/solution.go

// Solution represents the result of ToT exploration.
type Solution struct {
    Path      []*Thought
    Result    *ExecutionResult
    TreeStats TreeStats
}

type TreeStats struct {
    TotalThoughts int
    MaxDepth      int
    Expansions    int
    Pruned        int
    Backtracks    int
    TokensUsed    int
}

func (s *Solution) ReasoningTrace() string {
    var trace strings.Builder
    for i, thought := range s.Path {
        trace.WriteString(fmt.Sprintf("Step %d (value=%.2f):\n%s\n\n",
            i+1, thought.Value, thought.Content))
    }
    return trace.String()
}
```

## Integration with RLM

### Meta-Controller Integration

```go
// internal/rlm/meta/decision.go

type Decision struct {
    Mode           ExecutionMode
    // ... existing fields ...

    // ToT-specific
    UseToT         bool
    ToTConfig      *tot.Config
    ToTReason      string  // Why ToT was chosen
}

// internal/rlm/meta/controller.go

func (m *MetaController) shouldUseToT(state State) bool {
    // Use ToT for complex reasoning tasks
    signals := []bool{
        state.TaskClassification == TaskTypeReasoning,
        state.EstimatedComplexity > 0.7,
        state.HasMultipleApproaches,
        state.PreviousAttemptFailed,
    }

    trueCount := 0
    for _, s := range signals {
        if s {
            trueCount++
        }
    }

    return trueCount >= 2
}
```

### Controller Integration

```go
// internal/rlm/controller.go

func (c *Controller) orchestrate(ctx context.Context, state meta.State, parentID string) (string, int, error) {
    decision, err := c.metaController.Decide(ctx, state)
    if err != nil {
        return "", 0, err
    }

    // Check if ToT should be used
    if decision.UseToT {
        return c.executeWithToT(ctx, state, decision.ToTConfig)
    }

    // ... existing orchestration logic ...
}

func (c *Controller) executeWithToT(
    ctx context.Context,
    state meta.State,
    config *tot.Config,
) (string, int, error) {
    if config == nil {
        config = &tot.DefaultConfig()
    }

    totController := tot.NewController(
        tot.NewLLMGenerator(c.llmClient),
        tot.NewLLMEvaluator(c.llmClient),
        tot.NewBFSStrategy(config.MinValue, config.BranchingFactor),
        c.executor,
        *config,
    )

    solution, err := totController.Solve(ctx, state.Query)
    if err != nil {
        return "", 0, fmt.Errorf("ToT solve: %w", err)
    }

    if solution == nil {
        return "", 0, errors.New("no solution found")
    }

    // Log reasoning trace to memory
    c.storeReasoningTrace(ctx, solution)

    return solution.Result.Content, solution.TreeStats.TokensUsed, nil
}
```

## Observability

### Metrics

```go
var (
    totExpansions = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_tot_expansions",
            Help:    "Number of thought expansions per solve",
            Buckets: prometheus.LinearBuckets(1, 5, 10),
        },
    )

    totMaxDepth = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_tot_max_depth",
            Help:    "Maximum depth reached in thought tree",
            Buckets: prometheus.LinearBuckets(1, 1, 10),
        },
    )

    totBacktracks = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_tot_backtracks_total",
            Help: "Total number of backtracks",
        },
    )

    totPruned = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_tot_pruned_total",
            Help: "Total thoughts pruned",
        },
    )

    totSolveLatency = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_tot_solve_duration_seconds",
            Help:    "ToT solve duration",
            Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
        },
    )
)
```

### Tracing

```go
func (c *Controller) Solve(ctx context.Context, task string) (*Solution, error) {
    ctx, span := tracer.Start(ctx, "ToT.Solve",
        trace.WithAttributes(
            attribute.String("task", truncate(task, 100)),
            attribute.Int("max_depth", c.config.MaxDepth),
            attribute.Int("branching_factor", c.config.BranchingFactor),
        ),
    )
    defer span.End()

    // ... solve logic ...

    span.SetAttributes(
        attribute.Int("expansions", expansions),
        attribute.Int("thoughts_generated", len(tree.thoughts)),
        attribute.Int("max_depth_reached", tree.MaxDepth()),
    )

    return solution, nil
}
```

## Testing Strategy

### Unit Tests

```go
func TestBFSStrategy_NextThought(t *testing.T) {
    tree := NewThoughtTree("root")

    // Add evaluated thoughts
    t1 := &Thought{ID: "t1", ParentID: tree.root.ID, Value: 0.8, State: ThoughtStateEvaluated, Depth: 1}
    t2 := &Thought{ID: "t2", ParentID: tree.root.ID, Value: 0.6, State: ThoughtStateEvaluated, Depth: 1}
    t3 := &Thought{ID: "t3", ParentID: tree.root.ID, Value: 0.9, State: ThoughtStateEvaluated, Depth: 1}

    tree.AddThought(t1)
    tree.AddThought(t2)
    tree.AddThought(t3)

    strategy := &BFSStrategy{minValue: 0.3, valueDecay: 0.9}

    // Should select highest value thought
    next := strategy.NextThought(tree)
    assert.Equal(t, "t3", next.ID)
}

func TestController_Backtrack(t *testing.T) {
    // Mock generator that fails on first path
    generator := &MockGenerator{
        responses: map[string][]*Thought{
            "root": {
                {ID: "a", Content: "approach A", Value: 0.8},
                {ID: "b", Content: "approach B", Value: 0.6},
            },
            "a": {
                {ID: "a1", Content: "dead end", Value: 0.2}, // Will be pruned
            },
            "b": {
                {ID: "b1", Content: "solution", Value: 0.9},
            },
        },
    }

    ctrl := NewController(generator, &PassthroughEvaluator{}, &BFSStrategy{minValue: 0.3}, nil, DefaultConfig())

    solution, err := ctrl.Solve(context.Background(), "test task")
    require.NoError(t, err)

    // Should have found solution via backtrack to B
    assert.Contains(t, solution.Path[len(solution.Path)-1].ID, "b")
}
```

### Integration Tests

```go
func TestToT_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    client := newTestLLMClient(t)
    ctrl := NewController(
        NewLLMGenerator(client),
        NewLLMEvaluator(client),
        NewBFSStrategy(0.3, 3),
        newTestExecutor(t),
        Config{
            MaxDepth:        3,
            BranchingFactor: 2,
            MaxExpansions:   10,
            Timeout:         30 * time.Second,
        },
    )

    solution, err := ctrl.Solve(context.Background(),
        "Write a function to find the longest palindromic substring")

    require.NoError(t, err)
    assert.NotNil(t, solution)
    assert.Greater(t, len(solution.Path), 1)

    // Verify reasoning trace is coherent
    trace := solution.ReasoningTrace()
    assert.Contains(t, trace, "palindrome")
}
```

## Migration Path

### Phase 1: Opt-in ToT

```go
type ControllerConfig struct {
    EnableToT        bool
    ToTMinComplexity float64 // Minimum complexity to trigger ToT
}
```

### Phase 2: Automatic Activation

Enable ToT automatically for high-complexity reasoning tasks.

### Phase 3: Hybrid Mode

Combine ToT with decomposition for complex multi-step problems.

## Success Criteria

1. **Solution quality**: 20% improvement on reasoning benchmarks
2. **Exploration efficiency**: <50% of budget spent on pruned branches
3. **Backtrack success**: >60% of backtracks lead to better solutions
4. **Latency**: <2x overhead vs single-path for typical queries
5. **Integration**: Seamless with existing RLM orchestration

## Appendix: Comparison with Other Methods

| Method | Exploration | Evaluation | Backtracking |
|--------|-------------|------------|--------------|
| Chain-of-Thought | Single path | None | None |
| Self-Consistency | Multiple paths | Voting | None |
| Tree of Thoughts | Tree search | LLM scoring | Yes |
| LATS | Tree + MCTS | Reward model | Yes |

ToT provides the best balance of exploration and efficiency for our use case.
