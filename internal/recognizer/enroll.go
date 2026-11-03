package recognizer

import (
	"errors"
	"strings"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
)

// Enrollment selection errors, mapped to HTTP 422 by the API layer with
// these exact messages so the UI can explain what to fix.
var (
	// ErrNoFace marks a photo without any detected face.
	ErrNoFace = errors.New("no face detected")
	// ErrMultipleFaces marks photos with several similarly sized faces.
	ErrMultipleFaces = errors.New("registration image contains multiple similarly sized faces")
)

// SecondaryFaceRatio is the area ratio at which a second face counts as
// "similarly sized" and blocks enrollment (0.35, like the reference).
const SecondaryFaceRatio = 0.35

// MinEnrollmentConsistency is the lowest similarity a new sample may have
// against the same employee's existing samples (0.35, like the reference).
const MinEnrollmentConsistency = 0.35

// SelectEnrollmentFace picks the single enrollment-suitable face: the
// largest one, alone in the frame at that size, passing every quality gate.
func SelectEnrollmentFace(faces []engine.ExtractedFace) (engine.ExtractedFace, error) {
	if len(faces) == 0 {
		return engine.ExtractedFace{}, ErrNoFace
	}
	face := faces[0] // Extract already returns largest first
	if len(faces) > 1 {
		primary := faceArea(face)
		secondary := faceArea(faces[1])
		if secondary >= primary*SecondaryFaceRatio {
			return engine.ExtractedFace{}, ErrMultipleFaces
		}
	}
	if !face.Quality.Accepted || face.Embedding == nil {
		reasons := strings.Join(face.Quality.Reasons, ", ")
		if reasons == "" {
			reasons = "quality check failed"
		}
		return engine.ExtractedFace{}, errors.New("face is not suitable for registration: " + reasons)
	}
	return face, nil
}

// ConsistentWith reports whether the embedding agrees with at least one of
// the employee's existing samples (same model version, embeddings stored).
// An empty sample set is always consistent.
func ConsistentWith(embedding []float32, samples [][]float32, minConsistency float64) bool {
	if len(samples) == 0 {
		return true
	}
	query := normalize(embedding)
	if query == nil {
		return false
	}
	for _, sample := range samples {
		if len(sample) != len(query) {
			continue
		}
		if dot(query, normalize(sample)) >= minConsistency {
			return true
		}
	}
	return false
}

func faceArea(face engine.ExtractedFace) float64 {
	box := face.Face.BBoxXYXY
	width := float64(box[2] - box[0])
	height := float64(box[3] - box[1])
	if width <= 0 || height <= 0 {
		return 0
	}
	return width * height
}
