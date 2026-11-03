package engine

import (
	"math"
	"testing"
)

func TestBuildNCHWRawTensorOrdersChannels(t *testing.T) {
	img := NewImage(2, 1)
	img.Set(0, 0, 255, 0, 127)
	img.Set(1, 0, 10, 20, 30)

	// detector tensor: raw values, BGR plane order
	detector := buildNCHWRawBGR(img)
	if got := detector[0]; got != 127 {
		t.Fatalf("B(0,0) = %v, want 127", got)
	}
	if got := detector[1]; got != 30 { // plane 0 (B), pixel 1
		t.Fatalf("B(1,0) = %v, want 30", got)
	}
	if got := detector[2+0]; got != 0 { // plane 1 (G), pixel 0
		t.Fatalf("G(0,0) = %v, want 0", got)
	}
	if got := detector[4+0]; got != 255 { // plane 2 (R), pixel 0
		t.Fatalf("R(0,0) = %v, want 255", got)
	}

	// recognizer tensor: raw values, RGB plane order
	recognizer := buildNCHWRawRGB(img)
	if recognizer[0] != 255 || recognizer[1] != 10 {
		t.Fatalf("R plane wrong: %v %v", recognizer[0], recognizer[1])
	}
	if recognizer[4] != 127 {
		t.Fatalf("B plane wrong: %v", recognizer[4])
	}
}

func TestNormalizeEmbeddingProducesUnitVector(t *testing.T) {
	normalized := normalizeEmbedding([]float32{3, 4})
	if len(normalized) != 2 || math.Abs(float64(normalized[0])-0.6) > 1e-6 || math.Abs(float64(normalized[1])-0.8) > 1e-6 {
		t.Fatalf("unexpected normalized vector: %v", normalized)
	}
	if normalizeEmbedding([]float32{0, 0}) != nil {
		t.Fatalf("zero vector must be rejected")
	}
}

func TestCosineOfUnitVectors(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if got := cosine(normalizeEmbedding(a), normalizeEmbedding(a)); math.Abs(got-1) > 1e-6 {
		t.Fatalf("cos(a,a) = %v, want 1", got)
	}
	if got := cosine(a, b); math.Abs(got) > 1e-6 {
		t.Fatalf("cos(a,b) = %v, want 0", got)
	}
	if got := cosine(a, []float32{1}); got != -1 {
		t.Fatalf("length mismatch must yield -1, got %v", got)
	}
}
