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
