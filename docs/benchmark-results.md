# RLM Benchmark Results

**Date**: 2026-01-12
**Model**: OpenRouter routing (Claude 3.5 Sonnet default)
**Framework**: OOLONG-inspired synthetic tasks

## Executive Summary

RLM and Direct prompting each excel at different task types:

| Task Type | Winner | Why |
|-----------|--------|-----|
| **Counting/Pattern Matching** | RLM | Python regex is exact; 90-97% fewer tokens |
| **Aggregation/Summing** | RLM | Python arithmetic is reliable; 65% fewer tokens |
| **Simple Retrieval (Needle)** | Direct | 10-20x faster, no REPL overhead |
| **Simple Q&A** | Direct | Lower latency, simpler |

## When to Use RLM

Use RLM mode when the task requires **computation over context**:

- Counting occurrences of words/patterns
- Summing or aggregating values scattered in text
- Complex pattern matching with regex
- Multi-step analysis requiring intermediate results

## When to Use Direct Prompting

Use Direct mode when the task is **simple retrieval**:

- Finding a specific piece of information
- Answering questions about content
- Tasks where speed matters more than token efficiency

## Benchmark Results

### Task Type Comparison (8K Context)

| Task Type | Direct | RLM | Winner |
|-----------|--------|-----|--------|
| Needle (retrieval) | 100% in 21s | 100% in 3m37s | **Direct** (faster) |
| Counting | 0% | 100% | **RLM** |
| Aggregation | 100% | 100% | **RLM** (65% fewer tokens) |

### Task Type Comparison (32K Context)

| Task Type | Direct | RLM | Winner |
|-----------|--------|-----|--------|
| Needle (retrieval) | 100% | 100% | **Direct** (faster) |
| Counting | 0% | 100% | **RLM** |

### Token Efficiency

| Task | Direct Tokens | RLM Tokens | Savings |
|------|---------------|------------|---------|
| Counting 8K | 8,139 | 855 | **90% fewer** |
| Counting 32K | 32,116 | 851 | **97% fewer** |
| Aggregation 8K | 8,064 | 2,796 | **65% fewer** |

For computational tasks, RLM externalizes context to Python and processes it with code, rather than including full context in every LLM call.

### Context Length Support

Both modes work correctly at all tested context lengths:

| Context | Direct | RLM |
|---------|--------|-----|
| 4K | 100% | 100% |
| 8K | 100% | 100% |
| 16K | 100% | 100% |
| 32K | 100% | 100% |
| 64K | 100% | 100% |

## How RLM Works

RLM externalizes context to a Python REPL and uses iterative exploration:

1. **Context Externalization**: Large context loaded as Python variable
2. **Iterative Search**: LLM uses `grep()`, `peek()` to search
3. **Computation**: Python handles counting, summing, regex
4. **Final Output**: `FINAL()` returns the answer

Example trace for counting task:
```
Iteration 1: len(re.findall(r'target_word', context))
Iteration 2: FINAL("42")
```

## Task Types

| Type | Complexity | Description |
|------|------------|-------------|
| Needle | O(1) | Find specific hidden identifier |
| Counting | O(n) | Count occurrences of target word |
| Aggregation | O(n) | Sum values scattered in context |
| Pairing | O(n²) | Determine if entities share relationship |

## Running Benchmarks

```bash
# Quick validation (no API calls)
go test -v -run TestQuickBenchmarkWithMock ./internal/rlm/benchmark/

# RLM mode validation (~$0.05)
RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLMMode ./internal/rlm/benchmark/

# Full RLM vs Direct comparison
RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLMvsDirectEvaluation ./internal/rlm/benchmark/
```

**Requirements:**
- `OPENROUTER_API_KEY` environment variable
- Python 3 with venv in `pkg/python/.venv` (for RLM mode)

## Recommendations

| Use Case | Mode | Rationale |
|----------|------|-----------|
| Count word occurrences | RLM | Exact regex matching |
| Sum values in document | RLM | Reliable arithmetic |
| Find specific info | Direct | Faster, simpler |
| Pattern extraction | RLM | Code-based processing |
| Simple Q&A | Direct | Lower latency |

## Future Work

1. **Hybrid Mode**: Auto-select RLM vs Direct based on task classification
2. **RLM Speed**: Reduce iteration count and callback latency
3. **Task Detection**: Heuristics to identify computational vs retrieval tasks
