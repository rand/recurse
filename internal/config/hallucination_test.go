package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultHallucinationConfig(t *testing.T) {
	cfg := DefaultHallucinationConfig()

	// [SPEC-08.35] Disabled by default
	assert.False(t, cfg.Enabled)
	assert.False(t, cfg.MemoryGate.Enabled)
	assert.False(t, cfg.OutputVerification.Enabled)
	assert.False(t, cfg.TraceAuditing.Enabled)

	// Backend defaults
	assert.Equal(t, "haiku", cfg.Backend.Type)
	assert.Equal(t, 5, cfg.Backend.SamplingCount)

	// Memory gate defaults
	assert.Equal(t, 0.7, cfg.MemoryGate.MinConfidence)
	assert.True(t, cfg.MemoryGate.RejectUnsupported)
	assert.Equal(t, 2.0, cfg.MemoryGate.FlagThresholdBits)

	// Output verification defaults
	assert.Equal(t, 5.0, cfg.OutputVerification.FlagThresholdBits)
	assert.True(t, cfg.OutputVerification.WarnOnFlag)

	// Trace auditing defaults
	assert.True(t, cfg.TraceAuditing.CheckPostHoc)
	assert.False(t, cfg.TraceAuditing.StopOnContradiction)

	// General defaults
	assert.Equal(t, 10, cfg.BatchSize)
	assert.Equal(t, 5*time.Second, cfg.Timeout)
	assert.Equal(t, 1*time.Hour, cfg.CacheTTL)
}

func TestHallucinationConfigValidation(t *testing.T) {
	t.Run("accepts valid backend types", func(t *testing.T) {
		validTypes := []string{"self", "haiku", "external", "mock"}
		for _, backendType := range validTypes {
			cfg := DefaultHallucinationConfig()
			cfg.Backend.Type = backendType
			assert.Equal(t, backendType, cfg.Backend.Type)
		}
	})

	t.Run("min confidence bounds", func(t *testing.T) {
		cfg := DefaultHallucinationConfig()

		// Valid values
		cfg.MemoryGate.MinConfidence = 0.0
		assert.Equal(t, 0.0, cfg.MemoryGate.MinConfidence)

		cfg.MemoryGate.MinConfidence = 1.0
		assert.Equal(t, 1.0, cfg.MemoryGate.MinConfidence)

		cfg.MemoryGate.MinConfidence = 0.5
		assert.Equal(t, 0.5, cfg.MemoryGate.MinConfidence)
	})
}
