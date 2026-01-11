Store content as a named variable in the Python REPL for later manipulation.

Use this to "externalize" large content (file contents, API responses, etc.) so you can work with it programmatically using Python code via `rlm_execute`.

The content is stored as a string and can be accessed by name in subsequent `rlm_execute` calls.

Example workflow:
1. `rlm_externalize` with name="code" and content=<file contents>
2. `rlm_execute` with code="len(code)" to get the length
3. `rlm_execute` with code="code[0:500]" to peek at the first 500 chars
