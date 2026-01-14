package learning

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Applier retrieves and applies learned knowledge to new tasks.
type Applier struct {
	store *Store

	// Configuration
	cfg ApplierConfig
}

// ApplierConfig configures the knowledge applier.
type ApplierConfig struct {
	// MaxFacts limits relevant facts returned.
	MaxFacts int

	// MaxPatterns limits applicable patterns returned.
	MaxPatterns int

	// MaxConstraints limits constraints returned.
	MaxConstraints int

	// MinConfidence filters out low-confidence items.
	MinConfidence float64

	// ContextMaxTokens limits total context additions.
	ContextMaxTokens int
}

// NewApplier creates a new knowledge applier.
func NewApplier(store *Store, cfg ApplierConfig) *Applier {
	// Apply defaults
	if cfg.MaxFacts == 0 {
		cfg.MaxFacts = 10
	}
	if cfg.MaxPatterns == 0 {
		cfg.MaxPatterns = 5
	}
	if cfg.MaxConstraints == 0 {
		cfg.MaxConstraints = 10
	}
	if cfg.MinConfidence == 0 {
		cfg.MinConfidence = 0.3
	}
	if cfg.ContextMaxTokens == 0 {
		cfg.ContextMaxTokens = 2000
	}

	return &Applier{
		store: store,
		cfg:   cfg,
	}
}

// Apply retrieves relevant knowledge for a query and domain.
func (a *Applier) Apply(ctx context.Context, query string, domain string, projectPath string) (*ApplyResult, error) {
	start := time.Now()
	result := &ApplyResult{}

	// Get relevant facts
	facts, err := a.getRelevantFacts(ctx, query, domain)
	if err != nil {
		return nil, fmt.Errorf("get facts: %w", err)
	}
	result.RelevantFacts = facts

	// Get applicable patterns
	patterns, err := a.getApplicablePatterns(ctx, query, domain)
	if err != nil {
		return nil, fmt.Errorf("get patterns: %w", err)
	}
	result.ApplicablePatterns = patterns

	// Get preferences (cascading: project → domain → global)
	prefs, err := a.getPreferences(ctx, domain, projectPath)
	if err != nil {
		return nil, fmt.Errorf("get preferences: %w", err)
	}
	result.Preferences = prefs

	// Get constraints
	constraints, err := a.getConstraints(ctx, query, domain)
	if err != nil {
		return nil, fmt.Errorf("get constraints: %w", err)
	}
	result.Constraints = constraints

	// Generate context additions
	result.ContextAdditions = a.generateContextAdditions(result)

	// Calculate total confidence
	result.TotalConfidence = a.calculateTotalConfidence(result)

	result.ProcessingTime = time.Since(start)

	return result, nil
}

// getRelevantFacts retrieves facts relevant to the query.
func (a *Applier) getRelevantFacts(ctx context.Context, query string, domain string) ([]*LearnedFact, error) {
	// Search by content similarity
	facts, err := a.store.SearchFacts(ctx, query, a.cfg.MaxFacts*2)
	if err != nil {
		return nil, err
	}

	// Filter and rank
	var relevant []*LearnedFact
	for _, fact := range facts {
		if fact.Confidence < a.cfg.MinConfidence {
			continue
		}
		// Boost domain-matching facts
		if domain != "" && fact.Domain == domain {
			fact.Confidence *= 1.2
			if fact.Confidence > 1.0 {
				fact.Confidence = 1.0
			}
		}
		relevant = append(relevant, fact)
		if len(relevant) >= a.cfg.MaxFacts {
			break
		}
	}

	// Update access counts
	for _, fact := range relevant {
		fact.AccessCount++
		fact.LastAccessed = time.Now()
		// Fire and forget update
		go func(f *LearnedFact) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			a.store.UpdateFact(ctx, f)
		}(fact)
	}

	return relevant, nil
}

// getApplicablePatterns retrieves patterns that may apply.
func (a *Applier) getApplicablePatterns(ctx context.Context, query string, domain string) ([]*LearnedPattern, error) {
	// List patterns for the domain
	patterns, err := a.store.ListPatterns(ctx, "", a.cfg.MaxPatterns*3)
	if err != nil {
		return nil, err
	}

	// Filter by domain and success rate
	var applicable []*LearnedPattern
	for _, pattern := range patterns {
		if pattern.SuccessRate < a.cfg.MinConfidence {
			continue
		}

		// Check domain match
		domainMatch := domain == "" || len(pattern.Domains) == 0
		for _, d := range pattern.Domains {
			if d == domain {
				domainMatch = true
				break
			}
		}
		if !domainMatch {
			continue
		}

		// Check trigger relevance (simple keyword match)
		if pattern.Trigger != "" && !containsKeywords(query, pattern.Trigger) {
			continue
		}

		applicable = append(applicable, pattern)
		if len(applicable) >= a.cfg.MaxPatterns {
			break
		}
	}

	return applicable, nil
}

// getPreferences retrieves preferences with cascading scopes.
func (a *Applier) getPreferences(ctx context.Context, domain string, projectPath string) ([]*UserPreference, error) {
	var prefs []*UserPreference

	// Project-specific preferences (highest priority)
	if projectPath != "" {
		projectPrefs, err := a.store.ListPreferences(ctx, ScopeProject, projectPath)
		if err == nil {
			prefs = append(prefs, projectPrefs...)
		}
	}

	// Domain-specific preferences
	if domain != "" {
		domainPrefs, err := a.store.ListPreferences(ctx, ScopeDomain, domain)
		if err == nil {
			prefs = append(prefs, domainPrefs...)
		}
	}

	// Global preferences (lowest priority)
	globalPrefs, err := a.store.ListPreferences(ctx, ScopeGlobal, "")
	if err == nil {
		prefs = append(prefs, globalPrefs...)
	}

	// Deduplicate by key (higher scope wins)
	seen := make(map[string]bool)
	var unique []*UserPreference
	for _, pref := range prefs {
		if seen[pref.Key] {
			continue
		}
		if pref.Confidence < a.cfg.MinConfidence {
			continue
		}
		seen[pref.Key] = true
		unique = append(unique, pref)
	}

	return unique, nil
}

// getConstraints retrieves relevant constraints.
func (a *Applier) getConstraints(ctx context.Context, query string, domain string) ([]*LearnedConstraint, error) {
	constraints, err := a.store.ListConstraints(ctx, domain, a.cfg.MinConfidence)
	if err != nil {
		return nil, err
	}

	// Filter by trigger relevance
	var relevant []*LearnedConstraint
	for _, c := range constraints {
		// Include if no trigger or trigger matches
		if c.Trigger == "" || containsKeywords(query, c.Trigger) {
			relevant = append(relevant, c)
		}
		if len(relevant) >= a.cfg.MaxConstraints {
			break
		}
	}

	return relevant, nil
}

// generateContextAdditions creates text to add to LLM context.
func (a *Applier) generateContextAdditions(result *ApplyResult) []string {
	var additions []string
	tokenCount := 0

	// Add constraints (highest priority - things to avoid)
	for _, c := range result.Constraints {
		text := formatConstraint(c)
		tokens := estimateTokens(text)
		if tokenCount+tokens > a.cfg.ContextMaxTokens {
			break
		}
		additions = append(additions, text)
		tokenCount += tokens
	}

	// Add preferences
	for _, p := range result.Preferences {
		text := formatPreference(p)
		tokens := estimateTokens(text)
		if tokenCount+tokens > a.cfg.ContextMaxTokens {
			break
		}
		additions = append(additions, text)
		tokenCount += tokens
	}

	// Add relevant facts
	for _, f := range result.RelevantFacts {
		text := formatFact(f)
		tokens := estimateTokens(text)
		if tokenCount+tokens > a.cfg.ContextMaxTokens {
			break
		}
		additions = append(additions, text)
		tokenCount += tokens
	}

	// Add pattern hints
	for _, p := range result.ApplicablePatterns {
		text := formatPattern(p)
		tokens := estimateTokens(text)
		if tokenCount+tokens > a.cfg.ContextMaxTokens {
			break
		}
		additions = append(additions, text)
		tokenCount += tokens
	}

	return additions
}

// calculateTotalConfidence aggregates confidence across all knowledge.
func (a *Applier) calculateTotalConfidence(result *ApplyResult) float64 {
	if result.IsEmpty() {
		return 0
	}

	var sum float64
	var count int

	for _, f := range result.RelevantFacts {
		sum += f.Confidence
		count++
	}
	for _, p := range result.ApplicablePatterns {
		sum += p.SuccessRate
		count++
	}
	for _, p := range result.Preferences {
		sum += p.Confidence
		count++
	}
	for _, c := range result.Constraints {
		sum += c.Severity
		count++
	}

	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// Helper functions

func containsKeywords(text, keywords string) bool {
	textLower := strings.ToLower(text)
	for _, kw := range strings.Fields(strings.ToLower(keywords)) {
		if len(kw) >= 3 && strings.Contains(textLower, kw) {
			return true
		}
	}
	return false
}

func formatConstraint(c *LearnedConstraint) string {
	switch c.ConstraintType {
	case ConstraintAvoid:
		if c.Correction != "" {
			return fmt.Sprintf("[AVOID] %s Instead: %s", c.Description, c.Correction)
		}
		return fmt.Sprintf("[AVOID] %s", c.Description)
	case ConstraintRequire:
		return fmt.Sprintf("[REQUIRED] %s", c.Description)
	case ConstraintPrefer:
		return fmt.Sprintf("[PREFER] %s", c.Description)
	case ConstraintFormat:
		return fmt.Sprintf("[FORMAT] %s", c.Description)
	case ConstraintSecurity:
		return fmt.Sprintf("[SECURITY] %s", c.Description)
	default:
		return fmt.Sprintf("[CONSTRAINT] %s", c.Description)
	}
}

func formatPreference(p *UserPreference) string {
	return fmt.Sprintf("[PREFERENCE] %s: %v", p.Key, p.Value)
}

func formatFact(f *LearnedFact) string {
	if f.Domain != "" {
		return fmt.Sprintf("[FACT:%s] %s", f.Domain, f.Content)
	}
	return fmt.Sprintf("[FACT] %s", f.Content)
}

func formatPattern(p *LearnedPattern) string {
	return fmt.Sprintf("[PATTERN:%s] %s - %s", p.PatternType, p.Name, p.Trigger)
}

func estimateTokens(text string) int {
	// Rough estimate: ~4 chars per token
	return len(text) / 4
}
