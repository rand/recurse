# Testing Strategy

This document describes how to test Recurse components.

---

## Quick Reference

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for a specific package
go test ./internal/memory/...

# Run a specific test
go test -run TestHypergraphStore ./internal/memory/hypergraph

# Run tests with race detector
go test -race ./...

# Run tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## Test Categories

### Unit Tests

Test individual functions and types in isolation.

**Location**: Same package as the code being tested (`*_test.go`)

**Naming**: `Test<FunctionName>` or `Test<Type>_<Method>`

**Example**:
```go
func TestHypergraphStore_AddNode(t *testing.T) {
    store := NewHypergraphStore(":memory:")
    defer store.Close()
    
    node := &Node{
        ID:      "test-1",
        Type:    NodeTypeEntity,
        Content: "test content",
    }
    
    err := store.AddNode(node)
    assert.NoError(t, err)
    
    retrieved, err := store.GetNode("test-1")
    assert.NoError(t, err)
    assert.Equal(t, node.Content, retrieved.Content)
}
```

### Table-Driven Tests

Preferred for testing multiple cases:

```go
func TestBudgetLimits_Exceeded(t *testing.T) {
    tests := []struct {
        name     string
        state    BudgetState
        limits   BudgetLimits
        expected bool
    }{
        {
            name:     "under limits",
            state:    BudgetState{InputTokens: 1000},
            limits:   BudgetLimits{MaxInputTokens: 10000},
            expected: false,
        },
        {
            name:     "exactly at limit",
            state:    BudgetState{InputTokens: 10000},
            limits:   BudgetLimits{MaxInputTokens: 10000},
            expected: false,
        },
        {
            name:     "over limit",
            state:    BudgetState{InputTokens: 10001},
            limits:   BudgetLimits{MaxInputTokens: 10000},
            expected: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := tt.limits.Exceeded(tt.state)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### Integration Tests

Test interactions between components.

**Build tag**: Use `//go:build integration` for tests that need external resources

**Example**:
```go
//go:build integration

func TestRLMController_EndToEnd(t *testing.T) {
    // Skip if Python not available
    if _, err := exec.LookPath("python3"); err != nil {
        t.Skip("python3 not available")
    }
    
    ctrl := NewRLMController(Config{
        REPLPath: "python3",
    })
    defer ctrl.Close()
    
    // Test externalize → execute → synthesize flow
    err := ctrl.Externalize("code", "def foo(): return 42")
    require.NoError(t, err)
    
    output, err := ctrl.Execute("exec(code); print(foo())")
    require.NoError(t, err)
    assert.Contains(t, output, "42")
}
```

---

## Test Fixtures

### Database Fixtures

Use in-memory SQLite for fast tests:

```go
func setupTestStore(t *testing.T) *HypergraphStore {
    store := NewHypergraphStore(":memory:")
    t.Cleanup(func() { store.Close() })
    return store
}
```

### Mock LLM Responses

Use interfaces and mocks for LLM tests:

```go
type MockLLM struct {
    responses []string
    idx       int
}

func (m *MockLLM) Complete(prompt string) (string, error) {
    if m.idx >= len(m.responses) {
        return "", errors.New("no more responses")
    }
    resp := m.responses[m.idx]
    m.idx++
    return resp, nil
}
```

---

## Testing Specific Components

### Memory Subsystem

```bash
go test ./internal/memory/...
```

Key tests:
- Node CRUD operations
- Hyperedge creation and traversal
- Tier promotion logic
- Decay calculations

### RLM Controller

```bash
go test ./internal/rlm/...
```

Key tests:
- REPL lifecycle (start, execute, close)
- Sandbox constraints (timeout, memory)
- Decomposition strategies
- Sub-call invocation

### Budget Manager

```bash
go test ./internal/budget/...
```

Key tests:
- Token counting
- Cost calculations
- Limit enforcement
- Alert thresholds

---

## Coverage Goals

| Package | Target Coverage |
|---------|-----------------|
| `internal/memory/hypergraph` | 80% |
| `internal/memory/tiers` | 70% |
| `internal/rlm` | 70% |
| `internal/budget` | 90% |
| `internal/tui` | 50% (UI code is harder to test) |

---

## Before Committing

1. **Run all tests**: `go test ./...`
2. **Check for race conditions**: `go test -race ./...`
3. **Verify coverage didn't drop**: Check coverage report
4. **Run linter**: `golangci-lint run ./...`

---

## CI Pipeline

Tests run automatically on every push via GitHub Actions:

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: go test -race -coverprofile=coverage.out ./...
      - uses: codecov/codecov-action@v4
```
