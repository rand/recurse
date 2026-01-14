package lats

import (
	"regexp"
	"strings"
)

// Extended capability constants for fine-grained tool matching.
const (
	// File system capabilities
	CapFileReadSingle   ToolCapability = "FILE_READ_SINGLE"   // Read a single file
	CapFileReadMultiple ToolCapability = "FILE_READ_MULTIPLE" // Read multiple files
	CapFileWriteCreate  ToolCapability = "FILE_WRITE_CREATE"  // Create new files
	CapFileWriteModify  ToolCapability = "FILE_WRITE_MODIFY"  // Modify existing files
	CapFileDelete       ToolCapability = "FILE_DELETE"        // Delete files
	CapFileList         ToolCapability = "FILE_LIST"          // List directory contents

	// Search capabilities
	CapSearchContent  ToolCapability = "SEARCH_CONTENT"  // Search file contents
	CapSearchFilename ToolCapability = "SEARCH_FILENAME" // Search by filename
	CapSearchPattern  ToolCapability = "SEARCH_PATTERN"  // Regex/glob pattern search
	CapSearchSemantic ToolCapability = "SEARCH_SEMANTIC" // Semantic/embedding search

	// Code execution capabilities
	CapCodeExecutePython ToolCapability = "CODE_EXECUTE_PYTHON" // Execute Python code
	CapCodeExecuteShell  ToolCapability = "CODE_EXECUTE_SHELL"  // Execute shell commands
	CapCodeAnalyze       ToolCapability = "CODE_ANALYZE"        // Static code analysis
	CapCodeFormat        ToolCapability = "CODE_FORMAT"         // Code formatting

	// Memory capabilities
	CapMemoryQueryRecent    ToolCapability = "MEMORY_QUERY_RECENT"    // Query recent context
	CapMemoryQueryLongTerm  ToolCapability = "MEMORY_QUERY_LONGTERM"  // Query long-term memory
	CapMemoryQuerySemantic  ToolCapability = "MEMORY_QUERY_SEMANTIC"  // Semantic memory search
	CapMemoryStoreEphemeral ToolCapability = "MEMORY_STORE_EPHEMERAL" // Store temporary info
	CapMemoryStorePersist   ToolCapability = "MEMORY_STORE_PERSIST"   // Store persistent info

	// Web capabilities
	CapWebFetchPage ToolCapability = "WEB_FETCH_PAGE" // Fetch web page
	CapWebFetchAPI  ToolCapability = "WEB_FETCH_API"  // Fetch from API
	CapWebSearch    ToolCapability = "WEB_SEARCH"     // Web search

	// Computation capabilities
	CapComputeMath     ToolCapability = "COMPUTE_MATH"     // Mathematical computation
	CapComputeData     ToolCapability = "COMPUTE_DATA"     // Data processing
	CapComputeTransform ToolCapability = "COMPUTE_TRANSFORM" // Data transformation

	// Git capabilities
	CapGitRead  ToolCapability = "GIT_READ"  // Read git info (status, log, diff)
	CapGitWrite ToolCapability = "GIT_WRITE" // Write git operations (commit, push)
)

// RequirementLevel indicates how strongly a capability is needed.
type RequirementLevel int

const (
	// RequirementOptional - capability would help but isn't required.
	RequirementOptional RequirementLevel = iota
	// RequirementPreferred - capability is likely needed.
	RequirementPreferred
	// RequirementRequired - capability is essential.
	RequirementRequired
)

// CapabilityRequirement pairs a capability with its requirement level.
type CapabilityRequirement struct {
	Capability ToolCapability
	Level      RequirementLevel
	Reason     string
}

// QueryRequirements represents inferred requirements from a query.
type QueryRequirements struct {
	// Required capabilities must be present.
	Required []CapabilityRequirement

	// Preferred capabilities improve results.
	Preferred []CapabilityRequirement

	// EstimatedComplexity from 1-10.
	EstimatedComplexity int

	// SuggestedToolSequence is an ordered list of tool types.
	SuggestedToolSequence []string
}

// QueryAnalyzer infers capability requirements from queries.
type QueryAnalyzer struct {
	patterns []queryPattern
}

type queryPattern struct {
	pattern      *regexp.Regexp
	capabilities []CapabilityRequirement
	complexity   int
	tools        []string
}

// NewQueryAnalyzer creates a new query analyzer.
func NewQueryAnalyzer() *QueryAnalyzer {
	qa := &QueryAnalyzer{}
	qa.initPatterns()
	return qa
}

func (qa *QueryAnalyzer) initPatterns() {
	qa.patterns = []queryPattern{
		// File reading patterns
		{
			pattern: regexp.MustCompile(`(?i)\b(read|show|display|get|cat)\b.*\b(file|content|code)\b`),
			capabilities: []CapabilityRequirement{
				{CapFileReadSingle, RequirementRequired, "Query requests reading file content"},
			},
			complexity: 2,
			tools:      []string{"file_read"},
		},
		{
			pattern: regexp.MustCompile(`(?i)\b(read|show|get)\b.*\b(files|multiple|all)\b`),
			capabilities: []CapabilityRequirement{
				{CapFileReadMultiple, RequirementRequired, "Query requests reading multiple files"},
			},
			complexity: 3,
			tools:      []string{"file_read", "search"},
		},

		// File writing patterns
		{
			pattern: regexp.MustCompile(`(?i)\b(create|write|generate|make)\b.*\b(file|script|code)\b`),
			capabilities: []CapabilityRequirement{
				{CapFileWriteCreate, RequirementRequired, "Query requests creating a file"},
			},
			complexity: 3,
			tools:      []string{"file_write"},
		},
		{
			pattern: regexp.MustCompile(`(?i)\b(modify|update|edit|change|fix)\b.*\b(file|code)\b`),
			capabilities: []CapabilityRequirement{
				{CapFileReadSingle, RequirementRequired, "Need to read before modifying"},
				{CapFileWriteModify, RequirementRequired, "Query requests modifying a file"},
			},
			complexity: 4,
			tools:      []string{"file_read", "file_write"},
		},

		// Search patterns
		{
			pattern: regexp.MustCompile(`(?i)\b(find|search|look for|locate)\b.*\b(file|files|where)\b`),
			capabilities: []CapabilityRequirement{
				{CapSearchFilename, RequirementPreferred, "Query may need filename search"},
				{CapSearchContent, RequirementPreferred, "Query may need content search"},
			},
			complexity: 3,
			tools:      []string{"search"},
		},
		{
			pattern: regexp.MustCompile(`(?i)\b(grep|regex|pattern|match)\b`),
			capabilities: []CapabilityRequirement{
				{CapSearchPattern, RequirementRequired, "Query requests pattern-based search"},
			},
			complexity: 4,
			tools:      []string{"search"},
		},

		// Code execution patterns
		{
			pattern: regexp.MustCompile(`(?i)\b(run|execute|eval|compute|calculate)\b.*\b(python|code|script)\b`),
			capabilities: []CapabilityRequirement{
				{CapCodeExecutePython, RequirementRequired, "Query requests code execution"},
				{CapComputeMath, RequirementOptional, "May involve computation"},
			},
			complexity: 5,
			tools:      []string{"repl"},
		},
		{
			pattern: regexp.MustCompile(`(?i)\b(shell|bash|command|terminal)\b`),
			capabilities: []CapabilityRequirement{
				{CapCodeExecuteShell, RequirementRequired, "Query requests shell execution"},
			},
			complexity: 5,
			tools:      []string{"shell"},
		},

		// Computation patterns
		{
			pattern: regexp.MustCompile(`(?i)\b(calculate|compute|sum|average|count|math)\b`),
			capabilities: []CapabilityRequirement{
				{CapComputeMath, RequirementRequired, "Query requires mathematical computation"},
				{CapCodeExecutePython, RequirementPreferred, "Python recommended for computation"},
			},
			complexity: 3,
			tools:      []string{"repl"},
		},
		{
			pattern: regexp.MustCompile(`(?i)\b(analyze|process|transform|parse)\b.*\b(data|json|csv)\b`),
			capabilities: []CapabilityRequirement{
				{CapComputeData, RequirementRequired, "Query requires data processing"},
				{CapCodeExecutePython, RequirementPreferred, "Python recommended for data processing"},
			},
			complexity: 5,
			tools:      []string{"repl"},
		},

		// Memory patterns
		{
			pattern: regexp.MustCompile(`(?i)\b(remember|recall|what did|earlier|before)\b`),
			capabilities: []CapabilityRequirement{
				{CapMemoryQueryRecent, RequirementRequired, "Query references recent context"},
			},
			complexity: 2,
			tools:      []string{"search"},
		},
		{
			pattern: regexp.MustCompile(`(?i)\b(save|store|remember for later|persist)\b`),
			capabilities: []CapabilityRequirement{
				{CapMemoryStorePersist, RequirementRequired, "Query requests storing information"},
			},
			complexity: 2,
			tools:      []string{"memory_store"},
		},

		// Git patterns
		{
			pattern: regexp.MustCompile(`(?i)\b(git|commit|diff|status|log|branch)\b`),
			capabilities: []CapabilityRequirement{
				{CapGitRead, RequirementRequired, "Query involves git operations"},
			},
			complexity: 3,
			tools:      []string{"shell"},
		},
		{
			pattern: regexp.MustCompile(`(?i)\b(commit|push|merge)\b`),
			capabilities: []CapabilityRequirement{
				{CapGitWrite, RequirementRequired, "Query involves git write operations"},
			},
			complexity: 4,
			tools:      []string{"shell"},
		},

		// Web patterns
		{
			pattern: regexp.MustCompile(`(?i)\b(fetch|download|get)\b.*\b(url|webpage|website)\b`),
			capabilities: []CapabilityRequirement{
				{CapWebFetchPage, RequirementRequired, "Query requests fetching web content"},
			},
			complexity: 4,
			tools:      []string{"web_fetch"},
		},
		{
			pattern: regexp.MustCompile(`(?i)\b(api|endpoint|request)\b`),
			capabilities: []CapabilityRequirement{
				{CapWebFetchAPI, RequirementRequired, "Query involves API interaction"},
			},
			complexity: 5,
			tools:      []string{"web_fetch", "repl"},
		},
	}
}

// Analyze infers requirements from a query string.
func (qa *QueryAnalyzer) Analyze(query string) *QueryRequirements {
	req := &QueryRequirements{
		EstimatedComplexity: 1,
	}

	seenCapabilities := make(map[ToolCapability]bool)
	seenTools := make(map[string]bool)

	for _, p := range qa.patterns {
		if p.pattern.MatchString(query) {
			for _, cap := range p.capabilities {
				if !seenCapabilities[cap.Capability] {
					seenCapabilities[cap.Capability] = true
					if cap.Level == RequirementRequired {
						req.Required = append(req.Required, cap)
					} else {
						req.Preferred = append(req.Preferred, cap)
					}
				}
			}

			if p.complexity > req.EstimatedComplexity {
				req.EstimatedComplexity = p.complexity
			}

			for _, tool := range p.tools {
				if !seenTools[tool] {
					seenTools[tool] = true
					req.SuggestedToolSequence = append(req.SuggestedToolSequence, tool)
				}
			}
		}
	}

	return req
}

// CapabilityScore evaluates how well a tool matches requirements.
func (qa *QueryAnalyzer) CapabilityScore(profile ToolProfile, req *QueryRequirements) float64 {
	score := 0.0

	// Required capabilities must all be present
	requiredMet := 0
	for _, r := range req.Required {
		if profile.HasCapability(r.Capability) {
			requiredMet++
			score += 1.0
		}
	}

	// If not all required caps are met, heavily penalize
	if len(req.Required) > 0 && requiredMet < len(req.Required) {
		return score * 0.1 // 90% penalty
	}

	// Preferred capabilities add bonus
	for _, p := range req.Preferred {
		if profile.HasCapability(p.Capability) {
			score += 0.5
		}
	}

	// Normalize by maximum possible score
	maxScore := float64(len(req.Required)) + float64(len(req.Preferred))*0.5
	if maxScore > 0 {
		score = score / maxScore
	}

	return score
}

// AgentToolMatrix provides comprehensive tool profiles for typical agent tools.
var AgentToolMatrix = map[string]ToolProfile{
	"repl": {
		Name: "repl",
		Capabilities: []ToolCapability{
			CapCodeExecutePython,
			CapComputeMath,
			CapComputeData,
			CapComputeTransform,
		},
		CostEstimate: 100,  // Tokens for setup + execution overhead
		LatencyMS:    500,  // Python startup + execution
		Description:  "Execute Python code for computation, data processing, and analysis",
	},
	"shell": {
		Name: "shell",
		Capabilities: []ToolCapability{
			CapCodeExecuteShell,
			CapGitRead,
			CapGitWrite,
			CapFileList,
		},
		CostEstimate: 50,   // Lower overhead than REPL
		LatencyMS:    100,  // Shell is fast
		Description:  "Execute shell commands for system operations and git",
	},
	"file_read": {
		Name: "file_read",
		Capabilities: []ToolCapability{
			CapFileReadSingle,
			CapFileReadMultiple,
		},
		CostEstimate: 20,  // Just file I/O
		LatencyMS:    50,  // Fast disk access
		Description:  "Read file contents from the filesystem",
	},
	"file_write": {
		Name: "file_write",
		Capabilities: []ToolCapability{
			CapFileWriteCreate,
			CapFileWriteModify,
		},
		CostEstimate: 30,  // File I/O + potential validation
		LatencyMS:    100, // Write can be slower
		Description:  "Write or modify files on the filesystem",
	},
	"file_delete": {
		Name: "file_delete",
		Capabilities: []ToolCapability{
			CapFileDelete,
		},
		CostEstimate: 10,
		LatencyMS:    50,
		Description:  "Delete files from the filesystem",
	},
	"search": {
		Name: "search",
		Capabilities: []ToolCapability{
			CapSearchContent,
			CapSearchFilename,
			CapSearchPattern,
			CapMemoryQueryRecent,
		},
		CostEstimate: 30,  // Depends on index
		LatencyMS:    100, // Index lookup
		Description:  "Search for files and content using patterns or keywords",
	},
	"semantic_search": {
		Name: "semantic_search",
		Capabilities: []ToolCapability{
			CapSearchSemantic,
			CapMemoryQuerySemantic,
			CapMemoryQueryLongTerm,
		},
		CostEstimate: 200, // Embedding generation + search
		LatencyMS:    300, // Vector search overhead
		Description:  "Semantic search using embeddings for meaning-based retrieval",
	},
	"memory_store": {
		Name: "memory_store",
		Capabilities: []ToolCapability{
			CapMemoryStoreEphemeral,
			CapMemoryStorePersist,
		},
		CostEstimate: 50,  // Storage + potential embedding
		LatencyMS:    150, // Write to persistent store
		Description:  "Store information in memory for later retrieval",
	},
	"web_fetch": {
		Name: "web_fetch",
		Capabilities: []ToolCapability{
			CapWebFetchPage,
			CapWebFetchAPI,
		},
		CostEstimate: 100, // Network overhead
		LatencyMS:    1000, // Network latency
		Description:  "Fetch content from URLs or APIs",
	},
	"web_search": {
		Name: "web_search",
		Capabilities: []ToolCapability{
			CapWebSearch,
		},
		CostEstimate: 150, // Search API cost
		LatencyMS:    800, // Search API latency
		Description:  "Search the web for information",
	},
	"code_analysis": {
		Name: "code_analysis",
		Capabilities: []ToolCapability{
			CapCodeAnalyze,
			CapSearchContent,
		},
		CostEstimate: 80,
		LatencyMS:    200,
		Description:  "Analyze code for structure, dependencies, and issues",
	},
	"code_format": {
		Name: "code_format",
		Capabilities: []ToolCapability{
			CapCodeFormat,
			CapFileReadSingle,
			CapFileWriteModify,
		},
		CostEstimate: 40,
		LatencyMS:    100,
		Description:  "Format code according to style guidelines",
	},
}

// GetAgentToolMatrix returns a copy of the agent tool matrix.
func GetAgentToolMatrix() map[string]ToolProfile {
	matrix := make(map[string]ToolProfile, len(AgentToolMatrix))
	for k, v := range AgentToolMatrix {
		matrix[k] = v
	}
	return matrix
}

// RecommendTools returns tools ranked by capability match and cost efficiency.
func RecommendTools(req *QueryRequirements, profiles map[string]ToolProfile) []RankedTool {
	qa := &QueryAnalyzer{}

	var ranked []RankedTool
	for name, profile := range profiles {
		capScore := qa.CapabilityScore(profile, req)
		if capScore == 0 {
			continue // Skip tools that don't meet requirements
		}

		// Cost efficiency: prefer lower cost tools
		costFactor := 1.0 / (1.0 + profile.CostEstimate/100.0)

		// Latency efficiency: prefer faster tools
		latencyFactor := 1.0 / (1.0 + float64(profile.LatencyMS)/500.0)

		// Combined score: capability match is primary, cost/latency secondary
		totalScore := capScore*0.6 + costFactor*0.2 + latencyFactor*0.2

		ranked = append(ranked, RankedTool{
			Name:            name,
			Profile:         profile,
			CapabilityScore: capScore,
			EfficiencyScore: (costFactor + latencyFactor) / 2,
			TotalScore:      totalScore,
		})
	}

	// Sort by total score descending
	for i := 0; i < len(ranked)-1; i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].TotalScore > ranked[i].TotalScore {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}

	return ranked
}

// RankedTool represents a tool with its ranking scores.
type RankedTool struct {
	Name            string
	Profile         ToolProfile
	CapabilityScore float64 // How well capabilities match (0-1)
	EfficiencyScore float64 // Cost/latency efficiency (0-1)
	TotalScore      float64 // Combined ranking score
}

// CapabilityGroup represents a logical grouping of capabilities.
type CapabilityGroup struct {
	Name         string
	Description  string
	Capabilities []ToolCapability
}

// CapabilityGroups organizes capabilities into logical groups.
var CapabilityGroups = []CapabilityGroup{
	{
		Name:        "FileSystem",
		Description: "File system operations",
		Capabilities: []ToolCapability{
			CapFileRead, CapFileWrite, CapFileReadSingle, CapFileReadMultiple,
			CapFileWriteCreate, CapFileWriteModify, CapFileDelete, CapFileList,
		},
	},
	{
		Name:        "Search",
		Description: "Search and retrieval",
		Capabilities: []ToolCapability{
			CapSearch, CapSearchContent, CapSearchFilename, CapSearchPattern, CapSearchSemantic,
		},
	},
	{
		Name:        "CodeExecution",
		Description: "Code execution and analysis",
		Capabilities: []ToolCapability{
			CapCodeExecution, CapCodeExecutePython, CapCodeExecuteShell,
			CapCodeAnalyze, CapCodeFormat,
		},
	},
	{
		Name:        "Memory",
		Description: "Memory and context management",
		Capabilities: []ToolCapability{
			CapMemoryQuery, CapMemoryStore, CapMemoryQueryRecent, CapMemoryQueryLongTerm,
			CapMemoryQuerySemantic, CapMemoryStoreEphemeral, CapMemoryStorePersist,
		},
	},
	{
		Name:        "Web",
		Description: "Web and network operations",
		Capabilities: []ToolCapability{
			CapWebFetch, CapWebFetchPage, CapWebFetchAPI, CapWebSearch,
		},
	},
	{
		Name:        "Computation",
		Description: "Computational operations",
		Capabilities: []ToolCapability{
			CapComputation, CapComputeMath, CapComputeData, CapComputeTransform,
		},
	},
	{
		Name:        "Git",
		Description: "Version control operations",
		Capabilities: []ToolCapability{
			CapGitRead, CapGitWrite,
		},
	},
}

// GetCapabilityGroup returns the group containing a capability.
func GetCapabilityGroup(cap ToolCapability) *CapabilityGroup {
	capStr := string(cap)
	for i := range CapabilityGroups {
		for _, c := range CapabilityGroups[i].Capabilities {
			if string(c) == capStr || strings.HasPrefix(capStr, strings.TrimSuffix(string(c), "_")) {
				return &CapabilityGroups[i]
			}
		}
	}
	return nil
}
