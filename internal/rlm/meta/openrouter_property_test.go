package meta

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// Property: Model selection always returns a valid model or nil
func TestProperty_SelectModelReturnsValidModel(t *testing.T) {
	models := DefaultModels()
	selector := &AdaptiveSelector{models: models}
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		task := rapid.String().Draw(t, "task")
		budget := rapid.IntRange(0, 100000).Draw(t, "budget")
		depth := rapid.IntRange(0, 10).Draw(t, "depth")

		spec := selector.SelectModel(ctx, task, budget, depth)

		// Either nil or a valid model from the catalog
		if spec != nil {
			found := false
			for _, m := range models {
				if m.ID == spec.ID {
					found = true
					break
				}
			}
			assert.True(t, found, "returned model %s not in catalog", spec.ID)
		}
	})
}

// Property: Low budget always selects Fast tier
func TestProperty_LowBudgetSelectsFastTier(t *testing.T) {
	models := DefaultModels()
	selector := &AdaptiveSelector{models: models}
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		// Task without reasoning/complex keywords
		task := rapid.SampledFrom([]string{
			"simple task",
			"hello world",
			"basic operation",
			"quick check",
		}).Draw(t, "task")
		budget := rapid.IntRange(0, 999).Draw(t, "budget")
		depth := rapid.IntRange(0, 2).Draw(t, "depth")

		spec := selector.SelectModel(ctx, task, budget, depth)

		if spec != nil {
			assert.Equal(t, TierFast, spec.Tier,
				"low budget (%d) should select fast tier, got tier %d for %s",
				budget, spec.Tier, spec.Name)
		}
	})
}

// Property: High depth always selects Fast tier (regardless of budget)
func TestProperty_HighDepthSelectsFastTier(t *testing.T) {
	models := DefaultModels()
	selector := &AdaptiveSelector{models: models}
	ctx := context.Background()

	rapid.Check(t, func(t *rapid.T) {
		// Task without reasoning/complex keywords
		task := rapid.SampledFrom([]string{
			"simple task",
			"basic check",
			"ordinary work",
		}).Draw(t, "task")
		budget := rapid.IntRange(1000, 100000).Draw(t, "budget")
		depth := rapid.IntRange(3, 10).Draw(t, "depth")

		spec := selector.SelectModel(ctx, task, budget, depth)

		if spec != nil {
			assert.Equal(t, TierFast, spec.Tier,
				"high depth (%d) should select fast tier, got tier %d for %s",
				depth, spec.Tier, spec.Name)
		}
	})
}

// Property: Reasoning keywords always select Reasoning tier
func TestProperty_ReasoningKeywordsSelectReasoningTier(t *testing.T) {
	models := DefaultModels()
	selector := &AdaptiveSelector{models: models}
	ctx := context.Background()

	reasoningKeywords := []string{"prove", "theorem", "logic", "math", "calculate", "reason"}

	rapid.Check(t, func(t *rapid.T) {
		keyword := rapid.SampledFrom(reasoningKeywords).Draw(t, "keyword")
		prefix := rapid.SampledFrom([]string{"please ", "can you ", "I need to ", ""}).Draw(t, "prefix")
		suffix := rapid.SampledFrom([]string{" this", " the problem", " it", ""}).Draw(t, "suffix")
		task := prefix + keyword + suffix

		budget := rapid.IntRange(1000, 100000).Draw(t, "budget")
		depth := rapid.IntRange(0, 2).Draw(t, "depth")

		spec := selector.SelectModel(ctx, task, budget, depth)

		if spec != nil {
			assert.Equal(t, TierReasoning, spec.Tier,
				"task with '%s' should select reasoning tier, got tier %d for %s",
				keyword, spec.Tier, spec.Name)
		}
	})
}

// Property: Tier determination is deterministic
func TestProperty_TierDeterminationIsDeterministic(t *testing.T) {
	models := DefaultModels()
	selector := &AdaptiveSelector{models: models}

	rapid.Check(t, func(t *rapid.T) {
		task := rapid.String().Draw(t, "task")
		budget := rapid.IntRange(0, 100000).Draw(t, "budget")
		depth := rapid.IntRange(0, 10).Draw(t, "depth")

		tier1 := selector.determineTier(task, budget, depth)
		tier2 := selector.determineTier(task, budget, depth)

		assert.Equal(t, tier1, tier2, "tier determination should be deterministic")
	})
}

// Property: All tiers have at least one model in default catalog
func TestProperty_AllTiersHaveModels(t *testing.T) {
	models := DefaultModels()

	tiers := []ModelTier{TierFast, TierBalanced, TierPowerful, TierReasoning}
	for _, tier := range tiers {
		count := 0
		for _, m := range models {
			if m.Tier == tier {
				count++
			}
		}
		assert.Greater(t, count, 0, "tier %d should have at least one model", tier)
	}
}

// Property: Model costs are positive
func TestProperty_ModelCostsArePositive(t *testing.T) {
	models := DefaultModels()

	for _, m := range models {
		assert.GreaterOrEqual(t, m.InputCost, 0.0, "model %s input cost should be non-negative", m.ID)
		assert.GreaterOrEqual(t, m.OutputCost, 0.0, "model %s output cost should be non-negative", m.ID)
	}
}

// Property: Model context sizes are positive
func TestProperty_ModelContextSizesArePositive(t *testing.T) {
	models := DefaultModels()

	for _, m := range models {
		assert.Greater(t, m.ContextSize, 0, "model %s context size should be positive", m.ID)
	}
}

// Property: Model IDs are unique
func TestProperty_ModelIDsAreUnique(t *testing.T) {
	models := DefaultModels()
	seen := make(map[string]bool)

	for _, m := range models {
		assert.False(t, seen[m.ID], "duplicate model ID: %s", m.ID)
		seen[m.ID] = true
	}
}

// Property: Model IDs follow provider/model format
func TestProperty_ModelIDsHaveValidFormat(t *testing.T) {
	models := DefaultModels()

	for _, m := range models {
		assert.Contains(t, m.ID, "/", "model ID %s should contain '/'", m.ID)
		parts := splitModelID(m.ID)
		assert.Len(t, parts, 2, "model ID %s should have provider/model format", m.ID)
		assert.NotEmpty(t, parts[0], "model ID %s should have non-empty provider", m.ID)
		assert.NotEmpty(t, parts[1], "model ID %s should have non-empty model name", m.ID)
	}
}

func splitModelID(id string) []string {
	for i, c := range id {
		if c == '/' {
			return []string{id[:i], id[i+1:]}
		}
	}
	return []string{id}
}

// Property: extractContext returns valid defaults for empty/malformed prompts
func TestProperty_ExtractContextHandlesAnyInput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prompt := rapid.String().Draw(t, "prompt")

		budget, depth := extractContext(prompt)

		// Should always return valid non-negative values
		assert.GreaterOrEqual(t, budget, 0, "budget should be non-negative")
		assert.GreaterOrEqual(t, depth, 0, "depth should be non-negative")
	})
}

// Property: rankCandidates never panics and returns consistent results
func TestProperty_RankCandidatesNeverPanics(t *testing.T) {
	models := DefaultModels()
	selector := &AdaptiveSelector{models: models}

	rapid.Check(t, func(t *rapid.T) {
		task := rapid.String().Draw(t, "task")

		// Create random subset of candidates
		numCandidates := rapid.IntRange(0, len(models)).Draw(t, "numCandidates")
		var candidates []*ModelSpec
		for i := 0; i < numCandidates && i < len(models); i++ {
			candidates = append(candidates, &models[i])
		}

		// Should not panic
		result := selector.rankCandidates(candidates, task)

		// Result should be nil only if candidates is empty
		if len(candidates) > 0 {
			assert.NotNil(t, result, "rankCandidates should return a model when candidates exist")
		}
	})
}

// Property: Cheaper models preferred when strengths match equally
func TestProperty_CheaperModelsPreferredOnTie(t *testing.T) {
	// Create two models with same tier but different costs
	models := []ModelSpec{
		{ID: "test/expensive", Name: "Expensive", Tier: TierFast, InputCost: 10.0, OutputCost: 50.0, Strengths: []string{"test"}},
		{ID: "test/cheap", Name: "Cheap", Tier: TierFast, InputCost: 1.0, OutputCost: 5.0, Strengths: []string{"test"}},
	}
	selector := &AdaptiveSelector{models: models}

	// Task that matches both equally
	candidates := []*ModelSpec{&models[0], &models[1]}
	result := selector.rankCandidates(candidates, "test task")

	assert.Equal(t, "test/cheap", result.ID, "should prefer cheaper model on tie")
}
