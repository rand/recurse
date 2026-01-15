# SPEC-04: Learned Model Routing

> Dynamically select the optimal LLM for each task based on historical performance, cost constraints, and task characteristics.

## Overview

The model routing system learns which models work best for different task types, balancing quality, cost, and latency. It adapts over time based on outcomes and user feedback.

## Current State

- Static model selection regardless of task
- Expensive models used for simple tasks
- Cheap models fail on complex tasks
- Manual tuning required per use case

## Requirements

### [SPEC-04.01] Model Profiles

Track model capabilities and performance:

```go
type ModelProfile struct {
    ID              string
    Provider        string        // anthropic, openai, local
    Name            string        // claude-3-opus, gpt-4, etc.

    // Capabilities
    MaxTokens       int
    ContextWindow   int
    SupportsVision  bool
    SupportsTools   bool

    // Cost
    InputCostPer1K  float64
    OutputCostPer1K float64

    // Observed performance
    MedianLatency   time.Duration
    P95Latency      time.Duration
    SuccessRate     float64

    // Category scores (learned)
    CategoryScores  map[TaskCategory]float64
}
```

### [SPEC-04.02] Task Categories

Classify tasks for routing decisions:

| Category | Description | Examples |
|----------|-------------|----------|
| Simple | Direct answers, lookups | "What is X?" |
| Reasoning | Multi-step logic | "Why does X happen?" |
| Coding | Code generation/review | "Write a function..." |
| Creative | Writing, brainstorming | "Design a system..." |
| Analysis | Data analysis, summarization | "Summarize this..." |
| Conversation | Chat, clarification | "Can you explain...?" |

Features:
- Heuristic classification (fast, >85% confidence)
- LLM classification fallback (ambiguous cases)
- Caching for repeated patterns

### [SPEC-04.03] Task Features

Extract features for routing:

```go
type TaskFeatures struct {
    TokenCount        int
    HasCode           bool
    HasMath           bool
    HasImages         bool
    Languages         []string
    Category          TaskCategory
    EstimatedDepth    int       // Reasoning depth needed
    Ambiguity         float64   // 0=clear, 1=ambiguous
    ConversationTurns int
    PriorFailures     int
    Embedding         []float32 // For similarity matching
}
```

### [SPEC-04.04] Routing Algorithm

Score and select models:

```
Task → Extract Features → Find Similar Past Tasks
                              ↓
                     Score Each Model
                              ↓
             Quality + Cost + Latency + Historical
                              ↓
                     Select Best Model
```

Scoring weights (configurable):
- Quality: 0.4
- Cost: 0.3
- Latency: 0.1
- Historical: 0.2

### [SPEC-04.05] Routing Constraints

Support runtime constraints:

```go
type RoutingConstraints struct {
    MaxCostPerRequest    float64
    MaxLatency           time.Duration
    MinQuality           float64
    RequiredCapabilities []string
    ExcludedModels       []string
}
```

### [SPEC-04.06] Feedback Learning

Learn from outcomes:

| Outcome | Effect on Score |
|---------|-----------------|
| Success | +1.0 × learning rate |
| Corrected | +0.5 × learning rate |
| Failed | +0.0 × learning rate |

Features:
- Exponential moving average updates
- Weighted by task similarity
- Per-category score updates

### [SPEC-04.07] Routing Store

Track historical performance:

```sql
routing_history(id, timestamp, task, features, embedding,
                model_used, outcome, latency, tokens_used, cost, user_rating)

model_profiles(id, provider, name, success_rate, median_latency,
               p95_latency, category_scores, updated_at)
```

## Implementation Tasks

- [x] Define ModelProfile and TaskFeatures types (`internal/rlm/routing/types.go`)
- [x] Implement CategoryClassifier (heuristic + LLM) (`internal/rlm/routing/classifier.go`)
- [x] Create FeatureExtractor (`internal/rlm/routing/extractor.go`)
- [x] Build Router with scoring algorithm (`internal/rlm/routing/router.go`)
- [x] Implement RoutingStore (`internal/rlm/routing/store.go`)
- [x] Add RoutingLearner for feedback (`internal/rlm/routing/learner.go`)
- [x] Integrate with RLM Controller (`internal/rlm/routing/learned.go`)
- [x] Add explainability (reasoning for decisions) (`internal/rlm/routing/router.go`)
- [x] Add observability metrics (`internal/rlm/routing/router.go`)
- [x] Write tests (`internal/rlm/routing/*_test.go`)

## Dependencies

- `internal/memory/embeddings/` - Task embedding
- `internal/rlm/` - Controller integration
- `internal/budget/` - Cost tracking

## Acceptance Criteria

1. 30%+ reduction in API costs through appropriate model selection
2. <5% increase in correction rate vs always using best model
3. Measurable improvement in routing accuracy within 100 tasks
4. Routing decision overhead <50ms
5. Clear reasoning for every routing decision
