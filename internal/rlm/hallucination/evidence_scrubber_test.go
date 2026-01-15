package hallucination

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvidenceScrubber_ScrubByPositions(t *testing.T) {
	scrubber := NewEvidenceScrubber()

	t.Run("single span removal", func(t *testing.T) {
		context := "The Earth is round according to NASA scientists."
		spans := []EvidenceSpan{
			{ID: "1", StartPos: 19, EndPos: 47, Text: "according to NASA scientists"},
		}

		result := scrubber.ScrubByPositions(context, spans)

		assert.Equal(t, "The Earth is round [EVIDENCE REMOVED].", result.Scrubbed)
		assert.Len(t, result.RemovedSpans, 1)
		assert.Equal(t, "according to NASA scientists", result.RemovedSpans[0].Text)
	})

	t.Run("multiple non-overlapping spans", func(t *testing.T) {
		context := "Fact A is true. Fact B is also true."
		spans := []EvidenceSpan{
			{ID: "1", StartPos: 0, EndPos: 6, Text: "Fact A"},
			{ID: "2", StartPos: 16, EndPos: 22, Text: "Fact B"},
		}

		result := scrubber.ScrubByPositions(context, spans)

		assert.Equal(t, "[EVIDENCE REMOVED] is true. [EVIDENCE REMOVED] is also true.", result.Scrubbed)
		assert.Len(t, result.RemovedSpans, 2)
	})

	t.Run("empty spans", func(t *testing.T) {
		context := "No changes here."
		result := scrubber.ScrubByPositions(context, nil)

		assert.Equal(t, context, result.Scrubbed)
		assert.Empty(t, result.RemovedSpans)
	})

	t.Run("invalid span positions are skipped", func(t *testing.T) {
		context := "Valid text here."
		spans := []EvidenceSpan{
			{ID: "1", StartPos: -1, EndPos: 5},  // Invalid start
			{ID: "2", StartPos: 5, EndPos: 100}, // End beyond length
			{ID: "3", StartPos: 10, EndPos: 5},  // Start > End
		}

		result := scrubber.ScrubByPositions(context, spans)

		// All spans should be skipped
		assert.Equal(t, context, result.Scrubbed)
		assert.Empty(t, result.RemovedSpans)
	})
}

func TestEvidenceScrubber_OverlappingSpans(t *testing.T) {
	scrubber := NewEvidenceScrubber()

	t.Run("overlapping spans are merged", func(t *testing.T) {
		context := "AAAAABBBBBCCCCC"
		spans := []EvidenceSpan{
			{ID: "1", StartPos: 0, EndPos: 8},  // AAAAABBB
			{ID: "2", StartPos: 5, EndPos: 12}, // BBBBBCC
		}

		result := scrubber.ScrubByPositions(context, spans)

		// Should merge into one removal from 0-12
		assert.Equal(t, "[EVIDENCE REMOVED]CCC", result.Scrubbed)
		assert.Len(t, result.RemovedSpans, 1)
		assert.Equal(t, 0, result.RemovedSpans[0].StartPos)
		assert.Equal(t, 12, result.RemovedSpans[0].EndPos)
	})

	t.Run("adjacent spans are merged", func(t *testing.T) {
		context := "AAAAABBBBB"
		spans := []EvidenceSpan{
			{ID: "1", StartPos: 0, EndPos: 5},
			{ID: "2", StartPos: 5, EndPos: 10},
		}

		result := scrubber.ScrubByPositions(context, spans)

		assert.Equal(t, "[EVIDENCE REMOVED]", result.Scrubbed)
		assert.Len(t, result.RemovedSpans, 1)
	})

	t.Run("nested spans are merged", func(t *testing.T) {
		context := "AAAAABBBBBCCCCC"
		spans := []EvidenceSpan{
			{ID: "outer", StartPos: 0, EndPos: 15},
			{ID: "inner", StartPos: 5, EndPos: 10},
		}

		result := scrubber.ScrubByPositions(context, spans)

		// Inner is completely contained, should result in single removal
		assert.Equal(t, "[EVIDENCE REMOVED]", result.Scrubbed)
		assert.Len(t, result.RemovedSpans, 1)
	})

	t.Run("unsorted spans are handled", func(t *testing.T) {
		context := "First. Second. Third."
		spans := []EvidenceSpan{
			{ID: "3", StartPos: 15, EndPos: 20}, // Third
			{ID: "1", StartPos: 0, EndPos: 5},   // First
			{ID: "2", StartPos: 7, EndPos: 13},  // Second
		}

		result := scrubber.ScrubByPositions(context, spans)

		assert.Equal(t, "[EVIDENCE REMOVED]. [EVIDENCE REMOVED]. [EVIDENCE REMOVED].", result.Scrubbed)
		assert.Len(t, result.RemovedSpans, 3)
	})
}

func TestEvidenceScrubber_StructurePreservation(t *testing.T) {
	scrubber := NewEvidenceScrubber()
	scrubber.PreserveStructure = true

	t.Run("preserves paragraph breaks", func(t *testing.T) {
		context := "Header.\n\nParagraph one content.\n\nParagraph two content.\n\nFooter."
		spans := []EvidenceSpan{
			{ID: "1", StartPos: 9, EndPos: 54}, // Both paragraphs
		}

		result := scrubber.ScrubByPositions(context, spans)

		// Should have markers for each paragraph
		assert.Contains(t, result.Scrubbed, "[EVIDENCE REMOVED]\n\n[EVIDENCE REMOVED]")
	})

	t.Run("preserves line breaks", func(t *testing.T) {
		context := "Line 1\nLine 2\nLine 3"
		spans := []EvidenceSpan{
			{ID: "1", StartPos: 0, EndPos: 13}, // Line 1\nLine 2
		}

		result := scrubber.ScrubByPositions(context, spans)

		assert.Contains(t, result.Scrubbed, "[EVIDENCE REMOVED]\n[EVIDENCE REMOVED]")
	})

	t.Run("structure preservation disabled", func(t *testing.T) {
		scrubber := NewEvidenceScrubber()
		scrubber.PreserveStructure = false

		context := "Para 1.\n\nPara 2."
		spans := []EvidenceSpan{
			{ID: "1", StartPos: 0, EndPos: 16},
		}

		result := scrubber.ScrubByPositions(context, spans)

		// Should be single marker without structure
		assert.Equal(t, "[EVIDENCE REMOVED]", result.Scrubbed)
	})
}

func TestEvidenceScrubber_ScrubByCitations(t *testing.T) {
	scrubber := NewEvidenceScrubber()

	t.Run("removes cited evidence", func(t *testing.T) {
		context := `The Earth is approximately 4.5 billion years old.
This was determined through radiometric dating of meteorites.
Scientists have confirmed this through multiple methods.`

		citations := []Citation{
			{SpanID: "1"},
		}
		evidenceMap := map[string]string{
			"1": "This was determined through radiometric dating of meteorites.",
		}

		result := scrubber.ScrubByCitations(context, citations, evidenceMap)

		assert.NotContains(t, result.Scrubbed, "radiometric dating")
		assert.Contains(t, result.Scrubbed, "[EVIDENCE REMOVED]")
		assert.Len(t, result.RemovedSpans, 1)
	})

	t.Run("handles missing evidence", func(t *testing.T) {
		context := "Some text here."
		citations := []Citation{
			{SpanID: "nonexistent"},
		}
		evidenceMap := map[string]string{}

		result := scrubber.ScrubByCitations(context, citations, evidenceMap)

		assert.Equal(t, context, result.Scrubbed)
		assert.Empty(t, result.RemovedSpans)
	})

	t.Run("removes multiple occurrences", func(t *testing.T) {
		context := "NASA says yes. According to NASA, it's true. NASA confirmed."
		citations := []Citation{
			{SpanID: "source"},
		}
		evidenceMap := map[string]string{
			"source": "NASA",
		}

		result := scrubber.ScrubByCitations(context, citations, evidenceMap)

		// All occurrences should be removed
		assert.Equal(t, 3, strings.Count(result.Scrubbed, "[EVIDENCE REMOVED]"))
		assert.NotContains(t, result.Scrubbed, "NASA")
	})
}

func TestEvidenceScrubber_ScrubByPattern(t *testing.T) {
	scrubber := NewEvidenceScrubber()

	t.Run("removes pattern occurrences", func(t *testing.T) {
		context := "Value is 42. Another value is 42. Final value is 100."
		result := scrubber.ScrubByPattern(context, "42")

		assert.Equal(t, "Value is [EVIDENCE REMOVED]. Another value is [EVIDENCE REMOVED]. Final value is 100.", result.Scrubbed)
		assert.Len(t, result.RemovedSpans, 2)
	})

	t.Run("empty pattern returns unchanged", func(t *testing.T) {
		context := "Some text."
		result := scrubber.ScrubByPattern(context, "")

		assert.Equal(t, context, result.Scrubbed)
	})
}

func TestEvidenceScrubber_ScrubSection(t *testing.T) {
	scrubber := NewEvidenceScrubber()

	t.Run("removes section between markers", func(t *testing.T) {
		context := `Introduction text.

Evidence:
Source 1: The sun is a star.
Source 2: The Earth orbits the sun.
End Evidence

Conclusion text.`

		result := scrubber.ScrubSection(context, "Evidence:", "End Evidence")

		assert.NotContains(t, result.Scrubbed, "Source 1")
		assert.NotContains(t, result.Scrubbed, "Source 2")
		assert.Contains(t, result.Scrubbed, "Introduction text")
		assert.Contains(t, result.Scrubbed, "Conclusion text")
	})

	t.Run("handles section extending to end", func(t *testing.T) {
		context := `Main content.

References:
[1] First source
[2] Second source`

		result := scrubber.ScrubSection(context, "References:", "NONEXISTENT")

		assert.Contains(t, result.Scrubbed, "Main content")
		assert.NotContains(t, result.Scrubbed, "First source")
		assert.NotContains(t, result.Scrubbed, "Second source")
	})

	t.Run("handles multiple sections", func(t *testing.T) {
		context := `[START]Content A[END] middle [START]Content B[END]`
		result := scrubber.ScrubSection(context, "[START]", "[END]")

		assert.Equal(t, 2, strings.Count(result.Scrubbed, "[EVIDENCE REMOVED]"))
		assert.Contains(t, result.Scrubbed, " middle ")
	})
}

func TestCreateScrubbedContext(t *testing.T) {
	context := "The fact is supported by evidence here."
	spans := []EvidenceSpan{
		{ID: "1", StartPos: 22, EndPos: 38}, // "by evidence here"
	}

	scrubbed := CreateScrubbedContext(context, spans)

	assert.Equal(t, "The fact is supported [EVIDENCE REMOVED].", scrubbed)
}

func TestEvidenceSpansFromClaims(t *testing.T) {
	context := `The sky is blue according to scientific observation.
This has been verified by NASA and other agencies.`

	claims := []ClaimWithCitedEvidence{
		{
			Claim:    Claim{Content: "The sky is blue"},
			Evidence: "scientific observation",
		},
		{
			Claim:    Claim{Content: "Verified fact"},
			Evidence: "NASA",
		},
	}

	spans := EvidenceSpansFromClaims(context, claims)

	require.Len(t, spans, 2)
	assert.Equal(t, "scientific observation", spans[0].Text)
	assert.Equal(t, "NASA", spans[1].Text)
}

func TestEvidenceScrubber_CustomMarker(t *testing.T) {
	scrubber := NewEvidenceScrubber()
	scrubber.Marker = "[REDACTED]"

	context := "Secret information here."
	spans := []EvidenceSpan{
		{ID: "1", StartPos: 7, EndPos: 18},
	}

	result := scrubber.ScrubByPositions(context, spans)

	assert.Contains(t, result.Scrubbed, "[REDACTED]")
	assert.NotContains(t, result.Scrubbed, "[EVIDENCE REMOVED]")
}

// TestRealWorldScrubbing tests with realistic LLM context patterns.
func TestRealWorldScrubbing(t *testing.T) {
	scrubber := NewEvidenceScrubber()

	t.Run("RAG context scrubbing", func(t *testing.T) {
		context := `User question: What is the capital of France?

Retrieved context:
[Document 1] Paris is the capital and largest city of France. It has been the capital since the 10th century.
[Document 2] France is a country in Western Europe. Its capital Paris is known for the Eiffel Tower.
---END CONTEXT---

Based on the above context, Paris is the capital of France.`

		// Scrub the retrieved context section (between markers)
		result := scrubber.ScrubSection(context, "Retrieved context:", "---END CONTEXT---")

		assert.Contains(t, result.Scrubbed, "User question")
		assert.Contains(t, result.Scrubbed, "Based on")
		assert.NotContains(t, result.Scrubbed, "Document 1")
		assert.NotContains(t, result.Scrubbed, "Document 2")
		assert.NotContains(t, result.Scrubbed, "Eiffel Tower")
	})

	t.Run("inline citation scrubbing", func(t *testing.T) {
		context := `Go was designed at Google [1]. It was first released in 2009 [2].
The language emphasizes simplicity [1][3].

Sources:
[1] Go documentation
[2] Wikipedia
[3] Rob Pike interview`

		// Remove source citations
		citations := []Citation{
			{SpanID: "1"},
			{SpanID: "2"},
			{SpanID: "3"},
		}
		evidenceMap := map[string]string{
			"1": "Go documentation",
			"2": "Wikipedia",
			"3": "Rob Pike interview",
		}

		result := scrubber.ScrubByCitations(context, citations, evidenceMap)

		assert.NotContains(t, result.Scrubbed, "Go documentation")
		assert.NotContains(t, result.Scrubbed, "Wikipedia")
		assert.NotContains(t, result.Scrubbed, "Rob Pike interview")
	})
}
