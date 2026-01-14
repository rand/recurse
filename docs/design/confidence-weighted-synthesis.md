# Confidence-Weighted Synthesis Design

> Design document for `recurse-aa2`: [SPEC] Confidence-Weighted Synthesis Design

## Overview

This document specifies confidence-weighted synthesis for combining results from multiple RLM sub-calls. When decomposing tasks, sub-results have varying reliability. Confidence-weighted synthesis prioritizes high-confidence results and handles uncertain or conflicting outputs appropriately.

## Problem Statement

### Current Synthesis

```go
func (s *Synthesizer) Synthesize(results []SubCallResult) string {
    var combined strings.Builder
    for _, r := range results {
        combined.WriteString(r.Content)
        combined.WriteString("\n")
    }
    return combined.String()
}
```

**Issues**:
- All results treated equally regardless of quality
- No handling of conflicting information
- Uncertain results pollute the synthesis
- No source attribution for claims

### Desired Behavior

- Weight results by confidence scores
- Detect and resolve conflicts
- Flag uncertain portions
- Maintain provenance for claims

## Design Goals

1. **Confidence scoring**: Score each sub-result for reliability
2. **Weighted combination**: Prioritize high-confidence content
3. **Conflict detection**: Identify contradictory results
4. **Uncertainty propagation**: Communicate confidence to user
5. **Source tracking**: Attribute claims to sub-calls

## Core Types

### Confidence Score

```go
// internal/rlm/synthesize/confidence.go

// Confidence represents a reliability score with explanation.
type Confidence struct {
    Score      float64   // 0.0 to 1.0
    Components []ConfidenceComponent
}

type ConfidenceComponent struct {
    Factor string  // e.g., "source_reliability", "internal_consistency"
    Score  float64
    Weight float64
}

// Weighted returns the weighted average confidence.
func (c *Confidence) Weighted() float64 {
    if len(c.Components) == 0 {
        return c.Score
    }

    var sum, weightSum float64
    for _, comp := range c.Components {
        sum += comp.Score * comp.Weight
        weightSum += comp.Weight
    }

    if weightSum == 0 {
        return c.Score
    }

    return sum / weightSum
}

// ConfidenceLevel returns a human-readable level.
func (c *Confidence) Level() ConfidenceLevel {
    score := c.Weighted()
    switch {
    case score >= 0.9:
        return ConfidenceHigh
    case score >= 0.7:
        return ConfidenceMedium
    case score >= 0.5:
        return ConfidenceLow
    default:
        return ConfidenceVeryLow
    }
}

type ConfidenceLevel int

const (
    ConfidenceVeryLow ConfidenceLevel = iota
    ConfidenceLow
    ConfidenceMedium
    ConfidenceHigh
)
```

### Scored Result

```go
// internal/rlm/synthesize/result.go

// ScoredResult wraps a sub-call result with confidence.
type ScoredResult struct {
    SubCallResult
    Confidence  Confidence
    Claims      []Claim      // Extracted factual claims
    Conflicts   []Conflict   // Conflicts with other results
}

// Claim represents a factual assertion.
type Claim struct {
    ID         string
    Statement  string
    Confidence float64
    SourceID   string  // Sub-call that made this claim
    Evidence   string  // Supporting context
}

// Conflict represents contradictory claims.
type Conflict struct {
    ClaimA    *Claim
    ClaimB    *Claim
    Type      ConflictType
    Severity  float64 // 0=minor, 1=major
}

type ConflictType int

const (
    ConflictContradiction ConflictType = iota // Direct opposite
    ConflictInconsistency                      // Logically incompatible
    ConflictAmbiguity                          // Different interpretations
)
```

## Confidence Scorer

### Scorer Interface

```go
// internal/rlm/synthesize/scorer.go

// Scorer calculates confidence for sub-call results.
type Scorer interface {
    Score(ctx context.Context, result *SubCallResult, context *ScoringContext) (*Confidence, error)
}

type ScoringContext struct {
    OriginalQuery  string
    AllResults     []*SubCallResult
    MemoryContext  []ContextChunk
}
```

### LLM-Based Scorer

```go
// internal/rlm/synthesize/llm_scorer.go

type LLMScorer struct {
    client  LLMClient
    factors []ConfidenceFactor
}

type ConfidenceFactor struct {
    Name        string
    Description string
    Weight      float64
}

func DefaultFactors() []ConfidenceFactor {
    return []ConfidenceFactor{
        {
            Name:        "completeness",
            Description: "Does the response fully address the assigned subtask?",
            Weight:      0.25,
        },
        {
            Name:        "consistency",
            Description: "Is the response internally consistent without contradictions?",
            Weight:      0.25,
        },
        {
            Name:        "specificity",
            Description: "Does the response provide specific, actionable information?",
            Weight:      0.20,
        },
        {
            Name:        "grounding",
            Description: "Is the response grounded in the provided context?",
            Weight:      0.20,
        },
        {
            Name:        "hedging",
            Description: "Does the response appropriately express uncertainty?",
            Weight:      0.10,
        },
    }
}

func (s *LLMScorer) Score(
    ctx context.Context,
    result *SubCallResult,
    scoringCtx *ScoringContext,
) (*Confidence, error) {
    prompt := s.buildScoringPrompt(result, scoringCtx)

    response, _, err := s.client.Complete(ctx, prompt)
    if err != nil {
        return nil, err
    }

    components := s.parseScores(response)

    return &Confidence{
        Score:      s.computeOverall(components),
        Components: components,
    }, nil
}

func (s *LLMScorer) buildScoringPrompt(result *SubCallResult, ctx *ScoringContext) string {
    var factorDesc strings.Builder
    for _, f := range s.factors {
        factorDesc.WriteString(fmt.Sprintf("- %s: %s\n", f.Name, f.Description))
    }

    return fmt.Sprintf(`Evaluate this response for confidence/reliability.

Original Query: %s

Subtask: %s

Response:
%s

Rate each factor from 0.0 to 1.0:
%s

Format:
completeness: <score>
consistency: <score>
specificity: <score>
grounding: <score>
hedging: <score>`,
        ctx.OriginalQuery,
        result.Subtask,
        result.Content,
        factorDesc.String())
}
```

### Heuristic Scorer

```go
// internal/rlm/synthesize/heuristic_scorer.go

type HeuristicScorer struct {
    hedgeWords     []string
    certaintyWords []string
}

func NewHeuristicScorer() *HeuristicScorer {
    return &HeuristicScorer{
        hedgeWords: []string{
            "might", "maybe", "perhaps", "possibly", "could be",
            "I think", "I believe", "it seems", "probably",
            "not sure", "uncertain", "unclear",
        },
        certaintyWords: []string{
            "definitely", "certainly", "always", "never",
            "must be", "clearly", "obviously", "undoubtedly",
        },
    }
}

func (s *HeuristicScorer) Score(
    ctx context.Context,
    result *SubCallResult,
    scoringCtx *ScoringContext,
) (*Confidence, error) {
    content := strings.ToLower(result.Content)

    components := []ConfidenceComponent{
        s.scoreLength(result.Content),
        s.scoreHedging(content),
        s.scoreCertainty(content),
        s.scoreStructure(result.Content),
    }

    overall := 0.0
    totalWeight := 0.0
    for _, c := range components {
        overall += c.Score * c.Weight
        totalWeight += c.Weight
    }

    return &Confidence{
        Score:      overall / totalWeight,
        Components: components,
    }, nil
}

func (s *HeuristicScorer) scoreHedging(content string) ConfidenceComponent {
    hedgeCount := 0
    for _, word := range s.hedgeWords {
        hedgeCount += strings.Count(content, word)
    }

    // More hedging = lower confidence (but appropriate)
    // Calibrate: some hedging is good, too much suggests uncertainty
    score := 1.0 - math.Min(float64(hedgeCount)*0.1, 0.5)

    return ConfidenceComponent{
        Factor: "hedging",
        Score:  score,
        Weight: 0.2,
    }
}

func (s *HeuristicScorer) scoreStructure(content string) ConfidenceComponent {
    // Well-structured responses tend to be more reliable
    hasLists := strings.Contains(content, "- ") || strings.Contains(content, "1.")
    hasHeaders := strings.Contains(content, "##") || strings.Contains(content, "**")
    hasCode := strings.Contains(content, "```")

    score := 0.5
    if hasLists {
        score += 0.15
    }
    if hasHeaders {
        score += 0.15
    }
    if hasCode {
        score += 0.2
    }

    return ConfidenceComponent{
        Factor: "structure",
        Score:  math.Min(score, 1.0),
        Weight: 0.15,
    }
}
```

## Claim Extraction

### Claim Extractor

```go
// internal/rlm/synthesize/claims.go

type ClaimExtractor interface {
    Extract(ctx context.Context, result *ScoredResult) ([]Claim, error)
}

type LLMClaimExtractor struct {
    client LLMClient
}

func (e *LLMClaimExtractor) Extract(
    ctx context.Context,
    result *ScoredResult,
) ([]Claim, error) {
    prompt := fmt.Sprintf(`Extract factual claims from this text.

Text:
%s

For each claim, provide:
1. The claim statement
2. A confidence score (0.0-1.0) based on how certain the claim appears
3. Supporting evidence from the text

Format:
[Claim 1]
Statement: <claim>
Confidence: <score>
Evidence: <evidence>

[Claim 2]
...`, result.Content)

    response, _, err := e.client.Complete(ctx, prompt)
    if err != nil {
        return nil, err
    }

    claims := e.parseClaims(response, result.SubCallID)
    return claims, nil
}
```

## Conflict Detection

### Conflict Detector

```go
// internal/rlm/synthesize/conflicts.go

type ConflictDetector interface {
    Detect(ctx context.Context, claims []Claim) ([]Conflict, error)
}

type LLMConflictDetector struct {
    client    LLMClient
    threshold float64 // Minimum severity to report
}

func (d *LLMConflictDetector) Detect(
    ctx context.Context,
    claims []Claim,
) ([]Conflict, error) {
    if len(claims) < 2 {
        return nil, nil
    }

    // Build claim pairs for comparison
    var claimList strings.Builder
    for i, c := range claims {
        claimList.WriteString(fmt.Sprintf("[%d] %s (confidence: %.2f)\n",
            i, c.Statement, c.Confidence))
    }

    prompt := fmt.Sprintf(`Identify conflicts between these claims.

Claims:
%s

For each conflict found, provide:
1. The claim numbers involved
2. Type: contradiction (direct opposite), inconsistency (logically incompatible), or ambiguity (different interpretations)
3. Severity (0.0-1.0): how significant is this conflict?
4. Explanation

Format:
[Conflict 1]
Claims: <num1>, <num2>
Type: <type>
Severity: <score>
Explanation: <explanation>

If no conflicts, respond: NO_CONFLICTS`, claimList.String())

    response, _, err := d.client.Complete(ctx, prompt)
    if err != nil {
        return nil, err
    }

    if strings.Contains(response, "NO_CONFLICTS") {
        return nil, nil
    }

    conflicts := d.parseConflicts(response, claims)

    // Filter by threshold
    var significant []Conflict
    for _, c := range conflicts {
        if c.Severity >= d.threshold {
            significant = append(significant, c)
        }
    }

    return significant, nil
}
```

## Weighted Synthesizer

### Synthesizer

```go
// internal/rlm/synthesize/weighted.go

type WeightedSynthesizer struct {
    scorer     Scorer
    extractor  ClaimExtractor
    detector   ConflictDetector
    llmClient  LLMClient
    config     SynthesisConfig
}

type SynthesisConfig struct {
    MinConfidence     float64 // Minimum confidence to include
    ConflictThreshold float64 // Minimum conflict severity to flag
    IncludeProvenance bool    // Include source attribution
    MaxClaims         int     // Maximum claims to process
}

func DefaultSynthesisConfig() SynthesisConfig {
    return SynthesisConfig{
        MinConfidence:     0.3,
        ConflictThreshold: 0.5,
        IncludeProvenance: true,
        MaxClaims:         50,
    }
}

type SynthesisResult struct {
    Content        string
    OverallConf    float64
    ScoredResults  []*ScoredResult
    Conflicts      []Conflict
    Warnings       []string
}

func (s *WeightedSynthesizer) Synthesize(
    ctx context.Context,
    results []SubCallResult,
    query string,
) (*SynthesisResult, error) {
    scoringCtx := &ScoringContext{
        OriginalQuery: query,
        AllResults:    toPointers(results),
    }

    // Score each result
    scored := make([]*ScoredResult, 0, len(results))
    for i := range results {
        conf, err := s.scorer.Score(ctx, &results[i], scoringCtx)
        if err != nil {
            // Use neutral confidence on error
            conf = &Confidence{Score: 0.5}
        }

        scored = append(scored, &ScoredResult{
            SubCallResult: results[i],
            Confidence:    *conf,
        })
    }

    // Filter low-confidence results
    filtered := s.filterByConfidence(scored)

    // Extract claims
    var allClaims []Claim
    for _, r := range filtered {
        claims, err := s.extractor.Extract(ctx, r)
        if err == nil {
            r.Claims = claims
            allClaims = append(allClaims, claims...)
        }
    }

    // Detect conflicts
    conflicts, _ := s.detector.Detect(ctx, allClaims)

    // Mark conflicts on results
    s.markConflicts(filtered, conflicts)

    // Generate weighted synthesis
    content, err := s.generateSynthesis(ctx, filtered, conflicts, query)
    if err != nil {
        return nil, err
    }

    // Compute overall confidence
    overallConf := s.computeOverallConfidence(filtered)

    // Generate warnings
    warnings := s.generateWarnings(filtered, conflicts)

    return &SynthesisResult{
        Content:       content,
        OverallConf:   overallConf,
        ScoredResults: filtered,
        Conflicts:     conflicts,
        Warnings:      warnings,
    }, nil
}

func (s *WeightedSynthesizer) filterByConfidence(results []*ScoredResult) []*ScoredResult {
    var filtered []*ScoredResult
    for _, r := range results {
        if r.Confidence.Weighted() >= s.config.MinConfidence {
            filtered = append(filtered, r)
        }
    }
    return filtered
}

func (s *WeightedSynthesizer) generateSynthesis(
    ctx context.Context,
    results []*ScoredResult,
    conflicts []Conflict,
    query string,
) (string, error) {
    // Sort by confidence (highest first)
    sort.Slice(results, func(i, j int) bool {
        return results[i].Confidence.Weighted() > results[j].Confidence.Weighted()
    })

    // Build weighted context for synthesis
    var weightedContent strings.Builder
    for _, r := range results {
        conf := r.Confidence.Weighted()
        weightedContent.WriteString(fmt.Sprintf(
            "[Source %s, Confidence: %.0f%%]\n%s\n\n",
            r.SubCallID, conf*100, r.Content))
    }

    // Note conflicts
    var conflictNotes string
    if len(conflicts) > 0 {
        var notes strings.Builder
        notes.WriteString("CONFLICTS DETECTED:\n")
        for _, c := range conflicts {
            notes.WriteString(fmt.Sprintf("- %s vs %s: %s\n",
                c.ClaimA.Statement, c.ClaimB.Statement,
                conflictTypeString(c.Type)))
        }
        conflictNotes = notes.String()
    }

    prompt := fmt.Sprintf(`Synthesize these sources into a coherent response to the query.

Query: %s

Sources (ordered by confidence):
%s

%s

Instructions:
1. Prioritize information from higher-confidence sources
2. If sources conflict, prefer the higher-confidence source and note the disagreement
3. If confidence is low overall, express appropriate uncertainty
4. Include source attribution for key claims if helpful

Synthesized Response:`, query, weightedContent.String(), conflictNotes)

    response, _, err := s.llmClient.Complete(ctx, prompt)
    return response, err
}

func (s *WeightedSynthesizer) computeOverallConfidence(results []*ScoredResult) float64 {
    if len(results) == 0 {
        return 0
    }

    // Weighted average by content length
    var sum, weightSum float64
    for _, r := range results {
        weight := float64(len(r.Content))
        sum += r.Confidence.Weighted() * weight
        weightSum += weight
    }

    return sum / weightSum
}

func (s *WeightedSynthesizer) generateWarnings(
    results []*ScoredResult,
    conflicts []Conflict,
) []string {
    var warnings []string

    // Low confidence warning
    avgConf := s.computeOverallConfidence(results)
    if avgConf < 0.5 {
        warnings = append(warnings,
            fmt.Sprintf("Low overall confidence (%.0f%%). Results may be unreliable.", avgConf*100))
    }

    // Conflict warning
    if len(conflicts) > 0 {
        warnings = append(warnings,
            fmt.Sprintf("%d conflicts detected between sources. Review carefully.", len(conflicts)))
    }

    // Missing sources warning
    lowConfCount := 0
    for _, r := range results {
        if r.Confidence.Weighted() < 0.3 {
            lowConfCount++
        }
    }
    if lowConfCount > 0 {
        warnings = append(warnings,
            fmt.Sprintf("%d low-confidence sources excluded from synthesis.", lowConfCount))
    }

    return warnings
}
```

## Integration

### Controller Integration

```go
// internal/rlm/controller.go

func (c *Controller) synthesizeResults(
    ctx context.Context,
    results []SubCallResult,
    state meta.State,
) (string, error) {
    synthesizer := synthesize.NewWeightedSynthesizer(
        synthesize.NewLLMScorer(c.llmClient),
        synthesize.NewLLMClaimExtractor(c.llmClient),
        synthesize.NewLLMConflictDetector(c.llmClient, 0.5),
        c.llmClient,
        synthesize.DefaultSynthesisConfig(),
    )

    result, err := synthesizer.Synthesize(ctx, results, state.Query)
    if err != nil {
        // Fall back to simple synthesis
        return c.simpleSynthesize(results), nil
    }

    // Log warnings
    for _, warning := range result.Warnings {
        c.logger.Warn("synthesis warning", "warning", warning)
    }

    // Store confidence in trace
    c.traceStore.SetConfidence(state.TraceID, result.OverallConf)

    return result.Content, nil
}
```

## Observability

### Metrics

```go
var (
    synthesisConfidence = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_synthesis_confidence",
            Help:    "Distribution of synthesis confidence scores",
            Buckets: prometheus.LinearBuckets(0, 0.1, 11),
        },
    )

    synthesisConflicts = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_synthesis_conflicts",
            Help:    "Number of conflicts per synthesis",
            Buckets: prometheus.LinearBuckets(0, 1, 10),
        },
    )

    synthesisFilteredResults = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_synthesis_filtered_results",
            Help:    "Results filtered due to low confidence",
            Buckets: prometheus.LinearBuckets(0, 1, 10),
        },
    )
)
```

## Testing Strategy

### Unit Tests

```go
func TestConfidence_Weighted(t *testing.T) {
    conf := Confidence{
        Components: []ConfidenceComponent{
            {Factor: "a", Score: 0.8, Weight: 0.5},
            {Factor: "b", Score: 0.6, Weight: 0.3},
            {Factor: "c", Score: 0.4, Weight: 0.2},
        },
    }

    expected := (0.8*0.5 + 0.6*0.3 + 0.4*0.2) / (0.5 + 0.3 + 0.2)
    assert.InDelta(t, expected, conf.Weighted(), 0.001)
}

func TestWeightedSynthesizer_FilterByConfidence(t *testing.T) {
    synth := &WeightedSynthesizer{
        config: SynthesisConfig{MinConfidence: 0.5},
    }

    results := []*ScoredResult{
        {Confidence: Confidence{Score: 0.8}},
        {Confidence: Confidence{Score: 0.3}}, // Below threshold
        {Confidence: Confidence{Score: 0.6}},
    }

    filtered := synth.filterByConfidence(results)
    assert.Len(t, filtered, 2)
}

func TestConflictDetector_DetectContradiction(t *testing.T) {
    detector := NewMockConflictDetector()

    claims := []Claim{
        {ID: "1", Statement: "The function returns nil on error"},
        {ID: "2", Statement: "The function never returns nil"},
    }

    conflicts, err := detector.Detect(context.Background(), claims)
    require.NoError(t, err)
    assert.Len(t, conflicts, 1)
    assert.Equal(t, ConflictContradiction, conflicts[0].Type)
}
```

## Success Criteria

1. **Confidence accuracy**: Scorer correlates with human judgment (>0.7 correlation)
2. **Conflict detection**: >80% of obvious conflicts detected
3. **Synthesis quality**: Weighted synthesis preferred over naive in blind tests
4. **Performance**: <500ms overhead for typical synthesis
5. **Warnings**: Users report warnings as helpful

## Appendix: Confidence Interpretation

| Score | Level | Interpretation |
|-------|-------|----------------|
| 0.9-1.0 | High | Highly reliable, well-supported |
| 0.7-0.9 | Medium | Generally reliable, minor uncertainty |
| 0.5-0.7 | Low | Uncertain, use with caution |
| 0.0-0.5 | Very Low | Unreliable, verify independently |
