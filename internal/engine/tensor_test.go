package engine

import (
	"math"
	"testing"
)

func TestBuildNCHWScalesPixels(t *testing.T) {
	img := NewImage(2, 1)
	img.Set(0, 0, 255, 0, 127)
	img.Set(1, 0, 10, 20, 30)

	detector := buildNCHW(img, 0, 1.0/255.0)
	if got := detector[0]; math.Abs(float64(got)-1.0) > 1e-6 {
		t.Fatalf("R(0,0) = %v, want 1", got)
	}
	if got := detector[1]; math.Abs(float64(got)-10.0/255.0) > 1e-6 { // plane 0 (R), pixel 1
		t.Fatalf("R(1,0) = %v, want %v", got, 10.0/255.0)
	}
	if got := detector[2+0]; got != 0 { // plane 1 (G), pixel 0
		t.Fatalf("G(0,0) = %v, want 0", got)
	}
	if got := detector[4+0]; math.Abs(float64(got)-127.0/255.0) > 1e-6 { // plane 2 (B), pixel 0
		t.Fatalf("B(0,0) = %v, want %v", got, 127.0/255.0)
	}

	recognizer := buildNCHW(img, 127.5, 1.0/128.0)
	wantR := (255 - 127.5) / 128.0
	if math.Abs(float64(recognizer[0])-wantR) > 1e-6 {
		t.Fatalf("SFace R(0,0) = %v, want %v", recognizer[0], wantR)
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
