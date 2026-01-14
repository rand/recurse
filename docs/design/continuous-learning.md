# Continuous Learning Design

> Design document for `recurse-xwr`: [SPEC] Continuous Learning Design

## Overview

This document specifies a continuous learning system that enables the RLM to improve over time by learning from interactions, building domain knowledge, and adapting to user preferences. Unlike one-shot learning, this system maintains and refines knowledge across sessions.

## Problem Statement

### Current State

Stateless operation:

```go
func (c *Controller) Execute(ctx context.Context, task string) (*Result, error) {
    // Each execution starts fresh
    // No learning from past interactions
    return c.llm.Complete(ctx, task)
}
```

**Issues**:
- No accumulation of domain knowledge
- Repeated mistakes across sessions
- Cannot adapt to user preferences
- No improvement from successful patterns

## Design Goals

1. **Knowledge accumulation**: Build domain expertise over time
2. **Pattern learning**: Identify and reuse successful approaches
3. **Preference adaptation**: Learn user style and preferences
4. **Error avoidance**: Remember and avoid past mistakes
5. **Graceful degradation**: Work without learned data

## Core Types

### Learning Signals

```go
// internal/learning/signals.go

type LearningSignal interface {
    Type() SignalType
    Timestamp() time.Time
    Context() *SignalContext
}

type SignalType int

const (
    SignalSuccess      SignalType = iota // Task completed successfully
    SignalCorrection                      // User provided correction
    SignalRejection                       // User rejected output
    SignalPreference                      // Explicit preference stated
    SignalPattern                         // Successful pattern detected
    SignalError                           // Error or failure
)

type SignalContext struct {
    SessionID   string
    TaskID      string
    Query       string
    Output      string
    Category    string
    Domain      string
    Embedding   []float32
}

type SuccessSignal struct {
    BaseSignal
    Result      *ExecutionResult
    UserRating  *int
    Patterns    []string
}

type CorrectionSignal struct {
    BaseSignal
    Original    string
    Corrected   string
    Explanation string
    Severity    float64
}

type PreferenceSignal struct {
    BaseSignal
    Key         string
    Value       any
    Scope       PreferenceScope
}

type PreferenceScope int

const (
    ScopeGlobal   PreferenceScope = iota // Applies everywhere
    ScopeDomain                           // Applies to specific domain
    ScopeProject                          // Applies to specific project
)
```

### Knowledge Types

```go
// internal/learning/knowledge.go

type Knowledge interface {
    ID() string
    Type() KnowledgeType
    Confidence() float64
    Apply(ctx context.Context, input *ApplyInput) (*ApplyResult, error)
}

type KnowledgeType int

const (
    KnowledgeFact       KnowledgeType = iota // Learned facts
    KnowledgePattern                          // Successful patterns
    KnowledgePreference                       // User preferences
    KnowledgeConstraint                       // Rules/constraints
    KnowledgeExample                          // Example solutions
)

type LearnedFact struct {
    ID          string
    Content     string
    Domain      string
    Source      string
    Confidence  float64
    Citations   []string
    Embedding   []float32
    CreatedAt   time.Time
    AccessCount int
    LastAccess  *time.Time
}

type LearnedPattern struct {
    ID          string
    Name        string
    Description string
    Trigger     string      // When to apply this pattern
    Template    string      // Pattern template
    Examples    []string
    SuccessRate float64
    UseCount    int
    Domain      string
    Embedding   []float32
}

type UserPreference struct {
    ID          string
    Key         string
    Value       any
    Scope       PreferenceScope
    ScopeValue  string // Domain or project name
    Confidence  float64
    Source      string // How was this learned
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

## Learning Engine

### Engine Implementation

```go
// internal/learning/engine.go

type Engine struct {
    store       *KnowledgeStore
    extractor   *PatternExtractor
    consolidator *KnowledgeConsolidator
    applier     *KnowledgeApplier
    config      EngineConfig
    logger      *slog.Logger
}

type EngineConfig struct {
    MinConfidenceToStore    float64
    ConsolidationInterval   time.Duration
    MaxKnowledgeItems       int
    DecayHalfLife           time.Duration
}

func NewEngine(store *KnowledgeStore, config EngineConfig) *Engine {
    e := &Engine{
        store:       store,
        extractor:   NewPatternExtractor(),
        consolidator: NewKnowledgeConsolidator(store),
        applier:     NewKnowledgeApplier(store),
        config:      config,
    }

    // Start background consolidation
    go e.consolidationLoop()

    return e
}

func (e *Engine) ProcessSignal(ctx context.Context, signal LearningSignal) error {
    switch s := signal.(type) {
    case *SuccessSignal:
        return e.processSuccess(ctx, s)
    case *CorrectionSignal:
        return e.processCorrection(ctx, s)
    case *PreferenceSignal:
        return e.processPreference(ctx, s)
    default:
        return fmt.Errorf("unknown signal type: %T", signal)
    }
}

func (e *Engine) processSuccess(ctx context.Context, signal *SuccessSignal) error {
    // Extract patterns from successful execution
    patterns := e.extractor.ExtractPatterns(signal.Result)

    for _, pattern := range patterns {
        // Check if pattern already exists
        existing, err := e.store.FindSimilarPattern(ctx, pattern.Embedding)
        if err == nil && existing != nil && existing.Similarity > 0.9 {
            // Reinforce existing pattern
            e.store.ReinforcePattern(ctx, existing.Pattern.ID)
            continue
        }

        // Store new pattern
        if pattern.Confidence >= e.config.MinConfidenceToStore {
            e.store.SavePattern(ctx, pattern)
        }
    }

    // Update domain knowledge
    if signal.Context().Domain != "" {
        e.store.UpdateDomainStats(ctx, signal.Context().Domain, true)
    }

    return nil
}

func (e *Engine) processCorrection(ctx context.Context, signal *CorrectionSignal) error {
    // Create constraint from correction
    constraint := &LearnedConstraint{
        ID:          generateID(),
        Type:        ConstraintAvoid,
        Description: fmt.Sprintf("Avoid: %s", signal.Original),
        Correction:  signal.Corrected,
        Reason:      signal.Explanation,
        Confidence:  signal.Severity,
        Embedding:   signal.Context().Embedding,
        CreatedAt:   time.Now(),
    }

    return e.store.SaveConstraint(ctx, constraint)
}

func (e *Engine) processPreference(ctx context.Context, signal *PreferenceSignal) error {
    pref := &UserPreference{
        ID:         generateID(),
        Key:        signal.Key,
        Value:      signal.Value,
        Scope:      signal.Scope,
        ScopeValue: signal.Context().Domain,
        Confidence: 1.0,
        Source:     "explicit",
        CreatedAt:  time.Now(),
        UpdatedAt:  time.Now(),
    }

    return e.store.SavePreference(ctx, pref)
}

func (e *Engine) consolidationLoop() {
    ticker := time.NewTicker(e.config.ConsolidationInterval)
    defer ticker.Stop()

    for range ticker.C {
        ctx := context.Background()
        if err := e.consolidator.Consolidate(ctx); err != nil {
            e.logger.Error("consolidation failed", "error", err)
        }
    }
}
```

### Pattern Extractor

```go
// internal/learning/extractor.go

type PatternExtractor struct {
    llm      LLMClient
    embedder EmbeddingProvider
}

type ExtractedPattern struct {
    Name        string
    Description string
    Trigger     string
    Template    string
    Confidence  float64
    Embedding   []float32
}

func (e *PatternExtractor) ExtractPatterns(result *ExecutionResult) []*ExtractedPattern {
    var patterns []*ExtractedPattern

    // Extract code patterns
    if containsCode(result.Output) {
        codePatterns := e.extractCodePatterns(result)
        patterns = append(patterns, codePatterns...)
    }

    // Extract reasoning patterns
    if result.HasReasoningChain {
        reasoningPatterns := e.extractReasoningPatterns(result)
        patterns = append(patterns, reasoningPatterns...)
    }

    // Extract structural patterns
    structuralPatterns := e.extractStructuralPatterns(result)
    patterns = append(patterns, structuralPatterns...)

    return patterns
}

func (e *PatternExtractor) extractCodePatterns(result *ExecutionResult) []*ExtractedPattern {
    prompt := `Analyze this code and identify reusable patterns.

Code:
` + result.Output + `

For each pattern found, provide:
1. Name (short identifier)
2. Description (what it does)
3. Trigger (when to use it)
4. Template (generalized version)
5. Confidence (0.0-1.0)

Format as JSON array.`

    response, _, err := e.llm.Complete(context.Background(), prompt)
    if err != nil {
        return nil
    }

    return parsePatterns(response)
}

func (e *PatternExtractor) extractReasoningPatterns(result *ExecutionResult) []*ExtractedPattern {
    // Look for successful reasoning chains that can be templated
    if len(result.ReasoningSteps) < 2 {
        return nil
    }

    // Abstract the reasoning pattern
    steps := make([]string, len(result.ReasoningSteps))
    for i, step := range result.ReasoningSteps {
        steps[i] = abstractStep(step)
    }

    return []*ExtractedPattern{{
        Name:        fmt.Sprintf("reasoning-%s", result.Category),
        Description: "Reasoning pattern for " + result.Category,
        Trigger:     result.Category,
        Template:    strings.Join(steps, " -> "),
        Confidence:  0.7,
    }}
}
```

### Knowledge Consolidator

```go
// internal/learning/consolidator.go

type KnowledgeConsolidator struct {
    store  *KnowledgeStore
    llm    LLMClient
}

func (c *KnowledgeConsolidator) Consolidate(ctx context.Context) error {
    // 1. Merge similar facts
    if err := c.mergeSimilarFacts(ctx); err != nil {
        return fmt.Errorf("merge facts: %w", err)
    }

    // 2. Consolidate patterns
    if err := c.consolidatePatterns(ctx); err != nil {
        return fmt.Errorf("consolidate patterns: %w", err)
    }

    // 3. Decay old knowledge
    if err := c.applyDecay(ctx); err != nil {
        return fmt.Errorf("apply decay: %w", err)
    }

    // 4. Prune low-confidence items
    if err := c.pruneWeakKnowledge(ctx); err != nil {
        return fmt.Errorf("prune: %w", err)
    }

    return nil
}

func (c *KnowledgeConsolidator) mergeSimilarFacts(ctx context.Context) error {
    // Find clusters of similar facts
    facts, err := c.store.GetAllFacts(ctx)
    if err != nil {
        return err
    }

    clusters := clusterByEmbedding(facts, 0.85) // 85% similarity threshold

    for _, cluster := range clusters {
        if len(cluster) < 2 {
            continue
        }

        // Merge cluster into single fact
        merged := c.mergeFacts(ctx, cluster)
        if merged == nil {
            continue
        }

        // Delete original facts and save merged
        for _, fact := range cluster {
            c.store.DeleteFact(ctx, fact.ID)
        }
        c.store.SaveFact(ctx, merged)
    }

    return nil
}

func (c *KnowledgeConsolidator) mergeFacts(ctx context.Context, facts []*LearnedFact) *LearnedFact {
    // Use LLM to synthesize facts
    var contents []string
    for _, f := range facts {
        contents = append(contents, f.Content)
    }

    prompt := fmt.Sprintf(`Synthesize these related facts into a single, comprehensive fact:

%s

Provide a merged fact that captures all the information without redundancy.`, strings.Join(contents, "\n---\n"))

    response, _, err := c.llm.Complete(ctx, prompt)
    if err != nil {
        return nil
    }

    // Calculate merged confidence
    var maxConfidence float64
    var totalAccess int
    for _, f := range facts {
        if f.Confidence > maxConfidence {
            maxConfidence = f.Confidence
        }
        totalAccess += f.AccessCount
    }

    return &LearnedFact{
        ID:          generateID(),
        Content:     response,
        Domain:      facts[0].Domain,
        Source:      "consolidated",
        Confidence:  maxConfidence,
        AccessCount: totalAccess,
        CreatedAt:   time.Now(),
    }
}

func (c *KnowledgeConsolidator) applyDecay(ctx context.Context) error {
    // Apply Ebbinghaus-style decay to knowledge
    return c.store.ApplyDecay(ctx, func(k Knowledge, age time.Duration, accessCount int) float64 {
        // Base decay over time
        decayFactor := math.Exp(-age.Hours() / (24 * 30)) // Half-life of ~30 days

        // Access amplification
        accessBonus := 1.0 + math.Log1p(float64(accessCount))*0.1

        return k.Confidence() * decayFactor * accessBonus
    })
}
```

## Knowledge Application

### Applier Implementation

```go
// internal/learning/applier.go

type KnowledgeApplier struct {
    store    *KnowledgeStore
    embedder EmbeddingProvider
}

type ApplyInput struct {
    Query      string
    Context    []Message
    Domain     string
    Embedding  []float32
}

type ApplyResult struct {
    RelevantFacts       []*LearnedFact
    ApplicablePatterns  []*LearnedPattern
    Preferences         []*UserPreference
    Constraints         []*LearnedConstraint
    ContextAdditions    []string
}

func (a *KnowledgeApplier) Apply(ctx context.Context, input *ApplyInput) (*ApplyResult, error) {
    result := &ApplyResult{}

    // Find relevant facts
    facts, err := a.store.FindRelevantFacts(ctx, input.Embedding, 5)
    if err == nil {
        result.RelevantFacts = facts
    }

    // Find applicable patterns
    patterns, err := a.store.FindApplicablePatterns(ctx, input.Query, input.Domain)
    if err == nil {
        result.ApplicablePatterns = patterns
    }

    // Get preferences for scope
    prefs, err := a.store.GetPreferences(ctx, input.Domain)
    if err == nil {
        result.Preferences = prefs
    }

    // Get constraints
    constraints, err := a.store.FindConstraints(ctx, input.Embedding)
    if err == nil {
        result.Constraints = constraints
    }

    // Build context additions
    result.ContextAdditions = a.buildContextAdditions(result)

    return result, nil
}

func (a *KnowledgeApplier) buildContextAdditions(result *ApplyResult) []string {
    var additions []string

    // Add relevant facts
    if len(result.RelevantFacts) > 0 {
        var factStrings []string
        for _, f := range result.RelevantFacts {
            factStrings = append(factStrings, f.Content)
        }
        additions = append(additions, fmt.Sprintf(
            "Relevant learned facts:\n%s",
            strings.Join(factStrings, "\n"),
        ))
    }

    // Add preferences
    if len(result.Preferences) > 0 {
        var prefStrings []string
        for _, p := range result.Preferences {
            prefStrings = append(prefStrings, fmt.Sprintf("- %s: %v", p.Key, p.Value))
        }
        additions = append(additions, fmt.Sprintf(
            "User preferences:\n%s",
            strings.Join(prefStrings, "\n"),
        ))
    }

    // Add constraints
    if len(result.Constraints) > 0 {
        var constraintStrings []string
        for _, c := range result.Constraints {
            constraintStrings = append(constraintStrings, c.Description)
        }
        additions = append(additions, fmt.Sprintf(
            "Constraints to follow:\n%s",
            strings.Join(constraintStrings, "\n"),
        ))
    }

    return additions
}
```

## Knowledge Store

### Store Implementation

```go
// internal/learning/store.go

type KnowledgeStore struct {
    db       *sql.DB
    embedder EmbeddingProvider
}

func NewKnowledgeStore(db *sql.DB, embedder EmbeddingProvider) *KnowledgeStore {
    return &KnowledgeStore{db: db, embedder: embedder}
}

func (s *KnowledgeStore) SaveFact(ctx context.Context, fact *LearnedFact) error {
    if fact.Embedding == nil {
        embeddings, err := s.embedder.Embed(ctx, []string{fact.Content})
        if err == nil && len(embeddings) > 0 {
            fact.Embedding = embeddings[0]
        }
    }

    _, err := s.db.ExecContext(ctx, `
        INSERT INTO learned_facts
        (id, content, domain, source, confidence, citations, embedding, created_at, access_count)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, fact.ID, fact.Content, fact.Domain, fact.Source, fact.Confidence,
       jsonEncode(fact.Citations), vectorToBlob(fact.Embedding), fact.CreatedAt, fact.AccessCount)

    return err
}

func (s *KnowledgeStore) FindRelevantFacts(ctx context.Context, embedding []float32, limit int) ([]*LearnedFact, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT f.*, 1 - (f.embedding <=> ?) as similarity
        FROM learned_facts f
        WHERE f.confidence > 0.3
        ORDER BY f.embedding <=> ?
        LIMIT ?
    `, vectorToBlob(embedding), vectorToBlob(embedding), limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    return scanFacts(rows)
}

func (s *KnowledgeStore) RecordAccess(ctx context.Context, id string) error {
    _, err := s.db.ExecContext(ctx, `
        UPDATE learned_facts
        SET access_count = access_count + 1, last_access = ?
        WHERE id = ?
    `, time.Now(), id)
    return err
}

func (s *KnowledgeStore) ApplyDecay(ctx context.Context, fn func(Knowledge, time.Duration, int) float64) error {
    // Apply decay function to all knowledge types
    _, err := s.db.ExecContext(ctx, `
        UPDATE learned_facts
        SET confidence = confidence * EXP(-JULIANDAY('now') - JULIANDAY(created_at)) / 30.0)
            * (1.0 + 0.1 * LOG(1 + access_count))
        WHERE confidence > 0.1
    `)
    return err
}
```

## Integration

### Controller Integration

```go
// internal/rlm/controller.go

type Controller struct {
    // ... existing fields ...
    learningEngine *learning.Engine
    knowledgeApplier *learning.KnowledgeApplier
}

func (c *Controller) Execute(ctx context.Context, task string) (*Result, error) {
    // Get embedding for task
    embedding, _ := c.embedder.Embed(ctx, []string{task})

    // Apply learned knowledge
    applyInput := &learning.ApplyInput{
        Query:     task,
        Context:   c.conversation,
        Domain:    c.currentDomain,
        Embedding: embedding[0],
    }
    applied, err := c.knowledgeApplier.Apply(ctx, applyInput)
    if err != nil {
        c.logger.Warn("failed to apply knowledge", "error", err)
    }

    // Augment context with learned knowledge
    augmentedContext := c.buildContext(task, applied)

    // Execute
    result, err := c.executeWithContext(ctx, task, augmentedContext)
    if err != nil {
        c.learningEngine.ProcessSignal(ctx, &learning.ErrorSignal{
            BaseSignal: learning.BaseSignal{Ctx: c.signalContext(task)},
            Error:      err,
        })
        return nil, err
    }

    // Record success
    c.learningEngine.ProcessSignal(ctx, &learning.SuccessSignal{
        BaseSignal: learning.BaseSignal{Ctx: c.signalContext(task)},
        Result:     result,
    })

    return result, nil
}

func (c *Controller) HandleCorrection(ctx context.Context, original, corrected, explanation string) error {
    return c.learningEngine.ProcessSignal(ctx, &learning.CorrectionSignal{
        BaseSignal:  learning.BaseSignal{Ctx: c.signalContext(c.lastQuery)},
        Original:    original,
        Corrected:   corrected,
        Explanation: explanation,
        Severity:    0.8,
    })
}
```

## Observability

### Metrics

```go
var (
    knowledgeItems = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "rlm_knowledge_items",
            Help: "Number of knowledge items by type",
        },
        []string{"type"},
    )

    learningSignals = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_learning_signals_total",
            Help: "Learning signals processed",
        },
        []string{"type"},
    )

    knowledgeApplications = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_knowledge_applications_total",
            Help: "Knowledge applications by type",
        },
        []string{"type"},
    )

    knowledgeConfidence = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_knowledge_confidence",
            Help:    "Confidence distribution of applied knowledge",
            Buckets: prometheus.LinearBuckets(0, 0.1, 11),
        },
    )
)
```

## Success Criteria

1. **Knowledge accumulation**: 100+ facts/patterns learned per project
2. **Application rate**: >50% of tasks benefit from learned knowledge
3. **Quality improvement**: Measurable reduction in corrections over time
4. **Preference accuracy**: >90% accuracy on learned preferences
5. **Storage efficiency**: <10MB per 1000 knowledge items
