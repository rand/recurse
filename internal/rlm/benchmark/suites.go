package benchmark

// =============================================================================
// Predefined Benchmark Suites
// =============================================================================

// OOLONGSynthSuite returns a suite of OOLONG-style synthetic benchmarks.
// These test context understanding at various complexity levels.
func OOLONGSynthSuite(seed int64) Suite {
	return Suite{
		Name:        "OOLONG-Synth",
		Description: "Synthetic tasks testing long-context reasoning at constant, linear, and quadratic complexity",
		Generator:   NewMultiGenerator(seed),
	}
}

// ContextRotSuite returns a suite specifically designed to measure context rot.
// Tasks are generated at increasing context lengths to measure degradation.
func ContextRotSuite(seed int64) Suite {
	gen := NewNeedleGenerator(seed)

	var tasks []Task
	// Generate tasks at various context lengths
	for _, tokens := range []int{1000, 2000, 4000, 8000, 16000, 32000, 64000, 128000} {
		generated, _ := gen.Generate(tokens, 5)
		tasks = append(tasks, generated...)
	}

	return Suite{
		Name:        "Context-Rot",
		Description: "Measures performance degradation as context length increases",
		Tasks:       tasks,
	}
}

// AggregationSuite returns a suite testing multi-hop aggregation.
func AggregationSuite(seed int64) Suite {
	gen := NewAggregationGenerator(seed)

	var tasks []Task
	for _, tokens := range []int{4000, 16000, 64000, 128000} {
		generated, _ := gen.Generate(tokens, 10)
		tasks = append(tasks, generated...)
	}

	return Suite{
		Name:        "Aggregation",
		Description: "Tests ability to aggregate information from multiple locations",
		Tasks:       tasks,
	}
}

// PairingSuite returns a suite testing pairwise relationship reasoning.
func PairingSuite(seed int64) Suite {
	gen := NewPairingGenerator(seed)

	var tasks []Task
	for _, tokens := range []int{8000, 32000, 64000, 128000} {
		generated, _ := gen.Generate(tokens, 10)
		tasks = append(tasks, generated...)
	}

	return Suite{
		Name:        "Pairing",
		Description: "Tests quadratic complexity relationship reasoning",
		Tasks:       tasks,
	}
}

// QuickSuite returns a fast-running suite for development testing.
func QuickSuite(seed int64) Suite {
	counting := NewCountingGenerator(seed)
	needle := NewNeedleGenerator(seed + 1)

	var tasks []Task

	// Small context, few tasks
	countingTasks, _ := counting.Generate(2000, 3)
	needleTasks, _ := needle.Generate(2000, 3)

	tasks = append(tasks, countingTasks...)
	tasks = append(tasks, needleTasks...)

	return Suite{
		Name:        "Quick",
		Description: "Fast suite for development testing",
		Tasks:       tasks,
	}
}

// FullSuite returns a comprehensive benchmark suite.
func FullSuite(seed int64) Suite {
	return Suite{
		Name:        "Full",
		Description: "Comprehensive evaluation across all task types and context lengths",
		Generator:   NewMultiGenerator(seed),
	}
}

// MultiGenerator generates tasks across multiple types and context lengths.
type MultiGenerator struct {
	counting    *CountingGenerator
	needle      *NeedleGenerator
	pairing     *PairingGenerator
	aggregation *AggregationGenerator
}

// NewMultiGenerator creates a generator that produces diverse tasks.
func NewMultiGenerator(seed int64) *MultiGenerator {
	return &MultiGenerator{
		counting:    NewCountingGenerator(seed),
		needle:      NewNeedleGenerator(seed + 1),
		pairing:     NewPairingGenerator(seed + 2),
		aggregation: NewAggregationGenerator(seed + 3),
	}
}

// Generate creates a diverse set of tasks at the specified context length.
func (g *MultiGenerator) Generate(contextTokens int, count int) ([]Task, error) {
	var tasks []Task

	// Distribute count across generators
	perGenerator := count / 4
	if perGenerator < 1 {
		perGenerator = 1
	}

	counting, err := g.counting.Generate(contextTokens, perGenerator)
	if err != nil {
		return nil, err
	}
	tasks = append(tasks, counting...)

	needle, err := g.needle.Generate(contextTokens, perGenerator)
	if err != nil {
		return nil, err
	}
	tasks = append(tasks, needle...)

	// Only generate pairing/aggregation for larger contexts
	if contextTokens >= 8000 {
		pairing, err := g.pairing.Generate(contextTokens, perGenerator)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, pairing...)

		agg, err := g.aggregation.Generate(contextTokens, perGenerator)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, agg...)
	}

	return tasks, nil
}

// =============================================================================
// Benchmark Presets
// =============================================================================

// BenchmarkPreset defines a standard benchmark configuration.
type BenchmarkPreset struct {
	Name        string
	Description string
	Suite       Suite
	Config      RunConfig
}

// StandardPresets returns commonly used benchmark configurations.
func StandardPresets(seed int64) []BenchmarkPreset {
	return []BenchmarkPreset{
		{
			Name:        "quick-rlm",
			Description: "Fast RLM evaluation for development",
			Suite:       QuickSuite(seed),
			Config: RunConfig{
				UseRLM:           true,
				MaxIterations:    5,
				MaxTokensPerCall: 2048,
				Timeout:          2 * 60 * 1000000000, // 2 minutes
				ModelTier:        "fast",
			},
		},
		{
			Name:        "quick-direct",
			Description: "Fast direct prompting baseline",
			Suite:       QuickSuite(seed),
			Config: RunConfig{
				UseRLM:           false,
				MaxTokensPerCall: 4096,
				Timeout:          2 * 60 * 1000000000,
				ModelTier:        "balanced",
			},
		},
		{
			Name:        "context-rot-rlm",
			Description: "Context rot evaluation with RLM",
			Suite:       ContextRotSuite(seed),
			Config: RunConfig{
				UseRLM:           true,
				MaxIterations:    10,
				MaxTokensPerCall: 4096,
				Timeout:          5 * 60 * 1000000000,
				ModelTier:        "balanced",
			},
		},
		{
			Name:        "context-rot-direct",
			Description: "Context rot evaluation with direct prompting",
			Suite:       ContextRotSuite(seed),
			Config: RunConfig{
				UseRLM:           false,
				MaxTokensPerCall: 8192,
				Timeout:          5 * 60 * 1000000000,
				ModelTier:        "powerful",
			},
		},
		{
			Name:        "full-comparison",
			Description: "Full benchmark suite for RLM vs direct comparison",
			Suite:       FullSuite(seed),
			Config:      DefaultRunConfig(),
		},
	}
}

// GetPreset returns a preset by name.
func GetPreset(name string, seed int64) *BenchmarkPreset {
	presets := StandardPresets(seed)
	for _, p := range presets {
		if p.Name == name {
			return &p
		}
	}
	return nil
}
