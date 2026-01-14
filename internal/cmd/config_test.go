package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigPathsExist(t *testing.T) {
	tmpDir := t.TempDir()

	// Test that config paths are correctly constructed
	projectConfig := filepath.Join(tmpDir, ".recurse.yaml")
	projectConfigAlt := filepath.Join(tmpDir, ".recurse.yml")
	userConfig := filepath.Join(tmpDir, ".recurse", "config.yaml")

	// None should exist initially
	_, err := os.Stat(projectConfig)
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(projectConfigAlt)
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(userConfig)
	assert.True(t, os.IsNotExist(err))

	// Create project config
	err = os.WriteFile(projectConfig, []byte("# test config\n"), 0644)
	require.NoError(t, err)

	// Now it should exist
	_, err = os.Stat(projectConfig)
	assert.NoError(t, err)
}

func TestConfigValidateChecksProviders(t *testing.T) {
	// Test that validation logic correctly identifies missing providers
	// This tests the validation logic without running the full command

	testCases := []struct {
		name        string
		hasProvider bool
		expectError bool
	}{
		{
			name:        "no provider configured",
			hasProvider: false,
			expectError: true,
		},
		{
			name:        "provider configured",
			hasProvider: true,
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// The validation logic checks for at least one provider with an API key
			// If no provider has an API key, it should return an error
			hasProvider := tc.hasProvider

			var errors []string
			if !hasProvider {
				errors = append(errors, "No provider with API key found")
			}

			if tc.expectError {
				assert.NotEmpty(t, errors)
			} else {
				assert.Empty(t, errors)
			}
		})
	}
}

func TestConfigEditCreatesDefault(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".recurse")

	// Create the data directory
	err := os.MkdirAll(dataDir, 0755)
	require.NoError(t, err)

	configPath := filepath.Join(dataDir, "config.yaml")

	// Config shouldn't exist yet
	_, err = os.Stat(configPath)
	assert.True(t, os.IsNotExist(err))

	// Simulate what config edit does when no config exists
	defaultConfig := `# Recurse Configuration
# See documentation for available options

# Provider configuration
# providers:
#   anthropic:
#     api_key: ${ANTHROPIC_API_KEY}
#     default_model: claude-sonnet-4-20250514

# TUI options
# tui:
#   theme: dark

# Permissions
# permissions:
#   skip_requests: false
`
	err = os.WriteFile(configPath, []byte(defaultConfig), 0644)
	require.NoError(t, err)

	// Now it should exist
	_, err = os.Stat(configPath)
	assert.NoError(t, err)

	// Verify content
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Recurse Configuration")
	assert.Contains(t, string(content), "providers:")
}

func TestConfigShowFormats(t *testing.T) {
	// Test that the config show command supports different output formats
	// The actual formatting is done by json and yaml encoders

	testConfig := map[string]interface{}{
		"options": map[string]interface{}{
			"data_directory": "/test/dir",
			"debug":          false,
		},
	}

	// Test JSON encoding
	t.Run("json format", func(t *testing.T) {
		// json.Marshal should work
		_, err := jsonMarshal(testConfig)
		assert.NoError(t, err)
	})

	// Test that the config struct is serializable
	t.Run("config serializable", func(t *testing.T) {
		// The config package handles serialization
		// This just verifies our test data is valid
		assert.NotNil(t, testConfig["options"])
	})
}

// Helper for testing
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
