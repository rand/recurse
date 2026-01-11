Create relationships between nodes in working memory. Use this to build a knowledge graph of how concepts, files, and decisions connect.

## Parameters

- **label**: The relationship type (e.g., "contains", "calls", "depends_on")
- **subject_id**: The source node ID
- **object_id**: The target node ID

## Common Relationship Types

| Label | Meaning | Example |
|-------|---------|---------|
| `contains` | Subject contains object | file contains function |
| `calls` | Subject calls/invokes object | function calls function |
| `depends_on` | Subject depends on object | module depends on package |
| `references` | Subject references object | code references config |
| `implements` | Subject implements object | struct implements interface |
| `caused_by` | Subject was caused by object | error caused by decision |
| `led_to` | Subject led to object | decision led to outcome |

## Example

After storing entities:
```json
// First, store the entities
{"type": "entity", "content": "main.go", "subtype": "file"}  // returns id: "abc123"
{"type": "entity", "content": "func main()", "subtype": "function"}  // returns id: "def456"

// Then create the relationship
{"label": "contains", "subject_id": "abc123", "object_id": "def456"}
```

## Tips

- Store nodes first with `memory_store`, then use their IDs to create relationships
- Use `memory_query` with `related_to` to explore the graph you've built
- Consistent label naming helps with later queries
