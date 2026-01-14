package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/rand/recurse/internal/rlm/meta"
)

// Intelligent handles Claude-powered decision making for prompt analysis.
type Intelligent struct {
	metaController *meta.Controller
	models         []meta.ModelSpec
	enabled        bool
}

// IntelligentConfig configures intelligent analysis.
type IntelligentConfig struct {
	// Enabled controls whether intelligent analysis is active.
	Enabled bool

	// Models is the model catalog for routing decisions.
	Models []meta.ModelSpec
}

// NewIntelligent creates a new intelligent analyzer.
func NewIntelligent(metaCtrl *meta.Controller, cfg IntelligentConfig) *Intelligent {
	models := cfg.Models
	if len(models) == 0 {
		models = meta.DefaultModels()
	}

	return &Intelligent{
		metaController: metaCtrl,
		models:         models,
		enabled:        cfg.Enabled,
	}
}

// Analyze performs intelligent analysis of a user prompt.
func (i *Intelligent) Analyze(ctx context.Context, prompt string, contextTokens int) (*AnalysisResult, error) {
	start := time.Now()

	result := &AnalysisResult{
		OriginalPrompt: prompt,
		EnhancedPrompt: prompt,
	}

	if !i.enabled || i.metaController == nil {
		result.AnalysisTime = time.Since(start)
		return result, nil
	}

	// Get meta-controller decision
	state := meta.State{
		Task:           prompt,
		ContextTokens:  contextTokens,
		BudgetRemain:   10000,
		RecursionDepth: 0,
		MaxDepth:       5,
	}

	decision, err := i.metaController.Decide(ctx, state)
	if err != nil {
		slog.Warn("RLM meta-controller failed, continuing without orchestration", "error", err)
		result.AnalysisTime = time.Since(start)
		return result, nil
	}

	result.Decision = decision

	// Analyze context needs based on the prompt
	result.ContextNeeds = i.analyzeContextNeeds(prompt)

	// Determine routing
	result.Routing = i.determineRouting(prompt, decision)

	// Check if decomposition is recommended
	if decision.Action == meta.ActionDecompose {
		result.ShouldDecompose = true
		result.Subtasks = i.createSubtasks(prompt, decision)
	}

	result.AnalysisTime = time.Since(start)
	result.ModelUsed = "meta-controller"

	slog.Info("RLM analysis complete",
		"action", decision.Action,
		"reasoning", decision.Reasoning,
		"context_needs", len(result.ContextNeeds.FilePatterns)+len(result.ContextNeeds.SearchQueries),
		"should_decompose", result.ShouldDecompose,
		"duration", result.AnalysisTime)

	return result, nil
}

// analyzeContextNeeds identifies what context would help with the task.
func (i *Intelligent) analyzeContextNeeds(prompt string) *ContextNeeds {
	needs := &ContextNeeds{
		FilePatterns:         []string{},
		SearchQueries:        []string{},
		ConceptsToUnderstand: []string{},
		MemoryQueries:        []string{},
		Priority:             5,
	}

	promptLower := strings.ToLower(prompt)

	// Detect code-related requests
	if containsAny(promptLower, []string{"function", "method", "class", "struct", "interface", "type"}) {
		needs.SearchQueries = append(needs.SearchQueries, extractCodeTerms(prompt)...)
		needs.Priority = 7
	}

	// Detect file-related requests
	if containsAny(promptLower, []string{"file", "module", "package", "import"}) {
		needs.FilePatterns = append(needs.FilePatterns, "**/*.go", "**/*.ts", "**/*.py")
		needs.Priority = 8
	}

	// Detect bug/error investigation
	if containsAny(promptLower, []string{"bug", "error", "fix", "broken", "failing", "crash"}) {
		needs.SearchQueries = append(needs.SearchQueries, "error", "panic", "return err")
		needs.ConceptsToUnderstand = append(needs.ConceptsToUnderstand, "error handling patterns")
		needs.Priority = 9
	}

	// Detect architecture/design questions
	if containsAny(promptLower, []string{"architecture", "design", "structure", "how does", "explain"}) {
		needs.ConceptsToUnderstand = append(needs.ConceptsToUnderstand, "system architecture")
		needs.MemoryQueries = append(needs.MemoryQueries, "design decisions", "architecture")
		needs.Priority = 6
	}

	// Detect refactoring requests
	if containsAny(promptLower, []string{"refactor", "improve", "optimize", "clean up"}) {
		needs.Priority = 7
		needs.ConceptsToUnderstand = append(needs.ConceptsToUnderstand, "existing patterns", "code conventions")
	}

	// Detect test-related requests
	if containsAny(promptLower, []string{"test", "spec", "coverage", "mock"}) {
		needs.FilePatterns = append(needs.FilePatterns, "**/*_test.go", "**/*.test.ts", "**/test_*.py")
		needs.SearchQueries = append(needs.SearchQueries, "func Test")
	}

	return needs
}

// determineRouting decides how to route the task to models.
func (i *Intelligent) determineRouting(prompt string, decision *meta.Decision) *TaskRouting {
	routing := &TaskRouting{
		PrimaryTier:   meta.TierBalanced,
		SubtaskRoutes: make(map[string]string),
	}

	promptLower := strings.ToLower(prompt)

	// Determine primary tier based on task characteristics
	switch {
	case containsAny(promptLower, []string{"prove", "theorem", "mathematical", "logic", "formal"}):
		routing.PrimaryTier = meta.TierReasoning
		routing.Reasoning = "Task requires deep reasoning/mathematical analysis"

	case containsAny(promptLower, []string{"analyze", "refactor", "architect", "design complex"}):
		routing.PrimaryTier = meta.TierPowerful
		routing.Reasoning = "Task requires complex analysis and deep understanding"

	case containsAny(promptLower, []string{"quick", "simple", "just", "only"}):
		routing.PrimaryTier = meta.TierFast
		routing.Reasoning = "Task is simple and can be handled quickly"

	default:
		routing.PrimaryTier = meta.TierBalanced
		routing.Reasoning = "Task requires moderate complexity handling"
	}

	// Find best model for the tier
	for _, m := range i.models {
		if m.Tier == routing.PrimaryTier {
			routing.PrimaryModel = m.ID
			break
		}
	}

	// Set subtask routing defaults
	routing.SubtaskRoutes["code_search"] = i.findModelForTier(meta.TierFast)
	routing.SubtaskRoutes["analysis"] = i.findModelForTier(meta.TierBalanced)
	routing.SubtaskRoutes["generation"] = i.findModelForTier(meta.TierBalanced)
	routing.SubtaskRoutes["reasoning"] = i.findModelForTier(meta.TierReasoning)
	routing.SubtaskRoutes["synthesis"] = i.findModelForTier(meta.TierPowerful)

	return routing
}

// createSubtasks decomposes a task into subtasks based on the decision.
func (i *Intelligent) createSubtasks(prompt string, decision *meta.Decision) []Subtask {
	var subtasks []Subtask

	// Use chunks from decision if available
	if len(decision.Params.Chunks) > 0 {
		for idx, chunk := range decision.Params.Chunks {
			subtask := Subtask{
				ID:              fmt.Sprintf("subtask-%d", idx+1),
				Description:     chunk,
				Type:            string(decision.Params.Strategy),
				RecommendedTier: meta.TierBalanced,
				Priority:        len(decision.Params.Chunks) - idx,
			}
			subtask.RecommendedModel = i.findModelForTier(subtask.RecommendedTier)
			subtasks = append(subtasks, subtask)
		}
	} else {
		// Create default decomposition based on strategy
		switch decision.Params.Strategy {
		case meta.StrategyFile:
			subtasks = append(subtasks, Subtask{
				ID:              "gather-files",
				Description:     "Identify and read relevant files",
				Type:            "code_search",
				RecommendedTier: meta.TierFast,
				Priority:        10,
			})
			subtasks = append(subtasks, Subtask{
				ID:              "analyze-files",
				Description:     "Analyze file contents",
				Type:            "analysis",
				RecommendedTier: meta.TierBalanced,
				Dependencies:    []string{"gather-files"},
				Priority:        5,
			})

		case meta.StrategyFunction:
			subtasks = append(subtasks, Subtask{
				ID:              "find-functions",
				Description:     "Locate relevant functions",
				Type:            "code_search",
				RecommendedTier: meta.TierFast,
				Priority:        10,
			})
			subtasks = append(subtasks, Subtask{
				ID:              "analyze-functions",
				Description:     "Analyze function implementations",
				Type:            "analysis",
				RecommendedTier: meta.TierBalanced,
				Dependencies:    []string{"find-functions"},
				Priority:        5,
			})

		case meta.StrategyConcept:
			subtasks = append(subtasks, Subtask{
				ID:              "understand-concepts",
				Description:     "Build understanding of relevant concepts",
				Type:            "reasoning",
				RecommendedTier: meta.TierReasoning,
				Priority:        10,
			})
			subtasks = append(subtasks, Subtask{
				ID:              "apply-concepts",
				Description:     "Apply understanding to the task",
				Type:            "generation",
				RecommendedTier: meta.TierBalanced,
				Dependencies:    []string{"understand-concepts"},
				Priority:        5,
			})
		}

		// Always add synthesis if we have subtasks
		if len(subtasks) > 0 {
			deps := make([]string, len(subtasks))
			for idx, st := range subtasks {
				deps[idx] = st.ID
			}
			subtasks = append(subtasks, Subtask{
				ID:              "synthesize",
				Description:     "Combine results into final answer",
				Type:            "synthesis",
				RecommendedTier: meta.TierPowerful,
				Dependencies:    deps,
				Priority:        1,
			})
		}
	}

	// Assign models to subtasks
	for idx := range subtasks {
		if subtasks[idx].RecommendedModel == "" {
			subtasks[idx].RecommendedModel = i.findModelForTier(subtasks[idx].RecommendedTier)
		}
	}

	return subtasks
}

// findModelForTier finds a model ID for the given tier.
func (i *Intelligent) findModelForTier(tier meta.ModelTier) string {
	for _, m := range i.models {
		if m.Tier == tier {
			return m.ID
		}
	}
	if len(i.models) > 0 {
		return i.models[0].ID
	}
	return "anthropic/claude-haiku-4.5"
}

// IsEnabled returns whether intelligent analysis is enabled.
func (i *Intelligent) IsEnabled() bool {
	return i.enabled
}

// SetEnabled enables or disables intelligent analysis.
func (i *Intelligent) SetEnabled(enabled bool) {
	i.enabled = enabled
}

// Helper functions

func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func extractCodeTerms(prompt string) []string {
	terms := []string{}
	words := strings.Fields(prompt)
	for _, word := range words {
		// Look for CamelCase or snake_case terms
		if len(word) > 3 && (strings.Contains(word, "_") || hasUpperInMiddle(word)) {
			terms = append(terms, word)
		}
	}
	return terms
}

func hasUpperInMiddle(s string) bool {
	if len(s) < 2 {
		return false
	}
	for idx := 1; idx < len(s); idx++ {
		if s[idx] >= 'A' && s[idx] <= 'Z' {
			return true
		}
	}
	return false
}
