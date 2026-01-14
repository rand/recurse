# Adaptive Depth Allocation Design

> Design document for `recurse-jf5`: [SPEC] Adaptive Depth Allocation Design

## Overview

This document specifies adaptive depth allocation for the RLM system, dynamically adjusting recursion depth and compute budget based on query complexity. Simple queries get shallow, fast processing while complex queries get deep, thorough analysis.

## Problem Statement

### Current State

Fixed recursion depth regardless of query complexity:

```go
const MaxRecursionDepth = 5 // Same for all queries
```

**Issues**:
- Simple queries waste compute on unnecessary depth
- Complex queries may need more depth than allowed
- No adaptation based on intermediate results
- Uniform budget allocation across subtasks

### Desired Behavior

| Query Complexity | Depth | Budget | Example |
|-----------------|-------|--------|---------|
| Trivial | 1 | 5% | "What is 2+2?" |
| Simple | 2 | 15% | "Summarize this paragraph" |
| Moderate | 3-4 | 40% | "Analyze code structure" |
| Complex | 5-7 | 70% | "Debug multi-file issue" |
| Very Complex | 8+ | 100% | "Architect new system" |

## Design Goals

1. **Complexity estimation**: Classify query difficulty upfront
2. **Dynamic adjustment**: Adapt depth based on intermediate results
3. **Budget allocation**: Distribute resources across subtasks
4. **Early termination**: Stop when confident, even if budget remains
5. **Depth escalation**: Increase depth when needed

## Core Types

### Complexity Classification

```go
// internal/rlm/depth/complexity.go

// Complexity represents estimated query difficulty.
type Complexity struct {
    Level       ComplexityLevel
    Score       float64 // 0.0 to 1.0
    Signals     []ComplexitySignal
    Suggested   DepthConfig
}

type ComplexityLevel int

const (
    ComplexityTrivial  ComplexityLevel = iota // Single-step, immediate
    ComplexitySimple                           // Few steps, straightforward
    ComplexityModerate                         // Multi-step, some reasoning
    ComplexityComplex                          // Deep analysis required
    ComplexityVeryComplex                      // Extensive exploration
)

type ComplexitySignal struct {
    Name   string
    Value  float64
    Weight float64
}

type DepthConfig struct {
    InitialDepth int
    MaxDepth     int
    BudgetRatio  float64 // Fraction of total budget
    CanEscalate  bool    // Allow depth increase
}

func (c Complexity) DepthConfig() DepthConfig {
    switch c.Level {
    case ComplexityTrivial:
        return DepthConfig{
            InitialDepth: 1,
            MaxDepth:     1,
            BudgetRatio:  0.05,
            CanEscalate:  false,
        }
    case ComplexitySimple:
        return DepthConfig{
            InitialDepth: 2,
            MaxDepth:     3,
            BudgetRatio:  0.15,
            CanEscalate:  true,
        }
    case ComplexityModerate:
        return DepthConfig{
            InitialDepth: 3,
            MaxDepth:     5,
            BudgetRatio:  0.40,
            CanEscalate:  true,
        }
    case ComplexityComplex:
        return DepthConfig{
            InitialDepth: 5,
            MaxDepth:     7,
            BudgetRatio:  0.70,
            CanEscalate:  true,
        }
    case ComplexityVeryComplex:
        return DepthConfig{
            InitialDepth: 7,
            MaxDepth:     10,
            BudgetRatio:  1.0,
            CanEscalate:  true,
        }
    default:
        return DepthConfig{
            InitialDepth: 3,
            MaxDepth:     5,
            BudgetRatio:  0.40,
            CanEscalate:  true,
        }
    }
}
```

### Complexity Classifier

```go
// internal/rlm/depth/classifier.go

// Classifier estimates query complexity.
type Classifier interface {
    Classify(ctx context.Context, query string, context []ContextChunk) (*Complexity, error)
}

type HybridClassifier struct {
    heuristic *HeuristicClassifier
    llm       *LLMClassifier
    threshold float64 // Use LLM if heuristic confidence below this
}

func (c *HybridClassifier) Classify(
    ctx context.Context,
    query string,
    context []ContextChunk,
) (*Complexity, error) {
    // Try heuristic first (fast)
    hResult := c.heuristic.Classify(query, context)

    // If confident enough, use heuristic
    if hResult.confidence >= c.threshold {
        return hResult.complexity, nil
    }

    // Fall back to LLM for uncertain cases
    return c.llm.Classify(ctx, query, context)
}
```

### Heuristic Classifier

```go
// internal/rlm/depth/heuristic.go

type HeuristicClassifier struct {
    patterns []ComplexityPattern
}

type ComplexityPattern struct {
    Name    string
    Regex   *regexp.Regexp
    Level   ComplexityLevel
    Weight  float64
}

func NewHeuristicClassifier() *HeuristicClassifier {
    return &HeuristicClassifier{
        patterns: []ComplexityPattern{
            // Trivial patterns
            {Name: "simple_math", Regex: regexp.MustCompile(`what is \d+\s*[\+\-\*\/]\s*\d+`), Level: ComplexityTrivial, Weight: 0.9},
            {Name: "direct_lookup", Regex: regexp.MustCompile(`what is (the|a) \w+`), Level: ComplexitySimple, Weight: 0.7},

            // Simple patterns
            {Name: "summarize", Regex: regexp.MustCompile(`summarize|brief|overview`), Level: ComplexitySimple, Weight: 0.8},
            {Name: "explain_simple", Regex: regexp.MustCompile(`explain (what|how|why)`), Level: ComplexitySimple, Weight: 0.6},

            // Moderate patterns
            {Name: "compare", Regex: regexp.MustCompile(`compare|difference|versus|vs\.?`), Level: ComplexityModerate, Weight: 0.7},
            {Name: "analyze", Regex: regexp.MustCompile(`analyze|analysis|examine`), Level: ComplexityModerate, Weight: 0.7},
            {Name: "multiple_parts", Regex: regexp.MustCompile(`(first|then|next|finally|also)`), Level: ComplexityModerate, Weight: 0.5},

            // Complex patterns
            {Name: "debug", Regex: regexp.MustCompile(`debug|fix|error|bug|issue`), Level: ComplexityComplex, Weight: 0.8},
            {Name: "implement", Regex: regexp.MustCompile(`implement|create|build|develop`), Level: ComplexityComplex, Weight: 0.7},
            {Name: "multi_file", Regex: regexp.MustCompile(`(files?|modules?|components?).*(files?|modules?|components?)`), Level: ComplexityComplex, Weight: 0.8},

            // Very complex patterns
            {Name: "architect", Regex: regexp.MustCompile(`architect|design|system|infrastructure`), Level: ComplexityVeryComplex, Weight: 0.8},
            {Name: "refactor", Regex: regexp.MustCompile(`refactor|restructure|redesign`), Level: ComplexityVeryComplex, Weight: 0.7},
            {Name: "comprehensive", Regex: regexp.MustCompile(`comprehensive|thorough|complete|all`), Level: ComplexityVeryComplex, Weight: 0.6},
        },
    }
}

func (c *HeuristicClassifier) Classify(query string, context []ContextChunk) *classifyResult {
    query = strings.ToLower(query)
    signals := []ComplexitySignal{}

    // Pattern matching
    var maxLevel ComplexityLevel
    var totalScore float64
    var matchCount int

    for _, p := range c.patterns {
        if p.Regex.MatchString(query) {
            signals = append(signals, ComplexitySignal{
                Name:   p.Name,
                Value:  1.0,
                Weight: p.Weight,
            })
            if p.Level > maxLevel {
                maxLevel = p.Level
            }
            totalScore += p.Weight
            matchCount++
        }
    }

    // Context size signal
    contextTokens := c.estimateContextTokens(context)
    contextSignal := c.contextSizeSignal(contextTokens)
    signals = append(signals, contextSignal)

    // Query length signal
    queryTokens := len(strings.Fields(query))
    lengthSignal := c.queryLengthSignal(queryTokens)
    signals = append(signals, lengthSignal)

    // Compute confidence
    confidence := 0.5
    if matchCount > 0 {
        confidence = math.Min(0.9, 0.5+float64(matchCount)*0.15)
    }

    return &classifyResult{
        complexity: &Complexity{
            Level:   maxLevel,
            Score:   c.levelToScore(maxLevel),
            Signals: signals,
        },
        confidence: confidence,
    }
}

func (c *HeuristicClassifier) contextSizeSignal(tokens int) ComplexitySignal {
    // Larger context suggests more complexity
    var value float64
    switch {
    case tokens < 1000:
        value = 0.2
    case tokens < 5000:
        value = 0.4
    case tokens < 20000:
        value = 0.6
    case tokens < 50000:
        value = 0.8
    default:
        value = 1.0
    }

    return ComplexitySignal{
        Name:   "context_size",
        Value:  value,
        Weight: 0.3,
    }
}
```

### LLM Classifier

```go
// internal/rlm/depth/llm_classifier.go

type LLMClassifier struct {
    client LLMClient
}

func (c *LLMClassifier) Classify(
    ctx context.Context,
    query string,
    context []ContextChunk,
) (*Complexity, error) {
    prompt := fmt.Sprintf(`Classify the complexity of this task.

Task: %s

Context size: %d tokens

Rate complexity from 1-5:
1 = Trivial (single step, immediate answer)
2 = Simple (few steps, straightforward)
3 = Moderate (multi-step reasoning)
4 = Complex (deep analysis, multiple components)
5 = Very Complex (extensive exploration, system-level)

Also rate confidence (0.0-1.0) in your assessment.

Format:
Complexity: <1-5>
Confidence: <0.0-1.0>
Reasoning: <brief explanation>`, query, c.estimateTokens(context))

    response, _, err := c.client.Complete(ctx, prompt)
    if err != nil {
        return nil, err
    }

    return c.parseResponse(response)
}
```

## Depth Allocator

### Allocator

```go
// internal/rlm/depth/allocator.go

// Allocator manages depth and budget across execution.
type Allocator struct {
    classifier   Classifier
    totalBudget  int
    config       AllocatorConfig
}

type AllocatorConfig struct {
    MinDepth          int
    MaxDepth          int
    EscalationFactor  float64 // How much to increase depth on escalation
    TerminationConf   float64 // Confidence threshold for early termination
}

func DefaultAllocatorConfig() AllocatorConfig {
    return AllocatorConfig{
        MinDepth:         1,
        MaxDepth:         10,
        EscalationFactor: 1.5,
        TerminationConf:  0.9,
    }
}

// Allocate determines depth and budget for a query.
func (a *Allocator) Allocate(
    ctx context.Context,
    query string,
    context []ContextChunk,
) (*Allocation, error) {
    complexity, err := a.classifier.Classify(ctx, query, context)
    if err != nil {
        // Default to moderate on classification failure
        complexity = &Complexity{Level: ComplexityModerate}
    }

    depthConfig := complexity.DepthConfig()

    return &Allocation{
        Complexity:    complexity,
        InitialDepth:  depthConfig.InitialDepth,
        MaxDepth:      min(depthConfig.MaxDepth, a.config.MaxDepth),
        TokenBudget:   int(float64(a.totalBudget) * depthConfig.BudgetRatio),
        CanEscalate:   depthConfig.CanEscalate,
        SubtaskBudgets: make(map[string]int),
    }, nil
}

type Allocation struct {
    Complexity     *Complexity
    InitialDepth   int
    MaxDepth       int
    CurrentDepth   int
    TokenBudget    int
    TokensUsed     int
    CanEscalate    bool
    EscalationCount int
    SubtaskBudgets map[string]int
}

// AllocateSubtask distributes budget to a subtask.
func (a *Allocation) AllocateSubtask(subtaskID string, weight float64) int {
    remaining := a.TokenBudget - a.TokensUsed
    subtaskBudget := int(float64(remaining) * weight)

    a.SubtaskBudgets[subtaskID] = subtaskBudget
    return subtaskBudget
}

// ShouldEscalate checks if depth should increase.
func (a *Allocation) ShouldEscalate(confidence float64, progress float64) bool {
    if !a.CanEscalate {
        return false
    }

    if a.CurrentDepth >= a.MaxDepth {
        return false
    }

    // Escalate if low confidence and low progress
    return confidence < 0.5 && progress < 0.5
}

// Escalate increases allowed depth.
func (a *Allocation) Escalate(factor float64) {
    a.MaxDepth = min(a.MaxDepth+int(float64(a.MaxDepth)*factor), 10)
    a.EscalationCount++
}

// ShouldTerminate checks if execution can stop early.
func (a *Allocation) ShouldTerminate(confidence float64) bool {
    return confidence >= 0.9
}
```

### Budget Distribution

```go
// internal/rlm/depth/budget.go

// BudgetDistributor allocates budget across subtasks.
type BudgetDistributor struct {
    strategy DistributionStrategy
}

type DistributionStrategy int

const (
    DistributeEqual    DistributionStrategy = iota // Equal split
    DistributeWeighted                              // Weighted by complexity
    DistributeAdaptive                              // Adjust based on results
)

func (d *BudgetDistributor) Distribute(
    totalBudget int,
    subtasks []Subtask,
    strategy DistributionStrategy,
) map[string]int {
    budgets := make(map[string]int)

    switch strategy {
    case DistributeEqual:
        perTask := totalBudget / len(subtasks)
        for _, st := range subtasks {
            budgets[st.ID] = perTask
        }

    case DistributeWeighted:
        totalWeight := 0.0
        for _, st := range subtasks {
            totalWeight += st.Weight
        }
        for _, st := range subtasks {
            budgets[st.ID] = int(float64(totalBudget) * st.Weight / totalWeight)
        }

    case DistributeAdaptive:
        // Start with equal, adjust based on initial results
        perTask := totalBudget / len(subtasks)
        for _, st := range subtasks {
            budgets[st.ID] = perTask
        }
    }

    return budgets
}

type Subtask struct {
    ID          string
    Description string
    Weight      float64 // Relative importance/complexity
    Priority    int
}
```

## Adaptive Controller

### Controller Integration

```go
// internal/rlm/depth/controller.go

// AdaptiveController wraps RLM controller with depth management.
type AdaptiveController struct {
    inner     *Controller
    allocator *Allocator
}

func (c *AdaptiveController) Execute(
    ctx context.Context,
    query string,
    context []ContextChunk,
) (string, int, error) {
    // Get allocation
    allocation, err := c.allocator.Allocate(ctx, query, context)
    if err != nil {
        return "", 0, err
    }

    // Create depth-aware context
    depthCtx := &DepthContext{
        Allocation: allocation,
        StartDepth: 0,
    }

    return c.executeWithDepth(ctx, query, context, depthCtx)
}

func (c *AdaptiveController) executeWithDepth(
    ctx context.Context,
    query string,
    context []ContextChunk,
    depthCtx *DepthContext,
) (string, int, error) {
    allocation := depthCtx.Allocation

    // Check depth limit
    if depthCtx.CurrentDepth >= allocation.MaxDepth {
        // Direct execution at max depth
        return c.inner.ExecuteDirect(ctx, query, context)
    }

    // Execute with decomposition
    result, tokens, confidence, err := c.inner.ExecuteWithConfidence(ctx, query, context)
    if err != nil {
        return "", tokens, err
    }

    allocation.TokensUsed += tokens

    // Check for early termination
    if allocation.ShouldTerminate(confidence) {
        return result, tokens, nil
    }

    // Check for escalation
    progress := float64(allocation.TokensUsed) / float64(allocation.TokenBudget)
    if allocation.ShouldEscalate(confidence, progress) {
        allocation.Escalate(c.allocator.config.EscalationFactor)

        // Continue with increased depth
        depthCtx.CurrentDepth++
        return c.executeWithDepth(ctx, query, context, depthCtx)
    }

    return result, tokens, nil
}

type DepthContext struct {
    Allocation   *Allocation
    StartDepth   int
    CurrentDepth int
}
```

## Early Termination

### Termination Checker

```go
// internal/rlm/depth/termination.go

// TerminationChecker decides if execution can stop early.
type TerminationChecker struct {
    confidenceThreshold float64
    progressThreshold   float64
}

func (t *TerminationChecker) ShouldTerminate(state *ExecutionState) (bool, string) {
    // High confidence = done
    if state.Confidence >= t.confidenceThreshold {
        return true, "high confidence achieved"
    }

    // All subtasks complete
    if state.SubtasksDone == state.SubtasksTotal && state.SubtasksTotal > 0 {
        return true, "all subtasks complete"
    }

    // Budget nearly exhausted with good progress
    budgetUsed := float64(state.TokensUsed) / float64(state.TokenBudget)
    if budgetUsed > 0.95 && state.Confidence > 0.7 {
        return true, "budget exhausted with acceptable confidence"
    }

    return false, ""
}
```

## Observability

### Metrics

```go
var (
    complexityClassifications = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_complexity_classifications_total",
            Help: "Query complexity classifications",
        },
        []string{"level"},
    )

    depthAllocated = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_depth_allocated",
            Help:    "Allocated depth per query",
            Buckets: prometheus.LinearBuckets(1, 1, 10),
        },
    )

    depthUsed = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_depth_used",
            Help:    "Actual depth used per query",
            Buckets: prometheus.LinearBuckets(1, 1, 10),
        },
    )

    depthEscalations = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_depth_escalations_total",
            Help: "Number of depth escalations",
        },
    )

    earlyTerminations = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_early_terminations_total",
            Help: "Early terminations by reason",
        },
        []string{"reason"},
    )
)
```

## Testing Strategy

### Unit Tests

```go
func TestHeuristicClassifier_Patterns(t *testing.T) {
    classifier := NewHeuristicClassifier()

    tests := []struct {
        query    string
        expected ComplexityLevel
    }{
        {"what is 2+2", ComplexityTrivial},
        {"summarize this document", ComplexitySimple},
        {"compare these two approaches", ComplexityModerate},
        {"debug this error in the authentication module", ComplexityComplex},
        {"design the architecture for a new microservice", ComplexityVeryComplex},
    }

    for _, tt := range tests {
        t.Run(tt.query, func(t *testing.T) {
            result := classifier.Classify(tt.query, nil)
            assert.Equal(t, tt.expected, result.complexity.Level)
        })
    }
}

func TestAllocation_ShouldEscalate(t *testing.T) {
    allocation := &Allocation{
        CurrentDepth: 3,
        MaxDepth:     5,
        CanEscalate:  true,
    }

    // Low confidence, low progress -> escalate
    assert.True(t, allocation.ShouldEscalate(0.3, 0.2))

    // High confidence -> don't escalate
    assert.False(t, allocation.ShouldEscalate(0.8, 0.2))

    // At max depth -> don't escalate
    allocation.CurrentDepth = 5
    assert.False(t, allocation.ShouldEscalate(0.3, 0.2))
}

func TestAllocation_ShouldTerminate(t *testing.T) {
    allocation := &Allocation{}

    // High confidence -> terminate
    assert.True(t, allocation.ShouldTerminate(0.95))

    // Low confidence -> continue
    assert.False(t, allocation.ShouldTerminate(0.6))
}
```

## Success Criteria

1. **Accuracy**: Complexity classification matches human judgment (>80% agreement)
2. **Efficiency**: Trivial queries use <10% of budget
3. **Thoroughness**: Complex queries get sufficient depth
4. **Escalation**: Depth escalation improves outcomes in >70% of cases
5. **Early termination**: Saves >30% of budget on high-confidence results

## Appendix: Complexity Signals

| Signal | Description | Weight |
|--------|-------------|--------|
| Query length | Longer queries often more complex | 0.2 |
| Context size | More context suggests more complexity | 0.3 |
| Multi-part structure | "First... then... finally" | 0.4 |
| Domain keywords | "debug", "architect", "implement" | 0.5 |
| Explicit scope | "comprehensive", "thorough" | 0.4 |
