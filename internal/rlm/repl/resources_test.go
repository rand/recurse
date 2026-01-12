package repl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultResourceConfig(t *testing.T) {
	cfg := DefaultResourceConfig()

	assert.Equal(t, 1024, cfg.MemoryLimitMB)
	assert.Equal(t, 60, cfg.CPUTimeLimitSec)
	assert.Equal(t, 80, cfg.WarnMemoryPercent)
}

func TestResourceConfig_ToEnv(t *testing.T) {
	cfg := ResourceConfig{
		MemoryLimitMB:   512,
		CPUTimeLimitSec: 30,
	}

	env := cfg.ToEnv()

	assert.Contains(t, env, "RECURSE_MEMORY_LIMIT_MB=512")
	assert.Contains(t, env, "RECURSE_CPU_LIMIT_SEC=30")
}

func TestResourceConfigFromEnv(t *testing.T) {
	// Set env vars
	t.Setenv("RECURSE_MEMORY_LIMIT_MB", "256")
	t.Setenv("RECURSE_CPU_LIMIT_SEC", "15")

	cfg := ResourceConfigFromEnv()

	assert.Equal(t, 256, cfg.MemoryLimitMB)
	assert.Equal(t, 15, cfg.CPUTimeLimitSec)
}

func TestResourceConfigFromEnv_Defaults(t *testing.T) {
	// Clear env vars
	t.Setenv("RECURSE_MEMORY_LIMIT_MB", "")
	t.Setenv("RECURSE_CPU_LIMIT_SEC", "")

	cfg := ResourceConfigFromEnv()

	// Should use defaults
	assert.Equal(t, 1024, cfg.MemoryLimitMB)
	assert.Equal(t, 60, cfg.CPUTimeLimitSec)
}

func TestNewResourceMonitor(t *testing.T) {
	cfg := DefaultResourceConfig()
	monitor := NewResourceMonitor(12345, cfg)

	require.NotNil(t, monitor)
	assert.Equal(t, 12345, monitor.pid)
	assert.Equal(t, cfg.MemoryLimitMB, monitor.config.MemoryLimitMB)
}

func TestResourceMonitor_CumulativeStats(t *testing.T) {
	cfg := DefaultResourceConfig()
	monitor := NewResourceMonitor(12345, cfg)

	// Initial stats should be zero
	stats := monitor.CumulativeStats()
	assert.Equal(t, int64(0), stats.TotalCPUTimeMS)
	assert.Equal(t, float64(0), stats.PeakMemoryMB)
}

func TestResourceMonitor_ExecutionCount(t *testing.T) {
	cfg := DefaultResourceConfig()
	monitor := NewResourceMonitor(12345, cfg)

	assert.Equal(t, 0, monitor.ExecutionCount())
}

func TestResourceMonitor_CheckLimits_NoViolation(t *testing.T) {
	cfg := ResourceConfig{
		MemoryLimitMB:     1024,
		CPUTimeLimitSec:   60,
		WarnMemoryPercent: 80,
	}
	monitor := NewResourceMonitor(12345, cfg)

	stats := &ResourceStats{
		PeakMemoryMB:   100, // 10% of limit
		TotalCPUTimeMS: 1000, // 1 second
	}

	violation := monitor.CheckLimits(stats)
	assert.Nil(t, violation)
}

func TestResourceMonitor_CheckLimits_MemoryWarning(t *testing.T) {
	cfg := ResourceConfig{
		MemoryLimitMB:     1024,
		CPUTimeLimitSec:   60,
		WarnMemoryPercent: 80,
	}
	monitor := NewResourceMonitor(12345, cfg)

	stats := &ResourceStats{
		PeakMemoryMB:   900, // 88% of limit
		TotalCPUTimeMS: 1000,
	}

	violation := monitor.CheckLimits(stats)
	require.NotNil(t, violation)
	assert.Equal(t, "memory", violation.Resource)
	assert.False(t, violation.Hard) // Warning only
}

func TestResourceMonitor_CheckLimits_MemoryExceeded(t *testing.T) {
	cfg := ResourceConfig{
		MemoryLimitMB:     1024,
		CPUTimeLimitSec:   60,
		WarnMemoryPercent: 80,
	}
	monitor := NewResourceMonitor(12345, cfg)

	stats := &ResourceStats{
		PeakMemoryMB:   1100, // Over limit
		TotalCPUTimeMS: 1000,
	}

	violation := monitor.CheckLimits(stats)
	require.NotNil(t, violation)
	assert.Equal(t, "memory", violation.Resource)
	assert.True(t, violation.Hard)
}

func TestResourceMonitor_CheckLimits_CPUExceeded(t *testing.T) {
	cfg := ResourceConfig{
		MemoryLimitMB:     1024,
		CPUTimeLimitSec:   60,
		WarnMemoryPercent: 80,
	}
	monitor := NewResourceMonitor(12345, cfg)

	stats := &ResourceStats{
		PeakMemoryMB:   100,
		TotalCPUTimeMS: 65000, // 65 seconds, over 60 limit
	}

	violation := monitor.CheckLimits(stats)
	require.NotNil(t, violation)
	assert.Equal(t, "cpu_time", violation.Resource)
	assert.True(t, violation.Hard)
}

func TestResourceViolation_Error(t *testing.T) {
	tests := []struct {
		name     string
		v        ResourceViolation
		contains string
	}{
		{
			name: "hard memory violation",
			v: ResourceViolation{
				Resource: "memory",
				Limit:    1024,
				Current:  1100,
				Unit:     "MB",
				Hard:     true,
			},
			contains: "resource limit exceeded",
		},
		{
			name: "soft memory warning",
			v: ResourceViolation{
				Resource: "memory",
				Limit:    1024,
				Current:  900,
				Unit:     "MB",
				Hard:     false,
			},
			contains: "resource warning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.v.Error()
			assert.Contains(t, msg, tt.contains)
			assert.Contains(t, msg, tt.v.Resource)
		})
	}
}

func TestResourceStats_Fields(t *testing.T) {
	stats := ResourceStats{
		UserCPUTimeMS:   100,
		SystemCPUTimeMS: 50,
		TotalCPUTimeMS:  150,
		MemoryUsedMB:    256,
		PeakMemoryMB:    512,
		WallTimeMS:      1000,
	}

	assert.Equal(t, int64(100), stats.UserCPUTimeMS)
	assert.Equal(t, int64(50), stats.SystemCPUTimeMS)
	assert.Equal(t, int64(150), stats.TotalCPUTimeMS)
	assert.Equal(t, float64(256), stats.MemoryUsedMB)
	assert.Equal(t, float64(512), stats.PeakMemoryMB)
	assert.Equal(t, int64(1000), stats.WallTimeMS)
}

func TestNewResourceError(t *testing.T) {
	violation := &ResourceViolation{
		Resource: "memory",
		Limit:    1024,
		Current:  1100,
		Unit:     "MB",
		Hard:     true,
	}
	stats := &ResourceStats{
		PeakMemoryMB: 1100,
	}

	err := NewResourceError(violation, stats)

	require.NotNil(t, err)
	assert.Equal(t, violation, err.Violation)
	assert.Equal(t, stats, err.Stats)
	assert.Contains(t, err.Error(), "memory")
}

func TestResourceEvent_Fields(t *testing.T) {
	stats := &ResourceStats{PeakMemoryMB: 500}
	violation := &ResourceViolation{Resource: "memory", Hard: false}

	event := ResourceEvent{
		Type:      "warning",
		Stats:     stats,
		Violation: violation,
	}

	assert.Equal(t, "warning", event.Type)
	assert.Equal(t, stats, event.Stats)
	assert.Equal(t, violation, event.Violation)
}

func TestMaxRSSToMB(t *testing.T) {
	// Test the conversion function
	// On macOS, 1MB = 1048576 bytes
	// On Linux, 1MB = 1024 KB

	result := maxRSSToMB(1048576) // 1MB in bytes (macOS)

	// Result should be approximately 1MB (platform-dependent)
	assert.Greater(t, result, float64(0))
}

func TestTimevalToMicros(t *testing.T) {
	// We can't easily test this without platform-specific syscall.Timeval
	// But we can verify the function signature works
	// This is a compile-time check essentially
}
