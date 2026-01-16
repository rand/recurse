package reasoning

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rand/recurse/internal/memory/hypergraph"
)

// TraceManager manages reasoning traces in the hypergraph memory.
type TraceManager struct {
	store *hypergraph.Store
	git   *GitIntegration
}

// NewTraceManager creates a new TraceManager.
func NewTraceManager(store *hypergraph.Store, workDir string) *TraceManager {
	return &TraceManager{
		store: store,
		git:   NewGitIntegration(workDir),
	}
}

// CreateGoal creates a goal node and returns its ID.
func (tm *TraceManager) CreateGoal(ctx context.Context, description string) (string, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeDecision, description)
	node.Subtype = string(DecisionTypeGoal)

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return "", fmt.Errorf("create goal node: %w", err)
	}

	// Create decision record
	if err := tm.createDecisionRecord(ctx, node.ID, DecisionTypeGoal, "", StatusActive); err != nil {
		return "", err
	}

	return node.ID, nil
}

// CreateDecision creates a decision node linked to a goal.
func (tm *TraceManager) CreateDecision(ctx context.Context, goalID, description string) (string, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeDecision, description)
	node.Subtype = string(DecisionTypeDecision)

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return "", fmt.Errorf("create decision node: %w", err)
	}

	// Create decision record with parent
	if err := tm.createDecisionRecord(ctx, node.ID, DecisionTypeDecision, goalID, StatusActive); err != nil {
		return "", err
	}

	// Link goal → decision with "spawns" hyperedge
	if _, err := tm.createReasoningEdge(ctx, hypergraph.HyperedgeSpawns, "spawns decision", goalID, node.ID); err != nil {
		return "", err
	}

	return node.ID, nil
}

// CreateOption creates an option node for a decision.
func (tm *TraceManager) CreateOption(ctx context.Context, decisionID, description string) (string, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeDecision, description)
	node.Subtype = string(DecisionTypeOption)

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return "", fmt.Errorf("create option node: %w", err)
	}

	// Create decision record
	if err := tm.createDecisionRecord(ctx, node.ID, DecisionTypeOption, decisionID, StatusActive); err != nil {
		return "", err
	}

	// Link decision → option with "considers" hyperedge
	if _, err := tm.createReasoningEdge(ctx, hypergraph.HyperedgeConsiders, "considers option", decisionID, node.ID); err != nil {
		return "", err
	}

	return node.ID, nil
}

// ChooseOption marks an option as chosen and creates the appropriate hyperedge.
func (tm *TraceManager) ChooseOption(ctx context.Context, decisionID, optionID string) error {
	// Update option status
	if err := tm.updateDecisionStatus(ctx, optionID, StatusCompleted); err != nil {
		return err
	}

	// Create "chooses" hyperedge
	if _, err := tm.createReasoningEdge(ctx, hypergraph.HyperedgeChooses, "chose option", decisionID, optionID); err != nil {
		return err
	}

	return nil
}

// RejectOption marks an option as rejected with a reason.
func (tm *TraceManager) RejectOption(ctx context.Context, decisionID, optionID, reason string) error {
	// Update option status
	if err := tm.updateDecisionStatus(ctx, optionID, StatusRejected); err != nil {
		return err
	}

	// Create "rejects" hyperedge with reason in metadata
	metadata, _ := json.Marshal(map[string]string{"reason": reason})
	edge := hypergraph.NewHyperedge(hypergraph.HyperedgeRejects, "rejected option: "+reason)
	edge.Metadata = metadata

	if err := tm.store.CreateHyperedge(ctx, edge); err != nil {
		return fmt.Errorf("create rejects edge: %w", err)
	}

	// Add members
	if err := tm.store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      decisionID,
		Role:        hypergraph.RoleSubject,
		Position:    0,
	}); err != nil {
		return err
	}

	if err := tm.store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      optionID,
		Role:        hypergraph.RoleObject,
		Position:    1,
	}); err != nil {
		return err
	}

	return nil
}

// CreateAction creates an action node linked to a decision.
func (tm *TraceManager) CreateAction(ctx context.Context, decisionID, description string, files []string) (string, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeDecision, description)
	node.Subtype = string(DecisionTypeAction)

	// Set provenance with git info
	gitInfo, _ := tm.git.GetCurrentState(ctx)
	prov := hypergraph.Provenance{Source: "agent"}
	if gitInfo != nil {
		prov.Branch = gitInfo.Branch
		prov.CommitHash = gitInfo.CommitHash
	}
	provJSON, _ := json.Marshal(prov)
	node.Provenance = provJSON

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return "", fmt.Errorf("create action node: %w", err)
	}

	// Create decision record with files
	dn := &DecisionNode{
		NodeID:       node.ID,
		DecisionType: DecisionTypeAction,
		ParentID:     decisionID,
		Status:       StatusActive,
		Files:        files,
	}
	if gitInfo != nil {
		dn.Branch = gitInfo.Branch
		dn.CommitHash = gitInfo.CommitHash
	}
	if err := tm.insertDecisionNode(ctx, dn); err != nil {
		return "", err
	}

	// Link decision → action with "implements" hyperedge
	if _, err := tm.createReasoningEdge(ctx, hypergraph.HyperedgeImplements, "implements via action", decisionID, node.ID); err != nil {
		return "", err
	}

	return node.ID, nil
}

// CompleteAction marks an action as completed and captures diffs.
func (tm *TraceManager) CompleteAction(ctx context.Context, actionID string, captureWorkingDiffs bool) (*ActionRecord, error) {
	// Get current node
	node, err := tm.store.GetNode(ctx, actionID)
	if err != nil {
		return nil, fmt.Errorf("get action node: %w", err)
	}

	now := time.Now().UTC()
	record := &ActionRecord{
		ID:          actionID,
		Description: node.Content,
		StartTime:   node.CreatedAt,
		EndTime:     &now,
		Status:      StatusCompleted,
		NodeID:      actionID,
	}

	// Capture diffs if requested
	if captureWorkingDiffs {
		diffs, err := tm.git.CaptureWorkingDiffs(ctx)
		if err == nil && len(diffs) > 0 {
			record.Diffs = diffs
			record.FilesAffected = make([]string, len(diffs))
			for i, d := range diffs {
				record.FilesAffected[i] = d.FilePath
			}

			// Store diffs as snippet nodes linked to action
			for _, diff := range diffs {
				if _, err := tm.storeDiffAsSnippet(ctx, actionID, diff); err != nil {
					// Log but don't fail - diff capture is best-effort
					continue
				}
			}
		}
	}

	// Update action status
	if err := tm.updateDecisionStatus(ctx, actionID, StatusCompleted); err != nil {
		return nil, err
	}

	return record, nil
}

// CreateOutcome creates an outcome node linked to an action with captured diffs.
func (tm *TraceManager) CreateOutcome(ctx context.Context, actionID, description string, diffs []DiffRecord) (string, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeDecision, description)
	node.Subtype = string(DecisionTypeOutcome)

	// Include summary of changes in metadata
	metadata := map[string]any{
		"files_changed": len(diffs),
		"total_additions": sumAdditions(diffs),
		"total_removals":  sumRemovals(diffs),
	}
	metaJSON, _ := json.Marshal(metadata)
	node.Metadata = metaJSON

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return "", fmt.Errorf("create outcome node: %w", err)
	}

	// Create decision record
	if err := tm.createDecisionRecord(ctx, node.ID, DecisionTypeOutcome, actionID, StatusCompleted); err != nil {
		return "", err
	}

	// Link action → outcome with "produces" hyperedge
	// Include diff summary in edge metadata
	edgeMeta, _ := json.Marshal(map[string]any{
		"diffs_count": len(diffs),
	})
	edge := hypergraph.NewHyperedge(hypergraph.HyperedgeProduces, "produces outcome")
	edge.Metadata = edgeMeta

	if err := tm.store.CreateHyperedge(ctx, edge); err != nil {
		return "", fmt.Errorf("create produces edge: %w", err)
	}

	if err := tm.store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      actionID,
		Role:        hypergraph.RoleSubject,
		Position:    0,
	}); err != nil {
		return "", err
	}

	if err := tm.store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      node.ID,
		Role:        hypergraph.RoleObject,
		Position:    1,
	}); err != nil {
		return "", err
	}

	// Store each diff as a snippet and link to outcome
	for i, diff := range diffs {
		snippetID, err := tm.storeDiffAsSnippet(ctx, actionID, diff)
		if err != nil {
			continue
		}
		// Also link snippet to outcome
		if err := tm.store.AddMember(ctx, hypergraph.Membership{
			HyperedgeID: edge.ID,
			NodeID:      snippetID,
			Role:        hypergraph.RoleContext,
			Position:    2 + i,
		}); err != nil {
			continue
		}
	}

	return node.ID, nil
}

// CreateObservation creates an observation node that can inform decisions.
func (tm *TraceManager) CreateObservation(ctx context.Context, description string) (string, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeDecision, description)
	node.Subtype = string(DecisionTypeObservation)

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return "", fmt.Errorf("create observation node: %w", err)
	}

	if err := tm.createDecisionRecord(ctx, node.ID, DecisionTypeObservation, "", StatusCompleted); err != nil {
		return "", err
	}

	return node.ID, nil
}

// LinkObservationToDecision links an observation to a decision it informs.
func (tm *TraceManager) LinkObservationToDecision(ctx context.Context, observationID, decisionID string) error {
	_, err := tm.createReasoningEdge(ctx, hypergraph.HyperedgeInforms, "informs decision", observationID, decisionID)
	return err
}

// GetReasoningTrace assembles a complete reasoning trace starting from a goal.
func (tm *TraceManager) GetReasoningTrace(ctx context.Context, goalID string) (*ReasoningTrace, error) {
	goalNode, err := tm.store.GetNode(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("get goal: %w", err)
	}

	trace := &ReasoningTrace{
		ID:              uuid.New().String(),
		GoalID:          goalID,
		GoalDescription: goalNode.Content,
		StartTime:       goalNode.CreatedAt,
		Status:          StatusActive,
	}

	// Get git info
	gitInfo, _ := tm.git.GetCurrentState(ctx)
	if gitInfo != nil {
		trace.Branch = gitInfo.Branch
		trace.CommitHash = gitInfo.CommitHash
	}

	// Find decisions spawned by this goal
	decisions, err := tm.getChildDecisions(ctx, goalID)
	if err != nil {
		return trace, nil // Return partial trace
	}

	if len(decisions) > 0 {
		trace.DecisionID = decisions[0].NodeID

		// Get options for the decision
		options, _ := tm.getChildDecisions(ctx, decisions[0].NodeID)
		for _, opt := range options {
			if opt.Status == StatusRejected {
				trace.RejectedOptions = append(trace.RejectedOptions, RejectedOption{
					ID:          opt.NodeID,
					Description: opt.Prompt,
					NodeID:      opt.NodeID,
				})
			} else if opt.Status == StatusCompleted {
				trace.ChosenOption = opt.Prompt
			}
		}

		// Get actions
		actions, _ := tm.getActionsForDecision(ctx, decisions[0].NodeID)
		trace.Actions = actions

		// Aggregate diffs from all actions
		for _, action := range actions {
			trace.Diffs = append(trace.Diffs, action.Diffs...)
		}
	}

	return trace, nil
}

// storeDiffAsSnippet stores a diff record as a snippet node and returns the node ID.
func (tm *TraceManager) storeDiffAsSnippet(ctx context.Context, actionID string, diff DiffRecord) (string, error) {
	node := hypergraph.NewNode(hypergraph.NodeTypeSnippet, diff.UnifiedDiff)
	node.Subtype = "diff"

	prov := hypergraph.Provenance{
		File:       diff.FilePath,
		CommitHash: diff.CommitHash,
		Source:     "git-diff",
	}
	provJSON, _ := json.Marshal(prov)
	node.Provenance = provJSON

	metadata := map[string]any{
		"additions":   diff.Additions,
		"removals":    diff.Removals,
		"captured_at": diff.CapturedAt,
	}
	metaJSON, _ := json.Marshal(metadata)
	node.Metadata = metaJSON

	if err := tm.store.CreateNode(ctx, node); err != nil {
		return "", fmt.Errorf("create diff snippet: %w", err)
	}

	// Link action → snippet with context edge
	edge := hypergraph.NewHyperedge(hypergraph.HyperedgeContext, "diff for action")
	if err := tm.store.CreateHyperedge(ctx, edge); err != nil {
		return node.ID, nil // Node created, edge failed - partial success
	}

	tm.store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      actionID,
		Role:        hypergraph.RoleSubject,
		Position:    0,
	})
	tm.store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      node.ID,
		Role:        hypergraph.RoleObject,
		Position:    1,
	})

	return node.ID, nil
}

// createReasoningEdge creates a hyperedge between two nodes for reasoning traces.
func (tm *TraceManager) createReasoningEdge(ctx context.Context, edgeType hypergraph.HyperedgeType, label, fromID, toID string) (*hypergraph.Hyperedge, error) {
	edge := hypergraph.NewHyperedge(edgeType, label)

	if err := tm.store.CreateHyperedge(ctx, edge); err != nil {
		return nil, fmt.Errorf("create %s edge: %w", edgeType, err)
	}

	if err := tm.store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      fromID,
		Role:        hypergraph.RoleSubject,
		Position:    0,
	}); err != nil {
		return nil, err
	}

	if err := tm.store.AddMember(ctx, hypergraph.Membership{
		HyperedgeID: edge.ID,
		NodeID:      toID,
		Role:        hypergraph.RoleObject,
		Position:    1,
	}); err != nil {
		return nil, err
	}

	return edge, nil
}

// createDecisionRecord creates a record in the decisions table.
func (tm *TraceManager) createDecisionRecord(ctx context.Context, nodeID string, decType DecisionType, parentID string, status DecisionStatus) error {
	dn := &DecisionNode{
		NodeID:       nodeID,
		DecisionType: decType,
		ParentID:     parentID,
		Status:       status,
	}

	gitInfo, _ := tm.git.GetCurrentState(ctx)
	if gitInfo != nil {
		dn.Branch = gitInfo.Branch
		dn.CommitHash = gitInfo.CommitHash
	}

	return tm.insertDecisionNode(ctx, dn)
}

// insertDecisionNode inserts a decision node record into the database.
func (tm *TraceManager) insertDecisionNode(ctx context.Context, dn *DecisionNode) error {
	filesJSON, _ := dn.MarshalFiles()

	db := tm.store.DB()
	_, err := db.ExecContext(ctx, `
		INSERT INTO decisions (node_id, decision_type, confidence, prompt, files, branch, commit_hash, parent_id, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, dn.NodeID, dn.DecisionType, dn.Confidence, dn.Prompt, filesJSON, dn.Branch, dn.CommitHash, nullString(dn.ParentID), dn.Status)
	if err != nil {
		return fmt.Errorf("insert decision: %w", err)
	}
	return nil
}

// updateDecisionStatus updates the status of a decision node.
func (tm *TraceManager) updateDecisionStatus(ctx context.Context, nodeID string, status DecisionStatus) error {
	db := tm.store.DB()
	_, err := db.ExecContext(ctx, `
		UPDATE decisions SET status = ? WHERE node_id = ?
	`, status, nodeID)
	if err != nil {
		return fmt.Errorf("update decision status: %w", err)
	}
	return nil
}

// getChildDecisions retrieves decision nodes that have the given parent.
func (tm *TraceManager) getChildDecisions(ctx context.Context, parentID string) ([]*DecisionNode, error) {
	db := tm.store.DB()
	rows, err := db.QueryContext(ctx, `
		SELECT node_id, decision_type, confidence, prompt, files, branch, commit_hash, parent_id, status
		FROM decisions WHERE parent_id = ?
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []*DecisionNode
	for rows.Next() {
		dn, err := scanDecisionNode(rows)
		if err != nil {
			continue
		}
		decisions = append(decisions, dn)
	}
	return decisions, rows.Err()
}

// getActionsForDecision retrieves action records for a decision.
func (tm *TraceManager) getActionsForDecision(ctx context.Context, decisionID string) ([]ActionRecord, error) {
	children, err := tm.getChildDecisions(ctx, decisionID)
	if err != nil {
		return nil, err
	}

	var actions []ActionRecord
	for _, child := range children {
		if child.DecisionType == DecisionTypeAction {
			node, err := tm.store.GetNode(ctx, child.NodeID)
			if err != nil {
				continue
			}

			action := ActionRecord{
				ID:            child.NodeID,
				Description:   node.Content,
				StartTime:     node.CreatedAt,
				Status:        child.Status,
				FilesAffected: child.Files,
				NodeID:        child.NodeID,
			}

			// Load associated diff snippets
			diffs, err := tm.getDiffsForAction(ctx, child.NodeID)
			if err == nil && len(diffs) > 0 {
				action.Diffs = diffs
			}

			actions = append(actions, action)
		}
	}
	return actions, nil
}

// getDiffsForAction retrieves diff records associated with an action node.
// Diffs are stored as snippet nodes linked via HyperedgeContext edges.
func (tm *TraceManager) getDiffsForAction(ctx context.Context, actionID string) ([]DiffRecord, error) {
	// Get all hyperedges connected to this action
	edges, err := tm.store.GetNodeHyperedges(ctx, actionID)
	if err != nil {
		return nil, err
	}

	var diffs []DiffRecord
	for _, edge := range edges {
		// Only look at context edges (used for action → diff snippet links)
		if edge.Type != hypergraph.HyperedgeContext {
			continue
		}

		// Get member nodes of this hyperedge
		members, err := tm.store.GetMembers(ctx, edge.ID)
		if err != nil {
			continue
		}

		// Find snippet nodes in this edge (object role)
		for _, member := range members {
			if member.Role != hypergraph.RoleObject {
				continue
			}

			// Get the snippet node
			node, err := tm.store.GetNode(ctx, member.NodeID)
			if err != nil {
				continue
			}

			// Only process diff snippets
			if node.Type != hypergraph.NodeTypeSnippet || node.Subtype != "diff" {
				continue
			}

			// Parse provenance for file path and commit hash
			diff := DiffRecord{
				UnifiedDiff: node.Content,
			}

			if len(node.Provenance) > 0 {
				var prov hypergraph.Provenance
				if err := json.Unmarshal(node.Provenance, &prov); err == nil {
					diff.FilePath = prov.File
					diff.CommitHash = prov.CommitHash
				}
			}

			// Parse metadata for additions/removals/captured_at
			if len(node.Metadata) > 0 {
				var meta map[string]any
				if err := json.Unmarshal(node.Metadata, &meta); err == nil {
					if additions, ok := meta["additions"].(float64); ok {
						diff.Additions = int(additions)
					}
					if removals, ok := meta["removals"].(float64); ok {
						diff.Removals = int(removals)
					}
					if capturedAt, ok := meta["captured_at"].(string); ok {
						if t, err := time.Parse(time.RFC3339Nano, capturedAt); err == nil {
							diff.CapturedAt = t
						}
					}
				}
			}

			diffs = append(diffs, diff)
		}
	}

	return diffs, nil
}

func scanDecisionNode(rows *sql.Rows) (*DecisionNode, error) {
	var dn DecisionNode
	var confidence sql.NullInt64
	var prompt, filesJSON, branch, commitHash, parentID sql.NullString

	err := rows.Scan(&dn.NodeID, &dn.DecisionType, &confidence, &prompt, &filesJSON, &branch, &commitHash, &parentID, &dn.Status)
	if err != nil {
		return nil, err
	}

	if confidence.Valid {
		dn.Confidence = int(confidence.Int64)
	}
	dn.Prompt = prompt.String
	dn.Branch = branch.String
	dn.CommitHash = commitHash.String
	dn.ParentID = parentID.String

	if filesJSON.Valid && filesJSON.String != "" {
		dn.UnmarshalFiles(filesJSON.String)
	}

	return &dn, nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func sumAdditions(diffs []DiffRecord) int {
	total := 0
	for _, d := range diffs {
		total += d.Additions
	}
	return total
}

func sumRemovals(diffs []DiffRecord) int {
	total := 0
	for _, d := range diffs {
		total += d.Removals
	}
	return total
}
