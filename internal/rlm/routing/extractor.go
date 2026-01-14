package routing

import (
	"context"
	"regexp"
	"strings"
	"unicode"
)

// FeatureExtractor extracts task features for routing decisions.
type FeatureExtractor struct {
	classifier *CategoryClassifier
	config     ExtractorConfig
}

// ExtractorConfig configures the feature extractor.
type ExtractorConfig struct {
	// TokensPerChar is the approximate tokens per character (default 0.25).
	TokensPerChar float64

	// MinCodeBlockSize is the minimum size to consider as a code block.
	MinCodeBlockSize int
}

// NewFeatureExtractor creates a new feature extractor.
func NewFeatureExtractor(classifier *CategoryClassifier, cfg ExtractorConfig) *FeatureExtractor {
	if cfg.TokensPerChar <= 0 {
		cfg.TokensPerChar = 0.25 // ~4 chars per token average
	}
	if cfg.MinCodeBlockSize <= 0 {
		cfg.MinCodeBlockSize = 20
	}

	return &FeatureExtractor{
		classifier: classifier,
		config:     cfg,
	}
}

// Extract analyzes a query and returns extracted features.
func (e *FeatureExtractor) Extract(ctx context.Context, query string, conversationTurns, priorFailures int) *TaskFeatures {
	features := &TaskFeatures{
		TokenCount:        e.estimateTokens(query),
		HasCode:           e.detectCode(query),
		HasMath:           e.detectMath(query),
		Languages:         e.detectLanguages(query),
		EstimatedDepth:    e.estimateDepth(query),
		Ambiguity:         e.estimateAmbiguity(query),
		ConversationTurns: conversationTurns,
		PriorFailures:     priorFailures,
	}

	// Use classifier if available
	if e.classifier != nil {
		features.Category, features.CategoryConfidence = e.classifier.Classify(ctx, query)
	} else {
		features.Category = CategoryConversation
		features.CategoryConfidence = 0.5
	}

	return features
}

// estimateTokens estimates the token count for a query.
func (e *FeatureExtractor) estimateTokens(query string) int {
	// Simple estimation: chars * tokensPerChar
	// More accurate would use actual tokenizer
	return int(float64(len(query)) * e.config.TokensPerChar)
}

// detectCode checks if the query contains code.
func (e *FeatureExtractor) detectCode(query string) bool {
	// Check for code blocks
	if strings.Contains(query, "```") {
		return true
	}

	// Check for indented blocks (4+ spaces)
	lines := strings.Split(query, "\n")
	indentedLines := 0
	for _, line := range lines {
		if len(line) >= 4 && strings.HasPrefix(line, "    ") {
			indentedLines++
		}
	}
	if indentedLines >= 3 {
		return true
	}

	// Check for code-like patterns
	codePatterns := []*regexp.Regexp{
		regexp.MustCompile(`func\s+\w+\s*\(`),                    // Go function
		regexp.MustCompile(`def\s+\w+\s*\(`),                     // Python function
		regexp.MustCompile(`function\s+\w+\s*\(`),                // JS function
		regexp.MustCompile(`class\s+\w+`),                        // Class definition
		regexp.MustCompile(`import\s+[\w.]+`),                    // Import statement
		regexp.MustCompile(`(const|let|var)\s+\w+\s*=`),          // Variable declaration
		regexp.MustCompile(`if\s*\(.+\)\s*\{`),                   // If statement with braces
		regexp.MustCompile(`for\s*\(.+\)\s*\{`),                  // For loop
		regexp.MustCompile(`\w+\s*:=\s*`),                        // Go short declaration
		regexp.MustCompile(`\{\s*"[\w_]+"\s*:\s*`),               // JSON-like
		regexp.MustCompile(`<\w+(\s+\w+="[^"]*")*\s*/?>`),        // HTML/XML tag
		regexp.MustCompile(`@\w+\s*(def|class|async def)`),       // Python decorator
		regexp.MustCompile(`pub\s+(fn|struct|enum|trait|impl)`),  // Rust
		regexp.MustCompile(`fn\s+\w+\s*\([^)]*\)\s*(->|!)?`),     // Rust/Zig function
	}

	for _, pattern := range codePatterns {
		if pattern.MatchString(query) {
			return true
		}
	}

	// Check for high density of special characters common in code
	specialCount := 0
	for _, r := range query {
		if strings.ContainsRune("{}[]();:=<>+-*/&|^%@#$", r) {
			specialCount++
		}
	}
	// If >5% special characters, likely code
	if len(query) > 0 && float64(specialCount)/float64(len(query)) > 0.05 {
		return true
	}

	return false
}

// detectMath checks if the query contains mathematical content.
func (e *FeatureExtractor) detectMath(query string) bool {
	// Check for LaTeX math delimiters
	if strings.Contains(query, "$$") || strings.Contains(query, "\\(") || strings.Contains(query, "\\[") {
		return true
	}

	// Check for common math symbols
	mathSymbols := []string{"∑", "∫", "∏", "√", "∞", "±", "≤", "≥", "≠", "≈", "∈", "∉", "⊂", "⊃", "∪", "∩"}
	for _, sym := range mathSymbols {
		if strings.Contains(query, sym) {
			return true
		}
	}

	// Check for LaTeX commands
	latexPatterns := []*regexp.Regexp{
		regexp.MustCompile(`\\(frac|sqrt|sum|int|prod|lim)\{`),
		regexp.MustCompile(`\\(alpha|beta|gamma|delta|theta|pi|sigma|omega)`),
		regexp.MustCompile(`\^{[^}]+}`),
		regexp.MustCompile(`_{[^}]+}`),
	}
	for _, pattern := range latexPatterns {
		if pattern.MatchString(query) {
			return true
		}
	}

	// Check for mathematical expressions
	mathExprPatterns := []*regexp.Regexp{
		regexp.MustCompile(`\d+\s*[+\-*/^]\s*\d+`),            // Basic arithmetic
		regexp.MustCompile(`\([^)]+\)\s*[+\-*/^]\s*\(`),       // Nested expressions
		regexp.MustCompile(`(sin|cos|tan|log|ln|exp)\s*\(`),   // Math functions
		regexp.MustCompile(`d[xy]/d[xy]`),                     // Derivatives
		regexp.MustCompile(`\bprove\b.*\btheorem\b`),          // Theorem proving
		regexp.MustCompile(`\bsolve\b.*\bequation\b`),         // Equation solving
		regexp.MustCompile(`\bintegral\b|\bderivative\b`),     // Calculus terms
		regexp.MustCompile(`\bmatrix\b|\bdeterminant\b`),      // Linear algebra
		regexp.MustCompile(`P\([^)]+\)\s*=`),                  // Probability
	}
	for _, pattern := range mathExprPatterns {
		if pattern.MatchString(strings.ToLower(query)) {
			return true
		}
	}

	return false
}

// detectLanguages attempts to detect programming languages in the query.
func (e *FeatureExtractor) detectLanguages(query string) []string {
	languages := make(map[string]bool)
	queryLower := strings.ToLower(query)

	// Language-specific patterns
	langPatterns := map[string][]*regexp.Regexp{
		"go": {
			regexp.MustCompile(`\bgo\s+(build|test|run|mod|get)\b`),
			regexp.MustCompile(`package\s+\w+`),
			regexp.MustCompile(`func\s+\w+\s*\([^)]*\)\s*(\([^)]*\))?\s*\{`),
			regexp.MustCompile(`\w+\s*:=\s*`),
			regexp.MustCompile(`\bdefer\s+`),
			regexp.MustCompile(`go\.mod|go\.sum`),
		},
		"python": {
			regexp.MustCompile(`\bpython[23]?\b`),
			regexp.MustCompile(`def\s+\w+\s*\([^)]*\)\s*(->\s*\w+)?:`),
			regexp.MustCompile(`import\s+\w+|from\s+\w+\s+import`),
			regexp.MustCompile(`@\w+\s*\n\s*(def|class|async def)`),
			regexp.MustCompile(`if\s+__name__\s*==\s*["']__main__["']`),
			regexp.MustCompile(`requirements\.txt|setup\.py|pyproject\.toml`),
		},
		"javascript": {
			regexp.MustCompile(`\b(node|npm|yarn|bun)\b`),
			regexp.MustCompile(`(const|let|var)\s+\w+\s*=\s*(async\s+)?\(`),
			regexp.MustCompile(`=>\s*\{`),
			regexp.MustCompile(`module\.exports|require\(['"]\w+['"]\)`),
			regexp.MustCompile(`package\.json`),
		},
		"typescript": {
			regexp.MustCompile(`\btypescript\b|\bts\b`),
			regexp.MustCompile(`:\s*(string|number|boolean|any|void|never)\b`),
			regexp.MustCompile(`interface\s+\w+\s*\{`),
			regexp.MustCompile(`type\s+\w+\s*=`),
			regexp.MustCompile(`tsconfig\.json`),
		},
		"rust": {
			regexp.MustCompile(`\brust\b|\bcargo\b`),
			regexp.MustCompile(`fn\s+\w+\s*(<[^>]+>)?\s*\([^)]*\)\s*(->)?`),
			regexp.MustCompile(`\b(pub\s+)?(struct|enum|trait|impl)\s+\w+`),
			regexp.MustCompile(`let\s+(mut\s+)?\w+\s*:`),
			regexp.MustCompile(`Cargo\.toml`),
		},
		"java": {
			regexp.MustCompile(`\bjava\b`),
			regexp.MustCompile(`public\s+(static\s+)?class\s+\w+`),
			regexp.MustCompile(`(public|private|protected)\s+\w+\s+\w+\s*\(`),
			regexp.MustCompile(`@(Override|Deprecated|SuppressWarnings)`),
			regexp.MustCompile(`pom\.xml|build\.gradle`),
		},
		"c": {
			regexp.MustCompile(`#include\s*<[^>]+>`),
			regexp.MustCompile(`(int|void|char|float|double)\s+\w+\s*\(`),
			regexp.MustCompile(`malloc\s*\(|free\s*\(`),
			regexp.MustCompile(`\*\w+\s*=|&\w+`),
		},
		"cpp": {
			regexp.MustCompile(`\bc\+\+\b|\bcpp\b`),
			regexp.MustCompile(`#include\s*<(iostream|vector|string|map)>`),
			regexp.MustCompile(`std::\w+`),
			regexp.MustCompile(`class\s+\w+\s*:\s*(public|private|protected)`),
			regexp.MustCompile(`template\s*<`),
		},
		"sql": {
			regexp.MustCompile(`\bsql\b`),
			regexp.MustCompile(`\b(SELECT|INSERT|UPDATE|DELETE|CREATE|DROP|ALTER)\s+`),
			regexp.MustCompile(`\bFROM\s+\w+\s+(WHERE|JOIN|ORDER BY)`),
		},
		"shell": {
			regexp.MustCompile(`\bbash\b|\bzsh\b|\bsh\b`),
			regexp.MustCompile(`#!/bin/(ba)?sh`),
			regexp.MustCompile(`\$\{?\w+\}?`),
			regexp.MustCompile(`\|\s*grep|\|\s*awk|\|\s*sed`),
		},
		"zig": {
			regexp.MustCompile(`\bzig\b`),
			regexp.MustCompile(`const\s+\w+\s*=\s*@import`),
			regexp.MustCompile(`pub\s+fn\s+\w+`),
			regexp.MustCompile(`@as\(|@intCast\(|@ptrCast\(`),
		},
	}

	for lang, patterns := range langPatterns {
		for _, pattern := range patterns {
			if pattern.MatchString(queryLower) || pattern.MatchString(query) {
				languages[lang] = true
				break
			}
		}
	}

	// Check for explicit language mentions
	explicitLangs := map[string][]string{
		"go":         {"golang", "go language", "go code"},
		"python":     {"python", "py", "python3", "python2"},
		"javascript": {"javascript", "js", "ecmascript"},
		"typescript": {"typescript", "ts"},
		"rust":       {"rust", "rustlang"},
		"java":       {"java"},
		"c":          {" c code", " c program", "c language"},
		"cpp":        {"c++", "cpp", "cplusplus"},
		"sql":        {"sql", "mysql", "postgresql", "sqlite"},
		"shell":      {"bash", "shell", "zsh", "sh script"},
		"zig":        {"zig", "ziglang"},
	}

	for lang, keywords := range explicitLangs {
		for _, kw := range keywords {
			if strings.Contains(queryLower, kw) {
				languages[lang] = true
				break
			}
		}
	}

	result := make([]string, 0, len(languages))
	for lang := range languages {
		result = append(result, lang)
	}
	return result
}

// estimateDepth estimates the reasoning depth required for a query.
func (e *FeatureExtractor) estimateDepth(query string) int {
	queryLower := strings.ToLower(query)
	depth := 1 // Minimum depth

	// Check for complexity indicators
	complexityIndicators := []struct {
		patterns []string
		depth    int
	}{
		{[]string{"step by step", "walkthrough", "explain each"}, 2},
		{[]string{"compare", "contrast", "trade-offs", "pros and cons"}, 2},
		{[]string{"design", "architect", "plan", "strategy"}, 3},
		{[]string{"prove", "theorem", "derive", "mathematical proof"}, 4},
		{[]string{"optimize", "performance", "bottleneck"}, 3},
		{[]string{"debug", "troubleshoot", "diagnose"}, 2},
		{[]string{"refactor", "rewrite", "redesign"}, 3},
		{[]string{"implement", "build", "create from scratch"}, 2},
		{[]string{"analyze", "evaluate", "assess"}, 2},
		{[]string{"research", "investigate", "explore options"}, 3},
	}

	for _, indicator := range complexityIndicators {
		for _, pattern := range indicator.patterns {
			if strings.Contains(queryLower, pattern) {
				if indicator.depth > depth {
					depth = indicator.depth
				}
			}
		}
	}

	// Increase depth for longer queries
	wordCount := len(strings.Fields(query))
	if wordCount > 100 {
		depth++
	}
	if wordCount > 300 {
		depth++
	}

	// Increase depth if multiple code blocks
	codeBlockCount := strings.Count(query, "```")
	if codeBlockCount >= 4 {
		depth++
	}

	// Cap at 5
	if depth > 5 {
		depth = 5
	}

	return depth
}

// estimateAmbiguity estimates how ambiguous a query is (0=clear, 1=ambiguous).
func (e *FeatureExtractor) estimateAmbiguity(query string) float64 {
	queryLower := strings.ToLower(query)
	ambiguity := 0.0

	// Short queries are more ambiguous
	wordCount := len(strings.Fields(query))
	if wordCount < 5 {
		ambiguity += 0.3
	} else if wordCount < 10 {
		ambiguity += 0.1
	}

	// Vague language increases ambiguity
	vagueTerms := []string{
		"something", "stuff", "things", "etc",
		"maybe", "perhaps", "might", "could be",
		"kind of", "sort of", "like", "whatever",
		"good", "better", "best", "nice",
		"fix it", "make it work", "help",
	}
	for _, term := range vagueTerms {
		if strings.Contains(queryLower, term) {
			ambiguity += 0.1
		}
	}

	// Questions without specifics
	if strings.Contains(queryLower, "how do i") && wordCount < 8 {
		ambiguity += 0.2
	}
	if strings.Contains(queryLower, "what is") && wordCount < 6 {
		ambiguity += 0.2
	}

	// Clear indicators reduce ambiguity
	clearIndicators := []string{
		"specifically", "exactly", "precisely",
		"for example", "such as", "like this",
		"the following", "this code", "this file",
	}
	for _, indicator := range clearIndicators {
		if strings.Contains(queryLower, indicator) {
			ambiguity -= 0.15
		}
	}

	// Code blocks reduce ambiguity
	if strings.Contains(query, "```") {
		ambiguity -= 0.2
	}

	// File paths reduce ambiguity
	if strings.Contains(query, "/") && (strings.Contains(query, ".go") ||
		strings.Contains(query, ".py") || strings.Contains(query, ".ts") ||
		strings.Contains(query, ".js") || strings.Contains(query, ".rs")) {
		ambiguity -= 0.15
	}

	return clamp(ambiguity, 0, 1)
}

// hasNonASCII checks if the query contains non-ASCII characters.
func hasNonASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}
