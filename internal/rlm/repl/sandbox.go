package repl

import "time"

// SandboxConfig defines the constraints for Python execution.
type SandboxConfig struct {
	// ReadPaths are directories the REPL can read from.
	// Defaults to the current working directory.
	ReadPaths []string

	// WritePath is the single directory the REPL can write to.
	// Defaults to a temp directory.
	WritePath string

	// NetworkEnabled allows network access if true.
	// Defaults to false.
	NetworkEnabled bool

	// Timeout is the maximum execution time per cell.
	// Defaults to 30 seconds.
	Timeout time.Duration

	// MemoryLimitMB is the maximum memory usage in megabytes.
	// Defaults to 1024 (1GB).
	MemoryLimitMB int
}

// DefaultSandboxConfig returns the default sandbox configuration.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		ReadPaths:      []string{"."},
		WritePath:      "",  // Will be set to temp dir
		NetworkEnabled: false,
		Timeout:        30 * time.Second,
		MemoryLimitMB:  1024,
	}
}

// Validate checks the sandbox configuration for errors.
func (c *SandboxConfig) Validate() error {
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MemoryLimitMB <= 0 {
		c.MemoryLimitMB = 1024
	}
	return nil
}

// ToEnv converts the sandbox config to environment variables for the Python process.
func (c *SandboxConfig) ToEnv() []string {
	env := []string{
		"RECURSE_SANDBOX=1",
	}
	if c.NetworkEnabled {
		env = append(env, "RECURSE_NETWORK=1")
	}
	return env
}
