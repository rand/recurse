# Code Review Process

This document describes the code review checklist and process for Recurse.

---

## Quick Review Command

Use the slash command to trigger a code review:

```
/code-review
```

This will review current changes following this checklist.

---

## Review Checklist

### 1. Correctness

- [ ] Does the code do what it's supposed to do?
- [ ] Are there any obvious bugs?
- [ ] Are edge cases handled?
- [ ] Are error conditions handled appropriately?
- [ ] Is the logic correct?

### 2. Performance

- [ ] Any obvious performance issues?
- [ ] Appropriate use of concurrency?
- [ ] Memory allocations reasonable?
- [ ] Database queries efficient?
- [ ] No unnecessary I/O?

### 3. Design

- [ ] Follows existing patterns in the codebase?
- [ ] Idiomatic Go code?
- [ ] Appropriate abstraction level?
- [ ] Dependencies flow in the right direction?
- [ ] No circular dependencies?

### 4. Testing

- [ ] New functionality has tests?
- [ ] Tests cover edge cases?
- [ ] Tests are deterministic?
- [ ] Integration points tested?
- [ ] No flaky tests introduced?

### 5. Documentation

- [ ] Public functions have godoc comments?
- [ ] Complex logic is commented?
- [ ] README updated if needed?
- [ ] API changes documented?

### 6. Security

- [ ] No hardcoded secrets?
- [ ] Input validation present?
- [ ] Sandbox constraints enforced?
- [ ] No SQL injection vulnerabilities?
- [ ] File paths sanitized?

---

## Issue Classification

### Blocking Issues

**Must be fixed before commit.** Examples:

- Bug that will cause crash/data loss
- Security vulnerability
- Broken tests
- Major design flaw
- Missing critical error handling

### Non-Blocking Issues

**File as `bd` issues for later.** Examples:

- Minor style inconsistencies
- "Nice to have" improvements
- Future optimization opportunities
- Documentation improvements
- Refactoring suggestions

---

## Providing Feedback

### Format

Use specific, actionable feedback with file:line references:

```
internal/memory/hypergraph/store.go:42 - BLOCKING
Error is silently ignored. Should return or log:
    if err != nil {
+       return fmt.Errorf("adding node: %w", err)
    }

internal/rlm/controller.go:128 - non-blocking
Consider extracting this into a separate function for testability.
File issue: bd create "Refactor decompose logic" -p 3
```

### Tone

- Be constructive, not critical
- Explain the "why" behind suggestions
- Offer alternatives when rejecting an approach
- Acknowledge good patterns when you see them

---

## Self-Review Before Commit

Before requesting review or committing, verify:

```bash
# 1. Tests pass
go test ./...

# 2. No race conditions
go test -race ./...

# 3. Linter passes
golangci-lint run ./...

# 4. Code is formatted
go fmt ./...

# 5. No obvious issues
git diff --cached  # Review your own changes
```

---

## Review Process Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Author    │────▶│   Review    │────▶│   Merge     │
│   Submits   │     │   Process   │     │             │
└─────────────┘     └─────────────┘     └─────────────┘
                           │
                           ▼
              ┌─────────────────────────┐
              │  Blocking issues found? │
              └─────────────────────────┘
                    │           │
                   Yes          No
                    │           │
                    ▼           ▼
            ┌───────────┐  ┌───────────┐
            │   Fix &   │  │  File bd  │
            │ Re-submit │  │  issues   │
            └───────────┘  └───────────┘
                    │           │
                    └─────┬─────┘
                          │
                          ▼
                    ┌───────────┐
                    │   Merge   │
                    └───────────┘
```

---

## AI Agent Reviews

When Claude is reviewing code:

1. Apply this entire checklist systematically
2. Prioritize blocking issues first
3. Provide file:line references for all feedback
4. Suggest fixes, not just problems
5. File non-blocking issues to `bd` immediately
6. Be explicit about what's blocking vs non-blocking
