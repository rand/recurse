package repl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	envOnce   sync.Once
	envPython string
	envErr    error
)

// EnsureEnv ensures the Python environment is set up with uv.
// Returns the path to the Python interpreter in the venv.
// This is idempotent and safe to call multiple times.
func EnsureEnv(ctx context.Context) (string, error) {
	envOnce.Do(func() {
		envPython, envErr = setupEnv(ctx)
	})
	return envPython, envErr
}

// setupEnv performs the actual environment setup.
func setupEnv(ctx context.Context) (string, error) {
	// Find the pkg/python directory
	pkgDir, err := findPkgPython()
	if err != nil {
		return "", fmt.Errorf("find pkg/python: %w", err)
	}

	// Check if uv is available
	uvPath, err := exec.LookPath("uv")
	if err != nil {
		// Fall back to system Python if uv not available
		return "python3", nil
	}

	// Run uv sync to ensure venv and dependencies
	cmd := exec.CommandContext(ctx, uvPath, "sync", "--frozen")
	cmd.Dir = pkgDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// If sync fails, try without --frozen (first time setup)
		cmd = exec.CommandContext(ctx, uvPath, "sync")
		cmd.Dir = pkgDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("uv sync: %w", err)
		}
	}

	// Return path to venv Python
	venvPython := filepath.Join(pkgDir, ".venv", "bin", "python")
	if _, err := os.Stat(venvPython); err != nil {
		return "", fmt.Errorf("venv python not found at %s: %w", venvPython, err)
	}

	return venvPython, nil
}

// findPkgPython locates the pkg/python directory.
func findPkgPython() (string, error) {
	// Try relative to executable first
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		candidates := []string{
			filepath.Join(exeDir, "pkg", "python"),
			filepath.Join(exeDir, "..", "pkg", "python"),
		}
		for _, p := range candidates {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				return filepath.Abs(p)
			}
		}
	}

	// Try relative to cwd (for development)
	cwd, _ := os.Getwd()

	// Walk up from cwd to find pkg/python
	dir := cwd
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "pkg", "python")
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return filepath.Abs(candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("pkg/python not found (searched from %s)", cwd)
}

// CheckTooling verifies that ruff and other tools are available.
func CheckTooling(ctx context.Context) error {
	pkgDir, err := findPkgPython()
	if err != nil {
		return err
	}

	// Check for ruff in venv
	ruffPath := filepath.Join(pkgDir, ".venv", "bin", "ruff")
	if _, err := os.Stat(ruffPath); err != nil {
		// Try to install dev dependencies
		uvPath, err := exec.LookPath("uv")
		if err != nil {
			return fmt.Errorf("uv not found, cannot install tooling")
		}

		cmd := exec.CommandContext(ctx, uvPath, "sync", "--dev")
		cmd.Dir = pkgDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("install dev tools: %w", err)
		}
	}

	return nil
}
