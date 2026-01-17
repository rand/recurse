package config

import "os"

// Feature flags for rlm-core migration
// Set RLM_USE_CORE=true to use rlm-core Rust bindings instead of Go implementations

// UseRlmCore returns true if rlm-core should be used instead of native Go implementations.
// This is controlled by the RLM_USE_CORE environment variable.
func UseRlmCore() bool {
	return os.Getenv("RLM_USE_CORE") == "true"
}

// RlmCoreLibPath returns the path to the rlm-core shared library.
// Defaults to the standard build location in loop/rlm-core.
func RlmCoreLibPath() string {
	if path := os.Getenv("RLM_CORE_LIB_PATH"); path != "" {
		return path
	}
	// Default to relative path from recurse-rlmcore
	return "../loop/rlm-core/target/release"
}
