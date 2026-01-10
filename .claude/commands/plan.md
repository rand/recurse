Plan implementation for: $ARGUMENTS

## Planning Process

1. **Understand the goal** - What are we trying to achieve?
2. **Research existing code** - Read relevant files to understand current state
3. **Identify components** - What modules/packages are affected?
4. **Design approach** - How should we implement this?
5. **Break into tasks** - Create discrete, testable tasks
6. **Identify blockers** - What dependencies exist?

## Output Format

Produce a structured plan:

```markdown
# Plan: [Feature Name]

## Goal
[What we're trying to achieve]

## Affected Components
- [ ] internal/memory/... - [What changes]
- [ ] internal/rlm/... - [What changes]

## Implementation Steps
1. [First task] - [Rationale]
2. [Second task] - [Rationale]
...

## Dependencies
- Step 2 depends on Step 1
- Step 4 depends on external API

## Risks & Mitigations
- Risk: [Description] → Mitigation: [How to handle]

## Testing Strategy
- [How to verify this works]
```

## Then Create Issues

After planning, create issues in bd:

```bash
# Create epic
bd create "Epic: [Feature]" -t epic -p 1

# Create tasks
bd create "[Task 1]" -p 1
bd dep add TASK1_ID EPIC_ID --type parent-child

bd create "[Task 2]" -p 1  
bd dep add TASK2_ID EPIC_ID --type parent-child
bd dep add TASK2_ID TASK1_ID --type blocks  # If blocked by task 1
```

Now plan: $ARGUMENTS
