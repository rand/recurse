# Recurse

> A recursive language model (RLM) system with hypergraph memory for agentic coding.

Recurse is a personal agentic coding environment that extends [Crush](https://github.com/charmbracelet/crush) with:

- **RLM Orchestration**: Treat prompts as manipulable objects in a Python REPL
- **Tiered Hypergraph Memory**: Knowledge that persists and evolves across sessions
- **Embedded Reasoning Traces**: Decision graphs linked to git commits

## Quick Start

### Prerequisites

- **Go 1.21+**: `brew install go` or [golang.org](https://golang.org/dl/)
- **Python 3.11+**: `brew install python@3.11` or [python.org](https://python.org)
- **uv** (Python package manager): `curl -LsSf https://astral.sh/uv/install.sh | sh`
- **OpenRouter API Key**: Get one at [openrouter.ai](https://openrouter.ai)

### Installation

```bash
# Clone the repository
git clone https://github.com/rand/recurse.git
cd recurse

# Build the binary
go build -o recurse ./cmd/recurse

# (Optional) Install globally
go install ./cmd/recurse

# Set up Python environment for RLM mode
cd pkg/python
uv venv
source .venv/bin/activate
uv pip install pydantic ruff
cd ../..

# Set your API key
export OPENROUTER_API_KEY="your-key-here"
# Add to ~/.zshrc or ~/.bashrc to persist
```

### Validate Installation

```bash
# 1. Verify the binary works
./recurse --help

# 2. Run unit tests
go test ./...

# 3. (Optional) Run a quick benchmark to validate RLM works
# This makes real API calls - costs ~$0.05
RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLMMode ./internal/rlm/benchmark/
```

Expected output for the benchmark:
```
=== RUN   TestRealBenchmark_RLMMode
    Task: Needle in Haystack
    Expected Answer: CODE-XXXX
    RLM Mode: true
    Score: 1.00, Correct: true
--- PASS: TestRealBenchmark_RLMMode
```

## Updating Recurse

```bash
# Pull latest changes
git pull origin main

# Rebuild
go build -o recurse ./cmd/recurse

# Or if installed globally
go install ./cmd/recurse

# Update Python dependencies (if any changed)
cd pkg/python && uv pip install -r requirements.txt && cd ../..

# Run tests to verify
go test ./...
```

## Configuration

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `OPENROUTER_API_KEY` | Yes | Your OpenRouter API key for LLM access |
| `RECURSE_MODEL` | No | Default model (default: `anthropic/claude-3.5-sonnet`) |
| `RECURSE_DEBUG` | No | Enable debug logging (`1` to enable) |

### Config File

Create `~/.config/recurse/config.yaml`:

```yaml
# Model preferences
default_model: anthropic/claude-3.5-sonnet
meta_model: anthropic/claude-3-haiku  # For orchestration decisions

# RLM settings
rlm:
  min_context_for_rlm: 4000  # Tokens before RLM mode activates
  max_iterations: 10
  timeout: 5m

# Memory settings
memory:
  db_path: ~/.local/share/recurse/memory.db
  embedding_model: voyage-3
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    TUI Layer (Bubble Tea)               │
├─────────────────────────────────────────────────────────┤
│  Meta-Controller (Claude Haiku 4.5)                     │
│  ├─ RLM Controller (decompose, subcall, synthesize)     │
│  └─ Budget Manager (tokens, cost, limits)               │
├─────────────────────────────────────────────────────────┤
│  Tiered Hypergraph Memory                               │
│  ├─ Task tier (working memory)                          │
│  ├─ Session tier (accumulated context)                  │
│  └─ Long-term tier (persistent knowledge)               │
├─────────────────────────────────────────────────────────┤
│  Execution Layer                                        │
│  ├─ Python REPL (uv/ruff/ty/pydantic)                   │
│  ├─ Code Understanding (LSP/Tree-sitter)                │
│  └─ LLM Interface (Fantasy provider abstraction)        │
└─────────────────────────────────────────────────────────┘
```

## RLM Mode: When to Use

RLM (Recursive Language Model) mode externalizes context to a Python REPL for iterative exploration. Based on benchmarks:

| Task Type | Use RLM? | Why |
|-----------|----------|-----|
| Counting occurrences | **Yes** | 90-97% fewer tokens, 100% accuracy |
| Summing/aggregating | **Yes** | Python arithmetic is reliable |
| Pattern matching | **Yes** | Regex is more accurate than LLM counting |
| Finding specific info | No | Direct prompting is 10-20x faster |
| Simple Q&A | No | No benefit from code execution |

See [docs/benchmark-results.md](docs/benchmark-results.md) for detailed analysis.

## Development

This project uses [beads](https://github.com/steveyegge/beads) for issue tracking.

```bash
# Build
go build ./...

# Test
go test ./...

# Lint
golangci-lint run ./...

# Find work
bd ready

# Create issue
bd create "Title" -p 1 -t bug
```

### Running Benchmarks

```bash
# Quick validation (no API calls)
go test -v -run TestQuickBenchmarkWithMock ./internal/rlm/benchmark/

# Real LLM benchmark (costs money)
RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLMMode ./internal/rlm/benchmark/

# Full evaluation suite
RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLMvsDirectEvaluation ./internal/rlm/benchmark/
```

## Troubleshooting

### "OPENROUTER_API_KEY not set"

```bash
export OPENROUTER_API_KEY="your-key-here"
```

### "bootstrap.py not found"

Make sure you're running from the repository root, or set:
```bash
export RECURSE_BOOTSTRAP_PATH=/path/to/recurse/pkg/python/bootstrap.py
```

### "REPL timeout" errors

For large context operations, increase the timeout in your config or test:
```yaml
rlm:
  timeout: 10m
```

### Python REPL won't start

```bash
# Verify Python environment
cd pkg/python
source .venv/bin/activate
python bootstrap.py  # Should print JSON ready message
```

## Key Papers

| Paper | Contribution |
|-------|--------------|
| [RLM](https://arxiv.org/abs/2512.24601) | Prompts as manipulable objects |
| [HGMem](https://arxiv.org/abs/2512.23959) | Hypergraph for higher-order memory |
| [ACE](https://arxiv.org/abs/2510.04618) | Structured context evolution |
| [MemEvolve](https://arxiv.org/abs/2512.18746) | Meta-evolution of memory architecture |

## License

TBD

## Acknowledgments

- [Charmbracelet Crush](https://github.com/charmbracelet/crush) - The foundation
- [Beads](https://github.com/steveyegge/beads) - Issue tracking for AI agents
- [Deciduous](https://github.com/notactuallytreyanastasio/deciduous) - Decision graph concepts
