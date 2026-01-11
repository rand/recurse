package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	// pythonPath is the path to the Python interpreter.
	pythonPath string

	// bootstrapPath is the path to the bootstrap.py script.
	bootstrapPath string

	// workDir is the working directory for the REPL.
	workDir string
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
		opts.PythonPath = "python3"
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

	return "", fmt.Errorf("bootstrap.py not found (searched from %s)", cwd)
}

// Start launches the Python REPL subprocess.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running.Load() {
		return fmt.Errorf("REPL already running")
	}

	// Build command with sandbox environment
	cmd := exec.CommandContext(ctx, m.pythonPath, "-u", m.bootstrapPath)
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

	// Wait for ready signal
	if err := m.waitReady(ctx); err != nil {
		m.Stop()
		return fmt.Errorf("wait ready: %w", err)
	}

	return nil
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

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- m.cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		m.cmd.Process.Kill()
	}

	return nil
}

// Running returns true if the REPL is running.
func (m *Manager) Running() bool {
	return m.running.Load()
}

// Execute runs Python code and returns the result.
func (m *Manager) Execute(ctx context.Context, code string) (*ExecuteResult, error) {
	resp, err := m.call(ctx, "execute", ExecuteParams{Code: code})
	if err != nil {
		return nil, err
	}

	var result ExecuteResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}
	return &result, nil
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
