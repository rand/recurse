# Recommendations for Improving Recurse

**Based on**: Deep analysis of [rlm-claude-code](https://github.com/alexzhang13/rlm) and the [RLM research paper](https://arxiv.org/abs/2512.24601) by Zhang, Kraska, and Khattab.

**Date**: 2026-01-13

---

## Executive Summary

Recurse is a well-architected Go implementation of RLM principles with ~27K lines of core RLM/memory code, comprehensive tiered memory, reasoning traces, and full TUI integration. The project successfully implements Phases 1-4 of its roadmap with all tests passing.

This document presents **research-grounded recommendations** to advance Recurse from a functional implementation to a state-of-the-art RLM system. Recommendations are organized by:

1. **Critical Gaps** - High-impact improvements that address known limitations
2. **Intelligence Enhancements** - Better reasoning and decomposition
3. **Performance Optimizations** - Latency, cost, and scaling
4. **Reliability & Resilience** - Robustness guarantees
5. **Learning & Adaptation** - Self-improvement over time
6. **Architecture Refinements** - Maintainability and extensibility

---

## Part I: What Recurse Does Well

Before identifying improvements, acknowledge the strong foundations:

| Strength | Implementation |
|----------|----------------|
| **True context externalization** | REPL variables for conversation, files, tool_outputs |
| **Intelligent orchestration** | Meta-controller (Haiku 4.5) decides strategy |
| **Tiered hypergraph memory** | Task → Session → Long-term with evolution |
| **Full reasoning traces** | Deciduous-style decision graphs linked to git |
| **Comprehensive TUI** | Budget status, memory inspector, RLM trace view |
| **Task classification** | Rule-based + LLM fallback for activation decisions |
| **Multi-provider support** | OpenRouter + Anthropic with intelligent routing |
| **Strong test coverage** | 101 test files, all passing |

The architecture is sound. The recommendations below are enhancements, not corrections.

---

## Part II: Critical Gaps

### 2.1 Async Recursive Calls (HIGH PRIORITY)

**Research basis**: The RLM paper explicitly notes "lack of asynchrony can cause each query to range from a few seconds to several minutes" as a key limitation.

**Current state**: Recurse processes pending operations serially.

**Recommendation**: Implement full async pipeline with speculative execution.

```go
// internal/rlm/async_executor.go

type AsyncRLMExecutor struct {
    maxConcurrency int
    semaphore      chan struct{}
}

func (e *AsyncRLMExecutor) ExecuteParallel(
    ctx context.Context,
    ops []DeferredOperation,
) ([]Result, error) {
    g, ctx := errgroup.WithContext(ctx)
    results := make([]Result, len(ops))

    for i, op := range ops {
        i, op := i, op
        g.Go(func() error {
            e.semaphore <- struct{}{}
            defer func() { <-e.semaphore }()

            result, err := e.executeOne(ctx, op)
            if err != nil {
                return err
            }
            results[i] = result
            return nil
        })
    }

    return results, g.Wait()
}

// Speculative execution: try multiple approaches, use first success
func (e *AsyncRLMExecutor) SpeculativeExecute(
    ctx context.Context,
    primary DeferredOperation,
    alternatives []DeferredOperation,
) (Result, error) {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    resultCh := make(chan Result, 1)

    // Race primary against alternatives
    for _, op := range append([]DeferredOperation{primary}, alternatives...) {
        go func(op DeferredOperation) {
            if result, err := e.executeOne(ctx, op); err == nil {
                select {
                case resultCh <- result:
                    cancel() // Cancel other goroutines
                default:
                }
            }
        }(op)
    }

    select {
    case result := <-resultCh:
        return result, nil
    case <-ctx.Done():
        return Result{}, ctx.Err()
    }
}
```

**Expected impact**: 3-5x latency reduction for multi-call queries.

### 2.2 Embedding Integration (HIGH PRIORITY)

**Current state**: Embedding model is stubbed but not integrated. Memory queries use keyword matching only.

**Research basis**: [A-MEM](https://arxiv.org/abs/2502.12110) and [Zep](https://blog.getzep.com/) demonstrate significant improvements with semantic retrieval.

**Recommendation**: Integrate Voyage-3 or VoyageCode-3 for semantic memory search.

```go
// internal/memory/hypergraph/embeddings.go

type EmbeddingClient interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type VoyageClient struct {
    apiKey string
    model  string // "voyage-3" or "voyage-code-3"
}

func (c *VoyageClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    // Call Voyage API
    // Return 1024-dimensional embeddings
}

// Hybrid search combining FTS5 + vector similarity
func (s *Store) HybridSearch(
    ctx context.Context,
    query string,
    opts SearchOptions,
) ([]Node, error) {
    // 1. Get keyword matches via FTS5
    keywordResults := s.fts5Search(query, opts.Limit*2)

    // 2. Get semantic matches via embedding
    queryEmbedding, _ := s.embedder.Embed(ctx, []string{query})
    semanticResults := s.vectorSearch(queryEmbedding[0], opts.Limit*2)

    // 3. Combine with reciprocal rank fusion
    return s.reciprocalRankFusion(keywordResults, semanticResults, opts.Alpha)
}
```

**Schema addition**:
```sql
-- SQLite vec extension for vector search
CREATE VIRTUAL TABLE node_embeddings USING vec0(
    node_id TEXT PRIMARY KEY,
    embedding FLOAT[1024]  -- Voyage-3 dimension
);
```

### 2.3 Prompt Caching (HIGH PRIORITY)

**Research basis**: Claude's prompt caching can reduce costs by 90% and latency by 85% for repeated context.

**Current state**: No explicit prompt caching strategy.

**Recommendation**: Structure recursive calls to maximize cache hits.

```go
// internal/rlm/cache.go

type CacheAwareClient struct {
    cachePrefix map[string]string // query_type -> cached prefix
}

func (c *CacheAwareClient) PrepareCacheablePrompt(
    sharedContext string,
    querySpecific string,
) anthropic.MessageParams {
    return anthropic.MessageParams{
        System: []anthropic.SystemBlock{
            {
                Type: anthropic.SystemBlockTypeText,
                Text: sharedContext,
                CacheControl: &anthropic.CacheControlEphemeral{},
            },
        },
        Messages: []anthropic.MessageParam{
            {Role: "user", Content: querySpecific},
        },
    }
}
```

**Key insight**: Place stable context (file contents, conversation history) first with cache control. Query-specific content goes last.

---

## Part III: Intelligence Enhancements

### 3.1 Tree of Thoughts Integration

**Research basis**: [Tree of Thoughts (NeurIPS 2023)](https://arxiv.org/abs/2305.10601) showed 4%-74% improvement on complex reasoning tasks through deliberate exploration with backtracking.

**Current state**: Recurse uses linear recursive decomposition without explicit exploration of alternative paths.

**Recommendation**: Hybrid ToT-RLM architecture for complex reasoning.

```go
// internal/rlm/tot/tree.go

type ThoughtNode struct {
    ID            string
    Thought       string
    State         map[string]any
    Children      []*ThoughtNode
    ValueEstimate float64
    IsTerminal    bool
    Parent        *ThoughtNode
}

type ToTConfig struct {
    MaxBranches   int     // Thoughts to generate per node
    MaxDepth      int     // Maximum tree depth
    ValueThreshold float64 // Pruning threshold
    Strategy      string   // "bfs" | "dfs" | "best_first"
}

type ThoughtTree struct {
    config ToTConfig
    root   *ThoughtNode
}

func (t *ThoughtTree) Branch(node *ThoughtNode, thoughts []string) []*ThoughtNode {
    children := make([]*ThoughtNode, len(thoughts))
    for i, thought := range thoughts {
        children[i] = &ThoughtNode{
            ID:      uuid.New().String(),
            Thought: thought,
            Parent:  node,
            State:   cloneState(node.State),
        }
    }
    node.Children = children
    return children
}

func (t *ThoughtTree) EvaluateState(ctx context.Context, node *ThoughtNode) float64 {
    // Use LLM to self-evaluate progress toward goal
    prompt := fmt.Sprintf(
        "Given the goal and current reasoning state, rate progress 0-1.\n"+
        "Goal: %s\nState: %v\nRating:",
        t.goal, node.State,
    )
    // Call meta-controller for evaluation
    return t.metaCtrl.Evaluate(ctx, prompt)
}

func (t *ThoughtTree) Backtrack(to *ThoughtNode) {
    // Restore state to previous node for alternative exploration
    t.currentNode = to
}
```

**When to activate ToT**:
- Verification chains (need to check multiple approaches)
- Architectural decisions (multiple valid designs)
- Deep debugging (trace through multiple hypotheses)

### 3.2 LATS-Inspired Tool Orchestration

**Research basis**: [LATS (ICML 2024)](https://arxiv.org/abs/2310.04406) uses MCTS for tool orchestration, doubling ReAct performance on HotPotQA.

**Current state**: Tool calls are reactive and unplanned.

**Recommendation**: Planning phase before tool execution with UCB1 exploration.

```go
// internal/rlm/lats/orchestrator.go

type LATSOrchestrator struct {
    explorationWeight float64 // UCB1 constant (default: 1.414)
    maxRollouts       int
    maxDepth          int
}

type ToolPlan struct {
    Goal         string
    Steps        []ToolStep
    Dependencies map[int][]int // step -> depends_on_steps
    Confidence   float64
}

type ToolStep struct {
    StepID         int
    Tool           string
    Args           map[string]any
    ExpectedOutput string
    Fallback       *ToolStep
}

func (o *LATSOrchestrator) Plan(
    ctx context.Context,
    query string,
    availableTools []Tool,
) (*ToolPlan, error) {
    // 1. PLAN: Generate initial tool sequence
    initialPlan := o.generatePlan(ctx, query, availableTools)

    // 2. MCTS: Refine plan through exploration
    for rollout := 0; rollout < o.maxRollouts; rollout++ {
        // Select node with UCB1
        node := o.selectWithUCB1(initialPlan)

        // Expand: generate alternative tool actions
        children := o.expand(ctx, node, availableTools)

        // Simulate: estimate outcome
        value := o.simulate(ctx, children[0])

        // Backpropagate: update value estimates
        o.backpropagate(node, value)
    }

    // 3. Extract best path
    return o.extractBestPlan(initialPlan), nil
}

func (o *LATSOrchestrator) computeUCB1(node *ToolNode) float64 {
    if node.Visits == 0 {
        return math.Inf(1)
    }
    exploitation := node.Value / float64(node.Visits)
    exploration := o.explorationWeight * math.Sqrt(
        2*math.Log(float64(node.Parent.Visits))/float64(node.Visits),
    )
    return exploitation + exploration
}
```

### 3.3 Proactive REPL Computation

**Research basis**: [ARTIST (Microsoft, 2025)](https://www.microsoft.com/en-us/research/wp-content/uploads/2025/04/AgenticReasoning.pdf) demonstrates that LLMs augmented with Python interpreters systematically decompose complex problems.

**Current state**: REPL is reactive—used when model decides, not proactively suggested.

**Recommendation**: Detect computation patterns and suggest REPL approach.

```go
// internal/rlm/proactive.go

type ComputationAdvisor struct {
    patterns []ComputationPattern
}

type ComputationPattern struct {
    Name    string
    Regex   *regexp.Regexp
    Handler func(query string, context SessionContext) *REPLSuggestion
}

var DefaultPatterns = []ComputationPattern{
    {
        Name:  "counting",
        Regex: regexp.MustCompile(`(?i)\b(how many|count|number of|total)\b`),
        Handler: func(q string, ctx SessionContext) *REPLSuggestion {
            return &REPLSuggestion{
                Code: "len([x for x in data if condition])",
                Reason: "Use REPL for counting (more reliable than LLM arithmetic)",
            }
        },
    },
    {
        Name:  "arithmetic",
        Regex: regexp.MustCompile(`\b(\d+\s*[\+\-\*/]\s*\d+|\bcalculate\b|\bcompute\b)`),
        Handler: arithmeticHandler,
    },
    {
        Name:  "sorting",
        Regex: regexp.MustCompile(`(?i)\b(sort|order|rank|largest|smallest|top \d+)\b`),
        Handler: sortingHandler,
    },
}

func (a *ComputationAdvisor) SuggestREPL(
    query string,
    context SessionContext,
) *REPLSuggestion {
    for _, pattern := range a.patterns {
        if pattern.Regex.MatchString(query) {
            return pattern.Handler(query, context)
        }
    }
    return nil
}
```

### 3.4 Formal Verification for Code Changes

**Research basis**: [PREFACE (GLSVLSI 2025)](https://dl.acm.org/doi/10.1145/3716368.3735300) achieves formally verifiable code generation via RL-guided prompt repair.

**Current state**: CPMpy is mentioned in spec but not integrated.

**Recommendation**: Verification-aware decomposition for code tasks.

```go
// internal/rlm/verify/chain.go

type VerificationChain struct {
    solver ConstraintSolver // CPMpy via Python REPL
}

type Constraint struct {
    Type       string // "type_check" | "call_graph" | "invariant"
    Expression string
    Confidence float64
}

func (c *VerificationChain) GeneratePreconditions(
    ctx context.Context,
    change CodeChange,
) ([]Constraint, error) {
    // Use LLM to extract preconditions from docstrings/comments
    prompt := fmt.Sprintf(
        "Extract preconditions for this code change:\n%s\n"+
        "Output as JSON: [{\"type\": \"...\", \"expression\": \"...\"}]",
        change.Diff,
    )
    // Parse and return constraints
}

func (c *VerificationChain) Verify(
    ctx context.Context,
    constraints []Constraint,
    updatedCode string,
) (*VerificationResult, error) {
    // Execute CPMpy in REPL to check constraints
    code := c.buildVerificationCode(constraints, updatedCode)
    result, err := c.repl.Execute(ctx, code)
    if err != nil {
        return nil, err
    }
    return parseVerificationResult(result)
}
```

---

## Part IV: Performance Optimizations

### 4.1 Context Compression

**Research basis**: [KVzip (SNU, 2025)](https://techxplore.com/news/2025-11-ai-tech-compress-llm-chatbot.html) achieves 3-4x memory compression with 2x latency reduction.

**Recommendation**: Two-stage compression (extractive then abstractive).

```go
// internal/rlm/compress/compressor.go

type ContextCompressor struct {
    extractiveThreshold int // Token count to trigger extractive
    abstractiveThreshold int // Token count to trigger abstractive
}

func (c *ContextCompressor) Compress(
    ctx context.Context,
    content string,
    targetTokens int,
) (string, error) {
    currentTokens := c.countTokens(content)

    if currentTokens <= targetTokens {
        return content, nil
    }

    // Stage 1: Extractive (fast, lossless for key info)
    if currentTokens > c.extractiveThreshold {
        content = c.extractive(content, targetTokens*2)
    }

    // Stage 2: Abstractive (if still over budget)
    if c.countTokens(content) > targetTokens {
        content = c.abstractive(ctx, content, targetTokens)
    }

    return content, nil
}

func (c *ContextCompressor) extractive(content string, target int) string {
    // Score sentences by relevance, keep top-scoring
    sentences := c.splitSentences(content)
    scores := c.scoreSentences(sentences)
    return c.selectTopSentences(sentences, scores, target)
}

func (c *ContextCompressor) abstractive(
    ctx context.Context,
    content string,
    target int,
) string {
    // Use LLM to summarize while preserving key information
    prompt := fmt.Sprintf(
        "Summarize preserving all technical details in %d tokens:\n%s",
        target, content,
    )
    return c.llm.Complete(ctx, prompt)
}
```

### 4.2 Compute-Optimal Depth Allocation

**Research basis**: [Inference Scaling Laws (ICLR 2025)](https://arxiv.org/abs/2408.03314) demonstrated 4x efficiency gains through adaptive per-prompt compute allocation.

**Current state**: Static depth budget (default=2) regardless of query difficulty.

**Recommendation**: Adaptive depth based on difficulty estimation.

```go
// internal/rlm/compute.go

type ComputeAllocation struct {
    DepthBudget   int
    ModelTier     string // "haiku" | "sonnet" | "opus"
    ParallelCalls int
    TimeoutMS     int
    EstimatedCost float64
}

func AllocateCompute(
    query string,
    context SessionContext,
    totalBudget float64,
) ComputeAllocation {
    difficulty := estimateDifficulty(query, context)

    switch {
    case difficulty < 0.3:
        // Easy: shallow, cheap model
        return ComputeAllocation{
            DepthBudget:   1,
            ModelTier:     "haiku",
            ParallelCalls: 1,
            TimeoutMS:     10000,
        }
    case difficulty < 0.7:
        // Medium: balanced
        return ComputeAllocation{
            DepthBudget:   2,
            ModelTier:     "sonnet",
            ParallelCalls: 3,
            TimeoutMS:     30000,
        }
    default:
        // Hard: deep exploration with best model
        return ComputeAllocation{
            DepthBudget:   3,
            ModelTier:     "opus",
            ParallelCalls: 5,
            TimeoutMS:     120000,
        }
    }
}

func estimateDifficulty(query string, ctx SessionContext) float64 {
    difficulty := 0.0

    signals := extractComplexitySignals(query, ctx)

    if signals.RequiresMultiStep {
        difficulty += 0.3
    }
    if signals.CrossesDomains {
        difficulty += 0.2
    }
    if signals.AmbiguityScore > 0.5 {
        difficulty += 0.2
    }
    difficulty += math.Min(0.2, float64(ctx.TotalTokens)/100000)

    return math.Min(1.0, difficulty)
}
```

**Key insight**: Smaller models + more inference compute often beats larger models. Consider Haiku at depth=3 vs Opus at depth=1.

---

## Part V: Reliability & Resilience

### 5.1 Circuit Breaker for Recursive Calls

**Research basis**: Standard reliability pattern for distributed systems; prevents cascade failures.

**Current state**: Failed recursive calls retry but no circuit breaker.

**Recommendation**: Per-model-tier circuit breaker.

```go
// internal/rlm/resilience/breaker.go

type CircuitState int

const (
    StateClosed   CircuitState = iota // Normal operation
    StateOpen                         // Failing, reject calls
    StateHalfOpen                     // Testing recovery
)

type CircuitBreaker struct {
    state           CircuitState
    failureCount    int
    failureThreshold int
    recoveryTimeout time.Duration
    lastFailureTime time.Time
    mu              sync.Mutex
}

func (b *CircuitBreaker) Call(fn func() error) error {
    b.mu.Lock()

    switch b.state {
    case StateOpen:
        if time.Since(b.lastFailureTime) > b.recoveryTimeout {
            b.state = StateHalfOpen
        } else {
            b.mu.Unlock()
            return ErrCircuitOpen
        }
    }

    b.mu.Unlock()

    err := fn()

    b.mu.Lock()
    defer b.mu.Unlock()

    if err != nil {
        b.failureCount++
        b.lastFailureTime = time.Now()
        if b.failureCount >= b.failureThreshold {
            b.state = StateOpen
        }
        return err
    }

    // Success - reset
    b.failureCount = 0
    b.state = StateClosed
    return nil
}
```

### 5.2 Execution Guarantees

**Research basis**: The RLM paper acknowledges "lack of strong guarantees about controlling total API cost or total runtime."

**Current state**: Soft limits exist but can be exceeded.

**Recommendation**: Hard execution boundaries with graceful degradation.

```go
// internal/rlm/guarantees.go

type ExecutionGuarantees struct {
    maxCostUSD      float64
    maxDuration     time.Duration
    maxRecursiveCalls int

    costUsed        float64
    deadline        time.Time
    callsUsed       int
}

func (g *ExecutionGuarantees) CanProceed(estimatedCost float64) bool {
    if time.Now().After(g.deadline) {
        return false
    }
    if g.costUsed + estimatedCost > g.maxCostUSD {
        return false
    }
    if g.callsUsed >= g.maxRecursiveCalls {
        return false
    }
    return true
}

func (g *ExecutionGuarantees) OnBudgetExhausted() *DegradationPlan {
    remaining := g.deadline.Sub(time.Now())

    return &DegradationPlan{
        Strategy: "synthesize_partial",
        Message:  fmt.Sprintf("Budget exhausted. Synthesizing from %d partial results.", g.callsUsed),
        TimeRemaining: remaining,
    }
}
```

### 5.3 Confidence-Weighted Synthesis

**Research basis**: The RLM paper notes quality variance in recursive call results. Weighting by confidence improves aggregation.

**Recommendation**: Track and use confidence in result synthesis.

```go
// internal/rlm/synthesize/weighted.go

type RecursiveResult struct {
    Content        string
    Confidence     float64
    ReasoningTrace []string
    Cost           float64
}

func SynthesizeWeighted(results []RecursiveResult) SynthesisResult {
    // Weight results by confidence
    totalWeight := 0.0
    for _, r := range results {
        totalWeight += r.Confidence
    }

    // Build weighted synthesis prompt
    var parts []string
    for _, r := range results {
        weight := r.Confidence / totalWeight
        parts = append(parts, fmt.Sprintf(
            "[Weight: %.2f] %s", weight, r.Content,
        ))
    }

    // LLM synthesizes with awareness of confidence
    return llmSynthesize(parts, "weighted")
}
```

---

## Part VI: Learning & Adaptation

### 6.1 Continuous Learning from Outcomes

**Research basis**: [Agent Lightning (Microsoft, 2025)](https://arxiv.org/abs/2508.03680) enables RL-based training with zero code modifications.

**Current state**: Strategy cache provides basic pattern matching but no true learning.

**Recommendation**: Outcome-based learning loop.

```go
// internal/rlm/learning/learner.go

type ExecutionOutcome struct {
    Query           string
    QueryFeatures   QueryFeatures
    StrategyUsed    string
    ModelUsed       string
    DepthReached    int
    ToolsUsed       []string
    Success         bool
    QualityScore    float64 // User feedback or automatic evaluation
    Cost            float64
    LatencyMS       int64
}

type ContinuousLearner struct {
    routingAdjustments  map[string]float64 // "query_type:model" -> adjustment
    strategyPreferences map[string]float64 // "query_type:strategy" -> preference
    learningRate        float64
    persistence         *sql.DB
}

func (l *ContinuousLearner) RecordOutcome(outcome ExecutionOutcome) {
    // Extract signals
    signal := l.extractSignal(outcome)

    // Update routing preferences
    key := fmt.Sprintf("%s:%s", outcome.QueryFeatures.PrimaryType, outcome.ModelUsed)
    current := l.routingAdjustments[key]

    if outcome.Success && outcome.QualityScore > 0.8 {
        // Reinforce this routing
        l.routingAdjustments[key] = current + l.learningRate
    } else if !outcome.Success {
        // Discourage this routing
        l.routingAdjustments[key] = current - l.learningRate
    }

    // Persist for cross-session learning
    l.persist(outcome, signal)
}

func (l *ContinuousLearner) GetRoutingAdjustment(queryType, model string) float64 {
    return l.routingAdjustments[fmt.Sprintf("%s:%s", queryType, model)]
}
```

### 6.2 Learned Model Routing

**Research basis**: [RouteLLM (ICLR 2025)](https://github.com/lm-sys/RouteLLM) achieves 85% cost reduction while maintaining 95% of GPT-4 performance.

**Current state**: Smart router uses heuristics but no learned preferences.

**Recommendation**: Cascading router with outcome-based learning.

```go
// internal/rlm/routing/learned.go

type LearnedRouter struct {
    modelProfiles     map[string]ModelProfile
    learner           *ContinuousLearner
    confidenceThreshold float64
    cascadeOrder      []string // e.g., ["haiku", "sonnet", "opus"]
}

type ModelProfile struct {
    Strengths    []string
    CostPer1K    float64
    QualityBase  float64
}

func (r *LearnedRouter) Route(
    query string,
    context SessionContext,
    costSensitivity float64,
) RoutingDecision {
    features := extractFeatures(query, context)
    difficulty := estimateDifficulty(features)

    scores := make(map[string]float64)
    for model, profile := range r.modelProfiles {
        // Base quality estimate
        quality := r.estimateQuality(model, features)

        // Apply learned adjustments
        adjustment := r.learner.GetRoutingAdjustment(features.PrimaryType, model)
        quality += adjustment

        // Cost factor
        cost := 1.0 - (profile.CostPer1K / 0.015) // Normalized

        // Combined score
        scores[model] = (1-costSensitivity)*quality + costSensitivity*cost
    }

    best := maxKey(scores)
    return RoutingDecision{
        Model:      best,
        Confidence: scores[best],
        Reasoning:  r.explainRouting(best, features, scores),
    }
}

// Try cheaper models first, escalate on low confidence
func (r *LearnedRouter) CascadeRoute(
    ctx context.Context,
    query string,
    context SessionContext,
) (*CascadeResult, error) {
    for i, model := range r.cascadeOrder {
        result, err := r.execute(ctx, query, context, model)
        if err != nil {
            continue
        }

        confidence := r.estimateConfidence(result)
        if confidence >= r.confidenceThreshold {
            return &CascadeResult{
                Answer:     result,
                ModelUsed:  model,
                Escalations: i,
            }, nil
        }
    }

    // Return best effort from strongest model
    return r.execute(ctx, query, context, r.cascadeOrder[len(r.cascadeOrder)-1])
}
```

### 6.3 User Correction Learning

**Research basis**: RLHF and [ToTRL (2025)](https://arxiv.org/abs/2505.12717) show learning from feedback significantly improves reasoning.

**Recommendation**: Capture and learn from explicit user corrections.

```go
// internal/rlm/learning/corrections.go

type CorrectionType string

const (
    CorrectionClassifier CorrectionType = "classifier" // Wrong activation decision
    CorrectionExecution  CorrectionType = "execution"  // Wrong decomposition/result
    CorrectionRouting    CorrectionType = "routing"    // Wrong model choice
)

type UserCorrection struct {
    Query        string
    RLMOutput    string
    Correction   string
    Type         CorrectionType
    Timestamp    time.Time
}

type CorrectionLearner struct {
    corrections []UserCorrection
    learner     *ContinuousLearner
}

func (l *CorrectionLearner) RecordCorrection(c UserCorrection) {
    l.corrections = append(l.corrections, c)

    // Analyze patterns
    patterns := l.analyzePatterns()

    // Suggest adjustments
    if patterns.FrequentMisclassification != "" {
        log.Printf("Detected frequent misclassification for %s queries. "+
            "Consider adjusting classifier threshold.", patterns.FrequentMisclassification)
    }

    // Update learner
    l.learner.RecordCorrection(c)
}
```

---

## Part VII: Architecture Refinements

### 7.1 Modular Orchestrator Package

**Current state**: `rlm/controller.go`, `rlm/wrapper.go`, `rlm/orchestrator.go` have overlapping responsibilities.

**Recommendation**: Clean separation following rlm-claude-code's modular pattern.

```
internal/rlm/
├── orchestrator/
│   ├── core.go           # Base orchestration loop
│   ├── intelligent.go    # Claude-powered decisions
│   ├── async.go          # Async execution engine
│   ├── checkpointing.go  # Session persistence
│   └── steering.go       # User interaction
├── repl/
│   └── (existing)
├── decompose/
│   └── (existing)
└── synthesize/
    └── (existing)
```

### 7.2 REPL Plugin System

**Research basis**: The RLM paper shows emergent strategies vary by domain. Extensibility enables domain-specific functions.

**Recommendation**: Plugin system for domain-specific REPL functions.

```go
// internal/rlm/repl/plugin.go

type REPLPlugin interface {
    Name() string
    Functions() map[string]REPLFunction
    OnLoad(env *REPLEnvironment) error
}

type REPLFunction struct {
    Name        string
    Description string
    Handler     func(args ...any) (any, error)
}

type PluginManager struct {
    plugins map[string]REPLPlugin
}

func (m *PluginManager) Register(plugin REPLPlugin) error {
    m.plugins[plugin.Name()] = plugin
    return nil
}

// Example: Code analysis plugin
type CodeAnalysisPlugin struct{}

func (p *CodeAnalysisPlugin) Name() string { return "code_analysis" }

func (p *CodeAnalysisPlugin) Functions() map[string]REPLFunction {
    return map[string]REPLFunction{
        "ast_parse":     {Handler: astParse},
        "find_callers":  {Handler: findCallers},
        "find_callees":  {Handler: findCallees},
        "call_graph":    {Handler: buildCallGraph},
    }
}
```

### 7.3 Memory Backend Abstraction

**Current state**: `MemoryStore` directly implements SQLite operations.

**Recommendation**: Abstract interface for backend flexibility.

```go
// internal/memory/backend.go

type MemoryBackend interface {
    CreateNode(ctx context.Context, node Node) (string, error)
    GetNode(ctx context.Context, id string) (*Node, error)
    UpdateNode(ctx context.Context, id string, updates NodeUpdates) error
    DeleteNode(ctx context.Context, id string) error

    CreateEdge(ctx context.Context, edge Hyperedge) (string, error)
    GetEdge(ctx context.Context, id string) (*Hyperedge, error)

    Search(ctx context.Context, query string, opts SearchOptions) ([]Node, error)
    VectorSearch(ctx context.Context, embedding []float32, opts SearchOptions) ([]Node, error)

    Close() error
}

type SQLiteBackend struct {
    db *sql.DB
}

type InMemoryBackend struct {
    nodes map[string]Node
    edges map[string]Hyperedge
}

// Future: PostgresBackend for team scenarios
// Future: DuckDBBackend for analytics
```

---

## Part VIII: Implementation Roadmap

### Phase A: Critical Infrastructure (Highest Impact)

| Item | Impact | Effort | Rationale |
|------|--------|--------|-----------|
| Async recursive calls | HIGH | Medium | 3-5x latency reduction |
| Embedding integration | HIGH | Low | Semantic memory search |
| Prompt caching | HIGH | Low | 90% cost reduction potential |
| Circuit breaker | Medium | Low | Resilience |

### Phase B: Intelligence (Core Differentiation)

| Item | Impact | Effort | Rationale |
|------|--------|--------|-----------|
| Adaptive depth allocation | HIGH | Medium | 4x efficiency gains |
| ToT integration | HIGH | High | 4-74% reasoning improvement |
| LATS tool orchestration | HIGH | High | 2x tool use performance |
| Proactive REPL | Medium | Low | Better computation offloading |

### Phase C: Learning (Long-term Value)

| Item | Impact | Effort | Rationale |
|------|--------|--------|-----------|
| Continuous learner | Medium | Medium | Self-improvement over time |
| Learned routing | Medium | Medium | Cost-quality optimization |
| User correction learning | Medium | Low | Direct feedback integration |

### Phase D: Polish (Production Readiness)

| Item | Impact | Effort | Rationale |
|------|--------|--------|-----------|
| Context compression | Medium | Medium | Handle larger contexts |
| Formal verification | Medium | High | Correctness guarantees |
| Modular orchestrator | Low | Medium | Maintainability |
| REPL plugin system | Low | Medium | Extensibility |

---

## References

### Primary Sources
- [Recursive Language Models](https://arxiv.org/abs/2512.24601) - Zhang, Kraska, Khattab (MIT)
- [rlm-claude-code Implementation](https://github.com/alexzhang13/rlm)
- [Tree of Thoughts](https://arxiv.org/abs/2305.10601) - Yao et al. (NeurIPS 2023)
- [Inference Scaling Laws](https://arxiv.org/abs/2408.03314) - ICLR 2025

### Memory Systems
- [A-MEM: Agentic Memory](https://arxiv.org/abs/2502.12110) - NeurIPS 2025
- [Zep: Temporal Knowledge Graph](https://blog.getzep.com/)
- [HGMem Paper](https://arxiv.org/abs/2512.23959)
- [ACE: Context Evolution](https://arxiv.org/abs/2510.04618)

### Tool Orchestration
- [LATS](https://arxiv.org/abs/2310.04406) - ICML 2024
- [ARTIST](https://www.microsoft.com/en-us/research/wp-content/uploads/2025/04/AgenticReasoning.pdf) - Microsoft 2025
- [RouteLLM](https://github.com/lm-sys/RouteLLM) - ICLR 2025

### Context Engineering
- [Anthropic: Effective Context Engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)
- [Anthropic: Long-Running Agent Harnesses](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)

### Formal Verification
- [PREFACE](https://dl.acm.org/doi/10.1145/3716368.3735300) - GLSVLSI 2025

---

## Appendix: Measurement Framework

To track improvement, instrument these metrics:

### Intelligence Metrics
- **Decomposition quality**: % of queries where decomposition matches expert-labeled strategy
- **Synthesis accuracy**: % of synthesized answers rated correct by user/evaluator
- **Backtracking rate**: % of queries requiring backtracking (lower = better initial decomposition)

### Performance Metrics
- **P50/P95 latency**: End-to-end query completion time
- **Cost per query**: Total API cost, segmented by complexity
- **Cache hit rate**: % of recursive calls benefiting from prompt caching

### Reliability Metrics
- **Guarantee adherence**: % of queries completing within budget/time guarantees
- **Circuit breaker triggers**: Rate of circuit breaker activations
- **Recovery success**: % of checkpointed sessions successfully resumed

### Learning Metrics
- **Routing accuracy**: % of queries where initial model choice was optimal
- **User override rate**: % of queries where user forces RLM on/off
- **Correction rate**: % of RLM outputs requiring user correction
