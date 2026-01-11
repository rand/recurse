Store information in working memory. Use this to remember facts, entities, code snippets, decisions, and experiences during problem-solving.

## Memory Types

- **fact**: A piece of knowledge (e.g., "The API returns JSON", "Max retries is 3")
- **entity**: A named thing in the codebase (file, function, class, variable)
- **snippet**: A code fragment with file/line provenance
- **decision**: A choice made with rationale and alternatives considered
- **experience**: An outcome (success or failure) to learn from

## Examples

Store a fact:
```json
{"type": "fact", "content": "The database uses SQLite", "confidence": 0.9}
```

Store an entity:
```json
{"type": "entity", "content": "main.go", "subtype": "file"}
```

Store a code snippet:
```json
{"type": "snippet", "content": "func init() { ... }", "file": "config.go", "line": 42}
```

Store a decision:
```json
{"type": "decision", "content": "Use BFS for traversal", "rationale": "Avoids stack overflow", "alternatives": ["DFS", "A*"]}
```

Store an experience:
```json
{"type": "experience", "content": "Tried mocking the DB", "outcome": "Tests became flaky", "success": false}
```

## Deduplication

Facts and entities are automatically deduplicated. If you store the same content twice, the existing node is returned with an incremented access count.
