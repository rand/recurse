package verify

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewVerificationChain(t *testing.T) {
	chain := NewVerificationChain(nil)
	require.NotNil(t, chain)
	assert.Equal(t, 30*time.Second, chain.timeout)
}

func TestVerificationChain_SetTimeout(t *testing.T) {
	chain := NewVerificationChain(nil)
	chain.SetTimeout(10 * time.Second)
	assert.Equal(t, 10*time.Second, chain.timeout)
}

func TestVerificationChain_GeneratePreconditions_Docstrings(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def calculate(x, y):
    """
    Calculate the sum.

    @requires x must be positive
    @requires y must be greater than 0
    """
    return x + y
`

	change := CodeChange{
		After:    code,
		Language: "python",
	}

	constraints, err := chain.GeneratePreconditions(ctx, change)
	require.NoError(t, err)
	assert.NotEmpty(t, constraints)

	// Check that we extracted the @requires constraints
	var foundPositive, foundGreater bool
	for _, c := range constraints {
		if c.Type == ConstraintTypePrecondition {
			if c.Description == "x must be positive" {
				foundPositive = true
			}
			if c.Description == "y must be greater than 0" {
				foundGreater = true
			}
		}
	}
	assert.True(t, foundPositive, "should find 'x must be positive' constraint")
	assert.True(t, foundGreater, "should find 'y must be greater than 0' constraint")
}

func TestVerificationChain_GeneratePreconditions_TypeAnnotations(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def process(count: int, ratio: float, enabled: bool):
    return count * ratio if enabled else 0
`

	change := CodeChange{
		After:    code,
		Language: "python",
	}

	constraints, err := chain.GeneratePreconditions(ctx, change)
	require.NoError(t, err)

	// Should find type constraints
	typeConstraints := 0
	for _, c := range constraints {
		if c.Type == ConstraintTypeTypeCheck {
			typeConstraints++
		}
	}
	assert.GreaterOrEqual(t, typeConstraints, 3)
}

func TestVerificationChain_GeneratePreconditions_Asserts(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def divide(a, b):
    assert b != 0
    assert a >= 0
    return a / b
`

	change := CodeChange{
		After:    code,
		Language: "python",
	}

	constraints, err := chain.GeneratePreconditions(ctx, change)
	require.NoError(t, err)

	// Should find assert constraints
	assertConstraints := 0
	for _, c := range constraints {
		if c.Source == "assert" {
			assertConstraints++
		}
	}
	assert.GreaterOrEqual(t, assertConstraints, 2)
}

func TestVerificationChain_GeneratePostconditions_ReturnType(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def get_count() -> int:
    return 42
`

	change := CodeChange{
		After:    code,
		Language: "python",
	}

	constraints, err := chain.GeneratePostconditions(ctx, change)
	require.NoError(t, err)
	assert.NotEmpty(t, constraints)

	// Should have return type constraint
	var foundReturnType bool
	for _, c := range constraints {
		if c.Name == "return_type" {
			foundReturnType = true
		}
	}
	assert.True(t, foundReturnType)
}

func TestVerificationChain_GeneratePostconditions_Ensures(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def square(n):
    """
    @ensures result is non-negative
    """
    return n * n
`

	change := CodeChange{
		After:    code,
		Language: "python",
	}

	constraints, err := chain.GeneratePostconditions(ctx, change)
	require.NoError(t, err)

	var foundEnsures bool
	for _, c := range constraints {
		if c.Type == ConstraintTypePostcondition && c.Source == "docstring" {
			foundEnsures = true
		}
	}
	assert.True(t, foundEnsures)
}

func TestVerificationChain_GenerateInvariants(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def process():
    """
    @invariant count must be non-negative
    """
    count = 0
    for i in range(10):
        count += 1
    return count
`

	change := CodeChange{
		After:    code,
		Language: "python",
	}

	constraints, err := chain.GenerateInvariants(ctx, change)
	require.NoError(t, err)

	var foundInvariant bool
	for _, c := range constraints {
		if c.Type == ConstraintTypeInvariant {
			foundInvariant = true
		}
	}
	assert.True(t, foundInvariant)
}

func TestVerificationChain_VerifyChange_NoConstraints(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	// Code with no extractable constraints
	code := `
def hello():
    print("Hello, World!")
`

	change := CodeChange{
		After:    code,
		Language: "python",
	}

	result, err := chain.VerifyChange(ctx, change)
	require.NoError(t, err)
	assert.True(t, result.Satisfied)
	assert.Equal(t, StatusSatisfied, result.Status)
}

func TestVerificationChain_Verify_NoREPL(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	constraints := []Constraint{
		{
			Type:       ConstraintTypePrecondition,
			Name:       "test",
			Expression: "True",
		},
	}

	_, err := chain.Verify(ctx, constraints, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REPL manager not configured")
}

func TestVerificationChain_BuildVerificationCode(t *testing.T) {
	chain := NewVerificationChain(nil)

	constraints := []Constraint{
		{
			Type:       ConstraintTypePrecondition,
			Name:       "x_positive",
			Expression: "variables['x'] > 0",
			Variables:  []string{"x"},
		},
		{
			Type:       ConstraintTypePostcondition,
			Name:       "result_valid",
			Expression: "variables['result'] >= 0",
			Variables:  []string{"result"},
		},
	}

	code := chain.buildVerificationCode(constraints, "")

	// Check that the generated code has expected components
	assert.Contains(t, code, "from cpmpy import")
	assert.Contains(t, code, "variables['x']")
	assert.Contains(t, code, "variables['result']")
	assert.Contains(t, code, "model.solve()")
	assert.Contains(t, code, "json.dumps(result)")
}

func TestVerificationChain_ExtractFromDocstrings(t *testing.T) {
	chain := NewVerificationChain(nil)

	tests := []struct {
		name           string
		code           string
		constraintType ConstraintType
		wantCount      int
	}{
		{
			name: "requires annotation",
			code: `
@requires x > 0
@requires y >= 0
def foo():
    pass
`,
			constraintType: ConstraintTypePrecondition,
			wantCount:      2,
		},
		{
			name: "ensures annotation",
			code: `
@ensures result is positive
def bar():
    pass
`,
			constraintType: ConstraintTypePostcondition,
			wantCount:      1,
		},
		{
			name: "invariant annotation",
			code: `
@invariant counter >= 0
def baz():
    pass
`,
			constraintType: ConstraintTypeInvariant,
			wantCount:      1,
		},
		{
			name: "pre/post style",
			code: `
"""
Pre: n must be positive
Post: result is non-negative
"""
`,
			constraintType: ConstraintTypePrecondition,
			wantCount:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraints := chain.extractFromDocstrings(tt.code, tt.constraintType)
			assert.Len(t, constraints, tt.wantCount)
		})
	}
}

func TestVerificationChain_ExtractTypeConstraints_Python(t *testing.T) {
	chain := NewVerificationChain(nil)

	code := `
def process(x: int, y: float, flag: bool):
    pass
`

	constraints := chain.extractTypeConstraints(code, "python")

	assert.Len(t, constraints, 3)
	for _, c := range constraints {
		assert.Equal(t, ConstraintTypeTypeCheck, c.Type)
		assert.Equal(t, "type_annotation", c.Source)
		assert.Equal(t, 0.95, c.Confidence)
	}
}

func TestVerificationChain_ExtractTypeConstraints_Go(t *testing.T) {
	chain := NewVerificationChain(nil)

	code := `
func process(x int, y float64, flag bool) error {
    return nil
}
`

	constraints := chain.extractTypeConstraints(code, "go")

	assert.GreaterOrEqual(t, len(constraints), 3)
	for _, c := range constraints {
		assert.Equal(t, ConstraintTypeTypeCheck, c.Type)
	}
}

func TestVerificationChain_ExtractAssertConstraints(t *testing.T) {
	chain := NewVerificationChain(nil)

	code := `
def validate(x, y):
    assert x > 0
    assert y != 0
    return x / y
`

	constraints := chain.extractAssertConstraints(code, "python")

	assert.Len(t, constraints, 2)
	for _, c := range constraints {
		assert.Equal(t, "assert", c.Source)
		assert.Equal(t, 0.9, c.Confidence)
	}
}

func TestVerificationChain_NaturalToConstraint(t *testing.T) {
	chain := NewVerificationChain(nil)

	tests := []struct {
		natural  string
		expected string
	}{
		{"x must be positive", "variables['x'] > 0"},
		{"y is non-negative", "variables['y'] >= 0"},
		{"count must be greater than limit", "variables['count'] > limit"},
		{"value must be less than 100", "variables['value'] < 100"},
		{"result is non-positive", "variables['result'] <= 0"},
	}

	for _, tt := range tests {
		t.Run(tt.natural, func(t *testing.T) {
			result := chain.naturalToConstraint(tt.natural)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestVerificationChain_NaturalToConstraint_Range(t *testing.T) {
	chain := NewVerificationChain(nil)

	result := chain.naturalToConstraint("x must be in range [0, 100]")
	assert.Contains(t, result, "variables['x'] >= 0")
	assert.Contains(t, result, "variables['x'] <= 100")
}

func TestVerificationChain_NaturalToConstraint_Unrecognized(t *testing.T) {
	chain := NewVerificationChain(nil)

	result := chain.naturalToConstraint("this is something random")
	assert.Contains(t, result, "True")
	assert.Contains(t, result, "# this is something random")
}

func TestVerificationChain_ExtractVariables(t *testing.T) {
	chain := NewVerificationChain(nil)

	tests := []struct {
		expr     string
		expected []string
	}{
		{"variables['x'] > 0", []string{"x"}},
		{"variables['x'] + variables['y']", []string{"x", "y"}},
		{"x > y", []string{"x", "y"}},
		{"True", []string{}},
		{"isinstance(variables['count'], int)", []string{"count"}},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			vars := chain.extractVariables(tt.expr)
			for _, expected := range tt.expected {
				assert.Contains(t, vars, expected)
			}
		})
	}
}

func TestVerificationChain_TypeToConstraint(t *testing.T) {
	chain := NewVerificationChain(nil)

	tests := []struct {
		varName  string
		typeName string
		wantExpr string
	}{
		{"x", "int", "isinstance(variables['x'], int)"},
		{"y", "float", "isinstance(variables['y'], (int, float))"},
		{"flag", "bool", "isinstance(variables['flag'], bool)"},
		{"name", "str", "isinstance(variables['name'], str)"},
		{"obj", "CustomType", ""},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			result := chain.typeToConstraint(tt.varName, tt.typeName)
			assert.Equal(t, tt.wantExpr, result)
		})
	}
}

func TestVerificationChain_GoTypeToConstraint(t *testing.T) {
	chain := NewVerificationChain(nil)

	tests := []struct {
		varName  string
		typeName string
		wantExpr string
	}{
		{"x", "int", "isinstance(variables['x'], int)"},
		{"x", "int64", "isinstance(variables['x'], int)"},
		{"y", "float64", "isinstance(variables['y'], (int, float))"},
		{"flag", "bool", "isinstance(variables['flag'], bool)"},
		{"name", "string", "isinstance(variables['name'], str)"},
		{"obj", "CustomType", ""},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			result := chain.goTypeToConstraint(tt.varName, tt.typeName)
			assert.Equal(t, tt.wantExpr, result)
		})
	}
}

func TestConstraintType_Values(t *testing.T) {
	assert.Equal(t, ConstraintType("precondition"), ConstraintTypePrecondition)
	assert.Equal(t, ConstraintType("postcondition"), ConstraintTypePostcondition)
	assert.Equal(t, ConstraintType("invariant"), ConstraintTypeInvariant)
	assert.Equal(t, ConstraintType("type_check"), ConstraintTypeTypeCheck)
	assert.Equal(t, ConstraintType("call_graph"), ConstraintTypeCallGraph)
}

func TestVerificationStatus_Values(t *testing.T) {
	assert.Equal(t, VerificationStatus("satisfied"), StatusSatisfied)
	assert.Equal(t, VerificationStatus("violated"), StatusViolated)
	assert.Equal(t, VerificationStatus("unknown"), StatusUnknown)
	assert.Equal(t, VerificationStatus("timeout"), StatusTimeout)
	assert.Equal(t, VerificationStatus("error"), StatusError)
}

func TestCodeChange_Fields(t *testing.T) {
	change := CodeChange{
		Before:   "old code",
		After:    "new code",
		Language: "python",
		Context:  "some context",
	}

	assert.Equal(t, "old code", change.Before)
	assert.Equal(t, "new code", change.After)
	assert.Equal(t, "python", change.Language)
	assert.Equal(t, "some context", change.Context)
}

func TestConstraint_Fields(t *testing.T) {
	constraint := Constraint{
		Type:        ConstraintTypePrecondition,
		Name:        "test_constraint",
		Expression:  "x > 0",
		Description: "x must be positive",
		Source:      "docstring",
		Confidence:  0.8,
		Variables:   []string{"x"},
	}

	assert.Equal(t, ConstraintTypePrecondition, constraint.Type)
	assert.Equal(t, "test_constraint", constraint.Name)
	assert.Equal(t, "x > 0", constraint.Expression)
	assert.Equal(t, "x must be positive", constraint.Description)
	assert.Equal(t, "docstring", constraint.Source)
	assert.Equal(t, 0.8, constraint.Confidence)
	assert.Contains(t, constraint.Variables, "x")
}

func TestVerificationResult_Fields(t *testing.T) {
	result := VerificationResult{
		Satisfied: true,
		Status:    StatusSatisfied,
		CheckedConstraints: []ConstraintResult{
			{Satisfied: true, Message: "OK"},
		},
		Duration: 100 * time.Millisecond,
	}

	assert.True(t, result.Satisfied)
	assert.Equal(t, StatusSatisfied, result.Status)
	assert.Len(t, result.CheckedConstraints, 1)
	assert.Equal(t, 100*time.Millisecond, result.Duration)
}

func TestCounterExample_Fields(t *testing.T) {
	ce := CounterExample{
		Variables: map[string]any{
			"x": -5,
			"y": 10,
		},
		Explanation: "x must be positive but was -5",
	}

	assert.Equal(t, -5, ce.Variables["x"])
	assert.Equal(t, 10, ce.Variables["y"])
	assert.Contains(t, ce.Explanation, "x must be positive")
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestVerificationChain_VerifyChange_WithConstraints(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	// Code with extractable constraints but no REPL
	code := `
def calculate(x: int, y: int) -> int:
    """
    Calculate the sum of two positive numbers.

    @requires x must be positive
    @requires y must be positive
    @ensures result is non-negative
    """
    assert x > 0
    assert y > 0
    return x + y
`

	change := CodeChange{
		After:    code,
		Language: "python",
	}

	// Without REPL, verify should fail
	_, err := chain.VerifyChange(ctx, change)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REPL manager not configured")
}

func TestVerificationChain_ComplexConstraintExtraction(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def process_data(items: list, threshold: float, max_count: int) -> dict:
    """
    Process items that meet the threshold.

    @requires threshold must be in range [0, 1]
    @requires max_count must be positive
    @requires items is non-negative
    @ensures result is non-negative
    @invariant processed_count must be less than max_count
    """
    processed_count = 0
    results = {}

    for item in items:
        if item > threshold and processed_count < max_count:
            results[item] = True
            processed_count += 1

    return results
`

	change := CodeChange{
		After:    code,
		Language: "python",
	}

	// Extract all constraint types
	pre, err := chain.GeneratePreconditions(ctx, change)
	require.NoError(t, err)

	post, err := chain.GeneratePostconditions(ctx, change)
	require.NoError(t, err)

	inv, err := chain.GenerateInvariants(ctx, change)
	require.NoError(t, err)

	// Should have constraints from all sources
	assert.NotEmpty(t, pre, "should have preconditions")
	assert.NotEmpty(t, post, "should have postconditions")
	assert.NotEmpty(t, inv, "should have invariants")

	// Check specific constraints
	var foundRange, foundPositive bool
	for _, c := range pre {
		if c.Description == "threshold must be in range [0, 1]" {
			foundRange = true
		}
		if c.Description == "max_count must be positive" {
			foundPositive = true
		}
	}
	assert.True(t, foundRange, "should find range constraint")
	assert.True(t, foundPositive, "should find positive constraint")
}

func TestVerificationChain_GoCodeConstraints(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
// ValidateInput checks if the input is valid.
// @requires value must be positive
// @ensures result is non-negative
func ValidateInput(value int, name string, enabled bool) (int, error) {
    if value <= 0 {
        panic("value must be positive")
    }
    return value * 2, nil
}
`

	change := CodeChange{
		After:    code,
		Language: "go",
	}

	pre, err := chain.GeneratePreconditions(ctx, change)
	require.NoError(t, err)

	// Should extract Go type constraints
	var foundIntType, foundStringType, foundBoolType bool
	for _, c := range pre {
		if c.Type == ConstraintTypeTypeCheck {
			switch {
			case c.Name == "type_value":
				foundIntType = true
			case c.Name == "type_name":
				foundStringType = true
			case c.Name == "type_enabled":
				foundBoolType = true
			}
		}
	}
	assert.True(t, foundIntType, "should find int type constraint")
	assert.True(t, foundStringType, "should find string type constraint")
	assert.True(t, foundBoolType, "should find bool type constraint")
}

func TestVerificationChain_ParseVerificationResult_Success(t *testing.T) {
	chain := NewVerificationChain(nil)

	constraints := []Constraint{
		{Name: "c1", Type: ConstraintTypePrecondition},
		{Name: "c2", Type: ConstraintTypePostcondition},
	}

	jsonResult := `{
		"satisfied": true,
		"status": "satisfied",
		"constraints": [
			{"index": 0, "name": "c1", "type": "precondition", "added": true, "error": null},
			{"index": 1, "name": "c2", "type": "postcondition", "added": true, "error": null}
		],
		"counter_example": null
	}`

	result := &VerificationResult{}
	parsed, err := chain.parseVerificationResult(jsonResult, constraints, result)
	require.NoError(t, err)

	assert.True(t, parsed.Satisfied)
	assert.Equal(t, StatusSatisfied, parsed.Status)
	assert.Len(t, parsed.CheckedConstraints, 2)
	assert.Nil(t, parsed.CounterExample)
}

func TestVerificationChain_ParseVerificationResult_Failure(t *testing.T) {
	chain := NewVerificationChain(nil)

	constraints := []Constraint{
		{Name: "c1", Type: ConstraintTypePrecondition},
	}

	jsonResult := `{
		"satisfied": false,
		"status": "violated",
		"constraints": [
			{"index": 0, "name": "c1", "type": "precondition", "added": true, "error": null}
		],
		"counter_example": {
			"variables": {"x": "-5"},
			"explanation": "x must be positive but was -5"
		}
	}`

	result := &VerificationResult{}
	parsed, err := chain.parseVerificationResult(jsonResult, constraints, result)
	require.NoError(t, err)

	assert.False(t, parsed.Satisfied)
	assert.Equal(t, StatusViolated, parsed.Status)
	assert.NotNil(t, parsed.CounterExample)
	assert.Equal(t, "-5", parsed.CounterExample.Variables["x"])
}

func TestVerificationChain_ParseVerificationResult_InvalidJSON(t *testing.T) {
	chain := NewVerificationChain(nil)

	constraints := []Constraint{}
	jsonResult := `not valid json`

	result := &VerificationResult{}
	parsed, err := chain.parseVerificationResult(jsonResult, constraints, result)
	require.NoError(t, err) // Should not error, just return unknown status

	assert.Equal(t, StatusUnknown, parsed.Status)
}

func TestVerificationChain_BuildVerificationCode_MultipleConstraints(t *testing.T) {
	chain := NewVerificationChain(nil)

	constraints := []Constraint{
		{
			Type:       ConstraintTypePrecondition,
			Name:       "x_positive",
			Expression: "variables['x'] > 0",
			Variables:  []string{"x"},
		},
		{
			Type:       ConstraintTypePrecondition,
			Name:       "y_non_negative",
			Expression: "variables['y'] >= 0",
			Variables:  []string{"y"},
		},
		{
			Type:       ConstraintTypePostcondition,
			Name:       "result_valid",
			Expression: "variables['result'] == variables['x'] + variables['y']",
			Variables:  []string{"result", "x", "y"},
		},
	}

	code := chain.buildVerificationCode(constraints, "")

	// Check all variables are declared
	assert.Contains(t, code, "variables['x']")
	assert.Contains(t, code, "variables['y']")
	assert.Contains(t, code, "variables['result']")

	// Check all constraints are added
	assert.Contains(t, code, "constraint_0")
	assert.Contains(t, code, "constraint_1")
	assert.Contains(t, code, "constraint_2")

	// Check constraint names are tracked
	assert.Contains(t, code, `'name': "x_positive"`)
	assert.Contains(t, code, `'name': "y_non_negative"`)
	assert.Contains(t, code, `'name': "result_valid"`)
}

func TestVerificationChain_BuildVerificationCode_EmptyConstraints(t *testing.T) {
	chain := NewVerificationChain(nil)

	code := chain.buildVerificationCode([]Constraint{}, "")

	// Should still have basic structure
	assert.Contains(t, code, "from cpmpy import")
	assert.Contains(t, code, "model = Model()")
	assert.Contains(t, code, "model.solve()")
	assert.Contains(t, code, "json.dumps(result)")
}

// =============================================================================
// Property-Based Tests
// =============================================================================

func TestProperty_ConstraintExtractionDeterministic(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def foo(x: int) -> int:
    """
    @requires x must be positive
    @ensures result is non-negative
    """
    return x * 2
`

	change := CodeChange{After: code, Language: "python"}

	// Run extraction multiple times
	var results [][]Constraint
	for i := 0; i < 5; i++ {
		pre, err := chain.GeneratePreconditions(ctx, change)
		require.NoError(t, err)
		results = append(results, pre)
	}

	// All results should be identical
	for i := 1; i < len(results); i++ {
		assert.Equal(t, len(results[0]), len(results[i]),
			"constraint count should be deterministic")
		for j := range results[0] {
			assert.Equal(t, results[0][j].Expression, results[i][j].Expression,
				"constraint expressions should be deterministic")
		}
	}
}

func TestProperty_NaturalToConstraintIdempotent(t *testing.T) {
	chain := NewVerificationChain(nil)

	naturalPhrases := []string{
		"x must be positive",
		"y is non-negative",
		"count must be greater than 0",
		"value must be in range [0, 100]",
	}

	for _, phrase := range naturalPhrases {
		result1 := chain.naturalToConstraint(phrase)
		result2 := chain.naturalToConstraint(phrase)
		assert.Equal(t, result1, result2,
			"naturalToConstraint should be idempotent for: %s", phrase)
	}
}

func TestProperty_ExtractVariablesNoDuplicates(t *testing.T) {
	chain := NewVerificationChain(nil)

	expressions := []string{
		"variables['x'] > 0 and variables['x'] < 100",
		"variables['a'] + variables['b'] == variables['a'] * 2",
		"x > y and y > z and z > x",
	}

	for _, expr := range expressions {
		vars := chain.extractVariables(expr)

		// Check for duplicates
		seen := make(map[string]bool)
		for _, v := range vars {
			assert.False(t, seen[v], "variable %s should not be duplicated in expression: %s", v, expr)
			seen[v] = true
		}
	}
}

func TestProperty_TypeConstraintsAlwaysHighConfidence(t *testing.T) {
	chain := NewVerificationChain(nil)

	pythonCode := `
def foo(a: int, b: float, c: bool, d: str):
    pass
`
	goCode := `
func bar(a int, b float64, c bool, d string) {
}
`

	pyConstraints := chain.extractTypeConstraints(pythonCode, "python")
	goConstraints := chain.extractTypeConstraints(goCode, "go")

	for _, c := range pyConstraints {
		assert.GreaterOrEqual(t, c.Confidence, 0.9,
			"Python type constraints should have high confidence")
	}

	for _, c := range goConstraints {
		assert.GreaterOrEqual(t, c.Confidence, 0.9,
			"Go type constraints should have high confidence")
	}
}

func TestProperty_AllConstraintTypesHaveValidExpression(t *testing.T) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def process(x: int) -> int:
    """
    @requires x must be positive
    @ensures result is non-negative
    @invariant state must be valid
    """
    assert x > 0
    return x * 2
`

	change := CodeChange{After: code, Language: "python"}

	pre, _ := chain.GeneratePreconditions(ctx, change)
	post, _ := chain.GeneratePostconditions(ctx, change)
	inv, _ := chain.GenerateInvariants(ctx, change)

	allConstraints := append(append(pre, post...), inv...)

	for _, c := range allConstraints {
		assert.NotEmpty(t, c.Expression,
			"constraint %s should have non-empty expression", c.Name)
		assert.NotEmpty(t, c.Type,
			"constraint %s should have non-empty type", c.Name)
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkVerificationChain_ExtractPreconditions(b *testing.B) {
	chain := NewVerificationChain(nil)
	ctx := context.Background()

	code := `
def calculate(x: int, y: float, flag: bool) -> int:
    """
    @requires x must be positive
    @requires y must be non-negative
    @requires flag is True
    """
    assert x > 0
    assert y >= 0
    return int(x * y) if flag else 0
`
	change := CodeChange{After: code, Language: "python"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain.GeneratePreconditions(ctx, change)
	}
}

func BenchmarkVerificationChain_NaturalToConstraint(b *testing.B) {
	chain := NewVerificationChain(nil)
	phrases := []string{
		"x must be positive",
		"y is non-negative",
		"count must be greater than limit",
		"value must be in range [0, 100]",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		phrase := phrases[i%len(phrases)]
		_ = chain.naturalToConstraint(phrase)
	}
}

func BenchmarkVerificationChain_BuildVerificationCode(b *testing.B) {
	chain := NewVerificationChain(nil)
	constraints := []Constraint{
		{Name: "c1", Expression: "variables['x'] > 0", Variables: []string{"x"}},
		{Name: "c2", Expression: "variables['y'] >= 0", Variables: []string{"y"}},
		{Name: "c3", Expression: "variables['z'] < 100", Variables: []string{"z"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chain.buildVerificationCode(constraints, "")
	}
}
