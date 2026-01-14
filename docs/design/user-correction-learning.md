# User Correction Learning Design

> Design document for `recurse-7hg`: [SPEC] User Correction Learning Design

## Overview

This document specifies a system for learning from user corrections, enabling the RLM system to improve over time by remembering and applying feedback on previous outputs.

## Problem Statement

### Current State

No persistence of user feedback:

```
User: "Actually, the function should return nil, not an error"
RLM: "I understand" [forgets immediately]
... later ...
RLM: [makes same mistake]
```

**Issues**:
- Same corrections needed repeatedly
- No learning from mistakes
- User frustration from repetition
- Missed improvement opportunities

## Design Goals

1. **Capture corrections**: Detect and store user feedback
2. **Apply learning**: Use corrections in future responses
3. **Context-aware**: Match corrections to similar situations
4. **Confidence decay**: Reduce reliance on old corrections
5. **User control**: Allow viewing/editing corrections

## Core Types

### Correction Record

```go
// internal/learning/corrections/types.go

type Correction struct {
    ID           string
    Timestamp    time.Time
    SessionID    string

    // What was corrected
    OriginalOutput  string
    CorrectedOutput string

    // Context
    Query        string
    Context      []ContextChunk

    // Classification
    Category     CorrectionCategory
    Severity     float64 // 0=minor, 1=major

    // Embedding for similarity matching
    Embedding    []float32

    // Application tracking
    ApplyCount   int
    LastApplied  *time.Time
    Confidence   float64
}

type CorrectionCategory int

const (
    CategoryFactual     CorrectionCategory = iota // Wrong facts
    CategoryStyle                                  // Formatting/tone
    CategoryCode                                   // Code errors
    CategoryLogic                                  // Reasoning errors
    CategoryPreference                             // User preferences
    CategoryDomain                                 // Domain-specific
)

type CorrectionMatch struct {
    Correction *Correction
    Similarity float64
    Relevance  float64
}
```

### Correction Detector

```go
// internal/learning/corrections/detector.go

type Detector interface {
    // Detect identifies corrections in user messages.
    Detect(ctx context.Context, message string, lastOutput string) (*Detection, error)
}

type Detection struct {
    IsCorrection bool
    Confidence   float64
    Category     CorrectionCategory
    Original     string // What was wrong
    Corrected    string // What it should be
    Explanation  string // Why it's wrong
}

type LLMDetector struct {
    client LLMClient
}

func (d *LLMDetector) Detect(
    ctx context.Context,
    message string,
    lastOutput string,
) (*Detection, error) {
    prompt := fmt.Sprintf(`Analyze if this user message is correcting the previous output.

Previous Output:
%s

User Message:
%s

Determine:
1. Is this a correction? (yes/no)
2. If yes, what category? (factual/style/code/logic/preference/domain)
3. What specifically was wrong?
4. What is the correct version?
5. Confidence (0.0-1.0)

Format:
is_correction: <yes/no>
category: <category>
original: <what was wrong>
corrected: <correct version>
explanation: <why it was wrong>
confidence: <0.0-1.0>`, lastOutput, message)

    response, _, err := d.client.Complete(ctx, prompt)
    if err != nil {
        return nil, err
    }

    return d.parseDetection(response)
}
```

### Correction Patterns

```go
// internal/learning/corrections/patterns.go

// Common correction patterns for fast detection
var correctionPatterns = []struct {
    Pattern    *regexp.Regexp
    Category   CorrectionCategory
    Confidence float64
}{
    {regexp.MustCompile(`(?i)^actually[,\s]`), CategoryFactual, 0.8},
    {regexp.MustCompile(`(?i)^no[,\s].*should`), CategoryLogic, 0.85},
    {regexp.MustCompile(`(?i)that's (wrong|incorrect)`), CategoryFactual, 0.9},
    {regexp.MustCompile(`(?i)^instead[,\s]`), CategoryPreference, 0.7},
    {regexp.MustCompile(`(?i)please (use|prefer|always)`), CategoryPreference, 0.75},
    {regexp.MustCompile(`(?i)the (correct|right) (way|answer)`), CategoryFactual, 0.85},
    {regexp.MustCompile(`(?i)^fix(ed)?:`), CategoryCode, 0.9},
}

func QuickDetect(message string) (bool, CorrectionCategory, float64) {
    for _, p := range correctionPatterns {
        if p.Pattern.MatchString(message) {
            return true, p.Category, p.Confidence
        }
    }
    return false, 0, 0
}
```

## Correction Store

### Store Implementation

```go
// internal/learning/corrections/store.go

type Store struct {
    db        *sql.DB
    embedder  EmbeddingProvider
}

func (s *Store) Save(ctx context.Context, correction *Correction) error {
    // Generate embedding for similarity matching
    if correction.Embedding == nil {
        text := correction.Query + " " + correction.OriginalOutput
        embeddings, err := s.embedder.Embed(ctx, []string{text})
        if err == nil && len(embeddings) > 0 {
            correction.Embedding = embeddings[0]
        }
    }

    _, err := s.db.ExecContext(ctx, `
        INSERT INTO corrections
        (id, timestamp, session_id, original_output, corrected_output,
         query, category, severity, embedding, confidence)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, correction.ID, correction.Timestamp, correction.SessionID,
       correction.OriginalOutput, correction.CorrectedOutput,
       correction.Query, correction.Category, correction.Severity,
       vectorToBlob(correction.Embedding), correction.Confidence)

    return err
}

func (s *Store) FindSimilar(
    ctx context.Context,
    query string,
    output string,
    limit int,
) ([]*CorrectionMatch, error) {
    // Generate query embedding
    text := query + " " + output
    embeddings, err := s.embedder.Embed(ctx, []string{text})
    if err != nil {
        return nil, err
    }

    // Vector similarity search
    rows, err := s.db.QueryContext(ctx, `
        SELECT c.*, 1 - (c.embedding <=> ?) as similarity
        FROM corrections c
        WHERE c.confidence > 0.3
        ORDER BY c.embedding <=> ?
        LIMIT ?
    `, vectorToBlob(embeddings[0]), vectorToBlob(embeddings[0]), limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var matches []*CorrectionMatch
    for rows.Next() {
        correction, similarity, err := scanCorrectionWithSimilarity(rows)
        if err != nil {
            continue
        }

        matches = append(matches, &CorrectionMatch{
            Correction: correction,
            Similarity: similarity,
            Relevance:  s.computeRelevance(correction, similarity),
        })
    }

    return matches, nil
}

func (s *Store) computeRelevance(c *Correction, similarity float64) float64 {
    // Combine similarity with recency and success rate
    recencyFactor := 1.0
    if c.LastApplied != nil {
        age := time.Since(*c.LastApplied)
        recencyFactor = math.Exp(-age.Hours() / (24 * 30)) // Decay over month
    }

    successFactor := 1.0
    if c.ApplyCount > 0 {
        // Assume corrections that are applied often are good
        successFactor = math.Min(1.5, 1.0+float64(c.ApplyCount)*0.1)
    }

    return similarity * c.Confidence * recencyFactor * successFactor
}
```

## Correction Applier

### Applier Implementation

```go
// internal/learning/corrections/applier.go

type Applier struct {
    store     *Store
    llm       LLMClient
    threshold float64
}

func NewApplier(store *Store, llm LLMClient) *Applier {
    return &Applier{
        store:     store,
        llm:       llm,
        threshold: 0.6, // Minimum relevance to apply
    }
}

// Apply finds and applies relevant corrections to output.
func (a *Applier) Apply(
    ctx context.Context,
    query string,
    output string,
) (*ApplyResult, error) {
    // Find similar past corrections
    matches, err := a.store.FindSimilar(ctx, query, output, 5)
    if err != nil {
        return nil, err
    }

    // Filter by threshold
    var applicable []*CorrectionMatch
    for _, m := range matches {
        if m.Relevance >= a.threshold {
            applicable = append(applicable, m)
        }
    }

    if len(applicable) == 0 {
        return &ApplyResult{
            Modified:     false,
            OriginalText: output,
            ModifiedText: output,
        }, nil
    }

    // Apply corrections
    modified, applied, err := a.applyCorrections(ctx, output, applicable)
    if err != nil {
        return nil, err
    }

    // Update apply counts
    for _, c := range applied {
        a.store.RecordApplication(ctx, c.Correction.ID)
    }

    return &ApplyResult{
        Modified:           modified != output,
        OriginalText:       output,
        ModifiedText:       modified,
        AppliedCorrections: applied,
    }, nil
}

func (a *Applier) applyCorrections(
    ctx context.Context,
    output string,
    corrections []*CorrectionMatch,
) (string, []*CorrectionMatch, error) {
    var correctionDescriptions strings.Builder
    for i, c := range corrections {
        correctionDescriptions.WriteString(fmt.Sprintf(
            "[Correction %d]\nOriginal: %s\nCorrected: %s\n\n",
            i+1, c.Correction.OriginalOutput, c.Correction.CorrectedOutput))
    }

    prompt := fmt.Sprintf(`Apply these learned corrections to the output if relevant.

Learned Corrections:
%s

Current Output:
%s

Instructions:
1. Check if any correction applies to similar patterns in the output
2. Apply relevant corrections while preserving unrelated content
3. If no corrections apply, return the original unchanged

Modified Output:`, correctionDescriptions.String(), output)

    modified, _, err := a.llm.Complete(ctx, prompt)
    if err != nil {
        return output, nil, err
    }

    // Determine which corrections were actually applied
    var applied []*CorrectionMatch
    for _, c := range corrections {
        if strings.Contains(output, c.Correction.OriginalOutput) &&
           !strings.Contains(modified, c.Correction.OriginalOutput) {
            applied = append(applied, c)
        }
    }

    return modified, applied, nil
}

type ApplyResult struct {
    Modified           bool
    OriginalText       string
    ModifiedText       string
    AppliedCorrections []*CorrectionMatch
}
```

## Integration

### Controller Integration

```go
// internal/rlm/controller.go

type Controller struct {
    // ... existing fields ...
    correctionDetector *corrections.Detector
    correctionStore    *corrections.Store
    correctionApplier  *corrections.Applier
}

func (c *Controller) HandleUserMessage(ctx context.Context, message string) error {
    // Check if this is a correction
    if c.lastOutput != "" {
        detection, err := c.correctionDetector.Detect(ctx, message, c.lastOutput)
        if err == nil && detection.IsCorrection && detection.Confidence > 0.7 {
            // Save the correction
            correction := &Correction{
                ID:              generateID(),
                Timestamp:       time.Now(),
                SessionID:       c.sessionID,
                OriginalOutput:  detection.Original,
                CorrectedOutput: detection.Corrected,
                Query:           c.lastQuery,
                Category:        detection.Category,
                Confidence:      detection.Confidence,
            }
            if err := c.correctionStore.Save(ctx, correction); err != nil {
                c.logger.Warn("failed to save correction", "error", err)
            }
        }
    }

    return nil
}

func (c *Controller) postProcessOutput(ctx context.Context, output string) (string, error) {
    // Apply learned corrections
    result, err := c.correctionApplier.Apply(ctx, c.currentQuery, output)
    if err != nil {
        return output, nil // Fall back to original
    }

    if result.Modified {
        c.logger.Info("applied learned corrections",
            "count", len(result.AppliedCorrections))
    }

    return result.ModifiedText, nil
}
```

## User Interface

### Correction Management

```go
// internal/tui/corrections.go

type CorrectionView struct {
    store *corrections.Store
}

func (v *CorrectionView) ListCorrections(ctx context.Context) ([]CorrectionSummary, error) {
    corrections, err := v.store.List(ctx, ListOptions{
        Limit:   100,
        OrderBy: "timestamp DESC",
    })
    if err != nil {
        return nil, err
    }

    var summaries []CorrectionSummary
    for _, c := range corrections {
        summaries = append(summaries, CorrectionSummary{
            ID:        c.ID,
            Category:  c.Category.String(),
            Original:  truncate(c.OriginalOutput, 50),
            Corrected: truncate(c.CorrectedOutput, 50),
            Applied:   c.ApplyCount,
            Age:       time.Since(c.Timestamp),
        })
    }

    return summaries, nil
}

func (v *CorrectionView) DeleteCorrection(ctx context.Context, id string) error {
    return v.store.Delete(ctx, id)
}

func (v *CorrectionView) AdjustConfidence(ctx context.Context, id string, delta float64) error {
    return v.store.UpdateConfidence(ctx, id, delta)
}
```

## Observability

### Metrics

```go
var (
    correctionsDetected = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_corrections_detected_total",
            Help: "Corrections detected by category",
        },
        []string{"category"},
    )

    correctionsApplied = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_corrections_applied_total",
            Help: "Corrections successfully applied",
        },
    )

    correctionRelevance = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_correction_relevance",
            Help:    "Relevance scores of matched corrections",
            Buckets: prometheus.LinearBuckets(0, 0.1, 11),
        },
    )
)
```

## Success Criteria

1. **Detection accuracy**: >80% of corrections correctly identified
2. **Application relevance**: >90% of applied corrections are appropriate
3. **User satisfaction**: Reduced repeat corrections
4. **Performance**: <100ms overhead for correction check
5. **Storage efficiency**: <1KB per correction average
