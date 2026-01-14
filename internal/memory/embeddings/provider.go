// Package embeddings provides embedding generation and vector search functionality
// for the hypergraph memory system.
package embeddings

import (
	"context"
	"encoding/binary"
	"math"
)

// Provider generates embeddings from text.
type Provider interface {
	// Embed generates embeddings for one or more texts.
	// Returns vectors of the same length as input texts.
	Embed(ctx context.Context, texts []string) ([]Vector, error)

	// Dimensions returns the embedding dimension for this model.
	Dimensions() int

	// Model returns the model identifier.
	Model() string
}

// Vector is a dense embedding vector.
type Vector []float32

// Similarity computes cosine similarity between two vectors.
// Returns a value between -1 and 1, where 1 means identical direction.
func (v Vector) Similarity(other Vector) float32 {
	if len(v) != len(other) {
		return 0
	}
	if len(v) == 0 {
		return 0
	}

	var dot, normV, normO float32
	for i := range v {
		dot += v[i] * other[i]
		normV += v[i] * v[i]
		normO += other[i] * other[i]
	}

	if normV == 0 || normO == 0 {
		return 0
	}

	return dot / (float32(math.Sqrt(float64(normV))) * float32(math.Sqrt(float64(normO))))
}

// Distance computes the L2 (Euclidean) distance between two vectors.
func (v Vector) Distance(other Vector) float32 {
	if len(v) != len(other) {
		return float32(math.MaxFloat32)
	}

	var sum float32
	for i := range v {
		diff := v[i] - other[i]
		sum += diff * diff
	}

	return float32(math.Sqrt(float64(sum)))
}

// ToBytes serializes the vector to bytes for storage.
func (v Vector) ToBytes() []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// VectorFromBytes deserializes a vector from bytes.
func VectorFromBytes(b []byte) Vector {
	if len(b)%4 != 0 {
		return nil
	}
	v := make(Vector, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// Normalize returns a unit vector in the same direction.
func (v Vector) Normalize() Vector {
	if len(v) == 0 {
		return v
	}

	var norm float32
	for _, val := range v {
		norm += val * val
	}
	if norm == 0 {
		return v
	}

	norm = float32(math.Sqrt(float64(norm)))
	result := make(Vector, len(v))
	for i, val := range v {
		result[i] = val / norm
	}
	return result
}
