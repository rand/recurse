package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rand/recurse/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	// config show flags
	configShowCmd.Flags().BoolP("json", "j", false, "Output as JSON")
	configShowCmd.Flags().BoolP("yaml", "y", false, "Output as YAML")

	configCmd.AddCommand(
		configShowCmd,
		configEditCmd,
		configValidateCmd,
		configPathCmd,
	)
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
	Long:  "Commands for managing recurse configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show effective configuration",
	Long:  "Display the current effective configuration after merging all sources",
	Example: `
# Show config in human-readable format
recurse config show

# Show config as JSON
recurse config show --json

# Show config as YAML
recurse config show --yaml
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		asYAML, _ := cmd.Flags().GetBool("yaml")

		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}

		dataDir, _ := cmd.Flags().GetString("data-dir")
		debug, _ := cmd.Flags().GetBool("debug")

		cfg, err := config.Init(cwd, dataDir, debug)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if asJSON {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(cfg)
		}

		if asYAML {
			encoder := yaml.NewEncoder(os.Stdout)
			encoder.SetIndent(2)
			return encoder.Encode(cfg)
		}

		// Human-readable format
		fmt.Println("Effective Configuration")
		fmt.Println("=======================")
		fmt.Println()

		fmt.Println("Options:")
		fmt.Printf("  Data Directory:    %s\n", cfg.Options.DataDirectory)
		fmt.Printf("  Debug:             %v\n", cfg.Options.Debug)
		fmt.Printf("  Disable Metrics:   %v\n", cfg.Options.DisableMetrics)
		fmt.Println()

		if cfg.Providers != nil && cfg.Providers.Len() > 0 {
			fmt.Println("Providers:")
			for name, p := range cfg.Providers.Seq2() {
				fmt.Printf("  %s:\n", name)
				if p.BaseURL != "" {
					fmt.Printf("    Base URL:      %s\n", p.BaseURL)
				}
				if p.APIKey != "" {
					keyLen := len(p.APIKey)
					if keyLen > 8 {
						keyLen = 8
					}
					fmt.Printf("    API Key:       %s...\n", p.APIKey[:keyLen])
				}
			}
			fmt.Println()
		}

		if cfg.Permissions != nil {
			fmt.Println("Permissions:")
			fmt.Printf("  Skip Requests:   %v\n", cfg.Permissions.SkipRequests)
			if len(cfg.Permissions.AllowedTools) > 0 {
				fmt.Printf("  Allowed Tools:   %v\n", cfg.Permissions.AllowedTools)
			}
			fmt.Println()
		}

		return nil
	},
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open config in editor",
	Long:  "Open the configuration file in your default editor",
	Example: `
# Edit config with $EDITOR
recurse config edit
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}

		dataDir, _ := cmd.Flags().GetString("data-dir")
		cfg, err := config.Init(cwd, dataDir, false)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		// Find config file path
		// Check project-local first, then user config
		configPaths := []string{
			filepath.Join(cwd, ".recurse.yaml"),
			filepath.Join(cwd, ".recurse.yml"),
			filepath.Join(cfg.Options.DataDirectory, "config.yaml"),
		}

		var configPath string
		for _, p := range configPaths {
			if _, err := os.Stat(p); err == nil {
				configPath = p
				break
			}
		}

		if configPath == "" {
			// Create default config in data directory
			configPath = filepath.Join(cfg.Options.DataDirectory, "config.yaml")
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
			if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
				return fmt.Errorf("create default config: %w", err)
			}
			fmt.Printf("Created new config file: %s\n", configPath)
		}

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			editor = "vi"
		}

		execCmd := exec.Command(editor, configPath)
		execCmd.Stdin = os.Stdin
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr

		return execCmd.Run()
	},
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration",
	Long:  "Check the configuration for errors and warnings",
	Example: `
# Validate configuration
recurse config validate
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}

		dataDir, _ := cmd.Flags().GetString("data-dir")
		cfg, err := config.Init(cwd, dataDir, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ Configuration error: %v\n", err)
			return err
		}

		var warnings []string
		var errors []string

		// Check for API keys
		hasProvider := false
		if cfg.Providers != nil {
			for name, p := range cfg.Providers.Seq2() {
				if p.APIKey != "" {
					hasProvider = true
				} else {
					warnings = append(warnings, fmt.Sprintf("Provider '%s' has no API key configured", name))
				}
			}
		}

		if !hasProvider {
			errors = append(errors, "No provider with API key found - recurse requires at least one configured provider")
		}

		// Check data directory
		if cfg.Options.DataDirectory == "" {
			errors = append(errors, "Data directory not set")
		} else if _, err := os.Stat(cfg.Options.DataDirectory); os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("Data directory does not exist: %s (will be created)", cfg.Options.DataDirectory))
		}

		// Print results
		if len(errors) > 0 {
			fmt.Println("Errors:")
			for _, e := range errors {
				fmt.Printf("  ✗ %s\n", e)
			}
		}

		if len(warnings) > 0 {
			fmt.Println("Warnings:")
			for _, w := range warnings {
				fmt.Printf("  ⚠ %s\n", w)
			}
		}

		if len(errors) == 0 && len(warnings) == 0 {
			fmt.Println("✓ Configuration is valid")
		} else if len(errors) == 0 {
			fmt.Println("\n✓ Configuration is valid with warnings")
		}

		if len(errors) > 0 {
			return fmt.Errorf("configuration has %d error(s)", len(errors))
		}

		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file paths",
	Long:  "Display the paths where configuration files are loaded from",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}

		dataDir, _ := cmd.Flags().GetString("data-dir")
		cfg, err := config.Init(cwd, dataDir, false)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		fmt.Println("Configuration Paths (in order of precedence):")
		fmt.Println()

		paths := []struct {
			name   string
			path   string
			exists bool
		}{
			{"Project config", filepath.Join(cwd, ".recurse.yaml"), false},
			{"Project config (alt)", filepath.Join(cwd, ".recurse.yml"), false},
			{"User config", filepath.Join(cfg.Options.DataDirectory, "config.yaml"), false},
		}

		for i := range paths {
			if _, err := os.Stat(paths[i].path); err == nil {
				paths[i].exists = true
			}
		}

		for _, p := range paths {
			status := "✗"
			if p.exists {
				status = "✓"
			}
			fmt.Printf("  %s %s\n    %s\n", status, p.name, p.path)
		}

		fmt.Println()
		fmt.Printf("Data directory: %s\n", cfg.Options.DataDirectory)

		return nil
	},
}
