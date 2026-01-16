package learning

import (
	"context"
	"testing"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// TestProperty_PatternUpdateRoundtrip verifies patterns survive update cycle.
func TestProperty_PatternUpdateRoundtrip(t *testing.T) {
	graph, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	defer graph.Close()

	store := NewStore(graph)
	ctx := context.Background()

	rapid.Check(t, func(rt *rapid.T) {
		// Create pattern with random values
		name := rapid.StringN(1, 100, 200).Draw(rt, "name")
		trigger := rapid.String().Draw(rt, "trigger")
		template := rapid.String().Draw(rt, "template")
		successRate := rapid.Float64Range(0, 1).Draw(rt, "success_rate")
		usageCount := rapid.IntRange(0, 1000).Draw(rt, "usage_count")

		pattern := &LearnedPattern{
			Name:        name,
			PatternType: PatternTypeCode,
			Trigger:     trigger,
			Template:    template,
			Domains:     []string{"test"},
			SuccessRate: successRate,
			UsageCount:  usageCount,
			LastUsed:    time.Now(),
		}

		err := store.StorePattern(ctx, pattern)
		require.NoError(rt, err)
		require.NotEmpty(rt, pattern.ID)

		// Update with new random values
		newSuccessRate := rapid.Float64Range(0, 1).Draw(rt, "new_success_rate")
		newUsageCount := rapid.IntRange(0, 1000).Draw(rt, "new_usage_count")
		pattern.SuccessRate = newSuccessRate
		pattern.UsageCount = newUsageCount

		err = store.UpdatePattern(ctx, pattern)
		require.NoError(rt, err)

		// Verify roundtrip
		got, err := store.GetPattern(ctx, pattern.ID)
		require.NoError(rt, err)
		require.NotNil(rt, got)

		require.Equal(rt, pattern.ID, got.ID)
		require.Equal(rt, name, got.Name)
		require.InDelta(rt, newSuccessRate, got.SuccessRate, 0.0001)
		require.Equal(rt, newUsageCount, got.UsageCount)
	})
}

// TestProperty_ConstraintUpdateRoundtrip verifies constraints survive update cycle.
func TestProperty_ConstraintUpdateRoundtrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Create fresh store for each test to avoid constraint accumulation
		graph, err := hypergraph.NewStore(hypergraph.Options{})
		require.NoError(rt, err)
		defer graph.Close()

		store := NewStore(graph)
		ctx := context.Background()

		// Create constraint with random values
		description := rapid.StringN(1, 100, 200).Draw(rt, "description")
		domain := rapid.StringN(1, 20, 50).Draw(rt, "domain")
		severity := rapid.Float64Range(0, 1).Draw(rt, "severity")
		violationCount := rapid.IntRange(0, 100).Draw(rt, "violation_count")

		constraint := &LearnedConstraint{
			ConstraintType: ConstraintAvoid,
			Description:    description,
			Domain:         domain,
			Severity:       severity,
			Source:         SourceInferred,
			ViolationCount: violationCount,
		}

		err = store.StoreConstraint(ctx, constraint)
		require.NoError(rt, err)
		require.NotEmpty(rt, constraint.ID)

		// Update with new random values
		newSeverity := rapid.Float64Range(0, 1).Draw(rt, "new_severity")
		newViolationCount := rapid.IntRange(0, 100).Draw(rt, "new_violation_count")
		constraint.Severity = newSeverity
		constraint.ViolationCount = newViolationCount

		err = store.UpdateConstraint(ctx, constraint)
		require.NoError(rt, err)

		// Verify roundtrip
		constraints, err := store.ListConstraints(ctx, domain, 0)
		require.NoError(rt, err)
		require.Len(rt, constraints, 1)

		got := constraints[0]
		require.Equal(rt, constraint.ID, got.ID)
		require.Equal(rt, description, got.Description)
		require.InDelta(rt, newSeverity, got.Severity, 0.0001)
		require.Equal(rt, newViolationCount, got.ViolationCount)
	})
}

// TestProperty_PatternDeleteRemoves verifies delete removes pattern.
func TestProperty_PatternDeleteRemoves(t *testing.T) {
	graph, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	defer graph.Close()

	store := NewStore(graph)
	ctx := context.Background()

	rapid.Check(t, func(rt *rapid.T) {
		// Create pattern
		pattern := &LearnedPattern{
			Name:        rapid.StringN(1, 50, 100).Draw(rt, "name"),
			PatternType: PatternTypeCode,
			SuccessRate: rapid.Float64Range(0, 1).Draw(rt, "success_rate"),
		}

		err := store.StorePattern(ctx, pattern)
		require.NoError(rt, err)

		// Verify exists
		got, err := store.GetPattern(ctx, pattern.ID)
		require.NoError(rt, err)
		require.NotNil(rt, got)

		// Delete
		err = store.DeletePattern(ctx, pattern.ID)
		require.NoError(rt, err)

		// Verify removed (may return nil or error depending on store)
		got, err = store.GetPattern(ctx, pattern.ID)
		if err == nil {
			require.Nil(rt, got)
		}
	})
}

// TestProperty_ConstraintDeleteRemoves verifies delete removes constraint.
func TestProperty_ConstraintDeleteRemoves(t *testing.T) {
	graph, err := hypergraph.NewStore(hypergraph.Options{})
	require.NoError(t, err)
	defer graph.Close()

	store := NewStore(graph)
	ctx := context.Background()

	rapid.Check(t, func(rt *rapid.T) {
		// Use unique domain per test to avoid conflicts
		domain := rapid.StringN(10, 20, 30).Draw(rt, "domain")

		// Create constraint
		constraint := &LearnedConstraint{
			ConstraintType: ConstraintAvoid,
			Description:    rapid.StringN(1, 50, 100).Draw(rt, "description"),
			Domain:         domain,
			Severity:       rapid.Float64Range(0.1, 1).Draw(rt, "severity"),
			Source:         SourceInferred,
		}

		err := store.StoreConstraint(ctx, constraint)
		require.NoError(rt, err)

		// Verify exists
		constraints, err := store.ListConstraints(ctx, domain, 0)
		require.NoError(rt, err)
		require.Len(rt, constraints, 1)

		// Delete
		err = store.DeleteConstraint(ctx, constraint.ID)
		require.NoError(rt, err)

		// Verify removed
		constraints, err = store.ListConstraints(ctx, domain, 0)
		require.NoError(rt, err)
		require.Len(rt, constraints, 0)
	})
}

// TestProperty_DecayMonotonicallyDecreases verifies decay always reduces confidence.
func TestProperty_DecayMonotonicallyDecreases(t *testing.T) {
	consolidator := NewConsolidator(nil, ConsolidatorConfig{
		DecayHalfLife: 7 * 24 * time.Hour,
		MinConfidence: 0.01,
	})

	rapid.Check(t, func(rt *rapid.T) {
		confidence := rapid.Float64Range(0.1, 1.0).Draw(rt, "confidence")
		accessCount := rapid.IntRange(1, 100).Draw(rt, "access_count")
		// At least 1 hour ago to ensure some decay
		hoursAgo := rapid.IntRange(1, 720).Draw(rt, "hours_ago") // Up to 30 days
		lastAccess := time.Now().Add(-time.Duration(hoursAgo) * time.Hour)

		decayed := consolidator.calculateDecay(confidence, lastAccess, accessCount)

		// Decay should be less than or equal to original
		require.LessOrEqual(rt, decayed, confidence)
		// But greater than minimum
		require.Greater(rt, decayed, 0.0)
	})
}
