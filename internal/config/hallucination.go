package config

import "time"

// HallucinationConfig configures hallucination detection.
// [SPEC-08.34-35]
type HallucinationConfig struct {
	// Enabled enables hallucination detection globally.
	// Disabled by default for performance.
	Enabled bool `json:"enabled,omitempty" jsonschema:"description=Enable hallucination detection globally,default=false"`

	// Backend configures the verification backend.
	Backend HallucinationBackendConfig `json:"backend,omitempty" jsonschema:"description=Verification backend configuration"`

	// MemoryGate configures memory storage verification.
	MemoryGate MemoryGateConfig `json:"memory_gate,omitempty" jsonschema:"description=Memory gate verification settings"`

	// OutputVerification configures output verification.
	OutputVerification OutputVerificationConfig `json:"output_verification,omitempty" jsonschema:"description=Output verification settings"`

	// TraceAuditing configures reasoning trace auditing.
	TraceAuditing TraceAuditingConfig `json:"trace_auditing,omitempty" jsonschema:"description=Trace auditing settings"`

	// BatchSize limits the number of claims to verify in parallel.
	BatchSize int `json:"batch_size,omitempty" jsonschema:"description=Maximum claims to verify in parallel,default=10,example=5"`

	// Timeout is the maximum time for a single verification.
	Timeout time.Duration `json:"timeout,omitempty" jsonschema:"description=Maximum time for single verification,default=5s,example=10s"`

	// CacheTTL is how long to cache verification results.
	CacheTTL time.Duration `json:"cache_ttl,omitempty" jsonschema:"description=How long to cache verification results,default=1h,example=30m"`
}

// HallucinationBackendConfig configures the verification backend.
type HallucinationBackendConfig struct {
	// Type is the backend type: "self", "haiku", "external", "mock".
	Type string `json:"type,omitempty" jsonschema:"description=Backend type for verification,enum=self,enum=haiku,enum=external,enum=mock,default=haiku"`

	// Model is the model identifier for external backends.
	Model string `json:"model,omitempty" jsonschema:"description=Model identifier for external backends,example=claude-3-haiku-20240307"`

	// SamplingCount is the number of samples for fallback estimation.
	SamplingCount int `json:"sampling_count,omitempty" jsonschema:"description=Number of samples for fallback estimation,default=5,example=3"`
}

// MemoryGateConfig configures memory storage verification.
// [SPEC-08.15-18]
type MemoryGateConfig struct {
	// Enabled enables memory gate verification.
	Enabled bool `json:"enabled,omitempty" jsonschema:"description=Enable memory gate verification,default=false"`

	// MinConfidence is the minimum confidence threshold for storage.
	MinConfidence float64 `json:"min_confidence,omitempty" jsonschema:"description=Minimum confidence for memory storage,minimum=0,maximum=1,default=0.7,example=0.8"`

	// RejectUnsupported rejects unsupported claims from storage.
	RejectUnsupported bool `json:"reject_unsupported,omitempty" jsonschema:"description=Reject unsupported claims from storage,default=true"`

	// FlagThresholdBits is the budget gap threshold for flagging.
	FlagThresholdBits float64 `json:"flag_threshold_bits,omitempty" jsonschema:"description=Budget gap threshold for flagging claims,default=2.0,example=3.0"`
}

// OutputVerificationConfig configures output verification.
// [SPEC-08.19-22]
type OutputVerificationConfig struct {
	// Enabled enables output verification.
	Enabled bool `json:"enabled,omitempty" jsonschema:"description=Enable output verification,default=false"`

	// FlagThresholdBits is the budget gap threshold for flagging.
	FlagThresholdBits float64 `json:"flag_threshold_bits,omitempty" jsonschema:"description=Budget gap threshold for flagging output claims,default=5.0,example=3.0"`

	// WarnOnFlag shows warnings for flagged claims instead of blocking.
	WarnOnFlag bool `json:"warn_on_flag,omitempty" jsonschema:"description=Show warnings instead of blocking flagged claims,default=true"`
}

// TraceAuditingConfig configures reasoning trace auditing.
// [SPEC-08.23-26]
type TraceAuditingConfig struct {
	// Enabled enables trace auditing.
	Enabled bool `json:"enabled,omitempty" jsonschema:"description=Enable trace auditing,default=false"`

	// CheckPostHoc enables post-hoc hallucination detection.
	CheckPostHoc bool `json:"check_post_hoc,omitempty" jsonschema:"description=Check for post-hoc hallucinations in traces,default=true"`

	// StopOnContradiction stops trace processing on contradiction.
	StopOnContradiction bool `json:"stop_on_contradiction,omitempty" jsonschema:"description=Stop trace processing when contradiction detected,default=false"`
}

// DefaultHallucinationConfig returns sensible defaults for hallucination detection.
// [SPEC-08.35] All features are disabled by default.
func DefaultHallucinationConfig() HallucinationConfig {
	return HallucinationConfig{
		Enabled: false, // Disabled by default for performance
		Backend: HallucinationBackendConfig{
			Type:          "haiku", // Fast and cheap
			SamplingCount: 5,
		},
		MemoryGate: MemoryGateConfig{
			Enabled:           false,
			MinConfidence:     0.7,
			RejectUnsupported: true,
			FlagThresholdBits: 2.0,
		},
		OutputVerification: OutputVerificationConfig{
			Enabled:           false,
			FlagThresholdBits: 5.0,
			WarnOnFlag:        true,
		},
		TraceAuditing: TraceAuditingConfig{
			Enabled:             false,
			CheckPostHoc:        true,
			StopOnContradiction: false,
		},
		BatchSize: 10,
		Timeout:   5 * time.Second,
		CacheTTL:  1 * time.Hour,
	}
}
