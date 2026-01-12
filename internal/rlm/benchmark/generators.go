package benchmark

import (
	"fmt"
	"math/rand"
	"strings"
)

// =============================================================================
// OOLONG-style Synthetic Task Generators
// =============================================================================

// CountingGenerator creates tasks that require counting occurrences in context.
// This tests linear complexity: difficulty scales with context length.
type CountingGenerator struct {
	rng *rand.Rand
}

// NewCountingGenerator creates a new counting task generator.
func NewCountingGenerator(seed int64) *CountingGenerator {
	return &CountingGenerator{
		rng: rand.New(rand.NewSource(seed)),
	}
}

// Generate creates counting tasks at the specified context length.
func (g *CountingGenerator) Generate(contextTokens int, count int) ([]Task, error) {
	tasks := make([]Task, count)

	for i := 0; i < count; i++ {
		task, err := g.generateOne(contextTokens, i)
		if err != nil {
			return nil, err
		}
		tasks[i] = task
	}

	return tasks, nil
}

func (g *CountingGenerator) generateOne(contextTokens int, idx int) (Task, error) {
	// Generate a context with embedded items to count
	categories := []string{"apple", "banana", "cherry", "date", "elderberry"}
	targetCategory := categories[g.rng.Intn(len(categories))]

	// Estimate ~4 chars per token
	targetChars := contextTokens * 4
	var sb strings.Builder

	itemCount := 0
	totalCount := 0

	// Generate sentences with random items
	templates := []string{
		"The customer ordered a %s.",
		"We received a shipment of %s today.",
		"The recipe calls for fresh %s.",
		"She picked up some %s from the store.",
		"The basket contained several %s.",
	}

	for sb.Len() < targetChars {
		category := categories[g.rng.Intn(len(categories))]
		template := templates[g.rng.Intn(len(templates))]
		sentence := fmt.Sprintf(template, category)
		sb.WriteString(sentence)
		sb.WriteString(" ")

		if category == targetCategory {
			itemCount++
		}
		totalCount++
	}

	return Task{
		ID:             fmt.Sprintf("counting-%d-%d", contextTokens, idx),
		Name:           "Counting Task",
		Description:    fmt.Sprintf("Count occurrences of '%s' in the text", targetCategory),
		Complexity:     ComplexityLinear,
		Context:        sb.String(),
		ContextTokens:  contextTokens,
		Query:          fmt.Sprintf("How many times does '%s' appear in the text? Answer with just the number.", targetCategory),
		ExpectedAnswer: fmt.Sprintf("%d", itemCount),
		AnswerType:     AnswerNumeric,
		Metadata: map[string]any{
			"target_category": targetCategory,
			"total_items":     totalCount,
		},
	}, nil
}

// PairingGenerator creates tasks requiring pairwise relationship reasoning.
// This tests quadratic complexity: O(n^2) comparisons needed.
type PairingGenerator struct {
	rng *rand.Rand
}

// NewPairingGenerator creates a new pairing task generator.
func NewPairingGenerator(seed int64) *PairingGenerator {
	return &PairingGenerator{
		rng: rand.New(rand.NewSource(seed)),
	}
}

// Generate creates pairing tasks at the specified context length.
func (g *PairingGenerator) Generate(contextTokens int, count int) ([]Task, error) {
	tasks := make([]Task, count)

	for i := 0; i < count; i++ {
		task, err := g.generateOne(contextTokens, i)
		if err != nil {
			return nil, err
		}
		tasks[i] = task
	}

	return tasks, nil
}

func (g *PairingGenerator) generateOne(contextTokens int, idx int) (Task, error) {
	// Generate entities with relationships
	firstNames := []string{"Alice", "Bob", "Carol", "David", "Eve", "Frank", "Grace", "Henry"}
	relationships := []string{"works with", "reported to", "collaborated with", "mentored"}

	// Calculate how many entities we can fit
	// Each relationship mention is ~50 chars, we want sparse mentions
	numEntities := min(len(firstNames), contextTokens/500+2)
	entities := firstNames[:numEntities]

	// Generate relationship matrix (sparse)
	relMatrix := make(map[string]map[string]bool)
	for _, e := range entities {
		relMatrix[e] = make(map[string]bool)
	}

	// Randomly assign some relationships
	numRelationships := numEntities * 2
	for i := 0; i < numRelationships; i++ {
		e1 := entities[g.rng.Intn(len(entities))]
		e2 := entities[g.rng.Intn(len(entities))]
		if e1 != e2 {
			relMatrix[e1][e2] = true
		}
	}

	// Generate context with relationship mentions
	targetChars := contextTokens * 4
	var sb strings.Builder

	// Add filler text and relationship mentions
	fillerTemplates := []string{
		"The quarterly report showed strong performance across all departments.",
		"Management approved the new initiative last week.",
		"The team meeting was scheduled for Thursday afternoon.",
		"Budget allocations were finalized for the upcoming fiscal year.",
		"The project timeline was extended by two weeks.",
	}

	for sb.Len() < targetChars {
		// 30% chance of relationship mention, 70% filler
		if g.rng.Float32() < 0.3 && len(entities) > 1 {
			e1 := entities[g.rng.Intn(len(entities))]
			e2 := entities[g.rng.Intn(len(entities))]
			if e1 != e2 && relMatrix[e1][e2] {
				rel := relationships[g.rng.Intn(len(relationships))]
				sb.WriteString(fmt.Sprintf("%s %s %s during this period. ", e1, rel, e2))
			}
		} else {
			sb.WriteString(fillerTemplates[g.rng.Intn(len(fillerTemplates))])
			sb.WriteString(" ")
		}
	}

	// Pick two entities to ask about
	e1 := entities[g.rng.Intn(len(entities))]
	e2 := entities[g.rng.Intn(len(entities))]
	for e2 == e1 {
		e2 = entities[g.rng.Intn(len(entities))]
	}

	hasRelationship := relMatrix[e1][e2] || relMatrix[e2][e1]
	expectedAnswer := "no"
	if hasRelationship {
		expectedAnswer = "yes"
	}

	return Task{
		ID:             fmt.Sprintf("pairing-%d-%d", contextTokens, idx),
		Name:           "Relationship Pairing",
		Description:    "Determine if two entities have a relationship",
		Complexity:     ComplexityQuadratic,
		Context:        sb.String(),
		ContextTokens:  contextTokens,
		Query:          fmt.Sprintf("Based on the text, did %s and %s have any professional relationship? Answer 'yes' or 'no'.", e1, e2),
		ExpectedAnswer: expectedAnswer,
		AnswerType:     AnswerExact,
		Metadata: map[string]any{
			"entity1":          e1,
			"entity2":          e2,
			"has_relationship": hasRelationship,
			"num_entities":     numEntities,
		},
	}, nil
}

// NeedleGenerator creates needle-in-haystack tasks.
// This tests constant complexity: one piece of information to find.
type NeedleGenerator struct {
	rng *rand.Rand
}

// NewNeedleGenerator creates a new needle task generator.
func NewNeedleGenerator(seed int64) *NeedleGenerator {
	return &NeedleGenerator{
		rng: rand.New(rand.NewSource(seed)),
	}
}

// Generate creates needle-in-haystack tasks.
func (g *NeedleGenerator) Generate(contextTokens int, count int) ([]Task, error) {
	tasks := make([]Task, count)

	for i := 0; i < count; i++ {
		task, err := g.generateOne(contextTokens, i)
		if err != nil {
			return nil, err
		}
		tasks[i] = task
	}

	return tasks, nil
}

func (g *NeedleGenerator) generateOne(contextTokens int, idx int) (Task, error) {
	// The "needle" - a specific fact to find
	secretCode := fmt.Sprintf("CODE-%04d", g.rng.Intn(10000))
	needle := fmt.Sprintf("The secret access code is %s.", secretCode)

	// Generate haystack
	targetChars := contextTokens * 4
	haystackTemplates := []string{
		"The quarterly earnings report showed positive growth across all sectors.",
		"Management has decided to implement new policies starting next month.",
		"The research team published their findings in the latest journal.",
		"Customer satisfaction scores improved by 15% compared to last year.",
		"The infrastructure upgrade project is proceeding on schedule.",
		"New security protocols have been established for all departments.",
		"The annual review process will begin in the coming weeks.",
		"Market analysis indicates favorable conditions for expansion.",
	}

	var sb strings.Builder

	// Decide where to place the needle (position as percentage of total)
	needlePosition := g.rng.Float32()
	needlePlaced := false
	needleCharPosition := int(float32(targetChars) * needlePosition)

	for sb.Len() < targetChars {
		// Place needle at the right position
		if !needlePlaced && sb.Len() >= needleCharPosition {
			sb.WriteString(needle)
			sb.WriteString(" ")
			needlePlaced = true
			continue
		}

		template := haystackTemplates[g.rng.Intn(len(haystackTemplates))]
		sb.WriteString(template)
		sb.WriteString(" ")
	}

	// Ensure needle is placed
	if !needlePlaced {
		sb.WriteString(needle)
	}

	return Task{
		ID:             fmt.Sprintf("needle-%d-%d", contextTokens, idx),
		Name:           "Needle in Haystack",
		Description:    "Find a specific piece of information in a large context",
		Complexity:     ComplexityConstant,
		Context:        sb.String(),
		ContextTokens:  contextTokens,
		Query:          "What is the secret access code mentioned in the text?",
		ExpectedAnswer: secretCode,
		AnswerType:     AnswerContains,
		Metadata: map[string]any{
			"needle_position": needlePosition,
			"secret_code":     secretCode,
		},
	}, nil
}

// AggregationGenerator creates tasks requiring multi-step aggregation.
// Tests ability to gather and combine information from multiple locations.
type AggregationGenerator struct {
	rng *rand.Rand
}

// NewAggregationGenerator creates a new aggregation task generator.
func NewAggregationGenerator(seed int64) *AggregationGenerator {
	return &AggregationGenerator{
		rng: rand.New(rand.NewSource(seed)),
	}
}

// Generate creates aggregation tasks.
func (g *AggregationGenerator) Generate(contextTokens int, count int) ([]Task, error) {
	tasks := make([]Task, count)

	for i := 0; i < count; i++ {
		task, err := g.generateOne(contextTokens, i)
		if err != nil {
			return nil, err
		}
		tasks[i] = task
	}

	return tasks, nil
}

func (g *AggregationGenerator) generateOne(contextTokens int, idx int) (Task, error) {
	// Generate sales data across multiple regions
	regions := []string{"North", "South", "East", "West", "Central"}
	targetChars := contextTokens * 4

	// Generate random sales figures
	sales := make(map[string]int)
	totalSales := 0
	for _, region := range regions {
		amount := g.rng.Intn(100000) + 10000
		sales[region] = amount
		totalSales += amount
	}

	// Build context with scattered sales mentions
	var sb strings.Builder
	fillerText := []string{
		"The market conditions remained stable throughout the quarter.",
		"Customer acquisition costs decreased by 12% year-over-year.",
		"Product development teams shipped three major releases.",
		"Employee satisfaction surveys showed positive trends.",
		"Supply chain improvements reduced delivery times.",
	}

	regionMentions := make(map[string]bool)

	for sb.Len() < targetChars {
		// Mix sales data with filler
		if g.rng.Float32() < 0.2 {
			// Mention a region's sales
			region := regions[g.rng.Intn(len(regions))]
			if !regionMentions[region] || g.rng.Float32() < 0.3 {
				sb.WriteString(fmt.Sprintf("The %s region reported sales of $%d this quarter. ", region, sales[region]))
				regionMentions[region] = true
			}
		} else {
			sb.WriteString(fillerText[g.rng.Intn(len(fillerText))])
			sb.WriteString(" ")
		}
	}

	// Ensure all regions are mentioned at least once
	for _, region := range regions {
		if !regionMentions[region] {
			sb.WriteString(fmt.Sprintf("The %s region reported sales of $%d this quarter. ", region, sales[region]))
		}
	}

	return Task{
		ID:             fmt.Sprintf("aggregation-%d-%d", contextTokens, idx),
		Name:           "Sales Aggregation",
		Description:    "Sum sales figures from multiple regions",
		Complexity:     ComplexityLinear,
		Context:        sb.String(),
		ContextTokens:  contextTokens,
		Query:          "What is the total sales amount across all regions? Answer with just the number (no $ sign).",
		ExpectedAnswer: fmt.Sprintf("%d", totalSales),
		AnswerType:     AnswerNumeric,
		Metadata: map[string]any{
			"regional_sales": sales,
			"total_sales":    totalSales,
		},
	}, nil
}
