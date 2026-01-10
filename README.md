# Recurse

> A recursive language model (RLM) system with hypergraph memory for agentic coding.

Recurse is a personal agentic coding environment that extends [Crush](https://github.com/charmbracelet/crush) with:

- **RLM Orchestration**: Treat prompts as manipulable objects in a Python REPL
- **Tiered Hypergraph Memory**: Knowledge that persists and evolves across sessions
- **Embedded Reasoning Traces**: Decision graphs linked to git commits

## Status

🚧 **Under Development** - This is a personal daily-use tool, not a commercial product.

## Design

See [docs/SPEC.md](docs/SPEC.md) for the full technical specification.

### Architecture

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

### Key Papers

| Paper | Contribution |
|-------|--------------|
| [RLM](https://arxiv.org/abs/2512.24601) | Prompts as manipulable objects |
| [HGMem](https://arxiv.org/abs/2512.23959) | Hypergraph for higher-order memory |
| [ACE](https://arxiv.org/abs/2510.04618) | Structured context evolution |
| [MemEvolve](https://arxiv.org/abs/2512.18746) | Meta-evolution of memory architecture |

## Development

This project uses [beads](https://github.com/steveyegge/beads) for issue tracking.

```bash
# Build
go build ./...

# Test
go test ./...

# Find work
bd ready

# Create issue
bd create "Title" -p 1 -t bug
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for full development setup.

## License

TBD

## Acknowledgments

- [Charmbracelet Crush](https://github.com/charmbracelet/crush) - The foundation
- [Beads](https://github.com/steveyegge/beads) - Issue tracking for AI agents
- [Deciduous](https://github.com/notactuallytreyanastasio/deciduous) - Decision graph concepts
