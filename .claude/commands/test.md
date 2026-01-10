# Run Tests

Run the test suite and report results.

## Commands

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run with race detector
go test -race ./...

# Run specific package
go test ./internal/$ARGUMENTS/...
```

## On Failure

If tests fail:
1. Identify the failing test(s)
2. Read the error message and stack trace
3. Locate the relevant code
4. Determine if this is:
   - A bug in the new code → Fix it
   - A bug in the test → Fix the test
   - An expected behavior change → Update the test
5. Re-run to verify the fix

## On Success

If all tests pass:
- Confirm the test coverage is adequate
- Note any packages with low coverage
- Suggest additional tests if needed

Running tests now for: $ARGUMENTS
