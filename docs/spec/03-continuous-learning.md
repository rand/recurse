# SPEC-03: Continuous Learning

> Enable the RLM to improve over time by learning from interactions, building domain knowledge, and adapting to user preferences.

## Overview

The continuous learning system accumulates knowledge across sessions, learns successful patterns, adapts to user preferences, and avoids past mistakes. Unlike one-shot learning, this maintains and refines knowledge over time.

## Current State

- Stateless operation: each execution starts fresh
- No accumulation of domain knowledge
- Repeated mistakes across sessions
- Cannot adapt to user preferences

## Requirements

### [SPEC-03.01] Learning Signals

Define signal types for learning events:

```go
type SignalType int

const (
    SignalSuccess      // Task completed successfully
    SignalCorrection   // User provided correction
    SignalRejection    // User rejected output
    SignalPreference   // Explicit preference stated
    SignalPattern      // Successful pattern detected
    SignalError        // Error or failure
)
```

Features:
- Signal context with session, task, query, output
- Embedding for semantic matching
- Domain categorization

### [SPEC-03.02] Knowledge Types

Store different types of learned knowledge:

| Type | Purpose | Example |
|------|---------|---------|
| Fact | Learned facts | "Users authenticate via JWT tokens" |
| Pattern | Successful patterns | Error handling template |
| Preference | User preferences | "Prefer functional style" |
| Constraint | Rules/constraints | "Avoid X, use Y instead" |
| Example | Example solutions | Working code snippets |

Features:
- Confidence scoring (0.0-1.0)
- Embedding for similarity search
- Access tracking for decay calculation
- Source attribution (explicit vs inferred)

### [SPEC-03.03] Learning Engine

Process signals and extract knowledge:

```
Signal → Extract Patterns → Check Similarity → Store/Reinforce
                                ↓
                         Consolidation Loop
                                ↓
                      Merge → Decay → Prune
```

Features:
- Pattern extraction from successful executions
- Code pattern detection
- Reasoning pattern templating
- Background consolidation
- Configurable thresholds

### [SPEC-03.04] Knowledge Application

Apply learned knowledge to new tasks:

```go
type ApplyResult struct {
    RelevantFacts       []*LearnedFact
    ApplicablePatterns  []*LearnedPattern
    Preferences         []*UserPreference
    Constraints         []*LearnedConstraint
    ContextAdditions    []string
}
```

Features:
- Query by embedding similarity
- Domain-scoped preferences
- Constraint enforcement
- Context augmentation for LLM calls

### [SPEC-03.05] Knowledge Store

SQLite-backed storage with embeddings:

```sql
learned_facts(id, content, domain, source, confidence, embedding, ...)
learned_patterns(id, name, trigger, template, success_rate, ...)
user_preferences(id, key, value, scope, scope_value, ...)
learned_constraints(id, type, description, correction, ...)
```

Features:
- Vector similarity search
- Confidence decay over time
- Access count tracking
- Consolidation (merge similar facts)

### [SPEC-03.06] Preference Scopes

Support different preference scopes:

| Scope | Example | Applied When |
|-------|---------|--------------|
| Global | "Prefer concise output" | Always |
| Domain | "Use pytest for Python" | Python projects |
| Project | "Follow our style guide" | This project only |

## Implementation Tasks

- [ ] Define signal types and interfaces
- [ ] Implement Knowledge Store with SQLite
- [ ] Create Pattern Extractor (code, reasoning, structural)
- [ ] Implement Knowledge Consolidator
- [ ] Build Knowledge Applier
- [ ] Integrate with RLM Controller
- [ ] Add Ebbinghaus-style decay
- [ ] Add observability metrics
- [ ] Write unit and integration tests

## Dependencies

- `internal/memory/embeddings/` - Embedding provider
- `internal/rlm/` - Controller integration
- `internal/memory/hypergraph/` - Storage layer

## Acceptance Criteria

1. 100+ facts/patterns learned per project over time
2. >50% of tasks benefit from learned knowledge
3. Measurable reduction in corrections over time
4. >90% accuracy on learned preferences
5. <10MB storage per 1000 knowledge items
