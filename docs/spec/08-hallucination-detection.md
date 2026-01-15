# SPEC-08: Hallucination Detection

## Overview

[SPEC-08.01] The system SHALL implement information-theoretic hallucination detection based on the Strawberry/Pythea methodology to identify procedural hallucinations where models fail to use available evidence.

[SPEC-08.02] Hallucination detection MUST be configurable and MAY be enabled independently for memory storage, output verification, and reasoning trace auditing.

> **Informative**: Procedural hallucinations occur when a model has correct information in context but fails to route to it correctly. This differs from knowledge hallucinations where the model lacks information entirely.

## Theoretical Foundation

[SPEC-08.03] The detection algorithm SHALL compute two probability estimates for each claim:
- **p1**: P(claim is true | WITH cited evidence)
- **p0**: P(claim is true | WITHOUT cited evidence, pseudo-prior)

[SPEC-08.04] The system SHALL calculate information budget metrics:
- **RequiredBits**: KL divergence from target confidence to p0
- **ObservedBits**: KL divergence from p1 to p0
- **BudgetGap**: RequiredBits - ObservedBits

[SPEC-08.05] A claim SHALL be flagged as potentially hallucinated when RequiredBits exceeds ObservedBits, indicating the cited evidence does not justify the stated confidence level.

[SPEC-08.06] The KL divergence for Bernoulli distributions SHALL be computed as:
```
KL(Ber(p) || Ber(q)) = p * log(p/q) + (1-p) * log((1-p)/(1-q))
```

## Core Components

### Claim Extraction

[SPEC-08.07] The system SHALL extract atomic claims from text, where each claim is a single verifiable assertion.

[SPEC-08.08] Claims MUST include:
- Content: The assertion text
- Citations: References to evidence spans (if present)
- Confidence: Stated or inferred confidence level (0.0-1.0)
- Source: Origin location in the text

[SPEC-08.09] Non-assertive spans (questions, instructions, hedged statements) SHALL be marked as non-evidence and excluded from verification.

### Evidence Scrubbing

[SPEC-08.10] To compute p0, the system SHALL scrub cited evidence spans by replacing them with "[EVIDENCE REMOVED]" markers.

[SPEC-08.11] Evidence scrubbing MUST preserve the structural context while removing the factual content that could support the claim.

### Probability Estimation

[SPEC-08.12] Probability estimation SHALL be performed by querying a verification backend with the prompt pattern:
```
Given the following context:
{context}

Is the following claim true? Answer YES or NO.
Claim: {claim}
```

[SPEC-08.13] The system SHALL extract probability from the model's logprobs for the YES/NO tokens.

[SPEC-08.14] When logprobs are unavailable, the system MAY use a fallback estimation method based on repeated sampling.

## Integration Points

### Memory Gate

[SPEC-08.15] When memory gate is enabled, facts MUST be verified before storage in the hypergraph.

[SPEC-08.16] Facts with BudgetGap exceeding the configured threshold SHALL be rejected with status `ErrFactNotGrounded`.

[SPEC-08.17] Verified facts SHALL have their confidence adjusted: `confidence = min(stated_confidence, observed_bits / required_bits)`.

[SPEC-08.18] Rejected facts SHOULD be logged for analysis but MUST NOT be stored in long-term memory.

### Output Verification

[SPEC-08.19] When output verification is enabled, agent responses SHALL be verified before returning to the user.

[SPEC-08.20] The system SHALL extract claims from the response and verify each against the conversation context.

[SPEC-08.21] Verification results SHOULD be made available to the agent for self-correction.

[SPEC-08.22] The system MAY flag responses with high overall hallucination risk but SHOULD NOT automatically reject user-facing outputs.

### Reasoning Trace Auditing

[SPEC-08.23] When trace auditing is enabled, RLM execution traces SHALL be audited for procedural hallucinations.

[SPEC-08.24] Each trace step SHALL be verified as one of:
- ENTAILED: Supported by context or previous steps
- CONTRADICTED: Opposed by context
- NOT_IN_CONTEXT: Asserts unsupported facts
- UNVERIFIABLE: Too vague to determine

[SPEC-08.25] The system SHALL detect post-hoc hallucinations where the final answer is not derivable from the trace steps.

[SPEC-08.26] Post-hoc hallucination detection SHALL use an independent executor that attempts to derive the answer using only the trace steps.

## Verification Backend

[SPEC-08.27] The verification backend SHALL be configurable with the following options:
- `self`: Use the same model for verification
- `haiku`: Use Claude Haiku for fast verification
- `external`: Use an external verification service
- `configurable`: Select backend per-operation

[SPEC-08.28] The backend interface MUST expose:
```go
type VerifierBackend interface {
    EstimateProbability(ctx context.Context, claim, context string) (float64, error)
    BatchEstimate(ctx context.Context, claims []string, context string) ([]float64, error)
}
```

[SPEC-08.29] Verification calls SHOULD be batched for efficiency when multiple claims require verification.

[SPEC-08.30] The system SHALL cache verification results with configurable TTL to avoid redundant calls.

## REPL Integration

[SPEC-08.31] The Python REPL SHALL expose hallucination detection functions:
- `verify_claim(claim, evidence, confidence)`: Verify a single claim
- `verify_claims(text, context)`: Extract and verify all claims
- `audit_trace(steps)`: Audit a reasoning trace

[SPEC-08.32] REPL functions SHALL return structured results including p0, p1, required_bits, observed_bits, budget_gap, and status.

[SPEC-08.33] The REPL MAY be used by the agent for self-verification during task execution.

## Configuration

[SPEC-08.34] Hallucination detection SHALL be configured via the standard configuration system:
```yaml
hallucination:
  enabled: bool
  backend:
    type: string
    default: string
  memory_gate:
    enabled: bool
    min_confidence: float
    reject_unsupported: bool
  output_verification:
    enabled: bool
    flag_threshold_bits: float
  trace_auditing:
    enabled: bool
    check_post_hoc: bool
  batch_size: int
  timeout: duration
  cache_ttl: duration
```

[SPEC-08.35] Default configuration SHALL have hallucination detection disabled to avoid performance impact until explicitly enabled.

## Performance Requirements

[SPEC-08.36] Single claim verification SHOULD complete within 500ms.

[SPEC-08.37] Batch verification of N claims SHOULD complete within 500ms + (N * 100ms).

[SPEC-08.38] Memory gate verification MUST NOT block for more than 2 seconds; timeout SHALL result in storing with reduced confidence.

[SPEC-08.39] Output verification SHOULD run asynchronously and MAY be skipped under high load.

## Metrics and Observability

[SPEC-08.40] The system SHALL track the following metrics:
- Claims verified (count)
- Hallucinations detected (count by status)
- Verification latency (histogram)
- Cache hit rate
- Facts rejected by memory gate

[SPEC-08.41] Hallucination detection events SHALL be logged with sufficient detail for debugging and analysis.

## Error Handling

[SPEC-08.42] Verification backend failures SHALL be handled gracefully:
- Timeout: Store/return with reduced confidence
- Error: Log and continue without verification
- Rate limit: Queue for later verification

[SPEC-08.43] The system MUST NOT crash or block user operations due to hallucination detection failures.

## Future Extensions

> **Informative**: The following are potential future extensions not covered by this specification:
> - Integration with external fact-checking APIs
> - Learning from user corrections to improve detection
> - Automatic claim rewriting to reduce hallucination risk
> - Cross-session hallucination pattern detection
