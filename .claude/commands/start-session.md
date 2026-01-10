# Session Start

Beginning a new work session. Let me orient myself.

## Steps

1. **Sync issues from git**:
   ```bash
   bd sync
   ```

2. **Check what's ready to work on**:
   ```bash
   bd ready --json
   ```

3. **Review recent changes**:
   ```bash
   git log --oneline -10
   ```

4. **Check for any failed tests or builds**:
   ```bash
   go build ./...
   go test ./...
   ```

5. **Pick a task and mark it in progress**

## Report

After running these commands, I will:
- Summarize what work is available
- Highlight any blocking issues
- Recommend which task to start
- Note any failing tests that need attention

Running orientation now...
