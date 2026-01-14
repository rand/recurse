package lats

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Comprehensive benchmarks for LATS performance evaluation.

// BenchmarkController_Solve benchmarks the full MCTS solve loop.
func BenchmarkController_Solve(b *testing.B) {
	config := Config{
		MaxIterations:       20,
		MaxDepth:            5,
		ExplorationConstant: 1.414,
		TokenBudget:         10000,
		Timeout:             30 * time.Second,
		ValueDecay:          0.95,
	}

	expander := &MockExpander{ActionsPerNode: 3, Tools: []string{"a", "b", "c"}}
	simulator := &MockSimulator{
		ValueFunc:    func(n *Node) float64 { return 0.5 },
		TerminalFunc: func(n *Node) bool { return n.Depth >= 4 },
	}

	ctrl := NewController(expander, simulator, nil, nil, config)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ctrl.Solve(ctx, "benchmark query")
	}
}

// BenchmarkController_Solve_Varying benchmarks solve with varying iterations.
func BenchmarkController_Solve_Varying(b *testing.B) {
	iterations := []int{10, 25, 50, 100}

	for _, maxIter := range iterations {
		b.Run(fmt.Sprintf("iterations-%d", maxIter), func(b *testing.B) {
			config := Config{
				MaxIterations:       maxIter,
				MaxDepth:            6,
				ExplorationConstant: 1.414,
				TokenBudget:         100000,
				Timeout:             30 * time.Second,
				ValueDecay:          0.95,
			}

			expander := &MockExpander{ActionsPerNode: 3}
			simulator := &MockSimulator{
				ValueFunc:    func(n *Node) float64 { return 0.5 },
				TerminalFunc: func(n *Node) bool { return n.Depth >= 5 },
			}

			ctrl := NewController(expander, simulator, nil, nil, config)
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = ctrl.Solve(ctx, "benchmark query")
			}
		})
	}
}

// BenchmarkController_Solve_VaryingBranching benchmarks different branching factors.
func BenchmarkController_Solve_VaryingBranching(b *testing.B) {
	branchingFactors := []int{2, 3, 5, 8}

	for _, bf := range branchingFactors {
		b.Run(fmt.Sprintf("branching-%d", bf), func(b *testing.B) {
			config := Config{
				MaxIterations:       30,
				MaxDepth:            5,
				ExplorationConstant: 1.414,
				TokenBudget:         50000,
				Timeout:             30 * time.Second,
				ValueDecay:          0.95,
			}

			expander := &MockExpander{ActionsPerNode: bf}
			simulator := &MockSimulator{
				ValueFunc:    func(n *Node) float64 { return 0.5 },
				TerminalFunc: func(n *Node) bool { return n.Depth >= 4 },
			}

			ctrl := NewController(expander, simulator, nil, nil, config)
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = ctrl.Solve(ctx, "benchmark query")
			}
		})
	}
}

// BenchmarkBackpropagate benchmarks value backpropagation.
func BenchmarkBackpropagate(b *testing.B) {
	depths := []int{3, 5, 10, 20}

	for _, depth := range depths {
		b.Run(fmt.Sprintf("depth-%d", depth), func(b *testing.B) {
			config := Config{ValueDecay: 0.95}
			ctrl := NewController(nil, nil, nil, nil, config)

			// Build chain
			nodes := make([]*Node, depth+1)
			nodes[0] = &Node{ID: "root"}
			tree := NewTree(nodes[0])

			for i := 1; i <= depth; i++ {
				nodes[i] = &Node{ID: fmt.Sprintf("n%d", i), ParentID: nodes[i-1].ID}
				tree.AddNode(nodes[i])
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Reset visits for fair comparison
				for _, n := range nodes {
					n.Visits = 0
					n.TotalValue = 0
				}
				ctrl.backpropagate(tree, nodes[depth], 0.8)
			}
		})
	}
}

// BenchmarkTree_AddNode benchmarks tree node addition.
func BenchmarkTree_AddNode(b *testing.B) {
	root := &Node{ID: "root"}
	tree := NewTree(root)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		node := &Node{ID: fmt.Sprintf("node-%d", i), ParentID: "root"}
		tree.AddNode(node)
	}
}

// BenchmarkTree_GetNode benchmarks node lookup.
func BenchmarkTree_GetNode(b *testing.B) {
	root := &Node{ID: "root"}
	tree := NewTree(root)

	// Add many nodes
	for i := 0; i < 1000; i++ {
		tree.AddNode(&Node{ID: fmt.Sprintf("node-%d", i), ParentID: "root"})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tree.GetNode(fmt.Sprintf("node-%d", i%1000))
	}
}

// BenchmarkTree_Stats benchmarks tree statistics calculation.
func BenchmarkTree_Stats(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			root := &Node{ID: "root", Depth: 0}
			tree := NewTree(root)

			// Build tree
			nodes := []*Node{root}
			for i := 1; i < size; i++ {
				parent := nodes[i%len(nodes)]
				child := &Node{
					ID:       fmt.Sprintf("node-%d", i),
					ParentID: parent.ID,
					Depth:    parent.Depth + 1,
				}
				parent.Children = append(parent.Children, child)
				tree.AddNode(child)
				nodes = append(nodes, child)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = tree.Stats()
			}
		})
	}
}

// BenchmarkBudget_Operations benchmarks budget tracking.
func BenchmarkBudget_Operations(b *testing.B) {
	b.Run("Deduct", func(b *testing.B) {
		budget := NewBudget(1000000)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			budget.Deduct(10)
		}
	})

	b.Run("Remaining", func(b *testing.B) {
		budget := NewBudget(1000000)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = budget.Remaining()
		}
	})

	b.Run("Exhausted", func(b *testing.B) {
		budget := NewBudget(1000000)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = budget.Exhausted()
		}
	})
}

// BenchmarkHeuristicValuator benchmarks heuristic valuation.
func BenchmarkHeuristicValuator(b *testing.B) {
	valuator := NewHeuristicValuator()
	ctx := context.Background()

	observations := []int{1, 5, 10, 20}

	for _, numObs := range observations {
		b.Run(fmt.Sprintf("observations-%d", numObs), func(b *testing.B) {
			obs := make([]Observation, numObs)
			for i := 0; i < numObs; i++ {
				obs[i] = Observation{Success: i%2 == 0}
			}
			node := &Node{
				Depth: 3,
				State: &AgentState{Observations: obs},
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = valuator.Value(ctx, node)
			}
		})
	}
}

// BenchmarkToolRegistry benchmarks tool registry operations.
func BenchmarkToolRegistry(b *testing.B) {
	b.Run("Register", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			registry := NewToolRegistry()
			for j := 0; j < 10; j++ {
				tool := NewMockTool(fmt.Sprintf("tool-%d", j), "desc", nil)
				registry.Register(tool)
			}
		}
	})

	b.Run("Has", func(b *testing.B) {
		registry := NewToolRegistry()
		for i := 0; i < 20; i++ {
			registry.Register(NewMockTool(fmt.Sprintf("tool-%d", i), "desc", nil))
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = registry.Has(fmt.Sprintf("tool-%d", i%20))
		}
	})

	b.Run("Execute", func(b *testing.B) {
		registry := NewToolRegistry()
		registry.Register(NewMockTool("test", "desc", func(ctx context.Context, input string) (*ToolResult, error) {
			return &ToolResult{Output: "ok", Success: true}, nil
		}))
		ctx := context.Background()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = registry.Execute(ctx, "test", "input")
		}
	})
}

// BenchmarkCapabilityMatcher benchmarks capability matching.
func BenchmarkCapabilityMatcher(b *testing.B) {
	profiles := DefaultToolProfiles()
	matcher := NewCapabilityMatcher(profiles)

	b.Run("FindByCapability", func(b *testing.B) {
		caps := []ToolCapability{CapFileRead, CapSearch, CapCodeExecution}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = matcher.FindByCapability(caps[i%len(caps)])
		}
	})

	b.Run("BestToolFor", func(b *testing.B) {
		caps := []ToolCapability{CapFileRead, CapSearch, CapCodeExecution}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = matcher.BestToolFor(caps[i%len(caps)])
		}
	})
}

// BenchmarkQueryAnalyzer benchmarks query analysis.
func BenchmarkQueryAnalyzer(b *testing.B) {
	qa := NewQueryAnalyzer()

	queries := []string{
		"Read the file main.go",
		"Calculate the sum of these numbers",
		"Search for all Python files",
		"Execute this shell command",
		"Remember this information for later",
	}

	b.Run("Analyze", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = qa.Analyze(queries[i%len(queries)])
		}
	})

	b.Run("CapabilityScore", func(b *testing.B) {
		req := &QueryRequirements{
			Required: []CapabilityRequirement{
				{Capability: CapFileReadSingle, Level: RequirementRequired},
			},
			Preferred: []CapabilityRequirement{
				{Capability: CapSearchContent, Level: RequirementPreferred},
			},
		}
		profile := ToolProfile{
			Capabilities: []ToolCapability{CapFileReadSingle, CapSearchContent},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = qa.CapabilityScore(profile, req)
		}
	})
}

// BenchmarkRecommendTools benchmarks tool recommendation.
func BenchmarkRecommendTools(b *testing.B) {
	profiles := GetAgentToolMatrix()
	req := &QueryRequirements{
		Required: []CapabilityRequirement{
			{Capability: CapCodeExecutePython, Level: RequirementRequired},
		},
		Preferred: []CapabilityRequirement{
			{Capability: CapComputeMath, Level: RequirementPreferred},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RecommendTools(req, profiles)
	}
}

// BenchmarkAgentState_Clone benchmarks state cloning.
func BenchmarkAgentState_Clone(b *testing.B) {
	sizes := []int{0, 5, 10, 20}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("observations-%d", size), func(b *testing.B) {
			obs := make([]Observation, size)
			for i := 0; i < size; i++ {
				obs[i] = Observation{
					Action:  &Action{Tool: "test", Input: "input"},
					Result:  "result",
					Success: true,
					Tokens:  10,
				}
			}
			state := &AgentState{
				Query:          "test query",
				CurrentContext: "some context",
				TokensUsed:     1000,
				Observations:   obs,
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = state.Clone()
			}
		})
	}
}

// BenchmarkReactiveVsLATS compares reactive and LATS approaches.
func BenchmarkReactiveVsLATS(b *testing.B) {
	// Reactive: single tool call per step
	b.Run("Reactive", func(b *testing.B) {
		registry := NewToolRegistry()
		registry.Register(NewMockTool("tool", "desc", func(ctx context.Context, input string) (*ToolResult, error) {
			return &ToolResult{Output: "done", Success: true, Tokens: 10}, nil
		}))
		ctx := context.Background()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate reactive: just execute 5 tool calls sequentially
			for j := 0; j < 5; j++ {
				_, _ = registry.Execute(ctx, "tool", "input")
			}
		}
	})

	// LATS: MCTS-based planning
	b.Run("LATS", func(b *testing.B) {
		config := Config{
			MaxIterations:       15,
			MaxDepth:            5,
			ExplorationConstant: 1.414,
			TokenBudget:         10000,
			Timeout:             30 * time.Second,
			ValueDecay:          0.95,
		}

		expander := &MockExpander{ActionsPerNode: 2}
		simulator := &MockSimulator{
			ValueFunc:    func(n *Node) float64 { return 0.6 },
			TerminalFunc: func(n *Node) bool { return n.Depth >= 4 },
		}

		ctrl := NewController(expander, simulator, nil, nil, config)
		ctx := context.Background()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = ctrl.Solve(ctx, "query")
		}
	})
}

// BenchmarkPlanningOverhead measures LATS planning overhead vs direct execution.
func BenchmarkPlanningOverhead(b *testing.B) {
	registry := NewToolRegistry()
	registry.Register(NewMockTool("fast", "Fast tool", func(ctx context.Context, input string) (*ToolResult, error) {
		return &ToolResult{Output: "result", Success: true, Tokens: 5}, nil
	}))
	ctx := context.Background()

	b.Run("DirectExecution", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 3; j++ {
				_, _ = registry.Execute(ctx, "fast", "input")
			}
		}
	})

	b.Run("LATSPlanning", func(b *testing.B) {
		config := Config{
			MaxIterations:       10,
			MaxDepth:            3,
			ExplorationConstant: 1.414,
			TokenBudget:         5000,
			Timeout:             10 * time.Second,
			ValueDecay:          0.95,
		}

		expander := &MockExpander{ActionsPerNode: 2, Tools: []string{"fast"}}
		valuator := NewHeuristicValuator()
		simulator := NewRealSimulator(registry, valuator)

		ctrl := NewController(expander, simulator, valuator, registry, config)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = ctrl.Solve(ctx, "query")
		}
	})
}
