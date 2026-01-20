package rlm

import (
	"github.com/rand/recurse/internal/rlmcore"
)

// RLMCoreActivationCheck uses rlm-core's PatternClassifier for activation decisions.
// This supplements the Go TaskClassifier - rlm-core handles "should activate?"
// while TaskClassifier handles "what type of task?"
type RLMCoreActivationCheck struct {
	classifier *rlmcore.PatternClassifier
	ctx        *rlmcore.SessionContext
}

// NewRLMCoreActivationCheck creates a new activation checker using rlm-core.
// Returns nil if rlm-core is not available.
func NewRLMCoreActivationCheck() *RLMCoreActivationCheck {
	if !rlmcore.Available() {
		return nil
	}

	classifier, err := rlmcore.NewPatternClassifier()
	if err != nil {
		return nil
	}

	ctx, err := rlmcore.NewSessionContext()
	if err != nil {
		classifier.Free()
		return nil
	}

	return &RLMCoreActivationCheck{
		classifier: classifier,
		ctx:        ctx,
	}
}

// ShouldActivate checks if RLM should be activated for the given query.
// Returns (shouldActivate, score, reason).
func (c *RLMCoreActivationCheck) ShouldActivate(query string) (bool, int, string) {
	if c == nil || c.classifier == nil {
		return false, 0, ""
	}

	decision := c.classifier.ShouldActivate(query, c.ctx)
	defer decision.Free()

	return decision.ShouldActivate(), decision.Score(), decision.Reason()
}

// AddMessage adds a message to the context for better classification.
func (c *RLMCoreActivationCheck) AddMessage(role string, content string) {
	if c == nil || c.ctx == nil {
		return
	}

	switch role {
	case "user":
		_ = c.ctx.AddUserMessage(content)
	case "assistant":
		_ = c.ctx.AddAssistantMessage(content)
	}
}

// CacheFile adds a file to the context.
func (c *RLMCoreActivationCheck) CacheFile(path, content string) {
	if c == nil || c.ctx == nil {
		return
	}
	_ = c.ctx.CacheFile(path, content)
}

// Free releases rlm-core resources.
func (c *RLMCoreActivationCheck) Free() {
	if c == nil {
		return
	}
	if c.classifier != nil {
		c.classifier.Free()
		c.classifier = nil
	}
	if c.ctx != nil {
		c.ctx.Free()
		c.ctx = nil
	}
}

// UseRLMCoreForActivation returns true if rlm-core should be used for activation checks.
func UseRLMCoreForActivation() bool {
	return rlmcore.Available()
}
