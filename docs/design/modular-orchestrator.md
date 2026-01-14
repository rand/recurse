# Modular Orchestrator Package Design

> Design document for `recurse-o3l`: [SPEC] Modular Orchestrator Package Design

## Overview

This document specifies the refactoring of the RLM orchestration layer into a modular, composable package. This enables mixing orchestration strategies, easier testing, and cleaner separation of concerns.

## Problem Statement

### Current State

Monolithic controller with interleaved concerns:

```go
type Controller struct {
    // Everything in one struct
    metaController *meta.Controller
    synthesizer    *synthesize.Synthesizer
    llmClient      LLMClient
    replMgr        *repl.Manager
    memoryStore    *hypergraph.Store
    // ...
}

func (c *Controller) orchestrate(...) {
    // 500+ lines mixing decomposition, execution, synthesis
}
```

**Issues**:
- Hard to test individual components
- Difficult to swap strategies
- Tight coupling between layers
- Complex initialization

## Design Goals

1. **Modularity**: Independent, testable components
2. **Composability**: Mix and match strategies
3. **Clean interfaces**: Clear contracts between layers
4. **Extensibility**: Add new strategies easily
5. **Testability**: Mock any layer independently

## Core Architecture

### Layer Separation

```
┌─────────────────────────────────────────────┐
│              Orchestrator                    │
│  (coordinates execution flow)                │
├─────────────────────────────────────────────┤
│     Planner     │    Executor    │ Synthesizer│
│  (decomposition)│   (sub-calls)  │  (combine) │
├─────────────────────────────────────────────┤
│              Strategy Layer                  │
│  (ToT, LATS, Direct, Decompose)             │
├─────────────────────────────────────────────┤
│              Provider Layer                  │
│  (LLM, REPL, Memory, Tools)                 │
└─────────────────────────────────────────────┘
```

### Core Interfaces

```go
// pkg/orchestrator/interfaces.go

// Orchestrator coordinates the complete execution flow.
type Orchestrator interface {
    Execute(ctx context.Context, request *Request) (*Response, error)
}

// Planner decides how to decompose a task.
type Planner interface {
    Plan(ctx context.Context, task string, context *PlanContext) (*Plan, error)
}

// Executor runs individual operations.
type Executor interface {
    Execute(ctx context.Context, op *Operation) (*OperationResult, error)
    ExecuteBatch(ctx context.Context, ops []*Operation) ([]*OperationResult, error)
}

// Synthesizer combines results into final output.
type Synthesizer interface {
    Synthesize(ctx context.Context, results []*OperationResult, context *SynthContext) (*Synthesis, error)
}

// Strategy determines the execution approach.
type Strategy interface {
    Name() string
    Applicable(ctx context.Context, task string, context *StrategyContext) (bool, float64)
    Execute(ctx context.Context, task string, executor Executor) (*StrategyResult, error)
}
```

### Request/Response Types

```go
// pkg/orchestrator/types.go

type Request struct {
    ID          string
    Query       string
    Context     []ContextChunk
    Constraints *Constraints
    Metadata    map[string]any
}

type Constraints struct {
    MaxTokens     int
    MaxTime       time.Duration
    MaxDepth      int
    MinConfidence float64
}

type Response struct {
    ID          string
    Content     string
    Confidence  float64
    TokensUsed  int
    Duration    time.Duration
    Trace       *ExecutionTrace
    Metadata    map[string]any
}

type Plan struct {
    Strategy    string
    Operations  []*Operation
    Dependencies map[string][]string
    EstimatedTokens int
}

type Operation struct {
    ID          string
    Type        OperationType
    Input       string
    Context     []ContextChunk
    Priority    int
    DependsOn   []string
}

type OperationType int

const (
    OpTypeLLMCall OperationType = iota
    OpTypeREPLExec
    OpTypeMemoryQuery
    OpTypeToolCall
)
```

## Orchestrator Implementation

### Default Orchestrator

```go
// pkg/orchestrator/default.go

type DefaultOrchestrator struct {
    planner      Planner
    executor     Executor
    synthesizer  Synthesizer
    strategies   []Strategy
    config       Config
    metrics      *Metrics
}

type Config struct {
    DefaultStrategy string
    MaxParallel     int
    EnableTracing   bool
    Timeout         time.Duration
}

func New(opts ...Option) *DefaultOrchestrator {
    o := &DefaultOrchestrator{
        config: DefaultConfig(),
    }
    for _, opt := range opts {
        opt(o)
    }
    return o
}

func (o *DefaultOrchestrator) Execute(ctx context.Context, req *Request) (*Response, error) {
    ctx, span := tracer.Start(ctx, "Orchestrator.Execute")
    defer span.End()

    start := time.Now()

    // Select strategy
    strategy, err := o.selectStrategy(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("strategy selection: %w", err)
    }

    // Create plan
    planCtx := &PlanContext{
        Request:  req,
        Strategy: strategy.Name(),
    }
    plan, err := o.planner.Plan(ctx, req.Query, planCtx)
    if err != nil {
        return nil, fmt.Errorf("planning: %w", err)
    }

    // Execute strategy
    result, err := strategy.Execute(ctx, req.Query, o.executor)
    if err != nil {
        return nil, fmt.Errorf("execution: %w", err)
    }

    // Synthesize if multiple results
    var content string
    var confidence float64

    if len(result.SubResults) > 1 {
        synthCtx := &SynthContext{
            OriginalQuery: req.Query,
            Plan:          plan,
        }
        synthesis, err := o.synthesizer.Synthesize(ctx, result.SubResults, synthCtx)
        if err != nil {
            return nil, fmt.Errorf("synthesis: %w", err)
        }
        content = synthesis.Content
        confidence = synthesis.Confidence
    } else if len(result.SubResults) == 1 {
        content = result.SubResults[0].Content
        confidence = result.SubResults[0].Confidence
    }

    return &Response{
        ID:         req.ID,
        Content:    content,
        Confidence: confidence,
        TokensUsed: result.TotalTokens,
        Duration:   time.Since(start),
        Trace:      result.Trace,
    }, nil
}

func (o *DefaultOrchestrator) selectStrategy(ctx context.Context, req *Request) (Strategy, error) {
    stratCtx := &StrategyContext{
        Query:       req.Query,
        ContextSize: sumTokens(req.Context),
        Constraints: req.Constraints,
    }

    var best Strategy
    var bestScore float64

    for _, s := range o.strategies {
        applicable, score := s.Applicable(ctx, req.Query, stratCtx)
        if applicable && score > bestScore {
            best = s
            bestScore = score
        }
    }

    if best == nil {
        return nil, errors.New("no applicable strategy found")
    }

    return best, nil
}
```

### Options Pattern

```go
// pkg/orchestrator/options.go

type Option func(*DefaultOrchestrator)

func WithPlanner(p Planner) Option {
    return func(o *DefaultOrchestrator) {
        o.planner = p
    }
}

func WithExecutor(e Executor) Option {
    return func(o *DefaultOrchestrator) {
        o.executor = e
    }
}

func WithSynthesizer(s Synthesizer) Option {
    return func(o *DefaultOrchestrator) {
        o.synthesizer = s
    }
}

func WithStrategy(s Strategy) Option {
    return func(o *DefaultOrchestrator) {
        o.strategies = append(o.strategies, s)
    }
}

func WithConfig(c Config) Option {
    return func(o *DefaultOrchestrator) {
        o.config = c
    }
}
```

## Strategy Implementations

### Direct Strategy

```go
// pkg/orchestrator/strategy/direct.go

type DirectStrategy struct {
    llm LLMClient
}

func (s *DirectStrategy) Name() string { return "direct" }

func (s *DirectStrategy) Applicable(ctx context.Context, task string, c *StrategyContext) (bool, float64) {
    // Direct is always applicable but with varying scores
    if c.ContextSize < 5000 {
        return true, 0.8
    }
    if c.ContextSize < 20000 {
        return true, 0.5
    }
    return true, 0.2
}

func (s *DirectStrategy) Execute(ctx context.Context, task string, exec Executor) (*StrategyResult, error) {
    op := &Operation{
        ID:    generateID(),
        Type:  OpTypeLLMCall,
        Input: task,
    }

    result, err := exec.Execute(ctx, op)
    if err != nil {
        return nil, err
    }

    return &StrategyResult{
        Strategy:   "direct",
        SubResults: []*OperationResult{result},
        TotalTokens: result.Tokens,
    }, nil
}
```

### Decompose Strategy

```go
// pkg/orchestrator/strategy/decompose.go

type DecomposeStrategy struct {
    decomposer Decomposer
    parallel   bool
}

func (s *DecomposeStrategy) Name() string { return "decompose" }

func (s *DecomposeStrategy) Applicable(ctx context.Context, task string, c *StrategyContext) (bool, float64) {
    // Prefer decomposition for larger contexts
    if c.ContextSize > 10000 {
        return true, 0.9
    }
    if isMultiPartTask(task) {
        return true, 0.85
    }
    return true, 0.3
}

func (s *DecomposeStrategy) Execute(ctx context.Context, task string, exec Executor) (*StrategyResult, error) {
    // Decompose task
    chunks, err := s.decomposer.Decompose(ctx, task)
    if err != nil {
        return nil, err
    }

    // Create operations
    ops := make([]*Operation, len(chunks))
    for i, chunk := range chunks {
        ops[i] = &Operation{
            ID:       fmt.Sprintf("chunk-%d", i),
            Type:     OpTypeLLMCall,
            Input:    chunk,
            Priority: len(chunks) - i,
        }
    }

    // Execute
    var results []*OperationResult
    if s.parallel {
        results, err = exec.ExecuteBatch(ctx, ops)
    } else {
        for _, op := range ops {
            r, err := exec.Execute(ctx, op)
            if err != nil {
                return nil, err
            }
            results = append(results, r)
        }
    }

    if err != nil {
        return nil, err
    }

    return &StrategyResult{
        Strategy:    "decompose",
        SubResults:  results,
        TotalTokens: sumTokens(results),
    }, nil
}
```

### ToT Strategy

```go
// pkg/orchestrator/strategy/tot.go

type ToTStrategy struct {
    totController *tot.Controller
}

func (s *ToTStrategy) Name() string { return "tree-of-thoughts" }

func (s *ToTStrategy) Applicable(ctx context.Context, task string, c *StrategyContext) (bool, float64) {
    // ToT for complex reasoning tasks
    if containsReasoningKeywords(task) {
        return true, 0.85
    }
    if c.Constraints != nil && c.Constraints.MinConfidence > 0.8 {
        return true, 0.75
    }
    return false, 0
}

func (s *ToTStrategy) Execute(ctx context.Context, task string, exec Executor) (*StrategyResult, error) {
    solution, err := s.totController.Solve(ctx, task)
    if err != nil {
        return nil, err
    }

    return &StrategyResult{
        Strategy: "tree-of-thoughts",
        SubResults: []*OperationResult{{
            Content:    solution.Result.Content,
            Confidence: solution.Path[len(solution.Path)-1].Value,
            Tokens:     solution.TreeStats.TokensUsed,
        }},
        TotalTokens: solution.TreeStats.TokensUsed,
        Trace:       solution.ToTrace(),
    }, nil
}
```

## Executor Implementation

### Default Executor

```go
// pkg/orchestrator/executor/default.go

type DefaultExecutor struct {
    llm       LLMClient
    repl      REPLManager
    memory    MemoryStore
    tools     ToolRegistry
    parallel  int
}

func (e *DefaultExecutor) Execute(ctx context.Context, op *Operation) (*OperationResult, error) {
    switch op.Type {
    case OpTypeLLMCall:
        return e.executeLLM(ctx, op)
    case OpTypeREPLExec:
        return e.executeREPL(ctx, op)
    case OpTypeMemoryQuery:
        return e.queryMemory(ctx, op)
    case OpTypeToolCall:
        return e.executeTool(ctx, op)
    default:
        return nil, fmt.Errorf("unknown operation type: %d", op.Type)
    }
}

func (e *DefaultExecutor) ExecuteBatch(ctx context.Context, ops []*Operation) ([]*OperationResult, error) {
    results := make([]*OperationResult, len(ops))
    sem := make(chan struct{}, e.parallel)
    var mu sync.Mutex
    var firstErr error

    g, ctx := errgroup.WithContext(ctx)

    for i, op := range ops {
        i, op := i, op
        g.Go(func() error {
            sem <- struct{}{}
            defer func() { <-sem }()

            result, err := e.Execute(ctx, op)
            if err != nil {
                mu.Lock()
                if firstErr == nil {
                    firstErr = err
                }
                mu.Unlock()
                return nil // Continue with other ops
            }

            mu.Lock()
            results[i] = result
            mu.Unlock()
            return nil
        })
    }

    g.Wait()
    return results, firstErr
}

func (e *DefaultExecutor) executeLLM(ctx context.Context, op *Operation) (*OperationResult, error) {
    response, usage, err := e.llm.Complete(ctx, op.Input)
    if err != nil {
        return nil, err
    }

    return &OperationResult{
        ID:      op.ID,
        Content: response,
        Tokens:  usage.TotalTokens,
    }, nil
}
```

## Builder Pattern

### Orchestrator Builder

```go
// pkg/orchestrator/builder.go

type Builder struct {
    planner     Planner
    executor    Executor
    synthesizer Synthesizer
    strategies  []Strategy
    config      Config
    providers   *Providers
}

type Providers struct {
    LLM    LLMClient
    REPL   REPLManager
    Memory MemoryStore
    Tools  ToolRegistry
}

func NewBuilder() *Builder {
    return &Builder{
        config: DefaultConfig(),
    }
}

func (b *Builder) WithProviders(p *Providers) *Builder {
    b.providers = p
    return b
}

func (b *Builder) WithDefaultStrategies() *Builder {
    b.strategies = []Strategy{
        NewDirectStrategy(b.providers.LLM),
        NewDecomposeStrategy(NewDefaultDecomposer(b.providers.LLM), true),
        NewToTStrategy(tot.NewController(b.providers.LLM)),
        NewLATSStrategy(lats.NewController(b.providers)),
    }
    return b
}

func (b *Builder) WithStrategy(s Strategy) *Builder {
    b.strategies = append(b.strategies, s)
    return b
}

func (b *Builder) WithConfig(c Config) *Builder {
    b.config = c
    return b
}

func (b *Builder) Build() (*DefaultOrchestrator, error) {
    if b.providers == nil {
        return nil, errors.New("providers required")
    }

    if b.planner == nil {
        b.planner = NewDefaultPlanner(b.providers.LLM)
    }

    if b.executor == nil {
        b.executor = NewDefaultExecutor(b.providers, b.config.MaxParallel)
    }

    if b.synthesizer == nil {
        b.synthesizer = NewDefaultSynthesizer(b.providers.LLM)
    }

    return New(
        WithPlanner(b.planner),
        WithExecutor(b.executor),
        WithSynthesizer(b.synthesizer),
        WithConfig(b.config),
    ), nil
}
```

## Testing Support

### Mock Implementations

```go
// pkg/orchestrator/testing/mocks.go

type MockExecutor struct {
    ExecuteFunc      func(ctx context.Context, op *Operation) (*OperationResult, error)
    ExecuteBatchFunc func(ctx context.Context, ops []*Operation) ([]*OperationResult, error)
}

func (m *MockExecutor) Execute(ctx context.Context, op *Operation) (*OperationResult, error) {
    if m.ExecuteFunc != nil {
        return m.ExecuteFunc(ctx, op)
    }
    return &OperationResult{ID: op.ID, Content: "mock result"}, nil
}

type MockStrategy struct {
    name       string
    applicable bool
    score      float64
    result     *StrategyResult
}

func (m *MockStrategy) Name() string { return m.name }

func (m *MockStrategy) Applicable(ctx context.Context, task string, c *StrategyContext) (bool, float64) {
    return m.applicable, m.score
}

func (m *MockStrategy) Execute(ctx context.Context, task string, exec Executor) (*StrategyResult, error) {
    return m.result, nil
}
```

## Success Criteria

1. **Modularity**: Each component testable in isolation
2. **Composability**: Easy to swap strategies
3. **Performance**: No regression from refactoring
4. **Coverage**: 90%+ test coverage on new package
5. **Migration**: Seamless transition from current code
