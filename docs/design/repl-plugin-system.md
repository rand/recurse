# REPL Plugin System Design

> Design document for `recurse-t8p`: [SPEC] REPL Plugin System Design

## Overview

This document specifies a plugin system for the Python REPL, enabling extensible tool capabilities without modifying core REPL code. Plugins can add new tools, data types, and integrations while maintaining sandbox safety.

## Problem Statement

### Current State

Hardcoded tool handlers in REPL:

```python
def handle_command(cmd):
    if cmd.type == "execute":
        return execute_code(cmd.code)
    elif cmd.type == "llm_call":
        return make_llm_call(cmd.params)
    # Adding new tools requires modifying core code
```

**Issues**:
- New capabilities require core changes
- No isolation between tools
- Difficult to add domain-specific tools
- No plugin lifecycle management

## Design Goals

1. **Extensibility**: Add tools without core changes
2. **Isolation**: Plugins cannot break each other
3. **Discovery**: Auto-discover available plugins
4. **Lifecycle**: Proper init/cleanup hooks
5. **Safety**: Plugins run in sandbox

## Core Types

### Plugin Interface

```python
# internal/repl/plugins/base.py

from abc import ABC, abstractmethod
from typing import Any, Dict, List, Optional
from dataclasses import dataclass

@dataclass
class PluginMetadata:
    name: str
    version: str
    description: str
    author: str
    dependencies: List[str] = None
    capabilities: List[str] = None

class Plugin(ABC):
    """Base class for REPL plugins."""

    @property
    @abstractmethod
    def metadata(self) -> PluginMetadata:
        """Return plugin metadata."""
        pass

    @abstractmethod
    def initialize(self, context: 'PluginContext') -> None:
        """Called when plugin is loaded."""
        pass

    @abstractmethod
    def shutdown(self) -> None:
        """Called when plugin is unloaded."""
        pass

    def get_tools(self) -> List['Tool']:
        """Return tools provided by this plugin."""
        return []

    def get_types(self) -> List['CustomType']:
        """Return custom types provided by this plugin."""
        return []

    def on_repl_start(self) -> None:
        """Called when REPL session starts."""
        pass

    def on_repl_end(self) -> None:
        """Called when REPL session ends."""
        pass
```

### Tool Definition

```python
# internal/repl/plugins/tool.py

from dataclasses import dataclass
from typing import Any, Callable, Dict, List, Optional

@dataclass
class ToolParameter:
    name: str
    type: str
    description: str
    required: bool = True
    default: Any = None

@dataclass
class Tool:
    name: str
    description: str
    parameters: List[ToolParameter]
    handler: Callable[..., Any]
    category: str = "general"
    requires_sandbox: bool = True

    def execute(self, **kwargs) -> Any:
        """Execute the tool with given parameters."""
        # Validate parameters
        for param in self.parameters:
            if param.required and param.name not in kwargs:
                raise ValueError(f"Missing required parameter: {param.name}")
            if param.name not in kwargs and param.default is not None:
                kwargs[param.name] = param.default

        return self.handler(**kwargs)

    def to_schema(self) -> Dict[str, Any]:
        """Return JSON schema for tool parameters."""
        return {
            "name": self.name,
            "description": self.description,
            "parameters": {
                "type": "object",
                "properties": {
                    p.name: {"type": p.type, "description": p.description}
                    for p in self.parameters
                },
                "required": [p.name for p in self.parameters if p.required]
            }
        }
```

### Plugin Context

```python
# internal/repl/plugins/context.py

from typing import Any, Dict, Optional
import logging

class PluginContext:
    """Context provided to plugins during initialization."""

    def __init__(
        self,
        repl: 'REPLManager',
        config: Dict[str, Any],
        logger: logging.Logger
    ):
        self.repl = repl
        self.config = config
        self.logger = logger
        self._storage: Dict[str, Any] = {}

    def get_config(self, key: str, default: Any = None) -> Any:
        """Get plugin configuration value."""
        return self.config.get(key, default)

    def get_tool(self, name: str) -> Optional[Tool]:
        """Get a tool by name (from any plugin)."""
        return self.repl.get_tool(name)

    def store(self, key: str, value: Any) -> None:
        """Store data in plugin-local storage."""
        self._storage[key] = value

    def retrieve(self, key: str, default: Any = None) -> Any:
        """Retrieve data from plugin-local storage."""
        return self._storage.get(key, default)

    def emit_event(self, event: str, data: Any = None) -> None:
        """Emit an event to other plugins."""
        self.repl.emit_event(event, data)
```

## Plugin Manager

### Manager Implementation

```python
# internal/repl/plugins/manager.py

import importlib
import pkgutil
from pathlib import Path
from typing import Dict, List, Optional, Type
import logging

class PluginManager:
    """Manages plugin lifecycle and registration."""

    def __init__(self, repl: 'REPLManager', plugin_dirs: List[Path] = None):
        self.repl = repl
        self.plugin_dirs = plugin_dirs or []
        self.plugins: Dict[str, Plugin] = {}
        self.tools: Dict[str, Tool] = {}
        self.logger = logging.getLogger("plugins")
        self._event_handlers: Dict[str, List[callable]] = {}

    def discover_plugins(self) -> List[Type[Plugin]]:
        """Discover available plugins from plugin directories."""
        discovered = []

        for plugin_dir in self.plugin_dirs:
            if not plugin_dir.exists():
                continue

            for finder, name, ispkg in pkgutil.iter_modules([str(plugin_dir)]):
                if not ispkg:
                    continue

                try:
                    module = importlib.import_module(f"{plugin_dir.name}.{name}")
                    if hasattr(module, 'Plugin'):
                        discovered.append(module.Plugin)
                except Exception as e:
                    self.logger.error(f"Failed to load plugin {name}: {e}")

        return discovered

    def load_plugin(self, plugin_class: Type[Plugin], config: Dict = None) -> None:
        """Load and initialize a plugin."""
        plugin = plugin_class()
        metadata = plugin.metadata

        # Check dependencies
        for dep in metadata.dependencies or []:
            if dep not in self.plugins:
                raise PluginDependencyError(f"Missing dependency: {dep}")

        # Create context
        context = PluginContext(
            repl=self.repl,
            config=config or {},
            logger=logging.getLogger(f"plugins.{metadata.name}")
        )

        # Initialize
        try:
            plugin.initialize(context)
        except Exception as e:
            raise PluginInitError(f"Plugin {metadata.name} init failed: {e}")

        # Register tools
        for tool in plugin.get_tools():
            if tool.name in self.tools:
                raise PluginToolConflict(f"Tool {tool.name} already registered")
            self.tools[tool.name] = tool

        self.plugins[metadata.name] = plugin
        self.logger.info(f"Loaded plugin: {metadata.name} v{metadata.version}")

    def unload_plugin(self, name: str) -> None:
        """Unload a plugin."""
        if name not in self.plugins:
            return

        plugin = self.plugins[name]

        # Remove tools
        for tool in plugin.get_tools():
            del self.tools[tool.name]

        # Shutdown
        try:
            plugin.shutdown()
        except Exception as e:
            self.logger.error(f"Plugin {name} shutdown error: {e}")

        del self.plugins[name]
        self.logger.info(f"Unloaded plugin: {name}")

    def get_tool(self, name: str) -> Optional[Tool]:
        """Get a tool by name."""
        return self.tools.get(name)

    def list_tools(self) -> List[Tool]:
        """List all available tools."""
        return list(self.tools.values())

    def execute_tool(self, name: str, **kwargs) -> Any:
        """Execute a tool by name."""
        tool = self.get_tool(name)
        if not tool:
            raise ToolNotFoundError(f"Unknown tool: {name}")

        return tool.execute(**kwargs)

    def emit_event(self, event: str, data: Any = None) -> None:
        """Emit an event to all registered handlers."""
        for handler in self._event_handlers.get(event, []):
            try:
                handler(data)
            except Exception as e:
                self.logger.error(f"Event handler error: {e}")

    def on_event(self, event: str, handler: callable) -> None:
        """Register an event handler."""
        if event not in self._event_handlers:
            self._event_handlers[event] = []
        self._event_handlers[event].append(handler)
```

## Example Plugins

### File Operations Plugin

```python
# plugins/file_ops/plugin.py

from pathlib import Path
from repl.plugins import Plugin, PluginMetadata, Tool, ToolParameter

class FileOpsPlugin(Plugin):
    @property
    def metadata(self) -> PluginMetadata:
        return PluginMetadata(
            name="file_ops",
            version="1.0.0",
            description="File operation tools",
            author="recurse",
            capabilities=["file_read", "file_write", "file_search"]
        )

    def initialize(self, context):
        self.context = context
        self.sandbox_root = Path(context.get_config("sandbox_root", "/tmp/sandbox"))

    def shutdown(self):
        pass

    def get_tools(self) -> list:
        return [
            Tool(
                name="read_file",
                description="Read contents of a file",
                parameters=[
                    ToolParameter("path", "string", "Path to file"),
                    ToolParameter("encoding", "string", "File encoding", required=False, default="utf-8")
                ],
                handler=self._read_file,
                category="file"
            ),
            Tool(
                name="write_file",
                description="Write contents to a file",
                parameters=[
                    ToolParameter("path", "string", "Path to file"),
                    ToolParameter("content", "string", "Content to write"),
                ],
                handler=self._write_file,
                category="file"
            ),
            Tool(
                name="list_files",
                description="List files in a directory",
                parameters=[
                    ToolParameter("path", "string", "Directory path"),
                    ToolParameter("pattern", "string", "Glob pattern", required=False, default="*")
                ],
                handler=self._list_files,
                category="file"
            ),
        ]

    def _read_file(self, path: str, encoding: str = "utf-8") -> str:
        full_path = self._resolve_path(path)
        return full_path.read_text(encoding=encoding)

    def _write_file(self, path: str, content: str) -> dict:
        full_path = self._resolve_path(path)
        full_path.parent.mkdir(parents=True, exist_ok=True)
        full_path.write_text(content)
        return {"written": len(content), "path": str(full_path)}

    def _list_files(self, path: str, pattern: str = "*") -> list:
        full_path = self._resolve_path(path)
        return [str(p.relative_to(self.sandbox_root)) for p in full_path.glob(pattern)]

    def _resolve_path(self, path: str) -> Path:
        resolved = (self.sandbox_root / path).resolve()
        if not str(resolved).startswith(str(self.sandbox_root)):
            raise SecurityError("Path escapes sandbox")
        return resolved
```

### Data Analysis Plugin

```python
# plugins/data_analysis/plugin.py

import pandas as pd
import numpy as np
from repl.plugins import Plugin, PluginMetadata, Tool, ToolParameter

class DataAnalysisPlugin(Plugin):
    @property
    def metadata(self) -> PluginMetadata:
        return PluginMetadata(
            name="data_analysis",
            version="1.0.0",
            description="Data analysis and statistics tools",
            author="recurse",
            dependencies=[],
            capabilities=["dataframe", "statistics", "visualization"]
        )

    def initialize(self, context):
        self.context = context
        self.dataframes: dict = {}

    def shutdown(self):
        self.dataframes.clear()

    def get_tools(self) -> list:
        return [
            Tool(
                name="load_csv",
                description="Load CSV file into a dataframe",
                parameters=[
                    ToolParameter("path", "string", "Path to CSV file"),
                    ToolParameter("name", "string", "Name for the dataframe"),
                ],
                handler=self._load_csv,
                category="data"
            ),
            Tool(
                name="describe",
                description="Get statistical summary of a dataframe",
                parameters=[
                    ToolParameter("name", "string", "Dataframe name"),
                ],
                handler=self._describe,
                category="data"
            ),
            Tool(
                name="query",
                description="Query a dataframe with pandas expression",
                parameters=[
                    ToolParameter("name", "string", "Dataframe name"),
                    ToolParameter("expression", "string", "Pandas query expression"),
                ],
                handler=self._query,
                category="data"
            ),
        ]

    def _load_csv(self, path: str, name: str) -> dict:
        df = pd.read_csv(path)
        self.dataframes[name] = df
        return {
            "name": name,
            "shape": df.shape,
            "columns": list(df.columns)
        }

    def _describe(self, name: str) -> dict:
        df = self.dataframes.get(name)
        if df is None:
            raise ValueError(f"Unknown dataframe: {name}")
        return df.describe().to_dict()

    def _query(self, name: str, expression: str) -> list:
        df = self.dataframes.get(name)
        if df is None:
            raise ValueError(f"Unknown dataframe: {name}")
        result = df.query(expression)
        return result.to_dict(orient="records")
```

## Security

### Sandbox Integration

```python
# internal/repl/plugins/sandbox.py

from typing import Set

class PluginSandbox:
    """Enforces security constraints on plugins."""

    ALLOWED_MODULES = {
        'json', 'math', 'statistics', 'collections',
        'datetime', 'itertools', 'functools', 're'
    }

    FORBIDDEN_ATTRS = {
        '__import__', 'eval', 'exec', 'compile',
        'open', 'input', '__builtins__'
    }

    def __init__(self, allowed_paths: Set[str] = None):
        self.allowed_paths = allowed_paths or set()

    def validate_tool(self, tool: Tool) -> bool:
        """Validate a tool is safe to execute."""
        # Check handler source for forbidden patterns
        import inspect
        source = inspect.getsource(tool.handler)

        for forbidden in self.FORBIDDEN_ATTRS:
            if forbidden in source:
                return False

        return True

    def wrap_handler(self, handler: callable) -> callable:
        """Wrap handler with security checks."""
        def wrapped(**kwargs):
            # Validate inputs
            for key, value in kwargs.items():
                if isinstance(value, str) and '..' in value:
                    raise SecurityError("Path traversal detected")

            return handler(**kwargs)

        return wrapped
```

## Configuration

### Plugin Configuration

```yaml
# config/plugins.yaml

plugins:
  file_ops:
    enabled: true
    config:
      sandbox_root: /tmp/repl_sandbox
      max_file_size: 10485760  # 10MB

  data_analysis:
    enabled: true
    config:
      max_dataframe_size: 1000000

  custom_plugin:
    enabled: false
    path: /path/to/custom/plugin

plugin_dirs:
  - ./plugins
  - ~/.recurse/plugins
```

## Integration with REPL

### REPL Manager Integration

```python
# internal/repl/manager.py

class REPLManager:
    def __init__(self, config: dict):
        self.plugin_manager = PluginManager(
            repl=self,
            plugin_dirs=[Path(p) for p in config.get('plugin_dirs', [])]
        )
        self._load_plugins(config.get('plugins', {}))

    def _load_plugins(self, plugin_config: dict):
        """Load configured plugins."""
        for plugin_class in self.plugin_manager.discover_plugins():
            metadata = plugin_class().metadata
            if metadata.name in plugin_config:
                cfg = plugin_config[metadata.name]
                if cfg.get('enabled', True):
                    self.plugin_manager.load_plugin(plugin_class, cfg.get('config', {}))

    def handle_tool_call(self, name: str, params: dict) -> Any:
        """Handle a tool call from RLM."""
        return self.plugin_manager.execute_tool(name, **params)

    def get_available_tools(self) -> list:
        """Return schemas for all available tools."""
        return [tool.to_schema() for tool in self.plugin_manager.list_tools()]
```

## Success Criteria

1. **Extensibility**: New tools added without core changes
2. **Isolation**: Plugin failures don't crash REPL
3. **Discovery**: Plugins auto-discovered from configured paths
4. **Security**: Sandbox prevents malicious plugins
5. **Performance**: Plugin overhead <5ms per tool call
