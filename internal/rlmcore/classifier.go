package rlmcore

import (
	core "github.com/rand/rlm-core/go/rlmcore"
)

// PatternClassifier wraps the rlm-core PatternClassifier.
// Uses pattern matching and heuristics to classify query complexity.
type PatternClassifier struct {
	inner *core.PatternClassifier
}

// NewPatternClassifier creates a new rlm-core backed classifier.
func NewPatternClassifier() (*PatternClassifier, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewPatternClassifier()
	return &PatternClassifier{inner: inner}, nil
}

// NewPatternClassifierWithThreshold creates a classifier with custom threshold.
func NewPatternClassifierWithThreshold(threshold int) (*PatternClassifier, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewPatternClassifierWithThreshold(threshold)
	return &PatternClassifier{inner: inner}, nil
}

// ShouldActivate determines if RLM should activate for a query.
func (p *PatternClassifier) ShouldActivate(query string, ctx *SessionContext) *ActivationDecision {
	var coreCtx *core.SessionContext
	if ctx != nil {
		coreCtx = ctx.inner
	}
	decision := p.inner.ShouldActivate(query, coreCtx)
	return &ActivationDecision{inner: decision}
}

// Free releases the classifier resources.
func (p *PatternClassifier) Free() {
	if p.inner != nil {
		p.inner.Free()
		p.inner = nil
	}
}

// ActivationDecision wraps the rlm-core ActivationDecision.
type ActivationDecision struct {
	inner *core.ActivationDecision
}

// ShouldActivate returns true if RLM should be activated.
func (d *ActivationDecision) ShouldActivate() bool {
	return d.inner.ShouldActivate()
}

// Reason returns the reason for the activation decision.
func (d *ActivationDecision) Reason() string {
	return d.inner.Reason()
}

// Score returns the complexity score.
func (d *ActivationDecision) Score() int {
	return d.inner.Score()
}

// Free releases the decision resources.
func (d *ActivationDecision) Free() {
	if d.inner != nil {
		d.inner.Free()
		d.inner = nil
	}
}

// SessionContext wraps the rlm-core SessionContext.
type SessionContext struct {
	inner *core.SessionContext
}

// NewSessionContext creates a new session context.
func NewSessionContext() (*SessionContext, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewSessionContext()
	return &SessionContext{inner: inner}, nil
}

// AddUserMessage adds a user message to the context.
func (c *SessionContext) AddUserMessage(content string) error {
	return c.inner.AddUserMessage(content)
}

// AddAssistantMessage adds an assistant message to the context.
func (c *SessionContext) AddAssistantMessage(content string) error {
	return c.inner.AddAssistantMessage(content)
}

// CacheFile caches file content in the context.
func (c *SessionContext) CacheFile(path, content string) error {
	return c.inner.CacheFile(path, content)
}

// MessageCount returns the number of messages.
func (c *SessionContext) MessageCount() int64 {
	return c.inner.MessageCount()
}

// FileCount returns the number of cached files.
func (c *SessionContext) FileCount() int64 {
	return c.inner.FileCount()
}

// Free releases the context resources.
func (c *SessionContext) Free() {
	if c.inner != nil {
		c.inner.Free()
		c.inner = nil
	}
}
