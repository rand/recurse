// Package hallucination provides information-theoretic hallucination detection.
package hallucination

import (
	"regexp"
	"strings"
	"unicode"
)

// Claim represents an atomic verifiable assertion extracted from text.
// [SPEC-08.07] [SPEC-08.08]
type Claim struct {
	// Content is the assertion text.
	Content string

	// Citations are references to evidence spans supporting this claim.
	Citations []Citation

	// Confidence is the stated or inferred confidence level (0.0-1.0).
	// Default is 0.9 for unqualified assertions.
	Confidence float64

	// Source identifies where this claim originated (e.g., "response", "memory").
	Source string

	// Position is the character offset in the original text.
	Position int

	// IsAssertive indicates whether this is a verifiable assertion.
	// Non-assertive spans (questions, instructions) have this set to false.
	// [SPEC-08.09]
	IsAssertive bool
}

// Citation references an evidence span that supports a claim.
type Citation struct {
	// SpanID is the citation identifier (e.g., "1", "source-a").
	SpanID string

	// Text is the cited evidence text (if available).
	Text string

	// StartPos is the start position in the original evidence.
	StartPos int

	// EndPos is the end position in the original evidence.
	EndPos int
}

// ClaimExtractor extracts atomic claims from text.
// [SPEC-08.07]
type ClaimExtractor struct {
	// citationPattern matches citation references like [1], [source], etc.
	citationPattern *regexp.Regexp

	// hedgePatterns identify hedged/uncertain statements.
	hedgePatterns []*regexp.Regexp

	// DefaultConfidence for unqualified assertions.
	DefaultConfidence float64

	// MinClaimLength filters out very short claims.
	MinClaimLength int
}

// NewClaimExtractor creates a new claim extractor with default settings.
func NewClaimExtractor() *ClaimExtractor {
	return &ClaimExtractor{
		citationPattern: regexp.MustCompile(`\[([^\]]+)\]`),
		hedgePatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)^(I think|I believe|maybe|perhaps|possibly|probably|it seems|appears to)`),
			regexp.MustCompile(`(?i)\b(might|could be|may be|could)\b`),
			regexp.MustCompile(`(?i)(I'm not sure|not certain|unclear|uncertain)`),
			regexp.MustCompile(`(?i)(approximately|roughly|about|around|estimated)`),
		},
		DefaultConfidence: 0.9,
		MinClaimLength:    10,
	}
}

// Extract extracts all claims from the given text.
// Returns both assertive and non-assertive spans (marked via IsAssertive).
func (e *ClaimExtractor) Extract(text, source string) []Claim {
	var claims []Claim

	// Split text into sentences
	sentences := e.splitSentences(text)

	position := 0
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if len(sentence) < e.MinClaimLength {
			position += len(sentence) + 1
			continue
		}

		claim := Claim{
			Content:     sentence,
			Source:      source,
			Position:    position,
			IsAssertive: e.isAssertive(sentence),
			Confidence:  e.inferConfidence(sentence),
			Citations:   e.extractCitations(sentence),
		}

		claims = append(claims, claim)
		position += len(sentence) + 1
	}

	return claims
}

// ExtractAssertive extracts only assertive claims (filters non-assertive spans).
// [SPEC-08.09]
func (e *ClaimExtractor) ExtractAssertive(text, source string) []Claim {
	all := e.Extract(text, source)
	var assertive []Claim
	for _, c := range all {
		if c.IsAssertive {
			assertive = append(assertive, c)
		}
	}
	return assertive
}

// splitSentences splits text into sentences.
// Uses simple heuristics: split on . ! ? followed by space and uppercase.
func (e *ClaimExtractor) splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		current.WriteRune(r)

		// Check for sentence boundary
		if r == '.' || r == '!' || r == '?' {
			// Look ahead for space + uppercase or end of text
			if i+1 >= len(runes) {
				// End of text
				sentences = append(sentences, current.String())
				current.Reset()
			} else if i+2 < len(runes) && runes[i+1] == ' ' && unicode.IsUpper(runes[i+2]) {
				// Space followed by uppercase - likely new sentence
				sentences = append(sentences, current.String())
				current.Reset()
				i++ // Skip the space
			} else if i+2 < len(runes) && runes[i+1] == '\n' {
				// Newline - likely new sentence
				sentences = append(sentences, current.String())
				current.Reset()
			}
		}
	}

	// Add remaining text
	if current.Len() > 0 {
		sentences = append(sentences, current.String())
	}

	return sentences
}

// isAssertive determines if a sentence is an assertive claim.
// [SPEC-08.09] Non-assertive: questions, instructions, hedged statements.
func (e *ClaimExtractor) isAssertive(sentence string) bool {
	sentence = strings.TrimSpace(sentence)

	// Questions are not assertive
	if strings.HasSuffix(sentence, "?") {
		return false
	}

	// Imperatives/instructions are not assertive
	if e.isImperative(sentence) {
		return false
	}

	// Meta-commentary about the response itself is not assertive
	if e.isMetaCommentary(sentence) {
		return false
	}

	return true
}

// isImperative checks if a sentence is an imperative/instruction.
func (e *ClaimExtractor) isImperative(sentence string) bool {
	imperativeStarters := []string{
		"Please ", "Let me ", "Let's ", "Try ", "Consider ",
		"Note that ", "Remember ", "Don't ", "Do not ",
		"Make sure ", "Be sure ", "Ensure ", "Check ",
		"Run ", "Execute ", "Open ", "Close ", "Click ",
		"Go to ", "Navigate ", "Select ", "Choose ",
	}

	lower := strings.ToLower(sentence)
	for _, starter := range imperativeStarters {
		if strings.HasPrefix(lower, strings.ToLower(starter)) {
			return true
		}
	}

	return false
}

// isMetaCommentary checks if a sentence is meta-commentary about the response.
func (e *ClaimExtractor) isMetaCommentary(sentence string) bool {
	metaPatterns := []string{
		"I'll ", "I will ", "I can ", "I would ",
		"Here's ", "Here is ", "This is ",
		"Let me explain", "To summarize", "In summary",
		"As mentioned", "As I said", "As noted",
	}

	lower := strings.ToLower(sentence)
	for _, pattern := range metaPatterns {
		if strings.HasPrefix(lower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

// inferConfidence infers the confidence level from linguistic markers.
func (e *ClaimExtractor) inferConfidence(sentence string) float64 {
	// Check for hedge patterns - reduce confidence
	for _, pattern := range e.hedgePatterns {
		if pattern.MatchString(sentence) {
			return 0.6 // Hedged statements have lower confidence
		}
	}

	// Check for strong certainty markers
	certaintyMarkers := []string{
		"definitely", "certainly", "absolutely", "always", "never",
		"must be", "is guaranteed", "without doubt",
	}
	lower := strings.ToLower(sentence)
	for _, marker := range certaintyMarkers {
		if strings.Contains(lower, marker) {
			return 0.95
		}
	}

	// Check for moderate certainty
	moderateMarkers := []string{
		"usually", "typically", "generally", "often", "commonly",
	}
	for _, marker := range moderateMarkers {
		if strings.Contains(lower, marker) {
			return 0.8
		}
	}

	return e.DefaultConfidence
}

// extractCitations extracts citation references from the sentence.
// [SPEC-08.08]
func (e *ClaimExtractor) extractCitations(sentence string) []Citation {
	var citations []Citation

	matches := e.citationPattern.FindAllStringSubmatchIndex(sentence, -1)
	for _, match := range matches {
		if len(match) >= 4 {
			fullStart, fullEnd := match[0], match[1]
			groupStart, groupEnd := match[2], match[3]

			spanID := sentence[groupStart:groupEnd]

			// Skip common non-citation brackets
			if e.isNonCitationBracket(spanID) {
				continue
			}

			citations = append(citations, Citation{
				SpanID:   spanID,
				StartPos: fullStart,
				EndPos:   fullEnd,
			})
		}
	}

	return citations
}

// isNonCitationBracket checks if bracketed content is not a citation.
func (e *ClaimExtractor) isNonCitationBracket(content string) bool {
	// Skip things like [EDIT], [NOTE], [TODO], [sic]
	nonCitations := []string{
		"edit", "note", "todo", "sic", "emphasis added",
		"emphasis mine", "citation needed", "clarification needed",
	}

	lower := strings.ToLower(content)
	for _, nc := range nonCitations {
		if lower == nc {
			return true
		}
	}

	// Skip very long bracketed content (likely not citations)
	if len(content) > 50 {
		return true
	}

	return false
}

// ClaimWithCitedEvidence pairs a claim with its cited evidence text.
type ClaimWithCitedEvidence struct {
	Claim    Claim
	Evidence string // Concatenated evidence from all citations
}

// ResolveCitations resolves citation references against an evidence map.
// The evidenceMap keys should match citation SpanIDs.
func (e *ClaimExtractor) ResolveCitations(claims []Claim, evidenceMap map[string]string) []ClaimWithCitedEvidence {
	var resolved []ClaimWithCitedEvidence

	for _, claim := range claims {
		var evidenceParts []string
		for i := range claim.Citations {
			if text, ok := evidenceMap[claim.Citations[i].SpanID]; ok {
				claim.Citations[i].Text = text
				evidenceParts = append(evidenceParts, text)
			}
		}

		resolved = append(resolved, ClaimWithCitedEvidence{
			Claim:    claim,
			Evidence: strings.Join(evidenceParts, "\n"),
		})
	}

	return resolved
}

// StripCitations removes citation markers from claim content.
// Useful for comparing claims without citation noise.
func (e *ClaimExtractor) StripCitations(content string) string {
	return e.citationPattern.ReplaceAllString(content, "")
}
