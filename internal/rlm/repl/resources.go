package repl

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// ResourceConfig defines resource limits for REPL execution.
type ResourceConfig struct {
	// MemoryLimitMB is the maximum memory usage in megabytes.
	// This is enforced via Python's resource.setrlimit().
	MemoryLimitMB int

	// CPUTimeLimitSec is the maximum CPU time per execution in seconds.
	// This is enforced via Python's resource.setrlimit(RLIMIT_CPU).
	CPUTimeLimitSec int

	// WarnMemoryPercent triggers a warning when memory usage exceeds this
	// percentage of the limit. Defaults to 80.
	WarnMemoryPercent int
}

// DefaultResourceConfig returns sensible resource defaults.
func DefaultResourceConfig() ResourceConfig {
	return ResourceConfig{
		MemoryLimitMB:     1024, // 1GB
		CPUTimeLimitSec:   60,   // 60 seconds
		WarnMemoryPercent: 80,
	}
}

// ResourceStats contains resource usage statistics from an execution.
type ResourceStats struct {
	// UserCPUTimeMS is the user CPU time consumed in milliseconds.
	UserCPUTimeMS int64

	// SystemCPUTimeMS is the system CPU time consumed in milliseconds.
	SystemCPUTimeMS int64

	// TotalCPUTimeMS is the total CPU time (user + system) in milliseconds.
	TotalCPUTimeMS int64

	// MemoryUsedMB is the current memory usage in megabytes.
	MemoryUsedMB float64

	// PeakMemoryMB is the peak memory usage (max RSS) in megabytes.
	PeakMemoryMB float64

	// WallTimeMS is the wall-clock time of the execution in milliseconds.
	WallTimeMS int64
}

// ResourceMonitor tracks resource usage for the REPL process.
type ResourceMonitor struct {
	config ResourceConfig
	pid    int

	// Baseline stats captured at start
	baselineUserCPU   int64 // microseconds
	baselineSysCPU    int64 // microseconds
	baselinePeakMemMB float64

	// Cumulative stats for the session
	totalUserCPUMS   int64
	totalSysCPUMS    int64
	peakMemoryMB     float64
	executionCount   int
}

// NewResourceMonitor creates a new resource monitor for the given process.
func NewResourceMonitor(pid int, config ResourceConfig) *ResourceMonitor {
	return &ResourceMonitor{
		config: config,
		pid:    pid,
	}
}

// CaptureBaseline captures the initial resource state.
func (m *ResourceMonitor) CaptureBaseline() error {
	rusage, err := getProcessRusage(m.pid)
	if err != nil {
		return fmt.Errorf("get baseline rusage: %w", err)
	}

	m.baselineUserCPU = timevalToMicros(rusage.Utime)
	m.baselineSysCPU = timevalToMicros(rusage.Stime)
	m.baselinePeakMemMB = maxRSSToMB(rusage.Maxrss)

	return nil
}

// CaptureExecution captures resource usage delta for an execution.
func (m *ResourceMonitor) CaptureExecution(wallTimeMS int64) (*ResourceStats, error) {
	rusage, err := getProcessRusage(m.pid)
	if err != nil {
		return nil, fmt.Errorf("get rusage: %w", err)
	}

	currentUserCPU := timevalToMicros(rusage.Utime)
	currentSysCPU := timevalToMicros(rusage.Stime)
	currentPeakMemMB := maxRSSToMB(rusage.Maxrss)

	// Calculate delta from baseline
	userCPUDeltaMS := (currentUserCPU - m.baselineUserCPU) / 1000
	sysCPUDeltaMS := (currentSysCPU - m.baselineSysCPU) / 1000

	// Update cumulative stats
	m.totalUserCPUMS += userCPUDeltaMS
	m.totalSysCPUMS += sysCPUDeltaMS
	if currentPeakMemMB > m.peakMemoryMB {
		m.peakMemoryMB = currentPeakMemMB
	}
	m.executionCount++

	// Update baseline for next execution
	m.baselineUserCPU = currentUserCPU
	m.baselineSysCPU = currentSysCPU

	stats := &ResourceStats{
		UserCPUTimeMS:   userCPUDeltaMS,
		SystemCPUTimeMS: sysCPUDeltaMS,
		TotalCPUTimeMS:  userCPUDeltaMS + sysCPUDeltaMS,
		PeakMemoryMB:    currentPeakMemMB,
		WallTimeMS:      wallTimeMS,
	}

	return stats, nil
}

// CumulativeStats returns the cumulative resource usage for the session.
func (m *ResourceMonitor) CumulativeStats() ResourceStats {
	return ResourceStats{
		UserCPUTimeMS:   m.totalUserCPUMS,
		SystemCPUTimeMS: m.totalSysCPUMS,
		TotalCPUTimeMS:  m.totalUserCPUMS + m.totalSysCPUMS,
		PeakMemoryMB:    m.peakMemoryMB,
	}
}

// ExecutionCount returns the number of executions tracked.
func (m *ResourceMonitor) ExecutionCount() int {
	return m.executionCount
}

// CheckLimits checks if any resource limits are approaching or exceeded.
func (m *ResourceMonitor) CheckLimits(stats *ResourceStats) *ResourceViolation {
	// Check memory limit
	if m.config.MemoryLimitMB > 0 {
		memPercent := (stats.PeakMemoryMB / float64(m.config.MemoryLimitMB)) * 100
		if memPercent >= 100 {
			return &ResourceViolation{
				Resource: "memory",
				Limit:    float64(m.config.MemoryLimitMB),
				Current:  stats.PeakMemoryMB,
				Unit:     "MB",
				Hard:     true,
			}
		}
		if memPercent >= float64(m.config.WarnMemoryPercent) {
			return &ResourceViolation{
				Resource: "memory",
				Limit:    float64(m.config.MemoryLimitMB),
				Current:  stats.PeakMemoryMB,
				Unit:     "MB",
				Hard:     false, // Warning only
			}
		}
	}

	// Check CPU time limit
	if m.config.CPUTimeLimitSec > 0 {
		cpuLimitMS := int64(m.config.CPUTimeLimitSec * 1000)
		if stats.TotalCPUTimeMS >= cpuLimitMS {
			return &ResourceViolation{
				Resource: "cpu_time",
				Limit:    float64(m.config.CPUTimeLimitSec),
				Current:  float64(stats.TotalCPUTimeMS) / 1000,
				Unit:     "seconds",
				Hard:     true,
			}
		}
	}

	return nil
}

// ResourceViolation describes a resource limit that was exceeded.
type ResourceViolation struct {
	Resource string  // "memory" or "cpu_time"
	Limit    float64 // The configured limit
	Current  float64 // The current/peak usage
	Unit     string  // "MB" or "seconds"
	Hard     bool    // If true, execution should be terminated
}

func (v *ResourceViolation) Error() string {
	if v.Hard {
		return fmt.Sprintf("resource limit exceeded: %s %.2f%s (limit: %.2f%s)",
			v.Resource, v.Current, v.Unit, v.Limit, v.Unit)
	}
	return fmt.Sprintf("resource warning: %s %.2f%s approaching limit %.2f%s",
		v.Resource, v.Current, v.Unit, v.Limit, v.Unit)
}

// ToEnv converts resource config to environment variables for the Python process.
func (c *ResourceConfig) ToEnv() []string {
	env := []string{}
	if c.MemoryLimitMB > 0 {
		env = append(env, fmt.Sprintf("RECURSE_MEMORY_LIMIT_MB=%d", c.MemoryLimitMB))
	}
	if c.CPUTimeLimitSec > 0 {
		env = append(env, fmt.Sprintf("RECURSE_CPU_LIMIT_SEC=%d", c.CPUTimeLimitSec))
	}
	return env
}

// ResourceConfigFromEnv parses resource config from environment variables.
func ResourceConfigFromEnv() ResourceConfig {
	config := DefaultResourceConfig()

	if val := os.Getenv("RECURSE_MEMORY_LIMIT_MB"); val != "" {
		if v, err := strconv.Atoi(val); err == nil && v > 0 {
			config.MemoryLimitMB = v
		}
	}
	if val := os.Getenv("RECURSE_CPU_LIMIT_SEC"); val != "" {
		if v, err := strconv.Atoi(val); err == nil && v > 0 {
			config.CPUTimeLimitSec = v
		}
	}

	return config
}

// Helper functions for platform-specific resource handling

// getProcessRusage gets resource usage for the current process or children.
// On Unix, we use RUSAGE_CHILDREN to get stats for child processes.
func getProcessRusage(pid int) (*syscall.Rusage, error) {
	var rusage syscall.Rusage
	// RUSAGE_CHILDREN gives us stats for terminated children.
	// For a running child, we need RUSAGE_SELF from within the child,
	// which we get via the Python status() call.
	// Here we use RUSAGE_SELF as a fallback for the current process.
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &rusage); err != nil {
		return nil, err
	}
	return &rusage, nil
}

// timevalToMicros converts a syscall.Timeval to microseconds.
func timevalToMicros(tv syscall.Timeval) int64 {
	return tv.Sec*1000000 + int64(tv.Usec)
}

// maxRSSToMB converts max RSS to megabytes.
// On macOS, Maxrss is in bytes; on Linux it's in kilobytes.
func maxRSSToMB(maxrss int64) float64 {
	if runtime.GOOS == "darwin" {
		return float64(maxrss) / (1024 * 1024)
	}
	// Linux: Maxrss is in KB
	return float64(maxrss) / 1024
}

// ResourceError is returned when a resource limit is violated.
type ResourceError struct {
	Violation *ResourceViolation
	Stats     *ResourceStats
	Message   string
}

func (e *ResourceError) Error() string {
	return e.Message
}

// NewResourceError creates a resource error with full context.
func NewResourceError(violation *ResourceViolation, stats *ResourceStats) *ResourceError {
	return &ResourceError{
		Violation: violation,
		Stats:     stats,
		Message:   violation.Error(),
	}
}

// ResourceCallback is called when resource events occur.
type ResourceCallback func(event ResourceEvent)

// ResourceEvent describes a resource-related event.
type ResourceEvent struct {
	Type      string          // "warning", "limit_exceeded", "stats"
	Stats     *ResourceStats  // Current stats
	Violation *ResourceViolation // Non-nil if a violation occurred
	Timestamp time.Time
}
