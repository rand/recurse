package rlm

import (
	"regexp"
	"strings"
)

// TaskType represents the classification of a task.
type TaskType string

const (
	// TaskTypeComputational requires exact computation (counting, summing, etc.)
	TaskTypeComputational TaskType = "computational"

	// TaskTypeRetrieval requires finding specific information.
	TaskTypeRetrieval TaskType = "retrieval"

	// TaskTypeAnalytical requires reasoning across multiple pieces of information.
	TaskTypeAnalytical TaskType = "analytical"

	// TaskTypeTransformational requires reformatting or restructuring content.
	TaskTypeTransformational TaskType = "transformational"

	// TaskTypeUnknown when classification confidence is too low.
	TaskTypeUnknown TaskType = "unknown"
)

// Classification represents the result of classifying a task.
type Classification struct {
	Type       TaskType
	Confidence float64
	Signals    []string
}

// TaskClassifier classifies queries to determine optimal execution mode.
type TaskClassifier struct {
	computationalKeywords map[string]float64
	retrievalKeywords     map[string]float64
	analyticalKeywords    map[string]float64
	patterns              []queryPattern
}

type queryPattern struct {
	regex      *regexp.Regexp
	taskType   TaskType
	confidence float64
	name       string
}

// NewTaskClassifier creates a new classifier with default rules.
func NewTaskClassifier() *TaskClassifier {
	c := &TaskClassifier{
		computationalKeywords: map[string]float64{
			"how many":    0.9,
			"count":       0.9,
			"number of":   0.8,
			"total":       0.85,
			"sum":         0.9,
			"average":     0.85,
			"mean":        0.85,
			"add up":      0.8,
			"calculate":   0.8,
			"compute":     0.8,
			"list all":    0.7,
			"find all":    0.7,
			"all the":     0.5,
			"every":       0.5,
			"each":        0.4,
			"how often":   0.85,
			"frequency":   0.8,
			"occurrences": 0.9,
			"instances":   0.8,
		},
		retrievalKeywords: map[string]float64{
			"what is the":   0.7,
			"what was the":  0.7,
			"what are the":  0.6,
			"find the":      0.6,
			"where is":      0.7,
			"where was":     0.7,
			"who is":        0.7,
			"who was":       0.7,
			"when did":      0.7,
			"when was":      0.7,
			"which":         0.5,
			"the code":      0.8,
			"the password":  0.9,
			"the key":       0.7,
			"the secret":    0.85,
			"the id":        0.8,
			"the name":      0.6,
			"the number":    0.5,
			"mentioned":     0.6,
			"stated":        0.6,
			"according to":  0.6,
			"access code":   0.9,
			"secret code":   0.9,
		},
		analyticalKeywords: map[string]float64{
			"relationship":  0.8,
			"related":       0.7,
			"compare":       0.7,
			"comparison":    0.7,
			"difference":    0.6,
			"differ":        0.6,
			"why":           0.5,
			"because":       0.4,
			"reason":        0.5,
			"cause":         0.5,
			"effect":        0.5,
			"after":         0.4,
			"before":        0.4,
			"between":       0.5,
			"work with":     0.7,
			"worked with":   0.7,
			"collaborate":   0.7,
			"collaborated":  0.7,
			"connect":       0.6,
			"connected":     0.6,
		},
	}

	// Compile regex patterns
	c.patterns = []queryPattern{
		// Counting patterns - high confidence
		{
			regex:      regexp.MustCompile(`(?i)how many (times|occurrences|instances)`),
			taskType:   TaskTypeComputational,
			confidence: 0.95,
			name:       "how_many_times",
		},
		{
			regex:      regexp.MustCompile(`(?i)count (the |all )?(number of )?`),
			taskType:   TaskTypeComputational,
			confidence: 0.9,
			name:       "count_the",
		},
		{
			regex:      regexp.MustCompile(`(?i)how often (does|did|do|is|are|was|were)`),
			taskType:   TaskTypeComputational,
			confidence: 0.9,
			name:       "how_often",
		},
		{
			regex:      regexp.MustCompile(`(?i)appears? (in|throughout)`),
			taskType:   TaskTypeComputational,
			confidence: 0.85,
			name:       "appears_in",
		},

		// Summing patterns
		{
			regex:      regexp.MustCompile(`(?i)(total|sum|add up).{0,30}(\$|dollars?|amount|sales|cost|price|value)`),
			taskType:   TaskTypeComputational,
			confidence: 0.9,
			name:       "sum_money",
		},
		{
			regex:      regexp.MustCompile(`(?i)what is the (total|sum|combined)`),
			taskType:   TaskTypeComputational,
			confidence: 0.85,
			name:       "what_is_total",
		},
		{
			regex:      regexp.MustCompile(`(?i)(across|from) all (regions?|departments?|teams?|areas?)`),
			taskType:   TaskTypeComputational,
			confidence: 0.8,
			name:       "across_all",
		},

		// Retrieval patterns - specific information
		{
			regex:      regexp.MustCompile(`(?i)what is the .{1,30}(code|key|password|secret|id|identifier)`),
			taskType:   TaskTypeRetrieval,
			confidence: 0.95,
			name:       "what_is_code",
		},
		{
			regex:      regexp.MustCompile(`(?i)(find|locate|identify) the .{1,20}(mentioned|stated|given|provided)`),
			taskType:   TaskTypeRetrieval,
			confidence: 0.85,
			name:       "find_mentioned",
		},
		{
			regex:      regexp.MustCompile(`(?i)the (secret |access |authorization )?(code|key|password)`),
			taskType:   TaskTypeRetrieval,
			confidence: 0.9,
			name:       "the_secret_code",
		},
		{
			regex:      regexp.MustCompile(`(?i)answer with (just |only )?(the )?(number|code|name|value)`),
			taskType:   TaskTypeRetrieval,
			confidence: 0.7,
			name:       "answer_with_just",
		},

		// Analytical patterns - relationships
		{
			regex:      regexp.MustCompile(`(?i)did .{1,30} (work|meet|talk|speak|collaborate|interact) with`),
			taskType:   TaskTypeAnalytical,
			confidence: 0.85,
			name:       "did_work_with",
		},
		{
			regex:      regexp.MustCompile(`(?i)(is|are|was|were) .{1,30} (related|connected|linked) to`),
			taskType:   TaskTypeAnalytical,
			confidence: 0.85,
			name:       "is_related_to",
		},
		{
			regex:      regexp.MustCompile(`(?i)any .{0,20}(relationship|connection|link) between`),
			taskType:   TaskTypeAnalytical,
			confidence: 0.85,
			name:       "any_relationship",
		},
		{
			regex:      regexp.MustCompile(`(?i)answer.{0,10}(yes|no)[^a-z]`),
			taskType:   TaskTypeAnalytical,
			confidence: 0.6,
			name:       "yes_no_answer",
		},
	}

	return c
}

// Classify analyzes a query and optional context to determine task type.
func (c *TaskClassifier) Classify(query string, contexts []ContextSource) Classification {
	scores := make(map[TaskType]float64)
	var signals []string

	queryLower := strings.ToLower(query)

	// 1. Keyword matching
	for keyword, weight := range c.computationalKeywords {
		if strings.Contains(queryLower, keyword) {
			scores[TaskTypeComputational] += weight
			signals = append(signals, "keyword:"+keyword)
		}
	}

	for keyword, weight := range c.retrievalKeywords {
		if strings.Contains(queryLower, keyword) {
			scores[TaskTypeRetrieval] += weight
			signals = append(signals, "keyword:"+keyword)
		}
	}

	for keyword, weight := range c.analyticalKeywords {
		if strings.Contains(queryLower, keyword) {
			scores[TaskTypeAnalytical] += weight
			signals = append(signals, "keyword:"+keyword)
		}
	}

	// 2. Pattern matching (higher confidence than keywords)
	for _, pattern := range c.patterns {
		if pattern.regex.MatchString(query) {
			scores[pattern.taskType] += pattern.confidence
			signals = append(signals, "pattern:"+pattern.name)
		}
	}

	// 3. Context analysis (if available)
	if len(contexts) > 0 {
		ctxSignals := c.analyzeContext(contexts[0].Content)
		if ctxSignals.numericDensity > 0.005 {
			scores[TaskTypeComputational] += 0.3
			signals = append(signals, "context:high_numeric_density")
		}
		if ctxSignals.hasRepetitiveStructure {
			scores[TaskTypeComputational] += 0.2
			signals = append(signals, "context:repetitive_structure")
		}
		if ctxSignals.hasUniqueIdentifiers {
			scores[TaskTypeRetrieval] += 0.2
			signals = append(signals, "context:unique_identifiers")
		}
	}

	// 4. Select best type
	winner, confidence := c.selectBestType(scores)

	// 5. Apply confidence threshold
	if confidence < 0.5 {
		winner = TaskTypeUnknown
	}

	return Classification{
		Type:       winner,
		Confidence: confidence,
		Signals:    signals,
	}
}

// contextSignals contains signals extracted from context analysis.
type contextSignals struct {
	numericDensity        float64 // Numbers per character
	hasRepetitiveStructure bool   // Similar patterns repeated
	hasUniqueIdentifiers   bool   // Codes, IDs, etc.
}

// analyzeContext extracts classification signals from context content.
func (c *TaskClassifier) analyzeContext(content string) contextSignals {
	signals := contextSignals{}

	if len(content) == 0 {
		return signals
	}

	// Count numeric occurrences
	numericPattern := regexp.MustCompile(`\d+`)
	matches := numericPattern.FindAllString(content, -1)
	signals.numericDensity = float64(len(matches)) / float64(len(content))

	// Check for repetitive structure (same sentence patterns)
	lines := strings.Split(content, ".")
	if len(lines) > 10 {
		// Sample first few sentences and check similarity
		patterns := make(map[string]int)
		for i := 0; i < min(20, len(lines)); i++ {
			// Extract rough pattern (first 3 words)
			words := strings.Fields(strings.TrimSpace(lines[i]))
			if len(words) >= 3 {
				pattern := strings.Join(words[:3], " ")
				patterns[pattern]++
			}
		}
		// If any pattern appears 3+ times, it's repetitive
		for _, count := range patterns {
			if count >= 3 {
				signals.hasRepetitiveStructure = true
				break
			}
		}
	}

	// Check for unique identifiers (CODE-XXXX, ID-XXX, etc.)
	idPattern := regexp.MustCompile(`(?i)(CODE|ID|KEY|REF|NUM)[- ]?\d{3,}`)
	if idPattern.MatchString(content) {
		signals.hasUniqueIdentifiers = true
	}

	return signals
}

// selectBestType picks the task type with highest score and normalizes confidence.
func (c *TaskClassifier) selectBestType(scores map[TaskType]float64) (TaskType, float64) {
	if len(scores) == 0 {
		return TaskTypeUnknown, 0.0
	}

	var bestType TaskType
	var bestScore float64
	var totalScore float64

	for taskType, score := range scores {
		totalScore += score
		if score > bestScore {
			bestScore = score
			bestType = taskType
		}
	}

	if totalScore == 0 {
		return TaskTypeUnknown, 0.0
	}

	// Normalize confidence: winner's share of total score
	// Also cap at 1.0 and boost if winner is significantly ahead
	confidence := bestScore / totalScore

	// If winner has >2x the second place, boost confidence
	var secondBest float64
	for taskType, score := range scores {
		if taskType != bestType && score > secondBest {
			secondBest = score
		}
	}
	if secondBest > 0 && bestScore/secondBest > 2.0 {
		confidence = min(1.0, confidence*1.2)
	}

	// Scale to reasonable range (raw scores can exceed 1.0)
	if bestScore > 1.5 {
		confidence = min(1.0, 0.7+confidence*0.3)
	}

	return bestType, confidence
}

// RecommendedMode returns the execution mode for a given classification.
func (c *TaskClassifier) RecommendedMode(class Classification, contextTokens int, replAvailable bool) ExecutionMode {
	// High-confidence computational → RLM even for smaller contexts
	if class.Type == TaskTypeComputational && class.Confidence > 0.7 {
		if contextTokens >= 500 && replAvailable {
			return ModeRLM
		}
	}

	// High-confidence retrieval → Direct even for larger contexts
	if class.Type == TaskTypeRetrieval && class.Confidence > 0.7 {
		return ModeDirecte
	}

	// Analytical with large context → RLM for systematic search
	if class.Type == TaskTypeAnalytical && contextTokens >= 8000 && replAvailable {
		return ModeRLM
	}

	// Unknown or low confidence → use size-based default
	// This will be handled by the caller's threshold logic
	return ""
}
