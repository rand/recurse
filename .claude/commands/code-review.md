Review the current changes following docs/process/code-review.md.

## Review Criteria

1. **Correctness** - Does the code do what it's supposed to? Any bugs?
2. **Performance** - Compiler performance and generated code quality
3. **Design** - Idiomatic Go, follows existing patterns
4. **Testing** - Adequate coverage for new functionality
5. **Documentation** - Godoc comments, updated docs

## Output Format

Provide specific, actionable feedback with file:line references.

- **Blocking issues**: Must fix before commit
- **Non-blocking**: File as `bd` issues for later

## Example Output

```
internal/memory/hypergraph/store.go:42 - BLOCKING
Error is silently ignored. Should return or log.

internal/rlm/controller.go:128 - non-blocking
Consider extracting this into a separate function for testability.
→ bd create "Refactor decompose logic in controller.go" -p 3
```

Review the current diff now.
