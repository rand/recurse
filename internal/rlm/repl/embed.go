package repl

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed bootstrap.py
var embeddedBootstrap []byte

// extractEmbeddedBootstrap extracts the embedded bootstrap.py to a temp file.
// Returns the path to the extracted file.
func extractEmbeddedBootstrap() (string, error) {
	if len(embeddedBootstrap) == 0 {
		return "", fmt.Errorf("embedded bootstrap.py is empty")
	}

	// Create temp dir for extracted files
	tmpDir, err := os.MkdirTemp("", "recurse-repl-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	bootstrapPath := filepath.Join(tmpDir, "bootstrap.py")
	if err := os.WriteFile(bootstrapPath, embeddedBootstrap, 0644); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("write bootstrap.py: %w", err)
	}

	return bootstrapPath, nil
}
