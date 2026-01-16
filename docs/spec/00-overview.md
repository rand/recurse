# Recurse Specification Overview

> Recursive Language Model (RLM) system with hypergraph memory for agentic coding.

## Project Status

**Phase**: Complete
**Core Implementation**: All specs implemented (SPEC-01 through SPEC-09)
**Next Phase**: Testing, refinement, and new feature specs as needed

## Architecture Summary

```
┌─────────────────────────────────────────────────────────────────┐
│                         TUI Layer                               │
│  (Bubble Tea panels: Chat, Budget, Memory, RLM Trace)          │
├─────────────────────────────────────────────────────────────────┤
│                     Orchestration Layer                         │
│  (Meta-controller, RLM Controller, Budget Manager)             │
├─────────────────────────────────────────────────────────────────┤
│                      Memory Substrate                           │
│  (Tiered Hypergraph: Task → Session → Long-term)               │
│  (Embeddings: CodeRankEmbed local provider)                    │
├─────────────────────────────────────────────────────────────────┤
│                      Execution Layer                            │
│  (Python REPL, Code Understanding, LLM Interface, MCP)         │
├─────────────────────────────────────────────────────────────────┤
│                      Storage Layer                              │
│  (SQLite: conversations, hypergraph, embeddings)               │
└─────────────────────────────────────────────────────────────────┘
```

## Specifications

| Spec | Title | Status | Priority |
|------|-------|--------|----------|
| [SPEC-01](./01-cli-entrypoint.md) | CLI Entry Point | Complete | P0 |
| [SPEC-02](./02-tui-polish.md) | TUI Polish | Complete | P1 |
| [SPEC-03](./03-continuous-learning.md) | Continuous Learning | Complete | P2 |
| [SPEC-04](./04-model-routing.md) | Learned Model Routing | Complete | P2 |
| [SPEC-05](./05-context-compression.md) | Context Compression | Complete | P2 |
| [SPEC-06](./06-meta-evolution.md) | Meta-Evolution | Complete | P3 |
| [SPEC-07](./07-budget-management.md) | Budget Management | Complete | P1 |
| [SPEC-08](./08-hallucination-detection.md) | Hallucination Detection | Complete | P2 |
| [SPEC-09](./09-session-context-persistence.md) | Session Context Persistence | Complete | P1 |

## Implementation Order

1. **SPEC-01 (CLI)**: Wire up subsystems into usable CLI - foundation for everything
2. **SPEC-02 (TUI)**: Polish the user interface - enables interactive use
3. **SPEC-03/04/05**: Learning features - can be developed in parallel
4. **SPEC-06 (Meta)**: Self-improvement - depends on usage data from earlier specs

## Implementation Principles

1. **Spec-first**: Write specs before code
2. **Test-driven**: Tests accompany implementation
3. **Traceable**: Code references spec IDs `[SPEC-XX.YY]`
4. **Incremental**: Small, reviewable changes
