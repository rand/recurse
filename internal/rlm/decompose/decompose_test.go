package decompose

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileDecomposer_Basic(t *testing.T) {
	d := NewFileDecomposer()
	content := `// File: main.go
package main

func main() {
    fmt.Println("Hello")
}

// File: utils.go
package main

func helper() {}
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	assert.Equal(t, "main.go", chunks[0].Name)
	assert.Contains(t, chunks[0].Content, "package main")
	assert.Contains(t, chunks[0].Content, "func main()")

	assert.Equal(t, "utils.go", chunks[1].Name)
	assert.Contains(t, chunks[1].Content, "func helper()")
}

func TestFileDecomposer_HashMarker(t *testing.T) {
	d := NewFileDecomposer()
	content := `# File: script.py
def main():
    pass

# File: utils.py
def helper():
    pass
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	assert.Equal(t, "script.py", chunks[0].Name)
	assert.Equal(t, "utils.py", chunks[1].Name)
}

func TestFileDecomposer_NoMarkers(t *testing.T) {
	d := NewFileDecomposer()
	content := `package main

func main() {}
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	assert.Equal(t, "content", chunks[0].Name)
}

func TestFunctionDecomposer_Go(t *testing.T) {
	d := NewFunctionDecomposer("go")
	content := `package main

import "fmt"

func main() {
    fmt.Println("Hello")
}

func helper(x int) int {
    return x * 2
}
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.Len(t, chunks, 3) // preamble + 2 functions

	assert.Equal(t, "preamble", chunks[0].Name)
	assert.Contains(t, chunks[0].Content, "package main")

	assert.Equal(t, "main", chunks[1].Name)
	assert.Contains(t, chunks[1].Content, "func main()")

	assert.Equal(t, "helper", chunks[2].Name)
	assert.Contains(t, chunks[2].Content, "func helper")
}

func TestFunctionDecomposer_Python(t *testing.T) {
	d := NewFunctionDecomposer("python")
	content := `import os

def main():
    print("Hello")

def helper(x):
    return x * 2
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.Len(t, chunks, 3) // preamble + 2 functions

	assert.Equal(t, "main", chunks[1].Name)
	assert.Equal(t, "helper", chunks[2].Name)
}

func TestFunctionDecomposer_Rust(t *testing.T) {
	d := NewFunctionDecomposer("rust")
	content := `use std::io;

fn main() {
    println!("Hello");
}

pub fn helper(x: i32) -> i32 {
    x * 2
}

pub async fn async_fn() {}
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 3) // preamble + functions
}

func TestFunctionDecomposer_NoFunctions(t *testing.T) {
	d := NewFunctionDecomposer("go")
	content := `package main

const version = "1.0.0"
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	assert.Equal(t, "content", chunks[0].Name)
}

func TestConceptDecomposer_Basic(t *testing.T) {
	d := NewConceptDecomposer(100, 20)
	content := `First paragraph with some content.

Second paragraph with more content.

Third paragraph with even more content.

Fourth paragraph to test chunking.
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 1)

	// All content should be present across chunks
	var combined string
	for _, c := range chunks {
		combined += c.Content
	}
	assert.Contains(t, combined, "First paragraph")
	assert.Contains(t, combined, "Fourth paragraph")
}

func TestConceptDecomposer_LargeChunk(t *testing.T) {
	d := NewConceptDecomposer(50, 10)
	content := `Short paragraph one.

Short paragraph two.

Short paragraph three.
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 2)
}

func TestConceptDecomposer_SingleParagraph(t *testing.T) {
	d := NewConceptDecomposer(1000, 100)
	content := `This is a single paragraph with some content.`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	assert.Contains(t, chunks[0].Content, "single paragraph")
}

func TestAuto_FileMarkers(t *testing.T) {
	content := `// File: main.go
package main
`
	d := Auto(content)
	assert.Equal(t, StrategyFile, d.Strategy())
}

func TestAuto_GoFunctions(t *testing.T) {
	content := `package main

func main() {}
`
	d := Auto(content)
	assert.Equal(t, StrategyFunction, d.Strategy())
}

func TestAuto_PythonFunctions(t *testing.T) {
	content := `def main():
    pass
`
	d := Auto(content)
	assert.Equal(t, StrategyFunction, d.Strategy())
}

func TestAuto_PlainText(t *testing.T) {
	content := `This is just some plain text without any code.

It has multiple paragraphs but no function definitions.
`
	d := Auto(content)
	assert.Equal(t, StrategyConcept, d.Strategy())
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		content  string
		expected string
	}{
		{"package main\nfunc main() {}", "go"},
		{"def main():\n    pass", "python"},
		{"fn main() -> () {}", "rust"},
		{"function main() {}", "javascript"},
		{"random text", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := detectLanguage(tt.content)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestChunk_LineNumbers(t *testing.T) {
	d := NewFileDecomposer()
	content := `// File: first.go
line 2
line 3

// File: second.go
line 6
line 7
`

	chunks, err := d.Decompose(content)
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	assert.Equal(t, 2, chunks[0].StartLine)
	assert.Equal(t, 4, chunks[0].EndLine)

	assert.Equal(t, 6, chunks[1].StartLine)
}
