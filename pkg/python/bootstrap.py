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
import pathlib
import re
import sys
import time
import traceback
from contextlib import redirect_stderr, redirect_stdout
from typing import Any

# Try to import pydantic if available
try:
    import pydantic
    PYDANTIC_AVAILABLE = True
except ImportError:
    PYDANTIC_AVAILABLE = False


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
        >>> matches = grep(context, r"def \w+")
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
# LLM Callback Protocol
# =============================================================================
# These functions use a callback protocol to make LLM calls back to Go.
# During execution, Python sends a callback request to stdout and reads
# the response from stdin.

_callback_id_counter = 0
_final_output = None
_callback_enabled = True  # Can be disabled for testing


def _make_callback(callback_type: str, params: dict) -> dict:
    """
    Make a synchronous callback to Go and return the response.

    Protocol:
    1. Write callback request JSON to stdout
    2. Read callback response JSON from stdin
    3. Return the response
    """
    global _callback_id_counter
    _callback_id_counter += 1

    request = {
        "callback": callback_type,
        "callback_id": _callback_id_counter,
        "params": params
    }

    # Send request to Go via stdout
    sys.stdout.write(json.dumps(request) + "\n")
    sys.stdout.flush()

    # Read response from Go via stdin
    response_line = sys.stdin.readline()
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


def FINAL(response: str) -> str:
    """
    Mark a response as the final output.

    In the RLM paradigm, the LLM writes code that eventually calls FINAL()
    with the response to return to the user. This signals that processing
    is complete.

    Args:
        response: The final response string

    Returns:
        The response (also stored for retrieval)

    Example:
        >>> summary = llm_call("Summarize", context=full_context)
        >>> FINAL(f"Here's the summary:\\n{summary}")
    """
    global _final_output
    _final_output = response
    return response


def get_final_output():
    """Get the final output if FINAL() was called."""
    return _final_output


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
            "get_final_output": get_final_output,
            "clear_final_output": clear_final_output,
            "disable_callbacks": disable_callbacks,
            "enable_callbacks": enable_callbacks,
        }
        if PYDANTIC_AVAILABLE:
            self._globals["pydantic"] = pydantic

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
            # RLM helper functions
            "RLMContext", "peek", "grep", "partition", "partition_by_lines",
            "extract_functions", "count_tokens_approx", "summarize", "map_reduce",
            "find_relevant", "llm_call", "llm_batch", "FINAL", "get_final_output",
            "clear_final_output", "disable_callbacks", "enable_callbacks",
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
        """Return REPL status."""
        import resource
        mem_usage = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
        # On macOS, ru_maxrss is in bytes; on Linux it's in KB
        if sys.platform == "darwin":
            mem_mb = mem_usage / (1024 * 1024)
        else:
            mem_mb = mem_usage / 1024

        return {
            "running": True,
            "memory_used_mb": round(mem_mb, 2),
            "uptime_seconds": int(time.time() - self.start_time),
            "exec_count": self.exec_count
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
