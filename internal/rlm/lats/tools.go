package lats

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Tool represents a tool that can be executed.
type Tool interface {
	// Name returns the tool name.
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Execute runs the tool with the given input.
	Execute(ctx context.Context, input string) (*ToolResult, error)
}

// ToolResult contains the result of tool execution.
type ToolResult struct {
	// Output is the tool's output.
	Output string

	// Tokens is the token count for this execution.
	Tokens int

	// Success indicates if execution succeeded.
	Success bool

	// Metadata contains additional information.
	Metadata map[string]any
}

// ToolRegistry manages available tools.
type ToolRegistry struct {
	tools map[string]Tool
	mu    sync.RWMutex
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Get returns a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// Has checks if a tool exists.
func (r *ToolRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// Execute runs a tool by name.
func (r *ToolRegistry) Execute(ctx context.Context, name, input string) (*ToolResult, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	return tool.Execute(ctx, input)
}

// Describe returns a description of all available tools.
func (r *ToolRegistry) Describe() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var desc strings.Builder
	for name, tool := range r.tools {
		desc.WriteString(fmt.Sprintf("- %s: %s\n", name, tool.Description()))
	}
	return desc.String()
}

// Names returns the names of all registered tools.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Count returns the number of registered tools.
func (r *ToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// MockTool is a simple tool for testing.
type MockTool struct {
	name        string
	description string
	handler     func(ctx context.Context, input string) (*ToolResult, error)
}

// NewMockTool creates a mock tool.
func NewMockTool(name, description string, handler func(ctx context.Context, input string) (*ToolResult, error)) *MockTool {
	return &MockTool{
		name:        name,
		description: description,
		handler:     handler,
	}
}

// Name returns the tool name.
func (t *MockTool) Name() string {
	return t.name
}

// Description returns the tool description.
func (t *MockTool) Description() string {
	return t.description
}

// Execute runs the mock handler.
func (t *MockTool) Execute(ctx context.Context, input string) (*ToolResult, error) {
	if t.handler != nil {
		return t.handler(ctx, input)
	}
	return &ToolResult{
		Output:  fmt.Sprintf("Mock output for %s: %s", t.name, input),
		Success: true,
		Tokens:  10,
	}, nil
}

// ToolCapability represents a tool's capability.
type ToolCapability string

const (
	CapFileRead      ToolCapability = "FILE_READ"
	CapFileWrite     ToolCapability = "FILE_WRITE"
	CapCodeExecution ToolCapability = "CODE_EXECUTION"
	CapSearch        ToolCapability = "SEARCH"
	CapMemoryQuery   ToolCapability = "MEMORY_QUERY"
	CapMemoryStore   ToolCapability = "MEMORY_STORE"
	CapWebFetch      ToolCapability = "WEB_FETCH"
	CapComputation   ToolCapability = "COMPUTATION"
)

// ToolProfile describes a tool's capabilities and costs.
type ToolProfile struct {
	// Name is the tool name.
	Name string

	// Capabilities are what this tool can do.
	Capabilities []ToolCapability

	// CostEstimate is the estimated token cost per call.
	CostEstimate float64

	// LatencyMS is the estimated latency in milliseconds.
	LatencyMS int

	// Description is a human-readable description.
	Description string
}

// HasCapability checks if the profile has a capability.
func (p *ToolProfile) HasCapability(cap ToolCapability) bool {
	for _, c := range p.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// DefaultToolProfiles returns standard tool profiles.
func DefaultToolProfiles() map[string]ToolProfile {
	return map[string]ToolProfile{
		"repl": {
			Name:         "repl",
			Capabilities: []ToolCapability{CapCodeExecution, CapComputation},
			CostEstimate: 50,
			LatencyMS:    500,
			Description:  "Execute Python code in REPL",
		},
		"search": {
			Name:         "search",
			Capabilities: []ToolCapability{CapSearch, CapMemoryQuery},
			CostEstimate: 20,
			LatencyMS:    100,
			Description:  "Search memory for relevant information",
		},
		"file_read": {
			Name:         "file_read",
			Capabilities: []ToolCapability{CapFileRead},
			CostEstimate: 10,
			LatencyMS:    50,
			Description:  "Read file contents",
		},
		"file_write": {
			Name:         "file_write",
			Capabilities: []ToolCapability{CapFileWrite},
			CostEstimate: 15,
			LatencyMS:    100,
			Description:  "Write content to file",
		},
		"memory_store": {
			Name:         "memory_store",
			Capabilities: []ToolCapability{CapMemoryStore},
			CostEstimate: 30,
			LatencyMS:    150,
			Description:  "Store information in memory",
		},
	}
}

// CapabilityMatcher helps find tools by capability.
type CapabilityMatcher struct {
	profiles map[string]ToolProfile
}

// NewCapabilityMatcher creates a capability matcher.
func NewCapabilityMatcher(profiles map[string]ToolProfile) *CapabilityMatcher {
	return &CapabilityMatcher{profiles: profiles}
}

// FindByCapability returns tools with the given capability.
func (m *CapabilityMatcher) FindByCapability(cap ToolCapability) []string {
	var tools []string
	for name, profile := range m.profiles {
		if profile.HasCapability(cap) {
			tools = append(tools, name)
		}
	}
	return tools
}

// FindByCapabilities returns tools with all given capabilities.
func (m *CapabilityMatcher) FindByCapabilities(caps []ToolCapability) []string {
	var tools []string
	for name, profile := range m.profiles {
		hasAll := true
		for _, cap := range caps {
			if !profile.HasCapability(cap) {
				hasAll = false
				break
			}
		}
		if hasAll {
			tools = append(tools, name)
		}
	}
	return tools
}

// BestToolFor returns the best tool for a capability (lowest cost).
func (m *CapabilityMatcher) BestToolFor(cap ToolCapability) (string, bool) {
	var bestName string
	var bestCost float64 = -1

	for name, profile := range m.profiles {
		if profile.HasCapability(cap) {
			if bestCost < 0 || profile.CostEstimate < bestCost {
				bestName = name
				bestCost = profile.CostEstimate
			}
		}
	}

	return bestName, bestName != ""
}
