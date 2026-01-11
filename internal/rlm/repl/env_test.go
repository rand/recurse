package repl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindPkgPython(t *testing.T) {
	pkgDir, err := findPkgPython()
	require.NoError(t, err)
	assert.NotEmpty(t, pkgDir)

	// Verify it's actually the pkg/python directory
	assert.True(t, filepath.Base(pkgDir) == "python")
	assert.True(t, filepath.Base(filepath.Dir(pkgDir)) == "pkg")

	// Verify bootstrap.py exists there
	bootstrapPath := filepath.Join(pkgDir, "bootstrap.py")
	_, err = os.Stat(bootstrapPath)
	assert.NoError(t, err)

	// Verify pyproject.toml exists
	pyprojectPath := filepath.Join(pkgDir, "pyproject.toml")
	_, err = os.Stat(pyprojectPath)
	assert.NoError(t, err)
}
