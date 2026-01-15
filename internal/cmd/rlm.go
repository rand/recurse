package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/rand/recurse/internal/event"
	"github.com/spf13/cobra"
)

var rlmCmd = &cobra.Command{
	Use:   "rlm [task...]",
	Short: "Execute a task using Recursive Language Model orchestration",
	Long: `Execute a task using the RLM (Recursive Language Model) orchestration system.

RLM uses a meta-controller (Claude Haiku) to decide how to process tasks:
- DIRECT: Answer directly using current context
- DECOMPOSE: Break task into subtasks processed recursively
- MEMORY_QUERY: Retrieve relevant context from hypergraph memory
- SUBCALL: Process specific snippets with focused prompts
- SYNTHESIZE: Combine partial results into a coherent response

The task can be provided as arguments or piped from stdin.`,
	Example: `
# Execute a simple task
recurse rlm "Explain the RLM orchestration pattern"

# Execute a complex analysis task
recurse rlm "Analyze the memory management patterns in this codebase"

# Pipe input for analysis
cat main.go | recurse rlm "What does this code do?"

# Show trace information
recurse rlm --trace "Decompose the problem of implementing auth"

# Show memory statistics
recurse rlm --stats
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		showTrace, _ := cmd.Flags().GetBool("trace")
		showStats, _ := cmd.Flags().GetBool("stats")
		quiet, _ := cmd.Flags().GetBool("quiet")

		// Cancel on SIGINT or SIGTERM
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
		defer cancel()

		app, err := setupApp(cmd)
		if err != nil {
			return err
		}
		defer app.Shutdown()

		// Check if RLM is available
		if app.RLM == nil {
			return fmt.Errorf("RLM service not available - ensure an Anthropic provider is configured")
		}

		// Handle stats-only mode
		if showStats {
			stats := app.RLM.Stats()
			fmt.Printf("RLM Service Statistics:\n")
			fmt.Printf("  Total Executions: %d\n", stats.TotalExecutions)
			fmt.Printf("  Total Tokens:     %d\n", stats.TotalTokens)
			fmt.Printf("  Total Duration:   %s\n", stats.TotalDuration)
			fmt.Printf("  Tasks Completed:  %d\n", stats.TasksCompleted)
			fmt.Printf("  Sessions Ended:   %d\n", stats.SessionsEnded)
			fmt.Printf("  Errors:           %d\n", stats.Errors)

			traceStats := app.RLM.GetTraceStats()
			fmt.Printf("\nTrace Statistics:\n")
			fmt.Printf("  Total Events:     %d\n", traceStats.TotalEvents)
			fmt.Printf("  Total Tokens:     %d\n", traceStats.TotalTokens)
			fmt.Printf("  Max Depth:        %d\n", traceStats.MaxDepth)

			health, _ := app.RLM.HealthCheck(ctx)
			if health != nil {
				fmt.Printf("\nHealth Status:\n")
				fmt.Printf("  Running: %v\n", health.Running)
				fmt.Printf("  Healthy: %v\n", health.Healthy)
				for name, ok := range health.Checks {
					fmt.Printf("  %s: %v\n", name, ok)
				}
			}
			return nil
		}

		task := strings.Join(args, " ")

		task, err = MaybePrependStdin(task)
		if err != nil {
			slog.Error("Failed to read from stdin", "error", err)
			return err
		}

		if task == "" {
			return fmt.Errorf("no task provided")
		}

		event.SetNonInteractive(true)
		event.AppInitialized()

		// Clear previous trace events for a clean trace view
		if showTrace {
			_ = app.RLM.ClearTrace()
		}

		if !quiet {
			fmt.Fprintf(os.Stderr, "Executing RLM task...\n")
		}

		start := time.Now()
		result, err := app.RLM.Execute(ctx, task)
		if err != nil {
			return fmt.Errorf("RLM execution failed: %w", err)
		}

		// Print result
		fmt.Println(result.Response)

		// Show trace if requested
		if showTrace {
			fmt.Fprintf(os.Stderr, "\n--- Trace ---\n")
			events, _ := app.RLM.GetTraceEvents(50)
			for _, ev := range events {
				indent := strings.Repeat("  ", ev.Depth)
				fmt.Fprintf(os.Stderr, "%s[%s] %s (%d tokens)\n",
					indent, ev.Type, truncateStr(ev.Action, 60), ev.Tokens)
			}
			fmt.Fprintf(os.Stderr, "\nTotal tokens: %d, Duration: %s\n",
				result.TotalTokens, time.Since(start))
		}

		// Signal task completion
		app.RLMTaskComplete(ctx)

		return nil
	},
	PostRun: func(cmd *cobra.Command, args []string) {
		event.AppExited()
	},
}

func init() {
	rlmCmd.Flags().BoolP("trace", "t", false, "Show execution trace")
	rlmCmd.Flags().BoolP("stats", "s", false, "Show RLM statistics only")
	rlmCmd.Flags().BoolP("quiet", "q", false, "Suppress progress output")
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
