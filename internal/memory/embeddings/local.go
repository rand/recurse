package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultLocalURL        = "http://127.0.0.1:11435"
	defaultLocalModel      = "nomic-ai/CodeRankEmbed"
	defaultLocalDimensions = 768 // CodeRankEmbed outputs 768-dim vectors
	defaultStartupTimeout  = 120 * time.Second
	defaultLocalTimeout    = 60 * time.Second
)

// LocalProvider generates embeddings using a local Python server.
// It uses CodeRankEmbed by default, which achieves state-of-the-art
// code retrieval performance at only 137M parameters.
type LocalProvider struct {
	baseURL    string
	model      string
	httpClient *http.Client
	dimensions int
	isQuery    bool // Whether to add query prefix (for CodeRankEmbed)

	// Server management
	serverCmd  *exec.Cmd
	serverLock sync.Mutex
	autoStart  bool
}

// LocalConfig configures the local provider.
type LocalConfig struct {
	BaseURL   string        // Server URL (default: http://127.0.0.1:11435)
	Model     string        // Model name (default: nomic-ai/CodeRankEmbed)
	Timeout   time.Duration // HTTP timeout (default: 60s)
	AutoStart bool          // Auto-start server if not running (default: true)
	IsQuery   bool          // Whether embeddings are for queries vs documents
}

// LocalOption is a functional option for LocalProvider.
type LocalOption func(*LocalConfig)

// WithLocalURL sets the server URL.
func WithLocalURL(url string) LocalOption {
	return func(c *LocalConfig) {
		c.BaseURL = url
	}
}

// WithLocalModel sets the model name.
func WithLocalModel(model string) LocalOption {
	return func(c *LocalConfig) {
		c.Model = model
	}
}

// WithLocalTimeout sets the HTTP timeout.
func WithLocalTimeout(d time.Duration) LocalOption {
	return func(c *LocalConfig) {
		c.Timeout = d
	}
}

// WithAutoStart enables auto-starting the server.
func WithAutoStart(autoStart bool) LocalOption {
	return func(c *LocalConfig) {
		c.AutoStart = autoStart
	}
}

// WithQueryMode sets whether embeddings are for queries.
// When true, CodeRankEmbed adds the required prefix for query embeddings.
func WithQueryMode(isQuery bool) LocalOption {
	return func(c *LocalConfig) {
		c.IsQuery = isQuery
	}
}

// NewLocalProvider creates a new local embedding provider.
func NewLocalProvider(opts ...LocalOption) (*LocalProvider, error) {
	cfg := LocalConfig{
		BaseURL:   defaultLocalURL,
		Model:     defaultLocalModel,
		Timeout:   defaultLocalTimeout,
		AutoStart: true,
		IsQuery:   false,
	}

	// Check environment overrides
	if url := os.Getenv("EMBEDDING_SERVER_URL"); url != "" {
		cfg.BaseURL = url
	}
	if model := os.Getenv("EMBEDDING_MODEL"); model != "" {
		cfg.Model = model
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	p := &LocalProvider{
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		dimensions: localDimensionsForModel(cfg.Model),
		autoStart:  cfg.AutoStart,
		isQuery:    cfg.IsQuery,
	}

	return p, nil
}

func localDimensionsForModel(model string) int {
	switch model {
	case "nomic-ai/CodeRankEmbed":
		return 768
	case "nomic-ai/nomic-embed-text-v1.5":
		return 768
	case "nomic-ai/nomic-embed-code":
		return 768
	default:
		return defaultLocalDimensions
	}
}

// Embed generates embeddings for the given texts.
func (p *LocalProvider) Embed(ctx context.Context, texts []string) ([]Vector, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Check if server is running, start if needed
	if err := p.ensureServerRunning(ctx); err != nil {
		return nil, fmt.Errorf("ensure server: %w", err)
	}

	req := localRequest{
		Input:   texts,
		IsQuery: p.isQuery,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("local embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("local server error %d: %s", resp.StatusCode, string(respBody))
	}

	var result localResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(result.Data))
	}

	vectors := make([]Vector, len(result.Data))
	for i, d := range result.Data {
		vectors[i] = d.Embedding
	}

	return vectors, nil
}

// Dimensions returns the embedding dimension.
func (p *LocalProvider) Dimensions() int {
	return p.dimensions
}

// Model returns the model identifier.
func (p *LocalProvider) Model() string {
	return p.model
}

// IsRunning checks if the embedding server is running.
func (p *LocalProvider) IsRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// ensureServerRunning starts the server if not running and autoStart is enabled.
func (p *LocalProvider) ensureServerRunning(ctx context.Context) error {
	if p.IsRunning() {
		return nil
	}

	if !p.autoStart {
		return errors.New("embedding server not running (auto-start disabled)")
	}

	p.serverLock.Lock()
	defer p.serverLock.Unlock()

	// Double-check after acquiring lock
	if p.IsRunning() {
		return nil
	}

	return p.startServer(ctx)
}

// startServer starts the local embedding server.
func (p *LocalProvider) startServer(ctx context.Context) error {
	// Find the embedding server script
	scriptPath, err := findEmbeddingServerScript()
	if err != nil {
		return fmt.Errorf("find server script: %w", err)
	}

	// Find the venv Python in the same directory as the script
	// The venv is expected at .venv-embeddings/bin/python3
	scriptDir := filepath.Dir(scriptPath)
	venvPython := filepath.Join(scriptDir, ".venv-embeddings", "bin", "python3")

	// Use venv Python if it exists, otherwise fall back to system python3
	pythonPath := "python3"
	if _, err := os.Stat(venvPython); err == nil {
		pythonPath = venvPython
		slog.Info("Using venv Python for embedding server", "path", venvPython)
	} else {
		slog.Warn("Venv Python not found, using system python3", "expected", venvPython)
	}

	// Start the server
	p.serverCmd = exec.Command(pythonPath, scriptPath, "--model", p.model)
	p.serverCmd.Stdout = os.Stdout
	p.serverCmd.Stderr = os.Stderr

	if err := p.serverCmd.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	// Wait for server to be ready
	deadline := time.Now().Add(defaultStartupTimeout)
	for time.Now().Before(deadline) {
		if p.IsRunning() {
			return nil
		}
		select {
		case <-ctx.Done():
			p.serverCmd.Process.Kill()
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	p.serverCmd.Process.Kill()
	return errors.New("timeout waiting for embedding server to start")
}

// Stop stops the embedding server if it was started by this provider.
func (p *LocalProvider) Stop() error {
	p.serverLock.Lock()
	defer p.serverLock.Unlock()

	if p.serverCmd != nil && p.serverCmd.Process != nil {
		return p.serverCmd.Process.Kill()
	}
	return nil
}

// findEmbeddingServerScript locates the embedding server Python script.
func findEmbeddingServerScript() (string, error) {
	// Try relative to current executable
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates := []string{
			filepath.Join(dir, "pkg", "python", "embedding_server.py"),
			filepath.Join(dir, "..", "pkg", "python", "embedding_server.py"),
			filepath.Join(dir, "..", "..", "pkg", "python", "embedding_server.py"),
		}
		for _, path := range candidates {
			if _, err := os.Stat(path); err == nil {
				return filepath.Abs(path)
			}
		}
	}

	// Try relative to working directory
	if cwd, err := os.Getwd(); err == nil {
		candidates := []string{
			filepath.Join(cwd, "pkg", "python", "embedding_server.py"),
			filepath.Join(cwd, "..", "pkg", "python", "embedding_server.py"),
		}
		for _, path := range candidates {
			if _, err := os.Stat(path); err == nil {
				return filepath.Abs(path)
			}
		}
	}

	// Try GOPATH
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		path := filepath.Join(gopath, "src", "github.com", "rand", "recurse", "pkg", "python", "embedding_server.py")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", errors.New("embedding_server.py not found")
}

type localRequest struct {
	Input   []string `json:"input"`
	IsQuery bool     `json:"is_query"`
}

type localResponse struct {
	Data []struct {
		Embedding Vector `json:"embedding"`
		Index     int    `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		TotalTexts int `json:"total_texts"`
		DurationMs int `json:"duration_ms"`
	} `json:"usage"`
}
