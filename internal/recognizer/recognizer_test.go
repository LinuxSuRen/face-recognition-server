package recognizer

import (
	"errors"
	"math"
	"testing"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
)

func unitVector(dim, index int) []float32 {
	values := make([]float32, dim)
	values[index] = 1
	return values
}

func TestMatchKnownUnknownAndEmptyLibrary(t *testing.T) {
	templates := []Template{
		{EmployeeID: 1, EmployeeNo: "E001", Name: "张三", Embedding: unitVector(4, 0)},
		{EmployeeID: 2, EmployeeNo: "E002", Name: "李四", Embedding: unitVector(4, 1)},
	}
	known := Match(unitVector(4, 0), templates, DefaultThresholds)
	if known.Status != "known" || known.EmployeeID != 1 || known.Name != "张三" {
		t.Fatalf("expected known 张三, got %+v", known)
	}
	if known.Similarity != 1 {
		t.Fatalf("similarity = %v, want 1", known.Similarity)
	}

	// a face between the two identities: best is 0.7 above unknown (0.35)
	mixed := []float32{0.7, 0.71425, 0, 0}
	inBetween := Match(mixed, templates, DefaultThresholds)
	if inBetween.Status != "known" {
		t.Fatalf("0.70+ similarity must be known, got %+v", inBetween)
	}

	weak := Match([]float32{0.2, 0.2, 0.2, 0.9}, templates, DefaultThresholds)
	if weak.Status != "unknown" || weak.Reason != "below_threshold" {
		t.Fatalf("weak match must be below_threshold, got %+v", weak)
	}
	if weak.CandidateEmployeeID == 0 || weak.CandidateName == "" {
		t.Fatalf("candidate must be reported even when unknown: %+v", weak)
	}

	empty := Match(unitVector(4, 0), nil, DefaultThresholds)
	if empty.Status != "unknown" || empty.Reason != "empty_face_library" {
		t.Fatalf("no templates must be empty_face_library, got %+v", empty)
	}
}

func TestMatchUsesMaxTemplateScorePerEmployee(t *testing.T) {
	templates := []Template{
		{EmployeeID: 1, EmployeeNo: "E001", Name: "张三", Embedding: []float32{0.2, 0.98}},
		{EmployeeID: 1, EmployeeNo: "E001", Name: "张三", Embedding: unitVector(2, 0)},
	}
	result := Match(unitVector(2, 0), templates, DefaultThresholds)
	if result.Status != "known" || result.EmployeeID != 1 {
		t.Fatalf("max-template scoring failed: %+v", result)
	}
	if result.Similarity != 1 {
		t.Fatalf("expected the best sample to win with 1.0, got %v", result.Similarity)
	}
}

func TestMatchSkipsDimensionMismatchesAndBrokenEmbeddings(t *testing.T) {
	templates := []Template{
		{EmployeeID: 1, EmployeeNo: "E001", Name: "张三", Embedding: unitVector(8, 0)},
		{EmployeeID: 2, EmployeeNo: "E002", Name: "李四", Embedding: []float32{0, 0, 0, 0}},
	}
	result := Match(unitVector(4, 0), templates, DefaultThresholds)
	if result.Status != "unknown" || result.Reason != "empty_face_library" {
		t.Fatalf("mismatched dims and zero vectors must be skipped: %+v", result)
	}
	unusable := Match(nil, nil, DefaultThresholds)
	if unusable.Status != "unusable" || unusable.Reason != "poor_quality" {
		t.Fatalf("nil embedding must be unusable: %+v", unusable)
	}
}

func TestMatchMargin(t *testing.T) {
	templates := []Template{
		{EmployeeID: 1, EmployeeNo: "E001", Name: "张三", Embedding: unitVector(2, 0)},
		{EmployeeID: 2, EmployeeNo: "E002", Name: "李四", Embedding: []float32{0.6, 0.8}},
	}
	query := []float32{0.93633, 0.35112} // ~0.9363 vs #1, ~0.8427 vs #2
	result := Match(query, templates, DefaultThresholds)
	if result.Status != "known" || result.EmployeeID != 1 {
		t.Fatalf("unexpected verdict: %+v", result)
	}
	if math.Abs(result.Margin-0.0936) > 0.001 {
		t.Fatalf("margin = %v, want ~0.0936", result.Margin)
	}
}

func TestConsistentWith(t *testing.T) {
	samples := [][]float32{unitVector(3, 0)}
	if !ConsistentWith(unitVector(3, 0), samples, MinEnrollmentConsistency) {
		t.Fatalf("identical sample must be consistent")
	}
	if !ConsistentWith(unitVector(3, 0), nil, MinEnrollmentConsistency) {
		t.Fatalf("empty history must be consistent")
	}
	// 0.5 similarity beats the 0.35 floor
	if !ConsistentWith([]float32{0.5, 0.5, 0.70711}, samples, MinEnrollmentConsistency) {
		t.Fatalf("0.5 similarity must pass the 0.35 floor")
	}
	if ConsistentWith(unitVector(3, 2), samples, MinEnrollmentConsistency) {
		t.Fatalf("orthogonal sample must fail the consistency floor")
	}
}

func extractedFace(size float32, qualityOK bool, embedding []float32) engine.ExtractedFace {
	reasons := []string{}
	accepted := qualityOK
	if !qualityOK {
		reasons = append(reasons, "image_too_blurry")
	}
	return engine.ExtractedFace{
		Face: engine.Face{
			BBoxXYXY: [4]float32{10, 10, 10 + size, 10 + size},
			Score:    0.95,
		},
		Quality:   engine.Quality{Accepted: accepted, Score: 0.9, Reasons: reasons},
		Embedding: embedding,
	}
}

func TestSelectEnrollmentFace(t *testing.T) {
	if _, err := SelectEnrollmentFace(nil); !errors.Is(err, ErrNoFace) {
		t.Fatalf("no faces must be ErrNoFace, got %v", err)
	}

	// second face with 40% of the primary area blocks enrollment
	faces := []engine.ExtractedFace{
		extractedFace(100, true, unitVector(4, 0)),
		extractedFace(63.3, true, unitVector(4, 1)),
	}
	if _, err := SelectEnrollmentFace(faces); !errors.Is(err, ErrMultipleFaces) {
		t.Fatalf("similarly sized faces must be ErrMultipleFaces, got %v", err)
	}

	// a much smaller second face is tolerated
	faces[1] = extractedFace(30, true, unitVector(4, 1))
	selected, err := SelectEnrollmentFace(faces)
	if err != nil || selected.Face.BBoxXYXY[2] != 110 {
		t.Fatalf("largest face must win: %v %+v", err, selected)
	}

	blurry := []engine.ExtractedFace{extractedFace(100, false, unitVector(4, 0))}
	if _, err := SelectEnrollmentFace(blurry); err == nil ||
		err.Error() != "face is not suitable for registration: image_too_blurry" {
		t.Fatalf("quality failure must carry the reason, got %v", err)
	}
}
