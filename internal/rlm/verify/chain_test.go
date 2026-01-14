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
