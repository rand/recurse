// Package hallucination provides information-theoretic hallucination detection.
package hallucination

import (
	"sort"
	"strings"
)

// EvidenceMarker is the replacement text for scrubbed evidence.
const EvidenceMarker = "[EVIDENCE REMOVED]"

// EvidenceScrubber removes cited evidence from context to compute pseudo-prior.
// [SPEC-08.10] [SPEC-08.11]
type EvidenceScrubber struct {
	// Marker is the replacement text for scrubbed evidence.
	Marker string

	// PreserveStructure keeps paragraph/section breaks when scrubbing.
	PreserveStructure bool
}

// NewEvidenceScrubber creates a new evidence scrubber with default settings.
func NewEvidenceScrubber() *EvidenceScrubber {
	return &EvidenceScrubber{
		Marker:            EvidenceMarker,
		PreserveStructure: true,
	}
}

// EvidenceSpan represents a region of text that constitutes evidence.
type EvidenceSpan struct {
	// ID identifies this evidence span (e.g., citation ID).
	ID string

	// StartPos is the start position in the text.
	StartPos int

	// EndPos is the end position in the text (exclusive).
	EndPos int

	// Text is the evidence content.
	Text string
}

// ScrubResult contains the scrubbed context and metadata.
type ScrubResult struct {
	// Original is the original context text.
	Original string

	// Scrubbed is the context with evidence removed.
	Scrubbed string

	// RemovedSpans lists the evidence spans that were removed.
	RemovedSpans []EvidenceSpan

	// SpanCount is the number of spans removed.
	SpanCount int
}

// ScrubByPositions removes evidence at the specified positions.
// [SPEC-08.10]
func (s *EvidenceScrubber) ScrubByPositions(context string, spans []EvidenceSpan) ScrubResult {
	if len(spans) == 0 {
		return ScrubResult{
			Original: context,
			Scrubbed: context,
		}
	}

	// Sort spans by start position
	sortedSpans := make([]EvidenceSpan, len(spans))
	copy(sortedSpans, spans)
	sort.Slice(sortedSpans, func(i, j int) bool {
		return sortedSpans[i].StartPos < sortedSpans[j].StartPos
	})

	// Merge overlapping spans
	mergedSpans := s.mergeOverlapping(sortedSpans)

	// Build scrubbed text
	var result strings.Builder
	lastEnd := 0
	var removedSpans []EvidenceSpan

	for _, span := range mergedSpans {
		// Validate positions
		if span.StartPos < 0 || span.EndPos > len(context) || span.StartPos >= span.EndPos {
			continue
		}

		// Add text before this span
		if span.StartPos > lastEnd {
			result.WriteString(context[lastEnd:span.StartPos])
		}

		// Add marker (with structure preservation if enabled)
		if s.PreserveStructure {
			result.WriteString(s.structuredMarker(context[span.StartPos:span.EndPos]))
		} else {
			result.WriteString(s.Marker)
		}

		// Track removed span
		removedSpans = append(removedSpans, EvidenceSpan{
			ID:       span.ID,
			StartPos: span.StartPos,
			EndPos:   span.EndPos,
			Text:     context[span.StartPos:span.EndPos],
		})

		lastEnd = span.EndPos
	}

	// Add remaining text
	if lastEnd < len(context) {
		result.WriteString(context[lastEnd:])
	}

	return ScrubResult{
		Original:     context,
		Scrubbed:     result.String(),
		RemovedSpans: removedSpans,
		SpanCount:    len(removedSpans),
	}
}

// ScrubByCitations removes evidence based on citation references.
// The evidenceMap maps citation IDs to their text content.
func (s *EvidenceScrubber) ScrubByCitations(context string, citations []Citation, evidenceMap map[string]string) ScrubResult {
	var spans []EvidenceSpan

	for _, citation := range citations {
		evidenceText, ok := evidenceMap[citation.SpanID]
		if !ok {
			continue
		}

		// Find all occurrences of this evidence in the context
		positions := s.findAllOccurrences(context, evidenceText)
		for _, pos := range positions {
			spans = append(spans, EvidenceSpan{
				ID:       citation.SpanID,
				StartPos: pos,
				EndPos:   pos + len(evidenceText),
				Text:     evidenceText,
			})
		}
	}

	return s.ScrubByPositions(context, spans)
}

// ScrubByPattern removes text matching a pattern.
// Useful for removing inline citations like "According to [1]..."
func (s *EvidenceScrubber) ScrubByPattern(context string, pattern string) ScrubResult {
	var spans []EvidenceSpan

	positions := s.findAllOccurrences(context, pattern)
	for _, pos := range positions {
		spans = append(spans, EvidenceSpan{
			ID:       "pattern",
			StartPos: pos,
			EndPos:   pos + len(pattern),
			Text:     pattern,
		})
	}

	return s.ScrubByPositions(context, spans)
}

// ScrubSection removes an entire section of text (e.g., "Evidence:" block).
func (s *EvidenceScrubber) ScrubSection(context string, sectionStart, sectionEnd string) ScrubResult {
	var spans []EvidenceSpan

	startIdx := 0
	for {
		// Find section start
		start := strings.Index(context[startIdx:], sectionStart)
		if start == -1 {
			break
		}
		start += startIdx

		// Find section end
		endSearch := start + len(sectionStart)
		end := strings.Index(context[endSearch:], sectionEnd)
		if end == -1 {
			// Section extends to end of context
			end = len(context)
		} else {
			end = endSearch + end + len(sectionEnd)
		}

		spans = append(spans, EvidenceSpan{
			ID:       "section",
			StartPos: start,
			EndPos:   end,
			Text:     context[start:end],
		})

		startIdx = end
	}

	return s.ScrubByPositions(context, spans)
}

// mergeOverlapping merges overlapping spans into single spans.
func (s *EvidenceScrubber) mergeOverlapping(spans []EvidenceSpan) []EvidenceSpan {
	if len(spans) == 0 {
		return spans
	}

	var merged []EvidenceSpan
	current := spans[0]

	for i := 1; i < len(spans); i++ {
		span := spans[i]

		// Check for overlap or adjacency
		if span.StartPos <= current.EndPos {
			// Extend current span
			if span.EndPos > current.EndPos {
				current.EndPos = span.EndPos
			}
			// Combine IDs if different
			if current.ID != span.ID {
				current.ID = current.ID + "+" + span.ID
			}
		} else {
			// No overlap, save current and start new
			merged = append(merged, current)
			current = span
		}
	}

	// Don't forget the last span
	merged = append(merged, current)

	return merged
}

// structuredMarker creates a marker that preserves structural elements.
// [SPEC-08.11]
func (s *EvidenceScrubber) structuredMarker(removed string) string {
	// Count paragraph breaks in removed content
	paragraphs := strings.Count(removed, "\n\n")
	if paragraphs > 0 {
		// Preserve paragraph structure
		var parts []string
		for i := 0; i <= paragraphs; i++ {
			parts = append(parts, s.Marker)
		}
		return strings.Join(parts, "\n\n")
	}

	// Count single line breaks
	lines := strings.Count(removed, "\n")
	if lines > 0 {
		var parts []string
		for i := 0; i <= lines; i++ {
			parts = append(parts, s.Marker)
		}
		return strings.Join(parts, "\n")
	}

	return s.Marker
}

// findAllOccurrences finds all non-overlapping occurrences of needle in haystack.
func (s *EvidenceScrubber) findAllOccurrences(haystack, needle string) []int {
	var positions []int

	if needle == "" {
		return positions
	}

	offset := 0
	for {
		idx := strings.Index(haystack[offset:], needle)
		if idx == -1 {
			break
		}
		positions = append(positions, offset+idx)
		offset += idx + len(needle)
	}

	return positions
}

// CreateScrubbedContext is a convenience function that creates a scrubbed
// version of the context for p0 estimation.
func CreateScrubbedContext(context string, evidenceSpans []EvidenceSpan) string {
	scrubber := NewEvidenceScrubber()
	result := scrubber.ScrubByPositions(context, evidenceSpans)
	return result.Scrubbed
}

// EvidenceSpansFromClaims extracts evidence spans from claims with resolved citations.
func EvidenceSpansFromClaims(context string, claims []ClaimWithCitedEvidence) []EvidenceSpan {
	var spans []EvidenceSpan
	scrubber := NewEvidenceScrubber()

	for _, cwe := range claims {
		if cwe.Evidence == "" {
			continue
		}

		// Find where the evidence appears in the context
		positions := scrubber.findAllOccurrences(context, cwe.Evidence)
		for _, pos := range positions {
			spans = append(spans, EvidenceSpan{
				ID:       "evidence",
				StartPos: pos,
				EndPos:   pos + len(cwe.Evidence),
				Text:     cwe.Evidence,
			})
		}
	}

	return spans
}
