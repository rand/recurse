package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rand/recurse/internal/memory/hypergraph"
)

// Store provides persistent storage for learned knowledge.
// It wraps the hypergraph store with learning-specific operations.
type Store struct {
	graph *hypergraph.Store
}

// NewStore creates a new learning store backed by the given hypergraph store.
func NewStore(graph *hypergraph.Store) *Store {
	return &Store{graph: graph}
}

// Node subtypes for learned knowledge
const (
	SubtypeLearnedFact       = "learned_fact"
	SubtypeLearnedPattern    = "learned_pattern"
	SubtypeUserPreference    = "user_preference"
	SubtypeLearnedConstraint = "learned_constraint"
	SubtypeLearningSignal    = "learning_signal"
)

// StoreFact persists a learned fact.
func (s *Store) StoreFact(ctx context.Context, fact *LearnedFact) error {
	if fact.ID == "" {
		fact.ID = uuid.New().String()
	}
	if fact.CreatedAt.IsZero() {
		fact.CreatedAt = time.Now()
	}

	metadata, err := json.Marshal(fact.Metadata)
	if err != nil {
		metadata = []byte("{}")
	}

	node := &hypergraph.Node{
		ID:          fact.ID,
		Type:        hypergraph.NodeTypeFact,
		Subtype:     SubtypeLearnedFact,
		Content:     fact.Content,
		Tier:        hypergraph.TierLongterm,
		Confidence:  fact.Confidence,
		Embedding:   serializeEmbedding(fact.Embedding),
		AccessCount: fact.AccessCount,
		LastAccessed: timeToPtr(fact.LastAccessed),
		Metadata:    metadata,
		Provenance: mustMarshal(map[string]interface{}{
			"domain":         fact.Domain,
			"source":         fact.Source,
			"success_count":  fact.SuccessCount,
			"failure_count":  fact.FailureCount,
			"last_validated": fact.LastValidated,
		}),
		CreatedAt: fact.CreatedAt,
		UpdatedAt: time.Now(),
	}

	return s.graph.CreateNode(ctx, node)
}

// GetFact retrieves a learned fact by ID.
func (s *Store) GetFact(ctx context.Context, id string) (*LearnedFact, error) {
	node, err := s.graph.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	if node == nil || node.Subtype != SubtypeLearnedFact {
		return nil, nil
	}
	return nodeToFact(node)
}

// SearchFacts searches for facts by content similarity.
func (s *Store) SearchFacts(ctx context.Context, query string, limit int) ([]*LearnedFact, error) {
	results, err := s.graph.Search(ctx, query, hypergraph.SearchOptions{
		Types:   []hypergraph.NodeType{hypergraph.NodeTypeFact},
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}

	facts := make([]*LearnedFact, 0, len(results))
	for _, r := range results {
		if r.Node.Subtype != SubtypeLearnedFact {
			continue
		}
		fact, err := nodeToFact(r.Node)
		if err != nil {
			continue
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

// ListFacts lists learned facts with optional filtering.
func (s *Store) ListFacts(ctx context.Context, domain string, minConfidence float64, limit int) ([]*LearnedFact, error) {
	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:         []hypergraph.NodeType{hypergraph.NodeTypeFact},
		Subtypes:      []string{SubtypeLearnedFact},
		MinConfidence: minConfidence,
		Limit:         limit,
	})
	if err != nil {
		return nil, err
	}

	facts := make([]*LearnedFact, 0, len(nodes))
	for _, node := range nodes {
		fact, err := nodeToFact(node)
		if err != nil {
			continue
		}
		if domain != "" && fact.Domain != domain {
			continue
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

// UpdateFact updates an existing fact.
func (s *Store) UpdateFact(ctx context.Context, fact *LearnedFact) error {
	node, err := s.graph.GetNode(ctx, fact.ID)
	if err != nil {
		return fmt.Errorf("get node for update: %w", err)
	}
	if node == nil {
		return fmt.Errorf("fact not found: %s", fact.ID)
	}

	metadata, _ := json.Marshal(fact.Metadata)

	node.Content = fact.Content
	node.Confidence = fact.Confidence
	node.Metadata = metadata
	node.Embedding = serializeEmbedding(fact.Embedding)
	node.AccessCount = fact.AccessCount
	node.LastAccessed = timeToPtr(fact.LastAccessed)
	node.Provenance = mustMarshal(map[string]interface{}{
		"domain":         fact.Domain,
		"source":         fact.Source,
		"success_count":  fact.SuccessCount,
		"failure_count":  fact.FailureCount,
		"last_validated": fact.LastValidated,
	})

	return s.graph.UpdateNode(ctx, node)
}

// DeleteFact removes a fact.
func (s *Store) DeleteFact(ctx context.Context, id string) error {
	return s.graph.DeleteNode(ctx, id)
}

// StorePattern persists a learned pattern.
func (s *Store) StorePattern(ctx context.Context, pattern *LearnedPattern) error {
	if pattern.ID == "" {
		pattern.ID = uuid.New().String()
	}
	if pattern.CreatedAt.IsZero() {
		pattern.CreatedAt = time.Now()
	}

	metadata, _ := json.Marshal(map[string]interface{}{
		"pattern_type": pattern.PatternType,
		"trigger":      pattern.Trigger,
		"template":     pattern.Template,
		"examples":     pattern.Examples,
		"domains":      pattern.Domains,
		"success_rate": pattern.SuccessRate,
		"usage_count":  pattern.UsageCount,
		"last_used":    pattern.LastUsed,
		"extra":        pattern.Metadata,
	})

	node := &hypergraph.Node{
		ID:         pattern.ID,
		Type:       hypergraph.NodeTypeExperience,
		Subtype:    SubtypeLearnedPattern,
		Content:    pattern.Name,
		Tier:       hypergraph.TierLongterm,
		Confidence: pattern.SuccessRate,
		Embedding:  serializeEmbedding(pattern.Embedding),
		Metadata:   metadata,
		CreatedAt:  pattern.CreatedAt,
		UpdatedAt:  time.Now(),
	}

	return s.graph.CreateNode(ctx, node)
}

// GetPattern retrieves a pattern by ID.
func (s *Store) GetPattern(ctx context.Context, id string) (*LearnedPattern, error) {
	node, err := s.graph.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	if node == nil || node.Subtype != SubtypeLearnedPattern {
		return nil, nil
	}
	return nodeToPattern(node)
}

// ListPatterns lists learned patterns.
func (s *Store) ListPatterns(ctx context.Context, patternType PatternType, limit int) ([]*LearnedPattern, error) {
	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeExperience},
		Subtypes: []string{SubtypeLearnedPattern},
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}

	patterns := make([]*LearnedPattern, 0, len(nodes))
	for _, node := range nodes {
		pattern, err := nodeToPattern(node)
		if err != nil {
			continue
		}
		if patternType != "" && pattern.PatternType != patternType {
			continue
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

// StorePreference persists a user preference.
func (s *Store) StorePreference(ctx context.Context, pref *UserPreference) error {
	if pref.ID == "" {
		pref.ID = uuid.New().String()
	}
	if pref.CreatedAt.IsZero() {
		pref.CreatedAt = time.Now()
	}

	valueJSON, _ := json.Marshal(pref.Value)
	metadata, _ := json.Marshal(map[string]interface{}{
		"key":         pref.Key,
		"value":       json.RawMessage(valueJSON),
		"scope":       pref.Scope,
		"scope_value": pref.ScopeValue,
		"source":      pref.Source,
		"usage_count": pref.UsageCount,
		"last_used":   pref.LastUsed,
		"extra":       pref.Metadata,
	})

	// Content is "key=value" for searchability
	content := fmt.Sprintf("%s=%v", pref.Key, pref.Value)

	node := &hypergraph.Node{
		ID:         pref.ID,
		Type:       hypergraph.NodeTypeDecision,
		Subtype:    SubtypeUserPreference,
		Content:    content,
		Tier:       hypergraph.TierLongterm,
		Confidence: pref.Confidence,
		Metadata:   metadata,
		CreatedAt:  pref.CreatedAt,
		UpdatedAt:  time.Now(),
	}

	return s.graph.CreateNode(ctx, node)
}

// GetPreference retrieves a preference by ID.
func (s *Store) GetPreference(ctx context.Context, id string) (*UserPreference, error) {
	node, err := s.graph.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	if node == nil || node.Subtype != SubtypeUserPreference {
		return nil, nil
	}
	return nodeToPreference(node)
}

// GetPreferenceByKey retrieves a preference by key and scope.
func (s *Store) GetPreferenceByKey(ctx context.Context, key string, scope PreferenceScope, scopeValue string) (*UserPreference, error) {
	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeDecision},
		Subtypes: []string{SubtypeUserPreference},
		Limit:    100, // Reasonable limit
	})
	if err != nil {
		return nil, err
	}

	for _, node := range nodes {
		pref, err := nodeToPreference(node)
		if err != nil {
			continue
		}
		if pref.Key == key && pref.Scope == scope && pref.ScopeValue == scopeValue {
			return pref, nil
		}
	}
	return nil, nil
}

// ListPreferences lists user preferences with optional scope filtering.
func (s *Store) ListPreferences(ctx context.Context, scope PreferenceScope, scopeValue string) ([]*UserPreference, error) {
	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeDecision},
		Subtypes: []string{SubtypeUserPreference},
		Limit:    1000,
	})
	if err != nil {
		return nil, err
	}

	prefs := make([]*UserPreference, 0, len(nodes))
	for _, node := range nodes {
		pref, err := nodeToPreference(node)
		if err != nil {
			continue
		}
		// Filter by scope if specified
		if scope != "" && pref.Scope != scope {
			continue
		}
		if scopeValue != "" && pref.ScopeValue != scopeValue {
			continue
		}
		prefs = append(prefs, pref)
	}
	return prefs, nil
}

// StoreConstraint persists a learned constraint.
func (s *Store) StoreConstraint(ctx context.Context, constraint *LearnedConstraint) error {
	if constraint.ID == "" {
		constraint.ID = uuid.New().String()
	}
	if constraint.CreatedAt.IsZero() {
		constraint.CreatedAt = time.Now()
	}

	metadata, _ := json.Marshal(map[string]interface{}{
		"constraint_type": constraint.ConstraintType,
		"correction":      constraint.Correction,
		"trigger":         constraint.Trigger,
		"domain":          constraint.Domain,
		"severity":        constraint.Severity,
		"source":          constraint.Source,
		"violation_count": constraint.ViolationCount,
		"last_triggered":  constraint.LastTriggered,
		"extra":           constraint.Metadata,
	})

	node := &hypergraph.Node{
		ID:         constraint.ID,
		Type:       hypergraph.NodeTypeExperience,
		Subtype:    SubtypeLearnedConstraint,
		Content:    constraint.Description,
		Tier:       hypergraph.TierLongterm,
		Confidence: constraint.Severity,
		Embedding:  serializeEmbedding(constraint.Embedding),
		Metadata:   metadata,
		CreatedAt:  constraint.CreatedAt,
		UpdatedAt:  time.Now(),
	}

	return s.graph.CreateNode(ctx, node)
}

// ListConstraints lists learned constraints.
func (s *Store) ListConstraints(ctx context.Context, domain string, minSeverity float64) ([]*LearnedConstraint, error) {
	nodes, err := s.graph.ListNodes(ctx, hypergraph.NodeFilter{
		Types:         []hypergraph.NodeType{hypergraph.NodeTypeExperience},
		Subtypes:      []string{SubtypeLearnedConstraint},
		MinConfidence: minSeverity,
		Limit:         1000,
	})
	if err != nil {
		return nil, err
	}

	constraints := make([]*LearnedConstraint, 0, len(nodes))
	for _, node := range nodes {
		constraint, err := nodeToConstraint(node)
		if err != nil {
			continue
		}
		if domain != "" && constraint.Domain != domain {
			continue
		}
		constraints = append(constraints, constraint)
	}
	return constraints, nil
}

// RecordSignal stores a learning signal.
func (s *Store) RecordSignal(ctx context.Context, signal *LearningSignal) error {
	if signal.ID == "" {
		signal.ID = uuid.New().String()
	}

	metadata, _ := json.Marshal(signal.Metadata)

	node := &hypergraph.Node{
		ID:         signal.ID,
		Type:       hypergraph.NodeTypeExperience,
		Subtype:    SubtypeLearningSignal,
		Content:    signal.Context.Query,
		Tier:       hypergraph.TierSession,
		Confidence: signal.Confidence,
		Embedding:  serializeEmbedding(signal.Embedding),
		Metadata:   metadata,
		Provenance: mustMarshal(map[string]interface{}{
			"signal_type": signal.Type.String(),
			"domain":      signal.Domain,
			"session_id":  signal.Context.SessionID,
			"task_id":     signal.Context.TaskID,
			"model":       signal.Context.Model,
			"strategy":    signal.Context.Strategy,
		}),
		CreatedAt: signal.Timestamp,
		UpdatedAt: time.Now(),
	}

	return s.graph.CreateNode(ctx, node)
}

// Stats returns learning statistics.
func (s *Store) Stats(ctx context.Context) (*LearningStats, error) {
	graphStats, err := s.graph.Stats(ctx)
	if err != nil {
		return nil, err
	}

	stats := &LearningStats{
		TotalNodes: int(graphStats.NodeCount),
	}

	// Count by subtype manually since hypergraph.Stats doesn't track subtypes
	subtypes := []string{
		SubtypeLearnedFact,
		SubtypeLearnedPattern,
		SubtypeUserPreference,
		SubtypeLearnedConstraint,
		SubtypeLearningSignal,
	}

	for _, subtype := range subtypes {
		count, err := s.graph.CountNodes(ctx, hypergraph.NodeFilter{
			Subtypes: []string{subtype},
		})
		if err != nil {
			continue
		}
		switch subtype {
		case SubtypeLearnedFact:
			stats.FactCount = int(count)
		case SubtypeLearnedPattern:
			stats.PatternCount = int(count)
		case SubtypeUserPreference:
			stats.PreferenceCount = int(count)
		case SubtypeLearnedConstraint:
			stats.ConstraintCount = int(count)
		case SubtypeLearningSignal:
			stats.SignalCount = int(count)
		}
	}

	return stats, nil
}

// LearningStats contains learning storage statistics.
type LearningStats struct {
	TotalNodes      int `json:"total_nodes"`
	FactCount       int `json:"fact_count"`
	PatternCount    int `json:"pattern_count"`
	PreferenceCount int `json:"preference_count"`
	ConstraintCount int `json:"constraint_count"`
	SignalCount     int `json:"signal_count"`
}

// Helper functions

func nodeToFact(node *hypergraph.Node) (*LearnedFact, error) {
	fact := &LearnedFact{
		ID:           node.ID,
		Content:      node.Content,
		Confidence:   node.Confidence,
		Embedding:    deserializeEmbedding(node.Embedding),
		AccessCount:  node.AccessCount,
		LastAccessed: ptrToTime(node.LastAccessed),
		CreatedAt:    node.CreatedAt,
	}

	// Parse metadata
	if len(node.Metadata) > 0 {
		json.Unmarshal(node.Metadata, &fact.Metadata)
	}

	// Parse provenance for learning-specific fields
	if len(node.Provenance) > 0 {
		var prov map[string]interface{}
		if err := json.Unmarshal(node.Provenance, &prov); err == nil {
			if v, ok := prov["domain"].(string); ok {
				fact.Domain = v
			}
			if v, ok := prov["source"].(string); ok {
				fact.Source = SourceType(v)
			}
			if v, ok := prov["success_count"].(float64); ok {
				fact.SuccessCount = int(v)
			}
			if v, ok := prov["failure_count"].(float64); ok {
				fact.FailureCount = int(v)
			}
			if v, ok := prov["last_validated"].(string); ok {
				fact.LastValidated, _ = time.Parse(time.RFC3339, v)
			}
		}
	}

	return fact, nil
}

func nodeToPattern(node *hypergraph.Node) (*LearnedPattern, error) {
	pattern := &LearnedPattern{
		ID:        node.ID,
		Name:      node.Content,
		Embedding: deserializeEmbedding(node.Embedding),
		CreatedAt: node.CreatedAt,
	}

	if len(node.Metadata) > 0 {
		var meta map[string]interface{}
		if err := json.Unmarshal(node.Metadata, &meta); err == nil {
			if v, ok := meta["pattern_type"].(string); ok {
				pattern.PatternType = PatternType(v)
			}
			if v, ok := meta["trigger"].(string); ok {
				pattern.Trigger = v
			}
			if v, ok := meta["template"].(string); ok {
				pattern.Template = v
			}
			if v, ok := meta["examples"].([]interface{}); ok {
				for _, e := range v {
					if s, ok := e.(string); ok {
						pattern.Examples = append(pattern.Examples, s)
					}
				}
			}
			if v, ok := meta["domains"].([]interface{}); ok {
				for _, d := range v {
					if s, ok := d.(string); ok {
						pattern.Domains = append(pattern.Domains, s)
					}
				}
			}
			if v, ok := meta["success_rate"].(float64); ok {
				pattern.SuccessRate = v
			}
			if v, ok := meta["usage_count"].(float64); ok {
				pattern.UsageCount = int(v)
			}
			if v, ok := meta["last_used"].(string); ok {
				pattern.LastUsed, _ = time.Parse(time.RFC3339, v)
			}
			if v, ok := meta["extra"].(map[string]interface{}); ok {
				pattern.Metadata = v
			}
		}
	}

	return pattern, nil
}

func nodeToPreference(node *hypergraph.Node) (*UserPreference, error) {
	pref := &UserPreference{
		ID:        node.ID,
		Confidence: node.Confidence,
		CreatedAt: node.CreatedAt,
	}

	if len(node.Metadata) > 0 {
		var meta map[string]interface{}
		if err := json.Unmarshal(node.Metadata, &meta); err == nil {
			if v, ok := meta["key"].(string); ok {
				pref.Key = v
			}
			if v, ok := meta["value"]; ok {
				pref.Value = v
			}
			if v, ok := meta["scope"].(string); ok {
				pref.Scope = PreferenceScope(v)
			}
			if v, ok := meta["scope_value"].(string); ok {
				pref.ScopeValue = v
			}
			if v, ok := meta["source"].(string); ok {
				pref.Source = SourceType(v)
			}
			if v, ok := meta["usage_count"].(float64); ok {
				pref.UsageCount = int(v)
			}
			if v, ok := meta["last_used"].(string); ok {
				pref.LastUsed, _ = time.Parse(time.RFC3339, v)
			}
			if v, ok := meta["extra"].(map[string]interface{}); ok {
				pref.Metadata = v
			}
		}
	}

	return pref, nil
}

func nodeToConstraint(node *hypergraph.Node) (*LearnedConstraint, error) {
	constraint := &LearnedConstraint{
		ID:          node.ID,
		Description: node.Content,
		Embedding:   deserializeEmbedding(node.Embedding),
		CreatedAt:   node.CreatedAt,
	}

	if len(node.Metadata) > 0 {
		var meta map[string]interface{}
		if err := json.Unmarshal(node.Metadata, &meta); err == nil {
			if v, ok := meta["constraint_type"].(string); ok {
				constraint.ConstraintType = ConstraintType(v)
			}
			if v, ok := meta["correction"].(string); ok {
				constraint.Correction = v
			}
			if v, ok := meta["trigger"].(string); ok {
				constraint.Trigger = v
			}
			if v, ok := meta["domain"].(string); ok {
				constraint.Domain = v
			}
			if v, ok := meta["severity"].(float64); ok {
				constraint.Severity = v
			}
			if v, ok := meta["source"].(string); ok {
				constraint.Source = SourceType(v)
			}
			if v, ok := meta["violation_count"].(float64); ok {
				constraint.ViolationCount = int(v)
			}
			if v, ok := meta["last_triggered"].(string); ok {
				constraint.LastTriggered, _ = time.Parse(time.RFC3339, v)
			}
			if v, ok := meta["extra"].(map[string]interface{}); ok {
				constraint.Metadata = v
			}
		}
	}

	return constraint, nil
}

func serializeEmbedding(embedding []float32) []byte {
	if len(embedding) == 0 {
		return nil
	}
	// Simple serialization: store as JSON for now
	data, _ := json.Marshal(embedding)
	return data
}

func deserializeEmbedding(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	var embedding []float32
	json.Unmarshal(data, &embedding)
	return embedding
}

func mustMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}

func timeToPtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func ptrToTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
