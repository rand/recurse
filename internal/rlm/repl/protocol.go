// Package repl provides Python REPL management for RLM orchestration.
package repl

import (
	"encoding/json"
	"fmt"
)

// Request represents a JSON-RPC style request to the Python REPL.
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC style response from the Python REPL.
type Response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ErrorInfo      `json:"error,omitempty"`
}

// ErrorInfo contains error details from the REPL.
type ErrorInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *ErrorInfo) Error() string {
	return fmt.Sprintf("REPL error %d: %s", e.Code, e.Message)
}

// Error codes for REPL responses.
const (
	ErrCodeParse      = -32700 // Invalid JSON
	ErrCodeInvalidReq = -32600 // Invalid request
	ErrCodeMethod     = -32601 // Method not found
	ErrCodeParams     = -32602 // Invalid params
	ErrCodeInternal   = -32603 // Internal error
	ErrCodeTimeout    = -32000 // Execution timeout
	ErrCodeSandbox    = -32001 // Sandbox violation
	ErrCodeMemory     = -32002 // Memory limit exceeded
)

// ExecuteParams contains parameters for the "execute" method.
type ExecuteParams struct {
	Code string `json:"code"`
}

// ExecuteResult contains the result of code execution.
type ExecuteResult struct {
	Output    string `json:"output"`              // stdout/stderr combined
	ReturnVal string `json:"return_value"`        // repr of last expression
	Error     string `json:"error,omitempty"`     // exception info if any
	Duration  int64  `json:"duration_ms"`         // execution time in milliseconds
}

// SetVarParams contains parameters for the "set_var" method.
type SetVarParams struct {
	Name  string `json:"name"`
	Value string `json:"value"` // string content to store
}

// GetVarParams contains parameters for the "get_var" method.
type GetVarParams struct {
	Name   string `json:"name"`
	Start  int    `json:"start,omitempty"`  // slice start (optional)
	End    int    `json:"end,omitempty"`    // slice end (optional)
	AsRepr bool   `json:"as_repr,omitempty"` // return repr() instead of str()
}

// GetVarResult contains the result of getting a variable.
type GetVarResult struct {
	Value  string `json:"value"`
	Length int    `json:"length"` // total length of the variable
	Type   string `json:"type"`   // Python type name
}

// ListVarsResult contains the list of defined variables.
type ListVarsResult struct {
	Variables []VarInfo `json:"variables"`
}

// VarInfo describes a variable in the REPL namespace.
type VarInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Length int    `json:"length,omitempty"` // for strings/lists/dicts
	Size   int    `json:"size,omitempty"`   // memory size estimate in bytes
}

// StatusResult contains REPL status information.
type StatusResult struct {
	Running      bool    `json:"running"`
	MemoryUsedMB float64 `json:"memory_used_mb"`
	Uptime       int64   `json:"uptime_seconds"`
	ExecCount    int     `json:"exec_count"` // number of executions

	// Resource usage (CPU time in milliseconds)
	UserCPUMS  int64 `json:"user_cpu_ms,omitempty"`
	SysCPUMS   int64 `json:"sys_cpu_ms,omitempty"`
	TotalCPUMS int64 `json:"total_cpu_ms,omitempty"`
}

// encodeRequest creates a JSON-encoded request.
func encodeRequest(id int64, method string, params any) ([]byte, error) {
	req := Request{
		ID:     id,
		Method: method,
	}
	if params != nil {
		p, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = p
	}
	return json.Marshal(req)
}

// decodeResponse parses a JSON-encoded response.
func decodeResponse(data []byte) (*Response, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}

// =============================================================================
// Callback Protocol - enables Python to make LLM calls back to Go during execution
// =============================================================================

// CallbackRequest is sent by Python when it needs to make an LLM call.
type CallbackRequest struct {
	Callback   string                 `json:"callback"`    // "llm_call" or "llm_batch"
	CallbackID int64                  `json:"callback_id"` // unique ID for this callback
	Params     map[string]interface{} `json:"params"`      // callback-specific params
}

// LLMCallParams contains parameters for an llm_call callback.
type LLMCallParams struct {
	Prompt  string `json:"prompt"`
	Context string `json:"context"`
	Model   string `json:"model"` // "fast", "balanced", "powerful", "reasoning", "auto"
}

// LLMBatchParams contains parameters for an llm_batch callback.
type LLMBatchParams struct {
	Prompts  []string `json:"prompts"`
	Contexts []string `json:"contexts"`
	Model    string   `json:"model"`
}

// CallbackResponse is sent by Go in response to a callback request.
type CallbackResponse struct {
	CallbackID int64  `json:"callback_id"`
	Result     string `json:"result,omitempty"` // for single calls
	Results    []string `json:"results,omitempty"` // for batch calls
	Error      string `json:"error,omitempty"`
}

// IsCallbackRequest checks if a JSON line is a callback request.
func IsCallbackRequest(data []byte) bool {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	_, hasCallback := m["callback"]
	return hasCallback
}

// DecodeCallbackRequest parses a callback request.
func DecodeCallbackRequest(data []byte) (*CallbackRequest, error) {
	var req CallbackRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("unmarshal callback request: %w", err)
	}
	return &req, nil
}

// EncodeCallbackResponse creates a JSON-encoded callback response.
func EncodeCallbackResponse(resp *CallbackResponse) ([]byte, error) {
	return json.Marshal(resp)
}

// CallbackHandler is the interface for handling LLM callbacks from Python.
type CallbackHandler interface {
	// HandleLLMCall handles a single LLM call from Python.
	HandleLLMCall(prompt, context, model string) (string, error)

	// HandleLLMBatch handles a batch of LLM calls from Python.
	HandleLLMBatch(prompts, contexts []string, model string) ([]string, error)
}

// MemoryCallbackHandler handles memory operations from Python.
type MemoryCallbackHandler interface {
	// MemoryQuery searches memory for relevant nodes.
	MemoryQuery(query string, limit int) ([]MemoryNode, error)

	// MemoryAddFact adds a fact to memory.
	MemoryAddFact(content string, confidence float64) (string, error)

	// MemoryAddExperience adds an experience to memory.
	MemoryAddExperience(content, outcome string, success bool) (string, error)

	// MemoryGetContext retrieves recent context nodes.
	MemoryGetContext(limit int) ([]MemoryNode, error)

	// MemoryRelate creates a relationship between nodes.
	MemoryRelate(label, subjectID, objectID string) (string, error)
}

// MemoryNode represents a memory node returned to Python.
type MemoryNode struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
	Tier       string  `json:"tier"`
}

// MemoryQueryParams contains parameters for memory_query callback.
type MemoryQueryParams struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// MemoryAddFactParams contains parameters for memory_add_fact callback.
type MemoryAddFactParams struct {
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
}

// MemoryAddExperienceParams contains parameters for memory_add_experience callback.
type MemoryAddExperienceParams struct {
	Content string `json:"content"`
	Outcome string `json:"outcome"`
	Success bool   `json:"success"`
}

// MemoryRelateParams contains parameters for memory_relate callback.
type MemoryRelateParams struct {
	Label     string `json:"label"`
	SubjectID string `json:"subject_id"`
	ObjectID  string `json:"object_id"`
}
