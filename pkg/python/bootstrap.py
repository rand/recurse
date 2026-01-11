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
        stdlib = {"re", "json", "ast", "pathlib", "itertools", "collections", "Path", "pydantic"}

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
