// Package reasoning provides types and functions for capturing and managing
// reasoning traces with git integration, implementing SPEC.md section 5.
package reasoning

import (
	"encoding/json"
	"time"
)

// DecisionType represents the type of a decision node in a reasoning trace.
type DecisionType string

const (
	DecisionTypeGoal        DecisionType = "goal"        // High-level objective
	DecisionTypeDecision    DecisionType = "decision"    // Choice point
	DecisionTypeOption      DecisionType = "option"      // Approach considered
	DecisionTypeAction      DecisionType = "action"      // Implementation step
	DecisionTypeOutcome     DecisionType = "outcome"     // Result of actions
	DecisionTypeObservation DecisionType = "observation" // Discovery or insight
)

// DecisionStatus represents the status of a decision node.
type DecisionStatus string

const (
	StatusActive    DecisionStatus = "active"    // In progress
	StatusCompleted DecisionStatus = "completed" // Successfully completed
	StatusRejected  DecisionStatus = "rejected"  // Rejected/abandoned
)

// DiffRecord captures the changes made to a file during an action.
type DiffRecord struct {
	FilePath      string    `json:"file_path"`
	BeforeContent string    `json:"before_content,omitempty"` // Content before change (may be empty for new files)
	AfterContent  string    `json:"after_content,omitempty"`  // Content after change (may be empty for deleted files)
	UnifiedDiff   string    `json:"unified_diff"`             // Unified diff format
	Additions     int       `json:"additions"`                // Number of lines added
	Removals      int       `json:"removals"`                 // Number of lines removed
	CommitHash    string    `json:"commit_hash,omitempty"`    // Commit hash if committed
	CapturedAt    time.Time `json:"captured_at"`
}

// ActionRecord represents an implementation step in a reasoning trace.
type ActionRecord struct {
	ID            string         `json:"id"`
	Description   string         `json:"description"`
	StartTime     time.Time      `json:"start_time"`
	EndTime       *time.Time     `json:"end_time,omitempty"`
	Status        DecisionStatus `json:"status"`
	FilesAffected []string       `json:"files_affected,omitempty"`
	Diffs         []DiffRecord   `json:"diffs,omitempty"`
	NodeID        string         `json:"node_id,omitempty"` // Reference to hypergraph node
}

// RejectedOption represents an option that was considered but not chosen.
type RejectedOption struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Reason      string `json:"reason"` // Why it was rejected
	NodeID      string `json:"node_id,omitempty"`
}

// ReasoningTrace aggregates a complete reasoning chain from goal to outcome.
// Implements the structure defined in SPEC.md section 5.3.
type ReasoningTrace struct {
	ID              string           `json:"id"`
	GoalID          string           `json:"goal_id"`
	GoalDescription string           `json:"goal_description"`
	DecisionID      string           `json:"decision_id,omitempty"`
	ChosenOption    string           `json:"chosen_option,omitempty"`
	RejectedOptions []RejectedOption `json:"rejected_options,omitempty"`
	Actions         []ActionRecord   `json:"actions,omitempty"`
	Outcome         string           `json:"outcome,omitempty"`
	Branch          string           `json:"branch,omitempty"`
	CommitHash      string           `json:"commit_hash,omitempty"`
	Diffs           []DiffRecord     `json:"diffs,omitempty"` // Aggregated diffs from all actions
	StartTime       time.Time        `json:"start_time"`
	EndTime         *time.Time       `json:"end_time,omitempty"`
	Status          DecisionStatus   `json:"status"`
}

// DecisionNode represents the extended decision information stored in the decisions table.
type DecisionNode struct {
	NodeID       string         `json:"node_id"`
	DecisionType DecisionType   `json:"decision_type"`
	Confidence   int            `json:"confidence,omitempty"` // 0-100
	Prompt       string         `json:"prompt,omitempty"`     // User prompt that triggered this
	Files        []string       `json:"files,omitempty"`      // Associated files
	Branch       string         `json:"branch,omitempty"`
	CommitHash   string         `json:"commit_hash,omitempty"`
	ParentID     string         `json:"parent_id,omitempty"`
	Status       DecisionStatus `json:"status"`
}

// MarshalFiles converts the files slice to JSON for storage.
func (d *DecisionNode) MarshalFiles() (string, error) {
	if len(d.Files) == 0 {
		return "", nil
	}
	b, err := json.Marshal(d.Files)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// UnmarshalFiles parses the JSON files string into the Files slice.
func (d *DecisionNode) UnmarshalFiles(s string) error {
	if s == "" {
		d.Files = nil
		return nil
	}
	return json.Unmarshal([]byte(s), &d.Files)
}

// TraceMetadata holds additional context for a reasoning trace.
type TraceMetadata struct {
	ProjectPath string            `json:"project_path,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Custom      map[string]string `json:"custom,omitempty"`
}
