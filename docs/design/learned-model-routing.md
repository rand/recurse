# Learned Model Routing Design

> Design document for `recurse-dcn`: [SPEC] Learned Model Routing Design

## Overview

This document specifies a learned model routing system that dynamically selects the optimal LLM for each task based on historical performance data, cost constraints, and task characteristics. The system learns from outcomes to improve routing decisions over time.

## Problem Statement

### Current State

Static model selection:

```go
func (c *Controller) selectModel(task string) string {
    // Always use the same model regardless of task
    return c.config.DefaultModel
}
```

**Issues**:
- Expensive models used for simple tasks
- Cheap models fail on complex tasks
- No learning from past performance
- Manual tuning required for each use case

## Design Goals

1. **Adaptive selection**: Learn which models work best for which tasks
2. **Cost optimization**: Balance quality vs cost automatically
3. **Latency awareness**: Consider response time requirements
4. **Feedback integration**: Learn from user corrections and outcomes
5. **Explainable**: Provide reasoning for routing decisions

## Core Types

### Model Profile

```go
// internal/routing/types.go

type ModelProfile struct {
    ID          string
    Provider    string        // anthropic, openai, local
    Name        string        // claude-3-opus, gpt-4, etc.

    // Capabilities
    MaxTokens       int
    ContextWindow   int
    SupportsVision  bool
    SupportsTools   bool

    // Cost (per 1K tokens)
    InputCostPer1K  float64
    OutputCostPer1K float64

    // Observed performance
    MedianLatency   time.Duration
    P95Latency      time.Duration
    SuccessRate     float64

    // Category performance (learned)
    CategoryScores  map[TaskCategory]float64
}

type TaskCategory int

const (
    CategorySimple      TaskCategory = iota // Direct answers, simple queries
    CategoryReasoning                        // Multi-step logic
    CategoryCoding                           // Code generation/review
    CategoryCreative                         // Writing, brainstorming
    CategoryAnalysis                         // Data analysis, summarization
    CategoryConversation                     // Chat, clarification
)

type RoutingDecision struct {
    Model       *ModelProfile
    Confidence  float64
    Reasoning   string
    Alternatives []*ScoredModel
    Constraints *RoutingConstraints
}

type ScoredModel struct {
    Model      *ModelProfile
    Score      float64
    Components ScoreComponents
}

type ScoreComponents struct {
    QualityScore    float64
    CostScore       float64
    LatencyScore    float64
    CategoryMatch   float64
    HistoricalScore float64
}
```

### Task Features

```go
// internal/routing/features.go

type TaskFeatures struct {
    // Content analysis
    TokenCount      int
    HasCode         bool
    HasMath         bool
    HasImages       bool
    Languages       []string

    // Complexity indicators
    Category        TaskCategory
    EstimatedDepth  int     // Reasoning depth needed
    Ambiguity       float64 // 0=clear, 1=very ambiguous

    // Context
    ConversationTurns int
    PriorFailures     int

    // Embeddings for similarity matching
    Embedding       []float32
}

type FeatureExtractor struct {
    classifier  *CategoryClassifier
    embedder    EmbeddingProvider
}

func (e *FeatureExtractor) Extract(ctx context.Context, task string, context []Message) (*TaskFeatures, error) {
    features := &TaskFeatures{
        TokenCount: countTokens(task),
        HasCode:    containsCodePatterns(task),
        HasMath:    containsMathPatterns(task),
    }

    // Classify category
    category, confidence := e.classifier.Classify(task)
    features.Category = category

    // Estimate complexity
    features.EstimatedDepth = estimateReasoningDepth(task, context)
    features.Ambiguity = measureAmbiguity(task)

    // Generate embedding for similarity lookup
    embeddings, err := e.embedder.Embed(ctx, []string{task})
    if err == nil && len(embeddings) > 0 {
        features.Embedding = embeddings[0]
    }

    return features, nil
}
```

## Router Implementation

### Learned Router

```go
// internal/routing/router.go

type Router struct {
    models       []*ModelProfile
    store        *RoutingStore
    extractor    *FeatureExtractor
    constraints  *RoutingConstraints
    weights      *RoutingWeights
}

type RoutingConstraints struct {
    MaxCostPerRequest  float64
    MaxLatency         time.Duration
    MinQuality         float64
    RequiredCapabilities []string
    ExcludedModels     []string
}

type RoutingWeights struct {
    Quality    float64 // Weight for quality score
    Cost       float64 // Weight for cost score
    Latency    float64 // Weight for latency score
    Historical float64 // Weight for historical performance
}

func NewRouter(models []*ModelProfile, store *RoutingStore) *Router {
    return &Router{
        models:    models,
        store:     store,
        extractor: NewFeatureExtractor(),
        weights: &RoutingWeights{
            Quality:    0.4,
            Cost:       0.3,
            Latency:    0.1,
            Historical: 0.2,
        },
    }
}

func (r *Router) Route(
    ctx context.Context,
    task string,
    context []Message,
    constraints *RoutingConstraints,
) (*RoutingDecision, error) {
    // Extract task features
    features, err := r.extractor.Extract(ctx, task, context)
    if err != nil {
        return nil, fmt.Errorf("feature extraction: %w", err)
    }

    // Find similar past tasks
    similar, err := r.store.FindSimilar(ctx, features.Embedding, 10)
    if err != nil {
        return nil, fmt.Errorf("similarity search: %w", err)
    }

    // Score each model
    var scored []*ScoredModel
    for _, model := range r.models {
        if !r.meetsConstraints(model, features, constraints) {
            continue
        }

        score := r.scoreModel(model, features, similar, constraints)
        scored = append(scored, score)
    }

    if len(scored) == 0 {
        return nil, errors.New("no models meet constraints")
    }

    // Sort by score
    sort.Slice(scored, func(i, j int) bool {
        return scored[i].Score > scored[j].Score
    })

    best := scored[0]
    return &RoutingDecision{
        Model:       best.Model,
        Confidence:  best.Score,
        Reasoning:   r.explainDecision(best, features),
        Alternatives: scored[1:min(4, len(scored))],
        Constraints: constraints,
    }, nil
}

func (r *Router) scoreModel(
    model *ModelProfile,
    features *TaskFeatures,
    similar []*SimilarTask,
    constraints *RoutingConstraints,
) *ScoredModel {
    components := ScoreComponents{}

    // Quality score from category match
    components.CategoryMatch = model.CategoryScores[features.Category]
    components.QualityScore = r.computeQualityScore(model, features)

    // Cost score (inverse - lower cost = higher score)
    estimatedCost := r.estimateCost(model, features)
    if constraints != nil && constraints.MaxCostPerRequest > 0 {
        components.CostScore = 1.0 - (estimatedCost / constraints.MaxCostPerRequest)
    } else {
        components.CostScore = 1.0 / (1.0 + estimatedCost)
    }

    // Latency score
    if constraints != nil && constraints.MaxLatency > 0 {
        components.LatencyScore = 1.0 - float64(model.MedianLatency)/float64(constraints.MaxLatency)
    } else {
        components.LatencyScore = 1.0 / (1.0 + model.MedianLatency.Seconds())
    }

    // Historical performance on similar tasks
    components.HistoricalScore = r.computeHistoricalScore(model, similar)

    // Weighted combination
    score := r.weights.Quality*components.QualityScore +
        r.weights.Cost*components.CostScore +
        r.weights.Latency*components.LatencyScore +
        r.weights.Historical*components.HistoricalScore

    return &ScoredModel{
        Model:      model,
        Score:      score,
        Components: components,
    }
}

func (r *Router) computeHistoricalScore(model *ModelProfile, similar []*SimilarTask) float64 {
    if len(similar) == 0 {
        return 0.5 // Neutral if no history
    }

    var weightedSum, weightSum float64
    for _, task := range similar {
        if task.ModelUsed != model.ID {
            continue
        }

        // Weight by similarity
        weight := task.Similarity

        // Score based on outcome
        var score float64
        switch task.Outcome {
        case OutcomeSuccess:
            score = 1.0
        case OutcomeCorrected:
            score = 0.5
        case OutcomeFailed:
            score = 0.0
        }

        weightedSum += weight * score
        weightSum += weight
    }

    if weightSum == 0 {
        return 0.5
    }

    return weightedSum / weightSum
}

func (r *Router) meetsConstraints(
    model *ModelProfile,
    features *TaskFeatures,
    constraints *RoutingConstraints,
) bool {
    if constraints == nil {
        return true
    }

    // Check excluded models
    for _, excluded := range constraints.ExcludedModels {
        if model.ID == excluded || model.Name == excluded {
            return false
        }
    }

    // Check required capabilities
    for _, cap := range constraints.RequiredCapabilities {
        switch cap {
        case "vision":
            if !model.SupportsVision && features.HasImages {
                return false
            }
        case "tools":
            if !model.SupportsTools {
                return false
            }
        }
    }

    // Check context fits
    if features.TokenCount > model.ContextWindow {
        return false
    }

    // Check latency constraint
    if constraints.MaxLatency > 0 && model.P95Latency > constraints.MaxLatency {
        return false
    }

    return true
}
```

### Routing Store

```go
// internal/routing/store.go

type RoutingStore struct {
    db       *sql.DB
    embedder EmbeddingProvider
}

type TaskRecord struct {
    ID          string
    Timestamp   time.Time
    Task        string
    Features    *TaskFeatures
    Embedding   []float32
    ModelUsed   string
    Outcome     TaskOutcome
    Latency     time.Duration
    TokensUsed  int
    Cost        float64
    UserRating  *int // Optional 1-5 rating
}

type TaskOutcome int

const (
    OutcomeSuccess   TaskOutcome = iota // Task completed successfully
    OutcomeCorrected                     // User provided correction
    OutcomeFailed                        // Task failed or was rejected
)

type SimilarTask struct {
    TaskRecord
    Similarity float64
}

func (s *RoutingStore) RecordOutcome(ctx context.Context, record *TaskRecord) error {
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO routing_history
        (id, timestamp, task, features, embedding, model_used, outcome, latency, tokens_used, cost, user_rating)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, record.ID, record.Timestamp, record.Task, jsonEncode(record.Features),
       vectorToBlob(record.Embedding), record.ModelUsed, record.Outcome,
       record.Latency, record.TokensUsed, record.Cost, record.UserRating)

    return err
}

func (s *RoutingStore) FindSimilar(ctx context.Context, embedding []float32, limit int) ([]*SimilarTask, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT h.*, 1 - (h.embedding <=> ?) as similarity
        FROM routing_history h
        WHERE h.timestamp > datetime('now', '-30 days')
        ORDER BY h.embedding <=> ?
        LIMIT ?
    `, vectorToBlob(embedding), vectorToBlob(embedding), limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var results []*SimilarTask
    for rows.Next() {
        task, similarity, err := scanSimilarTask(rows)
        if err != nil {
            continue
        }
        results = append(results, &SimilarTask{
            TaskRecord: *task,
            Similarity: similarity,
        })
    }

    return results, nil
}

func (s *RoutingStore) UpdateModelProfile(ctx context.Context, modelID string) (*ModelProfile, error) {
    // Compute aggregated stats
    row := s.db.QueryRowContext(ctx, `
        SELECT
            AVG(CASE WHEN outcome = 0 THEN 1.0 ELSE 0.0 END) as success_rate,
            AVG(latency) as median_latency,
            PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY latency) as p95_latency
        FROM routing_history
        WHERE model_used = ?
        AND timestamp > datetime('now', '-7 days')
    `, modelID)

    var successRate, medianLatency, p95Latency float64
    if err := row.Scan(&successRate, &medianLatency, &p95Latency); err != nil {
        return nil, err
    }

    // Update profile
    _, err := s.db.ExecContext(ctx, `
        UPDATE model_profiles
        SET success_rate = ?, median_latency = ?, p95_latency = ?, updated_at = ?
        WHERE id = ?
    `, successRate, medianLatency, p95Latency, time.Now(), modelID)

    return s.GetModelProfile(ctx, modelID)
}
```

## Category Classifier

### Classifier Implementation

```go
// internal/routing/classifier.go

type CategoryClassifier struct {
    llm    LLMClient
    cache  *lru.Cache
}

func NewCategoryClassifier(llm LLMClient) *CategoryClassifier {
    cache, _ := lru.New(1000)
    return &CategoryClassifier{llm: llm, cache: cache}
}

func (c *CategoryClassifier) Classify(task string) (TaskCategory, float64) {
    // Check cache
    hash := hashTask(task)
    if cached, ok := c.cache.Get(hash); ok {
        result := cached.(*classificationResult)
        return result.Category, result.Confidence
    }

    // Fast heuristic classification
    category, confidence := c.heuristicClassify(task)
    if confidence > 0.85 {
        c.cache.Add(hash, &classificationResult{category, confidence})
        return category, confidence
    }

    // LLM classification for ambiguous cases
    category, confidence = c.llmClassify(task)
    c.cache.Add(hash, &classificationResult{category, confidence})

    return category, confidence
}

func (c *CategoryClassifier) heuristicClassify(task string) (TaskCategory, float64) {
    lower := strings.ToLower(task)

    // Coding indicators
    codePatterns := []string{
        "write", "code", "function", "implement", "debug", "fix",
        "refactor", "class", "method", "api", "test",
    }
    if matchesPatterns(lower, codePatterns) && (containsCodeBlock(task) || hasCodeKeywords(lower)) {
        return CategoryCoding, 0.9
    }

    // Reasoning indicators
    reasoningPatterns := []string{
        "why", "explain", "analyze", "compare", "evaluate",
        "what if", "how would", "step by step",
    }
    if matchesPatterns(lower, reasoningPatterns) {
        return CategoryReasoning, 0.8
    }

    // Creative indicators
    creativePatterns := []string{
        "write a story", "create", "imagine", "brainstorm",
        "draft", "compose", "design",
    }
    if matchesPatterns(lower, creativePatterns) {
        return CategoryCreative, 0.85
    }

    // Analysis indicators
    analysisPatterns := []string{
        "summarize", "extract", "find patterns", "statistics",
        "data", "analyze this", "review",
    }
    if matchesPatterns(lower, analysisPatterns) {
        return CategoryAnalysis, 0.8
    }

    // Simple/conversational (short, question-like)
    if len(task) < 200 && (strings.HasSuffix(task, "?") || isSimpleQuestion(task)) {
        return CategorySimple, 0.7
    }

    return CategoryConversation, 0.5
}

func (c *CategoryClassifier) llmClassify(task string) (TaskCategory, float64) {
    prompt := fmt.Sprintf(`Classify this task into exactly one category:
- simple: Direct factual questions, simple lookups
- reasoning: Multi-step logic, analysis, problem solving
- coding: Code generation, debugging, review
- creative: Writing, brainstorming, design
- analysis: Data analysis, summarization, extraction
- conversation: General chat, clarification

Task: %s

Respond with only: category,confidence
Example: reasoning,0.9`, task)

    response, _, err := c.llm.Complete(context.Background(), prompt)
    if err != nil {
        return CategorySimple, 0.5
    }

    return parseClassification(response)
}
```

## Feedback Integration

### Learning from Outcomes

```go
// internal/routing/learning.go

type RoutingLearner struct {
    store   *RoutingStore
    router  *Router
    logger  *slog.Logger
}

func (l *RoutingLearner) RecordSuccess(ctx context.Context, decision *RoutingDecision, result *ExecutionResult) error {
    record := &TaskRecord{
        ID:         generateID(),
        Timestamp:  time.Now(),
        Task:       result.OriginalTask,
        ModelUsed:  decision.Model.ID,
        Outcome:    OutcomeSuccess,
        Latency:    result.Duration,
        TokensUsed: result.TokensUsed,
        Cost:       result.Cost,
    }

    if err := l.store.RecordOutcome(ctx, record); err != nil {
        return err
    }

    // Update model profile
    l.updateModelScores(ctx, decision.Model.ID, OutcomeSuccess)

    return nil
}

func (l *RoutingLearner) RecordCorrection(ctx context.Context, decision *RoutingDecision, correction string) error {
    record := &TaskRecord{
        ID:        generateID(),
        Timestamp: time.Now(),
        ModelUsed: decision.Model.ID,
        Outcome:   OutcomeCorrected,
    }

    if err := l.store.RecordOutcome(ctx, record); err != nil {
        return err
    }

    // Corrections slightly decrease model score for this category
    l.updateModelScores(ctx, decision.Model.ID, OutcomeCorrected)

    return nil
}

func (l *RoutingLearner) RecordFailure(ctx context.Context, decision *RoutingDecision, err error) error {
    record := &TaskRecord{
        ID:        generateID(),
        Timestamp: time.Now(),
        ModelUsed: decision.Model.ID,
        Outcome:   OutcomeFailed,
    }

    if err := l.store.RecordOutcome(ctx, record); err != nil {
        return err
    }

    // Failures significantly decrease model score
    l.updateModelScores(ctx, decision.Model.ID, OutcomeFailed)

    return nil
}

func (l *RoutingLearner) updateModelScores(ctx context.Context, modelID string, outcome TaskOutcome) {
    // Retrieve current profile
    profile, err := l.store.GetModelProfile(ctx, modelID)
    if err != nil {
        l.logger.Error("failed to get model profile", "error", err)
        return
    }

    // Apply exponential moving average update
    alpha := 0.1 // Learning rate

    var newSuccessRate float64
    switch outcome {
    case OutcomeSuccess:
        newSuccessRate = profile.SuccessRate*(1-alpha) + 1.0*alpha
    case OutcomeCorrected:
        newSuccessRate = profile.SuccessRate*(1-alpha) + 0.5*alpha
    case OutcomeFailed:
        newSuccessRate = profile.SuccessRate*(1-alpha) + 0.0*alpha
    }

    l.store.UpdateSuccessRate(ctx, modelID, newSuccessRate)
}
```

## Integration

### Controller Integration

```go
// internal/rlm/controller.go

type Controller struct {
    // ... existing fields ...
    router        *routing.Router
    routingLearner *routing.RoutingLearner
}

func (c *Controller) Execute(ctx context.Context, task string) (*Result, error) {
    // Get routing decision
    constraints := c.getRoutingConstraints(ctx)
    decision, err := c.router.Route(ctx, task, c.conversation, constraints)
    if err != nil {
        return nil, fmt.Errorf("routing: %w", err)
    }

    c.logger.Info("routed task",
        "model", decision.Model.Name,
        "confidence", decision.Confidence,
        "reasoning", decision.Reasoning)

    // Execute with selected model
    client := c.getClientForModel(decision.Model)
    result, err := c.executeWithModel(ctx, client, task)

    // Record outcome
    if err != nil {
        c.routingLearner.RecordFailure(ctx, decision, err)
        return nil, err
    }

    c.routingLearner.RecordSuccess(ctx, decision, result)

    return result, nil
}

func (c *Controller) HandleUserCorrection(ctx context.Context, correction string) error {
    if c.lastDecision != nil {
        c.routingLearner.RecordCorrection(ctx, c.lastDecision, correction)
    }
    return nil
}
```

## Observability

### Metrics

```go
var (
    routingDecisions = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_routing_decisions_total",
            Help: "Routing decisions by model and category",
        },
        []string{"model", "category"},
    )

    routingOutcomes = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_routing_outcomes_total",
            Help: "Routing outcomes by model",
        },
        []string{"model", "outcome"},
    )

    routingConfidence = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_routing_confidence",
            Help:    "Confidence distribution of routing decisions",
            Buckets: prometheus.LinearBuckets(0, 0.1, 11),
        },
    )

    modelSuccessRate = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "rlm_model_success_rate",
            Help: "Current success rate by model",
        },
        []string{"model"},
    )
)
```

## Success Criteria

1. **Cost reduction**: 30%+ reduction in API costs through appropriate model selection
2. **Quality maintenance**: <5% increase in correction rate vs always using best model
3. **Learning speed**: Measurable improvement in routing accuracy within 100 tasks
4. **Latency**: Routing decision overhead <50ms
5. **Explainability**: Clear reasoning for every routing decision
