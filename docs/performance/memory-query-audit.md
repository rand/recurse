# Memory Query Performance Audit

**Date**: 2026-01-12
**Package**: `internal/memory/hypergraph`
**Scope**: `query.go` (455 LOC), `schema.sql`, `store.go`

## Executive Summary

The hypergraph memory system has reasonable indexing for basic lookups but contains several performance anti-patterns that will degrade with scale:

1. **Full table scans** in text search due to leading wildcard LIKE
2. **N+1 query patterns** in subgraph extraction
3. **Function calls in ORDER BY** preventing index usage
4. **Missing composite indices** for common query patterns

## Detailed Analysis

### 1. SearchByContent (query.go:32-97) - HIGH IMPACT

**Issue**: `content LIKE ?` with `"%"+query+"%"` prefix wildcard

```sql
SELECT ... FROM nodes WHERE content LIKE '%search_term%'
```

**Problem**: Leading wildcard (`%search_term`) forces full table scan - SQLite cannot use any index when the pattern starts with `%`.

**Current mitigations**:
- Type/tier filters reduce result set after scan
- LIMIT clause caps returned rows

**Recommendations**:
1. **FTS5 Full-Text Search** (recommended for production):
   ```sql
   CREATE VIRTUAL TABLE nodes_fts USING fts5(content, content='nodes', content_rowid='rowid');
   -- Query: SELECT * FROM nodes_fts WHERE nodes_fts MATCH 'search_term'
   ```
2. **Trigram index** if FTS5 too complex
3. **Short-term**: Add bloom filter for common queries

**Impact**: O(n) scan → O(log n) with FTS5

---

### 2. getImmediateConnections (query.go:186-258) - MEDIUM IMPACT

**Issue**: Double self-join on membership table

```sql
SELECT ... FROM nodes n
  JOIN membership m2 ON n.id = m2.node_id
  JOIN hyperedges h ON m2.hyperedge_id = h.id
  JOIN membership m1 ON h.id = m1.hyperedge_id
WHERE m1.node_id = ? AND n.id != ?
```

**Current indices**:
- `idx_membership_node(node_id)` ✓
- `idx_membership_edge(hyperedge_id)` ✓

**Problem**: For highly connected nodes (e.g., 100+ edges), this requires:
- Index lookup on m1.node_id
- For each row, join to hyperedges (indexed)
- For each hyperedge, join back to membership (indexed)
- Finally join to nodes

**Recommendations**:
1. **Composite index** for covering query:
   ```sql
   CREATE INDEX idx_membership_node_edge_role
     ON membership(node_id, hyperedge_id, role);
   ```
2. **Materialized connections table** for hot paths (if needed)

**Impact**: ~30% improvement for traversals with composite index

---

### 3. GetSubgraph (query.go:315-410) - HIGH IMPACT

**Issue**: Classic N+1 query pattern

```go
for id := range allNodeIDs {
    // Query 1 per node
    rows, err := s.db.QueryContext(ctx, `
        SELECT ... FROM membership WHERE node_id = ?
    `, id)
    ...
}
for id := range edgeIDs {
    // Query 1 per edge
    row := s.db.QueryRowContext(ctx, `
        SELECT ... FROM hyperedges WHERE id = ?
    `, id)
    ...
}
```

**Problem**: For a subgraph with 50 nodes and 30 edges, this issues 80 individual queries instead of 2 batched queries.

**Recommendations**:
1. **Batch membership query**:
   ```sql
   SELECT * FROM membership WHERE node_id IN (?, ?, ?, ...)
   ```
2. **Batch hyperedges query**:
   ```sql
   SELECT * FROM hyperedges WHERE id IN (?, ?, ?, ...)
   ```
3. **Single CTE query** to get entire subgraph in one round-trip

**Impact**: O(n) queries → O(1) queries, 10-50x improvement

---

### 4. RecentNodes (query.go:413-455) - MEDIUM IMPACT

**Issue**: Function in ORDER BY prevents index usage

```sql
ORDER BY julianday(COALESCE(last_accessed, updated_at)) DESC
```

**Current index**: `idx_nodes_accessed(last_accessed)` - NOT USED due to function

**Problem**: SQLite must compute `julianday(COALESCE(...))` for every row, then sort.

**Recommendations**:
1. **Remove COALESCE** - set `last_accessed = updated_at` on insert
2. **Computed column** (SQLite 3.31+):
   ```sql
   ALTER TABLE nodes ADD COLUMN effective_access
     GENERATED ALWAYS AS (COALESCE(last_accessed, updated_at)) STORED;
   CREATE INDEX idx_nodes_effective_access ON nodes(effective_access);
   ```
3. **Short-term**: Ensure `last_accessed` is always populated

**Impact**: Full sort → index-ordered retrieval

---

## Missing Indices

| Index | Benefit | Query Pattern |
|-------|---------|---------------|
| `idx_nodes_tier_accessed(tier, last_accessed DESC)` | RecentNodes with tier filter | WHERE tier IN (...) ORDER BY |
| `idx_membership_covering(node_id, hyperedge_id, role)` | Traversals | JOIN pattern |
| `idx_nodes_access_updated(access_count DESC, updated_at DESC)` | SearchByContent ordering | ORDER BY access_count DESC |

## Caching Recommendations

### LRU Cache for Hot Paths

1. **Node cache** (high hit rate expected):
   ```go
   type nodeCache struct {
       cache *lru.Cache[string, *Node]  // 1000 entries
   }
   ```

2. **Connection cache** (for repeated traversals):
   ```go
   type connectionCache struct {
       cache *lru.Cache[string, []*ConnectedNode]  // 100 entries, 60s TTL
   }
   ```

3. **Invalidation**: On node/edge mutation, evict affected entries

### Query Result Cache

For repeated identical queries (common in RLM loops):
- Cache key: hash of (query + args)
- Cache value: result slice
- TTL: 5-30 seconds depending on query type

## Implementation Priority

| Priority | Item | Effort | Impact |
|----------|------|--------|--------|
| P0 | Batch queries in GetSubgraph | Low | High |
| P1 | Add composite membership index | Low | Medium |
| P1 | Ensure last_accessed populated | Low | Medium |
| P2 | Add FTS5 for content search | Medium | High |
| P2 | Add node LRU cache | Medium | Medium |
| P3 | Computed column for effective_access | Low | Medium |

## Benchmarks (TODO)

To validate these findings, create benchmarks:

```go
func BenchmarkSearchByContent(b *testing.B) { ... }
func BenchmarkGetConnected(b *testing.B) { ... }
func BenchmarkGetSubgraph(b *testing.B) { ... }
func BenchmarkRecentNodes(b *testing.B) { ... }
```

Run with varying data sizes: 100, 1K, 10K, 100K nodes.

## Conclusion

The current implementation is suitable for small-to-medium hypergraphs (< 10K nodes). For production use with larger graphs:

1. **Immediate**: Fix N+1 in GetSubgraph, add composite index
2. **Short-term**: Add LRU caching, ensure last_accessed populated
3. **Medium-term**: Implement FTS5 for content search

Total estimated improvement: 5-20x for common query patterns at scale.
