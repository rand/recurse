# Task Classification for RLM Mode Selection

> Design document for `recurse-e8e`: Task-Type Detection for Smart Mode Selection

## Overview

This document defines the taxonomy of task types and the signals used to classify them for automatic RLM vs Direct mode selection.

## Task Taxonomy

### 1. Computational Tasks → **Prefer RLM**

Tasks requiring exact computation over context data. LLMs struggle with these; code execution excels.

| Subtype | Description | Example Query |
|---------|-------------|---------------|
| **Counting** | Count occurrences of pattern/word | "How many times does 'apple' appear?" |
| **Summing** | Add numeric values from context | "What is the total sales across regions?" |
| **Aggregation** | Collect and combine scattered data | "List all error codes mentioned" |
| **Pattern Matching** | Find all instances matching regex | "Find all email addresses in the text" |
| **Arithmetic** | Compute from extracted numbers | "What is the average of the scores?" |

**Why RLM wins**: Python's `len()`, `sum()`, `re.findall()` are exact. LLMs estimate and hallucinate counts.

**Benchmark evidence**:
- Counting: Direct 0%, RLM 100%
- Aggregation: Both 100%, but RLM uses 65% fewer tokens

---

### 2. Retrieval Tasks → **Prefer Direct**

Tasks requiring location and extraction of specific information. Single-pass attention excels.

| Subtype | Description | Example Query |
|---------|-------------|---------------|
| **Needle-in-Haystack** | Find specific hidden fact | "What is the secret code?" |
| **Fact Lookup** | Extract stated information | "What is the company's founding date?" |
| **Quote Extraction** | Find exact quote or statement | "What did the CEO say about growth?" |
| **Identifier Search** | Find specific ID/code/name | "What is the order number?" |

**Why Direct wins**:
- 10-20x faster (no iteration overhead)
- LLM attention mechanisms handle this well
- No REPL setup cost

**Benchmark evidence**:
- Needle 8K: Direct 21s, RLM 3m37s (both 100% accurate)

---

### 3. Analytical Tasks → **Context-Dependent**

Tasks requiring reasoning across multiple pieces of information.

| Subtype | Description | Example Query |
|---------|-------------|---------------|
| **Relationship** | Determine if entities are related | "Did Alice work with Bob?" |
| **Comparison** | Compare attributes of entities | "Which region had higher sales?" |
| **Temporal** | Understand sequence/timing | "What happened after the merger?" |
| **Causal** | Identify cause-effect | "Why did revenue decline?" |

**Mode selection**:
- Small context (<8K): Direct (LLM reasoning sufficient)
- Large context (>8K): RLM (can search and verify systematically)
- Quadratic complexity (pairing): RLM (code can enumerate efficiently)

---

### 4. Transformational Tasks → **Prefer Direct**

Tasks that transform or reformat content without computation.

| Subtype | Description | Example Query |
|---------|-------------|---------------|
| **Summarization** | Condense content | "Summarize this document" |
| **Translation** | Convert between formats | "Convert this to JSON" |
| **Rewriting** | Rephrase or restructure | "Make this more formal" |
| **Extraction** | Pull out structured data | "Extract the key points" |

**Why Direct wins**: These leverage LLM's core language capabilities.

---

## Classification Signals

### Query Keywords

High-confidence signals based on query text:

```go
var computationalKeywords = map[string]float64{
    "how many":     0.9,
    "count":        0.9,
    "number of":    0.8,
    "total":        0.85,
    "sum":          0.9,
    "average":      0.85,
    "mean":         0.85,
    "add up":       0.8,
    "calculate":    0.8,
    "compute":      0.8,
    "all.*that":    0.7,  // "all emails that..."
    "list all":     0.7,
    "find all":     0.7,
    "every":        0.6,
}

var retrievalKeywords = map[string]float64{
    "what is the":  0.7,
    "what was":     0.7,
    "find the":     0.6,
    "where is":     0.7,
    "who is":       0.7,
    "when did":     0.7,
    "which":        0.5,
    "the.*code":    0.8,  // "the secret code"
    "the.*number":  0.6,
    "the.*name":    0.6,
}

var analyticalKeywords = map[string]float64{
    "did.*and.*":   0.7,  // relationship query
    "relationship": 0.8,
    "related":      0.7,
    "compare":      0.7,
    "difference":   0.6,
    "why":          0.6,
    "because":      0.5,
    "after":        0.5,
    "before":       0.5,
}
```

### Query Structure Patterns

```go
type QueryPattern struct {
    Regex      string
    TaskType   TaskType
    Confidence float64
}

var queryPatterns = []QueryPattern{
    // Counting patterns
    {`how many (times|occurrences|instances)`, TaskTypeComputational, 0.95},
    {`count (the|all)?\s*(number of)?`, TaskTypeComputational, 0.9},
    {`how often`, TaskTypeComputational, 0.85},

    // Summing patterns
    {`(total|sum|add up).*(\$|dollars|amount|sales|cost)`, TaskTypeComputational, 0.9},
    {`what is the (total|sum)`, TaskTypeComputational, 0.85},

    // Retrieval patterns
    {`what is the .{1,30}(code|key|password|id|number)`, TaskTypeRetrieval, 0.9},
    {`find the .{1,20}(mentioned|stated|given)`, TaskTypeRetrieval, 0.8},

    // Analytical patterns
    {`did .+ (work|meet|talk|collaborate) with`, TaskTypeAnalytical, 0.8},
    {`(is|are|was|were) .+ related to`, TaskTypeAnalytical, 0.8},
}
```

### Answer Type Expectations

The expected answer format provides strong signals:

| Expected Format | Likely Task Type | Confidence |
|----------------|------------------|------------|
| Single number | Computational | 0.8 |
| Yes/No | Analytical | 0.6 |
| Code/ID pattern | Retrieval | 0.9 |
| List of items | Computational | 0.7 |
| Prose/paragraph | Transformational | 0.7 |

### Context Characteristics

Signals from the context itself:

```go
type ContextSignals struct {
    NumericDensity   float64  // Numbers per 1000 chars
    StructuredData   bool     // Contains tables, lists, JSON
    RepetitiveItems  bool     // Many similar items to count
    UniqueIdentifier bool     // Contains codes, IDs to find
}

func analyzeContext(content string) ContextSignals {
    // High numeric density → likely computational
    // Unique identifiers → likely retrieval
    // Repetitive structure → likely counting/aggregation
}
```

## Classification Algorithm

```go
func (c *TaskClassifier) Classify(query string, contexts []ContextSource) Classification {
    scores := make(map[TaskType]float64)
    signals := []string{}

    // 1. Keyword matching
    for keyword, weight := range computationalKeywords {
        if matchesKeyword(query, keyword) {
            scores[TaskTypeComputational] += weight
            signals = append(signals, "keyword:"+keyword)
        }
    }
    // ... repeat for other keyword maps

    // 2. Pattern matching
    for _, pattern := range queryPatterns {
        if matchesPattern(query, pattern.Regex) {
            scores[pattern.TaskType] += pattern.Confidence
            signals = append(signals, "pattern:"+pattern.Regex)
        }
    }

    // 3. Context analysis (if available)
    if len(contexts) > 0 {
        ctxSignals := analyzeContext(contexts[0].Content)
        if ctxSignals.NumericDensity > 0.01 {
            scores[TaskTypeComputational] += 0.3
            signals = append(signals, "context:high_numeric_density")
        }
    }

    // 4. Normalize and select winner
    winner, confidence := selectBestType(scores)

    return Classification{
        Type:       winner,
        Confidence: confidence,
        Signals:    signals,
    }
}
```

## Mode Selection Integration

```go
func (w *Wrapper) selectMode(query string, totalTokens int, contexts []ContextSource) ExecutionMode {
    classification := w.classifier.Classify(query, contexts)

    // High-confidence computational → RLM even for small context
    if classification.Type == TaskTypeComputational && classification.Confidence > 0.8 {
        if totalTokens >= 1000 && w.replMgr != nil {
            return ModeRLM
        }
    }

    // High-confidence retrieval → Direct even for large context
    if classification.Type == TaskTypeRetrieval && classification.Confidence > 0.8 {
        return ModeDirecte
    }

    // Analytical with large context → RLM
    if classification.Type == TaskTypeAnalytical && totalTokens >= 8000 {
        return ModeRLM
    }

    // Default: size-based threshold (existing behavior)
    if totalTokens >= w.minContextTokensForRLM && w.replMgr != nil {
        return ModeRLM
    }

    return ModeDirecte
}
```

## Validation Approach

### Test Corpus

Use existing benchmark generators to create labeled test set:

| Generator | Expected Classification | Count |
|-----------|------------------------|-------|
| CountingGenerator | Computational | 50 |
| AggregationGenerator | Computational | 50 |
| NeedleGenerator | Retrieval | 50 |
| PairingGenerator | Analytical | 50 |

### Accuracy Metrics

```go
type ClassifierMetrics struct {
    Accuracy    float64            // Overall correct classification
    Precision   map[TaskType]float64  // Per-type precision
    Recall      map[TaskType]float64  // Per-type recall
    Confusion   map[TaskType]map[TaskType]int  // Confusion matrix
}
```

### Success Criteria

- Overall accuracy: >90%
- Computational precision: >95% (false positives waste time on RLM)
- Retrieval precision: >90% (false positives miss token savings)
- No critical misclassifications (computational → retrieval)

## Edge Cases

### Ambiguous Queries

Some queries are genuinely ambiguous:

| Query | Could Be |
|-------|----------|
| "What are the sales figures?" | Retrieval (list them) or Computational (sum them) |
| "Find all the errors" | Retrieval (quote them) or Computational (count them) |

**Strategy**: Default to the mode that's safer for the ambiguity. For listing vs counting, Direct is safer (user can always ask for count specifically).

### Mixed Tasks

Some queries combine task types:

> "How many errors were reported, and what is the most common one?"

**Strategy**: Classify by the primary operation. Here, "how many" dominates → Computational.

### Context-Dependent Interpretation

The same query may have different optimal modes based on context:

> "What is the total?"

- In sales data context: Computational (sum the numbers)
- In a story context: Retrieval (find where "total" is mentioned)

**Strategy**: Use context signals to disambiguate.

## Implementation Phases

1. **Phase 1**: Keyword-based classification (this issue)
   - Implement basic keyword matching
   - Achieve >80% accuracy on benchmark corpus

2. **Phase 2**: Pattern matching enhancement
   - Add regex patterns for query structure
   - Target >90% accuracy

3. **Phase 3**: Context analysis
   - Analyze context characteristics
   - Handle edge cases better

4. **Future**: LLM-based classification
   - Use small/fast model to classify ambiguous cases
   - Only for low-confidence classifications
