package hallucination

import (
	"context"
	"strings"
	"time"
)

// SelfVerifyBackend uses the same model for verification.
// This is the simplest backend but may not catch model-specific biases.
type SelfVerifyBackend struct {
	client        LLMCompleter
	timeout       time.Duration
	samplingCount int
}

// NewSelfVerifyBackend creates a self-verification backend.
func NewSelfVerifyBackend(client LLMCompleter, timeout time.Duration, samplingCount int) *SelfVerifyBackend {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if samplingCount <= 0 {
		samplingCount = 5
	}
	return &SelfVerifyBackend{
		client:        client,
		timeout:       timeout,
		samplingCount: samplingCount,
	}
}

func (b *SelfVerifyBackend) Name() string {
	return "self"
}

func (b *SelfVerifyBackend) EstimateProbability(ctx context.Context, claim, context string) (float64, error) {
	ctx, cancel := contextWithTimeout(ctx, b.timeout)
	defer cancel()

	// Try with logprobs first if available
	if lc, ok := b.client.(LogprobsCompleter); ok {
		prob, err := b.estimateWithLogprobs(ctx, lc, claim, context)
		if err == nil {
			return prob, nil
		}
		// Fall through to sampling on error
	}

	// Fallback to sampling
	return b.estimateWithSampling(ctx, claim, context)
}

func (b *SelfVerifyBackend) BatchEstimate(ctx context.Context, claims []string, context string) ([]float64, error) {
	results := make([]float64, len(claims))

	// Simple sequential batch for now
	// TODO: implement concurrent batch processing
	for i, claim := range claims {
		prob, err := b.EstimateProbability(ctx, claim, context)
		if err != nil {
			return nil, err
		}
		results[i] = prob
	}

	return results, nil
}

func (b *SelfVerifyBackend) estimateWithLogprobs(ctx context.Context, client LogprobsCompleter, claim, context string) (float64, error) {
	prompt := BuildVerificationPrompt(claim, context)

	_, logprobs, err := client.CompleteWithLogprobs(ctx, prompt, 1)
	if err != nil {
		return 0, err
	}

	return extractProbabilityFromLogprobs(logprobs), nil
}

func (b *SelfVerifyBackend) estimateWithSampling(ctx context.Context, claim, context string) (float64, error) {
	prompt := BuildVerificationPrompt(claim, context)

	yesCount := 0
	for i := 0; i < b.samplingCount; i++ {
		response, err := b.client.Complete(ctx, prompt, 10)
		if err != nil {
			// On error, skip this sample
			continue
		}

		if isYesResponse(response) {
			yesCount++
		}
	}

	// Avoid division by zero
	if b.samplingCount == 0 {
		return 0.5, nil
	}

	return float64(yesCount) / float64(b.samplingCount), nil
}

// HaikuBackend uses Claude Haiku for fast verification.
// Haiku is cheaper and faster, suitable for high-volume verification.
type HaikuBackend struct {
	client        LLMCompleter
	timeout       time.Duration
	samplingCount int
}

// NewHaikuBackend creates a Haiku verification backend.
func NewHaikuBackend(client LLMCompleter, timeout time.Duration, samplingCount int) *HaikuBackend {
	if timeout <= 0 {
		timeout = 3 * time.Second // Haiku is faster
	}
	if samplingCount <= 0 {
		samplingCount = 3 // Fewer samples needed for Haiku
	}
	return &HaikuBackend{
		client:        client,
		timeout:       timeout,
		samplingCount: samplingCount,
	}
}

func (b *HaikuBackend) Name() string {
	return "haiku"
}

func (b *HaikuBackend) EstimateProbability(ctx context.Context, claim, context string) (float64, error) {
	ctx, cancel := contextWithTimeout(ctx, b.timeout)
	defer cancel()

	// Try with logprobs first if available
	if lc, ok := b.client.(LogprobsCompleter); ok {
		prob, err := b.estimateWithLogprobs(ctx, lc, claim, context)
		if err == nil {
			return prob, nil
		}
	}

	// Fallback to sampling
	return b.estimateWithSampling(ctx, claim, context)
}

func (b *HaikuBackend) BatchEstimate(ctx context.Context, claims []string, context string) ([]float64, error) {
	results := make([]float64, len(claims))

	for i, claim := range claims {
		prob, err := b.EstimateProbability(ctx, claim, context)
		if err != nil {
			return nil, err
		}
		results[i] = prob
	}

	return results, nil
}

func (b *HaikuBackend) estimateWithLogprobs(ctx context.Context, client LogprobsCompleter, claim, context string) (float64, error) {
	prompt := BuildVerificationPrompt(claim, context)

	_, logprobs, err := client.CompleteWithLogprobs(ctx, prompt, 1)
	if err != nil {
		return 0, err
	}

	return extractProbabilityFromLogprobs(logprobs), nil
}

func (b *HaikuBackend) estimateWithSampling(ctx context.Context, claim, context string) (float64, error) {
	prompt := BuildVerificationPrompt(claim, context)

	yesCount := 0
	for i := 0; i < b.samplingCount; i++ {
		response, err := b.client.Complete(ctx, prompt, 10)
		if err != nil {
			continue
		}

		if isYesResponse(response) {
			yesCount++
		}
	}

	if b.samplingCount == 0 {
		return 0.5, nil
	}

	return float64(yesCount) / float64(b.samplingCount), nil
}

// MockBackend returns a fixed probability for testing.
type MockBackend struct {
	probability   float64
	callCount     int
	lastClaim     string
	lastContext   string
	customHandler func(claim, context string) float64
}

// NewMockBackend creates a mock backend with fixed probability.
func NewMockBackend(probability float64) *MockBackend {
	return &MockBackend{
		probability: probability,
	}
}

// NewMockBackendWithHandler creates a mock backend with custom logic.
func NewMockBackendWithHandler(handler func(claim, context string) float64) *MockBackend {
	return &MockBackend{
		customHandler: handler,
	}
}

func (b *MockBackend) Name() string {
	return "mock"
}

func (b *MockBackend) EstimateProbability(_ context.Context, claim, context string) (float64, error) {
	b.callCount++
	b.lastClaim = claim
	b.lastContext = context

	if b.customHandler != nil {
		return b.customHandler(claim, context), nil
	}

	return b.probability, nil
}

func (b *MockBackend) BatchEstimate(_ context.Context, claims []string, context string) ([]float64, error) {
	results := make([]float64, len(claims))
	for i, claim := range claims {
		b.callCount++
		b.lastClaim = claim
		b.lastContext = context

		if b.customHandler != nil {
			results[i] = b.customHandler(claim, context)
		} else {
			results[i] = b.probability
		}
	}
	return results, nil
}

// CallCount returns the number of calls made to this mock.
func (b *MockBackend) CallCount() int {
	return b.callCount
}

// LastCall returns the last claim and context.
func (b *MockBackend) LastCall() (claim, context string) {
	return b.lastClaim, b.lastContext
}

// SetProbability updates the mock probability.
func (b *MockBackend) SetProbability(p float64) {
	b.probability = p
}

// Helper functions

func contextWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) < timeout {
			return ctx, func() {} // Parent deadline is sooner
		}
	}
	return context.WithTimeout(ctx, timeout)
}

func extractProbabilityFromLogprobs(logprobs map[string]float64) float64 {
	// Look for YES/NO token probabilities
	yesProb := 0.0
	noProb := 0.0

	for token, logprob := range logprobs {
		lower := strings.ToLower(strings.TrimSpace(token))
		switch lower {
		case "yes", "y", "true":
			yesProb += expLogprob(logprob)
		case "no", "n", "false":
			noProb += expLogprob(logprob)
		}
	}

	// If no YES/NO found, return neutral
	if yesProb == 0 && noProb == 0 {
		return 0.5
	}

	// Normalize to get probability
	total := yesProb + noProb
	if total == 0 {
		return 0.5
	}

	return yesProb / total
}

func expLogprob(logprob float64) float64 {
	// Logprobs are in natural log, convert to probability
	// Clamp to avoid overflow
	if logprob > 0 {
		logprob = 0
	}
	if logprob < -20 {
		logprob = -20
	}
	return exp(logprob)
}

func exp(x float64) float64 {
	// Simple exp approximation to avoid math import in this file
	// For small x, use Taylor series
	if x >= 0 {
		return 1 + x + x*x/2 + x*x*x/6
	}
	// For negative x, use 1/(e^-x)
	return 1 / (1 - x + x*x/2 - x*x*x/6)
}

func isYesResponse(response string) bool {
	response = strings.ToLower(strings.TrimSpace(response))
	return strings.HasPrefix(response, "yes") ||
		strings.HasPrefix(response, "y") ||
		strings.HasPrefix(response, "true")
}
