//go:build cgo && integration

package onnxrt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LinuxSuRen/face-recognition-server/internal/engine"
	"github.com/LinuxSuRen/face-recognition-server/internal/recognizer"
)

// TestIntegrationEngineRecognizesRealFace runs the full ONNX Runtime
// pipeline against a real photo. It requires:
//   - the models under $FACE_RECOGNITION_MODELS (scripts/download-models)
//   - the onnxruntime shared library loadable (FACE_RECOGNITION_ORT_LIB)
//   - network access to fetch the classic OpenCV sample photo on first run
//
// The test is skipped when the models are absent so plain CI still passes.
func TestIntegrationEngineRecognizesRealFace(t *testing.T) {
	modelsDir := os.Getenv("FACE_RECOGNITION_MODELS")
	if modelsDir == "" {
		modelsDir = "models"
	}
	detector := filepath.Join(modelsDir, "face_detection_yunet.onnx")
	recognizerModel := filepath.Join(modelsDir, "face_recognition_sface.onnx")
	if _, err := os.Stat(detector); err != nil {
		t.Skipf("detector model missing, run scripts/download-models: %v", err)
	}
	if _, err := os.Stat(recognizerModel); err != nil {
		t.Skipf("recognizer model missing, run scripts/download-models: %v", err)
	}

	photo, err := fetchSamplePhoto(t)
	if err != nil {
		t.Skipf("sample photo unavailable (offline?): %v", err)
	}
	img, err := engine.DecodeImage(photo)
	if err != nil {
		t.Fatalf("decoding sample photo: %v", err)
	}

	faceEngine, err := engine.New(engine.Options{
		DetectorPath:   detector,
		RecognizerPath: recognizerModel,
	})
	if err != nil {
		t.Fatalf("building engine: %v", err)
	}
	defer faceEngine.Close()

	faces, err := faceEngine.Extract(img)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if len(faces) == 0 {
		t.Fatalf("no face detected in the sample photo")
	}
	largest := faces[0]
	if largest.Face.Score < 0.5 {
		t.Fatalf("detector confidence too low: %v", largest.Face.Score)
	}
	if len(largest.Embedding) != 128 {
		t.Fatalf("SFace embedding dimension = %d, want 128", len(largest.Embedding))
	}
	if largest.Quality.FaceSize <= 0 {
		t.Fatalf("quality assessment missing: %+v", largest.Quality)
	}

	// re-extracting must be deterministic enough for self-recognition
	again, err := faceEngine.Extract(img)
	if err != nil {
		t.Fatalf("second extract failed: %v", err)
	}
	if len(again) == 0 {
		t.Fatalf("second pass found no face")
	}
	similarity := recognizer.Match(again[0].Embedding, []recognizer.Template{
		{EmployeeID: 1, EmployeeNo: "T", Name: "self", Embedding: largest.Embedding},
	}, recognizer.DefaultThresholds)
	if similarity.Status != "known" {
		t.Fatalf("identical photo must self-match, got %+v", similarity)
	}
}
