# Proactive REPL Computation Design

> Design document for `recurse-6p2`: [SPEC] Proactive REPL Computation Design

## Overview

This document specifies a proactive computation system where the Python REPL anticipates likely next operations based on conversation context and pre-computes results. This reduces latency by having results ready before they're explicitly requested.

## Problem Statement

### Current State

Reactive computation only:

```python
# User asks: "What's the average of column X?"
# REPL waits for explicit request, then computes
result = df['X'].mean()  # Computed only when asked
```

**Issues**:
- Latency for every computation request
- Obvious follow-up queries not anticipated
- Idle REPL during LLM thinking time
- No caching of related computations

## Design Goals

1. **Anticipation**: Predict likely next operations
2. **Background execution**: Compute during idle time
3. **Result caching**: Store pre-computed results
4. **Priority ranking**: Focus on most likely requests
5. **Resource management**: Limit speculative compute

## Core Types

### Computation Predictions

```python
# internal/repl/proactive/types.py

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional
from enum import Enum
import time

class PredictionSource(Enum):
    CONVERSATION = "conversation"    # From recent messages
    PATTERN = "pattern"              # From common patterns
    DATAFRAME = "dataframe"          # From loaded data
    CODE = "code"                    # From recent code
    EXPLICIT = "explicit"            # Hinted by LLM

@dataclass
class ComputationPrediction:
    """A predicted computation that might be requested."""
    id: str
    code: str
    description: str
    probability: float  # 0.0 to 1.0
    source: PredictionSource
    context: Dict[str, Any] = field(default_factory=dict)
    dependencies: List[str] = field(default_factory=list)
    estimated_time_ms: int = 100
    priority: int = 0

@dataclass
class ProactiveResult:
    """Result of a proactive computation."""
    prediction_id: str
    code: str
    result: Any
    execution_time_ms: float
    computed_at: float
    expires_at: float
    hit_count: int = 0

    def is_expired(self) -> bool:
        return time.time() > self.expires_at

    def record_hit(self) -> None:
        self.hit_count += 1

@dataclass
class ProactiveContext:
    """Context for generating predictions."""
    conversation: List[Dict[str, str]]
    loaded_dataframes: Dict[str, 'DataFrameInfo']
    recent_code: List[str]
    recent_results: List[Any]
    user_preferences: Dict[str, Any]
```

### DataFrame Analysis

```python
# internal/repl/proactive/dataframe.py

from dataclasses import dataclass
from typing import List, Dict, Any
import pandas as pd

@dataclass
class DataFrameInfo:
    """Metadata about a loaded DataFrame."""
    name: str
    shape: tuple
    columns: List[ColumnInfo]
    sample_values: Dict[str, List[Any]]
    memory_bytes: int

@dataclass
class ColumnInfo:
    name: str
    dtype: str
    null_count: int
    unique_count: int
    is_numeric: bool
    is_datetime: bool
    is_categorical: bool
    sample_values: List[Any]

class DataFrameAnalyzer:
    """Analyzes DataFrames to generate predictions."""

    def analyze(self, df: pd.DataFrame, name: str) -> DataFrameInfo:
        columns = []
        sample_values = {}

        for col in df.columns:
            col_info = ColumnInfo(
                name=col,
                dtype=str(df[col].dtype),
                null_count=int(df[col].isna().sum()),
                unique_count=int(df[col].nunique()),
                is_numeric=pd.api.types.is_numeric_dtype(df[col]),
                is_datetime=pd.api.types.is_datetime64_any_dtype(df[col]),
                is_categorical=df[col].dtype.name == 'category' or df[col].nunique() < 20,
                sample_values=df[col].dropna().head(5).tolist()
            )
            columns.append(col_info)
            sample_values[col] = col_info.sample_values

        return DataFrameInfo(
            name=name,
            shape=df.shape,
            columns=columns,
            sample_values=sample_values,
            memory_bytes=df.memory_usage(deep=True).sum()
        )

    def generate_predictions(self, info: DataFrameInfo) -> List[ComputationPrediction]:
        """Generate likely computations for a DataFrame."""
        predictions = []

        # Basic statistics for numeric columns
        for col in info.columns:
            if col.is_numeric:
                predictions.extend([
                    ComputationPrediction(
                        id=f"{info.name}_{col.name}_describe",
                        code=f"{info.name}['{col.name}'].describe()",
                        description=f"Statistics for {col.name}",
                        probability=0.8,
                        source=PredictionSource.DATAFRAME,
                        estimated_time_ms=50
                    ),
                    ComputationPrediction(
                        id=f"{info.name}_{col.name}_hist",
                        code=f"{info.name}['{col.name}'].hist()",
                        description=f"Histogram for {col.name}",
                        probability=0.6,
                        source=PredictionSource.DATAFRAME,
                        estimated_time_ms=200
                    ),
                ])

        # Value counts for categorical columns
        for col in info.columns:
            if col.is_categorical:
                predictions.append(
                    ComputationPrediction(
                        id=f"{info.name}_{col.name}_value_counts",
                        code=f"{info.name}['{col.name}'].value_counts()",
                        description=f"Value counts for {col.name}",
                        probability=0.75,
                        source=PredictionSource.DATAFRAME,
                        estimated_time_ms=30
                    )
                )

        # Correlation matrix for numeric columns
        numeric_cols = [c for c in info.columns if c.is_numeric]
        if len(numeric_cols) >= 2:
            predictions.append(
                ComputationPrediction(
                    id=f"{info.name}_correlation",
                    code=f"{info.name}[{[c.name for c in numeric_cols]}].corr()",
                    description="Correlation matrix",
                    probability=0.7,
                    source=PredictionSource.DATAFRAME,
                    estimated_time_ms=100
                )
            )

        # Missing data analysis
        if any(c.null_count > 0 for c in info.columns):
            predictions.append(
                ComputationPrediction(
                    id=f"{info.name}_missing",
                    code=f"{info.name}.isna().sum()",
                    description="Missing value counts",
                    probability=0.65,
                    source=PredictionSource.DATAFRAME,
                    estimated_time_ms=20
                )
            )

        return predictions
```

## Prediction Engine

### Prediction Implementation

```python
# internal/repl/proactive/predictor.py

from typing import List, Dict, Any, Optional
import re
from dataclasses import dataclass

class PredictionEngine:
    """Generates computation predictions from context."""

    def __init__(self, llm_client: Optional['LLMClient'] = None):
        self.llm_client = llm_client
        self.df_analyzer = DataFrameAnalyzer()
        self.pattern_matcher = PatternMatcher()

    def predict(self, context: ProactiveContext) -> List[ComputationPrediction]:
        predictions = []

        # 1. DataFrame-based predictions
        for name, info in context.loaded_dataframes.items():
            df_preds = self.df_analyzer.generate_predictions(info)
            predictions.extend(df_preds)

        # 2. Pattern-based predictions from recent code
        for code in context.recent_code[-5:]:
            pattern_preds = self.pattern_matcher.match(code, context)
            predictions.extend(pattern_preds)

        # 3. Conversation-based predictions
        conv_preds = self.predict_from_conversation(context.conversation)
        predictions.extend(conv_preds)

        # 4. LLM-based predictions (if available)
        if self.llm_client:
            llm_preds = self.predict_with_llm(context)
            predictions.extend(llm_preds)

        # Deduplicate and rank
        predictions = self.deduplicate(predictions)
        predictions = self.rank(predictions)

        return predictions[:20]  # Top 20 predictions

    def predict_from_conversation(
        self,
        conversation: List[Dict[str, str]]
    ) -> List[ComputationPrediction]:
        predictions = []

        if not conversation:
            return predictions

        last_messages = conversation[-3:]
        combined_text = " ".join(m.get("content", "") for m in last_messages).lower()

        # Detect data analysis intent
        if any(word in combined_text for word in ["average", "mean", "sum", "count"]):
            predictions.append(
                ComputationPrediction(
                    id="aggregation_hint",
                    code="df.describe()",
                    description="Statistical summary",
                    probability=0.7,
                    source=PredictionSource.CONVERSATION
                )
            )

        if any(word in combined_text for word in ["plot", "chart", "graph", "visualize"]):
            predictions.append(
                ComputationPrediction(
                    id="visualization_hint",
                    code="df.plot()",
                    description="Basic plot",
                    probability=0.6,
                    source=PredictionSource.CONVERSATION
                )
            )

        if any(word in combined_text for word in ["missing", "null", "na", "empty"]):
            predictions.append(
                ComputationPrediction(
                    id="missing_data_hint",
                    code="df.isna().sum()",
                    description="Missing data analysis",
                    probability=0.75,
                    source=PredictionSource.CONVERSATION
                )
            )

        return predictions

    def predict_with_llm(self, context: ProactiveContext) -> List[ComputationPrediction]:
        """Use LLM to predict likely next computations."""
        prompt = self._build_prediction_prompt(context)

        try:
            response = self.llm_client.complete(prompt)
            return self._parse_llm_predictions(response)
        except Exception:
            return []

    def _build_prediction_prompt(self, context: ProactiveContext) -> str:
        df_info = "\n".join(
            f"- {name}: {info.shape[0]} rows, columns: {[c.name for c in info.columns]}"
            for name, info in context.loaded_dataframes.items()
        )

        recent_code = "\n".join(context.recent_code[-3:])

        return f"""Based on the current data analysis context, predict the 5 most likely next Python operations.

Loaded DataFrames:
{df_info}

Recent code:
{recent_code}

Return as JSON array with fields: code, description, probability (0.0-1.0)
Only include operations that are very likely to be requested next."""

    def deduplicate(self, predictions: List[ComputationPrediction]) -> List[ComputationPrediction]:
        seen_codes = set()
        unique = []

        for pred in predictions:
            normalized = self._normalize_code(pred.code)
            if normalized not in seen_codes:
                seen_codes.add(normalized)
                unique.append(pred)

        return unique

    def rank(self, predictions: List[ComputationPrediction]) -> List[ComputationPrediction]:
        # Score by probability and estimated time (prefer fast + likely)
        for pred in predictions:
            pred.priority = int(
                pred.probability * 1000 - pred.estimated_time_ms * 0.1
            )

        return sorted(predictions, key=lambda p: -p.priority)

    @staticmethod
    def _normalize_code(code: str) -> str:
        return re.sub(r'\s+', ' ', code.strip())
```

### Pattern Matcher

```python
# internal/repl/proactive/patterns.py

from typing import List, Dict, Tuple
import re

class PatternMatcher:
    """Matches code patterns to generate follow-up predictions."""

    PATTERNS: List[Tuple[str, List[Dict]]] = [
        # After loading data, suggest exploration
        (
            r"pd\.read_csv\(['\"](.+?)['\"]\)",
            [
                {"code": "{df}.head()", "desc": "Preview first rows", "prob": 0.9},
                {"code": "{df}.info()", "desc": "DataFrame info", "prob": 0.85},
                {"code": "{df}.describe()", "desc": "Statistics", "prob": 0.8},
                {"code": "{df}.shape", "desc": "Get dimensions", "prob": 0.75},
            ]
        ),
        # After groupby, suggest aggregations
        (
            r"\.groupby\(['\"](.+?)['\"]\)",
            [
                {"code": "{prev}.mean()", "desc": "Group means", "prob": 0.8},
                {"code": "{prev}.count()", "desc": "Group counts", "prob": 0.75},
                {"code": "{prev}.sum()", "desc": "Group sums", "prob": 0.7},
            ]
        ),
        # After filtering, suggest shape/head
        (
            r"\[.+[<>=].+\]",
            [
                {"code": "{prev}.shape", "desc": "Filtered count", "prob": 0.85},
                {"code": "{prev}.head()", "desc": "Preview filtered", "prob": 0.8},
            ]
        ),
        # After selecting column, suggest statistics
        (
            r"\[['\"](\w+)['\"]\]$",
            [
                {"code": "{prev}.value_counts()", "desc": "Value counts", "prob": 0.7},
                {"code": "{prev}.describe()", "desc": "Column stats", "prob": 0.75},
                {"code": "{prev}.hist()", "desc": "Histogram", "prob": 0.6},
            ]
        ),
    ]

    def match(self, code: str, context: ProactiveContext) -> List[ComputationPrediction]:
        predictions = []

        for pattern, follow_ups in self.PATTERNS:
            if re.search(pattern, code):
                for follow_up in follow_ups:
                    pred_code = follow_up["code"].format(
                        df="df",  # Default DataFrame name
                        prev="result",  # Reference to previous result
                    )
                    predictions.append(
                        ComputationPrediction(
                            id=f"pattern_{hash(pred_code)}",
                            code=pred_code,
                            description=follow_up["desc"],
                            probability=follow_up["prob"],
                            source=PredictionSource.PATTERN,
                            context={"matched_pattern": pattern}
                        )
                    )

        return predictions
```

## Proactive Executor

### Executor Implementation

```python
# internal/repl/proactive/executor.py

import asyncio
import time
from typing import Dict, List, Optional
from concurrent.futures import ThreadPoolExecutor
import threading

class ProactiveExecutor:
    """Executes predicted computations in the background."""

    def __init__(
        self,
        sandbox: 'Sandbox',
        max_workers: int = 2,
        max_cache_size: int = 50,
        result_ttl_seconds: float = 300.0,  # 5 minutes
        max_compute_time_ms: int = 5000,
    ):
        self.sandbox = sandbox
        self.executor = ThreadPoolExecutor(max_workers=max_workers)
        self.cache: Dict[str, ProactiveResult] = {}
        self.max_cache_size = max_cache_size
        self.result_ttl = result_ttl_seconds
        self.max_compute_time = max_compute_time_ms / 1000.0
        self._lock = threading.Lock()
        self._running: Dict[str, asyncio.Future] = {}

    def submit_predictions(self, predictions: List[ComputationPrediction]) -> None:
        """Submit predictions for background execution."""
        for pred in predictions:
            if self._should_compute(pred):
                self._submit(pred)

    def _should_compute(self, pred: ComputationPrediction) -> bool:
        # Already cached and not expired?
        if pred.id in self.cache and not self.cache[pred.id].is_expired():
            return False

        # Already running?
        if pred.id in self._running:
            return False

        # Too expensive?
        if pred.estimated_time_ms > self.max_compute_time * 1000:
            return False

        # Low probability?
        if pred.probability < 0.3:
            return False

        return True

    def _submit(self, pred: ComputationPrediction) -> None:
        future = self.executor.submit(self._execute, pred)
        self._running[pred.id] = future

    def _execute(self, pred: ComputationPrediction) -> Optional[ProactiveResult]:
        start_time = time.time()

        try:
            # Execute in sandbox with timeout
            result = self.sandbox.execute(
                pred.code,
                timeout=self.max_compute_time
            )

            execution_time = (time.time() - start_time) * 1000

            proactive_result = ProactiveResult(
                prediction_id=pred.id,
                code=pred.code,
                result=result,
                execution_time_ms=execution_time,
                computed_at=time.time(),
                expires_at=time.time() + self.result_ttl,
            )

            self._cache_result(proactive_result)
            return proactive_result

        except Exception as e:
            # Log but don't propagate - proactive failures are acceptable
            return None
        finally:
            with self._lock:
                self._running.pop(pred.id, None)

    def _cache_result(self, result: ProactiveResult) -> None:
        with self._lock:
            # Evict expired entries
            self._evict_expired()

            # Evict LRU if at capacity
            if len(self.cache) >= self.max_cache_size:
                self._evict_lru()

            self.cache[result.prediction_id] = result

    def _evict_expired(self) -> None:
        expired = [k for k, v in self.cache.items() if v.is_expired()]
        for key in expired:
            del self.cache[key]

    def _evict_lru(self) -> None:
        if not self.cache:
            return

        # Find entry with oldest access (lowest hit count as proxy)
        lru_key = min(self.cache.keys(), key=lambda k: self.cache[k].hit_count)
        del self.cache[lru_key]

    def get_cached(self, code: str) -> Optional[ProactiveResult]:
        """Get a cached result if available."""
        # Look for exact match
        for result in self.cache.values():
            if result.code == code and not result.is_expired():
                result.record_hit()
                return result

        return None

    def get_stats(self) -> Dict[str, Any]:
        """Get executor statistics."""
        with self._lock:
            return {
                "cache_size": len(self.cache),
                "running_count": len(self._running),
                "total_hits": sum(r.hit_count for r in self.cache.values()),
                "cache_entries": [
                    {
                        "id": r.prediction_id,
                        "code": r.code[:50],
                        "hits": r.hit_count,
                        "age_seconds": time.time() - r.computed_at,
                    }
                    for r in self.cache.values()
                ]
            }
```

## Integration

### REPL Manager Integration

```python
# internal/repl/manager.py

class REPLManager:
    def __init__(self, config: dict):
        self.sandbox = Sandbox(config)
        self.prediction_engine = PredictionEngine()
        self.proactive_executor = ProactiveExecutor(
            sandbox=self.sandbox,
            max_workers=config.get('proactive_workers', 2),
        )
        self.context = ProactiveContext(
            conversation=[],
            loaded_dataframes={},
            recent_code=[],
            recent_results=[],
            user_preferences={},
        )

    def execute(self, code: str) -> Any:
        """Execute code, checking proactive cache first."""
        # Check proactive cache
        cached = self.proactive_executor.get_cached(code)
        if cached:
            self._update_context(code, cached.result)
            return cached.result

        # Execute normally
        result = self.sandbox.execute(code)

        # Update context
        self._update_context(code, result)

        # Trigger proactive predictions
        self._trigger_proactive()

        return result

    def _update_context(self, code: str, result: Any) -> None:
        self.context.recent_code.append(code)
        self.context.recent_results.append(result)

        # Keep last 10 items
        self.context.recent_code = self.context.recent_code[-10:]
        self.context.recent_results = self.context.recent_results[-10:]

        # Track loaded DataFrames
        if hasattr(result, 'columns'):  # Is a DataFrame
            df_name = self._extract_df_name(code)
            if df_name:
                analyzer = DataFrameAnalyzer()
                self.context.loaded_dataframes[df_name] = analyzer.analyze(result, df_name)

    def _trigger_proactive(self) -> None:
        """Generate and submit proactive predictions."""
        predictions = self.prediction_engine.predict(self.context)
        self.proactive_executor.submit_predictions(predictions)

    def update_conversation(self, messages: List[Dict[str, str]]) -> None:
        """Update conversation context from RLM."""
        self.context.conversation = messages
        self._trigger_proactive()

    def hint_computation(self, hints: List[str]) -> None:
        """Accept hints from LLM about likely next computations."""
        predictions = [
            ComputationPrediction(
                id=f"hint_{i}",
                code=hint,
                description="LLM-hinted computation",
                probability=0.9,  # High priority for explicit hints
                source=PredictionSource.EXPLICIT,
            )
            for i, hint in enumerate(hints)
        ]
        self.proactive_executor.submit_predictions(predictions)
```

### Go Controller Integration

```go
// internal/rlm/controller.go

type Controller struct {
    // ... existing fields ...
    replMgr *repl.Manager
}

func (c *Controller) Execute(ctx context.Context, task string) (*Result, error) {
    // Update REPL conversation context
    c.replMgr.UpdateConversation(c.conversation)

    // If we can predict likely REPL operations, hint them
    hints := c.predictREPLOperations(task)
    if len(hints) > 0 {
        c.replMgr.HintComputations(hints)
    }

    // Execute task
    result, err := c.executeTask(ctx, task)
    if err != nil {
        return nil, err
    }

    return result, nil
}

func (c *Controller) predictREPLOperations(task string) []string {
    // Quick heuristic prediction
    var hints []string

    lower := strings.ToLower(task)

    if strings.Contains(lower, "load") && strings.Contains(lower, "csv") {
        hints = append(hints, "df.head()", "df.info()", "df.describe()")
    }

    if strings.Contains(lower, "correlation") {
        hints = append(hints, "df.corr()")
    }

    if strings.Contains(lower, "missing") || strings.Contains(lower, "null") {
        hints = append(hints, "df.isna().sum()")
    }

    return hints
}
```

## Observability

### Metrics

```python
# internal/repl/proactive/metrics.py

from prometheus_client import Counter, Histogram, Gauge

proactive_predictions = Counter(
    'rlm_proactive_predictions_total',
    'Total predictions generated',
    ['source']
)

proactive_cache_hits = Counter(
    'rlm_proactive_cache_hits_total',
    'Cache hits for proactive computations'
)

proactive_cache_misses = Counter(
    'rlm_proactive_cache_misses_total',
    'Cache misses for proactive computations'
)

proactive_execution_time = Histogram(
    'rlm_proactive_execution_seconds',
    'Proactive computation execution time',
    buckets=[0.01, 0.05, 0.1, 0.5, 1.0, 5.0]
)

proactive_cache_size = Gauge(
    'rlm_proactive_cache_size',
    'Current proactive cache size'
)

proactive_hit_rate = Gauge(
    'rlm_proactive_hit_rate',
    'Proactive cache hit rate'
)
```

## Success Criteria

1. **Hit rate**: >20% of REPL operations served from proactive cache
2. **Latency reduction**: >50% latency reduction for cache hits
3. **Prediction accuracy**: >40% of predictions actually used
4. **Resource efficiency**: <10% CPU overhead for background compute
5. **Cache efficiency**: <100MB memory for proactive cache
