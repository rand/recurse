package rlm

import (
	"regexp"
	"strings"
)

// REPLSuggestion contains a proactive recommendation for using the REPL.
type REPLSuggestion struct {
	// Pattern identifies which computation pattern was matched.
	Pattern string

	// Approach describes the recommended computation strategy.
	Approach string

	// CodeHint provides example Python code for the computation.
	CodeHint string

	// Confidence indicates how confident the advisor is (0.0-1.0).
	Confidence float64

	// Variables lists detected variables/entities in the query.
	Variables []string
}

// ComputationPattern defines a pattern for detecting computation opportunities.
type ComputationPattern struct {
	// Name identifies the pattern.
	Name string

	// Regex matches queries where this pattern applies.
	Regex *regexp.Regexp

	// Approach describes the recommended computation method.
	Approach string

	// CodeTemplate provides a Python code template.
	CodeTemplate string

	// Confidence base confidence for this pattern.
	Confidence float64

	// VariableExtractor extracts relevant variables from the query.
	VariableExtractor func(query string) []string
}

// ComputationAdvisor proactively detects computation opportunities
// and suggests REPL-based approaches.
type ComputationAdvisor struct {
	patterns []ComputationPattern
}

// NewComputationAdvisor creates an advisor with default computation patterns.
func NewComputationAdvisor() *ComputationAdvisor {
	return &ComputationAdvisor{
		patterns: defaultComputationPatterns(),
	}
}

// SuggestREPL analyzes a query and returns a REPL suggestion if appropriate.
// Returns nil if no computation pattern matches.
func (a *ComputationAdvisor) SuggestREPL(query string, contexts []ContextSource) *REPLSuggestion {
	queryLower := strings.ToLower(query)

	var bestMatch *REPLSuggestion
	var bestConfidence float64

	for _, pattern := range a.patterns {
		if pattern.Regex.MatchString(queryLower) {
			confidence := pattern.Confidence

			// Boost confidence if context has relevant structure
			if len(contexts) > 0 {
				confidence = a.adjustConfidenceForContext(confidence, contexts[0].Content, pattern.Name)
			}

			if confidence > bestConfidence {
				variables := []string{}
				if pattern.VariableExtractor != nil {
					variables = pattern.VariableExtractor(query)
				}

				bestMatch = &REPLSuggestion{
					Pattern:    pattern.Name,
					Approach:   pattern.Approach,
					CodeHint:   pattern.CodeTemplate,
					Confidence: confidence,
					Variables:  variables,
				}
				bestConfidence = confidence
			}
		}
	}

	// Only return suggestions with sufficient confidence
	if bestMatch != nil && bestMatch.Confidence < 0.5 {
		return nil
	}

	return bestMatch
}

// AddPattern allows registering custom computation patterns.
func (a *ComputationAdvisor) AddPattern(pattern ComputationPattern) {
	a.patterns = append(a.patterns, pattern)
}

// Patterns returns all registered patterns (for testing/inspection).
func (a *ComputationAdvisor) Patterns() []ComputationPattern {
	return a.patterns
}

// adjustConfidenceForContext modifies confidence based on context signals.
func (a *ComputationAdvisor) adjustConfidenceForContext(base float64, content, patternName string) float64 {
	confidence := base

	// Count patterns benefit from numeric-dense content
	if strings.HasPrefix(patternName, "count") || strings.HasPrefix(patternName, "frequency") {
		numericPattern := regexp.MustCompile(`\d+`)
		matches := numericPattern.FindAllString(content, -1)
		density := float64(len(matches)) / float64(max(1, len(content)))
		if density > 0.005 {
			confidence += 0.1
		}
	}

	// Arithmetic patterns benefit from monetary/numeric content
	if strings.HasPrefix(patternName, "sum") || strings.HasPrefix(patternName, "average") {
		if strings.Contains(content, "$") || strings.Contains(content, "cost") ||
			strings.Contains(content, "price") || strings.Contains(content, "amount") {
			confidence += 0.15
		}
	}

	// Sorting/filtering patterns benefit from list-like structure
	if strings.HasPrefix(patternName, "sort") || strings.HasPrefix(patternName, "filter") {
		lines := strings.Split(content, "\n")
		if len(lines) > 5 {
			confidence += 0.1
		}
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// defaultComputationPatterns returns the built-in computation patterns.
func defaultComputationPatterns() []ComputationPattern {
	return []ComputationPattern{
		// Counting patterns
		{
			Name:       "count_occurrences",
			Regex:      regexp.MustCompile(`(?i)(how many|count|number of|total).{0,30}(times|occurrences|instances|mentions)`),
			Approach:   "Use collections.Counter or str.count() to count occurrences precisely",
			CodeTemplate: `from collections import Counter
# Count occurrences in text
text = context_content
count = text.lower().count("target_word")
# Or for multiple items:
words = text.split()
counter = Counter(words)`,
			Confidence:        0.9,
			VariableExtractor: extractCountTarget,
		},
		{
			Name:       "count_items",
			Regex:      regexp.MustCompile(`(?i)(how many|count|list all|find all).{0,20}(files?|functions?|classes?|imports?|lines?|methods?|variables?)`),
			Approach:   "Parse and enumerate code elements systematically",
			CodeTemplate: `import ast
# For Python code analysis
tree = ast.parse(code)
items = [node for node in ast.walk(tree) if isinstance(node, ast.FunctionDef)]
count = len(items)`,
			Confidence:        0.85,
			VariableExtractor: extractCodeElement,
		},
		{
			Name:       "frequency_analysis",
			Regex:      regexp.MustCompile(`(?i)(how often|frequency|most (common|frequent)|least (common|frequent))`),
			Approach:   "Build frequency distribution and analyze distribution",
			CodeTemplate: `from collections import Counter
items = extract_items(context)
freq = Counter(items)
most_common = freq.most_common(10)
least_common = freq.most_common()[-10:]`,
			Confidence: 0.85,
		},

		// Arithmetic patterns
		{
			Name:       "sum_values",
			Regex:      regexp.MustCompile(`(?i)(sum|total|add up|combined).{0,30}(\$|dollars?|amount|cost|price|value|sales|revenue)`),
			Approach:   "Extract numeric values and sum them precisely",
			CodeTemplate: `import re
# Extract numbers and sum
numbers = re.findall(r'\$?([\d,]+\.?\d*)', context)
values = [float(n.replace(',', '')) for n in numbers]
total = sum(values)`,
			Confidence: 0.9,
		},
		{
			Name:       "average_calculation",
			Regex:      regexp.MustCompile(`(?i)(average|mean|avg).{0,30}(of|across|for)`),
			Approach:   "Collect values and compute statistical average",
			CodeTemplate: `import statistics
values = extract_numbers(context)
avg = statistics.mean(values)
# Also available: median, stdev, etc.`,
			Confidence: 0.85,
		},
		{
			Name:       "percentage_calculation",
			Regex:      regexp.MustCompile(`(?i)(percentage|percent|ratio|proportion).{0,20}(of|that|which)`),
			Approach:   "Count matching items and compute percentage",
			CodeTemplate: `total = len(all_items)
matching = len([x for x in all_items if condition(x)])
percentage = (matching / total) * 100 if total > 0 else 0`,
			Confidence: 0.8,
		},

		// Sorting patterns
		{
			Name:       "sort_by_attribute",
			Regex:      regexp.MustCompile(`(?i)(sort|order|rank|arrange).{0,20}(by|based on|according to)`),
			Approach:   "Extract items with attributes and sort systematically",
			CodeTemplate: `items = parse_items(context)
sorted_items = sorted(items, key=lambda x: x.attribute, reverse=True)
# Top N:
top_n = sorted_items[:n]`,
			Confidence: 0.85,
		},
		{
			Name:       "find_extremes",
			Regex:      regexp.MustCompile(`(?i)(highest|lowest|largest|smallest|maximum|minimum|most|least|top|bottom)\s+\d*`),
			Approach:   "Extract values and find extremes precisely",
			CodeTemplate: `values = extract_values(context)
maximum = max(values)
minimum = min(values)
# With items: max(items, key=lambda x: x.value)`,
			Confidence: 0.85,
		},

		// Filtering patterns
		{
			Name:       "filter_by_condition",
			Regex:      regexp.MustCompile(`(?i)(filter|find|select|get).{0,20}(where|that|which|with).{0,20}(greater|less|more|fewer|above|below|between|equals?|contains?)`),
			Approach:   "Parse items and apply filter conditions precisely",
			CodeTemplate: `items = parse_items(context)
filtered = [x for x in items if condition(x)]
# Example conditions:
# x.value > threshold
# x.date.year == 2024
# "keyword" in x.text`,
			Confidence: 0.85,
		},
		{
			Name:       "filter_by_date",
			Regex:      regexp.MustCompile(`(?i)(from|since|before|after|during|between).{0,20}(january|february|march|april|may|june|july|august|september|october|november|december|\d{4}|\d{1,2}/\d{1,2})`),
			Approach:   "Parse dates and filter by temporal constraints",
			CodeTemplate: `from datetime import datetime
items = parse_items_with_dates(context)
filtered = [x for x in items if start_date <= x.date <= end_date]`,
			Confidence: 0.75,
		},

		// Comparison patterns
		{
			Name:       "compare_values",
			Regex:      regexp.MustCompile(`(?i)(compare|difference|change|growth|increase|decrease).{0,20}(between|from|over)`),
			Approach:   "Extract comparable values and compute differences",
			CodeTemplate: `value_a = extract_value(context, "period_a")
value_b = extract_value(context, "period_b")
difference = value_b - value_a
percent_change = ((value_b - value_a) / value_a) * 100 if value_a else 0`,
			Confidence: 0.8,
		},

		// Aggregation patterns
		{
			Name:       "group_by",
			Regex:      regexp.MustCompile(`(?i)(group|aggregate|summarize|breakdown).{0,20}(by|per|for each)`),
			Approach:   "Group items by attribute and aggregate",
			CodeTemplate: `from collections import defaultdict
groups = defaultdict(list)
for item in items:
    groups[item.category].append(item)
summary = {k: len(v) for k, v in groups.items()}`,
			Confidence: 0.8,
		},

		// Deduplication patterns
		{
			Name:       "unique_items",
			Regex:      regexp.MustCompile(`(?i)(unique|distinct|deduplicate|remove duplicates).{0,20}(items?|values?|entries?|records?)`),
			Approach:   "Extract items and deduplicate preserving order if needed",
			CodeTemplate: `items = extract_items(context)
unique = list(dict.fromkeys(items))  # Preserves order
# Or: unique = list(set(items))  # No order guarantee`,
			Confidence: 0.85,
		},
	}
}

// extractCountTarget attempts to extract the target being counted from a query.
func extractCountTarget(query string) []string {
	// Look for quoted strings
	quotedPattern := regexp.MustCompile(`["']([^"']+)["']`)
	matches := quotedPattern.FindAllStringSubmatch(query, -1)
	var targets []string
	for _, m := range matches {
		if len(m) > 1 {
			targets = append(targets, m[1])
		}
	}

	// Look for "of X" or "the X" patterns
	if len(targets) == 0 {
		ofPattern := regexp.MustCompile(`(?i)(?:of|the)\s+["']?(\w+)["']?`)
		matches = ofPattern.FindAllStringSubmatch(query, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 2 {
				// Filter out common stop words
				word := strings.ToLower(m[1])
				if word != "the" && word != "and" && word != "for" && word != "all" {
					targets = append(targets, m[1])
				}
			}
		}
	}

	return targets
}

// extractCodeElement attempts to extract the code element type from a query.
func extractCodeElement(query string) []string {
	elements := []string{
		"function", "class", "method", "variable", "import", "file", "line",
		"module", "package", "constant", "type", "interface", "struct",
	}

	queryLower := strings.ToLower(query)
	var found []string
	for _, elem := range elements {
		if strings.Contains(queryLower, elem) {
			found = append(found, elem)
		}
	}

	return found
}

// FormatForPrompt generates prompt text from a REPL suggestion.
func (s *REPLSuggestion) FormatForPrompt() string {
	if s == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Computation Suggestion\n\n")
	sb.WriteString("Pattern detected: ")
	sb.WriteString(s.Pattern)
	sb.WriteString("\n\n")
	sb.WriteString("Recommended approach: ")
	sb.WriteString(s.Approach)
	sb.WriteString("\n\n")

	if s.CodeHint != "" {
		sb.WriteString("Example code:\n```python\n")
		sb.WriteString(s.CodeHint)
		sb.WriteString("\n```\n")
	}

	if len(s.Variables) > 0 {
		sb.WriteString("\nDetected targets: ")
		sb.WriteString(strings.Join(s.Variables, ", "))
		sb.WriteString("\n")
	}

	return sb.String()
}
