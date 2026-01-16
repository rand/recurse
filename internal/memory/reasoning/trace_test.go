package reasoning

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) *hypergraph.Store {
	store, err := hypergraph.NewStore(hypergraph.Options{}) // Empty Path = in-memory
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func setupTestGitRepo(t *testing.T) string {
	dir := t.TempDir()

	// Initialize git repo
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test User"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Skipf("git not available: %v", err)
		}
	}

	// Create initial commit
	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("initial content\n"), 0644)

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = dir
	cmd.Run()

	return dir
}

func TestTraceManager_CreateGoal(t *testing.T) {
	store := setupTestStore(t)
	tm := NewTraceManager(store, t.TempDir())
	ctx := context.Background()

	goalID, err := tm.CreateGoal(ctx, "Implement feature X")
	require.NoError(t, err)
	assert.NotEmpty(t, goalID)

	// Verify node was created
	node, err := store.GetNode(ctx, goalID)
	require.NoError(t, err)
	assert.Equal(t, hypergraph.NodeTypeDecision, node.Type)
	assert.Equal(t, "goal", node.Subtype)
	assert.Equal(t, "Implement feature X", node.Content)
}

func TestTraceManager_CreateDecision(t *testing.T) {
	store := setupTestStore(t)
	tm := NewTraceManager(store, t.TempDir())
	ctx := context.Background()

	goalID, err := tm.CreateGoal(ctx, "Implement feature X")
	require.NoError(t, err)

	decisionID, err := tm.CreateDecision(ctx, goalID, "Choose implementation approach")
	require.NoError(t, err)
	assert.NotEmpty(t, decisionID)

	// Verify node was created
	node, err := store.GetNode(ctx, decisionID)
	require.NoError(t, err)
	assert.Equal(t, "decision", node.Subtype)

	// Verify hyperedge was created (spawns)
	edges, err := store.GetNodeHyperedges(ctx, decisionID)
	require.NoError(t, err)
	assert.Len(t, edges, 1)
	assert.Equal(t, hypergraph.HyperedgeSpawns, edges[0].Type)
}

func TestTraceManager_CreateAndChooseOption(t *testing.T) {
	store := setupTestStore(t)
	tm := NewTraceManager(store, t.TempDir())
	ctx := context.Background()

	goalID, _ := tm.CreateGoal(ctx, "Goal")
	decisionID, _ := tm.CreateDecision(ctx, goalID, "Decision")

	opt1ID, err := tm.CreateOption(ctx, decisionID, "Option A")
	require.NoError(t, err)

	opt2ID, err := tm.CreateOption(ctx, decisionID, "Option B")
	require.NoError(t, err)

	// Choose option A
	err = tm.ChooseOption(ctx, decisionID, opt1ID)
	require.NoError(t, err)

	// Reject option B
	err = tm.RejectOption(ctx, decisionID, opt2ID, "Too complex")
	require.NoError(t, err)

	// Verify edges
	opt1Edges, _ := store.GetNodeHyperedges(ctx, opt1ID)
	hasChooses := false
	for _, e := range opt1Edges {
		if e.Type == hypergraph.HyperedgeChooses {
			hasChooses = true
		}
	}
	assert.True(t, hasChooses, "Option A should have 'chooses' edge")

	opt2Edges, _ := store.GetNodeHyperedges(ctx, opt2ID)
	hasRejects := false
	for _, e := range opt2Edges {
		if e.Type == hypergraph.HyperedgeRejects {
			hasRejects = true
		}
	}
	assert.True(t, hasRejects, "Option B should have 'rejects' edge")
}

func TestTraceManager_CreateAction(t *testing.T) {
	store := setupTestStore(t)
	tm := NewTraceManager(store, t.TempDir())
	ctx := context.Background()

	goalID, _ := tm.CreateGoal(ctx, "Goal")
	decisionID, _ := tm.CreateDecision(ctx, goalID, "Decision")

	files := []string{"main.go", "util.go"}
	actionID, err := tm.CreateAction(ctx, decisionID, "Implement function", files)
	require.NoError(t, err)
	assert.NotEmpty(t, actionID)

	// Verify node
	node, err := store.GetNode(ctx, actionID)
	require.NoError(t, err)
	assert.Equal(t, "action", node.Subtype)

	// Verify implements edge
	edges, _ := store.GetNodeHyperedges(ctx, actionID)
	hasImplements := false
	for _, e := range edges {
		if e.Type == hypergraph.HyperedgeImplements {
			hasImplements = true
		}
	}
	assert.True(t, hasImplements)
}

func TestTraceManager_CreateOutcome(t *testing.T) {
	store := setupTestStore(t)
	tm := NewTraceManager(store, t.TempDir())
	ctx := context.Background()

	goalID, _ := tm.CreateGoal(ctx, "Goal")
	decisionID, _ := tm.CreateDecision(ctx, goalID, "Decision")
	actionID, _ := tm.CreateAction(ctx, decisionID, "Action", nil)

	diffs := []DiffRecord{
		{
			FilePath:    "main.go",
			UnifiedDiff: "+added line",
			Additions:   1,
			Removals:    0,
		},
	}

	outcomeID, err := tm.CreateOutcome(ctx, actionID, "Feature implemented successfully", diffs)
	require.NoError(t, err)
	assert.NotEmpty(t, outcomeID)

	// Verify produces edge
	edges, _ := store.GetNodeHyperedges(ctx, outcomeID)
	hasProduces := false
	for _, e := range edges {
		if e.Type == hypergraph.HyperedgeProduces {
			hasProduces = true
		}
	}
	assert.True(t, hasProduces)
}

func TestTraceManager_GetReasoningTrace(t *testing.T) {
	store := setupTestStore(t)
	tm := NewTraceManager(store, t.TempDir())
	ctx := context.Background()

	// Build a complete trace
	goalID, _ := tm.CreateGoal(ctx, "Implement authentication")
	decisionID, _ := tm.CreateDecision(ctx, goalID, "Choose auth method")

	opt1ID, _ := tm.CreateOption(ctx, decisionID, "JWT tokens")
	opt2ID, _ := tm.CreateOption(ctx, decisionID, "Session cookies")

	tm.ChooseOption(ctx, decisionID, opt1ID)
	tm.RejectOption(ctx, decisionID, opt2ID, "Not suitable for API")

	actionID, _ := tm.CreateAction(ctx, decisionID, "Implement JWT middleware", []string{"auth.go"})
	tm.CompleteAction(ctx, actionID, false)

	// Get the trace
	trace, err := tm.GetReasoningTrace(ctx, goalID)
	require.NoError(t, err)

	assert.Equal(t, goalID, trace.GoalID)
	assert.Equal(t, "Implement authentication", trace.GoalDescription)
	assert.Equal(t, decisionID, trace.DecisionID)
	assert.Len(t, trace.RejectedOptions, 1)
	assert.Len(t, trace.Actions, 1)
}

func TestGitIntegration_GetCurrentState(t *testing.T) {
	dir := setupTestGitRepo(t)
	git := NewGitIntegration(dir)
	ctx := context.Background()

	info, err := git.GetCurrentState(ctx)
	require.NoError(t, err)

	assert.NotEmpty(t, info.CommitHash)
	assert.NotEmpty(t, info.Branch)
	assert.False(t, info.IsDirty)
}

func TestGitIntegration_CaptureFileDiff(t *testing.T) {
	dir := setupTestGitRepo(t)
	git := NewGitIntegration(dir)
	ctx := context.Background()

	// Modify the test file
	testFile := filepath.Join(dir, "test.txt")
	newContent := "initial content\nnew line\n"

	diff, err := git.CaptureFileDiff(ctx, testFile, newContent)
	require.NoError(t, err)

	assert.Equal(t, "test.txt", diff.FilePath)
	assert.Equal(t, 1, diff.Additions)
	assert.Equal(t, 0, diff.Removals)
	assert.Contains(t, diff.UnifiedDiff, "+new line")
}

func TestGitIntegration_CaptureWorkingDiffs(t *testing.T) {
	dir := setupTestGitRepo(t)
	git := NewGitIntegration(dir)
	ctx := context.Background()

	// Modify the test file
	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("modified content\n"), 0644)

	diffs, err := git.CaptureWorkingDiffs(ctx)
	require.NoError(t, err)

	assert.Len(t, diffs, 1)
	assert.Equal(t, "test.txt", diffs[0].FilePath)
}

func TestDiffRecord_Fields(t *testing.T) {
	diff := DiffRecord{
		FilePath:      "src/main.go",
		BeforeContent: "old",
		AfterContent:  "new",
		UnifiedDiff:   "-old\n+new",
		Additions:     1,
		Removals:      1,
	}

	assert.Equal(t, "src/main.go", diff.FilePath)
	assert.Equal(t, 1, diff.Additions)
	assert.Equal(t, 1, diff.Removals)
}

func TestDecisionNode_MarshalFiles(t *testing.T) {
	dn := &DecisionNode{
		Files: []string{"a.go", "b.go"},
	}

	json, err := dn.MarshalFiles()
	require.NoError(t, err)
	assert.Equal(t, `["a.go","b.go"]`, json)

	dn2 := &DecisionNode{}
	err = dn2.UnmarshalFiles(json)
	require.NoError(t, err)
	assert.Equal(t, []string{"a.go", "b.go"}, dn2.Files)
}

func TestTraceManager_DiffCaptureIntegration(t *testing.T) {
	// Integration test: captures diffs from real git operations and stores in hypergraph
	dir := setupTestGitRepo(t)
	store := setupTestStore(t)
	tm := NewTraceManager(store, dir)
	ctx := context.Background()

	// Build a complete trace with an action
	goalID, err := tm.CreateGoal(ctx, "Implement feature")
	require.NoError(t, err)

	decisionID, err := tm.CreateDecision(ctx, goalID, "Choose approach")
	require.NoError(t, err)

	// Create action
	actionID, err := tm.CreateAction(ctx, decisionID, "Modify files", []string{"test.txt"})
	require.NoError(t, err)

	// Modify the test file to create a working diff
	testFile := filepath.Join(dir, "test.txt")
	err = os.WriteFile(testFile, []byte("initial content\nnew line added\n"), 0644)
	require.NoError(t, err)

	// Complete action with diff capture
	record, err := tm.CompleteAction(ctx, actionID, true)
	require.NoError(t, err)

	// Verify diffs were captured in the action record
	assert.Len(t, record.Diffs, 1, "should capture one diff")
	assert.Equal(t, "test.txt", record.Diffs[0].FilePath)
	assert.Equal(t, 1, record.Diffs[0].Additions)
	assert.Contains(t, record.Diffs[0].UnifiedDiff, "+new line added")

	// Verify diffs are stored and retrievable via GetReasoningTrace
	trace, err := tm.GetReasoningTrace(ctx, goalID)
	require.NoError(t, err)

	// The trace should have actions with diffs
	require.Len(t, trace.Actions, 1, "trace should have one action")
	assert.Len(t, trace.Actions[0].Diffs, 1, "action should have one diff")
	assert.Equal(t, "test.txt", trace.Actions[0].Diffs[0].FilePath)
}

func TestTraceManager_CreateOutcomeWithDiffs(t *testing.T) {
	store := setupTestStore(t)
	tm := NewTraceManager(store, t.TempDir())
	ctx := context.Background()

	// Build trace
	goalID, _ := tm.CreateGoal(ctx, "Goal")
	decisionID, _ := tm.CreateDecision(ctx, goalID, "Decision")
	actionID, _ := tm.CreateAction(ctx, decisionID, "Action", nil)

	// Create outcome with diffs
	diffs := []DiffRecord{
		{
			FilePath:    "main.go",
			UnifiedDiff: "+func main() {\n+    println(\"hello\")\n+}",
			Additions:   3,
			Removals:    0,
		},
		{
			FilePath:    "util.go",
			UnifiedDiff: "-old line\n+new line",
			Additions:   1,
			Removals:    1,
		},
	}

	outcomeID, err := tm.CreateOutcome(ctx, actionID, "Feature complete", diffs)
	require.NoError(t, err)
	assert.NotEmpty(t, outcomeID)

	// Verify outcome node has metadata with diff summary
	node, err := store.GetNode(ctx, outcomeID)
	require.NoError(t, err)

	var meta map[string]any
	err = json.Unmarshal(node.Metadata, &meta)
	require.NoError(t, err)

	assert.Equal(t, float64(2), meta["files_changed"])
	assert.Equal(t, float64(4), meta["total_additions"]) // 3 + 1
	assert.Equal(t, float64(1), meta["total_removals"])  // 0 + 1
}

func TestTraceManager_GetDiffsForAction(t *testing.T) {
	store := setupTestStore(t)
	tm := NewTraceManager(store, t.TempDir())
	ctx := context.Background()

	// Build trace
	goalID, _ := tm.CreateGoal(ctx, "Goal")
	decisionID, _ := tm.CreateDecision(ctx, goalID, "Decision")
	actionID, _ := tm.CreateAction(ctx, decisionID, "Action", nil)

	// Manually store a diff snippet (simulating what CompleteAction does)
	diff := DiffRecord{
		FilePath:    "test.go",
		UnifiedDiff: "-old\n+new",
		Additions:   1,
		Removals:    1,
		CommitHash:  "abc123",
	}
	snippetID, err := tm.storeDiffAsSnippet(ctx, actionID, diff)
	require.NoError(t, err)
	assert.NotEmpty(t, snippetID)

	// Retrieve diffs for the action
	diffs, err := tm.getDiffsForAction(ctx, actionID)
	require.NoError(t, err)

	assert.Len(t, diffs, 1)
	assert.Equal(t, "test.go", diffs[0].FilePath)
	assert.Equal(t, "abc123", diffs[0].CommitHash)
	assert.Equal(t, 1, diffs[0].Additions)
	assert.Equal(t, 1, diffs[0].Removals)
	assert.Contains(t, diffs[0].UnifiedDiff, "-old")
	assert.Contains(t, diffs[0].UnifiedDiff, "+new")
}
