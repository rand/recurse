package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"plugin"
	"sync"
)

// REPLPlugin defines the interface for REPL extensions.
// Plugins provide domain-specific functions that can be called from Python code.
type REPLPlugin interface {
	// Name returns the plugin's unique identifier.
	Name() string

	// Description returns a human-readable description of the plugin.
	Description() string

	// Functions returns the map of function names to their implementations.
	Functions() map[string]REPLFunction

	// OnLoad is called when the plugin is registered with the manager.
	// It receives the REPL environment for any initialization needs.
	OnLoad(ctx context.Context) error

	// OnUnload is called when the plugin is unregistered.
	OnUnload() error
}

// REPLFunction represents a callable function provided by a plugin.
type REPLFunction struct {
	// Name is the function name as it appears in Python.
	Name string

	// Description describes what the function does.
	Description string

	// Parameters describes the expected parameters.
	Parameters []FunctionParameter

	// Handler is the Go function that implements the behavior.
	// It receives arbitrary arguments and returns a result or error.
	Handler func(ctx context.Context, args ...any) (any, error)
}

// FunctionParameter describes a function parameter.
type FunctionParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// PluginManager manages REPL plugins and their functions.
type PluginManager struct {
	mu        sync.RWMutex
	plugins   map[string]REPLPlugin
	functions map[string]*registeredFunction
}

// registeredFunction tracks a function and its source plugin.
type registeredFunction struct {
	plugin   REPLPlugin
	function REPLFunction
}

// NewPluginManager creates a new plugin manager.
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins:   make(map[string]REPLPlugin),
		functions: make(map[string]*registeredFunction),
	}
}

// Register registers a plugin with the manager.
// The plugin's functions become available for invocation.
func (pm *PluginManager) Register(ctx context.Context, p REPLPlugin) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	name := p.Name()
	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %q already registered", name)
	}

	// Call plugin's OnLoad
	if err := p.OnLoad(ctx); err != nil {
		return fmt.Errorf("plugin %q OnLoad: %w", name, err)
	}

	// Register all functions
	for fname, fn := range p.Functions() {
		fullName := fmt.Sprintf("%s_%s", name, fname)
		if _, exists := pm.functions[fullName]; exists {
			// Rollback: unload the plugin
			p.OnUnload()
			return fmt.Errorf("function %q already registered", fullName)
		}
		pm.functions[fullName] = &registeredFunction{
			plugin:   p,
			function: fn,
		}
		slog.Debug("registered plugin function", "plugin", name, "function", fname, "full_name", fullName)
	}

	pm.plugins[name] = p
	slog.Info("registered plugin", "name", name, "functions", len(p.Functions()))

	return nil
}

// Unregister removes a plugin and its functions.
func (pm *PluginManager) Unregister(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	p, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %q not found", name)
	}

	// Remove all functions from this plugin
	for fname := range p.Functions() {
		fullName := fmt.Sprintf("%s_%s", name, fname)
		delete(pm.functions, fullName)
	}

	// Call plugin's OnUnload
	if err := p.OnUnload(); err != nil {
		slog.Warn("plugin OnUnload error", "plugin", name, "error", err)
	}

	delete(pm.plugins, name)
	slog.Info("unregistered plugin", "name", name)

	return nil
}

// Call invokes a plugin function by name.
func (pm *PluginManager) Call(ctx context.Context, name string, args ...any) (any, error) {
	pm.mu.RLock()
	reg, exists := pm.functions[name]
	pm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("function %q not found", name)
	}

	return reg.function.Handler(ctx, args...)
}

// HasFunction checks if a function is registered.
func (pm *PluginManager) HasFunction(name string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	_, exists := pm.functions[name]
	return exists
}

// ListPlugins returns all registered plugin names.
func (pm *PluginManager) ListPlugins() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}
	return names
}

// ListFunctions returns all registered function names.
func (pm *PluginManager) ListFunctions() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.functions))
	for name := range pm.functions {
		names = append(names, name)
	}
	return names
}

// GetPlugin returns a plugin by name.
func (pm *PluginManager) GetPlugin(name string) (REPLPlugin, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	p, exists := pm.plugins[name]
	return p, exists
}

// GetFunctionInfo returns information about a registered function.
func (pm *PluginManager) GetFunctionInfo(name string) (*FunctionInfo, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	reg, exists := pm.functions[name]
	if !exists {
		return nil, false
	}

	return &FunctionInfo{
		Name:        name,
		Plugin:      reg.plugin.Name(),
		Description: reg.function.Description,
		Parameters:  reg.function.Parameters,
	}, true
}

// FunctionInfo provides metadata about a registered function.
type FunctionInfo struct {
	Name        string              `json:"name"`
	Plugin      string              `json:"plugin"`
	Description string              `json:"description"`
	Parameters  []FunctionParameter `json:"parameters"`
}

// GenerateManifest creates a JSON manifest of all registered functions.
// This can be injected into the Python REPL for function discovery.
func (pm *PluginManager) GenerateManifest() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	manifest := struct {
		Plugins   []PluginManifest   `json:"plugins"`
		Functions []FunctionManifest `json:"functions"`
	}{}

	for name, p := range pm.plugins {
		manifest.Plugins = append(manifest.Plugins, PluginManifest{
			Name:        name,
			Description: p.Description(),
		})
	}

	for name, reg := range pm.functions {
		manifest.Functions = append(manifest.Functions, FunctionManifest{
			Name:        name,
			Plugin:      reg.plugin.Name(),
			Description: reg.function.Description,
			Parameters:  reg.function.Parameters,
		})
	}

	data, _ := json.MarshalIndent(manifest, "", "  ")
	return string(data)
}

// PluginManifest describes a plugin in the manifest.
type PluginManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// FunctionManifest describes a function in the manifest.
type FunctionManifest struct {
	Name        string              `json:"name"`
	Plugin      string              `json:"plugin"`
	Description string              `json:"description"`
	Parameters  []FunctionParameter `json:"parameters"`
}

// DiscoverPlugins loads plugins from the standard plugin directories.
// It looks in:
// - ~/.recurse/plugins/
// - ./plugins/ (relative to working directory)
func (pm *PluginManager) DiscoverPlugins(ctx context.Context) error {
	dirs := []string{}

	// User plugins directory
	home, err := os.UserHomeDir()
	if err == nil {
		dirs = append(dirs, filepath.Join(home, ".recurse", "plugins"))
	}

	// Local plugins directory
	cwd, err := os.Getwd()
	if err == nil {
		dirs = append(dirs, filepath.Join(cwd, "plugins"))
	}

	for _, dir := range dirs {
		if err := pm.loadPluginsFromDir(ctx, dir); err != nil {
			slog.Debug("plugin discovery skipped directory", "dir", dir, "error", err)
		}
	}

	return nil
}

// loadPluginsFromDir loads all .so plugins from a directory.
func (pm *PluginManager) loadPluginsFromDir(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if filepath.Ext(name) != ".so" {
			continue
		}

		path := filepath.Join(dir, name)
		if err := pm.LoadPlugin(ctx, path); err != nil {
			slog.Warn("failed to load plugin", "path", path, "error", err)
		}
	}

	return nil
}

// LoadPlugin loads a plugin from a shared object file.
// The plugin must export a "NewPlugin" symbol that returns REPLPlugin.
func (pm *PluginManager) LoadPlugin(ctx context.Context, path string) error {
	p, err := plugin.Open(path)
	if err != nil {
		return fmt.Errorf("open plugin: %w", err)
	}

	sym, err := p.Lookup("NewPlugin")
	if err != nil {
		return fmt.Errorf("lookup NewPlugin: %w", err)
	}

	newPlugin, ok := sym.(func() REPLPlugin)
	if !ok {
		return fmt.Errorf("NewPlugin has wrong signature")
	}

	replPlugin := newPlugin()
	return pm.Register(ctx, replPlugin)
}

// =============================================================================
// Built-in Plugins
// =============================================================================

// CodeAnalysisPlugin provides code analysis functions.
type CodeAnalysisPlugin struct{}

// NewCodeAnalysisPlugin creates a new code analysis plugin.
func NewCodeAnalysisPlugin() *CodeAnalysisPlugin {
	return &CodeAnalysisPlugin{}
}

func (p *CodeAnalysisPlugin) Name() string {
	return "code_analysis"
}

func (p *CodeAnalysisPlugin) Description() string {
	return "Provides code analysis functions for extracting structure from source files"
}

func (p *CodeAnalysisPlugin) OnLoad(ctx context.Context) error {
	return nil
}

func (p *CodeAnalysisPlugin) OnUnload() error {
	return nil
}

func (p *CodeAnalysisPlugin) Functions() map[string]REPLFunction {
	return map[string]REPLFunction{
		"count_lines": {
			Name:        "count_lines",
			Description: "Count lines, code lines, and comment lines in source code",
			Parameters: []FunctionParameter{
				{Name: "code", Type: "string", Description: "Source code to analyze", Required: true},
				{Name: "language", Type: "string", Description: "Programming language (go, python, etc.)", Required: false},
			},
			Handler: p.countLines,
		},
		"extract_imports": {
			Name:        "extract_imports",
			Description: "Extract import statements from source code",
			Parameters: []FunctionParameter{
				{Name: "code", Type: "string", Description: "Source code to analyze", Required: true},
				{Name: "language", Type: "string", Description: "Programming language", Required: true},
			},
			Handler: p.extractImports,
		},
		"find_functions": {
			Name:        "find_functions",
			Description: "Find function definitions in source code",
			Parameters: []FunctionParameter{
				{Name: "code", Type: "string", Description: "Source code to analyze", Required: true},
				{Name: "language", Type: "string", Description: "Programming language", Required: true},
			},
			Handler: p.findFunctions,
		},
	}
}

func (p *CodeAnalysisPlugin) countLines(ctx context.Context, args ...any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("code argument required")
	}

	code, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("code must be a string")
	}

	result := LineCount{
		Total:    0,
		Code:     0,
		Comment:  0,
		Blank:    0,
	}

	lines := splitLines(code)
	for _, line := range lines {
		result.Total++
		trimmed := trimSpace(line)

		if trimmed == "" {
			result.Blank++
		} else if isComment(trimmed) {
			result.Comment++
		} else {
			result.Code++
		}
	}

	return result, nil
}

func (p *CodeAnalysisPlugin) extractImports(ctx context.Context, args ...any) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("code and language arguments required")
	}

	code, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("code must be a string")
	}

	language, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("language must be a string")
	}

	var imports []string
	lines := splitLines(code)

	for _, line := range lines {
		trimmed := trimSpace(line)

		switch language {
		case "go":
			if hasPrefix(trimmed, "import ") || hasPrefix(trimmed, `"`) && hasSuffix(trimmed, `"`) {
				imports = append(imports, trimmed)
			}
		case "python":
			if hasPrefix(trimmed, "import ") || hasPrefix(trimmed, "from ") {
				imports = append(imports, trimmed)
			}
		case "javascript", "typescript":
			if hasPrefix(trimmed, "import ") || hasPrefix(trimmed, "require(") {
				imports = append(imports, trimmed)
			}
		}
	}

	return imports, nil
}

func (p *CodeAnalysisPlugin) findFunctions(ctx context.Context, args ...any) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("code and language arguments required")
	}

	code, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("code must be a string")
	}

	language, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("language must be a string")
	}

	var functions []FunctionDef
	lines := splitLines(code)

	for i, line := range lines {
		trimmed := trimSpace(line)

		switch language {
		case "go":
			if hasPrefix(trimmed, "func ") {
				name := extractGoFuncName(trimmed)
				if name != "" {
					functions = append(functions, FunctionDef{
						Name: name,
						Line: i + 1,
					})
				}
			}
		case "python":
			if hasPrefix(trimmed, "def ") || hasPrefix(trimmed, "async def ") {
				name := extractPythonFuncName(trimmed)
				if name != "" {
					functions = append(functions, FunctionDef{
						Name: name,
						Line: i + 1,
					})
				}
			}
		case "javascript", "typescript":
			if hasPrefix(trimmed, "function ") || contains(trimmed, "=> {") {
				name := extractJSFuncName(trimmed)
				if name != "" {
					functions = append(functions, FunctionDef{
						Name: name,
						Line: i + 1,
					})
				}
			}
		}
	}

	return functions, nil
}

// LineCount holds line counting results.
type LineCount struct {
	Total   int `json:"total"`
	Code    int `json:"code"`
	Comment int `json:"comment"`
	Blank   int `json:"blank"`
}

// FunctionDef represents a found function definition.
type FunctionDef struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// Helper functions (avoid importing strings for minimal dependencies)

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func isComment(line string) bool {
	return hasPrefix(line, "//") || hasPrefix(line, "#") || hasPrefix(line, "/*") || hasPrefix(line, "*")
}

func extractGoFuncName(line string) string {
	// "func Name(" or "func (r *Receiver) Name("
	line = line[5:] // skip "func "
	if hasPrefix(line, "(") {
		// Method - find closing paren
		for i := 1; i < len(line); i++ {
			if line[i] == ')' {
				line = trimSpace(line[i+1:])
				break
			}
		}
	}
	// Extract name before (
	for i := 0; i < len(line); i++ {
		if line[i] == '(' {
			return line[:i]
		}
	}
	return ""
}

func extractPythonFuncName(line string) string {
	// "def name(" or "async def name("
	if hasPrefix(line, "async def ") {
		line = line[10:]
	} else {
		line = line[4:] // skip "def "
	}
	for i := 0; i < len(line); i++ {
		if line[i] == '(' {
			return line[:i]
		}
	}
	return ""
}

func extractJSFuncName(line string) string {
	// "function name(" or "const name = ("
	if hasPrefix(line, "function ") {
		line = line[9:]
		for i := 0; i < len(line); i++ {
			if line[i] == '(' {
				return line[:i]
			}
		}
	}
	// Arrow function: "const name = " or "let name = "
	for _, prefix := range []string{"const ", "let ", "var "} {
		if hasPrefix(line, prefix) {
			line = line[len(prefix):]
			for i := 0; i < len(line); i++ {
				if line[i] == ' ' || line[i] == '=' {
					return line[:i]
				}
			}
		}
	}
	return ""
}

// =============================================================================
// Manager Integration
// =============================================================================

// SetPluginManager sets the plugin manager for handling plugin function calls.
func (m *Manager) SetPluginManager(pm *PluginManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pluginManager = pm
}

// PluginManager returns the current plugin manager.
func (m *Manager) PluginManager() *PluginManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pluginManager
}
