# Configuration Reference

This document describes the configuration options for Recurse, including config files, environment variables, and CLI flags.

## Configuration Files

Recurse loads configuration from multiple sources in order of precedence (highest first):

1. **CLI flags** - Command-line arguments override all other settings
2. **Project config** - `.recurse.yaml` or `.recurse.yml` in the current directory
3. **User config** - `~/.recurse/config.yaml` or `$XDG_CONFIG_HOME/recurse/config.yaml`

### File Locations

Check active config paths with:
```bash
recurse config path
```

### Example Configuration

```yaml
# Provider configuration
providers:
  anthropic:
    api_key: ${ANTHROPIC_API_KEY}
    type: anthropic
  openai:
    api_key: ${OPENAI_API_KEY}
    type: openai

# Model selection
models:
  large:
    model: claude-sonnet-4-20250514
    provider: anthropic
  small:
    model: claude-haiku-4-20250514
    provider: anthropic

# General options
options:
  data_directory: .recurse
  debug: false
  disable_metrics: false
  context_paths:
    - CLAUDE.md
    - .cursorrules

# Permission settings
permissions:
  allowed_tools:
    - bash
    - view

# TUI settings
options:
  tui:
    compact_mode: false
    diff_mode: unified
    keybindings:
      commands: "ctrl+k"        # Change from ctrl+p to avoid conflicts
    history:
      enabled: true
      max_items: 1000
      persistent: true          # Persist across sessions via memory
```

## Environment Variables

### Core Settings

| Variable | Description | Default |
|----------|-------------|---------|
| `RECURSE_GLOBAL_CONFIG` | Override global config directory | Platform-specific |
| `RECURSE_GLOBAL_DATA` | Override global data directory | Platform-specific |
| `RECURSE_SKILLS_DIR` | Custom skills directory | Platform-specific |
| `RECURSE_DISABLE_METRICS` | Disable telemetry (`true`/`false`) | `false` |
| `DO_NOT_TRACK` | Disable telemetry (industry standard) | `false` |
| `RECURSE_PPROF` | Enable profiling server | unset |

### Provider API Keys

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic API key for Claude models |
| `OPENAI_API_KEY` | OpenAI API key |
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `VOYAGE_API_KEY` | Voyage AI key for embeddings |

### Embedding Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `EMBEDDING_PROVIDER` | Provider type (`voyage`, `local`) | Auto-detect |
| `EMBEDDING_SERVER_URL` | URL for local embedding server | unset |
| `EMBEDDING_MODEL` | Model name for local embeddings | unset |

### Resource Limits

| Variable | Description | Default |
|----------|-------------|---------|
| `RECURSE_MEMORY_LIMIT_MB` | Memory limit for REPL in MB | 512 |
| `RECURSE_CPU_LIMIT_SEC` | CPU time limit for REPL in seconds | 30 |

### Development/Debug

| Variable | Description |
|----------|-------------|
| `CRUSH_DISABLE_ANTHROPIC_CACHE` | Disable prompt caching |
| `CRUSH_CORE_UTILS` | Enable core utils mode |
| `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE` | Disable provider auto-update |
| `CATWALK_URL` | Custom catwalk provider URL |

## CLI Flags

### Global Flags (all commands)

| Flag | Short | Description |
|------|-------|-------------|
| `--cwd <path>` | `-c` | Set working directory |
| `--data-dir <path>` | `-D` | Custom data directory |
| `--debug` | `-d` | Enable debug logging |
| `--help` | `-h` | Show help |
| `--version` | `-v` | Show version |

### Main Command Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--yolo` | `-y` | Auto-accept all permissions (dangerous) |
| `--no-tui` | | Run in non-interactive mode |
| `--budget <n>` | | Token budget limit |
| `--model <id>` | `-m` | Primary model to use |

## Commands

### `recurse config show`

Display effective configuration after merging all sources.

```bash
recurse config show         # Human-readable
recurse config show --json  # JSON format
recurse config show --yaml  # YAML format
```

### `recurse config edit`

Open config in `$EDITOR` (falls back to `$VISUAL`, then `vi`).

```bash
recurse config edit
```

### `recurse config validate`

Check configuration for errors and warnings.

```bash
recurse config validate
```

### `recurse config path`

Show all configuration file paths and which exist.

```bash
recurse config path
```

### `recurse memory stats`

Show memory system statistics.

```bash
recurse memory stats
```

### `recurse memory search`

Search the hypergraph memory.

```bash
recurse memory search "authentication"
recurse memory search -n 5 -j "user login"      # Limit 5, JSON output
recurse memory search -t longterm "schema"       # Filter by tier
recurse memory search -T fact "api endpoints"    # Filter by node type
```

### `recurse memory export`

Export memory to JSON.

```bash
recurse memory export              # To stdout
recurse memory export -o mem.json  # To file
```

### `recurse memory gc`

Run garbage collection on memory.

```bash
recurse memory gc --dry-run  # Preview changes
recurse memory gc --prune    # Actually prune low-value nodes
```

## Configuration Schema

### Provider Configuration

```yaml
providers:
  <provider-id>:
    id: string           # Provider identifier
    name: string         # Display name
    base_url: string     # API endpoint URL
    type: string         # openai | openai-compat | anthropic | gemini | azure | vertexai
    api_key: string      # API key (supports ${ENV_VAR} syntax)
    disable: boolean     # Disable this provider
    system_prompt_prefix: string  # Custom system prompt prefix
    extra_headers: map   # Additional HTTP headers
    extra_body: map      # Additional request body fields
    models: array        # Available models
```

### Model Selection

```yaml
models:
  large:                 # Primary model for complex tasks
    model: string        # Model ID
    provider: string     # Provider ID
    max_tokens: integer  # Override max tokens
    temperature: float   # Sampling temperature (0-1)
    think: boolean       # Enable thinking for Anthropic models
  small:                 # Fast model for simple tasks
    model: string
    provider: string
```

### MCP (Model Context Protocol) Configuration

```yaml
mcp:
  <server-name>:
    type: string         # stdio | sse | http
    command: string      # Command for stdio servers
    args: array          # Command arguments
    url: string          # URL for HTTP/SSE servers
    env: map             # Environment variables
    headers: map         # HTTP headers
    timeout: integer     # Timeout in seconds (default: 15)
    disabled: boolean    # Disable this server
    disabled_tools: array  # Tools to disable from this server
```

### LSP (Language Server Protocol) Configuration

```yaml
lsp:
  <server-name>:
    command: string      # LSP server command
    args: array          # Command arguments
    env: map             # Environment variables
    filetypes: array     # File types this server handles
    root_markers: array  # Project root markers
    init_options: map    # LSP initialization options
    options: map         # Server-specific settings
    disabled: boolean    # Disable this server
```

### Options

```yaml
options:
  data_directory: string        # Data storage location (default: .recurse)
  debug: boolean                # Enable debug logging
  debug_lsp: boolean            # Enable LSP debug logging
  disable_metrics: boolean      # Disable telemetry
  disable_auto_summarize: boolean  # Disable conversation summarization
  context_paths: array          # Files to load as context
  skills_paths: array           # Agent skills directories
  disabled_tools: array         # Built-in tools to disable
  initialize_as: string         # Context file name (default: AGENTS.md)
  tui:
    compact_mode: boolean       # Enable compact TUI mode
    diff_mode: string           # unified | split
    completions:
      max_depth: integer        # Max depth for ls tool
      max_items: integer        # Max items for ls tool
    keybindings:                # Customize keyboard shortcuts
      quit: string              # Quit (default: ctrl+c)
      help: string              # Show help (default: ctrl+g)
      commands: string          # Command palette (default: ctrl+p)
      suspend: string           # Suspend to shell (default: ctrl+z)
      models: string            # Model selector (default: ctrl+l)
      sessions: string          # Sessions dialog (default: ctrl+s)
      rlm_trace: string         # RLM trace viewer (default: ctrl+t)
      memory: string            # Memory viewer (default: ctrl+b)
      repl_output: string       # REPL output (default: ctrl+r)
      panel_view: string        # Panel view (default: ctrl+e)
      add_file: string          # File picker (default: /)
      send_message: string      # Send message (default: enter)
      open_editor: string       # External editor (default: ctrl+o)
      newline: string           # Insert newline (default: ctrl+j)
      prev_history: string      # Previous input (default: up)
      next_history: string      # Next input (default: down)
    history:                    # Input history settings
      enabled: boolean          # Enable history (default: true)
      max_items: integer        # Max items to keep (default: 1000)
      persistent: boolean       # Persist across sessions (default: true)
  attribution:
    trailer_style: string       # none | co-authored-by | assisted-by
    generated_with: boolean     # Add "Generated with Recurse" line
```

### Permissions

```yaml
permissions:
  allowed_tools: array   # Tools that don't require permission prompts
```

## Platform-Specific Paths

### macOS / Linux

- Global config: `~/.config/recurse/config.yaml` or `$XDG_CONFIG_HOME/recurse/config.yaml`
- Global data: `~/.local/share/recurse/` or `$XDG_DATA_HOME/recurse/`
- Skills: `~/.config/recurse/skills/` or `$XDG_CONFIG_HOME/recurse/skills/`

### Windows

- Global config: `%LOCALAPPDATA%\recurse\config.yaml`
- Global data: `%LOCALAPPDATA%\recurse\`
- Skills: `%LOCALAPPDATA%\recurse\skills\`

## Variable Resolution

Configuration values support environment variable expansion:

```yaml
providers:
  anthropic:
    api_key: ${ANTHROPIC_API_KEY}     # Expands from environment
    base_url: ${API_BASE:-default}    # With default value
```

The resolver supports:
- `${VAR}` - Simple expansion
- `${VAR:-default}` - Default if unset
- `${VAR:+alternate}` - Alternate if set
