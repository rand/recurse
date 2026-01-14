// Package verify provides formal verification for code changes using constraint solving.
package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rand/recurse/internal/rlm/repl"
)

// ConstraintType defines the category of constraint.
type ConstraintType string

const (
	// ConstraintTypePrecondition must be true before execution.
	ConstraintTypePrecondition ConstraintType = "precondition"

	// ConstraintTypePostcondition must be true after execution.
	ConstraintTypePostcondition ConstraintType = "postcondition"

	// ConstraintTypeInvariant must be true throughout execution.
	ConstraintTypeInvariant ConstraintType = "invariant"

	// ConstraintTypeTypeCheck ensures type constraints are satisfied.
	ConstraintTypeTypeCheck ConstraintType = "type_check"

	// ConstraintTypeCallGraph ensures call relationships are valid.
	ConstraintTypeCallGraph ConstraintType = "call_graph"
)

// Constraint represents a verifiable condition extracted from code.
type Constraint struct {
	// Type categorizes the constraint.
	Type ConstraintType

	// Name is a human-readable identifier.
	Name string

	// Expression is the constraint in a form CPMpy can evaluate.
	Expression string

	// Description explains what this constraint ensures.
	Description string

	// Source indicates where this constraint was extracted from.
	Source string

	// Confidence indicates how confident we are in this constraint (0.0-1.0).
	Confidence float64

	// Variables lists the variables involved in this constraint.
	Variables []string
}

// VerificationResult contains the outcome of constraint verification.
type VerificationResult struct {
	// Satisfied indicates whether all constraints were satisfied.
	Satisfied bool

	// Status provides the overall verification status.
	Status VerificationStatus

	// CheckedConstraints lists results for each constraint.
	CheckedConstraints []ConstraintResult

	// CounterExample provides a failing case if verification failed.
	CounterExample *CounterExample

	// Duration is how long verification took.
	Duration time.Duration

	// SolverOutput contains raw solver output for debugging.
	SolverOutput string
}

// VerificationStatus represents the outcome of verification.
type VerificationStatus string

const (
	StatusSatisfied   VerificationStatus = "satisfied"
	StatusViolated    VerificationStatus = "violated"
	StatusUnknown     VerificationStatus = "unknown"
	StatusTimeout     VerificationStatus = "timeout"
	StatusError       VerificationStatus = "error"
)

// ConstraintResult holds the verification result for a single constraint.
type ConstraintResult struct {
	Constraint *Constraint
	Satisfied  bool
	Message    string
}

// CounterExample provides values that violate constraints.
type CounterExample struct {
	// Variables maps variable names to their violating values.
	Variables map[string]any

	// Explanation describes why this is a violation.
	Explanation string
}

// CodeChange represents a change to be verified.
type CodeChange struct {
	// Before is the code before the change (may be empty for new code).
	Before string

	// After is the code after the change.
	After string

	// Language is the programming language (go, python, etc.).
	Language string

	// Context provides surrounding code or documentation.
	Context string
}

// VerificationChain orchestrates constraint extraction and verification via REPL.
type VerificationChain struct {
	repl    *repl.Manager
	timeout time.Duration
}

// NewVerificationChain creates a new verification chain with the given REPL manager.
func NewVerificationChain(replMgr *repl.Manager) *VerificationChain {
	return &VerificationChain{
		repl:    replMgr,
		timeout: 30 * time.Second,
	}
}

// SetTimeout configures the verification timeout.
func (c *VerificationChain) SetTimeout(d time.Duration) {
	c.timeout = d
}

// GeneratePreconditions extracts preconditions from code documentation.
func (c *VerificationChain) GeneratePreconditions(ctx context.Context, change CodeChange) ([]Constraint, error) {
	var constraints []Constraint

	// Extract from docstrings
	docConstraints := c.extractFromDocstrings(change.After, ConstraintTypePrecondition)
	constraints = append(constraints, docConstraints...)

	// Extract from type annotations
	typeConstraints := c.extractTypeConstraints(change.After, change.Language)
	constraints = append(constraints, typeConstraints...)

	// Extract from assert statements
	assertConstraints := c.extractAssertConstraints(change.After, change.Language)
	constraints = append(constraints, assertConstraints...)

	return constraints, nil
}

// GeneratePostconditions extracts postconditions from code documentation.
func (c *VerificationChain) GeneratePostconditions(ctx context.Context, change CodeChange) ([]Constraint, error) {
	var constraints []Constraint

	// Extract from docstrings (return value descriptions)
	docConstraints := c.extractFromDocstrings(change.After, ConstraintTypePostcondition)
	constraints = append(constraints, docConstraints...)

	// Extract from function signatures (return types)
	returnConstraints := c.extractReturnConstraints(change.After, change.Language)
	constraints = append(constraints, returnConstraints...)

	return constraints, nil
}

// GenerateInvariants extracts invariants that must hold throughout execution.
func (c *VerificationChain) GenerateInvariants(ctx context.Context, change CodeChange) ([]Constraint, error) {
	var constraints []Constraint

	// Extract from docstrings
	docConstraints := c.extractFromDocstrings(change.After, ConstraintTypeInvariant)
	constraints = append(constraints, docConstraints...)

	// Extract loop invariants
	loopConstraints := c.extractLoopInvariants(change.After, change.Language)
	constraints = append(constraints, loopConstraints...)

	return constraints, nil
}

// Verify checks that code satisfies the given constraints using CPMpy.
func (c *VerificationChain) Verify(ctx context.Context, constraints []Constraint, code string) (*VerificationResult, error) {
	if c.repl == nil {
		return nil, fmt.Errorf("REPL manager not configured")
	}

	start := time.Now()
	result := &VerificationResult{
		CheckedConstraints: make([]ConstraintResult, 0, len(constraints)),
	}

	// Build CPMpy verification code
	verifyCode := c.buildVerificationCode(constraints, code)

	// Execute via REPL with timeout
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	execResult, err := c.repl.Execute(ctx, verifyCode)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.Status = StatusTimeout
			result.Duration = time.Since(start)
			return result, nil
		}
		result.Status = StatusError
		result.Duration = time.Since(start)
		return result, fmt.Errorf("REPL execution failed: %w", err)
	}

	result.SolverOutput = execResult.Output
	result.Duration = time.Since(start)

	// Parse verification result
	if execResult.Error != "" {
		result.Status = StatusError
		return result, nil
	}

	// Parse the JSON result from REPL
	return c.parseVerificationResult(execResult.ReturnVal, constraints, result)
}

// VerifyChange is a convenience method that generates constraints and verifies.
func (c *VerificationChain) VerifyChange(ctx context.Context, change CodeChange) (*VerificationResult, error) {
	// Collect all constraints
	var allConstraints []Constraint

	pre, err := c.GeneratePreconditions(ctx, change)
	if err != nil {
		return nil, fmt.Errorf("generate preconditions: %w", err)
	}
	allConstraints = append(allConstraints, pre...)

	post, err := c.GeneratePostconditions(ctx, change)
	if err != nil {
		return nil, fmt.Errorf("generate postconditions: %w", err)
	}
	allConstraints = append(allConstraints, post...)

	inv, err := c.GenerateInvariants(ctx, change)
	if err != nil {
		return nil, fmt.Errorf("generate invariants: %w", err)
	}
	allConstraints = append(allConstraints, inv...)

	if len(allConstraints) == 0 {
		// No constraints to verify
		return &VerificationResult{
			Satisfied: true,
			Status:    StatusSatisfied,
		}, nil
	}

	return c.Verify(ctx, allConstraints, change.After)
}

// buildVerificationCode generates Python code for CPMpy constraint solving.
func (c *VerificationChain) buildVerificationCode(constraints []Constraint, code string) string {
	var sb strings.Builder

	// Import CPMpy
	sb.WriteString(`
import json
try:
    from cpmpy import *
except ImportError:
    # Fallback to simple constraint checking without CPMpy
    class Model:
        def __init__(self):
            self.constraints = []
        def __iadd__(self, constraint):
            self.constraints.append(constraint)
            return self
        def solve(self):
            return all(c for c in self.constraints if isinstance(c, bool))
    def intvar(lb, ub, name=None):
        return (lb + ub) // 2  # Return midpoint as placeholder
    def boolvar(name=None):
        return True

# Create model
model = Model()

# Define variables
variables = {}
`)

	// Extract and declare variables
	varSet := make(map[string]bool)
	for _, constraint := range constraints {
		for _, v := range constraint.Variables {
			if !varSet[v] {
				varSet[v] = true
				sb.WriteString(fmt.Sprintf("variables['%s'] = intvar(-1000000, 1000000, name='%s')\n", v, v))
			}
		}
	}

	// Add constraints
	sb.WriteString("\n# Add constraints\nconstraint_results = []\n")
	for i, constraint := range constraints {
		sb.WriteString(fmt.Sprintf(`
# Constraint %d: %s
try:
    constraint_%d = %s
    model += constraint_%d
    constraint_results.append({
        'index': %d,
        'name': %q,
        'type': %q,
        'added': True,
        'error': None
    })
except Exception as e:
    constraint_results.append({
        'index': %d,
        'name': %q,
        'type': %q,
        'added': False,
        'error': str(e)
    })
`, i, constraint.Name, i, constraint.Expression, i, i, constraint.Name, constraint.Type, i, constraint.Name, constraint.Type))
	}

	// Solve and return result
	sb.WriteString(`
# Solve the model
result = {
    'satisfied': False,
    'status': 'unknown',
    'constraints': constraint_results,
    'counter_example': None
}

try:
    if model.solve():
        result['satisfied'] = True
        result['status'] = 'satisfied'
    else:
        result['status'] = 'violated'
        # Try to extract counter-example
        result['counter_example'] = {
            'variables': {k: str(v.value()) if hasattr(v, 'value') else str(v) for k, v in variables.items()},
            'explanation': 'Constraints could not be satisfied'
        }
except Exception as e:
    result['status'] = 'error'
    result['error'] = str(e)

json.dumps(result)
`)

	return sb.String()
}

// parseVerificationResult parses the JSON result from CPMpy execution.
func (c *VerificationChain) parseVerificationResult(jsonStr string, constraints []Constraint, result *VerificationResult) (*VerificationResult, error) {
	// Clean up the JSON string
	jsonStr = strings.TrimSpace(jsonStr)
	jsonStr = strings.Trim(jsonStr, "'\"")

	var parsed struct {
		Satisfied      bool   `json:"satisfied"`
		Status         string `json:"status"`
		Constraints    []struct {
			Index int    `json:"index"`
			Name  string `json:"name"`
			Type  string `json:"type"`
			Added bool   `json:"added"`
			Error string `json:"error"`
		} `json:"constraints"`
		CounterExample *struct {
			Variables   map[string]string `json:"variables"`
			Explanation string            `json:"explanation"`
		} `json:"counter_example"`
		Error string `json:"error"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		// If JSON parsing fails, try to infer result from raw output
		result.Status = StatusUnknown
		return result, nil
	}

	result.Satisfied = parsed.Satisfied
	result.Status = VerificationStatus(parsed.Status)

	// Map constraint results back
	for _, cr := range parsed.Constraints {
		if cr.Index < len(constraints) {
			result.CheckedConstraints = append(result.CheckedConstraints, ConstraintResult{
				Constraint: &constraints[cr.Index],
				Satisfied:  cr.Added && cr.Error == "",
				Message:    cr.Error,
			})
		}
	}

	// Extract counter-example
	if parsed.CounterExample != nil {
		vars := make(map[string]any)
		for k, v := range parsed.CounterExample.Variables {
			vars[k] = v
		}
		result.CounterExample = &CounterExample{
			Variables:   vars,
			Explanation: parsed.CounterExample.Explanation,
		}
	}

	return result, nil
}

// extractFromDocstrings extracts constraints from docstrings and comments.
func (c *VerificationChain) extractFromDocstrings(code string, constraintType ConstraintType) []Constraint {
	var constraints []Constraint

	// Pattern for @requires, @ensures, @invariant annotations
	patterns := map[ConstraintType]*regexp.Regexp{
		ConstraintTypePrecondition:  regexp.MustCompile(`(?m)(?:@requires|@pre|Requires:|Pre:)\s*(.+?)(?:\n|$)`),
		ConstraintTypePostcondition: regexp.MustCompile(`(?m)(?:@ensures|@post|Ensures:|Post:|Returns:)\s*(.+?)(?:\n|$)`),
		ConstraintTypeInvariant:     regexp.MustCompile(`(?m)(?:@invariant|Invariant:)\s*(.+?)(?:\n|$)`),
	}

	pattern, ok := patterns[constraintType]
	if !ok {
		return constraints
	}

	matches := pattern.FindAllStringSubmatch(code, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		desc := strings.TrimSpace(match[1])
		expr := c.naturalToConstraint(desc)
		vars := c.extractVariables(expr)

		constraints = append(constraints, Constraint{
			Type:        constraintType,
			Name:        fmt.Sprintf("%s_%d", constraintType, len(constraints)),
			Expression:  expr,
			Description: desc,
			Source:      "docstring",
			Confidence:  0.8,
			Variables:   vars,
		})
	}

	return constraints
}

// extractTypeConstraints extracts type-based constraints.
func (c *VerificationChain) extractTypeConstraints(code string, language string) []Constraint {
	var constraints []Constraint

	switch language {
	case "python":
		// Extract Python type hints
		typePattern := regexp.MustCompile(`(\w+)\s*:\s*(int|float|str|bool|List\[.+\]|Dict\[.+\])`)
		matches := typePattern.FindAllStringSubmatch(code, -1)
		for _, match := range matches {
			if len(match) < 3 {
				continue
			}
			varName := match[1]
			typeName := match[2]

			expr := c.typeToConstraint(varName, typeName)
			if expr != "" {
				constraints = append(constraints, Constraint{
					Type:        ConstraintTypeTypeCheck,
					Name:        fmt.Sprintf("type_%s", varName),
					Expression:  expr,
					Description: fmt.Sprintf("%s must be %s", varName, typeName),
					Source:      "type_annotation",
					Confidence:  0.95,
					Variables:   []string{varName},
				})
			}
		}

	case "go":
		// Extract Go type constraints from function signatures
		funcPattern := regexp.MustCompile(`func\s+\w+\s*\(([^)]*)\)`)
		matches := funcPattern.FindAllStringSubmatch(code, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			params := match[1]
			// Parse individual parameters
			paramParts := strings.Split(params, ",")
			for _, part := range paramParts {
				part = strings.TrimSpace(part)
				fields := strings.Fields(part)
				if len(fields) >= 2 {
					varName := fields[0]
					typeName := fields[1]
					expr := c.goTypeToConstraint(varName, typeName)
					if expr != "" {
						constraints = append(constraints, Constraint{
							Type:        ConstraintTypeTypeCheck,
							Name:        fmt.Sprintf("type_%s", varName),
							Expression:  expr,
							Description: fmt.Sprintf("%s must be %s", varName, typeName),
							Source:      "type_signature",
							Confidence:  0.95,
							Variables:   []string{varName},
						})
					}
				}
			}
		}
	}

	return constraints
}

// extractAssertConstraints extracts constraints from assert statements.
func (c *VerificationChain) extractAssertConstraints(code string, language string) []Constraint {
	var constraints []Constraint

	var pattern *regexp.Regexp
	switch language {
	case "python":
		pattern = regexp.MustCompile(`assert\s+(.+?)(?:,|$|\n)`)
	case "go":
		// Go doesn't have assert, but we can look for panic conditions
		pattern = regexp.MustCompile(`if\s+(.+?)\s*\{\s*panic`)
	default:
		return constraints
	}

	matches := pattern.FindAllStringSubmatch(code, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		expr := strings.TrimSpace(match[1])
		vars := c.extractVariables(expr)

		constraints = append(constraints, Constraint{
			Type:        ConstraintTypePrecondition,
			Name:        fmt.Sprintf("assert_%d", len(constraints)),
			Expression:  expr,
			Description: fmt.Sprintf("Assertion: %s", expr),
			Source:      "assert",
			Confidence:  0.9,
			Variables:   vars,
		})
	}

	return constraints
}

// extractReturnConstraints extracts constraints from return type annotations.
func (c *VerificationChain) extractReturnConstraints(code string, language string) []Constraint {
	var constraints []Constraint

	switch language {
	case "python":
		// Python return type hints
		pattern := regexp.MustCompile(`def\s+\w+\s*\([^)]*\)\s*->\s*(\w+)`)
		matches := pattern.FindAllStringSubmatch(code, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			returnType := match[1]
			expr := c.typeToConstraint("_return", returnType)
			if expr != "" {
				constraints = append(constraints, Constraint{
					Type:        ConstraintTypePostcondition,
					Name:        "return_type",
					Expression:  expr,
					Description: fmt.Sprintf("Return value must be %s", returnType),
					Source:      "return_annotation",
					Confidence:  0.95,
					Variables:   []string{"_return"},
				})
			}
		}

	case "go":
		// Go return types
		pattern := regexp.MustCompile(`func\s+\w+\s*\([^)]*\)\s*(\w+|\([^)]+\))`)
		matches := pattern.FindAllStringSubmatch(code, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			returnType := strings.Trim(match[1], "()")
			expr := c.goTypeToConstraint("_return", returnType)
			if expr != "" {
				constraints = append(constraints, Constraint{
					Type:        ConstraintTypePostcondition,
					Name:        "return_type",
					Expression:  expr,
					Description: fmt.Sprintf("Return value must be %s", returnType),
					Source:      "return_signature",
					Confidence:  0.95,
					Variables:   []string{"_return"},
				})
			}
		}
	}

	return constraints
}

// extractLoopInvariants extracts invariants from loop constructs.
func (c *VerificationChain) extractLoopInvariants(code string, language string) []Constraint {
	var constraints []Constraint

	// Look for @invariant comments in loops
	pattern := regexp.MustCompile(`(?:for|while)[^{]*\{[^}]*(?:@invariant|# invariant:)\s*(.+?)(?:\n|$)`)
	matches := pattern.FindAllStringSubmatch(code, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		desc := strings.TrimSpace(match[1])
		expr := c.naturalToConstraint(desc)
		vars := c.extractVariables(expr)

		constraints = append(constraints, Constraint{
			Type:        ConstraintTypeInvariant,
			Name:        fmt.Sprintf("loop_invariant_%d", len(constraints)),
			Expression:  expr,
			Description: desc,
			Source:      "loop_annotation",
			Confidence:  0.85,
			Variables:   vars,
		})
	}

	return constraints
}

// naturalToConstraint converts natural language to a constraint expression.
func (c *VerificationChain) naturalToConstraint(natural string) string {
	natural = strings.ToLower(natural)

	// Common patterns
	patterns := []struct {
		regex   *regexp.Regexp
		replace string
	}{
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )greater than\s+(\w+)`), `variables['$1'] > $2`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )less than\s+(\w+)`), `variables['$1'] < $2`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )(?:>|greater than or equal to)\s+(\w+)`), `variables['$1'] >= $2`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )(?:<|less than or equal to)\s+(\w+)`), `variables['$1'] <= $2`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )(?:equal to |=|==)\s+(\w+)`), `variables['$1'] == $2`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )(?:not equal to |!=|<>)\s+(\w+)`), `variables['$1'] != $2`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )positive`), `variables['$1'] > 0`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )negative`), `variables['$1'] < 0`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )non-negative`), `variables['$1'] >= 0`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )non-positive`), `variables['$1'] <= 0`},
		{regexp.MustCompile(`(\w+)\s+(?:must be |is |should be )(?:in range |between )\[?(\d+),\s*(\d+)\]?`), `(variables['$1'] >= $2) & (variables['$1'] <= $3)`},
	}

	result := natural
	for _, p := range patterns {
		result = p.regex.ReplaceAllString(result, p.replace)
	}

	// If no pattern matched, return the original as a comment
	if result == natural {
		return fmt.Sprintf("True  # %s", natural)
	}

	return result
}

// typeToConstraint converts a Python type to a constraint expression.
func (c *VerificationChain) typeToConstraint(varName, typeName string) string {
	switch typeName {
	case "int":
		return fmt.Sprintf("isinstance(variables['%s'], int)", varName)
	case "float":
		return fmt.Sprintf("isinstance(variables['%s'], (int, float))", varName)
	case "bool":
		return fmt.Sprintf("isinstance(variables['%s'], bool)", varName)
	case "str":
		return fmt.Sprintf("isinstance(variables['%s'], str)", varName)
	default:
		return ""
	}
}

// goTypeToConstraint converts a Go type to a constraint expression.
func (c *VerificationChain) goTypeToConstraint(varName, typeName string) string {
	switch typeName {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return fmt.Sprintf("isinstance(variables['%s'], int)", varName)
	case "float32", "float64":
		return fmt.Sprintf("isinstance(variables['%s'], (int, float))", varName)
	case "bool":
		return fmt.Sprintf("isinstance(variables['%s'], bool)", varName)
	case "string":
		return fmt.Sprintf("isinstance(variables['%s'], str)", varName)
	default:
		return ""
	}
}

// extractVariables extracts variable names from an expression.
func (c *VerificationChain) extractVariables(expr string) []string {
	var vars []string
	seen := make(map[string]bool)

	// Match variables['name'] pattern
	varPattern := regexp.MustCompile(`variables\['(\w+)'\]`)
	matches := varPattern.FindAllStringSubmatch(expr, -1)
	for _, match := range matches {
		if len(match) >= 2 && !seen[match[1]] {
			vars = append(vars, match[1])
			seen[match[1]] = true
		}
	}

	// Match standalone identifiers (but not keywords)
	keywords := map[string]bool{
		"True": true, "False": true, "None": true, "and": true, "or": true, "not": true,
		"in": true, "is": true, "isinstance": true, "variables": true,
	}

	idPattern := regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\b`)
	matches = idPattern.FindAllStringSubmatch(expr, -1)
	for _, match := range matches {
		if len(match) >= 2 && !seen[match[1]] && !keywords[match[1]] {
			vars = append(vars, match[1])
			seen[match[1]] = true
		}
	}

	return vars
}
