package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Manager manages a Python REPL subprocess for RLM orchestration.
type Manager struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	stderr   io.ReadCloser
	sandbox  SandboxConfig
	reqID    atomic.Int64
	running  atomic.Bool
	startedAt time.Time

	// exitErr stores the error from process exit, if any.
	exitErr error

	// pythonPath is the path to the Python interpreter.
	pythonPath string

	// bootstrapPath is the path to the bootstrap.py script.
	bootstrapPath string

	// workDir is the working directory for the REPL.
	workDir string

	// callbackHandler handles LLM callbacks from Python during execution.
	callbackHandler CallbackHandler

	// memoryHandler handles memory callbacks from Python during execution.
	memoryHandler MemoryCallbackHandler

	// resourceMonitor tracks resource usage for the REPL process.
	resourceMonitor *ResourceMonitor

	// resourceCallback is called when resource events occur.
	resourceCallback ResourceCallback

	// pluginManager handles plugin function calls.
	pluginManager *PluginManager
}

// Options configures the REPL manager.
type Options struct {
	// PythonPath overrides the default Python interpreter.
	PythonPath string

	// BootstrapPath overrides the path to bootstrap.py.
	BootstrapPath string

	// WorkDir sets the working directory. Defaults to cwd.
	WorkDir string

	// Sandbox configures execution constraints.
	Sandbox SandboxConfig
}

// NewManager creates a new REPL manager with the given options.
func NewManager(opts Options) (*Manager, error) {
	if opts.PythonPath == "" {
		// Try to use uv-managed venv Python, fall back to system python3
		pythonPath, err := EnsureEnv(context.Background())
		if err != nil {
			slog.Debug("Failed to ensure uv env, using system python3", "error", err)
			opts.PythonPath = "python3"
		} else {
			opts.PythonPath = pythonPath
		}
	}

	if opts.WorkDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get cwd: %w", err)
		}
		opts.WorkDir = cwd
	}

	if err := opts.Sandbox.Validate(); err != nil {
		return nil, fmt.Errorf("invalid sandbox config: %w", err)
	}

	// Use provided bootstrap path or find it
	bootstrapPath := opts.BootstrapPath
	if bootstrapPath == "" {
		var err error
		bootstrapPath, err = findBootstrap()
		if err != nil {
			return nil, fmt.Errorf("find bootstrap: %w", err)
		}
	}

	return &Manager{
		pythonPath:    opts.PythonPath,
		bootstrapPath: bootstrapPath,
		workDir:       opts.WorkDir,
		sandbox:       opts.Sandbox,
	}, nil
}

// findBootstrap locates the bootstrap.py script.
// First tries filesystem locations (for development), then falls back to embedded version.
func findBootstrap() (string, error) {
	// Try relative to executable first
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		candidates := []string{
			filepath.Join(exeDir, "pkg", "python", "bootstrap.py"),
			filepath.Join(exeDir, "..", "pkg", "python", "bootstrap.py"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return filepath.Abs(p)
			}
		}
	}

	// Try relative to cwd (for development)
	cwd, _ := os.Getwd()

	// Walk up from cwd to find pkg/python/bootstrap.py (handles running from subdir)
	dir := cwd
	for i := 0; i < 10; i++ { // limit depth
		candidate := filepath.Join(dir, "pkg", "python", "bootstrap.py")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Abs(candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fall back to embedded bootstrap (for installed binaries)
	slog.Debug("bootstrap.py not found on filesystem, using embedded version")
	return extractEmbeddedBootstrap()
}

// Start launches the Python REPL subprocess.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running.Load() {
		return fmt.Errorf("REPL already running")
	}

	// Build command with sandbox environment
	// NOTE: We use exec.Command (not CommandContext) because the REPL process
	// should outlive the startup context. CommandContext would kill the process
	// when the context is cancelled, which happens immediately after startup.
	cmd := exec.Command(m.pythonPath, "-u", m.bootstrapPath)
	cmd.Dir = m.workDir
	cmd.Env = append(os.Environ(), m.sandbox.ToEnv()...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return fmt.Errorf("start process: %w", err)
	}

	m.cmd = cmd
	m.stdin = stdin
	m.stdout = bufio.NewReader(stdout)
	m.stderr = stderr
	m.running.Store(true)
	m.startedAt = time.Now()

	// Initialize resource monitor
	m.resourceMonitor = NewResourceMonitor(cmd.Process.Pid, m.sandbox.Resources)

	// Wait for ready signal
	if err := m.waitReady(ctx); err != nil {
		m.stopLocked()
		return fmt.Errorf("wait ready: %w", err)
	}

	// Capture baseline resource usage after startup
	if err := m.resourceMonitor.CaptureBaseline(); err != nil {
		// Non-fatal, just log
		slog.Debug("failed to capture baseline resource usage", "error", err)
	}

	// Start process monitor goroutine to detect unexpected exits
	go m.monitorProcess()

	return nil
}

// monitorProcess watches for process exit and updates the running state.
func (m *Manager) monitorProcess() {
	if m.cmd == nil || m.cmd.Process == nil {
		return
	}

	// Wait for the process to exit
	err := m.cmd.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Only update if we're still considered "running" (not a graceful stop)
	if m.running.Load() {
		m.running.Store(false)
		if err != nil {
			m.exitErr = fmt.Errorf("REPL process exited unexpectedly: %w", err)
			slog.Warn("REPL process exited unexpectedly", "error", err)
		} else {
			m.exitErr = fmt.Errorf("REPL process exited unexpectedly with status 0")
			slog.Warn("REPL process exited unexpectedly with status 0")
		}
	}
}

// waitReady waits for the REPL to signal it's ready.
func (m *Manager) waitReady(ctx context.Context) error {
	// Read the ready message (first line of output)
	readyCh := make(chan error, 1)
	go func() {
		line, err := m.stdout.ReadString('\n')
		if err != nil {
			readyCh <- fmt.Errorf("read ready: %w", err)
			return
		}
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			readyCh <- fmt.Errorf("parse ready: %w", err)
			return
		}
		if resp.Error != nil {
			readyCh <- resp.Error
			return
		}
		readyCh <- nil
	}()

	select {
	case err := <-readyCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for REPL ready")
	}
}

// Stop terminates the Python REPL subprocess.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

// stopLocked terminates the REPL subprocess. Caller must hold m.mu.
func (m *Manager) stopLocked() error {
	if !m.running.Load() {
		return nil
	}

	m.running.Store(false)

	// Send shutdown request
	if m.stdin != nil {
		req, _ := encodeRequest(0, "shutdown", nil)
		m.stdin.Write(req)
		m.stdin.Write([]byte("\n"))
		m.stdin.Close()
	}

	// The monitorProcess goroutine is already waiting on the process.
	// Give it time to exit gracefully, then force kill if needed.
	if m.cmd != nil && m.cmd.Process != nil {
		// Wait for graceful shutdown with timeout
		gracefulDone := make(chan struct{})
		go func() {
			// Poll until process exits or timeout
			for i := 0; i < 50; i++ { // 5 seconds total
				time.Sleep(100 * time.Millisecond)
				if m.cmd.ProcessState != nil {
					close(gracefulDone)
					return
				}
			}
		}()

		select {
		case <-gracefulDone:
			// Process exited gracefully
		case <-time.After(5 * time.Second):
			// Force kill
			m.cmd.Process.Kill()
		}
	}

	return nil
}

// Running returns true if the REPL is running.
func (m *Manager) Running() bool {
	return m.running.Load()
}

// ExitError returns the error from an unexpected process exit, if any.
func (m *Manager) ExitError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exitErr
}

// SetCallbackHandler sets the handler for LLM callbacks from Python.
// This must be set before Execute() to enable llm_call() in Python code.
func (m *Manager) SetCallbackHandler(handler CallbackHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbackHandler = handler
}

// SetMemoryHandler sets the handler for memory callbacks from Python.
// This must be set before Execute() to enable memory_* functions in Python code.
func (m *Manager) SetMemoryHandler(handler MemoryCallbackHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryHandler = handler
}

// SetResourceCallback sets the callback for resource events.
func (m *Manager) SetResourceCallback(callback ResourceCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resourceCallback = callback
}

// ResourceStats returns the cumulative resource usage for this REPL session.
func (m *Manager) ResourceStats() *ResourceStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resourceMonitor == nil {
		return nil
	}
	stats := m.resourceMonitor.CumulativeStats()
	return &stats
}

// Execute runs Python code and returns the result.
// If the code calls llm_call() or llm_batch(), these are handled via callbacks
// to the registered CallbackHandler.
func (m *Manager) Execute(ctx context.Context, code string) (*ExecuteResult, error) {
	return m.executeWithCallbacks(ctx, code)
}

// executeWithCallbacks handles code execution with potential LLM callbacks.
func (m *Manager) executeWithCallbacks(ctx context.Context, code string) (*ExecuteResult, error) {
	if !m.running.Load() {
		return nil, fmt.Errorf("REPL not running")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.reqID.Add(1)
	req, err := encodeRequest(id, "execute", ExecuteParams{Code: code})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	// Send execute request
	if _, err := m.stdin.Write(req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	if _, err := m.stdin.Write([]byte("\n")); err != nil {
		return nil, fmt.Errorf("write newline: %w", err)
	}

	// Read response with timeout, handling callbacks
	timeout := m.sandbox.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	deadline := time.Now().Add(timeout)

	for {
		// Check context and timeout
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout after %v", timeout)
		}

		// Read next line
		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			line, err := m.stdout.ReadString('\n')
			if err != nil {
				errCh <- fmt.Errorf("read response: %w", err)
				return
			}
			lineCh <- line
		}()

		var line string
		select {
		case line = <-lineCh:
		case err := <-errCh:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Until(deadline)):
			return nil, fmt.Errorf("timeout after %v", timeout)
		}

		lineBytes := []byte(line)

		// Check if this is a callback request
		if IsCallbackRequest(lineBytes) {
			if err := m.handleCallback(ctx, lineBytes); err != nil {
				return nil, fmt.Errorf("handle callback: %w", err)
			}
			continue // Read next line
		}

		// Must be the final response
		resp, err := decodeResponse(lineBytes)
		if err != nil {
			return nil, err
		}
		if resp.Error != nil {
			return nil, resp.Error
		}

		var result ExecuteResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("unmarshal result: %w", err)
		}
		return &result, nil
	}
}

// handleCallback processes a callback request from Python and sends the response.
func (m *Manager) handleCallback(ctx context.Context, data []byte) error {
	req, err := DecodeCallbackRequest(data)
	if err != nil {
		return err
	}

	var resp CallbackResponse
	resp.CallbackID = req.CallbackID

	switch req.Callback {
	// LLM callbacks
	case "llm_call":
		if m.callbackHandler == nil {
			resp.Error = "LLM callback handler not configured"
		} else {
			prompt, _ := req.Params["prompt"].(string)
			context, _ := req.Params["context"].(string)
			model, _ := req.Params["model"].(string)

			result, err := m.callbackHandler.HandleLLMCall(prompt, context, model)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = result
			}
		}

	case "llm_batch":
		if m.callbackHandler == nil {
			resp.Error = "LLM callback handler not configured"
		} else {
			promptsRaw, _ := req.Params["prompts"].([]interface{})
			contextsRaw, _ := req.Params["contexts"].([]interface{})
			model, _ := req.Params["model"].(string)

			prompts := make([]string, len(promptsRaw))
			for i, p := range promptsRaw {
				prompts[i], _ = p.(string)
			}
			contexts := make([]string, len(contextsRaw))
			for i, c := range contextsRaw {
				contexts[i], _ = c.(string)
			}

			results, err := m.callbackHandler.HandleLLMBatch(prompts, contexts, model)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Results = results
			}
		}

	// Memory callbacks
	case "memory_query":
		if m.memoryHandler == nil {
			resp.Error = "Memory callback handler not configured"
		} else {
			query, _ := req.Params["query"].(string)
			limit, _ := req.Params["limit"].(float64) // JSON numbers are float64

			nodes, err := m.memoryHandler.MemoryQuery(query, int(limit))
			if err != nil {
				resp.Error = err.Error()
			} else {
				// Encode nodes as JSON for the result
				nodesJSON, _ := json.Marshal(nodes)
				resp.Result = string(nodesJSON)
			}
		}

	case "memory_add_fact":
		if m.memoryHandler == nil {
			resp.Error = "Memory callback handler not configured"
		} else {
			content, _ := req.Params["content"].(string)
			confidence, _ := req.Params["confidence"].(float64)

			nodeID, err := m.memoryHandler.MemoryAddFact(content, confidence)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = nodeID
			}
		}

	case "memory_add_experience":
		if m.memoryHandler == nil {
			resp.Error = "Memory callback handler not configured"
		} else {
			// Parse params including extended fields [SPEC-09.02]
			params := MemoryAddExperienceParams{
				Content: getString(req.Params, "content"),
				Outcome: getString(req.Params, "outcome"),
				Success: getBool(req.Params, "success"),
				// Extended fields
				TaskDescription:  getString(req.Params, "task_description"),
				Approach:         getString(req.Params, "approach"),
				FilesModified:    getStringSlice(req.Params, "files_modified"),
				BlockersHit:      getStringSlice(req.Params, "blockers_hit"),
				InsightsGained:   getStringSlice(req.Params, "insights_gained"),
				RelatedDecisions: getStringSlice(req.Params, "related_decisions"),
				DurationSecs:     getInt(req.Params, "duration_secs"),
			}

			// Use extended method if any extended fields are present
			var nodeID string
			var err error
			if params.HasExtendedFields() {
				nodeID, err = m.memoryHandler.MemoryAddExperienceWithOptions(params)
			} else {
				nodeID, err = m.memoryHandler.MemoryAddExperience(params.Content, params.Outcome, params.Success)
			}
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = nodeID
			}
		}

	case "memory_get_context":
		if m.memoryHandler == nil {
			resp.Error = "Memory callback handler not configured"
		} else {
			limit, _ := req.Params["limit"].(float64)

			nodes, err := m.memoryHandler.MemoryGetContext(int(limit))
			if err != nil {
				resp.Error = err.Error()
			} else {
				nodesJSON, _ := json.Marshal(nodes)
				resp.Result = string(nodesJSON)
			}
		}

	case "memory_relate":
		if m.memoryHandler == nil {
			resp.Error = "Memory callback handler not configured"
		} else {
			label, _ := req.Params["label"].(string)
			subjectID, _ := req.Params["subject_id"].(string)
			objectID, _ := req.Params["object_id"].(string)

			edgeID, err := m.memoryHandler.MemoryRelate(label, subjectID, objectID)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = edgeID
			}
		}

	// Plugin function callbacks
	case "plugin_call":
		if m.pluginManager == nil {
			resp.Error = "Plugin manager not configured"
		} else {
			funcName, _ := req.Params["function"].(string)
			argsRaw, _ := req.Params["args"].([]interface{})

			result, err := m.pluginManager.Call(ctx, funcName, argsRaw...)
			if err != nil {
				resp.Error = err.Error()
			} else {
				// Encode result as JSON
				resultJSON, _ := json.Marshal(result)
				resp.Result = string(resultJSON)
			}
		}

	case "plugin_list":
		if m.pluginManager == nil {
			resp.Error = "Plugin manager not configured"
		} else {
			manifest := m.pluginManager.GenerateManifest()
			resp.Result = manifest
		}

	default:
		resp.Error = fmt.Sprintf("unknown callback: %s", req.Callback)
	}

	// Send response back to Python
	respBytes, err := EncodeCallbackResponse(&resp)
	if err != nil {
		return fmt.Errorf("encode callback response: %w", err)
	}

	if _, err := m.stdin.Write(respBytes); err != nil {
		return fmt.Errorf("write callback response: %w", err)
	}
	if _, err := m.stdin.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write newline: %w", err)
	}

	return nil
}

// SetVar stores a string value as a named variable in the REPL.
func (m *Manager) SetVar(ctx context.Context, name, value string) error {
	_, err := m.call(ctx, "set_var", SetVarParams{Name: name, Value: value})
	return err
}

// GetVar retrieves a variable's value, optionally sliced.
func (m *Manager) GetVar(ctx context.Context, name string, start, end int) (*GetVarResult, error) {
	resp, err := m.call(ctx, "get_var", GetVarParams{
		Name:  name,
		Start: start,
		End:   end,
	})
	if err != nil {
		return nil, err
	}

	var result GetVarResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}
	return &result, nil
}

// ListVars returns all variables in the REPL namespace.
func (m *Manager) ListVars(ctx context.Context) (*ListVarsResult, error) {
	resp, err := m.call(ctx, "list_vars", nil)
	if err != nil {
		return nil, err
	}

	var result ListVarsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}
	return &result, nil
}

// Status returns the REPL status.
func (m *Manager) Status(ctx context.Context) (*StatusResult, error) {
	if !m.running.Load() {
		return &StatusResult{Running: false}, nil
	}

	resp, err := m.call(ctx, "status", nil)
	if err != nil {
		return nil, err
	}

	var result StatusResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}
	result.Running = true
	return &result, nil
}

// call sends a request to the REPL and waits for a response.
func (m *Manager) call(ctx context.Context, method string, params any) (*Response, error) {
	if !m.running.Load() {
		return nil, fmt.Errorf("REPL not running")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.reqID.Add(1)
	req, err := encodeRequest(id, method, params)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	// Send request
	if _, err := m.stdin.Write(req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	if _, err := m.stdin.Write([]byte("\n")); err != nil {
		return nil, fmt.Errorf("write newline: %w", err)
	}

	// Read response with timeout
	timeout := m.sandbox.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	respCh := make(chan *Response, 1)
	errCh := make(chan error, 1)

	go func() {
		line, err := m.stdout.ReadString('\n')
		if err != nil {
			errCh <- fmt.Errorf("read response: %w", err)
			return
		}
		resp, err := decodeResponse([]byte(line))
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout after %v", timeout)
	}
}

// Helper functions for parsing callback params

func getString(params map[string]any, key string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return ""
}

func getBool(params map[string]any, key string) bool {
	if v, ok := params[key].(bool); ok {
		return v
	}
	return false
}

func getInt(params map[string]any, key string) int {
	if v, ok := params[key].(float64); ok {
		return int(v)
	}
	return 0
}

func getStringSlice(params map[string]any, key string) []string {
	if v, ok := params[key].([]any); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
