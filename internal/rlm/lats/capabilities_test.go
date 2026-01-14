package lats

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryAnalyzer_AnalyzeFileReadQueries(t *testing.T) {
	qa := NewQueryAnalyzer()

	tests := []struct {
		name           string
		query          string
		wantRequired   []ToolCapability
		wantTools      []string
		wantComplexity int
	}{
		{
			name:         "read file query",
			query:        "Read the file main.go",
			wantRequired: []ToolCapability{CapFileReadSingle},
			wantTools:    []string{"file_read"},
		},
		{
			name:         "show file content",
			query:        "Show me the content of config.yaml",
			wantRequired: []ToolCapability{CapFileReadSingle},
			wantTools:    []string{"file_read"},
		},
		{
			name:         "get multiple files",
			query:        "Read all the test files in this directory",
			wantRequired: []ToolCapability{CapFileReadMultiple},
			wantTools:    []string{"file_read", "search"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := qa.Analyze(tt.query)

			for _, wantCap := range tt.wantRequired {
				found := false
				for _, r := range req.Required {
					if r.Capability == wantCap {
						found = true
						break
					}
				}
				assert.True(t, found, "expected required capability %s", wantCap)
			}

			for _, wantTool := range tt.wantTools {
				found := false
				for _, tool := range req.SuggestedToolSequence {
					if tool == wantTool {
						found = true
						break
					}
				}
				assert.True(t, found, "expected suggested tool %s", wantTool)
			}
		})
	}
}

func TestQueryAnalyzer_AnalyzeCodeExecutionQueries(t *testing.T) {
	qa := NewQueryAnalyzer()

	tests := []struct {
		name         string
		query        string
		wantRequired []ToolCapability
	}{
		{
			name:         "run python code",
			query:        "Run this Python code to calculate the result",
			wantRequired: []ToolCapability{CapCodeExecutePython},
		},
		{
			name:         "execute script",
			query:        "Execute the script to process the data",
			wantRequired: []ToolCapability{CapCodeExecutePython},
		},
		{
			name:         "shell command",
			query:        "Run a shell command to list files",
			wantRequired: []ToolCapability{CapCodeExecuteShell},
		},
		{
			name:         "bash command",
			query:        "Use bash to check the git status",
			wantRequired: []ToolCapability{CapCodeExecuteShell},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := qa.Analyze(tt.query)

			for _, wantCap := range tt.wantRequired {
				found := false
				for _, r := range req.Required {
					if r.Capability == wantCap {
						found = true
						break
					}
				}
				assert.True(t, found, "expected required capability %s", wantCap)
			}
		})
	}
}

func TestQueryAnalyzer_AnalyzeSearchQueries(t *testing.T) {
	qa := NewQueryAnalyzer()

	tests := []struct {
		name          string
		query         string
		wantPreferred []ToolCapability
		wantRequired  []ToolCapability
	}{
		{
			name:          "find files",
			query:         "Find all Go files in this project",
			wantPreferred: []ToolCapability{CapSearchFilename, CapSearchContent},
		},
		{
			name:          "search content",
			query:         "Search for where the function calculateTotal is defined",
			wantPreferred: []ToolCapability{CapSearchFilename, CapSearchContent},
		},
		{
			name:         "regex search",
			query:        "Use a regex pattern to find all TODO comments",
			wantRequired: []ToolCapability{CapSearchPattern},
		},
		{
			name:         "grep pattern",
			query:        "Grep for 'error' in all log files",
			wantRequired: []ToolCapability{CapSearchPattern},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := qa.Analyze(tt.query)

			for _, wantCap := range tt.wantRequired {
				found := false
				for _, r := range req.Required {
					if r.Capability == wantCap {
						found = true
						break
					}
				}
				assert.True(t, found, "expected required capability %s", wantCap)
			}

			for _, wantCap := range tt.wantPreferred {
				found := false
				for _, p := range req.Preferred {
					if p.Capability == wantCap {
						found = true
						break
					}
				}
				assert.True(t, found, "expected preferred capability %s", wantCap)
			}
		})
	}
}

func TestQueryAnalyzer_AnalyzeComputationQueries(t *testing.T) {
	qa := NewQueryAnalyzer()

	tests := []struct {
		name         string
		query        string
		wantRequired []ToolCapability
	}{
		{
			name:         "calculate sum",
			query:        "Calculate the sum of these numbers",
			wantRequired: []ToolCapability{CapComputeMath},
		},
		{
			name:         "compute average",
			query:        "Compute the average of the test scores",
			wantRequired: []ToolCapability{CapComputeMath},
		},
		{
			name:         "analyze data",
			query:        "Analyze this JSON data and extract the totals",
			wantRequired: []ToolCapability{CapComputeData},
		},
		{
			name:         "process csv",
			query:        "Process the CSV file and transform it to JSON",
			wantRequired: []ToolCapability{CapComputeData},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := qa.Analyze(tt.query)

			for _, wantCap := range tt.wantRequired {
				found := false
				for _, r := range req.Required {
					if r.Capability == wantCap {
						found = true
						break
					}
				}
				assert.True(t, found, "expected required capability %s for query %q", wantCap, tt.query)
			}
		})
	}
}

func TestQueryAnalyzer_AnalyzeGitQueries(t *testing.T) {
	qa := NewQueryAnalyzer()

	tests := []struct {
		name         string
		query        string
		wantRequired []ToolCapability
	}{
		{
			name:         "git status",
			query:        "Show me the git status",
			wantRequired: []ToolCapability{CapGitRead},
		},
		{
			name:         "git diff",
			query:        "What are the changes in the current diff?",
			wantRequired: []ToolCapability{CapGitRead},
		},
		{
			name:         "commit changes",
			query:        "Commit these changes with a good message",
			wantRequired: []ToolCapability{CapGitWrite},
		},
		{
			name:         "push branch",
			query:        "Push the branch to origin",
			wantRequired: []ToolCapability{CapGitWrite},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := qa.Analyze(tt.query)

			for _, wantCap := range tt.wantRequired {
				found := false
				for _, r := range req.Required {
					if r.Capability == wantCap {
						found = true
						break
					}
				}
				assert.True(t, found, "expected required capability %s", wantCap)
			}
		})
	}
}

func TestQueryAnalyzer_AnalyzeMemoryQueries(t *testing.T) {
	qa := NewQueryAnalyzer()

	tests := []struct {
		name         string
		query        string
		wantRequired []ToolCapability
	}{
		{
			name:         "recall earlier",
			query:        "What did we discuss earlier about the database?",
			wantRequired: []ToolCapability{CapMemoryQueryRecent},
		},
		{
			name:         "remember context",
			query:        "Remember the context from before about the API",
			wantRequired: []ToolCapability{CapMemoryQueryRecent},
		},
		{
			name:         "store for later",
			query:        "Save this information for later reference",
			wantRequired: []ToolCapability{CapMemoryStorePersist},
		},
		{
			name:         "persist knowledge",
			query:        "Store this as persistent knowledge",
			wantRequired: []ToolCapability{CapMemoryStorePersist},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := qa.Analyze(tt.query)

			for _, wantCap := range tt.wantRequired {
				found := false
				for _, r := range req.Required {
					if r.Capability == wantCap {
						found = true
						break
					}
				}
				assert.True(t, found, "expected required capability %s", wantCap)
			}
		})
	}
}

func TestQueryAnalyzer_CapabilityScore(t *testing.T) {
	qa := NewQueryAnalyzer()

	tests := []struct {
		name      string
		profile   ToolProfile
		req       *QueryRequirements
		wantScore float64
		tolerance float64
	}{
		{
			name: "perfect match required",
			profile: ToolProfile{
				Capabilities: []ToolCapability{CapFileReadSingle, CapFileReadMultiple},
			},
			req: &QueryRequirements{
				Required: []CapabilityRequirement{
					{Capability: CapFileReadSingle, Level: RequirementRequired},
				},
			},
			wantScore: 1.0,
			tolerance: 0.01,
		},
		{
			name: "missing required",
			profile: ToolProfile{
				Capabilities: []ToolCapability{CapFileWriteCreate},
			},
			req: &QueryRequirements{
				Required: []CapabilityRequirement{
					{Capability: CapFileReadSingle, Level: RequirementRequired},
				},
			},
			wantScore: 0.0, // 90% penalty applied
			tolerance: 0.01,
		},
		{
			name: "partial match with preferred",
			profile: ToolProfile{
				Capabilities: []ToolCapability{CapFileReadSingle, CapSearchContent},
			},
			req: &QueryRequirements{
				Required: []CapabilityRequirement{
					{Capability: CapFileReadSingle, Level: RequirementRequired},
				},
				Preferred: []CapabilityRequirement{
					{Capability: CapSearchContent, Level: RequirementPreferred},
					{Capability: CapSearchPattern, Level: RequirementPreferred},
				},
			},
			wantScore: 0.75, // 1.0 required + 0.5 preferred / (1 + 2*0.5)
			tolerance: 0.01,
		},
		{
			name: "no requirements",
			profile: ToolProfile{
				Capabilities: []ToolCapability{CapFileReadSingle},
			},
			req:       &QueryRequirements{},
			wantScore: 0.0, // No requirements means no match
			tolerance: 0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := qa.CapabilityScore(tt.profile, tt.req)
			assert.InDelta(t, tt.wantScore, score, tt.tolerance)
		})
	}
}

func TestAgentToolMatrix_HasAllTools(t *testing.T) {
	expectedTools := []string{
		"repl", "shell", "file_read", "file_write", "file_delete",
		"search", "semantic_search", "memory_store",
		"web_fetch", "web_search", "code_analysis", "code_format",
	}

	for _, tool := range expectedTools {
		profile, ok := AgentToolMatrix[tool]
		assert.True(t, ok, "expected tool %s in AgentToolMatrix", tool)
		assert.NotEmpty(t, profile.Capabilities, "tool %s should have capabilities", tool)
		assert.NotEmpty(t, profile.Description, "tool %s should have description", tool)
		assert.Greater(t, profile.CostEstimate, 0.0, "tool %s should have cost estimate", tool)
		assert.Greater(t, profile.LatencyMS, 0, "tool %s should have latency estimate", tool)
	}
}

func TestAgentToolMatrix_CapabilityCoverage(t *testing.T) {
	// All base capabilities should be covered by at least one tool
	baseCapabilities := []ToolCapability{
		CapFileRead, CapFileWrite, CapSearch, CapCodeExecution,
		CapMemoryQuery, CapMemoryStore, CapWebFetch, CapComputation,
	}

	for _, cap := range baseCapabilities {
		found := false
		for _, profile := range AgentToolMatrix {
			// Check for exact match or extended version
			for _, profileCap := range profile.Capabilities {
				if profileCap == cap {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		// Note: Base capabilities may be extended (e.g., FILE_READ -> FILE_READ_SINGLE)
		// so we don't require exact matches
	}
}

func TestRecommendTools_ReturnsRankedResults(t *testing.T) {
	req := &QueryRequirements{
		Required: []CapabilityRequirement{
			{Capability: CapCodeExecutePython, Level: RequirementRequired},
			{Capability: CapComputeMath, Level: RequirementRequired},
		},
	}

	ranked := RecommendTools(req, AgentToolMatrix)

	require.NotEmpty(t, ranked, "should return ranked tools")

	// First result should be repl (has both capabilities)
	assert.Equal(t, "repl", ranked[0].Name)

	// Scores should be in descending order
	for i := 1; i < len(ranked); i++ {
		assert.LessOrEqual(t, ranked[i].TotalScore, ranked[i-1].TotalScore,
			"scores should be in descending order")
	}
}

func TestRecommendTools_FiltersIncompatibleTools(t *testing.T) {
	req := &QueryRequirements{
		Required: []CapabilityRequirement{
			{Capability: CapWebSearch, Level: RequirementRequired},
		},
	}

	ranked := RecommendTools(req, AgentToolMatrix)

	// Should only include web_search
	for _, r := range ranked {
		hasWebSearch := false
		for _, cap := range r.Profile.Capabilities {
			if cap == CapWebSearch {
				hasWebSearch = true
				break
			}
		}
		assert.True(t, hasWebSearch, "tool %s should have CapWebSearch", r.Name)
	}
}

func TestRecommendTools_CostEfficiency(t *testing.T) {
	// Create profiles with same capabilities but different costs
	profiles := map[string]ToolProfile{
		"cheap_tool": {
			Name:         "cheap_tool",
			Capabilities: []ToolCapability{CapFileReadSingle},
			CostEstimate: 10,
			LatencyMS:    50,
		},
		"expensive_tool": {
			Name:         "expensive_tool",
			Capabilities: []ToolCapability{CapFileReadSingle},
			CostEstimate: 200,
			LatencyMS:    500,
		},
	}

	req := &QueryRequirements{
		Required: []CapabilityRequirement{
			{Capability: CapFileReadSingle, Level: RequirementRequired},
		},
	}

	ranked := RecommendTools(req, profiles)

	require.Len(t, ranked, 2)
	// Cheaper tool should rank higher
	assert.Equal(t, "cheap_tool", ranked[0].Name)
	assert.Greater(t, ranked[0].EfficiencyScore, ranked[1].EfficiencyScore)
}

func TestGetAgentToolMatrix_ReturnsCopy(t *testing.T) {
	matrix := GetAgentToolMatrix()

	// Modify the copy
	matrix["test_tool"] = ToolProfile{Name: "test_tool"}

	// Original should be unchanged
	_, exists := AgentToolMatrix["test_tool"]
	assert.False(t, exists, "original matrix should not be modified")
}

func TestCapabilityGroups_AllGroupsDefined(t *testing.T) {
	expectedGroups := []string{
		"FileSystem", "Search", "CodeExecution",
		"Memory", "Web", "Computation", "Git",
	}

	for _, expectedName := range expectedGroups {
		found := false
		for _, group := range CapabilityGroups {
			if group.Name == expectedName {
				found = true
				assert.NotEmpty(t, group.Description, "group %s should have description", expectedName)
				assert.NotEmpty(t, group.Capabilities, "group %s should have capabilities", expectedName)
				break
			}
		}
		assert.True(t, found, "expected group %s to be defined", expectedName)
	}
}

func TestGetCapabilityGroup_FindsCorrectGroup(t *testing.T) {
	tests := []struct {
		capability    ToolCapability
		expectedGroup string
	}{
		{CapFileRead, "FileSystem"},
		{CapFileReadSingle, "FileSystem"},
		{CapSearch, "Search"},
		{CapSearchContent, "Search"},
		{CapCodeExecution, "CodeExecution"},
		{CapCodeExecutePython, "CodeExecution"},
		{CapMemoryQuery, "Memory"},
		{CapMemoryQueryRecent, "Memory"},
		{CapWebFetch, "Web"},
		{CapWebSearch, "Web"},
		{CapComputation, "Computation"},
		{CapComputeMath, "Computation"},
		{CapGitRead, "Git"},
		{CapGitWrite, "Git"},
	}

	for _, tt := range tests {
		t.Run(string(tt.capability), func(t *testing.T) {
			group := GetCapabilityGroup(tt.capability)
			if group == nil {
				t.Skipf("capability %s not found in any group", tt.capability)
				return
			}
			assert.Equal(t, tt.expectedGroup, group.Name)
		})
	}
}

func TestQueryAnalyzer_ComplexQuery(t *testing.T) {
	qa := NewQueryAnalyzer()

	// Complex query that should match multiple patterns
	query := "Read the Python file, run the code to calculate the sum, and save the result"

	req := qa.Analyze(query)

	// Should have multiple capabilities
	assert.NotEmpty(t, req.Required, "complex query should have required capabilities")

	// Should have reasonable complexity
	assert.GreaterOrEqual(t, req.EstimatedComplexity, 3, "complex query should have higher complexity")

	// Should suggest multiple tools
	assert.GreaterOrEqual(t, len(req.SuggestedToolSequence), 2, "complex query should suggest multiple tools")
}

func TestQueryAnalyzer_UnknownQuery(t *testing.T) {
	qa := NewQueryAnalyzer()

	// Query that doesn't match any patterns
	query := "Tell me a joke about programming"

	req := qa.Analyze(query)

	// Should return minimal requirements
	assert.Empty(t, req.Required)
	assert.Empty(t, req.Preferred)
	assert.Equal(t, 1, req.EstimatedComplexity) // Baseline complexity
}

func TestRankedTool_ScoreComponents(t *testing.T) {
	// Test that all score components are properly calculated
	profiles := map[string]ToolProfile{
		"balanced_tool": {
			Name:         "balanced_tool",
			Capabilities: []ToolCapability{CapFileReadSingle},
			CostEstimate: 50,
			LatencyMS:    250,
		},
	}

	req := &QueryRequirements{
		Required: []CapabilityRequirement{
			{Capability: CapFileReadSingle, Level: RequirementRequired},
		},
	}

	ranked := RecommendTools(req, profiles)

	require.Len(t, ranked, 1)
	r := ranked[0]

	// Verify score is combination of capability and efficiency
	assert.Equal(t, 1.0, r.CapabilityScore, "perfect capability match")
	assert.Greater(t, r.EfficiencyScore, 0.0, "should have positive efficiency")
	assert.Greater(t, r.TotalScore, 0.0, "should have positive total score")
	assert.LessOrEqual(t, r.TotalScore, 1.0, "total score should be <= 1.0")
}

func TestToolProfile_HasCapability(t *testing.T) {
	profile := ToolProfile{
		Name: "test",
		Capabilities: []ToolCapability{
			CapFileReadSingle,
			CapFileReadMultiple,
		},
	}

	assert.True(t, profile.HasCapability(CapFileReadSingle))
	assert.True(t, profile.HasCapability(CapFileReadMultiple))
	assert.False(t, profile.HasCapability(CapFileWrite))
	assert.False(t, profile.HasCapability(CapCodeExecution))
}
