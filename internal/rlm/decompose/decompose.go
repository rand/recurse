// Package decompose implements strategies for breaking down large content into manageable chunks.
package decompose

import (
	"bufio"
	"regexp"
	"strings"
)

// Strategy specifies how to decompose content.
type Strategy string

const (
	StrategyFile     Strategy = "file"
	StrategyFunction Strategy = "function"
	StrategyConcept  Strategy = "concept"
	StrategyCustom   Strategy = "custom"
)

// Chunk represents a decomposed piece of content.
type Chunk struct {
	// ID uniquely identifies this chunk within the decomposition.
	ID string `json:"id"`

	// Name is a human-readable name for this chunk.
	Name string `json:"name"`

	// Content is the actual content of the chunk.
	Content string `json:"content"`

	// StartLine is the starting line number (1-indexed) if from a file.
	StartLine int `json:"start_line,omitempty"`

	// EndLine is the ending line number (1-indexed) if from a file.
	EndLine int `json:"end_line,omitempty"`

	// Metadata contains additional context about the chunk.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Decomposer breaks down content into chunks.
type Decomposer interface {
	// Decompose breaks content into chunks using the configured strategy.
	Decompose(content string) ([]Chunk, error)

	// Strategy returns the decomposition strategy.
	Strategy() Strategy
}

// FileDecomposer breaks content into file-sized chunks based on markers.
type FileDecomposer struct {
	// FileMarker is a regex pattern that identifies file boundaries.
	// Default: `^(?://|#)\s*(?:File|FILE):\s*(.+)$`
	FileMarker *regexp.Regexp
}

// NewFileDecomposer creates a file-based decomposer.
func NewFileDecomposer() *FileDecomposer {
	return &FileDecomposer{
		FileMarker: regexp.MustCompile(`^(?://|#)\s*(?:File|FILE):\s*(.+)$`),
	}
}

// Strategy implements Decomposer.
func (d *FileDecomposer) Strategy() Strategy {
	return StrategyFile
}

// Decompose implements Decomposer.
func (d *FileDecomposer) Decompose(content string) ([]Chunk, error) {
	var chunks []Chunk
	var currentFile string
	var currentContent strings.Builder
	var startLine int
	lineNum := 0

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if matches := d.FileMarker.FindStringSubmatch(line); len(matches) > 1 {
			// Save previous chunk if exists
			if currentFile != "" && currentContent.Len() > 0 {
				chunks = append(chunks, Chunk{
					ID:        currentFile,
					Name:      currentFile,
					Content:   strings.TrimSpace(currentContent.String()),
					StartLine: startLine,
					EndLine:   lineNum - 1,
				})
			}
			// Start new file
			currentFile = strings.TrimSpace(matches[1])
			currentContent.Reset()
			startLine = lineNum + 1
		} else {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}

	// Add final chunk
	if currentFile != "" && currentContent.Len() > 0 {
		chunks = append(chunks, Chunk{
			ID:        currentFile,
			Name:      currentFile,
			Content:   strings.TrimSpace(currentContent.String()),
			StartLine: startLine,
			EndLine:   lineNum,
		})
	}

	// If no file markers found, treat entire content as single chunk
	if len(chunks) == 0 && len(content) > 0 {
		chunks = append(chunks, Chunk{
			ID:      "content",
			Name:    "content",
			Content: content,
		})
	}

	return chunks, nil
}

// FunctionDecomposer breaks content into function-sized chunks.
type FunctionDecomposer struct {
	// Language hint for parsing (e.g., "go", "python", "javascript").
	Language string

	// patterns for different languages
	patterns map[string]*regexp.Regexp
}

// NewFunctionDecomposer creates a function-based decomposer.
func NewFunctionDecomposer(language string) *FunctionDecomposer {
	d := &FunctionDecomposer{
		Language: strings.ToLower(language),
		patterns: map[string]*regexp.Regexp{
			"go":         regexp.MustCompile(`(?m)^func\s+(?:\([^)]+\)\s+)?(\w+)`),
			"python":    regexp.MustCompile(`(?m)^def\s+(\w+)`),
			"javascript": regexp.MustCompile(`(?m)^(?:async\s+)?function\s+(\w+)|^(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(`),
			"typescript": regexp.MustCompile(`(?m)^(?:async\s+)?function\s+(\w+)|^(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(`),
			"rust":      regexp.MustCompile(`(?m)^(?:pub\s+)?(?:async\s+)?fn\s+(\w+)`),
		},
	}
	return d
}

// Strategy implements Decomposer.
func (d *FunctionDecomposer) Strategy() Strategy {
	return StrategyFunction
}

// Decompose implements Decomposer.
func (d *FunctionDecomposer) Decompose(content string) ([]Chunk, error) {
	pattern, ok := d.patterns[d.Language]
	if !ok {
		// Default pattern for unknown languages
		pattern = regexp.MustCompile(`(?m)^(?:func|def|function|fn)\s+(\w+)`)
	}

	lines := strings.Split(content, "\n")
	matches := pattern.FindAllStringSubmatchIndex(content, -1)

	if len(matches) == 0 {
		// No functions found, return entire content
		return []Chunk{{
			ID:      "content",
			Name:    "content",
			Content: content,
		}}, nil
	}

	var chunks []Chunk
	for i, match := range matches {
		startIdx := match[0]
		var endIdx int
		if i+1 < len(matches) {
			endIdx = matches[i+1][0]
		} else {
			endIdx = len(content)
		}

		// Extract function name from the match
		var funcName string
		for j := 2; j < len(match); j += 2 {
			if match[j] >= 0 {
				funcName = content[match[j]:match[j+1]]
				break
			}
		}
		if funcName == "" {
			funcName = "unknown"
		}

		chunkContent := strings.TrimSpace(content[startIdx:endIdx])

		// Calculate line numbers
		startLine := strings.Count(content[:startIdx], "\n") + 1
		endLine := startLine + strings.Count(chunkContent, "\n")

		chunks = append(chunks, Chunk{
			ID:        funcName,
			Name:      funcName,
			Content:   chunkContent,
			StartLine: startLine,
			EndLine:   endLine,
			Metadata: map[string]string{
				"language": d.Language,
				"type":     "function",
			},
		})
	}

	// Handle content before first function
	if len(matches) > 0 && matches[0][0] > 0 {
		preamble := strings.TrimSpace(content[:matches[0][0]])
		if len(preamble) > 0 {
			preambleChunk := Chunk{
				ID:        "preamble",
				Name:      "preamble",
				Content:   preamble,
				StartLine: 1,
				EndLine:   strings.Count(preamble, "\n") + 1,
				Metadata: map[string]string{
					"language": d.Language,
					"type":     "preamble",
				},
			}
			chunks = append([]Chunk{preambleChunk}, chunks...)
		}
	}

	_ = lines // silence unused warning
	return chunks, nil
}

// ConceptDecomposer breaks content into conceptual chunks based on structure.
type ConceptDecomposer struct {
	// MaxChunkSize is the target maximum size for each chunk in characters.
	MaxChunkSize int

	// OverlapSize is the overlap between adjacent chunks for context.
	OverlapSize int
}

// NewConceptDecomposer creates a concept-based decomposer.
func NewConceptDecomposer(maxChunkSize, overlapSize int) *ConceptDecomposer {
	if maxChunkSize == 0 {
		maxChunkSize = 4000 // ~1000 tokens
	}
	if overlapSize == 0 {
		overlapSize = 200
	}
	return &ConceptDecomposer{
		MaxChunkSize: maxChunkSize,
		OverlapSize:  overlapSize,
	}
}

// Strategy implements Decomposer.
func (d *ConceptDecomposer) Strategy() Strategy {
	return StrategyConcept
}

// Decompose implements Decomposer.
func (d *ConceptDecomposer) Decompose(content string) ([]Chunk, error) {
	// Split by paragraph boundaries first
	paragraphs := splitParagraphs(content)

	var chunks []Chunk
	var currentChunk strings.Builder
	var chunkStart int
	chunkNum := 0

	for _, para := range paragraphs {
		// If adding this paragraph exceeds max size, finalize current chunk
		if currentChunk.Len() > 0 && currentChunk.Len()+len(para) > d.MaxChunkSize {
			chunkNum++
			chunks = append(chunks, Chunk{
				ID:      formatChunkID(chunkNum),
				Name:    formatChunkName(chunkNum, len(chunks)+1),
				Content: strings.TrimSpace(currentChunk.String()),
			})

			// Start new chunk with overlap
			currentChunk.Reset()
			chunkStart = max(0, currentChunk.Len()-d.OverlapSize)
			if chunkStart > 0 {
				// Add overlap from previous content
				prevContent := chunks[len(chunks)-1].Content
				if len(prevContent) > d.OverlapSize {
					currentChunk.WriteString(prevContent[len(prevContent)-d.OverlapSize:])
					currentChunk.WriteString("\n\n")
				}
			}
		}

		currentChunk.WriteString(para)
		currentChunk.WriteString("\n\n")
	}

	// Add final chunk
	if currentChunk.Len() > 0 {
		chunkNum++
		chunks = append(chunks, Chunk{
			ID:      formatChunkID(chunkNum),
			Name:    formatChunkName(chunkNum, len(chunks)+1),
			Content: strings.TrimSpace(currentChunk.String()),
		})
	}

	_ = chunkStart // silence unused warning
	return chunks, nil
}

// splitParagraphs splits content into paragraphs.
func splitParagraphs(content string) []string {
	// Split on double newlines
	parts := regexp.MustCompile(`\n\s*\n`).Split(content, -1)

	var paragraphs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) > 0 {
			paragraphs = append(paragraphs, p)
		}
	}
	return paragraphs
}

func formatChunkID(n int) string {
	return strings.ToLower(strings.ReplaceAll(formatChunkName(n, n), " ", "-"))
}

func formatChunkName(n, total int) string {
	return strings.TrimSpace(strings.Replace("chunk-"+string(rune('0'+n)), "chunk-", "chunk ", 1))
}

// Auto selects the best decomposition strategy based on content analysis.
func Auto(content string) Decomposer {
	// Check for file markers
	if regexp.MustCompile(`(?m)^(?://|#)\s*(?:File|FILE):`).MatchString(content) {
		return NewFileDecomposer()
	}

	// Check for function definitions
	if regexp.MustCompile(`(?m)^(?:func|def|function|fn)\s+\w+`).MatchString(content) {
		// Try to detect language
		lang := detectLanguage(content)
		return NewFunctionDecomposer(lang)
	}

	// Default to concept-based
	return NewConceptDecomposer(4000, 200)
}

// detectLanguage attempts to detect the programming language of content.
func detectLanguage(content string) string {
	// Simple heuristics
	switch {
	case strings.Contains(content, "package ") && strings.Contains(content, "func "):
		return "go"
	case strings.Contains(content, "def ") && strings.Contains(content, ":"):
		return "python"
	case strings.Contains(content, "fn ") && strings.Contains(content, "->"):
		return "rust"
	case strings.Contains(content, "function ") || strings.Contains(content, "const "):
		return "javascript"
	default:
		return "unknown"
	}
}
