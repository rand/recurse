package lats

import (
	"context"
	"fmt"
	"math"
	"testing"

	"pgregory.net/rapid"
)

// TestProperty_UCB1BalancesExplorationExploitation verifies UCB1 balances exploration/exploitation.
func TestProperty_UCB1BalancesExplorationExploitation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate nodes with varying visits and values
		parentVisits := rapid.IntRange(10, 1000).Draw(t, "parentVisits")
		explorationConst := rapid.Float64Range(0.5, 2.0).Draw(t, "exploration")

		// Create two explored nodes
		visits1 := rapid.IntRange(1, parentVisits/2).Draw(t, "visits1")
		visits2 := rapid.IntRange(1, parentVisits/2).Draw(t, "visits2")
		value1 := rapid.Float64Range(0.0, 1.0).Draw(t, "value1")
		value2 := rapid.Float64Range(0.0, 1.0).Draw(t, "value2")

		node1 := &Node{Visits: visits1, TotalValue: value1 * float64(visits1)}
		node2 := &Node{Visits: visits2, TotalValue: value2 * float64(visits2)}

		uct1 := node1.UCTValue(parentVisits, explorationConst)
		uct2 := node2.UCTValue(parentVisits, explorationConst)

		// Invariant 1: UCT values should be finite for explored nodes
		if math.IsInf(uct1, 0) || math.IsNaN(uct1) {
			t.Fatalf("UCT1 should be finite, got %v", uct1)
		}
		if math.IsInf(uct2, 0) || math.IsNaN(uct2) {
			t.Fatalf("UCT2 should be finite, got %v", uct2)
		}

		// Invariant 2: UCT should be >= Q-value (exploration bonus is non-negative)
		if uct1 < node1.QValue()-0.001 {
			t.Fatalf("UCT1 (%v) should be >= QValue (%v)", uct1, node1.QValue())
		}
		if uct2 < node2.QValue()-0.001 {
			t.Fatalf("UCT2 (%v) should be >= QValue (%v)", uct2, node2.QValue())
		}

		// Invariant 3: Less visited nodes get higher exploration bonus
		if visits1 < visits2 && value1 == value2 {
			// Node1 should have higher exploration bonus
			q1, q2 := node1.QValue(), node2.QValue()
			explore1, explore2 := uct1-q1, uct2-q2
			if explore1 < explore2-0.001 {
				t.Fatalf("Less visited node should have higher exploration bonus: %v vs %v", explore1, explore2)
			}
		}
	})
}

// TestProperty_UnexploredNodesHaveInfiniteUCT verifies unexplored nodes have infinite UCT.
func TestProperty_UnexploredNodesHaveInfiniteUCT(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		parentVisits := rapid.IntRange(1, 10000).Draw(t, "parentVisits")
		exploration := rapid.Float64Range(0.1, 3.0).Draw(t, "exploration")

		unexplored := &Node{Visits: 0}
		uct := unexplored.UCTValue(parentVisits, exploration)

		if !math.IsInf(uct, 1) {
			t.Fatalf("Unexplored node should have +Inf UCT, got %v", uct)
		}
	})
}

// TestProperty_QValueIsAverage verifies Q-value is average of total value.
func TestProperty_QValueIsAverage(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		visits := rapid.IntRange(1, 10000).Draw(t, "visits")
		totalValue := rapid.Float64Range(0.0, float64(visits)).Draw(t, "totalValue")

		node := &Node{Visits: visits, TotalValue: totalValue}
		qValue := node.QValue()

		expected := totalValue / float64(visits)
		if math.Abs(qValue-expected) > 0.0001 {
			t.Fatalf("QValue (%v) should equal average (%v)", qValue, expected)
		}

		// Q-value should be in [0, 1] if total value is bounded
		if qValue < 0 || qValue > 1+0.001 {
			t.Fatalf("QValue (%v) should be in [0, 1] range", qValue)
		}
	})
}

// TestProperty_BackpropagatePreservesVisitCount verifies backpropagation increments visits.
func TestProperty_BackpropagatePreservesVisitCount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		depth := rapid.IntRange(1, 10).Draw(t, "depth")
		value := rapid.Float64Range(0.0, 1.0).Draw(t, "value")
		decay := rapid.Float64Range(0.8, 1.0).Draw(t, "decay")

		// Build a chain of nodes
		nodes := make([]*Node, depth+1)
		nodes[0] = &Node{ID: "root"}
		tree := NewTree(nodes[0])

		for i := 1; i <= depth; i++ {
			nodes[i] = &Node{
				ID:       fmt.Sprintf("node-%d", i),
				ParentID: nodes[i-1].ID,
			}
			tree.AddNode(nodes[i])
		}

		// Store initial visits
		initialVisits := make([]int, len(nodes))
		for i, n := range nodes {
			initialVisits[i] = n.Visits
		}

		// Backpropagate
		config := Config{ValueDecay: decay}
		ctrl := NewController(nil, nil, nil, nil, config)
		ctrl.backpropagate(tree, nodes[depth], value)

		// Invariant: All nodes on path should have visits incremented by 1
		for i, n := range nodes {
			if n.Visits != initialVisits[i]+1 {
				t.Fatalf("Node %d visits should be %d, got %d", i, initialVisits[i]+1, n.Visits)
			}
		}

		// Invariant: Value should decay as we go up (leaf has highest, root has lowest)
		// nodes[0] is root, nodes[depth] is leaf where backprop starts
		for i := 1; i < len(nodes); i++ {
			// Child (higher index) should have >= parent (lower index) value
			if nodes[i].TotalValue < nodes[i-1].TotalValue-0.001 {
				t.Fatalf("Value should decay going up: node %d (depth %d) has %v, parent has %v",
					i, i, nodes[i].TotalValue, nodes[i-1].TotalValue)
			}
		}
	})
}

// TestProperty_TreeStatsAreConsistent verifies tree statistics are consistent.
func TestProperty_TreeStatsAreConsistent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numNodes := rapid.IntRange(1, 100).Draw(t, "numNodes")
		maxChildren := rapid.IntRange(1, 5).Draw(t, "maxChildren")

		// Build tree with random structure
		root := &Node{ID: "root", Depth: 0}
		tree := NewTree(root)
		nodes := []*Node{root}

		for i := 1; i < numNodes; i++ {
			parentIdx := rapid.IntRange(0, len(nodes)-1).Draw(t, fmt.Sprintf("parent-%d", i))
			parent := nodes[parentIdx]

			if len(parent.Children) >= maxChildren {
				continue
			}

			child := &Node{
				ID:       fmt.Sprintf("node-%d", i),
				ParentID: parent.ID,
				Depth:    parent.Depth + 1,
			}
			parent.Children = append(parent.Children, child)
			tree.AddNode(child)
			nodes = append(nodes, child)
		}

		stats := tree.Stats()

		// Invariant: NodesCreated should equal tree size
		if stats.NodesCreated != tree.Size() {
			t.Fatalf("NodesCreated (%d) should equal tree size (%d)", stats.NodesCreated, tree.Size())
		}

		// Invariant: MaxDepth should be non-negative
		if stats.MaxDepth < 0 {
			t.Fatalf("MaxDepth should be non-negative, got %d", stats.MaxDepth)
		}

		// Invariant: AvgBranching should be non-negative
		if stats.AvgBranching < 0 {
			t.Fatalf("AvgBranching should be non-negative, got %f", stats.AvgBranching)
		}
	})
}

// TestProperty_BudgetDeductionIsMonotonic verifies budget only decreases.
func TestProperty_BudgetDeductionIsMonotonic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(100, 100000).Draw(t, "total")
		numDeductions := rapid.IntRange(1, 20).Draw(t, "numDeductions")

		budget := NewBudget(total)
		prevRemaining := budget.Remaining()

		for i := 0; i < numDeductions; i++ {
			deduction := rapid.IntRange(1, 100).Draw(t, fmt.Sprintf("deduction-%d", i))
			budget.Deduct(deduction)

			currentRemaining := budget.Remaining()

			// Invariant: Remaining should only decrease
			if currentRemaining > prevRemaining {
				t.Fatalf("Budget remaining increased: %d -> %d", prevRemaining, currentRemaining)
			}

			// Invariant: Used + Remaining = Total (unless overspent)
			if budget.Used()+budget.Remaining() != total {
				t.Fatalf("Budget accounting error: used(%d) + remaining(%d) != total(%d)",
					budget.Used(), budget.Remaining(), total)
			}

			prevRemaining = currentRemaining
		}
	})
}

// TestProperty_AgentStateCloneIsIndependent verifies clones are independent.
func TestProperty_AgentStateCloneIsIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		query := rapid.String().Draw(t, "query")
		context := rapid.String().Draw(t, "context")
		tokens := rapid.IntRange(0, 10000).Draw(t, "tokens")
		numObs := rapid.IntRange(0, 10).Draw(t, "numObs")

		original := &AgentState{
			Query:          query,
			CurrentContext: context,
			TokensUsed:     tokens,
			Observations:   make([]Observation, numObs),
		}

		for i := 0; i < numObs; i++ {
			original.Observations[i] = Observation{
				Action:  &Action{Tool: fmt.Sprintf("tool-%d", i)},
				Result:  fmt.Sprintf("result-%d", i),
				Success: i%2 == 0,
			}
		}

		clone := original.Clone()

		// Modify clone
		clone.Query = "modified"
		clone.TokensUsed = 999999
		if len(clone.Observations) > 0 {
			clone.Observations[0].Result = "modified"
		}
		clone.Observations = append(clone.Observations, Observation{})

		// Invariant: Original should be unchanged
		if original.Query != query {
			t.Fatalf("Original query was modified")
		}
		if original.TokensUsed != tokens {
			t.Fatalf("Original tokens was modified")
		}
		if len(original.Observations) != numObs {
			t.Fatalf("Original observations count changed: %d -> %d", numObs, len(original.Observations))
		}
	})
}

// TestProperty_SelectionPrefersBetterNodes verifies selection is non-random.
func TestProperty_SelectionPrefersBetterNodes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numRuns := rapid.IntRange(10, 50).Draw(t, "numRuns")
		exploration := rapid.Float64Range(0.5, 2.0).Draw(t, "exploration")

		config := Config{ExplorationConstant: exploration}
		ctrl := NewController(nil, nil, nil, nil, config)

		// Create tree with one very good node and others mediocre
		root := &Node{ID: "root", Visits: 100}
		goodNode := &Node{ID: "good", ParentID: "root", Visits: 20, TotalValue: 18} // Q=0.9
		badNode := &Node{ID: "bad", ParentID: "root", Visits: 20, TotalValue: 2}    // Q=0.1

		root.Children = []*Node{goodNode, badNode}
		tree := NewTree(root)
		tree.AddNode(goodNode)
		tree.AddNode(badNode)

		goodCount := 0
		for i := 0; i < numRuns; i++ {
			selected := ctrl.selectNode(tree, root)
			if selected.ID == "good" {
				goodCount++
			}
		}

		// Invariant: Good node should be selected more often (>70% of time)
		ratio := float64(goodCount) / float64(numRuns)
		if ratio < 0.7 {
			t.Fatalf("Good node should be selected more often, ratio: %v", ratio)
		}
	})
}

// TestProperty_HeuristicValuatorBoundsOutput verifies valuator output is bounded.
func TestProperty_HeuristicValuatorBoundsOutput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		successBonus := rapid.Float64Range(0.01, 0.5).Draw(t, "successBonus")
		failurePenalty := rapid.Float64Range(0.01, 0.5).Draw(t, "failurePenalty")
		depthPenalty := rapid.Float64Range(0.001, 0.1).Draw(t, "depthPenalty")
		depth := rapid.IntRange(0, 20).Draw(t, "depth")
		numObs := rapid.IntRange(0, 20).Draw(t, "numObs")

		valuator := &HeuristicValuator{
			SuccessBonus:   successBonus,
			FailurePenalty: failurePenalty,
			DepthPenalty:   depthPenalty,
		}

		obs := make([]Observation, numObs)
		for i := 0; i < numObs; i++ {
			obs[i] = Observation{Success: rapid.Bool().Draw(t, fmt.Sprintf("success-%d", i))}
		}

		node := &Node{
			Depth: depth,
			State: &AgentState{Observations: obs},
		}

		value, err := valuator.Value(context.Background(), node)

		if err != nil {
			t.Fatalf("Valuator returned error: %v", err)
		}

		// Invariant: Value should be in [0, 1]
		if value < 0 || value > 1 {
			t.Fatalf("Value should be in [0, 1], got %v", value)
		}
	})
}

// TestProperty_ToolRegistryIsThreadSafe verifies registry is safe for concurrent access.
func TestProperty_ToolRegistryIsThreadSafe(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numTools := rapid.IntRange(1, 20).Draw(t, "numTools")
		numReaders := rapid.IntRange(1, 10).Draw(t, "numReaders")

		registry := NewToolRegistry()

		// Register tools
		for i := 0; i < numTools; i++ {
			name := fmt.Sprintf("tool-%d", i)
			tool := NewMockTool(name, "desc", nil)
			registry.Register(tool)
		}

		// Concurrent reads
		done := make(chan bool, numReaders)
		for i := 0; i < numReaders; i++ {
			go func() {
				_ = registry.Names()
				_ = registry.Count()
				_ = registry.Describe()
				for j := 0; j < numTools; j++ {
					_ = registry.Has(fmt.Sprintf("tool-%d", j))
				}
				done <- true
			}()
		}

		// Wait for all readers
		for i := 0; i < numReaders; i++ {
			<-done
		}

		// Invariant: Count should still be correct
		if registry.Count() != numTools {
			t.Fatalf("Registry count should be %d, got %d", numTools, registry.Count())
		}
	})
}

// TestProperty_CapabilityMatcherFindsCorrectTools verifies capability matching works.
func TestProperty_CapabilityMatcherFindsCorrectTools(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numProfiles := rapid.IntRange(1, 10).Draw(t, "numProfiles")

		profiles := make(map[string]ToolProfile)
		allCaps := []ToolCapability{CapFileRead, CapFileWrite, CapSearch, CapCodeExecution, CapMemoryQuery}

		// Generate random profiles
		for i := 0; i < numProfiles; i++ {
			name := fmt.Sprintf("tool-%d", i)
			numCaps := rapid.IntRange(1, 3).Draw(t, fmt.Sprintf("numCaps-%d", i))
			caps := make([]ToolCapability, numCaps)
			for j := 0; j < numCaps; j++ {
				capIdx := rapid.IntRange(0, len(allCaps)-1).Draw(t, fmt.Sprintf("cap-%d-%d", i, j))
				caps[j] = allCaps[capIdx]
			}
			profiles[name] = ToolProfile{
				Name:         name,
				Capabilities: caps,
				CostEstimate: float64(rapid.IntRange(1, 100).Draw(t, fmt.Sprintf("cost-%d", i))),
			}
		}

		matcher := NewCapabilityMatcher(profiles)

		// Test each capability
		for _, cap := range allCaps {
			tools := matcher.FindByCapability(cap)

			// Invariant: All returned tools should have the capability
			for _, toolName := range tools {
				profile := profiles[toolName]
				if !profile.HasCapability(cap) {
					t.Fatalf("Tool %s returned for %s but doesn't have capability", toolName, cap)
				}
			}

			// Invariant: All tools with capability should be in result
			for name, profile := range profiles {
				if profile.HasCapability(cap) {
					found := false
					for _, n := range tools {
						if n == name {
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("Tool %s has %s but wasn't returned", name, cap)
					}
				}
			}
		}
	})
}
