package rlm

import (
	"context"

	"github.com/rand/recurse/internal/rlm/repl"
)

// REPLCallbackHandler implements repl.CallbackHandler using SubCallRouter.
// This bridges Python's llm_call() to the Go LLM infrastructure.
type REPLCallbackHandler struct {
	router *SubCallRouter
	ctx    context.Context
	depth  int
	budget int
}

// NewREPLCallbackHandler creates a new callback handler.
func NewREPLCallbackHandler(router *SubCallRouter) *REPLCallbackHandler {
	return &REPLCallbackHandler{
		router: router,
		ctx:    context.Background(),
		depth:  0,
		budget: 100000, // Default budget
	}
}

// WithContext returns a copy with the given context.
func (h *REPLCallbackHandler) WithContext(ctx context.Context) *REPLCallbackHandler {
	return &REPLCallbackHandler{
		router: h.router,
		ctx:    ctx,
		depth:  h.depth,
		budget: h.budget,
	}
}

// WithDepth returns a copy with the given recursion depth.
func (h *REPLCallbackHandler) WithDepth(depth int) *REPLCallbackHandler {
	return &REPLCallbackHandler{
		router: h.router,
		ctx:    h.ctx,
		depth:  depth,
		budget: h.budget,
	}
}

// WithBudget returns a copy with the given token budget.
func (h *REPLCallbackHandler) WithBudget(budget int) *REPLCallbackHandler {
	return &REPLCallbackHandler{
		router: h.router,
		ctx:    h.ctx,
		depth:  h.depth,
		budget: budget,
	}
}

// HandleLLMCall handles a single LLM call from Python.
func (h *REPLCallbackHandler) HandleLLMCall(prompt, context, model string) (string, error) {
	if h.router == nil {
		return "", nil
	}

	resp := h.router.Call(h.ctx, SubCallRequest{
		Prompt:  prompt,
		Context: context,
		Model:   model,
		Depth:   h.depth,
		Budget:  h.budget,
	})

	if resp.Error != "" {
		return "", &CallbackError{Message: resp.Error}
	}

	return resp.Response, nil
}

// HandleLLMBatch handles a batch of LLM calls from Python.
func (h *REPLCallbackHandler) HandleLLMBatch(prompts, contexts []string, model string) ([]string, error) {
	if h.router == nil {
		return make([]string, len(prompts)), nil
	}

	// Build batch request
	requests := make([]SubCallRequest, len(prompts))
	for i := range prompts {
		ctx := ""
		if i < len(contexts) {
			ctx = contexts[i]
		}
		requests[i] = SubCallRequest{
			Prompt:  prompts[i],
			Context: ctx,
			Model:   model,
			Depth:   h.depth,
			Budget:  h.budget / len(prompts), // Divide budget among batch
		}
	}

	responses := h.router.BatchCall(h.ctx, requests)

	results := make([]string, len(responses))
	for i, resp := range responses {
		if resp.Error != "" {
			// Include error in result rather than failing entire batch
			results[i] = "[ERROR: " + resp.Error + "]"
		} else {
			results[i] = resp.Response
		}
	}

	return results, nil
}

// CallbackError represents an error from a callback.
type CallbackError struct {
	Message string
}

func (e *CallbackError) Error() string {
	return e.Message
}

// Verify interface compliance
var _ repl.CallbackHandler = (*REPLCallbackHandler)(nil)
