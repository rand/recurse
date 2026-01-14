# Formal Verification Integration Design

> Design document for `recurse-r26`: [SPEC] Formal Verification Integration Design

## Overview

This document specifies the integration of formal verification tools into the RLM system, enabling mathematical proofs of code correctness, invariant checking, and automated verification of generated code against specifications.

## Problem Statement

### Current State

No verification of generated code:

```go
func (c *Controller) GenerateCode(ctx context.Context, spec string) (string, error) {
    code, _, err := c.llm.Complete(ctx, spec)
    // No verification that code meets spec
    return code, err
}
```

**Issues**:
- Generated code may violate specifications
- No proof of correctness for critical code
- Runtime errors from invariant violations
- Manual review required for safety

## Design Goals

1. **Spec extraction**: Derive specifications from natural language
2. **Code verification**: Prove generated code meets specs
3. **Invariant checking**: Verify type and state invariants
4. **Counter-example generation**: Show why code fails
5. **Incremental verification**: Fast re-verification on changes

## Core Types

### Specifications

```go
// internal/verification/types.go

type Specification struct {
    ID          string
    Name        string
    Description string
    Language    SpecLanguage

    // Formal specification
    Preconditions  []Predicate
    Postconditions []Predicate
    Invariants     []Predicate

    // Source
    SourceType  SpecSource
    SourceText  string

    // Verification status
    Verified    bool
    LastChecked *time.Time
}

type SpecLanguage int

const (
    SpecLangSMT     SpecLanguage = iota // SMT-LIB format
    SpecLangTLA                          // TLA+
    SpecLangDafny                        // Dafny
    SpecLangLean                         // Lean 4
    SpecLangNatural                      // Natural language (needs extraction)
)

type SpecSource int

const (
    SourceUserProvided SpecSource = iota
    SourceExtracted                // Extracted from natural language
    SourceInferred                 // Inferred from code
)

type Predicate struct {
    ID          string
    Expression  string
    Language    SpecLanguage
    Variables   []Variable
    Description string
}

type Variable struct {
    Name string
    Type string
    Role VariableRole
}

type VariableRole int

const (
    RoleInput  VariableRole = iota
    RoleOutput
    RoleState
    RoleConstant
)
```

### Verification Results

```go
// internal/verification/results.go

type VerificationResult struct {
    Specification *Specification
    Code          string
    Status        VerificationStatus

    // Details
    ProvedClaims    []*ClaimResult
    FailedClaims    []*ClaimResult
    Warnings        []string

    // Counter-examples
    CounterExamples []*CounterExample

    // Performance
    Duration        time.Duration
    SolverCalls     int
}

type VerificationStatus int

const (
    StatusVerified   VerificationStatus = iota // All claims proved
    StatusFailed                               // Some claims failed
    StatusTimeout                              // Solver timed out
    StatusUnknown                              // Could not determine
    StatusError                                // Verification error
)

type ClaimResult struct {
    Claim       *Predicate
    Status      ClaimStatus
    Proof       string // Proof trace if available
    FailReason  string // Why it failed
}

type ClaimStatus int

const (
    ClaimProved ClaimStatus = iota
    ClaimFailed
    ClaimTimeout
    ClaimUnknown
)

type CounterExample struct {
    Claim       *Predicate
    Inputs      map[string]any
    State       map[string]any
    Trace       []TraceStep
    Explanation string
}

type TraceStep struct {
    Line        int
    Statement   string
    StateBefore map[string]any
    StateAfter  map[string]any
}
```

## Specification Extractor

### Extractor Implementation

```go
// internal/verification/extractor.go

type SpecExtractor struct {
    llm      LLMClient
    parser   *SpecParser
}

func NewSpecExtractor(llm LLMClient) *SpecExtractor {
    return &SpecExtractor{
        llm:    llm,
        parser: NewSpecParser(),
    }
}

func (e *SpecExtractor) Extract(ctx context.Context, naturalSpec string) (*Specification, error) {
    prompt := fmt.Sprintf(`Extract formal specifications from this natural language description.

Description:
%s

Extract:
1. Preconditions (what must be true before execution)
2. Postconditions (what must be true after execution)
3. Invariants (what must always be true)

Format each as SMT-LIB assertions. Use these conventions:
- Function inputs: input_<name>
- Function outputs: output_<name>
- State variables: state_<name>
- Use standard SMT types: Int, Real, Bool, (Array Int Int), String

Example output:
PRECONDITIONS:
(assert (>= input_n 0))
(assert (< input_n 1000))

POSTCONDITIONS:
(assert (= output_result (* input_n input_n)))

INVARIANTS:
(assert (>= state_count 0))

Now extract specifications:`, naturalSpec)

    response, _, err := e.llm.Complete(ctx, prompt)
    if err != nil {
        return nil, fmt.Errorf("extraction failed: %w", err)
    }

    return e.parser.Parse(response, naturalSpec)
}

func (e *SpecExtractor) InferFromCode(ctx context.Context, code string) (*Specification, error) {
    prompt := fmt.Sprintf(`Analyze this code and infer formal specifications.

Code:
%s

Infer:
1. Preconditions (input constraints the code assumes)
2. Postconditions (guarantees the code provides)
3. Invariants (properties that hold throughout execution)

Format as SMT-LIB assertions.`, code)

    response, _, err := e.llm.Complete(ctx, prompt)
    if err != nil {
        return nil, fmt.Errorf("inference failed: %w", err)
    }

    spec, err := e.parser.Parse(response, "")
    if err != nil {
        return nil, err
    }
    spec.SourceType = SourceInferred

    return spec, nil
}
```

### Spec Parser

```go
// internal/verification/parser.go

type SpecParser struct {
    smtParser *SMTParser
}

func (p *SpecParser) Parse(extracted string, source string) (*Specification, error) {
    spec := &Specification{
        ID:         generateID(),
        Language:   SpecLangSMT,
        SourceType: SourceExtracted,
        SourceText: source,
    }

    // Parse sections
    sections := p.splitSections(extracted)

    // Parse preconditions
    if pre, ok := sections["PRECONDITIONS"]; ok {
        preds, err := p.parsePredicates(pre)
        if err != nil {
            return nil, fmt.Errorf("parse preconditions: %w", err)
        }
        spec.Preconditions = preds
    }

    // Parse postconditions
    if post, ok := sections["POSTCONDITIONS"]; ok {
        preds, err := p.parsePredicates(post)
        if err != nil {
            return nil, fmt.Errorf("parse postconditions: %w", err)
        }
        spec.Postconditions = preds
    }

    // Parse invariants
    if inv, ok := sections["INVARIANTS"]; ok {
        preds, err := p.parsePredicates(inv)
        if err != nil {
            return nil, fmt.Errorf("parse invariants: %w", err)
        }
        spec.Invariants = inv
    }

    return spec, nil
}

func (p *SpecParser) parsePredicates(text string) ([]Predicate, error) {
    var predicates []Predicate

    // Extract (assert ...) expressions
    assertRegex := regexp.MustCompile(`\(assert\s+(.+?)\)`)
    matches := assertRegex.FindAllStringSubmatch(text, -1)

    for _, match := range matches {
        if len(match) < 2 {
            continue
        }

        expr := match[1]
        vars := p.extractVariables(expr)

        predicates = append(predicates, Predicate{
            ID:         generateID(),
            Expression: expr,
            Language:   SpecLangSMT,
            Variables:  vars,
        })
    }

    return predicates, nil
}
```

## Verifier Implementation

### SMT Verifier

```go
// internal/verification/smt.go

type SMTVerifier struct {
    solverPath string
    timeout    time.Duration
}

func NewSMTVerifier(solverPath string) *SMTVerifier {
    return &SMTVerifier{
        solverPath: solverPath,
        timeout:    30 * time.Second,
    }
}

func (v *SMTVerifier) Verify(ctx context.Context, spec *Specification, code string) (*VerificationResult, error) {
    start := time.Now()
    result := &VerificationResult{
        Specification: spec,
        Code:          code,
    }

    // Translate code to SMT
    smtCode, err := v.translateToSMT(code)
    if err != nil {
        result.Status = StatusError
        return result, fmt.Errorf("translation failed: %w", err)
    }

    // Build verification conditions
    vcs := v.buildVerificationConditions(spec, smtCode)

    // Check each verification condition
    var failed bool
    for _, vc := range vcs {
        claimResult, counterExample := v.checkVC(ctx, vc)

        if claimResult.Status == ClaimProved {
            result.ProvedClaims = append(result.ProvedClaims, claimResult)
        } else {
            result.FailedClaims = append(result.FailedClaims, claimResult)
            failed = true

            if counterExample != nil {
                result.CounterExamples = append(result.CounterExamples, counterExample)
            }
        }
    }

    if failed {
        result.Status = StatusFailed
    } else {
        result.Status = StatusVerified
    }

    result.Duration = time.Since(start)
    return result, nil
}

func (v *SMTVerifier) buildVerificationConditions(spec *Specification, smtCode string) []*VerificationCondition {
    var vcs []*VerificationCondition

    // For each postcondition: Pre ∧ Code → Post
    for _, post := range spec.Postconditions {
        vc := &VerificationCondition{
            Claim:         &post,
            Preconditions: spec.Preconditions,
            Code:          smtCode,
            Goal:          post.Expression,
        }
        vcs = append(vcs, vc)
    }

    // For each invariant: Pre ∧ Code → Inv (throughout execution)
    for _, inv := range spec.Invariants {
        vc := &VerificationCondition{
            Claim:         &inv,
            Preconditions: spec.Preconditions,
            Code:          smtCode,
            Goal:          inv.Expression,
            IsInvariant:   true,
        }
        vcs = append(vcs, vc)
    }

    return vcs
}

func (v *SMTVerifier) checkVC(ctx context.Context, vc *VerificationCondition) (*ClaimResult, *CounterExample) {
    // Build SMT query
    query := v.buildSMTQuery(vc)

    // Run solver
    ctx, cancel := context.WithTimeout(ctx, v.timeout)
    defer cancel()

    cmd := exec.CommandContext(ctx, v.solverPath)
    cmd.Stdin = strings.NewReader(query)
    output, err := cmd.Output()

    if ctx.Err() == context.DeadlineExceeded {
        return &ClaimResult{
            Claim:  vc.Claim,
            Status: ClaimTimeout,
        }, nil
    }

    if err != nil {
        return &ClaimResult{
            Claim:      vc.Claim,
            Status:     ClaimUnknown,
            FailReason: err.Error(),
        }, nil
    }

    // Parse result
    result := strings.TrimSpace(string(output))

    if result == "unsat" {
        // No counter-example exists = verified
        return &ClaimResult{
            Claim:  vc.Claim,
            Status: ClaimProved,
        }, nil
    }

    if result == "sat" {
        // Counter-example exists = failed
        counterExample := v.extractCounterExample(output)
        return &ClaimResult{
            Claim:      vc.Claim,
            Status:     ClaimFailed,
            FailReason: "Counter-example found",
        }, counterExample
    }

    return &ClaimResult{
        Claim:  vc.Claim,
        Status: ClaimUnknown,
    }, nil
}

func (v *SMTVerifier) buildSMTQuery(vc *VerificationCondition) string {
    var sb strings.Builder

    // Declare logic
    sb.WriteString("(set-logic ALL)\n")

    // Declare variables
    for _, v := range vc.Claim.Variables {
        sb.WriteString(fmt.Sprintf("(declare-const %s %s)\n", v.Name, v.Type))
    }

    // Assert preconditions
    for _, pre := range vc.Preconditions {
        sb.WriteString(fmt.Sprintf("(assert %s)\n", pre.Expression))
    }

    // Assert code semantics
    sb.WriteString(vc.Code)
    sb.WriteString("\n")

    // Assert negation of goal (looking for counter-example)
    sb.WriteString(fmt.Sprintf("(assert (not %s))\n", vc.Goal))

    // Check satisfiability
    sb.WriteString("(check-sat)\n")
    sb.WriteString("(get-model)\n")

    return sb.String()
}
```

### Code Translator

```go
// internal/verification/translator.go

type CodeTranslator struct {
    llm LLMClient
}

func (t *CodeTranslator) TranslateToSMT(ctx context.Context, code string, language string) (string, error) {
    prompt := fmt.Sprintf(`Translate this %s code to SMT-LIB format for verification.

Code:
%s

Requirements:
1. Model all variables as SMT constants
2. Model assignments as assertions about equality
3. Model conditionals as implications
4. Model loops as bounded unrolling (max 10 iterations)
5. Use standard SMT types

Output only the SMT-LIB code.`, language, code)

    response, _, err := t.llm.Complete(ctx, prompt)
    if err != nil {
        return "", err
    }

    return extractSMTCode(response), nil
}

func (t *CodeTranslator) TranslateGoToSMT(code string) (string, error) {
    // Parse Go code
    fset := token.NewFileSet()
    f, err := parser.ParseFile(fset, "", code, 0)
    if err != nil {
        return "", fmt.Errorf("parse error: %w", err)
    }

    // Translate AST to SMT
    translator := &goSMTTranslator{
        vars:    make(map[string]string),
        asserts: []string{},
    }

    ast.Walk(translator, f)

    return translator.String(), nil
}

type goSMTTranslator struct {
    vars    map[string]string
    asserts []string
}

func (t *goSMTTranslator) Visit(node ast.Node) ast.Visitor {
    switch n := node.(type) {
    case *ast.AssignStmt:
        t.translateAssignment(n)
    case *ast.IfStmt:
        t.translateIf(n)
    case *ast.ReturnStmt:
        t.translateReturn(n)
    }
    return t
}
```

## Verification Service

### Service Implementation

```go
// internal/verification/service.go

type VerificationService struct {
    extractor  *SpecExtractor
    verifier   *SMTVerifier
    translator *CodeTranslator
    cache      *VerificationCache
    logger     *slog.Logger
}

func NewVerificationService(llm LLMClient, solverPath string) *VerificationService {
    return &VerificationService{
        extractor:  NewSpecExtractor(llm),
        verifier:   NewSMTVerifier(solverPath),
        translator: &CodeTranslator{llm: llm},
        cache:      NewVerificationCache(),
    }
}

func (s *VerificationService) VerifyCode(
    ctx context.Context,
    code string,
    spec *Specification,
) (*VerificationResult, error) {
    // Check cache
    cacheKey := s.cacheKey(code, spec)
    if cached := s.cache.Get(cacheKey); cached != nil {
        return cached, nil
    }

    // Verify
    result, err := s.verifier.Verify(ctx, spec, code)
    if err != nil {
        return nil, err
    }

    // Cache result
    s.cache.Set(cacheKey, result)

    return result, nil
}

func (s *VerificationService) VerifyWithNaturalSpec(
    ctx context.Context,
    code string,
    naturalSpec string,
) (*VerificationResult, error) {
    // Extract formal spec
    spec, err := s.extractor.Extract(ctx, naturalSpec)
    if err != nil {
        return nil, fmt.Errorf("spec extraction: %w", err)
    }

    return s.VerifyCode(ctx, code, spec)
}

func (s *VerificationService) GenerateAndVerify(
    ctx context.Context,
    llm LLMClient,
    spec string,
    maxAttempts int,
) (*VerifiedCode, error) {
    // Extract formal specification
    formalSpec, err := s.extractor.Extract(ctx, spec)
    if err != nil {
        return nil, fmt.Errorf("spec extraction: %w", err)
    }

    for attempt := 0; attempt < maxAttempts; attempt++ {
        // Generate code
        code, _, err := llm.Complete(ctx, buildCodePrompt(spec, attempt))
        if err != nil {
            continue
        }

        // Verify
        result, err := s.VerifyCode(ctx, code, formalSpec)
        if err != nil {
            s.logger.Warn("verification error", "attempt", attempt, "error", err)
            continue
        }

        if result.Status == StatusVerified {
            return &VerifiedCode{
                Code:         code,
                Spec:         formalSpec,
                Verification: result,
                Attempts:     attempt + 1,
            }, nil
        }

        // Use counter-examples to guide next attempt
        if len(result.CounterExamples) > 0 {
            spec = augmentSpecWithCounterExamples(spec, result.CounterExamples)
        }
    }

    return nil, fmt.Errorf("failed to generate verified code after %d attempts", maxAttempts)
}

type VerifiedCode struct {
    Code         string
    Spec         *Specification
    Verification *VerificationResult
    Attempts     int
}
```

## Integration

### Controller Integration

```go
// internal/rlm/controller.go

type Controller struct {
    // ... existing fields ...
    verificationService *verification.VerificationService
}

func (c *Controller) GenerateVerifiedCode(ctx context.Context, spec string) (*VerifiedCode, error) {
    return c.verificationService.GenerateAndVerify(ctx, c.llm, spec, 5)
}

func (c *Controller) VerifyGeneratedCode(ctx context.Context, code string, spec string) (*VerificationResult, error) {
    return c.verificationService.VerifyWithNaturalSpec(ctx, code, spec)
}
```

## Observability

### Metrics

```go
var (
    verificationsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rlm_verifications_total",
            Help: "Total verification attempts",
        },
        []string{"status"},
    )

    verificationDuration = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "rlm_verification_duration_seconds",
            Help:    "Verification duration",
            Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
        },
    )

    specsExtracted = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_specs_extracted_total",
            Help: "Specifications extracted from natural language",
        },
    )

    counterExamplesFound = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "rlm_counter_examples_total",
            Help: "Counter-examples found during verification",
        },
    )
)
```

## Success Criteria

1. **Extraction accuracy**: >80% of natural language specs correctly formalized
2. **Verification speed**: <30s for typical function verification
3. **Counter-example quality**: Useful for debugging in >90% of cases
4. **Integration**: Seamless with code generation pipeline
5. **False positive rate**: <5% of verified code contains bugs
