package engine

import "math"

// normalizeEmbedding returns a unit-length copy of the vector. SFace outputs
// are near-unit already; normalizing makes cosine similarity a plain dot
// product and stabilizes thresholds across model builds.
func normalizeEmbedding(values []float32) []float32 {
	var norm float64
	for _, v := range values {
		norm += float64(v) * float64(v)
	}
	norm = sqrt(norm)
	if norm <= 1e-12 {
		return nil
	}
	out := make([]float32, len(values))
	for i, v := range values {
		out[i] = float32(float64(v) / norm)
	}
	return out
}

// cosine returns the cosine similarity of two unit-length vectors.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return -1
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

func sqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}
