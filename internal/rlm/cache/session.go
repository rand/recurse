package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// DecompositionSession manages caching for a decomposition chain.
type DecompositionSession struct {
	id            string
	sharedContext *StructuredPrompt
	strategy      Strategy
	expectedCalls int
	mu            sync.RWMutex
}

// NewDecompositionSession creates a session with shared cached context.
func NewDecompositionSession(
	id string,
	systemPrompt string,
	sharedContext string,
	expectedCalls int,
) *DecompositionSession {
	return NewDecompositionSessionWithStrategy(
		id,
		systemPrompt,
		sharedContext,
		expectedCalls,
		NewDefaultStrategy(),
	)
}

// NewDecompositionSessionWithStrategy creates a session with a custom strategy.
func NewDecompositionSessionWithStrategy(
	id string,
	systemPrompt string,
	sharedContext string,
	expectedCalls int,
	strategy Strategy,
) *DecompositionSession {
	ctx := &CacheContext{
		DecompositionID: id,
		ExpectedCalls:   expectedCalls,
		IsDecomposition: true,
	}

	blocks := []CacheableBlock{
		{
			Content:    systemPrompt,
			Role:       RoleSystem,
			TokenCount: EstimateTokens(systemPrompt),
		},
	}

	if sharedContext != "" {
		blocks = append(blocks, CacheableBlock{
			Content:    sharedContext,
			Role:       RoleContext,
			TokenCount: EstimateTokens(sharedContext),
		})
	}

	return &DecompositionSession{
		id:            id,
		sharedContext: strategy.StructurePrompt(blocks, ctx),
		strategy:      strategy,
		expectedCalls: expectedCalls,
	}
}

// ID returns the session identifier.
func (s *DecompositionSession) ID() string {
	return s.id
}

// PrepareSubcall prepares a prompt for a decomposition subcall.
func (s *DecompositionSession) PrepareSubcall(query string) *StructuredPrompt {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Clone shared context
	prompt := &StructuredPrompt{
		SystemBlocks:  make([]CacheableBlock, len(s.sharedContext.SystemBlocks)),
		SharedContext: make([]CacheableBlock, len(s.sharedContext.SharedContext)),
	}
	copy(prompt.SystemBlocks, s.sharedContext.SystemBlocks)
	copy(prompt.SharedContext, s.sharedContext.SharedContext)

	// Add query-specific content (not cached)
	prompt.QueryContent = append(prompt.QueryContent, CacheableBlock{
		Content:    query,
		Role:       RoleUser,
		TokenCount: EstimateTokens(query),
	})

	return prompt
}

// CacheableTokens returns the total cacheable tokens in the session.
func (s *DecompositionSession) CacheableTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sharedContext.CacheableTokens()
}

// SessionManager manages multiple caching sessions.
type SessionManager struct {
	sessions map[string]*CacheHierarchy
	metrics  *CacheMetrics
	strategy Strategy
	mu       sync.RWMutex
}

// NewSessionManager creates a new session manager.
func NewSessionManager(strategy Strategy) *SessionManager {
	if strategy == nil {
		strategy = NewDefaultStrategy()
	}
	return &SessionManager{
		sessions: make(map[string]*CacheHierarchy),
		metrics:  &CacheMetrics{},
		strategy: strategy,
	}
}

// GetOrCreate retrieves or creates a cache hierarchy for a session.
func (m *SessionManager) GetOrCreate(
	sessionID string,
	systemPrompt string,
	sessionContext string,
) *CacheHierarchy {
	m.mu.Lock()
	defer m.mu.Unlock()

	if hierarchy, ok := m.sessions[sessionID]; ok {
		m.metrics.CacheHits++
		return hierarchy
	}

	m.metrics.CacheMisses++

	hierarchy := &CacheHierarchy{
		levels: []CacheLevel{
			{
				Name:        "system",
				Content:     systemPrompt,
				TokenCount:  EstimateTokens(systemPrompt),
				CacheKey:    HashContent(systemPrompt),
				ShouldCache: EstimateTokens(systemPrompt) >= m.strategy.MinTokensToCache(),
			},
		},
	}

	if sessionContext != "" {
		hierarchy.levels = append(hierarchy.levels, CacheLevel{
			Name:        "session",
			Content:     sessionContext,
			TokenCount:  EstimateTokens(sessionContext),
			CacheKey:    HashContent(sessionContext),
			ShouldCache: EstimateTokens(sessionContext) >= m.strategy.MinTokensToCache(),
		})
	}

	m.sessions[sessionID] = hierarchy
	return hierarchy
}

// AddTaskContext adds task-specific context to a session.
func (m *SessionManager) AddTaskContext(sessionID string, taskContext string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if hierarchy, ok := m.sessions[sessionID]; ok {
		hierarchy.levels = append(hierarchy.levels, CacheLevel{
			Name:        "task",
			Content:     taskContext,
			TokenCount:  EstimateTokens(taskContext),
			CacheKey:    HashContent(taskContext),
			ShouldCache: EstimateTokens(taskContext) >= m.strategy.MinTokensToCache(),
		})
	}
}

// RemoveSession removes a session from the manager.
func (m *SessionManager) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// Metrics returns the current cache metrics.
func (m *SessionManager) Metrics() CacheMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.metrics
}

// CacheHierarchy represents a multi-level cache structure.
type CacheHierarchy struct {
	levels []CacheLevel
}

// CacheLevel represents a single level in the cache hierarchy.
type CacheLevel struct {
	Name        string
	Content     string
	TokenCount  int
	CacheKey    string
	ShouldCache bool
}

// BuildPrompt constructs a StructuredPrompt from the hierarchy.
func (h *CacheHierarchy) BuildPrompt(query string) *StructuredPrompt {
	prompt := &StructuredPrompt{}

	for i, level := range h.levels {
		block := CacheableBlock{
			Content:    level.Content,
			TokenCount: level.TokenCount,
		}

		// Cache all levels except the last (query)
		if level.ShouldCache && i < len(h.levels) {
			block.CacheControl = &CacheControl{Type: CacheTypeEphemeral}
		}

		switch i {
		case 0:
			block.Role = RoleSystem
			prompt.SystemBlocks = append(prompt.SystemBlocks, block)
		default:
			block.Role = RoleContext
			prompt.SharedContext = append(prompt.SharedContext, block)
		}
	}

	// Add query as final non-cached block
	if query != "" {
		prompt.QueryContent = append(prompt.QueryContent, CacheableBlock{
			Content:    query,
			Role:       RoleUser,
			TokenCount: EstimateTokens(query),
		})
	}

	return prompt
}

// TotalCacheableTokens returns tokens that can be cached.
func (h *CacheHierarchy) TotalCacheableTokens() int {
	total := 0
	for _, level := range h.levels {
		if level.ShouldCache {
			total += level.TokenCount
		}
	}
	return total
}

// Helper functions

// EstimateTokens estimates the token count for text.
// Rough estimate: ~4 characters per token for English.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text) / 4
}

// HashContent creates a short hash of content for cache keys.
func HashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:8])
}
