package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rand/recurse/internal/config"
	"github.com/rand/recurse/internal/memory/embeddings"
	"github.com/rand/recurse/internal/memory/evolution"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/spf13/cobra"
)

func init() {
	// memory search flags
	memorySearchCmd.Flags().IntP("limit", "n", 10, "Maximum number of results")
	memorySearchCmd.Flags().StringP("tier", "t", "", "Filter by tier (task, session, longterm)")
	memorySearchCmd.Flags().StringP("type", "T", "", "Filter by node type (fact, entity, code_snippet, etc.)")
	memorySearchCmd.Flags().BoolP("json", "j", false, "Output as JSON")

	// memory export flags
	memoryExportCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	memoryExportCmd.Flags().StringP("format", "f", "json", "Output format (json, jsonl)")

	// memory gc flags
	memoryGCCmd.Flags().BoolP("dry-run", "n", false, "Show what would be done without making changes")
	memoryGCCmd.Flags().BoolP("prune", "p", false, "Also prune low-confidence nodes")

	memoryCmd.AddCommand(
		memorySearchCmd,
		memoryStatsCmd,
		memoryGCCmd,
		memoryExportCmd,
	)
}

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Memory management commands",
	Long:  "Commands for managing the hypergraph memory system",
}

var memorySearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search memory",
	Long:  "Search the hypergraph memory for nodes matching the query",
	Example: `
# Search for authentication-related nodes
recurse memory search "authentication"

# Search with limit and JSON output
recurse memory search -n 5 -j "user login"

# Search in long-term memory only
recurse memory search -t longterm "database schema"
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]
		limit, _ := cmd.Flags().GetInt("limit")
		tier, _ := cmd.Flags().GetString("tier")
		nodeType, _ := cmd.Flags().GetString("type")
		asJSON, _ := cmd.Flags().GetBool("json")

		store, cleanup, err := openMemoryStore(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		opts := hypergraph.SearchOptions{
			Limit: limit,
		}

		if tier != "" {
			opts.Tiers = []hypergraph.Tier{hypergraph.Tier(tier)}
		}
		if nodeType != "" {
			opts.Types = []hypergraph.NodeType{hypergraph.NodeType(nodeType)}
		}

		results, err := store.Search(cmd.Context(), query, opts)
		if err != nil {
			return fmt.Errorf("search failed: %w", err)
		}

		if asJSON {
			return json.NewEncoder(os.Stdout).Encode(results)
		}

		if len(results) == 0 {
			fmt.Println("No results found.")
			return nil
		}

		for i, r := range results {
			fmt.Printf("\n[%d] %s (%s) - %.2f\n", i+1, r.Node.Type, r.Node.Tier, r.Score)
			fmt.Printf("    ID: %s\n", r.Node.ID)
			content := r.Node.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Printf("    %s\n", content)
		}
		fmt.Println()

		return nil
	},
}

var memoryStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show memory statistics",
	Long:  "Display statistics about the hypergraph memory store",
	Example: `
# Show memory statistics
recurse memory stats
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, cleanup, err := openMemoryStore(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		stats, err := store.Stats(cmd.Context())
		if err != nil {
			return fmt.Errorf("failed to get stats: %w", err)
		}

		fmt.Println("Memory Statistics")
		fmt.Println("=================")
		fmt.Printf("Total nodes:     %d\n", stats.NodeCount)
		fmt.Printf("Total hyperedges: %d\n", stats.HyperedgeCount)
		fmt.Println()

		fmt.Println("Nodes by Tier:")
		for tier, count := range stats.NodesByTier {
			fmt.Printf("  %-12s %d\n", tier+":", count)
		}
		fmt.Println()

		fmt.Println("Nodes by Type:")
		for nodeType, count := range stats.NodesByType {
			fmt.Printf("  %-15s %d\n", nodeType+":", count)
		}

		if store.HasEmbeddings() {
			fmt.Println()
			fmt.Println("Embeddings: enabled")
		}

		return nil
	},
}

var memoryGCCmd = &cobra.Command{
	Use:   "gc",
	Short: "Run garbage collection",
	Long:  "Run memory garbage collection to archive or prune low-value nodes",
	Example: `
# Dry run to see what would be affected
recurse memory gc --dry-run

# Run GC with pruning
recurse memory gc --prune
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		prune, _ := cmd.Flags().GetBool("prune")

		store, cleanup, err := openMemoryStore(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		ctx := cmd.Context()

		// Create lifecycle manager for GC operations
		cfg := evolution.DefaultLifecycleConfig()
		cfg.RunArchiveOnIdle = true
		cfg.RunPruneOnIdle = prune

		lm, err := evolution.NewLifecycleManager(store, cfg)
		if err != nil {
			return fmt.Errorf("create lifecycle manager: %w", err)
		}

		if dryRun {
			fmt.Println("Dry run mode - no changes will be made")
			fmt.Println()

			// Get stats before
			stats, err := store.Stats(ctx)
			if err != nil {
				return err
			}

			fmt.Printf("Current state:\n")
			fmt.Printf("  Total nodes: %d\n", stats.NodeCount)
			for tier, count := range stats.NodesByTier {
				fmt.Printf("  %-12s %d\n", tier+":", count)
			}

			// Show decay stats
			decayer := evolution.NewDecayer(store, cfg.Decay)
			decayStats, err := decayer.GetDecayStats(ctx)
			if err != nil {
				return fmt.Errorf("get decay stats: %w", err)
			}

			fmt.Printf("\nDecay statistics:\n")
			fmt.Printf("  Total nodes:      %d\n", decayStats.TotalNodes)
			fmt.Printf("  Archived nodes:   %d\n", decayStats.ArchivedNodes)
			fmt.Printf("  At-risk nodes:    %d\n", decayStats.AtRiskNodes)
			fmt.Printf("  Avg confidence:   %.2f\n", decayStats.AverageConfidence)

			return nil
		}

		fmt.Println("Running garbage collection...")
		result, err := lm.IdleMaintenance(ctx)
		if err != nil {
			return fmt.Errorf("gc failed: %w", err)
		}

		fmt.Printf("GC completed in %v\n", result.Duration)

		if result.Decay != nil {
			fmt.Printf("  Archived: %d nodes\n", result.Decay.NodesArchived)
			fmt.Printf("  Pruned:   %d nodes\n", result.Decay.NodesPruned)
		}

		if len(result.Errors) > 0 {
			fmt.Println("\nWarnings:")
			for _, e := range result.Errors {
				fmt.Printf("  - %v\n", e)
			}
		}

		return nil
	},
}

var memoryExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export memory to JSON",
	Long:  "Export the hypergraph memory to JSON format for backup or analysis",
	Example: `
# Export to stdout
recurse memory export

# Export to file
recurse memory export -o memory.json
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		format, _ := cmd.Flags().GetString("format")

		store, cleanup, err := openMemoryStore(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		ctx := cmd.Context()

		// Get all nodes (including archived for export)
		nodes, err := store.RecentNodes(ctx, 0, nil)
		if err != nil {
			return fmt.Errorf("failed to get nodes: %w", err)
		}

		stats, err := store.Stats(ctx)
		if err != nil {
			return fmt.Errorf("failed to get stats: %w", err)
		}

		export := struct {
			ExportedAt string             `json:"exported_at"`
			Stats      *hypergraph.Stats  `json:"stats"`
			Nodes      []*hypergraph.Node `json:"nodes"`
		}{
			ExportedAt: time.Now().Format(time.RFC3339),
			Stats:      stats,
			Nodes:      nodes,
		}

		// Determine output destination
		var w = os.Stdout
		if output != "" {
			f, err := os.Create(output)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer f.Close()
			w = f
		}

		encoder := json.NewEncoder(w)
		if format == "json" {
			encoder.SetIndent("", "  ")
		}

		if err := encoder.Encode(export); err != nil {
			return fmt.Errorf("encode export: %w", err)
		}

		if output != "" {
			fmt.Fprintf(os.Stderr, "Exported %d nodes to %s\n", len(nodes), output)
		}

		return nil
	},
}

// openMemoryStore opens the hypergraph memory store for CLI commands.
func openMemoryStore(cmd *cobra.Command) (*hypergraph.Store, func(), error) {
	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, nil, err
	}

	dataDir, _ := cmd.Flags().GetString("data-dir")
	cfg, err := config.Init(cwd, dataDir, false)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	// Build database path
	dbPath := cfg.Options.DataDirectory + "/memory.db"

	// Try to initialize embedding provider (optional)
	var provider embeddings.Provider
	provider, _ = embeddings.NewProvider() // Ignore errors - embeddings are optional

	store, err := hypergraph.NewStore(hypergraph.Options{
		Path:              dbPath,
		CreateIfNotExists: true,
		EmbeddingProvider: provider,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("open memory store: %w", err)
	}

	cleanup := func() {
		store.Close()
	}

	return store, cleanup, nil
}
