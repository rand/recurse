package tot

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkExploreWithBFS benchmarks BFS exploration.
func BenchmarkExploreWithBFS(b *testing.B) {
	benchmarks := []struct {
		branches int
		depth    int
	}{
		{2, 3},
		{2, 5},
		{3, 4},
		{4, 3},
	}

	for _, bm := range benchmarks {
		name := fmt.Sprintf("branches=%d/depth=%d", bm.branches, bm.depth)
		b.Run(name, func(b *testing.B) {
			config := ToTConfig{
				MaxBranches:          bm.branches,
				MaxDepth:             bm.depth,
				ValueThreshold:       0.0,
				MaxNodes:             10000,
				EvaluateBeforeExpand: false,
			}

			evaluator := &MockEvaluator{
				ValueFunc: func(node *ThoughtNode) float64 { return 0.5 },
			}
			generator := &MockGenerator{}

			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tree := NewThoughtTree(config, evaluator, generator)
				_, _ = tree.ExploreWithBFS(ctx, "Benchmark problem")
			}
		})
	}
}

// BenchmarkExploreWithDFS benchmarks DFS exploration.
func BenchmarkExploreWithDFS(b *testing.B) {
	benchmarks := []struct {
		branches int
		depth    int
	}{
		{2, 3},
		{2, 5},
		{3, 4},
		{4, 3},
	}

	for _, bm := range benchmarks {
		name := fmt.Sprintf("branches=%d/depth=%d", bm.branches, bm.depth)
		b.Run(name, func(b *testing.B) {
			config := ToTConfig{
				MaxBranches:          bm.branches,
				MaxDepth:             bm.depth,
				ValueThreshold:       0.0,
				MaxNodes:             10000,
				EvaluateBeforeExpand: false,
			}

			evaluator := &MockEvaluator{
				ValueFunc: func(node *ThoughtNode) float64 { return 0.5 },
			}
			generator := &MockGenerator{}

			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tree := NewThoughtTree(config, evaluator, generator)
				_, _ = tree.ExploreWithDFS(ctx, "Benchmark problem")
			}
		})
	}
}

// BenchmarkExploreWithBestFirst benchmarks best-first exploration.
func BenchmarkExploreWithBestFirst(b *testing.B) {
	benchmarks := []struct {
		branches int
		depth    int
	}{
		{2, 3},
		{2, 5},
		{3, 4},
		{4, 3},
	}

	for _, bm := range benchmarks {
		name := fmt.Sprintf("branches=%d/depth=%d", bm.branches, bm.depth)
		b.Run(name, func(b *testing.B) {
			config := ToTConfig{
				MaxBranches:          bm.branches,
				MaxDepth:             bm.depth,
				ValueThreshold:       0.0,
				MaxNodes:             10000,
				EvaluateBeforeExpand: false,
			}

			evaluator := &MockEvaluator{
				ValueFunc: func(node *ThoughtNode) float64 { return 0.5 },
			}
			generator := &MockGenerator{}

			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tree := NewThoughtTree(config, evaluator, generator)
				_, _ = tree.ExploreWithBestFirst(ctx, "Benchmark problem")
			}
		})
	}
}

// BenchmarkExploreWithBeam benchmarks beam search exploration.
func BenchmarkExploreWithBeam(b *testing.B) {
	benchmarks := []struct {
		branches  int
		depth     int
		beamWidth int
	}{
		{3, 4, 2},
		{3, 4, 5},
		{5, 3, 3},
		{5, 5, 2},
	}

	for _, bm := range benchmarks {
		name := fmt.Sprintf("branches=%d/depth=%d/beam=%d", bm.branches, bm.depth, bm.beamWidth)
		b.Run(name, func(b *testing.B) {
			config := ToTConfig{
				MaxBranches:          bm.branches,
				MaxDepth:             bm.depth,
				ValueThreshold:       0.0,
				MaxNodes:             10000,
				EvaluateBeforeExpand: false,
			}

			evaluator := &MockEvaluator{
				ValueFunc: func(node *ThoughtNode) float64 { return 0.5 },
			}
			generator := &MockGenerator{}

			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tree := NewThoughtTree(config, evaluator, generator)
				_, _ = tree.ExploreWithBeam(ctx, "Benchmark problem", bm.beamWidth)
			}
		})
	}
}

// BenchmarkExploreWithMCTS benchmarks MCTS exploration.
func BenchmarkExploreWithMCTS(b *testing.B) {
	benchmarks := []struct {
		branches   int
		depth      int
		iterations int
	}{
		{2, 4, 10},
		{2, 4, 50},
		{3, 3, 20},
		{3, 5, 30},
	}

	for _, bm := range benchmarks {
		name := fmt.Sprintf("branches=%d/depth=%d/iter=%d", bm.branches, bm.depth, bm.iterations)
		b.Run(name, func(b *testing.B) {
			config := ToTConfig{
				MaxBranches:          bm.branches,
				MaxDepth:             bm.depth,
				ValueThreshold:       0.0,
				MaxNodes:             10000,
				EvaluateBeforeExpand: false,
			}

			evaluator := &MockEvaluator{
				ValueFunc: func(node *ThoughtNode) float64 { return 0.5 },
			}
			generator := &MockGenerator{}

			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tree := NewThoughtTree(config, evaluator, generator)
				_, _ = tree.ExploreWithMCTS(ctx, "Benchmark problem", bm.iterations)
			}
		})
	}
}

// BenchmarkStrategyComparison compares all strategies on same config.
func BenchmarkStrategyComparison(b *testing.B) {
	config := ToTConfig{
		MaxBranches:          3,
		MaxDepth:             4,
		ValueThreshold:       0.0,
		MaxNodes:             1000,
		EvaluateBeforeExpand: false,
	}

	evaluator := &MockEvaluator{
		ValueFunc: func(node *ThoughtNode) float64 { return 0.5 },
	}
	generator := &MockGenerator{}

	ctx := context.Background()

	b.Run("BFS", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tree := NewThoughtTree(config, evaluator, generator)
			_, _ = tree.ExploreWithBFS(ctx, "Problem")
		}
	})

	b.Run("DFS", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tree := NewThoughtTree(config, evaluator, generator)
			_, _ = tree.ExploreWithDFS(ctx, "Problem")
		}
	})

	b.Run("BestFirst", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tree := NewThoughtTree(config, evaluator, generator)
			_, _ = tree.ExploreWithBestFirst(ctx, "Problem")
		}
	})

	b.Run("Beam-3", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tree := NewThoughtTree(config, evaluator, generator)
			_, _ = tree.ExploreWithBeam(ctx, "Problem", 3)
		}
	})

	b.Run("MCTS-20", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tree := NewThoughtTree(config, evaluator, generator)
			_, _ = tree.ExploreWithMCTS(ctx, "Problem", 20)
		}
	})
}

// BenchmarkNodeCreation benchmarks node creation overhead.
func BenchmarkNodeCreation(b *testing.B) {
	parent := NewThoughtNode("parent", "Parent thought", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewThoughtNode("child", "Child thought", parent)
	}
}

// BenchmarkSelectTopK benchmarks top-k selection.
func BenchmarkSelectTopK(b *testing.B) {
	sizes := []int{10, 50, 100, 500}

	for _, size := range sizes {
		nodes := make([]*ThoughtNode, size)
		for i := 0; i < size; i++ {
			nodes[i] = &ThoughtNode{
				ID:            fmt.Sprintf("node-%d", i),
				ValueEstimate: float64(i) / float64(size),
			}
		}

		b.Run(fmt.Sprintf("size=%d/k=5", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = selectTopK(nodes, 5)
			}
		})

		b.Run(fmt.Sprintf("size=%d/k=10", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = selectTopK(nodes, 10)
			}
		})
	}
}

// BenchmarkUCB1Selection benchmarks UCB1 child selection.
func BenchmarkUCB1Selection(b *testing.B) {
	childCounts := []int{3, 5, 10, 20}

	for _, numChildren := range childCounts {
		parent := &ThoughtNode{ID: "parent"}
		children := make([]*ThoughtNode, numChildren)
		for i := 0; i < numChildren; i++ {
			children[i] = &ThoughtNode{
				ID:     fmt.Sprintf("child-%d", i),
				Parent: parent,
			}
		}
		parent.Children = children

		visits := make(map[string]int)
		totalValue := make(map[string]float64)
		visits[parent.ID] = 100

		for _, child := range children {
			visits[child.ID] = 10
			totalValue[child.ID] = 5.0
		}

		b.Run(fmt.Sprintf("children=%d", numChildren), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = selectUCB1(parent, visits, totalValue)
			}
		})
	}
}

// BenchmarkPathTraversal benchmarks path traversal.
func BenchmarkPathTraversal(b *testing.B) {
	depths := []int{5, 10, 20, 50}

	for _, depth := range depths {
		// Build a chain of nodes
		var current *ThoughtNode
		for i := 0; i < depth; i++ {
			node := NewThoughtNode(fmt.Sprintf("node-%d", i), "Thought", current)
			if current != nil {
				current.AddChild(node)
			}
			current = node
		}

		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = current.Path()
			}
		})

		b.Run(fmt.Sprintf("depth=%d/thoughts", depth), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = current.PathThoughts()
			}
		})
	}
}

// BenchmarkTreeMetrics benchmarks metrics collection.
func BenchmarkTreeMetrics(b *testing.B) {
	config := ToTConfig{
		MaxBranches:          3,
		MaxDepth:             4,
		ValueThreshold:       0.0,
		MaxNodes:             1000,
		EvaluateBeforeExpand: false,
	}

	evaluator := &MockEvaluator{
		ValueFunc: func(node *ThoughtNode) float64 { return 0.5 },
	}
	generator := &MockGenerator{}

	tree := NewThoughtTree(config, evaluator, generator)
	ctx := context.Background()
	_, _ = tree.ExploreWithBFS(ctx, "Problem")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tree.Metrics()
	}
}

// BenchmarkFindBestPath benchmarks finding the best path.
func BenchmarkFindBestPath(b *testing.B) {
	config := ToTConfig{
		MaxBranches:          3,
		MaxDepth:             4,
		ValueThreshold:       0.0,
		MaxNodes:             500,
		EvaluateBeforeExpand: false,
	}

	evaluator := &MockEvaluator{
		ValueFunc: func(node *ThoughtNode) float64 { return 0.5 },
	}
	generator := &MockGenerator{}

	tree := NewThoughtTree(config, evaluator, generator)
	ctx := context.Background()
	_, _ = tree.ExploreWithBFS(ctx, "Problem")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tree.FindBestPath()
	}
}

// BenchmarkAllTerminals benchmarks collecting all terminal nodes.
func BenchmarkAllTerminals(b *testing.B) {
	config := ToTConfig{
		MaxBranches:          2,
		MaxDepth:             5,
		ValueThreshold:       0.0,
		MaxNodes:             200,
		EvaluateBeforeExpand: false,
	}

	evaluator := &MockEvaluator{
		ValueFunc: func(node *ThoughtNode) float64 { return 0.5 },
		TerminalFunc: func(node *ThoughtNode) bool {
			return node.Depth == 4 // Many terminals at depth 4
		},
	}
	generator := &MockGenerator{}

	tree := NewThoughtTree(config, evaluator, generator)
	ctx := context.Background()
	_, _ = tree.ExploreWithBFS(ctx, "Problem")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tree.AllTerminals()
	}
}
