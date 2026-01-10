# Session End

Wrapping up the current work session.

## Checklist

1. **File any discovered issues**
   - Bugs noticed but not fixed
   - Follow-up tasks identified
   - Technical debt observed
   
   ```bash
   bd create "Discovered: [issue]" -t bug -p 2
   bd dep add NEW_ID CURRENT_TASK_ID --type discovered-from
   ```

2. **Update task statuses**
   - Close completed tasks: `bd close ID --reason "Description"`
   - Update in-progress tasks if needed

3. **Run quality gates**
   ```bash
   go test ./...
   golangci-lint run ./...
   ```

4. **Sync the issue tracker**
   ```bash
   bd sync
   ```

5. **Commit and push**
   ```bash
   git add .
   git status
   # Commit with conventional format
   git commit -m "type(scope): description"
   git push
   ```

6. **Verify clean state**
   - All changes committed
   - Tests passing
   - Issues synced

## Session Summary

After completing the checklist, I will provide:
- Summary of work completed
- Issues filed or updated
- Recommended next task for future session
- Any blockers or concerns

Running end-session checklist now...
