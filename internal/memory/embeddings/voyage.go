package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"golang.org/x/time/rate"
)

const (
	voyageAPIURL      = "https://api.voyageai.com/v1/embeddings"
	defaultModel      = "voyage-3"
	defaultRateLimit  = 10.0 // requests per second
	defaultTimeout    = 30 * time.Second
	defaultDimensions = 1024
)

// VoyageProvider generates embeddings using the Voyage AI API.
type VoyageProvider struct {
	apiKey     string
	model      string
	httpClient *http.Client
	rateLimit  *rate.Limiter
	dimensions int
}

// VoyageConfig configures the Voyage provider.
type VoyageConfig struct {
	APIKey    string        // Required: Voyage API key (or set VOYAGE_API_KEY env var)
	Model     string        // Model to use: "voyage-3" (default), "voyage-code-3", "voyage-3-lite"
	RateLimit float64       // Requests per second (default: 10)
	Timeout   time.Duration // HTTP timeout (default: 30s)
}

// VoyageOption is a functional option for VoyageProvider.
type VoyageOption func(*VoyageConfig)

// WithAPIKey sets the API key.
func WithAPIKey(key string) VoyageOption {
	return func(c *VoyageConfig) {
		c.APIKey = key
	}
}

// WithModel sets the embedding model.
func WithModel(model string) VoyageOption {
	return func(c *VoyageConfig) {
		c.Model = model
	}
}

// WithRateLimit sets the rate limit in requests per second.
func WithRateLimit(rps float64) VoyageOption {
	return func(c *VoyageConfig) {
		c.RateLimit = rps
	}
}

// WithTimeout sets the HTTP request timeout.
func WithTimeout(d time.Duration) VoyageOption {
	return func(c *VoyageConfig) {
		c.Timeout = d
	}
}

// NewVoyageProvider creates a new Voyage embedding provider.
func NewVoyageProvider(opts ...VoyageOption) (*VoyageProvider, error) {
	cfg := VoyageConfig{
		APIKey:    os.Getenv("VOYAGE_API_KEY"),
		Model:     defaultModel,
		RateLimit: defaultRateLimit,
		Timeout:   defaultTimeout,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.APIKey == "" {
		return nil, errors.New("voyage API key required: set VOYAGE_API_KEY or use WithAPIKey()")
	}

	return &VoyageProvider{
		apiKey: cfg.APIKey,
		model:  cfg.Model,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		rateLimit:  rate.NewLimiter(rate.Limit(cfg.RateLimit), 1),
		dimensions: dimensionsForModel(cfg.Model),
	}, nil
}

func dimensionsForModel(model string) int {
	switch model {
	case "voyage-3", "voyage-code-3", "voyage-3-lite":
		return 1024
	case "voyage-3-large":
		return 1024
	default:
		return defaultDimensions
	}
}

// Embed generates embeddings for the given texts.
func (p *VoyageProvider) Embed(ctx context.Context, texts []string) ([]Vector, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Respect rate limit
	if err := p.rateLimit.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	req := voyageRequest{
		Model: p.model,
		Input: texts,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", voyageAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("voyage request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("voyage API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result voyageResponse
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

// Dimensions returns the embedding dimension for this model.
func (p *VoyageProvider) Dimensions() int {
	return p.dimensions
}

// Model returns the model identifier.
func (p *VoyageProvider) Model() string {
	return p.model
}

type voyageRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type voyageResponse struct {
	Data []struct {
		Embedding Vector `json:"embedding"`
		Index     int    `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}
