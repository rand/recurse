package repl

import (
	"fmt"
	"time"
)

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

	// Resources configures memory and CPU limits.
	Resources ResourceConfig
}

// DefaultSandboxConfig returns the default sandbox configuration.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		ReadPaths:      []string{"."},
		WritePath:      "",  // Will be set to temp dir
		NetworkEnabled: false,
		Timeout:        30 * time.Second,
		Resources:      DefaultResourceConfig(),
	}
}

// Validate checks the sandbox configuration for errors.
func (c *SandboxConfig) Validate() error {
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.Resources.MemoryLimitMB <= 0 {
		c.Resources.MemoryLimitMB = 1024
	}
	if c.Resources.CPUTimeLimitSec <= 0 {
		c.Resources.CPUTimeLimitSec = 60
	}
	if c.Resources.WarnMemoryPercent <= 0 {
		c.Resources.WarnMemoryPercent = 80
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
	// Add resource limits
	env = append(env, fmt.Sprintf("RECURSE_MEMORY_LIMIT_MB=%d", c.Resources.MemoryLimitMB))
	env = append(env, fmt.Sprintf("RECURSE_CPU_LIMIT_SEC=%d", c.Resources.CPUTimeLimitSec))
	return env
}

// MemoryLimitMB returns the memory limit for backward compatibility.
func (c *SandboxConfig) MemoryLimitMB() int {
	return c.Resources.MemoryLimitMB
}
