// Package meta implements the RLM meta-controller for intelligent orchestration decisions.
//
// The meta-controller is responsible for deciding how to handle tasks in the Recursive
// Language Model system. It analyzes task characteristics and context to determine
// the optimal strategy: direct answering, task decomposition, memory queries,
// sub-model calls, or result synthesis.
//
// # Architecture
//
// The package provides two LLM client implementations:
//
//   - HaikuClient: Single-model client using Claude Haiku via Anthropic API
//   - OpenRouterClient: Multi-model client with intelligent routing via OpenRouter
//
// # Model Routing (OpenRouter)
//
// When using OpenRouterClient, the system intelligently selects models based on:
//
//   - Task keywords: Math/logic tasks route to reasoning models
//   - Budget constraints: Low budget prefers cheaper/faster models
//   - Recursion depth: Deeper recursion uses simpler models
//   - Cost optimization: Prefers cheaper models when capabilities are equal
//
// # Model Tiers
//
//   - TierFast: Quick decisions, low latency (Haiku 4.5, Gemini Flash, GPT-5 Mini)
//   - TierBalanced: General tasks, good cost/performance (Sonnet 4.5, GPT-5.2)
//   - TierPowerful: Complex analysis (Opus 4.5, Gemini 3 Pro)
//   - TierReasoning: Deep reasoning, math, proofs (DeepSeek R1, QwQ)
//
// # Usage
//
//	// Using OpenRouter with intelligent routing
//	client, err := meta.NewOpenRouterClient(meta.OpenRouterConfig{
//	    APIKey: os.Getenv("OPENROUTER_API_KEY"),
//	})
//
//	// Create meta-controller
//	ctrl := meta.NewController(client, meta.DefaultConfig())
//
//	// Make orchestration decision
//	decision, err := ctrl.Decide(ctx, meta.State{
//	    Task:           "Analyze this code for bugs",
//	    ContextTokens:  1000,
//	    BudgetRemain:   5000,
//	    RecursionDepth: 0,
//	})
//
// # Actions
//
// The meta-controller can decide on the following actions:
//
//   - DIRECT: Answer directly using current context
//   - DECOMPOSE: Break task into subtasks by file/function/concept
//   - MEMORY_QUERY: Retrieve context from hypergraph memory
//   - SUBCALL: Invoke sub-LM on specific snippet
//   - SYNTHESIZE: Combine existing partial results
//
// # Environment Variables
//
//   - OPENROUTER_API_KEY: API key for OpenRouter (required for OpenRouterClient)
package meta
