#!/usr/bin/env python3
"""
Recurse Python REPL Bootstrap

This script runs as a subprocess, receiving JSON-RPC style requests on stdin
and sending responses on stdout. It provides a sandboxed execution environment
for RLM orchestration.
"""

import ast
import collections
import io
import itertools
import json
import os
import pathlib
import re
import resource
import shutil
import signal
import subprocess
import sys
import time
import traceback
from contextlib import redirect_stderr, redirect_stdout
from typing import Any


# =============================================================================
# Resource Limits Initialization
# =============================================================================
# Set hard resource limits based on environment variables from Go.
# This ensures the Python process cannot exceed configured limits.

def _init_resource_limits():
    """Initialize resource limits from environment variables."""
    # Memory limit (address space)
    # NOTE: We intentionally do NOT set RLIMIT_AS because it limits virtual
    # address space, not actual RAM usage. Libraries like numpy/pandas pre-allocate
    # large virtual memory maps that can easily exceed any reasonable limit.
    # Instead, we rely on the OS memory management and monitoring from Go.
    #
    # The RECURSE_MEMORY_LIMIT_MB environment variable is still passed for
    # informational purposes but not enforced via rlimit.

    # CPU time limit per execution
    cpu_limit_sec = os.environ.get("RECURSE_CPU_LIMIT_SEC")
    if cpu_limit_sec:
        try:
            limit_sec = int(cpu_limit_sec)
            # Set CPU time limit - this generates SIGXCPU when exceeded
            resource.setrlimit(resource.RLIMIT_CPU, (limit_sec, limit_sec))
        except (ValueError, resource.error) as e:
            sys.stderr.write(f"Warning: Could not set CPU limit: {e}\n")


def _handle_cpu_exceeded(signum, frame):
    """Handle SIGXCPU (CPU time limit exceeded)."""
    raise RuntimeError("CPU time limit exceeded")


# Install signal handler for CPU limit
signal.signal(signal.SIGXCPU, _handle_cpu_exceeded)

# Initialize limits at module load time
_init_resource_limits()

# Try to import pydantic if available
try:
    import pydantic
    PYDANTIC_AVAILABLE = True
except ImportError:
    PYDANTIC_AVAILABLE = False

# Try to import data science libraries if available
try:
    import numpy as np
    NUMPY_AVAILABLE = True
except ImportError:
    np = None
    NUMPY_AVAILABLE = False

try:
    import pandas as pd
    PANDAS_AVAILABLE = True
except ImportError:
    pd = None
    PANDAS_AVAILABLE = False

try:
    import polars as pl
    POLARS_AVAILABLE = True
except ImportError:
    pl = None
    POLARS_AVAILABLE = False

try:
    import seaborn as sns
    SEABORN_AVAILABLE = True
except ImportError:
    sns = None
    SEABORN_AVAILABLE = False


# =============================================================================
# CLI Tool Wrappers
# =============================================================================
# These functions provide access to Python development tools (uv, ruff, ty)
# from within the REPL.


def _find_tool(name: str) -> str | None:
    """Find a CLI tool, checking venv bin first, then PATH."""
    # Check venv bin directory first
    venv_bin = os.path.join(os.path.dirname(sys.executable), name)
    if os.path.isfile(venv_bin) and os.access(venv_bin, os.X_OK):
        return venv_bin
    # Fall back to PATH
    return shutil.which(name)


def uv(*args: str, capture: bool = True) -> str:
    """
    Run uv (Python package manager) with the given arguments.

    Args:
        *args: Command-line arguments to pass to uv
        capture: If True, capture and return output. If False, print directly.

    Returns:
        Command output as a string (if capture=True)

    Examples:
        >>> uv("pip", "list")  # List installed packages
        >>> uv("pip", "install", "requests")  # Install a package
        >>> uv("run", "python", "-c", "print('hello')")  # Run Python code
        >>> uv("--version")  # Show uv version
    """
    tool_path = _find_tool("uv")
    if not tool_path:
        raise RuntimeError("uv not found. Install with: curl -LsSf https://astral.sh/uv/install.sh | sh")

    result = subprocess.run(
        [tool_path, *args],
        capture_output=capture,
        text=True,
    )

    if capture:
        output = result.stdout
        if result.stderr:
            output += result.stderr
        if result.returncode != 0:
            raise RuntimeError(f"uv failed (exit {result.returncode}):\n{output}")
        return output
    else:
        if result.returncode != 0:
            raise RuntimeError(f"uv failed with exit code {result.returncode}")
        return ""


def ruff(*args: str, capture: bool = True) -> str:
    """
    Run ruff (Python linter/formatter) with the given arguments.

    Args:
        *args: Command-line arguments to pass to ruff
        capture: If True, capture and return output. If False, print directly.

    Returns:
        Command output as a string (if capture=True)

    Examples:
        >>> ruff("check", "myfile.py")  # Lint a file
        >>> ruff("check", ".", "--fix")  # Lint and fix current directory
        >>> ruff("format", "myfile.py")  # Format a file
        >>> ruff("format", ".", "--check")  # Check formatting without changes
        >>> ruff("--version")  # Show ruff version
    """
    tool_path = _find_tool("ruff")
    if not tool_path:
        raise RuntimeError("ruff not found. Install with: uv pip install ruff")

    result = subprocess.run(
        [tool_path, *args],
        capture_output=capture,
        text=True,
    )

    if capture:
        output = result.stdout
        if result.stderr:
            output += result.stderr
        # ruff returns non-zero for lint errors, which is expected behavior
        return output
    else:
        return ""


def ty(*args: str, capture: bool = True) -> str:
    """
    Run ty (Python type checker) with the given arguments.

    Args:
        *args: Command-line arguments to pass to ty
        capture: If True, capture and return output. If False, print directly.

    Returns:
        Command output as a string (if capture=True)

    Examples:
        >>> ty("check", "myfile.py")  # Type check a file
        >>> ty("check", ".")  # Type check current directory
        >>> ty("--version")  # Show ty version
    """
    tool_path = _find_tool("ty")
    if not tool_path:
        raise RuntimeError("ty not found. Install with: uv pip install ty")

    result = subprocess.run(
        [tool_path, *args],
        capture_output=capture,
        text=True,
    )

    if capture:
        output = result.stdout
        if result.stderr:
            output += result.stderr
        # ty returns non-zero for type errors, which is expected behavior
        return output
    else:
        return ""


def lint(path: str = ".", fix: bool = False) -> str:
    """
    Convenience function to lint Python code with ruff.

    Args:
        path: File or directory to lint (default: current directory)
        fix: If True, automatically fix issues where possible

    Returns:
        Lint output

    Examples:
        >>> lint()  # Lint current directory
        >>> lint("myfile.py")  # Lint a specific file
        >>> lint(fix=True)  # Lint and fix
    """
    args = ["check", path]
    if fix:
        args.append("--fix")
    return ruff(*args)


def fmt(path: str = ".", check: bool = False) -> str:
    """
    Convenience function to format Python code with ruff.

    Args:
        path: File or directory to format (default: current directory)
        check: If True, check formatting without making changes

    Returns:
        Format output

    Examples:
        >>> fmt()  # Format current directory
        >>> fmt("myfile.py")  # Format a specific file
        >>> fmt(check=True)  # Check formatting only
    """
    args = ["format", path]
    if check:
        args.append("--check")
    return ruff(*args)


def typecheck(path: str = ".") -> str:
    """
    Convenience function to type check Python code with ty.

    Args:
        path: File or directory to type check (default: current directory)

    Returns:
        Type check output

    Examples:
        >>> typecheck()  # Type check current directory
        >>> typecheck("myfile.py")  # Type check a specific file
    """
    return ty("check", path)


# =============================================================================
# RLM Helper Functions
# =============================================================================
# These functions implement the "Recursive Language Model" paradigm where
# context is externalized as manipulable Python variables. The LLM writes
# code to peek, grep, partition, and transform context rather than ingesting
# it directly.


class RLMContext:
    """Wrapper for externalized context with helper methods."""

    def __init__(self, content: str, name: str = "context", metadata: dict = None):
        self.content = content
        self.name = name
        self.metadata = metadata or {}
        self._lines = None

    @property
    def lines(self) -> list[str]:
        """Lazily split content into lines."""
        if self._lines is None:
            self._lines = self.content.split('\n')
        return self._lines

    def __len__(self) -> int:
        return len(self.content)

    def __str__(self) -> str:
        return self.content

    def __repr__(self) -> str:
        preview = self.content[:100] + "..." if len(self.content) > 100 else self.content
        return f"RLMContext({self.name!r}, len={len(self)}, preview={preview!r})"

    def __getitem__(self, key):
        """Support slicing."""
        return self.content[key]


def peek(ctx, start: int = 0, end: int = None, by_lines: bool = False) -> str:
    """
    View a slice of context.

    Args:
        ctx: Context string or RLMContext object
        start: Start index (character or line based on by_lines)
        end: End index (exclusive), None for end of content
        by_lines: If True, slice by line numbers instead of characters

    Returns:
        The sliced content as a string

    Example:
        >>> peek(context, 0, 1000)  # First 1000 chars
        >>> peek(context, 0, 50, by_lines=True)  # First 50 lines
    """
    content = ctx.content if isinstance(ctx, RLMContext) else str(ctx)

    if by_lines:
        lines = content.split('\n')
        if end is None:
            selected = lines[start:]
        else:
            selected = lines[start:end]
        return '\n'.join(selected)
    else:
        if end is None:
            return content[start:]
        return content[start:end]


def grep(ctx, pattern: str, context_lines: int = 0, ignore_case: bool = True) -> list[dict]:
    """
    Search for pattern in context, returning matching lines with context.

    Args:
        ctx: Context string or RLMContext object
        pattern: Regex pattern to search for
        context_lines: Number of lines before/after each match to include
        ignore_case: Whether to ignore case in pattern matching

    Returns:
        List of dicts with 'line_num', 'line', 'context_before', 'context_after'

    Example:
        >>> matches = grep(context, r"def \\w+")
        >>> matches = grep(context, "error", context_lines=2)
    """
    content = ctx.content if isinstance(ctx, RLMContext) else str(ctx)
    lines = content.split('\n')

    flags = re.IGNORECASE if ignore_case else 0
    compiled = re.compile(pattern, flags)

    results = []
    for i, line in enumerate(lines):
        if compiled.search(line):
            result = {
                'line_num': i + 1,  # 1-indexed
                'line': line,
            }
            if context_lines > 0:
                start = max(0, i - context_lines)
                end = min(len(lines), i + context_lines + 1)
                result['context_before'] = lines[start:i]
                result['context_after'] = lines[i+1:end]
            results.append(result)

    return results


def partition(ctx, n: int = 4, overlap: int = 0) -> list[str]:
    """
    Split context into n roughly equal chunks.

    Args:
        ctx: Context string or RLMContext object
        n: Number of partitions to create
        overlap: Number of characters to overlap between partitions

    Returns:
        List of string chunks

    Example:
        >>> chunks = partition(context, n=4)
        >>> results = [process(chunk) for chunk in chunks]
    """
    content = ctx.content if isinstance(ctx, RLMContext) else str(ctx)

    if n <= 0:
        raise ValueError("n must be positive")
    if len(content) == 0:
        return [""] * n

    chunk_size = len(content) // n
    if chunk_size == 0:
        chunk_size = 1

    chunks = []
    start = 0
    for i in range(n):
        if i == n - 1:
            # Last chunk gets the remainder
            chunks.append(content[start:])
        else:
            end = start + chunk_size + overlap
            chunks.append(content[start:end])
            start = start + chunk_size

    return chunks


def partition_by_lines(ctx, n: int = 4, overlap_lines: int = 0) -> list[str]:
    """
    Split context into n chunks by lines (respects line boundaries).

    Args:
        ctx: Context string or RLMContext object
        n: Number of partitions
        overlap_lines: Number of lines to overlap between chunks

    Returns:
        List of string chunks
    """
    content = ctx.content if isinstance(ctx, RLMContext) else str(ctx)
    lines = content.split('\n')

    if n <= 0:
        raise ValueError("n must be positive")
    if len(lines) == 0:
        return [""] * n

    chunk_size = len(lines) // n
    if chunk_size == 0:
        chunk_size = 1

    chunks = []
    start = 0
    for i in range(n):
        if i == n - 1:
            selected = lines[start:]
        else:
            end = start + chunk_size + overlap_lines
            selected = lines[start:end]
            start = start + chunk_size
        chunks.append('\n'.join(selected))

    return chunks


def extract_functions(ctx, language: str = "python") -> list[dict]:
    """
    Extract function definitions from code context.

    Args:
        ctx: Context string or RLMContext object
        language: Programming language ('python', 'go', 'javascript', etc.)

    Returns:
        List of dicts with 'name', 'start_line', 'signature', 'body'
    """
    content = ctx.content if isinstance(ctx, RLMContext) else str(ctx)
    lines = content.split('\n')

    patterns = {
        'python': r'^(\s*)(async\s+)?def\s+(\w+)\s*\((.*?)\)',
        'go': r'^func\s+(?:\(.*?\)\s+)?(\w+)\s*\((.*?)\)',
        'javascript': r'^(?:async\s+)?function\s+(\w+)\s*\((.*?)\)|^(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(',
        'typescript': r'^(?:async\s+)?function\s+(\w+)|^(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(',
    }

    pattern = patterns.get(language, patterns['python'])
    compiled = re.compile(pattern)

    functions = []
    for i, line in enumerate(lines):
        match = compiled.search(line)
        if match:
            functions.append({
                'name': match.group(3) if language == 'python' else match.group(1) or match.group(2) or match.group(3),
                'start_line': i + 1,
                'signature': line.strip(),
            })

    return functions


def count_tokens_approx(text: str) -> int:
    """
    Approximate token count (roughly 4 chars per token).

    This is a rough estimate - actual tokenization varies by model.
    """
    return len(text) // 4


def summarize(ctx, max_length: int = 500, focus: str = None, model: str = "fast") -> str:
    """
    Summarize context using an LLM call.

    This is a key emergent strategy for compressing information
    before passing to higher-level reasoning.

    Args:
        ctx: Context string or RLMContext object to summarize
        max_length: Approximate maximum length of summary in characters
        focus: Optional focus area for the summary (e.g., "API endpoints", "error handling")
        model: Model tier to use - 'fast' recommended for summaries

    Returns:
        A string summary of the context

    Example:
        >>> summary = summarize(large_file_content, focus="main function logic")
        >>> answer = llm_call("Based on this summary, what's the purpose?", summary)
    """
    content = ctx.content if isinstance(ctx, RLMContext) else str(ctx)

    prompt = f"Summarize the following content in approximately {max_length} characters."
    if focus:
        prompt += f" Focus on: {focus}."
    prompt += " Be concise and preserve key details."

    return llm_call(prompt, content, model)


def map_reduce(ctx, map_prompt: str, reduce_prompt: str, n_chunks: int = 4, model: str = "fast") -> str:
    """
    Apply map-reduce pattern to process large context.

    This is a powerful emergent strategy: partition the context,
    apply a map operation to each chunk, then reduce the results.

    Args:
        ctx: Context string or RLMContext object
        map_prompt: Prompt to apply to each chunk
        reduce_prompt: Prompt to combine the mapped results
        n_chunks: Number of chunks to partition into
        model: Model tier to use

    Returns:
        The reduced result string

    Example:
        >>> result = map_reduce(
        ...     large_codebase,
        ...     map_prompt="List all function names and their purpose",
        ...     reduce_prompt="Combine these function lists and identify the main API",
        ...     n_chunks=4
        ... )
    """
    chunks = partition(ctx, n=n_chunks)

    # Map phase
    mapped = llm_batch(
        [map_prompt] * len(chunks),
        contexts=chunks,
        model=model
    )

    # Reduce phase
    combined = "\n\n---\n\n".join([f"Chunk {i+1}:\n{m}" for i, m in enumerate(mapped)])
    return llm_call(reduce_prompt, combined, model)


def find_relevant(ctx, query: str, top_k: int = 5, model: str = "fast") -> list[dict]:
    """
    Find the most relevant sections of context for a query.

    Combines grep for keyword matching with LLM scoring for relevance.

    Args:
        ctx: Context string or RLMContext object
        query: The query to find relevant sections for
        top_k: Maximum number of relevant sections to return
        model: Model tier for relevance scoring

    Returns:
        List of dicts with 'section', 'relevance', 'start_line'

    Example:
        >>> relevant = find_relevant(codebase, "authentication logic")
        >>> for r in relevant:
        ...     print(f"Lines {r['start_line']}: {r['relevance']}")
    """
    content = ctx.content if isinstance(ctx, RLMContext) else str(ctx)

    # Extract keywords from query for initial filtering
    keywords = [w.lower() for w in query.split() if len(w) > 2]

    # Use grep to find potentially relevant sections
    matches = []
    for kw in keywords[:3]:  # Limit keywords to avoid noise
        matches.extend(grep(content, kw, context_lines=3))

    if not matches:
        # Fallback: partition and score each section
        chunks = partition_by_lines(ctx, n=min(10, len(content.split('\n')) // 20 + 1))
        sections = [{"section": c, "start_line": i * (len(content.split('\n')) // len(chunks))}
                    for i, c in enumerate(chunks) if c.strip()]
    else:
        # Deduplicate and expand matches into sections
        seen_lines = set()
        sections = []
        for m in matches:
            if m['line_num'] not in seen_lines:
                seen_lines.add(m['line_num'])
                context = m.get('context_before', []) + [m['line']] + m.get('context_after', [])
                sections.append({
                    "section": '\n'.join(context),
                    "start_line": m['line_num']
                })

    # Score sections for relevance (if too many)
    if len(sections) > top_k:
        # Use LLM to score relevance
        prompts = [f"Rate 0-10 how relevant this is to: '{query}'. Reply with just the number."] * len(sections)
        scores = llm_batch(prompts, [s['section'] for s in sections], model)

        for i, score in enumerate(scores):
            try:
                sections[i]['relevance'] = int(''.join(c for c in score if c.isdigit())[:2]) / 10
            except (ValueError, IndexError):
                sections[i]['relevance'] = 0.5

        sections.sort(key=lambda x: x.get('relevance', 0), reverse=True)
        sections = sections[:top_k]
    else:
        for s in sections:
            s['relevance'] = 1.0

    return sections


# =============================================================================
# Callback Protocol
# =============================================================================
# These functions use a callback protocol to communicate with Go.
# During execution, Python sends a callback request to stdout and reads
# the response from stdin.
#
# IMPORTANT: We save the original stdout/stdin before any redirections so that
# callbacks can communicate with Go even when code execution redirects stdout.

_original_stdout = sys.stdout  # Save before any redirections
_original_stdin = sys.stdin    # Save before any redirections

_callback_id_counter = 0
_final_output = None
_callback_enabled = True  # Can be disabled for testing
_memory_enabled = True    # Can be disabled for testing


def _make_callback(callback_type: str, params: dict) -> dict:
    """
    Make a synchronous callback to Go and return the response.

    Protocol:
    1. Write callback request JSON to stdout (original, not redirected)
    2. Read callback response JSON from stdin (original)
    3. Return the response
    """
    global _callback_id_counter
    _callback_id_counter += 1

    request = {
        "callback": callback_type,
        "callback_id": _callback_id_counter,
        "params": params
    }

    # Send request to Go via the ORIGINAL stdout (pipe to Go)
    # This bypasses any stdout redirection during code execution
    _original_stdout.write(json.dumps(request) + "\n")
    _original_stdout.flush()

    # Read response from Go via the ORIGINAL stdin (pipe from Go)
    response_line = _original_stdin.readline()
    if not response_line:
        raise RuntimeError("EOF while waiting for callback response")

    response = json.loads(response_line)

    if response.get("error"):
        raise RuntimeError(f"LLM callback error: {response['error']}")

    return response


def llm_call(prompt: str, context: str = "", model: str = "auto") -> str:
    """
    Make a sub-LLM call from within the REPL.

    This is a key RLM feature - the root LLM can spawn sub-LLM calls
    to process portions of context or perform specific subtasks.

    Args:
        prompt: The prompt to send to the LLM
        context: Optional context to include
        model: Model tier - 'fast', 'balanced', 'powerful', 'reasoning', or 'auto'

    Returns:
        The LLM's response string

    Example:
        >>> summary = llm_call("Summarize this code", context=chunk)
        >>> answer = llm_call("What does this function do?", context=func_body)
    """
    if not _callback_enabled:
        # Return placeholder when callbacks are disabled (for testing)
        return f"[LLM_CALL: prompt={prompt[:50]}..., context_len={len(context)}, model={model}]"

    try:
        response = _make_callback("llm_call", {
            "prompt": prompt,
            "context": context,
            "model": model
        })
        return response.get("result", "")
    except Exception as e:
        # Fallback to placeholder if callback fails
        return f"[LLM_CALL_ERROR: {e}]"


def llm_batch(prompts: list[str], contexts: list[str] = None, model: str = "auto") -> list[str]:
    """
    Make batch LLM calls (for map operations over partitioned context).

    Args:
        prompts: List of prompts
        contexts: Optional list of contexts (same length as prompts)
        model: Model tier to use

    Returns:
        List of LLM responses

    Example:
        >>> chunks = partition(context, n=4)
        >>> summaries = llm_batch(
        ...     ["Summarize this section"] * 4,
        ...     contexts=chunks
        ... )
    """
    if contexts is None:
        contexts = [""] * len(prompts)
    if len(prompts) != len(contexts):
        raise ValueError("prompts and contexts must have same length")

    if not _callback_enabled:
        # Return placeholders when callbacks are disabled
        return [f"[LLM_BATCH: prompt={p[:30]}...]" for p in prompts]

    try:
        response = _make_callback("llm_batch", {
            "prompts": prompts,
            "contexts": contexts,
            "model": model
        })
        return response.get("results", [""] * len(prompts))
    except Exception as e:
        # Fallback to individual calls if batch fails
        return [llm_call(p, c, model) for p, c in zip(prompts, contexts)]


def disable_callbacks():
    """Disable LLM callbacks (for testing without Go runtime)."""
    global _callback_enabled
    _callback_enabled = False


def enable_callbacks():
    """Enable LLM callbacks (default)."""
    global _callback_enabled
    _callback_enabled = True


# =============================================================================
# Memory Functions
# =============================================================================
# These functions provide access to the hypergraph memory system from Python.
# Memory stores facts, experiences, and relationships that persist across sessions.


class MemoryNode:
    """Represents a node from the hypergraph memory."""

    def __init__(self, data: dict):
        self.id = data.get("id", "")
        self.type = data.get("type", "")
        self.content = data.get("content", "")
        self.confidence = data.get("confidence", 0.0)
        self.tier = data.get("tier", "")

    def __repr__(self) -> str:
        return f"MemoryNode({self.type}, conf={self.confidence:.2f}, {self.content[:50]}...)"

    def __str__(self) -> str:
        return self.content


def memory_query(query: str, limit: int = 10) -> list[MemoryNode]:
    """
    Search memory for relevant nodes.

    Args:
        query: Search query string
        limit: Maximum number of results to return

    Returns:
        List of MemoryNode objects matching the query

    Example:
        >>> nodes = memory_query("error handling", limit=5)
        >>> for node in nodes:
        ...     print(f"{node.type}: {node.content[:100]}")
    """
    if not _memory_enabled:
        return []

    try:
        response = _make_callback("memory_query", {
            "query": query,
            "limit": limit
        })
        result = response.get("result", "[]")
        nodes_data = json.loads(result) if isinstance(result, str) else result
        return [MemoryNode(n) for n in nodes_data]
    except Exception as e:
        return []


def memory_add_fact(content: str, confidence: float = 0.8) -> str:
    """
    Add a fact to memory.

    Facts are pieces of knowledge extracted from context or reasoning.
    They persist and can be queried later.

    Args:
        content: The fact content
        confidence: Confidence level (0.0 to 1.0)

    Returns:
        The ID of the created node

    Example:
        >>> node_id = memory_add_fact("Function foo() returns a string", 0.95)
        >>> memory_add_fact("This codebase uses pytest for testing", 0.9)
    """
    if not _memory_enabled:
        return ""

    try:
        response = _make_callback("memory_add_fact", {
            "content": content,
            "confidence": confidence
        })
        return response.get("result", "")
    except Exception:
        return ""


def memory_add_experience(content: str, outcome: str, success: bool = True) -> str:
    """
    Add an experience to memory.

    Experiences track what worked and what didn't, enabling learning.

    Args:
        content: Description of the experience
        outcome: What happened as a result
        success: Whether this was a successful outcome

    Returns:
        The ID of the created node

    Example:
        >>> memory_add_experience(
        ...     "Used map_reduce for large file analysis",
        ...     "Successfully processed 100KB file in 4 chunks",
        ...     success=True
        ... )
    """
    if not _memory_enabled:
        return ""

    try:
        response = _make_callback("memory_add_experience", {
            "content": content,
            "outcome": outcome,
            "success": success
        })
        return response.get("result", "")
    except Exception:
        return ""


def memory_get_context(limit: int = 10) -> list[MemoryNode]:
    """
    Get recent context nodes from memory.

    Returns the most recently accessed/relevant nodes for context injection.

    Args:
        limit: Maximum number of nodes to return

    Returns:
        List of MemoryNode objects

    Example:
        >>> context = memory_get_context(20)
        >>> context_str = "\\n".join([n.content for n in context])
        >>> result = llm_call("Based on this context...", context_str)
    """
    if not _memory_enabled:
        return []

    try:
        response = _make_callback("memory_get_context", {
            "limit": limit
        })
        result = response.get("result", "[]")
        nodes_data = json.loads(result) if isinstance(result, str) else result
        return [MemoryNode(n) for n in nodes_data]
    except Exception:
        return []


def memory_relate(label: str, subject_id: str, object_id: str) -> str:
    """
    Create a relationship between two memory nodes.

    Args:
        label: Relationship label (e.g., "implements", "uses", "related_to")
        subject_id: ID of the source node
        object_id: ID of the target node

    Returns:
        The ID of the created edge

    Example:
        >>> fact1 = memory_add_fact("Function foo exists", 0.9)
        >>> fact2 = memory_add_fact("Function bar calls foo", 0.9)
        >>> memory_relate("calls", fact2, fact1)
    """
    if not _memory_enabled:
        return ""

    try:
        response = _make_callback("memory_relate", {
            "label": label,
            "subject_id": subject_id,
            "object_id": object_id
        })
        return response.get("result", "")
    except Exception:
        return ""


def disable_memory():
    """Disable memory callbacks (for testing without Go runtime)."""
    global _memory_enabled
    _memory_enabled = False


def enable_memory():
    """Enable memory callbacks (default)."""
    global _memory_enabled
    _memory_enabled = True


# =============================================================================
# Hallucination Detection Functions
# =============================================================================
# These functions provide access to the hallucination detection system from Python.
# They use the plugin callback mechanism to route to Go's HallucinationPlugin.
# [SPEC-08.31-33]

_hallucination_enabled = True  # Can be disabled for testing


def verify_claim(claim: str, evidence: str, confidence: float = 0.9) -> dict:
    """
    Verify a single claim against provided evidence.

    Uses information-theoretic hallucination detection to check if a claim
    is grounded in the evidence. Returns detailed metrics including probability
    estimates and budget gap analysis.

    [SPEC-08.31] [SPEC-08.32]

    Args:
        claim: The claim to verify
        evidence: Evidence to verify the claim against
        confidence: Stated confidence level (0.0-1.0), defaults to 0.9

    Returns:
        dict with fields:
            - claim: The original claim text
            - status: 'grounded', 'unsupported', 'contradicted', or 'unverifiable'
            - p0: Pseudo-prior P(claim | WITHOUT evidence)
            - p1: Posterior P(claim | WITH evidence)
            - required_bits: Information needed to reach target confidence
            - observed_bits: Actual information provided by evidence
            - budget_gap: required_bits - observed_bits (negative = well supported)
            - adjusted_confidence: Evidence-adjusted confidence
            - explanation: Human-readable explanation of the result

    Example:
        >>> result = verify_claim(
        ...     "The function returns a string",
        ...     "def foo() -> str: return 'hello'"
        ... )
        >>> if result['status'] == 'grounded':
        ...     print("Claim is supported by evidence")
    """
    if not _hallucination_enabled:
        return {
            'claim': claim,
            'status': 'unverifiable',
            'p0': 0.5,
            'p1': 0.5,
            'required_bits': 0.0,
            'observed_bits': 0.0,
            'budget_gap': 0.0,
            'adjusted_confidence': confidence,
            'explanation': 'Hallucination detection disabled'
        }

    try:
        response = _make_callback("plugin_call", {
            "function": "hallucination_verify_claim",
            "args": [claim, evidence, confidence]
        })
        result = response.get("result", "{}")
        if isinstance(result, str):
            return json.loads(result)
        return result
    except Exception as e:
        return {
            'claim': claim,
            'status': 'unverifiable',
            'p0': 0.5,
            'p1': 0.5,
            'required_bits': 0.0,
            'observed_bits': 0.0,
            'budget_gap': 0.0,
            'adjusted_confidence': confidence * 0.5,
            'explanation': f'Verification error: {e}'
        }


def verify_claims(text: str, context: str) -> dict:
    """
    Extract and verify all claims in text against context.

    Parses the text to extract assertive claims, then verifies each
    claim against the provided context. Returns aggregate statistics
    and per-claim results.

    [SPEC-08.31] [SPEC-08.32]

    Args:
        text: Text containing claims to verify
        context: Context to verify claims against

    Returns:
        dict with fields:
            - total_claims: Number of claims extracted
            - verified_claims: Number of claims that are grounded
            - flagged_claims: Number of claims that are unsupported/contradicted
            - overall_risk: Ratio of flagged claims (0.0-1.0)
            - results: List of individual claim verification results

    Example:
        >>> output = "The API returns JSON. Errors use HTTP 500."
        >>> context = "API documentation: returns XML, errors use HTTP 400"
        >>> result = verify_claims(output, context)
        >>> if result['flagged_claims'] > 0:
        ...     print(f"Warning: {result['flagged_claims']} claims may be hallucinated")
    """
    if not _hallucination_enabled:
        return {
            'total_claims': 0,
            'verified_claims': 0,
            'flagged_claims': 0,
            'overall_risk': 0.0,
            'results': []
        }

    try:
        response = _make_callback("plugin_call", {
            "function": "hallucination_verify_claims",
            "args": [text, context]
        })
        result = response.get("result", "{}")
        if isinstance(result, str):
            return json.loads(result)
        return result
    except Exception as e:
        return {
            'total_claims': 0,
            'verified_claims': 0,
            'flagged_claims': 0,
            'overall_risk': 0.0,
            'results': [],
            'error': str(e)
        }


def audit_trace(steps: list[dict], context: str = "", final_answer: str = "") -> dict:
    """
    Audit a reasoning trace for procedural hallucinations.

    Verifies that each step in a reasoning trace is entailed by prior
    steps and context. Detects post-hoc hallucinations where the final
    answer isn't derivable from the reasoning.

    [SPEC-08.31] [SPEC-08.33]

    Args:
        steps: List of trace steps, each with:
            - content (str): The step content (required)
            - type (str): Step type like 'premise', 'inference' (optional)
            - confidence (float): Confidence level (optional, default 0.9)
        context: Initial context for the trace (optional)
        final_answer: Final answer to check for derivability (optional)

    Returns:
        dict with fields:
            - valid: Whether the trace is valid overall
            - total_steps: Number of steps audited
            - flagged_steps: List of indices of problematic steps
            - post_hoc_hallucination: Whether answer isn't derivable from steps
            - derivability_score: Score for answer derivability (if checked)
            - overall: Overall assessment ('valid', 'has_issues', 'invalid')
            - step_results: Per-step verification results
            - recommendations: List of suggestions for improvement

    Example:
        >>> steps = [
        ...     {'content': 'Given x = 5', 'type': 'premise'},
        ...     {'content': 'y = x + 3 = 8', 'type': 'calculation'},
        ...     {'content': 'Therefore y = 8', 'type': 'conclusion'}
        ... ]
        >>> result = audit_trace(steps, context="Find y where y = x + 3")
        >>> if not result['valid']:
        ...     print(f"Issues at steps: {result['flagged_steps']}")
    """
    if not _hallucination_enabled:
        return {
            'valid': True,
            'total_steps': len(steps),
            'flagged_steps': [],
            'post_hoc_hallucination': False,
            'overall': 'valid',
            'step_results': [],
            'recommendations': []
        }

    try:
        response = _make_callback("plugin_call", {
            "function": "hallucination_audit_trace",
            "args": [steps, context, final_answer]
        })
        result = response.get("result", "{}")
        if isinstance(result, str):
            return json.loads(result)
        return result
    except Exception as e:
        return {
            'valid': True,
            'total_steps': len(steps),
            'flagged_steps': [],
            'post_hoc_hallucination': False,
            'overall': 'unverifiable',
            'step_results': [],
            'recommendations': [],
            'error': str(e)
        }


def disable_hallucination_detection():
    """Disable hallucination detection callbacks (for testing)."""
    global _hallucination_enabled
    _hallucination_enabled = False


def enable_hallucination_detection():
    """Enable hallucination detection callbacks (default)."""
    global _hallucination_enabled
    _hallucination_enabled = True


class FinalOutput:
    """Structured final output with metadata."""

    def __init__(self, content: str, output_type: str = "text", metadata: dict = None):
        self.content = content
        self.type = output_type  # "text", "json", "code", "markdown"
        self.metadata = metadata or {}

    def __str__(self) -> str:
        return self.content

    def __repr__(self) -> str:
        return f"FinalOutput(type={self.type!r}, len={len(self.content)})"

    def to_dict(self) -> dict:
        return {
            "content": self.content,
            "type": self.type,
            "metadata": self.metadata
        }


_final_output: FinalOutput | None = None


def FINAL(response: str, output_type: str = "text") -> str:
    """
    Mark a response as the final output.

    In the RLM paradigm, the LLM writes code that eventually calls FINAL()
    with the response to return to the user. This signals that processing
    is complete.

    Args:
        response: The final response string
        output_type: Type hint for output ("text", "json", "code", "markdown")

    Returns:
        The response (also stored for retrieval)

    Example:
        >>> summary = llm_call("Summarize", context=full_context)
        >>> FINAL(f"Here's the summary:\\n{summary}")
    """
    global _final_output
    _final_output = FinalOutput(str(response), output_type)
    return response


def FINAL_VAR(variable_name: str) -> str:
    """
    Return the value of a REPL variable as the final output.

    Use this when you've built up an answer in a variable and want to
    return it without re-serializing.

    Args:
        variable_name: Name of the variable containing the answer

    Returns:
        The variable's string value

    Example:
        >>> answer = ""
        >>> for chunk in partition(context, 4):
        ...     answer += llm_call("Summarize", chunk) + "\\n"
        >>> FINAL_VAR("answer")
    """
    # Access the variable from the caller's frame
    import inspect
    frame = inspect.currentframe()
    try:
        caller_locals = frame.f_back.f_locals
        caller_globals = frame.f_back.f_globals
        if variable_name in caller_locals:
            value = caller_locals[variable_name]
        elif variable_name in caller_globals:
            value = caller_globals[variable_name]
        else:
            raise NameError(f"Variable '{variable_name}' not found")
        return FINAL(str(value))
    finally:
        del frame


def FINAL_JSON(obj, indent: int = 2) -> str:
    """
    Return a JSON-formatted final output.

    Args:
        obj: Object to serialize as JSON
        indent: Indentation level for pretty-printing

    Returns:
        JSON string

    Example:
        >>> result = {"functions": extract_functions(code), "summary": summary}
        >>> FINAL_JSON(result)
    """
    global _final_output
    content = json.dumps(obj, indent=indent, default=str)
    _final_output = FinalOutput(content, "json")
    return content


def FINAL_CODE(code: str, language: str = "python") -> str:
    """
    Return code as the final output with language annotation.

    Args:
        code: The code to return
        language: Programming language for syntax highlighting

    Returns:
        The code string

    Example:
        >>> generated = llm_call("Generate a function that...", context)
        >>> FINAL_CODE(generated, "python")
    """
    global _final_output
    _final_output = FinalOutput(code, "code", {"language": language})
    return code


def get_final_output() -> str | None:
    """Get the final output content if FINAL() was called."""
    if _final_output is None:
        return None
    return _final_output.content


def get_final_metadata() -> dict | None:
    """Get full final output including metadata."""
    if _final_output is None:
        return None
    return _final_output.to_dict()


def has_final_output() -> bool:
    """Check if FINAL() has been called."""
    return _final_output is not None


def clear_final_output():
    """Clear the final output (for new execution)."""
    global _final_output
    _final_output = None


class REPLNamespace:
    """Manages the REPL namespace with variable tracking."""

    def __init__(self):
        self._vars: dict[str, Any] = {}
        # Pre-populate with standard imports
        self._globals = {
            "re": re,
            "json": json,
            "ast": ast,
            "pathlib": pathlib,
            "itertools": itertools,
            "collections": collections,
            "Path": pathlib.Path,
            # RLM helper functions
            "RLMContext": RLMContext,
            "peek": peek,
            "grep": grep,
            "partition": partition,
            "partition_by_lines": partition_by_lines,
            "extract_functions": extract_functions,
            "count_tokens_approx": count_tokens_approx,
            "summarize": summarize,
            "map_reduce": map_reduce,
            "find_relevant": find_relevant,
            "llm_call": llm_call,
            "llm_batch": llm_batch,
            "FINAL": FINAL,
            "FINAL_VAR": FINAL_VAR,
            "FINAL_JSON": FINAL_JSON,
            "FINAL_CODE": FINAL_CODE,
            "FinalOutput": FinalOutput,
            "get_final_output": get_final_output,
            "get_final_metadata": get_final_metadata,
            "has_final_output": has_final_output,
            "clear_final_output": clear_final_output,
            "disable_callbacks": disable_callbacks,
            "enable_callbacks": enable_callbacks,
            # Memory functions
            "MemoryNode": MemoryNode,
            "memory_query": memory_query,
            "memory_add_fact": memory_add_fact,
            "memory_add_experience": memory_add_experience,
            "memory_get_context": memory_get_context,
            "memory_relate": memory_relate,
            "disable_memory": disable_memory,
            "enable_memory": enable_memory,
            # CLI tools
            "uv": uv,
            "ruff": ruff,
            "ty": ty,
            "lint": lint,
            "fmt": fmt,
            "typecheck": typecheck,
            # Hallucination detection functions [SPEC-08.31-33]
            "verify_claim": verify_claim,
            "verify_claims": verify_claims,
            "audit_trace": audit_trace,
            "disable_hallucination_detection": disable_hallucination_detection,
            "enable_hallucination_detection": enable_hallucination_detection,
        }
        if PYDANTIC_AVAILABLE:
            self._globals["pydantic"] = pydantic
        # Data science libraries
        if NUMPY_AVAILABLE:
            self._globals["np"] = np
            self._globals["numpy"] = np
        if PANDAS_AVAILABLE:
            self._globals["pd"] = pd
            self._globals["pandas"] = pd
        if POLARS_AVAILABLE:
            self._globals["pl"] = pl
            self._globals["polars"] = pl
        if SEABORN_AVAILABLE:
            self._globals["sns"] = sns
            self._globals["seaborn"] = sns

    def set_var(self, name: str, value: str) -> None:
        """Store a string value as a variable."""
        self._vars[name] = value
        self._globals[name] = value

    def get_var(self, name: str) -> Any:
        """Get a variable's value."""
        if name in self._vars:
            return self._vars[name]
        if name in self._globals:
            return self._globals[name]
        raise KeyError(f"Variable '{name}' not found")

    def list_vars(self) -> list[dict]:
        """List all user-defined variables."""
        result = []
        for name, value in self._vars.items():
            info = {
                "name": name,
                "type": type(value).__name__,
            }
            if hasattr(value, "__len__"):
                try:
                    info["length"] = len(value)
                except Exception:
                    pass
            if hasattr(value, "__sizeof__"):
                try:
                    info["size"] = value.__sizeof__()
                except Exception:
                    pass
            result.append(info)
        return result

    def get_globals(self) -> dict:
        """Get the globals dict for exec()."""
        return self._globals.copy()

    def update_from_exec(self, new_globals: dict) -> None:
        """Update namespace after exec(), tracking new variables."""
        # Find new or changed variables
        builtins = set(dir(__builtins__)) if hasattr(__builtins__, '__iter__') else set()
        stdlib = {
            "re", "json", "ast", "pathlib", "itertools", "collections", "Path", "pydantic",
            # Data science libraries
            "np", "numpy", "pd", "pandas", "pl", "polars", "sns", "seaborn",
            # CLI tools
            "uv", "ruff", "ty", "lint", "fmt", "typecheck",
            # RLM helper functions
            "RLMContext", "peek", "grep", "partition", "partition_by_lines",
            "extract_functions", "count_tokens_approx", "summarize", "map_reduce",
            "find_relevant", "llm_call", "llm_batch", "FINAL", "FINAL_VAR",
            "FINAL_JSON", "FINAL_CODE", "FinalOutput", "get_final_output",
            "get_final_metadata", "has_final_output", "clear_final_output",
            "disable_callbacks", "enable_callbacks",
            # Memory functions
            "MemoryNode", "memory_query", "memory_add_fact", "memory_add_experience",
            "memory_get_context", "memory_relate", "disable_memory", "enable_memory",
            # Hallucination detection functions [SPEC-08.31-33]
            "verify_claim", "verify_claims", "audit_trace",
            "disable_hallucination_detection", "enable_hallucination_detection",
        }

        for name, value in new_globals.items():
            if name.startswith("_"):
                continue
            if name in builtins or name in stdlib:
                continue
            if name not in self._globals or self._globals[name] is not value:
                self._vars[name] = value
                self._globals[name] = value


class REPL:
    """JSON-RPC style Python REPL."""

    def __init__(self):
        self.namespace = REPLNamespace()
        self.exec_count = 0
        self.start_time = time.time()

    def handle_request(self, request: dict) -> dict:
        """Process a request and return a response."""
        req_id = request.get("id", 0)
        method = request.get("method", "")
        params = request.get("params", {})

        try:
            if method == "execute":
                result = self.execute(params.get("code", ""))
            elif method == "set_var":
                result = self.set_var(params.get("name"), params.get("value"))
            elif method == "get_var":
                result = self.get_var(
                    params.get("name"),
                    params.get("start", 0),
                    params.get("end", 0),
                    params.get("as_repr", False)
                )
            elif method == "list_vars":
                result = self.list_vars()
            elif method == "status":
                result = self.status()
            elif method == "shutdown":
                return {"id": req_id, "result": {"ok": True}}
            else:
                return {
                    "id": req_id,
                    "error": {"code": -32601, "message": f"Method not found: {method}"}
                }

            return {"id": req_id, "result": result}

        except Exception as e:
            return {
                "id": req_id,
                "error": {
                    "code": -32603,
                    "message": str(e),
                    "data": traceback.format_exc()
                }
            }

    def execute(self, code: str) -> dict:
        """Execute Python code and return the result."""
        self.exec_count += 1
        start = time.time()

        stdout_capture = io.StringIO()
        stderr_capture = io.StringIO()
        return_value = ""
        error = ""

        try:
            # Parse to check if it's an expression or statements
            try:
                tree = ast.parse(code, mode='eval')
                is_expr = True
            except SyntaxError:
                tree = ast.parse(code, mode='exec')
                is_expr = False

            globals_dict = self.namespace.get_globals()

            with redirect_stdout(stdout_capture), redirect_stderr(stderr_capture):
                if is_expr:
                    # Single expression - capture return value
                    result = eval(compile(tree, '<repl>', 'eval'), globals_dict)
                    if result is not None:
                        return_value = repr(result)
                    self.namespace.update_from_exec(globals_dict)
                else:
                    # Statements - execute and check for last expression
                    exec(compile(tree, '<repl>', 'exec'), globals_dict)
                    self.namespace.update_from_exec(globals_dict)

                    # Try to get value of last expression if it exists
                    if tree.body:
                        last = tree.body[-1]
                        if isinstance(last, ast.Expr):
                            try:
                                last_expr = ast.Expression(body=last.value)
                                result = eval(compile(last_expr, '<repl>', 'eval'), globals_dict)
                                if result is not None:
                                    return_value = repr(result)
                            except Exception:
                                pass

        except Exception as e:
            error = f"{type(e).__name__}: {e}\n{traceback.format_exc()}"

        duration_ms = int((time.time() - start) * 1000)

        return {
            "output": stdout_capture.getvalue() + stderr_capture.getvalue(),
            "return_value": return_value,
            "error": error,
            "duration_ms": duration_ms
        }

    def set_var(self, name: str, value: str) -> dict:
        """Store a string value as a named variable."""
        if not name or not name.isidentifier():
            raise ValueError(f"Invalid variable name: {name}")
        self.namespace.set_var(name, value)
        return {"ok": True}

    def get_var(self, name: str, start: int = 0, end: int = 0, as_repr: bool = False) -> dict:
        """Get a variable's value, optionally sliced."""
        value = self.namespace.get_var(name)
        total_len = len(value) if hasattr(value, "__len__") else 0

        # Apply slicing if specified
        if start or end:
            if hasattr(value, "__getitem__"):
                if end:
                    value = value[start:end]
                else:
                    value = value[start:]

        # Convert to string
        if as_repr:
            str_value = repr(value)
        else:
            str_value = str(value)

        return {
            "value": str_value,
            "length": total_len,
            "type": type(self.namespace.get_var(name)).__name__
        }

    def list_vars(self) -> dict:
        """List all user-defined variables."""
        return {"variables": self.namespace.list_vars()}

    def status(self) -> dict:
        """Return REPL status with resource usage."""
        rusage = resource.getrusage(resource.RUSAGE_SELF)

        # Memory: ru_maxrss is in bytes on macOS, KB on Linux
        if sys.platform == "darwin":
            mem_mb = rusage.ru_maxrss / (1024 * 1024)
        else:
            mem_mb = rusage.ru_maxrss / 1024

        # CPU time in milliseconds
        user_cpu_ms = int(rusage.ru_utime * 1000)
        sys_cpu_ms = int(rusage.ru_stime * 1000)

        return {
            "running": True,
            "memory_used_mb": round(mem_mb, 2),
            "uptime_seconds": int(time.time() - self.start_time),
            "exec_count": self.exec_count,
            "user_cpu_ms": user_cpu_ms,
            "sys_cpu_ms": sys_cpu_ms,
            "total_cpu_ms": user_cpu_ms + sys_cpu_ms,
        }


def main():
    """Main REPL loop."""
    repl = REPL()

    # Send ready signal
    ready_response = {"id": 0, "result": {"ready": True, "pydantic": PYDANTIC_AVAILABLE}}
    sys.stdout.write(json.dumps(ready_response) + "\n")
    sys.stdout.flush()

    # Process requests
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        try:
            request = json.loads(line)
        except json.JSONDecodeError as e:
            response = {"id": 0, "error": {"code": -32700, "message": f"Parse error: {e}"}}
            sys.stdout.write(json.dumps(response) + "\n")
            sys.stdout.flush()
            continue

        response = repl.handle_request(request)
        sys.stdout.write(json.dumps(response) + "\n")
        sys.stdout.flush()

        # Check for shutdown
        if request.get("method") == "shutdown":
            break


if __name__ == "__main__":
    main()
