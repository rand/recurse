Query working memory to retrieve stored facts, entities, snippets, decisions, and experiences.

## Query Modes

### Text Search
Search memory by content:
```json
{"query": "database", "limit": 5}
```

Filter by type:
```json
{"query": "SQLite", "type": "fact"}
```

### Recent Context
Get most recently accessed nodes:
```json
{"recent": true, "limit": 10}
```

### Related Nodes
Find nodes connected to a specific node:
```json
{"related_to": "node-id-here", "depth": 2}
```

### All Facts
Get all stored facts (default when no query specified):
```json
{"limit": 20}
```

## Response

Returns a list of matching nodes with:
- **id**: Unique node identifier
- **type**: Node type (fact, entity, snippet, decision, experience)
- **subtype**: Additional type info (e.g., "file" for entities)
- **content**: The stored content (truncated to 200 chars)
- **confidence**: Confidence score (0-1)
- **score**: Search relevance score (for text queries)
- **depth**: Graph distance (for related node queries)

## Tips

- Use `recent: true` to see what's currently in working memory
- Use `related_to` with a node ID to explore the knowledge graph
- Combine `query` with `type` to narrow results
