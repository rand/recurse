Invoke a sub-LM call on a specific snippet of content with a focused prompt.

## Parameters

- **prompt**: The task or question to apply to the snippet
- **snippet**: The content to process (code, text, or data)
- **max_tokens** (optional): Maximum tokens for the response (default: 2000)

## Use Cases

- Process a single function when analyzing a large codebase
- Summarize a section of documentation
- Extract specific information from a chunk of text
- Apply a transformation to a piece of code

## Example

```json
{
  "prompt": "Explain what this function does and identify any bugs",
  "snippet": "func calculateTotal(items []Item) float64 {\n  total := 0\n  for _, i := range items {\n    total += i.Price\n  }\n  return total\n}",
  "max_tokens": 500
}
```

## Notes

- Sub-calls are tracked for budget management
- Results can be collected and synthesized with rlm_synthesize
- Use rlm_status to check remaining budget before making sub-calls
