package rlm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/repl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationMockLLMClient is a test double for the full integration test.
type integrationMockLLMClient struct {
	responses map[string]string
	calls     []string
}

func newIntegrationMockClient() *integrationMockLLMClient {
	return &integrationMockLLMClient{
		responses: map[string]string{
			"summarize":  "This is a summary of the provided content.",
			"analyze":    "Analysis complete: the code contains 3 functions.",
			"default":    "Mock LLM response for testing.",
		},
		calls: []string{},
	}
}

func (m *integrationMockLLMClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	m.calls = append(m.calls, prompt)

	// Return different responses based on prompt content
	promptLower := strings.ToLower(prompt)
	if strings.Contains(promptLower, "summarize") {
		return m.responses["summarize"], nil
	}
	if strings.Contains(promptLower, "analyze") {
		return m.responses["analyze"], nil
	}
	return m.responses["default"], nil
}

// TestFullRLMPipeline tests the complete RLM flow end-to-end.
func TestFullRLMPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create mock LLM client
	mockClient := newIntegrationMockClient()

	// Create in-memory hypergraph store
	store, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	defer store.Close()

	// Create RLM service with minimal config
	svc, err := NewService(mockClient, ServiceConfig{
		Controller: ControllerConfig{
			MaxRecursionDepth: 3,
			MaxTokenBudget:    10000,
		},
		Meta: meta.DefaultConfig(),
	})
	require.NoError(t, err)
	defer svc.Stop()

	// Start service
	err = svc.Start(ctx)
	require.NoError(t, err)

	// Create and start REPL manager
	replMgr, err := repl.NewManager(repl.Options{})
	require.NoError(t, err)

	err = replMgr.Start(ctx)
	require.NoError(t, err)
	defer replMgr.Stop()

	// Wire up REPL with service (this sets up LLM and memory callbacks)
	svc.SetREPLManager(replMgr)

	t.Run("context_externalization", func(t *testing.T) {
		// Test storing context as REPL variables
		testCode := `
def hello():
    return "Hello, World!"

def add(a, b):
    return a + b

class Calculator:
    def multiply(self, x, y):
        return x * y
`
		err := replMgr.SetVar(ctx, "code_context", testCode)
		require.NoError(t, err)

		// Verify we can access it
		result, err := replMgr.GetVar(ctx, "code_context", 0, 100)
		require.NoError(t, err)
		assert.Contains(t, result.Value, "def hello")
	})

	t.Run("peek_grep_partition", func(t *testing.T) {
		// Test RLM helper functions
		result, err := replMgr.Execute(ctx, `
# Peek at first 50 chars
preview = peek(code_context, 0, 50)
preview
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Contains(t, result.ReturnVal, "def hello")

		// Test grep
		result, err = replMgr.Execute(ctx, `
matches = grep(code_context, r"def \w+")
len(matches)
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Equal(t, "3", result.ReturnVal) // hello, add, and multiply (inside Calculator)

		// Test partition
		result, err = replMgr.Execute(ctx, `
chunks = partition(code_context, n=2)
len(chunks)
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Equal(t, "2", result.ReturnVal)

		// Test extract_functions
		result, err = replMgr.Execute(ctx, `
funcs = extract_functions(code_context, "python")
[f['name'] for f in funcs]
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Contains(t, result.ReturnVal, "hello")
		assert.Contains(t, result.ReturnVal, "add")
	})

	t.Run("llm_call_callback", func(t *testing.T) {
		// Test that llm_call routes through Go and back
		result, err := replMgr.Execute(ctx, `
response = llm_call("Summarize this code", code_context, "fast")
response
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Contains(t, result.ReturnVal, "summary")

		// Verify the mock client received the call
		assert.NotEmpty(t, mockClient.calls)
	})

	t.Run("memory_operations", func(t *testing.T) {
		// Test memory_add_fact
		result, err := replMgr.Execute(ctx, `
fact_id = memory_add_fact("The code contains a Calculator class", 0.9)
fact_id != ""
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Equal(t, "True", result.ReturnVal)

		// Test memory_add_experience
		result, err = replMgr.Execute(ctx, `
exp_id = memory_add_experience(
    "Used grep to find function definitions",
    "Found 2 functions successfully",
    True
)
exp_id != ""
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Equal(t, "True", result.ReturnVal)

		// Test memory_query
		result, err = replMgr.Execute(ctx, `
nodes = memory_query("Calculator", limit=5)
len(nodes) > 0
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Equal(t, "True", result.ReturnVal)

		// Test memory_get_context
		result, err = replMgr.Execute(ctx, `
context_nodes = memory_get_context(10)
len(context_nodes) >= 2  # Should have at least fact and experience
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)
		assert.Equal(t, "True", result.ReturnVal)
	})

	t.Run("final_mechanism", func(t *testing.T) {
		// Clear any previous final output
		_, err := replMgr.Execute(ctx, "clear_final_output()")
		require.NoError(t, err)

		// Test FINAL()
		result, err := replMgr.Execute(ctx, `
analysis = "Found 2 functions: hello() and add()"
FINAL(analysis)
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)

		// Verify FINAL was set
		result, err = replMgr.Execute(ctx, `has_final_output()`)
		require.NoError(t, err)
		assert.Equal(t, "True", result.ReturnVal)

		// Get the final output
		result, err = replMgr.Execute(ctx, `get_final_output()`)
		require.NoError(t, err)
		assert.Contains(t, result.ReturnVal, "Found 2 functions")

		// Test FINAL_JSON
		_, err = replMgr.Execute(ctx, "clear_final_output()")
		require.NoError(t, err)

		result, err = replMgr.Execute(ctx, `
result = {"functions": ["hello", "add"], "classes": ["Calculator"]}
FINAL_JSON(result)
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)

		result, err = replMgr.Execute(ctx, `get_final_metadata()`)
		require.NoError(t, err)
		assert.Contains(t, result.ReturnVal, "json")
	})

	t.Run("full_rlm_workflow", func(t *testing.T) {
		// Simulate a complete RLM workflow
		_, err := replMgr.Execute(ctx, "clear_final_output()")
		require.NoError(t, err)

		// Set up context
		largeCode := `
package main

import "fmt"

func main() {
    result := processData(getData())
    fmt.Println(result)
}

func getData() []int {
    return []int{1, 2, 3, 4, 5}
}

func processData(data []int) int {
    sum := 0
    for _, v := range data {
        sum += v
    }
    return sum
}

func unusedFunc() {
    // This function is not used
}
`
		err = replMgr.SetVar(ctx, "go_code", largeCode)
		require.NoError(t, err)

		// Execute RLM-style analysis
		result, err := replMgr.Execute(ctx, `
# Step 1: Explore the context
preview = peek(go_code, 0, 200)
token_count = count_tokens_approx(go_code)

# Step 2: Find functions
matches = grep(go_code, r"func \w+")
func_names = [m['line'].strip() for m in matches]

# Step 3: Record findings in memory
memory_add_fact(f"Go code has {len(matches)} functions", 0.95)

# Step 4: Use LLM for deeper analysis
analysis = llm_call("Analyze the code structure", go_code[:500], "fast")

# Step 5: Construct final answer
result = {
    "token_estimate": token_count,
    "function_count": len(matches),
    "functions": func_names,
    "llm_analysis": analysis
}

FINAL_JSON(result)
`)
		require.NoError(t, err)
		assert.Empty(t, result.Error)

		// Verify final output
		result, err = replMgr.Execute(ctx, `has_final_output()`)
		require.NoError(t, err)
		assert.Equal(t, "True", result.ReturnVal)

		// Get metadata to verify it's JSON type
		result, err = replMgr.Execute(ctx, `get_final_metadata()['type']`)
		require.NoError(t, err)
		assert.Contains(t, result.ReturnVal, "json")
	})
}

// TestRLMServiceIntegration tests the Service-level integration.
func TestRLMServiceIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mockClient := newIntegrationMockClient()

	// Create service
	svc, err := NewService(mockClient, DefaultServiceConfig())
	require.NoError(t, err)
	defer svc.Stop()

	// Start service
	err = svc.Start(ctx)
	require.NoError(t, err)

	// Verify service is running
	assert.True(t, svc.IsRunning())

	// Check health
	health, err := svc.HealthCheck(ctx)
	require.NoError(t, err)
	assert.True(t, health.Running)

	// Verify components
	assert.NotNil(t, svc.Store())
	assert.NotNil(t, svc.Controller())
	assert.NotNil(t, svc.Wrapper())
	assert.NotNil(t, svc.SubCallRouter())
	assert.NotNil(t, svc.Orchestrator())
}

// e2eMockLLMClient tracks calls and provides configurable responses for end-to-end tests.
type e2eMockLLMClient struct {
	metaResponses []string // Responses for meta-controller (orchestration decisions)
	mainResponses []string // Responses for main LLM (actual content)
	callLog       []e2eCall
	metaIndex     int
	mainIndex     int
}

type e2eCall struct {
	Type   string // "meta" or "main"
	Prompt string
}

func newE2EMockClient(metaResp, mainResp []string) *e2eMockLLMClient {
	return &e2eMockLLMClient{
		metaResponses: metaResp,
		mainResponses: mainResp,
		callLog:       []e2eCall{},
	}
}

func (m *e2eMockLLMClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	// Detect if this is a meta-controller call (orchestration decision)
	promptLower := strings.ToLower(prompt)
	isMeta := strings.Contains(promptLower, "action") &&
		(strings.Contains(promptLower, "direct") ||
		 strings.Contains(promptLower, "decompose") ||
		 strings.Contains(promptLower, "memory_query"))

	if isMeta {
		m.callLog = append(m.callLog, e2eCall{Type: "meta", Prompt: prompt})
		if m.metaIndex < len(m.metaResponses) {
			resp := m.metaResponses[m.metaIndex]
			m.metaIndex++
			return resp, nil
		}
		return `{"action": "DIRECT", "reasoning": "Default"}`, nil
	}

	// Main LLM call for actual response generation
	m.callLog = append(m.callLog, e2eCall{Type: "main", Prompt: prompt})
	if m.mainIndex < len(m.mainResponses) {
		resp := m.mainResponses[m.mainIndex]
		m.mainIndex++
		return resp, nil
	}
	return "Mock main LLM response", nil
}

// TestExecuteEndToEnd tests the complete Execute flow.
func TestExecuteEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("direct_action_generates_response", func(t *testing.T) {
		// Setup mock client: meta returns DIRECT, main returns actual answer
		mockClient := newE2EMockClient(
			[]string{`{"action": "DIRECT", "reasoning": "Simple factual question"}`},
			[]string{"The answer is 4."},
		)

		store, err := hypergraph.NewStore(hypergraph.Options{})
		require.NoError(t, err)
		defer store.Close()

		metaCtrl := meta.NewController(mockClient, meta.DefaultConfig())
		cfg := DefaultControllerConfig()
		cfg.StoreDecisions = true
		ctrl := NewController(metaCtrl, mockClient, store, cfg)

		// Execute a simple question
		result, err := ctrl.Execute(ctx, "What is 2+2?")
		require.NoError(t, err)

		// Verify response is NOT echoed input
		assert.NotEqual(t, "What is 2+2?", result.Response, "Response should not echo input")
		assert.Contains(t, result.Response, "4", "Response should contain the answer")

		// Verify both meta and main LLM were called
		var metaCalls, mainCalls int
		for _, call := range mockClient.callLog {
			if call.Type == "meta" {
				metaCalls++
			} else {
				mainCalls++
			}
		}
		assert.GreaterOrEqual(t, metaCalls, 1, "Meta-controller should be called")
		assert.GreaterOrEqual(t, mainCalls, 1, "Main LLM should be called for response")
	})

	t.Run("memory_context_used", func(t *testing.T) {
		mockClient := newE2EMockClient(
			[]string{`{"action": "DIRECT", "reasoning": "Use memory context"}`},
			[]string{"Based on the context, the project uses Go."},
		)

		store, err := hypergraph.NewStore(hypergraph.Options{})
		require.NoError(t, err)
		defer store.Close()

		// Pre-populate memory with a fact
		fact := hypergraph.NewNode(hypergraph.NodeTypeFact, "The project uses Go language")
		fact.Confidence = 0.9
		require.NoError(t, store.CreateNode(ctx, fact))

		metaCtrl := meta.NewController(mockClient, meta.DefaultConfig())
		cfg := DefaultControllerConfig()
		cfg.StoreDecisions = false
		ctrl := NewController(metaCtrl, mockClient, store, cfg)

		// Execute query that should match the stored fact
		result, err := ctrl.Execute(ctx, "What language does this project use?")
		require.NoError(t, err)
		assert.NotEmpty(t, result.Response)

		// Verify fact was accessed (access count incremented)
		node, err := store.GetNode(ctx, fact.ID)
		require.NoError(t, err)
		assert.Greater(t, node.AccessCount, 0, "Fact should have been accessed")
	})

	t.Run("decision_stored_in_memory", func(t *testing.T) {
		mockClient := newE2EMockClient(
			[]string{`{"action": "DIRECT", "reasoning": "Store this decision"}`},
			[]string{"Decision stored response."},
		)

		store, err := hypergraph.NewStore(hypergraph.Options{})
		require.NoError(t, err)
		defer store.Close()

		metaCtrl := meta.NewController(mockClient, meta.DefaultConfig())
		cfg := DefaultControllerConfig()
		cfg.StoreDecisions = true
		ctrl := NewController(metaCtrl, mockClient, store, cfg)

		// Execute task
		_, err = ctrl.Execute(ctx, "Important task to remember")
		require.NoError(t, err)

		// Verify decision node was created
		nodes, err := store.ListNodes(ctx, hypergraph.NodeFilter{
			Types: []hypergraph.NodeType{hypergraph.NodeTypeDecision},
			Limit: 10,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(nodes), 1, "Decision node should be created")
	})

	t.Run("trace_events_recorded", func(t *testing.T) {
		mockClient := newE2EMockClient(
			[]string{`{"action": "DIRECT", "reasoning": "Traced task"}`},
			[]string{"Traced response."},
		)

		store, err := hypergraph.NewStore(hypergraph.Options{})
		require.NoError(t, err)
		defer store.Close()

		metaCtrl := meta.NewController(mockClient, meta.DefaultConfig())
		cfg := DefaultControllerConfig()
		cfg.TraceEnabled = true
		ctrl := NewController(metaCtrl, mockClient, store, cfg)

		// Attach a trace recorder
		tracer := &mockTraceRecorder{}
		ctrl.SetTracer(tracer)

		// Execute task
		_, err = ctrl.Execute(ctx, "Trace this task")
		require.NoError(t, err)

		// Verify trace events were recorded
		assert.GreaterOrEqual(t, len(tracer.events), 1, "Trace events should be recorded")
	})

	t.Run("execution_result_fields", func(t *testing.T) {
		mockClient := newE2EMockClient(
			[]string{`{"action": "DIRECT", "reasoning": "Complete test"}`},
			[]string{"Complete test response."},
		)

		store, err := hypergraph.NewStore(hypergraph.Options{})
		require.NoError(t, err)
		defer store.Close()

		metaCtrl := meta.NewController(mockClient, meta.DefaultConfig())
		ctrl := NewController(metaCtrl, mockClient, store, DefaultControllerConfig())

		result, err := ctrl.Execute(ctx, "Test task")
		require.NoError(t, err)

		// Verify all result fields are populated
		assert.Equal(t, "Test task", result.Task)
		assert.NotEmpty(t, result.Response)
		assert.Greater(t, result.TotalTokens, 0)
		assert.Greater(t, result.Duration, int64(0))
	})
}
