Examine a slice of an externalized variable without loading the entire content.

Use this to inspect portions of large content that was stored via `rlm_externalize`. This is more efficient than using `rlm_execute` with slicing for simple peeks.

Parameters:
- `name`: The variable name to peek at
- `start`: Starting character index (0-based, optional)
- `end`: Ending character index (exclusive, optional)

If neither start nor end is provided, returns the first 1000 characters.
